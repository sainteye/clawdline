//go:build linux

package machineusage

import (
	"os"
	"time"
)

// Read takes one sample of this machine.
func Read() (Sample, error) {
	return ReadProc("/proc", int64(os.Getpagesize()), time.Now())
}

// Swap reads one process's swapped-out bytes.
var Swap = SwapOf("/proc")
