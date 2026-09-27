package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// An Epic's owner breaking it into Feature and Issue items (work-system-v2
// §6.5, D58).
//
// Everywhere else a Session creates a Board item only on a person's message
// (session_items.go) and only a person assigns items to Sessions. An Epic is
// the exception: it is large, its owner plans it, and the plan is reviewed
// before implementing (work.EpicPlanGate). The person assigning the Epic to a
// Session is the authority for that Session to break it into pieces and hand
// them out, so no per-message run is needed — but only for that Epic, only
// after its plan gate, only as Feature or Issue, and at most
// work.EpicChildLimit of them. Each child names its Epic (ParentID) and says
// the Epic's owner created it (CreatedVia.Epic); the Epic cannot be moved to
// done while one is open (work.EpicDoneGate).

// NewEpicChildV2 is one child item an Epic's owner Session creates.
type NewEpicChildV2 struct {
	EpicID string
	// ExpectedVersion is the Epic's version the owner read.
	ExpectedVersion  int64
	SessionID        string
	Kind             work.Kind
	Title            string
	Description      string
	DeploymentPolicy work.DeploymentPolicy
	// Steps are the child's steps as the owner listed them. Absent, they are
	// seeded from the description's list when the child is assigned, as a
	// person's item's are.
	Steps []string
}

// CreateEpicChild writes the child item, its explicit steps, and a record on
// the Epic, in one transaction. The child is unassigned; assigning it is a
// separate step (AssignWorkV2.EpicOwner) so that a failed assignment leaves an
// honest unassigned item rather than a half-assigned one.
func (w *WorkSystemV2) CreateEpicChild(ctx context.Context, n NewEpicChildV2) (WorkV2View, error) {
	n.Title, n.Description = strings.TrimSpace(n.Title), strings.TrimSpace(n.Description)
	if n.DeploymentPolicy == "" {
		n.DeploymentPolicy = work.DeployAgentDecides
	}
	if err := work.EpicChildKind(n.Kind); err != nil {
		return WorkV2View{}, workV2Error(http.StatusUnprocessableEntity, "child_kind_not_allowed", err.(work.RefusalV2).Message)
	}
	if err := validateWorkV2Text(n.Title, n.Description); err != nil {
		return WorkV2View{}, err
	}
	steps, err := explicitSteps(n.Steps)
	if err != nil {
		return WorkV2View{}, err
	}
	actor := work.EpicOwnerActor(n.SessionID)
	var out WorkV2View
	err = w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		epic, err := tx.Item(n.EpicID)
		if err != nil {
			return err
		}
		if epic.Version != n.ExpectedVersion {
			return store.ErrConflict
		}
		if err := work.EpicChildParent(epic, n.SessionID); err != nil {
			return err
		}
		if all, _, err := tx.Children(epic.ID); err != nil {
			return err
		} else if all >= work.EpicChildLimit {
			return workV2Error(http.StatusInsufficientStorage, "epic_children_full",
				fmt.Sprintf("This Epic already holds %d child items, as many as one Epic may; nothing was created. "+
					"Split the remaining work into another Epic, or ask the person.", work.EpicChildLimit))
		}
		now := w.now()
		i := work.ItemV2{ID: newWorkID(), ProjectID: epic.ProjectID, ProjectPath: epic.ProjectPath, Kind: n.Kind,
			Title: n.Title, Description: n.Description, Phase: work.PhaseCreated,
			DeploymentPolicy: n.DeploymentPolicy, CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
			Cycle: 1, Version: 1, ParentID: epic.ID,
			CreatedVia: &work.CreatedViaV2{Session: n.SessionID, At: now.Unix(), Epic: epic.ID}}
		if err := work.ValidateNewV2(i); err != nil {
			return err
		}
		if err := tx.CreateItem(i, actor, payload(map[string]any{"project_id": i.ProjectID, "kind": i.Kind,
			"parent_id": epic.ID, "session_id": n.SessionID})); err != nil {
			return err
		}
		written := make([]work.StepV2, 0, len(steps))
		for position, title := range steps {
			step := work.StepV2{ID: newWorkID(), WorkID: i.ID, Title: title, Position: int64(position),
				CreatedBy: n.SessionID, CreatedAt: now, Version: 1}
			if err := tx.AddStep(step); err != nil {
				return err
			}
			written = append(written, step)
		}
		touched := epic
		touched.UpdatedAt = now
		if err := tx.PutItem(epic, touched, "epic.child_created", actor, payload(map[string]any{
			"child_id": i.ID, "kind": i.Kind, "title": i.Title})); err != nil {
			return err
		}
		out = WorkV2View{Item: i, Assignments: []work.AssignmentV2{}, Documents: []work.DocumentV2{},
			Images: []work.ImageV2{}, Steps: written, Events: []work.EventV2{}}
		return nil
	})
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	return out, nil
}

// epicOwnerMayAssign is the check an Epic owner's (re)assignment of a child
// makes inside the assignment's transaction: the item is a child of an Epic
// that session owns, open and past its plan gate.
func epicOwnerMayAssign(tx *store.WorkV2Tx, child work.ItemV2, session string) error {
	if child.ParentID == "" {
		return work.RefuseV2("not_epic_child",
			"That item is not a child of an Epic; only the person assigns it.")
	}
	epic, err := tx.Item(child.ParentID)
	if err != nil {
		return err
	}
	return work.EpicChildParent(epic, session)
}
