package projectassistant

import (
	"encoding/json"
	"strings"
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

func TestSafeAttemptClosedRefusesEveryObservedEffect(t *testing.T) {
	clean := AttemptFact{Stage: AttemptClosed, Effect: "none", CleanupConfirmed: true, Reason: "usage_limit"}
	if !SafeAttemptClosed(Turn{Attempts: []AttemptFact{clean}}) {
		t.Fatal("authoritative cleaned refusal was rejected")
	}
	for _, change := range []func(*AttemptFact){
		func(f *AttemptFact) { f.SendStarted = true },
		func(f *AttemptFact) { f.SendReturned = true },
		func(f *AttemptFact) { f.NativeActivity = true },
		func(f *AttemptFact) { f.ToolActivity = true },
		func(f *AttemptFact) { f.AnswerObserved = true },
		func(f *AttemptFact) { f.CleanupConfirmed = false },
		func(f *AttemptFact) { f.Effect = "unknown" },
	} {
		fact := clean
		change(&fact)
		if SafeAttemptClosed(Turn{Attempts: []AttemptFact{fact}}) {
			t.Fatalf("observed effect authorized another attempt: %+v", fact)
		}
	}
}

func TestSelectedFallbacksUseDistinctLoginsAndPreserveLegacyOmission(t *testing.T) {
	first, second := strings.Repeat("1", 32), strings.Repeat("2", 32)
	primary := FallbackStep{Agent: Codex, LoginProfile: first}
	steps := []FallbackStep{{Agent: Codex, LoginProfile: second}, {Agent: ClaudeCode}}
	if err := ValidateFallbackSteps(primary, steps); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]FallbackStep{{primary}, {steps[0], steps[0]}, {{Agent: Codex, LoginProfile: "unknown"}}} {
		if err := ValidateFallbackSteps(primary, bad); err == nil {
			t.Fatalf("accepted duplicate or malformed login %+v", bad)
		}
	}
	bot := Bot{Agent: Codex, LoginProfile: first, Name: "Synthetic", Appearance: Appearance{Blob, EntryColor}, Version: 1, Instructions: InstructionSnapshot{Revision: 1}}
	edit := EditBot{ExpectedVersion: 1, Name: bot.Name, Appearance: bot.Appearance, FallbackSteps: &steps}
	next, err := ApplyEdit(bot, edit)
	if err != nil || next.FallbackSteps.Steps()[0] != steps[0] || next.Instructions.Revision != 2 {
		t.Fatalf("new chain: %+v %v", next, err)
	}
	empty := []Agent{}
	oldEdit := EditBot{ExpectedVersion: next.Version, Name: next.Name, Appearance: next.Appearance, FallbackAgents: &empty}
	preserved, err := ApplyEdit(next, oldEdit)
	if err != nil || preserved.FallbackSteps != next.FallbackSteps {
		t.Fatalf("old empty client erased new logins: %+v %v", preserved, err)
	}
	legacy := []Agent{ClaudeCode}
	oldEdit.ExpectedVersion = preserved.Version
	oldEdit.FallbackAgents = &legacy
	if _, err = ApplyEdit(preserved, oldEdit); Code(err) != Invalid {
		t.Fatalf("conflicting old writer accepted: %v", err)
	}
	clear := []FallbackStep{}
	oldEdit.ExpectedVersion = preserved.Version
	oldEdit.FallbackSteps = &clear
	migrated, err := ApplyEdit(preserved, oldEdit)
	if err != nil || len(migrated.FallbackSteps.Steps()) != 0 || len(migrated.FallbackAgents.Agents()) != 1 {
		t.Fatalf("explicit reverse migration failed: %+v %v", migrated, err)
	}
	raw, err := json.Marshal(Bot{Agent: Codex})
	if err != nil || strings.Contains(string(raw), "fallback_steps") {
		t.Fatalf("new zero field changed legacy canonical input: %s %v", raw, err)
	}
	var round Bot
	raw, err = json.Marshal(next)
	if err != nil || json.Unmarshal(raw, &round) != nil || round.FallbackSteps != next.FallbackSteps {
		t.Fatal("selected identities lost JSON value semantics")
	}
}
