package projectassistantacp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestInstalledClaudeRejectsStateInsideARepositoryBeforeStarting(t *testing.T) {
	repository := t.TempDir()
	state := filepath.Join(repository, "state")
	if e := os.Mkdir(state, 0700); e != nil {
		t.Fatal(e)
	}
	provider := InstalledClaudeProvider(state)
	_, e := provider.Open(context.Background(), app.OpenRequest{Bot: pa.Bot{Agent: pa.ClaudeCode, Project: pa.ProjectScope{Kind: pa.RepositoryProject}}, Directory: repository}, func(context.Context, app.ProtocolEvent) error { return nil })
	if pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("repository write exception was not refused: %v", e)
	}
	entries, e := os.ReadDir(state)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("refused open changed state")
	}
}

func TestInstalledClaudeWithoutClaudeCodeStartsNothing(t *testing.T) {
	state := t.TempDir()
	provider := InstalledClaudeProvider(state)
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		caps, e := provider.Capabilities(context.Background(), pa.Bot{Agent: agent})
		want := pa.CLINotInstalled
		if agent == pa.Codex {
			want = pa.AdapterNotFound
		}
		if pa.Code(e) != want || caps.ReadOnlyEnforced || caps.NativeMemoryVerified {
			t.Fatalf("invented capability for %s: %+v %v", agent, caps, e)
		}
	}
	entries, e := os.ReadDir(state)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("missing setup created runtime state")
	}
}
