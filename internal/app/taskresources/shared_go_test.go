package taskresources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestSharedGoPressureTrimsOldEntriesAndReplaysWithoutRemovingTheCache(t *testing.T) {
	f := setup(t, taskresource.Cache)
	ctx := context.Background()
	var resource taskresource.Resource
	var path string
	if err := f.s.UseSharedGo(ctx, f.scope, f.scope.Project, func(r taskresource.Resource, p string) error { resource, path = r, p; return nil }); err != nil {
		t.Fatal(err)
	}
	originalOwner := f.s.Owner
	f.s.Owner = func(ctx context.Context, kind, id string) (OwnerState, error) {
		if kind != "shared_go" {
			return originalOwner(ctx, kind, id)
		}
		return OwnerState{Owner: resource.Owner, Complete: true, CompletedAt: f.now.Unix(), Presence: "absent", UsePresence: "absent", OutputsSaved: true, OutputRoot: f.output, EvidenceDigest: "verified-consumers"}, nil
	}
	if err := os.Mkdir(filepath.Join(path, "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 1))
	recent := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 2))
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, []byte("compiled cache"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := f.now.Add(-time.Hour)
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.s.FS.Snapshot(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	// Derive a one-entry pressure budget from the real filesystem allocation.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	remaining, err := f.s.FS.Snapshot(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("compiled cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	policy := taskresource.DefaultPolicy()
	policy.CacheBytes = remaining.Logical
	if remaining.AllocationKnown {
		policy.CacheBytes = remaining.Allocated
	}
	f.s.Policy = func() taskresource.Policy { return policy }
	if _, err := f.s.Seal(ctx, f.scope, resource.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.Plan(ctx, f.scope, []string{resource.ID})
	if err != nil || plan.Items[0].Decision.Outcome != "eligible" {
		t.Fatal(plan, err)
	}
	execution := uuid.NewString()
	result, err := f.s.Apply(ctx, f.scope, plan, execution)
	if err != nil || result.Items[0].Reason != "shared_cache_trimmed" || result.Items[0].LogicalBytes != snapshot.Logical-remaining.Logical {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old entry retained", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("recent entry lost", err)
	}
	replay, err := f.s.Apply(ctx, f.scope, plan, execution)
	if err != nil || !replay.Replayed || replay.Items[0].LogicalBytes != result.Items[0].LogicalBytes {
		t.Fatal(replay, err)
	}
	if err := f.s.FS.Verify(resource); err != nil {
		t.Fatal("reusable cache removed", err)
	}
	preserved(t, f)
}
