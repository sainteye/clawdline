package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/taskresources"
)

func TestResourceCLIUsesTheReviewedFileWithoutReplanning(t *testing.T) {
	plan := taskresources.Plan{Digest: "reviewed plan", Items: []taskresources.Inspection{}}
	raw, _ := json.Marshal(plan)
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	execution := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Clawdline-Orchestrator") != "synthetic test credential" {
			t.Error("missing machine authority")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/orchestrator/resources/apply" {
			t.Error(r.Method, r.URL.Path)
		}
		var body struct {
			Plan      taskresources.Plan `json:"plan"`
			Execution string             `json:"execution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Plan.Digest != plan.Digest || body.Execution != execution {
			t.Error(body, err)
		}
		_, _ = w.Write([]byte(`{"replayed":true}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "synthetic test credential", client: server.Client()}
	var out, errs bytes.Buffer
	if code := runResources(&out, &errs, b, []string{"apply", "--plan", path, "--execution", execution}); code != 0 || calls != 1 {
		t.Fatal(code, calls, errs.String())
	}
	calls = 0
	if code := runResources(&out, &errs, b, []string{"apply", "--plan", path}); code != 2 || calls != 0 {
		t.Fatal(code, calls)
	}
	if err := os.WriteFile(path, append(raw, []byte(` {"unexpected":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runResources(&out, &errs, b, []string{"apply", "--plan", path, "--execution", execution}); code != 2 || calls != 0 {
		t.Fatal(code, calls)
	}
}
