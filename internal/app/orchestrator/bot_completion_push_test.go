package orchestrator

import (
	"context"
	"encoding/json"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"testing"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// The push names the Bot, previews the message addressed to the person and
// leads to that message, so the tap never lands on a chat with nothing new.
func TestBotMessagePushRecoversOnceAndLeadsToItsMessage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	earlier, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := botCompletedPush{Bot: "bot", Conversation: "chat", Turn: "turn", Message: "reply-turn", Seq: 2, Name: "Synthetic Bot", Preview: "The report is ready", Tag: "bot-message-machine-bot-reply-turn"}
	payload, _ := json.Marshal(p)
	_, err = earlier.RecordIntent(ctx, nil, []store.Effect{{Kind: EffectBotCompletedPush, Subject: p.Tag, Payload: payload}})
	if err != nil {
		t.Fatal(err)
	}
	earlier.Close()
	later, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	pushes := 0
	b := &Broker{Store: later, Dir: dir, Push: func(ctx context.Context, title, body, terminal, tag string) (int, int, error) {
		pushes++
		bot, conversation := adapterpush.BotDestinationFromContext(ctx)
		if bot != p.Bot || conversation != p.Conversation || adapterpush.BotMessageFromContext(ctx) != p.Message || terminal != "" || tag != p.Tag {
			t.Fatalf("wrong destination %q %q %q %q", bot, conversation, terminal, tag)
		}
		if title != productcopy.Format("en", "bot.message.title", map[string]string{"name": p.Name}) || title != "Synthetic Bot" || body != "The report is ready" {
			t.Fatalf("push reads %q / %q", title, body)
		}
		return 1, 0, nil
	}}
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("recovery settled %d", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 || pushes != 1 {
		t.Fatalf("replayed %d / %d", n, pushes)
	}
	effects, _ := later.Effects(ctx, EffectBotCompletedPush, p.Tag)
	if len(effects) != 1 || effects[0].Outcome != "pushed" {
		t.Fatalf("effects %+v", effects)
	}
	b.PushEnabled = func(key string) bool {
		if key != "push_on_bot_done" {
			t.Fatalf("wrong preference %s", key)
		}
		return false
	}
	if got := runBotCompletedPush(ctx, b, store.Effect{Payload: payload}); got.outcome != "disabled_by_user" || pushes != 1 {
		t.Fatalf("mute %+v / %d", got, pushes)
	}
}

// An effect an earlier build queued for a plain completion names no message;
// it is settled without a push rather than leading to nothing.
func TestAQueuedCompletionWithoutAMessageDoesNotPush(t *testing.T) {
	payload, _ := json.Marshal(botCompletedPush{Bot: "bot", Conversation: "chat", Turn: "turn", Name: "Synthetic Bot", Tag: "bot-completed-machine-bot-turn"})
	pushes := 0
	b := &Broker{Push: func(context.Context, string, string, string, string) (int, int, error) { pushes++; return 1, 0, nil }}
	if got := runBotCompletedPush(context.Background(), b, store.Effect{Payload: payload}); got.outcome != "superseded_no_message" || pushes != 0 {
		t.Fatalf("legacy completion %+v / %d", got, pushes)
	}
}

// Only a message addressed to the person is pushed: a routine's or another
// Bot's reply, a background report and a work result are not.
func TestOnlyAMessageForThePersonEnqueuesAPush(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Broker{Store: s, Dir: dir}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Name: "Synthetic Bot"}
	f := pa.Fence{Key: bot.Key, ConversationID: "chat"}
	cases := []struct {
		msg  pa.Message
		push bool
	}{
		{pa.Message{ID: "routine-reply", Fence: f, Kind: pa.AssistantMessage, Text: "Checked", ForPerson: pa.ForPersonFlag(false)}, false},
		{pa.Message{ID: "work", Fence: f, Kind: pa.WorkMessage, Text: "Done", ForPerson: pa.ForPersonFlag(false)}, false},
		{pa.Message{ID: "mine", Fence: f, Kind: pa.UserMessage, Text: "Hi", ForPerson: pa.ForPersonFlag(true)}, false},
		{pa.Message{ID: "notice", Fence: f, Kind: pa.AssistantMessage, Text: "Deployment failed; needs you", ForPerson: pa.ForPersonFlag(true)}, true},
	}
	tx, err := s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if err = b.BotPersonMessageIntent(ctx, tx, bot, c.msg); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		effects, err := s.Effects(ctx, EffectBotCompletedPush, BotMessageTag("machine", "bot", c.msg.ID))
		if err != nil || (len(effects) == 1) != c.push || len(effects) > 1 {
			t.Fatalf("%s: %d effects (want push=%v) %v", c.msg.ID, len(effects), c.push, err)
		}
	}
	effects, _ := s.Effects(ctx, EffectBotCompletedPush, BotMessageTag("machine", "bot", "notice"))
	var p botCompletedPush
	if json.Unmarshal(effects[0].Payload, &p) != nil || p.Preview != "Deployment failed; needs you" || p.Message != "notice" {
		t.Fatalf("payload %+v", p)
	}
}

func TestBotCompletionIntentSharesThePrimaryTransaction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Broker{Store: s, Dir: dir}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Name: "Synthetic Bot"}
	reply := pa.Message{ID: "reply-turn", TurnID: "turn", Seq: 2, Kind: pa.AssistantMessage, Text: "Done", Fence: pa.Fence{ConversationID: "chat"}, ForPerson: pa.ForPersonFlag(true)}
	tx, err := s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.BotPersonMessageIntent(ctx, tx, bot, reply); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	subject := BotMessageTag("machine", "bot", "reply-turn")
	effects, err := s.Effects(ctx, EffectBotCompletedPush, subject)
	if err != nil || len(effects) != 0 {
		t.Fatalf("rollback %+v %v", effects, err)
	}
	tx, err = s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.BotPersonMessageIntent(ctx, tx, bot, reply); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	effects, err = s.Effects(ctx, EffectBotCompletedPush, subject)
	if err != nil || len(effects) != 1 {
		t.Fatalf("commit %+v %v", effects, err)
	}
	var payload botCompletedPush
	if json.Unmarshal(effects[0].Payload, &payload) != nil || payload.Bot != "bot" || payload.Conversation != "chat" || payload.Turn != "turn" || payload.Message != "reply-turn" || payload.Seq != 2 {
		t.Fatalf("payload %+v", payload)
	}
}
