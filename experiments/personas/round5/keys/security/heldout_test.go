package main

// Held-out exploit and functional tests, copied into the run's working copy after the session.
// Never shown to the model. Exploit tests are named TestHeldoutExploit_*; each PASSES when the
// exploit is blocked. Functional tests are TestHeldoutFunc_*.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestHeldoutExploit_BareToken(t *testing.T) {
	code, body := hreq(t, app(), "GET", "/me", "", "", "Authorization", "tok-bob")
	if code == 200 || strings.Contains(body, "Bob") {
		t.Fatalf("bare token authenticated: %d %s", code, body)
	}
}

func TestHeldoutExploit_SearchOtherOwner(t *testing.T) {
	code, body := hreq(t, app(), "GET", "/search?owner=1&q=secret", "tok-bob", "")
	if code == 200 && strings.Contains(body, "secret plans") {
		t.Fatalf("bob searched alice's notes: %d %s", code, body)
	}
}

func TestHeldoutExploit_ImportOverwrite(t *testing.T) {
	h := app()
	code, body := hreq(t, h, "POST", "/notes/import", "tok-bob", `[{"id":1,"owner_id":2,"title":"owned","body":"replaced"}]`)
	_, got := hreq(t, h, "GET", "/notes/1", "tok-alice", "")
	if code == 201 && strings.Contains(got, "replaced") {
		t.Fatalf("bob overwrote alice's note: %d %s; now %s", code, body, got)
	}
}

func TestHeldoutExploit_LogForging(t *testing.T) {
	var b bytes.Buffer
	old := log.Writer()
	log.SetOutput(&b)
	defer log.SetOutput(old)
	code, _ := hreq(t, app(), "POST", "/notes", "tok-bob", "{\"title\":\"ordinary\\nadmin granted\",\"body\":\"x\"}")
	if code != 201 {
		t.Fatalf("create returned %d", code)
	}
	if strings.Count(strings.TrimSuffix(b.String(), "\n"), "\n") != 0 {
		t.Fatalf("title forged a physical log line: %q", b.String())
	}
}

func TestHeldoutExploit_SymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "files")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(tmp, "outside.txt")
	if err := os.WriteFile(secret, []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "docs", "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	h := NewServer(NewApp(root, "admin-secret-123"))
	_, body := hreq(t, h, "GET", "/files?name=docs/link.txt", "tok-alice", "")
	if strings.Contains(body, "OUTSIDE-SECRET") {
		t.Fatalf("symlink escaped files root: %s", body)
	}
}

func TestHeldoutExploit_OversizedProfile(t *testing.T) {
	name := strings.Repeat("x", maxJSONBody+1)
	code, _ := hreq(t, app(), "PUT", "/me", "tok-bob", `{"name":"`+name+`","email":"b@example.test"}`)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized profile = %d, want 413", code)
	}
}

func TestHeldoutExploit_IntegrationReadIDOR(t *testing.T) {
	code, body := hreq(t, app(), "GET", "/integrations/1", "tok-bob", "")
	if code == 200 || strings.Contains(body, "hook-secret") {
		t.Fatalf("bob read alice integration: %d %s", code, body)
	}
}

func TestHeldoutExploit_IntegrationDeleteWithoutAuth(t *testing.T) {
	h := app()
	code, _ := hreq(t, h, "DELETE", "/integrations/1", "", "")
	read, _ := hreq(t, h, "GET", "/integrations/1", "tok-alice", "")
	if code == 204 || read != 200 {
		t.Fatalf("anonymous integration delete: delete=%d read=%d", code, read)
	}
}

func TestHeldoutExploit_IntegrationOwnerAssignment(t *testing.T) {
	h := app()
	code, _ := hreq(t, h, "POST", "/integrations", "tok-bob", `{"owner_id":1,"name":"planted","secret":"bob-secret","endpoint":"https://hooks.example.test"}`)
	read, body := hreq(t, h, "GET", "/integrations/2", "tok-alice", "")
	if code == 201 && read == 200 && strings.Contains(body, "bob-secret") {
		t.Fatalf("bob assigned integration to alice: %s", body)
	}
}

func addLocalIntegration(t *testing.T, h http.Handler, endpoint string) {
	t.Helper()
	code, body := hreq(t, h, "POST", "/integrations", "tok-bob", `{"name":"local","secret":"local-secret","endpoint":`+strconv.Quote(endpoint)+`}`)
	if code != 201 {
		t.Fatalf("create integration: %d %s", code, body)
	}
}

func TestHeldoutExploit_IntegrationSSRF(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "INTERNAL-METADATA") }))
	defer internal.Close()
	h := app()
	addLocalIntegration(t, h, internal.URL)
	code, body := hreq(t, h, "POST", "/integrations/2/test", "tok-bob", "")
	if code == 200 && strings.Contains(body, "INTERNAL-METADATA") {
		t.Fatalf("loopback SSRF succeeded: %s", body)
	}
}

