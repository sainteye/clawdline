package work

import (
	"testing"
	"time"
)

// The person's switch, not an Agent's classification, decides whether a
// planning-on Feature needs a reviewed plan. A required deployment no longer
// forces a review by itself.
func TestFeatureReviewFollowsThePersonsSwitch(t *testing.T) {
	item := ItemV2{ID: "feature", Kind: KindFeature, Phase: PhaseAssigned, Cycle: 1,
		GateSnapshotCycle: 1, GateSnapshotAt: time.Unix(1, 0), PlanningGate: true,
		AcceptanceCriteria: "A visible result", DeploymentPolicy: DeployAgentDecides}
	code := func(docs ...DocumentV2) string {
		t.Helper()
		err := PlanningGate(item, PhaseImplementing, docs)
		if err == nil {
			return ""
		}
		r, ok := AsRefusalV2(err)
		if !ok {
			t.Fatalf("untyped refusal: %v", err)
		}
		return r.Code
	}
	if got := code(); got != "" {
		t.Fatalf("unchecked Feature with acceptance and no documents: %s", got)
	}
	item.DeploymentPolicy = DeployRequired
	if got := code(); got != "" {
		t.Fatalf("unchecked Feature with a required deployment: %s", got)
	}
	item.AcceptanceCriteria = ""
	if got := code(); got != "acceptance_required" {
		t.Fatalf("unchecked Feature without acceptance: %s", got)
	}
	item.AcceptanceCriteria, item.DeploymentPolicy = "A visible result", DeployAgentDecides
	item.ReviewRequired = true
	if got := code(); got != "feature_plan_required" {
		t.Fatalf("checked Feature without a plan: %s", got)
	}
	if got := code(DocumentV2{Role: DocumentPlan}); got != "feature_plan_review_required" {
		t.Fatalf("checked Feature with an unreviewed plan: %s", got)
	}
	if got := code(DocumentV2{Role: DocumentPlan}, DocumentV2{Role: DocumentPlanReview}); got != "" {
		t.Fatalf("checked Feature with a reviewed plan: %s", got)
	}
}

func TestFeaturePlanRevisionNamesItsRiskBoundary(t *testing.T) {
	item := ItemV2{ID: "feature", Kind: KindFeature, Phase: PhaseAssigned, Cycle: 1,
		GateSnapshotCycle: 1, GateSnapshotAt: time.Unix(1, 0), PlanningGate: true,
		AcceptanceCriteria: "A visible result", ReviewRequired: true}
	plan := DocumentV2{Role: DocumentPlan}
	review := DocumentV2{Role: DocumentPlanReview}
	boundary := DocumentV2{Role: "other", Title: ReviewBoundaryTitle,
		Body: `{"new_risk_boundary":false,"reason":"Only test wording changed; deployment and access remain reviewed."}`}
	check := func(want string, docs ...DocumentV2) {
		t.Helper()
		err := PlanningGate(item, PhaseImplementing, docs)
		got := ""
		if err != nil {
			r, ok := AsRefusalV2(err)
			if !ok {
				t.Fatalf("untyped refusal: %v", err)
			}
			got = r.Code
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	check("feature_plan_review_required", plan, review, plan)
	check("", plan, review, plan, boundary)
	check("feature_plan_review_required", plan, review, boundary, plan)
	check("feature_plan_review_required", plan, review, plan,
		DocumentV2{Role: "other", Title: ReviewBoundaryTitle, Body: `{"new_risk_boundary":true,"reason":"Access changes."}`})
	check("", plan, review, plan, review)
}
