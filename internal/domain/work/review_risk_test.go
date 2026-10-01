package work

import (
	"testing"
	"time"
)

func TestFeatureReviewFollowsRecordedRisk(t *testing.T) {
	item := ItemV2{ID: "feature", Kind: KindFeature, Phase: PhaseAssigned, Cycle: 1,
		GateSnapshotCycle: 1, GateSnapshotAt: time.Unix(1, 0), PlanningGate: true,
		AcceptanceCriteria: "A visible result", DeploymentPolicy: DeployAgentDecides}
	code := func(docs ...DocumentV2) string {
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
	routine := DocumentV2{Role: "other", Title: ReviewRiskTitle, Body: `{"production_deployment":false,"access_or_security":false,"cross_data_transaction":false,"irreversible_effect":false,"reason":"Only a local display label changes."}`}
	if got := code(); got != "feature_plan_required" {
		t.Fatalf("unclassified: %s", got)
	}
	if got := code(routine); got != "" {
		t.Fatalf("routine: %s", got)
	}
	if got := code(routine, DocumentV2{Role: DocumentPlan}); got != "feature_plan_review_required" {
		t.Fatalf("plan changed after assessment: %s", got)
	}
	for _, body := range []string{
		`{"production_deployment":true,"access_or_security":false,"cross_data_transaction":false,"irreversible_effect":false,"reason":"Deploys production."}`,
		`{"production_deployment":false,"access_or_security":true,"cross_data_transaction":false,"irreversible_effect":false,"reason":"Changes access."}`,
		`{"production_deployment":false,"access_or_security":false,"cross_data_transaction":true,"irreversible_effect":false,"reason":"Moves ownership."}`,
		`{"production_deployment":false,"access_or_security":false,"cross_data_transaction":false,"irreversible_effect":true,"reason":"Cannot reverse."}`,
		`{"production_deployment":false,"reason":"Missing decisions."}`,
	} {
		if got := code(DocumentV2{Role: "other", Title: ReviewRiskTitle, Body: body}); got != "feature_plan_required" {
			t.Errorf("elevated or malformed %s: %s", body, got)
		}
	}
	item.DeploymentPolicy = DeployRequired
	if got := code(routine); got != "feature_plan_required" {
		t.Fatalf("required deployment: %s", got)
	}
}

func TestFeaturePlanRevisionNamesItsRiskBoundary(t *testing.T) {
	item := ItemV2{ID: "feature", Kind: KindFeature, Phase: PhaseAssigned, Cycle: 1,
		GateSnapshotCycle: 1, GateSnapshotAt: time.Unix(1, 0), PlanningGate: true,
		AcceptanceCriteria: "A visible result"}
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
