//go:build !windows

package projectassistantstore

import (
	"context"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"golang.org/x/sys/unix"
)

func TestOutputSnapshotRefusesFIFOWithoutOpeningIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (Outputs{Dir: t.TempDir()}).Snapshot(context.Background(), pa.BotKey{MachineID: "m", BotID: "b"}, root, path, false, func() error { return nil })
	if pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
}
