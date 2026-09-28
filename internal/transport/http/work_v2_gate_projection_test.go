package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestBoardGatePayloadIsCompactWhileItemPayloadKeepsBoundedDetail(t *testing.T) {
	item := work.ItemV2{ID: "11111111-1111-4111-8111-111111111111", ProjectID: "p", ProjectPath: "/p"}
	authorization := &contract.WorkGateAuthorizationSummary{Kind: "technical_person_override",
		Reason: "The person accepted the frozen candidate.", CreatedAt: 7}
	latest := &contract.WorkGateRoundSummary{ID: "22222222-2222-4222-8222-222222222222",
		CandidateCommit: strings.Repeat("a", 40), CriteriaDigest: strings.Repeat("b", 64),
		State: contract.WorkGateRoundStateTechnicalFailure, CreatedAt: 6}
	compact := contract.WorkGateCompactRead{VerifyGate: true, GateSnapshotCycle: 1,
		LatestRound: latest, CurrentAuthorization: authorization}
	detail := contract.WorkGateDetailRead{Acceptance: contract.WorkGateAcceptance{Criteria: "observable", Version: 1,
		Digest: strings.Repeat("b", 64)}, Compact: compact, RecentRounds: []contract.WorkGateRound{{
		ID: latest.ID, State: latest.State, Result: &contract.WorkGateResult{Summary: "private evidence detail"},
	}}, RecentRoundsTruncated: false}
	server := &Server{}
	listWire := server.workV2ItemOf(workV2ProjectCatalog{}, app.WorkV2View{Item: item, GateCompact: &compact})
	detailWire := server.workV2ItemOf(workV2ProjectCatalog{}, app.WorkV2View{Item: item, Gate: &detail})
	listJSON, err := json.Marshal(listWire)
	if err != nil {
		t.Fatal(err)
	}
	detailJSON, err := json.Marshal(detailWire)
	if err != nil {
		t.Fatal(err)
	}
	var listObject map[string]any
	if err := json.Unmarshal(listJSON, &listObject); err != nil {
		t.Fatal(err)
	}
	verification, ok := listObject["verification"].(map[string]any)
	_, hasRounds := verification["recent_rounds"]
	_, hasAcceptance := verification["acceptance"]
	if !ok || hasRounds || hasAcceptance || strings.Contains(string(listJSON), "private evidence detail") ||
		!strings.Contains(string(listJSON), "current_authorization") {
		t.Fatalf("list gate payload was not compact: %s", listJSON)
	}
	if !strings.Contains(string(detailJSON), "recent_rounds") || !strings.Contains(string(detailJSON), "private evidence detail") {
		t.Fatalf("item gate payload omitted bounded detail: %s", detailJSON)
	}
}
