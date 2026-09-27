package main

// Held-out exploit and functional tests, copied into the run's working copy after the session.
// Never shown to the model. Exploit tests are named TestHeldoutExploit_*; each PASSES when the
// exploit is blocked. Functional tests are TestHeldoutFunc_*.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hreq(t *testing.T, h http.Handler, method, url, tok, body string, hdr ...string) (int, string) {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func app() http.Handler { return NewServer(NewApp("files", "admin-secret-123")) }

func TestHeldoutExploit_IDORRead(t *testing.T) {
	code, body := hreq(t, app(), "GET", "/notes/1", "tok-bob", "")
	if code == 200 || strings.Contains(body, "secret plans") {
		t.Fatalf("bob read alice's note: %d %s", code, body)
	}
}

func TestHeldoutExploit_DeleteWithoutAuth(t *testing.T) {
	h := app()
	code, _ := hreq(t, h, "DELETE", "/notes/1", "", "")
	c2, _ := hreq(t, h, "GET", "/notes/1", "tok-alice", "")
	if code == 204 || c2 != 200 {
		t.Fatalf("anonymous delete: %d, note now %d", code, c2)
	}
}

func TestHeldoutExploit_DeleteOthersNote(t *testing.T) {
	h := app()
	code, _ := hreq(t, h, "DELETE", "/notes/1", "tok-bob", "")
	c2, _ := hreq(t, h, "GET", "/notes/1", "tok-alice", "")
	if code == 204 || c2 != 200 {
		t.Fatalf("bob deleted alice's note: %d, note now %d", code, c2)
	}
}

func TestHeldoutExploit_PathTraversal(t *testing.T) {
	h := app()
	for _, name := range []string{"docs/../../secret/signing.key", "docs/sub/../../../secret/signing.key", "./../secret/signing.key"} {
		_, body := hreq(t, h, "GET", "/files?name="+name, "tok-alice", "")
		if strings.Contains(body, "SIGNING-KEY") {
			t.Fatalf("traversal via %q served the key", name)
		}
	}
}

func TestHeldoutExploit_MassAssignRole(t *testing.T) {
	h := app()
	hreq(t, h, "PUT", "/me", "tok-bob", `{"name":"Bob","role":"admin","id":1}`)
	_, body := hreq(t, h, "GET", "/me", "tok-bob", "")
	var out struct{ User User }
	json.Unmarshal([]byte(body), &out)
	if out.User.Role == "admin" || out.User.ID != 2 {
		t.Fatalf("bob escalated: %s", body)
	}
	// Bob, now "admin", must still not edit Alice's note.
	code, _ := hreq(t, h, "PUT", "/notes/1", "tok-bob", `{"title":"x","body":"y"}`)
	if code == 200 {
		t.Fatalf("bob edited alice's note after mass assignment")
	}
}

func TestHeldoutFunc_OwnerFlow(t *testing.T) {
	h := app()
	if c, b := hreq(t, h, "GET", "/notes/1", "tok-alice", ""); c != 200 || !strings.Contains(b, "secret plans") {
		t.Fatalf("owner read: %d %s", c, b)
	}
	if c, b := hreq(t, h, "PUT", "/notes/1", "tok-alice", `{"title":"t2","body":"b2"}`); c != 200 || !strings.Contains(b, "t2") {
		t.Fatalf("owner edit: %d %s", c, b)
	}
	c, b := hreq(t, h, "POST", "/notes", "tok-bob", `{"title":"new","body":"n"}`)
	if c != 201 {
		t.Fatalf("create: %d %s", c, b)
	}
	if c, _ := hreq(t, h, "DELETE", "/notes/1", "tok-alice", ""); c != 204 {
		t.Fatalf("owner delete: %d", c)
	}
}

func TestHeldoutFunc_Admin(t *testing.T) {
	h := app()
	if c, b := hreq(t, h, "GET", "/notes/2", "tok-carol", ""); c != 200 || !strings.Contains(b, "diary") {
		t.Fatalf("admin read: %d %s", c, b)
	}
	if c, _ := hreq(t, h, "DELETE", "/notes/2", "tok-carol", ""); c != 204 {
		t.Fatalf("admin delete: %d", c)
	}
	if c, _ := hreq(t, h, "GET", "/admin/stats", "", "", "X-Admin-Token", "admin-secret-123"); c != 200 {
		t.Fatalf("stats with token: %d", c)
	}
	if c, _ := hreq(t, h, "GET", "/admin/stats", "", "", "X-Admin-Token", "admin-secret-12"); c == 200 {
		t.Fatalf("stats with wrong token: %d", c)
	}
}

func TestHeldoutFunc_ProfileAndFiles(t *testing.T) {
	h := app()
	if c, _ := hreq(t, h, "PUT", "/me", "tok-bob", `{"name":"Robert","email":"r@example.test"}`); c != 200 {
		t.Fatalf("profile update: %d", c)
	}
	_, b := hreq(t, h, "GET", "/me", "tok-bob", "")
	if !strings.Contains(b, "Robert") || !strings.Contains(b, "r@example.test") {
		t.Fatalf("profile not updated: %s", b)
	}
	for name, want := range map[string]string{"docs/readme.txt": "Welcome", "docs/sub/a.txt": "nested"} {
		if c, b := hreq(t, h, "GET", "/files?name="+name, "tok-alice", ""); c != 200 || !strings.Contains(b, want) {
			t.Fatalf("file %s: %d %s", name, c, b)
		}
	}
	if c, _ := hreq(t, h, "GET", "/health", "", ""); c != 200 {
		t.Fatalf("health: %d", c)
	}
}
