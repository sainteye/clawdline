package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Project's team charter workflow, for a terminal Session the person asked
// to change it (docs/bots.md, "Workflow"). The person never edits the graph
// by hand: they ask the lead Bot, or a Session that runs this command.

const charterWorkflowPath = "/v1/project-assistant-charters/workflow"

func charterCommand(args []string) {
	os.Exit(runCharter(args, os.Stdin, os.Stdout, os.Stderr, openMemoryDaemon))
}

func runCharter(args []string, stdin io.Reader, stdout, stderr io.Writer, open func() (memoryDaemon, error)) int {
	usage := func() int {
		fmt.Fprintln(stderr, cliCopy("misc", "charter.usage", "usage: clawdline charter workflow show [--project <dir|id>] [--json] | set --file <workflow.json|-> --reason <why> [--project <dir|id>] [--key <k>]"))
		fmt.Fprintln(stderr, cliCopy("misc", "charter.help", "Change the workflow only when the person asked for it. show --json prints the workflow JSON that set reads back."))
		return 2
	}
	if len(args) < 2 || args[0] != "workflow" || (args[1] != "show" && args[1] != "set") {
		return usage()
	}
	op := args[1]
	fs := flag.NewFlagSet("charter workflow "+op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "")
	asJSON := fs.Bool("json", false, "")
	file := fs.String("file", "", "")
	reason := fs.String("reason", "", "")
	key := fs.String("key", "", "")
	if fs.Parse(args[2:]) != nil || fs.NArg() != 0 {
		return usage()
	}
	if op == "show" && (*file != "" || *reason != "" || *key != "") || op == "set" && (*file == "" || strings.TrimSpace(*reason) == "" || *asJSON) {
		return usage()
	}
	var flow contract.ProjectAssistantCharterWorkflow
	if op == "set" {
		raw, err := readCharterFile(*file, stdin)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&flow); err != nil || flow.States == nil {
			fmt.Fprintln(stderr, cliCopy("misc", "charter.invalid_file", "The file must hold one workflow JSON object with states and transitions, as show --json prints it."))
			return 2
		}
		if flow.Transitions == nil {
			flow.Transitions = []contract.ProjectAssistantCharterWorkflowTransition{}
		}
	}
	d, err := open()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	id, err := charterProjectID(d, *project)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	a, err := d.request(http.MethodGet, charterWorkflowPath, url.Values{"project_id": {id}}, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !a.ok() {
		return charterRefusal(stderr, "charter workflow "+op, a)
	}
	var current contract.ProjectAssistantCharterResponse
	if err := json.Unmarshal(a.Body, &current); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "charter.unreadable", "The daemon's charter answer could not be read; nothing was changed."))
		return 1
	}
	if op == "show" {
		return showCharterWorkflow(stdout, current, *asJSON)
	}
	if *key == "" {
		*key = newKey("charter-workflow")
	}
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", *key)
	body := contract.ProjectAssistantCharterWorkflowSaveRequest{MachineID: current.MachineID, ProjectID: id, IdempotencyKey: *key,
		ExpectedVersion: current.Version, Workflow: flow, Reason: *reason}
	a, err = d.request(http.MethodPut, charterWorkflowPath, nil, body, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, cliCopy("misc", "charter.retry", "The result is uncertain. Retry with the same --key and file; do not create a new key."))
		return 1
	}
	if !a.ok() {
		return charterRefusal(stderr, "charter workflow set", a)
	}
	var saved contract.ProjectAssistantCharterResponse
	if err := json.Unmarshal(a.Body, &saved); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "charter.saved_unreadable", "The workflow was saved, but the daemon's answer could not be read."))
		return 1
	}
	fmt.Fprintf(stdout, cliCopy("misc", "charter.saved", "Saved the workflow as charter version %d. Every Bot of the Project hears it in its next prompt.\n"), saved.Version)
	return 0
}

