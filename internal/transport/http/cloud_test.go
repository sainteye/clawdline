package http

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/terminal"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func syntheticViewerPin(t *testing.T) *adaptercloud.PinnedStore {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pins := adaptercloud.NewPinnedStore(filepath.Join(t.TempDir(), "pins"))
	if err := pins.Pin("synthetic-account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub)}); err != nil {
		t.Fatal(err)
	}
	return pins
}

func TestCloudViewerGrantRequiresTheMachineTokenAndRejectsRouterLocalBearer(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	pins := syntheticViewerPin(t)
	pair := &cloudtransport.Pairing{Pinned: pins}
	SetCloudLine(f.dir, pairingOnlyLine{pairing: pair})
	t.Cleanup(func() { SetCloudLine(f.dir, nil) })
	_, remote := f.device("synthetic operator", true)
	path := "/v1/cloud/devices/grant"
	for _, token := range []string{"", remote} {
		got := f.do(http.MethodPost, path, token, `{"device":"viewer","scope":"operate"}`)
		if got.Code != 401 && got.Code != 403 {
			t.Fatalf("remote grant: %d %s", got.Code, got.Body)
		}
	}
	for _, scope := range []string{adaptercloud.ScopeReadOnly, adaptercloud.ScopeOperate} {
		got := f.do(http.MethodPost, path, f.local, `{"device":"viewer","scope":"`+scope+`"}`)
		if got.Code != 200 {
			t.Fatalf("local grant: %d %s", got.Code, got.Body)
		}
		if actual, err := pins.ViewerScope("viewer"); err != nil || actual != scope {
			t.Fatalf("grant: %q %v", actual, err)
		}
	}
	// The Cloud router holds the local bearer but is still a remote viewer.
	router := cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, "")}
	for _, path := range []string{path, "/v1/cloud/pairing", "/v1/cloud/pairing/offer", "/v1/cloud/devices/revoke"} {
		res, err := router.Do(context.Background(), cloudops.LocalRequest{Method: "POST", Path: path, Viewer: "viewer", Body: []byte(`{"device":"viewer","scope":"operate","offer":"synthetic"}`)})
		if err != nil || res.Status != 403 {
			t.Fatalf("Cloud management %s: %d %s %v", path, res.Status, res.Body, err)
		}
	}
	if got := f.do(http.MethodPost, path, f.local, `{"device":"missing","scope":"operate"}`); got.Code != 404 {
		t.Fatal(got.Code, got.Body)
	}
	if got := f.do(http.MethodPost, path, f.local, `{"device":"viewer","scope":"admin"}`); got.Code != 400 {
		t.Fatal(got.Code, got.Body)
	}
	pair.OnScopeChange = func(string) error { return errors.New("synthetic sweep failed") }
	if got := f.do(http.MethodPost, path, f.local, `{"device":"viewer","scope":"readonly"}`); got.Code != 503 || !strings.Contains(got.Body.String(), "viewer_scope_applied_cleanup_failed") {
		t.Fatalf("partial grant: %d %s", got.Code, got.Body)
	}
	if scope, err := pins.ViewerScope("viewer"); err != nil || scope != adaptercloud.ScopeReadOnly {
		t.Fatalf("partial grant elevated: %q %v", scope, err)
	}

}

