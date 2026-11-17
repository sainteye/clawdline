package projectassistant

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxHumanReactions               = 8
	MaxReactionEmojiBytes           = 32
	ReactOperation        Operation = "react"
	ReactionsChanged      EventKind = "reactions_changed"
)

// ReactionActor is metadata, never an execution or human authentication grant.
// Empty emoji lists retain their revision so a removed reaction cannot reset CAS.
type ReactionActor struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Bot        *BotKey     `json:"bot,omitempty"`
	Name       string      `json:"name"`
	Appearance *Appearance `json:"appearance,omitempty"`
	Revision   int64       `json:"revision"`
	Emojis     []string    `json:"emojis"`
	Own        bool        `json:"own,omitempty"`
}

type ReactionRecord struct {
	Fence     Fence         `json:"fence"`
	MessageID string        `json:"message_id"`
	Actor     ReactionActor `json:"actor"`
}

// HumanReactionID is derived only from an authenticated reader, never wire input.
func HumanReactionID(reader string) string {
	h := sha256.Sum256([]byte("clawdline-reaction-human:" + reader))
	return "human-" + hex.EncodeToString(h[:])
}

func ReactionEmoji(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxReactionEmojiBytes || !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.Is(unicode.Other_Alphabetic, r) || unicode.IsNumber(r) || unicode.IsSpace(r)
	}) {
		return "", fail(Invalid, "reaction_emoji")
	}
	return value, nil
}

func Reactable(m Message) bool { return m.Kind == UserMessage || m.Kind == AssistantMessage }
