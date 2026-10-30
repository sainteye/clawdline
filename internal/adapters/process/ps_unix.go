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

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type PS struct {
	// RemoteControl binds a native headless worker to its kernel generation and bridge.
	// Nil leaves external SDK workers undiscovered, as before.
	RemoteControl func(context.Context, int, string) (session.Session, bool)
	// table is a single process-table observation; nil uses the kernel reader.
	table func(context.Context) ([]byte, error)
	// Antigravity must validate the executable fingerprint and kernel birth.
	// Nil leaves both the standalone CLI and the old same-name IDE unclassified.
	Antigravity ports.AntigravityProcessVerifier
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

// The launch directory name travels in the provider's prompt argument. It
// ties an observed process to one pending intent without guessing from cwd or
// a terminal ID that may later be reused.
var squadLaunchID = regexp.MustCompile(`(?:/|\\)squad(?:/|\\)launches(?:/|\\)([A-Za-z0-9_-]{22})(?:/|\\)`)

// Scan lists the process table once and keeps one session per terminal.
//
// The column set deliberately excludes `lstart`, whose rendering follows the
// locale and changes width during the month, so nothing here counts columns.
// Subprocesses are pinned to LC_ALL=C for the same family of reasons.
func (p *PS) Scan(ctx context.Context) (session.Inventory, error) { return p.scan(ctx, nil) }

// ScanTTYs resolves only explicitly selected terminals, before reading provider records.
func (p *PS) ScanTTYs(ctx context.Context, ttys map[string]bool) (session.Inventory, error) {
	return p.scan(ctx, ttys)
}

func (p *PS) scan(ctx context.Context, ttys map[string]bool) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "ps",
		Complete:   true,
	}

	out, err := p.processTable(ctx)
	if err != nil {
		// An unreadable process table proves nothing about what is running.
		inv.Complete = false
		inv.Notes = append(inv.Notes, "ps failed: "+err.Error())
		return inv, err
	}

	workflowParents := workflowParentTable(string(out))
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		tty, pidText, pgidText, tpgidText := fields[0], fields[1], fields[2], fields[3]
		if ttys != nil && !ttys[tty] {
			continue
		}
		command := strings.Join(fields[5:], " ")

		assistant := classify(command)
		if assistant == "" && nativeClaudeRemoteExecutable(fields[5]) && claudeRemoteWorker(fields[6:]) {
			assistant = session.AssistantClaude
		}
		pid, _ := strconv.Atoi(pidText)
		if assistant == session.AssistantClaude {
			args := fields[6:]
			// The server is a launcher, not one of the conversations it serves.
			if len(args) > 0 && (args[0] == "remote-control" || args[0] == "rc") {
				continue
			}
			if claudeRemoteWorker(args) {
				// A worker can inherit its launcher's tty and tmux environment.
				// Never merge its conversation into that launcher's terminal.
				if ttys == nil && p.RemoteControl != nil {
					if row, ok := p.RemoteControl(ctx, pid, RemoteControlID(args)); ok {
						inv.Sessions = append(inv.Sessions, row)
					} else {
						inv.Complete = false
						inv.Notes = append(inv.Notes, "claude_remote_identity_unavailable")
					}
				}
				continue
			}
		}
		var antigravity ports.AntigravityProcess
		if assistant == "" && path.Base(fields[5]) == "agy" {
			fact, ok, complete := p.antigravityProcessStatus(ctx, pid, tty)
			if !complete {
				inv.Complete = false
				inv.Notes = append(inv.Notes, "antigravity_process_unreadable")
			}
			if ok {
				assistant = session.AssistantAntigravity
				antigravity = fact
			}
		}
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

		s := session.Session{
			ID:        tty,
			Backend:   session.BackendITerm,
			TTY:       tty,
			PID:       pid,
			Assistant: assistant,
			State:     session.StateUnknown,
			Evidence:  session.EvidenceProcess,
		}
		if assistant == session.AssistantAntigravity {
			s.Binding = session.BindingNoRecord
			// Launch argv is provisional: /clear, /fork and /resume can change it.
			s.ExpectedStart, _ = time.Parse(time.RFC3339Nano, antigravity.Birth)
		}
		if m := resumeID.FindStringSubmatch(command); assistant != session.AssistantAntigravity && len(m) == 2 {
			s.ConversationID = m[1]
			s.Binding = session.BindingCommandLine
			s.Evidence = session.EvidenceProcess
		}
		// The persona is read off the same command line, by the same rule:
		// what the process was launched with is proof, not inference.
		s.Persona = persona.FromCommandLine(command)
		s.WorkflowLaunchID = workflowAncestor(workflowParents, pid)
		if m := squadLaunchID.FindStringSubmatch(command); len(m) == 2 {
			s.SquadLaunchID = m[1]
		}
		inv.Sessions = append(inv.Sessions, s)
	}
	inv.Sessions = p.bindCodex(ctx, inv.Sessions, codexWriterPIDs(string(out), inv.Sessions))
	return inv, nil
}

