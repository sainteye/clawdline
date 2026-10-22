package http

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
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
