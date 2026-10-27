package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

func playbookCommand(args []string) {
	os.Exit(runPlaybook(args, os.Stdin, os.Stdout, os.Stderr, openBroker))
}

// Definitions share the snippet transport. Never allow this command to edit
// another reusable-text scope, or infer a Project from an agent's worktree.
func runPlaybook(args []string, stdin io.Reader, stdout, stderr io.Writer, open func(int) (*broker, error)) int {
	usage := func() int {
		fmt.Fprintln(stderr, cliCopy("misc", "playbook.usage", "usage: clawdline playbook list [--session <terminal>] | create (--project <key> | --global) --body-file <JSON path|-> | edit --body-file <JSON path|-> <id> | delete <id>"))
		fmt.Fprintln(stderr, cliCopy("misc", "playbook.help", "Put flags before the id. All actions accept --port; writes accept --key for an exact retry. JSON fields: title, body, optional source. See clawdline guide project."))
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	op := args[0]
	fs := flag.NewFlagSet("playbook "+op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "")
	key := fs.String("key", "", "")
	file := fs.String("body-file", "", "")
	project := fs.String("project", "", "")
	global := fs.Bool("global", false, "")
	session := fs.String("session", "", "")
	if fs.Parse(args[1:]) != nil {
		return usage()
	}
	switch op {
	case "list":
		if fs.NArg() != 0 || *key != "" || *file != "" || *project != "" || *global {
			return usage()
		}
	case "create":
		if fs.NArg() != 0 || *file == "" || (*project == "") == !*global || *session != "" {
			return usage()
		}
	case "edit", "delete":
		if fs.NArg() != 1 || fs.Arg(0) == "" || *project != "" || *global || *session != "" || (op == "edit") != (*file != "") {
			return usage()
		}
	default:
		return usage()
	}
	var body map[string]json.RawMessage
	if *file != "" {
		reader := stdin
		if *file != "-" {
			f, err := os.Open(*file)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			defer f.Close()
			reader = f
		}
		// Reuse the broker's existing input safety ceiling; the daemon owns
		// the stricter snippet admission limits and typed refusals.
		raw, err := io.ReadAll(io.LimitReader(reader, brokerAnswerLimit+1))
		if err != nil || len(raw) > brokerAnswerLimit || json.Unmarshal(raw, &body) != nil || len(body) == 0 {
			fmt.Fprintln(stderr, cliCopy("misc", "playbook.invalid_body", "Provide one nonempty JSON object within the CLI input limit."))
			return 2
		}
		for field := range body {
			if field != "title" && field != "body" && field != "source" {
				fmt.Fprintf(stderr, cliCopy("misc", "playbook.invalid_field", "Field %q is not writable here; use title, body or source.\n"), field)
				return 2
			}
		}
		if op == "create" {
			body["scope"] = json.RawMessage(`"playbook"`)
			if *project != "" {
				body["project"], _ = json.Marshal(*project)
			}
		}
	}
	b, err := open(*port)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	query := url.Values{}
	if *session != "" {
		query.Set("session", *session)
	}
	// Read before an edit/delete so a snippet ID cannot silently name a
	// different kind of text. The daemon has no GET-by-id snippet route.
	if op != "create" {
		a, err := b.request(http.MethodGet, "/v1/snippets", query, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !a.ok() {
			return report(stdout, stderr, "playbook "+op, a)
		}
		var list struct {
			Project  json.RawMessage   `json:"project,omitempty"`
			Snippets []json.RawMessage `json:"snippets"`
		}
		if json.Unmarshal(a.Body, &list) != nil || list.Snippets == nil {
			fmt.Fprintln(stderr, cliCopy("misc", "playbook.unreadable", "The saved Playbook list could not be read; nothing was changed."))
			return 1
		}
		filtered := make([]json.RawMessage, 0, len(list.Snippets))
		found := false
		for _, raw := range list.Snippets {
			var row struct{ ID, Scope string }
			if json.Unmarshal(raw, &row) != nil {
				return 1
			}
			if row.Scope == "playbook" {
				filtered = append(filtered, raw)
				found = found || row.ID == fs.Arg(0)
			}
		}
		if op == "list" {
			list.Snippets = filtered
			a.Body, _ = json.Marshal(list)
			return report(stdout, stderr, "playbook list", a)
		}
		if !found {
			fmt.Fprintln(stderr, cliCopy("misc", "playbook.not_found", "That id is not a saved Playbook; nothing was changed."))
			return 1
		}
	}
	method, path := http.MethodPost, "/v1/snippets"
	if op != "create" {
		path += "/" + url.PathEscape(fs.Arg(0))
		method = http.MethodPatch
		if op == "delete" {
			method = http.MethodDelete
		}
	}
	if *key == "" {
		*key = newKey("playbook")
	}
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", *key)
	var payload any
	if body != nil {
		payload = body
	}
	a, err := b.request(method, path, nil, payload, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, cliCopy("misc", "playbook.retry", "The result is uncertain. Retry with the same --key, action, id and JSON content; do not create a new key."))
		return 1
	}
	return report(stdout, stderr, "playbook "+op, a)
}
