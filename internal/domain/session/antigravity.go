package session

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// AntigravityScreenKind describes a positively recognized native 1.4.5 surface.
type AntigravityScreenKind string

const (
	AntigravityUnknown AntigravityScreenKind = "unknown"
	AntigravityIdle    AntigravityScreenKind = "idle"
	AntigravityWorking AntigravityScreenKind = "working"
	AntigravityDraft   AntigravityScreenKind = "draft"
	AntigravityMenu    AntigravityScreenKind = "menu"
)

type AntigravityScreen struct {
	Kind    AntigravityScreenKind
	Draft   string
	Working string
	Menu    Menu
	// Revision commits to the active permission question, offered grants and selection.
	Revision string
}

// AntigravityPermissionEffect distinguishes the actual offered grant scopes.
// The printed row number is an identifier, never a digit to send to the TUI.
type AntigravityPermissionEffect string

const (
	AntigravityAllowOnce               AntigravityPermissionEffect = "allow_once"
	AntigravityAllowConversationPrefix AntigravityPermissionEffect = "allow_conversation_prefix"
	AntigravityAllowPersistedPrefix    AntigravityPermissionEffect = "allow_persisted_prefix"
	AntigravityCancel                  AntigravityPermissionEffect = "cancel"
)

func (s AntigravityScreen) PermissionEffect(option int) (AntigravityPermissionEffect, bool) {
	if s.Kind != AntigravityMenu || s.Revision == "" {
		return "", false
	}
	switch option {
	case 1:
		return AntigravityAllowOnce, true
	case 2:
		return AntigravityAllowConversationPrefix, true
	case 3:
		return AntigravityAllowPersistedPrefix, true
	case 4:
		return AntigravityCancel, true
	}
	return "", false
}

// ReadAntigravityScreen never treats a bare > as readiness. fullyIdle must come
// from a fresh, verified, nonfailed native Stop, not PostInvocation or stored state.
func ReadAntigravityScreen(screen string, fullyIdle bool) AntigravityScreen {
	unknown := AntigravityScreen{Kind: AntigravityUnknown}
	lines := strings.Split(strings.TrimSpace(Plain(screen)), "\n")
	if len(lines) == 0 {
		return unknown
	}
	bottom := strings.TrimSpace(lines[len(lines)-1])
	// The native footer ends at the model/effort. Unknown wrapping stays unknown.
	if !strings.Contains(bottom, "Gemini ") || !strings.Contains(bottom, " · ") {
		return unknown
	}
	if m, revision, ok := antigravityPermission(lines); ok {
		return AntigravityScreen{Kind: AntigravityMenu, Menu: m, Revision: revision}
	}
	// The lowest complete composer must close directly onto the current footer.
	end := len(lines) - 2
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if end < 0 || !isBoxRule(lines[end]) {
		return unknown
	}
	start := end - 1
	for start >= 0 && !isBoxRule(lines[start]) {
		start--
	}
	if start < 0 || start+1 >= end {
		return unknown
	}
	if strings.TrimSpace(lines[start]) != strings.TrimSpace(lines[end]) {
		return unknown
	}
	head := strings.TrimSpace(lines[start+1])
	if head != ">" && !strings.HasPrefix(head, "> ") {
		return unknown
	}
	text := strings.TrimSpace(strings.TrimPrefix(head, ">"))
	for _, line := range lines[start+2 : end] {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			return unknown
		}
		text += "\n" + line
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		return unknown
	}
	if text == "Accept-edits mode: file edits auto-approved (shift+tab to cycle)" &&
		strings.HasPrefix(bottom, "? for shortcuts") && strings.Contains(bottom, "accept-edits · ") {
		text = ""
	}
	// Only the immediately adjacent active notice is consulted. Old errors or
	// quoted menus elsewhere in the transcript cannot become current controls.
	adjacent := start - 1
	for adjacent >= 0 && strings.TrimSpace(lines[adjacent]) == "" {
		adjacent--
	}
	if adjacent >= 0 {
		notice := strings.TrimSpace(lines[adjacent])
		for _, prefix := range []string{"Error:", "Quota exhausted", "Login required", "Sign in to continue", "Trust this folder?"} {
			if strings.HasPrefix(notice, prefix) {
				return unknown
			}
		}
	}
	// A working footer is live evidence; a quoted Generating line alone is not.
	if strings.HasPrefix(bottom, "esc to cancel") {
		if start > 0 {
			work := strings.TrimSpace(lines[start-1])
			if strings.Contains(work, "Generating...") && strings.ContainsAny(work, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⣯⣷⣾⣽⣻⢿⡿⣟") {
				return AntigravityScreen{Kind: AntigravityWorking, Draft: text, Working: "Generating..."}
			}
		}
		return unknown
	}
	if text != "" {
		return AntigravityScreen{Kind: AntigravityDraft, Draft: text}
	}
	if fullyIdle || strings.HasPrefix(bottom, "? for shortcuts") {
		return AntigravityScreen{Kind: AntigravityIdle}
	}
	return unknown
}

