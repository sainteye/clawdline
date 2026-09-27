package main

// Key check for round 3's reality-checker fixture: each test PASSES when the planted defect
// (or the decoy's correct behaviour) is present. Copy into a temp copy of the fixture and run
// `go test -run KeyCheck -v`. Not part of the fixture; the verifier never sees it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeyCheckD1PatchUnknown500(t *testing.T) {
	_, h := newTestServer(t)
	if rec := do(t, h, "PATCH", "/tasks/t0099", `{"done":true}`); rec.Code != 500 {
		t.Fatalf("want 500 (defect), got %d", rec.Code)
	}
}

func TestKeyCheckD2PatchWipesOmittedFields(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"x","due":"2026-04-01","tags":["home"]}`)
	got := decode[Task](t, do(t, h, "PATCH", "/tasks/t0001", `{"title":"y"}`))
	if !got.Due.IsZero() || len(got.Tags) != 0 {
		t.Fatalf("want due/tags wiped (defect), got %+v", got)
	}
}

func TestKeyCheckD3MigrationDropsCreated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	os.WriteFile(p, []byte(`[{"id":"t0001","title":"a","labels":"","created":"2025-10-03T01:12:44Z"}]`), 0o644)
	Migrate(p)
	s, _ := OpenStore(p)
	if tk := s.List()[0]; !tk.CreatedAt.IsZero() {
		t.Fatalf("want zero created_at (defect), got %v", tk.CreatedAt)
	}
}

func TestKeyCheckD4MigrationTagsNotNormalized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	os.WriteFile(p, []byte(`[{"id":"t0001","title":"a","labels":"Health, personal","created":"2025-10-03T01:12:44Z"}]`), 0o644)
	Migrate(p)
	s, _ := OpenStore(p)
	if tags := strings.Join(s.List()[0].Tags, "|"); tags != "Health| personal" {
		t.Fatalf("want raw tags (defect), got %q", tags)
	}
	loc, _ := time.LoadLocation("Asia/Taipei")
	h := NewServer(s, loc).Routes()
	if got := decode[[]Task](t, do(t, h, "GET", "/tasks?tag=health", "")); len(got) != 0 {
		t.Fatalf("want tag filter to miss migrated task (defect), got %d", len(got))
	}
}

func TestKeyCheckD5OverdueIncludesDueToday(t *testing.T) {
	_, h := newTestServer(t) // clock: 2026-03-10 09:00 Asia/Taipei
	do(t, h, "POST", "/tasks", `{"title":"today","due":"2026-03-10"}`)
	if got := decode[[]Task](t, do(t, h, "GET", "/tasks?overdue=true", "")); len(got) != 1 {
		t.Fatalf("want due-today listed as overdue (defect), got %d", len(got))
	}
}

func TestKeyCheckD6OverdueIncludesNoDue(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"no due"}`)
	if got := decode[[]Task](t, do(t, h, "GET", "/tasks?overdue=true", "")); len(got) != 1 {
		t.Fatalf("want no-due task listed as overdue (defect), got %d", len(got))
	}
}

func TestKeyCheckD7PatchSkipsTagRules(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"x"}`)
	rec := do(t, h, "PATCH", "/tasks/t0001", `{"tags":["A","B","C","D","E","F"]}`)
	if rec.Code != 200 {
		t.Fatalf("want 200 for 6 tags (defect), got %d", rec.Code)
	}
	if got := decode[Task](t, rec); got.Tags[0] != "A" {
		t.Fatalf("want uppercase kept (defect), got %v", got.Tags)
	}
	if got := decode[[]Task](t, do(t, h, "GET", "/tasks?tag=a", "")); len(got) != 0 {
		t.Fatalf("want filter miss (defect), got %d", len(got))
	}
}

func TestKeyCheckD8DeleteUnknown204(t *testing.T) {
	_, h := newTestServer(t)
	if rec := do(t, h, "DELETE", "/tasks/t0099", ""); rec.Code != 204 {
		t.Fatalf("want 204 (defect), got %d", rec.Code)
	}
}

func TestKeyCheckD9PatchInvalidBody200(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, "POST", "/tasks", `{"title":"x","due":"2026-04-01"}`)
	rec := do(t, h, "PATCH", "/tasks/t0001", `not json`)
	if rec.Code != 200 {
		t.Fatalf("want 200 for invalid body (defect), got %d", rec.Code)
	}
}

// Decoys: behaviour is correct.
func TestKeyCheckX1ZeroDueIsEmptyString(t *testing.T) {
	_, h := newTestServer(t)
	rec := do(t, h, "POST", "/tasks", `{"title":"x"}`)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["due"] != "" {
		t.Fatalf("want empty due, got %v", m["due"])
	}
}

func TestKeyCheckX2TodayUsesServiceTZ(t *testing.T) {
	s, h := newTestServer(t)
	loc, _ := time.LoadLocation("Asia/Taipei")
	// 2026-03-10 00:30 in Taipei is still 2026-03-09 in UTC: a task due 03-09 is overdue in Taipei.
	s.now = func() time.Time { return time.Date(2026, 3, 10, 0, 30, 0, 0, loc) }
	do(t, h, "POST", "/tasks", `{"title":"yesterday","due":"2026-03-09"}`)
	do(t, h, "POST", "/tasks", `{"title":"tomorrow","due":"2026-03-11"}`)
	got := decode[[]Task](t, do(t, h, "GET", "/tasks?overdue=true", ""))
	if len(got) != 1 || got[0].Title != "yesterday" {
		t.Fatalf("got %+v", got)
	}
}

func TestKeyCheckX3BackupAndIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	os.WriteFile(p, []byte(`[{"id":"t0001","title":"a","labels":"x","created":"2025-10-03T01:12:44Z"}]`), 0o644)
	Migrate(p)
	before, _ := os.ReadFile(p)
	again, err := Migrate(p)
	after, _ := os.ReadFile(p)
	if again || err != nil || string(before) != string(after) {
		t.Fatal("second migration changed the file")
	}
	if _, err := os.Stat(p + ".v1.bak"); err != nil {
		t.Fatal(err)
	}
}
