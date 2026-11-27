package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The console's read waits, its schedule retry ladder, its one-read-per-target
// rule and how long a read is held are numbers in TypeScript. Each is read from its source file and
// compared with its registered row, so neither can move without the other.
func TestConsoleReadWaitsAreRegistered(t *testing.T) {
	root := moduleRoot(t)
	for _, spec := range []struct {
		file, constant, row string
		unit                Unit
		atLimit             Action
	}{
		{"web/console/src/provider-quota/QuotaBar.tsx", "QUOTA_READ_DEADLINE_MS", "console.quota_read_wait_milliseconds", Milliseconds, Refuse},
		{"web/console/src/pages/schedules-lane.ts", "SCHEDULE_READ_DEADLINE_MS", "console.schedule_read_wait_milliseconds", Milliseconds, Refuse},
		{"web/console/src/pages/schedules-lane.ts", "SCHEDULE_RETRY_FIRST_MS", "console.schedule_retry_first_milliseconds", Milliseconds, Expire},
		{"web/console/src/pages/schedules-lane.ts", "SCHEDULE_RETRY_CEILING_MS", "console.schedule_retry_ceiling_milliseconds", Milliseconds, Expire},
		{"web/console/src/poll.ts", "READS_OUT_PER_TARGET", "console.reads_out_per_target", Rows, Coalesce},
		{"web/console/src/poll.ts", "HELD_READ_LOST_MS", "console.held_read_lost_milliseconds", Milliseconds, Expire},
		{"web/console/src/client.ts", "FLEET_RECONNECT_FIRST_MS", "console.fleet_retry_first_milliseconds", Milliseconds, Refuse},
		{"web/console/src/client.ts", "FLEET_RECONNECT_CEILING_MS", "console.fleet_retry_ceiling_milliseconds", Milliseconds, Refuse},
	} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec.file)))
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(`(?m)^(?:export )?const ` + spec.constant + ` = ([0-9_]+)\s*$`).FindSubmatch(raw)
		if match == nil {
			t.Errorf("%s no longer declares %s", spec.file, spec.constant)
			continue
		}
		limit, err := strconv.ParseInt(strings.ReplaceAll(string(match[1]), "_", ""), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range Register() {
			if row.Name != spec.row {
				continue
			}
			found = true
			if row.Limit != limit || row.Unit != spec.unit || row.AtLimit != spec.atLimit {
				t.Errorf("%s = %d in %s, registered as %+v", spec.constant, limit, spec.file, row)
			}
		}
		if !found {
			t.Errorf("%s (%s = %d) is unregistered: no row named %s", spec.file, spec.constant, limit, spec.row)
		}
	}
}
