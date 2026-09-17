package analytics

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
)

// The Swift app's own ledger, when there is one, is where the Usage page's
// rows come from: it is the record the Swift page draws, so reading it is the
// only way the two pages can show the same numbers. The transcript collector
// below it is the answer on a machine with no ledger — Linux, Windows, or a Mac
// that never ran the Swift app.
//
// Which of the two answered is not on the wire: the Swift payload has no field
// for it, and this daemon does not invent one. A reading carried after a
// failed copy shows up where the Swift page shows an old ledger, in
// `freshness` (its `latestObservedAt` is the carried reading's).

// Where the rows came from, for the log line written when it changes.
const (
	sourceLedger      = "swift_ledger"
	sourceTranscripts = "transcripts"
)

// ErrLedgerUnreadable means the Swift app's ledger exists and could not be
// read, and there is no earlier reading to carry. Nothing is answered then:
// the transcripts would be a different set of numbers under the same page, and
// an empty answer would read as an idle month. The message leaves the path out
// because paths do not cross this surface.
var ErrLedgerUnreadable = errors.New("The Swift app's usage ledger could not be read, so Usage Analytics does not know these numbers.")

// ledgerCache is one ledger reading turned into rows, kept until the reading
// changes.
type ledgerCache struct {
	readAt time.Time
	rows   []Row
	latest time.Time
}

// ledgerRows answers from the Swift ledger. ok is false when there is no
// ledger and the transcripts should answer instead.
func (c *Collector) ledgerRows(ctx context.Context, since time.Time) (rows []Row, ok bool, err error) {
	if c.ledger == nil {
		return nil, false, nil
	}
	reading := c.ledger.Read(ctx)
	switch {
	case reading.Missing:
		c.noteSource(sourceTranscripts, nil)
		return nil, false, nil
	case !reading.Known:
		c.noteSource(sourceLedger, reading.Err)
		if errors.Is(reading.Err, swiftstore.ErrLedgerChanging) ||
			errors.Is(reading.Err, context.DeadlineExceeded) || errors.Is(reading.Err, context.Canceled) {
			// The Swift app is writing; the next try is likely to land.
			return nil, true, ErrBusy
		}
		return nil, true, ErrLedgerUnreadable
	}
	c.noteSource(sourceLedger, reading.Err)

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
	return out, true, nil
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
		log.Printf("usage analytics: reading the Swift usage ledger failed: %v", err)
	case source == sourceLedger:
		log.Printf("usage analytics: rows come from the Swift usage ledger (%s)", c.ledger.Path())
	default:
		log.Printf("usage analytics: no Swift usage ledger; rows come from the assistants' transcripts")
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
