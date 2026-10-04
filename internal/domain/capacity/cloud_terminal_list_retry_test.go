package capacity

import "testing"

func TestCloudTerminalListRetryIsRegistered(t *testing.T) {
	for _, row := range Register() {
		if row.Name == CloudTerminalListRetry {
			if row.Limit != 1 {
				t.Fatalf("Cloud terminal list retry policy: %+v", row)
			}
			return
		}
	}
	t.Fatal("Cloud terminal list retry limit is not registered")
}
