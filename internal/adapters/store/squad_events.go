package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	MaxSquadEventIDBytes  = 128
	MaxSquadEventPageRows = 100
	// MaxSquadEventConversations is how many conversations one filtered
	// event read may name: the open Session and the list rows on screen.
	MaxSquadEventConversations = 50
	// MaxSquadEventConversationsBytes is the comma-joined form of that
	// many IDs at their longest, which the relay copies as its byte bound.
	MaxSquadEventConversationsBytes = MaxSquadEventConversations*(MaxSquadEventIDBytes+1) - 1
	// MaxSquadSnapshotConversations is how many conversations one batched
	// launch-snapshot read may name: the role rows on screen. IDs follow the
	// same rule as the event filter.
	MaxSquadSnapshotConversations = 50
	// MaxSquadSnapshotConversationsBytes is that list comma-joined at its
	// longest, which the relay copies as its byte bound.
	MaxSquadSnapshotConversationsBytes = MaxSquadSnapshotConversations*(MaxSquadEventIDBytes+1) - 1
)

var (
	ErrSquadActorUnauthorized = errors.New("squad_actor_unauthorized")
	ErrSquadEventInvalid      = errors.New("squad_event_invalid")
	ErrSquadEventConflict     = errors.New("squad_event_conflict")
)

const squadEventSchema = `
CREATE TABLE IF NOT EXISTS squad_skill_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  receipt_id TEXT NOT NULL UNIQUE,
  launch_id TEXT NOT NULL REFERENCES squad_launches(id),
  snapshot_id TEXT NOT NULL REFERENCES squad_snapshots(id),
  conversation_id TEXT NOT NULL,
  definition_id TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  skill_id TEXT NOT NULL,
  skill_version TEXT NOT NULL,
  client_event_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('read','applied','failed')),
  failure_code TEXT NOT NULL DEFAULT '',
  payload_digest TEXT NOT NULL,
  at INTEGER NOT NULL,
  UNIQUE(launch_id, client_event_id)
);
CREATE INDEX IF NOT EXISTS squad_skill_events_conversation_seq ON squad_skill_events(conversation_id,seq);
`

func openSquadEvents(db *sql.DB) error {
	_, err := db.Exec(squadEventSchema)
	return err
}

// SquadSkillEvent is what one bound agent reports. Identity and time are
// filled from the actor and the server; the caller cannot choose them.
type SquadSkillEvent struct {
	SkillID       string `json:"skill_id"`
	SkillVersion  string `json:"skill_version"`
	ClientEventID string `json:"client_event_id"`
	Status        string `json:"status"`
	FailureCode   string `json:"failure_code,omitempty"`
}

type SquadSkillReceipt struct {
	Seq            int64  `json:"seq"`
	ReceiptID      string `json:"receipt_id"`
	SnapshotID     string `json:"snapshot_id"`
	ConversationID string `json:"conversation_id"`
	DefinitionID   string `json:"definition_id"`
	ScopeID        string `json:"scope_id"`
	SkillID        string `json:"skill_id"`
	SkillVersion   string `json:"skill_version"`
	Status         string `json:"status"`
	FailureCode    string `json:"failure_code,omitempty"`
	At             int64  `json:"at"`
}

type squadSnapshotForEvent struct {
	DefinitionID string `json:"definition_id"`
	ScopeID      string `json:"scope_id"`
	Skills       []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	} `json:"skills"`
}

func validSquadEvent(event SquadSkillEvent) bool {
	if event.SkillID == "" || event.SkillVersion == "" || event.ClientEventID == "" ||
		len(event.ClientEventID) > MaxSquadEventIDBytes {
		return false
	}
	if event.Status != "read" && event.Status != "applied" && event.Status != "failed" {
		return false
	}
	return event.Status == "failed" || event.FailureCode == ""
}

