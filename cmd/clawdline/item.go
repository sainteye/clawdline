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

// `clawdline item`: a Board item a Session creates because the person told it
// to, in a message sent through Clawdline (docs/work-system-v2.md §2, amended
// 2026-09-25). `add` creates it under that message's run — read from this
// conversation's latest run unless --run names one — and it arrives assigned
// to this Session with its steps. `steps` lists an item's steps with their
// ids, `step-done` completes one after it is verified, and `phase` moves an
// item this Session owns to its next execution phase with that phase's
// evidence (docs/work-system-v2.md §6).
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
	project, kind, title, description, deploy, run string
	steps                                          []string
	phase                                          phaseEvidence
}

// phaseEvidence is what `item phase` carries to the daemon besides the next
// phase; the daemon decides which of it the transition needs.
type phaseEvidence struct {
	verification, commit, target, remote, landingProject, deployment, noDeployment string
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
	stepsFile := fs.String("steps-file", "", "a file holding the steps, one per non-empty line")
	deploy := fs.String("deploy", "", "required, not_required or agent_decides (default agent_decides)")
	run := fs.String("run", "", "the run of the person's message (default: this conversation's latest run)")
	var steps stringList
	fs.Var(&steps, "step", "one step of the item, in order; repeat it for each step")
	var ev phaseEvidence
	fs.StringVar(&ev.verification, "verification", "", "for merging: what was run to verify and what it showed")
	fs.StringVar(&ev.commit, "commit", "", "for deploying: the landed commit")
	fs.StringVar(&ev.target, "target", "", "for deploying: the local target branch the commit is on")
	fs.StringVar(&ev.remote, "remote", "", "for deploying: the remote whose tracking target also holds it")
	fs.StringVar(&ev.landingProject, "landing-project", "", "for deploying: the catalog Project whose repository holds the commit, when it is not the item's")
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
	case "add":
		if len(positional) != 0 {
			itemUsage()
		}
		f = itemFlags{project: *project, kind: *kind, title: *title, deploy: *deploy, run: *run, steps: steps}
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
	case "steps":
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "clawdline item steps: takes one item id, got %d arguments\n", len(positional))
			itemUsage()
		}
		rest = positional
	case "step-done", "phase":
		if len(positional) != 2 {
			fmt.Fprintf(os.Stderr, "clawdline item %s: takes an item id and one more argument, got %d arguments\n", op, len(positional))
			itemUsage()
		}
		rest = positional
		f.phase = ev
	default:
		itemUsage()
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
	fmt.Fprintln(os.Stderr, "                          [--step <text>]… [--steps-file f] [--description-file f | stdin]")
	fmt.Fprintln(os.Stderr, "                          [--deploy policy] [--run id] [--conversation id] [--key k] [--port n]")
	fmt.Fprintln(os.Stderr, "       clawdline item steps [--port n] <item id>")
	fmt.Fprintln(os.Stderr, "       clawdline item step-done [--conversation id] [--key k] [--port n] <item id> <step id>")
	fmt.Fprintln(os.Stderr, "       clawdline item phase [--verification t] [--commit c --target b --remote r [--landing-project id]]")
	fmt.Fprintln(os.Stderr, "                            [--deployment t | --no-deployment-reason t] [--conversation id] [--key k] [--port n]")
	fmt.Fprintln(os.Stderr, "                            <item id> <implementing|verifying|merging|deploying|done>")
	fmt.Fprintln(os.Stderr, "  add creates a Board item only because the person's message through Clawdline asked for one;")
	fmt.Fprintln(os.Stderr, "  it arrives assigned to this Session, and its --step rows are the item's steps, not to-dos;")
	fmt.Fprintln(os.Stderr, "  phase moves an item this Session owns one phase on, with that phase's evidence")
	os.Exit(2)
}

