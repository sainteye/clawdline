package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/productcopy"
)

const EffectBotDecisionPush = "bot.decision.push"

// EffectBotDecisionRetract quietly replaces a decision's notification once the
// question is answered, withdrawn or written past, so the lock screen stops
// asking for something nobody needs any more.
const EffectBotDecisionRetract = "bot.decision.retract"

// MaxBotDecisionPushRunes is how much of a Bot's question reaches a lock
// screen. The card in the chat holds the whole question.
const MaxBotDecisionPushRunes = 120

type botDecisionPush struct {
	Machine      string `json:"machine"`
	Bot          string `json:"bot"`
	Conversation string `json:"conversation"`
	Permission   string `json:"permission"`
	Name         string `json:"name"`
	Summary      string `json:"summary,omitempty"`
	Tag          string `json:"tag"`
}

// BotDecisionTag is the push tag, and so the push topic, of one waiting
// decision: a later notification about the same question replaces it.
func BotDecisionTag(machine, bot, permission string) string {
	return "bot-decision-" + machine + "-" + bot + "-" + permission
}

// BotDecisionIntent is called inside the Bot commit that first records a
// pending permission or ask_person question, never after it. The store calls
// it only on the transition into pending, so a replayed receipt, a later
// commit of the same offer and a daemon restart cannot enqueue a second push.
//
// The store also calls it for the commit that moves an offer out of pending;
// that records a retraction, which sends only if the push was sent.
func (b *Broker) BotDecisionIntent(ctx context.Context, tx *sql.Tx, bot pa.Bot, conversation string, offer pa.PermissionOffer) error {
	if offer.ID == "" || conversation == "" {
		return nil
	}
	kind := EffectBotDecisionPush
	if offer.State != pa.PermissionPending {
		kind = EffectBotDecisionRetract
	}
	p := botDecisionPush{Machine: bot.Key.MachineID, Bot: bot.Key.BotID, Conversation: conversation, Permission: offer.ID,
		Name: bot.Name, Tag: BotDecisionTag(bot.Key.MachineID, bot.Key.BotID, offer.ID)}
	if offer.Decision != nil && kind == EffectBotDecisionPush {
		p.Summary = BotDecisionSummary(offer.Decision.Question)
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err = b.Store.AppendEffectTx(ctx, tx, store.Effect{Kind: kind, Subject: p.Tag, Payload: payload}); err != nil {
		return err
	}
	return b.clearBotRequestWaiting(ctx, tx, bot, offer)
}

// clearBotRequestWaiting takes a Bot request card out of "Waiting for you"
// when the ask_person question it waits on closes: answered by the person,
// withdrawn by its Bot, or expired. A native permission is never waited on.
func (b *Broker) clearBotRequestWaiting(ctx context.Context, tx *sql.Tx, bot pa.Bot, offer pa.PermissionOffer) error {
	if offer.Kind != pa.DecisionOfferKind {
		return nil
	}
	by := br.Actor{Bot: bot.Key.BotID}
	switch offer.State {
	case pa.PermissionResolved:
		by = br.Actor{Person: true}
	case pa.PermissionCancelled, pa.PermissionExpired:
	default:
		return nil
	}
	return b.Store.ClearBotRequestWaitingTx(ctx, tx, offer.ID, by, b.now())
}

var (
	decisionSecretWord = regexp.MustCompile(`(?i)\b(?:sk|pk|ghp|gho|xox[abp]|glpat)[-_][A-Za-z0-9_-]{6,}|\b[A-Za-z0-9+/_=-]{24,}\b|\b[0-9a-f]{16,}\b`)
	decisionPathWord   = regexp.MustCompile(`(?:~|\.{1,2})?/[^\s/]+(?:/[^\s]*)?|[A-Za-z]:\\[^\s]*`)
	decisionSecretKV   = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|api[_-]?key|authorization|bearer)\b\s*[:=]?\s*\S+`)
)

// BotDecisionSummary is the first line of the question with anything shaped
// like a credential or a filesystem path taken out, cut to the push bound.
func BotDecisionSummary(question string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(question), "\n", 2)[0])
	line = decisionSecretKV.ReplaceAllString(line, "$1 …")
	line = decisionSecretWord.ReplaceAllString(line, "…")
	line = decisionPathWord.ReplaceAllString(line, "…")
	line = strings.Join(strings.Fields(line), " ")
	if utf8.RuneCountInString(line) > MaxBotDecisionPushRunes {
		r := []rune(line)
		line = strings.TrimSpace(string(r[:MaxBotDecisionPushRunes-1])) + "…"
	}
	return line
}

func runBotDecisionPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p botDecisionPush
	if json.Unmarshal(e.Payload, &p) != nil || p.Bot == "" || p.Conversation == "" || p.Permission == "" || p.Tag == "" || p.Name == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no waiting Bot decision"}
	}
	if b.PushEnabled != nil && !b.PushEnabled("push_on_waiting") {
		return effectResult{state: store.EffectDone, outcome: "disabled_by_user"}
	}
	// The question may have been answered, withdrawn or expired between the
	// commit and this run; a push about a closed question is noise.
	if b.BotPermission != nil {
		offer, err := b.BotPermission(ctx, pa.BotKey{MachineID: p.Machine, BotID: p.Bot}, p.Permission)
		if err == nil && (offer.State != pa.PermissionPending || !offer.ExpiresAt.IsZero() && !b.now().Before(offer.ExpiresAt)) {
			return effectResult{state: store.EffectDone, outcome: "already_closed"}
		}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	language := b.notificationLanguage()
	title := productcopy.Format(language, "bot.decision.title", map[string]string{"name": p.Name})
	body := productcopy.Format(language, "bot.decision.permission", map[string]string{"name": p.Name})
	if p.Summary != "" {
		body = productcopy.Format(language, "bot.decision.body", map[string]string{"question": p.Summary})
	}
	target := adapterpush.WithBotDecision(adapterpush.WithBotDestination(ctx, p.Bot, p.Conversation), p.Permission)
	sent, failed, err := b.Push(target, title, body, "", p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "bot": p.Bot, "permission": p.Permission, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "bot.decision.pushed", Subject: e.Subject, Payload: result}}
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

// holdBotDecisionPush is whether a pending decision push should wait for the
// next beat because a console reports its chat on screen. A wrong "on screen"
// (a tab left open on a desk) delays the push by at most
// MaxBotViewingHoldSeconds; it never drops it.
func (b *Broker) holdBotDecisionPush(e store.Effect) bool {
	if e.Kind != EffectBotDecisionPush || e.State != store.EffectPending || b.BotPresence == nil {
		return false
	}
	var p botDecisionPush
	if json.Unmarshal(e.Payload, &p) != nil {
		return false
	}
	now := b.now()
	if !e.CreatedAt.IsZero() && now.Sub(e.CreatedAt) >= MaxBotViewingHoldSeconds*time.Second {
		return false
	}
	return b.BotPresence.Viewing(p.Bot, p.Conversation, now)
}

func runBotDecisionRetract(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p botDecisionPush
	if json.Unmarshal(e.Payload, &p) != nil || p.Bot == "" || p.Conversation == "" || p.Permission == "" || p.Tag == "" || p.Name == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no closed Bot decision"}
	}
	// Only a notification that reached a push service has anything to
	// replace; a held, muted or unsubscribed push showed nothing.
	pushes, err := b.Store.Effects(ctx, EffectBotDecisionPush, p.Tag)
	if err != nil {
		return effectResult{state: store.EffectFailed, outcome: err.Error()}
	}
	shown := false
	for _, push := range pushes {
		shown = shown || push.Outcome == "pushed" || push.State == store.EffectStarted || push.State == store.EffectUnknown
	}
	if !shown {
		return effectResult{state: store.EffectDone, outcome: "never_pushed"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	language := b.notificationLanguage()
	title := productcopy.Format(language, "bot.decision.closed.title", map[string]string{"name": p.Name})
	body := productcopy.Format(language, "bot.decision.closed.body", nil)
	target := adapterpush.WithQuiet(adapterpush.WithBotDestination(ctx, p.Bot, p.Conversation))
	sent, failed, err := b.Push(target, title, body, "", p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "bot": p.Bot, "permission": p.Permission, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "bot.decision.retracted", Subject: e.Subject, Payload: result}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	default:
		return effectResult{state: store.EffectDone, outcome: "retracted", events: events}
	}
}
