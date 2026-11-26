package projectassistanthost

import (
	"context"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// DecisionAuthority checks the question card offerID as the person's answer
// behind a Board write made from the native conversation `conversation`,
// within window of the answer (projectassistantstore.DecisionAuthority).
func (h *Host) DecisionAuthority(ctx context.Context, conversation, offerID string, window time.Duration) (pa.DecisionAuthority, error) {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return pa.DecisionAuthority{}, failure(pa.Disabled, "store")
	}
	return st.DecisionAuthority(ctx, h.o.MachineID, conversation, offerID, h.o.Clock(), window)
}
