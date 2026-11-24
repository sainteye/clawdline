package projectassistant

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/work"
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
	Actor         AssistantExecutionActor
	Command       Command
	TurnID        string
	Title         string
	Brief         string
	Claims        []string
	TargetProject ProjectScope
	BoardItemID   string
	// ContinueOnCompletion asks the host to queue one turn after the worker settles.
	ContinueOnCompletion bool
	// Persona is an optional Session persona id. The broker admits it with
	// the same rule as every other dispatch; empty keeps the default Session.
	Persona string
	// Choice holds the optional kind, agent, model and effort arguments.
	// The broker resolves them against the machine's dispatch rules.
	Choice DispatchChoice
	// OwnerFence and OwnerTurnID, set only by the host, name the main chat
	// that owns work a Bot's thread worker dispatched at another Bot's
	// request: the turn is the host notice written there for it. Zero means
	// the actor's own conversation and turn own the work.
	OwnerFence  Fence
	OwnerTurnID string
}

// Owner is the conversation and turn that own the requested work.
func (q WorkRequest) Owner() (Fence, string) {
	if q.OwnerTurnID != "" {
		return q.OwnerFence, q.OwnerTurnID
	}
	return q.Actor.Fence, q.TurnID
}

// ValidateWorkTitle applies the broker's task title rules before an effect
// receipt is written. Titles are displayed independently of the full brief.
func ValidateWorkTitle(title string) error {
	if !utf8.ValidString(title) || strings.IndexFunc(title, unicode.IsControl) >= 0 ||
		strings.ContainsAny(title, "\u2028\u2029") || work.OutcomeTitleRefusal(title) != "" {
		return fail(Invalid, "work_title")
	}
	return nil
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
	if request.TurnID == "" || request.Brief == "" || request.Claims == nil {
		return fail(Invalid, "work")
	}
	if target := request.TargetProject; target != (ProjectScope{}) {
		if err := ValidateScope(target); err != nil {
			return err
		}
		if target.Kind != RepositoryProject || target.MachineID != current.Key.MachineID {
			return fail(ScopeDenied, "dispatch_project")
		}
	} else if len(request.Claims) == 0 && !request.ContinueOnCompletion && request.OwnerTurnID == "" {
		// Claimless work in the Bot's own Project is read-only work whose
		// result the Bot reads back: through a completion turn, or, for work
		// dispatched at another Bot's request (OwnerTurnID), through the
		// main chat's review turn.
		return fail(Invalid, "work")
	}
	if request.Title != "" {
		if err := ValidateWorkTitle(request.Title); err != nil {
			return err
		}
	}
	for _, claim := range request.Claims {
		if claim == "" {
			return fail(Invalid, "claims")
		}
	}
	return nil
}
