package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
