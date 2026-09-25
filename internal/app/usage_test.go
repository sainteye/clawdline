package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Every transcript here is synthetic, written by these tests under a
// temporary home; none is copied from a real one.

type usageHarness struct {
	t     *testing.T
	home  string
	dir   string
	store *store.Store
	lines []string
	u     *UsageLedger
}

func newUsageHarness(t *testing.T) *usageHarness {
	t.Helper()
	h := &usageHarness{t: t, home: t.TempDir(), dir: t.TempDir()}
	h.open()
	t.Cleanup(func() { h.store.Close() })
	return h
}

// open starts a ledger — a new process, as far as the ledger can tell — over
// the same store directory.
func (h *usageHarness) open() {
	h.t.Helper()
	if h.store != nil {
		h.store.Close()
	}
	st, err := store.Open(h.dir)
	if err != nil {
		h.t.Fatal(err)
	}
	h.store = st
	h.u = NewUsageLedger(st, h.home)
	h.u.Log = func(format string, args ...any) { h.lines = append(h.lines, fmt.Sprintf(format, args...)) }
}

func (h *usageHarness) session(conversation string) string {
	return filepath.Join(h.home, ".claude", "projects", "-example-repo", conversation+".jsonl")
}

func (h *usageHarness) subagent(parent, agent string) string {
	return filepath.Join(h.home, ".claude", "projects", "-example-repo", parent, "subagents", agent+".jsonl")
}

