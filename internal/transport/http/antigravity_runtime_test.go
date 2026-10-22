package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func runtimeServer(t *testing.T) (*Server, string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix listener")
	}
	root, err := os.MkdirTemp("/private/tmp", "ag-run-")
	if err != nil {
		root, err = os.MkdirTemp("/tmp", "ag-run-")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	tmux := terminal.NewTmux()
	s := &Server{cfg: config.Config{Dir: root}, terminals: []ports.TerminalHost{tmux}, inventory: app.Inventory{Process: process.New(), Identity: transcript.NewHost(), Terminals: []ports.TerminalHost{tmux}}}
	s.antigravityInstallation = func(context.Context) antigravity.Installation {
		return antigravity.Installation{InstalledKnown: true, Installed: true, Compatible: true, Version: "1.4.5", Path: exe}
	}
	s.wireAntigravity()
	return s, filepath.Join(root, "antigravity")
}

// These are ordinary copies of this test executable, not a vendor CLI.
func TestAntigravityRuntimeSyntheticProvider(t *testing.T) {
	root := os.Getenv("AG_RUNTIME_SYNTHETIC_ROOT")
	if root == "" {
		return
	}
	if os.Getenv("AG_RUNTIME_MANAGED") == "1" {
		wait := func(name string) {
			until := time.Now().Add(10 * time.Second)
			for time.Now().Before(until) {
				if _, err := os.Stat(filepath.Join(root, name)); err == nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			os.Exit(2)
		}
		wait("managed-open")
		bad := exec.Command(os.Args[0], "-test.run=^TestAntigravityRuntimeSyntheticHook$")
		bad.Env = append(os.Environ(), "AG_RUNTIME_WRONG_CAP=1")
		output, err := bad.CombinedOutput()
		if err != nil {
			output = append(output, []byte(err.Error())...)
		}
		if os.WriteFile(filepath.Join(root, "managed-bad-result"), output, 0600) != nil {
			os.Exit(2)
		}
		wait("managed-continue")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAntigravityRuntimeSyntheticHook$")
	output, err := cmd.CombinedOutput()
	if err != nil {
		output = append(output, []byte(err.Error())...)
	}
	if os.WriteFile(filepath.Join(root, "hook-result"), output, 0600) != nil {
		os.Exit(2)
	}
	// Keep the positively observed provider alive until its private pane is closed.
	<-time.After(20 * time.Second)
	os.Exit(0)
}

func TestAntigravityRuntimeSyntheticHook(t *testing.T) {
	root := os.Getenv("AG_RUNTIME_SYNTHETIC_ROOT")
	if root == "" {
		return
	}
	path, _ := transcript.AntigravityPath(filepath.Join(root, "home"), "74000000-0000-4000-8000-000000000003")
	payload, _ := json.Marshal(map[string]any{"conversationId": "74000000-0000-4000-8000-000000000003", "transcriptPath": path, "invocationNum": 1})
	var out bytes.Buffer
	var launch *antigravity.HookLaunch
	if os.Getenv("AG_RUNTIME_MANAGED") == "1" {
		var err error
		launch, err = antigravity.LoadHookLaunch(filepath.Join(root, "antigravity"), os.Getenv(antigravity.CapabilityFileEnv))
		if err != nil {
			fmt.Print(err.Error())
			os.Exit(1)
		}
		if os.Getenv("AG_RUNTIME_WRONG_CAP") == "1" {
			replacement := "0"
			if launch.Capability[0] == '0' {
				replacement = "1"
			}
			launch.Capability = replacement + launch.Capability[1:]
		}
	}
	err := antigravity.ObserveHook(context.Background(), filepath.Join(root, "antigravity"), "PreInvocation", launch, bytes.NewReader(payload), &out)
	if err != nil {
		fmt.Print(err.Error())
		os.Exit(1)
	}
	fmt.Print(out.String())
	os.Exit(0)
}

func TestAntigravityRuntimeNativeCompositionAndCallbacks(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("LC_ALL", "C")
	s, dir := runtimeServer(t)
	root := filepath.Dir(dir)
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	home := filepath.Join(root, "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	host := s.inventory.Identity.(*transcript.Host)
	host.Home = home
	path, _ := transcript.AntigravityPath(home, "74000000-0000-4000-8000-000000000003")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tmux := s.terminals[0].(*terminal.Tmux)
	tmux.Binary = binary
	tmux.Socket = filepath.Join(root, "tmux.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-S", tmux.Socket}, args...)...)
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "TMUX=") {
				cmd.Env = append(cmd.Env, v)
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("synthetic tmux %s: %v %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	admitted := make(chan struct{}, 1)
	responded := make(chan ports.AntigravityBinding, 1)
	stop, err := s.StartAntigravity(ctx, AntigravityRuntimeOptions{
		Admit: func(context.Context, antigravity.NativeUnixPeer, antigravity.Witness, string, json.RawMessage) (*antigravity.HookLaunch, error) {
			admitted <- struct{}{}
			return nil, nil
		},
		PreInvocationResponse: func(_ context.Context, b ports.AntigravityBinding) ([]byte, error) {
			responded <- b
			return []byte(`{"fixture":true}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	command("new-session", "-d", "-s", "synthetic", "exec env AG_RUNTIME_SYNTHETIC_ROOT="+quote(root)+" "+quote(exe)+" -test.run=^TestAntigravityRuntimeSyntheticProvider$")
	defer func() {
		cmd := exec.Command(binary, "-S", tmux.Socket, "kill-server")
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		_ = cmd.Run()
	}()
	var result []byte
	for ctx.Err() == nil {
		result, err = os.ReadFile(filepath.Join(root, "hook-result"))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || !bytes.Contains(result, []byte(`"fixture":true`)) {
		t.Fatalf("real hook callback failed: %s %v", result, err)
	}
	select {
	case <-admitted:
	default:
		t.Fatal("admission callback not invoked")
	}
	var binding ports.AntigravityBinding
	select {
	case binding = <-responded:
	default:
		t.Fatal("response callback not invoked")
	}
	p, err := s.inventory.Process.(*process.PS).Antigravity.VerifyAntigravityProcess(ctx, binding.Process.PID)
	if err != nil || p != binding.Process || strings.HasPrefix(p.TTY, "/dev/") {
		t.Fatalf("process port normalization: %+v %v", p, err)
	}
	fromPane, err := tmux.Antigravity(ctx, p.TerminalID, p.TTY)
	if err != nil || fromPane != p {
		t.Fatalf("pane callback differs: %+v %v", fromPane, err)
	}
	row := session.Session{ID: p.TerminalID, TTY: p.TTY, PID: p.PID, Backend: session.BackendTmux, Assistant: session.AssistantAntigravity}
	identity, ok := host.ForSession(ctx, row)
	if !ok || identity.ConversationID != binding.ConversationID {
		t.Fatalf("transcript identity differs: %+v", identity)
	}
	s.pictures = newPictures(root)
	if s.actions().Antigravity != s.antigravityBridge.Load() {
		t.Fatal("Actions uses a different bridge")
	}
}

func TestAntigravityRuntimeExplicitStartCancelJoinAndRestart(t *testing.T) {
	s, dir := runtimeServer(t)
	if s.antigravityBridge.Load() != nil {
		t.Fatal("bridge started before Start")
	}
	if _, err := s.terminals[0].(*terminal.Tmux).Antigravity(context.Background(), "%unmanaged", "tty"); err == nil || err.Error() != "antigravity_runtime_inactive" {
		t.Fatal("inactive mapping fabricated executable proof")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("construction created runtime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop, err := s.StartAntigravity(ctx, AntigravityRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.antigravityBridge.Load() == nil {
		t.Fatal("bridge not published")
	}
	if _, err = s.StartAntigravity(ctx, AntigravityRuntimeOptions{}); err == nil {
		t.Fatal("duplicate runtime")
	}
	conn, err := net.Dial("unix", antigravity.HookSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); stop() }()
	}
	wg.Wait()
	conn.Close()
	if s.antigravityBridge.Load() != nil {
		t.Fatal("inactive bridge still published")
	}
	if _, err = os.Lstat(antigravity.HookSocketPath(dir)); !os.IsNotExist(err) {
		t.Fatal("owned socket survived join")
	}
	stop, err = s.StartAntigravity(context.Background(), AntigravityRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stop()
}

func TestAntigravityRuntimeRefusesForeignAndRetainsReplacement(t *testing.T) {
	s, dir := runtimeServer(t)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := antigravity.HookSocketPath(dir)
	if err := os.WriteFile(path, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartAntigravity(context.Background(), AntigravityRuntimeOptions{}); err == nil || err.Error() != "antigravity_hook_socket_exists" {
		t.Fatalf("existing node: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "foreign" {
		t.Fatal("foreign bytes changed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stop, err := s.StartAntigravity(context.Background(), AntigravityRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	stop()
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatal("replacement removed")
	}
}

func TestAntigravityRuntimeIncompatibleAndCancelledStayInactive(t *testing.T) {
	s, dir := runtimeServer(t)
	s.antigravityInstallation = func(context.Context) antigravity.Installation {
		return antigravity.Installation{InstalledKnown: true, Installed: true, Compatible: false}
	}
	if _, err := s.StartAntigravity(context.Background(), AntigravityRuntimeOptions{}); err == nil {
		t.Fatal("incompatible started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StartAntigravity(ctx, AntigravityRuntimeOptions{}); err == nil {
		t.Fatal("cancelled started")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("inactive created state")
	}
}

func TestAntigravityRuntimeKeepsMissingAndUnknownDistinct(t *testing.T) {
	s, dir := runtimeServer(t)
	for _, test := range []struct {
		info antigravity.Installation
		code string
	}{
		{antigravity.Installation{}, "antigravity_runtime_installation_unavailable"},
		{antigravity.Installation{InstalledKnown: true}, "antigravity_runtime_inactive"},
		{antigravity.Installation{InstalledKnown: true, Installed: true}, "antigravity_runtime_incompatible"},
	} {
		s.antigravityInstallation = func(context.Context) antigravity.Installation { return test.info }
		if _, err := s.StartAntigravity(context.Background(), AntigravityRuntimeOptions{}); err == nil || err.Error() != test.code {
			t.Fatalf("installation state: %v", err)
		}
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("unavailable installation created runtime")
	}
}
