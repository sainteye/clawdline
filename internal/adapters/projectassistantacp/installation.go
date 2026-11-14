package projectassistantacp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The adapter version is the one Clawdline supplies (installation_tools.go);
// Claude Code and Node are the person's own, checked against supported floors.
const ClaudeAdapterVersion = "0.106.0"
const claudeEntrySHA = "f766071bfb432f79036ca90c67efc5306373be6b5129a240820d03146d2f4cb9"
const claudeAgentSHA = "811bb2fa230fa43531a2df20996ef710668a2e0723dc0ca9d782d221c221eb97"

// installationFormat 2 seals only the supplied adapter. Format 1 manifests
// pinned the person's Node and CLI digests, which every Claude Code update
// broke; they are replaced by a fresh setup.
const installationFormat = 2

// ClaudeInstallation is local state, not a model-supplied launch. Files seal
// the supplied adapter tree, including additions. The person's tools are
// found again for each execution and never saved. No credential, account
// identity, conversation or user environment is saved.
type ClaudeInstallation struct {
	Format  int               `json:"format"`
	Adapter string            `json:"adapter"`
	Files   map[string]string `json:"files"`
	tools   nativeTools
}

// CLIVersion is the person's Claude Code version found for this setup.
func (i ClaudeInstallation) CLIVersion() string { return i.tools.CLIVersion }

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
	if runtime.GOOS != "darwin" {
		return nil, refusal(pa.Unsupported, "native_platform")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	root, e := i.moduleRoot()
	if e != nil {
		return nil, e
	}
	for path, want := range map[string]string{i.Adapter: claudeEntrySHA, filepath.Join(filepath.Dir(i.Adapter), "v1", "acp-agent.js"): claudeAgentSHA} {
		actual, e := fileDigest(path)
		if e != nil {
			return nil, e
		}
		if actual != want {
			return nil, refusal(pa.ProviderVersionUnsupported, "native_artifact_digest")
		}
	}
	// Hashing just index.js would leave imported dependencies unsealed.
	digest, files, e := adapterTreeDigest(ctx, root)
	if e != nil {
		return nil, e
	}
	if digest != claudeAdapterTreeSHA {
		return nil, refusal(pa.ProviderVersionUnsupported, "native_dependencies_changed")
	}
	return files, nil
}

func nativeAccount() (string, string, error) {
	home, e := os.UserHomeDir()
	if e != nil || !filepath.IsAbs(home) {
		return "", "", refusal(pa.Invalid, "native_home")
	}
	current, e := user.Current()
	if e != nil || current.Username == "" || strings.ContainsAny(current.Username, "=\x00\n") {
		return "", "", refusal(pa.Invalid, "native_account")
	}
	return home, current.Username, nil
}

// SetupClaude finds the person's Claude Code and Node, installs the supplied
// adapter into state when it is absent or changed, and records its seal. It
// does not start the adapter, inspect auth, change feature opt-in or send a
// prompt. Installing the adapter is the only network use: npm fetches the
// lockfile's packages, each checked against the lockfile's integrity.
func SetupClaude(ctx context.Context, state string) (ClaudeInstallation, error) {
	var i ClaudeInstallation
	if runtime.GOOS != "darwin" {
		return i, refusal(pa.Unsupported, "native_platform")
	}
	resolved, e := filepath.EvalSymlinks(state)
	if e != nil {
		return i, refusal(pa.Invalid, "native_state")
	}
	home, account, e := nativeAccount()
	if e != nil {
		return i, e
	}
	tools, e := findTools(ctx, home, account, SearchDirectories(home))
	if e != nil {
		return i, e
	}
	dir, e := installAdapter(ctx, resolved, tools, home, account)
	if e != nil {
		return i, e
	}
	i = ClaudeInstallation{Format: installationFormat, Adapter: adapterEntry(dir), tools: tools}
	files, e := i.artifacts(ctx)
	if e != nil {
		return i, e
	}
	i.Files = files
	data, e := json.MarshalIndent(i, "", "  ")
	if e != nil {
		return i, e
	}
	path := installationPath(resolved)
	f, e := os.CreateTemp(filepath.Dir(path), ".claude-installation-")
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
		e = os.Rename(name, path)
	}
	if e != nil {
		return i, refusal(pa.Invalid, "native_state")
	}
	return i, nil
}

// readInstallation verifies the recorded adapter seal and finds the person's
// tools for this execution. It never installs anything.
func readInstallation(ctx context.Context, state string) (ClaudeInstallation, error) {
	var i ClaudeInstallation
	f, e := os.Open(installationPath(state))
	if e != nil {
		return i, refusal(pa.AdapterNotFound, "native_setup")
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, int64(capacity.MaximumBotFrameBytes)*8))
	if e != nil {
		return i, refusal(pa.Invalid, "native_manifest")
	}
	if json.Unmarshal(data, &i) != nil || len(i.Files) == 0 {
		return i, refusal(pa.Invalid, "native_manifest")
	}
	if i.Format != installationFormat {
		return i, refusal(pa.AdapterNotFound, "native_setup")
	}
	files, e := i.artifacts(ctx)
	if e != nil {
		return i, e
	}
	if !reflect.DeepEqual(files, i.Files) {
		return i, refusal(pa.ProviderVersionUnsupported, "native_dependencies_changed")
	}
	home, account, e := nativeAccount()
	if e != nil {
		return i, e
	}
	if i.tools, e = findTools(ctx, home, account, SearchDirectories(home)); e != nil {
		return i, e
	}
	return i, nil
}

