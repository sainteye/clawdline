package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// This synthetic ACP process reads the actual private paths in the native
// prompt. It does not contact a provider or manufacture a model response.
func TestBotInputACPPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_INPUT_PEER") != "1" {
		return
	}
	write := func(id json.RawMessage, v any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": v})
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(31)
		}
		switch f.Method {
		case "initialize":
			write(f.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new", "session/load":
			write(f.ID, map[string]string{"sessionId": "input-fixture"})
		case "session/prompt":
			var q struct{ Prompt []struct{ Type, Text string } }
			if json.Unmarshal(f.Params, &q) != nil {
				os.Exit(32)
			}
			reads := []map[string]any{}
			for _, block := range q.Prompt {
				if block.Type != "text" {
					continue
				}
				parts := strings.Split(block.Text, "Attached files (explicitly provided for this message; read with your native tools):")
				if len(parts) != 2 {
					continue
				}
				for _, line := range strings.Split(parts[1], "\n") {
					if !strings.HasPrefix(line, "- ") {
						continue
					}
					var path string
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "- ")), &path) != nil || !filepath.IsAbs(path) {
						os.Exit(33)
					}
					rel, e := filepath.Rel(os.Getenv("CLAWDLINE_INPUT_ROOT"), path)
					if e != nil || !filepath.IsLocal(rel) {
						os.Exit(37)
					}
					data, err := os.ReadFile(path)
					if err != nil {
						os.Exit(34)
					}
					sum := sha256.Sum256(data)
					reads = append(reads, map[string]any{"name": filepath.Base(path), "bytes": len(data), "sha256": hex.EncodeToString(sum[:]), "data": base64.StdEncoding.EncodeToString(data), "absolutePrivatePath": true})
				}
			}
			if len(reads) == 0 {
				os.Exit(35)
			}
			encoded, _ := json.Marshal(reads)
			tmp := os.Getenv("CLAWDLINE_INPUT_READS") + ".tmp"
			if os.WriteFile(tmp, encoded, 0600) != nil || os.Rename(tmp, os.Getenv("CLAWDLINE_INPUT_READS")) != nil {
				os.Exit(36)
			}
			for {
				if _, err := os.Stat(os.Getenv("CLAWDLINE_INPUT_HOLD")); os.IsNotExist(err) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			write(f.ID, map[string]string{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}

func inputACPFixture(t *testing.T) (*paServerFixture, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("owned native supervisor fixture is Unix-only")
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	f := newPAServerFixture(t, true)
	reads := filepath.Join(t.TempDir(), "native-reads.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: executable, Arguments: []string{"-test.run=^TestBotInputACPPeer$"}, Environment: []string{"CLAWDLINE_INPUT_PEER=1", "CLAWDLINE_INPUT_READS=" + reads, "CLAWDLINE_INPUT_HOLD=" + reads + ".hold", "CLAWDLINE_INPUT_ROOT=" + filepath.Join(f.s.cfg.Dir, "project-assistants", "attachments")}}}, Limits: acp.Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 4096, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			c := paServerCaps(b)
			c.SendMessage = true
			return c, nil
		}})
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	return f, reads
}

func inputFixtureFile(id, name string, data []byte) contract.ProjectAssistantInputFile {
	sum := sha256.Sum256(data)
	return contract.ProjectAssistantInputFile{ID: id, Name: name, Bytes: int64(len(data)), Sha256: hex.EncodeToString(sum[:])}
}
func inputFixtureFence(f *paServerFixture, got contract.ProjectAssistantGetResponse) contract.ProjectAssistantFence {
	return contract.ProjectAssistantFence{Key: paWireBotKey(f.key), Project: got.Bot.Project, ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}
}
func inputReadPath(f *paServerFixture, m pa.Message, file pa.InputFile, offset int64) string {
	return "/v1/project-assistants/" + f.key.BotID + "/input?" + (url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {m.Fence.ConversationID}, "message_id": {m.ID}, "id": {file.ID}, "offset": {fmt.Sprint(offset)}}).Encode()
}

