package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestSquadDispatchRequiresBoundActorAndHonorsDisabledRole(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	scope, ok := projects.ResolveScope(project)
	if !ok {
		t.Fatal("project scope unavailable")
	}
	document, err := json.Marshal(map[string]string{
		"definition_id": "clawdline.persona.product-manager", "scope_id": scope.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := st.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSquadTerminal(ctx, launch.ID, "terminal-a"); err != nil {
		t.Fatal(err)
	}
	if err := st.BindSquadConversation(ctx, launch.ID, "conversation-a"); err != nil {
		t.Fatal(err)
	}
	b := &Broker{Store: st, SquadActorRequired: true,
		SquadAutoAssignable: func(_ context.Context, persona, path string) (bool, error) {
			if persona != "frontend" || path != project {
				t.Fatalf("unexpected candidate: %q %q", persona, path)
			}
			return false, nil
		},
	}
	record := Record{Persona: "frontend", ProjectDir: project,
		Root: &RootRef{SessionID: "conversation-a", ProjectDir: project}}
	check := func(capability, terminal, conversation, wantCode string) {
		t.Helper()
		record.Root.SessionID = conversation
		err := b.checkSquadDispatchActor(ctx, DispatchRequest{ActorCapability: capability}, record, terminal)
		ref, ok := err.(Refusal)
		if !ok || ref.Code != wantCode {
			t.Fatalf("actor refusal = %v; want %s", err, wantCode)
		}
	}
	check("", "terminal-a", "conversation-a", "session_actor_required")
	check(launch.ActorCapability, "terminal-b", "conversation-a", "session_actor_required")
	check(launch.ActorCapability, "terminal-a", "conversation-b", "session_actor_required")
	check(launch.ActorCapability, "terminal-a", "conversation-a", "persona_disabled_for_auto_assignment")

	record.Persona = ""
	record.Root.SessionID = "conversation-a"
	if err := b.checkSquadDispatchActor(ctx, DispatchRequest{ActorCapability: launch.ActorCapability}, record, "terminal-a"); err != nil {
		t.Fatalf("dispatch without role candidate: %v", err)
	}
	if err := b.checkSquadDispatchActor(ctx, DispatchRequest{}, record, "legacy-terminal"); err != nil {
		t.Fatalf("legacy Session dispatch: %v", err)
	}
	pending, err := st.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSquadTerminal(ctx, pending.ID, "pending-terminal"); err != nil {
		t.Fatal(err)
	}
	check("", "pending-terminal", "conversation-a", "session_actor_required")
}
