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

func TestSquadDispatchUsesAdmittedProjectScope(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project := filepath.Join(dir, "project")
	other := filepath.Join(dir, "other")
	for _, path := range []string{project, other} {
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	scope, _ := projects.ResolveScope(project)
	document, _ := json.Marshal(map[string]string{"definition_id": "clawdline.persona.minimal-change", "scope_id": scope.ID})
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
	b := &Broker{Store: st, SquadActorRequired: true}
	for _, tc := range []struct{ name, project, nested, want string }{
		{"same without nested", project, "", ""},
		{"same with nested", project, project, ""},
		{"cross project", other, "", "session_scope_mismatch"},
		{"conflicting nested", project, other, "bad_task"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := json.Marshal(map[string]any{"session_id": "conversation-a", "assistant": "claude", "project_dir": tc.nested})
			p := 1
			claims := []string{}
			record, err := b.admit("11111111-1111-4111-8111-111111111111", draft{Protocol: &p, TaskID: "11111111-1111-4111-8111-111111111111", Assistant: "claude", ProjectDir: tc.project, Title: "Do the thing", Instructions: "Do it", Claims: &claims, Root: (*json.RawMessage)(&root)}, false, false)
			if err == nil {
				err = b.checkSquadDispatchActor(ctx, DispatchRequest{ActorCapability: launch.ActorCapability}, record, "terminal-a")
			}
			if got := refusalCode(err); got != tc.want {
				t.Fatalf("refusal = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}
