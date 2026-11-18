//go:build darwin || linux

package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestHeavyCallbackCanContinueWithAnActiveOverTargetCache(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	commitFile(t, project, "source.go", "package fixture\n")
	entry := filepath.Join("ab", fmt.Sprintf("%064x-d", 1))
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", `mkdir -p "$GOCACHE/ab"; printf compiled > "$GOCACHE/`+filepath.ToSlash(entry)+`"; printf retained > "$CLAWDLINE_ARTIFACT_DIR/result.txt"; while ! test -e release; do sleep 0.05; done`)
	req.Dir, req.Intent = project, CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	release := func() { _ = os.WriteFile(filepath.Join(project, "release"), nil, 0o600) }
	t.Cleanup(release)
	var r Record
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		r, _, err = b.Record(ctx, req.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, "artifacts", "result.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback did not publish its ready artifact", r.Verdict)
		}
		time.Sleep(10 * time.Millisecond)
	}
	p := taskresource.DefaultPolicy()
	p.CacheBytes = 1
	b.Resources.Policy = func() taskresource.Policy { return p }
	if err := b.AdmitHeavyResources(ctx); err != nil {
		t.Fatal("active callback could not acquire its next compile", err)
	}
	cacheEntry := filepath.Join(r.Callback.GoCachePath, entry)
	if _, err := os.Stat(cacheEntry); err != nil {
		t.Fatal("active cache entry was removed", err)
	}
	p.CacheTotal = p.CacheBytes
	if err := b.AdmitHeavyResources(ctx); err == nil {
		t.Fatal("aggregate cache pressure allowed another compile")
	}
	p.CacheTotal = taskresource.DefaultPolicy().CacheTotal
	release()
	if r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second); r.State != StateSuccess {
		t.Fatal(r.Verdict)
	}
	// Completed output publication is sealed on the next admission, without
	// waiting for the periodic reclaimer or a person to publish it manually.
	if err := b.AdmitHeavyResources(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheEntry); !os.IsNotExist(err) {
		t.Fatal("released cache was not trimmed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(r.Dir, "artifacts", "result.txt")); err != nil || string(raw) != "retained" {
		t.Fatal("saved result changed", err)
	}
}

func TestHeavyCallbacksShareOnlyGoCacheAndReleasePerCommand(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	commitFile(t, project, "source.go", "package fixture\n")
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, project, "worktree", "add", "-q", "-b", "linked", linked)
	start := func(dir, script string) Record {
		req := callbackRequest(t, uuid.NewString(), "sh", "-c", script)
		req.Dir, req.Intent = dir, CallbackIntentHeavy
		if _, err := b.StartCallback(ctx, req); err != nil {
			t.Fatal(err)
		}
		r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
		if r.State != StateSuccess {
			t.Fatal(r.Verdict)
		}
		return r
	}
	first := start(project, `printf reused > "$GOCACHE/proof"; printf result > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	second := start(linked, `test "$(cat "$GOCACHE/proof")" = reused; printf result > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	if first.Callback.GoCacheID == "" || first.Callback.GoCacheID != second.Callback.GoCacheID || first.Callback.CachePath == second.Callback.CachePath || first.Callback.ScratchPath == second.Callback.ScratchPath {
		t.Fatal("cache sharing crossed scratch boundaries", first.Callback, second.Callback)
	}
	resource, err := b.Store.TaskResource(ctx, first.Callback.GoCacheID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := b.sharedGoOwner(ctx, resource.Owner.ID)
	if err != nil || !owner.Complete || !owner.OutputsSaved || owner.Presence != "absent" {
		t.Fatal(owner, err)
	}
	if !b.callbackResourceMetadataNeeded(ctx, first.ID) {
		t.Fatal("shared consumer exit proof can expire")
	}
	if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), resource.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{resource.ID})
	if err != nil || plan.Items[0].Decision.Outcome != "eligible" {
		t.Fatal(plan, err)
	}
	// An unregistered filename is retained. Below-budget shared entries remain
	// reusable, while all ordinary task caches keep independent ownership.
	result, err := b.Resources.Apply(ctx, ResourceOwnerScope(), plan, uuid.NewString())
	if err != nil || result.Items[0].Reason != "shared_cache_within_budget" {
		t.Fatal(result, err)
	}
	if raw, err := os.ReadFile(filepath.Join(first.Callback.GoCachePath, "proof")); err != nil || string(raw) != "reused" {
		t.Fatal(err)
	}
	for _, r := range []Record{first, second} {
		if _, err := os.Stat(filepath.Join(r.Dir, "artifacts", "result.txt")); err != nil {
			t.Fatal(err)
		}
	}
	// A later active consumer invalidates the reviewed old publication. It
	// cannot be made idle by an inspector or by keeping the Root alive.
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", "sleep 2")
	req.Dir, req.Intent = project, CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	// Acceptance precedes preparation. Protection starts with the durable
	// cache-consumer link, before the process can use the shared cache.
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _, err := b.Record(ctx, req.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Callback.GoCacheID == resource.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accepted callback never attached its shared cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
	active, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{resource.ID})
	if err != nil || active.Items[0].Decision.Outcome == "eligible" {
		t.Fatal(active, err)
	}
	if _, err := b.Resources.Apply(ctx, ResourceOwnerScope(), plan, uuid.NewString()); err == nil {
		t.Fatal("stale consumer plan accepted")
	}
	denied := taskresources.Scope{Actor: "future-bot", Project: t.TempDir(), Inspect: true, Reclaim: true}
	if _, err := b.Resources.Plan(ctx, denied, []string{resource.ID}); err == nil {
		t.Fatal("cross-project cache inspection allowed")
	}
	settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	// The original scratch floor remains independent of cache eviction.
	rows, _, _ := b.Store.TaskResourcesByOwnerPage(ctx, "task", first.ID, "")
	for _, row := range rows {
		if row.Class != taskresource.Cache {
			continue
		}
		if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), row.ID); err != nil {
			t.Fatal(err)
		}
		p, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{row.ID})
		if err != nil || p.Items[0].Decision.Outcome != "eligible" {
			t.Fatal(p, err)
		}
	}
}
