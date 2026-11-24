package orchestrator

// Stable codes for why a spawn_failed task never started. A reader shows them
// in its own words; the raw SpawnError stays a diagnostic.
const (
	SpawnFailureCouldNotOpen         = "could_not_open"
	SpawnFailureDialogUnanswered     = "dialog_unanswered"
	SpawnFailureRestarted            = "restarted"
	SpawnFailureBriefingNotSubmitted = "briefing_not_submitted"
)

// SpawnFailureReason names why a spawn_failed task never started, from the
// record's own facts: a dialog left unanswered over the briefing, a restart
// that lost it, a composer that kept it after Enter, and otherwise a child
// that could not be opened or briefed. Empty on every other state.
func SpawnFailureReason(r *Record) string {
	if r == nil || r.State != StateSpawnFailed {
		return ""
	}
	b := r.Briefing
	switch {
	case !r.AwaitingDialogSince.IsZero() || b != nil && !b.CoveredAt.IsZero():
		return SpawnFailureDialogUnanswered
	case r.Unbriefed || b != nil && !b.ResumedAt.IsZero():
		return SpawnFailureRestarted
	case b != nil && !b.HeldSince.IsZero():
		return SpawnFailureBriefingNotSubmitted
	}
	return SpawnFailureCouldNotOpen
}

// BriefingBlock is what last stood in the briefing's way: "dialog",
// "held_in_composer" or "enter_failed", and empty when nothing was seen.
func BriefingBlock(b *Briefing) string {
	switch {
	case b == nil:
		return ""
	case !b.CoveredAt.IsZero():
		return "dialog"
	case b.EnterError != "":
		return "enter_failed"
	case !b.HeldSince.IsZero():
		return "held_in_composer"
	}
	return ""
}
