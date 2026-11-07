package antigravitysession

import (
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// TurnEndMarker is the line a Gemini conversation is asked to end its final
// message with. The installed runtime often writes the whole reply and then
// never answers session/prompt, so the conversation reads as working with
// nothing running (seven times in three hours on 2026-11-07). The model knows
// it has finished; the marker lets the host end the turn itself.
const TurnEndMarker = "[[clawdline:turn-end]]"

// TurnEndInstruction is added to every new owned conversation's launch
// context, and the transparency page lists it (gemini-turn-end).
const TurnEndInstruction = "Clawdline shows this conversation as working until the runtime reports that your turn has ended, and the runtime does not always report it. When you have finished the turn, meaning you will not call another tool or write anything more until the person replies, end your final message with this exact line: " + TurnEndMarker + "\nNever write it before a tool call, while a command you started is still running, or anywhere else. Clawdline removes the line from what the person sees."

// DefaultTurnEndGrace is how long the host waits, with no tool running and
// no frame arriving, after a marked final message before ending the turn.
// A turn the runtime does answer is answered within milliseconds of its last
// usage_update; a model that wrote the marker and goes on calls a tool within
// seconds, which clears the mark.
const DefaultTurnEndGrace = 15 * time.Second

// stripTurnEnd removes the marker line from an assistant entry and reports
// whether it ends the text. The marker may arrive split across chunks; the
// whole marker is matched in the accumulated text. Text after it means the
// model went on, so it does not mark the end.
func stripTurnEnd(text string) (string, bool) {
	i := strings.LastIndex(text, TurnEndMarker)
	if i < 0 {
		return text, false
	}
	before := strings.TrimRight(text[:i], " \t\r\n")
	after := strings.TrimLeft(text[i+len(TurnEndMarker):], " \t\r\n")
	if after == "" {
		return before, true
	}
	return before + "\n" + after, false
}

// noteTurnEnd runs under c.mu for each live assistant chunk after its text
// was accumulated. Only a marker with nothing but whitespace after it marks
// the end; anything the model does next clears it.
func (c *conversation) noteTurnEnd(found bool, chunk string) {
	if c.replaying || c.row.State != session.StateWorking {
		return
	}
	if found {
		c.endMarked = true
		return
	}
	if strings.TrimSpace(chunk) != "" {
		c.endMarked = false
	}
}

// noteReplyEnd follows the live update kinds of a running turn. Every one of
// the seven turns the runtime never answered on 2026-11-07 ended with
// assistant text then a usage_update; a step that ends in a tool call reports
// the tool, and a usage_update after text is how a generation step closes.
func (c *conversation) noteReplyEnd(kind string) {
	switch kind {
	case "usage_update":
		if c.lastKind == "agent_message_chunk" {
			c.replyEnded = true
		}
	case "agent_message_chunk":
		c.replyEnded = false
		c.lastKind = kind
	default:
		c.replyEnded = false
		c.lastKind = kind
	}
}
