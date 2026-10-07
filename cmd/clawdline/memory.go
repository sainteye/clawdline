package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline memory` is a Project's shared memory, the one store Claude Code
// and Codex sessions both read and write (docs/project-memory.md). Every
// command is a request to this machine's daemon with the orchestrator token,
// as the other thin commands are: a Codex session's sandbox cannot write the
// daemon's state directory, but it reaches the daemon on 127.0.0.1.

// Exit codes: 0 done, 1 refused (a bad name, a duplicate, an unknown entry),
// 2 a usage mistake, 3 could not check (no daemon, an unreadable answer).
const (
	memoryExitOK      = 0
	memoryExitRefused = 1
	memoryExitUsage   = 2
	memoryExitUnknown = 3
)

const memoryUsage = `usage:
  clawdline memory list   [--project <dir>] [--json]
  clawdline memory show   <name> [--project <dir>] [--json]
  clawdline memory add    --name <name> --description <one line> --type user|feedback|project|reference
                          [--body-file <file>] [--project <dir>]     (the body is read from stdin without --body-file)
  clawdline memory update --name <name> --description <one line> --type <type> [--body-file <file>] [--project <dir>]
  clawdline memory forget <name> [--project <dir>]
  clawdline memory import --from-claude [--apply] [--project <dir>]

--project defaults to the current directory's Git top level. A linked worktree is
the Project it was made from, so a dispatched child reads its Project's memory.`

func memoryCommand(args []string) {
	os.Exit(runMemory(args, os.Stdin, os.Stdout, os.Stderr, openMemoryDaemon))
}

// memoryDaemon is the daemon a command talks to; a test hands one in.
type memoryDaemon interface {
	request(method, path string, query url.Values, body any, key string) (answer, error)
}

func openMemoryDaemon() (memoryDaemon, error) { return openBroker(0) }

