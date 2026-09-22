package swiftstore

import (
	"log"
	"os"
	"strings"
	"sync"
)

// The switch that stops this daemon reading the Swift app's state at all
// (docs/cutover.md §7 B1).
//
// Everything this package reads is the Swift app's live state, and after that
// app retires none of it is updated again. This daemon draws the crown, the
// task chips, the waits, the delivery ticks and the titles from its own store
// first (the broker's tables, W1–W6) and treats the Swift store as history
// that fills in what its own store never held. With the switch off, that
// history is not read either: no file under the Swift app's directory or its
// board is opened, and every answer that would have drawn on them says
// `disabled` rather than `unknown` — the store was not read
// because a person said not to, which is known, not a failure (DG-7).
//
// The pictures the Swift app cached for old conversations are not state and
// are not governed here: cutover B3 keeps them readable after retirement,
// because an old message's picture has no second copy.
const EnvLegacyStore = "CLAWDLINE_NEXT_LEGACY_STORE"

// Source is how one of the Swift app's stores contributed to an answer.
type Source string

const (
	// SourceCurrent is a whole reading of the file as it is now.
	SourceCurrent Source = "current"
	// SourceStale is an earlier whole reading, carried because the newest
	// one was half-written.
	SourceStale Source = "stale"
	// SourceUnreadable is a store that is there and could not be read, with
	// no earlier reading to carry. Unknown: nothing may be drawn as settled.
	SourceUnreadable Source = "unreadable"
	// SourceAbsent is no store at all — a machine that never ran the Swift
	// app, or one that never wrote this file. Known, and it holds nothing.
	SourceAbsent Source = "absent"
	// SourceDisabled is a store this daemon was told not to read
	// (CLAWDLINE_NEXT_LEGACY_STORE=off). Known, and it contributes nothing.
	SourceDisabled Source = "disabled"
)

// Settled reports whether a source is known to contribute nothing: this
// daemon's own records are then the whole answer, not a partial one.
func (s Source) Settled() bool { return s == SourceAbsent || s == SourceDisabled }

var (
	warnLegacyValue sync.Once
	announceOff     sync.Once
)

// Disabled reports whether the Swift app's stores are switched off.
//
// `off` is the one word that turns them off; unset or `on` leaves them on.
// Anything else is said once in the log and leaves them on, because the
// default is the transition's and a misspelt switch must not look like one
// that worked — every answer still says `current` rather than `disabled`.
func Disabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvLegacyStore)))
	switch v {
	case "off":
		announceOff.Do(func() {
			log.Printf("legacy store: %s=off; the Swift app's store and board are not opened, "+
				"and every answer that would have read them says disabled", EnvLegacyStore)
		})
		return true
	case "", "on":
		return false
	}
	warnLegacyValue.Do(func() {
		log.Printf("legacy store: %s=%q is not on or off; the Swift app's stores are still read", EnvLegacyStore, v)
	})
	return false
}