// RecordSquadSkillEvent validates the capability and enabled skill against
// the immutable snapshot, then writes the event and receipt in one SQLite
// transaction. A retry with identical content returns the original receipt.
func (s *Store) RecordSquadSkillEvent(ctx context.Context, capability string, event SquadSkillEvent) (SquadSkillReceipt, error) {
	if !validSquadEvent(event) {
		return SquadSkillReceipt{}, ErrSquadEventInvalid
	}
	if capability == "" {
		return SquadSkillReceipt{}, ErrSquadActorUnauthorized
	}
	capSum := sha256.Sum256([]byte(capability))
	payload, err := json.Marshal(event)
	if err != nil {
		return SquadSkillReceipt{}, err
	}
	payloadSum := sha256.Sum256(payload)
	digest := hex.EncodeToString(payloadSum[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SquadSkillReceipt{}, classify(err)
	}
	defer tx.Rollback()
	var actor SquadActor
	var document []byte
	err = tx.QueryRowContext(ctx, `SELECT l.id,l.snapshot_id,l.terminal_id,l.conversation_id,s.document
		FROM squad_launches l JOIN squad_snapshots s ON s.id=l.snapshot_id
		WHERE l.actor_hash=? AND l.state='bound'`, hex.EncodeToString(capSum[:])).Scan(
		&actor.LaunchID, &actor.SnapshotID, &actor.TerminalID, &actor.ConversationID, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return SquadSkillReceipt{}, ErrSquadActorUnauthorized
	}
	if err != nil {
		return SquadSkillReceipt{}, classify(err)
	}
	var snapshot squadSnapshotForEvent
	if err := json.Unmarshal(document, &snapshot); err != nil || snapshot.DefinitionID == "" || snapshot.ScopeID == "" {
		return SquadSkillReceipt{}, ErrSquadEventInvalid
	}
	allowed := false
	for _, skill := range snapshot.Skills {
		if skill.ID == event.SkillID && skill.Version == event.SkillVersion && skill.Enabled {
			allowed = true
			break
		}
	}
	if !allowed {
		return SquadSkillReceipt{}, ErrSquadEventInvalid
	}
	var prior SquadSkillReceipt
	var priorDigest string
	err = tx.QueryRowContext(ctx, `SELECT seq,receipt_id,snapshot_id,conversation_id,definition_id,scope_id,
		skill_id,skill_version,status,failure_code,at,payload_digest FROM squad_skill_events
		WHERE launch_id=? AND client_event_id=?`, actor.LaunchID, event.ClientEventID).Scan(
		&prior.Seq, &prior.ReceiptID, &prior.SnapshotID, &prior.ConversationID, &prior.DefinitionID, &prior.ScopeID,
		&prior.SkillID, &prior.SkillVersion, &prior.Status, &prior.FailureCode, &prior.At, &priorDigest)
	if err == nil {
		if priorDigest != digest {
			return SquadSkillReceipt{}, ErrSquadEventConflict
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SquadSkillReceipt{}, classify(err)
	}
	receiptSource := sha256.Sum256([]byte(actor.LaunchID + "\x00" + event.ClientEventID))
	receiptID := hex.EncodeToString(receiptSource[:])
	at := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO squad_skill_events
		(receipt_id,launch_id,snapshot_id,conversation_id,definition_id,scope_id,skill_id,skill_version,
		client_event_id,status,failure_code,payload_digest,at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, receiptID, actor.LaunchID, actor.SnapshotID,
		actor.ConversationID, snapshot.DefinitionID, snapshot.ScopeID, event.SkillID, event.SkillVersion,
		event.ClientEventID, event.Status, event.FailureCode, digest, at)
	if err != nil {
		return SquadSkillReceipt{}, classify(err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return SquadSkillReceipt{}, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return SquadSkillReceipt{}, classify(err)
	}
	return SquadSkillReceipt{Seq: seq, ReceiptID: receiptID, SnapshotID: actor.SnapshotID,
		ConversationID: actor.ConversationID, DefinitionID: snapshot.DefinitionID, ScopeID: snapshot.ScopeID,
		SkillID: event.SkillID, SkillVersion: event.SkillVersion, Status: event.Status,
		FailureCode: event.FailureCode, At: at}, nil
}

func (s *Store) SquadEventHead(ctx context.Context) (int64, error) {
	var head int64
	err := s.rd.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM squad_skill_events`).Scan(&head)
	return head, classify(err)
}

// SquadEventsAfter reads receipts after an exclusive sequence cursor.
func (s *Store) SquadEventsAfter(ctx context.Context, after int64, pageRows int) ([]SquadSkillReceipt, bool, error) {
	return s.SquadEventsAfterFor(ctx, nil, after, pageRows)
}

// SquadEventsAfterFor reads receipts after an exclusive sequence cursor,
// only of the named conversations when any are named. Each conversation is
// read along the (conversation_id, seq) index, so the cost follows those
// conversations' receipts, not the length of the whole log.
func (s *Store) SquadEventsAfterFor(ctx context.Context, conversations []string, after int64, pageRows int) ([]SquadSkillReceipt, bool, error) {
	if after < 0 || pageRows < 1 || pageRows > MaxSquadEventPageRows || len(conversations) > MaxSquadEventConversations {
		return nil, false, ErrSquadEventInvalid
	}
	query := `SELECT seq,receipt_id,snapshot_id,conversation_id,definition_id,scope_id,
		skill_id,skill_version,status,failure_code,at FROM squad_skill_events
		WHERE seq>? ORDER BY seq LIMIT ?`
	args := []any{after, pageRows + 1}
	if len(conversations) > 0 {
		// One indexed range per conversation, merged by sequence: an
		// IN list over the whole table would let the planner scan by seq.
		parts := make([]string, 0, len(conversations))
		args = args[:0]
		named := make(map[string]bool, len(conversations))
		for _, conversation := range conversations {
			if conversation == "" || len(conversation) > MaxSquadEventIDBytes {
				return nil, false, ErrSquadEventInvalid
			}
			if named[conversation] {
				continue
			}
			named[conversation] = true
			parts = append(parts, `SELECT * FROM (SELECT seq,receipt_id,snapshot_id,conversation_id,definition_id,scope_id,
		skill_id,skill_version,status,failure_code,at FROM squad_skill_events INDEXED BY squad_skill_events_conversation_seq
		WHERE conversation_id=? AND seq>? ORDER BY seq LIMIT ?)`)
			args = append(args, conversation, after, pageRows+1)
		}
		query = strings.Join(parts, " UNION ALL ") + ` ORDER BY seq LIMIT ?`
		args = append(args, pageRows+1)
	}
	rows, err := s.rd.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, classify(err)
	}
	defer rows.Close()
	var events []SquadSkillReceipt
	for rows.Next() {
		var event SquadSkillReceipt
		if err := rows.Scan(&event.Seq, &event.ReceiptID, &event.SnapshotID, &event.ConversationID,
			&event.DefinitionID, &event.ScopeID, &event.SkillID, &event.SkillVersion, &event.Status, &event.FailureCode, &event.At); err != nil {
			return nil, false, classify(err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, classify(err)
	}
	hasMore := len(events) > pageRows
	if hasMore {
		events = events[:pageRows]
	}
	return events, hasMore, nil
}
