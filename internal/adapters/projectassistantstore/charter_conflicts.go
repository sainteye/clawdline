package projectassistantstore

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Charter conflict reports, one row per Bot, charter version and Bot
// sentence. A report is open until the person handles it or the Bot checks a
// newer version first. resolve_* keep the person's choice and the key it was
// made with, so a repeated choice finishes the same plan.
const charterConflictsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_conflicts (
 machine TEXT NOT NULL, project TEXT NOT NULL, id TEXT NOT NULL, bot TEXT NOT NULL, turn TEXT NOT NULL,
 version INTEGER NOT NULL, source TEXT NOT NULL, path TEXT NOT NULL, memory_entry TEXT NOT NULL,
 bot_sentence TEXT NOT NULL, charter_sentence TEXT NOT NULL, section TEXT NOT NULL, suggested TEXT NOT NULL,
 state TEXT NOT NULL, created_at INTEGER NOT NULL, resolved_at INTEGER NOT NULL,
 resolve_idem TEXT NOT NULL, resolve_action TEXT NOT NULL, resolve_plan TEXT NOT NULL,
 PRIMARY KEY(machine,project,id))`

// The charter version each Bot was last asked to compare, one row a Bot.
const charterChecksSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_checks (
 machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, version INTEGER NOT NULL,
 turn TEXT NOT NULL, at INTEGER NOT NULL, PRIMARY KEY(machine,bot))`

// CharterConflictID is a report's identity: the same Bot reporting the same
// sentence against the same version is one report.
func CharterConflictID(bot string, version int64, sentence string) string {
	return "cc-" + charterDigest(bot, strconv.FormatInt(version, 10), sentence)[:24]
}

const conflictColumns = `id,project,bot,turn,version,source,path,memory_entry,bot_sentence,charter_sentence,section,suggested,state,created_at,resolved_at`

func scanConflict(scan func(...any) error) (pa.CharterConflict, error) {
	var c pa.CharterConflict
	var created, resolved int64
	err := scan(&c.ID, &c.ProjectID, &c.BotID, &c.TurnID, &c.Version, &c.Source, &c.Path, &c.MemoryEntryID, &c.BotSentence, &c.CharterSentence, &c.Section, &c.Suggested, &c.State, &created, &resolved)
	c.CreatedAt = time.UnixMilli(created).UTC()
	if resolved > 0 {
		c.ResolvedAt = time.UnixMilli(resolved).UTC()
	}
	return c, err
}

// ReportCharterConflict stores one Bot's report against the Project's
// current charter. A repeated report answers the stored one, whatever became
// of it; a report against another version is stale.
func (s *Store) ReportCharterConflict(ctx context.Context, machine string, c pa.CharterConflict) (pa.CharterConflict, error) {
	if machine == "" || c.BotID == "" || c.TurnID == "" {
		return pa.CharterConflict{}, failure(pa.Invalid, "charter_conflict")
	}
	if err := pa.ValidateCharterConflict(c); err != nil {
		return pa.CharterConflict{}, err
	}
	c.ID = CharterConflictID(c.BotID, c.Version, c.BotSentence)
	c.State = pa.ConflictOpen
	c.CreatedAt = c.CreatedAt.UTC().Truncate(time.Millisecond)
	c.ResolvedAt = time.Time{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.CharterConflict{}, err
	}
	defer tx.Rollback()
	existing, err := scanConflict(tx.QueryRowContext(ctx, `SELECT `+conflictColumns+` FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND id=?`, machine, c.ProjectID, c.ID).Scan)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return pa.CharterConflict{}, err
	}
	current, err := currentCharter(ctx, tx, machine, c.ProjectID)
	if err != nil {
		return pa.CharterConflict{}, err
	}
	// A Bot compares the charter as it heard it: the version at which its
	// text or its playbooks last changed.
	if current.DeliveryVersion(c.BotID) != c.Version {
		return pa.CharterConflict{}, failure(pa.StaleVersion, "charter_version")
	}
	var open int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND state='open'`, machine, c.ProjectID).Scan(&open); err != nil {
		return pa.CharterConflict{}, err
	}
	if open >= pa.MaxCharterConflicts {
		return pa.CharterConflict{}, failure(pa.CapacityFull, "charter_conflicts")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_conflicts(machine,project,id,bot,turn,version,source,path,memory_entry,bot_sentence,charter_sentence,section,suggested,state,created_at,resolved_at,resolve_idem,resolve_action,resolve_plan)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,'','','')`, machine, c.ProjectID, c.ID, c.BotID, c.TurnID, c.Version, string(c.Source), c.Path, c.MemoryEntryID,
		c.BotSentence, c.CharterSentence, c.Section, c.Suggested, string(c.State), c.CreatedAt.UnixMilli()); err != nil {
		return pa.CharterConflict{}, err
	}
	return c, tx.Commit()
}

