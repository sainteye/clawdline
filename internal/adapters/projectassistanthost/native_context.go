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
	if err = s.AuthorizeRead(ctx, fence.Key); err != nil {
		return unknown, err
	}
	h.mu.Lock()
	r, store := h.engine, h.store
	live := h.enabled && !h.closed && !h.cleanupPending
	h.mu.Unlock()
	if !live || r == nil {
		return unknown, nil
	}
	// Live metadata needs current human and source authority. The actor
	// validates its own binding and project from the read-side facts.
	readCheck := func() error {
		if err := s.AuthorizeRead(ctx, fence.Key); err != nil {
			return err
		}
		_, _, err := h.background(ctx, fence.Key, true)
		return err
	}
	state, err := r.NativeContext(ctx, fence, readCheck, store.ReadNativeContextFacts)
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
