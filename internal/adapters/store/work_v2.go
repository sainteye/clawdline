package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

const workV2Schema = `
CREATE TABLE IF NOT EXISTS work_v2_items (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL,
  project_path      TEXT NOT NULL,
  kind              TEXT NOT NULL CHECK (kind IN ('feature','issue','epic','refactor','plan')),
  title             TEXT NOT NULL,
  description       TEXT NOT NULL,
  acceptance_criteria TEXT NOT NULL DEFAULT '',
  acceptance_version INTEGER NOT NULL DEFAULT 1,
  acceptance_digest TEXT NOT NULL DEFAULT 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
  phase             TEXT NOT NULL CHECK (phase IN ('created','assigning','assigned','implementing','verifying','merging','deploying','done','cancelled')),
  condition         TEXT NOT NULL DEFAULT '' CHECK (condition IN ('','blocked','waiting_user','owner_required','owner_offline','evidence_unknown','assignment_failed','assigned_unnotified')),
  user_action       TEXT NOT NULL DEFAULT '',
  decision_id       TEXT NOT NULL DEFAULT '',
  deployment_policy TEXT NOT NULL CHECK (deployment_policy IN ('required','not_required','agent_decides')),
  review_required   INTEGER NOT NULL DEFAULT 0 CHECK (review_required IN (0,1)),
  owner_session     TEXT NOT NULL DEFAULT '',
  created_by        TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  closed_at         INTEGER,
  cycle             INTEGER NOT NULL DEFAULT 1,
  gate_snapshot_cycle INTEGER NOT NULL DEFAULT 0,
  gate_snapshot_at  INTEGER NOT NULL DEFAULT 0,
  planning_gate     INTEGER NOT NULL DEFAULT 0 CHECK (planning_gate IN (0,1)),
  verify_gate       INTEGER NOT NULL DEFAULT 0 CHECK (verify_gate IN (0,1)),
  cycle_base_commit TEXT NOT NULL DEFAULT '',
  version           INTEGER NOT NULL DEFAULT 1,
  created_via       TEXT NOT NULL DEFAULT '',
  parent_id         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS work_v2_items_project ON work_v2_items(project_id, updated_at DESC, id);
CREATE INDEX IF NOT EXISTS work_v2_items_owner ON work_v2_items(owner_session, phase, updated_at DESC)
  WHERE owner_session <> '';
CREATE TABLE IF NOT EXISTS work_v2_assignments (
  id                 TEXT PRIMARY KEY,
  work_id            TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  mode               TEXT NOT NULL CHECK (mode IN ('existing_session','new_session')),
  session_id         TEXT NOT NULL DEFAULT '',
  terminal_id        TEXT NOT NULL DEFAULT '',
  assistant          TEXT NOT NULL DEFAULT '',
  model              TEXT NOT NULL DEFAULT '',
  state              TEXT NOT NULL CHECK (state IN ('assigning','active','released','failed')),
  human_actor        TEXT NOT NULL,
  root_assignment_id TEXT NOT NULL DEFAULT '',
  failure            TEXT NOT NULL DEFAULT '',
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  released_at        INTEGER,
  claimed_via        TEXT NOT NULL DEFAULT '',
  persona            TEXT NOT NULL DEFAULT '',
  gate_previewed     INTEGER NOT NULL DEFAULT 0 CHECK (gate_previewed IN (0,1)),
  planning_gate      INTEGER NOT NULL DEFAULT 0 CHECK (planning_gate IN (0,1)),
  verify_gate        INTEGER NOT NULL DEFAULT 0 CHECK (verify_gate IN (0,1)),
  cycle_base_commit  TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS work_v2_one_active_assignment ON work_v2_assignments(work_id)
  WHERE state = 'active';
CREATE INDEX IF NOT EXISTS work_v2_assignment_session ON work_v2_assignments(session_id, state, updated_at DESC);
CREATE TABLE IF NOT EXISTS work_v2_documents (
  id         TEXT PRIMARY KEY,
  work_id    TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('spec','design','test','deploy','completion_report','other','plan','plan_review')),
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  reference  TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL,
  version    INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_documents_item ON work_v2_documents(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_images (
  id         TEXT PRIMARY KEY,
  work_id    TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK (media_type IN ('image/png','image/jpeg')),
  sha256     TEXT NOT NULL,
  byte_count INTEGER NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  position   INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_images_item ON work_v2_images(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_steps (
  id           TEXT PRIMARY KEY,
  work_id      TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  done         INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0,1)),
  position     INTEGER NOT NULL,
  created_by   TEXT NOT NULL,
  completed_by TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  completed_at INTEGER,
  version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS work_v2_steps_item ON work_v2_steps(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_events (
  seq              INTEGER PRIMARY KEY AUTOINCREMENT,
  work_id          TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL,
  actor            TEXT NOT NULL,
  previous_version INTEGER NOT NULL,
  next_version     INTEGER NOT NULL,
  payload          TEXT NOT NULL DEFAULT '{}',
  at               INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_events_item ON work_v2_events(work_id, seq);
CREATE TABLE IF NOT EXISTS work_v2_phase_receipts (
  work_id         TEXT PRIMARY KEY REFERENCES work_v2_items(id) ON DELETE CASCADE,
  phase           TEXT NOT NULL,
  occurrence      INTEGER NOT NULL,
  entered_at      INTEGER NOT NULL,
  seen_occurrence INTEGER NOT NULL DEFAULT 0,
  seen_at         INTEGER
);
CREATE TRIGGER IF NOT EXISTS work_v2_events_no_update BEFORE UPDATE ON work_v2_events
  BEGIN SELECT RAISE(ABORT, 'work_v2_events_append_only'); END;
CREATE TRIGGER IF NOT EXISTS work_v2_events_no_delete BEFORE DELETE ON work_v2_events
  BEGIN SELECT RAISE(ABORT, 'work_v2_events_append_only'); END;
CREATE TABLE IF NOT EXISTS session_direct_todos (
  id           TEXT PRIMARY KEY,
  session_id   TEXT NOT NULL,
  text         TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  sent_at      INTEGER,
  read_at      INTEGER,
  completed_at INTEGER,
  completed_by TEXT NOT NULL DEFAULT '',
  version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS session_direct_todos_owner ON session_direct_todos(session_id, completed_at, created_at, id);
CREATE TABLE IF NOT EXISTS session_direct_todo_images (
  id         TEXT PRIMARY KEY,
  todo_id    TEXT NOT NULL REFERENCES session_direct_todos(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK (media_type IN ('image/png','image/jpeg')),
  sha256     TEXT NOT NULL,
  byte_count INTEGER NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  position   INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_direct_todo_images_todo ON session_direct_todo_images(todo_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_proposals (
  id                   TEXT PRIMARY KEY,
  project_id           TEXT NOT NULL,
  project_path         TEXT NOT NULL,
  kind                 TEXT NOT NULL CHECK (kind IN ('feature','issue','epic','refactor','plan')),
  title                TEXT NOT NULL,
  description          TEXT NOT NULL,
  reason               TEXT NOT NULL,
  suggested_acceptance TEXT NOT NULL DEFAULT '',
  session_id           TEXT NOT NULL,
  source_work_id       TEXT NOT NULL DEFAULT '',
  source_todo_id       TEXT NOT NULL DEFAULT '',
  state                TEXT NOT NULL CHECK (state IN ('pending','accepted','rejected')),
  accepted_work_id     TEXT NOT NULL DEFAULT '',
  created_at           INTEGER NOT NULL,
  resolved_at          INTEGER,
  version              INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS work_v2_proposals_state ON work_v2_proposals(state, created_at, id);
`

const (
	WorkV2OpenLimit       = 2_000
	WorkV2PlanningLimit   = 1_000
	WorkV2AssignmentLimit = 4_000
	WorkV2DocumentLimit   = 32
	WorkV2ImageLimit      = 6
	WorkV2ImageItemLimit  = 15 << 20
	WorkV2ImageTotalLimit = 512 << 20
	WorkV2StepLimit       = 128
	DirectTodoV2Limit     = 500
	WorkV2ProposalLimit   = 500
)

var (
	ErrNoWorkV2                 = errors.New("no such v2 work item")
	ErrWorkV2Full               = errors.New("v2 work items full")
	ErrPlanningV2Full           = errors.New("v2 planning items full")
	ErrDirectTodoFull           = errors.New("direct session todos full")
	ErrDirectTodoImagesFull     = errors.New("direct session todo images full")
	ErrDirectTodoImageBytesFull = errors.New("direct session todo image bytes full")
	ErrWorkV2DocsFull           = errors.New("v2 work documents full")
	ErrWorkV2ImagesFull         = errors.New("v2 work images full")
	ErrWorkV2ImageBytesFull     = errors.New("v2 work image bytes full")
	ErrWorkV2StepsFull          = errors.New("v2 work steps full")
	ErrWorkV2ProposalsFull      = errors.New("v2 work proposals full")
)

