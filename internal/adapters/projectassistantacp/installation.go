package projectassistantacp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This first native route deliberately supports one qualified artifact set.
// Configuration records existing files; it never installs or updates a CLI.
const ClaudeAdapterVersion = "0.106.0"
const ClaudeCLIVersion = "2.1.323"
const claudeNodeSHA = "59450bb6448c8a40b3f3b86da45c3babb2e0503e04c47e5a715e8e137389878b"
const claudeCLISHA = "5432918d904a9b6afef7bb79d20df24d76052eabc1f41bceb84091d21f8889a7"
const claudeEntrySHA = "f766071bfb432f79036ca90c67efc5306373be6b5129a240820d03146d2f4cb9"
const claudeAgentSHA = "811bb2fa230fa43531a2df20996ef710668a2e0723dc0ca9d782d221c221eb97"

// ClaudeInstallation is local owner configuration, not a model-supplied launch.
// Digests seal all dependency files, including additions, after explicit setup.
// No credential, account identity, conversation or user environment is saved.
type ClaudeInstallation struct {
	Node    string            `json:"node"`
	CLI     string            `json:"cli"`
	Adapter string            `json:"adapter"`
	Files   map[string]string `json:"files"`
}

func installationPath(state string) string {
	return filepath.Join(state, "project-assistants", "claude-installation.json")
}

