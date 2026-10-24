package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Synthetic help mirrors the measured standalone 1.4.5 option spellings.
const cliHelp = `Usage of agy:
  --conversation                  Resume a previous conversation by ID
  --prompt-interactive            Run an initial prompt interactively and continue the session
  --model                         Model for the current CLI session
  --effort                        Reasoning effort for the current CLI session (low|medium|high|xhigh|max)
  --mode                          Set the agent execution mode for this session (accept-edits, auto, plan)
  --input-format                  Input format for print mode (text, stream-json)
  --output-format                 Output format for print mode (text, json, stream-json)
`

func executable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standalone agy")
	if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureRunner(version, help string) Runner {
	return func(ctx context.Context, path, flag string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if flag == "--version" {
			return []byte(version), nil
		}
		if flag == "--help" {
			return []byte(help), nil
		}
		return nil, errors.New("unexpected command")
	}
}

func TestExecutableFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name, version, help string
		compatible          bool
	}{
		{"standalone", "1.4.5\n", cliHelp, true},
		{"editor", "1.107.0", "--new-window\n--install-extension\nserve-web", false},
		{"spoofed_version", "1.4.5", "--new-window\n--install-extension", false},
		{"unverified_version", "1.4.6", cliHelp, false},
		{"missing_option", "1.4.5", strings.ReplaceAll(cliHelp, "--conversation", "--continue"), false},
		{"mentioned_option", "1.4.5", strings.ReplaceAll(cliHelp, "--conversation", "See --conversation"), false},
		{"oversized", "1.4.5", cliHelp + strings.Repeat("x", CLIProbeOutputLimit), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := executable(t)
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			got := NewResolver([]string{path}, fixtureRunner(tc.version, tc.help)).Snapshot(context.Background())
			if !got.Installed || got.Compatible != tc.compatible || got.AuthState != AuthUnknown {
				t.Fatalf("%+v", got)
			}
			if tc.compatible && (got.Path != canonical || got.Version != "1.4.5") {
				t.Fatalf("%+v", got)
			}
			if !tc.compatible && (got.Path != "" || got.Version != "") {
				t.Fatalf("incompatible executable exposed: %+v", got)
			}
		})
	}
}

func TestMissingUnsafeAndNonExecutableNeverRun(t *testing.T) {
	path := executable(t)
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{filepath.Join(t.TempDir(), "missing"), "agy", "/tmp/unsafe\nagy", path} {
		got := NewResolver([]string{candidate}, func(context.Context, string, string) ([]byte, error) {
			t.Fatal("unsafe or absent executable was invoked")
			return nil, nil
		}).Snapshot(context.Background())
		if got.Installed || got.Compatible || got.AuthState != AuthUnknown {
			t.Fatalf("%+v", got)
		}
	}
}

func TestPollingCachesSuccessAndFailureAndInvalidatesChangedFiles(t *testing.T) {
	for _, version := range []string{"1.4.5", "1.107.0"} {
		path := executable(t)
		calls := 0
		r := NewResolver([]string{path}, func(ctx context.Context, path, flag string) ([]byte, error) {
			calls++
			return fixtureRunner(version, cliHelp)(ctx, path, flag)
		})
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r.Snapshot(context.Background()) }()
		}
		wg.Wait()
		want := 1
		if version == "1.4.5" {
			want = 2
		}
		if calls != want {
			t.Fatalf("poll calls %d, want %d", calls, want)
		}
		if err := os.WriteFile(path, []byte("changed executable size"), 0700); err != nil {
			t.Fatal(err)
		}
		r.Snapshot(context.Background())
		if calls != want*2 {
			t.Fatalf("changed file did not invalidate cache: %d", calls)
		}
	}
}

func TestProbeCancellationCanRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewResolver([]string{executable(t)}, fixtureRunner("1.4.5", cliHelp))
	got := r.Snapshot(ctx)
	if got.Compatible || got.Reason != "provider_cli_unreadable" {
		t.Fatalf("%+v", got)
	}
	if got := r.Snapshot(context.Background()); !got.Compatible {
		t.Fatalf("cancelled snapshot cached: %+v", got)
	}
}

func TestProbeContentionReturnsUnknownWithoutQueuing(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan Installation)
	r := NewResolver([]string{executable(t)}, func(ctx context.Context, path, flag string) ([]byte, error) {
		if flag == "--version" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return fixtureRunner("1.4.5", cliHelp)(ctx, path, flag)
	})
	go func() { finished <- r.Snapshot(context.Background()) }()
	<-started
	got := r.Snapshot(context.Background())
	close(release)
	first := <-finished
	if got.InstalledKnown || got.Compatible || got.Reason != "provider_cli_busy" || got.AuthState != AuthUnknown {
		t.Fatalf("contention: %+v", got)
	}
	if !first.Compatible || !first.InstalledKnown {
		t.Fatalf("probe: %+v", first)
	}
}

func TestProbeErrorsAreUnknownAndSymlinkRetargetInvalidatesCache(t *testing.T) {
	first, second := executable(t), executable(t)
	st, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(second, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "agy")
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	r := NewResolver([]string{link}, func(ctx context.Context, path, flag string) ([]byte, error) {
		canonical, _ := filepath.EvalSymlinks(second)
		if path == canonical {
			return nil, errors.New("executable unavailable")
		}
		return fixtureRunner("1.4.5", cliHelp)(ctx, path, flag)
	})
	if got := r.Snapshot(context.Background()); !got.Compatible {
		t.Fatalf("%+v", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot(context.Background()); got.Compatible || got.AuthState != AuthUnknown || got.Reason != "provider_cli_unreadable" {
		t.Fatalf("%+v", got)
	}
}

func TestRealSyntheticExecutableUsesOnlyLocalFingerprintCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	path := filepath.Join(t.TempDir(), "standalone agy")
	script := "#!/bin/sh\n[ \"$LC_ALL\" = C ] || exit 12\ncase \"$1\" in\n--version) echo 1.4.5;;\n--help) cat <<'HELP'\n" + cliHelp + "HELP\n;;\n*) exit 13;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	got := NewResolver([]string{path}, nil).Snapshot(context.Background())
	if !got.Compatible || got.AuthState != AuthUnknown {
		t.Fatalf("%+v", got)
	}
}

func TestProbeOutputBoundRefusesWithoutRetainingOverflow(t *testing.T) {
	w := &limitedOutput{}
	if _, err := w.Write([]byte(strings.Repeat("x", CLIProbeOutputLimit))); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("overflow accepted")
	}
	if len(w.data) != CLIProbeOutputLimit {
		t.Fatalf("retained %d", len(w.data))
	}
}
