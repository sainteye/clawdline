package transcript

import (
	"encoding/json"
	"strings"
)

// Turn is one thing that was said, flattened to what a reader needs.
//
// The two assistants write very different records — Claude nests a message with
// typed content blocks, Codex writes response items and events — so this is
// deliberately not their union. It is the part both have and a person reads:
// who spoke, when, and the text. Tool calls are named rather than expanded,
// because a transcript panel that reproduced every tool payload would be the
// log file again.
type Turn struct {
	Role string `json:"role"`
	At   string `json:"at"`
	Text string `json:"text"`
	// Tool is set when this turn was a tool use rather than prose, and carries
	// the tool's name. Text is then a short description, never the payload.
	Tool string `json:"tool,omitempty"`
}

// ReadClaudeTurns returns the most recent turns of a Claude conversation.
//
// It reads backwards, because the answer to "what just happened" is at the end
// and these files reach a hundred megabytes.
func ReadClaudeTurns(path string, n int) ([]Turn, error) {
	lines, err := LastLines(path, n*4, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Turn, 0, n)
	for _, line := range lines {
		var rec struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if rec.Type != "assistant" && rec.Type != "user" {
			continue
		}
		text, tool := claudeContent(rec.Message.Content)
		if text == "" && tool == "" {
			continue
		}
		role := rec.Message.Role
		if role == "" {
			role = rec.Type
		}
		out = append(out, Turn{Role: role, At: rec.Timestamp, Text: text, Tool: tool})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// claudeContent flattens one message's content blocks.
//
// Content is a string on some messages and a list of typed blocks on others, so
// both are accepted rather than one being treated as malformed.
func claudeContent(raw json.RawMessage) (text, tool string) {
	if len(raw) == 0 {
		return "", ""
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return strings.TrimSpace(asString), ""
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Thought string          `json:"thinking"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", ""
	}
	parts := []string{}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, t)
			}
		case "tool_use":
			tool = b.Name
		case "tool_result":
			tool = "result"
		}
	}
	return strings.Join(parts, "\n\n"), tool
}

// ReadCodexTurns returns the most recent turns of a Codex thread.
func ReadCodexTurns(path string, n int) ([]Turn, error) {
	lines, err := LastLines(path, n*8, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Turn, 0, n)
	for _, line := range lines {
		var rec struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Name    string `json:"name"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "response_item" {
			continue
		}
		p := rec.Payload
		switch p.Type {
		case "message":
			parts := []string{}
			for _, c := range p.Content {
				if t := strings.TrimSpace(c.Text); t != "" {
					parts = append(parts, t)
				}
			}
			if len(parts) == 0 {
				continue
			}
			out = append(out, Turn{Role: p.Role, At: rec.Timestamp, Text: strings.Join(parts, "\n\n")})
		case "function_call", "local_shell_call", "custom_tool_call":
			name := p.Name
			if name == "" {
				name = p.Type
			}
			out = append(out, Turn{Role: "assistant", At: rec.Timestamp, Tool: name})
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
