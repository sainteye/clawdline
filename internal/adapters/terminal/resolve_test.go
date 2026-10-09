//go:build !windows

package terminal

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// versionedTmux writes an executable that answers `-V` with version and
// records every other call's arguments, one line each, in the file it
// returns. It never starts a server.
func versionedTmux(t *testing.T, version string) (path, calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	path = filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nif [ \"$1\" = -V ]; then echo '" + version + "'; exit 0; fi\n" +
		"echo \"$*\" >>" + calls + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, calls
}

func resolverWith(system, carried, sock string) *tmuxResolver {
	fallbacks := []string{}
	if system != "" {
		fallbacks = []string{system}
	}
	return &tmuxResolver{
		name:      "clawdline-no-such-tmux",
		fallbacks: fallbacks,
		bundled:   func() string { return carried },
		socket:    func() string { return sock },
	}
}

// The person's tmux, new enough, is the one used, on the default server: it
// is where their own sessions are.
func TestASystemTmuxNewEnoughIsPreferred(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 3.4")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	got := resolverWith(system, carried, "/tmp/x/sessions.sock").resolve(context.Background())
	if got.Path != system || got.Bundled || got.Socket != "" || got.Version != "tmux 3.4" || got.Passed != "" {
		t.Fatalf("%+v", got)
	}
	if args := got.Args("ls"); strings.Join(args, " ") != "ls" {
		t.Fatalf("the default server was given a socket: %v", args)
	}
}

// A tmux older than the floor is passed over for the carried one, which runs
// on its own socket and says why.
func TestATmuxTooOldFallsToTheCarriedOneOnItsOwnSocket(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 2.9a")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	got := resolverWith(system, carried, "/tmp/x/sessions.sock").resolve(context.Background())
	if got.Path != carried || !got.Bundled || got.Socket != "/tmp/x/sessions.sock" || got.Version != "tmux 3.6a" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Passed, "tmux 2.9a") || !strings.Contains(got.Passed, "older than tmux 3.0") {
		t.Fatalf("passed over without saying why: %q", got.Passed)
	}
	if args := got.Args("ls"); strings.Join(args, " ") != "-S /tmp/x/sessions.sock ls" {
		t.Fatalf("the carried tmux was not pointed at its own socket: %v", args)
	}
}

// With no tmux on the machine the carried one is used; with neither there is
// none, and nothing pretends otherwise.
func TestNoSystemTmuxUsesTheCarriedOneAndNeitherIsNone(t *testing.T) {
	carried, _ := versionedTmux(t, "tmux 3.6a")
	got := resolverWith("", carried, "/tmp/x/sessions.sock").resolve(context.Background())
	if got.Path != carried || !got.Bundled || got.Passed != "" {
		t.Fatalf("%+v", got)
	}
	none := resolverWith("", "", "/tmp/x/sessions.sock").resolve(context.Background())
	if none.Found() || none.Describe() != "no tmux" {
		t.Fatalf("%+v", none)
	}
}

// A tmux too old with nothing carried is still run, as before releases
// carried one: listing and typing never needed 3.0, and the owned terminal
// refuses it by its own check.
func TestATmuxTooOldWithNothingCarriedIsStillRun(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 2.9a")
	got := resolverWith(system, "", "/tmp/x/sessions.sock").resolve(context.Background())
	if got.Path != system || got.Bundled || got.Passed == "" {
		t.Fatalf("%+v", got)
	}
}

// A version that cannot be read is not a version proved too old.
func TestAnUnreadVersionKeepsTheSystemTmux(t *testing.T) {
	system, _ := versionedTmux(t, "tmux master")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	got := resolverWith(system, carried, "/tmp/x/sessions.sock").resolve(context.Background())
	if got.Path != system || got.Bundled {
		t.Fatalf("%+v", got)
	}
}

// `tmux -V` is asked once per file, not once per call.
func TestTheVersionIsAskedOncePerBinary(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 3.6a")
	asked := 0
	r := resolverWith(system, "", "")
	r.version = func(ctx context.Context, path string) (string, error) {
		asked++
		return TmuxVersion(ctx, path)
	}
	for i := 0; i < 5; i++ {
		r.resolve(context.Background())
	}
	if asked != 1 {
		t.Fatalf("tmux -V asked %d times", asked)
	}
}

