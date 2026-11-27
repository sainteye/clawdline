package projectassistantstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Team charters live outside every Bot aggregate, keyed by machine and
// Project. Each save inserts one immutable version row; the highest version
// is current. A version row also keeps the idempotency key and request digest
// of the write that made it, so a repeated write whose reply was lost answers
// its own effect and a reused key with another request is a conflict.
//
// playbooks, text_version and deliveries came later as added columns, not a
// schema version: a row written before them reads as no playbooks whose text
// changed at its own version. text_version 0 means the row's own version.
const charterVersionsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_versions (
 machine TEXT NOT NULL, project TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0),
 author TEXT NOT NULL, reason TEXT NOT NULL, created_at INTEGER NOT NULL, text TEXT NOT NULL,
 idem TEXT NOT NULL, request TEXT NOT NULL, PRIMARY KEY(machine,project,version))`

// charterPlaybookColumns are added to a table made before them. An older
// daemon names its columns when it inserts, so its rows take these defaults.
var charterPlaybookColumns = []struct{ name, ddl string }{
	{"playbooks", `ALTER TABLE project_assistant_charter_versions ADD COLUMN playbooks TEXT NOT NULL DEFAULT '[]'`},
	{"text_version", `ALTER TABLE project_assistant_charter_versions ADD COLUMN text_version INTEGER NOT NULL DEFAULT 0`},
	{"deliveries", `ALTER TABLE project_assistant_charter_versions ADD COLUMN deliveries TEXT NOT NULL DEFAULT '{}'`},
}

func addCharterPlaybookColumns(ctx context.Context, tx *sql.Tx) error {
	for _, c := range charterPlaybookColumns {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('project_assistant_charter_versions') WHERE name=?`, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.ExecContext(ctx, c.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

const charterVersionColumns = `version,author,reason,created_at,text,playbooks,text_version,deliveries`

func scanCharterVersion(scan func(...any) error, extra ...any) (pa.CharterVersion, error) {
	var out pa.CharterVersion
	var at int64
	var playbooks, deliveries string
	if err := scan(append([]any{&out.Version, &out.Author, &out.Reason, &at, &out.Text, &playbooks, &out.TextVersion, &deliveries}, extra...)...); err != nil {
		return pa.CharterVersion{}, err
	}
	out.CreatedAt = time.UnixMilli(at).UTC()
	if out.TextVersion <= 0 {
		out.TextVersion = out.Version
	}
	if err := json.Unmarshal([]byte(playbooks), &out.Playbooks); err != nil {
		return pa.CharterVersion{}, err
	}
	if len(out.Playbooks) == 0 {
		out.Playbooks = nil
	}
	if err := json.Unmarshal([]byte(deliveries), &out.Deliveries); err != nil {
		return pa.CharterVersion{}, err
	}
	return out, nil
}

const charterIdemIndex = `CREATE INDEX IF NOT EXISTS project_assistant_charter_idem ON project_assistant_charter_versions(machine,project,idem)`

// Pending lead proposals. Applying or dismissing one removes its row.
const charterProposalsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_proposals (
 machine TEXT NOT NULL, project TEXT NOT NULL, id TEXT NOT NULL, base INTEGER NOT NULL,
 text TEXT NOT NULL, summary TEXT NOT NULL, bot TEXT NOT NULL, turn TEXT NOT NULL,
 created_at INTEGER NOT NULL, PRIMARY KEY(machine,project,id))`

// The charter version each Bot's most recent prompt carried, one row a Bot.
const charterReadsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_reads (
 machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, version INTEGER NOT NULL,
 turn TEXT NOT NULL, at INTEGER NOT NULL, PRIMARY KEY(machine,bot))`

// CharterWrite saves Text as the next version while the Project is still at
// ExpectedVersion (0 when it has no charter yet). The new version keeps the
// current version's playbooks, so a writer that knows nothing of playbooks
// never removes them.
type CharterWrite struct {
	MachineID       string
	ProjectID       string
	ExpectedVersion int64
	Text            string
	Reason          string
	Author          pa.CharterAuthor
	IdempotencyKey  string
	At              time.Time
	// shape, when set, makes the new version's text and playbooks from the
	// current version inside the write's transaction.
	shape func(current pa.CharterVersion) (string, []pa.CharterPlaybook, error)
}

// CharterRead is the version one Bot's latest prompt carried.
type CharterRead struct {
	BotID   string
	Version int64
	TurnID  string
	At      time.Time
}

func charterDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strconv.Itoa(len(p))))
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// latestCharter is the newest version row, a removal included; version 0
// when the Project never had one.
func latestCharter(ctx context.Context, r reader, machine, project string) (pa.CharterVersion, error) {
	out, err := scanCharterVersion(r.QueryRowContext(ctx, `SELECT `+charterVersionColumns+` FROM project_assistant_charter_versions WHERE machine=? AND project=? ORDER BY version DESC LIMIT 1`, machine, project).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.CharterVersion{}, nil
	}
	return out, err
}

