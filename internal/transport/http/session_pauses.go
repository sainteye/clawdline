package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
)

// Pause commands use the machine token; the read is available to the paired
// console so a person can distinguish delivery from a receiver's safe point.
func (s *Server) sessionPausesRoute(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(routePath(r), "/")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if p == "/v1/orchestrator/pauses" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		rows, err := s.store.Pauses(ctx)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := make([]orchestrator.PauseView, 0, len(rows))
		for _, row := range rows {
			view := orchestrator.WirePause(row)
			if effects, err := s.store.Effects(ctx, orchestrator.EffectPauseDelivery, row.ID); err == nil {
				for i := len(effects) - 1; i >= 0; i-- {
					effect := effects[i]
					if effect.State == store.EffectUnknown {
						if !row.WakeRequestedAt.IsZero() && row.WakeDeliveredAt.IsZero() {
							view.WakeError = "delivery_outcome_unknown"
						} else if row.DeliveredAt.IsZero() {
							view.DeliveryError = "delivery_outcome_unknown"
						}
						break
					}
					if effect.State == store.EffectDone {
						break
					}
				}
			} else if row.DeliveredAt.IsZero() {
				view.DeliveryError = "delivery_receipt_unreadable"
			}
			out = append(out, view)
		}
		writeJSON(w, map[string]any{"at": time.Now().Unix(), "source": "broker", "pauses": out})
		return
	}
	if r.Method != http.MethodPost {
		writeNoSuchRoute(w, r)
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Session pause commands need the orchestrator token.")
		return
	}
	if p == "/v1/orchestrator/pauses" {
		var body struct {
			RequestID     string `json:"request_id"`
			Requester     string `json:"requester_session_id"`
			Target        string `json:"target_session_id"`
			Reason        string `json:"reason"`
			WakeCondition string `json:"wake_condition"`
		}
		if !decodeClosed(w, r, &body, "request_id", "requester_session_id", "target_session_id", "reason", "wake_condition") {
			return
		}
		if !s.isBoundCoordinator(ctx, body.Requester) {
			writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusForbidden, Code: "not_coordinator", Message: "Only the registered online Clawdfather may request a Session pause."})
			return
		}
		row, err := s.broker.RequestPause(ctx, orchestrator.PauseRequest{ID: body.RequestID, Requester: body.Requester,
			Target: body.Target, Reason: body.Reason, WakeCondition: body.WakeCondition})
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, orchestrator.WirePause(row))
		return
	}
	rest := strings.TrimPrefix(p, "/v1/orchestrator/pauses/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || id == "" {
		writeNoSuchRoute(w, r)
		return
	}
	id = decodeSegment(id)
	var body struct {
		Session string `json:"session_id"`
	}
	if !decodeClosed(w, r, &body, "session_id") {
		return
	}
	var err error
	var row store.PauseRow
	switch action {
	case "observed", "safe", "resumed":
		if _, err = s.broker.LiveRootSession(ctx, body.Session); err != nil {
			writeBrokerError(w, err)
			return
		}
		row, err = s.broker.PauseReceipt(ctx, id, body.Session, action)
	case "wake", "retry":
		if !s.isBoundCoordinator(ctx, body.Session) {
			writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusForbidden, Code: "not_coordinator", Message: "Only the registered online Clawdfather may wake or retry this request."})
			return
		}
		if action == "wake" {
			row, err = s.broker.WakePause(ctx, id, body.Session)
		} else {
			row, err = s.broker.RetryPauseDelivery(ctx, id)
		}
	default:
		writeNoSuchRoute(w, r)
		return
	}
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	// The app methods return store.PauseRow; keep its wire representation in one place.
	writeJSON(w, orchestrator.WirePause(row))
}

func (s *Server) isBoundCoordinator(ctx context.Context, conversation string) bool {
	if conversation == "" {
		return false
	}
	c := s.coordinator()
	c.Read = s.freshReading
	st, err := c.State(ctx)
	return err == nil && st.Record != nil && st.Liveness == coordinator.Online && st.Record.ConversationID == conversation
}