func antigravityPermission(lines []string) (Menu, string, bool) {
	// This is the measured command approval surface, not a generic > picker.
	last := len(lines) - 1
	if last < 1 || !strings.HasPrefix(strings.TrimSpace(lines[last]), "esc to cancel") {
		return Menu{}, "", false
	}
	control := last - 1
	for control >= 0 && strings.TrimSpace(lines[control]) == "" {
		control--
	}
	if control < 0 || strings.TrimSpace(lines[control]) != "↑/↓ Navigate · tab Amend · ctrl+g edit/expand command" {
		return Menu{}, "", false
	}
	first := control
	for first > 0 && strings.TrimSpace(lines[first-1]) != "Run this command?" {
		first--
	}
	if first == 0 {
		return Menu{}, "", false
	}
	question := first - 2
	for question >= 0 && strings.TrimSpace(lines[question]) != "Requesting permission for:" {
		if isBoxRule(lines[question]) || strings.HasPrefix(strings.TrimSpace(lines[question]), ">") {
			return Menu{}, "", false
		}
		question--
	}
	if question < 0 {
		return Menu{}, "", false
	}
	frame := question - 1
	for frame >= 0 && strings.TrimSpace(lines[frame]) == "" {
		frame--
	}
	if frame < 0 || !isBoxRule(lines[frame]) {
		return Menu{}, "", false
	}
	heading := frame - 1
	for heading >= 0 && strings.TrimSpace(lines[heading]) == "" {
		heading--
	}
	if heading < 0 || strings.TrimSpace(lines[heading]) != "Command" {
		return Menu{}, "", false
	}
	m := Menu{Question: strings.TrimSpace(strings.Join(lines[question:first], "\n")), Numbered: false}
	for _, raw := range lines[first:control] {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		selected := strings.HasPrefix(t, "> ")
		if selected {
			t = strings.TrimPrefix(t, "> ")
		}
		r, ok := menuRow(t)
		if !ok || r.number != len(m.Options)+1 {
			return Menu{}, "", false
		}
		m.Options = append(m.Options, MenuOption{Number: r.number, Label: r.label, Selected: selected})
	}
	if len(m.Options) != 4 {
		return Menu{}, "", false
	}
	if m.Options[0].Label != "Yes, run command" || m.Options[3].Label != "No, cancel" ||
		!strings.HasPrefix(m.Options[1].Label, "Yes, and always allow in this conversation for commands that start with '") ||
		!strings.HasPrefix(m.Options[2].Label, "Yes, and always allow for commands that start with '") ||
		!strings.HasSuffix(m.Options[2].Label, "' (Persist to settings.json)") {
		return Menu{}, "", false
	}
	selected := 0
	for _, o := range m.Options {
		if o.Selected {
			selected++
		}
	}
	if selected != 1 {
		return Menu{}, "", false
	}
	m.Selected = firstSelected(m.Options)
	sum := sha256.Sum256([]byte(strings.Join(lines[question:control+1], "\n")))
	return m, hex.EncodeToString(sum[:]), true
}

// AntigravitySendReady is admission for ordinary text, never a slash command.
func AntigravitySendReady(screen, text string, fullyIdle bool) bool {
	return strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), "/") && !strings.HasPrefix(strings.TrimSpace(text), "!") &&
		ReadAntigravityScreen(screen, fullyIdle).Kind == AntigravityIdle
}
