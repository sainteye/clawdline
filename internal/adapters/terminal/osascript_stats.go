package terminal

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// iTerm2 integration requires a new explicit opt-in. Existing iterm_scan
// settings and macOS Automation grants never enable it implicitly.
var itermIntegration atomic.Bool
var itermScanOff atomic.Bool

func SetITermIntegration(on bool) { itermIntegration.Store(on) }
func ITermIntegration() bool      { return itermIntegration.Load() }

// SetITermScan preserves the optional polling preference inside the integration.
func SetITermScan(on bool) { itermScanOff.Store(!on) }
func ITermScan() bool      { return ITermIntegration() && !itermScanOff.Load() }

// A requested action may bypass polling, but never the integration consent.
func itermMayAct(ctx context.Context) bool {
	return ITermIntegration() && (ITermScan() || ports.WasAsked(ctx))
}

// ITermScanDisabledBy is the typed reason a disabled iTerm2 source carries on
// a reading and on the wire.
const ITermScanDisabledBy = "setting"

// ITermScanOff is an action on an iTerm2 session refused because scanning is
// turned off. Nothing was sent to iTerm2, so it unwraps to Unsent: a caller
// deciding whether a retry could type a line twice has the same evidence as
// for any other refusal before the first byte.
type ITermScanOff struct{ Op string }

func (e ITermScanOff) Error() string {
	return "iTerm2 integration or scanning is turned off in Settings, so Clawdline did not " + e.Op
}
func (e ITermScanOff) Unwrap() error { return Unsent{Why: e.Error()} }

// osascriptKindsLimit is how many distinct script kinds the counters keep
// apart. The kinds are fixed names in this package (list, capture, send, key,
// reveal-…); a kind past the limit is counted under "other" rather than
// growing the table.
const osascriptKindsLimit = 32

// osascriptOther is the kind a run past osascriptKindsLimit is counted as.
const osascriptOther = "other"

// OsascriptCount is one kind of osascript run, counted.
type OsascriptCount struct {
	Kind     string
	Runs     int64
	Failures int64
	Total    time.Duration
	Max      time.Duration
}

// OsascriptWindow is the counts since a moment, ordered by kind.
type OsascriptWindow struct {
	Since time.Time
	Kinds []OsascriptCount
}

// Runs, Failures, Total and Max are the window's sums across kinds.
func (w OsascriptWindow) Runs() (runs, failures int64, total, longest time.Duration) {
	for _, k := range w.Kinds {
		runs += k.Runs
		failures += k.Failures
		total += k.Total
		if k.Max > longest {
			longest = k.Max
		}
	}
	return
}

type osascriptCounter struct {
	mu     sync.Mutex
	since  time.Time
	total  map[string]*OsascriptCount
	window time.Time
	hour   map[string]*OsascriptCount
}

var osascriptStats = newOsascriptCounter(time.Now())

func newOsascriptCounter(now time.Time) *osascriptCounter {
	return &osascriptCounter{since: now, window: now,
		total: map[string]*OsascriptCount{}, hour: map[string]*OsascriptCount{}}
}

func (c *osascriptCounter) record(kind string, took time.Duration, failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, table := range []map[string]*OsascriptCount{c.total, c.hour} {
		k := kind
		if _, seen := table[k]; !seen && len(table) >= osascriptKindsLimit-1 {
			k = osascriptOther
		}
		row := table[k]
		if row == nil {
			row = &OsascriptCount{Kind: k}
			table[k] = row
		}
		row.Runs++
		if failed {
			row.Failures++
		}
		row.Total += took
		if took > row.Max {
			row.Max = took
		}
	}
}

func snapshotCounts(since time.Time, table map[string]*OsascriptCount) OsascriptWindow {
	out := OsascriptWindow{Since: since, Kinds: make([]OsascriptCount, 0, len(table))}
	for _, row := range table {
		out.Kinds = append(out.Kinds, *row)
	}
	sort.Slice(out.Kinds, func(i, j int) bool { return out.Kinds[i].Kind < out.Kinds[j].Kind })
	return out
}

// read answers the counts since the process started and since the last
// hourly line, without resetting either.
func (c *osascriptCounter) read() (total, window OsascriptWindow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return snapshotCounts(c.since, c.total), snapshotCounts(c.window, c.hour)
}

// take answers the window since the last call and starts the next one.
func (c *osascriptCounter) take(now time.Time) OsascriptWindow {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := snapshotCounts(c.window, c.hour)
	c.window, c.hour = now, map[string]*OsascriptCount{}
	return out
}

// OsascriptReading is every osascript run this process has started, by kind,
// since it started and since the last hourly log line.
func OsascriptReading() (total, window OsascriptWindow) { return osascriptStats.read() }

// TakeOsascriptWindow answers the counts since the previous call and starts a
// new window; the hourly log line is its only caller.
func TakeOsascriptWindow(now time.Time) OsascriptWindow { return osascriptStats.take(now) }

// recordOsascript counts one osascript run that finished. Every iTerm2 script
// in this package reaches it through osascriptRun.done. The pasteboard
// reader's osascript (internal/adapters/artifacts) is not an iTerm2 Apple
// Event and is not counted.
func recordOsascript(kind string, took time.Duration, failed bool) {
	osascriptStats.record(kind, took, failed)
}
