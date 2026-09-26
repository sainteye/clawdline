package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Offering back the sessions a reboot took away (docs/session-restore.md).
//
// Every complete reading of the machine is shown to Observe, which keeps the
// current boot's rows equal to the conversations that reading saw. After the
// next boot, the previous boot's rows — less those already answered and those
// open again — are what Restorable offers, and Restore opens them through the
// one launcher this daemon has (Starter.Resume).

// The shipped bounds, each registered in internal/domain/capacity.
const (
	// restoreRowsLimit is how many conversations one boot records.
	restoreRowsLimit = 200
	// restoreBootsLimit is how many boots are kept: this one and the one
	// before it.
	restoreBootsLimit = 2
	// restoreBatchLimit is how many conversations one restore may name.
	restoreBatchLimit = 20
	// restoreSeenLimit is how stale a recorded last_seen may grow while the
	// set of open conversations stays the same.
	restoreSeenLimit = 5 * time.Minute
)

// The reason Restorable is unavailable.
const RestoreBootUnknown = "boot_unknown"

// The per-row codes Restore answers.
const (
	RestoreNotRestorable        = "not_restorable"
	RestorePlaceUnavailable     = "place_unavailable"
	RestoreConversationNotFound = "conversation_not_found"
	RestoreOpenFailed           = "open_failed"
	RestoreOverCapacity         = "over_capacity"
)

// ErrRestorePlaceUnavailable is what a Resume answers when the row's
// directory is no longer a place this machine can open.
var ErrRestorePlaceUnavailable = errors.New("that directory is no longer a place this machine can open")

// ErrRestoreOverCapacity is what a Resume answers when the opening queue was
// full.
var ErrRestoreOverCapacity = errors.New("other sessions are already being opened on this machine")

// ErrRestoreBatch is a restore that names more conversations than one request
// may.
var ErrRestoreBatch = errors.New("too many conversations in one restore")

// RestoreStore is the part of the store this needs.
type RestoreStore interface {
	RecordBoot(ctx context.Context, boot string, rows []store.RestoreRow, now time.Time, keep int) error
	PreviousBoot(ctx context.Context, current string) (store.RestoreBoot, bool, error)
	RestoreRows(ctx context.Context, boot string) ([]store.RestoreRow, error)
	ResolveRestore(ctx context.Context, boot string, ids []string, resolution string, now time.Time) (int64, error)
}

// SessionRestore records and offers. The zero limits are the shipped ones.
type SessionRestore struct {
	Store RestoreStore
	// Boot answers this boot's id (internal/adapters/bootid.Read).
	Boot func(ctx context.Context) (string, error)
	// Children is the terminal ids among rows that are the broker's own
	// children. Restoring a child outside its task is wrong, so they are never
	// recorded.
	Children func(ctx context.Context, rows []session.Session) map[string]bool
	// Title names a row: the manual title when one is known, else the
	// session's own. Nil is the session's own.
	Title func(ctx context.Context, row session.Session) string
	Now   func() time.Time

	RowsLimit, BootsLimit, BatchLimit int
	SeenEvery                         time.Duration

	mu        sync.Mutex
	boot      string
	bootErr   error
	lastKey   string
	lastWrite time.Time
	written   bool
	dropped   int64
	writeErr  error
}

