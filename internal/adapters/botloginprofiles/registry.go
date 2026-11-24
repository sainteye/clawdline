// Package botloginprofiles owns managed native login slots. Native credentials
// remain opaque; only the agent's own login/status protocol touches them.
package botloginprofiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/botprofilevault"
	secrets "github.com/sainteye/clawdline/internal/app"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Registry struct {
	db                    *sql.DB
	root, source, machine string
	native                app.LoginNative
	mu                    sync.Mutex
	running               map[string]app.LoginProcess
	closed                bool
	targets               map[string]secretTarget
	vault                 *botprofilevault.Files
	Now                   func() time.Time
	// ProbeBackoff is the first wait between check reads; tests set it to zero.
	ProbeBackoff time.Duration
	// StatusTimeout bounds one native status read whatever the native does.
	StatusTimeout time.Duration
	workers       sync.WaitGroup
}

func fail(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }
func id() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("login profile identity unavailable")
	}
	return hex.EncodeToString(b[:])
}

// New performs no native action and never reads default CLI configuration.
// account is the authenticated source account, never a request parameter.
func New(ctx context.Context, db *sql.DB, state, account, machine string, native app.LoginNative) (*Registry, error) {
	if db == nil || native == nil || account == "" || machine == "" || !filepath.IsAbs(state) {
		return nil, fail(pa.Invalid, "login_profile_source")
	}
	resolved, e := filepath.EvalSymlinks(state)
	if e != nil {
		return nil, fail(pa.Invalid, "login_profile_state")
	}
	source := sha256.Sum256([]byte(account + "\x00" + machine))
	r := &Registry{db: db, root: filepath.Join(resolved, "project-assistants", "login-profiles", hex.EncodeToString(source[:])), source: hex.EncodeToString(source[:]), machine: machine, native: native, running: map[string]app.LoginProcess{}, targets: map[string]secretTarget{}, Now: time.Now, ProbeBackoff: pa.LoginProbeBackoff, StatusTimeout: pa.MaxLoginStatusMilliseconds * time.Millisecond}
	r.vault = botprofilevault.New(r.root)
	// Schema installation is owned by the Bot store, not this constructor.
	_, e = db.ExecContext(ctx, `UPDATE project_assistant_login_profiles SET state='unknown',revision=revision+1 WHERE source=? AND state IN ('accepted','running','removing')`, r.source)
	if e != nil {
		return nil, e
	}
	return r, nil
}

