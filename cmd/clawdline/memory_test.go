package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// fakeMemoryDaemon answers the place list and the memory routes from a real
// store in a temporary state directory, and counts what it was asked.
type fakeMemoryDaemon struct {
	t       *testing.T
	project string
	store   *memory.Store
	calls   []string
	down    bool
}

func (f *fakeMemoryDaemon) request(method, path string, _ url.Values, body any, _ string) (answer, error) {
	f.calls = append(f.calls, method+" "+path)
	if f.down {
		return answer{}, errors.New("the daemon at http://127.0.0.1:1 did not answer")
	}
	reply := func(status int, v any) (answer, error) {
		data, _ := json.Marshal(v)
		return answer{Status: status, Body: data}, nil
	}
	refuse := func(status int, code, detail string) (answer, error) {
		return reply(status, contract.Refusal{Error: code, Detail: detail})
	}
	if path == "/v1/places" {
		return reply(200, contract.StartPlaceList{Places: []contract.StartPlace{{ID: "place-1", Path: f.project}}})
	}
	if slug, ok := strings.CutPrefix(path, "/v1/projects/place-1/memory-groups/"); ok && method == http.MethodPut {
		key, _ := memory.KeyFor(f.project)
		outcome, err := f.store.SetGroup(key, slug, body.(contract.ProjectMemoryGroupSet).Description)
		if err != nil {
			return refuse(400, "memory_entry_invalid", err.Error())
		}
		return reply(200, contract.ProjectMemoryWriteAnswer{Name: slug, Outcome: contract.ProjectMemoryOutcome(outcome)})
	}
	rest, ok := strings.CutPrefix(path, "/v1/projects/place-1/memory")
	if !ok {
		return refuse(404, "not_found", path)
	}
	key, _ := memory.KeyFor(f.project)
	name := strings.TrimPrefix(rest, "/")
	storeRefusal := func(err error) (answer, error) {
		switch {
		case errors.Is(err, memory.ErrDuplicate):
			return refuse(409, "memory_entry_exists", err.Error())
		case errors.Is(err, memory.ErrNotFound):
			return refuse(404, "memory_entry_not_found", err.Error())
		case errors.Is(err, memory.ErrInvalid):
			return refuse(400, "memory_entry_invalid", err.Error())
		case errors.Is(err, memory.ErrNoGroup):
			return refuse(400, "memory_group_not_found", err.Error())
		}
		return refuse(500, "memory_unreadable", err.Error())
	}
	entryOf := func() memory.Entry {
		e := body.(contract.ProjectMemoryEntry)
		return memory.Entry{Name: e.Name, Description: e.Description, Type: memory.Type(e.Type), Group: e.Group, Body: e.Body}
	}
	switch {
	case name == "" && method == http.MethodGet:
		l, err := f.store.Listing(key)
		if err != nil {
			return storeRefusal(err)
		}
		return reply(200, httptransport.MemoryListAnswer(key, l))
	case name == "" && method == http.MethodPost:
		outcome, err := f.store.Add(key, entryOf())
		if err != nil {
			return storeRefusal(err)
		}
		return reply(201, contract.ProjectMemoryWriteAnswer{Name: entryOf().Name, Outcome: contract.ProjectMemoryOutcome(outcome)})
	case method == http.MethodGet:
		e, err := f.store.Get(key, name)
		if err != nil {
			return storeRefusal(err)
		}
		return reply(200, contract.ProjectMemoryEntry{Name: e.Name, Description: e.Description, Type: contract.ProjectMemoryType(e.Type), Group: e.Group, Body: e.Body})
	case method == http.MethodPut:
		outcome, err := f.store.Update(key, entryOf())
		if err != nil {
			return storeRefusal(err)
		}
		return reply(200, contract.ProjectMemoryWriteAnswer{Name: name, Outcome: contract.ProjectMemoryOutcome(outcome)})
	case method == http.MethodDelete:
		outcome, err := f.store.Forget(key, name)
		if err != nil {
			return storeRefusal(err)
		}
		return reply(200, contract.ProjectMemoryWriteAnswer{Name: name, Outcome: contract.ProjectMemoryOutcome(outcome)})
	}
	return refuse(405, "method_not_allowed", method)
}

