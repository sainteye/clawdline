package terminal

import (
	"context"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// Warm shells: a login shell started ahead of time in a Project's directory,
// so that starting a Session there does not wait for the person's rc files.
//
// A new tmux Session is a login shell first and the assistant second
// (openPane). On 2026-11-21 that shell took 0.9–2.1 s to read one person's rc
// files on a busy machine, two thirds of the time from the press to Claude
// Code's composer. A warm shell has already read them.
//
// **One per directory, started in that directory.** rc files may depend on
// the directory a shell starts in, so a shell warmed elsewhere and moved with
// `cd` would not be the shell the person gets from a cold start. A start
// whose directory has no warm shell opens a cold pane exactly as before.
//
// The shells live in a tmux session of their own, WarmSessionName, each in a
// window marked @clawdline_warm. Taking one moves its window to where a cold
// `new-window` would have gone, and from there it is an ordinary new pane:
// the launch line is pasted and confirmed the same way. A daemon that starts
// again finds the marked windows the previous one left and keeps using them.

// WarmSessionName is the tmux session the warm shells wait in.
const WarmSessionName = "clawdline-warm"

// MaxWarmShells is how many directories keep a warm shell at once
// (`terminal.warm_shells`). Past it the shell whose directory was started in
// longest ago is closed.
const MaxWarmShells = 3

// WarmShellSecondsLimit is how long a warm shell is kept before it is closed
// and started again (`terminal.warm_shell_seconds`), so edits to rc files reach
// the next Session without anyone restarting anything.
const WarmShellSecondsLimit = 30 * 60

// warmReplenishDelay is how long after a start its directory's next warm
// shell is started.
var warmReplenishDelay = 3 * time.Second

// warmShellNames are the programs a warm pane may be running when it is
// taken: a shell at its prompt, with nothing of its own started yet.
var warmShellNames = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "dash": true,
	"ksh": true, "tcsh": true, "csh": true, "nu": true, "pwsh": true}

// WarmShells keeps the warm shells of one daemon.
type WarmShells struct {
	// Enabled is the `warm_shells` setting; nil is on.
	Enabled func() bool
	// Lang is the launcher's LANG for new panes (Launcher.Lang).
	Lang func(context.Context) string

	now      func() time.Time
	mu       sync.Mutex
	adopted  bool
	shells   map[string]warmShell // directory -> its warm shell
	replenno sync.Mutex           // one replenishment at a time
}

type warmShell struct {
	pane    string
	started time.Time
	used    time.Time // when its directory was last started in; orders eviction
}

func NewWarmShells() *WarmShells {
	return &WarmShells{now: time.Now, shells: map[string]warmShell{}}
}

func (w *WarmShells) on() bool { return w != nil && (w.Enabled == nil || w.Enabled()) }

func (w *WarmShells) clock() time.Time {
	if w.now == nil {
		return time.Now()
	}
	return w.now()
}

func warmKey(cwd string) string { return filepath.Clean(cwd) }

// adopt reads the warm windows a previous daemon left, once.
func (w *WarmShells) adopt(ctx context.Context, bin TmuxChoice) {
	w.mu.Lock()
	if w.adopted {
		w.mu.Unlock()
		return
	}
	w.adopted = true
	w.mu.Unlock()
	out, err := runTmux(ctx, bin, "list-panes", "-a", "-F",
		"#{pane_id} #{?@clawdline_warm,1,0} #{?@clawdline_warm_at,#{@clawdline_warm_at},0} #{@clawdline_warm_cwd}")
	if err != nil {
		return
	}
	now := w.clock()
	var stale []string
	w.mu.Lock()
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, " ", 4)
		if len(f) != 4 || f[1] != "1" || !strings.HasPrefix(f[0], "%") || f[3] == "" {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		started := time.Unix(at, 0)
		key := warmKey(f[3])
		if _, dup := w.shells[key]; dup || at <= 0 || now.Sub(started) >= WarmShellSecondsLimit*time.Second {
			stale = append(stale, f[0])
			continue
		}
		w.shells[key] = warmShell{pane: f[0], started: started, used: started}
	}
	stale = append(stale, w.evictLocked()...)
	w.mu.Unlock()
	for _, pane := range stale {
		killPane(ctx, bin, pane)
	}
}

