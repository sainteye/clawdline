package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// An Agent's waiting_user points at a decision (decision-wait): the Board
// item and the decision change together, in one transaction, whichever side
// ends the wait.

type decisionWait struct {
	t      *testing.T
	p      *Participation
	v2     *WorkSystemV2
	st     *store.Store
	clock  *boardClock
	closed []DecisionClosure
}

func newDecisionWait(t *testing.T) *decisionWait {
	t.Helper()
	p, _, st, clock := newParticipation(t)
	v2 := NewWorkSystemV2(st)
	v2.Now = clock.now
	dw := &decisionWait{t: t, p: p, v2: v2, st: st, clock: clock}
	p.DecisionClosed = func(_ context.Context, c DecisionClosure) { dw.closed = append(dw.closed, c) }
	return dw
}

// owned is a new item assigned to session.
func (dw *decisionWait) owned(session string) WorkV2View {
	dw.t.Helper()
	ctx := context.Background()
	created, err := dw.v2.Create(ctx, NewWorkV2{ProjectID: "project-p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Release the build", Description: "It needs the person's release window.", Actor: "user"}, nil)
	if err != nil {
		dw.t.Fatal(err)
	}
	assigned, err := dw.v2.Assign(ctx, created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: session, Actor: "user"}, false, nil)
	if err != nil {
		dw.t.Fatal(err)
	}
	return assigned
}

func (dw *decisionWait) ask(session, workID string) work.Decision {
	dw.t.Helper()
	d, err := dw.p.OpenDecision(context.Background(), DecisionRequest{Session: session, WorkID: workID,
		Question: "Is the release window confirmed?", Default: "cannot", Due: time.Hour,
		Options: []work.Option{{ID: "done", Label: "I've done it"}, {ID: "cannot", Label: "I can't"}}}, nil)
	if err != nil {
		dw.t.Fatal(err)
	}
	return d
}

func (dw *decisionWait) wait(v WorkV2View, session, decision string) (WorkV2View, error) {
	waiting := work.ConditionWaitingUser
	return dw.v2.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: v.Item.Version,
		Condition: &waiting, DecisionID: &decision, Actor: session, OwnerSession: session}, nil)
}

// waiting is an owned item waiting on a fresh decision.
func (dw *decisionWait) waiting() (WorkV2View, work.Decision) {
	dw.t.Helper()
	v := dw.owned(theRoot)
	d := dw.ask(theRoot, v.Item.ID)
	w, err := dw.wait(v, theRoot, d.ID)
	if err != nil {
		dw.t.Fatal(err)
	}
	if w.Item.Condition != work.ConditionWaitingUser || w.Item.DecisionID != d.ID {
		dw.t.Fatalf("waiting item = %+v", w.Item)
	}
	return w, d
}

func (dw *decisionWait) item(id string) WorkV2View {
	dw.t.Helper()
	v, err := dw.v2.Item(context.Background(), id)
	if err != nil {
		dw.t.Fatal(err)
	}
	return v
}

func (dw *decisionWait) decision(id string) work.Decision {
	dw.t.Helper()
	d, err := dw.p.Decision(context.Background(), id)
	if err != nil {
		dw.t.Fatal(err)
	}
	return d
}

func eventWith(v WorkV2View, kind string, parts ...string) bool {
	for _, e := range v.Events {
		if e.Kind != kind {
			continue
		}
		all := true
		for _, part := range parts {
			all = all && strings.Contains(e.Payload, part)
		}
		if all {
			return true
		}
	}
	return false
}

