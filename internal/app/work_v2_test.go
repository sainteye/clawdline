package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestDirectSessionLandingClosesAndRemainsInRecentHistory(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindIssue)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(next work.Phase, verification string, landing *VerifiedLandingV2, deployment string) {
		t.Helper()
		owned, err = w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
			SessionID: "session-a", Next: next, Verification: verification, Landing: landing,
			Deployment: deployment, Actor: "session-a"}, nil)
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	advance(work.PhaseImplementing, "", nil, "")
	advance(work.PhaseVerifying, "", nil, "")
	advance(work.PhaseMerging, "tests passed", nil, "")
	landing := &VerifiedLandingV2{Commit: strings.Repeat("a", 40), Target: "main",
		TargetCommit: strings.Repeat("b", 40), Remote: "origin", RemoteCommit: strings.Repeat("c", 40)}
	advance(work.PhaseDeploying, "", landing, "")
	advance(work.PhaseDone, "", nil, "production deployment receipt")
	if owned.Item.OwnerSession != "" || !owned.Item.Phase.Terminal() {
		t.Fatalf("completion did not release ownership: %+v", owned.Item)
	}
	recent, truncated, err := w.RecentlyCompleted(context.Background(), "session-a")
	if err != nil || truncated || len(recent) != 1 || recent[0].Item.ID != v.Item.ID {
		t.Fatalf("recent completion: %+v %v %v", recent, truncated, err)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "item.phase_changed" && strings.Contains(event.Payload, `"landing":{"commit":"`+landing.Commit) {
			found = true
		}
	}
	if !found {
		t.Fatalf("verified landing was not retained: %+v", full.Events)
	}
}

func TestTheCompletingAgentCanRetractAMistakenCompletion(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindIssue)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Assistant: "codex",
		Model: "default", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(next work.Phase, verification string, landing *VerifiedLandingV2, deployment string) {
		t.Helper()
		owned, err = w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
			SessionID: "session-a", Next: next, Verification: verification, Landing: landing,
			Deployment: deployment, Actor: "session-a"}, nil)
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	advance(work.PhaseImplementing, "", nil, "")
	advance(work.PhaseVerifying, "", nil, "")
	advance(work.PhaseMerging, "tests passed", nil, "")
	advance(work.PhaseDeploying, "", &VerifiedLandingV2{Commit: strings.Repeat("a", 40), Target: "main",
		TargetCommit: strings.Repeat("b", 40), Remote: "origin", RemoteCommit: strings.Repeat("c", 40)}, "")
	advance(work.PhaseDone, "", nil, "production deployment receipt")

	if _, err := w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a", Reason: " ",
	}, nil); err == nil || !strings.Contains(err.Error(), "reason_required") {
		t.Fatalf("reasonless completion correction = %v", err)
	}
	if _, err := w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a",
		Reason: strings.Repeat("x", workV2CompletionReasonLimit+1),
	}, nil); err == nil || !strings.Contains(err.Error(), "reason_too_large") {
		t.Fatalf("oversize completion correction = %v", err)
	}
	if _, err := w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-b", Reason: "The reported behavior still fails.",
	}, nil); err == nil || !strings.Contains(err.Error(), "not_completing_session") {
		t.Fatalf("another Session reopened the completion: %v", err)
	}
	reopened, err := w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a", Reason: "The reported behavior still fails.",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Item.Phase != work.PhaseImplementing || reopened.Item.OwnerSession != "session-a" ||
		reopened.Item.Cycle != 2 || !reopened.Item.ClosedAt.IsZero() {
		t.Fatalf("reopened item = %+v", reopened.Item)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, assignment := range full.Assignments {
		if assignment.State == "active" {
			active++
			if assignment.SessionID != "session-a" || assignment.TerminalID != "terminal-a" || assignment.Assistant != "codex" {
				t.Fatalf("replacement assignment = %+v", assignment)
			}
		}
	}
	if len(full.Assignments) != 2 || active != 1 {
		t.Fatalf("assignment history = %+v", full.Assignments)
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "item.completion_retracted" && strings.Contains(event.Payload, "reported behavior still fails") {
			found = true
		}
	}
	if !found {
		t.Fatalf("completion correction event = %+v", full.Events)
	}
}

