package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// seedOwnedItem puts one Board item on the conversation with an active
// assignment, in phase, with one step done when stepped.
func seedOwnedItem(t *testing.T, s *Server, id, conversation string, phase work.Phase, stepped bool) {
	t.Helper()
	at := time.Unix(90_000, 0)
	item := work.ItemV2{ID: id, ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue, Title: "t " + id,
		Description: "d", Phase: phase, DeploymentPolicy: work.DeployAgentDecides, OwnerSession: conversation,
		CreatedBy: "local", CreatedAt: at, UpdatedAt: at, Cycle: 1, Version: 1}
	if err := s.store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		if stepped {
			if err := tx.AddStep(work.StepV2{ID: id + "-step", WorkID: id, Title: "one", Done: true,
				CreatedBy: "local", CreatedAt: at, CompletedAt: at, Version: 1}); err != nil {
				return err
			}
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: id + "-a", WorkID: id, Mode: "existing_session",
			SessionID: conversation, TerminalID: "%4", State: "active", CreatedAt: at, UpdatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
}

func ownerOf(t *testing.T, s *Server, id string) string {
	t.Helper()
	v, err := s.workV2().Item(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return v.Item.OwnerSession
}

// The list names an unstarted item apart from a started one, a close is
// blocked by both, and the person's forced close — pinned to the reading that
// listed them — takes the session off the unstarted one only, then closes.
func TestAForcedCloseReleasesOnlyUnstartedBoardItems(t *testing.T) {
	p := archivePane("conv-quiet")
	s := archiveServer(t, p)
	seedOwnedItem(t, s, "10000000-0000-4000-8000-000000000001", "conv-quiet", work.PhaseAssigned, false)
	seedOwnedItem(t, s, "10000000-0000-4000-8000-000000000002", "conv-quiet", work.PhaseAssigned, true)

	c := s.sessionsPayloadFrom(context.Background(), s.freshReading(context.Background())).Sessions[0].Closeability
	codes := map[string]string{}
	for _, r := range c.Reasons {
		codes[r.SubjectID] = r.Code
	}
	if codes["10000000-0000-4000-8000-000000000001"] != "board_item_unstarted" ||
		codes["10000000-0000-4000-8000-000000000002"] != "board_item_open" {
		t.Fatalf("reasons: %+v", c.Reasons)
	}

	blocked := act(t, s, "close", "%4", "close-1", `{"expected_closeability_version":"`+c.Version+`"}`)
	if blocked.Code != http.StatusConflict || codeOf(t, blocked) != "close_blocked" || len(p.done()) != 0 {
		t.Fatalf("unforced: %d %s, terminal=%q", blocked.Code, blocked.Body, p.done())
	}
	if ownerOf(t, s, "10000000-0000-4000-8000-000000000001") != "conv-quiet" {
		t.Fatal("an unforced close released an item")
	}

	closed := act(t, s, "close", "%4", "close-2", `{"force":true,"expected_closeability_version":"`+c.Version+`"}`)
	if closed.Code != http.StatusOK || len(p.done()) != 1 {
		t.Fatalf("forced: %d %s, terminal=%q", closed.Code, closed.Body, p.done())
	}
	if got := ownerOf(t, s, "10000000-0000-4000-8000-000000000001"); got != "" {
		t.Fatalf("the unstarted item is still owned by %q", got)
	}
	if got := ownerOf(t, s, "10000000-0000-4000-8000-000000000002"); got != "conv-quiet" {
		t.Fatalf("the started item was released: owner %q", got)
	}
	// A second release of the same conversation finds nothing left to do.
	if err := s.releaseUnstarted(context.Background(), "conv-quiet", "local"); err != nil {
		t.Fatalf("retried release: %v", err)
	}
}

// A deploying item whose deploy the Session already put to the person, as an
// open decision, is not on the close's list; a deploying item with no such
// decision is. Once that one is done, a plain close takes the session off the
// asked item before the terminal goes, and the decision stays open.
func TestACloseLeavesTheDeployDecisionWithThePerson(t *testing.T) {
	ctx := context.Background()
	p := archivePane("conv-quiet")
	s := archiveServer(t, p)
	const asked, bare = "10000000-0000-4000-8000-000000000003", "10000000-0000-4000-8000-000000000004"
	seedOwnedItem(t, s, asked, "conv-quiet", work.PhaseDeploying, true)
	seedOwnedItem(t, s, bare, "conv-quiet", work.PhaseDeploying, true)
	d, err := s.participation().OpenDecision(ctx, app.DecisionRequest{Session: "conv-quiet", WorkID: asked,
		Question: "Deploy it?", Default: "later", Due: time.Hour,
		Options: []work.Option{{ID: "done", Label: "Deployed"}, {ID: "later", Label: "Later"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiting := work.ConditionWaitingUser
	if _, err := s.workV2().Edit(ctx, asked, app.EditWorkV2{ExpectedVersion: 1, Condition: &waiting,
		DecisionID: &d.ID, Actor: "conv-quiet", OwnerSession: "conv-quiet"}, nil); err != nil {
		t.Fatal(err)
	}

	c := s.sessionsPayloadFrom(ctx, s.freshReading(ctx)).Sessions[0].Closeability
	if len(c.Reasons) != 1 || c.Reasons[0].SubjectID != bare || c.Reasons[0].Code != "board_item_open" {
		t.Fatalf("reasons: %+v", c.Reasons)
	}
	if err := s.store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(bare)
		if err != nil {
			return err
		}
		next := prev
		next.Phase = work.PhaseDone
		return tx.PutItem(prev, next, "test.done", "test", `{}`)
	}); err != nil {
		t.Fatal(err)
	}

	c = s.sessionsPayloadFrom(ctx, s.freshReading(ctx)).Sessions[0].Closeability
	closed := act(t, s, "close", "%4", "close-1", `{"expected_closeability_version":"`+c.Version+`"}`)
	if closed.Code != http.StatusOK || len(p.done()) != 1 {
		t.Fatalf("close: %d %s, terminal=%q", closed.Code, closed.Body, p.done())
	}
	v, err := s.workV2().Item(ctx, asked)
	if err != nil {
		t.Fatal(err)
	}
	if v.Item.OwnerSession != "" || v.Item.Condition != work.ConditionWaitingUser || v.Item.DecisionID != d.ID {
		t.Fatalf("after close: %+v", v.Item)
	}
	if got, err := s.participation().Decision(ctx, d.ID); err != nil || got.State != work.DecisionOpen {
		t.Fatalf("decision after close: %+v %v", got, err)
	}
	// A replayed release finds nothing left to do.
	if err := s.releaseAwaitingDeploy(ctx, "conv-quiet", "local"); err != nil {
		t.Fatalf("retried release: %v", err)
	}
}
