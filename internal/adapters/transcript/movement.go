package transcript

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// When a session last moved, out of the record the assistant keeps about it.
//
// `session.Activity` says why this record and no other: both an assistant turn
// and a person's message are appended to it, so one reading follows both.
//
// **It is the newest record's own timestamp, not the file's.** The file's
// modification time was the first thing tried, because it costs a stat and
// nothing else. Measured against this machine on 2026-09-20 it is wrong in the
// way that matters: three transcripts in three unrelated projects all carried
// `15:25:30` that afternoon while the newest turn in each of them was from the
// previous day. Something touches those files without appending a turn — and a
// list ordered by that would put three sessions nobody had spoken to since
// yesterday above one somebody replied to an hour ago, which is exactly the
// wrong answer given confidently. So the answer is the `timestamp` the
// assistant itself wrote on the last record, which both Claude Code and Codex
// put on every line.
//
// **The file is opened only when it has changed.** The answer is kept against
// the file's size and modification time, as the title cache is, so a list
// redrawn every two seconds costs one stat per row and a read only of the rows
// that moved — never the transcript sweep this was asked not to become.
//
// **The path is the other thing that costs something.** Claude Code's is
// computed from the working directory and the conversation id. Codex files its
// rollout under the date it started, with the id only in the file's name, so
// finding it means walking `~/.codex/sessions` — which is why that answer is
// kept too and a thread that has one is never walked for twice.

// activityTail is how much of a record's end the last turn is read from. It is
// `titleTail`'s size and for the same reason: one line carries a whole tool
// result, so a tail measured in kilobytes can land inside a single record. It
// is not a capacity row for the same reason `titleTail` is not — it bounds one
// read, not something this daemon holds.
const activityTail = 512_000

// rolloutRetry is how long a walk that found nothing stands before another is
// made. A Codex thread writes its rollout at the first message, not at
// startup, so "not there yet" is a real and temporary answer and re-walking
// the tree for it on every two-second reading would be the scan this is meant
// not to be.
const rolloutRetry = 30 * time.Second

// Movements is what this daemon remembers about when each session last moved:
// where its record is, and what the newest turn in it said the time was when
// the file was last this size.
//
// It holds the register's `cache.session_activity` rows at most, letting go of
// the one read longest ago; everything in it is found again on a miss.
type Movements struct {
	mu   sync.Mutex
	seen *lru[movementReading]
	now  func() time.Time
}

type movementReading struct {
	// path is where the record is, or empty when no walk found one. Only
	// Codex needs it; Claude Code's path is computed.
	path string
	// pathAt is when that walk was made, so a negative answer can be retried
	// without being retried every time.
	pathAt time.Time
	// size and mod are the file as it was when at was read.
	size int64
	mod  time.Time
	// at is the newest record's own timestamp, and read whether that was
	// found.
	at   time.Time
	read bool
}

// NewMovements is an empty cache at the register row's default limit.
func NewMovements() *Movements {
	return &Movements{seen: newLRU[movementReading](capacity.CacheSessionActivity), now: time.Now}
}

// SetLimit is the capacity override for `cache.session_activity`.
func (m *Movements) SetLimit(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen.setLimit(n)
}

// Reading is the `cache.session_activity` row.
func (m *Movements) Reading() capacity.Reading {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen.reading()
}

func (m *Movements) get(id string) (movementReading, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen.get(id)
}

func (m *Movements) put(id string, r movementReading) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen.put(id, r)
}

// rollout is where this Codex thread's record is, and false when no walk found
// one recently enough to be worth believing.
func (m *Movements) rollout(home, id string) (string, bool) {
	prev, had := m.get(id)
	if had && prev.path != "" {
		// A rollout does not move once it exists, but it can be removed; a
		// path that has gone falls through to another walk.
		if _, err := os.Stat(prev.path); err == nil {
			return prev.path, true
		}
	} else if had && prev.path == "" && m.now().Sub(prev.pathAt) < rolloutRetry {
		return "", false
	}
	found := CodexPath(home, id)
	prev.path, prev.pathAt = found, m.now()
	m.put(id, prev)
	return found, found != ""
}

