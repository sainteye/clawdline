package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
)

// The attach line a person is handed names `clawdline tmux` when the daemon
// runs the carried tmux: a bare `tmux` either does not exist on that machine
// or reaches a different server.
func init() {
	projects.TmuxClient = func() string {
		return tmuxClient(ResolveTmux(context.Background()), os.Getenv("CLAWDLINE_NEXT_DIR"))
	}
}

// tmuxClient is the words that run choice from a person's shell. The carried
// server's socket is under the daemon's state directory, which `clawdline
// tmux` finds the way the daemon did: a daemon started on a directory of its
// own names it in the line, or the line reaches the default directory's
// server, which is not the one listing these sessions.
func tmuxClient(choice TmuxChoice, nextDir string) string {
	if !choice.Bundled {
		return "tmux"
	}
	if nextDir == "" {
		return "clawdline tmux"
	}
	return "CLAWDLINE_NEXT_DIR=" + shellQuote(nextDir) + " clawdline tmux"
}

// Which tmux this daemon runs (docs/design-decisions.md D72).
//
// A release carries a tmux of its own (tools/release/tmux), so a machine
// without one still gets a working Clawdline. The person's tmux, when there is
// one new enough, is still the one used: it is the server their own sessions
// are on, and a daemon that listed some other server would show them nothing
// they started. The carried one is the fallback, and it is never pointed at
// the default server: two tmux versions on one socket refuse each other with
// a protocol mismatch, and the person's `tmux ls` would start failing the day
// Clawdline started a server there. It always runs as
// `<bundled> -S <CLAWDLINE_NEXT_DIR>/tmux/sessions.sock`.

// BundledTmuxEnv names a carried tmux explicitly. A release finds its own
// beside the executable (bundledTmuxCandidates); this is for a checkout's
// build, which has none there, and for tests.
const BundledTmuxEnv = "CLAWDLINE_NEXT_TMUX"

// SessionsSocketName is the bundled tmux's server for assistant sessions, in
// `<CLAWDLINE_NEXT_DIR>/tmux/`. The owned terminals have `term.sock` beside it
// (terminal/owned): a shell a person types into and an assistant the broker
// dispatched are listed by different routes and must not share a server.
const SessionsSocketName = "sessions.sock"

// TmuxChoice is the tmux every call runs, and the server it reaches.
type TmuxChoice struct {
	// Path is the absolute executable, "" when there is no tmux at all.
	Path string
	// Version is what `tmux -V` printed, trimmed; "" when it was not read.
	Version string
	// Bundled says Path is the tmux this release carries, and Socket is then
	// the only server it is ever pointed at.
	Bundled bool
	Socket  string
	// Passed is why a tmux on this machine was not the one chosen, said
	// whichever was chosen instead.
	Passed string
}

// Found is whether there is a tmux to run.
func (c TmuxChoice) Found() bool { return c.Path != "" }

// Args is args with the server this choice reaches in front of them.
func (c TmuxChoice) Args(args ...string) []string {
	if c.Socket == "" {
		return args
	}
	return append([]string{"-S", c.Socket}, args...)
}

// Command is one tmux invocation against the chosen server.
func (c TmuxChoice) Command(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, c.Path, c.Args(args...)...)
}

// Words is the command line that runs this tmux, quoted for a shell: the
// absolute path, and the socket when there is one. It is what a tab this
// daemon opens types, so the tab's own PATH does not matter.
func (c TmuxChoice) Words() string {
	w := shellQuote(c.Path)
	if c.Socket != "" {
		w += " -S " + shellQuote(c.Socket)
	}
	return w
}

