package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline task wait <task id>... [--timeout d] [--any]`: a root waiting for
// its children in one command.
//
// A root that dispatched children used to learn they had finished only from
// the completion line the daemon typed into its composer, and then ran
// `task show` and `task ack` for each. This blocks until the tasks settle,
// prints each one as `task show` does, and closes each notice it printed
// (closeNotice), so the ordinary path is one command.
//
// Exit status: 0 every task settled and succeeded; 1 a settled task did not
// succeed and was not cancelled; 5 a settled task was cancelled (`clawdline
// task cancel`) and none failed otherwise; 3 the timeout came first (with
// --any: nothing settled; without: not everything did — what did settle is
// still printed and closed); 2 a usage mistake; 4 a task could not be read —
// no such task, the daemon did not answer, or its answer could not be read.
// Unknown is not success.
//
// When more than one applies the first in 4, 3, 1, 5 wins. A wait that could
// not read a task, or did not see them all settle, is not an answer about
// them; and a failure the root did not cause is news it must not miss behind
// a cancel it made itself, which is why 1 is over 5. 5 is its own code so a
// root's script can tell "I stopped it" from "it failed".

const (
	// waitIDLimit is how many tasks one wait follows. A root has a handful of
	// children at once; each costs one GET per poll.
	waitIDLimit = 32
	// waitTimeoutLimit is the longest --timeout accepted. The default is 9
	// minutes so the wait ends inside a 10-minute foreground tool call; a
	// longer one is for a terminal, and two hours is longer than any task's
	// own timeout.
	waitTimeoutLimit = 2 * time.Hour
	// waitPollLimit is the longest pause between polls: it starts at
	// waitFirstPoll and doubles up to this.
	waitPollLimit = 10 * time.Second
	// waitReadLimit is how many polls in a row may fail to reach the daemon
	// before the wait says it could not read the task.
	waitReadLimit = 3
	// waitRequestTimeout bounds each GET, shorter than brokerTimeout so a
	// stuck daemon costs a poll, not the whole wait.
	waitRequestTimeout = 15 * time.Second
)

const (
	waitDefaultTimeout = 9 * time.Minute
	waitFirstPoll      = time.Second
)

// waitOptions is one invocation, read from the command line.
type waitOptions struct {
	port    int
	ids     []string
	timeout time.Duration
	any     bool
}

// waitArgs reads the command line. Flags may come before, between or after
// the ids. Duplicate ids are followed once.
func waitArgs(args []string) (waitOptions, error) {
	fs := flag.NewFlagSet("task wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	timeout := fs.Duration("timeout", waitDefaultTimeout, "how long to wait at most")
	anyOne := fs.Bool("any", false, "return once one task has finished")
	var ids []string
	seen := map[string]bool{}
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return waitOptions{}, err
		}
		if fs.NArg() == 0 {
			break
		}
		id := strings.TrimSpace(fs.Arg(0))
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		rest = fs.Args()[1:]
	}
	switch {
	case len(ids) == 0:
		return waitOptions{}, fmt.Errorf("%s", cliCopy("misc", "task_wait.id_required", "at least one task id is required"))
	case len(ids) > waitIDLimit:
		return waitOptions{}, fmt.Errorf(cliCopy("misc", "task_wait.too_many_ids", "%d task ids; one wait follows at most %d"), len(ids), waitIDLimit)
	case *timeout <= 0:
		return waitOptions{}, fmt.Errorf("%s", cliCopy("misc", "task_wait.timeout_positive", "--timeout must be positive"))
	case *timeout > waitTimeoutLimit:
		return waitOptions{}, fmt.Errorf(cliCopy("misc", "task_wait.timeout_too_long", "--timeout %s is longer than %s"), *timeout, waitTimeoutLimit)
	}
	return waitOptions{port: *port, ids: ids, timeout: *timeout, any: *anyOne}, nil
}

func waitCommand(args []string) {
	opts, err := waitArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, cliCopy("misc", "task_wait.clawdline_task_wait.758f95fb", "clawdline task wait:"), err)
		taskUsage()
	}
	b, err := openBroker(opts.port)
	if err != nil {
		fail(err)
	}
	b.client = &http.Client{Timeout: waitRequestTimeout}
	os.Exit(waitTasks(os.Stdout, os.Stderr, b, opts, waitClock{now: time.Now, sleep: time.Sleep}))
}

