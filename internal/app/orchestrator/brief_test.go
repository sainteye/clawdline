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
// how it ended, how many leftovers, and the one command. Its machine half
// keeps what a machine reads and nothing the sentence or the command says
// again (version 3).
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
	for _, key := range []string{"protocol", "version", "kind", "task", "state", "leftovers", "body", "notice_id"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("the notice lost its %q", key)
		}
	}
	for _, key := range []string{"audience", "result_path", "ack_path", "child_may_still_write", "claims_released",
		"outstanding"} {
		if _, ok := fields[key]; ok {
			t.Errorf("the notice still carries %q", key)
		}
	}
	body, _ := fields["body"].(string)
	for _, want := range []string{"clawdline task show " + r.ID, "that closes this notice",
		"3 leftover", "nothing is committed", r.Worktree.Path} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not say %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"result.json", "/v1/orchestrator/proposals", "/completion/ack", "task ack"} {
		if strings.Contains(body, gone) {
			t.Errorf("the body still spells out %q:\n%s", gone, body)
		}
	}
	// The ceiling: the title and the checkout path are the task's, and the
	// rest is ours. It was 1,000 bytes of ours before. The id inside the
	// `clawdline task land` an empty branch is offered is the task's too: the
	// line used to name an action without a command, which was shorter only
	// because nobody could run it.
	if !strings.Contains(body, "clawdline task land "+r.ID+" abandoned") {
		t.Errorf("an empty branch is not offered the command that abandons it:\n%s", body)
	}
	if ours := len(body) - len(r.Title) - len(r.Worktree.Path) - len(r.Worktree.Branch) - len(r.ID); ours > 330 {
		t.Errorf("the body spends %d bytes of its own, want at most 330:\n%s", ours, body)
	}
}

// CHILD.md carries this task and points at the protocol every child shares;
// the protocol itself is `clawdline guide child`, read once instead of on
// every call. The result.json contract stays, because a child that never
// reads the guide must still be able to finish.
func TestTheBriefingPointsAtTheSharedProtocolInsteadOfCarryingIt(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir()), Executable: "/opt/clawdline", Port: 7791}
	r := briefTestRecord()
	r.Persona = "backend"
	brief := b.ChildBrief(r, "/p")
	for _, want := range []string{"`'/opt/clawdline' guide child`", "result.json.tmp", `"clawdline_protocol": 1`,
		"'/opt/clawdline' task finish --port 7791", "/progress", "/notify"} {
		if !strings.Contains(brief, want) {
			t.Errorf("CHILD.md does not carry %q", want)
		}
	}
	guide := ChildGuide()
	for _, shared := range []string{"is not decoration", "You are the bottom of this tree",
		"command screening", "the paragraph you were going to write anyway", "Do the cheapest verification pass"} {
		if strings.Contains(brief, shared) {
			t.Errorf("CHILD.md still carries the shared protocol's %q", shared)
		}
		if !strings.Contains(guide, shared) {
			t.Errorf("clawdline guide child does not carry %q", shared)
		}
	}
	// A persona is the launch's system prompt; the briefing does not repeat it.
	if strings.Contains(brief, "Clawdline persona") || strings.Contains(brief, "Backend Engineer") {
		t.Error("CHILD.md carries the persona the system prompt already has")
	}
	// The guide is the same for every task: nothing of one is in it.
	for _, particular := range []string{r.ID, "/opt/clawdline", "7791", "<TASK_SECRET>"} {
		if strings.Contains(guide, particular) {
			t.Errorf("clawdline guide child carries the task-particular %q", particular)
		}
	}
}

func TestEnvironmentBlockedBuildKeepsAResumableFailureContract(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir()), Executable: "/opt/clawdline", Port: 7791}
	brief := b.ChildBrief(briefTestRecord(), "/p")
	for _, want := range []string{"shared dependency directory is not writable", "status: failure",
		"verification.last: fail", "command to rerun", "changed source paths",
		"commit state", "keep changes here"} {
		if !strings.Contains(brief, want) {
			t.Errorf("blocked build contract is missing %q", want)
		}
	}
}