func (p *PS) processTable(ctx context.Context) ([]byte, error) {
	if p.table != nil {
		return p.table(ctx)
	}
	cmd := exec.CommandContext(ctx, "/bin/ps", "-ax", "-o", "tty=,pid=,pgid=,tpgid=,ppid=,command=")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	return cmd.Output()
}

func (p *PS) antigravityProcess(ctx context.Context, pid int, tty string) (ports.AntigravityProcess, bool) {
	fact, ok, _ := p.antigravityProcessStatus(ctx, pid, tty)
	return fact, ok
}
func (p *PS) antigravityProcessStatus(ctx context.Context, pid int, tty string) (ports.AntigravityProcess, bool, bool) {
	if p.Antigravity == nil {
		return ports.AntigravityProcess{}, false, true
	}
	fact, err := p.Antigravity.VerifyAntigravityProcess(ctx, pid)
	if err != nil {
		known, ok := err.(ports.AntigravityNonCLI)
		return fact, false, ok && known.AntigravityNotStandalone()
	}
	ok := fact.PID == pid && fact.Birth != "" && fact.Executable != "" && fact.TTY == tty && fact.TerminalID != ""
	return fact, ok, ok
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
// What exists from the first message on in direct mode is the open descriptor:
// the process holds its own rollout open, and that file says which conversation
// it belongs to. Managed app-server versions may put that descriptor on the
// dedicated no-tty app-server descendant instead; its parent chain and native
// subcommand are read from this same process snapshot. That writer joins the
// one open-file batch, and conflicting conversation roots remain ambiguous.
// A shared daemon outside that ancestry remains unbound here; transcript.Host
// can separately validate its live writer record,
// rollout, name, cwd and exact iTerm title after this scan. Before the first
// message the answer is `no_record` rather than silence — which is the whole
// difference between a session that will name itself shortly and one this
// machine is failing to read.
//
// The pid asked about is the terminal's foreground process, which for Codex is
// the platform binary and not the npm wrapper that started it: a wrapper run as
// `node .../bin/codex` is not classified as an assistant at all, because
// classify reads the executable and that one is `node`. Measured on this Mac on
// 2026-09-20 across four direct Codex sessions, the wrapper held no rollout
// open in any of them and the binary held every one — so asking the wrong one
// of the two would have turned every session into a false `no_record`.
//
// The same read also asks the process for its cwd. Once a rollout exists its
// own head remains the stronger answer; before the first message there is no
// head, and the process cwd is the only evidence available. It lets that first
// screen carry the same project name and mark it will have after the message.
//
// A session resumed from the command line is already named, so no rollout
// head is read for it. When its directory is missing it still joins this one
// batched kernel read; the identity stays untouched and only cwd is filled.
//
// It runs as one call for every Codex row after the table is built. The open
// file table now answers two questions from the same observation: which
// conversation an unnamed process belongs to, and which positively-classified
// subagent threads it currently holds open. Running is therefore direct
// process evidence for Codex, not the missing-ending inference Claude needs.
func (p *PS) bindCodex(ctx context.Context, rows []session.Session, writers ...map[int][]int) []session.Session {
	var pids []int
	seen := map[int]bool{}
	addPID := func(pid int) {
		if !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}
	for _, s := range rows {
		if s.Assistant == session.AssistantCodex && s.PID != 0 {
			addPID(s.PID)
			if len(writers) > 0 {
				for _, pid := range writers[0][s.PID] {
					addPID(pid)
				}
			}
		}
	}
	if len(pids) == 0 {
		return rows
	}
	files, cwds, read := map[int][]string{}, map[int]string{}, false
	if p.Open != nil {
		files, cwds, read = p.Open(ctx, pids)
	}
	// Identity and background work ask the same first-line questions. Cache
	// them for this one process-table reading so a rollout is opened once,
	// even when it answers both.
	head := p.Head
	if head != nil {
		values := map[string]RolloutMeta{}
		known := map[string]bool{}
		original := head
		head = func(path string) (RolloutMeta, bool) {
			if ok, seen := known[path]; seen {
				return values[path], ok
			}
			value, ok := original(path)
			known[path] = ok
			if ok {
				values[path] = value
			}
			return value, ok
		}
	}
	for i, s := range rows {
		if s.Assistant != session.AssistantCodex || s.PID == 0 {
			continue
		}
		if rows[i].CWD == "" {
			rows[i].CWD = cwds[s.PID]
		}
		paths := files[s.PID]
		// Keep a directly-owned rollout authoritative. Managed app-server
		// versions keep it on a no-tty descendant instead; only fall back to
		// those proven descendants when the foreground TUI holds no rollout.
		if len(writers) > 0 && !hasCodexRollout(paths) {
			paths = codexWriterFiles(files, writers[0][s.PID])
		}
		if s.ConversationID == "" {
			id, binding, detail, cwd := codexConversation(paths, read, head)
			rows[i].ConversationID = id
			rows[i].Binding = binding
			rows[i].BindingDetail = detail
			// A directory nobody read never takes one away: the row keeps
			// whatever else proved where the session is, and an empty answer here
			// stays the empty cell the console draws no project beside.
			if cwd != "" {
				rows[i].CWD = cwd
			}
		}
		rows[i].Agents, rows[i].AgentReading = codexAgents(
			paths, read, rows[i].ConversationID, head)
	}
	return rows
}

// codexAgents is the current provider-native work under one conversation.
//
// A rollout is admitted only when Codex positively wrote `thread_source:
// subagent`; a missing field is not a subagent (retired Codex.swift:94-101).
// An unreadable table or an unrecognised rollout head makes the reading
// unknown instead of turning it into zero, which is the U4 rule.
func codexAgents(paths []string, read bool, conversation string, head RolloutHead) ([]session.Agent, session.AgentReading) {
	if !read {
		return nil, session.AgentReading{State: session.AgentsUnknown, Reason: session.AgentsUnreadable}
	}
	agents := []session.Agent{}
	for _, path := range paths {
		if _, ok := codexRolloutID(path); !ok {
			continue
		}
		meta, ok := headOf(head, path)
		if !ok {
			return nil, session.AgentReading{State: session.AgentsUnknown, Reason: session.AgentsUnrecognized}
		}
		if meta.ThreadSource == "" {
			// This is an older person's rollout unless another positive field
			// says otherwise. Absence is deliberately not classification.
			continue
		}
		if meta.ThreadSource != "subagent" {
			continue
		}
		if conversation == "" || meta.Conversation != conversation || meta.Thread == "" || meta.Thread == conversation {
			return nil, session.AgentReading{State: session.AgentsUnknown, Reason: session.AgentsUnrecognized}
		}
		kind := meta.AgentType
		if kind == "" {
			kind = "subagent"
		}
		agents = append(agents, session.Agent{
			ID: meta.Thread, What: kind, Type: kind, State: session.AgentRunning,
		})
	}
	return agents, session.AgentReading{State: session.AgentsComplete}
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
