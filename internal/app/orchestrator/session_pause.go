package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/lane"
)

// A pause is a request to stop at a safe turn boundary. Only the addressed
// Session can acknowledge observing it and reaching that boundary. A typed
// command alone never makes a Session paused.
type PauseRequest struct {
	ID, Requester, Target, Reason, WakeCondition string
}

type pauseEffect struct {
	ID, Target, Kind string
}

func pauseWire(p store.PauseRow, kind string) string {
	text := "Pause request " + p.ID + " from Clawdfather: " + p.Reason + ". Finish any important command, then run `clawdline coordination observed " + p.ID + "` and `clawdline coordination safe " + p.ID + "`; end this model turn. Wake condition: " + p.WakeCondition + ". Do not run resource work until the lease is granted or the named wait is released."
	if kind == "wake" {
		text = "Resume request " + p.ID + ": the wake condition is ready. Run `clawdline coordination resumed " + p.ID + "`, then acquire the required lease or confirm the wait is released before work."
	}
	encoded, _ := json.Marshal(map[string]any{"protocol": "clawdline.coordination", "version": 1,
		"kind": kind, "request_id": p.ID, "body": text})
	return "<clawdline-notice>" + string(encoded) + "</clawdline-notice>"
}

func pauseDelivery(p store.PauseRow, kind string) store.Effect {
	body, _ := json.Marshal(pauseEffect{ID: p.ID, Target: p.Target, Kind: kind})
	return store.Effect{Kind: EffectPauseDelivery, Subject: p.ID, Payload: body}
}

func (b *Broker) RequestPause(ctx context.Context, req PauseRequest) (store.PauseRow, error) {
	if !isLowercaseUUID(req.ID) || !isLowercaseUUID(req.Requester) || !isLowercaseUUID(req.Target) ||
		req.Requester == req.Target || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > leaseReasonLabel ||
		strings.TrimSpace(req.WakeCondition) == "" || len(req.WakeCondition) > leaseReasonLabel {
		return store.PauseRow{}, refuse(http.StatusBadRequest, "bad_pause", "Name one request id, requester and target conversation id, reason and wake condition.")
	}
	now := b.now()
	row, ids, err := b.Store.DecidePause(ctx, req.Target, func(old *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		if old != nil {
			if old.ID == req.ID {
				if old.Requester != req.Requester || old.Reason != req.Reason || old.WakeCondition != req.WakeCondition {
					return nil, nil, refuse(http.StatusConflict, "pause_request_conflict", "That request id already names different pause details.")
				}
				return nil, nil, nil
			}
			if old.ResumedAt.IsZero() {
				return nil, nil, refuse(http.StatusConflict, "session_pause_active", "That Session already has a pause request; inspect it before requesting another.")
			}
		}
		p := store.PauseRow{Target: req.Target, ID: req.ID, Requester: req.Requester,
			Reason: req.Reason, WakeCondition: req.WakeCondition, AcceptedAt: now}
		return &p, []store.Effect{pauseDelivery(p, "pause")}, nil
	})
	if err != nil {
		return store.PauseRow{}, leaseStoreError(err)
	}
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state != store.EffectDone {
			break
		}
	}
	return b.Store.Pause(ctx, row.ID)
}

func (b *Broker) PauseReceipt(ctx context.Context, id, target, stage string) (store.PauseRow, error) {
	if !isLowercaseUUID(id) || !isLowercaseUUID(target) {
		return store.PauseRow{}, refuse(http.StatusBadRequest, "bad_pause", "A pause receipt names its request and receiver conversation ids.")
	}
	row, _, err := b.Store.DecidePause(ctx, target, func(old *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		if old == nil || old.ID != id {
			return nil, nil, refuse(http.StatusNotFound, "pause_not_found", "No current pause has that id for this Session.")
		}
		p := *old
		switch stage {
		case "observed":
			if p.DeliveredAt.IsZero() {
				return nil, nil, refuse(http.StatusConflict, "pause_not_delivered", "The pause request has not been delivered.")
			}
			if !p.ObservedAt.IsZero() {
				return nil, nil, nil
			}
			p.ObservedAt = b.now()
		case "safe":
			if p.ObservedAt.IsZero() {
				return nil, nil, refuse(http.StatusConflict, "pause_not_observed", "A safe point follows the receiver's observation receipt.")
			}
			if !p.SafeAt.IsZero() {
				return nil, nil, nil
			}
			p.SafeAt = b.now()
		case "resumed":
			if p.WakeDeliveredAt.IsZero() {
				return nil, nil, refuse(http.StatusConflict, "wake_not_delivered", "The wake request has not been delivered.")
			}
			if !p.ResumedAt.IsZero() {
				return nil, nil, nil
			}
			p.ResumedAt = b.now()
		default:
			return nil, nil, refuse(http.StatusBadRequest, "bad_pause_stage", "Use observed, safe or resumed.")
		}
		return &p, nil, nil
	})
	if err != nil {
		return store.PauseRow{}, leaseStoreError(err)
	}
	return row, nil
}

