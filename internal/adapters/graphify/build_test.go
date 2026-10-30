package graphify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
func fixtureEngine(_ context.Context, input, output string, phase func(string)) error {
	phase("parsing")
	return os.WriteFile(filepath.Join(output, "graph.json"), []byte(`{"graph":{"schema_version":1},"nodes":[{"id":"main","label":"main","source_file":"main.go"}],"links":[]}`), 0600)
}
func awaitBuild(t *testing.T, b *Builder, root string) BuildStatus {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		status := b.Status(root)
		if status.State != "running" {
			return status
		}
		select {
		case <-deadline:
			t.Fatal("build did not settle")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBuildPublishesCompleteNativeIndexAndInertGraph(t *testing.T) {
	root := buildFixture(t)
	b := NewBuilder(t.TempDir(), nil, fixtureEngine)
	if _, err := b.Start(root, "one"); err != nil {
		t.Fatal(err)
	}
	status := awaitBuild(t, b, root)
	if status.State != "succeeded" || status.Files != 1 {
		t.Fatalf("%+v", status)
	}
	view, err := View(t.Context(), root)
	if err != nil || view.State != Matched || len(view.Nodes) != 1 {
		t.Fatalf("%+v %v", view, err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "graphify-out", "graph.json"))
	if _, err = b.Start(root, "two"); err != BuildError("index_exists") {
		t.Fatalf("overwrite accepted: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "graphify-out", "graph.json"))
	if string(before) != string(after) {
		t.Fatal("existing index changed")
	}
	for _, entry := range mustReadDir(t, root) {
		if entry.Name() != "main.go" && entry.Name() != "graphify-out" {
			t.Fatalf("left temporary project content %s", entry.Name())
		}
	}
}
func mustReadDir(t *testing.T, root string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestBuildRefusesChangesAndConcurrentForeignIndex(t *testing.T) {
	for _, change := range []string{"edit", "delete", "add", "config", "foreign-index", "invalid-graph"} {
		t.Run(change, func(t *testing.T) {
			root := buildFixture(t)
			b := NewBuilder(t.TempDir(), nil, func(ctx context.Context, input, output string, p func(string)) error {
				if err := fixtureEngine(ctx, input, output, p); err != nil {
					return err
				}
				switch change {
				case "edit":
					return os.WriteFile(filepath.Join(root, "main.go"), []byte("changed"), 0600)
				case "delete":
					return os.Remove(filepath.Join(root, "main.go"))
				case "add":
					return os.WriteFile(filepath.Join(root, "new.go"), []byte("package main"), 0600)
				case "config":
					return os.WriteFile(filepath.Join(root, ".graphifyignore"), nil, 0600)
				case "foreign-index":
					return os.Mkdir(filepath.Join(root, "graphify-out"), 0700)
				case "invalid-graph":
					return os.WriteFile(filepath.Join(output, "graph.json"), []byte(`{"graph":{"schema_version":1},"nodes":[{"id":"a"}],"links":[{"source":"a","target":"absent"}]}`), 0600)
				}
				return nil
			})
			if _, err := b.Start(root, "one"); err != nil {
				t.Fatal(err)
			}
			status := awaitBuild(t, b, root)
			if status.State != "failed" {
				t.Fatalf("%+v", status)
			}
			if change == "foreign-index" {
				if entries := mustReadDir(t, filepath.Join(root, "graphify-out")); len(entries) != 0 {
					t.Fatal("foreign empty directory overwritten")
				}
			} else if _, err := os.Lstat(filepath.Join(root, "graphify-out")); !os.IsNotExist(err) {
				t.Fatalf("published changed source: %v", err)
			}
		})
	}
}

func TestBuildDeduplicatesAndRefusesOtherRootWithoutQueue(t *testing.T) {
	root := buildFixture(t)
	other := buildFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	b := NewBuilder(t.TempDir(), nil, func(ctx context.Context, input, output string, p func(string)) error {
		calls.Add(1)
		close(started)
		<-release
		return fixtureEngine(ctx, input, output, p)
	})
	if _, err := b.Start(root, "same"); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := b.Start(root, "same"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Start(root, "another-click"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Start(other, "other"); err != BuildError("busy") {
		t.Fatalf("%v", err)
	}
	close(release)
	awaitBuild(t, b, root)
	if calls.Load() != 1 {
		t.Fatalf("ran %d times", calls.Load())
	}
	if _, err := b.Start(root, "same"); err != nil {
		t.Fatalf("lost receipt: %v", err)
	}
}

func TestBuildSwitchOffCancelsBeforePublishingAndReleasesSlot(t *testing.T) {
	root := buildFixture(t)
	var enabled atomic.Bool
	enabled.Store(true)
	started := make(chan struct{})
	b := NewBuilder(t.TempDir(), enabled.Load, func(ctx context.Context, input, output string, p func(string)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if _, err := b.Start(root, "one"); err != nil {
		t.Fatal(err)
	}
	<-started
	enabled.Store(false)
	if got := awaitBuild(t, b, root); got.Error != "disabled" {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "graphify-out")); !os.IsNotExist(err) {
		t.Fatal("disabled job published")
	}
	if _, err := b.Start(root, "two"); err != BuildError("disabled") {
		t.Fatalf("%v", err)
	}
}

func TestLocalParserNeverInheritsKeysAndCapsLogs(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "private-test-key")
	t.Setenv("HTTPS_PROXY", "http://invalid.test")
	env, _ := json.Marshal(parserEnvironment(t.TempDir()))
	for _, s := range []string{"OPENAI_API_KEY", "private-test-key", "HTTPS_PROXY"} {
		if containsBytes(env, []byte(s)) {
			t.Fatal("inherited credentials or proxy")
		}
	}
	var output cappedOutput
	data := make([]byte, BuildLogBytesLimit+1)
	if n, err := output.Write(data); n != len(data) || err != nil || len(output.data) != BuildLogBytesLimit {
		t.Fatal("log cap failed")
	}
}

func TestBuildKeepsUnresolvedReferencesWithoutClaimingTheirSourceWasChecked(t *testing.T) {
	root := buildFixture(t)
	b := NewBuilder(t.TempDir(), nil, func(_ context.Context, input, output string, phase func(string)) error {
		return os.WriteFile(filepath.Join(output, "graph.json"), []byte(`{"graph":{"schema_version":1},"nodes":[{"id":"main","source_file":"main.go"},{"id":"node_fs","label":"node:fs","source_file":"node:fs","source_location":"unknown"}],"links":[{"source":"main","target":"node_fs","relation":"imports"}]}`), 0600)
	})
	if _, err := b.Start(root, "one"); err != nil {
		t.Fatal(err)
	}
	if got := awaitBuild(t, b, root); got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	view, err := View(t.Context(), root)
	if err != nil || view.State != Matched || view.UnverifiedSources != 1 || view.TotalNodes != 2 || view.TotalLinks != 1 {
		t.Fatalf("%+v %v", view, err)
	}
	for _, node := range view.Nodes {
		if node.Label == "node:fs" && (node.File != "" || node.Location != "") {
			t.Fatal("unverified import presented as a checked source file")
		}
	}
}
func containsBytes(a, b []byte) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if string(a[i:i+len(b)]) == string(b) {
			return true
		}
	}
	return false
}
