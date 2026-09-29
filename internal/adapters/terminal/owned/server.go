//go:build !windows

package owned

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tmuxterm "github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// callTimeout is the longest one tmux invocation may take. A server that has
// not answered by then is unreachable, not absent.
const callTimeout = 5 * time.Second

// Server is the ordinary shells this daemon holds, on a tmux server of its own.
type Server struct {
	sock, conf string
	machine    Machine
	// environ is this daemon's environment; os.Environ unless a test sets it.
	environ func() []string

	// readyMu guards the first successful ready; after it the four values
	// below never change.
	readyMu  sync.Mutex
	readied  bool
	readyErr error
	binary   string
	lang     string
	env      []string
	shell    string

	// mu makes an open one step: counting the terminals and adding one, so
	// two opens at once cannot both be the eighth.
	mu sync.Mutex

	signal ports.PaneSignal
	subsMu sync.Mutex
	subs   map[string]map[*subscription]struct{}
}

type subscription struct{ wake chan struct{} }

var _ ports.OwnedTerminals = (*Server)(nil)

// New is the terminal server that belongs to the daemon whose directory is
// nextDir: its socket is `<nextDir>/tmux/term.sock`, so a daemon started on
// another directory — a test's, a second install — has a server of its own
// without anybody choosing a name. Nothing is started until the first open.
//
// The only error is a directory so deep the socket's path does not fit a Unix
// socket address: terminal_socket_path_too_long.
func New(nextDir string) (*Server, error) {
	dir := filepath.Join(nextDir, "tmux")
	sock := filepath.Join(dir, "term.sock")
	if err := checkSocketPath(sock); err != nil {
		return nil, err
	}
	s := &Server{sock: sock, conf: filepath.Join(dir, "term.conf"), environ: os.Environ,
		subs: map[string]map[*subscription]struct{}{}}
	s.signal = tmuxterm.NewPaneSignal(filepath.Join(dir, "signal"), pipeHost{s})
	if s.signal != nil {
		s.signal.OnMoved(s.moved)
		// FIFOs a previous life of this daemon left are removed. Their far
		// ends died with the reader that went with that life, and tmux takes
		// a dead pipe off the pane on its next write.
		for _, pane := range s.signal.Abandoned() {
			s.signal.Forget(pane)
		}
	}
	return s, nil
}

// CountReading is the `terminal.count` row: how many terminals are open on
// the server under nextDir. It reads the socket and nothing else — no FIFO is
// touched, so a daemon's own Server keeps its change signals.
//
// A machine that cannot open a terminal at all (no tmux, too old, a socket
// path too long) has none open, and that is a known 0 with the reason; a
// server that did not answer is unknown, never 0.
func CountReading(ctx context.Context, nextDir string) capacity.Reading {
	dir := filepath.Join(nextDir, "tmux")
	s := &Server{sock: filepath.Join(dir, "term.sock"), conf: filepath.Join(dir, "term.conf"), environ: os.Environ}
	if err := checkSocketPath(s.sock); err != nil {
		return capacity.Reading{Known: true, Note: "no terminal can be opened here: " + err.Error()}
	}
	terms, err := s.List(ctx)
	if code, _ := terminal.CodeOf(err); code == terminal.CodeUnsupported {
		return capacity.Reading{Known: true, Note: "no terminal can be opened here: " + err.Error()}
	}
	if err != nil {
		return capacity.Reading{Err: err.Error()}
	}
	return capacity.Reading{Known: true, Used: int64(len(terms))}
}

// sunPathBytes is the size of a Unix socket address's path on macOS, the
// terminating NUL included; Linux has 108, so the smaller one holds on both.
const sunPathBytes = 104

func checkSocketPath(sock string) error {
	if len(sock) >= sunPathBytes {
		return terminal.Refuse(terminal.CodeSocketPathTooLong, fmt.Sprintf(
			"the terminal server's socket %s is %d bytes, and a Unix socket's path must be under %d",
			sock, len(sock), sunPathBytes))
	}
	return nil
}

