package app

import (
	"context"
	"errors"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ReconcileSquadLaunches binds only a provider process whose command line
// carries the exact launch ID and whose terminal matches the recorded open.
// The receipt file repairs a crash after a terminal opened but before SQLite
// recorded its ID. Ambiguous or not-yet-named sessions stay pending.
func ReconcileSquadLaunches(ctx context.Context, st *store.Store, stateDir string, inv session.Inventory) error {
	observed := map[string]session.Session{}
	for _, item := range inv.Sessions {
		if item.SquadLaunchID != "" && item.IsAssistant() {
			if prior, seen := observed[item.SquadLaunchID]; seen && (prior.ID != item.ID || prior.ConversationID != item.ConversationID) {
				// Conflicting process or conversation claims are not authoritative.
				observed[item.SquadLaunchID] = session.Session{}
			} else if !seen {
				observed[item.SquadLaunchID] = item
			}
		}
	}
	var cursorAt int64
	var cursorID string
	for {
		pending, err := st.PendingSquadLaunchesAfter(ctx, cursorAt, cursorID)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			break
		}
		for _, launch := range pending {
			terminalID := launch.TerminalID
			if terminalID == "" {
				terminalID, err = squadfiles.ReadTerminal(stateDir, launch.ID)
				if errors.Is(err, os.ErrNotExist) {
					// The process launch ID is itself durable evidence when both the
					// receipt write and SQLite terminal write were interrupted.
					// An ambiguous process claim stays pending.
					terminalID = observed[launch.ID].ID
					if terminalID == "" {
						continue
					}
					err = nil
				}
				if err != nil {
					return err
				}
				if err := st.RecordSquadTerminal(ctx, launch.ID, terminalID); err != nil {
					return err
				}
			}
			item := observed[launch.ID]
			if item.ID == "" || item.ID != terminalID || item.ConversationID == "" {
				continue
			}
			if err := st.BindSquadConversation(ctx, launch.ID, item.ConversationID); err != nil {
				return err
			}
		}
		last := pending[len(pending)-1]
		cursorAt, cursorID = last.CreatedAt, last.ID
		if len(pending) < store.MaxSquadRecoveryRows {
			break
		}
	}
	for launchID, item := range observed {
		if item.ID == "" || item.ConversationID == "" {
			continue
		}
		if err := st.RebindSquadConversation(ctx, launchID, item.ID, item.ConversationID); errors.Is(err, store.ErrSquadLaunchUnknown) {
			continue
		} else if err != nil {
			return err
		}
	}
	return nil
}
