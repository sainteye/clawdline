package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// `clawdline item`: a Board item a Session creates because the person told it
// to, in a message sent through Clawdline (docs/work-system-v2.md §2, amended
// 2026-09-25). `add` creates it under that message's run — read from this
// conversation's latest run unless --run names one — and it arrives
// unassigned with its steps, or assigned to this Session with --assign-self
// when that message told it to do the work now. `claim` takes an existing,
// unassigned item
// for this Session under that message's run, when the message told it to.
// `acceptance` lets the owning Session fill an empty acceptance contract.
// `steps` lists an item's steps with their
// ids, `step-add` breaks an item this Session owns into further steps,
// `step-done` completes one after it is verified, `doc` adds a document to
// an item this Session owns — an Epic's plan and the review of it are two —
// and `phase` moves an item this Session owns to its next execution phase
// with that phase's evidence (docs/work-system-v2.md §6). `child` is the
// Epic owner's exception to "only on the person's word": the Session the
// person assigned an Epic to breaks it, after its reviewed plan, into Feature
// and Issue items and may assign each to a Session; `assign` hands such a
// child to a Session afterwards (docs/work-system-v2.md §6.5), and hands any
// unassigned Feature or Issue to a new Session, with a persona, when the
// person's message asks for it — under that message's run, as `claim`.
//
// A person's TODO list said together with a Board item is that item's steps:
// pass them with --step. They are not this Session's own to-dos, which
// `clawdline todo` writes.

// stringList is a repeatable flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ", ") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// itemFlags is what `item add` was told.
type itemFlags struct {
	project, kind, title, description, acceptance, deploy, run string
	expectedVersion                                            int64
	docID                                                      string
	steps                                                      []string
	phase                                                      phaseEvidence
	doc                                                        docFlags
	assign                                                     assignFlags
}

// assignFlags is who `item add`, `item child` or `item assign` hands the item
// to: an existing Session by terminal id, a new Session Clawdline opens, or —
// for `item add` only — the Session running the command (self).
type assignFlags struct {
	terminal, assistant, model, persona string
	open, self                          bool
}

// choices is how many Sessions were named; more than one is refused.
func (a assignFlags) choices() int {
	n := 0
	for _, named := range []bool{a.terminal != "", a.open, a.self} {
		if named {
			n++
		}
	}
	return n
}

// personaRefusal is why --persona cannot go with this choice, or "": a
// persona is chosen when a new Session opens, so it never goes with an
// existing terminal, and a name this build's catalog lacks is refused before
// anything is asked, as `dispatch --persona` does.
func (a assignFlags) personaRefusal(terminalFlag, newFlag string) string {
	switch {
	case a.persona == "":
		return ""
	case a.terminal != "":
		return "--persona goes with a new Session, not with " + terminalFlag + "; an existing Session keeps the persona it was opened with."
	case !a.open:
		return "--persona goes with " + newFlag + "; it names what the new Session opens as."
	}
	_, builtin := persona.Known(a.persona)
	if !builtin && !squad.ValidCustomID(a.persona) {
		return fmt.Sprintf("--persona %q is not a persona this build has; it has %s.", a.persona, strings.Join(persona.IDs(), ", "))
	}
	return ""
}

// request is the daemon's assign object, or nil when no Session was named.
func (a assignFlags) request() map[string]any {
	switch {
	case a.self:
		return map[string]any{"mode": "self"}
	case a.terminal != "":
		return map[string]any{"mode": "existing_session", "terminal_id": a.terminal}
	case a.open:
		out := map[string]any{"mode": "new_session"}
		if a.assistant != "" {
			out["assistant"] = a.assistant
		}
		if a.model != "" {
			out["model"] = a.model
		}
		if a.persona != "" {
			out["persona"] = a.persona
		}
		return out
	}
	return nil
}

// docFlags is what `item doc` was told: the document's role, title,
// reference and body.
type docFlags struct {
	role, title, reference, body string
}

// phaseEvidence is what `item phase` carries to the daemon besides the next
// phase; the daemon decides which of it the transition needs.
type phaseEvidence struct {
	verification, commit, target, remote, landingProject, noLanding, deployment, noDeployment string
}

