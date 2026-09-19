package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// W7 (broker-design #43): a machine that cannot open a child is refused at the
// door, by name, before anything exists.

// fakeTerminals is a capability set a test chooses, standing where this
// machine's terminals would answer.
func fakeTerminals(caps ...ports.Capability) func(context.Context) ports.Capabilities {
	return func(context.Context) ports.Capabilities { return caps }
}

func has(name ports.CapabilityName, via ...string) ports.Capability {
	return ports.Capability{Name: name, State: ports.CapabilityAvailable, Via: via, Reason: "through " + strings.Join(via, " and ")}
}

func lacks(name ports.CapabilityName, reason string) ports.Capability {
	return ports.Capability{Name: name, State: ports.CapabilityUnavailable, Reason: reason}
}

func notKnown(name ports.CapabilityName) ports.Capability {
	return ports.Capability{Name: name, State: ports.CapabilityUnknown, Reason: "could not be read"}
}

// dispatchOne writes a brief and dispatches it, as a root would.
func dispatchOne(t *testing.T, b *Broker, ctx context.Context, id string) (Dispatched, error) {
	t.Helper()
	writeBrief(t, b, id, b.Dir, nil)
	return b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
}

// ② A fake machine with no terminal to open a child in and no screen to read:
// 409 no_child_capability naming both, and nothing recorded, made, opened or
// counted against the rate window. The one it does have is not named.
func TestAMachineThatCannotOpenAChildIsRefusedBeforeAnythingExists(t *testing.T) {
	b, ctx := newTestBroker(t)
	launcher := &fakeLauncher{} // no tmux, no iTerm2
	b.Launcher = launcher
	b.TerminalCapabilities = fakeTerminals(
		lacks(ports.CapReadScreen, "this fake machine has no screen reader"),
		has(ports.CapSendKeys, "tmux"),
	)
	id := "c7000001-0000-4000-8000-000000000001"
	_, err := dispatchOne(t, b, ctx, id)
	if refusalCode(err) != "no_child_capability" {
		t.Fatalf("dispatch answered %v, want no_child_capability", err)
	}
	ref := err.(Refusal)
	if ref.Status != 409 {
		t.Fatalf("status %d", ref.Status)
	}
	for _, want := range []string{"open_child is unavailable", "read_screen is unavailable",
		"this fake machine has no screen reader", "Nothing was recorded"} {
		if !strings.Contains(ref.Message, want) {
			t.Errorf("the refusal does not say %q: %s", want, ref.Message)
		}
	}
	if strings.Contains(ref.Message, "send_keys") {
		t.Errorf("a capability the machine has was named missing: %s", ref.Message)
	}
	missing, _ := ref.Extra["missing"].([]string)
	if strings.Join(missing, ",") != "open_child,read_screen" {
		t.Errorf("missing = %v", missing)
	}
	rows, _ := ref.Extra["capabilities"].([]CapabilityRow)
	if len(rows) != 3 || rows[0].Name != "open_child" || rows[2].State != "available" {
		t.Errorf("the refusal carries %+v", rows)
	}

	// Nothing exists: no record, no briefing, no tab asked for, no dispatch
	// counted.
	if _, _, err := b.Record(ctx, id); !isNotFound(err) {
		t.Fatalf("a refused dispatch left a record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Tasks.Path(id), "CHILD.md")); !os.IsNotExist(err) {
		t.Fatalf("a refused dispatch wrote CHILD.md: %v", err)
	}
	if len(b.dispatches) != 0 {
		t.Fatalf("a refused dispatch was counted: %d", len(b.dispatches))
	}
}

// A capability the machine has only through a backend the child will not open
// in is missing for that child: typing through iTerm2 reaches no tmux pane.
func TestACapabilityInTheWrongBackendIsMissingForTheChild(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Launcher = &openingLauncher{pane: "%71"} // a tmux server with panes
	b.TerminalCapabilities = fakeTerminals(has(ports.CapReadScreen, "tmux", "iterm"), has(ports.CapSendKeys, "iterm"))
	_, err := dispatchOne(t, b, ctx, "c7000002-0000-4000-8000-000000000002")
	if refusalCode(err) != "no_child_capability" {
		t.Fatalf("dispatch answered %v", err)
	}
	msg := err.(Refusal).Message
	if !strings.Contains(msg, "send_keys is unavailable: a child would open in tmux") || strings.Contains(msg, "read_screen") {
		t.Fatalf("the refusal says: %s", msg)
	}
}

// Unknown is not no (DG-7): a broker with no launcher, or terminals that could
// not be read, refuses nothing, and the spawn decides as it always has.
func TestAnUnknownCapabilityRefusesNothing(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.TerminalCapabilities = fakeTerminals(notKnown(ports.CapReadScreen), notKnown(ports.CapSendKeys))
	out, err := dispatchOne(t, b, ctx, "c7000003-0000-4000-8000-000000000003")
	if err != nil {
		t.Fatalf("an unknown answer refused the dispatch: %v", err)
	}
	if out.Record.State != StateSpawnFailed {
		t.Fatalf("with no launcher the spawn still fails as before: %q", out.Record.State)
	}
	caps := b.ChildCapabilities(ctx)
	if c, _ := caps.Find(ports.CapOpenChild); c.State != ports.CapabilityUnknown {
		t.Fatalf("open_child with no launcher: %+v", c)
	}
}

// The control that shows the refusal above can be red: the same door with
// every capability present admits the dispatch, opens the tab and types the
// briefing into it.
func TestAMachineWithEveryCapabilityIsAdmitted(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &openingLauncher{pane: "%72"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%72", Assistant: session.AssistantClaude},
		}
	}
	b.TerminalCapabilities = fakeTerminals(has(ports.CapReadScreen, "tmux"), has(ports.CapSendKeys, "tmux"))
	out, err := dispatchOne(t, b, ctx, "c7000004-0000-4000-8000-000000000004")
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.ChildTerminalID != "%72" || keys.count("%72") != 1 {
		t.Fatalf("record %+v, typed %d", out.Record, keys.count("%72"))
	}
}

