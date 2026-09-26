//go:build !linux && !darwin

package machineusage

// Read has no reader on this platform yet. Windows keeps these counters behind
// the PDH counters and the toolhelp snapshot, a reader of its own.
func Read() (Sample, error) { return Sample{}, ErrUnsupported }

// Swap has nothing to read where Read has nothing.
func Swap(int) int64 { return 0 }
