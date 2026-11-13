package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type paJointFixture struct {
	f            *paServerFixture
	pins         map[string]adaptercloud.PinnedDevice
	old, current pa.Fence
	work         pa.WorkLink
}

func newPAJointFixture(t *testing.T) *paJointFixture {
	t.Helper()
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	return &paJointFixture{f: f, pins: map[string]adaptercloud.PinnedDevice{
		"owner":  f.pin(t, "owner", adaptercloud.ScopeOperate, false),
		"reader": f.pin(t, "reader", adaptercloud.ScopeReadOnly, false),
	}}
}

// Restore validates the complete synthetic aggregate before its real SQL reader
// becomes the producer for the actual TypeScript consumer.
func (j *paJointFixture) seedHistory(t *testing.T) {
	t.Helper()
	f := j.f
	store := featurestore.New(f.s.store.ProjectAssistantDatabase())
	x, err := store.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	x.Bot.Key.BotID, x.Bot.ConversationID = "joint-bot", "joint-conversation"
	x.Conversation.Key, x.Conversation.ID = x.Bot.Key, x.Bot.ConversationID
	x.Conversation.Generation, x.Conversation.BindingID, x.Conversation.ActiveTurnID = 2, "current-binding", "current-turn"
	x.Memory.Key = x.Bot.Key
	x.Context.Memory = x.Memory
	j.old = pa.Fence{Key: x.Bot.Key, Project: x.Bot.Project, ConversationID: x.Bot.ConversationID, BindingID: "old-binding", Generation: 1}
	j.current = j.old
	j.current.BindingID, j.current.Generation = "current-binding", 2
	x.Bindings = []pa.ExecutionBinding{
		{Fence: j.old, Agent: pa.Codex, MemoryRevision: 1, InstructionRevision: 1, Revoked: true},
		{Fence: j.current, Agent: pa.Codex, MemoryRevision: 1, InstructionRevision: 1},
	}
	x.Messages, x.Turns, x.Permissions, x.WorkLinks, x.Events, x.Receipts, x.ReadCursors = nil, nil, nil, nil, nil, nil, nil
	now := time.Now().UTC()
	for i := 1; i <= 120; i++ {
		m := pa.Message{ID: fmt.Sprintf("joint-message-%03d", i), Fence: j.current, Seq: int64(i), Kind: pa.AssistantMessage, Text: "synthetic durable fact", At: now}
		if i <= 2 {
			m.Kind, m.TurnID = pa.UserMessage, "current-turn"
			state := pa.TurnWaitingPermission
			if i == 1 {
				m.Fence, m.TurnID, state = j.old, "old-turn", pa.TurnUnknown
			}
			x.Turns = append(x.Turns, pa.Turn{ID: m.TurnID, Fence: m.Fence, MessageID: m.ID, State: state, Instructions: x.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: "send-" + m.TurnID})
		}
		x.Messages = append(x.Messages, m)
	}
	x.Conversation.LastMessageSeq = int64(len(x.Messages))
	j.work = pa.WorkLink{ID: "old-work", Fence: j.old, TurnID: "old-turn", TaskID: "synthetic-task", State: pa.WorkUnknown}
	x.WorkLinks = []pa.WorkLink{j.work}
	for i := 0; i < 3; i++ {
		x.Permissions = append(x.Permissions, pa.PermissionOffer{ID: fmt.Sprintf("offer-%d", i), Fence: j.current, TurnID: "current-turn", NativeRequestID: fmt.Sprintf("request-%d", i), State: pa.PermissionPending, ExpiresAt: now.Add(time.Hour), Options: []pa.PermissionOption{
			{ID: "allow", Label: "Allow once", Kind: pa.AllowOnce, Allow: true, ReadOnly: true},
			{ID: "reject", Label: "Reject", Kind: pa.RejectOnce, ReadOnly: true},
		}})
	}
	// The original Bot owns source event1. The selected Bot owns600 source
	// events and a trace; a different Bot will own the final machine frontier.
	for i := 0; i < 600; i++ {
		x.Events = append(x.Events, pa.Event{Version: pa.ContractVersion, ID: fmt.Sprintf("joint-source-%d", i), Seq: int64(i + 2), Fence: j.current, Kind: pa.SourceUpdated, At: now,
			Source: &pa.SourceStatus{MachineID: f.key.MachineID, State: pa.SourceOnline, Access: pa.Interactive, Readable: true, Writable: true, Fresh: true, ObservedAt: now}})
	}
	trace := pa.Trace{TurnID: "current-turn", Category: "read", Text: "complete stored trace", Complete: true}
	x.Events = append(x.Events, pa.Event{Version: pa.ContractVersion, ID: "joint-trace", Seq: 602, Fence: j.current, Kind: pa.TraceUpdated, At: now, Trace: &trace})
	if err = store.Restore(context.Background(), x); err != nil {
		t.Fatal("valid joint Restore", err)
	}
	f.key = x.Bot.Key
	other := f.create()
	other.Command.Key.BotID, other.Command.IdempotencyKey = "joint-other", "create-joint-other"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, other))
}

