package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/workflow"
)

// MaxWorkflowSnapshotBytes bounds one complete immutable launch snapshot.
const MaxWorkflowSnapshotBytes = 1 << 20

var (
	ErrWorkflowSnapshotTooLarge  = errors.New("workflow_snapshot_too_large")
	ErrWorkflowLaunchUnknown     = errors.New("workflow_launch_unknown")
	ErrWorkflowLaunchConflict    = errors.New("workflow_launch_conflict")
	ErrWorkflowConversationTaken = errors.New("workflow_conversation_taken")
)

const workflowSchema = `
CREATE TABLE IF NOT EXISTS workflow_snapshots (
 id TEXT PRIMARY KEY, document BLOB NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS workflow_launches (
 id TEXT PRIMARY KEY,
 snapshot_id TEXT NOT NULL REFERENCES workflow_snapshots(id),
 terminal_id TEXT NOT NULL DEFAULT '',
 conversation_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','bound','failed')),
 created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS workflow_conversations (
 conversation_id TEXT PRIMARY KEY,
 snapshot_id TEXT NOT NULL REFERENCES workflow_snapshots(id)
);
`

// WorkflowLaunch is a durable intent. A failed intent cannot later bind, and a
// bound intent cannot change its terminal, conversation, or snapshot.
type WorkflowLaunch struct {
	ID             string            `json:"id"`
	SnapshotID     string            `json:"snapshot_id"`
	Snapshot       workflow.Snapshot `json:"snapshot"`
	TerminalID     string            `json:"terminal_id"`
	ConversationID string            `json:"conversation_id"`
	State          string            `json:"state"`
	CreatedAt      int64             `json:"created_at"`
}

// ensureWorkflowSchema permits older stores to adopt workflow storage without
// an application-wide migration. Existing records are never rewritten.
func (s *Store) ensureWorkflowSchema(ctx context.Context) error {
	// The last-created table is the completion marker. Normal reads stay on
	// the read pool instead of queuing schema statements behind all writes.
	var initialized bool
	if err := s.rd.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='workflow_conversations')`).Scan(&initialized); err != nil {
		return classify(err)
	}
	if initialized {
		return nil
	}
	_, err := s.db.ExecContext(ctx, workflowSchema)
	return classify(err)
}

// PrepareWorkflowLaunch atomically stores an immutable snapshot and a unique
// pending intent before a process is launched. Snapshot identity is its digest.
func (s *Store) PrepareWorkflowLaunch(ctx context.Context, snapshot workflow.Snapshot) (WorkflowLaunch, error) {
	if err := snapshot.Validate(); err != nil {
		return WorkflowLaunch{}, err
	}
	snapshot, err := workflow.Seal(snapshot)
	if err != nil {
		return WorkflowLaunch{}, err
	}
	document, err := json.Marshal(snapshot)
	if err != nil {
		return WorkflowLaunch{}, err
	}
	if len(document) > MaxWorkflowSnapshotBytes {
		return WorkflowLaunch{}, ErrWorkflowSnapshotTooLarge
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return WorkflowLaunch{}, err
	}
	id, err := randomSquadValue(16)
	if err != nil {
		return WorkflowLaunch{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowLaunch{}, classify(err)
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_snapshots(id,document,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, snapshot.Digest, document, now); err != nil {
		return WorkflowLaunch{}, classify(err)
	}
	var existing []byte
	if err := tx.QueryRowContext(ctx, `SELECT substr(document,1,?) FROM workflow_snapshots WHERE id=?`, MaxWorkflowSnapshotBytes+1, snapshot.Digest).Scan(&existing); err != nil {
		return WorkflowLaunch{}, classify(err)
	}
	if !bytes.Equal(document, existing) {
		return WorkflowLaunch{}, workflow.ErrDigestMismatch
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_launches(id,snapshot_id,state,created_at) VALUES(?,?,'pending',?)`, id, snapshot.Digest, now); err != nil {
		return WorkflowLaunch{}, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return WorkflowLaunch{}, classify(err)
	}
	// Decode the persisted document so returned slices never alias caller data.
	var independent workflow.Snapshot
	if err := json.Unmarshal(document, &independent); err != nil {
		return WorkflowLaunch{}, err
	}
	return WorkflowLaunch{ID: id, SnapshotID: snapshot.Digest, Snapshot: independent, State: "pending", CreatedAt: now}, nil
}

