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

type PS struct {
	// Open is the table of open files, which is how a Codex session that was
	// not resumed is tied to its conversation. New fills it with this
	// platform's reader; a test puts its own table here.
	Open OpenFiles
	// Head reads the first record of one of those transcripts, which is what
	// says whether it is the conversation or one sub-thread of it. Nil is a
	// scan that cannot read any of them, and answers accordingly.
	Head RolloutHead
}

func New() *PS { return &PS{Open: systemOpenFiles, Head: readRolloutHead} }

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
			s.Binding = session.BindingCommandLine
			s.Evidence = session.EvidenceProcess
		}
		inv.Sessions = append(inv.Sessions, s)
	}
	inv.Sessions = p.bindCodex(ctx, inv.Sessions)
	return inv, nil
}

// bindCodex names the Codex sessions the command line could not.
//
// Claude Code writes a record per pid and Codex writes none, so a Codex
// session that was not started with `codex resume <id>` carries its id
// nowhere a scan can see — and its rollout does not exist yet either, because
// that file is written at the first message and not at startup. Measured on
// this Mac on 2026-09-20: 15 s, 54 s and 88 s between a thread being created
// and its rollout appearing, and three threads that day never got one at all.
//
// What does exist from the first message on is the open descriptor: the
// process holds its own rollout open, and that file says which conversation it
// belongs to. So the window this cannot close is startup to first message, and
// within that window the answer is `no_record` rather than silence — which is
// the whole difference between a session that will name itself shortly and one
// this machine is failing to read.
//
// The pid asked about is the terminal's foreground process, which for Codex is
// the platform binary and not the npm wrapper that started it: a wrapper run as
// `node .../bin/codex` is not classified as an assistant at all, because
// classify reads the executable and that one is `node`. Measured on this Mac on
// 2026-09-20 across four Codex sessions, the wrapper held no rollout open in
// any of them and the binary held every one — so asking the wrong one of the
// two would have turned every session into a false `no_record`.
//
// It runs as one call for every row that needs it, after the table is built,
// so a machine with no unnamed Codex on it pays nothing.
func (p *PS) bindCodex(ctx context.Context, rows []session.Session) []session.Session {
	var pids []int
	for _, s := range rows {
		if s.Assistant == session.AssistantCodex && s.ConversationID == "" && s.PID != 0 {
			pids = append(pids, s.PID)
		}
	}
	if len(pids) == 0 {
		return rows
	}
	files, read := map[int][]string{}, false
	if p.Open != nil {
		files, read = p.Open(ctx, pids)
	}
	for i, s := range rows {
		if s.Assistant != session.AssistantCodex || s.ConversationID != "" || s.PID == 0 {
			continue
		}
		id, binding, detail := codexConversation(files[s.PID], read, p.Head)
		rows[i].ConversationID = id
		rows[i].Binding = binding
		rows[i].BindingDetail = detail
	}
	return rows
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
