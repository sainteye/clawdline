package antigravitylaunch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

type fixture struct {
	config  Config
	store   *Store
	request projects.LaunchRequest
	prompt  string
}

func setup(t *testing.T) fixture {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("private store requires native Unix ownership")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := Config{StateDir: filepath.Join(base, "antigravity"), SquadLaunchRoot: filepath.Join(base, "squad", "launches")}
	prompt := filepath.Join(config.SquadLaunchRoot, "AAAAAAAAAAAAAAAAAAAAAA", "personas", "backend.md")
	if err := os.MkdirAll(filepath.Dir(prompt), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, prompt, []byte("Synthetic immutable role ROLE_COPPER"))
	s, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	req := projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: base, SquadPromptPath: prompt, Persona: "backend", Memory: "Synthetic original MEMORY_FIR", Language: "zh-Hant-TW", Executable: filepath.Join(base, "agy"), PermissionMode: "plan"}
	return fixture{config: config, store: s, request: req, prompt: prompt}
}
func put(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != "antigravity_context_"+want {
		t.Fatalf("got %v, want typed %s", err, want)
	}
}
func prep(t *testing.T, f fixture) Prepared {
	t.Helper()
	p, e := f.store.Prepare(context.Background(), f.request)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func process(p Prepared) ports.AntigravityProcess {
	return ports.AntigravityProcess{PID: 1234, ParentPID: 123, Birth: "2026-10-22T10:00:00.123456Z", Executable: p.Request.Executable, TTY: "/dev/ttysynthetic", TerminalID: "%42", BootID: "synthetic-boot", KernelGeneration: "synthetic-exec-1"}
}
func pin(t *testing.T, s *Store, p Prepared) ports.AntigravityProcess {
	t.Helper()
	ctx := context.Background()
	native := process(p)
	if err := s.AttachTerminal(ctx, p.Record.ID, "tmux", native.TerminalID); err != nil {
		t.Fatal(err)
	}
	if err := s.PinProcess(ctx, p.Record.ID, native); err != nil {
		t.Fatal(err)
	}
	return native
}
func binding(p ports.AntigravityProcess, id string, seq uint64) ports.AntigravityBinding {
	return ports.AntigravityBinding{Process: p, ConversationID: id, TranscriptPath: "/synthetic/native/" + id + "/log.jsonl", Event: "PreInvocation", Sequence: seq, Verified: true, ObservedAt: time.Date(2026, 10, 22, 10, 0, int(seq%60), 0, time.UTC)}
}

func TestPrepareRoleAndRoleless(t *testing.T) {
	for _, role := range []bool{true, false} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			f := setup(t)
			if !role {
				f.request.Persona = ""
				f.request.SquadPromptPath = ""
			}
			p := prep(t, f)
			if !p.Record.Prepared || p.Request.AntigravityContext == nil {
				t.Fatal("missing durable context")
			}
			cap, err := os.ReadFile(p.Record.CapabilityPath)
			if err != nil || !validHash(string(cap)) || len(cap) != 64 || hash(cap) != p.Record.CapabilityDigest {
				t.Fatal("invalid capability contract")
			}
			if len(p.PrivateEnv) != 1 || !strings.Contains(p.PrivateEnv[0], projects.ShellQuoted(p.Record.CapabilityPath)) || strings.Contains(p.PrivateEnv[0], string(cap)) {
				t.Fatal("capability escaped private file")
			}
			document, err := os.ReadFile(p.Record.ContextPath)
			if err != nil || hash(document) != p.Record.ContextHash {
				t.Fatal("context not durable")
			}
			for _, path := range []string{f.config.StateDir, filepath.Dir(p.Record.CapabilityPath)} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("directory not private")
				}
			}
			for _, path := range []string{p.Record.ContextPath, p.Record.CapabilityPath} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("file not private")
				}
			}
			again, err := f.store.PrepareLaunch(context.Background(), p.Record.ID, f.request)
			if err != nil || again.Record != p.Record {
				t.Fatalf("identical retry: %v", err)
			}
			f.request.Memory = "different"
			_, err = f.store.PrepareLaunch(context.Background(), p.Record.ID, f.request)
			code(t, err, "launch_conflict")
			if role && p.Request.AntigravityContext.Prompt != "Synthetic immutable role ROLE_COPPER" {
				t.Fatal("role not pinned")
			}
			if !role && p.Request.AntigravityContext.Prompt != "" {
				t.Fatal("roleless gained a role")
			}
			if _, err := projects.Admit(p.Request); err != nil {
				t.Fatalf("prepared request failed real admission: %v", err)
			}
		})
	}
}

