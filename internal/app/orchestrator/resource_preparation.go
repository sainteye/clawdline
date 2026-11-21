package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// There is one bounded preparation worker per broker, not one goroutine per
// queue poll. The existing durable lease waiter is its intent and queue bound.
// On restart a poll resumes preparation; an old ready result cannot authorize
// the new service incarnation. No preparation grants or starts compilation.
type resourcePreparationState struct {
	mu       sync.Mutex
	moveMu   sync.Mutex
	running  bool
	request  string
	granting string
	cancel   context.CancelFunc
	done     chan struct{}
	closed   bool
	// receipts counts the preparation receipts this broker has written. A
	// poll notes it before reading receipts; a different count when it goes
	// to start a preparation means one finished in between, and the next
	// poll reads that receipt instead of measuring again.
	receipts uint64
	// beforeRestart, set only by tests, runs after a poll has read the
	// receipts and decided to start a preparation, before it does.
	beforeRestart func()
}
type resourcePreparationResult struct {
	RequestedAt int64                         `json:"requested_at"`
	Identity    string                        `json:"identity"`
	At          int64                         `json:"at"`
	Ticket      taskresources.AdmissionTicket `json:"ticket"`
	Refusal     *Refusal                      `json:"refusal,omitempty"`
}

func preparationIdentity(w store.LeaseRow) string {
	return evidenceDigest(struct {
		Session, Callback string
		PID               int
		Start             int64
	}{w.Session, w.CallbackTaskID, w.PID, w.ProcessStart.Unix()})
}

func (b *Broker) preparedResourceLease(ctx context.Context, req LeaseRequest, w store.LeaseRow) (bool, error) {
	b.resourcePreparation.mu.Lock()
	seen := b.resourcePreparation.receipts
	b.resourcePreparation.mu.Unlock()
	cancelled, err := b.Store.LatestEvents(ctx, "resource.preparation.cancelled", []string{req.RequestID})
	if err != nil {
		return false, storeError(err)
	}
	if _, ok := cancelled[req.RequestID]; ok {
		return false, refuse(409, "lease_cancelled", "This compile request was cancelled; use a new request to queue again.")
	}
	completed, err := b.Store.LatestEvents(ctx, "resource.preparation.completed", []string{req.RequestID})
	if err != nil {
		return false, storeError(err)
	}
	if _, ok := completed[req.RequestID]; ok {
		return false, refuse(409, "lease_completed", "This compile lease was released; use a new request to queue again.")
	}
	events, err := b.Store.LatestEvents(ctx, "resource.preparation.finished", []string{req.RequestID})
	if err != nil {
		return false, storeError(err)
	}
	if e, ok := events[req.RequestID]; ok {
		var r resourcePreparationResult
		if json.Unmarshal(e.Payload, &r) != nil {
			return false, refuse(507, "resource_receipt_unknown", "Managed resource service is unavailable.")
		}
		if r.Identity == preparationIdentity(w) {
			if r.Refusal != nil {
				return false, *r.Refusal
			}
		}
		if r.RequestedAt == w.RequestedAt.Unix() && r.Identity == preparationIdentity(w) {
			state := &b.resourcePreparation
			state.mu.Lock()
			if state.granting != "" {
				state.mu.Unlock()
				return false, nil
			}
			age := b.now().Unix() - r.At
			ready := age >= 0 && age <= taskresource.PreparationReadySecondsLimit && b.Resources.ConsumeAdmission(r.Ticket)
			if ready {
				state.granting = req.RequestID
			}
			state.mu.Unlock()
			if ready {
				return true, nil
			}
		}
	}
	if hook := b.resourcePreparation.beforeRestart; hook != nil {
		hook()
	}
	b.startResourcePreparation(ctx, w, seen)
	return false, nil
}

// startResourcePreparation starts the one preparation for w, unless one is
// running, a grant is in progress, or a receipt was written after the poll
// that asks noted seen.
func (b *Broker) startResourcePreparation(ctx context.Context, w store.LeaseRow, seen uint64) {
	state := &b.resourcePreparation
	state.mu.Lock()
	if state.closed || state.running || state.granting != "" || state.receipts != seen {
		state.mu.Unlock()
		return
	}
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), reclaimDeadline)
	run = taskresources.WithCallback(taskresources.WithCaller(run, "heavy.acquire", w.RequestID), w.CallbackTaskID)
	finished := make(chan struct{})
	state.running, state.request, state.cancel, state.done = true, w.RequestID, cancel, finished
	state.mu.Unlock()
	go func() {
		defer close(finished)
		go b.watchResourcePreparation(run, cancel, w, finished)
		defer func() {
			cancel()
			state.mu.Lock()
			state.running = false
			state.request = ""
			state.cancel = nil
			state.mu.Unlock()
		}()
		result := resourcePreparationResult{RequestedAt: w.RequestedAt.Unix(), Identity: preparationIdentity(w)}
		if err := b.AdmitHeavyResources(run); err != nil {
			ref, ok := err.(Refusal)
			if !ok {
				ref = refuse(507, "resource_preparation_failed", "Managed resource service is unavailable.")
			}
			result.Refusal = &ref
		} else {
			result.Ticket = b.Resources.AdmissionTicket()
		}
		result.At = b.now().Unix()
		rec, done := recordContext(run)
		defer done()
		// A cancelled or replaced waiter must not receive a late ready result.
		st, err := b.Store.Lease(rec, ResourceCompile, compileKey)
		if err != nil {
			return
		}
		present := false
		for _, current := range st.Waiters {
			if run.Err() == nil && current.RequestID == w.RequestID && current.RequestedAt.Unix() == w.RequestedAt.Unix() && preparationIdentity(current) == result.Identity && b.now().Sub(current.AskedAt) <= waiterSilence {
				present = true
				break
			}
		}
		if !present {
			return
		}
		body, _ := json.Marshal(result)
		if err := b.Store.Append(rec, store.Event{Kind: "resource.preparation.finished", Subject: w.RequestID, Payload: body}); err != nil {
			log.Printf("resource preparation receipt unavailable: %v", err)
			return
		}
		// Counted before running is cleared: a poll that read the receipts
		// before this one existed sees the count move and does not restart.
		state.mu.Lock()
		state.receipts++
		state.mu.Unlock()
	}()
}

// An abandoned poll must not occupy the one preparation worker for the whole
// reclamation deadline. Silence uses the existing lease-waiter liveness rule;
// it is cancellation of preparation, never evidence permitting deletion.
func (b *Broker) watchResourcePreparation(ctx context.Context, cancel context.CancelFunc, w store.LeaseRow, done <-chan struct{}) {
	tick := time.NewTicker(time.Duration(leaseRetry) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-tick.C:
			st, err := b.Store.Lease(ctx, ResourceCompile, compileKey)
			present := false
			for _, current := range st.Waiters {
				if current.RequestID == w.RequestID && current.RequestedAt.Equal(w.RequestedAt) && preparationIdentity(current) == preparationIdentity(w) && b.now().Sub(current.AskedAt) <= waiterSilence {
					present = true
					break
				}
			}
			if err != nil || !present {
				cancel()
				return
			}
		}
	}
}

func (b *Broker) cancelResourcePreparation(id string) {
	state := &b.resourcePreparation
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.running && state.request == id && state.cancel != nil {
		state.cancel()
	}
}

func (b *Broker) finishResourceGrant(id string) {
	state := &b.resourcePreparation
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.granting == id {
		state.granting = ""
	}
}
