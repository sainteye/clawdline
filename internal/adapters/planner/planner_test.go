package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftResolvesOnlyNumberedPlacesAndNormalizesAnswer(t *testing.T) {
	places := []Place{{ID: "one", Label: "One", Path: "/one"}, {ID: "two", Label: "Two", Path: "/two"}}
	raw := []byte(`{"project":2,"assistant":"CODEX","model":"opus","instructions":"  do it  ","title":"  Work  ","confidence":12,"question":"why?","kind":"schedule","at":"9:7","days":["Friday","mon","fri","noday"]}`)
	draft := DraftFrom(raw, places)
	if draft.PlaceID == nil || *draft.PlaceID != "two" {
		t.Fatalf("place = %#v", draft.PlaceID)
	}
	if draft.Assistant != "codex" || draft.Model != "" {
		t.Fatalf("assistant/model = %q/%q", draft.Assistant, draft.Model)
	}
	if draft.Instructions != "do it" || draft.Title != "Work" {
		t.Fatalf("text = %#v", draft)
	}
	if draft.Confidence != 1 || draft.Question != "" || draft.At != "09:07" {
		t.Fatalf("confidence/question/time = %#v", draft)
	}
	if got := strings.Join(draft.Days, ","); got != "mon,fri" {
		t.Fatalf("days = %q", got)
	}
}

func TestDraftLeavesUnsafeOrIncompleteChoicesForThePerson(t *testing.T) {
	// Codex is not schema-bound and sometimes writes the bare word instead of
	// the list Claude was asked for; both spell the same daily draft.
	draft := DraftFrom([]byte(`{"project":99,"assistant":"other","model":"huge","instructions":"x","confidence":0.9,"question":"When?","kind":"schedule","at":"25:00","days":"daily"}`),
		[]Place{{ID: "one"}})
	if draft.PlaceID != nil {
		t.Fatalf("unlisted place = %q", *draft.PlaceID)
	}
	if draft.Assistant != "claude" || draft.Model != "" || draft.At != "" {
		t.Fatalf("normalized draft = %#v", draft)
	}
	if draft.Confidence != Sure-0.01 || draft.Question != "When?" {
		t.Fatalf("incomplete schedule = %#v", draft)
	}
	if got := strings.Join(draft.Days, ","); got != "daily" {
		t.Fatalf("days = %q", got)
	}
}

func TestDraftMakesAnEditableBoardItemWithoutCreatingIt(t *testing.T) {
	draft := DraftFrom([]byte(`{"project":1,"assistant":"claude","model":"sonnet","instructions":"must be ignored by the Board","title":"Session voice Board item","description":"Let the Session-list microphone draft a Board item for review.","confidence":0.91,"question":"","kind":"work","work_kind":"feature","at":"11:30","days":["daily"]}`),
		[]Place{{ID: "clawdline", Label: "Clawdline", Path: "/code/clawdline"}})
	if draft.PlaceID == nil || *draft.PlaceID != "clawdline" {
		t.Fatalf("place = %#v", draft.PlaceID)
	}
	if draft.Kind != "work" || draft.WorkKind != "feature" || draft.Title != "Session voice Board item" {
		t.Fatalf("Board identity = %#v", draft)
	}
	if draft.Description != "Let the Session-list microphone draft a Board item for review." {
		t.Fatalf("description = %q", draft.Description)
	}
	if draft.Instructions != "" {
		t.Fatalf("Board draft kept session instructions = %q", draft.Instructions)
	}
	if draft.At != "" || len(draft.Days) != 0 {
		t.Fatalf("Board draft kept schedule fields = %#v", draft)
	}
}

func TestPromptNamesBoardConfirmationFields(t *testing.T) {
	prompt := Prompt([]Place{{ID: "one", Label: "One", Path: "/one"}}, []string{"claude"})
	for _, want := range []string{"kind: work", "work_kind:", "description:", "start and create nothing"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not name %q", want)
		}
	}
}

func TestPlannerUsesNoToolsAndFallsBackOnlyWhenNeeded(t *testing.T) {
	runs := 0
	p := Planner{
		Home: t.TempDir(),
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/fake/claude", nil
			}
			return "", errors.New("missing")
		},
		Run: func(_ context.Context, executable string, args []string, stdin, _ string, _ []string) ([]byte, error) {
			runs++
			joined := strings.Join(args, " ")
			if executable != "/fake/claude" || !hasEmptyTools(args) || !strings.Contains(joined, "--strict-mcp-config") || stdin != "open One" {
				t.Fatalf("run = %q %q stdin=%q", executable, joined, stdin)
			}
			return []byte(`{"structured_output":{"project":1,"assistant":"claude","model":"sonnet","instructions":"","title":"Open One","confidence":0.9,"question":"","kind":"session","at":"","days":[]}}`), nil
		},
	}
	draft, err := p.Draft(context.Background(), "open One", []Place{{ID: "one", Label: "One", Path: "/one"}}, []string{"claude"})
	if err != nil || draft.PlaceID == nil || *draft.PlaceID != "one" || draft.Instructions != "" || runs != 1 {
		t.Fatalf("draft=%#v runs=%d err=%v", draft, runs, err)
	}
}

func hasEmptyTools(args []string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--tools" && args[i+1] == "" {
			return true
		}
	}
	return false
}

func TestPlannerSaysWhenNeitherCLIExists(t *testing.T) {
	p := Planner{Home: t.TempDir(), LookPath: func(string) (string, error) { return "", errors.New("missing") }}
	_, err := p.Draft(context.Background(), "anything", nil, nil)
	if !errors.Is(err, ErrNoPlanner) {
		t.Fatalf("error = %v", err)
	}
}

func TestPlannerFallsBackToToollessCodex(t *testing.T) {
	home := t.TempDir()
	runs := 0
	p := Planner{
		Home: home,
		LookPath: func(name string) (string, error) {
			return "/fake/" + name, nil
		},
		Run: func(_ context.Context, executable string, args []string, stdin, dir string, env []string) ([]byte, error) {
			runs++
			if executable == "/fake/claude" {
				return nil, errors.New("quota unavailable")
			}
			joined := strings.Join(args, " ")
			if executable != "/fake/codex" || !strings.Contains(joined, "--sandbox read-only") ||
				!strings.Contains(joined, "agents.enabled=false") || !strings.Contains(stdin, "<sentence>\nopen One\n</sentence>") {
				t.Fatalf("codex run = %q %q stdin=%q", executable, joined, stdin)
			}
			if os.Getenv("CODEX_HOME") == "" && strings.Join(env, "\n") != "CODEX_HOME="+filepath.Join(home, ".codex") {
				t.Fatalf("env = %#v", env)
			}
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "-o" {
					answer := `{"project":1,"assistant":"codex","model":"","instructions":"","title":"Open One","confidence":0.9,"question":"","kind":"session","at":"","days":[]}`
					if err := os.WriteFile(args[i+1], []byte(answer), 0o600); err != nil {
						t.Fatal(err)
					}
					return nil, nil
				}
			}
			t.Fatalf("no output path in %q (dir %q)", joined, dir)
			return nil, nil
		},
	}
	draft, err := p.Draft(context.Background(), "open One", []Place{{ID: "one", Label: "One", Path: "/one"}}, []string{"claude", "codex"})
	if err != nil || runs != 2 || draft.PlaceID == nil || *draft.PlaceID != "one" || draft.Assistant != "codex" {
		t.Fatalf("draft=%#v runs=%d err=%v", draft, runs, err)
	}
}
