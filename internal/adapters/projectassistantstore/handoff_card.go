package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This table is source-local. A Bot export or import does not transfer a
// pending draft, a claim, or the authority to adopt it after a move.
const handoffCardSchema = `CREATE TABLE IF NOT EXISTS project_assistant_handoff_cards (
 machine TEXT NOT NULL, bot TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot))`

const handoffReceiptSchema = `CREATE TABLE IF NOT EXISTS project_assistant_handoff_receipts (
 machine TEXT NOT NULL, bot TEXT NOT NULL, command_key TEXT NOT NULL, content_hash TEXT NOT NULL,
 PRIMARY KEY(machine,bot,command_key))`

var _ app.HandoffRepository = (*Store)(nil)

func loadHandoffCard(ctx context.Context, db reader, key pa.BotKey) (*pa.HandoffCard, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT data FROM project_assistant_handoff_cards WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var card pa.HandoffCard
	if err = json.Unmarshal(raw, &card); err != nil {
		return nil, err
	}
	if card.Fence.Key != key {
		return nil, failure(pa.ScopeDenied, "handoff_key")
	}
	return &card, nil
}

func writeHandoffCard(ctx context.Context, tx *sql.Tx, card pa.HandoffCard) error {
	raw, err := json.Marshal(card)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_handoff_cards(machine,bot,data) VALUES(?,?,?)
		ON CONFLICT(machine,bot) DO UPDATE SET data=excluded.data`, card.Fence.Key.MachineID, card.Fence.Key.BotID, raw)
	return err
}

func handoffFence(a aggregate) pa.Fence {
	c := a.Conversation
	return pa.Fence{Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
}

func matchesHandoff(a aggregate, card pa.HandoffCard) bool {
	return card.Fence == handoffFence(a) && card.MemoryRevision == a.Memory.Revision && card.InstructionRevision == a.Bot.Instructions.Revision
}

func canPrepareHandoff(a aggregate, admitting string) bool {
	main := a.Conversation.MainBindingID
	if main == "" {
		main = a.Conversation.BindingID
	}
	if b := a.Bindings[main]; b.NativeSessionID == "" || b.Revoked || a.Conversation.ActiveTurnID != "" {
		return false
	}
	for _, turn := range a.Turns {
		if turn.ID == admitting {
			continue
		}
		switch turn.State {
		case pa.TurnAccepted, pa.TurnQueued, pa.TurnRunning, pa.TurnWaitingPermission, pa.TurnUnknown:
			return false
		}
	}
	return true
}

func handoffView(a aggregate, card *pa.HandoffCard) app.HandoffView {
	view := app.HandoffView{Fence: handoffFence(a), CanPrepare: canPrepareHandoff(a, "")}
	if card == nil || card.Fence.Project != a.Bot.Project || card.Fence.ConversationID != a.Conversation.ID {
		return view
	}
	visible := *card
	if visible.State == pa.HandoffDraft && !matchesHandoff(a, visible) {
		visible.State, visible.BookmarkMessageID = pa.HandoffWithdrawn, ""
	}
	if visible.State == pa.HandoffDraft {
		view.CanPrepare = false
	} else {
		visible.Text = ""
	}
	view.Card = &visible
	return view
}

func (s *Store) ReadHandoff(ctx context.Context, key pa.BotKey, conversation string) (app.HandoffView, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return app.HandoffView{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, key)
	if err != nil {
		return app.HandoffView{}, err
	}
	if conversation != a.Conversation.ID {
		return app.HandoffView{}, failure(pa.ScopeDenied, "handoff_conversation")
	}
	card, err := loadHandoffCard(ctx, tx, key)
	if err != nil {
		return app.HandoffView{}, err
	}
	return handoffView(a, card), nil
}

func handoffReplay(ctx context.Context, db reader, cmd pa.Command) (bool, error) {
	var hash string
	err := db.QueryRowContext(ctx, `SELECT content_hash FROM project_assistant_handoff_receipts
		WHERE machine=? AND bot=? AND command_key=?`, cmd.Key.MachineID, cmd.Key.BotID, cmd.IdempotencyKey).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hash != cmd.ContentHash {
		return false, failure(pa.IdempotencyConflict, "handoff_command")
	}
	return true, nil
}

func (s *Store) HandoffReplay(ctx context.Context, cmd pa.Command) (bool, error) {
	return handoffReplay(ctx, s.db, cmd)
}

func (s *Store) WriteHandoff(ctx context.Context, write app.HandoffWrite) (app.HandoffView, error) {
	q := write.Request
	key := q.Command.Key
	unlock := s.lock(key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.HandoffView{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, key)
	if err != nil {
		return app.HandoffView{}, err
	}
	if a.Bot.Project != q.Fence.Project || a.Conversation.ID != q.Fence.ConversationID || key != q.Fence.Key {
		return app.HandoffView{}, failure(pa.ScopeDenied, "handoff_conversation")
	}
	card, err := loadHandoffCard(ctx, tx, key)
	if err != nil {
		return app.HandoffView{}, err
	}
	if replay, e := handoffReplay(ctx, tx, q.Command); e != nil {
		return app.HandoffView{}, e
	} else if replay {
		return handoffView(a, card), nil
	}
	if err = pa.CheckFence(q.Fence, handoffFence(a)); err != nil {
		return app.HandoffView{}, err
	}
	if q.Action == app.PrepareHandoff {
		if !handoffView(a, card).CanPrepare || write.Preview == nil || !matchesHandoff(a, *write.Preview) ||
			write.Preview.BookmarkMessageID != "" && !hasHandoffBookmark(a, write.Preview.BookmarkMessageID) {
			return app.HandoffView{}, failure(pa.StaleVersion, "handoff_prepare")
		}
		card = write.Preview
	} else {
		if card == nil || card.State != pa.HandoffDraft || q.Ref == nil || card.Ref != *q.Ref || !matchesHandoff(a, *card) {
			return app.HandoffView{}, failure(pa.StaleVersion, "handoff_revision")
		}
		switch q.Action {
		case app.EditHandoff:
			card.Text = q.Text
		case app.KeepHandoff:
			card.Text, card.State = "", pa.HandoffKept
		default:
			return app.HandoffView{}, failure(pa.Invalid, "handoff_action")
		}
		card.Ref.Revision++
	}
	if err = writeHandoffCard(ctx, tx, *card); err != nil {
		return app.HandoffView{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_handoff_receipts(machine,bot,command_key,content_hash) VALUES(?,?,?,?)`,
		key.MachineID, key.BotID, q.Command.IdempotencyKey, q.Command.ContentHash); err != nil {
		return app.HandoffView{}, err
	}
	if err = tx.Commit(); err != nil {
		return app.HandoffView{}, err
	}
	return handoffView(a, card), nil
}

