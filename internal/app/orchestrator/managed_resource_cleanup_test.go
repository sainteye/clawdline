//go:build darwin || linux

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func managedCleanupFixture(t *testing.T, count int) (*Broker, []taskresources.Allocation) {
	t.Helper()
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.ReclaimAuto = true
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.StopResourceWorkers(ctx); err != nil {
			t.Error(err)
		}
	})
	start := time.Now().Add(-48 * time.Hour)
	b.Resources.Clock = func() time.Time { return start }
	project := t.TempDir()
	owners := make(map[string]taskresources.OwnerState, count)
	b.Resources.Owner = func(_ context.Context, _, id string) (taskresources.OwnerState, error) {
		return owners[id], nil
	}
	scope := ResourceOwnerScope()
	var resources []taskresources.Allocation
	for range count {
		id := uuid.NewString()
		o := taskresources.OwnerState{
			Owner:      taskresource.Owner{Kind: "task", ID: id, Project: project},
			OutputRoot: t.TempDir(),
		}
		owners[id] = o
		a, err := b.Resources.Allocate(ctx, scope, "task", id, taskresource.Scratch)
		if err != nil {
			t.Fatal(err)
		}
		o.Complete, o.OutputsSaved, o.CompletedAt = true, true, start.Unix()
		o.Presence, o.UsePresence, o.EvidenceDigest = "absent", "absent", "saved"
		owners[id] = o
		if err := os.WriteFile(filepath.Join(o.OutputRoot, "result"), []byte("saved output"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.Path, "scratch"), []byte("temporary data"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Resources.Seal(ctx, scope, a.Resource.ID); err != nil {
			t.Fatal(err)
		}
		a.Artifacts = o.OutputRoot
		resources = append(resources, a)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Resource.ID < resources[j].Resource.ID })
	b.Resources.Clock = time.Now
	return b, resources
}

func runManagedCleanupForTest(t *testing.T, b *Broker) {
	t.Helper()
	b.requestManagedResourceCleanup(context.Background())
	b.managedCleanup.mu.Lock()
	done := b.managedCleanup.done
	b.managedCleanup.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("managed cleanup worker did not finish")
	}
}

func TestManagedCleanupReclaimsLaterResourcesAfterDeadline(t *testing.T) {
	b, resources := managedCleanupFixture(t, 2)
	b.Resources.Fault = func(phase, id string) error {
		if phase == "before_intent" && id == resources[0].Resource.ID {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	start := time.Now()
	runManagedCleanupForTest(t, b)
	assertManagedCleanupBytes(t, resources, 1, time.Since(start))
}

func TestManagedCleanupReplansOnlyChangedResource(t *testing.T) {
	b, resources := managedCleanupFixture(t, 2)
	uses := map[string]int{}
	b.Resources.Uses = func(_ context.Context, r taskresource.Resource) string {
		uses[r.ID]++
		if r.ID == resources[0].Resource.ID && (uses[r.ID] == 2 || uses[r.ID] == 3) {
			if err := os.WriteFile(filepath.Join(resources[0].Path, "scratch"), []byte("a changed scratch file"), 0600); err != nil {
				t.Error(err)
				return "unknown"
			}
		}
		return "absent"
	}
	start := time.Now()
	runManagedCleanupForTest(t, b)
	assertManagedCleanupBytes(t, resources, 2, time.Since(start))
	if uses[resources[0].Resource.ID] <= uses[resources[1].Resource.ID] || uses[resources[1].Resource.ID] != 4 {
		t.Fatalf("only the changed resource should be replanned, use probes = %v", uses)
	}
}

func TestManagedCleanupSkipsAScanThatRunsOutOfTime(t *testing.T) {
	b, resources := managedCleanupFixture(t, 2)
	b.Resources.Uses = func(ctx context.Context, r taskresource.Resource) string {
		if r.ID == resources[0].Resource.ID {
			<-ctx.Done()
			return "unknown"
		}
		return "absent"
	}
	start := time.Now()
	if next, err := b.sweepManagedResourceBatches(context.Background(), "", 500*time.Millisecond); next != "" || err != nil {
		t.Fatal(next, err)
	}
	assertManagedCleanupBytes(t, resources, 1, time.Since(start))
}

func TestManagedCleanupResumesAfterLastCompletedResource(t *testing.T) {
	b, resources := managedCleanupFixture(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Resources.Uses = func(_ context.Context, r taskresource.Resource) string {
		if r.ID == resources[1].Resource.ID {
			cancel()
			return "unknown"
		}
		return "absent"
	}
	next, err := b.sweepManagedResourceBatches(ctx, "", 5*time.Second)
	first := resources[0].Resource.ID
	if next != first || !errors.Is(err, context.Canceled) {
		t.Fatalf("want resume after %s on interruption, got %q (%v)", first, next, err)
	}
	row, err := b.Store.TaskResource(context.Background(), first)
	if err != nil || row.LastOutcome != "removed" || row.LastTargets != first {
		t.Fatalf("completed removal should have its own receipt: %+v (%v)", row, err)
	}
	b.Resources.Uses = nil
	start := time.Now()
	if _, err := b.sweepManagedResourceBatches(context.Background(), next, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	assertManagedCleanupBytes(t, resources, 3, time.Since(start))
	retained, err := b.Store.TaskResource(context.Background(), first)
	if err != nil || retained.LastExecution != row.LastExecution {
		t.Fatalf("original receipt should remain: %+v (%v)", retained, err)
	}
}

func TestManagedCleanupKeepsUnverifiedResourcesAndAdvancesToNextBatch(t *testing.T) {
	b, resources := managedCleanupFixture(t, capacity.ResourceCleanupBatchRowsLimit+1)
	readOwner := b.Resources.Owner
	b.Resources.Owner = func(ctx context.Context, kind, id string) (taskresources.OwnerState, error) {
		owner, err := readOwner(ctx, kind, id)
		if id == resources[0].Resource.Owner.ID {
			owner.Presence = "unknown"
		}
		return owner, err
	}
	if err := os.Symlink("scratch", filepath.Join(resources[1].Path, "unverified")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources[2].Artifacts, "result"), []byte("changed output"), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	runManagedCleanupForTest(t, b)
	assertManagedCleanupBytes(t, resources, len(resources)-3, time.Since(start))
}

func assertManagedCleanupBytes(t *testing.T, resources []taskresources.Allocation, removed int, elapsed time.Duration) {
	t.Helper()
	var reclaimed int
	for i, a := range resources {
		data, err := os.ReadFile(filepath.Join(a.Path, "scratch"))
		if i >= len(resources)-removed {
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("eligible scratch %d should be reclaimed, got %q (%v)", i, data, err)
			} else {
				reclaimed += len("temporary data")
			}
		} else if err != nil || string(data) != "temporary data" {
			t.Errorf("deferred scratch %d should remain, got %q (%v)", i, data, err)
		}
		if _, err := os.Stat(filepath.Join(a.Artifacts, "result")); err != nil {
			t.Errorf("saved output %d should remain: %v", i, err)
		}
	}
	t.Logf("reclaimed %d of %d original bytes in %s", reclaimed, len(resources)*len("temporary data"), elapsed)
}
