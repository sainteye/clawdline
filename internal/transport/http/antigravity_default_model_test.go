//go:build darwin || linux

package http

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestOwnedAntigravitySessionUsesConfiguredDefaultModelAndEffort(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := antigravitysession.New(context.Background(), antigravitysession.Options{Assistant: session.AssistantAntigravity, Executable: exe, AccountDir: dir, Args: []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	}()
	s.antigravitySessions.Store(h)

	starter := app.Starter{
		DefaultModel: func(assistant string) string {
			if assistant == projects.AssistantAntigravity {
				return "synthetic-pro"
			}
			return ""
		},
		DefaultReasoningEffort: func(assistant string) string {
			if assistant == projects.AssistantAntigravity {
				return "low"
			}
			return ""
		},
	}

	row, err := s.openOwnedAssistant(t.Context(), starter, projects.Place{Path: dir}, "", "", nil, projects.AssistantAntigravity)
	if err != nil {
		t.Fatal(err)
	}

	facts, err := h.Facts(row)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Model != "synthetic-pro" || facts.Effort != "low" {
		t.Fatalf("default model and effort not applied: model=%q effort=%q", facts.Model, facts.Effort)
	}

	starterCompound := app.Starter{
		DefaultModel: func(assistant string) string {
			if assistant == projects.AssistantAntigravity {
				return "synthetic-pro-low"
			}
			return ""
		},
	}
	row2, err := s.openOwnedAssistant(t.Context(), starterCompound, projects.Place{Path: dir}, "", "", nil, projects.AssistantAntigravity)
	if err != nil {
		t.Fatal(err)
	}
	facts2, err := h.Facts(row2)
	if err != nil {
		t.Fatal(err)
	}
	if facts2.Model != "synthetic-pro" || facts2.Effort != "low" {
		t.Fatalf("compound default model not applied: model=%q effort=%q", facts2.Model, facts2.Effort)
	}
}
