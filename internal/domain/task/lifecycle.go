package task

// Delivery is the rung a finished task's work has reached.
//
// It is deliberately a second machine rather than more states on the first.
// A terminal child state cannot be the completion state of a code-producing
// graph: the delivery is durable and still pending. Delivered is not reviewed,
// and reviewed is not landed.
type Delivery string

const (
	DeliveryDelivered Delivery = "delivered"
	DeliveryReviewed  Delivery = "reviewed"
	DeliveryPending   Delivery = "pending_landing"
	DeliveryLanded    Delivery = "landed"
)

// ReviewVerdict is what an independent reader concluded.
//
// Three values, not two. The Swift app's graph records a review as `done` or
// `failed`, so `changes_required` — the ordinary outcome of a review that did
// its job — is indistinguishable from a review that died, and it blocks the
// correction node that is supposed to follow it. Of eighteen typed verdicts
// measured on this machine, none was `safe_to_land`, which means nearly every
// feature hit that wall.
type ReviewVerdict string

const (
	VerdictSafeToLand      ReviewVerdict = "safe_to_land"
	VerdictChangesRequired ReviewVerdict = "changes_required"
	VerdictAbandoned       ReviewVerdict = "abandoned"
)

// Advances reports whether work may continue past a review with this verdict.
// A review that asked for changes has finished its job, and the node that
// carries out those changes is the next one, not a blocked one.
func (v ReviewVerdict) Advances() bool {
	return v == VerdictSafeToLand || v == VerdictChangesRequired
}
