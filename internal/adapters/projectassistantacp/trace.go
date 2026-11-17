package projectassistantacp

import (
	"encoding/json"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Native steps follow Codync's updates.rs and tool_entry.rs at 9b471897:
// consecutive text/thought chunks share a segment; each tool keeps its own
// title, status and latest nonempty output/diffs. Raw tool inputs are not copied.
type nativeToolTrace struct {
	Title  string
	Status string
	Output string
	Diffs  []pa.PermissionDiff
}

// The supplied codex-acp 2.6 adapter streams command output through these
// existing ACP v1 metadata fields (its ToolCallRenderer.outputChunk).
type nativeTerminalOutput struct {
	Data       string `json:"data"`
	TerminalID string `json:"terminal_id"`
}

type nativeTraceUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	Content       json.RawMessage `json:"content"`
	Meta          struct {
		Delta  *nativeTerminalOutput `json:"terminal_output_delta"`
		Output *nativeTerminalOutput `json:"terminal_output"`
	} `json:"_meta"`
}

func (c *Client) traceUpdate(raw json.RawMessage) error {
	var u nativeTraceUpdate
	if json.Unmarshal(raw, &u) != nil {
		return refusal(pa.Invalid, "update")
	}
	category := u.SessionUpdate
	e := c.event(c.eventID())
	c.mu.Lock()
	var trace pa.Trace
	switch category {
	case "agent_message_chunk", "agent_thought_chunk":
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Content, &block) != nil || block.Type != "text" || block.Text == "" {
			c.mu.Unlock()
			return nil
		}
		if c.traceSegment.Category != category || c.traceSegment.ID == "" {
			c.traceSegment = pa.Trace{ID: "segment-" + e.ID, TurnID: e.TurnID, Category: category}
		}
		trace = c.traceSegment
		trace.Text = block.Text
	case "tool_call", "tool_call_update":
		if category == "tool_call" {
			c.traceSegment = pa.Trace{}
		}
		chunk := u.Meta.Delta
		if chunk == nil {
			chunk = u.Meta.Output
		}
		if chunk != nil && chunk.TerminalID != "" {
			if u.ToolCallID != "" && u.ToolCallID != chunk.TerminalID {
				c.mu.Unlock()
				return refusal(pa.Invalid, "tool_terminal_id")
			}
			u.ToolCallID = chunk.TerminalID
		}
		if u.ToolCallID == "" {
			u.ToolCallID = "event-" + e.ID
		}
		if c.traceTools == nil {
			c.traceTools = make(map[string]nativeToolTrace)
		}
		t := c.traceTools[u.ToolCallID]
		if u.Title != "" {
			t.Title = u.Title
		}
		if u.Status != "" {
			t.Status = u.Status
		}
		var blocks []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Path    string `json:"path"`
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if len(u.Content) > 0 && string(u.Content) != "null" && json.Unmarshal(u.Content, &blocks) != nil {
			c.mu.Unlock()
			return refusal(pa.Invalid, "tool_content")
		}
		var output []string
		var diffs []pa.PermissionDiff
		for _, b := range blocks {
			switch {
			case b.Type == "diff":
				diffs = append(diffs, pa.PermissionDiff{Path: b.Path, OldText: b.OldText, NewText: b.NewText})
			case b.Type == "text" && b.Text != "":
				output = append(output, b.Text)
			case b.Type == "content" && b.Content.Type == "text" && b.Content.Text != "":
				output = append(output, b.Content.Text)
			}
		}
		if len(output) > 0 {
			t.Output = strings.Join(output, "\n")
		}
		if len(diffs) > 0 {
			t.Diffs = diffs
		}
		if chunk != nil {
			t.Output += chunk.Data
		}
		c.traceTools[u.ToolCallID] = t
		parts := []string{t.Title}
		if t.Status != "" {
			parts = append(parts, t.Status)
		}
		if t.Output != "" {
			parts = append(parts, t.Output)
		}
		for _, diff := range t.Diffs {
			parts = append(parts, diff.Path+"\n---\n"+diff.OldText+"\n+++\n"+diff.NewText)
		}
		trace = pa.Trace{ID: "tool-" + u.ToolCallID, TurnID: e.TurnID, Category: "tool_call", Text: strings.TrimSpace(strings.Join(parts, "\n")), Complete: t.Status == "completed" || t.Status == "failed"}
	default:
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	e.Trace = &trace
	return c.sink(c.ctx, e)
}
