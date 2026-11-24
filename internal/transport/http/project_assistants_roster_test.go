package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Snapshot transport fixture: real HTTP/Service/SQLite, no local SSE and no model.
func TestProjectAssistantRosterCloudHTTP(t *testing.T) {
	testProjectAssistantRosterStatusHTTP(t, true)
}

// The native event fixture publishes through the real Store and HTTP gate. It
// starts no provider or model; the browser mounts the normal Bots component.
func TestProjectAssistantRosterStatusHTTP(t *testing.T) {
	testProjectAssistantRosterStatusHTTP(t, false)
}

func testProjectAssistantRosterStatusHTTP(t *testing.T, cloud bool) {
	f := newPAServerFixture(t, true)
	// The roster fixture supplies its own active turns. Startup recovery
	// processes the turns of a previous daemon, which this fixture does not model.
	ctx := context.Background()
	if _, err := f.s.settingsFile().Set(map[string]any{"project_assistants_enabled": true}); err != nil {
		t.Fatal(err)
	}
	h, err := f.s.ensureProjectAssistantHost(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.mu.Lock()
	f.s.projectAssistants.allowed = true
	f.s.projectAssistants.mu.Unlock()
	st := store.New(f.s.store.ProjectAssistantDatabase())
	var sequence atomic.Int64
	change := func(key pa.BotKey, phase string) error {
		x, err := st.Export(context.Background(), key)
		if err != nil {
			return err
		}
		id := fmt.Sprintf("roster-%d", sequence.Add(1))
		c := pa.Command{Key: key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("a", 64)}
		m := app.Mutation{Command: c, ExpectedVersion: x.Bot.Version, Receipt: pa.Receipt{ID: id, State: pa.EffectDelivered}}
		conversation := x.Conversation
		fence := pa.Fence{Key: key, Project: x.Bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
		if phase == "start" || len(x.Turns) == 0 {
			if fence.BindingID == "" {
				fence.BindingID, fence.Generation = "roster-binding", 1
				m.Binding = &pa.ExecutionBinding{Fence: fence, Agent: x.Bot.Agent, MemoryRevision: 1, InstructionRevision: 1}
				conversation.BindingID, conversation.Generation = fence.BindingID, fence.Generation
			}
			turn := pa.Turn{ID: id, Fence: fence, MessageID: "message-" + id, State: pa.TurnRunning, Instructions: x.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: id}
			m.Turn = &turn
			conversation.ActiveTurnID = id
			m.Messages = []pa.Message{{ID: turn.MessageID, Fence: fence, TurnID: id, Kind: pa.UserMessage, Text: "Read the notes", At: time.Now()}}
		} else {
			for _, turn := range x.Turns {
				if turn.ID == conversation.ActiveTurnID {
					saved := turn
					m.Turn = &saved
					break
				}
			}
		}
		if m.Turn == nil {
			return fmt.Errorf("no active fixture turn")
		}
		for _, offer := range x.Permissions {
			if offer.State == pa.PermissionPending {
				offer.State, offer.SelectedOptionID = pa.PermissionResolved, "allow"
				m.Permissions = append(m.Permissions, offer)
			}
		}
		if m.Turn.State == pa.TurnWaitingPermission && phase == "done" {
			m.Turn.State = pa.TurnRunning
			m.Conversation = &conversation
			if _, err := st.Commit(context.Background(), m); err != nil {
				return err
			}
			x, err = st.Export(context.Background(), key)
			if err != nil {
				return err
			}
			m.ExpectedVersion = x.Bot.Version
			m.Command.IdempotencyKey += "-complete"
			m.Receipt.ID = m.Command.IdempotencyKey
		}
		trace := pa.Trace{TurnID: m.Turn.ID, Category: "tool_call", Text: "Read notes.md"}
		switch phase {
		case "waiting":
			m.Turn.State = pa.TurnWaitingPermission
			m.Permissions = []pa.PermissionOffer{{ID: id, Fence: fence, TurnID: m.Turn.ID, NativeRequestID: id, State: pa.PermissionPending, ExpiresAt: time.Now().Add(time.Hour), Action: &pa.PermissionAction{Title: "Run unit tests"}, Options: []pa.PermissionOption{{ID: "allow", Kind: pa.AllowOnce, Allow: true}}}}
		case "writing":
			trace.Category, trace.Text = "agent_message_chunk", "Synthetic partial reply"
		case "done", "failed", "unknown":
			conversation.ActiveTurnID = ""
			m.Turn.State = pa.TurnSucceeded
			if phase == "failed" {
				m.Turn.State = pa.TurnFailed
				m.Turn.Failure = &pa.Failure{Code: pa.AdapterNotFound}
			}
			if phase == "unknown" {
				m.Turn.State = pa.TurnUnknown
				m.Turn.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown}
			}
			m.Messages = append(m.Messages, pa.Message{ID: "reply-" + id, Fence: fence, TurnID: m.Turn.ID, Kind: pa.AssistantMessage, Text: "Fresh reply", At: time.Now()})
		}
		if conversation.ActiveTurnID != "" {
			m.Events = []pa.Event{{Version: pa.ContractVersion, ID: id, Fence: fence, Kind: pa.TraceUpdated, Trace: &trace}}
		}
		m.Conversation = &conversation
		_, err = st.Commit(context.Background(), m)
		return err
	}
	for _, row := range []struct{ id, name, phase string }{{"reading", "Reading helper", "start"}, {"approval", "Approval helper", "waiting"}, {"failed", "Failed helper", "failed"}, {"finished", "Finished helper", "done"}, {"unknown", "Unknown helper", "unknown"}} {
		f.key.BotID = row.id
		q := f.create()
		q.Command.IdempotencyKey = "create-" + row.id
		q.Settings.Name = row.name
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, q))
		if err := change(f.key, row.phase); err != nil {
			t.Fatal(err)
		}
	}
	f.key.BotID = "reading"
	w := f.request(t, "GET", "/v1/project-assistants?machine_id="+f.key.MachineID+"&query=&cursor=&page_size=100", f.local, nil)
	paServerOK(t, w)
	var listed contract.ProjectAssistantListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	got := map[string]contract.ProjectAssistantBotSummary{}
	for _, item := range listed.Items {
		got[item.Bot.Key.BotID] = item
	}
	if got["reading"].Status != "working" || got["reading"].Activity != "Read notes.md" || got["approval"].Status != "needsInput" || got["approval"].Activity != "Needs approval: Run unit tests" || got["failed"].Status != "error" || got["finished"].Status != "idle" || got["unknown"].Failure == nil || string(got["unknown"].Failure.Code) != "execution_effect_unknown" {
		t.Fatalf("roster projection: %+v", got)
	}
	env, script := "CLAWDLINE_ROSTER_E2E_DIR", "roster-status.e2e.mjs"
	if cloud {
		env, script = "CLAWDLINE_ROSTER_CLOUD_E2E_DIR", "roster-cloud.e2e.mjs"
		if err := change(f.key, "unknown"); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("BOTS_PAGINATION") == "1" {
			for n := 0; n < 101; n++ {
				f.key.BotID = fmt.Sprintf("z-extra%03d", n)
				q := f.create()
				q.Command.IdempotencyKey = "create-" + f.key.BotID
				q.Settings.Name = "Pagination helper"
				paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, q))
			}
			f.key.BotID = "reading"
		}
	}
	if dir := os.Getenv(env); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/fixture/phase" {
				if r.Method != "POST" {
					w.WriteHeader(405)
					return
				}
				if r.URL.Query().Get("phase") == "delete-extra" {
					key := f.key
					key.BotID = "z-extra100"
					if err := st.DeleteBot(r.Context(), key); err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
					w.WriteHeader(204)
					return
				}
				if err := change(f.key, r.URL.Query().Get("phase")); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.WriteHeader(204)
				return
			}
			f.h.ServeHTTP(w, r)
		}))
		defer server.Close()
		cmd := exec.Command("node", "../../../web/console/src/project-assistants/"+script)
		cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READONLY_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_ARTIFACTS="+dir)
		log, err := cmd.CombinedOutput()
		if e := os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600); e != nil {
			t.Fatal(e)
		}
		if err != nil {
			t.Fatalf("browser: %v\n%s", err, log)
		}
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("roster metadata started a provider")
	}
}
