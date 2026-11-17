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

func TestBotCompletionPushRecoversOnceWithItsOwnConversation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	earlier, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := botCompletedPush{Bot: "bot", Conversation: "chat", Turn: "turn", Name: "Synthetic Bot", Tag: "bot-completed-turn"}
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
		if bot != p.Bot || conversation != p.Conversation || terminal != "" || tag != p.Tag || title != productcopy.Format("en", "bot.completed.title", nil) || body != productcopy.Format("en", "bot.completed.body", map[string]string{"name": p.Name}) {
			t.Fatalf("wrong destination %q %q %q %q", bot, conversation, terminal, tag)
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
	turn := pa.Turn{ID: "turn", State: pa.TurnSucceeded, Fence: pa.Fence{ConversationID: "chat"}}
	tx, err := s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.BotCompletionIntent(ctx, tx, bot, turn); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	subject := "bot-completed-machine-bot-turn"
	effects, err := s.Effects(ctx, EffectBotCompletedPush, subject)
	if err != nil || len(effects) != 0 {
		t.Fatalf("rollback %+v %v", effects, err)
	}
	tx, err = s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.BotCompletionIntent(ctx, tx, bot, turn); err != nil {
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
	if json.Unmarshal(effects[0].Payload, &payload) != nil || payload.Bot != "bot" || payload.Conversation != "chat" || payload.Turn != "turn" {
		t.Fatalf("payload %+v", payload)
	}
}
