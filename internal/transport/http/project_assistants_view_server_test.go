package http

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// Append synthetic durable source events through the real host-owned writer.
// These fixtures never start a runtime or contact a provider.
func paViewServerEvents(t *testing.T, f *paServerFixture, key pa.BotKey, fence pa.Fence, count int) int64 {
	t.Helper()
	started := time.Now()
	defer func() { t.Logf("fixture durable events count=%d elapsed=%s", count, time.Since(started)) }()
	db := f.s.store.ProjectAssistantDatabase()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var seq int64
	var start int64
	if err = tx.QueryRow(`SELECT coalesce(max(seq),0) FROM project_assistant_events`).Scan(&start); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("source-%s-%d-%d", key.BotID, start, i)
		r, err := tx.Exec(`INSERT INTO project_assistant_events(machine,bot,event_id,data) VALUES(?,?,?,?)`, key.MachineID, key.BotID, id, []byte("null"))
		if err != nil {
			t.Fatal(err)
		}
		seq, err = r.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		e := pa.Event{Version: pa.ContractVersion, ID: id, Seq: seq, Fence: fence, Kind: pa.SourceUpdated, At: time.Now().UTC(), Source: &pa.SourceStatus{MachineID: key.MachineID, State: pa.SourceOnline, Fresh: true, Readable: true, Writable: true, Access: pa.Interactive}}
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`UPDATE project_assistant_events SET data=? WHERE seq=?`, raw, seq); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return seq
}

func paViewServerRead(t *testing.T, f *paServerFixture, mode pa.ReadViewMode, epoch, cursor string, after int64, size int, token string) contract.ProjectAssistantReadViewResponse {
	t.Helper()
	g := f.get(t, f.local)
	started := time.Now()
	w := f.request(t, "GET", paViewURL(f.key, g.Conversation.ID, mode, epoch, cursor, after, size), token, nil)
	t.Logf("source request mode=%s page_size=%d after_seq=%d status=%d elapsed=%s", mode, size, after, w.Code, time.Since(started))
	paAssertHTTPContract(t, w, 200, "ProjectAssistantReadViewResponse")
	var out contract.ProjectAssistantReadViewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectAssistantViewServerRealReaderFrontierAndLifecycle(t *testing.T) {
	started := time.Now()
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	t.Logf("fixture server/enable/create/get elapsed=%s", time.Since(started))
	fence := pa.Fence{Key: f.key, Project: pa.ProjectScope{MachineID: g.Bot.Project.MachineID, ProjectID: g.Bot.Project.ProjectID, CanonicalID: g.Bot.Project.CanonicalID, Kind: pa.ProjectKind(g.Bot.Project.Kind)}, ConversationID: g.Conversation.ID}
	last := paViewServerEvents(t, f, f.key, fence, 600)
	other := f.create()
	other.Command.Key.BotID = "other-bot"
	other.Command.IdempotencyKey = "create-other"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, other))
	// Durable message sequence belongs to the conversation, not the source log.
	started = time.Now()
	messages := make([]pa.Message, 650)
	for i := range messages {
		messages[i] = pa.Message{ID: fmt.Sprintf("message-%d", i+1), Fence: fence, Seq: int64(i + 1), Kind: pa.AssistantMessage, Text: "synthetic visible fact", At: time.Now().UTC()}
	}
	raw, _ := json.Marshal(messages)
	t.Logf("fixture message build/marshal rows=%d bytes=%d elapsed=%s", len(messages), len(raw), time.Since(started))
	started = time.Now()
	if _, err := f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',?) WHERE machine=? AND bot=?`, string(raw), len(messages), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture message write elapsed=%s", time.Since(started))
	snapshot := paViewServerRead(t, f, pa.ReadViewSnapshot, "", "", 0, 100, f.readonly)
	if snapshot.Head.EventFrontier != last+1 || snapshot.Snapshot == nil || len(snapshot.Items) != 100 || snapshot.Items[0].Message.Seq != 650 || !snapshot.HasMore {
		t.Fatalf("source frontier/message confused: %+v", snapshot)
	}
	historyPage := paViewServerRead(t, f, pa.ReadViewHistory, snapshot.SourceEpoch, snapshot.NextCursor, 0, 100, f.readonly)
	if len(historyPage.Items) != 100 || historyPage.Items[0].Message.Seq != int64(len(messages)-100) || historyPage.NextSeq != 0 {
		t.Fatal("older history lost its position or advanced event cursor")
	}
	events := paViewServerRead(t, f, pa.ReadViewEvents, snapshot.SourceEpoch, "", snapshot.Head.EventFrontier, 256, f.readonly)
	if len(events.Items) != 0 || events.NextSeq != snapshot.Head.EventFrontier {
		t.Fatal("snapshot replayed initial 600 events")
	}
	newSeq := paViewServerEvents(t, f, f.key, fence, 1)
	events = paViewServerRead(t, f, pa.ReadViewEvents, snapshot.SourceEpoch, "", snapshot.Head.EventFrontier, 256, f.readonly)
	if len(events.Items) != 1 || events.NextSeq != newSeq || events.Items[0].Event.Seq != newSeq {
		t.Fatal("new source event lost after snapshot frontier")
	}
	for _, test := range []struct {
		epoch string
		after int64
	}{{"old-incarnation", 0}, {snapshot.SourceEpoch, newSeq + 1}} {
		w := f.request(t, "GET", paViewURL(f.key, g.Conversation.ID, pa.ReadViewEvents, test.epoch, "", test.after, 256), f.readonly, nil)
		paAssertHTTPFailure(t, w, 410, pa.CursorExpired)
	}
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	history := paViewServerRead(t, f, pa.ReadViewSnapshot, "", "", 0, 100, f.readonly)
	if history.Source.Access != contract.ProjectAssistantAccessHistoryOnly || history.Source.Writable || f.providerCalls.Load() != 0 {
		t.Fatal("disabled history started provider or gained write access")
	}
	if err := f.s.StopProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "GET", paViewURL(f.key, g.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), f.readonly, nil), 403, pa.Disabled)
}

