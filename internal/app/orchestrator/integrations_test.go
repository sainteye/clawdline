package orchestrator

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/skills"
	"regexp"
	"strings"
	"testing"
)

func TestBrokerUsesMachineIntegrationsForChildrenAndOpenedSessions(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		b, ctx := newTestBroker(t)
		calls := 0
		b.IntegrationInstructions = func(context.Context, string) (string, error) {
			calls++
			return "MACHINE-INTEGRATION", nil
		}
		_, line := dispatchWith(t, b, "c2800021-0000-4000-8000-000000000021", assistant, nil)
		if calls != 1 || !strings.Contains(line, "MACHINE-INTEGRATION") {
			t.Fatalf("child %s missed machine integration: calls=%d %s", assistant, calls, line)
		}
		launcher := &recordingLauncher{pane: "%93"}
		b.Launcher = launcher
		if _, err := b.openSession(ctx, t.TempDir(), "clawdline-integration-test", assistant, "", "", nil); err != nil || calls != 2 || !strings.Contains(launcher.line(), "MACHINE-INTEGRATION") {
			t.Fatalf("opened %s missed opt-in: calls=%d err=%v %s", assistant, calls, err, launcher.line())
		}
		b.IntegrationInstructions = func(context.Context, string) (string, error) {
			return "", errors.New("unreadable opt-in")
		}
		if _, err := b.openSession(ctx, t.TempDir(), "clawdline-integration-refused", assistant, "", "", nil); err == nil {
			t.Fatal("opened despite unreadable opt-in")
		}
	}
}

func TestChildInheritsTheParentsWorkflowInsteadOfTheMachineChoice(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		b, ctx := newTestBroker(t)
		dir := t.TempDir()
		snap, err := skills.ResolveWorkflow("superpowers", false)
		if err != nil {
			t.Fatal(err)
		}
		parent, err := b.Store.PrepareWorkflowLaunch(ctx, snap)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Store.RecordWorkflowTerminal(ctx, parent.ID, "%1"); err != nil {
			t.Fatal(err)
		}
		if err := b.Store.BindWorkflowConversation(ctx, parent.ID, "%1", rootConversation); err != nil {
			t.Fatal(err)
		}
		if _, err := nextconfig.Open(dir).Set(map[string]any{"workflow_profile": "poteto", "graphify_enabled": true}); err != nil {
			t.Fatal(err)
		}
		service := workflowlaunch.Service{Store: b.Store, Dir: dir, Resolve: skills.ResolveWorkflow}
		b.PrepareWorkflow = service.Prepare
		b.IntegrationInstructions = func(context.Context, string) (string, error) {
			t.Fatal("child consulted mutable legacy settings")
			return "", nil
		}
		record, line := dispatchWith(t, b, "c2800022-0000-4000-8000-000000000022", assistant, nil)
		if record.State == StateSpawnFailed || !strings.Contains(line, "Evidence-led debugging") || strings.Contains(line, "Poteto workflow") || strings.Contains(line, "ordinary source search") {
			t.Fatalf("wrong child workflow: %+v %s", record, line)
		}
		matches := regexp.MustCompile(`clawdline-workflow:([A-Za-z0-9_-]{22})`).FindStringSubmatch(line)
		if len(matches) != 2 {
			t.Fatalf("no durable launch identity: %s", line)
		}
		launch, found, err := b.Store.WorkflowLaunchByID(ctx, matches[1])
		if err != nil || !found || launch.SnapshotID != snap.Digest || launch.TerminalID != record.ChildTerminalID {
			t.Fatalf("child snapshot/terminal receipt mismatch: %+v %v", launch, err)
		}
	}
}