func itemCommand(args []string) {
	if len(args) == 0 {
		itemUsage()
	}
	op := args[0]
	fs := flag.NewFlagSet("item "+op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	key := fs.String("key", "", "the Idempotency-Key; reuse the one printed by a failed attempt to retry it")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	project := fs.String("project", "", "the Project id from the catalog")
	kind := fs.String("kind", "", "feature, issue, epic, refactor or plan")
	title := fs.String("title", "", "the item's title")
	descriptionFile := fs.String("description-file", "", "a file holding the description; - or absent reads stdin")
	acceptanceFile := fs.String("acceptance-file", "", "a file holding the acceptance criteria Markdown")
	stepsFile := fs.String("steps-file", "", "a file holding the steps, one per non-empty line")
	deploy := fs.String("deploy", "", "required, not_required or agent_decides (default agent_decides)")
	run := fs.String("run", "", "the run of the person's message (default: this conversation's latest run)")
	expectedVersion := fs.Int64("expected-version", 0, "the item's version the write expects; refused as version_conflict when it moved on (required for acceptance-revise)")
	docID := fs.String("doc", "", "for show: print only this document's body, raw")
	role := fs.String("role", "", "for doc: spec, design, test, deploy, completion_report, plan, plan_review or other")
	reference := fs.String("reference", "", "for doc: a path in the Project, a URL, or for plan_review the review task's id")
	bodyFile := fs.String("body-file", "", "for doc or acceptance: a file holding Markdown; - or absent reads stdin")
	var assign assignFlags
	fs.StringVar(&assign.terminal, "assign-terminal", "", "for child: assign it to the existing Session in this terminal")
	fs.BoolVar(&assign.open, "assign-new", false, "for child: assign it to a new Session Clawdline opens")
	fs.BoolVar(&assign.self, "assign-self", false, "for add: assign it to this Session, when the person's message asks it to do the work now")
	fs.StringVar(&assign.terminal, "terminal", "", "for assign: the existing Session's terminal id")
	fs.BoolVar(&assign.open, "new", false, "for assign: a new Session Clawdline opens")
	fs.StringVar(&assign.assistant, "assistant", "", "with a new Session: claude or codex (default codex)")
	fs.StringVar(&assign.model, "model", "", "with a new Session: the model (default the assistant's)")
	fs.StringVar(&assign.persona, "persona", "", "with a new Session: a built-in persona to open it as (none by default)")
	var steps stringList
	fs.Var(&steps, "step", "one step of the item, in order; repeat it for each step")
	var ev phaseEvidence
	fs.StringVar(&ev.verification, "verification", "", "for merging, or deploying an ungated item: what was run to verify and what it showed")
	fs.StringVar(&ev.commit, "commit", "", "for deploying (from merging, or from implementing when the verify gate is off): the landed commit")
	fs.StringVar(&ev.target, "target", "", "for deploying: the local target branch the commit is on")
	fs.StringVar(&ev.remote, "remote", "", "for deploying: the remote whose tracking target also holds it")
	fs.StringVar(&ev.landingProject, "landing-project", "", "for deploying: the catalog Project whose repository holds the commit, when it is not the item's")
	fs.StringVar(&ev.noLanding, "no-landing-reason", "", "for deploying: why there is no code to land (instead of --commit)")
	fs.StringVar(&ev.deployment, "deployment", "", "for done: what was deployed, where, which version")
	fs.StringVar(&ev.noDeployment, "no-deployment-reason", "", "for done: why nothing needs deploying")
	positional, err := parseInterspersed(fs, args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "clawdline item %s: %v\n", op, err)
		itemUsage()
	}
	var f itemFlags
	var rest []string
	switch op {
	case "add", "child":
		if op == "add" && len(positional) != 0 {
			itemUsage()
		}
		if op == "child" {
			if len(positional) != 1 {
				fmt.Fprintf(os.Stderr, "clawdline item child: takes one Epic id, got %d arguments\n", len(positional))
				itemUsage()
			}
			rest = positional
		}
		f = itemFlags{project: *project, kind: *kind, title: *title, deploy: *deploy, run: *run, steps: steps, assign: assign}
		// A terminal on stdin is nobody piping a description: it is not read,
		// so the command never waits on a keyboard, and the daemon's
		// description_required says what is missing.
		if st, statErr := os.Stdin.Stat(); *descriptionFile != "" || (statErr == nil && st.Mode()&os.ModeCharDevice == 0) {
			description, err := readTextFrom(*descriptionFile, os.Stdin, "description")
			if err != nil {
				fail(err)
			}
			f.description = description
		}
		if *stepsFile != "" {
			fh, err := os.Open(*stepsFile)
			if err != nil {
				fail(err)
			}
			lines, err := todoLines(fh)
			_ = fh.Close()
			if err != nil {
				fail(err)
			}
			f.steps = append(f.steps, lines...)
		}
		if *acceptanceFile != "" {
			acceptance, err := readTextFrom(*acceptanceFile, os.Stdin, "acceptance criteria")
			if err != nil {
				fail(err)
			}
			f.acceptance = acceptance
		}
	case "claim":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item claim: takes one item id, got %d arguments\n", len(positional))
			itemUsage()
		}
		rest = positional
		f.run = *run
	case "name":
		if len(positional) != 2 {
			fmt.Fprintln(os.Stderr, "clawdline item name: takes an item id and the Session's task name")
			itemUsage()
		}
		rest = positional
	case "assign":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item assign: takes one item id, got %d arguments\n", len(positional))
			itemUsage()
		}
		rest = positional
		f.assign = assign
		f.run = *run
	case "steps", "show":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item %s: takes one item id, got %d arguments\n", op, len(positional))
			itemUsage()
		}
		rest = positional
		if op == "show" {
			f.docID = strings.TrimSpace(*docID)
		}
	case "step-add":
		if len(positional) < 1 {
			fmt.Fprintln(os.Stderr, "clawdline item step-add: takes an item id and the steps' titles")
			itemUsage()
		}
		rest = positional
		// Titles one per non-empty stdin line when none are arguments, as
		// `todo add` reads them; a terminal is not waited on.
		if st, statErr := os.Stdin.Stat(); len(rest) == 1 && statErr == nil && st.Mode()&os.ModeCharDevice == 0 {
			lines, err := todoLines(os.Stdin)
			if err != nil {
				fail(err)
			}
			rest = append(rest, lines...)
		}
	case "finish":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item finish: takes one item id, got %d arguments\n", len(positional))
			itemUsage()
		}
		rest = positional
		f.phase = ev
	case "step-done", "phase":
		if len(positional) != 2 {
			fmt.Fprintf(os.Stderr, "clawdline item %s: takes an item id and one more argument, got %d arguments\n", op, len(positional))
			itemUsage()
		}
		rest = positional
		f.phase = ev
	case "doc":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item doc: takes one item id, got %d arguments\n", len(positional))
			itemUsage()
		}
		rest = positional
		f.doc = docFlags{role: *role, title: *title, reference: *reference}
		// Like add's description: a terminal on stdin is not waited on.
		if st, statErr := os.Stdin.Stat(); *bodyFile != "" || (statErr == nil && st.Mode()&os.ModeCharDevice == 0) {
			body, err := readTextFrom(*bodyFile, os.Stdin, "document body")
			if err != nil {
				fail(err)
			}
			f.doc.body = body
		}
	case "acceptance", "acceptance-revise":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item %s: takes one item id, got %d arguments\n", op, len(positional))
			itemUsage()
		}
		rest = positional
		if st, statErr := os.Stdin.Stat(); *bodyFile != "" || (statErr == nil && st.Mode()&os.ModeCharDevice == 0) {
			body, err := readTextFrom(*bodyFile, os.Stdin, "acceptance criteria")
			if err != nil {
				fail(err)
			}
			f.acceptance = body
		}
		f.run, f.expectedVersion = *run, *expectedVersion
	default:
		itemUsage()
	}
	if f.expectedVersion == 0 {
		f.expectedVersion = *expectedVersion
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(sessionItem(os.Stdout, os.Stderr, b, op, f, rest, *conversation, *key, os.Getenv))
}

