package orchestrator

import (
	"context"
	"log"
	"sync"
)

// Completion notifications coalesce into one advancing worker. It is separate
// from slow legacy checkout reclamation and uses the same inspect/plan/apply
// authority as explicit reclamation. A terminal callback alone never proves idle.
type managedResourceCleanup struct {
	mu               sync.Mutex
	running, pending bool
	after            string
	cancel           context.CancelFunc
	done             chan struct{}
	closed           bool
}

func (b *Broker) requestManagedResourceCleanup(ctx context.Context) {
	if !b.ReclaimAuto || b.Resources == nil {
		return
	}
	state := &b.managedCleanup
	state.mu.Lock()
	state.pending = true
	state.mu.Unlock()
	b.runManagedResourceCleanup(ctx)
}

func (b *Broker) runManagedResourceCleanup(ctx context.Context) {
	if !b.ReclaimAuto || b.Resources == nil {
		return
	}
	state := &b.managedCleanup
	state.mu.Lock()
	if state.closed || state.running || !state.pending {
		state.mu.Unlock()
		return
	}
	state.running, state.pending = true, false
	after := state.after
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), reclaimDeadline)
	done := make(chan struct{})
	state.cancel, state.done = cancel, done
	state.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		defer func() { state.mu.Lock(); state.running = false; state.mu.Unlock() }()
		for {
			next := ""
			err := b.Resources.Maintenance(run, func() error { _, n, e := b.SweepTaskResources(run, after); next = n; return e })
			if err != nil {
				log.Printf("managed resource cleanup deferred: %v", err)
				return
			}
			state.mu.Lock()
			state.after = next
			after = next
			if next == "" {
				if !state.pending {
					state.mu.Unlock()
					return
				}
				state.pending = false
			}
			state.mu.Unlock()
			if run.Err() != nil {
				return
			}
		}
	}()
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
