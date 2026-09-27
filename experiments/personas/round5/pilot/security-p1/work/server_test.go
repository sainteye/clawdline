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
