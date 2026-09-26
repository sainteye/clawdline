package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// Whether compacting early is worth it (docs/token-ledger.md "Did compacting
// early pay").
//
// The person set `claude_auto_compact_window` as an experiment: every Claude
// child the broker launches after that carries the window, and its record
// says which it got (Record.AutoCompactWindow, compact.go). This is the
// side-by-side that answers the experiment's two questions at once — did the
// tasks with a window cost less, and did they do worse — by grouping child
// tasks by the window they were launched with and reading, for each group,
// the ledger's bill beside the broker's own account of how the task ended.
//
// The unit is a child task, not a session: a task is what a brief asked for
// and what ended well or badly, and its sessions are folded into it as its
// bill is (ForTask). Root Assignments and handoff receivers are Clawdline
// launches too, but they are long-lived and have no ending to compare, so
// they are not part of this answer.

const (
	// compareTooFewLimit is the fewest tasks a group needs before its shares
	// and rates are shown. Below it a group says `too_few` and every
	// percentage is null: one failure in three tasks is not a 33% failure
	// rate anybody should act on.
	compareTooFewLimit = 5
	// compareTaskLimit is how many tasks one answer reads, newest first.
	// Each is two ledger queries; past it the answer says `truncated` and
	// covers the newest ones only.
	compareTaskLimit = 500
	// compareExcludedLimit is how many excluded tasks the answer names; the
	// count is always whole.
	compareExcludedLimit = 50
	// CompareDefaultSince is the range asked when none is named.
	CompareDefaultSince = 14 * 24 * time.Hour
	// compareSinceDaysLimit is the longest range a `since` may name as a
	// duration, in days; a Unix time may name any.
	compareSinceDaysLimit = 3650
)

// Why a child task in the range is left out of every group.
const (
	// CompareExcludedCodex is a Codex task: it is never given a window, so
	// it answers a different question.
	CompareExcludedCodex = "codex"
	// CompareExcludedNotLaunched is a Claude task whose tab never opened —
	// queued, or spawn_failed before a launch — so no window was decided.
	CompareExcludedNotLaunched = "not_launched"
	// CompareExcludedUnrecorded is a Claude task that was launched and whose
	// record does not say with what window. Since the setting's own daemon
	// there is no such task: every launch records its window. It is kept for
	// a record that has the field and no value.
	CompareExcludedUnrecorded = "window_unrecorded"
)

// CompareBeforeSetting is the group of Claude tasks launched by a daemon
// that could not set a window at all — their records predate the field, so
// Claude Code compacted them near its own window, which is exactly "none".
// They are kept apart from `none` rather than folded into it because they are
// another period's work: the comparison shows both and lets the reader judge.
// Measured on 2026-09-26, two hours after the setting was turned on: `none`
// held one task and 164 launched Claude tasks were excluded as unrecorded,
// so a readout a week later would have compared the window against nothing.
const CompareBeforeSetting int64 = -1

// CompareNotRecorded is each quality signal the brief asked for that nothing
// records, so the answer names it rather than showing a count of zero.
var CompareNotRecorded = []CompareMissing{{
	Name: "finish_refusals",
	Why: "`clawdline task finish` refuses an invalid result.json in the child's own process and writes " +
		"nothing; no record, result or event keeps the refusal.",
}}

// CompareMissing is a signal the comparison cannot show, and why.
type CompareMissing struct {
	Name string
	Why  string
}

// CompactionGroup is the tasks launched with one window: 0 is none.
type CompactionGroup struct {
	Window int64
	// Tasks is every task in the group; Read those with a ledger reading,
	// over which every cost, call and context figure is taken.
	Tasks, Read, Sessions int
	// TooFew says Tasks is under compareTooFewLimit: every share and rate
	// below is nil.
	TooFew bool

	CostTotal, CostMedian float64
	// CostKnown is false when some task's cost has a part with no price.
	CostKnown          bool
	CallsPerTask       float64
	CompactionsPerTask float64
	PeakMedian         int64
	PeakMax            int64
	// AboveShare is the share of the sessions' own cost spent on calls made
	// with more than 200k tokens of context; nil when TooFew or nothing was
	// spent.
	AboveShare *float64

	// How the tasks ended, from the broker's records. Ended is every task in
	// a final state and is what each rate is a share of; Running the rest.
	Ended, Running                                      int
	Success, Failure, Timeout, Cancelled, Stalled, Lost int
	SuccessRate, FailureRate, TimeoutRate, StalledRate  *float64
	// Respawns is how many tasks in the group are a respawn of an earlier
	// spawn_failed one.
	Respawns int
}