// parseInterspersed parses flags wherever they stand among the positional
// arguments: `item phase <id> merging --verification …` is how a person and
// the guide write it, and the flag package alone stops at `<id>` and leaves
// the flags as positionals. After "--" everything is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func itemUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline item add --project <id> --kind <feature|issue|epic|refactor|plan> --title <t>")
	fmt.Fprintln(os.Stderr, "                          [--step <text>]… [--steps-file f] [--description-file f | stdin] [--acceptance-file f]")
	fmt.Fprintln(os.Stderr, "                          [--assign-self | --assign-terminal <terminal id> | --assign-new [--assistant a] [--model m] [--persona id]]")
	fmt.Fprintln(os.Stderr, "                          [--deploy policy] [--run id] [--conversation id] [--key k] [--port n]")
	fmt.Fprintln(os.Stderr, "       clawdline item claim [--run id] [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item name [--conversation id] [--port n] <item id> <Session task name>")
	fmt.Fprintln(os.Stderr, "       clawdline item child --kind <feature|issue> --title <t> [--step <text>]… [--steps-file f] [--acceptance-file f]")
	fmt.Fprintln(os.Stderr, "                          [--description-file f | stdin] [--deploy policy]")
	fmt.Fprintln(os.Stderr, "                          [--assign-terminal <terminal id> | --assign-new [--assistant a] [--model m] [--persona id]]")
	fmt.Fprintln(os.Stderr, "                          [--conversation id] [--key k] [--port n] <epic id>")
	fmt.Fprintln(os.Stderr, "       clawdline item assign (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona id])")
	fmt.Fprintln(os.Stderr, "                          [--run id] [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item show [--doc <doc id>] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item steps [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item step-add [--conversation id] [--key k] [--port n] <item id> <title> [<title>]… | stdin")
	fmt.Fprintln(os.Stderr, "       clawdline item step-done [--conversation id] [--key k] [--port n] <item id> <step id>")
	fmt.Fprintln(os.Stderr, "       clawdline item acceptance [--body-file f | stdin] [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item acceptance-revise --run id --expected-version n [--body-file f | stdin]")
	fmt.Fprintln(os.Stderr, "                            [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item doc --role <role> --title <t> [--reference r] [--body-file f | stdin]")
	fmt.Fprintln(os.Stderr, "                          [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item phase [--verification t] [--commit c --target b --remote r [--landing-project id] | --no-landing-reason t]")
	fmt.Fprintln(os.Stderr, "                            [--deployment t | --no-deployment-reason t] [--conversation id] [--key k] [--port n]")
	fmt.Fprintln(os.Stderr, "                            <item id> <implementing|verifying|merging|deploying|done>")
	fmt.Fprintln(os.Stderr, "       clawdline item finish [--verification t] (--deployment t | --no-deployment-reason t)")
	fmt.Fprintln(os.Stderr, "                            [--commit c] [--target b] [--remote r] [--landing-project id] [--no-landing-reason t]")
	fmt.Fprintln(os.Stderr, "                            [--conversation id] [--key k] [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "  add creates a Board item only because the person's message through Clawdline asked for one;")
	fmt.Fprintln(os.Stderr, "  the registered Clawdfather may create it unassigned, then delegate to a Project Session with --assign-new or --assign-terminal;")
	fmt.Fprintln(os.Stderr, "  it arrives unassigned, and its --step rows are the item's steps, not to-dos; --assign-self")
	fmt.Fprintln(os.Stderr, "  assigns it to this Session, only when the person's message asks this Session to do the work now;")
	fmt.Fprintln(os.Stderr, "  claim assigns an existing item to this Session only because the person's message through")
	fmt.Fprintln(os.Stderr, "  Clawdline told it to take that item; never on its own initiative;")
	fmt.Fprintln(os.Stderr, "  name lets an assigned new Feature Root choose its own task name once, after reading the item;")
	fmt.Fprintln(os.Stderr, "  child breaks an Epic this Session owns, after its reviewed plan (implementing or later), into a")
	fmt.Fprintln(os.Stderr, "  feature or issue item, assigned to the Session in that terminal, to a new one, or to nobody yet;")
	fmt.Fprintln(os.Stderr, "  assign hands a child of an Epic this Session owns to a Session (terminal ids: `clawdline guide send`),")
	fmt.Fprintln(os.Stderr, "  or, only because the person's message through Clawdline asked for it, an unassigned Feature or Issue")
	fmt.Fprintln(os.Stderr, "  to a new Session (--new), under that message's run; never on its own initiative;")
	fmt.Fprintln(os.Stderr, "  --persona opens that new Session as a built-in persona (`GET /v1/personas` lists them); none by default;")
	fmt.Fprintln(os.Stderr, "  step-add breaks an item this Session owns into ordered steps, after any it already has;")
	fmt.Fprintln(os.Stderr, "  acceptance fills missing criteria once; acceptance-revise relays a person's explicit revision message;")
	fmt.Fprintln(os.Stderr, "  doc adds a document to an item this Session owns, or revises the one with the same --role and --title")
	fmt.Fprintln(os.Stderr, "  (an item's one completion_report whatever its title; plan and plan_review are always added); an Epic needs a plan, and a plan_review")
	fmt.Fprintln(os.Stderr, "  whose --reference is the id of the plan_review child that reviewed it, before implementing;")
	fmt.Fprintln(os.Stderr, "  phase moves an item this Session owns one phase on, with that phase's evidence;")
	fmt.Fprintln(os.Stderr, "  finish takes it from implementing onward to done in one step once its work has landed:")
	fmt.Fprintln(os.Stderr, "  the commit, target and remote come from the recorded landing unless given, and every gate still applies")
	os.Exit(2)
}

