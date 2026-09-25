package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// refusalMessage is what a refusal said, for the sentences a caller fixes a
// brief by.
func refusalMessage(err error) string {
	var ref Refusal
	if errors.As(err, &ref) {
		return ref.Message
	}
	return ""
}

// `reasoning_effort` is carried to Codex's command line rather than refused by
// name. It used to be on the refused list, and the alternative that refusal
// named — "or through the Swift app" — is an app that has been retired, so the
// way out it offered was dead. Meanwhile a schedule could already be stored
// with the field and then fail `bad_task` on every run.
func TestACodexBriefMayNameItsReasoningEffort(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8000000-0000-4000-8000-000000000001"
	for _, want := range []string{"high", "xhigh"} {
		writeBrief(t, b, id, project, map[string]any{"assistant": "codex", "reasoning_effort": want})
		r, err := b.ReadDraft(id)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if r.ReasoningEffort != want {
			t.Fatalf("%s: record carries %q", want, r.ReasoningEffort)
		}
	}
	// Absent stays absent: a brief that named nothing is the model's own
	// default, not a value this broker chose for it.
	writeBrief(t, b, id, project, map[string]any{"assistant": "codex"})
	if r, err := b.ReadDraft(id); err != nil || r.ReasoningEffort != "" {
		t.Fatalf("an unnamed effort became %q (%v)", r.ReasoningEffort, err)
	}
	writeBrief(t, b, id, project, map[string]any{"assistant": "codex", "reasoning_effort": nil})
	if r, err := b.ReadDraft(id); err != nil || r.ReasoningEffort != "" {
		t.Fatalf("a null effort became %q (%v)", r.ReasoningEffort, err)
	}
}

// The two refusals are the Swift app's own sentences, and the closed list is
// closed: anything but those two names, and anything that is not a name at
// all, is `bad_task`.
func TestAReasoningEffortIsCodexOnlyAndOneOfTwoNames(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8000000-0000-4000-8000-000000000002"
	for _, c := range []struct {
		brief map[string]any
		want  string
	}{
		{map[string]any{"assistant": "claude", "reasoning_effort": "high"},
			"reasoning_effort is only valid when assistant is codex"},
		{map[string]any{"assistant": "codex", "reasoning_effort": "medium"},
			"reasoning_effort must be one of: high, xhigh"},
		{map[string]any{"assistant": "codex", "reasoning_effort": 1},
			"reasoning_effort must be one of: high, xhigh"},
		{map[string]any{"assistant": "codex", "reasoning_effort": "HIGH"},
			"reasoning_effort must be one of: high, xhigh"},
	} {
		writeBrief(t, b, id, project, c.brief)
		_, err := b.ReadDraft(id)
		if refusalCode(err) != "bad_task" || refusalMessage(err) != c.want {
			t.Errorf("%v refused as %q / %q, want %q", c.brief, refusalCode(err), refusalMessage(err), c.want)
		}
	}
}

// The two that really are unsupported keep saying so, and their sentence no
// longer sends anybody to an app that is gone.
func TestTheFieldsThisBrokerStillRefusesNameNoRetiredApp(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8000000-0000-4000-8000-000000000003"
	for _, field := range []string{"serialize", "attach_session"} {
		writeBrief(t, b, id, project, map[string]any{field: []string{"x"}})
		_, err := b.ReadDraft(id)
		said := refusalMessage(err)
		if refusalCode(err) != "bad_task" || !strings.HasPrefix(said, field+" is not supported") {
			t.Errorf("%s refused as %q / %q", field, refusalCode(err), said)
		}
		if strings.Contains(said, "Swift app") {
			t.Errorf("%s still points at the retired app: %q", field, said)
		}
	}
}

// The whole line a child's tab is typed, with the effort in it: the brief, the
// launch and the dispatch's own flags composed exactly as `spawn` composes
// them. Pinned here because this string is the only place the three meet, and
// it is what a person's machine executes.
func TestTheLineACodexChildIsTypedCarriesTheEffort(t *testing.T) {
	r := Record{Assistant: "codex", PermissionMode: "full", Model: "gpt-5.6-sol", ReasoningEffort: "xhigh"}
	launch, err := projects.Admit(projects.LaunchRequest{
		ProjectRoot: "/work/app", Assistant: r.Assistant, Model: r.Model, ReasoningEffort: r.ReasoningEffort,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := shellCommand(launch, r, "/tmp/tasks", "/work/app")
	want := "env -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u CODEX_SANDBOX -u CODEX_SANDBOX_NETWORK_DISABLED " +
		"codex --model gpt-5.6-sol --config model_reasoning_effort=xhigh --add-dir '/tmp/tasks' " +
		"--ask-for-approval never --sandbox workspace-write " +
		`-c 'projects={"/work/app"={trust_level="trusted"}}'`
	if got != want {
		t.Fatalf("line\n got %q\nwant %q", got, want)
	}
}

// A task.json that was read but did not decode names the field and the type
// it wanted. It used to say "No readable task.json", the missing-file
// sentence, and a root with a string where an array belongs went looking for
// a path problem.
func TestABriefWithAWrongTypeNamesTheField(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8000000-0000-4000-8000-000000000004"
	for _, c := range []struct {
		brief map[string]any
		want  string
	}{
		{map[string]any{"deliverables": "docs/a.md"}, `task.json field "deliverables" must be an array of strings, not a JSON string`},
		{map[string]any{"timeout_minutes": "30"}, `task.json field "timeout_minutes" must be an integer, not a JSON string`},
		{map[string]any{"title": 7}, `task.json field "title" must be a string, not a JSON number`},
	} {
		writeBrief(t, b, id, project, c.brief)
		_, err := b.ReadDraft(id)
		said := refusalMessage(err)
		if refusalCode(err) != "bad_task" || !strings.HasPrefix(said, c.want) {
			t.Errorf("%v refused as %q / %q, want prefix %q", c.brief, refusalCode(err), said, c.want)
		}
		if strings.Contains(said, "No readable") {
			t.Errorf("%v still reads as a missing file: %q", c.brief, said)
		}
	}
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "task.json"), []byte(`{"title": `), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadDraft(id); !strings.HasPrefix(refusalMessage(err), "task.json is not valid JSON at byte") {
		t.Errorf("a truncated task.json refused as %q", refusalMessage(err))
	}
	if err := os.Remove(filepath.Join(b.Tasks.Path(id), "task.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadDraft(id); !strings.HasPrefix(refusalMessage(err), "No readable task.json") {
		t.Errorf("a missing task.json refused as %q", refusalMessage(err))
	}
}
