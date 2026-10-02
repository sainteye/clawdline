package work

import (
	"errors"
	"strconv"
	"strings"
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
		"review":      func(i *ItemV2) { i.Kind, i.ReviewRequired = KindIssue, true },
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
		err := EpicPlanGate(c.item, c.next, c.plans, PlanReviewSummary{})
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
		if got := code(PlanningGate(item(kind, false), PhaseImplementing, nil, PlanReviewSummary{})); got != "" {
			t.Errorf("planning-off %s = %s", kind, got)
		}
	}
	if got := code(PlanningGate(item(KindIssue, true), PhaseImplementing, nil, PlanReviewSummary{})); got != "" {
		t.Fatalf("Issue was gated: %s", got)
	}
	feature := item(KindFeature, true)
	feature.ReviewRequired = true
	if got := code(PlanningGate(feature, PhaseImplementing, nil, PlanReviewSummary{})); got != "feature_plan_required" {
		t.Fatalf("Feature without plan = %s", got)
	}
	if got := code(PlanningGate(feature, PhaseImplementing, docs(DocumentPlan), PlanReviewSummary{})); got != "feature_plan_review_required" {
		t.Fatalf("Feature without review = %s", got)
	}
	// A successful review child records itself (app.AddPlanReviewFromTask),
	// so the refusal says to wait for it before it offers the manual command.
	var waiting RefusalV2
	if !errors.As(PlanningGate(feature, PhaseImplementing, docs(DocumentPlan), PlanReviewSummary{}), &waiting) ||
		!strings.Contains(waiting.Message, "wait for that child to finish") ||
		!strings.Contains(waiting.Message, "Only if it is not") {
		t.Fatalf("review-required message does not say to wait for the child: %+v", waiting)
	}
	if got := code(PlanningGate(feature, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan), PlanReviewSummary{})); got != "feature_plan_review_required" {
		t.Fatalf("Feature revision without boundary evidence = %s", got)
	}
	epic := item(KindEpic, true)
	if got := code(PlanningGate(epic, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan), PlanReviewSummary{})); got != "epic_plan_review_required" {
		t.Fatalf("Epic accepted one stale review: %s", got)
	}
	if got := code(PlanningGate(epic, PhaseImplementing,
		docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview, DocumentPlan), PlanReviewSummary{})); got != "" {
		t.Fatalf("Epic exceeded its two-review ceiling: %s", got)
	}
}

