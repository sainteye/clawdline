package capacity

import "testing"

func TestCloudTerminalEarlyFrameBufferIsRegistered(t *testing.T) {
	for _, row := range Register() {
		if row.Name != CloudTerminalEarlyFrames {
			continue
		}
		if row.Class != Buffer || row.Unit != Rows || row.Limit != 1 || row.AtLimit != Coalesce {
			t.Fatalf("Cloud terminal early frame buffer policy: %+v", row)
		}
		return
	}
	t.Fatal("Cloud terminal early frame buffer is not registered")
}
