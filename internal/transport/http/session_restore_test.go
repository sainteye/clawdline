package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// restoreServer is a Server over its own store, whose previous boot had two
// conversations open, and which is now in boot "boot-after".
func restoreServer(t *testing.T, boot func(context.Context) (string, error)) *Server {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	then := time.Now().Add(-time.Hour)
	rows := []store.RestoreRow{
		{ConversationID: "conv-a", Assistant: "claude", CWD: "/nowhere/a", Place: "place-a", Title: "first", Backend: "tmux"},
		{ConversationID: "conv-b", Assistant: "codex", CWD: "/nowhere/b", Place: "place-b", Backend: "iterm"},
	}
	if err := st.RecordBoot(context.Background(), "boot-before", rows, then, 2); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir}, store: st}
	s.restore = &app.SessionRestore{Store: st, Boot: boot}
	return s
}

func bootAfter(context.Context) (string, error) { return "boot-after", nil }

func asSender(r *http.Request, device string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey{}, access{
		verdict: auth.Verdict{Allowed: true, Device: device, Caps: auth.NewCaps(auth.Read, auth.Send)},
	}))
}

func restoreCall(t *testing.T, s *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if !s.restorableRoute(rec, req) {
		t.Fatalf("%s %s was not one of the restore routes", req.Method, req.URL.Path)
	}
	return rec
}

func restorePost(path, key, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req
}

func TestRestorableIsReadByAnyPairedDevice(t *testing.T) {
	s := restoreServer(t, bootAfter)
	rec := restoreCall(t, s, asDevice(httptest.NewRequest(http.MethodGet, restorablePath, nil), "phone", false))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got contract.RestorableSessions
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.PreviousBootLastSeen == 0 || len(got.Sessions) != 2 {
		t.Fatalf("answer = %+v", got)
	}
	a := got.Sessions[0]
	if a.ConversationID != "conv-a" || a.Title != "first" || a.PlaceLabel != "a" || a.Place != "place-a" || a.LastSeen == 0 {
		t.Fatalf("first row = %+v (want conv-a first by id at an equal last_seen)", a)
	}
	if strings.Contains(rec.Body.String(), `"reason"`) {
		t.Fatalf("an available answer carried a reason: %s", rec.Body)
	}
}

func TestRestorableWithoutABootIdSaysSoAndOffersNothing(t *testing.T) {
	s := restoreServer(t, func(context.Context) (string, error) { return "", errors.New("unsupported") })
	rec := restoreCall(t, s, asDevice(httptest.NewRequest(http.MethodGet, restorablePath, nil), "phone", false))
	var got contract.RestorableSessions
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	if got.Available || got.Reason != contract.RestorableReasonBootUnknown || len(got.Sessions) != 0 {
		t.Fatalf("answer = %+v", got)
	}
}

func TestRestoreAndDismissNeedASenderAndAKey(t *testing.T) {
	s := restoreServer(t, bootAfter)
	for _, path := range []string{restorePath, dismissPath} {
		if rec := restoreCall(t, s, asDevice(restorePost(path, "k", `{"conversations":[]}`), "viewer", false)); rec.Code != http.StatusForbidden {
			t.Errorf("%s by a read-only device: %d %s", path, rec.Code, rec.Body)
		}
		if rec := restoreCall(t, s, asSender(restorePost(path, "", `{"conversations":[]}`), "phone")); rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), "Idempotency-Key") {
			t.Errorf("%s with no key: %d %s", path, rec.Code, rec.Body)
		}
		if rec := restoreCall(t, s, asSender(httptest.NewRequest(http.MethodGet, path, nil), "phone")); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: %d", path, rec.Code)
		}
	}
	if rec := restoreCall(t, s, asSender(restorePost(restorePath, "k-bad", `{"conversation":["x"]}`), "phone")); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown field: %d %s", rec.Code, rec.Body)
	}
}

func TestRestoreAnswersEachRowAndARetryIsTheFirstAnswer(t *testing.T) {
	s := restoreServer(t, bootAfter)
	rec := restoreCall(t, s, asSender(restorePost(restorePath, "k1", `{"conversations":["never-open"]}`), "phone"))
	var got contract.RestoreSessionsAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	if len(got.Results) != 1 || got.Results[0].OK || got.Results[0].Code != contract.RestoreCodeNotRestorable {
		t.Fatalf("results = %+v", got.Results)
	}
	// The same key for a different list is a different request.
	if rec := restoreCall(t, s, asSender(restorePost(restorePath, "k1", `{"conversations":["conv-a"]}`), "phone")); rec.Code != http.StatusConflict {
		t.Fatalf("a reused key for another body: %d %s", rec.Code, rec.Body)
	}
	names := make([]string, 21)
	for i := range names {
		names[i] = `"c` + string(rune('a'+i)) + `"`
	}
	many := `{"conversations":[` + strings.Join(names, ",") + `]}`
	if rec := restoreCall(t, s, asSender(restorePost(restorePath, "k-many", many), "phone")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "restore_batch_too_large") {
		t.Fatalf("21 conversations: %d %s", rec.Code, rec.Body)
	}
}

func TestDismissAllIsReplayedUnderItsKey(t *testing.T) {
	s := restoreServer(t, bootAfter)
	dismiss := func(key, body string) contract.DismissRestorableAnswer {
		t.Helper()
		rec := restoreCall(t, s, asSender(restorePost(dismissPath, key, body), "phone"))
		var got contract.DismissRestorableAnswer
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
		}
		return got
	}
	if got := dismiss("d1", `{}`); got.Dismissed != 2 || !got.OK {
		t.Fatalf("dismiss all = %+v", got)
	}
	if got := dismiss("d1", `{}`); got.Dismissed != 2 {
		t.Fatalf("the retry was carried out again instead of answered: %+v", got)
	}
	if got := dismiss("d2", `{"conversations":["conv-a"]}`); got.Dismissed != 0 {
		t.Fatalf("a dismissed row was dismissed again: %+v", got)
	}
	rec := restoreCall(t, s, asDevice(httptest.NewRequest(http.MethodGet, restorablePath, nil), "phone", false))
	if !strings.Contains(rec.Body.String(), `"sessions":[]`) {
		t.Fatalf("dismissed rows are still offered: %s", rec.Body)
	}
}
