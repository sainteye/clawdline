package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// briefTestRecord is a task as admission leaves it: instructions with a
// heading and a code fence of their own, declared writes, deliverables.
func briefTestRecord() Record {
	return Record{ID: "b7000000-0000-4000-8000-000000000001", Title: "shorter protocol", Kind: "custom",
		TimeoutMinutes: 240, ProjectDir: "/p", Assistant: "claude",
		Instructions: "Cut the protocol.\n\n## Deliver\n1. a thing\n\n```go\nfunc x() {}\n```\nmarker-7f3a",
		Claims:       []string{"internal/a.go", "docs/b.md"},
		Deliverables: []string{"docs/b.md"},
		Root:         &RootRef{SessionID: "conv", Assistant: "claude"}}
}

// A child reads one file. It was reading CHILD.md and then task.json, 2.4
// times a session on average (measured on 2026-09-26), for a title and
// instructions the broker already had in hand when it wrote CHILD.md.
func TestTheBriefingCarriesTheTaskItself(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir()), Executable: "/opt/clawdline"}
	r := briefTestRecord()
	brief := b.ChildBrief(r, "/p")

	for _, want := range append([]string{r.Instructions, r.Title, "custom", "240 minutes"},
		append(r.Claims, r.Deliverables...)...) {
		if !strings.Contains(brief, want) {
			t.Errorf("CHILD.md does not carry %q", want)
		}
	}
	// The instructions' own fence cannot close the one they sit in, and
	// their heading is not one of the briefing's.
	if !strings.Contains(brief, "````") {
		t.Error("instructions holding a ``` fence are wrapped in a fence they can close")
	}

	// task.json is named once, as the daemon's copy the child need not read.
	var named []string
	for _, line := range strings.Split(brief, "\n") {
		if strings.Contains(line, "task.json") {
			named = append(named, line)
		}
	}
	if len(named) != 1 || !strings.Contains(named[0], "need not read it") {
		t.Errorf("CHILD.md names task.json on %d line(s), want one saying it need not be read: %q", len(named), named)
	}
	if strings.Contains(brief, "read that file now") {
		t.Error("CHILD.md still tells the child to read task.json")
	}
}

// Signing is one command and one sentence. The curl recipe and the
// accepted.json paragraph are the command's to know.
func TestTheBriefingSignsWithOneCommand(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir()), Executable: "/opt/clawdline", Port: 7791}
	r := briefTestRecord()
	brief := b.ChildBrief(r, "/p")
	want := "CLAWDLINE_TASK_SECRET=<TASK_SECRET> '/opt/clawdline' task accept --port 7791 '" + b.Tasks.Path(r.ID) + "'"
	if !strings.Contains(brief, want) {
		t.Errorf("CHILD.md does not sign with %q", want)
	}
	for _, gone := range []string{"/accepted \\", "accepted.json"} {
		if strings.Contains(brief, gone) {
			t.Errorf("CHILD.md still carries %q", gone)
		}
	}
}

// The completion notice's human half is a pointer, not a manual: which task,
// how it ended, how many leftovers, and the two commands. Its machine half
// keeps every key it had.
func TestTheCompletionNoticeIsShort(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir())}
	r := Record{ID: "b7000000-0000-4000-8000-000000000002", Title: strings.Repeat("t", 60), State: StateSuccess,
		Notice: &Notice{ID: "n-0123456789abcdef"},
		Claims: []string{"a.go"}, LeaseScope: LeaseWorktree,
		Worktree: &Worktree{Branch: "clawdline/task/b7000000-0000-4000-8000-000000000002",
			Path: "/Users/someone/.config/clawdline-next/worktrees/clawdline-3b9e26c1/b7000000-0000-4000-8000-000000000002"},
		Landing: &Landing{State: LandingPending, Settlement: SettlementEmpty},
		Result: &taskdir.Result{Status: "success", Leftovers: []work.Leftover{
			{Title: "one"}, {Title: "two"}, {Title: "three"}}}}

	wire, err := b.NoticeWire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(wire, "<clawdline-notice>"), "</clawdline-notice>")
	var fields map[string]any
	if err := json.Unmarshal([]byte(inner), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"protocol", "version", "kind", "audience", "task", "state", "result_path",
		"outstanding", "leftovers", "claims_released", "child_may_still_write", "body", "notice_id", "ack_path"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("the notice lost its %q", key)
		}
	}
	body, _ := fields["body"].(string)
	for _, want := range []string{"clawdline task show " + r.ID, "clawdline task ack " + r.ID + " " + r.Notice.ID,
		"3 leftover", "nothing is committed", r.Worktree.Path} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not say %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"result.json", "/v1/orchestrator/proposals", "/completion/ack"} {
		if strings.Contains(body, gone) {
			t.Errorf("the body still spells out %q:\n%s", gone, body)
		}
	}
	// The ceiling: the title and the checkout path are the task's, and the
	// rest is ours. It was 1,000 bytes of ours before.
	if ours := len(body) - len(r.Title) - len(r.Worktree.Path) - len(r.Worktree.Branch); ours > 330 {
		t.Errorf("the body spends %d bytes of its own, want at most 330:\n%s", ours, body)
	}
}
