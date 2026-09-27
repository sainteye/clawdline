package main

import (
	"encoding/json"
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

func do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
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

// GET /files must not escape the configured files directory via "..".
func TestFilesPathTraversalBlocked(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := do(h, "GET", "/files?name=docs/../../secret/signing.key", "tok-alice", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected traversal to be rejected, got %d: %s", rec.Code, rec.Body.String())
	}

	// legitimate read still works
	rec = do(h, "GET", "/files?name=docs/readme.txt", "tok-alice", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome") {
		t.Fatalf("legit file read broken: %d %s", rec.Code, rec.Body.String())
	}
}

// GET /notes/{id} must not let a non-owner, non-admin user read someone else's note.
func TestNoteReadIsOwnerOrAdminOnly(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := do(h, "GET", "/notes/1", "tok-bob", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected bob to be denied alice's note, got %d: %s", rec.Code, rec.Body.String())
	}

	// owner can still read
	rec = do(h, "GET", "/notes/1", "tok-alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner read broken: %d %s", rec.Code, rec.Body.String())
	}
	// admin can still read
	rec = do(h, "GET", "/notes/1", "tok-carol", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin read broken: %d %s", rec.Code, rec.Body.String())
	}
}

// DELETE /notes/{id} must require authentication and owner/admin authorization.
func TestNoteDeleteRequiresAuthAndOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := do(h, "DELETE", "/notes/2", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated delete to be rejected, got %d", rec.Code)
	}

	rec = do(h, "DELETE", "/notes/1", "tok-bob", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected non-owner delete to be rejected, got %d", rec.Code)
	}

	// owner can still delete their own note
	rec = do(h, "DELETE", "/notes/2", "tok-bob", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner delete broken: %d %s", rec.Code, rec.Body.String())
	}
	// admin can still delete someone else's note
	rec = do(h, "DELETE", "/notes/1", "tok-carol", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete broken: %d %s", rec.Code, rec.Body.String())
	}
}

// PUT /me must not allow mass assignment of role or id.
func TestProfileUpdateCannotEscalateRole(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := do(h, "PUT", "/me", "tok-bob", `{"name":"Bob","email":"bob@example.test","role":"admin","id":999}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile update failed: %d %s", rec.Code, rec.Body.String())
	}
	var u User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.Role != "user" || u.ID != 2 {
		t.Fatalf("mass assignment succeeded: role=%q id=%d", u.Role, u.ID)
	}

	// legitimate name/email update still works
	rec = do(h, "PUT", "/me", "tok-bob", `{"name":"Bobby","email":"bobby@example.test"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legit profile update broken: %d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &u)
	if u.Name != "Bobby" || u.Email != "bobby@example.test" {
		t.Fatalf("legit profile update did not apply: %+v", u)
	}
}
