package work

import (
	"testing"
	"time"
)

// The exit a proposal was missing, as a pure rule: what ends a question, and
// beside each the smallest change of facts that leaves it standing (DG-8).

var withdrawNow = time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)

func stillOwed() Todo { return Todo{State: TodoStateOpen, CreatedAt: withdrawNow.Add(-time.Hour)} }
func wasClosed() Todo {
	return Todo{State: TodoStateDone, Reason: ReasonLanded, CreatedAt: withdrawNow.Add(-time.Hour)}
}

func TestWhatEndsAQuestionAndWhatDoesNot(t *testing.T) {
	rule := Proposal{State: ProposalPending, Source: SourceRule, Signals: []Signal{SignalCrossSession}}
	mine := Proposal{State: ProposalPending, Source: SourceSession, Signals: []Signal{SignalCrossSession}}
	cases := []struct {
		name  string
		p     Proposal
		facts SubjectFacts
		want  string
	}{
		{"a line still owed is still a question", mine, SubjectFacts{Todos: []Todo{stillOwed()}}, ""},
		{"every to-do of it is over", mine, SubjectFacts{Todos: []Todo{wasClosed(), wasClosed()}},
			WithdrawnSubjectSettled},
		{"one of them is still owed", mine, SubjectFacts{Todos: []Todo{wasClosed(), stillOwed()}}, ""},
		{"a work item exists for it", mine, SubjectFacts{HasItem: true, Todos: []Todo{stillOwed()}},
			WithdrawnSubjectTracked},
		{"tracked outranks settled", mine, SubjectFacts{HasItem: true, Todos: []Todo{wasClosed()}},
			WithdrawnSubjectTracked},
		// A leftover's subject is a line nobody has dispatched anything for.
		// "All of them are over" must not be true of none, or the one
		// proposal a person really has to answer would be first to go.
		{"a subject with no to-dos at all", Proposal{State: ProposalPending, Source: SourceSession,
			Signals: []Signal{SignalLeftover}}, SubjectFacts{}, ""},
		{"a rule that would not fire now", rule, SubjectFacts{Todos: []Todo{stillOwed()}}, WithdrawnRuleSpent},
		{"the same, asked by a person's session", mine, SubjectFacts{Todos: []Todo{stillOwed()}}, ""},
		{"a rule that would still fire", Proposal{State: ProposalPending, Source: SourceRule,
			Signals: []Signal{SignalCrossSession, SignalLongLived}}, SubjectFacts{Todos: []Todo{stillOwed()}}, ""},
		{"an answered one is not withdrawn", Proposal{State: ProposalAnswered, Answer: AnswerNo},
			SubjectFacts{Todos: []Todo{wasClosed()}}, ""},
		{"nor an expired one", Proposal{State: ProposalExpired}, SubjectFacts{Todos: []Todo{wasClosed()}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := WithdrawProposal(c.p, c.facts, withdrawNow)
			if (c.want == "") == ok {
				t.Fatalf("withdrawn=%v, wanted %q", ok, c.want)
			}
			if !ok {
				if got.State != c.p.State {
					t.Fatalf("left as %s", got.State)
				}
				return
			}
			if got.State != ProposalWithdrawn || got.WithdrawnReason != c.want ||
				!got.WithdrawnAt.Equal(withdrawNow) {
				t.Fatalf("%s (%s) at %v", got.State, got.WithdrawnReason, got.WithdrawnAt)
			}
		})
	}
}

// The bar the rules hold themselves to: I1 is carried by every dispatch, so a
// rule resting on it alone asks one question per dispatch.
func TestTheRuleBarNeedsMoreThanADispatch(t *testing.T) {
	cases := []struct {
		signals []Signal
		want    bool
	}{
		{nil, false},
		{[]Signal{SignalCrossSession}, false},
		{[]Signal{SignalCrossSession, SignalLongLived}, true},
		{[]Signal{SignalExternalEffect}, true},
	}
	for _, c := range cases {
		if got := RuleWorthy(c.signals); got != c.want {
			t.Fatalf("%v: %v", c.signals, got)
		}
	}
	// The gate says the same thing to a rule and lets a session through on
	// the signal the rule refuses.
	p := DefaultProposalPolicy()
	facts := ProposalFacts{ByRule: true, Signals: []Signal{SignalCrossSession}}
	if _, err := GateProposal(facts, p, withdrawNow); gateCode(err) != RefuseBelowThreshold {
		t.Fatalf("a rule on I1 alone: %v", err)
	}
	facts.ByRule = false
	if _, err := GateProposal(facts, p, withdrawNow); err != nil {
		t.Fatalf("a session on I1 alone: %v", err)
	}
}

// A withdrawn proposal is not an answer and not a decline: a line that comes
// back to life may be proposed again, and the withdrawn row does not count
// toward "proposed and not taken".
func TestAWithdrawnProposalDoesNotStandInTheWay(t *testing.T) {
	p := DefaultProposalPolicy()
	prior := Proposal{ID: "old", State: ProposalWithdrawn, WithdrawnReason: WithdrawnSubjectSettled,
		Signals: []Signal{SignalCrossSession}}
	facts := ProposalFacts{Signals: []Signal{SignalCrossSession}, Prior: []Proposal{prior},
		HeardAt: withdrawNow.Add(-time.Minute)}
	v, err := GateProposal(facts, p, withdrawNow)
	if err != nil || !v.Ask {
		t.Fatalf("after a withdrawal: %v %+v", err, v)
	}
	// And it cannot be answered, because nothing is being asked any more.
	if _, err := AnswerProposal(prior, AnswerTrack, "user", withdrawNow); gateCode(err) != "proposal_withdrawn" {
		t.Fatalf("answering a withdrawn proposal: %v", err)
	}
}
