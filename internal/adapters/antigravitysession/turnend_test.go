package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func emitUpdate(t *testing.T, c *conversation, row session.Session, update map[string]any) {
	t.Helper()
	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": row.ConversationID, "update": update}})
	if err := c.update(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
}

func say(text string) map[string]any {
	return map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}
}

// The runtime wrote whole replies and never answered session/prompt, so the
// conversation read as working (2026-11-07). A final message ending with the
// marker now ends the turn after a short silence: no error, the marker hidden,
// and the person told the turn finished.
func TestAMarkedFinalMessageEndsTheTurnTheRuntimeNeverAnswers(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "durable"}[durable], func(t *testing.T) { markedEnd(t, durable) })
	}
}

func markedEnd(t *testing.T, durable bool) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	var h *Host
	var row session.Session
	var dir string
	if durable {
		var opts Options
		h, row, opts = persistentFixture(t)
		dir = opts.AccountDir
	} else {
		h, row, dir = fixture(t)
	}
	h.opts.TurnEndGrace = 50 * time.Millisecond
	finished := make(chan struct{}, 1)
	h.opts.TurnComplete = func(context.Context, session.Session) { finished <- struct{}{} }
	if err := h.Send(context.Background(), row, "finish the work"); err != nil {
		t.Fatal(err)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.row.State == session.StateWorking && c.writeGate == nil
	})
	// The marker split across chunks, as streaming may deliver it.
	emitUpdate(t, c, row, say("All findings are fixed.\n[[clawdline:"))
	emitUpdate(t, c, row, say("turn-end]]\n"))
	idleHost(t, h, row)
	raw, err := os.ReadFile(filepath.Join(dir, "cancelled"))
	if err != nil || string(raw) != row.ConversationID {
		t.Fatalf("the marked turn was not ended: %q, %v", raw, err)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	reply := ""
	for _, e := range page.Entries {
		if e.Role == "error" {
			t.Fatalf("a marked end left an error: %+v", page.Entries)
		}
		if e.Role == "assistant" {
			reply = e.Text
		}
	}
	if reply != "All findings are fixed." {
		t.Fatalf("assistant reply = %q, want the marker removed", reply)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("a marked end did not report the turn complete")
	}
}

// A model that writes the marker and then calls a tool is still working.
func TestAMarkFollowedByAToolKeepsTheTurnRunning(t *testing.T) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	h, row, _ := fixture(t)
	h.opts.TurnEndGrace = 50 * time.Millisecond
	if err := h.Send(context.Background(), row, "work on"); err != nil {
		t.Fatal(err)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.row.State == session.StateWorking && c.writeGate == nil
	})
	emitUpdate(t, c, row, say("Done for now.\n"+TurnEndMarker))
	emitUpdate(t, c, row, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "late", "title": "One more check", "kind": "read", "status": "completed"})
	time.Sleep(1500 * time.Millisecond) // past the grace and a watchdog tick
	if inv, err := h.Inventory(context.Background()); err != nil || inv.Sessions[0].State != session.StateWorking {
		t.Fatalf("a mark followed by a tool ended the turn: %+v, %v", inv, err)
	}
}

func TestTheTurnEndMarkerIsRemovedFromTheReply(t *testing.T) {
	for _, tc := range []struct{ in, out string }{
		{"Done.\n" + TurnEndMarker, "Done."},
		{"Done.\n" + TurnEndMarker + "\n", "Done."},
		{"Done. " + TurnEndMarker + "\nP.S.", "Done.\nP.S."},
		{"No marker here.", "No marker here."},
	} {
		got, found := stripTurnEnd(tc.in)
		if got != tc.out || found != (strings.Contains(tc.in, TurnEndMarker) && !strings.HasSuffix(tc.out, "P.S.")) {
			t.Errorf("stripTurnEnd(%q) = %q, %v", tc.in, got, found)
		}
	}
}