// currentCharter is the charter the Project has now: version 0 when it never
// had one or its newest version is a removal.
func currentCharter(ctx context.Context, r reader, machine, project string) (pa.CharterVersion, error) {
	v, err := latestCharter(ctx, r, machine, project)
	if err != nil || v.Author == pa.CharterByRemoval {
		return pa.CharterVersion{}, err
	}
	return v, nil
}

// Charter returns the Project's current charter; version 0 means none.
func (s *Store) Charter(ctx context.Context, machine, project string) (pa.CharterVersion, error) {
	return currentCharter(ctx, s.reader, machine, project)
}

// CharterAt returns one retained version with its text and playbooks.
func (s *Store) CharterAt(ctx context.Context, machine, project string, version int64) (pa.CharterVersion, error) {
	out, err := scanCharterVersion(s.reader.QueryRowContext(ctx, `SELECT `+charterVersionColumns+` FROM project_assistant_charter_versions WHERE machine=? AND project=? AND version=?`, machine, project, version).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return out, failure(pa.NotFound, "charter_version")
	}
	return out, err
}

// CharterHistory lists the retained versions, newest first, without text.
func (s *Store) CharterHistory(ctx context.Context, machine, project string) ([]pa.CharterVersion, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT version,author,reason,created_at FROM project_assistant_charter_versions WHERE machine=? AND project=? ORDER BY version DESC LIMIT ?`, machine, project, pa.MaxCharterVersions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pa.CharterVersion{}
	for rows.Next() {
		var v pa.CharterVersion
		var at int64
		if err = rows.Scan(&v.Version, &v.Author, &v.Reason, &at); err != nil {
			return nil, err
		}
		v.CreatedAt = time.UnixMilli(at).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// Charters lists every Project of the machine that has a charter.
func (s *Store) Charters(ctx context.Context, machine string) ([]pa.CharterSummary, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT v.project,v.version,v.created_at,v.author,
 (SELECT count(*) FROM project_assistant_charter_proposals p WHERE p.machine=v.machine AND p.project=v.project)
 FROM project_assistant_charter_versions v WHERE v.machine=? AND v.version=(SELECT max(version) FROM project_assistant_charter_versions w WHERE w.machine=v.machine AND w.project=v.project)
 AND v.author<>'removed' ORDER BY v.project LIMIT ?`, machine, pa.MaxCharterProjects+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pa.CharterSummary{}
	for rows.Next() {
		var c pa.CharterSummary
		var at int64
		if err = rows.Scan(&c.ProjectID, &c.Version, &at, &c.UpdatedBy, &c.Pending); err != nil {
			return nil, err
		}
		c.UpdatedAt = time.UnixMilli(at).UTC()
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > pa.MaxCharterProjects {
		return out[:pa.MaxCharterProjects], failure(pa.CapacityFull, "charter_projects")
	}
	return out, nil
}

// SaveCharter stores the next version. A repeat of a write that already took
// effect answers the version it made; the same key with a different request
// is an idempotency conflict; a moved charter is stale.
func (s *Store) SaveCharter(ctx context.Context, w CharterWrite) (pa.CharterVersion, error) {
	if w.MachineID == "" {
		return pa.CharterVersion{}, failure(pa.Invalid, "machine_id")
	}
	if err := pa.ValidateCharterProject(w.ProjectID); err != nil {
		return pa.CharterVersion{}, err
	}
	if err := pa.ValidateCharterText(w.Text, w.Reason); err != nil {
		return pa.CharterVersion{}, err
	}
	if w.IdempotencyKey == "" || w.ExpectedVersion < 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "idempotency_key")
	}
	request := charterDigest("save", string(w.Author), strconv.FormatInt(w.ExpectedVersion, 10), w.Text, w.Reason)
	return s.insertCharter(ctx, w, request, nil)
}

