package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
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
		}
		return refuse(500, "memory_unreadable", err.Error())
	}
	entryOf := func() memory.Entry {
		e := body.(contract.ProjectMemoryEntry)
		return memory.Entry{Name: e.Name, Description: e.Description, Type: memory.Type(e.Type), Body: e.Body}
	}
	switch {
	case name == "" && method == http.MethodGet:
		entries, err := f.store.List(key)
		if err != nil {
			return storeRefusal(err)
		}
		out := contract.ProjectMemoryList{ProjectKey: key, Entries: []contract.ProjectMemorySummary{}}
		for _, e := range entries {
			out.Entries = append(out.Entries, contract.ProjectMemorySummary{Name: e.Name, Description: e.Description, Type: contract.ProjectMemoryType(e.Type)})
		}
		out.Index, out.IndexCut = memory.RenderIndex(entries)
		return reply(200, out)
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
		return reply(200, contract.ProjectMemoryEntry{Name: e.Name, Description: e.Description, Type: contract.ProjectMemoryType(e.Type), Body: e.Body})
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
	if code != 0 || !strings.Contains(out, "1 to import:\n  new-lesson (feedback)") ||
		!strings.Contains(out, "1 already in the store, skipped:\n  already-there") ||
		!strings.Contains(out, "MEMORY.md: not an entry") || !strings.Contains(out, "Nothing was imported") {
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