func openWorkV2(db *sql.DB) error {
	if _, err := db.Exec(workV2Schema); err != nil {
		return err
	}
	if err := openWorkV2Gates(db); err != nil {
		return err
	}
	has, err := hasColumn(db, "work_v2_items", "user_action")
	if err != nil {
		return err
	}
	if !has {
		_, err = db.Exec(`ALTER TABLE work_v2_items ADD COLUMN user_action TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return err
		}
	}
	// Acceptance and per-cycle gates were added together. Older items start at
	// version one. An already-active Epic receives its exact person-authored
	// description as compatibility acceptance so its formerly valid reviewed
	// plan is not trapped by the newly introduced planning gate.
	// Only already-owned, non-terminal work receives a compatibility snapshot:
	// Epics retain their old plan gate and every other kind retains the old
	// ungated behaviour. Pending, unassigned and terminal rows stay unsnapped.
	columns := []struct {
		name, ddl string
	}{
		{"acceptance_criteria", `ALTER TABLE work_v2_items ADD COLUMN acceptance_criteria TEXT NOT NULL DEFAULT ''`},
		{"acceptance_version", `ALTER TABLE work_v2_items ADD COLUMN acceptance_version INTEGER NOT NULL DEFAULT 1`},
		{"acceptance_digest", `ALTER TABLE work_v2_items ADD COLUMN acceptance_digest TEXT NOT NULL DEFAULT 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'`},
		{"gate_snapshot_cycle", `ALTER TABLE work_v2_items ADD COLUMN gate_snapshot_cycle INTEGER NOT NULL DEFAULT 0`},
		{"gate_snapshot_at", `ALTER TABLE work_v2_items ADD COLUMN gate_snapshot_at INTEGER NOT NULL DEFAULT 0`},
		{"planning_gate", `ALTER TABLE work_v2_items ADD COLUMN planning_gate INTEGER NOT NULL DEFAULT 0 CHECK (planning_gate IN (0,1))`},
		{"verify_gate", `ALTER TABLE work_v2_items ADD COLUMN verify_gate INTEGER NOT NULL DEFAULT 0 CHECK (verify_gate IN (0,1))`},
		{"cycle_base_commit", `ALTER TABLE work_v2_items ADD COLUMN cycle_base_commit TEXT NOT NULL DEFAULT ''`},
		// review_required is the person's "Needs independent review" switch on
		// a Feature; every item written before it reads unchecked.
		{"review_required", `ALTER TABLE work_v2_items ADD COLUMN review_required INTEGER NOT NULL DEFAULT 0 CHECK (review_required IN (0,1))`},
		// decision_id is the open decision an Agent's waiting_user points at;
		// every item written before it waits, if at all, on its user_action.
		{"decision_id", `ALTER TABLE work_v2_items ADD COLUMN decision_id TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		if has, err = hasColumn(db, "work_v2_items", column.name); err != nil {
			return err
		} else if !has {
			if _, err = db.Exec(column.ddl); err != nil {
				return err
			}
		}
	}
	type legacyEpicAcceptance struct {
		id, criteria string
	}
	var legacyEpicAcceptances []legacyEpicAcceptance
	rows, err := db.Query(`SELECT id, description FROM work_v2_items
		WHERE gate_snapshot_cycle=0 AND owner_session<>'' AND kind='epic'
		  AND phase IN ('assigned','implementing','verifying','merging','deploying')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var candidate legacyEpicAcceptance
		if err := rows.Scan(&candidate.id, &candidate.criteria); err != nil {
			rows.Close()
			return err
		}
		legacyEpicAcceptances = append(legacyEpicAcceptances, candidate)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err = db.Exec(`UPDATE work_v2_items
		SET gate_snapshot_cycle=cycle,
		    gate_snapshot_at=COALESCE((SELECT a.created_at FROM work_v2_assignments a
		      WHERE a.work_id=work_v2_items.id AND a.state='active' ORDER BY a.rowid DESC LIMIT 1), updated_at),
		    planning_gate=CASE WHEN kind='epic' THEN 1 ELSE 0 END,
		    verify_gate=0
		WHERE gate_snapshot_cycle=0 AND owner_session<>''
		  AND phase IN ('assigned','implementing','verifying','merging','deploying')`); err != nil {
		return err
	}
	for _, candidate := range legacyEpicAcceptances {
		if _, err = db.Exec(`UPDATE work_v2_items
			SET acceptance_criteria=?, acceptance_version=1, acceptance_digest=?
			WHERE id=?`, candidate.criteria, work.AcceptanceDigest(candidate.criteria), candidate.id); err != nil {
			return err
		}
	}
	// created_via is the person's message a Session created the item on
	// (work-system-v2 §2, amended 2026-09-25). Items written before it have
	// none, which reads as "created by a person or by nobody's message".
	if has, err = hasColumn(db, "work_v2_items", "created_via"); err != nil {
		return err
	} else if !has {
		if _, err = db.Exec(`ALTER TABLE work_v2_items ADD COLUMN created_via TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// claimed_via is the person's message a Session claimed the item on;
	// assignments written before it were all made by a person.
	if has, err = hasColumn(db, "work_v2_assignments", "claimed_via"); err != nil {
		return err
	} else if !has {
		if _, err = db.Exec(`ALTER TABLE work_v2_assignments ADD COLUMN claimed_via TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// persona is the built-in persona a new Session was opened as; assignments
	// written before it opened none. The catalog is closed in Go, not with a
	// CHECK, so a new persona needs no table rebuild.
	if has, err = hasColumn(db, "work_v2_assignments", "persona"); err != nil {
		return err
	} else if !has {
		if _, err = db.Exec(`ALTER TABLE work_v2_assignments ADD COLUMN persona TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, ddl string }{
		{"gate_previewed", `ALTER TABLE work_v2_assignments ADD COLUMN gate_previewed INTEGER NOT NULL DEFAULT 0 CHECK (gate_previewed IN (0,1))`},
		{"planning_gate", `ALTER TABLE work_v2_assignments ADD COLUMN planning_gate INTEGER NOT NULL DEFAULT 0 CHECK (planning_gate IN (0,1))`},
		{"verify_gate", `ALTER TABLE work_v2_assignments ADD COLUMN verify_gate INTEGER NOT NULL DEFAULT 0 CHECK (verify_gate IN (0,1))`},
		{"cycle_base_commit", `ALTER TABLE work_v2_assignments ADD COLUMN cycle_base_commit TEXT NOT NULL DEFAULT ''`},
	} {
		if has, err = hasColumn(db, "work_v2_assignments", column.name); err != nil {
			return err
		} else if !has {
			if _, err = db.Exec(column.ddl); err != nil {
				return err
			}
		}
	}
	// parent_id is the Epic an item was broken out of by the Epic's owner
	// Session; items written before it have none. The index is made here,
	// after the column exists, not in the schema: on an old store the schema
	// runs before this column is added.
	if has, err = hasColumn(db, "work_v2_items", "parent_id"); err != nil {
		return err
	} else if !has {
		if _, err = db.Exec(`ALTER TABLE work_v2_items ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS work_v2_items_parent ON work_v2_items(parent_id, phase)
    WHERE parent_id <> ''`); err != nil {
		return err
	}
	if err := migrateWorkV2DocumentRoles(db); err != nil {
		return err
	}
	// A store from before the pictures moved to files still has their bytes
	// in a `data` column; Open moves them out (openReferenceImages) and then
	// rebuilds the tables without it, which also widens a PNG-only
	// media-type CHECK to take a JPEG.
	return addReferenceImageHashColumns(db)
}

// migrateWorkV2DocumentRoles widens the CHECK on an existing document table:
// completion_report was added first, plan and plan_review (an Epic's gate)
// after it. SQLite cannot alter a CHECK in place, so keep every row while
// rebuilding the table. The schema call recreates the index after its old
// copy is dropped. A table whose CHECK already names plan_review is current,
// which makes a second open a no-op.
func migrateWorkV2DocumentRoles(db *sql.DB) error {
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='work_v2_documents'`).Scan(&ddl)
	if err == sql.ErrNoRows || (err == nil && strings.Contains(ddl, "'plan_review'")) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	steps := []string{
		`DROP TABLE IF EXISTS work_v2_documents_before_role_change`,
		`ALTER TABLE work_v2_documents RENAME TO work_v2_documents_before_role_change`,
		`DROP INDEX IF EXISTS work_v2_documents_item`,
		workV2Schema,
		`INSERT INTO work_v2_documents (id,work_id,role,title,body,reference,position,version,created_at,updated_at)
		 SELECT id,work_id,role,title,body,reference,position,version,created_at,updated_at
		 FROM work_v2_documents_before_role_change ORDER BY rowid`,
		`DROP TABLE work_v2_documents_before_role_change`,
	}
	for _, step := range steps {
		if _, err := tx.Exec(step); err != nil {
			return fmt.Errorf("work_v2_documents.role: %w", err)
		}
	}
	return tx.Commit()
}

type WorkV2Tx struct {
	ctx   context.Context
	tx    *sql.Tx
	s     *Store
	wrote int64
	// added is the picture files this transaction wrote, dropped the ones
	// whose rows it deleted (work_v2_image_files.go settles both).
	added, dropped []imageRef
}

func (s *Store) WriteWorkV2(ctx context.Context, fn func(*WorkV2Tx) error) error {
	var t *WorkV2Tx
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		t = &WorkV2Tx{ctx: ctx, tx: tx, s: s}
		if err := fn(t); err != nil {
			return 0, err
		}
		return t.wrote, nil
	})
	s.settleImageFiles(ctx, t, err)
	return err
}

func (t *WorkV2Tx) CompleteReceipt(k ReceiptKey, a ReceiptAnswer) error {
	if err := t.s.completeReceipt(t.ctx, t.tx, k, a); err != nil {
		return err
	}
	t.wrote++
	return nil
}

// UpdateOpened keeps a broker record change in the same transaction as a
// Board item and its active assignment. A nil next row is an idempotent no-op.
func (t *WorkV2Tx) UpdateOpened(table, id string, change func(Opened) (*Opened, []Event, error)) (Opened, error) {
	name, err := openedTable(table)
	if err != nil {
		return Opened{}, err
	}
	cur, err := scanOpened(t.tx.QueryRowContext(t.ctx,
		`SELECT id, state, record, created_at, updated_at FROM `+name+` WHERE id = ?`, id))
	if err != nil {
		return Opened{}, err
	}
	next, events, err := change(cur)
	if err != nil || next == nil {
		return cur, err
	}
	if _, err := t.tx.ExecContext(t.ctx,
		`UPDATE `+name+` SET state = ?, record = ?, updated_at = ? WHERE id = ?`,
		next.State, string(next.Record), next.UpdatedAt.Unix(), id); err != nil {
		return Opened{}, err
	}
	if err := insertEvents(t.ctx, t.tx, events); err != nil {
		return Opened{}, err
	}
	t.wrote += 1 + int64(len(events))
	return *next, nil
}

// InvalidateWorkV2VerificationAuthorization is the store-side seam for the
// verification coordinator's rounds and PASS/override authorization. This
// core owns no round table yet, so there is nothing durable to stale here;
// the later coordinator slice implements the body without changing the app
// transitions that already call it inside their item transaction.
func (t *WorkV2Tx) InvalidateWorkV2VerificationAuthorization(prev, next work.ItemV2, reason string) error {
	return t.invalidateWorkGateAuthorization(prev, next, reason)
}

// AddEffect records an external effect in the same transaction as the work
// change that owes it. The caller runs the returned id only after this write
// commits; a daemon that stops first leaves the durable row to recovery.
func (t *WorkV2Tx) AddEffect(e Effect) (int64, error) {
	id, err := t.s.insertEffect(t.ctx, t.tx, e)
	if err != nil {
		return 0, err
	}
	t.wrote++
	return id, nil
}

const workV2Columns = `id, project_id, project_path, kind, title, description,
  acceptance_criteria, acceptance_version, acceptance_digest, phase, condition, user_action,
  deployment_policy, owner_session, created_by, created_at, updated_at, closed_at, cycle,
  gate_snapshot_cycle, gate_snapshot_at, planning_gate, verify_gate, cycle_base_commit, version, created_via, parent_id,
  review_required, decision_id`

const workV2ItemColumns = `i.id, i.project_id, i.project_path, i.kind, i.title, i.description,
  i.acceptance_criteria, i.acceptance_version, i.acceptance_digest, i.phase, i.condition, i.user_action,
  i.deployment_policy, i.owner_session, i.created_by, i.created_at, i.updated_at, i.closed_at, i.cycle,
  i.gate_snapshot_cycle, i.gate_snapshot_at, i.planning_gate, i.verify_gate, i.cycle_base_commit, i.version, i.created_via, i.parent_id,
  i.review_required, i.decision_id`

func scanWorkV2(sc scanner) (work.ItemV2, error) {
	var i work.ItemV2
	var created, updated, gateAt int64
	var closed sql.NullInt64
	var via string
	err := sc.Scan(&i.ID, &i.ProjectID, &i.ProjectPath, &i.Kind, &i.Title, &i.Description,
		&i.AcceptanceCriteria, &i.AcceptanceVersion, &i.AcceptanceDigest, &i.Phase,
		&i.Condition, &i.UserAction, &i.DeploymentPolicy, &i.OwnerSession, &i.CreatedBy, &created, &updated, &closed,
		&i.Cycle, &i.GateSnapshotCycle, &gateAt, &i.PlanningGate, &i.VerifyGate, &i.CycleBaseCommit,
		&i.Version, &via, &i.ParentID, &i.ReviewRequired, &i.DecisionID)
	if err == sql.ErrNoRows {
		return work.ItemV2{}, ErrNoWorkV2
	}
	if err != nil {
		return work.ItemV2{}, err
	}
	i.CreatedAt, i.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	if gateAt > 0 {
		i.GateSnapshotAt = time.Unix(gateAt, 0)
	}
	if closed.Valid {
		i.ClosedAt = time.Unix(closed.Int64, 0)
	}
	if via != "" {
		var v work.CreatedViaV2
		// A column this daemon wrote and cannot read is kept out of the
		// answer rather than failing the whole Board read.
		if json.Unmarshal([]byte(via), &v) == nil && (v.Run != "" || v.Epic != "") {
			i.CreatedVia = &v
		}
	}
	return i, nil
}

func createdViaColumn(v *work.CreatedViaV2) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func gateSnapshotUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func (t *WorkV2Tx) Item(id string) (work.ItemV2, error) {
	return scanWorkV2(t.tx.QueryRowContext(t.ctx, `SELECT `+workV2Columns+` FROM work_v2_items WHERE id = ?`, id))
}

// AcceptanceChangedAt reads the event that established the current contract,
// rather than the item's later phase or condition update time.
func (t *WorkV2Tx) AcceptanceChangedAt(i work.ItemV2) (time.Time, error) {
	var at int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT at FROM work_v2_events WHERE work_id=?
	  AND kind='item.edited' AND json_valid(payload)
	  AND json_extract(payload, '$.acceptance')=1
	  AND json_extract(payload, '$.acceptance_version')=?
	  ORDER BY seq DESC LIMIT 1`, i.ID, i.AcceptanceVersion).Scan(&at)
	if err == sql.ErrNoRows && i.AcceptanceVersion == 1 {
		err = t.tx.QueryRowContext(t.ctx, `SELECT at FROM work_v2_events WHERE work_id=?
		  AND kind='item.created' ORDER BY seq LIMIT 1`, i.ID).Scan(&at)
	}
	return time.Unix(at, 0), err
}

func (t *WorkV2Tx) ActiveOwnedItemCount(session string) (int64, error) {
	return t.count(`owner_session=? AND phase NOT IN ('done','cancelled')`, session)
}

// Tasks is every broker task bound to a Board item: the ones whose work_id
// is the item, and the ones that carry it among their also_work_ids (one
// child doing several items). The second half reads only the rows that carry
// any (the partial index broker_tasks_also_work), not the whole table.
func (t *WorkV2Tx) Tasks(id string) ([]BrokerRow, error) {
	return queryWorkV2Tasks(t.ctx, t.tx, id)
}

func queryWorkV2Tasks(ctx context.Context, q querier, id string) ([]BrokerRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+brokerColumns+` FROM broker_tasks
	  WHERE json_valid(record) AND json_extract(record, '$.work_id') = ?
	  UNION
	  SELECT `+brokerColumns+` FROM broker_tasks
	  WHERE json_valid(record) AND json_extract(record, '$.also_work_ids') IS NOT NULL
	    AND EXISTS (SELECT 1 FROM json_each(record, '$.also_work_ids') WHERE value = ?)
	  ORDER BY created_at,id`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerRow{}
	for rows.Next() {
		r, err := scanBroker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Item(ctx context.Context, id string) (work.ItemV2, error) {
	if err := reading(); err != nil {
		return work.ItemV2{}, err
	}
	return scanWorkV2(s.rd.QueryRowContext(ctx, `SELECT `+workV2Columns+` FROM work_v2_items WHERE id = ?`, id))
}

func (t *WorkV2Tx) count(where string, args ...any) (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_items WHERE `+where, args...).Scan(&n)
	return n, err
}

func (t *WorkV2Tx) CreateItem(i work.ItemV2, actor, payload string) error {
	if i.AcceptanceVersion == 0 || i.AcceptanceDigest == "" {
		work.SetAcceptance(&i, i.AcceptanceCriteria)
	}
	where := `phase NOT IN ('done','cancelled') AND kind IN ('feature','issue','epic')`
	limit, full := int64(WorkV2OpenLimit), ErrWorkV2Full
	if i.Planning() {
		where, limit, full = `kind IN ('refactor','plan') AND phase NOT IN ('done','cancelled')`, WorkV2PlanningLimit, ErrPlanningV2Full
	}
	if n, err := t.count(where); err != nil {
		return err
	} else if n >= limit {
		return full
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_items
	      (id, project_id, project_path, kind, title, description, acceptance_criteria, acceptance_version,
	       acceptance_digest, phase, condition, user_action, deployment_policy, owner_session, created_by,
	       created_at, updated_at, closed_at, cycle, gate_snapshot_cycle, gate_snapshot_at, planning_gate, verify_gate,
	       cycle_base_commit, version, created_via, parent_id, review_required, decision_id)
	      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.ProjectID, i.ProjectPath, i.Kind, i.Title, i.Description, i.AcceptanceCriteria,
		i.AcceptanceVersion, i.AcceptanceDigest, i.Phase, i.Condition,
		i.UserAction, i.DeploymentPolicy, i.OwnerSession, i.CreatedBy, i.CreatedAt.Unix(), i.UpdatedAt.Unix(), i.Cycle,
		i.GateSnapshotCycle, gateSnapshotUnix(i.GateSnapshotAt), i.PlanningGate, i.VerifyGate, i.CycleBaseCommit, i.Version,
		createdViaColumn(i.CreatedVia), i.ParentID, i.ReviewRequired, i.DecisionID)
	if err != nil {
		return err
	}
	t.wrote++
	return t.AppendEvent(work.EventV2{WorkID: i.ID, Kind: "item.created", Actor: actor,
		PreviousVersion: 0, NextVersion: i.Version, Payload: payload, At: i.CreatedAt})
}

// Children counts an Epic's child items: all of them, open or closed, and
// the open ones among them.
func (t *WorkV2Tx) Children(parentID string) (all, open int64, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN phase NOT IN ('done','cancelled') THEN 1 ELSE 0 END),0)
    FROM work_v2_items WHERE parent_id = ?`, parentID).Scan(&all, &open)
	return all, open, err
}

// ItemsCreatedBy counts every item, open or closed, whose creator is actor —
// how many items one person's message has already backed.
func (t *WorkV2Tx) ItemsCreatedBy(actor string) (int64, error) {
	return t.count(`created_by = ?`, actor)
}

// PristineEquivalentItem finds the item an uncertain create already wrote.
// A second, independently keyed press must not turn the same user decision
// into two unassigned cards. Only an untouched item is reusable: once either
// a person or a Session changes it, an identical create is a new decision.
func (t *WorkV2Tx) PristineEquivalentItem(i work.ItemV2) (work.ItemV2, bool, error) {
	found, err := scanWorkV2(t.tx.QueryRowContext(t.ctx, `SELECT `+workV2Columns+` FROM work_v2_items
      WHERE project_id=? AND project_path=? AND kind=? AND title=? AND description=? AND acceptance_criteria=?
        AND deployment_policy=? AND review_required=? AND phase='created' AND owner_session='' AND closed_at IS NULL
        AND created_via='' AND cycle=1 AND version=1
      ORDER BY created_at, id LIMIT 1`,
		i.ProjectID, i.ProjectPath, i.Kind, i.Title, i.Description, i.AcceptanceCriteria, i.DeploymentPolicy, i.ReviewRequired))
	if errors.Is(err, ErrNoWorkV2) {
		return work.ItemV2{}, false, nil
	}
	return found, err == nil, err
}

// PutItem writes one item change and its event. Every change passes through
// here, so here is where an item that stops waiting on a decision — its
// condition cleared or replaced, another decision linked, its owner changed,
// the item closed — withdraws that decision in the same transaction
// (work.LeaveDecision): a Session that no longer waits must not leave the
// person a question in "Waiting on you".
func (t *WorkV2Tx) PutItem(prev, next work.ItemV2, kind, actor, payload string) error {
	left := work.LeaveDecision(prev, &next)
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_items SET project_id=?, project_path=?, kind=?, title=?,
	      description=?, acceptance_criteria=?, acceptance_version=?, acceptance_digest=?, phase=?, condition=?, user_action=?,
	      decision_id=?, deployment_policy=?, owner_session=?, updated_at=?, closed_at=?, cycle=?, gate_snapshot_cycle=?, gate_snapshot_at=?, planning_gate=?,
	      verify_gate=?, cycle_base_commit=?, review_required=?, version=version+1 WHERE id=? AND version=?`,
		next.ProjectID, next.ProjectPath, next.Kind, next.Title, next.Description, next.AcceptanceCriteria,
		next.AcceptanceVersion, next.AcceptanceDigest, next.Phase, next.Condition,
		next.UserAction, next.DecisionID, next.DeploymentPolicy, next.OwnerSession, next.UpdatedAt.Unix(), zeroOrUnix(next.ClosedAt), next.Cycle,
		next.GateSnapshotCycle, gateSnapshotUnix(next.GateSnapshotAt), next.PlanningGate, next.VerifyGate,
		next.CycleBaseCommit, next.ReviewRequired, prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	next.Version = prev.Version + 1
	if prev.Phase != next.Phase {
		if err := t.enterPhase(prev.ID, next.Phase, next.UpdatedAt); err != nil {
			return err
		}
	}
	if err := t.AppendEvent(work.EventV2{WorkID: prev.ID, Kind: kind, Actor: actor,
		PreviousVersion: prev.Version, NextVersion: next.Version, Payload: payload, At: next.UpdatedAt}); err != nil {
		return err
	}
	if left == "" {
		return nil
	}
	return t.withdrawDecision(left, next, actor)
}

// withdrawDecision withdraws the decision an item stopped waiting on, if it
// is still open, and records that on the item. A decision already answered,
// defaulted or withdrawn — the usual case when its own end cleared the item —
// is left as it is.
func (t *WorkV2Tx) withdrawDecision(id string, item work.ItemV2, actor string) error {
	p := t.participation()
	defer func() { t.wrote += p.wrote }()
	prev, err := p.Decision(id)
	if errors.Is(err, ErrNoDecision) {
		return nil
	}
	if err != nil {
		return err
	}
	next, ok := work.WithdrawDecision(prev, actor, item.UpdatedAt)
	if !ok {
		return nil
	}
	if err := p.PutDecision(next, &prev, 0); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"decision_id": id, "state": next.State})
	return t.AppendEvent(work.EventV2{WorkID: item.ID, Kind: "decision.withdrawn", Actor: actor,
		PreviousVersion: item.Version, NextVersion: item.Version, Payload: string(body), At: item.UpdatedAt})
}

