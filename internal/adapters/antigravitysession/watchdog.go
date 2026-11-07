package antigravitysession

import "github.com/sainteye/clawdline/internal/adapters/antigravityacp"

// trackActiveTool is called under c.mu, before either durable or memory
// projection. Transcript eviction must not turn a live tool into inactivity.
// Each active ID keeps the native kind it was announced with.
func (c *conversation) trackActiveTool(d *antigravityacp.ToolCallDelta, initial bool) error {
	if d.Status == nil && !initial {
		return nil
	}
	status := "pending"
	if d.Status != nil {
		status = *d.Status
	}
	switch status {
	case "completed", "failed":
		if _, ok := c.activeTools[d.ToolCallID]; ok {
			delete(c.activeTools, d.ToolCallID)
			c.activeToolBytes -= len(d.ToolCallID)
		}
	case "pending", "in_progress":
		if kind, ok := c.activeTools[d.ToolCallID]; ok {
			if kind == "" && d.Kind != nil {
				c.activeTools[d.ToolCallID] = *d.Kind
			}
			return nil
		}
		if len(d.ToolCallID) > MaxActiveToolBytes-c.activeToolBytes {
			return refusal("view_too_large")
		}
		if c.activeTools == nil {
			c.activeTools = make(map[string]string)
		}
		kind := ""
		if d.Kind != nil {
			kind = *d.Kind
		}
		c.activeTools[d.ToolCallID] = kind
		c.activeToolBytes += len(d.ToolCallID)
	}
	return nil
}

// synchronousToolKinds are ACP tool kinds the runtime finishes before the
// model writes again. Execute, fetch, other and unknown kinds can run in the
// background while the model keeps writing: on 2026-11-07 a `task wait
// --timeout 9m` command stayed in_progress for seven and a half minutes while
// the model wrote that it was waiting, then completed normally.
var synchronousToolKinds = map[string]bool{"read": true, "edit": true, "delete": true, "move": true, "search": true, "think": true}

// modelMovedOn releases synchronous tools still marked active when the model
// writes again: a result the model has already used is no longer running. On
// 2026-11-07 three view_file (read) calls stayed in_progress after their
// results arrived, the model wrote on and fell silent, and they held the
// inactivity watchdog off with nothing running.
func (c *conversation) modelMovedOn() {
	for id, kind := range c.activeTools {
		if synchronousToolKinds[kind] {
			delete(c.activeTools, id)
			c.activeToolBytes -= len(id)
		}
	}
}

// ActiveToolBytes reports the largest per-turn tracker, matching its per-turn
// capacity rather than charging unrelated Sessions to one Session's budget.
func (h *Host) ActiveToolBytes() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	largest := 0
	for _, c := range h.rows {
		c.mu.Lock()
		largest = max(largest, c.activeToolBytes)
		c.mu.Unlock()
	}
	return largest
}
