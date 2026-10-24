package antigravityacp_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	acp "github.com/sainteye/clawdline/internal/adapters/antigravityacp"
)

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *acp.CodecError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	if err.Error() != "antigravity_acp_"+code {
		t.Fatalf("error leaked data: %v", err)
	}
}
func TestObservedInitialize(t *testing.T) {
	b, err := os.ReadFile("testdata/initialize-1.4.1.json")
	if err != nil {
		t.Fatal(err)
	}
	i, err := acp.DecodeInitialize(b, []byte(`"initialize-fixture"`))
	if err != nil {
		t.Fatal(err)
	}
	if i.ProtocolVersion != 1 || i.AgentInfo.Name != "antigravity-acp" || i.AgentInfo.Title != "Google Antigravity" || i.AgentInfo.Version != "1.4.1" {
		t.Fatalf("unexpected declaration: %+v", i)
	}
	c := i.AgentCapabilities
	for _, v := range []*bool{c.LoadSession, c.Prompt.Image, c.Prompt.Audio, c.Prompt.EmbeddedContext, c.MCP.HTTP, c.MCP.SSE} {
		if v == nil || !*v {
			t.Fatal("missing observed capability")
		}
	}
	if string(c.Session.List) != "{}" || string(c.Session.Resume) != "{}" || string(c.Auth.Logout) != "{}" {
		t.Fatal("object declarations lost")
	}
	var meta any
	if json.Unmarshal(c.Meta, &meta) != nil || !strings.Contains(string(c.Meta), `"antigravity"`) {
		t.Fatal("extension lost")
	}
	ids := []string{"oauth-personal", "oauth-business", "gemini-api-key", "agent-platform"}
	if len(i.AuthMethods) != len(ids) {
		t.Fatal("auth methods changed")
	}
	for n, id := range ids {
		if i.AuthMethods[n].ID != id || i.AuthMethods[n].Type != nil {
			t.Fatal("auth declaration changed")
		}
	}
	for n := range b {
		b[n] = 'x'
	}
	if i.AgentInfo.Name != "antigravity-acp" || !json.Valid(c.Meta) {
		t.Fatal("input aliases initialization")
	}
}
func TestInitializeUnknownFalseAndAPIOnly(t *testing.T) {
	for _, caps := range []string{`{}`, `{"loadSession":false}`, `{"loadSession":null}`} {
		b := []byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":` + caps + `,"authMethods":[{"id":"gemini-api-key","name":"API key"}]}}`)
		i, err := acp.DecodeInitialize(b, []byte(`1`))
		if err != nil {
			t.Fatal(err)
		}
		v := i.AgentCapabilities.LoadSession
		if caps == `{"loadSession":false}` {
			if v == nil || *v {
				t.Fatal("known false lost")
			}
		} else if v != nil {
			t.Fatal("missing became known")
		}
		if len(i.AuthMethods) != 1 || i.AuthMethods[0].ID != "gemini-api-key" {
			t.Fatal("personal choice invented")
		}
		if len(i.AgentCapabilities.Session.Resume) != 0 {
			t.Fatal("resume invented")
		}
	}
}

// A near-miss decoder based on encoding/json's case-insensitive struct matching
// would project the extension LoadSession as the protocol field loadSession.
func TestCaseDistinctFieldsCannotOverrideDeclarations(t *testing.T) {
	i, err := acp.DecodeInitialize([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false,"LoadSession":true}}}`), []byte(`1`))
	if err == nil && (i.AgentCapabilities.LoadSession == nil || *i.AgentCapabilities.LoadSession) {
		t.Fatal("case-distinct extension overrode known false")
	}
	o, err := acp.DecodePermission([]byte(strings.Replace(permission, `"optionId":"native-yes-42"`, `"optionId":"native-yes-42","OptionID":"injected"`, 1)), "generation-a", "session-a")
	if err == nil {
		_, err = o.PrepareSelected(o.Fence(), "injected")
		if err == nil {
			t.Fatal("case-distinct optionId authorized an extension ID")
		}
	}
}
func TestInitializeRefusals(t *testing.T) {
	for _, tc := range []struct{ name, message, id, code string }{
		{"protocol", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":2}}`, `1`, "unsupported_protocol"},
		{"missing protocol", `{"jsonrpc":"2.0","id":1,"result":{}}`, `1`, "invalid_initialize"},
		{"wrong id", `{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}`, `1`, "unexpected_id"},
		{"rpc", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"private/path/token"}}`, `1`, "rpc_error"},
		{"null rpc code", `{"jsonrpc":"2.0","id":1,"error":{"code":null,"message":"private/path/token"}}`, `1`, "invalid_message"},
		{"both", `{"jsonrpc":"2.0","id":1,"result":{},"error":{}}`, `1`, "invalid_message"},
		{"neither", `{"jsonrpc":"2.0","id":1}`, `1`, "invalid_message"},
		{"rpc version", `{"jsonrpc":"1.0","id":1,"result":{}}`, `1`, "invalid_message"},
		{"trailing", `{"jsonrpc":"2.0","id":1,"result":{}} {}`, `1`, "invalid_message"},
		{"duplicate", `{"jsonrpc":"2.0","id":1,"id":2,"result":{}}`, `1`, "invalid_message"},
		{"bad bool", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":"true"}}}`, `1`, "invalid_initialize"},
		{"bad object", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"list":true}}}}`, `1`, "invalid_initialize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := acp.DecodeInitialize([]byte(tc.message), []byte(tc.id))
			wantCode(t, err, tc.code)
		})
	}
	for _, id := range []string{`9007199254740993`, `"9007199254740993"`, `""`} {
		_, err := acp.DecodeInitialize([]byte(`{"jsonrpc":"2.0","id":`+id+`,"result":{"protocolVersion":1}}`), []byte(id))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := acp.DecodeInitialize([]byte(`{"jsonrpc":"2.0","id":9007199254740993,"result":{"protocolVersion":1}}`), []byte(`9007199254740992`))
	wantCode(t, err, "unexpected_id")
}
