package orchestrator

import (
	"context"
	"errors"
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