func (r *Registry) get(ctx context.Context, profile string) (pa.LoginProfile, error) {
	if profile == "" || !pa.ValidLoginProfileID(profile) {
		return pa.LoginProfile{}, fail(pa.Invalid, "login_profile")
	}
	var p pa.LoginProfile
	e := r.db.QueryRowContext(ctx, `SELECT id,agent,name,state,revision FROM project_assistant_login_profiles WHERE source=? AND id=? AND removed=0`, r.source, profile).Scan(&p.ID, &p.Agent, &p.Name, &p.State, &p.Revision)
	p.Kind = "account"
	var kind string
	if e == nil {
		ke := r.db.QueryRowContext(ctx, `SELECT kind FROM project_assistant_login_kinds WHERE source=? AND id=?`, r.source, p.ID).Scan(&kind)
		if ke == nil {
			p.Kind = kind
		} else if ke == sql.ErrNoRows {
			return p, fail(pa.OutcomeUnknown, "login_profile_kind")
		} else {
			return p, ke
		}
	}
	if p.Kind == "api_key" {
		p.Variables = []string{pa.LoginKeyVariable(p.Agent)}
	}
	if e == nil && p.Kind != "account" && p.Kind != "api_key" {
		return p, fail(pa.OutcomeUnknown, "login_profile_kind")
	}
	if e == sql.ErrNoRows {
		return p, fail(pa.NotFound, "login_profile")
	}
	return p, e
}
func (r *Registry) List(ctx context.Context, agent pa.Agent) ([]pa.LoginProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agent != pa.ClaudeCode && agent != pa.Codex {
		return nil, fail(pa.Unsupported, "login_profile_agent")
	}
	rows, e := r.db.QueryContext(ctx, `SELECT p.id,p.agent,p.name,p.state,p.revision,coalesce(k.kind,'unknown') FROM project_assistant_login_profiles p LEFT JOIN project_assistant_login_kinds k ON k.source=p.source AND k.id=p.id WHERE p.source=? AND p.agent=? AND (p.removed=0 OR p.state IN ('unknown','cleanup_unknown')) ORDER BY p.id LIMIT ?`, r.source, agent, pa.MaxLoginProfiles)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []pa.LoginProfile{}
	for rows.Next() {
		var p pa.LoginProfile
		if e = rows.Scan(&p.ID, &p.Agent, &p.Name, &p.State, &p.Revision, &p.Kind); e != nil {
			return nil, e
		}
		if p.Kind == "unknown" {
			p.State = "unknown"
		}
		if p.Kind == "api_key" {
			p.Variables = []string{pa.LoginKeyVariable(p.Agent)}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// checkDirectory refuses every symlink and non-private directory component
// below the canonical state root. No project path enters this function.
func (r *Registry) slot(p pa.LoginProfile) (string, error) {
	if !pa.ValidLoginProfileID(p.ID) || p.ID == "" {
		return "", fail(pa.Invalid, "login_profile")
	}
	state := filepath.Dir(filepath.Dir(filepath.Dir(r.root)))
	path := filepath.Join(r.root, p.ID)
	rel, e := filepath.Rel(state, path)
	if e != nil || strings.HasPrefix(rel, "..") {
		return "", fail(pa.ScopeDenied, "login_profile_slot")
	}
	current := state
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if e = os.Mkdir(current, 0700); e != nil && !os.IsExist(e) {
			return "", fail(pa.Invalid, "login_profile_slot")
		}
		info, e := os.Lstat(current)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return "", fail(pa.ScopeDenied, "login_profile_slot")
		}
	}
	return path, nil
}
func (r *Registry) environment(p pa.LoginProfile) ([]string, error) {
	return r.profileEnvironment(p, true)
}
func (r *Registry) profileEnvironment(p pa.LoginProfile, withKey bool) ([]string, error) {
	path, e := r.slot(p)
	if e != nil {
		return nil, e
	}
	var env []string
	switch p.Agent {
	case pa.ClaudeCode:
		env = []string{"CLAUDE_CONFIG_DIR=" + path}
	case pa.Codex:
		env = []string{"CODEX_HOME=" + path}
	default:
		return nil, fail(pa.Unsupported, "login_profile_agent")
	}
	if p.Kind == "api_key" && withKey {
		value, e := r.vault.Get(p.ID, r.aad(p), secrets.MaxSecretHandoffBytes)
		if errors.Is(e, botprofilevault.ErrMissing) {
			return nil, fail(pa.AuthRequired, "login_profile_key_missing")
		}
		if e != nil {
			return nil, fail(pa.AuthProbeUnknown, "login_profile_vault")
		}
		defer botprofilevault.Wipe(value)
		env = append(env, pa.LoginKeyVariable(p.Agent)+"="+string(value))
	}
	return env, nil
}

func (r *Registry) selected(ctx context.Context, b pa.Bot) (pa.LoginProfile, error) {
	if r.closed {
		return pa.LoginProfile{}, fail(pa.Revoked, "login_profile_source")
	}
	if b.Key.MachineID != r.machine || b.Project.MachineID != r.machine {
		return pa.LoginProfile{}, fail(pa.ScopeDenied, "login_profile_machine")
	}
	p, e := r.get(ctx, b.LoginProfile)
	if e != nil {
		return p, e
	}
	if p.Agent != b.Agent {
		return p, fail(pa.ScopeDenied, "login_profile_agent")
	}
	if p.State != "signed_in" && p.State != "signed_out" {
		return p, fail(pa.OutcomeUnknown, "login_profile_state")
	}
	return p, nil
}
func (r *Registry) WithSelection(ctx context.Context, b pa.Bot, commit func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, step := range pa.StepChain(b) {
		if step.LoginProfile != "" {
			selected := b
			selected.Agent = step.Agent
			selected.LoginProfile = step.LoginProfile
			if _, e := r.selected(ctx, selected); e != nil {
				return e
			}
		}
	}
	return commit()
}

// Acquire persists a hold before any probe/execution. A lost cleanup receipt
// keeps the hold after restart, refusing deletion instead of guessing PIDs.
func (r *Registry) Acquire(ctx context.Context, b pa.Bot) ([]string, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	none := func() {}
	if b.LoginProfile == "" {
		return nil, none, nil
	}
	p, e := r.selected(ctx, b)
	if e != nil {
		return nil, none, e
	}
	if p.State != "signed_in" {
		return nil, none, fail(pa.AuthRejected, "login_profile_signed_out")
	}
	// A failed key read says nothing about the stored key. It is reported and
	// not written down, so the next execution reads the vault again.
	// A definitely missing key is the authoritative refusal: the profile is
	// signed out and the next step is entering a key.
	env, e := r.environment(p)
	if pa.Code(e) == pa.AuthRequired {
		r.state(p, "signed_out")
	}
	if e != nil {
		return nil, none, e
	}
	lease := id()
	_, e = r.db.ExecContext(ctx, `INSERT INTO project_assistant_login_holds(source,profile,id) VALUES(?,?,?)`, r.source, p.ID, lease)
	if e != nil {
		return nil, none, e
	}
	var once sync.Once
	return env, func() {
		once.Do(func() {
			_, _ = r.db.ExecContext(context.Background(), `DELETE FROM project_assistant_login_holds WHERE source=? AND profile=? AND id=?`, r.source, p.ID, lease)
		})
	}, nil
}

type journal struct {
	pa.Receipt
	Profile pa.LoginProfile `json:"profile"`
}

func (r *Registry) Apply(ctx context.Context, q pa.LoginProfileAction, admit func(context.Context) error) (pa.LoginProfile, error) {
	if q.Action == "submit-key" {
		return pa.LoginProfile{}, fail(pa.Invalid, "login_secret_route")
	}
	return r.apply(ctx, q, nil, admit)
}
func (r *Registry) SubmitSecret(ctx context.Context, q pa.LoginProfileAction, value []byte, admit func(context.Context) error) (pa.LoginProfile, error) {
	if q.Action != "submit-key" {
		return pa.LoginProfile{}, fail(pa.Invalid, "login_secret_action")
	}
	return r.apply(ctx, q, value, admit)
}

// apply holds the registry lock for the durable decision only. A check's
// native status or vault reads run after the lock is released, so a read that
// hangs never stops another Bot from acquiring its own profile.
func (r *Registry) apply(ctx context.Context, q pa.LoginProfileAction, secret []byte, admit func(context.Context) error) (pa.LoginProfile, error) {
	r.mu.Lock()
	p, probe, e := r.applyLocked(ctx, q, secret, admit)
	r.mu.Unlock()
	if e != nil || !probe {
		return p, e
	}
	return r.check(ctx, p, admit)
}
func (r *Registry) applyLocked(ctx context.Context, q pa.LoginProfileAction, secret []byte, admit func(context.Context) error) (pa.LoginProfile, bool, error) {
	if r.closed {
		return pa.LoginProfile{}, false, fail(pa.Revoked, "login_profile_source")
	}
	if q.Command.Key.MachineID != r.machine {
		return pa.LoginProfile{}, false, fail(pa.ScopeDenied, "login_profile_machine")
	}
	var data []byte
	e := r.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=? AND operation=? AND command_key=?`, q.Command.Key.MachineID, q.Command.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey).Scan(&data)
	if e == nil {
		var j journal
		if json.Unmarshal(data, &j) != nil {
			return pa.LoginProfile{}, false, fail(pa.OutcomeUnknown, "login_profile_receipt")
		}
		if _, e = pa.DecideReplay(q.Command, &j.Receipt); e != nil {
			return pa.LoginProfile{}, false, e
		}
		return j.Profile, false, nil
	}
	if e != sql.ErrNoRows {
		return pa.LoginProfile{}, false, e
	}
	if e = admit(ctx); e != nil {
		return pa.LoginProfile{}, false, e
	}
	if q.Agent != pa.ClaudeCode && q.Agent != pa.Codex {
		return pa.LoginProfile{}, false, fail(pa.Unsupported, "login_profile_agent")
	}
	if !utf8.ValidString(q.Name) || utf8.RuneCountInString(q.Name) > pa.MaxNameRunes || strings.ContainsAny(q.Name, "\x00\r\n") {
		return pa.LoginProfile{}, false, fail(pa.Invalid, "login_profile_name")
	}
	var p pa.LoginProfile
	if q.Action == "create" {
		if q.ProfileID != "" || q.Revision != 0 || strings.TrimSpace(q.Name) == "" {
			return p, false, fail(pa.Invalid, "login_profile_create")
		}
		var n int
		if e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_profiles WHERE source=? AND (removed=0 OR state IN ('unknown','cleanup_unknown'))`, r.source).Scan(&n); e != nil {
			return p, false, e
		}
		if n >= pa.MaxLoginProfiles {
			return p, false, fail(pa.CapacityFull, "login_profiles")
		}
		kind := q.Kind
		if kind == "" {
			kind = "account"
		}
		if kind != "account" && kind != "api_key" {
			return p, false, fail(pa.Invalid, "login_profile_kind")
		}
		p = pa.LoginProfile{ID: id(), Agent: q.Agent, Kind: kind, Name: q.Name, State: "signed_out", Revision: 1}
		if kind == "api_key" {
			p.Variables = []string{pa.LoginKeyVariable(p.Agent)}
		}
		// Reserve the opaque identity before creating its empty slot. A failed slot
		// setup preserves an unknown profile, never deletes unexamined native bytes.
	} else {
		p, e = r.get(ctx, q.ProfileID)
		if e != nil {
			return p, false, e
		}
		if p.Agent != q.Agent {
			return p, false, fail(pa.ScopeDenied, "login_profile_agent")
		}
		if p.Revision != q.Revision {
			return p, false, fail(pa.StaleVersion, "login_profile")
		}
		p.Revision++
	}
	removed := false
	switch q.Action {
	case "create":
	case "rename":
		if strings.TrimSpace(q.Name) == "" {
			return p, false, fail(pa.Invalid, "login_profile_name")
		}
		p.Name = q.Name
	case "prepare-key", "submit-key", "start":
		if (q.Action == "start") != (p.Kind == "account") {
			return p, false, fail(pa.Invalid, "login_profile_kind")
		}
		// An unknown key profile accepts a replacement key: the person's new key
		// is the explicit way out. An unknown account still needs a check first,
		// because an earlier native sign-in may still be writing its slot.
		if p.State == "accepted" || p.State == "running" || p.State == "unknown" && p.Kind == "account" {
			return p, false, fail(pa.OutcomeUnknown, "login_profile_state")
		}
		// A selected profile is an immutable credential identity. Changing its
		// account requires deselecting it first, which advances each Bot's
		// instruction revision before that profile can be selected again.
		// The registry lock also serializes this check with execution probes.
		var references, holds int
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND (json_extract(CAST(data AS TEXT),'$.Bot.login_profile')=? OR EXISTS(SELECT 1 FROM json_each(json_extract(CAST(data AS TEXT),'$.Bot.fallback_steps')) f WHERE json_extract(f.value,'$.login_profile')=?))`, r.machine, p.ID, p.ID).Scan(&references)
		if e != nil {
			return p, false, e
		}
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_holds WHERE source=? AND profile=?`, r.source, p.ID).Scan(&holds)
		if e != nil {
			return p, false, e
		}
		if references != 0 || holds != 0 {
			return p, false, fail(pa.ScopeDenied, "login_profile_in_use")
		}
		if q.Action == "prepare-key" {
			for target, v := range r.targets {
				if !r.Now().Before(v.expires) {
					delete(r.targets, target)
				}
			}
			if len(r.targets) >= pa.MaxLoginSecretTargets {
				return p, false, fail(pa.CapacityFull, "login_secret_targets")
			}
			p.Handoff = &pa.LoginSecretTarget{ID: id(), Expires: r.Now().Add(secrets.MaxSecretHandoffSeconds * time.Second)}
		} else if q.Action == "submit-key" {
			target, ok := r.targets[q.Target]
			if !ok || !r.Now().Before(target.expires) || !target.matches(q) {
				return p, false, fail(pa.Expired, "login_secret_target")
			}
			if len(secret) == 0 || len(secret) > secrets.MaxSecretHandoffBytes || strings.ContainsAny(string(secret), "\x00\r\n") {
				return p, false, fail(pa.Invalid, "login_secret_value")
			}
			p.State = "accepted"
		} else {
			p.State = "accepted"
		}
	case "cancel":
		if p.Kind != "account" {
			return p, false, fail(pa.Invalid, "login_profile_kind")
		}
		if r.running[p.ID] == nil {
			return p, false, fail(pa.OutcomeUnknown, "login_profile_process")
		}
	case "check":
		// Check is the way out of unknown: it never borrows the old receipt and
		// settles only from a fresh read. A sign-in this daemon is running is
		// still settled by that sign-in finishing.
		if r.running[p.ID] != nil {
			return p, false, fail(pa.OutcomeUnknown, "login_profile_state")
		}
	case "remove":
		var references, holds int
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND (json_extract(CAST(data AS TEXT),'$.Bot.login_profile')=? OR EXISTS(SELECT 1 FROM json_each(json_extract(CAST(data AS TEXT),'$.Bot.fallback_steps')) f WHERE json_extract(f.value,'$.login_profile')=?))`, r.machine, p.ID, p.ID).Scan(&references)
		if e != nil {
			return p, false, e
		}
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_holds WHERE source=? AND profile=?`, r.source, p.ID).Scan(&holds)
		if e != nil {
			return p, false, e
		}
		// An unknown key profile has no native process: with no execution hold
		// its key and slot can be removed. An unknown account may still have a
		// native sign-in writing its slot and must be checked first.
		if references != 0 || holds != 0 || r.running[p.ID] != nil || p.State == "unknown" && p.Kind == "account" || p.State == "running" || p.State == "accepted" {
			return p, false, fail(pa.ScopeDenied, "login_profile_in_use")
		}
		removed = true
		p.State = "removing"
	default:
		return p, false, fail(pa.Invalid, "login_profile_action")
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return p, false, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_login_profiles(source,id,agent,name,state,revision,removed) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source,id) DO UPDATE SET name=excluded.name,state=excluded.state,revision=excluded.revision,removed=excluded.removed`, r.source, p.ID, p.Agent, p.Name, p.State, p.Revision, removed)
	if e != nil {
		return p, false, e
	}
	if q.Action == "create" {
		if _, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_login_kinds(source,id,kind) VALUES(?,?,?)`, r.source, p.ID, p.Kind); e != nil {
			return p, false, e
		}
	}
	j := journal{Receipt: pa.Receipt{ID: id(), Command: q.Command, State: pa.EffectAccepted, ResourceID: p.ID}, Profile: p}
	data, e = json.Marshal(j)
	if e != nil {
		return p, false, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_receipts(machine,bot,operation,command_key,receipt_id,data) VALUES(?,?,?,?,?,?)`, q.Command.Key.MachineID, q.Command.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey, j.ID, data)
	if e != nil {
		return p, false, e
	}
	if e = tx.Commit(); e != nil {
		return p, false, fail(pa.OutcomeUnknown, "login_profile_commit")
	}
	if q.Action == "rename" {
		return p, false, nil
	}
	if q.Action == "check" {
		return p, true, nil
	}
	if q.Action == "prepare-key" {
		r.targets[p.Handoff.ID] = secretTarget{key: q.Command.Key, fence: q.Fence, profile: p.ID, revision: p.Revision, authority: q.Authority, agent: p.Agent, expires: p.Handoff.Expires}
		return p, false, nil
	}
	if q.Action == "submit-key" {
		delete(r.targets, q.Target)
		if e = admit(ctx); e != nil {
			r.state(p, "unknown")
			return p, false, e
		}
		if _, e = r.profileEnvironment(p, false); e == nil {
			e = r.vault.Put(p.ID, r.aad(p), secret, secrets.MaxSecretHandoffBytes)
		}
		if e != nil {
			r.state(p, "unknown")
			return p, false, fail(pa.OutcomeUnknown, "login_profile_vault")
		}
		if e = admit(ctx); e != nil {
			r.state(p, "unknown")
			return p, false, e
		}
		r.state(p, "signed_in")
		return p, false, nil
	}
	if p.Kind == "api_key" && (q.Action == "create" || q.Action == "remove") {
		if q.Action == "remove" {
			if e = r.vault.Remove(p.ID); e != nil {
				r.state(p, "cleanup_unknown")
				return p, false, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
			}
			path, e := r.slot(p)
			if e != nil || os.RemoveAll(path) != nil {
				r.state(p, "cleanup_unknown")
				return p, false, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
			}
			r.state(p, "removed")
		} else {
			_, e = r.profileEnvironment(p, false)
		}
		if e != nil {
			r.state(p, "unknown")
			return p, false, fail(pa.AuthProbeUnknown, "login_profile_vault")
		}
		return p, false, nil
	}
	env, e := r.environment(p)
	if e != nil {
		r.state(p, "unknown")
		return p, false, e
	}
	if e = admit(ctx); e != nil {
		r.state(p, "unknown")
		return p, false, e
	}
	switch q.Action {
	case "remove":
		if e = r.native.SignOut(ctx, p.Agent, env); e != nil {
			r.state(p, "cleanup_unknown")
			return p, false, fail(pa.ExecutionEffectUnknown, "login_profile_sign_out")
		}
		path, e := r.slot(p)
		if e != nil {
			r.state(p, "cleanup_unknown")
			return p, false, e
		}
		if e = os.RemoveAll(path); e != nil {
			r.state(p, "cleanup_unknown")
			return p, false, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
		}
		r.state(p, "removed")
	case "start":
		process, e := r.native.Start(ctx, p.Agent, env)
		if e != nil {
			r.state(p, "unknown")
			return p, false, fail(pa.OutcomeUnknown, "login_profile_start")
		}
		r.running[p.ID] = process
		r.state(p, "running")
		r.workers.Add(1)
		go r.finish(p, env, process)
	case "cancel":
		if e = r.running[p.ID].Close(ctx); e != nil {
			r.state(p, "unknown")
			return p, false, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
		}
	}
	// Return the durable acceptance snapshot. Replays return these exact bytes;
	// GET observes subsequent running/signed-in facts, never an invented success.
	return p, false, nil
}

// check settles a profile from one fresh read, retried a bounded number of
// times. It runs without the registry lock and each native read has its own
// deadline. Only an answer changes the stored state: signed in (a readable key
// or a native signed-in status) or the native signed-out status, which is the
// authoritative refusal. An interrupted or timed-out read leaves the previous
// state in place, so a failure is never cached and the next check can settle.
// The answer is written only if nothing changed the profile while it was read.
func (r *Registry) check(ctx context.Context, p pa.LoginProfile, admit func(context.Context) error) (pa.LoginProfile, error) {
	if e := admit(ctx); e != nil {
		return p, e
	}
	field := "login_profile_status"
	var env []string
	if p.Kind == "api_key" {
		field = "login_profile_vault"
	} else {
		// The slot directory is created under the lock, as every other caller does.
		r.mu.Lock()
		var e error
		env, e = r.profileEnvironment(p, false)
		r.mu.Unlock()
		if e != nil {
			return p, e
		}
	}
	settled := ""
	wait := r.ProbeBackoff
	for attempt := 1; ; attempt++ {
		var e error
		if p.Kind == "api_key" {
			var raw []byte
			raw, e = r.vault.Get(p.ID, r.aad(p), secrets.MaxSecretHandoffBytes)
			botprofilevault.Wipe(raw)
			if e == nil {
				settled = "signed_in"
			} else if errors.Is(e, botprofilevault.ErrMissing) {
				settled, e = "signed_out", nil
			}
		} else {
			settled, e = r.status(ctx, p.Agent, env)
		}
		if e == nil && (settled == "signed_in" || settled == "signed_out") {
			break
		}
		if attempt >= pa.MaxLoginProbeAttempts || ctx.Err() != nil {
			return p, fail(pa.AuthProbeUnknown, field)
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return p, fail(pa.AuthProbeUnknown, field)
			case <-timer.C:
			}
			wait = min(2*wait, pa.MaxLoginProbeBackoffMilliseconds*time.Millisecond)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return p, fail(pa.Revoked, "login_profile_source")
	}
	if r.running[p.ID] != nil {
		return p, fail(pa.OutcomeUnknown, "login_profile_state")
	}
	written, e := r.db.ExecContext(context.Background(), `UPDATE project_assistant_login_profiles SET state=?,revision=revision+1 WHERE source=? AND id=? AND revision=? AND removed=0`, settled, r.source, p.ID, p.Revision)
	if e != nil {
		return p, fail(pa.AuthProbeUnknown, field)
	}
	if n, e := written.RowsAffected(); e != nil || n != 1 {
		return p, fail(pa.StaleVersion, "login_profile")
	}
	return p, nil
}

// status is one native status read with its own deadline. A native that does
// not honour cancellation is abandoned at the deadline instead of waited for.
func (r *Registry) status(ctx context.Context, agent pa.Agent, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.StatusTimeout)
	defer cancel()
	type answer struct {
		state string
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		state, e := r.native.Status(ctx, agent, env)
		done <- answer{state, e}
	}()
	select {
	case a := <-done:
		return a.state, a.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *Registry) state(p pa.LoginProfile, state string) {
	_, _ = r.db.ExecContext(context.Background(), `UPDATE project_assistant_login_profiles SET state=?,revision=revision+1 WHERE source=? AND id=?`, state, r.source, p.ID)
}
func (r *Registry) finish(p pa.LoginProfile, env []string, process app.LoginProcess) {
	defer r.workers.Done()
	e := process.Wait(context.Background())
	cleanup := process.Close(context.Background())
	state := "unknown"
	if e == nil && cleanup == nil {
		if v, err := r.status(context.Background(), p.Agent, env); err == nil {
			state = v
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cleanup == nil {
		delete(r.running, p.ID)
	}
	r.state(p, state)
}
func (r *Registry) Close(ctx context.Context) error   { return r.stop(ctx, true) }
func (r *Registry) Suspend(ctx context.Context) error { return r.stop(ctx, false) }
func (r *Registry) stop(ctx context.Context, closed bool) error {
	r.mu.Lock()
	r.closed = r.closed || closed
	processes := make([]app.LoginProcess, 0, len(r.running))
	for _, p := range r.running {
		processes = append(processes, p)
	}
	r.mu.Unlock()
	for _, p := range processes {
		if e := p.Close(ctx); e != nil {
			return fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
		}
	}
	done := make(chan struct{})
	go func() { r.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
	}
}

// CheckProject prevents managed slots below any selected Project, including a
// non-repository place. The caller supplies the canonical source resolution.
func (r *Registry) CheckProject(directory string) error {
	project, e := filepath.EvalSymlinks(directory)
	if e != nil {
		return fail(pa.ScopeDenied, "login_profile_project")
	}
	rel, e := filepath.Rel(project, r.root)
	if e != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fail(pa.ScopeDenied, "login_profile_project_overlap")
	}
	return nil
}

// Quarantine records an unresolved native owner without releasing its durable
// hold. Future native effects must not borrow a previous signed-in receipt.
func (r *Registry) Quarantine(ctx context.Context, b pa.Bot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, e := r.get(ctx, b.LoginProfile)
	if e != nil {
		return e
	}
	if b.Key.MachineID != r.machine || p.Agent != b.Agent {
		return fail(pa.ScopeDenied, "login_profile_quarantine")
	}
	_, e = r.db.ExecContext(ctx, `UPDATE project_assistant_login_profiles SET state='unknown',revision=revision+1 WHERE source=? AND id=?`, r.source, p.ID)
	return e
}
