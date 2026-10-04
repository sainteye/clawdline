package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func terminalLifecycleFixture(t *testing.T) (*Link, *adaptercloud.Spool, *terminalConnection, *terminals.Service, <-chan struct{}) {
	t.Helper()
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: time.Now().Add(time.Minute), terminalID: terminal.NewID(), client: "tab", receipts: map[string][]byte{}}
	svc := terminals.New(&stillCloudHost{}, func(terminals.Principal) error { return nil })
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: time.Now,
		TerminalService: func() (*terminals.Service, error) { return svc, nil }},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID(c.viewer, c.id): c}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer,
		Secret: master, KeyID: adaptercloud.MasterKeyID}
	retired := make(chan struct{}, 2)
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "retire" {
			retired <- struct{}{}
		}
		return nil
	}
	return l, spool, c, svc, retired
}

func TestRejectedCloudFrameRetiresConnectionInsteadOfRetrying(t *testing.T) {
	l, spool, c, svc, retired := terminalLifecycleFixture(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	frame := terminal.Frame{Rev: "still", At: time.Now(), Lines: []string{"$ "}}
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, frame); err != nil {
		t.Fatal(err)
	}
	if !c.framePending {
		t.Fatal("frame is not awaiting relay settlement")
	}
	l.terminalReceiptSettled("term/machine/viewer/"+c.id, c.framePendingSeq, adaptercloud.SettlePeerError)
	if l.getTerminalConnection(c.viewer, c.id) != nil {
		t.Fatal("rejected frame retained its connection")
	}
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("rejected frame did not retire relay registration")
	}
	before := spool.NextSequence()
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, frame); err == nil {
		t.Fatal("rejected connection sent another frame")
	}
	if spool.NextSequence() != before {
		t.Fatal("rejected frame was retried")
	}
}

func TestReleasedListConnectionRetiresAfterReceiptSettles(t *testing.T) {
	l, _, c, svc, retired := terminalLifecycleFixture(t)
	result, err := l.terminalOperation(context.Background(), svc, terminals.Principal{Device: c.viewer, Cloud: true}, c,
		terminalRequest{Operation: "release_connection"})
	if err != nil || result == nil {
		t.Fatalf("release operation: %v, %v", result, err)
	}
	if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: testRequestID, Connection: c.id, Operation: "release_connection", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if l.getTerminalConnection(c.viewer, c.id) == nil {
		t.Fatal("connection retired before release receipt settled")
	}
	var settled string
	for id := range l.terminalRetireAfterReceipt {
		settled = id
	}
	if settled == "" {
		t.Fatal("release receipt was not tracked for retirement")
	}
	const channel = "termr/machine/viewer/"
	seq, err := strconv.ParseUint(strings.TrimPrefix(settled, channel+c.id+"/"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	l.terminalReceiptSettled(channel+c.id, seq, adaptercloud.SettleDelivered)
	if l.getTerminalConnection(c.viewer, c.id) != nil {
		t.Fatal("released connection still consumes a viewer slot")
	}
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("relay registration was not retired")
	}
}

func TestRejectedOpenReceiptNeverStartsTerminalFrames(t *testing.T) {
	l, spool, c, svc, retired := terminalLifecycleFixture(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	if err := l.watchTerminal(context.Background(), svc, p, c, c.terminalID, c.client); err != nil {
		t.Fatal(err)
	}
	if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: testRequestID, Connection: c.id, Operation: "open", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if !c.watchReceiptPending || c.watchReady == nil {
		t.Fatal("first frame was not gated by the receipt")
	}
	if _, ok := spool.Row(1); ok {
		t.Fatal("frame preceded receipt acknowledgement")
	}
	l.terminalReceiptSettled("termr/machine/viewer/"+c.id, c.watchReceiptSeq, adaptercloud.SettlePeerError)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("rejected receipt did not retire")
	}
	if _, ok := spool.Row(1); ok {
		t.Fatal("rejected receipt released a frame")
	}
}

func TestUnconfirmedCloudConnectionReleasesViewerSlot(t *testing.T) {
	l, _, c, _, retired := terminalLifecycleFixture(t)
	c.unconfirmedDeadline = time.Now().Add(-time.Second)
	l.sweepTerminalConnections()
	if l.getTerminalConnection(c.viewer, c.id) != nil {
		t.Fatal("unconfirmed connection kept its viewer slot")
	}
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("unconfirmed registration was not retired")
	}
}

