package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestTranscriptFillIsBounded(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web", "console", "src", "session", "history-fill.ts"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`export const HISTORY_FILL_PAGES = ([0-9]+)`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("history fill limit is missing")
	}
	limit, err := strconv.ParseInt(string(match[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range Register() {
		if e.Name == "console.transcript_fill_pages" {
			if e.Limit != limit || e.AtLimit != Refuse {
				t.Fatalf("automatic history fill must match its registered read budget: %+v", e)
			}
			return
		}
	}
	t.Fatal("automatic history fill has no capacity registration")
}

func TestTranscriptReadRecoveryIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web", "console", "src", "session", "transcript-trouble.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"console.transcript_slow_milliseconds":  "TRANSCRIPT_SLOW_MS",
		"console.transcript_quiet_milliseconds": "TRANSCRIPT_QUIET_MS",
		"console.transcript_quiet_failures":     "TRANSCRIPT_QUIET_FAILURES",
	} {
		match := regexp.MustCompile(`export const ` + source + ` = ([0-9_]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("missing transcript recovery bound %s", source)
		}
		limit, err := strconv.ParseInt(strings.ReplaceAll(string(match[1]), "_", ""), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range Register() {
			if e.Name == name {
				found = true
				if e.Limit != limit {
					t.Errorf("%s must match %s: got %d want %d", name, source, e.Limit, limit)
				}
			}
		}
		if !found {
			t.Errorf("transcript recovery has no capacity registration: %s", name)
		}
	}
}
