// Package antigravityacp decodes already bounded ACP v1 messages. It owns no
// transport, authentication, policy authority, durable receipt or send effect.
package antigravityacp

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CodecError contains stable codes only, never provider text or filesystem paths.
type CodecError struct {
	Code    string
	RPCCode int64 // meaningful only when Code is rpc_error
}

func (e *CodecError) Error() string { return "antigravity_acp_" + e.Code }
func refusal(code string) error     { return &CodecError{Code: code} }

// Initialization contains declarations, not evidence of authentication or grants.
// Every byte slice and pointer is owned by this result, independent of input.
type Initialization struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	AgentInfo         *AgentInfo      `json:"agentInfo"`
	AgentCapabilities Capabilities    `json:"agentCapabilities"`
	AuthMethods       []AuthMethod    `json:"authMethods"`
	Meta              json.RawMessage `json:"_meta"`
}
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}
type AuthMethod struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Type *string         `json:"type"`
	Meta json.RawMessage `json:"_meta"`
}

// A nil boolean means undeclared; a non-nil false is an explicit declaration.
// Object capabilities remain raw declarations; only non-null objects advertise
// support. Extension metadata must never be treated as current authority.
type Capabilities struct {
	LoadSession *bool `json:"loadSession"`
	Prompt      struct {
		Image           *bool `json:"image"`
		Audio           *bool `json:"audio"`
		EmbeddedContext *bool `json:"embeddedContext"`
	} `json:"promptCapabilities"`
	MCP struct {
		HTTP *bool `json:"http"`
		SSE  *bool `json:"sse"`
	} `json:"mcpCapabilities"`
	Session struct {
		List   json.RawMessage `json:"list"`
		Resume json.RawMessage `json:"resume"`
	} `json:"sessionCapabilities"`
	Auth struct {
		Logout json.RawMessage `json:"logout"`
	} `json:"auth"`
	Meta json.RawMessage `json:"_meta"`
}

// DecodeInitialize accepts a complete response and the owner's expected JSON
// request ID token. String and integer IDs are distinct; numbers never use floats.
func DecodeInitialize(message, expectedID []byte) (Initialization, error) {
	var out Initialization
	m, err := messageObject(message)
	if err != nil {
		return out, err
	}
	if !stringEquals(m["jsonrpc"], "2.0") {
		return out, refusal("invalid_message")
	}
	if _, ok := m["method"]; ok {
		return out, refusal("invalid_message")
	}
	if _, ok := m["params"]; ok {
		return out, refusal("invalid_message")
	}
	id, err := requestID(m["id"])
	if err != nil {
		return out, err
	}
	expected, err := requestID(expectedID)
	if err != nil {
		return out, err
	}
	if id != expected {
		return out, refusal("unexpected_id")
	}
	result, hasResult := m["result"]
	rpcError, hasError := m["error"]
	if hasResult == hasError {
		return out, refusal("invalid_message")
	}
	if hasError {
		e, err := object(rpcError)
		if err != nil {
			return out, refusal("invalid_message")
		}
		var code int64
		var message string
		if _, err := requestID(e["code"]); err != nil || len(e["code"]) == 0 || e["code"][0] == '"' || json.Unmarshal(e["code"], &code) != nil || !isString(e["message"], &message) {
			return out, refusal("invalid_message")
		}
		return out, &CodecError{Code: "rpc_error", RPCCode: code}
	}
	r, err := object(result)
	if err != nil {
		return out, refusal("invalid_initialize")
	}
	// encoding/json matches struct fields without regard to case. Refuse those
	// aliases at protocol fields so extensions cannot override declarations.
	if !exactFields(result, "protocolVersion", "agentInfo", "agentCapabilities", "authMethods", "_meta") ||
		!exactFields(r["agentInfo"], "name", "title", "version") ||
		!exactFields(r["agentCapabilities"], "loadSession", "promptCapabilities", "mcpCapabilities", "sessionCapabilities", "auth", "_meta") {
		return out, refusal("invalid_initialize")
	}
	if caps, e := object(r["agentCapabilities"]); e == nil {
		if !exactFields(caps["promptCapabilities"], "image", "audio", "embeddedContext") ||
			!exactFields(caps["mcpCapabilities"], "http", "sse") ||
			!exactFields(caps["sessionCapabilities"], "list", "resume") ||
			!exactFields(caps["auth"], "logout") {
			return out, refusal("invalid_initialize")
		}
	}
	var methods []json.RawMessage
	if raw, ok := r["authMethods"]; ok && json.Unmarshal(raw, &methods) != nil {
		return out, refusal("invalid_initialize")
	}
	for _, raw := range methods {
		if !exactFields(raw, "id", "name", "type", "_meta") {
			return out, refusal("invalid_initialize")
		}
	}
	var protocol int
	if len(r["protocolVersion"]) == 0 || json.Unmarshal(r["protocolVersion"], &protocol) != nil {
		return out, refusal("invalid_initialize")
	}
	if protocol != 1 {
		return out, refusal("unsupported_protocol")
	}
	if json.Unmarshal(result, &out) != nil {
		return Initialization{}, refusal("invalid_initialize")
	}
	if out.AgentInfo != nil && (out.AgentInfo.Name == "" || out.AgentInfo.Version == "") {
		return Initialization{}, refusal("invalid_initialize")
	}
	seen := map[string]bool{}
	for _, a := range out.AuthMethods {
		if a.ID == "" || a.Name == "" || seen[a.ID] {
			return Initialization{}, refusal("invalid_initialize")
		}
		seen[a.ID] = true
		if len(a.Meta) != 0 && !bytes.Equal(a.Meta, []byte("null")) {
			if _, err := object(a.Meta); err != nil {
				return Initialization{}, refusal("invalid_initialize")
			}
		}
	}
	for _, raw := range []json.RawMessage{out.Meta, out.AgentCapabilities.Meta, out.AgentCapabilities.Session.List, out.AgentCapabilities.Session.Resume, out.AgentCapabilities.Auth.Logout} {
		if len(raw) != 0 && !bytes.Equal(raw, []byte("null")) {
			if _, err := object(raw); err != nil {
				return Initialization{}, refusal("invalid_initialize")
			}
		}
	}
	return out, nil
}

