package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// The rest of the coordination plane's routes (design-decisions §6 W5): file
// waits, leases, the completion ledger's manual path, and detached tasks.
// Every change here needs the orchestrator token (gate.go writePolicy); the
// reads a phone has a use for — the waits and the leases — are read-level.

// --- file waits ---------------------------------------------------------------

func (s *Server) waitsRoute(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(routePath(r), "/")
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	switch {
	case p == "/v1/orchestrator/waits" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		rows, err := s.broker.Waits(ctx)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.WaitList{Waits: make([]contract.Wait, 0, len(rows)), At: time.Now().Unix()}
		for _, row := range rows {
			out.Waits = append(out.Waits, wireWait(row))
		}
		writeJSON(w, out)
	case p == "/v1/orchestrator/waits" && r.Method == http.MethodPost:
		if !machineAuthed(r) {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Registering a wait needs the orchestrator token.")
			return
		}
		var body contract.WaitRequest
		if !decodeClosed(w, r, &body, "repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition") {
			return
		}
		out, err := s.broker.RegisterWait(ctx, orchestrator.WaitRequest{
			Repository: body.Repository, Paths: body.Paths, Owner: strings.TrimSpace(body.OwnerSessionID),
			Waiter: strings.TrimSpace(body.WaiterSessionID), Reason: body.Reason, ReleaseCondition: body.ReleaseCondition,
		})
		if err != nil {
			if ref, ok := err.(orchestrator.Refusal); ok && out.Wait.ID != "" {
				if ref.Extra == nil {
					ref.Extra = map[string]any{}
				}
				ref.Extra["wait"] = wireWait(out.Wait)
				writeBrokerRefusal(w, ref)
				return
			}
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.WaitResult{OK: true, Deduplicated: out.Deduplicated, Wait: wireWait(out.Wait)})
	case strings.HasPrefix(p, "/v1/orchestrator/waits/") && r.Method == http.MethodPost:
		if !machineAuthed(r) {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Moving a wait needs the orchestrator token.")
			return
		}
		rest := strings.TrimPrefix(p, "/v1/orchestrator/waits/")
		id, action, _ := strings.Cut(rest, "/")
		id = decodeSegment(id)
		switch action {
		case "release":
			var body contract.WaitReleaseRequest
			if !decodeClosed(w, r, &body, "owner_session_id", "commit", "note") {
				return
			}
			n, err := s.broker.ReleaseWait(ctx, id, strings.TrimSpace(body.OwnerSessionID), body.Commit, body.Note)
			if err != nil {
				writeBrokerError(w, err)
				return
			}
			writeJSON(w, contract.WaitReleaseResult{OK: true, ID: id, Released: int64(n)})
		case "cancel":
			var body contract.WaitCancelRequest
			if !decodeClosed(w, r, &body, "waiter_session_id") {
				return
			}
			if err := s.broker.CancelWait(ctx, id, strings.TrimSpace(body.WaiterSessionID)); err != nil {
				writeBrokerError(w, err)
				return
			}
			writeJSON(w, contract.WaitCancelResult{OK: true, ID: id})
		default:
			writeRefusal(w, http.StatusNotFound, "not_found", "That is not a wait action.")
		}
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such wait route.")
	}
}

func wireWait(row store.WaitRow) contract.Wait {
	out := contract.Wait{
		ID: row.ID, Repository: row.Repository, Paths: append([]string{}, row.Paths...),
		OwnerSessionID: row.Owner, ReleaseCondition: row.ReleaseCondition, CreatedAt: row.CreatedAt.Unix(),
		Waiters: []contract.WaitWaiter{},
	}
	for _, x := range row.Waiters {
		if !x.Open() {
			continue
		}
		ww := contract.WaitWaiter{SessionID: x.Waiter, Reason: x.Reason, CreatedAt: x.CreatedAt.Unix()}
		if !x.RequestDeliveredAt.IsZero() {
			ww.RequestDeliveredAt = x.RequestDeliveredAt.Unix()
		}
		if !x.ReleaseDeliveredAt.IsZero() {
			ww.ReleaseDeliveredAt = x.ReleaseDeliveredAt.Unix()
		}
		out.Waiters = append(out.Waiters, ww)
	}
	return out
}

