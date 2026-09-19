package tunnel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Phase is where the tunnel is: off, coming up, up, or failed with a reason.
type Phase string

const (
	PhaseOff      Phase = "off"
	PhaseStarting Phase = "starting"
	PhaseUp       Phase = "up"
	PhaseFailed   Phase = "failed"
)

// Status is what the tunnel is doing, in the words the tunnel itself used. A
// failure carries a sentence rather than a code because it is read on a
// settings card, and "status 1" is not something anybody can act on.
type Status struct {
	Phase Phase
	Mode  Mode
	// URL is the address, once a connection to the edge is registered; a
	// quick tunnel's is its secret.
	URL string
	// Reason is why it failed, when it did.
	Reason string
	// Attempts is how many launches in a row have died, counted towards
	// attemptLimit.
	Attempts int
	// Command is the argv of the child that is running or was last started,
	// binary first, so the command line can be checked against the plan.
	Command []string
	// Changed is when Phase, URL or Reason last moved.
	Changed time.Time
}

// Inputs is everything Apply decides on. The caller reads it at the moment of
// the call — the settings file and the gate's own authority — and nothing here
// keeps a copy of it between calls.
type Inputs struct {
	// Mode, Name, Hostname and Binary are `remote_tunnel`,
	// `remote_tunnel_name`, `remote_hostname` and `cloudflared_path` as the
	// file has them.
	Mode, Name, Hostname, Binary string
	// Remote is the `remote` switch.
	Remote bool
	// Paired is whether anybody but this machine is let in (Refusal).
	Paired bool
	// Port is the daemon's own.
	Port int
}

// Options are the supervisor's knobs, each with a default. Tests shorten them.
type Options struct {
	// Dir is the daemon's state directory: the config file and the pid file
	// are written there, and nowhere else.
	Dir string
	// Log receives one line per thing worth knowing.
	Log func(format string, args ...any)
	// PathEnv is the PATH searched for cloudflared; the process's own when "".
	PathEnv string
	// Backoff, Grace and SteadyRun default to Backoff, graceDefault and
	// steadyRunDefault.
	Backoff   func(attempt int) time.Duration
	Grace     time.Duration
	SteadyRun time.Duration
	Now       func() time.Time
}

const (
	// attemptLimit is the launches in a row that may die before this gives up
	// and says so. A little over a minute of backoff: long enough to ride out
	// a laptop waking, short enough that a wrong tunnel name is reported while
	// somebody is still looking at the window.
	attemptLimit = 6
	// steadyRunDefault is how long a child has to have lived for its death to
	// count as a fresh problem rather than the same one getting worse.
	steadyRunDefault = time.Minute
	// graceDefault is between SIGTERM and SIGKILL. cloudflared is started with
	// a two-second grace period of its own.
	graceDefault = 3 * time.Second
	// PIDName is the record of the child this daemon started, so that a daemon
	// killed outright can find the tunnel it left behind (Reclaim).
	PIDName = "cloudflared.pid"
)

