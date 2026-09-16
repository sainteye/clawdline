//go:build darwin || linux

// Package process reads the process table. On Unix that is `ps`; the Windows
// implementation lives beside this file and answers the same port.
package process

import (
	"context"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

type PS struct{}

func New() *PS { return &PS{} }

// Scan lists the process table once.
//
// The column set is deliberately free of `lstart`: its rendering follows the
// locale, and in some locales the day field changes width, so any parser that
// counts columns is wrong for part of every month. Here `command` is last and
// everything before it is a single token, which needs no counting at all.
// Child processes are pinned to LC_ALL=C for the same family of reasons.
func (p *PS) Scan(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "ps",
		Complete:   true,
	}

	cmd := exec.CommandContext(ctx, "/bin/ps", "-ax", "-o", "tty=,pid=,ppid=,command=")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		// An unreadable process table proves nothing about what is running.
		inv.Complete = false
		inv.Notes = append(inv.Notes, "ps failed: "+err.Error())
		return inv, err
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		tty, pidText := fields[0], fields[1]
		command := strings.Join(fields[3:], " ")

		assistant := classify(command)
		if assistant == "" || tty == "??" || tty == "-" {
			continue
		}
		pid, _ := strconv.Atoi(pidText)
		inv.Sessions = append(inv.Sessions, session.Session{
			ID:        tty,
			Backend:   session.BackendITerm,
			TTY:       tty,
			PID:       pid,
			Assistant: assistant,
			State:     session.StateUnknown,
			// A process proves something is running and nothing about what it
			// is doing. Saying so is the point of this field.
			Evidence: session.EvidenceProcess,
		})
	}
	return inv, nil
}

// classify decides whether a command line is an assistant we coordinate.
//
// It matches the executable, never the whole line: a grep for "claude" would
// also match a shell that merely mentions it, and an editor holding this file
// open. That distinction has its own recorded failure in the Swift app's notes.
func classify(command string) session.Assistant {
	if command == "" {
		return ""
	}
	first := strings.Fields(command)[0]
	switch path.Base(strings.TrimSuffix(first, ":")) {
	case "claude":
		return session.AssistantClaude
	case "codex":
		return session.AssistantCodex
	}
	return ""
}
