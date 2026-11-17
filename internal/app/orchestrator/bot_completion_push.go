package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/productcopy"
)

const EffectBotCompletedPush = "bot.completed.push"

type botCompletedPush struct {
	Bot          string `json:"bot"`
	Conversation string `json:"conversation"`
	Turn         string `json:"turn"`
	Name         string `json:"name"`
	Tag          string `json:"tag"`
}

// BotCompletionIntent is called inside the successful Bot commit, never after it.
// Receipt replay and an already terminal turn cannot enqueue another intent.
func (b *Broker) BotCompletionIntent(ctx context.Context, tx *sql.Tx, bot pa.Bot, turn pa.Turn) error {
	if !pa.NotifiableCompletion(bot, turn) {
		return nil
	}
	p := botCompletedPush{Bot: bot.Key.BotID, Conversation: turn.Fence.ConversationID, Turn: turn.ID,
		Name: bot.Name,
		Tag:  "bot-completed-" + bot.Key.MachineID + "-" + bot.Key.BotID + "-" + turn.ID}
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return b.Store.AppendEffectTx(ctx, tx, store.Effect{Kind: EffectBotCompletedPush, Subject: p.Tag, Payload: payload})
}

func runBotCompletedPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p botCompletedPush
	if json.Unmarshal(e.Payload, &p) != nil || p.Bot == "" || p.Conversation == "" || p.Turn == "" || p.Tag == "" || p.Name == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no completed Bot task"}
	}
	if b.PushEnabled != nil && !b.PushEnabled("push_on_bot_done") {
		return effectResult{state: store.EffectDone, outcome: "disabled_by_user"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	language := b.notificationLanguage()
	title := productcopy.Format(language, "bot.completed.title", nil)
	body := productcopy.Format(language, "bot.completed.body", map[string]string{"name": p.Name})
	sent, failed, err := b.Push(adapterpush.WithBotDestination(ctx, p.Bot, p.Conversation), title, body, "", p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "bot": p.Bot, "turn": p.Turn, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "bot.completed.pushed", Subject: e.Subject, Payload: result}}
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