func (b *Broker) WakePause(ctx context.Context, id, requester string) (store.PauseRow, error) {
	if !isLowercaseUUID(id) || !isLowercaseUUID(requester) {
		return store.PauseRow{}, refuse(http.StatusBadRequest, "bad_pause", "A wake names its request and requester conversation ids.")
	}
	old, err := b.Store.Pause(ctx, id)
	if err != nil {
		return store.PauseRow{}, refuse(http.StatusNotFound, "pause_not_found", "No current pause has that id.")
	}
	if !old.WakeRequestedAt.IsZero() || !old.ResumedAt.IsZero() {
		return old, nil
	}
	if err := b.wakeReady(ctx, old); err != nil {
		return store.PauseRow{}, err
	}
	row, ids, err := b.Store.DecidePause(ctx, old.Target, func(current *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		if current == nil || current.ID != id {
			return nil, nil, refuse(http.StatusConflict, "pause_changed", "The pause changed before the wake request.")
		}
		// HTTP has verified the currently bound Clawdfather. A new binding
		// may wake a request left by its offline predecessor.
		if current.SafeAt.IsZero() {
			return nil, nil, refuse(http.StatusConflict, "not_at_safe_point", "The Session has not acknowledged a safe point.")
		}
		if !current.ResumedAt.IsZero() || !current.WakeRequestedAt.IsZero() {
			return nil, nil, nil
		}
		p := *current
		p.WakeRequestedAt = b.now()
		return &p, []store.Effect{pauseDelivery(p, "wake")}, nil
	})
	if err != nil {
		return store.PauseRow{}, leaseStoreError(err)
	}
	b.runRecorded(ctx, ids)
	return b.Store.Pause(ctx, row.ID)
}

// wakeReady checks a typed wait or lease condition before sending a wake.
// Availability is only a hint: the receiver still acquires the lease before
// doing work, since another asker can race this read.
func (b *Broker) wakeReady(ctx context.Context, p store.PauseRow) error {
	condition := strings.TrimSpace(p.WakeCondition)
	if strings.HasPrefix(condition, "wait:") {
		id := strings.TrimSpace(strings.TrimPrefix(condition, "wait:"))
		if !isLowercaseUUID(id) {
			return refuse(http.StatusBadRequest, "bad_wake_condition", "A wait condition is wait:<wait id>.")
		}
		w, err := b.Store.WaitByID(ctx, id)
		if err != nil {
			return refuse(http.StatusConflict, "wait_unconfirmed", "The named wait cannot be confirmed released.")
		}
		for _, waiter := range w.Waiters {
			if waiter.Waiter == p.Target {
				if !waiter.ReleaseDeliveredAt.IsZero() {
					return nil
				}
				return refuse(http.StatusConflict, "wait_not_released", "The wait's release has not reached this Session.")
			}
		}
		return refuse(http.StatusConflict, "wait_unconfirmed", "This Session is not a waiter on the named wait.")
	}
	if strings.HasPrefix(condition, "lease:") {
		resource := strings.TrimPrefix(condition, "lease:")
		key := compileKey
		if strings.HasPrefix(resource, ResourceLanding+":") {
			key = strings.TrimPrefix(resource, ResourceLanding+":")
			resource = ResourceLanding
		}
		if resource != ResourceCompile && resource != ResourceLanding && resource != ResourceRestart ||
			resource == ResourceLanding && !strings.HasPrefix(key, "/") {
			return refuse(http.StatusBadRequest, "bad_wake_condition", "A lease condition names heavy_compile, landing:<absolute-checkout> or daemon_restart.")
		}
		views, err := b.Leases(ctx)
		if err != nil {
			return err
		}
		for _, v := range views {
			if v.Holder != nil && v.Holder.Session != p.Target &&
				(resource == ResourceRestart && (v.Resource == ResourceCompile || v.Resource == ResourceLanding) ||
					resource != ResourceRestart && v.Resource == ResourceRestart) {
				return refuse(http.StatusConflict, "conflicting_resource_held", "A conflicting exclusive operation is still running.")
			}
			if v.Resource != resource || v.Key != key {
				continue
			}
			if v.Holder != nil && v.Holder.Session != p.Target {
				return refuse(http.StatusConflict, "lease_held", "Another Session still holds the required lease.")
			}
			for _, q := range v.Queue {
				if q.Proving {
					if q.Session != p.Target {
						return refuse(http.StatusConflict, "lease_queued_ahead", "Another Session is ahead in the lease queue.")
					}
					break
				}
			}
		}
	}
	return nil
}

