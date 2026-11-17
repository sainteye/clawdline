package projectassistantstore

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestArtifactTitleArchiveAndReplacement(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "title-archive")
	f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}
	out := pa.Output{ID: "publication-one", Name: "Original", ArtifactID: "artifact", SHA256: fmt.Sprintf("%x", sha256.Sum256(nil))}
	m := pa.Message{ID: "card-one", Fence: f, Kind: pa.AssistantMessage, Text: "Artifact", Output: &out, At: b.CreatedAt}
	_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "publish"), ExpectedVersion: b.Version, Messages: []pa.Message{m}, Receipt: pa.Receipt{ID: "published", State: pa.EffectDelivered}})
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.Bot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	title := pa.ArtifactDisplayTitle{ArtifactID: out.ArtifactID, OutputID: out.ID, Title: "Renamed", Version: 2}
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.RenameArtifactOperation, "rename"), ExpectedVersion: b.Version, ArtifactTitle: &title, Receipt: pa.Receipt{ID: "renamed", State: pa.EffectDelivered}})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, target, _ := testDB(t)
	if err = target.RestoreArchive(testContext, archive); err != nil {
		t.Fatal(err)
	}
	restored, err := target.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Messages, archive.Data.Messages) || !reflect.DeepEqual(restored.ArtifactTitles, archive.Data.ArtifactTitles) {
		t.Fatal("restore changed immutable publications or lost titles")
	}
	if got := app.PublishedOutputs(restored.DisplayMessages()); len(got) != 1 || got[0].Name != "Renamed" || got[0].TitleVersion != 2 {
		t.Fatal("native list projection", got)
	}
	out.ID, out.Name = "publication-two", "Replacement"
	m.ID, m.Output = "card-two", &out
	_, err = target.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "replace"), ExpectedVersion: restored.Bot.Version, Messages: []pa.Message{m}, Receipt: pa.Receipt{ID: "replaced", State: pa.EffectDelivered}})
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := target.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced.ArtifactTitles) != 0 {
		t.Fatal("old overlay survived replacement")
	}
	if got := app.PublishedOutputs(replaced.DisplayMessages()); len(got) != 1 || got[0].Name != "Replacement" || got[0].TitleVersion != 1 {
		t.Fatal("replacement projection", got)
	}
	if replaced.Messages[0].Output.Name != "Original" {
		t.Fatal("historical publication was rewritten")
	}
}