// insertCharter runs one version insert; before, when set, runs inside the
// same transaction after the version check (a proposal removal).
func (s *Store) insertCharter(ctx context.Context, w CharterWrite, request string, before func(*sql.Tx) error) (pa.CharterVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	defer tx.Rollback()
	var priorRequest string
	prior, err := scanCharterVersion(tx.QueryRowContext(ctx, `SELECT `+charterVersionColumns+`,request FROM project_assistant_charter_versions WHERE machine=? AND project=? AND idem=? ORDER BY version DESC LIMIT 1`, w.MachineID, w.ProjectID, w.IdempotencyKey).Scan, &priorRequest)
	if err == nil {
		if priorRequest != request {
			return pa.CharterVersion{}, failure(pa.IdempotencyConflict, "charter")
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return pa.CharterVersion{}, err
	}
	latest, err := latestCharter(ctx, tx, w.MachineID, w.ProjectID)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	current := latest
	if latest.Author == pa.CharterByRemoval {
		current = pa.CharterVersion{}
	}
	if current.Version != w.ExpectedVersion {
		return current, failure(pa.StaleVersion, "charter_version")
	}
	if before != nil {
		if err = before(tx); err != nil {
			return pa.CharterVersion{}, err
		}
	}
	// A charter made after a removal is numbered after it, so a Bot that
	// heard an older charter of the same number is told again.
	next := pa.CharterVersion{Version: latest.Version + 1, Author: w.Author, Reason: w.Reason, CreatedAt: w.At.UTC().Truncate(time.Millisecond), Text: w.Text, Playbooks: current.Playbooks}
	if w.shape != nil {
		if next.Text, next.Playbooks, err = w.shape(current); err != nil {
			return pa.CharterVersion{}, err
		}
	}
	if err = pa.ValidateCharterDocument(next.Text, next.Playbooks); err != nil {
		return pa.CharterVersion{}, err
	}
	pa.NextCharterDeliveries(latest, &next)
	playbooks, err := json.Marshal(append([]pa.CharterPlaybook{}, next.Playbooks...))
	if err != nil {
		return pa.CharterVersion{}, err
	}
	deliveries, err := json.Marshal(next.Deliveries)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	// The primary key fences a concurrent writer that read the same version.
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_versions(machine,project,version,author,reason,created_at,text,idem,request,playbooks,text_version,deliveries) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.MachineID, w.ProjectID, next.Version, string(next.Author), next.Reason, next.CreatedAt.UnixMilli(), next.Text, w.IdempotencyKey, request, string(playbooks), next.TextVersion, string(deliveries)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return current, failure(pa.StaleVersion, "charter_version")
		}
		return pa.CharterVersion{}, err
	}
	// The oldest versions past the retention bound go; the current one stays.
	if _, err = tx.ExecContext(ctx, `DELETE FROM project_assistant_charter_versions WHERE machine=? AND project=? AND version<=?`, w.MachineID, w.ProjectID, next.Version-pa.MaxCharterVersions); err != nil {
		return pa.CharterVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return pa.CharterVersion{}, err
	}
	return next, nil
}

// RestoreCharter saves a copy of an earlier retained version as the next one.
func (s *Store) RestoreCharter(ctx context.Context, machine, project string, expected, version int64, idem string, at time.Time) (pa.CharterVersion, error) {
	if idem == "" || expected < 0 || version <= 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "version")
	}
	w := CharterWrite{MachineID: machine, ProjectID: project, ExpectedVersion: expected, Author: pa.CharterByRestore, IdempotencyKey: idem, At: at, Reason: "Restored version " + strconv.FormatInt(version, 10)}
	request := charterDigest("restore", strconv.FormatInt(expected, 10), strconv.FormatInt(version, 10))
	// A repeated restore answers its own version before the old one is read:
	// it may have been evicted since.
	old, err := scanCharterVersion(s.db.QueryRowContext(ctx, `SELECT `+charterVersionColumns+` FROM project_assistant_charter_versions WHERE machine=? AND project=? AND version=?`, machine, project, version).Scan)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pa.CharterVersion{}, err
	}
	var restoreErr error
	// A removal has no text to bring back.
	if errors.Is(err, sql.ErrNoRows) || old.Author == pa.CharterByRemoval {
		restoreErr = failure(pa.NotFound, "charter_version")
	}
	// The restored version brings back that version's playbooks too.
	w.Text = old.Text
	w.shape = func(pa.CharterVersion) (string, []pa.CharterPlaybook, error) { return old.Text, old.Playbooks, nil }
	return s.insertCharter(ctx, w, request, func(*sql.Tx) error { return restoreErr })
}

