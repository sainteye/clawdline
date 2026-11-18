package projectassistanthost

import (
	"context"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NativeContext reads only the currently owned native session. In particular,
// runtime() is not a read path: even an idle read must not initialize execution.
// No reading is persisted or exposed as permission to run maintenance.
func (h *Host) NativeContext(ctx context.Context, fence pa.Fence) (app.NativeContextState, error) {
	unknown := app.NativeContextState{Fence: fence}
	s, err := h.Service()
	if err != nil {
		return unknown, err
	}
	readCheck := func() error {
		bot, conversation, _, err := s.Get(ctx, fence.Key)
		if err != nil {
			return err
		}
		current := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
		return pa.CheckSendFence(fence, current)
	}
	if err = readCheck(); err != nil {
		return unknown, err
	}
	h.mu.Lock()
	r := h.engine
	live := h.enabled && !h.closed && !h.cleanupPending
	h.mu.Unlock()
	if !live || r == nil {
		return unknown, nil
	}
	// Recheck human read authority inside the borrowed actor's lifetime as
	// well; a post-read database call outside it could outlive native cleanup.
	state, err := r.NativeContext(ctx, fence, readCheck)
	if err != nil {
		return unknown, err
	}
	h.mu.Lock()
	live = h.enabled && !h.closed && !h.cleanupPending && h.engine == r
	h.mu.Unlock()
	if !live {
		return unknown, nil
	}
	return state, nil
}
