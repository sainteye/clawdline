package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// rekeyedPair is the fixture's connection as the old one, and a new one
// rekeyed from it that has published its first frame.
func rekeyedPair(t *testing.T) (*Link, *terminals.Service, *terminalConnection, *terminalConnection) {
	t.Helper()
	l, _, old, svc, _ := terminalLifecycleFixture(t)
	c := &terminalConnection{viewer: old.viewer, id: "AQEBAQEBAQEBAQEBAQEBAQ", keyID: old.keyID, key: old.key,
		expires: time.Now().Add(time.Minute), terminalID: old.terminalID, client: old.client,
		receipts: map[string][]byte{}, rekeyPending: true, publishedFrameSeq: 1}
	l.terminalMu.Lock()
	l.terminalConnections[terminalConnectionID(c.viewer, c.id)] = c
	l.terminalMu.Unlock()
	return l, svc, old, c
}

func activation(old *terminalConnection, c *terminalConnection, seq uint64) terminalRequest {
	return terminalRequest{V: 1, Type: "terminal_request", RequestID: testRequestID, Connection: c.id,
		Operation: "activate_connection", Client: c.client,
		Body: json.RawMessage(fmt.Sprintf(`{"old_connection":%q,"first_frame_seq":%d}`, old.id, seq))}
}

// 2026-10-06 22:50: a background tab's timers slept past its 30-second lease,
// then its direct upgrade rekeyed the connection. Activation was refused
// terminal_invalid because no lease was held, the tab asked again on every
// frame, and the screen sat on "input may already have been applied" for
// good. Activation moves frames and receipts to the viewer's new key; it
// types nothing, so a lapsed lease is no reason to refuse it.
func TestRekeyActivatesAfterTheLeaseLapsedWhileIdle(t *testing.T) {
	l, svc, old, c := rekeyedPair(t)
	if control := svc.Control(c.terminalID); control.Held {
		t.Fatal("the fixture holds a lease; this test is about none being held")
	}
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	result, err := l.terminalOperation(context.Background(), svc, p, c, activation(old, c, 1))
	if err != nil {
		t.Fatalf("activation after a lapsed lease was refused: %v", err)
	}
	if got := result.(map[string]any); got["connection"] != c.id || got["retired_connection"] != old.id {
		t.Fatalf("activation result %v", got)
	}
}

// What activation still checks: the same viewer's rekey of the same terminal
// and client, with its first frame published. Each refusal says which in its
// stage row, since all share terminal_invalid on the wire.
func TestRekeyActivationNamesTheCheckItFailed(t *testing.T) {
	cases := map[string]func(old, c *terminalConnection) uint64{
		"activation_frame_unpublished": func(old, c *terminalConnection) uint64 { return 2 },
		"activation_not_rekeying":      func(old, c *terminalConnection) uint64 { c.rekeyPending = false; return 1 },
		"activation_other_client":      func(old, c *terminalConnection) uint64 { old.client = "another-tab"; return 1 },
		"activation_other_terminal":    func(old, c *terminalConnection) uint64 { old.terminalID = terminal.NewID(); return 1 },
	}
	for want, spoil := range cases {
		t.Run(want, func(t *testing.T) {
			l, svc, old, c := rekeyedPair(t)
			seq := spoil(old, c)
			_, err := l.terminalOperation(context.Background(), svc, terminals.Principal{Device: c.viewer, Cloud: true}, c, activation(old, c, seq))
			if code, _ := terminal.CodeOf(err); code != terminal.CodeInvalid {
				t.Fatalf("refusal code %q, want terminal_invalid (err %v)", code, err)
			}
			if got := stageReasonOf(err); got != want {
				t.Fatalf("stage reason %q, want %q", got, want)
			}
		})
	}
	l, svc, old, c := rekeyedPair(t)
	l.terminalMu.Lock()
	delete(l.terminalConnections, terminalConnectionID(old.viewer, old.id))
	l.terminalMu.Unlock()
	_, err := l.terminalOperation(context.Background(), svc, terminals.Principal{Device: c.viewer, Cloud: true}, c, activation(old, c, 1))
	if got := stageReasonOf(err); got != "activation_old_gone" {
		t.Fatalf("stage reason %q, want activation_old_gone", got)
	}
}

// The renewal that found the lease gone answered terminal_unreachable, so the
// tab read a lapsed lease as a machine that could not be reached. The
// service's own refusals keep their codes over Cloud.
func TestCloudControlRefusalKeepsTheLeaseCode(t *testing.T) {
	l, svc, _, c := rekeyedPair(t)
	req := terminalRequest{V: 1, Type: "terminal_request", RequestID: testRequestID, Connection: c.id,
		Operation: "control", TerminalID: string(c.terminalID), Client: c.client, Body: json.RawMessage(`{"action":"renew"}`)}
	_, err := l.terminalOperation(context.Background(), svc, terminals.Principal{Device: c.viewer, Cloud: true}, c, req)
	if code, known := terminal.CodeOf(err); !known || code != terminal.CodeNotController {
		t.Fatalf("a renewal without a lease is %q (known=%v), want not_controller", code, known)
	}
}

func TestRefusedStageRowCarriesItsReason(t *testing.T) {
	row := withRefusalCode("cloud terminal stage n=3 stage=receipt_published op=activate_connection outcome=refused",
		terminalReceipt{Status: "refused", Error: "terminal_invalid", reason: "activation_other_client"})
	if !strings.HasSuffix(row, " code=terminal_invalid reason=activation_other_client") {
		t.Fatalf("stage row %q", row)
	}
}
