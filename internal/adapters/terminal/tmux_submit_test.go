package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// These run Send against a real, private tmux server whose one pane holds a
// small program standing in for an assistant's composer (composerStub). The
// program is this test binary itself, started again with an environment
// variable that makes TestComposerStubProgram be it.
//
// The stub reproduces what Claude Code 2.1.278 was measured doing on this
// Mac: the pty hands a reader at most 1022 bytes per read, a read longer than
// 800 bytes that is not inside a bracketed paste is taken for a paste and never
// reaches the message, and a Return in a read of its own submits whatever the
// input line holds. A 1045-byte completion notice typed with `send-keys -l`
// and then Enter therefore submitted only its last 23 bytes — the
// `ck"}</clawdline-notice>` a root kept receiving.

const (
	stubLogEnv  = "CLAWDLINE_COMPOSER_STUB_LOG"
	stubModeEnv = "CLAWDLINE_COMPOSER_STUB_MODE"
)

// stubEvent is one line of the stub's log: a raw read, or a submitted line.
type stubEvent struct {
	Read   string  `json:"read,omitempty"`
	Submit *string `json:"submit,omitempty"`
}

// TestComposerStubProgram is not a test. It is the program in the pane, and
// skips everywhere else.
func TestComposerStubProgram(t *testing.T) {
	logPath := os.Getenv(stubLogEnv)
	if logPath == "" {
		t.Skip("only runs as the program inside a test pane")
	}
	composerStub(logPath, os.Getenv(stubModeEnv))
	os.Exit(0)
}

// composerStub draws a framed composer, reads the tty in raw mode and logs
// every read and every submitted line. In "blind" mode it reads and logs but
// never draws what it read: a program that is not taking its input.
func composerStub(logPath, mode string) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(2)
	}
	logEvent := func(e stubEvent) {
		raw, _ := json.Marshal(e)
		logFile.Write(append(raw, '\n'))
	}
	draw := func(line string) {
		fmt.Fprintf(os.Stdout, "\x1b[2J\x1b[H❯ %s\r\n──────────\r\n", line)
	}
	// Ask for bracketed paste, as every assistant CLI does.
	fmt.Fprint(os.Stdout, "\x1b[?2004h")
	draw("")
	var shown, message, paste []byte
	pasting := false
	pastes := 0
	buf := make([]byte, 64*1024)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		chunk := buf[:n]
		logEvent(stubEvent{Read: string(chunk)})
		if mode == "blind" {
			continue
		}
		if !pasting && !bytes.Contains(chunk, []byte("\x1b[200~")) && len(chunk) > 800 {
			// An unbracketed burst: taken for a paste, and lost.
			continue
		}
		for len(chunk) > 0 {
			if pasting {
				end := bytes.Index(chunk, []byte("\x1b[201~"))
				if end < 0 {
					paste = append(paste, chunk...)
					break
				}
				paste = append(paste, chunk[:end]...)
				chunk = chunk[end+len("\x1b[201~"):]
				pasting = false
				if len(paste) > 800 {
					pastes++
					shown = append(shown, fmt.Sprintf("[Pasted text #%d]", pastes)...)
				} else {
					shown = append(shown, paste...)
				}
				message = append(message, paste...)
				paste = nil
				continue
			}
			if bytes.HasPrefix(chunk, []byte("\x1b[200~")) {
				pasting = true
				chunk = chunk[len("\x1b[200~"):]
				continue
			}
			c := chunk[0]
			chunk = chunk[1:]
			if c == '\r' {
				line := string(message)
				logEvent(stubEvent{Submit: &line})
				shown, message = nil, nil
				continue
			}
			shown = append(shown, c)
			message = append(message, c)
		}
		draw(string(shown))
	}
}

// privateTmux starts a tmux server nobody else uses, with one pane running
// the composer stub, and answers the pane and the stub's log.
func privateTmux(t *testing.T, mode string) (session.Session, string) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-tmux-submit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_PANE")
	t.Setenv("TMUX_TMPDIR", dir)
	logPath := filepath.Join(dir, "stub.log")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("stty raw -echo; exec env %s=%s %s=%s %s -test.run '^TestComposerStubProgram$'",
		stubLogEnv, logPath, stubModeEnv, mode, self)
	out, err := exec.Command(bin, "-f", "/dev/null", "new-session", "-d", "-x", "160", "-y", "40",
		"-P", "-F", "#{pane_id}", command).Output()
	if err != nil {
		t.Skipf("could not start a private tmux server: %v", err)
	}
	t.Cleanup(func() { exec.Command(bin, "kill-server").Run() })
	pane := session.Session{ID: strings.TrimSpace(string(out)), Backend: session.BackendTmux}
	// Wait for the stub to draw its composer, which is also the moment it
	// has asked for bracketed paste.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if screen, ok := NewTmux().Capture(context.Background(), pane); ok && strings.Contains(screen, "❯") {
			return pane, logPath
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the stub never drew its composer")
	return pane, logPath
}

