package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Archiving a Session (docs/session-archive.md).
//
// A Session that has been quiet for days still holds its assistant's memory
// while its process runs. Archiving it is the ordinary close — every
// protection Actions.Close has, answered with the same refusals — followed by
// a durable row naming the conversation, so it can be resumed whenever the
// person wants it back. The row is written only after the close succeeded;
// a refused or failed close writes nothing. Restoring resumes the
// conversation through the same resume path a reboot restore uses, and the
// row goes only when that open succeeded.

// The shipped bounds, each registered in internal/domain/capacity.
const (
	// archiveRowsLimit is how many archived conversations are kept; past it
	// the ones archived longest ago are dropped.
	archiveRowsLimit = 500
	// archiveBatchLimit is how many conversations one restore may name.
	archiveBatchLimit = 20
)

// The refusals Archive adds to the close's own.
const (
	// ArchiveNoConversation: the Session has no conversation id this machine
	// could resume, so archiving it would only close it.
	ArchiveNoConversation = "archive_no_conversation"
	// ArchiveUnavailable: this daemon keeps no archive record.
	ArchiveUnavailable = "archive_unavailable"
	// ArchiveNotRecorded: the Session was closed and the row could not be
	// written.
	ArchiveNotRecorded = "archive_not_recorded"
)

// The per-row codes RestoreArchived answers besides the reboot restore's
// place_unavailable, conversation_not_found, open_failed and over_capacity.
const (
	ArchiveNotArchived = "not_archived"
	ArchiveAlreadyOpen = "already_open"
)

// ErrArchiveBatch is a restore that names more conversations than one
// request may.
var ErrArchiveBatch = errors.New("too many conversations in one archive restore")

// ArchiveStore is the part of the store this needs.
type ArchiveStore interface {
	ArchiveSession(ctx context.Context, row store.ArchiveRow, keep int) (int64, error)
	ArchivedSessions(ctx context.Context) ([]store.ArchiveRow, error)
	RemoveArchived(ctx context.Context, conversation string) error
}

// SessionArchive keeps the archive record. The zero limits are the shipped
// ones.
type SessionArchive struct {
	Store ArchiveStore
	// Title is the label the Session row shows. Nil is the Session's own
	// custom title, else its label.
	Title func(ctx context.Context, row session.Session) string
	Now   func() time.Time

	RowsLimit, BatchLimit int

	mu      sync.Mutex
	dropped int64
}

func (a *SessionArchive) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *SessionArchive) rowsLimit() int  { return orDefault(a.RowsLimit, archiveRowsLimit) }
func (a *SessionArchive) batchLimit() int { return orDefault(a.BatchLimit, archiveBatchLimit) }

// Dropped is how many archived rows have been dropped past RowsLimit.
func (a *SessionArchive) Dropped() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dropped
}

// row is what the record keeps of s, as of now.
func (a *SessionArchive) row(ctx context.Context, s session.Session) store.ArchiveRow {
	title := s.CustomTitle
	if title == "" {
		title = s.Label
	}
	if a.Title != nil {
		if t := a.Title(ctx, s); t != "" {
			title = t
		}
	}
	return store.ArchiveRow{ConversationID: s.ConversationID, Assistant: string(s.Assistant), CWD: s.CWD,
		Place: projects.PlaceID(s.CWD), Title: title, Persona: s.Persona, Backend: string(s.Backend),
		ArchivedAt: a.now()}
}

func (a *SessionArchive) record(ctx context.Context, row store.ArchiveRow) error {
	dropped, err := a.Store.ArchiveSession(ctx, row, a.rowsLimit())
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.dropped += dropped
	a.mu.Unlock()
	return nil
}

// Archive closes the Session exactly as Close does — the same closeability
// check, the same refusals, `force` over `close_blocked` and never over
// `closeability_unknown` — and then records its conversation. A Session with
// no conversation to resume is refused before anything is closed.
func (a Actions) Archive(ctx context.Context, id string, force bool) (session.Session, store.ArchiveRow, error) {
	if a.Archives == nil || a.Archives.Store == nil {
		return session.Session{}, store.ArchiveRow{}, Refusal{Code: ArchiveUnavailable,
			Detail: "this machine keeps no archive record"}
	}
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, store.ArchiveRow{}, err
	}
	if !restorable(s) {
		return s, store.ArchiveRow{}, Refusal{Code: ArchiveNoConversation,
			Detail: "this session has no conversation this machine could resume, so archiving it would only close it"}
	}
	// The label is read while the row is still on the machine's list.
	row := a.Archives.row(ctx, s)
	closed, err := a.Close(ctx, id, force)
	if err != nil {
		return closed, store.ArchiveRow{}, err
	}
	if err := a.Archives.record(ctx, row); err != nil {
		return closed, store.ArchiveRow{}, Refusal{Code: ArchiveNotRecorded,
			Detail: "the session was closed and its archive record could not be written; " +
				"its conversation is still in the assistant's own history", Cause: err}
	}
	return closed, row, nil
}

// Archived is every archived conversation, most recently archived first.
func (a *SessionArchive) Archived(ctx context.Context) ([]store.ArchiveRow, error) {
	return a.Store.ArchivedSessions(ctx)
}

// RestoreArchived resumes each named archived conversation, one at a time,
// through resume, and removes the row of each one that opened. A name with no
// row is answered not_archived, and one open in a terminal now (live)
// already_open; nothing is opened for either.
func (a *SessionArchive) RestoreArchived(ctx context.Context, ids []string, live session.Inventory,
	resume func(context.Context, store.ArchiveRow) (Started, error)) ([]RestoreResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) > a.batchLimit() {
		return nil, ErrArchiveBatch
	}
	rows, err := a.Store.ArchivedSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.ArchiveRow{}
	for _, row := range rows {
		byID[row.ConversationID] = row
	}
	open := map[string]bool{}
	for _, s := range live.Sessions {
		if s.ConversationID != "" {
			open[s.ConversationID] = true
		}
	}
	out := make([]RestoreResult, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			out = append(out, RestoreResult{ConversationID: id, Code: ArchiveNotArchived,
				Message: "That conversation is not in this machine's archive."})
			continue
		}
		if open[id] {
			out = append(out, RestoreResult{ConversationID: id, Code: ArchiveAlreadyOpen,
				Message: "That conversation is already open in a terminal on this machine."})
			continue
		}
		made, err := resume(ctx, row)
		if err != nil {
			code, message := restoreFailure(err)
			out = append(out, RestoreResult{ConversationID: id, Code: code, Message: message})
			continue
		}
		// The tab is open whatever the store says next; a row that could not
		// be removed is offered again, and restoring it answers already_open.
		_ = a.Store.RemoveArchived(ctx, id)
		out = append(out, RestoreResult{ConversationID: id, OK: true, Started: made})
	}
	return out, nil
}
