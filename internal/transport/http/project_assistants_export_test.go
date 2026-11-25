package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func TestProjectAssistantExportIncludesStoredRoutinesAndGroupsWithoutSecrets(t *testing.T) {
	f, _, server := newRoutineHumanFixture(t)
	ctx := context.Background()
	db := f.s.store.ProjectAssistantDatabase()
	repository := store.New(db)
	bot, err := repository.Bot(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	routine := pa.Routine{ID: "export-routine", Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID, Name: "Synthetic export routine", Instruction: "Report synthetic facts", Triggers: []pa.RoutineTrigger{{Type: "webhook"}, {Type: "event", Source: "github", Event: "push", Filters: map[string]string{"branch": "main"}}}, NextRuns: []time.Time{{}, {}}, TimeoutSeconds: 60, Progress: "Handled through #40", ProgressRevision: 1, Version: 1}
	commit := func(id string, mutation app.Mutation) {
		t.Helper()
		current, e := repository.Bot(ctx, f.key)
		if e != nil {
			t.Fatal(e)
		}
		mutation.Command = paDomainCommand(f.command(pa.EditOperation, id))
		mutation.Command.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(id)))
		mutation.ExpectedVersion = current.Version
		mutation.Receipt = pa.Receipt{ID: id, State: pa.EffectDelivered}
		if _, e = repository.Commit(ctx, mutation); e != nil {
			t.Fatal(e)
		}
	}
	commit("export-routine-save", app.Mutation{Routine: &routine})
	_, secret, err := repository.RoutineHook(ctx, routine.ID)
	if err != nil || secret == "" {
		t.Fatal("fixture did not create a hook secret", err)
	}
	run := pa.RoutineRun{ID: "export-run", RoutineID: routine.ID, Source: "webhook", Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID, State: pa.TurnSucceeded, Instruction: routine.Instruction, TimeoutSeconds: 60, CreatedAt: now, FinishedAt: &now, Published: true, DeliveryID: "synthetic-delivery", MemoryRevision: 1, Event: pa.RoutineEvent{"source": "github", "event": "push", "text": "Synthetic event", "count": json.Number("9007199254740993"), "nested": map[string]any{"ok": true, "items": []any{"one", nil}}}}
	commit("export-run-save", app.Mutation{RoutineRun: &run})
	if err = (store.GroupInstaller{}).Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	groupKey := pa.BotKey{MachineID: bot.Key.MachineID, BotID: "export-group"}
	groups := store.NewGroupStore(db)
	_, err = groups.CreateGroup(ctx, pa.Command{Key: groupKey, Operation: pa.CreateOperation, IdempotencyKey: "export-group-create", ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic-group")))}, pa.GroupDraft{Key: groupKey, Project: bot.Project, ConversationID: "room/export", Name: "Synthetic export room", Members: []pa.BotKey{bot.Key}}, pa.GroupLimits{MaxRounds: 3, MaxReplies: 10, MaxLateTurns: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = groups.UpdateRoom(ctx, groupKey, 0, func(g pa.Group, room pa.RoomState) (pa.RoomState, error) {
		next, _, e := pa.StartRoomTurn(g, room)
		if e != nil {
			return room, e
		}
		return pa.MarkOverdue(next, pa.RoomTicket{Group: groupKey, Member: bot.Key, Epoch: next.Epoch, JoinedRevision: 1}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := repository.Export(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Routines) != 1 || len(want.RoutineRuns) != 1 || len(want.Groups) != 1 || want.Routines[0].Progress == "" || want.GroupSchemaVersion != store.GroupSchemaVersion {
		t.Fatal("incomplete stored fixture")
	}
	compare := func(t *testing.T, raw []byte) {
		t.Helper()
		if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte(`"secret"`)) || bytes.Contains(raw, []byte(`"proof"`)) {
			t.Fatal("export leaked hook authentication material")
		}
		var got map[string]json.RawMessage
		if e := json.Unmarshal(raw, &got); e != nil {
			t.Fatal(e)
		}
		for name, value := range map[string]any{"routines": want.Routines, "routine_runs": want.RoutineRuns, "groups": want.Groups, "group_schema_version": want.GroupSchemaVersion} {
			expected, e := json.Marshal(value)
			if e != nil {
				t.Fatal(e)
			}
			var left, right any
			leftDecoder := json.NewDecoder(bytes.NewReader(got[name]))
			leftDecoder.UseNumber()
			if e = leftDecoder.Decode(&left); e != nil {
				t.Errorf("export missing %s: %v", name, e)
				continue
			}
			rightDecoder := json.NewDecoder(bytes.NewReader(expected))
			rightDecoder.UseNumber()
			if e = rightDecoder.Decode(&right); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(left, right) {
				t.Errorf("export %s differs field-by-field from Store.Export: got %s want %s", name, got[name], expected)
			}
		}
		// A viewer built before these additions still decodes its known fields.
		var old struct {
			ContractVersion string `json:"contract_version"`
			SchemaVersion   int    `json:"schema_version"`
			Bot             struct {
				Key pa.BotKey `json:"key"`
			} `json:"bot"`
		}
		if e := json.Unmarshal(raw, &old); e != nil || old.ContractVersion != want.ContractVersion || old.SchemaVersion != want.SchemaVersion || old.Bot.Key != want.Bot.Key {
			t.Fatal("old export viewer compatibility changed", e)
		}
	}
	t.Run("HTTP", func(t *testing.T) {
		status, raw := routineHumanTCP(t, f, server, "GET", "/v1/project-assistants/"+f.key.BotID+"/export?machine_id="+f.key.MachineID, f.readonly, nil)
		if status != 200 {
			t.Fatalf("export HTTP %d: %s", status, raw)
		}
		compare(t, raw)
	})
	t.Run("Cloud", func(t *testing.T) {
		f.pin(t, "export-reader", "readonly", false)
		bridge := cloudops.Bridge{MachineID: f.identity.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}}
		request, _ := json.Marshal(map[string]any{"type": "project-assistants.export", "session": cloudops.MachineReplySession, "request": "export-check", "machine_id": f.key.MachineID, "bot_id": f.key.BotID})
		answer := bridge.Handle(ctx, cloudops.Command{Channel: "ctl/" + f.identity.MachineID, Class: cloudops.ClassCtl, Sender: "export-reader", Sequence: 1, Plaintext: request})
		if !answer.OK() {
			t.Fatalf("Cloud export refused: %s", answer.Payload)
		}
		var envelope struct {
			Body json.RawMessage `json:"body"`
		}
		if e := json.Unmarshal(answer.Payload, &envelope); e != nil {
			t.Fatal(e)
		}
		compare(t, envelope.Body)
	})
}
