package codegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureStore(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE project_metadata(key TEXT PRIMARY KEY,value TEXT)`,
		`INSERT INTO project_metadata VALUES('index_state','complete'),('indexed_with_version','1.6.2')`,
		`CREATE TABLE files(path TEXT,content_hash TEXT)`,
		`CREATE TABLE nodes(id TEXT,name TEXT,file_path TEXT,start_line INTEGER)`,
		`CREATE TABLE edges(id TEXT,source TEXT,target TEXT,kind TEXT,provenance TEXT)`,
		`INSERT INTO nodes VALUES('one','Start','main.go',2),('two','main','main.go',3),('untrusted','<script>','../../outside',1)`,
		`INSERT INTO edges VALUES('e','two','one','calls','heuristic')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
func fixtureEngine(t *testing.T) Engine {
	return func(ctx context.Context, input, output string, phase func(string)) error {
		fixtureStore(t, filepath.Join(output, "native"))
		return nil
	}
}
func testBuilder(t *testing.T, allowed func() bool, engine Engine) *Builder {
	t.Helper()
	b := NewBuilder(t.TempDir(), allowed, engine)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return b
}
func awaitBuild(t *testing.T, b *Builder, root string) BuildStatus {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		s := b.Status(root)
		if s.State != "running" {
			return s
		}
		select {
		case <-timeout:
			t.Fatal("build did not stop")
		case <-time.After(time.Millisecond):
		}
	}
}
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Start(){}\nfunc main(){Start()}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestBuildPublishesSeparateNativeStoreAndBoundedInertView(t *testing.T) {
	root := fixtureRoot(t)
	b := testBuilder(t, nil, func(ctx context.Context, input, output string, phase func(string)) error {
		relative, err := filepath.Rel(root, input)
		if err != nil || !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return BuildError("snapshot_visible_in_project")
		}
		return fixtureEngine(t)(ctx, input, output, phase)
	})
	if _, err := b.Start(root, "one"); err != nil {
		t.Fatal(err)
	}
	if s := awaitBuild(t, b, root); s.State != "succeeded" {
		t.Fatal(s)
	}
	status, err := Inspect(t.Context(), root)
	if err != nil || status.State != Matched {
		t.Fatalf("%+v %v", status, err)
	}
	v, err := b.View(t.Context(), root)
	if err != nil || len(v.Nodes) != 3 || len(v.Links) != 1 || v.UnverifiedSources != 1 || v.Links[0].Confidence != "INFERRED" {
		t.Fatalf("%+v %v", v, err)
	}
	for _, n := range v.Nodes {
		if n.Label == "<script>" && n.File != "" {
			t.Fatal("unverified source escaped")
		}
	}
	before, _ := os.ReadFile(filepath.Join(root, ".codegraph", "codegraph.db"))
	if _, err := View(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".codegraph", "codegraph.db"))
	if string(before) != string(after) {
		t.Fatal("viewer changed native store")
	}
	st, _ := os.Stat(filepath.Join(root, "main.go"))
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Other(){}\nfunc main(){Other()}\n"), 0600)
	os.Chtimes(filepath.Join(root, "main.go"), st.ModTime(), st.ModTime())
	status, err = Inspect(t.Context(), root)
	if err != nil || status.State != "stale" {
		t.Fatalf("same-mtime edit accepted: %+v %v", status, err)
	}
}
func TestBuildRejectsChangedSourceAndPreservesForeignIndexes(t *testing.T) {
	root := fixtureRoot(t)
	b := testBuilder(t, nil, func(ctx context.Context, input, output string, p func(string)) error {
		fixtureStore(t, filepath.Join(output, "native"))
		return os.WriteFile(filepath.Join(root, "main.go"), []byte("package changed"), 0600)
	})
	b.Start(root, "changed")
	if s := awaitBuild(t, b, root); s.Error != "source_changed" {
		t.Fatal(s)
	}
	if _, err := os.Stat(filepath.Join(root, ".codegraph")); !os.IsNotExist(err) {
		t.Fatal("published stale index")
	}
	os.Mkdir(filepath.Join(root, ".codegraph"), 0700)
	keep := filepath.Join(root, ".codegraph", "keep")
	os.WriteFile(keep, []byte("user data"), 0600)
	if _, err := b.Start(root, "foreign"); err != BuildError("index_exists") {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(keep); string(data) != "user data" {
		t.Fatal("foreign data changed")
	}
}
func TestDisableCancelsExactBuildAndCloseJoinsIt(t *testing.T) {
	var on atomic.Bool
	on.Store(true)
	started := make(chan struct{})
	stopped := make(chan struct{})
	b := testBuilder(t, on.Load, func(ctx context.Context, _, _ string, _ func(string)) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	root := fixtureRoot(t)
	b.Start(root, "one")
	<-started
	if _, err := b.Start(fixtureRoot(t), "two"); err != BuildError("busy") {
		t.Fatal(err)
	}
	on.Store(false)
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("off switch left parser running")
	}
	if s := awaitBuild(t, b, root); s.Error != "disabled" {
		t.Fatal(s)
	}
	if _, err := b.Query(t.Context(), root, "main"); err == nil {
		t.Fatal("disabled query accepted")
	}
}
func TestUnverifiedStoreAndUnsafeSourceAreRefused(t *testing.T) {
	root := fixtureRoot(t)
	b := testBuilder(t, nil, fixtureEngine(t))
	b.Start(root, "one")
	awaitBuild(t, b, root)
	os.WriteFile(filepath.Join(root, ".codegraph", "codegraph.db"), []byte("broken"), 0600)
	s, err := Inspect(t.Context(), root)
	if err != nil || s.State == Matched {
		t.Fatalf("broken native index accepted: %+v %v", s, err)
	}
	outside := fixtureRoot(t)
	if err := os.Symlink(filepath.Join(outside, "main.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Skip(err)
	}
	if _, _, err := scan(t.Context(), root, ""); err != BuildError("unsafe_source") {
		t.Fatal(err)
	}
}
func TestRuntimeEnvironmentAndLaunchDoNotGrantProviderAccess(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("NODE_OPTIONS", "--require malicious")
	env := strings.Join(parserEnvironment(t.TempDir()), "\n")
	if strings.Contains(env, "secret") || strings.Contains(env, "NODE_OPTIONS") {
		t.Fatal("provider environment inherited")
	}
	instructions, err := LaunchInstructions(t.Context(), t.TempDir())
	if err != nil || !strings.Contains(instructions, "clawdline codegraph query") || !strings.Contains(instructions, "untrusted") {
		t.Fatal(instructions, err)
	}
	var lock runtimeManifest
	if json.Unmarshal(runtimeLock, &lock) != nil || len(lock.Packages) != 6 || lock.Version != Version {
		t.Fatal("invalid runtime lock")
	}
	for _, p := range lock.Packages {
		if !strings.Contains(p.URL, "1.6.2.tgz") || !strings.HasPrefix(p.Integrity, "sha512-") {
			t.Fatal("unpinned runtime")
		}
	}
}

func TestViewerReturnsProgressWithoutCancellingAnInitializingService(t *testing.T) {
	root := fixtureRoot(t)
	b := testBuilder(t, nil, fixtureEngine(t))
	b.Start(root, "one")
	awaitBuild(t, b, root)
	resolved, _ := canonical(root)
	parent, cancel := context.WithCancel(b.ctx)
	pending := &service{root: resolved, ctx: parent, cancel: cancel, initialized: make(chan struct{}), done: make(chan struct{})}
	pending.lastUsed.Store(time.Now().Unix())
	b.mu.Lock()
	b.native = true
	b.services[resolved] = pending
	b.mu.Unlock()
	defer close(pending.done)
	ctx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	view, err := b.View(ctx, root)
	if err != nil || view.State != "stale" || view.Reason != "codegraph_reconciling" || len(view.Nodes) == 0 {
		t.Fatalf("pending service hid progress: %+v %v", view, err)
	}
	aborted, abort := context.WithCancel(t.Context())
	abort()
	if _, err = b.ensureService(aborted, root); err != context.Canceled {
		t.Fatal(err)
	}
	if parent.Err() != nil {
		t.Fatal("an aborted reader cancelled daemon-owned startup")
	}
}
