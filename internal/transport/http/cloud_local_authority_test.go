package http

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// What a Cloud viewer is when it reaches this machine's routes, measured at
// the routes rather than reasoned about from the headers.
//
// Every in-process Cloud request carries this machine's own local token
// (`cloud.LocalAuthorizer`). A word sent with `X-Clawdline-Actor: device` is
// that token minus `Local` and minus the machine door; a word sent without it
// is this machine. These tests pin both halves where they matter: the words
// that deliberately keep this machine's exemption, and the one capability the
// device header deliberately leaves in place.

// localAuthorityStandIn is this daemon's own handler over a fresh state
// directory and the two credentials the comparison needs: this machine's own
// token, which every Cloud request arrives holding, and a device paired on
// this machine's own network that may read and send — every paired device's
// capabilities since 2026-10-08.
type localAuthorityStandIn struct {
	server  *Server
	handler http.Handler
	local   string
	machine string
	lan     string
}

func newLocalAuthorityStandIn(t *testing.T) *localAuthorityStandIn {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}, store: st, icons: &icon.Registry{}}
	handler := s.Handler()
	local, machine, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	_, lan, err := s.gate().auth.AddDevice("lan-phone", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	return &localAuthorityStandIn{server: s, handler: handler, local: local, machine: machine, lan: lan}
}

// bridge answers a Cloud viewer through handler, stamped by authorize.
func (s *localAuthorityStandIn) bridge(handler http.Handler, authorize func(*http.Request)) cloudops.Bridge {
	return cloudops.Bridge{MachineID: "mac-01",
		Router:        cloud.Router{Handler: handler, Authorize: authorize},
		AllowCommands: func() bool { return true }}
}

// askContentRail is the same plaintext on the `r/` rail, where the Session
// content reads are answered.
func askContentRail(t *testing.T, b cloudops.Bridge, seq uint64, body map[string]any) cloudops.Answer {
	t.Helper()
	plaintext, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b.Handle(context.Background(), cloudops.Command{
		Channel: "r/mac-01", Class: cloudops.ClassCtl, Sender: "web_viewer-01",
		Sequence: seq, Plaintext: plaintext})
}

func askBridge(t *testing.T, b cloudops.Bridge, seq uint64, body map[string]any) cloudops.Answer {
	t.Helper()
	plaintext, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b.Handle(context.Background(), cloudops.Command{
		Channel: "ctl/mac-01", Class: cloudops.ClassCtl, Sender: "web_viewer-01",
		Sequence: seq, Plaintext: plaintext})
}

// TestEveryLocalWordThatNeedsThisMachineSaysSo is the guard on the one way a
// Cloud viewer wears this machine's own exemption.
//
// A word routed as Local reaches its route holding this machine's token with
// `Local` set, so a route that answers only this machine (`requireLocal`, the
// gate's Local-only writes) lets it through. That is right for exactly the
// words the table flags `local_exemption` — the assistant-run pairing offer
// and its status, plus the operation-gated same-network opt-in — and wrong
// for any other: a new Local word on such a route
// would hand every viewer on the account this machine's own hand, silently.
//
// Each Local word's own decoder builds its own request from the table's own
// example, and the route is asked as a device paired on this machine's own
// network would ask it — the same capabilities, the machine door still open,
// only `Local` missing. A 403 there is a route that tells this machine apart,
// and the word must carry the flag; a flagged word whose route does not is a
// flag that no longer says anything.
func TestEveryLocalWordThatNeedsThisMachineSaysSo(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	s := newLocalAuthorityStandIn(t)
	stamp := cloud.LocalAuthorizer(s.local, s.machine)
	// The status the route itself wrote, not the bridge's answer: the bridge
	// refuses some words on some rails before any route is asked, and that
	// refusal says nothing about the route.
	routed := 0
	// This stand-in wires no broker, tmux or Cloud link, so a route that got
	// past its checks may dereference what is not there, or wait for a
	// producer that never runs. Every Local-only check stands first in its
	// handler and answers at once, so a route that panicked or is still
	// waiting was not refused.
	reached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		done := make(chan int, 1)
		go func() {
			defer func() {
				if recover() != nil {
					done <- http.StatusInternalServerError
				}
			}()
			s.handler.ServeHTTP(rec, r)
			done <- rec.Code
		}()
		select {
		case routed = <-done:
		case <-time.After(2 * time.Second):
			routed = http.StatusGatewayTimeout
		}
		w.WriteHeader(routed)
		if routed != http.StatusGatewayTimeout {
			_, _ = w.Write(rec.Body.Bytes())
		}
	})
	asLANDevice := s.bridge(reached, func(r *http.Request) {
		stamp(r)
		r.Header.Set("Authorization", "Bearer "+s.lan)
	})
	// A Session content read is answered on the `r/` rail and refused on
	// `ctl/`; the rail checks the viewer's `read_transcript` before the route.
	asLANDevice.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) cloudops.Authority {
		return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
			WriteGateAllows: true, ReadTranscriptAllows: true}
	}

	checked, exempt := 0, 0
	var seq uint64
	for _, o := range ops {
		if o.Local == nil || o.Local.Device {
			continue
		}
		// Exercise the credential boundary with the optional feature enabled.
		// Its default-off refusal is tested separately by graphify_test.go.
		if o.Op == "project-graphify" || o.Op == "session-graphify" {
			if _, err := s.server.settingsFile().Set(map[string]any{"graphify_enabled": true}); err != nil {
				t.Fatal(err)
			}
		}
		reachedOnce := false
		for _, shape := range o.Shapes {
			example := o.Example
			if shape.Example != nil {
				example = shape.Example
			}
			body := map[string]any{"type": o.Op}
			for _, field := range shape.Body {
				body[field] = example[field]
			}
			routed = 0
			seq++
			answer := askBridge(t, asLANDevice, seq, body)
			if routed == 0 {
				body["machine_id"] = "mac-01"
				seq++
				answer = askContentRail(t, asLANDevice, seq, body)
			}
			if routed == 0 {
				// The bridge refused this shape itself (a menu answer that
				// names no question, say): it asks no route at all.
				continue
			}
			checked++
			reachedOnce = true
			onlyThisMachine := routed == http.StatusForbidden
			switch {
			case onlyThisMachine && !o.Local.LocalExemption:
				t.Errorf("%s %v runs as this machine on a route only this machine may use "+
					"(%d %s %s): send it as the device, or flag it `local_exemption` in "+
					"internal/domain/cloudvocab/vocabulary.json if every Cloud viewer is meant to "+
					"have this machine's own exemption there",
					o.Op, shape.Body, routed, answer.Code, answer.Payload)
			case !onlyThisMachine && o.Local.LocalExemption:
				t.Errorf("%s %v is flagged `local_exemption` and its route answers a paired "+
					"device %d: the flag no longer marks anything", o.Op, shape.Body, routed)
			case onlyThisMachine:
				exempt++
			}
		}
		if !reachedOnce {
			t.Errorf("%s reaches its route in none of its %d shapes on either rail, so "+
				"nothing here says whether that route is only this machine's", o.Op, len(o.Shapes))
		}
	}
	if checked == 0 || exempt == 0 {
		t.Fatalf("checked %d Local shapes, %d exempt: the guard is no longer looking at anything", checked, exempt)
	}
	t.Logf("checked %d Local shapes; %d deliberately need this machine", checked, exempt)
}

