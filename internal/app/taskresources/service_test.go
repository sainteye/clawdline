package taskresources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

type fixture struct {
	s              *Service
	owner          *OwnerState
	scope          Scope
	allocation     Allocation
	output, db, fs string
	now            time.Time
}

func setup(t *testing.T, class taskresource.Class) fixture {
	t.Helper()
	base := t.TempDir()
	db := filepath.Join(base, "state")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(base, "resources")
	root, err := resourcefs.Open(managed)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(base, "artifacts")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "result.txt"), []byte("retained result"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_790_000_000, 0)
	owner := &OwnerState{Owner: taskresource.Owner{Kind: "task", ID: "30000000-0000-4000-8000-000000000001", Project: base}, Presence: "present", UsePresence: "present", OutputRoot: output, EvidenceDigest: "synthetic-durable-result"}
	s := &Service{Store: st, FS: root, Clock: func() time.Time { return now.Add(-26 * time.Hour) }, FreeSpace: func() (int64, error) { return 1 << 40, nil }, Owner: func(_ context.Context, kind, id string) (OwnerState, error) {
		if kind != owner.Owner.Kind || id != owner.Owner.ID {
			return OwnerState{}, fail("owner_unknown")
		}
		return *owner, nil
	}}
	t.Cleanup(func() { s.FS.Close(); s.Store.Close() })
	scope := Scope{Actor: "person", Project: base, Inspect: true, Reclaim: true, Publish: true, Unpin: true}
	allocation, err := s.Allocate(context.Background(), scope, "task", owner.Owner.ID, class)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allocation.Path, "rebuildable.tmp"), []byte("temporary computation"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Clock = func() time.Time { return now }
	owner.Complete = true
	owner.CompletedAt = now.Add(-25 * time.Hour).Unix()
	owner.Presence = "absent"
	owner.UsePresence = "absent"
	owner.OutputsSaved = true
	return fixture{s, owner, scope, allocation, output, db, managed, now}
}

func (f fixture) seal(t *testing.T) Plan {
	t.Helper()
	ctx := context.Background()
	if _, err := f.s.Seal(ctx, f.scope, f.allocation.Resource.ID); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.Plan(ctx, f.scope, []string{f.allocation.Resource.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Decision.Outcome != "eligible" {
		t.Fatal(p)
	}
	return p
}

func preserved(t *testing.T, f fixture) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.output, "result.txt"))
	if err != nil || string(b) != "retained result" {
		t.Fatal("protected result changed", err)
	}
}

