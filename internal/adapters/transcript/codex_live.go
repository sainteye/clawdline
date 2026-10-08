package transcript

import (
	"context"
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
// longer held. An empty Name is a thread Codex has not named yet, which joins
// only the bare directory title (codexLiveFor).
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
// only when the lock is held now and its rollout head says it is the
// conversation root. Its name comes from session_index.jsonl or, for the
// threads Codex 0.160 names only there, its state database; a thread with
// neither is kept unnamed rather than dropped. The terminal title and cwd are
// checked later, once terminal and process inventories have been merged.
//
// It takes the session index just read and the previous refresh's rollout
// paths, and returns new maps for Host.Refresh to publish: names, live
// threads and rollout paths. Neither argument is written, because the
// previous paths are still what a concurrent reading may be holding.
func refreshCodexLive(home string, names map[string]string, rollouts map[string]string) (map[string]string, map[string]codexLiveIdentity, map[string]string) {
	ids, complete := heldCodexThreadIDs(filepath.Join(home, ".codex", "thread-writer-locks"))
	if !complete {
		return names, nil, rollouts
	}
	names = nameFromCodexState(home, names, ids)
	needed := make(map[string]bool, len(ids))
	paths := make(map[string]string, len(ids))
	for _, id := range ids {
		if path := rollouts[id]; path != "" {
			if _, err := os.Stat(path); err == nil {
				paths[id] = path
				continue
			}
		}
		needed[id] = true
	}
	if len(needed) > 0 {
		for id, path := range codexRolloutPaths(home, needed) {
			paths[id] = path
		}
	}
	live := make(map[string]codexLiveIdentity, len(paths))
	for id, path := range paths {
		meta, ok := process.ReadRolloutHead(path)
		if !ok || meta.Thread != id || meta.Conversation != id || meta.CWD == "" {
			continue
		}
		live[id] = codexLiveIdentity{ID: id, Name: names[id], CWD: meta.CWD}
	}
	return names, live, paths
}

// nameFromCodexState answers the session index's names with the live ids it
// does not name filled from Codex's state database. The index wins where both
// have a name: it is what this path has always trusted, so a thread named in
// both keeps exactly its old title contract, and the database is asked only
// about the few live gaps rather than its whole history on every refresh.
//
// The answer is a new map whenever anything is added, never the index
// written in place, so no map a reading may hold is ever written.
func nameFromCodexState(home string, index map[string]string, ids []string) map[string]string {
	var unnamed []string
	for _, id := range ids {
		if index[id] == "" {
			unnamed = append(unnamed, id)
		}
	}
	found := codexStateNames(home, unnamed)
	if len(found) == 0 {
		return index
	}
	names := make(map[string]string, len(index)+len(found))
	for id, name := range index {
		names[id] = name
	}
	for id, name := range found {
		names[id] = name
	}
	return names
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

// codexBareRowsKey carries one reading's bare-title count (ObserveRows).
type codexBareRowsKey struct{}

// ObserveRows counts, once per inventory reading, the iTerm Codex rows whose
// terminal title is the bare `<base(cwd)> (codex)`, per cwd, and answers a
// context carrying that count for the reading's identity questions.
//
// codexLiveFor sees one row at a time, but the bare title is not unique to a
// thread: a second Codex tab opened in the same directory shows it too before
// its own thread holds a writer lock, and both rows would otherwise take the
// one unnamed live root. Inventory.Read already holds every merged row before
// it asks for identities, so it hands them over here rather than this host
// reading terminals of its own.
//
// The count rides in the reading's context rather than on Host because one
// Host serves readings that overlap: kept on Host, another reading's Refresh
// cleared it between this reading's ObserveRows and its ForSession, and a
// tab alone in its directory went unbound in 8 of 12 concurrent reads on
// 2026-10-05. A context with no count binds no unnamed thread instead of
// guessing.
func (h *Host) ObserveRows(ctx context.Context, rows []session.Session) context.Context {
	bare := map[string]int{}
	for _, s := range rows {
		if s.Backend != session.BackendITerm || s.Assistant != session.AssistantCodex || s.CWD == "" {
			continue
		}
		if codexObservedTitle(s.Label) == codexBareTitle(s.CWD) {
			bare[s.CWD]++
		}
	}
	return context.WithValue(ctx, codexBareRowsKey{}, bare)
}

func codexBareTitle(cwd string) string { return filepath.Base(cwd) + " (codex)" }

// codexLiveFor uses the terminal title only as the final join key, never as a
// name in its own right. A duplicate exact match binds nothing.
//
// A named thread's title is `<name> | <base(cwd)> (codex)`; an unnamed one's
// is the bare `<base(cwd)> (codex)`. Each identity is compared only with the
// form its own name implies, so neither kind can take the other's terminal.
// Every unnamed live root in one cwd expects the same bare title, so two of
// them are the duplicate match that binds nothing: cwd alone never chooses.
// The same holds from the terminal side: the bare title binds only while
// exactly one row in this reading's cwd shows it (ObserveRows, carried in
// ctx).
func codexLiveFor(ctx context.Context, lives map[string]codexLiveIdentity, s session.Session) (codexLiveIdentity, bool) {
	if s.Backend != session.BackendITerm || s.CWD == "" || s.Label == "" {
		return codexLiveIdentity{}, false
	}
	label := codexTerminalTitle(s.Label)
	found, matches := codexLiveMatches(ctx, lives, s, label)
	if matches != 0 {
		return found, matches == 1
	}
	// A Codex tab waiting for the person alternates these two observed status
	// markers in its title. Try its unadorned title only when the exact one
	// matched nothing: a conversation literally named with the marker keeps
	// its exact identity, and an ambiguous match is never ranked away.
	if plain, marked := codexActionRequiredTitle(label); marked {
		found, matches = codexLiveMatches(ctx, lives, s, plain)
		return found, matches == 1
	}
	return codexLiveIdentity{}, false
}

var codexActionRequiredPrefixes = [...]string{
	"[ ! ] Action Required | ",
	"[ . ] Action Required | ",
}

func codexActionRequiredTitle(label string) (string, bool) {
	for _, prefix := range codexActionRequiredPrefixes {
		if plain, ok := strings.CutPrefix(label, prefix); ok {
			return plain, true
		}
	}
	return label, false
}

func codexObservedTitle(label string) string {
	plain, _ := codexActionRequiredTitle(codexTerminalTitle(label))
	return plain
}

func codexLiveMatches(ctx context.Context, lives map[string]codexLiveIdentity, s session.Session, label string) (codexLiveIdentity, int) {
	if label == codexBareTitle(s.CWD) {
		bare, _ := ctx.Value(codexBareRowsKey{}).(map[string]int)
		if bare[s.CWD] != 1 {
			return codexLiveIdentity{}, 2
		}
	}
	var found codexLiveIdentity
	matches := 0
	for _, live := range lives {
		if live.CWD != s.CWD {
			continue
		}
		expected := codexBareTitle(live.CWD)
		if live.Name != "" {
			expected = live.Name + " | " + expected
		}
		if label != expected {
			continue
		}
		found = live
		matches++
	}
	return found, matches
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
