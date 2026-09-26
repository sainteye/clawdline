//go:build darwin

package machineusage

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// readTimeout bounds each command one reading runs. `ps` over a few hundred
// processes answers in tens of milliseconds; a Mac that is swapping hard can
// take seconds, and the dashboard would rather say it could not read than hang.
const readTimeout = 10 * time.Second

// Read takes one sample of this Mac: processes from `ps`, memory from
// `vm_stat` and hw.memsize, swap and load from their sysctls. The CPU is the
// processes' own (ProcCPU; darwin.go says why).
func Read() (Sample, error) {
	s := Sample{At: time.Now(), Cores: runtime.NumCPU(), ProcCPU: true}

	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return Sample{}, fmt.Errorf("hw.memsize: %w", err)
	}
	s.MemTotal = int64(total)
	vm, err := run("/usr/bin/vm_stat")
	if err != nil {
		return Sample{}, err
	}
	v, err := parseVMStat(vm)
	if err != nil {
		return Sample{}, err
	}
	s.MemAvailable = max(s.MemTotal-v.used(), 0)

	if raw, err := unix.SysctlRaw("vm.swapusage"); err == nil {
		if t, u, ok := decodeSwapUsage(raw); ok {
			s.SwapTotal, s.SwapFree = t, max(t-u, 0)
		}
	}
	if raw, err := unix.SysctlRaw("vm.loadavg"); err == nil {
		if l, ok := decodeLoadAvg(raw); ok {
			s.Load = l
		}
	}

	ps, err := run("/bin/ps", "-axo", "pid=,ppid=,rss=,time=,comm=")
	if err != nil {
		return Sample{}, err
	}
	s.Procs = parsePS(ps)
	if len(s.Procs) == 0 {
		return Sample{}, fmt.Errorf("ps listed no processes")
	}
	// The reading is of the moment `ps` finished, which is what the next
	// reading's interval is measured from.
	s.At = time.Now()
	return s, nil
}

// Swap has no per-process reading on a Mac without privilege: task_info on
// another process needs it. The rows show resident memory only.
func Swap(int) int64 { return 0 }

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}
