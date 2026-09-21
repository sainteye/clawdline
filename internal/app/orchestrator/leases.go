package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Leases: one holder of a resource at a time, with a clock on the proof of
// life and not on the work (docs/design-decisions.md D06 ②).
//
// Two resources use it. `heavy_compile` is the machine's one compile slot
// (cutover A10): four swift-frontend processes once force-rebooted this Mac,
// so a second verification waits for the first instead of starting beside it.
// `landing` is one checkout's landing (D20): two roots merging into the same
// working tree at once is the collision the Swift app's landing queue was
// built for, and a lease is the part of that queue anybody ever needed.
//
// The rules are the four sentences the Swift app paid for (broker-design §4 J):
//
//   - b2f25048: a waiter that died at the head of the queue must not hold
//     everybody behind it while the lock itself is free — a waiter that stops
//     asking is passed over.
//   - 24a33139: one Ctrl-C must not turn the slot into a permanent roadblock —
//     a holder whose proof of life lapsed and whose process is positively gone
//     is replaced by the next asker.
//   - 15924b14: a clock on the work is wrong; a clock on the proof of life is
//     right — a long build that is still renewing, or whose process is still
//     there, keeps the slot however long it takes.
//   - b2a64352 and O9: one reading that failed is not a death — a process the
//     system would not answer about is `unknown`, and unknown keeps the lease.

// The two resources.
const (
	ResourceCompile = "heavy_compile"
	ResourceLanding = "landing"
	// compileKey is the compile slot's one key: the machine has one slot.
	compileKey = "machine"
)

const (
	// leaseRenewal is how long a renewal proves a holder alive (the Swift
	// lease's renewalDeadline): a holder that renews every 20 seconds misses
	// two before its proof lapses.
	leaseRenewal = 60 * time.Second
	// waiterSilence is how long a waiter may go without asking and still hold
	// its place at the head (waiterDeadline): 24 polls at five seconds.
	waiterSilence = 120 * time.Second
	// waiterForgotten is when a silent waiter's row is removed: it asked once,
	// half an hour ago, and has not asked since.
	waiterForgotten = 30 * time.Minute
	// LeaseQueueLimit is how many waiters one lease keeps (the Swift lease's
	// queueDepthLimit). Past it a new asker is refused with 429 queue_full —
	// registered as `leases.queue` in the capacity register.
	LeaseQueueLimit = 32
	// leaseRetry is how long a queued asker is told to wait before asking
	// again.
	leaseRetry       = 5
	leaseHolderLabel = 200
	leaseReasonLabel = 500
)

// LeaseRequest is one ask for a lease.
type LeaseRequest struct {
	Resource string
	// Checkout names the working tree a landing lease is for. A compile slot
	// has no key of its own.
	Checkout string
	// RequestID is the asker's own lowercase UUID and the one proof of
	// ownership: whoever holds it renews and releases, nobody else does.
	RequestID string
	Holder    string
	Reason    string
	// Session is the conversation id whose presence proves the asker alive
	// (#36: a session is named by its conversation, never by its terminal).
	Session string
	// PID is the process whose existence proves it alive; ProcessStart, when
	// given, tells that process from a later one with the same pid.
	PID          int
	ProcessStart time.Time
	Phase        string
}

// LeaseAnswer is what an ask, a renewal, a release or a cancel came to.
type LeaseAnswer struct {
	// State is granted, queued, renewed, released or cancelled.
	State    string
	Resource string
	Key      string
	LeaseID  string
	// Position is the asker's place among the waiters that are still asking,
	// one-based; zero once granted.
	Position int
	// HoldReason says why a queued asker is not the holder yet.
	HoldReason string
	RetryAfter int
	View       LeaseView
}

// LeaseView is one lease as a reader sees it.
type LeaseView struct {
	Resource string
	Key      string
	Holder   *LeaseHolder
	Queue    []LeaseWaiter
}

// LeaseHolder is the holder, with the proof it is judged by.
type LeaseHolder struct {
	LeaseID     string
	RequestID   string
	Holder      string
	Reason      string
	Session     string
	PID         int
	AcquiredAt  time.Time
	RenewedAt   time.Time
	Phase       string
	HeldSeconds int
	RenewalAge  int
	Liveness    string
	LivenessWhy string
}

