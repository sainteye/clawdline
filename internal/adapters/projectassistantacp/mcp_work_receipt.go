package projectassistantacp

import (
	"context"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const workStatusDescription = "Read a work receipt owned by this execution; it cannot grant human approval. " +
	"Use task_id for an explicit status question or receipt recovery. End your turn while work runs; the daemon brings back the result automatically. A repeated check for a running task returns work_completion_pending: end the turn. After dispatch_work answered pa_outcome_unknown without a task_id, give the same idempotency_key instead: " +
	"state accepted returns the task_id and work; not_accepted means safe_to_resend_same_key, so resend the same key and content once; " +
	"undetermined gives a reason and is not safe to resend. A key you never dispatched answers pa_not_found. To correct or add to a running task, use message_work."

func mcpWorkStatusTool(tool func(string, string, map[string]any, ...string) any) any {
	text := map[string]any{"type": "string", "minLength": 1}
	return tool("work_status", workStatusDescription, map[string]any{"task_id": text, "idempotency_key": text})
}

// workStatus reads by task_id, or by the dispatcher's own idempotency key.
// The Bot identity behind the key is the host's authenticated fence.
func (s *ownedMCPServer) workStatus(ctx context.Context, arguments json.RawMessage) (any, error) {
	var q struct {
		TaskID string `json:"task_id"`
		Key    string `json:"idempotency_key"`
	}
	if mcpDecode(arguments, &q) != nil || (q.TaskID == "") == (q.Key == "") {
		return nil, refusal(pa.Invalid, "mcp_work")
	}
	if q.TaskID != "" {
		return s.tools.Status(ctx, q.TaskID)
	}
	tools, ok := s.tools.(app.WorkReceiptTools)
	if !ok {
		return nil, refusal(pa.Unsupported, "work_receipt")
	}
	return tools.WorkReceipt(ctx, q.Key)
}