func TestCloudQueuedSendRechecksOriginalViewerAfterTheLaneWait(t *testing.T) {
	p := waitingPane("%4", "")
	s, h, _, _ := wholeServer(t, touchPane{p}, touchPane{p})
	local, machine, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	pins := syntheticViewerPin(t)
	b := cloudops.Bridge{MachineID: "machine", AllowCommands: func() bool { return true }, Router: cloudtransport.Router{Handler: h, Authorize: cloudtransport.LocalAuthorizer(local, machine)}}
	b.Authority = func(_ context.Context, sender string, _ ed25519.PublicKey, write bool) cloudops.Authority {
		scope, err := pins.ViewerScope(sender)
		return cloudops.Authority{ClockReady: true, RosterReadable: err == nil, RosterAllowsSender: err == nil, WriteGateAllows: true, ViewerReadOnly: write && scope == adaptercloud.ScopeReadOnly}
	}
	lanes := app.TerminalLanes()
	release, err := lanes.Acquire(context.Background(), lane.TerminalKey(string(session.BackendTmux), "%4"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	admitted := lanes.Stats().Admitted
	done := make(chan cloudops.Answer, 1)
	go func() {
		done <- b.Handle(context.Background(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "viewer", Sequence: 1, Plaintext: []byte(`{"type":"send","session":"%4","request":"queued-downgrade","text":"never typed","images":[]}`)})
	}()
	deadline := time.Now().Add(time.Second)
	for lanes.Stats().Admitted <= admitted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if lanes.Stats().Admitted <= admitted {
		t.Fatal("send did not queue")
	}
	if err := pins.SetScope("viewer", adaptercloud.ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case a := <-done:
		if a.Status != 403 || a.Code != "viewer_read_only" {
			t.Fatalf("queued downgrade: %d %s %s", a.Status, a.Code, a.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("queued send did not finish")
	}
	for _, effect := range p.done() {
		if effect == "send:never typed" {
			t.Fatal("downgraded send reached the host")
		}
	}
}

type scopeTerminalLine struct {
	pairingOnlyLine
	pins *adaptercloud.PinnedStore
}

func (l scopeTerminalLine) PinnedTerminalViewer(string) (string, bool) { return "synthetic", true }
func (l scopeTerminalLine) TerminalViewerAccess(string) error          { return nil }
func (l scopeTerminalLine) TerminalViewerOperate(device string) error {
	scope, err := l.pins.ViewerScope(device)
	if err != nil || scope != adaptercloud.ScopeOperate {
		return terminal.Refuse(terminal.RefusalCode("viewer_read_only"), "This device may read, and not send.")
	}
	return nil
}

func TestCloudDowngradeVoidsTerminalLeaseAndPreservesReads(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	id := terminal.ID(f.open(f.local).ID)
	pins := syntheticViewerPin(t)
	pair := &cloudtransport.Pairing{Pinned: pins}
	SetCloudLine(f.dir, scopeTerminalLine{pairingOnlyLine: pairingOnlyLine{pair}, pins: pins})
	t.Cleanup(func() { SetCloudLine(f.dir, nil) })
	svc, err := f.s.CloudTerminalService()
	if err != nil {
		t.Fatal(err)
	}
	pair.OnScopeChange = func(string) error { svc.Revalidate(); return nil }
	p := terminals.Principal{Cloud: true, Device: "viewer"}
	lease, err := svc.SetControl(context.Background(), p, id, "tab", terminals.Acquire)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := svc.Watch(context.Background(), p, id, "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	if err := pair.SetViewerScope("viewer", adaptercloud.ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	if svc.Control(id).Held {
		t.Fatal("downgrade retained lease")
	}
	if _, err := svc.List(context.Background(), p, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Find(context.Background(), p, id); err != nil {
		t.Fatal(err)
	}
	for _, effect := range []func() error{
		func() error {
			_, _, err := svc.Input(context.Background(), p, id, "tab", lease.Epoch, 1, []byte("forbidden"))
			return err
		},
		func() error { return svc.Resize(context.Background(), p, id, "tab", lease.Epoch, 80, 24) },
		func() error { return svc.Close(context.Background(), p, id, "tab", lease.Epoch) },
		func() error {
			_, err := svc.SetControl(context.Background(), p, id, "tab", terminals.Acquire)
			return err
		},
	} {
		if code, _ := terminal.CodeOf(effect()); code != "viewer_read_only" {
			t.Fatalf("effect: %s", code)
		}
	}
	// The stream's next event must remain readable after the lease notification.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stop := errors.New("frame observed")
	frames := 0
	err = watch.Run(ctx, func(event terminals.Event) error {
		if event.Kind == terminals.EventRefusal {
			encoded, _ := json.Marshal(event)
			t.Fatalf("downgrade closed read stream: %s", encoded)
		}
		if event.Kind == terminals.EventFrame {
			frames++
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || frames != 1 {
		t.Fatalf("readonly stream: frames=%d err=%v", frames, err)
	}
}

func TestReadonlyCloudPushRegistrationKeepsItsReadLevelContract(t *testing.T) {
	s := newPushStandIn(t)
	s.bridge.Authority = func(_ context.Context, _ string, _ ed25519.PublicKey, write bool) cloudops.Authority {
		return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
			WriteGateAllows: !write, ViewerReadOnly: write}
	}
	a, payload := s.ask(t, 1, map[string]any{"type": "push-subscribe", "session": cloudops.MachineReplySession,
		"request": "synthetic-readonly-push", "subscription": cloudSubscription(t, "https://fcm.googleapis.com/fcm/send/synthetic-readonly")})
	if !a.OK() {
		t.Fatalf("readonly subscribe: %d %s %s", a.Status, a.Code, a.Payload)
	}
	id, _ := bodyOf(t, payload)["id"].(string)
	if id == "" {
		t.Fatal("readonly subscription has no id")
	}
}
