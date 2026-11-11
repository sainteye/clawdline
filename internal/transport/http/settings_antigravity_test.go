package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestAntigravityDefaultsThroughAuthenticatedRoute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	f, _ := newGateFixture(t)
	var err error
	_, f.read, err = f.g.auth.AddDeviceWithPermission("explicit reader", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "next")}}
	h := f.g.wrap(http.HandlerFunc(s.defaultModelsRoute))
	request := func(token, method, body string) map[string]any {
		t.Helper()
		r := call{method: method, path: "/v1/settings/default-models", body: body,
			headers: map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}}.do(h)
		want := 200
		if token == f.read && method == http.MethodPost {
			want = 403
		}
		if r.Code != want {
			t.Fatalf("route %s: %d %s", method, r.Code, r.Body)
		}
		var answer map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	request(f.read, http.MethodPost, `{"antigravity_default_model":"native-synthetic"}`)
	for _, key := range []string{"antigravity_default_model", "antigravity_default_effort"} {
		value := "native-synthetic"
		if strings.HasSuffix(key, "effort") {
			value = "max"
		}
		answer := request(f.send, http.MethodPost, `{"`+key+`":"`+value+`"}`)
		if answer[key] != value {
			t.Fatalf("field not retained: %#v", answer)
		}
	}
	answer := request(f.read, http.MethodGet, "")
	if answer["antigravity_default_model"] != "native-synthetic" || answer["antigravity_default_effort"] != "max" {
		t.Fatalf("readback: %#v", answer)
	}
	models := answer["models"].(map[string]any)
	if rows, ok := models["antigravity"].([]any); !ok || len(rows) != 0 {
		t.Fatal("unknown native catalog must be an empty array")
	}
	for _, body := range []string{`{"antigravity_default_model":"INVALID MODEL"}`, `{"antigravity_default_effort":"ultra"}`, `{"antigravity_default_effort":42}`, `{"remote":true}`} {
		r := defaultModelsCall(t, s, http.MethodPost, "application/json", body)
		if r.Code != 400 {
			t.Fatalf("invalid body accepted: %d %s", r.Code, r.Body)
		}
	}
	answer = request(f.local, http.MethodPost, `{"antigravity_default_model":"","antigravity_default_effort":""}`)
	if answer["antigravity_default_model"] != "" || answer["antigravity_default_effort"] != "" {
		t.Fatal("native default not restored")
	}
}

func TestSavedProviderEffortReachesBothLaunchCompositionsWithoutRestart(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, icons: icon.NewRegistry()}
	callbacks := []struct {
		name string
		read func(string) string
	}{
		{"console", s.starter(startReading{}).DefaultReasoningEffort},
		{"broker", newBroker(s).DefaultReasoningEffort},
	}
	for _, update := range []struct{ body, native string }{
		{`{"codex_default_effort":"xhigh","antigravity_default_effort":"medium"}`, "medium"},
		{`{"antigravity_default_effort":"max"}`, "max"},
		{`{"antigravity_default_effort":""}`, ""},
	} {
		response := defaultModelsCall(t, s, http.MethodPost, "application/json", update.body)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body)
		}
		for _, callback := range callbacks {
			for assistant, want := range map[string]string{"antigravity": update.native, "codex": "xhigh", "claude": "", "foreign": ""} {
				if got := callback.read(assistant); got != want {
					t.Errorf("%s/%s got %q, want %q", callback.name, assistant, got, want)
				}
			}
		}
	}
}

func TestAntigravityDefaultsWithInstalledCatalog(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	agyScript := "#!/bin/sh\nif [ \"$1\" = \"models\" ]; then\n  printf \"gemini-3.8-flash-medium\\tGemini 3.8 Flash (Medium)\\ngemini-4-argon-high\\tGemini 4 Argon (High)\\n\"\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "agy"), []byte(agyScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)

	s := &Server{cfg: config.Config{Dir: filepath.Join(home, "next")}}
	response := defaultModelsCall(t, s, http.MethodGet, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("get default models: %d %s", response.Code, response.Body)
	}
	var answer map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	models, ok := answer["models"].(map[string]any)
	if !ok {
		t.Fatalf("missing models map: %#v", answer)
	}
	nativeModels, ok := models["antigravity"].([]any)
	if !ok || len(nativeModels) < 2 {
		t.Fatalf("expected antigravity models, got: %#v", models["antigravity"])
	}
	first := nativeModels[0].(map[string]any)
	if first["value"] != "gemini-3.8-flash-medium" || first["label"] != "Gemini 3.8 Flash (Medium)" {
		t.Fatalf("unexpected first model: %#v", first)
	}

	saveResp := defaultModelsCall(t, s, http.MethodPost, "application/json", `{"antigravity_default_model":"gemini-3.8-flash-medium"}`)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("save antigravity default model: %d %s", saveResp.Code, saveResp.Body)
	}
	var savedAnswer map[string]any
	if err := json.Unmarshal(saveResp.Body.Bytes(), &savedAnswer); err != nil {
		t.Fatal(err)
	}
	if savedAnswer["antigravity_default_model"] != "gemini-3.8-flash-medium" {
		t.Fatalf("default model not saved: %#v", savedAnswer)
	}
}
