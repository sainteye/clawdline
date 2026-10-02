package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

func settingsCall(t *testing.T, s *Server, method, contentType, body string) (*httptest.ResponseRecorder, contract.SettingsSnapshot, contract.Refusal) {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/settings", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.settingsRoute(rec, req)
	var snap contract.SettingsSnapshot
	var refusal contract.Refusal
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
			t.Fatalf("%s: %v in %s", method, err, rec.Body)
		}
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
	}
	return rec, snap, refusal
}

func defaultModelsCall(t *testing.T, s *Server, method, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/settings/default-models", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.defaultModelsRoute(rec, req)
	return rec
}

func workGateSettingsCall(t *testing.T, s *Server, method, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/settings/work-gates", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.workGateSettingsRoute(rec, req)
	return rec
}

func TestWorkGateSettingsRouteNeverCarriesOtherSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}

	rec := workGateSettingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"planning_gate\":true,\"verify_gate\":false}\n" {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}
	rec = workGateSettingsCall(t, s, http.MethodPost, "application/json",
		`{"planning_gate":false,"verify_gate":true}`)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"planning_gate\":false,\"verify_gate\":true}\n" {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	rec = workGateSettingsCall(t, s, http.MethodPost, "application/json", `{"remote":true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"bad_request"`) {
		t.Fatalf("unrelated key: %d %s", rec.Code, rec.Body)
	}
	rec = workGateSettingsCall(t, s, http.MethodPost, "application/json", `{"planning_gate":"on"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_planning_gate"`) {
		t.Fatalf("wrong shape: %d %s", rec.Code, rec.Body)
	}
	rec = workGateSettingsCall(t, s, http.MethodPost, "text/plain", `{"planning_gate":true}`)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("plain text: %d %s", rec.Code, rec.Body)
	}
}

