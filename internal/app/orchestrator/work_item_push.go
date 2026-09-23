package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// EffectWorkItemCompletedPush is the one notification a Board item owes when
// it enters done. It uses the shared outbox so completion and the intent to
// notify are atomic, while the network call stays outside the write lock.
const EffectWorkItemCompletedPush = "work.item.completed.push"

// WorkItemCompletedPush carries words fixed when the item completed. A
// recovery that sends it later therefore describes the completion that
// actually recorded the effect, and Terminal keeps a tap pointed at its
// owning Session when that Session is still present.
type WorkItemCompletedPush struct {
	WorkID   string `json:"work_id"`
	Terminal string `json:"terminal,omitempty"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Tag      string `json:"tag"`
}

func runWorkItemCompletedPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p WorkItemCompletedPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.WorkID == "" || p.WorkID != e.Subject || p.Title == "" || p.Body == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no completed work item"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	sent, failed, err := b.Push(ctx, p.Title, p.Body, p.Terminal, p.Tag)
	body, _ := json.Marshal(map[string]any{"effect": e.ID, "terminal": p.Terminal, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "work.item.completed.pushed", Subject: p.WorkID, Payload: body}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	default:
		return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
	}
}
