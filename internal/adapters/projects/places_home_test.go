package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The home directory is not offered just because a session started there, but
// once the person adds it, it is listed: on 2026-10-16 a fresh Linux machine's
// folder picker offered ~, the add answered success, and the dialog went on
// saying there were no projects.
func TestAnAddedHomeDirectoryIsListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	listed := func(p *Places) bool {
		for _, place := range p.List([]string{home}, 10) {
			if place.Path == home {
				return true
			}
		}
		return false
	}
	p := NewPlaces(func(string) string { return "" }, nil)
	if listed(p) {
		t.Fatal("the home directory is listed only because a session runs there")
	}
	p.Registered = func() ([]RegisteredPlace, error) {
		return []RegisteredPlace{{Path: home, AddedAt: 1}}, nil
	}
	if !listed(p) {
		t.Fatal("the home directory the person added is not listed")
	}
}

func TestCodexProjectDiscoveryKeepsUserIDEThreads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex", "sessions", time.Now().Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ source, thread string }{
		{"cli", "user"}, {"vscode", "user"}, {"exec", "user"}, {"vscode", "guardian"},
	} {
		body := fmt.Sprintf(`{"type":"session_meta","payload":{"cwd":"/project-%d","source":"%s","thread_source":"%s"}}`, i, tc.source, tc.thread)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("rollout-%d.jsonl", i)), []byte(body+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPlaces(nil, nil)
	for pass := 0; pass < 2; pass++ {
		rows := p.codexRecorded(60, 40)
		got := map[string]bool{}
		for _, row := range rows {
			got[row.Path] = true
		}
		if len(got) != 2 || !got["/project-0"] || !got["/project-1"] {
			t.Fatalf("discovery pass %d: %v", pass, rows)
		}
	}
}
