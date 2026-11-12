package cloudops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/url"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type projectAssistantRouter func(context.Context, LocalRequest) (LocalResponse, error)

func projectAssistantOperationCases() []operationCase {
	key := map[string]any{"machine_id": "mac-01", "bot_id": "bot"}
	project := map[string]any{"machine_id": "mac-01", "project_id": "project", "canonical_id": "canonical", "kind": "repository"}
	fence := map[string]any{"key": key, "project": project, "conversation_id": "chat", "binding_id": "binding", "generation": 1}
	appearance := map[string]any{"shape": "blob", "color": "#e39567"}
	rows := []struct {
		word, method, suffix string
		fields               map[string]any
		query                map[string]string
	}{
		{"status", "GET", "/status", nil, nil},
		{"list", "GET", "", map[string]any{"machine_id": "mac-01", "query": "search", "cursor": "page", "page_size": 2}, map[string]string{"machine_id": "mac-01", "query": "search", "cursor": "page", "page_size": "2"}},
		{"get", "GET", "/bot", key, map[string]string{"machine_id": "mac-01"}},
		{"export", "GET", "/bot/export", key, map[string]string{"machine_id": "mac-01"}},
		{"history", "GET", "/bot/messages", map[string]any{"key": key, "conversation_id": "chat", "before_seq": 3, "page_size": 2}, map[string]string{"machine_id": "mac-01", "conversation_id": "chat", "before_seq": "3", "page_size": "2"}},
		{"events", "GET", "/bot/events", map[string]any{"key": key, "after_seq": 3, "page_size": 2}, map[string]string{"machine_id": "mac-01", "after_seq": "3", "page_size": "2"}},
		{"create", "POST", "", map[string]any{"settings": map[string]any{"target": map[string]any{"machine_id": "mac-01", "project_id": "project"}, "name": "Name", "appearance": appearance, "agent": "codex", "permission_policy": "ask", "instructions": "", "role_snapshot_ref": ""}}, nil},
		{"edit", "PATCH", "/bot", map[string]any{"settings": map[string]any{"expected_version": 1, "name": "Name", "appearance": appearance, "instructions": ""}}, nil},
		{"send", "POST", "/bot/messages", map[string]any{"fence": fence, "text": "hello", "quote_message_id": ""}, nil},
		{"stop", "POST", "/bot/stop", map[string]any{"fence": fence, "turn_id": "turn"}, nil},
		{"answer", "POST", "/bot/permissions/offer/answer", map[string]any{"fence": fence, "offer_id": "offer", "option_id": "option", "turn_id": "turn"}, nil},
		{"read", "POST", "/bot/read", map[string]any{"cursor": map[string]any{"key": key, "conversation_id": "chat", "reader_id": "hint", "message_seq": 2}}, nil},
		{"observed", "POST", "/bot/observed", map[string]any{"receipt_id": "receipt", "event_seq": 2}, nil},
		{"forget", "POST", "/bot/memory/forget", map[string]any{"project": project, "expected_revision": 1, "source_ids": []any{"source"}}, nil},
		{"memory", "POST", "/bot/memory", map[string]any{"expected_revision": 1, "entry": map[string]any{"id": "memory", "key": key, "project": project, "text": "remember", "sources": []any{}, "revision": 1, "forgotten": false}}, nil},
	}
	out := []operationCase{}
	for _, r := range rows {
		body := map[string]any{}
		for k, v := range r.fields {
			body[k] = v
		}
		c := operationCase{word: "project-assistants." + r.word, method: r.method, path: "/v1/project-assistants" + r.suffix, query: r.query, session: MachineReplySession}
		c.answers = &router{body: `{"contract_version":"project-assistants.v1","ok":true}`}
		if r.method != "GET" {
			op := map[string]string{"observed": "observe", "memory": "memory_save"}[r.word]
			if op == "" {
				op = r.word
			}
			body["command"] = map[string]any{"key": key, "operation": op, "idempotency_key": "case-" + r.word, "content_hash": strings.Repeat("a", 64)}
			local, _ := json.Marshal(body)
			c.body2 = string(local)
			c.name = "action:case-" + r.word
		} else {
			c.name = "read:case-" + r.word
		}
		body["type"] = c.word
		body["session"] = MachineReplySession
		body["request"] = "case-" + r.word
		c.body = body
		out = append(out, c)
	}
	return out
}

