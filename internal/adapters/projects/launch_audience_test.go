package projects

import (
	"strings"
	"testing"
)

func TestLaunchAudienceOmitsInteractiveRulesForAutomatedWorkers(t *testing.T) {
	for _, audience := range []LaunchAudience{AudienceChild, AudienceGateChecker} {
		t.Run("codex_"+string(audience), func(t *testing.T) {
			launch, err := Admit(LaunchRequest{
				Audience:                audience,
				ProjectRoot:             "/workspace/demo",
				Assistant:               AssistantCodex,
				Language:                "zh-Hant-TW",
				Memory:                  "Synthetic project memory",
				IntegrationInstructions: "Synthetic workflow rules",
			})
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(launch.Arguments, " ")
			if strings.Contains(joined, "clawdline guide note") || strings.Contains(joined, "answer Note") {
				t.Fatalf("automated Codex worker received Note instruction: %s", joined)
			}
			if strings.Contains(joined, "Clawdline Board item title") {
				t.Fatalf("automated Codex worker received Board-authoring language instruction: %s", joined)
			}
			if !strings.Contains(joined, "Synthetic project memory") || !strings.Contains(joined, "Synthetic workflow rules") {
				t.Fatalf("automated Codex worker lost memory or workflow instructions: %s", joined)
			}
		})

		t.Run("antigravity_"+string(audience), func(t *testing.T) {
			on := true
			ctx, err := PrepareAntigravityContextForAudience("", "", "Synthetic memory", "zh-Hant-TW", &on, audience)
			if err != nil {
				t.Fatal(err)
			}
			ctx.WorkflowInstructions = "Synthetic workflow rules"
			if ctx.ShouldExplainSteps() {
				t.Fatalf("automated Antigravity worker should not enable step explanations")
			}
			if reminder := ctx.TurnReminder(); reminder != "" {
				t.Fatalf("automated Antigravity worker should not get turn reminder: %q", reminder)
			}
			inst := ctx.Instructions()
			if strings.Contains(inst, "clawdline note create") || strings.Contains(inst, "answer Note") {
				t.Fatalf("automated Antigravity worker received Note instruction: %s", inst)
			}
			if strings.Contains(inst, "Clawdline Board item title") {
				t.Fatalf("automated Antigravity worker received Board language instruction: %s", inst)
			}
			if !strings.Contains(inst, "Synthetic memory") || !strings.Contains(inst, "Synthetic workflow rules") {
				t.Fatalf("automated Antigravity worker lost memory or workflow instructions: %s", inst)
			}
		})
	}

	t.Run("root keeps interactive rules", func(t *testing.T) {
		launch, err := Admit(LaunchRequest{
			Audience:    AudienceRoot,
			ProjectRoot: "/workspace/demo",
			Assistant:   AssistantCodex,
			Language:    "zh-Hant-TW",
			Memory:      "Synthetic memory",
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(launch.Arguments, " ")
		if !strings.Contains(joined, "clawdline guide note") || !strings.Contains(joined, "Clawdline Board item title") {
			t.Fatalf("Root Codex launch lost interactive rules: %s", joined)
		}

		on := true
		ctx, err := PrepareAntigravityContextForAudience("", "", "Synthetic memory", "zh-Hant-TW", &on, AudienceRoot)
		if err != nil {
			t.Fatal(err)
		}
		if !ctx.ShouldExplainSteps() || ctx.TurnReminder() == "" {
			t.Fatalf("Root Antigravity lost step explanations or reminder")
		}
		if inst := ctx.Instructions(); !strings.Contains(inst, "clawdline note create") || !strings.Contains(inst, BoardLanguageInstruction("zh-Hant-TW")) {
			t.Fatalf("Root Antigravity lost interactive rules: %s", inst)
		}
	})

	t.Run("unknown audience rejected", func(t *testing.T) {
		if _, err := Admit(LaunchRequest{Audience: "unknown", ProjectRoot: "/workspace/demo", Assistant: AssistantCodex}); err == nil {
			t.Fatal("expected unknown audience refusal")
		}
	})
}
