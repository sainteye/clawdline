package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		Kind: work.KindFeature, Title: "Board item title", Description: "d", Phase: work.PhaseAssigned,
		OwnerSession:     "resumed-conversation",
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at, Cycle: 1, Version: 1}
	if err := st.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: "assignment-a", WorkID: item.ID, Mode: mode,
			SessionID: "resumed-conversation", TerminalID: "OLD-TERMINAL", Assistant: "claude", State: "active", HumanActor: "local",
			RootAssignment: a.ID, CreatedAt: at, UpdatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Dir: dir}
	return &Server{cfg: cfg, store: st, broker: &orchestrator.Broker{Store: st, Dir: dir}, icons: &icon.Registry{}}, ctx
}

// liveRowLabels builds the session list over these rows, all on screen at
// once, and answers each row's name by terminal id. The info route, which
// asks for one row alone, must say the same.
func liveRowLabels(t *testing.T, s *Server, ctx context.Context, rows ...session.Session) map[string]string {
	t.Helper()
	s.inventory = app.Inventory{Process: fixedInventory{session.Inventory{Sessions: rows,
		Complete: true, Provenance: "ps", Sources: map[string]bool{"ps": true}}}}
	snap := s.sessionsPayloadFrom(ctx, s.inventory.Read(ctx))
	if len(snap.Sessions) != len(rows) {
		t.Fatalf("the payload carried %d rows of %d", len(snap.Sessions), len(rows))
	}
	out := map[string]string{}
	for _, row := range snap.Sessions {
		out[row.ID] = row.Label
	}
	for _, row := range rows {
		if info := s.sessionDisplayLabel(ctx, row); info != out[row.ID] {
			t.Errorf("the info route says %q and the list says %q", info, out[row.ID])
		}
	}
	return out
}

func liveRowLabel(t *testing.T, s *Server, ctx context.Context, row session.Session) string {
	t.Helper()
	return liveRowLabels(t, s, ctx, row)[row.ID]
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

// A terminal reused by a different conversation borrows nothing, even while
// the conversation the assignment opened is on screen in another tab: the
// label follows the conversation, not the assistant or the terminal.
func TestAnotherConversationInTheTerminalDoesNotBorrowTheBoardTitle(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	reused := resumedRow("another-conversation")
	reused.ID, reused.TTY = "OLD-TERMINAL", "ttys032"
	unknown := resumedRow("")
	unknown.ID, unknown.TTY = "THIRD-TERMINAL", "ttys033"
	got := liveRowLabels(t, s, ctx, resumedRow("resumed-conversation"), reused, unknown)
	if got["NEW-TERMINAL"] != "Board item title" {
		t.Errorf("the resumed row is named %q", got["NEW-TERMINAL"])
	}
	if got["OLD-TERMINAL"] != "Root assignment 905fbc68" {
		t.Errorf("a different conversation in the reused terminal is named %q", got["OLD-TERMINAL"])
	}
	// A row whose conversation is unknown is never lent a label.
	if got["THIRD-TERMINAL"] == "Board item title" {
		t.Errorf("a row with no conversation id was named %q", got["THIRD-TERMINAL"])
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

func TestOwningRootNamesItsSessionAndKeepsTheNameAfterResume(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	path := "/v1/work/v2/agent/items/10000000-0000-4000-8000-0000000000b1/session-name"
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path,
		`{"session_id":"resumed-conversation","title":"Repair Session naming"}`, "name-one"))
	if rec.Code != http.StatusOK {
		t.Fatalf("name: %d %s", rec.Code, rec.Body)
	}
	if got := liveRowLabel(t, s, ctx, resumedRow("resumed-conversation")); got != "Repair Session naming" {
		t.Fatalf("resumed label = %q", got)
	}
}

func nameBoardRoot(s *Server, title, key string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost,
		"/v1/work/v2/agent/items/10000000-0000-4000-8000-0000000000b1/session-name",
		`{"session_id":"resumed-conversation","title":"`+title+`"}`, key))
	return rec
}

