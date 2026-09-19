package cloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/app/cloudops"
	"github.com/sainteye/clawdline-go/internal/contract"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// A local router that answers by path: the session list and the task list are
// two routes, and the publisher reads both every pass.
type pathRouter struct {
	mu     sync.Mutex
	bodies map[string]string
	status map[string]int
	asked  []cloudops.LocalRequest
}

func newPathRouter(sessions, tasks string) *pathRouter {
	return &pathRouter{
		bodies: map[string]string{"/v1/sessions": sessions, "/v1/orchestrator/tasks": tasks},
		status: map[string]int{},
	}
}

func (r *pathRouter) Do(_ context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, req)
	body, ok := r.bodies[req.Path]
	status := r.status[req.Path]
	if !ok {
		return cloudops.LocalResponse{Status: http.StatusNotFound, Body: []byte(`{"error":"not_found"}`)}, nil
	}
	if status == 0 {
		status = http.StatusOK
	}
	return cloudops.LocalResponse{Status: status, Body: []byte(body), ContentType: "application/json"}, nil
}

func (r *pathRouter) set(path, body string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies[path] = body
	r.status[path] = status
}

// sessionsBody is a complete scan listing these terminals.
func sessionsBody(complete bool, ids ...string) string {
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "assistant": "claude", "tty": "ttys001", "title": "a session"})
	}
	body, _ := json.Marshal(map[string]any{"at": 17, "scan": map[string]any{"complete": complete}, "sessions": rows})
	return string(body)
}

func tasksBody(rows ...map[string]any) string {
	body, _ := json.Marshal(map[string]any{"at": 17, "tasks": rows, "store": "absent",
		"page": map[string]any{"cursor": 0, "limit": 50, "fields": "list"}})
	return string(body)
}

// Fixture ids: one hex digit is most of each, so none of them can be a real
// machine's.
func taskID(i int) string  { return fmt.Sprintf("c6%06d-0000-4000-8000-%012d", i, i) }
func childID(i int) string { return fmt.Sprintf("7E%06d-0000-4000-8000-%012d", i, i) }

const rootTerminal = "7E999999-0000-4000-8000-000000000999"

