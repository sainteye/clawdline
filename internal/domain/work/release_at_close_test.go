package work

import (
	"testing"
	"time"
)

// Only the release ReleaseAtClose makes keeps an item's decision across an
// owner change; every other change LeaveDecision already handled is as before.
func TestOnlyAReleaseAtCloseKeepsTheDecisionAcrossAnOwnerChange(t *testing.T) {
	now := time.Unix(1_000, 0)
	asked := ItemV2{ID: "i", Phase: PhaseDeploying, Condition: ConditionWaitingUser, DecisionID: "d1",
		OwnerSession: "s1"}

	next := ReleaseAtClose(asked, now)
	if left := LeaveDecision(asked, &next); left != "" || next.DecisionID != "d1" ||
		next.Condition != ConditionWaitingUser || next.OwnerSession != "" {
		t.Fatalf("release at close: left=%q next=%+v", left, next)
	}

	for name, c := range map[string]struct {
		prev ItemV2
		edit func(*ItemV2)
	}{
		"unassign": {asked, func(n *ItemV2) { n.OwnerSession, n.Condition = "", ConditionOwnerRequired }},
		"reassign": {asked, func(n *ItemV2) { n.OwnerSession = "s2" }},
		"release not deploying": {func() ItemV2 { i := asked; i.Phase = PhaseImplementing; return i }(),
			func(n *ItemV2) { n.OwnerSession = "" }},
		"release and move on": {asked, func(n *ItemV2) { n.OwnerSession, n.Phase = "", PhaseDone }},
	} {
		next := c.prev
		c.edit(&next)
		if left := LeaveDecision(c.prev, &next); left != "d1" || next.DecisionID != "" || next.Condition == ConditionWaitingUser {
			t.Fatalf("%s: left=%q next=%+v", name, left, next)
		}
	}
}
