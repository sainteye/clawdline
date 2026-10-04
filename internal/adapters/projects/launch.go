package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/persona"
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

// ReasoningEfforts is `ReasoningEffort.allCases`: the only two names Codex's
// `model_reasoning_effort` is set to from here, and the order the refusal
// lists them in.
//
// Two rather than Codex's own five. The value travels from a brief a person
// wrote to a command line, so it is a closed list like the assistant and the
// permission — a name that is not on it is refused, never passed through — and
// the two on it are the two that mean something to somebody writing a task
// down: the default is already what an unset field gets.
var ReasoningEfforts = []string{"high", "xhigh"}

// KnownReasoningEffort is `ReasoningEffort(rawValue:)`: exact, and two cases.
func KnownReasoningEffort(name string) bool {
	for _, e := range ReasoningEfforts {
		if e == name {
			return true
		}
	}
	return false
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
	const squadCapability = "CLAWDLINE_SQUAD_CAPABILITY_FILE"
	switch assistant {
	case AssistantClaude:
		return []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
			"CLAUDE_PID", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
			"CLAUDE_CODE_BRIDGE_SESSION_ID", "CLAUDE_EFFORT", "AI_AGENT", squadCapability}
	case AssistantCodex:
		return []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID",
			"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", squadCapability}
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
	// ReasoningEffort is Codex's `model_reasoning_effort`, empty for the
	// model's own default. It is a Codex setting and Claude Code has no
	// equivalent flag, so it is refused rather than dropped on the other
	// assistant: a brief that asked for it and got a session without it would
	// have been answered yes to something that did not happen.
	ReasoningEffort string
	// Language is Claude Code's `language` setting, or the BCP 47 tag used in
	// Codex's Board-item instruction. Empty leaves the assistant as it starts.
	// Each is admitted from its own closed language mapper in
	// claude_language.go.
	Language string
	// Persona is a built-in persona id (internal/domain/persona), empty for
	// none. It is a closed list like the model and the language, and a name
	// that is not on it is refused, never passed through.
	Persona string
	// PersonaDir is the directory the daemon wrote the persona texts to
	// (persona.Dir). It is needed exactly when Persona is set.
	PersonaDir string
	// SquadPromptPath is a private, immutable prompt assembled from one
	// persisted launch snapshot. When set it replaces the mutable built-in
	// persona file. Only daemon launch code may fill this field.
	SquadPromptPath string
}

// Launch is ProviderLaunchPlan: the one command a new terminal is given.
type Launch struct {
	ProjectRoot string
	Assistant   string
	Arguments   []string
}

// ErrInvalidLaunch is SessionLaunchRefusal.invalid.
var ErrInvalidLaunch = errors.New("invalid_launch")

// ErrUnknownPersona is a persona id the catalog does not have. It is always
// joined with ErrInvalidLaunch; a route that names the persona in its path
// answers it as `unknown_persona`.
var ErrUnknownPersona = errors.New("the persona is not one this machine knows")

// PersonaArgs is how each assistant is given a persona, every value already
// quoted for the shell the line is typed into: the joiners that build that
// line (Launch.ShellCommand, the broker's shellCommand and openSession) only
// put spaces between arguments.
//
//   - Claude Code reads the file into its system prompt, which survives
//     compaction: `--append-system-prompt-file <path>`.
//   - Codex has no such flag, so its developer instructions point at the
//     file (persona.CodexInstruction). `-c` overrides in memory and writes
//     nothing, and it replaces a `developer_instructions` the person set in
//     ~/.codex/config.toml for this session (docs/personas.md).
func PersonaArgs(assistant string, p persona.Persona, path string) []string {
	if assistant == AssistantCodex {
		return codexDeveloperArgs(persona.CodexInstruction(p, path))
	}
	return []string{"--append-system-prompt-file", ShellQuoted(path)}
}

func codexDeveloperArgs(instructions ...string) []string {
	return []string{"-c", ShellQuoted("developer_instructions=" + TOMLString(strings.Join(instructions, "\n\n")))}
}

const codexNoteInstruction = "When you need the person to choose between concrete options in a Clawdline Session, read `clawdline guide note` and create one answer Note with the question and complete suggested replies before asking in chat. Wait for the person's sent message before acting on the choice."