func TestInspectionIsReadOnlyAndCleanupReplaysItsReceipt(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	unsealed, err := f.s.Plan(ctx, f.scope, []string{f.allocation.Resource.ID})
	if err != nil || unsealed.Items[0].Decision.Reason != "outputs_unsealed" {
		t.Fatal(unsealed, err)
	}
	plan := f.seal(t)
	before, err := f.s.Store.TaskResource(ctx, f.allocation.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.s.Inventory(ctx, f.scope, "", 1)
	if err != nil || len(page.Items) != 1 || page.Next == "" {
		t.Fatal(page, err)
	}
	after, err := f.s.Store.TaskResource(ctx, f.allocation.Resource.ID)
	if err != nil || before != after {
		t.Fatal("inspection changed the register", after, err)
	}
	execution := uuid.NewString()
	result, err := f.s.Apply(ctx, f.scope, plan, execution)
	if err != nil || len(result.Items) != 1 || result.Items[0].Outcome != "removed" {
		t.Fatal(result, err)
	}
	if absent, err := f.s.FS.Absent(f.allocation.Resource); err != nil || !absent {
		t.Fatal(absent, err)
	}
	replay, err := f.s.Apply(ctx, f.scope, plan, execution)
	if err != nil || !replay.Replayed || replay.Items[0] != result.Items[0] {
		t.Fatal(replay, err)
	}
	preserved(t, f)
}

func TestBotCanRequestCleanupButCannotPublishUnpinOrReadAnotherProject(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	plan := f.seal(t)
	bot := Scope{Actor: "inspection-bot", Project: f.owner.Owner.Project, Inspect: true, Reclaim: true}
	if _, err := f.s.Seal(ctx, bot, f.allocation.Resource.ID); err == nil {
		t.Fatal("Bot published an owner's preservation proof")
	}
	if _, err := f.s.Pin(ctx, f.scope, f.allocation.Resource.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Pin(ctx, bot, f.allocation.Resource.ID, false); err == nil {
		t.Fatal("Bot removed a pin")
	}
	if _, err := f.s.Apply(ctx, bot, plan, uuid.NewString()); err == nil {
		t.Fatal("stale plan ignored a pin")
	}
	other := bot
	other.Project = t.TempDir()
	page, err := f.s.Inventory(ctx, other, "", 1)
	if err != nil || len(page.Items) != 0 || page.Next == "" {
		t.Fatal("scope leaked or cursor stalled", page, err)
	}
	if _, err := f.s.Plan(ctx, other, []string{f.allocation.Resource.ID}); err == nil {
		t.Fatal("cross-Project resource read")
	}
	preserved(t, f)
}

func TestChangedBytesOutputsReaderAndPolicyInvalidatePlans(t *testing.T) {
	for _, change := range []string{"scratch", "output", "reader", "policy"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t, taskresource.Scratch)
			ctx := context.Background()
			plan := f.seal(t)
			switch change {
			case "scratch":
				if err := os.WriteFile(filepath.Join(f.allocation.Path, "new.tmp"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "output":
				if err := os.WriteFile(filepath.Join(f.output, "new.txt"), []byte("new result"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "reader":
				f.owner.UsePresence = "present"
			case "policy":
				p := taskresource.DefaultPolicy()
				p.ScratchSeconds++
				f.s.Policy = func() taskresource.Policy { return p }
			}
			if _, err := f.s.Apply(ctx, f.scope, plan, uuid.NewString()); err == nil {
				t.Fatal("stale plan removed data")
			}
			if absent, err := f.s.FS.Absent(f.allocation.Resource); err != nil || absent {
				t.Fatal("resource removed", err)
			}
			preserved(t, f)
		})
	}
}

func TestReaderAppearingAfterIntentIsKept(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	plan := f.seal(t)
	f.s.Fault = func(point, id string) error {
		if point == "after_intent" {
			f.owner.UsePresence = "present"
		}
		return nil
	}
	result, err := f.s.Apply(context.Background(), f.scope, plan, uuid.NewString())
	if err != nil || result.Items[0].Outcome != "kept" || result.Items[0].Reason != "resource_changed_before_remove" {
		t.Fatal(result, err)
	}
	if absent, err := f.s.FS.Absent(f.allocation.Resource); err != nil || absent {
		t.Fatal(absent, err)
	}
	preserved(t, f)
}

func TestInterruptedCleanupReconcilesInsteadOfDeletingTwice(t *testing.T) {
	for _, point := range []string{"after_intent", "after_remove"} {
		t.Run(point, func(t *testing.T) {
			f := setup(t, taskresource.Scratch)
			ctx := context.Background()
			plan := f.seal(t)
			execution := uuid.NewString()
			f.s.Fault = func(p, id string) error {
				if p == point {
					return errors.New("synthetic interruption")
				}
				return nil
			}
			if _, err := f.s.Apply(ctx, f.scope, plan, execution); err == nil {
				t.Fatal("fault did not interrupt")
			}
			f.s.Fault = nil
			if err := f.s.recoverPage(ctx, ""); err != nil {
				t.Fatal(err)
			}
			r, err := f.s.Store.TaskResource(ctx, f.allocation.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if point == "after_intent" && r.LastOutcome != "kept" {
				t.Fatal("unremoved bytes not retained", r)
			}
			if point == "after_remove" && r.LastOutcome != "removed" {
				t.Fatal("completed unlink not reconciled", r)
			}
			replayed, err := f.s.Apply(ctx, f.scope, plan, execution)
			if err != nil || !replayed.Replayed || !replayed.OutcomeUnknown {
				t.Fatal(replayed, err)
			}
			preserved(t, f)
		})
	}
}

func TestPressureReclaimsOnlyEligibleDataAndKeepsActiveResources(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "eligible", true: "active"}[active], func(t *testing.T) {
			f := setup(t, taskresource.Scratch)
			f.seal(t)
			p := taskresource.DefaultPolicy()
			p.ScratchTotal = 1
			f.s.Policy = func() taskresource.Policy { return p }
			if active {
				f.owner.UsePresence = "present"
			}
			err := f.s.Admit(context.Background())
			if active && err == nil {
				t.Fatal("over-budget active resource admitted new work")
			}
			if !active && err != nil {
				t.Fatal("safe pressure reclaim failed", err)
			}
			absent, err := f.s.FS.Absent(f.allocation.Resource)
			if err != nil || absent == active {
				t.Fatal("wrong pressure disposition", absent, err)
			}
			preserved(t, f)
		})
	}
}

func TestIndividualCachePressureKeepsProtectedDataWithoutBlockingWork(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsealed", true: "active"}[active], func(t *testing.T) {
			f := setup(t, taskresource.Cache)
			p := taskresource.DefaultPolicy()
			p.CacheBytes = 1
			f.s.Policy = func() taskresource.Policy { return p }
			if active {
				f.owner.Complete = false
				f.owner.Presence, f.owner.UsePresence = "present", "present"
			}
			if err := f.s.Admit(context.Background()); err != nil {
				t.Fatal("cache eviction target blocked work before outputs could be sealed", err)
			}
			if raw, err := os.ReadFile(filepath.Join(f.allocation.Path, "rebuildable.tmp")); err != nil || string(raw) != "temporary computation" {
				t.Fatal("protected cache was removed", err)
			}
			// The aggregate limit still refuses work without deleting active or
			// unsealed data. Only the per-cache eviction target is nonblocking.
			p.CacheTotal = p.CacheBytes
			if err := f.s.Admit(context.Background()); err == nil || err.Error() != "cache_total_over_budget" {
				t.Fatal("aggregate protection lost", err)
			}
			preserved(t, f)
		})
	}
}