func (f projectAssistantRouter) Do(ctx context.Context, req LocalRequest) (LocalResponse, error) {
	return f(ctx, req)
}

func projectAssistantRows(t *testing.T) []cloudvocab.Operation {
	t.Helper()
	rows, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	var out []cloudvocab.Operation
	for _, row := range rows {
		if strings.HasPrefix(row.Op, "project-assistants.") {
			out = append(out, row)
		}
	}
	if len(out) != 15 {
		t.Fatalf("Bot vocabulary has %d words", len(out))
	}
	return out
}

func projectAssistantExample(t *testing.T, row cloudvocab.Operation) body {
	t.Helper()
	raw, _ := json.Marshal(row.Example)
	b, err := decodeBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	b["type"] = row.Op
	return b
}

// A real SourceClient and captured authenticated-connection adapter produce the
// fixtures at runtime. No browser, daemon, pairing, provider or network is used.
const projectAssistantCapture = `
import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';
const root=process.argv[1];
const {ProjectAssistantSourceClient,capturedAssistantRelayFetch}=await import(pathToFileURL(root+'/web/console/src/cloud/project-assistants.ts'));
const {CARRIED}=await import(pathToFileURL(root+'/web/console/src/cloud/carry-generated.ts'));
const rows=JSON.parse(readFileSync(root+'/internal/domain/cloudvocab/vocabulary.json','utf8')).operations.filter(x=>x.op.startsWith('project-assistants.'));
const machine='source-one', fixtures=[], status={contract_version:'project-assistants.v1',sources:[{machine_id:machine,state:'online',access:'interactive',fresh:true,readable:true,writable:true,observed_at:'',capabilities:[]}]};
let http, sequence=0;
const connection={machineDescriptor:()=>({machine:{commands:Object.keys(CARRIED)}}),
 _machineRequest:async(machine,word,fields,kind)=>receive('captured-read-'+(++sequence),machine,word,fields,kind),
 _machineRequestAs:async(ref,machine,word,fields,kind)=>receive(ref,machine,word,fields,kind)};
function receive(ref,machine,word,fields,kind){fixtures.push({...http,machine,word,ref,kind,plaintext:{type:word,session:'__clawdline_machine__',request:ref,...fields}});return word.endsWith('.status')?status:{contract_version:'project-assistants.v1',echo:word};}
const relay=capturedAssistantRelayFetch(machine,()=>connection,Object.keys(CARRIED));
const grant={accountKey:'fixture-account',readerID:'trusted-reader',machineID:machine,authorizationRevision:'revision',apiLevel:999,paired:true,online:true,writable:true};
const source=new ProjectAssistantSourceClient(grant,async(input,init)=>{http={input,method:init.method,body:init.body??null,idempotency:new Headers(init.headers).get('Idempotency-Key')};return relay(input,init)},()=>grant);
const signal=new AbortController().signal;
await source.status(signal);await source.projects(signal);
for(const row of rows){const word=row.op.split('.').at(-1);if(word==='status')continue;const {request,session,...fields}=row.example;const method={read:'markRead',observed:'observe',memory:'saveMemory'}[word]??word;await source[method](fields,signal);}
process.stdout.write(JSON.stringify(fixtures));
`

