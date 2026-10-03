package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// NameOwnedSession changes the label of the Root this item's current
// new-session assignment opened. The item, assignment and Root are checked
// and changed in one transaction, so reassignment cannot pass between them.
// It is first-write-only: a repeated name is a no-op and a different name is
// a conflict. No receipt may replay a success after ownership was released.
func (w *WorkSystemV2) NameOwnedSession(ctx context.Context, itemID, sessionID, title string) (orchestrator.RootAssignment, error) {
	var named orchestrator.RootAssignment
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		item, err := tx.Item(itemID)
		if err != nil {
			return err
		}
		assignment, err := tx.ActiveAssignment(itemID)
		if err != nil {
			return err
		}
		if item.OwnerSession != sessionID || assignment.SessionID != sessionID || assignment.Mode != "new_session" ||
			assignment.RootAssignment == "" || assignment.TerminalID == "" || assignment.State != "active" {
			return workV2Error(http.StatusConflict, "not_item_owner",
				"Only the item's active new Session may name itself, and this request came from another Session. "+
					"See the item's owner with `clawdline item show "+itemID+"`.")
		}
		_, err = tx.UpdateOpened(store.TableRootAssignments, assignment.RootAssignment, func(opened store.Opened) (*store.Opened, []store.Event, error) {
			if err := json.Unmarshal(opened.Record, &named); err != nil {
				return nil, nil, workV2Error(http.StatusConflict, "root_assignment_unreadable", "The Root Assignment cannot be read.")
			}
			if named.ID != assignment.RootAssignment || named.Executor == nil || named.Executor.TerminalID != assignment.TerminalID ||
				named.Assistant != assignment.Assistant || named.ProjectDir != item.ProjectPath {
				return nil, nil, workV2Error(http.StatusConflict, "root_assignment_mismatch",
					"The active assignment does not match the Session's Root Assignment.")
			}
			if named.AgentNamed {
				if named.Label == title {
					return nil, nil, nil
				}
				return nil, nil, workV2Error(http.StatusConflict, "session_already_named",
					"This Session has already named itself; use the Session's manual title action to change it.")
			}
			named.Label, named.AgentNamed = title, true
			body, err := json.Marshal(named)
			if err != nil {
				return nil, nil, err
			}
			opened.Record, opened.UpdatedAt = body, w.now()
			return &opened, []store.Event{{Kind: "root_assignment.session_named", Subject: named.ID,
				Payload: json.RawMessage(`{}`)}}, nil
		})
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNoOpened) {
			return workV2Error(http.StatusConflict, "root_assignment_missing", "The active assignment has no readable Root Assignment.")
		}
		return err
	})
	return named, mapWorkV2Error(err)
}
