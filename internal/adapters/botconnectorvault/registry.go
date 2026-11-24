// Package botconnectorvault owns connector-specific encrypted credentials and
// expiring person-to-source submission targets. It does not launch connectors.
package botconnectorvault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botprofilevault"
	secrets "github.com/sainteye/clawdline/internal/app"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Scope struct {
	Bot       pa.Bot
	Ref       pa.ExtensionRef
	Authority string
}

type Action = pa.ConnectorCredentialAction
type Result = pa.ConnectorCredential

type target struct {
	scope, authority string
	fence            pa.Fence
	revision         int64
	expires          time.Time
}

type record struct {
	id, scope string
	Result
}

type journal struct {
	pa.Receipt
	Scope  string `json:"scope"`
	Result Result `json:"credential"`
}

type Registry struct {
	db                    *sql.DB
	root, source, machine string
	vault                 *botprofilevault.Files
	mu                    sync.Mutex
	targets               map[string]target
	closed                bool
	Now                   func() time.Time
}

func fail(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

func opaque() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fail(pa.OutcomeUnknown, "connector_identity")
	}
	return hex.EncodeToString(value[:]), nil
}

// New receives authenticated source identity from composition, never HTTP.
// Schema creation belongs to the feature installer. A restarted accepted write
// remains unknown until the person enters a replacement, and in-memory targets
// cannot survive the source restart.
func New(ctx context.Context, db *sql.DB, state, account, machine string) (*Registry, error) {
	if db == nil || account == "" || machine == "" || !filepath.IsAbs(state) {
		return nil, fail(pa.Invalid, "connector_source")
	}
	resolved, err := filepath.EvalSymlinks(state)
	if err != nil {
		return nil, fail(pa.ScopeDenied, "connector_state")
	}
	source := sha256.Sum256([]byte(account + "\x00" + machine))
	r := &Registry{db: db, source: hex.EncodeToString(source[:]), machine: machine, targets: map[string]target{}, Now: time.Now}
	r.root = filepath.Join(resolved, "project-assistants", "connector-credentials", r.source)
	r.vault = botprofilevault.NewConnector(r.root)
	if _, err = db.ExecContext(ctx, `UPDATE project_assistant_connector_credentials SET state='unknown',revision=revision+1 WHERE source=? AND state='accepted'`, r.source); err != nil {
		return nil, err
	}
	if _, err = db.ExecContext(ctx, `UPDATE project_assistant_connector_credentials SET state='unknown',revision=revision+1 WHERE source=? AND state='ready' AND EXISTS(SELECT 1 FROM project_assistant_connector_holds h WHERE h.source=project_assistant_connector_credentials.source AND h.bot=project_assistant_connector_credentials.bot AND h.connector=project_assistant_connector_credentials.connector AND h.scope=project_assistant_connector_credentials.scope AND h.revision=project_assistant_connector_credentials.revision)`, r.source); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) scope(s Scope) (string, error) {
	if r.closed {
		return "", fail(pa.Revoked, "connector_source")
	}
	if s.Bot.Key.MachineID != r.machine || s.Bot.Project.MachineID != r.machine || s.Bot.Key.BotID == "" {
		return "", fail(pa.ScopeDenied, "connector_machine")
	}
	if err := pa.ValidateScope(s.Bot.Project); err != nil {
		return "", err
	}
	if s.Bot.Agent != pa.Codex && s.Bot.Agent != pa.ClaudeCode {
		return "", fail(pa.Unsupported, "connector_agent")
	}
	if !strings.HasPrefix(s.Ref.ID, "connector:") || pa.ValidateExtensions(pa.ExtensionSelection{Connectors: []pa.ExtensionRef{s.Ref}}) != nil {
		return "", fail(pa.Invalid, "connector_reference")
	}
	if _, err := hex.DecodeString(s.Ref.Fingerprint); err != nil {
		return "", fail(pa.Invalid, "connector_reference")
	}
	// Full source, Project, Bot, Agent and current definition fingerprint own the
	// encrypted record. Authority epochs own targets, not the durable credential.
	data, err := json.Marshal(struct {
		Source  string
		Key     pa.BotKey
		Project pa.ProjectScope
		Agent   pa.Agent
		Ref     pa.ExtensionRef
	}{r.source, s.Bot.Key, s.Bot.Project, s.Bot.Agent, s.Ref})
	if err != nil {
		return "", fail(pa.Invalid, "connector_scope")
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

func (r *Registry) read(ctx context.Context, s Scope) (record, error) {
	var p record
	err := r.db.QueryRowContext(ctx, `SELECT id,scope,state,revision FROM project_assistant_connector_credentials WHERE source=? AND bot=? AND connector=?`, r.source, s.Bot.Key.BotID, s.Ref.ID).Scan(&p.id, &p.scope, &p.State, &p.Revision)
	if err == sql.ErrNoRows {
		return record{Result: Result{State: "missing"}}, nil
	}
	if err == nil {
		if len(p.id) != 32 || len(p.scope) != 64 || p.Revision < 1 {
			return p, fail(pa.OutcomeUnknown, "connector_credential_record")
		}
		if _, err = hex.DecodeString(p.id); err != nil {
			return p, fail(pa.OutcomeUnknown, "connector_credential_record")
		}
		if _, err = hex.DecodeString(p.scope); err != nil {
			return p, fail(pa.OutcomeUnknown, "connector_credential_record")
		}
		switch p.State {
		case "prepared", "accepted", "ready", "removed", "unknown", "cleanup_unknown":
		default:
			return p, fail(pa.OutcomeUnknown, "connector_credential_record")
		}
	}
	return p, err
}

func (r *Registry) aad(p record) []byte {
	return []byte("clawdline.bot-connector-credential.v1\x00" + r.source + "\x00" + p.id + "\x00" + p.scope)
}

// directory creates only fixed private components below the canonical state.
func (r *Registry) directory() error {
	state := filepath.Dir(filepath.Dir(filepath.Dir(r.root)))
	rel, err := filepath.Rel(state, r.root)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fail(pa.ScopeDenied, "connector_vault")
	}
	current := state
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err = os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return fail(pa.OutcomeUnknown, "connector_vault")
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return fail(pa.ScopeDenied, "connector_vault")
		}
	}
	return nil
}