func TestResumeKeepsOriginalAndNativeOnlyHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := prep(t, f)
	native := pin(t, f.store, p)
	id := uuid.NewString()
	b := binding(native, id, 1)
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal("binding retry", err)
	}
	f.request.Resume = id
	f.request.Memory = "today's mutable memory"
	f.request.Language = "en"
	f.request.SquadPromptPath = ""
	resumed := prep(t, f)
	if *resumed.Request.AntigravityContext != *p.Request.AntigravityContext || resumed.Request.Memory != p.Request.Memory || resumed.Request.Language != p.Request.Language || resumed.Request.SquadPromptPath != f.prompt {
		t.Fatal("resume substituted current settings")
	}
	// A new Squad publication with identical bytes retains the original path.
	newPrompt := filepath.Join(f.config.SquadLaunchRoot, "BBBBBBBBBBBBBBBBBBBBBB", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(newPrompt), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, newPrompt, []byte(p.Request.AntigravityContext.Prompt))
	f.request.SquadPromptPath = newPrompt
	if q := prep(t, f); q.Request.SquadPromptPath != f.prompt {
		t.Fatal("original path replaced")
	}
	put(t, newPrompt, []byte("new role catalog bytes"))
	_, err := f.store.Prepare(ctx, f.request)
	code(t, err, "resume_prompt_conflict")
	unknown := uuid.NewString()
	_, err = f.store.Restore(ctx, f.request.ProjectRoot, unknown)
	code(t, err, "unavailable")
	f.request.Resume = unknown
	f.request.SquadPromptPath = ""
	_, err = f.store.Prepare(ctx, f.request)
	code(t, err, "unavailable")
	_, err = f.store.Restore(ctx, filepath.Dir(f.request.ProjectRoot), id)
	code(t, err, "workspace_conflict")
	// Requested resume UUID cannot make history. A resumed process may positively
	// observe a new UUID instead; no requested-ID gate can manufacture authority.
	if err := f.store.Deactivate(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	next := process(resumed)
	next.PID++
	next.TerminalID = "%43"
	if err := f.store.AttachTerminal(ctx, resumed.Record.ID, "tmux", next.TerminalID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PinProcess(ctx, resumed.Record.ID, next); err != nil {
		t.Fatal(err)
	}
	branch := uuid.NewString()
	if err := f.store.RecordBinding(ctx, resumed.Record.ID, binding(next, branch, 2), KeepConversation); err != nil {
		t.Fatal(err)
	}
	third := uuid.NewString()
	b3 := binding(next, third, 3)
	code(t, f.store.RecordBinding(ctx, resumed.Record.ID, b3, KeepConversation), "switch_requires_policy")
	if err := f.store.RecordBinding(ctx, resumed.Record.ID, b3, AllowConversationSwitch); err != nil {
		t.Fatal(err)
	}
	workspaces, err := f.store.WorkspaceBindings(ctx)
	if err != nil || len(workspaces) != 3 {
		t.Fatalf("lost original history: %v %v", workspaces, err)
	}
	for _, conversation := range []string{id, branch, third} {
		c, err := f.store.Restore(ctx, p.Request.ProjectRoot, conversation)
		if err != nil || c != *p.Request.AntigravityContext {
			t.Fatal("history overwritten", err)
		}
	}
}

func TestProcessPinRestartAndAmbiguity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := prep(t, f)
	native := pin(t, f.store, p)
	for _, change := range []func(*ports.AntigravityProcess){func(p *ports.AntigravityProcess) { p.PID++ }, func(p *ports.AntigravityProcess) { p.Birth = "2026-10-22T10:00:00Z" }, func(p *ports.AntigravityProcess) { p.Executable += "2" }, func(p *ports.AntigravityProcess) { p.TerminalID = "%9" }, func(p *ports.AntigravityProcess) { p.TTY += "2" }, func(p *ports.AntigravityProcess) { p.KernelGeneration += "2" }, func(p *ports.AntigravityProcess) { p.BootID += "2" }, func(p *ports.AntigravityProcess) { p.ParentPID++ }} {
		wrong := native
		change(&wrong)
		if _, err := f.store.LookupProcess(ctx, wrong); err == nil {
			t.Fatal("wrong process accepted")
		}
		if err := f.store.PinProcess(ctx, p.Record.ID, wrong); err == nil {
			t.Fatal("repin overwrote process")
		}
	}
	other := prep(t, f)
	code(t, f.store.AttachTerminal(ctx, other.Record.ID, "tmux", native.TerminalID), "terminal_conflict")
	r, err := f.store.LookupProcess(ctx, native)
	if err != nil {
		t.Fatal(err)
	}
	commit, hook, err := f.store.Commitment(ctx, r)
	if err != nil || commit.ID != r.ID || commit.Process != native || commit.CapabilityDigest != hash([]byte(hook.Capability)) {
		t.Fatal("bridge commitment mismatch", err)
	}
	id := uuid.NewString()
	b := binding(native, id, 1)
	if err := f.store.RecordBinding(ctx, r.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(f.config); err == nil {
		t.Fatal("second writer acquired lock")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	_, err = reopened.LookupProcess(ctx, native)
	code(t, err, "process_unverified")
	if _, _, err := reopened.Commitment(ctx, r); err == nil {
		t.Fatal("reopened process had authority")
	}
	if _, err := reopened.Restore(ctx, p.Request.ProjectRoot, id); err != nil {
		t.Fatal("historical evidence lost on restart", err)
	}
	if err := reopened.PinProcess(ctx, r.ID, native); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LookupProcess(ctx, native); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateFilesTamperAndForeignPaths(t *testing.T) {
	for _, kind := range []string{"context", "prompt", "symlink-context", "symlink-prompt", "foreign-prompt", "hardlink", "mode", "capability", "parent-replacement"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			p := prep(t, f)
			native := pin(t, f.store, p)
			ctx := context.Background()
			foreign := filepath.Join(f.request.ProjectRoot, "foreign.md")
			put(t, foreign, []byte("foreign bytes"))
			switch kind {
			case "context":
				tampered, err := projects.PrepareAntigravityContext(f.prompt, p.Request.AntigravityContext.Prompt, "changed but internally valid memory", "en")
				if err != nil {
					t.Fatal(err)
				}
				bytes, _, err := tampered.Document()
				if err != nil {
					t.Fatal(err)
				}
				put(t, p.Record.ContextPath, bytes)
			case "prompt":
				put(t, f.prompt, []byte("tampered role"))
			case "symlink-context":
				if err := os.Remove(p.Record.ContextPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, p.Record.ContextPath); err != nil {
					t.Fatal(err)
				}
			case "symlink-prompt":
				if err := os.Remove(f.prompt); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, f.prompt); err != nil {
					t.Fatal(err)
				}
			case "foreign-prompt":
				f.request.SquadPromptPath = foreign
				_, err := f.store.Prepare(ctx, f.request)
				if err == nil {
					t.Fatal("foreign prompt read")
				}
				return
			case "hardlink":
				if err := os.Remove(p.Record.ContextPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(foreign, p.Record.ContextPath); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(p.Record.ContextPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "capability":
				put(t, p.Record.CapabilityPath, []byte(strings.Repeat("a", 64)+"\n"))
			case "parent-replacement":
				if err := os.Rename(filepath.Dir(p.Record.ContextPath), filepath.Dir(p.Record.ContextPath)+"-moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(p.Record.ContextPath)+"-moved", filepath.Dir(p.Record.ContextPath)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.store.LookupProcess(ctx, native); err == nil {
				t.Fatal("tampered private state authorized")
			}
		})
	}
}

func TestInterruptedPublicationCancellationAndFull(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := uuid.NewString()
	f.store.beforeWrite = func(phase string) error {
		if phase == "context" {
			return errors.New("synthetic full disk")
		}
		return nil
	}
	_, err := f.store.PrepareLaunch(ctx, id, f.request)
	code(t, err, "write_failed")
	if f.store.state.Records[id].Prepared {
		t.Fatal("partial publication became authoritative")
	}
	if _, err := f.store.LookupProcess(ctx, process(Prepared{Request: f.request})); err == nil {
		t.Fatal("pending record authorized")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f.store = s
	p, err := s.PrepareLaunch(ctx, id, f.request)
	if err != nil || !p.Record.Prepared {
		t.Fatal("pending retry could not recover", err)
	}
	files, err := os.ReadDir(filepath.Dir(p.Record.ContextPath))
	if err != nil || len(files) != 2 {
		t.Fatal("retry left extra files", files, err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Prepare(cancelCtx, f.request)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	// Exercise actual on-disk admission through the exact finite ceiling.
	for i := 1; i < MaxLaunches; i++ {
		if _, err := s.Prepare(ctx, f.request); err != nil {
			t.Fatal(i, err)
		}
	}
	_, err = s.Prepare(ctx, f.request)
	code(t, err, "launches_full")
	stats, err := s.Stats(ctx)
	if err != nil || stats.Launches != MaxLaunches || stats.Pending != 0 {
		t.Fatal(stats, err)
	}
	dirs, err := os.ReadDir(s.launches.path)
	if err != nil || len(dirs) != MaxLaunches {
		t.Fatal("unbounded orphan directories", len(dirs), err)
	}
}

func TestReplacedIndexLockAndDirectoryRefuse(t *testing.T) {
	for _, kind := range []string{"index", "lock", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			p := prep(t, f)
			ctx := context.Background()
			switch kind {
			case "index":
				put(t, filepath.Join(f.config.StateDir, "context-index.json"), []byte(`{}`))
			case "lock":
				path := filepath.Join(f.config.StateDir, "context-store.lock")
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				put(t, path, nil)
			case "directory":
				if err := os.Rename(f.config.StateDir, f.config.StateDir+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.config.StateDir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.AttachTerminal(ctx, p.Record.ID, "tmux", "%42"); err == nil {
				t.Fatal("replaced private state accepted")
			}
		})
	}
}

func TestPendingCapabilityRecoveryAndNonPromptFiles(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := uuid.NewString()
	f.store.beforeWrite = func(phase string) error {
		if phase == "context" {
			return errors.New("interrupt")
		}
		return nil
	}
	if _, err := f.store.PrepareLaunch(ctx, id, f.request); err == nil {
		t.Fatal("expected interruption")
	}
	r := f.store.state.Records[id]
	put(t, r.CapabilityPath, []byte("partial"))
	// A crash may also leave the fixed replacement stage, which is recovered
	// without new temporary names. It is not an authoritative context.
	put(t, filepath.Join(f.config.StateDir, ".context-index.json.next"), []byte("partial"))
	f.store.beforeWrite = nil
	if _, err := f.store.PrepareLaunch(ctx, id, f.request); err != nil {
		t.Fatal("partial files could not recover", err)
	}
	for _, name := range []string{"capability", "snapshot.json"} {
		path := filepath.Join(filepath.Dir(filepath.Dir(f.prompt)), name)
		put(t, path, []byte("private non-prompt"))
		bad := f.request
		bad.SquadPromptPath = path
		if _, err := f.store.Prepare(ctx, bad); err == nil {
			t.Fatal("read non-prompt file", name)
		}
	}
}
