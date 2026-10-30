package antigravitysession

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ClaimAutomaticName persists before a potentially paid operation. A failed or
// interrupted attempt is never repeated by another turn, reload or restart.
func (h *Host) ClaimAutomaticName(ctx context.Context, row session.Session) (bool, error) {
	c, err := h.lookup(row)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.hidden || c.store == nil || c.storeErr != nil || c.autoNameAttempted || c.row.Rungs.Thread != "" || c.row.CustomTitle != "" {
		return false, nil
	}
	c.autoNameAttempted = true
	if err := c.saveState(); err != nil {
		return false, err
	}
	return true, nil
}

// SaveAutomaticName fills only the automatic rung. Manual and task names keep
// their higher priority; neither this method nor replay changes those stores.
func (h *Host) SaveAutomaticName(ctx context.Context, row session.Session, title string) error {
	c, err := h.lookup(row)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > int(capacity.Default(capacity.SessionTitleCharacters)) {
		return refusal("invalid_title")
	}
	if c.hidden || !c.autoNameAttempted || c.row.Rungs.Thread != "" || c.storeErr != nil {
		return refusal("identity_changed")
	}
	old := c.row.Rungs.Thread
	c.row.Rungs.Thread = title
	if err := c.saveState(); err != nil {
		c.row.Rungs.Thread = old
		return err
	}
	return nil
}
