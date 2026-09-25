package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Session creating a Board item on its person's word (work-system-v2 §2,
// invariant 1 as amended on 2026-09-25).
//
// A work item is created by a person, or by a Session relaying a person's
// explicit message through that message's run. The run is the proof: it is
// issued only when a person sends the Session a message through this daemon
// (runs.go), so an item created under one rests on words the person
// demonstrably said to that Session. A person typing straight into the
// terminal leaves no run, and that case stays the proposal path.
//
// An executable item arrives already assigned to the Session that created it,
// with its steps in place; a planning item is created unassigned, in Planning,
// exactly as a person's would be. Everything — the item, its assignment, its
// steps and the receipt — is one transaction: it is written whole or not at
// all.

// runItemLimit is how many items one person's message may back. A message
// that asks for more is a plan, which belongs on the Board as one planning
// item or in several messages; a Session looping on one run is stopped here.
const runItemLimit = 5

// NewSessionItemV2 is one item a Session creates on a person's message.
type NewSessionItemV2 struct {
	// Run is the person's message, already found and checked as a relay's run
	// (Runs.Relay): issued by this daemon, and recent.
	Run work.Run
	// SessionID, TerminalID and Assistant are the live Session creating it.
	SessionID  string
	TerminalID string
	Assistant  string
	// SessionProject is the canonical Project the Session works in; an
	// executable item is assigned to the Session only in that Project.
	SessionProject string

	ProjectID        string
	ProjectPath      string
	Kind             work.Kind
	Title            string
	Description      string
	DeploymentPolicy work.DeploymentPolicy
	// Steps are the item's steps as the person listed them. Given, they are
	// the steps and the description is not read for more; absent, an
	// executable item's steps are seeded from its description's list, as a
	// person's assignment would.
	Steps []string
}

// CreateFromSession creates the item, and for an executable kind its active
// assignment to the creating Session and its steps, in one transaction.
func (w *WorkSystemV2) CreateFromSession(ctx context.Context, n NewSessionItemV2, file WorkV2Filer) (WorkV2View, error) {
	if err := work.RelayTo(n.Run, n.SessionID, time.Time{}); err != nil {
		return WorkV2View{}, relayRefusal(err)
	}
	n.ProjectID, n.ProjectPath = strings.TrimSpace(n.ProjectID), strings.TrimSpace(n.ProjectPath)
	n.Title, n.Description = strings.TrimSpace(n.Title), strings.TrimSpace(n.Description)
	if n.DeploymentPolicy == "" {
		n.DeploymentPolicy = work.DeployAgentDecides
	}
	if err := validateWorkV2Text(n.Title, n.Description); err != nil {
		return WorkV2View{}, err
	}
	steps, err := explicitSteps(n.Steps)
	if err != nil {
		return WorkV2View{}, err
	}
	actor := n.Run.Actor()
	now := w.now()
	i := work.ItemV2{ID: newWorkID(), ProjectID: n.ProjectID, ProjectPath: n.ProjectPath, Kind: n.Kind,
		Title: n.Title, Description: n.Description, Phase: work.PhaseCreated,
		DeploymentPolicy: n.DeploymentPolicy, CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
		Cycle: 1, Version: 1,
		CreatedVia: &work.CreatedViaV2{Run: n.Run.ID, Session: n.SessionID, At: n.Run.At.Unix(), Excerpt: n.Run.Excerpt}}
	if err := work.ValidateNewV2(i); err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	if len(steps) > 0 && i.Planning() {
		return WorkV2View{}, workV2Error(http.StatusUnprocessableEntity, "planning_has_no_steps",
			"Epic, Refactor and Plan stay in Planning and carry no steps; nothing was created. "+
				"Create a Feature or Issue for work with steps.")
	}
	if !i.Planning() && n.SessionProject != i.ProjectPath {
		return WorkV2View{}, workV2Error(http.StatusConflict, "project_mismatch",
			"This Session is not working in that Project, so the item could not be assigned to it; nothing was created.")
	}
	var out WorkV2View
	err = w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if backed, err := tx.ItemsCreatedBy(actor); err != nil {
			return err
		} else if backed >= runItemLimit {
			return workV2Error(http.StatusTooManyRequests, "run_items_exhausted",
				fmt.Sprintf("That message has already created %d items, as many as one message may; nothing was created. "+
					"Ask the person to send another message if they want more.", runItemLimit))
		}
		if err := tx.CreateItem(i, actor, payload(map[string]any{"project_id": i.ProjectID, "kind": i.Kind,
			"via_run": n.Run.ID, "session_id": n.SessionID})); err != nil {
			return err
		}
		out = WorkV2View{Item: i, Assignments: []work.AssignmentV2{}, Documents: []work.DocumentV2{},
			Images: []work.ImageV2{}, Steps: []work.StepV2{}, Events: []work.EventV2{}}
		if !i.Planning() {
			a := work.AssignmentV2{ID: newWorkID(), WorkID: i.ID, Mode: "existing_session", SessionID: n.SessionID,
				TerminalID: n.TerminalID, Assistant: n.Assistant, State: "active", HumanActor: actor,
				CreatedAt: now, UpdatedAt: now}
			if err := tx.CreateAssignment(a); err != nil {
				return err
			}
			seeded, err := addSessionItemSteps(tx, i, steps, n.SessionID, now)
			if err != nil {
				return err
			}
			next := i
			next.OwnerSession, next.Phase = n.SessionID, work.PhaseAssigned
			if err := tx.PutItem(i, next, "item.assigned", actor, payload(map[string]any{
				"assignment_id": a.ID, "mode": a.Mode, "pending": false, "session_id": a.SessionID,
				"seeded_steps": len(seeded), "via_run": n.Run.ID})); err != nil {
				return err
			}
			next.Version++
			out.Item, out.Assignments, out.Steps = next, []work.AssignmentV2{a}, seeded
		}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	return out, nil
}

// explicitSteps are the steps a Session named, trimmed, empty rows dropped,
// held to the per-item step bound and the title bound.
func explicitSteps(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > workV2TitleLimit || !utf8.ValidString(s) {
			return nil, workV2Error(http.StatusBadRequest, "invalid_step",
				fmt.Sprintf("Step %d is over 240 bytes; nothing was created.", len(out)+1))
		}
		out = append(out, s)
	}
	if len(out) > store.WorkV2StepLimit {
		return nil, workV2Error(http.StatusRequestEntityTooLarge, "too_many_steps",
			fmt.Sprintf("An item holds at most %d steps; nothing was created.", store.WorkV2StepLimit))
	}
	return out, nil
}

// addSessionItemSteps writes the steps a Session named, in order, or — when it
// named none — seeds them from the description's list exactly as a person's
// assignment does (seedDescriptionSteps). Never both, so no step is written
// twice.
func addSessionItemSteps(tx *store.WorkV2Tx, item work.ItemV2, titles []string, session string, at time.Time) ([]work.StepV2, error) {
	if len(titles) == 0 {
		return seedDescriptionSteps(tx, item, session, at)
	}
	steps := make([]work.StepV2, 0, len(titles))
	for position, title := range titles {
		step := work.StepV2{ID: newWorkID(), WorkID: item.ID, Title: title, Position: int64(position),
			CreatedBy: session, CreatedAt: at, Version: 1}
		if err := tx.AddStep(step); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func relayRefusal(err error) error {
	if refused, ok := err.(*work.Refusal); ok {
		return workV2Error(refused.Status, refused.Code, refused.Message)
	}
	return err
}
