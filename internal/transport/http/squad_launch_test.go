package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestSquadSessionRoleMatchesThePresentationCatalog(t *testing.T) {
	f := newSquadFixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ definition, want string }{
		{"clawdline.persona.architect", "architect"},
		{"personal.persona.editor", "personal.persona.editor"},
	} {
		document, _ := json.Marshal(map[string]string{"definition_id": tc.definition, "scope_id": "global"})
		launch, err := f.s.store.PrepareSquadLaunch(ctx, document)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.store.RecordSquadTerminal(ctx, launch.ID, "%1"); err != nil {
			t.Fatal(err)
		}
		row := session.Session{ID: "%1", SquadLaunchID: launch.ID}
		if got := f.s.sessionSquadRole(ctx, row); got != tc.want {
			t.Fatalf("role=%q want %q", got, tc.want)
		}
		row.ID = "%2"
		if got := f.s.sessionSquadRole(ctx, row); got != "" {
			t.Fatalf("wrong terminal inherited %q", got)
		}
	}
}