// ensureInstallation sets up on first use, so a person never places files by
// hand. A missing or changed adapter is rebuilt; a missing or too old Claude
// Code or Node is reported, never installed.
func ensureInstallation(ctx context.Context, state string) (ClaudeInstallation, error) {
	i, e := readInstallation(ctx, state)
	if e == nil {
		return i, nil
	}
	var f *pa.Failure
	if !errors.As(e, &f) {
		return i, e
	}
	switch f.Field {
	case "native_setup", "native_manifest", "native_dependencies_changed", "native_artifact_digest", "native_artifact", "native_dependencies":
	default:
		return i, e
	}
	if _, e = SetupClaude(ctx, state); e != nil {
		return i, e
	}
	return readInstallation(ctx, state)
}

// claudeResumeQualified admits native session/load for the pinned artifact
// set under nativeHistoryPolicy. A real cross-process round trip on exactly
// the pinned adapter and Claude Code 2.1.323 passed on 2026-11-14: a new process loaded the same
// native session and answered a code word that only the earlier turn held,
// with no credential-shaped file written and forgotten or foreign bindings
// refused before any native start. Changing the adapter or the history
// policy needs that proof again; scripted peers are not that evidence.
var claudeResumeQualified = true

func nativeLimits() Limits {
	return Limits{RPCPending: capacity.MaximumBotRPCPending, IncomingPermissions: capacity.MaximumBotPermissions, Notifications: capacity.MaximumBotNotifications, FrameBytes: capacity.MaximumBotFrameBytes, CaptureBytes: capacity.MaximumBotCaptureBytes, StderrBytes: capacity.MaximumBotStderrBytes, OwnedDescendants: capacity.MaximumBotOwnedDescendants, WriteTimeout: time.Duration(capacity.MaximumBotWriteMilliseconds) * time.Millisecond, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond, RPCTimeout: time.Duration(capacity.MaximumBotRPCMilliseconds) * time.Millisecond, DescendantPoll: time.Duration(capacity.MaximumBotSurveyMilliseconds) * time.Millisecond}
}

// InstalledClaudeProvider is the production factory. Its construction and all
// metadata operations are inert. Runtime admission sets up the supplied adapter
// on first use and finds the person's tools; only Open starts the adapter.
func InstalledClaudeProvider(state string) app.Provider { return &installedClaude{state: state} }

type installedClaude struct{ state string }

func (p *installedClaude) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	_, caps, cleanup, e := p.prepare(ctx, b, nil)
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
	q.PersistNativeSession = true
	config, caps, cleanup, e := p.prepare(ctx, q.Bot, &q)
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
	if e == nil {
		state, err := filepath.EvalSymlinks(p.state)
		if err == nil {
			h, planErr := historyForRequest(state, q)
			err = planErr
			if err == nil {
				err = h.recordID(session.SessionID())
			}
		}
		if err != nil {
			e = err
		}
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
func (p *installedClaude) prepare(ctx context.Context, b pa.Bot, request *app.OpenRequest) (Configuration, pa.Capabilities, func(), error) {
	caps := pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, NativeMemory: pa.NativeMemoryUnknown}
	if b.Agent != pa.ClaudeCode {
		return Configuration{}, caps, func() {}, refusal(pa.AdapterNotFound, "native_provider")
	}
	i, e := ensureInstallation(ctx, p.state)
	if e != nil {
		return Configuration{}, caps, func() {}, e
	}
	state, e := filepath.EvalSymlinks(p.state)
	if e != nil {
		return Configuration{}, caps, func() {}, refusal(pa.Invalid, "native_state")
	}
	history := ""
	if request != nil {
		h, err := historyForRequest(state, *request)
		if err != nil {
			return Configuration{}, caps, func() {}, err
		}
		if err = privateDirectory(state, h.Config); err == nil {
			err = privateDirectory(state, h.Owners)
		}
		if err == nil && request.Resume {
			err = h.verifyID(request.Binding.NativeSessionID)
		}
		if err != nil {
			return Configuration{}, caps, func() {}, err
		}
		history = h.Config
	}
	config, cleanup, e := i.isolatedWithHistory(ctx, state, b, history)
	if e != nil {
		return config, caps, cleanup, e
	}
	caps.CLIVersion, caps.AdapterVersion = i.tools.CLIVersion, ClaudeAdapterVersion
	caps.Evidence = "installed Claude Code at or above the supported floor; supplied adapter sealed by digest; native tools act in the Project after per-action approval; native config, history and memory isolated per Bot and Project"
	caps.ReadOnlyEnforced, caps.NativeMemoryVerified = false, true
	caps.NativeMemory = pa.NativeMemoryIsolated
	caps.Resume = claudeResumeQualified
	caps.SendMessage = true
	return config, caps, cleanup, nil
}

// CheckClaudeInstallation verifies the supplied adapter and the person's tools.
// Success is deliberately not an auth, session/new, prompt or model receipt.
func CheckClaudeInstallation(ctx context.Context, state string) error {
	i, e := readInstallation(ctx, state)
	if e != nil {
		return e
	}
	_, cleanup, e := i.isolated(ctx, state, pa.Bot{})
	defer cleanup()
	return e
}
