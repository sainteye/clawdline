package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
const charterVersionsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_charter_versions (
 machine TEXT NOT NULL, project TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0),
 author TEXT NOT NULL, reason TEXT NOT NULL, created_at INTEGER NOT NULL, text TEXT NOT NULL,
 idem TEXT NOT NULL, request TEXT NOT NULL, PRIMARY KEY(machine,project,version))`

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
// ExpectedVersion (0 when it has no charter yet).
type CharterWrite struct {
	MachineID       string
	ProjectID       string
	ExpectedVersion int64
	Text            string
	Reason          string
	Author          pa.CharterAuthor
	IdempotencyKey  string
	At              time.Time
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

func currentCharter(ctx context.Context, r reader, machine, project string) (pa.CharterVersion, error) {
	var out pa.CharterVersion
	var at int64
	err := r.QueryRowContext(ctx, `SELECT version,author,reason,created_at,text FROM project_assistant_charter_versions WHERE machine=? AND project=? ORDER BY version DESC LIMIT 1`, machine, project).
		Scan(&out.Version, &out.Author, &out.Reason, &at, &out.Text)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.CharterVersion{}, nil
	}
	out.CreatedAt = time.UnixMilli(at).UTC()
	return out, err
}

// Charter returns the Project's current charter; version 0 means none.
func (s *Store) Charter(ctx context.Context, machine, project string) (pa.CharterVersion, error) {
	return currentCharter(ctx, s.reader, machine, project)
}

// CharterAt returns one retained version with its text.
func (s *Store) CharterAt(ctx context.Context, machine, project string, version int64) (pa.CharterVersion, error) {
	var out pa.CharterVersion
	var at int64
	err := s.reader.QueryRowContext(ctx, `SELECT version,author,reason,created_at,text FROM project_assistant_charter_versions WHERE machine=? AND project=? AND version=?`, machine, project, version).
		Scan(&out.Version, &out.Author, &out.Reason, &at, &out.Text)
	if errors.Is(err, sql.ErrNoRows) {
		return out, failure(pa.NotFound, "charter_version")
	}
	out.CreatedAt = time.UnixMilli(at).UTC()
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
 ORDER BY v.project LIMIT ?`, machine, pa.MaxCharterProjects+1)
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
	var prior pa.CharterVersion
	var priorRequest string
	var at int64
	err = tx.QueryRowContext(ctx, `SELECT version,author,reason,created_at,text,request FROM project_assistant_charter_versions WHERE machine=? AND project=? AND idem=? ORDER BY version DESC LIMIT 1`, w.MachineID, w.ProjectID, w.IdempotencyKey).
		Scan(&prior.Version, &prior.Author, &prior.Reason, &at, &prior.Text, &priorRequest)
	if err == nil {
		if priorRequest != request {
			return pa.CharterVersion{}, failure(pa.IdempotencyConflict, "charter")
		}
		prior.CreatedAt = time.UnixMilli(at).UTC()
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return pa.CharterVersion{}, err
	}
	current, err := currentCharter(ctx, tx, w.MachineID, w.ProjectID)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	if current.Version != w.ExpectedVersion {
		return current, failure(pa.StaleVersion, "charter_version")
	}
	if before != nil {
		if err = before(tx); err != nil {
			return pa.CharterVersion{}, err
		}
	}
	next := pa.CharterVersion{Version: current.Version + 1, Author: w.Author, Reason: w.Reason, CreatedAt: w.At.UTC().Truncate(time.Millisecond), Text: w.Text}
	// The primary key fences a concurrent writer that read the same version.
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_versions(machine,project,version,author,reason,created_at,text,idem,request) VALUES(?,?,?,?,?,?,?,?,?)`,
		w.MachineID, w.ProjectID, next.Version, string(next.Author), next.Reason, next.CreatedAt.UnixMilli(), next.Text, w.IdempotencyKey, request); err != nil {
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
	var text string
	err := s.db.QueryRowContext(ctx, `SELECT text FROM project_assistant_charter_versions WHERE machine=? AND project=? AND version=?`, machine, project, version).Scan(&text)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		text = ""
	case err != nil:
		return pa.CharterVersion{}, err
	}
	var restoreErr error
	if errors.Is(err, sql.ErrNoRows) {
		restoreErr = failure(pa.NotFound, "charter_version")
	}
	w.Text = text
	return s.insertCharter(ctx, w, request, func(*sql.Tx) error { return restoreErr })
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

// CharterBot is what the charter pages show of one Bot: its Project, whether
// it leads, and whether it has a running or queued turn now.
type CharterBot struct {
	BotID     string
	Name      string
	ProjectID string
	Lead      bool
	Hidden    bool
	Running   bool
	Queued    bool
}

// CharterBots lists the machine's Bots, without private background workers.
func (s *Store) CharterBots(ctx context.Context, machine string) ([]CharterBot, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT bot,
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.name'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.project.project_id'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.lead'),0),
 coalesce(json_extract(CAST(data AS TEXT),'$.Bot.hidden'),0),
 coalesce(json_extract(CAST(data AS TEXT),'$.Conversation.active_turn_id'),'')<>'',
 EXISTS(SELECT 1 FROM json_each(CAST(data AS TEXT),'$.Turns') t WHERE json_extract(t.value,'$.state') IN ('accepted','queued'))
 FROM project_assistant_bots WHERE machine=? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL ORDER BY bot`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CharterBot{}
	for rows.Next() {
		var b CharterBot
		if err = rows.Scan(&b.BotID, &b.Name, &b.ProjectID, &b.Lead, &b.Hidden, &b.Running, &b.Queued); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