func (j *paJointFixture) seedCapacity(t *testing.T) {
	t.Helper()
	f, now := j.f, time.Now().UTC()
	g := f.get(t, f.local)
	fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID}
	messages := []pa.Message{
		{ID: "user", Fence: fence, Seq: 1, TurnID: "turn", Kind: pa.UserMessage, Text: "synthetic input", At: now},
		{ID: "latest", Fence: fence, Seq: 2, TurnID: "turn", Kind: pa.AssistantMessage, Text: "complete newest fact", At: now},
	}
	turns := map[string]pa.Turn{"turn": {ID: "turn", Fence: fence, MessageID: "user", State: pa.TurnSucceeded, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "synthetic"}, MemoryRevision: 1, IdempotencyKey: "synthetic-turn"}}
	works := map[string]pa.WorkLink{"work": {ID: "work", Fence: fence, TurnID: "turn", TaskID: strings.Repeat("x", cloudops.ReadPlaintextBytesLimit), State: pa.WorkCompleted}}
	a, _ := json.Marshal(messages)
	b, _ := json.Marshal(turns)
	c, _ := json.Marshal(works)
	if _, err := f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Messages',json(?),'$.Conversation.last_message_seq',2,'$.Turns',json(?),'$.WorkLinks',json(?)) WHERE machine=? AND bot=?`, string(a), string(b), string(c), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
}

func (j *paJointFixture) lateWork(t *testing.T) {
	t.Helper()
	f := j.f
	j.work.State = pa.WorkCompleted
	rawWork, _ := json.Marshal(map[string]pa.WorkLink{j.work.ID: j.work})
	tx, err := f.s.store.ProjectAssistantDatabase().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.WorkLinks',json(?)) WHERE machine=? AND bot=?`, string(rawWork), f.key.MachineID, f.key.BotID); err != nil {
		t.Fatal(err)
	}
	r, err := tx.Exec(`INSERT INTO project_assistant_events(machine,bot,event_id,data) VALUES(?,?,?,?)`, f.key.MachineID, f.key.BotID, "late-old-work", []byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	seq, _ := r.LastInsertId()
	event := pa.Event{Version: pa.ContractVersion, ID: "late-old-work", Seq: seq, Fence: j.old, Kind: pa.WorkUpdated, Work: &j.work, At: time.Now().UTC()}
	raw, _ := json.Marshal(event)
	if _, err = tx.Exec(`UPDATE project_assistant_events SET data=? WHERE seq=?`, raw, seq); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	other := f.create()
	other.Command.Key.BotID, other.Command.IdempotencyKey = "later-other", "later-other-create"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, other))
}

type paJointFrame struct {
	ID        int             `json:"id"`
	Kind      string          `json:"kind"`
	Fixture   string          `json:"fixture"`
	Sender    string          `json:"sender"`
	Word      string          `json:"word"`
	Ref       string          `json:"ref"`
	Fields    json.RawMessage `json:"fields"`
	Operation string          `json:"operation"`
	HTTP      struct {
		Input, Method string
		Body          *string
		Idempotency   *string
	} `json:"http"`
	Report json.RawMessage `json:"report"`
}

type paJointCaptureRouter struct {
	t     *testing.T
	frame paJointFrame
}