func waitInputTurn(t *testing.T, f *paServerFixture) app.Export {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		if len(x.Turns) > 0 && x.Conversation.ActiveTurnID == "" {
			terminal := true
			for _, turn := range x.Turns {
				if turn.State == pa.TurnQueued || turn.State == pa.TurnRunning || turn.State == pa.TurnAccepted {
					terminal = false
				}
				if turn.State == pa.TurnFailed || turn.State == pa.TurnUnknown {
					for _, event := range x.Events {
						if event.Turn != nil && event.Turn.ID == turn.ID {
							t.Logf("input turn %s: state=%s failure=%+v", turn.ID, event.Turn.State, event.Turn.Failure)
						}
					}
					t.Fatalf("synthetic input turn: %+v", x.Turns)
				}
			}
			if terminal {
				return x
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("input fixture timed out", x.Turns)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProjectAssistantInputOwnedHTTP(t *testing.T) {
	f, reads := inputACPFixture(t)
	if dir := os.Getenv("CLAWDLINE_INPUT_E2E_DIR"); dir != "" {
		server := httptest.NewServer(f.h)
		defer server.Close()
		command := exec.Command("node", "../../../web/console/src/project-assistants/inputs.e2e.mjs")
		command.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
		log, e := command.CombinedOutput()
		_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
		if e != nil {
			t.Fatalf("browser: %v\n%s", e, log)
		}
	} else {
		data := []byte("Explicit input from the real HTTP fixture.\n")
		file := inputFixtureFile("11111111-1111-4111-8111-111111111111", "notes.txt", data)
		fence := inputFixtureFence(f, f.get(t, f.local))
		upload := contract.ProjectAssistantUploadRequest{Command: f.command(pa.UploadOperation, "upload-first"), Fence: fence, File: file, Offset: 0, Data: base64.StdEncoding.EncodeToString(data), Done: true}
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/upload", f.local, upload))
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "file-only"), Fence: fence, UploadIds: []string{file.ID}}))
	}
	x := waitInputTurn(t, f)
	log, err := os.ReadFile(reads)
	if err != nil {
		t.Fatal("native did not read bytes", err)
	}
	var native []struct {
		Name                string
		Bytes               int64
		SHA256              string
		Data                string
		AbsolutePrivatePath bool
	}
	if json.Unmarshal(log, &native) != nil || len(native) == 0 || !native[0].AbsolutePrivatePath {
		t.Fatal("native read evidence", string(log))
	}

	for _, read := range native {
		data, e := base64.StdEncoding.DecodeString(read.Data)
		sum := sha256.Sum256(data)
		if e != nil || int64(len(data)) != read.Bytes || hex.EncodeToString(sum[:]) != read.SHA256 {
			t.Fatal("native read bytes/digest mismatch")
		}
		matched := false
		for _, m := range x.Messages {
			for _, file := range m.Files {
				if file.Name == read.Name && file.Bytes == read.Bytes && file.SHA256 == read.SHA256 {
					matched = true
				}
			}
		}
		if !matched {
			t.Fatal("native read was not this Bot's message input")
		}
	}
	for _, m := range x.Messages {
		for _, file := range m.Files {
			w := f.request(t, "GET", inputReadPath(f, m, file, 0), f.local, nil)
			paServerOK(t, w)
			var response contract.ProjectAssistantInputResponse
			_ = json.Unmarshal(w.Body.Bytes(), &response)
			data, err := base64.StdEncoding.DecodeString(response.Data)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(w.Body.Bytes(), []byte(f.s.cfg.Dir)) || len(data) != int(min(file.Bytes, int64(pa.MaxInputChunkBytes))) {
				t.Fatal("private path or chunk mismatch")
			}
			if dir := os.Getenv("CLAWDLINE_INPUT_E2E_DIR"); dir != "" {
				_ = os.WriteFile(filepath.Join(dir, "native-reads.json"), log, 0600)
			}
		}
	}
}