func paViewServerBridge(t *testing.T, f *paServerFixture, sender string, fields contract.ProjectAssistantReadViewRequest) cloudops.Answer {
	return paViewServerBridgeContext(context.Background(), t, f, sender, fields)
}

func paViewServerBridgeContext(ctx context.Context, t *testing.T, f *paServerFixture, sender string, fields contract.ProjectAssistantReadViewRequest) cloudops.Answer {
	t.Helper()
	b, _ := json.Marshal(fields)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	body["type"] = "project-assistants.view"
	body["session"] = cloudops.MachineReplySession
	body["request"] = strings.Repeat("<", 128)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	bridge := cloudops.Bridge{MachineID: f.identity.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}, AllowCommands: func() bool { return false }}
	started := time.Now()
	answer := bridge.Handle(ctx, cloudops.Command{Channel: "ctl/" + f.identity.MachineID, Class: cloudops.ClassCtl, Sender: sender, Sequence: 1, Plaintext: raw})
	t.Logf("Cloud Bridge request mode=%s page_size=%d after_seq=%d status=%d elapsed=%s", fields.Mode, fields.PageSize, fields.AfterSeq, answer.Status, time.Since(started))
	return answer
}

func TestProjectAssistantViewServerLongHistoryFitsExistingCloudReadDeadline(t *testing.T) {
	started := time.Now()
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID}
	t.Logf("fixture server/enable/create/get elapsed=%s", time.Since(started))
	paViewServerEvents(t, f, f.key, fence, 600)
	other := f.create()
	other.Command.Key.BotID, other.Command.IdempotencyKey = "other-bot", "create-other"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, other))
	started = time.Now()
	messages := make([]pa.Message, 9001)
	for i := range messages {
		messages[i] = pa.Message{ID: fmt.Sprintf("message-%d", i+1), Fence: fence, Seq: int64(i + 1), Kind: pa.AssistantMessage, Text: "synthetic visible fact", At: time.Now().UTC()}
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture message build/marshal rows=%d bytes=%d elapsed=%s", len(messages), len(raw), time.Since(started))
	started = time.Now()
	if _, err = f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',?) WHERE machine=? AND bot=?`, string(raw), len(messages), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture message write elapsed=%s", time.Since(started))
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	// This is the existing Cloud reader wait budget (READ_TIMEOUT_MS=60000),
	// not a raised timeout or a latency target invented for this feature.
	read := func(q contract.ProjectAssistantReadViewRequest) cloudops.Answer {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return paViewServerBridgeContext(ctx, t, f, "reader", q)
	}
	q := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: g.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100}
	a := read(q)
	if !a.OK() {
		t.Fatalf("9001-message source did not answer within the existing Cloud read wait: status=%d %s", a.Status, a.Payload)
	}
	var envelope struct {
		Body contract.ProjectAssistantReadViewResponse `json:"body"`
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Body.Head.EventFrontier != 602 || len(envelope.Body.Items) != 100 || envelope.Body.Items[0].Message.Seq != 9001 || !envelope.Body.HasMore || len(a.Payload) > cloudops.ReadPlaintextBytesLimit {
		t.Fatal("long history lost bounded latest facts or the machine frontier")
	}
	snapshot := envelope.Body
	q.Mode, q.SourceEpoch, q.Cursor = contract.ProjectAssistantReadViewModeHistory, snapshot.SourceEpoch, snapshot.NextCursor
	a = read(q)
	if !a.OK() {
		t.Fatalf("9001-message history exceeded the existing per-request Cloud wait: %d %s", a.Status, a.Payload)
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Body.Items) != int(q.PageSize) || envelope.Body.Items[0].Message.Seq != int64(len(messages))-q.PageSize || envelope.Body.NextSeq != 0 {
		t.Fatal("long older page lost its position or advanced the event cursor")
	}
	q.Mode, q.Cursor, q.AfterSeq, q.PageSize = contract.ProjectAssistantReadViewModeEvents, "", snapshot.Head.EventFrontier, 256
	a = read(q)
	if !a.OK() {
		t.Fatalf("long-history event resume exceeded the existing Cloud wait: %d %s", a.Status, a.Payload)
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Body.Items) != 0 || envelope.Body.NextSeq != snapshot.Head.EventFrontier {
		t.Fatal("long history replayed initial source events")
	}
}

