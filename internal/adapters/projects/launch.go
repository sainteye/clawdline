package projects

import (
	"errors"
	"strings"
)

// The rules for starting an assistant in a place, from StartPoints.swift and
// SessionLaunchPolicy.swift. Everything here is pure: it decides which
// terminal, and which one line is typed into it, and touches nothing.
//
// The shape is the Swift app's and it is the point of this file. A client
// never names a directory — it names a place id off a list this machine built —
// and never names a command: the assistant is one of two literals, a model is
// a name out of a closed alphabet, and a conversation is a lowercase UUID that
// this machine has just listed for that place. There is no field anywhere on
// the route a path or a command could be written into.

// Assistants a place can be started with, by the path segment that names them.
const (
	AssistantClaude = "claude"
	AssistantCodex  = "codex"
)

// KnownAssistant is `Assistant(rawValue:)`: exact, and two cases.
func KnownAssistant(name string) bool {
	return name == AssistantClaude || name == AssistantCodex
}

// StartModels is Planner.models: the only model names the start route's
// fourth segment answers to.
var StartModels = []string{"haiku", "sonnet", "opus"}

// KnownStartModel is `Planner.models.contains(model)`.
func KnownStartModel(name string) bool {
	for _, m := range StartModels {
		if m == name {
			return true
		}
	}
	return false
}

// ModelName is SessionLaunchPolicy.modelName: `[a-z0-9._-]`, 1…64, never
// opening with `-`. No string it admits holds a character a shell reads.
func ModelName(raw string) (string, bool) {
	if raw == "" || len(raw) > 64 || strings.HasPrefix(raw, "-") {
		return "", false
	}
	for _, r := range raw {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return "", false
		}
	}
	return raw, true
}

// SessionName is SessionLaunchPolicy.sessionID: a UUID as both CLIs write it,
// lowercase, and nothing else. `--resume` takes an optional value, so anything
// looser would open the CLI's own picker in a tab nobody is sitting at.
func SessionName(raw string) (string, bool) {
	if len(raw) != 36 {
		return "", false
	}
	groups := strings.Split(raw, "-")
	want := []int{8, 4, 4, 4, 12}
	if len(groups) != len(want) {
		return "", false
	}
	for i, g := range groups {
		if len(g) != want[i] {
			return "", false
		}
		for _, r := range g {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return "", false
			}
		}
	}
	return raw, true
}

// ShellQuoted is SessionLaunchPolicy.shellQuoted.
func ShellQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// InheritedIdentityKeys is SessionLaunchPolicy.inheritedIdentityKeys: what a
// terminal may have inherited from whatever launched it, and a new session must
// not be handed.
func InheritedIdentityKeys(assistant string) []string {
	switch assistant {
	case AssistantClaude:
		return []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
			"CLAUDE_PID", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
			"CLAUDE_CODE_BRIDGE_SESSION_ID", "CLAUDE_EFFORT", "AI_AGENT"}
	case AssistantCodex:
		return []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID",
			"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"}
	}
	return nil
}

// LaunchRequest is ProviderLaunchRequest for the start and resume routes: the
// permission is always `ask` and there is no extra directory, because nothing
// a browser can reach names either.
type LaunchRequest struct {
	ProjectRoot string
	Assistant   string
	Model       string
	Resume      string
}

// Launch is ProviderLaunchPlan: the one command a new terminal is given.
type Launch struct {
	ProjectRoot string
	Assistant   string
	Arguments   []string
}

// ErrInvalidLaunch is SessionLaunchRefusal.invalid.
var ErrInvalidLaunch = errors.New("invalid_launch")

