package app

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ReconcileWorkflowLaunches requires the actual provider to descend from the
// launch wrapper. Terminal reuse or a matching cwd alone cannot create a binding.
func ReconcileWorkflowLaunches(ctx context.Context, st *store.Store, inv session.Inventory) error {
	seen := map[string]session.Session{}
	var order []string
	var failures []error
	for _, item := range inv.Sessions {
		if item.WorkflowLaunchID == "" || !item.IsAssistant() {
			continue
		}
		if old, ok := seen[item.WorkflowLaunchID]; ok && (old.ID != item.ID || old.ConversationID != item.ConversationID) {
			seen[item.WorkflowLaunchID] = session.Session{}
		} else if !ok {
			seen[item.WorkflowLaunchID] = item
			order = append(order, item.WorkflowLaunchID)
		}
	}
	for _, id := range order {
		item := seen[id]
		if item.ID == "" {
			continue
		}
		_, found, err := st.WorkflowLaunchByID(ctx, id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !found {
			continue
		}
		if err := st.RecordWorkflowTerminal(ctx, id, item.ID); err != nil {
			failures = append(failures, err)
			continue
		}
		if item.ConversationID != "" {
			if err := st.BindWorkflowConversation(ctx, id, item.ID, item.ConversationID); err != nil {
				failures = append(failures, err)
				continue
			}
		}
	}
	return errors.Join(failures...)
}
