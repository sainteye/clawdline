package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestListInfoReturnsOnlyTheSessionDisplayIdentity(t *testing.T) {
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantCodex, Label: "A named session", CWD: "/private/project", State: session.StateWorking}}
	s := paneServer(t, p)
	req := httptest.NewRequest("GET", "/v1/sessions/%2519/info?parts=list", nil)
	rec := httptest.NewRecorder()
	s.sessionInfoRoute(rec, req, "%19")
	if rec.Code != 200 {
		t.Fatalf("list info: %d %s", rec.Code, rec.Body.String())
	}
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	info, _ := reply["info"].(map[string]any)
	if len(info) != 2 {
		t.Fatalf("list info carries more than its session and empty models: %v", info)
	}
	identity, _ := info["session"].(map[string]any)
	if identity["id"] != "%19" || identity["title"] == "" || identity["assistant"] != "codex" ||
		identity["cwd"] != "/private/project" || len(identity) != 4 {
		t.Fatalf("list info exposes wrong or excess identity fields: %v", identity)
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
