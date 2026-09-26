package main

import (
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
// **It fails open, on purpose.** docs/machine-resource-scheduling.md records a
// lease that twice stopped a build from reaching its compiler — on the path
// taken when the broker was not answering, which is the path after a crash,
// exactly when somebody needs to rebuild. So a daemon that does not answer, a
// refusal this command does not understand, or a wait longer than --max-wait
// each run the command anyway and say so. Waiting is the point; refusing to
// build never is.
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
// before running anyway.
const heavyDefaultWait = 30 * time.Minute

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
}

// heavyDeps is everything the command touches outside itself, so the test can
// stand each one in.
type heavyDeps struct {
	broker    *broker // nil: no daemon to ask
	sample    func() (machineusage.Sample, error)
	sleep     func(time.Duration)
	now       func() time.Time
	run       func(argv []string, env []string) (int, error)
	getenv    func(string) string
	stderr    io.Writer
	renew     time.Duration
	poll      time.Duration
	pid       int
	requestID string
}

func heavyCommand(args []string) {
	fs := flag.NewFlagSet("heavy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "what this is, shown to whoever waits behind it")
	minAvail := fs.String("min-available", "", "memory that must be available first, e.g. 1500M or 1G (default: a quarter of the machine, at most 1G)")
	wait := fs.Duration("max-wait", heavyDefaultWait, "how long to wait for the slot and memory before running anyway")
	noSlot := fs.Bool("no-slot", false, "check memory only; do not queue for the compile slot")
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
	if os.Getenv(heavyEnv) == "" && !*noSlot {
		if b, err := openBroker(*port); err == nil {
			deps.broker = b
		} else {
			fmt.Fprintf(os.Stderr, "clawdline heavy: no compile slot (%v); running without one\n", err)
		}
	}
	os.Exit(runHeavy(heavyOptions{reason: *reason, minAvailable: floor, wait: *wait, noSlot: *noSlot}, fs.Args(), deps))
}

func heavyUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline heavy [--reason text] [--min-available 1G] [--max-wait 30m] [--no-slot] -- <command> [args…]")
	fmt.Fprintln(os.Stderr, "  waits for the machine's compile slot and for memory, then runs the command and gives the slot back")
	os.Exit(2)
}

// runHeavy is the whole command after its flags: the answer is the exit status
// to leave with, which is the command's own whenever it ran.
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

	if slot.b != nil {
		req := contract.LeaseRequest{
			RequestID: d.requestID, Resource: contract.LeaseResourceHeavyCompile,
			Holder: heavyHolder(argv), Reason: heavyReason(opts.reason, argv),
			SessionID: firstEnv(d.getenv, conversationEnv), PID: int64(d.pid), Phase: "waiting",
		}
		if at := swiftstore.ProcessStart(d.pid); !at.IsZero() {
			req.ProcessStart = at.Unix()
		}
		slot.acquire(req, deadline, d)
	}
	waitForMemory(opts, deadline, d, slot, say)

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

func (s *heavySlot) acquire(req contract.LeaseRequest, deadline time.Time, d heavyDeps) {
	lastPosition := int64(-1)
	for {
		a, err := s.b.request("POST", "/v1/orchestrator/leases", nil, req, "")
		if err != nil {
			s.say("the daemon did not answer (%v); running without the compile slot", err)
			s.b = nil
			return
		}
		var reply contract.LeaseReply
		_ = json.Unmarshal(a.Body, &reply)
		switch {
		case a.ok() && reply.State == "granted":
			if lastPosition >= 0 {
				s.say("the compile slot is ours")
			}
			s.held = true
			return
		case a.ok() && reply.State == "queued":
			if reply.Position != lastPosition {
				s.say("waiting for the compile slot: %s", queueSentence(reply))
				lastPosition = reply.Position
			}
		case a.Status == 429:
			// queue_full: the line is long, not closed.
			if lastPosition != 0 {
				s.say("the compile slot's queue is full; asking again")
				lastPosition = 0
			}
		default:
			code, message := a.refusal()
			s.say("the compile slot was refused (%d %s: %s); running without it", a.Status, code, message)
			s.b = nil
			return
		}
		if !d.now().Before(deadline) {
			s.say("waited past --max-wait for the compile slot; running anyway beside its holder")
			s.cancel()
			return
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
// while it waits so nobody else takes the memory it is waiting for.
func waitForMemory(opts heavyOptions, deadline time.Time, d heavyDeps, slot *heavySlot, say func(string, ...any)) {
	lastSaid := time.Time{}
	lastRenew := d.now()
	for {
		s, err := d.sample()
		if errors.Is(err, machineusage.ErrUnsupported) {
			return
		}
		if err != nil {
			say("memory could not be read (%v); not waiting for it", err)
			return
		}
		ok, why := memoryRoom(s, opts.minAvailable)
		if ok {
			if !lastSaid.IsZero() {
				say("memory is back; starting")
			}
			return
		}
		now := d.now()
		if !now.Before(deadline) {
			say("waited past --max-wait for memory (%s); running anyway", why)
			return
		}
		if lastSaid.IsZero() || now.Sub(lastSaid) >= 30*time.Second {
			say("waiting for memory: %s", why)
			lastSaid = now
		}
		if slot.b != nil && slot.held && now.Sub(lastRenew) >= d.renew {
			_, _ = slot.b.request("POST", "/v1/orchestrator/leases/renew", nil, slot.owner("waiting for memory"), "")
			lastRenew = now
		}
		d.sleep(d.poll)
	}
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

func firstEnv(getenv func(string) string, names []string) string {
	for _, name := range names {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
	}
	return ""
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