// evictLocked removes shells past MaxWarmShells, least recently used first,
// and answers their panes for the caller to close.
func (w *WarmShells) evictLocked() []string {
	if len(w.shells) <= MaxWarmShells {
		return nil
	}
	keys := make([]string, 0, len(w.shells))
	for k := range w.shells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return w.shells[keys[i]].used.Before(w.shells[keys[j]].used) })
	var gone []string
	for _, k := range keys[:len(keys)-MaxWarmShells] {
		gone = append(gone, w.shells[k].pane)
		delete(w.shells, k)
	}
	return gone
}

// take answers the warm pane for cwd, already moved to target (a session id
// or name, ""), and unmarked. A shell that is gone, busy, too old or
// somewhere else is closed and answers false: the caller opens a cold pane.
func (w *WarmShells) take(ctx context.Context, bin TmuxChoice, cwd, target string) (string, bool) {
	if !w.on() || target == "" {
		return "", false
	}
	w.adopt(ctx, bin)
	key := warmKey(cwd)
	w.mu.Lock()
	shell, ok := w.shells[key]
	delete(w.shells, key)
	w.mu.Unlock()
	if !ok {
		return "", false
	}
	if w.clock().Sub(shell.started) >= WarmShellSecondsLimit*time.Second {
		killPane(ctx, bin, shell.pane)
		return "", false
	}
	out, err := runTmux(ctx, bin, "display-message", "-p", "-t", shell.pane,
		"#{pane_id} #{pane_dead} #{?@clawdline_warm,1,0} #{pane_current_command} #{session_name}")
	// Spaces, not tabs: under LC_ALL=C tmux rewrites control characters in
	// format output. The session name, which may hold a space, comes last.
	f := strings.SplitN(strings.TrimRight(out, "\n"), " ", 5)
	if err != nil || len(f) != 5 || f[0] != shell.pane || f[1] != "0" || f[2] != "1" || f[4] != WarmSessionName ||
		!warmShellNames[strings.TrimPrefix(filepath.Base(f[3]), "-")] {
		if err == nil {
			killPane(ctx, bin, shell.pane)
		}
		return "", false
	}
	if _, err := runTmux(ctx, bin, "set-option", "-p", "-u", "-t", shell.pane, "@clawdline_warm"); err != nil {
		killPane(ctx, bin, shell.pane)
		return "", false
	}
	if _, err := runTmux(ctx, bin, "move-window", "-s", shell.pane, "-t", target+":"); err != nil {
		log.Printf("tmux: a warm shell could not be moved to %s: %v", target, err)
		killPane(ctx, bin, shell.pane)
		return "", false
	}
	return shell.pane, true
}

// Replenish makes sure cwd has a warm shell, and that the record holds no more
// than MaxWarmShells. With the setting off it closes every warm shell instead.
// It is called after a start in cwd, off the start's path.
func (w *WarmShells) Replenish(ctx context.Context, bin TmuxChoice, cwd string) {
	if w == nil || !bin.Found() {
		return
	}
	w.replenno.Lock()
	defer w.replenno.Unlock()
	if !w.on() {
		w.clear(ctx, bin)
		return
	}
	w.adopt(ctx, bin)
	key := warmKey(cwd)
	now := w.clock()
	w.mu.Lock()
	if shell, ok := w.shells[key]; ok && now.Sub(shell.started) < WarmShellSecondsLimit*time.Second {
		shell.used = now
		w.shells[key] = shell
		w.mu.Unlock()
		return
	}
	old, had := w.shells[key]
	delete(w.shells, key)
	w.mu.Unlock()
	if had {
		killPane(ctx, bin, old.pane)
	}
	pane, err := w.open(ctx, bin, cwd, now)
	if err != nil {
		log.Printf("tmux: no warm shell for %s: %v", cwd, err)
		return
	}
	w.mu.Lock()
	w.shells[key] = warmShell{pane: pane, started: now, used: now}
	gone := w.evictLocked()
	w.mu.Unlock()
	for _, p := range gone {
		killPane(ctx, bin, p)
	}
}

