package orchestrator

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// Completion and periodic requests share one advancing worker, separate from
// legacy checkout reclamation. A terminal callback alone never proves idle.
type managedResourceCleanup struct {
	mu               sync.Mutex
	running, pending bool
	measure          bool
	after            string
	retryAt          time.Time
	cancel           context.CancelFunc
	done             chan struct{}
	closed           bool
}

func (b *Broker) requestManagedResourceCleanup(ctx context.Context) {
	b.scheduleManagedResourceCleanup(ctx, false)
}

func (b *Broker) scheduleManagedResourceCleanup(ctx context.Context, measure bool) {
	if !b.ReclaimAuto || b.Resources == nil {
		return
	}
	state := &b.managedCleanup
	state.mu.Lock()
	state.pending = true
	state.measure = state.measure || measure
	state.mu.Unlock()
	b.runManagedResourceCleanup(ctx)
}

func (b *Broker) runManagedResourceCleanup(ctx context.Context) {
	if !b.ReclaimAuto || b.Resources == nil {
		return
	}
	state := &b.managedCleanup
	state.mu.Lock()
	if state.closed || state.running || !state.pending || b.now().Before(state.retryAt) {
		state.mu.Unlock()
		return
	}
	state.running, state.pending = true, false
	after, measure := state.after, state.measure
	state.measure = false
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), reclaimDeadline)
	done := make(chan struct{})
	state.cancel, state.done = cancel, done
	state.mu.Unlock()
	go func() {
		budget := capacity.ResourceCleanupItemSecondsLimit * time.Second
		next, err := b.sweepManagedResourceBatches(run, after, budget)
		if measure && err == nil {
			sample, stop := context.WithTimeout(run, budget)
			_, _ = b.Resources.Measure(sample)
			stop()
		}
		if err != nil && run.Err() == nil {
			log.Printf("managed resource cleanup paused: %v", err)
		}
		state.mu.Lock()
		state.after = next
		state.retryAt = time.Time{}
		if err != nil && !state.closed {
			state.pending = true
			state.measure = state.measure || measure
			if run.Err() == nil {
				state.retryAt = b.now().Add(capacity.ResourceCleanupBackoffSecondsLimit * time.Second)
			}
		}
		cancel()
		state.running = false
		close(done)
		state.mu.Unlock()
	}()
}

func (b *Broker) sweepManagedResourceBatches(ctx context.Context, after string, budget time.Duration) (string, error) {
	for ctx.Err() == nil {
		page, stop := context.WithTimeout(ctx, budget)
		rows, next, err := b.Store.TaskResourcePage(page, after, capacity.ResourceCleanupBatchRowsLimit, true)
		stop()
		if err != nil {
			return after, err
		}
		for _, r := range rows {
			wait, stop := context.WithTimeout(ctx, budget)
			started := false
			err := b.Resources.Maintenance(wait, func() error {
				started = true
				stop()
				item, cancel := context.WithTimeout(ctx, budget)
				defer cancel()
				return b.sweepManagedResource(item, r.ID)
			})
			stop()
			if ctx.Err() != nil {
				return after, ctx.Err()
			}
			if !started {
				// The admission gate was busy. Resume from this resource.
				return after, err
			}
			if err != nil {
				// A slow or unstable resource cannot hold up later rows. It
				// remains registered for the next completion or periodic pass.
				log.Printf("managed resource cleanup deferred for %s: %v", r.ID, err)
			}
			after = r.ID
		}
		if next == "" {
			return "", nil
		}
	}
	return after, ctx.Err()
}

func (b *Broker) sweepManagedResource(ctx context.Context, id string) error {
	var err error
	for range capacity.ResourceCleanupPlanAttemptsLimit {
		result, applyErr := b.Resources.SweepResource(ctx, id)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = applyErr
		if len(result.Items) == 1 && result.Items[0].Reason == "resource_changed_after_plan" {
			err = taskresources.Failure{Code: result.Items[0].Reason}
		}
		var failure taskresources.Failure
		if !errors.As(err, &failure) || (failure.Code != "resource_plan_changed" && failure.Code != "resource_changed_after_plan") {
			return err
		}
	}
	return err
}

// StopResourceWorkers fences new preparation before closing its filesystem or
// store. Accepted lease waiters remain durable and resume on a later poll.
func (b *Broker) StopResourceWorkers(ctx context.Context) error {
	preparation, cleanup := &b.resourcePreparation, &b.managedCleanup
	preparation.mu.Lock()
	preparation.closed = true
	if preparation.cancel != nil {
		preparation.cancel()
	}
	pDone := preparation.done
	preparation.mu.Unlock()
	cleanup.mu.Lock()
	cleanup.closed = true
	if cleanup.cancel != nil {
		cleanup.cancel()
	}
	cDone := cleanup.done
	cleanup.mu.Unlock()
	for _, done := range []<-chan struct{}{pDone, cDone} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (b *Broker) CloseTaskResources() error {
	// Allow the existing final-record grace for cancelled work to settle before
	// closing descriptors. An unknown stop leaves the filesystem handle open.
	ctx, cancel := recordContext(context.Background())
	defer cancel()
	if err := b.StopResourceWorkers(ctx); err != nil {
		return err
	}
	return b.Resources.FS.Close()
}
