package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The library reads stored output snapshots and stored attachments through HTTP.
func TestProjectAssistantLibraryBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_LIBRARY_E2E_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_LIBRARY_E2E_DIR for headless library acceptance")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := libraryUIFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-library.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "library-browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("headless library: %v\n%s", err, log)
	}
	t.Log("real HTTP, SQLite, output, input and picture snapshots; zero provider/model requests")
}

func libraryFixtureCommand(key pa.BotKey, id string) pa.Command {
	return pa.Command{Key: key, Operation: pa.EditOperation, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(id)))}
}

func libraryUIFixture(t *testing.T) *paServerFixture {
	t.Helper()
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	ctx := context.Background()
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(f.s.cfg.Dir, "project-assistants", "attachments")
	var picture bytes.Buffer
	if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	attachment, err := (store.Attachments{Dir: root}).Put(f.key, "image/png", picture.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("Explicit library input.\n")
	file := inputFixtureFile("11111111-1111-4111-8111-111111111111", "library-notes.txt", data)
	fence := pa.Fence{Key: f.key, Project: x.Bot.Project, ConversationID: x.Conversation.ID, BindingID: x.Conversation.BindingID, Generation: x.Conversation.Generation}
	input := pa.InputFile{ID: file.ID, Name: file.Name, Bytes: file.Bytes, SHA256: file.Sha256}
	if _, err = (store.Inputs{Dir: root}).Put(ctx, fence, input, 0, data, true, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	outputs := store.Outputs{Dir: root}
	for _, entry := range []struct{ id, name, content, artifact string }{{"file", "result.bin", string(make([]byte, 384*1024+4)), ""}, {"artifact-v1", "First version", "<h1>First version</h1>", "library-artifact"}, {"artifact-v2", "Interactive report", outputFixtureHTML, "library-artifact"}} {
		path := filepath.Join(f.project, entry.id)
		if err = os.WriteFile(path, []byte(entry.content), 0600); err != nil {
			t.Fatal(err)
		}
		out, e := outputs.Snapshot(ctx, f.key, f.project, path, entry.artifact != "", func() error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		out.ID = entry.id
		out.Name = entry.name
		out.ArtifactID = entry.artifact
		m := pa.Message{ID: entry.id, Fence: fence, Kind: pa.AssistantMessage, Text: entry.name, Output: &out, At: time.Now()}
		if _, err = store.New(f.s.store.ProjectAssistantDatabase()).Commit(ctx, app.Mutation{Command: libraryFixtureCommand(f.key, entry.id), ExpectedVersion: x.Bot.Version, Messages: []pa.Message{m}, Receipt: pa.Receipt{ID: entry.id, State: pa.EffectDelivered}}); err != nil {
			t.Fatal(err)
		}
		x, err = store.New(f.s.store.ProjectAssistantDatabase()).Export(ctx, f.key)
		if err != nil {
			t.Fatal(err)
		}
	}
	messages := []pa.Message{{ID: "library-input", Fence: fence, Kind: pa.UserMessage, Files: []pa.InputFile{input}, Attachments: []pa.Attachment{attachment}, At: time.Now().Add(-24 * time.Hour)}}
	// The initial 100-row view cannot include the library's older shared files.
	for i := 0; i < 100; i++ {
		messages = append(messages, pa.Message{ID: fmt.Sprintf("library-text-%d", i), Fence: fence, Kind: pa.AssistantMessage, Text: "Synthetic history", At: time.Now()})
	}
	command := libraryFixtureCommand(f.key, "library-seed")
	if _, err = store.New(f.s.store.ProjectAssistantDatabase()).Commit(ctx, app.Mutation{Command: command, ExpectedVersion: x.Bot.Version, Messages: messages, Receipt: pa.Receipt{ID: "library-seeded", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}

	return f
}

func TestProjectAssistantLibraryReadOnlySnapshots(t *testing.T) {
	f := libraryUIFixture(t)
	got := f.get(t, f.readonly)
	first := paHTTPDecode[contract.ProjectAssistantReadViewResponse](t, f.request(t, "GET", paViewURL(f.key, got.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), f.readonly, nil))
	if first.Source.Writable || !first.Source.Readable || first.NextCursor == "" || len(first.Items) != 100 {
		t.Fatalf("snapshot: %+v", first)
	}
	page := paHTTPDecode[contract.ProjectAssistantReadViewResponse](t, f.request(t, "GET", paViewURL(f.key, got.Conversation.ID, pa.ReadViewHistory, first.SourceEpoch, first.NextCursor, 0, 100), f.readonly, nil))
	outputs, inputs, pictures := 0, 0, 0
	for _, item := range page.Items {
		m := item.Message
		if m == nil {
			continue
		}
		if m.Output != nil {
			outputs++
		}
		inputs += len(m.Files)
		pictures += len(m.Attachments)
	}
	if outputs != 3 || inputs != 1 || pictures != 1 {
		t.Fatalf("shared counts output/input/picture=%d/%d/%d", outputs, inputs, pictures)
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range x.Messages {
		if m.Output != nil {
			paServerOK(t, f.request(t, "GET", paOutputFixturePath(f, m, 0, m.Output.ArtifactID != ""), f.readonly, nil))
		}
		for _, file := range m.Files {
			paServerOK(t, f.request(t, "GET", inputReadPath(f, m, file, 0), f.readonly, nil))
		}
		for _, picture := range m.Attachments {
			paServerOK(t, f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/attachments/"+picture.ID+"?machine_id="+f.key.MachineID, f.readonly, nil))
		}
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("library reads invoked a provider")
	}
}
