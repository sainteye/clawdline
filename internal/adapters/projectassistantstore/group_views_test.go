package projectassistantstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestUnquotedRoomCommandKeepsItsPreviousDigest(t *testing.T) {
	db, store, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	a := seedIn(t, store, "a", scope, false)
	key := gk("room")
	draft := pa.GroupDraft{Key: key, Project: scope, ConversationID: "room/chat", Name: "Room", Members: []pa.BotKey{a.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(key, pa.CreateOperation, "create", ""), draft, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	rooms, err := app.NewGroupRooms(groups, groupLimits, func() time.Time { return groupTime })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = rooms.Send(testContext, app.PersonAuthority{Principal: "person", Machine: "machine"}, command(key, pa.SendOperation, "legacy"), 0, "hello", pa.BotKey{}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_group_receipts WHERE machine='machine' AND grp='room' AND command_key='legacy'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var receipt pa.Receipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	// Frozen API76 canonical bytes; API88 must not add a zero quote field.
	legacy := `{"principal":"person","key":{"machine_id":"machine","bot_id":"room"},"operation":"send","payload":{"text":"hello","reply_to_author":{"machine_id":"","bot_id":""}}}`
	want := fmt.Sprintf("%x", sha256Sum(legacy))
	if receipt.Command.ContentHash != want {
		t.Fatalf("an existing no-quote replay would conflict: got %s want %s", receipt.Command.ContentHash, want)
	}
}

func TestGroupPreviewsKeepSourceTimeAndFitTheReadBudget(t *testing.T) {
	db, store, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	a := seedIn(t, store, "a", scope, false)
	b := seedIn(t, store, "b", scope, false)
	for i, member := range []pa.Bot{a, b} {
		key := gk(string(rune('g' + i)))
		draft := pa.GroupDraft{Key: key, Project: scope, ConversationID: "room/" + key.BotID, Name: "Room", Members: []pa.BotKey{member.Key}}
		if _, err := groups.CreateGroup(testContext, groupCmd(key, pa.CreateOperation, "create-"+key.BotID, ""), draft, groupLimits, groupTime); err != nil {
			t.Fatal(err)
		}
		entry, _, _, err := groups.PersonWrote(testContext, command(key, pa.SendOperation, "say-"+key.BotID), 0, "real latest message", pa.BotKey{})
		if err != nil || entry.At.IsZero() {
			t.Fatalf("new source line has no write time: %+v %v", entry, err)
		}
	}
	// A historical line predating source timestamps stays undated, even if
	// metadata was renamed later. It remains the room's actual latest line.
	old := pa.RoomEntry{Seq: 2, Kind: pa.RoomMemberEntry, Author: a.Key, Text: "older schema line"}
	raw, _ := json.Marshal(old)
	if _, err := db.Exec(`INSERT INTO project_assistant_group_lines(machine,grp,seq,data) VALUES('machine','g',2,?)`, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.EditGroup(testContext, groupCmd(gk("g"), pa.EditOperation, "rename", ""), pa.GroupEdit{ExpectedVersion: 1, Name: "Renamed later", Members: []pa.BotKey{a.Key}}, groupLimits, groupTime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var roomBytes int
	if err := db.QueryRow(`SELECT length(data) FROM project_assistant_group_rooms WHERE machine='machine' AND grp='g'`).Scan(&roomBytes); err != nil {
		t.Fatal(err)
	}
	previews, err := groups.GroupPreviews(testContext, "machine", len(raw)+roomBytes)
	if err != nil || len(previews) != 2 || previews[0].LastEntry == nil || *previews[0].LastEntry != old || !previews[0].LastEntry.At.IsZero() {
		t.Fatalf("preview invented time or selected metadata instead of the line: %+v %v", previews, err)
	}
	// The first group's real line and room consume exactly the budget. The
	// second group's metadata remains after optional capacity is spent.
	if previews[0].Room == nil || previews[1].LastEntry != nil || previews[1].Room != nil || previews[1].Group.Key != gk("h") {
		t.Fatalf("optional data escaped the total byte budget: %+v", previews)
	}
	full, err := groups.GroupPreviews(testContext, "machine", 1<<20)
	if err != nil || len(full) != 2 || full[1].Room == nil || full[1].LastEntry == nil || full[1].LastEntry.At.IsZero() {
		t.Fatalf("a fitting actual preview was dropped: %+v %v", full, err)
	}
}

func TestRoomHistoryShowsOnlyCurrentMemberExecution(t *testing.T) {
	_, store, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	a := seedIn(t, store, "a", scope, false)
	b := seedIn(t, store, "b", scope, false)
	key := gk("room")
	draft := pa.GroupDraft{Key: key, Project: scope, ConversationID: "room/chat", Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(key, pa.CreateOperation, "create", ""), draft, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	_, room, _, err := groups.PersonWrote(testContext, command(key, pa.SendOperation, "say"), 0, "hello", pa.BotKey{})
	if err != nil {
		t.Fatal(err)
	}
	group, _ := groups.Group(testContext, key)
	for i, bot := range []pa.Bot{a, b} {
		ticket, err := pa.TicketFor(group, room, bot.Key, false)
		if err != nil {
			t.Fatal(err)
		}
		m := finishing(t, store, bot, ticket, "pending-"+bot.Key.BotID)
		m.Turn.State = pa.TurnQueued
		if i == 0 {
			m.Turn.State = pa.TurnRunning
			m.Conversation.ActiveTurnID = m.Turn.ID
		}
		if _, err = store.Commit(testContext, m); err != nil {
			t.Fatal(err)
		}
	}
	page, err := groups.History(testContext, key, 0, 100)
	if err != nil || len(page.Activity) != 2 || page.Activity[0].Member != a.Key || page.Activity[0].State != pa.TurnRunning || page.Activity[1].Member != b.Key || page.Activity[1].State != pa.TurnQueued || len(page.Lines) != 1 || page.Room.Epoch != room.Epoch {
		t.Fatalf("activity does not describe the actual stored turns: %+v %v", page, err)
	}
	if len(page.MemberOutcomes) != 2 || page.MemberOutcomes[0].Name != a.Name || page.MemberOutcomes[1].Name != b.Name {
		t.Fatalf("current turns do not have scoped member names: %+v", page.MemberOutcomes)
	}
	// Removing and rejoining a member invalidates its previous ticket even
	// though its original Bot turn remains queued in storage.
	for i, members := range [][]pa.BotKey{{a.Key}, {a.Key, b.Key}} {
		if _, err = groups.EditGroup(testContext, groupCmd(key, pa.EditOperation, "membership-"+string(rune('0'+i)), ""), pa.GroupEdit{ExpectedVersion: int64(i + 1), Name: "Room", Members: members}, groupLimits, groupTime); err != nil {
			t.Fatal(err)
		}
	}
	page, err = groups.History(testContext, key, 0, 100)
	if err != nil || len(page.Activity) != 1 || page.Activity[0].Member != a.Key ||
		len(page.MemberOutcomes) != 1 || page.MemberOutcomes[0].Member != a.Key {
		t.Fatalf("a removed/rejoined member claimed stale work: %+v %v", page, err)
	}
	if _, err = groups.UpdateRoom(testContext, key, room.Revision, func(_ pa.Group, state pa.RoomState) (pa.RoomState, error) {
		next, _, err := pa.StopRoom(state, groupLimits)
		return next, err
	}); err != nil {
		t.Fatal(err)
	}
	page, err = groups.History(testContext, key, 0, 100)
	if err != nil || len(page.Activity) != 0 || len(page.MemberOutcomes) != 0 {
		t.Fatalf("a stopped room still claimed member work: %+v %v", page, err)
	}
}

func TestRoomOutcomeDoesNotRevealForeignOrUnverifiedCompletion(t *testing.T) {
	_, store, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	a := seedIn(t, store, "a", scope, false)
	key := gk("room")
	draft := pa.GroupDraft{Key: key, Project: scope, ConversationID: "room/chat", Name: "Room", Members: []pa.BotKey{a.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(key, pa.CreateOperation, "create", ""), draft, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	_, room, _, err := groups.PersonWrote(testContext, command(key, pa.SendOperation, "first"), 0, "hello", pa.BotKey{})
	if err != nil {
		t.Fatal(err)
	}
	group, err := groups.Group(testContext, key)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := pa.TicketFor(group, room, a.Key, false)
	if err != nil {
		t.Fatal(err)
	}
	private := finishing(t, store, a, ticket, "direct")
	private.Turn.Room = nil
	private.Turn.Origin = pa.UserOrigin
	private.Turn.State = pa.TurnUnknown
	private.Turn.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "private diagnostics"}
	private.Messages[0].Kind = pa.UserMessage
	private.Messages[0].Text = "private direct message"
	if _, err = store.Commit(testContext, private); err != nil {
		t.Fatal(err)
	}
	foreign := ticket
	foreign.Group = gk("other-room")
	if _, err = store.Commit(testContext, finishing(t, store, a, foreign, "foreign")); err != nil {
		t.Fatal(err)
	}
	// A legacy terminal turn has no publication receipt. The room cannot
	// decide whether the model passed or a room line was withheld.
	if _, err = store.Commit(testContext, finishing(t, store, a, ticket, "legacy")); err != nil {
		t.Fatal(err)
	}
	page, err := groups.History(testContext, key, 0, 100)
	if err != nil || len(page.MemberOutcomes) != 1 || page.MemberOutcomes[0].TurnID != "legacy" ||
		page.MemberOutcomes[0].State != pa.TurnSucceeded || page.MemberOutcomes[0].Disposition != "" ||
		len(page.Lines) != 1 || strings.Contains(page.Lines[0].Text, "private") {
		t.Fatalf("wrong room evidence: %+v %v", page, err)
	}
	if _, _, _, err = groups.PersonWrote(testContext, command(key, pa.SendOperation, "second"), room.Revision, "new round", pa.BotKey{}); err != nil {
		t.Fatal(err)
	}
	page, err = groups.History(testContext, key, 0, 100)
	if err != nil || len(page.MemberOutcomes) != 0 {
		t.Fatalf("previous round's turn was published again: %+v %v", page, err)
	}
}

func TestRoomQuoteKeepsTheSourceAuthorAndBoundedUnicodeExcerpt(t *testing.T) {
	db, store, groups, _ := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	a := seedIn(t, store, "a", scope, false)
	b := seedIn(t, store, "b", scope, false)
	key := gk("room")
	draft := pa.GroupDraft{Key: key, Project: scope, ConversationID: "room/chat", Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(key, pa.CreateOperation, "create", ""), draft, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	original := pa.RoomEntry{Seq: 1, Kind: pa.RoomMemberEntry, Author: a.Key, Text: strings.Repeat("界", pa.MaxMessageBytes)}
	raw, _ := json.Marshal(original)
	if _, err := db.Exec(`INSERT INTO project_assistant_group_lines(machine,grp,seq,data) VALUES('machine','room',1,?)`, raw); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := groups.PersonWrote(testContext, command(key, pa.SendOperation, "forged-author"), 0, "reply", b.Key, 1); pa.Code(err) != pa.Invalid {
		t.Fatalf("a forged quote author was accepted: %v", err)
	}
	entry, _, _, err := groups.PersonWrote(testContext, command(key, pa.SendOperation, "reply"), 0, "@Bot b please review", pa.BotKey{}, 1)
	if err != nil || entry.Quote == nil || entry.Quote.Author != a.Key || entry.ReplyToAuthor != a.Key || entry.Quote.Seq != 1 || !entry.Quote.Truncated || len(entry.Quote.Text) > pa.MaxMessageBytes || !utf8.ValidString(entry.Quote.Text) || !strings.HasPrefix(original.Text, entry.Quote.Text) {
		t.Fatalf("quote lost its source or byte boundary: %+v %v", entry, err)
	}
	stored, err := roomLine(testContext, db, key, 1)
	if err != nil || stored.Text != original.Text {
		t.Fatal("retaining an excerpt changed the original line", err)
	}
	recipients := pa.RoomSpeakersFor([]pa.RoomSpeaker{{Key: a.Key, Name: a.Name}, {Key: b.Key, Name: b.Name}}, []pa.RoomEntry{entry}, false, 0, nil)
	if len(recipients) != 2 || recipients[0].Key != a.Key || recipients[1].Key != b.Key {
		t.Fatalf("quote and explicit mention lost their separate recipients: %+v", recipients)
	}
}