func keysOf(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// **A machine that has nothing to group publishes what it always did.** The
// descriptor is `at`, `app` and `machine`, whether the task list is empty, the
// route could not be read, or every task it holds is finished with its child
// tab closed.
func TestNoTaskAViewerCanReachLeavesTheDescriptorAsItWas(t *testing.T) {
	finishedAndClosed := map[string]any{"id": taskID(1), "state": "success", "title": "done",
		"created": 100, "finishedAt": 200, "child": map[string]any{"terminalId": childID(1)}}
	for _, c := range []struct {
		name   string
		tasks  string
		status int
	}{
		{name: "no tasks at all", tasks: tasksBody()},
		{name: "the task route refused", tasks: `{"error":{"code":"store_unreadable"}}`, status: http.StatusInternalServerError},
		{name: "the task route is not there", tasks: "", status: http.StatusNotFound},
		{name: "only finished tasks whose tabs are closed", tasks: tasksBody(finishedAndClosed)},
	} {
		t.Run(c.name, func(t *testing.T) {
			router := newPathRouter(sessionsBody(true, "%19"), c.tasks)
			if c.status != 0 {
				router.set("/v1/orchestrator/tasks", c.tasks, c.status)
			}
			out := &collector{}
			publisher := newPublisher(router, out)
			publisher.firstPass(context.Background())

			body := out.payload(t, "orch/mac-01")
			if got := strings.Join(keysOf(body), ","); got != "app,at,machine" {
				t.Errorf("the descriptor's keys are %s; a machine with nothing to group must publish app,at,machine", got)
			}
			// And the rest of the pass is the one it was: the descriptor, one
			// row and the inventory, in that order.
			if names := out.channels(); len(names) != 3 || names[0] != "orch/mac-01" {
				t.Errorf("the pass published %v", names)
			}
		})
	}
}

// **A task with a parent is in the projection, and nothing a viewer does not
// read goes with it.** The record carries everything the Swift app's list
// route once let through — the four fields its deny-list dropped and the five
// lifecycle records its measurement found riding along — and the snapshot
// carries only the nine paths the hosted console reads.
func TestAChildTaskIsProjectedAndItsLifecycleRecordsAreNot(t *testing.T) {
	child := map[string]any{
		"id": taskID(1), "task_id": taskID(1), "state": "briefed", "title": "把 task 清單送過去",
		"created": 1000, "created_at": 1000, "kind": "custom", "assistant": "claude",
		"project_dir": "/Users/someone/code/project", "dir": "/Users/someone/tasks/" + taskID(1),
		"claims": []string{"internal/transport/cloud"}, "claims_declared": true, "permission": "full",
		"isolation": "worktree", "spawnedAt": 1001, "briefedAt": 1002, "depth": 1,
		"child": map[string]any{"terminalId": childID(1), "backend": "iterm", "sessionId": "c6000001-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		"root": map[string]any{"terminalId": rootTerminal, "sessionId": "c6000002-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			"assistant": "claude", "label": "project root", "taskId": taskID(9)},
		"usage": map[string]any{"total": 123456, "costUsd": 1.25, "input": 1, "output": 2,
			"cacheRead": 3, "cacheWrite": 4, "model": "claude-test"},
		// The Swift deny-list.
		"summary": "what the child wrote about itself", "review": map[string]any{"verdict": "ship"},
		"graph": map[string]any{"nodes": []any{}}, "progress": []any{"a note"},
		// What rode along after it (OrchestratorPersistence.swift, 2026-09-16).
		"completion_delivery": map[string]any{"state": "delivered"}, "landing": map[string]any{"state": "pending"},
		"executor":     map[string]any{"observed_at": 5, "inventory_generation": 6},
		"verification": map[string]any{"runs": 1}, "worktree": map[string]any{"path": "/Users/someone/wt"},
	}
	finishedOpen := map[string]any{"id": taskID(2), "state": "success", "title": "finished, tab still open",
		"created": 900, "finishedAt": 950, "child": map[string]any{"terminalId": childID(2)},
		"root": map[string]any{"sessionId": "c6000003-cccc-4ccc-8ccc-cccccccccccc", "label": "root not on screen"}}
	finishedClosed := map[string]any{"id": taskID(3), "state": "failure", "title": "finished, tab closed",
		"created": 800, "finishedAt": 850, "child": map[string]any{"terminalId": childID(3)}}
	unknownState := map[string]any{"id": taskID(4), "state": "a state from a newer build", "title": "kept",
		"created": 700}
	neverOpened := map[string]any{"id": taskID(5), "state": "spawn_failed", "title": "no tab", "created": 600}

	router := newPathRouter(sessionsBody(true, rootTerminal, childID(1), childID(2)),
		tasksBody(child, finishedOpen, finishedClosed, unknownState, neverOpened))
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	body := out.payload(t, "orch/mac-01")
	tasks, _ := body["tasks"].([]any)
	var ids []string
	for _, row := range tasks {
		ids = append(ids, row.(map[string]any)["id"].(string))
	}
	if want := []string{taskID(1), taskID(2), taskID(4)}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("the projection holds %v; want the running child, the finished one whose tab is open and the unknown state, in the route's order", ids)
	}

	projected := tasks[0].(map[string]any)
	if got := strings.Join(keysOf(projected), ","); got != "child,created,id,root,state,title,usage" {
		t.Errorf("a projected record has %s", got)
	}
	for _, gone := range []string{"summary", "review", "graph", "progress", "completion_delivery", "landing",
		"executor", "verification", "worktree", "claims", "dir", "project_dir", "task_id", "created_at"} {
		if _, ok := projected[gone]; ok {
			t.Errorf("%s reached the Cloud snapshot", gone)
		}
	}
	if got := projected["child"].(map[string]any); len(got) != 1 || got["terminalId"] != childID(1) {
		t.Errorf("child is %v; want only its terminal", got)
	}
	if got := projected["root"].(map[string]any); len(got) != 1 || got["terminalId"] != rootTerminal {
		// The parent is the whole point: `rowDepth`, `tasksOfRoot` and
		// `grouped` read root.terminalId and nothing else of it.
		t.Errorf("root is %v; want only the parent's terminal", got)
	}
	if got := projected["usage"].(map[string]any); len(got) != 2 || got["total"] != float64(123456) || got["costUsd"] != 1.25 {
		t.Errorf("usage is %v; want total and costUsd", got)
	}
	// A root this daemon could not resolve to a tab is no `root` at all, not
	// an empty object.
	if _, ok := tasks[1].(map[string]any)["root"]; ok {
		t.Errorf("a root with no terminal was published: %v", tasks[1])
	}
	if tasks[1].(map[string]any)["finishedAt"] != float64(950) {
		t.Errorf("a finished task lost its finishedAt: %v", tasks[1])
	}
	// The read is the local console's page, not the whole store.
	for _, asked := range router.asked {
		if asked.Path == "/v1/orchestrator/tasks" && asked.Query["limit"] != "50" {
			t.Errorf("the task list was read with %v", asked.Query)
		}
	}
}

