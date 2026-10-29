package http

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/skills"
)

func TestIntegrationLaunchReadsCurrentMachineOptInAcrossProjects(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	roots := []string{t.TempDir(), t.TempDir()}
	for _, on := range []bool{false, true, false} {
		if _, err := s.settingsFile().Set(map[string]any{"workflow_skills_enabled": on}); err != nil {
			t.Fatal(err)
		}
		for _, root := range roots {
			text, err := s.integrationLaunchText(context.Background(), root)
			if err != nil || on && text != skills.WorkflowInstructions() || !on && text != "" {
				t.Fatalf("enabled=%v: %q %v", on, text, err)
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatalf("launch mutated project: %v %v", files, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.integrationLaunchText(ctx, roots[0]); err == nil {
		t.Fatal("ignored canceled launch")
	}
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "config.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if text, err := s.integrationLaunchText(context.Background(), roots[0]); err == nil || strings.TrimSpace(text) != "" {
		t.Fatalf("unreadable opt-in became enabled: %q %v", text, err)
	}
}

func TestGraphifyLaunchGuidanceIsIndependentAndDoesNotReadAnIndexWhenOff(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	if text, err := s.integrationLaunchText(context.Background(), missing); err != nil || text != "" {
		t.Fatalf("disabled adapter inspected project: %q %v", text, err)
	}
	if _, err := s.settingsFile().Set(map[string]any{"graphify_enabled": true}); err != nil {
		t.Fatal(err)
	}
	text, err := s.integrationLaunchText(context.Background(), root)
	if err != nil || !strings.Contains(text, "ordinary source search") || strings.Contains(text, skills.WorkflowInstructions()) {
		t.Fatalf("graphify-only missing graph: %q %v", text, err)
	}
	if err := os.Mkdir(filepath.Join(root, "graphify-out"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.settingsFile().Set(map[string]any{"workflow_skills_enabled": true}); err != nil {
		t.Fatal(err)
	}
	text, err = s.integrationLaunchText(context.Background(), root)
	if err != nil || !strings.Contains(text, "clawdline graphify check .") || !strings.Contains(text, skills.WorkflowInstructions()) {
		t.Fatalf("combined opt-in lost guidance: %q %v", text, err)
	}
	if _, err := s.settingsFile().Set(map[string]any{"graphify_enabled": false}); err != nil {
		t.Fatal(err)
	}
	text, err = s.integrationLaunchText(context.Background(), root)
	if err != nil || text != skills.WorkflowInstructions() {
		t.Fatalf("graphify off retained its guidance: %q %v", text, err)
	}
}
