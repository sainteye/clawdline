package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

const outputFixtureHTML = `<!doctype html><html><body><h1>Interactive report</h1><button onclick="this.textContent='Clicked'">Try interaction</button><button onclick="location.href='https://escape.invalid/navigation'">Try blocked navigation</button><p id="isolation"></p><a href="https://example.invalid/report">Reference</a><script>
let denied=0;try{parent.parent.document.body.dataset.compromised='yes'}catch{denied++}try{localStorage.setItem('authority','bad')}catch{denied++}document.querySelector('#isolation').textContent='Isolated '+denied;
window.open('https://escape.invalid/popup');fetch('https://escape.invalid/network').catch(()=>{});
</script></body></html>`

// A real subprocess speaks ACP and then the owned MCP HTTP descriptor. It is
// synthetic protocol traffic, never a provider/model request.
func TestBotOutputMCPPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_OUTPUT_PEER") != "1" {
		return
	}
	var endpoint, authorization string
	write := func(id json.RawMessage, v any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": v})
	}
	request := func(method string, id any, params any) []byte {
		v := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != nil {
			v["id"] = id
		}
		if params != nil {
			v["params"] = params
		}
		body, _ := json.Marshal(v)
		r, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		c := http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
		resp, e := c.Do(r)
		if e != nil {
			os.Exit(21)
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || resp.StatusCode >= 400 {
			os.Exit(22)
		}
		return b
	}
	call := func(name string, args any, failed bool) json.RawMessage {
		b := request("tools/call", "fixture-call", map[string]any{"name": name, "arguments": args})
		var v struct {
			Result struct {
				IsError bool
				Content []struct{ Text string }
			}
			Error json.RawMessage
		}
		if json.Unmarshal(b, &v) != nil || len(v.Error) > 0 || v.Result.IsError != failed || len(v.Result.Content) != 1 {
			fmt.Fprintln(os.Stderr, string(b))
			os.Exit(23)
		}
		return json.RawMessage(v.Result.Content[0].Text)
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(24)
		}
		switch f.Method {
		case "initialize":
			write(f.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new":
			var q struct {
				MCPServers []struct {
					URL     string
					Headers []struct{ Name, Value string }
				}
			}
			if json.Unmarshal(f.Params, &q) != nil || len(q.MCPServers) != 1 {
				os.Exit(25)
			}
			endpoint = q.MCPServers[0].URL
			authorization = q.MCPServers[0].Headers[0].Value
			write(f.ID, map[string]string{"sessionId": "output-fixture"})
		case "session/prompt":
			request("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "output-fixture", "version": "1"}})
			request("notifications/initialized", nil, nil)
			root := os.Getenv("CLAWDLINE_OUTPUT_PROJECT")
			file := map[string]any{"idempotency_key": "file-one", "path": filepath.Join(root, "result.bin"), "name": "result.bin"}
			call("send_file", file, false)
			_ = os.Remove(filepath.Join(root, "result.bin"))
			call("send_file", file, false)
			file["name"] = "changed.bin"
			call("send_file", file, true)
			artifact := map[string]any{"idempotency_key": "artifact-one", "path": filepath.Join(root, "report.html"), "title": "Interactive report"}
			var first pa.Output
			if json.Unmarshal(call("publish_artifact", artifact, false), &first) != nil || first.ArtifactID == "" {
				os.Exit(26)
			}
			if os.WriteFile(filepath.Join(root, "report.html"), []byte(outputFixtureHTML), 0600) != nil {
				os.Exit(27)
			}
			artifact["artifact"] = first.ArtifactID
			artifact["idempotency_key"] = "artifact-two"
			call("publish_artifact", artifact, false)
			call("publish_artifact", artifact, false)
			call("send_message", map[string]any{"idempotency_key": "summary", "text": "The report is ready.\n\n" + strings.Repeat("The report includes an example you can try.\n\n", 18)}, false)
			call("send_message", map[string]any{"idempotency_key": "follow-up", "text": "You can open it here."}, false)
			var listed []app.ArtifactInfo
			if json.Unmarshal(call("list_artifacts", map[string]any{}, false), &listed) != nil || len(listed) != 1 {
				os.Exit(28)
			}
			saved, e := os.ReadFile(listed[0].Path)
			if e != nil || string(saved) != outputFixtureHTML {
				os.Exit(29)
			}
			write(f.ID, map[string]string{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}

func outputMCPFixture(t *testing.T) (*paServerFixture, app.Export) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native supervisor fixture is Unix-only")
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	f := newPAServerFixture(t, true)
	data := bytes.Repeat([]byte{0, 255, 3, 10}, pa.MaxOutputChunkBytes/4+1)
	if err := os.WriteFile(filepath.Join(f.project, "result.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.project, "report.html"), []byte("<h1>First version</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: executable, Arguments: []string{"-test.run=^TestBotOutputMCPPeer$"}, Environment: []string{"CLAWDLINE_OUTPUT_PEER=1", "CLAWDLINE_OUTPUT_PROJECT=" + f.project}}}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 4096, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			c := paServerCaps(b)
			c.SendMessage = true
			return c, nil
		}})
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "publish-fixture"))
	deadline := time.Now().Add(15 * time.Second)
	for {
		x, e := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
		if e != nil {
			t.Fatal(e)
		}
		if len(x.Turns) > 0 && x.Conversation.ActiveTurnID == "" && (x.Turns[0].State == pa.TurnSucceeded || x.Turns[0].State == pa.TurnFailed || x.Turns[0].State == pa.TurnUnknown) {
			if len(app.PublishedOutputs(x.Messages)) != 2 {
				t.Fatalf("publication failed: turns=%+v messages=%+v", x.Turns, x.Messages)
			}
			return f, x
		}
		if time.Now().After(deadline) {
			t.Fatal("MCP fixture timed out", x.Turns)
		}
		time.Sleep(time.Millisecond)
	}
}

