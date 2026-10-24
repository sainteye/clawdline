// Package antigravityproject composes local consent transactions with native
// hook files. It authenticates no person, starts no provider and publishes no
// production permission mode. The transport owns explicit consent and catalog
// membership before calling this private adapter.
package antigravityproject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

// Scope must be shown explicitly by the future consent UI. Catalog membership
// and an active grant do not imply provider-native workspace trust.
const Scope = "project-and-linked-worktrees"

type Error string

func (e Error) Error() string { return string(e) }

const (
	Invalid           Error = "antigravity_project_invalid"
	Unverified        Error = "antigravity_project_unverified"
	Changed           Error = "antigravity_project_changed"
	NotActive         Error = "antigravity_project_not_active"
	HelperUnavailable Error = "antigravity_project_helper_unavailable"
	HooksUnverified   Error = "antigravity_project_hooks_unverified"
)

type consentStore interface {
	AntigravityProjectConsent(context.Context, string) (store.AntigravityProjectConsent, error)
	ApplyAntigravityProjectConsent(context.Context, store.AntigravityConsentCommand) (store.AntigravityProjectConsent, error)
}

// Resolve must prove canonical catalog membership, including linked-worktree
// membership when applicable. A path prefix alone is not that proof.
type Config struct {
	Store              *store.Store
	Resolve            func(context.Context, string) (string, error)
	Helper, ConfigRoot string
	// Nil deliberately leaves revocation available when the helper is missing.
	// Preview, enable, workspace preparation and policy checks then refuse.
	Image *antigravity.PinnedExecutable
}

type Manager struct {
	store              consentStore
	resolve            func(context.Context, string) (string, error)
	helper, configRoot string
	image              *antigravity.PinnedExecutable
	apply              func(antigravity.InstallPreview) error
}

func New(ctx context.Context, cfg Config) (*Manager, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Store == nil || cfg.Resolve == nil || !filepath.IsAbs(cfg.Helper) {
		return nil, Invalid
	}
	if _, err := antigravity.ProviderStateDir(cfg.ConfigRoot); err != nil {
		return nil, Invalid
	}
	return &Manager{store: cfg.Store, resolve: cfg.Resolve, helper: cfg.Helper, configRoot: cfg.ConfigRoot, image: cfg.Image, apply: antigravity.ApplyInstall}, nil
}

// Plan is a private preview, not permission. HTTP must not accept caller-chosen
// roots, helper paths or these fields as a substitute for a catalog lookup.
type Plan struct {
	ProjectRoot, Workspace, BeforeDigest, HelperDigest, ManagedDigest string
	Revision                                                          int64
}

type Outcome struct {
	Consent store.AntigravityProjectConsent
	// unknown includes an unreadable or uncertain filesystem effect.
	Hooks string
}

