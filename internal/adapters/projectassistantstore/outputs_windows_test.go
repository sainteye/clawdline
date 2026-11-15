package projectassistantstore

import "testing"

func TestOutputSnapshotRefusesFIFOWithoutOpeningIt(t *testing.T) {
	t.Skip("FIFO fixture requires Unix; regular-file snapshot checks run on every platform")
}
