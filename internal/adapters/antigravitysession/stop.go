package antigravitysession

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Stop ends only this owned worker's process, including an active prompt or
// metadata call. The history remains available for an explicit later resume.
func (h *Host) Stop(ctx context.Context, row session.Session) error {
	if ctx == nil || ctx.Err() != nil {
		return refusal("cancelled")
	}
	c, err := h.lookup(row)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
	if c.stopping {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("busy")
	}
	c.stopping = true
	c.hidden = true
	c.permission = nil
	if c.busy {
		c.uncertain = true
		c.row.State = session.StateUnknown
	}
	client := c.client
	err = c.saveState()
	h.jobs.Add(1)
	c.mu.Unlock()
	h.mu.Unlock()
	defer h.jobs.Done()
	// Never hold a conversation lock while joining the client's reader.
	if client != nil {
		err = errors.Join(err, client.Close())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = nil
	c.row.PID = 0
	c.stopping = false
	return errors.Join(err, c.saveState())
}