func TestPlanningGateBlocksOnlyAReviewWithABlockingFinding(t *testing.T) {
	docs := func(roles ...string) []DocumentV2 {
		out := make([]DocumentV2, 0, len(roles))
		for _, role := range roles {
			out = append(out, DocumentV2{Role: role})
		}
		return out
	}
	gated := func(kind Kind) ItemV2 {
		return ItemV2{ID: "w", Kind: kind, Phase: PhaseAssigned, Cycle: 1, GateSnapshotCycle: 1,
			GateSnapshotAt: time.Unix(1, 0), PlanningGate: true, ReviewRequired: kind == KindFeature, AcceptanceCriteria: "It works."}
	}
	receipt := func(severities ...string) []byte {
		var findings []string
		for n, s := range severities {
			sev := ""
			if s != "-" {
				sev = `"severity":` + strconv.Quote(s) + `,`
			}
			findings = append(findings, `{"id":"f`+strconv.Itoa(n)+`",`+sev+`"summary":"finding `+strconv.Itoa(n)+`","evidence":["x"]}`)
		}
		return []byte(`{"verdict":"x","axes":[{"axis":"specification","status":"findings","findings":[` +
			strings.Join(findings, ",") + `]},{"axis":"repository_invariants","status":"pass","findings":[]},` +
			`{"axis":"runtime_failure_behavior","status":"pass","findings":[]}]}`)
	}
	reviewed := docs(DocumentPlan, DocumentPlanReview)
	cases := []struct {
		name  string
		item  ItemV2
		plans []DocumentV2
		raw   []byte
		want  string
	}{
		{"zero findings", gated(KindFeature), reviewed, receipt(), ""},
		{"only non-blocking", gated(KindFeature), reviewed, receipt("non_blocking", "minor", "important"), ""},
		{"contains blocking", gated(KindFeature), reviewed, receipt("non_blocking", "blocking"), "feature_plan_review_blocking"},
		{"epic contains blocking", gated(KindEpic), reviewed, receipt("blocking"), "epic_plan_review_blocking"},
		{"legacy finding without severity", gated(KindFeature), reviewed, receipt("blocking", "-"), ""},
		{"legacy unreadable receipt", gated(KindFeature), reviewed, nil, ""},
		{"feature revised after a blocking review still waits for a review", gated(KindFeature),
			docs(DocumentPlan, DocumentPlanReview, DocumentPlan), receipt("blocking"), "feature_plan_review_required"},
		{"epic revised with a round left waits for its next review", gated(KindEpic),
			docs(DocumentPlan, DocumentPlanReview, DocumentPlan), receipt("blocking"), "epic_plan_review_required"},
		{"epic second round still blocking", gated(KindEpic),
			docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview), receipt("blocking"), "epic_plan_review_blocking"},
		{"epic rounds used and plan revised after a blocking review", gated(KindEpic),
			docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview, DocumentPlan), receipt("blocking"), "epic_plan_review_blocking"},
		{"epic second round non-blocking", gated(KindEpic),
			docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview), receipt("non_blocking"), ""},
	}
	for _, c := range cases {
		err := PlanningGate(c.item, PhaseImplementing, c.plans, ReadPlanReview(c.raw))
		got := ""
		if refusal, ok := AsRefusalV2(err); ok {
			got = refusal.Code
		} else if err != nil {
			t.Fatalf("%s: untyped %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: code = %q, want %q (%v)", c.name, got, c.want, err)
		}
	}

	err := PlanningGate(gated(KindFeature), PhaseImplementing, reviewed, ReadPlanReview(receipt("blocking", "non_blocking")))
	refusal, _ := AsRefusalV2(err)
	for _, want := range []string{`"finding 0"`, "clawdline dispatch --kind plan_review --work-id w", "--role plan", PlanningGateOverride} {
		if !strings.Contains(refusal.Message, want) {
			t.Errorf("blocking refusal does not say %q: %s", want, refusal.Message)
		}
	}
	if strings.Contains(refusal.Message, `"finding 1"`) {
		t.Errorf("blocking refusal lists a non-blocking finding: %s", refusal.Message)
	}

	err = PlanningGate(gated(KindEpic), PhaseImplementing, docs(DocumentPlan, DocumentPlanReview, DocumentPlan, DocumentPlanReview),
		ReadPlanReview(receipt("blocking")))
	refusal, _ = AsRefusalV2(err)
	for _, want := range []string{"used its 2 plan reviews", "raises the review limit", PlanningGateOverride} {
		if !strings.Contains(refusal.Message, want) {
			t.Errorf("Epic limit refusal does not say %q: %s", want, refusal.Message)
		}
	}
	if strings.Contains(refusal.Message, "dispatch --kind plan_review") {
		t.Errorf("Epic limit refusal offers a third review: %s", refusal.Message)
	}

	many := make([]string, PlanReviewBlockingListLimit+3)
	for n := range many {
		many[n] = "blocking"
	}
	err = PlanningGate(gated(KindFeature), PhaseImplementing, reviewed, ReadPlanReview(receipt(many...)))
	refusal, _ = AsRefusalV2(err)
	if !strings.Contains(refusal.Message, "and 3 more") || strings.Contains(refusal.Message, `"finding 8"`) {
		t.Errorf("a long blocking list is not bounded: %s", refusal.Message)
	}
}

func TestPlanReviewDispatchGateLetsOnlyAReviewThroughABlockingPlan(t *testing.T) {
	plans := []DocumentV2{{Role: DocumentPlan}, {Role: DocumentPlanReview}}
	item := ItemV2{ID: "w", Kind: KindEpic, Phase: PhaseAssigned, Cycle: 1, GateSnapshotCycle: 1,
		GateSnapshotAt: time.Unix(1, 0), PlanningGate: true, AcceptanceCriteria: "It works."}
	blocking := PlanReviewSummary{Blocking: []string{"no rollback"}}
	if refusal, ok := AsRefusalV2(PlanReviewDispatchGate(item, "custom", plans, blocking)); !ok || refusal.Code != "epic_plan_review_blocking" {
		t.Fatalf("a dispatch past a blocking review was not refused")
	}
	if err := PlanReviewDispatchGate(item, DocumentPlanReview, plans, blocking); err != nil {
		t.Fatalf("a new plan review was refused: %v", err)
	}
	if err := PlanReviewDispatchGate(item, "custom", plans, PlanReviewSummary{Blocking: nil}); err != nil {
		t.Fatalf("a non-blocking review stopped a dispatch: %v", err)
	}
	if err := PlanReviewDispatchGate(item, "custom", plans, PlanReviewSummary{Legacy: true}); err != nil {
		t.Fatalf("a legacy review stopped a dispatch: %v", err)
	}
	moved := item
	moved.Phase = PhaseImplementing
	if err := PlanReviewDispatchGate(moved, "custom", plans, blocking); err != nil {
		t.Fatalf("an item already implementing was refused: %v", err)
	}
	feature := item
	feature.Kind = KindFeature
	if err := PlanReviewDispatchGate(feature, "custom", plans, blocking); err != nil {
		t.Fatalf("a Feature without the review switch was refused: %v", err)
	}
}