// LeaseWaiter is one asker in line.
type LeaseWaiter struct {
	RequestID   string
	Holder      string
	Reason      string
	Session     string
	PID         int
	Position    int
	RequestedAt time.Time
	Waited      int
	Proving     bool
}

// proofOf is one row's liveness: what the renewal, the process and the
// session each say, reduced to alive, gone or unknown. It is read before the
// write right is taken — a kernel question and the beat's last reading, both
// local, neither a subprocess — and handed to the decision.
type proof struct {
	verdict string // "alive", "gone", "unknown"
	why     string
}

// livenessOf judges one holder. A renewal inside its window is alive and ends
// the question. Past it, the holder's own named proofs are asked: any one
// alive keeps it; all of them positively gone releases it; anything else is
// unknown and keeps it. A holder that named no proof at all chose the renewal
// as its only one, and a lapsed renewal is then its own word that it stopped.
func (b *Broker) livenessOf(r store.LeaseRow, renewed time.Time, now time.Time) proof {
	if !renewed.IsZero() && now.Sub(renewed) <= leaseRenewal {
		return proof{"alive", "proving"}
	}
	named, gone := 0, 0
	if r.PID > 0 {
		named++
		switch b.processProof(r.PID, r.ProcessStart) {
		case "alive":
			return proof{"alive", "process_running"}
		case "gone":
			gone++
		}
	}
	if r.Session != "" {
		named++
		switch b.ownerLiveness(r.Session, "") {
		case work.Live:
			return proof{"alive", "session_live"}
		case work.Gone:
			gone++
		}
	}
	switch {
	case named == 0:
		return proof{"gone", "heartbeat_lapsed"}
	case gone == named:
		return proof{"gone", "owner_gone"}
	}
	return proof{"unknown", "evidence_unknown"}
}

// processProof asks the kernel about one process. A start time that differs
// from the recorded one by more than the tolerance is a later process that
// was given the same pid: the one that asked is gone.
func (b *Broker) processProof(pid int, start time.Time) string {
	gone, known := store.ProcessGone(pid)
	if !known {
		return "unknown"
	}
	if gone {
		return "gone"
	}
	if !start.IsZero() && b.ProcessStart != nil {
		now := b.ProcessStart(pid)
		if now.IsZero() {
			return "unknown"
		}
		d := now.Sub(start)
		if d < 0 {
			d = -d
		}
		if d > 2*time.Second {
			return "gone"
		}
	}
	return "alive"
}

// leaseKey resolves the key a request is for.
func leaseKey(req LeaseRequest) (string, error) {
	switch req.Resource {
	case ResourceCompile:
		if req.Checkout != "" {
			return "", refuse(http.StatusBadRequest, "bad_lease", "The compile slot is the machine's; it takes no checkout.")
		}
		return compileKey, nil
	case ResourceLanding:
		dir := strings.TrimSpace(req.Checkout)
		if dir == "" || !filepath.IsAbs(dir) {
			return "", refuse(http.StatusBadRequest, "bad_lease", "A landing lease names the absolute path of the checkout it lands into.")
		}
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
		return filepath.Clean(dir), nil
	case "":
		return "", refuse(http.StatusBadRequest, "bad_lease", "resource is required: heavy_compile or landing.")
	}
	return "", refuseWith(http.StatusBadRequest, "unknown_resource",
		"This machine leases heavy_compile and landing only.", map[string]any{"resource": req.Resource})
}

func validLeaseRequest(req LeaseRequest) error {
	if !isLowercaseUUID(req.RequestID) {
		return refuse(http.StatusBadRequest, "bad_lease", "request_id must be one lowercase UUID; it is how the lease knows you.")
	}
	if strings.TrimSpace(req.Holder) == "" || len(req.Holder) > leaseHolderLabel {
		return refuse(http.StatusBadRequest, "bad_lease", "holder is a label of at most 200 characters, and it is required.")
	}
	if len(req.Reason) > leaseReasonLabel {
		return refuse(http.StatusBadRequest, "bad_lease", "reason is at most 500 characters.")
	}
	if req.Session != "" && !isLowercaseUUID(req.Session) {
		return sessionIsTerminal(req.Session)
	}
	if req.PID < 0 {
		return refuse(http.StatusBadRequest, "bad_lease", "pid must be positive.")
	}
	return nil
}

