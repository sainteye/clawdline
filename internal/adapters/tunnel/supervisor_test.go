package tunnel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The supervisor is driven here by a fake cloudflared: a shell script that
// writes down the command line it was given and then says, on stderr, what a
// real one says. Nothing in this file reaches the network, and the real
// binary on this machine is never run — every test names its fake in
// `cloudflared_path`, which is looked at before anywhere else.

type fake struct {
	dir  string
	bin  string
	argv string
}

// newFake writes a cloudflared that records its arguments and then runs body.
func newFake(t *testing.T, body string) fake {
	t.Helper()
	if runtime.GOOS == "windows" {
		// A fake that is not a shell script would not be enough on its own:
		// BinaryPath takes a file for a program by its Unix execute bits,
		// which Go never reports for a file on Windows, so a supervisor there
		// refuses every cloudflared, a fake one included, as not installed.
		t.Skip("the fake cloudflared is a shell script, and BinaryPath does not recognise a program on Windows yet")
	}
	dir := t.TempDir()
	f := fake{dir: dir, bin: filepath.Join(dir, "cloudflared"), argv: filepath.Join(dir, "argv")}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + f.argv + "'\n" + body + "\n"
	if err := os.WriteFile(f.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// stay keeps the fake alive, as a tunnel is, and leaves on SIGTERM. Its sleeps
// hold no pipe, so the supervisor sees the end of the output when it goes.
const stay = "trap 'exit 0' TERM\nwhile :; do sleep 0.05 >/dev/null 2>&1; done"

func emit(lines ...string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("printf '%s\\n' '" + strings.ReplaceAll(l, "'", `'\''`) + "' >&2\n")
	}
	return b.String()
}

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l *logs) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func newSupervisor(t *testing.T, l *logs) (*Supervisor, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	s := New(Options{
		Dir:     dir,
		Log:     l.printf,
		PathEnv: t.TempDir(),
		Backoff: func(int) time.Duration { return time.Millisecond },
		Grace:   500 * time.Millisecond,
	})
	t.Cleanup(s.Stop)
	return s, dir
}

func waitFor(t *testing.T, s *Supervisor, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := s.Status()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s; status is %+v", what, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gone is the system's answer that pid is no longer there. A platform that
// cannot answer fails the test rather than have it read as either.
func gone(t *testing.T, pid int) bool {
	t.Helper()
	g, known := processGone(pid)
	if !known {
		t.Fatalf("this platform cannot say whether pid %d is running", pid)
	}
	return g
}

func readPID(t *testing.T, dir string) pidRecord {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, PIDName))
	if err != nil {
		t.Fatal(err)
	}
	var rec pidRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func quickInputs(bin string) Inputs {
	return Inputs{Mode: "quick", Binary: bin, Remote: true, Paired: true, Port: 7817}
}

