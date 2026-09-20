package session

import (
	"regexp"
	"strings"
)

// Gap is one region of one source that a reading could not see into, named.
//
// A source that answers only "I could not read everything" has told a reader
// that its list is short and nothing else. It cannot say what is missing, so
// every absence on the machine becomes unprovable at once — including absences
// that source was never asked about.
//
// Measured on this Mac on 2026-09-20: one iTerm2 window (id 27898, titled
// `sleep 300`) answered null to `tabs()`, `currentTab()` and `currentSession()`
// in twelve readings out of twelve, while the other two windows listed their
// eleven sessions in every one. That single window made all thirteen rows of
// `/v1/sessions` read `closeability = unknown` with `session_inventory_stale`,
// ten of them tmux panes iTerm2 has never listed and cannot hide — and a
// session the person had closed stayed on their phone saying it "is not
// absent, it is unseen".
//
// So the incompleteness is named: which source, which region of it, and what
// was asked. A reader can then ask the narrower question — could what I am
// looking for be in the part that was not read? — instead of the one nobody
// can answer.
type Gap struct {
	// Source is the provenance of the reading that met it: "iterm".
	Source string
	// Scope is the kind of region, in that source's own words: "window".
	Scope string
	// ID is the region's own id, where the source can name one. A source that
	// could not even name what it failed to read leaves this empty; it is
	// still a gap, and a gap with no name is never sealed.
	ID string
	// Detail is what was asked and what came back, in one sentence, for a
	// person reading a diagnostic.
	Detail string
	// Sealed is set when another source has accounted for everything this
	// region could be hiding. It is the only thing that makes a region nobody
	// can read stop costing the whole machine its answers.
	//
	// **Time does not seal a gap.** A window unread for two hours is a window
	// unread: "long enough" is the same guess made more slowly, and D05 ③ is
	// not relaxed by waiting. What seals one is a second source, read in the
	// same moment and by another mechanism, that leaves the region nothing to
	// hold.
	Sealed bool
	// SealedBy is that reason with its numbers in it, so a reader can check it
	// rather than take it. Empty on a gap that is still open.
	SealedBy string
}

// Open says whether this gap still costs its source its authority.
func (g Gap) Open() bool { return !g.Sealed }

// SourceFor is the provenance of the source that lists a backend's sessions.
//
// Empty means no source here speaks for that backend, which is an answer: a
// reader then falls back to the whole reading rather than to a source that was
// never asked.
func SourceFor(b Backend) string {
	switch b {
	case BackendITerm:
		return "iterm"
	case BackendTmux:
		return "tmux"
	}
	return ""
}

// tmuxPane is a tmux pane id, which tmux alone issues: `%` and digits.
var tmuxPane = regexp.MustCompile(`^%[0-9]+$`)

// itermSession is an iTerm2 session id, which iTerm2 issues as an upper-case
// UUID and nothing else on this machine does.
var itermSession = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// SourceForID is the source that would have listed an id, from the shape of
// the id alone.
//
// It exists for the one question a reading cannot answer from its own rows:
// an id that is *not* in the list has no row to read a backend off. The shapes
// are each issued by exactly one source, so this is a lookup and not a guess;
// anything else answers empty, and a caller that gets empty must fall back to
// the whole reading rather than pick a source.
func SourceForID(id string) string {
	switch {
	case tmuxPane.MatchString(id):
		return "tmux"
	case itermSession.MatchString(id):
		return "iterm"
	case strings.HasPrefix(id, "tty") || strings.HasPrefix(id, "pts/"):
		// A row only the process table saw is listed under its tty.
		return "ps"
	}
	return ""
}

// ProvesAbsence says whether this reading may be read as "everything that
// source has", and when it may not, what is in the way.
//
// This is D05 ③ in one place. `Complete` is the AND over every source, which
// is the right answer to "is this list all there is" and the wrong one to "is
// this pane gone": on a Mac with one iTerm2 window that will not answer, the
// AND is false in every reading for ever, so a tmux pane that tmux listed
// completely could never be shown to have gone, and no session could ever be
// closed, tombstoned or settled.
//
// A source empty of gaps answers for itself. A source with gaps answers only
// once every one of them is sealed — by another source, in the same reading,
// and never by how long the gap has been there.
func (i Inventory) ProvesAbsence(source string) (bool, string) {
	complete, known := i.Sources[source]
	if source == "" || !known {
		// Either the id is of no shape a source here issues, or no source in
		// this reading lists that kind of session at all — a machine with no
		// tmux running on it, asked about a pane. A reading that read this
		// whole machine has answered both: there is no such session on it.
		// An incomplete one has answered neither, which is where every caller
		// stood before the sources could be told apart.
		if i.Complete {
			return true, ""
		}
		return false, i.Because()
	}
	if complete {
		return true, ""
	}
	if open := i.OpenGaps(source); len(open) > 0 {
		return false, open[0].Detail
	}
	return false, source + " did not finish its reading"
}

// OpenGaps are the unsealed gaps of one source, or of every source when the
// name is empty.
func (i Inventory) OpenGaps(source string) []Gap {
	out := []Gap{}
	for _, g := range i.Gaps {
		if g.Open() && (source == "" || g.Source == source) {
			out = append(out, g)
		}
	}
	return out
}

// Because is this reading's own sentence about why it is not authoritative,
// gaps first, for a refusal a person has to read.
func (i Inventory) Because() string {
	if open := i.OpenGaps(""); len(open) > 0 {
		parts := make([]string, 0, len(open))
		for _, g := range open {
			parts = append(parts, g.Detail)
		}
		return strings.Join(parts, "; ")
	}
	if len(i.Notes) > 0 {
		return strings.Join(i.Notes, "; ")
	}
	return "this reading did not finish"
}
