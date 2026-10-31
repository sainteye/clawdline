package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The response reader must share the published public-analytics admission bounds.
func TestPublicAnalyticsUIBoundsAreRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/cloud/admin/visits.ts"))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]Entry{}
	for _, row := range Register() {
		entries[row.Name] = row
	}
	for _, field := range []string{"days", "rows", "countries", "page", "readMs", "path", "activeMs"} {
		match := regexp.MustCompile(field + `: ([0-9]+)`).FindStringSubmatch(string(raw))
		if len(match) != 2 {
			t.Fatalf("missing analytics reader bound %s", field)
		}
		n, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		row, ok := entries["console.public_analytics_"+strings.ToLower(field)]
		if !ok {
			t.Errorf("unregistered public analytics bound: %s", field)
			continue
		}
		if row.Limit != n {
			t.Errorf("%s: registry %d, reader %d", field, row.Limit, n)
		}
	}
}
