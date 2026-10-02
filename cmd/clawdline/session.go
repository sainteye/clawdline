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

	"github.com/sainteye/clawdline/internal/app/orchestrator"
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
// in the order they are asked. Read them through conversationFromEnv, which
// refuses when two assistants' variables disagree.
var conversationEnv = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SESSION_ID"}

func sessionCommand(args []string) {
	if len(args) > 0 && args[0] == "close" {
		sessionCloseCommand(args[1:])
		return
	}
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
	fmt.Fprintln(os.Stderr, "usage: clawdline session close [--dry-run] [--conversation id] [--terminal id] [--port n]")
	fmt.Fprintln(os.Stderr, "  audits what this session (or a Feature Root its Epic opened) still owes, and closes it only when nothing is")
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
	if conversation == "" {
		var err error
		// With --terminal the conversation only asks for the reminders, which
		// are left out rather than asked for the wrong one.
		if conversation, _, err = conversationFromEnv(getenv); err != nil && terminal == "" {
			fmt.Fprintf(stderr, "clawdline session report: %s Nothing was reported.\n", conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	if terminal == "" {
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
	code := report(stdout, stderr, "session report", a)
	if code == 0 {
		remindOpenTodos(stderr, a.Body)
		if conversation != "" {
			remindUnacknowledgedCompletions(stderr, b, conversation)
		}
	}
	return code
}

// remindUnacknowledgedCompletions says, after the receipt, which of this
// root's children finished without the root acknowledging it — the line it
// was typed may never have reached it (a question on screen, a notice that
// gave up). It asks the root's own session-todos, the list a turn boundary
// reads; the receipt stands whatever this finds, and a list that could not be
// read is said as unknown, never as none. With only --terminal there is no
// conversation to ask about, and nothing is said.
func remindUnacknowledgedCompletions(stderr io.Writer, b *broker, conversation string) {
	unknown := "Whether a child of this Session finished without being acknowledged could not be read."
	a, err := b.request(http.MethodGet, "/v1/work/v2/agent/session-todos/"+url.PathEscape(conversation), nil, nil, "")
	if err != nil || !a.ok() {
		fmt.Fprintln(stderr, unknown)
		return
	}
	var answer struct {
		Completions []struct {
			TaskID      string `json:"task_id"`
			Title       string `json:"title"`
			State       string `json:"state"`
			ResultPath  string `json:"result_path"`
			NoticeID    string `json:"notice_id"`
			NoticeState string `json:"notice_state"`
		} `json:"unacknowledged_completions"`
		Unknown bool `json:"unacknowledged_completions_unknown"`
	}
	if json.Unmarshal(a.Body, &answer) != nil || answer.Unknown {
		fmt.Fprintln(stderr, unknown)
		return
	}
	if len(answer.Completions) == 0 {
		return
	}
	phrase := "child tasks of this Session finished and are not acknowledged:"
	if len(answer.Completions) == 1 {
		phrase = "child task of this Session finished and is not acknowledged:"
	}
	fmt.Fprintln(stderr, len(answer.Completions), phrase)
	for _, c := range answer.Completions {
		fmt.Fprintf(stderr, "  %s  %s (%s; notice %s)  read %s\n", c.TaskID, c.Title, c.State, c.NoticeState, c.ResultPath)
		fmt.Fprintf(stderr, "    then: %s\n", orchestrator.ShowCommand(c.TaskID, c.NoticeID))
	}
}

// remindOpenTodos says, after the receipt, which to-dos the person sent to
// this Session are still not checked off. The receipt stands either way: a
// turn may end with to-dos open, and the exit status does not change.
func remindOpenTodos(stderr io.Writer, body []byte) {
	var answer struct {
		OpenTodos []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"open_todos"`
		Truncated bool `json:"open_todos_truncated"`
		Unknown   bool `json:"open_todos_unknown"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return
	}
	if answer.Unknown {
		fmt.Fprintln(stderr, "The to-dos sent to this Session could not be read, so whether any are still open is unknown.")
		return
	}
	if len(answer.OpenTodos) == 0 {
		return
	}
	count := fmt.Sprint(len(answer.OpenTodos))
	if answer.Truncated {
		count = "At least " + count
	}
	phrase := "to-dos were sent to this Session and are not checked off:"
	if len(answer.OpenTodos) == 1 && !answer.Truncated {
		phrase = "to-do was sent to this Session and is not checked off:"
	}
	fmt.Fprintln(stderr, count, phrase)
	for _, td := range answer.OpenTodos {
		fmt.Fprintf(stderr, "  %s  %s\n", td.ID, td.Text)
	}
	fmt.Fprintln(stderr, "Complete each finished one with: clawdline todo done <id>")
}

// conversationFromEnv is this assistant's conversation from the environment
// and the variable that named it. A process can carry another assistant's
// variable too — a Codex started from a tmux whose global environment holds a
// Claude Code session id — and the order of conversationEnv cannot say which
// is the one running, so two assistants naming different conversations is an
// error, never the first one asked. Empty, with no error, when none is set.
func conversationFromEnv(getenv func(string) string) (id, name string, err error) {
	var found []string
	seen := map[string]string{} // assistant -> its conversation
	for _, env := range conversationEnv {
		v := strings.TrimSpace(getenv(env))
		if v == "" {
			continue
		}
		assistant := conversationAssistant[env]
		if _, ok := seen[assistant]; ok {
			continue
		}
		seen[assistant] = v
		found = append(found, env+"="+v)
		if id == "" {
			id, name = v, env
		} else if v != id {
			err = twoConversationsError(found)
		}
	}
	if err != nil {
		return "", "", err
	}
	return id, name, nil
}

// twoConversationsError lists the variables, as NAME=value, of two assistants
// that name different conversations.
type twoConversationsError []string

func (e twoConversationsError) Error() string {
	return "cannot tell which assistant is running this: " + strings.Join(e, " and ") +
		" name different conversations, and one of them was inherited from another assistant"
}

// conversationRefusal is the sentence that says how to settle an ambiguous
// environment: the command's own flag, or unsetting the other assistant's
// variable.
func conversationRefusal(err error, flag string) string {
	return fmt.Sprintf("%v. Pass %s <the conversation id of the assistant running this command>, "+
		"or run it without the other assistant's variable (for example `env -u CLAUDE_CODE_SESSION_ID clawdline …` from Codex).",
		err, flag)
}

// `clawdline session close`: a finished Session closing itself, or an Epic
// owner closing a Feature Root it opened. It reads the Session's closeability
// first, prints every blocker with who moves it, and closes only a `safe`
// one, naming the version it read so a Session that changed meanwhile is not
// closed on an old answer. It never forces: the daemon refuses force from an
// agent, and this command has no way to ask for it.

func sessionCloseCommand(args []string) {
	fs := flag.NewFlagSet("session close", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", false, "audit only: print what the Session still owes, close nothing")
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	terminal := fs.String("terminal", "", "the terminal to close (default: this Session's own)")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		sessionUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(closeSession(os.Stdout, os.Stderr, b, *conversation, *terminal, *dryRun, os.Getenv))
}

// sessionCloseAudit is the daemon's closeability answer for an agent.
type sessionCloseAudit struct {
	TerminalID string `json:"terminal_id"`
	State      string `json:"state"`
	Version    string `json:"version"`
	Authority  string `json:"authority"`
	Reasons    []struct {
		Code        string `json:"code"`
		Kind        string `json:"kind"`
		SubjectKind string `json:"subject_kind"`
		SubjectID   string `json:"subject_id"`
		Mover       struct {
			Kind         string `json:"kind"`
			Self         bool   `json:"self"`
			PersonNeeded bool   `json:"person_needed"`
			SessionID    string `json:"session_id"`
		} `json:"mover"`
	} `json:"reasons"`
}

// closeSession is the command, answering its exit status.
func closeSession(stdout, stderr io.Writer, b *broker, conversation, terminal string, dryRun bool,
	getenv func(string) string) int {
	if conversation == "" {
		var err error
		if conversation, _, err = conversationFromEnv(getenv); err != nil {
			fmt.Fprintf(stderr, "clawdline session close: %s Nothing was closed.\n", conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	if conversation == "" {
		fmt.Fprintf(stderr, "clawdline session close: cannot tell which conversation this is: none of %s is set. "+
			"Pass --conversation <this assistant's conversation id>. Nothing was closed.\n",
			strings.Join(conversationEnv, ", "))
		return 2
	}
	if terminal == "" {
		who, err := b.request(http.MethodGet, "/v1/orchestrator/whoami",
			url.Values{"conversation_id": {conversation}}, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, "clawdline session close:", err)
			return 1
		}
		if !who.ok() {
			return report(stdout, stderr, "session close (whoami)", who)
		}
		var id struct {
			TerminalID string `json:"terminal_id"`
		}
		if json.Unmarshal(who.Body, &id) != nil || id.TerminalID == "" {
			fmt.Fprintln(stderr, "clawdline session close: whoami answered without a terminal id. Nothing was closed.")
			return 1
		}
		terminal = id.TerminalID
	}
	base := "/v1/work/v2/agent/sessions/" + url.PathEscape(terminal)
	a, err := b.request(http.MethodGet, base+"/closeability", url.Values{"session_id": {conversation}}, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline session close:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "session close (closeability)", a)
	}
	var audit sessionCloseAudit
	if json.Unmarshal(a.Body, &audit) != nil || audit.State == "" {
		fmt.Fprintln(stderr, "clawdline session close: the closeability answer could not be read. Nothing was closed.")
		return 1
	}
	printCloseAudit(stdout, audit)
	turn := audit.onlyThisTurn()
	if turn && dryRun {
		fmt.Fprintf(stdout, "%s: the only thing left is this turn; a real run would schedule the close for when "+
			"the turn ends (dry run: nothing was scheduled).\n", terminal)
		return 0
	}
	if audit.State != "safe" && !turn {
		fmt.Fprintf(stderr, "clawdline session close: %s is %s; nothing was closed.\n", terminal, audit.State)
		return 1
	}
	if dryRun {
		fmt.Fprintf(stdout, "%s is safe to close (dry run: nothing was closed).\n", terminal)
		return 0
	}
	c, err := b.request(http.MethodPost, base+"/close", nil, map[string]string{
		"session_id": conversation, "expected_closeability_version": audit.Version,
	}, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline session close:", err)
		return 1
	}
	if turn && c.Status == http.StatusAccepted {
		fmt.Fprintf(stdout, "%s: close scheduled. It is carried out when this turn ends, if nothing else is owed by "+
			"then; type nothing after this command. A daemon restart before then drops it: run it again.\n", terminal)
		return 0
	}
	return report(stdout, stderr, "session close", c)
}

// onlyThisTurn is the daemon's own test (agent_close_schedule.go): a Session
// closing itself whose one blocker is the turn it is running.
func (audit sessionCloseAudit) onlyThisTurn() bool {
	if audit.Authority != "self" || audit.State != "blocked" || len(audit.Reasons) == 0 {
		return false
	}
	for _, r := range audit.Reasons {
		if r.Code != "terminal_working" || r.SubjectID != audit.TerminalID {
			return false
		}
	}
	return true
}

// printCloseAudit names every blocker and who moves it.
func printCloseAudit(w io.Writer, audit sessionCloseAudit) {
	fmt.Fprintf(w, "%s: %s (closing as %s)\n", audit.TerminalID, audit.State, audit.Authority)
	for _, r := range audit.Reasons {
		mover := r.Mover.Kind
		switch {
		case r.Mover.Self:
			mover = "this session"
		case r.Mover.PersonNeeded:
			mover = "the person"
		case r.Mover.SessionID != "":
			mover = "session " + r.Mover.SessionID
		}
		subject := ""
		if r.SubjectID != "" {
			subject = " " + strings.TrimSpace(r.SubjectKind+" "+r.SubjectID)
		}
		fmt.Fprintf(w, "  %s (%s)%s — moved by %s\n", r.Code, r.Kind, subject, mover)
	}
}
