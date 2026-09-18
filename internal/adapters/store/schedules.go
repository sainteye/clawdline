package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Schedules live in this daemon's own store, one row per file.
//
// The row's body is the Swift app's schedule file, byte for byte what the
// parser reads (`internal/domain/schedule`), so an export is the file and an
// import is a copy. What the file cannot say about itself is beside it:
// when this daemon first saw it, the latest occurrence it already decided, the
// last one it missed. Those three are durable because the decisions they
// record must survive a restart — the Swift app keeps the "handled" table in
// memory and says so, and a restart inside a catch-up window is exactly when a
// second session for the same occurrence would open.
//
// The table the interval model used (`schedules`) is left in place and no
// longer read: a store that already has it keeps opening. Rows written into it
// by that model cannot be expressed in this file format; LegacyScheduleRows
// counts them so the daemon can say so rather than drop them silently.
const scheduleSchema = `
CREATE TABLE IF NOT EXISTS schedule_files (
  id          TEXT    PRIMARY KEY,
  body        TEXT    NOT NULL,
  first_seen  INTEGER NOT NULL DEFAULT 0,
  last_fire   INTEGER NOT NULL DEFAULT 0,
  last_missed INTEGER NOT NULL DEFAULT 0,
  updated_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS schedule_runs (
  task_id     TEXT    PRIMARY KEY,
  schedule_id TEXT    NOT NULL,
  created_at  INTEGER NOT NULL,
  fire_at     INTEGER NOT NULL DEFAULT 0,
  how         TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS schedule_runs_by_schedule ON schedule_runs(schedule_id, created_at);
CREATE TABLE IF NOT EXISTS schedule_webhook_bindings (
  hook_id     TEXT    PRIMARY KEY,
  schedule_id TEXT    NOT NULL UNIQUE,
  bound_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS schedule_webhook_binds (
  request_id   TEXT    PRIMARY KEY,
  digest       TEXT    NOT NULL,
  status       INTEGER,
  code         TEXT,
  body         BLOB,
  created_at   INTEGER NOT NULL,
  completed_at INTEGER
);
`

func openSchedules(db *sql.DB) error {
	_, err := db.Exec(scheduleSchema)
	return err
}

