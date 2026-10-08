package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Callbacks: a wait handed to the daemon instead of held in a turn.
//
// A root that deploys, or waits on anything slow, used to keep its turn open:
// it ran the deploy and then polled — `curl BUILD.json` again, `task wait`
// again — and every poll was a turn that reread its whole context. A child
// already had the answer to that: when it finishes the daemon types a notice
// into its root's composer, and the root is free until then. A callback is the
// same answer for a command. The root names the command; this daemon runs it
// as a broker task with no tab; when it exits the task settles through the
// ordinary Settle, and everything after that — the notice and its ladder,
// `unacknowledged_completions`, `task show`, `task wait`, the ACK, the Board
// line — is the child's path unchanged.
//
// Three rules, each from the review of the first plan:
//
//   - **A callback is what the route wrote, not what a brief says.** Only
//     StartCallback writes Record.Callback, and every decision below asks it.
//     A child's brief whose kind is "callback" is refused, so no child is ever
//     taken for one — exempted from child slots, settled by recovery, killed.
//   - **Liveness is a lock the tree inherits, not a pid.** The command is
//     started holding an exclusive flock on <dir>/run.lock; every descendant
//     inherits the open file, so the lock is held exactly while any of the
//     tree lives. "Has it ended" is one non-blocking flock, and a pid that
//     was reused for something else cannot answer it wrongly.
//   - **A signal goes only to a group proven to be ours.** In the process that
//     started it, an unreaped leader pins its pid; after a restart, the pid
//     must be alive with the start time recorded when it began. Otherwise
//     nothing is signalled and the verdict says so.
//
// Starting and stopping are outbox effects (D08): recorded in the transaction
// that owes them, run after it commits, and run again after a crash — which
// they may be, because each finds out from the task directory whether it
// already happened.

// TaskKindCallback is a callback's Kind, for display only (see Callback).
const TaskKindCallback = "callback"

const (
	CallbackIntentWait  = "wait"
	CallbackIntentHeavy = "heavy"
)

const (
	// EffectCallbackStart starts a callback's command. Idempotent by the
	// evidence in its task directory: an exit file or a held lock means it
	// already started, and it is adopted rather than started twice.
	EffectCallbackStart = "callback.start"
	// EffectCallbackStop stops a callback's process group when the task is
	// settled by anything other than its own exit — its timeout, a cancel.
	EffectCallbackStop = "callback.stop"
)

// Bounds on a callback, registered in internal/domain/capacity.
const (
	// MaxCallbackArgs is how many words one callback's command may have.
	MaxCallbackArgs = 64
	// CallbackCommandLimit is the bytes of all of a command's words together.
	CallbackCommandLimit = 16 << 10
	// MaxCallbacksPerRoot is how many callbacks one root may have running.
	MaxCallbacksPerRoot = 8
	// MaxCallbacksPerMachine is how many may run on this machine at once.
	MaxCallbacksPerMachine = 16
	// CallbackLogLimit is the size at which a running callback's output.log
	// is emptied, with a line saying so; the command keeps appending.
	CallbackLogLimit = 8 << 20
	// CallbackTailLimit is how much of the end of output.log is read for the
	// verdict.
	CallbackTailLimit = 8 << 10
	// CallbackRetentionDays is how long a settled callback's files are kept.
	CallbackRetentionDays = 7
)

// callbackStopGrace is how long a stopped group has between TERM and KILL.
const callbackStopGrace = 5 * time.Second

// callbackEnv is the environment a caller may hand its command: what a deploy
// needs to find its tools, and nothing that is a credential. The daemon's own
// environment is not used — on Linux its PATH is the unit's, and on macOS it
// is launchd's, and neither has the tools a person's shell has.
var callbackEnv = map[string]bool{
	"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"USER": true, "LOGNAME": true, "SHELL": true, "TMPDIR": true, "TERM": true,
}

// CallbackEnvNames is the names a caller may pass, sorted.
func CallbackEnvNames() []string {
	return []string{"HOME", "LANG", "LC_ALL", "LC_CTYPE", "LOGNAME", "PATH", "SHELL", "TERM", "TMPDIR", "USER"}
}