func TestProjectAssistantCapturedSourceRoundTrip(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is needed for the real captured SourceClient fixture")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", projectAssistantCapture, root)
	raw, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("capture: %v %s", err, exit.Stderr)
		}
		t.Fatal(err)
	}
	var fixtures []struct {
		Input       string          `json:"input"`
		Method      string          `json:"method"`
		Body        *string         `json:"body"`
		Idempotency *string         `json:"idempotency"`
		Machine     string          `json:"machine"`
		Word        string          `json:"word"`
		Ref         string          `json:"ref"`
		Kind        string          `json:"kind"`
		Plaintext   json.RawMessage `json:"plaintext"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 16 {
		t.Fatalf("captured %d methods, want all 16", len(fixtures))
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		t.Run(fixture.Word, func(t *testing.T) {
			if seen[fixture.Word] {
				t.Fatalf("duplicate %s", fixture.Word)
			}
			seen[fixture.Word] = true
			u, err := url.Parse(fixture.Input)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			bridge := Bridge{MachineID: fixture.Machine, AllowCommands: func() bool { return true }, Router: projectAssistantRouter(func(_ context.Context, request LocalRequest) (LocalResponse, error) {
				calls++
				actor := actorDevice
				if fixture.Word == "projects" {
					actor = ""
				} // existing catalog behavior
				if request.Method != fixture.Method || request.Path != u.EscapedPath() || request.Viewer != "authenticated-device" || request.Header[actorHeader] != actor {
					t.Errorf("HTTP identity/method/path: %+v", request)
				}
				query := map[string]string{}
				for key, value := range u.Query() {
					query[key] = value[0]
				}
				if !reflect.DeepEqual(query, request.Query) && !(len(query) == 0 && len(request.Query) == 0) {
					t.Errorf("query=%v want %v", request.Query, query)
				}
				if fixture.Body == nil {
					if request.Body != nil {
						t.Fatal("GET gained body")
					}
				} else {
					var got, want any
					json.Unmarshal(request.Body, &got)
					json.Unmarshal([]byte(*fixture.Body), &want)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body=%s want %s", request.Body, *fixture.Body)
					}
					if request.Header["Idempotency-Key"] != *fixture.Idempotency || fixture.Ref != *fixture.Idempotency {
						t.Fatal("stable command ref changed")
					}
				}
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1","receipt":{"id":"same-receipt"}}`)}, nil
			})}
			answer := bridge.Handle(context.Background(), Command{Channel: "ctl/" + fixture.Machine, Class: ClassCtl, Sender: "authenticated-device", Plaintext: fixture.Plaintext})
			if !answer.OK() || calls != 1 || answer.Name != fixture.Kind+":"+fixture.Ref {
				t.Fatalf("answer=%+v calls=%d", answer, calls)
			}
			body := answerOf(t, answer)["body"].(map[string]any)
			if body["receipt"].(map[string]any)["id"] != "same-receipt" {
				t.Fatal("receipt lost")
			}
		})
	}
	for _, row := range projectAssistantRows(t) {
		if !seen[row.Op] {
			t.Errorf("uncaptured %s", row.Op)
		}
	}
}

func TestProjectAssistantInvalidEnvelopeNeverReachesLocal(t *testing.T) {
	for _, row := range projectAssistantRows(t) {
		t.Run(row.Op, func(t *testing.T) {
			base := projectAssistantExample(t, row)
			var mutations []body
			for key := range base {
				if key == "type" || key == "session" || key == "request" {
					continue
				}
				missing := cloneBody(base)
				delete(missing, key)
				mutations = append(mutations, missing)
				null := cloneBody(base)
				null[key] = nil
				mutations = append(mutations, null)
			}
			extra := cloneBody(base)
			extra["principal"] = "forged"
			mutations = append(mutations, extra)
			if row.Kind == cloudvocab.Command {
				for _, path := range []string{"command.key.bot_id", "command.operation", "command.idempotency_key", "command.key.machine_id"} {
					changed := cloneBody(base)
					setProjectAssistantField(changed, path, "wrong")
					mutations = append(mutations, changed)
				}
				// A different valid Bot ID is not inherently malformed. Replace that
				// probe with a path escape, and test actual cross-field conflicts.
				setProjectAssistantField(mutations[len(mutations)-4], "command.key.bot_id", "../other")
				mutations = mutations[:len(mutations)-1] // machine mismatch is checked by the host's trusted source
				for _, path := range []string{"fence.key.bot_id", "cursor.key.bot_id", "entry.key.bot_id", "settings.target.machine_id", "project.machine_id", "entry.project.machine_id"} {
					if _, present := base.genLookup(path); present {
						changed := cloneBody(base)
						setProjectAssistantField(changed, path, "foreign")
						mutations = append(mutations, changed)
					}
				}
				changed := cloneBody(base)
				changed["command"].(map[string]any)["credential"] = "forged"
				mutations = append(mutations, changed)
			}
			for _, path := range []string{"page_size", "before_seq", "after_seq", "event_seq", "expected_revision", "fence.generation", "settings.expected_version", "cursor.message_seq"} {
				if _, present := base.genLookup(path); !present {
					continue
				}
				for _, value := range []any{json.Number("1.1"), json.Number("1e0"), json.Number("-1"), json.Number("9007199254740992"), "1", nil} {
					changed := cloneBody(base)
					setProjectAssistantField(changed, path, value)
					mutations = append(mutations, changed)
				}
			}
			for i, changed := range mutations {
				calls := 0
				bridge := open(projectAssistantRouter(func(context.Context, LocalRequest) (LocalResponse, error) {
					calls++
					return LocalResponse{Status: 200, Body: []byte(`{}`)}, nil
				}))
				answer := bridge.Handle(context.Background(), request(t, ClassCtl, changed))
				if answer.OK() || calls != 0 {
					t.Errorf("mutation %d accepted or routed (%d): %s", i, calls, describeBody(changed))
				}
			}
		})
	}
}

