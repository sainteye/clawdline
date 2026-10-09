package terminal

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// Every run is counted under its kind, with its failures, its summed time and
// its longest, and the hourly window starts again when it is taken while the
// totals since start keep counting.
func TestOsascriptRunsAreCountedByKind(t *testing.T) {
	start := time.Unix(1000, 0)
	c := newOsascriptCounter(start)
	c.record("list", 200*time.Millisecond, false)
	c.record("list", 900*time.Millisecond, true)
	c.record("capture", 50*time.Millisecond, false)

	total, hour := c.read()
	if len(total.Kinds) != 2 || total.Kinds[0].Kind != "capture" || total.Kinds[1].Kind != "list" {
		t.Fatalf("kinds are not ordered by name: %+v", total.Kinds)
	}
	list := total.Kinds[1]
	if list.Runs != 2 || list.Failures != 1 || list.Total != 1100*time.Millisecond || list.Max != 900*time.Millisecond {
		t.Fatalf("list counted as %+v", list)
	}
	if runs, failures, sum, longest := hour.Runs(); runs != 3 || failures != 1 ||
		sum != 1150*time.Millisecond || longest != 900*time.Millisecond {
		t.Fatalf("hour sums: runs=%d failures=%d total=%s max=%s", runs, failures, sum, longest)
	}

	taken := c.take(start.Add(time.Hour))
	if runs, _, _, _ := taken.Runs(); runs != 3 || !taken.Since.Equal(start) {
		t.Fatalf("the taken window: %+v", taken)
	}
	c.record("send", time.Second, false)
	total, hour = c.read()
	if runs, _, _, _ := total.Runs(); runs != 4 {
		t.Fatalf("the totals lost runs when the window was taken: %d", runs)
	}
	if len(hour.Kinds) != 1 || hour.Kinds[0].Kind != "send" || !hour.Since.Equal(start.Add(time.Hour)) {
		t.Fatalf("the next window: %+v", hour)
	}
}

// Past the kinds limit a new kind is counted as `other`: the table stops
// growing and no run goes uncounted.
func TestOsascriptKindsPastTheLimitAreOther(t *testing.T) {
	c := newOsascriptCounter(time.Unix(0, 0))
	for n := 0; n < osascriptKindsLimit+10; n++ {
		c.record(fmt.Sprintf("kind-%02d", n), time.Millisecond, false)
	}
	total, _ := c.read()
	if len(total.Kinds) != osascriptKindsLimit {
		t.Fatalf("%d kinds kept, want %d", len(total.Kinds), osascriptKindsLimit)
	}
	runs, _, _, _ := total.Runs()
	if runs != int64(osascriptKindsLimit+10) {
		t.Fatalf("%d runs counted of %d", runs, osascriptKindsLimit+10)
	}
	var other OsascriptCount
	for _, k := range total.Kinds {
		if k.Kind == osascriptOther {
			other = k
		}
	}
	if other.Runs != 11 {
		t.Fatalf("other counted %d runs, want 11", other.Runs)
	}
}

// The switch defaults to on, and a refusal while it is off is Unsent: nothing
// reached iTerm2, so a retry cannot type a line twice.
func TestITermScanSwitch(t *testing.T) {
	if !ITermScan() {
		t.Fatal("iTerm2 scanning is off before anything turned it off")
	}
	SetITermScan(false)
	t.Cleanup(func() { SetITermScan(true) })
	if ITermScan() {
		t.Fatal("SetITermScan(false) left it on")
	}
	var unsent Unsent
	if err := error(ITermScanOff{Op: "type"}); !errors.As(err, &unsent) {
		t.Fatalf("ITermScanOff does not unwrap to Unsent: %v", err)
	}
}
