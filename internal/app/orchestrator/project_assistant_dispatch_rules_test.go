package orchestrator

import (
	"errors"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func codingToCodex(c *AssistantCurrent) {
	c.Assistant = "claude"
	c.DispatchAgents = []pa.DispatchAgentOption{{Agent: pa.ClaudeCode, Efforts: []string{}}, {Agent: pa.Codex, Efforts: []string{"high", "xhigh"}}}
	c.SharedSettings = pa.SharedSettings{Rules: []pa.DispatchRule{{ID: "code", Kind: "coding", Agents: []pa.DispatchRuleAgent{{Agent: pa.Codex, Effort: "xhigh"}}, Scope: pa.SharedScope{All: true}}}}
}

func TestBotDispatchRuleStartsTheRulesAgentWithItsEffort(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	codingToCodex(c)
	q.Choice = pa.DispatchChoice{Kind: "coding"}
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.Assistant != "codex" || out.Record.ReasoningEffort != "xhigh" {
		t.Fatalf("rule dispatch = %s/%s, want codex/xhigh", out.Record.Assistant, out.Record.ReasoningEffort)
	}
}

func TestBotDispatchWithoutRulesStaysOnClaude(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	c.Assistant = "claude"
	q.Choice = pa.DispatchChoice{Kind: "coding"}
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil || out.Record.Assistant != "claude" || out.Record.ReasoningEffort != "" {
		t.Fatalf("no-rule dispatch = %+v, %v", out.Record.Assistant, err)
	}
}

func TestBotDispatchExplicitAgentBeatsTheRuleAndBadValuesAreRefused(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	codingToCodex(c)
	q.Choice = pa.DispatchChoice{Kind: "coding", Agent: "antigravity"}
	_, err := b.DispatchAssistant(ctx, q)
	var f *pa.Failure
	if !errors.As(err, &f) || f.Field != "dispatch_agent" || f.Allowed != "claude, codex" {
		t.Fatalf("unavailable agent = %v %+v", err, f)
	}
	q.Choice = pa.DispatchChoice{Kind: "coding", Agent: "claude"}
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil || out.Record.Assistant != "claude" {
		t.Fatalf("explicit agent = %v, %v", out.Record.Assistant, err)
	}
}

// A rule that sends work to Codex launches it with the person's dispatch
// permission. Before, the Bot carried Claude's Auto mode and Codex was refused.
func TestBotDispatchRuleToCodexUsesTheDispatchPermission(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	codingToCodex(c)
	q.Choice = pa.DispatchChoice{Kind: "coding"}
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.Assistant != "codex" || out.Record.PermissionMode != "full" {
		t.Fatalf("rule dispatch = %s/%s, want codex/full (the unset dispatch setting)", out.Record.Assistant, out.Record.PermissionMode)
	}
}
