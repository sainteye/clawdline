package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A brief may name a built-in persona, and one it names is on the record; a
// name the catalog does not have is refused by name, with the catalog listed.
// No kind has a default: a plan_review names none unless its brief does.
func TestABriefMayNameAPersona(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8100000-0000-4000-8000-000000000001"
	writeBrief(t, b, id, project, map[string]any{"assistant": "claude", "persona": "code-reviewer"})
	r, err := b.ReadDraft(id)
	if err != nil || r.Persona != "code-reviewer" {
		t.Fatalf("admitted %q (%v)", r.Persona, err)
	}
	for _, brief := range []map[string]any{
		{"assistant": "claude", "kind": "plan_review"},
		{"assistant": "codex", "persona": nil},
	} {
		writeBrief(t, b, id, project, brief)
		if r, err := b.ReadDraft(id); err != nil || r.Persona != "" {
			t.Fatalf("%v became %q (%v)", brief, r.Persona, err)
		}
	}
	for value, want := range map[any]string{
		"wizard": "persona must be one of: architect, backend, frontend, minimal-change, code-reviewer, " +
			"reality-checker, security, technical-writer",
		"Architect": "persona must be one of:",
		7:           "persona must be a string",
	} {
		writeBrief(t, b, id, project, map[string]any{"assistant": "claude", "persona": value})
		_, err := b.ReadDraft(id)
		if refusalCode(err) != "bad_task" || !strings.HasPrefix(refusalMessage(err), want) {
			t.Errorf("%v refused as %q / %q", value, refusalCode(err), refusalMessage(err))
		}
	}
	// It is kept with the task: the record is stored as JSON.
	body, _ := json.Marshal(Record{ID: id, Persona: "security"})
	var back Record
	if err := json.Unmarshal(body, &back); err != nil || back.Persona != "security" {
		t.Fatalf("%s → %+v (%v)", body, back, err)
	}
}

// The child's tab is typed the persona's flag, naming the file the daemon
// wrote under its own state directory.
func TestAChildIsLaunchedAsItsPersona(t *testing.T) {
	for _, c := range []struct{ assistant, want string }{
		{"claude", "--append-system-prompt-file '%s'"},
		{"codex", `-c 'developer_instructions="clawdline-persona:security - `},
	} {
		b, ctx := newTestBroker(t)
		b.Type = (&typedKeys{}).Type
		launcher := &recordingLauncher{pane: "%64"}
		b.Launcher = launcher
		which := session.AssistantClaude
		if c.assistant == "codex" {
			which = session.AssistantCodex
		}
		b.Live = func(context.Context) []session.Session { return []session.Session{{ID: "%64", Assistant: which}} }
		r := Record{Protocol: Protocol, ID: "7ab00050-0000-4000-8000-000000000050", Assistant: c.assistant,
			Title: "t", Claims: []string{}, Persona: "security"}
		got := b.spawn(ctx, r, t.TempDir(), "s", func() {})
		if got.State == StateSpawnFailed {
			t.Fatalf("%s: the tab did not open: %s", c.assistant, got.SpawnError)
		}
		want := c.want
		if strings.Contains(want, "%s") {
			want = strings.Replace(want, "%s", filepath.Join(b.Dir, "personas", "security.md"), 1)
		}
		if !strings.Contains(launcher.line(), want) {
			t.Errorf("%s: the line does not carry the persona:\n%s", c.assistant, launcher.line())
		}
	}
}

// A Root Assignment may name one: the Feature Root is launched as it, its
// ASSIGNMENT.md says so in a PERSONA section, and the persona is part of the
// request its receipt compares.
func TestARootAssignmentMayNameAPersona(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	launcher := &recordingLauncher{pane: "%65"}
	b.Launcher = launcher
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%65", Assistant: session.AssistantClaude}}
	}
	req := RootAssignmentRequest{RequestID: "c6500002-0000-4000-8000-000000000002", Assistant: "claude",
		ProjectDir: t.TempDir(), Label: "Feature X", Persona: "architect", Assignment: Assignment{Objective: "o",
			Scope: "s", Constraints: "c", RelevantReferences: "r", Acceptance: "a"}}
	a, _, err := b.OpenRootAssignment(ctx, req.RequestID, req)
	if err != nil || a.State != AssignmentBriefed || a.Persona != "architect" {
		t.Fatalf("%+v %v", a, err)
	}
	file := filepath.Join(b.Dir, "personas", "architect.md")
	if !strings.Contains(launcher.line(), "--append-system-prompt-file '"+file+"'") {
		t.Fatalf("the Feature Root was not launched as the architect:\n%s", launcher.line())
	}
	brief, _ := os.ReadFile(a.BriefPath)
	if !strings.Contains(string(brief), "PERSONA\nArchitect (architect). Its definition is "+file) {
		t.Fatalf("ASSIGNMENT.md has no PERSONA section:\n%s", brief)
	}
	req.Persona = "security"
	if _, _, err := b.OpenRootAssignment(ctx, req.RequestID, req); refusalCode(err) != "request_conflict" {
		t.Fatalf("the same request id with another persona: %v", err)
	}
	req.RequestID, req.Persona = "c6500003-0000-4000-8000-000000000003", "wizard"
	if _, _, err := b.OpenRootAssignment(ctx, req.RequestID, req); refusalCode(err) != "bad_root_assignment" ||
		!strings.Contains(refusalMessage(err), "persona must be one of") {
		t.Fatalf("an unknown persona: %v", err)
	}
	// None is no section, and a request without one digests as it did
	// before the field existed.
	if strings.Contains(AssignmentBrief("x", req.Assignment, "", ""), "PERSONA") {
		t.Fatal("a brief without a persona has a PERSONA section")
	}
	req.Persona = ""
	if body, _ := json.Marshal(req); strings.Contains(string(body), "persona") {
		t.Fatalf("an empty persona is in the digest: %s", body)
	}
}

// A handoff's receiver carries no persona in this version.
func TestAHandoffOpensWithoutAPersona(t *testing.T) {
	b, ctx := newTestBroker(t)
	launcher := &recordingLauncher{pane: "%66"}
	b.Launcher = launcher
	if _, err := b.openSession(ctx, t.TempDir(), "clawdline-handoff-test", "claude", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(launcher.line(), "--append-system-prompt-file") {
		t.Fatalf("a session opened without a persona carries one:\n%s", launcher.line())
	}
}
