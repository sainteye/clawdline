package antigravitysession

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (c *conversation) requestPermission(ctx context.Context, p *antigravityprocess.PermissionRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil || c.hidden || c.staging || c.row.ConversationID != p.Offer.Fence().SessionID {
		return refusal("invalid_permission_owner")
	}
	c.permission = p
	return nil
}

func permissionMenu(p *antigravityprocess.PermissionRequest) *session.Menu {
	var tool struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(p.Offer.ToolCall(), &tool)
	// The native tool title and labels are preserved, including their language.
	m := &session.Menu{Question: tool.Title, Revision: p.Revision, Numbered: true}
	for i, option := range p.Offer.Options() {
		m.Options = append(m.Options, session.MenuOption{Number: i + 1, Label: option.Name})
	}
	return m
}

// SelectProtocol answers through the ordinary Session key action. The numeric
// key maps only to an offered native ID; it never infers an allow/deny kind.
func (h *Host) SelectProtocol(ctx context.Context, s session.Session, digit int, expect string, before func() error) error {
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	c.mu.Lock()
	p := c.permission
	if c.hidden || p == nil || !p.Active() || expect == "" || session.MenuFingerprint(*permissionMenu(p)) != expect {
		c.mu.Unlock()
		return refusal("stale_offer")
	}
	options := p.Offer.Options()
	if digit < 1 || digit > 9 || digit > len(options) {
		c.mu.Unlock()
		return refusal("unknown_option")
	}
	option := options[digit-1].OptionID
	c.mu.Unlock()
	return p.Select(ctx, option, func() error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.hidden || c.permission != p || h.ctx.Err() != nil {
			return refusal("stale_offer")
		}
		return nil
	})
}