func inputUploadRequest(f *paServerFixture, fence contract.ProjectAssistantFence, id, name string, data []byte) contract.ProjectAssistantUploadRequest {
	return contract.ProjectAssistantUploadRequest{Command: f.command(pa.UploadOperation, "upload-"+id), Fence: fence, File: inputFixtureFile(id, name, data), Data: base64.StdEncoding.EncodeToString(data), Done: true}
}

func TestProjectAssistantInputHTTPAdmissionReadbackAndRestart(t *testing.T) {
	f, _ := inputACPFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID
	fence := inputFixtureFence(f, f.get(t, f.local))
	data := []byte{0, 255, 3, 10}
	q := inputUploadRequest(f, fence, "11111111-1111-4111-8111-111111111111", "binary", data)
	if w := f.request(t, "POST", base+"/upload", f.readonly, q); w.Code != 403 {
		t.Fatal("read-only upload admitted", w.Code)
	}
	// Incomplete data cannot reach the native prompt.
	q.Done = false
	q.Data = base64.StdEncoding.EncodeToString(data[:2])
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, q))
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "send-input"), Fence: fence, UploadIds: []string{q.File.ID}}
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/messages", f.local, send), 400, pa.Invalid)
	q.Offset = 3
	q.Data = base64.StdEncoding.EncodeToString(data[3:])
	q.Done = true
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/upload", f.local, q), 400, pa.Invalid)
	q.Offset = 0
	q.Data = base64.StdEncoding.EncodeToString([]byte{8, 9})
	q.Done = false
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/upload", f.local, q), 409, pa.IdempotencyConflict)
	q.Data = base64.StdEncoding.EncodeToString(data[:2])
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, q))
	q.Offset = 2
	q.Data = base64.StdEncoding.EncodeToString(data[2:])
	q.Done = true
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, q))
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, q))
	// Lose the first response: exact send replay resolves its durable receipt.
	first := f.request(t, "POST", base+"/messages", f.local, send)
	paServerOK(t, first)
	x := waitInputTurn(t, f)
	again := f.request(t, "POST", base+"/messages", f.local, send)
	paServerOK(t, again)
	var a, b contract.ProjectAssistantReceiptResponse
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(again.Body.Bytes(), &b)
	if a.Receipt.ID != b.Receipt.ID {
		t.Fatal("uncertain send replay created another receipt")
	}
	current := inputFixtureFence(f, f.get(t, f.local))
	send.Command = f.command(pa.SendOperation, "new-key")
	send.Fence = current
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/messages", f.local, send), 403, pa.ScopeDenied)
	var m pa.Message
	for _, candidate := range x.Messages {
		if len(candidate.Files) > 0 {
			m = candidate
		}
	}
	path := inputReadPath(f, m, m.Files[0], 0)
	paServerOK(t, f.request(t, "GET", path, f.readonly, nil))
	paServerOK(t, f.request(t, "GET", path, f.lan, nil))
	paAssertHTTPFailure(t, f.request(t, "GET", strings.Replace(path, "message_id="+m.ID, "message_id=foreign", 1), f.local, nil), 404, pa.NotFound)
	paAssertHTTPFailure(t, f.request(t, "GET", strings.Replace(path, "conversation_id="+m.Fence.ConversationID, "conversation_id=foreign", 1), f.local, nil), 403, pa.ScopeDenied)
	if err := f.s.StopProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
	f.s = restarted
	f.h = restarted.Handler()
	paServerOK(t, f.request(t, "GET", path, f.local, nil))
	// The final publication fence refuses bytes read before local revocation.
	f.s.projectAssistants.beforeViewPublish = func() { device := f.s.gate().auth.Verify(f.lan).Device; _ = f.s.gate().auth.Revoke(device) }
	paAssertHTTPFailure(t, f.request(t, "GET", path, f.lan, nil), 403, pa.Revoked)
	f.s.projectAssistants.beforeViewPublish = nil
	paServerOK(t, f.request(t, "POST", base+"/delete", f.local, contract.ProjectAssistantDeleteRequest{Command: f.command(pa.DeleteOperation, "delete-inputs")}))
	if _, err = os.Stat(filepath.Join(f.s.cfg.Dir, "project-assistants", "attachments")); err == nil {
		// The shared root may remain, but the deleted Bot's input path cannot.
		if _, _, err = (store.Inputs{Dir: filepath.Join(f.s.cfg.Dir, "project-assistants", "attachments")}).Resolve(context.Background(), m.Fence, m.Files[0].ID); err == nil {
			t.Fatal("delete retained readable input")
		}
	}
}

