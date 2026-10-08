package cloudops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// Only operations on a live terminal use the execution generation. Machine
// commands (including start and resume) have different destination contracts.
func sessionMutation(word string) bool {
	switch word {
	case "send", "answer", "key", "interrupt", "end", "archive-session":
		return true
	}
	return false
}

var generationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func executionGeneration(raw any) (string, bool) {
	g, ok := raw.(string)
	return g, ok && generationPattern.MatchString(g)
}

// The scope is global within the daemon's existing 4096-row receipt window.
// JSON tuple encoding prevents separators in terminal IDs from aliasing keys.
func sessionReceiptKey(sender, machine, session, generation, action, request string) store.ReceiptKey {
	actor, _ := json.Marshal([]string{sender, machine, session, generation, action})
	return store.ReceiptKey{Scope: "cloud.session", Actor: string(actor), Key: request}
}

func sessionDigest(parsed body) string {
	canonical, _ := json.Marshal(parsed)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func init() {
	register(op{name: "session-receipt", read: true, decode: func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "target_request", "execution_generation", "action") {
			return plan{}, false
		}
		p, ok := actionPlan(b, true)
		return p, ok && p.request != ""
	}})
}

// sessionCommand admits the exact destination and request before the local
// route can have an effect. A dead owner is an unknown outcome, not a retry.
func (b Bridge) sessionCommand(ctx context.Context, cmd Command, parsed body, p plan, o op, generation string) Answer {
	if b.Receipts == nil || b.AdmitExecution == nil {
		return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable",
			Message: "This machine cannot verify a durable receipt for that action."}, nil)
	}
	k := sessionReceiptKey(cmd.Sender, b.MachineID, p.target, generation, o.name, p.request)
	claim, err := b.Receipts.ClaimReceipt(ctx, k, sessionDigest(parsed), store.ReceiptPolicy{}, time.Now())
	if err != nil {
		return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable",
			Message: "This machine cannot verify a durable receipt for that action."}, nil)
	}
	switch claim.Outcome {
	case store.ReceiptMismatch:
		return b.publish(cmd, p, Refusal{Status: 409, Code: "idempotency_key_reused",
			Message: "This request ID was already used with different parameters."}, nil)
	case store.ReceiptExpired, store.ReceiptOrphaned:
		return b.publish(cmd, p, Refusal{Status: 409, Code: "receipt_outcome_unknown",
			Message: "The earlier action may have run; check its receipt before acting again."}, nil)
	case store.ReceiptPending:
		return b.publish(cmd, p, Refusal{Status: 409, Code: "receipt_pending",
			Message: "This request is still being processed."}, nil)
	case store.ReceiptFull:
		return b.publish(cmd, p, Refusal{Status: 429, Code: "receipt_capacity",
			Message: "This machine has reached its receipt limit; try later with the same request ID."}, nil)
	case store.ReceiptReplay:
		var answer Answer
		if json.Unmarshal(claim.Answer.Body, &answer) != nil {
			return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable",
				Message: "This machine cannot read the earlier action's receipt."}, nil)
		}
		return answer
	case store.ReceiptNew:
		if err := b.AdmitExecution(ctx, b.MachineID, p.target, generation); err != nil {
			answer := b.publish(cmd, p, executionRefusal(err), nil)
			if !b.completeSessionReceipt(ctx, k, answer) {
				return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_outcome_unknown",
					Message: "The action may have run, but its durable result could not be recorded."}, nil)
			}
			return answer
		}
		p.executionGeneration = generation
		answer := b.route(ctx, cmd, p, o)
		if !b.completeSessionReceipt(ctx, k, answer) {
			return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_outcome_unknown",
				Message: "The action may have run, but its durable result could not be recorded."}, nil)
		}
		return answer
	default:
		return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable",
			Message: "This machine cannot verify a durable receipt for that action."}, nil)
	}
}