// itemTextLimit bounds one description, acceptance text or document body the
// CLI reads: the daemon's own limit for each (internal/app
// workV2DescriptionLimit), so a text the daemon would refuse is refused here
// before it is sent rather than after.
const itemTextLimit = 64 << 10

// readTextFrom reads a named file, or r when the name is empty or "-",
// within the daemon's limit for one item text (itemTextLimit).
func readTextFrom(name string, r io.Reader, what string) (string, error) {
	if name != "" && name != "-" {
		fh, err := os.Open(name)
		if err != nil {
			return "", err
		}
		defer fh.Close()
		r = fh
	}
	data, err := io.ReadAll(io.LimitReader(r, itemTextLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > itemTextLimit {
		return "", fmt.Errorf("the %s is larger than %d bytes, the most the daemon keeps for one item text", what, itemTextLimit)
	}
	return string(data), nil
}

// itemWire is the part of the daemon's item answer this command prints.
type itemWire struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	Kind               string  `json:"kind"`
	Phase              string  `json:"phase"`
	OwnerSession       *string `json:"owner_session"`
	Version            int64   `json:"version"`
	AcceptanceCriteria string  `json:"acceptance_criteria"`
	AcceptanceVersion  int64   `json:"acceptance_version"`
	AcceptanceDigest   string  `json:"acceptance_digest"`
	GateSnapshotCycle  int64   `json:"gate_snapshot_cycle"`
	PlanningGate       bool    `json:"planning_gate"`
	VerifyGate         bool    `json:"verify_gate"`
	ReviewRequired     bool    `json:"review_required"`
	ParentID           string  `json:"parent_id"`
	Steps              []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Done     bool   `json:"done"`
		Position int64  `json:"position"`
	} `json:"steps"`
	Documents []itemDocWire `json:"documents"`
}

type itemDocWire struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Reference string `json:"reference"`
	Position  int64  `json:"position"`
	Version   int64  `json:"version"`
}

func itemOf(a answer) (itemWire, bool) {
	var got struct {
		Item itemWire `json:"item"`
	}
	if json.Unmarshal(a.Body, &got) != nil || got.Item.ID == "" {
		return itemWire{}, false
	}
	return got.Item, true
}

// verifyGateHint is printed only for an item whose captured verify gate is
// on: such an item walks verifying and merging, which the guide's ordinary
// path leaves out. Every command that prints an item shares it.
const verifyGateHint = "  verify gate on: this item walks verifying and merging before deploying; read \"Captured planning and verification gates\" in `clawdline guide board`"

func printVerifyGateHint(w io.Writer, it itemWire) {
	if it.VerifyGate {
		fmt.Fprintln(w, verifyGateHint)
	}
}

// printItem prints an item as the daemon's GET answers it, whole: an
// empty step list there means the item has none. A write's answer carries
// no steps or documents, so it is never printed through here (itemWrote).
func printItem(w io.Writer, it itemWire) {
	printItemHead(w, it)
	if len(it.Steps) == 0 {
		fmt.Fprintln(w, "  (no steps)")
	}
	for _, s := range it.Steps {
		mark := " "
		if s.Done {
			mark = "x"
		}
		fmt.Fprintf(w, "  [%s] %s  %s\n", mark, s.ID, s.Title)
	}
	for _, d := range it.Documents {
		fmt.Fprintf(w, "  doc %s  %s  %s  v%d\n", d.ID, d.Role, d.Title, d.Version)
	}
}

// printItemHead prints what every answer about an item carries: its row,
// acceptance and gates, without steps or documents.
func printItemHead(w io.Writer, it itemWire) {
	owner := "unassigned"
	if it.OwnerSession != nil && *it.OwnerSession != "" {
		owner = "assigned to " + *it.OwnerSession
	}
	fmt.Fprintf(w, "%s  %s  [%s, %s, %s]\n", it.ID, it.Title, it.Kind, it.Phase, owner)
	if it.AcceptanceCriteria != "" {
		fmt.Fprintf(w, "  acceptance v%d sha256:%s\n%s\n", it.AcceptanceVersion, it.AcceptanceDigest, it.AcceptanceCriteria)
	}
	if it.GateSnapshotCycle > 0 {
		fmt.Fprintf(w, "  gates cycle %d: planning=%t verification=%t\n", it.GateSnapshotCycle, it.PlanningGate, it.VerifyGate)
	}
	printVerifyGateHint(w, it)
	if it.Kind == "feature" {
		// The person's switch, read when the Feature asks to enter
		// implementing; the Agent follows it and never sets it.
		fmt.Fprintf(w, "  needs independent review (set by the person): %t\n", it.ReviewRequired)
	}
}