// Whether a finished task is reachable follows what a viewer holds: a partial
// scan publishes rows and no inventory, so a row it missed is still held; a
// complete one tombstones it.
func TestAFinishedTaskFollowsTheRowsAViewerHolds(t *testing.T) {
	finished := map[string]any{"id": taskID(1), "state": "success", "title": "t", "created": 1,
		"finishedAt": 2, "child": map[string]any{"terminalId": childID(1)}}
	router := newPathRouter(sessionsBody(true, childID(1), childID(2)), tasksBody(finished))
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())
	if _, ok := out.payload(t, "orch/mac-01")["tasks"]; !ok {
		t.Fatal("a finished task whose tab is listed was left out")
	}

	out.reset()
	router.set("/v1/sessions", sessionsBody(false, childID(2)), 0)
	publisher.Pass(context.Background())
	for _, name := range out.channels() {
		if name == "orch/mac-01" {
			t.Fatalf("a partial scan changed the task list, but the viewer still holds the row: %v", out.channels())
		}
	}

	out.reset()
	router.set("/v1/sessions", sessionsBody(true, childID(2)), 0)
	publisher.Pass(context.Background())
	if _, ok := out.payload(t, "orch/mac-01")["tasks"]; ok {
		t.Error("a complete scan tombstoned the child's row and its finished task was still sent")
	}
}

// The snapshot goes out when what a viewer reads in it changed, and not when
// only `at` did; a task list that could not be read is not an empty one.
func TestTheTaskListIsSentOnChangeAndKeptThroughAFailedRead(t *testing.T) {
	live := map[string]any{"id": taskID(1), "state": "briefed", "title": "t", "created": 1,
		"child": map[string]any{"terminalId": childID(1)}, "executor": map[string]any{"observed_at": 1}}
	router := newPathRouter(sessionsBody(true, childID(1)), tasksBody(live))
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	// Only what the projection does not carry moved.
	out.reset()
	live["executor"] = map[string]any{"observed_at": 99}
	router.set("/v1/orchestrator/tasks", tasksBody(live), 0)
	publisher.Pass(context.Background())
	if len(out.channels()) != 0 {
		t.Errorf("an executor reading republished: %v", out.channels())
	}

	// The route fails: nothing is sent, and what is held is the last list.
	router.set("/v1/orchestrator/tasks", `{}`, http.StatusInternalServerError)
	publisher.Pass(context.Background())
	if len(out.channels()) != 0 {
		t.Errorf("a failed task read republished: %v", out.channels())
	}
	if len(publisher.tasks) != 1 {
		t.Errorf("a failed task read dropped the list a viewer holds: %v", publisher.tasks)
	}

	// The task finishes: that is a change.
	live["state"], live["finishedAt"] = "success", 5
	router.set("/v1/orchestrator/tasks", tasksBody(live), 0)
	publisher.Pass(context.Background())
	tasks, _ := out.payload(t, "orch/mac-01")["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["state"] != "success" {
		t.Errorf("the finished state was not published: %v", tasks)
	}
}

