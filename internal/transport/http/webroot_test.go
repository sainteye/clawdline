package http

import (
	"os"
	"path/filepath"
	"testing"
)

// A release puts the console at dist/ beside the binary, and the app bundle at
// Contents/Resources/web beside Contents/MacOS; a checkout's bin/ has neither.
func TestTheConsoleInstalledWithTheBinaryIsFoundWithoutConfiguration(t *testing.T) {
	put := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	release := t.TempDir()
	put(filepath.Join(release, "dist"))
	if got := webRootBeside(filepath.Join(release, "clawdline")); got != filepath.Join(release, "dist") {
		t.Fatalf("release layout: %q", got)
	}

	app := filepath.Join(t.TempDir(), "Clawdline Next.app", "Contents")
	put(filepath.Join(app, "Resources", "web"))
	if got := webRootBeside(filepath.Join(app, "MacOS", "clawdline")); got != filepath.Join(app, "Resources", "web") {
		t.Fatalf("bundle layout: %q", got)
	}

	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := webRootBeside(filepath.Join(checkout, "clawdline")); got != "" {
		t.Fatalf("a dist/ with no index.html is not a console: %q", got)
	}
}
