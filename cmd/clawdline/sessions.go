package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline sessions`: GET /v1/orchestrator/sessions, read-only. The
// Sessions a send, a wait or a handoff can name, with their state and the
// task each was opened for. An answer that cannot be read is an error, never
// an empty table: no rows would read as "no Session is open".
func sessionsCommand(args []string) {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, cliCopy("workflow", "sessions.the_daemon_s_port_default_clawdline_next_port", "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)"))
	asJSON := fs.Bool("json", false, cliCopy("workflow", "sessions.print_the_daemon_s_answer_as_it_is", "print the daemon's answer as it is"))
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, cliCopy("workflow", "sessions.usage_clawdline_sessions_json_port_n", "usage: clawdline sessions [--json] [--port n]"))
		os.Exit(2)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(sessionsRun(os.Stdout, os.Stderr, b, *asJSON))
}

func sessionsRun(stdout, stderr io.Writer, b *broker, asJSON bool) int {
	const name = "sessions"
	a, err := b.request(http.MethodGet, "/v1/orchestrator/sessions", nil, nil, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, name, a)
	}
	var book struct {
		Sessions *[]contract.BrokerAddressRow `json:"sessions"`
	}
	if json.Unmarshal(a.Body, &book) != nil || book.Sessions == nil {
		fmt.Fprintf(stderr, cliCopy("workflow", "sessions.clawdline_s_the_daemon_s_answer_is_not", "clawdline %s: the daemon's answer is not a session list; the sessions are unknown: %s\n"),
			name, clip(string(a.Body), 200))
		return 1
	}
	if asJSON {
		return report(stdout, stderr, name, a)
	}
	if len(*book.Sessions) == 0 {
		fmt.Fprintln(stdout, cliCopy("workflow", "sessions.no_session_is_open", "No Session is open."))
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, cliCopy("workflow", "sessions.terminal_assistant_state_work_task_label_cwd", "TERMINAL\tASSISTANT\tSTATE\tWORK\tTASK\tLABEL\tCWD"))
	for _, s := range *book.Sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, dash(string(s.Assistant)), dash(string(s.State)),
			dash(string(s.WorkState)), dash(s.TaskID), oneLine(s.Label), dash(s.CWD))
	}
	_ = tw.Flush()
	return 0
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
