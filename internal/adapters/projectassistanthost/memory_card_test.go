package projectassistanthost

import (
	"context"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type memoryCardTools interface {
	MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	EditMemory(context.Context, string, app.MemoryEdit) error
	MemoryNeedsPerson() bool
}

func liveMemory(t *testing.T, f *hostFixture, key pa.BotKey) map[string]pa.MemoryEntry {
	t.Helper()
	x, err := f.h.store.Export(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]pa.MemoryEntry{}
	for _, e := range x.MemoryEntries {
		if !e.Forgotten {
			out[e.Text] = e
		}
	}
	return out
}

// A Bot asked by another Bot to remember something (Atlas asking Lens and
// Fuse to record an acceptance case) gets a memory card in its own main chat;
// the person's Save writes the literal fact, Don't save writes nothing.
func TestPeerRequestMemoryBecomesACardThePersonSaves(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "atlas"), f.create(t, "lens")
	if _, err := f.send(t, a, "Ask Lens to remember the acceptance case."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := sender.q.Tools.(memoryCardTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Please remember the acceptance case.", IdempotencyKey: "remember"})
	if err != nil {
		t.Fatal(err)
	}
	sender.reply <- app.PromptResult{FinalText: "Asked Lens."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, a.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	if peer := fixtureReceive(t, f.p.sent); peer.Turn.Origin != pa.BotRequestOrigin {
		t.Fatalf("unexpected worker prompt: %+v", peer.Turn)
	}
	tools := worker.q.Tools.(memoryCardTools)
	const keep, drop = "Acceptance covers only the observed peer-memory case.", "A fact the person declines."
	for i, text := range []string{keep, drop} {
		if err = tools.EditMemory(ctx, "card-"+string(rune('a'+i)), app.MemoryEdit{Append: text}); err != nil {
			t.Fatalf("edit_memory in a peer request = %v (%s), want a memory card", err, pa.Code(err))
		}
	}
	if !tools.MemoryNeedsPerson() {
		t.Fatal("the peer turn's edit_memory does not tell the Bot it waits for the person")
	}
	if err = tools.EditMemory(ctx, "card-old", app.MemoryEdit{Old: "Acceptance", New: "Changed"}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a peer turn rewrote memory: %v", err)
	}
	if got := liveMemory(t, f, b.Key); len(got) != 0 {
		t.Fatalf("peer text reached memory without the person: %+v", got)
	}
	cards := map[string]pa.PermissionOffer{}
	for _, p := range decisionOffers(t, f, b.Key) {
		if p.Decision.Memory != "" {
			cards[p.Decision.Memory] = p
		}
	}
	if len(cards) != 2 || cards[keep].State != pa.PermissionPending || cards[keep].Decision.ExchangeID != "" || cards[keep].ExpiresAt.IsZero() {
		t.Fatalf("Lens's main chat has no pending memory cards: %+v", cards)
	}
	// The card does not hold the exchange: Atlas still gets Lens's reply.
	worker.reply <- app.PromptResult{FinalText: "Proposed the fact for the person to save."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Exchange(ctx, a.Key, x.ID)
		return err == nil && got.Outcome == pa.ExchangeCompleted
	})
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	press := func(key string, card pa.PermissionOffer, option string) error {
		_, e := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: key}, pa.PermissionAnswer{Fence: card.Fence, OfferID: card.ID, OptionID: option})
		return e
	}
	if err = press("typed", cards[keep], ""); err == nil {
		t.Fatal("a memory card accepted an answer that is neither Save nor Don't save")
	}
	if err = press("save", cards[keep], pa.MemorySaveOptionID); err != nil {
		t.Fatal(err)
	}
	if err = press("skip", cards[drop], pa.MemorySkipOptionID); err != nil {
		t.Fatal(err)
	}
	got := liveMemory(t, f, b.Key)
	saved, ok := got[keep]
	if len(got) != 1 || !ok || len(saved.Sources) != 1 || saved.Sources[0].Kind != "human" {
		t.Fatalf("memory after Save and Don't save = %+v", got)
	}
	if err = press("save-again", cards[keep], pa.MemorySkipOptionID); err == nil {
		t.Fatal("a memory card was answered twice")
	}
	for _, p := range decisionOffers(t, f, b.Key) {
		if p.State != pa.PermissionResolved {
			t.Fatalf("card left pending: %+v", p)
		}
	}
	t.Logf("peer edit_memory -> card; Save wrote %q as the person's entry; Don't save wrote nothing", keep)
}
