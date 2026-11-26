package http

import (
	"context"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/projectassistanttest"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// This is a read fixture, not exchange admission or model execution. Durable
// SQLite facts cross the real authenticated HTTP handler and paired SourceClient.
func TestProjectAssistantExchangeCapturedReadSQLite(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required for the paired SourceClient read fixture")
	}
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	got := f.get(t, f.local)
	peerRequest := f.create()
	peerRequest.Command.Key.BotID = "review-peer"
	peerRequest.Command.IdempotencyKey = "create-review-peer"
	peerRequest.Settings.Name = "Synthetic reviewer"
	w := f.request(t, "POST", "/v1/project-assistants", f.local, peerRequest)
	paServerOK(t, w)
	var peer contract.ProjectAssistantBotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &peer); err != nil {
		t.Fatal(err)
	}
	// Disable before seeding history: disabling correctly drops pending replies.
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	x := pa.Exchange{ID: "read-only-exchange", Project: pa.ProjectScope{MachineID: got.Bot.Project.MachineID, ProjectID: got.Bot.Project.ProjectID, CanonicalID: got.Bot.Project.CanonicalID, Kind: pa.ProjectKind(got.Bot.Project.Kind)},
		Source: pa.ExchangeEndpoint{Key: f.key, ConversationID: got.Conversation.ID}, Target: pa.ExchangeEndpoint{Key: pa.BotKey{MachineID: f.key.MachineID, BotID: "review-peer"}, ConversationID: peer.Bot.ConversationID},
		SourceTurnID: "source-turn", RequestTurnID: "request-turn", Request: "Review the synthetic patch.\n<script>unsafe()</script>", Reply: "Review retained through SQLite.", Detail: "Synthetic durable detail", Outcome: pa.ExchangeCompleted, Revision: 2, Hops: 1, Delivery: "waiting"}
	raw, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	db := f.s.store.ProjectAssistantDatabase()
	if _, err = db.Exec(`INSERT INTO project_assistant_exchanges(machine,id,source,target,outstanding,revision,data) VALUES(?,?,?,?,?,?,?)`, f.key.MachineID, x.ID, f.key.BotID, "review-peer", false, x.Revision, raw); err != nil {
		t.Fatal(err)
	}
	// Seed a chat whose valid history exceeds the Cloud export envelope, then
	// keep its newest notice at revision 1 while the durable exchange is final.
	const sender = "exchange-reader"
	f.pin(t, sender, "readonly", false)
	messageText := strings.Repeat("x", pa.MaxMessageBytes)
	messageCount := cloudops.ReadPlaintextBytesLimit/len(messageText) + 1
	fence := pa.Fence{Key: x.Source.Key, Project: x.Project, ConversationID: x.Source.ConversationID}
	at := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	messages := make([]pa.Message, 0, messageCount+1)
	for i := 1; i <= messageCount; i++ {
		messages = append(messages, pa.Message{ID: "earlier-" + strconv.Itoa(i), Seq: int64(i), Fence: fence, Text: messageText, Kind: pa.AssistantMessage, At: at})
	}
	notice := x
	notice.Revision, notice.Outcome, notice.Reply, notice.Detail, notice.Delivery = 1, pa.ExchangeQueued, "", "", ""
	messages = append(messages, pa.Message{ID: "exchange-notice", Seq: int64(len(messages) + 1), Fence: fence, Text: x.Request, Kind: pa.NoticeMessage, At: at, Exchange: &notice})
	history, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Messages',json(?),'$.Conversation.last_message_seq',?) WHERE machine=? AND bot=?`, string(history), len(messages), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	if err := projectassistanttest.MoveInlineCollections(context.Background(), db, f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	bridge := cloudops.Bridge{MachineID: f.identity.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}}
	exportRequest, err := json.Marshal(map[string]any{"type": "project-assistants.export", "session": cloudops.MachineReplySession, "request": "exchange-export", "machine_id": f.key.MachineID, "bot_id": f.key.BotID})
	if err != nil {
		t.Fatal(err)
	}
	export := bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.identity.MachineID, Class: cloudops.ClassCtl, Sender: sender, Sequence: 1, Plaintext: exportRequest})
	if export.Status != 429 || export.Code != string(pa.CapacityFull) || !strings.Contains(string(export.Payload), "cloud_reply_bytes") {
		t.Fatalf("expected the Cloud export byte limit; got status %d code %s", export.Status, export.Code)
	}
	cloudPage := paViewServerBridge(t, f, sender, contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: x.Source.ConversationID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100})
	if !cloudPage.OK() {
		t.Fatalf("bounded Cloud view: %s", cloudPage.Payload)
	}
	server := httptest.NewServer(f.h)
	defer server.Close()
	input, err := json.Marshal(map[string]any{"scope": map[string]any{"key": paWireBotKey(f.key), "project": got.Bot.Project, "conversation_id": got.Conversation.ID}, "reference": paWireExchange(notice), "expected": paWireExchange(x)})
	if err != nil {
		t.Fatal(err)
	}
	script := `
import assert from "node:assert/strict";
import {ProjectAssistantSourceClient} from "./console/src/cloud/project-assistants.ts";
import {loadExchangePage} from "./console/src/project-assistants/exchanges.ts";
const {scope,reference,expected}=JSON.parse(process.env.EXCHANGE_INPUT);
const grant={accountKey:"local",readerID:"synthetic-reader",machineID:scope.key.machine_id,authorizationRevision:"fixture-grant",apiLevel:78,paired:true,online:true,writable:false};
const calls=[];
const source=new ProjectAssistantSourceClient(grant,(path,init)=>{calls.push([path,init?.method??"GET"]);return fetch(process.env.EXCHANGE_ORIGIN+path,{...init,headers:{...init?.headers,Authorization:"Bearer "+process.env.EXCHANGE_TOKEN}})},()=>grant);
const status=await source.status(new AbortController().signal);
assert.equal(status.sources[0].access,"history_only");
const value=await loadExchangePage(source,scope,reference,new AbortController().signal);
assert.deepEqual(value.entries.map(row=>row.exchange),[expected]);
assert.equal(value.bot.name,"Synthetic Bot");
assert.ok(value.next?.cursor);
const earlier=await loadExchangePage(source,scope,reference,new AbortController().signal,value.next);
assert.equal(earlier.entries.length,0);
assert.ok(calls.every(([,method])=>method==="GET"));
assert.equal(calls.filter(([path])=>path.includes("/export?")).length,0);
source.revoke();
await assert.rejects(loadExchangePage(source,scope,reference,new AbortController().signal),e=>e.error?.code==="pa_revoked");
console.log("paired SourceClient -> authenticated TCP HTTP -> real SQLite: exact history-only exchange; only GET; revoked read refused; model calls=0");
`
	cmd := exec.Command("node", "--experimental-strip-types", "--input-type=module", "-e", script)
	cmd.Dir = filepath.Join("..", "..", "..", "web")
	cmd.Env = append(os.Environ(), "EXCHANGE_INPUT="+string(input), "EXCHANGE_ORIGIN="+server.URL, "EXCHANGE_TOKEN="+f.readonly)
	log, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("paired read fixture: %v\n%s", err, log)
	}
	var retained []byte
	if err = db.QueryRow(`SELECT data FROM project_assistant_exchanges WHERE machine=? AND id=?`, f.key.MachineID, x.ID).Scan(&retained); err != nil || string(retained) != string(raw) {
		t.Fatal("read changed durable exchange", err)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("read opened the native/model provider")
	}
	t.Log(string(log))
}

