package http

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// twoPanes is one tmux host with two Sessions in it, recording which terminal
// each typed brief went to.
type twoPanes struct {
	*pane
	other session.Session
}

func (p *twoPanes) Inventory(ctx context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Sessions: []session.Session{p.s, p.other}}, nil
}

func (p *twoPanes) Send(ctx context.Context, s session.Session, text string) error {
	p.act("send:" + s.ID + ":" + text)
	return nil
}

func reassignServer(t *testing.T) (*Server, *twoPanes, app.WorkV2View) {
	t.Helper()
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	two := &twoPanes{pane: p, other: session.Session{ID: "%5", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		CWD: p.s.CWD, ConversationID: "10000000-0000-4000-8000-000000000005", State: session.StateIdle}}
	hosts := []ports.TerminalHost{two}
	dir := s.broker.Dir
	s.terminals, s.inventory = hosts, app.Inventory{Terminals: hosts, Screen: two}
	s.broker = &orchestrator.Broker{Store: s.store, Tasks: taskdir.New(dir), Dir: dir}
	return s, two, v
}

// The person moves an item a Session is already working on to another
// Session: the new one owns it in the same phase, with its steps kept, and is
// told it is taking over; the old one is told to stop and can no longer move
// the item.
func TestThePersonMovesAnAssignedItemToAnotherSession(t *testing.T) {
	s, p, v := reassignServer(t)
	ctx := context.Background()
	first, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	working, err := s.workV2().Advance(ctx, v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: first.Item.Version,
		SessionID: p.s.ConversationID, Next: work.PhaseImplementing, Actor: p.s.ConversationID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.acts = nil

	moved, err := s.assignWorkV2(ctx, v.Item.ID, "local", working.Item.Version, "existing_session", p.other.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Item.OwnerSession != p.other.ConversationID || moved.Item.Phase != work.PhaseImplementing {
		t.Fatalf("reassigned item = %+v", moved.Item)
	}
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	active := []string{}
	for _, a := range after.Assignments {
		if a.State == "active" {
			active = append(active, a.SessionID)
		}
	}
	if len(active) != 1 || active[0] != p.other.ConversationID {
		t.Fatalf("active assignments = %v, want only the new Session", active)
	}

	sent := p.done()
	var toOld, toNew string
	for _, line := range sent {
		switch {
		case strings.HasPrefix(line, "send:"+p.s.ID+":"):
			toOld = line
		case strings.HasPrefix(line, "send:"+p.other.ID+":"):
			toNew = line
		}
	}
	if !strings.Contains(toNew, "reassigned Board item "+v.Item.ID) || !strings.Contains(toNew, "phase implementing") ||
		!strings.Contains(toNew, "before starting over") {
		t.Fatalf("new owner brief = %q (all: %v)", toNew, sent)
	}
	if !strings.Contains(toOld, "moved Board item "+v.Item.ID) || !strings.Contains(toOld, "no longer yours") {
		t.Fatalf("previous owner notice = %q (all: %v)", toOld, sent)
	}

	_, err = s.workV2().Advance(ctx, v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: moved.Item.Version,
		SessionID: p.s.ConversationID, Next: work.PhaseVerifying, Actor: p.s.ConversationID}, nil)
	var refusal *app.WorkError
	if !errors.As(err, &refusal) || refusal.Code != "not_item_owner" {
		t.Fatalf("the previous owner moved the item on: %v", err)
	}
}

// Choosing the Session that already owns the item is refused by name and
// changes nothing: reminding it is a separate action.
func TestReassigningAnItemToItsOwnerIsRefused(t *testing.T) {
	s, p, v := reassignServer(t)
	ctx := context.Background()
	first, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.acts = nil
	_, err = s.assignWorkV2(ctx, v.Item.ID, "local", first.Item.Version, "existing_session", p.s.ID, "", "", nil)
	var refusal *app.WorkError
	if !errors.As(err, &refusal) || refusal.Code != "assignment_unchanged" {
		t.Fatalf("same-owner assignment: %v", err)
	}
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil || after.Item.Version != first.Item.Version || len(after.Assignments) != 1 {
		t.Fatalf("a refused reassignment wrote %+v %v", after, err)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("a refused reassignment typed %v", got)
	}
}
