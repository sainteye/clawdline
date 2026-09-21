package analytics

import (
	"testing"
	"time"
)

func TestProjectWorktreesSaysFeatureAttributionWasNotMeasured(t *testing.T) {
	got, err := Worktrees(WorktreeQuery{Project: "project", Usage: Query{}}, []Row{{
		IntervalKey: "one",
		ProjectKey:  "/work/project",
		WorkingDir:  "/work/project",
	}}, time.Unix(1_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got["status"] != "not_measured" {
		t.Fatalf("status = %v", got["status"])
	}
	read := got["read"].(map[string]any)
	if read["featureRowsStatus"] != "not_measured" {
		t.Fatalf("featureRowsStatus = %v", read["featureRowsStatus"])
	}
	if _, exists := read["featureRows"]; exists {
		t.Fatalf("an unperformed attribution read reported a row count: %v", read)
	}
}

func TestUnconfiguredClassifierDoesNotClaimACompleteReviewRead(t *testing.T) {
	got := features([]Row{{IntervalKey: "one"}})
	if got["status"] != "not_measured" {
		t.Fatalf("feature status = %v", got["status"])
	}
	evidence := got["roleEvidence"].(obj)
	receipts := evidence["reviewReceipts"].(obj)
	if receipts["status"] != "unconfigured" {
		t.Fatalf("review receipt status = %v", receipts["status"])
	}
	if _, exists := receipts["read"]; exists {
		t.Fatalf("an unperformed review read reported a row count: %v", receipts)
	}
}
