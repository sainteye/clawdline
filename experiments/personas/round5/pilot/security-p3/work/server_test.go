package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func request(t *testing.T, h http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer(NewApp("files", "x")).ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
}

func TestAuthorizationBoundaries(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))

	if rec := request(t, h, "GET", "/notes/2", "", "Bearer tok-alice"); rec.Code != http.StatusNotFound {
		t.Fatalf("read another user's note: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, h, "DELETE", "/notes/2", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete: %d", rec.Code)
	}
	if rec := request(t, h, "DELETE", "/notes/2", "", "Bearer tok-alice"); rec.Code != http.StatusNotFound {
		t.Fatalf("delete another user's note: %d", rec.Code)
	}
	if rec := request(t, h, "GET", "/search?owner=2", "", "Bearer tok-alice"); rec.Code != http.StatusForbidden {
		t.Fatalf("search another user's notes: %d", rec.Code)
	}
	if rec := request(t, h, "GET", "/me", "", "tok-alice"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("scheme-less token accepted: %d", rec.Code)
	}

	req := httptest.NewRequest("GET", "/admin/stats", nil)
	req.Header.Set("X-Admin-Token", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("partial admin token accepted: %d", rec.Code)
	}
}

func TestProfileCannotEscalatePrivileges(t *testing.T) {
	h := NewServer(NewApp("files", "x"))
	rec := request(t, h, "PUT", "/me", `{"name":"Mallory","email":"<email>","role":"admin","id":3}`, "Bearer tok-alice")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("privileged fields accepted: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, h, "DELETE", "/notes/2", "", "Bearer tok-alice"); rec.Code != http.StatusNotFound {
		t.Fatalf("role changed despite rejected request: %d", rec.Code)
	}
}

func TestImportAssignsNewIDsAndAuthenticatedOwner(t *testing.T) {
	h := NewServer(NewApp("files", "x"))
	body := `[{"id":2,"owner_id":2,"title":"imported","body":"safe"}]`
	rec := request(t, h, "POST", "/notes/import", body, "Bearer tok-alice")
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(t, h, "GET", "/notes/3", "", "Bearer tok-alice")
	var note Note
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &note) != nil || note.OwnerID != 1 {
		t.Fatalf("imported note ownership: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, h, "GET", "/notes/2", "", "Bearer tok-bob"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "bob private") {
		t.Fatalf("import replaced an existing note: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJSONLimitsAndTrailingData(t *testing.T) {
	h := NewServer(NewApp("files", "x"))
	rec := request(t, h, "POST", "/notes", `{"title":"ok"}{"title":"extra"}`, "Bearer tok-alice")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON accepted: %d", rec.Code)
	}
	rec = request(t, h, "POST", "/notes", `{"title":"x","body":"`+strings.Repeat("a", maxJSONBody)+`"}`, "Bearer tok-alice")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", rec.Code)
	}
}

func TestFilesStayBelowRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	h := NewServer(NewApp(root, "x"))
	for _, target := range []string{"/files?name=../secret", "/files?name=escape"} {
		if rec := request(t, h, "GET", target, "", "Bearer tok-alice"); rec.Code == http.StatusOK {
			t.Fatalf("file escape succeeded for %s: %s", target, rec.Body.String())
		}
	}
}

func TestOwnerCanSearchAndImport(t *testing.T) {
	h := NewServer(NewApp("files", "x"))
	req := httptest.NewRequest("GET", "/search?owner=2&q=diary", nil)
	req.Header.Set("Authorization", "Bearer tok-bob")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "diary") {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("POST", "/notes/import", strings.NewReader(`[{"title":"old","body":"saved"}]`))
	req.Header.Set("Authorization", "Bearer tok-bob")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
}