func TestPendingResourceIntentSurvivesActualStoreRestart(t *testing.T) {
	for _, unlinked := range []bool{false, true} {
		t.Run(map[bool]string{false: "before unlink", true: "after unlink"}[unlinked], func(t *testing.T) {
			f := setup(t, taskresource.Scratch)
			ctx := context.Background()
			plan := f.seal(t)
			execution := uuid.NewString()
			key := store.ReceiptKey{Scope: "resources.cleanup", Actor: f.scope.Actor, Key: execution}
			if _, err := f.s.Store.ClaimReceipt(ctx, key, plan.Digest, store.ReceiptPolicy{}, f.now); err != nil {
				t.Fatal(err)
			}
			r := plan.Items[0].Resource
			r.LastPlan = plan.Digest
			r.LastTargets = r.ID
			if _, err := f.s.mark(ctx, r, execution, f.scope.Actor, "removing", "completed_owned_scratch"); err != nil {
				t.Fatal(err)
			}
			if unlinked {
				if err := f.s.FS.Remove(ctx, r, plan.Items[0].Snapshot.Digest); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.s.Store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.s.FS.Close(); err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(f.db)
			if err != nil {
				t.Fatal(err)
			}
			f.s.Store = st
			root, err := resourcefs.Open(f.fs)
			if err != nil {
				t.Fatal(err)
			}
			f.s.FS = root
			if err := f.s.recoverPage(ctx, ""); err != nil {
				t.Fatal(err)
			}
			current, err := st.TaskResource(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := "kept"
			if unlinked {
				expected = "removed"
			}
			if current.LastOutcome != expected {
				t.Fatal(current)
			}
			result, err := f.s.Apply(ctx, f.scope, plan, execution)
			if err != nil || !result.OutcomeUnknown {
				t.Fatal(result, err)
			}
			if len(result.Items) != 1 || result.Items[0].LogicalBytes != 0 {
				t.Fatal("recovery invented freed bytes", result)
			}
			preserved(t, f)
		})
	}
}

func TestScopedInspectionDoesNotExposeCrossProjectRecoveryTargets(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	f.seal(t)
	targets := f.allocation.Resource.ID + "," + uuid.NewString()
	if _, err := f.s.Store.UpdateTaskResource(ctx, f.allocation.Resource.ID, "resource.kept", f.now, func(r *taskresource.Resource) error { r.LastTargets = targets; return nil }); err != nil {
		t.Fatal(err)
	}
	bot := Scope{Actor: "inspection-bot", Project: f.owner.Owner.Project, Inspect: true, Reclaim: true}
	page, err := f.s.Inventory(ctx, bot, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Resource.LastTargets != "" {
		t.Fatal("recovery target list leaked", page, err)
	}
	pinned, err := f.s.Pin(ctx, bot, f.allocation.Resource.ID, true)
	if err != nil || pinned.LastTargets != "" {
		t.Fatal("protection reply leaked recovery targets", pinned, err)
	}
	stored, err := f.s.Store.TaskResource(ctx, f.allocation.Resource.ID)
	if err != nil || stored.LastTargets != targets {
		t.Fatal("private recovery evidence was discarded", stored, err)
	}
	denied := bot
	denied.Project = t.TempDir()
	r, err := f.s.resource(ctx, denied, f.allocation.Resource.ID)
	if err == nil || r.ID != "" {
		t.Fatal("denied lookup returned a foreign resource", r, err)
	}
}

func TestInterruptedMeasurementKeepsItsCause(t *testing.T) {
	for _, phase := range []string{"legacy", "after legacy", "free space"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t, taskresource.Scratch)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := context.Canceled
			switch phase {
			case "legacy":
				cause = context.DeadlineExceeded
				f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
					return taskresource.Usage{}, context.DeadlineExceeded
				}
			case "after legacy":
				f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
					cancel()
					return taskresource.Usage{Known: true}, nil
				}
			case "free space":
				f.s.FreeSpace = func() (int64, error) { cancel(); return 1 << 40, nil }
			}
			_, err := f.s.Measure(ctx)
			var failure Failure
			if !errors.As(err, &failure) || failure.Code != "resource_measurement_interrupted" || !errors.Is(err, cause) {
				t.Fatalf("interrupted measurement lost its classification or cause: %v", err)
			}
			if f.s.Usage().Known {
				t.Fatal("interrupted measurement published a known total")
			}
			preserved(t, f)
		})
	}
}