// The paired-device fixture starts at the verified encrypted-channel seam:
// cryptographic sender identity is supplied by the channel, not the JSON body.
func TestProjectAssistantInputCloudBoundary(t *testing.T) {
	f, _ := inputACPFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID
	pins := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName))
	ownerSigner, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	readerSigner, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, readerKey := ownerSigner.PublicKey(), readerSigner.PublicKey()
	for _, row := range []struct {
		id, scope string
		key       domaincloud.DeviceKey
	}{{"input-owner", adaptercloud.ScopeOperate, ownerSigner}, {"input-reader", adaptercloud.ScopeReadOnly, readerSigner}} {
		if err := pins.Pin(f.identity.AccountID, adaptercloud.PinnedDevice{DeviceID: row.id, PublicKey: base64.StdEncoding.EncodeToString(row.key.PublicKey()), Fingerprint: row.key.Fingerprint(), PairedAt: time.Now().Unix(), Scope: row.scope}); err != nil {
			t.Fatal(err)
		}
	}
	contentKey, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}, AllowCommands: func() bool { return true }}

	bridge.Authority = func(_ context.Context, sender string, key ed25519.PublicKey, write bool) cloudops.Authority {
		current, ok, err := pins.PublicKeyFor(sender)
		refused, e := pins.Refused(sender)
		scope, other := pins.ViewerScope(sender)
		allowed := err == nil && e == nil && other == nil && !refused && ok && bytes.Equal(current, key)
		return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: allowed, WriteGateAllows: allowed && (!write || scope == adaptercloud.ScopeOperate), ViewerReadOnly: scope == adaptercloud.ScopeReadOnly}
	}
	sequence := uint64(0)
	call := func(sender string, key []byte, fields map[string]any) cloudops.Answer {
		raw, _ := json.Marshal(fields)
		signer := ownerSigner
		if bytes.Equal(key, readerKey) {
			signer = readerSigner
		}
		sequence++
		envelope, err := domaincloud.Seal(raw, domaincloud.SealParams{Ch: "ctl/" + f.key.MachineID, Seq: sequence, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: "fixture-key", Sender: sender, Key: contentKey, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		lookup := func(id string) (ed25519.PublicKey, bool) {
			current, ok, e := pins.PublicKeyFor(id)
			refused, other := pins.Refused(id)
			return current, ok && e == nil && other == nil && !refused
		}
		opened, err := envelope.Open(contentKey, lookup)
		if err != nil {
			return cloudops.Answer{Status: 403}
		}
		verified, ok := lookup(sender)
		if !ok {
			return cloudops.Answer{Status: 403}
		}
		return bridge.Handle(context.Background(), cloudops.Command{Channel: envelope.Ch, Class: cloudops.ClassCtl, Sender: sender, VerifiedKey: verified, Sequence: sequence, Plaintext: opened})
	}

	fence := inputFixtureFence(f, f.get(t, f.local))
	data := []byte{0, 255, 10, 3}
	q := inputUploadRequest(f, fence, "11111111-1111-4111-8111-111111111111", "binary", data)
	raw, _ := json.Marshal(q)
	fields := map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	fields["type"] = "project-assistants.upload"
	fields["session"] = cloudops.MachineReplySession
	fields["request"] = q.Command.IdempotencyKey
	if answer := call("input-reader", readerKey, fields); answer.OK() {
		t.Fatal("read-only upload admitted")
	}
	if answer := call("input-owner", readerKey, fields); answer.OK() {
		t.Fatal("wrong signing key admitted")
	}
	if answer := call("input-owner", ownerKey, fields); !answer.OK() {
		t.Fatalf("Cloud upload: %d %s", answer.Status, answer.Payload)
	}
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "cloud-input"), Fence: fence, UploadIds: []string{q.File.ID}}
	raw, _ = json.Marshal(send)
	fields = map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	fields["type"] = "project-assistants.send-files"
	fields["session"] = cloudops.MachineReplySession
	fields["request"] = send.Command.IdempotencyKey
	if answer := call("input-owner", ownerKey, fields); !answer.OK() {
		t.Fatalf("Cloud send: %d %s", answer.Status, answer.Payload)
	}
	x := waitInputTurn(t, f)
	var m pa.Message
	for _, candidate := range x.Messages {
		if len(candidate.Files) > 0 {
			m = candidate
		}
	}
	fields = map[string]any{"type": "project-assistants.input", "session": cloudops.MachineReplySession, "request": strings.Repeat("<", 128), "machine_id": f.key.MachineID, "bot_id": f.key.BotID, "conversation_id": m.Fence.ConversationID, "message_id": m.ID, "id": m.Files[0].ID, "offset": 0}
	answer := call("input-reader", readerKey, fields)
	if !answer.OK() || len(answer.Payload) > cloudops.ReadPlaintextBytesLimit {
		t.Fatalf("Cloud read: %d %s", answer.Status, answer.Payload)
	}
	var reply struct {
		Body contract.ProjectAssistantInputResponse
	}
	if json.Unmarshal(answer.Payload, &reply) != nil || reply.Body.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatal("Cloud bytes changed")
	}
	fields["offset"] = -1
	if call("input-reader", readerKey, fields).OK() {
		t.Fatal("negative offset admitted")
	}
	fields["offset"] = 0
	fields["machine_id"] = "foreign"
	if call("input-reader", readerKey, fields).OK() {
		t.Fatal("foreign machine admitted")
	}
	fields["machine_id"] = f.key.MachineID
	// Revoke after bytes were loaded but before HTTP/Cloud publication.
	f.s.projectAssistants.beforeViewPublish = func() {
		_, _ = adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("input-reader", time.Now())
	}
	if call("input-reader", readerKey, fields).OK() {
		t.Fatal("revoked reader received late bytes")
	}
	f.s.projectAssistants.beforeViewPublish = nil
	if call("input-reader", readerKey, fields).OK() {
		t.Fatal("revoked reader admitted")
	}
	// Existing local reads still work, proving refusal belongs to the pairing.
	paServerOK(t, f.request(t, "GET", inputReadPath(f, m, m.Files[0], 0), f.local, nil))
	_ = base
}