func runMemory(args []string, stdin io.Reader, stdout, stderr io.Writer, open func() (memoryDaemon, error)) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stderr, memoryUsage)
		if len(args) == 0 {
			return memoryExitUsage
		}
		return memoryExitOK
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("memory "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", "", "the Project's directory (default: the current directory's Git top level)")
	asJSON := fs.Bool("json", false, "print the daemon's answer")
	name := fs.String("name", "", "the entry's name: lowercase letters, digits and single hyphens")
	description := fs.String("description", "", "one line that says what the entry is for")
	kind := fs.String("type", "", "user, feedback, project or reference")
	bodyFile := fs.String("body-file", "", "the file the body is read from (default: stdin)")
	fromClaude := fs.Bool("from-claude", false, "import Claude Code's auto-memory for this Project")
	apply := fs.Bool("apply", false, "copy the entries the import listed")
	// A name may come before the flags (`show foo --json`) as well as after.
	var positional []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return memoryExitUsage
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	usage := func(msg string) int {
		fmt.Fprintf(stderr, "clawdline memory %s: %s\n\n%s\n", verb, msg, memoryUsage)
		return memoryExitUsage
	}
	switch verb {
	case "list", "add", "update", "import":
		if len(positional) > 0 {
			return usage("unexpected argument " + positional[0])
		}
	case "show", "forget":
		if len(positional) == 1 && *name == "" {
			*name = positional[0]
		} else if len(positional) > 0 || *name == "" {
			return usage("name exactly one entry")
		}
	default:
		return usage("unknown command")
	}
	if verb == "import" && !*fromClaude {
		return usage("--from-claude is the one source an import reads")
	}
	if verb != "import" && *apply {
		return usage("--apply is for import")
	}
	var entry memory.Entry
	if verb == "add" || verb == "update" {
		body, err := readMemoryBody(*bodyFile, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "clawdline memory %s: %v\n", verb, err)
			return memoryExitUsage
		}
		entry = memory.Entry{Name: *name, Description: *description, Type: memory.Type(*kind), Body: body}
		// Refused here, before the daemon is asked, with the store's words.
		if err := memory.Validate(entry); err != nil {
			fmt.Fprintf(stderr, "clawdline memory %s: refused: %v\n", verb, err)
			return memoryExitRefused
		}
	} else if (verb == "show" || verb == "forget") && !memory.ValidName(*name) {
		fmt.Fprintf(stderr, "clawdline memory %s: refused: %q is not an entry name (lowercase letters, digits and single hyphens)\n", verb, *name)
		return memoryExitRefused
	}
	dir, err := memoryProjectDir(*project)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline memory %s: %v\n", verb, err)
		return memoryExitUnknown
	}
	d, err := open()
	if err != nil {
		fmt.Fprintf(stderr, "clawdline memory %s: %v\n", verb, err)
		return memoryExitUnknown
	}
	place, err := memoryPlaceFor(d, dir)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline memory %s: %v\n", verb, err)
		return memoryExitUnknown
	}
	base := "/v1/projects/" + url.PathEscape(place) + "/memory"
	call := func(method, path string, body any) (answer, int) {
		a, err := d.request(method, path, nil, body, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline memory %s: %v\n", verb, err)
			return a, memoryExitUnknown
		}
		if !a.ok() {
			code, message := a.refusal()
			fmt.Fprintf(stderr, "clawdline memory %s: refused, %d %s: %s\n", verb, a.Status, code, message)
			if a.Status >= 500 {
				return a, memoryExitUnknown
			}
			return a, memoryExitRefused
		}
		return a, memoryExitOK
	}
	wire := func(e memory.Entry) contract.ProjectMemoryEntry {
		return contract.ProjectMemoryEntry{Name: e.Name, Description: e.Description,
			Type: contract.ProjectMemoryType(e.Type), Body: e.Body}
	}
	switch verb {
	case "list":
		a, code := call(http.MethodGet, base, nil)
		if code != memoryExitOK {
			return code
		}
		var list contract.ProjectMemoryList
		if json.Unmarshal(a.Body, &list) != nil {
			fmt.Fprintln(stderr, "clawdline memory list: the daemon's answer was not readable")
			return memoryExitUnknown
		}
		if *asJSON {
			stdout.Write(indentJSON(a.Body))
			return memoryExitOK
		}
		if len(list.Entries) == 0 {
			fmt.Fprintf(stdout, "No shared memory for %s yet. Record one with `clawdline memory add`.\n", dir)
			return memoryExitOK
		}
		fmt.Fprintf(stdout, "%d entries for %s:\n\n", len(list.Entries), dir)
		var group contract.ProjectMemoryType
		for _, e := range list.Entries {
			if e.Type != group {
				group = e.Type
				fmt.Fprintf(stdout, "%s\n", group)
			}
			fmt.Fprintf(stdout, "  %s — %s\n", e.Name, e.Description)
		}
		if list.IndexCut {
			fmt.Fprintf(stdout, "\nThe index a launched session is given is cut at %d bytes; it names fewer entries than this list.\n", memory.IndexByteLimit)
		}
		return memoryExitOK
	case "show":
		a, code := call(http.MethodGet, base+"/"+url.PathEscape(*name), nil)
		if code != memoryExitOK {
			return code
		}
		var e contract.ProjectMemoryEntry
		if json.Unmarshal(a.Body, &e) != nil {
			fmt.Fprintln(stderr, "clawdline memory show: the daemon's answer was not readable")
			return memoryExitUnknown
		}
		if *asJSON {
			stdout.Write(indentJSON(a.Body))
			return memoryExitOK
		}
		io.WriteString(stdout, memory.Format(memory.Entry{Name: e.Name, Description: e.Description,
			Type: memory.Type(e.Type), Body: e.Body}))
		return memoryExitOK
	case "add", "update":
		method, path := http.MethodPost, base
		if verb == "update" {
			method, path = http.MethodPut, base+"/"+url.PathEscape(entry.Name)
		}
		a, code := call(method, path, wire(entry))
		if code != memoryExitOK {
			return code
		}
		return printMemoryWrite(stdout, stderr, verb, a)
	case "forget":
		a, code := call(http.MethodDelete, base+"/"+url.PathEscape(*name), nil)
		if code != memoryExitOK {
			return code
		}
		return printMemoryWrite(stdout, stderr, verb, a)
	}
	// import
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "clawdline memory import: no home directory: %v\n", err)
		return memoryExitUnknown
	}
	canonical, _ := projects.CanonicalProjectKey(dir)
	var source string
	for _, candidate := range memory.ClaudeSources(home, dir, canonical) {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			source = candidate
			break
		}
	}
	if source == "" {
		fmt.Fprintf(stderr, "clawdline memory import: Claude Code has no auto-memory for %s (looked in %s)\n",
			dir, strings.Join(memory.ClaudeSources(home, dir, canonical), ", "))
		return memoryExitRefused
	}
	entries, skipped, err := memory.ReadClaude(source)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline memory import: %s could not be read: %v\n", source, err)
		return memoryExitUnknown
	}
	a, code := call(http.MethodGet, base, nil)
	if code != memoryExitOK {
		return code
	}
	var list contract.ProjectMemoryList
	if json.Unmarshal(a.Body, &list) != nil {
		fmt.Fprintln(stderr, "clawdline memory import: the daemon's answer was not readable")
		return memoryExitUnknown
	}
	have := map[string]bool{}
	for _, e := range list.Entries {
		have[e.Name] = true
	}
	var todo, already []memory.Entry
	for _, e := range entries {
		if have[e.Name] {
			already = append(already, e)
		} else {
			todo = append(todo, e)
		}
	}
	fmt.Fprintf(stdout, "From %s (read only; nothing there is changed):\n\n", source)
	fmt.Fprintf(stdout, "%d to import:\n", len(todo))
	for _, e := range todo {
		fmt.Fprintf(stdout, "  %s (%s) — %s\n", e.Name, e.Type, e.Description)
	}
	fmt.Fprintf(stdout, "%d already in the store, skipped:\n", len(already))
	for _, e := range already {
		fmt.Fprintf(stdout, "  %s\n", e.Name)
	}
	fmt.Fprintf(stdout, "%d files that are not entries, skipped:\n", len(skipped))
	for _, s := range skipped {
		fmt.Fprintf(stdout, "  %s: %s\n", s.File, s.Reason)
	}
	if !*apply {
		if len(todo) > 0 {
			fmt.Fprintln(stdout, "\nNothing was imported. Run again with --apply to copy these entries.")
		}
		return memoryExitOK
	}
	imported := 0
	for _, e := range todo {
		a, err := d.request(http.MethodPost, base, nil, wire(e), "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline memory import: %s: %v — %d imported before it; run the import again to see what is left\n", e.Name, err, imported)
			return memoryExitUnknown
		}
		var out contract.ProjectMemoryWriteAnswer
		switch code, message := a.refusal(); {
		case a.ok() && json.Unmarshal(a.Body, &out) == nil && out.Outcome == contract.ProjectMemoryOutcomeCreated:
			imported++
		case a.ok():
			fmt.Fprintf(stdout, "  %s was already there with the same content; skipped\n", e.Name)
		case code == "memory_entry_exists":
			fmt.Fprintf(stdout, "  %s was added by somebody else meanwhile; skipped\n", e.Name)
		default:
			fmt.Fprintf(stderr, "clawdline memory import: %s refused, %d %s: %s — %d imported before it\n",
				e.Name, a.Status, code, message, imported)
			return memoryExitRefused
		}
	}
	fmt.Fprintf(stdout, "\nImported %d entries.\n", imported)
	return memoryExitOK
}

