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
	t.Run("full board transfers to the witnessed receiver", func(t *testing.T) {
		f := newHandoffMessageBoardFixture(t)
		archived := f.create(f.project.ID, f.lead, "Archived board")
		f.change(f.project.ID, f.lead, sb.Actor{}, sb.Command{Action: "archive", Board: archived}, "")
		board := f.create(f.project.ID, f.lead, "Active board")
		existing := f.create(f.project.ID, f.next, "Receiver's prior board")
		f.change(f.project.ID, f.lead, sb.Actor{}, sb.Command{Action: "summary", Board: board, Text: "Keep this summary"}, "")
		f.change(f.project.ID, f.lead, sb.Actor{}, sb.Command{Action: "post", Board: board, Text: "Keep this post"}, "post")
		for i := 1; i < sb.MaxMembers; i++ {
			actor := sb.Actor{ID: fmt.Sprintf("claude:member-%d", i), Label: "Member"}
			f.change(f.project.ID, actor, sb.Actor{}, sb.Command{Action: "join", Board: board}, "")
		}
		f.open(board)

		f.bindReceiver()
		f.receiver.Activity.At = f.now.Add(-time.Second)
		if n := f.read(); n != 0 {
			t.Fatalf("activity from before the briefing moved %d handoffs", n)
		}
		f.receiver.Activity.At = f.now
		f.receiverStart = f.now
		if n := f.read(); n != 0 {
			t.Fatalf("a replacement process moved %d handoffs", n)
		}
		f.receiverStart = f.now.Add(-time.Second)
		f.receiveOnce()

		got := f.page(f.project.ID, f.next.ID, board)
		if got.Board.Lead != f.next.ID || len(got.Board.Members) != sb.MaxMembers || got.Board.Summary != "Keep this summary" ||
			len(got.Posts) != 1 || got.Posts[0].Author != f.lead || got.Posts[0].Text != "Keep this post" {
			t.Fatalf("transferred Lead or history: %+v", got)
		}
		f.change(f.project.ID, f.next, sb.Actor{}, sb.Command{Action: "summary", Board: board, Text: "Next decision"}, "")
		f.denied(f.lead, board)
		f.denied(f.next, existing)
		if got := f.page("", "", existing).Board; got.Lead != "" || len(got.Members) != 0 {
			t.Fatalf("the receiver's previous board retained its role or membership: %+v", got)
		}
		if got := f.page(f.project.ID, f.lead.ID, archived).Board; got.ArchivedAt == 0 || got.Lead != f.lead.ID || len(got.Members) != 1 {
			t.Fatalf("archived membership or Lead changed: %+v", got)
		}
	})

	t.Run("manual Lead transfer survives an already joined receiver", func(t *testing.T) {
		f := newHandoffMessageBoardFixture(t)
		peer := sb.Actor{ID: "claude:peer", Label: "Peer"}
		board := f.create(f.project.ID, f.lead, "Manual transfer")
		for _, actor := range []sb.Actor{peer, f.next} {
			f.change(f.project.ID, actor, sb.Actor{}, sb.Command{Action: "join", Board: board}, "")
		}
		f.open(board)
		f.change(f.project.ID, f.lead, peer, sb.Command{Action: "transfer", Board: board, Target: peer.ID}, "")
		f.receiveOnce()

		if got := f.page(f.project.ID, f.next.ID, board).Board; got.Lead != peer.ID || len(got.Members) != 2 {
			t.Fatalf("ordinary membership or manual Lead transfer: %+v", got)
		}
		f.denied(f.lead, board)
	})

	t.Run("moving after capture does not enroll the receiver", func(t *testing.T) {
		f := newHandoffMessageBoardFixture(t)
		board := f.create(f.project.ID, f.lead, "Captured board")
		existing := f.create(f.project.ID, f.next, "Receiver's board")
		f.open(board)
		late := f.create(f.project.ID, f.lead, "Joined after starting handoff")
		f.receiveOnce()

		f.denied(f.next, board)
		f.denied(f.next, late)
		if got := f.page(f.project.ID, f.lead.ID, late).Board; got.Lead != f.lead.ID || len(got.Members) != 1 {
			t.Fatalf("later membership changed: %+v", got)
		}
		if got := f.page(f.project.ID, f.next.ID, existing).Board; got.Lead != f.next.ID || len(got.Members) != 1 {
			t.Fatalf("the receiver's board changed: %+v", got)
		}
	})

	t.Run("membership from a different project is excluded", func(t *testing.T) {
		f := newHandoffMessageBoardFixture(t)
		other, ok := projects.ResolveScope(t.TempDir())
		if !ok {
			t.Fatal("could not resolve the other project")
		}
		board := f.create(other.ID, f.lead, "Other project")
		f.open("")
		f.bindReceiver()
		if n := f.read(); n != 0 {
			t.Fatalf("another project's membership moved: %d handoffs", n)
		}
		if got := f.page(other.ID, f.lead.ID, board).Board; got.Lead != f.lead.ID || len(got.Members) != 1 {
			t.Fatalf("another project's board changed: %+v", got)
		}
	})
}