// waitClock is the wall clock, injectable so a test does not wait.
type waitClock struct {
	now   func() time.Time
	sleep func(time.Duration)
}

// waitTasks is the command, answering its exit status.
func waitTasks(stdout, stderr io.Writer, b *broker, opts waitOptions, clock waitClock) int {
	deadline := clock.now().Add(opts.timeout)
	settled := map[string]contract.BrokerTask{}
	last := map[string]contract.BrokerTask{}
	misses := map[string]int{}
	failed, cancelled := false, false
	pause := waitFirstPoll
	for {
		for _, id := range opts.ids {
			if _, done := settled[id]; done {
				continue
			}
			t, transient, err := readWaitedTask(b, id)
			if err != nil {
				misses[id]++
				if !transient || misses[id] >= waitReadLimit {
					fmt.Fprintf(stderr, cliCopy("misc", "task_wait.clawdline_task_wait_could_not_read.15268251", "clawdline task wait: could not read task %s: %v\n"), id, err)
					return 4
				}
				continue
			}
			misses[id] = 0
			last[id] = t
			if !orchestrator.State(t.State).Terminal() {
				continue
			}
			settled[id] = t
			if len(settled) > 1 {
				fmt.Fprintln(stdout)
			}
			writeTaskView(stdout, t)
			closeNotice(stdout, stderr, b, "task wait", t, true)
			switch t.State {
			case contract.TaskStateSuccess:
			case contract.TaskStateCancelled:
				cancelled = true
			default:
				failed = true
			}
		}
		if len(settled) == len(opts.ids) || (opts.any && len(settled) > 0) {
			break
		}
		left := deadline.Sub(clock.now())
		if left <= 0 {
			break
		}
		clock.sleep(min(pause, left))
		pause = min(2*pause, waitPollLimit)
	}

	running := 0
	for _, id := range opts.ids {
		if _, done := settled[id]; done {
			continue
		}
		if running == 0 && len(settled) > 0 {
			fmt.Fprintln(stdout)
		}
		running++
		if t, ok := last[id]; ok {
			fmt.Fprintf(stdout, cliCopy("misc", "task_wait.still_running_s_s_s.2eef2e2a", "still running: %s  %s (%s)\n"), id, t.Title, t.State)
		} else {
			fmt.Fprintf(stdout, cliCopy("misc", "task_wait.still_running_s_not_read_yet.861d2e8e", "still running: %s (not read yet)\n"), id)
		}
	}
	switch {
	case len(settled) == 0 || (!opts.any && running > 0):
		fmt.Fprintf(stderr, cliCopy("misc", "task_wait.clawdline_task_wait_timed_out_after.89c87cdd", "clawdline task wait: timed out after %s with %d of %d task(s) still running\n"),
			opts.timeout, running, len(opts.ids))
		return 3
	case failed:
		return 1
	case cancelled:
		return 5
	}
	return 0
}

// readWaitedTask reads one task. transient says the daemon could not be
// reached, which the next poll may cure; anything else — no such task, a
// refusal, an answer that does not read — will not.
func readWaitedTask(b *broker, id string) (contract.BrokerTask, bool, error) {
	a, err := b.request(http.MethodGet, "/v1/orchestrator/tasks/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		return contract.BrokerTask{}, true, err
	}
	if !a.ok() {
		code, message := a.refusal()
		if code == "" {
			return contract.BrokerTask{}, false, fmt.Errorf(cliCopy("misc", "task_wait.daemon_answered", "the daemon answered %d: %s"), a.Status,
				strings.TrimSpace(string(a.Body)))
		}
		return contract.BrokerTask{}, false, fmt.Errorf(cliCopy("misc", "task_wait.refused", "refused, %d %s: %s"), a.Status, code, message)
	}
	var got contract.BrokerTaskEnvelope
	if err := json.Unmarshal(a.Body, &got); err != nil {
		return contract.BrokerTask{}, false, fmt.Errorf(cliCopy("misc", "task_wait.answer_unreadable", "the daemon's answer could not be read: %w"), err)
	}
	if got.Task.State == contract.TaskStateUnreadable {
		return contract.BrokerTask{}, false, fmt.Errorf("%s", cliCopy("misc", "task_wait.record_unreadable", "the daemon could not read the task's record (state unreadable)"))
	}
	return got.Task, false, nil
}
