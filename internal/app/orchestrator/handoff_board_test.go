package orchestrator

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestHandoffBoardTransferWaitsForReceiverAndMovesOnlyCapturedWork(t *testing.T) {
	b, ctx := newTestBroker(t)
	project := t.TempDir()
	otherProject := t.TempDir()
	now := time.Now().Truncate(time.Second)
	owned := "7bd00000-0000-4000-8000-000000000001"
	other := "7bd00000-0000-4000-8000-000000000002"
	closed := "7bd00000-0000-4000-8000-000000000003"
	verifying := "7bd00000-0000-4000-8000-000000000004"
	pending := "7bd00000-0000-4000-8000-000000000005"
	for _, tc := range []struct {
		id, path string
		phase    work.Phase
	}{
		{owned, project, work.PhaseImplementing},
		{other, otherProject, work.PhaseImplementing},
		{closed, project, work.PhaseDone},
		{verifying, project, work.PhaseVerifying},
		{pending, project, work.PhaseImplementing},
	} {
		item := work.ItemV2{ID: tc.id, ProjectID: "p", ProjectPath: tc.path, Kind: work.KindFeature,
			Title: "Work", Phase: tc.phase, DeploymentPolicy: work.DeployAgentDecides,
			OwnerSession: rootConversation, CreatedBy: "person", CreatedAt: now, UpdatedAt: now,
			Cycle: 1, Version: 1}
		if tc.id == verifying {
			item.VerifyGate, item.GateSnapshotCycle = true, 1
		}
		if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			if err := tx.CreateItem(item, "person", "{}"); err != nil {
				return err
			}
			return tx.CreateAssignment(work.AssignmentV2{ID: NewUUID(), WorkID: tc.id,
				Mode: "existing_session", SessionID: rootConversation, TerminalID: "%1", Assistant: "claude",
				State: "active", HumanActor: "person", CreatedAt: now, UpdatedAt: now})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		return tx.CreateAssignment(work.AssignmentV2{ID: NewUUID(), WorkID: pending,
			Mode: "new_session", State: "assigning", HumanActor: "person", CreatedAt: now, UpdatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	id := "7bd00000-0000-4000-8000-000000000010"
	h := Handoff{ID: id, State: HandoffDelivered, ProjectDir: project, FromSession: rootConversation,
		Assistant: "claude", BoardItems: []string{owned, other, closed, verifying, pending},
		Opened: &openedSession{TerminalID: "%2"}}
	if err := b.createOpened(ctx, store.TableHandoffs, id, HandoffDelivered, h, "handoff.delivered", now); err != nil {
		t.Fatal(err)
	}
	to := "7bd00000-0000-4000-8000-000000000020"
	read := reading{sessions: map[string]session.Session{"%2": {
		ID: "%2", Assistant: session.AssistantClaude, ConversationID: to, State: session.StateIdle,
		Activity: session.Activity{Reason: session.ActivityNoRecord}}}}
	if n := b.tendHandoffBoards(ctx, read); n != 0 {
		t.Fatalf("idle receiver moved %d handoffs before reading", n)
	}
	read.sessions["%2"] = session.Session{ID: "%2", Assistant: session.AssistantClaude,
		ConversationID: to, State: session.StateIdle, Activity: session.Activity{At: now}}
	if n := b.tendHandoffBoards(ctx, read); n != 1 {
		t.Fatalf("working receiver moved %d handoffs", n)
	}
	if n := b.tendHandoffBoards(ctx, read); n != 0 {
		t.Fatalf("replay moved %d handoffs", n)
	}
	for _, tc := range []struct{ id, owner string }{{owned, to}, {other, rootConversation}, {closed, rootConversation}, {verifying, to}, {pending, rootConversation}} {
		item, err := b.Store.WorkV2Item(ctx, tc.id)
		if err != nil || item.OwnerSession != tc.owner {
			t.Fatalf("item %s owner=%q err=%v", tc.id, item.OwnerSession, err)
		}
		if tc.id == verifying && item.Phase != work.PhaseImplementing {
			t.Fatalf("old verification survived a new owner: %s", item.Phase)
		}
		assignments, err := b.Store.WorkV2Assignments(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, a := range assignments {
			if a.State == "active" {
				active++
				if a.SessionID != tc.owner {
					t.Fatalf("item %s active owner=%s", tc.id, a.SessionID)
				}
			}
		}
		if active != 1 {
			t.Fatalf("item %s has %d active assignments", tc.id, active)
		}
	}
	stored, err := b.HandoffByID(ctx, id)
	if err != nil || stored.BoardTransferredAt == 0 {
		t.Fatalf("handoff transfer marker: %+v %v", stored, err)
	}
}

func TestFailedHandoffCannotTransferBoardWork(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "7bd00000-0000-4000-8000-000000000030"
	h := Handoff{ID: id, State: HandoffSpawnFailed, BoardItems: []string{"7bd00000-0000-4000-8000-000000000031"},
		Opened: &openedSession{TerminalID: "%2"}}
	if err := b.createOpened(ctx, store.TableHandoffs, id, HandoffSpawnFailed, h, "handoff.spawn_failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	to := "7bd00000-0000-4000-8000-000000000020"
	read := reading{sessions: map[string]session.Session{"%2": {
		ID: "%2", Assistant: session.AssistantClaude, ConversationID: to, State: session.StateWorking}}}
	if n := b.tendHandoffBoards(ctx, read); n != 0 {
		t.Fatalf("failed handoff moved %d Board items", n)
	}
}
