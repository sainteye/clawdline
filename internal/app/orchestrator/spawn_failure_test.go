package orchestrator

import (
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestSpawnFailureReasonNamesWhatStoppedTheStart(t *testing.T) {
	at := time.Unix(1000, 0)
	cases := []struct {
		name string
		r    Record
		want string
	}{
		{"not failed", Record{State: StateFailure, AwaitingDialogSince: at}, ""},
		{"dialog waiting", Record{State: StateSpawnFailed, AwaitingDialogSince: at}, SpawnFailureDialogUnanswered},
		{"dialog over the briefing", Record{State: StateSpawnFailed, Briefing: &Briefing{HeldSince: at, CoveredAt: at}}, SpawnFailureDialogUnanswered},
		{"restart lost it", Record{State: StateSpawnFailed, Unbriefed: true}, SpawnFailureRestarted},
		{"restart read it held", Record{State: StateSpawnFailed, Briefing: &Briefing{HeldSince: at, ResumedAt: at}}, SpawnFailureRestarted},
		{"held in composer", Record{State: StateSpawnFailed, Briefing: &Briefing{HeldSince: at}}, SpawnFailureBriefingNotSubmitted},
		{"could not open", Record{State: StateSpawnFailed, SpawnError: "open failed"}, SpawnFailureCouldNotOpen},
	}
	for _, c := range cases {
		if got := SpawnFailureReason(&c.r); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestABotWorkLinkCarriesTheSpawnFailureReason(t *testing.T) {
	owner := &AssistantOwner{Fence: pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}}, TurnID: "turn"}
	failed := AssistantWorkSnapshotOf(Record{ID: "t", State: StateSpawnFailed, AwaitingDialogSince: time.Unix(1, 0), AssistantOwner: owner})
	if failed.Link.State != pa.WorkFailed || failed.Link.FailureReason != SpawnFailureDialogUnanswered {
		t.Fatalf("spawn_failed link: %+v", failed.Link)
	}
	ran := AssistantWorkSnapshotOf(Record{ID: "t", State: StateFailure, AssistantOwner: owner})
	if ran.Link.FailureReason != "" {
		t.Fatalf("a task that ran carries a start failure: %+v", ran.Link)
	}
}
