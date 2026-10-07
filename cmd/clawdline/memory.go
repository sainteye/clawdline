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
  clawdline memory list   [--group <slug>] [--project <dir>] [--json]
  clawdline memory show   <name> [--project <dir>] [--json]
  clawdline memory add    --name <name> --description <one line> --type user|feedback|project|reference
                          [--group <slug>] [--body-file <file>] [--project <dir>]     (the body is read from stdin without --body-file)
  clawdline memory update --name <name> --description <one line> --type <type> [--group <slug>] [--body-file <file>] [--project <dir>]
  clawdline memory forget <name> [--project <dir>]
  clawdline memory group  set <slug> --description <when to read it> [--project <dir>]
  clawdline memory group  list [--project <dir>] [--json]
  clawdline memory import --from-claude [--apply] [--project <dir>]

An entry without --group is resident: the index every launched session is given
lists it by name. An entry in a group is listed by ` + "`clawdline memory list --group <slug>`" + `;
the index gives the group one line. --project defaults to the current directory's
Git top level. A linked worktree is the Project it was made from, so a dispatched
child reads its Project's memory.`

func memoryCopy(key, english string) string { return cliCopy("memory", key, english) }

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
		fmt.Fprintln(stderr, memoryCopy("usage", memoryUsage))
		if len(args) == 0 {
			return memoryExitUsage
		}
		return memoryExitOK
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("memory "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", "", memoryCopy("flag_project", "the Project's directory (default: the current directory's Git top level)"))
	asJSON := fs.Bool("json", false, memoryCopy("flag_json", "print the daemon's answer"))
	name := fs.String("name", "", memoryCopy("flag_name", "the entry's name: lowercase letters, digits and single hyphens"))
	description := fs.String("description", "", memoryCopy("flag_description", "one line that says what the entry is for"))
	kind := fs.String("type", "", memoryCopy("flag_type", "user, feedback, project or reference"))
	bodyFile := fs.String("body-file", "", memoryCopy("flag_body_file", "the file the body is read from (default: stdin)"))
	fromClaude := fs.Bool("from-claude", false, memoryCopy("flag_from_claude", "import Claude Code's auto-memory for this Project"))
	apply := fs.Bool("apply", false, memoryCopy("flag_apply", "copy the entries the import listed"))
	group := fs.String("group", "", memoryCopy("flag_group", "the group an entry is filed under, or the group to list"))
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
	groupGiven := false
	fs.Visit(func(f *flag.Flag) { groupGiven = groupGiven || f.Name == "group" })
	usage := func(msg string) int {
		fmt.Fprintf(stderr, memoryCopy("usage_error", "clawdline memory %s: %s\n\n%s\n"), verb, msg, memoryCopy("usage", memoryUsage))
		return memoryExitUsage
	}
	var groupVerb string
	switch verb {
	case "list", "add", "update", "import":
		if len(positional) > 0 {
			return usage(memoryCopy("unexpected_argument", "unexpected argument ") + positional[0])
		}
	case "group":
		switch {
		case len(positional) == 1 && positional[0] == "list":
		case len(positional) == 2 && positional[0] == "set":
			*group = positional[1]
		default:
			return usage(memoryCopy("group_usage", "use `group set <slug> --description <one line>` or `group list`"))
		}
		groupVerb = positional[0]
	case "show", "forget":
		if len(positional) == 1 && *name == "" {
			*name = positional[0]
		} else if len(positional) > 0 || *name == "" {
			return usage(memoryCopy("one_name", "name exactly one entry"))
		}
	default:
		return usage(memoryCopy("unknown_command", "unknown command"))
	}
	if verb == "import" && !*fromClaude {
		return usage(memoryCopy("import_source_required", "--from-claude is the one source an import reads"))
	}
	if verb != "import" && *apply {
		return usage(memoryCopy("apply_for_import", "--apply is for import"))
	}
	if groupGiven && verb != "list" && verb != "add" && verb != "update" {
		return usage(memoryCopy("group_flag_for", "--group is for list, add and update"))
	}
	if *group != "" && !memory.ValidName(*group) {
		fmt.Fprintf(stderr, memoryCopy("group_refused", "clawdline memory %s: refused: %q is not a group (lowercase letters, digits and single hyphens)\n"), verb, *group)
		return memoryExitRefused
	}
	if groupVerb == "set" {
		if err := memory.ValidateGroup(*group, *description); err != nil {
			fmt.Fprintf(stderr, memoryCopy("operation_refused", "clawdline memory %s: refused: %v\n"), verb, err)
			return memoryExitRefused
		}
	}
	var entry memory.Entry
	if verb == "add" || verb == "update" {
		body, err := readMemoryBody(*bodyFile, stdin)
		if err != nil {
			fmt.Fprintf(stderr, memoryCopy("operation_error", "clawdline memory %s: %v\n"), verb, err)
			return memoryExitUsage
		}
		entry = memory.Entry{Name: *name, Description: *description, Type: memory.Type(*kind), Group: *group, Body: body}
		// Refused here, before the daemon is asked, with the store's words.
		if err := memory.Validate(entry); err != nil {
			fmt.Fprintf(stderr, memoryCopy("operation_refused", "clawdline memory %s: refused: %v\n"), verb, err)
			return memoryExitRefused
		}
	} else if (verb == "show" || verb == "forget") && !memory.ValidName(*name) {
		fmt.Fprintf(stderr, memoryCopy("name_refused", "clawdline memory %s: refused: %q is not an entry name (lowercase letters, digits and single hyphens)\n"), verb, *name)
		return memoryExitRefused
	}
	dir, err := memoryProjectDir(*project)
	if err != nil {
		fmt.Fprintf(stderr, memoryCopy("operation_error", "clawdline memory %s: %v\n"), verb, err)
		return memoryExitUnknown
	}
	d, err := open()
	if err != nil {
		fmt.Fprintf(stderr, memoryCopy("operation_error", "clawdline memory %s: %v\n"), verb, err)
		return memoryExitUnknown
	}
	place, err := memoryPlaceFor(d, dir)
	if err != nil {
		fmt.Fprintf(stderr, memoryCopy("operation_error", "clawdline memory %s: %v\n"), verb, err)
		return memoryExitUnknown
	}
	base := "/v1/projects/" + url.PathEscape(place) + "/memory"
	call := func(method, path string, body any) (answer, int) {
		a, err := d.request(method, path, nil, body, "")
		if err != nil {
			fmt.Fprintf(stderr, memoryCopy("operation_error", "clawdline memory %s: %v\n"), verb, err)
			return a, memoryExitUnknown
		}
		if !a.ok() {
			code, message := a.refusal()
			fmt.Fprintf(stderr, memoryCopy("http_refused", "clawdline memory %s: refused, %d %s: %s\n"), verb, a.Status, code, message)
			if a.Status >= 500 {
				return a, memoryExitUnknown
			}
			return a, memoryExitRefused
		}
		return a, memoryExitOK
	}
	wire := func(e memory.Entry) contract.ProjectMemoryEntry {
		return contract.ProjectMemoryEntry{Name: e.Name, Description: e.Description,
			Type: contract.ProjectMemoryType(e.Type), Group: e.Group, Body: e.Body}
	}
	fetch := func() (contract.ProjectMemoryList, []byte, int) {
		a, code := call(http.MethodGet, base, nil)
		if code != memoryExitOK {
			return contract.ProjectMemoryList{}, nil, code
		}
		var list contract.ProjectMemoryList
		if json.Unmarshal(a.Body, &list) != nil {
			fmt.Fprintf(stderr, memoryCopy("list_unreadable_verb", "clawdline memory %s: the daemon's answer was not readable\n"), verb)
			return list, nil, memoryExitUnknown
		}
		return list, a.Body, memoryExitOK
	}
	switch verb {
	case "list":
		list, raw, code := fetch()
		if code != memoryExitOK {
			return code
		}
		if *group != "" {
			return printMemoryGroup(stdout, stderr, list, *group, *asJSON)
		}
		if *asJSON {
			stdout.Write(indentJSON(raw))
			return memoryExitOK
		}
		if len(list.Entries) == 0 {
			fmt.Fprintf(stdout, memoryCopy("empty_list", "No shared memory for %s yet. Record one with `clawdline memory add`.\n"), dir)
			return memoryExitOK
		}
		var residents []contract.ProjectMemorySummary
		for _, e := range list.Entries {
			if e.Group == "" {
				residents = append(residents, e)
			}
		}
		fmt.Fprintf(stdout, memoryCopy("list_header_grouped", "%d entries for %s: %d resident, %d in groups.\n\n"),
			len(list.Entries), dir, len(residents), len(list.Entries)-len(residents))
		printMemoryEntries(stdout, residents)
		if len(list.Groups) > 0 {
			fmt.Fprintln(stdout, memoryCopy("groups_heading", "\nGroups (list one with `clawdline memory list --group <slug>`):"))
			for _, g := range list.Groups {
				fmt.Fprintf(stdout, memoryCopy("group_line", "  %s — %s (%d entries)\n"), g.Slug, g.Description, g.Entries)
			}
		}
		if list.IndexCut {
			fmt.Fprintf(stdout, memoryCopy("index_cut_resident", "\nThe index a launched session is given is bounded at %d bytes and the resident entries alone pass it: it names fewer resident entries than this list. Move some into a group with `clawdline memory update --group <slug>`.\n"), memory.IndexByteLimit)
		}
		return memoryExitOK
	case "group":
		if groupVerb == "set" {
			a, code := call(http.MethodPut, memoryGroupPath(place, *group), contract.ProjectMemoryGroupSet{Description: *description})
			if code != memoryExitOK {
				return code
			}
			return printMemoryWrite(stdout, stderr, verb, a)
		}
		list, raw, code := fetch()
		if code != memoryExitOK {
			return code
		}
		if *asJSON {
			var only struct {
				Groups json.RawMessage `json:"groups"`
			}
			_ = json.Unmarshal(raw, &only)
			stdout.Write(indentJSON(only.Groups))
			return memoryExitOK
		}
		if len(list.Groups) == 0 {
			fmt.Fprintln(stdout, memoryCopy("no_groups", "No groups yet. Create one with `clawdline memory group set <slug> --description <when to read it>`."))
			return memoryExitOK
		}
		for _, g := range list.Groups {
			fmt.Fprintf(stdout, memoryCopy("group_line", "  %s — %s (%d entries)\n"), g.Slug, g.Description, g.Entries)
		}
		return memoryExitOK
	case "show":
		a, code := call(http.MethodGet, base+"/"+url.PathEscape(*name), nil)
		if code != memoryExitOK {
			return code
		}
		var e contract.ProjectMemoryEntry
		if json.Unmarshal(a.Body, &e) != nil {
			fmt.Fprintln(stderr, memoryCopy("show_unreadable", "clawdline memory show: the daemon's answer was not readable"))
			return memoryExitUnknown
		}
		if *asJSON {
			stdout.Write(indentJSON(a.Body))
			return memoryExitOK
		}
		io.WriteString(stdout, memory.Format(memory.Entry{Name: e.Name, Description: e.Description,
			Type: memory.Type(e.Type), Group: e.Group, Body: e.Body}))
		return memoryExitOK
	case "add", "update":
		method, path := http.MethodPost, base
		if verb == "update" {
			method, path = http.MethodPut, base+"/"+url.PathEscape(entry.Name)
		}
		// An update without --group keeps the entry where it is; only
		// `--group ""` makes it resident again.
		if verb == "update" && !groupGiven {
			a, code := call(http.MethodGet, path, nil)
			if code != memoryExitOK {
				return code
			}
			var current contract.ProjectMemoryEntry
			if json.Unmarshal(a.Body, &current) != nil {
				fmt.Fprintln(stderr, memoryCopy("show_unreadable_update", "clawdline memory update: the daemon's answer was not readable"))
				return memoryExitUnknown
			}
			entry.Group = current.Group
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
		fmt.Fprintf(stderr, memoryCopy("no_home", "clawdline memory import: no home directory: %v\n"), err)
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
		fmt.Fprintf(stderr, memoryCopy("no_claude_memory", "clawdline memory import: Claude Code has no auto-memory for %s (looked in %s)\n"),
			dir, strings.Join(memory.ClaudeSources(home, dir, canonical), ", "))
		return memoryExitRefused
	}
	plan, err := memory.ReadClaude(source)
	if err != nil {
		fmt.Fprintf(stderr, memoryCopy("source_unreadable", "clawdline memory import: %s could not be read: %v\n"), source, err)
		return memoryExitUnknown
	}
	list, _, code := fetch()
	if code != memoryExitOK {
		return code
	}
	have := map[string]bool{}
	for _, e := range list.Entries {
		have[e.Name] = true
	}
	haveGroup := map[string]bool{}
	for _, g := range list.Groups {
		haveGroup[g.Slug] = true
	}
	var todo, already []memory.Entry
	for _, e := range plan.Entries {
		if have[e.Name] {
			already = append(already, e)
		} else {
			todo = append(todo, e)
		}
	}
	fmt.Fprintf(stdout, memoryCopy("from_source", "From %s (read only; nothing there is changed):\n\n"), source)
	if len(plan.Groups) > 0 {
		fmt.Fprintf(stdout, memoryCopy("import_groups", "%d groups, from the sub-indexes MEMORY.md links:\n"), len(plan.Groups))
		for _, g := range plan.Groups {
			state := ""
			if haveGroup[g.Slug] {
				state = memoryCopy("group_exists", " (already in the store; its description is kept)")
			}
			fmt.Fprintf(stdout, memoryCopy("import_group_line", "  %s — %d entries, from %s — %s%s\n"), g.Slug, len(g.Members), g.File, g.Description, state)
		}
		if len(plan.Groups) > memory.GroupCountLimit {
			fmt.Fprintf(stdout, memoryCopy("too_many_groups", "  A Project holds at most %d groups; the import stops at the one past it.\n"), memory.GroupCountLimit)
		}
		fmt.Fprintln(stdout)
	}
	fmt.Fprintf(stdout, memoryCopy("import_count", "%d to import:\n"), len(todo))
	printImportEntries(stdout, todo, plan.Groups)
	if len(plan.Unlinked) > 0 {
		fmt.Fprintf(stdout, memoryCopy("unlinked_count", "%d not linked from MEMORY.md, imported as resident:\n"), len(plan.Unlinked))
		for _, name := range plan.Unlinked {
			fmt.Fprintf(stdout, "  %s\n", name)
		}
	}
	fmt.Fprintf(stdout, memoryCopy("already_count", "%d already in the store, skipped:\n"), len(already))
	for _, e := range already {
		fmt.Fprintf(stdout, "  %s\n", e.Name)
	}
	fmt.Fprintf(stdout, memoryCopy("skipped_count", "%d files that are not entries, skipped:\n"), len(plan.Skipped))
	for _, s := range plan.Skipped {
		fmt.Fprintf(stdout, "  %s: %s\n", s.File, s.Reason)
	}
	if !*apply {
		if len(todo) > 0 || len(plan.Groups) > 0 {
			fmt.Fprintln(stdout, memoryCopy("plan_only", "\nNothing was imported. Run again with --apply to copy these entries."))
		}
		return memoryExitOK
	}
	// Groups first: an entry naming a group nobody set is refused.
	for _, g := range plan.Groups {
		if haveGroup[g.Slug] {
			continue
		}
		if _, code := call(http.MethodPut, memoryGroupPath(place, g.Slug), contract.ProjectMemoryGroupSet{Description: g.Description}); code != memoryExitOK {
			return code
		}
	}
	imported := 0
	for _, e := range todo {
		a, err := d.request(http.MethodPost, base, nil, wire(e), "")
		if err != nil {
			fmt.Fprintf(stderr, memoryCopy("import_error_partial", "clawdline memory import: %s: %v — %d imported before it; run the import again to see what is left\n"), e.Name, err, imported)
			return memoryExitUnknown
		}
		var out contract.ProjectMemoryWriteAnswer
		switch code, message := a.refusal(); {
		case a.ok() && json.Unmarshal(a.Body, &out) == nil && out.Outcome == contract.ProjectMemoryOutcomeCreated:
			imported++
		case a.ok():
			fmt.Fprintf(stdout, memoryCopy("unchanged_entry", "  %s was already there with the same content; skipped\n"), e.Name)
		case code == "memory_entry_exists":
			fmt.Fprintf(stdout, memoryCopy("raced_entry", "  %s was added by somebody else meanwhile; skipped\n"), e.Name)
		default:
			fmt.Fprintf(stderr, memoryCopy("import_refused_partial", "clawdline memory import: %s refused, %d %s: %s — %d imported before it\n"),
				e.Name, a.Status, code, message, imported)
			return memoryExitRefused
		}
	}
	fmt.Fprintf(stdout, memoryCopy("imported_count", "\nImported %d entries.\n"), imported)
	return memoryExitOK
}

