package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer(NewApp("files", "x")).ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
}

func authed(method, target, token string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// A traversal that doesn't start with ".." or "/" must still be contained
// inside filesDir, not resolve to files outside it (e.g. secret/signing.key).
func TestFilesPathTraversalBlocked(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/files?name=docs/../../secret/signing.key", "tok-alice", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal was not blocked, got 200 body=%q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/files?name=docs/readme.txt", "tok-alice", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("legitimate file under filesDir should still work, got %d", rec.Code)
	}
}

// A user must not be able to grant themselves admin, or hijack another
// user's id, via PUT /me.
func TestProfileUpdateCannotEscalatePrivilege(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	body, _ := json.Marshal(map[string]any{"name": "Alice2", "role": "admin", "id": 2})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed("PUT", "/me", "tok-alice", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/me", "tok-alice", nil))
	var out struct {
		User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.User.Role != "user" {
		t.Fatalf("role was escalated to %q", out.User.Role)
	}
	if out.User.ID != 1 {
		t.Fatalf("id was hijacked to %d", out.User.ID)
	}
	if out.User.Name != "Alice2" {
		t.Fatalf("legitimate name update should still work, got %q", out.User.Name)
	}
}

// Only the owner (or an admin) may read a note.
func TestGetNoteEnforcesOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/notes/1", "tok-bob", nil)) // note 1 belongs to alice
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bob should not be able to read alice's note, got %d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/notes/1", "tok-alice", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("alice should be able to read her own note, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("GET", "/notes/1", "tok-carol", nil)) // carol is admin
	if rec.Code != http.StatusOK {
		t.Fatalf("admin should be able to read any note, got %d", rec.Code)
	}
}

// DELETE must require authentication and ownership, like GET/PUT do.
func TestDeleteNoteRequiresAuthAndOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/notes/1", nil)) // no token at all
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete should be rejected, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("DELETE", "/notes/1", "tok-bob", nil)) // bob deleting alice's note
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bob should not be able to delete alice's note, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed("DELETE", "/notes/1", "tok-alice", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("alice should be able to delete her own note, got %d", rec.Code)
	}
}

// Admin token check should behave correctly (functional coverage for the
// constant-time comparison; timing itself isn't asserted in a unit test).
func TestAdminStatsToken(t *testing.T) {
	h := NewServer(NewApp("files", "secret-token"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	req.Header.Set("X-Admin-Token", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token should be forbidden, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/admin/stats", nil)
	req.Header.Set("X-Admin-Token", "secret-token")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token should be accepted, got %d", rec.Code)
	}
}

// A title containing a newline must not be able to forge extra log lines.
func TestAuditLogTitleIsQuoted(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	h := NewServer(NewApp("files", "x"))
	body, _ := json.Marshal(map[string]string{
		"title": "hi\naudit: created note id=999 owner=1 title=\"forged\"",
		"body":  "b",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed("POST", "/notes", "tok-alice", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d", rec.Code)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("attacker-controlled title forged extra log lines: %q", buf.String())
	}
}
