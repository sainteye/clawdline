package orchestrator

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/store"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Bot request card effects (docs/bot-request-cards.md §5, §7.2). The store
// queues each in the transaction of the card write that caused it, keyed so
// one handoff or one completion has one row; these handlers drain them.
//
//   - bot_request.handoff wakes the new holder with one host-notice turn.
//   - bot_request.done.notice wakes each asking Bot with one host-notice turn.
//   - bot_request.done pushes one notification to the person.
//
// Both notices go through the Bot runtime's receipt for their key, so a retry,
// a restart or a second drain of the same row queues no second turn: the
// handlers are idempotent, and a full Bot is retried until
// pa.CompletionWakeRetryWindow ends. The push cannot be asked whether it
// already happened, so its handler is not idempotent and an interrupted push
// is recorded unknown, never sent twice.

// MaxBotRequestCardHops bounds following merged_into from a named card to the
// card that absorbed it.
const MaxBotRequestCardHops = 8

// BotRequestNotice is one notice turn for one Bot on this machine.
type BotRequestNotice struct {
	Bot     string
	Key     string
	CardID  string
	Version int64
	// Kind is "handoff" or "done".
	Kind  string
	Title string
	Role  string
	Note  string
	From  string
	// LastAttempt is set when the retry window has ended, so a full Bot is
	// final and the host tells the person instead.
	LastAttempt bool
}

func init() {
	effectHandlers[store.EffectBotRequestHandoff] = effectHandler{idempotent: true, run: runBotRequestHandoff}
	effectHandlers[store.EffectBotRequestDoneNotice] = effectHandler{idempotent: true, run: runBotRequestDoneNotice}
	effectHandlers[store.EffectBotRequestDone] = effectHandler{idempotent: false, run: runBotRequestDonePush}
}

// botRequestCard resolves the card a dispatch names for bot: it follows
// merged_into to the surviving card, which must be open and have bot in its
// chain. "Not found" and "could not check" stay apart.
func (b *Broker) botRequestCard(ctx context.Context, id, bot string) (string, error) {
	if b.Store == nil {
		return "", assistantFailure(pa.Unsupported, "bot_request_store")
	}
	for range MaxBotRequestCardHops {
		c, err := b.Store.BotRequestCard(ctx, id)
		if errors.Is(err, store.ErrNoBotRequest) {
			return "", assistantFailure(pa.NotFound, "bot_request_id")
		}
		if err != nil {
			return "", err
		}
		if c.State == br.StateMerged && c.MergedInto != "" {
			id = c.MergedInto
			continue
		}
		if c.State != br.StateOpen {
			return "", assistantFailure(pa.ScopeDenied, "bot_request_closed")
		}
		if !c.InChain(bot) {
			return "", assistantFailure(pa.ScopeDenied, "bot_request_chain")
		}
		return c.ID, nil
	}
	return "", assistantFailure(pa.CapacityFull, "bot_request_merged_into")
}

// BotRequestCardFor is the card check DispatchAssistant applies, for callers
// that must decide before dispatching: the surviving card's id, or a typed
// refusal.
func (b *Broker) BotRequestCardFor(ctx context.Context, id, bot string) (string, error) {
	return b.botRequestCard(ctx, id, bot)
}

func runBotRequestHandoff(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p store.BotRequestHandoffPayload
	if json.Unmarshal(e.Payload, &p) != nil || p.CardID == "" || p.ToBot == "" || p.Version <= 0 {
		return effectResult{state: store.EffectFailed, outcome: "bot_request_payload"}
	}
	c, err := b.Store.BotRequestCard(ctx, p.CardID)
	if errors.Is(err, store.ErrNoBotRequest) {
		return effectResult{state: store.EffectFailed, outcome: "bot_request_missing"}
	}
	if err != nil {
		return effectResult{deferErr: err}
	}
	if c.State != br.StateOpen || c.Baton.Bot != p.ToBot {
		// The baton moved on or the card closed before the notice ran: the
		// later handoff has its own row, and a closed card starts no work.
		return effectResult{state: store.EffectDone, outcome: "bot_request_superseded"}
	}
	n := BotRequestNotice{Bot: p.ToBot, Key: pa.BotRequestHandoffKey(p.CardID, p.Version), CardID: p.CardID, Version: p.Version,
		Kind: "handoff", Title: c.Title, Role: p.Role, Note: p.Note, From: p.From}
	return deliverBotRequestNotice(ctx, b, e, n, "bot_request_handoff_delivered")
}

func runBotRequestDoneNotice(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p store.BotRequestDonePayload
	if json.Unmarshal(e.Payload, &p) != nil || p.CardID == "" || p.Cycle <= 0 {
		return effectResult{state: store.EffectFailed, outcome: "bot_request_payload"}
	}
	outcome := "bot_request_done_delivered"
	for _, bot := range p.Bots {
		n := BotRequestNotice{Bot: bot, Key: br.NotifyKey(p.CardID, p.Cycle), CardID: p.CardID, Version: p.Cycle,
			Kind: "done", Title: p.Title, From: p.DoneBy}
		res := deliverBotRequestNotice(ctx, b, e, n, outcome)
		if res.deferErr != nil {
			// The Bots already told hold a receipt for this key; the retry
			// skips them.
			return res
		}
		if res.state == store.EffectFailed {
			outcome = "bot_request_done_partly_refused: " + res.outcome
		}
	}
	return effectResult{state: store.EffectDone, outcome: outcome}
}

func deliverBotRequestNotice(ctx context.Context, b *Broker, e store.Effect, n BotRequestNotice, outcome string) effectResult {
	last := b.now().Sub(e.CreatedAt) >= pa.CompletionWakeRetryWindow
	if b.BotRequestNotice == nil {
		if last {
			return effectResult{state: store.EffectFailed, outcome: "bot_host_unavailable"}
		}
		return effectResult{deferErr: errors.New("Bot request notice host is unavailable")}
	}
	n.LastAttempt = last
	err := b.BotRequestNotice(HostContext(ctx), n)
	if err == nil {
		return effectResult{state: store.EffectDone, outcome: outcome}
	}
	code := pa.Code(err)
	if AssistantWorkRefused(code) || last {
		return effectResult{state: store.EffectFailed, outcome: string(code)}
	}
	return effectResult{deferErr: err}
}

// runBotRequestDonePush tells the person once that a card is done.
func runBotRequestDonePush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p store.BotRequestDonePayload
	if json.Unmarshal(e.Payload, &p) != nil || p.CardID == "" || p.Cycle <= 0 {
		return effectResult{state: store.EffectFailed, outcome: "bot_request_payload"}
	}
	if !p.Person {
		return effectResult{state: store.EffectDone, outcome: "no_person"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	title, body := botRequestDoneText(b.notificationLanguage(), p.Title)
	sent, failed, err := b.Push(ctx, title, body, "", br.NotifyKey(p.CardID, p.Cycle))
	payload, _ := json.Marshal(map[string]any{"card": p.CardID, "cycle": p.Cycle, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "bot_request.done.pushed", Subject: p.CardID, Payload: payload}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	}
	return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
}

func botRequestDoneText(language, title string) (string, string) {
	if title == "" {
		title = "-"
	}
	if language == "zh-Hant" {
		return "交辦卡已完成", "「" + title + "」已完成。"
	}
	return "Bot request done", "\"" + title + "\" is done."
}
