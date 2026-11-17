package botconnectorvault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botprofilevault"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

type fixture struct {
	r     *Registry
	db    *sql.DB
	state string
	scope Scope
	n     int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(state, "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`CREATE TABLE project_assistant_connector_credentials(source TEXT NOT NULL,bot TEXT NOT NULL,connector TEXT NOT NULL,id TEXT NOT NULL,scope TEXT NOT NULL,state TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(source,bot,connector))`,
		`CREATE TABLE project_assistant_connector_holds(source TEXT NOT NULL,bot TEXT NOT NULL,connector TEXT NOT NULL,scope TEXT NOT NULL,revision INTEGER NOT NULL,id TEXT NOT NULL,PRIMARY KEY(source,id))`,
		`CREATE TABLE project_assistant_receipts(machine TEXT NOT NULL,bot TEXT NOT NULL,operation TEXT NOT NULL,command_key TEXT NOT NULL,receipt_id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(machine,bot,operation,command_key),UNIQUE(machine,bot,receipt_id))`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	r, err := New(context.Background(), db, state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{r: r, db: db, state: state, scope: Scope{Bot: pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: "repository"}, ConversationID: "chat", Agent: pa.Codex}, Ref: pa.ExtensionRef{ID: "connector:fixture", Fingerprint: strings.Repeat("a", 64)}, Authority: "source-authority-one"}}
}

func (f *fixture) action(kind, target string) Action {
	f.n++
	q := Action{Command: pa.Command{Key: f.scope.Bot.Key, Operation: "connector-credential", IdempotencyKey: fmt.Sprintf("fixture-%d", f.n)}, Fence: pa.Fence{Key: f.scope.Bot.Key, Project: f.scope.Bot.Project, ConversationID: "chat"}, ExpectedVersion: 1, Ref: f.scope.Ref, Action: kind, Target: target, Authority: f.scope.Authority}
	data, _ := json.Marshal(q)
	h := sha256.Sum256(data)
	q.Command.ContentHash = hex.EncodeToString(h[:])
	return q
}

func allow(context.Context) error { return nil }

func (f *fixture) store(t *testing.T, value []byte) Action {
	t.Helper()
	p, err := f.r.Apply(context.Background(), f.scope, f.action("prepare", ""), nil, false, allow)
	if err != nil || p.Handoff == nil {
		t.Fatal("prepare", p, err)
	}
	q := f.action("submit", p.Handoff.ID)
	result, err := f.r.Apply(context.Background(), f.scope, q, value, true, allow)
	if err != nil || result.State != "accepted" {
		t.Fatal("submit", result, err)
	}
	return q
}

