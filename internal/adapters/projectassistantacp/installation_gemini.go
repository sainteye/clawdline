package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const geminiAppDir = "antigravity-acp"

// The isolated profile keeps native memories and transcripts inside the Bot's
// privacy epoch. Antigravity can still use the machine's personal OAuth login.
const geminiSettings = `{"useG1Credits":false,"auth":{"type":"oauth-personal"},"toolPermission":"request-review","allowNonWorkspaceAccess":false}`

type installedGemini struct {
	state    string
	resolver *antigravity.Resolver
}

func newInstalledGemini(state string) *installedGemini {
	return &installedGemini{state: state, resolver: antigravity.NewResolver(antigravity.DefaultCandidates(), nil)}
}

func (p *installedGemini) Capabilities(ctx context.Context, bot pa.Bot) (pa.Capabilities, error) {
	_, caps, err := p.prepare(ctx, bot)
	return caps, err
}

func (p *installedGemini) prepare(ctx context.Context, bot pa.Bot) (Configuration, pa.Capabilities, error) {
	caps := pa.Capabilities{Key: bot.Key, Project: bot.Project, Agent: bot.Agent, NativeMemory: pa.NativeMemoryUnknown}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Configuration{}, caps, refusal(pa.Unsupported, "native_platform")
	}
	if bot.Agent != pa.Gemini || bot.LoginProfile != "" {
		return Configuration{}, caps, refusal(pa.Unsupported, "login_profile_agent")
	}
	info := p.resolver.Snapshot(ctx)
	if err := ctx.Err(); err != nil {
		return Configuration{}, caps, err
	}
	switch {
	case !info.InstalledKnown:
		return Configuration{}, caps, refusal(pa.CapacityFull, "native_probe")
	case !info.Installed:
		return Configuration{}, caps, refusal(pa.AdapterNotFound, "gemini_cli")
	case !info.Compatible:
		return Configuration{}, caps, refusal(pa.ProviderVersionUnsupported, "gemini_cli")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return Configuration{}, caps, refusal(pa.Invalid, "native_home")
	}
	config := Configuration{Executable: info.Path, Arguments: []string{
		"acp", "--app-data-dir", geminiAppDir, "--process-per-session", "--sandbox", "--disable-telemetry",
	}, Environment: []string{
		"HOME=" + home, "PATH=" + filepath.Dir(info.Path) + string(os.PathListSeparator) + nativePath,
		"BROWSER=/usr/bin/false", "LANG=C", "LC_ALL=C",
	}}
	caps.CLIVersion, caps.AdapterVersion = info.Version, info.Version
	caps.NativeMemory, caps.NativeMemoryVerified = pa.NativeMemoryIsolated, true
	caps.Resume, caps.SendMessage = true, true
	caps.Evidence = "Antigravity CLI fingerprint; explicit Bot profile; personal OAuth and AI Credits off"
	return config, caps, nil
}

func geminiProject(state, dir string, project pa.ProjectScope) (string, error) {
	state, err := filepath.EvalSymlinks(state)
	if err != nil {
		return "", refusal(pa.Invalid, "native_state")
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil || !filepath.IsAbs(dir) {
		return "", refusal(pa.ScopeDenied, "native_project")
	}
	// Personal workspaces are leaves under the state root, alongside the
	// private profile tree. Admit only the resolved leaf owned by this scope.
	if project.Kind == pa.PersonalWorkspace {
		if pa.ValidateScope(project) != nil || dir != filepath.Join(state, app.PersonalWorkspaceDirectory, project.ProjectID) {
			return "", refusal(pa.ScopeDenied, "native_project")
		}
		return state, nil
	}
	// A Project cannot make this state accessible through its workspace tools.
	for _, paths := range [][2]string{{dir, state}, {state, dir}} {
		rel, err := filepath.Rel(paths[0], paths[1])
		if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return "", refusal(pa.ScopeDenied, "native_state_project_overlap")
		}
	}
	return state, nil
}

func withGeminiProfile(config Configuration, state, profile string) (Configuration, error) {
	settingsDir := filepath.Join(profile, geminiAppDir)
	if err := privateDirectory(state, settingsDir); err != nil {
		return Configuration{}, err
	}
	path := filepath.Join(settingsDir, "settings.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		file, err := os.CreateTemp(settingsDir, "settings-*.json")
		if err != nil {
			return Configuration{}, refusal(pa.Invalid, "gemini_settings")
		}
		defer os.Remove(file.Name())
		_, writeErr := io.WriteString(file, geminiSettings)
		if closeErr := file.Close(); writeErr != nil || closeErr != nil {
			return Configuration{}, refusal(pa.Invalid, "gemini_settings")
		}
		// Publish the complete file once. Never overwrite a changed policy.
		if err = os.Link(file.Name(), path); err != nil && !os.IsExist(err) {
			return Configuration{}, refusal(pa.Invalid, "gemini_settings")
		}
	}
	if err := checkGeminiSettings(path); err != nil {
		return Configuration{}, err
	}
	config.Arguments = append(config.Arguments, "--gemini-dir", profile)
	config.geminiSettings = path
	return config, nil
}

func checkGeminiSettings(path string) error {
	refuse := refusal(pa.MemoryIsolationUnverified, "gemini_settings")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return refuse
	}
	file, err := os.Open(path)
	if err != nil {
		return refuse
	}
	defer file.Close()
	actual, err := io.ReadAll(io.LimitReader(file, int64(len(geminiSettings)+1)))
	if err != nil || string(actual) != geminiSettings {
		return refuse
	}
	return nil
}

func (p *installedGemini) Configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	config, caps, err := p.prepare(ctx, bot)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	state, err := geminiProject(p.state, dir, bot.Project)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	scratch := filepath.Join(state, "project-assistants", "configuration")
	if err := privateDirectory(state, scratch); err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	profile, err := os.MkdirTemp(scratch, "gemini-")
	if err != nil {
		return pa.ConfigurationCatalog{}, refusal(pa.Invalid, "native_directory")
	}
	cleanup := func() { _ = os.RemoveAll(profile) }
	config, err = withGeminiProfile(config, state, profile)
	if err != nil {
		cleanup()
		return pa.ConfigurationCatalog{}, err
	}
	result, err := probeConfiguration(ctx, config, caps, nativeLimits(), bot, dir, model)
	releaseConfiguration(cleanup, err)
	return result, err
}

func (p *installedGemini) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (native app.ProviderSession, err error) {
	configured := false
	defer func() {
		if !configured && err != nil {
			err = &app.OpenNotStarted{Cause: err}
		}
	}()
	if err := pa.ValidateMode(q.Bot.Agent, q.Bot.Mode); err != nil {
		return nil, err
	}
	state, err := geminiProject(p.state, q.Directory, q.Bot.Project)
	if err != nil {
		return nil, err
	}
	config, caps, err := p.prepare(ctx, q.Bot)
	if err != nil {
		return nil, err
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
	config, err = withGeminiProfile(config, state, history.Config)
	if err != nil {
		return nil, err
	}
	provider, err := NewProvider(ProviderOptions{
		Configurations: map[pa.Agent]Configuration{pa.Gemini: config},
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
