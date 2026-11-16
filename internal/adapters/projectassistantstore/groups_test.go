package projectassistantstore

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Upstream Codync values; MaxMembers is this test's own, since upstream has none.
var groupLimits = pa.GroupLimits{MaxMembers: 8, MaxRounds: 3, MaxReplies: 10, MaxLateTurns: 3}
var groupTime = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

func groupDB(t *testing.T) (*sql.DB, *Store, *GroupStore, string) {
	t.Helper()
	db, s, path := testDB(t)
	if err := (GroupInstaller{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	return db, s, NewGroupStore(db), path
}

// seedIn stores a real Bot row in scope, through the ordinary Store commit.
func seedIn(t *testing.T, s *Store, id string, scope pa.ProjectScope, hidden bool) pa.Bot {
	t.Helper()
	b := pa.Bot{Key: pa.BotKey{MachineID: scope.MachineID, BotID: id}, Project: scope, ConversationID: "chat/" + id, Name: "Bot " + id, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Hidden: hidden, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Be helpful"}, Version: 1, CreatedAt: groupTime, UpdatedAt: groupTime}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	summary := pa.BotSummary{Bot: b}
	_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.CreateOperation, "create/"+id), Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "created/" + id, State: pa.EffectDelivered, ResourceID: id}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "created/" + id, Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.BotCreated, Bot: &summary}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sha256Sum(s string) [32]byte { return sha256.Sum256([]byte(s)) }

func gk(id string) pa.BotKey { return pa.BotKey{MachineID: "machine", BotID: id} }

func groupCmd(key pa.BotKey, op pa.Operation, id, payload string) pa.Command {
	c := command(key, op, id)
	c.ContentHash = fmt.Sprintf("%x", sha256Sum(string(op)+"/"+id+"/"+payload))
	return c
}

func TestGroupInstallerIsExplicitAndRefusesUnknownVersions(t *testing.T) {
	db, _, _ := testDB(t)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='project_assistant_groups'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("feature install created group tables: %d %v", n, err)
	}
	for range 2 {
		if err := (GroupInstaller{}).Install(testContext, db); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE project_assistant_group_schema SET version=9`); err != nil {
		t.Fatal(err)
	}
	if err := (GroupInstaller{}).Install(testContext, db); pa.Code(err) != pa.Unsupported {
		t.Fatalf("future group schema accepted: %v", err)
	}
	bare, err := sql.Open("sqlite", t.TempDir()+"/bare.db")
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	if err = (GroupInstaller{}).Install(testContext, bare); err == nil {
		t.Fatal("installed group tables without the feature schema")
	}
}

func TestGroupLifecycleAgainstStoredBots(t *testing.T) {
	db, store, groups, path := groupDB(t)
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	sibling := scope
	sibling.ProjectID = "project-2" // the same canonical repository, registered as another Project
	a := seedIn(t, store, "a", scope, false)
	b := seedIn(t, store, "b", scope, false)
	h := seedIn(t, store, "h", scope, true)
	seedIn(t, store, "x", sibling, false)
	draft := pa.GroupDraft{Key: gk("g"), Project: scope, ConversationID: "room/g", Name: "Room", Members: []pa.BotKey{a.Key, h.Key}}

	for _, c := range []struct {
		name    string
		members []pa.BotKey
		code    pa.ErrorCode
	}{
		{"other Project", []pa.BotKey{a.Key, gk("x")}, pa.ScopeDenied},
		{"duplicate", []pa.BotKey{a.Key, a.Key}, pa.Invalid},
		{"missing", []pa.BotKey{a.Key, gk("none")}, pa.NotFound},
		{"foreign machine", []pa.BotKey{a.Key, {MachineID: "other", BotID: "a"}}, pa.ScopeDenied},
	} {
		d := draft
		d.Members = c.members
		if _, err := groups.CreateGroup(testContext, groupCmd(d.Key, pa.CreateOperation, "bad-"+c.name, c.name), d, groupLimits, groupTime); pa.Code(err) != c.code {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	create := groupCmd(draft.Key, pa.CreateOperation, "create-g", "a,h")
	r, err := groups.CreateGroup(testContext, create, draft, groupLimits, groupTime)
	if err != nil || r.ResourceID != "g" {
		t.Fatalf("%+v %v", r, err)
	}
	if again, err := groups.CreateGroup(testContext, create, draft, groupLimits, groupTime); err != nil || again != r {
		t.Fatalf("replay %+v %v", again, err)
	}
	changed := create
	changed.ContentHash = fmt.Sprintf("%x", sha256Sum("changed"))
	if _, err = groups.CreateGroup(testContext, changed, draft, groupLimits, groupTime); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("changed payload: %v", err)
	}
	// The same Bots again open the group they share.
	twin := draft
	twin.Key, twin.ConversationID, twin.Members = gk("g-twin"), "room/twin", []pa.BotKey{h.Key, a.Key}
	if r2, err := groups.CreateGroup(testContext, groupCmd(twin.Key, pa.CreateOperation, "create-twin", "h,a"), twin, groupLimits, groupTime); err != nil || r2.ResourceID != "g" {
		t.Fatalf("same members: %+v %v", r2, err)
	}
	if _, err = groups.Group(testContext, twin.Key); pa.Code(err) != pa.NotFound {
		t.Fatal("same members created a second group")
	}
	// A group cannot be nested, nor use a Bot's key.
	nested := pa.GroupDraft{Key: gk("g2"), Project: scope, ConversationID: "room/g2", Name: "Nested", Members: []pa.BotKey{b.Key, gk("g")}}
	if _, err = groups.CreateGroup(testContext, groupCmd(nested.Key, pa.CreateOperation, "nested", ""), nested, groupLimits, groupTime); pa.Code(err) != pa.Invalid {
		t.Fatalf("nested: %v", err)
	}
	asBot := pa.GroupDraft{Key: b.Key, Project: scope, ConversationID: "room/b", Name: "Bot", Members: []pa.BotKey{a.Key}}
	if _, err = groups.CreateGroup(testContext, groupCmd(b.Key, pa.CreateOperation, "as-bot", ""), asBot, groupLimits, groupTime); pa.Code(err) != pa.Invalid {
		t.Fatalf("group as Bot: %v", err)
	}

	// Room turn, membership change and stale tickets, stored.
	st, err := groups.UpdateRoom(testContext, gk("g"), 0, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) {
		next, _, err := pa.StartRoomTurn(g, s)
		return next, err
	})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := groups.Group(testContext, gk("g"))
	ticketA, _ := pa.TicketFor(g, st, a.Key, false)
	ticketH, _ := pa.TicketFor(g, st, h.Key, true)
	if err = groups.CheckTicket(testContext, ticketA); err != nil {
		t.Fatal(err)
	}
	if _, err = groups.UpdateRoom(testContext, gk("g"), 0, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) { return s, nil }); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("room CAS: %v", err)
	}
	st, err = groups.UpdateRoom(testContext, gk("g"), st.Revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) {
		next, _, err := pa.StartRoomTurn(g, s)
		return next, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = groups.CheckTicket(testContext, ticketA); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("stale epoch: %v", err)
	}
	if err = groups.CheckTicket(testContext, ticketH); err != nil {
		t.Fatalf("late ticket after a newer turn: %v", err)
	}
	edit := groupCmd(gk("g"), pa.EditOperation, "edit-1", "a,b")
	if _, err = groups.EditGroup(testContext, edit, pa.GroupEdit{ExpectedVersion: 1, Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	if _, err = groups.EditGroup(testContext, groupCmd(gk("g"), pa.EditOperation, "edit-stale", "a"), pa.GroupEdit{ExpectedVersion: 1, Name: "Room", Members: []pa.BotKey{a.Key}}, groupLimits, groupTime); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("stale edit: %v", err)
	}
	if err = groups.CheckTicket(testContext, ticketH); pa.Code(err) != pa.Revoked {
		t.Fatalf("removed member's late answer: %v", err)
	}

	// Deleting a member Bot: it leaves the group; its pending ticket is revoked.
	st, _ = groups.Room(testContext, gk("g"))
	g, _ = groups.Group(testContext, gk("g"))
	ticketB, _ := pa.TicketFor(g, st, b.Key, true)
	if err = store.DeleteBot(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	if err = groups.CheckTicket(testContext, ticketB); pa.Code(err) != pa.Revoked {
		t.Fatalf("deleted member's ticket: %v", err)
	}
	if left, err := groups.LeaveGroups(testContext, b.Key, groupTime); err != nil || len(left) != 1 {
		t.Fatalf("leave %v %v", left, err)
	}

	// Reopen: everything is read back from the file.
	db.Close()
	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err = (GroupInstaller{}).Install(testContext, db2); err != nil {
		t.Fatal(err)
	}
	groups, store = NewGroupStore(db2), New(db2)
	g, err = groups.Group(testContext, gk("g"))
	if err != nil || len(g.Members) != 1 || g.Members[0].Key != a.Key || g.Version != 3 {
		t.Fatalf("reopened group %+v %v", g, err)
	}
	if again, err := groups.EditGroup(testContext, edit, pa.GroupEdit{ExpectedVersion: 1, Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}, groupLimits, groupTime); err != nil || again.ResourceID != "g" {
		t.Fatalf("edit replay after reopen: %+v %v", again, err)
	}
	st, _ = groups.Room(testContext, gk("g"))
	if st.Epoch != 2 {
		t.Fatalf("reopened room %+v", st)
	}

	// Group Stop drops late answers; deleting the group keeps member Bots.
	ticketA, _ = pa.TicketFor(g, st, a.Key, true)
	st, err = groups.UpdateRoom(testContext, gk("g"), st.Revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) {
		next, _, err := pa.StopRoom(s, groupLimits)
		return next, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = groups.CheckTicket(testContext, ticketA); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("late answer after group Stop: %v", err)
	}
	del := groupCmd(gk("g"), pa.DeleteOperation, "delete", "")
	if _, err = groups.DeleteGroup(testContext, del, g.Version, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	if again, err := groups.DeleteGroup(testContext, del, g.Version, groupLimits, groupTime); err != nil || again.ResourceID != "g" {
		t.Fatalf("delete replay %v", err)
	}
	for _, k := range []pa.BotKey{a.Key, h.Key} {
		bot, err := store.Bot(testContext, k)
		if err != nil || bot.ConversationID != "chat/"+k.BotID {
			t.Fatalf("member %s after group delete: %v", k.BotID, err)
		}
		if _, err = store.Conversation(testContext, k, bot.ConversationID); err != nil {
			t.Fatalf("member conversation %s: %v", k.BotID, err)
		}
	}
	if g, _ = groups.Group(testContext, gk("g")); !g.Deleted {
		t.Fatal("not tombstoned")
	}
	ticket := pa.RoomTicket{Group: gk("g"), Member: a.Key, Epoch: st.Epoch, JoinedRevision: 1}
	if err = groups.CheckTicket(testContext, ticket); pa.Code(err) != pa.NotFound {
		t.Fatalf("deleted group published: %v", err)
	}
	if _, err = groups.UpdateRoom(testContext, gk("g"), st.Revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) { return s, nil }); pa.Code(err) != pa.NotFound {
		t.Fatalf("room write to deleted group: %v", err)
	}
	st2, _ := groups.Room(testContext, gk("g"))
	if st2.Epoch <= st.Epoch {
		t.Fatalf("deleting the group did not move the room epoch: %+v", st2)
	}
}

func TestGroupWriteFailureRollsBackTheWholeMutation(t *testing.T) {
	for _, stage := range []string{"group", "members", "room", "receipt"} {
		t.Run(stage, func(t *testing.T) {
			db, store, groups, _ := groupDB(t)
			scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
			a := seedIn(t, store, "a", scope, false)
			injected := errors.New("injected " + stage)
			groups.hook = func(s string) error {
				if s == stage {
					return injected
				}
				return nil
			}
			d := pa.GroupDraft{Key: gk("g"), Project: scope, ConversationID: "room/g", Name: "Room", Members: []pa.BotKey{a.Key}}
			c := groupCmd(d.Key, pa.CreateOperation, "create", "a")
			if _, err := groups.CreateGroup(testContext, c, d, groupLimits, groupTime); !errors.Is(err, injected) {
				t.Fatalf("want injected failure, got %v", err)
			}
			for _, table := range []string{"project_assistant_groups", "project_assistant_group_members", "project_assistant_group_rooms", "project_assistant_group_receipts"} {
				var n int
				if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
					t.Fatalf("%s kept %d rows after failure (%v)", table, n, err)
				}
			}
			groups.hook = nil
			if r, err := groups.CreateGroup(testContext, c, d, groupLimits, groupTime); err != nil || r.ResourceID != "g" {
				t.Fatalf("retry %+v %v", r, err)
			}
			// An edit failing at the members stage leaves the old version.
			if stage == "members" {
				b := seedIn(t, store, "b", scope, false)
				groups.hook = func(s string) error {
					if s == "members" {
						return injected
					}
					return nil
				}
				if _, err := groups.EditGroup(testContext, groupCmd(d.Key, pa.EditOperation, "edit", "a,b"), pa.GroupEdit{ExpectedVersion: 1, Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}, groupLimits, groupTime); !errors.Is(err, injected) {
					t.Fatal(err)
				}
				if g, _ := groups.Group(testContext, d.Key); g.Version != 1 || len(g.Members) != 1 {
					t.Fatalf("partial edit %+v", g)
				}
			}
		})
	}
}
