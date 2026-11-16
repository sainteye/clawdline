package projectassistanthost

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (w NativeWork) ListBots(ctx context.Context, q pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
	if w.listBots == nil {
		return pa.BotDirectoryPage{}, failure(pa.Disabled, "collaboration_tools")
	}
	return w.listBots(ctx, q)
}
func (w NativeWork) MessageBot(ctx context.Context, q pa.BotMessageRequest) (pa.Exchange, error) {
	if w.messageBot == nil {
		return pa.Exchange{}, failure(pa.Disabled, "collaboration_tools")
	}
	return w.messageBot(ctx, q)
}

func (h *Host) CollaborationUsage(ctx context.Context) (int64, int64, error) {
	h.mu.Lock()
	s := h.store
	h.mu.Unlock()
	if s == nil {
		return 0, 0, failure(pa.Disabled, "collaboration_store")
	}
	return s.CollaborationUsage(ctx, h.o.MachineID)
}
