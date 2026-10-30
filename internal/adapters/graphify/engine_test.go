package graphify

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in exercises the production wheel preparation and parser entry
// point. Normal offline tests do not download dependencies or use a real home.
func TestRealLocalParser(t *testing.T) {
	cache := os.Getenv("CLAWDLINE_GRAPHIFY_SMOKE_CACHE")
	if cache == "" {
		t.Skip("explicit local parser smoke only")
	}
	root := buildFixture(t)
	if project := os.Getenv("CLAWDLINE_GRAPHIFY_SMOKE_PROJECT"); project != "" {
		var err error
		root, err = filepath.EvalSymlinks(project)
		if err != nil {
			t.Fatal(err)
		}
	}
	b := NewBuilder(cache, nil, nil)
	if dest := os.Getenv("CLAWDLINE_GRAPHIFY_SMOKE_GRAPH"); dest != "" {
		b.engine = func(ctx context.Context, input, output string, phase func(string)) error {
			err := b.extract(ctx, input, output, phase)
			if err == nil {
				if raw, e := os.ReadFile(filepath.Join(output, "graph.json")); e == nil {
					if e = os.WriteFile(dest, raw, 0600); e != nil {
						return e
					}
				}
			}
			return err
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), BuildSecondsLimit*time.Second)
	defer cancel()
	job := &buildJob{}
	if err := b.build(ctx, root, job); err != nil {
		t.Fatalf("phase=%s: %v", job.Phase, err)
	}
	view, err := View(t.Context(), root)
	if err != nil || view.State != Matched || len(view.Nodes) == 0 {
		t.Fatalf("%+v %v", view, err)
	}
	t.Logf("state=%s nodes=%d links=%d source_files=%d source_bytes=%d", view.State, view.TotalNodes, view.TotalLinks, job.Files, job.Bytes)
	// An attempted upload fails inside the same pinned Python interpreter and
	// audit entry point. Source files are never executed by the AST parser.
	home := t.TempDir()
	entry := filepath.Join(home, "blocked.py")
	guard := localParser[:len(localParser)-len("main()\n")]
	// Use only the audit hook prefix, before any parser imports or CLI setup.
	for i := 0; i+len("# Force sequential") <= len(guard); i++ {
		if guard[i:i+len("# Force sequential")] == "# Force sequential" {
			guard = guard[:i]
			break
		}
	}
	guard += "import socket\nsocket.create_connection(('127.0.0.1', 9))\n"
	if err = os.WriteFile(entry, []byte(guard), 0600); err != nil {
		t.Fatal(err)
	}
	// Locate the runtime with its lock marker without reading any user install.
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	python := ""
	for _, e := range entries {
		if e.IsDir() {
			if _, err = os.Stat(filepath.Join(cache, e.Name(), "clawdline-parser-lock.txt")); err == nil {
				python = privatePython(filepath.Join(cache, e.Name()))
			}
		}
	}
	if python == "" {
		t.Fatal("missing managed runtime")
	}
	if err = b.runParserCommand(ctx, home, python, "-I", "-B", entry); err == nil || !strings.Contains(err.Error(), "Clawdline local parser blocks network and child processes") {
		t.Fatalf("network tripwire was not observed: %v", err)
	}
}

type vanishedCacheEntry struct{}

func (vanishedCacheEntry) Name() string               { return "cache.tmp" }
func (vanishedCacheEntry) IsDir() bool                { return false }
func (vanishedCacheEntry) Type() fs.FileMode          { return 0 }
func (vanishedCacheEntry) Info() (fs.FileInfo, error) { return nil, os.ErrNotExist }

type changingCacheFS struct{ fs.FS }

func (f changingCacheFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if name == "." {
		entries = append(entries, vanishedCacheEntry{})
	}
	return entries, err
}
func TestParserDiskBudgetAcceptsCacheFilesRenamedDuringItsScan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "retained"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := parserDiskSizeFS(t.Context(), changingCacheFS{os.DirFS(dir)}); err != nil {
		t.Fatalf("atomic cache rename stopped a valid build: %v", err)
	}
}