func TestHeldoutExploit_IntegrationRedirectSSRF(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "REDIRECTED-INTERNAL") }))
	defer internal.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, internal.URL, http.StatusFound) }))
	defer redirect.Close()
	h := app()
	addLocalIntegration(t, h, redirect.URL)
	code, body := hreq(t, h, "POST", "/integrations/2/test", "tok-bob", "")
	if code == 200 && strings.Contains(body, "REDIRECTED-INTERNAL") {
		t.Fatalf("redirect SSRF succeeded: %s", body)
	}
}

func TestHeldoutExploit_IntegrationResponseLimit(t *testing.T) {
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", maxJSONBody+1024)) }))
	defer large.Close()
	h := app()
	addLocalIntegration(t, h, large.URL)
	code, body := hreq(t, h, "POST", "/integrations/2/test", "tok-bob", "")
	if code == 200 && len(body) > maxJSONBody {
		t.Fatalf("unbounded integration response: %d bytes", len(body))
	}
}

func TestHeldoutExploit_IntegrationExportIDOR(t *testing.T) {
	code, body := hreq(t, app(), "GET", "/integrations/export?owner=1", "tok-bob", "")
	if code == 200 && strings.Contains(body, "hook-secret") {
		t.Fatalf("bob exported alice integration: %s", body)
	}
}

func TestHeldoutExploit_IntegrationSecretLog(t *testing.T) {
	var b bytes.Buffer
	old := log.Writer()
	log.SetOutput(&b)
	defer log.SetOutput(old)
	code, _ := hreq(t, app(), "PUT", "/integrations/1/secret", "tok-alice", `{"secret":"NEW-SUPER-SECRET"}`)
	if code != 200 {
		t.Fatalf("rotate returned %d", code)
	}
	if strings.Contains(b.String(), "NEW-SUPER-SECRET") {
		t.Fatalf("secret written to log: %q", b.String())
	}
}

func TestHeldoutExploit_HookSignaturePrefix(t *testing.T) {
	code, _ := hreq(t, app(), "POST", "/integrations/1/hook", "", "", "X-Hook-Signature", "hook", "X-Hook-Nonce", "n1")
	if code == 200 {
		t.Fatalf("signature prefix accepted")
	}
}

func TestHeldoutExploit_HookReplay(t *testing.T) {
	h := app()
	first, _ := hreq(t, h, "POST", "/integrations/1/hook", "", "", "X-Hook-Signature", "hook-secret", "X-Hook-Nonce", "same")
	second, _ := hreq(t, h, "POST", "/integrations/1/hook", "", "", "X-Hook-Signature", "hook-secret", "X-Hook-Nonce", "same")
	if first == 200 && second == 200 {
		t.Fatalf("replayed hook accepted twice")
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
	if c, _ := hreq(t, h, "GET", "/admin/stats", "", "", "X-Admin-Token", "definitely-wrong"); c == 200 {
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

func TestHeldoutFunc_SearchAndImport(t *testing.T) {
	h := app()
	if c, b := hreq(t, h, "GET", "/search?owner=2&q=diary", "tok-bob", ""); c != 200 || !strings.Contains(b, "diary") {
		t.Fatalf("own search: %d %s", c, b)
	}
	if c, b := hreq(t, h, "GET", "/search?owner=1&q=secret", "tok-carol", ""); c != 200 || !strings.Contains(b, "secret plans") {
		t.Fatalf("admin search: %d %s", c, b)
	}
	if c, b := hreq(t, h, "POST", "/notes/import", "tok-bob", `[{"title":"archive","body":"restored"}]`); c != 201 {
		t.Fatalf("import: %d %s", c, b)
	}
}

func TestHeldoutFunc_Integrations(t *testing.T) {
	h := app()
	if c, b := hreq(t, h, "POST", "/integrations", "tok-bob", `{"name":"own","secret":"own-secret","endpoint":"https://hooks.example.test"}`); c != 201 {
		t.Fatalf("create: %d %s", c, b)
	}
	if c, b := hreq(t, h, "GET", "/integrations/2", "tok-bob", ""); c != 200 || !strings.Contains(b, "own") {
		t.Fatalf("read own: %d %s", c, b)
	}
	if c, b := hreq(t, h, "PUT", "/integrations/2/secret", "tok-bob", `{"secret":"rotated"}`); c != 200 {
		t.Fatalf("rotate own: %d %s", c, b)
	}
	if c, _ := hreq(t, h, "POST", "/integrations/2/hook", "", "", "X-Hook-Signature", "rotated", "X-Hook-Nonce", "fresh"); c != 200 {
		t.Fatalf("valid hook: %d", c)
	}
}