func TestPressureAdmissionVisitsOneAdvancingCleanupPage(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	f.owner.Complete = false
	for i := 0; i < taskresource.PageRowsLimit; i++ {
		if _, err := f.s.Allocate(ctx, Scope{Actor: "daemon", AllProjects: true, Inspect: true, Publish: true}, "task", f.owner.Owner.ID, taskresource.Scratch); err != nil {
			t.Fatal(err)
		}
	}
	f.owner.Complete = true
	seen := map[string]bool{}
	f.s.Uses = func(_ context.Context, r taskresource.Resource) string { seen[r.ID] = true; return "present" }
	p := taskresource.DefaultPolicy()
	f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		return taskresource.Usage{Known: true, ScratchTotal: p.ScratchTotal + 1}, nil
	}
	if err := f.s.Admit(ctx); err == nil {
		t.Fatal("active over-budget resources were admitted")
	}
	if len(seen) > taskresource.PageRowsLimit {
		t.Fatalf("one admission inspected %d resources, page limit %d", len(seen), taskresource.PageRowsLimit)
	}
	prior := seen
	seen = map[string]bool{}
	if err := f.s.Admit(ctx); err == nil {
		t.Fatal("next over-budget admission passed")
	}
	advanced := false
	for id := range seen {
		if !prior[id] {
			advanced = true
		}
	}
	if !advanced {
		t.Fatal("later resources were starved by the admission cleanup cursor")
	}
	preserved(t, f)
}

