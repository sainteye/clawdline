package orchestrator

import (
	"strings"
	"testing"
)

func TestAChildUsesTheMachinesDefaultModelUnlessItsBriefOverridesIt(t *testing.T) {
	b, _ := newTestBroker(t)
	b.DefaultReasoningEffort = func() string { return "high" }
	b.DefaultModel = func(assistant string) string {
		if assistant == "codex" {
			return "gpt-6-sol"
		}
		return "claude-opus-5-5"
	}
	r, line := dispatchWith(t, b, "c2800011-0000-4000-8000-000000000011", "codex", nil)
	if r.Model != "gpt-6-sol" || r.ReasoningEffort != "high" || !strings.Contains(line, " codex --model gpt-6-sol --config model_reasoning_effort=high") {
		t.Fatalf("machine default: record=%q line=%s", r.Model, line)
	}

	b, _ = newTestBroker(t)
	b.DefaultModel = func(string) string { return "gpt-6-sol" }
	b.DefaultReasoningEffort = func() string { return "high" }
	r, line = dispatchWith(t, b, "c2800012-0000-4000-8000-000000000012", "codex",
		map[string]any{"model": "gpt-5.6-luna", "reasoning_effort": "xhigh"})
	if r.Model != "gpt-5.6-luna" || !strings.Contains(line, "--model gpt-5.6-luna") || strings.Contains(line, "gpt-6-sol") || r.ReasoningEffort != "xhigh" || !strings.Contains(line, "model_reasoning_effort=xhigh") {
		t.Fatalf("brief override: record=%q line=%s", r.Model, line)
	}
}

func TestABrokerOpenedSessionUsesTheMachinesDefaultModel(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.DefaultModel = func(assistant string) string {
		if assistant == "claude" {
			return "claude-opus-5-5"
		}
		return "gpt-6-sol"
	}
	launcher := &recordingLauncher{pane: "%93"}
	b.Launcher = launcher
	if _, err := b.openSession(ctx, t.TempDir(), "clawdline-root-default-model", "claude", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launcher.line(), " claude --model claude-opus-5-5") {
		t.Fatalf("machine default: %s", launcher.line())
	}
	if _, err := b.openSession(ctx, t.TempDir(), "clawdline-root-explicit-model", "claude", "sonnet", "", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launcher.line(), " claude --model sonnet") || strings.Contains(launcher.line(), "claude-opus-5-5") {
		t.Fatalf("explicit override: %s", launcher.line())
	}
}
