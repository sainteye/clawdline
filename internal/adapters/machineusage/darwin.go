package machineusage

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// The macOS reader's parsing, outside a build tag so this machine's tests read
// the formats a Mac writes. Which commands and sysctls are asked is Darwin's
// alone (read_darwin.go).
//
// A Mac keeps its CPU counters behind host_statistics, which needs cgo, and
// this repository builds with CGO_ENABLED=0. So on a Mac the machine's CPU is
// the sum of every process's CPU time between two readings — kernel_task
// included — over the cores and the interval: a sample with ProcCPU set.
// Time spent by a process that ended between the two readings is the part it
// misses, which is also the part Linux's per-session figures miss.

// darwinTicksPerSecond is the unit a Mac sample's Ticks are in: `ps` prints
// CPU time to the hundredth of a second.
const darwinTicksPerSecond = 100

// parsePS reads `ps -axo pid=,ppid=,rss=,time=,comm=`: RSS in KiB, the
// accumulated CPU time as [[dd-]hh:]mm:ss.ss, and the executable's path last,
// which may itself hold spaces. A row that does not read is skipped: it is a
// process that changed while `ps` was printing it.
func parsePS(out string) map[int]Proc {
	procs := map[int]Proc{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		ticks, ok := cpuTicks(f[3])
		// pid 0 is kernel_task on a Mac: the kernel's own time, which the
		// machine's CPU figure needs. No session's tree can hold it.
		if err1 != nil || err2 != nil || err3 != nil || !ok || pid < 0 {
			continue
		}
		// The path is everything after the fourth field, rejoined as ps
		// wrote it; the row is named by the executable's own name.
		comm := strings.Join(f[4:], " ")
		procs[pid] = Proc{PID: pid, PPID: ppid, Comm: filepath.Base(comm), Ticks: ticks, RSS: max(rss, 0) * 1024}
	}
	return procs
}

// cpuTicks reads ps's CPU time — "0:00.03", "13:57.86", "2:03:04.50",
// "1-02:03:04" — as hundredths of a second.
func cpuTicks(s string) (uint64, bool) {
	days := 0.0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil || n < 0 {
			return 0, false
		}
		days, s = float64(n), rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || secs < 0 {
		return 0, false
	}
	total := days*86400 + secs
	mult := 60.0
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return 0, false
		}
		total += float64(n) * mult
		mult *= 60
	}
	return uint64(total*darwinTicksPerSecond + 0.5), true
}

// vmStat is the part of `vm_stat` the memory figure needs, in pages.
type vmStat struct {
	pageSize                              int64
	free, speculative, wired, purgeable   int64
	anonymous, compressor, fileBacked     int64
	sawAnonymous, sawWired, sawCompressor bool
}

// parseVMStat reads `vm_stat`: a header naming the page size, then one
// "Name: count." row each.
func parseVMStat(out string) (vmStat, error) {
	var v vmStat
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "page size of "); i >= 0 {
			f := strings.Fields(line[i+len("page size of "):])
			if len(f) > 0 {
				v.pageSize, _ = strconv.ParseInt(f[0], 10, 64)
			}
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64)
		if err != nil {
			continue
		}
		switch strings.Trim(strings.TrimSpace(name), `"`) {
		case "Pages free":
			v.free = n
		case "Pages speculative":
			v.speculative = n
		case "Pages wired down":
			v.wired, v.sawWired = n, true
		case "Pages purgeable":
			v.purgeable = n
		case "Anonymous pages":
			v.anonymous, v.sawAnonymous = n, true
		case "Pages occupied by compressor":
			v.compressor, v.sawCompressor = n, true
		case "File-backed pages":
			v.fileBacked = n
		}
	}
	if v.pageSize <= 0 || !v.sawAnonymous || !v.sawWired || !v.sawCompressor {
		return vmStat{}, fmt.Errorf("vm_stat: no page size, anonymous, wired or compressor row")
	}
	return v, nil
}

// used is Activity Monitor's "Memory Used": app memory (anonymous pages less
// the purgeable ones), wired, and what the compressor occupies. File-backed
// pages are cache the system gives back when asked, so they count as
// available, as they do on Linux.
func (v vmStat) used() int64 {
	app := max(v.anonymous-v.purgeable, 0)
	return (app + v.wired + v.compressor) * v.pageSize
}

// decodeSwapUsage reads the vm.swapusage sysctl, a struct xsw_usage:
// total, avail and used as uint64 bytes, then the page size and a flag.
func decodeSwapUsage(b []byte) (total, used int64, ok bool) {
	if len(b) < 24 {
		return 0, 0, false
	}
	t := binary.LittleEndian.Uint64(b[0:8])
	u := binary.LittleEndian.Uint64(b[16:24])
	return int64(t), int64(u), true
}

// decodeLoadAvg reads the vm.loadavg sysctl, a struct loadavg: three
// fixed-point uint32 averages, padding, and the scale as a 64-bit long.
func decodeLoadAvg(b []byte) ([3]float64, bool) {
	var out [3]float64
	if len(b) < 24 {
		return out, false
	}
	scale := float64(binary.LittleEndian.Uint64(b[16:24]))
	if scale <= 0 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		out[i] = float64(binary.LittleEndian.Uint32(b[i*4:])) / scale
	}
	return out, true
}