func (r *SessionRestore) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *SessionRestore) rowsLimit() int  { return orDefault(r.RowsLimit, restoreRowsLimit) }
func (r *SessionRestore) bootsLimit() int { return orDefault(r.BootsLimit, restoreBootsLimit) }
func (r *SessionRestore) batchLimit() int { return orDefault(r.BatchLimit, restoreBatchLimit) }
func (r *SessionRestore) seenEvery() time.Duration {
	if r.SeenEvery > 0 {
		return r.SeenEvery
	}
	return restoreSeenLimit
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// currentBoot is this boot's id, read once. A failed read is asked again on
// the next call rather than remembered: the id does not change while this
// process runs, and a transient failure must not switch the feature off.
func (r *SessionRestore) currentBoot(ctx context.Context) (string, error) {
	r.mu.Lock()
	if r.boot != "" {
		b := r.boot
		r.mu.Unlock()
		return b, nil
	}
	r.mu.Unlock()
	if r.Boot == nil {
		return "", errors.New("no boot id source")
	}
	id, err := r.Boot(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || id == "" {
		if err == nil {
			err = errors.New("the boot id source answered empty")
		}
		r.bootErr = err
		return "", err
	}
	r.boot, r.bootErr = id, nil
	return id, nil
}

// restorable is a row worth recording: an assistant this daemon can resume,
// with the conversation id that resuming needs.
func restorable(s session.Session) bool {
	return s.ConversationID != "" && s.CWD != "" &&
		(s.Assistant == session.AssistantClaude || s.Assistant == session.AssistantCodex)
}

// Observe records one reading. Only a complete reading changes anything, and
// only a changed set of conversations — or a last_seen older than SeenEvery —
// is written.
func (r *SessionRestore) Observe(ctx context.Context, inv session.Inventory) {
	if r == nil || r.Store == nil || !inv.Complete {
		return
	}
	boot, err := r.currentBoot(ctx)
	if err != nil {
		return
	}
	var candidates []session.Session
	for _, s := range inv.Sessions {
		if restorable(s) {
			candidates = append(candidates, s)
		}
	}
	key := restoreKey(candidates)
	now := r.now()

	// One observer at a time: two scans finishing together must not write
	// their sets in the other order.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written && key == r.lastKey && now.Sub(r.lastWrite) < r.seenEvery() {
		return
	}

	var children map[string]bool
	if r.Children != nil && len(candidates) > 0 {
		children = r.Children(ctx, candidates)
	}
	seen := map[string]bool{}
	var kept []session.Session
	for _, s := range candidates {
		if children[s.ID] || seen[s.ConversationID] {
			continue
		}
		seen[s.ConversationID] = true
		kept = append(kept, s)
	}
	// Past the limit, the conversations that moved longest ago give way.
	var dropped int64
	if limit := r.rowsLimit(); len(kept) > limit {
		sort.SliceStable(kept, func(i, j int) bool {
			return kept[i].Activity.At.After(kept[j].Activity.At)
		})
		dropped = int64(len(kept) - limit)
		kept = kept[:limit]
	}
	rows := make([]store.RestoreRow, 0, len(kept))
	for _, s := range kept {
		title := s.CustomTitle
		if title == "" {
			title = s.Label
		}
		if r.Title != nil {
			if t := r.Title(ctx, s); t != "" {
				title = t
			}
		}
		rows = append(rows, store.RestoreRow{
			ConversationID: s.ConversationID, Assistant: string(s.Assistant), CWD: s.CWD,
			Place: projects.PlaceID(s.CWD), Title: title, Backend: string(s.Backend),
		})
	}
	if err := r.Store.RecordBoot(ctx, boot, rows, now, r.bootsLimit()); err != nil {
		r.writeErr = err
		return
	}
	r.writeErr = nil
	r.lastKey, r.lastWrite, r.written = key, now, true
	r.dropped += dropped
}

// Dropped is how many conversations have gone unrecorded past RowsLimit.
func (r *SessionRestore) Dropped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

func restoreKey(rows []session.Session) string {
	parts := make([]string, 0, len(rows))
	for _, s := range rows {
		parts = append(parts, strings.Join([]string{s.ConversationID, string(s.Assistant), s.CWD,
			string(s.Backend), s.ID, s.Label, s.CustomTitle}, "\x1f"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1e")
}

// Restorable is what Restorable answers.
type Restorable struct {
	Available bool
	Reason    string
	// Previous is the boot the rows are from; zero when there is none.
	Previous store.RestoreBoot
	Rows     []store.RestoreRow
}

// Restorable is the previous boot's unanswered rows that are not open now.
// live is a reading of the machine; a conversation in it is not offered.
func (r *SessionRestore) Restorable(ctx context.Context, live session.Inventory) (Restorable, error) {
	boot, err := r.currentBoot(ctx)
	if err != nil {
		return Restorable{Available: false, Reason: RestoreBootUnknown, Rows: []store.RestoreRow{}}, nil
	}
	out := Restorable{Available: true, Rows: []store.RestoreRow{}}
	prev, ok, err := r.Store.PreviousBoot(ctx, boot)
	if err != nil {
		return Restorable{}, err
	}
	if !ok {
		return out, nil
	}
	out.Previous = prev
	rows, err := r.Store.RestoreRows(ctx, prev.ID)
	if err != nil {
		return Restorable{}, err
	}
	open := map[string]bool{}
	for _, s := range live.Sessions {
		if s.ConversationID != "" {
			open[s.ConversationID] = true
		}
	}
	for _, row := range rows {
		if row.Resolution != "" || open[row.ConversationID] {
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// RestoreResult is one row of a restore's answer.
type RestoreResult struct {
	ConversationID string
	OK             bool
	Code           string
	Message        string
	Started        Started
}

// Restore opens each named conversation that is on offer, one at a time,
// through resume, and marks each one that opened as restored. A name not on
// offer is answered not_restorable and nothing is opened for it.
func (r *SessionRestore) Restore(ctx context.Context, ids []string, live session.Inventory,
	resume func(context.Context, store.RestoreRow) (Started, error)) ([]RestoreResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) > r.batchLimit() {
		return nil, ErrRestoreBatch
	}
	offer, err := r.Restorable(ctx, live)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.RestoreRow{}
	for _, row := range offer.Rows {
		byID[row.ConversationID] = row
	}
	out := make([]RestoreResult, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !offer.Available || !ok {
			out = append(out, RestoreResult{ConversationID: id, Code: RestoreNotRestorable,
				Message: "That conversation is not one this machine is offering to restore."})
			continue
		}
		made, err := resume(ctx, row)
		if err != nil {
			code, message := restoreFailure(err)
			out = append(out, RestoreResult{ConversationID: id, Code: code, Message: message})
			continue
		}
		// The tab is open whatever the store says next; a row that could not
		// be marked is offered again, which is the safer of the two mistakes.
		_, _ = r.Store.ResolveRestore(ctx, offer.Previous.ID, []string{id}, store.RestoreRestored, r.now())
		out = append(out, RestoreResult{ConversationID: id, OK: true, Started: made})
	}
	return out, nil
}

// Dismiss answers the named rows — every one on offer when ids is nil — as
// not wanted, and says how many it changed.
func (r *SessionRestore) Dismiss(ctx context.Context, ids []string) (int64, bool, error) {
	boot, err := r.currentBoot(ctx)
	if err != nil {
		return 0, false, nil
	}
	prev, ok, err := r.Store.PreviousBoot(ctx, boot)
	if err != nil || !ok {
		return 0, true, err
	}
	if ids != nil {
		ids = uniqueIDs(ids)
	}
	n, err := r.Store.ResolveRestore(ctx, prev.ID, ids, store.RestoreDismissed, r.now())
	return n, true, err
}

func restoreFailure(err error) (string, string) {
	switch {
	case errors.Is(err, ErrRestorePlaceUnavailable):
		return RestorePlaceUnavailable, "That directory is no longer a place this machine can open."
	case errors.Is(err, ErrRestoreOverCapacity):
		return RestoreOverCapacity, "Other sessions are already being opened on this machine; try this one again shortly."
	}
	var refusal StartRefusal
	if errors.As(err, &refusal) {
		if refusal.Code == "not_found" {
			return RestoreConversationNotFound, "This machine no longer lists that conversation for its directory."
		}
		return RestoreOpenFailed, refusal.Message
	}
	return RestoreOpenFailed, "This machine could not open that session."
}

func uniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