func stringEquals(raw []byte, expected string) bool {
	var s string
	return isString(raw, &s) && s == expected
}
func isString(raw []byte, out *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, out) == nil
}

// requestID returns a canonical string identity or an exact integer token.
// Fractions/exponents are deliberately refused rather than rounded/coerced.
func requestID(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if !json.Valid(raw) || !utf8.Valid(raw) || !pairedSurrogates(raw) {
		return "", refusal("invalid_id")
	}
	var s string
	if isString(raw, &s) {
		b, _ := json.Marshal(s)
		return string(b), nil
	}
	start := 0
	if len(raw) > 0 && raw[0] == '-' {
		start = 1
	}
	if start == len(raw) {
		return "", refusal("invalid_id")
	}
	for _, b := range raw[start:] {
		if b < '0' || b > '9' {
			return "", refusal("invalid_id")
		}
	}
	return string(raw), nil
}
func object(raw []byte) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, refusal("invalid_message")
	}
	return out, nil
}
func messageObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) || !pairedSurrogates(raw) {
		return nil, refusal("invalid_message")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueValue(d); err != nil {
		return nil, refusal("invalid_message")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, refusal("invalid_message")
	}
	return object(raw)
}

// Optional objects can be absent/null. Unknown extensions are accepted, but
// case variants of protocol field names are ambiguous and refused.
func exactFields(raw []byte, fields ...string) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	m, err := object(raw)
	if err != nil {
		return false
	}
	for key := range m {
		for _, field := range fields {
			if key != field && strings.EqualFold(key, field) {
				return false
			}
		}
	}
	return true
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Refuse that lossy
// identity conversion while accepting actual U+FFFD and valid surrogate pairs.
// json.Valid must already have checked syntax before this scan.
func pairedSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		n, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, _ := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

// Reject duplicate keys at every depth, including escaped spelling aliases.
func uniqueValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return refusal("invalid_message")
			}
			seen[key] = true
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
	} else if delim == '[' {
		for d.More() {
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
	} else {
		return refusal("invalid_message")
	}
	_, err = d.Token()
	return err
}
