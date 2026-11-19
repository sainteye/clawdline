package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
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

func TestSessionMessageBoardsFollowAnExactHandoffReceiver(t *testing.T) {
	b, ctx := newTestBroker(t)
	project, _ := projects.ResolveScope(t.TempDir())
	other, _ := projects.ResolveScope(t.TempDir())
	now := time.Now().Truncate(time.Second)
	b.Clock = func() time.Time { return now }
	receiverStart := now.Add(-time.Second)
	b.ProcessStart = func(int) time.Time { return receiverStart }
	b.Launcher = &openingLauncher{pane: "%2"}
	b.Type = (&typedKeys{}).Type
	receiver := session.Session{ID: "%2", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		PID: 41, CWD: project.Key, Observation: session.Observation{Freshness: session.FreshnessCurrent}}
	sender := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation, CWD: project.Key}
	b.Live = func(context.Context) []session.Session { return []session.Session{sender, receiver} }
	lead := sb.Actor{ID: "claude:" + rootConversation, Label: "Original lead"}
	peer := sb.Actor{ID: "claude:peer", Label: "Peer"}
	nextConversation := "7bd00000-0000-4000-8000-000000000045"
	next := sb.Actor{ID: "codex:" + nextConversation, Label: "Receiving Session"}
	change := func(scope string, actor, target sb.Actor, cmd sb.Command, key string) string {
		t.Helper()
		out, err := b.Store.ChangeSessionBoard(ctx, scope, actor, target, cmd, key, now)
		if err != nil {
			t.Fatalf("%s: %v", cmd.Action, err)
		}
		return out.ID
	}
	create := func(scope string, actor sb.Actor, name string) string {
		return change(scope, actor, sb.Actor{}, sb.Command{Action: "create", Name: name, Text: "Share decisions"}, name)
	}
	board := create(project.ID, lead, "Active board")
	memberBoard := create(project.ID, peer, "Member board")
	manual := create(project.ID, lead, "Manual transfer")
	archived := create(project.ID, lead, "Archived board")
	crossProject := create(other.ID, lead, "Other project")
	change(project.ID, lead, sb.Actor{}, sb.Command{Action: "summary", Board: board, Text: "Keep this summary"}, "")
	change(project.ID, lead, sb.Actor{}, sb.Command{Action: "post", Board: board, Text: "Keep this post"}, "post")
	for i := 1; i < sb.MaxMembers; i++ {
		actor := sb.Actor{ID: fmt.Sprintf("claude:member-%d", i), Label: "Member"}
		change(project.ID, actor, sb.Actor{}, sb.Command{Action: "join", Board: board}, "")
	}
	change(project.ID, lead, sb.Actor{}, sb.Command{Action: "join", Board: memberBoard}, "")
	change(project.ID, next, sb.Actor{}, sb.Command{Action: "join", Board: memberBoard}, "")
	change(project.ID, peer, sb.Actor{}, sb.Command{Action: "join", Board: manual}, "")
	change(project.ID, lead, sb.Actor{}, sb.Command{Action: "archive", Board: archived}, "")

	yes := true
	summary := "## Goal\nCarry the review forward.\n\n## Verified decisions\nUse the current build.\n\n## Blockers\nNone.\n\n## Evidence\n- docs/acceptance.md\n\n## Next step\nRead the evidence.\n"
	request := HandoffRequest{ID: "7bd00000-0000-4000-8000-000000000046", ProjectDir: project.Key,
		FromSession: rootConversation, Assistant: "codex", Plain: &yes, Milestone: &yes, Summary: &summary}
	h, _, err := b.OpenHandoff(ctx, request)
	if err != nil || h.State != HandoffDelivered {
		t.Fatalf("handoff: state=%s failure=%s err=%v", h.State, h.Failure, err)
	}
	late := create(project.ID, lead, "Joined after starting handoff")
	change(project.ID, lead, peer, sb.Command{Action: "transfer", Board: manual, Target: peer.ID}, "")

	read := func() int {
		return b.tendHandoffBoards(ctx, reading{sessions: map[string]session.Session{receiver.ID: receiver}})
	}
	receiver.ConversationID, receiver.Label = nextConversation, next.Label
	receiver.Activity = session.Activity{At: now.Add(-time.Second)}
	if n := read(); n != 0 {
		t.Fatalf("activity from before the briefing moved %d handoffs", n)
	}
	receiver.Activity.At = now
	receiverStart = now
	if n := read(); n != 0 {
		t.Fatalf("a replacement process moved %d handoffs", n)
	}
	receiverStart = now.Add(-time.Second)
	if n := read(); n != 1 {
		t.Fatalf("bound receiver moved %d handoffs", n)
	}
	if n := read(); n != 0 {
		t.Fatalf("the same handoff ran again: %d", n)
	}
	page := func(scope, actor, id string) sb.Page {
		t.Helper()
		out, err := b.Store.SessionBoardPage(ctx, scope, actor, id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := page(project.ID, next.ID, board)
	if got.Board.Lead != next.ID || len(got.Board.Members) != sb.MaxMembers || got.Board.Summary != "Keep this summary" ||
		len(got.Posts) != 1 || got.Posts[0].Author != lead {
		t.Fatalf("transferred Lead or history: %+v", got)
	}
	change(project.ID, next, sb.Actor{}, sb.Command{Action: "summary", Board: board, Text: "Next decision"}, "")
	if _, err := b.Store.SessionBoardPage(ctx, project.ID, lead.ID, board, 0, 0); !errors.Is(err, sb.ErrDenied) {
		t.Fatalf("the former Lead still has membership: %v", err)
	}
	for _, id := range []string{memberBoard, manual} {
		got := page(project.ID, next.ID, id)
		if got.Board.Lead != peer.ID || len(got.Board.Members) != 2 {
			t.Fatalf("ordinary membership or manual Lead transfer: %+v", got.Board)
		}
	}
	for _, id := range []string{archived, late, crossProject} {
		if got := page("", "", id).Board; got.Lead != lead.ID || len(got.Members) != 1 {
			t.Fatalf("an ineligible board changed: %+v", got)
		}
	}
}
