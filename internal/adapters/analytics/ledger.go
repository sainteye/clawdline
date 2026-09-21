package analytics

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
)

// The Usage page's rows are this daemon's own reading of the assistants'
// records (rows.go) — the ledger this daemon keeps, since it outlives the
// Swift app (cutover B2). The Swift app's usage ledger, when there is one, is
// history beside it: its rows fill in the conversations whose records are no
// longer on disk (the assistants clear old transcripts), and nothing else. One
// conversation is answered by one source, never both, so a month is never
// counted twice; the answer says which sources it used (`source`), and an
// unreadable ledger makes it `partial`, named, rather than an idle month.
//
// With the legacy switch off (swiftstore/legacy.go) the ledger is not opened
// at all and `source.legacyLedger` says `disabled`.

// Where the rows came from, for the log line written when it changes.
const (
	sourceLedger      = "swift_ledger"
	sourceTranscripts = "transcripts"
)

// ledgerCache is one ledger reading turned into rows, kept until the reading
// changes.
type ledgerCache struct {
	readAt time.Time
	rows   []Row
	latest time.Time
}

// ledgerRows answers the Swift ledger's rows and how the ledger was read. A
// ledger that is absent, switched off or unreadable answers no rows and says
// which; only a ledger caught mid-write is an error (ErrBusy), because the
// next try is likely to land and an answer without it would flicker.
func (c *Collector) ledgerRows(ctx context.Context, since time.Time) (rows []Row, status swiftstore.Source, err error) {
	if c.ledger == nil {
		return nil, swiftstore.SourceAbsent, nil
	}
	reading := c.ledger.Read(ctx)
	switch {
	case reading.Disabled:
		c.noteSource(sourceTranscripts, nil)
		return nil, swiftstore.SourceDisabled, nil
	case reading.Missing:
		c.noteSource(sourceTranscripts, nil)
		return nil, swiftstore.SourceAbsent, nil
	case !reading.Known:
		c.noteSource(sourceLedger, reading.Err)
		if errors.Is(reading.Err, swiftstore.ErrLedgerChanging) ||
			errors.Is(reading.Err, context.DeadlineExceeded) || errors.Is(reading.Err, context.Canceled) {
			// The Swift app is writing; the next try is likely to land.
			return nil, swiftstore.SourceUnreadable, ErrBusy
		}
		return nil, swiftstore.SourceUnreadable, nil
	}
	c.noteSource(sourceLedger, reading.Err)
	status = swiftstore.SourceCurrent
	if reading.Stale {
		status = swiftstore.SourceStale
	}

	c.mu.Lock()
	cache := c.ledgerRead
	if cache == nil || !cache.readAt.Equal(reading.ReadAt) {
		cache = &ledgerCache{readAt: reading.ReadAt, rows: ledgerToRows(reading), latest: reading.Latest}
		c.ledgerRead = cache
	}
	c.mu.Unlock()

	labels := c.scheduleLabels()
	out := make([]Row, 0, len(cache.rows))
	newest := -1
	for i, r := range cache.rows {
		if !r.UpdatedAt.Before(cache.latest) && newest < 0 {
			newest = i
		}
		if !since.IsZero() && r.StartedAt.Before(since) && i != newest {
			continue
		}
		if r.ScheduleID != "" {
			r.RootLabel = labels[r.ScheduleID]
		}
		out = append(out, r)
	}
	// `freshness` is the whole ledger's newest write, which may belong to a
	// row that started before the range. That row is carried so Run sees it;
	// its start keeps it out of every range the scan was asked for.
	return out, status, nil
}

// withHistory is this daemon's own rows, and the ledger's rows for the
// conversations those do not hold. A ledger row with no conversation cannot
// be said to be covered, and is kept.
func withHistory(own, legacy []Row) []Row {
	covered := make(map[string]bool, len(own))
	for _, r := range own {
		if r.SessionID != "" {
			covered[r.Assistant+"\x00"+r.SessionID] = true
		}
	}
	out := append(make([]Row, 0, len(own)+len(legacy)), own...)
	for _, r := range legacy {
		if r.SessionID != "" && covered[r.Assistant+"\x00"+r.SessionID] {
			continue
		}
		r.Legacy = true
		out = append(out, r)
	}
	sortNewestFirst(out)
	return out
}

