package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline task cancel <task id> --reason "…"`: a root stopping a child it
// dispatched by mistake — a wrong brief, a wrong scope, a duplicate.
//
// The daemon settles the task `cancelled` with the reason as its verdict,
// closes the child's tab, and lets its claims and child slot go; a branch with
// commits on it is kept as a pending landing for the root to look at
// (orchestrator.CancelTask). Only the task's root Session, or the person from
// the console, may: the command names the calling Session by its squad
// capability when it has one, and by its conversation id otherwise.
//
// The Idempotency-Key is derived from the task and the caller, so running the
// same cancel again — after a lost answer, or by a root that forgot it had —
// is answered as the cancel that already succeeded (`replayed`), not refused.
//
// Exit status: 0 cancelled, or already cancelled by this caller; 1 refused —
// not this Session's task, already ended, a reason too long — or the daemon
// could not be reached; 2 a usage mistake.

type cancelInvocation struct {
	port         int
	id           string
	reason       string
	conversation string
	key          string
}

// cancelArgs reads the command line. Flags may come before or after the id.
func cancelArgs(args []string) (cancelInvocation, error) {
	fs := flag.NewFlagSet("task cancel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	reason := fs.String("reason", "", "why the task is cancelled")
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	key := fs.String("key", "", "the Idempotency-Key (default: derived from the task and the caller)")
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return cancelInvocation{}, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		words = append(words, args[0])
		args = args[1:]
	}
	if len(words) != 1 {
		return cancelInvocation{}, errors.New("exactly one task id is required")
	}
	id := strings.TrimSpace(words[0])
	if !orchestrator.IsTaskID(id) {
		return cancelInvocation{}, fmt.Errorf("%q is not a task id", id)
	}
	r := strings.Join(strings.Fields(*reason), " ")
	switch {
	case r == "":
		return cancelInvocation{}, errors.New(`--reason is required: say why, e.g. --reason "wrong brief"`)
	case len(r) > orchestrator.CancelReasonLimit:
		return cancelInvocation{}, fmt.Errorf("--reason is %d bytes; at most %d", len(r), orchestrator.CancelReasonLimit)
	}
	return cancelInvocation{port: *port, id: id, reason: r, conversation: strings.TrimSpace(*conversation),
		key: strings.TrimSpace(*key)}, nil
}

func cancelCommand(args []string) {
	inv, err := cancelArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task cancel:", err)
		taskUsage()
	}
	b, err := openBroker(inv.port)
	if err != nil {
		fail(err)
	}
	os.Exit(cancelTask(os.Stdout, os.Stderr, b, inv, os.Getenv))
}

// cancelKey is the derived Idempotency-Key: one per task and caller, so the
// same cancel sent twice is one cancel.
func cancelKey(id, caller string) string {
	sum := sha256.Sum256([]byte(id + "\x00" + caller))
	return "task-cancel-" + hex.EncodeToString(sum[:16])
}

// cancelTask is the command, answering its exit status.
func cancelTask(stdout, stderr io.Writer, b *broker, inv cancelInvocation, getenv func(string) string) int {
	conversation := inv.conversation
	if conversation == "" {
		var err error
		if conversation, _, err = conversationFromEnv(getenv); err != nil {
			fmt.Fprintf(stderr, "clawdline task cancel: %s Nothing was cancelled.\n", conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	body := map[string]string{"reason": inv.reason}
	if conversation != "" {
		body["session_id"] = conversation
	}
	key := inv.key
	if key == "" {
		key = cancelKey(inv.id, conversation)
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/tasks/"+url.PathEscape(inv.id)+"/cancel", nil, body, key)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task cancel:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "task cancel", a)
	}
	var got contract.BrokerCancelResult
	if json.Unmarshal(a.Body, &got) != nil || got.Task.ID == "" {
		fmt.Fprintln(stderr, "clawdline task cancel: the daemon answered without the task it cancelled")
		return 1
	}
	line := "cancelled " + got.Task.ID
	if got.Replayed {
		line += " (already cancelled by this request; nothing was done again)"
	}
	fmt.Fprintln(stdout, line)
	writeTaskView(stdout, got.Task)
	return 0
}