func TestServerRestartContinuesOriginalQueueAcrossTwoBoots(t *testing.T) {
	ctx := t.Context()
	f := newPAServerFixture(t, true)
	firstProvider := &paSoakPeer{unknownSent: make(chan struct{})}
	firstProvider.lose.Store(true)
	f.s.projectAssistants.provider = func() (app.Provider, error) { return firstProvider, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))

	server := httptest.NewServer(f.s.Handler())
	t.Cleanup(server.Close)
	client := &paSoakHTTP{fixture: f, server: server}
	client.reconnect()
	_, original := client.send(t, "original-request")
	select {
	case <-firstProvider.unknownSent:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(3 * time.Second):
		t.Fatal("original prompt did not reach the provider")
	}
	_, queued := client.send(t, "queued-request")
	originalChat := client.get(t).Conversation.ID

	for boot := range 2 {
		if err := f.s.StopProjectAssistants(ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.s.store.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := New(f.s.cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
		f.s, f.h = restarted, restarted.Handler()
		provider := &paSoakPeer{resume: true}
		restarted.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
		if err = restarted.StartProjectAssistants(ctx); err != nil {
			t.Fatal(err)
		}
		x := paSoakAwait(t, f, queued.ResourceID, pa.TurnSucceeded)
		var recoveries, originals, queuedMessages int
		for _, turn := range x.Turns {
			if pa.RestartSourceTurn(turn) == original.ResourceID && turn.State == pa.TurnSucceeded {
				recoveries++
			}
			if turn.ID == original.ResourceID && turn.State == pa.TurnFailed && turn.Failure != nil && turn.Failure.Field == pa.RestartResumedField {
				originals++
			}
		}
		for _, message := range x.Messages {
			if message.TurnID == queued.ResourceID && message.Kind == pa.UserMessage {
				queuedMessages++
			}
		}
		wantSends := int32(2)
		if boot == 1 {
			wantSends = 0
		}
		if x.Conversation.ID != originalChat || recoveries != 1 || originals != 1 || queuedMessages != 1 || provider.sends.Load() != wantSends {
			t.Fatalf("boot %d: same conversation=%v recoveries=%d originals=%d queued messages=%d sends=%d", boot+1, x.Conversation.ID == originalChat, recoveries, originals, queuedMessages, provider.sends.Load())
		}
	}
}

func TestServerSendUnblocksQueueAfterUnverifiedRestart(t *testing.T) {
	ctx := t.Context()
	f := newPAServerFixture(t, true)
	provider := &paSoakPeer{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	exchange := seedQueuedBotStatus(t, f, pa.TurnUnknown)
	repo := featurestore.New(f.s.store.ProjectAssistantDatabase())
	before, err := repo.Export(ctx, f.key)
	if err != nil || len(before.Turns) != 2 {
		t.Fatalf("seeded turns: %v, %v", before.Turns, err)
	}
	var original pa.Turn
	for _, turn := range before.Turns {
		if turn.State == pa.TurnUnknown {
			original = turn
		}
	}
	if original.ID == "" {
		t.Fatal("missing interrupted turn")
	}
	bot := before.Bot
	bot.Version++
	receipt, err := repo.Commit(ctx, app.Mutation{
		Command:         pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "restart-unknown", ContentHash: strings.Repeat("a", 64)},
		ExpectedVersion: before.Bot.Version, Bot: &bot, Turn: &original,
		Receipt: pa.Receipt{ID: "restart-unknown", ResourceID: original.ID, State: pa.EffectUnknown},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.s.Handler())
	t.Cleanup(server.Close)
	client := &paSoakHTTP{fixture: f, server: server}
	client.reconnect()

	path := "/v1/project-assistants/" + f.key.BotID + "/messages"
	got := client.get(t)
	input := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "continue"), Text: "continue",
		Fence: paWireFence(pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation})}
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_manual_continue BEFORE INSERT ON project_assistant_receipts
		WHEN NEW.command_key='continue' BEGIN SELECT RAISE(ABORT, 'injected admission failure'); END`); err != nil {
		t.Fatal(err)
	}
	if status := client.request(t, "POST", path, input, nil); status != 500 {
		t.Fatalf("injected failure returned HTTP %d", status)
	}
	old, oldErr := repo.Turn(ctx, f.key, original.ID)
	queued, queuedErr := repo.Turn(ctx, f.key, exchange.ReplyTurnID)
	if oldErr != nil || queuedErr != nil || old.State != pa.TurnUnknown || queued.State != pa.TurnQueued || provider.sends.Load() != 0 {
		t.Fatalf("failed admission released the queue: %s, %s, %v, %v", old.State, queued.State, oldErr, queuedErr)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_manual_continue"); err != nil {
		t.Fatal(err)
	}

	q, admitted := client.send(t, "continue")
	var replay contract.ProjectAssistantReceiptResponse
	if status := client.request(t, "POST", path, q, &replay); status != 200 || replay.Receipt != admitted {
		t.Fatalf("retry changed the admission: HTTP %d, %+v", status, replay)
	}
	_ = paSoakAwait(t, f, admitted.ResourceID, pa.TurnSucceeded)
	after := paSoakAwait(t, f, exchange.ReplyTurnID, pa.TurnSucceeded)
	turns := make(map[string]pa.Turn, len(after.Turns))
	for _, turn := range after.Turns {
		turns[turn.ID] = turn
	}
	if len(turns) != 3 || turns[admitted.ResourceID].State != pa.TurnSucceeded || turns[exchange.ReplyTurnID].State != pa.TurnSucceeded ||
		turns[original.ID].State != pa.TurnFailed || turns[original.ID].Failure.Code != pa.ExecutionEffectUnknown ||
		after.Conversation.ID != before.Conversation.ID {
		t.Fatalf("queue or original history changed unexpectedly: %+v", turns)
	}
	if first, second := turns[admitted.ResourceID].StartedAt, turns[exchange.ReplyTurnID].StartedAt; first == nil || second == nil || first.After(*second) {
		t.Fatalf("releasing message did not run before held status report: %v versus %v", first, second)
	}
	kept, err := repo.Receipt(ctx, receipt.Command)
	if err != nil || kept == nil || *kept != receipt || provider.sends.Load() != 2 {
		t.Fatalf("uncertain receipt or prompt count changed: %+v, %v, %d", kept, err, provider.sends.Load())
	}
}

// On 2026-11-25 the daemon log showed the person's Bot messages refused for
// five days as pa_outcome_unknown / inspect_interrupted_turn: an earlier turn
// had an unknown outcome that no restart continuation owned, or a stale
// active-turn pointer named it. A new message never repeats that turn, so it
// is admitted through the real SQLite store and runs after the queue.
func TestServerSendIsAdmittedBehindAnyUnknownTurn(t *testing.T) {
	for _, tc := range []struct{ name, field, active string }{
		{"execution", "execution", ""},
		{"steer unknown", pa.SteerUnknownField, ""},
		{"stale active turn", "execution", "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			f := newPAServerFixture(t, true)
			provider := &paSoakPeer{}
			f.s.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
			f.enable(t)
			paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
			exchange := seedQueuedBotStatus(t, f, pa.TurnUnknown)
			db := f.s.store.ProjectAssistantDatabase()
			if _, err := db.ExecContext(ctx, `UPDATE project_assistant_bot_facts SET data=json_set(CAST(data AS TEXT),'$.failure.field',?) WHERE machine=? AND bot=? AND collection='Turns' AND member='original'`,
				tc.field, f.key.MachineID, f.key.BotID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Conversation.active_turn_id',?) WHERE machine=? AND bot=?`,
				tc.active, f.key.MachineID, f.key.BotID); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(f.s.Handler())
			t.Cleanup(server.Close)
			client := &paSoakHTTP{fixture: f, server: server}
			client.reconnect()
			_, admitted := client.send(t, "continue")
			after := paSoakAwait(t, f, admitted.ResourceID, pa.TurnSucceeded)
			turns := make(map[string]pa.Turn, len(after.Turns))
			for _, turn := range after.Turns {
				turns[turn.ID] = turn
			}
			if turns[exchange.ReplyTurnID].State != pa.TurnSucceeded || turns["original"].State != pa.TurnFailed ||
				turns["original"].Failure.Code != pa.ExecutionEffectUnknown || after.Conversation.ActiveTurnID != "" {
				t.Fatalf("queue or original history changed unexpectedly: %+v", turns)
			}
			if first, second := turns[exchange.ReplyTurnID].StartedAt, turns[admitted.ResourceID].StartedAt; first == nil || second == nil || first.After(*second) {
				t.Fatalf("queued status did not run first: %v versus %v", first, second)
			}
			if provider.sends.Load() != 2 {
				t.Fatalf("sent %d prompts; the unknown turn must not be repeated", provider.sends.Load())
			}
		})
	}
}