// sessionIsTerminal is the one refusal for a session named the other way
// (#36): a value that is not a conversation id is a terminal, a tty or a
// label, and none of them is a session here.
func sessionIsTerminal(value string) Refusal {
	return refuseWith(http.StatusConflict, "session_id_is_terminal",
		"A session is named by its conversation id (one lowercase UUID) everywhere on this daemon; that value "+
			"is not one. A terminal id is a place a session is, not the session. Resolve yours with "+
			"GET /v1/orchestrator/whoami?conversation_id=… and send the conversation id.",
		map[string]any{"value": value})
}

// Acquire asks for a lease, or asks again. It is idempotent on the request
// id: the holder asking again is a renewal, a waiter asking again keeps its
// place and proves it is still there.
func (b *Broker) Acquire(ctx context.Context, req LeaseRequest) (LeaseAnswer, error) {
	if err := validLeaseRequest(req); err != nil {
		return LeaseAnswer{}, err
	}
	key, err := leaseKey(req)
	if err != nil {
		return LeaseAnswer{}, err
	}
	now := b.now()
	proofs, err := b.leaseProofs(ctx, req.Resource, key, now)
	if err != nil {
		return LeaseAnswer{}, err
	}
	var answer LeaseAnswer
	err = b.Store.DecideLease(ctx, req.Resource, key, func(st store.LeaseState) (store.LeaseChange, error) {
		var change store.LeaseChange
		ev := func(kind string, body map[string]any) {
			body["resource"], body["key"] = req.Resource, key
			payload, _ := json.Marshal(body)
			change.Events = append(change.Events, store.Event{Kind: kind, Subject: req.Resource + ":" + key, Payload: payload})
		}
		// The holder asking again renews.
		if h := st.Holder; h != nil && h.RequestID == req.RequestID {
			next := *h
			next.RenewedAt = now
			if req.Phase != "" {
				next.Phase = req.Phase
			}
			change.SetHolder = &next
			answer = LeaseAnswer{State: "granted", LeaseID: h.LeaseID}
			return change, nil
		}
		holderFree := st.Holder == nil
		if h := st.Holder; h != nil {
			p, ok := proofs[h.RequestID]
			if !ok {
				// Read after the proofs were taken: nothing is known of it yet.
				p = proof{"unknown", "evidence_unknown"}
			}
			if p.verdict == "gone" {
				change.ClearHolder = true
				holderFree = true
				ev("lease.taken_over", map[string]any{"from": h.RequestID, "holder": h.Holder, "why": p.why,
					"held_seconds": int(now.Sub(h.AcquiredAt) / time.Second)})
			}
		}
		// The line, oldest first, with this asker in it.
		waiters := make([]store.LeaseRow, 0, len(st.Waiters)+1)
		mine := -1
		for _, w := range st.Waiters {
			if w.RequestID != req.RequestID && now.Sub(w.AskedAt) > waiterForgotten {
				change.DropWaiters = append(change.DropWaiters, w.RequestID)
				continue
			}
			if w.RequestID == req.RequestID {
				w.AskedAt = now
				mine = len(waiters)
			}
			waiters = append(waiters, w)
		}
		if mine < 0 {
			if line := b.leaseLine(); len(waiters) >= line {
				return change, refuseWith(http.StatusTooManyRequests, "queue_full",
					fmt.Sprintf("%d askers are already waiting for this lease; nothing was queued.", line),
					map[string]any{"retry_after": 30, "limit": line})
			}
			waiters = append(waiters, store.LeaseRow{
				Resource: req.Resource, Key: key, RequestID: req.RequestID, Holder: req.Holder, Reason: req.Reason,
				Session: req.Session, PID: req.PID, ProcessStart: req.ProcessStart, RequestedAt: now, AskedAt: now,
			})
			mine = len(waiters) - 1
		}
		// The head is the oldest waiter still asking. A waiter that stopped
		// asking is passed over and keeps its row until it is forgotten.
		position := 1
		for i := 0; i < mine; i++ {
			if now.Sub(waiters[i].AskedAt) <= waiterSilence {
				position++
			}
		}
		if holderFree && position == 1 {
			w := waiters[mine]
			lease := uuidLike()
			change.SetHolder = &store.LeaseRow{
				Resource: req.Resource, Key: key, RequestID: w.RequestID, LeaseID: lease, Holder: w.Holder,
				Reason: w.Reason, Session: w.Session, PID: w.PID, ProcessStart: w.ProcessStart,
				RequestedAt: w.RequestedAt, AcquiredAt: now, RenewedAt: now, Phase: req.Phase,
			}
			change.DropWaiters = append(change.DropWaiters, w.RequestID)
			ev("lease.granted", map[string]any{"request": w.RequestID, "holder": w.Holder,
				"waited_seconds": int(now.Sub(w.RequestedAt) / time.Second)})
			answer = LeaseAnswer{State: "granted", LeaseID: lease}
			return change, nil
		}
		change.PutWaiters = append(change.PutWaiters, waiters[mine])
		why := "queued_behind_others"
		if position == 1 {
			why = "holder_proving"
			if st.Holder != nil {
				if p, ok := proofs[st.Holder.RequestID]; ok && p.verdict == "unknown" {
					why = "evidence_unknown"
				}
			}
		}
		answer = LeaseAnswer{State: "queued", Position: position, HoldReason: why, RetryAfter: leaseRetry}
		return change, nil
	})
	if err != nil {
		return LeaseAnswer{}, leaseStoreError(err)
	}
	answer.Resource, answer.Key = req.Resource, key
	answer.View, _ = b.leaseView(ctx, req.Resource, key)
	return answer, nil
}