// Nothing paired: the refusal is the status, and cloudflared is never run.
func TestNothingPairedLaunchesNothing(t *testing.T) {
	f := newFake(t, emit(bannerLine, registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	in := quickInputs(f.bin)
	in.Paired = false
	s.Apply(in)
	st := s.Status()
	if st.Phase != PhaseFailed || !strings.Contains(st.Reason, "no paired device") {
		t.Fatalf("status: %+v", st)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(f.argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cloudflared ran without a paired device (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfigName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a config was written for a refused tunnel (%v)", err)
	}
}

// Paired: the fake is run with exactly the arguments the plan names, the
// banner alone does not publish the address, the registration does, and the
// revocation that leaves nothing paired takes the tunnel down.
func TestQuickTunnelUpThenRevoked(t *testing.T) {
	f := newFake(t, emit(termsLine, bannerLine)+"sleep 0.3\n"+emit(registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	s.Apply(quickInputs(f.bin))

	// The banner comes first; until a connection registers it is only a
	// promise, and the status says starting.
	if st := s.Status(); st.Phase != PhaseStarting || st.URL != "" {
		t.Fatalf("before registration: %+v", st)
	}
	st := waitFor(t, s, "up", func(st Status) bool { return st.Phase == PhaseUp })
	if st.URL != "https://denied-franchise-william-jade.trycloudflare.com" {
		t.Fatalf("url: %q", st.URL)
	}

	config := filepath.Join(dir, ConfigName)
	argv, err := os.ReadFile(f.argv)
	if err != nil {
		t.Fatal(err)
	}
	want := Arguments(Plan{Mode: Quick, Port: 7817}, config)
	if got := strings.Split(strings.TrimSpace(string(argv)), "\n"); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cloudflared was given\n %q\nwant\n %q", got, want)
	}
	if strings.Join(st.Command, "\x00") != strings.Join(append([]string{f.bin}, want...), "\x00") {
		t.Fatalf("status command: %q", st.Command)
	}
	body, err := os.ReadFile(config)
	if err != nil || string(body) != ConfigFile(Plan{Mode: Quick, Port: 7817}, "") {
		t.Fatalf("config %q: %v", body, err)
	}
	if info, err := os.Stat(config); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode: %v %v", info.Mode(), err)
	}
	rec := readPID(t, dir)
	if rec.Config != config || gone(t, rec.PID) {
		t.Fatalf("pid record: %+v", rec)
	}
	if !strings.Contains(l.all(), "tunnel: up at https://denied-franchise-william-jade.trycloudflare.com — via tpe01") {
		t.Fatalf("log:\n%s", l.all())
	}

	in := quickInputs(f.bin)
	in.Paired = false
	s.Apply(in)
	if st := s.Status(); st.Phase != PhaseFailed || st.URL != "" {
		t.Fatalf("after revocation: %+v", st)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !gone(t, rec.PID) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still running after the revocation", rec.PID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, PIDName)); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pid file outlived its process")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Applying the same settings again leaves a running tunnel alone: a restart
// would throw a quick tunnel's address away.
func TestSamePlanIsLeftAlone(t *testing.T) {
	f := newFake(t, emit(bannerLine, registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	s.Apply(quickInputs(f.bin))
	waitFor(t, s, "up", func(st Status) bool { return st.Phase == PhaseUp })
	first := readPID(t, dir).PID
	s.Apply(quickInputs(f.bin))
	s.Apply(quickInputs(f.bin))
	if st := s.Status(); st.Phase != PhaseUp || readPID(t, dir).PID != first || gone(t, first) {
		t.Fatalf("an unchanged plan restarted the tunnel: %+v", st)
	}
}

// A plan that changed replaces the child, and the old child's death — which
// was wanted — is not counted against the new one.
func TestReplacedChildIsNotAFailure(t *testing.T) {
	f := newFake(t, emit(bannerLine, registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	s.Apply(quickInputs(f.bin))
	waitFor(t, s, "up", func(st Status) bool { return st.Phase == PhaseUp })
	first := readPID(t, dir).PID
	moved := quickInputs(f.bin)
	moved.Port = 7818
	s.Apply(moved)
	st := waitFor(t, s, "up on the new port", func(st Status) bool {
		return st.Phase == PhaseUp && strings.Contains(strings.Join(st.Command, " "), "127.0.0.1:7818")
	})
	time.Sleep(700 * time.Millisecond) // past the old child's grace
	if st = s.Status(); st.Attempts != 0 || st.Phase != PhaseUp {
		t.Fatalf("the stopped child was charged as a failure: %+v\n%s", st, l.all())
	}
	if !gone(t, first) {
		t.Fatalf("the replaced child %d is still running", first)
	}
}

// A cloudflared that keeps dying is retried, then given up on, with its own
// sentence as the reason — here the bare stderr line a mistyped tunnel name
// produces, which is not a log line at all.
func TestGivesUpWithCloudflaredsOwnWords(t *testing.T) {
	f := newFake(t, emit(badNameLine)+"exit 1")
	var l logs
	s, _ := newSupervisor(t, &l)
	s.Apply(quickInputs(f.bin))
	st := waitFor(t, s, "given up", func(st Status) bool { return st.Phase == PhaseFailed })
	if !strings.Contains(st.Reason, "would not stay up") || !strings.Contains(st.Reason, "neither the ID nor the name") {
		t.Fatalf("reason: %q", st.Reason)
	}
	if st.Attempts != attemptLimit {
		t.Fatalf("attempts: %d", st.Attempts)
	}
	if n := strings.Count(l.all(), "starting, pid"); n != attemptLimit+1 {
		t.Fatalf("%d launches, want %d:\n%s", n, attemptLimit+1, l.all())
	}
}

// A named tunnel: the listing that finds its credentials and the run are both
// pointed at this daemon's file, the run names the tunnel, and the address is
// the hostname, published when a connection registers.
func TestNamedTunnel(t *testing.T) {
	f := newFake(t, `if [ "$2" = "--config" ] && [ "$4" = "list" ]; then
  printf '%s\n' "$@" > "$0.list"; echo '[{"id":"0b1c2d3e","name":"clawdline"}]'; exit 0
fi
`+emit(registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	s.Apply(Inputs{Mode: "named", Name: "clawdline", Hostname: "https://Mac.Example.com/", Binary: f.bin, Remote: true, Paired: true, Port: 7817})
	st := waitFor(t, s, "up", func(st Status) bool { return st.Phase == PhaseUp })
	if st.URL != "https://mac.example.com" || st.Mode != Named {
		t.Fatalf("status: %+v", st)
	}
	config := filepath.Join(dir, ConfigName)
	list, err := os.ReadFile(f.bin + ".list")
	if err != nil {
		t.Fatalf("the credentials were never asked for: %v", err)
	}
	if got := strings.Join(strings.Fields(string(list)), " "); got != "tunnel --config "+config+" list --output json" {
		t.Fatalf("listing: %q", got)
	}
	argv, _ := os.ReadFile(f.argv)
	want := Arguments(Plan{Mode: Named, Name: "clawdline", Hostname: "mac.example.com", Port: 7817}, config)
	if got := strings.Split(strings.TrimSpace(string(argv)), "\n"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("run: %q", got)
	}
	body, _ := os.ReadFile(config)
	if !strings.Contains(string(body), `- hostname: "mac.example.com"`) || !strings.Contains(string(body), `tunnel: "clawdline"`) {
		t.Fatalf("config:\n%s", body)
	}
}

// Stop is what the daemon does on its way out, and it waits for the child.
func TestStopTakesTheChildWithIt(t *testing.T) {
	f := newFake(t, emit(bannerLine, registered)+stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	s.Apply(quickInputs(f.bin))
	waitFor(t, s, "up", func(st Status) bool { return st.Phase == PhaseUp })
	pid := readPID(t, dir).PID
	s.Stop()
	if !gone(t, pid) {
		t.Fatalf("pid %d outlived Stop", pid)
	}
	if st := s.Status(); st.Phase != PhaseOff {
		t.Fatalf("status after stop: %+v", st)
	}
}

// Reclaim stops the cloudflared an earlier daemon left, and only when that
// process's command line is provably ours.
func TestReclaim(t *testing.T) {
	f := newFake(t, stay)
	var l logs
	s, dir := newSupervisor(t, &l)
	config := filepath.Join(dir, ConfigName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Ours: the command line carries --config and this directory's file.
	left := exec.Command(f.bin, "tunnel", "--config", config, "--url", "http://127.0.0.1:7817")
	if err := left.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = left.Wait(); close(exited) }()
	time.Sleep(100 * time.Millisecond)
	s.writePID(left.Process.Pid)
	s.Reclaim()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		_ = left.Process.Kill()
		t.Fatalf("the left-behind tunnel was not stopped:\n%s", l.all())
	}
	if _, err := os.Stat(filepath.Join(dir, PIDName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the pid file stayed")
	}

	// Not ours: the pid now names something else. Left alone.
	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	s.writePID(other.Process.Pid)
	s.Reclaim()
	time.Sleep(100 * time.Millisecond)
	if gone(t, other.Process.Pid) {
		t.Fatal("a process that is not this daemon's cloudflared was stopped")
	}
	if !strings.Contains(l.all(), "is not this daemon's cloudflared") {
		t.Fatalf("log:\n%s", l.all())
	}
}