// RecordWorkflowTerminal records the first terminal and accepts identical retries.
func (s *Store) RecordWorkflowTerminal(ctx context.Context, launchID, terminalID string) error {
	if launchID == "" || terminalID == "" {
		return ErrWorkflowLaunchConflict
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var existing, state string
	err = tx.QueryRowContext(ctx, `SELECT terminal_id,state FROM workflow_launches WHERE id=?`, launchID).Scan(&existing, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkflowLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if state == "failed" || (existing != "" && existing != terminalID) {
		return ErrWorkflowLaunchConflict
	}
	if existing == terminalID {
		return nil
	}
	if state != "pending" {
		return ErrWorkflowLaunchConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_launches SET terminal_id=? WHERE id=?`, terminalID, launchID); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// BindWorkflowConversation requires the launch's recorded terminal. A provider
// conversation retains its original snapshot; resumes may bind a new terminal
// only when their snapshot matches that immutable original.
func (s *Store) BindWorkflowConversation(ctx context.Context, launchID, terminalID, conversationID string) error {
	if launchID == "" || terminalID == "" || conversationID == "" {
		return ErrWorkflowLaunchConflict
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var terminal, conversation, state, snapshotID string
	err = tx.QueryRowContext(ctx, `SELECT terminal_id,conversation_id,state,snapshot_id FROM workflow_launches WHERE id=?`, launchID).Scan(&terminal, &conversation, &state, &snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkflowLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if terminal != terminalID || state == "failed" {
		return ErrWorkflowLaunchConflict
	}
	if state == "bound" && conversation == conversationID {
		return nil
	}
	if state != "pending" || conversation != "" {
		return ErrWorkflowLaunchConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_conversations(conversation_id,snapshot_id) VALUES(?,?) ON CONFLICT(conversation_id) DO NOTHING`, conversationID, snapshotID); err != nil {
		return classify(err)
	}
	var originalSnapshot string
	if err := tx.QueryRowContext(ctx, `SELECT snapshot_id FROM workflow_conversations WHERE conversation_id=?`, conversationID).Scan(&originalSnapshot); err != nil {
		return classify(err)
	}
	if originalSnapshot != snapshotID {
		return ErrWorkflowConversationTaken
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_launches SET conversation_id=?,state='bound' WHERE id=?`, conversationID, launchID); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// FailWorkflowLaunch preserves the intent for diagnosis; it cannot invalidate
// an already-bound conversation. Repeating a failure is idempotent.
func (s *Store) FailWorkflowLaunch(ctx context.Context, launchID string) error {
	if launchID == "" {
		return ErrWorkflowLaunchUnknown
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM workflow_launches WHERE id=?`, launchID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkflowLaunchUnknown
	}
	if err != nil {
		return classify(err)
	}
	if state == "bound" {
		return ErrWorkflowLaunchConflict
	}
	if state == "failed" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_launches SET state='failed' WHERE id=?`, launchID); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// WorkflowSnapshotForConversation reads the original binding, including after a
// restart or settings edit. An older conversation has no inferred snapshot.
func (s *Store) WorkflowSnapshotForConversation(ctx context.Context, conversationID string) (workflow.Snapshot, bool, error) {
	if conversationID == "" {
		return workflow.Snapshot{}, false, nil
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return workflow.Snapshot{}, false, err
	}
	var document []byte
	var snapshotID string
	err := s.rd.QueryRowContext(ctx, `SELECT s.id,substr(s.document,1,?) FROM workflow_snapshots s JOIN workflow_conversations c ON s.id=c.snapshot_id WHERE c.conversation_id=?`, MaxWorkflowSnapshotBytes+1, conversationID).Scan(&snapshotID, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.Snapshot{}, false, nil
	}
	if err != nil {
		return workflow.Snapshot{}, false, classify(err)
	}
	snapshot, err := decodeWorkflowSnapshot(document, snapshotID)
	return snapshot, err == nil, err
}

// WorkflowLaunchByID returns one bounded snapshot, never a historical scan.
func (s *Store) WorkflowLaunchByID(ctx context.Context, id string) (WorkflowLaunch, bool, error) {
	if id == "" {
		return WorkflowLaunch{}, false, nil
	}
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return WorkflowLaunch{}, false, err
	}
	var l WorkflowLaunch
	var document []byte
	err := s.rd.QueryRowContext(ctx, `SELECT l.id,l.snapshot_id,l.terminal_id,l.conversation_id,l.state,l.created_at,substr(s.document,1,?) FROM workflow_launches l JOIN workflow_snapshots s ON l.snapshot_id=s.id WHERE l.id=?`, MaxWorkflowSnapshotBytes+1, id).Scan(&l.ID, &l.SnapshotID, &l.TerminalID, &l.ConversationID, &l.State, &l.CreatedAt, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkflowLaunch{}, false, nil
	}
	if err != nil {
		return WorkflowLaunch{}, false, classify(err)
	}
	l.Snapshot, err = decodeWorkflowSnapshot(document, l.SnapshotID)
	return l, err == nil, err
}

func decodeWorkflowSnapshot(document []byte, snapshotID string) (workflow.Snapshot, error) {
	if len(document) > MaxWorkflowSnapshotBytes {
		return workflow.Snapshot{}, ErrWorkflowSnapshotTooLarge
	}
	if !json.Valid(document) {
		return workflow.Snapshot{}, workflow.ErrInvalidSnapshot
	}
	var snapshot workflow.Snapshot
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return workflow.Snapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return workflow.Snapshot{}, err
	}
	if snapshot.Digest != snapshotID {
		return workflow.Snapshot{}, workflow.ErrDigestMismatch
	}
	return snapshot, nil
}

// WorkflowCounts is actual retained storage usage, not a launch estimate.
type WorkflowCounts struct {
	Launches             int64
	LargestSnapshotBytes int64
}

// WorkflowUsage reads both diagnostics from one consistent SQLite statement.
func (s *Store) WorkflowUsage(ctx context.Context) (WorkflowCounts, error) {
	if err := s.ensureWorkflowSchema(ctx); err != nil {
		return WorkflowCounts{}, err
	}
	var counts WorkflowCounts
	err := s.rd.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM workflow_launches), (SELECT coalesce(max(length(CAST(document AS BLOB))),0) FROM workflow_snapshots)`).Scan(&counts.Launches, &counts.LargestSnapshotBytes)
	return counts, classify(err)
}