// Past the row bound the oldest finished records go first, and every
// unfinished one stays; the log says so once, not every pass.
func TestTheTaskListIsCutOldestFinishedFirst(t *testing.T) {
	var rows []map[string]any
	listed := []string{}
	const live, finished = 30, 100
	for i := 0; i < live; i++ {
		rows = append(rows, map[string]any{"id": taskID(i), "state": "briefed", "title": "live", "created": 10_000 - i})
	}
	for i := live; i < live+finished; i++ {
		rows = append(rows, map[string]any{"id": taskID(i), "state": "success", "title": "done", "created": 10_000 - i,
			"finishedAt": 20_000 - i, "child": map[string]any{"terminalId": childID(i)}})
		listed = append(listed, childID(i))
	}
	router := newPathRouter(sessionsBody(true, listed...), tasksBody(rows...))
	out := &collector{}
	var lines []string
	publisher := newPublisher(router, out)
	publisher.Log = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	publisher.firstPass(context.Background())

	tasks, _ := out.payload(t, "orch/mac-01")["tasks"].([]any)
	if len(tasks) != TaskListLimit {
		t.Fatalf("%d records went out; the bound is %d", len(tasks), TaskListLimit)
	}
	for i := 0; i < live; i++ {
		if tasks[i].(map[string]any)["state"] != "briefed" {
			t.Fatalf("record %d is %v; every unfinished task must be kept ahead of the finished", i, tasks[i])
		}
	}
	if last := tasks[len(tasks)-1].(map[string]any)["id"]; last != taskID(TaskListLimit-1) {
		t.Errorf("the last record kept is %v; the newest finished ones must be the ones kept", last)
	}
	publisher.Pass(context.Background())
	var said []string
	for _, line := range lines {
		if strings.Contains(line, "leaves out") {
			said = append(said, line)
		}
	}
	if len(said) != 1 || !strings.Contains(said[0], "leaves out the 30 oldest") {
		t.Errorf("the cut was logged as %q; want one line naming the 30 left out", said)
	}
}

// Past the byte bound the tail is cut too, whatever the row count.
func TestTheTaskListIsCutToItsByteBound(t *testing.T) {
	var rows []map[string]any
	title := strings.Repeat("長", 2_000)
	for i := 0; i < TaskListLimit; i++ {
		rows = append(rows, map[string]any{"id": taskID(i), "state": "briefed", "title": title, "created": 10_000 - i})
	}
	projected, omitted := projectTasks(rows, nil)
	encoded := mustJSON(projected)
	// The records and their commas, without the brackets around them.
	if size := len(encoded) - 2; size > TaskListBytesLimit || size < TaskListBytesLimit-len(mustJSON(projected[0])) {
		t.Errorf("the records take %d bytes; the bound is %d and the cut must stop within one record of it", size, TaskListBytesLimit)
	}
	if omitted != TaskListLimit-len(projected) || omitted == 0 {
		t.Errorf("%d kept, %d said to be left out", len(projected), omitted)
	}
	if projected[0]["id"] != taskID(0) {
		t.Error("the byte bound cut from the front")
	}
}

// measuredPass is one pass through the real relay's sealing: what the
// publisher handed over (plaintext) and the envelopes the spool holds for the
// wire (sealed), per channel kind.
type measuredPass struct {
	orchPlain, orchSealed   int
	tasksBytes              int
	passPlain, passSealed   int
	envelopes, projectedRow int
	routeBody               int
}

func measurePass(t *testing.T, sessions, tasks string) measuredPass {
	t.Helper()
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// The transport is never run: Publish reserves, seals and hands the row to
	// the spool, and the spool's drain worker is what would write it.
	relay := &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "mac-01", Signer: signer, Secret: secret}
	var m measuredPass
	first := spool.NextSequence()
	publisher := newPublisher(newPathRouter(sessions, tasks), &collector{})
	publisher.Publish = func(ctx context.Context, out Outbound) error {
		m.passPlain += len(out.Payload)
		if strings.HasPrefix(out.Channel, "orch/") {
			m.orchPlain = len(out.Payload)
		}
		return relay.Publish(ctx, out)
	}
	publisher.firstPass(context.Background())
	for seq := first; seq < spool.NextSequence(); seq++ {
		row, ok := spool.Row(seq)
		if !ok {
			continue
		}
		m.envelopes++
		m.passSealed += len(row.Sealed)
		if row.Channel == adaptercloud.SpoolChannelOrch {
			m.orchSealed = len(row.Sealed)
		}
	}
	if len(publisher.tasks) > 0 {
		m.tasksBytes = len(mustJSON(publisher.tasks))
		m.projectedRow = m.tasksBytes / len(publisher.tasks)
	}
	m.routeBody = len(tasks)
	return m
}

