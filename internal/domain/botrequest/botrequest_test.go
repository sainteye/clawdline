package botrequest

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)

var root = Authority{Bot: "lead", ConversationID: "c1", TurnID: "t1", MessageID: "m1"}

func newCard(t *testing.T, in CreateInput) Card {
	t.Helper()
	c, err := NewCard(CardID("machine", "lead", "c1", "m1"), "p1", "/p", "lead", root, in, t0)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func code(t *testing.T, err error, want string) {
	t.Helper()
	if got := CodeOf(err); got != want {
		t.Fatalf("refusal %q (%v), want %q", got, err, want)
	}
}

func TestCardIDIsOnePerPersonMessage(t *testing.T) {
	a := CardID("machine", "lead", "c1", "m1")
	if a != CardID("machine", "lead", "c1", "m1") {
		t.Fatal("same root message, different ids")
	}
	if a == CardID("machine", "lead", "c1", "m2") {
		t.Fatal("different messages share an id")
	}
	if len(a) != 36 || strings.ToLower(a) != a {
		t.Fatalf("not a lowercase UUID: %s", a)
	}
}

func TestCreateNeedsChecklistOrKnownKind(t *testing.T) {
	_, err := NewCard("x", "p", "/p", "lead", root, CreateInput{Title: "chat"}, t0)
	code(t, err, CodeChecklistEmpty)
	c := newCard(t, CreateInput{Title: "feature", Kind: "development"})
	if len(c.Checklist) != 2 || c.Checklist[0].Key != "main" || c.Checklist[1].Key != "github" {
		t.Fatalf("development default: %+v", c.Checklist)
	}
	_, err = NewCard("x", "p", "/p", "lead", Authority{Bot: "lead"}, CreateInput{Title: "t", Kind: "development"}, t0)
	code(t, err, CodeNoAuthority)
}

// Each row of §7.3 refuses the wrong actor.
func TestAuthorityTable(t *testing.T) {
	c := newCard(t, CreateInput{Title: "feature", Kind: "development"})
	c.Chain = []string{"lead", "reviewer", "other"}
	c.Baton = Baton{Role: RoleReviewer, Bot: "reviewer"}
	person, lead, holder, other, stranger := Actor{Person: true}, Actor{Bot: "lead"}, Actor{Bot: "reviewer"}, Actor{Bot: "other"}, Actor{Bot: "stranger"}
	rows := []struct {
		act  Action
		want map[Actor]bool
	}{
		{ActEdit, map[Actor]bool{person: true, lead: true, holder: true, other: false, stranger: false}},
		{ActHandoffChain, map[Actor]bool{person: true, lead: true, holder: true, other: false, stranger: false}},
		{ActHandoffOutside, map[Actor]bool{person: true, lead: false, holder: false, other: false, stranger: false}},
		{ActVerdict, map[Actor]bool{person: false, lead: false, holder: true, other: false, stranger: false}},
		{ActMark, map[Actor]bool{person: true, lead: false, holder: true, other: false, stranger: false}},
		{ActWait, map[Actor]bool{person: false, lead: false, holder: true, other: false, stranger: false}},
		{ActDone, map[Actor]bool{person: true, lead: false, holder: true, other: false, stranger: false}},
		{ActCancelReopen, map[Actor]bool{person: true, lead: false, holder: false, other: false, stranger: false}},
	}
	for _, r := range rows {
		for a, ok := range r.want {
			err := Authorize(c, a, r.act)
			if ok && err != nil {
				t.Errorf("%s by %s refused: %v", r.act, a.Name(), err)
			}
			if !ok && CodeOf(err) != CodeForbidden {
				t.Errorf("%s by %s: %v, want %s", r.act, a.Name(), err, CodeForbidden)
			}
		}
	}
	// A verdict needs the reviewer role, not just the baton.
	c.Baton.Role = RoleMaker
	code(t, Authorize(c, holder, ActVerdict), CodeForbidden)
	code(t, Authorize(c, Actor{}, ActEdit), CodeForbidden)
}

func TestHandoffStaysInTheChainForBots(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Kind: "development"})
	code(t, c.Handoff(Actor{Bot: "lead"}, "outsider", RoleReviewer, ""), CodeForbidden)
	if err := c.Handoff(Actor{Person: true}, "reviewer", RoleReviewer, "review abc"); err != nil {
		t.Fatal(err)
	}
	if c.Baton != (Baton{Role: RoleReviewer, Bot: "reviewer"}) || !c.InChain("reviewer") {
		t.Fatalf("baton %+v chain %v", c.Baton, c.Chain)
	}
	if err := c.Handoff(Actor{Bot: "reviewer"}, "lead", RoleMaker, "back to you"); err != nil {
		t.Fatal(err)
	}
}

