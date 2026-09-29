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

func TestRunBackedRevisionRetractsVerification(t *testing.T) {
	w, verifying, due := gateCoordinatorFixture(t)
	passed := passVerificationForTest(t, w, due)
	if passed.Gate == nil || passed.Gate.Compact.CurrentAuthorization == nil {
		t.Fatal("fixture lacks live PASS")
	}
	run := work.Run{ID: "10000000-0000-4000-8000-000000000001", Session: passed.Item.OwnerSession,
		At: passed.Item.UpdatedAt.Add(2 * time.Second), Excerpt: "Please revise this item's acceptance to include the second check"}
	criteria := "- The revised result passes its own check."
	revised, err := w.Edit(context.Background(), passed.Item.ID, EditWorkV2{ExpectedVersion: passed.Item.Version,
		AcceptanceCriteria: &criteria, OwnerSession: passed.Item.OwnerSession, RevisionRun: &run}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Item.Phase != work.PhaseImplementing || revised.Item.AcceptanceVersion != passed.Item.AcceptanceVersion+1 {
		t.Fatalf("revision: %+v", revised.Item)
	}
	full, err := w.Item(context.Background(), passed.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Gate == nil || full.Gate.Compact.CurrentAuthorization != nil {
		t.Fatalf("old PASS still authorizes merge: %+v", full.Gate)
	}
	if len(full.Events) == 0 || !strings.Contains(full.Events[len(full.Events)-1].Payload, run.ID) {
		t.Fatalf("run missing from audit: %+v", full.Events)
	}
	_ = verifying
}

func TestRunBackedRevisionCannotChangeMergingAcceptance(t *testing.T) {
	w := newWorkV2Test(t)
	created, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Locked contract", Description: "Keep merge criteria fixed.",
		AcceptanceCriteria: "- Initial contract.", Actor: "person"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "person"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	implementing, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: assigned.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifying, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: implementing.Item.Version,
		SessionID: "session-a", Next: work.PhaseVerifying, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	merging, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: verifying.Item.Version,
		SessionID: "session-a", Next: work.PhaseMerging, Actor: "session-a", Verification: "focused checks passed"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := work.Run{ID: "10000000-0000-4000-8000-000000000001", Session: "session-a",
		At: merging.Item.UpdatedAt.Add(time.Second), Excerpt: "Please revise acceptance for Locked contract"}
	criteria := "- Changed after merge began."
	_, err = w.Edit(context.Background(), created.Item.ID, EditWorkV2{ExpectedVersion: merging.Item.Version,
		AcceptanceCriteria: &criteria, OwnerSession: "session-a", RevisionRun: &run}, nil)
	if got := workErrorCode(t, err); got != "acceptance_locked" {
		t.Fatalf("merging revision = %s", got)
	}
	full, err := w.Item(context.Background(), created.Item.ID)
	if err != nil || full.Item.AcceptanceCriteria != merging.Item.AcceptanceCriteria || full.Item.Version != merging.Item.Version {
		t.Fatalf("merge lock changed: %+v %v", full.Item, err)
	}
}

func TestAcceptanceRevisionInvalidationFailureRollsBackContractAndAudit(t *testing.T) {
	w := newWorkV2Test(t)
	created, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Check revision", Description: "Keep the old contract on failure.",
		AcceptanceCriteria: "- First contract.", Actor: "person"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "person"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := work.Run{ID: "10000000-0000-4000-8000-000000000001", Session: "session-a",
		At: assigned.Item.UpdatedAt.Add(time.Second), Excerpt: "Please revise acceptance for Check revision"}
	w.VerificationInvalidator = func(_ *store.WorkV2Tx, _, _ work.ItemV2, _ string) error { return errors.New("injected failure") }
	newCriteria := "- Replacement contract."
	_, err = w.Edit(context.Background(), assigned.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		AcceptanceCriteria: &newCriteria, OwnerSession: "session-a", RevisionRun: &run}, nil)
	if err == nil {
		t.Fatal("invalidation failure was ignored")
	}
	full, err := w.Item(context.Background(), assigned.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Item.Version != assigned.Item.Version || full.Item.AcceptanceVersion != assigned.Item.AcceptanceVersion ||
		full.Item.AcceptanceCriteria != assigned.Item.AcceptanceCriteria {
		t.Fatalf("partial revision: %+v", full.Item)
	}
	for _, event := range full.Events {
		if strings.Contains(event.Payload, run.ID) {
			t.Fatalf("failed revision was audited as complete: %+v", event)
		}
	}
}
