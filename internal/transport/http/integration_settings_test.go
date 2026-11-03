package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

func TestIntegrationSettingsAreIndependentMachineOptIns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := &Server{cfg: config.Config{Dir: dir}}
	read := func(server *Server, method, body string) map[string]bool {
		t.Helper()
		r := httptest.NewRequest(method, "/v1/settings/integrations", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.integrationSettingsRoute(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, body, w.Code, w.Body)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		var profile string
		if err := json.Unmarshal(raw["workflow_profile"], &profile); err != nil {
			t.Fatal(err)
		}
		delete(raw, "workflow_profile")
		encoded, _ := json.Marshal(raw)
		var got map[string]bool
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read(s, "GET", "")
	if len(got) != 2 || got["workflow_skills_enabled"] || got["graphify_enabled"] {
		t.Fatalf("new machine must opt in: %v", got)
	}
	got = read(s, "POST", `{"workflow_skills_enabled":true}`)
	if !got["workflow_skills_enabled"] || got["graphify_enabled"] {
		t.Fatalf("skills toggle affected graphify: %v", got)
	}
	got = read(s, "POST", `{"graphify_enabled":true}`)
	if !got["workflow_skills_enabled"] || !got["graphify_enabled"] {
		t.Fatalf("graphify toggle lost skills opt-in: %v", got)
	}
	got = read(s, "POST", `{"workflow_skills_enabled":false}`)
	if got["workflow_skills_enabled"] || !got["graphify_enabled"] {
		t.Fatalf("disabling skills changed graphify: %v", got)
	}
	got = read(&Server{cfg: config.Config{Dir: dir}}, "GET", "")
	if got["workflow_skills_enabled"] || !got["graphify_enabled"] {
		t.Fatalf("restart did not retain machine settings: %v", got)
	}
	got = read(&Server{cfg: config.Config{Dir: t.TempDir()}}, "GET", "")
	if got["workflow_skills_enabled"] || got["graphify_enabled"] {
		t.Fatalf("another machine inherited opt-in: %v", got)
	}
}

func TestIntegrationSettingsRejectMalformedChangesAtomically(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for _, body := range []string{
		`{"workflow_skills_enabled":null}`, `{"graphify_enabled":"on"}`,
		`{"workflow_skills_enabled":true,"remote":true}`, `{"project":"/somewhere"}`,
		`{"workflow_skills_enabled":true} {}`, `[]`, `null`,
		`{"workflow_profile":"unknown","graphify_enabled":true}`, `{"workflow_profile":null}`,
		`{"workflow_profile":"poteto","workflow_skills_enabled":true}`,
	} {
		r := httptest.NewRequest("POST", "/v1/settings/integrations", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.integrationSettingsRoute(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body)
		}
		values, err := s.settingsFile().Read()
		if err != nil {
			t.Fatal(err)
		}
		if enabled, _ := values.Bool("workflow_skills_enabled"); enabled {
			t.Fatalf("rejected write partially enabled skills: %s", body)
		}
	}
}

func TestIntegrationSettingsRequireSendCapableDevice(t *testing.T) {
	f, _ := newGateFixture(t)
	_, reader, err := f.g.auth.AddDeviceWithPermission("explicit reader", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	handler := f.g.wrap(http.HandlerFunc(s.integrationSettingsRoute))
	for _, tc := range []struct {
		token string
		want  int
	}{{reader, 403}, {f.send, 200}} {
		answer := (call{method: "POST", path: "/v1/settings/integrations", body: `{"workflow_skills_enabled":true}`,
			headers: map[string]string{"Authorization": "Bearer " + tc.token, "Content-Type": "application/json", actorHeader: actorDevice}}).do(handler)
		if answer.Code != tc.want {
			t.Fatalf("device gate: %d %s; want %d", answer.Code, answer.Body, tc.want)
		}
	}
}