func setProjectAssistantField(b body, path string, value any) {
	parts := strings.Split(path, ".")
	object := map[string]any(b)
	for _, part := range parts[:len(parts)-1] {
		object = object[part].(map[string]any)
	}
	object[parts[len(parts)-1]] = value
}

func TestProjectAssistantBotIDsAndNestedLookup(t *testing.T) {
	for _, id := range []string{"status", ".", "..", "a/b", `a\b`, "a\x00b", "a\u0085b", ""} {
		if projectAssistantBotID(id) {
			t.Errorf("accepted id %q", id)
		}
	}
	if !projectAssistantBotID("機器人 🌱") {
		t.Fatal("Unicode segment refused")
	}
	b := body{"command": map[string]any{"key": map[string]any{"bot_id": "nested"}}, "command.key.bot_id": "literal", "array": []any{map[string]any{"bot_id": "wrong"}}, "string": "text"}
	if b.genAt("command.key.bot_id") != "nested" {
		t.Fatal("read dotted literal key")
	}
	for _, path := range []string{"command.missing.bot_id", "command.key.bot_id.secret", "array.bot_id", "string.bot_id", "foreign.bot_id"} {
		if _, ok := b.genLookup(path); ok {
			t.Errorf("looked up %s", path)
		}
	}
	if b.genAt("string") != "text" {
		t.Fatal("ordinary field changed")
	}
}

func TestProjectAssistantReadLevelAndLiveAuthority(t *testing.T) {
	for _, row := range projectAssistantRows(t) {
		t.Run(row.Op, func(t *testing.T) {
			calls := 0
			requires := []bool{}
			bridge := open(projectAssistantRouter(func(_ context.Context, _ LocalRequest) (LocalResponse, error) {
				calls++
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
			}))
			bridge.AllowCommands = func() bool { return false }
			bridge.Authority = func(_ context.Context, sender string, key ed25519.PublicKey, write bool) Authority {
				if sender != "viewer-device-01" || string(key) != "verified-key" {
					t.Fatal("trusted sender context changed")
				}
				requires = append(requires, write)
				return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: !write}
			}
			cmd := request(t, ClassCtl, projectAssistantExample(t, row))
			cmd.VerifiedKey = []byte("verified-key")
			answer := bridge.Handle(context.Background(), cmd)
			if row.WriteGate {
				if answer.OK() || calls != 0 {
					t.Fatal("content write passed disabled gate")
				}
			} else {
				if !answer.OK() || calls != 1 {
					t.Fatalf("read/metadata denied: %+v", answer)
				}
			}
			if row.Kind == cloudvocab.Command && !row.WriteGate && !reflect.DeepEqual(requires, []bool{false}) {
				t.Fatalf("metadata asked for content permission: %v", requires)
			}
			if row.Kind == cloudvocab.Command {
				bridge.AllowCommands = func() bool { return true }
				calls = 0
				bridge.Authority = func(context.Context, string, ed25519.PublicKey, bool) Authority {
					return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: false, WriteGateAllows: true}
				}
				answer = bridge.Handle(context.Background(), cmd)
				if answer.OK() || calls != 0 {
					t.Fatal("revoked sender reached Local")
				}
			}
		})
	}
}

