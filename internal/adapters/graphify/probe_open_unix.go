//go:build unix

package graphify

import (
	"os"
	"syscall"
)

// Opening an attacker-replaced FIFO must not wait for a writer before the
// regular-file check can reject it. Regular files ignore O_NONBLOCK.
func openProbeFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
