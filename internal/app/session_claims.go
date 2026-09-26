package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Session claiming a Board item on its person's word: the assignment
// counterpart of CreateFromSession. The person's message, proven by its run,
// is what lets a machine-authenticated caller take an item at all; the item
// goes to the Session that message was said to and to no other, and reads
// afterwards exactly as if the person had assigned it to that Session from
// the Board, plus the message it was claimed on.

// runClaimLimit is how many items one person's message may have claimed. It
// mirrors runItemLimit: a message that names more is a plan, and a Session
// looping on one run is stopped here.
const runClaimLimit = 5

// ClaimWorkV2 is one claim a Session makes on a person's message.
type ClaimWorkV2 struct {
	// Run is the person's message, already found and checked as a relay's
	// run (Runs.Relay).
	Run             work.Run
	ExpectedVersion int64
	// SessionID, TerminalID and Assistant are the live, non-child Session the
	// run was said to.
	SessionID  string
	TerminalID string
	Assistant  string
}

// ClaimFromSession assigns the item to the claiming Session through Assign,
// in one transaction with the checks that make a claim different from a
// person's assignment: the run was said to this Session, the item has no
// owner and no assignment in flight, and the run's claim budget is not spent.
// The caller has already checked that the Session works in the item's
// Project.
func (w *WorkSystemV2) ClaimFromSession(ctx context.Context, id string, c ClaimWorkV2, file WorkV2Filer) (WorkV2View, error) {
	if err := work.RelayTo(c.Run, c.SessionID, time.Time{}); err != nil {
		return WorkV2View{}, relayRefusal(err)
	}
	return w.Assign(ctx, id, AssignWorkV2{ExpectedVersion: c.ExpectedVersion, Mode: "existing_session",
		SessionID: c.SessionID, TerminalID: c.TerminalID, Assistant: c.Assistant, Actor: c.Run.Actor(),
		Claim: &work.CreatedViaV2{Run: c.Run.ID, Session: c.SessionID, At: c.Run.At.Unix(), Excerpt: c.Run.Excerpt}},
		false, file)
}

// claimable refuses a claim Assign would otherwise turn into a reassignment:
// a person may move an item from one Session to another, a Session may only
// take one nobody holds.
func claimable(tx *store.WorkV2Tx, prev work.ItemV2, actor string) error {
	if prev.OwnerSession != "" || prev.Phase == work.PhaseAssigning {
		return workV2Error(http.StatusConflict, "item_assigned",
			"That item already has a Session, or one is being opened for it; nothing was claimed. "+
				"Only the person moves an item between Sessions.")
	}
	if old, err := tx.ActiveAssignment(prev.ID); err != nil {
		return err
	} else if old.ID != "" {
		return workV2Error(http.StatusConflict, "item_assigned",
			"That item already has a Session; nothing was claimed. Only the person moves an item between Sessions.")
	}
	if claimed, err := tx.AssignmentsClaimedBy(actor); err != nil {
		return err
	} else if claimed >= runClaimLimit {
		return workV2Error(http.StatusTooManyRequests, "run_claims_exhausted",
			fmt.Sprintf("That message has already had %d items claimed, as many as one message may; nothing was claimed. "+
				"Ask the person to send another message if they want more.", runClaimLimit))
	}
	return nil
}
