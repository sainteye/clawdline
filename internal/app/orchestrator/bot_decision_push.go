package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/productcopy"
)

const EffectBotDecisionPush = "bot.decision.push"

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
func (b *Broker) BotDecisionIntent(ctx context.Context, tx *sql.Tx, bot pa.Bot, conversation string, offer pa.PermissionOffer) error {
	if offer.State != pa.PermissionPending || offer.ID == "" || conversation == "" {
		return nil
	}
	p := botDecisionPush{Machine: bot.Key.MachineID, Bot: bot.Key.BotID, Conversation: conversation, Permission: offer.ID,
		Name: bot.Name, Tag: BotDecisionTag(bot.Key.MachineID, bot.Key.BotID, offer.ID)}
	if offer.Decision != nil {
		p.Summary = BotDecisionSummary(offer.Decision.Question)
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return b.Store.AppendEffectTx(ctx, tx, store.Effect{Kind: EffectBotDecisionPush, Subject: p.Tag, Payload: payload})
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
		if err == nil && offer.State != pa.PermissionPending {
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
