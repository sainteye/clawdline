package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The moment a root is told. Until now what a child did not do reached the
// root's tab as prose in a file it might read; this is the one line that says
// there is something there and what it costs to raise it — and, for a task
// with nothing left over, says nothing at all.
func TestTheFinishedLineCarriesWhatIsLeftOver(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir())}
	r := Record{ID: "a7000000-0000-4000-8000-000000000001", Title: "half of it", State: StateSuccess,
		Notice: &Notice{ID: "n1"},
		Result: &taskdir.Result{Status: "success", Leftovers: []work.Leftover{
			{Title: "the four daemon defects", Why: "out of time"},
			{Title: "the half-done feature"},
		}}}

	line := b.FinishedLine(r, "n1")
	for _, want := range []string{"2 leftover(s)", "clawdline task show " + r.ID} {
		if !strings.Contains(line, want) {
			t.Errorf("the line does not say %q:\n%s", want, line)
		}
	}
	// How to raise one, and that nothing reaches the person's board until
	// they answer, is the guide's (§5): read once a session, not typed once a
	// child.
	if strings.Contains(line, "/v1/orchestrator/proposals") {
		t.Errorf("the line still spells out the proposal route:\n%s", line)
	}

	// The envelope carries the count, and stays one physical line whatever
	// the child wrote.
	wire, err := b.NoticeWire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	var body noticeBody
	inner := strings.TrimSuffix(strings.TrimPrefix(wire, "<clawdline-notice>"), "</clawdline-notice>")
	if err := json.Unmarshal([]byte(inner), &body); err != nil {
		t.Fatal(err)
	}
	if body.Leftovers != 2 {
		t.Fatalf("the envelope says %d leftovers", body.Leftovers)
	}
	if strings.ContainsAny(wire, "\n\r") {
		t.Fatal("the notice is not one physical line")
	}

	// A delivery with nothing left over, and a task with no result at all,
	// say nothing: an extra clause on every finished task would be noise, and
	// the broker's own verdict is not the child speaking.
	quiet := r
	quiet.Result = &taskdir.Result{Status: "success"}
	if strings.Contains(b.FinishedLine(quiet, "n1"), "leftover") {
		t.Error("a delivery with no leftovers still said something")
	}
	timedOut := Record{ID: r.ID, Title: r.Title, State: StateTimeout, Verdict: "passed its timeout"}
	if strings.Contains(b.FinishedLine(timedOut, ""), "leftover") {
		t.Error("a task with no result named leftovers")
	}
}

// The child's briefing names the field, and says in the same breath that it is
// not a new obligation. A field no briefing mentions is a field no child ever
// writes, which is how this whole path would have been inert.
func TestTheBriefingAsksForLeftoversWithoutRequiringThem(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir())}
	r := Record{ID: "a7000000-0000-4000-8000-000000000001", Title: "half of it", Kind: "custom",
		TimeoutMinutes: 30, ProjectDir: "/p", Assistant: "claude",
		Root: &RootRef{SessionID: "conv", Assistant: "claude"}}
	brief := b.ChildBrief(r, "/p")
	for _, want := range []string{`"leftovers"`, "suggested_acceptance", "It is optional",
		"not a new obligation", "Leave it out when you finished everything you were asked.",
		"what will be different when it is done", "Do not use the person", "60 characters"} {
		if !strings.Contains(brief, want) {
			t.Errorf("CHILD.md does not say %q", want)
		}
	}
}