// CharterConflicts lists a Project's open reports and the handled ones it
// keeps, newest first. Dismissed and superseded reports are not listed.
func (s *Store) CharterConflicts(ctx context.Context, machine, project string) ([]pa.CharterConflict, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT `+conflictColumns+` FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND state NOT IN ('dismissed','superseded')
 ORDER BY state<>'open', created_at DESC, id LIMIT ?`, machine, project, pa.MaxCharterConflicts+pa.MaxCharterConflictsResolved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pa.CharterConflict{}
	for rows.Next() {
		c, err := scanConflict(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CharterConflict reads one report.
func (s *Store) CharterConflict(ctx context.Context, machine, project, id string) (pa.CharterConflict, error) {
	c, err := scanConflict(s.reader.QueryRowContext(ctx, `SELECT `+conflictColumns+` FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND id=?`, machine, project, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return c, failure(pa.NotFound, "charter_conflict")
	}
	return c, err
}

// OpenCharterConflicts counts each Project's open reports on the machine.
func (s *Store) OpenCharterConflicts(ctx context.Context, machine string) (map[string]int, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT project,count(*) FROM project_assistant_charter_conflicts WHERE machine=? AND state='open' GROUP BY project`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var project string
		var n int
		if err = rows.Scan(&project, &n); err != nil {
			return nil, err
		}
		out[project] = n
	}
	return out, rows.Err()
}

// ConflictResolution is the person's choice in progress: the action, the key
// it was made with and the plan the daemon chose for it.
type ConflictResolution struct {
	Conflict pa.CharterConflict
	Action   pa.CharterConflictAction
	Plan     string
	// Repeated says this key already chose; Plan is the one it chose then.
	Repeated bool
}

// BeginConflictResolution records the person's choice on an open report
// before anything is changed. The same key answers the plan it chose; another
// key on a report somebody already handled is stale.
func (s *Store) BeginConflictResolution(ctx context.Context, machine, project, id string, action pa.CharterConflictAction, idem, plan string) (ConflictResolution, error) {
	if idem == "" || id == "" {
		return ConflictResolution{}, failure(pa.Invalid, "charter_conflict")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConflictResolution{}, err
	}
	defer tx.Rollback()
	var priorIdem, priorAction, priorPlan string
	row := tx.QueryRowContext(ctx, `SELECT `+conflictColumns+`,resolve_idem,resolve_action,resolve_plan FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND id=?`, machine, project, id)
	c, err := scanConflict(func(dest ...any) error { return row.Scan(append(dest, &priorIdem, &priorAction, &priorPlan)...) })
	if errors.Is(err, sql.ErrNoRows) {
		return ConflictResolution{}, failure(pa.NotFound, "charter_conflict")
	}
	if err != nil {
		return ConflictResolution{}, err
	}
	if priorIdem != "" {
		if priorIdem != idem {
			return ConflictResolution{Conflict: c}, failure(pa.StaleVersion, "charter_conflict")
		}
		if priorAction != string(action) {
			return ConflictResolution{}, failure(pa.IdempotencyConflict, "charter_conflict")
		}
		return ConflictResolution{Conflict: c, Action: action, Plan: priorPlan, Repeated: true}, nil
	}
	if c.State != pa.ConflictOpen {
		return ConflictResolution{Conflict: c}, failure(pa.StaleVersion, "charter_conflict")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_charter_conflicts SET resolve_idem=?,resolve_action=?,resolve_plan=? WHERE machine=? AND project=? AND id=?`, idem, string(action), plan, machine, project, id); err != nil {
		return ConflictResolution{}, err
	}
	return ConflictResolution{Conflict: c, Action: action, Plan: plan}, tx.Commit()
}

// FinishConflictResolution sets what became of a report and trims the
// handled reports past the retention bound, oldest first.
func (s *Store) FinishConflictResolution(ctx context.Context, machine, project, id string, state pa.CharterConflictState, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_charter_conflicts SET state=?,resolved_at=? WHERE machine=? AND project=? AND id=?`, string(state), at.UTC().UnixMilli(), machine, project, id); err != nil {
		return err
	}
	if err = trimHandledConflicts(ctx, tx, machine, project); err != nil {
		return err
	}
	return tx.Commit()
}