// sessionItem is the command, answering its exit status.
func sessionItem(stdout, stderr io.Writer, b *broker, op string, f itemFlags, args []string, conversation, key string,
	getenv func(string) string) int {
	name := "item " + op
	if op == "steps" || op == "show" {
		it, code := readItem(stdout, stderr, b, name, strings.TrimSpace(args[0]))
		if code != 0 {
			return code
		}
		if op == "show" {
			return itemShow(stdout, stderr, it, f.docID)
		}
		printItem(stdout, it)
		return 0
	}
	if conversation == "" {
		var err error
		if conversation, _, err = conversationFromEnv(getenv); err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %s Nothing was changed.\n", name, conversationRefusal(err, "--conversation"))
			return 2
		}
	}
	if conversation == "" {
		fmt.Fprintf(stderr, "clawdline %s: cannot tell which conversation this is: none of %s is set. "+
			"Pass --conversation <this assistant's conversation id>. Nothing was changed.\n",
			name, strings.Join(conversationEnv, ", "))
		return 2
	}
	if op == "name" {
		return itemName(stdout, stderr, b, args, conversation)
	}
	if op == "step-add" {
		return itemStepAdd(stdout, stderr, b, args, conversation, key)
	}
	var path string
	var body map[string]any
	// docBefore is the version of the document `doc` revises, zero when it adds one.
	var docBefore int64
	switch op {
	case "add":
		if strings.TrimSpace(f.project) == "" || strings.TrimSpace(f.kind) == "" || strings.TrimSpace(f.title) == "" {
			fmt.Fprintf(stderr, "clawdline %s: --project, --kind and --title are all required. Nothing was created.\n", name)
			return 2
		}
		if f.assign.choices() > 1 {
			fmt.Fprintf(stderr, "clawdline %s: --assign-self, --assign-terminal and --assign-new are one choice. Nothing was created.\n", name)
			return 2
		}
		if why := f.assign.personaRefusal("--assign-terminal", "--assign-new"); why != "" {
			fmt.Fprintf(stderr, "clawdline %s: %s Nothing was created.\n", name, why)
			return 2
		}
		run, code := wordRun(stdout, stderr, b, name, f.run, conversation,
			"Nothing was created. Without a message sent through Clawdline, file a proposal "+
				"(POST /v1/work/v2/agent/proposals) and tell the person to accept it in the Board's Agent proposals.")
		if code != 0 {
			return code
		}
		steps := make([]string, 0, len(f.steps))
		for _, s := range f.steps {
			if s = strings.TrimSpace(s); s != "" {
				steps = append(steps, s)
			}
		}
		body = map[string]any{"session_id": conversation, "via": map[string]string{"run": run},
			"project_id": f.project, "kind": f.kind, "title": f.title, "description": f.description,
			"acceptance_criteria": f.acceptance}
		if f.deploy != "" {
			body["deployment_policy"] = f.deploy
		}
		if len(steps) > 0 {
			body["steps"] = steps
		}
		if a := f.assign.request(); a != nil {
			body["assign"] = a
		}
		path = "/v1/work/v2/agent/items"
	case "child":
		epicID := strings.TrimSpace(args[0])
		if strings.TrimSpace(f.kind) == "" || strings.TrimSpace(f.title) == "" {
			fmt.Fprintf(stderr, "clawdline %s: --kind and --title are both required. Nothing was created.\n", name)
			return 2
		}
		if f.assign.self {
			fmt.Fprintf(stderr, "clawdline %s: --assign-self is for item add; an Epic's child goes to "+
				"--assign-terminal or --assign-new. Nothing was created.\n", name)
			return 2
		}
		if f.assign.terminal != "" && f.assign.open {
			fmt.Fprintf(stderr, "clawdline %s: --assign-terminal and --assign-new are one choice. Nothing was created.\n", name)
			return 2
		}
		if why := f.assign.personaRefusal("--assign-terminal", "--assign-new"); why != "" {
			fmt.Fprintf(stderr, "clawdline %s: %s Nothing was created.\n", name, why)
			return 2
		}
		body = map[string]any{"session_id": conversation, "kind": f.kind,
			"title": f.title, "description": f.description, "acceptance_criteria": f.acceptance}
		if steps := trimmedSteps(f.steps); len(steps) > 0 {
			body["steps"] = steps
		}
		if f.deploy != "" {
			body["deployment_policy"] = f.deploy
		}
		if a := f.assign.request(); a != nil {
			body["assign"] = a
		}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(epicID) + "/children"
	case "assign":
		itemID := strings.TrimSpace(args[0])
		a := f.assign.request()
		if a == nil || f.assign.self || f.assign.choices() > 1 {
			fmt.Fprintf(stderr, "clawdline %s: name one Session: --terminal <terminal id> or --new. Nothing was changed.\n", name)
			return 2
		}
		if why := f.assign.personaRefusal("--terminal", "--new"); why != "" {
			fmt.Fprintf(stderr, "clawdline %s: %s Nothing was changed.\n", name, why)
			return 2
		}
		it, code := readItem(stdout, stderr, b, name, itemID)
		if code != 0 {
			return code
		}
		body = map[string]any{"session_id": conversation}
		for k, v := range a {
			body[k] = v
		}
		// An item that is no Epic's child goes to a new Session only on the
		// person's message, under its run; an Epic's child on its owner's
		// authority, unless --run names a message.
		if it.ParentID == "" || strings.TrimSpace(f.run) != "" {
			if !f.assign.open {
				fmt.Fprintf(stderr, "clawdline %s: on the person's message a Session assigns an item only to a new Session (--new); "+
					"to take it itself, use `clawdline item claim %s`. Nothing was changed.\n", name, itemID)
				return 2
			}
			run, code := wordRun(stdout, stderr, b, name, f.run, conversation,
				"Nothing was assigned. Without a message sent through Clawdline asking for it, leave the item for the person to assign.")
			if code != 0 {
				return code
			}
			body["via"] = map[string]string{"run": run}
		}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/assign"
	case "claim":
		itemID := strings.TrimSpace(args[0])
		run, code := wordRun(stdout, stderr, b, name, f.run, conversation,
			"Nothing was claimed. Without a message sent through Clawdline, leave the item for the person to assign.")
		if code != 0 {
			return code
		}
		body = map[string]any{"session_id": conversation, "via": map[string]string{"run": run}}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/claim"
	case "step-done":
		itemID, stepID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		body = map[string]any{"session_id": conversation}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/steps/" + url.PathEscape(stepID) + "/complete"
	case "phase":
		itemID, next := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		ev := f.phase
		landing := ev.commit != "" || ev.target != "" || ev.remote != ""
		if (landing || ev.landingProject != "") && (ev.commit == "" || ev.target == "" || ev.remote == "") {
			fmt.Fprintf(stderr, "clawdline %s: --commit, --target and --remote go together. Nothing was changed.\n", name)
			return 2
		}
		a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(itemID), nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return 1
		}
		it, ok := itemOf(a)
		if !a.ok() || !ok {
			return report(stdout, stderr, name, a)
		}
		// Read for the verification gate only; the write acts on the
		// version the item holds then, unless --expected-version names one.
		body = map[string]any{"session_id": conversation, "next": next}
		if next == "verifying" && it.VerifyGate {
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(stderr, "clawdline %s: candidate worktree: %v\n", name, err)
				return 1
			}
			gitRead := func(args ...string) (string, error) {
				cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
				out, err := cmd.Output()
				return strings.TrimSpace(string(out)), err
			}
			branch, branchErr := gitRead("symbolic-ref", "--short", "HEAD")
			commit, commitErr := gitRead("rev-parse", "--verify", "HEAD^{commit}")
			if branchErr != nil || commitErr != nil || branch == "" || commit == "" {
				fmt.Fprintf(stderr, "clawdline %s: verification needs a branch-attached Git worktree with a readable HEAD. Nothing was changed.\n", name)
				return 1
			}
			body["candidate"] = map[string]string{"worktree": cwd, "branch": branch, "commit": commit}
		}
		for k, v := range map[string]string{"verification": ev.verification, "deployment": ev.deployment,
			"no_deployment_reason": ev.noDeployment, "no_landing_reason": ev.noLanding} {
			if strings.TrimSpace(v) != "" {
				body[k] = v
			}
		}
		if landing {
			l := map[string]string{"commit": ev.commit, "target": ev.target, "remote": ev.remote}
			if ev.landingProject != "" {
				l["project"] = ev.landingProject
			}
			body["landing"] = l
		}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/phase"
	case "finish":
		// The rest of the way in one command, once the work has landed: the
		// daemon walks verifying, merging, deploying and done through the
		// same gates `phase` meets, and reads the commit, target and remote
		// from the landing it already holds. A flag given here wins over what
		// it would read.
		itemID, ev := strings.TrimSpace(args[0]), f.phase
		body = map[string]any{"session_id": conversation}
		for k, v := range map[string]string{"verification": ev.verification, "deployment": ev.deployment,
			"no_deployment_reason": ev.noDeployment, "no_landing_reason": ev.noLanding} {
			if strings.TrimSpace(v) != "" {
				body[k] = v
			}
		}
		l := map[string]string{}
		for k, v := range map[string]string{"commit": ev.commit, "target": ev.target, "remote": ev.remote,
			"project": ev.landingProject} {
			if strings.TrimSpace(v) != "" {
				l[k] = v
			}
		}
		if len(l) > 0 {
			body["landing"] = l
		}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/finish"
	case "doc":
		itemID, d := strings.TrimSpace(args[0]), f.doc
		if strings.TrimSpace(d.role) == "" || strings.TrimSpace(d.title) == "" {
			fmt.Fprintf(stderr, "clawdline %s: --role and --title are both required. Nothing was added.\n", name)
			return 2
		}
		a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(itemID), nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return 1
		}
		it, ok := itemOf(a)
		if !a.ok() || !ok {
			return report(stdout, stderr, name, a)
		}
		// After every document already there, as step-add places steps; a
		// revision keeps the place of the document it revises, so sending the
		// same text again is answered as a retry, not a new version.
		position := int64(0)
		for j, doc := range it.Documents {
			if j == 0 || doc.Position >= position {
				position = doc.Position + 1
			}
		}
		if prior, ok := revisedDocument(it, d.role, d.title); ok {
			position, docBefore = prior.Position, prior.Version
		}
		body = map[string]any{"session_id": conversation, "role": d.role,
			"title": d.title, "body": d.body, "reference": d.reference, "position": position}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/documents"
	case "acceptance":
		itemID := strings.TrimSpace(args[0])
		if strings.TrimSpace(f.acceptance) == "" {
			fmt.Fprintf(stderr, "clawdline %s: provide non-empty Markdown with --body-file or stdin. Nothing was changed.\n", name)
			return 2
		}
		body = map[string]any{"session_id": conversation, "acceptance_criteria": f.acceptance}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/edit"
	case "acceptance-revise":
		itemID := strings.TrimSpace(args[0])
		if strings.TrimSpace(f.acceptance) == "" || f.run == "" || f.expectedVersion < 1 {
			fmt.Fprintf(stderr, "clawdline %s: non-empty Markdown, --run and --expected-version are required. Nothing was changed.\n", name)
			return 2
		}
		body = map[string]any{"expected_version": f.expectedVersion, "session_id": conversation,
			"acceptance_criteria": f.acceptance, "via": map[string]string{"run": f.run}}
		path = "/v1/work/v2/agent/items/" + url.PathEscape(itemID) + "/acceptance-revision"
	default:
		fmt.Fprintf(stderr, "clawdline item: no such action %q\n", op)
		return 2
	}
	// The daemon compares a version only when one was named: a version read
	// a moment ago and sent straight back protects nothing.
	if f.expectedVersion > 0 {
		body["expected_version"] = f.expectedVersion
	}
	if key == "" {
		key = newKey("item")
	}
	// Said before the request, so that an attempt that dies on the way can
	// be retried as the same write rather than made a second time.
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", key)
	method := http.MethodPost
	if op == "acceptance" {
		method = http.MethodPatch
	}
	a, err := b.request(method, path, nil, body, key)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		fmt.Fprintf(stderr, "To retry the same write: clawdline %s --key %s …\n", name, key)
		return 1
	}
	it, ok := itemOf(a)
	if !a.ok() || !ok {
		return report(stdout, stderr, name, a)
	}
	itemWrote(stdout, op, args, it)
	if op == "doc" {
		docWrote(stdout, it, f.doc, docBefore)
	}
	if op == "acceptance-revise" {
		var revision struct {
			Source struct {
				Run       string `json:"run"`
				SessionID string `json:"session_id"`
				Excerpt   string `json:"excerpt"`
				At        int64  `json:"at"`
			} `json:"acceptance_source"`
		}
		if json.Unmarshal(a.Body, &revision) == nil {
			fmt.Fprintf(stdout, "  requested by run %s in Session %s at %d: %s\n",
				revision.Source.Run, revision.Source.SessionID, revision.Source.At, revision.Source.Excerpt)
		}
	}
	if op == "child" || op == "add" {
		var got struct {
			AssignmentState string `json:"assignment_state"`
			AssignmentError *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"assignment_error"`
		}
		if json.Unmarshal(a.Body, &got) == nil && got.AssignmentError != nil {
			fmt.Fprintf(stderr, "clawdline %s: the item was created but not assigned: %s: %s\n",
				name, got.AssignmentError.Code, got.AssignmentError.Message)
			if op == "child" {
				fmt.Fprintf(stderr, "Assign it with `clawdline item assign %s --terminal <id> | --new`, or leave it for the person.\n", it.ID)
			} else {
				fmt.Fprintf(stderr, "The person can assign item %s from the Board.\n", it.ID)
			}
			return 1
		}
		if op == "add" {
			switch got.AssignmentState {
			case "not_requested":
				fmt.Fprintf(stderr, "Board item %s was created but not assigned. The person can assign it from the Board.\n", it.ID)
				if f.assign.request() == nil && it.Kind != "plan" {
					fmt.Fprintf(stderr, "When the person's message asks this Session to do it, take it with `clawdline item claim %s`.\n", it.ID)
				}
			case "awaiting_user":
				fmt.Fprintf(stderr, "Board item %s is awaiting the person's first dialog in the new Project Session; no owner is assigned yet. Open that Session and answer its first screen, then check the Board.\n", it.ID)
				return 1
			case "pending":
				fmt.Fprintf(stderr, "Board item %s was created; delegation outcome needs review. Check the Board before trying another assignment.\n", it.ID)
				return 1
			}
		}
	}
	if (op == "add" || op == "claim") && it.Kind == "epic" && it.OwnerSession != nil {
		fmt.Fprintf(stdout, "This is an Epic: write its plan with `clawdline item doc %s --role plan --title Plan`, "+
			"have a child review it (`clawdline dispatch --kind plan_review --work-id %s --title \"Review the plan\" --claims \"\"`), record the review with "+
			"`--role plan_review --reference <task id>`, then move it to implementing. `clawdline guide epic` says how.\n",
			it.ID, it.ID)
	}
	return 0
}

