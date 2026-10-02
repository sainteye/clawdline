package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Every persona is written where a launch names it, private to the person,
// and a restart that finds the same bytes leaves the file alone.
func TestPersonaFilesAreWrittenWhereALaunchNamesThem(t *testing.T) {
	dir := t.TempDir()
	if err := WritePersonaFiles(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(persona.Dir(dir))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("the directory: %v %v", st.Mode(), err)
	}
	for _, p := range persona.All() {
		path := persona.Path(dir, p.ID)
		body, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(body, []byte(p.Text())) {
			t.Fatalf("%s: %v", p.ID, err)
		}
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v", p.ID, st.Mode())
		}
	}
	if body, err := os.ReadFile(filepath.Join(persona.Dir(dir), persona.LicenseFileName)); err != nil ||
		!strings.Contains(string(body), "MIT License") {
		t.Errorf("the upstream licence is not beside the texts: %v", err)
	}

	// Unchanged: not rewritten.
	path := persona.Path(dir, "architect")
	old := time.Unix(1_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// Changed (an older build's text, or a hand edit): replaced.
	edited := persona.Path(dir, "security")
	if err := os.WriteFile(edited, []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePersonaFiles(dir); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); !st.ModTime().Equal(old) {
		t.Error("an identical file was rewritten")
	}
	p, _ := persona.Known("security")
	if body, _ := os.ReadFile(edited); string(body) != p.Text() {
		t.Error("a changed file was not replaced")
	}
	if st, _ := os.Stat(edited); st.Mode().Perm() != 0o600 {
		t.Errorf("a replaced file is %v", st.Mode())
	}
}

// personaTerminal is a tmux server that is running and records the one
// command it was asked to open.
type personaTerminal struct{ command string }

func (f *personaTerminal) ITermRunning(context.Context) (bool, error) { return false, nil }
func (f *personaTerminal) TmuxReach(context.Context) int              { return int(projects.TmuxRunning) }
func (f *personaTerminal) NewITermTab(context.Context, string) (string, error) {
	return "", errors.New("no iTerm here")
}
func (f *personaTerminal) NewTmuxWindow(_ context.Context, _, command string) (string, error) {
	f.command = command
	return "%70", nil
}
func (f *personaTerminal) PrepareTmuxViewer(context.Context, string) (string, error) {
	return "", errors.New("no viewer")
}
func (f *personaTerminal) NewTmuxSession(context.Context, string, string, string) (string, error) {
	return "", errors.New("the server is running")
}
func (f *personaTerminal) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return true, nil
}

// A start and a resume both carry the persona to the command line; a name
// the catalog does not have is the typed refusal a page can show.
func TestAStartAndAResumeCarryAPersona(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	next := t.TempDir()
	term := &personaTerminal{}
	conversation := "0f1e2d3c-0000-4000-8000-000000000002"
	s := Starter{
		Terminal:   func() projects.TerminalChoice { return projects.TerminalTmux },
		Launcher:   term,
		PersonaDir: persona.Dir(next),
		Past: func(context.Context, projects.Place, string) []projects.Past {
			return []projects.Past{{ID: conversation}}
		},
	}
	ctx := context.Background()
	file := persona.Path(next, "code-reviewer")

	if _, err := s.Start(ctx, place, "claude", "", "", "code-reviewer"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.command, "--append-system-prompt-file '"+file+"'") {
		t.Fatalf("start: %s", term.command)
	}

	if _, err := s.Resume(ctx, place, conversation, "claude", "code-reviewer"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.command, "--resume "+conversation) ||
		!strings.Contains(term.command, "--append-system-prompt-file '"+file+"'") {
		t.Fatalf("resume: %s", term.command)
	}

	term.command = ""
	for _, open := range []func() error{
		func() error { _, err := s.Start(ctx, place, "claude", "", "", "wizard"); return err },
		func() error { _, err := s.Resume(ctx, place, conversation, "codex", "wizard"); return err },
	} {
		var refusal StartRefusal
		if err := open(); !errors.As(err, &refusal) || refusal.Status != 400 || refusal.Code != "unknown_persona" {
			t.Errorf("an unknown persona: %v", err)
		}
	}
	if term.command != "" {
		t.Fatalf("a refused persona still opened: %s", term.command)
	}

	// None is the command line it always was.
	if _, err := s.Start(ctx, place, "claude", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(term.command, "append-system-prompt") {
		t.Fatalf("no persona: %s", term.command)
	}
}

// The persona is read from the process's command line, so the terminal row
// that wins the identity merge must not drop it, whichever side it is on.
func TestAMergedRowKeepsThePersonaTheProcessRowRead(t *testing.T) {
	terminal := session.Session{ID: "%3", Backend: session.BackendTmux, Label: "tab"}
	process := session.Session{ID: "ttys003", PID: 42, Assistant: session.AssistantClaude, Persona: "security"}
	for name, got := range map[string]session.Session{
		"process first":  richer(process, terminal),
		"process second": richer(terminal, process),
	} {
		if got.Persona != "security" || got.ID != "%3" {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := richer(terminal, session.Session{ID: "ttys003"}); got.Persona != "" {
		t.Errorf("a persona from nowhere: %+v", got)
	}
}

// A session that was launched as a persona is restored as it.
func TestARestoredSessionComesBackAsItsPersona(t *testing.T) {
	as := claudeRow("%1", "p-1")
	as.Persona = "architect"
	st, after, _ := previousBoot(t, as, claudeRow("%2", "p-2"))
	rows := recorded(t, st, "boot-before")
	if rows["p-1"].Persona != "architect" || rows["p-2"].Persona != "" {
		t.Fatalf("recorded: %+v / %+v", rows["p-1"], rows["p-2"])
	}
	handed := map[string]string{}
	resume := func(_ context.Context, row store.RestoreRow) (Started, error) {
		handed[row.ConversationID] = row.Persona
		return Started{ID: "%9", Backend: "tmux"}, nil
	}
	if _, err := after.Restore(context.Background(), []string{"p-1", "p-2"}, complete(), resume); err != nil {
		t.Fatal(err)
	}
	if handed["p-1"] != "architect" || handed["p-2"] != "" || len(handed) != 2 {
		t.Fatalf("resume was handed %v", handed)
	}
}