func TestProjectAssistantViewCloudBridgeRouterServerRealStore(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	q := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: g.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100}
	a := paViewServerBridge(t, f, "reader", q)
	if !a.OK() || a.Mutating || a.Session != cloudops.MachineReplySession || len(a.Payload) > cloudops.ReadPlaintextBytesLimit {
		t.Fatalf("real Cloud read failed: %+v %s", a, a.Payload)
	}
	var envelope struct {
		Body contract.ProjectAssistantReadViewResponse `json:"body"`
	}
	if err := json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Body.Head.Key != q.Key || envelope.Body.SourceEpoch == "" || envelope.Body.Snapshot == nil {
		t.Fatal("real store response lost through Cloud")
	}
	q.Mode = contract.ProjectAssistantReadViewModeEvents
	q.SourceEpoch = envelope.Body.SourceEpoch
	q.PageSize = 256
	q.AfterSeq = envelope.Body.Head.EventFrontier + 1
	a = paViewServerBridge(t, f, "reader", q)
	if a.OK() || !strings.Contains(string(a.Payload), `"code":"`+string(pa.CursorExpired)+`"`) {
		t.Fatalf("typed source cursor failure lost: %s", a.Payload)
	}
	q.Mode = contract.ProjectAssistantReadViewModeSnapshot
	q.SourceEpoch = ""
	q.AfterSeq = 0
	q.PageSize = 100
	a = paViewServerBridge(t, f, "missing-reader", q)
	if a.OK() || strings.Contains(string(a.Payload), `"snapshot"`) {
		t.Fatal("unpaired Cloud reader received content")
	}
	if f.providerCalls.Load() != 0 || f.s.projectAssistants.host.Usage().Processes != 0 {
		t.Fatal("read allocated provider/runtime")
	}
}

func TestProjectAssistantViewServerDiscardsPackedBytesAfterAuthorityChanges(t *testing.T) {
	for _, change := range []string{"revoked", "downgraded", "expired", "reader", "account", "machine", "disabled", "shutdown"} {
		t.Run(change, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			f.enable(t)
			paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
			g := f.get(t, f.local)
			pin := f.pin(t, "viewer", adaptercloud.ScopeOperate, false)
			called := 0
			f.s.projectAssistants.beforeViewPublish = func() {
				called++
				if f.s.store.ProjectAssistantReader().Stats().InUse != 0 {
					t.Fatal("SQL read transaction survived into response publication")
				}
				pinned := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName))
				switch change {
				case "revoked":
					if _, err := pinned.Revoke("viewer", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "downgraded", "expired", "reader":
					if change == "downgraded" {
						pin.Scope = adaptercloud.ScopeReadOnly
					}
					if change == "expired" {
						pin.ExpiresAt = time.Now().Unix() - 1
					}
					if change == "reader" {
						pin.Invited = true
					}
					if err := pinned.Pin(f.identity.AccountID, pin); err != nil {
						t.Fatal(err)
					}
				case "account", "machine":
					i := f.identity
					if change == "account" {
						i.AccountID = "changed-account"
					} else {
						i.MachineID = "changed-machine"
					}
					if err := adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Save(i); err != nil {
						t.Fatal(err)
					}
				case "disabled":
					if err := f.s.projectAssistants.host.Disable(context.Background()); err != nil {
						t.Fatal(err)
					}
				case "shutdown":
					if err := f.s.StopProjectAssistants(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			}
			q := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: g.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100}
			a := paViewServerBridge(t, f, "viewer", q)
			if called != 1 || a.OK() || strings.Contains(string(a.Payload), `"snapshot"`) || strings.Contains(string(a.Payload), "Synthetic fixture") {
				t.Fatalf("packed result escaped %s barrier: %s", change, a.Payload)
			}
			if f.providerCalls.Load() != 0 {
				t.Fatal("authority barrier initialized provider")
			}
		})
	}
}

