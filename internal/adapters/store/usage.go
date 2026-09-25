package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// The token ledger's rows (docs/token-ledger.md "Storage and reading"): one
// per transcript — a session's own, or one of its subagents' — holding how far
// it was read and what its tokens were spent on.
//
// **Totals only.** The state is transcript.LedgerState as JSON, which is
// sizes by category, offsets, digests and message ids; the totals are tokens
// and dollars by category. No prompt, tool input or transcript text is ever
// written here. The store keeps both as opaque JSON: what they mean is the
// transcript package's, and this one only keeps them.
//
// A row is named by its assistant and conversation, not its path: a
// transcript whose project directory was renamed is the same conversation,
// and its state (which names the file by a digest of its start) goes on from
// its offset rather than counting the conversation twice.
const usageSchema = `
CREATE TABLE IF NOT EXISTS usage_transcripts (
  assistant       TEXT    NOT NULL,
  conversation    TEXT    NOT NULL,
  path            TEXT    NOT NULL,
  parent          TEXT    NOT NULL DEFAULT '',
  task_id         TEXT    NOT NULL DEFAULT '',
  root_assignment TEXT    NOT NULL DEFAULT '',
  opening_read    INTEGER NOT NULL DEFAULT 0,
  state           TEXT    NOT NULL DEFAULT '{}',
  spent           TEXT    NOT NULL DEFAULT '{}',
  measured        TEXT    NOT NULL DEFAULT '{}',
  size            INTEGER NOT NULL DEFAULT 0,
  modified_at     INTEGER NOT NULL DEFAULT 0,
  more            INTEGER NOT NULL DEFAULT 0,
  read_at         INTEGER NOT NULL DEFAULT 0,
  reason          TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (assistant, conversation)
);
CREATE INDEX IF NOT EXISTS usage_transcripts_path ON usage_transcripts(path);
CREATE INDEX IF NOT EXISTS usage_transcripts_conversation ON usage_transcripts(conversation);
CREATE INDEX IF NOT EXISTS usage_transcripts_parent ON usage_transcripts(parent) WHERE parent <> '';
CREATE INDEX IF NOT EXISTS usage_transcripts_task ON usage_transcripts(task_id) WHERE task_id <> '';
CREATE INDEX IF NOT EXISTS usage_transcripts_root_assignment ON usage_transcripts(root_assignment)
  WHERE root_assignment <> '';
CREATE INDEX IF NOT EXISTS usage_transcripts_read ON usage_transcripts(read_at);
`

func openUsage(db *sql.DB) error {
	_, err := db.Exec(usageSchema)
	return err
}

// Why a transcript's row is not a current reading.
const (
	// UsageTranscriptMissing is a transcript read before and gone since: its
	// totals are the last reading's, and there will be no newer one.
	UsageTranscriptMissing = "transcript_missing"
	// UsageTranscriptUnreadable is a transcript that is there and could not be
	// read; its totals, if any, are from before.
	UsageTranscriptUnreadable = "transcript_unreadable"
	// UsageNotYetRead is a transcript, or a session, the ledger has no reading
	// of yet.
	UsageNotYetRead = "not_yet_read"
)

// UsageRow is one transcript's row.
type UsageRow struct {
	Assistant    string
	Conversation string
	Path         string
	// Parent is the session a subagent transcript belongs to; empty for a
	// session's own.
	Parent string
	// TaskID and RootAssignment are what the transcript's first message
	// names: a dispatched child's task, or the Root Assignment that opened it.
	TaskID         string
	RootAssignment string
	// OpeningRead says the first message has been looked at, so it is not
	// read again on every pass.
	OpeningRead bool
	// State is transcript.LedgerState, Spent its per-category totals and
	// Measured its count by part, all as JSON.
	State    json.RawMessage
	Spent    json.RawMessage
	Measured json.RawMessage
	// Size and ModifiedAt are the file as last read; More says that read
	// stopped before its end.
	Size       int64
	ModifiedAt time.Time
	More       bool
	// ReadAt is the last pass that fed this transcript; zero when none has.
	ReadAt time.Time
	// Reason is one of the Usage… constants, or empty.
	Reason string
}

const usageColumns = `assistant, conversation, path, parent, task_id, root_assignment, opening_read,
  state, spent, measured, size, modified_at, more, read_at, reason`

func scanUsage(row interface{ Scan(...any) error }) (UsageRow, error) {
	var r UsageRow
	var opening, more int64
	var state, spent, measured string
	var modified, read int64
	if err := row.Scan(&r.Assistant, &r.Conversation, &r.Path, &r.Parent, &r.TaskID, &r.RootAssignment,
		&opening, &state, &spent, &measured, &r.Size, &modified, &more, &read, &r.Reason); err != nil {
		return UsageRow{}, err
	}
	r.OpeningRead, r.More = opening != 0, more != 0
	r.State, r.Spent, r.Measured = json.RawMessage(state), json.RawMessage(spent), json.RawMessage(measured)
	if modified != 0 {
		r.ModifiedAt = time.Unix(0, modified)
	}
	if read != 0 {
		r.ReadAt = time.Unix(0, read)
	}
	return r, nil
}

