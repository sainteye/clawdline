package orchestrator

import "testing"

// A pending landing nobody can check, told apart from one that simply has not
// been recorded yet.
//
// This is the distinction the whole fourth freshness word rests on. Nineteen
// rows sat in this ledger saying they were owed; every one of them was read
// perfectly, and not one of them could be checked, because a landing is proved
// by asking git whether the delivery is on the branch the record names and no
// record named one. The ledger answered `current` about that, and the console
// would have drawn it as settled arithmetic.
//
// The control that makes it red (DG-8): a row that names a target and whose
// branch was read is checkable, and must not be swept into the same word.
func TestAPendingLandingNobodyCanCheckSaysSo(t *testing.T) {
	cases := []struct {
		name string
		l    *Landing
		want bool
	}{
		{"no record at all", nil, false},
		{"already landed", &Landing{State: LandingLanded, Target: ""}, false},
		{"abandoned", &Landing{State: LandingAbandoned}, false},
		{"pending, no target named", &Landing{State: LandingPending}, true},
		{"pending, branch could not be read", &Landing{State: LandingPending, Target: "master",
			Settlement: SettlementUnreadable}, true},
		{"pending, target named, branch empty", &Landing{State: LandingPending, Target: "master",
			Settlement: SettlementEmpty}, false},
		{"pending, target named, branch carries a delivery", &Landing{State: LandingPending, Target: "master",
			Settlement: SettlementCarried}, false},
	}
	for _, c := range cases {
		if got := Unverifiable(c.l); got != c.want {
			t.Errorf("%s: Unverifiable = %v, want %v", c.name, got, c.want)
		}
	}
}

// An empty branch is not an unverifiable one, and the difference is the whole
// value of keeping both facts.
//
// "Its branch carried nothing, and it was supposed to reach master" is an
// answer: the work was never committed, and somebody can go and look. "Nobody
// wrote down where it was supposed to go" is not an answer at all. A ledger
// that folded the two together would send a person to look at a checkout for
// a row whose real problem is that the dispatch never said where it was going.
func TestAnEmptyBranchIsNotAnUnansweredQuestion(t *testing.T) {
	empty := &Landing{State: LandingPending, Target: "master", Settlement: SettlementEmpty}
	nameless := &Landing{State: LandingPending, Settlement: SettlementEmpty}
	if Unverifiable(empty) {
		t.Error("a branch read and found empty is an answer, not a missing one")
	}
	if !Unverifiable(nameless) {
		t.Error("a row with no target is unanswerable however well its branch was read")
	}
}
