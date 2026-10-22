package http

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/antigravityproject"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/launchplan"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// These private immutable inputs belong to one concrete runtime and Project
// owner. No browser setter, settings inspector or successful callback can
// manufacture native enforcement evidence.
type antigravityPolicyInputs struct {
	lifetime     context.Context
	projectOwner *antigravityproject.Manager
	launchStore  *antigravitylaunch.Store
	bridge       *antigravity.Bridge
	project      func(context.Context, string) (antigravityProjectPolicy, error)
	behavior     func(antigravity.ImageFingerprint, string) (antigravityNativeBehavior, error)
}
type antigravityProjectPolicy struct{ Revision, HookDigest, HelperDigest string }
type antigravityNativeEnforcement uint8

// This strategy is conditional on an exact-image mandatory native approval
// contract. Selecting it is not evidence that the installed CLI implements it.
const antigravityReviewEachTool antigravityNativeEnforcement = 1

type antigravityNativeBehavior struct {
	ID, ImageDigest, Platform, Architecture, Ceiling, Mode string
	Enforcement                                            antigravityNativeEnforcement
}

func (b antigravityNativeBehavior) toolDecision() (string, error) {
	if b.Enforcement != antigravityReviewEachTool || b.Ceiling != "ask" {
		return "", antigravityPolicyRefusal("native_enforcement_unverified")
	}
	return "force_ask", nil
}

func antigravityPolicyRefusal(reason string) error {
	return &antigravity.BridgeError{Code: "antigravity_policy_" + reason}
}
func nativePolicyDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// No compiled production row is active. The exact native permission/grant/
// failure matrix, not version/help or observed argv, must activate a row.
func productionAntigravityBehavior(_ antigravity.ImageFingerprint, _ string) (antigravityNativeBehavior, error) {
	return antigravityNativeBehavior{}, antigravityPolicyRefusal("native_behavior_unverified")
}
func (i *antigravityPolicyInputs) inspect(ctx context.Context, workspace string, image antigravity.ImageFingerprint, ceiling string) (antigravityProjectPolicy, antigravityNativeBehavior, error) {
	if err := ctx.Err(); err != nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, err
	}
	if i == nil || i.project == nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("project_consent_unavailable")
	}
	if ceiling != "ask" && ceiling != "edits" && ceiling != "full" {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("ceiling_invalid")
	}
	resolver := i.behavior
	if resolver == nil {
		resolver = productionAntigravityBehavior
	}
	behavior, err := resolver(image, ceiling)
	if err != nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("native_behavior_unverified")
	}
	if behavior.ID == "" || behavior.ImageDigest != image.Digest || behavior.Platform != image.Platform || behavior.Architecture != image.Architecture || behavior.Ceiling != ceiling ||
		(behavior.Mode != "plan" && behavior.Mode != "accept-edits" && behavior.Mode != "auto") {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("native_behavior_unverified")
	}
	if _, err := behavior.toolDecision(); err != nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, err
	}
	project, err := i.project(ctx, workspace)
	if err != nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("project_consent_unavailable")
	}
	if !nativePolicyDigest(project.Revision) || !nativePolicyDigest(project.HookDigest) || !nativePolicyDigest(project.HelperDigest) {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, antigravityPolicyRefusal("project_plan_unverified")
	}
	if err := ctx.Err(); err != nil {
		return antigravityProjectPolicy{}, antigravityNativeBehavior{}, err
	}
	return project, behavior, nil
}
func (s *Server) currentAntigravityPolicy(ctx context.Context, owner *antigravitylaunch.Store, bridge *antigravity.Bridge, inputs *antigravityPolicyInputs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner == nil || bridge == nil || inputs == nil || inputs.lifetime == nil || inputs.lifetime.Err() != nil || inputs.projectOwner == nil || s.antigravityProjectOwner.Load() != inputs.projectOwner || inputs.launchStore != owner || inputs.bridge != bridge || s.antigravityLaunchStore.Load() != owner || s.antigravityBridge.Load() != bridge || s.antigravityPolicyInputs.Load() != inputs {
		return antigravityPolicyRefusal("runtime_unavailable")
	}
	return nil
}

