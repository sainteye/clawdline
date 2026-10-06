package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline callback --title "…" -- <command…>`: hand a wait to the daemon
// and end the turn.
//
// A root that deploys used to keep its turn open while the deploy ran and
// then poll for the result, every poll a turn that reread its whole context.
// This starts the command under the daemon (orchestrator/callback.go) and
// returns at once; when the command exits, the daemon types the same
// `<clawdline-notice>` a finished child sends, and `clawdline task show <id>`
// prints how it ended and closes the notice.
//
// The command is run as its words, with no shell; `sh -c '…'` is the way to
// ask for one. Its environment is this shell's PATH, HOME, locale, USER,
// SHELL, TMPDIR and TERM — never a credential: a command that needs one reads
// it from its own file.
//
// Exit status: 0 started (or already started under this --task-id); 1
// refused, or the daemon could not be reached; 2 a usage mistake.

type callbackInvocation struct {
	port         int
	title        string
	timeout      time.Duration
	workID       string
	dir          string
	taskID       string
	conversation string
	argv         []string
	json         bool
}

func callbackArgs(args []string, cwd string) (callbackInvocation, error) {
	fs := flag.NewFlagSet("callback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inv := callbackInvocation{}
	fs.IntVar(&inv.port, "port", 0, "the daemon's port")
	fs.StringVar(&inv.title, "title", "", "one line of at most 60 characters saying what will be true when it succeeds")
	fs.DurationVar(&inv.timeout, "timeout", 30*time.Minute, "stop the command after this long (1m to 4h)")
	fs.StringVar(&inv.workID, "work-id", "", "the Board item this wait serves")
	fs.StringVar(&inv.dir, "dir", "", "the directory the command runs in (default: this one)")
	fs.StringVar(&inv.taskID, "task-id", "", "a lowercase UUID to reuse when retrying an uncertain start")
	fs.StringVar(&inv.conversation, "conversation", "", "this assistant's conversation id (default: from the environment)")
	fs.BoolVar(&inv.json, "json", false, "print the daemon's answer")
	if err := fs.Parse(args); err != nil {
		return inv, err
	}
	inv.argv = fs.Args()
	if len(inv.argv) == 0 {
		return inv, errors.New(cliCopy("misc", "callback.command_required", "name the command after --: clawdline callback --title \"…\" -- <command> [args…]"))
	}
	if strings.TrimSpace(inv.title) == "" {
		return inv, errors.New(cliCopy("misc", "callback.title_required", "--title is required"))
	}
	if inv.timeout < time.Minute || inv.timeout > 240*time.Minute {
		return inv, errors.New(cliCopy("misc", "callback.timeout_invalid", "--timeout must be 1m to 4h"))
	}
	if inv.workID != "" && !orchestrator.IsTaskID(inv.workID) {
		return inv, fmt.Errorf(cliCopy("misc", "callback.work_id_invalid", "--work-id %q is not a lowercase UUID"), inv.workID)
	}
	if inv.taskID != "" && !orchestrator.IsTaskID(inv.taskID) {
		return inv, fmt.Errorf(cliCopy("misc", "callback.task_id_invalid", "--task-id %q is not a lowercase UUID"), inv.taskID)
	}
	if inv.dir == "" {
		inv.dir = cwd
	}
	if abs, err := filepath.Abs(inv.dir); err == nil {
		inv.dir = abs
	}
	return inv, nil
}

func callbackCommand(args []string) {
	cwd, _ := os.Getwd()
	inv, err := callbackArgs(args, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, cliCopy("misc", "callback.clawdline_callback.0e6befe9", "clawdline callback:"), err)
		fmt.Fprintln(os.Stderr, cliCopy("misc", "callback.usage_clawdline_callback_title_time.ed8f80b7", `usage: clawdline callback --title "…" [--timeout 30m] [--work-id <item>] [--dir D] [--task-id uuid] [--json] -- <command> [args…]`))
		os.Exit(2)
	}
	b, err := openBroker(inv.port)
	if err != nil {
		fail(err)
	}
	os.Exit(startCallback(os.Stdout, os.Stderr, b, inv, os.Getenv))
}

// startCallback is the command, answering its exit status.
func startCallback(stdout, stderr io.Writer, b *broker, inv callbackInvocation, getenv func(string) string) int {
	conversation, name := inv.conversation, ""
	if conversation == "" {
		var err error
		if conversation, name, err = conversationFromEnv(getenv); err != nil || conversation == "" {
			if err == nil {
				err = errors.New(cliCopy("misc", "callback.no_conversation", "no conversation id"))
			}
			fmt.Fprintf(stderr, cliCopy("misc", "callback.clawdline_callback_s_nothing_was_st.12e9f2cd", "clawdline callback: %s Nothing was started.\n"), conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	assistant := conversationAssistant[name]
	if assistant == "" {
		assistant = "claude"
		if getenv("CODEX_THREAD_ID") != "" {
			assistant = "codex"
		}
	}
	id := inv.taskID
	if id == "" {
		id = newCallbackID()
	}
	env := map[string]string{}
	for _, k := range orchestrator.CallbackEnvNames() {
		if v := getenv(k); v != "" {
			env[k] = v
		}
	}
	body := map[string]any{
		"task_id": id, "title": inv.title, "argv": inv.argv, "dir": inv.dir, "env": env,
		"timeout_minutes": int(inv.timeout / time.Minute),
		"root":            map[string]string{"session_id": conversation, "assistant": assistant},
	}
	if inv.workID != "" {
		body["work_id"] = inv.workID
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/callbacks", nil, body, "")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "callback.clawdline_callback.0e6befe9", "clawdline callback:"), err)
		fmt.Fprintf(stderr, cliCopy("misc", "callback.check_clawdline_task_show_s_before.17b564c8", "Check `clawdline task show %s` before starting it again with --task-id %s.\n"), id, id)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "callback", a)
	}
	if inv.json {
		stdout.Write(a.Body)
		fmt.Fprintln(stdout)
		return 0
	}
	var got contract.BrokerDispatchResult
	if json.Unmarshal(a.Body, &got) != nil || got.Task.ID == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "callback.clawdline_callback_the_daemon_answe.9136210b", "clawdline callback: the daemon answered without the task it started"))
		return 1
	}
	line := "callback " + got.Task.ID + " " + string(got.Task.State)
	if got.Replayed {
		line += cliCopy("misc", "callback.replayed", " (already started under this id; nothing was started again)")
	}
	fmt.Fprintln(stdout, line)
	for _, w := range got.Warnings {
		fmt.Fprintln(stdout, cliCopy("misc", "callback.warning.47578bd3", "warning:"), w.Code, w.Message)
	}
	if string(got.Task.State) == string(orchestrator.StateBriefed) {
		fmt.Fprintf(stdout, cliCopy("misc", "callback.end_turn", "End your turn now: a notice arrives when the command exits, and `clawdline task show %s` prints how it ended.\n"), got.Task.ID)
	} else {
		writeTaskView(stdout, got.Task)
	}
	return 0
}

func newCallbackID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