// A publishing card with no task, commit or verdict goes created → done with
// a URL as its last note; done is refused while an entry is open.
func TestPublishingCardCompletesWithAURL(t *testing.T) {
	c := newCard(t, CreateInput{Title: "weekly post", Kind: "publishing"})
	lead := Actor{Bot: "lead"}
	if _, err := c.Check(lead, []Mark{{Label: "draft", State: EntryDone, Note: "draft in chat"}}, t0); err != nil {
		t.Fatal(err)
	}
	_, err := c.Done(lead, t0)
	code(t, err, CodeChecklistOpen)
	if _, err := c.Check(lead, []Mark{
		{Label: "reviewed by the person", State: EntryDone, Note: "approved"},
		{Label: "published", State: EntryDone, Note: "https://example.invalid/post/1"},
	}, t0); err != nil {
		t.Fatal(err)
	}
	if d, n := c.Progress(); d != 3 || n != 3 {
		t.Fatalf("progress %d/%d", d, n)
	}
	ok, err := c.Done(lead, t0)
	if err != nil || !ok || c.State != StateDone || c.ProgressState() != ProgressDone {
		t.Fatalf("done %v %v %s", ok, err, c.State)
	}
	again, err := c.Done(lead, t0)
	if err != nil || again {
		t.Fatalf("second done changed something: %v %v", again, err)
	}
	if got := c.Checklist[2]; got.ReportedBy != "bot:lead" || got.Note != "https://example.invalid/post/1" {
		t.Fatalf("last entry %+v", got)
	}
}

func TestNotNeededNeedsAReasonAndCheckIsIdempotent(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Kind: "development"})
	lead := Actor{Bot: "lead"}
	_, err := c.Check(lead, []Mark{{Label: "GitHub", State: EntryNotNeeded}}, t0)
	code(t, err, CodeInvalid)
	marks := []Mark{{Label: "main", State: EntryDone, Note: "abc123"}, {Label: "GitHub", State: EntryNotNeeded, Note: "local only"}}
	if ch, err := c.Check(lead, marks, t0); err != nil || !ch {
		t.Fatalf("first check %v %v", ch, err)
	}
	v := c
	if ch, err := c.Check(lead, marks, t0.Add(time.Hour)); err != nil || ch {
		t.Fatalf("repeated check changed: %v %v", ch, err)
	}
	if c.Checklist[0].ReportedAt != v.Checklist[0].ReportedAt {
		t.Fatal("a repeated mark moved its report time")
	}
	// All-or-nothing: a bad mark leaves the good one unapplied.
	_, err = c.Check(lead, []Mark{{Label: "dev", State: EntryDone}, {Label: "cloud", State: "maybe"}}, t0)
	code(t, err, CodeInvalid)
	if c.entry("dev") >= 0 {
		t.Fatal("a refused check added an entry")
	}
}

func TestVerdictIsRecordedAndBlocksNothing(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Kind: "development"})
	rev := Actor{Bot: "reviewer"}
	if err := c.Handoff(Actor{Bot: "lead"}, "reviewer", RoleReviewer, ""); err == nil {
		t.Fatal("handoff outside the chain by a Bot was accepted")
	}
	c.Chain = append(c.Chain, "reviewer")
	if err := c.Handoff(Actor{Bot: "lead"}, "reviewer", RoleReviewer, ""); err != nil {
		t.Fatal(err)
	}
	code(t, c.RecordVerdict(rev, "v1", "", VerdictReject, "", t0), CodeInvalid)
	if err := c.RecordVerdict(rev, "v1", "", VerdictReject, "missing tests", t0); err != nil {
		t.Fatal(err)
	}
	if err := c.Handoff(rev, "lead", RoleMaker, "rejected"); err != nil {
		t.Fatalf("a reject blocked the handoff: %v", err)
	}
	if _, err := c.Check(Actor{Bot: "lead"}, []Mark{{Label: "main", State: EntryDone}, {Label: "GitHub", State: EntryDone}}, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Done(Actor{Bot: "lead"}, t0); err != nil {
		t.Fatalf("a reject blocked done: %v", err)
	}
}

