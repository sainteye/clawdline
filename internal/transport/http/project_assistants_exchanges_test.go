package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
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