// RemoveCharter takes the Project's charter away while it is still at
// expected, as a removal version with empty text and no playbooks, and drops
// its pending proposals with it. A repeat of a removal that took effect
// answers it.
func (s *Store) RemoveCharter(ctx context.Context, machine, project string, expected int64, idem string, at time.Time) (pa.CharterVersion, error) {
	if idem == "" || expected <= 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "expected_version")
	}
	if err := pa.ValidateCharterProject(project); err != nil {
		return pa.CharterVersion{}, err
	}
	w := CharterWrite{MachineID: machine, ProjectID: project, ExpectedVersion: expected, Author: pa.CharterByRemoval, IdempotencyKey: idem, At: at,
		shape: func(pa.CharterVersion) (string, []pa.CharterPlaybook, error) { return "", nil, nil }}
	request := charterDigest("remove", strconv.FormatInt(expected, 10))
	return s.insertCharter(ctx, w, request, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_charter_proposals WHERE machine=? AND project=?`, machine, project)
		return err
	})
}

// CharterPlaybookWrite saves one playbook as the next version while the
// Project is still at ExpectedVersion. An empty Playbook.ID creates one.
type CharterPlaybookWrite struct {
	MachineID       string
	ProjectID       string
	ExpectedVersion int64
	Playbook        pa.CharterPlaybook
	Reason          string
	Author          pa.CharterAuthor
	IdempotencyKey  string
	At              time.Time
}

func newPlaybookID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pb-" + hex.EncodeToString(b), nil
}

// SaveCharterPlaybook creates or replaces one playbook of the current
// charter, keeping its text and the other playbooks. A Project without a
// charter has nowhere to keep one.
func (s *Store) SaveCharterPlaybook(ctx context.Context, w CharterPlaybookWrite) (pa.CharterVersion, error) {
	if w.MachineID == "" {
		return pa.CharterVersion{}, failure(pa.Invalid, "machine_id")
	}
	if err := pa.ValidateCharterProject(w.ProjectID); err != nil {
		return pa.CharterVersion{}, err
	}
	if err := pa.ValidateCharterPlaybook(w.Playbook); err != nil {
		return pa.CharterVersion{}, err
	}
	if err := pa.ValidateCharterText("", w.Reason); err != nil {
		return pa.CharterVersion{}, err
	}
	if w.IdempotencyKey == "" || w.ExpectedVersion < 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "idempotency_key")
	}
	if w.Author == "" {
		w.Author = pa.CharterByPerson
	}
	p := w.Playbook
	p.BotIDs = append([]string{}, p.BotIDs...)
	request := charterDigest("playbook", string(w.Author), strconv.FormatInt(w.ExpectedVersion, 10), p.ID, p.Title, p.Text, strings.Join(p.BotIDs, "\n"), w.Reason)
	write := CharterWrite{MachineID: w.MachineID, ProjectID: w.ProjectID, ExpectedVersion: w.ExpectedVersion, Reason: w.Reason, Author: w.Author, IdempotencyKey: w.IdempotencyKey, At: w.At,
		shape: func(current pa.CharterVersion) (string, []pa.CharterPlaybook, error) {
			if current.Version <= 0 {
				return "", nil, failure(pa.Invalid, "charter")
			}
			playbooks := slices.Clone(current.Playbooks)
			if p.ID == "" {
				id, err := newPlaybookID()
				if err != nil {
					return "", nil, err
				}
				p.ID = id
				return current.Text, append(playbooks, p), nil
			}
			at := slices.IndexFunc(playbooks, func(o pa.CharterPlaybook) bool { return o.ID == p.ID })
			if at < 0 {
				return "", nil, failure(pa.NotFound, "charter_playbook")
			}
			playbooks[at] = p
			return current.Text, playbooks, nil
		}}
	return s.insertCharter(ctx, write, request, nil)
}

// RemoveCharterPlaybook removes one playbook from the current charter as the
// next version. The Bots it was assigned to hear the charter without it.
func (s *Store) RemoveCharterPlaybook(ctx context.Context, machine, project string, expected int64, id, idem string, at time.Time) (pa.CharterVersion, error) {
	if idem == "" || expected <= 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "expected_version")
	}
	if err := pa.ValidateCharterProject(project); err != nil {
		return pa.CharterVersion{}, err
	}
	if id == "" {
		return pa.CharterVersion{}, failure(pa.Invalid, "playbook_id")
	}
	request := charterDigest("playbook-remove", strconv.FormatInt(expected, 10), id)
	w := CharterWrite{MachineID: machine, ProjectID: project, ExpectedVersion: expected, Author: pa.CharterByPerson, IdempotencyKey: idem, At: at,
		shape: func(current pa.CharterVersion) (string, []pa.CharterPlaybook, error) {
			kept := slices.DeleteFunc(slices.Clone(current.Playbooks), func(o pa.CharterPlaybook) bool { return o.ID == id })
			if len(kept) == len(current.Playbooks) {
				return "", nil, failure(pa.NotFound, "charter_playbook")
			}
			return current.Text, kept, nil
		}}
	return s.insertCharter(ctx, w, request, nil)
}

// ProposeCharter records a lead proposal. The same Bot turn proposing the
// same text again answers the existing proposal.
func (s *Store) ProposeCharter(ctx context.Context, machine string, p pa.CharterProposal) (pa.CharterProposal, error) {
	if machine == "" || p.BotID == "" || p.TurnID == "" {
		return pa.CharterProposal{}, failure(pa.Invalid, "charter_proposal")
	}
	if err := pa.ValidateCharterProject(p.ProjectID); err != nil {
		return pa.CharterProposal{}, err
	}
	if err := pa.ValidateCharterText(p.Text, p.Summary); err != nil {
		return pa.CharterProposal{}, err
	}
	if p.BaseVersion < 0 {
		return pa.CharterProposal{}, failure(pa.Invalid, "base_version")
	}
	p.ID = "cp-" + charterDigest(p.BotID, p.TurnID, strconv.FormatInt(p.BaseVersion, 10), p.Text)[:24]
	p.CreatedAt = p.CreatedAt.UTC().Truncate(time.Millisecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.CharterProposal{}, err
	}
	defer tx.Rollback()
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_charter_proposals WHERE machine=? AND project=? AND id=?`, machine, p.ProjectID, p.ID).Scan(&existing); err != nil {
		return pa.CharterProposal{}, err
	}
	if existing > 0 {
		return p, nil
	}
	current, err := currentCharter(ctx, tx, machine, p.ProjectID)
	if err != nil {
		return pa.CharterProposal{}, err
	}
	if current.Version != p.BaseVersion {
		return pa.CharterProposal{}, failure(pa.StaleVersion, "charter_proposal_base")
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_charter_proposals WHERE machine=? AND project=?`, machine, p.ProjectID).Scan(&pending); err != nil {
		return pa.CharterProposal{}, err
	}
	if pending >= pa.MaxCharterProposals {
		return pa.CharterProposal{}, failure(pa.CapacityFull, "charter_proposals")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_proposals(machine,project,id,base,text,summary,bot,turn,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		machine, p.ProjectID, p.ID, p.BaseVersion, p.Text, p.Summary, p.BotID, p.TurnID, p.CreatedAt.UnixMilli()); err != nil {
		return pa.CharterProposal{}, err
	}
	return p, tx.Commit()
}

func scanProposals(rows *sql.Rows) ([]pa.CharterProposal, error) {
	defer rows.Close()
	out := []pa.CharterProposal{}
	for rows.Next() {
		var p pa.CharterProposal
		var at int64
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.BaseVersion, &p.Text, &p.Summary, &p.BotID, &p.TurnID, &at); err != nil {
			return nil, err
		}
		p.CreatedAt = time.UnixMilli(at).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// CharterProposals lists a Project's pending proposals, oldest first.
func (s *Store) CharterProposals(ctx context.Context, machine, project string) ([]pa.CharterProposal, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT id,project,base,text,summary,bot,turn,created_at FROM project_assistant_charter_proposals WHERE machine=? AND project=? ORDER BY created_at,id LIMIT ?`, machine, project, pa.MaxCharterProposals)
	if err != nil {
		return nil, err
	}
	return scanProposals(rows)
}