func TestRootNamingIsFirstWriteOnlyAndChecksCurrentOwnershipOnEveryRequest(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	if rec := nameBoardRoot(s, "Repair naming", "first"); rec.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	if rec := nameBoardRoot(s, "Repair naming", "another-key"); rec.Code != http.StatusOK {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}
	if rec := nameBoardRoot(s, "A different name", "different"); rec.Code != http.StatusConflict || codeOf(t, rec) != "session_already_named" {
		t.Fatalf("different name: %d %s", rec.Code, rec.Body)
	}
	if err := s.store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		item, err := tx.Item("10000000-0000-4000-8000-0000000000b1")
		if err != nil {
			return err
		}
		assignment, err := tx.ActiveAssignment(item.ID)
		if err != nil {
			return err
		}
		assignment.State = "released"
		if err := tx.UpdateAssignment(assignment); err != nil {
			return err
		}
		next := item
		next.OwnerSession = "another-conversation"
		return tx.PutItem(item, next, "item.reassigned", "local", `{}`)
	}); err != nil {
		t.Fatal(err)
	}
	if rec := nameBoardRoot(s, "Repair naming", "first"); rec.Code != http.StatusConflict || codeOf(t, rec) != "not_item_owner" {
		t.Fatalf("old request after reassignment: %d %s", rec.Code, rec.Body)
	}
	if got := liveRowLabel(t, s, ctx, resumedRow("resumed-conversation")); got != "Repair naming" {
		t.Fatalf("the original conversation lost its name: %q", got)
	}
}

func TestRootNamingRejectsInvalidNamesAndExistingSessionAssignments(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	for _, tc := range []struct{ title, code string }{
		{"   ", "invalid_title"},
		{strings.Repeat("界", 67), "invalid_title"},
	} {
		rec := nameBoardRoot(s, tc.title, "invalid-"+tc.title)
		if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != tc.code {
			t.Fatalf("invalid %q: %d %s", tc.title, rec.Code, rec.Body)
		}
	}
	invalidUTF8 := httptest.NewRequest(http.MethodPost,
		"/v1/work/v2/agent/items/10000000-0000-4000-8000-0000000000b1/session-name",
		strings.NewReader("{\"session_id\":\"resumed-conversation\",\"title\":\"\xff\"}"))
	invalidUTF8 = invalidUTF8.WithContext(agentWorkV2Request(http.MethodPost, "/", `{}`, "invalid-utf8").Context())
	bad := httptest.NewRecorder()
	s.workV2Route(bad, invalidUTF8)
	if bad.Code != http.StatusBadRequest || codeOf(t, bad) != "invalid_title" {
		t.Fatalf("invalid UTF-8: %d %s", bad.Code, bad.Body)
	}
	if got := liveRowLabel(t, s, ctx, resumedRow("resumed-conversation")); got != "Board item title" {
		t.Fatalf("invalid names changed the row: %q", got)
	}
	other, _ := boardTitleFixture(t, "existing_session")
	if rec := nameBoardRoot(other, "Should not apply", "existing"); rec.Code != http.StatusConflict || codeOf(t, rec) != "not_item_owner" {
		t.Fatalf("existing Session: %d %s", rec.Code, rec.Body)
	}
}

func TestRootNamingRejectsAMismatchedRootRecord(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	_, err := s.store.UpdateOpened(ctx, store.TableRootAssignments, "root-a", func(o store.Opened) (*store.Opened, []store.Event, error) {
		var root orchestrator.RootAssignment
		if err := json.Unmarshal(o.Record, &root); err != nil {
			return nil, nil, err
		}
		root.Assistant = "codex"
		o.Record, _ = json.Marshal(root)
		return &o, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := nameBoardRoot(s, "Wrong Root", "wrong-root"); rec.Code != http.StatusConflict || codeOf(t, rec) != "root_assignment_mismatch" {
		t.Fatalf("mismatched Root: %d %s", rec.Code, rec.Body)
	}
	_, err = s.store.UpdateOpened(ctx, store.TableRootAssignments, "root-a", func(o store.Opened) (*store.Opened, []store.Event, error) {
		var root orchestrator.RootAssignment
		if err := json.Unmarshal(o.Record, &root); err != nil {
			return nil, nil, err
		}
		root.Assistant, root.ID = "claude", "another-root"
		o.Record, _ = json.Marshal(root)
		return &o, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := nameBoardRoot(s, "Wrong Root", "wrong-id"); rec.Code != http.StatusConflict || codeOf(t, rec) != "root_assignment_mismatch" {
		t.Fatalf("mismatched Root id: %d %s", rec.Code, rec.Body)
	}
}

func TestExplicitRenameAndManualTitleOutrankAnAgentNamedBoardRoot(t *testing.T) {
	s, ctx := boardTitleFixture(t, "new_session")
	if rec := nameBoardRoot(s, "Repair naming", "first"); rec.Code != http.StatusOK {
		t.Fatalf("name: %d %s", rec.Code, rec.Body)
	}
	row := resumedRow("resumed-conversation")
	row.CustomTitle = "Person's /rename"
	if got := liveRowLabel(t, s, ctx, row); got != "Person's /rename" {
		t.Fatalf("/rename was covered by %q", got)
	}
	if _, err := s.saveSessionTitle(row, "Person's Clawdline title", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := liveRowLabel(t, s, ctx, row); got != "Person's Clawdline title" {
		t.Fatalf("manual title was covered by %q", got)
	}
}
