package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// The capacity register's pushes (design-decisions C4, docs/limits.md §4.5).
//
// The register is not the broker's, but the outbox is the daemon's one way to
// do something to the world after a commit (D08), and this broker is what runs
// it — including the recovery that settles, exactly once, an effect a process
// that has since died left behind. A capacity push is therefore one more kind
// here rather than a second outbox beside this one: the capacity beat records
// the `capacity.notify` event and this effect in one transaction, and hands
// the effect to RunEffects.

// EffectCapacityPush is the push one capacity notice owes.
const EffectCapacityPush = "capacity.push"

// CapacityPush is what a capacity push carries. The words were fixed when the
// notice was recorded, so a push run late — by a recovery after a crash —
// says what was true when the row crossed its line, and the state in it says
// which line that was.
type CapacityPush struct {
	Name  string `json:"name"`
	State string `json:"state"`
	From  string `json:"from,omitempty"`
	Used  int64  `json:"used"`
	Limit int64  `json:"limit"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
}

// runCapacityPush sends one capacity notice to every device that asked for
// notifications. Tapping it opens this daemon's page; no session is its
// subject.
//
// Sent is what the push services accepted. Whether a phone showed it is
// nothing a push service answers, so it is not recorded as delivered.
func runCapacityPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p CapacityPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Name == "" || p.Title == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no notice"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	sent, failed, err := b.Push(ctx, p.Title, p.Body, "", p.Tag)
	body, _ := json.Marshal(map[string]any{"name": p.Name, "state": p.State, "effect": e.ID,
		"sent": sent, "failed": failed})
	events := []store.Event{{Kind: "capacity.notify.pushed", Subject: p.Name, Payload: body}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		// Nobody asked to be notified: the notice is still in the store, in
		// /v1/diagnostics and in the Settings page's capacity block, and there
		// is nothing to retry.
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	}
	return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
}

// RunEffects runs, in order and after the commit that recorded them, effects
// this broker's store recorded for somebody outside the broker — the capacity
// beat's pushes — exactly as it runs its own: started before it is attempted,
// finished with how it ended, and never run inside a write.
func (b *Broker) RunEffects(ctx context.Context, ids []int64) {
	b.runRecorded(ctx, ids)
}
