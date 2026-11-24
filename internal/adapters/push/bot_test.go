package push

import (
	"context"
	"net/url"
	"strings"
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

func TestAQuietReplacementIsSilentOnEveryEnvelope(t *testing.T) {
	b := newBrowser(t)
	n := Notification{Title: "Xavia no longer needs your decision", Body: "Nothing more is needed here.", URL: "/#bot=x&conversation=c", Tag: "bot-decision-m-x-d", Silent: QuietFromContext(WithQuiet(context.Background()))}
	apple := b.subscription("a", "https://web.push.apple.com/one", "phone")
	apple.Origin = "https://console.example"
	for _, s := range []Subscription{apple, b.subscription("c", "https://fcm.googleapis.com/one", "laptop")} {
		message, _, ok := Build(n, s)
		if !ok || !strings.Contains(string(message.Plaintext), `"silent":true`) || !strings.Contains(string(message.Plaintext), Topic(n.Tag)) {
			t.Fatalf("%s: %s", s.Endpoint, message.Plaintext)
		}
		t.Logf("%s payload: %s", message.ContentType, message.Plaintext)
	}
	if QuietFromContext(context.Background()) {
		t.Fatal("an ordinary push is quiet")
	}
}
