package capacity

import "testing"

func TestCloudTerminalObservationRowsIsRegistered(t *testing.T) {
	for _, row := range Register() {
		if row.Name != CloudTerminalObservationRows {
			continue
		}
		if row.Class != Observation || row.Unit != Rows || row.Limit != 128 || row.AtLimit != EvictOldest || row.Deviation != "" {
			t.Fatalf("Cloud terminal browser observation policy: %+v", row)
		}
		return
	}
	t.Fatal("Cloud terminal browser observation limit is not registered")
}
