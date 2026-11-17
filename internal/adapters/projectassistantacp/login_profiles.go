package projectassistantacp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type profileEnvKey struct{}

func withProfileEnvironment(ctx context.Context, env []string) context.Context {
	return context.WithValue(ctx, profileEnvKey{}, env)
}
func profileEnvironment(ctx context.Context) []string {
	v, _ := ctx.Value(profileEnvKey{}).([]string)
	return v
}

func profileHold(ctx context.Context, profiles app.LoginProfiles, b pa.Bot) (context.Context, []string, func(), error) {
	if b.LoginProfile == "" {
		return ctx, nil, func() {}, nil
	}
	if profiles == nil {
		return ctx, nil, func() {}, refusal(pa.Unsupported, "login_profiles")
	}
	env, release, e := profiles.Acquire(ctx, b)
	return withProfileEnvironment(ctx, env), env, release, e
}

// InstalledLoginNative runs only the two supported CLI account protocols.
// stdout/stderr are never surfaced: browser callbacks stay native-owned.
type InstalledLoginNative struct {
	// Resolve is a trusted composition seam for owned native fixtures, never HTTP input.
	Resolve func(context.Context, pa.Agent, []string) (Configuration, error)
}

func (n InstalledLoginNative) configuration(ctx context.Context, agent pa.Agent, env []string) (Configuration, error) {
	if n.Resolve != nil {
		return n.Resolve(ctx, agent, env)
	}
	return loginConfiguration(ctx, agent, env)
}
func loginConfiguration(ctx context.Context, agent pa.Agent, env []string) (Configuration, error) {
	home, user, e := nativeAccount()
	if e != nil {
		return Configuration{}, e
	}
	spec := claudeSpec
	if agent == pa.Codex {
		spec = codexSpec
	} else if agent != pa.ClaudeCode {
		return Configuration{}, refusal(pa.Unsupported, "login_profile_agent")
	}
	tools, e := findToolsFor(withProfileEnvironment(ctx, env), home, user, SearchDirectories(home), spec)
	if e != nil {
		return Configuration{}, e
	}
	base := []string{"HOME=" + home, "USER=" + user, "PATH=" + filepath.Dir(tools.Node) + string(os.PathListSeparator) + nativePath, "LANG=C", "LC_ALL=C", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1"}
	return Configuration{Executable: tools.CLI, Environment: append(base, env...)}, nil
}

type loginProcess struct{ p *Process }

func (p *loginProcess) Wait(ctx context.Context) error {
	if !waitOwnedDone(ctx, p.p.done) {
		return refusal(pa.ExecutionEffectUnknown, "login_profile_wait")
	}
	p.p.mu.Lock()
	defer p.p.mu.Unlock()
	if p.p.waitErr != nil {
		return refusal(pa.AuthRejected, "login_profile_login")
	}
	return nil
}
func (p *loginProcess) Close(ctx context.Context) error { return p.p.Close(ctx) }
func (n InstalledLoginNative) Start(ctx context.Context, agent pa.Agent, env []string) (app.LoginProcess, error) {
	c, e := n.configuration(ctx, agent, env)
	if e != nil {
		return nil, e
	}
	args := []string{"auth", "login"}
	if agent == pa.Codex {
		args = []string{"login"}
	}
	c.Arguments = append(c.Arguments, args...)
	// The slot is resolved by the registry. HOME remains the person's home.
	p, e := StartProcess(ctx, ProcessOptions{Configuration: c, Directory: os.TempDir(), Limits: nativeLimits()})
	if e != nil {
		return nil, e
	}
	go func() { _, _ = io.Copy(io.Discard, p.stdout) }()
	return &loginProcess{p}, nil
}
func (n InstalledLoginNative) Status(ctx context.Context, agent pa.Agent, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotToolProbeMilliseconds)*time.Millisecond)
	defer cancel()
	c, e := n.configuration(ctx, agent, env)
	if e != nil {
		return "unknown", e
	}
	args := []string{"auth", "status", "--json"}
	if agent == pa.Codex {
		args = []string{"login", "status"}
	}
	c.Arguments = append(c.Arguments, args...)
	data, exit, e := runLoginCommand(ctx, c, true)
	if e != nil {
		return "unknown", e
	}
	if agent == pa.ClaudeCode {
		var v struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal(data, &v) != nil || v.LoggedIn == nil {
			return "unknown", refusal(pa.AuthProbeUnknown, "login_profile_status")
		}
		if *v.LoggedIn && exit == 0 {
			return "signed_in", nil
		}
		if !*v.LoggedIn {
			return "signed_out", nil
		}
		return "unknown", refusal(pa.AuthProbeUnknown, "login_profile_status")
	}
	if exit == 0 {
		return "signed_in", nil
	}
	if exit == 1 {
		return "signed_out", nil
	}
	return "unknown", refusal(pa.AuthProbeUnknown, "login_profile_status")
}

func InstalledProviderWithProfiles(state string, profiles app.LoginProfiles) app.Provider {
	return &installedAgents{claude: &installedClaude{state: state, profiles: profiles}, codex: &installedCodex{state: state, profiles: profiles}}
}

func (n InstalledLoginNative) SignOut(ctx context.Context, agent pa.Agent, env []string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotToolProbeMilliseconds)*time.Millisecond)
	defer cancel()
	c, e := n.configuration(ctx, agent, env)
	if e != nil {
		return e
	}
	args := []string{"auth", "logout"}
	if agent == pa.Codex {
		args = []string{"logout"}
	}
	c.Arguments = append(c.Arguments, args...)
	_, exit, e := runLoginCommand(ctx, c, false)
	if e != nil || exit != 0 {
		return refusal(pa.ExecutionEffectUnknown, "login_profile_sign_out")
	}
	return nil
}

func runLoginCommand(ctx context.Context, c Configuration, capture bool) ([]byte, int, error) {
	p, e := StartProcess(ctx, ProcessOptions{Configuration: c, Directory: os.TempDir(), Limits: nativeLimits()})
	if e != nil {
		return nil, -1, e
	}
	var out bytes.Buffer
	limited := &limitedBuffer{b: &out, left: capacity.MaximumBotFeatureProbeBytes}
	var writer io.Writer = io.Discard
	if capture {
		writer = limited
	}
	read := make(chan struct{})
	go func() { _, _ = io.Copy(writer, p.stdout); close(read) }()
	done := waitOwnedDone(ctx, p.done)
	if done {
		done = waitOwnedDone(ctx, read)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), nativeLimits().CleanupTimeout)
	defer cancel()
	if e = p.Close(cleanup); e != nil || !done || limited.over {
		return nil, -1, refusal(pa.ExecutionEffectUnknown, "login_profile_probe_cleanup")
	}
	p.mu.Lock()
	waitErr := p.waitErr
	p.mu.Unlock()
	exit := 0
	if waitErr != nil {
		if code, ok := waitErr.(*exec.ExitError); ok {
			exit = code.ExitCode()
		} else {
			return nil, -1, refusal(pa.AuthProbeUnknown, "login_profile_status")
		}
	}
	return out.Bytes(), exit, nil
}

func nativeProfileOnly(env []string) []string {
	out := []string{}
	for _, v := range env {
		if strings.HasPrefix(v, "CODEX_HOME=") || strings.HasPrefix(v, "CLAUDE_CONFIG_DIR=") {
			out = append(out, v)
		}
	}
	return out
}