// participation is the decisions register as this transaction sees it: the
// same SQL transaction, so a decision and the item that waits on it change
// together or not at all.
func (t *WorkV2Tx) participation() *WorkTx {
	return &WorkTx{ctx: t.ctx, tx: t.tx, s: t.s}
}

// Decision reads one decision inside this item transaction.
func (t *WorkV2Tx) Decision(id string) (work.Decision, error) {
	return t.participation().Decision(id)
}

// enterPhase counts one more entry into a phase. Every phase change passes
// through PutItem, so the occurrence is the item's own count of phase entries:
// a person who saw an item in deploying has not seen it after it went back to
// implementing and came to deploying again. An item that has not changed
// phase since this table existed has no row, and reads as nothing to accept:
// a store upgraded under a long Board must not raise every finished item at once.
func (t *WorkV2Tx) enterPhase(workID string, phase work.Phase, at time.Time) error {
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_phase_receipts (work_id, phase, occurrence, entered_at)
	      VALUES (?, ?, 1, ?)
	      ON CONFLICT(work_id) DO UPDATE SET phase=excluded.phase, occurrence=occurrence+1, entered_at=excluded.entered_at`,
		workID, phase, at.Unix()); err != nil {
		return err
	}
	t.wrote++
	return nil
}

// MarkWorkV2PhaseSeen records that the person opened an item while it showed
// phase. Only the occurrence the item is in now is marked, and only when it is
// still that phase: a view of deploying that answers after the item moved on
// says nothing about the phase it moved to. It answers whether the receipt
// was new; an already-seen, moved-on or never-entered occurrence is not an
// error, because opening an item is not a request that can fail.
func (s *Store) MarkWorkV2PhaseSeen(ctx context.Context, workID string, phase work.Phase, at time.Time) (bool, error) {
	marked := false
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		res, err := tx.tx.ExecContext(ctx, `UPDATE work_v2_phase_receipts SET seen_occurrence=occurrence, seen_at=?
		  WHERE work_id=? AND phase=? AND seen_occurrence<occurrence
		    AND EXISTS (SELECT 1 FROM work_v2_items i WHERE i.id=work_v2_phase_receipts.work_id AND i.phase=?)`,
			at.Unix(), workID, phase, phase)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		tx.wrote += n
		marked = n > 0
		return nil
	})
	return marked, err
}

// PendingAcceptance is the newest Board item a Session delivered that the
// person has not opened since it reached deploying or done, and how many such
// items that Session has.
type PendingAcceptance struct {
	WorkID    string
	Title     string
	Phase     work.Phase
	EnteredAt time.Time
	Count     int64
}

// WorkV2PendingAcceptanceForSessions answers, for each conversation, the
// item waiting for the person to look at it. A deploying item belongs to its
// owner_session; a done item has released its owner, so it belongs to the
// Session whose assignment was released when it closed, as in
// CompletedWorkV2ForSession. A conversation with nothing pending is absent
// from the answer; an error means none of them can be told apart from it.
// The answer is one row per conversation whatever the Board holds.
func (s *Store) WorkV2PendingAcceptanceForSessions(ctx context.Context, sessionIDs []string) (map[string]PendingAcceptance, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := map[string]PendingAcceptance{}
	ids := make([]string, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	// SQLite takes the bare columns of an aggregate query from the row that
	// gave MAX: the newest entry is the one named, the count covers the rest.
	rows, err := s.rd.QueryContext(ctx, `SELECT owner, id, title, phase, MAX(entered_at), COUNT(*) FROM (
		  SELECT i.owner_session AS owner, i.id, i.title, i.phase, r.entered_at
		  FROM work_v2_items i JOIN work_v2_phase_receipts r ON r.work_id=i.id AND r.phase=i.phase
		  WHERE i.phase='deploying' AND r.seen_occurrence<r.occurrence
		    AND i.owner_session IN (SELECT value FROM json_each(?1))
		  UNION ALL
		  SELECT a.session_id AS owner, i.id, i.title, i.phase, r.entered_at
		  FROM work_v2_items i JOIN work_v2_phase_receipts r ON r.work_id=i.id AND r.phase=i.phase
		  JOIN work_v2_assignments a ON a.work_id=i.id AND a.state='released' AND a.released_at=i.closed_at
		  WHERE i.phase='done' AND r.seen_occurrence<r.occurrence
		    AND a.session_id IN (SELECT value FROM json_each(?1))
		) GROUP BY owner`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var p PendingAcceptance
		var entered int64
		if err := rows.Scan(&owner, &p.WorkID, &p.Title, &p.Phase, &entered, &p.Count); err != nil {
			return nil, err
		}
		p.EnteredAt = time.Unix(entered, 0)
		out[owner] = p
	}
	return out, rows.Err()
}

// LatestEventOf returns the kind and actor of the newest event of one of the
// given kinds for one item, or empty strings when there is none. seq is the
// event ledger's insertion order.
func (t *WorkV2Tx) LatestEventOf(workID string, kinds ...string) (kind, actor string, err error) {
	if len(kinds) == 0 {
		return "", "", nil
	}
	args := []any{workID}
	for _, k := range kinds {
		args = append(args, k)
	}
	err = t.tx.QueryRowContext(t.ctx, `SELECT kind, actor FROM work_v2_events WHERE work_id=? AND kind IN (?`+
		strings.Repeat(",?", len(kinds)-1)+`) ORDER BY seq DESC LIMIT 1`, args...).Scan(&kind, &actor)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return kind, actor, err
}

func (t *WorkV2Tx) AppendEvent(e work.EventV2) error {
	if strings.TrimSpace(e.Payload) == "" {
		e.Payload = "{}"
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_events
      (work_id, kind, actor, previous_version, next_version, payload, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.WorkID, e.Kind, e.Actor, e.PreviousVersion, e.NextVersion, e.Payload, e.At.Unix()); err != nil {
		return err
	}
	t.wrote++
	return nil
}

func queryWorkV2(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, stmt string, args ...any) ([]work.ItemV2, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.ItemV2{}
	for rows.Next() {
		i, err := scanWorkV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Items(ctx context.Context, project, owner, status, search string, limit int) ([]work.ItemV2, bool, error) {
	return s.WorkV2ItemsPage(ctx, project, owner, status, search, 0, "", limit)
}

// WorkV2ItemsPage reads one newest-first keyset page. The last row from the
// preceding page is named by afterUpdated and afterID; an empty id is the first
// page. Rows that change after a page was read move to the front on the next
// fresh read instead of making an offset skip or duplicate unrelated rows.
func (s *Store) WorkV2ItemsPage(ctx context.Context, project, owner, status, search string,
	afterUpdated int64, afterID string, limit int) ([]work.ItemV2, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		return nil, false, fmt.Errorf("work v2 limit is required")
	}
	stmt := `SELECT ` + workV2Columns + ` FROM work_v2_items WHERE 1=1`
	args := []any{}
	if project != "" {
		stmt += ` AND project_id = ?`
		args = append(args, project)
	}
	if owner != "" {
		stmt += ` AND owner_session = ?`
		args = append(args, owner)
	}
	switch status {
	case "open":
		stmt += ` AND phase NOT IN ('done','cancelled')`
	case "done":
		stmt += ` AND phase = 'done'`
	case "all":
	default:
		return nil, false, fmt.Errorf("unknown work v2 status %q", status)
	}
	if search = strings.TrimSpace(search); search != "" {
		stmt += ` AND (instr(lower(title), lower(?)) > 0 OR instr(lower(description), lower(?)) > 0)`
		args = append(args, search, search)
	}
	if afterID != "" {
		stmt += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, afterUpdated, afterUpdated, afterID)
	}
	stmt += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	items, err := queryWorkV2(ctx, s.rd, stmt, args...)
	if len(items) > limit {
		return items[:limit], true, err
	}
	return items, false, err
}

// CompletedWorkV2ForSession keeps a Session's own completed responsibility
// visible after completion releases owner_session. The assignment released in
// the same transaction as closed_at identifies the completing Session; an
// older assignee must not be offered somebody else's completion to retract.
func (s *Store) CompletedWorkV2ForSession(ctx context.Context, session string, limit int) ([]work.ItemV2, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	if session == "" || limit <= 0 {
		return nil, false, fmt.Errorf("session and work v2 limit are required")
	}
	rows, err := queryWorkV2(ctx, s.rd, `SELECT `+workV2ItemColumns+` FROM work_v2_items i
    WHERE i.phase='done' AND EXISTS (
      SELECT 1 FROM work_v2_assignments a
      WHERE a.work_id=i.id AND a.session_id=? AND a.state='released' AND a.released_at=i.closed_at
    ) ORDER BY i.closed_at DESC, i.id DESC LIMIT ?`, session, limit+1)
	if len(rows) > limit {
		return rows[:limit], true, err
	}
	return rows, false, err
}

func scanAssignmentV2(sc scanner) (work.AssignmentV2, error) {
	var a work.AssignmentV2
	var created, updated int64
	var released sql.NullInt64
	var via string
	err := sc.Scan(&a.ID, &a.WorkID, &a.Mode, &a.SessionID, &a.TerminalID, &a.Assistant, &a.Model,
		&a.State, &a.HumanActor, &a.RootAssignment, &a.Failure, &created, &updated, &released, &via, &a.Persona,
		&a.GatePreviewed, &a.PlanningGate, &a.VerifyGate, &a.CycleBaseCommit)
	if err != nil {
		return a, err
	}
	a.CreatedAt, a.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	if released.Valid {
		a.ReleasedAt = time.Unix(released.Int64, 0)
	}
	if via != "" {
		var v work.CreatedViaV2
		// Kept out of the answer, as an item's created_via is, when unreadable.
		if json.Unmarshal([]byte(via), &v) == nil && v.Run != "" {
			a.ClaimedVia = &v
		}
	}
	return a, nil
}

const assignmentV2Columns = `id, work_id, mode, session_id, terminal_id, assistant, model, state,
  human_actor, root_assignment_id, failure, created_at, updated_at, released_at, claimed_via, persona,
  gate_previewed, planning_gate, verify_gate, cycle_base_commit`

// AssignmentsClaimedBy counts every assignment, active or released, a Session
// claimed on actor's word — how many items one person's message has already
// had claimed.
func (t *WorkV2Tx) AssignmentsClaimedBy(actor string) (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_assignments
    WHERE human_actor = ? AND claimed_via <> ''`, actor).Scan(&n)
	return n, err
}

