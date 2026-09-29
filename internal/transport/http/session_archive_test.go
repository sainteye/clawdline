package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func archivePane(conversation string) *pane {
	return &pane{s: session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle, CWD: "/nowhere/quiet-project", ConversationID: conversation, Label: "quiet work",
		TTY: "ttys004", PID: os.Getpid()}}
}

func archiveServer(t *testing.T, p *pane) *Server {
	t.Helper()
	s := paneServer(t, p)
	s.swift = swiftstore.Open(t.TempDir())
	s.icons = &icon.Registry{}
	s.archive = &app.SessionArchive{Store: s.store, Title: s.sessionDisplayLabel}
	return s
}

func archiveCall(t *testing.T, s *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if !s.archivedRoute(rec, req) {
		t.Fatalf("%s %s was not one of the archive routes", req.Method, req.URL.Path)
	}
	return rec
}

func archivedNow(t *testing.T, s *Server) contract.ArchivedSessions {
	t.Helper()
	rec := archiveCall(t, s, asDevice(httptest.NewRequest(http.MethodGet, archivedPath, nil), "viewer", false))
	var got contract.ArchivedSessions
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	return got
}

// An archive retried under its key closes once and is answered with the
// first answer: the close's shape and the row it recorded.
func TestAnArchiveRetriedUnderItsKeyClosesOnce(t *testing.T) {
	p := archivePane("conv-quiet")
	s := archiveServer(t, p)
	first := act(t, s, "archive", "%4", "archive-1", `{}`)
	again := act(t, s, "archive", "%4", "archive-1", `{}`)
	if first.Code != 200 || again.Code != 200 {
		t.Fatalf("answers %d %s / %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	if got := p.done(); len(got) != 1 || got[0] != "close" {
		t.Fatalf("terminal saw %q; the retry closed a second time", got)
	}
	if again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("the retry was not the first answer: %q vs %q", again.Body, first.Body)
	}
	var answer contract.ArchiveAnswer
	if err := json.Unmarshal(first.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !answer.OK || answer.ID != "%4" || answer.Action != "archived" || answer.Forced ||
		answer.Archived.ConversationID != "conv-quiet" || answer.Archived.CWD != "/nowhere/quiet-project" ||
		answer.Archived.PlaceLabel != "quiet-project" || answer.Archived.Title == "" || answer.Archived.ArchivedAt == 0 || answer.Archived.Icon == nil {
		t.Fatalf("answer = %+v", answer)
	}
	list := archivedNow(t, s)
	if len(list.Sessions) != 1 || list.Sessions[0].ConversationID != "conv-quiet" || list.Sessions[0].Icon == nil {
		t.Fatalf("list = %+v", list)
	}
}

// An archive names its key, needs a device that may send, and a session with
// no conversation is refused before anything is closed.
func TestAnArchiveNeedsAKeyASenderAndAConversation(t *testing.T) {
	p := archivePane("conv-quiet")
	s := archiveServer(t, p)
	if rec := act(t, s, "archive", "%4", "", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("with no key: %d %s", rec.Code, rec.Body)
	}
	req := asDevice(restorePost("/v1/sessions/%254/archive", "k", `{}`), "viewer", false)
	rec := httptest.NewRecorder()
	s.sessionAction(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a read-only device: %d %s", rec.Code, rec.Body)
	}
	nothing := archivePane("")
	s = archiveServer(t, nothing)
	if rec := act(t, s, "archive", "%4", "k2", `{"force":true}`); rec.Code != http.StatusConflict ||
		codeOf(t, rec) != app.ArchiveNoConversation {
		t.Fatalf("no conversation: %d %s", rec.Code, rec.Body)
	}
	if len(nothing.done()) != 0 || len(archivedNow(t, s).Sessions) != 0 {
		t.Fatalf("closed %q, listed %+v", nothing.done(), archivedNow(t, s))
	}
}

func TestArchiveUsesTheSameCurrentCloseEvidenceAsClose(t *testing.T) {
	p := archivePane("conv-quiet")
	s := archiveServer(t, p)
	stale := act(t, s, "archive", "%4", "archive-stale", `{"expected_closeability_version":"stale"}`)
	if stale.Code != http.StatusConflict || codeOf(t, stale) != "close_not_proven" || len(p.done()) != 0 {
		t.Fatalf("stale archive: %d %s, terminal=%q", stale.Code, stale.Body, p.done())
	}
	if _, err := s.workV2().CreateSessionTodos(context.Background(), app.NewSessionTodosV2{
		SessionID: "conv-quiet", Texts: []string{"finish the work"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	blocked := act(t, s, "archive", "%4", "archive-blocked", `{}`)
	if blocked.Code != http.StatusConflict || codeOf(t, blocked) != "close_blocked" || len(p.done()) != 0 {
		t.Fatalf("blocked archive: %d %s, terminal=%q", blocked.Code, blocked.Body, p.done())
	}
	current := s.sessionsPayloadFrom(context.Background(), s.freshReading(context.Background())).Sessions[0].Closeability.Version
	if _, err := s.workV2().CreateSessionTodos(context.Background(), app.NewSessionTodosV2{
		SessionID: "conv-quiet", Texts: []string{"newly assigned work"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	changed := act(t, s, "archive", "%4", "archive-old-force",
		fmt.Sprintf(`{"force":true,"expected_closeability_version":%q}`, current))
	if changed.Code != http.StatusConflict || codeOf(t, changed) != "close_not_proven" || len(p.done()) != 0 {
		t.Fatalf("new duty under an old forced decision: %d %s, terminal=%q", changed.Code, changed.Body, p.done())
	}
}

// The list is newest first and carries the persona only while this build
// has it.
func TestTheArchivedListIsNewestFirst(t *testing.T) {
	s := archiveServer(t, archivePane("x"))
	ctx := context.Background()
	for i, id := range []string{"older", "newest", "middle"} {
		at := map[string]int64{"older": 100, "newest": 300, "middle": 200}[id]
		row := store.ArchiveRow{ConversationID: id, Assistant: "claude", CWD: "/nowhere/p" + string(rune('a'+i)),
			Place: "place", Title: "t " + id, Persona: "wizard", ArchivedAt: time.Unix(at, 0)}
		if _, err := s.store.ArchiveSession(ctx, row, 500); err != nil {
			t.Fatal(err)
		}
	}
	list := archivedNow(t, s)
	var order []string
	for _, r := range list.Sessions {
		order = append(order, r.ConversationID)
		if r.Persona != "" {
			t.Fatalf("a persona this build does not have was listed: %+v", r)
		}
	}
	if strings.Join(order, ",") != "newest,middle,older" || list.Sessions[0].ArchivedAt != 300 {
		t.Fatalf("list = %+v", list)
	}
}

// A restore goes through the shared resume path: a row whose directory is no
// longer a place is answered place_unavailable and stays; a name with no row
// is not_archived; a retry is the first answer; an oversized list is refused
// whole.
func TestRestoringArchivedAnswersEachRowThroughTheSharedPath(t *testing.T) {
	s := archiveServer(t, archivePane("x"))
	ctx := context.Background()
	row := store.ArchiveRow{ConversationID: "0f1e2d3c-0000-4000-8000-000000000001", Assistant: "claude",
		CWD: "/nowhere/gone", Place: "place-gone", ArchivedAt: time.Unix(100, 0)}
	if _, err := s.store.ArchiveSession(ctx, row, 500); err != nil {
		t.Fatal(err)
	}
	body := `{"conversations":["` + row.ConversationID + `","never"]}`
	first := archiveCall(t, s, asSender(restorePost(archivedRestorePath, "r1", body), "phone"))
	again := archiveCall(t, s, asSender(restorePost(archivedRestorePath, "r1", body), "phone"))
	var got contract.RestoreArchivedAnswer
	if err := json.Unmarshal(first.Body.Bytes(), &got); err != nil || first.Code != http.StatusOK {
		t.Fatalf("%d %v %s", first.Code, err, first.Body)
	}
	if len(got.Results) != 2 || got.Results[0].Code != contract.RestoreArchivedCodePlaceUnavailable ||
		got.Results[1].Code != contract.RestoreArchivedCodeNotArchived || got.Results[0].OK {
		t.Fatalf("results = %+v", got.Results)
	}
	if again.Body.String() != first.Body.String() {
		t.Fatalf("the retry was not the first answer: %s", again.Body)
	}
	if list := archivedNow(t, s); len(list.Sessions) != 1 {
		t.Fatalf("a row that did not open was removed: %+v", list)
	}
	if rec := archiveCall(t, s, asSender(restorePost(archivedRestorePath, "", body), "phone")); rec.Code != http.StatusBadRequest {
		t.Fatalf("with no key: %d", rec.Code)
	}
	if rec := archiveCall(t, s, asDevice(restorePost(archivedRestorePath, "r2", body), "viewer", false)); rec.Code != http.StatusForbidden {
		t.Fatalf("a read-only device: %d", rec.Code)
	}
	names := make([]string, 21)
	for i := range names {
		names[i] = `"c` + string(rune('a'+i)) + `"`
	}
	big := `{"conversations":[` + strings.Join(names, ",") + `]}`
	if rec := archiveCall(t, s, asSender(restorePost(archivedRestorePath, "r3", big), "phone")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "archive_batch_too_large") {
		t.Fatalf("21 names: %d %s", rec.Code, rec.Body)
	}
}