// prepareProductionAntigravity is shared by the owning HTTP receipt and broker
// intent. It allocates no ID, installs no hooks and opens/sends nothing. The
// exact project plan must already be authorized and current at its owner.
func (s *Server) prepareProductionAntigravity(ctx context.Context, req projects.LaunchRequest, intent ports.AntigravityLaunchIntent) (launchplan.PreparedAntigravity, error) {
	owner, bridge, inputs := s.antigravityLaunchStore.Load(), s.antigravityBridge.Load(), s.antigravityPolicyInputs.Load()
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return launchplan.PreparedAntigravity{}, err
	}
	image, err := bridge.ImageFingerprint(ctx)
	if err != nil {
		return launchplan.PreparedAntigravity{}, err
	}
	project, behavior, err := inputs.inspect(ctx, req.ProjectRoot, image, intent.Ceiling)
	if err != nil {
		return launchplan.PreparedAntigravity{}, err
	}
	req.Executable, req.PermissionMode = image.Executable, behavior.Mode
	policy := antigravitylaunch.NativePolicy{Version: antigravitylaunch.PolicyVersion, Ceiling: intent.Ceiling,
		ExecutableDigest: image.Digest, CompatibilityID: behavior.ID, NativeMode: behavior.Mode,
		HookConfigDigest: project.HookDigest, HelperDigest: project.HelperDigest, ConsentRevision: project.Revision,
		Model: req.Model, Effort: req.ReasoningEffort}
	prepared, err := owner.PrepareLaunchWithPolicy(ctx, intent.LaunchID, req, policy)
	if err != nil {
		return launchplan.PreparedAntigravity{}, err
	}
	abort := func(c context.Context) error { return owner.RevokeUnopenedLaunch(c, intent.LaunchID) }
	if _, err = projects.Admit(prepared.Request); err != nil {
		// This method has positively not opened anything. Preserve the original
		// ID/evidence but prevent partially admitted preparation from activation.
		if revokeErr := abort(ctx); revokeErr != nil {
			return launchplan.PreparedAntigravity{}, antigravityPolicyRefusal("preparation_uncertain")
		}
		return launchplan.PreparedAntigravity{}, err
	}
	if err := s.checkPreparedAntigravityPolicy(ctx, prepared.Record); err != nil {
		if revokeErr := abort(ctx); revokeErr != nil {
			return launchplan.PreparedAntigravity{}, antigravityPolicyRefusal("preparation_uncertain")
		}
		return launchplan.PreparedAntigravity{}, err
	}
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return launchplan.PreparedAntigravity{}, err
	}
	return launchplan.PreparedAntigravity{LaunchID: intent.LaunchID, Request: prepared.Request, PrivateEnv: prepared.PrivateEnv,
		AttachTerminal: func(c context.Context, backend, terminal string) error {
			return owner.AttachTerminal(c, intent.LaunchID, backend, terminal)
		}, AbortBeforeOpen: abort}, nil
}

