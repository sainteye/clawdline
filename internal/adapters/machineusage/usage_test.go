package machineusage

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// A session's share is its whole tree: the assistant, the shell it opened and
// the compiler that shell started. A session opened from inside another is
// counted as itself, once, and not again in the one above it.
func TestASessionIsItsWholeTreeAndANestedSessionIsItsOwn(t *testing.T) {
	t0 := time.Unix(1000, 0)
	prev := Sample{At: t0, CPUTotal: 1000, CPUIdle: 800, Procs: map[int]Proc{
		1:   {PID: 1, PPID: 0, Comm: "systemd", Ticks: 10, Start: 1},
		100: {PID: 100, PPID: 1, Comm: "claude", Ticks: 50, Start: 5},
		101: {PID: 101, PPID: 100, Comm: "bash", Ticks: 1, Start: 6},
		200: {PID: 200, PPID: 101, Comm: "claude", Ticks: 20, Start: 7},
	}}
	cur := Sample{At: t0.Add(2 * time.Second), Cores: 2, CPUTotal: 1400, CPUIdle: 900,
		MemTotal: 2000, MemAvailable: 500, SwapTotal: 1000, SwapFree: 400,
		Procs: map[int]Proc{
			1:   {PID: 1, PPID: 0, Comm: "systemd", Ticks: 14, Start: 1, RSS: 5},
			100: {PID: 100, PPID: 1, Comm: "claude", Ticks: 90, Start: 5, RSS: 100},
			101: {PID: 101, PPID: 100, Comm: "bash", Ticks: 1, Start: 6, RSS: 3},
			// Began inside the interval: all of its time was spent in it.
			102: {PID: 102, PPID: 101, Comm: "compile", Ticks: 200, Start: 900, RSS: 400},
			200: {PID: 200, PPID: 101, Comm: "claude", Ticks: 60, Start: 7, RSS: 80},
		}}
	swap := map[int]int64{100: 7, 200: 11}
	u := Compute(prev, cur, []Root{{Key: "a", PID: 100}, {Key: "b", PID: 200}},
		func(pid int) int64 { return swap[pid] }, 5)

	if !near(u.CPUPercent, 75) {
		t.Fatalf("machine cpu %.2f, want 75 (300 busy of 400)", u.CPUPercent)
	}
	if u.MemUsed != 1500 || u.SwapUsed != 600 || u.Interval != 2*time.Second {
		t.Fatalf("memory %d swap %d interval %s", u.MemUsed, u.SwapUsed, u.Interval)
	}
	if len(u.Groups) != 2 {
		t.Fatalf("groups %+v", u.Groups)
	}
	a, b := u.Groups[0], u.Groups[1]
	// a: claude 40 + bash 0 + compile 200 = 240 of 400
	if a.Key != "a" || a.Processes != 3 || a.RSS != 503 || a.Swap != 7 || !near(a.CPUPercent, 60) {
		t.Fatalf("session a %+v", a)
	}
	if b.Key != "b" || b.Processes != 1 || b.RSS != 80 || b.Swap != 11 || !near(b.CPUPercent, 10) {
		t.Fatalf("session b %+v", b)
	}
	if len(u.Others) != 1 || u.Others[0].Comm != "systemd" || u.Others[0].RSS != 5 || !near(u.Others[0].CPUPercent, 1) {
		t.Fatalf("others %+v", u.Others)
	}
}

// A pid reused by a different process is a new process: its counter is not
// subtracted from the one that had the number before.
func TestAReusedPidIsANewProcess(t *testing.T) {
	prev := Sample{At: time.Unix(0, 0), CPUTotal: 0, Procs: map[int]Proc{
		9: {PID: 9, PPID: 1, Ticks: 500, Start: 3},
	}}
	cur := Sample{At: time.Unix(1, 0), CPUTotal: 100, Procs: map[int]Proc{
		9: {PID: 9, PPID: 1, Ticks: 30, Start: 70},
	}}
	u := Compute(prev, cur, []Root{{Key: "x", PID: 9}}, nil, 5)
	if len(u.Groups) != 1 || !near(u.Groups[0].CPUPercent, 30) {
		t.Fatalf("groups %+v", u.Groups)
	}
}

// A session whose process is gone has no row, and a counter that went
// backwards reads as nothing spent rather than as a huge number.
func TestAGoneRootHasNoRowAndNoCounterWrapsAround(t *testing.T) {
	prev := Sample{At: time.Unix(0, 0), CPUTotal: 100, CPUIdle: 50, Procs: map[int]Proc{
		5: {PID: 5, PPID: 1, Ticks: 90, Start: 2},
	}}
	cur := Sample{At: time.Unix(1, 0), CPUTotal: 90, CPUIdle: 40, Procs: map[int]Proc{
		5: {PID: 5, PPID: 1, Ticks: 10, Start: 2},
	}}
	u := Compute(prev, cur, []Root{{Key: "gone", PID: 77}, {Key: "here", PID: 5}}, nil, 5)
	if u.CPUPercent != 0 || len(u.Groups) != 1 || u.Groups[0].Key != "here" || u.Groups[0].CPUPercent != 0 {
		t.Fatalf("usage %+v", u)
	}
}

