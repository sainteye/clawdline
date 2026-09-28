package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestCompactGateProjectionListsATechnicalFailureWithoutAResult(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	at := time.Unix(1_790_600_000, 0)
	item := work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindFeature, Title: "gate failure", Description: "gate failure", Phase: work.PhaseVerifying,
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at, Cycle: 1, Version: 1}
	roundID := "20000000-0000-4000-8000-000000000001"
	attemptID := "30000000-0000-4000-8000-000000000001"
	round := contract.WorkGateRound{ID: roundID, ItemID: item.ID, Cycle: item.Cycle,
		Acceptance:     contract.WorkGateAcceptance{Criteria: "prove it", Version: 1, Digest: strings.Repeat("a", 64)},
		Candidate:      contract.WorkGateCandidateReceipt{Commit: strings.Repeat("b", 40), Tree: strings.Repeat("c", 40)},
		CheckerPersona: "reality-checker", State: contract.WorkGateRoundStateQueued,
		Attempts: []contract.WorkGateAttempt{{ID: attemptID, RoundID: roundID, Attempt: 0,
			State: contract.WorkGateAttemptStateQueued, CreatedAt: at.Unix(), UpdatedAt: at.Unix()}},
		CreatedAt: at.Unix(), UpdatedAt: at.Unix()}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		if err := tx.CreateWorkGateRound(round, strings.Repeat("d", 40)); err != nil {
			return err
		}
		return tx.FinishWorkGateAttemptAndRound(roundID, attemptID, "provider unavailable",
			contract.WorkGateRoundStateTechnicalFailure, at.Add(time.Second))
	}); err != nil {
		t.Fatal(err)
	}

	compact, err := s.WorkGateCompactDetails(ctx, []work.ItemV2{item})
	if err != nil {
		t.Fatalf("technical failure disappeared from list: %v", err)
	}
	latest := compact[item.ID].LatestRound
	if latest == nil || latest.State != contract.WorkGateRoundStateTechnicalFailure || latest.Verdict != "" {
		t.Fatalf("latest round = %+v", latest)
	}
}