// TestACloudViewerManagesTheBoardOnPurpose pins a decision the person made on
// 2026-10-19: a Cloud viewer may manage the board — turn it on and off, and
// give or withdraw AI reading consent — while a device paired on this
// machine's own network may not.
//
// `board-command` is sent as the device, and the header takes away `Local`
// and the machine door and nothing else. The capabilities stay this machine's
// own, Admin among them, and `boardViewer` grants manage from Admin. So this
// is measured three ways: the verdict the route sees, the board actually
// changing, and the LAN device being refused the same change.
func TestACloudViewerManagesTheBoardOnPurpose(t *testing.T) {
	s := newLocalAuthorityStandIn(t)
	cloudViewer := cloud.LocalAuthorizer(s.local, s.machine)

	// The verdict, as the gate hands it to the route.
	var seen access
	var manage bool
	probe := s.server.gate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = accessOf(r)
		manage = boardViewer(r).CanManage
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	command := func(requestID string, revision int64) map[string]any {
		return map[string]any{"type": "board-command", "session": cloudops.MachineReplySession,
			"request": requestID, "command": map[string]any{
				"operation": "set_ai_consent", "provider": "codex", "enabled": true,
				"policy": "board-reading-v1", "expectedRevision": revision, "requestId": requestID}}
	}
	if answer := askBridge(t, s.bridge(probe, cloudViewer), 1, command("board-probe", 0)); !answer.OK() {
		t.Fatalf("the probe was not reached: %+v", answer)
	}
	v := seen.verdict
	if !v.Allowed || v.Local || seen.machine {
		t.Fatalf("a device-header Cloud request is judged allowed=%v local=%v machine=%v; "+
			"want allowed, not Local, not the machine", v.Allowed, v.Local, seen.machine)
	}
	if !v.Caps.Has(auth.Admin) || !v.Caps.Has(auth.Send) {
		t.Fatalf("a device-header Cloud request holds %v; the header takes away Local and the "+
			"machine door only, so Admin and Send stay", v.Caps)
	}
	if !manage {
		t.Fatal("a Cloud viewer may not manage the board; that is not the decision this test pins")
	}

	// The board, really changed through the real route.
	ctx := context.Background()
	before, err := s.server.boardSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer := askBridge(t, s.bridge(s.handler, cloudViewer), 2, command("board-consent", before.Revision))
	if !answer.OK() {
		t.Fatalf("a Cloud viewer could not give AI reading consent: %d %s %s",
			answer.Status, answer.Code, answer.Payload)
	}
	after, err := s.server.boardSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || after.NarrativeConsent == nil || *after.NarrativeConsent != "codex" {
		t.Fatalf("the board did not change: before %+v after %+v", before, after)
	}

	// And a LAN-paired device, which may send, still may not manage.
	rec := call{path: "/v1/board", headers: map[string]string{
		"Authorization": "Bearer " + s.lan, "Content-Type": "application/json",
		"Idempotency-Key": "board-lan"},
		body: `{"operation":"set_enabled","requestId":"board-lan","expectedRevision":` +
			jsonInt(after.Revision) + `,"enabled":false}`}.do(s.handler)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a LAN-paired device managed the board: %d %s", rec.Code, rec.Body)
	}
}
