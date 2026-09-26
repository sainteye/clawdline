package machineusage

import (
	"encoding/binary"
	"testing"
	"time"
)

// `ps -axo pid=,ppid=,rss=,time=,comm=` as a Mac prints it: right-aligned
// numbers, CPU time in minutes past sixty, and application paths with spaces.
const macPS = `    1     0  13024 12:45.10 /sbin/launchd
    0     0      0 1-02:03:04 kernel_task
  612     1 412336 123:04.55 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
  700   612  98304  0:03.20 /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/A/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)
 4242  4100 241664  2:03:04.50 claude
 4300  4242   6144  0:00.03 /bin/zsh
 garbage row
`

func TestAMacProcessListIsReadAsPSWritesIt(t *testing.T) {
	procs := parsePS(macPS)
	if len(procs) != 6 {
		t.Fatalf("%d processes: %+v", len(procs), procs)
	}
	// kernel_task is pid 0 and is the kernel's CPU time; it is kept.
	if k := procs[0]; k.Comm != "kernel_task" || k.Ticks != (86400+2*3600+3*60+4)*100 {
		t.Fatalf("kernel_task %+v", k)
	}
	chrome := procs[612]
	if chrome.Comm != "Google Chrome" || chrome.PPID != 1 || chrome.RSS != 412336*1024 || chrome.Ticks != (123*60+4)*100+55 {
		t.Fatalf("chrome %+v", chrome)
	}
	if helper := procs[700]; helper.Comm != "Google Chrome Helper (Renderer)" || helper.PPID != 612 {
		t.Fatalf("helper %+v", helper)
	}
	if c := procs[4242]; c.Comm != "claude" || c.Ticks != (2*3600+3*60+4)*100+50 {
		t.Fatalf("claude %+v", c)
	}
}

func TestPSCPUTimesInEveryShape(t *testing.T) {
	for in, want := range map[string]uint64{
		"0:00.03":    3,
		"13:57.86":   (13*60+57)*100 + 86,
		"123:04.55":  (123*60+4)*100 + 55,
		"2:03:04.50": (2*3600+3*60+4)*100 + 50,
		"1-02:03:04": (86400 + 2*3600 + 3*60 + 4) * 100,
		"0:00":       0,
	} {
		if got, ok := cpuTicks(in); !ok || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "12", "a:b", "1:2:3:4", "-1:00.00", "x-1:00"} {
		if _, ok := cpuTicks(bad); ok {
			t.Errorf("%q read as a time", bad)
		}
	}
}

// vm_stat as an Apple-silicon Mac prints it (16 KiB pages).
const macVMStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                3000.
Pages active:                            200000.
Pages inactive:                          190000.
Pages speculative:                         2000.
Pages throttled:                              0.
Pages wired down:                        100000.
Pages purgeable:                          10000.
"Translation faults":                 987654321.
Pages copy-on-write:                   12345678.
Pages zero filled:                    123456789.
Pages reactivated:                      1234567.
Pages purged:                            123456.
File-backed pages:                       150000.
Anonymous pages:                         240000.
Pages stored in compressor:              300000.
Pages occupied by compressor:             60000.
Decompressions:                         2345678.
Compressions:                           3456789.
Pageins:                                4567890.
Pageouts:                                 12345.
Swapins:                                  67890.
Swapouts:                                 78901.
`

func TestAMacsMemoryIsWhatActivityMonitorCallsUsed(t *testing.T) {
	v, err := parseVMStat(macVMStat)
	if err != nil {
		t.Fatal(err)
	}
	// app 240000-10000 + wired 100000 + compressor 60000 = 390000 pages
	if v.pageSize != 16384 || v.used() != 390000*16384 {
		t.Fatalf("%+v used %d", v, v.used())
	}
	if _, err := parseVMStat("Pages free: 3.\n"); err == nil {
		t.Fatal("a vm_stat with no page size read as memory")
	}
}

func TestTheSwapAndLoadSysctlsAreDecoded(t *testing.T) {
	swap := make([]byte, 32)
	binary.LittleEndian.PutUint64(swap[0:], 2<<30)  // total
	binary.LittleEndian.PutUint64(swap[8:], 1<<30)  // avail
	binary.LittleEndian.PutUint64(swap[16:], 1<<30) // used
	if total, used, ok := decodeSwapUsage(swap); !ok || total != 2<<30 || used != 1<<30 {
		t.Fatalf("swap %d %d %v", total, used, ok)
	}
	load := make([]byte, 24)
	for i, v := range []uint32{2048 * 3, 1024, 512} {
		binary.LittleEndian.PutUint32(load[i*4:], v)
	}
	binary.LittleEndian.PutUint64(load[16:], 2048)
	if l, ok := decodeLoadAvg(load); !ok || l != [3]float64{3, 0.5, 0.25} {
		t.Fatalf("load %v %v", l, ok)
	}
	if _, _, ok := decodeSwapUsage(swap[:8]); ok {
		t.Fatal("a short swapusage decoded")
	}
	if _, ok := decodeLoadAvg(make([]byte, 24)); ok {
		t.Fatal("a zero scale decoded")
	}
}

// Without machine counters, the machine's CPU is its processes' time over
// the cores and the interval, and a session's share is measured the same way.
func TestAMacsCPUIsItsProcessesTime(t *testing.T) {
	t0 := time.Unix(1000, 0)
	prev := Sample{At: t0, Cores: 4, ProcCPU: true, Procs: map[int]Proc{
		0:  {PID: 0, Comm: "kernel_task", Ticks: 1000},
		10: {PID: 10, PPID: 1, Comm: "claude", Ticks: 500},
		11: {PID: 11, PPID: 10, Comm: "go", Ticks: 50},
		// A pid reused inside the interval: its counter went backwards.
		20: {PID: 20, PPID: 1, Comm: "old", Ticks: 9000},
	}}
	cur := Sample{At: t0.Add(2 * time.Second), Cores: 4, ProcCPU: true, MemTotal: 8 << 30, MemAvailable: 4 << 30,
		Procs: map[int]Proc{
			0:  {PID: 0, Comm: "kernel_task", Ticks: 1040},
			10: {PID: 10, PPID: 1, Comm: "claude", Ticks: 520},
			11: {PID: 11, PPID: 10, Comm: "go", Ticks: 290},
			20: {PID: 20, PPID: 1, Comm: "new", Ticks: 60},
		}}
	u := Compute(prev, cur, []Root{{Key: "s", PID: 10}}, nil, 5)
	// 2 s × 4 cores = 800 ticks; busy 40 + 20 + 240 + 60 = 360
	if !near(u.CPUPercent, 45) {
		t.Fatalf("machine %.2f, want 45", u.CPUPercent)
	}
	if len(u.Groups) != 1 || !near(u.Groups[0].CPUPercent, 32.5) || u.Groups[0].Processes != 2 {
		t.Fatalf("session %+v, want 260 of 800", u.Groups)
	}
}