// Every call the backend makes reaches the carried tmux's own socket: the
// listing, a capture, a keystroke, a new session. None of them may reach the
// default server, where another tmux version would refuse it.
func TestEveryCallOnTheCarriedTmuxNamesItsSocket(t *testing.T) {
	carried, calls := versionedTmux(t, "tmux 3.6a")
	t.Setenv("PATH", t.TempDir())
	t.Setenv(BundledTmuxEnv, carried)
	sock := filepath.Join(t.TempDir(), "sessions.sock")
	saved := defaultResolver
	defaultResolver = &tmuxResolver{name: "tmux", fallbacks: []string{}, socket: func() string { return sock }}
	t.Cleanup(func() { defaultResolver = saved })

	tmux := NewTmux()
	ctx := context.Background()
	if _, err := tmux.Inventory(ctx); err != nil {
		t.Fatal(err)
	}
	tmux.Capture(ctx, session.Session{ID: "%3", Backend: session.BackendTmux})
	tmux.Screen(ctx, session.Session{ID: "%3", Backend: session.BackendTmux}, 10)
	_ = tmux.run(ctx, "kill-pane", "-t", "%3")
	tmux.PipedPanes(ctx)
	if line, err := (Launcher{Tmux: tmux}).PrepareTmuxViewer(ctx, "s"); err != nil ||
		line != "exec '"+carried+"' -S '"+sock+"' attach -t '=s'" {
		t.Fatalf("viewer line %q, %v", line, err)
	}

	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 6 {
		t.Fatalf("expected every call recorded, got %q", lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "-S "+sock+" ") {
			t.Errorf("a call did not name the carried tmux's socket: %q", l)
		}
	}
}

func TestBundledTmuxIsFoundBesideTheExecutable(t *testing.T) {
	dir := t.TempDir()
	got := bundledTmuxCandidates(filepath.Join(dir, "releases", "v1.0.0"))
	if got[0] != filepath.Join(dir, "releases", "v1.0.0", "libexec", "tmux") {
		t.Fatalf("release archive: %v", got)
	}
	app := bundledTmuxCandidates("/Applications/Clawdline Next.app/Contents/MacOS")
	if filepath.Clean(app[1]) != "/Applications/Clawdline Next.app/Contents/Helpers/tmux" {
		t.Fatalf("app bundle: %v", app)
	}
	t.Setenv(BundledTmuxEnv, filepath.Join(dir, "missing"))
	if BundledTmux() != "" {
		t.Fatal("a named tmux that is not there was answered")
	}
}

func TestTheWordsAPersonTypesAreQuoted(t *testing.T) {
	c := TmuxChoice{Path: "/a b/tmux", Bundled: true, Socket: "/s'ock"}
	if got := c.Words(); got != `'/a b/tmux' -S '/s'\''ock'` {
		t.Fatal(got)
	}
}

