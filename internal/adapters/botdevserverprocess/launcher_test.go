package botdevserverprocess

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func limits() acp.Limits {
	return acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20,
		CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second,
		CleanupTimeout: 5 * time.Second, RPCTimeout: 5 * time.Second, DescendantPoll: 10 * time.Millisecond}
}

func projectFixture(t *testing.T) (pa.ProjectResolution, servers.Spec) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := pa.ProjectResolution{Scope: pa.ProjectScope{MachineID: "synthetic-machine", ProjectID: "synthetic-project", CanonicalID: "synthetic-repository", Kind: pa.RepositoryProject}, Directory: root, Available: true, Current: true}
	return project, servers.Spec{Key: pa.BotKey{MachineID: project.Scope.MachineID, BotID: "synthetic-bot"}, Project: project.Scope, Name: "Synthetic server", Command: "printf synthetic", Directory: root}
}

func TestCommandEnvironmentIsASnapshotOfSelectedSourceValues(t *testing.T) {
	environment := []string{"PATH=/source/bin", "HOME=/source/home", "LANG=C", "HOME=/source/current",
		"OPENAI_API_KEY=synthetic", "ANTHROPIC_API_KEY=synthetic", "CLAWDLINE_NEXT_DIR=synthetic",
		"NODE_OPTIONS=synthetic", "BASH_ENV=synthetic", "ENV=synthetic", "LD_PRELOAD=synthetic", "DYLD_INSERT_LIBRARIES=synthetic"}
	l, err := New(limits(), environment)
	if err != nil {
		t.Fatal(err)
	}
	environment[0] = "PATH=/changed"
	if !reflect.DeepEqual(l.environment, []string{"HOME=/source/current", "LANG=C", "PATH=/source/bin"}) {
		t.Fatal("the command inherited a provider credential, startup hook or mutable environment")
	}
	if _, err := New(limits(), []string{"HOME=bad\x00value"}); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
}

func TestCommandDirectoryUsesTheCurrentCompleteCanonicalProject(t *testing.T) {
	project, _ := projectFixture(t)
	child := filepath.Join(project.Directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"child", "child" + string(filepath.Separator), filepath.Join("child", "."), child} {
		if got, err := ResolveDirectory(project, folder); err != nil || got != child {
			t.Fatalf("directory=%s %v", got, err)
		}
	}
	for _, folder := range []string{"..", t.TempDir(), filepath.Join("..", "outside")} {
		if _, err := ResolveDirectory(project, folder); pa.Code(err) != pa.ScopeDenied {
			t.Fatalf("outside directory=%v", err)
		}
	}
	if _, err := ResolveDirectory(project, "missing"); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Directory, "file"), []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDirectory(project, "file"); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
	project.Current = false
	if _, err := ResolveDirectory(project, ""); err == nil {
		t.Fatal("a stale Project resolution admitted a directory")
	}
}
