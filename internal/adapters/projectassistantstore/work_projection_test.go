package projectassistantstore

import (
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A projection of later broker facts is distinct from permission to replay
// the original effect. The repository receives these facts from its trusted
// application owner; broker authentication is verified in the integration suite.
func TestUnknownWorkAcceptsLaterFactsWithoutReplayingItsCommand(t *testing.T) {
	for _, kind := range []string{"landing", "board link"} {
		t.Run(kind, func(t *testing.T) {
			_, s, _ := testDB(t)
			b := seed(t, s, "one")
			f := populate(t, s, b, pa.EffectDelivered)
			unknown := pa.WorkLink{ID: "work", Fence: f, TurnID: "turn", TaskID: "task", State: pa.WorkUnknown}
			original := command(b.Key, pa.DispatchOperation, "original-dispatch")
			bot, err := s.Bot(testContext, b.Key)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Commit(testContext, app.Mutation{Command: original, ExpectedVersion: bot.Version,
				WorkLinks: []pa.WorkLink{unknown}, Receipt: pa.Receipt{ID: "original-dispatch", State: pa.EffectUnknown, ResourceID: "task"}})
			if err != nil {
				t.Fatal(err)
			}
			originalReceipt, err := s.Receipt(testContext, original)
			if err != nil || originalReceipt.State != pa.EffectUnknown {
				t.Fatalf("unknown dispatch receipt: %+v %v", originalReceipt, err)
			}
			before, err := s.Export(testContext, b.Key)
			if err != nil {
				t.Fatal(err)
			}
			updated := unknown
			updated.BoardItemID = "board-item"
			if kind == "landing" {
				updated.State = pa.WorkLanded
				updated.LandingCommit = "0123456789012345678901234567890123456789"
			}
			projection := command(b.Key, pa.DispatchOperation, "verified-broker-fact")
			mutation := app.Mutation{Command: projection, ExpectedVersion: before.Bot.Version,
				WorkLinks: []pa.WorkLink{updated}, Receipt: pa.Receipt{ID: "projection", State: pa.EffectDelivered, ResourceID: updated.ID},
				Events: []pa.Event{{Version: pa.ContractVersion, ID: "projection-event", Fence: f, Kind: pa.WorkUpdated, Work: &updated}}}
			if _, err = s.Commit(testContext, mutation); err != nil {
				t.Fatalf("later broker fact could not update unknown projection: %v", err)
			}
			after, err := s.Export(testContext, b.Key)
			if err != nil || len(after.WorkLinks) != 1 || after.WorkLinks[0] != updated {
				t.Fatalf("projection not saved: %+v %v", after.WorkLinks, err)
			}
			if len(after.Messages) != len(before.Messages) || len(after.Events) != len(before.Events)+1 {
				t.Fatal("a work metadata update created a visible message or lost its event")
			}
			if _, err = s.Commit(testContext, mutation); err != nil {
				t.Fatal(err)
			}
			repeated, err := s.Export(testContext, b.Key)
			if err != nil || !reflect.DeepEqual(after, repeated) {
				t.Fatal("same projection replay changed stored facts, messages or event sequence")
			}
			// Repeating the ORIGINAL intent must return its unknown receipt,
			// even though a different projection now records confirmed facts.
			receipt, err := s.Commit(testContext, app.Mutation{Command: original, ExpectedVersion: -1})
			if err != nil || !reflect.DeepEqual(receipt, *originalReceipt) {
				t.Fatalf("original effect was replayed/refined: %+v %v", receipt, err)
			}
			final, err := s.Export(testContext, b.Key)
			if err != nil || !reflect.DeepEqual(after, final) {
				t.Fatal("original unknown replay mutated the new projection")
			}
			foreign := updated
			foreign.TaskID = "foreign-task"
			_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.DispatchOperation, "foreign"), ExpectedVersion: after.Bot.Version,
				WorkLinks: []pa.WorkLink{foreign}, Receipt: pa.Receipt{ID: "foreign", State: pa.EffectDelivered}})
			requireCode(t, err, pa.ScopeDenied)
		})
	}
}