func itemName(stdout, stderr io.Writer, b *broker, args []string, conversation string) int {
	name := "item name"
	itemID, title := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	if itemID == "" || title == "" {
		fmt.Fprintln(stderr, "clawdline item name: an item id and a nonempty task name are required. Nothing was changed.")
		return 2
	}
	a, err := b.request(http.MethodPost, "/v1/work/v2/agent/items/"+url.PathEscape(itemID)+"/session-name",
		nil, map[string]any{"session_id": conversation, "title": title}, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, name, a)
	}
	var got struct {
		StoredTitle  string  `json:"stored_title"`
		DisplayTitle *string `json:"display_title"`
	}
	if json.Unmarshal(a.Body, &got) != nil || got.StoredTitle == "" {
		fmt.Fprintln(stderr, "clawdline item name: the daemon returned no stored Session name.")
		return 1
	}
	fmt.Fprintf(stdout, "Session name: %s\n", got.StoredTitle)
	if got.DisplayTitle != nil && *got.DisplayTitle != got.StoredTitle {
		fmt.Fprintf(stdout, "Displayed name: %s\n", *got.DisplayTitle)
	}
	return 0
}

// itemShow prints the item as `steps` does and every document's body after
// it; with docID, only that document's body, raw, so it can be piped. An id
// the item does not hold is refused with the ids it does.
func itemShow(stdout, stderr io.Writer, it itemWire, docID string) int {
	if docID != "" {
		ids := make([]string, 0, len(it.Documents))
		for _, d := range it.Documents {
			if d.ID == docID {
				fmt.Fprint(stdout, d.Body)
				if d.Body != "" && !strings.HasSuffix(d.Body, "\n") {
					fmt.Fprintln(stdout)
				}
				return 0
			}
			ids = append(ids, d.ID)
		}
		held := "it holds no documents"
		if len(ids) > 0 {
			held = "its documents are " + strings.Join(ids, ", ")
		}
		fmt.Fprintf(stderr, "clawdline item show: item %s has no document %s; %s.\n", it.ID, docID, held)
		return 1
	}
	printItem(stdout, it)
	fmt.Fprintf(stdout, "  item version %d\n", it.Version)
	for _, d := range it.Documents {
		fmt.Fprintf(stdout, "\n===== doc %s  %s  %s  (v%d) =====\n", d.ID, d.Role, d.Title, d.Version)
		if d.Reference != "" {
			fmt.Fprintf(stdout, "reference: %s\n", d.Reference)
		}
		if d.Body != "" {
			fmt.Fprint(stdout, d.Body)
			if !strings.HasSuffix(d.Body, "\n") {
				fmt.Fprintln(stdout)
			}
		}
	}
	return 0
}

