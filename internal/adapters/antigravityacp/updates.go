package antigravityacp

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// UpdateDecodeError exposes stable local codes, never opaque provider data.
// It is separate from the initialization codec so this decoder also stands alone.
type UpdateDecodeError struct{ Code string }

func (e *UpdateDecodeError) Error() string { return "antigravity_acp_" + e.Code }

// SessionUpdate is one notification, in input order. The owner appends Text for
// message/thought chunks separately; the decoder neither coalesces nor replays.
// Ignored updates are recognizable but must not become chat or provider errors.
// No raw input, output, extension metadata or billing counters are retained.
type SessionUpdate struct {
	SessionID string
	Kind      string
	Ignored   bool
	Text      string
	Tool      *ToolCallDelta
	Context   *ContextUsage
}

// ContextUsage is current context occupancy, not incremental or billed tokens.
type ContextUsage struct{ Used, Size int64 }

// ToolCallDelta preserves absent/null fields as nil. A present empty title or
// empty content/locations slice is an explicit replacement. Callers merge only
// non-nil fields, keyed by ToolCallID, and must not erase earlier partial data.
type ToolCallDelta struct {
	ToolCallID string
	Title      *string
	Kind       *string
	Status     *string
	Content    *[]ToolUpdateContent
	Locations  *[]ToolUpdateLocation
}

type ToolUpdateLocation struct {
	Path string
	Line *int64
}

// ToolUpdateContent retains renderable ACP content, diffs and terminal links.
// Unknown extensions are omitted. Unsupported content is represented only by its
// type and Ignored flag, never flattened into assistant text.
type ToolUpdateContent struct {
	Type       string
	Ignored    bool
	Content    *UpdateContent
	Path       *string
	OldText    *string
	NewText    *string
	TerminalID *string
}

type UpdateContent struct {
	Type        string
	Ignored     bool
	Text        *string
	Data        *string
	MimeType    *string
	URI         *string
	Name        *string
	Description *string
	Resource    *UpdateResource
}

type UpdateResource struct {
	URI      string
	MimeType *string
	Text     *string
	Blob     *string
}

// DecodeSessionUpdate accepts a complete, already transport-bounded JSON-RPC
// notification for exactly expectedSession. It owns no transport or mutable
// shared state, starts no goroutines, and returns independently owned values.
// Optional unsupported variants return Ignored; unknown variants return a typed
// unsupported_update error. Wrong-session notifications are always errors,
// including otherwise ignored variants. Error results contain no partial data.
func DecodeSessionUpdate(message []byte, expectedSession string) (SessionUpdate, error) {
	fail := func(code string) (SessionUpdate, error) {
		return SessionUpdate{}, &UpdateDecodeError{Code: code}
	}
	if expectedSession == "" {
		return fail("invalid_owner")
	}
	if !utf8.Valid(message) || !json.Valid(message) || !updateEscapesValid(message) {
		return fail("invalid_message")
	}
	d := json.NewDecoder(bytes.NewReader(message))
	d.UseNumber()
	value, err := updateValue(d)
	if err != nil {
		return fail("invalid_message")
	}
	if _, err := d.Token(); err != io.EOF {
		return fail("invalid_message")
	}
	m, ok := value.(map[string]any)
	if !ok || m["jsonrpc"] != "2.0" || m["method"] != "session/update" {
		return fail("invalid_message")
	}
	for _, key := range []string{"id", "result", "error"} {
		if _, exists := m[key]; exists {
			return fail("invalid_message")
		}
	}
	p, ok := m["params"].(map[string]any)
	if !ok {
		return fail("invalid_update")
	}
	session, ok := p["sessionId"].(string)
	if !ok || session == "" {
		return fail("invalid_update")
	}
	if session != expectedSession {
		return fail("wrong_session")
	}
	u, ok := p["update"].(map[string]any)
	if !ok {
		return fail("invalid_update")
	}
	kind, ok := u["sessionUpdate"].(string)
	if !ok || kind == "" {
		return fail("invalid_update")
	}
	out := SessionUpdate{SessionID: session, Kind: kind}
	switch kind {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk":
		content, valid := updateContent(u["content"])
		if !valid {
			return fail("invalid_update")
		}
		if content.Type == "text" {
			out.Text = *content.Text
		} else {
			out.Ignored = true
		}
	case "tool_call", "tool_call_update":
		tool, valid := updateTool(u, kind == "tool_call")
		if !valid {
			return fail("invalid_update")
		}
		out.Tool = tool
	case "usage_update":
		used, usedOK := updateInteger(u["used"])
		size, sizeOK := updateInteger(u["size"])
		if !usedOK || !sizeOK || used < 0 || size < 0 {
			return fail("invalid_update")
		}
		out.Context = &ContextUsage{Used: used, Size: size}
	case "plan", "available_commands_update", "current_mode_update", "config_option_update", "session_info_update":
		// The variant is known, but its optional payload is deliberately not
		// normalized. In particular, history user text is not assistant output.
		out.Ignored = true
	default:
		return fail("unsupported_update")
	}
	return out, nil
}

