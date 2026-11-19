package projectassistantacp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type installedCodexMaintenance struct {
	codex           installedCodex
	binding         pa.ExecutionBinding
	profileRevision int64
	cliVersion      string
	inspect         codexAdapterLoader
}

// InstalledCodexMaintenanceProvider takes the exact binding and account epoch
// and CLI version already qualified by the source. A current profile alone cannot
// establish which account created previously stored native history. This
// factory is not registered as the chat or production maintenance provider.
func InstalledCodexMaintenanceProvider(state string, profiles app.LoginProfiles, binding pa.ExecutionBinding, profileRevision int64, cliVersion string) (app.Provider, error) {
	if err := pa.CheckFence(binding.Fence, binding.Fence); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(state) || profiles == nil || binding.Agent != pa.Codex || binding.Revoked ||
		binding.SessionSlot != "" || !nativeSessionUUID.MatchString(binding.NativeSessionID) ||
		binding.InstructionRevision <= 0 || binding.MemoryRevision <= 0 || binding.ForgottenThrough < 0 || binding.ForgottenThrough > binding.MemoryRevision ||
		binding.LoginProfile == "" || !pa.ValidLoginProfileID(binding.LoginProfile) || profileRevision <= 0 || strings.TrimSpace(cliVersion) == "" {
		return nil, refusal(pa.Invalid, "native_maintenance_identity")
	}
	return &installedCodexMaintenance{
		codex: installedCodex{state: state, profiles: profiles}, binding: binding,
		profileRevision: profileRevision, cliVersion: cliVersion, inspect: readAdapter,
	}, nil
}

func (p *installedCodexMaintenance) checkProfile(ctx context.Context, b pa.Bot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fence := p.binding.Fence
	if b.Key != fence.Key || b.Project != fence.Project || b.ConversationID != fence.ConversationID ||
		b.Agent != p.binding.Agent || b.LoginProfile != p.binding.LoginProfile || b.Instructions.Revision != p.binding.InstructionRevision {
		return refusal(pa.ScopeDenied, "native_maintenance_binding")
	}
	profiles, err := p.codex.profiles.List(ctx, pa.Codex)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if profile.ID == b.LoginProfile && profile.Agent == pa.Codex && profile.Kind == "account" &&
			profile.State == "signed_in" && profile.Revision == p.profileRevision {
			return nil
		}
	}
	return refusal(pa.Revoked, "native_maintenance_account")
}

func (p *installedCodexMaintenance) prepare(ctx context.Context, b pa.Bot) (Configuration, pa.Capabilities, func(), error) {
	none := func() {}
	if err := p.checkProfile(ctx, b); err != nil {
		return Configuration{}, pa.Capabilities{}, none, err
	}
	config, caps, cleanup, err := p.codex.prepareWithAdapter(ctx, b, p.inspect)
	if err != nil {
		return config, caps, cleanup, err
	}
	// The second check runs while the acquired native account hold still
	// protects the profile used by the process and probes.
	if err = p.checkProfile(ctx, b); err != nil {
		cleanup()
		return Configuration{}, pa.Capabilities{}, none, err
	}
	if caps.CLIVersion != p.cliVersion {
		cleanup()
		return Configuration{}, pa.Capabilities{}, none, refusal(pa.ProviderVersionUnsupported, "native_maintenance_version")
	}
	caps.SendMessage = false
	caps.Evidence = "qualified managed Codex maintenance account, verified adapter seal and native memory off"
	return config, caps, cleanup, nil
}

func (p *installedCodexMaintenance) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	_, caps, cleanup, err := p.prepare(ctx, b)
	cleanup()
	return caps, err
}