// revisedDocument is the document on it that writing role and title would
// revise, as the daemon chooses it (work.DocumentRevisable): the
// completion_report whatever its title, otherwise the earliest with the same
// role and title.
func revisedDocument(it itemWire, role, title string) (itemDocWire, bool) {
	role, title = strings.TrimSpace(role), strings.TrimSpace(title)
	if !work.DocumentRevisable(role, title) {
		return itemDocWire{}, false
	}
	for _, d := range it.Documents {
		if d.Role == role && (d.Title == title || role == work.DocumentCompletionReport) {
			return d, true
		}
	}
	return itemDocWire{}, false
}

// docWrote says whether `item doc` added a document or revised one, and at
// which version; before is the revised document's version read before the
// write, zero when there was none.
func docWrote(stdout io.Writer, it itemWire, d docFlags, before int64) {
	got, ok := revisedDocument(it, d.role, d.title)
	if !ok {
		return
	}
	switch {
	case before == 0:
		fmt.Fprintf(stdout, "  added document %s (%s) at v%d\n", got.ID, got.Role, got.Version)
	case got.Version == before:
		fmt.Fprintf(stdout, "  document %s (%s) already held this text; nothing changed, still v%d\n", got.ID, got.Role, got.Version)
	default:
		fmt.Fprintf(stdout, "  revised document %s (%s) to v%d\n", got.ID, got.Role, got.Version)
	}
}

