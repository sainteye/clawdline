//go:build darwin || linux

package antigravityproject

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

type fixture struct {
	m               *Manager
	st              *store.Store
	cfg             Config
	root, workspace string
	catalog         map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "project")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	helper := filepath.Join(root, "helper")
	if err := os.WriteFile(helper, []byte("synthetic helper image"), 0700); err != nil {
		t.Fatal(err)
	}
	pin, err := antigravity.NewPinnedExecutable(context.Background(), helper)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{st: st, root: root, workspace: workspace, catalog: map[string]string{workspace: workspace}}
	f.cfg = Config{Store: st, Image: pin, Helper: helper, ConfigRoot: state, Resolve: func(ctx context.Context, path string) (string, error) {
		if root, ok := f.catalog[path]; ok {
			return root, nil
		}
		return "", errors.New("synthetic private catalog failure")
	}}
	f.m, err = New(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func preview(t *testing.T, f *fixture) Plan {
	t.Helper()
	p, err := f.m.Preview(context.Background(), f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func enable(t *testing.T, f *fixture, p Plan, key string) Outcome {
	t.Helper()
	out, err := f.m.Enable(context.Background(), p, "person-device", key, key)
	if err != nil {
		t.Fatal(err)
	}
	if out.Consent.Phase != store.AntigravityConsentActive || out.Hooks != "installed" {
		t.Fatal(out)
	}
	return out
}
func state(t *testing.T, f *fixture) store.AntigravityProjectConsent {
	t.Helper()
	c, err := f.st.AntigravityProjectConsent(context.Background(), f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func reopen(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(f.cfg.ConfigRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f.st = st
	f.cfg.Store = st
	f.m, err = New(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
}

type failingAnswerStore struct {
	*store.Store
	action store.AntigravityConsentAction
	failed bool
	calls  int
}

func (s *failingAnswerStore) ApplyAntigravityProjectConsent(ctx context.Context, c store.AntigravityConsentCommand) (store.AntigravityProjectConsent, error) {
	out, err := s.Store.ApplyAntigravityProjectConsent(ctx, c)
	if c.Action == s.action {
		s.calls++
		if err == nil && !s.failed {
			s.failed = true
			return store.AntigravityProjectConsent{}, store.AntigravityConsentWriteFailed
		}
	}
	return out, err
}

func TestConsentEnableReplaysCannotReauthorizeOrRemoveANewerGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := preview(t, f)
	if _, err := f.m.Check(ctx, f.workspace); err != NotActive {
		t.Fatal(err)
	}
	active := enable(t, f, p, "enable-first")
	if _, err := f.m.Disable(ctx, f.workspace, active.Consent.Revision, "person-device", "enable-first", "enable-first"); err != store.AntigravityConsentKeyConflict {
		t.Fatal("changed action reused enable key", err)
	}
	original, err := f.m.Check(ctx, f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	realApply := f.m.apply
	f.m.apply = func(p antigravity.InstallPreview) error { calls++; return realApply(p) }
	if replay := enable(t, f, p, "enable-first"); replay != active || calls != 0 {
		t.Fatal(replay, calls)
	}
	changed := p
	changed.BeforeDigest = "changed-preview"
	if _, err := f.m.Enable(ctx, changed, "person-device", "enable-first", "enable-first"); err != store.AntigravityConsentKeyConflict {
		t.Fatal(err)
	}
	disabled, err := f.m.Disable(ctx, f.workspace, active.Consent.Revision, "person-device", "disable-first", "disable-first")
	if err != nil || disabled.Consent.Phase != store.AntigravityConsentDisabled || disabled.Hooks != "removed" {
		t.Fatal(disabled, err)
	}
	beforeCalls := calls
	if _, err := f.m.Enable(ctx, p, "person-device", "enable-first", "enable-first"); err != Changed {
		t.Fatal(err)
	}
	if calls != beforeCalls || state(t, f) != disabled.Consent {
		t.Fatal("old enable changed revoked state")
	}
	p2 := preview(t, f)
	newActive := enable(t, f, p2, "enable-second")
	beforeCalls = calls
	if _, err := f.m.Disable(ctx, f.workspace, active.Consent.Revision, "person-device", "disable-first", "disable-first"); err != Changed {
		t.Fatal(err)
	}
	if calls != beforeCalls || state(t, f) != newActive.Consent {
		t.Fatal("old disable removed new grant")
	}
	current, err := f.m.Check(ctx, f.workspace)
	if err != nil || current.RevisionDigest == original.RevisionDigest {
		t.Fatal(current, err)
	}
}

func TestConsentRecoversCommittedPhasesAndUncertainFileEffects(t *testing.T) {
	for _, kind := range []string{"prepare-answer", "installed-answer", "activate-answer"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			p := preview(t, f)
			ctx := context.Background()
			if kind == "installed-answer" {
				real := f.m.apply
				f.m.apply = func(p antigravity.InstallPreview) error {
					if err := real(p); err != nil {
						return err
					}
					return errors.New("synthetic lost file answer")
				}
			}
			if kind == "prepare-answer" || kind == "activate-answer" {
				action := store.AntigravityConsentPrepare
				if kind == "activate-answer" {
					action = store.AntigravityConsentActivate
				}
				f.m.store = &failingAnswerStore{Store: f.st, action: action}
			}
			if _, err := f.m.Enable(ctx, p, "person-device", "recover", "recover"); err == nil {
				t.Fatal("injected lost answer succeeded")
			}
			before := state(t, f)
			want := store.AntigravityConsentPreparing
			if kind == "activate-answer" {
				want = store.AntigravityConsentActive
			}
			if before.Phase != want {
				t.Fatal(before)
			}
			reopen(t, f)
			writes := 0
			real := f.m.apply
			f.m.apply = func(p antigravity.InstallPreview) error { writes++; return real(p) }
			active := enable(t, f, p, "recover")
			if active.Consent.Revision != 2 {
				t.Fatal(active)
			}
			expectedWrites := 0
			if kind == "prepare-answer" {
				expectedWrites = 1
			}
			if writes != expectedWrites {
				t.Fatal("recovery repeated known installation", writes, expectedWrites)
			}
		})
	}
}

func TestConsentRevocationWinsAuthorityDuringInstallation(t *testing.T) {
	f := newFixture(t)
	p := preview(t, f)
	ctx := context.Background()
	real := f.m.apply
	f.m.apply = func(p antigravity.InstallPreview) error {
		current := state(t, f)
		if current.Phase != store.AntigravityConsentPreparing {
			t.Fatal("files preceded durable preparation", current)
		}
		if _, err := f.st.ApplyAntigravityProjectConsent(ctx, store.AntigravityConsentCommand{ProjectRoot: f.workspace, Action: store.AntigravityConsentDisable, ExpectedRevision: current.Revision, Actor: "other-device", Key: "revoke-during-install", OperationID: "revoke-during-install"}); err != nil {
			t.Fatal(err)
		}
		return real(p)
	}
	if _, err := f.m.Enable(ctx, p, "person-device", "race", "race"); err != Changed {
		t.Fatal(err)
	}
	if state(t, f).Phase != store.AntigravityConsentDisabled {
		t.Fatal("file effect restored authority")
	}
	if _, err := f.m.Check(ctx, f.workspace); err != NotActive {
		t.Fatal(err)
	}
	// A late file effect can leave inactive residue. Cleanup is separate and
	// must not lie about whether the earlier revocation was committed.
	f.m.apply = real
	out, err := f.m.Disable(ctx, f.workspace, state(t, f).Revision, "person-device", "cleanup", "cleanup")
	if err != nil || out.Hooks != "removed" {
		t.Fatal(out, err)
	}
}

func TestConsentDisablesWithoutAUsableHelperAndKeepsFailedCleanupDisabled(t *testing.T) {
	for _, kind := range []string{"missing-pin", "replaced-helper", "cleanup-failure", "uncertain-cleanup"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			active := enable(t, f, preview(t, f), "enable")
			ctx := context.Background()
			switch kind {
			case "missing-pin":
				f.cfg.Image = nil
				var err error
				f.m, err = New(ctx, f.cfg)
				if err != nil {
					t.Fatal(err)
				}
			case "replaced-helper":
				if err := os.WriteFile(f.cfg.Helper, []byte("changed helper image"), 0700); err != nil {
					t.Fatal(err)
				}
			case "cleanup-failure":
				f.m.apply = func(antigravity.InstallPreview) error { return errors.New("synthetic cleanup failure") }
			case "uncertain-cleanup":
				real := f.m.apply
				f.m.apply = func(p antigravity.InstallPreview) error {
					if err := real(p); err != nil {
						return err
					}
					return errors.New("synthetic lost cleanup answer")
				}
			}
			if kind == "missing-pin" || kind == "replaced-helper" {
				if _, err := f.m.Check(ctx, f.workspace); err != HelperUnavailable {
					t.Fatal("unusable helper retained authority", err)
				}
			}
			out, err := f.m.Disable(ctx, f.workspace, active.Consent.Revision, "person-device", "disable", "disable")
			if out.Consent.Phase != store.AntigravityConsentDisabled || state(t, f).Phase != store.AntigravityConsentDisabled {
				t.Fatal(out, err)
			}
			failed := kind == "cleanup-failure" || kind == "uncertain-cleanup"
			if failed && (err == nil || out.Hooks != "unknown") {
				t.Fatal(out, err)
			}
			if !failed && (err != nil || out.Hooks != "removed") {
				t.Fatal(out, err)
			}
			if _, err := f.m.Check(ctx, f.workspace); err != NotActive {
				t.Fatal(err)
			}
			if kind == "uncertain-cleanup" {
				f.m.apply = func(antigravity.InstallPreview) error { t.Fatal("repeated known removal"); return nil }
				replayed, err := f.m.Disable(ctx, f.workspace, active.Consent.Revision, "person-device", "disable", "disable")
				if err != nil || replayed.Hooks != "removed" {
					t.Fatal(replayed, err)
				}
			}
		})
	}
}

func TestConsentPreparesLinkedWorkspacesWithoutChangingOtherGrants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	enable(t, f, preview(t, f), "enable")
	original, err := f.m.Check(ctx, f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(f.root, "linked")
	if err := os.Mkdir(linked, 0700); err != nil {
		t.Fatal(err)
	}
	f.catalog[linked] = f.workspace
	if _, err := f.m.Check(ctx, linked); err != HooksUnverified {
		t.Fatal("worktree creation became ready", err)
	}
	prepared, err := f.m.EnsureWorkspace(ctx, linked)
	if err != nil || prepared.Consent != original.Consent || prepared.RevisionDigest != original.RevisionDigest {
		t.Fatal(prepared, err)
	}
	if current, err := f.m.Check(ctx, f.workspace); err != nil || current != original {
		t.Fatal("new worktree invalidated existing grant", current, err)
	}
	other := filepath.Join(f.root, "other-project")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	f.catalog[other] = other
	if _, err := f.m.EnsureWorkspace(ctx, other); err != NotActive {
		t.Fatal("another Project borrowed consent", err)
	}
	unknown := filepath.Join(f.root, "unknown")
	if _, err := f.m.EnsureWorkspace(ctx, unknown); err != Unverified {
		t.Fatal(err)
	}
}

type afterReadStore struct {
	*store.Store
	after func(store.AntigravityProjectConsent)
}

func (s *afterReadStore) AntigravityProjectConsent(ctx context.Context, root string) (store.AntigravityProjectConsent, error) {
	current, err := s.Store.AntigravityProjectConsent(ctx, root)
	if err == nil && s.after != nil {
		after := s.after
		s.after = nil
		after(current)
	}
	return current, err
}

func TestConsentCheckRechecksRevocationAfterItsFirstRead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	enable(t, f, preview(t, f), "enable")
	f.m.store = &afterReadStore{Store: f.st, after: func(current store.AntigravityProjectConsent) {
		if _, err := f.st.ApplyAntigravityProjectConsent(ctx, store.AntigravityConsentCommand{ProjectRoot: f.workspace, Action: store.AntigravityConsentDisable, ExpectedRevision: current.Revision, Actor: "other-device", Key: "revoke-after-read", OperationID: "revoke-after-read"}); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := f.m.Check(ctx, f.workspace)
	if state(t, f).Phase != store.AntigravityConsentDisabled {
		t.Fatal("revocation did not actually commit")
	}
	if err != Changed {
		t.Fatal("dependency reads reused revoked grant", err)
	}
}

func TestConsentPlanCannotBeRedirectedOrUsedAfterFileChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := preview(t, f)
	if _, err := f.m.Enable(ctx, p, "invalid actor", "key", "op"); err != Invalid {
		t.Fatal(err)
	}
	wrong := p
	wrong.ProjectRoot = f.root
	if _, err := f.m.Enable(ctx, wrong, "person-device", "key", "op"); err != Changed {
		t.Fatal(err)
	}
	dir := filepath.Join(f.workspace, ".agents")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{"foreign":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Enable(ctx, p, "person-device", "key", "op"); err != Changed {
		t.Fatal(err)
	}
	if state(t, f).Phase != store.AntigravityConsentAbsent {
		t.Fatal("stale preview changed consent")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.m.Preview(cancelCtx, f.workspace); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.m.image = nil
	if _, err := f.m.Preview(ctx, f.workspace); err != HelperUnavailable {
		t.Fatal(err)
	}
}