// The sentence a plan that opens nothing gives, per platform: the Mac keeps
// the Swift app's words, and nowhere else is told iTerm2 "is not running".
func TestTheNoChildSentenceSaysWhatThisPlatformIs(t *testing.T) {
	cases := []struct {
		goos   string
		plan   childPlan
		want   string
		unwant string
	}{
		{"darwin", childPlan{choice: projects.TerminalTmux, kind: projects.PlanNoTmux, asked: true},
			"there is no tmux on this Mac", ""},
		{"darwin", childPlan{choice: projects.TerminalITerm, kind: projects.PlanNotRunning, asked: true},
			"iTerm2 is not running, and this will not launch it for you.", ""},
		{"linux", childPlan{choice: projects.TerminalAuto, kind: projects.PlanNotRunning, asked: true},
			"linux has no iTerm2, and tmux is not installed.", "not running"},
		{"linux", childPlan{choice: projects.TerminalAuto, kind: projects.PlanNotRunning, asked: true, reach: projects.TmuxInstalled},
			"no tmux server is running", "iTerm2 is not running"},
		{"windows", childPlan{choice: projects.TerminalITerm, kind: projects.PlanNotRunning, asked: true},
			"windows has no iTerm2; set it to tmux", "not running"},
		{"windows", childPlan{choice: projects.TerminalTmux, kind: projects.PlanNoTmux, asked: true},
			"tmux is not installed", "this Mac"},
	}
	for _, c := range cases {
		got := c.plan.capability(c.goos)
		if got.State != ports.CapabilityUnavailable || !strings.Contains(got.Reason, c.want) ||
			(c.unwant != "" && strings.Contains(got.Reason, c.unwant)) {
			t.Errorf("%s %+v: %+v", c.goos, c.plan, got)
		}
		if err := got.Refusal(c.goos); err == nil || !strings.Contains(err.Error(), "open_child is unavailable on this machine ("+c.goos+")") {
			t.Errorf("%s: the named refusal is %v", c.goos, err)
		}
	}
}

// D16 end to end on the broker's side: a result put in place by
// `clawdline task finish` — no node — is the result the broker settles on,
// symbols and all; and the briefing names that command, not node.
func TestAResultFinishedWithoutNodeIsTheOneCollected(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Executable = "/opt/clawdline/bin/clawdline"
	b.Port = 7791
	id := "c7000005-0000-4000-8000-000000000005"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "w7", State: StateBriefed,
		Claims: []string{}, TimeoutMinutes: 240}
	r.CreatedAt = b.now()
	r.Dir = b.Tasks.Path(id)
	if err := b.save(ctx, r, HashSecret(w1Secret), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	brief := b.ChildBrief(r, b.Dir)
	want := "'/opt/clawdline/bin/clawdline' task finish --port 7791 '" + r.Dir + "'"
	if !strings.Contains(brief, want) || strings.Contains(brief, "node ") {
		t.Fatalf("the briefing's finishing line is not %q:\n%s", want, brief)
	}
	if _, err := b.Tasks.Write(r.Brief(), brief); err != nil {
		t.Fatal(err)
	}
	result := `{"clawdline_protocol": 1, "task_id": "` + id + `", "task_secret": "` + w1Secret + `",
 "status": "success", "summary": "done without node", "symbols": ["taskFinish"],
 "verification": {"runs": 1, "seconds": 3, "last": "pass", "scope": "go test"}}`
	if err := os.WriteFile(filepath.Join(r.Dir, "result.json.tmp"), []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := taskdir.Finish(r.Dir); err != nil {
		t.Fatal(err)
	}
	if err := b.Complete(ctx, id, w1Secret); err != nil {
		t.Fatal(err)
	}
	got, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateSuccess || got.Result == nil || strings.Join(got.Result.Symbols, ",") != "taskFinish" ||
		got.Result.Verify == nil || got.Result.Verify.Scope != "go test" {
		t.Fatalf("settled %q with %+v", got.State, got.Result)
	}
}