type Policy struct {
	Consent                                  store.AntigravityProjectConsent
	RevisionDigest, HookDigest, HelperDigest string
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func identity(v string) bool {
	if len(v) == 0 || len(v) > store.AntigravityConsentIdentityBytesLimit {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func (m *Manager) root(ctx context.Context, workspace string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := m.resolve(ctx, workspace)
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", Unverified
	}
	return root, nil
}
func (m *Manager) helperImage(ctx context.Context) (antigravity.ImageFingerprint, error) {
	if m.image == nil {
		return antigravity.ImageFingerprint{}, HelperUnavailable
	}
	image, err := m.image.Fingerprint(ctx)
	if err != nil || image.Executable != m.helper {
		return antigravity.ImageFingerprint{}, HelperUnavailable
	}
	return image, nil
}
func (m *Manager) stable(ctx context.Context, root, workspace string, want store.AntigravityProjectConsent, helper bool) error {
	currentRoot, err := m.root(ctx, workspace)
	if err != nil {
		return err
	}
	if currentRoot != root {
		return Changed
	}
	if helper {
		if _, err := m.helperImage(ctx); err != nil {
			return err
		}
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return err
	}
	if cur != want {
		return Changed
	}
	return ctx.Err()
}

func (m *Manager) Preview(ctx context.Context, workspace string) (Plan, error) {
	root, err := m.root(ctx, workspace)
	if err != nil {
		return Plan{}, err
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Plan{}, err
	}
	image, err := m.helperImage(ctx)
	if err != nil {
		return Plan{}, err
	}
	p, err := antigravity.PreviewInstallForState(workspace, m.helper, m.configRoot, false)
	if err != nil || p.Disabled {
		return Plan{}, HooksUnverified
	}
	managed, err := antigravity.ManagedHookPlanDigest(m.helper, m.configRoot)
	if err != nil {
		return Plan{}, HooksUnverified
	}
	if err := m.stable(ctx, root, workspace, cur, true); err != nil {
		return Plan{}, err
	}
	return Plan{ProjectRoot: root, Workspace: workspace, BeforeDigest: p.BeforeDigest, HelperDigest: image.Digest, ManagedDigest: managed, Revision: cur.Revision}, nil
}

func (m *Manager) Check(ctx context.Context, workspace string) (Policy, error) {
	root, err := m.root(ctx, workspace)
	if err != nil {
		return Policy{}, err
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Policy{}, err
	}
	if cur.Phase != store.AntigravityConsentActive {
		return Policy{}, NotActive
	}
	image, err := m.helperImage(ctx)
	if err != nil {
		return Policy{}, err
	}
	installed, err := antigravity.InspectInstallForState(workspace, m.helper, m.configRoot)
	if err != nil {
		return Policy{}, HooksUnverified
	}
	if image.Digest != cur.HelperSHA256 || installed.PlanDigest != cur.PlanSHA256 {
		return Policy{}, Changed
	}
	if err := m.stable(ctx, root, workspace, cur, true); err != nil {
		return Policy{}, err
	}
	return Policy{Consent: cur, RevisionDigest: digest(struct {
		Root    string
		Consent store.AntigravityProjectConsent
	}{root, cur}), HookDigest: installed.Digest, HelperDigest: image.Digest}, nil
}

// Enable preserves the caller-owned operation identity across lost answers and
// restart. The whole approved plan (including pre-install bytes) is bound into
// the operation stored by SQLite. Replayed answers are never live authority.
func (m *Manager) Enable(ctx context.Context, plan Plan, actor, key, operation string) (Outcome, error) {
	if !identity(actor) || !identity(key) || !identity(operation) || plan.Revision < 0 {
		return Outcome{}, Invalid
	}
	root, err := m.root(ctx, plan.Workspace)
	if err != nil {
		return Outcome{}, err
	}
	if root != plan.ProjectRoot {
		return Outcome{}, Changed
	}
	image, err := m.helperImage(ctx)
	if err != nil {
		return Outcome{}, err
	}
	managed, err := antigravity.ManagedHookPlanDigest(m.helper, m.configRoot)
	if err != nil {
		return Outcome{}, HooksUnverified
	}
	if image.Digest != plan.HelperDigest || managed != plan.ManagedDigest {
		return Outcome{}, Changed
	}
	current, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Outcome{}, err
	}
	if current.Revision == plan.Revision {
		p, err := antigravity.PreviewInstallForState(plan.Workspace, m.helper, m.configRoot, false)
		if err != nil || p.Disabled {
			return Outcome{}, HooksUnverified
		}
		if p.BeforeDigest != plan.BeforeDigest {
			return Outcome{}, Changed
		}
	}
	op := digest(struct {
		Scope     string
		Plan      Plan
		Operation string
	}{Scope, plan, operation})
	startKey := digest([]string{"start", key})
	finishKey := digest([]string{"activate", key})
	cmd := store.AntigravityConsentCommand{ProjectRoot: root, Action: store.AntigravityConsentPrepare, ExpectedRevision: plan.Revision, OperationID: op, HelperSHA256: plan.HelperDigest, PlanSHA256: plan.ManagedDigest, Actor: actor, Key: startKey}
	prepared, err := m.store.ApplyAntigravityProjectConsent(ctx, cmd)
	if err != nil {
		return Outcome{}, err
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Outcome{}, err
	}
	// A completed retry is status-only, never another install or activation.
	if cur.Phase == store.AntigravityConsentActive && cur.OperationID == prepared.OperationID && cur.Revision == prepared.Revision+1 && cur.HelperSHA256 == prepared.HelperSHA256 && cur.PlanSHA256 == prepared.PlanSHA256 {
		checked, err := m.Check(ctx, plan.Workspace)
		if err != nil {
			return Outcome{Consent: cur, Hooks: "unknown"}, err
		}
		if checked.Consent != cur {
			return Outcome{Consent: checked.Consent, Hooks: "unknown"}, Changed
		}
		return Outcome{Consent: cur, Hooks: "installed"}, nil
	}
	if cur != prepared {
		return Outcome{Consent: cur, Hooks: "unknown"}, Changed
	}
	if err := m.stable(ctx, root, plan.Workspace, prepared, true); err != nil {
		return Outcome{Consent: prepared, Hooks: "unknown"}, err
	}
	installed, inspectErr := antigravity.InspectInstallForState(plan.Workspace, m.helper, m.configRoot)
	if inspectErr != nil {
		p, err := antigravity.PreviewInstallForState(plan.Workspace, m.helper, m.configRoot, false)
		if err != nil || p.Disabled {
			return Outcome{Consent: prepared, Hooks: "unknown"}, HooksUnverified
		}
		if p.BeforeDigest != plan.BeforeDigest {
			return Outcome{Consent: prepared, Hooks: "unknown"}, Changed
		}
		if err := m.stable(ctx, root, plan.Workspace, prepared, true); err != nil {
			return Outcome{Consent: prepared, Hooks: "unknown"}, err
		}
		if err := m.apply(p); err != nil {
			return Outcome{Consent: prepared, Hooks: "unknown"}, HooksUnverified
		}
		installed, err = antigravity.InspectInstallForState(plan.Workspace, m.helper, m.configRoot)
		if err != nil {
			return Outcome{Consent: prepared, Hooks: "unknown"}, HooksUnverified
		}
	}
	if installed.PlanDigest != plan.ManagedDigest {
		return Outcome{Consent: prepared, Hooks: "installed"}, Changed
	}
	if err := m.stable(ctx, root, plan.Workspace, prepared, true); err != nil {
		return Outcome{Consent: prepared, Hooks: "installed"}, err
	}
	cmd.Action = store.AntigravityConsentActivate
	cmd.ExpectedRevision = prepared.Revision
	cmd.Key = finishKey
	active, err := m.store.ApplyAntigravityProjectConsent(ctx, cmd)
	if err != nil {
		return Outcome{Consent: prepared, Hooks: "installed"}, err
	}
	checked, err := m.Check(ctx, plan.Workspace)
	if err != nil {
		return Outcome{Consent: active, Hooks: "unknown"}, err
	}
	if checked.Consent != active {
		return Outcome{Consent: checked.Consent, Hooks: "unknown"}, Changed
	}
	return Outcome{Consent: active, Hooks: "installed"}, nil
}

// EnsureWorkspace uses an existing explicitly Project-wide grant, including
// its verified linked worktrees. It creates no new grant/revision or native
// trust and never runs a model. Each workspace's files remain independently
// checked; creating a worktree alone does not make it ready.
func (m *Manager) EnsureWorkspace(ctx context.Context, workspace string) (Policy, error) {
	root, err := m.root(ctx, workspace)
	if err != nil {
		return Policy{}, err
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Policy{}, err
	}
	if cur.Phase != store.AntigravityConsentActive {
		return Policy{}, NotActive
	}
	image, err := m.helperImage(ctx)
	if err != nil {
		return Policy{}, err
	}
	managed, err := antigravity.ManagedHookPlanDigest(m.helper, m.configRoot)
	if err != nil || managed != cur.PlanSHA256 || image.Digest != cur.HelperSHA256 {
		return Policy{}, Changed
	}
	if _, err := antigravity.InspectInstallForState(workspace, m.helper, m.configRoot); err != nil {
		p, err := antigravity.PreviewInstallForState(workspace, m.helper, m.configRoot, false)
		if err != nil || p.Disabled {
			return Policy{}, HooksUnverified
		}
		if err := m.stable(ctx, root, workspace, cur, true); err != nil {
			return Policy{}, err
		}
		if err := m.apply(p); err != nil {
			return Policy{}, HooksUnverified
		}
	}
	checked, err := m.Check(ctx, workspace)
	if err != nil {
		return Policy{}, err
	}
	if checked.Consent != cur {
		return Policy{}, Changed
	}
	return checked, nil
}

// Disable persists loss of authority before any file cleanup. The helper may
// be missing/replaced. Failure to remove files returns disabled + unknown;
// neither an old receipt nor cleanup failure can restore the grant.
func (m *Manager) Disable(ctx context.Context, workspace string, revision int64, actor, key, operation string) (Outcome, error) {
	if !identity(actor) || !identity(key) || !identity(operation) || revision < 0 {
		return Outcome{}, Invalid
	}
	root, err := m.root(ctx, workspace)
	if err != nil {
		return Outcome{}, err
	}
	op := digest(struct {
		Scope, Root, Workspace, Operation string
		Revision                          int64
	}{Scope, root, workspace, operation, revision})
	cmd := store.AntigravityConsentCommand{ProjectRoot: root, Action: store.AntigravityConsentDisable, ExpectedRevision: revision, OperationID: op, Actor: actor, Key: digest([]string{"start", key})}
	disabled, err := m.store.ApplyAntigravityProjectConsent(ctx, cmd)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Consent: disabled, Hooks: "unknown"}
	if err := m.stable(ctx, root, workspace, disabled, false); err != nil {
		return out, err
	}
	if _, err := antigravity.InspectRemovedForState(workspace, m.helper, m.configRoot); err == nil {
		if err := m.stable(ctx, root, workspace, disabled, false); err != nil {
			return out, err
		}
		out.Hooks = "removed"
		return out, nil
	}
	p, err := antigravity.PreviewInstallForState(workspace, m.helper, m.configRoot, true)
	if err != nil || p.Disabled {
		return out, HooksUnverified
	}
	if err := m.stable(ctx, root, workspace, disabled, false); err != nil {
		return out, err
	}
	if err := m.apply(p); err != nil {
		return out, HooksUnverified
	}
	if _, err := antigravity.InspectRemovedForState(workspace, m.helper, m.configRoot); err != nil {
		return out, HooksUnverified
	}
	if err := m.stable(ctx, root, workspace, disabled, false); err != nil {
		return out, err
	}
	out.Hooks = "removed"
	return out, nil
}