func paOutputFixturePath(f *paServerFixture, m pa.Message, offset int64, artifact bool) string {
	q := url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {m.Fence.ConversationID}, "message_id": {m.ID}, "id": {m.Output.ID}, "artifact_id": {""}, "offset": {fmt.Sprint(offset)}}
	if artifact {
		q.Set("id", "")
		q.Set("artifact_id", m.Output.ArtifactID)
	}
	return "/v1/project-assistants/" + f.key.BotID + "/output?" + q.Encode()
}

func TestProjectAssistantOutputOwnedMCPHTTP(t *testing.T) {
	f, x := outputMCPFixture(t)
	cards := []pa.Message{}
	for _, m := range x.Messages {
		if m.Output != nil {
			cards = append(cards, m)
		}
	}
	if len(cards) != 3 {
		t.Fatal("retry created duplicate cards", len(cards))
	}
	var assembled []byte
	for offset := int64(0); offset < cards[0].Output.Bytes; {
		w := f.request(t, "GET", paOutputFixturePath(f, cards[0], offset, false), f.local, nil)
		paServerOK(t, w)
		var r contract.ProjectAssistantOutputResponse
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		chunk, err := base64.StdEncoding.DecodeString(r.Data)
		if err != nil {
			t.Fatal(err)
		}
		assembled = append(assembled, chunk...)
		offset += int64(len(chunk))
	}
	if !bytes.Equal(assembled, bytes.Repeat([]byte{0, 255, 3, 10}, pa.MaxOutputChunkBytes/4+1)) {
		t.Fatal("file snapshot changed")
	}
	w := f.request(t, "GET", paOutputFixturePath(f, cards[1], 0, true), f.local, nil)
	paServerOK(t, w)
	var latest contract.ProjectAssistantOutputResponse
	_ = json.Unmarshal(w.Body.Bytes(), &latest)
	html, _ := base64.StdEncoding.DecodeString(latest.Data)
	if string(html) != outputFixtureHTML || latest.Output.ID != cards[2].Output.ID {
		t.Fatal("old card did not resolve latest whole snapshot")
	}
	foreign := strings.Replace(paOutputFixturePath(f, cards[0], 0, false), "conversation_id="+x.Conversation.ID, "conversation_id=foreign", 1)
	paAssertHTTPFailure(t, f.request(t, "GET", foreign, f.local, nil), 403, pa.ScopeDenied)
	missing := strings.Replace(paOutputFixturePath(f, cards[0], 0, false), "message_id="+url.QueryEscape(cards[0].ID), "message_id=foreign", 1)
	paAssertHTTPFailure(t, f.request(t, "GET", missing, f.local, nil), 404, pa.NotFound)
	// A new Server and Store read the same committed card and immutable bytes.
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
	paServerOK(t, f.request(t, "GET", paOutputFixturePath(f, cards[0], 0, false), f.local, nil))
	// Revocation at the final publication fence discards already-read bytes.
	f.s.projectAssistants.beforeViewPublish = func() { id := f.s.gate().auth.Verify(f.lan).Device; _ = f.s.gate().auth.Revoke(id) }
	paAssertHTTPFailure(t, f.request(t, "GET", paOutputFixturePath(f, cards[0], 0, false), f.lan, nil), 403, pa.Revoked)
	f.s.projectAssistants.beforeViewPublish = nil
	if dir := os.Getenv("CLAWDLINE_OUTPUT_E2E_DIR"); dir != "" {
		server := httptest.NewServer(f.h)
		defer server.Close()
		command := exec.Command("node", "../../../web/console/src/project-assistants/outputs.e2e.mjs")
		command.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
		log, e := command.CombinedOutput()
		if writeErr := os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if e != nil {
			t.Fatalf("browser: %v\n%s", e, log)
		}
	}
}

