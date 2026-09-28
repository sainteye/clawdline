package work

import (
	"testing"
	"time"
)

func TestV2KindsAndAreas(t *testing.T) {
	for _, k := range []Kind{KindFeature, KindIssue, KindEpic, KindRefactor, KindPlan} {
		if !k.Valid() {
			t.Fatalf("%s is not valid", k)
		}
	}
	if !KindFeature.Executable() || !KindIssue.Executable() || !KindEpic.Executable() ||
		KindRefactor.Executable() || KindPlan.Executable() {
		t.Fatal("the executable boundary changed")
	}
	cases := []struct {
		item ItemV2
		want string
	}{
		{ItemV2{Kind: KindPlan, Phase: PhaseCreated}, "planning"},
		{ItemV2{Kind: KindRefactor, Phase: PhaseCreated}, "planning"},
		{ItemV2{Kind: KindEpic, Phase: PhaseCreated}, "unassigned"},
		{ItemV2{Kind: KindEpic, Phase: PhaseImplementing, OwnerSession: "session-a"}, "implementing"},
		{ItemV2{Kind: KindFeature, Phase: PhaseImplementing}, "unassigned"},
		{ItemV2{Kind: KindFeature, Phase: PhaseVerifying, OwnerSession: "session-a"}, "verifying"},
		{ItemV2{Kind: KindFeature, Phase: PhaseDone}, "recently_done"},
	}
	for _, c := range cases {
		if got := c.item.Area(); got != c.want {
			t.Errorf("%+v area %q, want %q", c.item, got, c.want)
		}
	}
}

func TestV2CreationRequiresHumanReadableWork(t *testing.T) {
	valid := ItemV2{ProjectID: "project", Kind: KindFeature, Title: "Feature", Description: "What changes",
		Phase: PhaseCreated, DeploymentPolicy: DeployAgentDecides}
	if err := ValidateNewV2(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ItemV2){
		"project":     func(i *ItemV2) { i.ProjectID = "" },
		"kind":        func(i *ItemV2) { i.Kind = "wish" },
		"title":       func(i *ItemV2) { i.Title = " " },
		"description": func(i *ItemV2) { i.Description = "" },
		"policy":      func(i *ItemV2) { i.DeploymentPolicy = "maybe" },
	} {
		t.Run(name, func(t *testing.T) {
			got := valid
			mutate(&got)
			if ValidateNewV2(got) == nil {
				t.Fatal("invalid creation was accepted")
			}
		})
	}
}

