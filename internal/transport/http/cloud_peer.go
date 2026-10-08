package http

// Local and paired-viewer routes for a fixed machine peer exchange. The
// target inbox is structured work, never injected into ordinary chat.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/adapters/store"
	peerapp "github.com/sainteye/clawdline/internal/app/agenthandoff"
	peercontract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/auth"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type CloudPeerLine interface {
	PublishPeer(context.Context, peercontract.Request, []byte) (peerapp.Result, error)
	ControlPeer(context.Context, cloudtransport.PeerControlInput) (cloudtransport.PeerControlResult, error)
}

func (s *Server) cloudPeerControlRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A peer pair or grant is changed with POST.")
		return
	}
	access := accessOf(r).verdict
	if !access.Allowed || !(access.Local || access.Caps.Has(auth.Send)) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may not manage machine peer grants.")
		return
	}
	line := s.peerLine()
	if line == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "peer_unavailable", "The machine peer line is unavailable.")
		return
	}
	var input cloudtransport.PeerControlInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&input); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The peer control request is invalid.")
		return
	}
	answer, err := line.ControlPeer(r.Context(), input)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "peer_control_refused", "The machine peer action was refused.")
		return
	}
	writeJSON(w, answer)
}

func (s *Server) peerLine() CloudPeerLine {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		return nil
	}
	peer, _ := line.(CloudPeerLine)
	return peer
}

// PeerReceipts gives the machine peer receiver this daemon's existing durable
// idempotency table. The Cloud link never receives a viewer credential here.
func (s *Server) PeerReceipts() peerapp.Receipts { return s.store }

// PeerExecute is called only after authenticated envelope opening, two fresh
// authority reads and a receipt claim. A message or handoff lands in a
// separate inbox with its exact source and target execution identity.
func (s *Server) PeerExecute(ctx context.Context, request peercontract.Request, body []byte) error {
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.PutInbox(ctx, peerstore.InboxItem{Request: request, Body: string(body), At: time.Now().UTC()})
}

func (s *Server) cloudPeerSendRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A peer message is sent with POST.")
		return
	}
	access := accessOf(r).verdict
	if !access.Allowed || !(access.Local || access.Caps.Has(auth.Send)) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may not send peer work.")
		return
	}
	line := s.peerLine()
	if line == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "peer_unavailable", "The machine peer line is unavailable.")
		return
	}
	var input struct {
		Request peercontract.Request `json:"request"`
		Body    string               `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, peercontract.MaxBodyBytes+4096)).Decode(&input); err != nil ||
		len(input.Body) == 0 || len(input.Body) > peercontract.MaxBodyBytes {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The peer request or body is invalid.")
		return
	}
	result, err := line.PublishPeer(r.Context(), input.Request, []byte(input.Body))
	if err != nil {
		code := "peer_send_unavailable"
		var refusal peercontract.Refusal
		if errors.As(err, &refusal) {
			code = refusal.Error()
		}
		writeJSON(w, peerapp.Result{RequestID: input.Request.RequestID, Code: code,
			MachineExecution: "unknown", RelayAccepted: "unknown", RelayDelivered: "unknown",
			SessionDelivered: "unknown", AgentObserved: "unknown", AgentAcknowledged: "unknown"})
		return
	}
	writeJSON(w, result)
}

func (s *Server) cloudPeerInboxRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A peer inbox is read with GET.")
		return
	}
	access := accessOf(r).verdict
	if !access.Allowed || !(access.Local || access.Caps.Has(auth.Read)) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may not read the peer inbox.")
		return
	}
	query := r.URL.Query()
	machine, session, generation := query.Get("machine_id"), query.Get("session_id"), query.Get("execution_generation")
	if len(query.Get("before")) > 128 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The peer inbox cursor is invalid.")
		return
	}
	if err := s.AdmitExecutionTarget(r.Context(), machine, session, generation); err != nil {
		writeRefusal(w, http.StatusConflict, "execution_generation_changed", "The target Session execution changed or could not be verified.")
		return
	}
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "peer_unavailable", "The peer inbox is unavailable.")
		return
	}
	defer st.Close()
	items, nextBefore, err := st.InboxPage(r.Context(), peercontract.Endpoint{
		MachineID: machine, SessionID: session, ExecutionGeneration: generation,
	}, query.Get("before"))
	if err != nil {
		if errors.Is(err, peerstore.ErrNotFound) {
			writeRefusal(w, http.StatusNotFound, "peer_cursor_not_found", "The peer inbox cursor does not belong to this execution.")
			return
		}
		writeRefusal(w, http.StatusServiceUnavailable, "peer_unavailable", "The peer inbox is unavailable.")
		return
	}
	type inboxDelivery struct {
		Request    peercontract.Request `json:"request"`
		BodyBase64 string               `json:"body_base64"`
		At         time.Time            `json:"at"`
		Receipt    peerapp.Result       `json:"receipt"`
	}
	filtered := make([]inboxDelivery, 0, len(items))
	for _, item := range items {
		actor, key, _ := peercontract.ReceiptIdentity(item.Request)
		claim, err := s.store.LookupReceipt(r.Context(), store.ReceiptKey{
			Scope: peercontract.ReceiptScope, Actor: actor, Key: key,
		})
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "peer_receipt_unavailable", "The target receipt is unavailable.")
			return
		}
		receipt := peerapp.Result{RequestID: item.Request.RequestID, Code: "handoff_receipt_unavailable",
			MachineExecution: "unknown", RelayAccepted: "unknown", RelayDelivered: "unknown",
			SessionDelivered: "unknown", AgentObserved: "unknown", AgentAcknowledged: "unknown"}
		if claim.Outcome == store.ReceiptReplay {
			var saved peerapp.Result
			if json.Unmarshal(claim.Answer.Body, &saved) == nil && saved.RequestID == item.Request.RequestID {
				receipt = saved
			}
		} else if claim.Outcome == store.ReceiptPending {
			receipt.Code, receipt.MachineExecution = "handoff_receipt_pending", "pending"
		}
		filtered = append(filtered, inboxDelivery{Request: item.Request,
			BodyBase64: base64.StdEncoding.EncodeToString([]byte(item.Body)), At: item.At, Receipt: receipt})
	}
	writeJSON(w, struct {
		MachineID          string          `json:"machine_id"`
		SessionID          string          `json:"session_id"`
		ExpectedGeneration string          `json:"expected_generation"`
		NextBefore         string          `json:"next_before,omitempty"`
		Items              []inboxDelivery `json:"items"`
	}{MachineID: machine, SessionID: session, ExpectedGeneration: generation, NextBefore: nextBefore, Items: filtered})
}

func (s *Server) cloudPeerOutboxRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A peer relay receipt is read with GET.")
		return
	}
	access := accessOf(r).verdict
	if !access.Allowed || !(access.Local || access.Caps.Has(auth.Read)) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may not read peer relay receipts.")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/cloud/peer/outbox/")
	if id == "" || strings.Contains(id, "/") {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A peer request ID is required.")
		return
	}
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "peer_unavailable", "The peer relay receipt is unavailable.")
		return
	}
	defer st.Close()
	item, err := st.Outbox(r.Context(), id)
	if err != nil || item.Request.Source.MachineID != s.executionMachine() {
		writeRefusal(w, http.StatusNotFound, "peer_request_missing", "This machine has no such peer request.")
		return
	}
	writeJSON(w, item)
}