// ledgerToRows is `UsageLedger.row(from:)` in this package's vocabulary.
func ledgerToRows(reading swiftstore.LedgerReading) []Row {
	out := make([]Row, 0, len(reading.Intervals))
	for _, iv := range reading.Intervals {
		r := Row{
			IntervalKey:     iv.IntervalKey,
			TaskID:          deref(iv.TaskID),
			ScheduleID:      deref(iv.ScheduleID),
			StartedAt:       iv.StartedAt,
			UpdatedAt:       iv.UpdatedAt,
			Assistant:       iv.Assistant,
			Model:           deref(iv.Model),
			Origin:          iv.Origin,
			ProjectKey:      deref(iv.ProjectKey),
			WorkingDir:      deref(iv.WorkingDir),
			SessionID:       iv.SessionID,
			BoundaryKind:    iv.BoundaryKind,
			BoundaryID:      iv.BoundaryID,
			Tokens:          [4]*int64{iv.InputNew, iv.Output, iv.CacheRead, iv.CacheWrite},
			SourceTotal:     iv.SourceTotal,
			CostValue:       iv.CostValue,
			CostUnit:        deref(iv.CostUnit),
			CostBasis:       iv.CostBasis,
			PriceSnapshotID: deref(iv.PriceSnapshotID),
			MissingCost:     deref(iv.MissingReason),
			Coverage:        iv.Coverage,
			CoverageReasons: iv.CoverageReasons,
			InputBasis:      deref(iv.InputBasis),
			ParentTaskID:    deref(iv.ParentTaskID),
			LandingState:    deref(iv.LandingState),
			LandingVerified: iv.LandingVerified,
			Reconciliation:  deref(iv.Reconciliation),
			GraphID:         deref(iv.GraphID),
			RetryOf:         deref(iv.RetryOf),
			Attempt:         iv.Attempt,
			Disposition:     deref(iv.Disposition),
			Corrections:     reading.Corrections[iv.IntervalKey],
		}
		if iv.Depth != nil {
			r.Depth = *iv.Depth
		}
		if iv.EndedAt != nil {
			r.EndedAt = *iv.EndedAt
		}
		out = append(out, r)
	}
	// The ledger was read in this order already; sorting again keeps the
	// contract in one place for both sources.
	sortNewestFirst(out)
	return out
}

// scheduleLabels is `Orchestrator.usageScheduleLabels`: the newest retained
// run's root label for each schedule, overridden by the live schedule's title.
func (c *Collector) scheduleLabels() map[string]string {
	labels := map[string]string{}
	if c.swift == nil {
		return labels
	}
	snap := c.swift.Read()
	if snap.Known {
		tasks := append([]swiftstore.Task(nil), snap.Tasks...)
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Created < tasks[j].Created })
		for _, t := range tasks {
			if t.ScheduleID == nil || t.RootLabel == nil {
				continue
			}
			if label := strings.TrimSpace(*t.RootLabel); label != "" {
				labels[*t.ScheduleID] = label
			}
		}
	}
	for id, title := range c.swift.ScheduleTitles() {
		labels[id] = title
	}
	return labels
}

// noteSource logs the source the rows come from when it changes, and a
// failed ledger read each time it newly fails.
func (c *Collector) noteSource(source string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	failing := err != nil
	if source == c.source && failing == c.sourceFailing {
		return
	}
	c.source, c.sourceFailing = source, failing
	switch {
	case failing:
		log.Printf("usage analytics: reading the Swift usage ledger failed: %v; the history it holds is missing from answers", err)
	case source == sourceLedger:
		log.Printf("usage analytics: rows come from the assistants' transcripts, with the Swift usage ledger (%s) as history", c.ledger.Path())
	default:
		log.Printf("usage analytics: rows come from the assistants' transcripts; no Swift usage ledger is read")
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
