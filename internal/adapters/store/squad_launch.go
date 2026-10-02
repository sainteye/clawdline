package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MaxSquadSnapshotBytes bounds the complete, immutable launch document. The
// component limits for persona, handbook, and skill text are checked by their
// owners before this final bound is reached.
const MaxSquadSnapshotBytes = 1 << 20

// MaxSquadRecoveryRows is one inventory pass's pending-intent scan budget.
const MaxSquadRecoveryRows = 256

var (
	ErrSquadSnapshotTooLarge  = errors.New("squad_snapshot_too_large")
	ErrSquadLaunchUnknown     = errors.New("squad_launch_unknown")
	ErrSquadLaunchConflict    = errors.New("squad_launch_conflict")
	ErrSquadLaunchCapacity    = errors.New("squad_launch_capacity")
	ErrSquadConversationTaken = errors.New("squad_conversation_taken")
)

const squadLaunchSchema = `
CREATE TABLE IF NOT EXISTS squad_snapshots (
  id TEXT PRIMARY KEY,
  document BLOB NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS squad_launches (
  id TEXT PRIMARY KEY,
  snapshot_id TEXT NOT NULL REFERENCES squad_snapshots(id),
  actor_hash TEXT NOT NULL UNIQUE,
  terminal_id TEXT NOT NULL DEFAULT '',
  conversation_id TEXT,
  resume_of TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('pending','bound','failed')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS squad_launches_snapshot ON squad_launches(snapshot_id);
CREATE INDEX IF NOT EXISTS squad_launches_conversation ON squad_launches(conversation_id);
`

func openSquadLaunch(db *sql.DB) error {
	if _, err := db.Exec(squadLaunchSchema); err != nil {
		return err
	}
	return openSquadEvents(db)
}

// SquadLaunch is the pending intent persisted before a terminal opens.
// ActorCapability is returned once for a private launch file; only its digest
// is kept in SQLite. It must not be logged or returned to a browser.
type SquadLaunch struct {
	ID              string
	SnapshotID      string
	ActorCapability string
}

type SquadActor struct {
	LaunchID       string
	SnapshotID     string
	TerminalID     string
	ConversationID string
	DefinitionID   string
	ScopeID        string
}

// SquadSessionBinding is the durable authority for a live terminal's role and
// scope. A conversation ID alone is insufficient when the same conversation
// has been resumed in another terminal.
type SquadSessionBinding struct {
	TerminalID     string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	SnapshotID     string `json:"snapshot_id"`
	DefinitionID   string `json:"definition_id"`
	ScopeID        string `json:"scope_id"`
}