func TestServerRestartPreservesQueuedBotStatus(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		original pa.TurnState
	}{
		{"enabled with unknown original", true, pa.TurnUnknown},
		{"enabled with finished original", true, pa.TurnSucceeded},
		{"disabled", false, pa.TurnUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			f := newPAServerFixture(t, true)
			f.enable(t)
			paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
			exchange := seedQueuedBotStatus(t, f, tc.original)
			db := f.s.store.ProjectAssistantDatabase()
			if err := f.s.StopProjectAssistants(ctx); err != nil {
				t.Fatal(err)
			}
			retained, err := featurestore.New(db).Exchange(ctx, f.key, exchange.ID)
			if err != nil || retained.Delivery != "queued" {
				t.Fatalf("shutdown lost queued status: %s, %v", retained.Delivery, err)
			}
			if !tc.enabled {
				if _, err := f.s.settingsFile().Set(map[string]any{"project_assistants_enabled": false}); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.s.store.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(f.s.cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
			provider := &paSoakPeer{}
			restarted.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
			if err = restarted.StartProjectAssistants(ctx); err != nil {
				t.Fatal(err)
			}
			store := featurestore.New(restarted.store.ProjectAssistantDatabase())
			if tc.original == pa.TurnSucceeded {
				f.s, f.h = restarted, restarted.Handler()
				export := paSoakAwait(t, f, exchange.ReplyTurnID, pa.TurnSucceeded)
				retained, err = store.Exchange(ctx, f.key, exchange.ID)
				if err != nil || retained.Delivery != "delivered" || retained.Outcome != pa.ExchangeUnknown || len(export.Turns) != 2 || provider.sends.Load() != 1 {
					t.Fatalf("restart must only deliver the original status: delivery=%s outcome=%s turns=%d sends=%d err=%v", retained.Delivery, retained.Outcome, len(export.Turns), provider.sends.Load(), err)
				}
				return
			}
			if tc.enabled {
				// The unknown original still blocks model work, but the status is already in the chat.
				retained, err = store.Exchange(ctx, f.key, exchange.ID)
				if err != nil || retained.Delivery != "queued" || retained.Revision != exchange.Revision {
					t.Fatalf("enabled startup changed queued status: %s, revision %d, %v", retained.Delivery, retained.Revision, err)
				}
				f.s, f.h = restarted, restarted.Handler()
				var page contract.ProjectAssistantReadViewResponse
				history := f.request(t, "GET", paViewURL(f.key, exchange.Source.ConversationID, pa.ReadViewSnapshot, "", "", 0, 100), f.local, nil)
				paServerOK(t, history)
				if err = json.Unmarshal(history.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				var shown bool
				for _, item := range page.Items {
					if item.Message != nil && item.Message.Exchange != nil && item.Message.Exchange.ID == exchange.ID {
						shown = item.Message.Exchange.Outcome == string(pa.ExchangeUnknown) && item.Message.Exchange.Delivery == "queued"
					}
				}
				if !shown {
					t.Fatal("interrupted status was missing from the blocked chat")
				}
				paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
			}
			retained, err = store.Exchange(ctx, f.key, exchange.ID)
			last, lastErr := store.Turn(ctx, f.key, exchange.ReplyTurnID)
			if err != nil || lastErr != nil || retained.Delivery != "dropped" || last.State != pa.TurnCancelled || last.StartedAt != nil {
				t.Fatalf("disabled state did not cancel the unstarted reply: %s, %s, %v, %v", retained.Delivery, last.State, err, lastErr)
			}
		})
	}
}

// Use the durable state left when the recipient was interrupted and its status was queued.
func seedQueuedBotStatus(t *testing.T, f *paServerFixture, originalState pa.TurnState) pa.Exchange {
	t.Helper()
	ctx := t.Context()
	got := f.get(t, f.local)
	peerRequest := f.create()
	peerRequest.Command.Key.BotID = "review-peer"
	peerRequest.Command.IdempotencyKey = "create-review-peer"
	peerRequest.Settings.Name = "Synthetic peer"
	peer := f.request(t, "POST", "/v1/project-assistants", f.local, peerRequest)
	paServerOK(t, peer)
	var recipient contract.ProjectAssistantBotResponse
	if err := json.Unmarshal(peer.Body.Bytes(), &recipient); err != nil {
		t.Fatal(err)
	}

	project := paDomainProjectScope(got.Bot.Project)
	fence := pa.Fence{Key: f.key, Project: project, ConversationID: got.Conversation.ID, Generation: got.Conversation.Generation, BindingID: got.Conversation.BindingID}
	bindings := map[string]pa.ExecutionBinding{}
	if originalState == pa.TurnSucceeded {
		fence.Generation, fence.BindingID = 1, "original-binding"
		bindings[fence.BindingID] = pa.ExecutionBinding{Fence: fence, Agent: pa.Agent(got.Bot.Agent), MemoryRevision: 1, InstructionRevision: 1}
	}
	exchange := pa.Exchange{ID: "interrupted-request", Project: project,
		Source:       pa.ExchangeEndpoint{Key: f.key, ConversationID: got.Conversation.ID},
		Target:       pa.ExchangeEndpoint{Key: pa.BotKey{MachineID: f.key.MachineID, BotID: "review-peer"}, ConversationID: recipient.Bot.ConversationID},
		SourceTurnID: "original", RequestTurnID: "peer-request", ReplyTurnID: "interrupted-request-reply-turn",
		Request: "Check a synthetic fact.", Outcome: pa.ExchangeUnknown, Revision: 3, Hops: 1, Delivery: "queued",
		Detail: "execution_effect_unknown:restart"}
	reply := pa.Turn{ID: exchange.ReplyTurnID, Fence: fence, MessageID: "status-message", State: pa.TurnQueued, Origin: pa.BotReplyOrigin, ExchangeID: exchange.ID, Hops: 1, IdempotencyKey: exchange.ID + "-reply"}
	original := pa.Turn{ID: exchange.SourceTurnID, Fence: fence, MessageID: "original-message", State: originalState, Origin: pa.UserOrigin, IdempotencyKey: "original-key", MemoryRevision: 1, Instructions: pa.InstructionSnapshot{Revision: 1}}
	if originalState == pa.TurnUnknown {
		original.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "restart"}
	}
	messages := []pa.Message{
		{ID: original.MessageID, TurnID: original.ID, Fence: fence, Seq: 1, Kind: pa.UserMessage, Text: "Ask the peer.", At: time.Now().Add(-2 * time.Hour)},
		{ID: reply.MessageID, TurnID: reply.ID, Fence: fence, Seq: 2, Kind: pa.NoticeMessage, Text: exchange.Request, At: time.Now(), Exchange: &exchange},
	}
	db := f.s.store.ProjectAssistantDatabase()
	xRaw, _ := json.Marshal(exchange)
	tRaw, _ := json.Marshal(map[string]pa.Turn{original.ID: original, reply.ID: reply})
	mRaw, _ := json.Marshal(messages)
	bRaw, _ := json.Marshal(bindings)
	if _, err := db.ExecContext(ctx, "INSERT INTO project_assistant_exchanges(machine,id,source,target,outstanding,revision,data) VALUES (?,?,?,?,0,?,?)", f.key.MachineID, exchange.ID, f.key.BotID, exchange.Target.Key.BotID, exchange.Revision, xRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Turns',json(?),'$.Messages',json(?),'$.Bindings',json(?),'$.Conversation.binding_id',?,'$.Conversation.generation',?,'$.Conversation.last_message_seq',2) WHERE machine=? AND bot=?`, string(tRaw), string(mRaw), string(bRaw), fence.BindingID, fence.Generation, f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	if err := projectassistanttest.MoveInlineCollections(context.Background(), db, f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	return exchange
}
