package taskresources

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/store"
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

func TestSharedGoUseReconcilesAnInterruptedTrimWithoutRepeatingDeletion(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint("other-live-process=", foreign), func(t *testing.T) {
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
			file := filepath.Join(path, "retained-cache-entry")
			if err := os.WriteFile(file, []byte("still reusable"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.Seal(ctx, f.scope, resource.ID); err != nil {
				t.Fatal(err)
			}
			plan, err := f.s.Plan(ctx, f.scope, []string{resource.ID})
			if err != nil || plan.Items[0].Decision.Outcome != "eligible" {
				t.Fatal(plan, err)
			}
			execution := uuid.NewString()
			f.s.Fault = func(phase, id string) error {
				if phase == "after_intent" {
					return fmt.Errorf("synthetic interrupted trim")
				}
				return nil
			}
			if _, err := f.s.Apply(ctx, f.scope, plan, execution); err == nil {
				t.Fatal("fault did not interrupt cleanup")
			}
			consumer := f.s
			if foreign {
				// The Store protocol permits one handle per daemon process. Use
				// a genuinely live other process, not an unsupported second local
				// handle, to prove its pending execution cannot be taken over.
				key := store.ReceiptKey{Scope: "resources.cleanup", Actor: f.scope.Actor, Key: uuid.NewString()}
				processCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				process := exec.CommandContext(processCtx, os.Args[0], "-test.run=^TestSharedGoPendingTrimOwnerProcess$")
				process.Env = append(os.Environ(), "CLAWDLINE_TEST_TRIM_DB="+f.db, "CLAWDLINE_TEST_TRIM_KEY="+key.Key, "CLAWDLINE_TEST_TRIM_DIGEST="+plan.Digest)
				process.Stderr = os.Stderr
				input, err := process.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := process.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := process.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					_ = input.Close()
					if err := process.Wait(); err != nil {
						t.Error(err)
					}
				}()
				if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
					t.Fatal("pending owner did not start", line, err)
				}
				if _, err := f.s.Store.UpdateTaskResource(ctx, resource.ID, "resource.test_pending", f.now, func(r *taskresource.Resource) error { r.LastExecution = key.Key; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			attached := false
			err = consumer.UseSharedGo(ctx, f.scope, f.scope.Project, func(r taskresource.Resource, p string) error {
				attached = r.ID == resource.ID && p == path
				return nil
			})
			if foreign {
				if err == nil || attached {
					t.Fatal("another live handle's cleanup was taken over", err)
				}
				current, readErr := f.s.Store.TaskResource(ctx, resource.ID)
				if readErr != nil || current.LastOutcome != "removing" {
					t.Fatal("pending intent was changed", current, readErr)
				}
				pending, readErr := f.s.Store.LookupReceipt(ctx, store.ReceiptKey{Scope: "resources.cleanup", Actor: current.LastActor, Key: current.LastExecution})
				if readErr != nil || pending.Outcome != store.ReceiptPending {
					t.Fatal("another live execution's receipt was changed", pending, readErr)
				}
			} else if err != nil || !attached {
				t.Fatal("interrupted cleanup permanently blocked normal cache use", err)
			} else {
				replayed, replayErr := f.s.Apply(ctx, f.scope, plan, execution)
				if replayErr != nil || !replayed.Replayed || !replayed.OutcomeUnknown {
					t.Fatal("reconciliation replaced an uncertain receipt", replayed, replayErr)
				}
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "still reusable" {
				t.Fatal("reconciliation repeated deletion", err)
			}
			preserved(t, f)
		})
	}
}

func TestSharedGoPendingTrimOwnerProcess(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_TEST_TRIM_DB")
	if dir == "" {
		return
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key := store.ReceiptKey{Scope: "resources.cleanup", Actor: "person", Key: os.Getenv("CLAWDLINE_TEST_TRIM_KEY")}
	if _, err := st.ClaimReceipt(context.Background(), key, os.Getenv("CLAWDLINE_TEST_TRIM_DIGEST"), store.ReceiptPolicy{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}
