package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// Did a change make one unit of work cheaper (docs/token-ledger.md, the
// section of that name).
//
// A unit of work is a child task: what a brief asked for, with an ending the
// broker recorded. Its sessions are fresh, so its bill (ForTask) is that
// unit's own increment. A long-lived owner session's bill is not — it is the
// cumulative cost of everything that session ever did, far more than
// any one unit — so no owner session, Root
// Assignment or Board item bill is ever a sample here. When per-unit ledger
// cursors exist for Board items, their deltas are WorkUnitSamples too and go
// through the same fold.
//
// The comparison is before/after, not a trial: units are not assigned to
// periods at random. The report therefore compares like with like (strata
// fixed at dispatch), asks for a minimum number of units per period, and
// never names a cause.

// WorkReportDefinitionVersion is the version of what a sample's fields mean
// and how they are read. A frozen baseline carries it; a report refuses to
// compare samples of two versions.
const WorkReportDefinitionVersion = 1

const (
	// workSampleLimit is how many child tasks one work-samples answer reads,
	// newest first. Each is two ledger queries; past it the answer says
	// `truncated` and covers the newest only.
	workSampleLimit = 2000
	// workReportMinUnits is the fewest completed comparable units each period
	// of a group needs before the group gets a verdict other than
	// insufficient_evidence.
	workReportMinUnits = 20
)

// Thresholds of a `met` verdict, as integer ratios so that a value exactly
// at the boundary is decided exactly: the trial's cache-read median is at
// most 7/10 of the baseline's (a drop of at least 30%), and its duration p75
// is at most 11/10 of the baseline's (a rise of at most 10%).
const (
	workCacheReadNum, workCacheReadDen = 7, 10
	workDurationNum, workDurationDen   = 11, 10
)

// The kinds of unit a sample can be.
const (
	WorkUnitTask = "task"
	WorkUnitItem = "item"
)

// How a unit ended. Running has not ended yet.
const (
	WorkEndingSuccess   = "success"
	WorkEndingFailure   = "failure"
	WorkEndingTimeout   = "timeout"
	WorkEndingCancelled = "cancelled"
	WorkEndingStalled   = "stalled"
	WorkEndingLost      = "lost"
	WorkEndingRunning   = "running"
)

// What the ledger can say about a sample's tokens. Only complete, unpriced
// and mixed_models are readable: their token parts are the whole bill.
const (
	// WorkDataComplete is a whole reading of every session, every token priced.
	WorkDataComplete = "complete"
	// WorkDataUnpriced is a whole reading with part of it from a model that
	// has no price: the cost is unknown, never zero.
	WorkDataUnpriced = "unpriced"
	// WorkDataMixedModels is a whole reading whose sessions ran on more
	// than one model.
	WorkDataMixedModels = "mixed_models"
	// WorkDataLedgerBehind is a unit some of whose sessions the ledger has
	// read only in part, or not at all, or whose transcript went missing or
	// unreadable: the token parts are a lower bound, not the bill.
	WorkDataLedgerBehind = "ledger_behind"
	// WorkDataNotYetRead is a unit the ledger has no reading of at all.
	WorkDataNotYetRead = "not_yet_read"
)

// Scope buckets: the number of claims (declared writes) a brief named when it
// was dispatched. A claim count is fixed before the outcome, where a timeout
// is often a default nobody chose.
const (
	WorkScopeNone   = "0"
	WorkScopeSmall  = "1-3"
	WorkScopeMedium = "4-10"
	WorkScopeLarge  = ">10"
)

// WorkKindUnspecified is a task whose brief named no kind.
const WorkKindUnspecified = "unspecified"

// WorkModelMixed is a unit whose sessions ran on more than one model, and
// WorkModelUnknown one whose bill and record name none.
const (
	WorkModelMixed   = "mixed"
	WorkModelUnknown = "unknown"
)

// Verdicts.
const (
	WorkVerdictMet          = "met"
	WorkVerdictNotMet       = "not_met"
	WorkVerdictInsufficient = "insufficient_evidence"
)

// Conditions a not_met verdict names.
const (
	WorkFailedCacheRead     = "cache_read_drop_under_30pct"
	WorkFailedDuration      = "duration_p75_rose_over_10pct"
	WorkFailedFailureRate   = "failure_rate_worsened"
	WorkFailedRedoRate      = "redo_rate_worsened"
	WorkFailedTimeoutStall  = "timeout_stalled_rate_worsened"
	WorkFailedNoBaselineUse = "baseline_cache_read_is_zero"
)