func TestCacheTargetAdmissionDoesNotWaitForOptionalOwnerInspection(t *testing.T) {
	f := setup(t, taskresource.Cache)
	p := taskresource.DefaultPolicy()
	p.CacheBytes = 1
	f.s.Policy = func() taskresource.Policy { return p }
	ownerCalls, measurements := 0, 0
	f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		measurements++
		return taskresource.Usage{Known: true}, nil
	}
	f.s.Owner = func(c context.Context, _, _ string) (OwnerState, error) {
		ownerCalls++
		<-c.Done()
		return OwnerState{}, c.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := f.s.Admit(ctx)
	t.Logf("same request: measurements=%d owner_inspections=%d elapsed=%s context=%v", measurements, ownerCalls, time.Since(start), ctx.Err())
	if err != nil || ownerCalls != 0 || measurements != 1 {
		t.Fatalf("optional cache maintenance consumed admission deadline: err=%v owners=%d measurements=%d", err, ownerCalls, measurements)
	}
	preserved(t, f)
}

func TestUsageAccountingDoesNotGrantDeletionOfProtectedContents(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	if err := os.WriteFile(filepath.Join(f.allocation.Path, "result.json"), []byte("protected test fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := f.s.Measure(context.Background())
	if err != nil || !u.Known || u.ScratchTotal == 0 {
		t.Fatal("a protected test name made all storage unknown", u, err)
	}
	if _, err := f.s.Seal(context.Background(), f.scope, f.allocation.Resource.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.Plan(context.Background(), f.scope, []string{f.allocation.Resource.ID})
	if err != nil || len(plan.Items) != 1 || plan.Items[0].Decision.Outcome != "kept" || plan.Items[0].Decision.Reason != "resource_tree_unverified" {
		t.Fatal("accounting weakened deletion verification", plan, err)
	}
	preserved(t, f)
}

func TestAdmissionProofInvalidatesOnUsePolicyAndIncarnation(t *testing.T) {
	f := setup(t, taskresource.Cache)
	ctx := context.Background()
	if _, err := f.s.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	ticket := f.s.AdmissionTicket()
	if ticket.Instance == "" || !f.s.ConsumeAdmission(ticket) || f.s.ConsumeAdmission(ticket) {
		t.Fatal("proof reused after grant")
	}
	if _, err := f.s.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	ticket = f.s.AdmissionTicket()
	p := taskresource.DefaultPolicy()
	p.DiskReserve++
	f.s.Policy = func() taskresource.Policy { return p }
	if f.s.ConsumeAdmission(ticket) {
		t.Fatal("changed policy accepted old proof")
	}
	f.s.Policy = nil
	if _, err := f.s.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	ticket = f.s.AdmissionTicket()
	other := &Service{Store: f.s.Store, FS: f.s.FS, Owner: f.s.Owner, FreeSpace: f.s.FreeSpace}
	if _, err := other.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	_ = other.AdmissionTicket()
	if other.ConsumeAdmission(ticket) {
		t.Fatal("another service accepted old proof")
	}
}

func TestMeasurementCannotOverwriteAnAllocationInvalidation(t *testing.T) {
	f := setup(t, taskresource.Cache)
	ctx := context.Background()
	f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		f.owner.Complete = false
		_, err := f.s.Allocate(ctx, f.scope, "task", f.owner.Owner.ID, taskresource.Scratch)
		if err != nil {
			t.Fatal(err)
		}
		return taskresource.Usage{Known: true}, nil
	}
	if _, err := f.s.Measure(ctx); !errors.Is(err, errMeasurementChanged) || f.s.Usage().Known || f.s.AdmissionTicket().Instance != "" {
		t.Fatal("concurrent mutation became a verified admission proof", err)
	}
}

func TestAdmissionRepeatsAccountingAfterRealRemoval(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	plan := f.seal(t)
	attempts := 0
	f.s.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		attempts++
		if attempts == 1 {
			if _, err := f.s.Apply(c, f.scope, plan, uuid.NewString()); err != nil {
				return taskresource.Usage{}, err
			}
		}
		return taskresource.Usage{Known: true}, nil
	}
	if err := f.s.Admit(ctx); err != nil || attempts != 2 {
		t.Fatalf("admission with a removal during the first scan: %d scans, %v", attempts, err)
	}
	if usage := f.s.Usage(); !usage.Known || usage.ScratchTotal != 0 || f.s.AdmissionTicket().Instance == "" {
		t.Fatalf("expected a fresh admission proof for the post-removal total: %+v", usage)
	}
	preserved(t, f)
}

func TestMeasurementWaitsForRemovalReceiptAndCanBeCancelled(t *testing.T) {
	f := setup(t, taskresource.Scratch)
	ctx := context.Background()
	plan := f.seal(t)
	removed, release := make(chan struct{}), make(chan struct{})
	f.s.Fault = func(point, _ string) error {
		if point == "after_remove" {
			close(removed)
			<-release
		}
		return nil
	}
	applied := make(chan error, 1)
	go func() {
		_, err := f.s.Apply(ctx, f.scope, plan, uuid.NewString())
		applied <- err
	}()
	var applyErr error
	finish := sync.OnceFunc(func() { close(release); applyErr = <-applied })
	defer finish()
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("fixture did not reach the unpublished removal")
	}
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := f.s.Measure(wait)
	var failure Failure
	if !errors.As(err, &failure) || !errors.Is(err, context.DeadlineExceeded) || failure.Diagnostic().Phase != "measure.begin" || f.s.Usage().Known {
		t.Fatal("accounting crossed a removal with no durable outcome", err)
	}
	finish()
	if applyErr != nil {
		t.Fatal(applyErr)
	}
	if u, err := f.s.Measure(ctx); err != nil || !u.Known || u.ScratchTotal != 0 {
		t.Fatal("accounting did not observe the durable removal", u, err)
	}
}