// Callback is the command a callback runs and what became of it.
type Callback struct {
	// Intent is declared when the callback is created. An older callback with
	// no intent is treated as a wait, never as proof of heavy work.
	Intent string            `json:"intent,omitempty"`
	Argv   []string          `json:"argv"`
	Dir    string            `json:"dir"`
	Env    map[string]string `json:"env,omitempty"`
	// PID and PGID are the wrapper's, once it started; LeaderStart is that
	// pid's start time in Unix seconds as the kernel gave it then (sysctl on
	// macOS, /proc on Linux — no locale or time zone in it), compared before
	// any signal after a restart.
	PID         int       `json:"pid,omitempty"`
	PGID        int       `json:"pgid,omitempty"`
	LeaderStart int64     `json:"leader_start,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	// Exit is the command's exit status, or Signal what killed it. Both
	// empty on a callback still running, or one whose outcome is unknown.
	Exit   *int   `json:"exit,omitempty"`
	Signal string `json:"signal,omitempty"`
}

// CallbackRequest is POST /v1/orchestrator/callbacks.
type CallbackRequest struct {
	TaskID         string
	Intent         string
	Title          string
	Argv           []string
	Dir            string
	Env            map[string]string
	TimeoutMinutes int
	WorkID         string
	Root           RootRef
}

// callbackRuns is the callbacks this process started and is still waiting on.
type callbackRuns struct {
	mu   sync.Mutex
	runs map[string]*callbackRun
}

type callbackRun struct {
	pgid   int
	waited bool
}

func (c *callbackRuns) put(id string, pgid int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs == nil {
		c.runs = map[string]*callbackRun{}
	}
	c.runs[id] = &callbackRun{pgid: pgid}
}

// waiting reports whether this process started id and has not reaped it.
func (c *callbackRuns) waiting(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.runs[id]
	return r != nil && !r.waited
}

func (c *callbackRuns) reaped(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.runs, id)
}

// callbackFiles are the paths a callback's evidence lives at.
type callbackFiles struct{ dir string }

func (f callbackFiles) log() string  { return filepath.Join(f.dir, "output.log") }
func (f callbackFiles) lock() string { return filepath.Join(f.dir, "run.lock") }
func (f callbackFiles) pid() string  { return filepath.Join(f.dir, "pid") }
func (f callbackFiles) exit() string { return filepath.Join(f.dir, "exit") }

// attempt is written, durably, by the daemon just before it starts the
// command: the one fact that tells "never started" from "started and died".
func (f callbackFiles) attempt() string { return filepath.Join(f.dir, "attempt") }

// callbackWrapper is the shell the command runs under. It records its own
// pid, runs the command, and writes the exit status where a daemon that
// restarted meanwhile can still read it.
const callbackWrapper = `d=$CLAWDLINE_CALLBACK_DIR; echo $$ > "$d/pid"; "$@"; c=$?; ` +
	`printf '%s' "$c" > "$d/exit.tmp" && mv "$d/exit.tmp" "$d/exit"; exit $c`

// StartCallback admits a callback and starts its command.
func (b *Broker) StartCallback(ctx context.Context, req CallbackRequest) (Dispatched, error) {
	bad := func(msg string) (Dispatched, error) {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "bad_task", msg)
	}
	badRaw := func(msg string) (Dispatched, error) {
		return Dispatched{}, refuseRaw(http.StatusUnprocessableEntity, "bad_task", msg)
	}
	if !IsTaskID(req.TaskID) {
		return bad("task_id must be a lowercase UUID.")
	}
	intent := req.Intent
	if intent == "" {
		intent = CallbackIntentWait
	}
	if intent != CallbackIntentWait && intent != CallbackIntentHeavy {
		return bad("intent must be wait or heavy.")
	}
	held, _, err := b.Record(ctx, req.TaskID)
	switch {
	case err == nil:
		if held.Callback == nil {
			return Dispatched{}, refuseWith(http.StatusConflict, "task_exists",
				"A task with this id exists and is not a callback; nothing was started.", map[string]any{"task": held.ID})
		}
		priorIntent := held.Callback.Intent
		if priorIntent == "" {
			priorIntent = CallbackIntentWait
		}
		if priorIntent != intent {
			return Dispatched{}, refuseWith(http.StatusConflict, "intent_conflict",
				"This callback id already has a different intent; nothing was started.", map[string]any{"task": held.ID})
		}
		return Dispatched{Record: held, Replayed: true}, nil
	case !isNotFound(err):
		return Dispatched{}, err
	}
	if !callbackSupported {
		return Dispatched{}, refuse(http.StatusNotImplemented, "no_callback_capability",
			"This platform cannot run a callback yet: it has no way to stop the command's whole process tree.")
	}
	title := strings.TrimSpace(req.Title)
	if reason := work.OutcomeTitleRefusal(title); reason != "" {
		return badRaw("title: " + reason)
	}
	if len(req.Argv) == 0 || len(req.Argv) > MaxCallbackArgs {
		return bad(fmt.Sprintf("argv must have 1 to %d words.", MaxCallbackArgs))
	}
	size := 0
	for _, a := range req.Argv {
		size += len(a)
		if strings.IndexByte(a, 0) >= 0 {
			return bad("argv may not contain a NUL byte.")
		}
	}
	if size > CallbackCommandLimit || req.Argv[0] == "" {
		return bad(fmt.Sprintf("argv must name a command, and all its words together are at most %d bytes.", CallbackCommandLimit))
	}
	if !filepath.IsAbs(req.Dir) {
		return bad("dir must be an absolute path.")
	}
	if st, err := os.Stat(req.Dir); err != nil || !st.IsDir() {
		return badRaw("dir is not a directory on this machine: " + req.Dir)
	}
	env := map[string]string{}
	for k, v := range req.Env {
		if !callbackEnv[k] {
			return badRaw("env may name only " + strings.Join(CallbackEnvNames(), ", ") + "; " + k + " is not one of them.")
		}
		if strings.IndexByte(v, 0) >= 0 || len(v) > CallbackCommandLimit {
			return badRaw("env " + k + " is not a usable value.")
		}
		env[k] = v
	}
	timeout := req.TimeoutMinutes
	if timeout == 0 {
		timeout = 30
	}
	if timeout < 1 || timeout > 240 {
		return bad("timeout_minutes must be 1 to 240.")
	}
	if req.Root.SessionID == "" {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "root_session_required",
			"A callback wakes the session that asked for it; root.session_id names that conversation.")
	}
	if req.Root.Assistant != "claude" && req.Root.Assistant != "codex" {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "root_assistant_required",
			"root.assistant must be claude or codex.")
	}
	if req.WorkID != "" && !IsTaskID(req.WorkID) {
		return bad("work_id must be a lowercase UUID naming a work item, or left out.")
	}
	root := &RootRef{SessionID: req.Root.SessionID, Assistant: req.Root.Assistant,
		ProjectDir: filepath.Clean(req.Dir), Label: req.Root.Label}
	record := Record{
		Protocol:       Protocol,
		ID:             req.TaskID,
		Kind:           TaskKindCallback,
		Assistant:      req.Root.Assistant,
		Claims:         []string{},
		Isolation:      IsolationNone,
		LeaseScope:     LeaseShared,
		ProjectDir:     filepath.Clean(req.Dir),
		Title:          title,
		Instructions:   shellWords(req.Argv),
		TimeoutMinutes: timeout,
		Root:           root,
		WorkID:         req.WorkID,
		Callback:       &Callback{Intent: intent, Argv: append([]string(nil), req.Argv...), Dir: filepath.Clean(req.Dir), Env: env},
	}
	if err := b.bindLine(ctx, &record); err != nil {
		return Dispatched{}, err
	}
	if err := b.checkNamedWork(ctx, record); err != nil {
		return Dispatched{}, err
	}
	rootTerminal, err := b.resolveRoot(ctx, record.Root)
	if err != nil {
		return Dispatched{}, err
	}
	record.RootTerminalID = rootTerminal

	live, err := b.liveTasks(ctx)
	if err != nil {
		return Dispatched{}, err
	}
	mine, all := 0, 0
	for _, other := range live {
		if other.Callback == nil {
			continue
		}
		all++
		if other.Root != nil && other.Root.SessionID == root.SessionID {
			mine++
		}
	}
	if mine >= MaxCallbacksPerRoot {
		return Dispatched{}, refuseWith(http.StatusTooManyRequests, "callback_capacity",
			fmt.Sprintf("This session already has %d callbacks running; retry when one ends.", MaxCallbacksPerRoot),
			map[string]any{"retry_after": 60})
	}
	if all >= MaxCallbacksPerMachine {
		return Dispatched{}, refuseWith(http.StatusTooManyRequests, "callback_capacity",
			fmt.Sprintf("This machine already has %d callbacks running; retry when one ends.", MaxCallbacksPerMachine),
			map[string]any{"retry_after": 60})
	}
	if repo, err := b.Git.Toplevel(ctx, record.ProjectDir); err == nil {
		record.Repository = repo
	}

	record.CreatedAt = b.now()
	record.Dir = b.Tasks.Path(record.ID)
	// Running from the moment it is recorded: there is no tab to open or
	// brief, and `queued` would let the orphan sweep mistake it for a
	// dispatch whose caller went away.
	record.State = StateBriefed
	record.Dispatcher = b.Store.Owner()
	if err := os.MkdirAll(record.Dir, 0o700); err != nil {
		return Dispatched{}, refuseRaw(http.StatusInternalServerError, "callback_dir_unavailable",
			"Could not make the callback's task directory: "+err.Error())
	}
	ids, err := b.create(ctx, record, "", store.Effect{Kind: EffectCallbackStart, Subject: record.ID})
	if err != nil {
		return Dispatched{}, err
	}
	warnings := []Warning{}
	if err := b.commitNamedWork(ctx, record); err != nil {
		warnings = append(warnings, Warning{Code: "work_not_placed", Task: record.ID,
			Message: "The work item this callback names could not be moved onto the board (" + err.Error() +
				"); the board's sweep makes the same change from the same facts within a tick."})
	}
	b.runRecorded(ctx, ids)
	now, _, err := b.Record(ctx, record.ID)
	if err != nil {
		now = record
	}
	return Dispatched{Record: now, Warnings: warnings}, nil
}

// The two callback effects find out from the callback's task directory whether
// they already happened, so a recovery may run them again. They are added here
// rather than in the table's literal because starting one can settle its task,
// and settling runs effects: the literal would refer to itself.
func init() {
	effectHandlers[EffectCallbackStart] = effectHandler{idempotent: true, run: runCallbackStart}
	effectHandlers[EffectCallbackStop] = effectHandler{idempotent: true, run: runCallbackStop}
}

// runCallbackStart is EffectCallbackStart.
func runCallbackStart(ctx context.Context, b *Broker, e store.Effect) effectResult {
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil {
		return effectResult{state: store.EffectFailed, outcome: "the callback could not be read: " + err.Error()}
	}
	if r.Callback == nil {
		return effectResult{state: store.EffectFailed, outcome: "task " + r.ID + " is not a callback"}
	}
	if r.State.Terminal() {
		return effectResult{state: store.EffectDone, outcome: "the callback had already ended"}
	}
	files := callbackFiles{r.Dir}
	// Already ran, or running: this is a recovery's second attempt, and the
	// first one got as far as the command.
	if _, err := os.Stat(files.exit()); err == nil {
		return effectResult{state: store.EffectDone, outcome: "the command had already run; its exit status is on disk"}
	}
	if held, err := lockHeld(files.lock()); err == nil && held {
		// The wrapper writes its pid first thing; a start caught between
		// exec and that line is given the moment it needs.
		pid, _ := readPID(files.pid())
		for wait := 0; pid == 0 && wait < 50 && ctx.Err() == nil; wait++ {
			time.Sleep(200 * time.Millisecond)
			pid, _ = readPID(files.pid())
		}
		if pid == 0 {
			return effectResult{state: store.EffectFailed, outcome: "its lock is held but no pid was written; " +
				"not adopted, and nothing was started"}
		}
		if r.Callback.PID == 0 {
			start := leaderStart(pid)
			_, _ = b.mutate(ctx, r.ID, "task.callback.adopted", func(x *Record) error {
				if x.Callback != nil && x.Callback.PID == 0 {
					x.Callback.PID, x.Callback.PGID, x.Callback.LeaderStart = pid, pid, start
				}
				return nil
			})
		}
		return effectResult{state: store.EffectDone, outcome: "the command was already running; adopted"}
	}
	// Attempted before, and neither finished nor running: whatever happened
	// to it — the machine went down with it, it was killed — it is not run a
	// second time.
	if _, err := os.Stat(files.attempt()); err == nil {
		b.settleCallbackExit(ctx, r.ID, nil, "", 0)
		return effectResult{state: store.EffectDone, outcome: "started before and ended with no exit status; not run again"}
	}
	proc, err := startCallbackProcess(r, files, b.callbackLive(ctx, r.ID))
	if errors.Is(err, errCallbackSettled) {
		return effectResult{state: store.EffectDone, outcome: "the callback was settled before its command started; not started"}
	}
	if errors.Is(err, errCallbackUnread) {
		// Not started and not settled: the beat settles it "never started"
		// once nothing is left to start it, and no command ran.
		return effectResult{state: store.EffectFailed, outcome: "not started: " + err.Error()}
	}
	if err != nil {
		if _, serr := b.Settle(ctx, r.ID, StateFailure, "The command could not be started: "+err.Error(), nil); serr != nil &&
			!errors.Is(serr, errAlreadyTerminal) {
			return effectResult{state: store.EffectFailed, outcome: "start failed and could not be recorded: " + serr.Error()}
		}
		return effectResult{state: store.EffectDone, outcome: "the command could not be started: " + err.Error()}
	}
	b.callbacks.put(r.ID, proc.pgid)
	start := leaderStart(proc.pid)
	began := b.now()
	_, _ = b.mutate(ctx, r.ID, "task.callback.started", func(x *Record) error {
		if x.Callback != nil {
			x.Callback.PID, x.Callback.PGID, x.Callback.LeaderStart, x.Callback.StartedAt = proc.pid, proc.pgid, start, began
		}
		return nil
	})
	go b.awaitCallback(r.ID, proc, began)
	return effectResult{state: store.EffectDone, outcome: fmt.Sprintf("started pid %d", proc.pid)}
}

// awaitCallback reaps a callback this process started and settles it on how
// it ended. A settlement that lost the race to a timeout or a cancel is not
// an error: the task already says why it ended.
func (b *Broker) awaitCallback(id string, proc *callbackProcess, began time.Time) {
	exit, signal := proc.wait()
	ctx, cancel := context.WithTimeout(context.Background(), effectLifetime)
	defer cancel()
	// Settled before it is forgotten, so the beat cannot read the gap as a
	// command nobody is waiting on and call a signal "unknown".
	b.settleCallbackExit(ctx, id, exit, signal, b.now().Sub(began))
	b.callbacks.reaped(id)
}

// settleCallbackExit settles a callback on its own exit.
func (b *Broker) settleCallbackExit(ctx context.Context, id string, exit *int, signal string, took time.Duration) {
	r, _, err := b.Record(ctx, id)
	if err != nil || r.State.Terminal() {
		return
	}
	state := StateFailure
	head := ""
	switch {
	case exit != nil:
		head = fmt.Sprintf("exit %d after %s", *exit, roundDuration(took))
		if *exit == 0 {
			state = StateSuccess
		}
	case signal != "":
		head = fmt.Sprintf("killed by %s after %s", signal, roundDuration(took))
	case !attempted(callbackFiles{r.Dir}):
		head = "never started: the daemon that was to start it stopped first, and it was not started later"
	default:
		head = "outcome unknown: the command ended while no daemon was watching it and wrote no exit status; it was not run again"
	}
	why := callbackVerdict(head, callbackFiles{r.Dir})
	_, _, err = b.settleWith(ctx, id, state, why, nil, func(x *Record) {
		if x.Callback != nil {
			x.Callback.Exit, x.Callback.Signal = exit, signal
		}
	})
	if err != nil && !errors.Is(err, errAlreadyTerminal) {
		// The beat tries again from the exit file on its next pass.
		return
	}
}

// tendCallback is the beat's look at one live callback. It answers true when
// it settled it.
func (b *Broker) tendCallback(ctx context.Context, r Record) bool {
	files := callbackFiles{r.Dir}
	trimCallbackLog(files)
	if !r.Deadline().IsZero() && !b.now().Before(r.Deadline()) {
		why := callbackVerdict(fmt.Sprintf("stopped at its %d-minute timeout", r.TimeoutMinutes), files)
		_, ids, err := b.settle(ctx, r.ID, StateTimeout, why, nil, callbackStopEffect(r))
		if err == nil {
			b.runRecorded(ctx, ids)
			return true
		}
		return false
	}
	if b.callbacks.waiting(r.ID) {
		return false
	}
	// Not this process's to wait on. Only a callback this process recorded,
	// or one whose recorder is gone, is this process's to settle: two
	// daemons on one store must not both decide one.
	if r.Dispatcher != "" && r.Dispatcher != b.Store.Owner() && !b.Store.OwnerGone(r.Dispatcher) {
		return false
	}
	if r.Callback.PID == 0 && !attempted(files) {
		// Its start effect has not run yet; RecoverEffects owns that. A
		// start still open is left to finish — the timeout above is what
		// ends one that never does.
		if b.now().Sub(r.CreatedAt) < unattendedStarted || b.startOpen(ctx, r.ID) {
			return false
		}
	}
	if raw, err := os.ReadFile(files.exit()); err == nil {
		code, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		var exit *int
		if err == nil {
			exit = &code
		}
		b.settleCallbackExit(ctx, r.ID, exit, "", b.now().Sub(callbackBegan(r)))
		return true
	}
	// Ended, with no exit status — the wrapper itself was killed, or the
	// command never started — or about to start. The lock decides which, and
	// is held while the task is settled, so a start that was waiting for it
	// finds the task settled and does not begin.
	held, err := fence(files.lock())
	if err != nil || held == nil {
		return false
	}
	defer unfence(held)
	if _, err := os.Stat(files.exit()); err == nil {
		return false // it finished as the lock was taken; the next pass reads it
	}
	b.settleCallbackExit(ctx, r.ID, nil, "", 0)
	return true
}

// startOpen reports whether r's start effect is still to run, or running.
func (b *Broker) startOpen(ctx context.Context, id string) bool {
	effects, err := b.Store.Effects(ctx, EffectCallbackStart, id)
	if err != nil {
		return true
	}
	for _, e := range effects {
		if e.State == store.EffectPending || e.State == store.EffectStarted {
			return true
		}
	}
	return false
}

// attempted reports whether a start ever got as far as its marker.
func attempted(files callbackFiles) bool {
	_, err := os.Stat(files.attempt())
	return err == nil
}

// callbackLive is the check a start makes once it holds the lock: the task,
// read again, must still be running.
func (b *Broker) callbackLive(ctx context.Context, id string) func() error {
	return func() error {
		now, _, err := b.Record(ctx, id)
		switch {
		case err != nil:
			return fmt.Errorf("%w: %v", errCallbackUnread, err)
		case now.State.Terminal():
			return errCallbackSettled
		}
		return nil
	}
}

// errCallbackSettled is a start that found its task already settled.
var errCallbackSettled = errors.New("the callback was settled before its command started")

// errCallbackUnread is a start that could not reread its task under the lock.
var errCallbackUnread = errors.New("the callback could not be read again before starting")

func callbackBegan(r Record) time.Time {
	if r.Callback != nil && !r.Callback.StartedAt.IsZero() {
		return r.Callback.StartedAt
	}
	return r.CreatedAt
}

// callbackStopEffect is the stop a settlement of r owes, as an outbox row.
func callbackStopEffect(r Record) store.Effect {
	return store.Effect{Kind: EffectCallbackStop, Subject: r.ID}
}

// runCallbackStop is EffectCallbackStop.
func runCallbackStop(ctx context.Context, b *Broker, e store.Effect) effectResult {
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil || r.Callback == nil {
		return effectResult{state: store.EffectFailed, outcome: "the callback could not be read"}
	}
	files := callbackFiles{r.Dir}
	if held, err := lockHeld(files.lock()); err == nil && !held {
		return effectResult{state: store.EffectDone, outcome: "the command had already ended"}
	}
	// The lock is held. A start may be between its exec and recording the
	// pid; give it a moment rather than calling a running command unstarted.
	pid, pgid, recorded := r.Callback.PID, r.Callback.PGID, r.Callback.LeaderStart
	for wait := 0; pid == 0 && wait < 50 && ctx.Err() == nil; wait++ {
		time.Sleep(200 * time.Millisecond)
		if held, err := lockHeld(files.lock()); err == nil && !held {
			return effectResult{state: store.EffectDone, outcome: "the command ended, or its start found the task settled"}
		}
		if now, _, err := b.Record(ctx, r.ID); err == nil && now.Callback != nil {
			pid, pgid, recorded = now.Callback.PID, now.Callback.PGID, now.Callback.LeaderStart
		}
	}
	if pid == 0 {
		// Final: a failed effect is not tried again.
		return effectResult{state: store.EffectFailed, outcome: "not signalled, and not tried again: its lock is held but " +
			"no started pid was recorded, so nothing could be proven to be its own"}
	}
	ours := b.callbacks.waiting(r.ID) && sameGroup(pid, pgid)
	if !ours && recorded != 0 && sameGroup(pid, pgid) {
		// Kernel start times read again may differ by a second on Linux,
		// where they are derived from boot time.
		if d := leaderStart(pid) - recorded; d >= -1 && d <= 1 {
			ours = true
		}
	}
	if !ours {
		return effectResult{state: store.EffectDone, outcome: "not signalled: the command's leader is gone, " +
			"so the processes still holding its lock cannot be proven to be its own"}
	}
	if err := stopGroup(pid, callbackStopGrace); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "could not stop the command: " + err.Error()}
	}
	return effectResult{state: store.EffectDone, outcome: fmt.Sprintf("stopped process group %d", pid)}
}

// callbackVerdict is head and the last lines of the command's output, cut so
// the whole fits the verdict (summaryLimit) and the last line survives.
func callbackVerdict(head string, files callbackFiles) string {
	tail := readTail(files.log(), CallbackTailLimit)
	budget := summaryLimit - utf8.RuneCountInString(head) - 64
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	kept := []string{}
	used := 0
	for i := len(lines) - 1; i >= 0 && len(kept) < 20; i-- {
		n := utf8.RuneCountInString(lines[i]) + 1
		if used+n > budget {
			break
		}
		kept = append([]string{lines[i]}, kept...)
		used += n
	}
	if len(kept) == 0 || (len(kept) == 1 && kept[0] == "") {
		return head + "; it printed nothing — output: " + files.log()
	}
	return head + "; last lines of " + files.log() + ":\n" + strings.Join(kept, "\n")
}

func readTail(path string, limit int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	from := st.Size() - limit
	if from < 0 {
		from = 0
	}
	buf := make([]byte, st.Size()-from)
	n, _ := f.ReadAt(buf, from)
	text := string(buf[:n])
	if from > 0 {
		// The first line read is probably cut; drop it.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return strings.ToValidUTF8(text, "?")
}

// trimCallbackLog empties an output.log past CallbackLogLimit and says so in
// it. The command's descriptor is O_APPEND, so it goes on writing at the new
// end; the bytes dropped are the oldest, and a verdict reads only the end.
func trimCallbackLog(files callbackFiles) {
	st, err := os.Stat(files.log())
	if err != nil || st.Size() <= CallbackLogLimit {
		return
	}
	keep := readTail(files.log(), CallbackTailLimit)
	f, err := os.OpenFile(files.log(), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return
	}
	_, _ = io.WriteString(f, fmt.Sprintf("[clawdline: output passed %d MiB; earlier output was dropped]\n%s",
		CallbackLogLimit>>20, keep))
}

// leaderStart is when pid started, in Unix seconds, or 0 when the kernel would
// not say. Two readings of one process agree; a reused pid does not.
func leaderStart(pid int) int64 {
	at := swiftstore.ProcessStart(pid)
	if at.IsZero() {
		return 0
	}
	return at.Unix()
}

func readPID(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(raw)))
}

func roundDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d.Round(time.Second)
}

// shellWords is argv as a person would type it, for display.
func shellWords(argv []string) string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' || r == '@' || r == ',' || r == '+' ||
				(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
		}) < 0 {
			out = append(out, a)
			continue
		}
		out = append(out, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(out, " ")
}

// reclaimCallback removes a callback's evidence files — output.log, pid, exit,
// attempt, run.lock — once the store says it settled more than
// CallbackRetentionDays ago. Its record and task directory stay. The output
// may hold whatever a deploy printed, so it is not kept for ever; and this
// is the reclaim sweep's, under a reason of its own,
// because the sweep is the one place the broker deletes files (reclaim.go).
func (b *Broker) reclaimCallback(ctx context.Context, r Record, now time.Time, dryRun bool) ReclaimDecision {
	if r.Callback == nil || !r.State.Terminal() || r.Dir == "" || r.FinishedAt.IsZero() ||
		now.Sub(r.FinishedAt) < CallbackRetentionDays*24*time.Hour {
		return ReclaimDecision{}
	}
	files := callbackFiles{r.Dir}
	present := []string{}
	var bytes int64
	for _, p := range []string{files.log(), files.pid(), files.exit(), files.attempt(), files.lock()} {
		if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() {
			present = append(present, p)
			bytes += info.Size()
		}
	}
	if len(present) == 0 {
		return ReclaimDecision{}
	}
	if !ownedPath(b.Tasks.Dir, r.Dir, r.ID) {
		return kept(r, ReclaimCallbackFiles, r.Dir, WhyPathNotOwned, map[string]any{"root": b.Tasks.Dir})
	}
	d := ReclaimDecision{Task: r.ID, Subject: ReclaimCallbackFiles, Path: r.Dir, Reason: WhyCallbackFiles, Bytes: bytes,
		Evidence: map[string]any{"ended_at": r.FinishedAt.Unix(), "files": len(present)}}
	if dryRun {
		d.Outcome = ReclaimWouldRemove
		return d
	}
	intent := d
	intent.Outcome = reclaimRemoving
	if err := b.markReclaim(ctx, intent, b.now()); err != nil {
		d.Evidence["intent"] = err.Error()
		return kept(r, ReclaimCallbackFiles, r.Dir, WhyUnrecorded, d.Evidence)
	}
	for _, p := range present {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			d.Evidence["remove"] = err.Error()
			return kept(r, ReclaimCallbackFiles, r.Dir, WhyRemoveFailed, d.Evidence)
		}
	}
	d.Outcome = ReclaimRemoved
	return d
}

// callbackFinishedLine is the notice sentence for a callback: how its command
// ended, in the verdict's own first line, and the one command to read it.
func callbackFinishedLine(r Record, short, noticeID string) string {
	head := r.Verdict
	if i := strings.IndexAny(head, ";\n"); i >= 0 {
		head = head[:i]
	}
	line := fmt.Sprintf("callback %s finished: %s", short, r.State)
	if head != "" {
		line += " (" + head + ")"
	}
	if r.State == StateCancelled {
		line += "; its command was stopped"
	}
	return line + " — run " + ShowCommand(r.ID, noticeID)
}