// A real tmux standing in for the carried one: opened, listed, read and closed
// on its own socket, and nothing on the default server. Skipped where there is
// no tmux to stand in.
func TestTheCarriedTmuxRunsOnItsOwnServer(t *testing.T) {
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	// Under /tmp: a per-user temporary directory is too deep for a socket.
	dir, err := os.MkdirTemp("/tmp", "clt-carried-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// The default server, were anything to reach it, is a private one too.
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv(BundledTmuxEnv, real)
	sock := filepath.Join(dir, SessionsSocketName)
	saved := defaultResolver
	defaultResolver = &tmuxResolver{name: "tmux", fallbacks: []string{}, socket: func() string { return sock }}
	t.Cleanup(func() { defaultResolver = saved })
	t.Cleanup(func() { _ = exec.Command(real, "-S", sock, "kill-server").Run() })

	tmux := NewTmux()
	ctx := context.Background()
	opened, err := tmux.Open(ctx, ports.OpenRequest{Name: "carried", Cwd: dir, Command: "printf 'carried-ok\\n'; /bin/sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := tmux.Inventory(ctx)
	if err != nil || !inv.Complete || len(inv.Sessions) != 1 || inv.Sessions[0].ID != opened.ID {
		t.Fatalf("the carried server's pane was not listed: %+v %v", inv, err)
	}
	var screen string
	for i := 0; i < 50 && !strings.Contains(screen, "carried-ok"); i++ {
		screen, _ = tmux.Capture(ctx, inv.Sessions[0])
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(screen, "carried-ok") {
		t.Fatalf("the pane was not read: %q", screen)
	}
	if out, err := exec.Command(real, "ls").CombinedOutput(); err == nil {
		t.Fatalf("the default server was started: %s", out)
	}
}

// liveSocket is a listening Unix socket under /tmp, where the path is short
// enough for one: it stands in for a carried server's sessions.sock.
func liveSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "clt-kept-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, SessionsSocketName)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return sock
}

// A machine that ran on the carried tmux and then installed its own keeps the
// carried server while it still holds a session, so those sessions stay
// listed and driven; it says which tmux waits and for what.
func TestTheCarriedServerIsKeptWhileItHoldsASession(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 3.5a")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	sock := liveSocket(t)
	asked := 0
	r := resolverWith(system, carried, sock)
	r.sessions = func(_ context.Context, path, socket string) (bool, bool) {
		asked++
		if path != carried || socket != sock {
			t.Fatalf("asked %s on %s", path, socket)
		}
		return true, true
	}
	for i := 0; i < 3; i++ {
		got := r.resolve(context.Background())
		if got.Path != carried || !got.Bundled || got.Socket != sock {
			t.Fatalf("the carried server was dropped: %+v", got)
		}
		if !strings.Contains(got.Passed, system) || !strings.Contains(got.Passed, "tmux 3.5a") || !strings.Contains(got.Passed, sock) {
			t.Fatalf("kept without saying why: %q", got.Passed)
		}
	}
	if asked != 1 {
		t.Fatalf("the carried server was asked %d times in three calls", asked)
	}
}

// Once the carried server holds nothing, the machine's own tmux is used, and
// the empty server is not asked again while its socket is the same file.
func TestAnEmptyCarriedServerGivesWayToTheSystemTmux(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 3.5a")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	sock := liveSocket(t)
	asked := 0
	r := resolverWith(system, carried, sock)
	r.sessions = func(context.Context, string, string) (bool, bool) { asked++; return false, true }
	for i := 0; i < 3; i++ {
		if got := r.resolve(context.Background()); got.Path != system || got.Bundled || got.Passed != "" {
			t.Fatalf("%+v", got)
		}
	}
	if asked != 1 {
		t.Fatalf("an empty server was asked %d times", asked)
	}
}

// No socket is no carried server: nothing is asked. A server that will not
// answer is no proof its sessions are gone, so the socket alone keeps it.
func TestNoSocketAsksNothingAndASilentServerIsKept(t *testing.T) {
	system, _ := versionedTmux(t, "tmux 3.5a")
	carried, _ := versionedTmux(t, "tmux 3.6a")
	r := resolverWith(system, carried, filepath.Join(t.TempDir(), SessionsSocketName))
	r.sessions = func(context.Context, string, string) (bool, bool) {
		t.Fatal("asked a server with no socket")
		return false, false
	}
	if got := r.resolve(context.Background()); got.Path != system {
		t.Fatalf("%+v", got)
	}
	silent := resolverWith(system, carried, liveSocket(t))
	silent.sessions = func(context.Context, string, string) (bool, bool) { return false, false }
	if got := silent.resolve(context.Background()); got.Path != carried || !got.Bundled {
		t.Fatalf("a server that did not answer was dropped: %+v", got)
	}
}

// The real question, asked of a real tmux on a private socket: a session is
// there, then the server is gone and that is an answer too.
func TestTheCarriedServerIsAskedForItsSessions(t *testing.T) {
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "clt-ask-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	sock := filepath.Join(dir, SessionsSocketName)
	t.Cleanup(func() { _ = exec.Command(real, "-S", sock, "kill-server").Run() })
	ctx := context.Background()
	if has, known := carriedHasSessions(ctx, real, sock); has || !known {
		t.Fatalf("no server read as has=%v known=%v", has, known)
	}
	if out, err := exec.Command(real, "-S", sock, "-f", "/dev/null", "new-session", "-d", "-s", "kept", "/bin/sleep 30").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if has, known := carriedHasSessions(ctx, real, sock); !has || !known {
		t.Fatalf("a held session read as has=%v known=%v", has, known)
	}
	if out, err := exec.Command(real, "-S", sock, "kill-server").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if has, known := carriedHasSessions(ctx, real, sock); has || !known {
		t.Fatalf("a stopped server read as has=%v known=%v", has, known)
	}
}

// The line a person is handed reaches the server the daemon lists: on a state
// directory of the daemon's own, it carries that directory.
func TestTheAttachLineReachesTheDaemonsOwnStateDirectory(t *testing.T) {
	carried := TmuxChoice{Path: "/x/libexec/tmux", Bundled: true, Socket: "/s/tmux/sessions.sock"}
	for _, c := range []struct {
		choice TmuxChoice
		dir    string
		want   string
	}{
		{TmuxChoice{Path: "/usr/bin/tmux"}, "/s", "tmux"},
		{carried, "", "clawdline tmux"},
		{carried, "/tmp/a dir's", `CLAWDLINE_NEXT_DIR='/tmp/a dir'\''s' clawdline tmux`},
	} {
		if got := tmuxClient(c.choice, c.dir); got != c.want {
			t.Errorf("tmuxClient(%+v, %q) = %q, want %q", c.choice, c.dir, got, c.want)
		}
	}
}
