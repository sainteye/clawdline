package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestNewCodexInfoShowsModelWithoutTranscript(t *testing.T) {
	screen, err := os.ReadFile("../../domain/session/testdata/menu-codex-composer.txt")
	if err != nil {
		t.Fatal(err)
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantCodex, State: session.StateIdle}, screen: string(screen)}
	s := paneServer(t, p)
	for _, parts := range []string{"summary", "all"} {
		req := httptest.NewRequest("GET", "/v1/sessions/%2519/info?parts="+parts, nil)
		rec := httptest.NewRecorder()
		s.sessionInfoRoute(rec, req, "%19")
		var reply struct {
			Info struct {
				Session struct {
					Model  string `json:"model"`
					Effort string `json:"effort"`
				} `json:"session"`
				Usage   any `json:"usage"`
				Context any `json:"context"`
			} `json:"info"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || reply.Info.Session.Model != "gpt-5.6-sol" || reply.Info.Session.Effort != "high" || reply.Info.Usage != nil || reply.Info.Context != nil {
			t.Fatalf("new session info: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestClaudeInfoCarriesLiveStatusEffortWithoutATranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "statusline-cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session-test-effort.json"), []byte(`{"effort":{"level":"high"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, ConversationID: "test-effort", State: session.StateIdle}}
	s := paneServer(t, p)
	for _, parts := range []string{"summary", "all"} {
		rec := httptest.NewRecorder()
		s.sessionInfoRoute(rec, httptest.NewRequest("GET", "/v1/sessions/%2519/info?parts="+parts, nil), "%19")
		var reply struct {
			Info struct {
				Session struct {
					Effort string `json:"effort"`
				} `json:"session"`
			} `json:"info"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || reply.Info.Session.Effort != "high" {
			t.Fatalf("Claude effort: %d %s", rec.Code, rec.Body.String())
		}
	}
}

// What `/v1/sessions/{id}/info` may say about a context window, and what it
// may not. The Swift app's `SessionInfo.infoPayload` draws the same line.

func TestAGuessedWindowDoesNotGoOnTheWire(t *testing.T) {
	facts := transcript.Facts{Model: "claude-opus-5", Fill: &transcript.Fill{UsedTokens: 162_277}}
	got := wireSessionContext(facts, session.AssistantClaude, transcript.ClaudeStatusLine{})
	if got == nil {
		t.Fatal("a guessed window still has a percentage to report")
	}
	if got.UsedPercent < 16.2 || got.UsedPercent > 16.3 {
		t.Fatalf("usedPercent = %v", got.UsedPercent)
	}
	if got.WindowTokens != 0 {
		t.Fatalf("windowTokens = %d, want it withheld: this build guessed it", got.WindowTokens)
	}
	// The key has to be absent, not zero: a client that draws `x / y tokens`
	// reads it as a measurement.
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["windowTokens"]; ok {
		t.Fatalf("wire = %s, want no windowTokens key at all", body)
	}
	if _, ok := wire["usedPercent"]; !ok {
		t.Fatalf("wire = %s, want usedPercent", body)
	}
}

func TestAnExactWindowGoesOnTheWireWithBothSides(t *testing.T) {
	status := transcript.ClaudeStatusLine{Found: true, WindowTokens: 1_000_000, HasWindowTokens: true}
	facts := transcript.Facts{Model: "claude-opus-5", Fill: &transcript.Fill{UsedTokens: 162_277}}
	got := wireSessionContext(facts, session.AssistantClaude, status)
	if got == nil || got.WindowTokens != 1_000_000 || got.UsedTokens != 162_277 {
		t.Fatalf("context = %+v", got)
	}
}

func TestCodexCarriesItsOwnWindowAndNeedsNoStatusCache(t *testing.T) {
	at := transcript.Context{UsedPercent: 6.85, UsedTokens: 17_703, HasUsedTokens: true,
		WindowTokens: 258_400, WindowIsExact: true}
	got := wireSessionContext(transcript.Facts{Context: &at}, session.AssistantCodex, transcript.ClaudeStatusLine{})
	if got == nil || got.WindowTokens != 258_400 || got.UsedTokens != 17_703 {
		t.Fatalf("context = %+v", got)
	}
}

func TestASessionWithNothingToMeasureDrawsNoContextAtAll(t *testing.T) {
	// A model with no window row and no status line to ask: the key is
	// absent, so the status line leaves the cell out rather than drawing a
	// permanent green 0%.
	facts := transcript.Facts{Model: "claude-some-unreleased-thing", Fill: &transcript.Fill{UsedTokens: 9}}
	if got := wireSessionContext(facts, session.AssistantClaude, transcript.ClaudeStatusLine{}); got != nil {
		t.Fatalf("context = %+v, want none", got)
	}
	// A Codex rollout that named no window is the same answer.
	if got := wireSessionContext(transcript.Facts{}, session.AssistantCodex, transcript.ClaudeStatusLine{}); got != nil {
		t.Fatalf("context = %+v, want none", got)
	}
}