func memoryGroupPath(place, slug string) string {
	return "/v1/projects/" + url.PathEscape(place) + "/memory-groups/" + url.PathEscape(slug)
}

// printMemoryEntries is entries in index order, under a line per type.
func printMemoryEntries(stdout io.Writer, entries []contract.ProjectMemorySummary) {
	var kind contract.ProjectMemoryType
	for _, e := range entries {
		if e.Type != kind {
			kind = e.Type
			fmt.Fprintf(stdout, "%s\n", kind)
		}
		fmt.Fprintf(stdout, "  %s — %s\n", e.Name, e.Description)
	}
}

// printMemoryGroup is `list --group <slug>`: the group's line, then its
// entries. A group the store does not know is refused, not an empty list.
func printMemoryGroup(stdout, stderr io.Writer, list contract.ProjectMemoryList, slug string, asJSON bool) int {
	var group *contract.ProjectMemoryGroup
	for i := range list.Groups {
		if list.Groups[i].Slug == slug {
			group = &list.Groups[i]
		}
	}
	if group == nil {
		fmt.Fprintf(stderr, memoryCopy("no_such_group", "clawdline memory list: refused: there is no group %q; `clawdline memory group list` lists them\n"), slug)
		return memoryExitRefused
	}
	members := []contract.ProjectMemorySummary{}
	for _, e := range list.Entries {
		if e.Group == slug {
			members = append(members, e)
		}
	}
	if asJSON {
		data, _ := json.Marshal(struct {
			Group   contract.ProjectMemoryGroup     `json:"group"`
			Entries []contract.ProjectMemorySummary `json:"entries"`
		}{*group, members})
		stdout.Write(indentJSON(data))
		return memoryExitOK
	}
	fmt.Fprintf(stdout, memoryCopy("group_header", "%s — %s (%d entries):\n\n"), group.Slug, group.Description, len(members))
	printMemoryEntries(stdout, members)
	return memoryExitOK
}

