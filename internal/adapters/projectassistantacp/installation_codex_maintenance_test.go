package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type maintenanceProfiles struct {
	app.LoginProfiles
	mu              sync.Mutex
	current         pa.LoginProfile
	holds           int
	changeOnAcquire bool
	env             []string
}

func (r *maintenanceProfiles) List(context.Context, pa.Agent) ([]pa.LoginProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []pa.LoginProfile{r.current}, nil
}

func (r *maintenanceProfiles) Acquire(context.Context, pa.Bot) ([]string, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holds++
	if r.changeOnAcquire {
		r.current.Revision++
	}
	return r.env, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.holds--
	}, nil
}

func (r *maintenanceProfiles) rotate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Revision++
}

func (r *maintenanceProfiles) active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.holds
}

type codexMaintenanceFixture struct {
	p        *installedCodexMaintenance
	profiles *maintenanceProfiles
	request  app.OpenRequest
	frames   string
	owner    string
}

func newCodexMaintenanceFixture(t *testing.T, mode string) codexMaintenanceFixture {
	t.Helper()
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fence := testFence()
	profile := pa.LoginProfile{ID: strings.Repeat("b", 32), Agent: pa.Codex, Kind: "account", State: "signed_in", Revision: 7}
	binding := pa.ExecutionBinding{Fence: fence, Agent: pa.Codex, LoginProfile: profile.ID,
		NativeSessionID: uuid.NewString(), MemoryRevision: 3, InstructionRevision: 4}
	bot := pa.Bot{Key: fence.Key, Project: fence.Project, ConversationID: fence.ConversationID, Agent: pa.Codex,
		LoginProfile: profile.ID, Instructions: pa.InstructionSnapshot{Revision: 4, Text: "Synthetic chat instructions"},
		Mode: "plan"}
	req := app.OpenRequest{Bot: bot, Binding: binding, Resume: true, PrimaryAgent: pa.Codex, Directory: project}
	history, err := historyForRequest(state, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateDirectory(state, history.Owners); err != nil {
		t.Fatal(err)
	}
	if err := history.recordID(binding.NativeSessionID); err != nil {
		t.Fatal(err)
	}
	frames := filepath.Join(state, "frames.jsonl")
	registry := &maintenanceProfiles{current: profile, env: []string{
		"HOME=" + t.TempDir(), "CODEX_HOME=" + t.TempDir(), "GORACE=atexit_sleep_ms=0",
		"CLAWDLINE_MAINTENANCE_TEST_PEER=" + frames, "CLAWDLINE_MAINTENANCE_TEST_MODE=" + mode,
		"CLAWDLINE_MAINTENANCE_TEST_ID=" + binding.NativeSessionID,
	}}
	provider, err := InstalledCodexMaintenanceProvider(state, registry, binding, profile.Revision, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	return codexMaintenanceFixture{p: provider.(*installedCodexMaintenance), profiles: registry,
		request: req, frames: frames, owner: filepath.Join(history.Owners, binding.NativeSessionID+".json")}
}

func (f codexMaintenanceFixture) installFixtureAdapter(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "fixture-codex")
	script := "#!/bin/sh\n[ \"$1\" = features ] && [ \"$2\" = list ] || exit 7\nprintf 'memories stable false\\n'\n"
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.p.inspect = func(_ context.Context, _ string, spec adapterSpec) (ClaudeInstallation, error) {
		if spec.Name != codexSpec.Name {
			t.Fatal("wrong installed adapter source")
		}
		return ClaudeInstallation{Adapter: "-test.run=^TestCodexMaintenanceFixturePeer$",
			tools: nativeTools{Node: exe, CLI: cli, CLIVersion: "synthetic"}}, nil
	}
}

func TestCodexMaintenanceDoesNotInstallAnAbsentAdapter(t *testing.T) {
	f := newCodexMaintenanceFixture(t, "")
	_, err := f.p.Capabilities(context.Background(), f.request.Bot)
	if pa.Code(err) != pa.AdapterNotFound || f.profiles.active() != 0 {
		t.Fatal("capability check did not refuse a missing installation cleanly", err, f.profiles.active())
	}
	if _, err := os.Stat(manifestPath(f.p.codex.state, codexSpec)); !os.IsNotExist(err) {
		t.Fatal("maintenance installed or wrote an adapter manifest", err)
	}
}

func TestCodexMaintenanceRejectsUnqualifiedBindingAndAccount(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"fresh", "different binding", "foreign bot", "extension", "default profile", "api key", "rotated account", "missing history owner"} {
		t.Run(name, func(t *testing.T) {
			f := newCodexMaintenanceFixture(t, "")
			req := f.request
			switch name {
			case "fresh":
				req.Resume = false
			case "different binding":
				req.Binding.Fence.Generation++
			case "foreign bot":
				req.Bot.Key.BotID = "another"
			case "extension":
				req.Extensions.Skills = "Synthetic active chat skill"
			case "default profile":
				req.Bot.LoginProfile = ""
			case "api key":
				f.profiles.current.Kind = "api_key"
			case "rotated account":
				f.profiles.rotate()
			case "missing history owner":
				if err := os.Remove(f.owner); err != nil {
					t.Fatal(err)
				}
			}
			f.p.inspect = func(context.Context, string, adapterSpec) (ClaudeInstallation, error) {
				t.Error("unqualified request reached the adapter inspection")
				return ClaudeInstallation{}, refusal(pa.Unsupported, "unexpected_probe")
			}
			session, err := f.p.Open(ctx, req, nil)
			var notStarted *app.OpenNotStarted
			if session != nil || !errors.As(err, &notStarted) || f.profiles.active() != 0 {
				t.Fatal("unqualified target was not refused before native open", err)
			}
			if _, err := os.Stat(f.frames); !os.IsNotExist(err) {
				t.Fatal("a native fixture started despite the refusal", err)
			}
		})
	}
}