func TestProjectAssistantFailureAndCapacity(t *testing.T) {
	rows := projectAssistantRows(t)
	var row cloudvocab.Operation
	for _, r := range rows {
		if r.Op == "project-assistants.export" {
			row = r
		}
	}
	base := projectAssistantExample(t, row)
	for _, code := range []pa.ErrorCode{pa.StaleGeneration, pa.CapacityFull, pa.OutcomeUnknown} {
		body := projectAssistantReplyFailure(code, "fence")
		var value map[string]any
		json.Unmarshal(body, &value)
		value["error"].(map[string]any)["retry_after_seconds"] = 7
		body, _ = json.Marshal(value)
		answer := open(&router{status: 429, body: string(body)}).Handle(context.Background(), request(t, ClassCtl, base))
		if answer.Status != 429 || answer.Code != string(code) || !projectAssistantSame(answerOf(t, answer)["body"], value) {
			t.Fatalf("source refusal lost: %+v", answer)
		}
	}
	name := "read:" + row.Example["request"].(string)
	empty := []byte(`{"contract_version":"project-assistants.v1","secret":""}`)
	overhead := len(payload(name, 200, empty, nil))
	for _, delta := range []int{0, 1} {
		secret := "合成容量測試資料"
		repeated := secret + strings.Repeat("x", ReadPlaintextBytesLimit-overhead-len(secret)+delta)
		body, _ := json.Marshal(map[string]string{"contract_version": pa.ContractVersion, "secret": repeated})
		answer := open(&router{body: string(body)}).Handle(context.Background(), request(t, ClassCtl, base))
		if delta == 0 {
			if !answer.OK() || len(answer.Payload) != ReadPlaintextBytesLimit {
				t.Fatalf("at-boundary bytes=%d status=%d", len(answer.Payload), answer.Status)
			}
			continue
		}
		if answer.Status != 429 || answer.Code != "pa_capacity_full" || strings.Contains(string(answer.Payload), secret) {
			t.Fatalf("oversize content/status: status=%d code=%s bytes=%d", answer.Status, answer.Code, len(answer.Payload))
		}
		failure := answerOf(t, answer)["body"].(map[string]any)
		if failure["contract_version"] != pa.ContractVersion || failure["error"].(map[string]any)["field"] != "cloud_reply_bytes" {
			t.Fatal("typed capacity failure missing")
		}
		t.Logf("source bytes=%d complete payload bytes=%d plaintext limit=%d failure payload bytes=%d", len(body), len(payload(name, 200, body, nil)), ReadPlaintextBytesLimit, len(answer.Payload))
	}
	for _, bad := range []string{`{"contract_version":"other","error":{"code":"pa_capacity_full","field":"x","retry_after_seconds":0}}`, `{"contract_version":"project-assistants.v1","error":{"code":"not-a-pa-code","field":"x","retry_after_seconds":0}}`, `{"contract_version":"project-assistants.v1","error":{"code":"pa_capacity_full","field":"x","retry_after_seconds":0,"credential":"secret"}}`} {
		if _, ok := projectAssistantFailure(LocalResponse{Status: 429, Body: []byte(bad)}); ok {
			t.Fatal("untyped envelope crossed partial hook")
		}
	}
}

