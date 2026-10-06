package main

import (
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
)

// handoffTimeout is how long `clawdline handoff` waits: the daemon opens the
// receiver and waits for its composer for up to four minutes before it
// answers.
const handoffTimeout = 5 * time.Minute

// handoffOptions are the flags of `clawdline handoff`.
type handoffOptions struct {
	summary, title, assistant, model, conversation string
	check                                          bool
}

func handoffCommand(args []string) {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o handoffOptions
	fs.StringVar(&o.summary, "summary", "", cliCopy("workflow", "handoff.the_milestone_summary_to_hand_over_markdown", "the milestone summary to hand over (Markdown)"))
	fs.StringVar(&o.title, "title", "", cliCopy("workflow", "handoff.the_receiver_s_tab_title_at_most_200", "the receiver's tab title, at most 200 characters"))
	fs.StringVar(&o.assistant, "assistant", "", cliCopy("workflow", "handoff.claude_or_codex_default_this_assistant", "claude or codex (default: this assistant)"))
	fs.StringVar(&o.model, "model", "", cliCopy("workflow", "handoff.the_receiver_s_model", "the receiver's model"))
	fs.StringVar(&o.conversation, "conversation", "", cliCopy("workflow", "handoff.this_assistant_s_conversation_id_default_from_the", "this assistant's conversation id (default: from the environment)"))
	fs.BoolVar(&o.check, "check", false, cliCopy("workflow", "handoff.check_the_summary_and_print_its_problems_open", "check the summary and print its problems; open nothing"))
	port := fs.Int("port", 0, cliCopy("workflow", "handoff.the_daemon_s_port_default_clawdline_next_port", "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)"))
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || o.summary == "" {
		handoffUsage()
	}
	if o.check {
		os.Exit(checkMilestoneSummary(os.Stdout, os.Stderr, o.summary))
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	b.client.Timeout = handoffTimeout
	os.Exit(openMilestoneHandoff(os.Stdout, os.Stderr, b, o, os.Getenv, gitToplevel))
}

func handoffUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("workflow", "handoff.usage_clawdline_handoff_summary_file_title_t_assistant", "usage: clawdline handoff --summary <file> [--title t] [--assistant claude|codex] [--model m] [--conversation id] [--port n]"))
	fmt.Fprintln(os.Stderr, cliCopy("workflow", "handoff.clawdline_handoff_summary_file_check", "       clawdline handoff --summary <file> --check"))
	fmt.Fprintln(os.Stderr, cliCopy("workflow", "handoff.hands_this_session_s_line_of_work_to", "  hands this Session's line of work to a fresh Session at a milestone; the summary has the sections"))
	fmt.Fprintln(os.Stderr, "  "+strings.Join(orchestrator.MilestoneSections, ", ")+fmt.Sprintf(cliCopy("workflow", "handoff.at_most_d_bytes", ", at most %d bytes"), orchestrator.MilestoneSummaryLimit))
	os.Exit(2)
}

// checkMilestoneSummary prints every problem the daemon would refuse the
// summary for. 0 when there are none, 1 when there are, 2 when it cannot be
// read.
func checkMilestoneSummary(stdout, stderr io.Writer, path string) int {
	data, err := readSummary(path)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 2
	}
	problems := orchestrator.CheckMilestoneSummary(data)
	if len(problems) == 0 {
		fmt.Fprintf(stdout, cliCopy("workflow", "handoff.s_is_a_milestone_summary_d_of_d", "%s is a milestone summary (%d of %d bytes).\n"), path, len(data), orchestrator.MilestoneSummaryLimit)
		return 0
	}
	for _, p := range problems {
		if p.Line > 0 {
			fmt.Fprintf(stdout, "%s:%d: %s: %s\n", path, p.Line, p.Rule, p.Detail)
		} else {
			fmt.Fprintf(stdout, "%s: %s: %s\n", path, p.Rule, p.Detail)
		}
	}
	return 1
}

func readSummary(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// One byte past the limit is enough to say it is over.
	return io.ReadAll(io.LimitReader(f, orchestrator.MilestoneSummaryLimit+1))
}

// openMilestoneHandoff checks the summary, writes it as the package's
// handoff.md under the daemon's package root, and opens the handoff.
func openMilestoneHandoff(stdout, stderr io.Writer, b *broker, o handoffOptions,
	getenv func(string) string, toplevel func() (string, error)) int {
	data, err := readSummary(o.summary)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 2
	}
	if problems := orchestrator.CheckMilestoneSummary(data); len(problems) > 0 {
		fmt.Fprintf(stderr, cliCopy("workflow", "handoff.clawdline_handoff_s_is_not_a_milestone_summary", "clawdline handoff: %s is not a milestone summary; `clawdline handoff --summary %s --check` lists the %d problems.\n"),
			o.summary, o.summary, len(problems))
		return 1
	}
	from, envName := o.conversation, ""
	if from == "" {
		from, envName, err = conversationFromEnv(getenv)
		if err != nil {
			fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
			return 2
		}
	}
	if from == "" {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff_no_conversation_id_pass_conversation", "clawdline handoff: no conversation id; pass --conversation"))
		return 2
	}
	assistant := o.assistant
	if assistant == "" {
		assistant = conversationAssistant[envName]
	}
	project, err := toplevel()
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff_the_project_is_this_directory_s", "clawdline handoff: the project is this directory's git top-level:"), err)
		return 2
	}

	list, err := b.request(http.MethodGet, "/v1/orchestrator/handoffs", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 1
	}
	if !list.ok() {
		return report(stdout, stderr, "handoff", list)
	}
	var root struct {
		PackageRoot string `json:"package_root"`
	}
	if json.Unmarshal(list.Body, &root) != nil || !filepath.IsAbs(root.PackageRoot) {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff_the_daemon_did_not_say_where", "clawdline handoff: the daemon did not say where handoff packages go"))
		return 1
	}
	id := orchestrator.NewUUID()
	dir := filepath.Join(root.PackageRoot, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.md"), data, 0o600); err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 1
	}
	// Said before the wait, so a timeout still leaves the id to read.
	fmt.Fprintf(stderr, cliCopy("workflow", "handoff.clawdline_handoff_opening_s_get_v1_orchestrator_handoffs", "clawdline handoff: opening %s (GET /v1/orchestrator/handoffs/%s reads it)\n"), id, id)
	body := map[string]any{"handoff_id": id, "from_session": from, "coordinator_plain_handoff": true,
		"milestone": true, "project_dir": project}
	for k, v := range map[string]string{"assistant": assistant, "model": o.model, "title": o.title} {
		if v != "" {
			body[k] = v
		}
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/handoffs", nil, body, "")
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) || strings.Contains(err.Error(), "Timeout") {
			fmt.Fprintf(stderr, cliCopy("workflow", "handoff.clawdline_handoff_no_answer_yet_the_handoff_may", "clawdline handoff: no answer yet; the handoff may still open. Read it with GET /v1/orchestrator/handoffs/%s\n"), id)
		}
		fmt.Fprintln(stderr, cliCopy("workflow", "handoff.clawdline_handoff", "clawdline handoff:"), err)
		return 1
	}
	return report(stdout, stderr, "handoff", a)
}
