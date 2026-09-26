package orchestrator

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A schedule's Claude Code run is opened in a project folder recorded as
// trusted first, so its first screen is a composer rather than the
// workspace-trust dialog nothing may answer. A task a session dispatched, a
// run in a disposable worktree and a Codex run record nothing.
func TestAScheduledClaudeRunIsOpenedInATrustedFolder(t *testing.T) {
	project := t.TempDir()
	cases := []struct {
		name      string
		assistant string
		schedule  string
		worktree  *Worktree
		trusted   bool
	}{
		{"scheduled claude in the project folder", "claude", "5c000000-0000-4000-8000-000000000003", nil, true},
		{"dispatched by a session", "claude", "", nil, false},
		{"scheduled claude in a worktree", "claude", "5c000000-0000-4000-8000-000000000003",
			&Worktree{Path: project, Branch: "b"}, false},
		{"scheduled codex", "codex", "5c000000-0000-4000-8000-000000000003", nil, false},
	}
	for _, c := range cases {
		b, ctx := newTestBroker(t)
		b.Type = (&typedKeys{}).Type
		b.Launcher = &openingLauncher{pane: "%63"}
		which := session.AssistantClaude
		if c.assistant == "codex" {
			which = session.AssistantCodex
		}
		b.Live = func(context.Context) []session.Session {
			return []session.Session{{ID: "%63", Assistant: which}}
		}
		var trusted []string
		b.TrustClaudeProject = func(dir string) error { trusted = append(trusted, dir); return nil }
		r := Record{Protocol: Protocol, ID: "7ab00040-0000-4000-8000-000000000040", Assistant: c.assistant,
			Title: "daily", Claims: []string{}, ScheduleID: c.schedule, Worktree: c.worktree}
		got := b.spawn(ctx, r, project, "s", func() {})
		if got.State == StateSpawnFailed {
			t.Fatalf("%s: the tab did not open: %s", c.name, got.SpawnError)
		}
		if c.trusted && (len(trusted) != 1 || trusted[0] != project) {
			t.Errorf("%s: trusted %v, want [%s]", c.name, trusted, project)
		}
		if !c.trusted && len(trusted) != 0 {
			t.Errorf("%s: trusted %v, want nothing", c.name, trusted)
		}
	}
}