func (r *Registry) expire() {
	for id, t := range r.targets {
		if !r.Now().Before(t.expires) {
			delete(r.targets, id)
		}
	}
}

func (r *Registry) SecretTargetUsage() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expire()
	return len(r.targets)
}

// CredentialUsage counts retained identities, including revoked/unknown bytes.
// An unreadable database is an error, never an invented zero allocation.
func (r *Registry) CredentialUsage(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_connector_credentials WHERE source=?`, r.source).Scan(&count)
	return count, err
}

func (r *Registry) HoldUsage(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_connector_holds WHERE source=?`, r.source).Scan(&count)
	return count, err
}

// Status reveals only current metadata. A changed definition never inherits a
// credential issued to an earlier fingerprint.
func (r *Registry) Status(ctx context.Context, s Scope) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	scope, err := r.scope(s)
	if err != nil {
		return Result{}, err
	}
	p, err := r.read(ctx, s)
	if err == nil && p.id != "" && p.scope != scope {
		return Result{State: "missing"}, nil
	}
	return p.Result, err
}

// Apply journals a fixed operation before storing any secret bytes. A lost
// response replay acknowledges that operation without inspecting, comparing or
// replacing the newly supplied value. Values never enter the receipt digest.
func (r *Registry) Apply(ctx context.Context, s Scope, q Action, value []byte, secret bool, admit func(context.Context) error) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	scope, err := r.scope(s)
	if err != nil {
		return Result{}, err
	}
	if admit == nil || q.Command.Key != s.Bot.Key || q.Ref != s.Ref || q.Authority != s.Authority || q.Fence.Key != s.Bot.Key || q.Fence.Project != s.Bot.Project || q.Fence.ConversationID != s.Bot.ConversationID {
		return Result{}, fail(pa.ScopeDenied, "connector_target_scope")
	}
	if q.Command.Operation != pa.ConnectorCredentialOperation {
		return Result{}, fail(pa.Invalid, "connector_operation")
	}
	if _, err = pa.DecideReplay(q.Command, nil); err != nil {
		return Result{}, err
	}
	var data []byte
	err = r.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=? AND operation=? AND command_key=?`, r.machine, s.Bot.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey).Scan(&data)
	if err == nil {
		var j journal
		if json.Unmarshal(data, &j) != nil || j.Scope != scope {
			return Result{}, fail(pa.OutcomeUnknown, "connector_receipt")
		}
		if _, err = pa.DecideReplay(q.Command, &j.Receipt); err != nil {
			return Result{}, err
		}
		return j.Result, nil
	}
	if err != sql.ErrNoRows {
		return Result{}, err
	}
	if err = admit(ctx); err != nil {
		return Result{}, err
	}
	if secret != (q.Action == "submit") || (q.Action != "prepare" && q.Action != "submit" && q.Action != "remove") {
		return Result{}, fail(pa.Invalid, "connector_secret_action")
	}
	p, err := r.read(ctx, s)
	if err != nil {
		return Result{}, err
	}
	oldRevision := p.Revision
	if p.id == "" {
		var count int
		if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_connector_credentials WHERE source=?`, r.source).Scan(&count); err != nil {
			return Result{}, err
		}
		if count >= pa.MaxConfigurationChoices {
			return Result{}, fail(pa.CapacityFull, "connector_credentials")
		}
		if p.id, err = opaque(); err != nil {
			return Result{}, err
		}
	}
	if q.Action == "submit" {
		t, found := r.targets[q.Target]
		if !found || !r.Now().Before(t.expires) || t.scope != scope || t.authority != q.Authority || t.fence != q.Fence || t.revision != p.Revision || p.scope != scope || p.State != "prepared" {
			return Result{}, fail(pa.Expired, "connector_secret_target")
		}
		if len(value) == 0 || len(value) > secrets.MaxSecretHandoffBytes || strings.ContainsAny(string(value), "\x00\r\n") {
			return Result{}, fail(pa.Invalid, "connector_secret_value")
		}
	} else if q.Action == "prepare" && p.State == "accepted" {
		return Result{}, fail(pa.OutcomeUnknown, "connector_credential")
	}
	// An unknown credential (a restart during a write or a held execution, or an
	// uncertain cleanup) accepts a replacement from the person. That is its way
	// out: the new revision revokes every earlier capture through Check, and the
	// uncertain holds stay recorded. Without a replacement it is never lent.
	p.scope = scope
	p.Revision++
	p.Handoff = nil
	switch q.Action {
	case "prepare":
		r.expire()
		if len(r.targets) >= pa.MaxLoginSecretTargets {
			return Result{}, fail(pa.CapacityFull, "connector_secret_targets")
		}
		id, err := opaque()
		if err != nil {
			return Result{}, err
		}
		p.State = "prepared"
		p.Handoff = &pa.LoginSecretTarget{ID: id, Expires: r.Now().Add(secrets.MaxSecretHandoffSeconds * time.Second)}
	case "submit":
		p.State = "accepted"
	case "remove":
		// Revocation is immediate. Preserve encrypted bytes because a captured
		// process or uncertain cleanup may still own this credential identity.
		if p.State == "accepted" || p.State == "unknown" || p.State == "cleanup_unknown" {
			p.State = "cleanup_unknown"
		} else {
			p.State = "removed"
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	changed, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_connector_credentials(source,bot,connector,id,scope,state,revision) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source,bot,connector) DO UPDATE SET scope=excluded.scope,state=excluded.state,revision=excluded.revision WHERE project_assistant_connector_credentials.revision=?`, r.source, s.Bot.Key.BotID, s.Ref.ID, p.id, p.scope, p.State, p.Revision, oldRevision)
	if err != nil {
		return Result{}, err
	}
	if n, err := changed.RowsAffected(); err != nil || n != 1 {
		return Result{}, fail(pa.StaleVersion, "connector_credential")
	}
	id, err := opaque()
	if err != nil {
		return Result{}, err
	}
	j := journal{Receipt: pa.Receipt{ID: id, Command: q.Command, State: pa.EffectAccepted, ResourceID: s.Ref.ID}, Scope: scope, Result: p.Result}
	data, err = json.Marshal(j)
	if err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_receipts(machine,bot,operation,command_key,receipt_id,data) VALUES(?,?,?,?,?,?)`, r.machine, s.Bot.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey, id, data); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, fail(pa.OutcomeUnknown, "connector_credential_commit")
	}
	for id, t := range r.targets {
		if t.scope == scope {
			delete(r.targets, id)
		}
	}
	if q.Action == "prepare" {
		r.targets[p.Handoff.ID] = target{scope: scope, authority: q.Authority, fence: q.Fence, revision: p.Revision, expires: p.Handoff.Expires}
		return p.Result, nil
	}
	if q.Action == "remove" {
		return p.Result, nil
	}
	unknown := func() (Result, error) {
		_, _ = r.db.ExecContext(context.Background(), `UPDATE project_assistant_connector_credentials SET state='unknown' WHERE source=? AND bot=? AND connector=? AND revision=?`, r.source, s.Bot.Key.BotID, s.Ref.ID, p.Revision)
		return p.Result, fail(pa.OutcomeUnknown, "connector_credential_vault")
	}
	if err = admit(ctx); err != nil {
		return unknown()
	}
	if r.directory() != nil || r.vault.Put(p.id, r.aad(p), value, secrets.MaxSecretHandoffBytes) != nil {
		return unknown()
	}
	if err = admit(ctx); err != nil {
		return unknown()
	}
	result, err := r.db.ExecContext(ctx, `UPDATE project_assistant_connector_credentials SET state='ready' WHERE source=? AND bot=? AND connector=? AND revision=? AND state='accepted'`, r.source, s.Bot.Key.BotID, s.Ref.ID, p.Revision)
	if err != nil {
		return unknown()
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return unknown()
	}
	// Return the committed acceptance receipt. Status establishes stored state.
	return p.Result, nil
}

