package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
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

func TestClaudeDispatchAddsItsOwnTaskDirectoryAsFileAccess(t *testing.T) {
	for _, c := range []struct {
		name     string
		redirect bool
		external bool
	}{
		{name: "broker task directory"},
		{name: "redirected task directory", redirect: true},
		{name: "external task directory", external: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			launcher := &recordingLauncher{pane: "%63"}
			b.Launcher = launcher
			b.Type = (&typedKeys{}).Type
			b.Live = func(context.Context) []session.Session {
				return []session.Session{{ID: "%63", Assistant: session.AssistantClaude}}
			}
			b.ClaudeSetsLanguage = func() bool { return false }
			if _, err := nextconfig.Open(b.Dir).Set(map[string]any{"language": "zh-TW"}); err != nil {
				t.Fatal(err)
			}
			b.TrustClaudeProject = func(dir string) error {
				t.Errorf("an unscheduled launch recorded persistent trust for %q", dir)
				return nil
			}
			switch {
			case c.redirect:
				if err := os.Symlink(t.TempDir(), b.Tasks.Dir); err != nil {
					t.Skipf("directory symlink unavailable: %v", err)
				}
			case c.external:
				b.Tasks.Dir = t.TempDir()
			default:
				if err := os.Mkdir(b.Tasks.Dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			project := t.TempDir()
			r := Record{Protocol: Protocol, ID: "7ab00040-0000-4000-8000-000000000040", Assistant: "claude",
				Title: "Bot work", PermissionMode: "auto", Worktree: &Worktree{Path: project}}
			if got := b.spawn(ctx, r, project, "s", func() {}); got.State == StateSpawnFailed {
				t.Fatalf("the tab did not open: %s", got.SpawnError)
			}
			line := launcher.line()
			if !strings.Contains(line, "--permission-mode auto") || strings.Count(line, "--settings ") != 1 {
				t.Fatalf("launch lost its mode or combined settings: %s", line)
			}
			if c.redirect || c.external {
				if strings.Contains(line, "additionalDirectories") || !strings.Contains(line, "--add-dir "+projects.ShellQuoted(b.Tasks.Dir)) {
					t.Fatalf("the directory must go through Claude Code's own trust dialog: %s", line)
				}
				return
			}
			real, err := filepath.EvalSymlinks(b.Tasks.Dir)
			if err != nil {
				t.Fatal(err)
			}
			settings, err := json.Marshal(map[string]any{
				"language":    "Traditional Chinese (繁體中文)",
				"permissions": map[string]any{"additionalDirectories": []string{real}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(line, "--add-dir ") || !strings.Contains(line, "--settings "+projects.ShellQuoted(string(settings))) {
				t.Fatalf("expected file access and language in one Claude Code setting: %s", line)
			}
		})
	}
}
