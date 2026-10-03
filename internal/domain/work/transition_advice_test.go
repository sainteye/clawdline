package work

import (
	"strings"
	"testing"
)

// TestTransitionAdviceMatchesAgentTransition holds phaseSteps to
// AgentTransition's switch: every step it advises is one AgentTransition
// takes with that step's evidence, every move AgentTransition takes with all
// evidence is a step it advises, and each refusal names the legal next phases
// and one `item phase` command.
func TestTransitionAdviceMatchesAgentTransition(t *testing.T) {
	phases := []Phase{PhaseAssigned, PhaseImplementing, PhaseVerifying, PhaseMerging, PhaseDeploying, PhaseDone}
	for _, policy := range []DeploymentPolicy{DeployRequired, DeployNotRequired, DeployAgentDecides} {
		for _, gate := range []bool{true, false} {
			for _, from := range phases[:5] {
				i := ItemV2{ID: "item-1", OwnerSession: "s", Phase: from, VerifyGate: gate, DeploymentPolicy: policy}
				advised := map[Phase]bool{}
				for _, s := range phaseSteps(i) {
					advised[s.Next] = true
					f := s.Flags
					if err := AgentTransition(i, s.Next, strings.Contains(f, "--verification"),
						strings.Contains(f, "--commit"), strings.Contains(f, "--deployment "),
						strings.Contains(f, "--no-deployment-reason")); err != nil {
						t.Errorf("%s -> %s (gate %v, %s) is advised with %q and refused: %v", from, s.Next, gate, policy, f, err)
					}
				}
				for _, next := range phases {
					err := AgentTransition(i, next, true, true, true, true)
					if err == nil && !advised[next] {
						t.Errorf("%s -> %s (gate %v, %s) is legal and not advised", from, next, gate, policy)
					}
					if err == nil || !strings.Contains(err.Error(), "invalid_transition") {
						continue
					}
					if none := AgentTransition(i, next, false, false, false, false); none != nil {
						msg := none.Error()
						if !strings.Contains(msg, "legal next phases") || !strings.Contains(msg, "`clawdline item phase item-1 ") {
							t.Errorf("%s -> %s refusal names no legal phase or command: %s", from, next, msg)
						}
					}
				}
				if err := AgentTransition(i, PhaseDone, false, false, false, false); from == PhaseDeploying && err != nil {
					msg := err.Error()
					want := map[DeploymentPolicy]string{DeployRequired: "without --deployment.",
						DeployNotRequired: "without --no-deployment-reason.", DeployAgentDecides: "without --deployment or --no-deployment-reason."}[policy]
					if !strings.Contains(msg, want) {
						t.Errorf("deploying -> done (%s) does not name its missing evidence %q: %s", policy, want, msg)
					}
				}
			}
		}
	}
	i := ItemV2{ID: "item-1", OwnerSession: "s", Phase: PhaseMerging, VerifyGate: true, DeploymentPolicy: DeployAgentDecides}
	msg := AgentTransition(i, PhaseDeploying, false, false, false, false).Error()
	for _, want := range []string{"without --commit/--target/--remote or --no-landing-reason",
		"`clawdline item phase item-1 deploying --commit <sha> --target <branch> --remote <remote>`"} {
		if !strings.Contains(msg, want) {
			t.Errorf("merging -> deploying refusal lacks %q: %s", want, msg)
		}
	}
}