func TestAnAgentsWaitingUserNeedsADecisionNotFreeText(t *testing.T) {
	dw := newDecisionWait(t)
	v := dw.owned(theRoot)
	waiting := work.ConditionWaitingUser
	action := "Confirm the release window."
	for name, c := range map[string]EditWorkV2{
		"bare condition": {Condition: &waiting},
		"free text":      {Condition: &waiting, UserAction: &action},
	} {
		c.ExpectedVersion, c.Actor, c.OwnerSession = v.Item.Version, theRoot, theRoot
		_, err := dw.v2.Edit(context.Background(), v.Item.ID, c, nil)
		if codeOf(err) != "waiting_user_requires_decision" || !strings.Contains(err.Error(), "POST /v1/orchestrator/decisions") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// The person may still name a free-text action, and the daemon's own
	// waits are not Agent writes.
	person, err := dw.v2.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: v.Item.Version,
		Condition: &waiting, UserAction: &action, Actor: "user", Person: true}, nil)
	if err != nil || person.Item.UserAction != action {
		t.Fatalf("person's action: %+v %v", person.Item, err)
	}
	// An item that already waits on that text keeps it until its condition
	// clears; the Agent replaces it with a decision.
	d := dw.ask(theRoot, v.Item.ID)
	linked, err := dw.wait(person, theRoot, d.ID)
	if err != nil || linked.Item.DecisionID != d.ID || linked.Item.UserAction != "" {
		t.Fatalf("linked over a free-text wait: %+v %v", linked.Item, err)
	}
	if !eventWith(dw.item(v.Item.ID), "item.edited", `"decision_id":"`+d.ID+`"`) {
		t.Fatalf("the edit does not record its decision: %+v", dw.item(v.Item.ID).Events)
	}
}

func TestADecisionAnItemWaitsOnIsTheOwnersOpenQuestionAboutIt(t *testing.T) {
	dw := newDecisionWait(t)
	v := dw.owned(theRoot)
	other := dw.owned("other-session")
	theirs := dw.ask("other-session", other.Item.ID)
	aboutOther := dw.owned(theRoot)
	elsewhere := dw.ask(theRoot, aboutOther.Item.ID)
	closed := dw.ask(theRoot, v.Item.ID)
	if _, err := dw.p.AnswerDecision(context.Background(), closed.ID, "done", "user", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	blocked := work.ConditionBlocked
	mine := dw.ask(theRoot, v.Item.ID)
	for code, try := range map[string]func() error{
		"decision_not_found":     func() error { _, err := dw.wait(v, theRoot, newWorkID()); return err },
		"decision_other_session": func() error { _, err := dw.wait(v, theRoot, theirs.ID); return err },
		"decision_other_item":    func() error { _, err := dw.wait(v, theRoot, elsewhere.ID); return err },
		"decision_not_open":      func() error { _, err := dw.wait(v, theRoot, closed.ID); return err },
		"decision_requires_waiting_user": func() error {
			_, err := dw.v2.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: v.Item.Version,
				Condition: &blocked, DecisionID: &mine.ID, Actor: theRoot, OwnerSession: theRoot}, nil)
			return err
		},
	} {
		if err := try(); codeOf(err) != code {
			t.Fatalf("%s: %v", code, err)
		}
	}
	if got := dw.item(v.Item.ID).Item; got.Condition != "" || got.DecisionID != "" {
		t.Fatalf("a refused link changed the item: %+v", got)
	}
}

func TestTheDefaultStandingOnExpiryEndsTheWait(t *testing.T) {
	dw := newDecisionWait(t)
	v, d := dw.waiting()
	dw.clock.at = dw.clock.at.Add(2 * time.Hour)
	if err := dw.p.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := dw.decision(d.ID); got.State != work.DecisionDefaulted || got.Answer != "cannot" {
		t.Fatalf("decision after the sweep = %+v", got)
	}
	after := dw.item(v.Item.ID)
	if after.Item.Condition != "" || after.Item.DecisionID != "" {
		t.Fatalf("defaulted item still waits: %+v", after.Item)
	}
	if !eventWith(after, "decision.closed", `"state":"defaulted"`, `"answer":"cannot"`, `"label":"I can't"`) {
		t.Fatalf("no defaulted fact on the item: %+v", after.Events)
	}
	if len(dw.closed) != 1 {
		t.Fatalf("closures = %+v", dw.closed)
	}
	notice := dw.closed[0].Notice()
	if !strings.Contains(notice, "default stands") || !strings.Contains(notice, `"I can't"`) ||
		!strings.Contains(notice, "option cannot") || !strings.Contains(notice, v.Item.ID) {
		t.Fatalf("notice = %q", notice)
	}
}