func (t *WorkV2Tx) ActiveAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? AND state='active'`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

// PendingAssignment is any new-Session assignment still in flight, including
// the interval before its Root Assignment has an id or opens a dialog.
func (t *WorkV2Tx) PendingAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? AND state='assigning'
    ORDER BY rowid DESC LIMIT 1`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

// AwaitedAssignment is the item's pending assignment whose Feature Root is
// waiting on a dialog (WorkV2AwaitedAssignments); zero when there is none.
func (t *WorkV2Tx) AwaitedAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? AND state='assigning' AND root_assignment_id<>''
    ORDER BY rowid DESC LIMIT 1`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

// LatestAssignment returns the last assignment fact written for one item.
// rowid is the insertion order inside this append-only ledger; timestamps have
// one-second precision and cannot distinguish two assignments made together.
func (t *WorkV2Tx) LatestAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? ORDER BY rowid DESC LIMIT 1`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

func (t *WorkV2Tx) Assignment(id string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, errors.New("no such v2 assignment")
	}
	return a, err
}

func (t *WorkV2Tx) CreateAssignment(a work.AssignmentV2) error {
	if a.State == "assigning" {
		var n int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_assignments WHERE state='assigning'`).Scan(&n); err != nil {
			return err
		} else if n >= WorkV2AssignmentLimit {
			return errors.New("work v2 assignments full")
		}
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_assignments
      (id, work_id, mode, session_id, terminal_id, assistant, model, state, human_actor,
       root_assignment_id, failure, created_at, updated_at, released_at, claimed_via, persona,
	       gate_previewed, planning_gate, verify_gate, cycle_base_commit)
	      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkID, a.Mode, a.SessionID, a.TerminalID, a.Assistant, a.Model, a.State, a.HumanActor,
		a.RootAssignment, a.Failure, a.CreatedAt.Unix(), a.UpdatedAt.Unix(), zeroOrUnix(a.ReleasedAt),
		createdViaColumn(a.ClaimedVia), a.Persona, a.GatePreviewed, a.PlanningGate, a.VerifyGate, a.CycleBaseCommit)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) UpdateAssignment(a work.AssignmentV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_assignments SET session_id=?, terminal_id=?, assistant=?,
      model=?, state=?, root_assignment_id=?, failure=?, updated_at=?, released_at=?, gate_previewed=?,
	      planning_gate=?, verify_gate=?, cycle_base_commit=? WHERE id=?`,
		a.SessionID, a.TerminalID, a.Assistant, a.Model, a.State, a.RootAssignment, a.Failure,
		a.UpdatedAt.Unix(), zeroOrUnix(a.ReleasedAt), a.GatePreviewed, a.PlanningGate, a.VerifyGate,
		a.CycleBaseCommit, a.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return errors.New("no such v2 assignment")
	}
	t.wrote++
	return nil
}

