package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func sessionItemRun(t *testing.T, w *WorkSystemV2, conversation string) work.Run {
	t.Helper()
	runs := &Runs{Store: w.Store, Now: w.Now}
	run, err := runs.Issue(context.Background(), session.Session{ID: "terminal-a", ConversationID: conversation,
		Assistant: session.AssistantClaude}, "local", "Please put this on the Board:\n- first\n- second")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func newSessionItem(run work.Run, kind work.Kind, description string, steps ...string) NewSessionItemV2 {
	return NewSessionItemV2{Run: run, SessionID: "conv-a", TerminalID: "terminal-a", Assistant: "claude",
		SessionProject: "/p", ProjectID: "p", ProjectPath: "/p", Kind: kind, Title: "From the message",
		Description: description, AcceptanceCriteria: "The requested result is observable.", Steps: steps}
}

func refusedAsWork(t *testing.T, err error, code string) {
	t.Helper()
	var we *WorkError
	if !errors.As(err, &we) || we.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// An executable item a Session creates on its person's message, asking to
// take it, arrives assigned to that Session, with the description's list as
// its steps and the message as its provenance, in one write.
func TestASessionCreatesAnAssignedItemWithStepsFromThePersonsMessage(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	n := newSessionItem(run, work.KindFeature, "Do these:\n- first\n- second\n- third")
	n.AssignSelf = true
	v, err := w.CreateFromSession(context.Background(), n, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Item.OwnerSession != "conv-a" || v.Item.Phase != work.PhaseAssigned || v.Item.CreatedBy != run.Actor() {
		t.Fatalf("item: %+v", v.Item)
	}
	if len(v.Assignments) != 1 || v.Assignments[0].State != "active" || v.Assignments[0].Mode != "existing_session" ||
		v.Assignments[0].TerminalID != "terminal-a" || v.Assignments[0].Assistant != "claude" {
		t.Fatalf("assignment: %+v", v.Assignments)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range full.Steps {
		titles = append(titles, s.Title)
	}
	if strings.Join(titles, "|") != "first|second|third" {
		t.Fatalf("steps: %v", titles)
	}
	if via := full.Item.CreatedVia; via == nil || via.Run != run.ID || via.Session != "conv-a" ||
		via.At != run.At.Unix() || via.Excerpt != "Please put this on the Board: - first - second" {
		t.Fatalf("provenance: %+v", full.Item.CreatedVia)
	}
	var created, assigned bool
	for _, e := range full.Events {
		created = created || (e.Kind == "item.created" && strings.Contains(e.Payload, `"via_run":"`+run.ID+`"`) &&
			strings.Contains(e.Payload, `"session_id":"conv-a"`))
		assigned = assigned || e.Kind == "item.assigned"
	}
	if !created || !assigned {
		t.Fatalf("events: %+v", full.Events)
	}
	active, _, err := w.List(context.Background(), "", "conv-a", "open", "")
	if err != nil || len(active) != 1 {
		t.Fatalf("the Session's assigned projection: %d %v", len(active), err)
	}
}

// Without AssignSelf an executable item is created unassigned, where the
// person's unassigned items of its kind wait, and still carries its steps —
// the explicit ones, or the description's list — for whoever takes it.
func TestASessionCreatedItemIsUnassignedWithItsStepsByDefault(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	for _, n := range []NewSessionItemV2{
		newSessionItem(run, work.KindFeature, "Later:\n- first\n- second"),
		newSessionItem(run, work.KindIssue, "Later", "first", "second"),
		newSessionItem(run, work.KindEpic, "Later", "first", "second"),
	} {
		v, err := w.CreateFromSession(context.Background(), n, nil)
		if err != nil {
			t.Fatal(err)
		}
		if v.Item.OwnerSession != "" || v.Item.Phase != work.PhaseCreated || len(v.Assignments) != 0 ||
			v.Item.Area() == "planning" {
			t.Fatalf("%s arrived taken: %+v", n.Kind, v.Item)
		}
		full, err := w.Item(context.Background(), v.Item.ID)
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, s := range full.Steps {
			titles = append(titles, s.Title)
		}
		if strings.Join(titles, "|") != "first|second" {
			t.Fatalf("%s steps: %v", n.Kind, titles)
		}
		for _, e := range full.Events {
			if e.Kind == "item.assigned" {
				t.Fatalf("%s was assigned: %+v", n.Kind, full.Events)
			}
		}
	}
	mine, _, err := w.List(context.Background(), "", "conv-a", "open", "")
	if err != nil || len(mine) != 0 {
		t.Fatalf("the Session's assigned projection: %d %v", len(mine), err)
	}
}

func TestMachineStewardCreatesBoardItemBeforeDelegatingWithoutOwningProjectCode(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	n := newSessionItem(run, work.KindFeature, "Work for a Project owner", "implement", "verify")
	n.SessionProject = "/machine-workspace"
	n.MachineTriage = true
	v, err := w.CreateFromSession(context.Background(), n, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Item.Phase != work.PhaseCreated || v.Item.OwnerSession != "" || len(v.Assignments) != 0 || len(v.Steps) != 2 {
		t.Fatalf("machine item must wait for its Project owner: %+v", v)
	}
	if v.Item.CreatedVia == nil || v.Item.CreatedVia.Run != run.ID {
		t.Fatalf("the person's message was lost: %+v", v.Item.CreatedVia)
	}
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "project-owner", TerminalID: "project-terminal",
		Assistant: "codex", Actor: run.Actor()}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.Item.OwnerSession != "project-owner" || assigned.Item.Phase != work.PhaseAssigned {
		t.Fatalf("delegated item has no Project owner: %+v", assigned.Item)
	}
}

// Steps the Session names are the steps, in order, and the description's list
// is not read for more — no step is written twice, and nothing becomes a
// Session to-do.
func TestExplicitStepsWinOverTheDescriptionList(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	v, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindIssue,
		"Context:\n- not a step\n- nor this", " one ", "", "two", "three"), nil)
	if err != nil {
		t.Fatal(err)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range full.Steps {
		titles = append(titles, s.Title)
	}
	if strings.Join(titles, "|") != "one|two|three" {
		t.Fatalf("steps: %v", titles)
	}
	todos, _, err := w.DirectTodos(context.Background(), "conv-a", true, false)
	if err != nil || len(todos) != 0 {
		t.Fatalf("direct to-dos: %d %v", len(todos), err)
	}
	many := make([]string, store.WorkV2StepLimit+1)
	for i := range many {
		many[i] = "step"
	}
	_, err = w.CreateFromSession(context.Background(), newSessionItem(run, work.KindIssue, "Too many", many...), nil)
	refusedAsWork(t, err, "too_many_steps")
}

// A planning kind is created exactly as a person's would be: unassigned, in
// Planning, without steps.
func TestAPlanningItemFromASessionStaysUnassignedInPlanning(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	v, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindRefactor, "- a\n- b"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Item.OwnerSession != "" || v.Item.Area() != "planning" || len(v.Assignments) != 0 || len(v.Steps) != 0 {
		t.Fatalf("planning item: %+v", v)
	}
	_, err = w.CreateFromSession(context.Background(), newSessionItem(run, work.KindPlan, "plan", "a", "b"), nil)
	refusedAsWork(t, err, "planning_has_no_steps")
}

// Whatever fails inside the write, nothing of it stays: not the item, not its
// assignment, not its steps.
func TestASessionCreatedItemIsWrittenWholeOrNotAtAll(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	unclaimed := func(WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		return store.ReceiptKey{Scope: "work", Actor: "conv-a", Key: "never-claimed"}, store.ReceiptAnswer{Status: 201}, true
	}
	_, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindFeature, "- a\n- b"), unclaimed)
	if err == nil {
		t.Fatal("a receipt that could not be filed did not fail the write")
	}
	items, _, err := w.List(context.Background(), "", "", "all", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("a failed write left %d items (%v)", len(items), err)
	}
	// The failed attempt did not spend the run's allowance either.
	for i := 0; i < runItemLimit; i++ {
		if _, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindIssue, "item"), nil); err != nil {
			t.Fatalf("item %d: %v", i+1, err)
		}
	}
}

