package projectassistantacp

import (
	"context"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const mcpMessageWorkDescription = "Send a correction or extra instruction to a work Session this conversation dispatched with dispatch_work while it is still running. Give only its task_id; the task record decides the Session. The worker sees it as quoted guidance from you (the dispatcher), not as the user's authorization, and it grants no new permission such as deploying or pushing. It is typed into the worker's input at once, the same way `clawdline send` delivers a Session message; a busy worker reads it at its next opportunity. stage delivered means it reached the worker's input, not that it was read or followed; stage accepted means delivery is not settled yet. Work dispatched elsewhere, not yet started, finished or cancelled is refused. Text is at most 8,192 UTF-8 bytes. Reuse the same idempotency key after an uncertain response; it never sends twice."

func mcpMessageWorkTool() any {
	text := map[string]any{"type": "string", "minLength": 1}
	return map[string]any{"name": "message_work", "description": mcpMessageWorkDescription, "inputSchema": map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"task_id", "text", "idempotency_key"},
		"properties": map[string]any{
			"task_id":         text,
			"text":            map[string]any{"type": "string", "minLength": 1, "maxLength": pa.MaxWorkMessageBytes},
			"idempotency_key": text,
		},
	}}
}

func (s *ownedMCPServer) messageWork(ctx context.Context, arguments json.RawMessage) (any, error) {
	var q struct {
		TaskID string `json:"task_id"`
		Text   string `json:"text"`
		ID     string `json:"idempotency_key"`
	}
	if mcpDecode(arguments, &q) != nil || q.TaskID == "" || q.ID == "" {
		return nil, refusal(pa.Invalid, "mcp_message_work")
	}
	if err := pa.ValidateWorkMessage(q.Text); err != nil {
		return nil, err
	}
	tools, ok := s.tools.(app.WorkMessageTools)
	if !ok {
		return nil, refusal(pa.Unsupported, "message_work")
	}
	receipt, err := tools.MessageWork(ctx, q.TaskID, q.Text, q.ID)
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
