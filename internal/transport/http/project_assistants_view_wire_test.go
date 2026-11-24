package http

import (
	"encoding/json"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantViewProgressCrossesCloudReadBridge(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	snapshot := paViewServerRead(t, f, pa.ReadViewSnapshot, "", "", 0, 20, f.local)
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	q := contract.ProjectAssistantReadViewRequest{
		Key: snapshot.Head.Key, ConversationID: snapshot.Head.Conversation.ID,
		Mode: contract.ProjectAssistantReadViewModeProgress, SourceEpoch: snapshot.SourceEpoch, PageSize: 20,
	}
	answer := paViewServerBridge(t, f, "reader", q)
	if !answer.OK() || answer.Mutating {
		t.Fatalf("progress request lost through the read bridge: %s", answer.Payload)
	}
	var response struct {
		Body contract.ProjectAssistantReadViewResponse `json:"body"`
	}
	if err := json.Unmarshal(answer.Payload, &response); err != nil {
		t.Fatal(err)
	}
	if response.Body.Mode != q.Mode || response.Body.SourceEpoch != q.SourceEpoch || response.Body.Items == nil {
		t.Fatalf("progress response changed: %+v", response.Body)
	}
}

func TestProjectAssistantViewProgressWireKeepsStartTimeAndSourceStep(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 987654321, time.UTC)
	row := app.ReadViewItem{
		ID: `["turn","segment"]`, Kind: app.ReadItemTrace, OccurredAt: at, ObservedEventSeq: 12,
		Trace: &pa.Trace{ID: "segment", TurnID: "turn", Category: "agent_message_chunk", Text: "Progress update."},
	}
	wire, err := paWireReadItem(row)
	if err != nil || wire.Trace == nil || wire.Trace.Text != row.Trace.Text || wire.ObservedEventSeq != row.ObservedEventSeq ||
		wire.ID != row.ID || wire.OccurredAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("progress wire row = %+v, err = %v", wire, err)
	}
}
