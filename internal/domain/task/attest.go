package task

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// Version names the rules that produced a closeability reading.
//
// A reading with no version is a reading nobody can state the rules for, and a
// reader is right to treat it as unproven. It changes when the rules change,
// which is what lets an old reading be recognised as old rather than merely
// looking the same.
const Version = "clgo1"

// MaxAge is how long a closeability reading stays current.
//
// It is short because the thing it describes moves: an obligation can open
// between two reads, and a `safe` that outlived its evidence is the one answer
// this whole projection exists to prevent.
const MaxAge = 45 * time.Second

// Attest returns the identifier of a reading that proves nothing is owed.
//
// It is empty unless the reading actually proves that, because the identifier
// is the proof: a caller hands it back to say which reading it acted on, and
// one minted for a blocked or unknown state would be a receipt for something
// that did not happen.
//
// The identifier is derived from what was read rather than from a counter, so
// two identical readings have one id and a changed one cannot keep the old id
// by accident.
func Attest(subject string, state CloseState, owed []Obligation, at time.Time) string {
	if state != CloseSafe {
		return ""
	}
	ids := make([]string, 0, len(owed))
	for _, o := range owed {
		ids = append(ids, o.ID)
	}
	sort.Strings(ids)
	h := sha256.Sum256([]byte(Version + "\x00" + subject + "\x00" + strings.Join(ids, ",")))
	return Version + "_" + hex.EncodeToString(h[:])[:16]
}

// Freshness says whether a reading may still be acted on.
func Freshness(observed, now time.Time) string {
	if now.Sub(observed) <= MaxAge {
		return "current"
	}
	return "stale"
}