// LastActivity answers when this session's own record last grew.
//
// It is the optional port `internal/app.Inventory` looks for on its identity
// source. Every kind of nothing is a named answer, never a very old time:
// ordering a row by a time nobody could read would hide it at the bottom of
// the list, which is the one thing this must not do.
func (h *Host) LastActivity(ctx context.Context, s session.Session) session.Activity {
	path, why := h.recordPath(s)
	if path == "" {
		return why
	}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return session.Activity{
				Reason: session.ActivityNoRecord,
				Detail: "the session has written no conversation record yet",
			}
		}
		return session.Activity{
			Reason: session.ActivityUnreadable,
			Detail: "this machine could not read the session's conversation record",
		}
	}

	key := s.ConversationID
	prev, had := h.movements.get(key)
	if had && prev.read && prev.size == st.Size() && prev.mod.Equal(st.ModTime()) {
		return session.Activity{At: prev.at, Evidence: session.EvidenceTranscript}
	}

	at, ok := lastTurnAt(path)
	prev.size, prev.mod, prev.at, prev.read = st.Size(), st.ModTime(), at, ok
	h.movements.put(key, prev)
	if !ok {
		// The record is there and was read, and no turn in the part of it
		// this reading looks at carries a time it understands. That is this
		// machine failing to read — never the session being quiet — so it is
		// said as one rather than falling back to the file's own clock, which
		// is the reading this whole file exists to stop trusting.
		return session.Activity{
			Reason: session.ActivityUnreadable,
			Detail: "no turn this reading could read carried a time of its own",
		}
	}
	return session.Activity{At: at, Evidence: session.EvidenceTranscript}
}

// timestampKey is how both assistants write a record's own time.
var timestampKey = []byte(`"timestamp":"`)

// lastTurnAt is the newest timestamp in the end of a record.
//
// It is read backwards from the end rather than by parsing lines, because one
// line can be a whole tool result and the last one may be half written as this
// reads. An occurrence that does not parse is stepped over: a truncated value
// is not an answer, and the one before it is.
func lastTurnAt(path string) (time.Time, bool) {
	data, _, err := tailData(path, activityTail)
	if err != nil {
		return time.Time{}, false
	}
	for end := len(data); end > 0; {
		at := bytes.LastIndex(data[:end], timestampKey)
		if at < 0 {
			return time.Time{}, false
		}
		end = at
		value := data[at+len(timestampKey):]
		if close := bytes.IndexByte(value, '"'); close >= 0 {
			value = value[:close]
		}
		// The fraction is required, as the Swift app's formatter requires it
		// (turns.go `object.time`), so a shape this does not understand is
		// refused rather than half read.
		if len(value) < 20 || value[19] != '.' {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, string(value))
		if err != nil {
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// recordPath is the file whose newest turn answers, or the kind of nothing
// that stands in its place.
func (h *Host) recordPath(s session.Session) (string, session.Activity) {
	switch s.Assistant {
	case session.AssistantClaude:
		// The registry's working directory outranks the process table's: a
		// session files its transcript under the directory it was launched
		// in, which is the one the registry keeps.
		cwd := s.CWD
		if r, ok := h.claude[s.PID]; ok && r.CWD != "" {
			cwd = r.CWD
		}
		if s.ConversationID == "" || cwd == "" {
			return "", session.Activity{
				Reason: session.ActivityNoRecord,
				Detail: "this session has no conversation to read a record of yet",
			}
		}
		return ClaudePath(h.Home, cwd, s.ConversationID), session.Activity{}

	case session.AssistantCodex:
		if s.ConversationID == "" {
			return "", session.Activity{
				Reason: session.ActivityNoRecord,
				Detail: "this session has no conversation to read a record of yet",
			}
		}
		if path, ok := h.movements.rollout(h.Home, s.ConversationID); ok {
			return path, session.Activity{}
		}
		// Telling "the tree could not be read" from "this thread has no
		// rollout yet" is one stat of the directory, and they are different
		// facts: only the first is this machine's own to fix.
		if _, err := os.Stat(filepath.Join(h.Home, ".codex", "sessions")); err != nil {
			return "", session.Activity{
				Reason: session.ActivityUnreadable,
				Detail: "this machine could not read where Codex keeps its records",
			}
		}
		return "", session.Activity{
			Reason: session.ActivityNoRecord,
			Detail: "Codex writes a thread's rollout at its first message, and this one has none yet",
		}
	}
	return "", session.Activity{
		Reason: session.ActivityUnsupported,
		Detail: "nothing here reads an activity time for this assistant",
	}
}
