package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// A launch line is typed into a shell that has only just started, and a long
// one does not arrive.
//
// Measured on 2026-09-30: a codex launch with a persona, typed into a new
// iTerm2 tab, reached the tab as its first 1024 bytes, cut off mid-argument,
// and the assistant never started. The text is written before the shell's
// line editor has the terminal, while the tty is still in canonical mode, and
// macOS keeps at most MAX_CANON (1024) bytes of one line there. The same
// happened on tmux: a 1610-byte line pasted into a fresh pane showed 1024
// bytes and ran nothing.
//
// So a line longer than MaxTypedLaunchBytes is written to a private script
// under the daemon's own directory, and what is typed is `. '<script>'`.
//
// **Why `.` and not `exec /bin/sh`, or iTerm2's `command`.** Sourcing runs
// the line in the tab's own interactive shell, exactly as typing it would:
// the PATH, aliases and functions the person's rc files set up, the same
// shell's quoting, the `cd` landing in that shell, and a prompt to come back
// to when the assistant exits. `exec /bin/sh` would run it under another
// shell without the rc files and close the tab on exit; a tab opened with
// iTerm2's `command` has no interactive shell at all. The script's bytes are
// the line's bytes, so any quoting the line survived being typed with, it
// survives being read from a file with.
//
// The script removes itself as its first command, before the line runs: the
// shell holds it open, so the rest is still read. One that is never run — a
// tab whose shell threw its typeahead away — stays behind, and at most
// MaxLaunchScripts are kept, the oldest removed first. The typed line in the
// shell's history names a script that is gone by then; that history entry is
// the one thing a person sees differently.
const (
	// MaxTypedLaunchBytes is the longest line typed into a new tab as it is:
	// half of macOS's 1024-byte MAX_CANON, so what a script path adds to the
	// short line never brings it near.
	MaxTypedLaunchBytes = 512
	// MaxLaunchScripts is how many launch scripts are kept at once.
	MaxLaunchScripts = 64
)

// launchScriptDir is where the scripts are written: the daemon's own
// directory, which config.Dir resolves as every other file of the daemon's.
func launchScriptDir() string { return filepath.Join(config.Dir(), "launch-lines") }

// typedLaunchLine is what is typed for line: line itself when it is short
// enough and one line, and otherwise the line that sources a script holding
// it. A line with a newline in it — a Project's memory handed to Claude Code
// inside one quoted argument (projects.MemoryArgs) — would be typed as two
// lines, the first ending inside an open quote, so it is always scripted.
func typedLaunchLine(line string) (string, error) {
	if len(line) <= MaxTypedLaunchBytes && !strings.ContainsAny(line, "\r\n") {
		return line, nil
	}
	dir := launchScriptDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", Failure{Message: "The launch line is too long to type and could not be written down: " + err.Error()}
	}
	// MkdirAll leaves the mode of a directory that was already there.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", Failure{Message: "The launch line is too long to type and could not be written down: " + err.Error()}
	}
	pruneLaunchScripts(dir, MaxLaunchScripts-1)
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", Failure{Message: "The launch line is too long to type and could not be written down: " + err.Error()}
	}
	path := filepath.Join(dir, "launch-"+hex.EncodeToString(id[:])+".sh")
	quoted := projects.ShellQuoted(path)
	typed := ". " + quoted
	if len(typed) > MaxTypedLaunchBytes {
		return "", Failure{Message: "The launch line is too long to type, and so is the path of the file it would be written to."}
	}
	body := "rm -f " + quoted + "\n" + line + "\n"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.WriteString(body)
		err = errors.Join(err, f.Close())
	}
	if err != nil {
		os.Remove(path)
		return "", Failure{Message: "The launch line is too long to type and could not be written down: " + err.Error()}
	}
	return typed, nil
}

// pruneLaunchScripts removes the oldest launch scripts in dir until at most
// keep are left. What it cannot remove is only logged: the launch it makes
// room for does not depend on it.
func pruneLaunchScripts(dir string, keep int) {
	paths, _ := filepath.Glob(filepath.Join(dir, "launch-*.sh"))
	if len(paths) <= keep {
		return
	}
	type script struct {
		path string
		mod  int64
	}
	scripts := make([]script, 0, len(paths))
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			scripts = append(scripts, script{p, st.ModTime().UnixNano()})
		}
	}
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].mod < scripts[j].mod })
	for _, s := range scripts[:max(0, len(scripts)-keep)] {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("terminal: an old launch script could not be removed: %v", err)
		}
	}
}

// LaunchScriptsReading is capacity.TerminalLaunchScripts: the scripts waiting
// for a tab to run them.
func LaunchScriptsReading() capacity.Reading {
	paths, err := filepath.Glob(filepath.Join(launchScriptDir(), "launch-*.sh"))
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	return capacity.Reading{Known: true, Used: int64(len(paths))}
}
