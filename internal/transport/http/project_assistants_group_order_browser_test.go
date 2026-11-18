package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestGroupMemberOrderBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_ORDER_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_ORDER_ARTIFACTS and PLAYWRIGHT_MODULE")
	}
	f := newGroupHTTPFixture(t)
	bob := f.bots[1]
	changed, _, err := f.service.Edit(t.Context(), pa.Command{Key: bob.Key, Operation: pa.EditOperation, IdempotencyKey: "order-bob-appearance"}, pa.EditBot{ExpectedVersion: bob.Version, Name: bob.Name, Appearance: pa.Appearance{Shape: pa.Squircle, Color: "#b39af2"}, Instructions: bob.Instructions.Text})
	if err != nil {
		t.Fatal(err)
	}
	f.bots[1] = changed
	key := pa.BotKey{MachineID: "machine", BotID: "charlie"}
	charlie, _, err := f.service.Create(t.Context(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "order-charlie"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "machine", ProjectID: "project"}, Name: "Charlie", Appearance: pa.Appearance{Shape: pa.Hex, Color: "#75c6a5"}, Agent: pa.Codex, PermissionPolicy: pa.Ask})
	if err != nil {
		t.Fatal(err)
	}
	f.bots = append(f.bots, charlie)
	group := f.create(t)
	edit := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "seed-opposite-order"), Settings: contract.ProjectAssistantGroupEditSettings{ExpectedVersion: group.Version, Name: group.Name, Members: []contract.ProjectAssistantBotKey{paWireBotKey(bob.Key), paWireBotKey(f.bots[0].Key)}}}
	if status, body := f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", edit); status != 200 {
		t.Fatalf("seed member order: %d %s", status, body)
	}
	state := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := f.service.GroupHistory(r.Context(), app.GroupHistoryQuery{Key: pa.BotKey{MachineID: "machine", BotID: f.groupID}, PageSize: 100})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		catalog := pa.GroupCatalog{Bots: map[pa.BotKey]pa.Bot{}}
		for _, bot := range f.bots {
			catalog.Bots[bot.Key] = bot
		}
		speakers := pa.RoomSpeakersFor(pa.RoomSpeakers(page.Group, catalog), []pa.RoomEntry{{Seq: 1, Kind: "person", Text: "@all"}}, false, 0, map[pa.BotKey]int64{})
		keys := []pa.BotKey{}
		for _, speaker := range speakers {
			keys = append(keys, speaker.Key)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"group": page.Group, "first_round": keys, "history_lines": len(page.Lines)})
	}))
	defer state.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-order-viewer", Sequence: 1, Plaintext: data})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-group-order.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+f.server.URL, "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_ORDER_STATE="+state.URL, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("group order: %v\n%s", err, output)
	}
	for _, bot := range f.bots {
		snapshot, err := f.repo.Export(t.Context(), bot.Key)
		if err != nil || len(snapshot.Turns) != 0 {
			t.Fatal("order editing created an execution turn", bot.Key, err)
		}
	}
	t.Log(string(output))
}
