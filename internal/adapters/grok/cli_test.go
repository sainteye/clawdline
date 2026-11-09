package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyTheCodingCLIIsCompatible(t *testing.T) {
	for _, tc := range []struct {
		name, version, help string
		compatible          bool
	}{
		{"coding CLI", "grok 1.0.77 (synthetic)", "Grok Build TUI\n--permission-mode\n--output-format\nagent", true},
		{"unrelated CLI", "grok 0.1.0", "Chat with xAI\n--api-key", false},
		{"editor", "grok 1.0.77", "Grok Build\n--permission-mode\n--output-format\nagent\n--install-extension", false},
		{"foreign version", "another CLI 1.0.77", "Grok Build\n--permission-mode\n--output-format\nagent", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "grok")
			if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
				t.Fatal(err)
			}
			calls := 0
			r := NewResolver([]string{path}, func(_ context.Context, _ string, flag string) ([]byte, error) {
				calls++
				if flag == "--version" {
					return []byte(tc.version), nil
				}
				return []byte(tc.help), nil
			})
			got := r.Snapshot(t.Context())
			if got.Compatible != tc.compatible || !got.InstalledKnown || !got.Installed || string(got.AuthState) != "unknown" {
				t.Fatalf("installation=%+v", got)
			}
			r.Snapshot(t.Context())
			if calls != 2 {
				t.Fatalf("unchanged probe repeated %d commands", calls)
			}
		})
	}
}
