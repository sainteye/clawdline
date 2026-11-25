package projectassistant

import (
	"errors"
	"strings"
	"testing"
)

var testOptions = []DispatchAgentOption{
	{Agent: ClaudeCode},
	{Agent: Codex, Efforts: []string{"high", "xhigh"}},
}

func codingRule() SharedSettings {
	return SharedSettings{Rules: []DispatchRule{
		{ID: "code", Kind: "coding", Agents: []DispatchRuleAgent{{Agent: Codex, Model: "gpt-5.5", Effort: "xhigh"}}, Scope: SharedScope{All: true}},
		{ID: "image", Kind: "image", Agents: []DispatchRuleAgent{{Agent: Grok}, {Agent: Gemini}, {Agent: Codex}}, Scope: SharedScope{All: true}},
	}}
}

func TestDispatchRuleChoosesAgentModelAndEffort(t *testing.T) {
	got, err := ResolveDispatch(codingRule(), testOptions, "p1", "claude", DispatchChoice{Kind: "Coding"})
	if err != nil || got.Assistant != "codex" || got.Model != "gpt-5.5" || got.Effort != "xhigh" || got.RuleID != "code" || got.Source != "rule" {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Ordered fallback skips agents this machine cannot dispatch.
	got, err = ResolveDispatch(codingRule(), testOptions, "p1", "claude", DispatchChoice{Kind: "image"})
	if err != nil || got.Assistant != "codex" || got.RuleID != "image" {
		t.Fatalf("fallback got %+v, %v", got, err)
	}
}

func TestExplicitDispatchArgumentsBeatRules(t *testing.T) {
	got, err := ResolveDispatch(codingRule(), testOptions, "p1", "claude", DispatchChoice{Kind: "coding", Agent: "claude"})
	if err != nil || got.Assistant != "claude" || got.Model != "" || got.Effort != "" || got.Source != "explicit" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = ResolveDispatch(codingRule(), testOptions, "p1", "claude", DispatchChoice{Kind: "coding", Effort: "high", Model: "gpt-5.4"})
	if err != nil || got.Assistant != "codex" || got.Effort != "high" || got.Model != "gpt-5.4" {
		t.Fatalf("explicit effort got %+v, %v", got, err)
	}
}

func TestNoRulesKeepsClaudeCode(t *testing.T) {
	for _, c := range []DispatchChoice{{}, {Kind: "coding"}} {
		got, err := ResolveDispatch(SharedSettings{}, testOptions, "p1", "claude", c)
		if err != nil || got != (ResolvedDispatch{Assistant: "claude", Source: "default"}) {
			t.Fatalf("%+v: got %+v, %v", c, got, err)
		}
	}
	got, _ := ResolveDispatch(codingRule(), testOptions, "p1", "claude", DispatchChoice{Kind: "design"})
	if got.Assistant != "claude" || got.Source != "default" {
		t.Fatalf("an unmatched kind got %+v", got)
	}
}

func TestInvalidDispatchArgumentsAreRefusedWithAllowedValues(t *testing.T) {
	cases := []struct {
		choice  DispatchChoice
		field   string
		allowed []string
	}{
		{DispatchChoice{Agent: "grok"}, "dispatch_agent", []string{"claude", "codex"}},
		{DispatchChoice{Agent: "codex", Effort: "max"}, "dispatch_effort", []string{"high", "xhigh"}},
		{DispatchChoice{Effort: "high"}, "dispatch_effort", nil},
		{DispatchChoice{Model: "GPT 5"}, "dispatch_model", nil},
	}
	for _, c := range cases {
		_, err := ResolveDispatch(codingRule(), testOptions, "p1", "claude", c.choice)
		var f *Failure
		if !errors.As(err, &f) || f.Code != Invalid || f.Field != c.field || f.Allowed != strings.Join(c.allowed, ", ") {
			t.Fatalf("%+v: got %v %+v", c.choice, err, f)
		}
	}
	only := SharedSettings{Rules: []DispatchRule{{ID: "g", Kind: "image", Agents: []DispatchRuleAgent{{Agent: Grok}}, Scope: SharedScope{All: true}}}}
	_, err := ResolveDispatch(only, testOptions, "p1", "claude", DispatchChoice{Kind: "image"})
	if Code(err) != Unsupported {
		t.Fatalf("a rule with no dispatchable agent must not silently run Claude: %v", err)
	}
}

func TestProjectRulesAndOverridesComeFirst(t *testing.T) {
	s := codingRule()
	s.Rules = append(s.Rules, DispatchRule{ID: "p", Kind: "coding", Agents: []DispatchRuleAgent{{Agent: ClaudeCode}}, Scope: SharedScope{Projects: []string{"p2"}}})
	if got, _ := ResolveDispatch(s, testOptions, "p2", "claude", DispatchChoice{Kind: "coding"}); got.RuleID != "p" {
		t.Fatalf("project rule got %+v", got)
	}
	s.ProjectOverrides = map[string]ProjectSettingsOverride{"p3": {Rules: []DispatchRule{{ID: "o", Kind: "coding", Agents: []DispatchRuleAgent{{Agent: ClaudeCode}}}}}}
	if got, _ := ResolveDispatch(s, testOptions, "p3", "claude", DispatchChoice{Kind: "coding"}); got.RuleID != "o" {
		t.Fatalf("override got %+v", got)
	}
}

func TestSharedMemoryAppliesToChosenProjectsAndNewOnesByDefault(t *testing.T) {
	s := SharedSettings{Memory: []SharedMemoryEntry{
		{ID: "a", Text: "Answer with the conclusion first", Scope: SharedScope{All: true}},
		{ID: "b", Text: "Use the design tokens", Scope: SharedScope{Projects: []string{"web"}}},
		{ID: "c", Text: "Nowhere", Scope: SharedScope{}},
	}}
	if got := SharedMemoryOpening(s, "project-added-later", true); !strings.Contains(got, "conclusion first") || strings.Contains(got, "design tokens") || strings.Contains(got, "Nowhere") {
		t.Fatalf("new project got %q", got)
	}
	if got := SharedMemoryOpening(s, "web", true); !strings.Contains(got, "design tokens") || !strings.Contains(got, "take precedence") {
		t.Fatalf("web got %q", got)
	}
	if got := SharedMemoryOpening(s, "web", false); got != "" {
		t.Fatalf("a Bot that turned shared memory off got %q", got)
	}
}

func TestSharedSettingsBounds(t *testing.T) {
	var s SharedSettings
	for i := range MaxSharedMemoryEntries + 1 {
		s.Memory = append(s.Memory, SharedMemoryEntry{ID: "m" + string(rune('a'+i)), Text: "x", Scope: SharedScope{All: true}})
	}
	if Code(ValidateSharedSettings(s)) != CapacityFull {
		t.Fatal("too many entries accepted")
	}
	s.Memory = []SharedMemoryEntry{{ID: "a", Text: strings.Repeat("x", MaxSharedMemoryEntryBytes+1), Scope: SharedScope{All: true}}}
	if ValidateSharedSettings(s) == nil {
		t.Fatal("an oversized entry was accepted")
	}
	if ValidateSharedSettings(codingRule()) != nil {
		t.Fatal("valid rules refused")
	}
}

func TestABotThatTurnsSharedMemoryOffGetsNothing(t *testing.T) {
	s := SharedSettings{Memory: []SharedMemoryEntry{{ID: "a", Text: "Answer with the conclusion first", Scope: SharedScope{All: true}}}, MemoryOptOut: []string{"quiet"}}
	if got := SharedMemoryOpening(s, "p", s.UsesSharedMemory("quiet")); got != "" {
		t.Fatalf("an opted-out Bot got %q", got)
	}
	if got := SharedMemoryOpening(s, "p", s.UsesSharedMemory("new-bot")); got == "" {
		t.Fatal("a new Bot must use shared memory by default")
	}
}

func TestSavedDefaultRunsUnmatchedWorkAndYieldsWhenUnavailable(t *testing.T) {
	s := codingRule()
	s.Default = &DispatchRuleAgent{Agent: Codex, Model: "gpt-5.5", Effort: "high"}
	for _, c := range []DispatchChoice{{}, {Kind: "design"}} {
		got, err := ResolveDispatch(s, testOptions, "p1", "claude", c)
		if err != nil || got != (ResolvedDispatch{Assistant: "codex", Model: "gpt-5.5", Effort: "high", Source: "default"}) {
			t.Fatalf("%+v: got %+v, %v", c, got, err)
		}
	}
	// A rule still wins over the default.
	if got, _ := ResolveDispatch(s, testOptions, "p1", "claude", DispatchChoice{Kind: "coding"}); got.Source != "rule" || got.Effort != "xhigh" {
		t.Fatalf("rule lost to the default: %+v", got)
	}
	// A default agent this machine cannot dispatch falls back to the policy agent.
	s.Default = &DispatchRuleAgent{Agent: Grok}
	if got, err := ResolveDispatch(s, testOptions, "p1", "claude", DispatchChoice{}); err != nil || got != (ResolvedDispatch{Assistant: "claude", Source: "default"}) {
		t.Fatalf("unavailable default got %+v, %v", got, err)
	}
}

func TestSavedDefaultIsValidated(t *testing.T) {
	for _, d := range []DispatchRuleAgent{{Agent: "nobody"}, {Agent: Codex, Model: "Has Space"}, {Agent: Codex, Effort: strings.Repeat("x", 17)}} {
		if err := ValidateSharedSettings(SharedSettings{Default: &d}); Code(err) != Invalid {
			t.Fatalf("%+v accepted: %v", d, err)
		}
	}
	if err := ValidateSharedSettings(SharedSettings{Default: &DispatchRuleAgent{Agent: Grok}}); err != nil {
		t.Fatalf("a valid default was refused: %v", err)
	}
}