// ScheduleFile is one stored schedule and this daemon's facts about it.
type ScheduleFile struct {
	ID        string
	Body      []byte
	FirstSeen time.Time
	// LastFire is the latest occurrence already decided: run, skipped because a
	// run was still working, or missed.
	LastFire   time.Time
	LastMissed time.Time
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// ceilSecond rounds a sighting up to the next whole second. The store keeps
// seconds, and an occurrence is compared with the sighting by Before: a row
// first read at 09:00:00.4 stored as 09:00:00 would make the 09:00 occurrence
// not-before its own sighting, which is firing on sight by half a second.
func ceilSecond(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	if cut := t.Truncate(time.Second); !cut.Equal(t) {
		return cut.Add(time.Second)
	}
	return t
}

func fromUnix(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// ScheduleFiles returns every stored schedule, stamping anything it is seeing
// for the first time.
//
// The stamp is written on read rather than on write because a row can arrive
// without passing through a write this daemon made — restored, copied, written
// by hand into the database — and those are exactly the rows that fired the
// instant the clock ticked (docs/plan.md §3.2).
func (s *Store) ScheduleFiles(ctx context.Context) ([]ScheduleFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, body, first_seen, last_fire, last_missed FROM schedule_files ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	out := []ScheduleFile{}
	unseen := []string{}
	now := ceilSecond(time.Now())
	for rows.Next() {
		var f ScheduleFile
		var body string
		var seen, fire, missed int64
		if err := rows.Scan(&f.ID, &body, &seen, &fire, &missed); err != nil {
			rows.Close()
			return nil, err
		}
		f.Body = []byte(body)
		f.FirstSeen, f.LastFire, f.LastMissed = fromUnix(seen), fromUnix(fire), fromUnix(missed)
		if f.FirstSeen.IsZero() {
			f.FirstSeen = now
			unseen = append(unseen, f.ID)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// Closed before writing: this store holds one connection, and an UPDATE
	// issued with the cursor open would wait for a reader waiting for it.
	rows.Close()
	for _, id := range unseen {
		// A sighting that could not be recorded does not become "seen": the
		// value returned already says now, so this read fires nothing, and the
		// next read tries to stamp it again.
		_, _ = s.db.ExecContext(ctx,
			`UPDATE schedule_files SET first_seen = ? WHERE id = ? AND first_seen = 0`, now.Unix(), id)
	}
	return out, nil
}

// ScheduleFileByID is one stored schedule, or false when there is none.
func (s *Store) ScheduleFileByID(ctx context.Context, id string) (ScheduleFile, bool, error) {
	all, err := s.ScheduleFiles(ctx)
	if err != nil {
		return ScheduleFile{}, false, err
	}
	for _, f := range all {
		if f.ID == id {
			return f, true, nil
		}
	}
	return ScheduleFile{}, false, nil
}

// ErrScheduleExists is a create naming an id that is already stored.
var ErrScheduleExists = errors.New("a schedule with that id already exists")

// CreateScheduleFile stores a new schedule. `firstSeen` is the instant the
// daemon is making or receiving it; `handled` is an occurrence to treat as
// already decided, which an import uses so that nothing the previous owner was
// responsible for is run a second time here.
func (s *Store) CreateScheduleFile(ctx context.Context, id string, body []byte, firstSeen, handled time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO schedule_files (id, body, first_seen, last_fire, last_missed, updated_at)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		id, string(body), unix(ceilSecond(firstSeen)), unix(handled), time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScheduleExists
	}
	return nil
}

// ReplaceScheduleFile rewrites a schedule's body and nothing else: its
// sighting and its decided occurrences belong to the row, not to the save.
func (s *Store) ReplaceScheduleFile(ctx context.Context, id string, body []byte) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE schedule_files SET body = ?, updated_at = ? WHERE id = ?`, string(body), time.Now().Unix(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteScheduleFile removes a schedule. Its runs stay: they are tasks with
// their own records, and removing a schedule is not cancelling its work.
func (s *Store) DeleteScheduleFile(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedule_files WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkScheduleFire records an occurrence as decided. It only ever moves
// forward, so a late writer cannot hand an occurrence back to the timer.
func (s *Store) MarkScheduleFire(ctx context.Context, id string, fire time.Time, missed bool) error {
	q := `UPDATE schedule_files SET last_fire = MAX(last_fire, ?) WHERE id = ?`
	if missed {
		q = `UPDATE schedule_files SET last_fire = MAX(last_fire, ?), last_missed = MAX(last_missed, ?) WHERE id = ?`
		_, err := s.db.ExecContext(ctx, q, fire.Unix(), fire.Unix(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, q, fire.Unix(), id)
	return err
}

// ClaimScheduleFire records an occurrence as decided only if nothing has
// decided it yet, and says whether this caller is the one that did. It is the
// compare-and-set the timer takes before dispatching: two daemons pointed at
// one state directory, or a timer and a manual run, cannot both claim it.
func (s *Store) ClaimScheduleFire(ctx context.Context, id string, fire time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE schedule_files SET last_fire = ? WHERE id = ? AND last_fire < ?`, fire.Unix(), id, fire.Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RestoreScheduleFire hands an occurrence back to the timer — only if it is
// still the one recorded, so a later decision is never undone.
func (s *Store) RestoreScheduleFire(ctx context.Context, id string, fire, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedule_files SET last_fire = ? WHERE id = ? AND last_fire = ?`, unix(before), id, fire.Unix())
	return err
}

// ScheduleRun is one task a schedule made, joined to the task's own record.
type ScheduleRun struct {
	TaskID     string
	ScheduleID string
	Created    time.Time
	Fire       time.Time
	How        string
	State      string
	Assistant  string
	ProjectDir string
	Summary    string
}

// RecordScheduleRun links a task to the schedule that made it. Written before
// the broker is asked to record the task, so a process that dies between the
// two still leaves the schedule knowing its run; ForgetScheduleRun takes the
// link back when the broker answers that it recorded nothing.
func (s *Store) RecordScheduleRun(ctx context.Context, taskID, scheduleID string, created, fire time.Time, how string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO schedule_runs (task_id, schedule_id, created_at, fire_at, how) VALUES (?, ?, ?, ?, ?)`,
		taskID, scheduleID, created.Unix(), unix(fire), how)
	return err
}

// ForgetScheduleRun removes a link written ahead of a dispatch that then
// recorded no task — only on the broker's answer that there is none.
func (s *Store) ForgetScheduleRun(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedule_runs WHERE task_id = ?`, taskID)
	return err
}

// LegacyScheduleRows counts rows the retired interval model left behind.
func (s *Store) LegacyScheduleRows(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules`).Scan(&n)
	return n, err
}

// ScheduleRuns is every retained run, newest first. `scheduleID` empty means
// every schedule's.
func (s *Store) ScheduleRuns(ctx context.Context, scheduleID string) ([]ScheduleRun, error) {
	// The task's state is the broker's, read from the same row its beat
	// writes (D07): a run whose task timed out is finished here the moment
	// the broker says so, with nothing to copy across. A run from before the
	// broker ran schedules names a task no broker row holds, and reads with
	// no state — the Swift registry's "record gone", which is not working.
	q := `SELECT r.task_id, r.schedule_id, r.created_at, r.fire_at, r.how,
	             COALESCE(t.state, ''), COALESCE(t.assistant, ''), COALESCE(t.project, ''),
	             COALESCE(x.body, '')
	      FROM schedule_runs r
	      LEFT JOIN broker_tasks t ON t.id = r.task_id
	      LEFT JOIN broker_task_texts x ON x.task_id = r.task_id AND x.field = '` + TextSummary + `'`
	args := []any{}
	if scheduleID != "" {
		q += ` WHERE r.schedule_id = ?`
		args = append(args, scheduleID)
	}
	q += ` ORDER BY r.created_at DESC, r.task_id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []ScheduleRun{}
	for rows.Next() {
		var r ScheduleRun
		var created, fire int64
		if err := rows.Scan(&r.TaskID, &r.ScheduleID, &created, &fire, &r.How,
			&r.State, &r.Assistant, &r.ProjectDir, &r.Summary); err != nil {
			rows.Close()
			return nil, err
		}
		r.Created, r.Fire = fromUnix(created), fromUnix(fire)
		out = append(out, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ErrBindingConflict is a hook or a schedule that is already bound elsewhere.
var ErrBindingConflict = errors.New("binding_conflict")

// ScheduleWebhook is the hook bound to a schedule, if any.
func (s *Store) ScheduleWebhook(ctx context.Context, scheduleID string) (string, bool, error) {
	var hook string
	err := s.db.QueryRowContext(ctx,
		`SELECT hook_id FROM schedule_webhook_bindings WHERE schedule_id = ?`, scheduleID).Scan(&hook)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hook, true, nil
}

// BindScheduleWebhook is `ScheduleWebhookBindingStore.bind`: the same pair
// again is a no-op, a schedule already bound needs the hook it is replacing
// named, and a hook belongs to one schedule.
func (s *Store) BindScheduleWebhook(ctx context.Context, hookID, scheduleID, replaceHookID string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx,
		`SELECT hook_id FROM schedule_webhook_bindings WHERE schedule_id = ?`, scheduleID).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if current == hookID {
		return nil
	}
	if replaceHookID != "" {
		if current != replaceHookID {
			return ErrBindingConflict
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_webhook_bindings WHERE hook_id = ?`, replaceHookID); err != nil {
			return err
		}
	} else if current != "" {
		return ErrBindingConflict
	}
	var other string
	err = tx.QueryRowContext(ctx,
		`SELECT schedule_id FROM schedule_webhook_bindings WHERE hook_id = ?`, hookID).Scan(&other)
	if err == nil {
		return ErrBindingConflict
	}
	if err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schedule_webhook_bindings (hook_id, schedule_id, bound_at) VALUES (?, ?, ?)`,
		hookID, scheduleID, at.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// WebhookBind is what one bind request id has already been answered with.
type WebhookBind struct {
	Fresh, Pending, Conflict bool
	Status                   int
	Code                     string
	Body                     []byte
}

// BeginWebhookBind is `ScheduleWebhookBindLedger.begin`: a request id seen
// with another digest is a conflict, one still in flight is pending, one
// completed replays its answer, and a new one is recorded before any effect.
func (s *Store) BeginWebhookBind(ctx context.Context, requestID, digest string, now time.Time) (WebhookBind, error) {
	var got string
	var status sql.NullInt64
	var code sql.NullString
	var body []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT digest, status, code, body FROM schedule_webhook_binds WHERE request_id = ?`, requestID).
		Scan(&got, &status, &code, &body)
	if err == sql.ErrNoRows {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO schedule_webhook_binds (request_id, digest, created_at) VALUES (?, ?, ?)`,
			requestID, digest, now.Unix())
		return WebhookBind{Fresh: true}, err
	}
	if err != nil {
		return WebhookBind{}, err
	}
	if got != digest {
		return WebhookBind{Conflict: true}, nil
	}
	if !status.Valid {
		return WebhookBind{Pending: true}, nil
	}
	return WebhookBind{Status: int(status.Int64), Code: code.String, Body: body}, nil
}

// CompleteWebhookBind files the answer a bind request id replays from now on.
func (s *Store) CompleteWebhookBind(ctx context.Context, requestID, digest string, status int, code string, body []byte, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedule_webhook_binds SET status = ?, code = ?, body = ?, completed_at = ?
		 WHERE request_id = ? AND digest = ?`, status, code, body, now.Unix(), requestID, digest)
	return err
}
