package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/machineusage"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline heavy -- <command>` runs a build, a test suite or anything else
// that needs much of the machine, after the machine can take it.
//
// On 2026-09-26 this machine had 2 cores and 1.9 GB (later 3.7 GB), and
// several sessions each ran `go test ./...` or `go vet ./...` whenever their
// own work reached that step. Overlapping, they drove the load to 26-34 and
// swapped 800 pages a second; every read in the console took 8-74 s, and
// Claude Code killed the background checks of the one session trying to land,
// twice, for low memory. None of those builds was wrong alone.
//
// So a heavy command asks two things first:
//
//  1. The machine's one compile slot: the broker's `heavy_compile` lease
//     (internal/app/orchestrator/leases.go), which already knew how to queue,
//     to pass over a waiter that stopped asking, and to hand the slot on when a
//     holder's process is gone. It had no caller; this is it.
//  2. Memory: the kernel's available memory above a floor, and its memory
//     pressure-stall average below a ceiling (machineusage, the same reading
//     the dashboard draws).
//
// **It fails open, on purpose.** docs/records/machine-resource-scheduling-2026-09.md records a
// lease that twice stopped a build from reaching its compiler — on the path
// taken when the broker was not answering, which is the path after a crash,
// exactly when somebody needs to rebuild. So a daemon that does not answer, or
// a refusal this command does not understand, runs the command anyway and says
// so. Waiting is the point; refusing to build never is.
//
// A wait longer than --max-wait is different: the machine is answering, and
// busy. Running beside the holder anyway is the overlap this command exists
// to prevent, so it leaves the line and exits with heavyExitTimedOut without
// running the command — a code the caller can tell from the command's own
// failure, and retry.
//
// **It is quiet while it waits.** An agent watching its output wakes on every
// line, and a token review on 2026-10-01 counted at least 140 agent turns spent
// reading "waiting for the compile slot: number N in line" as the number
// moved. So a wait says one line when it starts and one when it ends, and
// nothing in between.
//
// The command itself runs at a lower CPU priority, and on Linux as the first
// thing the kernel's out-of-memory killer takes, ahead of the sessions a person
// is typing into. A `heavy` inside a `heavy` (a script that wraps its own
// steps) runs directly: asking for the slot its own parent holds would queue
// behind itself.

// heavyEnv is set in the command's environment to the request id holding the
// slot, so a nested `heavy` knows.
const heavyEnv = "CLAWDLINE_HEAVY"

// heavyRenewEvery is how often a holder proves itself alive; the broker's
// renewal lapses after 60 seconds.
const heavyRenewEvery = 20 * time.Second

// heavyPollEvery is how often a queued asker or a memory wait looks again.
const heavyPollEvery = 5 * time.Second

// heavyDefaultWait is how long the slot and memory together are waited for
// before giving up with heavyExitTimedOut.
const heavyDefaultWait = 30 * time.Minute

// heavyExitTimedOut is the exit status when --max-wait passed before the slot
// or the memory was had, and the command was not run: EX_TEMPFAIL from
// sysexits.h, "try again later". Nothing else in this repository exits 75.
const heavyExitTimedOut = 75

// heavyExitHandedOff means the command was accepted as a callback; end this
// turn and wait for its notice rather than running another copy.
const heavyExitHandedOff = 76

// heavyPressureCeiling is the memory stall average (percent of the last ten
// seconds some task waited on memory) above which a start waits. 10 is where
// the dashboard calls memory short.
const heavyPressureCeiling = 10.0

// heavyFloorCap is the most the automatic available-memory floor asks for: a
// quarter of the machine, up to one GiB. A Go test run of this repository
// peaked near 1 GB on 2026-09-26.
const heavyFloorCap = 1 << 30

type heavyOptions struct {
	reason       string
	minAvailable int64 // bytes; 0 chooses a quarter of the machine, at most heavyFloorCap
	wait         time.Duration
	noSlot       bool
	handoff      bool
	port         int
}

// heavyDeps is everything the command touches outside itself, so the test can
// stand each one in.
type heavyDeps struct {
	broker     *broker // nil: no daemon to ask
	sample     func() (machineusage.Sample, error)
	sleep      func(time.Duration)
	now        func() time.Time
	run        func(argv []string, env []string) (int, error)
	getenv     func(string) string
	stderr     io.Writer
	renew      time.Duration
	poll       time.Duration
	pid        int
	requestID  string
	executable string
	cwd        string
}