// ready finds tmux, checks its version and reads the environment a shell
// starts with, once. A machine without tmux is terminal_unsupported on every
// call, with the same sentence the capability gives.
func (s *Server) ready(ctx context.Context) error {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if s.readied {
		return s.readyErr
	}
	found, _ := tmuxterm.FindTmux()
	if found == "" {
		s.readied, s.readyErr = true, terminal.Refuse(terminal.CodeUnsupported, "tmux is not installed")
		return s.readyErr
	}
	version, err := tmuxterm.TmuxVersion(ctx, found)
	if err != nil {
		// Not remembered: the next call asks again.
		return terminal.Refuse(terminal.CodeUnreachable, "tmux's version could not be read: "+err.Error())
	}
	if major, minor, ok := tmuxterm.ParseTmuxVersion(version); ok && !tmuxterm.TmuxNewEnough(major, minor) {
		s.readied, s.readyErr = true, terminal.Refuse(terminal.CodeUnsupported, fmt.Sprintf("%s is older than tmux %d.%d",
			strings.TrimSpace(version), tmuxterm.TmuxMinimumMajor, tmuxterm.TmuxMinimumMinor))
		return s.readyErr
	}
	s.env, s.shell = s.machine.paneEnv(ctx)
	for _, kv := range s.env {
		if v, ok := strings.CutPrefix(kv, "LANG="); ok {
			s.lang = v
		}
	}
	s.binary, s.readied = found, true
	return nil
}

// call runs one tmux invocation against this server. `-u` because the
// client's own locale is not what decides how this server reads its bytes;
// `-f` so that whichever call starts the server starts it with this
// daemon's configuration.
func (s *Server) call(ctx context.Context, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	full := append([]string{"-S", s.sock, "-f", s.conf, "-u"}, args...)
	cmd := exec.CommandContext(ctx, s.binary, full...)
	cmd.Env = clientEnv(s.environ(), s.lang)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), classify(ctx, args[0], strings.TrimSpace(stderr.String()), err)
	}
	return string(out), nil
}

// classify is a failed call as a refusal: the terminal positively not there,
// or the server not answering. Only tmux's own words decide the first.
func classify(ctx context.Context, op, said string, err error) error {
	if ctx.Err() != nil {
		return &terminal.Refusal{Code: terminal.CodeUnreachable,
			Detail: fmt.Sprintf("tmux %s did not answer within %s", op, callTimeout), Err: err}
	}
	if absent(said) {
		return &terminal.Refusal{Code: terminal.CodeClosed, Detail: said, Err: err}
	}
	if said == "" {
		said = err.Error()
	}
	return &terminal.Refusal{Code: terminal.CodeUnreachable, Detail: "tmux " + op + ": " + said, Err: err}
}

// absent is tmux saying the thing is not there: no such session or pane, or no
// server on this daemon's own socket, which holds nothing else.
func absent(said string) bool {
	switch {
	case strings.Contains(said, "can't find session"), strings.Contains(said, "can't find pane"),
		strings.Contains(said, "can't find window"), strings.Contains(said, "no server running"):
		return true
	case strings.HasPrefix(said, "error connecting to"):
		return strings.Contains(said, "No such file or directory") || strings.Contains(said, "Connection refused")
	}
	return false
}

// confVersion is the configuration's version. A running server that says
// another is given this one with `source-file`, so every option here must
// be one that is safe to change under running shells: F1's acceptance opens
// sessions after a source-file and checks an earlier process survived it.
const confVersion = "1"

// conf is the server's configuration. **No global `window-size`**: tmux
// 3.6a crashes on `new-session` under a global `window-size manual` (plan v3
// review 2, N1). A terminal is sized only by `resize-window`, which sets that
// one window to manual.
func conf() string {
	return strings.Join([]string{
		"# Written by Clawdline for its own terminal server. Replaced when its version changes.",
		"set -g prefix None",
		"set -g prefix2 None",
		"set -g status off",
		"set -g mouse off",
		"set -s escape-time 0",
		"set -g history-limit 5000",
		"set -g default-terminal tmux-256color",
		"set -g remain-on-exit off",
		"set -s @clawdline_conf_version " + confVersion,
		"",
	}, "\n")
}

