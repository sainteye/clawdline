package http

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestReplayLongToolBodyTraffic(t *testing.T) {
	directory := os.Getenv("CLAWDLINE_TOOL_BODY_REPLAY_DIR")
	if directory == "" {
		t.Skip("explicit private export required")
	}
	total, compact, rows, plain, patch := 0, 0, 0, 0, 0
	paths, err := filepath.Glob(filepath.Join(directory, "transcript-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	namePattern := regexp.MustCompile(`^transcript-\d+\.json$`)
	pages := 0
	for _, path := range paths {
		if !namePattern.MatchString(filepath.Base(path)) {
			continue
		}
		pages++
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var page map[string]json.RawMessage
		if err = json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		var entries []contract.TranscriptEntry
		if err = json.Unmarshal(page["entries"], &entries); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(page)
		total += len(before)
		rows += len(entries)
		for index, e := range entries {
			entries[index] = compactEntryDetails(e, transcript.Entry{Before: int64(index)}, "fixed-private-replay")
			if entries[index].Detail != nil {
				if len(e.FileChanges) > 0 {
					patch++
				} else {
					plain++
				}
			}
		}
		page["entries"], err = json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(page)
		compact += len(after)
	}
	if pages == 0 {
		t.Fatal("no transcript-N.json pages in the explicit export")
	}
	fmt.Printf("rows=%d pages=%d baseline_json=%d lazy_json=%d plain_lazy=%d patch_lazy=%d reduction=%.4f\n", rows, pages, total, compact, plain, patch, 1-float64(compact)/float64(total))
}
