package capacity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestServiceMonitorBudgetIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "api/v1/service-monitor-budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]int64
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	rows := cloudServiceMonitoringBounds()
	if len(rows) != len(values) {
		t.Fatalf("budget has %d keys, registry has %d", len(values), len(rows))
	}
	for _, row := range rows {
		key := strings.TrimPrefix(row.Name, "cloud.service_monitor_")
		if values[key] != row.Limit {
			t.Errorf("%s: budget %d, registry %d", key, values[key], row.Limit)
		}
	}
}

func TestPerformanceBrowserBoundsAreRegistered(t *testing.T) {
	for _, spec := range []struct {
		file, prefix, name string
		keys               map[string]string
	}{
		{"web/console/src/cloud/admin/health/performance.ts", "console.service_performance_", "PERFORMANCE_LIMITS", map[string]string{"bodyBytes": "body_bytes", "points": "points", "routes": "routes", "deployments": "deployments", "alerts": "alerts", "refreshMs": "refresh_ms", "timeoutMs": "timeout_ms"}},
		{"web/console/src/machine/api-performance.ts", "console.local_performance_", "LOCAL_PERFORMANCE_LIMITS", map[string]string{"bodyBytes": "body_bytes", "points": "points", "routes": "routes", "refreshMs": "refresh_ms", "timeoutMs": "timeout_ms"}},
	} {
		raw, err := os.ReadFile(filepath.Join(moduleRoot(t), spec.file))
		if err != nil {
			t.Fatal(err)
		}
		block := regexp.MustCompile(`(?s)export const ` + spec.name + ` = \{(.*?)\} as const`).FindSubmatch(raw)
		if len(block) != 2 {
			t.Fatalf("missing %s", spec.name)
		}
		found := 0
		for _, m := range regexp.MustCompile(`(\w+):\s*(\d+)`).FindAllStringSubmatch(string(block[1]), -1) {
			key, ok := spec.keys[m[1]]
			if !ok {
				t.Errorf("unregistered browser guard %s", m[1])
				continue
			}
			found++
			n, _ := strconv.ParseInt(m[2], 10, 64)
			matched := false
			for _, row := range servicePerformanceUIBounds() {
				if row.Name == spec.prefix+key {
					matched = true
					if row.Limit != n {
						t.Errorf("%s: page %d registry %d", row.Name, n, row.Limit)
					}
				}
			}
			if !matched {
				t.Errorf("missing %s%s", spec.prefix, key)
			}
		}
		if found != len(spec.keys) {
			t.Errorf("found %d guards, expected %d", found, len(spec.keys))
		}
	}
}