// itemWrote says what a write did and prints the item row its answer
// carries. That answer holds no steps or documents, so none are claimed
// about: `item show` reads them.
func itemWrote(stdout io.Writer, op string, args []string, it itemWire) {
	what := map[string]string{"add": "the item", "child": "the child item", "assign": "the assignment",
		"claim": "the claim", "finish": "the finish", "doc": "the document",
		"acceptance": "the acceptance criteria", "acceptance-revise": "the acceptance revision"}[op]
	if op == "step-done" && len(args) > 1 {
		what = "step " + strings.TrimSpace(args[1]) + " done"
	} else if op == "phase" && len(args) > 1 {
		what = "phase " + strings.TrimSpace(args[1])
	}
	fmt.Fprintf(stdout, "wrote %s; item %s is at version %d\n", what, it.ID, it.Version)
	printItemHead(stdout, it)
	fmt.Fprintf(stdout, "  steps and documents: clawdline item show %s\n", it.ID)
}

// readItem reads one item whole, as the daemon's GET answers it.
func readItem(stdout, stderr io.Writer, b *broker, name, id string) (itemWire, int) {
	a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		return itemWire{}, 1
	}
	it, ok := itemOf(a)
	if !a.ok() || !ok {
		return itemWire{}, report(stdout, stderr, name, a)
	}
	return it, 0
}

// trimmedSteps drops empty step titles and trims the rest.
func trimmedSteps(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// wordRun is the run a write on the person's word goes under: the one --run
// named, else this conversation's latest. nothing is said when there is none.
func wordRun(stdout, stderr io.Writer, b *broker, name, named, conversation, nothing string) (string, int) {
	if run := strings.TrimSpace(named); run != "" {
		return run, 0
	}
	a, err := b.request(http.MethodGet, "/v1/orchestrator/sessions/"+url.PathEscape(conversation)+"/run", nil, nil, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		return "", 1
	}
	if !a.ok() {
		report(stdout, stderr, name, a)
		fmt.Fprintln(stderr, nothing)
		return "", 1
	}
	var got struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if json.Unmarshal(a.Body, &got) != nil || got.Run.ID == "" {
		fmt.Fprintf(stderr, "clawdline %s: the daemon's run answer names no run: %s\n", name, strings.TrimSpace(string(a.Body)))
		return "", 1
	}
	return got.Run.ID, 0
}

// itemStepAdd posts one step per title, in order. Each is written after a
// fresh read of the item, for its version and for the last position: the
// daemon orders steps by position and then by id, and an id is random, so
// each new step takes the position after every step already there. --key is
// the first write's; the rest get their own, each printed before its write.
func itemStepAdd(stdout, stderr io.Writer, b *broker, args []string, conversation, key string) int {
	const name = "item step-add"
	itemID := strings.TrimSpace(args[0])
	var titles []string
	for _, t := range args[1:] {
		if t = strings.TrimSpace(t); t != "" {
			titles = append(titles, t)
		}
	}
	if len(titles) == 0 {
		fmt.Fprintf(stderr, "clawdline %s: no step titles, as arguments or on stdin. Nothing was changed.\n", name)
		return 2
	}
	read := func() (itemWire, int) {
		a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(itemID), nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return itemWire{}, 1
		}
		it, ok := itemOf(a)
		if !a.ok() || !ok {
			return itemWire{}, report(io.Discard, stderr, name, a)
		}
		return it, 0
	}
	stopped := func(added int) {
		if added == 0 {
			fmt.Fprintf(stderr, "No step was added.\n")
			return
		}
		fmt.Fprintf(stderr, "%d of %d steps were added; the rest were not: %s\n", added, len(titles),
			strings.Join(titles[added:], " | "))
	}
	for i, title := range titles {
		it, code := read()
		if code != 0 {
			stopped(i)
			return code
		}
		position := int64(0)
		for j, s := range it.Steps {
			if j == 0 || s.Position >= position {
				position = s.Position + 1
			}
		}
		k := key
		if k == "" || i > 0 {
			k = newKey("item")
		}
		fmt.Fprintf(stderr, "Idempotency-Key: %s\n", k)
		body := map[string]any{"session_id": conversation, "title": title, "position": position}
		a, err := b.request(http.MethodPost, "/v1/work/v2/agent/items/"+url.PathEscape(itemID)+"/steps", nil, body, k)
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			fmt.Fprintf(stderr, "To retry the same write: clawdline %s --key %s %s …\n", name, k, itemID)
			stopped(i)
			return 1
		}
		if !a.ok() {
			code := report(io.Discard, stderr, name, a)
			stopped(i)
			return code
		}
	}
	it, code := read()
	if code != 0 {
		return code
	}
	printItem(stdout, it)
	return 0
}
