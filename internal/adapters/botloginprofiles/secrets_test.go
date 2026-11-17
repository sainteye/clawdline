package botloginprofiles

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestKeyTargetsKeepScopeRevocationAndInFlightCredentialAuthority(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(state, "fixture.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = (store.Installer{}).Install(ctx, db); e != nil {
		t.Fatal(e)
	}
	r, e := New(ctx, db, state, "synthetic-source", "machine", &nativeFixture{})
	if e != nil {
		t.Fatal(e)
	}
	serial := 0
	action := func(kind string, p pa.LoginProfile) pa.LoginProfileAction {
		serial++
		return pa.LoginProfileAction{Command: pa.Command{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Operation: pa.LoginProfileOperation, IdempotencyKey: fmt.Sprint(serial), ContentHash: strings.Repeat("a", 64)}, Agent: pa.Codex, Action: kind, Kind: "api_key", Name: "Synthetic key", ProfileID: p.ID, Revision: p.Revision, Authority: "synthetic-current-principal-project-epoch"}
	}
	allow := func(context.Context) error { return nil }
	create := func() pa.LoginProfile {
		p, e := r.Apply(ctx, action("create", pa.LoginProfile{}), allow)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	prepare := func(p pa.LoginProfile) (pa.LoginProfile, pa.LoginProfileAction) {
		p, e := r.Apply(ctx, action("prepare-key", p), allow)
		if e != nil {
			t.Fatal(e)
		}
		q := action("submit-key", p)
		q.Target = p.Handoff.ID
		return p, q
	}
	p := create()
	_, q := prepare(p)
	for _, change := range []func(*pa.LoginProfileAction){func(q *pa.LoginProfileAction) { q.Authority = "foreign-principal" }, func(q *pa.LoginProfileAction) { q.Command.Key.BotID = "foreign-bot" }, func(q *pa.LoginProfileAction) { q.Fence.Project.ProjectID = "foreign-project" }, func(q *pa.LoginProfileAction) { q.Agent = pa.ClaudeCode }, func(q *pa.LoginProfileAction) { q.ProfileID = strings.Repeat("f", 32) }} {
		bad := q
		serial++
		bad.Command.IdempotencyKey = fmt.Sprint(serial)
		change(&bad)
		if _, e = r.SubmitSecret(ctx, bad, []byte("SYNTHETIC_PROFILE_KEY_ONLY"), allow); e == nil {
			t.Fatal("foreign target scope accepted")
		}
	}
	other, e := New(ctx, db, state, "foreign-source", "machine", &nativeFixture{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.SubmitSecret(ctx, q, []byte("SYNTHETIC_PROFILE_KEY_ONLY"), allow); e == nil {
		t.Fatal("cross-source target accepted")
	}
	if _, e = r.SubmitSecret(ctx, q, []byte("SYNTHETIC_PROFILE_KEY_ONLY"), allow); e != nil {
		t.Fatal(e)
	}
	profiles, e := r.List(ctx, pa.Codex)
	if e != nil {
		t.Fatal(e)
	}
	p = profiles[0]
	b := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: p.ID}
	_, release, e := r.Acquire(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Apply(ctx, action("prepare-key", p), allow); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("key changed beneath captured native hold", e)
	}
	if _, e = r.Apply(ctx, action("remove", p), allow); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("held key removed", e)
	}
	release()
	if e = os.Remove(filepath.Join(r.root, "key-vault-v1", p.ID+".sealed")); e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.Acquire(ctx, b); pa.Code(e) != pa.AuthProbeUnknown {
		t.Fatal("missing vault record did not refuse native acquisition", e)
	}
	p, e = r.get(ctx, p.ID)
	if e != nil || p.State != "unknown" {
		t.Fatal("lost key authority retained a signed-in claim", e)
	}
	for _, revokeAt := range []int{2, 3} {
		p = create()
		p, q = prepare(p)
		calls := 0
		_, e = r.SubmitSecret(ctx, q, []byte("SYNTHETIC_PROFILE_KEY_ONLY"), func(context.Context) error {
			calls++
			if calls == revokeAt {
				return fail(pa.Revoked, "synthetic-revocation")
			}
			return nil
		})
		if pa.Code(e) != pa.Revoked {
			t.Fatal("live revocation not preserved", revokeAt, e)
		}
		p, e = r.get(ctx, p.ID)
		if e != nil || p.State != "unknown" {
			t.Fatal("revoked effect claimed signed-in state", e)
		}
		b.LoginProfile = p.ID
		if _, _, e = r.Acquire(ctx, b); pa.Code(e) != pa.OutcomeUnknown {
			t.Fatal("unknown revoked key acquired native authority", e)
		}
	}
}