// open starts one warm shell in cwd.
func (w *WarmShells) open(ctx context.Context, bin TmuxChoice, cwd string, now time.Time) (string, error) {
	if err := bin.PrepareSocket(); err != nil {
		return "", err
	}
	if err := clearTmuxAssistantIdentity(ctx, bin); err != nil {
		return "", err
	}
	l := Launcher{Lang: w.Lang}
	create := []string{"new-window", "-d", "-t", "=" + WarmSessionName + ":", "-P", "-F", "#{pane_id}"}
	if _, err := runTmux(ctx, bin, "has-session", "-t", "="+WarmSessionName); err != nil {
		create = []string{"new-session", "-d", "-s", WarmSessionName, "-P", "-F", "#{pane_id}"}
	}
	args := append(append(create, l.paneLocale(ctx)...), "-c", cwd)
	out, said, err := runTmuxSaid(ctx, bin, "", args...)
	if err != nil {
		return "", err
	}
	id, err := newPaneID(out, said)
	if err != nil {
		return "", err
	}
	for _, o := range [][2]string{{"@clawdline_warm", "1"}, {"@clawdline_warm_cwd", warmKey(cwd)},
		{"@clawdline_warm_at", strconv.FormatInt(now.Unix(), 10)}} {
		if _, err := runTmux(ctx, bin, "set-option", "-p", "-t", id, o[0], o[1]); err != nil {
			killPane(ctx, bin, id)
			return "", err
		}
	}
	return id, nil
}

// Clear closes every warm shell, for the setting being turned off.
func (w *WarmShells) Clear(ctx context.Context, bin TmuxChoice) {
	if w == nil || !bin.Found() {
		return
	}
	w.replenno.Lock()
	defer w.replenno.Unlock()
	w.clear(ctx, bin)
}

func (w *WarmShells) clear(ctx context.Context, bin TmuxChoice) {
	w.adopt(ctx, bin)
	w.mu.Lock()
	var panes []string
	for k, s := range w.shells {
		panes = append(panes, s.pane)
		delete(w.shells, k)
	}
	w.mu.Unlock()
	for _, p := range panes {
		killPane(ctx, bin, p)
	}
}

// killPane closes a warm pane this record holds. Only panes still marked warm
// are closed, so a pane that was taken meanwhile is never touched.
func killPane(ctx context.Context, bin TmuxChoice, pane string) {
	ctx = context.WithoutCancel(ctx)
	out, err := runTmux(ctx, bin, "display-message", "-p", "-t", pane, "#{pane_id} #{?@clawdline_warm,1,0}")
	if err != nil || strings.TrimRight(out, "\n") != pane+" 1" {
		return
	}
	if _, err := runTmux(ctx, bin, "kill-pane", "-t", pane); err != nil {
		log.Printf("tmux: the warm shell %s could not be closed: %v", pane, err)
	}
}

// homeSession is where a cold `new-window` goes once a warm session exists:
// the session tmux itself would choose, the most recently active one, with the
// warm session left out. "" when no warm session exists, which leaves
// new-window's own choice exactly as it was; ok is false when the warm session
// is the only one.
func homeSession(ctx context.Context, bin TmuxChoice) (target string, ok bool) {
	out, err := runTmux(ctx, bin, "list-sessions", "-F", "#{session_activity} #{session_id} #{session_name}")
	if err != nil {
		return "", true
	}
	warm := false
	best, bestAt := "", int64(-1)
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			continue
		}
		if f[2] == WarmSessionName {
			warm = true
			continue
		}
		at, _ := strconv.ParseInt(f[0], 10, 64)
		if at > bestAt {
			best, bestAt = f[1], at
		}
	}
	if !warm {
		return "", true
	}
	return best, best != ""
}

// startedSessionName is the session a cold start makes when the warm session
// is the only one on the server (homeSession's !ok).
const startedSessionName = projects.TmuxStartedSessionName

// TmuxBinary is the tmux this launcher runs, for work on its server outside a
// launch (WarmShells.Clear).
func (l Launcher) TmuxBinary(ctx context.Context) TmuxChoice { return l.binary(ctx) }