func (s *Store) SquadBindingForSession(ctx context.Context, terminalID, conversationID string) (SquadSessionBinding, bool, error) {
	if terminalID == "" || conversationID == "" {
		return SquadSessionBinding{}, false, nil
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT l.terminal_id,l.conversation_id,l.snapshot_id,s.document
		FROM squad_launches l JOIN squad_snapshots s ON s.id=l.snapshot_id
		WHERE l.terminal_id=? AND l.conversation_id=? AND l.state='bound' LIMIT 2`, terminalID, conversationID)
	if err != nil {
		return SquadSessionBinding{}, false, classify(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return SquadSessionBinding{}, false, classify(rows.Err())
	}
	var binding SquadSessionBinding
	var document []byte
	if err := rows.Scan(&binding.TerminalID, &binding.ConversationID, &binding.SnapshotID, &document); err != nil {
		return SquadSessionBinding{}, false, classify(err)
	}
	if rows.Next() {
		return SquadSessionBinding{}, false, ErrSquadLaunchConflict
	}
	if err := rows.Err(); err != nil {
		return SquadSessionBinding{}, false, classify(err)
	}
	var identity struct {
		DefinitionID string `json:"definition_id"`
		ScopeID      string `json:"scope_id"`
	}
	if err := json.Unmarshal(document, &identity); err != nil || identity.DefinitionID == "" || identity.ScopeID == "" {
		return SquadSessionBinding{}, false, errors.New("invalid_squad_snapshot_identity")
	}
	binding.DefinitionID, binding.ScopeID = identity.DefinitionID, identity.ScopeID
	return binding, true, nil
}

func randomSquadValue(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func newSquadIdentity() (string, string, string, error) {
	launchID, err := randomSquadValue(16)
	if err != nil {
		return "", "", "", err
	}
	capability, err := randomSquadValue(32)
	if err != nil {
		return "", "", "", err
	}
	sum := sha256.Sum256([]byte(capability))
	return launchID, capability, hex.EncodeToString(sum[:]), nil
}

// PrepareSquadLaunch stores the exact launch document and a pending intent in
// one transaction. A matching document reuses its content-addressed snapshot
// while each launch receives a different identity and capability.
func (s *Store) PrepareSquadLaunch(ctx context.Context, document json.RawMessage) (SquadLaunch, error) {
	if !json.Valid(document) || len(document) == 0 {
		return SquadLaunch{}, errors.New("invalid_squad_snapshot")
	}
	if len(document) > MaxSquadSnapshotBytes {
		return SquadLaunch{}, ErrSquadSnapshotTooLarge
	}
	sum := sha256.Sum256(document)
	snapshotID := hex.EncodeToString(sum[:])
	launchID, capability, capHash, err := newSquadIdentity()
	if err != nil {
		return SquadLaunch{}, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SquadLaunch{}, classify(err)
	}
	defer tx.Rollback()
	if err := checkSquadLaunchCapacity(ctx, tx); err != nil {
		return SquadLaunch{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO squad_snapshots(id,document,created_at)
		VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, snapshotID, []byte(document), now); err != nil {
		return SquadLaunch{}, classify(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO squad_launches
		(id,snapshot_id,actor_hash,state,created_at,updated_at) VALUES(?,?,?,'pending',?,?)`,
		launchID, snapshotID, capHash, now, now); err != nil {
		return SquadLaunch{}, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return SquadLaunch{}, classify(err)
	}
	return SquadLaunch{ID: launchID, SnapshotID: snapshotID, ActorCapability: capability}, nil
}

// PrepareSquadResume creates a new launch capability for an existing provider
// conversation while retaining its original snapshot. It cannot substitute
// current catalog or Project settings for the historical document.
func (s *Store) PrepareSquadResume(ctx context.Context, conversationID string) (SquadLaunch, error) {
	if conversationID == "" {
		return SquadLaunch{}, ErrSquadLaunchUnknown
	}
	launchID, capability, capHash, err := newSquadIdentity()
	if err != nil {
		return SquadLaunch{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SquadLaunch{}, classify(err)
	}
	defer tx.Rollback()
	if err := checkSquadLaunchCapacity(ctx, tx); err != nil {
		return SquadLaunch{}, err
	}
	var snapshotID string
	err = tx.QueryRowContext(ctx, `SELECT snapshot_id FROM squad_launches
		WHERE conversation_id=? AND state='bound' ORDER BY created_at,id LIMIT 1`, conversationID).Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return SquadLaunch{}, ErrSquadLaunchUnknown
	}
	if err != nil {
		return SquadLaunch{}, classify(err)
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO squad_launches
		(id,snapshot_id,actor_hash,resume_of,state,created_at,updated_at)
		VALUES(?,?,?,?,'pending',?,?)`, launchID, snapshotID, capHash, conversationID, now, now); err != nil {
		return SquadLaunch{}, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return SquadLaunch{}, classify(err)
	}
	return SquadLaunch{ID: launchID, SnapshotID: snapshotID, ActorCapability: capability}, nil
}

func checkSquadLaunchCapacity(ctx context.Context, tx *sql.Tx) error {
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM squad_launches WHERE state='pending'`).Scan(&pending); err != nil {
		return classify(err)
	}
	if pending >= MaxSquadRecoveryRows {
		return ErrSquadLaunchCapacity
	}
	return nil
}

