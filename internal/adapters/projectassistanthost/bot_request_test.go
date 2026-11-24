package projectassistanthost

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
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