func heavyCommand(args []string) {
	fs := flag.NewFlagSet("heavy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "what this is, shown to whoever waits behind it")
	minAvail := fs.String("min-available", "", "memory that must be available first, e.g. 1500M or 1G (default: a quarter of the machine, at most 1G)")
	wait := fs.Duration("max-wait", heavyDefaultWait, "how long to wait for the slot and memory; past it the command is not run and heavy exits 75")
	noSlot := fs.Bool("no-slot", false, "check memory only; do not queue for the compile slot")
	handoff := fs.Bool("handoff", false, "hand a queued wait to a callback and exit 76")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		heavyUsage()
	}
	floor, err := parseBytes(*minAvail)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline heavy:", err)
		heavyUsage()
	}
	deps := heavyDeps{
		sample: machineusage.Read,
		sleep:  time.Sleep,
		now:    time.Now,
		run:    runForeground,
		getenv: os.Getenv,
		stderr: os.Stderr,
		renew:  heavyRenewEvery,
		poll:   heavyPollEvery,
		pid:    os.Getpid(),
	}
	deps.executable, _ = os.Executable()
	deps.cwd, _ = os.Getwd()
	if *port == 0 {
		// The callback does not inherit CLAWDLINE_NEXT_PORT, so pin the same
		// daemon port in the heavy command it runs later.
		*port, _ = daemonPort()
	}
	if os.Getenv(heavyEnv) == "" && (!*noSlot || *handoff) {
		if b, err := openBroker(*port); err == nil {
			deps.broker = b
		} else if !*noSlot {
			fmt.Fprintf(os.Stderr, "clawdline heavy: no compile slot (%v); running without one\n", err)
		}
	}
	os.Exit(runHeavy(heavyOptions{reason: *reason, minAvailable: floor, wait: *wait, noSlot: *noSlot, handoff: *handoff, port: *port}, fs.Args(), deps))
}

func heavyUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline heavy [--reason text] [--min-available 1G] [--max-wait 30m] [--no-slot] [--handoff] -- <command> [args…]")
	fmt.Fprintln(os.Stderr, "  waits for the machine's compile slot and for memory, then runs the command and gives the slot back")
	fmt.Fprintln(os.Stderr, "  exits with the command's own status; 75 when --max-wait passed; 76 when a callback took the queued command")
	os.Exit(2)
}

// runHeavy is the whole command after its flags: the answer is the exit status
// to leave with, which is the command's own whenever it ran, and
// heavyExitTimedOut when --max-wait passed first.
func runHeavy(opts heavyOptions, argv []string, d heavyDeps) int {
	say := func(format string, a ...any) { fmt.Fprintf(d.stderr, "clawdline heavy: "+format+"\n", a...) }
	if held := d.getenv(heavyEnv); held != "" {
		// The parent holds the slot and checked memory already.
		code, err := d.run(argv, []string{heavyEnv + "=" + held})
		return heavyExit(code, err, say)
	}
	if d.requestID == "" {
		d.requestID = newUUID()
	}
	deadline := d.now().Add(opts.wait)
	slot := &heavySlot{b: d.broker, id: d.requestID, say: say}
	attempted := false
	tryHandoff := func(place string) bool {
		if !opts.handoff || attempted {
			return false
		}
		attempted = true
		id, err := heavyHandoff(opts, argv, d)
		if err != nil {
			say("handoff unavailable (%v); waiting in place", err)
			return false
		}
		say("callback %s started (%s); End your turn; the notice arrives when it finishes (exit %d)", id, place, heavyExitHandedOff)
		return true
	}

	if slot.b != nil && !opts.noSlot {
		req := contract.LeaseRequest{
			RequestID: d.requestID, Resource: contract.LeaseResourceHeavyCompile,
			Holder: heavyHolder(argv), Reason: heavyReason(opts.reason, argv),
			SessionID: conversationOf(d.getenv), PID: int64(d.pid), Phase: "waiting",
		}
		if at := swiftstore.ProcessStart(d.pid); !at.IsZero() {
			req.ProcessStart = at.Unix()
		}
		if code := slot.acquire(req, deadline, opts.wait, d, tryHandoff); code != 0 {
			return code
		}
	}
	if code := waitForMemory(opts, deadline, d, slot, say, tryHandoff); code != 0 {
		slot.release()
		return code
	}

	stop := slot.keepAlive(d.renew)
	code, err := d.run(argv, []string{heavyEnv + "=" + d.requestID})
	stop()
	slot.release()
	return heavyExit(code, err, say)
}

