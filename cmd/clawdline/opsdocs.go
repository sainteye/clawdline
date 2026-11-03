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

	"github.com/sainteye/clawdline/internal/contract"
	domain "github.com/sainteye/clawdline/internal/domain/opsdocs"
)

// `clawdline ops` is a Project's Ops documents list (docs/ops-documents.md):
// a Session reads the documents a Playbook names, adds a link, and reports
// what a link it was asked to read is. Like `clawdline memory` it asks this
// machine's daemon with the orchestrator token; a Session in a linked
// worktree reads the list of the repository it was made from.

// Exit codes: 0 done, 1 refused, 2 a usage mistake, 3 could not check.
const (
	opsExitOK      = 0
	opsExitRefused = 1
	opsExitUsage   = 2
	opsExitUnknown = 3
)

const opsUsage = `usage:
  clawdline ops list     [--session <terminal>] [--project <dir>] [--json]
  clawdline ops read     <id>... [--json]
  clawdline ops add-link <url> [--title <title>] [--session <terminal>] [--resolve] [--project <dir>]
  clawdline ops resolve  <id> --todo <todo id> (--title <title> | --needs-access --reason <what the person must do>)

Without --session, the Project is the current directory's Git top level; a
linked worktree is the Project it was made from. read prints a file's text, or
for a link its title, type, URL and status. --resolve asks the --session
Session to read the new link: it creates the to-do, which the person sends.`

func opsCopy(key, english string) string { return cliCopy("opsdocs", key, english) }

func opsCommand(args []string) {
	os.Exit(runOps(args, os.Stdout, os.Stderr, openMemoryDaemon))
}