// ensureServer writes the configuration and brings a running server up to
// it. It starts nothing: the next call that needs a server starts it with
// `-f`.
func (s *Server) ensureServer(ctx context.Context) error {
	dir := filepath.Dir(s.sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return terminal.Refuse(terminal.CodeUnreachable, "the terminal server's directory: "+err.Error())
	}
	// tmux refuses a socket directory others can enter, and so do we.
	if err := os.Chmod(dir, 0o700); err != nil {
		return terminal.Refuse(terminal.CodeUnreachable, "the terminal server's directory: "+err.Error())
	}
	want := conf()
	if have, err := os.ReadFile(s.conf); err != nil || string(have) != want {
		tmp := s.conf + ".tmp"
		if err := os.WriteFile(tmp, []byte(want), 0o600); err != nil {
			return terminal.Refuse(terminal.CodeUnreachable, "writing the terminal server's configuration: "+err.Error())
		}
		if err := os.Rename(tmp, s.conf); err != nil {
			return terminal.Refuse(terminal.CodeUnreachable, "writing the terminal server's configuration: "+err.Error())
		}
	}
	out, err := s.call(ctx, "", "show-options", "-sqv", "@clawdline_conf_version")
	if code, _ := terminal.CodeOf(err); code == terminal.CodeClosed {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == confVersion {
		return nil
	}
	_, err = s.call(ctx, "", "source-file", s.conf)
	return err
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// Open starts a login shell for a person in the project's directory.
func (s *Server) Open(ctx context.Context, req ports.OpenTerminal) (terminal.Terminal, error) {
	if req.Cols < 1 || req.Rows < 1 {
		return terminal.Terminal{}, terminal.Refuse(terminal.CodeInvalid, fmt.Sprintf("a terminal of %dx%d", req.Cols, req.Rows))
	}
	if !projectIDPattern.MatchString(req.ProjectID) {
		return terminal.Terminal{}, terminal.Refuse(terminal.CodeInvalid, fmt.Sprintf("project id %q", req.ProjectID))
	}
	if !filepath.IsAbs(req.ProjectPath) {
		return terminal.Terminal{}, terminal.Refuse(terminal.CodeInvalid, "the project's directory is not an absolute path")
	}
	if info, err := os.Stat(req.ProjectPath); err != nil || !info.IsDir() {
		return terminal.Terminal{}, terminal.Refuse(terminal.CodeInvalid, "the project's directory is not a directory: "+req.ProjectPath)
	}
	if err := s.ready(ctx); err != nil {
		return terminal.Terminal{}, err
	}
	command, err := s.paneCommand()
	if err != nil {
		return terminal.Terminal{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureServer(ctx); err != nil {
		return terminal.Terminal{}, err
	}
	open, err := s.List(ctx)
	if err != nil {
		return terminal.Terminal{}, err
	}
	if len(open) >= terminal.MaxTerminals {
		return terminal.Terminal{}, terminal.Refuse(terminal.CodeFull,
			fmt.Sprintf("%d terminals are open on this machine, the most it holds", len(open)))
	}

	id := terminal.NewID()
	target := "=" + id.SessionName() + ":"
	args := []string{"new-session", "-d", "-s", id.SessionName(),
		"-x", strconv.Itoa(req.Cols), "-y", strconv.Itoa(req.Rows),
		// `-c` is expanded as a format, where `#(…)` runs a command.
		"-c", strings.ReplaceAll(req.ProjectPath, "#", "##"),
		"--"}
	args = append(args, command...)
	// The options go in the same invocation, so no list ever sees the
	// session without its id.
	args = append(args,
		";", "set-option", "-t", target, "@clawdline_terminal", string(id),
		";", "set-option", "-t", target, "@clawdline_project", req.ProjectID,
		";", "set-option", "-t", target, "@clawdline_created", time.Now().UTC().Format(time.RFC3339),
		// A size given at creation is only the client's; resize-window
		// makes it this window's own, as every later resize does.
		";", "resize-window", "-t", target, "-x", strconv.Itoa(req.Cols), "-y", strconv.Itoa(req.Rows))
	if _, err := s.call(ctx, "", args...); err != nil {
		return terminal.Terminal{}, err
	}
	return s.find(ctx, id)
}

// paneCommand is `env -i <whitelist> $SHELL -l` as arguments. tmux reads an
// argument that ends in `;` as the end of a command, so that `;` is escaped,
// and one ending in `\;` — which no escape can keep — is refused.
func (s *Server) paneCommand() ([]string, error) {
	out := []string{"/usr/bin/env", "-i"}
	for _, arg := range append(append([]string{}, s.env...), s.shell, "-l") {
		switch {
		case strings.HasSuffix(arg, `\;`):
			key, _, _ := strings.Cut(arg, "=")
			return nil, terminal.Refuse(terminal.CodeInvalid, key+" ends in `\\;`, which tmux cannot be given as an argument")
		case strings.HasSuffix(arg, ";"):
			arg = strings.TrimSuffix(arg, ";") + `\;`
		}
		out = append(out, arg)
	}
	return out, nil
}

// List is every terminal on this daemon's server.
func (s *Server) List(ctx context.Context) ([]terminal.Terminal, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	out, err := s.call(ctx, "", "list-sessions", "-F", listFormat())
	if code, _ := terminal.CodeOf(err); code == terminal.CodeClosed {
		// No server on this daemon's own socket: nothing is open, and that
		// is an answer.
		return []terminal.Terminal{}, nil
	}
	if err != nil {
		return nil, err
	}
	terms := parseList(out)
	if terms == nil {
		terms = []terminal.Terminal{}
	}
	return terms, nil
}

func (s *Server) find(ctx context.Context, id terminal.ID) (terminal.Terminal, error) {
	terms, err := s.List(ctx)
	if err != nil {
		return terminal.Terminal{}, err
	}
	for _, t := range terms {
		if t.ID == id {
			return t, nil
		}
	}
	return terminal.Terminal{}, terminal.Refuse(terminal.CodeClosed, "no terminal "+string(id))
}

// target is the exact tmux target for a terminal: `=` so that `clt-ab` never
// matches `clt-abc`, and `:` so it names the session's window and pane.
func (s *Server) target(ctx context.Context, id terminal.ID) (string, error) {
	if !id.Valid() {
		return "", terminal.Refuse(terminal.CodeInvalid, fmt.Sprintf("terminal id %q", id))
	}
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	return "=" + id.SessionName() + ":", nil
}

// Frame reads the visible screen, the cursor and the modes in one invocation.
func (s *Server) Frame(ctx context.Context, id terminal.ID) (terminal.Frame, error) {
	target, err := s.target(ctx, id)
	if err != nil {
		return terminal.Frame{}, err
	}
	at := time.Now()
	out, err := s.call(ctx, "", "display-message", "-p", "-t", target, frameFormat(),
		";", "capture-pane", "-p", "-e", "-t", target)
	if err != nil {
		return terminal.Frame{}, err
	}
	f, err := parseFrame(out, at)
	if err != nil {
		return terminal.Frame{}, &terminal.Refusal{Code: terminal.CodeUnreachable, Detail: err.Error(), Err: err}
	}
	return f, nil
}

// Keys types bytes as keystrokes: `send-keys -H`, one hex byte per argument,
// so tmux looks for no key names in them and a multi-byte sequence arrives
// whole (internal/adapters/terminal/keys.go).
func (s *Server) Keys(ctx context.Context, id terminal.ID, data []byte) error {
	if len(data) == 0 {
		return terminal.Refuse(terminal.CodeInvalid, "there is no key to send")
	}
	if len(data) > terminal.MaxInputBytes {
		return terminal.Refuse(terminal.CodeInputTooLarge,
			fmt.Sprintf("%d bytes; one batch carries at most %d", len(data), terminal.MaxInputBytes))
	}
	target, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	args := make([]string, 0, len(data)+4)
	args = append(args, "send-keys", "-t", target, "-H")
	for _, b := range data {
		args = append(args, hex.EncodeToString([]byte{b}))
	}
	_, err = s.call(ctx, "", args...)
	return err
}

// Paste types text as a paste. `paste-buffer -p` brackets it when the program
// asked for bracketed paste, which is the one mode a frame cannot report.
func (s *Server) Paste(ctx context.Context, id terminal.ID, text string) error {
	if text == "" {
		return terminal.Refuse(terminal.CodeInvalid, "there is nothing to paste")
	}
	if len(text) > terminal.MaxPasteBytes {
		return terminal.Refuse(terminal.CodeInputTooLarge,
			fmt.Sprintf("%d bytes; one paste carries at most %d", len(text), terminal.MaxPasteBytes))
	}
	target, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	var nonce [6]byte
	_, _ = rand.Read(nonce[:])
	buffer := "clt-paste-" + hex.EncodeToString(nonce[:])
	_, err = s.call(ctx, text, "load-buffer", "-b", buffer, "-",
		";", "paste-buffer", "-p", "-d", "-b", buffer, "-t", target)
	if err != nil {
		// The buffer outlives a failed paste; it holds what was pasted.
		clean, cancel := context.WithTimeout(context.Background(), callTimeout)
		_, _ = s.call(clean, "", "delete-buffer", "-b", buffer)
		cancel()
	}
	return err
}

// Resize sets the window to cols × rows, and to manual sizing.
func (s *Server) Resize(ctx context.Context, id terminal.ID, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return terminal.Refuse(terminal.CodeInvalid, fmt.Sprintf("a terminal of %dx%d", cols, rows))
	}
	target, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.call(ctx, "", "resize-window", "-t", target, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows))
	return err
}

// Close ends the terminal's session and everything running in it.
func (s *Server) Close(ctx context.Context, id terminal.ID) error {
	target, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	pane, perr := s.paneOf(ctx, target)
	_, err = s.call(ctx, "", "kill-session", "-t", strings.TrimSuffix(target, ":"))
	if code, _ := terminal.CodeOf(err); code == terminal.CodeClosed {
		err = nil
	}
	if err == nil && perr == nil {
		s.dropPane(pane)
	}
	return err
}

// History is up to `lines` lines of scrollback and then the visible screen.
func (s *Server) History(ctx context.Context, id terminal.ID, lines int) ([]string, error) {
	if lines < 1 {
		return nil, terminal.Refuse(terminal.CodeInvalid, fmt.Sprintf("%d lines of history", lines))
	}
	if lines > terminal.MaxHistoryLines {
		lines = terminal.MaxHistoryLines
	}
	target, err := s.target(ctx, id)
	if err != nil {
		return nil, err
	}
	out, err := s.call(ctx, "", "capture-pane", "-p", "-e", "-S", "-"+strconv.Itoa(lines), "-t", target)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n"), nil
}

func (s *Server) paneOf(ctx context.Context, target string) (string, error) {
	out, err := s.call(ctx, "", "display-message", "-p", "-t", target, "#{pane_id}")
	if err != nil {
		return "", err
	}
	pane := strings.TrimSpace(out)
	if !tmuxterm.IsPaneID(pane) {
		return "", terminal.Refuse(terminal.CodeUnreachable, fmt.Sprintf("tmux answered pane %q", pane))
	}
	return pane, nil
}

// Changed subscribes to the terminal's drawing. The first subscriber on a
// pane attaches `pipe-pane` into a FIFO (internal/adapters/terminal
// signal_unix.go); the last one to stop takes it off.
func (s *Server) Changed(ctx context.Context, id terminal.ID) (<-chan struct{}, func(), error) {
	target, err := s.target(ctx, id)
	if err != nil {
		return nil, func() {}, err
	}
	pane, err := s.paneOf(ctx, target)
	if err != nil {
		return nil, func() {}, err
	}
	if s.signal == nil {
		return nil, func() {}, nil
	}
	if !s.signal.Attach(pane) {
		return nil, func() {}, terminal.Refuse(terminal.CodeUnreachable, "the terminal's change signal could not be attached")
	}
	sub := &subscription{wake: make(chan struct{}, 1)}
	s.subsMu.Lock()
	if s.subs[pane] == nil {
		s.subs[pane] = map[*subscription]struct{}{}
	}
	s.subs[pane][sub] = struct{}{}
	s.subsMu.Unlock()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			s.subsMu.Lock()
			delete(s.subs[pane], sub)
			last := len(s.subs[pane]) == 0
			if last {
				delete(s.subs, pane)
			}
			s.subsMu.Unlock()
			if last {
				s.signal.Detach(pane)
			}
		})
	}
	return sub.wake, stop, nil
}