// TOMLString is value as a TOML basic string: quoted, with the backslash, the
// double quote and every control character escaped.
func TOMLString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

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
	if req.ReasoningEffort != "" {
		if req.Assistant != AssistantCodex {
			return Launch{}, errors.Join(ErrInvalidLaunch,
				errors.New("a reasoning effort is a Codex setting and this is not Codex"))
		}
		if !KnownReasoningEffort(req.ReasoningEffort) {
			return Launch{}, errors.Join(ErrInvalidLaunch,
				errors.New("the provider reasoning effort is not one this machine starts"))
		}
	}
	if req.Language != "" {
		known := req.Assistant == AssistantClaude && knownClaudeLanguage(req.Language) ||
			req.Assistant == AssistantCodex && CodexBoardLanguage(req.Language) == req.Language
		if !known {
			return Launch{}, errors.Join(ErrInvalidLaunch,
				errors.New("the response language is not one this machine starts"))
		}
	}
	var personaArgs []string
	var codexInstructions []string
	if req.SquadPromptPath != "" {
		if !filepath.IsAbs(req.SquadPromptPath) || !usable(req.SquadPromptPath) {
			return Launch{}, errors.Join(ErrInvalidLaunch, errors.New("the squad prompt is not a safe absolute path"))
		}
		if req.Assistant == AssistantCodex {
			instruction := `Your fixed Clawdline role definition and handbook are in the file "` + req.SquadPromptPath +
				`". Read it completely before your first answer and follow it for this session.`
			if _, ok := persona.Known(req.Persona); ok {
				instruction = persona.Marker + req.Persona + " - " + instruction
			}
			codexInstructions = append(codexInstructions, instruction)
		} else {
			personaArgs = []string{"--append-system-prompt-file", ShellQuoted(req.SquadPromptPath)}
		}
	} else if req.Persona != "" {
		p, ok := persona.Known(req.Persona)
		if !ok {
			return Launch{}, errors.Join(ErrInvalidLaunch, ErrUnknownPersona)
		}
		if !usable(req.PersonaDir) {
			return Launch{}, errors.Join(ErrInvalidLaunch,
				errors.New("the persona directory is not a safe absolute path"))
		}
		path := filepath.Join(req.PersonaDir, persona.FileName(p.ID))
		if req.Assistant == AssistantCodex {
			codexInstructions = append(codexInstructions, persona.CodexInstruction(p, path))
		} else {
			personaArgs = PersonaArgs(req.Assistant, p, path)
		}
	}
	if req.Assistant == AssistantCodex && req.Language != "" {
		codexInstructions = append(codexInstructions, codexBoardLanguageInstruction(req.Language))
	}
	if len(codexInstructions) > 0 {
		codexInstructions = append(codexInstructions, codexNoteInstruction)
		personaArgs = codexDeveloperArgs(codexInstructions...)
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
	// After the model and before everything else, which is where
	// `SessionLaunchPolicy.admit` puts it and what the Swift app's measured
	// command line reads as.
	if req.ReasoningEffort != "" {
		args = append(args, "--config", "model_reasoning_effort="+req.ReasoningEffort)
	}
	if req.Language != "" && req.Assistant == AssistantClaude {
		args = append(args, claudeLanguageArgs(req.Language)...)
	}
	args = append(args, personaArgs...)
	return Launch{ProjectRoot: req.ProjectRoot, Assistant: req.Assistant, Arguments: args}, nil
}

// UpdateCheckArgs keeps Codex from asking, on its first screen, whether to
// update itself.
//
// Measured on 2026-09-27: codex-cli 0.154.0 found 0.157.1 published and opened
// with
//
//	✨ Update available! 0.154.0 -> 0.157.1
//	› 1. Update now (runs `sh -c 'curl -fsSL … | CODEX_NON_INTERACTIVE=1 sh'`)
//	  2. Skip
//	  3. Skip until next version
//
// before it drew a composer. Two Board items assigned to a new Codex Session
// that morning both ended "the child is showing a dialog", and every Claude
// assignment beside them was briefed. A session opened to do somebody's work
// is not the place to decide whether to reinstall the CLI; the person still
// sees the offer in any Codex they start in a terminal themselves. Like the
// broker's trustArgs, `-c` holds for this one run and writes nothing; the
// same launch with it drew the composer straight away.
//
// A session started from the console needs it as much: on 2026-09-28 one
// opened on the same menu, read as not yet started, and a line sent to it
// from the console was pasted into the menu and came back send_unsubmitted.
// Every Codex launch this daemon types carries it (Starter.Start and the
// broker's shellCommand and openSession).
func UpdateCheckArgs(assistant string) []string {
	if assistant != AssistantCodex {
		return nil
	}
	return []string{"-c", "check_for_update_on_startup=false"}
}

