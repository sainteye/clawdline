package projectassistant

import (
	"slices"
	"time"
)

type ActorOperation string

const (
	DispatchWork   ActorOperation = "dispatch_work"
	ReadWork       ActorOperation = "read_work"
	ProposeWork    ActorOperation = "propose_work"
	PublishMessage ActorOperation = "publish_message"
)

// AssistantExecutionActor is resolved from a host-issued capability, never
// decoded from model arguments. It has no terminal, user run or master token.
// PA-05 verifies token authenticity and resolves revocation on every operation.
type AssistantExecutionActor struct {
	CapabilityID string
	Fence        Fence
	Operations   []ActorOperation
	ExpiresAt    time.Time
	Revoked      bool
}

func CheckActor(actor AssistantExecutionActor, current Fence, operation ActorOperation, now time.Time) error {
	if actor.CapabilityID == "" {
		return fail(ScopeDenied, "capability")
	}
	if actor.Revoked {
		return fail(Revoked, "capability")
	}
	if !now.Before(actor.ExpiresAt) {
		return fail(Expired, "capability")
	}
	if err := CheckFence(actor.Fence, current); err != nil {
		return err
	}
	// Even a malformed issuer cannot turn a Bot into a human approval actor.
	switch operation {
	case DispatchWork, ReadWork, ProposeWork, PublishMessage:
	default:
		return fail(ScopeDenied, "actor_operation")
	}
	if !slices.Contains(actor.Operations, operation) {
		return fail(ScopeDenied, "actor_operation")
	}
	return nil
}

type WorkRequest struct {
	Actor   AssistantExecutionActor
	Command Command
	TurnID  string
	Brief   string
	Claims  []string
}

// CheckWorkRequest does not replace the broker's path/claims validation. It
// rejects empty dispatches before the existing isolated-worktree authority.
func CheckWorkRequest(request WorkRequest, current Fence, now time.Time) error {
	if err := CheckActor(request.Actor, current, DispatchWork, now); err != nil {
		return err
	}
	if request.Command.Key != current.Key {
		return fail(ScopeDenied, "work_command")
	}
	if request.Command.Operation != DispatchOperation {
		return fail(Invalid, "work_operation")
	}
	if _, err := DecideReplay(request.Command, nil); err != nil {
		return err
	}
	if request.TurnID == "" || request.Brief == "" || len(request.Claims) == 0 {
		return fail(Invalid, "work")
	}
	for _, claim := range request.Claims {
		if claim == "" {
			return fail(Invalid, "claims")
		}
	}
	return nil
}