// Snapshot is a fresh display projection, never launch authority. A failed
// preview or file inspection remains explicit while durable revocation stays
// available. No path or private operation identity crosses the HTTP boundary.
type Snapshot struct {
	Consent       store.AntigravityProjectConsent
	Preview       *Plan
	Hooks, Reason string
}

func (m *Manager) Status(ctx context.Context, workspace string) (Snapshot, error) {
	root, err := m.root(ctx, workspace)
	if err != nil {
		return Snapshot{}, err
	}
	cur, err := m.store.AntigravityProjectConsent(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Consent: cur, Hooks: "unknown"}
	if p, e := m.Preview(ctx, workspace); e == nil {
		if p.Revision != cur.Revision {
			return Snapshot{}, Changed
		}
		out.Preview = &p
	} else {
		out.Reason = string(HelperUnavailable)
		if e == HooksUnverified {
			out.Reason = string(HooksUnverified)
		}
	}
	if cur.Phase == store.AntigravityConsentActive {
		checked, e := m.Check(ctx, workspace)
		if e == nil && checked.Consent == cur {
			out.Hooks = "installed"
		} else {
			out.Reason = string(HooksUnverified)
		}
	} else if _, e := antigravity.InspectRemovedForState(workspace, m.helper, m.configRoot); e == nil {
		out.Hooks = "removed"
	}
	if err := m.stable(ctx, root, workspace, cur, false); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}