// leaseProofs reads the liveness of everybody holding or waiting for one
// lease, outside the write right.
func (b *Broker) leaseProofs(ctx context.Context, resource, key string, now time.Time) (map[string]proof, error) {
	st, err := b.Store.Lease(ctx, resource, key)
	if err != nil {
		return nil, leaseStoreError(err)
	}
	out := map[string]proof{}
	if h := st.Holder; h != nil {
		out[h.RequestID] = b.livenessOf(*h, h.RenewedAt, now)
	}
	return out, nil
}

// LeaseOwner names who is renewing, releasing or cancelling: the request id
// the lease was asked with.
type LeaseOwner struct {
	Resource  string
	Checkout  string
	RequestID string
	Phase     string
}

// Renew proves the holder alive.
func (b *Broker) Renew(ctx context.Context, o LeaseOwner) (LeaseAnswer, error) {
	return b.ownerMove(ctx, o, "renewed")
}

// Release gives the lease back. A release when nothing holds it is a release
// that already happened; one by somebody who is not the holder is refused.
func (b *Broker) Release(ctx context.Context, o LeaseOwner) (LeaseAnswer, error) {
	return b.ownerMove(ctx, o, "released")
}

// Cancel takes an asker out of the line.
func (b *Broker) Cancel(ctx context.Context, o LeaseOwner) (LeaseAnswer, error) {
	return b.ownerMove(ctx, o, "cancelled")
}

func (b *Broker) ownerMove(ctx context.Context, o LeaseOwner, move string) (LeaseAnswer, error) {
	if !isLowercaseUUID(o.RequestID) {
		return LeaseAnswer{}, refuse(http.StatusBadRequest, "bad_lease", "request_id must be the lowercase UUID the lease was asked with.")
	}
	key, err := leaseKey(LeaseRequest{Resource: o.Resource, Checkout: o.Checkout})
	if err != nil {
		return LeaseAnswer{}, err
	}
	now := b.now()
	answer := LeaseAnswer{State: move, Resource: o.Resource, Key: key}
	err = b.Store.DecideLease(ctx, o.Resource, key, func(st store.LeaseState) (store.LeaseChange, error) {
		var change store.LeaseChange
		ev := func(kind string, body map[string]any) {
			body["resource"], body["key"], body["request"] = o.Resource, key, o.RequestID
			payload, _ := json.Marshal(body)
			change.Events = append(change.Events, store.Event{Kind: kind, Subject: o.Resource + ":" + key, Payload: payload})
		}
		h := st.Holder
		switch move {
		case "renewed":
			if h == nil || h.RequestID != o.RequestID {
				return change, refuseWith(http.StatusConflict, "lease_lost",
					"This request does not hold the lease any more; ask for it again before going on.",
					map[string]any{"resource": o.Resource, "key": key})
			}
			next := *h
			next.RenewedAt = now
			if o.Phase != "" {
				next.Phase = o.Phase
			}
			change.SetHolder = &next
			answer.LeaseID = h.LeaseID
		case "released":
			if h == nil {
				return change, nil
			}
			if h.RequestID != o.RequestID {
				for _, w := range st.Waiters {
					if w.RequestID == o.RequestID {
						// A waiter giving up before it was granted.
						change.DropWaiters = []string{w.RequestID}
						ev("lease.cancelled", map[string]any{})
						return change, nil
					}
				}
				return change, refuse(http.StatusForbidden, "not_holder", "Another request holds this lease.")
			}
			change.ClearHolder = true
			answer.LeaseID = h.LeaseID
			ev("lease.released", map[string]any{"held_seconds": int(now.Sub(h.AcquiredAt) / time.Second)})
		case "cancelled":
			for _, w := range st.Waiters {
				if w.RequestID == o.RequestID {
					change.DropWaiters = []string{w.RequestID}
					ev("lease.cancelled", map[string]any{})
					return change, nil
				}
			}
			return change, refuse(http.StatusNotFound, "not_queued", "That request is not waiting for this lease.")
		}
		return change, nil
	})
	if err != nil {
		return LeaseAnswer{}, leaseStoreError(err)
	}
	answer.View, _ = b.leaseView(ctx, o.Resource, key)
	return answer, nil
}

