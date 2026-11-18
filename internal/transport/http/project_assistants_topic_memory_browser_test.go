package http

import (
	"context"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
)

// Production Chat, memory editor and SourceClient use a real isolated Host/store.
// This scenario edits metadata only and sends no native prompt.
func TestTopicMemoryEditorBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_TOPIC_MEMORY_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_TOPIC_MEMORY_ARTIFACTS and PLAYWRIGHT_MODULE for the topic editor")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, _ := memorySettingsFixture(t)
	got := f.get(t, f.local)
	seed := contract.ProjectAssistantMemoryWriteRequest{Command: f.command(pa.MemorySaveOperation, "seed-topic-readonly"), ExpectedRevision: 2, Entry: contract.ProjectAssistantMemoryEntry{ID: "readonly-topic", Key: got.Bot.Key, Project: got.Bot.Project, Kind: pa.TopicMemory, Title: "Readonly topic", Summary: "An authorized index", Text: "READONLY-TOPIC-BODY", Sources: []contract.ProjectAssistantMemorySource{}}}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory", f.local, seed))
	server := httptest.NewServer(f.h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-topic-memory.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("topic memory editor: %v\n%s", err, output)
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Turns) != 0 {
		t.Fatal("metadata scenario started a native turn")
	}
	t.Log(string(output))
}
