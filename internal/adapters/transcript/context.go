package transcript

import (
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// statusCacheLimit is as much of Claude Code's per-session status-line cache
// this reads. That file is a fixed handful of fields rewritten in place — a
// few hundred bytes — so this is a ceiling a healthy file never approaches,
// not a page of something that grows. Past it the file is not read at all
// rather than decoded from a half: a truncated object decodes to nothing
// useful, and a context reading nobody could take is absent, not zero.
const statusCacheLimit = 64 << 10

// Context is how full one conversation's context window is: the reading the
// status line draws as `ctx 54%`, in the Swift app's `SessionInfo.Context`
// shape.
//
// The two sides come from different places and are not equally certain. The
// used side is counted — the newest turn's own input, or the assistant's own
// running total. The window is exact only when the assistant said what it is;
// otherwise it is this build's estimate for the model, and `WindowIsExact`
// says which of the two happened. See `ClaudeWindow`.
type Context struct {
	// UsedPercent is 0…100, clamped: a conversation past its window is full,
	// not 104% full.
	UsedPercent float64
	// UsedTokens is the counted used side. Absent when the only reading
	// available was a percentage somebody else worked out.
	UsedTokens    int64
	HasUsedTokens bool
	// WindowTokens is how much fits, and is always set — a Context cannot be
	// worked out without one.
	WindowTokens int64
	// WindowIsExact is whether the assistant itself said how much fits.
	WindowIsExact bool
}

// Fill is the used side of a context window on its own: what the next turn
// will carry, before anything has said how much fits. It is what a Claude
// transcript knows; the window comes from elsewhere.
type Fill struct{ UsedTokens int64 }

// ClaudeWindow is the Swift app's `SessionInfo.claudeWindow`: the window sizes
// used only when the per-session status-line cache is absent. It is an
// estimate table — ids are matched by prefix so a dated id keeps its row —
// while `context_window_size` in the cache is Claude Code's exact answer when
// it is there. A model with no row has no window, and a session on it draws no
// context reading at all rather than one against a number nobody stands behind.
func ClaudeWindow(model string) (int64, bool) {
	for _, id := range []string{"claude-fable-5", "claude-opus-5", "claude-sonnet-5"} {
		if strings.HasPrefix(model, id) {
			return 1_000_000, true
		}
	}
	if strings.HasPrefix(model, "claude-haiku-4-5") {
		return 200_000, true
	}
	return 0, false
}

// ClaudeStatusLine is what Claude Code's own status line last wrote down about
// one session, in `~/.claude/statusline-cache/session-<id>.json`. One file,
// read once: it carries both the session's cost and the exact context window,
// and reading it twice per request would be two answers about one moment.
//
// There is deliberately no age check on it. An idle session's window does not
// change, so a file written an hour ago still says truly how much fits; the
// used side, which does move, is read from the transcript when there is one.
type ClaudeStatusLine struct {
	// Found is whether the file was there and decoded. Every other field is
	// meaningless without it.
	Found bool
	// CostUsd is `cost.total_cost_usd`.
	CostUsd float64
	HasCost bool
	// WindowTokens is `context_window.context_window_size`: Claude Code's own
	// exact answer for how much fits.
	WindowTokens    int64
	HasWindowTokens bool
	// UsedTokens is `context_window.total_input_tokens`, which is the only
	// used side there is before the transcript has an assistant turn.
	UsedTokens    int64
	HasUsedTokens bool
	// UsedPercent is `context_window.used_percentage`, kept for writers that
	// supply nothing else.
	UsedPercent    float64
	HasUsedPercent bool
}

// ReadClaudeStatusLine reads that file. An absent, oversized or unreadable one
// is `Found: false` — which is "nobody wrote it down", never "zero".
func ReadClaudeStatusLine(home, sessionID string) ClaudeStatusLine {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return ClaudeStatusLine{}
	}
	path := filepath.Join(home, ".claude", "statusline-cache", "session-"+sessionID+".json")
	data, ok := readAtMost(path, statusCacheLimit)
	if !ok {
		return ClaudeStatusLine{}
	}
	rec, ok := decodeObject(data)
	if !ok {
		return ClaudeStatusLine{}
	}
	out := ClaudeStatusLine{Found: true}
	if cost, ok := rec.object("cost"); ok {
		if v, ok := looseFloat(cost["total_cost_usd"]); ok && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 {
			out.CostUsd, out.HasCost = v, true
		}
	}
	window, ok := rec.object("context_window")
	if !ok {
		return out
	}
	if v, ok := looseIntOK(window, "context_window_size"); ok && v > 0 {
		out.WindowTokens, out.HasWindowTokens = v, true
	}
	if v, ok := looseIntOK(window, "total_input_tokens"); ok && v >= 0 {
		out.UsedTokens, out.HasUsedTokens = v, true
	}
	if v, ok := looseFloat(window["used_percentage"]); ok && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 {
		out.UsedPercent, out.HasUsedPercent = v, true
	}
	return out
}