func TestProjectAssistantInputConfigurationAndPrincipalFence(t *testing.T) {
	f, _ := inputACPFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID
	got := f.get(t, f.local)
	q := inputUploadRequest(f, inputFixtureFence(f, got), "11111111-1111-4111-8111-111111111111", "two", []byte{1, 2})
	q.Done = false
	q.Data = base64.StdEncoding.EncodeToString([]byte{1})
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, q))
	q.Offset = 1
	q.Data = base64.StdEncoding.EncodeToString([]byte{2})
	q.Done = true
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/upload", f.lan, q), 409, pa.StaleVersion)
	got = f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "reconfigure-input"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: got.Bot.Version, Name: got.Bot.Name, Appearance: got.Bot.Appearance, Instructions: "A revised role"}}
	paServerOK(t, f.request(t, "PATCH", base, f.local, edit))
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/upload", f.local, q), 409, pa.StaleVersion)
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "stale-file"), Fence: q.Fence, UploadIds: []string{q.File.ID}}
	paAssertHTTPFailure(t, f.request(t, "POST", base+"/messages", f.local, send), 409, pa.StaleVersion)
}

func TestProjectAssistantInputQueueTakebackAndStopEvidence(t *testing.T) {
	f, reads := inputACPFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID
	if err := os.WriteFile(reads+".hold", nil, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(reads + ".hold")
	uploadSend := func(id, token string) pa.Receipt {
		t.Helper()
		fence := inputFixtureFence(f, f.get(t, token))
		q := inputUploadRequest(f, fence, id, "input", []byte{0, 255, 3})
		paServerOK(t, f.request(t, "POST", base+"/upload", token, q))
		w := f.request(t, "POST", base+"/messages", token, contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "send-"+id), Fence: fence, UploadIds: []string{id}})
		paServerOK(t, w)
		var r contract.ProjectAssistantReceiptResponse
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return paDomainReceiptForInputTest(r.Receipt)
	}
	uploadSend("11111111-1111-4111-8111-111111111111", f.local)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(reads); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native read did not start")
		}
		time.Sleep(time.Millisecond)
	}
	queued := uploadSend("22222222-2222-4222-8222-222222222222", f.local)
	find := func(id string) pa.Message {
		t.Helper()
		x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range x.Messages {
			if len(m.Files) > 0 && m.Files[0].ID == id {
				return m
			}
		}
		t.Fatal("message absent", id)
		return pa.Message{}
	}
	taken := find("22222222-2222-4222-8222-222222222222")
	inputStore := store.Inputs{Dir: f.s.projectAssistantAttachments().Dir}
	takenPath, err := inputStore.SentPath(context.Background(), taken.Fence, taken.Files[0])
	if err != nil {
		t.Fatal("queued private file absent before takeback", err)
	}
	got := f.get(t, f.local)
	takeback := contract.ProjectAssistantStopRequest{Command: f.command(pa.StopOperation, "unqueue-input"), Fence: inputFixtureFence(f, got), TurnID: queued.ResourceID}
	paServerOK(t, f.request(t, "POST", base+"/stop", f.local, takeback))
	paAssertHTTPFailure(t, f.request(t, "GET", inputReadPath(f, taken, taken.Files[0], 0), f.local, nil), 404, pa.NotFound)
	if _, err := os.Stat(takenPath); !os.IsNotExist(err) {
		t.Fatalf("takeback left private file readable at its native path: %v", err)
	}
	paServerOK(t, f.request(t, "POST", base+"/stop", f.local, takeback))
	if _, _, err := inputStore.Resolve(context.Background(), taken.Fence, taken.Files[0].ID); pa.Code(err) != pa.NotFound {
		t.Fatal("takeback retained upload metadata", err)
	}
	uploadSend("33333333-3333-4333-8333-333333333333", f.local)
	dropped := find("33333333-3333-4333-8333-333333333333")
	staged := inputUploadRequest(f, inputFixtureFence(f, f.get(t, f.local)), "44444444-4444-4444-8444-444444444444", "partial", []byte{1, 2})
	staged.Done = false
	staged.Data = base64.StdEncoding.EncodeToString([]byte{1})
	paServerOK(t, f.request(t, "POST", base+"/upload", f.local, staged))
	got = f.get(t, f.local)
	paServerOK(t, f.request(t, "POST", base+"/stop", f.local, contract.ProjectAssistantStopRequest{Command: f.command(pa.StopOperation, "stop-inputs"), Fence: inputFixtureFence(f, got), TurnID: got.Conversation.ActiveTurnID}))
	paServerOK(t, f.request(t, "GET", inputReadPath(f, dropped, dropped.Files[0], 0), f.local, nil))
	staged.Offset = 1
	staged.Data = base64.StdEncoding.EncodeToString([]byte{2})
	staged.Done = true
	w := f.request(t, "POST", base+"/upload", f.local, staged)
	if w.Code != 409 {
		t.Fatalf("Stop allowed an old partial upload: %d %s", w.Code, w.Body)
	}

}

func paDomainReceiptForInputTest(r contract.ProjectAssistantReceipt) pa.Receipt {
	return pa.Receipt{ID: r.ID, ResourceID: r.ResourceID}
}