func newFakeMemoryDaemon(t *testing.T) *fakeMemoryDaemon {
	t.Helper()
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &fakeMemoryDaemon{t: t, project: project, store: memory.New(t.TempDir())}
}

func (f *fakeMemoryDaemon) run(stdin string, args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := runMemory(append(args, "--project", f.project), strings.NewReader(stdin), &out, &errs,
		func() (memoryDaemon, error) { return f, nil })
	return code, out.String(), errs.String()
}

func TestMemoryCommandsAddListShowUpdateForget(t *testing.T) {
	f := newFakeMemoryDaemon(t)
	add := []string{"add", "--name", "invented-lesson", "--description", "an invented lesson", "--type", "feedback"}
	if code, out, errs := f.run("Invented body.\n", add...); code != 0 || out != "invented-lesson: created\n" {
		t.Fatalf("add: %d %q %q", code, out, errs)
	}
	if code, out, _ := f.run("Invented body.\n", add...); code != 0 || out != "invented-lesson: unchanged\n" {
		t.Fatalf("the same add again: %d %q", code, out)
	}
	if code, _, errs := f.run("Another body.\n", add...); code != memoryExitRefused || !strings.Contains(errs, "memory_entry_exists") {
		t.Fatalf("a different add under the same name: %d %q", code, errs)
	}
	if code, out, _ := f.run("", "list"); code != 0 || !strings.Contains(out, "invented-lesson — an invented lesson") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, out, _ := f.run("", "show", "invented-lesson"); code != 0 || !strings.Contains(out, "metadata:\n  type: feedback\n---\n\nInvented body.\n") {
		t.Fatalf("show: %d %q", code, out)
	}
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("Changed body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	update := []string{"update", "--name", "invented-lesson", "--description", "changed", "--type", "project", "--body-file", body}
	if code, out, errs := f.run("", update...); code != 0 || out != "invented-lesson: updated\n" {
		t.Fatalf("update: %d %q %q", code, out, errs)
	}
	if code, out, _ := f.run("", "forget", "invented-lesson"); code != 0 || out != "invented-lesson: forgotten\n" {
		t.Fatalf("forget: %d %q", code, out)
	}
	if code, out, _ := f.run("", "list"); code != 0 || !strings.Contains(out, "No shared memory") {
		t.Fatalf("empty list: %d %q", code, out)
	}
}

func TestMemoryCommandsRefuseClearly(t *testing.T) {
	f := newFakeMemoryDaemon(t)
	for _, tc := range []struct {
		args []string
		code int
		says string
	}{
		{[]string{"add", "--name", "Bad Name", "--description", "d", "--type", "user"}, memoryExitRefused, "a name is lowercase"},
		{[]string{"add", "--name", "ok", "--description", "d", "--type", "opinion"}, memoryExitRefused, "the type is one of"},
		{[]string{"add", "--name", "ok", "--type", "user"}, memoryExitRefused, "a description is required"},
		{[]string{"show", "../etc"}, memoryExitRefused, "is not an entry name"},
		{[]string{"show"}, memoryExitUsage, "name exactly one entry"},
		{[]string{"rename", "x"}, memoryExitUsage, "unknown command"},
		{[]string{"import"}, memoryExitUsage, "--from-claude"},
		{[]string{"list", "--apply"}, memoryExitUsage, "--apply is for import"},
	} {
		f.calls = nil
		code, _, errs := f.run("body", tc.args...)
		if code != tc.code || !strings.Contains(errs, tc.says) {
			t.Errorf("%v: exit %d %q, want %d and %q", tc.args, code, errs, tc.code, tc.says)
		}
		if len(f.calls) != 0 {
			t.Errorf("%v asked the daemon %v before refusing", tc.args, f.calls)
		}
	}
	if code, _, errs := f.run("", "show", "never-written"); code != memoryExitRefused || !strings.Contains(errs, "memory_entry_not_found") {
		t.Fatalf("unknown entry: %d %q", code, errs)
	}
	if code, _, errs := f.run("", "forget", "never-written"); code != memoryExitRefused || !strings.Contains(errs, "memory_entry_not_found") {
		t.Fatalf("forget unknown: %d %q", code, errs)
	}
	// Could not check is not "no": a daemon that did not answer is exit 3.
	f.down = true
	if code, _, _ := f.run("", "list"); code != memoryExitUnknown {
		t.Fatalf("no daemon: %d", code)
	}
}

// The import lists, copies only with --apply, skips names already in the
// store, and never writes the source.
func TestMemoryImportFromClaudeDryRunThenApply(t *testing.T) {
	f := newFakeMemoryDaemon(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := filepath.Join(home, ".claude", "projects", projects.Slug(f.project), "memory")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"MEMORY.md":        "- [x](x.md) — index\n",
		"new-lesson.md":    "---\nname: new-lesson\ndescription: invented and new\nmetadata:\n  type: feedback\n---\n\nInvented.\n",
		"already-there.md": "---\nname: already-there\ndescription: invented and old\nmetadata:\n  type: project\n---\n\nInvented.\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	key, _ := memory.KeyFor(f.project)
	if _, err := f.store.Add(key, memory.Entry{Name: "already-there", Description: "the store's own", Type: memory.TypeUser, Body: "kept"}); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, source)
	code, out, errs := f.run("", "import", "--from-claude")
	if code != 0 || !strings.Contains(out, "1 to import:\n resident (1):\n  new-lesson (feedback)") ||
		!strings.Contains(out, "1 already in the store, skipped:\n  already-there") ||
		!strings.Contains(out, "MEMORY.md: the index") || !strings.Contains(out, "Nothing was imported") {
		t.Fatalf("dry run: %d %q %q", code, out, errs)
	}
	if _, err := f.store.Get(key, "new-lesson"); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("a dry run wrote the store: %v", err)
	}
	code, out, errs = f.run("", "import", "--from-claude", "--apply")
	if code != 0 || !strings.Contains(out, "Imported 1 entries.") {
		t.Fatalf("apply: %d %q %q", code, out, errs)
	}
	if got, err := f.store.Get(key, "new-lesson"); err != nil || got.Type != memory.TypeFeedback {
		t.Fatalf("imported: %+v %v", got, err)
	}
	if got, _ := f.store.Get(key, "already-there"); got.Description != "the store's own" {
		t.Fatalf("an entry already in the store was overwritten: %+v", got)
	}
	if after := snapshotDir(t, source); after != before {
		t.Fatalf("the import changed Claude Code's memory directory")
	}
	if code, out, _ := f.run("", "import", "--from-claude", "--apply"); code != 0 || !strings.Contains(out, "0 to import") {
		t.Fatalf("a second import: %d %q", code, out)
	}
}

