package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
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
	server := httptest.NewServer(f.h)
	defer server.Close()
	input, err := json.Marshal(map[string]any{"scope": map[string]any{"key": paWireBotKey(f.key), "project": got.Bot.Project, "conversation_id": got.Conversation.ID}, "reference": paWireExchange(x)})
	if err != nil {
		t.Fatal(err)
	}
	script := `
import assert from "node:assert/strict";
import {ProjectAssistantSourceClient} from "./console/src/cloud/project-assistants.ts";
import {loadExchange} from "./console/src/project-assistants/exchanges.ts";
const {scope,reference}=JSON.parse(process.env.EXCHANGE_INPUT);
const grant={accountKey:"local",readerID:"synthetic-reader",machineID:scope.key.machine_id,authorizationRevision:"fixture-grant",apiLevel:78,paired:true,online:true,writable:false};
const calls=[];
const source=new ProjectAssistantSourceClient(grant,(path,init)=>{calls.push([path,init?.method??"GET"]);return fetch(process.env.EXCHANGE_ORIGIN+path,{...init,headers:{...init?.headers,Authorization:"Bearer "+process.env.EXCHANGE_TOKEN}})},()=>grant);
const status=await source.status(new AbortController().signal);
assert.equal(status.sources[0].access,"history_only");
const value=await loadExchange(source,scope,reference,new AbortController().signal);
assert.deepEqual(value.exchange,reference);
assert.ok(calls.every(([,method])=>method==="GET"));
assert.equal(calls.filter(([path])=>path.includes("/export?")).length,1);
assert.equal(value.exchange.delivery,"waiting");
assert.equal(value.exchange.reply,"Review retained through SQLite.");
source.revoke();
await assert.rejects(loadExchange(source,scope,reference,new AbortController().signal),e=>e.error?.code==="pa_revoked");
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