func (p *installedCodexMaintenance) Open(ctx context.Context, q app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	refused := func(err error) (app.ProviderSession, error) { return nil, &app.OpenNotStarted{Cause: err} }
	if !q.Resume || q.Binding != p.binding || q.PrimaryAgent != pa.Codex || !filepath.IsAbs(q.Directory) ||
		q.Tools != nil || q.Extensions.Skills != "" || len(q.Extensions.Connectors) != 0 ||
		len(q.Extensions.Folders) != 0 || q.Extensions.State != "" {
		return refused(refusal(pa.ScopeDenied, "native_maintenance_open"))
	}
	if err := p.checkProfile(ctx, q.Bot); err != nil {
		return refused(err)
	}
	state, err := filepath.EvalSymlinks(p.codex.state)
	if err != nil {
		return refused(refusal(pa.Invalid, "native_state"))
	}
	project, err := filepath.EvalSymlinks(q.Directory)
	if err != nil {
		return refused(refusal(pa.ScopeDenied, "native_project"))
	}
	rel, err := filepath.Rel(project, state)
	if err != nil || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return refused(refusal(pa.ScopeDenied, "native_state_project_overlap"))
	}
	history, err := historyForRequest(state, q)
	if err != nil {
		return refused(err)
	}
	if owners, err := filepath.EvalSymlinks(history.Owners); err != nil || owners != history.Owners {
		return refused(refusal(pa.ResumeUnsupported, "native_history_owner"))
	}
	if err := history.verifyID(q.Binding.NativeSessionID); err != nil {
		return refused(err)
	}

	config, caps, cleanup, err := p.prepare(ctx, q.Bot)
	if err != nil {
		cleanup()
		return refused(err)
	}
	if err := history.verifyID(q.Binding.NativeSessionID); err != nil {
		cleanup()
		return refused(err)
	}
	limits := nativeLimits()
	proc, err := StartProcess(ctx, ProcessOptions{Configuration: config, Directory: project, Limits: limits})
	if err != nil {
		// Initial process discovery can fail after spawn and without returning
		// a cleanup handle. Keep the account hold when that cleanup is unknown.
		if code := pa.Code(err); code == pa.ExecutionEffectUnknown || code == pa.CapacityFull {
			return nil, err
		}
		cleanup()
		return refused(err)
	}
	c, err := NewClient(proc, limits, q.Binding.Fence, func(context.Context, app.ProtocolEvent) error {
		return refusal(pa.ScopeDenied, "native_maintenance_event")
	}, nil)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), limits.CleanupTimeout)
		defer cancel()
		if closeErr := proc.Close(closeCtx); closeErr != nil {
			return nil, errors.Join(refusal(pa.ExecutionEffectUnknown, "native_maintenance_cleanup"), err, closeErr)
		}
		cleanup()
		return refused(err)
	}
	c.adapterVersion = caps.AdapterVersion
	s := &codexMaintenanceSession{
		isolatedNativeSession: &isolatedNativeSession{ProviderSession: c, cleanup: cleanup},
		check:                 func(ctx context.Context) error { return p.checkProfile(ctx, q.Bot) },
	}
	q.Directory, q.PersistNativeSession = project, true
	err = c.initializeSession(ctx, q, true)
	if err == nil {
		err = s.check(ctx)
	}
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), limits.CleanupTimeout)
		defer cancel()
		// Keep the handle for the Host even if our first cleanup succeeds.
		return s, errors.Join(err, s.Close(closeCtx))
	}
	return s, nil
}

type codexMaintenanceSession struct {
	*isolatedNativeSession
	check func(context.Context) error
}

func (*codexMaintenanceSession) Send(context.Context, app.PromptRequest) (app.PendingPrompt, error) {
	return nil, refusal(pa.ScopeDenied, "native_maintenance_chat")
}
func (*codexMaintenanceSession) Answer(context.Context, string, string) error {
	return refusal(pa.ScopeDenied, "native_maintenance_permission")
}
func (*codexMaintenanceSession) Steer(context.Context, string) (app.SteerOutcome, error) {
	return "", refusal(pa.ScopeDenied, "native_maintenance_chat")
}
func (s *codexMaintenanceSession) BeginCompaction(ctx context.Context, fence pa.Fence) (app.PendingNativeCompaction, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	return s.isolatedNativeSession.BeginCompaction(ctx, fence)
}