// ApplyCharterProposal saves a pending proposal as the next version while the
// charter is still at expected and at the proposal's base, and removes it.
func (s *Store) ApplyCharterProposal(ctx context.Context, machine, project, id string, expected int64, idem string, at time.Time) (pa.CharterVersion, error) {
	if idem == "" || id == "" || expected < 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "charter_proposal")
	}
	request := charterDigest("apply", id, strconv.FormatInt(expected, 10))
	var p pa.CharterProposal
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id,project,base,text,summary,bot,turn,created_at FROM project_assistant_charter_proposals WHERE machine=? AND project=? AND id=?`, machine, project, id).
		Scan(&p.ID, &p.ProjectID, &p.BaseVersion, &p.Text, &p.Summary, &p.BotID, &p.TurnID, &created)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return pa.CharterVersion{}, err
	}
	w := CharterWrite{MachineID: machine, ProjectID: project, ExpectedVersion: expected, Text: p.Text, Reason: p.Summary, Author: pa.CharterByLeadProposal, IdempotencyKey: idem, At: at}
	return s.insertCharter(ctx, w, request, func(tx *sql.Tx) error {
		if missing {
			return failure(pa.NotFound, "charter_proposal")
		}
		if p.BaseVersion != expected {
			return failure(pa.StaleVersion, "charter_proposal_base")
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_charter_proposals WHERE machine=? AND project=? AND id=?`, machine, project, id)
		if err != nil {
			return err
		}
		if n, e := res.RowsAffected(); e != nil || n != 1 {
			return errors.Join(failure(pa.NotFound, "charter_proposal"), e)
		}
		return nil
	})
}