func TestOnlyTheClosedAgentLifecycleAdvances(t *testing.T) {
	i := ItemV2{Kind: KindFeature, OwnerSession: "session-a", Phase: PhaseAssigned,
		DeploymentPolicy: DeployAgentDecides}
	if err := AgentTransition(i, PhaseImplementing, false, false, false, false); err != nil {
		t.Fatal(err)
	}
	i.Phase = PhaseVerifying
	if AgentTransition(i, PhaseMerging, false, false, false, false) == nil {
		t.Fatal("verification without evidence advanced")
	}
	if err := AgentTransition(i, PhaseMerging, true, false, false, false); err != nil {
		t.Fatal(err)
	}
	i.Phase = PhaseMerging
	if AgentTransition(i, PhaseDeploying, true, false, false, false) == nil {
		t.Fatal("an unlanded merge advanced")
	}
	if err := AgentTransition(i, PhaseDeploying, true, true, false, false); err != nil {
		t.Fatal(err)
	}
	i.Phase = PhaseDeploying
	if AgentTransition(i, PhaseDone, true, true, false, false) == nil {
		t.Fatal("agent_decides completed without deploy evidence or a reason")
	}
	if err := AgentTransition(i, PhaseDone, true, true, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnassignedOrTerminalItemRejectsAgentProgress(t *testing.T) {
	i := ItemV2{Kind: KindFeature, Phase: PhaseAssigned, DeploymentPolicy: DeployAgentDecides}
	if AgentTransition(i, PhaseImplementing, false, false, false, false) == nil {
		t.Fatal("unassigned work advanced")
	}
	i.OwnerSession, i.Phase = "session-a", PhaseDone
	if AgentTransition(i, PhaseImplementing, false, false, false, false) == nil {
		t.Fatal("terminal work advanced")
	}
}

// The Epic gate reads the plan documents in the order they were written: a
// plan, then a review after the latest plan. Only an Epic's step from
// assigned to implementing is gated.
func TestEpicPlanGate(t *testing.T) {
	epic := ItemV2{ID: "e", Kind: KindEpic, Phase: PhaseAssigned, OwnerSession: "s"}
	docs := func(roles ...string) []DocumentV2 {
		out := []DocumentV2{}
		for _, r := range roles {
			out = append(out, DocumentV2{Role: r})
		}
		return out
	}
	cases := []struct {
		name  string
		item  ItemV2
		next  Phase
		plans []DocumentV2
		want  string
	}{
		{"nothing", epic, PhaseImplementing, docs(), "epic_plan_required"},
		{"a review alone", epic, PhaseImplementing, docs(DocumentPlanReview), "epic_plan_required"},
		{"a plan alone", epic, PhaseImplementing, docs(DocumentPlan), "epic_plan_review_required"},
		{"a review before the plan", epic, PhaseImplementing, docs(DocumentPlanReview, DocumentPlan), "epic_plan_review_required"},
		{"a rewritten plan", epic, PhaseImplementing, docs(DocumentPlan, DocumentPlanReview, DocumentPlan), "epic_plan_review_required"},
		{"a reviewed plan", epic, PhaseImplementing, docs(DocumentPlan, DocumentPlanReview), ""},
		{"a re-reviewed plan", epic, PhaseImplementing, docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview), ""},
		{"a plan rewritten after two reviews", epic, PhaseImplementing, docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview, DocumentPlan), ""},
		{"a Feature", ItemV2{Kind: KindFeature, Phase: PhaseAssigned}, PhaseImplementing, docs(), ""},
		{"back from verifying", ItemV2{Kind: KindEpic, Phase: PhaseVerifying}, PhaseImplementing, docs(), ""},
	}
	for _, c := range cases {
		err := EpicPlanGate(c.item, c.next, c.plans)
		got := ""
		if r, ok := AsRefusalV2(err); ok {
			got = r.Code
		} else if err != nil {
			t.Fatalf("%s: untyped %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPlanRolesApplyToAFeatureOrEpic(t *testing.T) {
	for _, role := range []string{DocumentPlan, DocumentPlanReview} {
		if !DocumentRoleValid(role) {
			t.Fatalf("%s is not a role", role)
		}
		if DocumentRoleApplies(ItemV2{Kind: KindEpic}, role) != nil {
			t.Fatalf("%s refused on an Epic", role)
		}
		if DocumentRoleApplies(ItemV2{Kind: KindFeature}, role) != nil {
			t.Fatalf("%s refused on a Feature", role)
		}
		for _, k := range []Kind{KindIssue, KindRefactor, KindPlan} {
			if r, ok := AsRefusalV2(DocumentRoleApplies(ItemV2{Kind: k}, role)); !ok || r.Code != "document_role_not_applicable" {
				t.Fatalf("%s on a %s: %v", role, k, r)
			}
		}
	}
	if DocumentRoleApplies(ItemV2{Kind: KindFeature}, "completion_report") != nil || DocumentRoleValid("wish") {
		t.Fatal("the other roles changed")
	}
}

func TestAcceptanceUsesExactBytesAndMonotonicVersions(t *testing.T) {
	var item ItemV2
	if !SetAcceptance(&item, "- observable\n") || item.AcceptanceVersion != 1 ||
		item.AcceptanceDigest != AcceptanceDigest("- observable\n") {
		t.Fatalf("initial acceptance = %+v", item)
	}
	if SetAcceptance(&item, "- observable\n") || item.AcceptanceVersion != 1 {
		t.Fatalf("identical bytes changed acceptance = %+v", item)
	}
	if !SetAcceptance(&item, "- observable") || item.AcceptanceVersion != 2 ||
		item.AcceptanceDigest == AcceptanceDigest("- observable\n") {
		t.Fatalf("exact-byte change = %+v", item)
	}
}

func TestPlanningGateUsesTheCapturedModeAndKind(t *testing.T) {
	docs := func(roles ...string) []DocumentV2 {
		out := make([]DocumentV2, 0, len(roles))
		for _, role := range roles {
			out = append(out, DocumentV2{Role: role})
		}
		return out
	}
	item := func(kind Kind, planning bool) ItemV2 {
		return ItemV2{ID: "w", Kind: kind, Phase: PhaseAssigned, Cycle: 3, GateSnapshotCycle: 3,
			GateSnapshotAt: time.Unix(1, 0), PlanningGate: planning, AcceptanceCriteria: "It works."}
	}
	code := func(err error) string {
		if err == nil {
			return ""
		}
		if refusal, ok := AsRefusalV2(err); ok {
			return refusal.Code
		}
		t.Fatalf("untyped refusal: %v", err)
		return ""
	}
	for _, kind := range []Kind{KindFeature, KindEpic} {
		if got := code(PlanningGate(item(kind, false), PhaseImplementing, nil)); got != "" {
			t.Errorf("planning-off %s = %s", kind, got)
		}
	}
	if got := code(PlanningGate(item(KindIssue, true), PhaseImplementing, nil)); got != "" {
		t.Fatalf("Issue was gated: %s", got)
	}
	feature := item(KindFeature, true)
	if got := code(PlanningGate(feature, PhaseImplementing, nil)); got != "feature_plan_required" {
		t.Fatalf("Feature without plan = %s", got)
	}
	if got := code(PlanningGate(feature, PhaseImplementing, docs(DocumentPlan))); got != "feature_plan_review_required" {
		t.Fatalf("Feature without review = %s", got)
	}
	if got := code(PlanningGate(feature, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan))); got != "" {
		t.Fatalf("Feature exceeded its one-review ceiling: %s", got)
	}
	epic := item(KindEpic, true)
	if got := code(PlanningGate(epic, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan))); got != "epic_plan_review_required" {
		t.Fatalf("Epic accepted one stale review: %s", got)
	}
	if got := code(PlanningGate(epic, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview, DocumentPlan))); got != "" {
		t.Fatalf("Epic exceeded its two-review ceiling: %s", got)
	}
}
