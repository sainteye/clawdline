package transcript

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// SetAntigravityBindings installs a local bridge source without maintaining a
// second transcript resolver or guessing from cwd/most-recent files.
func (h *Host) SetAntigravityBindings(source ports.AntigravityBindings) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.antigravity = source
}

func (h *Host) antigravityBinding(ctx context.Context, s session.Session) (ports.AntigravityBinding, session.Binding, bool) {
	h.mu.RLock()
	source := h.antigravity
	h.mu.RUnlock()
	if source == nil {
		return ports.AntigravityBinding{}, session.BindingNoRecord, false
	}
	b, err := source.AntigravityBinding(ctx, s)
	if err != nil || !b.Verified {
		return ports.AntigravityBinding{}, session.BindingUnreadable, false
	}
	ok := b.ConversationID != "" && b.TranscriptPath != ""
	return b, session.BindingNoRecord, ok
}
