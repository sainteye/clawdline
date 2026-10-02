package app

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// deploying puts an owned item in deploying, as if it had got there.
func (dw *decisionWait) deploying(v WorkV2View) WorkV2View {
	dw.t.Helper()
	if err := dw.st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(v.Item.ID)
		if err != nil {
			return err
		}
		next := prev
		next.Phase = work.PhaseDeploying
		return tx.PutItem(prev, next, "test.phase", "test", `{}`)
	}); err != nil {
		dw.t.Fatal(err)
	}
	return dw.item(v.Item.ID)
}

func responsibilityCodes(t *testing.T, st *store.Store) map[string]string {
	t.Helper()
	got, err := st.OpenSessionResponsibilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range got {
		out[r.ID] = r.Code
	}
	return out
}

// A deploying item whose Session already asked the person to deploy, by an
// open decision, does not block the close; one with no such decision does.
// The close releases the first and keeps the same decision, a repeated
// release finds nothing to do, and the person's answer leaves it unassigned.
func TestAClosingSessionLeavesTheDeployQuestionWithThePerson(t *testing.T) {
	ctx := context.Background()
	dw := newDecisionWait(t)
	bare := dw.deploying(dw.owned(theRoot))
	asked := dw.deploying(dw.owned(theRoot))
	d := dw.ask(theRoot, asked.Item.ID)
	asked, err := dw.wait(asked, theRoot, d.ID)
	if err != nil {
		t.Fatal(err)
	}

	codes := responsibilityCodes(t, dw.st)
	if codes[bare.Item.ID] != "board_item_open" {
		t.Fatalf("a deploying item with no decision does not block: %v", codes)
	}
	if code, ok := codes[asked.Item.ID]; ok {
		t.Fatalf("an item awaiting the person to deploy blocks as %q", code)
	}
	list, err := dw.st.DeployingAwaitingPersonItems(ctx, theRoot)
	if err != nil || len(list) != 1 || list[0].ID != asked.Item.ID || list[0].Version != asked.Item.Version {
		t.Fatalf("release list = %+v %v", list, err)
	}

	if _, err := dw.v2.ReleaseAtClose(ctx, bare.Item.ID, bare.Item.Version, "user"); codeOf(err) != "item_not_awaiting_deploy" {
		t.Fatalf("released an item with no decision: %v", err)
	}
	released, err := dw.v2.ReleaseAtClose(ctx, asked.Item.ID, asked.Item.Version, "user")
	if err != nil {
		t.Fatal(err)
	}
	got := dw.item(asked.Item.ID)
	if got.Item.OwnerSession != "" || got.Item.Condition != work.ConditionWaitingUser || got.Item.DecisionID != d.ID ||
		got.Item.Version != released.Item.Version {
		t.Fatalf("released item = %+v", got.Item)
	}
	if !eventWith(got, "item.released_at_close", d.ID) || eventWith(got, "decision.withdrawn") {
		t.Fatalf("events = %+v", got.Events)
	}
	if got := dw.decision(d.ID); got.State != work.DecisionOpen {
		t.Fatalf("the decision is %s after the release", got.State)
	}
	if _, err := dw.v2.ReleaseAtClose(ctx, asked.Item.ID, got.Item.Version, "user"); codeOf(err) != "item_unassigned" {
		t.Fatalf("a repeated release: %v", err)
	}
	if list, err := dw.st.DeployingAwaitingPersonItems(ctx, theRoot); err != nil || len(list) != 0 {
		t.Fatalf("after release: %+v %v", list, err)
	}

	if _, err := dw.p.AnswerDecision(ctx, d.ID, "done", "user", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := dw.item(asked.Item.ID)
	if after.Item.OwnerSession != "" || after.Item.Condition == work.ConditionWaitingUser || after.Item.DecisionID != "" ||
		after.Item.Area() != "unassigned" {
		t.Fatalf("answered item = %+v", after.Item)
	}
	if got := dw.decision(d.ID); got.State != work.DecisionAnswered || got.Answer != "done" {
		t.Fatalf("decision = %+v", got)
	}
}