func TestProjectAssistantViewServerCloudOversizedStoredItemKeepsHonestPrefix(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID}
	now := time.Now().UTC()
	messages := []pa.Message{
		{ID: "user", Fence: fence, Seq: 1, TurnID: "turn", Kind: pa.UserMessage, Text: "synthetic input", At: now},
		{ID: "latest", Fence: fence, Seq: 2, TurnID: "turn", Kind: pa.AssistantMessage, Text: "complete newest fact", At: now},
	}
	turns := map[string]pa.Turn{"turn": {ID: "turn", Fence: fence, MessageID: "user", State: pa.TurnSucceeded,
		Instructions: pa.InstructionSnapshot{Revision: 1, Text: "synthetic"}, MemoryRevision: 1, IdempotencyKey: "synthetic-turn"}}
	// The durable WorkLink TaskID has no string byte bound. The source must
	// refuse this complete oversized fact instead of skipping or slicing it.
	work := map[string]pa.WorkLink{"work": {ID: "work", Fence: fence, TurnID: "turn", TaskID: strings.Repeat("x", cloudops.ReadPlaintextBytesLimit), State: pa.WorkCompleted}}
	rawMessages, _ := json.Marshal(messages)
	rawTurns, _ := json.Marshal(turns)
	rawWork, _ := json.Marshal(work)
	_, err := f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',2,'$.Turns',json(?),'$.WorkLinks',json(?)) WHERE machine=? AND bot=?`, string(rawMessages), string(rawTurns), string(rawWork), f.key.MachineID, f.key.BotID)
	if err != nil {
		t.Fatal(err)
	}
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	q := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: g.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100}
	a := paViewServerBridge(t, f, "reader", q)
	if !a.OK() || len(a.Payload) > cloudops.ReadPlaintextBytesLimit {
		t.Fatalf("legal prefix did not cross real Cloud path: %d %s", a.Status, a.Payload)
	}
	var envelope struct {
		Body contract.ProjectAssistantReadViewResponse `json:"body"`
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	page := envelope.Body
	if len(page.Items) != 1 || page.Items[0].ID != "latest" || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("lost honest prefix before oversized stored row: %+v", page)
	}
	q.Mode = contract.ProjectAssistantReadViewModeHistory
	q.SourceEpoch, q.Cursor = page.SourceEpoch, page.NextCursor
	a = paViewServerBridge(t, f, "reader", q)
	if a.OK() || a.Status != 429 || !strings.Contains(string(a.Payload), `"code":"`+string(pa.CapacityFull)+`"`) || strings.Contains(string(a.Payload), `"items"`) {
		t.Fatalf("oversized first item was skipped/truncated or typed failure lost: %s", a.Payload)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("bounded history initialized provider")
	}
}

