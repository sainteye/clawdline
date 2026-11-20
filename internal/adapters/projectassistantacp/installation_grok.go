package projectassistantacp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/grok"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type installedGrok struct {
	state    string
	resolver *antigravity.Resolver
}

func newInstalledGrok(state string) *installedGrok {
	return &installedGrok{state: state, resolver: grok.NewResolver(grok.DefaultCandidates(), nil)}
}

func (p *installedGrok) prepare(ctx context.Context, b pa.Bot) (Configuration, pa.Capabilities, error) {
	caps := pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, NativeMemory: pa.NativeMemoryUnknown}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Configuration{}, caps, refusal(pa.Unsupported, "native_platform")
	}
	if b.Agent != pa.Grok {
		return Configuration{}, caps, refusal(pa.Unsupported, "agent")
	}
	if b.LoginProfile != "" {
		return Configuration{}, caps, refusal(pa.Unsupported, "login_profile_agent")
	}
	info := p.resolver.Snapshot(ctx)
	if err := ctx.Err(); err != nil {
		return Configuration{}, caps, err
	}
	if !info.InstalledKnown {
		return Configuration{}, caps, refusal(pa.CapacityFull, "native_probe")
	}
	if !info.Installed || !info.Compatible {
		code := pa.AdapterNotFound
		if info.Installed {
			code = pa.ProviderVersionUnsupported
		}
		return Configuration{}, caps, refusal(code, "grok_cli")
	}
	home, err := os.UserHomeDir()
	account := grok.AccountDir()
	if err != nil || !filepath.IsAbs(home) || !filepath.IsAbs(account) {
		return Configuration{}, caps, refusal(pa.Invalid, "native_home")
	}
	caps.CLIVersion = info.Version
	// Grok is the ACP implementation itself. Its protocol identity is checked
	// at initialize, before authenticating or opening a native session.
	caps.AdapterVersion = info.Version
	caps.Evidence = "Grok Build CLI fingerprint; process-wide native memory disabled; ACP checks for a cached personal login when opening a session"
	caps.NativeMemory, caps.NativeMemoryVerified = pa.NativeMemoryDisabled, true
	caps.Resume = true
	caps.SendMessage = true
	return Configuration{
		Executable: info.Path,
		Arguments:  []string{"--permission-mode", pa.NativeMode(b.Agent, b.Mode), "agent", "--no-leader", "stdio"},
		Environment: []string{
			"HOME=" + home, "GROK_HOME=" + account, "PATH=" + filepath.Dir(info.Path) + string(os.PathListSeparator) + nativePath,
			"GROK_MEMORY=0", "LANG=C", "LC_ALL=C",
		},
	}, caps, nil
}

func (p *installedGrok) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	_, caps, err := p.prepare(ctx, b)
	return caps, err
}

func (p *installedGrok) Configuration(ctx context.Context, b pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	config, caps, err := p.prepare(ctx, b)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return probeConfiguration(ctx, config, caps, nativeLimits(), b, dir, model)
}

func (p *installedGrok) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (native app.ProviderSession, err error) {
	configured := false
	defer func() {
		if !configured && err != nil {
			err = &app.OpenNotStarted{Cause: err}
		}
	}()
	if err := pa.ValidateMode(q.Bot.Agent, q.Bot.Mode); err != nil {
		return nil, err
	}
	state, err := filepath.EvalSymlinks(p.state)
	if err != nil {
		return nil, refusal(pa.Invalid, "native_state")
	}
	dir, err := filepath.EvalSymlinks(q.Directory)
	if err != nil || !filepath.IsAbs(dir) {
		return nil, refusal(pa.ScopeDenied, "native_project")
	}
	rel, err := filepath.Rel(dir, state)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil, refusal(pa.ScopeDenied, "native_state_project_overlap")
	}
	history, err := historyForRequest(state, q)
	if err == nil {
		err = privateDirectory(state, history.Owners)
	}
	if err == nil && q.Resume {
		err = history.verifyID(q.Binding.NativeSessionID)
	}
	if err != nil {
		return nil, err
	}
	config, caps, err := p.prepare(ctx, q.Bot)
	if err != nil {
		return nil, err
	}
	provider, err := NewProvider(ProviderOptions{
		Configurations: map[pa.Agent]Configuration{pa.Grok: config},
		Limits:         nativeLimits(),
		Verify:         func(context.Context, pa.Bot, Configuration) (pa.Capabilities, error) { return caps, nil },
	})
	if err != nil {
		return nil, err
	}
	q.PersistNativeSession = true
	configured = true
	native, err = provider.Open(ctx, q, sink)
	if err != nil {
		return native, err
	}
	if err = history.recordID(native.SessionID()); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), nativeLimits().CleanupTimeout)
		defer cancel()
		if closeErr := native.Close(cleanup); closeErr != nil {
			return native, errors.Join(refusal(pa.ExecutionEffectUnknown, "owned_cleanup"), err, closeErr)
		}
		return native, err
	}
	return native, nil
}
