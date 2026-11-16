package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The consumer receives authenticated TCP responses from the real Host and
// empty SQL fixture. No handcrafted successful event bypasses that boundary.
func TestProjectAssistantMetadataEventCursor(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node 24 is required", err)
	}
	f := newPAServerFixture(t, true)
	f.enable(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	raw, err := json.Marshal(f.create())
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", server.URL+"/v1/project-assistants", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.local)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "create")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("create: HTTP %d", response.StatusCode)
	}
	// A synthetic stored message lets the real read command establish a positive
	// reader observation without starting a provider or changing metadata events.
	store := featurestore.New(f.s.store.ProjectAssistantDatabase())
	x, err := store.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: x.Bot.Key, Project: x.Bot.Project, ConversationID: x.Bot.ConversationID}
	x.Messages = []pa.Message{{ID: "stored-message", Fence: fence, Seq: 1, Kind: pa.AssistantMessage, Text: "Synthetic stored fact", At: time.Now().UTC()}}
	messages, err := json.Marshal(x.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',1) WHERE machine=? AND bot=?`, string(messages), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	malformed := make([]any, 0)
	for _, cursor := range []pa.ReadCursor{{ReaderID: "foreign"}, {MessageSeq: 1}, {Key: pa.BotKey{MachineID: "foreign"}}, {ConversationID: "foreign"}} {
		projected, projectionErr := paWireBotSummary(pa.BotSummary{Bot: x.Bot, ReadCursor: cursor})
		if projectionErr != nil {
			t.Fatal(projectionErr)
		}
		malformed = append(malformed, projected.ReadCursor)
	}
	boot, err := json.Marshal(map[string]any{"url": server.URL, "token": f.local, "key": paWireBotKey(f.key), "malformed": malformed})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(root, "internal/transport/http/testdata/project-assistants-event-cursor.mjs"), root)
	cmd.Stdin = bytes.NewReader(boot)
	output, runErr := cmd.CombinedOutput()
	t.Log(string(output))
	// These are source facts even on the intentionally red pre-fix run.
	x, err = store.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range x.Events {
		if event.Bot != nil && event.Bot.ReadCursor != (pa.ReadCursor{}) {
			t.Fatalf("metadata event acquired a reader observation: %+v", event.Bot.ReadCursor)
		}
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("metadata initialized a provider")
	}
	if runErr != nil {
		t.Fatal("actual TypeScript consumer:", runErr)
	}
}

func TestProjectAssistantNonzeroSummaryCursorIsPreserved(t *testing.T) {
	key := pa.BotKey{MachineID: "synthetic-machine", BotID: "synthetic-bot"}
	bot := pa.Bot{Key: key, ConversationID: "conversation"}
	for _, cursor := range []pa.ReadCursor{
		{Key: key, ConversationID: bot.ConversationID, ReaderID: "observed-reader", MessageSeq: 7},
		{Key: pa.BotKey{MachineID: "foreign"}},
		{ConversationID: "foreign"},
		{ReaderID: "foreign"},
		{MessageSeq: 1},
		{MessageSeq: -1},
	} {
		got, err := paWireBotSummary(pa.BotSummary{Bot: bot, ReadCursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.ReadCursor, paWireReadCursor(cursor)) {
			t.Fatalf("nonzero cursor changed: got %+v, input %+v", got.ReadCursor, cursor)
		}
	}
}
