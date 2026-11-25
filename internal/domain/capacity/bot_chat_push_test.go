package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// While the live roster stream is connected, a Bot chat reads its events when
// that stream reports a change and keeps only a slow fallback timer. The
// fallback interval lives in chat-push.ts and must match the registered bound.
func TestBotChatPushedFallbackIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/project-assistants/chat-push.ts"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`export const PushedEventFallbackMs = ([0-9]+)`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("pushed chat fallback interval missing")
	}
	limit, err := strconv.ParseInt(string(match[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range Register() {
		if row.Name == "console.bot_chat_pushed_fallback_milliseconds" {
			if row.Limit != limit || row.Unit != Milliseconds {
				t.Fatal(row)
			}
			return
		}
	}
	t.Fatal("pushed chat fallback interval is not registered")
}