// ClaudeContext is the Swift app's `SessionInfo.claudeContext`: Claude's live
// conversation fill.
//
// The window is the stable fact the status-line cache records; the used side
// is the newest parent assistant turn in the transcript, because a cache file
// can be one turn behind and a sidechain is a different conversation. Before
// the first assistant turn the cache is the only reading there is, and an
// older writer that supplied only a percentage gets an honest partial answer
// rather than a made-up token count.
func ClaudeContext(fill *Fill, status ClaudeStatusLine, model string) (Context, bool) {
	window, exact := status.WindowTokens, true
	if !status.HasWindowTokens {
		var ok bool
		if window, ok = ClaudeWindow(model); !ok {
			return Context{}, false
		}
		exact = false
	}
	if fill != nil {
		return contextOf(fill.UsedTokens, window, exact), true
	}
	if status.HasUsedTokens {
		return contextOf(status.UsedTokens, window, exact), true
	}
	if status.HasUsedPercent {
		return Context{UsedPercent: status.UsedPercent, WindowTokens: window, WindowIsExact: exact}, true
	}
	return Context{}, false
}

// contextOf is a counted used side against a window.
func contextOf(used, window int64, exact bool) Context {
	percent := float64(used) * 100 / float64(window)
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	return Context{
		UsedPercent: percent, UsedTokens: used, HasUsedTokens: true,
		WindowTokens: window, WindowIsExact: exact,
	}
}

// claudeFill is the used side of Claude's context window, read from the end of
// the transcript: the newest assistant turn of this conversation, whatever its
// input side cost.
//
// It is the one reading here that survives a transcript past the read limit,
// because the newest turn is at the end of the file and the tail is the end of
// the file. A cumulative total cannot be summed from a tail; this can.
func claudeFill(data []byte) *Fill {
	var out *Fill
	reversedLines(data, func(line []byte) bool {
		if !bytes.Contains(line, []byte(`"assistant"`)) || !bytes.Contains(line, []byte(`"usage"`)) {
			return false
		}
		rec, ok := decodeObject(line)
		if !ok {
			return false
		}
		if kind, _ := rec.str("type"); kind != "assistant" {
			return false
		}
		// A sidechain is a different conversation sharing this file; its turns
		// never entered the window this reading is about.
		if side, ok := rec.boolean("isSidechain"); ok && side {
			return false
		}
		message, ok := rec.object("message")
		if !ok {
			return false
		}
		counts, ok := message.object("usage")
		if !ok {
			return false
		}
		// `<synthetic>` is what Claude Code writes when the provider refused
		// the turn, and it carries a complete, all-zero `usage`. It satisfies
		// every guard above, so without this the line reads a green `ctx 0%`
		// for a conversation that just hit its window — the one moment the
		// number is worth looking at.
		if named, ok := message.str("model"); ok && strings.HasPrefix(named, "<") {
			return false
		}
		keys := []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"}
		named, total := false, int64(0)
		for _, key := range keys {
			if _, present := counts[key]; !present {
				continue
			}
			named = true
			v, ok := looseIntOK(counts, key)
			if !ok || v < 0 {
				// A malformed counter is not a zero: this turn says nothing,
				// so the one before it is asked instead.
				return false
			}
			total += v
		}
		if !named {
			return false
		}
		out = &Fill{UsedTokens: total}
		return true
	})
	return out
}

// readAtMost is a whole small file, or nothing when it is larger than limit.
func readAtMost(path string, limit int64) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, false
	}
	return data, true
}