func (b Bridge) completeSessionReceipt(ctx context.Context, key store.ReceiptKey, answer Answer) bool {
	encoded, err := json.Marshal(answer)
	// The caller can leave after the local effect. Its canceled context must
	// not discard the only durable record of that effect.
	return err == nil && b.Receipts.CompleteReceipt(context.WithoutCancel(ctx), key,
		store.ReceiptAnswer{Status: answer.Status, Body: encoded}) == nil
}

// The execution authority returns typed errors whose Error text is its stable
// code. Only its documented codes cross the wire; other failures stay closed.
func executionRefusal(err error) Refusal {
	switch err.Error() {
	case "execution_generation_changed":
		return Refusal{Status: 409, Code: "execution_generation_changed", Message: "This session is no longer the execution that was selected."}
	case "execution_target_missing":
		return Refusal{Status: 404, Code: "execution_target_missing", Message: "This session's execution is no longer present."}
	case "execution_target_required", "execution_machine_mismatch":
		return Refusal{Status: 400, Code: err.Error(), Message: "This action did not name its selected execution."}
	case "execution_source_unknown", "execution_check_unavailable", "execution_records_full":
		return Refusal{Status: 503, Code: err.Error(), Message: "This machine cannot verify the selected execution."}
	default:
		return Refusal{Status: 503, Code: "execution_check_unavailable", Message: "This machine cannot verify the selected execution."}
	}
}

// sessionReceipt is a read keyed by the original request ID, not an admission.
// It never sends an action or infers success from a relay acknowledgement.
func (b Bridge) sessionReceipt(ctx context.Context, cmd Command, parsed body) Answer {
	if cmd.Class != ClassCtl || !parsed.has("type", "session", "request", "target_request", "execution_generation", "action") {
		return b.refuse(cmd, parsed, "session-receipt", Refusal{Status: 400, Code: "malformed_read", Message: "This receipt read is malformed."})
	}
	p, ok := actionPlan(parsed, true)
	target, targetOK := requestName(parsed["target_request"])
	generation, generationOK := executionGeneration(parsed["execution_generation"])
	action, actionOK := parsed.str("action")
	if !ok || p.request == "" || !targetOK || !generationOK || !actionOK || !sessionMutation(action) {
		return b.refuse(cmd, parsed, "session-receipt", Refusal{Status: 400, Code: "malformed_read", Message: "This receipt read is malformed."})
	}
	p.name = "read:" + p.request
	if refusal, denied := b.authorize(ctx, cmd.Sender, cmd.VerifiedKey, false); denied {
		return b.publish(cmd, p, refusal, nil)
	}
	if b.Receipts == nil {
		return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable", Message: "This machine cannot read its receipts."}, nil)
	}
	k := sessionReceiptKey(cmd.Sender, b.MachineID, p.target, generation, action, target)
	reading, err := b.Receipts.LookupReceipt(ctx, k)
	if err != nil {
		return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable", Message: "This machine cannot read its receipts."}, nil)
	}
	result := "unknown"
	var status int
	var code string
	switch reading.Outcome {
	case store.ReceiptMissing:
		result = "missing"
	case store.ReceiptPending:
		result = "pending"
	case store.ReceiptExpired, store.ReceiptOrphaned:
		result = "unknown"
	case store.ReceiptReplay:
		var original Answer
		if json.Unmarshal(reading.Answer.Body, &original) != nil {
			return b.publish(cmd, p, Refusal{Status: 503, Code: "receipt_unavailable", Message: "This machine cannot read its receipts."}, nil)
		}
		status, code = original.Status, original.Code
		switch {
		case original.OK():
			result = "completed"
		case code == "route_failed" || code == "receipt_outcome_unknown":
			result = "unknown"
		default:
			result = "rejected"
		}
	}
	data, _ := json.Marshal(map[string]any{"request": target, "action": action,
		"execution_generation": generation, "machine_execution": result, "status": status, "code": code,
		"relay_accepted": "unknown", "relay_delivered": "unknown", "viewer_observed": "unknown", "viewer_acknowledged": "unknown"})
	return b.publish(cmd, p, Refusal{}, data)
}
