//go:build linux

package terminal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What /proc says is on a terminal (tty_linux.go), as the close a person
// presses reads it.

// ptsTwo is /dev/pts/2 as a stat's tty_nr spells it: major 136, minor 2.
const ptsTwo = 136<<8 | 2

// statLine is one /proc/<pid>/stat, with every field the reader skips zero.
func statLine(pid, ppid, pgrp, ttyNr, tpgid int, comm string, start uint64) string {
	f := []string{strconv.Itoa(pid), "(" + comm + ")", "S", strconv.Itoa(ppid), strconv.Itoa(pgrp),
		strconv.Itoa(pgrp), strconv.Itoa(ttyNr), strconv.Itoa(tpgid)}
	for n := 9; n <= 21; n++ {
		f = append(f, "0")
	}
	f = append(f, strconv.FormatUint(start, 10), "0", "0")
	return strings.Join(f, " ") + "\n"
}

func fakeProc(t *testing.T, stats map[int]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, line := range stats {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Not a process: /proc holds these beside the pids, and they are skipped.
	if err := os.Mkdir(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// A command that spells itself with spaces and a closing parenthesis is still
// one command, and the fields after it are still counted from the last ")".
func TestAStatLineWhoseCommandHoldsParenthesesStillReads(t *testing.T) {
	st, err := parseProcStat([]byte(statLine(42, 7, 42, ptsTwo, 42, "a b) (c", 12345)))
	if err != nil {
		t.Fatal(err)
	}
	if st.pid != 42 || st.ppid != 7 || st.pgrp != 42 || st.tpgid != 42 || st.comm != "a b) (c" ||
		st.ttyMajor != 136 || st.ttyMinor != 2 || st.startTicks != 12345 {
		t.Fatalf("parsed: %+v", st)
	}
	// A minor past 255 is split across the high bits, as the kernel packs it.
	st, err = parseProcStat([]byte(statLine(1, 0, 1, 136<<8|(300&0xff)|(300&^0xff)<<12, 1, "x", 0)))
	if err != nil || st.ttyMinor != 300 {
		t.Fatalf("minor 300: %+v %v", st, err)
	}
	if _, err := parseProcStat([]byte("42 (short) S 1 2")); err == nil {
		t.Fatal("a truncated line read as a whole one")
	}
}

// **The pane that could not be closed**, as /proc showed it on 2026-09-26: a
// tmux shell whose parent is the tmux server (not on the pty), and a Claude
// Code in front of it stopped at a question. Before this reading the ladder
// had no group to signal and answered close_quit_refused.
func TestAClaudeCodeAtAQuestionIsAGroupTheCloseMaySignal(t *testing.T) {
	boot := time.Unix(1_790_000_000, 0)
	root := fakeProc(t, map[int]string{
		// The tmux server, on no tty at all.
		900: statLine(900, 1, 900, 0, -1, "tmux: server", 100),
		// The shell and the assistant on pts/2; the assistant is in front.
		276334: statLine(276334, 900, 276334, ptsTwo, 276348, "bash", 5000),
		276348: statLine(276348, 276334, 276348, ptsTwo, 276348, "claude", 5050),
		// Something on another pty is not this terminal's.
		300: statLine(300, 900, 300, 136<<8|3, 300, "claude", 10),
	})
	procs, fg, err := procsOnTTY(root, 136, 2, boot, "/dev/pts/2")
	if err != nil {
		t.Fatal(err)
	}
	if fg != 276348 || len(procs) != 2 {
		t.Fatalf("fg=%d procs=%+v", fg, procs)
	}
	sight := ttySightOf(procs, fg, session.Session{PID: 276348, Assistant: session.AssistantClaude})
	want := boot.Add(50*time.Second + 500*time.Millisecond)
	if !sight.Job || sight.Assistant != session.AssistantClaude || sight.PID != 276348 ||
		sight.Group != 276348 || !sight.Start.Equal(want) {
		t.Fatalf("sight: %+v (start want %v)", sight, want)
	}
}

// A shell at its prompt, whose parent is the tmux server, holds nothing — the
// state in which the pane is simply closed.
func TestATmuxShellAtItsPromptHoldsNothing(t *testing.T) {
	root := fakeProc(t, map[int]string{
		900: statLine(900, 1, 900, 0, -1, "tmux: server", 100),
		500: statLine(500, 900, 500, ptsTwo, 500, "bash", 5000),
	})
	procs, fg, err := procsOnTTY(root, 136, 2, time.Unix(0, 0), "/dev/pts/2")
	if err != nil {
		t.Fatal(err)
	}
	if sight := ttySightOf(procs, fg, session.Session{}); sight.Job {
		t.Fatalf("shell at its prompt: %+v", sight)
	}
}

// A listing whose rows disagree on the foreground group was read across a
// change, and is not an answer anything may be signalled on.
func TestAForegroundThatMovedDuringTheWalkIsNotAnAnswer(t *testing.T) {
	root := fakeProc(t, map[int]string{
		500: statLine(500, 900, 500, ptsTwo, 500, "bash", 1),
		501: statLine(501, 500, 501, ptsTwo, 501, "claude", 2),
	})
	if _, _, err := procsOnTTY(root, 136, 2, time.Unix(0, 0), "/dev/pts/2"); err == nil {
		t.Fatal("a torn listing was answered")
	}
	if _, _, err := procsOnTTY(root, 136, 9, time.Unix(0, 0), "/dev/pts/9"); err == nil {
		t.Fatal("a tty with nobody on it was answered")
	}
}

// This machine's own /proc answers for this process: its stat is readable and
// the boot time is in the past.
func TestThisMachinesProcReads(t *testing.T) {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skip("no /proc here:", err)
	}
	st, err := parseProcStat(raw)
	if err != nil || st.pid != os.Getpid() {
		t.Fatalf("own stat: %+v %v", st, err)
	}
	boot, err := bootTime()
	if err != nil || boot.IsZero() || !boot.Before(time.Now()) {
		t.Fatalf("boot: %v %v", boot, err)
	}
}