// realisticTasks is what this daemon's own route answers for n tasks, built
// from the generated row type so every field the route writes is there: half
// running, half finished with their tabs open, one root above all of them.
func realisticTasks(n int, title func(int) string) (string, []string) {
	rows := make([]contract.TaskRow, 0, n)
	listed := []string{rootTerminal}
	for i := 0; i < n; i++ {
		row := contract.TaskRow{
			ID: taskID(i), TaskID: taskID(i), Assistant: "claude", ProjectDir: "/Users/someone/code/project",
			Claims: []string{"internal/transport/cloud", "internal/app/cloudops", "docs/cloud.md"}, ClaimsDeclared: true,
			Created: 1_789_800_000 + int64(i), CreatedAt: 1_789_800_000 + int64(i), Depth: 1,
			Dir: "/Users/someone/.config/clawdline-next/tasks/" + taskID(i), Kind: "custom", Permission: "full",
			Isolation: "worktree", Title: title(i), SpawnedAt: 1_789_800_001 + int64(i), BriefedAt: 1_789_800_020 + int64(i),
			Child: &contract.TaskChild{TerminalID: childID(i), Backend: "iterm"},
			Root: &contract.TaskRoot{TerminalID: rootTerminal, SessionID: "c6999999-0000-4000-8000-000000000999",
				Assistant: "claude", Label: "project root"},
		}
		if i%2 == 0 {
			row.State = contract.TaskStateBriefed
		} else {
			row.State = contract.TaskStateSuccess
			row.FinishedAt = 1_789_803_600 + int64(i)
			row.Usage = &contract.TaskUsage{Total: 4_567_890, Input: 1_234, Output: 56_789, CacheRead: 4_000_000,
				CacheWrite: 505_867, CostUsd: 12.3456, Model: "claude-opus-5"}
		}
		listed = append(listed, childID(i))
		rows = append(rows, row)
	}
	body, _ := json.Marshal(contract.TaskList{At: 1_789_803_700, Tasks: rows, Store: contract.StoreReadingAbsent,
		Page: contract.TaskPage{Limit: 50, Fields: "list"}})
	return string(body), listed
}

// The numbers in tasklist.go and in the task report: one pass through the
// real relay's sealing, for a machine with 0, 10 and 100 tasks, and for 100
// tasks with every title at the broker's 200-character limit. `go test -run
// TestMeasure -v` prints them. What is asserted is the claim the bounds rest
// on: at the row limit with the longest titles this daemon admits, the list
// is under the byte bound, so the row bound is the one that binds.
func TestMeasureTheOrchSnapshotThroughTheRelay(t *testing.T) {
	typical := func(i int) string {
		return fmt.Sprintf("Cloud 上看不到主／子 session：把 task 清單送過去 #%d", i)
	}
	longest := func(int) string { return strings.Repeat("長", 200) }
	for _, c := range []struct {
		name  string
		n     int
		title func(int) string
	}{
		{"N=0", 0, typical}, {"N=10", 10, typical}, {"N=100", 100, typical}, {"N=100, 200-character titles", 100, longest},
	} {
		tasks, listed := realisticTasks(c.n, c.title)
		// The pass without the task list, over the same sessions, is the
		// baseline the task list adds to.
		without := measurePass(t, sessionsBody(true, listed...), tasksBody())
		with := measurePass(t, sessionsBody(true, listed...), tasks)
		t.Logf("%-30s route body %7d B | tasks %6d B (%3d B/task) | orch plaintext %6d B sealed %6d B (without tasks %d/%d) | whole pass %d envelopes, plaintext %d B sealed %d B (+%d sealed)",
			c.name, with.routeBody, with.tasksBytes, with.projectedRow, with.orchPlain, with.orchSealed,
			without.orchPlain, without.orchSealed, with.envelopes, with.passPlain, with.passSealed, with.passSealed-without.passSealed)
		if c.n == 0 && (with.orchPlain != without.orchPlain || with.tasksBytes != 0) {
			t.Errorf("no tasks changed the descriptor: %d bytes against %d", with.orchPlain, without.orchPlain)
		}
		if c.n == 100 && with.tasksBytes > TaskListBytesLimit {
			t.Errorf("%s: the list is %d bytes, past the %d byte bound the row bound was meant to keep it under",
				c.name, with.tasksBytes, TaskListBytesLimit)
		}
	}
}
