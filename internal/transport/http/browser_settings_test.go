package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	"github.com/sainteye/clawdline/internal/config"
)

func TestBrowserSettingsAreTwoSwitchesOffByDefault(t *testing.T) {
	f, _ := newGateFixture(t)
	_, reader, err := f.g.auth.AddDeviceWithPermission("explicit reader", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	handler := f.g.wrap(http.HandlerFunc(s.browserSettingsRoute))
	send := func(token, method, body string) (int, map[string]any) {
		t.Helper()
		answer := (call{method: method, path: "/v1/browser/settings", body: body,
			headers: map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", actorHeader: actorDevice}}).do(handler)
		var got map[string]any
		_ = json.Unmarshal(answer.Body.Bytes(), &got)
		return answer.Code, got
	}
	code, got := send(f.send, "GET", "")
	if code != 200 || got["browser_enabled"] != false || got["browser_notify_new_site"] != false || len(got) != 2 {
		t.Fatalf("a new machine: %d %v", code, got)
	}
	code, got = send(f.send, "POST", `{"browser_enabled":true}`)
	if code != 200 || got["browser_enabled"] != true || got["browser_notify_new_site"] != false {
		t.Fatalf("turning browser use on: %d %v", code, got)
	}
	if !s.browserSetting("browser_enabled") {
		t.Fatal("the bridge does not see the switch the route wrote")
	}
	code, got = send(f.send, "POST", `{"browser_notify_new_site":true}`)
	if code != 200 || got["browser_enabled"] != true || got["browser_notify_new_site"] != true {
		t.Fatalf("a partial write changed the other switch: %d %v", code, got)
	}
	for _, body := range []string{`{"codegraph_enabled":true}`, `{"browser_enabled":"yes"}`, `{"browser_enabled":1}`, `[]`, `{"browser_enabled":true}{}`} {
		if code, _ := send(f.send, "POST", body); code != 400 {
			t.Fatalf("%s: %d, want 400", body, code)
		}
	}
	if code, _ := send(reader, "POST", `{"browser_enabled":false}`); code != 403 {
		t.Fatalf("a device that may only read changed a switch: %d", code)
	}
	if code, _ := send(reader, "GET", ""); code != 200 {
		t.Fatalf("a device that may read could not read: %d", code)
	}
	// An agent may not turn its own browser use on or off.
	answer := (call{method: "POST", path: "/v1/browser/settings", body: `{"browser_enabled":false}`,
		headers: map[string]string{"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}).do(handler)
	if answer.Code != 403 || !s.browserSetting("browser_enabled") {
		t.Fatalf("the machine token wrote the switch: %d %s", answer.Code, answer.Body)
	}
}

func TestTheBrowserAuditFiltersBySiteAndTime(t *testing.T) {
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{}}
	b := s.browser()
	for _, u := range []string{"https://github.com/a", "https://example.com/b", "https://github.com/c"} {
		_, _ = b.Do(t.Context(), "c1", "open", json.RawMessage(`{"url":"`+u+`"}`))
	}
	handler := f.g.wrap(http.HandlerFunc(s.browserAuditRoute))
	read := func(query url.Values) (int, []browserbridge.AuditRow) {
		t.Helper()
		answer := (call{method: "GET", path: "/v1/browser/audit?" + query.Encode(),
			headers: map[string]string{"Authorization": "Bearer " + f.read, actorHeader: actorDevice}}).do(handler)
		var got struct {
			Rows []browserbridge.AuditRow `json:"rows"`
		}
		_ = json.Unmarshal(answer.Body.Bytes(), &got)
		return answer.Code, got.Rows
	}
	// Refused attempts remain visible, including the requested site.
	if code, rows := read(url.Values{"limit": {"10"}}); code != 200 || len(rows) != 3 {
		t.Fatalf("refusals without a host: %d %+v", code, rows)
	}
	b.SetPersonControl(7, true)
	if code, rows := read(url.Values{"since": {"0"}, "limit": {"10"}}); code != 200 || len(rows) != 4 || rows[0].Op != "takeover" {
		t.Fatalf("since=0: %d %+v", code, rows)
	}
	if code, rows := read(url.Values{"since": {"99999999999"}}); code != 200 || len(rows) != 0 {
		t.Fatalf("since the future: %d %+v", code, rows)
	}
	if code, rows := read(url.Values{"site": {"github.com"}}); code != 200 || len(rows) != 2 {
		t.Fatalf("requested site is filterable: %d %+v", code, rows)
	}
	for _, bad := range []url.Values{{"since": {"-1"}}, {"since": {"yesterday"}}} {
		if code, _ := read(bad); code != 400 {
			t.Fatalf("%v: %d, want 400", bad, code)
		}
	}
}

func TestBrowserStopRefusesAnUnsavedControlState(t *testing.T) {
	f, _ := newGateFixture(t)
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	if err := s.browser().SetMuted("c1", true); err != nil {
		t.Fatal(err)
	}
	backup := dir + "-moved"
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	restored := false
	defer func() {
		if !restored {
			_ = os.Remove(dir)
			_ = os.Rename(backup, dir)
		}
	}()
	handler := f.g.wrap(http.HandlerFunc(s.browserStopRoute))
	send := func() *httptest.ResponseRecorder {
		return (call{method: "POST", path: "/v1/browser/stop", body: `{"stopped":true}`, headers: map[string]string{"Authorization": "Bearer " + f.send, "Content-Type": "application/json", actorHeader: actorDevice}}).do(handler)
	}
	first := send()
	if first.Code != http.StatusServiceUnavailable || codeOf(t, first) != "browser_state_write_failed" || !s.browser().Status().Stopped || !s.browser().Status().StateError {
		t.Fatalf("false durable acknowledgement: %d %s", first.Code, first.Body)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	restored = true
	again := send()
	if again.Code != http.StatusOK || s.browser().Status().StateError {
		t.Fatalf("retry: %d %s", again.Code, again.Body)
	}
}