// Supervisor runs one cloudflared, or none.
//
// What is kept from the Swift app, and what is not:
//
//   - A launch is stamped with a generation, and a child's exit or output is
//     ignored unless its stamp is current. A "we meant to stop it" flag was the
//     obvious version and was wrong there: a stop followed by a start cleared
//     the flag before the old child's exit was seen, and the corpse of the
//     tunnel stopped on purpose was charged as a crash of the new one.
//   - A quick tunnel's banner is remembered and published only when a
//     connection is registered: the banner comes about a second before the
//     address can carry anything, and an address that 404s reads as a broken
//     feature.
//   - The address is logged once. The Swift app kept every address it had ever
//     logged in a set that only grew; this keeps the last.
//   - A tunnel that outlives its daemon is the worst kind of leak. The Swift
//     app killed its child at exit and could do nothing about SIGKILL. This
//     one writes the child's pid beside its config, and a daemon that starts
//     and finds that file stops the process it names — only when that
//     process's command line is provably this daemon's cloudflared (Reclaim).
type Supervisor struct {
	opts       Options
	configPath string
	pidPath    string

	mu         sync.Mutex
	status     Status
	running    *Plan
	child      *child
	generation int
	failures   int
	startedAt  time.Time
	retry      *time.Timer
	pendingURL string
	complaint  string
	loggedURL  string
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// New makes a supervisor with nothing running.
func New(opts Options) *Supervisor {
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	if opts.Backoff == nil {
		opts.Backoff = Backoff
	}
	if opts.Grace <= 0 {
		opts.Grace = graceDefault
	}
	if opts.SteadyRun <= 0 {
		opts.SteadyRun = steadyRunDefault
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.PathEnv == "" {
		opts.PathEnv = os.Getenv("PATH")
	}
	return &Supervisor{
		opts:       opts,
		configPath: filepath.Join(opts.Dir, ConfigName),
		pidPath:    filepath.Join(opts.Dir, PIDName),
		status:     Status{Phase: PhaseOff, Mode: Off, Changed: opts.Now()},
	}
}

// ConfigPath is where the file cloudflared is pointed at lives.
func (s *Supervisor) ConfigPath() string { return s.configPath }

// Status is a copy of what the tunnel is doing now.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.status
	out.Command = append([]string(nil), s.status.Command...)
	return out
}

// Installed reports whether cloudflared can be found, as the next launch
// would find it.
func (s *Supervisor) Installed(configured string) (string, bool) {
	bin := BinaryPath(configured, s.opts.PathEnv)
	return bin, bin != ""
}

// Apply starts, stops or restarts the tunnel to match in. It is safe to call
// whenever anything may have changed, and cheap when nothing has: a running
// tunnel whose plan still matches is left exactly where it is. Calls are
// serialised, and each one decides on the inputs it was handed.
func (s *Supervisor) Apply(in Inputs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode := ParseMode(in.Mode)
	if mode == Off {
		s.halt()
		s.failures = 0
		s.set(PhaseOff, Off, "", "")
		return
	}
	host, hostOK := Hostname(in.Hostname)
	name := strings.TrimSpace(in.Name)
	if why := Refusal(mode, in.Paired, in.Remote, name, host, hostOK); why != "" {
		s.halt()
		s.failures = 0
		if s.status.Phase != PhaseFailed || s.status.Reason != why {
			s.opts.Log("tunnel: refused — %s", why)
		}
		s.set(PhaseFailed, mode, "", why)
		return
	}
	bin := BinaryPath(in.Binary, s.opts.PathEnv)
	plan := Plan{Mode: mode, Name: name, Hostname: host, Port: in.Port, Binary: bin}
	if s.running != nil && *s.running == plan && (s.child != nil || s.retry != nil) {
		return
	}
	s.halt()
	s.failures = 0
	s.start(plan)
}

// Stop takes the tunnel down and waits, up to the grace period and a second,
// for the child to be gone: this is what the daemon calls on its way out.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	c := s.child
	s.halt()
	s.failures = 0
	s.set(PhaseOff, s.status.Mode, "", "")
	s.mu.Unlock()
	if c == nil {
		return
	}
	select {
	case <-c.done:
	case <-time.After(s.opts.Grace + time.Second):
	}
}

// start launches plan. The caller holds mu and has halted whatever ran.
func (s *Supervisor) start(plan Plan) {
	if plan.Binary == "" {
		s.set(PhaseFailed, plan.Mode, "", "cloudflared is not installed — `brew install cloudflared`, "+
			"or put its absolute path in \"cloudflared_path\" in config.json.")
		return
	}
	if plan.Mode != Named {
		s.launch(plan, "")
		return
	}
	// A named tunnel's credentials are asked of cloudflared rather than
	// guessed: the file is named after the tunnel's id, which the person does
	// not know it by. That asks Cloudflare's API and may take its fifteen
	// seconds, so it is asked outside the lock — every route that applies the
	// tunnel, and every read of its status, would otherwise wait on it — and
	// the launch happens only if nothing was applied in the meantime.
	//
	// The listing is pointed at this daemon's file like every other
	// cloudflared this package runs, so it is written first, without the
	// credentials it is about to look for.
	if err := writeFile(s.configPath, []byte(ConfigFile(plan, ""))); err != nil {
		s.set(PhaseFailed, plan.Mode, "", "The tunnel's configuration could not be written: "+err.Error())
		s.opts.Log("tunnel: config not written — %v", err)
		return
	}
	p := plan
	s.running = &p
	gen := s.generation
	configPath := s.configPath
	s.status.Attempts = s.failures
	s.set(PhaseStarting, plan.Mode, "", "")
	s.retry = time.AfterFunc(0, func() {
		credentials := credentialsFile(plan.Binary, plan.Name, configPath)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.generation != gen || s.running == nil || *s.running != plan {
			return
		}
		s.retry = nil
		s.launch(plan, credentials)
	})
}

