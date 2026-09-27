package http

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// oneTmuxPane is a launcher whose every new tmux session is the one pane.
type oneTmuxPane struct{ pane string }

func (l oneTmuxPane) ITermRunning(context.Context) (bool, error) { return false, nil }
func (l oneTmuxPane) TmuxReach(context.Context) int              { return 2 }
func (l oneTmuxPane) NewITermTab(context.Context, string) (string, error) {
	return "", errors.New("no iTerm2")
}
func (l oneTmuxPane) NewTmuxWindow(context.Context, string, string) (string, error) {
	return l.pane, nil
}
func (l oneTmuxPane) NewTmuxSession(context.Context, string, string, string) (string, error) {
	return l.pane, nil
}
func (l oneTmuxPane) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return false, nil
}

func orchestratorScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../app/orchestrator/testdata/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// dialogServer is a Board item and a broker whose new Codex Session is the
// test's pane, opening on Codex's update menu.
func dialogServer(t *testing.T) (*Server, *pane, app.WorkV2View, *sync.Mutex, *string) {
	t.Helper()
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	var mu sync.Mutex
	shown := orchestratorScreen(t, "codex-update")
	dir := s.broker.Dir
	s.broker = &orchestrator.Broker{Store: s.store, Tasks: taskdir.New(dir), Dir: dir,
		Launcher: oneTmuxPane{pane: p.s.ID},
		Live:     func(context.Context) []session.Session { return []session.Session{p.s} },
		Screen: func(_ context.Context, id string) (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			return shown, id == p.s.ID
		},
		Type: func(_ context.Context, id, text string) error {
			p.act("type:" + id + ":" + text)
			return nil
		},
	}
	s.broker.RootAssignmentSettled = func(ctx context.Context, _ orchestrator.RootAssignment) {
		s.settleAwaitedAssignments(ctx)
	}
	return s, p, v, &mu, &shown
}

// A Board item's new Codex Session that opens on a question only the person
// may answer is not a failed assignment: the item waits, says what it waits
// on, refuses a second assignment meanwhile, and is owned by that Session once
// the person answers.
func TestABoardItemWaitsForTheDialogItsNewSessionOpenedOn(t *testing.T) {
	s, p, v, mu, shown := dialogServer(t)
	ctx := context.Background()

	waiting, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "new_session", "", "codex", "", nil)
	if err != nil {
		t.Fatalf("an assignment whose Session is at a dialog was refused: %v", err)
	}
	if waiting.Item.Phase != work.PhaseAssigning || waiting.Item.OwnerSession != "" ||
		waiting.Item.Condition != work.ConditionWaitingUser || waiting.Item.UserAction != app.AssignmentDialogAction {
		t.Fatalf("the waiting item = %+v", waiting.Item)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("something was typed at the dialog: %v", got)
	}

	_, err = s.assignWorkV2(ctx, v.Item.ID, "local", waiting.Item.Version, "new_session", "", "claude", "", nil)
	var refusal *app.WorkError
	if !errors.As(err, &refusal) || refusal.Code != "assignment_awaiting_dialog" {
		t.Fatalf("a second assignment while the first waits: %v", err)
	}

	mu.Lock()
	*shown = orchestratorScreen(t, "codex-composer")
	mu.Unlock()
	s.broker.Pass(ctx)

	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Item.OwnerSession != p.s.ConversationID || after.Item.Phase != work.PhaseAssigned ||
		after.Item.Condition != "" || after.Item.UserAction != "" {
		t.Fatalf("after the person answered, the item = %+v", after.Item)
	}
	typed := p.done()
	if len(typed) != 1 || !strings.HasPrefix(typed[0], "type:"+p.s.ID+":") || !strings.Contains(typed[0], "ASSIGNMENT.md") {
		t.Fatalf("typed %v, want the assignment line once", typed)
	}
}

// Closing the tab instead fails the assignment, and takes back the question
// the item was asking.
func TestABoardItemWhoseDialogTabIsClosedIsAFailedAssignment(t *testing.T) {
	s, p, v, _, _ := dialogServer(t)
	ctx := context.Background()
	if _, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "new_session", "", "codex", "", nil); err != nil {
		t.Fatal(err)
	}
	s.broker.Reading = func(context.Context) session.Inventory {
		other := session.Session{ID: "%9", Backend: session.BackendTmux, Assistant: session.AssistantClaude}
		return session.Inventory{Sessions: []session.Session{other}, Complete: true, Sources: map[string]bool{"tmux": true}}
	}
	s.broker.Pass(ctx)

	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Item.Phase != work.PhaseCreated || after.Item.Condition != work.ConditionAssignmentFailed ||
		after.Item.UserAction != "" || after.Item.OwnerSession != "" {
		t.Fatalf("after the tab closed, the item = %+v", after.Item)
	}
	if len(after.Assignments) != 1 || after.Assignments[0].State != "failed" ||
		!strings.Contains(after.Assignments[0].Failure, "tab was closed") {
		t.Fatalf("assignments = %+v", after.Assignments)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("typed %v", got)
	}
}