func trimHandledConflicts(ctx context.Context, tx *sql.Tx, machine, project string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND state<>'open' AND id NOT IN
 (SELECT id FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND state<>'open' ORDER BY resolved_at DESC, id LIMIT ?)`, machine, project, machine, project, pa.MaxCharterConflictsResolved)
	return err
}

// RecordCharterCheck notes that a Bot was asked to compare version with its
// own sentences. Its open reports against older versions are superseded:
// the new check reports again whatever still disagrees.
func (s *Store) RecordCharterCheck(ctx context.Context, machine, project, bot string, version int64, turn string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_checks(machine,bot,project,version,turn,at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(machine,bot) DO UPDATE SET project=excluded.project,version=excluded.version,turn=excluded.turn,at=excluded.at`, machine, bot, project, version, turn, at.UTC().UnixMilli()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_charter_conflicts SET state='superseded',resolved_at=? WHERE machine=? AND bot=? AND state='open' AND (project<>? OR version<?)`,
		at.UTC().UnixMilli(), machine, bot, project, version); err != nil {
		return err
	}
	if err = trimHandledConflicts(ctx, tx, machine, project); err != nil {
		return err
	}
	return tx.Commit()
}

// CharterChecks returns, by Bot, the version each Bot of the Project was
// last asked to compare.
func (s *Store) CharterChecks(ctx context.Context, machine, project string) (map[string]int64, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT bot,version FROM project_assistant_charter_checks WHERE machine=? AND project=?`, machine, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var bot string
		var v int64
		if err = rows.Scan(&bot, &v); err != nil {
			return nil, err
		}
		out[bot] = v
	}
	return out, rows.Err()
}

// ReportedVersions names, by Bot, the versions each Bot reported anything
// against.
func (s *Store) ReportedVersions(ctx context.Context, machine, project string) (map[string]map[int64]bool, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT DISTINCT bot,version FROM project_assistant_charter_conflicts WHERE machine=? AND project=?`, machine, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[int64]bool{}
	for rows.Next() {
		var bot string
		var v int64
		if err = rows.Scan(&bot, &v); err != nil {
			return nil, err
		}
		if out[bot] == nil {
			out[bot] = map[int64]bool{}
		}
		out[bot][v] = true
	}
	return out, rows.Err()
}

// ReportedBots names the Bots that reported anything against version.
func (s *Store) ReportedBots(ctx context.Context, machine, project string, version int64) (map[string]bool, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT DISTINCT bot FROM project_assistant_charter_conflicts WHERE machine=? AND project=? AND version=?`, machine, project, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var bot string
		if err = rows.Scan(&bot); err != nil {
			return nil, err
		}
		out[bot] = true
	}
	return out, rows.Err()
}
