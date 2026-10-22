package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAntigravityRootAndEffortStayExplicit(t *testing.T) {
	for _, name := range []string{"claude", "codex", "antigravity", "agy", ""} {
		body := json.RawMessage(`{"session_id":"synthetic-owner","assistant":"` + name + `"}`)
		root, err := admitRoot(&body)
		if name == "agy" || name == "" {
			if err == nil {
				t.Fatal("unnamed root fallback", name)
			}
		} else if err != nil || root.Assistant != name {
			t.Fatal(root, err)
		}
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		got, err := admitReasoningEffort(json.RawMessage(`"`+effort+`"`), "antigravity")
		if err != nil || got != effort {
			t.Fatal(got, err)
		}
	}
	if _, err := admitReasoningEffort(json.RawMessage(`"ultra"`), "antigravity"); err == nil || !strings.Contains(err.Error(), "xhigh, max") || strings.Contains(err.Error(), "ultra") {
		t.Fatal("foreign effort vocabulary", err)
	}
}

func TestAntigravityComposerDoesNotUseClaudeFallback(t *testing.T) {
	frame := strings.Repeat("─", 60)
	idle := "Antigravity CLI 1.4.5\n" + frame + "\n>\n" + frame + "\n? for shortcuts                 Gemini 3.8 Flash · high"
	if !ComposerReady(idle, session.AssistantAntigravity) || ReadComposer(idle, session.AssistantAntigravity) != ComposerEmpty {
		t.Fatal("native idle")
	}
	draft := strings.Replace(idle, "\n>\n", "\n> retained draft\n", 1)
	if ComposerReady(draft, session.AssistantAntigravity) || ReadComposer(draft, session.AssistantAntigravity) != ComposerDraft {
		t.Fatal("draft overwritten")
	}
	if ComposerReady(frame+"\n❯\n"+frame, session.AssistantAntigravity) || ComposerReady(frame+"\n❯\n"+frame, session.Assistant("foreign")) {
		t.Fatal("Claude fallback")
	}
}