// ClawdlinePathEnv makes the daemon's stable CLI available to an assistant.
func ClawdlinePathEnv() string {
	return "PATH=" + ShellQuoted(filepath.Join(config.Dir(), "bin")) +
		string(os.PathListSeparator) + `"$PATH"`
}

// ShellCommand is ProviderLaunchPlan.shellCommand: `env -u …` first, so the
// program runs as its own process with nothing in front of it in `ps`.
func (l Launch) ShellCommand() string {
	return l.ShellCommandWithEnv(nil)
}

// ShellCommandWithEnv applies launch-owned values after inherited identity is cleared.
func (l Launch) ShellCommandWithEnv(set []string) string {
	prefix := ""
	if keys := InheritedIdentityKeys(l.Assistant); len(keys) > 0 {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = "-u " + k
		}
		prefix = "env " + strings.Join(append(parts, set...), " ") + " "
	}
	return prefix + ClawdlinePathEnv() + " " + strings.Join(append([]string{l.Assistant}, l.Arguments...), " ")
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
	// TerminalITermNative is the escape hatch to how `iterm` opened a session
	// before 2026-10-02: the program runs in the iTerm2 tab itself, and every
	// read, send and key goes through iTerm2's Apple Events.
	TerminalITermNative TerminalChoice = "iterm_native"
)

// TerminalChoices is every value the `terminal` key takes, in the order
// Settings offers them.
var TerminalChoices = []string{string(TerminalAuto), string(TerminalITerm), string(TerminalITermNative),
	string(TerminalTmux)}

// ParseTerminalChoice reads the `terminal` key. Absent or unknown is `auto`,
// which is what an unset Swift config means.
func ParseTerminalChoice(raw string) TerminalChoice {
	switch TerminalChoice(raw) {
	case TerminalITerm, TerminalITermNative, TerminalTmux:
		return TerminalChoice(raw)
	}
	return TerminalAuto
}

// NamesITerm is whether the setting asks for iTerm2 by name, as a viewer or
// natively: either way a closed iTerm2 is a refusal, never a fallback.
func (c TerminalChoice) NamesITerm() bool {
	return c == TerminalITerm || c == TerminalITermNative
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
	// PlanITermTmux runs the program in a new detached tmux session and shows
	// it in a new iTerm2 tab that only attaches to it. The session is the tmux
	// pane: reading, typing, keys and closing all go through tmux, and the one
	// Apple Event is the one that opens the tab
	// (docs/interface.md, "An iTerm2 tab that only shows a tmux session").
	PlanITermTmux
)

// ITermBundleID is StartPoints.itermBundleID.
const ITermBundleID = "com.googlecode.iterm2"

// ChoosePlan is StartPoints.plan(terminal:running:tmux:), and like it reads
// nothing: every fact it decides from is an argument.
//
// Where the Swift app opened a native iTerm2 tab, a machine with tmux installed
// now opens a tmux session with an iTerm2 tab as its viewer: iTerm2's Apple
// Events stall for minutes at a time, and a session read and typed into through
// them is a session nobody can reach while they do. Only `iterm_native`, or a
// machine without tmux, still opens the native tab.
func ChoosePlan(choice TerminalChoice, itermOpen bool, tmux TmuxReach) PlanKind {
	switch choice {
	case TerminalITermNative:
		if itermOpen {
			return PlanITerm
		}
		return PlanNotRunning
	case TerminalITerm:
		if itermOpen {
			return itermPlan(tmux)
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
		return itermPlan(tmux)
	}
	switch tmux {
	case TmuxRunning:
		return PlanTmux
	case TmuxInstalled:
		return PlanTmuxDetached
	}
	return PlanNotRunning
}

// itermPlan is the plan for an open iTerm2: a viewer tab on a tmux session when
// tmux is installed, running or not, and the native tab when it is not.
func itermPlan(tmux TmuxReach) PlanKind {
	if tmux == TmuxAbsent {
		return PlanITerm
	}
	return PlanITermTmux
}

// TmuxStartedSessionName is Tmux.startedSessionName: the session a server this
// daemon started for itself is given, so there is a name to attach to.
const TmuxStartedSessionName = "clawdline"

// TmuxAttachCommand is Tmux.attachCommand.
func TmuxAttachCommand() string { return "tmux attach -t " + TmuxStartedSessionName }

// TmuxAttachSessionCommand is the line a person types to see the session
// called name. The target is `=name`, which tmux matches exactly rather than
// as a prefix, and it is quoted: zsh reads a word opening with `=` as a
// command to look up.
func TmuxAttachSessionCommand(name string) string {
	return "tmux attach -t " + ShellQuoted("="+name)
}
