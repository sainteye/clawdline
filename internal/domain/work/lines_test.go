package work

import "testing"

// Which line a dispatch is on, rule by rule, each with the control the next
// rule answers.
func TestADispatchIsOnTheLineItsFactsSay(t *testing.T) {
	const (
		task  = "7a5c0000-0000-4000-8000-000000000001"
		named = "0f0f0f0f-0000-4000-8000-000000000001"
		prior = "0f0f0f0f-0000-4000-8000-000000000002"
		root  = "root-conv"
	)
	cases := []struct {
		name string
		f    LineFacts
		line string
		from string
	}{
		{"named wins over everything", LineFacts{Task: task, Named: named, Proposed: prior, Owner: root, RespawnLine: prior, Graph: "g"},
			named, WorkNamed},
		{"a proposal's line outranks the rules", LineFacts{Task: task, Proposed: prior, Owner: root, Graph: "g"},
			prior, WorkProposal},
		{"a respawn keeps its original's line", LineFacts{Task: task, Owner: root, RespawnLine: prior, Graph: "g"},
			prior, WorkRespawn},
		{"a graph node is on its graph's line", LineFacts{Task: task, Owner: root, Graph: "g"},
			GraphLine(root, "g"), WorkGraph},
		{"otherwise a dispatch begins its own", LineFacts{Task: task, Owner: root, Kind: "custom"},
			task, WorkDispatch},
		{"no root, no line", LineFacts{Task: task, Kind: "custom"}, "", ""},
		{"a step of other work nobody placed", LineFacts{Task: task, Owner: root, Kind: "code-review"}, "", ""},
		{"a named step is on its line", LineFacts{Task: task, Owner: root, Kind: "review", Named: named},
			named, WorkNamed},
	}
	for _, c := range cases {
		line, from := LineOf(c.f)
		if line != c.line || from != c.from {
			t.Errorf("%s: got %q %q, want %q %q", c.name, line, from, c.line, c.from)
		}
	}
	// A graph's line is the same for every node of it under one root, and a
	// different one under another root or for another graph.
	a, b := GraphLine(root, "g"), GraphLine(root, "g")
	if a != b || a == GraphLine("other", "g") || a == GraphLine(root, "h") || !RunShaped(a) {
		t.Fatalf("graph lines: %s %s", a, b)
	}
}

// The line a proposal named for a task is the newest one about that task,
// whatever the answer.
func TestTheNewestProposalAboutATaskNamesItsLine(t *testing.T) {
	const task = "7a5c0000-0000-4000-8000-000000000001"
	prior := []Proposal{
		{WorkID: "0f0f0f0f-0000-4000-8000-000000000001", TaskID: task, State: ProposalAnswered, Answer: AnswerNo},
		{WorkID: "0f0f0f0f-0000-4000-8000-000000000002", TaskID: "7a5c0000-0000-4000-8000-000000000002"},
		{WorkID: "0f0f0f0f-0000-4000-8000-000000000003", TaskID: task, State: ProposalPending},
	}
	if got := ProposedLine(prior, task); got != prior[2].WorkID {
		t.Fatalf("got %q", got)
	}
	if got := ProposedLine(prior[:1], task); got != prior[0].WorkID {
		t.Fatalf("a declined proposal still named the line: %q", got)
	}
	if got := ProposedLine(nil, task); got != "" {
		t.Fatalf("no proposal named %q", got)
	}
}
