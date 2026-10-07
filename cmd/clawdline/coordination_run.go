package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/contract"
)

// coordination run is the operation-scoped, fail-closed counterpart to heavy.
// A merge or restart must never silently proceed without its exclusive lease.
func coordinationRunCommand(args []string) {
	fs := flag.NewFlagSet("coordination run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	resource := fs.String("resource", "", "landing or daemon_restart")
	checkout := fs.String("checkout", "", "absolute landing checkout")
	reason := fs.String("reason", "", "visible reason")
	maxWait := fs.Duration("max-wait", 30*time.Minute, "maximum queue wait")
	handoff := fs.Bool("handoff", true, "hand the wait to a callback")
	port := fs.Int("port", 0, "daemon port")
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 || *maxWait <= 0 ||
		(*resource != "landing" && *resource != "daemon_restart") ||
		(*resource == "landing" && !filepath.IsAbs(*checkout)) ||
		(*resource == "daemon_restart" && *checkout != "") {
		coordinationRunUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opts := coordinationRunOptions{resource: *resource, checkout: *checkout, reason: *reason,
		maxWait: *maxWait, handoff: *handoff, port: *port}
	os.Exit(runCoordinatedOperation(os.Stdout, os.Stderr, b, opts, fs.Args(), time.Now, time.Sleep, os.Getenv, runForeground))
}

func coordinationRunUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "coordination.run_usage", "usage: clawdline coordination run --resource landing --checkout /repo [--handoff] -- <command> [args…]; or --resource daemon_restart"))
	os.Exit(2)
}

type coordinationRunOptions struct {
	resource, checkout, reason string
	maxWait                    time.Duration
	handoff                    bool
	port                       int
}

func runCoordinatedOperation(stdout, stderr io.Writer, b *broker, opts coordinationRunOptions, argv []string,
	now func() time.Time, sleep func(time.Duration), getenv func(string) string,
	run func([]string, []string) (int, error)) int {
	id := newUUID()
	conversation, _, err := conversationFromEnv(getenv)
	if err != nil || conversation == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "coordination.identity", "This command needs the caller's conversation ID from its Session environment."))
		return 2
	}
	req := contract.LeaseRequest{RequestID: id, Resource: contract.LeaseResource(opts.resource),
		Checkout: opts.checkout, Holder: conversation, Reason: opts.reason,
		SessionID: conversation, PID: int64(os.Getpid()), Phase: "waiting"}
	if start := swiftstore.ProcessStart(os.Getpid()); !start.IsZero() {
		req.ProcessStart = start.Unix()
	}
	owner := contract.LeaseOwnerRequest{RequestID: id, Resource: req.Resource, Checkout: opts.checkout}
	deadline := now().Add(opts.maxWait)
	queued := false
	for {
		a, err := b.request(http.MethodPost, "/v1/orchestrator/leases", nil, req, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !a.ok() {
			return report(stdout, stderr, "coordination run", a)
		}
		var reply contract.LeaseReply
		if json.Unmarshal(a.Body, &reply) != nil {
			fmt.Fprintln(stderr, cliCopy("misc", "coordination.unreadable", "Coordination status was unreadable."))
			return 1
		}
		if reply.State == "granted" {
			break
		}
		if reply.State != "queued" {
			fmt.Fprintln(stderr, cliCopy("misc", "coordination.unreadable", "Coordination status was unreadable."))
			return 1
		}
		if !queued {
			fmt.Fprintf(stderr, cliCopy("misc", "coordination.waiting", "Waiting for %s, position %d.\n"), opts.resource, reply.Position)
			queued = true
		}
		if opts.handoff {
			if startCoordinatedCallback(stdout, stderr, b, opts, argv) {
				_, _ = b.request(http.MethodPost, "/v1/orchestrator/leases/cancel", nil, owner, "")
				return heavyExitHandedOff
			}
			opts.handoff = false
		}
		if !now().Before(deadline) {
			_, _ = b.request(http.MethodPost, "/v1/orchestrator/leases/cancel", nil, owner, "")
			fmt.Fprintln(stderr, cliCopy("misc", "coordination.timed_out", "The lease wait ended before a grant; the command did not run."))
			return heavyExitTimedOut
		}
		sleep(5 * time.Second)
	}
	// The wrapper remains alive throughout the command. If the daemon restarts,
	// its persisted lease and this exact process prove ownership until release.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				owner.Phase = "running"
				_, _ = b.request(http.MethodPost, "/v1/orchestrator/leases/renew", nil, owner, "")
			}
		}
	}()
	code, runErr := run(argv, nil)
	cancel()
	wg.Wait()
	owner.Phase = ""
	if a, err := b.request(http.MethodPost, "/v1/orchestrator/leases/release", nil, owner, ""); err != nil || !a.ok() {
		fmt.Fprintln(stderr, cliCopy("misc", "coordination.release_failed", "The lease release could not be confirmed; inspect the holder before another exclusive operation."))
	}
	if runErr != nil {
		fmt.Fprintln(stderr, runErr)
		return 1
	}
	return code
}

func startCoordinatedCallback(stdout, stderr io.Writer, b *broker, opts coordinationRunOptions, argv []string) bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	cmd := []string{"--title", "Queued exclusive operation finished", "--timeout", strconv.Itoa(int((opts.maxWait+time.Hour)/time.Minute)) + "m", "--dir", cwd}
	if opts.port != 0 {
		cmd = append(cmd, "--port", strconv.Itoa(opts.port))
	}
	cmd = append(cmd, "--", executable, "coordination", "run", "--resource", opts.resource)
	if opts.checkout != "" {
		cmd = append(cmd, "--checkout", opts.checkout)
	}
	if opts.reason != "" {
		cmd = append(cmd, "--reason", opts.reason)
	}
	cmd = append(cmd, "--max-wait", opts.maxWait.String(), "--handoff=false", "--")
	cmd = append(cmd, argv...)
	inv, err := callbackArgs(cmd, cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return false
	}
	if startCallback(stdout, stderr, b, inv, os.Getenv) != 0 {
		return false
	}
	return true
}