func TestAnAgentCannotReverseThePersonsCancellation(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := w.Cancel(context.Background(), v.Item.ID, owned.Item.Version, "local", "The person stopped the work.", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: cancelled.Item.Version, SessionID: "session-a", Reason: "I want to continue.",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "item_not_done") {
		t.Fatalf("Agent reversed cancellation: %v", err)
	}
}

func newWorkV2Test(t *testing.T) *WorkSystemV2 {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := NewWorkSystemV2(st)
	w.Now = func() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }
	return w
}

func createWorkV2Test(t *testing.T, w *WorkSystemV2, kind work.Kind) WorkV2View {
	t.Helper()
	v, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p", Kind: kind,
		Title: "Owned work", Description: "A complete description", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWorkV2KeepsHumanAndAgentAuthoritiesSeparate(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	title := "Agent refined title"
	if _, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Title: &title, Actor: "session-b", OwnerSession: "session-b"}, nil); err == nil {
		t.Fatal("another Session edited the item")
	}
	edited, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Title: &title, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if err != nil || edited.Item.Title != title {
		t.Fatalf("owner edit: %+v %v", edited, err)
	}
	if _, err := w.Cancel(context.Background(), v.Item.ID, edited.Item.Version, "local", "no longer wanted", nil); err != nil {
		t.Fatal(err)
	}
}

func TestWaitingForThePersonNamesAndClearsTheRequiredAction(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiting := work.ConditionWaitingUser
	if _, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Condition: &waiting, Actor: "session-a", OwnerSession: "session-a"}, nil); err == nil {
		t.Fatal("waiting_user without a requested action was accepted")
	}
	action := "Approve the production rollout in the deployment console."
	edited, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Condition: &waiting, UserAction: &action, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if err != nil || edited.Item.Condition != waiting || edited.Item.UserAction != action {
		t.Fatalf("waiting action: %+v %v", edited.Item, err)
	}
	clear := work.Condition("")
	cleared, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: edited.Item.Version,
		Condition: &clear, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if err != nil || cleared.Item.Condition != "" || cleared.Item.UserAction != "" {
		t.Fatalf("cleared waiting action: %+v %v", cleared.Item, err)
	}
}

func TestARequestedUserActionIsBoundedAndOnlyBelongsToWaitingUser(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	action := "Choose an option"
	_, err = w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		UserAction: &action, Actor: "session-a", OwnerSession: "session-a"}, nil)
	var refusal *WorkError
	if !errors.As(err, &refusal) || refusal.Code != "user_action_requires_waiting_user" {
		t.Fatalf("action without waiting_user = %v", err)
	}
	waiting := work.ConditionWaitingUser
	tooLarge := strings.Repeat("x", workV2UserActionLimit+1)
	_, err = w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Condition: &waiting, UserAction: &tooLarge, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if !errors.As(err, &refusal) || refusal.Code != "user_action_too_large" {
		t.Fatalf("oversize action = %v", err)
	}
}

func TestPlanningNeverBecomesExecutableAndProposalNeedsOwnedEvidence(t *testing.T) {
	w := newWorkV2Test(t)
	planning := createWorkV2Test(t, w, work.KindEpic)
	if _, err := w.Assign(context.Background(), planning.Item.ID, AssignWorkV2{ExpectedVersion: 1,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil); err == nil {
		t.Fatal("planning item was assignable")
	}
	executable := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), executable.Item.ID, AssignWorkV2{ExpectedVersion: 1,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.Propose(context.Background(), work.ProposalV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Follow-up", Description: "A newly discovered concern", Reason: "Found while implementing",
		SessionID: "session-a", SourceWorkID: assigned.Item.ID})
	if err != nil || p.State != "pending" {
		t.Fatalf("proposal: %+v %v", p, err)
	}
	_, err = w.Propose(context.Background(), work.ProposalV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Foreign", Description: "Not this owner's evidence", Reason: "No authority",
		SessionID: "session-b", SourceWorkID: assigned.Item.ID})
	var refusal *WorkError
	if !errors.As(err, &refusal) || refusal.Code != "proposal_source_invalid" {
		t.Fatalf("foreign proposal: %v", err)
	}
}

