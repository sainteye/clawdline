package store

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"
)

// A task rewrite as one transaction (D08, G13).
//
// The broker's `mutate` used to read a record, change it and save it under a
// mutex this process held. The rule it enforced — a change is a function of
// the record as it is when the change is made — was right, and it is kept;
// what moves is where the rule is enforced. Here the read, the decision and
// the write are one BEGIN IMMEDIATE transaction, so the rule holds against a
// writer in another process too, and the write itself is still a
// compare-and-set against the version that was read, so a bug that let two
// writers through would be a conflict and not a silent overwrite.

// Tx is what a change function may do while it holds the write right. Every
// method here runs inside the one transaction; nothing on it reaches outside
// the database.
type Tx struct {
	ctx context.Context
	tx  *sql.Tx
	s   *Store
	// wrote is how many rows the methods below changed. A change that decides
	// to leave the task row alone still commits what it wrote through here.
	wrote int64
}

// Notice reads a task's completion envelope as the transaction sees it.
func (t *Tx) Notice(taskID string) (BrokerNotice, error) {
	return scanNotice(t.tx.QueryRowContext(t.ctx,
		`SELECT `+noticeColumns+` FROM broker_notices WHERE task_id = ?`, taskID))
}

// AppendNote is AppendBrokerNote inside the transaction, so a note and what
// it proved about the task are one fact.
func (t *Tx) AppendNote(taskID, note string, at time.Time) (BrokerNote, bool, error) {
	res, err := t.tx.ExecContext(t.ctx,
		`INSERT OR IGNORE INTO broker_notes (task_id, at, note) VALUES (?, ?, ?)`,
		taskID, at.Unix(), note)
	if err != nil {
		return BrokerNote{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return BrokerNote{}, false, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return BrokerNote{}, false, err
	}
	t.wrote += n
	return BrokerNote{Seq: seq, TaskID: taskID, At: time.Unix(at.Unix(), 0), Note: note}, true, nil
}

// BrokerWrite is what a change decided: the row to store, and what goes with
// it in the same transaction.
type BrokerWrite struct {
	Row BrokerRow
	// Notice is a completion envelope to open, or nil.
	Notice *BrokerNotice
	Events []Event
	// Effects are the side effects this change owes the world — a tab to
	// close, a message to type. They are recorded here, as intent, and run
	// after the commit (outbox.go).
	Effects []Effect
}

// UpdateBrokerTask reads one task inside a write transaction, hands it to
// change, and stores what change answers — in that transaction, only if the
// row is still at the version read, with its notice, events and effects.
//
// change answering (nil, nil) leaves the task row as it is, and commits only
// what it wrote through the Tx. An error
// from change is returned as it is, and nothing is written. The answer is the
// row as stored afterwards (or as read, when nothing was written) and the ids
// of the effects recorded, which belong to this handle.
func (s *Store) UpdateBrokerTask(ctx context.Context, id string, change func(tx *Tx, row BrokerRow) (*BrokerWrite, error)) (BrokerRow, []int64, error) {
	var out BrokerRow
	var ids []int64
	var inner error
	err := s.write(ctx, func(sqlTx *sql.Tx) (int64, error) {
		row, err := scanBroker(sqlTx.QueryRowContext(ctx,
			`SELECT `+brokerColumns+` FROM broker_tasks WHERE id = ?`, id))
		if err != nil {
			return 0, err
		}
		s.reads.records.Add(1)
		if row.Texts, err = s.readTexts(ctx, sqlTx, id); err != nil {
			return 0, err
		}
		out = row
		t := &Tx{ctx: ctx, tx: sqlTx, s: s}
		w, err := change(t, row)
		if err != nil {
			// The change function's own refusal is not the store failing:
			// carried out beside the write rather than through it.
			inner = err
			return 0, nil
		}
		if w == nil {
			return t.wrote, nil
		}
		next := w.Row
		if next.UpdatedAt.IsZero() {
			next.UpdatedAt = time.Now()
		}
		res, err := sqlTx.ExecContext(ctx,
			`UPDATE broker_tasks SET project = ?, repository = ?, assistant = ?, state = ?,
			   updated_at = ?, record = ?, version = version + 1
			 WHERE id = ? AND version = ?`,
			next.Project, next.Repository, next.Assistant, next.State,
			next.UpdatedAt.Unix(), string(next.Record), id, row.Version)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return 0, err
		} else if n != 1 {
			return 0, ErrConflict
		}
		changed := 1 + t.wrote
		if next.Texts != nil {
			n, err := writeTexts(ctx, sqlTx, id, next.Texts)
			if err != nil {
				return 0, err
			}
			changed += n
		}
		if w.Notice != nil {
			if err := insertNotice(ctx, sqlTx, *w.Notice); err != nil {
				return 0, err
			}
			changed++
		}
		if err := insertEvents(ctx, sqlTx, w.Events); err != nil {
			return 0, err
		}
		for _, e := range w.Effects {
			eid, err := s.insertEffect(ctx, sqlTx, e)
			if err != nil {
				return 0, err
			}
			ids = append(ids, eid)
		}
		next.ID, next.Version, next.SecretHash, next.CreatedAt = id, row.Version+1, row.SecretHash, row.CreatedAt
		out = next
		return changed + int64(len(w.Events)+len(w.Effects)), nil
	})
	if err != nil {
		return BrokerRow{}, nil, err
	}
	if inner != nil {
		return out, nil, inner
	}
	return out, ids, nil
}

// readStats counts what the store handed back to readers, so "the beat no
// longer reads the whole table" is a number a test can hold (G33).
type readStats struct {
	// records is how many task rows were returned with their record.
	records atomic.Int64
	// texts is how many long-prose bodies were returned.
	texts atomic.Int64
}

// ReadCounts is how many task records and long-prose bodies this handle has
// returned since it opened.
func (s *Store) ReadCounts() (records, texts int64) {
	return s.reads.records.Load(), s.reads.texts.Load()
}

// openW2 creates the tables W2 added — long prose, the outbox and the request
// receipts — and moves the prose already inside task records out to its own
// table.
func openW2(db *sql.DB) error {
	for _, ddl := range []string{textsSchema, outboxSchema, receiptsSchema} {
		if _, err := db.Exec(ddl); err != nil {
			return err
		}
	}
	return migrateTexts(db)
}