// launch writes the config and runs cloudflared. The caller holds mu.
func (s *Supervisor) launch(plan Plan, credentials string) {
	// Written before every launch: the port or the hostname may have moved
	// since the last one, and a stale file is a tunnel pointing somewhere that
	// is no longer there.
	if err := writeFile(s.configPath, []byte(ConfigFile(plan, credentials))); err != nil {
		s.running = nil
		s.set(PhaseFailed, plan.Mode, "", "The tunnel's configuration could not be written: "+err.Error())
		s.opts.Log("tunnel: config not written — %v", err)
		return
	}
	args := Arguments(plan, s.configPath)
	cmd := exec.Command(plan.Binary, args...)
	// cloudflared never reads stdin, and a child holding an inherited terminal
	// can stop for a reason nothing here would see. exec gives it /dev/null.
	cmd.Stdin = nil
	stdout, err1 := cmd.StdoutPipe()
	stderr, err2 := cmd.StderrPipe()
	if err := errors.Join(err1, err2); err != nil {
		s.running = nil
		s.set(PhaseFailed, plan.Mode, "", "cloudflared would not start — "+err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		s.running = nil
		s.set(PhaseFailed, plan.Mode, "", "cloudflared would not start — "+err.Error())
		s.opts.Log("tunnel: launch failed — %v", err)
		return
	}

	s.generation++
	mine := s.generation
	c := &child{cmd: cmd, done: make(chan struct{})}
	s.child = c
	p := plan
	s.running = &p
	s.startedAt = s.opts.Now()
	s.pendingURL = ""
	s.complaint = ""
	s.status.Command = append([]string{plan.Binary}, args...)
	s.writePID(cmd.Process.Pid)
	s.set(PhaseStarting, plan.Mode, "", "")
	s.opts.Log("tunnel: %s starting, pid %d", plan.Mode, cmd.Process.Pid)

	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); s.read(stdout, mine) }()
	go func() { defer readers.Done(); s.read(stderr, mine) }()
	go func() {
		// Wait closes the pipes, so it comes after both readers have seen
		// their end, which they do when the child exits.
		readers.Wait()
		err := cmd.Wait()
		close(c.done)
		s.exited(mine, exitStatus(err))
	}()
}

// read takes whole lines only. A read lands wherever the kernel says, and the
// half of a buffer holding the hostname is as likely to be the second half as
// the first. A run longer than lineLimit without a newline is dropped whole.
func (s *Supervisor) read(r io.Reader, stamp int) {
	br := bufio.NewReaderSize(r, 4096)
	var carry []byte
	dropping := false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 && !dropping {
			carry = append(carry, chunk...)
			if len(carry) > lineLimit {
				carry = carry[:0]
				dropping = true
				s.opts.Log("tunnel: dropped a line of cloudflared output longer than %d bytes", lineLimit)
			}
		}
		switch {
		case err == nil:
			if !dropping {
				s.consider(strings.TrimRight(string(carry), "\r\n"), stamp)
			}
			carry = carry[:0]
			dropping = false
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			if len(carry) > 0 && !dropping {
				s.consider(strings.TrimRight(string(carry), "\r\n"), stamp)
			}
			return
		}
	}
}

