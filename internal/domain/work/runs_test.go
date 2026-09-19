package work

import (
	"testing"
	"time"
)

// A relay rests on a run this daemon issued, inside the window, said to the
// session the answer belongs to; each refusal has its control beside it.
func TestARelayRestsOnARunThatWasIssued(t *testing.T) {
	at := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	run := Run{ID: "5e1f0000-0000-4000-8000-000000000001", Session: "root-conv", Terminal: "%3", At: at}
	if err := CheckRun(run, false, at); !refusedAs(err, "run_unknown") {
		t.Fatalf("a run never issued: %v", err)
	}
	if err := CheckRun(run, true, at.Add(RelayWindow)); err != nil {
		t.Fatalf("control, at the edge of the window: %v", err)
	}
	if err := CheckRun(run, true, at.Add(RelayWindow+time.Second)); !refusedAs(err, "run_expired") {
		t.Fatalf("past the window: %v", err)
	}
	if err := RelayTo(run, "root-conv", at); err != nil {
		t.Fatalf("control, the same session, the same second as the question: %v", err)
	}
	if err := RelayTo(run, "another-conv", at); !refusedAs(err, "run_other_session") {
		t.Fatalf("another session's question: %v", err)
	}
	if err := RelayTo(run, "root-conv", at.Add(time.Second)); !refusedAs(err, "run_before_question") {
		t.Fatalf("a message from before the question: %v", err)
	}
	unknown := run
	unknown.Session = ""
	if err := RelayTo(unknown, "", at); !refusedAs(err, "run_other_session") {
		t.Fatalf("a run to a conversation nobody knew answers nobody's question: %v", err)
	}
	if run.Actor() != "user_via_session:"+run.ID || run.Evidence()["session"] != "root-conv" {
		t.Fatalf("actor %q evidence %v", run.Actor(), run.Evidence())
	}
	for _, id := range []string{"run-7", "", "5E1F0000-0000-4000-8000-000000000001", "5e1f0000000040008000000000000001xxxx"} {
		if RunShaped(id) {
			t.Fatalf("%q is not a run this daemon issues", id)
		}
	}
	if !RunShaped(run.ID) {
		t.Fatal("control: an issued id")
	}
}

// The rules wait for the root's own chance before proposing a line.
func TestTheRulesWaitForTheRootsOwnProposal(t *testing.T) {
	p := DefaultProposalPolicy()
	first := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	if RuleDue(first, p, first.Add(p.RuleAfter-time.Second)) {
		t.Fatal("proposed inside the root's grace")
	}
	if !RuleDue(first, p, first.Add(p.RuleAfter)) {
		t.Fatal("control: the grace is over")
	}
	if RuleDue(time.Time{}, p, first) {
		t.Fatal("a line with no task has no first dispatch to wait from")
	}
}
