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
// playbooks, text_version, deliveries and workflow came later as added
// columns, not a schema version: a row written before them reads as no
// playbooks and no workflow, whose text changed at its own version.
// text_version 0 means the row's own version; workflow ” means none.
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
	{"workflow", `ALTER TABLE project_assistant_charter_versions ADD COLUMN workflow TEXT NOT NULL DEFAULT ''`},
}

// charterProposalColumns are added to a proposals table made before
// workflow proposals: an older row is a text proposal.
var charterProposalColumns = []struct{ name, ddl string }{
	{"kind", `ALTER TABLE project_assistant_charter_proposals ADD COLUMN kind TEXT NOT NULL DEFAULT 'text'`},
	{"workflow", `ALTER TABLE project_assistant_charter_proposals ADD COLUMN workflow TEXT NOT NULL DEFAULT ''`},
}

func addCharterPlaybookColumns(ctx context.Context, tx *sql.Tx) error {
	if err := addColumns(ctx, tx, "project_assistant_charter_versions", charterPlaybookColumns); err != nil {
		return err
	}
	return addColumns(ctx, tx, "project_assistant_charter_proposals", charterProposalColumns)
}

func addColumns(ctx context.Context, tx *sql.Tx, table string, columns []struct{ name, ddl string }) error {
	for _, c := range columns {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, table, c.name).Scan(&n); err != nil {
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

const charterVersionColumns = `version,author,reason,created_at,text,playbooks,text_version,deliveries,workflow`

// decodeWorkflow reads a stored workflow; ” is none.
func decodeWorkflow(stored string) (*pa.CharterWorkflow, error) {
	if stored == "" {
		return nil, nil
	}
	var w pa.CharterWorkflow
	if err := json.Unmarshal([]byte(stored), &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// encodeWorkflow is the stored form of a workflow; none is ”.
func encodeWorkflow(w *pa.CharterWorkflow) (string, error) {
	if w == nil || len(w.States) == 0 {
		return "", nil
	}
	out := pa.CharterWorkflow{States: append([]pa.WorkflowState{}, w.States...), Transitions: append([]pa.WorkflowTransition{}, w.Transitions...)}
	b, err := json.Marshal(out)
	return string(b), err
}

func scanCharterVersion(scan func(...any) error, extra ...any) (pa.CharterVersion, error) {
	var out pa.CharterVersion
	var at int64
	var playbooks, deliveries, workflow string
	if err := scan(append([]any{&out.Version, &out.Author, &out.Reason, &at, &out.Text, &playbooks, &out.TextVersion, &deliveries, &workflow}, extra...)...); err != nil {
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
	w, err := decodeWorkflow(workflow)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	out.Workflow = w
	return out, nil
}

const charterIdemIndex = `CREATE INDEX IF NOT EXISTS project_assistant_charter_idem ON project_assistant_charter_versions(machine,project,idem)`

// Pending lead proposals. Applying or dismissing one removes its row. kind
// and workflow are added columns (charterProposalColumns).
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
// current version's playbooks and workflow, so a writer that knows nothing
// of them never removes them.
type CharterWrite struct {
	MachineID       string
	ProjectID       string
	ExpectedVersion int64
	Text            string
	Reason          string
	Author          pa.CharterAuthor
	IdempotencyKey  string
	At              time.Time
	// shape, when set, makes the new version's text, playbooks and workflow
	// from the current version inside the write's transaction.
	shape func(tx *sql.Tx, current pa.CharterVersion) (charterBody, error)
}

// charterBody is what a version holds besides its metadata.
type charterBody struct {
	Text      string
	Playbooks []pa.CharterPlaybook
	Workflow  *pa.CharterWorkflow
}

func bodyOf(v pa.CharterVersion) charterBody {
	return charterBody{Text: v.Text, Playbooks: v.Playbooks, Workflow: v.Workflow}
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
	next := pa.CharterVersion{Version: latest.Version + 1, Author: w.Author, Reason: w.Reason, CreatedAt: w.At.UTC().Truncate(time.Millisecond), Text: w.Text, Playbooks: current.Playbooks, Workflow: current.Workflow}
	if w.shape != nil {
		body, err := w.shape(tx, current)
		if err != nil {
			return pa.CharterVersion{}, err
		}
		next.Text, next.Playbooks, next.Workflow = body.Text, body.Playbooks, body.Workflow
	}
	if err = pa.ValidateCharterDocument(next.Text, next.Playbooks); err != nil {
		return pa.CharterVersion{}, err
	}
	if next.Workflow != nil && len(next.Workflow.States) == 0 {
		next.Workflow = nil
	}
	// A kept or restored workflow is checked as a graph; its Bots were
	// checked when it was written and may have gone since.
	if next.Workflow != nil {
		if err = pa.ValidateCharterWorkflow(*next.Workflow, nil); err != nil {
			return pa.CharterVersion{}, err
		}
	}
	workflow, err := encodeWorkflow(next.Workflow)
	if err != nil {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_versions(machine,project,version,author,reason,created_at,text,idem,request,playbooks,text_version,deliveries,workflow) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.MachineID, w.ProjectID, next.Version, string(next.Author), next.Reason, next.CreatedAt.UnixMilli(), next.Text, w.IdempotencyKey, request, string(playbooks), next.TextVersion, string(deliveries), workflow); err != nil {
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
	// The restored version brings back that version's playbooks and
	// workflow too.
	w.Text = old.Text
	w.shape = func(*sql.Tx, pa.CharterVersion) (charterBody, error) { return bodyOf(old), nil }
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
		shape: func(*sql.Tx, pa.CharterVersion) (charterBody, error) { return charterBody{}, nil }}
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
		shape: func(_ *sql.Tx, current pa.CharterVersion) (charterBody, error) {
			if current.Version <= 0 {
				return charterBody{}, failure(pa.Invalid, "charter")
			}
			body := bodyOf(current)
			body.Playbooks = slices.Clone(current.Playbooks)
			if p.ID == "" {
				id, err := newPlaybookID()
				if err != nil {
					return charterBody{}, err
				}
				p.ID = id
				body.Playbooks = append(body.Playbooks, p)
				return body, nil
			}
			at := slices.IndexFunc(body.Playbooks, func(o pa.CharterPlaybook) bool { return o.ID == p.ID })
			if at < 0 {
				return charterBody{}, failure(pa.NotFound, "charter_playbook")
			}
			body.Playbooks[at] = p
			return body, nil
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
		shape: func(_ *sql.Tx, current pa.CharterVersion) (charterBody, error) {
			body := bodyOf(current)
			body.Playbooks = slices.DeleteFunc(slices.Clone(current.Playbooks), func(o pa.CharterPlaybook) bool { return o.ID == id })
			if len(body.Playbooks) == len(current.Playbooks) {
				return charterBody{}, failure(pa.NotFound, "charter_playbook")
			}
			return body, nil
		}}
	return s.insertCharter(ctx, w, request, nil)
}

// CharterWorkflowWrite replaces the workflow of the current charter as the
// next version while the Project is still at ExpectedVersion, keeping its
// text and playbooks. A Project without a charter gets one holding only the
// workflow.
type CharterWorkflowWrite struct {
	MachineID       string
	ProjectID       string
	ExpectedVersion int64
	Workflow        pa.CharterWorkflow
	Reason          string
	Author          pa.CharterAuthor
	IdempotencyKey  string
	At              time.Time
}

// SaveCharterWorkflow writes a workflow, validated exactly as a lead
// proposal is: the graph's rules, then that every named Bot is a visible
// Bot of the Project.
func (s *Store) SaveCharterWorkflow(ctx context.Context, w CharterWorkflowWrite) (pa.CharterVersion, error) {
	if w.MachineID == "" {
		return pa.CharterVersion{}, failure(pa.Invalid, "machine_id")
	}
	if err := pa.ValidateCharterProject(w.ProjectID); err != nil {
		return pa.CharterVersion{}, err
	}
	if err := pa.ValidateCharterText("", w.Reason); err != nil {
		return pa.CharterVersion{}, err
	}
	if err := pa.ValidateCharterWorkflow(w.Workflow, nil); err != nil {
		return pa.CharterVersion{}, err
	}
	if w.IdempotencyKey == "" || w.ExpectedVersion < 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "idempotency_key")
	}
	if w.Author == "" {
		w.Author = pa.CharterByPerson
	}
	flow := pa.CharterWorkflow{States: append([]pa.WorkflowState{}, w.Workflow.States...), Transitions: append([]pa.WorkflowTransition{}, w.Workflow.Transitions...)}
	encoded, err := encodeWorkflow(&flow)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	request := charterDigest("workflow", string(w.Author), strconv.FormatInt(w.ExpectedVersion, 10), encoded, w.Reason)
	write := CharterWrite{MachineID: w.MachineID, ProjectID: w.ProjectID, ExpectedVersion: w.ExpectedVersion, Reason: w.Reason, Author: w.Author, IdempotencyKey: w.IdempotencyKey, At: w.At,
		shape: func(tx *sql.Tx, current pa.CharterVersion) (charterBody, error) {
			if err := checkWorkflowBots(ctx, tx, w.MachineID, w.ProjectID, flow); err != nil {
				return charterBody{}, err
			}
			body := bodyOf(current)
			body.Workflow = &flow
			return body, nil
		}}
	return s.insertCharter(ctx, write, request, nil)
}

// checkWorkflowBots refuses a workflow naming a Bot that is not a visible
// Bot of the Project on this machine, inside the write's transaction.
func checkWorkflowBots(ctx context.Context, r reader, machine, project string, w pa.CharterWorkflow) error {
	bots, err := charterBots(ctx, r, machine)
	if err != nil {
		return err
	}
	visible := map[string]bool{}
	for _, b := range bots {
		if b.ProjectID == project && !b.Hidden {
			visible[b.BotID] = true
		}
	}
	return pa.ValidateCharterWorkflow(w, func(id string) bool { return visible[id] })
}

// ProposeCharter records a lead proposal of a new text, or of a new
// workflow (Kind workflow). The same Bot turn proposing the same thing again
// answers the existing proposal.
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
	workflow := ""
	switch p.Kind {
	case "", pa.CharterProposalText:
		p.Kind, p.Workflow = pa.CharterProposalText, nil
		p.ID = "cp-" + charterDigest(p.BotID, p.TurnID, strconv.FormatInt(p.BaseVersion, 10), p.Text)[:24]
	case pa.CharterProposalWorkflow:
		if p.Workflow == nil || p.Text != "" {
			return pa.CharterProposal{}, failure(pa.Invalid, "workflow")
		}
		if err := pa.ValidateCharterWorkflow(*p.Workflow, nil); err != nil {
			return pa.CharterProposal{}, err
		}
		var err error
		if workflow, err = encodeWorkflow(p.Workflow); err != nil {
			return pa.CharterProposal{}, err
		}
		p.ID = "cp-" + charterDigest(p.BotID, p.TurnID, strconv.FormatInt(p.BaseVersion, 10), "workflow", workflow)[:24]
	default:
		return pa.CharterProposal{}, failure(pa.Invalid, "kind")
	}
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
	if p.Workflow != nil {
		if err = checkWorkflowBots(ctx, tx, machine, p.ProjectID, *p.Workflow); err != nil {
			return pa.CharterProposal{}, err
		}
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_charter_proposals WHERE machine=? AND project=?`, machine, p.ProjectID).Scan(&pending); err != nil {
		return pa.CharterProposal{}, err
	}
	if pending >= pa.MaxCharterProposals {
		return pa.CharterProposal{}, failure(pa.CapacityFull, "charter_proposals")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_charter_proposals(machine,project,id,base,text,summary,bot,turn,created_at,kind,workflow) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		machine, p.ProjectID, p.ID, p.BaseVersion, p.Text, p.Summary, p.BotID, p.TurnID, p.CreatedAt.UnixMilli(), string(p.Kind), workflow); err != nil {
		return pa.CharterProposal{}, err
	}
	return p, tx.Commit()
}

const charterProposalColumnList = `id,project,base,text,summary,bot,turn,created_at,kind,workflow`

func scanProposal(scan func(...any) error) (pa.CharterProposal, error) {
	var p pa.CharterProposal
	var at int64
	var workflow string
	if err := scan(&p.ID, &p.ProjectID, &p.BaseVersion, &p.Text, &p.Summary, &p.BotID, &p.TurnID, &at, &p.Kind, &workflow); err != nil {
		return pa.CharterProposal{}, err
	}
	p.CreatedAt = time.UnixMilli(at).UTC()
	w, err := decodeWorkflow(workflow)
	if err != nil {
		return pa.CharterProposal{}, err
	}
	p.Workflow = w
	if p.Kind == "" {
		p.Kind = pa.CharterProposalText
	}
	return p, nil
}

func scanProposals(rows *sql.Rows) ([]pa.CharterProposal, error) {
	defer rows.Close()
	out := []pa.CharterProposal{}
	for rows.Next() {
		p, err := scanProposal(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CharterProposals lists a Project's pending proposals, oldest first.
func (s *Store) CharterProposals(ctx context.Context, machine, project string) ([]pa.CharterProposal, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT `+charterProposalColumnList+` FROM project_assistant_charter_proposals WHERE machine=? AND project=? ORDER BY created_at,id LIMIT ?`, machine, project, pa.MaxCharterProposals)
	if err != nil {
		return nil, err
	}
	return scanProposals(rows)
}

// ApplyCharterProposal saves a pending proposal as the next version while the
// charter is still at expected and at the proposal's base, and removes it. A
// text proposal keeps the current workflow; a workflow proposal keeps the
// current text and playbooks, and its Bots must still be the Project's.
func (s *Store) ApplyCharterProposal(ctx context.Context, machine, project, id string, expected int64, idem string, at time.Time) (pa.CharterVersion, error) {
	if idem == "" || id == "" || expected < 0 {
		return pa.CharterVersion{}, failure(pa.Invalid, "charter_proposal")
	}
	request := charterDigest("apply", id, strconv.FormatInt(expected, 10))
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+charterProposalColumnList+` FROM project_assistant_charter_proposals WHERE machine=? AND project=? AND id=?`, machine, project, id).Scan)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return pa.CharterVersion{}, err
	}
	w := CharterWrite{MachineID: machine, ProjectID: project, ExpectedVersion: expected, Text: p.Text, Reason: p.Summary, Author: pa.CharterByLeadProposal, IdempotencyKey: idem, At: at}
	if p.Kind == pa.CharterProposalWorkflow && p.Workflow != nil {
		proposed := *p.Workflow
		w.shape = func(tx *sql.Tx, current pa.CharterVersion) (charterBody, error) {
			if err := checkWorkflowBots(ctx, tx, machine, project, proposed); err != nil {
				return charterBody{}, err
			}
			body := bodyOf(current)
			body.Workflow = &proposed
			return body, nil
		}
	}
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
	return charterBots(ctx, s.reader, machine)
}

func charterBots(ctx context.Context, r reader, machine string) ([]CharterBot, error) {
	rows, err := r.QueryContext(ctx, `SELECT bot,
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
