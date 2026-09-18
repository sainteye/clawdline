package store

import (
	"context"
	"database/sql"
)

// A task's long prose, in a table of its own (D25, D26).
//
// A task record is read on every beat and every list; its instructions and
// its summary are read when somebody opens that one task. Kept inside the
// record, every beat and every list decoded all of them, for every task this
// broker had ever held (limits N1, G33). Here they are rows the hot path never
// selects.
//
// Nothing is deleted and nothing is moved to a cold tier (D26): a summary is
// stored whole — cutting it is the screen's business (limits N8) — and it
// stays in this database until the `store.db` capacity row says otherwise.

// The two bodies this table holds for a task.
const (
	TextInstructions = "instructions"
	TextSummary      = "summary"
)

const textsSchema = `
CREATE TABLE IF NOT EXISTS broker_task_texts (
  task_id TEXT NOT NULL,
  field   TEXT NOT NULL,
  body    TEXT NOT NULL,
  PRIMARY KEY (task_id, field)
);
`

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readTexts reads every long-prose body a task has.
func (s *Store) readTexts(ctx context.Context, q querier, taskID string) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT field, body FROM broker_task_texts WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var field, body string
		if err := rows.Scan(&field, &body); err != nil {
			return nil, err
		}
		out[field] = body
	}
	s.reads.texts.Add(int64(len(out)))
	return out, rows.Err()
}

// writeTexts stores each body given, removes each one given empty, and
// answers how many rows it changed. An unchanged body is not rewritten.
func writeTexts(ctx context.Context, tx *sql.Tx, taskID string, texts map[string]string) (int64, error) {
	changed := int64(0)
	for field, body := range texts {
		var res sql.Result
		var err error
		if body == "" {
			res, err = tx.ExecContext(ctx, `DELETE FROM broker_task_texts WHERE task_id = ? AND field = ?`, taskID, field)
		} else {
			res, err = tx.ExecContext(ctx,
				`INSERT INTO broker_task_texts (task_id, field, body) VALUES (?, ?, ?)
				 ON CONFLICT(task_id, field) DO UPDATE SET body = excluded.body
				 WHERE broker_task_texts.body <> excluded.body`, taskID, field, body)
		}
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		changed += n
	}
	return changed, nil
}

// migrateTexts moves the prose still inside task records written before this
// table existed out to it, in one transaction.
//
// Stated as what it wants: a record whose instructions and summary are
// already empty is not touched, so running this on every open is a no-op once
// it has run. A record that is not valid JSON is left exactly as it was — it
// is an unreadable row (D05 ②), and this code has no business rewriting it.
func migrateTexts(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	const pending = `json_valid(record) AND (
	    COALESCE(json_extract(record, '$.instructions'), '') <> ''
	 OR (json_type(record, '$.result') = 'object' AND COALESCE(json_extract(record, '$.result.summary'), '') <> ''))`
	if _, err := tx.Exec(`INSERT INTO broker_task_texts (task_id, field, body)
	    SELECT id, 'instructions', json_extract(record, '$.instructions') FROM broker_tasks
	    WHERE ` + pending + ` AND COALESCE(json_extract(record, '$.instructions'), '') <> ''
	    ON CONFLICT(task_id, field) DO UPDATE SET body = excluded.body`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO broker_task_texts (task_id, field, body)
	    SELECT id, 'summary', json_extract(record, '$.result.summary') FROM broker_tasks
	    WHERE ` + pending + ` AND json_type(record, '$.result') = 'object'
	      AND COALESCE(json_extract(record, '$.result.summary'), '') <> ''
	    ON CONFLICT(task_id, field) DO UPDATE SET body = excluded.body`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE broker_tasks SET
	      record = CASE WHEN json_type(record, '$.result') = 'object'
	                    THEN json_set(record, '$.instructions', '', '$.result.summary', '')
	                    ELSE json_set(record, '$.instructions', '') END,
	      version = version + 1
	    WHERE ` + pending); err != nil {
		return err
	}
	return tx.Commit()
}