// WorkV2Assignment reads one durable assignment by id. Activation uses it to
// retain the gate preview that was shown to a Root Assignment before a dialog
// or daemon restart.
func (s *Store) WorkV2Assignment(ctx context.Context, id string) (work.AssignmentV2, error) {
	if err := reading(); err != nil {
		return work.AssignmentV2{}, err
	}
	return scanAssignmentV2(s.rd.QueryRowContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE id=?`, id))
}

func (s *Store) WorkV2Assignments(ctx context.Context, workID string) ([]work.AssignmentV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? ORDER BY created_at, id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.AssignmentV2{}
	for rows.Next() {
		a, err := scanAssignmentV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// WorkV2AwaitedAssignments is every new-Session assignment still pending on a
// Feature Root that was opened and is waiting for a person to answer its first
// screen. Pending rows are bounded by WorkV2AssignmentLimit.
func (s *Store) WorkV2AwaitedAssignments(ctx context.Context) ([]work.AssignmentV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE state='assigning' AND root_assignment_id<>'' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.AssignmentV2{}
	for rows.Next() {
		a, err := scanAssignmentV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// WorkV2ActiveClaim is the person's message a Session claimed an item on,
// when the item's active assignment is such a claim; nil otherwise.
func (s *Store) WorkV2ActiveClaim(ctx context.Context, workID string) (*work.CreatedViaV2, error) {
	a, err := scanAssignmentV2(s.rd.QueryRowContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? AND state='active' AND claimed_via <> ''`, workID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a.ClaimedVia, err
}

// WorkV2Relations reads the bounded relation sets shown on a page of
// Board cards. Each table is read once for the whole page; callers do not
// multiply store round trips by the number of cards.
type WorkV2Relations struct {
	Documents map[string][]work.DocumentV2
	Images    map[string][]work.ImageV2
	Steps     map[string][]work.StepV2
	Claims    map[string]*work.CreatedViaV2
	Progress  map[string]WorkV2CardProgress
}

// WorkV2CardProgress is the latest phase transition's card-sized evidence.
// It is read from append-only events rather than the item's edit timestamp.
type WorkV2CardProgress struct {
	PhaseEnteredAt     int64
	DeploymentEvidence string
	NoDeploymentReason string
	// NoLandingReason is why the item's latest entry into deploying rested on
	// no landing (`--no-landing-reason`); empty when it rested on one. It is
	// read from that entry, not the latest event, so it outlives done.
	NoLandingReason string
}

func (s *Store) WorkV2Relations(ctx context.Context, workIDs []string) (WorkV2Relations, error) {
	out := WorkV2Relations{
		Documents: map[string][]work.DocumentV2{}, Images: map[string][]work.ImageV2{},
		Steps: map[string][]work.StepV2{}, Claims: map[string]*work.CreatedViaV2{},
		Progress: map[string]WorkV2CardProgress{},
	}
	if err := reading(); err != nil {
		return out, err
	}
	if len(workIDs) == 0 {
		return out, nil
	}
	list, err := json.Marshal(workIDs)
	if err != nil {
		return out, err
	}
	ids := string(list)
	progress, err := s.WorkV2CardProgress(ctx, workIDs)
	if err != nil {
		return out, err
	}
	out.Progress = progress

	documents, err := s.rd.QueryContext(ctx, `SELECT id,work_id,role,title,body,reference,position,version,created_at,updated_at
    FROM work_v2_documents WHERE work_id IN (SELECT value FROM json_each(?)) ORDER BY work_id,position,id`, ids)
	if err != nil {
		return out, err
	}
	for documents.Next() {
		var d work.DocumentV2
		var created, updated int64
		if err := documents.Scan(&d.ID, &d.WorkID, &d.Role, &d.Title, &d.Body, &d.Reference, &d.Position,
			&d.Version, &created, &updated); err != nil {
			documents.Close()
			return out, err
		}
		d.CreatedAt, d.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out.Documents[d.WorkID] = append(out.Documents[d.WorkID], d)
	}
	if err := documents.Err(); err != nil {
		documents.Close()
		return out, err
	}
	if err := documents.Close(); err != nil {
		return out, err
	}

	images, err := s.rd.QueryContext(ctx, `SELECT `+workV2ImageColumns+`
    FROM work_v2_images WHERE work_id IN (SELECT value FROM json_each(?)) ORDER BY work_id,position,id`, ids)
	if err != nil {
		return out, err
	}
	for images.Next() {
		i, err := scanWorkV2Image(images)
		if err != nil {
			images.Close()
			return out, err
		}
		out.Images[i.WorkID] = append(out.Images[i.WorkID], i)
	}
	if err := images.Err(); err != nil {
		images.Close()
		return out, err
	}
	if err := images.Close(); err != nil {
		return out, err
	}

	steps, err := s.rd.QueryContext(ctx, `SELECT id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version
    FROM work_v2_steps WHERE work_id IN (SELECT value FROM json_each(?)) ORDER BY work_id,position,id`, ids)
	if err != nil {
		return out, err
	}
	for steps.Next() {
		var st work.StepV2
		var done bool
		var created int64
		var completed sql.NullInt64
		if err := steps.Scan(&st.ID, &st.WorkID, &st.Title, &done, &st.Position, &st.CreatedBy,
			&st.CompletedBy, &created, &completed, &st.Version); err != nil {
			steps.Close()
			return out, err
		}
		st.Done, st.CreatedAt = done, time.Unix(created, 0)
		if completed.Valid {
			st.CompletedAt = time.Unix(completed.Int64, 0)
		}
		out.Steps[st.WorkID] = append(out.Steps[st.WorkID], st)
	}
	if err := steps.Err(); err != nil {
		steps.Close()
		return out, err
	}
	if err := steps.Close(); err != nil {
		return out, err
	}

	claims, err := s.rd.QueryContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id IN (SELECT value FROM json_each(?))
      AND state='active' AND claimed_via <> '' ORDER BY work_id,created_at,id`, ids)
	if err != nil {
		return out, err
	}
	for claims.Next() {
		a, err := scanAssignmentV2(claims)
		if err != nil {
			claims.Close()
			return out, err
		}
		if _, exists := out.Claims[a.WorkID]; !exists {
			out.Claims[a.WorkID] = a.ClaimedVia
		}
	}
	if err := claims.Err(); err != nil {
		claims.Close()
		return out, err
	}
	if err := claims.Close(); err != nil {
		return out, err
	}
	return out, nil
}

// WorkV2CardProgress reads one latest phase event per item in a bounded page.
func (s *Store) WorkV2CardProgress(ctx context.Context, workIDs []string) (map[string]WorkV2CardProgress, error) {
	out := map[string]WorkV2CardProgress{}
	if err := reading(); err != nil {
		return nil, err
	}
	if len(workIDs) == 0 {
		return out, nil
	}
	list, err := json.Marshal(workIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `WITH latest AS (
		SELECT work_id, MAX(seq) AS seq FROM work_v2_events
		WHERE work_id IN (SELECT value FROM json_each(?)) AND kind='item.phase_changed'
		GROUP BY work_id
	), deploying AS (
		SELECT work_id, MAX(seq) AS seq FROM work_v2_events
		WHERE work_id IN (SELECT value FROM json_each(?)) AND kind='item.phase_changed'
		  AND json_valid(payload) AND json_extract(payload, '$.to')='deploying'
		GROUP BY work_id
	)
	SELECT e.work_id, e.at, e.payload, COALESCE((SELECT json_extract(d.payload, '$.no_landing_reason')
	  FROM work_v2_events d JOIN deploying ON deploying.seq=d.seq WHERE deploying.work_id=e.work_id), '')
	FROM work_v2_events e
	JOIN latest ON latest.seq=e.seq`, string(list), string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw, noLanding string
		var at int64
		if err := rows.Scan(&id, &at, &raw, &noLanding); err != nil {
			return nil, err
		}
		var change struct {
			Deployment         string `json:"deployment"`
			NoDeploymentReason string `json:"no_deployment_reason"`
		}
		if err := json.Unmarshal([]byte(raw), &change); err != nil {
			return nil, err
		}
		out[id] = WorkV2CardProgress{PhaseEnteredAt: at, DeploymentEvidence: change.Deployment,
			NoDeploymentReason: change.NoDeploymentReason, NoLandingReason: noLanding}
	}
	return out, rows.Err()
}

// WorkV2RootAssignmentsForSessions answers, for each of one assistant's
// conversations, the Root Assignment that opened it from a Board item, in one
// read. Existing-session assignments do not rename a conversation, and failed
// openings carry no usable session id. An empty projectPath matches every
// project: a conversation id already names one conversation, and a live row
// has no place, only a working directory that is where the process is now.
func (s *Store) WorkV2RootAssignmentsForSessions(ctx context.Context, projectPath, assistant string, sessionIDs []string) (map[string]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := map[string]string{}
	ids := make([]string, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if strings.TrimSpace(assistant) == "" || len(ids) == 0 {
		return out, nil
	}
	// One JSON parameter, so the number of conversations on screen is not a
	// number of SQL variables.
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT a.session_id, a.root_assignment_id
		FROM work_v2_assignments a JOIN work_v2_items i ON i.id=a.work_id
		WHERE a.session_id IN (SELECT value FROM json_each(?)) AND a.assistant=? AND (?='' OR i.project_path=?)
		  AND a.mode='new_session' AND a.root_assignment_id<>'' AND a.state IN ('active','released')
		ORDER BY a.updated_at DESC, a.rowid DESC`, string(list), assistant, projectPath, projectPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var session, rootAssignment string
		if err := rows.Scan(&session, &rootAssignment); err != nil {
			return nil, err
		}
		if _, seen := out[session]; !seen {
			out[session] = rootAssignment
		}
	}
	return out, rows.Err()
}

