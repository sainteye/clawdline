package projectassistant

import (
	"strings"
	"testing"
	"time"
)

func TestBotActorRejectsForgedScopesAndHumanOperations(t *testing.T) {
	f := testFence()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	actor := AssistantExecutionActor{CapabilityID: "cap-a", Fence: f, Operations: []ActorOperation{DispatchWork, ReadWork, ProposeWork, PublishMessage}, ExpiresAt: now.Add(time.Minute)}
	for _, operation := range actor.Operations {
		if err := CheckActor(actor, f, operation, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []ActorOperation{"approve", "accept_proposal", "complete_board_step", "terminal_send", "machine_admin"} {
		forged := actor
		forged.Operations = append([]ActorOperation{operation}, actor.Operations...)
		if Code(CheckActor(forged, f, operation, now)) != ScopeDenied {
			t.Fatalf("human or terminal operation accepted: %s", operation)
		}
	}
	for _, mutate := range []func(*AssistantExecutionActor){
		func(a *AssistantExecutionActor) { a.Fence.Key.BotID = "bot-other" },
		func(a *AssistantExecutionActor) { a.Fence.Key.MachineID = "machine-other" },
		func(a *AssistantExecutionActor) { a.Fence.Project.CanonicalID = "repo-other" },
		func(a *AssistantExecutionActor) { a.Fence.ConversationID = "chat-other" },
	} {
		forged := actor
		mutate(&forged)
		if Code(CheckActor(forged, f, DispatchWork, now)) != ScopeDenied {
			t.Fatal("cross-scope capability accepted")
		}
	}
	late := actor
	late.Fence.Generation++
	if Code(CheckActor(late, f, DispatchWork, now)) != StaleGeneration {
		t.Fatal("old actor accepted")
	}
	late = actor
	late.Revoked = true
	if Code(CheckActor(late, f, DispatchWork, now)) != Revoked {
		t.Fatal("revoked actor accepted")
	}
	late = actor
	late.ExpiresAt = now
	if Code(CheckActor(late, f, DispatchWork, now)) != Expired {
		t.Fatal("expired actor accepted")
	}
	late = actor
	late.CapabilityID = ""
	if Code(CheckActor(late, f, DispatchWork, now)) != ScopeDenied {
		t.Fatal("anonymous actor accepted")
	}
}
func TestWorkRequiresScopedActorAndDeclaredClaims(t *testing.T) {
	f := testFence()
	now := time.Now()
	request := WorkRequest{Actor: AssistantExecutionActor{CapabilityID: "cap-a", Fence: f, Operations: []ActorOperation{DispatchWork}, ExpiresAt: now.Add(time.Minute)}, Command: Command{Key: f.Key, Operation: "dispatch", IdempotencyKey: "dispatch-a", ContentHash: strings.Repeat("a", 64)}, TurnID: "turn-a", Brief: "Synthetic isolated work", Claims: []string{"example.go"}}
	if err := CheckWorkRequest(request, f, now); err != nil {
		t.Fatal(err)
	}
	request.Claims = nil
	if Code(CheckWorkRequest(request, f, now)) != Invalid {
		t.Fatal("unclaimed work accepted")
	}
	request.Claims = []string{"example.go"}
	request.Command.Key.BotID = "other"
	if Code(CheckWorkRequest(request, f, now)) != ScopeDenied {
		t.Fatal("work command escaped actor scope")
	}
}
