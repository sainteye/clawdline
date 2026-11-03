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
func TestCommunityShareBoundsAreRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/community/share-metadata.ts"))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		field, row string
		scale      int64
	}{
		{"fetchMs", "console.community_share_fetch_seconds", 1000},
		{"responseBytes", "console.community_share_response_bytes", 1},
		{"textChars", "console.community_share_text_characters", 1},
	}
	entries := map[string]Entry{}
	for _, row := range Register() {
		entries[row.Name] = row
	}
	for _, check := range checks {
		match := regexp.MustCompile(check.field + `: ([0-9_]+)`).FindStringSubmatch(string(raw))
		if len(match) != 2 {
			t.Fatalf("missing community share bound %s", check.field)
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(match[1], "_", ""), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		row, ok := entries[check.row]
		if !ok {
			t.Errorf("unregistered community share bound: %s", check.row)
			continue
		}
		if row.Limit*check.scale != n {
			t.Errorf("%s: registry %d, UI %d", check.row, row.Limit*check.scale, n)
		}
	}
}