func (s *Server) checkPreparedAntigravityPolicy(ctx context.Context, expected antigravitylaunch.Record) error {
	owner, bridge, inputs := s.antigravityLaunchStore.Load(), s.antigravityBridge.Load(), s.antigravityPolicyInputs.Load()
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return err
	}
	policy, err := owner.PolicyForLaunch(ctx, expected.ID)
	if err != nil {
		return err
	}
	current, err := owner.PreparedRecord(ctx, expected.ID)
	if err != nil {
		return err
	}
	if expected.Policy == nil || policy != *expected.Policy || current.ContextHash != expected.ContextHash || current.RequestDigest != expected.RequestDigest || current.Workspace != expected.Workspace {
		return antigravityPolicyRefusal("launch_changed")
	}
	image, err := bridge.ImageFingerprint(ctx)
	if err != nil {
		return err
	}
	project, behavior, err := inputs.inspect(ctx, current.Workspace, image, policy.Ceiling)
	if err != nil {
		return err
	}
	if policy.ExecutableDigest != image.Digest || policy.CompatibilityID != behavior.ID || policy.NativeMode != behavior.Mode || policy.ConsentRevision != project.Revision || policy.HookConfigDigest != project.HookDigest || policy.HelperDigest != project.HelperDigest {
		return antigravityPolicyRefusal("launch_changed")
	}
	latest, err := owner.PolicyForLaunch(ctx, expected.ID)
	if err != nil {
		return err
	}
	if latest != policy {
		return antigravityPolicyRefusal("launch_changed")
	}
	return s.currentAntigravityPolicy(ctx, owner, bridge, inputs)
}
func (s *Server) checkProductionAntigravityPolicy(ctx context.Context, record antigravitylaunch.Record, process ports.AntigravityProcess) error {
	owner, bridge, inputs := s.antigravityLaunchStore.Load(), s.antigravityBridge.Load(), s.antigravityPolicyInputs.Load()
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return err
	}
	if err := s.checkPreparedAntigravityPolicy(ctx, record); err != nil {
		return err
	}
	current, err := owner.PreparedRecord(ctx, record.ID)
	if err != nil {
		return err
	}
	if (current.Process.PID != 0 && current.Process != process) || current.TerminalID != process.TerminalID || current.Backend == "" || process.Executable == "" {
		return antigravityPolicyRefusal("process_unverified")
	}
	plan, err := bridge.ExecutionPlan(ctx, process)
	if err != nil {
		return err
	}
	policy := *current.Policy
	if plan.WorkspaceCWD != current.Workspace || plan.Mode != policy.NativeMode || plan.Model != policy.Model || plan.Effort != policy.Effort || plan.ConversationID != current.RequestedResume {
		return antigravityPolicyRefusal("execution_plan_changed")
	}
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return err
	}
	// ExecutionPlan verifies launch tokens, not mutable effective mode. The
	// final shared policy check selects the same closed enforcement strategy
	// and rereads consent, image and immutable policy after the kernel read.
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return err
	}
	return s.checkPreparedAntigravityPolicy(ctx, current)
}
func (s *Server) checkAntigravityActionPolicy(ctx context.Context, binding ports.AntigravityBinding) error {
	owner := s.antigravityLaunchStore.Load()
	if owner == nil || !binding.Verified {
		return managedAntigravityFailure()
	}
	record, found, err := owner.ManagedRecordForProcess(ctx, binding.Process)
	if err != nil {
		return err
	}
	if !found {
		return nil
	} // positively unowned native observation
	if record.Revoked || record.NativeID != binding.ConversationID {
		return managedAntigravityFailure()
	}
	// An inactive or policy-free managed process cannot borrow unmanaged IO.
	if _, err := owner.LookupProcess(ctx, binding.Process); err != nil {
		return err
	}
	return s.checkProductionAntigravityPolicy(ctx, record, binding.Process)
}
func (s *Server) managedAntigravityToolResponse(ctx context.Context, binding ports.AntigravityBinding) ([]byte, error) {
	owner := s.antigravityLaunchStore.Load()
	if owner == nil || !binding.Verified || binding.Event != "PreToolUse" {
		return nil, managedAntigravityFailure()
	}
	record, found, err := owner.ManagedRecordForProcess(ctx, binding.Process)
	if err != nil {
		return nil, err
	}
	if !found {
		return antigravity.ObserverResponse(binding.Event)
	}
	if err := s.checkAntigravityActionPolicy(ctx, binding); err != nil {
		return nil, err
	}
	inputs, bridge := s.antigravityPolicyInputs.Load(), s.antigravityBridge.Load()
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return nil, err
	}
	image, err := bridge.ImageFingerprint(ctx)
	if err != nil {
		return nil, err
	}
	_, behavior, err := inputs.inspect(ctx, record.Workspace, image, record.Policy.Ceiling)
	if err != nil {
		return nil, err
	}
	// No native tool argument controls this uniform response. Selective
	// exceptions require a separate validated classification contract.
	decision, err := behavior.toolDecision()
	if err != nil {
		return nil, err
	}
	response, err := json.Marshal(struct {
		Decision string `json:"decision"`
	}{decision})
	if err != nil {
		return nil, managedAntigravityFailure()
	}
	if err := s.currentAntigravityPolicy(ctx, owner, bridge, inputs); err != nil {
		return nil, err
	}
	if err := s.checkAntigravityActionPolicy(ctx, binding); err != nil {
		return nil, err
	}
	return response, nil
}