// CompareExcluded is one task left out, and why.
type CompareExcluded struct {
	TaskID string
	Reason string
}

// CompactionComparison is the whole answer.
type CompactionComparison struct {
	Since, Until time.Time
	MinTasks     int
	Groups       []CompactionGroup
	// Excluded counts every task in the range left out; ExcludedTasks names
	// the newest compareExcludedLimit of them.
	Excluded          int
	ExcludedTasks     []CompareExcluded
	ExcludedTruncated bool
	// Truncated says the range held more than compareTaskLimit tasks and only
	// the newest were read.
	Truncated   bool
	NotRecorded []CompareMissing
}

// ErrCompareSince is a `since` this comparison cannot read.
var ErrCompareSince = errors.New("since is a number of days or hours (`14d`, `36h`) or a Unix time in seconds")

// ParseCompareSince reads `since`: empty is CompareDefaultSince ago, `<n>d`
// or `<n>h` is that long ago, and a bare number is a Unix time in seconds.
func ParseCompareSince(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.Add(-CompareDefaultSince), nil
	}
	unit := time.Duration(0)
	switch {
	case strings.HasSuffix(raw, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(raw, "h"):
		unit = time.Hour
	}
	digits := raw
	if unit != 0 {
		digits = raw[:len(raw)-1]
	}
	if digits == "" || strings.TrimLeft(digits, "0123456789") != "" || len(digits) > 12 {
		return time.Time{}, ErrCompareSince
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return time.Time{}, ErrCompareSince
	}
	if unit == 0 {
		return time.Unix(n, 0), nil
	}
	if n == 0 || n*int64(unit/time.Hour) > compareSinceDaysLimit*24 {
		return time.Time{}, ErrCompareSince
	}
	return now.Add(-time.Duration(n) * unit), nil
}

// CompareCompactionSince is CompareCompaction over a `since` as a route or
// a command spells it (ParseCompareSince), read against this ledger's clock.
func (u *UsageLedger) CompareCompactionSince(ctx context.Context, raw string) (CompactionComparison, error) {
	since, err := ParseCompareSince(raw, u.now())
	if err != nil {
		return CompactionComparison{}, err
	}
	return u.CompareCompaction(ctx, since)
}

// CompareCompaction reads every child task created since since, newest first
// up to compareTaskLimit, with its bill, and folds them into groups.
func (u *UsageLedger) CompareCompaction(ctx context.Context, since time.Time) (CompactionComparison, error) {
	until := u.now()
	heads, err := u.Store.BrokerTaskHeads(ctx)
	if err != nil {
		return CompactionComparison{}, err
	}
	var ids []string
	truncated := false
	for _, h := range heads {
		if h.CreatedAt.Before(since) {
			continue
		}
		if len(ids) == compareTaskLimit {
			truncated = true
			break
		}
		ids = append(ids, h.ID)
	}
	rows, err := u.Store.BrokerTaskRecords(ctx, ids)
	if err != nil {
		return CompactionComparison{}, err
	}
	var records []orchestrator.Record
	bills := map[string]TaskUsage{}
	for _, id := range ids {
		row, ok := rows[id]
		if !ok {
			continue
		}
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			// A row whose record cannot be read says nothing about its
			// window; the task list names it as unreadable.
			continue
		}
		if r.ID == "" {
			r.ID = id
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = row.CreatedAt
		}
		records = append(records, r)
		if compareExclusion(r) != "" {
			continue
		}
		bill, err := u.ForTask(ctx, r.ID)
		if err != nil {
			return CompactionComparison{}, err
		}
		bills[r.ID] = bill
	}
	out := FoldCompactionComparison(records, bills)
	out.Since, out.Until, out.Truncated = since, until, truncated
	return out, nil
}

// compareExclusion is why a task is left out, or empty when it has a window.
func compareExclusion(r orchestrator.Record) string {
	_, why := compareWindow(r)
	return why
}

// compareWindow is the group a task falls in, or why it falls in none.
func compareWindow(r orchestrator.Record) (int64, string) {
	switch {
	case r.AutoCompactWindow != nil:
		return *r.AutoCompactWindow, ""
	case r.Assistant != "" && r.Assistant != "claude":
		return 0, CompareExcludedCodex
	case r.SpawnedAt.IsZero() && r.ChildTerminalID == "":
		return 0, CompareExcludedNotLaunched
	case r.AutoCompactRequested == nil:
		// Launched by a daemon with no window to give: before the setting.
		return CompareBeforeSetting, ""
	}
	return 0, CompareExcludedUnrecorded
}

