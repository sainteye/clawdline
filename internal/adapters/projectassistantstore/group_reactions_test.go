package projectassistantstore

import (
	"errors"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestGroupReactionAndReceiptRollBackTogether(t *testing.T) {
	db, bots, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	bot := seedIn(t, bots, "one", scope, false)
	draft := pa.GroupDraft{Key: gk("room"), Project: scope, ConversationID: "room-chat", Name: "Room", Members: []pa.BotKey{bot.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(draft.Key, pa.CreateOperation, "create", "room"), draft, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	room, err := groups.Room(testContext, draft.Key)
	if err != nil {
		t.Fatal(err)
	}
	line, _, _, err := groups.PersonWrote(testContext, groupCmd(draft.Key, pa.SendOperation, "send", "hi"), room.Revision, "hi", nil, pa.BotKey{})
	if err != nil {
		t.Fatal(err)
	}
	request := app.GroupReactionRequest{Command: groupCmd(draft.Key, pa.ReactOperation, "one-reaction", "plus"), ConversationID: draft.ConversationID, GroupVersion: 1, MessageSeq: line.Seq, Emoji: "👍", Present: true}
	actor := pa.HumanReactionID("reader")
	failed := errors.New("synthetic receipt failure")
	groups.hook = func(stage string) error {
		if stage == "receipt" {
			return failed
		}
		return nil
	}
	if _, err := groups.ReactToGroupLine(testContext, request, actor); !errors.Is(err, failed) {
		t.Fatalf("injected write: %v", err)
	}
	// Re-open the adapter over the same database. The failed receipt and its
	// reaction must both be gone, so the original expected revision remains valid.
	reopened := NewGroupStore(db)
	page, err := reopened.History(testContext, draft.Key, 0, 1)
	if err != nil || len(page.Reactions) != 0 {
		t.Fatalf("partial reaction: %+v %v", page.Reactions, err)
	}
	first, err := reopened.ReactToGroupLine(testContext, request, actor)
	if err != nil {
		t.Fatal(err)
	}
	next, err := reopened.ReactToGroupLine(testContext, request, actor)
	if err != nil || first != next {
		t.Fatalf("receipt replay: %+v %+v %v", first, next, err)
	}
	page, err = reopened.History(testContext, draft.Key, 0, 1)
	entries := page.Reactions[line.Seq]
	if err != nil || len(entries) != 1 || entries[0].Revision != 1 || len(entries[0].Emojis) != 1 || entries[0].Emojis[0] != "👍" || entries[0].Own {
		t.Fatalf("persisted neutral reaction: %+v %v", entries, err)
	}
}
