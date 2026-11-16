package push

import (
	"context"
	"net/url"
	"testing"
)

func TestBotDestinationKeepsBothTargetFieldsAndMachine(t *testing.T) {
	got := BotDestinationURL("bot &一", "conversation#one", "source/one")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q, err := url.ParseQuery(u.EscapedFragment())
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("bot") != "bot &一" || q.Get("conversation") != "conversation#one" || q.Get("machine") != "source/one" || q.Has("session") {
		t.Fatalf("destination = %q", got)
	}
	if BotDestinationURL("b", "", "m") != "/" {
		t.Fatal("partial target must not open a Bot")
	}
	ctx := WithBotDestination(context.Background(), "b", "c")
	if bot, conversation := BotDestinationFromContext(ctx); bot != "b" || conversation != "c" {
		t.Fatalf("context = %q %q", bot, conversation)
	}
}
