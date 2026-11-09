package antigravityprocess

import (
	"context"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/domain/session"
	"time"
)

// Interrupt requests native turn cancellation without closing the conversation.
// A successful write is not a cancelled stopReason; the prompt owns that reply.
func (c *Client) Interrupt(ctx context.Context, before func() error) error {
	if ctx == nil || ctx.Err() != nil {
		return failure("cancelled", NotWritten)
	}
	c.stateMu.Lock()
	id, permission := c.activePrompt, c.activePermission
	c.stateMu.Unlock()
	if id == "" {
		return nil
	}
	// ACP requires answering outstanding permission requests when cancelling.
	// Release the reader before the cancellation notification, never grant a tool.
	if permission != nil && permission.Active() {
		if err := permission.Cancel(ctx, before); err != nil {
			return err
		}
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if ctx.Err() != nil {
		return failure("cancelled", NotWritten)
	}
	if before != nil {
		if err := before(); err != nil {
			return beforeWriteError{err}
		}
	}
	c.stateMu.Lock()
	if c.terminal != nil {
		c.stateMu.Unlock()
		return failure("closed", NotWritten)
	}
	if c.activePrompt != id {
		c.stateMu.Unlock()
		return nil
	}
	if c.cancelledPrompt == id {
		c.stateMu.Unlock()
		// Repeated interrupt while cancellation is already pending forces immediate teardown.
		c.shutdown(failure("cancelled", Uncertain))
		return nil
	}
	c.cancelledPrompt = id
	c.stateMu.Unlock()
	params := map[string]any{"sessionId": c.SessionID()}
	if c.opts.Assistant == session.AssistantGrok {
		params["_meta"] = map[string]string{"cancelTrigger": "ctrl_c"}
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": params})
	if err := c.writeUnlocked(b); err != nil {
		c.shutdown(failure("write_failed", Uncertain))
		return failure("write_failed", Uncertain)
	}
	c.cancelRequested(id)
	grace := c.opts.StopGraceTimeout
	if grace <= 0 {
		grace = 4 * time.Second
	}
	go func(promptID string, d time.Duration) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-c.done:
			return
		case <-timer.C:
			c.stateMu.Lock()
			stillPending := c.activePrompt == promptID
			c.stateMu.Unlock()
			if stillPending {
				c.shutdown(failure("cancelled", Uncertain))
			}
		}
	}(id, grace)
	return nil
}
