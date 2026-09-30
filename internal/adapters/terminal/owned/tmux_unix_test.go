//go:build !windows

package owned

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tmuxterm "github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// These run a real tmux, on a server under a directory each test makes for
// itself, and kill only that server. They never talk to the person's own
// tmux server except to list it.

// testDir is a CLAWDLINE_NEXT_DIR of the test's own. Under /tmp rather than
// t.TempDir: macOS's per-user temporary directory is deep enough to put the
// socket past a Unix socket's 104 bytes.
func testDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "clt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newServer(t *testing.T, dir string) *Server {
	t.Helper()
	if found, _ := tmuxterm.FindTmux(); found == "" {
		t.Skip("tmux is not installed")
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s.binary != "" {
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			_, _ = s.call(ctx, "", "kill-server")
			cancel()
		}
	})
	return s
}

func open(t *testing.T, s *Server, cols, rows int) terminal.Terminal {
	t.Helper()
	term, err := s.Open(context.Background(), ports.OpenTerminal{
		ProjectPath: t.TempDir(), ProjectID: "prj_test", Cols: cols, Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	return term
}

func keys(t *testing.T, s *Server, id terminal.ID, text string) {
	t.Helper()
	if err := s.Keys(context.Background(), id, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]|\x1b[=>]`)

func plain(f terminal.Frame) []string {
	out := make([]string, len(f.Lines))
	for i, line := range f.Lines {
		out[i] = strings.TrimRight(escapes.ReplaceAllString(line, ""), " ")
	}
	return out
}

func hasLine(f terminal.Frame, want string) bool { return slices.Contains(plain(f), want) }

// waitFrame reads frames until one satisfies ok.
func waitFrame(t *testing.T, s *Server, id terminal.ID, what string, ok func(terminal.Frame) bool) terminal.Frame {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last terminal.Frame
	for time.Now().Before(deadline) {
		f, err := s.Frame(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if ok(f) {
			return f
		}
		last = f
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no frame showed %s; the last:\n%s", what, strings.Join(plain(last), "\n"))
	return last
}

func raw(t *testing.T, s *Server, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := s.call(ctx, "", args...)
	return strings.TrimSpace(out), err
}

// Acceptance 1.
func TestOpenTypeResizeRebuildAndClose(t *testing.T) {
	dir := testDir(t)
	s := newServer(t, dir)
	term := open(t, s, 80, 24)
	if term.Status != terminal.Running || term.Cols != 80 || term.Rows != 24 || term.ProjectID != "prj_test" {
		t.Fatalf("%+v", term)
	}
	// `h""i` so the line typed is not the line answered.
	keys(t, s, term.ID, "echo h\"\"i\r")
	waitFrame(t, s, term.ID, "hi", func(f terminal.Frame) bool { return hasLine(f, "hi") })

	if err := s.Resize(context.Background(), term.ID, 120, 40); err != nil {
		t.Fatal(err)
	}
	size, err := raw(t, s, "display-message", "-p", "-t", "="+term.ID.SessionName()+":", "#{window_width}x#{window_height}")
	if err != nil || size != "120x40" {
		t.Fatalf("after resize: %q %v", size, err)
	}
	before := waitFrame(t, s, term.ID, "120x40", func(f terminal.Frame) bool { return f.Cols == 120 && f.Rows == 40 })

	// The server is the record: a new adapter on the same directory lists
	// the same terminal and reads the same screen.
	again, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := again.List(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != term.ID || list[0].Cols != 120 {
		t.Fatalf("rebuilt list: %+v %v", list, err)
	}
	after, err := again.Frame(context.Background(), term.ID)
	if err != nil || !slices.Equal(after.Lines, before.Lines) || after.Rev != before.Rev {
		t.Fatalf("rebuilt frame differs: %v\n%q\n%q", err, before.Lines, after.Lines)
	}

	if err := again.Close(context.Background(), term.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := raw(t, s, "has-session", "-t", "="+term.ID.SessionName()); err == nil {
		t.Fatal("has-session found the closed terminal")
	}
	if err := again.Close(context.Background(), term.ID); err != nil {
		t.Fatalf("closing it again: %v", err)
	}
	if _, err := again.Frame(context.Background(), term.ID); !isCode(err, terminal.CodeClosed) {
		t.Fatalf("a closed terminal's frame: %v", err)
	}
}

func isCode(err error, want terminal.RefusalCode) bool {
	code, ok := terminal.CodeOf(err)
	return ok && code == want
}

// Acceptance 2.
func TestTheFrameCarriesApplicationCursorMode(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	// Read while `cat` runs: a line editor at a prompt sets its own modes
	// (zsh's zle sends smkx, which is both DECCKM and DECKPAM), and the
	// frame would be read against whatever it last set.
	keys(t, s, term.ID, `printf '\e[?1h\e>'; cat`+"\r")
	waitFrame(t, s, term.ID, "app_cursor on and app_keypad off", func(f terminal.Frame) bool {
		return f.Modes.AppCursor && !f.Modes.AppKeypad
	})
	keys(t, s, term.ID, "\x03")
	keys(t, s, term.ID, `printf '\e[?1l\e='; cat`+"\r")
	waitFrame(t, s, term.ID, "app_cursor off and app_keypad on", func(f terminal.Frame) bool {
		return !f.Modes.AppCursor && f.Modes.AppKeypad
	})
	keys(t, s, term.ID, "\x03")
}

// Acceptance 3: without -J a long line is the rows it wrapped onto.
func TestALongLineIsTheRowsItWrappedOnto(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	keys(t, s, term.ID, "printf 'x%.0s' $(seq 1 200); echo\r")
	full, tail := strings.Repeat("x", 80), strings.Repeat("x", 40)
	waitFrame(t, s, term.ID, "200 x in three rows", func(f terminal.Frame) bool {
		lines := plain(f)
		for i := 0; i+2 < len(lines); i++ {
			if lines[i] == full && lines[i+1] == full && lines[i+2] == tail {
				return true
			}
		}
		return false
	})
}

// sleeper is the pid of the `sleep 600` running in a terminal, or 0. By
// its command line, because under an emulator (qemu in a container) every
// process's name is the emulator's.
func sleeper(t *testing.T, s *Server, id terminal.ID) int {
	t.Helper()
	pane, err := raw(t, s, "display-message", "-p", "-t", "="+id.SessionName()+":", "#{pane_pid}")
	if err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("pgrep", "-f", "-P", pane, "sleep 600").Output()
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	return pid
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// Acceptance 4: the generated configuration carries three opens, and a
// source-file of it into a running server leaves what runs there running.
func TestTheGeneratedConfigSurvivesOpensAndASourceFile(t *testing.T) {
	s := newServer(t, testDir(t))
	first := open(t, s, 80, 24)
	keys(t, s, first.ID, "sleep 600\r")
	open(t, s, 90, 30)
	open(t, s, 100, 20)
	list, err := s.List(context.Background())
	if err != nil || len(list) != 3 {
		t.Fatalf("after three opens: %+v %v", list, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	pid := sleeper(t, s, first.ID)
	for pid == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		pid = sleeper(t, s, first.ID)
	}
	if pid == 0 {
		t.Fatal("the first terminal never ran sleep 600")
	}

	// A server running an older configuration is given this one.
	if _, err := raw(t, s, "set-option", "-s", "@clawdline_conf_version", "0"); err != nil {
		t.Fatal(err)
	}
	open(t, s, 80, 24)
	if v, err := raw(t, s, "show-options", "-sqv", "@clawdline_conf_version"); err != nil || v != confVersion {
		t.Fatalf("the running server was not given the configuration: %q %v", v, err)
	}
	if !alive(pid) || sleeper(t, s, first.ID) != pid {
		t.Fatalf("after source-file the first terminal's sleep %d is gone", pid)
	}
	if list, _ := s.List(context.Background()); len(list) != 4 {
		t.Fatalf("after the fourth open: %d terminals", len(list))
	}
}

// Acceptance 5: a server started with everything the daemon carries still
// gives a shell only the whitelist.
func TestAServerStartedWithTheDaemonsWholeEnvironmentGivesACleanShell(t *testing.T) {
	agent := os.Getenv("SSH_AUTH_SOCK")
	if agent == "" {
		agent = "/tmp/clt-test-agent.sock"
	}
	daemon := map[string]string{
		"CODEX_THREAD_ID": "t", "CODEX_CI": "1", "CODEX_SESSION_ID": "s", "LC_ALL": "C", "NO_COLOR": "1",
		"SSH_AUTH_SOCK": agent,
	}
	for _, k := range []string{"HOME", "USER", "LOGNAME", "SHELL", "PATH", "TMPDIR", "LANG"} {
		if v := os.Getenv(k); v != "" {
			daemon[k] = v
		}
	}
	var environ []string
	for k, v := range daemon {
		environ = append(environ, k+"="+v)
	}
	sort.Strings(environ)

	s := newServer(t, testDir(t))
	s.machine.Getenv = func(k string) string { return daemon[k] }
	s.environ = func() []string { return environ }
	if err := s.ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	err := s.ensureServer(context.Background())
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// Started first, by somebody else, with all of it and nothing removed.
	start := exec.Command(s.binary, "-S", s.sock, "-f", s.conf, "new-session", "-d", "-s", "first")
	start.Env = environ
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("starting the server: %v %s", err, out)
	}

	term := open(t, s, 120, 24)
	keys(t, s, term.ID, `echo "C=$(env | grep -c CODEX_)=" "L=[$LC_ALL]" "S=[$SSH_AUTH_SOCK]" "T=$(tmux ls 2>/dev/null | grep -c '^clt-')="`+"\r")
	want := "C=0= L=[] S=[" + agent + "] T=0="
	waitFrame(t, s, term.ID, want, func(f terminal.Frame) bool { return hasLine(f, want) })

	// And the terminal is not the server's own session.
	list, _ := s.List(context.Background())
	if len(list) != 1 || list[0].ID != term.ID {
		t.Fatalf("list: %+v", list)
	}
}

// The daemon's systemd account on Linux has nologin as its shell, in SHELL
// and in the user database. A terminal still opens a shell that answers.
func TestAServiceAccountWithNologinGetsAShellThatAnswers(t *testing.T) {
	var nologin string
	for _, path := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/usr/bin/false", "/bin/false"} {
		if _, err := os.Stat(path); err == nil {
			nologin = path
			break
		}
	}
	if nologin == "" {
		t.Skip("no nologin or false on this machine")
	}
	s := newServer(t, testDir(t))
	s.machine.Getenv = func(k string) string {
		if k == "SHELL" {
			return nologin
		}
		return os.Getenv(k)
	}
	s.machine.LoginShell = func(context.Context, string) string { return nologin }
	term := open(t, s, 100, 24)
	keys(t, s, term.ID, "echo h\"\"i\r")
	waitFrame(t, s, term.ID, "hi echoed under SHELL="+nologin, func(f terminal.Frame) bool { return hasLine(f, "hi") })
	t.Logf("SHELL=%s ran %s", nologin, s.shell)
}

// Acceptance 6. Run as the rest are, this is a daemon with whatever LANG the
// test has. Run under `env -i HOME=… PATH=/usr/bin:/bin`, it is a daemon
// started from the desktop, and the LANG is the machine's.
func TestTheShellIsUTF8AndEchoesChinese(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 100, 24)
	t.Logf("daemon LANG=%q, shell LANG=%q", os.Getenv("LANG"), s.lang)
	keys(t, s, term.ID, `locale | grep LC_CTYPE | tr -d '"' | sed 's/^/CT:/'`+"\r")
	f := waitFrame(t, s, term.ID, "LC_CTYPE", func(f terminal.Frame) bool {
		for _, line := range plain(f) {
			if strings.HasPrefix(line, "CT:LC_CTYPE=") {
				return true
			}
		}
		return false
	})
	for _, line := range plain(f) {
		if strings.HasPrefix(line, "CT:LC_CTYPE=") {
			t.Logf("%s", line)
			if !isUTF8(strings.TrimPrefix(line, "CT:LC_CTYPE=")) {
				t.Fatalf("the shell's LC_CTYPE is not UTF-8: %s", line)
			}
		}
	}
	// A LANG naming a locale this machine does not have reads as UTF-8
	// above and is C underneath: `locale` says so on stderr.
	keys(t, s, term.ID, `echo "E=$(locale 2>&1 >/dev/null | grep -c Cannot)="`+"\r")
	waitFrame(t, s, term.ID, "no locale errors", func(f terminal.Frame) bool { return hasLine(f, "E=0=") })
	if err := s.Keys(context.Background(), term.ID, append([]byte("echo "), 0xe4, 0xb8, 0xad, '\r')); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, s, term.ID, "中 echoed", func(f terminal.Frame) bool { return hasLine(f, "中") })
}

// Acceptance 7: the person's own tmux server never has a terminal in it, and
// the session inventory, which reads that server, has none of ours.
func TestTheDefaultServerAndTheInventoryNeverSeeATerminal(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	tty, err := raw(t, s, "display-message", "-p", "-t", "="+term.ID.SessionName()+":", "#{pane_tty}")
	if err != nil || tty == "" {
		t.Fatalf("pane tty: %q %v", tty, err)
	}
	// Listing only. No -S: the default server, whatever it has.
	out, _ := exec.Command(s.binary, "list-sessions", "-F", "#{session_name}").CombinedOutput()
	for _, name := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(name, "clt-") {
			t.Fatalf("the default server lists %s", name)
		}
	}
	inv, _ := tmuxterm.NewTmux().Inventory(context.Background())
	for _, sess := range inv.Sessions {
		if "/dev/"+sess.TTY == tty || sess.TTY == strings.TrimPrefix(tty, "/dev/") {
			t.Fatalf("the inventory has the terminal's pane: %+v", sess)
		}
	}
}

func TestTheNinthTerminalIsRefused(t *testing.T) {
	s := newServer(t, testDir(t))
	for range terminal.MaxTerminals {
		open(t, s, 40, 10)
	}
	_, err := s.Open(context.Background(), ports.OpenTerminal{ProjectPath: t.TempDir(), ProjectID: "p", Cols: 40, Rows: 10})
	if !isCode(err, terminal.CodeFull) {
		t.Fatalf("the ninth open: %v", err)
	}
	if list, _ := s.List(context.Background()); len(list) != terminal.MaxTerminals {
		t.Fatalf("%d open", len(list))
	}
}

func TestPasteHistoryAndTheChangeSignal(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	ctx := context.Background()

	wake, stop, err := s.Changed(ctx, term.ID)
	if err != nil || wake == nil {
		t.Fatalf("Changed: %v", err)
	}
	defer stop()
	// Drain what the shell's start drew.
	time.Sleep(300 * time.Millisecond)
	select {
	case <-wake:
	default:
	}
	keys(t, s, term.ID, "x")
	select {
	case <-wake:
	case <-time.After(3 * time.Second):
		t.Fatal("typing drew nothing the signal heard")
	}
	keys(t, s, term.ID, "\x15")

	// A shell that asked for bracketed paste takes a pasted newline as
	// text, not as Enter, so the Enter is its own key.
	if err := s.Paste(ctx, term.ID, "echo pas\"\"ted"); err != nil {
		t.Fatal(err)
	}
	keys(t, s, term.ID, "\r")
	waitFrame(t, s, term.ID, "pasted", func(f terminal.Frame) bool { return hasLine(f, "pasted") })

	keys(t, s, term.ID, "seq 1 2500\r")
	waitFrame(t, s, term.ID, "2500", func(f terminal.Frame) bool { return hasLine(f, "2500") })
	lines, err := s.History(ctx, term.ID, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) > terminal.MaxHistoryLines+24 || len(lines) < terminal.MaxHistoryLines {
		t.Fatalf("history of %d lines", len(lines))
	}
	if _, err := s.History(ctx, term.ID, 0); !isCode(err, terminal.CodeInvalid) {
		t.Fatalf("history of 0 lines: %v", err)
	}

	stop()
	if err := s.Close(ctx, term.ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.sock), "signal", "*.fifo")); len(left) != 0 {
		t.Fatalf("FIFOs left: %v", left)
	}
}

func TestOversizedInputIsRefusedBeforeAnythingIsTyped(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	ctx := context.Background()
	if err := s.Keys(ctx, term.ID, make([]byte, terminal.MaxInputBytes+1)); !isCode(err, terminal.CodeInputTooLarge) {
		t.Fatalf("keys: %v", err)
	}
	if err := s.Paste(ctx, term.ID, strings.Repeat("a", terminal.MaxPasteBytes+1)); !isCode(err, terminal.CodeInputTooLarge) {
		t.Fatalf("paste: %v", err)
	}
	if err := s.Keys(ctx, term.ID, []byte(strings.Repeat("a", terminal.MaxInputBytes))); err != nil {
		t.Fatalf("a batch at the limit: %v", err)
	}
	if err := s.Keys(ctx, "trm_nope", []byte("a")); !isCode(err, terminal.CodeInvalid) {
		t.Fatalf("a malformed id: %v", err)
	}
	if err := s.Keys(ctx, terminal.NewID(), []byte("a")); !isCode(err, terminal.CodeClosed) {
		t.Fatalf("an id with no terminal: %v", err)
	}
}

// No server at all is an empty list, and a server that cannot be reached is
// an error, never an empty list.
func TestNoServerIsEmptyAndAnUnreachableServerIsNot(t *testing.T) {
	dir := testDir(t)
	s := newServer(t, dir)
	list, err := s.List(context.Background())
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("no server: %v %v", list, err)
	}
	open(t, s, 80, 24)
	// A tmux that fails without saying the terminal is absent.
	binary := s.binary
	s.binary = "/usr/bin/false"
	list, err = s.List(context.Background())
	s.binary = binary
	if !isCode(err, terminal.CodeUnreachable) || list != nil {
		t.Fatalf("an unreachable server: %v %v", list, err)
	}
}

// Acceptance 9, measured on demand (CLAWDLINE_TERMINAL_LATENCY=1): from the
// bytes being sent to the first frame that shows them, woken by the change
// signal as the stream above this will be.
func TestEchoLatency(t *testing.T) {
	if os.Getenv("CLAWDLINE_TERMINAL_LATENCY") == "" {
		t.Skip("set CLAWDLINE_TERMINAL_LATENCY=1 to measure")
	}
	s := newServer(t, testDir(t))
	term := open(t, s, 200, 24)
	ctx := context.Background()
	wake, stop, err := s.Changed(ctx, term.ID)
	if err != nil || wake == nil {
		t.Fatalf("Changed: %v", err)
	}
	defer stop()
	keys(t, s, term.ID, ": ")
	waitFrame(t, s, term.ID, "the prompt", func(f terminal.Frame) bool { return strings.Contains(strings.Join(plain(f), "\n"), ": ") })

	var samples []time.Duration
	for i := range 60 {
		if i%10 == 0 && i > 0 {
			keys(t, s, term.ID, "\x15: ")
			time.Sleep(100 * time.Millisecond)
		}
		for len(wake) > 0 {
			<-wake
		}
		token := "<" + strings.Repeat(string(rune('a'+i%26)), 2) + ">"
		start := time.Now()
		keys(t, s, term.ID, token)
		for {
			select {
			case <-wake:
			case <-time.After(100 * time.Millisecond):
			}
			f, err := s.Frame(ctx, term.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(plain(f), "\n"), token) {
				samples = append(samples, time.Since(start))
				break
			}
			if time.Since(start) > 5*time.Second {
				t.Fatalf("%s never showed", token)
			}
		}
	}
	slices.Sort(samples)
	p := func(q float64) time.Duration { return samples[int(q*float64(len(samples)-1))] }
	t.Logf("echo latency over %d keystrokes: p50=%v p95=%v min=%v max=%v", len(samples), p(0.5), p(0.95), samples[0], samples[len(samples)-1])
}

// Acceptance 11's restart, in two processes (CLAWDLINE_TERMINAL_PHASE=open,
// then =find, on the same CLAWDLINE_TERMINAL_DIR): the process that opened a
// terminal ends, and a new one lists it by the same id with its `sleep 600`
// still running. The server holds the terminals; the daemon only asks it.
func TestATerminalOutlivesTheProcessThatOpenedIt(t *testing.T) {
	phase, dir := os.Getenv("CLAWDLINE_TERMINAL_PHASE"), os.Getenv("CLAWDLINE_TERMINAL_DIR")
	if phase == "" || dir == "" {
		t.Skip("set CLAWDLINE_TERMINAL_PHASE=open|find and CLAWDLINE_TERMINAL_DIR")
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "probe")
	switch phase {
	case "open":
		term := open(t, s, 80, 24)
		keys(t, s, term.ID, "sleep 600\r")
		deadline := time.Now().Add(10 * time.Second)
		pid := sleeper(t, s, term.ID)
		for pid == 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			pid = sleeper(t, s, term.ID)
		}
		if pid == 0 {
			t.Fatal("sleep 600 never ran")
		}
		if err := os.WriteFile(record, []byte(string(term.ID)+" "+strconv.Itoa(pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("opened %s with sleep 600 as pid %d, from pid %d", term.ID, pid, os.Getpid())
	case "find":
		t.Cleanup(func() { _, _ = raw(t, s, "kill-server") })
		data, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		idText, pidText, _ := strings.Cut(string(data), " ")
		id := terminal.ID(idText)
		pid, _ := strconv.Atoi(pidText)
		list, err := s.List(context.Background())
		if err != nil || len(list) != 1 || list[0].ID != id {
			t.Fatalf("after the restart: %+v %v", list, err)
		}
		if !alive(pid) || sleeper(t, s, id) != pid {
			t.Fatalf("sleep 600 (pid %d) did not survive", pid)
		}
		t.Logf("pid %d found %s with sleep 600 still pid %d", os.Getpid(), id, pid)
	}
}

// The capacity row counts what is open on this directory's server, and reads
// no server as a known 0.
func TestTheCountRowReadsTheOwnedServer(t *testing.T) {
	dir := testDir(t)
	s := newServer(t, dir)
	if r := CountReading(context.Background(), dir); !r.Known || r.Used != 0 {
		t.Fatalf("no server: %+v", r)
	}
	open(t, s, 80, 24)
	open(t, s, 80, 24)
	if r := CountReading(context.Background(), dir); !r.Known || r.Used != 2 {
		t.Fatalf("two open: %+v", r)
	}
}

// A shell that exits on its own takes its pane with it, and the change signal
// has to say so: a viewer is told `exited` by reading the screen after a wake,
// and without one it would wait for the next beat. Measured from the Enter to
// the wake after which the terminal reads as gone.
func TestAShellThatExitsWakesTheChangeSignal(t *testing.T) {
	s := newServer(t, testDir(t))
	term := open(t, s, 80, 24)
	ctx := context.Background()
	wake, stop, err := s.Changed(ctx, term.ID)
	if err != nil || wake == nil {
		t.Fatalf("Changed: %v", err)
	}
	defer stop()
	// Once the shell reads what it is typed: a shell still starting takes
	// the exit only when it is ready, and that wait is not the signal's.
	keys(t, s, term.ID, "echo ready\r")
	waitFrame(t, s, term.ID, "ready", func(f terminal.Frame) bool { return hasLine(f, "ready") })
	time.Sleep(100 * time.Millisecond)
	for len(wake) > 0 {
		<-wake
	}
	began := time.Now()
	keys(t, s, term.ID, "exit\r")
	deadline := time.After(time.Second)
	for {
		select {
		case <-wake:
			if _, err := s.Frame(ctx, term.ID); isCode(err, terminal.CodeClosed) {
				t.Logf("the exit woke the signal %v after the Enter", time.Since(began).Round(time.Millisecond))
				return
			}
		case <-deadline:
			t.Fatal("the shell exited and the change signal stayed quiet for a second")
		}
	}
}
