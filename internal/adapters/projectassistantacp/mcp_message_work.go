package projectassistantacp

import (
	"context"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const mcpMessageWorkDescription = "Send a correction or extra instruction to a work Session this conversation dispatched with dispatch_work while it is still running. Give only its task_id; the task record decides the Session. The worker sees it as quoted guidance from you (the dispatcher), not as the user's authorization, and it grants no new permission such as deploying or pushing. It is typed into the worker's input at once, the same way `clawdline send` delivers a Session message; a busy worker reads it at its next opportunity. stage delivered means it reached the worker's input, not that it was read or followed; stage accepted means delivery is not settled yet. Work dispatched elsewhere, not yet started, finished or cancelled is refused. Text is at most 8,192 UTF-8 bytes. Reuse the same idempotency key after an uncertain response; it never sends twice. " +
	"在 dispatch_work 派出的工作階段仍在執行時，傳送修正或補充指示。只填 task_id，由任務紀錄決定對象。對方會看到這是你（派工者）轉來的引用內容，不是使用者授權，不會因此取得部署、push 等額外權限。訊息會立即打進對方的輸入框，與 `clawdline send` 相同；對方忙碌時會在下一個空檔讀到。stage 為 delivered 只代表已送進輸入框，不代表已讀或照做；accepted 代表尚未確認送達。不是自己派的、尚未開始、已結束或已取消的工作會被拒絕。內容上限 8,192 個 UTF-8 位元組。結果不明時沿用原冪等鍵查詢，不會重送。"

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