// heavySlot is this run's side of the compile slot lease. Every failure to
// reach the broker turns it off rather than stopping the run.
type heavySlot struct {
	b    *broker
	id   string
	say  func(string, ...any)
	held bool
}

// acquire returns 0 when the command can proceed, 75 on timeout, or 76 after
// a callback accepted the command.
//
// While queued it asks again every poll (the broker passes over a waiter that
// stops asking), and says nothing: one line when it joins the line, one when
// the slot is its own.
func (s *heavySlot) acquire(req contract.LeaseRequest, deadline time.Time, wait time.Duration, d heavyDeps, tryHandoff func(string) bool) int {
	queued := false
	since := d.now()
	for {
		a, err := s.b.request("POST", "/v1/orchestrator/leases", nil, req, "")
		if err != nil {
			s.say("the daemon did not answer (%v); running without the compile slot", err)
			s.b = nil
			return 0
		}
		var reply contract.LeaseReply
		_ = json.Unmarshal(a.Body, &reply)
		switch {
		case a.ok() && reply.State == "granted":
			if queued {
				s.say("the compile slot is ours after %s; starting", d.now().Sub(since).Round(time.Second))
			}
			s.held = true
			return 0
		case a.ok() && reply.State == "queued":
			place := "queue position unknown"
			if reply.Position > 0 {
				place = fmt.Sprintf("number %d in line", reply.Position)
			}
			if tryHandoff(place) {
				s.cancel()
				return heavyExitHandedOff
			}
			if !queued {
				s.say("waiting for the compile slot (%s); quiet until it is ours, or %s passes and this exits %d without running",
					queueSentence(reply), wait, heavyExitTimedOut)
				queued = true
			}
		case a.Status == 429:
			if tryHandoff("the queue is full") {
				s.cancel()
				return heavyExitHandedOff
			}
			// queue_full: the line is long, not closed.
			if !queued {
				s.say("waiting for the compile slot (its queue is full); quiet until it is ours, or %s passes and this exits %d without running",
					wait, heavyExitTimedOut)
				queued = true
			}
		default:
			code, message := a.refusal()
			s.say("the compile slot was refused (%d %s: %s); running without it", a.Status, code, message)
			s.b = nil
			return 0
		}
		if !d.now().Before(deadline) {
			s.say("waited %s (--max-wait) for the compile slot without getting it; the command was not run (exit %d)",
				wait, heavyExitTimedOut)
			s.cancel()
			return heavyExitTimedOut
		}
		pause := d.poll
		if r := time.Duration(reply.RetryAfterSeconds) * time.Second; r > pause {
			pause = r
		}
		d.sleep(pause)
	}
}

// keepAlive renews the slot while the command runs. A lost lease is said and
// the command keeps running: the slot is a courtesy to the others, not a
// reason to throw away a build that is half done.
func (s *heavySlot) keepAlive(every time.Duration) func() {
	if s.b == nil || !s.held {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a, err := s.b.request("POST", "/v1/orchestrator/leases/renew", nil, s.owner("running"), "")
				if err == nil && !a.ok() {
					if code, _ := a.refusal(); code == "lease_lost" {
						s.say("the compile slot lapsed while running; the command goes on")
						s.held = false
						return
					}
				}
			}
		}
	}()
	return func() { cancel(); wg.Wait() }
}

func (s *heavySlot) release() {
	if s.b == nil || !s.held {
		return
	}
	if a, err := s.b.request("POST", "/v1/orchestrator/leases/release", nil, s.owner(""), ""); err != nil || !a.ok() {
		// Unreleased, it frees itself a minute after the renewals stop.
		s.say("the compile slot could not be given back; it frees itself within a minute")
	}
}

func (s *heavySlot) cancel() {
	if s.b == nil {
		return
	}
	_, _ = s.b.request("POST", "/v1/orchestrator/leases/cancel", nil, s.owner(""), "")
	s.b = nil
}

