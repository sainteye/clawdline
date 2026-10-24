package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestOwnedContextUsesPublishedRoleSkillsAndProjectMemory(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	place := projects.Place{Path: t.TempDir()}
	resolutions := 0
	s := Starter{Squad: st, SquadDir: t.TempDir(),
		ResolveSquadSnapshot: func(_ context.Context, id, path string) (json.RawMessage, error) {
			resolutions++
			if id != "backend" || path != place.Path {
				t.Fatal("role preparation changed Project or persona")
			}
			return json.Marshal(map[string]any{
				"definition_id": "clawdline.persona.backend", "scope_id": "project-test",
				"definition": map[string]string{"version": "1", "body": "Original backend instructions"},
				"handbook":   map[string]string{"text": "Original shared handbook"},
				"skills": []any{map[string]any{"id": "example.skill.source", "version": "1", "enabled": true,
					"name": map[string]string{"en": "Source check"}, "purpose": map[string]string{"en": "Check sources"}, "content": "Read the actual source before deciding."}},
			})
		},
		Memory: func(path string) string {
			if path != place.Path {
				t.Fatal("memory from another Project")
			}
			return "Original Project memory"
		},
		Language: func(assistant string) string {
			if assistant != projects.AssistantAntigravity {
				t.Fatal("wrong language provider")
			}
			return "zh-Hant-TW"
		},
	}
	got, err := s.PrepareOwnedContext(context.Background(), place, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if got.LaunchID == "" || got.Persona != "backend" || got.Context.Validate() != nil {
		t.Fatal("missing immutable role identity")
	}
	for _, want := range []string{"Original backend instructions", "Original shared handbook", "example.skill.source", "Original Project memory", "zh-Hant-TW"} {
		if !strings.Contains(got.Context.Instructions(), want) {
			t.Fatalf("context missing %q", want)
		}
	}
	if data, err := os.ReadFile(got.Files.CapabilityPath); err != nil || strings.Contains(got.Context.Instructions(), string(data)) {
		t.Fatal("missing capability or secret included in instructions")
	}
	reused, err := s.OwnedContextFromPublished(context.Background(), place, got.Persona, got.LaunchID, got.Files)
	if err != nil || resolutions != 1 || reused.Context.Instructions() != got.Context.Instructions() || reused.LaunchID != got.LaunchID || reused.Files.CapabilityPath != got.Files.CapabilityPath {
		t.Fatal("published role was prepared again or changed", err)
	}
	plain, err := s.PrepareOwnedContext(context.Background(), place, "")
	if err != nil || plain.LaunchID != "" || plain.Files.CapabilityPath != "" || !strings.Contains(plain.Context.Instructions(), "Original Project memory") {
		t.Fatal("no-role memory acquired a role or disappeared", err)
	}
	if _, err := (Starter{}).PrepareOwnedContext(context.Background(), place, "backend"); err == nil {
		t.Fatal("requested role silently launched without a snapshot")
	}
}
