package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebhookCreationSavesTheSecretWithoutPrintingIt(t *testing.T) {
	const secret = "https://api.example.test/v1/schedule-webhook-trigger/swhk_test"
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprint(replay), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v1/orchestrator/schedule-webhooks/create" || r.Header.Get("Idempotency-Key") != "one" {
					t.Errorf("bad request: %v", r)
				}
				fmt.Fprintf(w, `{"schema":"clawdline.schedule_webhook.management.v1","hook":{"hook_id":"swh_test","state":"pending_binding","revision":0},"secret_available":%t,"public_url":%q}`, !replay, secret)
			}))
			defer server.Close()
			open := func(int) (*broker, error) {
				return &broker{base: server.URL, token: "orchestrator-credential", client: server.Client()}, nil
			}
			file := filepath.Join(t.TempDir(), "url")
			args := []string{"--url-file", file, "--key", "one", "--session", "conversation", "--run", "run"}
			var out, errs bytes.Buffer
			code := runWebhookCreate(&out, &errs, args, open)
			if strings.Contains(out.String()+errs.String(), secret) {
				t.Fatal("secret printed")
			}
			if replay {
				if code != 1 || !strings.Contains(errs.String(), "secret_unavailable hook_id=swh_test") {
					t.Fatalf("replay: %d %s", code, &errs)
				}
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatal("failed creation left a URL file")
				}
				return
			}
			data, err := os.ReadFile(file)
			if code != 0 || err != nil || string(data) != secret+"\n" {
				t.Fatalf("save failed: %d %v %s", code, err, &errs)
			}
			info, _ := os.Stat(file)
			if info.Mode().Perm() != 0600 {
				t.Fatalf("mode = %v", info.Mode())
			}
			if runWebhookCreate(&out, &errs, args, open) != 1 || calls != 1 {
				t.Fatal("existing URL file was overwritten or a second hook was requested")
			}
		})
	}
}
