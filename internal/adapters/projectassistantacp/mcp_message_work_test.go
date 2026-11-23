package projectassistantacp

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type messageWorkTestTools struct {
	mcpTestTools
	sent *[]string
}

func (t messageWorkTestTools) MessageWork(_ context.Context, taskID, text, key string) (pa.WorkMessageReceipt, error) {
	if taskID == "foreign-task" {
		return pa.WorkMessageReceipt{}, &pa.Failure{Code: pa.ScopeDenied, Field: "work_owner"}
	}
	*t.sent = append(*t.sent, taskID+"|"+key+"|"+text)
	return pa.WorkMessageReceipt{TaskID: taskID, IdempotencyKey: key, Stage: pa.WorkMessageDelivered, AcceptedAt: time.Unix(10, 0), DeliveredAt: time.Unix(11, 0)}, nil
}

// message_work is listed with its limits, routes only task_id/text/key to the
// host, reports the delivered stage, and refuses oversize text before the host.
func TestOwnedMCPMessageWorkRoutesOnlyTheTaskAndReportsItsStage(t *testing.T) {
	var sent []string
	f := newMCPFixture(t, messageWorkTestTools{sent: &sent})
	f.initialize(t)
	_, list := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if !bytes.Contains(list, []byte(`"message_work"`)) || !bytes.Contains(list, []byte("not as the user's authorization")) || !bytes.Contains(list, []byte("不是使用者授權")) {
		t.Fatalf("message_work is not described: %s", list)
	}
	_, b := f.post(t, mcpCall("message_work", map[string]any{"task_id": "owned-task", "text": "Look at the backend first.", "idempotency_key": "k1"}), nil)
	if !bytes.Contains(b, []byte(`"isError":false`)) || !bytes.Contains(b, []byte(`\"stage\":\"delivered\"`)) {
		t.Fatalf("delivered answer: %s", b)
	}
	if len(sent) != 1 || sent[0] != "owned-task|k1|Look at the backend first." {
		t.Fatalf("host received %v", sent)
	}
	for _, args := range []map[string]any{
		{"task_id": "owned-task", "text": strings.Repeat("x", pa.MaxWorkMessageBytes+1), "idempotency_key": "k2"},
		{"task_id": "owned-task", "text": "hi"},
		{"task_id": "owned-task", "text": "hi", "idempotency_key": "k3", "terminal": "%1"},
	} {
		_, b = f.post(t, mcpCall("message_work", args), nil)
		if !bytes.Contains(b, []byte(`"isError":true`)) {
			t.Fatalf("accepted %v: %s", args, b)
		}
	}
	_, b = f.post(t, mcpCall("message_work", map[string]any{"task_id": "foreign-task", "text": "hi", "idempotency_key": "k4"}), nil)
	if !bytes.Contains(b, []byte(string(pa.ScopeDenied))) || !bytes.Contains(b, []byte("work_owner")) {
		t.Fatalf("foreign refusal lost its field: %s", b)
	}
	if len(sent) != 1 {
		t.Fatalf("refused calls reached the host: %v", sent)
	}
}
