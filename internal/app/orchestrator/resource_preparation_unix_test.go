//go:build darwin || linux

package orchestrator

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestStoragePreparationOutlivesTheShortLeaseRequest(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer func() { close(release); waitPreparationIdle(t, b) }()
	b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return taskresource.Usage{Known: true}, nil
		case <-c.Done():
			return taskresource.Usage{}, c.Err()
		}
	}
	attempt, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic delayed disk preparation"}
	answer, err := b.Acquire(attempt, req)
	if err != nil || answer.State != "queued" || answer.HoldReason != "storage_preparing" {
		t.Fatalf("disk preparation consumed the short request: answer=%+v err=%v", answer, err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background preparation did not start")
	}
	leases, err := b.Leases(ctx)
	if err != nil || len(leases) != 1 || leases[0].Holder != nil || len(leases[0].Queue) != 1 {
		t.Fatal("preparation must persist a waiter without holding compilation", leases, err)
	}
}

func waitPreparationIdle(t *testing.T, b *Broker) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for {
		b.resourcePreparation.mu.Lock()
		busy := b.resourcePreparation.running
		b.resourcePreparation.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(end) {
			t.Fatal("preparation did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}
func preparedLease(t *testing.T, b *Broker, ctx context.Context, req LeaseRequest) LeaseAnswer {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for {
		a, err := b.Acquire(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if a.State == "granted" {
			return a
		}
		if time.Now().After(end) {
			t.Fatal("prepared request never acquired its lease", a)
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPreparationCoalescesPollsAndOnlyGrantsAfterReadiness(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	var calls atomic.Int32
	release := make(chan struct{})
	closed := false
	b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		calls.Add(1)
		select {
		case <-release:
			return taskresource.Usage{Known: true}, nil
		case <-c.Done():
			return taskresource.Usage{}, c.Err()
		}
	}
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic coalesced preparation"}
	t.Cleanup(func() {
		if !closed {
			close(release)
		}
		b.cancelResourcePreparation(req.RequestID)
		waitPreparationIdle(t, b)
	})
	for i := 0; i < 10; i++ {
		a, err := b.Acquire(ctx, req)
		if err != nil || a.State != "queued" {
			t.Fatal(a, err)
		}
	}
	close(release)
	closed = true
	waitPreparationIdle(t, b)
	a := preparedLease(t, b, ctx, req)
	if a.LeaseID == "" || calls.Load() != 1 {
		t.Fatal("duplicate poll repeated the preparation", a, calls.Load())
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: req.RequestID}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(ctx, req); refusalCode(err) != "lease_completed" {
		t.Fatal("released identity started another compilation", err)
	}
}

func TestCancelledInFlightPreparationStopsWithoutALateResult(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	entered := make(chan struct{})
	b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		close(entered)
		<-c.Done()
		return taskresource.Usage{}, c.Err()
	}
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic cancelled preparation"}
	if _, err := b.Acquire(ctx, req); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := b.Cancel(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: req.RequestID}); err != nil {
		t.Fatal(err)
	}
	waitPreparationIdle(t, b)
	results, err := b.Store.LatestEvents(ctx, "resource.preparation.finished", []string{req.RequestID})
	if err != nil || len(results) != 0 {
		t.Fatal("cancelled work published a late result", results, err)
	}
	if _, err := b.Acquire(ctx, req); refusalCode(err) != "lease_cancelled" {
		t.Fatal(err)
	}
}

func TestStoppingResourceWorkersPreservesTheWaiterWithoutGranting(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	entered := make(chan struct{})
	b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		close(entered)
		<-c.Done()
		return taskresource.Usage{}, c.Err()
	}
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic daemon stop"}
	if _, err := b.Acquire(ctx, req); err != nil {
		t.Fatal(err)
	}
	<-entered
	stop, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := b.StopResourceWorkers(stop); err != nil {
		t.Fatal(err)
	}
	st, err := b.Store.Lease(ctx, ResourceCompile, compileKey)
	if err != nil || st.Holder != nil || len(st.Waiters) != 1 {
		t.Fatal(st, err)
	}
}
func TestCancelledPreparationCannotLaterGrantOrReuseItsReadyResult(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic cancellation"}
	if a, err := b.Acquire(ctx, req); err != nil || a.State != "queued" {
		t.Fatal(a, err)
	}
	waitPreparationIdle(t, b)
	if _, err := b.Cancel(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: req.RequestID}); err != nil {
		t.Fatal(err)
	}
	if a, err := b.Acquire(ctx, req); refusalCode(err) != "lease_cancelled" || a.State == "granted" {
		t.Fatal("cancelled request reused a ready proof", a, err)
	}
	st, err := b.Store.Lease(ctx, ResourceCompile, compileKey)
	if err != nil || st.Holder != nil || len(st.Waiters) != 0 {
		t.Fatal(st, err)
	}
}
func TestPreparationRestartRequiresAFreshServiceProof(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic restart"}
	if _, err := b.Acquire(ctx, req); err != nil {
		t.Fatal(err)
	}
	waitPreparationIdle(t, b)
	old := b.Resources
	next := &Broker{Store: b.Store, AsyncResourceAdmission: true, Resources: &taskresources.Service{Store: old.Store, FS: old.FS, Owner: old.Owner, FreeSpace: old.FreeSpace, OtherUsage: old.OtherUsage}}
	var calls atomic.Int32
	next.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		calls.Add(1)
		return taskresource.Usage{Known: true}, nil
	}
	if a, err := next.Acquire(ctx, req); err != nil || a.State != "queued" {
		t.Fatal("old incarnation authorized a new broker", a, err)
	}
	preparedLease(t, next, ctx, req)
	waitPreparationIdle(t, next)
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestExpiredPreparationRequiresAnotherMeasurement(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	var calls atomic.Int32
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		calls.Add(1)
		return taskresource.Usage{Known: true}, nil
	}
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic expired proof"}
	if _, err := b.Acquire(ctx, req); err != nil {
		t.Fatal(err)
	}
	waitPreparationIdle(t, b)
	events, err := b.Store.LatestEvents(ctx, "resource.preparation.finished", []string{req.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	var result resourcePreparationResult
	if err := json.Unmarshal(events[req.RequestID].Payload, &result); err != nil {
		t.Fatal(err)
	}
	result.At -= taskresource.PreparationReadySecondsLimit + 1
	raw, _ := json.Marshal(result)
	if err := b.Store.Append(ctx, store.Event{Kind: "resource.preparation.finished", Subject: req.RequestID, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	if a, err := b.Acquire(ctx, req); err != nil || a.State != "queued" {
		t.Fatal("expired readiness granted a lease", a, err)
	}
	preparedLease(t, b, ctx, req)
	waitPreparationIdle(t, b)
	if calls.Load() != 2 {
		t.Fatal("expiry did not require fresh accounting", calls.Load())
	}
}
func TestPreparationReturnsDurableHardCapacityRefusalWithoutHoldingLease(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	b.Resources.FreeSpace = func() (int64, error) { return 1, nil }
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic reserve pressure"}
	if a, err := b.Acquire(ctx, req); err != nil || a.State != "queued" {
		t.Fatal(a, err)
	}
	waitPreparationIdle(t, b)
	if _, err := b.Acquire(ctx, req); refusalCode(err) != "disk_reserve_unavailable" {
		t.Fatal(err)
	}
	if _, err := b.Acquire(ctx, req); refusalCode(err) != "disk_reserve_unavailable" {
		t.Fatal("retry changed the durable refusal", err)
	}
	events, err := b.Store.LatestEvents(ctx, "resource.preparation.finished", []string{req.RequestID})
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	st, err := b.Store.Lease(ctx, ResourceCompile, compileKey)
	if err != nil || st.Holder != nil || len(st.Waiters) != 0 {
		t.Fatal(st, err)
	}
}
