// Package machineusage reads how much of this machine is in use and which
// session is using it: the CPU and memory of the whole machine, and each
// session's share, summed over the tree of processes it started.
//
// It exists because "everything is slow" had two candidate causes that looked
// identical from a browser — this daemon answering slowly, or the machine under
// it out of memory — and telling them apart took a terminal, `vmstat`, `ps` and
// the sysstat history (2026-09-26: 2 cores, 1.9 GB, nine sessions and a Go
// build; load 26 and 800 pages a second swapped out while every read took
// 8-74 s, and 3 ms once the build ended). The dashboard beside the session
// counts is that terminal work, read continuously.
//
// A process's CPU time is a counter, so a share needs two readings. The
// arithmetic lives here, apart from the platform reader, so it can be checked
// with made-up samples.
package machineusage

import (
	"errors"
	"sort"
	"time"
)

// ErrUnsupported is the platform having no reader yet. It is a refusal to
// guess, not an empty machine: the route answers it as its own code.
var ErrUnsupported = errors.New("machine usage is read on Linux and macOS only")

// Proc is one process as a sample saw it.
type Proc struct {
	PID  int
	PPID int
	Comm string
	// Ticks is user plus system CPU time, in the same clock ticks as the
	// machine's CPU counters, so the two divide without a conversion.
	Ticks uint64
	// Start is when the process began, in ticks since boot. A pid with a
	// different start is a different process that reused the number.
	Start uint64
	RSS   int64 // bytes resident
}

// Pressure is the kernel's pressure-stall reading over the last ten seconds,
// as percentages of wall time. Absent where the kernel has none.
type Pressure struct {
	CPUSome, MemorySome, MemoryFull, IOSome float64
}

// Sample is one reading of the machine.
type Sample struct {
	At    time.Time
	Cores int
	// CPUTotal and CPUIdle are the machine's CPU counters summed over every
	// core; idle includes waiting for I/O.
	CPUTotal, CPUIdle uint64

	MemTotal, MemAvailable int64 // bytes
	SwapTotal, SwapFree    int64 // bytes
	Load                   [3]float64
	Pressure               *Pressure
	Procs                  map[int]Proc

	// ProcCPU says the machine has no CPU counters of its own here (a Mac
	// without cgo): its CPU is the sum of every process's CPU time over the
	// interval, and Ticks are in hundredths of a second. CPUTotal and CPUIdle
	// are unused.
	ProcCPU bool
}

// Root is a process whose tree is one row: a session's assistant, or this
// daemon.
type Root struct {
	Key string
	PID int
}

// Group is what one root's tree used over the interval.
type Group struct {
	Key        string
	PID        int
	Processes  int
	CPUPercent float64 // of the whole machine, all cores: 0-100
	RSS        int64
	Swap       int64
}

// Other is processes outside every root, gathered by name.
type Other struct {
	Comm       string
	Processes  int
	CPUPercent float64
	RSS        int64
}

// Usage is the difference between two samples.
type Usage struct {
	At         time.Time
	Interval   time.Duration
	Cores      int
	CPUPercent float64
	MemTotal   int64
	MemUsed    int64
	MemAvail   int64
	SwapTotal  int64
	SwapUsed   int64
	Load       [3]float64
	Pressure   *Pressure
	Groups     []Group
	Others     []Other
}

// maxDepth bounds the walk up a parent chain. A chain is at most as long as
// the process tree is deep; the bound only guards a cycle read mid-reuse.
const maxDepth = 256