func TestCodexMaintenanceRechecksRevisionUnderTheProfileHold(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture version probe uses a shell")
	}
	f := newCodexMaintenanceFixture(t, "")
	f.installFixtureAdapter(t)
	f.profiles.changeOnAcquire = true
	session, err := f.p.Open(context.Background(), f.request, nil)
	if session != nil || pa.Code(err) != pa.Revoked || f.profiles.active() != 0 {
		t.Fatal("account changed under configuration preparation", err, f.profiles.active())
	}
	if _, err := os.Stat(f.frames); !os.IsNotExist(err) {
		t.Fatal("changed account reached native startup", err)
	}
}

func TestCodexMaintenanceRejectsAnAdapterProbeWithChangedCLIVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture version probe uses a shell")
	}
	f := newCodexMaintenanceFixture(t, "")
	f.installFixtureAdapter(t)
	ctx := context.Background()
	if _, err := f.p.Capabilities(ctx, f.request.Bot); err != nil {
		t.Fatal(err)
	}
	inspect := f.p.inspect
	f.p.inspect = func(ctx context.Context, state string, spec adapterSpec) (ClaudeInstallation, error) {
		i, err := inspect(ctx, state, spec)
		i.tools.CLIVersion = "different"
		return i, err
	}
	session, err := f.p.Open(ctx, f.request, nil)
	if session != nil || pa.Code(err) != pa.ProviderVersionUnsupported || f.profiles.active() != 0 {
		t.Fatal("the qualified version changed between capability and launch", err, f.profiles.active())
	}
	if _, err := os.Stat(f.frames); !os.IsNotExist(err) {
		t.Fatal("changed version reached native startup", err)
	}
}

func TestCodexMaintenanceUsesOnlyThePinnedLoadAndBareCommand(t *testing.T) {
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("native supervision is not available")
	}
	f := newCodexMaintenanceFixture(t, "completed")
	f.installFixtureAdapter(t)
	ctx := context.Background()
	caps, err := f.p.Capabilities(ctx, f.request.Bot)
	if err != nil || caps.SendMessage || !caps.Resume || !caps.NativeMemoryVerified || caps.NativeMemory != pa.NativeMemoryDisabled || f.profiles.active() != 0 {
		t.Fatal("maintenance capability proof advertised a chat tool or leaked its account hold", caps, err)
	}
	s, err := f.p.Open(ctx, f.request, func(context.Context, app.ProtocolEvent) error {
		t.Error("maintenance emitted a Host chat event")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(ctx) })
	if s.SessionID() != f.request.Binding.NativeSessionID || f.profiles.active() != 1 {
		t.Fatal("native open did not hold the exact qualified account")
	}
	if _, err := s.Send(ctx, app.PromptRequest{Text: "chat must not be sent"}); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("ordinary prompt escaped", err)
	}
	if _, err := s.(app.SteeringSession).Steer(ctx, app.PromptRequest{Text: "chat must not steer"}); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("steering escaped", err)
	}
	if err := s.Answer(ctx, "tool", "allow"); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("maintenance answered a tool permission", err)
	}
	reader := s.(app.NativeContextReader).NativeContext()
	if reader.NativeSessionID != f.request.Binding.NativeSessionID || reader.Fence != f.request.Binding.Fence || !reader.CompactAdvertised {
		t.Fatal("missing qualified compact advertisement", reader)
	}
	pending, err := s.(app.NativeCompactor).BeginCompaction(ctx, f.request.Binding.Fence)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pending.Wait(ctx)
	if err != nil || result.State != app.NativeCompactionCompleted || result.NativeSessionID != f.request.Binding.NativeSessionID || result.ID != "owned-terminal" {
		t.Fatal("the native maintenance port lost the matching terminal", result, err)
	}
	if err := s.Close(ctx); err != nil || f.profiles.active() != 0 {
		t.Fatal("confirmed cleanup lost the account hold", err, f.profiles.active())
	}
	frames := f.observed(t)
	t.Logf("ACP requests %v, terminal %s (%s), %d account holds after close", fixtureMethods(frames), result.State, result.ID, f.profiles.active())
	if methods := fixtureMethods(frames); !reflect.DeepEqual(methods, []string{"initialize", "session/load", "session/prompt"}) {
		t.Fatal("unexpected native traffic", methods)
	}
	var load, command any
	if err := json.Unmarshal(frames[1].Params, &load); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[2].Params, &command); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"cwd": f.request.Directory, "mcpServers": []any{}, "sessionId": f.request.Binding.NativeSessionID}; !reflect.DeepEqual(load, want) {
		t.Fatal("load applied chat setup", load)
	}
	if want := map[string]any{"sessionId": f.request.Binding.NativeSessionID, "prompt": []any{map[string]any{"type": "text", "text": "/compact"}}}; !reflect.DeepEqual(command, want) {
		t.Fatal("maintenance command carried chat content", command)
	}
}

