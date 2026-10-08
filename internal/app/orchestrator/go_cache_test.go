package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func cacheTask(t *testing.T, pane string, register bool) (*Broker, context.Context, Record, string) {
	t.Helper()
	b, ctx := newTestBroker(t)
	id := "b7310000-0000-4000-8000-000000000001"
	work := filepath.Join(b.Tasks.Path(id), "work")
	cache := filepath.Join(work, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	r := Record{Protocol: Protocol, ID: id, Assistant: "codex", Title: "gate", Dir: b.Tasks.Path(id),
		State: StateSuccess, CreatedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now().Add(-time.Hour),
		ChildBackend: "tmux", ChildTerminalID: pane, Gate: &GateOrigin{}}
	if err := b.save(ctx, r, HashSecret("fictional-secret"), "task.success"); err != nil {
		t.Fatal(err)
	}
	if register {
		if err := b.registerGateGoCache(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{{ID: "%root", Assistant: session.AssistantClaude}},
			Sources: map[string]bool{"tmux": true}}
	}
	return b, ctx, r, cache
}

func oneCache(t *testing.T, b *Broker, ctx context.Context, id string) (outcome, reason string) {
	t.Helper()
	rows, err := b.Store.GoCachesByOwner(ctx, "task", id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("Go cache registration: %d rows, %v", len(rows), err)
	}
	if rows[0].Path != b.gateScratch(id).Cache || rows[0].OwnerKind != "task" ||
		rows[0].OwnerID != id || !rows[0].Rebuildable || rows[0].Purpose == "" || rows[0].CreatedAt.IsZero() {
		t.Fatalf("incomplete durable cache owner: %+v", rows[0])
	}
	return rows[0].LastOutcome, rows[0].LastReason
}

func TestGateGoCacheRegistrationRejectsSharedAndEscapedPaths(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", true)
	if err := b.registerGateGoCache(ctx, r); err != nil {
		t.Fatalf("idempotent registration: %v", err)
	}
	if err := b.Store.RegisterGoCache(ctx, store.GoCache{Path: cache, OwnerKind: "task",
		OwnerID: "b7310000-0000-4000-8000-000000000002", Purpose: "verification_gate_build_cache",
		Rebuildable: true, CreatedAt: time.Now()}); err == nil {
		t.Fatal("a second owner claimed the same cache path")
	}
	shared := filepath.Join(t.TempDir(), "go-build")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := validateGateGoCache(r.Dir, shared); err == nil {
		t.Fatal("a shared cache was accepted as task scratch")
	}
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, cache); err != nil {
		t.Fatal(err)
	}
	if err := validateGateGoCache(r.Dir, cache); err == nil {
		t.Fatal("a symlink escape was accepted")
	}
	if err := b.registerGateGoCache(ctx, r); err == nil {
		t.Fatal("the broker registered an escaped cache")
	}
	// The launch preparation must reject the same escape before its chmod.
	r.Worktree = &Worktree{Path: t.TempDir(), Detached: true}
	if err := b.prepareGateReadonly(r); err == nil {
		t.Fatal("gate preparation accepted a symlink into shared storage")
	}
	if info, err := os.Stat(shared); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("shared cache was altered: %v, %v", info, err)
	}
}

func TestLegacyUnregisteredGateScratchStillReclaimsByTaskOwnership(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", false)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cache, "outside")); err != nil {
		t.Fatal(err)
	}
	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, r.ID, ReclaimTaskDir); d.Outcome != ReclaimRemoved {
		t.Fatalf("legacy task scratch should follow its owner proof: %+v", d)
	}
	if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy task scratch remains: %v", err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "untouched" {
		t.Fatalf("outside symlink target changed: %q, %v", got, err)
	}
	if rows, err := b.Store.GoCachesByOwner(ctx, "task", r.ID); err != nil || len(rows) != 0 {
		t.Fatalf("legacy cache gained invented registration: %+v, %v", rows, err)
	}
}

func TestGateGoCacheKeepsUnknownAndPresentOwnersThenRemovesReadOnlyScratch(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", true)
	b.Reading = func(context.Context) session.Inventory { return session.Inventory{} }
	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, r.ID, ReclaimTaskDir); d.Reason != WhyOwnerUnknown {
		t.Fatalf("unknown owner: %+v", d)
	}
	if outcome, reason := oneCache(t, b, ctx, r.ID); outcome != ReclaimKept || reason != WhyOwnerUnknown {
		t.Fatalf("cache after unknown owner: %s/%s", outcome, reason)
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{{ID: "%child", Assistant: session.AssistantCodex}},
			Sources: map[string]bool{"tmux": true}}
	}
	rep, err = b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, r.ID, ReclaimTaskDir); d.Reason != WhyOwnerPresent {
		t.Fatalf("present owner: %+v", d)
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{{ID: "%root", Assistant: session.AssistantClaude}},
			Sources: map[string]bool{"tmux": true}}
	}
	module := filepath.Join(cache, "go-mod", "example.invalid", "read-only@v1")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(module, 0o555); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cache, "outside")); err != nil {
		t.Fatal(err)
	}
	rep, err = b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, r.ID, ReclaimTaskDir); d.Outcome != ReclaimRemoved {
		t.Fatalf("owner gone: %+v", d)
	}
	if outcome, reason := oneCache(t, b, ctx, r.ID); outcome != ReclaimRemoved || reason != WhyWorkScratch {
		t.Fatalf("cache after removal: %s/%s", outcome, reason)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "untouched" {
		t.Fatalf("symlink target changed: %q, %v", got, err)
	}
}

func TestGateGoCacheRemovalFailureRetriesAndInterruptedIntentRecovers(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", true)
	b.removeTaskScratch = func(_, _, _ string) error { return errors.New("injected interruption") }
	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, r.ID, ReclaimTaskDir); d.Reason != WhyRemoveFailed {
		t.Fatalf("interrupted removal: %+v", d)
	}
	if outcome, reason := oneCache(t, b, ctx, r.ID); outcome != ReclaimKept || reason != WhyRemoveFailed {
		t.Fatalf("interrupted cache: %s/%s", outcome, reason)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("cache lost after injected failure: %v", err)
	}
	b.removeTaskScratch = nil
	if _, err := b.Reclaim(ctx, false); err != nil {
		t.Fatal(err)
	}
	if outcome, _ := oneCache(t, b, ctx, r.ID); outcome != ReclaimRemoved {
		t.Fatalf("retry outcome: %s", outcome)
	}

	// A process may stop after recording the removal intent and unlinking
	// work/, but before it records the final cache outcome. The next sweep
	// reconciles that exact intent rather than losing the cleanup history.
	b2, ctx2, r2, _ := cacheTask(t, "%child", true)
	if err := b2.Store.MarkGoCacheCleanup(ctx2, b2.gateScratch(r2.ID).Cache, "task", r2.ID,
		reclaimRemoving, WhyWorkScratch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(r2.Dir, "work")); err != nil {
		t.Fatal(err)
	}
	if _, err := b2.Reclaim(ctx2, false); err != nil {
		t.Fatal(err)
	}
	if outcome, reason := oneCache(t, b2, ctx2, r2.ID); outcome != ReclaimRemoved || reason != WhyWorkScratch {
		t.Fatalf("recovered intent: %s/%s", outcome, reason)
	}
}
