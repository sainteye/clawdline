package store

import (
	"bytes"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestPublicGateReadsDoNotCarryLocalCandidatePaths(t *testing.T) {
	const private = "/private/recognizable/checker-path"
	round := contract.WorkGateRound{Candidate: contract.WorkGateCandidateReceipt{
		Repository: private + "/repository", Worktree: private + "/worktree",
		Branch: "feature", Commit: "commit", Tree: "tree",
	}}
	public := publicWorkGateRound(round)
	if public.Candidate.Repository != "" || public.Candidate.Worktree != "" ||
		public.Candidate.Commit != round.Candidate.Commit || public.Candidate.Tree != round.Candidate.Tree {
		t.Fatalf("public candidate = %+v", public.Candidate)
	}
	export := workGateExportDocument(work.ItemV2{ID: "item", Version: 7}, []contract.WorkGateRound{round})
	if bytes.Contains(export.Document, []byte(private)) {
		t.Fatalf("export disclosed local path: %s", export.Document)
	}
	if !bytes.Contains(export.Document, []byte(`"commit":"commit"`)) {
		t.Fatalf("export lost immutable candidate identity: %s", export.Document)
	}
}