func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		info, _ := e.Info()
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		b.WriteString(e.Name() + info.ModTime().String() + info.Mode().String() + string(data))
	}
	return b.String()
}

func TestMemoryCopyUsesChineseAndLaterLanguageFallback(t *testing.T) {
	oldLanguage := commandLanguage
	t.Cleanup(func() { commandLanguage = oldLanguage })
	f := newFakeMemoryDaemon(t)
	commandLanguage = "zh-Hant"
	if code, out, errs := f.run("", "list"); code != 0 || !strings.Contains(out, "尚無共用記憶") || errs != "" {
		t.Fatalf("Traditional Chinese list: %d %q %q", code, out, errs)
	}
	commandLanguage = "ja"
	if code, out, errs := f.run("", "list"); code != 0 || !strings.Contains(out, "No shared memory") || errs != "" {
		t.Fatalf("later language fallback: %d %q %q", code, out, errs)
	}
	bundle := cliCatalogGroups["memory"]
	if !validCLICatalog(bundle.english, bundle.locales["zh-Hant"], true) {
		t.Fatal("memory English and Traditional Chinese catalogs differ")
	}
}

// writeClaudeTree writes an invented Claude Code memory directory: MEMORY.md
// with resident entries and links to sub-indexes, each sub-index linking its
// entries, and one entry no file links.
func writeClaudeTree(t *testing.T, source string, groups map[string]int, resident int) {
	t.Helper()
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(source, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(name string) {
		write(name+".md", "---\nname: "+name+"\ndescription: an invented lesson called "+name+
			"\nmetadata:\n  type: feedback\n---\n\nInvented body.\n")
	}
	var root strings.Builder
	root.WriteString("Invented index.\n\n")
	for slug, n := range groups {
		var sub strings.Builder
		sub.WriteString("---\nname: index-" + slug + "\ndescription: Read before any invented " + slug + " work\nmetadata:\n  type: reference\n---\n\n")
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("%s-lesson-%02d", slug, i)
			entry(name)
			sub.WriteString("- [" + name + "](" + name + ".md) — hook\n")
		}
		write("index-"+slug+".md", sub.String())
		root.WriteString("- [" + slug + "](index-" + slug + ".md) — when " + slug + "\n")
	}
	for i := 0; i < resident; i++ {
		name := fmt.Sprintf("always-%02d", i)
		entry(name)
		root.WriteString("- [" + name + "](" + name + ".md) — always\n")
	}
	write("MEMORY.md", root.String())
	entry("linked-from-nowhere")
}