// RecordSquadTerminal makes the first successful terminal creation durable.
// A repeat with the same ID is idempotent; a different ID never replaces it.
func (s *Store) RecordSquadTerminal(ctx context.Context, launchID, terminalID string) error {
	if launchID == "" || terminalID == "" {
		return ErrSquadLaunchConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE squad_launches SET terminal_id=?,updated_at=?
		WHERE id=? AND state='pending' AND terminal_id=''`, terminalID, time.Now().Unix(), launchID)
	if err != nil {
		return classify(err)
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	var existing, state string
	err = s.rd.QueryRowContext(ctx, `SELECT terminal_id,state FROM squad_launches WHERE id=?`, launchID).Scan(&existing, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSquadLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if existing == terminalID && state != "failed" {
		return nil
	}
	return ErrSquadLaunchConflict
}

// BindSquadConversation attaches a provider conversation once to a terminal
// that was recorded. A duplicate conversation across launches is refused.
func (s *Store) BindSquadConversation(ctx context.Context, launchID, conversationID string) error {
	if launchID == "" || conversationID == "" {
		return ErrSquadLaunchConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var snapshotID, terminalID, resumeOf, state string
	var existing sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT snapshot_id,terminal_id,resume_of,state,conversation_id
		FROM squad_launches WHERE id=?`, launchID).Scan(&snapshotID, &terminalID, &resumeOf, &state, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSquadLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if existing.Valid && existing.String == conversationID {
		return nil
	}
	if existing.Valid || state != "pending" || terminalID == "" || (resumeOf != "" && resumeOf != conversationID) {
		return ErrSquadLaunchConflict
	}
	var originalSnapshot string
	err = tx.QueryRowContext(ctx, `SELECT snapshot_id FROM squad_launches
		WHERE conversation_id=? AND state='bound' ORDER BY created_at,id LIMIT 1`, conversationID).Scan(&originalSnapshot)
	if err == nil {
		if resumeOf == "" {
			return ErrSquadConversationTaken
		}
		if originalSnapshot != snapshotID {
			return ErrSquadLaunchConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if resumeOf != "" {
			return ErrSquadLaunchConflict
		}
	} else {
		return classify(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE squad_launches
		SET conversation_id=?,state='bound',updated_at=? WHERE id=?`, conversationID, time.Now().Unix(), launchID); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// RebindSquadConversation follows a new provider conversation observed in the
// same launch process and terminal. Resumed launches keep their original ID.
func (s *Store) RebindSquadConversation(ctx context.Context, launchID, terminalID, conversationID string) error {
	if launchID == "" || terminalID == "" || conversationID == "" {
		return ErrSquadLaunchConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var heldTerminal, heldConversation, resumeOf, state string
	err = tx.QueryRowContext(ctx, `SELECT terminal_id,conversation_id,resume_of,state FROM squad_launches WHERE id=?`, launchID).
		Scan(&heldTerminal, &heldConversation, &resumeOf, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSquadLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if state != "bound" || heldTerminal != terminalID {
		return ErrSquadLaunchConflict
	}
	if heldConversation == conversationID || resumeOf != "" {
		return nil
	}
	var taken int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM squad_launches WHERE conversation_id=? AND state='bound' AND id<>? LIMIT 1`, conversationID, launchID).Scan(&taken)
	if err == nil {
		return ErrSquadConversationTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return classify(err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE squad_launches SET conversation_id=?,updated_at=? WHERE id=? AND terminal_id=? AND conversation_id=? AND state='bound'`,
		conversationID, time.Now().Unix(), launchID, terminalID, heldConversation)
	if err != nil {
		return classify(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ErrSquadLaunchConflict
	}
	return classify(tx.Commit())
}

// SquadSnapshotForConversation returns the original document, never current
// settings. Absence tells the caller to use explicit legacy_unsnapshotted mode.
func (s *Store) SquadSnapshotForConversation(ctx context.Context, conversationID string) (json.RawMessage, string, bool, error) {
	var document []byte
	var snapshotID string
	err := s.rd.QueryRowContext(ctx, `SELECT s.document,s.id FROM squad_launches l
		JOIN squad_snapshots s ON s.id=l.snapshot_id
		WHERE l.conversation_id=? AND l.state='bound'
		ORDER BY l.created_at,l.id LIMIT 1`, conversationID).Scan(&document, &snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, classify(err)
	}
	sum := sha256.Sum256(document)
	if hex.EncodeToString(sum[:]) != snapshotID {
		return nil, "", false, fmt.Errorf("squad snapshot %s digest mismatch", snapshotID)
	}
	return json.RawMessage(document), snapshotID, true, nil
}

func (s *Store) SquadLargestSnapshotBytes(ctx context.Context) (int64, error) {
	var bytes int64
	err := s.rd.QueryRowContext(ctx, `SELECT COALESCE(MAX(LENGTH(document)),0) FROM squad_snapshots`).Scan(&bytes)
	return bytes, classify(err)
}

// AuthenticateSquadActor accepts only a bound launch's capability. The
// machine token and a caller-supplied conversation ID are never sufficient.
func (s *Store) AuthenticateSquadActor(ctx context.Context, capability string) (SquadActor, bool, error) {
	if capability == "" {
		return SquadActor{}, false, nil
	}
	sum := sha256.Sum256([]byte(capability))
	var actor SquadActor
	var document []byte
	err := s.rd.QueryRowContext(ctx, `SELECT l.id,l.snapshot_id,l.terminal_id,l.conversation_id,s.document
		FROM squad_launches l JOIN squad_snapshots s ON s.id=l.snapshot_id
		WHERE l.actor_hash=? AND l.state='bound'`, hex.EncodeToString(sum[:])).Scan(
		&actor.LaunchID, &actor.SnapshotID, &actor.TerminalID, &actor.ConversationID, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return SquadActor{}, false, nil
	}
	if err != nil {
		return SquadActor{}, false, classify(err)
	}
	var identity struct {
		DefinitionID string `json:"definition_id"`
		ScopeID      string `json:"scope_id"`
	}
	if err := json.Unmarshal(document, &identity); err != nil || identity.DefinitionID == "" || identity.ScopeID == "" {
		return SquadActor{}, false, ErrSquadActorUnauthorized
	}
	actor.DefinitionID, actor.ScopeID = identity.DefinitionID, identity.ScopeID
	return actor, true, nil
}

// SquadHasLaunchForTerminal distinguishes a legacy Session from a squad
// launch whose actor binding is still pending. Pending launches may not fall
// back to legacy dispatch simply by omitting their capability.
func (s *Store) SquadHasLaunchForTerminal(ctx context.Context, terminalID string) (bool, error) {
	if terminalID == "" {
		return false, nil
	}
	var exists int
	err := s.rd.QueryRowContext(ctx, `SELECT 1 FROM squad_launches
		WHERE terminal_id=? AND state IN ('pending','bound') LIMIT 1`, terminalID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return exists == 1, classify(err)
}

// FailSquadLaunch records an opening that never produced a terminal. An
// intent with a terminal remains pending for recovery instead of being lost.
func (s *Store) FailSquadLaunch(ctx context.Context, launchID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE squad_launches SET state='failed',updated_at=?
		WHERE id=? AND state='pending' AND terminal_id=''`, time.Now().Unix(), launchID)
	if err != nil {
		return classify(err)
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	return ErrSquadLaunchConflict
}

type PendingSquadLaunch struct {
	ID         string
	TerminalID string
}

func (s *Store) PendingSquadLaunches(ctx context.Context) ([]PendingSquadLaunch, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT id,terminal_id FROM squad_launches
		WHERE state='pending' ORDER BY created_at,id LIMIT ?`, MaxSquadRecoveryRows)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var pending []PendingSquadLaunch
	for rows.Next() {
		var item PendingSquadLaunch
		if err := rows.Scan(&item.ID, &item.TerminalID); err != nil {
			return nil, classify(err)
		}
		pending = append(pending, item)
	}
	return pending, classify(rows.Err())
}

func (s *Store) SquadPendingLaunchCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM squad_launches WHERE state='pending'`).Scan(&count)
	return count, classify(err)
}
