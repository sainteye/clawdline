package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These bounds cross the TypeScript UI and the daemon's capacity contract.
func TestSupportUIBoundsAreRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/support/api.ts"))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		field, row string
		scale      int64
	}{
		{"requestMs", "console.support_request_seconds", 1000},
		{"refreshMs", "console.support_refresh_seconds", 1000},
		{"titleChars", "console.support_title_characters", 1},
		{"imageCount", "console.support_image_count", 1},
		{"imageRawBytes", "console.support_image_source_bytes", 1},
		{"imageBytes", "console.support_image_bytes", 1},
		{"imageEdge", "console.support_image_edge", 1},
		{"imageRequestMs", "console.support_image_upload_seconds", 1000},
	}
	entries := map[string]Entry{}
	for _, row := range Register() {
		entries[row.Name] = row
	}
	for _, check := range checks {
		match := regexp.MustCompile(check.field + `: ([0-9_]+)`).FindStringSubmatch(string(raw))
		if len(match) != 2 {
			t.Fatalf("missing support UI bound %s", check.field)
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(match[1], "_", ""), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		row, ok := entries[check.row]
		if !ok {
			t.Errorf("unregistered support UI bound: %s", check.row)
			continue
		}
		if row.Limit*check.scale != n {
			t.Errorf("%s: registry %d, UI %d", check.row, row.Limit*check.scale, n)
		}
	}
}
