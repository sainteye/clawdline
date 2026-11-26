package projectassistantstore

import (
	"encoding/json"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// One Bot whose stored chat no longer decodes does not hide every other Bot's
// question cards: its row is skipped, and the card is still found.
func TestAnUnreadableBotDoesNotHideAnotherBotsCard(t *testing.T) {
	db, s, _ := testDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	fence := pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "xavia"}, Project: pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "/code/p"},
		ConversationID: "chat-1", BindingID: "b1", Generation: 1}
	a := aggregate{
		Bot:          pa.Bot{Key: fence.Key},
		Conversation: pa.Conversation{ID: "chat-1", BindingID: "b1"},
		Bindings:     map[string]pa.ExecutionBinding{"b1": {Fence: fence, NativeSessionID: "native-1"}},
		Permissions: map[string]pa.PermissionOffer{"card-1": {ID: "card-1", Fence: fence, Kind: pa.DecisionOfferKind, State: pa.PermissionResolved,
			Decision: &pa.DecisionRequest{Question: "Open it?", AnswerText: "Yes"}}},
		Messages: []pa.Message{{Kind: pa.UserMessage, At: now.Add(-time.Minute), DecisionReply: &pa.DecisionReply{OfferID: "card-1", Answer: "Yes"}}},
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	// "atlas" sorts before "xavia", so the broken row is read first.
	for bot, row := range map[string][]byte{"atlas": []byte("{not json"), "xavia": data} {
		if _, err := db.ExecContext(testContext, `INSERT INTO project_assistant_bots(machine,bot,version,data) VALUES('m',?,1,?)`, bot, row); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.DecisionAuthority(testContext, "m", "native-1", "card-1", now, time.Hour)
	if err != nil || got.OfferID != "card-1" || got.BotID != "xavia" {
		t.Fatalf("the card behind an unreadable Bot = %+v, %v", got, err)
	}
	if _, err := s.DecisionAuthority(testContext, "m", "native-1", "card-2", now, time.Hour); !isDecisionRefusal(err, pa.DecisionUnknown) {
		t.Fatalf("an unknown card beside an unreadable Bot = %v", err)
	}
}

func isDecisionRefusal(err error, code string) bool {
	r, ok := err.(*pa.DecisionRefusal)
	return ok && r.Code == code
}
