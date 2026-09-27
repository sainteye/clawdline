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

// GET /notes/{id} must stay owner/admin-only, exactly like PUT/DELETE, but
// must still let the owner and an admin read the note.
func TestGetNoteBlocksNonOwners(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "GET", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bob reading alice's note: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("alice reading her own note: got %d, want 200", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/1", "tok-carol", ""); rec.Code != http.StatusOK {
		t.Fatalf("admin reading alice's note: got %d, want 200", rec.Code)
	}
}

// DELETE /notes/{id} must require authentication and ownership/admin,
// while still letting the owner and an admin delete.
func TestDeleteNoteRequiresAuthAndOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	if rec := doReq(h, "DELETE", "/notes/2", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous delete: got %d, want 401", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/2", "tok-alice", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("alice deleting bob's note: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "GET", "/notes/2", "tok-bob", ""); rec.Code != http.StatusOK {
		t.Fatalf("bob's note should still exist: got %d", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/2", "tok-bob", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("bob deleting his own note: got %d, want 204", rec.Code)
	}
	if rec := doReq(h, "DELETE", "/notes/1", "tok-carol", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("admin deleting alice's note: got %d, want 204", rec.Code)
	}
}

// PUT /me may only change name/email; role and id must not be assignable.
func TestUpdateProfileCannotEscalate(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "PUT", "/me", "tok-bob", `{"name":"Bob","email":"bob@example.test","role":"admin","id":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legit profile update: got %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("role was escalated via PUT /me: %s", rec.Body.String())
	}

	// bob must remain unable to touch alice's note.
	if rec := doReq(h, "DELETE", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bob deleting alice's note after PUT /me: got %d, want 404", rec.Code)
	}

	// legitimate name/email update still works.
	rec = doReq(h, "PUT", "/me", "tok-bob", `{"name":"Bobby","email":"bobby@example.test"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bobby") {
		t.Fatalf("legit name update failed: %d %s", rec.Code, rec.Body.String())
	}
}

// GET /files must stay inside filesDir even when ".." isn't the first path segment,
// while legitimate nested reads keep working.
func TestFilesPathTraversalBlocked(t *testing.T) {
	h := NewServer(NewApp("files", "x"))

	rec := doReq(h, "GET", "/files?name=docs/readme.txt", "tok-alice", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome") {
		t.Fatalf("legit file read failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "GET", "/files?name=docs/sub/a.txt", "tok-alice", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "nested doc") {
		t.Fatalf("legit nested file read failed: %d %s", rec.Code, rec.Body.String())
	}

	for _, name := range []string{
		"../secret/signing.key",
		"docs/../../secret/signing.key",
		"docs/../../../../../../etc/passwd",
	} {
		rec := doReq(h, "GET", "/files?name="+name, "tok-alice", "")
		if rec.Code == http.StatusOK {
			t.Fatalf("path traversal via %q succeeded: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

// GET /admin/stats: right token works, wrong token is rejected.
func TestAdminStatsToken(t *testing.T) {
	h := NewServer(NewApp("files", "right-token"))

	r := httptest.NewRequest("GET", "/admin/stats", nil)
	r.Header.Set("X-Admin-Token", "right-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid admin token: got %d, want 200", rec.Code)
	}

	r = httptest.NewRequest("GET", "/admin/stats", nil)
	r.Header.Set("X-Admin-Token", "wrong-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid admin token: got %d, want 403", rec.Code)
	}
}
