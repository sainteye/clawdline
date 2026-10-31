// Package issuesync keeps each opted-in Board item's external issue in step
// with the item (docs/issue-sync.md, D73).
//
// It reconciles rather than reacts. Nothing is attached to the many places an
// item's phase changes: a link row records what the tracker was last seen to
// hold, work.NextIssueOp says what the item's current phase owes it, and a
// pass makes that one call. The phase is durable before any call is made
// (D08), a failed call is the same question asked again later, and a call
// whose answer was lost is caught by the marker an issue carries.
package issuesync

import (
	"context"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// Link is one item's issue sync, as stored.
type Link struct {
	ItemID        string
	Target        work.IssueTarget
	Observed      work.IssueObserved
	ExternalKey   string // "#12" or "OPS-7"
	URL           string
	LastError     string
	Attempts      int
	NextAttemptAt time.Time
}

// Due is a link whose item phase owes the tracker a call.
type Due struct {
	Link        Link
	Title       string
	Description string
	Phase       work.Phase
}

// Result is what one call established.
type Result struct {
	Observed    work.IssueObserved
	ExternalKey string
	URL         string
}

// Store is the link table. Pending answers links whose next attempt is due
// and whose item phase owes a call; the store applies work.NextIssueOp
// itself or answers a superset, which RunOnce filters.
type Store interface {
	PendingIssueLinks(ctx context.Context, now time.Time, limit int) ([]Due, error)
	RecordIssueSynced(ctx context.Context, itemID string, r Result, at time.Time) error
	RecordIssueFailed(ctx context.Context, itemID, code string, attempts int, next, at time.Time) error
}

// Tracker performs one call. Create must first look for an issue an earlier
// attempt already opened (work.IssueMarker or its Jira label) and answer that
// one instead of opening a second.
type Tracker interface {
	Create(ctx context.Context, d Due) (Result, error)
	Close(ctx context.Context, d Due, notPlanned bool) error
	Reopen(ctx context.Context, d Due) error
}

// RetryAfter is implemented by an error that knows how long to wait.
type RetryAfter interface{ Wait() time.Duration }

// Code is a failure's stable name for the card and the log.
type Code interface{ Code() string }

// MaxLinksPerPass bounds the calls one pass makes, so a backlog after a long
// offline stretch is worked through steadily instead of in one burst.
const MaxLinksPerPass = 20

type Reconciler struct {
	Store    Store
	Trackers map[work.IssueProvider]Tracker
	Now      func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// RunOnce makes every call that is due now and answers how many it made.
// A failure is recorded on its own link and never stops the others.
func (r *Reconciler) RunOnce(ctx context.Context) (int, error) {
	due, err := r.Store.PendingIssueLinks(ctx, r.now(), MaxLinksPerPass)
	if err != nil {
		return 0, err
	}
	calls := 0
	for _, d := range due {
		if ctx.Err() != nil {
			return calls, ctx.Err()
		}
		op := work.NextIssueOp(d.Phase, d.Link.Observed)
		if op == work.IssueOpNone {
			continue
		}
		tracker := r.Trackers[d.Link.Target.Provider]
		if tracker == nil {
			continue
		}
		calls++
		result, err := r.call(ctx, tracker, op, d)
		at := r.now()
		if err != nil {
			attempts := d.Link.Attempts + 1
			if rerr := r.Store.RecordIssueFailed(ctx, d.Link.ItemID, failureCode(err), attempts, at.Add(backoff(err, attempts)), at); rerr != nil {
				return calls, rerr
			}
			continue
		}
		if err := r.Store.RecordIssueSynced(ctx, d.Link.ItemID, result, at); err != nil {
			return calls, err
		}
	}
	return calls, nil
}

func (r *Reconciler) call(ctx context.Context, t Tracker, op work.IssueOp, d Due) (Result, error) {
	keep := Result{ExternalKey: d.Link.ExternalKey, URL: d.Link.URL}
	switch op {
	case work.IssueOpCreate:
		return t.Create(ctx, d)
	case work.IssueOpCloseCompleted, work.IssueOpCloseNotPlanned:
		keep.Observed = work.IssueClosed
		return keep, t.Close(ctx, d, op == work.IssueOpCloseNotPlanned)
	case work.IssueOpReopen:
		keep.Observed = work.IssueOpen
		return keep, t.Reopen(ctx, d)
	}
	return Result{}, errors.New("unknown issue operation")
}

// backoff is the wait before a link is tried again: what the tracker asked
// for, or one minute doubling to a ceiling of an hour.
func backoff(err error, attempts int) time.Duration {
	var ra RetryAfter
	if errors.As(err, &ra) && ra.Wait() > 0 {
		return ra.Wait()
	}
	d := time.Minute
	for i := 1; i < attempts && d < MaxRetrySeconds*time.Second; i++ {
		d *= 2
	}
	if d > MaxRetrySeconds*time.Second {
		d = MaxRetrySeconds * time.Second
	}
	return d
}

// MaxRetrySeconds is the longest wait between two attempts on one link.
const MaxRetrySeconds = 3600

func failureCode(err error) string {
	var c Code
	if errors.As(err, &c) {
		return c.Code()
	}
	return "issue_sync_failed"
}
