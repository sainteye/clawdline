package projectassistantstore

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var groupScope = pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}

func TestInstallSchemasAddsGroupTablesAfterTheFeature(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for range 2 {
		if err = app.InstallSchemas(testContext, db, Installer{}, GroupInstaller{}); err != nil {
			t.Fatal(err)
		}
	}
	var feature, groups int
	if err = db.QueryRow(`SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&feature); err != nil || feature != pa.SchemaVersion {
		t.Fatalf("feature schema %d %v", feature, err)
	}
	if err = db.QueryRow(`SELECT version FROM project_assistant_group_schema WHERE id=1`).Scan(&groups); err != nil || groups != (GroupInstaller{}).Version() {
		t.Fatalf("group schema %d %v", groups, err)
	}
	// Separately versioned: installing groups leaves the feature version alone.
	if feature != pa.SchemaVersion || groups != 1 {
		t.Fatalf("versions moved: feature %d groups %d", feature, groups)
	}
}

func TestDeleteBotLeavesItsGroupsInTheSameTransaction(t *testing.T) {
	_, store, groups, _ := groupDB(t)
	a := seedIn(t, store, "a", groupScope, false)
	b := seedIn(t, store, "b", groupScope, false)
	d := pa.GroupDraft{Key: gk("g"), Project: groupScope, ConversationID: "room/g", Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(d.Key, pa.CreateOperation, "create", "a,b"), d, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	// A failure while writing the membership rolls the whole deletion back.
	store.groupHook = func(stage string) error {
		if stage == "members" {
			return errors.New("injected")
		}
		return nil
	}
	if err := store.DeleteBot(testContext, a.Key); err == nil {
		t.Fatal("deletion committed past a failed group write")
	}
	if _, err := store.Bot(testContext, a.Key); err != nil {
		t.Fatalf("Bot row gone after rollback: %v", err)
	}
	if g, _ := groups.Group(testContext, d.Key); len(g.Members) != 2 {
		t.Fatalf("membership changed after rollback: %+v", g.Members)
	}
	store.groupHook = nil
	if err := store.DeleteBot(testContext, a.Key); err != nil {
		t.Fatal(err)
	}
	g, err := groups.Group(testContext, d.Key)
	if err != nil || len(g.Members) != 1 || g.Members[0].Key != b.Key || g.MembershipRevision != 2 {
		t.Fatalf("after delete: %+v %v", g, err)
	}
}

func TestDeleteBotWithoutGroupTablesStillDeletes(t *testing.T) {
	_, store, _ := testDB(t)
	a := seed(t, store, "a")
	if err := store.DeleteBot(testContext, a.Key); err != nil {
		t.Fatal(err)
	}
}

func TestExportCarriesGroupsAndRestoreRefusesLiveOnes(t *testing.T) {
	_, store, groups, _ := groupDB(t)
	a := seedIn(t, store, "a", groupScope, false)
	b := seedIn(t, store, "b", groupScope, false)
	loner := seedIn(t, store, "c", groupScope, false)
	d := pa.GroupDraft{Key: gk("g"), Project: groupScope, ConversationID: "room/g", Name: "Room", Members: []pa.BotKey{a.Key, b.Key}}
	if _, err := groups.CreateGroup(testContext, groupCmd(d.Key, pa.CreateOperation, "create", "a,b"), d, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	x, err := store.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Groups) != 1 || x.GroupSchemaVersion != GroupSchemaVersion || x.Groups[0].Group.Key != d.Key || len(x.Groups[0].Group.Members) != 2 || x.Groups[0].Room.Group != d.Key || x.Groups[0].Group.Deleted {
		t.Fatalf("export groups %+v (version %d)", x.Groups, x.GroupSchemaVersion)
	}
	if none, err := store.Export(testContext, loner.Key); err != nil || none.Groups != nil || none.GroupSchemaVersion != 0 {
		t.Fatalf("a Bot in no group exported %+v %v", none.Groups, err)
	}
	_, empty, _ := testDB(t)
	if err = empty.Restore(testContext, x); pa.Code(err) != pa.Unsupported {
		t.Fatalf("restored a live group membership: %v", err)
	}
	if _, err = groups.DeleteGroup(testContext, groupCmd(d.Key, pa.DeleteOperation, "delete", "1"), 1, groupLimits, groupTime); err != nil {
		t.Fatal(err)
	}
	x, err = store.Export(testContext, a.Key)
	if err != nil || len(x.Groups) != 1 || !x.Groups[0].Group.Deleted {
		t.Fatalf("tombstone not marked: %+v %v", x.Groups, err)
	}
	if err = empty.Restore(testContext, x); err != nil {
		t.Fatalf("a tombstone blocked restore: %v", err)
	}
	stale := x
	stale.GroupSchemaVersion = 9
	_, other, _ := testDB(t)
	if err = other.Restore(testContext, stale); pa.Code(err) != pa.Unsupported {
		t.Fatalf("unknown group export version accepted: %v", err)
	}
}