// The others are the heaviest names in memory, as many as asked for.
func TestTheOthersAreTheHeaviestNames(t *testing.T) {
	cur := Sample{At: time.Unix(1, 0), Procs: map[int]Proc{
		2: {PID: 2, PPID: 1, Comm: "a", RSS: 1},
		3: {PID: 3, PPID: 1, Comm: "b", RSS: 9},
		4: {PID: 4, PPID: 1, Comm: "b", RSS: 1},
		5: {PID: 5, PPID: 1, Comm: "c", RSS: 5},
	}}
	u := Compute(Sample{At: time.Unix(0, 0)}, cur, nil, nil, 2)
	if len(u.Others) != 2 || u.Others[0].Comm != "b" || u.Others[0].Processes != 2 || u.Others[0].RSS != 10 || u.Others[1].Comm != "c" {
		t.Fatalf("others %+v", u.Others)
	}
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The reader against a /proc laid out as Linux lays it out, including a
// command with a space and a parenthesis in its name.
func TestAProcTreeIsReadAsLinuxWritesIt(t *testing.T) {
	root := t.TempDir()
	write(t, root, "stat", "cpu  100 5 50 800 40 0 5 0 7 0\ncpu0 50 2 25 400 20 0 3 0 0 0\ncpu1 50 3 25 400 20 0 2 0 0 0\nintr 1\n")
	write(t, root, "meminfo", "MemTotal:        2000 kB\nMemFree:  10 kB\nMemAvailable:     500 kB\nSwapTotal:  1000 kB\nSwapFree:  400 kB\n")
	write(t, root, "loadavg", "14.13 5.62 2.20 3/400 12345\n")
	write(t, root, "pressure/memory", "some avg10=36.54 avg60=37.32 avg300=18.25 total=1\nfull avg10=4.95 avg60=17.88 avg300=10.90 total=1\n")
	write(t, root, "42/stat", "42 (tmux: server (x)) S 1 42 42 0 -1 4194560 1 0 0 0 30 12 0 0 20 0 1 0 555 1000 25 18446744073709551615\n")
	write(t, root, "42/status", "Name:\ttmux\nVmSwap:\t    2048 kB\n")
	write(t, root, "self/stat", "not a pid")
	write(t, root, "43/stat", "garbage")

	s, err := ReadProc(root, 4096, time.Unix(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if s.Cores != 2 || s.CPUTotal != 1000 || s.CPUIdle != 840 {
		t.Fatalf("cpu: cores %d total %d idle %d", s.Cores, s.CPUTotal, s.CPUIdle)
	}
	if s.MemTotal != 2000*1024 || s.MemAvailable != 500*1024 || s.SwapTotal != 1000*1024 || s.SwapFree != 400*1024 {
		t.Fatalf("memory %+v", s)
	}
	if s.Load != [3]float64{14.13, 5.62, 2.20} {
		t.Fatalf("load %v", s.Load)
	}
	if s.Pressure == nil || s.Pressure.MemorySome != 36.54 || s.Pressure.MemoryFull != 4.95 || s.Pressure.CPUSome != 0 {
		t.Fatalf("pressure %+v", s.Pressure)
	}
	p, ok := s.Procs[42]
	if !ok || len(s.Procs) != 1 {
		t.Fatalf("procs %+v", s.Procs)
	}
	if p.Comm != "tmux: server (x)" || p.PPID != 1 || p.Ticks != 42 || p.Start != 555 || p.RSS != 25*4096 {
		t.Fatalf("proc %+v", p)
	}
	if got := SwapOf(root)(42); got != 2048*1024 {
		t.Fatalf("swap %d", got)
	}
	if got := SwapOf(root)(99); got != 0 {
		t.Fatalf("swap of a gone process %d", got)
	}
}

// A machine with no /proc/stat is an error, not an idle machine.
func TestAMissingProcIsAnError(t *testing.T) {
	if _, err := ReadProc(t.TempDir(), 4096, time.Now()); err == nil {
		t.Fatal("an empty root read as a machine")
	}
}

// The first request waits for a second sample; a second request inside the
// minimum interval is the same answer; a later one is measured from the last.
func TestTheSamplerMeasuresBetweenItsOwnReadings(t *testing.T) {
	var reads int
	clock := time.Now()
	read := func() (Sample, error) {
		reads++
		clock = clock.Add(time.Second)
		return Sample{At: clock, CPUTotal: uint64(reads) * 100, CPUIdle: uint64(reads) * 50,
			Procs: map[int]Proc{}}, nil
	}
	var slept []time.Duration
	s := newSampler(read, nil, func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil })

	u, err := s.Usage(context.Background(), nil)
	if err != nil || reads != 2 || len(slept) != 1 || !near(u.CPUPercent, 50) {
		t.Fatalf("first: %+v %v reads=%d slept=%v", u, err, reads, slept)
	}
	// The fake clock is a second ahead of the wall clock per read, so the
	// cached answer is "recent" by time.Since only when At is not in the future
	// by more than minInterval; ask with the same rows right away.
	s.last.At = time.Now()
	if _, err := s.Usage(context.Background(), nil); err != nil || reads != 2 {
		t.Fatalf("a second ask inside the interval read again: reads=%d", reads)
	}
	s.last.At = time.Now().Add(-time.Minute)
	if _, err := s.Usage(context.Background(), nil); err != nil || reads != 3 || len(slept) != 1 {
		t.Fatalf("a later ask: reads=%d slept=%v", reads, slept)
	}
	// Different rows are a different question.
	s.last.At = time.Now()
	if _, err := s.Usage(context.Background(), []Root{{Key: "k", PID: 1}}); err != nil || reads != 4 {
		t.Fatalf("new rows reused the answer: reads=%d", reads)
	}
}

// A reader that fails is the answer's failure; nothing is cached from it.
func TestTheSamplerCarriesTheReadersFailure(t *testing.T) {
	s := newSampler(func() (Sample, error) { return Sample{}, ErrUnsupported }, nil, sleepCtx)
	if _, err := s.Usage(context.Background(), nil); !errors.Is(err, ErrUnsupported) || s.prev != nil {
		t.Fatalf("err %v prev %v", err, s.prev)
	}
}