func TestReopenStartsANewNotificationCycle(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Checklist: []string{"one"}})
	person := Actor{Person: true}
	if _, err := c.Done(person, t0); err != nil {
		t.Fatal(err)
	}
	first := NotifyKey(c.ID, c.Cycle)
	_, err := c.Reopen(Actor{Bot: "lead"})
	code(t, err, CodeForbidden)
	if ok, err := c.Reopen(person); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if NotifyKey(c.ID, c.Cycle) == first {
		t.Fatal("reopen kept the notification key")
	}
	if got := c.NotifyTargets(Actor{Bot: "lead"}); len(got) != 0 {
		t.Fatalf("the Bot that marked it done is told: %v", got)
	}
	if got := c.NotifyTargets(person); len(got) != 1 || got[0] != "lead" {
		t.Fatalf("targets %v", got)
	}
}

func TestWaitingIsTheHoldersAndShowsAsWaiting(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Checklist: []string{"one"}})
	_, err := c.SetWaiting(Actor{Person: true}, "q1")
	code(t, err, CodeForbidden)
	if ok, err := c.SetWaiting(Actor{Bot: "lead"}, "q1"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if c.ProgressState() != ProgressWaiting {
		t.Fatal(c.ProgressState())
	}
	if ok, err := c.SetWaiting(Actor{Person: true}, ""); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

// A handoff notice turn may dispatch naming its card, and nothing else.
func TestHandoffDispatchIsScopedToTheCard(t *testing.T) {
	c := newCard(t, CreateInput{Title: "f", Kind: "development"})
	c.Chain = append(c.Chain, "integrator")
	if err := c.Handoff(Actor{Bot: "lead"}, "integrator", RoleIntegrator, "merge it"); err != nil {
		t.Fatal(err)
	}
	if err := HandoffDispatch(c, c.ID, "integrator", c.ID); err != nil {
		t.Fatalf("the new holder could not dispatch for its card: %v", err)
	}
	code(t, HandoffDispatch(c, c.ID, "integrator", ""), CodeHandoffScope)
	code(t, HandoffDispatch(c, c.ID, "integrator", CardID("machine", "lead", "c1", "other")), CodeHandoffScope)
	code(t, HandoffDispatch(c, c.ID, "lead", c.ID), CodeHandoffScope)
	c.State = StateDone
	code(t, HandoffDispatch(c, c.ID, "integrator", c.ID), CodeHandoffScope)
}

// A card is a few kilobytes of text; its sizes and counts are deliberately
// not capped (§10, "Limits deliberately not set"), so a long card with many
// entries, Bots and verdicts is ordinary work, not a refusal.
func TestALargeCardIsNotRefused(t *testing.T) {
	c, err := NewCard("x", "p", "/p", "lead", root, CreateInput{
		Title: strings.Repeat("t", 2_000), Description: strings.Repeat("d", 64<<10), Checklist: []string{"a"}}, t0)
	if err != nil {
		t.Fatalf("long title and description: %v", err)
	}
	var marks []Mark
	for i := 0; i < 50; i++ {
		marks = append(marks, Mark{Label: "e" + itoa(int64(i))})
	}
	if _, err := c.Check(Actor{Bot: "lead"}, marks, t0); err != nil || len(c.Checklist) != 51 {
		t.Fatalf("50 more entries: %v, %d entries", err, len(c.Checklist))
	}
	if _, err := c.Check(Actor{Bot: "lead"}, []Mark{{Label: "a", State: EntryDone, Note: strings.Repeat("x", 32<<10)}}, t0); err != nil {
		t.Fatalf("long note: %v", err)
	}
	for i := 0; i < 40; i++ {
		c.JoinChain("b" + itoa(int64(i)))
	}
	if len(c.Chain) != 41 {
		t.Fatalf("chain %d", len(c.Chain))
	}
	if err := c.Handoff(Actor{Bot: "lead"}, "b1", RoleReviewer, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := c.RecordVerdict(Actor{Bot: "b1"}, "v"+itoa(int64(i)), "", VerdictPass, "", t0); err != nil {
			t.Fatalf("verdict %d: %v", i, err)
		}
	}
}
