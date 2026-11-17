package projectassistant

import (
	"strings"
	"testing"
)

func TestReactionEmojiMatchesPinnedValidation(t *testing.T) {
	for _, value := range []string{"👍", "❤️", "👨‍👩‍👧‍👦", "🎉✅", "!"} {
		if got, e := ReactionEmoji(" " + value + " "); e != nil || got != value {
			t.Fatal(value, got, e)
		}
	}
	for _, value := range []string{"", " ", "ok", "1️⃣", "👍 👍", "\u0345", strings.Repeat("👍", 9), string([]byte{0xff})} {
		if _, e := ReactionEmoji(value); Code(e) != Invalid {
			t.Fatal("invalid emoji accepted", value, e)
		}
	}
}
