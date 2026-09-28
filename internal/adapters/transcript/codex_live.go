package transcript

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// codexLiveIdentity is one conversation whose writer lock is held now. It is
// not a historical index: refreshCodexLive drops it as soon as the lock is no
// longer held.
type codexLiveIdentity struct {
	ID   string
	Name string
	CWD  string
}

// refreshCodexLive reads the second identity path used by Codex 0.157.1's
// managed app-server. In that mode the foreground TUI owns the tty while the
// shared daemon owns the rollout descriptor, so asking only the TUI's open
// files answers a false `no_record`.
//
// A writer-lock filename alone proves too little. A candidate is admitted
// only when the lock is held now, its rollout head says it is the conversation
// root, and Codex's own name index names it. The terminal title and cwd are
// checked later, once terminal and process inventories have been merged.
func (h *Host) refreshCodexLive() {
	ids, complete := heldCodexThreadIDs(filepath.Join(h.Home, ".codex", "thread-writer-locks"))
	if !complete {
		h.codexLive = nil
		return
	}
	needed := make(map[string]bool, len(ids))
	paths := make(map[string]string, len(ids))
	for _, id := range ids {
		if h.codex[id] == "" {
			continue
		}
		if path := h.codexRollouts[id]; path != "" {
			if _, err := os.Stat(path); err == nil {
				paths[id] = path
				continue
			}
		}
		needed[id] = true
	}
	if len(needed) > 0 {
		for id, path := range codexRolloutPaths(h.Home, needed) {
			paths[id] = path
		}
	}
	live := make(map[string]codexLiveIdentity, len(paths))
	for id, path := range paths {
		meta, ok := process.ReadRolloutHead(path)
		if !ok || meta.Thread != id || meta.Conversation != id || meta.CWD == "" {
			continue
		}
		live[id] = codexLiveIdentity{ID: id, Name: h.codex[id], CWD: meta.CWD}
	}
	h.codexLive = live
	h.codexRollouts = paths
}

// codexRolloutPaths walks Codex's tree once for every set of newly-live ids,
// stopping as soon as each requested suffix has been found. Existing live
// paths are reused by Host, while ended ids are dropped from that cache.
func codexRolloutPaths(home string, needed map[string]bool) map[string]string {
	out := map[string]string{}
	root := filepath.Join(home, ".codex", "sessions")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if len(name) < 42 || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if len(id) < 36 {
			return nil
		}
		id = id[len(id)-36:]
		if !needed[id] {
			return nil
		}
		out[id] = path
		if len(out) == len(needed) {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

// codexLiveFor uses the terminal title only as the final join key, never as a
// name in its own right. A duplicate exact match binds nothing.
func (h *Host) codexLiveFor(s session.Session) (codexLiveIdentity, bool) {
	if s.Backend != session.BackendITerm || s.CWD == "" || s.Label == "" {
		return codexLiveIdentity{}, false
	}
	label := codexTerminalTitle(s.Label)
	var found codexLiveIdentity
	matches := 0
	for _, live := range h.codexLive {
		if live.CWD != s.CWD {
			continue
		}
		expected := live.Name + " | " + filepath.Base(live.CWD) + " (codex)"
		if label != expected {
			continue
		}
		found = live
		matches++
	}
	return found, matches == 1
}

// Codex prefixes the title with one braille spinner cell while it works. The
// thread name begins after that one cell and a space; no other prefix is
// accepted or normalised.
func codexTerminalTitle(label string) string {
	label = strings.TrimSpace(label)
	r, size := utf8.DecodeRuneInString(label)
	if r >= 0x2800 && r <= 0x28ff && len(label) > size && label[size] == ' ' {
		return strings.TrimSpace(label[size:])
	}
	return label
}