func TestConnectorSecretReplayNeverComparesOrReplacesValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	value := []byte("SYNTHETIC_CONNECTOR_CREDENTIAL")
	q := f.store(t, value)
	got, revision, err := f.r.Get(ctx, f.scope)
	if err != nil || !bytes.Equal(got, value) || revision != 2 {
		t.Fatal("capture", revision, err)
	}
	botprofilevault.Wipe(got)
	first, err := f.r.Apply(ctx, f.scope, q, nil, true, func(context.Context) error { t.Fatal("receipt replay admitted a fresh effect"); return nil })
	if err != nil || first.State != "accepted" {
		t.Fatal("receipt replay", first, err)
	}
	second, err := f.r.Apply(ctx, f.scope, q, []byte("A_DIFFERENT_SYNTHETIC_VALUE"), true, allow)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("replay exposed value comparison", second, err)
	}
	got, _, err = f.r.Get(ctx, f.scope)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatal("replay replaced original bytes", err)
	}
	botprofilevault.Wipe(got)
	rows, err := f.db.Query(`SELECT data FROM project_assistant_receipts`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil || bytes.Contains(data, value) || bytes.Contains(data, []byte("A_DIFFERENT_SYNTHETIC_VALUE")) {
			t.Fatal("receipt exposed credential", err)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if f.r.SecretTargetUsage() != 0 {
		t.Fatal("submitted target retained")
	}
}

func TestConnectorTargetsExpireAndBindFullAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()
	f.r.Now = func() time.Time { return now }
	p, err := f.r.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow)
	if err != nil {
		t.Fatal(err)
	}
	q := f.action("submit", p.Handoff.ID)
	for _, mutate := range []func(*Scope, *Action){
		func(s *Scope, a *Action) { s.Authority = "other"; a.Authority = "other" },
		func(s *Scope, a *Action) { s.Bot.Agent = pa.ClaudeCode },
		func(s *Scope, a *Action) { s.Bot.Project.CanonicalID = "other"; a.Fence.Project = s.Bot.Project },
		func(s *Scope, a *Action) { s.Ref.Fingerprint = strings.Repeat("b", 64); a.Ref = s.Ref },
		func(s *Scope, a *Action) {
			s.Bot.Key.BotID = "other"
			a.Command.Key = s.Bot.Key
			a.Fence.Key = s.Bot.Key
		},
	} {
		s, a := f.scope, q
		mutate(&s, &a)
		if _, err = f.r.Apply(ctx, s, a, []byte("SYNTHETIC"), true, allow); err == nil {
			t.Fatal("foreign target accepted")
		}
	}
	now = p.Handoff.Expires
	if _, err = f.r.Apply(ctx, f.scope, q, []byte("SYNTHETIC"), true, allow); pa.Code(err) != pa.Expired {
		t.Fatal("expired target accepted", err)
	}
	if f.r.SecretTargetUsage() != 0 {
		t.Fatal("expired target counted")
	}

	f = newFixture(t)
	p, err = f.r.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Apply(ctx, f.scope, f.action("submit", p.Handoff.ID), []byte("SYNTHETIC"), true, allow); pa.Code(err) != pa.Expired {
		t.Fatal("target survived restart", err)
	}
}

func TestConnectorCredentialRotationRevokesCapturedUseAndPreservesRemovedBytes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store(t, []byte("SYNTHETIC_FIRST"))
	value, revision, err := f.r.Get(ctx, f.scope)
	if err != nil || f.r.Check(ctx, f.scope, revision) != nil {
		t.Fatal("capture", err)
	}
	defer botprofilevault.Wipe(value)
	f.store(t, []byte("SYNTHETIC_SECOND"))
	if pa.Code(f.r.Check(ctx, f.scope, revision)) != pa.Revoked {
		t.Fatal("rotation retained old authority")
	}
	p, err := f.r.read(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.r.root, "connector-key-vault-v1", p.id+".sealed")
	sealed, err := os.ReadFile(path)
	if err != nil || bytes.Contains(sealed, []byte("SYNTHETIC_SECOND")) {
		t.Fatal("vault encryption", err)
	}
	if _, err = f.r.Apply(ctx, f.scope, f.action("remove", ""), nil, false, allow); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.r.Get(ctx, f.scope); pa.Code(err) != pa.AuthRequired {
		t.Fatal("removed credential remained available", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(sealed, after) {
		t.Fatal("revocation deleted uncertain owned bytes", err)
	}
}

func TestConnectorVaultReopenTamperMissingKeyAndSourceIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store(t, []byte("SYNTHETIC_SECRET"))
	reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := reopened.Get(ctx, f.scope)
	if err != nil || string(value) != "SYNTHETIC_SECRET" {
		t.Fatal("reopen", err)
	}
	botprofilevault.Wipe(value)
	foreign, err := New(ctx, f.db, f.state, "another-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = foreign.Get(ctx, f.scope); pa.Code(err) != pa.AuthRequired {
		t.Fatal("another source read credential", err)
	}
	p, err := f.r.read(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.r.root, "connector-key-vault-v1", p.id+".sealed")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = reopened.Get(ctx, f.scope); pa.Code(err) != pa.AuthProbeUnknown {
		t.Fatal("tampered record accepted", err)
	}
	key := filepath.Join(f.r.root, "connector-key-vault-v1", "profile-aes256-gcm.key")
	if err = os.Remove(key); err != nil {
		t.Fatal(err)
	}
	pending, err := f.r.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.r.Apply(ctx, f.scope, f.action("submit", pending.Handoff.ID), []byte("SYNTHETIC_REPLACEMENT"), true, allow); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal("missing key replaced", err)
	}
	if _, err = os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("missing authority regenerated", err)
	}
	status, err := f.r.Status(ctx, f.scope)
	if err != nil || status.State != "unknown" {
		t.Fatal("failed write lost uncertainty", status, err)
	}
}

