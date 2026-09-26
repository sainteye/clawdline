//go:build !linux

package machineusage

// Read has no reader on this platform yet. macOS keeps these counters behind
// host_statistics and proc_pidinfo, and Windows behind the PDH counters; each is
// a reader of its own, not a guess from `ps`.
func Read() (Sample, error) { return Sample{}, ErrUnsupported }

// Swap has nothing to read where Read has nothing.
func Swap(int) int64 { return 0 }