// Admit is SessionLaunchPolicy.admit with the capability half left to the
// caller, which knows what this platform can open.
func Admit(req LaunchRequest) (Launch, error) {
	if !usable(req.ProjectRoot) {
		return Launch{}, errors.Join(ErrInvalidLaunch,
			errors.New("the launch project root is not a safe absolute path"))
	}
	if !KnownAssistant(req.Assistant) {
		return Launch{}, errors.Join(ErrInvalidLaunch, errors.New("the assistant is not one this machine starts"))
	}
	if req.Model != "" {
		if _, ok := ModelName(req.Model); !ok {
			return Launch{}, errors.Join(ErrInvalidLaunch, errors.New("the provider model is not an allowlisted slug"))
		}
	}
	if req.Resume != "" {
		if _, ok := SessionName(req.Resume); !ok {
			return Launch{}, errors.Join(ErrInvalidLaunch, errors.New("the provider resume id is not a lowercase UUID"))
		}
	}
	var args []string
	// `resume` first: its value is optional to the CLI, so anything but the id
	// immediately after it changes what it means.
	if req.Resume != "" {
		if req.Assistant == AssistantClaude {
			args = append(args, "--resume", req.Resume)
		} else {
			args = append(args, "resume", req.Resume)
		}
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return Launch{ProjectRoot: req.ProjectRoot, Assistant: req.Assistant, Arguments: args}, nil
}

// ShellCommand is ProviderLaunchPlan.shellCommand: `env -u …` first, so the
// program runs as its own process with nothing in front of it in `ps`.
func (l Launch) ShellCommand() string {
	prefix := ""
	if keys := InheritedIdentityKeys(l.Assistant); len(keys) > 0 {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = "-u " + k
		}
		prefix = "env " + strings.Join(parts, " ") + " "
	}
	return prefix + strings.Join(append([]string{l.Assistant}, l.Arguments...), " ")
}

// ShellLine is ProviderLaunchPlan.shellLine: what an iTerm2 tab is typed.
func (l Launch) ShellLine() string {
	return "cd " + ShellQuoted(l.ProjectRoot) + " && " + l.ShellCommand()
}

// TerminalChoice is StartPoints.TerminalChoice, spelled as config.json spells it.
type TerminalChoice string

const (
	TerminalAuto  TerminalChoice = "auto"
	TerminalITerm TerminalChoice = "iterm"
	TerminalTmux  TerminalChoice = "tmux"
)

// ParseTerminalChoice reads the `terminal` key. Absent or unknown is `auto`,
// which is what an unset Swift config means.
func ParseTerminalChoice(raw string) TerminalChoice {
	switch TerminalChoice(raw) {
	case TerminalITerm, TerminalTmux:
		return TerminalChoice(raw)
	}
	return TerminalAuto
}

// TmuxReach is StartPoints.TmuxReach.
type TmuxReach int

const (
	TmuxAbsent TmuxReach = iota
	TmuxInstalled
	TmuxRunning
)

// PlanKind is StartPoints.Plan's case.
type PlanKind int

const (
	PlanITerm PlanKind = iota
	PlanTmux
	PlanTmuxDetached
	PlanNotRunning
	PlanNoTmux
)

// ITermBundleID is StartPoints.itermBundleID.
const ITermBundleID = "com.googlecode.iterm2"

// ChoosePlan is StartPoints.plan(terminal:running:tmux:), and like it reads
// nothing: every fact it decides from is an argument.
func ChoosePlan(choice TerminalChoice, itermOpen bool, tmux TmuxReach) PlanKind {
	switch choice {
	case TerminalITerm:
		if itermOpen {
			return PlanITerm
		}
		return PlanNotRunning
	case TerminalTmux:
		switch tmux {
		case TmuxRunning:
			return PlanTmux
		case TmuxInstalled:
			return PlanTmuxDetached
		}
		return PlanNoTmux
	}
	if itermOpen {
		return PlanITerm
	}
	if tmux == TmuxRunning {
		return PlanTmux
	}
	return PlanNotRunning
}

// TmuxStartedSessionName is Tmux.startedSessionName: the session a server this
// daemon started for itself is given, so there is a name to attach to.
const TmuxStartedSessionName = "clawdline"

// TmuxAttachCommand is Tmux.attachCommand.
func TmuxAttachCommand() string { return "tmux attach -t " + TmuxStartedSessionName }
