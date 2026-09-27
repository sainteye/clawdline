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

// DELETE /notes/{id} must require auth and ownership (or admin), not be wide open.
func TestDeleteNoteRequiresOwnerOrAdmin(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "DELETE", "/notes/1", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete: got %d, want 401", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("note should still exist: got %d", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/1", "tok-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("owner delete: got %d, want 204", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/2", "tok-carol", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: got %d, want 204", rec.Code)
	}
}

// GET /notes/{id} must not leak notes to users who don't own them.
func TestGetNoteBlocksOtherUsers(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "GET", "/notes/2", "tok-alice", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner read: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/2", "tok-bob", ""); rec.Code != http.StatusOK {
		t.Fatalf("owner read: got %d, want 200", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/2", "tok-carol", ""); rec.Code != http.StatusOK {
		t.Fatalf("admin read: got %d, want 200", rec.Code)
	}
}

// PUT /me must only let a user change name/email, not role or id.
func TestUpdateProfileCannotEscalateRole(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "PUT", "/me", "tok-alice", `{"name":"A2","email":"a2@example.test","role":"admin","id":999}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"role":"user"`) {
		t.Fatalf("role must not change: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":1`) {
		t.Fatalf("id must not change: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"A2"`) {
		t.Fatalf("legit name update should apply: %s", rec.Body.String())
	}

	// Escalation must not carry over into note access.
	if rec := doReq(h, "PUT", "/notes/2", "tok-alice", `{"title":"x","body":"y"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("alice should still not be able to edit bob's note: got %d", rec.Code)
	}
}

// GET /files must not escape filesDir via "../" segments embedded later in the path.
func TestFilesPathTraversalBlocked(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "GET", "/files?name=docs/../../secret/signing.key", "tok-alice", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal: got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SIGNING-KEY") {
		t.Fatalf("secret leaked: %s", rec.Body.String())
	}

	rec = doReq(h, "GET", "/files?name=docs/readme.txt", "tok-alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("legit file read: got %d", rec.Code)
	}
}
