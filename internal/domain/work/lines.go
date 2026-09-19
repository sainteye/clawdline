package work

import (
	"crypto/sha256"
	"encoding/hex"
)

// Which line of work a dispatch belongs to (design-decisions D36: a dispatch
// carries a work_id, and one that names none is bound by the broker's rules).
//
// A line of work is the id its tasks, its to-dos, its proposals and — once a
// person follows it — its work item share. It is decided when the broker
// admits the dispatch, from facts the dispatch itself carries, and never from
// what an agent says of itself later:
//
//   - a work_id the dispatch named is the line (the root said so);
//   - a task stored before lines were bound, that a proposal was made about,
//     is on the line that proposal named: the proposal is the fact that
//     named its line, and a work item a person made from it follows that line;
//   - a respawn is the same work as the task it retries, so it is on that
//     task's line;
//   - the nodes of one task graph are one split-and-join the root declared
//     (graphs.go in the broker), so they share a line, derived from the root
//     and the graph's id;
//   - otherwise the dispatch begins a line of its own, named by its task id:
//     a new line of work is what "a root sent a child" is (I1), and naming it
//     by the task that began it makes the answer the same however often it is
//     asked — at admission, or when a start finds a task stored before this
//     rule existed.
//
// Two dispatches are never put on one line by a guess — the same project, the
// same hour, a similar title. A wrong merge would close one person's item on
// another piece of work's landing; a line that should have been joined is
// joined by the root naming its work_id.
//
// A task with no root owes no session anything and is on no line. A step of
// other work — a review, a test, a correction — whose line was neither named
// nor inherited stays unbound: it is a step of a line the broker cannot see,
// and giving it a line of its own would make a line nobody would ever be
// asked about (§4.2 never proposes a step).

// How a task's line was decided: the task record's `work_from`.
const (
	WorkNamed    = "named"
	WorkProposal = "proposal"
	WorkRespawn  = "respawn"
	WorkGraph    = "graph"
	WorkDispatch = "dispatch"
)

// LineFacts is what a dispatch says about its line of work.
type LineFacts struct {
	Task string
	// Named is the work_id the dispatch named, empty when none.
	Named string
	// Proposed is the line the newest proposal about this task named, empty
	// when none was made (ProposedLine).
	Proposed string
	Kind     string
	// Owner is the root's conversation id; empty for a task with no root.
	Owner string
	// RespawnLine is the line of the task a respawn retries, empty when it is
	// not a respawn or that task is on none.
	RespawnLine string
	// Graph is the id of the task graph the dispatch is a node of, or empty.
	Graph string
}

// LineOf is the line a dispatch is on and how that was decided; empty when it
// is on none.
func LineOf(f LineFacts) (workID, from string) {
	switch {
	case f.Named != "":
		return f.Named, WorkNamed
	case f.Proposed != "":
		return f.Proposed, WorkProposal
	case f.Owner == "":
		return "", ""
	case f.RespawnLine != "":
		return f.RespawnLine, WorkRespawn
	case f.Graph != "":
		return GraphLine(f.Owner, f.Graph), WorkGraph
	case auxiliary(f.Kind):
		return "", ""
	}
	return f.Task, WorkDispatch
}

// GraphLine is the line every node of one root's task graph shares: a
// lowercase UUID derived from the root and the graph's id, so the nodes agree
// on it without reading one another.
func GraphLine(owner, graph string) string {
	sum := sha256.Sum256([]byte("clawdline work line\x00" + owner + "\x00" + graph))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ProposedLine is the line the newest proposal about task named, whatever was
// answered: a proposal that was declined still named the line its task's
// facts belong to, and a second line for the same task would be proposed
// again and refused as a duplicate for ever.
func ProposedLine(prior []Proposal, task string) string {
	line := ""
	for _, p := range prior {
		if p.TaskID == task && p.WorkID != "" {
			line = p.WorkID
		}
	}
	return line
}