func TestMemoryImportKeepsClaudesGroups(t *testing.T) {
	f := newFakeMemoryDaemon(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := filepath.Join(home, ".claude", "projects", projects.Slug(f.project), "memory")
	writeClaudeTree(t, source, map[string]int{"alpha": 3, "beta": 2}, 2)
	before := snapshotDir(t, source)
	key, _ := memory.KeyFor(f.project)

	code, out, errs := f.run("", "import", "--from-claude")
	if code != 0 || !strings.Contains(out, "2 groups, from the sub-indexes MEMORY.md links:\n") ||
		!strings.Contains(out, "  alpha — 3 entries, from index-alpha.md — Read before any invented alpha work\n") ||
		!strings.Contains(out, "  beta — 2 entries, from index-beta.md") ||
		!strings.Contains(out, "8 to import:\n resident (3):\n") || !strings.Contains(out, " in group alpha (3):\n  alpha-lesson-00") ||
		!strings.Contains(out, "1 not linked from MEMORY.md, imported as resident:\n  linked-from-nowhere") ||
		!strings.Contains(out, "index-alpha.md: a sub-index; it becomes group alpha") {
		t.Fatalf("dry run: %d\n%s\n%s", code, out, errs)
	}
	if l, _ := f.store.Listing(key); len(l.Entries) != 0 || len(l.Groups) != 0 {
		t.Fatalf("a dry run wrote the store: %+v", l)
	}
	code, out, errs = f.run("", "import", "--from-claude", "--apply")
	if code != 0 || !strings.Contains(out, "Imported 8 entries.") {
		t.Fatalf("apply: %d %q %q", code, out, errs)
	}
	l, err := f.store.Listing(key)
	if err != nil || len(l.Entries) != 8 || len(l.Groups) != 2 || l.Groups[0].Slug != "alpha" || l.Groups[0].Entries != 3 ||
		l.Groups[1].Entries != 2 || l.Groups[0].Description != "Read before any invented alpha work" {
		t.Fatalf("store after import: %+v %v", l, err)
	}
	for _, e := range l.Entries {
		if strings.HasPrefix(e.Name, "index-") {
			t.Fatalf("a sub-index was imported as an entry: %s", e.Name)
		}
	}
	if got, _ := f.store.Get(key, "beta-lesson-01"); got.Group != "beta" {
		t.Fatalf("beta-lesson-01 is in group %q", got.Group)
	}
	if got, _ := f.store.Get(key, "linked-from-nowhere"); got.Group != "" {
		t.Fatalf("an unlinked entry was grouped: %q", got.Group)
	}
	if after := snapshotDir(t, source); after != before {
		t.Fatal("the import changed Claude Code's memory directory")
	}
	if code, out, _ := f.run("", "import", "--from-claude", "--apply"); code != 0 || !strings.Contains(out, "0 to import") ||
		!strings.Contains(out, "(already in the store; its description is kept)") {
		t.Fatalf("a second import: %d %q", code, out)
	}
}

func TestMemoryListByGroupAndGroupCommands(t *testing.T) {
	f := newFakeMemoryDaemon(t)
	if code, _, errs := f.run("body", "add", "--name", "tag-first", "--description", "invented", "--type", "project", "--group", "release-steps"); code != 1 ||
		!strings.Contains(errs, "memory_group_not_found") {
		t.Fatalf("an add naming an unset group: %d %q", code, errs)
	}
	if code, out, errs := f.run("", "group", "set", "release-steps", "--description", "before cutting a release"); code != 0 || out != "release-steps: created\n" {
		t.Fatalf("group set: %d %q %q", code, out, errs)
	}
	if code, _, errs := f.run("", "group", "set", "Bad Slug", "--description", "x"); code != 1 || !strings.Contains(errs, "refused") {
		t.Fatalf("a bad slug: %d %q", code, errs)
	}
	for _, args := range [][]string{
		{"add", "--name", "tag-first", "--description", "invented tag lesson", "--type", "project", "--group", "release-steps"},
		{"add", "--name", "bump-version", "--description", "invented bump lesson", "--type", "feedback", "--group", "release-steps"},
		{"add", "--name", "always-here", "--description", "invented resident lesson", "--type", "user"},
	} {
		if code, _, errs := f.run("Invented body.", args...); code != 0 {
			t.Fatalf("%v: %d %q", args, code, errs)
		}
	}
	code, out, errs := f.run("", "list", "--group", "release-steps")
	if code != 0 || out != "release-steps — before cutting a release (2 entries):\n\nfeedback\n  bump-version — invented bump lesson\nproject\n  tag-first — invented tag lesson\n" {
		t.Fatalf("list --group: %d %q %q", code, out, errs)
	}
	code, out, _ = f.run("", "list")
	if code != 0 || !strings.Contains(out, "3 entries for") || !strings.Contains(out, "1 resident, 2 in groups") ||
		!strings.Contains(out, "  always-here — invented resident lesson") || strings.Contains(out, "tag-first") ||
		!strings.Contains(out, "  release-steps — before cutting a release (2 entries)") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, out, _ := f.run("", "group", "list"); code != 0 || out != "  release-steps — before cutting a release (2 entries)\n" {
		t.Fatalf("group list: %d %q", code, out)
	}
	if code, _, errs := f.run("", "list", "--group", "never-set"); code != 1 || !strings.Contains(errs, "no group") {
		t.Fatalf("an unknown group: %d %q", code, errs)
	}
	if code, _, _ := f.run("", "show", "tag-first", "--group", "x"); code != 2 {
		t.Fatalf("--group on show: %d", code)
	}
	if code, out, _ := f.run("", "show", "tag-first"); code != 0 || !strings.Contains(out, "  group: release-steps\n") {
		t.Fatalf("show: %d %q", code, out)
	}
	key, _ := memory.KeyFor(f.project)
	if code, _, errs := f.run("New body.", "update", "--name", "tag-first", "--description", "a new invented line", "--type", "project"); code != 0 {
		t.Fatalf("update without --group: %d %q", code, errs)
	}
	if got, _ := f.store.Get(key, "tag-first"); got.Group != "release-steps" || got.Description != "a new invented line" {
		t.Fatalf("an update without --group moved the entry: %+v", got)
	}
	if code, _, errs := f.run("New body.", "update", "--name", "tag-first", "--description", "a new invented line", "--type", "project", "--group", ""); code != 0 {
		t.Fatalf("update --group '': %d %q", code, errs)
	}
	if got, _ := f.store.Get(key, "tag-first"); got.Group != "" {
		t.Fatalf("--group '' left the entry grouped: %+v", got)
	}
}
