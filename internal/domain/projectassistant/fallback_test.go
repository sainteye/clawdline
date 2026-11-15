package projectassistant

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFallbackConfigDefaultsAndUniqueIdentities(t *testing.T) {
	for _, agents := range [][]Agent{nil, {Codex}} {
		if err := ValidateFallbacks(ClaudeCode, agents); err != nil {
			t.Fatal(err)
		}
	}
	for _, agents := range [][]Agent{{ClaudeCode}, {Codex, Codex}, {Agent("custom")}, {Codex, ClaudeCode, Codex, ClaudeCode}} {
		if err := ValidateFallbacks(ClaudeCode, agents); err == nil {
			t.Fatalf("accepted invalid identities: %v", agents)
		}
	}
	var old Bot
	if err := json.Unmarshal([]byte(`{"agent":"claude"}`), &old); err != nil {
		t.Fatal(err)
	}
	if len(old.FallbackAgents.Agents()) != 0 {
		t.Fatal("old row enabled fallback")
	}
	config := FallbackConfigOf([]Agent{Codex})
	raw, err := json.Marshal(config)
	if err != nil || string(raw) != `["codex"]` {
		t.Fatalf("wire: %s %v", raw, err)
	}
	var round FallbackConfig
	if err = json.Unmarshal(raw, &round); err != nil || round != config {
		t.Fatal("config lost value semantics")
	}
}

func TestFallbackRestMatchesUpstreamCadence(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, reason := range []string{"rate_limited", "overloaded"} {
		var previous *AgentRest
		for _, seconds := range []int{60, 300, 1500, 3600, 3600} {
			rest := RestAfter("m", ClaudeCode, reason, now, previous)
			if rest.Until.Sub(now) != time.Duration(seconds)*time.Second {
				t.Fatalf("%s backoff: %+v", reason, rest)
			}
			previous = rest
		}
		next := RestAfter("m", ClaudeCode, reason, previous.Until.Add(time.Hour), previous)
		if next.Failures != 1 || next.Until.Sub(previous.Until.Add(time.Hour)) != time.Minute {
			t.Fatal("backoff did not reset an hour after prior expiry")
		}
	}
	for reason, seconds := range map[string]int{"usage_limit": 3600, "out_of_credits": 21600, "signed_out": 3600} {
		rest := RestAfter("m", Codex, reason, now, nil)
		if rest.Until.Sub(now) != time.Duration(seconds)*time.Second {
			t.Fatalf("%s: %+v", reason, rest)
		}
	}
	for _, reason := range []string{"model_missing", "unsupported", "", "timeout"} {
		if rest := RestAfter("m", Codex, reason, now, nil); rest != nil {
			t.Fatalf("unexpected global rest: %+v", rest)
		}
	}
}
