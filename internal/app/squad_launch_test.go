package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestSquadStartAndResumeUseOriginalImmutablePrompt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	place := projects.Place{Path: t.TempDir()}
	term := &personaTerminal{}
	conversation := "0f1e2d3c-0000-4000-8000-000000000099"
	roleBody := "Original role text"
	resolveCalls := 0
	starter := Starter{Squad: st, SquadDir: dir,
		ResolveSquadSnapshot: func(context.Context, string, string) (json.RawMessage, error) {
			resolveCalls++
			return json.Marshal(map[string]any{
				"definition_id": "clawdline.persona.architect", "scope_id": "project-test",
				"definition": map[string]any{"version": "1", "body": roleBody},
				"handbook":   map[string]any{"text": "One convention"},
			})
		},
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Launcher: term,
		Past: func(context.Context, projects.Place, string) []projects.Past {
			return []projects.Past{{ID: conversation}}
		},
	}
	if _, err := starter.Start(ctx, place, "codex", "", "", "architect"); err != nil {
		t.Fatal(err)
	}
	pathPattern := regexp.MustCompile(regexp.QuoteMeta(filepath.Join(dir, "squad", "launches")) + `/[A-Za-z0-9_-]{22}/personas/architect\.md`)
	firstPath := pathPattern.FindString(term.command)
	if firstPath == "" || !strings.Contains(term.command, "CLAWDLINE_SQUAD_CAPABILITY_FILE=") {
		t.Fatalf("snapshot launch command = %s", term.command)
	}
	if clear := strings.Index(term.command, "-u CLAWDLINE_SQUAD_CAPABILITY_FILE"); clear < 0 ||
		strings.Index(term.command, "CLAWDLINE_SQUAD_CAPABILITY_FILE=") < clear {
		t.Fatalf("role capability must be set after stale capability is cleared: %s", term.command)
	}
	firstPrompt, err := os.ReadFile(firstPath)
	if err != nil || !strings.Contains(string(firstPrompt), roleBody) || !strings.Contains(string(firstPrompt), "One convention") {
		t.Fatalf("first prompt = %q, %v", firstPrompt, err)
	}
	pending, err := st.PendingSquadLaunches(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if err := st.BindSquadConversation(ctx, pending[0].ID, conversation); err != nil {
		t.Fatal(err)
	}
	roleBody = "Changed role text"
	if _, err := starter.Resume(ctx, place, conversation, "codex", "architect"); err != nil {
		t.Fatal(err)
	}
	secondPath := pathPattern.FindString(term.command)
	if secondPath == "" || secondPath == firstPath || resolveCalls != 1 {
		t.Fatalf("resume used current settings: path=%q calls=%d", secondPath, resolveCalls)
	}
	secondPrompt, err := os.ReadFile(secondPath)
	if err != nil || !strings.Contains(string(secondPrompt), "Original role text") ||
		strings.Contains(string(secondPrompt), "Changed role text") ||
		filepath.Dir(firstPath) == filepath.Dir(secondPath) {
		t.Fatalf("resume prompt = %q, %v", secondPrompt, err)
	}
}
