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

func TestAuthenticationAndNoteAuthorization(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))

	req := httptest.NewRequest("GET", "/me", nil)
	req.Header.Set("Authorization", "tok-alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bare token accepted: %d", rec.Code)
	}

	if rec = request(t, h, "GET", "/notes/2", "tok-alice", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner read: %d %s", rec.Code, rec.Body.String())
	}
	if rec = request(t, h, "DELETE", "/notes/2", "tok-alice", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete: %d", rec.Code)
	}
	if rec = request(t, h, "GET", "/search?owner=2", "tok-alice", ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "bob private") {
		t.Fatalf("cross-owner search: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfileCannotChangeRole(t *testing.T) {
	a := NewApp("files", "admin-secret")
	h := NewServer(a)
	rec := request(t, h, "PUT", "/me", "tok-alice", `{"name":"Alice","email":"a@test","role":"admin"}`)
	if rec.Code != http.StatusBadRequest || a.users["tok-alice"].Role != "user" {
		t.Fatalf("mass assignment was not rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportAssignsOwnerAndFreshID(t *testing.T) {
	a := NewApp("files", "admin-secret")
	h := NewServer(a)
	rec := request(t, h, "POST", "/notes/import", "tok-bob", `[{"id":1,"owner_id":1,"title":"imported","body":"x"}]`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	if a.notes[1].Title != "alice private" || a.notes[3].OwnerID != 2 {
		t.Fatalf("import overwrote data or trusted owner: %#v", a.notes)
	}
}

func TestAdminTokenRequiresExactNonEmptyMatch(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	for _, token := range []string{"", "admin"} {
		req := httptest.NewRequest("GET", "/admin/stats", nil)
		req.Header.Set("X-Admin-Token", token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("token %q accepted", token)
		}
	}
}

func TestFilesRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	h := NewServer(NewApp(root, "admin-secret"))
	rec := request(t, h, "GET", "/files?name=escape", "tok-alice", "")
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("symlink escape: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationOwnershipSecretsAndReplay(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	rec := request(t, h, "GET", "/integrations/1", "tok-bob", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner integration read: %d", rec.Code)
	}
	rec = request(t, h, "GET", "/integrations/1", "tok-alice", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "hook-secret") {
		t.Fatalf("secret exposed: %d %s", rec.Code, rec.Body.String())
	}

	for i, signature := range []string{"hook", "hook-secret", "hook-secret"} {
		req := httptest.NewRequest("POST", "/integrations/1/hook", nil)
		req.Header.Set("X-Hook-Signature", signature)
		req.Header.Set("X-Hook-Nonce", "nonce")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := []int{http.StatusForbidden, http.StatusOK, http.StatusConflict}[i]
		if rec.Code != want {
			t.Fatalf("hook attempt %d: got %d want %d", i, rec.Code, want)
		}
	}
}

func TestRequestBodyLimitAndTrailingJSON(t *testing.T) {
	h := NewServer(NewApp("files", "admin-secret"))
	large, _ := json.Marshal(map[string]string{"title": "x", "body": strings.Repeat("a", maxJSONBody)})
	if rec := request(t, h, "POST", "/notes", "tok-alice", string(large)); rec.Code != http.StatusBadRequest {
		t.Fatalf("large body accepted: %d", rec.Code)
	}
	if rec := request(t, h, "POST", "/notes", "tok-alice", `{"title":"x"}{"title":"y"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON accepted: %d", rec.Code)
	}
}
