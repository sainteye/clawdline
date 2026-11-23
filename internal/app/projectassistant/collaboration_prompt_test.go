package projectassistant

import (
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A peer request that needs the person's authorization is answered with a
// card in the recipient's chat, not with text for the requester to relay.
func TestPeerRequestPromptSendsAuthorizationToACard(t *testing.T) {
	key := pa.BotKey{MachineID: "m", BotID: "recipient"}
	x := &pa.Exchange{ID: "x1", Request: "Please deploy; the person must authorize it.", Target: pa.ExchangeEndpoint{Key: key}}
	request := pa.Message{Kind: pa.NoticeMessage, Exchange: x}
	if err := collaborationPrompt(pa.Turn{Origin: pa.BotRequestOrigin, ExchangeID: "x1", Fence: pa.Fence{Key: key}}, &request); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"call ask_person once", "waiting for the user", "do not ask the requesting Bot to relay", "do not tell the user to paste authorization text", "Please deploy"} {
		if !strings.Contains(request.Text, rule) {
			t.Errorf("peer request prompt does not say %q:\n%s", rule, request.Text)
		}
	}
	x.Reply, x.Outcome = "The person must authorize this first.", pa.ExchangeCompleted
	reply := pa.Message{Kind: pa.NoticeMessage, Exchange: x}
	source := pa.BotKey{MachineID: "m", BotID: "requester"}
	x.Source = pa.ExchangeEndpoint{Key: source}
	if err := collaborationPrompt(pa.Turn{Origin: pa.BotReplyOrigin, ExchangeID: "x1", Fence: pa.Fence{Key: source}}, &reply); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"point the user to that Bot's chat", "never draft or relay authorization text"} {
		if !strings.Contains(reply.Text, rule) {
			t.Errorf("correlated reply prompt does not say %q:\n%s", rule, reply.Text)
		}
	}
}