// Compute is the usage between prev and cur, with each process given to the
// nearest root above it (itself included), so a session started from inside
// another is counted once, as itself. swapOf reads a process's swapped-out
// bytes; it is asked only for processes that belong to a root. topOthers is
// how many of the unowned names are kept, heaviest in memory first.
//
// A process that ended between the two samples is not counted: its time is in
// the machine's total and in no row. A process that began after prev counts all
// of its time, since all of it was spent inside the interval.
func Compute(prev, cur Sample, roots []Root, swapOf func(int) int64, topOthers int) Usage {
	u := Usage{
		At: cur.At, Interval: cur.At.Sub(prev.At), Cores: cur.Cores,
		MemTotal: cur.MemTotal, MemAvail: cur.MemAvailable,
		SwapTotal: cur.SwapTotal, Load: cur.Load, Pressure: cur.Pressure,
	}
	u.MemUsed = clamp(cur.MemTotal-cur.MemAvailable, 0, cur.MemTotal)
	u.SwapUsed = clamp(cur.SwapTotal-cur.SwapFree, 0, cur.SwapTotal)

	// A process seen before, with the same start and a counter that did not
	// go backwards, spent the difference; anything else is a process that
	// began inside the interval and spent all of its time in it. The counter
	// check is what tells a reused pid apart where there is no start time
	// (a Mac's `ps`).
	spent := func(p Proc) uint64 {
		if before, ok := prev.Procs[p.PID]; ok && before.Start == p.Start && p.Ticks >= before.Ticks {
			return p.Ticks - before.Ticks
		}
		return p.Ticks
	}

	var total uint64
	if cur.ProcCPU {
		if cur.At.After(prev.At) && cur.Cores > 0 {
			total = uint64(cur.At.Sub(prev.At).Seconds()*darwinTicksPerSecond*float64(cur.Cores) + 0.5)
		}
		if total > 0 {
			var busy uint64
			for _, p := range cur.Procs {
				busy += spent(p)
			}
			u.CPUPercent = percent(min(busy, total), total)
		}
	} else {
		if cur.CPUTotal > prev.CPUTotal {
			total = cur.CPUTotal - prev.CPUTotal
		}
		if total > 0 {
			idle := uint64(0)
			if cur.CPUIdle > prev.CPUIdle {
				idle = cur.CPUIdle - prev.CPUIdle
			}
			u.CPUPercent = percent(total-min(idle, total), total)
		}
	}

	index := map[int]int{}
	for i, r := range roots {
		if _, dup := index[r.PID]; dup || r.PID <= 0 {
			continue
		}
		index[r.PID] = i
	}
	groups := make([]Group, len(roots))
	ticks := make([]uint64, len(roots))
	for i, r := range roots {
		groups[i] = Group{Key: r.Key, PID: r.PID}
	}
	others := map[string]*Other{}
	otherTicks := map[string]uint64{}

	for _, p := range cur.Procs {
		owner, ok := ownerOf(p.PID, cur.Procs, index)
		if ok {
			g := &groups[owner]
			g.Processes++
			g.RSS += p.RSS
			if swapOf != nil {
				g.Swap += swapOf(p.PID)
			}
			ticks[owner] += spent(p)
			continue
		}
		o := others[p.Comm]
		if o == nil {
			o = &Other{Comm: p.Comm}
			others[p.Comm] = o
		}
		o.Processes++
		o.RSS += p.RSS
		otherTicks[p.Comm] += spent(p)
	}
	for i := range groups {
		if total > 0 {
			groups[i].CPUPercent = percent(min(ticks[i], total), total)
		}
	}
	// A root whose process is gone keeps no row: its session is ending.
	for i := range groups {
		if _, alive := cur.Procs[groups[i].PID]; alive {
			u.Groups = append(u.Groups, groups[i])
		}
	}
	for comm, o := range others {
		if total > 0 {
			o.CPUPercent = percent(min(otherTicks[comm], total), total)
		}
		u.Others = append(u.Others, *o)
	}
	sort.Slice(u.Others, func(i, j int) bool {
		if u.Others[i].RSS != u.Others[j].RSS {
			return u.Others[i].RSS > u.Others[j].RSS
		}
		return u.Others[i].Comm < u.Others[j].Comm
	})
	if len(u.Others) > topOthers {
		u.Others = u.Others[:max(topOthers, 0)]
	}
	return u
}

// ownerOf walks up from pid to the nearest root.
func ownerOf(pid int, procs map[int]Proc, roots map[int]int) (int, bool) {
	for depth := 0; depth < maxDepth && pid > 0; depth++ {
		if i, ok := roots[pid]; ok {
			return i, true
		}
		p, ok := procs[pid]
		if !ok || p.PPID == pid {
			return 0, false
		}
		pid = p.PPID
	}
	return 0, false
}

func percent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) * 100 / float64(whole)
}

func clamp(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if hi > lo && v > hi {
		return hi
	}
	return v
}
