//go:build !windows

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tmuxterm "github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/terminal/owned"
	"github.com/sainteye/clawdline/internal/contract"
)

// A real terminal server under a directory of the test's own, killed by its
// own socket: never the person's default server.
func realTermFixture(t *testing.T) *termFixture {
	t.Helper()
	binary := tmuxterm.FindTmux(context.Background()).Path
	if binary == "" {
		t.Skip("tmux is not installed")
	}
	// Under /tmp: macOS's per-user temporary directory is deep enough to put
	// the socket past a Unix socket's 104 bytes.
	dir, err := os.MkdirTemp("/tmp", "clt-http-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	host, err := owned.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command(binary, "-S", filepath.Join(dir, "tmux", "term.sock"), "kill-server").Run()
		_ = os.RemoveAll(dir)
	})
	return newTermFixture(t, host)
}

// shellStartWait is how long a test waits for the first thing a shell answers.
//
// The terminal runs the person's own login shell (`$SHELL -l`), and the first
// answer waits for that profile to load, which is the shell's time, not the
// stream's. Measured on 2026-10-02 on a 14-core Mac with a load average of 110:
// the first frame reached the stream 0.3-0.6 s after the test began, and `hi`
// 2.4-4.1 s after it, against the 5 s TestARealTerminalThroughTheHandler used
// to allow; idle, the whole test took 1.0-1.4 s. A passing run does not wait
// any longer for the larger number.
const shellStartWait = 30 * time.Second

func frameHas(e sseEvent, want string) bool {
	if e.name != "frame" {
		return false
	}
	var f contract.TerminalFrame
	if json.Unmarshal([]byte(e.data), &f) != nil {
		return false
	}
	for _, line := range f.Lines {
		if strings.TrimRight(sgr.ReplaceAllString(line, ""), " ") == want {
			return true
		}
	}
	return false
}

func stateIs(e sseEvent, status string) bool {
	var s contract.TerminalState
	return e.name == "state" && json.Unmarshal([]byte(e.data), &s) == nil && string(s.Status) == status
}

// Acceptance 9: open, acquire, type, see it streamed, close — through the
// handler, on a real tmux.
func TestARealTerminalThroughTheHandler(t *testing.T) {
	f := realTermFixture(t)
	srv := f.server()
	term := f.open(f.local)
	c, rec := f.control(term.ID, f.local, "a", "acquire")
	if rec.Code != http.StatusOK {
		t.Fatalf("acquire: %d %s", rec.Code, rec.Body)
	}
	stream, _ := f.stream(srv, term.ID, f.local, "a")
	stream.until(t, 5*time.Second, "the first frame", func(e sseEvent) bool { return e.name == "frame" })
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 1, "echo hi\r"); rec.Code != http.StatusOK {
		t.Fatalf("input: %d %s", rec.Code, rec.Body)
	}
	stream.until(t, shellStartWait, "a frame with hi", func(e sseEvent) bool { return frameHas(e, "hi") })
	if rec := f.do(http.MethodDelete, "/v1/terminals/"+term.ID, f.local,
		`{"epoch":`+strconv.FormatInt(c.Epoch, 10)+`,"client":"a"}`); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	e := stream.until(t, 2*time.Second, "state closed", func(e sseEvent) bool { return stateIs(e, "closed") })
	var st contract.TerminalState
	_ = json.Unmarshal([]byte(e.data), &st)
	if st.ClosedBy == nil || !st.ClosedBy.SameClient {
		t.Fatalf("closed by %+v", st.ClosedBy)
	}
}

// Acceptance 10: a shell that exits tells every viewer within a second, and
// nothing more is typed.
//
// `exit` is the case a person types, and on its own it cannot tell whether
// the change signal hears the end: the shell draws its last line a few
// milliseconds before it goes, and the stream reading the screen after that
// wake already finds the terminal gone. So the second case ends the shell
// with nothing drawn after the echo, 200 ms later, and only the signal's own
// word that the pane went (owned.pipeOrExit) reaches the viewers before the
// next beat.
func TestAShellThatExitsTellsEveryViewer(t *testing.T) {
	for name, line := range map[string]string{"exit": "exit\r", "a silent end": "sleep 0.2; kill -9 $$\r"} {
		t.Run(name, func(t *testing.T) { shellEnds(t, line) })
	}
}

func shellEnds(t *testing.T, line string) {
	f := realTermFixture(t)
	srv := f.server()
	term := f.open(f.local)
	_, phone := f.device("phone", true)
	c, _ := f.control(term.ID, f.local, "a", "acquire")
	streams := []*sse{}
	for _, who := range []struct{ token, client string }{{f.local, "a"}, {f.local, "b"}, {phone, "p"}} {
		s, status := f.stream(srv, term.ID, who.token, who.client)
		if status != http.StatusOK {
			t.Fatalf("stream %s: %d", who.client, status)
		}
		s.until(t, 5*time.Second, "the first frame", func(e sseEvent) bool { return e.name == "frame" })
		streams = append(streams, s)
	}
	// The clock starts once the shell reads what it is typed: a shell still
	// starting takes the exit only when it is ready, and that wait is the
	// shell's, not the stream's.
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 1, "echo ready\r"); rec.Code != http.StatusOK {
		t.Fatalf("input: %d %s", rec.Code, rec.Body)
	}
	for _, s := range streams {
		s.until(t, shellStartWait, "the shell's answer", func(e sseEvent) bool { return frameHas(e, "ready") })
	}
	began := time.Now()
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 2, line); rec.Code != http.StatusOK {
		t.Fatalf("input: %d %s", rec.Code, rec.Body)
	}
	for i, s := range streams {
		e := s.until(t, time.Second, "state exited", func(e sseEvent) bool { return stateIs(e, "exited") })
		t.Logf("viewer %d was told exited %v after the Enter", i, e.at.Sub(began).Round(time.Millisecond))
	}
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 3, "echo later\r"); rec.Code != http.StatusNotFound || termCode(rec) != "terminal_closed" {
		t.Fatalf("input after the exit: %d %s", rec.Code, rec.Body)
	}
}

// sgr is the colour and cursor sequences a frame line may carry.
var sgr = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
