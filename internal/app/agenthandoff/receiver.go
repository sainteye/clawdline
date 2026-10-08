// Package agenthandoff owns durable admission for peer-machine Agent work.
// Its transport and grant reader are injected; neither an account roster nor
// the viewer Cloud bridge is a fallback when those dependencies are absent.
package agenthandoff

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	contract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

// Result reports independent evidence. A receiver cannot fill relay or Agent
// observation fields merely because its local effect returned.
type Result struct {
	RequestID         string `json:"request_id"`
	Code              string `json:"code"`
	MachineExecution  string `json:"machine_execution"`
	RelayAccepted     string `json:"relay_accepted"`
	RelayDelivered    string `json:"relay_delivered"`
	SessionDelivered  string `json:"session_delivered"`
	AgentObserved     string `json:"agent_observed"`
	AgentAcknowledged string `json:"agent_acknowledged"`
}

func result(id, code, execution string) Result {
	return Result{RequestID: id, Code: code, MachineExecution: execution,
		RelayAccepted: "unknown", RelayDelivered: "unknown",
		SessionDelivered: "unknown", AgentObserved: "unknown", AgentAcknowledged: "unknown"}
}

// Receipts is the existing SQLite request receipt seam.
type Receipts interface {
	ClaimReceipt(context.Context, store.ReceiptKey, string, store.ReceiptPolicy, time.Time) (store.ReceiptClaim, error)
	CompleteReceipt(context.Context, store.ReceiptKey, store.ReceiptAnswer) error
}

// Receiver needs fresh local pair, grant, capability and execution facts on
// both sides of the durable claim. Execute is the only effectful callback.
// It persists the kind and both exact endpoints in the structured inbox.
type Receiver struct {
	Receipts Receipts
	Facts    func(context.Context, contract.Request, contract.Principal) (contract.Facts, error)
	Execute  func(context.Context, contract.Request, []byte) error
	Now      func() time.Time
}

// Receive never retries a possibly executed request. A failed receipt write
// after Execute yields unknown; the same ID will later be orphaned, not rerun.
func (r Receiver) Receive(ctx context.Context, request contract.Request, body []byte, principal contract.Principal) Result {
	if r.Receipts == nil || r.Facts == nil || r.Execute == nil {
		return result(request.RequestID, "handoff_unavailable", "unknown")
	}
	read := func() error {
		facts, err := r.Facts(ctx, request, principal)
		if err != nil {
			return contract.GrantUnavailable
		}
		facts.Principal = principal
		return contract.Admit(request, body, facts)
	}
	if err := read(); err != nil {
		return result(request.RequestID, err.Error(), "rejected")
	}
	actor, key, digest := contract.ReceiptIdentity(request)
	receiptKey := store.ReceiptKey{Scope: contract.ReceiptScope, Actor: actor, Key: key}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	claim, err := r.Receipts.ClaimReceipt(ctx, receiptKey, digest, store.ReceiptPolicy{}, now)
	if err != nil {
		return result(request.RequestID, "handoff_receipt_unavailable", "unknown")
	}
	switch claim.Outcome {
	case store.ReceiptMismatch:
		return result(request.RequestID, "handoff_request_id_reused", "rejected")
	case store.ReceiptExpired:
		return result(request.RequestID, "handoff_receipt_expired", "unknown")
	case store.ReceiptPending:
		return result(request.RequestID, "handoff_receipt_pending", "pending")
	case store.ReceiptFull:
		return result(request.RequestID, "handoff_receipt_capacity", "rejected")
	case store.ReceiptReplay:
		var previous Result
		if json.Unmarshal(claim.Answer.Body, &previous) != nil || previous.RequestID != request.RequestID {
			return result(request.RequestID, "handoff_receipt_unavailable", "unknown")
		}
		return previous
	case store.ReceiptOrphaned:
		answer := result(request.RequestID, "handoff_outcome_unknown", "unknown")
		return r.complete(ctx, receiptKey, answer)
	case store.ReceiptNew:
		// Grant revocation or execution replacement between the first read and
		// claim must still stop the effect.
		if err := read(); err != nil {
			return r.complete(ctx, receiptKey, result(request.RequestID, err.Error(), "rejected"))
		}
		if err := r.Execute(ctx, request, body); err != nil {
			return r.complete(ctx, receiptKey, result(request.RequestID, "handoff_outcome_unknown", "unknown"))
		}
		answer := result(request.RequestID, "ok", "completed")
		answer.SessionDelivered = "delivered"
		return r.complete(ctx, receiptKey, answer)
	default:
		return result(request.RequestID, "handoff_receipt_unavailable", "unknown")
	}
}

func (r Receiver) complete(ctx context.Context, key store.ReceiptKey, answer Result) Result {
	encoded, err := json.Marshal(answer)
	if err != nil || r.Receipts.CompleteReceipt(context.WithoutCancel(ctx), key,
		store.ReceiptAnswer{Status: 200, Body: encoded}) != nil {
		return result(answer.RequestID, "handoff_outcome_unknown", "unknown")
	}
	return answer
}
