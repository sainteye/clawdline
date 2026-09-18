package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	boardstore "github.com/sainteye/clawdline-go/internal/adapters/board"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// The board's two settings, kept in clawdline.sqlite3 (design-decisions D37).
//
// A command is claimed under its receipt — scope `board`, the viewer, the
// requestId — before it is decided; it is decided and written, with its
// receipt completed, in one transaction; and a resend is answered with the
// revision that command produced, which the receipt holds, not with the
// revision the board has reached since. The receipt window is the store's
// (24 hours): a full window refuses a new command with 429, and nothing
// inside it is evicted — the old document evicted the oldest receipt by count,
// which is how a retry becomes a second change.

// boardScope is the receipt scope of board commands.
const boardScope = "board"

// boardSettingsError is a store failure as the board's own refusal.
func boardSettingsError(err error) error {
	var refusal boardstore.Refusal
	switch {
	case errors.As(err, &refusal):
		return refusal
	case errors.Is(err, store.ErrBusy):
		return boardstore.Refusal{Status: http.StatusServiceUnavailable, Code: "store_busy",
			Message: "The store was held by another writer; nothing was done. Retry."}
	case errors.Is(err, store.ErrConflict):
		return boardstore.Refusal{Status: http.StatusConflict, Code: "revision_conflict",
			Message: "The board has moved since you read it."}
	}
	return boardstore.Refusal{Status: http.StatusServiceUnavailable, Code: "board_store_unreadable", Message: err.Error()}
}

// boardSettings reads the board-level facts, and writes nothing (DG-6: a
// read has a read's effect). Before the old document has been carried over,
// it answers what that document says; a document that cannot be read is a
// refusal, as it was before, and never a fresh board.
func (s *Server) boardSettings(ctx context.Context) (boardstore.SettingsState, error) {
	row, err := s.store.BoardSettings(ctx)
	if errors.Is(err, store.ErrNoBoardSettings) {
		doc, found, derr := s.board().settings.Document()
		switch {
		case derr != nil:
			return boardstore.SettingsState{}, derr
		case !found:
			// Nothing was ever written: a fresh board is enabled.
			return boardstore.SettingsState{Enabled: true}, nil
		}
		row, err = store.BoardSettings(doc), nil
	}
	if err != nil {
		return boardstore.SettingsState{}, boardSettingsError(err)
	}
	return boardstore.SettingsState{Revision: row.Revision, Enabled: row.Enabled,
		NarrativeConsent: row.NarrativeConsent, UpdatedAt: row.UpdatedAt}, nil
}

// carryBoardSettings writes the old document's facts into the store, once:
// when there is no row yet and there is a document. It runs when the daemon
// starts and before the first command, never on a read.
func (s *Server) carryBoardSettings(ctx context.Context) error {
	if _, err := s.store.BoardSettings(ctx); !errors.Is(err, store.ErrNoBoardSettings) {
		if err != nil {
			return boardSettingsError(err)
		}
		return nil
	}
	d := s.board()
	doc, found, err := d.settings.Document()
	if err != nil || !found {
		return err
	}
	if _, err := s.store.SeedBoardSettings(ctx, store.BoardSettings(doc), d.settings.Path()); err != nil {
		return boardSettingsError(err)
	}
	return nil
}

// boardReceiptAnswer is what a board command's receipt keeps: the revision it
// produced.
type boardReceiptAnswer struct {
	Revision int64 `json:"revision"`
}

// boardApply carries out one board command under its receipt. answered is
// true when the request has already been answered — refused, or in progress.
func (s *Server) boardApply(w http.ResponseWriter, r *http.Request, actor string, c boardstore.Command, raw []byte) (boardstore.Outcome, bool) {
	if err := boardstore.ValidateCommand(actor, c); err != nil {
		writeBoardRefusal(w, err, false)
		return boardstore.Outcome{}, true
	}
	// Before the first command, the old document's facts are carried over:
	// a command decides from the person's setting, never from a fresh board.
	if err := s.carryBoardSettings(r.Context()); err != nil {
		writeBoardRefusal(w, err, false)
		return boardstore.Outcome{}, true
	}
	k := store.ReceiptKey{Scope: boardScope, Actor: actor, Key: c.RequestID}
	policy := store.ReceiptPolicy{Limit: int(CapacityLimit(capacity.BoardReceipts))}
	replay, proceed := s.claimOnce(w, r, k, boardstore.CommandDigest(raw), policy, "request_id_conflict")
	if replay != nil {
		var a boardReceiptAnswer
		if err := json.Unmarshal(replay.Body, &a); err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "board_store_unreadable", "The command's receipt could not be read.")
			return boardstore.Outcome{}, true
		}
		// The same command twice changes nothing twice, and says what the
		// first time did.
		return boardstore.Outcome{Revision: a.Revision, Replayed: true}, false
	}
	if !proceed {
		return boardstore.Outcome{}, true
	}
	next, err := s.store.ApplyBoardSettings(r.Context(), k,
		func(cur store.BoardSettings) (store.BoardSettings, error) {
			row, err := boardstore.Transition(boardstore.SettingsRow(cur), c, time.Now())
			return store.BoardSettings(row), err
		},
		func(next store.BoardSettings) store.ReceiptAnswer {
			body, _ := json.Marshal(boardReceiptAnswer{Revision: next.Revision})
			return store.ReceiptAnswer{Status: http.StatusOK, Body: body}
		})
	if err != nil {
		// Nothing was changed: the key is given back, and a resend is decided
		// afresh.
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		writeBoardRefusal(w, boardSettingsError(err), false)
		return boardstore.Outcome{}, true
	}
	return boardstore.Outcome{Revision: next.Revision}, false
}

// boardReceiptReading is the `board.receipts` row: the board scope's receipts
// inside their window.
func (s *Server) boardReceiptReading() capacity.Reading {
	uses, err := s.store.ReceiptUses(context.Background(), store.ReceiptWindow, time.Now())
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	r := capacity.Reading{Known: true}
	for _, u := range uses {
		if u.Scope == boardScope {
			r.Used = int64(u.Live)
		}
	}
	return r
}
