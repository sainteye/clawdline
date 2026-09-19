package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// The landing list: GET /v1/orchestrator/landings.
//
// Every pending landing in this broker's records, derived on each read and
// never kept (D01: the landing record is the one fact, and this is a view of
// it). Beside each row is who has to move it and where they were last seen —
// the Swift app's `landingOwnershipRecord` — so a root reading the list can
// tell "the session that owes this is working" from "nobody who could land
// this is running" without opening anything.

// LandingSession is one assistant session in the reading the caller took: the
// identity a landing's executor or root is matched on, and the work state the
// session list projected for it.
type LandingSession struct {
	TerminalID     string
	Assistant      string
	ConversationID string
	WorkState      string
}

// LandingReading is one reading of the machine, taken by the caller once for
// the whole list.
type LandingReading struct {
	Sessions []LandingSession
	// Processes is whether the process table answered completely, and
	// Anonymous whether any assistant in the reading could not be named. Only
	// a complete table with every assistant named proves that a root is not
	// running — the rule the landing's obligation is read by (presence), and
	// for the same reason: this Mac's whole reading is never complete while
	// iTerm2 cannot be asked (D05 ③).
	Processes bool
	Anonymous bool
}

// Ownership statuses, as the Swift app spells them.
const (
	OwnershipObservedWorking = "observed_working"
	OwnershipObservedReady   = "observed_ready_or_holding"
	OwnershipObservedOther   = "observed_other"
	OwnershipTaskStillLive   = "task_still_live"
	OwnershipNotObserved     = "not_observed"
	OwnershipUnknown         = "unknown"
)

// LandingOwnership is who has to move a pending landing, and where the
// reading placed them.
type LandingOwnership struct {
	// Subject is "executor" while the task runs and "root" once it has
	// finished: the one who can act on the landing next.
	Subject string
	Status  string
	Reason  string
	// WorkState is the matched session's work state; "" when nothing was
	// matched exactly once.
	WorkState string
}

// PendingLanding is one row of the list.
type PendingLanding struct {
	Record Record
	// Since is when the landing became owed: when the task finished, or when
	// it was created if it never did (as Owed reads it).
	Since time.Time
	// Paths is the landing write set: what the task declared at dispatch
	// (D21), or its lease when nothing was declared.
	Paths      []string
	RootKey    string
	Obligation Obligation
	Ownership  LandingOwnership
}

// PendingLandings reads every pending landing, oldest first. A record that
// cannot be decoded may be a landing somebody owes, so a ledger holding one
// is refused rather than answered short (DG-7).
func (b *Broker) PendingLandings(ctx context.Context, rd LandingReading) ([]PendingLanding, error) {
	records, unreadable, err := b.Records(ctx)
	if err != nil {
		return nil, err
	}
	if len(unreadable) > 0 {
		return nil, refuseWith(http.StatusServiceUnavailable, "landings_incomplete",
			fmt.Sprintf("%d stored task(s) could not be read, so this list could be missing a landing.", len(unreadable)),
			map[string]any{"unreadable": len(unreadable)})
	}
	out := []PendingLanding{}
	for _, r := range records {
		if r.Landing == nil || r.Landing.State != LandingPending {
			continue
		}
		since := r.FinishedAt
		if since.IsZero() {
			since = r.CreatedAt
		}
		paths, known := r.DeclaredWrites()
		if !known || paths == nil {
			paths = r.Lease()
		}
		if paths == nil {
			paths = []string{}
		}
		out = append(out, PendingLanding{
			Record:     r,
			Since:      since,
			Paths:      paths,
			RootKey:    rootKey(r.Root),
			Obligation: b.Obligation(r),
			Ownership:  LandingOwnershipOf(r, rd),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].Record.ID < out[j].Record.ID
	})
	return out, nil
}

// LandingOwnershipOf places the one who has to move r's landing in a reading.
//
// Every observed status is one exact match: the executor by the terminal the
// broker opened for it, the root by its assistant and conversation — never by
// a label, a directory or a tty. Two matches are ambiguous and read as
// unknown. No match is absence only when the reading could prove absence;
// otherwise it is unknown too, and unknown never becomes "gone".
//
// One difference from the Swift app, on purpose: it answered unknown for every
// row whenever the whole reading was incomplete. That reading is never
// complete on this Mac (D05 ③), so the column would never say anything; an
// exact match is positive evidence from whatever part was read, and only
// absence waits for the source that can prove it.
func LandingOwnershipOf(r Record, rd LandingReading) LandingOwnership {
	if !r.State.Terminal() {
		out := LandingOwnership{Subject: "executor"}
		var matches []LandingSession
		if r.ChildTerminalID != "" {
			for _, s := range rd.Sessions {
				if s.TerminalID == r.ChildTerminalID && (r.Assistant == "" || s.Assistant == r.Assistant) {
					matches = append(matches, s)
				}
			}
		}
		switch len(matches) {
		case 1:
			out.Status, out.Reason, out.WorkState = observedStatus(matches[0].WorkState), "exact_executor_observation", matches[0].WorkState
		case 0:
			out.Status, out.Reason = OwnershipTaskStillLive, "live_task_without_exact_executor_observation"
		default:
			out.Status, out.Reason = OwnershipUnknown, "executor_observation_ambiguous"
		}
		return out
	}
	out := LandingOwnership{Subject: "root"}
	if r.Root == nil || r.Root.SessionID == "" || r.Root.Assistant == "" {
		out.Status, out.Reason = OwnershipUnknown, "root_identity_missing"
		return out
	}
	var matches []LandingSession
	for _, s := range rd.Sessions {
		if s.ConversationID == r.Root.SessionID && s.Assistant == r.Root.Assistant {
			matches = append(matches, s)
		}
	}
	switch {
	case len(matches) == 1:
		out.Status, out.Reason, out.WorkState = observedStatus(matches[0].WorkState), "exact_root_observation", matches[0].WorkState
	case len(matches) > 1:
		out.Status, out.Reason = OwnershipUnknown, "root_observation_ambiguous"
	case rd.Processes && !rd.Anonymous:
		out.Status, out.Reason = OwnershipNotObserved, "root_absent_from_complete_inventory"
	default:
		out.Status, out.Reason = OwnershipUnknown, "session_inventory_incomplete"
	}
	return out
}

// observedStatus is the Swift app's `observedLandingStatus`.
func observedStatus(workState string) string {
	switch workState {
	case "working":
		return OwnershipObservedWorking
	case "ready", "holding":
		return OwnershipObservedReady
	}
	return OwnershipObservedOther
}