// printImportEntries is the entries an import would copy: the resident ones
// first, then each group's.
func printImportEntries(stdout io.Writer, entries []memory.Entry, groups []memory.ClaudeGroup) {
	line := func(e memory.Entry) { fmt.Fprintf(stdout, "  %s (%s) — %s\n", e.Name, e.Type, e.Description) }
	resident := 0
	for _, e := range entries {
		if e.Group == "" {
			resident++
		}
	}
	if resident > 0 {
		fmt.Fprintf(stdout, memoryCopy("import_resident", " resident (%d):\n"), resident)
		for _, e := range entries {
			if e.Group == "" {
				line(e)
			}
		}
	}
	for _, g := range groups {
		n := 0
		for _, e := range entries {
			if e.Group == g.Slug {
				n++
			}
		}
		if n == 0 {
			continue
		}
		fmt.Fprintf(stdout, memoryCopy("import_in_group", " in group %s (%d):\n"), g.Slug, n)
		for _, e := range entries {
			if e.Group == g.Slug {
				line(e)
			}
		}
	}
}

func printMemoryWrite(stdout, stderr io.Writer, verb string, a answer) int {
	var out contract.ProjectMemoryWriteAnswer
	if json.Unmarshal(a.Body, &out) != nil || out.Outcome == "" {
		fmt.Fprintf(stderr, memoryCopy("write_unreadable", "clawdline memory %s: the daemon's answer was not readable; run `clawdline memory list` to see what the store holds\n"), verb)
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
			return "", errors.New(memoryCopy("body_required", "give the body with --body-file <file>, or pipe it on stdin"))
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
		return "", fmt.Errorf(memoryCopy("not_directory", "%s is not a directory"), abs)
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
		return "", fmt.Errorf(memoryCopy("not_absolute", "%s is not an absolute path"), dir)
	}
	a, err := d.request(http.MethodGet, "/v1/places", nil, nil, "")
	if err != nil {
		return "", err
	}
	if !a.ok() {
		code, message := a.refusal()
		return "", fmt.Errorf(memoryCopy("projects_refused", "the Project list was refused, %d %s: %s"), a.Status, code, message)
	}
	var list contract.StartPlaceList
	if err := json.Unmarshal(a.Body, &list); err != nil {
		return "", fmt.Errorf(memoryCopy("projects_unreadable", "the daemon's Project list was not readable: %w"), err)
	}
	for _, p := range list.Places {
		if got, ok := projects.CanonicalProjectKey(p.Path); ok && got == want {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf(memoryCopy("project_not_listed", "this machine does not list %s as a Project; run `clawdline project add %s` first"), want, want)
}
