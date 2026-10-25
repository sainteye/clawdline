package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedLaunchImagesMatchDaemon(t *testing.T) {
	if err := generate(filepath.Join("..", "..", "..", "..", ".."), true); err != nil {
		t.Fatal(err)
	}
}

func TestMissingAndHTMLLaunchImagesAreRejected(t *testing.T) {
	root := t.TempDir()
	console := filepath.Join(root, "web", "console")
	public := filepath.Join(console, "public")
	if err := os.MkdirAll(public, 0755); err != nil {
		t.Fatal(err)
	}
	page := []byte(`<link rel="apple-touch-startup-image" href="/splash-16x32.png">`)
	if err := os.WriteFile(filepath.Join(console, "index.html"), page, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err == nil {
		t.Fatal("a missing asset passed")
	}
	if err := os.WriteFile(filepath.Join(public, "splash-16x32.png"), []byte("<html>SPA fallback</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err == nil {
		t.Fatal("the HTML fallback passed as a launch image")
	}
	if err := generate(root, false); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err != nil {
		t.Fatal(err)
	}
}
