package antigravitysession

import (
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/contract"
)

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

// A native cancelled result ends the current invocation, not proof that every
// external side effect stopped. Persist that outcome without inventing output
// or replacing tools whose own terminal result was already received.
func (c *conversation) cancelActiveTools() error {
	status := "cancelled"
	for id := range c.activeTools {
		delta := &antigravityacp.ToolCallDelta{ToolCallID: id, Status: &status}
		if c.store != nil {
			if err := c.project(antigravityacp.SessionUpdate{Tool: delta}); err != nil {
				return err
			}
		} else if index, ok := c.tools[id]; ok && index >= 0 && index < len(c.entries) && c.entries[index].Activity != nil {
			applyToolDelta(&c.entries[index], delta)
			c.changed()
		}
	}
	return nil
}

func toolStillRunning(e contract.TranscriptEntry) bool {
	if e.Role != "tool" || e.Activity == nil {
		return false
	}
	switch e.Activity.Status {
	case "pending", "in_progress", "inProgress":
		return true
	}
	return false
}

// settleUnreportedTools is called under c.mu once nothing can report for this
// generation any more: its process closed, or the daemon started without it.
// A tool can stay in_progress after end_turn while it runs in the background,
// so a finished turn alone is no reason to settle it. The revision changes so
// viewers reread the rows; the activity clock does not, since nothing new
// happened in the conversation.
func (c *conversation) settleUnreportedTools() error {
	settle := func(e *contract.TranscriptEntry) bool {
		if !toolStillRunning(*e) {
			return false
		}
		e.Activity.Status = ToolStatusUnreported
		return true
	}
	if c.store == nil {
		changed := false
		for i := range c.entries {
			changed = settle(&c.entries[i]) || changed
		}
		if changed {
			c.revision++
		}
		return nil
	}
	rows, err := c.store.db.Query(`SELECT ordinal,tool_id,body FROM entries WHERE id=? AND generation=? AND tool_id<>''
	 AND json_extract(CAST(body AS TEXT),'$.activity.status') IN ('pending','in_progress','inProgress')`, c.row.ID, c.generation)
	if err != nil {
		return refusal("history_unavailable")
	}
	type found struct {
		ordinal int64
		tool    string
		entry   contract.TranscriptEntry
	}
	var open []found
	for rows.Next() {
		var f found
		var raw []byte
		if rows.Scan(&f.ordinal, &f.tool, &raw) != nil || json.Unmarshal(raw, &f.entry) != nil {
			rows.Close()
			return refusal("history_unavailable")
		}
		if settle(&f.entry) {
			open = append(open, f)
		}
	}
	if rows.Close() != nil || rows.Err() != nil {
		return refusal("history_unavailable")
	}
	if len(open) == 0 {
		return nil
	}
	c.revision++
	for _, f := range open {
		if err := c.saveEntry(f.ordinal, f.tool, f.entry); err != nil {
			return err
		}
	}
	return nil
}

// ToolStatusUnreported is the status of a tool whose Gemini process closed
// before the tool reported a result. It says only that no result can arrive;
// it does not claim the command stopped, failed or was cancelled.
const ToolStatusUnreported = "unreported"
