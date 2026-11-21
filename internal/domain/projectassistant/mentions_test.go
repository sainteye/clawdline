package projectassistant

import (
	"strings"
	"testing"
)

func TestBotMentionsKeepPickedIDsOnceInOrder(t *testing.T) {
	text := "請 @[Lynn](bot:b-2) 查信，再問 @[研究員 A](bot:b-9)，@[Lynn](bot:b-2) 優先；@Lynn 與 @[x](user:1) 不算"
	got := BotMentions(text)
	if len(got) != 2 || got[0] != (BotMention{"Lynn", "b-2"}) || got[1] != (BotMention{"研究員 A", "b-9"}) {
		t.Fatalf("%+v", got)
	}
	hint := WithBotMentionHint(text)
	if !strings.HasPrefix(hint, text) || !strings.Contains(hint, `"target_bot_id":"b-2"`) || !strings.Contains(hint, "message_bot") {
		t.Fatal(hint)
	}
	if WithBotMentionHint("沒有提及") != "沒有提及" {
		t.Fatal("text without mentions changed")
	}
}
