package http

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func managedAntigravityFailure() error {
	return &antigravity.BridgeError{Code: "antigravity_managed_launch_unverified"}
}

// ManagedAntigravityOptions composes the two owned runtimes without starting
// either. The service obtains native credentials before invoking admission and
// repeats the complete process/capability proof in Observe afterwards.
func (s *Server) ManagedAntigravityOptions() AntigravityRuntimeOptions {
	return AntigravityRuntimeOptions{
		Admit:                 s.admitManagedAntigravity,
		PreInvocationResponse: s.managedAntigravityResponse,
	}
}

func (s *Server) admitManagedAntigravity(ctx context.Context, peer antigravity.NativeUnixPeer,
	witness antigravity.Witness, _ string, _ json.RawMessage) (*antigravity.HookLaunch, error) {
	if witness.LaunchID == "" && witness.Capability == "" {
		return nil, nil
	}
	if !peer.UIDKnown || peer.UID != uint32(os.Geteuid()) || peer.PID != witness.HelperPID {
		return nil, managedAntigravityFailure()
	}
	owner, bridge := s.antigravityLaunchStore.Load(), s.antigravityBridge.Load()
	if owner == nil || bridge == nil {
		return nil, managedAntigravityFailure()
	}
	// A guessed ID or wrong secret cannot even pin a prepared process.
	record, err := owner.VerifyLaunchCapability(ctx, witness.LaunchID, witness.Capability)
	if err != nil {
		return nil, err
	}
	process, err := bridge.VerifyHookAncestor(ctx, witness)
	if err != nil {
		return nil, err
	}
	if err := verifyManagedAntigravityWorkspace(ctx, process, record.Workspace); err != nil {
		return nil, err
	}

	if err := owner.PinProcess(ctx, record.ID, process); err != nil {
		return nil, err
	}
	current, err := owner.LookupProcess(ctx, process)
	if err != nil || current.ID != record.ID {
		return nil, managedAntigravityFailure()
	}
	commitment, launch, err := owner.Commitment(ctx, current)
	if err != nil {
		return nil, err
	}
	if err := bridge.CommitLaunch(commitment); err != nil {
		return nil, err
	}
	return &launch, nil
}

// Recheck the kernel workspace at admission and again at context use. An
// inherited capability cannot silently move original Project context elsewhere.
func verifyManagedAntigravityWorkspace(ctx context.Context, process ports.AntigravityProcess, workspace string) error {
	observation, err := antigravity.NewNativeProcessReader(nil).ReadAntigravityProcessWithWorkspace(ctx, process.PID)
	if err != nil {
		return err
	}
	fresh := observation.Process
	fresh.TTY = strings.TrimPrefix(fresh.TTY, "/dev/")
	fresh.TerminalID = process.TerminalID // separately resolved and rechecked by Bridge
	if fresh != process || observation.WorkspaceCWD == "" || observation.WorkspaceCWD != workspace {
		return managedAntigravityFailure()
	}
	return nil
}

func (s *Server) managedAntigravityResponse(ctx context.Context, binding ports.AntigravityBinding) ([]byte, error) {
	owner := s.antigravityLaunchStore.Load()
	if owner == nil {
		return antigravity.ObserverResponse(binding.Event)
	}
	record, err := owner.LookupProcess(ctx, binding.Process)
	if err != nil {
		var typed *antigravitylaunch.Error
		if errors.As(err, &typed) && typed.Code == "antigravity_context_process_unverified" {
			return antigravity.ObserverResponse(binding.Event) // explicit unmanaged observation
		}
		return nil, err
	}
	if err := verifyManagedAntigravityWorkspace(ctx, binding.Process, record.Workspace); err != nil {
		return nil, err
	}
	// Native conversation evidence is recorded before preparing original context.
	// The response receipt remains PREPARED; only a separate native transcript
	// proof may mark context loaded or attach a role badge.
	if err := owner.RecordBinding(ctx, record.ID, binding, antigravitylaunch.KeepConversation); err != nil {
		return nil, err
	}
	return owner.PreInvocationResponse(ctx, binding)
}
