package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
)

func TestDaemonHistoryScanUsesOnlyTheIsolatedHome(t *testing.T) {
	for _, mode := range []struct {
		name, isolated string
		want           int
	}{
		{name: "isolated", isolated: "1", want: 0},
		{name: "custom state without isolation", want: 3},
	} {
		t.Run(mode.name, func(t *testing.T) {
			fixtureHome := t.TempDir()
			stateDir := t.TempDir()
			t.Setenv("HOME", fixtureHome)
			t.Setenv("USERPROFILE", fixtureHome)
			t.Setenv("CLAWDLINE_NEXT_DIR", stateDir)
			t.Setenv("CLAWDLINE_NEXT_PORT", "17727")
			t.Setenv("CLAWDLINE_ISOLATED", mode.isolated)
			for _, key := range []string{
				"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GEMINI_HOME", "GEMINI_CLI_HOME",
				"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
				"APPDATA", "LOCALAPPDATA", "CLAWDLINE_NEXT_INSTALL_ROOT", "CLAWDLINE_NEXT_BIN_DIR",
				"TMUX", "TMUX_TMPDIR",
			} {
				t.Setenv(key, fixtureHome)
			}
			now := time.Now()
			claude := filepath.Join(".claude", "projects", "sample", "sample-claude.jsonl")
			codex := filepath.Join(".codex", "sessions", now.Format("2006"), now.Format("01"), now.Format("02"), "rollout-sample.jsonl")
			gemini := filepath.Join(".gemini", "antigravity-cli", "brain", "00000000-0000-4000-8000-000000000001", ".system_generated", "logs", "transcript_full.jsonl")
			for path, row := range map[string]string{
				claude: `{"type":"user","message":{"content":"synthetic"}}`,
				codex:  `{"type":"session_meta","payload":{"id":"sample-codex"}}`,
				gemini: `{"step_index":0,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-01-01T00:00:00Z","content":"synthetic","input_tokens":1,"cache_read_tokens":0,"output_tokens":1}`,
			} {
				path = filepath.Join(fixtureHome, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(row+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if err := delegateNamedDaemon(); err != nil {
				t.Fatal(err)
			}
			historyHome, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(config.Dir())
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			ledger := app.NewUsageLedger(st, historyHome)
			ledger.Now = func() time.Time { return now }
			pass, err := ledger.Pass(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if pass.Due != mode.want || pass.Fed != mode.want {
				t.Fatalf("startup usage scan found %d and read %d transcripts, want %d for both", pass.Due, pass.Fed, mode.want)
			}
			for _, key := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GEMINI_HOME"} {
				want := fixtureHome
				if mode.isolated == "1" {
					want = ""
				}
				if got := os.Getenv(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			t.Logf("startup usage scan found %d and read %d transcripts", pass.Due, pass.Fed)
		})
	}
}
