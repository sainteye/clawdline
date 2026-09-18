package orchestrator

import (
	"strings"
	"time"
	"unicode/utf8"
)

// This Mac's house rules, as they go into a briefing (docs/design-decisions.md
// D23 ① and ④).
//
// Two files: a base that says how work is handed out on this machine, and a
// local one that is the person's own additions. The Swift app pastes both into
// every child briefing and cuts the result at 16,000 **characters**, at a
// paragraph break, and when it has to cut it cuts the base — the local file is
// the part written for this one machine, and the part nobody else would think
// to put back (its I1). The first Go version counted bytes and cut from the
// end, which is the local file: measured on 2026-09-18 the two came to 15,376
// bytes, about 600 short of this broker quietly dropping the person's own
// rules first, which is the opposite of the fix the Swift app shipped.

// PolicyLimit is the ceiling, in characters.
const PolicyLimit = 16000

// policyNear is where the ceiling starts being worth saying out loud: at 90%
// nothing is cut yet, and that is the moment a person can still decide what
// to trim themselves.
const policyNear = PolicyLimit * 9 / 10

// PolicyReading is what one composition did, for /v1/diagnostics.
type PolicyReading struct {
	Chars      int
	BaseChars  int
	LocalChars int
	Limit      int
	// Cut is whether the base had to be shortened (or dropped) to fit.
	Cut bool
	// NearLimit is whether the two together, before any cut, passed 90% of
	// the limit.
	NearLimit bool
	At        time.Time
}

// ComposePolicy joins the base and the local house rules for one briefing.
//
// The local file is always kept whole — even when it alone is over the limit,
// because a briefing that is too long is visible and a rule that silently went
// missing is not. The base is cut at its last paragraph break that fits, or at
// a character boundary when it has none.
func ComposePolicy(base, local string) (string, PolicyReading) {
	base = strings.TrimSpace(base)
	local = strings.TrimSpace(local)
	const sep = "\n\n"
	reading := PolicyReading{
		BaseChars:  utf8.RuneCountInString(base),
		LocalChars: utf8.RuneCountInString(local),
		Limit:      PolicyLimit,
	}
	join := func(b string) string {
		switch {
		case b == "":
			return local
		case local == "":
			return b
		}
		return b + sep + local
	}
	whole := join(base)
	total := utf8.RuneCountInString(whole)
	reading.NearLimit = total > policyNear
	if total <= PolicyLimit {
		reading.Chars = total
		return whole, reading
	}

	reading.Cut = true
	budget := PolicyLimit - reading.LocalChars
	if local != "" {
		budget -= utf8.RuneCountInString(sep)
	}
	kept := ""
	if budget > 0 {
		kept = cutAtParagraph(base, budget)
	}
	out := join(kept)
	reading.Chars = utf8.RuneCountInString(out)
	return out, reading
}

// cutAtParagraph is the longest prefix of v of at most limit characters that
// ends at a paragraph break, or the hard prefix when there is no break in it.
func cutAtParagraph(v string, limit int) string {
	if utf8.RuneCountInString(v) <= limit {
		return v
	}
	runes := []rune(v)
	prefix := string(runes[:limit])
	if i := strings.LastIndex(prefix, "\n\n"); i > 0 {
		return strings.TrimSpace(prefix[:i])
	}
	return strings.TrimSpace(prefix)
}

// policy is the house rules for a briefing being written now, read from the
// files at every dispatch so that a person editing them restarts nothing.
func (b *Broker) policy() string {
	if b.Policy == nil {
		return ""
	}
	out, _ := ComposePolicy(b.Policy())
	return out
}

// PolicyRead is what a briefing written now would carry, for /v1/diagnostics:
// composed afresh rather than remembered, because "near the limit" is a
// warning about the next briefing, and the files may have changed since the
// last one. False when this broker has no policy source.
func (b *Broker) PolicyRead() (PolicyReading, bool) {
	if b.Policy == nil {
		return PolicyReading{}, false
	}
	_, reading := ComposePolicy(b.Policy())
	reading.At = b.now()
	return reading, true
}
