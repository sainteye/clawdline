package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
)

// What the delivery branch held when the task ended reaches the wire, under
// the name the contract gives it.
//
// It has to: the whole point of keeping it is that somebody other than the
// broker — a root reading `/inflight`, a person reading the console — can see
// that a row owes a landing nothing could ever prove. A fact only the store
// holds answers nobody's question.
func TestTheWireCarriesWhatTheBranchHeld(t *testing.T) {
	for _, settlement := range []orchestrator.LandingSettlement{
		orchestrator.SettlementEmpty,
		orchestrator.SettlementCarried,
		orchestrator.SettlementUnreadable,
	} {
		l := &orchestrator.Landing{State: orchestrator.LandingPending, Settlement: settlement}
		body, err := json.Marshal(brokerLanding(l, orchestrator.ObligationLive))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"settlement":"`+string(settlement)+`"`) {
			t.Errorf("the wire does not carry %q: %s", settlement, body)
		}
	}

	// A task with no branch of its own was asked nothing, and absent is how
	// "nobody asked" is spelled — never a value that reads as an answer.
	body, err := json.Marshal(brokerLanding(&orchestrator.Landing{State: orchestrator.LandingPending}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "settlement") {
		t.Errorf("a landing nobody asked about still carries a settlement: %s", body)
	}
}
