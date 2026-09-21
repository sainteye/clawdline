package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// W4 (docs/design-decisions.md §6): one dispatch path. A schedule's run goes
// through the broker, so it and a dispatched task see each other's claims
// before anything opens; it has its own secret, CHILD.md and timeout; and a
// run that never writes result.json ends on its own clock and lets the next
// occurrence run. Every test here drives a real broker and a real store; only
// the terminal is fake, and it counts what it was asked to open.

const w4Root = "379d0000-0000-4000-8000-000000000001"

// w4Terminal is the machine's terminals: one root session, and a tab per
// child the broker opens, each of which is an assistant ready to be typed at.
type w4Terminal struct {
	mu    sync.Mutex
	opens int
	typed map[string]string
}

func (f *w4Terminal) ITermRunning(context.Context) (bool, error) { return false, nil }
func (f *w4Terminal) TmuxReach(context.Context) int              { return 2 }
func (f *w4Terminal) NewITermTab(context.Context, string) (string, error) {
	return "", errors.New("no iTerm here")
}
func (f *w4Terminal) NewTmuxWindow(context.Context, string, string) (string, error) {
	return "", errors.New("never a window on somebody's server")
}
func (f *w4Terminal) NewTmuxSession(context.Context, string, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	return fmt.Sprintf("%%9%d", f.opens), nil
}
func (f *w4Terminal) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return true, nil
}

func (f *w4Terminal) opened() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens
}

func (f *w4Terminal) sessions() []session.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: w4Root}}
	for i := 1; i <= f.opens; i++ {
		out = append(out, session.Session{ID: fmt.Sprintf("%%9%d", i), Assistant: session.AssistantClaude})
	}
	return out
}

func (f *w4Terminal) typeLine(_ context.Context, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.typed == nil {
		f.typed = map[string]string{}
	}
	f.typed[id] = text
	return nil
}

func (f *w4Terminal) lineFor(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.typed[id]
}

type w4Fixture struct {
	dir   string
	st    *store.Store
	b     *orchestrator.Broker
	book  *ScheduleBook
	term  *w4Terminal
	now   time.Time
	clock sync.Mutex
}

func (f *w4Fixture) at() time.Time {
	f.clock.Lock()
	defer f.clock.Unlock()
	return f.now
}

func (f *w4Fixture) set(t time.Time) {
	f.clock.Lock()
	defer f.clock.Unlock()
	f.now = t
}

func newW4(t *testing.T) *w4Fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &w4Fixture{dir: dir, st: st, term: &w4Terminal{},
		now: time.Date(2026, 9, 18, 9, 0, 30, 0, time.UTC)}
	f.b = &orchestrator.Broker{
		Store:    st,
		Tasks:    taskdir.New(dir),
		Git:      git.New(),
		Dir:      dir,
		Launcher: f.term,
		Live:     func(context.Context) []session.Session { return f.term.sessions() },
		Type:     f.term.typeLine,
		Clock:    f.at,
	}
	f.book = &ScheduleBook{
		Store: st, Broker: f.b, Now: f.at, Location: time.UTC,
		IsDirectory: func(p string) bool {
			info, err := os.Stat(p)
			return err == nil && info.IsDir()
		},
	}
	return f
}