// Leases is every lease with a holder or a waiter, as a reader sees it.
func (b *Broker) Leases(ctx context.Context) ([]LeaseView, error) {
	all, err := b.Store.Leases(ctx)
	if err != nil {
		return nil, leaseStoreError(err)
	}
	out := make([]LeaseView, 0, len(all))
	for _, st := range all {
		out = append(out, b.viewOf(st, b.now()))
	}
	return out, nil
}

func (b *Broker) leaseView(ctx context.Context, resource, key string) (LeaseView, error) {
	st, err := b.Store.Lease(ctx, resource, key)
	if err != nil {
		return LeaseView{Resource: resource, Key: key}, err
	}
	return b.viewOf(st, b.now()), nil
}

func (b *Broker) viewOf(st store.LeaseState, now time.Time) LeaseView {
	v := LeaseView{Resource: st.Resource, Key: st.Key, Queue: []LeaseWaiter{}}
	if h := st.Holder; h != nil {
		p := b.livenessOf(*h, h.RenewedAt, now)
		v.Holder = &LeaseHolder{
			LeaseID: h.LeaseID, RequestID: h.RequestID, Holder: h.Holder, Reason: h.Reason, Session: h.Session,
			PID: h.PID, AcquiredAt: h.AcquiredAt, RenewedAt: h.RenewedAt, Phase: h.Phase,
			HeldSeconds: int(now.Sub(h.AcquiredAt) / time.Second), RenewalAge: int(now.Sub(h.RenewedAt) / time.Second),
			Liveness: p.verdict, LivenessWhy: p.why,
		}
	}
	position := 0
	for _, w := range st.Waiters {
		proving := now.Sub(w.AskedAt) <= waiterSilence
		row := LeaseWaiter{RequestID: w.RequestID, Holder: w.Holder, Reason: w.Reason, Session: w.Session,
			PID: w.PID, RequestedAt: w.RequestedAt, Waited: int(now.Sub(w.RequestedAt) / time.Second), Proving: proving}
		if proving {
			position++
			row.Position = position
		}
		v.Queue = append(v.Queue, row)
	}
	return v
}

// leaseStoreError is a store failure said as a refusal.
func leaseStoreError(err error) error {
	var ref Refusal
	if errors.As(err, &ref) {
		return ref
	}
	if errors.Is(err, store.ErrBusy) {
		return refuseWith(http.StatusServiceUnavailable, "orchestrator_store_busy",
			"Another writer held the broker's store longer than this one waits; nothing was changed. Retry.",
			map[string]any{"retry_after": 1})
	}
	return refuseWith(http.StatusServiceUnavailable, "orchestrator_store_unavailable",
		"The broker's store could not be read; nothing was changed.", map[string]any{"cause": err.Error()})
}

// NewUUID mints a lowercase UUID: a lease's id, a role's id.
func NewUUID() string { return uuidLike() }

// leaseLine is the lease line's ceiling as the register resolved it.
func (b *Broker) leaseLine() int {
	if b.LeaseLine > 0 && b.LeaseLine < LeaseQueueLimit {
		return b.LeaseLine
	}
	return LeaseQueueLimit
}

// openWaits is the open waits' ceiling as the register resolved it.
func (b *Broker) openWaits() int {
	if b.OpenWaits > 0 && b.OpenWaits < WaitsOpenLimit {
		return b.OpenWaits
	}
	return WaitsOpenLimit
}