func TestRelayDeliveredFrameDoesNotConfirmViewerConnection(t *testing.T) {
	l, _, c, svc, retired := terminalLifecycleFixture(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	c.unconfirmedDeadline = time.Now().Add(time.Second)
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID,
		terminal.Frame{Rev: "still", At: time.Now(), Lines: []string{"$ "}}); err != nil {
		t.Fatal(err)
	}
	l.terminalReceiptSettled("term/machine/viewer/"+c.id, c.framePendingSeq, adaptercloud.SettleDelivered)
	l.terminalMu.Lock()
	deadline := c.unconfirmedDeadline
	l.terminalMu.Unlock()
	if deadline.IsZero() {
		t.Fatal("relay delivery cleared viewer confirmation deadline")
	}
	l.terminalMu.Lock()
	c.unconfirmedDeadline = time.Now().Add(-time.Second)
	l.terminalMu.Unlock()
	l.sweepTerminalConnections()
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("unobserved frame kept relay registration")
	}
}

func TestTerminalOpeningReceiptNeedsSubsequentViewerRequest(t *testing.T) {
	for _, operation := range []string{"open", "read", "rekey_connection"} {
		t.Run(operation, func(t *testing.T) {
			l, _, c, _, retired := terminalLifecycleFixture(t)
			c.unconfirmedDeadline = time.Time{}
			if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
				RequestID: testRequestID, Connection: c.id, Operation: operation, Status: "ok"}); err != nil {
				t.Fatal(err)
			}
			l.terminalMu.Lock()
			deadline := c.unconfirmedDeadline
			l.terminalMu.Unlock()
			if deadline.IsZero() {
				t.Fatal("opening receipt did not arm viewer confirmation deadline")
			}
			l.terminalMu.Lock()
			c.unconfirmedDeadline = time.Now().Add(-time.Second)
			l.terminalMu.Unlock()
			l.sweepTerminalConnections()
			select {
			case <-retired:
			case <-time.After(time.Second):
				t.Fatal("unconfirmed receipt kept relay registration")
			}
		})
	}
}

func TestUnacknowledgedFirstFrameReceiptExpiresWithoutFrame(t *testing.T) {
	l, spool, c, svc, retired := terminalLifecycleFixture(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	if err := l.watchTerminal(context.Background(), svc, p, c, c.terminalID, c.client); err != nil {
		t.Fatal(err)
	}
	if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: testRequestID, Connection: c.id, Operation: "read", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	l.terminalMu.Lock()
	c.watchReceiptDeadline = time.Now().Add(-time.Second)
	l.terminalMu.Unlock()
	l.sweepTerminalConnections()
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("lost receipt kept relay registration")
	}
	if l.getTerminalConnection(c.viewer, c.id) != nil {
		t.Fatal("lost receipt kept the connection")
	}
	if _, ok := spool.Row(1); ok {
		t.Fatal("lost receipt released a frame")
	}
}

