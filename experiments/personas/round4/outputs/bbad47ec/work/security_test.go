package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doReq(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(rec, req)
	return rec
}

// PUT /me must not let a caller escalate their own role via mass assignment.
func TestPutMeCannotEscalateRole(t *testing.T) {
	app := NewApp("files", "admin-tok")
	h := NewServer(app)

	rec := doReq(h, "PUT", "/me", "tok-alice", `{"name":"Alice","email":"alice@example.test","role":"admin","id":3}`)
	if rec.Code != 200 {
		t.Fatalf("PUT /me failed: %d %s", rec.Code, rec.Body.String())
	}

	app.mu.Lock()
	u := app.users["tok-alice"]
	app.mu.Unlock()
	if u.Role != "user" {
		t.Fatalf("role was escalated to %q via PUT /me", u.Role)
	}
	if u.ID != 1 {
		t.Fatalf("id was changed to %d via PUT /me", u.ID)
	}
}

// GET /notes/{id} must not let one user read another user's private note.
func TestGetNoteEnforcesOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "admin-tok"))

	rec := doReq(h, "GET", "/notes/2", "tok-alice", "") // bob's private note
	if rec.Code != 404 {
		t.Fatalf("alice was able to read bob's note: %d %s", rec.Code, rec.Body.String())
	}
}

// DELETE /notes/{id} must require authentication and ownership.
func TestDeleteNoteRequiresAuthAndOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "admin-tok"))

	rec := doReq(h, "DELETE", "/notes/1", "", "")
	if rec.Code != 401 {
		t.Fatalf("unauthenticated caller was able to delete a note: %d", rec.Code)
	}

	rec = doReq(h, "DELETE", "/notes/2", "tok-alice", "") // bob's note
	if rec.Code != 404 {
		t.Fatalf("alice was able to delete bob's note: %d", rec.Code)
	}
}

// GET /files must not allow escaping the configured files directory via
// an embedded ".." segment that doesn't appear at the start of the name.
func TestFilesRejectsPathTraversal(t *testing.T) {
	h := NewServer(NewApp("files", "admin-tok"))

	rec := doReq(h, "GET", "/files?name=docs/../../secret/signing.key", "tok-alice", "")
	if rec.Code == 200 {
		t.Fatalf("path traversal escaped files dir: got %d body=%q", rec.Code, rec.Body.String())
	}
}

// GET /admin/stats must require the exact configured admin token.
func TestAdminStatsRequiresToken(t *testing.T) {
	h := NewServer(NewApp("files", "admin-tok"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	req.Header.Set("X-Admin-Token", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("wrong admin token accepted: %d", rec.Code)
	}
}
