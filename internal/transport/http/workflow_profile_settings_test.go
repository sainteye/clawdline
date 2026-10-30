package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/config"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowSelectionMigratesWithoutChangingOtherIntegrations(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"workflow_skills_enabled":true,"graphify_enabled":true,"remote":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	read := func(method, body string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, "/v1/settings/integrations", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.integrationSettingsRoute(w, r)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	before, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	got := read("GET", "")
	after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if got["workflow_profile"] != "superpowers" || string(before) != string(after) {
		t.Fatalf("legacy read did not preserve bytes: %v", got)
	}
	for _, profile := range []string{"poteto", "clawdline", "superpowers"} {
		got = read("POST", `{"workflow_profile":"`+profile+`"}`)
		if got["workflow_profile"] != profile || got["graphify_enabled"] != true {
			t.Fatalf("selection affected Graphify: %v", got)
		}
	}
	got = read("POST", `{"workflow_skills_enabled":false}`)
	if got["workflow_profile"] != "clawdline" {
		t.Fatalf("old client disable ignored: %v", got)
	}
	values, err := s.settingsFile().Read()
	remote, _ := values.Bool("remote")
	if err != nil || !remote {
		t.Fatalf("unrelated preference lost: %v", err)
	}
}