const handoffMessageBoardReceiver = "7bd00000-0000-4000-8000-000000000045"

type handoffMessageBoardFixture struct {
	t             *testing.T
	b             *Broker
	ctx           context.Context
	project       projects.Scope
	now           time.Time
	receiverStart time.Time
	receiver      session.Session
	lead, next    sb.Actor
}

func newHandoffMessageBoardFixture(t *testing.T) *handoffMessageBoardFixture {
	t.Helper()
	b, ctx := newTestBroker(t)
	project, ok := projects.ResolveScope(t.TempDir())
	if !ok {
		t.Fatal("could not resolve the handoff project")
	}
	now := time.Now().Truncate(time.Second)
	f := &handoffMessageBoardFixture{
		t: t, b: b, ctx: ctx, project: project, now: now, receiverStart: now.Add(-time.Second),
		lead: sb.Actor{ID: "claude:" + rootConversation, Label: "Original Lead"},
		next: sb.Actor{ID: "codex:" + handoffMessageBoardReceiver, Label: "Receiving Session"},
		receiver: session.Session{ID: "%2", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
			PID: 41, CWD: project.Key, Observation: session.Observation{Freshness: session.FreshnessCurrent}},
	}
	sender := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation, CWD: project.Key}
	b.Live = func(context.Context) []session.Session { return []session.Session{sender, f.receiver} }
	b.Clock = func() time.Time { return now }
	b.ProcessStart = func(int) time.Time { return f.receiverStart }
	b.Launcher = &openingLauncher{pane: f.receiver.ID}
	b.Type = (&typedKeys{}).Type
	return f
}

func (f *handoffMessageBoardFixture) create(scope string, actor sb.Actor, name string) string {
	f.t.Helper()
	return f.change(scope, actor, sb.Actor{}, sb.Command{Action: "create", Name: name, Text: "Share decisions"}, name)
}

func (f *handoffMessageBoardFixture) change(scope string, actor, target sb.Actor, cmd sb.Command, key string) string {
	f.t.Helper()
	out, err := f.b.Store.ChangeSessionBoard(f.ctx, scope, actor, target, cmd, key, f.now)
	if err != nil {
		f.t.Fatalf("%s: %v", cmd.Action, err)
	}
	return out.ID
}

func (f *handoffMessageBoardFixture) open(board string) {
	f.t.Helper()
	yes := true
	summary := "## Goal\nCarry the review forward.\n\n## Verified decisions\nUse the current build.\n\n## Blockers\nNone.\n\n## Evidence\n- docs/acceptance.md\n\n## Next step\nRead the evidence.\n"
	request := HandoffRequest{ID: "7bd00000-0000-4000-8000-000000000046", ProjectDir: f.project.Key,
		FromSession: rootConversation, Assistant: "codex", Plain: &yes, Milestone: &yes, Summary: &summary}
	h, _, err := f.b.OpenHandoff(f.ctx, request)
	if err != nil || h.State != HandoffDelivered {
		f.t.Fatalf("handoff: state=%s failure=%s err=%v", h.State, h.Failure, err)
	}
	if board == "" {
		if h.SessionBoards != nil {
			f.t.Fatalf("captured a board outside the handoff project: %+v", h.SessionBoards)
		}
		return
	}
	if h.SessionBoards == nil || len(h.SessionBoards.Boards) != 1 || h.SessionBoards.Boards[0] != board {
		f.t.Fatalf("handoff did not capture exactly the sender's active board: %+v", h.SessionBoards)
	}
}

func (f *handoffMessageBoardFixture) bindReceiver() {
	f.receiver.ConversationID = handoffMessageBoardReceiver
	f.receiver.Label = f.next.Label
	f.receiver.Activity = session.Activity{At: f.now}
}

func (f *handoffMessageBoardFixture) read() int {
	return f.b.tendHandoffBoards(f.ctx, reading{sessions: map[string]session.Session{f.receiver.ID: f.receiver}})
}

func (f *handoffMessageBoardFixture) receiveOnce() {
	f.t.Helper()
	f.bindReceiver()
	if n := f.read(); n != 1 {
		f.t.Fatalf("bound receiver moved %d handoffs", n)
	}
	if n := f.read(); n != 0 {
		f.t.Fatalf("the same handoff ran again: %d", n)
	}
}

func (f *handoffMessageBoardFixture) page(scope, actor, id string) sb.Page {
	f.t.Helper()
	out, err := f.b.Store.SessionBoardPage(f.ctx, scope, actor, id, 0, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *handoffMessageBoardFixture) denied(actor sb.Actor, board string) {
	f.t.Helper()
	if _, err := f.b.Store.SessionBoardPage(f.ctx, f.project.ID, actor.ID, board, 0, 0); !errors.Is(err, sb.ErrDenied) {
		f.t.Fatalf("actor %s still has membership on %s: %v", actor.ID, board, err)
	}
}
