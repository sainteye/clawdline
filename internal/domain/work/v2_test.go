package work

import "testing"

func TestV2KindsAndAreas(t *testing.T) {
	for _, k := range []Kind{KindFeature, KindIssue, KindEpic, KindRefactor, KindPlan} {
		if !k.Valid() {
			t.Fatalf("%s is not valid", k)
		}
	}
	if !KindFeature.Executable() || !KindIssue.Executable() || KindPlan.Executable() {
		t.Fatal("the executable boundary changed")
	}
	cases := []struct {
		item ItemV2
		want string
	}{
		{ItemV2{Kind: KindPlan, Phase: PhaseCreated}, "planning"},
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