func TestRekeyReceiptStartsInheritedWatchAfterDelivery(t *testing.T) {
	l, spool, c, _, _ := terminalLifecycleFixture(t)
	c.rekeyPending = true
	if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: testRequestID, Connection: c.id, Operation: "rekey_connection", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if !c.rekeyReceiptPending {
		t.Fatal("rekey receipt is not tracked")
	}
	if _, ok := spool.Row(1); ok {
		t.Fatal("new frame preceded rekey receipt acknowledgement")
	}
	l.terminalReceiptSettled("termr/machine/viewer/"+c.id, c.rekeyReceiptSeq, adaptercloud.SettleDelivered)
	deadline := time.After(time.Second)
	for {
		if row, ok := spool.Row(1); ok {
			env, err := domaincloud.DecodeEnvelope(row.Sealed)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := env.Open(c.key, func(string) (ed25519.PublicKey, bool) { return l.relay.Signer.PublicKey(), true })
			if err != nil {
				t.Fatal(err)
			}
			var frame struct {
				TerminalID string `json:"terminal_id"`
				FrameSeq   uint64 `json:"frame_seq"`
				Frame      struct {
					Lines []string `json:"lines"`
				} `json:"frame"`
			}
			if err := json.Unmarshal(plain, &frame); err != nil {
				t.Fatal(err)
			}
			if env.Ch != "term/machine/viewer/"+c.id || frame.FrameSeq != 1 || frame.TerminalID != string(c.terminalID) || len(frame.Frame.Lines) != 1 {
				t.Fatalf("rekey did not inherit and sign a full first frame: %+v", frame)
			}
			l.closeTerminalConnection(c)
			return
		}
		select {
		case <-deadline:
			t.Fatal("rekey receipt did not release a complete first frame")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRekeyBodyUsesFixedEnvelopeFields(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"old_connection": testConnection})
	request := terminalRequest{V: 1, Type: "terminal_request", RequestID: testRequestID,
		Connection: "AQEBAQEBAQEBAQEBAQEBAQ", Operation: "rekey_connection", KeyID: testKeyID,
		Key: base64.StdEncoding.EncodeToString(key), Body: body}
	raw, _ := json.Marshal(request)
	if _, err := decodeTerminalRequest(raw); err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"previous_connection":"`+testConnection+`"}`)...)
	if _, err := decodeTerminalRequest(raw); err == nil {
		t.Fatal("obsolete top-level previous_connection was admitted")
	}
}

func TestRekeyNeedsSameViewerAndLiveInheritedTerminal(t *testing.T) {
	l, _, c, _, _ := terminalLifecycleFixture(t)
	req := terminalRequest{Connection: "AQEBAQEBAQEBAQEBAQEBAQ", Operation: "rekey_connection",
		Body: json.RawMessage(`{"old_connection":"` + testConnection + `"}`)}
	old, code := l.terminalRekeySource("viewer", req)
	if code != "" || old != c || !old.terminalID.Valid() || old.client != "tab" {
		t.Fatalf("valid old connection was not inherited: old=%p code=%s", old, code)
	}
	if old, code := l.terminalRekeySource("another-viewer", req); old != nil || code != string(terminal.CodeInvalid) {
		t.Fatal("another viewer inherited a terminal connection")
	}
	if old, code := l.terminalRekeySource("viewer", terminalRequest{Connection: req.Connection,
		Body: json.RawMessage(`{"old_connection":"` + testConnection + `","extra":true}`)}); old != nil || code != string(terminal.CodeInvalid) {
		t.Fatal("rekey accepted an expanded body")
	}
	l.terminalMu.Lock()
	c.denied = true
	l.terminalMu.Unlock()
	if old, code := l.terminalRekeySource("viewer", req); old != nil || code != string(terminal.CodeInvalid) {
		t.Fatal("revoked old connection was inherited")
	}
}

func TestCaptureWithoutClientKeepsLeaseIdentityForRekey(t *testing.T) {
	l, _, c, svc, _ := terminalLifecycleFixture(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	if err := l.watchTerminal(context.Background(), svc, p, c, c.terminalID, ""); err != nil {
		t.Fatal(err)
	}
	l.terminalMu.Lock()
	client := c.client
	l.terminalMu.Unlock()
	if client != "tab" {
		t.Fatalf("capture erased lease client: %q", client)
	}
	l.closeTerminalConnection(c)
}

func TestActivationRetiresOldConnectionOnlyAfterDeliveredReceipt(t *testing.T) {
	for _, kind := range []adaptercloud.SettleKind{adaptercloud.SettleDelivered, adaptercloud.SettlePeerError} {
		t.Run(string(kind), func(t *testing.T) {
			l, _, c, _, retired := terminalLifecycleFixture(t)
			old := &terminalConnection{viewer: c.viewer, id: "AQEBAQEBAQEBAQEBAQEBAQ", keyID: c.keyID,
				key: c.key, expires: time.Now().Add(time.Minute), terminalID: c.terminalID, client: c.client}
			l.terminalMu.Lock()
			l.terminalConnections[terminalConnectionID(old.viewer, old.id)] = old
			l.terminalMu.Unlock()
			c.rekeyPending = true
			if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
				RequestID: testRequestID, Connection: c.id, Operation: "activate_connection", Status: "ok",
				Result: map[string]any{"connection": c.id, "retired_connection": old.id}}); err != nil {
				t.Fatal(err)
			}
			if l.getTerminalConnection(old.viewer, old.id) == nil {
				t.Fatal("old connection retired before receipt delivery")
			}
			select {
			case <-retired:
				t.Fatal("retire metadata overtook receipt")
			default:
			}
			l.terminalReceiptSettled("termr/machine/viewer/"+c.id, c.activateReceiptSeq, kind)
			if kind == adaptercloud.SettleDelivered {
				if l.getTerminalConnection(old.viewer, old.id) != nil {
					t.Fatal("delivered activation kept old connection")
				}
				if c.rekeyPending {
					t.Fatal("delivered activation did not complete rekey")
				}
			} else {
				if l.getTerminalConnection(old.viewer, old.id) == nil {
					t.Fatal("lost activation destroyed old connection")
				}
				if l.getTerminalConnection(c.viewer, c.id) != nil {
					t.Fatal("rejected activation kept new connection")
				}
			}
		})
	}
}
