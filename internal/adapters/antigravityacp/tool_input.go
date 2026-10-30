package antigravityacp

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

func toolInput(raw any) *ToolInput {
	// Historical replay sends JSON-encoded arguments, while live updates send
	// an object. Both describe the same input; neither enables a filesystem read.
	if text, ok := raw.(string); ok {
		d := json.NewDecoder(bytes.NewBufferString(text))
		d.UseNumber()
		value, err := updateValue(d)
		if err != nil {
			return nil
		}
		if _, err := d.Token(); err != io.EOF {
			return nil
		}
		raw = value
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	str := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := m[key].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	out := &ToolInput{Path: str("AbsolutePath", "TargetFile"), Command: str("CommandLine", "command_line", "command"), Query: str("SearchTerm", "Query"), Summary: str("toolSummary", "toolAction")}
	for key, dst := range map[string]**int64{"StartLine": &out.StartLine, "EndLine": &out.EndLine} {
		if n, ok := updateInteger(m[key]); ok && n > 0 {
			*dst = &n
		}
	}
	if list, ok := m["Subagents"].([]any); ok {
		for _, raw := range list {
			if item, ok := raw.(map[string]any); ok {
				name, _ := item["Role"].(string)
				model, _ := item["Model"].(string)
				if name != "" {
					out.Delegates = append(out.Delegates, ToolDelegate{Name: name, Model: model})
				}
			}
		}
	}
	return out
}

// Native exec IDs explicitly namespace steps by trajectory. Unscoped call_N
// IDs provide no owner evidence and must never be assigned to a nearby agent.
func toolAgentID(id, session string) string {
	prefix, ordinal, ok := strings.Cut(id, ":")
	if !ok || prefix == session {
		return ""
	}
	if _, err := strconv.ParseUint(ordinal, 10, 64); err != nil {
		return ""
	}
	if len(prefix) == 36 && (prefix[8] != '-' || prefix[13] != '-' || prefix[18] != '-' || prefix[23] != '-') {
		return ""
	}
	compact := strings.ReplaceAll(prefix, "-", "")
	if compact == strings.ReplaceAll(session, "-", "") {
		return ""
	}
	if len(compact) != 32 || len(prefix) != 32 && len(prefix) != 36 {
		return ""
	}
	for _, c := range compact {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return prefix
}
