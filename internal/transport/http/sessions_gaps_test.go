package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/icon"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// oneBlindWindow is this Mac on 2026-09-20: iTerm2 listed eleven sessions and
// would not describe a twelfth window, tmux listed every pane it has, and the
// process table read cleanly.
func oneBlindWindow() session.Inventory {
	return session.Inventory{
		Provenance: "merged",
		ObservedAt: time.Now(),
		Complete:   false,
		Sources:    map[string]bool{"ps": true, "tmux": true, "iterm": false},
		Gaps: []session.Gap{{Source: "iterm", Scope: "window", ID: "27898",
			Detail: "iTerm2 window 27898 would not list its tabs (tabs() answered null)"}},
		Sessions: []session.Session{
			{ID: "%8", TTY: "ttys008", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
				State: session.StateIdle, PID: 4242, ConversationID: "conv-tmux"},
			{ID: "7A5C0000-0000-4000-8000-000000000009", TTY: "ttys015", Backend: session.BackendITerm,
				Assistant: session.AssistantClaude, State: session.StateIdle, PID: 4243, ConversationID: "conv-iterm"},
		},
	}
}

func gapServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{store: st, broker: &orchestrator.Broker{Store: st, Dir: dir},
		icons: &icon.Registry{}, inventory: app.Inventory{}}
}

// One window iTerm2 will not describe must not put every row on the machine
// beyond anybody's reach.
//
// What the person saw before this: thirteen rows, every one `unknown` with
// `session_inventory_stale`, ten of them tmux panes that iTerm2 has never
// listed. The reason it was drawn on all of them is that closeability was
// handed the AND over every source; it is now handed the completeness of the
// source that lists the row (D05 ③).
func TestAWindowITermCannotReadDoesNotMakeEveryRowUnknown(t *testing.T) {
	s := gapServer(t)
	snap := s.sessionsPayloadFrom(context.Background(), oneBlindWindow())
	if len(snap.Sessions) != 2 {
		t.Fatalf("the payload carried %d rows", len(snap.Sessions))
	}
	stale := func(id string) bool {
		for _, row := range snap.Sessions {
			if row.ID != id {
				continue
			}
			for _, reason := range row.Closeability.Reasons {
				if reason.Code == "session_inventory_stale" {
					return true
				}
			}
		}
		return false
	}
	if stale("%8") {
		t.Error("a tmux pane tmux listed completely reads as a stale inventory because of an iTerm2 window")
	}
	if !stale("7A5C0000-0000-4000-8000-000000000009") {
		t.Error("an iTerm2 row does not say its own source could not be read")
	}
}

// And the window is on the wire, so the person can see what is in the way
// instead of asking iTerm2 by hand.
func TestTheSnapshotSaysWhichWindowWouldNotOpen(t *testing.T) {
	s := gapServer(t)
	snap := s.sessionsPayloadFrom(context.Background(), oneBlindWindow())
	var seen bool
	for _, src := range snap.Scan.Sources {
		if src.Source != "iterm" {
			if len(src.Gaps) != 0 {
				t.Errorf("%s carries another source's gaps: %+v", src.Source, src.Gaps)
			}
			continue
		}
		seen = true
		if len(src.Gaps) != 1 || src.Gaps[0].ID != "27898" || src.Gaps[0].Sealed {
			t.Fatalf("iterm's gaps: %+v", src.Gaps)
		}
		if !strings.Contains(src.Gaps[0].Detail, "tabs()") {
			t.Errorf("the gap does not say what was asked: %q", src.Gaps[0].Detail)
		}
	}
	if !seen {
		t.Fatal("no iterm source on the snapshot")
	}
}
