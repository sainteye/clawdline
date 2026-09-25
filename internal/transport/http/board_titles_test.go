package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

type fixedInventory struct{ inv session.Inventory }

func (p fixedInventory) Scan(context.Context) (session.Inventory, error) { return p.inv, nil }

// boardTitleFixture stores one Board item whose Root Assignment opened a
// conversation with mode `mode`, and a live row holding that conversation (or
// another one) in a terminal the assignment never opened.
func boardTitleFixture(t *testing.T, mode string) (*Server, context.Context) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)

	// The executor tab the broker opened for the assignment: gone after the
	// crash, and its id is not the one the resumed conversation now runs in.
	a := orchestrator.RootAssignment{ID: "root-a", Assistant: "claude", ProjectDir: "/project-a",
		Label: "Board item title", State: "briefed", CreatedAt: at.Unix(), BriefedAt: at.Unix() + 5}
	body, _ := json.Marshal(a)
	var rec map[string]any
	_ = json.Unmarshal(body, &rec)
	rec["executor"] = map[string]any{"terminal_id": "OLD-TERMINAL", "backend": "iterm", "opened_at": at.Unix()}
	body, _ = json.Marshal(rec)
	if err := st.CreateOpened(ctx, store.TableRootAssignments, store.Opened{ID: a.ID, State: a.State,
		Record: body, CreatedAt: at, UpdatedAt: at}, nil); err != nil {
		t.Fatal(err)
	}
	item := work.ItemV2{ID: "10000000-0000-4000-8000-0000000000b1", ProjectID: "project-a", ProjectPath: "/project-a",
		Kind: work.KindFeature, Title: "Board item title", Description: "d", Phase: work.PhaseCreated,
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at, Cycle: 1, Version: 1}
	if err := st.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: "assignment-a", WorkID: item.ID, Mode: mode,
			SessionID: "resumed-conversation", Assistant: "claude", State: "active", HumanActor: "local",
			RootAssignment: a.ID, CreatedAt: at, UpdatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Dir: dir}
	return &Server{cfg: cfg, store: st, broker: &orchestrator.Broker{Store: st, Dir: dir}, icons: &icon.Registry{}}, ctx
}

func liveRowLabel(t *testing.T, s *Server, ctx context.Context, row session.Session) string {
	t.Helper()
	s.inventory = app.Inventory{Process: fixedInventory{session.Inventory{Sessions: []session.Session{row},
		Complete: true, Provenance: "ps", Sources: map[string]bool{"ps": true}}}}
	snap := s.sessionsPayloadFrom(ctx, s.inventory.Read(ctx))
	if len(snap.Sessions) != 1 {
		t.Fatalf("the payload carried %d rows", len(snap.Sessions))
	}
	if info := s.sessionDisplayLabel(ctx, row); info != snap.Sessions[0].Label {
		t.Errorf("the info route says %q and the list says %q", info, snap.Sessions[0].Label)
	}
	return snap.Sessions[0].Label
}

func resumedRow(conversation string) session.Session {
	return session.Session{ID: "NEW-TERMINAL", TTY: "ttys031", Assistant: session.AssistantClaude,
		Backend: session.BackendITerm, State: session.StateIdle, PID: 999999,
		ConversationID: conversation, Rungs: session.LabelRungs{Thread: "Root assignment 905fbc68"}}
}

// A conversation a Board item opened, resumed into a new tab after a crash,
// keeps the Board's title in the live list: its terminal id, pid and start
// are all new, so only the conversation still names it.
func TestAResumedBoardSessionKeepsItsTitleInTheLiveList(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	if got := liveRowLabel(t, s, ctx, resumedRow("resumed-conversation")); got != "Board item title" {
		t.Fatalf("the resumed row is named %q; its Root Assignment says %q", got, "Board item title")
	}
}

// A terminal reused by a different conversation borrows nothing: the
// conversation it runs is not the one the assignment opened.
func TestAnotherConversationInTheTerminalDoesNotBorrowTheBoardTitle(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	row := resumedRow("another-conversation")
	row.ID = "OLD-TERMINAL"
	if got := liveRowLabel(t, s, ctx, row); got != "Root assignment 905fbc68" {
		t.Fatalf("a different conversation in the reused terminal is named %q", got)
	}
	// And a row whose conversation is unknown is never lent a label.
	if got := liveRowLabel(t, s, ctx, resumedRow("")); got == "Board item title" {
		t.Fatalf("a row with no conversation id was named %q", got)
	}
}

// Work handed to a session that already had its own identity does not rename
// it, as in the resume picker.
func TestAnExistingSessionAssignmentDoesNotRenameTheLiveRow(t *testing.T) {
	s, ctx := boardTitleFixture(t, "existing_session")
	if got := liveRowLabel(t, s, ctx, resumedRow("resumed-conversation")); got != "Root assignment 905fbc68" {
		t.Fatalf("an existing_session assignment renamed the row to %q", got)
	}
}

// A name the person typed still outranks the Board's.
func TestAManualTitleOutranksTheBoardTitle(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	row := resumedRow("resumed-conversation")
	if _, err := s.saveSessionTitle(row, "Typed by the person", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := liveRowLabel(t, s, ctx, row); got != "Typed by the person" {
		t.Fatalf("the manual title lost to %q", got)
	}
}
