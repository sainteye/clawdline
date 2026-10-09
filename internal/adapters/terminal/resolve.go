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

	"github.com/sainteye/clawdline/internal/config"
)

// Which tmux this daemon runs (docs/design-decisions.md D33).
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

	mu    sync.Mutex
	known map[string]versionMemo
}

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
