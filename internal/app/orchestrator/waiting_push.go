package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// The push a session standing on a question owes the person (app.Waiting,
// docs/push.md "有人在等你回答").
//
// It is one more kind on this outbox for the reason the capacity push is: the
// outbox is the daemon's one way to do something to the world after a commit
// (D08), and its recovery is what makes "once" hold across a crash — a push
// begun and never finished is recorded unknown and not sent again, because a
// push cannot be asked afterwards whether it went.

// EffectWaitingPush is the push one stopped session owes.
const EffectWaitingPush = "session.waiting.push"

// WaitingPush is what that push carries. The words were fixed when the stop
// was recorded, so a push a recovery runs late says what was true then.
type WaitingPush struct {
	// Key is the stop's subject: the terminal and the conversation in it.
	Key string `json:"key"`
	// Terminal is where tapping the notification lands (D24).
	Terminal string `json:"terminal"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Tag      string `json:"tag"`
}

// runWaitingPush sends one stopped session's push to every device that asked
// for notifications. Sent is what the push services accepted; whether a phone
// showed it is nothing a push service answers.
func runWaitingPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p WaitingPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Key == "" || p.Title == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no stop"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	sent, failed, err := b.Push(ctx, p.Title, p.Body, p.Terminal, p.Tag)
	body, _ := json.Marshal(map[string]any{"effect": e.ID, "terminal": p.Terminal, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "session.waiting.pushed", Subject: p.Key, Payload: body}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		// Nobody asked to be notified. The session is still drawn waiting on
		// every list, and there is nothing to retry.
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	}
	return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
}
