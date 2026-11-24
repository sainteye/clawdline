package projectassistantstore

import (
	"bytes"
	"database/sql"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func networkWrite(bot pa.Bot) pa.NetworkSettingsWrite {
	return pa.NetworkSettingsWrite{Scope: pa.NetworkScope{AccountID: "fixture-account", Key: bot.Key, Project: bot.Project}, Fence: pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}, ExpectedBotVersion: bot.Version}
}

func TestNetworkSettingsKeepAccountScopeCASAndHistoricalBytes(t *testing.T) {
	db, store, path := testDB(t)
	bot := seed(t, store, "network-bot")
	var before []byte
	if err := db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, bot.Key.MachineID, bot.Key.BotID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	write := networkWrite(bot)
	initial, err := store.NetworkSettings(testContext, write.Scope)
	if err != nil || initial.Revision != 0 || initial.OfferedToNetwork || initial.ReachableByAssistants {
		t.Fatalf("default: %+v %v", initial, err)
	}
	on := true
	write.Patch = pa.NetworkSettingsPatch{OfferedToNetwork: &on, ReachableByAssistants: &on}
	ready := pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkReady}, Assistants: pa.NetworkCapability{State: pa.NetworkReady}}
	first, err := store.SaveNetworkSettings(testContext, write, ready)
	if err != nil || first.Revision != 1 || !first.OfferedToNetwork || !first.ReachableByAssistants {
		t.Fatalf("save: %+v %v", first, err)
	}
	_, err = store.SaveNetworkSettings(testContext, write, ready)
	requireCode(t, err, pa.StaleVersion)
	write.ExpectedRevision = 1
	write.Patch = pa.NetworkSettingsPatch{}
	preserved, err := store.SaveNetworkSettings(testContext, write, pa.NetworkCapabilities{})
	if err != nil || preserved != first {
		t.Fatalf("omission: %+v %v", preserved, err)
	}
	wrongAccount := write.Scope
	wrongAccount.AccountID = "other-account"
	isolated, err := store.NetworkSettings(testContext, wrongAccount)
	if err != nil || isolated.Revision != 0 || isolated.OfferedToNetwork || isolated.ReachableByAssistants {
		t.Fatalf("account crossed: %+v %v", isolated, err)
	}
	wrongProject := write.Scope
	wrongProject.Project.ProjectID = "other-project"
	_, err = store.NetworkSettings(testContext, wrongProject)
	requireCode(t, err, pa.ScopeDenied)
	stale := write
	stale.Fence.Generation++
	_, err = store.SaveNetworkSettings(testContext, stale, ready)
	requireCode(t, err, pa.StaleGeneration)
	// Chat activity advances Bot.Version; the settings revision alone guards a toggle.
	busy := write
	busy.ExpectedBotVersion--
	off := false
	busy.Patch = pa.NetworkSettingsPatch{ReachableByAssistants: &off}
	toggled, err := store.SaveNetworkSettings(testContext, busy, ready)
	if err != nil || toggled.Revision != 2 || toggled.ReachableByAssistants || !toggled.OfferedToNetwork {
		t.Fatalf("toggle after Bot activity: %+v %v", toggled, err)
	}
	first = toggled
	var after []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, bot.Key.MachineID, bot.Key.BotID).Scan(&after); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("network settings rewrote historical Bot bytes: %v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := New(reopened).NetworkSettings(testContext, write.Scope)
	if err != nil || stored != first {
		t.Fatalf("restart: %+v %v", stored, err)
	}
	if _, err = reopened.Exec(`DELETE FROM project_assistant_bots WHERE machine=? AND bot=?`, bot.Key.MachineID, bot.Key.BotID); err != nil {
		t.Fatal(err)
	}
	_, err = New(reopened).NetworkSettings(testContext, write.Scope)
	requireCode(t, err, pa.NotFound)
}

func TestNetworkMigrationPreservesSchemaNineAggregateAndArchive(t *testing.T) {
	db, store, _ := testDB(t)
	bot := seed(t, store, "legacy-network")
	archive, err := store.ExportArchive(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	archive.Data.SchemaVersion = 9
	archive.Manifest.SchemaVersion = 9
	archive.Manifest.SHA256, err = digest(archive.Data)
	if err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, bot.Key.MachineID, bot.Key.BotID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE project_assistant_network_settings; UPDATE project_assistant_schema SET version=9`); err != nil {
		t.Fatal(err)
	}
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	var after []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, bot.Key.MachineID, bot.Key.BotID).Scan(&after); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("migration rewrote historical aggregate: %v", err)
	}
	_, restored, _ := testDB(t)
	if err = restored.RestoreArchive(testContext, archive); err != nil {
		t.Fatalf("schema9 archive: %v", err)
	}
	settings, err := restored.NetworkSettings(testContext, networkWrite(bot).Scope)
	if err != nil || settings.Revision != 0 || settings.OfferedToNetwork || settings.ReachableByAssistants {
		t.Fatalf("archive restored network authority: %+v %v", settings, err)
	}
}
