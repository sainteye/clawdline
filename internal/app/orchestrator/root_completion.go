package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// RecordRootClosure links an actual Session close to the assignment whose
// launch identity the close route verified before taking the terminal away.
// The store update and its event are one transaction; a repeat writes neither.
func (b *Broker) RecordRootClosure(ctx context.Context, id string, closed session.Session, method string, forced bool) error {
	if b == nil || b.Store == nil || id == "" || closed.ID == "" || (method != "close" && method != "archive" && method != "scheduled") {
		return errors.New("root closure has no verified assignment and close identity")
	}
	_, effectIDs, err := b.Store.UpdateOpenedWithEffects(ctx, store.TableRootAssignments, id, func(o store.Opened) (*store.Opened, []store.Event, []store.Effect, error) {
		var a RootAssignment
		if err := json.Unmarshal(o.Record, &a); err != nil {
			return nil, nil, nil, err
		}
		if a.State != AssignmentBriefed || a.Executor == nil || a.Executor.TerminalID != closed.ID ||
			a.Executor.Backend != string(closed.Backend) || a.Assistant != string(closed.Assistant) {
			return nil, nil, nil, errors.New("closed Session is not the Root Assignment executor")
		}
		if a.Closure != nil {
			return nil, nil, nil, nil
		}
		a.Closure = &RootAssignmentClosure{TerminalID: closed.ID, ConversationID: closed.ConversationID,
			Method: method, Forced: forced, Safe: !forced, ClosedAt: b.now().Unix()}
		body, err := json.Marshal(a)
		if err != nil {
			return nil, nil, nil, err
		}
		payload, err := json.Marshal(a.Closure)
		if err != nil {
			return nil, nil, nil, err
		}
		var effects []store.Effect
		if a.BotOwner != nil && a.BotOwner.DeliveredAt == 0 && b.AssistantRootClosed != nil {
			effects = append(effects, store.Effect{Kind: EffectAssistantRootClosed, Subject: a.ID, Payload: payload})
		}
		return &store.Opened{ID: o.ID, State: o.State, Record: body, CreatedAt: o.CreatedAt, UpdatedAt: b.now()},
			[]store.Event{{Kind: "root_assignment.closed", Subject: id, Payload: payload}}, effects, nil
	})
	if err != nil {
		return err
	}
	if len(effectIDs) > 0 {
		b.runRecorded(ctx, effectIDs)
	}
	return nil
}

// rootCleanup projects the current evidence without changing the assignment
// or treating the brief directory, project checkout or shared cache as scratch.
func (b *Broker) rootCleanup(ctx context.Context, rd reading, a RootAssignment) *RootCleanupEligibility {
	p := rootCleanupWithOccupancy(rd, a, func(paths ...string) (string, error) {
		return b.occupied(ctx, paths...)
	})
	b.projectRegisteredRootResources(ctx, a.ID, p)
	return p
}

func rootCleanupWithOccupancy(rd reading, a RootAssignment, occupied func(...string) (string, error)) *RootCleanupEligibility {
	p := &RootCleanupEligibility{Owner: "unknown", Scratch: "unregistered", Preservation: "unverified",
		Reason: "completion_missing", NextOwner: "root"}
	p.Completion = a.Closure != nil && a.Executor != nil && a.Closure.Safe && !a.Closure.Forced &&
		a.Closure.TerminalID == a.Executor.TerminalID && a.Closure.ClosedAt > 0
	if a.Closure != nil && !p.Completion {
		p.Reason = "close_forced_or_uncertain"
	}
	if a.Executor == nil || a.Executor.TerminalID == "" {
		p.Reason = "owner_unknown"
		p.NextOwner = "broker"
		return p
	}
	if _, present := rd.session(a.Executor.TerminalID); present {
		p.Owner = "present"
		p.Reason = "owner_present"
		return p
	}
	if len(rd.sessions) == 0 || !rd.sourceComplete(a.Executor.Backend) {
		p.Reason = "owner_unknown"
		p.NextOwner = "broker"
		return p
	}
	// A shell still working in the project or the brief directory is enough to
	// retain. These paths can be shared, so their emptiness is conservative
	// evidence only; it never by itself authorizes deletion.
	inside, err := occupied(a.ProjectDir, filepath.Dir(a.BriefPath))
	if err != nil {
		p.Reason = "owner_unknown"
		p.NextOwner = "broker"
		return p
	}
	if inside != "" {
		p.Owner = "present"
		p.Reason = "owner_present"
		return p
	}
	p.Owner = "absent"
	if p.Completion {
		p.Reason = "scratch_not_registered"
		p.NextOwner = "broker"
	}
	return p
}

// ProjectRootCleanup obtains a fresh terminal reading for the requested
// assignments. The stored rows never contain this transient conclusion.
func (b *Broker) ProjectRootCleanup(ctx context.Context, assignments []RootAssignment) {
	rd := b.read(ctx)
	occupied := b.occupancySnapshot(ctx)
	for i := range assignments {
		assignments[i].Cleanup = rootCleanupWithOccupancy(rd, assignments[i], occupied)
		b.projectRegisteredRootResources(ctx, assignments[i].ID, assignments[i].Cleanup)
	}
}

// A Root projection is not an aggregate deletion grant. Registered resource
// plans independently show retention, output manifests, pins and live readers.
func (b *Broker) projectRegisteredRootResources(ctx context.Context, id string, p *RootCleanupEligibility) {
	if b.Resources == nil {
		return
	}
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "root", id, "")
	if err != nil {
		p.Scratch = "unknown"
		p.Reason = "resource_register_unknown"
		return
	}
	if len(rows) == 0 {
		return
	}
	p.Scratch = "registered"
	p.Preservation = "per_resource_manifest"
	if p.Completion && p.Owner == "absent" {
		p.Reason = "inspect_registered_resources"
		p.NextOwner = "resource_service"
	}
}