func runOps(args []string, stdout, stderr io.Writer, open func() (memoryDaemon, error)) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stderr, opsCopy("usage", opsUsage))
		if len(args) == 0 {
			return opsExitUsage
		}
		return opsExitOK
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("ops "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	session := fs.String("session", "", opsCopy("flag_session", "the Session terminal whose Project is meant"))
	project := fs.String("project", "", opsCopy("flag_project", "the Project's directory (default: the current directory's Git top level)"))
	asJSON := fs.Bool("json", false, opsCopy("flag_json", "print the daemon's answer"))
	title := fs.String("title", "", opsCopy("flag_title", "the document's title"))
	resolve := fs.Bool("resolve", false, opsCopy("flag_resolve", "ask the --session Session to read the new link"))
	todo := fs.String("todo", "", opsCopy("flag_todo", "the resolve to-do's id"))
	needsAccess := fs.Bool("needs-access", false, opsCopy("flag_needs_access", "the link could not be opened"))
	reason := fs.String("reason", "", opsCopy("flag_reason", "what the person must do so the link can be read"))
	var positional []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return opsExitUsage
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	usage := func(msg string) int {
		fmt.Fprintf(stderr, opsCopy("usage_error", "clawdline ops %s: %s\n\n%s\n"), verb, msg, opsCopy("usage", opsUsage))
		return opsExitUsage
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	allow := func(names ...string) string {
		ok := map[string]bool{}
		for _, n := range names {
			ok[n] = true
		}
		for name := range given {
			if !ok[name] {
				return name
			}
		}
		return ""
	}
	switch verb {
	case "list":
		if len(positional) > 0 {
			return usage(opsCopy("unexpected_argument", "unexpected argument ") + positional[0])
		}
		if bad := allow("session", "project", "json"); bad != "" {
			return usage(opsCopy("flag_not_for", "this command does not take --") + bad)
		}
		if *session != "" && *project != "" {
			return usage(opsCopy("session_or_project", "give --session or --project, not both"))
		}
	case "read":
		if len(positional) == 0 {
			return usage(opsCopy("read_ids", "name at least one Ops document id"))
		}
		if len(positional) > domain.ReadIDsLimit {
			return usage(fmt.Sprintf(opsCopy("read_too_many", "read at most %d documents at a time"), domain.ReadIDsLimit))
		}
		if bad := allow("json"); bad != "" {
			return usage(opsCopy("flag_not_for", "this command does not take --") + bad)
		}
	case "add-link":
		if len(positional) != 1 {
			return usage(opsCopy("one_url", "give exactly one URL"))
		}
		if bad := allow("title", "session", "resolve", "project", "json"); bad != "" {
			return usage(opsCopy("flag_not_for", "this command does not take --") + bad)
		}
		if *resolve && *session == "" {
			return usage(opsCopy("resolve_needs_session", "--resolve needs --session, the Session that should read the link"))
		}
		if *session != "" && *project != "" {
			return usage(opsCopy("session_or_project", "give --session or --project, not both"))
		}
		if _, _, err := domain.ClassifyURL(positional[0]); err != nil {
			fmt.Fprintf(stderr, opsCopy("bad_url", "clawdline ops %s: refused: an Ops link is an http or https URL\n"), verb)
			return opsExitRefused
		}
	case "resolve":
		if len(positional) != 1 {
			return usage(opsCopy("one_id", "name exactly one Ops document id"))
		}
		if bad := allow("todo", "title", "needs-access", "reason", "json"); bad != "" {
			return usage(opsCopy("flag_not_for", "this command does not take --") + bad)
		}
		if *todo == "" || *needsAccess == (*title != "") || *needsAccess != (*reason != "") {
			return usage(opsCopy("resolve_shape", "give --todo and either --title, or --needs-access with --reason"))
		}
	default:
		return usage(opsCopy("unknown_command", "unknown command"))
	}
	d, err := open()
	if err != nil {
		fmt.Fprintf(stderr, opsCopy("operation_error", "clawdline ops %s: %v\n"), verb, err)
		return opsExitUnknown
	}
	call := func(method, path string, query url.Values, body any) (answer, int) {
		key := ""
		if method != http.MethodGet {
			key = newKey("ops")
		}
		a, err := d.request(method, path, query, body, key)
		if err != nil {
			fmt.Fprintf(stderr, opsCopy("operation_error", "clawdline ops %s: %v\n"), verb, err)
			return a, opsExitUnknown
		}
		if !a.ok() {
			code, message := a.refusal()
			fmt.Fprintf(stderr, opsCopy("http_refused", "clawdline ops %s: refused, %d %s: %s\n"), verb, a.Status, code, message)
			if a.Status >= 500 {
				return a, opsExitUnknown
			}
			return a, opsExitRefused
		}
		return a, opsExitOK
	}
	unreadable := func() int {
		fmt.Fprintf(stderr, opsCopy("unreadable", "clawdline ops %s: the daemon's answer was not readable\n"), verb)
		return opsExitUnknown
	}
	// place is the place id the list is read and written under.
	place := func() (string, int) {
		if *session != "" {
			a, code := call(http.MethodGet, "/v1/ops", url.Values{"session": {*session}}, nil)
			if code != opsExitOK {
				return "", code
			}
			var list contract.OpsList
			if json.Unmarshal(a.Body, &list) != nil || list.Place == "" {
				return "", unreadable()
			}
			return list.Place, opsExitOK
		}
		dir, err := memoryProjectDir(*project)
		if err != nil {
			fmt.Fprintf(stderr, opsCopy("operation_error", "clawdline ops %s: %v\n"), verb, err)
			return "", opsExitUnknown
		}
		id, err := memoryPlaceFor(d, dir)
		if err != nil {
			fmt.Fprintf(stderr, opsCopy("operation_error", "clawdline ops %s: %v\n"), verb, err)
			return "", opsExitUnknown
		}
		return id, opsExitOK
	}
	switch verb {
	case "list":
		var a answer
		code := opsExitOK
		if *session != "" {
			a, code = call(http.MethodGet, "/v1/ops", url.Values{"session": {*session}}, nil)
		} else {
			id, c := place()
			if c != opsExitOK {
				return c
			}
			a, code = call(http.MethodGet, "/v1/projects/"+url.PathEscape(id)+"/ops", nil, nil)
		}
		if code != opsExitOK {
			return code
		}
		if *asJSON {
			stdout.Write(indentJSON(a.Body))
			return opsExitOK
		}
		var list contract.OpsList
		if json.Unmarshal(a.Body, &list) != nil {
			return unreadable()
		}
		printOpsList(stdout, list)
		return opsExitOK
	case "read":
		status := opsExitOK
		for i, id := range positional {
			a, code := call(http.MethodGet, "/v1/ops/"+url.PathEscape(id), nil, nil)
			if code != opsExitOK {
				status = max(status, code)
				continue
			}
			if *asJSON {
				stdout.Write(indentJSON(a.Body))
				continue
			}
			var read contract.OpsRead
			if json.Unmarshal(a.Body, &read) != nil {
				status = max(status, unreadable())
				continue
			}
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			printOpsRead(stdout, read)
		}
		return status
	case "add-link":
		id, code := place()
		if code != opsExitOK {
			return code
		}
		a, code := call(http.MethodPost, "/v1/projects/"+url.PathEscape(id)+"/ops", nil,
			map[string]string{"kind": domain.KindLink, "url": positional[0], "title": *title})
		if code != opsExitOK {
			return code
		}
		var created contract.OpsEntryAnswer
		if json.Unmarshal(a.Body, &created) != nil {
			return unreadable()
		}
		fmt.Fprintf(stdout, opsCopy("added", "Added %s: %s (%s)\n"), created.Entry.ID, created.Entry.Title, created.Entry.Type)
		if !*resolve {
			return opsExitOK
		}
		a, code = call(http.MethodPost, "/v1/projects/"+url.PathEscape(id)+"/ops/"+url.PathEscape(created.Entry.ID)+"/resolve-request",
			nil, map[string]string{"terminal": *session})
		if code != opsExitOK {
			return code
		}
		var requested contract.OpsResolveRequested
		if json.Unmarshal(a.Body, &requested) != nil {
			return unreadable()
		}
		fmt.Fprintf(stdout, opsCopy("resolve_created", "Created to-do %s in Session %s. It is not sent: the person sends it from the Ops panel or the Session's to-dos.\n"),
			requested.Todo.ID, *session)
		return opsExitOK
	case "resolve":
		body := map[string]any{"todo": *todo}
		if *needsAccess {
			body["needs_access"], body["reason"] = true, *reason
		} else {
			body["title"] = *title
		}
		a, code := call(http.MethodPost, "/v1/ops/"+url.PathEscape(positional[0])+"/resolve", nil, body)
		if code != opsExitOK {
			return code
		}
		if *asJSON {
			stdout.Write(indentJSON(a.Body))
			return opsExitOK
		}
		var resolved contract.OpsEntryAnswer
		if json.Unmarshal(a.Body, &resolved) != nil {
			return unreadable()
		}
		fmt.Fprintf(stdout, opsCopy("resolved", "Recorded %s: %s (%s)\n"), resolved.Entry.ID, resolved.Entry.Title, resolved.Entry.Status)
		return opsExitOK
	}
	return opsExitUsage
}

func printOpsList(w io.Writer, list contract.OpsList) {
	if len(list.Entries) == 0 {
		fmt.Fprintln(w, opsCopy("empty", "No Ops documents in this Project yet."))
		return
	}
	names := map[string]string{"": opsCopy("no_collection", "(no collection)")}
	order := []string{""}
	for _, c := range list.Collections {
		names[c.ID] = c.Name
		order = append(order, c.ID)
	}
	for _, c := range order {
		header := false
		for _, e := range list.Entries {
			if e.Collection != c {
				continue
			}
			if !header {
				fmt.Fprintf(w, "%s\n", names[c])
				header = true
			}
			where := e.Path
			if e.Kind == domain.KindLink {
				where = e.URL
			}
			fmt.Fprintf(w, "  %s  %s  [%s, %s]  %s\n", e.ID, e.Title, e.Type, e.Status, where)
		}
	}
}

func printOpsRead(w io.Writer, read contract.OpsRead) {
	e := read.Entry
	fmt.Fprintf(w, "# %s\n", e.Title)
	fmt.Fprintf(w, opsCopy("read_meta", "id: %s  type: %s  status: %s\n"), e.ID, e.Type, e.Status)
	if e.Kind == domain.KindLink {
		fmt.Fprintf(w, "URL: %s\n", e.URL)
		if e.AccessNote != "" {
			fmt.Fprintf(w, opsCopy("access_note", "Needs access: %s\n"), e.AccessNote)
		}
		fmt.Fprintln(w, opsCopy("link_hint", "Read it with a connector you have (Google Drive, Notion, web fetch). If you cannot read it, ask the person to grant access."))
		return
	}
	fmt.Fprintf(w, opsCopy("path", "path: %s\n"), e.Path)
	if read.Version == "" {
		if e.AccessNote != "" {
			fmt.Fprintln(w, e.AccessNote)
		} else {
			fmt.Fprintln(w, opsCopy("no_text", "This file is not text this command prints."))
		}
		return
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, read.Content)
	if !strings.HasSuffix(read.Content, "\n") {
		fmt.Fprintln(w)
	}
}