func TestThePersonsAnswerEndsTheWaitAndTellsTheOwner(t *testing.T) {
	dw := newDecisionWait(t)
	v, d := dw.waiting()
	if _, err := dw.p.AnswerDecision(context.Background(), d.ID, "done", "user", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := dw.item(v.Item.ID)
	if after.Item.Condition != "" || after.Item.DecisionID != "" {
		t.Fatalf("answered item still waits: %+v", after.Item)
	}
	if !eventWith(after, "decision.closed", `"state":"answered"`, `"answer":"done"`, `"label":"I've done it"`) {
		t.Fatalf("no answered fact on the item: %+v", after.Events)
	}
	if len(dw.closed) != 1 || dw.closed[0].Owner.SessionID != theRoot {
		t.Fatalf("closures = %+v", dw.closed)
	}
	if notice := dw.closed[0].Notice(); !strings.Contains(notice, "The person answered decision "+d.ID) ||
		!strings.Contains(notice, `"I've done it"`) || !strings.Contains(notice, "option done, answered") {
		t.Fatalf("notice = %q", notice)
	}
}

// The Session withdrawing — clearing or replacing the condition, or the item
// leaving it — takes the question out of "Waiting on you", and a late answer
// to it is refused by name.
func TestAWithdrawnWaitTakesTheQuestionAway(t *testing.T) {
	dw := newDecisionWait(t)
	ctx := context.Background()
	none, blocked := work.Condition(""), work.ConditionBlocked
	cases := map[string]func(v WorkV2View) error{
		"condition cleared": func(v WorkV2View) error {
			_, err := dw.v2.Edit(ctx, v.Item.ID, EditWorkV2{ExpectedVersion: v.Item.Version, Condition: &none,
				Actor: theRoot, OwnerSession: theRoot}, nil)
			return err
		},
		"another condition": func(v WorkV2View) error {
			_, err := dw.v2.Edit(ctx, v.Item.ID, EditWorkV2{ExpectedVersion: v.Item.Version, Condition: &blocked,
				Actor: theRoot, OwnerSession: theRoot}, nil)
			return err
		},
		"another decision": func(v WorkV2View) error {
			next := dw.ask(theRoot, v.Item.ID)
			_, err := dw.wait(v, theRoot, next.ID)
			return err
		},
		"released": func(v WorkV2View) error {
			_, err := dw.v2.Unassign(ctx, v.Item.ID, v.Item.Version, "user", nil)
			return err
		},
		"reassigned": func(v WorkV2View) error {
			_, err := dw.v2.Assign(ctx, v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
				Mode: "existing_session", SessionID: "other-session", Actor: "user"}, false, nil)
			return err
		},
		"cancelled": func(v WorkV2View) error {
			_, err := dw.v2.Cancel(ctx, v.Item.ID, v.Item.Version, "user", "Not needed", nil)
			return err
		},
		"done": func(v WorkV2View) error {
			_, err := dw.v2.Complete(ctx, v.Item.ID, v.Item.Version, "user", "Done by hand", nil)
			return err
		},
	}
	for name, withdraw := range cases {
		v, d := dw.waiting()
		if err := withdraw(v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := dw.decision(d.ID); got.State != work.DecisionWithdrawn || got.Answer != "" {
			t.Fatalf("%s: decision = %+v", name, got)
		}
		after := dw.item(v.Item.ID)
		if after.Item.DecisionID == d.ID {
			t.Fatalf("%s: item still points at the withdrawn decision: %+v", name, after.Item)
		}
		if !eventWith(after, "decision.withdrawn", `"decision_id":"`+d.ID+`"`) {
			t.Fatalf("%s: no withdrawal on the item: %+v", name, after.Events)
		}
		open, err := dw.p.DecisionList(ctx, work.DecisionOpen, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range open.Rows {
			if row.ID == d.ID {
				t.Fatalf("%s: a withdrawn decision still waits on the person", name)
			}
		}
		if _, err := dw.p.AnswerDecision(ctx, d.ID, "done", "user", "", nil, nil); codeOf(err) != "decision_withdrawn" {
			t.Fatalf("%s: answering a withdrawn decision = %v", name, err)
		}
	}
	if len(dw.closed) != 0 {
		t.Fatalf("a withdrawal told the owner an answer: %+v", dw.closed)
	}
}