// Cloud output reads start at the verified encrypted-channel boundary.
func TestProjectAssistantOutputCloudRead(t *testing.T) {
	f, x := outputMCPFixture(t)
	cards := []pa.Message{}
	for _, m := range x.Messages {
		if m.Output != nil {
			cards = append(cards, m)
		}
	}
	// Start at the verified encrypted-channel seam and exercise the generated
	// decoder, pairing grant, actual router and bounded complete Cloud reply.
	pin := f.pin(t, "output-reader", adaptercloud.ScopeReadOnly, false)
	public, _ := base64.StdEncoding.DecodeString(pin.PublicKey)
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}, AllowCommands: func() bool { return true }}
	fields := map[string]any{"type": "project-assistants.output", "session": cloudops.MachineReplySession, "request": strings.Repeat("<", 128), "machine_id": f.key.MachineID, "bot_id": f.key.BotID, "conversation_id": x.Conversation.ID, "message_id": cards[0].ID, "id": cards[0].Output.ID, "artifact_id": "", "offset": 0}
	ask := func() cloudops.Answer {
		raw, _ := json.Marshal(fields)
		return bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: "output-reader", VerifiedKey: ed25519.PublicKey(public), Sequence: 1, Plaintext: raw})
	}
	answer := ask()
	if !answer.OK() || len(answer.Payload) > cloudops.ReadPlaintextBytesLimit {
		t.Fatalf("Cloud output reply: %d %s", answer.Status, answer.Payload)
	}
	var carried struct {
		Body contract.ProjectAssistantOutputResponse
	}
	if err := json.Unmarshal(answer.Payload, &carried); err != nil || carried.Body.Output.ID != cards[0].Output.ID {
		t.Fatal("Cloud output scope", err)
	}
	fields["machine_id"] = "foreign"
	if ask().OK() {
		t.Fatal("foreign Cloud machine admitted")
	}
	fields["machine_id"] = f.key.MachineID
	fields["message_id"] = "foreign"
	if ask().OK() {
		t.Fatal("foreign Cloud card admitted")
	}
	fields["message_id"] = cards[0].ID
	fields["offset"] = -1
	if ask().OK() {
		t.Fatal("invalid Cloud chunk admitted")
	}
	fields["offset"] = 0
	if _, err := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("output-reader", time.Now()); err != nil {
		t.Fatal(err)
	}
	if ask().OK() {
		t.Fatal("revoked Cloud reader admitted")
	}
}