func (r paJointCaptureRouter) Do(_ context.Context, q cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	r.t.Helper()
	u, err := url.Parse(r.frame.HTTP.Input)
	if err != nil {
		r.t.Fatal(err)
	}
	if q.Method != r.frame.HTTP.Method || q.Path != u.EscapedPath() || q.Viewer != "authenticated-device" || q.Header[actorHeader] != actorDevice {
		r.t.Fatalf("captured method/path/authority changed: %+v", q)
	}
	wantQuery := map[string]string{}
	for k, v := range u.Query() {
		wantQuery[k] = v[0]
	}
	if !reflect.DeepEqual(q.Query, wantQuery) && !(len(q.Query) == 0 && len(wantQuery) == 0) {
		r.t.Fatal("captured query changed")
	}
	if r.frame.HTTP.Body == nil {
		if q.Body != nil {
			r.t.Fatal("captured read gained a body")
		}
	} else {
		var got, want any
		if json.Unmarshal(q.Body, &got) != nil || json.Unmarshal([]byte(*r.frame.HTTP.Body), &want) != nil || !reflect.DeepEqual(got, want) || r.frame.HTTP.Idempotency == nil || q.Header["Idempotency-Key"] != *r.frame.HTTP.Idempotency || r.frame.Ref != *r.frame.HTTP.Idempotency {
			r.t.Fatal("captured complete body or stable action key changed")
		}
	}
	// Only original16 compatibility capture uses this router. View always goes
	// through the real production Router, Server, Host and Store below.
	var body any = map[string]any{"contract_version": pa.ContractVersion, "echo": r.frame.Word}
	source := map[string]any{"machine_id": "source-one", "state": "online", "access": "interactive", "fresh": true, "readable": true, "writable": true, "observed_at": "", "capabilities": []any{}}
	if r.frame.Word == "project-assistants.status" {
		body = map[string]any{"contract_version": pa.ContractVersion, "sources": []any{source}}
	}
	if r.frame.Word == "project-assistants.projects" {
		body = map[string]any{"contract_version": pa.ContractVersion, "source": source, "agents": []string{"claude", "codex"}, "revision": "capture", "items": []any{map[string]any{"target": map[string]string{"machine_id": "source-one", "project_id": "project"}, "label": "Project"}}, "has_more": false, "next_cursor": ""}
	}
	raw, _ := json.Marshal(body)
	return cloudops.LocalResponse{Status: 200, Body: raw}, nil
}

