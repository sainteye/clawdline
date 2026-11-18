package projectassistantstore

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// JSON exercises the persisted shape as well as its old-entry defaults.
func topicMemoryWrite(t *testing.T, b pa.Bot, revision int64, id, body string) app.MemoryWrite {
	t.Helper()
	write := memoryWrite(b, revision, id, "source-"+id, body)
	raw, err := json.Marshal(write.Entry)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["kind"] = "note"
	fields["title"] = "Release checklist"
	fields["summary"] = "Deployment readiness steps"
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &write.Entry); err != nil {
		t.Fatal(err)
	}
	return write
}

func TestTopicNoteIndexDetailAndForgetAreOneScopedMemory(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "topic-memory")
	other := seed(t, s, "another-memory")
	body := strings.Repeat("BODY-ONLY-TOPIC-DETAIL ", 300)
	write := topicMemoryWrite(t, b, 1, "release-note", body)
	revision, err := s.Save(testContext, write)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.Text, "BODY-ONLY-TOPIC-DETAIL") || !strings.Contains(snapshot.Text, "Release checklist") || !strings.Contains(snapshot.Text, "Deployment readiness steps") || !strings.Contains(snapshot.Text, "release-note") {
		t.Fatal("topic memory must expose its name and summary index without injecting the body")
	}
	_, layer, _, err := s.ReadMemory(testContext, b.Key, "")
	if err != nil || len(layer.Entries) != 1 || layer.Entries[0].Text != "Deployment readiness steps" {
		t.Fatal("note preview is not its summary", layer, err)
	}
	_, _, detail, err := s.ReadMemory(testContext, b.Key, "release-note")
	if err != nil || detail != body {
		t.Fatal("on-demand body was lost", err)
	}
	if _, _, _, err = s.ReadMemory(testContext, other.Key, "release-note"); pa.Code(err) != pa.NotFound {
		t.Fatal("another Bot read a topic note", err)
	}
	stale := write
	stale.Command = command(b.Key, pa.MemorySaveOperation, "stale-note-edit")
	if _, err = s.Save(testContext, stale); pa.Code(err) != pa.StaleVersion {
		t.Fatal("stale note edit was accepted", err)
	}
	revision, err = s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget-note"), Project: b.Project, ExpectedRevision: revision.Revision, SourceIDs: []string{"release-note"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.Snapshot(testContext, b.Key)
	if err != nil || snapshot.Text != "" {
		t.Fatal("forgotten note index survived", snapshot, err)
	}
	_, layer, _, err = s.ReadMemory(testContext, b.Key, "")
	if err != nil || len(layer.Entries) != 0 {
		t.Fatal("forgotten note remained listed", err)
	}
	if _, _, _, err = s.ReadMemory(testContext, b.Key, "release-note"); pa.Code(err) != pa.NotFound {
		t.Fatal("forgotten body remained readable", err)
	}
	var stored []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, b.Key.MachineID, b.Key.BotID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "BODY-ONLY-TOPIC-DETAIL") || strings.Contains(string(stored), "Release checklist") || strings.Contains(string(stored), "Deployment readiness steps") {
		t.Fatal("forgotten note body or index remains in live storage")
	}
	resurrect := topicMemoryWrite(t, b, revision.Revision, "release-note", body)
	resurrect.Command = command(b.Key, pa.MemorySaveOperation, "resurrect-note")
	if _, err = s.Save(testContext, resurrect); pa.Code(err) != pa.ContextStale {
		t.Fatal("forgotten note resurrected", err)
	}
}

func TestTopicMemoryMigrationKeepsSchemaElevenShortFactsAndArchives(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "legacy-topic-memory")
	if _, err := s.Save(testContext, memoryWrite(b, 1, "old-short-fact", "old-source", "A retained short fact")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=11`); err != nil {
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
		t.Fatal("topic memory migration rewrote old Bot/history data")
	}
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	archive.Manifest.SchemaVersion = 11
	archive.Data.SchemaVersion = 11
	archive.Manifest.SHA256, err = digest(archive.Data)
	if err != nil {
		t.Fatal(err)
	}
	_, restored, _ := testDB(t)
	if err = restored.RestoreArchive(testContext, archive); err != nil {
		t.Fatal("legacy short-fact archive refused", err)
	}
	snapshot, err := restored.Snapshot(testContext, b.Key)
	if err != nil || snapshot.Text != "A retained short fact" {
		t.Fatal("legacy short fact changed", snapshot, err)
	}
	note := topicMemoryWrite(t, b, 2, "new-topic", "A topic body")
	if _, err = s.Save(testContext, note); err != nil {
		t.Fatal(err)
	}
	next, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if next.Manifest.SchemaVersion != 12 {
		t.Fatal("named note archive lacks its protecting schema")
	}
	next.Manifest.SchemaVersion = 11
	next.Data.SchemaVersion = 11
	next.Manifest.SHA256, err = digest(next.Data)
	if err != nil {
		t.Fatal(err)
	}
	_, wrong, _ := testDB(t)
	if err = wrong.RestoreArchive(testContext, next); pa.Code(err) != pa.Unsupported {
		t.Fatal("a topic note was mislabeled as a legacy archive", err)
	}
}

func TestTopicIndexDoesNotDecodeChatOrReturnItsBody(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "topic-index-only")
	if _, err := s.Save(testContext, topicMemoryWrite(t, b, 1, "topic", "BODY-ONLY-TOPIC-DETAIL")); err != nil {
		t.Fatal(err)
	}
	// An unrelated damaged chat must not be decoded to read the memory index.
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Messages',json('["unrelated damaged chat"]')) WHERE machine=? AND bot=?`, b.Key.MachineID, b.Key.BotID); err != nil {
		t.Fatal(err)
	}
	rows, err := s.MemoryEntries(testContext, b.Key)
	if err != nil || len(rows) != 1 || rows[0].Text != "Deployment readiness steps" || rows[0].Title != "Release checklist" {
		t.Fatal("compact note index failed", rows, err)
	}
	if strings.Contains(rows[0].Text, "BODY-ONLY") {
		t.Fatal("note body entered the index")
	}
}

func TestMemoryIntentKeysRemainScopedByBot(t *testing.T) {
	_, s, _ := testDB(t)
	for _, id := range []string{"first-memory-owner", "second-memory-owner"} {
		b := seed(t, s, id)
		write := memoryWrite(b, 1, "same-intent", "source", "An owned fact")
		got, err := s.Save(testContext, write)
		if err != nil || got.Revision != 2 {
			t.Fatal("one Bot intent collided with another Bot", id, got, err)
		}
		replay, err := s.Save(testContext, write)
		if err != nil || replay.Revision != 2 {
			t.Fatal("scoped replay changed its original result", id, replay, err)
		}
		changed := write
		changed.Entry.Text = "Conflicting fact"
		changed.Command.ContentHash = strings.Repeat("f", 64)
		if _, err = s.Save(testContext, changed); pa.Code(err) != pa.IdempotencyConflict {
			t.Fatal("changed memory intent was replayed", id, err)
		}
	}
}
