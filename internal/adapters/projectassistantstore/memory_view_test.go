package projectassistantstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strings"
	"testing"
)

func TestSettingsSchemaSixPreservesVersionFiveRowsAndArchives(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "legacy-settings")
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=5`); err != nil {
		t.Fatal(err)
	}
	var before, after []byte
	if err := db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, b.Key.MachineID, b.Key.BotID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, b.Key.MachineID, b.Key.BotID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("metadata migration rewrote legacy JSON")
	}
	x, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	x.Manifest.SchemaVersion = 5
	x.Data.SchemaVersion = 5
	x.Manifest.SHA256, err = digest(x.Data)
	if err != nil {
		t.Fatal(err)
	}
	_, target, _ := testDB(t)
	if err = target.RestoreArchive(testContext, x); err != nil {
		t.Fatal("v5 archive refused", err)
	}
	got, err := target.Bot(testContext, b.Key)
	if err != nil || got.Notify != "" || got.RuntimeSettings.Model != "" {
		t.Fatal("legacy defaults changed", got, err)
	}
}

func TestMemoryProjectionIsBoundedAndForgettingRemovesItsDetail(t *testing.T) {
	db, s, _ := testDB(t)
	bot := seed(t, s, "memory-view")
	revision := int64(1)
	for i := 0; i < pa.MaxMemoryEntries; i++ {
		id := fmt.Sprintf("note-%03d", i)
		entry := pa.MemoryEntry{ID: id, Key: bot.Key, Project: bot.Project, Revision: revision, Text: strings.Repeat("測試", 200), Sources: []pa.MemorySource{{ID: id, Key: bot.Key, Project: bot.Project, Kind: "human", Revision: revision}}}
		m, err := s.Save(testContext, app.MemoryWrite{Command: command(bot.Key, pa.MemorySaveOperation, id), Entry: entry, ExpectedRevision: revision})
		if err != nil {
			t.Fatal(err)
		}
		revision = m.Revision
	}
	_, layer, _, err := s.ReadMemory(testContext, bot.Key, "")
	if err != nil || !layer.Available || layer.Truncated || len(layer.Entries) != pa.MaxMemoryEntries || layer.Used != pa.MaxMemoryEntries || len([]rune(layer.Entries[0].Text)) != pa.MaxMemoryPreviewRunes {
		t.Fatalf("bounded projection %+v %v", layer, err)
	}
	_, _, text, err := s.ReadMemory(testContext, bot.Key, "note-000")
	if err != nil || len([]rune(text)) != 400 {
		t.Fatalf("long detail len=%d %v", len(text), err)
	}
	_, err = s.Forget(testContext, app.ForgetRequest{Command: command(bot.Key, pa.ForgetOperation, "forget-view"), Project: bot.Project, ExpectedRevision: revision, SourceIDs: []string{"note-000"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.ReadMemory(testContext, bot.Key, "note-000"); pa.Code(err) != pa.NotFound {
		t.Fatalf("forgotten detail=%v", err)
	}
	// Historical tombstones may outlive the live-entry cap. The view exposes
	// their real count without transferring those private source identifiers.
	ids := make([]string, 10000)
	for i := range ids {
		ids[i] = fmt.Sprintf("synthetic-historical-source-%d", i)
	}
	raw, _ := json.Marshal(ids)
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Memory.forgotten_source_ids',json(?))`, string(raw)); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := s.ReadMemory(testContext, bot.Key, "")
	if err != nil || state.ForgottenCount != int64(len(ids)) {
		t.Fatal("historical forget count", state, err)
	}
	encoded, _ := json.Marshal(state)
	if strings.Contains(string(encoded), ids[0]) {
		t.Fatal("bounded view transferred historical tombstone IDs")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.ReadMemory(testContext, bot.Key, ""); err == nil {
		t.Fatal("unreadable database looked empty")
	}
}