func TestProjectAssistantJointCapturedSourceAndRealReadViews(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for joint TypeScript acceptance", err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]*paJointFixture{"main": newPAJointFixture(t), "capacity": newPAJointFixture(t), "revoked": newPAJointFixture(t), "scope": newPAJointFixture(t)}
	fixtures["main"].seedHistory(t)
	fixtures["capacity"].seedCapacity(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(root, "internal/transport/http/testdata/project-assistants-joint.mjs"), root)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		input.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var artifacts *os.File
	if dir := os.Getenv("CLAWDLINE_PA_JOINT_ARTIFACTS"); dir != "" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		artifacts, err = os.OpenFile(filepath.Join(dir, "go-responses.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer artifacts.Close()
	}
	decoder, encoder := json.NewDecoder(output), json.NewEncoder(input)
	count, report := 0, json.RawMessage(nil)
	for {
		var frame paJointFrame
		if err = decoder.Decode(&frame); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal("Node protocol", err, stderr.String())
		}
		response := map[string]any{"id": frame.ID}
		switch frame.Kind {
		case "init":
			meta := map[string]any{}
			for name, fixture := range fixtures {
				got := fixture.f.get(t, fixture.f.local)
				meta[name] = map[string]any{"key": got.Bot.Key, "project": got.Bot.Project, "conversation_id": got.Conversation.ID}
			}
			response["fixtures"] = meta
		case "bridge", "capture":
			var fields map[string]any
			if err = json.Unmarshal(frame.Fields, &fields); err != nil {
				t.Fatal(err)
			}
			fields["type"], fields["session"], fields["request"] = frame.Word, cloudops.MachineReplySession, frame.Ref
			plain, _ := json.Marshal(fields)
			var bridge cloudops.Bridge
			command := cloudops.Command{Channel: "ctl/source-one", Class: cloudops.ClassCtl, Sender: "authenticated-device", Sequence: uint64(frame.ID), Plaintext: plain}
			if frame.Kind == "capture" {
				if frame.Word == "project-assistants.view" {
					t.Fatal("View must never use the compatibility echo router")
				}
				bridge = cloudops.Bridge{MachineID: "source-one", Router: paJointCaptureRouter{t, frame}, AllowCommands: func() bool { return true }}
			} else {
				j := fixtures[frame.Fixture]
				if j == nil {
					t.Fatal("unknown fixture")
				}
				f := j.f
				key, keyErr := base64.StdEncoding.DecodeString(j.pins[frame.Sender].PublicKey)
				if keyErr != nil {
					t.Fatal(keyErr)
				}
				bridge = cloudops.Bridge{MachineID: f.key.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}, AllowCommands: func() bool { return false }}
				command.Channel, command.Sender, command.VerifiedKey = "ctl/"+f.key.MachineID, frame.Sender, ed25519.PublicKey(key)
			}
			answer := bridge.Handle(ctx, command)
			var envelope struct {
				Body json.RawMessage `json:"body"`
			}
			if err = json.Unmarshal(answer.Payload, &envelope); err != nil || len(envelope.Body) == 0 {
				t.Fatalf("missing real reply body: %v %s", err, answer.Payload)
			}
			sum := sha256.Sum256(answer.Payload)
			response["status"], response["body"], response["sha256"], response["wire_bytes"] = answer.Status, envelope.Body, hex.EncodeToString(sum[:]), len(answer.Payload)
			count++
			if artifacts != nil {
				raw, _ := json.Marshal(map[string]any{"frame": frame, "raw_go_reply": string(answer.Payload), "sha256": hex.EncodeToString(sum[:])})
				if _, err = artifacts.Write(append(raw, '\n')); err != nil {
					t.Fatal(err)
				}
			}
		case "control":
			j := fixtures[frame.Fixture]
			switch frame.Operation {
			case "break_scope":
				// A stored memory identity that disagrees with its owning Bot must
				// fail at the actual reader, never become a shaped success page.
				if _, err = j.f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_bots SET data=json_set(data,'$.Memory.key.bot_id','foreign') WHERE machine=? AND bot=?`, j.f.key.MachineID, j.f.key.BotID); err != nil {
					t.Fatal(err)
				}
			case "late_work":
				j.lateWork(t)
			case "disable":
				if err = j.f.s.projectAssistants.host.Disable(ctx); err != nil {
					t.Fatal(err)
				}
			case "revoke_after_pack":
				j.f.s.projectAssistants.beforeViewPublish = func() {
					if j.f.s.store.ProjectAssistantReader().Stats().InUse != 0 {
						t.Fatal("SQL reader held at publication")
					}
					if _, revokeErr := adaptercloud.NewPinnedStore(filepath.Join(j.f.s.cfg.Dir, cloudkeys.DirName)).Revoke("owner", time.Now()); revokeErr != nil {
						t.Fatal(revokeErr)
					}
				}
			default:
				t.Fatal("unknown control")
			}
			response["ok"] = true
		case "report":
			report = append(json.RawMessage(nil), frame.Report...)
			response["ok"] = true
		default:
			t.Fatal("unknown Node frame")
		}
		for _, fixture := range fixtures {
			if fixture.f.providerCalls.Load() != 0 || fixture.f.s.projectAssistants.host.Usage().Processes != 0 {
				t.Fatal("joint reads initialized a provider/runtime")
			}
		}
		if err = encoder.Encode(response); err != nil {
			t.Fatal(err)
		}
	}
	input.Close()
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatalf("actual TypeScript joint consumer: %v\n%s", err, stderr.String())
	}
	if len(report) == 0 {
		t.Fatal("joint consumer produced no report")
	}
	t.Logf("real Bridge responses=%d; actual TypeScript report=%s", count, report)
	if artifacts != nil {
		if err = os.WriteFile(filepath.Join(filepath.Dir(artifacts.Name()), "node-report.json"), report, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
