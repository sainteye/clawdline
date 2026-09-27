package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func request(t *testing.T, h http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

func TestProfileCannotChangeIdentityOrRole(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	rec := request(t, h, "PUT", "/me", "tok-alice", `{"name":"Mallory","email":"<email>","role":"admin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mass assignment accepted: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(t, h, "GET", "/me", "tok-alice", "")
	if strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatal("profile role was changed")
	}
}

func TestNoteAuthorizationAndImportOwnership(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	if rec := request(t, h, "GET", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("read another user's note: %d", rec.Code)
	}
	if rec := request(t, h, "DELETE", "/notes/1", "tok-bob", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted another user's note: %d", rec.Code)
	}
	rec := request(t, h, "POST", "/notes/import", "tok-bob", `[{"id":1,"owner_id":1,"title":"old","body":"saved"}]`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import failed: %d", rec.Code)
	}
	if rec = request(t, h, "GET", "/notes/1", "tok-alice", ""); !strings.Contains(rec.Body.String(), "alice private") {
		t.Fatal("import replaced an existing note")
	}
}

func TestExactAdminAndHookSecrets(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	req.Header.Set("X-Admin-Token", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatal("admin token prefix accepted")
	}
	req = httptest.NewRequest("POST", "/integrations/1/hook", nil)
	req.Header.Set("X-Hook-Signature", "hook-secret")
	req.Header.Set("X-Hook-Nonce", "n1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid hook rejected: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("hook replay accepted: %d", rec.Code)
	}
}

func TestFileContainmentIncludingSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	h := NewServer(NewApp(root, "admin-secret"))
	if rec := request(t, h, "GET", "/files?name=escape", "tok-alice", ""); rec.Code == http.StatusOK {
		t.Fatal("served a symlink outside the file root")
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