// moved wakes every subscriber on a pane without waiting for any: a wake-up
// already pending says the same thing as a second one.
func (s *Server) moved(pane string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for sub := range s.subs[pane] {
		select {
		case sub.wake <- struct{}{}:
		default:
		}
	}
}

// dropPane forgets a closed terminal's pane: its subscribers stop hearing
// from it and its FIFO goes. They find it closed on their next frame.
func (s *Server) dropPane(pane string) {
	s.subsMu.Lock()
	held := len(s.subs[pane]) > 0
	delete(s.subs, pane)
	s.subsMu.Unlock()
	if held && s.signal != nil {
		s.signal.Detach(pane)
	}
}

// pipeHost is ports.PipeHost on this daemon's own server, for the change
// signal.
type pipeHost struct{ s *Server }

func (p pipeHost) Pipe(ctx context.Context, paneID, command string) bool {
	if !tmuxterm.IsPaneID(paneID) || command == "" {
		return false
	}
	_, err := p.s.call(ctx, "", "pipe-pane", "-t", paneID, command)
	return err == nil
}

func (p pipeHost) Unpipe(ctx context.Context, paneID string) bool {
	if !tmuxterm.IsPaneID(paneID) {
		return false
	}
	_, err := p.s.call(ctx, "", "pipe-pane", "-t", paneID)
	return err == nil
}

func (p pipeHost) PipedPanes(ctx context.Context) map[string]bool {
	out, err := p.s.call(ctx, "", "list-panes", "-a", "-F", "#{pane_id}|#{pane_pipe}")
	state := map[string]bool{}
	if err != nil {
		return state
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		pane, piped, ok := strings.Cut(line, "|")
		if ok && tmuxterm.IsPaneID(pane) && (piped == "0" || piped == "1") {
			state[pane] = piped == "1"
		}
	}
	return state
}