// --- leases -------------------------------------------------------------------

// leasesRoute is the compile slot and the landing leases (D06 ②, D20).
//
//	GET  /v1/orchestrator/leases            every lease with a holder or a waiter
//	POST /v1/orchestrator/leases            ask, or ask again (idempotent on request_id)
//	POST /v1/orchestrator/leases/renew      the holder proving it is alive
//	POST /v1/orchestrator/leases/release    the holder giving it back
//	POST /v1/orchestrator/leases/cancel     a waiter leaving the line
func (s *Server) leasesRoute(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(routePath(r), "/")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if p != "/v1/orchestrator/leases" {
			writeRefusal(w, http.StatusNotFound, "not_found", "No such lease route.")
			return
		}
		views, err := s.broker.Leases(ctx)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.LeaseList{Leases: make([]contract.LeaseRecord, 0, len(views)), At: time.Now().Unix()}
		for _, v := range views {
			out.Leases = append(out.Leases, wireLease(v))
		}
		writeJSON(w, out)
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Leases are asked for with POST.")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "A lease needs the orchestrator token.")
		return
	}
	var answer orchestrator.LeaseAnswer
	var err error
	switch p {
	case "/v1/orchestrator/leases":
		var body contract.LeaseRequest
		if !decodeClosed(w, r, &body, "request_id", "resource", "checkout", "holder", "reason", "session_id",
			"pid", "process_start", "phase") {
			return
		}
		req := orchestrator.LeaseRequest{
			Resource: string(body.Resource), Checkout: body.Checkout, RequestID: strings.TrimSpace(body.RequestID),
			Holder: body.Holder, Reason: body.Reason, Session: strings.TrimSpace(body.SessionID),
			PID: int(body.PID), Phase: body.Phase,
		}
		if body.ProcessStart > 0 {
			req.ProcessStart = time.Unix(body.ProcessStart, 0)
		}
		answer, err = s.broker.Acquire(ctx, req)
	case "/v1/orchestrator/leases/renew", "/v1/orchestrator/leases/release", "/v1/orchestrator/leases/cancel":
		var body contract.LeaseOwnerRequest
		if !decodeClosed(w, r, &body, "request_id", "resource", "checkout", "phase") {
			return
		}
		o := orchestrator.LeaseOwner{Resource: string(body.Resource), Checkout: body.Checkout,
			RequestID: strings.TrimSpace(body.RequestID), Phase: body.Phase}
		switch p {
		case "/v1/orchestrator/leases/renew":
			answer, err = s.broker.Renew(ctx, o)
		case "/v1/orchestrator/leases/release":
			answer, err = s.broker.Release(ctx, o)
		default:
			answer, err = s.broker.Cancel(ctx, o)
		}
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such lease route.")
		return
	}
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	out := contract.LeaseReply{OK: true, State: answer.State, LeaseID: answer.LeaseID, Lease: wireLease(answer.View)}
	if answer.State == "queued" {
		out.Position = int64(answer.Position)
		out.HoldReason = answer.HoldReason
		out.RetryAfterSeconds = int64(answer.RetryAfter)
	}
	writeJSON(w, out)
}

func wireLease(v orchestrator.LeaseView) contract.LeaseRecord {
	out := contract.LeaseRecord{Resource: contract.LeaseResource(v.Resource), Key: v.Key, Queue: []contract.LeaseWaiter{}}
	if h := v.Holder; h != nil {
		out.Holder = &contract.LeaseHolder{
			LeaseID: h.LeaseID, RequestID: h.RequestID, Holder: h.Holder, Reason: h.Reason, SessionID: h.Session,
			PID: int64(h.PID), AcquiredAt: h.AcquiredAt.Unix(), RenewedAt: h.RenewedAt.Unix(), Phase: h.Phase,
			HeldSeconds: int64(h.HeldSeconds), RenewalAgeSeconds: int64(h.RenewalAge),
			Liveness: contract.LeaseLiveness(h.Liveness), LivenessReason: h.LivenessWhy,
		}
	}
	for _, q := range v.Queue {
		out.Queue = append(out.Queue, contract.LeaseWaiter{
			RequestID: q.RequestID, Holder: q.Holder, Reason: q.Reason, SessionID: q.Session, PID: int64(q.PID),
			Position: int64(q.Position), RequestedAt: q.RequestedAt.Unix(), WaitedSeconds: int64(q.Waited),
			Proving: q.Proving,
		})
		if q.Proving {
			out.QueueDepth++
		}
	}
	return out
}