func hasHandoffBookmark(a aggregate, id string) bool {
	for _, message := range a.Messages {
		if message.ID == id {
			return true
		}
	}
	return false
}

func (s *Store) CommitHandoffInput(ctx context.Context, m app.Mutation, fence pa.Fence, ref *pa.HandoffRef) (pa.Receipt, error) {
	unlock := s.lock(m.Command.Key)
	defer unlock()
	return s.commitWith(ctx, m, func(ctx context.Context, tx *sql.Tx, a aggregate) error {
		card, err := loadHandoffCard(ctx, tx, m.Command.Key)
		if err != nil {
			return err
		}
		ready := card != nil && card.State == pa.HandoffDraft && matchesHandoff(a, *card)
		if ref == nil && !ready {
			return nil
		}
		if !ready || ref == nil || card.Ref != *ref {
			return failure(pa.StaleVersion, "handoff_revision")
		}
		if fence != card.Fence || m.Turn == nil || m.Turn.Fence != fence ||
			m.Turn.Origin != pa.UserOrigin || m.Turn.State != pa.TurnQueued || !canPrepareHandoff(a, m.Turn.ID) {
			return failure(pa.StaleVersion, "handoff_admission")
		}
		card.State, card.TurnID = pa.HandoffAdopted, m.Turn.ID
		return writeHandoffCard(ctx, tx, *card)
	})
}

func (s *Store) HandoffForTurn(ctx context.Context, key pa.BotKey, turnID string) (*pa.HandoffCard, error) {
	card, err := loadHandoffCard(ctx, s.db, key)
	if err != nil || card == nil || card.TurnID != turnID {
		return nil, err
	}
	return card, nil
}

func (s *Store) ClaimHandoffText(ctx context.Context, expected pa.HandoffCard) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	card, err := loadHandoffCard(ctx, tx, expected.Fence.Key)
	if err != nil {
		return "", err
	}
	if card == nil || *card != expected || card.State != pa.HandoffAdopted || card.Claimed {
		return "", failure(pa.ContextStale, "handoff_claim")
	}
	text := card.Text
	card.Text, card.Claimed = "", true
	if err = writeHandoffCard(ctx, tx, *card); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return text, nil
}

func forgetHandoffCard(ctx context.Context, tx *sql.Tx, key pa.BotKey) error {
	card, err := loadHandoffCard(ctx, tx, key)
	if err != nil || card == nil {
		return err
	}
	card.BookmarkMessageID, card.Text = "", ""
	if card.State == pa.HandoffDraft || card.State == pa.HandoffAdopted && !card.Claimed {
		card.State = pa.HandoffWithdrawn
	}
	return writeHandoffCard(ctx, tx, *card)
}

// A terminal input that never reached the prompt boundary no longer needs
// its summary. Keep its adoption marker for receipt and recovery correlation.
func settleHandoffInput(ctx context.Context, tx *sql.Tx, turn *pa.Turn) error {
	if turn == nil || turn.Origin != pa.UserOrigin {
		return nil
	}
	switch turn.State {
	case pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown, pa.TurnSucceeded:
	default:
		return nil
	}
	card, err := loadHandoffCard(ctx, tx, turn.Fence.Key)
	if err != nil || card == nil || card.TurnID != turn.ID || card.State != pa.HandoffAdopted || card.Claimed {
		return err
	}
	card.Text, card.State = "", pa.HandoffWithdrawn
	return writeHandoffCard(ctx, tx, *card)
}
