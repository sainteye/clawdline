// Package transcript reads what the assistants write about themselves. These
// are their own records, so a reading here is better evidence than anything
// drawn on a screen — and the sessions say so on the wire.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// ClaudeRegistry is one row of ~/.claude/sessions/<pid>.json: Claude Code's own
// live record of a running session. It carries the conversation id, the tmux
// pane and an official status, none of which has to be guessed from a screen.
type ClaudeRegistry struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Tmux      string `json:"tmux"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

// Pane returns the tmux pane id out of a value like "clawdline:@8.%12".
func (r ClaudeRegistry) Pane() string {
	if i := strings.LastIndex(r.Tmux, "."); i >= 0 {
		return r.Tmux[i+1:]
	}
	return ""
}

// ClaudeRegistryByPID reads every live session record Claude Code keeps.
func ClaudeRegistryByPID(home string) map[int]ClaudeRegistry {
	out := map[int]ClaudeRegistry{}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r ClaudeRegistry
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		out[pid] = r
	}
	return out
}

// ProjectSlug is the folder Claude Code files a working directory's
// transcripts under: every UTF-16 code unit that is not an ASCII letter or
// digit becomes a dash. That is Claude's scheme, reproduced as the Swift app's
// `TranscriptPaths.slug` reproduces it — `/a/b_c.d` is `-a-b-c-d`, and a
// replacement of the separators alone finds nothing for either of those.
func ProjectSlug(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		if r < utf8.RuneSelf && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			b.WriteRune(r)
			continue
		}
		units := utf16.RuneLen(r)
		if units < 1 {
			units = 1
		}
		b.WriteString(strings.Repeat("-", units))
	}
	return b.String()
}

// titleTail is how much of a transcript's end the title is read from. A
// session repeats its `aiTitle` as it goes, so the newest one is in here; a
// `/rename` can be anywhere, which is why that one alone is also looked for
// further back.
const titleTail = 512_000

// Titles reads what a Claude conversation calls itself, once per version of
// its transcript.
//
// The fleet list asks on every reading, for every session, and the answer
// changes only when the file does. So the answer is kept against the file's
// size and modification time, and an unchanged transcript is not opened.
//
// It holds the register's `cache.transcript_titles` rows at most, letting go
// of the one read longest ago.
type Titles struct {
	mu   sync.Mutex
	seen *lru[titleReading]
}

type titleReading struct {
	size   int64
	mod    time.Time
	title  string
	custom string
}

func NewTitles() *Titles { return &Titles{seen: newLRU[titleReading](capacity.CacheTranscriptTitles)} }

// SetLimit is the capacity override for `cache.transcript_titles`.
func (t *Titles) SetLimit(n int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen.setLimit(n)
}

// Reading is the `cache.transcript_titles` row.
func (t *Titles) Reading() capacity.Reading {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen.reading()
}

// Read returns the conversation's effective title — the last `customTitle` a
// `/rename` wrote, otherwise the last `aiTitle` — and the `customTitle` on its
// own. Both are empty for a file that cannot be read.
//
// A rename can sit arbitrarily far before the tail, so a file larger than the
// tail with no rename in it is scanned once in full. After that an append that
// stays within the tail window keeps the rename already found: anything newer
// would be in the tail.
func (t *Titles) Read(path string) (title, custom string) {
	st, err := os.Stat(path)
	if err != nil {
		return "", ""
	}
	size, mod := st.Size(), st.ModTime()

	t.mu.Lock()
	prev, had := t.seen.get(path)
	t.mu.Unlock()
	if had && prev.size == size && prev.mod.Equal(mod) {
		return prev.title, prev.custom
	}

	data, complete, err := tailData(path, titleTail)
	if err != nil {
		return "", ""
	}
	recent := lastTitle(data, "customTitle")
	ai := lastTitle(data, "aiTitle")
	switch {
	case recent != "":
		title, custom = recent, recent
	case had && size > prev.size && size-prev.size <= titleTail:
		custom = prev.custom
		title = firstNonEmpty(custom, ai)
	case complete:
		title = ai
	default:
		custom = lastTitleInFile(path, "customTitle")
		title = firstNonEmpty(custom, ai)
	}

	t.mu.Lock()
	t.seen.put(path, titleReading{size: size, mod: mod, title: title, custom: custom})
	t.mu.Unlock()
	return title, custom
}

// lastTitle is the last record in data whose top-level key is a non-empty
// string. Every line is tested for the key before anything is decoded: a title
// is one line in hundreds.
func lastTitle(data []byte, key string) string {
	needle := []byte(`"` + key + `"`)
	found := ""
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !bytes.Contains(line, needle) {
			continue
		}
		if v := titleIn(line, key); v != "" {
			found = v
		}
	}
	return found
}

// lastTitleInFile is lastTitle over the whole file, read forward a line at a
// time so a transcript of tens of megabytes is never held at once.
func lastTitleInFile(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	needle := []byte(`"` + key + `"`)
	found := ""
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(line, needle) {
			if v := titleIn(line, key); v != "" {
				found = v
			}
		}
		if err != nil {
			return found
		}
	}
}

func titleIn(line []byte, key string) string {
	var rec map[string]json.RawMessage
	if json.Unmarshal(line, &rec) != nil {
		return ""
	}
	var v string
	if json.Unmarshal(rec[key], &v) != nil {
		return ""
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