func stubEvents(t *testing.T, logPath string) []stubEvent {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []stubEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e stubEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unreadable stub log line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// noticeSizedLine is a completion notice as long as the one a root kept
// receiving in pieces: 1045 bytes, with the multi-byte title a real one has.
func noticeSizedLine() string {
	head := `<clawdline-notice>{"protocol":"clawdline.notice","version":2,"kind":"task_finished",` +
		`"task":{"title":"清單排序加上最近活動；送出時顯示正在送出"},"body":"`
	tail := `","ack_path":"/v1/orchestrator/tasks/00000000-0000-0000-0000-000000000000/completion/ack"}</clawdline-notice>`
	pad := 1045 - len(head) - len(tail)
	return head + strings.Repeat("x", pad) + tail
}

// A notice-sized line reaches the program as one line: submitted once, whole.
// With `send-keys -l` and an immediate Enter it was submitted as its last 23
// bytes, and the rest never reached the message at all. Send may nudge a
// framed composer whose screen has not caught up with the first Enter; after
// the stub has submitted the line, those bounded follow-up Enters are empty.
func TestTmuxSendSubmitsANoticeSizedLineWhole(t *testing.T) {
	pane, logPath := privateTmux(t, "")
	text := noticeSizedLine()
	if len(text) != 1045 {
		t.Fatalf("the line is %d bytes, want 1045", len(text))
	}
	if err := NewTmux().Send(context.Background(), pane, text); err != nil {
		t.Fatalf("send: %v", err)
	}
	var submitted []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		submitted = submitted[:0]
		for _, e := range stubEvents(t, logPath) {
			if e.Submit != nil {
				submitted = append(submitted, *e.Submit)
			}
		}
		if len(submitted) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A moment more, for a second submit that should not come.
	time.Sleep(300 * time.Millisecond)
	submitted = submitted[:0]
	for _, e := range stubEvents(t, logPath) {
		if e.Submit != nil {
			submitted = append(submitted, *e.Submit)
		}
	}
	valid := len(submitted) >= 1 && len(submitted) <= 1+len(nudgePauses)
	for i, s := range submitted {
		if (i == 0 && s != text) || (i > 0 && s != "") {
			valid = false
		}
	}
	if !valid {
		for i, s := range submitted {
			t.Logf("submitted[%d] (%d bytes): …%q", i, len(s), s[max(0, len(s)-40):])
		}
		t.Fatalf("want the %d-byte line once and whole, followed only by at most %d empty nudges; got %d submission(s)",
			len(text), len(nudgePauses), len(submitted))
	}
}

// A program that reads the paste and never shows it is never sent an Enter:
// Send answers Unsubmitted, and no Return byte reaches the program at all.
func TestTmuxSendPressesNoEnterUntilThePaneShowsTheText(t *testing.T) {
	pane, logPath := privateTmux(t, "blind")
	defer func(was time.Duration) { sendConfirm = was }(sendConfirm)
	sendConfirm = 800 * time.Millisecond
	text := "please read the briefing 0123456789abcdef0123456789abcdef"
	err := NewTmux().Send(context.Background(), pane, text)
	var unsubmitted Unsubmitted
	if !errors.As(err, &unsubmitted) {
		t.Fatalf("send to a program that never showed the text answered %T %v, want Unsubmitted", err, err)
	}
	time.Sleep(300 * time.Millisecond)
	var read strings.Builder
	for _, e := range stubEvents(t, logPath) {
		read.WriteString(e.Read)
	}
	if !strings.Contains(read.String(), text) {
		t.Fatalf("the text never reached the program: %q", read.String())
	}
	if strings.ContainsAny(read.String(), "\r\n") {
		t.Fatalf("an Enter reached a program that never showed the text: %q", read.String())
	}
}
