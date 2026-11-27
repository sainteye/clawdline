package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

// An Antigravity conversation whose transcript is on disk but not read yet is
// not_yet_read, like Claude and Codex, not an unknown session.
func TestUsageSessionKnowsAnUnreadAntigravityTranscript(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	id := "a1111111-1111-4111-8111-111111111111"
	logs := filepath.Join(u.home, ".gemini", "antigravity-cli", "brain", id, ".system_generated", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "transcript_full.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := u.get("/v1/usage/sessions/"+id, nil)
	var got contract.UsageSession
	if code != http.StatusOK || json.Unmarshal(body, &got) != nil || got.Reason != contract.UsageReasonNotYetRead {
		t.Fatalf("%d %s", code, body)
	}
	// A conversation with no transcript anywhere stays unknown.
	if code, body = u.get("/v1/usage/sessions/a2222222-2222-4222-8222-222222222222", nil); code != http.StatusNotFound {
		t.Fatalf("missing transcript: %d %s", code, body)
	}
}