// WorkNotRecorded is each quality guardrail the report was asked for that
// nothing records, shown as not_recorded and never as zero.
var WorkNotRecorded = []CompareMissing{
	{Name: "missed_handoff", Why: "no record says a handoff was owed and not made; a handoff that never happened leaves nothing behind"},
	{Name: "human_rescue", Why: "a person finishing or redirecting a child by hand in its tab is not told apart from the child's own work"},
	{Name: "deploy_rollback", Why: "no record ties a rollback to the unit whose change was rolled back"},
}

// WorkUnitSample is one unit of work: its strata, fixed at dispatch, and what
// it spent and how it ended. Raw: a report is recomputed from these alone.
type WorkUnitSample struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`

	// Strata, decided before the outcome.
	WorkKind string `json:"work_kind"`
	Scope    string `json:"scope"`
	Model    string `json:"model"`
	CrossEnd bool   `json:"cross_end"`

	// Token parts of the whole bill, its subagents included.
	Input      float64 `json:"input"`
	CacheWrite float64 `json:"cache_write"`
	CacheRead  float64 `json:"cache_read"`
	Output     float64 `json:"output"`
	// Calls and CallsAbove are the unit's sessions' own calls, and those made
	// with more than 200k tokens of context; a subagent's calls are not in
	// either (its tokens are in the parts above).
	Calls      int64   `json:"calls"`
	CallsAbove int64   `json:"calls_above"`
	Cost       float64 `json:"cost"`
	CostKnown  bool    `json:"cost_known"`

	CreatedAt time.Time `json:"created_at"`
	// EndedAt is zero, and DurationSeconds 0, while the unit runs.
	EndedAt         time.Time `json:"ended_at"`
	DurationSeconds int64     `json:"duration_seconds"`
	Ending          string    `json:"ending"`
	// Respawn says this unit retried an earlier one; Respawned that a later
	// one in the same read retried this one.
	Respawn   bool   `json:"respawn"`
	Respawned bool   `json:"respawned"`
	Data      string `json:"data"`
}

// Readable says the sample's token parts are its whole bill.
func (s WorkUnitSample) Readable() bool {
	switch s.Data {
	case WorkDataComplete, WorkDataUnpriced, WorkDataMixedModels:
		return true
	}
	return false
}

// Redo says the unit is a respawn or was respawned.
func (s WorkUnitSample) Redo() bool { return s.Respawn || s.Respawned }

// Total is every token of every part.
func (s WorkUnitSample) Total() float64 { return s.Input + s.CacheWrite + s.CacheRead + s.Output }

func (s WorkUnitSample) ended() bool { return s.Ending != WorkEndingRunning && s.Ending != "" }

// comparable is a unit the primary metric is taken over: it succeeded and
// its bill is whole.
func (s WorkUnitSample) comparable() bool { return s.Ending == WorkEndingSuccess && s.Readable() }

// WorkSamples is one read of the ledger: the units created in [Since, Until).
type WorkSamples struct {
	DefinitionVersion int              `json:"definition_version"`
	Since             time.Time        `json:"since"`
	Until             time.Time        `json:"until"`
	GeneratedAt       time.Time        `json:"generated_at"`
	Truncated         bool             `json:"truncated"`
	Samples           []WorkUnitSample `json:"samples"`
}

// ErrWorkUntil is an `until` the work samples cannot read.
var ErrWorkUntil = errors.New("until is a Unix time in seconds, after since")

// WorkSamplesSince is WorkSamplesBetween over a `since` as ParseCompareSince
// reads it and an `until` that is a Unix time in seconds, now when empty.
func (u *UsageLedger) WorkSamplesSince(ctx context.Context, rawSince, rawUntil string) (WorkSamples, error) {
	now := u.now()
	since, err := ParseCompareSince(rawSince, now)
	if err != nil {
		return WorkSamples{}, err
	}
	until := now
	if rawUntil = strings.TrimSpace(rawUntil); rawUntil != "" {
		n, err := strconv.ParseInt(rawUntil, 10, 64)
		if err != nil || len(rawUntil) > 12 || strings.TrimLeft(rawUntil, "0123456789") != "" {
			return WorkSamples{}, ErrWorkUntil
		}
		until = time.Unix(n, 0)
	}
	if !until.After(since) {
		return WorkSamples{}, ErrWorkUntil
	}
	return u.WorkSamplesBetween(ctx, since, until)
}

// WorkSamplesBetween reads every child task created in [since, until),
// newest first up to workSampleLimit, into samples.
func (u *UsageLedger) WorkSamplesBetween(ctx context.Context, since, until time.Time) (WorkSamples, error) {
	out := WorkSamples{DefinitionVersion: WorkReportDefinitionVersion, Since: since, Until: until,
		GeneratedAt: u.now(), Samples: []WorkUnitSample{}}
	heads, err := u.Store.BrokerTaskHeads(ctx)
	if err != nil {
		return out, err
	}
	var ids []string
	for _, h := range heads {
		if h.CreatedAt.Before(since) || !h.CreatedAt.Before(until) {
			continue
		}
		if len(ids) == workSampleLimit {
			out.Truncated = true
			break
		}
		ids = append(ids, h.ID)
	}
	rows, err := u.Store.BrokerTaskRecords(ctx, ids)
	if err != nil {
		return out, err
	}
	var records []orchestrator.Record
	for _, id := range ids {
		row, ok := rows[id]
		if !ok {
			continue
		}
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			continue
		}
		if r.ID == "" {
			r.ID = id
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = row.CreatedAt
		}
		records = append(records, r)
	}
	bills := map[string]workBill{}
	for _, r := range records {
		b, err := u.workBill(ctx, r.ID)
		if err != nil {
			return out, err
		}
		bills[r.ID] = b
	}
	out.Samples = WorkSamplesOf(records, bills)
	return out, nil
}

// workBill is a task's bill and the models its sessions ran on.
type workBill struct {
	Usage  TaskUsage
	Models []string
}

func (u *UsageLedger) workBill(ctx context.Context, taskID string) (workBill, error) {
	rows, err := u.Store.UsageRowsForTask(ctx, taskID)
	if err != nil {
		return workBill{}, err
	}
	rows = ownRows(rows)
	subs, err := u.Store.UsageRowsWithParents(ctx, conversationsOf(rows))
	if err != nil {
		return workBill{}, err
	}
	return workBill{Usage: FoldTask(taskID, rows, subs), Models: rowModels(rows)}, nil
}

// rowModels is every model the rows' readings name, sorted.
func rowModels(rows []store.UsageRow) []string {
	seen := map[string]bool{}
	for _, r := range rows {
		state, _, _ := usageLedgerOf(r)
		if state.Model != "" {
			seen[state.Model] = true
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// WorkSamplesOf is a sample per record, in the order given.
func WorkSamplesOf(records []orchestrator.Record, bills map[string]workBill) []WorkUnitSample {
	retried := map[string]bool{}
	for _, r := range records {
		if r.RespawnOf != "" {
			retried[r.RespawnOf] = true
		}
	}
	out := make([]WorkUnitSample, 0, len(records))
	for _, r := range records {
		s := WorkUnitSample{Kind: WorkUnitTask, ID: r.ID, WorkKind: r.Kind, Scope: workScope(len(r.Claims)),
			CrossEnd: workCrossEnd(r.Claims), CreatedAt: r.CreatedAt, Ending: workEnding(r),
			Respawn: r.RespawnOf != "", Respawned: retried[r.ID]}
		if s.WorkKind == "" {
			s.WorkKind = WorkKindUnspecified
		}
		if s.Ending != WorkEndingRunning && !r.FinishedAt.IsZero() {
			s.EndedAt = r.FinishedAt
			if d := r.FinishedAt.Sub(r.CreatedAt); d > 0 {
				s.DurationSeconds = int64(d / time.Second)
			}
		}
		b := bills[r.ID]
		m := b.Usage.Totals.Measured
		s.Input, s.CacheWrite, s.CacheRead, s.Output = m.Input, m.CacheWrite1h+m.CacheWrite5m, m.CacheRead, m.Output
		s.Cost, s.CostKnown = m.Cost, m.CostKnown()
		counted, whole := 0, true
		for _, sess := range b.Usage.Sessions {
			if sess.counted() {
				counted++
				s.Calls += sess.Calls
				s.CallsAbove += sess.CallsAbove
			}
			if !sess.counted() || sess.Reason != "" || sess.More {
				whole = false
			}
			for _, sub := range sess.Subagents {
				if sub.Reason != "" {
					whole = false
				}
			}
		}
		switch {
		case len(b.Models) > 1:
			s.Model = WorkModelMixed
		case len(b.Models) == 1:
			s.Model = b.Models[0]
		case r.Model != "":
			s.Model = r.Model
		default:
			s.Model = WorkModelUnknown
		}
		switch {
		case counted == 0:
			s.Data = WorkDataNotYetRead
		case !whole:
			s.Data = WorkDataLedgerBehind
		case len(b.Models) > 1:
			s.Data = WorkDataMixedModels
		case !s.CostKnown:
			s.Data = WorkDataUnpriced
		default:
			s.Data = WorkDataComplete
		}
		if s.Data == WorkDataNotYetRead {
			// Nothing was read: the parts are unknown, not zero, and the
			// sample is counted as unreadable wherever it is counted.
			s.CostKnown = false
		}
		out = append(out, s)
	}
	return out
}

// workEnding is how a task ended, as compareEnding counts it.
func workEnding(r orchestrator.Record) string {
	if !r.State.Terminal() {
		return WorkEndingRunning
	}
	switch r.State {
	case orchestrator.StateSuccess:
		return WorkEndingSuccess
	case orchestrator.StateFailure:
		return WorkEndingFailure
	case orchestrator.StateTimeout:
		return WorkEndingTimeout
	case orchestrator.StateCancelled:
		return WorkEndingCancelled
	case orchestrator.StateSpawnFailed:
		if r.Stalled() {
			return WorkEndingStalled
		}
	}
	return WorkEndingLost
}

func workScope(claims int) string {
	switch {
	case claims == 0:
		return WorkScopeNone
	case claims <= 3:
		return WorkScopeSmall
	case claims <= 10:
		return WorkScopeMedium
	}
	return WorkScopeLarge
}

// workCrossEnd says the claims touch both the console (`web/`) and the Go
// code (`internal/` or `cmd/`).
func workCrossEnd(claims []string) bool {
	web, goCode := false, false
	for _, c := range claims {
		c = strings.TrimPrefix(strings.TrimSpace(c), "./")
		switch {
		case c == "web" || strings.HasPrefix(c, "web/"):
			web = true
		case c == "internal" || c == "cmd" || strings.HasPrefix(c, "internal/") || strings.HasPrefix(c, "cmd/"):
			goCode = true
		}
	}
	return web && goCode
}

// ---------- the fold ----------

// WorkInterval is a confidence interval of a median.
type WorkInterval struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	// Coverage is the interval's exact coverage, at least 0.95.
	Coverage float64 `json:"coverage"`
}

// WorkPeriod is one group's units in one period.
type WorkPeriod struct {
	// Units is every unit; Unreadable those whose tokens are not the whole
	// bill (counted here, never as zero tokens). Ended is every unit in a
	// final state and what each rate is a share of; Running the rest.
	Units      int `json:"units"`
	Unreadable int `json:"unreadable"`
	Ended      int `json:"ended"`
	Running    int `json:"running"`
	Successful int `json:"successful"`
	// Comparable is the successful units with a whole bill: every median,
	// interval and share below is over them.
	Comparable int `json:"comparable"`

	CacheReadMedian   *float64      `json:"cache_read_median"`
	CacheReadInterval *WorkInterval `json:"cache_read_interval"`
	TotalMedian       *float64      `json:"total_median"`
	// AboveShare is the share of the comparable units' own calls made with
	// more than 200k tokens of context.
	AboveShare     *float64 `json:"above_200k_share"`
	DurationMedian *int64   `json:"duration_median_seconds"`
	DurationP75    *int64   `json:"duration_p75_seconds"`
	CostMedian     *float64 `json:"cost_median"`
	// CostKnown is false when some comparable unit's cost has a part with
	// no price: CostMedian is then not the whole cost.
	CostKnown bool `json:"cost_known"`

	// Guardrails: counts, and their shares of Ended.
	Failure            int      `json:"failure"`
	Redo               int      `json:"redo"`
	TimeoutStalled     int      `json:"timeout_stalled"`
	FailureRate        *float64 `json:"failure_rate"`
	RedoRate           *float64 `json:"redo_rate"`
	TimeoutStalledRate *float64 `json:"timeout_stalled_rate"`
}

// WorkGroup is one stratum: its two periods and its verdict.
type WorkGroup struct {
	WorkKind string     `json:"work_kind"`
	Scope    string     `json:"scope"`
	Model    string     `json:"model"`
	CrossEnd bool       `json:"cross_end"`
	Baseline WorkPeriod `json:"baseline"`
	Trial    WorkPeriod `json:"trial"`
	// CacheReadChange and DurationP75Change are trial/baseline - 1, nil
	// when either side has none.
	CacheReadChange   *float64 `json:"cache_read_change"`
	DurationP75Change *float64 `json:"duration_p75_change"`
	Verdict           string   `json:"verdict"`
	// Failed names each condition a not_met verdict did not meet.
	Failed []string `json:"failed"`
}

// WorkReport is the whole answer.
type WorkReport struct {
	DefinitionVersion int         `json:"definition_version"`
	MinUnits          int         `json:"min_units"`
	Groups            []WorkGroup `json:"groups"`
	Verdict           string      `json:"verdict"`
	// Why is the overall verdict in one sentence.
	Why         string           `json:"why"`
	NotRecorded []CompareMissing `json:"not_recorded"`
}

type workStratum struct {
	kind, scope, model string
	crossEnd           bool
}

// FoldWorkReport groups both periods' samples by stratum and gives each group
// and the whole a verdict. It reads nothing but its arguments.
func FoldWorkReport(baseline, trial []WorkUnitSample) WorkReport {
	out := WorkReport{DefinitionVersion: WorkReportDefinitionVersion, MinUnits: workReportMinUnits,
		Groups: []WorkGroup{}, NotRecorded: WorkNotRecorded}
	split := func(samples []WorkUnitSample) map[workStratum][]WorkUnitSample {
		m := map[workStratum][]WorkUnitSample{}
		for _, s := range samples {
			if s.Kind != "" && s.Kind != WorkUnitTask && s.Kind != WorkUnitItem {
				continue
			}
			k := workStratum{s.WorkKind, s.Scope, s.Model, s.CrossEnd}
			m[k] = append(m[k], s)
		}
		return m
	}
	before, after := split(baseline), split(trial)
	var keys []workStratum
	for k := range before {
		keys = append(keys, k)
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.scope != b.scope {
			return a.scope < b.scope
		}
		if a.model != b.model {
			return a.model < b.model
		}
		return !a.crossEnd && b.crossEnd
	})
	met, judged := 0, 0
	var notMet []string
	for _, k := range keys {
		g := WorkGroup{WorkKind: k.kind, Scope: k.scope, Model: k.model, CrossEnd: k.crossEnd,
			Baseline: foldWorkPeriod(before[k]), Trial: foldWorkPeriod(after[k]), Failed: []string{}}
		judgeWorkGroup(&g)
		switch g.Verdict {
		case WorkVerdictMet:
			met++
			judged++
		case WorkVerdictNotMet:
			judged++
			notMet = append(notMet, WorkGroupName(g))
		}
		out.Groups = append(out.Groups, g)
	}
	switch {
	case judged == 0:
		out.Verdict = WorkVerdictInsufficient
		out.Why = fmt.Sprintf("no group has %d completed comparable units in both periods", workReportMinUnits)
	case met == judged:
		out.Verdict = WorkVerdictMet
		out.Why = fmt.Sprintf("every group with enough evidence met the thresholds (%d of %d groups had enough)", judged, len(out.Groups))
	default:
		out.Verdict = WorkVerdictNotMet
		out.Why = fmt.Sprintf("%d of %d groups with enough evidence did not meet the thresholds: %s",
			len(notMet), judged, strings.Join(notMet, "; "))
	}
	return out
}

// WorkGroupName is a group as a person reads it.
func WorkGroupName(g WorkGroup) string {
	cross := "one-end"
	if g.CrossEnd {
		cross = "cross-end"
	}
	return fmt.Sprintf("%s/%s claims/%s/%s", g.WorkKind, g.Scope, g.Model, cross)
}

func foldWorkPeriod(samples []WorkUnitSample) WorkPeriod {
	p := WorkPeriod{CostKnown: true}
	var reads, totals, costs []float64
	var durations []int64
	var calls, above int64
	for _, s := range samples {
		p.Units++
		if !s.Readable() {
			p.Unreadable++
		}
		if !s.ended() {
			p.Running++
			continue
		}
		p.Ended++
		switch s.Ending {
		case WorkEndingSuccess:
			p.Successful++
		case WorkEndingFailure:
			p.Failure++
		case WorkEndingTimeout, WorkEndingStalled:
			p.TimeoutStalled++
		}
		if s.Redo() {
			p.Redo++
		}
		if !s.comparable() {
			continue
		}
		p.Comparable++
		reads = append(reads, s.CacheRead)
		totals = append(totals, s.Total())
		costs = append(costs, s.Cost)
		p.CostKnown = p.CostKnown && s.CostKnown
		durations = append(durations, s.DurationSeconds)
		calls += s.Calls
		above += s.CallsAbove
	}
	if p.Comparable > 0 {
		m := medianFloat(reads)
		p.CacheReadMedian = &m
		p.CacheReadInterval = medianInterval(reads)
		t := medianFloat(totals)
		p.TotalMedian = &t
		c := medianFloat(costs)
		p.CostMedian = &c
		d := medianInt(durations)
		p.DurationMedian = &d
		q := percentileNearestRank(durations, 75)
		p.DurationP75 = &q
		if calls > 0 {
			p.AboveShare = share(float64(above), float64(calls))
		}
	} else {
		p.CostKnown = false
	}
	if p.Ended > 0 {
		e := float64(p.Ended)
		p.FailureRate = share(float64(p.Failure), e)
		p.RedoRate = share(float64(p.Redo), e)
		p.TimeoutStalledRate = share(float64(p.TimeoutStalled), e)
	}
	return p
}

// judgeWorkGroup decides one group's verdict.
func judgeWorkGroup(g *WorkGroup) {
	b, t := g.Baseline, g.Trial
	if b.CacheReadMedian != nil && t.CacheReadMedian != nil && *b.CacheReadMedian > 0 {
		v := *t.CacheReadMedian / *b.CacheReadMedian - 1
		g.CacheReadChange = &v
	}
	if b.DurationP75 != nil && t.DurationP75 != nil && *b.DurationP75 > 0 {
		v := float64(*t.DurationP75)/float64(*b.DurationP75) - 1
		g.DurationP75Change = &v
	}
	if b.Comparable < workReportMinUnits || t.Comparable < workReportMinUnits {
		g.Verdict = WorkVerdictInsufficient
		return
	}
	switch {
	case *b.CacheReadMedian <= 0:
		g.Failed = append(g.Failed, WorkFailedNoBaselineUse)
	case *t.CacheReadMedian*workCacheReadDen > *b.CacheReadMedian*workCacheReadNum:
		g.Failed = append(g.Failed, WorkFailedCacheRead)
	}
	if *t.DurationP75*workDurationDen > *b.DurationP75*workDurationNum {
		g.Failed = append(g.Failed, WorkFailedDuration)
	}
	worse := func(before, after *float64) bool { return before != nil && after != nil && *after > *before }
	if worse(b.FailureRate, t.FailureRate) {
		g.Failed = append(g.Failed, WorkFailedFailureRate)
	}
	if worse(b.RedoRate, t.RedoRate) {
		g.Failed = append(g.Failed, WorkFailedRedoRate)
	}
	if worse(b.TimeoutStalledRate, t.TimeoutStalledRate) {
		g.Failed = append(g.Failed, WorkFailedTimeoutStall)
	}
	g.Verdict = WorkVerdictMet
	if len(g.Failed) > 0 {
		g.Verdict = WorkVerdictNotMet
	}
}

// percentileNearestRank is the p-th percentile by nearest rank: the
// ceil(p/100 * n)-th smallest value. It is always one of the values.
func percentileNearestRank(v []int64, p int) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := (p*len(s) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return s[rank-1]
}

// medianInterval is the exact distribution-free interval of a median from
// order statistics: with the values sorted x(1) ≤ … ≤ x(n), it is
// [x(k), x(n-k+1)] for the largest k whose coverage, 1 - 2·P(B ≤ k-1) with
// B ~ Binomial(n, 1/2), is at least 95%. No resampling, so no seed: the same
// values always give the same interval. Under six values no such k exists
// and the answer is nil.
func medianInterval(v []float64) *WorkInterval {
	n := len(v)
	if n < 6 {
		return nil
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	// cdf[j] = P(B ≤ j).
	logHalf := float64(n) * math.Log(0.5)
	lgN, _ := math.Lgamma(float64(n + 1))
	tail := 0.0
	k := 0
	coverage := 0.0
	for j := 0; j <= n/2; j++ {
		lgJ, _ := math.Lgamma(float64(j + 1))
		lgNJ, _ := math.Lgamma(float64(n - j + 1))
		tail += math.Exp(lgN - lgJ - lgNJ + logHalf)
		// tail is P(B ≤ j): the interval [x(j+1), x(n-j)] covers with
		// probability 1 - 2·tail.
		c := 1 - 2*tail
		if c < 0.95 {
			break
		}
		k, coverage = j+1, c
	}
	if k == 0 {
		return nil
	}
	return &WorkInterval{Low: s[k-1], High: s[n-k], Coverage: coverage}
}