func TestCodexMaintenanceNeverOpensFreshOrCompactsAfterLostAuthority(t *testing.T) {
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("native supervision is not available")
	}
	for _, tc := range []struct {
		name string
		code pa.ErrorCode
	}{
		{"wrong adapter", pa.ProviderVersionUnsupported},
		{"no load capability", pa.ResumeUnsupported},
		{"load refused", pa.ResumeUnsupported},
		{"wrong native ID", pa.ScopeDenied},
		{"no compact command", pa.Unsupported},
		{"rotated after load", pa.Revoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.name
			f := newCodexMaintenanceFixture(t, name)
			f.installFixtureAdapter(t)
			ctx := context.Background()
			s, err := f.p.Open(ctx, f.request, nil)
			if name == "no compact command" || name == "rotated after load" {
				if err != nil || s == nil {
					t.Fatal("the test did not reach native registration", err)
				}
				if name == "rotated after load" {
					f.profiles.rotate()
				}
				_, err = s.(app.NativeCompactor).BeginCompaction(ctx, f.request.Binding.Fence)
			}
			if s == nil || pa.Code(err) != tc.code {
				t.Fatal("unqualified native state lost its typed refusal", err)
			}
			if err := s.Close(ctx); err != nil || f.profiles.active() != 0 {
				t.Fatal("after-close process ownership not observed", err, f.profiles.active())
			}
			operations := []string{"initialize"}
			if name != "wrong adapter" && name != "no load capability" {
				operations = append(operations, "session/load")
			}
			if got := fixtureMethods(f.observed(t)); !reflect.DeepEqual(got, operations) {
				t.Fatal("unknown native history was retried or opened fresh", got)
			}
		})
	}
}

func fixtureMethods(frames []frame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.Method)
	}
	return out
}

func (f codexMaintenanceFixture) observed(t *testing.T) []frame {
	t.Helper()
	data, err := os.ReadFile(f.frames)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var out []frame
	for scanner.Scan() {
		var event frame
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		out = append(out, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCodexMaintenanceFixturePeer(t *testing.T) {
	path := os.Getenv("CLAWDLINE_MAINTENANCE_TEST_PEER")
	if path == "" {
		t.Skip("synthetic ACP child only")
	}
	mode := os.Getenv("CLAWDLINE_MAINTENANCE_TEST_MODE")
	nativeID := os.Getenv("CLAWDLINE_MAINTENANCE_TEST_ID")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(6)
	}
	output := json.NewEncoder(os.Stdout)
	send := func(f frame) {
		f.JSONRPC = "2.0"
		if output.Encode(f) != nil {
			os.Exit(7)
		}
	}
	notify := func(update map[string]any) {
		send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": nativeID, "update": update})})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req frame
		data := scanner.Bytes()
		if _, e := log.Write(append(append([]byte(nil), data...), '\n')); e != nil || json.Unmarshal(data, &req) != nil {
			os.Exit(8)
		}
		var result any
		switch req.Method {
		case "initialize":
			version := CodexAdapterVersion
			if mode == "wrong adapter" {
				version = "unqualified"
			}
			result = map[string]any{
				"protocolVersion": 1, "agentInfo": map[string]string{"name": adapterPackage(pa.Codex), "version": version},
				"agentCapabilities": map[string]bool{"loadSession": mode != "no load capability"},
			}
		case "session/load":
			if mode == "load refused" {
				send(frame{ID: req.ID, Error: &rpcError{Code: -32603, Message: "Synthetic load refusal"}})
				continue
			}
			id := nativeID
			if mode == "wrong native ID" {
				id = "foreign-session"
			}
			if mode != "no compact command" && mode != "wrong native ID" {
				notify(map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]string{"name": "compact"}}})
			}
			// A mode offer that differs from the Bot is not rewritten during maintenance.
			result = map[string]any{"sessionId": id, "modes": map[string]string{"currentModeId": "different"}}
		case "session/prompt":
			for _, state := range []string{"in_progress", "completed"} {
				notify(map[string]any{"sessionUpdate": "compaction_update", "compactionId": "owned-terminal", "status": state})
			}
			result = map[string]string{"stopReason": "end_turn"}
		default:
			os.Exit(9)
		}
		send(frame{ID: req.ID, Result: raw(result)})
	}
	if scanner.Err() != nil || log.Close() != nil {
		os.Exit(10)
	}
	os.Exit(0)
}