func (s *Supervisor) consider(line string, stamp int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stamp != s.generation || s.running == nil {
		return
	}
	if c := Complaint(line); c != "" {
		s.complaint = c
		s.opts.Log("tunnel: %s", c)
	}
	plan := *s.running
	if plan.Mode == Quick {
		if url := QuickURL(line); url != "" {
			s.pendingURL = url
		}
	}
	location, ok := RegisteredConnection(line)
	if !ok {
		return
	}
	switch plan.Mode {
	case Quick:
		if s.pendingURL == "" {
			return
		}
		s.announce(s.pendingURL, location)
	case Named:
		s.announce("https://"+plan.Hostname, location)
	}
}

// announce publishes the address, and logs it once: a quick tunnel's address
// exists nowhere else, so the log may be the only copy there ever was — and
// the log is also what people attach to bug reports, so a reconnection gets a
// line with the secret part taken out.
func (s *Supervisor) announce(url, location string) {
	s.set(PhaseUp, s.running.Mode, url, "")
	if url != s.loggedURL {
		s.loggedURL = url
		s.opts.Log("tunnel: up at %s — via %s", url, location)
		return
	}
	s.opts.Log("tunnel: back up at %s — via %s", Redacted(url), location)
}

func (s *Supervisor) exited(stamp, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Somebody else's corpse: stopped on purpose, or already replaced.
	if stamp != s.generation {
		return
	}
	lived := s.opts.Now().Sub(s.startedAt)
	s.child = nil
	s.removePID()
	// A child that ran a while and then died is a fresh problem, so the count
	// starts over. Measured from launch, not from coming up: a tunnel that
	// registers and drops over and over is exactly what the count is for.
	if lived >= s.opts.SteadyRun {
		s.failures = 0
	}
	s.failures++
	plan := s.running
	if plan == nil {
		return
	}
	if s.failures > attemptLimit {
		why := s.complaint
		if why == "" {
			why = fmt.Sprintf("cloudflared exited with status %d.", status)
		}
		s.running = nil
		s.status.Attempts = s.failures - 1
		s.set(PhaseFailed, plan.Mode, "", "The tunnel would not stay up. "+why)
		s.opts.Log("tunnel: gave up after %d tries — %s", s.failures-1, why)
		return
	}
	wait := s.opts.Backoff(s.failures)
	s.opts.Log("tunnel: cloudflared exited (status %d) after %s — try %d of %d in %s",
		status, lived.Round(time.Second), s.failures, attemptLimit, wait)
	s.status.Attempts = s.failures
	s.set(PhaseStarting, plan.Mode, "", "")
	again := *plan
	gen := s.generation
	s.retry = time.AfterFunc(wait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		// A halt in the meantime moved the generation; this timer is stale.
		if s.generation != gen || s.running == nil || *s.running != again {
			return
		}
		s.retry = nil
		s.start(again)
	})
}

// halt stops everything except deciding what to say afterwards, which is the
// caller's: a stop is off, a refusal is failed, a restart is neither. The
// caller holds mu.
func (s *Supervisor) halt() {
	if s.retry != nil {
		s.retry.Stop()
		s.retry = nil
	}
	// Moving the generation is what tells the exit watcher this death was
	// wanted.
	s.generation++
	s.running = nil
	s.pendingURL = ""
	s.startedAt = time.Time{}
	s.status.Attempts = 0
	c := s.child
	if c == nil {
		return
	}
	s.child = nil
	pid := c.cmd.Process.Pid
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = c.cmd.Process.Kill()
	}
	grace := s.opts.Grace
	go func() {
		select {
		case <-c.done:
		case <-time.After(grace):
			// cloudflared agreed to be gone well inside this; anything still
			// here has stopped listening, and a tunnel is not a thing to leave
			// running because a process was rude about closing.
			_ = c.cmd.Process.Kill()
			<-c.done
		}
		s.mu.Lock()
		if s.child == nil {
			s.removePID()
		}
		s.mu.Unlock()
	}()
	s.opts.Log("tunnel: stopped (pid %d)", pid)
}

func (s *Supervisor) set(phase Phase, mode Mode, url, reason string) {
	if s.status.Phase == phase && s.status.Mode == mode && s.status.URL == url && s.status.Reason == reason {
		return
	}
	s.status.Phase = phase
	s.status.Mode = mode
	s.status.URL = url
	s.status.Reason = reason
	s.status.Changed = s.opts.Now()
}

