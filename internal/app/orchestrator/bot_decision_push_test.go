package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type sentPush struct{ bot, conversation, decision, title, body, terminal, tag string }

func decisionBroker(t *testing.T, s *store.Store, dir string, sent *[]sentPush) *Broker {
	return &Broker{Store: s, Dir: dir, Push: func(ctx context.Context, title, body, terminal, tag string) (int, int, error) {
		bot, conversation := adapterpush.BotDestinationFromContext(ctx)
		*sent = append(*sent, sentPush{bot, conversation, adapterpush.BotDecisionFromContext(ctx), title, body, terminal, tag})
		return 1, 0, nil
	}}
}

func TestABotDecisionPushesOnceAndNotAgainAfterARestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "xavia"}, Name: "Xavia"}
	offer := pa.PermissionOffer{ID: "decision-1", State: pa.PermissionPending, Kind: pa.DecisionOfferKind,
		Decision: &pa.DecisionRequest{Question: "Merge and deploy this scope now? token=abc123def456 at /Users/someone/secret/plan.md\nDetails follow"}}
	tx, err := s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sent []sentPush
	if err = decisionBroker(t, s, dir, &sent).BotDecisionIntent(ctx, tx, bot, "chat", offer); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	s.Close()

	later, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	b := decisionBroker(t, later, dir, &sent)
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("recovery settled %d", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 || len(sent) != 1 {
		t.Fatalf("a restart sent again: %d effects, %d pushes", n, len(sent))
	}
	got := sent[0]
	if got.tag != BotDecisionTag("m", "xavia", "decision-1") || got.bot != "xavia" || got.conversation != "chat" || got.decision != "decision-1" || got.terminal != "" {
		t.Fatalf("wrong destination %+v", got)
	}
	if got.title != "Xavia needs your decision" || !strings.HasPrefix(got.body, "Merge and deploy this scope now?") {
		t.Fatalf("wrong words %+v", got)
	}
	for _, leaked := range []string{"abc123def456", "/Users", "secret/plan", "Details follow"} {
		if strings.Contains(got.body, leaked) {
			t.Fatalf("body leaks %q: %q", leaked, got.body)
		}
	}
	t.Logf("push payload: title=%q body=%q tag=%q bot=%q conversation=%q decision=%q", got.title, got.body, got.tag, got.bot, got.conversation, got.decision)
}

func TestMemoryProposalNeverPushesThePerson(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bot := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "b"}}
	offer := pa.PermissionOffer{ID: "proposal", State: pa.PermissionPending, Kind: pa.DecisionOfferKind,
		Decision: &pa.DecisionRequest{Question: pa.MemoryProposalQuestion, Memory: "Use the accessible template."}}
	var sent []sentPush
	b := decisionBroker(t, s, dir, &sent)
	tx, err := s.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = b.BotDecisionIntent(ctx, tx, bot, "chat", offer); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := b.RecoverEffects(ctx); n != 0 || len(sent) != 0 {
		t.Fatalf("proposal enqueued %d effects and sent %d pushes", n, len(sent))
	}

	// Also suppress an effect queued by a daemon that treated proposals as questions.
	b.BotPermission = func(context.Context, pa.BotKey, string) (pa.PermissionOffer, error) { return offer, nil }
	payload, _ := json.Marshal(botDecisionPush{Machine: "m", Bot: "b", Conversation: "chat", Permission: offer.ID, Name: "Bot", Tag: "tag"})
	if r := runBotDecisionPush(ctx, b, store.Effect{Payload: payload}); r.outcome != "memory_panel" || len(sent) != 0 {
		t.Fatalf("legacy proposal effect = %+v, sent %d pushes", r, len(sent))
	}
}

func TestADecisionAnsweredBeforeItsPushIsNotPushed(t *testing.T) {
	ctx := context.Background()
	p := botDecisionPush{Machine: "m", Bot: "b", Conversation: "c", Permission: "decision-2", Name: "Bot", Summary: "Ship?", Tag: BotDecisionTag("m", "b", "decision-2")}
	payload, _ := json.Marshal(p)
	var sent []sentPush
	b := decisionBroker(t, nil, "", &sent)
	b.BotPermission = func(_ context.Context, key pa.BotKey, id string) (pa.PermissionOffer, error) {
		if key.BotID != "b" || id != "decision-2" {
			t.Fatalf("read %v %s", key, id)
		}
		return pa.PermissionOffer{ID: id, State: pa.PermissionResolved}, nil
	}
	if r := runBotDecisionPush(ctx, b, store.Effect{Payload: payload}); r.outcome != "already_closed" || len(sent) != 0 {
		t.Fatalf("an answered question pushed: %+v %d", r, len(sent))
	}
	b.BotPermission = nil
	b.PushEnabled = func(key string) bool { return key != "push_on_waiting" }
	if r := runBotDecisionPush(ctx, b, store.Effect{Payload: payload}); r.outcome != "disabled_by_user" || len(sent) != 0 {
		t.Fatalf("muted push sent: %+v", r)
	}
}

func TestAToolPermissionPushHasNoQuestionText(t *testing.T) {
	p := botDecisionPush{Machine: "m", Bot: "b", Conversation: "c", Permission: "perm-1", Name: "Atlas", Tag: "t"}
	payload, _ := json.Marshal(p)
	var sent []sentPush
	if r := runBotDecisionPush(context.Background(), decisionBroker(t, nil, "", &sent), store.Effect{Payload: payload}); r.outcome != "pushed" || len(sent) != 1 || sent[0].body != "Atlas is waiting for your permission. Open the chat to answer." {
		t.Fatalf("%+v %+v", r, sent)
	}
}

func TestADecisionSummaryIsCutToItsBound(t *testing.T) {
	long := strings.Repeat("決定", MaxBotDecisionPushRunes)
	if got := []rune(BotDecisionSummary(long)); len(got) != MaxBotDecisionPushRunes || got[len(got)-1] != '…' {
		t.Fatalf("summary is %d runes", len(got))
	}
	if got := BotDecisionSummary("Use ghp_abcdefghijklmnop or sk-live_1234567890 now"); strings.Contains(got, "ghp_") || strings.Contains(got, "sk-") {
		t.Fatalf("credential kept: %q", got)
	}
}