func (s *heavySlot) owner(phase string) contract.LeaseOwnerRequest {
	return contract.LeaseOwnerRequest{RequestID: s.id, Resource: contract.LeaseResourceHeavyCompile, Phase: phase}
}

func queueSentence(r contract.LeaseReply) string {
	who := ""
	if h := r.Lease.Holder; h != nil && h.Holder != "" {
		who = ", held by " + h.Holder
	}
	return fmt.Sprintf("number %d in line%s", r.Position, who)
}

// waitForMemory holds the start until the machine has room, renewing the slot
// while it waits. Its status is 0, 75, or 76 as for acquire.
func waitForMemory(opts heavyOptions, deadline time.Time, d heavyDeps, slot *heavySlot, say func(string, ...any), tryHandoff func(string) bool) int {
	waiting := false
	lastRenew := d.now()
	for {
		s, err := d.sample()
		if errors.Is(err, machineusage.ErrUnsupported) {
			return 0
		}
		if err != nil {
			say("memory could not be read (%v); not waiting for it", err)
			return 0
		}
		ok, why := memoryRoom(s, opts.minAvailable)
		if ok {
			if waiting {
				say("memory is back; starting")
			}
			return 0
		}
		now := d.now()
		if !now.Before(deadline) {
			say("waited %s (--max-wait) for memory (%s); the command was not run (exit %d)", opts.wait, why, heavyExitTimedOut)
			return heavyExitTimedOut
		}
		if !waiting {
			if tryHandoff("waiting for memory") {
				return heavyExitHandedOff
			}
			say("waiting for memory: %s; quiet until it is back, or --max-wait passes and this exits %d without running",
				why, heavyExitTimedOut)
			waiting = true
		}
		if slot.b != nil && slot.held && now.Sub(lastRenew) >= d.renew {
			_, _ = slot.b.request("POST", "/v1/orchestrator/leases/renew", nil, slot.owner("waiting for memory"), "")
			lastRenew = now
		}
		d.sleep(d.poll)
	}
}

// heavyHandoff starts the same heavy invocation without --handoff. The extra
// hour lets the daemon cover both the requested queue wait and a substantial
// compile run; callback itself permits at most four hours.
func heavyHandoff(opts heavyOptions, argv []string, d heavyDeps) (string, error) {
	if d.broker == nil {
		return "", errors.New("the daemon is unreachable")
	}
	if state := d.getenv("CLAWDLINE_NEXT_DIR"); state != "" && state != filepath.Join(d.getenv("HOME"), ".config", "clawdline-next") {
		return "", errors.New("the callback cannot inherit a custom Clawdline state directory")
	}
	if d.executable == "" || d.cwd == "" {
		return "", errors.New("the current executable or directory is unknown")
	}
	if d.getenv("CLAWDLINE_TASK_SECRET") != "" || heavyChildWorktree(d.cwd, d.getenv) {
		return "", errors.New("a Clawdline child must finish its own task")
	}
	if id, _, err := conversationFromEnv(d.getenv); err != nil || id == "" {
		return "", errors.New("no unambiguous conversation id in the environment")
	}
	timeout := opts.wait + time.Hour
	if timeout > 240*time.Minute {
		timeout = 240 * time.Minute
	}
	if timeout < time.Minute {
		timeout = time.Minute
	}
	// Callback timeout is sent in whole minutes, so round up rather than
	// silently cutting the final partial minute from the run allowance.
	timeout = (timeout + time.Minute - 1) / time.Minute * time.Minute
	cmd := []string{"--title", "Queued heavy command finished", "--timeout", timeout.String(), "--dir", d.cwd}
	if opts.port != 0 {
		cmd = append(cmd, "--port", strconv.Itoa(opts.port))
	}
	cmd = append(cmd, "--", d.executable, "heavy")
	if opts.reason != "" {
		cmd = append(cmd, "--reason", opts.reason)
	}
	if opts.minAvailable != 0 {
		cmd = append(cmd, "--min-available", strconv.FormatInt(opts.minAvailable, 10))
	}
	cmd = append(cmd, "--max-wait", opts.wait.String())
	if opts.noSlot {
		cmd = append(cmd, "--no-slot")
	}
	cmd = append(cmd, "--")
	cmd = append(cmd, argv...)
	inv, err := callbackArgs(cmd, d.cwd)
	if err != nil {
		return "", err
	}
	inv.json = true
	var out, errs bytes.Buffer
	if code := startCallback(&out, &errs, d.broker, inv, d.getenv); code != 0 {
		return "", fmt.Errorf("callback refused (exit %d): %s", code, strings.TrimSpace(errs.String()))
	}
	var result contract.BrokerDispatchResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Task.ID == "" {
		return "", errors.New("callback response did not name its task")
	}
	return result.Task.ID, nil
}