func (b *Broker) RetryPauseDelivery(ctx context.Context, id string) (store.PauseRow, error) {
	old, err := b.Store.Pause(ctx, id)
	if err != nil {
		return store.PauseRow{}, refuse(http.StatusNotFound, "pause_not_found", "No current pause has that id.")
	}
	row, ids, err := b.Store.DecidePause(ctx, old.Target, func(current *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		if current == nil || current.ID != id {
			return nil, nil, refuse(http.StatusConflict, "pause_changed", "The pause changed before retry.")
		}
		p := *current
		kind := "pause"
		if !p.WakeRequestedAt.IsZero() {
			kind = "wake"
			if !p.WakeDeliveredAt.IsZero() {
				return nil, nil, nil
			}
			p.WakeError = ""
		} else {
			if !p.DeliveredAt.IsZero() {
				return nil, nil, nil
			}
			p.DeliveryError = ""
		}
		return &p, []store.Effect{pauseDelivery(p, kind)}, nil
	})
	if err != nil {
		return store.PauseRow{}, leaseStoreError(err)
	}
	b.runRecorded(ctx, ids)
	return b.Store.Pause(ctx, row.ID)
}

func runPauseDelivery(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var d pauseEffect
	if json.Unmarshal(e.Payload, &d) != nil {
		return effectResult{state: store.EffectFailed, outcome: "bad_pause_effect"}
	}
	p, err := b.Store.Pause(ctx, d.ID)
	if err != nil || p.Target != d.Target {
		return effectResult{state: store.EffectFailed, outcome: "pause_not_found"}
	}
	if d.Kind == "pause" && !p.DeliveredAt.IsZero() || d.Kind == "wake" && !p.WakeDeliveredAt.IsZero() {
		return effectResult{state: store.EffectDone, outcome: "already_delivered", stage: StageDelivered}
	}
	outcome := ""
	target, err := b.terminalFor(ctx, p.Target, "")
	if err != nil {
		outcome = "session_unreachable"
	} else if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		outcome = "session_menu_open"
	} else if err = b.typeLine(ctx, target.ID, pauseWire(p, d.Kind)); err != nil {
		var busy lane.Busy
		if errors.As(err, &busy) {
			outcome = "session_busy"
		} else {
			outcome = "delivery_failed: " + err.Error()
		}
	}
	if outcome != "" {
		_, _, _ = b.Store.DecidePause(ctx, p.Target, func(old *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
			if old == nil || old.ID != p.ID {
				return nil, nil, nil
			}
			next := *old
			if d.Kind == "wake" {
				next.WakeError = outcome
			} else {
				next.DeliveryError = outcome
			}
			return &next, nil, nil
		})
		return effectResult{state: store.EffectFailed, outcome: outcome}
	}
	_, _, err = b.Store.DecidePause(ctx, p.Target, func(old *store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		if old == nil || old.ID != p.ID {
			return nil, nil, nil
		}
		next := *old
		if d.Kind == "wake" {
			next.WakeDeliveredAt = b.now()
			next.WakeError = ""
		} else {
			next.DeliveredAt = b.now()
			next.DeliveryError = ""
		}
		return &next, nil, nil
	})
	if err != nil {
		return effectResult{state: store.EffectUnknown, outcome: "typed_but_receipt_unavailable"}
	}
	return effectResult{state: store.EffectDone, outcome: "typed", stage: StageDelivered}
}

func pauseState(p store.PauseRow) string {
	switch {
	case !p.ResumedAt.IsZero():
		return "resumed"
	case !p.WakeRequestedAt.IsZero():
		return "resuming"
	case !p.SafeAt.IsZero():
		return "safe_point"
	case !p.ObservedAt.IsZero():
		return "observed"
	case !p.DeliveredAt.IsZero():
		return "delivered"
	default:
		return "accepted"
	}
}

type PauseView struct {
	ID              string `json:"id"`
	Target          string `json:"target_session_id"`
	Requester       string `json:"requester_session_id"`
	Reason          string `json:"reason"`
	WakeCondition   string `json:"wake_condition"`
	State           string `json:"state"`
	AcceptedAt      int64  `json:"accepted_at"`
	DeliveredAt     int64  `json:"delivered_at,omitempty"`
	ObservedAt      int64  `json:"observed_at,omitempty"`
	SafeAt          int64  `json:"safe_at,omitempty"`
	WakeRequestedAt int64  `json:"wake_requested_at,omitempty"`
	WakeDeliveredAt int64  `json:"wake_delivered_at,omitempty"`
	ResumedAt       int64  `json:"resumed_at,omitempty"`
	DeliveryError   string `json:"delivery_error,omitempty"`
	WakeError       string `json:"wake_error,omitempty"`
}

func WirePause(p store.PauseRow) PauseView {
	toUnix := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	return PauseView{ID: p.ID, Target: p.Target, Requester: p.Requester,
		Reason: p.Reason, WakeCondition: p.WakeCondition, State: pauseState(p),
		AcceptedAt: toUnix(p.AcceptedAt), DeliveredAt: toUnix(p.DeliveredAt),
		ObservedAt: toUnix(p.ObservedAt), SafeAt: toUnix(p.SafeAt),
		WakeRequestedAt: toUnix(p.WakeRequestedAt), WakeDeliveredAt: toUnix(p.WakeDeliveredAt),
		ResumedAt: toUnix(p.ResumedAt), DeliveryError: p.DeliveryError, WakeError: p.WakeError}
}
