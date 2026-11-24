package botloginprofiles

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// statusScript answers native status probes from a queue: an error entry
// stands for an interrupted or timed-out read, a state entry for a real answer.
type statusScript struct {
	answers []any
	probes  int
}

func (s *statusScript) Start(context.Context, pa.Agent, []string) (app.LoginProcess, error) {
	return nil, errors.New("synthetic native start unavailable")
}
func (s *statusScript) Status(context.Context, pa.Agent, []string) (string, error) {
	s.probes++
	if len(s.answers) == 0 {
		return "", context.DeadlineExceeded
	}
	next := s.answers[0]
	s.answers = s.answers[1:]
	if e, ok := next.(error); ok {
		return "", e
	}
	return next.(string), nil
}
func (*statusScript) SignOut(context.Context, pa.Agent, []string) error { return nil }

type recoveryFixture struct {
	db     *sql.DB
	state  string
	native *statusScript
	n      int
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	state := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(state, "fixture.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	if e = (store.Installer{}).Install(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	return &recoveryFixture{db: db, state: state, native: &statusScript{}}
}

func (f *recoveryFixture) open(t *testing.T) *Registry {
	t.Helper()
	r, e := New(context.Background(), f.db, f.state, "synthetic-account", "machine", f.native)
	if e != nil {
		t.Fatal(e)
	}
	r.ProbeBackoff = 0
	return r
}

func (f *recoveryFixture) apply(r *Registry, operation string, p pa.LoginProfile, kind string) (pa.LoginProfile, error) {
	f.n++
	q := pa.LoginProfileAction{Command: pa.Command{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Operation: pa.LoginProfileOperation, IdempotencyKey: operation + strings.Repeat("x", f.n), ContentHash: strings.Repeat("a", 64)}, ProfileID: p.ID, Revision: p.Revision, Agent: pa.Codex, Action: operation, Kind: kind, Name: "Synthetic account"}
	return r.Apply(context.Background(), q, func(context.Context) error { return nil })
}

func current(t *testing.T, r *Registry, id string) pa.LoginProfile {
	t.Helper()
	p, e := r.get(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// A restart during a sign-in leaves the profile unknown. A probe that is
// interrupted keeps it unknown; the next probe that answers settles it without
// another restart, and an authoritative signed-out answer stays signed out.
func TestRestartUnknownProfileSettlesOnTheNextSuccessfulCheck(t *testing.T) {
	ctx := context.Background()
	f := newRecoveryFixture(t)
	r := f.open(t)
	p, e := f.apply(r, "create", pa.LoginProfile{}, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='running'`); e != nil {
		t.Fatal(e)
	}
	restarted := f.open(t)
	p = current(t, restarted, p.ID)
	if p.State != "unknown" {
		t.Fatal("restart kept an in-flight sign-in as settled", p.State)
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: p.ID}
	if _, _, e = restarted.Acquire(ctx, bot); pa.Code(e) != pa.OutcomeUnknown {
		t.Fatal("an unknown profile lent its old sign-in", e)
	}

	interrupted := errors.New("synthetic status read interrupted")
	f.native.answers = []any{interrupted, context.DeadlineExceeded, interrupted, interrupted}
	if _, e = f.apply(restarted, "check", p, ""); pa.Code(e) != pa.AuthProbeUnknown {
		t.Fatal("an interrupted probe was reported as settled", e)
	}
	if f.native.probes != pa.MaxLoginProbeAttempts {
		t.Fatal("probe attempts", f.native.probes)
	}
	p = current(t, restarted, p.ID)
	if p.State != "unknown" {
		t.Fatal("an interrupted probe invented a state", p.State)
	}

	f.native.answers = []any{context.DeadlineExceeded, "signed_in"}
	if _, e = f.apply(restarted, "check", p, ""); e != nil {
		t.Fatal("a probe that answered after one timeout did not settle", e)
	}
	p = current(t, restarted, p.ID)
	if p.State != "signed_in" {
		t.Fatal("successful probe did not recover the profile", p.State)
	}
	_, release, e := restarted.Acquire(ctx, bot)
	if e != nil {
		t.Fatal("recovered profile still refused execution", e)
	}
	release()

	// A probe failure on a settled profile is not cached as a new state.
	f.native.answers = []any{interrupted, interrupted, interrupted}
	if _, e = f.apply(restarted, "check", p, ""); pa.Code(e) != pa.AuthProbeUnknown {
		t.Fatal(e)
	}
	p = current(t, restarted, p.ID)
	if p.State != "signed_in" {
		t.Fatal("one failed probe latched a healthy profile", p.State)
	}

	// An authoritative answer is kept: signed out refuses execution.
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='running'`); e != nil {
		t.Fatal(e)
	}
	again := f.open(t)
	p = current(t, again, p.ID)
	f.native.answers = []any{"signed_out"}
	if _, e = f.apply(again, "check", p, ""); e != nil {
		t.Fatal(e)
	}
	p = current(t, again, p.ID)
	if p.State != "signed_out" {
		t.Fatal("authoritative signed-out answer lost", p.State)
	}
	if _, _, e = again.Acquire(ctx, bot); pa.Code(e) != pa.AuthRejected {
		t.Fatal("a signed-out profile acquired execution", e)
	}
}

// An API key profile left unknown by a restart settles from a fresh vault read.
// A vault read that fails is not cached: the stored key is readable again once
// the fault clears, and the person can always enter a replacement key.
func TestRestartUnknownKeyProfileSettlesFromAFreshVaultRead(t *testing.T) {
	ctx := context.Background()
	f := newRecoveryFixture(t)
	r := f.open(t)
	p, e := f.apply(r, "create", pa.LoginProfile{}, "api_key")
	if e != nil {
		t.Fatal(e)
	}
	p.Kind = "api_key"
	if e = r.vault.Put(p.ID, r.aad(p), []byte("SYNTHETIC_PROFILE_KEY"), 16384); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='signed_in'`); e != nil {
		t.Fatal(e)
	}
	p = current(t, r, p.ID)
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: p.ID}

	vault := filepath.Join(r.root, "key-vault-v1")
	if e = os.Chmod(vault, 0); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Chmod(vault, 0700) })
	if _, _, e = r.Acquire(ctx, bot); pa.Code(e) != pa.AuthProbeUnknown {
		t.Fatal("interrupted vault read", e)
	}
	p = current(t, r, p.ID)
	if _, e = f.apply(r, "check", p, ""); pa.Code(e) != pa.AuthProbeUnknown {
		t.Fatal("interrupted vault check", e)
	}
	if state := current(t, r, p.ID).State; state != "signed_in" {
		t.Fatal("one failed vault read latched the profile", state)
	}
	if e = os.Chmod(vault, 0700); e != nil {
		t.Fatal(e)
	}
	_, release, e := r.Acquire(ctx, bot)
	if e != nil {
		t.Fatal("the key stayed unusable after the fault cleared", e)
	}
	release()

	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='accepted'`); e != nil {
		t.Fatal(e)
	}
	restarted := f.open(t)
	p = current(t, restarted, p.ID)
	if p.State != "unknown" {
		t.Fatal("restart kept an in-flight key write as settled", p.State)
	}
	if _, e = f.apply(restarted, "check", p, ""); e != nil {
		t.Fatal("fresh vault read did not settle", e)
	}
	p = current(t, restarted, p.ID)
	if p.State != "signed_in" {
		t.Fatal(p.State)
	}

	// The explicit next step for an unknown key profile is a replacement key.
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='accepted'`); e != nil {
		t.Fatal(e)
	}
	restarted = f.open(t)
	p = current(t, restarted, p.ID)
	prepared, e := f.apply(restarted, "prepare-key", p, "")
	if e != nil || prepared.Handoff == nil {
		t.Fatal("an unknown key profile refused a replacement key", e)
	}
}