func TestPersonCommandsVersionReferenceImages(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	added, err := w.AddImage(context.Background(), v.Item.ID, AddImageV2{ExpectedVersion: v.Item.Version,
		Title: "state.png", Data: []byte("png"), Width: 3, Height: 2, Actor: "local"}, nil)
	if err != nil || added.Item.Version != v.Item.Version+1 || len(added.Images) != 1 {
		t.Fatalf("add: %+v %v", added, err)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil || len(full.Images) != 1 || full.Images[0].Title != "state.png" {
		t.Fatalf("read: %+v %v", full, err)
	}
	if _, err := w.DeleteImage(context.Background(), v.Item.ID, full.Images[0].ID, v.Item.Version, "local", nil); err == nil {
		t.Fatal("a stale version deleted a reference image")
	}
	deleted, err := w.DeleteImage(context.Background(), v.Item.ID, full.Images[0].ID, added.Item.Version, "local", nil)
	if err != nil || deleted.Item.Version != added.Item.Version+1 {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	full, _ = w.Item(context.Background(), v.Item.ID)
	if len(full.Images) != 0 {
		t.Fatalf("deleted image remains: %+v", full.Images)
	}
}

func TestPersonAddsImagesOnlyBeforeDirectTodoDelivery(t *testing.T) {
	w := newWorkV2Test(t)
	todo, err := w.CreateDirectTodo(context.Background(), NewDirectTodoV2{
		SessionID: "session-a", Text: "Use this screenshot", Actor: "local",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	added, image, err := w.AddDirectTodoImage(context.Background(), todo.ID, AddDirectTodoImageV2{
		ExpectedVersion: todo.Version, SessionID: todo.SessionID, Title: "screen.png",
		Data: []byte("png"), Width: 3, Height: 2, Actor: "local",
	}, nil)
	if err != nil || added.Version != todo.Version+1 || image.TodoID != todo.ID {
		t.Fatalf("add: %+v %+v %v", added, image, err)
	}
	images, err := w.DirectTodoImages(context.Background(), todo.ID)
	if err != nil || len(images) != 1 || images[0].Title != "screen.png" {
		t.Fatalf("read: %+v %v", images, err)
	}
	if _, err := w.MarkDirectTodoSent(context.Background(), todo.ID, todo.SessionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.AddDirectTodoImage(context.Background(), todo.ID, AddDirectTodoImageV2{
		ExpectedVersion: added.Version + 1, SessionID: todo.SessionID, Data: []byte("png"), Width: 1, Height: 1,
	}, nil); err == nil {
		t.Fatal("a delivered to-do accepted another image")
	}
}

func TestDirectTodoCanBeSentAgainOnlyAfterTheSessionReadsIt(t *testing.T) {
	w := newWorkV2Test(t)
	todo, err := w.CreateDirectTodo(context.Background(), NewDirectTodoV2{
		SessionID: "session-a", Text: "Please handle this", Actor: "local",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Unix(1_790_000_001, 0)
	sent, err := w.MarkDirectTodoSent(context.Background(), todo.ID, todo.SessionID, first)
	if err != nil || !sent.SentAt.Equal(first) || !sent.ReadAt.IsZero() {
		t.Fatalf("first send: %+v %v", sent, err)
	}
	if _, err := w.MarkDirectTodoSent(context.Background(), todo.ID, todo.SessionID, first.Add(time.Second)); err == nil {
		t.Fatal("an unread delivery was sent again")
	} else if e, ok := err.(*WorkError); !ok || e.Code != "todo_awaiting_read" {
		t.Fatalf("unread resend: %T %v", err, err)
	}
	rows, _, err := w.DirectTodos(context.Background(), todo.SessionID, false, true)
	if err != nil || len(rows) != 1 || rows[0].ReadAt.IsZero() {
		t.Fatalf("read: %+v %v", rows, err)
	}
	second := first.Add(2 * time.Second)
	resent, err := w.MarkDirectTodoSent(context.Background(), todo.ID, todo.SessionID, second)
	if err != nil || !resent.SentAt.Equal(second) || !resent.ReadAt.IsZero() {
		t.Fatalf("resend: %+v %v", resent, err)
	}
}
