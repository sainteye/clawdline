package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// A scan in progress may briefly make the old holder's liveness unknown.
// Every attempt inspects afresh; no unknown reading authorizes a takeover.
const coordinatorBindAttemptLimit = 6

// coordinator bind registers the caller's exact conversation, or rebinds an
// older role only after the machine proves that holder offline. The daemon
// checks workspace and liveness again at commit time.
func coordinatorCommand(args []string) {
	if len(args) == 0 || args[0] != "bind" {
		coordinatorUsage()
	}
	fs := flag.NewFlagSet("coordinator bind", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		coordinatorUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(bindCoordinator(os.Stdout, os.Stderr, b, *conversation, os.Getenv))
}

func coordinatorUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "coordinator.usage_clawdline_coordinator_bind_co.0531b99d", "usage: clawdline coordinator bind [--conversation id] [--port n]"))
	os.Exit(2)
}

func bindCoordinator(stdout, stderr io.Writer, b *broker, conversation string, getenv func(string) string) int {
	return bindCoordinatorWithWait(stdout, stderr, b, conversation, getenv, func() { time.Sleep(300 * time.Millisecond) })
}

func bindCoordinatorWithWait(stdout, stderr io.Writer, b *broker, conversation string, getenv func(string) string, wait func()) int {
	conversation = strings.TrimSpace(conversation)
	if conversation == "" {
		var err error
		if conversation, _, err = conversationFromEnv(getenv); err != nil {
			fmt.Fprintf(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind_s_no_rol.9f393468", "clawdline coordinator bind: %s No role was changed.\n"), conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	if conversation == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind_pass_con.cfa52d1a", "clawdline coordinator bind: pass --conversation <this assistant's conversation id>; no role was changed"))
		return 2
	}
	for attempt := 0; attempt < coordinatorBindAttemptLimit; attempt++ {
		if attempt > 0 {
			wait()
		}
		code, retry := inspectAndBindCoordinator(stdout, stderr, b, conversation, attempt+1 < coordinatorBindAttemptLimit)
		if !retry {
			return code
		}
	}
	return 1
}

func inspectAndBindCoordinator(stdout, stderr io.Writer, b *broker, conversation string, canRetry bool) (int, bool) {
	read, err := b.request(http.MethodGet, "/v1/orchestrator/coordinator", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind.261adc78", "clawdline coordinator bind:"), err)
		return 1, false
	}
	if !read.ok() {
		return report(stdout, stderr, "coordinator bind (inspect)", read), false
	}
	var state contract.CoordinatorInspection
	if err := json.Unmarshal(read.Body, &state); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind_role_ins.c8330b67", "clawdline coordinator bind: role inspection was unreadable; no role was changed"))
		return 1, false
	}
	path := "/v1/orchestrator/coordinator/register"
	body := any(map[string]any{"session_id": conversation})
	if state.Coordinator.Configured && state.Coordinator.Session != nil &&
		state.Coordinator.Session.SessionID != conversation {
		if state.Coordinator.Status != contract.CoordinatorStatusOffline {
			if state.Coordinator.Status == contract.CoordinatorStatusUnknown && canRetry {
				return 1, true
			}
			fmt.Fprintf(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind_old_role.d61e6b9a", "clawdline coordinator bind: old role is %s; only a proven offline holder may be replaced\n"), state.Coordinator.Status)
			return 1, false
		}
		path = "/v1/orchestrator/coordinator/rebind"
		body = map[string]any{"expected_coordinator_id": state.Coordinator.ID,
			"expected_generation": state.Coordinator.Generation, "session_id": conversation}
	}
	answer, err := b.request(http.MethodPost, path, nil, body, "")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "coordinator.clawdline_coordinator_bind.261adc78", "clawdline coordinator bind:"), err)
		return 1, false
	}
	if !answer.ok() && refusalCode(answer) == "coordinator_liveness_unknown" && canRetry {
		return 1, true
	}
	return report(stdout, stderr, "coordinator bind", answer), false
}