func printMemoryWrite(stdout, stderr io.Writer, verb string, a answer) int {
	var out contract.ProjectMemoryWriteAnswer
	if json.Unmarshal(a.Body, &out) != nil || out.Outcome == "" {
		fmt.Fprintf(stderr, "clawdline memory %s: the daemon's answer was not readable; run `clawdline memory list` to see what the store holds\n", verb)
		return memoryExitUnknown
	}
	fmt.Fprintf(stdout, "%s: %s\n", out.Name, out.Outcome)
	return memoryExitOK
}

// readMemoryBody is the body from a file, or from stdin when stdin is not a
// terminal; at most one byte past the store's bound is read, so an oversize
// body is refused by the store's own check.
func readMemoryBody(path string, stdin io.Reader) (string, error) {
	var r io.Reader = stdin
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		r = f
	} else if f, ok := stdin.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("give the body with --body-file <file>, or pipe it on stdin")
		}
	}
	data, err := io.ReadAll(io.LimitReader(r, memory.EntryBodyLimit+1))
	return string(data), err
}

// memoryProjectDir is --project, or the current directory, at its Git top
// level; a directory outside Git is a Project of its own.
func memoryProjectDir(given string) (string, error) {
	dir := given
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	if top, err := gitTopLevel(abs); err == nil {
		return top, nil
	}
	return abs, nil
}

// memoryPlaceFor is the daemon's place id for dir's Project.
func memoryPlaceFor(d memoryDaemon, dir string) (string, error) {
	want, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		return "", fmt.Errorf("%s is not an absolute path", dir)
	}
	a, err := d.request(http.MethodGet, "/v1/places", nil, nil, "")
	if err != nil {
		return "", err
	}
	if !a.ok() {
		code, message := a.refusal()
		return "", fmt.Errorf("the Project list was refused, %d %s: %s", a.Status, code, message)
	}
	var list contract.StartPlaceList
	if err := json.Unmarshal(a.Body, &list); err != nil {
		return "", fmt.Errorf("the daemon's Project list was not readable: %w", err)
	}
	for _, p := range list.Places {
		if got, ok := projects.CanonicalProjectKey(p.Path); ok && got == want {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("this machine does not list %s as a Project; run `clawdline project add %s` first", want, want)
}
