package http

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
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
		PreToolUseResponse:    s.managedAntigravityToolResponse,
		AfterObservation:      s.observeManagedAntigravityCompletion,
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

	if record.Policy != nil {
		if err := s.checkProductionAntigravityPolicy(ctx, record, process); err != nil {
			return nil, err
		}
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
	_, managed, err := owner.ManagedRecordForProcess(ctx, binding.Process)
	if err != nil {
		return nil, err
	}
	if !managed {
		return antigravity.ObserverResponse(binding.Event)
	}
	record, err := owner.LookupProcess(ctx, binding.Process)
	if err != nil {
		return nil, err
	}
	if record.Policy != nil {
		if err := s.checkProductionAntigravityPolicy(ctx, record, binding.Process); err != nil {
			return nil, err
		}
	}
	if err := verifyManagedAntigravityWorkspace(ctx, binding.Process, record.Workspace); err != nil {
		return nil, err
	}
	home, err := s.antigravityRecordHome()
	if err != nil {
		return nil, managedAntigravityFailure()
	}
	check := func(c context.Context, evidence ports.AntigravityBinding) error {
		return s.checkManagedAntigravityContext(c, record.ID, evidence)
	}
	injection, err := owner.ExpectedInjection(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if injection.Text == "" {
		// An empty immutable context emits no system step. Retain native
		// identity and preparation, but never wait for or claim loaded proof.
		if err := check(ctx, binding); err != nil {
			return nil, err
		}
		if err := owner.RecordBinding(ctx, record.ID, binding, antigravitylaunch.KeepConversation); err != nil {
			return nil, err
		}
		return owner.PreInvocationResponse(ctx, binding)
	}
	// A second response cannot overwrite the unresolved baseline of the first.
	// Verify its native system row synchronously; no poller or automatic retry.
	previous, found, err := owner.Receipt(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if found && previous.Fenced {
		if err := owner.VerifyContextLoaded(ctx, record.ID, home, check); err != nil {
			return nil, err
		}
	}
	// Positive native identity and the complete baseline precede response
	// publication. PREPARED is still not a loaded role or an applied receipt.
	if err := check(ctx, binding); err != nil {
		return nil, err
	}
	if err := owner.RecordBinding(ctx, record.ID, binding, antigravitylaunch.KeepConversation); err != nil {
		return nil, err
	}
	fence, err := transcript.CaptureAntigravityContextFence(ctx, home, binding, check)
	if err != nil {
		return nil, err
	}
	return owner.PrepareFencedResponse(ctx, binding, fence)
}

// checkManagedAntigravityContext rechecks complete kernel execution, exact
// terminal, capability, original files, kernel workspace and positive native
// identity around every proof read. Later events may advance the sequence but
// may not switch the execution or conversation. State freshness is unrelated
// to original context having been loaded into this still-current execution.
func (s *Server) checkManagedAntigravityContext(ctx context.Context, launchID string, evidence ports.AntigravityBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	owner, bridge := s.antigravityLaunchStore.Load(), s.antigravityBridge.Load()
	if owner == nil || bridge == nil || !evidence.Verified {
		return managedAntigravityFailure()
	}
	fresh, err := bridge.VerifyAntigravityProcess(ctx, evidence.Process.PID)
	if err != nil {
		return err
	}
	if fresh != evidence.Process {
		return managedAntigravityFailure()
	}
	record, err := owner.LookupProcess(ctx, fresh)
	if err != nil {
		return err
	}
	if record.ID != launchID || (record.NativeID != "" && record.NativeID != evidence.ConversationID) {
		return managedAntigravityFailure()
	}
	if record.Policy != nil {
		if err := s.checkProductionAntigravityPolicy(ctx, record, fresh); err != nil {
			return err
		}
	}
	if err := verifyManagedAntigravityWorkspace(ctx, fresh, record.Workspace); err != nil {
		return err
	}
	birth, err := time.Parse(time.RFC3339Nano, fresh.Birth)
	if err != nil {
		return managedAntigravityFailure()
	}
	native, err := bridge.AntigravityBinding(ctx, session.Session{ID: fresh.TerminalID, PID: fresh.PID, TTY: fresh.TTY, Backend: session.Backend(record.Backend), Assistant: session.AssistantAntigravity, ExpectedStart: birth, ConversationID: evidence.ConversationID})
	if err != nil {
		return err
	}
	if !native.Verified || native.Process != fresh || native.ConversationID != evidence.ConversationID || native.TranscriptPath != evidence.TranscriptPath || native.Sequence < evidence.Sequence || s.antigravityLaunchStore.Load() != owner || s.antigravityBridge.Load() != bridge {
		return managedAntigravityFailure()
	}
	return ctx.Err()
}

// Completion verifies the most recent fenced response without preparing another
// injection. The first model turn can therefore retain loaded evidence without
// requiring a second PreInvocation. It does not create Squad skill receipts.
func (s *Server) observeManagedAntigravityCompletion(ctx context.Context, binding ports.AntigravityBinding) error {
	if binding.Event != "PostInvocation" && binding.Event != "Stop" {
		return ctx.Err()
	}
	if !binding.Verified {
		return managedAntigravityFailure()
	}
	owner := s.antigravityLaunchStore.Load()
	if owner == nil {
		return ctx.Err()
	}
	_, managed, err := owner.ManagedRecordForProcess(ctx, binding.Process)
	if err != nil {
		return err
	}
	if !managed {
		return ctx.Err()
	}
	record, err := owner.LookupProcess(ctx, binding.Process)
	if err != nil {
		return err
	}
	receipt, found, err := owner.Receipt(ctx, record.ID)
	if err != nil {
		return err
	}
	if !found {
		return ctx.Err() // no response was prepared; never manufacture proof
	}
	if !receipt.Fenced {
		injection, err := owner.ExpectedInjection(ctx, record.ID)
		if err != nil {
			return err
		}
		if injection.Text != "" || receipt.InjectionDigest != injection.Digest || receipt.Loaded {
			return managedAntigravityFailure()
		}
		return s.checkManagedAntigravityContext(ctx, record.ID, binding)
	}
	if err := s.checkManagedAntigravityContext(ctx, record.ID, binding); err != nil {
		return err
	}
	home, err := s.antigravityRecordHome()
	if err != nil {
		return managedAntigravityFailure()
	}
	check := func(c context.Context, evidence ports.AntigravityBinding) error {
		return s.checkManagedAntigravityContext(c, record.ID, evidence)
	}
	if err := owner.VerifyContextLoaded(ctx, record.ID, home, check); err != nil {
		return err
	}
	return check(ctx, binding)
}
