//go:build darwin || linux

// Package process reads the process table. On Unix that is `ps`; the Windows
// implementation answers the same port beside this file.
package process

import (
	"context"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

type PS struct{}

func New() *PS { return &PS{} }

// resumeID matches the conversation a session was resumed with. Both assistants
// put it on their own command line, which makes it proof rather than inference:
// `claude --resume <uuid>` and `codex resume <uuid>`.
var resumeID = regexp.MustCompile(`(?:--resume|\bresume)[= ]+([0-9a-fA-F-]{8,})`)

// Scan lists the process table once and keeps one session per terminal.
//
// The column set deliberately excludes `lstart`, whose rendering follows the
// locale and changes width during the month, so nothing here counts columns.
// Subprocesses are pinned to LC_ALL=C for the same family of reasons.
func (p *PS) Scan(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "ps",
		Complete:   true,
	}

	cmd := exec.CommandContext(ctx, "/bin/ps", "-ax", "-o", "tty=,pid=,pgid=,tpgid=,command=")
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
		if len(fields) < 5 {
			continue
		}
		tty, pidText, pgidText, tpgidText := fields[0], fields[1], fields[2], fields[3]
		command := strings.Join(fields[4:], " ")

		assistant := classify(command)
		if assistant == "" || tty == "??" || tty == "-" {
			continue
		}
		// One terminal runs one session, and the terminal itself says which
		// process that is: the foreground process group is what the tty is
		// currently attached to. Matching on the program name instead would
		// also collect the sandbox and app-server helpers an assistant spawns
		// beside itself — on this machine one terminal carried five processes
		// named `codex`, only one of which was the session.
		if pgidText != tpgidText || pgidText == "0" {
			continue
		}

		pid, _ := strconv.Atoi(pidText)
		s := session.Session{
			ID:        tty,
			Backend:   session.BackendITerm,
			TTY:       tty,
			PID:       pid,
			Assistant: assistant,
			State:     session.StateUnknown,
			Evidence:  session.EvidenceProcess,
		}
		if m := resumeID.FindStringSubmatch(command); len(m) == 2 {
			s.ConversationID = m[1]
			s.Evidence = session.EvidenceProcess
		}
		inv.Sessions = append(inv.Sessions, s)
	}
	return inv, nil
}

// classify decides whether a command line is an assistant we coordinate. It
// matches the executable, never the whole line: a grep for "claude" would also
// match a shell that merely mentions it.
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
