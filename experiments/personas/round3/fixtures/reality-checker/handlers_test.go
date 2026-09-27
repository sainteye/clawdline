package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Taipei")
	s := NewServer(store, loc)
	s.now = func() time.Time { return time.Date(2026, 3, 10, 9, 0, 0, 0, loc) }
	return s, s.Routes()
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHealthz(t *testing.T) {
	_, h := newTestServer(t)
	if rec := do(t, h, "GET", "/healthz", ""); rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestCreateAndGet(t *testing.T) {
	_, h := newTestServer(t)
	rec := do(t, h, "POST", "/tasks", `{"title":"Buy milk","due":"2026-03-12","tags":["Home"," errands "]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	created := decode[Task](t, rec)
	if created.ID != "t0001" || created.Due.Format(dateLayout) != "2026-03-12" {
		t.Fatalf("unexpected %+v", created)
	}
	if strings.Join(created.Tags, ",") != "home,errands" {
		t.Fatalf("tags not normalized: %v", created.Tags)
	}
	if got := rec.Header().Get("Location"); got != "/tasks/t0001" {
		t.Fatalf("location %q", got)
	}
	rec = do(t, h, "GET", "/tasks/t0001", "")
	if rec.Code != 200 || decode[Task](t, rec).Title != "Buy milk" {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateValidation(t *testing.T) {
	_, h := newTestServer(t)
	for _, body := range []string{
		`{"title":""}`,
		`{"title":"x","due":"12/03/2026"}`,
		`{"title":"x","tags":["a","b","c","d","e","f"]}`,
		`not json`,
	} {
		if rec := do(t, h, "POST", "/tasks", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
}

func TestGetUnknown(t *testing.T) {
	_, h := newTestServer(t)
	if rec := do(t, h, "GET", "/tasks/t0099", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestPatchMarksDone(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"Call the plumber"}`)
	rec := do(t, h, "PATCH", "/tasks/t0001", `{"done":true}`)
	if rec.Code != 200 || !decode[Task](t, rec).Done {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
}

func TestFilterByTag(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"a","tags":["work"]}`)
	do(t, h, "POST", "/tasks", `{"title":"b","tags":["home"]}`)
	got := decode[[]Task](t, do(t, h, "GET", "/tasks?tag=Work", ""))
	if len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestOverdue(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"late","due":"2026-03-01"}`)
	do(t, h, "POST", "/tasks", `{"title":"later","due":"2026-04-01"}`)
	do(t, h, "POST", "/tasks", `{"title":"late but done","due":"2026-03-02"}`)
	do(t, h, "PATCH", "/tasks/t0003", `{"done":true,"due":"2026-03-02"}`)
	got := decode[[]Task](t, do(t, h, "GET", "/tasks?overdue=true", ""))
	if len(got) != 1 || got[0].Title != "late" {
		t.Fatalf("got %+v", got)
	}
}

func TestDelete(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"x"}`)
	if rec := do(t, h, "DELETE", "/tasks/t0001", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/tasks/t0001", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("after delete got %d", rec.Code)
	}
}
