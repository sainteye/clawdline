package http

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Both briefs an owner receives — typed into an existing Session, and the
// acceptance of a new Session's Root Assignment — tell it to break
// multi-stage work into steps with `clawdline item step-add` first, and that
// a single straightforward change takes none.
func TestEveryOwnerBriefSaysWhenToBreakTheItemIntoSteps(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	if _, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	sent := p.done()
	if len(sent) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	for name, brief := range map[string]string{
		"existing Session":  sent[0],
		"Root Assignment":   workV2RootAssignmentAcceptance(v.Item.ID, v.Item.Kind),
		"brief as composed": workV2AssignmentBrief(v.Item.ID, v.Item.Title, v.Item.Kind),
	} {
		for _, want := range []string{"clawdline item step-add " + v.Item.ID, "clawdline item step-done",
			"multi-stage", "takes no steps", "clawdline item phase " + v.Item.ID + " implementing"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s brief lacks %q:\n%s", name, want, brief)
			}
		}
	}
}

// Every brief an Epic's owner receives spells out the Epic procedure — write
// the plan, have a child review it, record the review, break it into steps,
// then implement — and a Feature's does not.
func TestAnEpicsOwnerBriefSaysPlanReviewStepsThenImplement(t *testing.T) {
	const id = "0e0e0e0e-0000-4000-8000-000000000001"
	for name, brief := range map[string]string{
		"Root Assignment": workV2RootAssignmentAcceptance(id, work.KindEpic),
		"assignment":      workV2AssignmentBrief(id, "Big", work.KindEpic),
		"reassignment":    workV2ReassignmentBrief(id, "Big", work.KindEpic, work.PhaseAssigned),
	} {
		for _, want := range []string{"clawdline item doc " + id + " --role plan ",
			"clawdline dispatch --kind plan_review --work-id " + id, "--role plan_review", "--reference <task id>",
			"clawdline item step-add " + id, "clawdline item phase " + id + " implementing", "clawdline guide epic"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s brief lacks %q:\n%s", name, want, brief)
			}
		}
	}
	if strings.Contains(workV2AssignmentBrief(id, "Small", work.KindFeature), "plan_review") {
		t.Error("a Feature's brief asks for a plan review")
	}
}

// An Epic that changes a person-facing interface, journey, or product policy
// needs an independent UX/product review before merging. The owner brief must
// make that conditional mandatory rather than adding ceremony to backend-only
// work, and the reviewer must report gaps as unverified instead of inventing
// a pass.
func TestAnEpicsOwnerBriefRequiresApplicableUXProductReview(t *testing.T) {
	const id = "0e0e0e0e-0000-4000-8000-000000000003"
	for name, brief := range map[string]string{
		"Root Assignment": workV2RootAssignmentAcceptance(id, work.KindEpic),
		"assignment":      workV2AssignmentBrief(id, "Big", work.KindEpic),
		"reassignment":    workV2ReassignmentBrief(id, "Big", work.KindEpic, work.PhaseAssigned),
	} {
		for _, want := range []string{"human-facing interface", "product policy", "before merging",
			"clawdline dispatch --kind review --work-id " + id + " --claims \"\" --persona ux-architect",
			"mark unverified and say why"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s brief lacks %q:\n%s", name, want, brief)
			}
		}
	}
}

func TestRootAssignmentAcceptanceStartsWithTheExactGovernedCriteria(t *testing.T) {
	criteria := "- first byte stays first\n- trailing newline stays\n"
	item := work.ItemV2{ID: "0e0e0e0e-0000-4000-8000-000000000002", Kind: work.KindFeature,
		Phase: work.PhaseAssigned, Cycle: 1, GateSnapshotCycle: 1, PlanningGate: true,
		AcceptanceCriteria: criteria}
	got := workV2RootAssignmentAcceptanceForItem(item)
	if !strings.HasPrefix(got, criteria+"\n") {
		t.Fatalf("Root ACCEPTANCE did not begin with exact criteria:\n%q", got)
	}
	for _, want := range []string{"planning_gate=true", "verify_gate=false", "plan_review", "clawdline item phase " + item.ID} {
		if !strings.Contains(got, want) {
			t.Errorf("Root ACCEPTANCE lacks %q:\n%s", want, got)
		}
	}
}

func TestEmptyGatedAcceptanceAsksTheAssignedAgentToWriteIt(t *testing.T) {
	item := work.ItemV2{ID: "0e0e0e0e-0000-4000-8000-000000000003", Kind: work.KindEpic,
		Phase: work.PhaseAssigned, Cycle: 1, GateSnapshotCycle: 1, PlanningGate: true}
	for name, brief := range map[string]string{
		"existing Session": workV2AssignmentBriefForItem(item),
		"new Root":         workV2RootAssignmentAcceptanceForItem(item),
	} {
		if !strings.Contains(brief, "clawdline item acceptance "+item.ID) ||
			!strings.Contains(brief, "The person does not need to fill acceptance criteria") {
			t.Errorf("%s omitted Agent acceptance instruction: %s", name, brief)
		}
	}
}