func TestProjectAssistantHistoryAndExportByteBounds(t *testing.T) {
	var historyRow, exportRow cloudvocab.Operation
	for _, row := range projectAssistantRows(t) {
		if row.Op == "project-assistants.history" {
			historyRow = row
		}
		if row.Op == "project-assistants.export" {
			exportRow = row
		}
	}
	for _, char := range []string{"x", "<"} {
		messages := make([]contract.ProjectAssistantMessage, pa.MaxPageRows)
		for i := range messages {
			messages[i] = contract.ProjectAssistantMessage{ID: "message", Seq: int64(i + 1), Text: strings.Repeat(char, pa.MaxMessageBytes), Kind: contract.ProjectAssistantMessageKindAssistant}
		}
		history := contract.ProjectAssistantHistoryResponse{ContractVersion: pa.ContractVersion, Items: messages, HasMore: true, NextBeforeSeq: 1}
		export := contract.ProjectAssistantExport{ContractVersion: pa.ContractVersion, SchemaVersion: pa.SchemaVersion, Messages: messages}
		for _, fixture := range []struct {
			row   cloudvocab.Operation
			value any
		}{{historyRow, history}, {exportRow, export}} {
			raw, err := json.Marshal(fixture.value)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			bridge := open(projectAssistantRouter(func(context.Context, LocalRequest) (LocalResponse, error) {
				calls++
				return LocalResponse{Status: 200, Body: raw}, nil
			}))
			answer := bridge.Handle(context.Background(), request(t, ClassCtl, projectAssistantExample(t, fixture.row)))
			fullBytes := len(payload("read:"+fixture.row.Example["request"].(string), 200, raw, nil))
			if calls != 1 {
				t.Fatal("read retried")
			}
			if char == "x" {
				if !answer.OK() || len(answer.Payload) != fullBytes || !projectAssistantSame(answerOf(t, answer)["body"], fixture.value) {
					t.Fatal("bounded response changed")
				}
			} else {
				if answer.Status != 429 || answer.Code != "pa_capacity_full" || len(answer.Payload) > ReadPlaintextBytesLimit || strings.Contains(string(answer.Payload), "next_before_seq") || strings.Contains(string(answer.Payload), "message") {
					t.Fatal("oversized content/cursor escaped")
				}
			}
			t.Logf("%s char=%q rows=%d text bytes/row=%d source JSON bytes=%d full payload bytes=%d limit=%d answer status=%d answer bytes=%d", fixture.row.Op, char, len(messages), pa.MaxMessageBytes, len(raw), fullBytes, ReadPlaintextBytesLimit, answer.Status, len(answer.Payload))
		}
	}
	// Invalid/missing contracts are unknown outcomes, never a successful empty page.
	for _, raw := range []string{`{}`, `null`, `[]`, `{"contract_version":"other"}`} {
		answer := open(&router{body: raw}).Handle(context.Background(), request(t, ClassCtl, projectAssistantExample(t, exportRow)))
		if answer.Status != 502 || answer.Code != "pa_outcome_unknown" {
			t.Fatalf("unknown source became success: %+v", answer)
		}
	}
}

func TestProjectAssistantMachineReplyIsolation(t *testing.T) {
	var row cloudvocab.Operation
	for _, r := range projectAssistantRows(t) {
		if r.Op == "project-assistants.get" {
			row = r
		}
	}
	for _, machine := range []string{"source-one", "source-two"} {
		calls := 0
		bridge := Bridge{MachineID: machine, Router: projectAssistantRouter(func(_ context.Context, req LocalRequest) (LocalResponse, error) {
			calls++
			if req.Query["machine_id"] != machine {
				t.Fatal("source machine substituted")
			}
			return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
		})}
		b := projectAssistantExample(t, row)
		b["machine_id"] = machine
		cmd := request(t, ClassCtl, b)
		cmd.Channel = "ctl/" + machine
		if answer := bridge.Handle(context.Background(), cmd); !answer.OK() || calls != 1 {
			t.Fatalf("own source denied: %+v", answer)
		}
		cmd.Channel = "ctl/foreign"
		calls = 0
		if answer := bridge.Handle(context.Background(), cmd); answer.Code != "wrong_machine" || calls != 0 {
			t.Fatal("foreign machine route reached Local")
		}
	}
}
