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
	_, err := b.Store.UpdateOpened(ctx, store.TableRootAssignments, id, func(o store.Opened) (*store.Opened, []store.Event, error) {
		var a RootAssignment
		if err := json.Unmarshal(o.Record, &a); err != nil {
			return nil, nil, err
		}
		if a.State != AssignmentBriefed || a.Executor == nil || a.Executor.TerminalID != closed.ID ||
			a.Executor.Backend != string(closed.Backend) || a.Assistant != string(closed.Assistant) {
			return nil, nil, errors.New("closed Session is not the Root Assignment executor")
		}
		if a.Closure != nil {
			return nil, nil, nil
		}
		a.Closure = &RootAssignmentClosure{TerminalID: closed.ID, ConversationID: closed.ConversationID,
			Method: method, Forced: forced, Safe: !forced, ClosedAt: b.now().Unix()}
		body, err := json.Marshal(a)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(a.Closure)
		if err != nil {
			return nil, nil, err
		}
		return &store.Opened{ID: o.ID, State: o.State, Record: body, CreatedAt: o.CreatedAt, UpdatedAt: b.now()},
			[]store.Event{{Kind: "root_assignment.closed", Subject: id, Payload: payload}}, nil
	})
	return err
}

// rootCleanup projects the current evidence without changing the assignment
// or treating the brief directory, project checkout or shared cache as scratch.
func (b *Broker) rootCleanup(ctx context.Context, rd reading, a RootAssignment) *RootCleanupEligibility {
	return rootCleanupWithOccupancy(rd, a, func(paths ...string) (string, error) {
		return b.occupied(ctx, paths...)
	})
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
	var cwds []string
	var cwdErr error
	read := false
	occupied := func(paths ...string) (string, error) {
		targets := make([]string, 0, len(paths))
		for _, path := range paths {
			if path != "" {
				targets = append(targets, foldPath(path))
			}
		}
		if len(targets) == 0 {
			return "", nil
		}
		if !read {
			read = true
			readCWDs := b.ProcessCWDs
			if readCWDs == nil {
				readCWDs = lsofCWDs
			}
			cwds, cwdErr = readCWDs(ctx)
		}
		if cwdErr != nil {
			return "", cwdErr
		}
		return occupiedFromCWDs(cwds, targets...), nil
	}
	for i := range assignments {
		assignments[i].Cleanup = rootCleanupWithOccupancy(rd, assignments[i], occupied)
	}
}
