package antigravitysession

import "github.com/sainteye/clawdline/internal/adapters/antigravityacp"

// trackActiveTool is called under c.mu, before either durable or memory
// projection. Transcript eviction must not turn a live tool into inactivity.
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
		if _, ok := c.activeTools[d.ToolCallID]; ok {
			return nil
		}
		if len(d.ToolCallID) > MaxActiveToolBytes-c.activeToolBytes {
			return refusal("view_too_large")
		}
		if c.activeTools == nil {
			c.activeTools = make(map[string]struct{})
		}
		c.activeTools[d.ToolCallID] = struct{}{}
		c.activeToolBytes += len(d.ToolCallID)
	}
	return nil
}

// modelMovedOn releases every tool still marked active when the model writes
// again. The model generates its next reply only after it has the results of
// the tools it called, so a tool it never reported complete is no longer
// running for this turn. On 2026-11-07 three view_file calls stayed
// in_progress after their replies arrived, the model wrote on and fell silent,
// and the tools held the inactivity watchdog off with nothing running.
func (c *conversation) modelMovedOn() {
	if len(c.activeTools) == 0 {
		return
	}
	c.activeTools = nil
	c.activeToolBytes = 0
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
