package projectassistant

import (
	"strconv"
	"strings"
	"testing"
)

func followUpRound(id, replyTo string, size int) Exchange {
	a, b := BotKey{MachineID: "m", BotID: "a"}, BotKey{MachineID: "m", BotID: "b"}
	return Exchange{ID: id, ReplyTo: replyTo, Source: ExchangeEndpoint{Key: a}, Target: ExchangeEndpoint{Key: b}, Outcome: ExchangeCompleted, Request: strings.Repeat("q", size), Reply: strings.Repeat("r", size)}
}

func TestReplyToNeedsTheRequesterOfAFinishedRoundOfThePair(t *testing.T) {
	a, b, c := BotKey{MachineID: "m", BotID: "a"}, BotKey{MachineID: "m", BotID: "b"}, BotKey{MachineID: "m", BotID: "c"}
	x := followUpRound("x", "", 1)
	if err := CheckReplyTo(x, a, b); err != nil {
		t.Fatalf("a finished round of the pair was refused: %v", err)
	}
	for _, c := range []struct {
		name           string
		x              Exchange
		source, target BotKey
		field          string
	}{
		{"recipient follows up", x, b, a, "reply_to_exchange_requester"},
		{"other target", x, a, c, "reply_to_exchange_pair"},
		{"queued", withOutcome(x, ExchangeQueued), a, b, "reply_to_exchange_open"},
		{"running", withOutcome(x, ExchangeSent), a, b, "reply_to_exchange_open"},
		{"waiting for the person", withOutcome(x, ExchangeNeedsPerson), a, b, "reply_to_exchange_waiting_person"},
	} {
		err := CheckReplyTo(c.x, c.source, c.target)
		if Code(err) != ScopeDenied || err.(*Failure).Field != c.field {
			t.Errorf("%s: want %s %s, got %v", c.name, ScopeDenied, c.field, err)
		}
	}
	for _, o := range []ExchangeOutcome{ExchangeFailed, ExchangeCancelled, ExchangeUnknown} {
		if err := CheckReplyTo(withOutcome(x, o), a, b); err != nil {
			t.Errorf("a %s round must be quotable: %v", o, err)
		}
	}
}

func withOutcome(x Exchange, o ExchangeOutcome) Exchange { x.Outcome = o; return x }

func TestQuotedRoundsStopAtTheirBounds(t *testing.T) {
	a, b := BotKey{MachineID: "m", BotID: "a"}, BotKey{MachineID: "m", BotID: "b"}
	var chain []Exchange
	for i := 0; i < MaxCollaborationQuoteRounds+2; i++ {
		chain = append(chain, followUpRound("x"+strconv.Itoa(i), "x"+strconv.Itoa(i+1), 10))
	}
	kept, omitted := QuoteRounds(chain, a, b)
	if len(kept) != MaxCollaborationQuoteRounds || !omitted || kept[0].ID != "x0" {
		t.Fatalf("round bound: kept %d omitted %v", len(kept), omitted)
	}
	// The largest possible single round always fits; a second one does not.
	big := []Exchange{followUpRound("big0", "big1", MaxCollaborationRequestBytes), followUpRound("big1", "", MaxCollaborationRequestBytes)}
	kept, omitted = QuoteRounds(big, a, b)
	if len(kept) != 1 || !omitted {
		t.Fatalf("byte bound: kept %d omitted %v", len(kept), omitted)
	}
	total := 0
	for _, x := range kept {
		total += len(x.Request) + len(x.Reply)
	}
	if total > MaxCollaborationQuoteBytes {
		t.Fatalf("quote of %d bytes exceeds its %d-byte budget", total, MaxCollaborationQuoteBytes)
	}
	// A round of another pair ends the chain even if it was linked.
	foreign := followUpRound("y", "", 1)
	foreign.Target.Key.BotID = "c"
	kept, omitted = QuoteRounds([]Exchange{followUpRound("x", "y", 1), foreign}, a, b)
	if len(kept) != 1 || !omitted {
		t.Fatalf("foreign round: kept %d omitted %v", len(kept), omitted)
	}
	kept, omitted = QuoteRounds([]Exchange{followUpRound("x", "", 1)}, a, b)
	if len(kept) != 1 || omitted {
		t.Fatalf("complete chain: kept %d omitted %v", len(kept), omitted)
	}
}