func TestProjectAssistantViewServerRetainsOldGenerationFactsAndUnknownBarrier(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	old := pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID, BindingID: "old-binding", Generation: 1}
	current := old
	current.BindingID, current.Generation = "current-binding", 2
	now := time.Now().UTC()
	messages := []pa.Message{
		{ID: "old-user", Fence: old, Seq: 1, TurnID: "old-turn", Kind: pa.UserMessage, Text: "old input", At: now},
		{ID: "current-user", Fence: current, Seq: 2, TurnID: "current-turn", Kind: pa.UserMessage, Text: "current input", At: now},
	}
	turns := map[string]pa.Turn{
		"old-turn":     {ID: "old-turn", Fence: old, MessageID: "old-user", State: pa.TurnUnknown, Instructions: pa.InstructionSnapshot{Revision: 1}, MemoryRevision: 1, IdempotencyKey: "old"},
		"current-turn": {ID: "current-turn", Fence: current, MessageID: "current-user", State: pa.TurnSucceeded, Instructions: pa.InstructionSnapshot{Revision: 1}, MemoryRevision: 1, IdempotencyKey: "current"},
	}
	bindings := map[string]pa.ExecutionBinding{
		old.BindingID:     {Fence: old, Agent: pa.Codex, NativeSessionID: "synthetic-native-old", MemoryRevision: 1, InstructionRevision: 1, Revoked: true},
		current.BindingID: {Fence: current, Agent: pa.Codex, NativeSessionID: "synthetic-native-current", MemoryRevision: 1, InstructionRevision: 1},
	}
	work := pa.WorkLink{ID: "old-work", Fence: old, TurnID: "old-turn", TaskID: "synthetic-task", State: pa.WorkCompleted}
	rawMessages, _ := json.Marshal(messages)
	rawTurns, _ := json.Marshal(turns)
	rawBindings, _ := json.Marshal(bindings)
	rawWork, _ := json.Marshal(map[string]pa.WorkLink{work.ID: work})
	db := f.s.store.ProjectAssistantDatabase()
	_, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',2,'$.Conversation.binding_id',?,'$.Conversation.generation',2,'$.Turns',json(?),'$.Bindings',json(?),'$.WorkLinks',json(?)) WHERE machine=? AND bot=?`, string(rawMessages), current.BindingID, string(rawTurns), string(rawBindings), string(rawWork), f.key.MachineID, f.key.BotID)
	if err != nil {
		t.Fatal(err)
	}
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	q := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: g.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 1}
	a := paViewServerBridge(t, f, "reader", q)
	if !a.OK() || strings.Contains(string(a.Payload), "synthetic-native") {
		t.Fatalf("read lost history or exposed native session: %s", a.Payload)
	}
	var envelope struct {
		Body contract.ProjectAssistantReadViewResponse `json:"body"`
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	snapshot := envelope.Body
	if snapshot.Head.UnknownTurnCount != 1 || snapshot.Head.CurrentFence.Generation != 2 || snapshot.Snapshot == nil || len(snapshot.Snapshot.PendingTurns) != 0 || !snapshot.HasMore {
		t.Fatal("old unknown barrier disappeared or became current execution")
	}
	q.Mode, q.SourceEpoch, q.Cursor, q.PageSize = contract.ProjectAssistantReadViewModeHistory, snapshot.SourceEpoch, snapshot.NextCursor, 100
	a = paViewServerBridge(t, f, "reader", q)
	if !a.OK() {
		t.Fatalf("old-generation history refused: %s", a.Payload)
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	history := envelope.Body
	if history.NextSeq != 0 || len(history.Items) != 2 || history.Items[0].Work == nil || history.Items[0].Work.Fence.Generation != 1 || history.Head.CurrentFence.Generation != 2 {
		t.Fatal("older generation fact was lost or promoted to current execution")
	}
	// A late durable event for the old generation still follows the machine
	// snapshot frontier; generation filtering must not silently discard it.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO project_assistant_events(machine,bot,event_id,data) VALUES(?,?,?,?)`, f.key.MachineID, f.key.BotID, "late-old-work", []byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	event := pa.Event{Version: pa.ContractVersion, ID: "late-old-work", Seq: seq, Fence: old, Kind: pa.WorkUpdated, Work: &work, At: now}
	rawEvent, _ := json.Marshal(event)
	if _, err = tx.Exec(`UPDATE project_assistant_events SET data=? WHERE seq=?`, rawEvent, seq); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	q.Mode, q.Cursor, q.AfterSeq, q.PageSize = contract.ProjectAssistantReadViewModeEvents, "", snapshot.Head.EventFrontier, 256
	a = paViewServerBridge(t, f, "reader", q)
	if !a.OK() {
		t.Fatalf("late old-generation event refused: %s", a.Payload)
	}
	if err = json.Unmarshal(a.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Body.Items) != 1 || envelope.Body.NextSeq != seq || envelope.Body.Items[0].Event.Work == nil || envelope.Body.Items[0].Event.Work.Fence.Generation != 1 {
		t.Fatal("late old-generation work event was skipped")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("history initialized provider")
	}
}
