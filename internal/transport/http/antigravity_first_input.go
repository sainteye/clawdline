package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Native policy is a trusted local check, not a browser boolean or a generic
// permission-mode translation. It must recheck provider permission and the
// current usage policy at each boundary. Missing policy never allows input.
type antigravityFirstInputPolicy func(context.Context, antigravitylaunch.Record, ports.AntigravityProcess) error

type managedAntigravityFirstInputs struct {
	server *Server
	owner  *antigravitylaunch.Store
	bridge *antigravity.Bridge
	policy antigravityFirstInputPolicy
}

// sendPreparedAntigravityFirst composes the native private path. Public start
// and send routes remain guarded until their native policy is independently
// verified. No capability, fake UUID or caller-supplied pane crosses the wire.
func (s *Server) sendPreparedAntigravityFirst(ctx context.Context, launchID, requestID, text string, policy antigravityFirstInputPolicy) (session.Session, error) {
	a := s.actions()
	a.AntigravityFirstInput = s.preparedAntigravityInputs(policy)
	return a.SendAntigravityFirst(ctx, launchID, requestID, text)
}

func (s *Server) preparedAntigravityInputs(policy antigravityFirstInputPolicy) *managedAntigravityFirstInputs {
	return &managedAntigravityFirstInputs{server: s, owner: s.antigravityLaunchStore.Load(), bridge: s.antigravityBridge.Load(), policy: policy}
}
func (p *managedAntigravityFirstInputs) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.server == nil || p.owner == nil || p.bridge == nil || p.policy == nil || p.server.antigravityLaunchStore.Load() != p.owner || p.server.antigravityBridge.Load() != p.bridge {
		return managedAntigravityFailure()
	}
	return nil
}
func (p *managedAntigravityFirstInputs) tmux() (*terminal.Tmux, error) {
	var found *terminal.Tmux
	for _, host := range p.server.terminals {
		if t, ok := host.(*terminal.Tmux); ok {
			if found != nil {
				return nil, managedAntigravityFailure()
			}
			found = t
		}
	}
	if found == nil {
		return nil, managedAntigravityFailure()
	}
	return found, nil
}

func (p *managedAntigravityFirstInputs) Target(ctx context.Context, id string) (ports.AntigravityFirstInputTarget, error) {
	empty := ports.AntigravityFirstInputTarget{}
	if err := p.ready(ctx); err != nil {
		return empty, err
	}
	record, err := p.owner.PreparedRecord(ctx, id)
	if err != nil {
		return empty, err
	}
	if record.RequestedResume != "" || record.Backend != string(session.BackendTmux) || record.TerminalID == "" {
		return empty, managedAntigravityFailure()
	}
	slot, hasSlot, err := p.owner.FirstInputForLaunch(ctx, id)
	if err != nil {
		return empty, err
	}
	if record.NativeID != "" && !hasSlot {
		return empty, managedAntigravityFailure()
	}
	tmux, err := p.tmux()
	if err != nil {
		return empty, err
	}
	native, err := tmux.PreparedAntigravityPaneProcess(ctx, record.TerminalID, p.bridge)
	if err != nil {
		return empty, err
	}
	if err := verifyManagedAntigravityWorkspace(ctx, native, record.Workspace); err != nil {
		return empty, err
	}
	if err := p.policy(ctx, record, native); err != nil {
		return empty, err
	}
	_, bound, err := p.bridge.PreparedTmuxBinding(ctx, native)
	if err != nil {
		return empty, err
	}
	// Only the existing consumed slot can admit a later positive identity.
	// A conversation observed before this first reservation is not a fresh launch.
	if bound && (!hasSlot || slot.Phase != antigravitylaunch.FirstInputEnterAttempted) {
		return empty, managedAntigravityFailure()
	}
	if err := p.owner.PinProcess(ctx, id, native); err != nil {
		return empty, err
	}
	current, err := p.owner.LookupProcess(ctx, native)
	if err != nil || current.ID != id {
		return empty, managedAntigravityFailure()
	}
	commitment, _, err := p.owner.Commitment(ctx, current)
	if err != nil {
		return empty, err
	}
	if err := p.bridge.CommitLaunch(commitment); err != nil {
		return empty, err
	}
	target := ports.AntigravityFirstInputTarget{LaunchID: id, Backend: record.Backend, Process: native}
	if err := p.Check(ctx, target); err != nil {
		return empty, err
	}
	return target, nil
}

func (p *managedAntigravityFirstInputs) Check(ctx context.Context, target ports.AntigravityFirstInputTarget) error {
	if err := p.ready(ctx); err != nil {
		return err
	}
	record, err := p.owner.LookupProcess(ctx, target.Process)
	if err != nil {
		return err
	}
	if record.ID != target.LaunchID || record.Backend != target.Backend || target.Backend != string(session.BackendTmux) || record.RequestedResume != "" {
		return managedAntigravityFailure()
	}
	if err := verifyManagedAntigravityWorkspace(ctx, target.Process, record.Workspace); err != nil {
		return err
	}
	if err := p.policy(ctx, record, target.Process); err != nil {
		return err
	}
	binding, bound, err := p.bridge.PreparedTmuxBinding(ctx, target.Process)
	if err != nil {
		return err
	}
	slot, hasSlot, err := p.owner.FirstInputForLaunch(ctx, record.ID)
	if err != nil {
		return err
	}
	if bound {
		if !hasSlot || slot.Process != target.Process || slot.Phase != antigravitylaunch.FirstInputEnterAttempted || (record.NativeID != "" && record.NativeID != binding.ConversationID) || binding.Sequence < record.BindingSequence {
			return managedAntigravityFailure()
		}
		if binding.Sequence > record.BindingSequence {
			if err := p.owner.RecordBinding(ctx, record.ID, binding, antigravitylaunch.KeepConversation); err != nil {
				return err
			}
		}
	} else if record.NativeID != "" {
		return managedAntigravityFailure()
	}
	// Do this last, including after a possible durable UUID latch. Literal tmux
	// load/paste/key callbacks each call this path; lost foreground stops the
	// next effect, even when the process and private capability still exist.
	tmux, err := p.tmux()
	if err != nil {
		return err
	}
	fresh, err := tmux.PreparedAntigravityPaneProcess(ctx, target.Process.TerminalID, p.bridge)
	if err != nil {
		return err
	}
	if fresh != target.Process {
		return managedAntigravityFailure()
	}
	return p.ready(ctx)
}

func (p *managedAntigravityFirstInputs) Reserve(ctx context.Context, target ports.AntigravityFirstInputTarget, key, digest string) (ports.AntigravityFirstInputAttempt, error) {
	if err := p.Check(ctx, target); err != nil {
		return nil, err
	}
	_, attempt, err := p.owner.ReserveFirstInput(ctx, target.LaunchID, key, digest, target.Process)
	if err != nil || attempt == nil {
		return nil, err
	}
	return attempt, nil
}
