package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
)

// GateDispatch is the closed daemon-authored request for an independent
// checker. Callers cannot choose permissions, claims, isolation, or kind.
type GateDispatch struct {
	TaskID       string
	WorkID       string
	Title        string
	Instructions string
	Persona      string
	Root         RootRef
	Gate         GateOrigin
}

// DispatchGate persists the immutable task brief before asking Dispatch to
// create any terminal. A replay of the same deterministic task id returns the
// already-held task and never opens a second checker.
func (b *Broker) DispatchGate(ctx context.Context, in GateDispatch) (Dispatched, error) {
	record := Record{Protocol: Protocol, ID: in.TaskID, Kind: TaskKindVerificationGate,
		Assistant: "codex", PermissionMode: "ask", Claims: []string{}, Isolation: IsolationWorktree,
		ProjectDir: in.Gate.Candidate.Repository, Title: in.Title, Instructions: in.Instructions,
		TimeoutMinutes: 30, Persona: in.Persona, Root: &in.Root, WorkID: in.WorkID,
		CreatedAt: b.now().UTC(), Gate: &in.Gate}
	brief := record.Brief()
	body, err := json.Marshal(brief)
	if err != nil {
		return Dispatched{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return Dispatched{}, err
	}
	if err := b.writeBriefOnce(in.TaskID, raw); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Dispatched{}, err
		}
		existingBody, readErr := os.ReadFile(filepath.Join(b.Tasks.Path(in.TaskID), "task.json"))
		var existing taskdir.Brief
		if readErr != nil || json.Unmarshal(existingBody, &existing) != nil {
			return Dispatched{}, refuse(409, "gate_replay_mismatch", "The existing checker brief cannot prove the requested replay identity.")
		}
		var existingGate GateOrigin
		if json.Unmarshal(existing.VerificationGate, &existingGate) != nil || existing.TaskID != in.TaskID ||
			existing.Kind != TaskKindVerificationGate || existing.Assistant != "codex" || existing.Isolation != IsolationWorktree ||
			existing.ProjectDir != in.Gate.Candidate.Repository || existing.WorkID != in.WorkID || existing.Persona != in.Persona ||
			len(existing.Claims) != 0 || !reflect.DeepEqual(existingGate, in.Gate) {
			return Dispatched{}, refuse(409, "gate_replay_mismatch",
				fmt.Sprintf("Task %s already has a different immutable checker identity.", in.TaskID))
		}
	}
	return b.Dispatch(ctx, DispatchRequest{TaskID: in.TaskID, Secret: NewSecret(), InternalGate: true})
}

// FindGateRespawn reconciles a Respawn response lost after the descendant was
// durably admitted. A gate family has at most one descendant, so more than one
// is corruption and is never guessed through.
func (b *Broker) FindGateRespawn(ctx context.Context, parent string) (Record, bool, error) {
	records, _, err := b.Records(ctx)
	if err != nil {
		return Record{}, false, err
	}
	var found Record
	for _, record := range records {
		if record.RespawnOf != parent || record.Gate == nil || record.Kind != TaskKindVerificationGate {
			continue
		}
		if found.ID != "" && found.ID != record.ID {
			return Record{}, false, refuse(409, "gate_respawn_ambiguous", "More than one checker descendant claims the same gate attempt.")
		}
		found = record
	}
	return found, found.ID != "", nil
}

func (b *Broker) PurgeGateSubmissions(taskID string) error {
	return b.Tasks.PurgeGateSubmissions(taskID)
}
