package main

import (
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

func doReq(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// GET /notes/{id} must only be readable by its owner or an admin.
func TestNoteReadIsOwnerOrAdminOnly(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "GET", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bob (non-owner) reading alice's note: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("alice reading her own note: got %d, want 200", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-carol", ""); rec.Code != http.StatusOK {
		t.Fatalf("admin reading alice's note: got %d, want 200", rec.Code)
	}
}

// DELETE /notes/{id} must require authentication and ownership (or admin).
func TestNoteDeleteRequiresAuthAndOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "DELETE", "/notes/1", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete: got %d, want 401", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bob (non-owner) deleting alice's note: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("note should still exist after blocked delete: got %d", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/1", "tok-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("alice deleting her own note: got %d, want 204", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/2", "tok-carol", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("admin deleting bob's note: got %d, want 204", rec.Code)
	}
}

// PUT /me must not let a user overwrite fields beyond name/email (no mass assignment).
func TestProfileUpdateCannotEscalateRole(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "PUT", "/me", "tok-bob", `{"id":3,"name":"Bob","email":"bob@example.test","role":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legit-shaped update: got %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("role was overwritten via mass assignment: %s", rec.Body.String())
	}

	rec = doReq(h, "GET", "/me", "tok-bob", "")
	if !strings.Contains(rec.Body.String(), `"id":2`) || !strings.Contains(rec.Body.String(), `"role":"user"`) {
		t.Fatalf("bob's id/role changed unexpectedly: %s", rec.Body.String())
	}

	rec = doReq(h, "PUT", "/me", "tok-bob", `{"name":"Bobby","email":"bobby@example.test"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bobby") {
		t.Fatalf("legit profile update failed: %d %s", rec.Code, rec.Body.String())
	}
}

// GET /files must stay confined to the files directory, even with embedded ".." segments.
func TestFilesPathTraversalBlocked(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "GET", "/files?name=docs/readme.txt", "tok-alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("legit file read: got %d, want 200", rec.Code)
	}

	traversals := []string{
		"/files?name=docs/sub/../../../secret/signing.key",
		"/files?name=../secret/signing.key",
		"/files?name=/etc/passwd",
	}
	for _, path := range traversals {
		rec := doReq(h, "GET", path, "tok-alice", "")
		if rec.Code == http.StatusOK {
			t.Fatalf("traversal %q was not blocked: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}
