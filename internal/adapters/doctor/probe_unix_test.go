//go:build darwin || linux

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func assertHistoryPermissions(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
}

func TestVersionProbeRunsNoPromptAndExportsOnlyRecognizedVersions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	path := filepath.Join(dir, "claude")
	for _, tc := range []struct{ output, state, version string }{{"2.1.34 (Claude Code)", "ok", "2.1.34"}, {"Bearer private-key", "unrecognized", ""}} {
		script := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] && [ \"$LC_ALL\" = C ] || exit 9\nprintf '%s\\n' '" + tc.output + "'\n"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		got := Probe(context.Background(), "claude")
		if got.State != tc.state || got.Version != tc.version {
			t.Fatalf("%+v", got)
		}
	}
	if got := Probe(context.Background(), "codex"); got.State != "not_found" {
		t.Fatalf("missing command: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Probe(ctx, "claude"); got.State != "timeout" {
		t.Fatalf("cancellation: %+v", got)
	}
}
