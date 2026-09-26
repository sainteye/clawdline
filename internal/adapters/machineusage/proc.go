package machineusage

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReadProc takes one sample from a Linux /proc mounted at root. The parsing is
// here, outside a build tag, so a fixture tree checks it on every platform;
// only which root is read is Linux's (read_linux.go).
//
// A process that ends while it is being read is skipped: it is in the machine's
// counters and in no row, which is where Compute puts an ended process anyway.
func ReadProc(root string, pageSize int64, now time.Time) (Sample, error) {
	s := Sample{At: now, Procs: map[int]Proc{}}
	if err := readCPU(filepath.Join(root, "stat"), &s); err != nil {
		return Sample{}, err
	}
	if err := readMeminfo(filepath.Join(root, "meminfo"), &s); err != nil {
		return Sample{}, err
	}
	if raw, err := os.ReadFile(filepath.Join(root, "loadavg")); err == nil {
		f := strings.Fields(string(raw))
		for i := 0; i < 3 && i < len(f); i++ {
			s.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	s.Pressure = readPressure(filepath.Join(root, "pressure"))

	entries, err := os.ReadDir(root)
	if err != nil {
		return Sample{}, err
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue
		}
		p, ok := parseStat(pid, raw, pageSize)
		if ok {
			s.Procs[pid] = p
		}
	}
	return s, nil
}

// SwapOf reads one process's swapped-out bytes from /proc/<pid>/status. Zero
// when it cannot be read: the process ended, or the kernel keeps no count.
func SwapOf(root string) func(int) int64 {
	return func(pid int) int64 {
		raw, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "status"))
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if rest, ok := strings.CutPrefix(line, "VmSwap:"); ok {
				return kilobytes(rest)
			}
		}
		return 0
	}
}

// parseStat reads /proc/<pid>/stat. The command sits in parentheses and may
// itself hold spaces and parentheses, so the fields are counted from the last
// closing one.
func parseStat(pid int, raw []byte, pageSize int64) (Proc, bool) {
	open := bytes.IndexByte(raw, '(')
	close := bytes.LastIndexByte(raw, ')')
	if open < 0 || close < open {
		return Proc{}, false
	}
	f := strings.Fields(string(raw[close+1:]))
	// state(0) ppid(1) … utime(11) stime(12) … starttime(19) vsize(20) rss(21)
	if len(f) < 22 {
		return Proc{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	utime, err2 := strconv.ParseUint(f[11], 10, 64)
	stime, err3 := strconv.ParseUint(f[12], 10, 64)
	start, err4 := strconv.ParseUint(f[19], 10, 64)
	rss, err5 := strconv.ParseInt(f[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		return Proc{}, false
	}
	return Proc{PID: pid, PPID: ppid, Comm: string(raw[open+1 : close]),
		Ticks: utime + stime, Start: start, RSS: max(rss, 0) * pageSize}, true
}

// readCPU reads the aggregate line of /proc/stat and counts the cores.
// Guest time is already inside user time, so only the first eight columns are
// summed.
func readCPU(path string, s *Sample) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		if f[0] != "cpu" {
			s.Cores++
			continue
		}
		if len(f) < 5 {
			return fmt.Errorf("%s: the cpu line has %d columns", path, len(f))
		}
		for i := 1; i < len(f) && i <= 8; i++ {
			n, err := strconv.ParseUint(f[i], 10, 64)
			if err != nil {
				return fmt.Errorf("%s: column %d: %w", path, i, err)
			}
			s.CPUTotal += n
			if i == 4 || i == 5 { // idle, iowait
				s.CPUIdle += n
			}
		}
		found = true
	}
	if !found {
		return fmt.Errorf("%s: no cpu line", path)
	}
	return nil
}

func readMeminfo(path string, s *Sample) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			s.MemTotal = kilobytes(rest)
			seen++
		case "MemAvailable":
			s.MemAvailable = kilobytes(rest)
			seen++
		case "SwapTotal":
			s.SwapTotal = kilobytes(rest)
		case "SwapFree":
			s.SwapFree = kilobytes(rest)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if seen < 2 {
		return fmt.Errorf("%s: no MemTotal or MemAvailable", path)
	}
	return nil
}

// readPressure reads the ten-second averages. Nil when the kernel has no
// pressure files, which is not the same as no pressure.
func readPressure(dir string) *Pressure {
	avg10 := func(name, kind string) (float64, bool) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false
		}
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || f[0] != kind {
				continue
			}
			if v, ok := strings.CutPrefix(f[1], "avg10="); ok {
				n, err := strconv.ParseFloat(v, 64)
				return n, err == nil
			}
		}
		return 0, false
	}
	var p Pressure
	var ok [4]bool
	p.CPUSome, ok[0] = avg10("cpu", "some")
	p.MemorySome, ok[1] = avg10("memory", "some")
	p.MemoryFull, ok[2] = avg10("memory", "full")
	p.IOSome, ok[3] = avg10("io", "some")
	if !ok[0] && !ok[1] && !ok[2] && !ok[3] {
		return nil
	}
	return &p
}

// kilobytes reads " 1234 kB" as bytes.
func kilobytes(s string) int64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n * 1024
}