// EpicRootParent is the stable visual relationship recorded when an Epic
// owner assigned a child item to a new, independently owned Root Session.
// The owner's conversation is taken from the assignment actor, not the
// Epic's current owner, which may change after this Root was opened.
type EpicRootParent struct {
	OwnerSession string
	EpicID       string
}

func (s *Store) WorkV2EpicRootParentsForSessions(ctx context.Context, assistant string, sessionIDs []string) (map[string]EpicRootParent, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := map[string]EpicRootParent{}
	if assistant == "" || len(sessionIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(sessionIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT a.session_id, a.human_actor, i.parent_id
		FROM work_v2_assignments a JOIN work_v2_items i ON i.id=a.work_id
		JOIN work_v2_items epic ON epic.id=i.parent_id AND epic.kind='epic' AND epic.project_id=i.project_id
		WHERE a.session_id IN (SELECT value FROM json_each(?)) AND a.assistant=?
		  AND a.mode='new_session' AND a.root_assignment_id<>''
		  AND a.state IN ('active','released') AND i.parent_id<>''
		  AND substr(a.human_actor,1,?)=?
		ORDER BY a.updated_at DESC, a.rowid DESC`, string(ids), assistant, len(work.ActorEpicOwner), work.ActorEpicOwner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID, actor, epicID string
		if err := rows.Scan(&sessionID, &actor, &epicID); err != nil {
			return nil, err
		}
		owner := strings.TrimPrefix(actor, work.ActorEpicOwner)
		if owner != "" && owner != sessionID {
			if _, seen := out[sessionID]; !seen {
				out[sessionID] = EpicRootParent{OwnerSession: owner, EpicID: epicID}
			}
		}
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Events(ctx context.Context, workID string, after int64, limit int) ([]work.EventV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT seq, work_id, kind, actor, previous_version, next_version, payload, at
    FROM work_v2_events WHERE work_id=? AND seq>? ORDER BY seq LIMIT ?`, workID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.EventV2{}
	for rows.Next() {
		var e work.EventV2
		var at int64
		if err := rows.Scan(&e.Seq, &e.WorkID, &e.Kind, &e.Actor, &e.PreviousVersion, &e.NextVersion, &e.Payload, &at); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) AddDocument(d work.DocumentV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_documents WHERE work_id=?`, d.WorkID).Scan(&n); err != nil {
		return err
	} else if n >= WorkV2DocumentLimit {
		return ErrWorkV2DocsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_documents
    (id,work_id,role,title,body,reference,position,version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.WorkID, d.Role, d.Title, d.Body, d.Reference, d.Position, d.Version, d.CreatedAt.Unix(), d.UpdatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

// PlanDocuments includes the review boundary declarations in the same
// insertion order as plans and reviews. Seconds tie, so rowid breaks it. A
// plan_review carries its reference, the review task whose receipt the
// planning gate reads.
func (t *WorkV2Tx) PlanDocuments(workID string) ([]work.DocumentV2, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT role, title, body, reference, created_at FROM work_v2_documents
    WHERE work_id=? AND (role IN ('plan','plan_review') OR
      (role='other' AND title=?)) ORDER BY created_at, rowid`, workID,
		work.ReviewBoundaryTitle)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.DocumentV2{}
	for rows.Next() {
		var d work.DocumentV2
		var created int64
		if err := rows.Scan(&d.Role, &d.Title, &d.Body, &d.Reference, &created); err != nil {
			return nil, err
		}
		d.WorkID, d.CreatedAt = workID, time.Unix(created, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// PlanReviewDocument is the plan_review an item already holds for one review
// task, read inside the transaction that would otherwise add a second. The
// earliest wins when an older store holds duplicates.
func (t *WorkV2Tx) PlanReviewDocument(workID, reference string) (work.DocumentV2, bool, error) {
	var d work.DocumentV2
	var created, updated int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,work_id,role,title,body,reference,position,version,created_at,updated_at
    FROM work_v2_documents WHERE work_id=? AND role='plan_review' AND reference=? ORDER BY created_at,rowid LIMIT 1`,
		workID, reference).Scan(&d.ID, &d.WorkID, &d.Role, &d.Title, &d.Body, &d.Reference, &d.Position,
		&d.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return work.DocumentV2{}, false, nil
	}
	if err != nil {
		return work.DocumentV2{}, false, err
	}
	d.CreatedAt, d.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return d, true, nil
}

// BrokerTask reads one broker task inside a work transaction: the store has
// one connection, so a read beside the transaction would wait on it forever.
func (t *WorkV2Tx) BrokerTask(id string) (BrokerRow, error) {
	return scanBroker(t.tx.QueryRowContext(t.ctx, `SELECT `+brokerColumns+` FROM broker_tasks WHERE id = ?`, id))
}

func (s *Store) WorkV2Documents(ctx context.Context, workID string) ([]work.DocumentV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT id,work_id,role,title,body,reference,position,version,created_at,updated_at
    FROM work_v2_documents WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.DocumentV2{}
	for rows.Next() {
		var d work.DocumentV2
		var created, updated int64
		if err := rows.Scan(&d.ID, &d.WorkID, &d.Role, &d.Title, &d.Body, &d.Reference, &d.Position,
			&d.Version, &created, &updated); err != nil {
			return nil, err
		}
		d.CreatedAt, d.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) AddImage(i work.ImageV2, data []byte) error {
	var count, itemBytes, totalBytes int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*),COALESCE(SUM(byte_count),0)
    FROM work_v2_images WHERE work_id=?`, i.WorkID).Scan(&count, &itemBytes); err != nil {
		return err
	}
	if count >= WorkV2ImageLimit {
		return ErrWorkV2ImagesFull
	}
	if int64(len(data)) > WorkV2ImageItemLimit-itemBytes {
		return ErrWorkV2ImageBytesFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT
    COALESCE((SELECT SUM(byte_count) FROM work_v2_images),0) +
    COALESCE((SELECT SUM(byte_count) FROM session_direct_todo_images),0)`).Scan(&totalBytes); err != nil {
		return err
	}
	if int64(len(data)) > WorkV2ImageTotalLimit-totalBytes {
		return ErrWorkV2ImageBytesFull
	}
	sum, err := t.addImageFile(imageRef{i.ID, i.MediaType}, data)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_images
    (id,work_id,title,media_type,sha256,byte_count,width,height,position,created_by,created_at)
    VALUES (?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.WorkID, i.Title, i.MediaType, sum, len(data), i.Width,
		i.Height, i.Position, i.CreatedBy, i.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

// addImageFile writes a picture's file before its row is inserted, and
// remembers it so a transaction that does not commit takes it back.
func (t *WorkV2Tx) addImageFile(r imageRef, data []byte) (string, error) {
	sum, err := t.s.images.write(r, data)
	if err != nil {
		return "", err
	}
	t.added = append(t.added, r)
	return sum, nil
}

func scanWorkV2Image(sc scanner) (work.ImageV2, error) {
	var i work.ImageV2
	var created int64
	err := sc.Scan(&i.ID, &i.WorkID, &i.Title, &i.MediaType, &i.ByteCount, &i.Width, &i.Height,
		&i.Position, &i.CreatedBy, &created)
	if err != nil {
		return i, err
	}
	i.CreatedAt = time.Unix(created, 0)
	return i, nil
}

const workV2ImageColumns = `id,work_id,title,media_type,byte_count,width,height,position,created_by,created_at`

func (t *WorkV2Tx) Image(id string) (work.ImageV2, error) {
	return scanWorkV2Image(t.tx.QueryRowContext(t.ctx, `SELECT `+workV2ImageColumns+` FROM work_v2_images WHERE id=?`, id))
}

func (t *WorkV2Tx) DeleteImage(id string) error {
	r := imageRef{id: id}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT media_type FROM work_v2_images WHERE id=?`, id).Scan(&r.mediaType); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM work_v2_images WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return sql.ErrNoRows
	}
	t.wrote++
	t.dropped = append(t.dropped, r)
	return nil
}

func (s *Store) WorkV2Images(ctx context.Context, workID string) ([]work.ImageV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT `+workV2ImageColumns+`
    FROM work_v2_images WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.ImageV2{}
	for rows.Next() {
		i, err := scanWorkV2Image(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// WorkV2ImageBytes returns a durable Board or Session-to-do reference image
// without exposing the database or the picture files to the HTTP layer.
//
// The media type is the one recorded with the row: image/png, or image/jpeg
// for a photograph stored since photographs stayed JPEG. A row whose file is
// gone or is not the bytes it recorded answers ErrReferenceImageMissing or
// ErrReferenceImageMismatch, with ok true: the image exists and cannot be read.
func (s *Store) WorkV2ImageBytes(ctx context.Context, id string) ([]byte, string, bool, error) {
	if err := reading(); err != nil {
		return nil, "", false, err
	}
	r, byteCount, sum, ok, err := s.referenceImage(ctx, id)
	if !ok || err != nil {
		return nil, "", ok, err
	}
	data, err := s.images.read(r, byteCount, sum)
	return data, r.mediaType, true, err
}

func (t *WorkV2Tx) AddStep(s work.StepV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_steps WHERE work_id=?`, s.WorkID).Scan(&n); err != nil {
		return err
	} else if n >= WorkV2StepLimit {
		return ErrWorkV2StepsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_steps
    (id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version)
    VALUES (?,?,?,?,?,?,?,?,?,?)`, s.ID, s.WorkID, s.Title, s.Done, s.Position, s.CreatedBy, s.CompletedBy,
		s.CreatedAt.Unix(), zeroOrUnix(s.CompletedAt), s.Version)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) Step(id string) (work.StepV2, error) {
	var st work.StepV2
	var done bool
	var created int64
	var completed sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version
    FROM work_v2_steps WHERE id=?`, id).Scan(&st.ID, &st.WorkID, &st.Title, &done, &st.Position,
		&st.CreatedBy, &st.CompletedBy, &created, &completed, &st.Version)
	if err != nil {
		return st, err
	}
	st.Done, st.CreatedAt = done, time.Unix(created, 0)
	if completed.Valid {
		st.CompletedAt = time.Unix(completed.Int64, 0)
	}
	return st, nil
}

func (t *WorkV2Tx) Steps(workID string) ([]work.StepV2, error) {
	return queryWorkV2Steps(t.ctx, t.tx, workID)
}

func (t *WorkV2Tx) PutStep(prev, next work.StepV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_steps SET title=?,done=?,position=?,completed_by=?,completed_at=?,version=version+1
    WHERE id=? AND version=?`, next.Title, next.Done, next.Position, next.CompletedBy, zeroOrUnix(next.CompletedAt), prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Steps(ctx context.Context, workID string) ([]work.StepV2, error) {
	return queryWorkV2Steps(ctx, s.rd, workID)
}

func queryWorkV2Steps(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, workID string) ([]work.StepV2, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version
    FROM work_v2_steps WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.StepV2{}
	for rows.Next() {
		var st work.StepV2
		var done bool
		var created int64
		var completed sql.NullInt64
		if err := rows.Scan(&st.ID, &st.WorkID, &st.Title, &done, &st.Position, &st.CreatedBy,
			&st.CompletedBy, &created, &completed, &st.Version); err != nil {
			return nil, err
		}
		st.Done, st.CreatedAt = done, time.Unix(created, 0)
		if completed.Valid {
			st.CompletedAt = time.Unix(completed.Int64, 0)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanDirectTodoV2(sc scanner) (work.DirectTodoV2, error) {
	var td work.DirectTodoV2
	var created int64
	var sent, read, completed sql.NullInt64
	err := sc.Scan(&td.ID, &td.SessionID, &td.Text, &td.CreatedBy, &created, &sent, &read, &completed,
		&td.CompletedBy, &td.Version)
	if err == sql.ErrNoRows {
		return td, errors.New("no such direct todo")
	}
	if err != nil {
		return td, err
	}
	td.CreatedAt = time.Unix(created, 0)
	if sent.Valid {
		td.SentAt = time.Unix(sent.Int64, 0)
	}
	if read.Valid {
		td.ReadAt = time.Unix(read.Int64, 0)
	}
	if completed.Valid {
		td.CompletedAt = time.Unix(completed.Int64, 0)
	}
	return td, nil
}

const directTodoV2Columns = `id,session_id,text,created_by,created_at,sent_at,read_at,completed_at,completed_by,version`

func (t *WorkV2Tx) CreateDirectTodo(td work.DirectTodoV2) error {
	return t.CreateDirectTodos([]work.DirectTodoV2{td})
}

// CreateDirectTodos adds every row or none: the open-row bound is checked
// against the whole batch before anything is written, so a batch that would
// cross it is refused whole. Rows keep the order they are given in; rows
// created in the same second are read back in insertion (rowid) order.
func (t *WorkV2Tx) CreateDirectTodos(tds []work.DirectTodoV2) error {
	if len(tds) == 0 {
		return nil
	}
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM session_direct_todos
    WHERE session_id=? AND completed_at IS NULL`, tds[0].SessionID).Scan(&n); err != nil {
		return err
	} else if n+int64(len(tds)) > DirectTodoV2Limit {
		return ErrDirectTodoFull
	}
	for _, td := range tds {
		if td.SessionID != tds[0].SessionID {
			return errors.New("a direct to-do batch names one Session")
		}
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO session_direct_todos
    (id,session_id,text,created_by,created_at,read_at,version) VALUES (?,?,?,?,?,?,1)`,
			td.ID, td.SessionID, td.Text, td.CreatedBy, td.CreatedAt.Unix(), zeroOrUnix(td.ReadAt)); err != nil {
			return err
		}
		t.wrote++
	}
	return nil
}

func (t *WorkV2Tx) AddDirectTodoImage(i work.DirectTodoImageV2, data []byte) error {
	var count, todoBytes, totalBytes int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*),COALESCE(SUM(byte_count),0)
    FROM session_direct_todo_images WHERE todo_id=?`, i.TodoID).Scan(&count, &todoBytes); err != nil {
		return err
	}
	if count >= WorkV2ImageLimit {
		return ErrDirectTodoImagesFull
	}
	if int64(len(data)) > WorkV2ImageItemLimit-todoBytes {
		return ErrDirectTodoImageBytesFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT
    COALESCE((SELECT SUM(byte_count) FROM work_v2_images),0) +
    COALESCE((SELECT SUM(byte_count) FROM session_direct_todo_images),0)`).Scan(&totalBytes); err != nil {
		return err
	}
	if int64(len(data)) > WorkV2ImageTotalLimit-totalBytes {
		return ErrDirectTodoImageBytesFull
	}
	sum, err := t.addImageFile(imageRef{i.ID, i.MediaType}, data)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO session_direct_todo_images
    (id,todo_id,title,media_type,sha256,byte_count,width,height,position,created_by,created_at)
    VALUES (?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.TodoID, i.Title, i.MediaType, sum, len(data), i.Width,
		i.Height, i.Position, i.CreatedBy, i.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func scanDirectTodoImageV2(sc scanner) (work.DirectTodoImageV2, error) {
	var i work.DirectTodoImageV2
	var created int64
	err := sc.Scan(&i.ID, &i.TodoID, &i.Title, &i.MediaType, &i.ByteCount, &i.Width, &i.Height,
		&i.Position, &i.CreatedBy, &created)
	if err != nil {
		return i, err
	}
	i.CreatedAt = time.Unix(created, 0)
	return i, nil
}

const directTodoImageV2Columns = `id,todo_id,title,media_type,byte_count,width,height,position,created_by,created_at`

func (s *Store) DirectTodoV2Images(ctx context.Context, todoID string) ([]work.DirectTodoImageV2, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT `+directTodoImageV2Columns+`
    FROM session_direct_todo_images WHERE todo_id=? ORDER BY position,id`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.DirectTodoImageV2{}
	for rows.Next() {
		i, err := scanDirectTodoImageV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

type DirectTodoImagePayload struct {
	Image work.DirectTodoImageV2
	Data  []byte
}

// DirectTodoV2ImagePayloads is a to-do's pictures with their bytes, read from
// their files after the rows: one missing or mismatched file refuses the whole
// set (ErrReferenceImageMissing, ErrReferenceImageMismatch), since a to-do
// typed with a picture left out is not the to-do the person wrote.
func (s *Store) DirectTodoV2ImagePayloads(ctx context.Context, todoID string) ([]DirectTodoImagePayload, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+directTodoImageV2Columns+`,sha256
    FROM session_direct_todo_images WHERE todo_id=? ORDER BY position,id`, todoID)
	if err != nil {
		return nil, err
	}
	out := []DirectTodoImagePayload{}
	var sums []string
	for rows.Next() {
		var p DirectTodoImagePayload
		var created int64
		var sum string
		if err := rows.Scan(&p.Image.ID, &p.Image.TodoID, &p.Image.Title, &p.Image.MediaType, &p.Image.ByteCount,
			&p.Image.Width, &p.Image.Height, &p.Image.Position, &p.Image.CreatedBy, &created, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		p.Image.CreatedAt = time.Unix(created, 0)
		out = append(out, p)
		sums = append(sums, sum)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		data, err := s.images.read(imageRef{out[i].Image.ID, out[i].Image.MediaType}, out[i].Image.ByteCount, sums[i])
		if err != nil {
			return nil, err
		}
		out[i].Data = data
	}
	return out, nil
}

func (t *WorkV2Tx) DirectTodo(id string) (work.DirectTodoV2, error) {
	return scanDirectTodoV2(t.tx.QueryRowContext(t.ctx, `SELECT `+directTodoV2Columns+`
    FROM session_direct_todos WHERE id=?`, id))
}

func (t *WorkV2Tx) PutDirectTodo(prev, next work.DirectTodoV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE session_direct_todos SET sent_at=?,read_at=?,completed_at=?,
    completed_by=?,version=version+1 WHERE id=? AND version=?`, zeroOrUnix(next.SentAt), zeroOrUnix(next.ReadAt),
		zeroOrUnix(next.CompletedAt), next.CompletedBy, prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

// DeleteDirectTodo deletes a to-do, and its pictures' rows with it by the
// cascade; their files go once the delete commits.
func (t *WorkV2Tx) DeleteDirectTodo(id, session string) (bool, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT i.id,i.media_type FROM session_direct_todo_images i
    JOIN session_direct_todos d ON d.id=i.todo_id WHERE d.id=? AND d.session_id=?`, id, session)
	if err != nil {
		return false, err
	}
	var pictures []imageRef
	for rows.Next() {
		var r imageRef
		if err := rows.Scan(&r.id, &r.mediaType); err != nil {
			rows.Close()
			return false, err
		}
		pictures = append(pictures, r)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM session_direct_todos WHERE id=? AND session_id=?`, id, session)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err == nil && n > 0 {
		t.wrote++
		t.dropped = append(t.dropped, pictures...)
	}
	return n > 0, err
}

func (s *Store) DirectTodosV2(ctx context.Context, session string, includeCompleted bool, markRead bool, limit int) ([]work.DirectTodoV2, bool, error) {
	var out []work.DirectTodoV2
	var truncated bool
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		stmt := `SELECT ` + directTodoV2Columns + ` FROM session_direct_todos WHERE session_id=?`
		if !includeCompleted {
			stmt += ` AND completed_at IS NULL`
			stmt += ` ORDER BY created_at,rowid LIMIT ?`
		} else {
			// Open work remains the actionable prefix in creation order. Completed
			// rows follow newest first so the bounded person view keeps the most
			// useful confirmations without hiding an old open row.
			stmt += ` ORDER BY completed_at IS NOT NULL,
        CASE WHEN completed_at IS NULL THEN created_at END,
        CASE WHEN completed_at IS NOT NULL THEN completed_at END DESC,rowid LIMIT ?`
		}
		rows, err := tx.tx.QueryContext(ctx, stmt, session, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			td, err := scanDirectTodoV2(rows)
			if err != nil {
				return err
			}
			out = append(out, td)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) > limit {
			out, truncated = out[:limit], true
		}
		if !markRead {
			return nil
		}
		now := time.Now().Unix()
		res, err := tx.tx.ExecContext(ctx, `UPDATE session_direct_todos SET read_at=?,version=version+1
      WHERE session_id=? AND completed_at IS NULL AND read_at IS NULL`, now, session)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			tx.wrote += n
			for n := range out {
				if out[n].ReadAt.IsZero() {
					out[n].ReadAt = time.Unix(now, 0)
					out[n].Version++
				}
			}
		}
		return nil
	})
	return out, truncated, err
}

func (s *Store) WorkV2Counts(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for name, q := range map[string]string{
		"items": `SELECT COUNT(*) FROM work_v2_items`, "assignments": `SELECT COUNT(*) FROM work_v2_assignments`,
		"documents": `SELECT COUNT(*) FROM work_v2_documents`, "steps": `SELECT COUNT(*) FROM work_v2_steps`,
		"images": `SELECT COUNT(*) FROM work_v2_images`, "todo_images": `SELECT COUNT(*) FROM session_direct_todo_images`,
		"events": `SELECT COUNT(*) FROM work_v2_events`, "direct_todos": `SELECT COUNT(*) FROM session_direct_todos`,
		"proposals": `SELECT COUNT(*) FROM work_v2_proposals`,
	} {
		var n int64
		if err := s.rd.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

// WorkV2ProjectCount is the complete item count and the same open count the
// Board's "open" filter calls in progress. Keeping this aggregation in one
// store read lets the Projects catalog summarize every Project without one
// Board request per row.
type WorkV2ProjectCount struct {
	Items int64
	Open  int64
}

// WorkV2ProjectCounts groups the current work-system items by their canonical
// Project path. The start-place id stored by Work System v2 and the retired
// catalog's Project id use different namespaces, while both retain this path.
// Terminal work remains in Items, while Open excludes done and cancelled
// exactly as WorkV2Items(status="open") does.
func (s *Store) WorkV2ProjectCounts(ctx context.Context) (map[string]WorkV2ProjectCount, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT project_path,COUNT(*),
    SUM(CASE WHEN phase NOT IN ('done','cancelled') THEN 1 ELSE 0 END)
    FROM work_v2_items GROUP BY project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WorkV2ProjectCount{}
	for rows.Next() {
		var projectPath string
		var count WorkV2ProjectCount
		if err := rows.Scan(&projectPath, &count.Items, &count.Open); err != nil {
			return nil, err
		}
		out[projectPath] = count
	}
	return out, rows.Err()
}

func scanProposalV2(sc scanner) (work.ProposalV2, error) {
	var p work.ProposalV2
	var created int64
	var resolved sql.NullInt64
	err := sc.Scan(&p.ID, &p.ProjectID, &p.ProjectPath, &p.Kind, &p.Title, &p.Description, &p.Reason,
		&p.SuggestedAcceptance, &p.SessionID, &p.SourceWorkID, &p.SourceTodoID, &p.State,
		&p.AcceptedWorkID, &created, &resolved, &p.Version)
	if err != nil {
		return p, err
	}
	p.CreatedAt = time.Unix(created, 0)
	if resolved.Valid {
		p.ResolvedAt = time.Unix(resolved.Int64, 0)
	}
	return p, nil
}

const proposalV2Columns = `id,project_id,project_path,kind,title,description,reason,suggested_acceptance,
session_id,source_work_id,source_todo_id,state,accepted_work_id,created_at,resolved_at,version`

func (t *WorkV2Tx) CreateProposal(p work.ProposalV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_proposals WHERE state='pending'`).Scan(&n); err != nil {
		return err
	}
	if n >= WorkV2ProposalLimit {
		return ErrWorkV2ProposalsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_proposals
    (id,project_id,project_path,kind,title,description,reason,suggested_acceptance,session_id,source_work_id,source_todo_id,state,accepted_work_id,created_at,version)
    VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, p.ID, p.ProjectID, p.ProjectPath, p.Kind, p.Title, p.Description,
		p.Reason, p.SuggestedAcceptance, p.SessionID, p.SourceWorkID, p.SourceTodoID, p.State, p.AcceptedWorkID, p.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) Proposal(id string) (work.ProposalV2, error) {
	return scanProposalV2(t.tx.QueryRowContext(t.ctx, `SELECT `+proposalV2Columns+` FROM work_v2_proposals WHERE id=?`, id))
}

func (t *WorkV2Tx) ResolveProposal(prev work.ProposalV2, state, accepted string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_proposals SET state=?,accepted_work_id=?,resolved_at=?,version=version+1 WHERE id=? AND version=?`,
		state, accepted, at.Unix(), prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Proposals(ctx context.Context, state string, limit int) ([]work.ProposalV2, bool, error) {
	query := `SELECT ` + proposalV2Columns + ` FROM work_v2_proposals`
	args := []any{}
	if state != "" {
		query += ` WHERE state=?`
		args = append(args, state)
	}
	query += ` ORDER BY created_at,id LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.rd.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []work.ProposalV2{}
	for rows.Next() {
		p, err := scanProposalV2(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	return out, truncated, nil
}

// WorkV2CapacityCounts returns the bounded cardinalities used by diagnostics.
// Per-owner bounds report the fullest owner rather than the total table.
func (s *Store) WorkV2CapacityCounts(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	queries := map[string]string{
		"open":               `SELECT COUNT(*) FROM work_v2_items WHERE closed_at IS NULL`,
		"planning":           `SELECT COUNT(*) FROM work_v2_items WHERE kind IN ('refactor','plan') AND closed_at IS NULL`,
		"assignments":        `SELECT COUNT(*) FROM work_v2_assignments`,
		"documents_per_item": `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM work_v2_documents GROUP BY work_id)`,
		"images_per_item": `SELECT COALESCE(MAX(n),0) FROM (
      SELECT COUNT(*) n FROM work_v2_images GROUP BY work_id
      UNION ALL SELECT COUNT(*) n FROM session_direct_todo_images GROUP BY todo_id)`,
		"image_bytes_max": `SELECT COALESCE(MAX(byte_count),0) FROM (
      SELECT byte_count FROM work_v2_images UNION ALL SELECT byte_count FROM session_direct_todo_images)`,
		"image_bytes_per_item": `SELECT COALESCE(MAX(n),0) FROM (
      SELECT SUM(byte_count) n FROM work_v2_images GROUP BY work_id
      UNION ALL SELECT SUM(byte_count) n FROM session_direct_todo_images GROUP BY todo_id)`,
		"image_bytes_total": `SELECT COALESCE(SUM(byte_count),0) FROM (
      SELECT byte_count FROM work_v2_images UNION ALL SELECT byte_count FROM session_direct_todo_images)`,
		"steps_per_item":         `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM work_v2_steps GROUP BY work_id)`,
		"direct_todos":           `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM session_direct_todos WHERE completed_at IS NULL GROUP BY session_id)`,
		"proposals":              `SELECT COUNT(*) FROM work_v2_proposals WHERE state='pending'`,
		"root_landings_per_item": `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM broker_root_landings GROUP BY work_id)`,
	}
	for name, query := range queries {
		var n int64
		if err := s.rd.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

// ResetWorkV1 deletes only the daemon's v1 work-system tables. It is never
// called by Open or migrate: the administrative HTTP/CLI boundary must first
// show and bind a dry-run confirmation to these counts.
func (s *Store) WorkV1Counts(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, table := range []string{"proposals", "decisions", "digests", "todos", "moves", "board_items", "backlog", "work"} {
		var exists int
		if err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		var n int64
		if err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			return nil, err
		}
		counts[table] = n
	}
	return counts, nil
}

func (s *Store) ResetWorkV1(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		// V1 protected its move history from ordinary deletion. This explicit
		// administrative transaction removes and restores that guard around the
		// exact reset; no other writer can enter between BEGIN IMMEDIATE and
		// commit.
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS moves_no_delete`); err != nil {
			return 0, err
		}
		tables := []string{"proposals", "decisions", "digests", "todos", "moves", "board_items", "backlog", "work"}
		var wrote int64
		for _, table := range tables {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
				return 0, err
			}
			if exists == 0 {
				continue
			}
			var n int64
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
				return 0, err
			}
			counts[table] = n
			res, err := tx.ExecContext(ctx, `DELETE FROM `+table)
			if err != nil {
				return 0, fmt.Errorf("clear %s: %w", table, err)
			}
			if changed, _ := res.RowsAffected(); changed > 0 {
				wrote += changed
			}
		}
		if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS moves_no_delete BEFORE DELETE ON moves
          BEGIN SELECT RAISE(ABORT, 'moves_append_only'); END`); err != nil {
			return 0, err
		}
		return wrote, nil
	})
	return counts, err
}
