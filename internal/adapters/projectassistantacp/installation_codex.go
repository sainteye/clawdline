package projectassistantacp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// InstalledProvider is the production factory for both agents: a Claude Code
// Bot runs through installedClaude and a Codex Bot through installedCodex.
// Construction and metadata are inert, as for each agent alone.
func InstalledProvider(state string) app.Provider {
	return &installedAgents{claude: &installedClaude{state: state}, codex: &installedCodex{state: state}}
}

type installedAgents struct {
	claude *installedClaude
	codex  *installedCodex
}

func (p *installedAgents) pick(agent pa.Agent) (app.Provider, error) {
	switch agent {
	case pa.ClaudeCode:
		return p.claude, nil
	case pa.Codex:
		return p.codex, nil
	}
	return nil, refusal(pa.AdapterNotFound, "native_provider")
}

func (p *installedAgents) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	provider, e := p.pick(b.Agent)
	if e != nil {
		return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, NativeMemory: pa.NativeMemoryUnknown}, e
	}
	return provider.Capabilities(ctx, b)
}

func (p *installedAgents) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	provider, e := p.pick(q.Bot.Agent)
	if e != nil {
		return nil, e
	}
	return provider.Open(ctx, q, sink)
}

// installedCodex follows CodyNC as installedClaude does: the person's codex
// runs as the person, in the Bot's Project, with its own tools and login.
// Codex keeps its ChatGPT login in $CODEX_HOME/auth.json, so its home is not
// replaced; native memory is admitted only while codex itself reports its
// memories feature off. Each execution starts a new native session from the
// Host's filtered context: Codex resume has not been proved, so it is off.
type installedCodex struct{ state string }

func (p *installedCodex) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	_, caps, cleanup, e := p.prepare(ctx, b)
	defer cleanup()
	return caps, e
}

func (p *installedCodex) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if q.Bot.Project.Kind == pa.RepositoryProject {
		directory, e := filepath.EvalSymlinks(q.Directory)
		if e != nil {
			return nil, refusal(pa.ScopeDenied, "native_project")
		}
		state, e := filepath.EvalSymlinks(p.state)
		if e != nil {
			return nil, refusal(pa.Invalid, "native_state")
		}
		rel, e := filepath.Rel(directory, state)
		if e != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, refusal(pa.ScopeDenied, "native_state_project_overlap")
		}
	}
	config, caps, cleanup, e := p.prepare(ctx, q.Bot)
	if e != nil {
		cleanup()
		return nil, e
	}
	provider, e := NewProvider(ProviderOptions{Configurations: map[pa.Agent]Configuration{pa.Codex: config}, Limits: nativeLimits(), Verify: func(ctx context.Context, b pa.Bot, _ Configuration) (pa.Capabilities, error) {
		if b.Key != q.Bot.Key || b.Project != q.Bot.Project {
			return caps, refusal(pa.ScopeDenied, "native_configuration")
		}
		if _, e := readAdapter(ctx, p.state, codexSpec); e != nil {
			return caps, e
		}
		return caps, nil
	}})
	if e != nil {
		cleanup()
		return nil, e
	}
	session, e := provider.Open(ctx, q, sink)
	if session == nil {
		cleanup()
		return nil, e
	}
	if e != nil {
		ctx, cancel := context.WithTimeout(context.Background(), nativeLimits().CleanupTimeout)
		defer cancel()
		if closeErr := session.Close(ctx); closeErr != nil {
			return &isolatedNativeSession{ProviderSession: session, cleanup: cleanup}, errors.Join(e, closeErr)
		}
		cleanup()
		return nil, e
	}
	return &isolatedNativeSession{ProviderSession: session, cleanup: cleanup}, nil
}

func (p *installedCodex) prepare(ctx context.Context, b pa.Bot) (Configuration, pa.Capabilities, func(), error) {
	none := func() {}
	caps := pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, NativeMemory: pa.NativeMemoryUnknown}
	if b.Agent != pa.Codex {
		return Configuration{}, caps, none, refusal(pa.AdapterNotFound, "native_provider")
	}
	i, e := ensureAdapter(ctx, p.state, codexSpec)
	if e != nil {
		return Configuration{}, caps, none, e
	}
	state, e := filepath.EvalSymlinks(p.state)
	if e != nil {
		return Configuration{}, caps, none, refusal(pa.Invalid, "native_state")
	}
	home, account, e := nativeAccount()
	if e != nil {
		return Configuration{}, caps, none, e
	}
	path := filepath.Dir(i.tools.Node) + string(os.PathListSeparator) + nativePath
	base := []string{"HOME=" + home, "USER=" + account, "PATH=" + path, "LANG=C", "LC_ALL=C"}
	on, e := codexMemoriesOn(ctx, i.tools.CLI, base)
	if e != nil {
		return Configuration{}, caps, none, e
	}
	if on {
		return Configuration{}, caps, none, refusal(pa.MemoryIsolationUnverified, "codex_memories_enabled")
	}
	root := filepath.Join(state, "project-assistants", "runtime")
	if e = os.MkdirAll(root, 0700); e != nil {
		return Configuration{}, caps, none, refusal(pa.Invalid, "native_state")
	}
	scope, _ := json.Marshal(struct {
		Key     pa.BotKey
		Project pa.ProjectScope
	}{b.Key, b.Project})
	sum := sha256.Sum256(scope)
	owned, e := os.MkdirTemp(root, hex.EncodeToString(sum[:])+"-")
	if e != nil {
		return Configuration{}, caps, none, refusal(pa.Invalid, "native_state")
	}
	cleanup := func() { _ = os.RemoveAll(owned) }
	tmp := filepath.Join(owned, "tmp")
	if e = os.Mkdir(tmp, 0700); e != nil {
		cleanup()
		return Configuration{}, caps, none, refusal(pa.Invalid, "native_state")
	}
	environment := append(base, "TMPDIR="+tmp, "CODEX_PATH="+i.tools.CLI)
	caps.CLIVersion, caps.AdapterVersion = i.tools.CLIVersion, CodexAdapterVersion
	caps.Evidence = "installed codex at or above the supported floor; supplied adapter sealed by digest; codex reports its memories feature off; the person's own Codex home and login"
	caps.NativeMemory, caps.NativeMemoryVerified = pa.NativeMemoryDisabled, true
	caps.Resume = false
	caps.SendMessage = true
	return Configuration{Executable: i.tools.Node, Arguments: []string{i.Adapter}, Environment: environment}, caps, cleanup, nil
}

// codexMemoriesOn reads codex's own effective feature state rather than
// parsing its configuration file: `codex features list` prints one line per
// feature with its stage and effective state.
func codexMemoriesOn(ctx context.Context, cli string, env []string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotToolProbeMilliseconds)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "features", "list")
	cmd.Env, cmd.Stdin = env, nil
	var out bytes.Buffer
	limited := &limitedBuffer{b: &out, left: capacity.MaximumBotFeatureProbeBytes}
	cmd.Stdout = limited
	if e := cmd.Run(); e != nil || limited.over {
		return false, refusal(pa.MemoryIsolationUnverified, "codex_features")
	}
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "memories" {
			switch fields[len(fields)-1] {
			case "false":
				return false, nil
			case "true":
				return true, nil
			}
		}
	}
	return false, refusal(pa.MemoryIsolationUnverified, "codex_features")
}

// CheckCodexInstallation verifies the supplied Codex adapter and the person's
// codex and Node, as CheckClaudeInstallation does for Claude.
func CheckCodexInstallation(ctx context.Context, state string) error {
	_, e := readAdapter(ctx, state, codexSpec)
	return e
}
