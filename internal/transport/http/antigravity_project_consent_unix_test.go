//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestAntigravityConsentHTTPRecoveryRevocationAndCloud(t *testing.T) {
	f := projectCloudStandIn(t)
	project, _ := f.server.workV2Project(t.Context(), f.place)
	stop, err := f.server.StartAntigravityProjectConsent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	local, _, err := f.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/projects/" + f.place + "/antigravity-consent"
	request := func(method, url, body, key, credential string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:7757"+url, strings.NewReader(body))
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		out := httptest.NewRecorder()
		f.handler.ServeHTTP(out, r)
		return out
	}
	read := func() contract.AntigravityConsentStatus {
		t.Helper()
		r := request("GET", path, "", "", local)
		var out contract.AntigravityConsentStatus
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &out) != nil {
			t.Fatalf("status %d %s", r.Code, r.Body.String())
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cached consent")
		}
		if strings.Contains(r.Body.String(), project.Path) || strings.Contains(r.Body.String(), "operation_id") {
			t.Fatal("private authority fields crossed HTTP")
		}
		return out
	}
	initial := read()
	if initial.Phase != "absent" || initial.Revision != 0 || initial.Preview == nil {
		t.Fatal(initial)
	}
	if _, err := os.Stat(filepath.Join(project.Path, ".agents", "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("read installed hooks", err)
	}
	encode := func(action string, status contract.AntigravityConsentStatus) string {
		t.Helper()
		in := contract.AntigravityConsentChange{Action: contract.AntigravityConsentChangeAction(action), Revision: status.Revision, Scope: status.Scope}
		if action == "enable" {
			in.Preview = status.Preview
		}
		b, e := json.Marshal(in)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	enable := encode("enable", initial)
	for _, token := range []string{"", f.machine} {
		if out := request("POST", path, enable, "enable", token); out.Code == 200 {
			t.Fatal("unauthorized or orchestrator enabled")
		}
	}
	// A paired read-only verdict does not acquire send capability.
	r := httptest.NewRequest("POST", path, strings.NewReader(enable))
	r.Header.Set("Idempotency-Key", "readonly")
	r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Device: "reader", Caps: auth.NewCaps(auth.Read)}}))
	out := httptest.NewRecorder()
	f.server.antigravityProjectConsentRoute(out, r, f.place)
	if out.Code != 403 {
		t.Fatal("read-only mutation", out.Code)
	}
	for _, bad := range []string{`{"action":"disable","revision":0,"scope":"all"}`, `{"action":"disable","scope":"project-and-linked-worktrees"}`, `{"action":"disable","revision":null,"scope":"project-and-linked-worktrees"}`, strings.TrimSuffix(enable, "}") + `,"project_root":"/foreign"}`, enable + enable} {
		if out := request("POST", path, bad, "invalid", local); out.Code != 400 {
			t.Fatalf("invalid input %d %s", out.Code, out.Body.String())
		}
	}
	if out := request("POST", path, enable, "", local); out.Code != 400 {
		t.Fatal("missing key", out.Code)
	}
	if out := request("POST", path, strings.Repeat(" ", antigravityConsentBodyBytesLimit+1), "large", local); out.Code != 413 {
		t.Fatal("body guard", out.Code)
	}
	if out := request("GET", path, "x", "", local); out.Code != 400 {
		t.Fatal("read body", out.Code)
	}
	if out := request("GET", path+"?workspace=/foreign", "", "", local); out.Code != 400 {
		t.Fatal("query", out.Code)
	}
	// Discard the first successful answer, then recover the same operation.
	if out := request("POST", path, enable, "enable", local); out.Code != 200 {
		t.Fatalf("enable %d %s", out.Code, out.Body.String())
	}
	active := read()
	if active.Phase != "active" || active.Hooks != "installed" || active.Revision != 2 {
		t.Fatal(active)
	}
	if out := request("POST", path, enable, "enable", local); out.Code != 200 {
		t.Fatalf("lost answer recovery %d %s", out.Code, out.Body.String())
	}
	if read().Revision != active.Revision {
		t.Fatal("retry repeated transition")
	}
	if out := request("POST", path, encode("disable", active), "enable", local); out.Code != 409 {
		t.Fatal("changed action key reused", out.Code)
	}
	disable := encode("disable", active)
	if out := request("POST", path, disable, "disable", local); out.Code != 200 {
		t.Fatal("disable", out.Code, out.Body.String())
	}
	disabled := read()
	if disabled.Phase != "disabled" || disabled.Hooks != "removed" || disabled.Revision != 3 {
		t.Fatal(disabled)
	}
	if out := request("POST", path, enable, "enable", local); out.Code != 409 {
		t.Fatal("old grant replay", out.Code)
	}
	if read().Phase != "disabled" {
		t.Fatal("historical receipt reauthorized")
	}
	// Actual Cloud bridge + verified local authorizer, not a hand-built actor.
	readCloud, _ := f.ask(t, 1, map[string]any{"type": "project-antigravity-consent-status", "session": cloudops.MachineReplySession, "request": "consent-read", "project": f.place})
	if !readCloud.OK() || !strings.Contains(string(readCloud.Payload), `"disabled"`) {
		t.Fatalf("Cloud read %+v", readCloud)
	}
	body := map[string]any{"action": "enable", "revision": disabled.Revision, "scope": disabled.Scope, "preview": disabled.Preview}
	change, _ := f.ask(t, 2, map[string]any{"type": "project-antigravity-consent-change", "session": cloudops.MachineReplySession, "request": "consent-cloud-enable", "project": f.place, "item": body})
	if !change.OK() {
		t.Fatalf("Cloud enable %+v", change)
	}
	if read().Phase != "active" {
		t.Fatal("Cloud write did not reach durable consent")
	}
	if f.server.antigravityPolicyInputs.Load() != nil {
		t.Fatal("HTTP consent enabled unverified native modes")
	}
}
func TestAntigravityConsentReadAfterCleanupFailureRemainsRevoked(t *testing.T) {
	f := projectCloudStandIn(t)
	project, _ := f.server.workV2Project(t.Context(), f.place)
	stop, err := f.server.StartAntigravityProjectConsent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	owner := f.server.antigravityProjectOwner.Load()
	p, err := owner.Preview(t.Context(), project.Path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := owner.Enable(t.Context(), p, "device", "enable", "operation")
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(project.Path, ".agents", "hooks.json")
	if err := os.WriteFile(settings, []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Disable(t.Context(), project.Path, out.Consent.Revision, "device", "disable", "revoke"); err == nil {
		t.Fatal("malformed cleanup accepted")
	}
	status, err := owner.Status(t.Context(), project.Path)
	if err != nil || status.Consent.Phase != store.AntigravityConsentDisabled || status.Hooks != "unknown" || status.Preview != nil {
		t.Fatal(status, err)
	}
	stop()
	local, _, _ := f.server.CloudCredentials()
	r := httptest.NewRequest("GET", "http://127.0.0.1:7757/v1/projects/"+f.place+"/antigravity-consent", nil)
	r.Header.Set("Authorization", "Bearer "+local)
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, r)
	if res.Code != 503 {
		t.Fatal("stopped owner", res.Code)
	}
}