// One message backs at most five items; the sixth is refused and writes
// nothing, and a message to another Session backs none here.
func TestOneMessageBacksAtMostFiveItemsAndOnlyForItsOwnSession(t *testing.T) {
	w := newWorkV2Test(t)
	run := sessionItemRun(t, w, "conv-a")
	for i := 0; i < runItemLimit; i++ {
		kind := work.KindIssue
		if i == 0 {
			kind = work.KindRefactor // planning items count too
		}
		if _, err := w.CreateFromSession(context.Background(), newSessionItem(run, kind, "item"), nil); err != nil {
			t.Fatalf("item %d: %v", i+1, err)
		}
	}
	_, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindIssue, "item"), nil)
	refusedAsWork(t, err, "run_items_exhausted")
	items, _, _ := w.List(context.Background(), "", "", "all", "")
	if len(items) != runItemLimit {
		t.Fatalf("items after the refusal: %d", len(items))
	}

	other := sessionItemRun(t, w, "conv-b")
	_, err = w.CreateFromSession(context.Background(), newSessionItem(other, work.KindIssue, "item"), nil)
	refusedAsWork(t, err, "run_other_session")

	fresh := sessionItemRun(t, w, "conv-a")
	n := newSessionItem(fresh, work.KindIssue, "item")
	n.SessionProject = "/elsewhere"
	_, err = w.CreateFromSession(context.Background(), n, nil)
	refusedAsWork(t, err, "project_mismatch")
}

// A run older than the relay window carries no item: Runs.Relay refuses it
// before anything is created.
func TestAnExpiredRunIsRefusedAsARelay(t *testing.T) {
	w := newWorkV2Test(t)
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	runs := &Runs{Store: w.Store, Now: func() time.Time { return at }}
	run, err := runs.Issue(context.Background(), session.Session{ID: "terminal-a", ConversationID: "conv-a"}, "local", "make an item")
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(work.RelayWindow + time.Minute)
	_, err = runs.Relay(context.Background(), run.ID)
	refusedAsWork(t, err, "run_expired")
}

// The excerpt a run keeps is the message with its whitespace folded, cut on a
// character boundary.
func TestARunKeepsABoundedExcerptOfTheMessage(t *testing.T) {
	if got := RunExcerptOf("  a\n\tb  "); got != "a b" {
		t.Fatalf("folded: %q", got)
	}
	long := strings.Repeat("字", runExcerptLimit+50)
	got := RunExcerptOf(long)
	if utf8.RuneCountInString(got) != runExcerptLimit || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Fatalf("cut: %d runes, %q…", utf8.RuneCountInString(got), got[:9])
	}
	exact := strings.Repeat("a", runExcerptLimit)
	if RunExcerptOf(exact) != exact {
		t.Fatal("a message exactly at the limit was cut")
	}
}