func (h *usageHarness) write(path string, lines ...string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *usageHarness) pass() UsagePass {
	h.t.Helper()
	p, err := h.u.Pass(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func said(text string) string {
	b, _ := json.Marshal(text)
	return `{"type":"user","message":{"role":"user","content":` + string(b) + `}}`
}

// call is one API call: new context written, old context read, output.
func call(id string, write, read, output int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"role":"assistant","model":"claude-opus-5-5",`+
		`"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"cache_creation_input_tokens":%d,`+
		`"cache_read_input_tokens":%d,"output_tokens":%d}}}`, id, write, read, output)
}

// referenceSpent is reference with its categories.
func referenceSpent(t *testing.T, path string) (transcript.Tokens, map[transcript.Category]transcript.Tokens) {
	t.Helper()
	var s transcript.LedgerState
	for {
		res, err := s.Feed(path)
		if err != nil {
			t.Fatal(err)
		}
		if !res.More {
			break
		}
	}
	spent, _ := s.Totals()
	return reference(t, path), spent
}

// reference is the transcript read whole by a fresh state, the way nothing
// in the daemon reads it: what a pass's stored totals must equal.
func reference(t *testing.T, path string) transcript.Tokens {
	t.Helper()
	var s transcript.LedgerState
	for {
		res, err := s.Feed(path)
		if err != nil {
			t.Fatal(err)
		}
		if !res.More {
			break
		}
	}
	spent, measured := s.Totals()
	for _, c := range transcript.Categories {
		measured.Cost += spent[c].Cost
		measured.Unpriced += spent[c].Unpriced
	}
	return measured
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(1, math.Abs(b)) }

func sameTokens(a, b transcript.Tokens) bool {
	return near(a.Input, b.Input) && near(a.CacheWrite1h, b.CacheWrite1h) && near(a.CacheWrite5m, b.CacheWrite5m) &&
		near(a.CacheRead, b.CacheRead) && near(a.Output, b.Output) && near(a.Cost, b.Cost) && near(a.Unpriced, b.Unpriced)
}

func categorySum(c map[transcript.Category]transcript.Tokens) transcript.Tokens {
	var sum transcript.Tokens
	for _, k := range transcript.Categories {
		sum = addTokens(sum, c[k])
	}
	return sum
}

func (h *usageHarness) row(conversation string) store.UsageRow {
	h.t.Helper()
	r, ok, err := h.store.UsageRow(context.Background(), "claude", conversation)
	if err != nil || !ok {
		h.t.Fatalf("no row for %s: %v", conversation, err)
	}
	return r
}

func TestAPassReadsNewTranscriptsAndContinuesOldOnesFromTheirOffset(t *testing.T) {
	h := newUsageHarness(t)
	a := h.session("a")
	h.write(a, said("Rename the helper."), call("m1", 12000, 0, 90))
	if p := h.pass(); p.Fed != 1 || p.Due != 1 {
		t.Fatalf("first pass: %+v", p)
	}
	first := h.row("a")
	var state transcript.LedgerState
	if err := json.Unmarshal(first.State, &state); err != nil || state.Offset == 0 {
		t.Fatalf("stored state: %+v %v", state, err)
	}
	// Nothing changed: nothing is due.
	if p := h.pass(); p.Due != 0 {
		t.Fatalf("an unchanged transcript was due: %+v", p)
	}
	// A new transcript and an appended one.
	h.write(h.session("b"), said("Look at the build."), call("n1", 5000, 0, 10))
	h.write(a, said("Now shorten it."), call("m2", 300, 12001, 30))
	if p := h.pass(); p.Fed != 2 {
		t.Fatalf("second pass: %+v", p)
	}
	second := h.row("a")
	if err := json.Unmarshal(second.State, &state); err != nil || state.Offset <= mustOffset(t, first) || state.Calls != 2 {
		t.Fatalf("the second read did not go on from the first: %+v", state)
	}
	for _, c := range []string{"a", "b"} {
		got, err := h.u.ForSession(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if want := reference(t, h.session(c)); !sameTokens(got.Totals.Measured, want) {
			t.Errorf("%s: measured %+v, want %+v", c, got.Totals.Measured, want)
		}
		if !sameTokens(categorySum(got.Totals.Categories), got.Totals.Measured) {
			t.Errorf("%s: categories do not add up to the measured count", c)
		}
		if got.Reason != "" || got.ReadAt.IsZero() {
			t.Errorf("%s: %+v", c, got)
		}
	}
}

func mustOffset(t *testing.T, r store.UsageRow) int64 {
	t.Helper()
	var s transcript.LedgerState
	if err := json.Unmarshal(r.State, &s); err != nil {
		t.Fatal(err)
	}
	return s.Offset
}

func TestARemovedTranscriptKeepsItsTotalsAndSaysItIsMissing(t *testing.T) {
	h := newUsageHarness(t)
	a := h.session("a")
	h.write(a, said("Rename the helper."), call("m1", 12000, 0, 90))
	h.pass()
	want := reference(t, a)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if p := h.pass(); p.Missing != 1 {
		t.Fatalf("the pass did not find it gone: %+v", p)
	}
	got, err := h.u.ForSession(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason != store.UsageTranscriptMissing || !sameTokens(got.Totals.Measured, want) {
		t.Fatalf("a removed transcript: reason %q, measured %+v, want %+v", got.Reason, got.Totals.Measured, want)
	}
	if len(got.Gaps) != 1 || !got.Gaps[0].Counted {
		t.Fatalf("gaps: %+v", got.Gaps)
	}
	// Said, and not asked about again.
	if p := h.pass(); p.Due != 0 {
		t.Fatalf("a missing transcript was due again: %+v", p)
	}
	// A session never read is not yet read, not zero.
	never, err := h.u.ForSession(context.Background(), "nobody")
	if err != nil || never.Reason != store.UsageNotYetRead {
		t.Fatalf("an unknown session: %+v %v", never, err)
	}
}

func TestOneUnreadableTranscriptDoesNotStopTheOthersAndIsLoggedOnce(t *testing.T) {
	h := newUsageHarness(t)
	h.write(h.session("a"), said("Rename the helper."), call("m1", 12000, 0, 90))
	h.write(h.session("c"), said("Look at the build."), call("n1", 5000, 0, 10))
	// A directory where a transcript should be: it is found, it opens, and
	// it cannot be read — on every platform.
	if err := os.MkdirAll(h.session("b"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := h.pass()
	if p.Fed != 2 || p.Unreadable != 1 {
		t.Fatalf("pass: %+v", p)
	}
	for _, c := range []string{"a", "c"} {
		if r := h.row(c); r.Reason != "" || r.ReadAt.IsZero() {
			t.Errorf("%s was not read beside the unreadable one: %+v", c, r)
		}
	}
	if r := h.row("b"); r.Reason != store.UsageTranscriptUnreadable {
		t.Fatalf("the unreadable one: %+v", r)
	}
	h.pass()
	h.pass()
	unreadable := 0
	for _, l := range h.lines {
		if strings.Contains(l, store.UsageTranscriptUnreadable) {
			unreadable++
		}
		if strings.Contains(l, h.home) {
			t.Errorf("a log line names a path under the home: %s", l)
		}
	}
	if unreadable != 1 {
		t.Fatalf("the unreadable transcript was logged %d times: %q", unreadable, h.lines)
	}
}

func TestThePassLimitCarriesOver(t *testing.T) {
	h := newUsageHarness(t)
	h.u.PassLimit = 2
	// Two transcripts that stay due on every pass, first in the order: a
	// pass that started from the top each time would spend its whole limit
	// on them and never reach the rest.
	for _, c := range []string{"a0", "a1"} {
		if err := os.MkdirAll(h.session(c), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for i := 0; i < 3; i++ {
		c := fmt.Sprintf("s%d", i)
		names = append(names, c)
		h.write(h.session(c), said("Look."), call("m"+c, 1000, 0, 10))
	}
	p := h.pass()
	if p.Due != 5 || p.Fed+p.Unreadable != 2 || !p.Limited {
		t.Fatalf("first pass: %+v", p)
	}
	h.pass()
	h.pass()
	for _, c := range names {
		if r := h.row(c); r.ReadAt.IsZero() {
			t.Errorf("%s was never reached in three passes", c)
		}
	}
	limited := 0
	for _, l := range h.lines {
		if strings.Contains(l, "are due and a pass reads") {
			limited++
		}
	}
	if limited != 1 {
		t.Fatalf("the limit was said %d times: %q", limited, h.lines)
	}
}

func TestALookBackWindowLeavesOlderTranscriptsAlone(t *testing.T) {
	h := newUsageHarness(t)
	old := h.session("old")
	h.write(old, said("Look."), call("m1", 1000, 0, 10))
	h.write(h.session("new"), said("Look."), call("n1", 1000, 0, 10))
	long := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}
	if p := h.pass(); p.Due != 1 || p.Fed != 1 {
		t.Fatalf("pass: %+v", p)
	}
	if _, ok, _ := h.store.UsageRow(context.Background(), "claude", "old"); ok {
		t.Fatal("a transcript older than the window was read")
	}
}

func TestSubagentRowsFoldIntoTheParentsDelegate(t *testing.T) {
	h := newUsageHarness(t)
	parent := h.session("p")
	h.write(parent, said("Rename the helper."), call("m1", 12000, 0, 90))
	agent := h.subagent("p", "agent-x")
	h.write(agent, said("Search the tree."), call("s1", 4000, 0, 50), call("s2", 100, 4001, 20))
	h.pass()
	sub := h.row("agent-x")
	if sub.Parent != "p" {
		t.Fatalf("the subagent row is not linked to its parent: %+v", sub)
	}
	got, err := h.u.ForSession(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	own, agentWant := reference(t, parent), reference(t, agent)
	if !sameTokens(got.Totals.Measured, addTokens(own, agentWant)) {
		t.Fatalf("rollup %+v, want own %+v plus subagent %+v", got.Totals.Measured, own, agentWant)
	}
	var ownState transcript.LedgerState
	if err := json.Unmarshal(h.row("p").State, &ownState); err != nil {
		t.Fatal(err)
	}
	ownSpent, _ := ownState.Totals()
	delegate := got.Totals.Categories[transcript.CategoryDelegate]
	if !sameTokens(delegate, addTokens(ownSpent[transcript.CategoryDelegate], agentWant)) {
		t.Fatalf("delegate %+v does not hold the subagent's %+v", delegate, agentWant)
	}
	if len(got.Subagents) != 1 || got.Subagents[0].Conversation != "agent-x" || got.Subagents[0].Calls != 2 {
		t.Fatalf("subagents: %+v", got.Subagents)
	}
	if !sameTokens(categorySum(got.Totals.Categories), got.Totals.Measured) {
		t.Fatal("categories do not add up to the rollup")
	}
	// The subagent is its own row, not a session of its own.
	if got.Calls != 1 {
		t.Fatalf("the session's own calls: %d", got.Calls)
	}
}

func TestARestartedReaderContinuesWithoutDoubleCounting(t *testing.T) {
	h := newUsageHarness(t)
	a := h.session("a")
	// More calls than the reader remembers message ids for: a pass that read
	// them again would count them again.
	lines := []string{said("Rename the helper.")}
	for i := 0; i < 40; i++ {
		lines = append(lines, call(fmt.Sprintf("m%d", i), 300, 12000+300*i, 30), said("More."))
	}
	h.write(a, lines...)
	h.pass()
	h.open()
	h.write(a, said("And the test."), call("last", 200, 30000, 40))
	h.pass()
	// And once more with nothing new.
	h.open()
	h.pass()
	got, err := h.u.ForSession(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	want, wantSpent := referenceSpent(t, a)
	if !sameTokens(got.Totals.Measured, want) || got.Calls != 41 {
		t.Fatalf("after two restarts: %+v calls %d, want %+v", got.Totals.Measured, got.Calls, want)
	}
	for _, c := range transcript.Categories {
		if !sameTokens(got.Totals.Categories[c], wantSpent[c]) {
			t.Errorf("%s: %+v, want %+v", c, got.Totals.Categories[c], wantSpent[c])
		}
	}
}

func TestTheFirstMessageNamesTheTaskOrRootAssignment(t *testing.T) {
	cases := []struct{ text, task, ra string }{
		{"You are a Clawdline CHILD agent for task 7c1e. Say this line first", "7c1e", ""},
		{"You are an independently owned Clawdline Feature Root for Root Assignment ra-9.\nRead", "", "ra-9"},
		{"Please rename the helper.", "", ""},
	}
	for _, c := range cases {
		if task, ra := usageOpening(c.text); task != c.task || ra != c.ra {
			t.Errorf("%q: %q %q", c.text, task, ra)
		}
	}
}

// ---------- attribution over the Board and the broker ----------

func (h *usageHarness) task(id, root string, at time.Time) {
	h.t.Helper()
	row := store.BrokerRow{ID: id, Project: "/p", Assistant: "claude", State: "running", CreatedAt: at, UpdatedAt: at,
		SecretHash: "h", Record: json.RawMessage(`{"id":"` + id + `","root":{"session_id":"` + root + `"}}`)}
	if err := h.store.SaveBrokerTask(context.Background(), row, nil); err != nil {
		h.t.Fatal(err)
	}
}

func (h *usageHarness) child(conversation, taskID string) {
	h.write(h.session(conversation), said("You are a Clawdline CHILD agent for task "+taskID+". Say this line first."),
		call("m-"+conversation, 3000, 0, 20))
}

func names(sessions []SessionUsage) []string {
	var out []string
	for _, s := range sessions {
		out = append(out, s.Conversation)
	}
	sort.Strings(out)
	return out
}

func TestForTaskIncludesExactlyTheSessionsItsFirstMessageNames(t *testing.T) {
	h := newUsageHarness(t)
	h.child("c1", "task-1")
	h.child("c2", "task-2")
	h.write(h.session("plain"), said("Please look."), call("m", 1000, 0, 10))
	h.write(h.subagent("c1", "agent-1"), said("Search."), call("s", 1000, 0, 10))
	h.pass()
	got, err := h.u.ForTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names(got.Sessions), ",") != "c1" || len(got.Gaps) != 0 {
		t.Fatalf("task-1: %v %+v", names(got.Sessions), got.Gaps)
	}
	want := addTokens(reference(t, h.session("c1")), reference(t, h.subagent("c1", "agent-1")))
	if !sameTokens(got.Totals.Measured, want) {
		t.Fatalf("task-1 totals %+v, want %+v", got.Totals.Measured, want)
	}
	none, err := h.u.ForTask(context.Background(), "task-9")
	if err != nil || len(none.Sessions) != 0 || len(none.Gaps) != 1 || none.Gaps[0].Reason != store.UsageNotYetRead {
		t.Fatalf("an unread task: %+v %v", none, err)
	}
}

func TestForItemIncludesItsOwnersAndTheTasksTheyDispatchedWhileOwningIt(t *testing.T) {
	h := newUsageHarness(t)
	ctx := context.Background()
	t0 := time.Unix(1_790_000_000, 0)
	item := work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Fix it", Description: "Fix it", Phase: work.PhaseImplementing,
		DeploymentPolicy: work.DeployAgentDecides, OwnerSession: "owner-2", CreatedBy: "local",
		CreatedAt: t0, UpdatedAt: t0, Cycle: 1, Version: 1}
	if err := h.store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		// The first owner held it for an hour and was released.
		if err := tx.CreateAssignment(work.AssignmentV2{ID: "as-1", WorkID: item.ID, Mode: "existing_session",
			SessionID: "owner-1", State: "released", HumanActor: "local", CreatedAt: t0,
			UpdatedAt: t0.Add(time.Hour), ReleasedAt: t0.Add(time.Hour)}); err != nil {
			return err
		}
		// The second was opened by a Root Assignment; the assignment never
		// learnt its session, and its first message names the assignment.
		if err := tx.CreateAssignment(work.AssignmentV2{ID: "as-2", WorkID: item.ID, Mode: "new_session",
			State: "released", HumanActor: "local", RootAssignment: "ra-2", CreatedAt: t0.Add(2 * time.Hour),
			UpdatedAt: t0.Add(3 * time.Hour), ReleasedAt: t0.Add(3 * time.Hour)}); err != nil {
			return err
		}
		// The current owner, still holding it.
		return tx.CreateAssignment(work.AssignmentV2{ID: "as-3", WorkID: item.ID, Mode: "existing_session",
			SessionID: "owner-2", State: "active", HumanActor: "local", CreatedAt: t0.Add(4 * time.Hour),
			UpdatedAt: t0.Add(4 * time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}
	h.write(h.session("owner-1"), said("Take the item."), call("o1", 8000, 0, 40))
	h.write(h.session("opened"), said("You are an independently owned Clawdline Feature Root for Root Assignment ra-2.\nRead it."),
		call("o2", 8000, 0, 40))
	h.write(h.session("owner-2"), said("Take the item."), call("o3", 8000, 0, 40))
	h.write(h.session("bystander"), said("Something else."), call("o4", 8000, 0, 40))

	// Dispatched while owning it: counted.
	h.task("t-during-1", "owner-1", t0.Add(30*time.Minute))
	h.task("t-during-ra", "opened", t0.Add(150*time.Minute))
	h.task("t-during-2", "owner-2", t0.Add(5*time.Hour))
	// Dispatched by an owner outside its stint, or by somebody else: not.
	h.task("t-after-1", "owner-1", t0.Add(90*time.Minute))
	h.task("t-bystander", "bystander", t0.Add(30*time.Minute))
	// A task whose child has not been read yet: included, and said.
	h.task("t-unread", "owner-2", t0.Add(6*time.Hour))

	h.child("k1", "t-during-1")
	h.child("k2", "t-during-ra")
	h.child("k3", "t-during-2")
	h.child("k4", "t-after-1")
	h.child("k5", "t-bystander")
	h.write(h.subagent("k3", "agent-k3"), said("Search."), call("s", 1000, 0, 10))
	h.pass()

	got, err := h.u.ForItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.Join(names(got.Sessions), ","); s != "opened,owner-1,owner-2" {
		t.Fatalf("owner sessions: %s", s)
	}
	var tasks []string
	var children []string
	for _, task := range got.Tasks {
		tasks = append(tasks, task.TaskID)
		children = append(children, names(task.Sessions)...)
	}
	sort.Strings(tasks)
	sort.Strings(children)
	if s := strings.Join(tasks, ","); s != "t-during-1,t-during-2,t-during-ra,t-unread" {
		t.Fatalf("tasks: %s", s)
	}
	if s := strings.Join(children, ","); s != "k1,k2,k3" {
		t.Fatalf("child sessions: %s", s)
	}
	if len(got.Gaps) != 1 || got.Gaps[0].Kind != "task" || got.Gaps[0].ID != "t-unread" ||
		got.Gaps[0].Reason != store.UsageNotYetRead {
		t.Fatalf("gaps: %+v", got.Gaps)
	}
	var want transcript.Tokens
	for _, c := range []string{"opened", "owner-1", "owner-2", "k1", "k2", "k3"} {
		want = addTokens(want, reference(t, h.session(c)))
	}
	want = addTokens(want, reference(t, h.subagent("k3", "agent-k3")))
	if !sameTokens(got.Totals.Measured, want) {
		t.Fatalf("item totals %+v, want %+v", got.Totals.Measured, want)
	}
	if !sameTokens(categorySum(got.Totals.Categories), got.Totals.Measured) {
		t.Fatal("the item's categories do not add up")
	}
	if _, err := h.u.ForItem(ctx, "10000000-0000-4000-8000-00000000ffff"); err != ErrUsageNoItem {
		t.Fatalf("an unknown item: %v", err)
	}
}