func TestConnectorVaultUnknownAcceptedWriteAndPrivateDirectory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store(t, []byte("SYNTHETIC_SECRET"))
	if _, err := f.db.Exec(`UPDATE project_assistant_connector_credentials SET state='accepted'`); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	status, err := reopened.Status(ctx, f.scope)
	if err != nil || status.State != "unknown" {
		t.Fatal("restart forgot accepted effect", status, err)
	}
	if _, err = reopened.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal("unknown effect reopened", err)
	}
	removed, err := reopened.Apply(ctx, f.scope, f.action("remove", ""), nil, false, allow)
	if err != nil || removed.State != "cleanup_unknown" {
		t.Fatal("unknown cleanup bytes became reusable", removed, err)
	}
	if _, err = reopened.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal("remove bypassed unknown cleanup barrier", err)
	}

	f = newFixture(t)
	outside := t.TempDir()
	if err = os.Mkdir(filepath.Join(f.state, "project-assistants"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(f.state, "project-assistants", "connector-credentials")); err != nil {
		t.Fatal(err)
	}
	pending, err := f.r.Apply(ctx, f.scope, f.action("prepare", ""), nil, false, allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.r.Apply(ctx, f.scope, f.action("submit", pending.Handoff.ID), []byte("SYNTHETIC"), true, allow); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal("symlink vault accepted", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("symlink wrote outside owned root", err)
	}
}

func TestConnectorCredentialHoldsSurviveRestartAndOnlyReleaseVerifiedCleanup(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.store(t, []byte("SYNTHETIC_HELD_SECRET"))
	value, revision, release, quarantine, err := f.r.Acquire(ctx, f.scope)
	if err != nil || revision != 2 || string(value) != "SYNTHETIC_HELD_SECRET" {
		t.Fatal("held capture", revision, err)
	}
	botprofilevault.Wipe(value)
	if used, err := f.r.HoldUsage(ctx); err != nil || used != 1 {
		t.Fatal("hold not durable", used, err)
	}
	if err = quarantine(ctx); err != nil {
		t.Fatal(err)
	}
	if err = release(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("unknown cleanup released its hold", err)
	}
	reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := reopened.Status(ctx, f.scope); err != nil || status.State != "unknown" {
		t.Fatal("restart cleared quarantine", status, err)
	}
	if used, err := reopened.HoldUsage(ctx); err != nil || used != 1 {
		t.Fatal("restart forgot hold", used, err)
	}

	f = newFixture(t)
	f.store(t, []byte("SYNTHETIC_RESTART_HELD_SECRET"))
	value, _, _, _, err = f.r.Acquire(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	botprofilevault.Wipe(value)
	reopened, err = New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := reopened.Status(ctx, f.scope); err != nil || status.State != "unknown" {
		t.Fatal("restart assumed a captured execution had stopped", status, err)
	}

	f = newFixture(t)
	f.store(t, []byte("SYNTHETIC_CLEAN_SECRET"))
	value, _, release, _, err = f.r.Acquire(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	botprofilevault.Wipe(value)
	if err = release(ctx); err != nil {
		t.Fatal(err)
	}
	if err = release(ctx); err != nil {
		t.Fatal("release not idempotent", err)
	}
	reopened, err = New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := reopened.Status(ctx, f.scope); err != nil || status.State != "ready" {
		t.Fatal("verified cleanup became unknown", status, err)
	}
}

func TestConnectorOldCaptureQuarantineCannotRewriteReplacementCredential(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store(t, []byte("SYNTHETIC_OLD_CAPTURE"))
	value, _, _, quarantine, err := f.r.Acquire(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	botprofilevault.Wipe(value)
	f.store(t, []byte("SYNTHETIC_REPLACEMENT_CAPTURE"))
	if err = quarantine(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := f.r.Status(ctx, f.scope)
	if err != nil || status.State != "ready" || status.Revision != 4 {
		t.Fatal("old cleanup changed replacement authority", status, err)
	}
	if used, err := f.r.HoldUsage(ctx); err != nil || used != 1 {
		t.Fatal("old unknown capture hold was removed", used, err)
	}
}