// --- the completion ledger's manual path --------------------------------------

func (s *Server) completionsRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "The completion ledger needs the orchestrator token.")
		return
	}
	p := strings.TrimSuffix(routePath(r), "/")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch {
	case p == "/v1/orchestrator/completions" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		pending := true
		switch r.URL.Query().Get("pending") {
		case "", "true", "1":
		case "false", "0":
			pending = false
		default:
			writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
				Message: "pending is true, false, 1 or 0."})
			return
		}
		rows, err := s.broker.Completions(ctx, !pending)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.CompletionList{Completions: make([]contract.CompletionRow, 0, len(rows)), PendingOnly: pending,
			At: time.Now().Unix()}
		for _, c := range rows {
			row := contract.CompletionRow{TaskID: c.Record.ID, TaskState: string(c.Record.State), Title: c.Record.Title,
				Delivery: wireNotice(c.Notice)}
			if c.Record.Root != nil {
				row.RootSessionID = c.Record.Root.SessionID
			}
			out.Completions = append(out.Completions, row)
		}
		writeJSON(w, out)
	case p == "/v1/orchestrator/completions/reconcile" && r.Method == http.MethodPost:
		var body contract.CompletionReconcileRequest
		if !decodeClosed(w, r, &body, "task_id", "include_dead_letter") {
			return
		}
		rearmed, limited, err := s.broker.Rearm(ctx, strings.TrimSpace(body.TaskID), body.IncludeDeadLetter)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.CompletionReconcileResult{OK: true, Rearmed: rearmed, Limited: limited, BatchLimit: 25})
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such completions route.")
	}
}

// wireNotice is one envelope on the wire.
func wireNotice(n orchestrator.Notice) contract.BrokerNotice {
	stamp := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	out := contract.BrokerNotice{
		NoticeID: n.ID, State: contract.BrokerNoticeState(n.State), Attempts: int64(n.Attempts),
		CreatedAt: stamp(n.CreatedAt), LastAttemptAt: stamp(n.LastAttemptAt), NextRetryAt: stamp(n.NextRetryAt),
		TransportDeliveredAt: stamp(n.DeliveredAt), ObservedAt: stamp(n.ObservedAt),
		AcknowledgedAt: stamp(n.AcknowledgedAt), DeadLetterAt: stamp(n.DeadLetterAt), Recipient: n.Recipient,
	}
	if e := n.LastError; e != nil {
		out.LastError = &contract.BrokerNoticeError{Code: e.Code, Message: e.Message, At: stamp(e.At)}
	}
	return out
}

// --- detached automation ------------------------------------------------------

// brokerDetached is POST /v1/orchestrator/detached-tasks: the one route that
// accepts `root.session_id: null` with `root.poll_only: true`. Unattended
// automation only: nobody is told when it finishes; whoever started it polls
// GET /v1/orchestrator/tasks/<id> and reads result.json.
func (s *Server) brokerDetached(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Detached tasks are dispatched with POST.")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Dispatching needs the orchestrator token.")
		return
	}
	var body struct {
		TaskID     string           `json:"task_id"`
		Secret     string           `json:"secret"`
		Generation *json.RawMessage `json:"inventory_generation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TaskID == "" || body.Secret == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "task_id and secret are required.")
		return
	}
	req := orchestrator.DispatchRequest{TaskID: body.TaskID, Secret: body.Secret, Detached: true}
	if body.Generation != nil {
		var generation string
		if json.Unmarshal(*body.Generation, &generation) == nil {
			req.Generation = generation
			req.Offered = true
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	out, err := s.broker.Dispatch(ctx, req)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerDispatchResult{
		OK: true, Task: s.brokerTaskRow(ctx, out.Record), Replayed: out.Replayed, Warnings: brokerWarnings(out.Warnings),
	})
}