func fileDigest(path string) (string, error) {
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() {
		return "", refusal(pa.Invalid, "native_artifact")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", refusal(pa.AdapterNotFound, "native_artifact")
	}
	defer f.Close()
	info, e = f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return "", refusal(pa.Invalid, "native_artifact")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", refusal(pa.AdapterNotFound, "native_artifact")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (i ClaudeInstallation) moduleRoot() (string, error) {
	suffix := filepath.Join("node_modules", "@agentclientprotocol", "claude-agent-acp", "dist", "index.js")
	if !filepath.IsAbs(i.Adapter) || !strings.HasSuffix(i.Adapter, string(filepath.Separator)+suffix) {
		return "", refusal(pa.Invalid, "native_adapter_path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(i.Adapter)))), nil
}

func (i ClaudeInstallation) artifacts(ctx context.Context) (map[string]string, error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return nil, refusal(pa.Unsupported, "native_platform")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	root, e := i.moduleRoot()
	if e != nil {
		return nil, e
	}
	for _, executable := range []string{i.Node, i.CLI} {
		info, e := os.Stat(executable)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return nil, refusal(pa.AdapterNotFound, "native_executable")
		}
	}
	out := map[string]string{}
	for path, want := range map[string]string{i.Node: claudeNodeSHA, i.CLI: claudeCLISHA, i.Adapter: claudeEntrySHA, filepath.Join(filepath.Dir(i.Adapter), "v1", "acp-agent.js"): claudeAgentSHA} {
		if !filepath.IsAbs(path) {
			return nil, refusal(pa.Invalid, "native_artifact")
		}
		actual, e := fileDigest(path)
		if e != nil {
			return nil, e
		}
		if actual != want {
			return nil, refusal(pa.ProviderVersionUnsupported, "native_artifact_digest")
		}
		out[path] = actual
	}
	// A local owner chooses the installation. Subsequent additions or edits are
	// refused; hashing just index.js would leave imported dependencies unsealed.
	e = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if walkErr != nil {
			return refusal(pa.AdapterNotFound, "native_dependencies")
		}
		if d.IsDir() {
			return nil
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil {
			return refusal(pa.Invalid, "native_dependency_link")
		}
		rel, e := filepath.Rel(root, resolved)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return refusal(pa.ScopeDenied, "native_dependency_link")
		}
		digest, e := fileDigest(path)
		if e != nil {
			return e
		}
		out[path] = digest
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// ConfigureClaude pins an already installed, qualified adapter. It does not
// start Node/Claude, inspect auth, change feature opt-in, or contact a service.
func ConfigureClaude(ctx context.Context, state, node, adapter, cli string) (ClaudeInstallation, error) {
	var err error
	node, err = filepath.EvalSymlinks(node)
	if err != nil {
		return ClaudeInstallation{}, refusal(pa.AdapterNotFound, "native_artifact")
	}
	adapter, err = filepath.EvalSymlinks(adapter)
	if err != nil {
		return ClaudeInstallation{}, refusal(pa.AdapterNotFound, "native_artifact")
	}
	cli, err = filepath.EvalSymlinks(cli)
	if err != nil {
		return ClaudeInstallation{}, refusal(pa.AdapterNotFound, "native_artifact")
	}
	i := ClaudeInstallation{Node: node, Adapter: adapter, CLI: cli}
	files, e := i.artifacts(ctx)
	if e != nil {
		return i, e
	}
	i.Files = files
	data, e := json.MarshalIndent(i, "", "  ")
	if e != nil {
		return i, e
	}
	dir := filepath.Dir(installationPath(state))
	if e = os.MkdirAll(dir, 0700); e != nil {
		return i, refusal(pa.Invalid, "native_state")
	}
	f, e := os.CreateTemp(dir, ".claude-installation-")
	if e != nil {
		return i, refusal(pa.Invalid, "native_state")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(append(data, '\n')); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, installationPath(state))
	}
	if e != nil {
		return i, refusal(pa.Invalid, "native_state")
	}
	return i, nil
}

func readInstallation(ctx context.Context, state string) (ClaudeInstallation, error) {
	var i ClaudeInstallation
	f, e := os.Open(installationPath(state))
	if e != nil {
		return i, refusal(pa.AdapterNotFound, "native_setup")
	}
	defer f.Close()
	data, e := io.ReadAll(f)
	if e != nil {
		return i, refusal(pa.Invalid, "native_manifest")
	}
	if json.Unmarshal(data, &i) != nil || len(i.Files) == 0 {
		return i, refusal(pa.Invalid, "native_manifest")
	}
	files, e := i.artifacts(ctx)
	if e != nil {
		return i, e
	}
	if !reflect.DeepEqual(files, i.Files) {
		return i, refusal(pa.ProviderVersionUnsupported, "native_dependencies_changed")
	}
	return i, nil
}

func nativeLimits() Limits {
	return Limits{RPCPending: capacity.MaximumBotRPCPending, IncomingPermissions: capacity.MaximumBotPermissions, Notifications: capacity.MaximumBotNotifications, FrameBytes: capacity.MaximumBotFrameBytes, CaptureBytes: capacity.MaximumBotCaptureBytes, StderrBytes: capacity.MaximumBotStderrBytes, OwnedDescendants: capacity.MaximumBotOwnedDescendants, WriteTimeout: time.Duration(capacity.MaximumBotWriteMilliseconds) * time.Millisecond, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond, RPCTimeout: time.Duration(capacity.MaximumBotRPCMilliseconds) * time.Millisecond, DescendantPoll: time.Duration(capacity.MaximumBotSurveyMilliseconds) * time.Millisecond}
}

// InstalledClaudeProvider is the production factory. Its construction and all
// metadata operations are inert. Only runtime admission reads pinned files and
// verifies the local OS boundary; only Open starts the native adapter.
func InstalledClaudeProvider(state string) app.Provider { return &installedClaude{state: state} }

type installedClaude struct{ state string }

func (p *installedClaude) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	_, caps, cleanup, e := p.prepare(ctx, b)
	defer cleanup()
	return caps, e
}
func (p *installedClaude) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	// Native scratch output must not become a write exception inside a repository.
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
	provider, e := NewProvider(ProviderOptions{Configurations: map[pa.Agent]Configuration{pa.ClaudeCode: config}, Limits: nativeLimits(), Verify: func(ctx context.Context, b pa.Bot, _ Configuration) (pa.Capabilities, error) {
		if b.Key != q.Bot.Key || b.Project != q.Bot.Project {
			return caps, refusal(pa.ScopeDenied, "native_configuration")
		}
		if _, e := readInstallation(ctx, p.state); e != nil {
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
	return &isolatedNativeSession{ProviderSession: session, cleanup: cleanup}, e
}
func (p *installedClaude) prepare(ctx context.Context, b pa.Bot) (Configuration, pa.Capabilities, func(), error) {
	caps := pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, NativeMemory: pa.NativeMemoryUnknown}
	if b.Agent != pa.ClaudeCode {
		return Configuration{}, caps, func() {}, refusal(pa.AdapterNotFound, "native_provider")
	}
	i, e := readInstallation(ctx, p.state)
	if e != nil {
		return Configuration{}, caps, func() {}, e
	}
	config, cleanup, e := i.isolated(ctx, p.state, b, nativeLimits())
	if e != nil {
		return config, caps, cleanup, e
	}
	caps.CLIVersion, caps.AdapterVersion = ClaudeCLIVersion, ClaudeAdapterVersion
	caps.Evidence = "pinned artifacts and live OS write/exec/memory controls; no authentication or model assertion"
	caps.ReadOnlyEnforced, caps.NativeMemoryVerified = true, true
	caps.NativeMemory = pa.NativeMemoryDisabled
	// Native transcripts are not written into the person's CLI directory. The
	// app's durable context reconstructs a fresh native session on restart.
	caps.Resume = false
	caps.SendMessage = true
	return config, caps, cleanup, nil
}

// CheckClaudeInstallation performs only local artifact and kernel controls.
// Success is deliberately not an auth, session/new, prompt or model receipt.
func CheckClaudeInstallation(ctx context.Context, state string) error {
	i, e := readInstallation(ctx, state)
	if e != nil {
		return e
	}
	_, cleanup, e := i.isolated(ctx, state, pa.Bot{}, nativeLimits())
	defer cleanup()
	return e
}