// pidRecord is the pid file: the child and the config path it was pointed at,
// which is what proves a process is ours (Reclaim).
type pidRecord struct {
	PID    int    `json:"pid"`
	Config string `json:"config"`
}

func (s *Supervisor) writePID(pid int) {
	body, _ := json.Marshal(pidRecord{PID: pid, Config: s.configPath})
	if err := writeFile(s.pidPath, body); err != nil {
		s.opts.Log("tunnel: the pid file could not be written, so a tunnel left by a crash will not be found: %v", err)
	}
}

func (s *Supervisor) removePID() { _ = os.Remove(s.pidPath) }

// Reclaim stops the cloudflared a previous run of this daemon left behind,
// and only that one. The daemon calls it once, before its first Apply.
//
// A process is stopped only when the system answers that the recorded pid is
// alive **and** its command line carries `--config <this directory's file>`.
// No such process: the record is stale and goes. A process that is something
// else: left alone, record removed. A process table that cannot be read:
// nothing is stopped and the record stays, because unknown never authorises a
// kill.
func (s *Supervisor) Reclaim() {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := os.ReadFile(s.pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var rec pidRecord
	if err != nil || json.Unmarshal(body, &rec) != nil || rec.PID <= 1 {
		s.opts.Log("tunnel: %s could not be read; nothing was stopped", s.pidPath)
		return
	}
	command, alive, known := commandOf(rec.PID)
	switch {
	case !known:
		s.opts.Log("tunnel: could not tell what pid %d is; a tunnel left by an earlier run may still be up", rec.PID)
		return
	case !alive:
		s.removePID()
		return
	case rec.Config != s.configPath || !strings.Contains(command, "--config "+s.configPath):
		s.opts.Log("tunnel: pid %d is not this daemon's cloudflared any more; left alone", rec.PID)
		s.removePID()
		return
	}
	if p, err := os.FindProcess(rec.PID); err == nil {
		if err := p.Signal(syscall.SIGTERM); err != nil {
			_ = p.Kill()
		}
		s.opts.Log("tunnel: stopped pid %d, a cloudflared an earlier run of this daemon left up", rec.PID)
	}
	s.removePID()
}

// commandOf asks the process table for one pid's command line. known is false
// when the table could not be read at all.
func commandOf(pid int) (command string, alive, known bool) {
	if runtime.GOOS == "windows" {
		return "", false, false
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(strings.TrimSpace(string(out))) == 0 {
		// ps's own answer for a pid that is not there.
		return "", false, true
	}
	if err != nil {
		return "", false, false
	}
	command = strings.TrimSpace(string(out))
	return command, command != "", true
}

// credentialsFile is a named tunnel's credentials, if cloudflared can say
// where they are; "" when it cannot, since cloudflared can often find them from
// cert.pem itself and a wrong path is worse than none. Bounded: it asks
// Cloudflare's API, and fifteen seconds is this repository's other outbound
// deadline, not a measurement of cloudflared.
func credentialsFile(binary, name, configPath string) string {
	// `--config` here too. Listing starts nothing, but it reads the person's
	// ~/.cloudflared/config.yml without it, and no cloudflared this package
	// runs is left to read that file.
	cmd := exec.Command(binary, "tunnel", "--config", configPath, "list", "--output", "json")
	var out strings.Builder
	cmd.Stdout = &limitWriter{w: &out, left: lineLimit}
	if err := cmd.Start(); err != nil {
		return ""
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return ""
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return ""
	}
	var rows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(out.String()), &rows) != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, row := range rows {
		if row.Name != name || !ValidName(row.ID) {
			continue
		}
		path := filepath.Join(home, ".cloudflared", row.ID+".json")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// limitWriter keeps the first `left` bytes and throws the rest away.
type limitWriter struct {
	w    io.Writer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if l.left <= 0 {
		return n, nil
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	_, err := l.w.Write(p)
	return n, err
}

// writeFile writes atomically, owner-only: the config names the person's
// tunnel and hostname, and the pid file names a process to stop.
func writeFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
