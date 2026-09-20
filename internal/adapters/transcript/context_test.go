package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The expectations here are the Swift app's `SessionInfo.claudeContext`,
// `SessionInfo.claudeWindow` and the context half of `SessionInfo.codexFacts`.

func statusCache(t *testing.T, sessionID, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "statusline-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session-"+sessionID+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestClaudeContextCountsTheNewestTurnAgainstTheCachesExactWindow(t *testing.T) {
	path := writeRecord(t,
		assistantTurn("claude-opus-5", 9, 9, 9, 9),
		assistantTurn("claude-opus-5", 2, 1000, 535_783, 708),
	)
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Fill == nil || facts.Fill.UsedTokens != 536_493 {
		t.Fatalf("fill = %+v, want the newest turn's input side", facts.Fill)
	}
	home := statusCache(t, "abc", `{"context_window":{"context_window_size":1000000,"total_input_tokens":1,"used_percentage":99}}`)
	got, ok := ClaudeContext(facts.Fill, ReadClaudeStatusLine(home, "abc"), facts.Model)
	if !ok {
		t.Fatal("no context")
	}
	want := Context{UsedPercent: 53.6493, UsedTokens: 536_493, HasUsedTokens: true,
		WindowTokens: 1_000_000, WindowIsExact: true}
	if got != want {
		t.Fatalf("context = %+v, want %+v (the transcript's used side, not the cache's)", got, want)
	}
}

func TestAGuessedWindowIsMarkedGuessedAndAnUnknownModelHasNoContext(t *testing.T) {
	fill := &Fill{UsedTokens: 100_000}
	got, ok := ClaudeContext(fill, ClaudeStatusLine{}, "claude-haiku-4-5-20251001")
	if !ok || got.WindowTokens != 200_000 || got.WindowIsExact || got.UsedPercent != 50 {
		t.Fatalf("context = %+v, %v; want a guessed 200k window", got, ok)
	}
	if _, ok := ClaudeContext(fill, ClaudeStatusLine{}, "some-other-model"); ok {
		t.Fatal("a model with no window row must have no context at all")
	}
	if _, ok := ClaudeContext(nil, ClaudeStatusLine{}, "claude-opus-5"); ok {
		t.Fatal("a window with nothing to measure against it is not a reading")
	}
}

func TestARefusedTurnDoesNotReadAsAnEmptyWindow(t *testing.T) {
	// The all-zero `usage` of a `<synthetic>` turn satisfies every other
	// guard, and it arrives exactly when the window is full.
	path := writeRecord(t,
		assistantTurn("claude-opus-5", 2, 1000, 900_000, 0),
		assistantTurn("<synthetic>", 0, 0, 0, 0),
	)
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Fill == nil || facts.Fill.UsedTokens != 900_002 {
		t.Fatalf("fill = %+v, want the turn before the refusal", facts.Fill)
	}
}

func TestASidechainTurnIsADifferentConversation(t *testing.T) {
	side := assistantTurn("claude-opus-5", 7, 0, 0, 0)
	side["isSidechain"] = true
	path := writeRecord(t, assistantTurn("claude-opus-5", 5, 0, 0, 0), side)
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Fill == nil || facts.Fill.UsedTokens != 5 {
		t.Fatalf("fill = %+v, want the parent conversation's turn", facts.Fill)
	}
}

func TestBeforeTheFirstTurnTheCacheIsTheOnlyReading(t *testing.T) {
	home := statusCache(t, "abc", `{"context_window":{"context_window_size":200000,"total_input_tokens":50000}}`)
	got, ok := ClaudeContext(nil, ReadClaudeStatusLine(home, "abc"), "claude-opus-5")
	if !ok || got.UsedPercent != 25 || got.UsedTokens != 50_000 || !got.WindowIsExact {
		t.Fatalf("context = %+v, %v", got, ok)
	}

	// A writer that supplied only a percentage gets a partial answer rather
	// than a token count nobody wrote down.
	home = statusCache(t, "old", `{"context_window":{"context_window_size":200000,"used_percentage":41.5}}`)
	got, ok = ClaudeContext(nil, ReadClaudeStatusLine(home, "old"), "claude-opus-5")
	if !ok || got.UsedPercent != 41.5 || got.HasUsedTokens {
		t.Fatalf("context = %+v, %v", got, ok)
	}
}

func TestAFullWindowIsHundredPercentAndNotMore(t *testing.T) {
	got, ok := ClaudeContext(&Fill{UsedTokens: 1_400_000}, ClaudeStatusLine{}, "claude-opus-5")
	if !ok || got.UsedPercent != 100 {
		t.Fatalf("context = %+v, %v, want a clamped 100", got, ok)
	}
}

func TestAnAbsentOrOversizedStatusCacheIsUnknownAndNotZero(t *testing.T) {
	home := statusCache(t, "abc", `{"cost":{"total_cost_usd":1.5}}`)
	if status := ReadClaudeStatusLine(home, "missing"); status.Found {
		t.Fatal("a session with no cache file must not be Found")
	}
	if status := ReadClaudeStatusLine(home, "../escape"); status.Found {
		t.Fatal("a session id that is a path must not be read")
	}
	big := statusCache(t, "big", `{"context_window":{"context_window_size":1000000},"pad":"`+
		strings.Repeat("x", statusCacheLimit)+`"}`)
	status := ReadClaudeStatusLine(big, "big")
	if status.Found || status.HasWindowTokens {
		t.Fatalf("status = %+v, want nothing from a file past the read limit", status)
	}
}

func TestCodexReadsBothSidesOfItsOwnWindow(t *testing.T) {
	path := writeRecord(t,
		m{"type": "turn_context", "payload": m{"type": "turn_context", "model": "gpt-5.6-sol"}},
		m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{
			"total_token_usage":    m{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
			"last_token_usage":     m{"total_tokens": 17_703},
			"model_context_window": 258_400,
		}}},
	)
	facts, err := NewRecordFacts().Read(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Context == nil {
		t.Fatal("no context")
	}
	got := *facts.Context
	if got.UsedTokens != 17_703 || got.WindowTokens != 258_400 || !got.WindowIsExact {
		t.Fatalf("context = %+v", got)
	}
	if got.UsedPercent < 6.8 || got.UsedPercent > 6.9 {
		t.Fatalf("usedPercent = %v", got.UsedPercent)
	}
}

func TestACodexRolloutThatNamedNoWindowHasNoContext(t *testing.T) {
	path := writeRecord(t, m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{
		"total_token_usage": m{"total_tokens": 120}, "last_token_usage": m{"total_tokens": 7}}}})
	facts, err := NewRecordFacts().Read(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Context != nil {
		t.Fatalf("context = %+v, want none", facts.Context)
	}
}
