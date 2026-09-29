package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
)

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
	fmt.Fprintln(os.Stderr, "usage: clawdline coordinator bind [--conversation id] [--port n]")
	os.Exit(2)
}

func bindCoordinator(stdout, stderr io.Writer, b *broker, conversation string, getenv func(string) string) int {
	conversation = strings.TrimSpace(conversation)
	if conversation == "" {
		for _, name := range conversationEnv {
			if v := strings.TrimSpace(getenv(name)); v != "" {
				conversation = v
				break
			}
		}
	}
	if conversation == "" {
		fmt.Fprintln(stderr, "clawdline coordinator bind: pass --conversation <this assistant's conversation id>; no role was changed")
		return 2
	}
	read, err := b.request(http.MethodGet, "/v1/orchestrator/coordinator", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline coordinator bind:", err)
		return 1
	}
	if !read.ok() {
		return report(stdout, stderr, "coordinator bind (inspect)", read)
	}
	var state contract.CoordinatorInspection
	if err := json.Unmarshal(read.Body, &state); err != nil {
		fmt.Fprintln(stderr, "clawdline coordinator bind: role inspection was unreadable; no role was changed")
		return 1
	}
	path := "/v1/orchestrator/coordinator/register"
	body := any(map[string]any{"session_id": conversation})
	if state.Coordinator.Configured && state.Coordinator.Session != nil &&
		state.Coordinator.Session.SessionID != conversation {
		if state.Coordinator.Status != contract.CoordinatorStatusOffline {
			fmt.Fprintf(stderr, "clawdline coordinator bind: old role is %s; only a proven offline holder may be replaced\n", state.Coordinator.Status)
			return 1
		}
		path = "/v1/orchestrator/coordinator/rebind"
		body = map[string]any{"expected_coordinator_id": state.Coordinator.ID,
			"expected_generation": state.Coordinator.Generation, "session_id": conversation}
	}
	answer, err := b.request(http.MethodPost, path, nil, body, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline coordinator bind:", err)
		return 1
	}
	return report(stdout, stderr, "coordinator bind", answer)
}
