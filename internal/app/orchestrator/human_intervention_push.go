package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// EffectHumanInterventionPush is the notification owed by an agent-authored
// note that the person did not explicitly request.
const EffectHumanInterventionPush = "human.intervention.push"

type HumanInterventionPush struct {
	NoteID   string `json:"note_id"`
	Terminal string `json:"terminal"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Tag      string `json:"tag"`
}

func runHumanInterventionPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p HumanInterventionPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.NoteID == "" || p.NoteID != e.Subject ||
		p.Terminal == "" || p.Title == "" || p.Body == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no note notification"}
	}
	if b.NotifyEnabled != nil && !b.NotifyEnabled() {
		return effectResult{state: store.EffectDone, outcome: "agent_notify_disabled"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	sent, failed, err := b.Push(ctx, p.Title, p.Body, p.Terminal, p.Tag)
	body, _ := json.Marshal(map[string]any{"effect": e.ID, "terminal": p.Terminal, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "human.intervention.pushed", Subject: p.NoteID, Payload: body}}
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
