package antigravitysession

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func (h *Host) InterruptProtocol(ctx context.Context, s session.Session, before func() error) error {
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
	client := c.client
	if c.hidden {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Unlock()
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	if client == nil {
		return nil
	}
	return client.Interrupt(ctx, func() error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.hidden || c.client != client || h.ctx.Err() != nil {
			return refusal("closed")
		}
		return nil
	})
}