// schedule stores one daily-at-09:00 schedule whose template is task, seen
// long before today, and answers its id.
func (f *w4Fixture) schedule(t *testing.T, id, title string, task map[string]any) string {
	t.Helper()
	tmpl := map[string]any{"assistant": "claude", "project_dir": f.dir, "title": title,
		"instructions": "do the scheduled thing"}
	for k, v := range task {
		tmpl[k] = v
	}
	body, _ := json.Marshal(map[string]any{
		"clawdline_schedule": 1, "schedule_id": id, "title": title,
		"when": map[string]any{"at": "09:00", "days": "daily"},
		"task": tmpl, "enabled": true, "catch_up_hours": 6,
		"created_at": f.at().Add(-72 * time.Hour).Unix(),
	})
	if err := f.st.CreateScheduleFile(context.Background(), id, body,
		f.at().Add(-72*time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	return id
}

// dispatch sends an ordinary owned task through the broker's own route.
func (f *w4Fixture) dispatch(t *testing.T, id string, claims []string) (orchestrator.Dispatched, error) {
	t.Helper()
	brief, _ := json.Marshal(map[string]any{
		"clawdline_protocol": 1, "task_id": id, "assistant": "claude", "project_dir": f.dir,
		"title": "owned", "instructions": "do the owned thing", "claims": claims,
		"root": map[string]any{"session_id": w4Root, "assistant": "claude", "label": "the root"},
	})
	dir := f.b.Tasks.Path(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), brief, 0o600); err != nil {
		t.Fatal(err)
	}
	return f.b.Dispatch(context.Background(), orchestrator.DispatchRequest{
		TaskID: id, Secret: strings.Repeat("ab", 32)})
}

func (f *w4Fixture) runs(t *testing.T, schedule string) []store.ScheduleRun {
	t.Helper()
	runs, err := f.st.ScheduleRuns(context.Background(), schedule)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func brokerCode(err error) (string, map[string]any) {
	var ref orchestrator.Refusal
	if errors.As(err, &ref) {
		return ref.Code, ref.Extra
	}
	return "", nil
}

// ① A scheduled run and a dispatched task are arbitrated against each other,
// both ways round, and the refusal comes before anything is opened. Before W4
// the run went through app.Dispatcher, whose `tasks` table the broker never
// read and whose check never read the broker's: the same two dispatches both
// opened a tab.
func TestAScheduledRunAndADispatchedTaskRefuseEachOther(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	owned := "e4000001-0000-4000-8000-000000000001"
	if _, err := f.dispatch(t, owned, []string{"src"}); err != nil {
		t.Fatal(err)
	}
	opens := f.term.opened()

	// A run that claims under the owned task's claim: refused, 409, naming the
	// task in the way — and nothing opened, and the run not kept.
	blocked := f.schedule(t, "5c000001-0000-4000-8000-000000000001", "nightly sweep",
		map[string]any{"claims": []string{"src/app"}})
	reply := f.book.Run(ctx, blocked)
	if reply.Status != 409 || reply.Code != "workspace_busy" || reply.Extra["blocking_task"] != owned {
		t.Fatalf("a run over a dispatched task's claims answered %d %s %v", reply.Status, reply.Code, reply.Extra)
	}
	if got := f.term.opened(); got != opens {
		t.Fatalf("the refused run opened %d tab(s)", got-opens)
	}
	if runs := f.runs(t, blocked); len(runs) != 0 {
		t.Fatalf("the refused run is still linked to its schedule: %+v", runs)
	}

	// The control, with everything else the same: claims that do not overlap
	// are admitted. The refusal above is about the overlap and nothing else.
	apart := f.schedule(t, "5c000002-0000-4000-8000-000000000002", "docs sweep",
		map[string]any{"claims": []string{"docs"}})
	if reply := f.book.Run(ctx, apart); !reply.OK() {
		t.Fatalf("a run apart from the dispatched task was refused: %d %s %s", reply.Status, reply.Code, reply.Message)
	}

	// The other way round: a live run holds its claims against a dispatch.
	if _, err := f.b.Settle(ctx, owned, orchestrator.StateSuccess, "", nil); err != nil {
		t.Fatal(err)
	}
	if reply := f.book.Run(ctx, blocked); !reply.OK() {
		t.Fatalf("with the owned task finished the run was refused: %d %s %s", reply.Status, reply.Code, reply.Message)
	}
	run := f.runs(t, blocked)[0].TaskID
	opens = f.term.opened()
	_, err := f.dispatch(t, "e4000002-0000-4000-8000-000000000002", []string{"src/app/main.go"})
	code, extra := brokerCode(err)
	if code != "workspace_busy" || extra["blocking_task"] != run || extra["root_label"] != "nightly sweep" {
		t.Fatalf("a dispatch over a live run's claims answered %v (extra %v)", err, extra)
	}
	if got := f.term.opened(); got != opens {
		t.Fatalf("the refused dispatch opened %d tab(s)", got-opens)
	}
}

// ② A run is a task with its own secret, briefing and clock: the secret it
// was typed is the one its record authenticates, and no other; CHILD.md is in
// its directory and says who started it; its timeout is its template's. The
// old path gave it none of the three.
func TestAScheduledRunHasItsOwnSecretBriefingAndClock(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	id := f.schedule(t, "5c000003-0000-4000-8000-000000000003", "morning report",
		map[string]any{"claims": []string{}, "timeout_minutes": 7})
	reply := f.book.Run(ctx, id)
	if !reply.OK() {
		t.Fatalf("run: %d %s %s", reply.Status, reply.Code, reply.Message)
	}
	taskID, _ := reply.Body["task_id"].(string)
	record, _, err := f.b.Record(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ScheduleID != id || record.Root != nil || record.TimeoutMinutes != 7 ||
		!record.Deadline().Equal(record.CreatedAt.Add(7*time.Minute)) {
		t.Fatalf("the run's record: schedule %q root %v timeout %d", record.ScheduleID, record.Root, record.TimeoutMinutes)
	}

	line := f.term.lineFor(record.ChildTerminalID)
	secret := regexp.MustCompile(`TASK_SECRET=([0-9a-f]{64})$`).FindStringSubmatch(line)
	if secret == nil {
		t.Fatalf("the child was typed no secret: %q", line)
	}
	if _, _, err := f.b.Authenticate(ctx, taskID, secret[1]); err != nil {
		t.Fatalf("the typed secret does not authenticate its own task: %v", err)
	}
	if _, _, err := f.b.Authenticate(ctx, taskID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("any secret authenticates the run") // the control: the check can say no
	}

	brief, err := os.ReadFile(filepath.Join(f.b.Tasks.Path(taskID), "CHILD.md"))
	if err != nil {
		t.Fatalf("no CHILD.md: %v", err)
	}
	if !strings.Contains(string(brief), `started by the schedule "morning report"`) ||
		!strings.Contains(string(brief), "You have 7 minutes") {
		t.Fatalf("CHILD.md does not brief a scheduled run:\n%s", brief)
	}
	if strings.Contains(string(brief), secret[1]) {
		t.Fatal("the secret was written into CHILD.md")
	}
}

// ③ A run that never writes result.json ends on its own clock, and the next
// occurrence runs. The control is the same schedule a day earlier, with the
// broker's clock not yet run: the live run holds the occurrence back, which is
// the rule the timeout exists to release. Before W4 that hold had no end.
func TestARunThatNeverReportsTimesOutAndFreesItsSchedule(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	id := f.schedule(t, "5c000004-0000-4000-8000-000000000004", "hourly check",
		map[string]any{"timeout_minutes": 1})

	day1 := f.book.Beat(ctx)
	if day1.Due != 1 || day1.Fired != 1 {
		t.Fatalf("day 1: %+v", day1)
	}
	first := f.runs(t, id)[0].TaskID
	if reply := f.book.Run(ctx, id); reply.Code != "schedule_active" {
		t.Fatalf("a second run while the first is live answered %d %s", reply.Status, reply.Code)
	}

	// Day 2, the broker's beat not run: the run is still live, and holds it.
	f.set(f.at().Add(24 * time.Hour))
	if day2 := f.book.Beat(ctx); day2.Fired != 0 {
		t.Fatalf("day 2 fired past a live run: %+v", day2)
	}

	// Day 3: the broker's beat runs the run's own clock first.
	f.set(f.at().Add(24 * time.Hour))
	pass := f.b.Pass(ctx)
	if pass.TimedOut != 1 {
		t.Fatalf("the broker's pass timed out %d task(s), want the run", pass.TimedOut)
	}
	ended, _, err := f.b.Record(ctx, first)
	if err != nil || ended.State != orchestrator.StateTimeout || ended.Result != nil {
		t.Fatalf("the run ended as %q (result %v, err %v)", ended.State, ended.Result, err)
	}
	day3 := f.book.Beat(ctx)
	if day3.Due != 1 || day3.Fired != 1 {
		t.Fatalf("day 3, with the run timed out: %+v", day3)
	}
	runs := f.runs(t, id)
	if len(runs) != 2 || runs[0].TaskID == first || runs[1].State != string(orchestrator.StateTimeout) {
		t.Fatalf("runs after day 3: %+v", runs)
	}
}

// ④ The older skeleton's tables are not made in a fresh store, and a store
// that still has them is not written by a scheduled run from dispatch to
// timeout. Only the broker's tables change.
func TestTheRetiredTablesAreNotWritten(t *testing.T) {
	fresh := newW4(t)
	db, err := sql.Open("sqlite", filepath.Join(fresh.dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	retired := []string{"tasks", "obligations", "receipts", "board", "board_commands"}
	for _, name := range retired {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil || n != 0 {
			t.Errorf("a fresh store made the retired table %s (%d, %v)", name, n, err)
		}
	}

	// An older store, with the tables in it.
	dir := t.TempDir()
	old, err := sql.Open("sqlite", filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, assistant TEXT, project TEXT, claims TEXT, state TEXT, created_at INTEGER)`,
		`CREATE TABLE obligations (id TEXT PRIMARY KEY, kind TEXT, subject TEXT, mover_kind TEXT, mover_id TEXT, opened_at INTEGER, evidence TEXT, note TEXT, closed_at INTEGER)`,
		`CREATE TABLE receipts (command_id TEXT PRIMARY KEY, at INTEGER, seq INTEGER)`,
	} {
		if _, err := old.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	_ = old.Close()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := newW4(t)
	_ = f.st.Close()
	f.dir, f.st = dir, st
	f.b.Store, f.b.Tasks, f.b.Dir = st, taskdir.New(dir), dir
	f.book.Store = st
	t.Cleanup(func() { _ = st.Close() })
	id := f.schedule(t, "5c000005-0000-4000-8000-000000000005", "old store", map[string]any{"timeout_minutes": 1})
	if reply := f.book.Run(context.Background(), id); !reply.OK() {
		t.Fatalf("run: %d %s %s", reply.Status, reply.Code, reply.Message)
	}
	f.set(f.at().Add(2 * time.Minute))
	if p := f.b.Pass(context.Background()); p.TimedOut != 1 {
		t.Fatalf("pass: %+v", p)
	}
	check, err := sql.Open("sqlite", filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	for _, name := range []string{"tasks", "obligations", "receipts"} {
		var n int
		if err := check.QueryRow(`SELECT COUNT(*) FROM ` + name).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s holds %d row(s) after a scheduled run (%v)", name, n, err)
		}
	}
	var brokerRows int
	if err := check.QueryRow(`SELECT COUNT(*) FROM broker_tasks`).Scan(&brokerRows); err != nil || brokerRows != 1 {
		t.Fatalf("the broker holds %d task(s) (%v); the control that this store was written at all", brokerRows, err)
	}
}

// A run the machine has no room for is handed back to the timer, not spent:
// the broker's admission is the back-pressure (G30). The occurrence runs on
// the next pass once there is room.
func TestAFullMachineHandsTheOccurrenceBack(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.b.MaxChildren = 1 // four children on the machine
	fill := []string{}
	for i := 0; i < 4; i++ {
		id := f.schedule(t, fmt.Sprintf("5c00001%d-0000-4000-8000-000000000000", i), fmt.Sprintf("filler %d", i), nil)
		if reply := f.book.Run(ctx, id); !reply.OK() {
			t.Fatalf("filler %d: %d %s %s", i, reply.Status, reply.Code, reply.Message)
		}
		fill = append(fill, f.runs(t, id)[0].TaskID)
	}
	id := f.schedule(t, "5c000006-0000-4000-8000-000000000006", "when there is room", nil)
	if p := f.book.Beat(ctx); p.Due != 1 || p.Fired != 0 {
		t.Fatalf("a full machine: %+v", p)
	}
	if runs := f.runs(t, id); len(runs) != 0 {
		t.Fatalf("a refused run stayed linked: %+v", runs)
	}
	if _, err := f.b.Settle(ctx, fill[0], orchestrator.StateSuccess, "", nil); err != nil {
		t.Fatal(err)
	}
	if p := f.book.Beat(ctx); p.Due != 1 || p.Fired != 1 {
		t.Fatalf("with room again, the same occurrence: %+v", p)
	}
}