// Describe is one line for a person: which tmux, and from where.
func (c TmuxChoice) Describe() string {
	version := c.Version
	if version == "" {
		version = "tmux (version unread)"
	}
	switch {
	case !c.Found():
		return "no tmux"
	case c.Bundled:
		return version + ", carried by this release, on its own server " + c.Socket
	}
	return version + " at " + c.Path
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// tmuxResolver decides TmuxChoice. Every field is a seam a test replaces;
// the zero value of each is this machine.
type tmuxResolver struct {
	// name is what is looked up on the PATH: "tmux".
	name string
	// fallbacks are where package managers put tmux when this process's PATH
	// does not reach them, as a daemon an app or launchd started often does.
	fallbacks []string
	// bundled is the carried tmux's path, or "".
	bundled func() string
	// socket is the carried tmux's server.
	socket func() string
	// version is `tmux -V`.
	version func(ctx context.Context, path string) (string, error)
	// sessions says whether the carried tmux's server holds a session
	// (carriedHasSessions); known is false when it would not say.
	sessions func(ctx context.Context, path, socket string) (has, known bool)

	mu    sync.Mutex
	known map[string]versionMemo
	// kept is the last answer about the carried server, while its socket is
	// the same file: one entry, never a list.
	kept keptMemo
}

// keptMemo is whether the carried server held a session, asked at `asked`
// of the socket file `ino` was.
type keptMemo struct {
	socket string
	ino    uint64
	asked  time.Time
	has    bool
}

// keptRecheck is how long an answer that the carried server holds a session
// stands before it is asked again. A server whose last session ends removes
// its socket, which ends the answer at once; this is for one that was killed
// and left the file behind.
const keptRecheck = 10 * time.Second

// versionMemo is one binary's version, kept while the file is the same file.
// There is one entry per path the resolver ever probes, and those are the
// fallbacks, the PATH's tmux and the carried one: a handful, by construction.
type versionMemo struct {
	size    int64
	mod     time.Time
	version string
	err     error
}

var tmuxFallbacks = []string{
	"/opt/homebrew/bin/tmux",
	"/usr/local/bin/tmux",
	"/usr/bin/tmux",
	"/opt/local/bin/tmux",
}

// defaultResolver is the one every Tmux without a resolver of its own asks.
var defaultResolver = &tmuxResolver{name: "tmux"}

// ResolveTmux is which tmux this daemon runs, asked now. It costs a PATH
// lookup and, the first time each binary is seen, one `tmux -V`, which starts
// no server.
func ResolveTmux(ctx context.Context) TmuxChoice { return defaultResolver.resolve(ctx) }

func (r *tmuxResolver) resolve(ctx context.Context) TmuxChoice {
	system := r.system()
	var passed string
	var old TmuxChoice
	if system != "" {
		version, err := r.versionOf(ctx, system)
		choice := TmuxChoice{Path: system, Version: strings.TrimSpace(version)}
		major, minor, ok := ParseTmuxVersion(version)
		// A version that could not be read is not a version proved too old:
		// the person's tmux stays the one used, as it was before a release
		// carried one, and the owned terminal says it could not read it.
		if err != nil || !ok || TmuxNewEnough(major, minor) {
			if kept, ok := r.keepCarried(ctx, choice); ok {
				return kept
			}
			return choice
		}
		passed = fmt.Sprintf("%s at %s is older than tmux %d.%d", choice.Version, system, TmuxMinimumMajor, TmuxMinimumMinor)
		old = choice
	}
	if carried := r.carried(); carried != "" {
		version, _ := r.versionOf(ctx, carried)
		return TmuxChoice{Path: carried, Version: strings.TrimSpace(version), Bundled: true,
			Socket: r.socketPath(), Passed: passed}
	}
	// No carried tmux: an old one is still better than none for listing and
	// typing, which never needed 3.0. The owned terminal refuses it by its
	// own version check.
	old.Passed = passed
	return old
}

// keepCarried is the carried tmux, kept while its server still holds a
// session, on a machine whose own tmux would otherwise be chosen.
//
// A machine that ran on the carried tmux and then installed its own would
// otherwise switch every call to the default server the moment the new tmux
// appeared on the PATH, and every session still running on sessions.sock —
// assistants included — would vanish from the list and could no longer be
// typed into or closed. Both servers cannot be listed side by side: each
// numbers its panes from %0, and a pane id is the whole address a session
// has (session.SourceForID), so two servers' %3 would be one session to
// everything above this package. The carried server is kept until its last
// session ends, when tmux removes the socket and the next call reaches the
// machine's own tmux.
func (r *tmuxResolver) keepCarried(ctx context.Context, system TmuxChoice) (TmuxChoice, bool) {
	carried := r.carried()
	if carried == "" {
		return TmuxChoice{}, false
	}
	socket := r.socketPath()
	ino, ok := socketFile(socket)
	if !ok {
		return TmuxChoice{}, false
	}
	r.mu.Lock()
	m := r.kept
	r.mu.Unlock()
	has := m.has
	fresh := m.socket == socket && m.ino == ino && !m.asked.IsZero() &&
		(!m.has || time.Since(m.asked) < keptRecheck)
	if !fresh {
		ask := r.sessions
		if ask == nil {
			ask = carriedHasSessions
		}
		answer, known := ask(ctx, carried, socket)
		if known {
			has = answer
			r.mu.Lock()
			r.kept = keptMemo{socket: socket, ino: ino, asked: time.Now(), has: has}
			r.mu.Unlock()
		} else if m.socket != socket || m.ino != ino {
			// A server that would not answer has no authority to prove its
			// sessions gone (D05 ③): the socket is there, so it is kept.
			has = true
		}
	}
	if !has {
		return TmuxChoice{}, false
	}
	version, _ := r.versionOf(ctx, carried)
	return TmuxChoice{Path: carried, Version: strings.TrimSpace(version), Bundled: true, Socket: socket,
		Passed: fmt.Sprintf("%s at %s is used once the sessions still on %s have ended", system.Version, system.Path, socket)}, true
}

// carriedHasSessions asks the carried server whether it holds a session.
// known is false when it would not say; "no server" is an answer, false.
func carriedHasSessions(ctx context.Context, path, socket string) (has, known bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-S", socket, "list-sessions", "-F", "#{session_id}")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if NoServer(stderr.String()) {
			return false, true
		}
		return false, false
	}
	return strings.TrimSpace(string(out)) != "", true
}

