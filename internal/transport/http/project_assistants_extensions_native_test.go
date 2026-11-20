package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

var extensionNativeCanary = []byte{0, 255, 128, 42, 1, 0}

// Both native ACP and connector MCP run as owned local subprocesses of this
// test executable. No model, provider account or external endpoint is used.
func TestExtensionsNativeProtocolPeer(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if arg == "extensions-native" || arg == "extensions-connector" {
			mode = arg
		}
	}
	if mode == "" {
		return
	}
	var endpoint, bearer string
	folderRead := false
	initialized, ready := false, false
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var f struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(21)
		}
		var result any = map[string]any{}
		if mode == "extensions-connector" {
			switch f.Method {
			case "initialize":
				initialized = true
				result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fixture", "version": "1"}}
			case "notifications/initialized":
				if !initialized {
					os.Exit(22)
				}
				ready = true
				continue
			case "tools/list":
				if !ready {
					os.Exit(23)
				}
				result = map[string]any{"tools": []any{map[string]any{"name": "fixture_echo", "inputSchema": map[string]string{"type": "object"}}}}
			case "tools/call":
				if !ready {
					os.Exit(24)
				}
				result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "owned native connector response"}}}
			default:
				os.Exit(25)
			}
		} else {
			switch f.Method {
			case "initialize":
				result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}}
			case "session/new":
				var prompt struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				_ = json.Unmarshal(f.Params["systemPrompt"], &prompt)
				for _, p := range prompt.Content {
					match := regexp.MustCompile(`read "([^"]+/SKILL\.md)"`).FindStringSubmatch(p.Text)
					if len(match) == 2 {
						body, e := os.ReadFile(filepath.Join(filepath.Dir(match[1]), "assets", "canary.bin"))
						folderRead = e == nil && bytes.Equal(body, extensionNativeCanary)
					}
				}
				var servers []struct {
					Name, URL string
					Headers   []struct{ Name, Value string }
				}
				_ = json.Unmarshal(f.Params["mcpServers"], &servers)
				for _, server := range servers {
					if server.Name == "fixture-connector" {
						endpoint = server.URL
						for _, h := range server.Headers {
							if h.Name == "Authorization" {
								bearer = h.Value
							}
						}
					}
				}
				result = map[string]any{"sessionId": "extensions-native-session", "configOptions": []any{}}
			case "session/prompt":
				if !folderRead || endpoint == "" {
					os.Exit(26)
				}
				for n, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"native-fixture","version":"1"}}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fixture_echo","arguments":{}}}`} {
					r, _ := http.NewRequest("POST", endpoint, strings.NewReader(body))
					r.Header.Set("Authorization", bearer)
					r.Header.Set("Content-Type", "application/json")
					response, e := http.DefaultClient.Do(r)
					if e != nil {
						os.Exit(27)
					}
					data, e := io.ReadAll(response.Body)
					response.Body.Close()
					if e != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
						os.Exit(28)
					}
					if n == 3 && !strings.Contains(string(data), "owned native connector response") {
						os.Exit(29)
					}
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "extensions-native-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Owned folder binary and stdio connector read through native ACP."}}}})
				result = map[string]string{"stopReason": "end_turn"}
			case "session/cancel":
				continue
			}
		}
		if len(f.ID) > 0 {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": result})
		}
	}
	os.Exit(0)
}

func TestFolderAndStdioExtensionsCrossStoredHTTPAndNativeACP(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	f, _ := loginCapabilitiesFixture(t, func(original app.Provider) app.Provider {
		provider, e := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestExtensionsNativeProtocolPeer$", "--", "extensions-native"}}}, Limits: acp.Limits{RPCPending: 16, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 4 << 20, StderrBytes: 64 << 10, OwnedDescendants: 32, WriteTimeout: time.Second, CleanupTimeout: 5 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return paServerCaps(b), nil
		}})
		if e != nil {
			t.Fatal(e)
		}
		return provider
	})
	g := f.get(t, f.local)
	skills := []squad.Skill{{SkillID: "owned-folder", Folder: true, Content: "# Owned fixture\nRead assets/canary.bin.", Files: []squad.SkillFile{{Path: "assets/canary.bin", ContentBase64: base64.StdEncoding.EncodeToString(extensionNativeCanary)}}, Name: squad.Names{En: "Owned folder fixture"}}}
	raw, _ := json.Marshal(skills)
	if e = os.WriteFile(filepath.Join(f.s.cfg.Dir, "synthetic-installed-skill.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	definitions, _ := json.Marshal([]botcapabilities.Connector{{ID: "owned-stdio", Name: "fixture-connector", Executable: exe, Arguments: []string{"-test.run=^TestExtensionsNativeProtocolPeer$", "--", "extensions-connector"}, Project: paDomainProjectScope(g.Bot.Project), Agent: pa.Codex, Enabled: true}})
	if e = os.WriteFile(filepath.Join(f.s.cfg.Dir, "project-assistants", "connectors.json"), definitions, 0600); e != nil {
		t.Fatal(e)
	}
	path := "/v1/project-assistants/" + f.key.BotID + "/extensions?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {g.Bot.ConversationID}}.Encode()
	w := f.request(t, "GET", path, f.readonly, nil)
	paServerOK(t, w)
	catalog := paHTTPDecode[contract.ProjectAssistantExtensionsResponse](t, w).Catalog
	selection := contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{}}
	for _, offer := range catalog.Items {
		ref := contract.ProjectAssistantExtensionRef{ID: offer.ID, Fingerprint: offer.Fingerprint}
		if offer.Kind == "skill" {
			selection.Skills = append(selection.Skills, ref)
		} else {
			selection.Connectors = append(selection.Connectors, ref)
		}
	}
	if len(selection.Skills) != 1 || len(selection.Connectors) != 1 {
		t.Fatal("owned definitions not offered", catalog)
	}
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "owned-native-extensions"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Extensions: &selection}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	sent := f.send(t, f.local, "owned-native-extensions")
	paServerOK(t, sent)
	turn := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
	x := fallbackHTTPWait(t, f)
	for _, m := range x.Messages {
		if m.TurnID == turn && strings.Contains(m.Text, "Owned folder binary and stdio connector read through native ACP.") {
			return
		}
	}
	t.Fatal(fmt.Sprintf("native folder/stdio proof missing: turns=%v", x.Turns))
}
