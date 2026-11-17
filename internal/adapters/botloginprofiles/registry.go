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
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	workers               sync.WaitGroup
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
	r := &Registry{db: db, root: filepath.Join(resolved, "project-assistants", "login-profiles", hex.EncodeToString(source[:])), source: hex.EncodeToString(source[:]), machine: machine, native: native, running: map[string]app.LoginProcess{}}
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
	rows, e := r.db.QueryContext(ctx, `SELECT id,agent,name,state,revision FROM project_assistant_login_profiles WHERE source=? AND agent=? AND (removed=0 OR state IN ('unknown','cleanup_unknown')) ORDER BY id LIMIT ?`, r.source, agent, pa.MaxLoginProfiles)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []pa.LoginProfile{}
	for rows.Next() {
		var p pa.LoginProfile
		if e = rows.Scan(&p.ID, &p.Agent, &p.Name, &p.State, &p.Revision); e != nil {
			return nil, e
		}
		p.Kind = "account"
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
	path, e := r.slot(p)
	if e != nil {
		return nil, e
	}
	switch p.Agent {
	case pa.ClaudeCode:
		return []string{"CLAUDE_CONFIG_DIR=" + path}, nil
	case pa.Codex:
		return []string{"CODEX_HOME=" + path}, nil
	}
	return nil, fail(pa.Unsupported, "login_profile_agent")
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
	if b.LoginProfile != "" {
		if _, e := r.selected(ctx, b); e != nil {
			return e
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
	env, e := r.environment(p)
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return pa.LoginProfile{}, fail(pa.Revoked, "login_profile_source")
	}
	if q.Command.Key.MachineID != r.machine {
		return pa.LoginProfile{}, fail(pa.ScopeDenied, "login_profile_machine")
	}
	var data []byte
	e := r.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=? AND operation=? AND command_key=?`, q.Command.Key.MachineID, q.Command.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey).Scan(&data)
	if e == nil {
		var j journal
		if json.Unmarshal(data, &j) != nil {
			return pa.LoginProfile{}, fail(pa.OutcomeUnknown, "login_profile_receipt")
		}
		if _, e = pa.DecideReplay(q.Command, &j.Receipt); e != nil {
			return pa.LoginProfile{}, e
		}
		return j.Profile, nil
	}
	if e != sql.ErrNoRows {
		return pa.LoginProfile{}, e
	}
	if e = admit(ctx); e != nil {
		return pa.LoginProfile{}, e
	}
	if q.Agent != pa.ClaudeCode && q.Agent != pa.Codex {
		return pa.LoginProfile{}, fail(pa.Unsupported, "login_profile_agent")
	}
	if !utf8.ValidString(q.Name) || utf8.RuneCountInString(q.Name) > pa.MaxNameRunes || strings.ContainsAny(q.Name, "\x00\r\n") {
		return pa.LoginProfile{}, fail(pa.Invalid, "login_profile_name")
	}
	var p pa.LoginProfile
	if q.Action == "create" {
		if q.ProfileID != "" || q.Revision != 0 || strings.TrimSpace(q.Name) == "" {
			return p, fail(pa.Invalid, "login_profile_create")
		}
		var n int
		if e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_profiles WHERE source=? AND (removed=0 OR state IN ('unknown','cleanup_unknown'))`, r.source).Scan(&n); e != nil {
			return p, e
		}
		if n >= pa.MaxLoginProfiles {
			return p, fail(pa.CapacityFull, "login_profiles")
		}
		p = pa.LoginProfile{ID: id(), Agent: q.Agent, Kind: "account", Name: q.Name, State: "signed_out", Revision: 1}
		// Reserve the opaque identity before creating its empty slot. A failed slot
		// setup preserves an unknown profile, never deletes unexamined native bytes.
	} else {
		p, e = r.get(ctx, q.ProfileID)
		if e != nil {
			return p, e
		}
		if p.Agent != q.Agent {
			return p, fail(pa.ScopeDenied, "login_profile_agent")
		}
		if p.Revision != q.Revision {
			return p, fail(pa.StaleVersion, "login_profile")
		}
		p.Revision++
	}
	removed := false
	switch q.Action {
	case "create":
	case "rename":
		if strings.TrimSpace(q.Name) == "" {
			return p, fail(pa.Invalid, "login_profile_name")
		}
		p.Name = q.Name
	case "start":
		if p.State == "accepted" || p.State == "running" || p.State == "unknown" {
			return p, fail(pa.OutcomeUnknown, "login_profile_state")
		}
		// A selected profile is an immutable credential identity. Changing its
		// account requires deselecting it first, which advances each Bot's
		// instruction revision before that profile can be selected again.
		// The registry lock also serializes this check with execution probes.
		var references, holds int
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND json_extract(CAST(data AS TEXT),'$.Bot.login_profile')=?`, r.machine, p.ID).Scan(&references)
		if e != nil {
			return p, e
		}
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_holds WHERE source=? AND profile=?`, r.source, p.ID).Scan(&holds)
		if e != nil {
			return p, e
		}
		if references != 0 || holds != 0 {
			return p, fail(pa.ScopeDenied, "login_profile_in_use")
		}
		p.State = "accepted"
	case "cancel":
		if r.running[p.ID] == nil {
			return p, fail(pa.OutcomeUnknown, "login_profile_process")
		}
	case "check":
		if r.running[p.ID] != nil || p.State == "unknown" {
			return p, fail(pa.OutcomeUnknown, "login_profile_state")
		}
	case "remove":
		var references, holds int
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND json_extract(CAST(data AS TEXT),'$.Bot.login_profile')=?`, r.machine, p.ID).Scan(&references)
		if e != nil {
			return p, e
		}
		e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_login_holds WHERE source=? AND profile=?`, r.source, p.ID).Scan(&holds)
		if e != nil {
			return p, e
		}
		if references != 0 || holds != 0 || r.running[p.ID] != nil || p.State == "unknown" || p.State == "running" || p.State == "accepted" {
			return p, fail(pa.ScopeDenied, "login_profile_in_use")
		}
		removed = true
		p.State = "removing"
	default:
		return p, fail(pa.Invalid, "login_profile_action")
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_login_profiles(source,id,agent,name,state,revision,removed) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source,id) DO UPDATE SET name=excluded.name,state=excluded.state,revision=excluded.revision,removed=excluded.removed`, r.source, p.ID, p.Agent, p.Name, p.State, p.Revision, removed)
	if e != nil {
		return p, e
	}
	j := journal{Receipt: pa.Receipt{ID: id(), Command: q.Command, State: pa.EffectAccepted, ResourceID: p.ID}, Profile: p}
	data, e = json.Marshal(j)
	if e != nil {
		return p, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_receipts(machine,bot,operation,command_key,receipt_id,data) VALUES(?,?,?,?,?,?)`, q.Command.Key.MachineID, q.Command.Key.BotID, q.Command.Operation, q.Command.IdempotencyKey, j.ID, data)
	if e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, fail(pa.OutcomeUnknown, "login_profile_commit")
	}
	if q.Action == "rename" {
		return p, nil
	}
	env, e := r.environment(p)
	if e != nil {
		r.state(p, "unknown")
		return p, e
	}
	if e = admit(ctx); e != nil {
		r.state(p, "unknown")
		return p, e
	}
	switch q.Action {
	case "remove":
		if e = r.native.SignOut(ctx, p.Agent, env); e != nil {
			r.state(p, "cleanup_unknown")
			return p, fail(pa.ExecutionEffectUnknown, "login_profile_sign_out")
		}
		path, e := r.slot(p)
		if e != nil {
			r.state(p, "cleanup_unknown")
			return p, e
		}
		if e = os.RemoveAll(path); e != nil {
			r.state(p, "cleanup_unknown")
			return p, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
		}
		r.state(p, "removed")
	case "start":
		process, e := r.native.Start(ctx, p.Agent, env)
		if e != nil {
			r.state(p, "unknown")
			return p, fail(pa.OutcomeUnknown, "login_profile_start")
		}
		r.running[p.ID] = process
		r.state(p, "running")
		r.workers.Add(1)
		go r.finish(p, env, process)
	case "cancel":
		if e = r.running[p.ID].Close(ctx); e != nil {
			r.state(p, "unknown")
			return p, fail(pa.ExecutionEffectUnknown, "login_profile_cleanup")
		}
	case "check":
		state, e := r.native.Status(ctx, p.Agent, env)
		if e != nil {
			r.state(p, "unknown")
			return p, fail(pa.AuthProbeUnknown, "login_profile_status")
		}
		r.state(p, state)
	}
	// Return the durable acceptance snapshot. Replays return these exact bytes;
	// GET observes subsequent running/signed-in facts, never an invented success.
	return p, nil
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
		if v, err := r.native.Status(context.Background(), p.Agent, env); err == nil {
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
