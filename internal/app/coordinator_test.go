package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The machine role on this daemon's own store (W5, cutover A7). Each test has
// the control that makes its check red (DG-8).

const (
	convA = "11111111-1111-4111-8111-111111111111"
	convB = "22222222-2222-4222-8222-222222222222"
)

type machine struct {
	rows     []session.Session
	complete bool
	sources  map[string]bool
	at       time.Time
}

func (m *machine) read(context.Context) session.Inventory {
	return session.Inventory{Sessions: m.rows, Complete: m.complete, Sources: m.sources, ObservedAt: m.at}
}

func newRole(t *testing.T) (*Coordinator, *machine, *time.Time) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	m := &machine{complete: true, at: now, sources: map[string]bool{"tmux": true, "ps": true}}
	c := &Coordinator{
		Store: st, Read: m.read,
		ProcessStart: func(pid int) time.Time { return time.Unix(int64(1_800_000_000+pid), 0) },
		Clock:        func() time.Time { return now },
	}
	return c, m, &now
}

func row(terminal, conversation string, pid int) session.Session {
	return session.Session{ID: terminal, Backend: session.BackendTmux, TTY: "ttys00" + terminal[1:], PID: pid,
		Assistant: session.AssistantCodex, ConversationID: conversation, Label: "clawdfather " + terminal}
}

func code(err error) string {
	var r RoleRefusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func TestTheRoleIsRegisteredOnceAndNeverTakenOver(t *testing.T) {
	c, m, _ := newRole(t)
	ctx := context.Background()
	m.rows = []session.Session{row("%1", convA, 101), row("%2", convB, 102)}

	if _, _, err := c.Register(ctx, "%1"); code(err) != "session_id_is_terminal" {
		t.Fatalf("a terminal id as the session: %v", err)
	}
	m.complete, m.sources["tmux"] = false, false
	if _, _, err := c.Register(ctx, convA); code(err) != "coordinator_liveness_unknown" {
		t.Fatalf("registering from an incomplete reading: %v", err)
	}
	m.complete, m.sources["tmux"] = true, true
	st, created, err := c.Register(ctx, convA)
	if err != nil || !created || st.Record.ConversationID != convA || st.Record.TerminalID != "%1" || st.Record.Generation != 1 {
		t.Fatalf("register: %v %v %+v", err, created, st.Record)
	}
	if _, created, err := c.Register(ctx, convA); err != nil || created {
		t.Fatalf("the same session again: %v created=%v", err, created)
	}
	if _, _, err := c.Register(ctx, convB); code(err) != "coordinator_exists" {
		t.Fatalf("a second session registering: %v", err)
	}
	// The record is in the broker's store, and only one row of it.
	rec, status, err := c.Store.Coordinator(ctx)
	if err != nil || status != store.CoordinatorReady || rec.ID != st.Record.ID {
		t.Fatalf("stored: %v %v %+v", err, status, rec)
	}
}

func TestTheRoleMovesOnlyWhenTheBoundSessionIsProvablyGone(t *testing.T) {
	c, m, now := newRole(t)
	ctx := context.Background()
	m.rows = []session.Session{row("%1", convA, 101), row("%2", convB, 102)}
	st, _, err := c.Register(ctx, convA)
	if err != nil {
		t.Fatal(err)
	}
	id := st.Record.ID
	*now = now.Add(time.Minute)
	m.at = *now

	if _, _, err := c.Rebind(ctx, id, 1, convB); code(err) != "coordinator_online" {
		t.Fatalf("rebinding over a live holder: %v", err)
	}
	m.rows = []session.Session{row("%2", convB, 102)}
	// The pane's own source could not answer: unknown, not gone — even with
	// every other source complete (D05 ③).
	m.sources["tmux"] = false
	if _, _, err := c.Rebind(ctx, id, 1, convB); code(err) != "coordinator_liveness_unknown" {
		t.Fatalf("rebinding on an unreadable tmux: %v", err)
	}
	// And the other way: iTerm2 cannot be asked, as on this Mac, but tmux —
	// the source the bound pane belongs to — answered in full.
	m.sources["tmux"], m.sources["iterm"], m.complete = true, false, false
	if _, _, err := c.Rebind(ctx, id, 2, convB); code(err) != "coordinator_generation_mismatch" {
		t.Fatalf("a stale generation: %v", err)
	}
	if _, _, err := c.Rebind(ctx, "not-the-role", 1, convB); code(err) != "coordinator_identity_mismatch" {
		t.Fatalf("another role's id: %v", err)
	}
	next, moved, err := c.Rebind(ctx, id, 1, convB)
	if err != nil || !moved || next.Record.Generation != 2 || next.Record.ConversationID != convB ||
		next.Record.ID != id || len(next.Record.Aliases) != 1 || next.Record.Aliases[0].ConversationID != convA {
		t.Fatalf("rebind: %v %v %+v", err, moved, next.Record)
	}
	// A caller that read generation 1 cannot move it again.
	if _, _, err := c.Rebind(ctx, id, 1, convB); code(err) != "coordinator_generation_mismatch" {
		t.Fatalf("a second rebind from the old view: %v", err)
	}
	// The compare-and-set is the store's too, not only this code's: a write
	// against the generation that has passed is refused by the file.
	stale := *next.Record
	stale.Generation = 3
	if err := c.Store.CommitCoordinator(ctx, &store.CoordinatorExpect{ID: id, Generation: 1}, stale, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a stale write reached the row: %v", err)
	}
}

func TestTheCrownIsTheExactProcess(t *testing.T) {
	c, m, _ := newRole(t)
	ctx := context.Background()
	bound := row("%1", convA, 101)
	m.rows = []session.Session{bound}
	st, _, err := c.Register(ctx, convA)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Crowned(st.Record, bound) {
		t.Fatal("the bound session wears no crown")
	}
	// Control: the same terminal and conversation in a new process is not
	// the bound one.
	restarted := bound
	restarted.PID = 999
	if c.Crowned(st.Record, restarted) {
		t.Fatal("a restarted process wore the crown")
	}
	if c.Crowned(nil, bound) {
		t.Fatal("no role, and a crown")
	}
}

func TestAnOldRowIsNeitherReadAsARoleNorOverwritten(t *testing.T) {
	c, m, _ := newRole(t)
	ctx := context.Background()
	m.rows = []session.Session{row("%1", convA, 101)}
	// What the first version of /v1/next/coordinator wrote: an id made of two
	// names and no process start.
	legacy := coordinator.Record{ID: "%1:" + convA, Identity: coordinator.Identity{ConversationID: convA,
		TerminalID: "%1", Assistant: "codex", PID: 101}, RegisteredAt: time.Now(), Generation: 1}
	if err := c.Store.CommitCoordinator(ctx, nil, legacy, nil); err != nil {
		t.Fatal(err)
	}
	st, err := c.State(ctx)
	if err != nil || st.Status != store.CoordinatorUnsupported || st.Record != nil {
		t.Fatalf("an old row read as %v %+v %v", st.Status, st.Record, err)
	}
	if _, _, err := c.Register(ctx, convA); code(err) != "coordinator_store_invalid" {
		t.Fatalf("registering over an old row: %v", err)
	}
}