// A dispatched child lives in a task-ID worktree with a matching CHILD.md.
// The explicit secret handles children that run outside that checkout.
func heavyChildWorktree(cwd string, getenv func(string) string) bool {
	state := getenv("CLAWDLINE_NEXT_DIR")
	if state == "" {
		state = filepath.Join(getenv("HOME"), ".config", "clawdline-next")
	}
	worktrees := filepath.Join(state, "worktrees") + string(os.PathSeparator)
	if !strings.HasPrefix(cwd, worktrees) {
		return false
	}
	rel := strings.TrimPrefix(cwd, worktrees)
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) < 2 {
		return false
	}
	id := parts[1]
	if len(id) != 36 {
		return false
	}
	_, err := os.Stat(filepath.Join(state, "tasks", id, "CHILD.md"))
	return err == nil
}

// memoryRoom says whether a sample leaves room to start, and if not, why.
func memoryRoom(s machineusage.Sample, floor int64) (bool, string) {
	if floor <= 0 {
		floor = min(int64(heavyFloorCap), s.MemTotal/4)
	}
	if s.MemAvailable < floor {
		return false, fmt.Sprintf("%s available, %s wanted", mib(s.MemAvailable), mib(floor))
	}
	if p := s.Pressure; p != nil && p.MemorySome >= heavyPressureCeiling {
		return false, fmt.Sprintf("tasks waited on memory %.0f%% of the last 10 s", p.MemorySome)
	}
	return true, ""
}

func mib(n int64) string { return fmt.Sprintf("%d MB", n>>20) }

// heavyExit is the exit status to leave with: the command's own, 128 plus the
// signal for one that was killed, and 127 for one that could not start.
func heavyExit(code int, err error, say func(string, ...any)) int {
	if err == nil {
		return code
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		if c := exit.ExitCode(); c >= 0 {
			return c
		}
		return 1
	}
	say("%v", err)
	return 127
}

// runForeground runs the command with this terminal, and passes a TERM on to
// it. An interrupt typed at the terminal reaches it by itself (it is in this
// process group); this process only outlives it to give the slot back.
func runForeground(argv []string, env []string) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), env...)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	// The command inherits its priority from the thread that starts it: on
	// Linux a nice value is a thread's, so this goroutine keeps one thread,
	// lowers it, and starts the command from it. The thread is never handed
	// back; this process only waits from here on.
	runtime.LockOSThread()
	lowerPriority()
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-signals:
				if sig == syscall.SIGTERM {
					if cmd.Process.Signal(sig) != nil {
						_ = cmd.Process.Kill()
					}
				}
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func heavyHolder(argv []string) string {
	dir, _ := os.Getwd()
	label := filepath.Base(dir) + ": " + strings.Join(argv, " ")
	return clipBytes(label, 200)
}

func heavyReason(reason string, argv []string) string {
	if strings.TrimSpace(reason) == "" {
		reason = strings.Join(argv, " ")
	}
	return clipBytes(reason, 500)
}

// clipBytes cuts s to at most n bytes, on a character boundary and ending in
// an ellipsis when cut: the broker counts a label's bytes, and `clip`'s n
// characters plus its three-byte "…" came to 202 of 200 on a long command line
// (refused bad_lease on the first live run, 2026-09-26).
func clipBytes(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// newUUID is a random lowercase version 4 UUID: the lease's proof of
// ownership.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseBytes reads "1500M", "1G", "800MB" or plain bytes. Empty is zero.
func parseBytes(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "IB"), "B")
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil || f < 0 {
		return 0, fmt.Errorf("--min-available %q is not a size like 1500M or 1G", s)
	}
	return int64(f * float64(mult)), nil
}
