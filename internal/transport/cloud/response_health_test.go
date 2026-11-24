package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// Every relay location in the map names Relay.PublishTracked in this build.
func TestTheRelayLocationsNamePublishTracked(t *testing.T) {
	want := runtime.FuncForPC(reflect.ValueOf((*Relay).PublishTracked).Pointer()).Name()
	n := 0
	for _, e := range connectionhealth.Locations() {
		if !strings.HasPrefix(e.Code, "cloud.relay.") {
			continue
		}
		n++
		if e.Function != want || !connectionhealth.ValidLocation(e.Code) {
			t.Errorf("%s names %s", e.Code, e.Function)
		}
	}
	if n != 4 {
		t.Fatalf("%d relay locations", n)
	}
}

// heldFence holds a reservation inside the spool's lock while it is on.
type heldFence struct {
	on   atomic.Bool
	held chan struct{}
	free chan struct{}
}

func (f *heldFence) Reserve(uint64) error {
	if f.on.Load() {
		f.held <- struct{}{}
		<-f.free
	}
	return nil
}

func (f *heldFence) Ceiling() (uint64, error) { return 1, nil }

type responseRig struct {
	tracker   *connectionhealth.Tracker
	spool     *adaptercloud.Spool
	transport *adaptercloud.Transport
	relay     *Relay
	viewer    domaincloud.DeviceKey
	secret    domaincloud.ContentKey
}

func newResponseRig(t *testing.T, tracker *connectionhealth.Tracker, fence adaptercloud.SequenceFence) *responseRig {
	t.Helper()
	machine, _ := domaincloud.NewDeviceKey(rand.Reader)
	viewer, _ := domaincloud.NewDeviceKey(rand.Reader)
	secret, _ := domaincloud.NewContentKey(rand.Reader)
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), fence, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	relay := &Relay{MachineID: "mac-01", Depth: 4, Spool: spool, Signer: machine, Secret: secret}
	transport, err := adaptercloud.New(adaptercloud.Options{
		RelayURL: "wss://relay.invalid", Token: &adaptercloud.TokenSource{},
		Identity: adaptercloud.Identity{AccountID: "account", MachineID: "mac-01", MachineCredential: "credential"},
		Signer:   machine, Replay: domaincloud.NewReplayWindow(64), ContentKey: secret, Spool: spool,
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) { return viewer.PublicKey(), sender == "viewer" },
		Inbound:      relay.Deliver,
		// The frame is handed on as the relay socket hands it: not direct,
		// so its answer is spooled.
		InboundTraced: func(env domaincloud.Envelope, plain []byte, key ed25519.PublicKey, correlation string, _ bool, frameStarted time.Time) {
			relay.DeliverTraced(env, plain, key, correlation, false, frameStarted)
		},
		Health: tracker,
	})
	if err != nil {
		t.Fatal(err)
	}
	relay.Transport = transport
	return &responseRig{tracker: tracker, spool: spool, transport: transport, relay: relay, viewer: viewer, secret: secret}
}

// responseBridge supplies the current roster and execution proof required by
// the pinned read rail; it does not relax the production admission rules.
func responseBridge(t *testing.T) cloudops.Bridge {
	b := answering(t)
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) cloudops.Authority {
		return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: true, ReadTranscriptAllows: true}
	}
	b.AdmitExecution = func(context.Context, string, string, string) error { return nil }
	return b
}