func TestAdmissionProofRechecksReserveAndRootWithoutWaitingForMutation(t *testing.T) {
	f := setup(t, taskresource.Cache)
	ctx := context.Background()
	if _, err := f.s.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	ticket := f.s.AdmissionTicket()
	f.s.mu.Lock()
	ready := f.s.ConsumeAdmission(ticket)
	f.s.mu.Unlock()
	if ready {
		t.Fatal("busy mutation was accepted")
	}
	free := f.s.FreeSpace
	f.s.FreeSpace = func() (int64, error) { return 1, nil }
	if f.s.ConsumeAdmission(ticket) {
		t.Fatal("old reserve authorized a grant")
	}
	f.s.FreeSpace = free
	if err := os.WriteFile(filepath.Join(f.fs, ".owner"), []byte("changed boundary"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.s.ConsumeAdmission(ticket) {
		t.Fatal("changed managed root accepted")
	}
}

func TestCancelledAdmissionDoesNotWaitForUnrelatedMaintenance(t *testing.T) {
	f := setup(t, taskresource.Cache)
	ctx := context.Background()
	if _, err := f.s.Measure(ctx); err != nil {
		t.Fatal(err)
	}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		_ = f.s.Maintenance(ctx, func() error { close(entered); <-release; return nil })
	}()
	stop := sync.OnceFunc(func() { close(release); <-finished })
	defer stop()
	<-entered
	wait, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- f.s.Admit(wait) }()
	cancel()
	select {
	case err := <-result:
		var failure Failure
		if !errors.As(err, &failure) || failure.Diagnostic().Phase != "admission.wait" || !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if !f.s.Usage().Known {
			t.Fatal("a cancelled waiter invalidated current accounting")
		}
	case <-time.After(200 * time.Millisecond):
		stop()
		<-result
		t.Fatal("cancelled admission waited for unrelated maintenance")
	}
}
