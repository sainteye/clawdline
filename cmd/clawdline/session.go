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
)

// `clawdline session report`: the root's own delivery receipt for a turn that
// is finished — the check that reads "delivered, awaiting approval" on the
// session's row. It is two requests the guide used to spell out as curl:
// whoami, to turn this conversation into the terminal the daemon watches, and
// POST …/sessions/<terminal>/complete with the one sentence.
//
// The conversation is this assistant's own id, which both assistants export
// to the commands they run: Claude Code as CLAUDE_CODE_SESSION_ID (the
// transcript's uuid; measured with Claude Code on 2026-09-19) and Codex as
// CODEX_THREAD_ID. `--conversation` names it when neither is there, and
// `--terminal` skips the lookup when the caller already knows the row.

// conversationEnv are the variables that name this assistant's conversation,
// in the order they are asked.
var conversationEnv = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SESSION_ID"}

func sessionCommand(args []string) {
	if len(args) == 0 || args[0] != "report" {
		sessionUsage()
	}
	fs := flag.NewFlagSet("session report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	summary := fs.String("summary", "", "one concrete sentence, at most 500 characters")
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	terminal := fs.String("terminal", "", "the terminal id to report for, instead of asking whoami")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		sessionUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(reportSession(os.Stdout, os.Stderr, b, *summary, *conversation, *terminal, os.Getenv))
}

func sessionUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline session report --summary <sentence> [--conversation id | --terminal id] [--port n]")
	fmt.Fprintln(os.Stderr, "  records this session's finished turn: delivered, awaiting approval")
	os.Exit(2)
}

// reportSession is the command, answering its exit status.
func reportSession(stdout, stderr io.Writer, b *broker, summary, conversation, terminal string,
	getenv func(string) string) int {
	// Its length is the daemon's to judge (1–500 characters): one rule, in
	// the one place that enforces it.
	summary = strings.TrimSpace(summary)
	if summary == "" {
		fmt.Fprintln(stderr, "clawdline session report: --summary is required: one concrete sentence about what was delivered")
		return 2
	}
	if terminal == "" {
		if conversation == "" {
			for _, name := range conversationEnv {
				if v := strings.TrimSpace(getenv(name)); v != "" {
					conversation = v
					break
				}
			}
		}
		if conversation == "" {
			fmt.Fprintf(stderr, "clawdline session report: cannot tell which conversation this is: none of %s is set. "+
				"Pass --conversation <this assistant's conversation id>. Nothing was reported.\n",
				strings.Join(conversationEnv, ", "))
			return 2
		}
		who, err := b.request(http.MethodGet, "/v1/orchestrator/whoami",
			url.Values{"conversation_id": {conversation}}, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, "clawdline session report:", err)
			return 1
		}
		if !who.ok() {
			return report(stdout, stderr, "session report (whoami)", who)
		}
		var id struct {
			TerminalID string `json:"terminal_id"`
		}
		if json.Unmarshal(who.Body, &id) != nil || id.TerminalID == "" {
			fmt.Fprintln(stderr, "clawdline session report: whoami answered without a terminal id. Nothing was reported.")
			return 1
		}
		terminal = id.TerminalID
	}
	// One path segment: a tmux id such as %47 is otherwise read as an escape.
	path := "/v1/orchestrator/sessions/" + url.PathEscape(terminal) + "/complete"
	a, err := b.request(http.MethodPost, path, nil, map[string]string{"summary": summary}, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline session report:", err)
		return 1
	}
	return report(stdout, stderr, "session report", a)
}
