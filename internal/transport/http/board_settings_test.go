package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boardstore "github.com/sainteye/clawdline-go/internal/adapters/board"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// D37: the board's settings are a row in the store. The old document is
// carried over once and never written; a command's receipt is the store's,
// and a resend is answered with the revision that command produced — the old
// document answered with whatever revision the board had reached since.
func TestBoardSettingsMovedIntoTheStoreAndReplayTheirOwnRevision(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "project-board.json")
	old := `{"schemaVersion":2,"revision":3,"enabled":false,"narrativeConsent":"claude","presentationEpoch":4,"updatedAt":1.5,"receipts":[]}`
	if err := os.WriteFile(doc, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	boardByServer.Store(s, &boardDeps{legacy: boardstore.OpenLegacy(filepath.Join(dir, "absent.json")),
		settings: boardstore.OpenSettings(dir)})
	ctx := context.Background()
	got, err := s.boardSettings(ctx)
	if err != nil || got.Revision != 3 || got.Enabled || got.NarrativeConsent == nil || *got.NarrativeConsent != "claude" {
		t.Fatalf("read before the carry-over: %+v %v", got, err)
	}
	// A read writes nothing.
	if _, err := st.BoardSettings(ctx); !errors.Is(err, store.ErrNoBoardSettings) {
		t.Fatalf("a read carried the document over: %v", err)
	}
	post := func(body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/v1/board", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
		rec := httptest.NewRecorder()
		s.boardWrite(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	result := func(out map[string]any) (float64, bool) {
		r, _ := out["result"].(map[string]any)
		rev, _ := r["revision"].(float64)
		replayed, _ := r["replayed"].(bool)
		return rev, replayed
	}
	first := `{"operation":"set_enabled","requestId":"r1","expectedRevision":3,"enabled":true}`
	if code, out := post(first); code != 200 {
		t.Fatalf("first: %d %v", code, out)
	} else if rev, replayed := result(out); rev != 4 || replayed {
		t.Fatalf("first answered revision %v replayed %v", rev, replayed)
	}
	if code, out := post(`{"operation":"set_enabled","requestId":"r2","expectedRevision":4,"enabled":false}`); code != 200 {
		t.Fatalf("second: %d %v", code, out)
	}
	// The first command again: its own revision, not the board's.
	code, out := post(first)
	if rev, replayed := result(out); code != 200 || rev != 4 || !replayed {
		t.Fatalf("the resend answered %d revision %v replayed %v", code, rev, replayed)
	}
	if now, _ := s.boardSettings(ctx); now.Revision != 5 || now.Enabled {
		t.Fatalf("the resend changed the board: %+v", now)
	}
	if code, out := post(`{"operation":"set_enabled","requestId":"r1","expectedRevision":5,"enabled":true}`); code != 409 || out["error"] != "request_id_conflict" {
		t.Fatalf("a reused requestId: %d %v", code, out)
	}
	if code, out := post(`{"operation":"set_enabled","requestId":"r3","expectedRevision":1,"enabled":true}`); code != 409 || out["error"] != "revision_conflict" {
		t.Fatalf("a stale revision: %d %v", code, out)
	}
	// The old document was never written.
	if body, _ := os.ReadFile(doc); string(body) != old {
		t.Fatalf("the old document was rewritten: %s", body)
	}
}

// A document that exists and cannot be read is not a fresh board: the
// person's setting is unknown, and the answer says so rather than carrying
// over "enabled".
func TestAnUnreadableOldDocumentIsNotAFreshBoard(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project-board.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	boardByServer.Store(s, &boardDeps{legacy: boardstore.OpenLegacy(filepath.Join(dir, "absent.json")),
		settings: boardstore.OpenSettings(dir)})
	_, err = s.boardSettings(context.Background())
	var refusal boardstore.Refusal
	if !errors.As(err, &refusal) || refusal.Status != 503 {
		t.Fatalf("an unreadable document answered %v", err)
	}
	if err := s.carryBoardSettings(context.Background()); !errors.As(err, &refusal) || refusal.Status != 503 {
		t.Fatalf("carrying an unreadable document answered %v", err)
	}
	if _, err := st.BoardSettings(context.Background()); !errors.Is(err, store.ErrNoBoardSettings) {
		t.Fatalf("something was carried over from a document nobody could read: %v", err)
	}
}
