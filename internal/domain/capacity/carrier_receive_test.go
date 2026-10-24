package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestCarrierReceiveBudgetsAreRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/cloud/status-client.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ constant, row string }{
		{"CARRIER_RECEIVE_BYTES", "console.carrier_receive_bytes"},
		{"CARRIER_RECEIVE_ROWS", "console.carrier_receive_rows"},
	} {
		match := regexp.MustCompile(`export const ` + spec.constant + ` = ([0-9]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("missing receive budget %s", spec.constant)
		}
		limit, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range Register() {
			if row.Name == spec.row {
				found = true
				if row.Limit != limit || row.AtLimit != Refuse {
					t.Errorf("receive budget mismatch: %+v", row)
				}
			}
		}
		if !found {
			t.Errorf("receive budget is unregistered: %s", spec.row)
		}
	}
}