func TestDefaultModelsRouteNeverCarriesOtherSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-6-sol","display_name":"GPT-6 Sol","visibility":"list"},
		{"slug":"gpt-hidden","display_name":"Hidden","visibility":"hide"}
	]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{
		"clientDataCacheSlots":{"opus":{"model":"claude-opus-5-5"}},
		"additionalModelOptionsCache":[{"value":"claude-fable-5-1[1m]"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	claudeBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(claudeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeBin, "claude"), []byte(
		"claude-opus-5-5 claude-fable-5-1 claude-fable-5 claude-opus-5"), 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}

	rec := defaultModelsCall(t, s, http.MethodPost, "application/json",
		`{"codex_default_model":"gpt-6","claude_default_model":"claude-sonnet-4-5"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["codex_default_model"] != "gpt-6" || got["claude_default_model"] != "claude-sonnet-4-5" {
		t.Fatalf("narrow answer: %#v", got)
	}
	models, ok := got["models"].(map[string]any)
	if !ok || len(models) != 2 || !strings.Contains(rec.Body.String(), `"value":"gpt-6-sol","label":"GPT-6 Sol"`) ||
		strings.Contains(rec.Body.String(), "gpt-hidden") ||
		!strings.Contains(rec.Body.String(), `"value":"opus","label":"Opus 5.5"`) ||
		!strings.Contains(rec.Body.String(), `"value":"fable","label":"Fable 5.1"`) ||
		!strings.Contains(rec.Body.String(), `"value":"claude-fable-5","label":"Fable 5"`) {
		t.Fatalf("provider model choices: %s", rec.Body)
	}

	rec = defaultModelsCall(t, s, http.MethodPost, "application/json", `{"remote":true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"bad_request"`) {
		t.Fatalf("unrelated key: %d %s", rec.Code, rec.Body)
	}
	rec = defaultModelsCall(t, s, http.MethodPost, "application/json", `{"codex_default_model":"GPT 6"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_default_model"`) {
		t.Fatalf("invalid model: %d %s", rec.Code, rec.Body)
	}

	rec = defaultModelsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "remote") || strings.Contains(rec.Body.String(), "hotkey") {
		t.Fatalf("read disclosed another setting: %d %s", rec.Code, rec.Body)
	}
}

// The route reads the file as it is, writes only what it was sent, and refuses
// the write a page elsewhere could make without asking.
func TestSettingsRoute(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// The hotkey is the macOS shell's; elsewhere it is refused by name
		// (TestSettingsRefuseWhatThisMachineCannotDo).
		t.Skip("walks the hotkey, which only the macOS shell registers")
	}
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}

	rec, snap, _ := settingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || snap.Exists || snap.Hotkey != nil || snap.ScopeApp != nil {
		t.Fatalf("first read: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"hotkey":null`) {
		t.Fatalf("an absent key is null on the wire: %s", rec.Body)
	}

	// A simple cross-site request: plain text, no preflight.
	rec, _, refusal := settingsCall(t, s, http.MethodPost, "text/plain", `{"hotkey":"cmd+shift+k"}`)
	if rec.Code != http.StatusUnsupportedMediaType || refusal.Error != "unsupported_media_type" {
		t.Fatalf("plain text: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused write left a file: %v", err)
	}

	for body, code := range map[string]string{
		`{"hotkey":"k"}`:               "invalid_hotkey",
		`{"scope_app":"com.x;rm -rf"}`: "invalid_scope",
		// A key this app does not set, and one it does with a value it will not take.
		`{"status_dir":"/tmp"}`:             "bad_request",
		`{"language":"kl"}`:                 "invalid_language",
		`{"terminal":"ghostty"}`:            "invalid_terminal",
		`{"codex_default_model":"GPT 6"}`:   "invalid_default_model",
		`{"claude_default_model":"-opus"}`:  "invalid_default_model",
		`{"output_size":40}`:                "invalid_output_size",
		`{"orchestrator_max_children":2.5}`: "invalid_orchestrator_max_children",
		`{"notch":"yes"}`:                   "invalid_notch",
		`{"mascot":"../escape"}`:            "invalid_mascot",
		`not json`:                          "bad_request",
	} {
		rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", body)
		if rec.Code != http.StatusBadRequest || refusal.Error != code {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}

	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json; charset=utf-8",
		`{"hotkey":"cmd+shift+k","scope_app":null}`)
	if rec.Code != http.StatusOK || !snap.Exists || snap.Hotkey == nil || *snap.Hotkey != "cmd+shift+k" || snap.ScopeApp != nil {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json", `{"scope_app":""}`)
	if rec.Code != http.StatusOK || snap.Hotkey == nil || *snap.Hotkey != "cmd+shift+k" || snap.ScopeApp == nil || *snap.ScopeApp != "" {
		t.Fatalf("second write: %d %s", rec.Code, rec.Body)
	}
	// The way back to the native iTerm2 tab is a value the route takes.
	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json", `{"terminal":"iterm_native"}`)
	if rec.Code != http.StatusOK || snap.Terminal == nil || *snap.Terminal != "iterm_native" {
		t.Fatalf("iterm_native: %d %s", rec.Code, rec.Body)
	}

	// A file somebody broke by hand is refused and left as it was.
	broken := []byte(`{"hotkey": "cmd+shift+k",`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", `{"hotkey":"cmd+j"}`)
	if rec.Code != http.StatusConflict || refusal.Error != "settings_file_invalid" {
		t.Fatalf("broken file: %d %s", rec.Code, rec.Body)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(after) != string(broken) {
		t.Fatalf("broken file was replaced with %s", after)
	}

	rec, _, _ = settingsCall(t, s, http.MethodDelete, "", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestSettingsRouteCarriesWorkGateDefaultsAndUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}
	rec, _, _ := settingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"planning_gate":true`) ||
		!strings.Contains(rec.Body.String(), `"verify_gate":false`) {
		t.Fatalf("default gates: %d %s", rec.Code, rec.Body)
	}
	rec, _, refusal := settingsCall(t, s, http.MethodPost, "application/json",
		`{"planning_gate":false,"verify_gate":true}`)
	if rec.Code != http.StatusOK || refusal.Error != "" ||
		!strings.Contains(rec.Body.String(), `"planning_gate":false`) ||
		!strings.Contains(rec.Body.String(), `"verify_gate":true`) {
		t.Fatalf("updated gates: %d %s", rec.Code, rec.Body)
	}
	rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", `{"planning_gate":"off"}`)
	if rec.Code != http.StatusBadRequest || refusal.Error != "invalid_planning_gate" {
		t.Fatalf("wrong-shaped gate: %d %s", rec.Code, rec.Body)
	}
}

// Every row of the native settings window, through the one route it writes.
//
// The window is a web page and its rows are data (web/console/src/pages/settings/window);
// what makes a row real is that this route takes the value and hands it back. A
// key that reaches the file but comes back missing is a control that silently
// does nothing, which is the failure this walks the whole table to rule out.
func TestSettingsEveryWindowRow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}

	// One value per key, each inside what the file accepts.
	sent := map[string]any{}
	for _, key := range nextconfig.Settables {
		// What only the macOS shell acts on is refused by name elsewhere
		// (capabilities.go), and walked there by its own test.
		if _, shells := platformSettings[key.Name]; shells && runtime.GOOS != "darwin" {
			continue
		}
		switch key.Kind {
		case "string":
			switch {
			case len(key.Choices) > 0:
				sent[key.Name] = key.Choices[len(key.Choices)-1]
			case key.Name == "hotkey":
				sent[key.Name] = "cmd+shift+k"
			case key.Name == "scope_app":
				sent[key.Name] = "com.googlecode.iterm2,com.apple.Terminal"
			case key.Name == "output_font":
				sent[key.Name] = "Menlo"
			case key.Name == "remote_hostname":
				sent[key.Name] = "mac.example.com"
			default:
				sent[key.Name] = "clawd"
			}
		case "bool":
			sent[key.Name] = true
		case "number":
			sent[key.Name] = (key.Min + key.Max) / 2
		case "int":
			sent[key.Name] = int64((key.Min + key.Max) / 2)
		}
	}
	body, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	rec, _, refusal := settingsCall(t, s, http.MethodPost, "application/json", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("the whole window: %d %s %s", rec.Code, refusal.Error, rec.Body)
	}

	// Read it back off disk through the route, as the window does when it opens.
	rec, _, _ = settingsCall(t, s, http.MethodGet, "", "")
	var back map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	for name, want := range sent {
		got, ok := back[name]
		if !ok || got == nil {
			t.Fatalf("%s: written and not answered (%v)", name, got)
		}
		if wantNumber, isNumber := want.(float64); isNumber {
			if got != wantNumber {
				t.Fatalf("%s: sent %v, answered %v", name, want, got)
			}
			continue
		}
		if wantInt, isInt := want.(int64); isInt {
			if got != float64(wantInt) {
				t.Fatalf("%s: sent %v, answered %v", name, want, got)
			}
			continue
		}
		if got != want {
			t.Fatalf("%s: sent %v, answered %v", name, want, got)
		}
	}

	// `on_state_change` is read, never written: a hand edit reaches the window
	// as the statement the original window shows.
	file := filepath.Join(dir, "config.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	onDisk["on_state_change"] = []string{"/usr/local/bin/tell me", "--quiet"}
	edited, err := json.Marshal(onDisk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	rec, snap, _ := settingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || len(snap.OnStateChange) != 2 || snap.OnStateChange[0] != "/usr/local/bin/tell me" {
		t.Fatalf("on_state_change: %d %v", rec.Code, snap.OnStateChange)
	}
	rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json",
		`{"on_state_change":["rm","-rf","/"]}`)
	if rec.Code != http.StatusBadRequest || refusal.Error != "bad_request" {
		t.Fatalf("on_state_change is not writable here: %d %s", rec.Code, rec.Body)
	}

	// A key whose value is the wrong shape for it is left out of the answer
	// rather than guessed at, and the rest of the file still reads.
	onDisk["output_size"] = "eleven"
	edited, _ = json.Marshal(onDisk)
	if err := os.WriteFile(file, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	rec, snap, _ = settingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || snap.OutputSize != nil || snap.Mascot == nil {
		t.Fatalf("a key of the wrong shape: %d %s", rec.Code, rec.Body)
	}
}
