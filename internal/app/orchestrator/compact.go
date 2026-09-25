package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// When the Claude sessions this broker opens compact their context.
//
// A week's token ledger on one machine put 73% of the cost in cache reads, and
// in two long sessions 90–95% of it was spent after the context passed 200k
// tokens: every call re-reads the whole history. Claude Code compacts by
// itself only near its 1M window. `CLAUDE_CODE_AUTO_COMPACT_WINDOW=<tokens>`
// in a session's environment moves that point (measured 2026-09-25 against
// claude 2.1.282: a 60000 window compacted at 67k–83k, three times, and the
// task still finished); the `autoCompactWindow` key passed through
// `--settings` did not.
//
// Compaction replaces history with a summary, so it may cost quality — detail
// lost, files read again, decisions forgotten. That is why this is the
// person's setting to experiment with, `claude_auto_compact_window`, off by
// default, rather than a change: nothing is set until they set it, and a
// task.json's `auto_compact_window` overrides it for that one task so two runs
// of one brief can be compared.
//
// Only sessions this broker opens carry it — a child, a Root Assignment, a
// handoff's receiver — and only Claude ones. Codex is never given it, and a
// session the person opened is never this broker's to change.

// AutoCompactEnv is the variable Claude Code reads.
const AutoCompactEnv = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// The window's bounds, in tokens. Below the minimum a session's first turn —
// its instructions, a briefing, the files it opens — is already past the
// window and it compacts on nearly every call; above the maximum is past
// Claude Code's own 1M window, where the variable changes nothing. The
// settings file (nextconfig.Settables) and a task.json take the same range.
const (
	minAutoCompactWindow = 50_000
	maxAutoCompactWindow = 1_000_000
)

// AutoCompactBounds is the window's inclusive range, for the settings table
// and the words that describe it.
func AutoCompactBounds() (lo, hi int64) { return minAutoCompactWindow, maxAutoCompactWindow }

// ValidAutoCompactWindow is whether n is a window this broker applies. Zero
// is not one: it is how "do not intervene" is written.
func ValidAutoCompactWindow(n int64) bool {
	return n >= minAutoCompactWindow && n <= maxAutoCompactWindow
}

// autoCompactRefusal is the one sentence both a task.json and the settings
// file refuse a window with.
func autoCompactRefusal(field string) string {
	return fmt.Sprintf("%s must be null, or a whole number of tokens from %d to %d", field,
		minAutoCompactWindow, maxAutoCompactWindow)
}

// admitAutoCompact reads task.json's `auto_compact_window`. Absent answers
// nil — the machine's setting decides at launch. `null` answers 0: this task
// runs with no window whatever the setting says. A number answers itself.
//
// A number on a Codex task is refused by name, as `reasoning_effort` on a
// Claude one is: the variable is Claude Code's, and a broker that dropped it
// would run the comparison the caller asked for with one half missing. `null`
// is accepted on either, because "do not intervene" is already true of Codex.
func admitAutoCompact(raw json.RawMessage, assistant string) (*int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	none := int64(0)
	if string(raw) == "null" {
		return &none, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil || !ValidAutoCompactWindow(n) {
		return nil, refuse(http.StatusUnprocessableEntity, "bad_task", autoCompactRefusal("auto_compact_window"))
	}
	if assistant != projects.AssistantClaude {
		return nil, refuse(http.StatusUnprocessableEntity, "bad_task",
			"auto_compact_window is only valid when assistant is claude")
	}
	return &n, nil
}

// autoCompactFor is the window a session opened with assistant is launched
// with: requested when a task named one (0 for none), the machine's setting
// otherwise. Nil for anything that is not Claude — the question does not
// apply, which is different from "none was applied".
func (b *Broker) autoCompactFor(assistant string, requested *int64) *int64 {
	if assistant != projects.AssistantClaude {
		return nil
	}
	window := int64(0)
	switch {
	case requested != nil:
		window = *requested
	case b.AutoCompactWindow != nil:
		window = b.AutoCompactWindow()
	}
	if !ValidAutoCompactWindow(window) {
		// A hand-edited settings file with a value out of range is read as
		// the setting's default, not clamped into a window nobody chose.
		window = 0
	}
	return &window
}

// autoCompactEnv is the assignment an `env` prefix carries for window, or
// nothing when no window applies.
func autoCompactEnv(window *int64) []string {
	if window == nil || *window == 0 {
		return nil
	}
	return []string{AutoCompactEnv + "=" + strconv.FormatInt(*window, 10)}
}

// envPrefix is the `env …` a launch line starts with: the identity keys a
// session must not inherit, unset, and then what this broker sets. Empty when
// there is neither.
func envPrefix(assistant string, set []string) string {
	parts := []string{}
	for _, k := range projects.InheritedIdentityKeys(assistant) {
		parts = append(parts, "-u "+k)
	}
	parts = append(parts, set...)
	if len(parts) == 0 {
		return ""
	}
	out := "env"
	for _, p := range parts {
		out += " " + p
	}
	return out + " "
}
