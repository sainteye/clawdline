package codegraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in integration acceptance uses the real pinned runtime. The first run
// downloads at most the single locked platform archive; subsequent runs reuse it.
func TestCodeGraphNativeLifecycle(t *testing.T) {
	cache := os.Getenv("CODEGRAPH_NATIVE_TEST_CACHE")
	if cache == "" {
		t.Skip("set CODEGRAPH_NATIVE_TEST_CACHE for native acceptance")
	}
	root := fixtureRoot(t)
	var on atomic.Bool
	on.Store(true)
	b := NewBuilder(cache, on.Load, nil)
	defer func() { _ = b.Close(context.Background()) }()
	if _, err := b.Start(root, "native"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Minute)
	for b.Status(root).State == "running" {
		select {
		case <-deadline:
			t.Fatal("native build timeout")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if s := b.Status(root); s.State != "succeeded" {
		t.Fatalf("native build: %+v", s)
	}
	initial, initialErr := Inspect(t.Context(), root)
	t.Logf("initial check=%+v error=%v", initial, initialErr)
	svc, e := b.ensureService(t.Context(), root)
	if e != nil {
		t.Fatal(e)
	}
	if b.ServiceCount() != 1 {
		t.Fatal("path aliases started duplicate native services")
	}
	var initialLive serviceStatus
	e = svc.call(t.Context(), "status", "", &initialLive)
	t.Logf("initial service=%+v error=%v", initialLive, e)
	answer, err := nativeQuery(t, b, root, "Start")
	if err != nil || !strings.Contains(answer.Text, "Start") {
		t.Fatalf("query: %+v %v", answer, err)
	}
	view, err := b.View(t.Context(), root)
	if err != nil || view.State != Matched || view.TotalNodes == 0 {
		t.Fatalf("view: %+v %v", view, err)
	}
	t.Logf("initial nodes=%d links=%d watching=%v", view.TotalNodes, view.TotalLinks, view.Watching)
	file := filepath.Join(root, "main.go")
	stat, _ := os.Stat(file)
	os.WriteFile(file, []byte("package main\nfunc Newer(){}\nfunc main(){Newer()}\n"), 0600)
	os.Chtimes(file, stat.ModTime(), stat.ModTime())
	// A source check must reject the old generation before reconciliation.
	status, _ := Inspect(t.Context(), root)
	if status.State == Matched {
		t.Fatal("same-mtime stale source accepted")
	}
	s, err := b.ensureService(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	// Let the actual watcher settle. Degraded environments use the production periodic reconciler.
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		status, _ = Inspect(t.Context(), root)
		if status.State == Matched {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if view.Watching && status.State != Matched {
		t.Fatal("watcher did not reconcile same-mtime edit")
	}

	if !view.Watching {
		until := time.Now().Add(time.Duration(ReconcileSecondsLimit+5) * time.Second)
		for time.Now().Before(until) {
			status, _ = Inspect(t.Context(), root)
			if status.State == Matched {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if status.State != Matched {
			t.Fatal("periodic reconciliation did not catch a missed watch event")
		}
		t.Log("degraded watcher recovered through the production periodic reconciler")
	}
	answer, err = nativeQuery(t, b, root, "Newer")
	if err != nil || !strings.Contains(answer.Text, "Newer") {
		t.Fatalf("updated query: %+v %v", answer, err)
	}
	os.WriteFile(filepath.Join(root, "added.go"), []byte("package main\nfunc Added(){}"), 0600)
	var live serviceStatus
	syncNow := func() {
		t.Helper()
		for i := 0; i < 30; i++ {
			err = s.call(t.Context(), "sync", "", &live)
			if err == nil {
				return
			}
			if err != BuildError("busy") {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("reconcile remained busy")
	}
	syncNow()
	answer, err = nativeQuery(t, b, root, "Added")
	if err != nil || !strings.Contains(answer.Text, "Added") {
		t.Fatal(answer, err)
	}
	os.Rename(filepath.Join(root, "added.go"), filepath.Join(root, "moved.go"))
	syncNow()
	os.Remove(filepath.Join(root, "moved.go"))
	syncNow()
	// Scope changes must remove previously indexed rows, including equal-mtime
	// configuration rewrites which an upstream stat cache may otherwise miss.
	os.WriteFile(filepath.Join(root, "hidden.go"), []byte("package main\nfunc Hidden(){}"), 0600)
	syncNow()
	config := filepath.Join(root, "codegraph.json")
	os.WriteFile(config, []byte(`{"exclude":["hidden.go"]}`), 0600)
	syncNow()
	metadata, _ := os.Stat(config)
	os.WriteFile(config, []byte(`{"exclude":["absent.go"]}`), 0600)
	os.Chtimes(config, metadata.ModTime(), metadata.ModTime())
	syncNow()
	answer, err = nativeQuery(t, b, root, "Hidden")
	if err != nil {
		check, _ := Inspect(t.Context(), root)
		var state serviceStatus
		se := s.call(t.Context(), "status", "", &state)
		t.Logf("scope check=%+v service=%+v error=%v", check, state, se)
	}
	if err != nil || !strings.Contains(answer.Text, "Hidden") {
		t.Fatalf("scope rewrite: %+v %v", answer, err)
	}
	// Each exact worktree root owns an independent index; another unindexed root
	// cannot borrow the first root's database or its successful freshness check.
	other := fixtureRoot(t)
	if _, err = b.Query(t.Context(), other, "Newer"); err == nil {
		t.Fatal("borrowed another worktree index")
	}
	if err = b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.exited() {
		t.Fatal("close did not join native process")
	}
	os.WriteFile(file, []byte("package main\nfunc Offline(){}\nfunc main(){Offline()}\n"), 0600)
	b = NewBuilder(cache, on.Load, nil)
	answer, err = nativeQuery(t, b, root, "Offline")
	if err != nil || !strings.Contains(answer.Text, "Offline") {
		t.Fatalf("restart catch-up: %+v %v", answer, err)
	}
	s, err = b.ensureService(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	on.Store(false)
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("disable orphaned native process")
	}
	if _, err = b.Query(t.Context(), root, "Offline"); err != BuildError("disabled") {
		t.Fatalf("disabled query: %v", err)
	}
}

// File-watch callbacks can still arrive after an explicit sync. The contract is
// eventual fresh success or a typed busy/stale refusal during that overlap.
func nativeQuery(t *testing.T, b *Builder, root, question string) (QueryResult, error) {
	t.Helper()
	var answer QueryResult
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for {
		answer, err = b.Query(t.Context(), root, question)
		if err == nil || (err != BuildError("busy") && err != BuildError("source_changed")) || time.Now().After(deadline) {
			return answer, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestCodeGraphNativeProject(t *testing.T) {
	root := os.Getenv("CODEGRAPH_NATIVE_PROJECT_ROOT")
	cache := os.Getenv("CODEGRAPH_NATIVE_TEST_CACHE")
	if root == "" || cache == "" {
		t.Skip("set task-owned CODEGRAPH_NATIVE_PROJECT_ROOT and CODEGRAPH_NATIVE_TEST_CACHE")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("acceptance requires a new empty project path")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	repo, err := canonical("../../..")
	if err != nil {
		t.Fatal(err)
	}
	sources, reading, err := scan(t.Context(), repo, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("input files=%d bytes=%d", len(sources), reading.Bytes)
	b := NewBuilder(cache, nil, nil)
	defer b.Close(context.Background())
	started := time.Now()
	if _, err = b.Start(root, "project"); err != nil {
		t.Fatal(err)
	}
	for b.Status(root).State == "running" {
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Logf("build=%+v elapsed=%s", b.Status(root), time.Since(started))
	if b.Status(root).State != "succeeded" {
		t.Fatal("native project build failed")
	}
	answer, err := nativeQuery(t, b, root, "codegraphQueryRoute")
	if err != nil || !strings.Contains(answer.Text, "internal/transport/http/codegraph.go") {
		t.Fatal("project query", answer, err)
	}
	view, err := b.View(t.Context(), root)
	if err != nil || view.State != Matched || len(view.Nodes) > ViewNodeLimit || len(view.Links) > ViewLinkLimit {
		t.Fatalf("project view %+v %v", view, err)
	}
	t.Logf("native nodes=%d links=%d shown=%d/%d watching=%v", view.TotalNodes, view.TotalLinks, len(view.Nodes), len(view.Links), view.Watching)
	if output := os.Getenv("CODEGRAPH_NATIVE_VIEW_OUTPUT"); output != "" {
		raw, _ := json.Marshal(view)
		if err := os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe := filepath.Join(root, "internal", "adapters", "codegraph", "acceptance_probe.go")
	if err = os.WriteFile(probe, []byte("package codegraph\nfunc IndexProbeBefore(){}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	answer, err = nativeQuery(t, b, root, "IndexProbeBefore")
	if err != nil || !strings.Contains(answer.Text, "IndexProbeBefore") {
		t.Fatal("full-project incremental add", answer, err)
	}
	stamp, _ := os.Stat(probe)
	os.WriteFile(probe, []byte("package codegraph\nfunc IndexProbeAfterx(){}\n"), 0600)
	os.Chtimes(probe, stamp.ModTime(), stamp.ModTime())
	answer, err = nativeQuery(t, b, root, "IndexProbeAfterx")
	if err != nil || !strings.Contains(answer.Text, "IndexProbeAfterx") {
		t.Fatal("full-project same-mtime edit", answer, err)
	}
	os.Remove(probe)
	service, err := b.ensureService(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	var state serviceStatus
	for i := 0; i < 30; i++ {
		err = service.call(t.Context(), "sync", "", &state)
		if err == nil {
			break
		}
		if err != BuildError("busy") {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("full-project removal", err)
	}
	t.Log("full-project add, same-mtime edit and removal reconciled")
}