func updateTool(u map[string]any, initial bool) (*ToolCallDelta, bool) {
	id, ok := u["toolCallId"].(string)
	if !ok || id == "" {
		return nil, false
	}
	out := &ToolCallDelta{ToolCallID: id}
	for _, field := range []struct {
		key string
		dst **string
	}{{"title", &out.Title}, {"kind", &out.Kind}, {"status", &out.Status}} {
		if !updateOptionalString(u, field.key, field.dst) {
			return nil, false
		}
	}
	if initial && out.Title == nil {
		return nil, false
	}
	if raw := u["content"]; raw != nil {
		items, ok := raw.([]any)
		if !ok {
			return nil, false
		}
		content := make([]ToolUpdateContent, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			typ, ok := m["type"].(string)
			if !ok || typ == "" {
				return nil, false
			}
			c := ToolUpdateContent{Type: typ}
			switch typ {
			case "content":
				var valid bool
				c.Content, valid = updateContent(m["content"])
				if !valid {
					return nil, false
				}
			case "diff":
				if !updateOptionalString(m, "path", &c.Path) || c.Path == nil ||
					!updateOptionalString(m, "oldText", &c.OldText) ||
					!updateOptionalString(m, "newText", &c.NewText) || c.NewText == nil {
					return nil, false
				}
			case "terminal":
				if !updateOptionalString(m, "terminalId", &c.TerminalID) || c.TerminalID == nil || *c.TerminalID == "" {
					return nil, false
				}
			default:
				c.Ignored = true
			}
			content = append(content, c)
		}
		out.Content = &content
	}
	if raw := u["locations"]; raw != nil {
		items, ok := raw.([]any)
		if !ok {
			return nil, false
		}
		locations := make([]ToolUpdateLocation, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			path, ok := m["path"].(string)
			if !ok || path == "" {
				return nil, false
			}
			location := ToolUpdateLocation{Path: path}
			if m["line"] != nil {
				line, valid := updateInteger(m["line"])
				if !valid || line < 0 {
					return nil, false
				}
				location.Line = &line
			}
			locations = append(locations, location)
		}
		out.Locations = &locations
	}
	return out, true
}

func updateContent(raw any) (*UpdateContent, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	typ, ok := m["type"].(string)
	if !ok || typ == "" {
		return nil, false
	}
	c := &UpdateContent{Type: typ}
	required := func(key string, dst **string) bool {
		return updateOptionalString(m, key, dst) && *dst != nil
	}
	switch typ {
	case "text":
		return c, required("text", &c.Text)
	case "image", "audio":
		return c, required("data", &c.Data) && required("mimeType", &c.MimeType)
	case "resource_link":
		return c, required("uri", &c.URI) && required("name", &c.Name) &&
			updateOptionalString(m, "mimeType", &c.MimeType) && updateOptionalString(m, "description", &c.Description)
	case "resource":
		r, ok := m["resource"].(map[string]any)
		if !ok {
			return nil, false
		}
		uri, ok := r["uri"].(string)
		if !ok || uri == "" {
			return nil, false
		}
		resource := &UpdateResource{URI: uri}
		if !updateOptionalString(r, "mimeType", &resource.MimeType) ||
			!updateOptionalString(r, "text", &resource.Text) || !updateOptionalString(r, "blob", &resource.Blob) ||
			(resource.Text == nil) == (resource.Blob == nil) {
			return nil, false
		}
		c.Resource = resource
	default:
		c.Ignored = true
	}
	return c, true
}

func updateOptionalString(m map[string]any, key string, dst **string) bool {
	if m[key] == nil {
		return true
	}
	s, ok := m[key].(string)
	if ok {
		*dst = &s
	}
	return ok
}

func updateInteger(value any) (int64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return i, err == nil
}

// Decode maps explicitly to avoid encoding/json's case-insensitive struct field
// matching, and reject duplicate keys at every depth rather than last-wins.
func updateValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if _, exists := m[name]; !ok || exists {
				return nil, &UpdateDecodeError{Code: "invalid_message"}
			}
			m[name], err = updateValue(d)
			if err != nil {
				return nil, err
			}
		}
		_, err := d.Token()
		return m, err
	case json.Delim('['):
		items := []any{}
		for d.More() {
			item, err := updateValue(d)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		_, err := d.Token()
		return items, err
	default:
		return token, nil
	}
}

// json.Valid has already checked escape syntax. Refuse unpaired UTF-16 escapes
// instead of silently turning distinct provider identities/text into U+FFFD.
func updateEscapesValid(raw []byte) bool {
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
