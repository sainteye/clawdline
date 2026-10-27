package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestVideoPreviewBudgetsMatchTheBrowser(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/session/video-poster.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		symbol, name string
		value        int64
	}{
		{"READS", "reads", MaximumVideoPreviewReads},
		{"SIDE", "side", MaximumVideoPreviewSide},
		{"BYTES", "bytes", MaximumVideoPreviewBytes},
		{"MILLISECONDS", "milliseconds", MaximumVideoPreviewMilliseconds},
	} {
		match := regexp.MustCompile(`export const VIDEO_PREVIEW_` + row.symbol + ` = ([0-9]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("missing browser budget %s", row.symbol)
		}
		value, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil || value != row.value {
			t.Fatalf("browser budget mismatch: %s", row.symbol)
		}
		found := false
		for _, entry := range Register() {
			if entry.Name == "console.video_preview_"+row.name {
				found = true
				if entry.Limit != value || entry.AtLimit != Refuse {
					t.Fatalf("registered budget mismatch: %+v", entry)
				}
			}
		}
		if !found {
			t.Fatalf("unregistered preview budget: %s", row.name)
		}
	}
}