// DismissCharterProposal removes a pending proposal. Dismissing one that is
// already gone succeeds, so a repeated dismissal answers the same list.
func (s *Store) DismissCharterProposal(ctx context.Context, machine, project, id string) error {
	if id == "" {
		return failure(pa.Invalid, "charter_proposal")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_assistant_charter_proposals WHERE machine=? AND project=? AND id=?`, machine, project, id)
	return err
}

// RecordCharterRead notes the charter version a Bot's prompt carried.
func (s *Store) RecordCharterRead(ctx context.Context, machine, project, bot string, version int64, turn string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_assistant_charter_reads(machine,bot,project,version,turn,at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(machine,bot) DO UPDATE SET project=excluded.project,version=excluded.version,turn=excluded.turn,at=excluded.at`, machine, bot, project, version, turn, at.UTC().UnixMilli())
	return err
}

// CharterReads returns, by Bot, the version each Bot's latest prompt in the
// Project carried.
func (s *Store) CharterReads(ctx context.Context, machine, project string) (map[string]CharterRead, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT bot,version,turn,at FROM project_assistant_charter_reads WHERE machine=? AND project=?`, machine, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]CharterRead{}
	for rows.Next() {
		var r CharterRead
		var at int64
		if err = rows.Scan(&r.BotID, &r.Version, &r.TurnID, &at); err != nil {
			return nil, err
		}
		r.At = time.UnixMilli(at).UTC()
		out[r.BotID] = r
	}
	return out, rows.Err()
}

// CharterBot is what the charter pages show of one Bot: its Project and the
// Project's kind, whether it leads, and whether it has a running or queued
// turn now.
type CharterBot struct {
	BotID       string
	Name        string
	ProjectID   string
	ProjectKind pa.ProjectKind
	Lead        bool
	Hidden      bool
	Running     bool
	Queued      bool
}

// CharterBots lists the machine's Bots, without private background workers.
func (s *Store) CharterBots(ctx context.Context, machine string) ([]CharterBot, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT bot,
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.name'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.project.project_id'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.project.kind'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.lead'),0),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.hidden'),0),
 coalesce(json_extract(CAST(data AS TEXT),'$.Conversation.active_turn_id'),'')<>'',
 EXISTS(SELECT 1 FROM project_assistant_bot_facts t WHERE t.machine=project_assistant_bots.machine AND t.bot=project_assistant_bots.bot AND t.collection='Turns' AND json_extract(CAST(t.data AS TEXT),'$.state') IN ('accepted','queued'))
 FROM project_assistant_bots WHERE machine=? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL ORDER BY bot`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CharterBot{}
	for rows.Next() {
		var b CharterBot
		if err = rows.Scan(&b.BotID, &b.Name, &b.ProjectID, &b.ProjectKind, &b.Lead, &b.Hidden, &b.Running, &b.Queued); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
