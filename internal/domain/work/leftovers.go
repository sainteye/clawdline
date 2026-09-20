package work

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a child did not do, in a shape a machine can read — and the one moment
// in this loop that asks whether it is worth registering.
//
// The measurement this exists for: on this machine every one of the fourteen
// Backlog rows was written in one of two moments, and both of them were "the
// person has just complained". What a root learned from a child's report — the
// four daemon defects one child handed back, the feature that turned out to be
// half done — reached the Backlog only when the root happened to be writing a
// summary. Nothing in the loop ever asked "you have just learned something;
// does it belong on the Backlog?".
//
// It is mechanical, because every report in this repository already ends in the
// same two paragraphs: what the task did not do, and what its root has to pick
// up. Those lines are the shape of a Backlog row. So this is those paragraphs
// written where a machine can read them, and nothing more:
//
//   - a child writes them in `result.json` as `leftovers`, which it was going
//     to write in its report anyway. Three fields, all of them prose it
//     already has;
//   - nothing is required. A result with no leftovers is a complete delivery,
//     exactly as before (§4's TD-1 is still the only thing a dispatch owes);
//   - what a root does with them is one ordinary proposal each (PT-9). A
//     person answers it track, later or no, and only that answer makes a row.
//     No session puts anything on a person's board or Backlog by itself
//     (CM-2), which is why this is a candidate and never a creation.
type Leftover struct {
	// Title is the row this would become, in the child's own words.
	Title string `json:"title"`
	// Why is why the child did not do it. It is the sentence a person needs
	// to answer with, so it travels into the proposal's question.
	Why string `json:"why,omitempty"`
	// Acceptance is what the child thinks would count as done. It is a
	// suggestion and is never treated as a commitment by anything here.
	Acceptance string `json:"suggested_acceptance,omitempty"`
}

// The bounds of one result's account of what it did not do. They are
// admission bounds, not a store that accumulates: a result is refused whole
// past them (taskdir.ValidateResult) and nothing is kept in part. Registered
// on the capacity guard's baseline.
//
// Eight rows because the list is the end of one report, not a backlog of its
// own: a child with more than eight things it did not do has a boundary
// problem that another list will not fix. The title's bound is a work item's
// own (app.workTitleLimit), because that is what the row becomes.
const (
	LeftoversLimit          = 8
	LeftoverTitleLimit      = 200
	LeftoverWhyLimit        = 500
	LeftoverAcceptanceLimit = 500
)

// SignalLeftover is why a leftover's proposal is worth a person's attention: a
// child reported, in its own delivery, that it did not do this.
//
// It is deliberately not one of ProposalSignals. I1–I3 are read from the
// broker's facts about a line of work that exists; a leftover has no line and
// no facts yet, and what stands behind it is a child's own account of itself.
// Keeping it out of that list is what stops it being counted as evidence about
// a line of work (SignalsOf never produces it).
const SignalLeftover Signal = "leftover"

// Leftover reports whether a proposal's subject is a leftover rather than a
// line of work. It is read from the signals the proposal was recorded with, so
// a proposal read back from the store answers it the same way.
//
// It matters past bookkeeping: a leftover proposal carries the task that
// raised it, and that task must never be bound onto the leftover's new line
// (app.Participation.Answer). The task is provenance, not a dispatch of this
// work.
func (p Proposal) Leftover() bool {
	for _, s := range p.Signals {
		if s == SignalLeftover {
			return true
		}
	}
	return false
}

// ParseLeftovers reads a result's leftovers: trimmed, bounded, and refused by
// name when they are not usable.
//
// An empty list and no list are the same answer — nothing was left over —
// because a child that wrote neither has not failed to deliver. Everything
// else is refused rather than trimmed away: a leftover silently dropped is the
// exact failure this whole path exists to stop.
func ParseLeftovers(in []Leftover) ([]Leftover, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > LeftoversLimit {
		return nil, refuse(400, "too_many_leftovers",
			"A result names at most %d leftovers; this one names %d.", LeftoversLimit, len(in))
	}
	out := make([]Leftover, 0, len(in))
	seen := map[string]bool{}
	for _, lo := range in {
		lo.Title, lo.Why, lo.Acceptance = oneLine(lo.Title), oneLine(lo.Why), oneLine(lo.Acceptance)
		switch {
		case lo.Title == "" || utf8.RuneCountInString(lo.Title) > LeftoverTitleLimit:
			return nil, refuse(400, "invalid_leftover",
				"Each leftover has a title of 1 to %d characters.", LeftoverTitleLimit)
		case utf8.RuneCountInString(lo.Why) > LeftoverWhyLimit:
			return nil, refuse(400, "invalid_leftover",
				"A leftover's why is at most %d characters.", LeftoverWhyLimit)
		case utf8.RuneCountInString(lo.Acceptance) > LeftoverAcceptanceLimit:
			return nil, refuse(400, "invalid_leftover",
				"A leftover's suggested_acceptance is at most %d characters.", LeftoverAcceptanceLimit)
		case seen[lo.Title]:
			// A leftover is named by its title when it is proposed, so two of
			// one title in one result are two things nobody can tell apart.
			return nil, refuse(400, "invalid_leftover",
				"Two leftovers of one result have the same title (%q); a title names one of them.", lo.Title)
		}
		seen[lo.Title] = true
		out = append(out, lo)
	}
	return out, nil
}

// FindLeftover is the leftover a proposal names, by its exact title.
func FindLeftover(list []Leftover, title string) (Leftover, bool) {
	title = oneLine(title)
	for _, lo := range list {
		if oneLine(lo.Title) == title {
			return lo, true
		}
	}
	return Leftover{}, false
}

// oneLine is a leftover's field as it is kept: trimmed, and with every control
// character — a newline above all — turned into a space. These strings travel
// inside the completion notice, which must encode to one physical line or the
// far side reads two messages (orchestrator notice.go).
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// LeftoverQuestion is the one sentence a leftover is put to a person with. It
// is the server's, as every proposal's question is (Question): what is asked
// is what was recorded, in the person's language.
//
// It carries the why, because a person answering "should this be registered"
// is answering about the reason, not about a title they did not write.
func LeftoverQuestion(lo Leftover, task string) string {
	q := "child 交回的任務" + shortTask(task) + "說它沒做「" + lo.Title + "」"
	if lo.Why != "" {
		q += "（" + lo.Why + "）"
	}
	return q + "。要不要登記？回覆：追蹤／之後（Backlog）／不用"
}

// shortTask is a task id as a sentence names it: the first eight characters,
// which is how every line this daemon writes for a person names one.
func shortTask(id string) string {
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return " " + id + " "
}

// PriorLeftovers narrows a task's earlier proposals to the ones about this one
// leftover of it.
//
// It is the whole reason a leftover's duplicate rules work at all. A delivery
// that named three leftovers becomes three proposals carrying that one task
// id, and PriorProposals reads them all as one line of work's history; without
// this, the second of the three would be refused as a duplicate of the first,
// and two of a child's three answers would be lost to a rule meant to stop
// asking twice about one thing.
func PriorLeftovers(all []Proposal, title string) []Proposal {
	title = oneLine(title)
	out := []Proposal{}
	for _, p := range all {
		if p.Leftover() && oneLine(p.Title) == title {
			out = append(out, p)
		}
	}
	return out
}