// FoldCompactionComparison is the comparison of these tasks, newest first,
// with the bills of those that have a window.
func FoldCompactionComparison(records []orchestrator.Record, bills map[string]TaskUsage) CompactionComparison {
	out := CompactionComparison{MinTasks: compareTooFewLimit, NotRecorded: CompareNotRecorded,
		Groups: []CompactionGroup{}, ExcludedTasks: []CompareExcluded{}}
	type acc struct {
		g            CompactionGroup
		costs        []float64
		peaks        []int64
		calls, compa int64
		above, own   float64
	}
	groups := map[int64]*acc{}
	for _, r := range records {
		if why := compareExclusion(r); why != "" {
			out.Excluded++
			if len(out.ExcludedTasks) < compareExcludedLimit {
				out.ExcludedTasks = append(out.ExcludedTasks, CompareExcluded{TaskID: r.ID, Reason: why})
			} else {
				out.ExcludedTruncated = true
			}
			continue
		}
		w, _ := compareWindow(r)
		a := groups[w]
		if a == nil {
			a = &acc{g: CompactionGroup{Window: w, CostKnown: true}}
			groups[w] = a
		}
		a.g.Tasks++
		compareEnding(&a.g, r)
		bill := bills[r.ID]
		a.g.Sessions += len(bill.Sessions)
		read := false
		var peak int64
		for _, s := range bill.Sessions {
			if !s.counted() {
				continue
			}
			read = true
			a.calls += s.Calls
			a.compa += s.Compactions
			peak = max(peak, s.PeakContext)
			own := s.Totals.Measured.Cost
			for _, sub := range s.Subagents {
				own -= sub.Measured.Cost
			}
			a.own += own
			a.above += s.Above.Cost
		}
		if !read {
			continue
		}
		a.g.Read++
		a.costs = append(a.costs, bill.Totals.Measured.Cost)
		a.peaks = append(a.peaks, peak)
		a.g.CostTotal += bill.Totals.Measured.Cost
		a.g.CostKnown = a.g.CostKnown && bill.Totals.CostKnown()
	}

	windows := make([]int64, 0, len(groups))
	for w := range groups {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i] < windows[j] })
	for _, w := range windows {
		a := groups[w]
		g := a.g
		g.TooFew = g.Tasks < compareTooFewLimit
		if g.Read > 0 {
			g.CostMedian = medianFloat(a.costs)
			g.CallsPerTask = float64(a.calls) / float64(g.Read)
			g.CompactionsPerTask = float64(a.compa) / float64(g.Read)
			g.PeakMedian = medianInt(a.peaks)
			for _, p := range a.peaks {
				g.PeakMax = max(g.PeakMax, p)
			}
		}
		if !g.TooFew {
			if a.own > 0 {
				g.AboveShare = share(a.above, a.own)
			}
			if g.Ended > 0 {
				ended := float64(g.Ended)
				g.SuccessRate = share(float64(g.Success), ended)
				g.FailureRate = share(float64(g.Failure), ended)
				g.TimeoutRate = share(float64(g.Timeout), ended)
				g.StalledRate = share(float64(g.Stalled), ended)
			}
		}
		out.Groups = append(out.Groups, g)
	}
	return out
}

// compareEnding counts how one task ended. A stalled child is spawn_failed
// with the broker's stall report on it (stall.go); any other spawn_failed —
// a tab that never opened, a briefing that could not be typed — is Lost.
func compareEnding(g *CompactionGroup, r orchestrator.Record) {
	if r.RespawnOf != "" {
		g.Respawns++
	}
	if !r.State.Terminal() {
		g.Running++
		return
	}
	g.Ended++
	switch r.State {
	case orchestrator.StateSuccess:
		g.Success++
	case orchestrator.StateFailure:
		g.Failure++
	case orchestrator.StateTimeout:
		g.Timeout++
	case orchestrator.StateCancelled:
		g.Cancelled++
	case orchestrator.StateSpawnFailed:
		if r.Stalled() {
			g.Stalled++
		} else {
			g.Lost++
		}
	}
}

func share(part, whole float64) *float64 {
	v := part / whole
	return &v
}

func medianFloat(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func medianInt(v []int64) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// CompareGroupName is a group as a person reads it: `none`, or its window.
func CompareGroupName(window int64) string {
	if window == CompareBeforeSetting {
		return "before-setting"
	}
	if window == 0 {
		return "none"
	}
	return fmt.Sprintf("%d", window)
}