// request admits one signed, sealed read from the viewer and answers what
// reached the relay queue.
func (r *responseRig) request(t *testing.T) Inbound {
	t.Helper()
	env, err := domaincloud.Seal(plaintext(t, map[string]any{"type": "git", "session": "%19", "machine_id": "mac-01",
		"expected_generation": "0123456789abcdef0123456789abcdef"}),
		domaincloud.SealParams{Ch: "r/mac-01", Seq: 7, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl,
			KeyID: adaptercloud.MasterKeyID, Sender: "viewer", Key: r.secret, Signer: r.viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := env.CanonicalJSON()
	if !r.transport.AcceptDirect("viewer", raw) {
		t.Fatal("the envelope was not admitted")
	}
	select {
	case in := <-r.relay.Requests():
		return in
	default:
		t.Fatal("nothing reached the relay queue")
		return Inbound{}
	}
}

// One request keeps one correlation from the frame the transport read,
// through the lane and the handler, into the answer's phase and onto the
// spool row the drain will write it from. Nothing new is minted, the row is
// the only row, and the sealed bytes are the envelope alone.
func TestOneCorrelationRunsFromTheFrameThroughTheAnswerToTheSpoolRow(t *testing.T) {
	tracker := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	rig := newResponseRig(t, tracker, nil)
	in := rig.request(t)
	if in.Correlation != "op-1" || in.Direct {
		t.Fatalf("the request carries %q (direct %t)", in.Correlation, in.Direct)
	}
	l := &Link{opts: LinkOptions{Health: tracker}}
	service := Service{MachineID: "mac-01", Bridge: responseBridge(t), Transport: rig.relay, Log: t.Logf}
	h := l.lanePhase(in, connectionhealth.ClassRead, connectionhealth.LocReadLane)
	l.answerTraced(context.Background(), service, in, h, connectionhealth.LocReadHandler)

	if phases, _ := tracker.Snapshot(); len(phases) != 0 {
		t.Fatalf("phases left open: %+v", phases)
	}
	if rows := rig.spool.Rows(); rows != 1 {
		t.Fatalf("%d spool rows for one answer", rows)
	}
	d := rig.spool.SendNext()
	if d.Row == nil || d.Row.Correlation != "op-1" {
		t.Fatalf("the answer's row: %+v", d.Row)
	}
	if bytes.Contains(d.Row.Sealed, []byte("op-1")) {
		t.Fatal("the correlation reached the sealed envelope")
	}
	if _, err := domaincloud.DecodeEnvelope(d.Row.Sealed); err != nil {
		t.Fatalf("the sealed row is not an envelope: %v", err)
	}
	completed, failed := tracker.Counts()
	// The frame, the answer's own phase; the lane-and-handler phase is read.
	if completed[connectionhealth.ClassTransport] != 2 || completed[connectionhealth.ClassRead] != 1 ||
		failed[connectionhealth.ClassTransport] != 0 {
		t.Fatalf("counts: completed %+v failed %+v", completed, failed)
	}
	if next := tracker.NewCorrelation(); next != "op-2" {
		t.Fatalf("the chain minted extra ids; next is %s", next)
	}
}

func phaseOf(t *testing.T, tracker *connectionhealth.Tracker, correlation, location string) connectionhealth.Phase {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		phases, _ := tracker.Snapshot()
		for _, p := range phases {
			if p.Spec.Correlation == correlation && p.Spec.Location == location {
				return p
			}
		}
		time.Sleep(time.Millisecond)
	}
	phases, _ := tracker.Snapshot()
	t.Fatalf("no phase of %s at %s: %+v", correlation, location, phases)
	return connectionhealth.Phase{}
}

// An answer held at each boundary of the publish is seen there, under its
// own correlation: waiting for the spool's lock to reserve, sealing, and
// waiting for the lock again to queue the sealed bytes. Releasing them queues
// both answers once each.
func TestAHeldAnswerIsSeenAtTheBoundaryItIsHeldAt(t *testing.T) {
	tracker := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	fence := &heldFence{held: make(chan struct{}), free: make(chan struct{})}
	rig := newResponseRig(t, tracker, fence)
	var holdClock atomic.Bool
	clockHeld, clockFree := make(chan struct{}), make(chan struct{})
	rig.relay.Now = func() time.Time {
		if holdClock.Load() {
			clockHeld <- struct{}{}
			<-clockFree
		}
		return time.Now()
	}
	answer := func(correlation string, done chan<- error) {
		ctx := connectionhealth.WithCorrelation(context.Background(), tracker, correlation)
		done <- rig.relay.Publish(ctx, Outbound{Channel: "t/mac-01/%2519", Class: "stream", Payload: []byte(`{"ok":true}`)})
	}

	// b is inside the seal (the relay's clock is read there).
	holdClock.Store(true)
	bDone := make(chan error, 1)
	go answer("op-b", bDone)
	<-clockHeld
	holdClock.Store(false)
	if p := phaseOf(t, tracker, "op-b", connectionhealth.LocRelaySeal); p.Spec.Stage != connectionhealth.StageHandlerCompleted {
		t.Fatalf("sealing: %+v", p.Spec)
	}
	// a holds the spool's lock in its reservation.
	fence.on.Store(true)
	aDone := make(chan error, 1)
	go answer("op-a", aDone)
	<-fence.held
	if p := phaseOf(t, tracker, "op-a", connectionhealth.LocRelayReserve); p.Spec.Stage != connectionhealth.StageHandlerCompleted ||
		p.Spec.Class != connectionhealth.ClassTransport {
		t.Fatalf("reserving: %+v", p.Spec)
	}
	// b, sealed, now waits for that lock to queue its bytes.
	clockFree <- struct{}{}
	if p := phaseOf(t, tracker, "op-b", connectionhealth.LocRelaySpoolSeal); p.Spec.Stage != connectionhealth.StageResponseSealed {
		t.Fatalf("queueing: %+v", p.Spec)
	}
	fence.on.Store(false)
	close(fence.free)
	if err := <-aDone; err != nil {
		t.Fatal(err)
	}
	if err := <-bDone; err != nil {
		t.Fatal(err)
	}
	if phases, _ := tracker.Snapshot(); len(phases) != 0 {
		t.Fatalf("phases left open: %+v", phases)
	}
	seen := map[string]int{}
	for d := rig.spool.SendNext(); d.Row != nil; d = rig.spool.SendNext() {
		seen[d.Row.Correlation]++
	}
	if len(seen) != 2 || seen["op-a"] != 1 || seen["op-b"] != 1 {
		t.Fatalf("queued rows by correlation: %v", seen)
	}
}

// A direct answer's phase is the carrier write alone: it names
// cloud.relay.direct_write, never a spool stage, and takes no spool row.
func TestADirectAnswerIsTracedOnlyToItsCarrierWrite(t *testing.T) {
	tracker := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	rig := newResponseRig(t, tracker, nil)
	entered, free := make(chan struct{}), make(chan struct{})
	rig.relay.Direct = func(string, string, domaincloud.Class, []byte) error {
		close(entered)
		<-free
		return nil
	}
	ctx := connectionhealth.WithCorrelation(context.Background(), tracker, "op-d")
	done := make(chan error, 1)
	go func() {
		done <- rig.relay.Publish(ctx, Outbound{Channel: "t/mac-01/%2519", Class: "stream", Direct: true,
			Reply: Reply{Sender: "viewer"}, Payload: []byte(`{}`)})
	}()
	<-entered
	if p := phaseOf(t, tracker, "op-d", connectionhealth.LocRelayDirect); p.Spec.Stage != connectionhealth.StageHandlerCompleted {
		t.Fatalf("a direct write: %+v", p.Spec)
	}
	close(free)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rows := rig.spool.Rows(); rows != 0 {
		t.Fatalf("a direct answer took %d spool rows", rows)
	}
}

// With monitoring off the chain records nothing, allocates nothing for
// health, and queues the same single row with no correlation on it.
func TestAnAnswerWithoutHealthIsUnchanged(t *testing.T) {
	rig := newResponseRig(t, nil, nil)
	in := rig.request(t)
	if in.Correlation != "" {
		t.Fatalf("a request without health carries %q", in.Correlation)
	}
	l := &Link{}
	service := Service{MachineID: "mac-01", Bridge: responseBridge(t), Transport: rig.relay, Log: t.Logf}
	l.answerTraced(context.Background(), service, in, l.lanePhase(in, connectionhealth.ClassRead, connectionhealth.LocReadLane),
		connectionhealth.LocReadHandler)
	d := rig.spool.SendNext()
	if rig.spool.Rows() != 1 || d.Row == nil || d.Row.Correlation != "" {
		t.Fatalf("the row: %+v", d.Row)
	}
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		if connectionhealth.WithCorrelation(ctx, nil, "op-1") != ctx {
			t.Fatal("a nil tracker wrapped the context")
		}
		if connectionhealth.StartCarried(ctx, connectionhealth.StageHandlerCompleted, connectionhealth.LocRelayReserve).Recorded() {
			t.Fatal("an untraced context recorded a phase")
		}
	})
	if allocs != 0 {
		t.Fatalf("%v allocations with monitoring off", allocs)
	}
}
