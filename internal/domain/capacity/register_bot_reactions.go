package capacity

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

func reactionBounds() []Entry {
	return []Entry{
		{Name: "bots.human_reactions", Class: Buffer, Unit: Rows, Limit: pa.MaxHumanReactions, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxHumanReactions"}},
		{Name: "bots.reaction_emoji_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxReactionEmojiBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxReactionEmojiBytes"}},
	}
}