func readCharterFile(path string, stdin io.Reader) ([]byte, error) {
	reader := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	}
	raw, err := io.ReadAll(io.LimitReader(reader, pa.MaxCharterBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > pa.MaxCharterBodyBytes {
		return nil, errors.New(cliCopy("misc", "charter.file_too_large", "The workflow file is larger than a charter write may be."))
	}
	return raw, nil
}

// charterProjectID is --project as a Project id: a directory names the
// Project registered at its Git top level; anything else is the id itself.
func charterProjectID(d memoryDaemon, given string) (string, error) {
	if given != "" {
		if st, err := os.Stat(given); err != nil || !st.IsDir() {
			return given, nil
		}
	}
	dir, err := memoryProjectDir(given)
	if err != nil {
		return "", err
	}
	return memoryPlaceFor(d, dir)
}

func showCharterWorkflow(stdout io.Writer, c contract.ProjectAssistantCharterResponse, asJSON bool) int {
	if asJSON {
		out, _ := json.MarshalIndent(c.Workflow, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	if c.Workflow == nil {
		fmt.Fprintf(stdout, cliCopy("misc", "charter.no_workflow", "Charter version %d of this Project has no workflow yet.\n"), c.Version)
		return 0
	}
	flow := pa.CharterWorkflow{}
	for _, s := range c.Workflow.States {
		flow.States = append(flow.States, pa.WorkflowState{ID: s.ID, Title: s.Title, BotID: s.BotID, Kind: pa.WorkflowStateKind(s.Kind), Note: s.Note})
	}
	for _, t := range c.Workflow.Transitions {
		flow.Transitions = append(flow.Transitions, pa.WorkflowTransition{From: t.From, To: t.To, Label: t.Label, Kind: pa.WorkflowTransitionKind(t.Kind)})
	}
	fmt.Fprintf(stdout, cliCopy("misc", "charter.version", "Charter version %d\n"), c.Version)
	fmt.Fprintln(stdout, pa.RenderCharterWorkflow(&flow, nil))
	return 0
}

// charterRefusal prints the daemon's refusal and, for a workflow rule,
// which state or transition broke it.
func charterRefusal(stderr io.Writer, name string, a answer) int {
	var envelope contract.ProjectAssistantErrorResponse
	if json.Unmarshal(a.Body, &envelope) != nil || envelope.Error.Code == "" {
		return report(io.Discard, stderr, name, a)
	}
	field := envelope.Error.Field
	fmt.Fprintf(stderr, cliCopy("misc", "charter.refused", "clawdline %s: refused, %d %s: %s\n"), name, a.Status, envelope.Error.Code, field)
	rule, subject, _ := strings.Cut(field, ":")
	if line := charterRuleLine(rule, subject); line != "" {
		fmt.Fprintln(stderr, line)
	}
	return 1
}

func charterRuleLine(rule, subject string) string {
	switch rule {
	case "workflow_start":
		if subject != "" {
			return fmt.Sprintf(cliCopy("misc", "charter.rule_second_start", "State %s is a second start; a workflow has exactly one."), subject)
		}
		return cliCopy("misc", "charter.rule_start", "The workflow needs exactly one state of kind start.")
	case "workflow_done":
		return cliCopy("misc", "charter.rule_done", "The workflow needs at least one state of kind done.")
	case "workflow_state_id":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_state_id", "State %s needs a unique id of lowercase letters, digits and hyphens."), subject)
	case "workflow_state_title":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_state_title", "State %s needs a one-line title."), subject)
	case "workflow_state_kind":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_state_kind", "State %s has a kind other than start, step or done."), subject)
	case "workflow_state_note":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_state_note", "State %s has a note with control characters."), subject)
	case "workflow_state_bot":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_state_bot", "State %s names a Bot that is not a visible Bot of this Project; use its id, or empty for any Bot."), subject)
	case "workflow_transition_from":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_transition_from", "Transition %s starts at a state that does not exist."), subject)
	case "workflow_transition_to":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_transition_to", "Transition %s leads to a state that does not exist."), subject)
	case "workflow_transition_label":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_transition_label", "Transition %s needs a one-line label, or none."), subject)
	case "workflow_transition_kind":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_transition_kind", "Transition %s has a kind other than normal, pass or return."), subject)
	case "workflow_done_exit":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_done_exit", "Transition %s leaves a done state; nothing may."), subject)
	case "workflow_unreachable":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_unreachable", "State %s cannot be reached from the start state."), subject)
	case "charter_workflow_states", "charter_workflow_transitions", "charter_workflow_title", "charter_workflow_note", "charter_workflow_id":
		return fmt.Sprintf(cliCopy("misc", "charter.rule_bound", "The workflow is past a bound (%s %s); see clawdline guide capacity."), rule, subject)
	case "charter_version":
		return cliCopy("misc", "charter.rule_stale", "The charter changed since it was read; run show again and retry.")
	}
	return ""
}
