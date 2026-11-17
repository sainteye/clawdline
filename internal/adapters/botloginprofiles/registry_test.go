package botloginprofiles

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

type nativeFixture struct {
	starts  int
	process *heldProcess
}
type heldProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *heldProcess) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *heldProcess) Close(context.Context) error { p.once.Do(func() { close(p.done) }); return nil }
func (n *nativeFixture) Start(context.Context, pa.Agent, []string) (app.LoginProcess, error) {
	n.starts++
	n.process = &heldProcess{done: make(chan struct{})}
	return n.process, nil
}
func (*nativeFixture) Status(context.Context, pa.Agent, []string) (string, error) {
	return "signed_in", nil
}
func (*nativeFixture) SignOut(context.Context, pa.Agent, []string) error { return nil }

func TestSchemaSevenKindsMigrateWithoutReplacingMissingAuthority(t *testing.T) {
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
	q := pa.LoginProfileAction{Command: pa.Command{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Operation: pa.LoginProfileOperation, IdempotencyKey: "old-account", ContentHash: strings.Repeat("a", 64)}, Agent: pa.Codex, Action: "create", Name: "Synthetic legacy account"}
	p, e := r.Apply(ctx, q, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`DROP TABLE project_assistant_login_kinds`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE project_assistant_schema SET version=7`); e != nil {
		t.Fatal(e)
	}
	if e = (store.Installer{}).Install(ctx, db); e != nil {
		t.Fatal(e)
	}
	migrated, e := r.get(ctx, p.ID)
	if e != nil || migrated.Kind != "account" {
		t.Fatal("schema7 account migration", e)
	}
	if _, e = db.Exec(`DELETE FROM project_assistant_login_kinds`); e != nil {
		t.Fatal(e)
	}
	if e = (store.Installer{}).Install(ctx, db); e != nil {
		t.Fatal(e)
	}
	if _, e = r.get(ctx, p.ID); pa.Code(e) != pa.OutcomeUnknown {
		t.Fatal("missing schema8 authority was regenerated", e)
	}
}

func TestProfileAuthorityBusyRemovalAndRestartKeepUnknownBytes(t *testing.T) {
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
	native := &nativeFixture{}
	r, e := New(ctx, db, state, "account-a", "machine", native)
	if e != nil {
		t.Fatal(e)
	}
	action := func(operation string, p pa.LoginProfile) pa.LoginProfileAction {
		return pa.LoginProfileAction{Command: pa.Command{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Operation: pa.LoginProfileOperation, IdempotencyKey: operation + p.ID, ContentHash: strings.Repeat("a", 64)}, ProfileID: p.ID, Revision: p.Revision, Agent: pa.Codex, Action: operation, Name: "Synthetic account"}
	}
	p, e := r.Apply(ctx, action("create", pa.LoginProfile{}), func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	other, e := New(ctx, db, state, "account-b", "machine", native)
	if e != nil {
		t.Fatal(e)
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: p.ID}
	if _, _, e = other.Acquire(ctx, bot); pa.Code(e) != pa.NotFound {
		t.Fatal("another source account borrowed a login", e)
	}
	if _, _, e = r.Acquire(ctx, bot); pa.Code(e) != pa.AuthRejected {
		t.Fatal("an unsigned account acquired a native execution hold", e)
	}
	if _, e = r.Apply(ctx, action("check", p), func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	profiles, e := r.List(ctx, pa.Codex)
	if e != nil || len(profiles) != 1 {
		t.Fatal(profiles, e)
	}
	p = profiles[0]
	_, release, e := r.Acquire(ctx, bot)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Apply(ctx, action("remove", p), func(context.Context) error { return nil }); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("busy execution profile removed", e)
	}
	if _, e = r.Apply(ctx, action("start", p), func(context.Context) error { return nil }); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("a native execution hold allowed account replacement", e)
	}
	release()
	start := action("start", p)
	accepted, e := r.Apply(ctx, start, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if replay, e := r.Apply(ctx, start, func(context.Context) error { t.Fatal("replay reran effect admission"); return nil }); e != nil || !reflect.DeepEqual(replay, accepted) || native.starts != 1 {
		t.Fatal("lost response started another native login", replay, e)
	}
	// A new source instance cannot prove that the old sign-in has ended. It
	// marks the durable owner unknown and refuses both execution and deletion.
	restarted, e := New(ctx, db, state, "account-a", "machine", native)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = restarted.Acquire(ctx, bot); pa.Code(e) != pa.OutcomeUnknown {
		t.Fatal("restart borrowed uncertain account", e)
	}
	rows, e := restarted.List(ctx, pa.Codex)
	if e != nil || len(rows) != 1 || rows[0].State != "unknown" {
		t.Fatal(rows, e)
	}
	if _, e = restarted.Apply(ctx, action("remove", rows[0]), func(context.Context) error { return nil }); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("unknown credentials deleted", e)
	}
	path, e := r.slot(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("unknown slot disappeared", e)
	}
	closeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if e = r.Close(closeCtx); e != nil {
		t.Fatal(e)
	}
}

func TestProfileSlotsRejectSymlinksAndProjectOverlap(t *testing.T) {
	state := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(state, "fixture.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = (store.Installer{}).Install(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	r, e := New(context.Background(), db, state, "account", "machine", &nativeFixture{})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.CheckProject(state); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("slot inside a Project accepted", e)
	}
	if e = os.MkdirAll(filepath.Dir(r.root), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(t.TempDir(), r.root); e != nil {
		t.Fatal(e)
	}
	if _, e = r.slot(pa.LoginProfile{ID: strings.Repeat("a", 32)}); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("profile slot followed a link", e)
	}
}
