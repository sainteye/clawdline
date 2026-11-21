package projectassistant

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A person picks a Bot from the composer's @ list (or a routine instruction's)
// and the console stores the choice inline as @[Name](bot:ID). The name is only
// what the person saw; the ID is what message_bot needs.
var botMention = regexp.MustCompile(`@\[([^\]\n]+)\]\(bot:([^)\s]+)\)`)

type BotMention struct {
	Name  string `json:"name"`
	BotID string `json:"target_bot_id"`
}

// BotMentions returns each distinct mentioned Bot once, in first-mention order.
func BotMentions(text string) []BotMention {
	var out []BotMention
	seen := map[string]bool{}
	for _, m := range botMention.FindAllStringSubmatch(text, -1) {
		if seen[m[2]] {
			continue
		}
		seen[m[2]] = true
		out = append(out, BotMention{Name: m[1], BotID: m[2]})
	}
	return out
}

// WithBotMentionHint tells the model which Bots the person picked. It grants
// nothing: message_bot still checks that the target may be contacted.
func WithBotMentionHint(text string) string {
	mentions := BotMentions(text)
	if len(mentions) == 0 {
		return text
	}
	data, _ := json.Marshal(mentions)
	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\n[Clawdline: @[Name](bot:ID) marks a Bot the person picked. To involve it, call message_bot with that target_bot_id and a self-contained request; its reply arrives later in this chat. Mentioned Bots (JSON): ")
	b.Write(data)
	b.WriteString("]")
	return b.String()
}
