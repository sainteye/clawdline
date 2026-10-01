package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestSquadObservationRecoversOpenedTerminalWithoutGuessingIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	document := json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test","definition":{"version":"1","body":"Architect role"}}`)
	launch, err := st.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squadfiles.Publish(dir, launch.ID, launch.SnapshotID, launch.ActorCapability, document)
	if err != nil {
		t.Fatal(err)
	}
	if err := squadfiles.RecordTerminal(files, "terminal-a"); err != nil {
		t.Fatal(err)
	}
	wrong := session.Inventory{Sessions: []session.Session{{ID: "terminal-a", Assistant: session.AssistantCodex,
		ConversationID: "conversation-a", SquadLaunchID: "some-other-launch-id"}}}
	if err := ReconcileSquadLaunches(ctx, st, dir, wrong); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.AuthenticateSquadActor(ctx, launch.ActorCapability); err != nil || ok {
		t.Fatalf("wrong process bound actor: %t, %v", ok, err)
	}
	good := session.Inventory{Sessions: []session.Session{{ID: "terminal-a", Assistant: session.AssistantCodex,
		ConversationID: "conversation-a", SquadLaunchID: launch.ID}}}
	if err := ReconcileSquadLaunches(ctx, st, dir, good); err != nil {
		t.Fatal(err)
	}
	if actor, ok, err := st.AuthenticateSquadActor(ctx, launch.ActorCapability); err != nil || !ok ||
		actor.ConversationID != "conversation-a" || actor.TerminalID != "terminal-a" {
		t.Fatalf("recovered actor = %+v, %t, %v", actor, ok, err)
	}
}

func TestSquadObservationRecoversFromUniqueProcessWithoutTerminalReceipt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	document := json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test"}`)
	launch, err := st.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	inv := session.Inventory{Sessions: []session.Session{{ID: "terminal-a", Assistant: session.AssistantCodex,
		ConversationID: "conversation-a", SquadLaunchID: launch.ID}}}
	if err := ReconcileSquadLaunches(ctx, st, dir, inv); err != nil {
		t.Fatal(err)
	}
	if actor, ok, err := st.AuthenticateSquadActor(ctx, launch.ActorCapability); err != nil || !ok || actor.TerminalID != "terminal-a" {
		t.Fatalf("unrecovered actor = %+v, %t, %v", actor, ok, err)
	}
}

func TestSquadObservationFollowsConversationChangeInSameLaunch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	launch, err := st.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSquadTerminal(ctx, launch.ID, "terminal-a"); err != nil {
		t.Fatal(err)
	}
	observe := func(conversation string) {
		t.Helper()
		inv := session.Inventory{Sessions: []session.Session{{ID: "terminal-a", Assistant: session.AssistantCodex,
			ConversationID: conversation, SquadLaunchID: launch.ID}}}
		if err := ReconcileSquadLaunches(ctx, st, dir, inv); err != nil {
			t.Fatal(err)
		}
	}
	observe("startup-conversation")
	observe("assigned-conversation")
	actor, ok, err := st.AuthenticateSquadActor(ctx, launch.ActorCapability)
	if err != nil || !ok || actor.ConversationID != "assigned-conversation" {
		t.Fatalf("actor after provider identity changed = %+v, %t, %v", actor, ok, err)
	}
}
