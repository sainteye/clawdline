package projectassistanthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type fakeTurns struct {
	turns     map[string]pa.Turn
	exchanges map[string]pa.Exchange
}

func (f fakeTurns) Turn(_ context.Context, _ pa.BotKey, id string) (pa.Turn, error) {
	t, ok := f.turns[id]
	if !ok {
		return pa.Turn{}, failure(pa.NotFound, "turn")
	}
	return t, nil
}
func (f fakeTurns) Exchange(_ context.Context, _ pa.BotKey, id string) (pa.Exchange, error) {
	x, ok := f.exchanges[id]
	if !ok {
		return pa.Exchange{}, failure(pa.NotFound, "exchange")
	}
	return x, nil
}

var brNow = time.UnixMilli(1_795_500_000_000)

func brBot(id string) pa.Bot {
	return pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: id}, Project: pa.ProjectScope{MachineID: "m", ProjectID: "p"}}
}

func brCall(t *testing.T, s *store.Store, look fakeTurns, turn botRequestTurn, name string, args any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return botRequestTool(context.Background(), s, look, turn, "m", "v-1", brNow, name, raw)
}

func brOpen(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBotRequestCreateNeedsAPersonRootedTurn(t *testing.T) {
	s := brOpen(t)
	look := fakeTurns{turns: map[string]pa.Turn{}, exchanges: map[string]pa.Exchange{}}
	create := map[string]any{"idempotency_key": "k", "title": "Ship it", "kind": "development"}
	for _, origin := range []pa.TurnOrigin{pa.HostNoticeOrigin, pa.CompactOrigin, pa.BotReplyOrigin, "routine"} {
		turn := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t1", MessageID: "msg", Origin: origin}}
		if _, err := brCall(t, s, look, turn, "bot_request_create", create); br.CodeOf(err) != br.CodeNoAuthority {
			t.Fatalf("origin %q: got %v, want %s", origin, err, br.CodeNoAuthority)
		}
	}
	routine := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t1", MessageID: "msg", Origin: pa.UserOrigin, RoutineRunID: "r"}}
	if _, err := brCall(t, s, look, routine, "bot_request_create", create); br.CodeOf(err) != br.CodeNoAuthority {
		t.Fatalf("routine run: got %v", err)
	}
	network := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t1", MessageID: "msg", Origin: pa.UserOrigin, Network: &pa.NetworkWork{}}}
	if _, err := brCall(t, s, look, network, "bot_request_create", create); br.CodeOf(err) != br.CodeNoAuthority {
		t.Fatalf("network turn: got %v", err)
	}
	// A Bot-to-Bot request whose chain ends in a routine roots in no person.
	look.turns["src"] = pa.Turn{ID: "src", MessageID: "m0", Origin: pa.HostNoticeOrigin}
	look.exchanges["x1"] = pa.Exchange{ID: "x1", Source: pa.ExchangeEndpoint{Key: brBot("lead").Key, ConversationID: "c"}, SourceTurnID: "src"}
	peer := botRequestTurn{Bot: brBot("coo"), Conversation: "c2", Turn: pa.Turn{ID: "t2", MessageID: "m2", Origin: pa.BotRequestOrigin, ExchangeID: "x1"}}
	if _, err := brCall(t, s, look, peer, "bot_request_create", create); br.CodeOf(err) != br.CodeNoAuthority {
		t.Fatalf("chain to a host notice: got %v", err)
	}
}

func TestBotRequestOnePersonMessageThroughTwoBotsIsOneCard(t *testing.T) {
	s := brOpen(t)
	look := fakeTurns{
		turns:     map[string]pa.Turn{"t1": {ID: "t1", MessageID: "msg-1", Origin: pa.UserOrigin}},
		exchanges: map[string]pa.Exchange{"x1": {ID: "x1", Source: pa.ExchangeEndpoint{Key: brBot("lead").Key, ConversationID: "c"}, SourceTurnID: "t1"}},
	}
	lead := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: look.turns["t1"]}
	coo := botRequestTurn{Bot: brBot("coo"), Conversation: "c2", Turn: pa.Turn{ID: "t2", MessageID: "m2", Origin: pa.BotRequestOrigin, ExchangeID: "x1"}}
	create := map[string]any{"idempotency_key": "k", "title": "Ship it", "kind": "development"}
	a, err := brCall(t, s, look, lead, "bot_request_create", create)
	if err != nil {
		t.Fatal(err)
	}
	b, err := brCall(t, s, look, coo, "bot_request_create", map[string]any{"idempotency_key": "other", "title": "Ship it too", "checklist": []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	first := a.(struct {
		botRequestReply
		Created bool `json:"created"`
	})
	second := b.(struct {
		botRequestReply
		Created bool `json:"created"`
	})
	if !first.Created || second.Created || first.Card.ID != second.Card.ID {
		t.Fatalf("want one card created once: %+v / %+v", first, second)
	}
	if len(second.Card.Chain) != 2 || second.Card.Authorities[0].MessageID != "msg-1" {
		t.Fatalf("chain %v authority %+v", second.Card.Chain, second.Card.Authorities)
	}
}

func TestBotRequestBatchCheckIsIdempotent(t *testing.T) {
	s := brOpen(t)
	look := fakeTurns{turns: map[string]pa.Turn{}, exchanges: map[string]pa.Exchange{}}
	var cards []map[string]any
	for _, msg := range []string{"a", "b"} {
		turn := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t-" + msg, MessageID: msg, Origin: pa.UserOrigin}}
		out, err := brCall(t, s, look, turn, "bot_request_create", map[string]any{"idempotency_key": "k-" + msg, "title": "Card " + msg, "kind": "development"})
		if err != nil {
			t.Fatal(err)
		}
		c := out.(struct {
			botRequestReply
			Created bool `json:"created"`
		}).Card
		cards = append(cards, map[string]any{"card_id": c.ID, "expected_version": c.Version})
	}
	lead := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t9", MessageID: "z", Origin: pa.UserOrigin}}
	args := map[string]any{"idempotency_key": "tick", "cards": cards, "entries": []br.Mark{{Label: "main", State: br.EntryDone, Note: "abc123"}}}
	for round, want := range []string{"ok", "unchanged"} {
		out, err := brCall(t, s, look, lead, "bot_request_check", args)
		if err != nil {
			t.Fatal(err)
		}
		results := out.(map[string]any)["results"].([]store.BotRequestCheckResult)
		if len(results) != 2 {
			t.Fatalf("round %d: %+v", round, results)
		}
		for _, r := range results {
			if r.Outcome != want {
				t.Fatalf("round %d: %+v, want %s", round, r, want)
			}
		}
	}
	// A stranger Bot may not tick them.
	stranger := botRequestTurn{Bot: brBot("x"), Conversation: "c", Turn: pa.Turn{ID: "t8", MessageID: "y", Origin: pa.UserOrigin}}
	out, err := brCall(t, s, look, stranger, "bot_request_check", map[string]any{"idempotency_key": "tick2", "cards": cards, "entries": []br.Mark{{Label: "GitHub", State: br.EntryDone, Note: "n"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out.(map[string]any)["results"].([]store.BotRequestCheckResult) {
		if r.Outcome != "refused" {
			t.Fatalf("stranger tick: %+v", r)
		}
	}
}

func TestBotRequestDoneAndRetryAnswerTheSameCard(t *testing.T) {
	s := brOpen(t)
	look := fakeTurns{turns: map[string]pa.Turn{}, exchanges: map[string]pa.Exchange{}}
	lead := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t1", MessageID: "m", Origin: pa.UserOrigin}}
	out, err := brCall(t, s, look, lead, "bot_request_create", map[string]any{"idempotency_key": "k", "title": "T", "checklist": []string{"only"}})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(struct {
		botRequestReply
		Created bool `json:"created"`
	}).Card
	if _, err := brCall(t, s, look, lead, "bot_request_done", map[string]any{"idempotency_key": "d0", "card_id": c.ID, "expected_version": c.Version}); br.CodeOf(err) != br.CodeChecklistOpen {
		t.Fatalf("done with open checklist: %v", err)
	}
	out, err = brCall(t, s, look, lead, "bot_request_check", map[string]any{"idempotency_key": "c1", "card_id": c.ID, "expected_version": c.Version, "entries": []br.Mark{{Label: "only", State: br.EntryDone, Note: "n"}}})
	if err != nil {
		t.Fatal(err)
	}
	v := out.(botRequestReply).Card.Version
	if _, err := brCall(t, s, look, lead, "bot_request_check", map[string]any{"idempotency_key": "c2", "card_id": c.ID, "expected_version": c.Version, "entries": []br.Mark{{Label: "only", State: br.EntryDone, Note: "m"}}}); br.CodeOf(err) != br.CodeVersionConflict {
		t.Fatalf("stale version: %v", err)
	}
	for i := range 2 {
		out, err = brCall(t, s, look, lead, "bot_request_done", map[string]any{"idempotency_key": "d1", "card_id": c.ID, "expected_version": v})
		if err != nil {
			t.Fatal(err)
		}
		r := out.(botRequestReply)
		if r.Card.State != br.StateDone || (i == 1) != r.Replayed {
			t.Fatalf("round %d: %+v", i, r)
		}
	}
	if _, err := brCall(t, s, look, lead, "bot_request_read", map[string]any{"card_id": "missing"}); br.CodeOf(err) != br.CodeNotFound {
		t.Fatalf("missing card: %v", err)
	}
}

func brCreated(t *testing.T, out any, err error) br.Card {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return out.(struct {
		botRequestReply
		Created bool `json:"created"`
	}).Card
}

// ageBotRequestEffects moves this card kind's outbox rows past the broker's
// unattended age, so the next beat drains them as it would after a restart.
func ageBotRequestEffects(t *testing.T, f *hostFixture, kind string) {
	t.Helper()
	if _, err := f.o.Database.Exec(`UPDATE outbox SET created_at = ? WHERE kind = ? AND state = 'pending'`, time.Now().Add(-time.Minute).Unix(), kind); err != nil {
		t.Fatal(err)
	}
}

func brNoPrompt(t *testing.T, f *hostFixture) {
	t.Helper()
	select {
	case q := <-f.p.sent:
		t.Fatalf("a second notice turn was queued: %+v", q.Turn)
	case <-time.After(300 * time.Millisecond):
	}
}

// One person message, one card: work two Bots dispatch naming it attaches to
// it; a handoff wakes the new holder once across a retry and a restart; in
// that turn only work naming the card is accepted; and completion tells the
// person and the asking Bot once per cycle (docs/bot-request-cards.md §12).
func TestBotRequestHandoffWakesOnceAndScopesDispatchToTheCard(t *testing.T) {
	opened := make(chan NativeWork, 8)
	n := startNative(t, func(f *hostFixture) {
		inner, first := f.o.NativeWork, true
		f.o.NativeWork = func(ctx context.Context, q app.OpenRequest, w NativeWork) (func(), error) {
			opened <- w
			if first {
				// startNative reads the first turn's tools from its own
				// one-slot channel; later turns are read from opened only.
				first = false
				return inner(ctx, q, w)
			}
			return func() {}, nil
		}
	})
	<-opened
	f, ctx := n.f, context.Background()
	broker := f.o.Broker
	session := fixtureReceive(t, f.p.sessions)
	call := func(tools NativeWork, name string, args any) (any, error) {
		raw, _ := json.Marshal(args)
		return tools.BotRequest(ctx, name, raw)
	}
	out, err := call(n.tools, "bot_request_create", map[string]any{"idempotency_key": "open", "title": "Ship it", "checklist": []string{"build"}})
	card := brCreated(t, out, err)
	// A second Bot the same message reached joins the card.
	look := fakeTurns{
		turns:     map[string]pa.Turn{n.prompt.Turn.ID: n.prompt.Turn},
		exchanges: map[string]pa.Exchange{"x1": {ID: "x1", Source: pa.ExchangeEndpoint{Key: n.bot.Key, ConversationID: n.prompt.Turn.Fence.ConversationID}, SourceTurnID: n.prompt.Turn.ID}},
	}
	coo := botRequestTurn{Bot: brBot("coo"), Conversation: "c2", Turn: pa.Turn{ID: "t2", MessageID: "m2", Origin: pa.BotRequestOrigin, ExchangeID: "x1"}}
	coo.Bot.Key.MachineID = n.bot.Key.MachineID
	out, err = botRequestTool(ctx, broker.Store, look, coo, n.bot.Key.MachineID, "v-coo", time.Now(), "bot_request_create", mustJSON(t, map[string]any{"idempotency_key": "coo", "title": "Ship it", "kind": "development"}))
	if joined := brCreated(t, out, err); joined.ID != card.ID || len(joined.Chain) != 2 {
		t.Fatalf("second Bot did not join the card: %+v", joined)
	}

	named := pa.WithBotRequest(ctx, card.ID)
	first, err := n.tools.Dispatch(named, "card-work-1", "First part", "Change a.go.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if first.BotRequestID != card.ID {
		t.Fatalf("work link does not name the card: %+v", first)
	}
	for _, id := range []string{first.TaskID} {
		r, _, err := broker.Record(ctx, id)
		if err != nil || r.BotRequestID != card.ID {
			t.Fatalf("task %s not attached: %q %v", id, r.BotRequestID, err)
		}
	}
	plain, err := n.tools.DispatchWithCompletion(ctx, "plain-work", "Unrelated", "Read b.go.", []string{}, "", "", "")
	if err != nil || plain.BotRequestID != "" {
		t.Fatalf("a dispatch naming no card attached to one: %+v %v", plain, err)
	}
	if _, err := n.tools.Dispatch(pa.WithBotRequest(ctx, "00000000-0000-8000-8000-000000000000"), "missing-card", "Missing", "Change a.go.", []string{"a.go"}); pa.Code(err) != pa.NotFound {
		t.Fatalf("a missing card was accepted: %v", err)
	}
	// The second Bot, in the chain, is accepted by the broker's card check;
	// a Bot outside it is not.
	if id, err := broker.BotRequestCardFor(ctx, card.ID, "coo"); err != nil || id != card.ID {
		t.Fatalf("chain Bot refused: %q %v", id, err)
	}
	if _, err := broker.BotRequestCardFor(ctx, card.ID, "stranger"); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("stranger accepted: %v", err)
	}
	session.reply <- app.PromptResult{FinalText: "Opened the card."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})

	// The holder hands the baton on (here to itself as reviewer); a retried
	// handoff with the same key queues nothing new.
	handoff := map[string]any{"idempotency_key": "hand", "card_id": card.ID, "expected_version": card.Version + 1, "to_bot": n.bot.Key.BotID, "role": br.RoleReviewer, "note": "review a.go"}
	cur, err := broker.Store.BotRequestCard(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	handoff["expected_version"] = cur.Version
	if _, err := botRequestTool(ctx, broker.Store, f.h.store, botRequestTurn{Bot: n.bot, Conversation: n.prompt.Turn.Fence.ConversationID, Turn: n.prompt.Turn}, n.bot.Key.MachineID, "v", time.Now(), "bot_request_handoff", mustJSON(t, handoff)); err != nil {
		t.Fatal(err)
	}
	if _, err := botRequestTool(ctx, broker.Store, f.h.store, botRequestTurn{Bot: n.bot, Conversation: n.prompt.Turn.Fence.ConversationID, Turn: n.prompt.Turn}, n.bot.Key.MachineID, "v", time.Now(), "bot_request_handoff", mustJSON(t, handoff)); err != nil {
		t.Fatal(err)
	}
	ageBotRequestEffects(t, f, store.EffectBotRequestHandoff)
	broker.RecoverEffects(ctx)
	wake := fixtureReceive(t, f.p.sent)
	if got, _ := pa.BotRequestHandoffCard(wake.Turn); got != card.ID {
		t.Fatalf("the notice turn does not name the card: %+v", wake.Turn)
	}
	handTools := fixtureReceive(t, opened)
	wakeSession := fixtureReceive(t, f.p.sessions)
	// Retry and restart: the same notice again, and the row run again.
	held, _ := broker.Store.BotRequestCard(ctx, card.ID)
	if err := broker.BotRequestNotice(ctx, orchestrator.BotRequestNotice{Bot: n.bot.Key.BotID, Key: pa.BotRequestHandoffKey(card.ID, held.Version), CardID: card.ID, Version: held.Version, Kind: "handoff"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.o.Database.Exec(`UPDATE outbox SET state = 'pending', created_at = ? WHERE kind = ?`, time.Now().Add(-time.Minute).Unix(), store.EffectBotRequestHandoff); err != nil {
		t.Fatal(err)
	}
	broker.RecoverEffects(ctx)
	brNoPrompt(t, f)

	// In the handoff turn only work naming this card is accepted.
	if _, err := handTools.Dispatch(ctx, "hand-none", "None", "Read d.go.", []string{}); br.CodeOf(err) != br.CodeHandoffScope {
		t.Fatalf("dispatch naming no card in a handoff turn: %v", err)
	}
	if _, err := handTools.Dispatch(pa.WithBotRequest(ctx, "00000000-0000-8000-8000-000000000000"), "hand-other", "Other", "Read d.go.", []string{}); br.CodeOf(err) != br.CodeHandoffScope {
		t.Fatalf("dispatch naming another card in a handoff turn: %v", err)
	}
	second, err := handTools.Dispatch(named, "hand-card", "Second part", "Change a.go again.", []string{"a.go"})
	if err != nil || second.BotRequestID != card.ID {
		t.Fatalf("dispatch naming the card in its handoff turn: %+v %v", second, err)
	}
	wakeSession.reply <- app.PromptResult{FinalText: "Reviewed."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, wake.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})

	// Completion: one push to the person, one notice to the asking Bot, once
	// per cycle, and again after a reopen.
	var pushes []string
	broker.Push = func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		pushes = append(pushes, tag)
		return 1, 0, nil
	}
	person := br.Actor{Person: true}
	complete := func(round string) {
		cur, err := broker.Store.BotRequestCard(ctx, card.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := broker.Store.UpdateBotRequest(ctx, store.BotRequestWrite{CardID: card.ID, Expected: cur.Version, Actor: person, Key: "done-" + round, Now: time.Now(), Apply: func(c *br.Card) ([]br.Event, error) {
			if _, err := c.Check(person, []br.Mark{{Label: "build", State: br.EntryDone, Note: "ok"}}, time.Now()); err != nil {
				return nil, err
			}
			if _, err := c.Done(person, time.Now()); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventDone}}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		ageBotRequestEffects(t, f, store.EffectBotRequestDone)
		ageBotRequestEffects(t, f, store.EffectBotRequestDoneNotice)
		broker.RecoverEffects(ctx)
		done := fixtureReceive(t, f.p.sent)
		if !strings.HasPrefix(done.Turn.IdempotencyKey, pa.BotRequestDonePrefix) {
			t.Fatalf("round %s: not a completion notice: %+v", round, done.Turn)
		}
		fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "Noted."}
		fixtureEventually(t, func() bool {
			turn, err := f.h.store.Turn(ctx, n.bot.Key, done.Turn.ID)
			return err == nil && turn.State == pa.TurnSucceeded
		})
		// A restart finds the rows done and sends nothing again.
		broker.RecoverEffects(ctx)
		brNoPrompt(t, f)
	}
	complete("1")
	if len(pushes) != 1 {
		t.Fatalf("pushes after the first completion: %v", pushes)
	}
	cur, _ = broker.Store.BotRequestCard(ctx, card.ID)
	if _, err := broker.Store.UpdateBotRequest(ctx, store.BotRequestWrite{CardID: card.ID, Expected: cur.Version, Actor: person, Key: "reopen", Now: time.Now(), Apply: func(c *br.Card) ([]br.Event, error) {
		if _, err := c.Reopen(person); err != nil {
			return nil, err
		}
		return []br.Event{{Kind: br.EventReopened}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	complete("2")
	if len(pushes) != 2 || pushes[0] == pushes[1] {
		t.Fatalf("pushes after reopen and second completion: %v", pushes)
	}
	// The card's history records each delivery once (§4, §5), across the
	// retries and restarts above.
	events, err := broker.Store.BotRequestEvents(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	for _, e := range events {
		if e.Kind == br.EventNotified {
			notified = append(notified, e.Detail)
		}
	}
	slices.Sort(notified)
	want := []string{"bots cycle=1 bot_request_done_delivered", "bots cycle=2 bot_request_done_delivered", "person cycle=1 pushed", "person cycle=2 pushed"}
	if !slices.Equal(notified, want) {
		t.Fatalf("notified events %q, want %q", notified, want)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
