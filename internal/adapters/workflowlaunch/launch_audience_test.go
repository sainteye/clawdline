package workflowlaunch

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/workflow"
	"github.com/sainteye/clawdline/skills"
)

func TestPrepareForAudienceSeparatesRootChildAndGateChecker(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir, root := t.TempDir(), t.TempDir()
	service := Service{Store: st, Dir: dir, Resolve: skills.ResolveWorkflow}

	if _, err := nextconfig.Open(dir).Set(map[string]any{
		"workflow_profile":                 "poteto",
		"codegraph_enabled":                true,
		"space_saving_images":              true,
		"browser_enabled":                  true,
		"tool_library_suggestions_enabled": true,
	}); err != nil {
		t.Fatal(err)
	}

	rootPrepared, err := service.PrepareForAudience(ctx, root, "", "", projects.AudienceRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Poteto workflow",
		"ordinary source search",
		workflow.SpaceSavingImageInstructions(),
		workflow.BrowserUseInstructions(),
		workflow.ToolLibrarySuggestionInstructions(),
	} {
		if !strings.Contains(rootPrepared.Text, want) {
			t.Fatalf("Root missing %q", want)
		}
	}
	if err := rootPrepared.Bind(ctx, "%root", "conv-root"); err != nil {
		t.Fatal(err)
	}

	childPrepared, err := service.PrepareForAudience(ctx, root, "", "conv-root", projects.AudienceChild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(childPrepared.Text, workflow.ToolLibrarySuggestionInstructions()) {
		t.Fatalf("Child should not receive tool-library suggestion instructions: %s", childPrepared.Text)
	}
	for _, kept := range []string{
		"Poteto workflow",
		"ordinary source search",
		workflow.SpaceSavingImageInstructions(),
		workflow.BrowserUseInstructions(),
	} {
		if !strings.Contains(childPrepared.Text, kept) {
			t.Fatalf("Child lost inherited/work instruction %q", kept)
		}
	}

	gatePrepared, err := service.PrepareForAudience(ctx, root, "", "conv-root", projects.AudienceGateChecker)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range []string{
		workflow.ToolLibrarySuggestionInstructions(),
		workflow.BrowserUseInstructions(),
		workflow.SpaceSavingImageInstructions(),
	} {
		if strings.Contains(gatePrepared.Text, omitted) {
			t.Fatalf("GateChecker should omit interactive/media instruction %q", omitted)
		}
	}
	if !strings.Contains(gatePrepared.Text, "Poteto workflow") || !strings.Contains(gatePrepared.Text, "ordinary source search") {
		t.Fatalf("GateChecker lost inherited workflow/codegraph: %s", gatePrepared.Text)
	}
}