func jsonOr(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func nanosOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// SaveUsageRow writes one transcript's row, whole.
func (s *Store) SaveUsageRow(ctx context.Context, r UsageRow) error {
	if r.Assistant == "" || r.Conversation == "" {
		return errors.New("a usage row needs its assistant and conversation")
	}
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO usage_transcripts (`+usageColumns+`)
      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
      ON CONFLICT(assistant, conversation) DO UPDATE SET
        path=excluded.path, parent=excluded.parent, task_id=excluded.task_id,
        root_assignment=excluded.root_assignment, opening_read=excluded.opening_read,
        state=excluded.state, spent=excluded.spent, measured=excluded.measured,
        size=excluded.size, modified_at=excluded.modified_at, more=excluded.more,
        read_at=excluded.read_at, reason=excluded.reason`,
			r.Assistant, r.Conversation, r.Path, r.Parent, r.TaskID, r.RootAssignment, boolInt(r.OpeningRead),
			jsonOr(r.State), jsonOr(r.Spent), jsonOr(r.Measured), r.Size, nanosOrZero(r.ModifiedAt),
			boolInt(r.More), nanosOrZero(r.ReadAt), r.Reason)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// UsageRow is one transcript's row, and whether there is one.
func (s *Store) UsageRow(ctx context.Context, assistant, conversation string) (UsageRow, bool, error) {
	rows, err := s.usageRows(ctx, `WHERE assistant=? AND conversation=?`, assistant, conversation)
	if err != nil || len(rows) == 0 {
		return UsageRow{}, false, err
	}
	return rows[0], true, nil
}

// UsageRowsForConversations is every row naming one of these conversations,
// under any assistant.
func (s *Store) UsageRowsForConversations(ctx context.Context, conversations []string) ([]UsageRow, error) {
	return s.usageRowsIn(ctx, "conversation", conversations)
}

// UsageRowsWithParents is every subagent row belonging to one of these
// sessions.
func (s *Store) UsageRowsWithParents(ctx context.Context, parents []string) ([]UsageRow, error) {
	return s.usageRowsIn(ctx, "parent", parents)
}

// UsageRowsForTask is every row whose first message names this child task.
func (s *Store) UsageRowsForTask(ctx context.Context, taskID string) ([]UsageRow, error) {
	return s.usageRowsIn(ctx, "task_id", []string{taskID})
}

// UsageRowsForRootAssignments is every row whose first message names one of
// these Root Assignments.
func (s *Store) UsageRowsForRootAssignments(ctx context.Context, ids []string) ([]UsageRow, error) {
	return s.usageRowsIn(ctx, "root_assignment", ids)
}

// UsageRowsReadSince is every row a pass fed at or after since: the rows
// whose file the next pass should still find.
func (s *Store) UsageRowsReadSince(ctx context.Context, since time.Time) ([]UsageRow, error) {
	return s.usageRows(ctx, `WHERE read_at >= ? AND reason <> ?`, since.UnixNano(), UsageTranscriptMissing)
}

func (s *Store) usageRowsIn(ctx context.Context, column string, values []string) ([]UsageRow, error) {
	var keep []any
	for _, v := range values {
		if v != "" {
			keep = append(keep, v)
		}
	}
	if len(keep) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",")
	return s.usageRows(ctx, `WHERE `+column+` IN (`+marks+`)`, keep...)
}

func (s *Store) usageRows(ctx context.Context, where string, args ...any) ([]UsageRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+usageColumns+` FROM usage_transcripts `+where+
		` ORDER BY assistant, conversation`, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		r, err := scanUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageTaskRef is a child task a session dispatched, and when.
type UsageTaskRef struct {
	ID        string
	CreatedAt time.Time
}

// BrokerTasksDispatchedBy is every broker task whose record names session as
// its root (`root.session_id`) and that was created in [from, to]. A zero to
// is no end.
func (s *Store) BrokerTasksDispatchedBy(ctx context.Context, session string, from, to time.Time) ([]UsageTaskRef, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	if session == "" {
		return nil, nil
	}
	query := `SELECT id, created_at FROM broker_tasks
    WHERE json_valid(record) AND json_extract(record, '$.root.session_id') = ? AND created_at >= ?`
	args := []any{session, from.Unix()}
	if !to.IsZero() {
		query += ` AND created_at <= ?`
		args = append(args, to.Unix())
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []UsageTaskRef
	for rows.Next() {
		var ref UsageTaskRef
		var at int64
		if err := rows.Scan(&ref.ID, &at); err != nil {
			return nil, err
		}
		ref.CreatedAt = time.Unix(at, 0)
		out = append(out, ref)
	}
	return out, rows.Err()
}
