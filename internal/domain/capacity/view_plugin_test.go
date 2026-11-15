package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The browser works with old daemons too; its compiled bounds must keep the
// registered policy rather than silently depending on a newer server response.
func TestViewPluginBoundsAreRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/view-plugins/limits.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ field, row string }{
		{"archiveBytes", "archive_bytes"}, {"expandedBytes", "expanded_bytes"},
		{"manifestBytes", "manifest_bytes"}, {"packages", "packages"},
		{"files", "files"},
		{"messageBytes", "message_bytes"}, {"sessions", "sessions"},
		{"pending", "pending"}, {"requestMilliseconds", "request_milliseconds"},
		{"requestsPerSecond", "requests_per_second"}, {"textCharacters", "text_characters"},
	} {
		m := regexp.MustCompile(c.field + `: ([0-9]+)`).FindSubmatch(raw)
		if len(m) != 2 {
			t.Fatalf("missing browser plugin bound %s", c.field)
		}
		n, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := Default("console.view_plugin_" + c.row); got != n {
			t.Errorf("%s: browser %d, register %d", c.field, n, got)
		}
	}
	sdk, err := os.ReadFile(filepath.Join(moduleRoot(t), "plugins/sdk/view-plugin-v1.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		limit int64
	}{{"CONNECT_MS", MaximumViewPluginRequestMilliseconds}, {"MAX_PENDING", MaximumViewPluginPending}} {
		m := regexp.MustCompile(c.name + ` = ([0-9]+)`).FindSubmatch(sdk)
		if len(m) != 2 || string(m[1]) != strconv.FormatInt(c.limit, 10) {
			t.Errorf("SDK %s disagrees with registered policy", c.name)
		}
	}
}