// Get is source-only and returns a disposable capture, never an HTTP response.
// A backend must Check its captured revision before effects and publication.
func (r *Registry) Get(ctx context.Context, s Scope) ([]byte, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.get(ctx, s)
}

func (r *Registry) get(ctx context.Context, s Scope) ([]byte, int64, error) {
	scope, err := r.scope(s)
	if err != nil {
		return nil, 0, err
	}
	p, err := r.read(ctx, s)
	if err != nil {
		return nil, 0, err
	}
	if p.State != "ready" || p.scope != scope {
		if p.State == "accepted" || p.State == "unknown" || p.State == "cleanup_unknown" {
			return nil, 0, fail(pa.OutcomeUnknown, "connector_credential")
		}
		return nil, 0, fail(pa.AuthRequired, "connector_credential")
	}
	if err = r.directory(); err != nil {
		return nil, 0, err
	}
	value, err := r.vault.Get(p.id, r.aad(p), secrets.MaxSecretHandoffBytes)
	if err != nil {
		return nil, 0, fail(pa.AuthProbeUnknown, "connector_credential_vault")
	}
	return value, p.Revision, nil
}

// Acquire persists the execution's hold before returning a credential capture.
// A restart cannot infer verified cleanup from the disappearance of an owner.
func (r *Registry) Acquire(ctx context.Context, s Scope) ([]byte, int64, func(context.Context) error, func(context.Context) error, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, revision, err := r.get(ctx, s)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	scope, err := r.scope(s)
	if err != nil {
		botprofilevault.Wipe(value)
		return nil, 0, nil, nil, err
	}
	var count int
	if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_connector_holds WHERE source=?`, r.source).Scan(&count); err != nil {
		botprofilevault.Wipe(value)
		return nil, 0, nil, nil, err
	}
	if count >= pa.MaxLoginSecretTargets {
		botprofilevault.Wipe(value)
		return nil, 0, nil, nil, fail(pa.CapacityFull, "connector_credential_holds")
	}
	id, err := opaque()
	if err != nil {
		botprofilevault.Wipe(value)
		return nil, 0, nil, nil, err
	}
	if _, err = r.db.ExecContext(ctx, `INSERT INTO project_assistant_connector_holds(source,bot,connector,scope,revision,id) VALUES(?,?,?,?,?,?)`, r.source, s.Bot.Key.BotID, s.Ref.ID, scope, revision, id); err != nil {
		botprofilevault.Wipe(value)
		return nil, 0, nil, nil, err
	}
	uncertain, released := false, false
	release := func(ctx context.Context) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if uncertain {
			return fail(pa.ExecutionEffectUnknown, "connector_credential_hold")
		}
		if released {
			return nil
		}
		_, err := r.db.ExecContext(ctx, `DELETE FROM project_assistant_connector_holds WHERE source=? AND bot=? AND connector=? AND scope=? AND revision=? AND id=?`, r.source, s.Bot.Key.BotID, s.Ref.ID, scope, revision, id)
		if err == nil {
			released = true
		}
		return err
	}
	quarantine := func(ctx context.Context) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if released {
			return nil
		}
		uncertain = true
		_, err := r.db.ExecContext(ctx, `UPDATE project_assistant_connector_credentials SET state='unknown',revision=revision+1 WHERE source=? AND bot=? AND connector=? AND scope=? AND revision=?`, r.source, s.Bot.Key.BotID, s.Ref.ID, scope, revision)
		return err
	}
	return value, revision, release, quarantine, nil
}

func (r *Registry) Check(ctx context.Context, s Scope, revision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	scope, err := r.scope(s)
	if err != nil {
		return err
	}
	p, err := r.read(ctx, s)
	if err != nil {
		return err
	}
	if p.scope != scope || p.State != "ready" || p.Revision != revision {
		return fail(pa.Revoked, "connector_credential")
	}
	return ctx.Err()
}

// Quarantine preserves an identity whose owned execution cleanup is uncertain.
// A newer revision has already revoked the old capture and is never rewritten.
func (r *Registry) Quarantine(ctx context.Context, s Scope, revision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	scope, err := r.scope(s)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE project_assistant_connector_credentials SET state='unknown',revision=revision+1 WHERE source=? AND bot=? AND connector=? AND scope=? AND revision=?`, r.source, s.Bot.Key.BotID, s.Ref.ID, scope, revision)
	return err
}

func (r *Registry) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	clear(r.targets)
	return nil
}

// Suspend withdraws unfinished delivery targets while leaving the source-owned
// registry usable after the independently controlled feature is enabled again.
func (r *Registry) Suspend(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.targets)
	return nil
}