// system is tmux on this process's PATH, then in the places package managers
// put it. Every call runs the absolute path this answers, so one found only
// in a fallback is as usable as one on the PATH.
func (r *tmuxResolver) system() string {
	name := r.name
	if name == "" {
		name = "tmux"
	}
	if found, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(found); err == nil {
			return abs
		}
		return found
	}
	fallbacks := r.fallbacks
	if fallbacks == nil {
		fallbacks = tmuxFallbacks
	}
	for _, path := range fallbacks {
		if found, err := exec.LookPath(path); err == nil {
			return found
		}
	}
	return ""
}

func (r *tmuxResolver) carried() string {
	if r.bundled != nil {
		return r.bundled()
	}
	return BundledTmux()
}

func (r *tmuxResolver) socketPath() string {
	if r.socket != nil {
		return r.socket()
	}
	return filepath.Join(config.Dir(), "tmux", SessionsSocketName)
}

// versionOf is `tmux -V` for path, asked once per file.
func (r *tmuxResolver) versionOf(ctx context.Context, path string) (string, error) {
	info, statErr := os.Stat(path)
	r.mu.Lock()
	if m, ok := r.known[path]; ok && statErr == nil && m.size == info.Size() && m.mod.Equal(info.ModTime()) {
		r.mu.Unlock()
		return m.version, m.err
	}
	r.mu.Unlock()
	ask := r.version
	if ask == nil {
		ask = TmuxVersion
	}
	version, err := ask(ctx, path)
	if statErr == nil && ctx.Err() == nil {
		r.mu.Lock()
		if r.known == nil {
			r.known = map[string]versionMemo{}
		}
		r.known[path] = versionMemo{size: info.Size(), mod: info.ModTime(), version: version, err: err}
		r.mu.Unlock()
	}
	return version, err
}

// BundledTmux is the tmux this release carries, or "" when it carries none.
// BundledTmuxEnv names one explicitly; otherwise it is looked for beside the
// executable: `libexec/tmux` in a release archive (tools/release/build.sh),
// `Contents/Helpers/tmux` in the macOS app (tools/package-macos.sh).
func BundledTmux() string {
	if v := os.Getenv(BundledTmuxEnv); v != "" {
		if executableFile(v) {
			return v
		}
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for _, p := range bundledTmuxCandidates(filepath.Dir(exe)) {
		if executableFile(p) {
			return p
		}
	}
	return ""
}

// bundledTmuxCandidates are where a release puts its tmux, relative to the
// directory its `clawdline` is in.
func bundledTmuxCandidates(dir string) []string {
	return []string{
		filepath.Join(dir, "libexec", "tmux"),
		filepath.Join(dir, "..", "Helpers", "tmux"),
	}
}

func executableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
