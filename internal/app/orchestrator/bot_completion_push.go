package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// EffectBotCompletedPush tells the person about one Bot message addressed to
// them: the reply to their own message, or a notify_person message. The kind
// keeps its earlier name so queued effects stay in one outbox lane.
const EffectBotCompletedPush = "bot.completed.push"

type botCompletedPush struct {
	Bot          string `json:"bot"`
	Conversation string `json:"conversation"`
	Turn         string `json:"turn,omitempty"`
	// Message is the message the tap opens the chat at; an effect without
	// one was queued for a plain completion before pushes followed unread.
	Message string `json:"message,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
	Name    string `json:"name"`
	Preview string `json:"preview,omitempty"`
	Tag     string `json:"tag"`
}

// BotMessageTag is the push tag of one Bot message addressed to the person.
func BotMessageTag(machine, bot, message string) string {
	return "bot-message-" + machine + "-" + bot + "-" + message
}

// BotPersonMessageIntent is called inside the Bot commit that first records a
// message addressed to the person (pa.CountsAsUnread), never after it. The
// store calls it once per such message, so a replayed receipt cannot enqueue
// a second push. A routine, a request from another Bot, a background helper
// or a work result reaches here only through notify_person.
func (b *Broker) BotPersonMessageIntent(ctx context.Context, tx *sql.Tx, bot pa.Bot, msg pa.Message, reply bool) error {
	if bot.Notify == "off" || bot.Hidden || msg.ID == "" || msg.Fence.ConversationID == "" || !pa.CountsAsUnread(msg, bot.Key) {
		return nil
	}
	preview := BotDecisionSummary
	if reply {
		preview = botReplyPushPreview
	}
	p := botCompletedPush{Bot: bot.Key.BotID, Conversation: msg.Fence.ConversationID, Turn: msg.TurnID, Message: msg.ID, Seq: msg.Seq,
		Name: bot.Name, Preview: preview(msg.Text), Tag: BotMessageTag(bot.Key.MachineID, bot.Key.BotID, msg.ID)}
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return b.Store.AppendEffectTx(ctx, tx, store.Effect{Kind: EffectBotCompletedPush, Subject: p.Tag, Payload: payload})
}

var botReplyListMarker = regexp.MustCompile(`^(?:[-+*•]|[0-9]+[.)])[ 	]+`)

// botReplyPushPreview keeps a short opening intact and fills in a lead-in
// ending in a colon with the paragraph or list it introduces.
func botReplyPushPreview(text string) string {
	lead, rest, _ := strings.Cut(strings.TrimSpace(text), "\n")
	lead = strings.TrimSpace(lead)
	separator := " "
	switch {
	case strings.HasSuffix(lead, "："):
		separator = ""
	case !strings.HasSuffix(lead, ":"):
		return botPushPreview(lead)
	}

	var preview strings.Builder
	preview.WriteString(lead)
	inList := false
	for line := range strings.SplitSeq(strings.TrimSpace(rest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			break
		}
		item := botReplyListMarker.ReplaceAllString(line, "")
		if inList && item == line {
			break
		}
		preview.WriteString(separator)
		preview.WriteString(item)
		if item == line {
			break
		}
		inList = true
		separator = " · "
	}
	if preview.Len() == len(lead) {
		return botPushPreview(strings.TrimRight(lead, ":："))
	}
	return botPushPreview(preview.String())
}

func runBotCompletedPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p botCompletedPush
	if json.Unmarshal(e.Payload, &p) != nil || p.Bot == "" || p.Conversation == "" || p.Tag == "" || p.Name == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no Bot message"}
	}
	if p.Message == "" {
		// Queued by an earlier build for a completion that named no message:
		// the person may find nothing new where it leads.
		return effectResult{state: store.EffectDone, outcome: "superseded_no_message"}
	}
	if b.PushEnabled != nil && !b.PushEnabled("push_on_bot_done") {
		return effectResult{state: store.EffectDone, outcome: "disabled_by_user"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	language := b.notificationLanguage()
	title := productcopy.Format(language, "bot.message.title", map[string]string{"name": p.Name})
	body := productcopy.Format(language, "bot.message.empty", map[string]string{"name": p.Name})
	if p.Preview != "" {
		body = productcopy.Format(language, "bot.message.body", map[string]string{"preview": p.Preview})
	}
	target := adapterpush.WithBotMessage(adapterpush.WithBotDestination(ctx, p.Bot, p.Conversation), p.Message)
	sent, failed, err := b.Push(target, title, body, "", p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "bot": p.Bot, "turn": p.Turn, "message": p.Message, "sent": sent, "failed": failed})
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