// readTextFrom reads a named file, or r when the name is empty or "-",
// within the daemon's work-system body cap.
func readTextFrom(name string, r io.Reader, what string) (string, error) {
	if name != "" && name != "-" {
		fh, err := os.Open(name)
		if err != nil {
			return "", err
		}
		defer fh.Close()
		r = fh
	}
	data, err := io.ReadAll(io.LimitReader(r, todoInputLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > todoInputLimit {
		return "", fmt.Errorf("the %s is larger than %d bytes, the most the daemon reads", what, todoInputLimit)
	}
	return string(data), nil
}

// itemWire is the part of the daemon's item answer this command prints.
type itemWire struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Kind         string  `json:"kind"`
	Phase        string  `json:"phase"`
	OwnerSession *string `json:"owner_session"`
	Version      int64   `json:"version"`
	Steps        []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	} `json:"steps"`
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

func printItem(w io.Writer, it itemWire) {
	owner := "unassigned"
	if it.OwnerSession != nil && *it.OwnerSession != "" {
		owner = "assigned to " + *it.OwnerSession
	}
	fmt.Fprintf(w, "%s  %s  [%s, %s, %s]\n", it.ID, it.Title, it.Kind, it.Phase, owner)
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
}

// sessionItem is the command, answering its exit status.
func sessionItem(stdout, stderr io.Writer, b *broker, op string, f itemFlags, args []string, conversation, key string,
	getenv func(string) string) int {
	name := "item " + op
	if op == "steps" {
		a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(args[0]), nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return 1
		}
		it, ok := itemOf(a)
		if !a.ok() || !ok {
			return report(stdout, stderr, name, a)
		}
		printItem(stdout, it)
		return 0
	}
	if conversation == "" {
		for _, env := range conversationEnv {
			if v := strings.TrimSpace(getenv(env)); v != "" {
				conversation = v
				break
			}
		}
	}
	if conversation == "" {
		fmt.Fprintf(stderr, "clawdline %s: cannot tell which conversation this is: none of %s is set. "+
			"Pass --conversation <this assistant's conversation id>. Nothing was changed.\n",
			name, strings.Join(conversationEnv, ", "))
		return 2
	}
	var path string
	var body map[string]any
	switch op {
	case "add":
		if strings.TrimSpace(f.project) == "" || strings.TrimSpace(f.kind) == "" || strings.TrimSpace(f.title) == "" {
			fmt.Fprintf(stderr, "clawdline %s: --project, --kind and --title are all required. Nothing was created.\n", name)
			return 2
		}
		run := strings.TrimSpace(f.run)
		if run == "" {
			a, err := b.request(http.MethodGet, "/v1/orchestrator/sessions/"+url.PathEscape(conversation)+"/run", nil, nil, "")
			if err != nil {
				fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
				return 1
			}
			if !a.ok() {
				report(stdout, stderr, name, a)
				fmt.Fprintf(stderr, "Nothing was created. Without a message sent through Clawdline, file a proposal "+
					"(POST /v1/work/v2/agent/proposals) and tell the person to accept it in the Board's Agent proposals.\n")
				return 1
			}
			var got struct {
				Run struct {
					ID string `json:"id"`
				} `json:"run"`
			}
			if json.Unmarshal(a.Body, &got) != nil || got.Run.ID == "" {
				fmt.Fprintf(stderr, "clawdline %s: the daemon's run answer names no run: %s\n", name, strings.TrimSpace(string(a.Body)))
				return 1
			}
			run = got.Run.ID
		}
		steps := make([]string, 0, len(f.steps))
		for _, s := range f.steps {
			if s = strings.TrimSpace(s); s != "" {
				steps = append(steps, s)
			}
		}
		body = map[string]any{"session_id": conversation, "via": map[string]string{"run": run},
			"project_id": f.project, "kind": f.kind, "title": f.title, "description": f.description}
		if f.deploy != "" {
			body["deployment_policy"] = f.deploy
		}
		if len(steps) > 0 {
			body["steps"] = steps
		}
		path = "/v1/work/v2/agent/items"
	case "step-done":
		itemID, stepID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		a, err := b.request(http.MethodGet, "/v1/work/v2/items/"+url.PathEscape(itemID), nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return 1
		}
		it, ok := itemOf(a)
		if !a.ok() || !ok {
			return report(stdout, stderr, name, a)
		}
		body = map[string]any{"expected_version": it.Version, "session_id": conversation}
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
		body = map[string]any{"expected_version": it.Version, "session_id": conversation, "next": next}
		for k, v := range map[string]string{"verification": ev.verification, "deployment": ev.deployment,
			"no_deployment_reason": ev.noDeployment} {
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
	default:
		fmt.Fprintf(stderr, "clawdline item: no such action %q\n", op)
		return 2
	}
	if key == "" {
		key = newKey("item")
	}
	// Said before the request, so that an attempt that dies on the way can
	// be retried as the same write rather than made a second time.
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", key)
	a, err := b.request(http.MethodPost, path, nil, body, key)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		fmt.Fprintf(stderr, "To retry the same write: clawdline %s --key %s …\n", name, key)
		return 1
	}
	it, ok := itemOf(a)
	if !a.ok() || !ok {
		return report(stdout, stderr, name, a)
	}
	printItem(stdout, it)
	return 0
}
