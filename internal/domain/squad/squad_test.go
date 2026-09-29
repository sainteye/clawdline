package squad

import (
	"errors"
	"strings"
	"testing"
)

func TestSquadBuiltinsAndPolicy(t *testing.T) {
	c := Builtins()
	if len(c.Definitions) != 42 || len(c.Skills) != 0 {
		t.Fatalf("builtins: %d definitions, %d skills", len(c.Definitions), len(c.Skills))
	}
	ids := map[string]bool{}
	for _, d := range c.Definitions {
		if !d.Builtin || d.ShortID == "" || d.DefinitionID != BuiltinID(d.ShortID) ||
			!strings.HasPrefix(d.Version, "sha256:") || len(d.Digest) != 64 ||
			d.Body == "" || d.Source == "" || d.License == "" || d.Skills == nil || len(d.Skills) != 0 {
			t.Fatalf("incomplete builtin: %+v", d)
		}
		if id, ok := ResolveID(d.ShortID); !ok || id != d.DefinitionID {
			t.Fatalf("alias %s => %q %t", d.ShortID, id, ok)
		}
		ids[d.DefinitionID] = true
	}
	if len(ids) != 42 || ValidCustomID(BuiltinID("backend")) || !ValidCustomID("example.persona.writer") {
		t.Fatal("definition namespace collision")
	}
	e := Effective{ScopeID: "project-a", Personas: []EffectivePersona{
		{DefinitionID: BuiltinID("backend"), AutoAssign: Field[bool]{Value: true}},
		{DefinitionID: BuiltinID("security"), AutoAssign: Field[bool]{Value: false}},
	}}
	if _, err := Candidates(Actor{DefinitionID: BuiltinID("product-manager"), ScopeID: "project-a"}, e); !errors.Is(err, ErrActorUnverified) {
		t.Fatalf("unverified actor: %v", err)
	}
	if _, err := Candidates(Actor{Verified: true, DefinitionID: BuiltinID("backend"), ScopeID: "project-a"}, e); !errors.Is(err, ErrActorNotManager) {
		t.Fatalf("non-manager: %v", err)
	}
	if _, err := Candidates(Actor{Verified: true, DefinitionID: BuiltinID("architect"), ScopeID: "project-b"}, e); !errors.Is(err, ErrActorUnverified) {
		t.Fatalf("cross-project actor: %v", err)
	}
	got, err := Candidates(Actor{Verified: true, DefinitionID: BuiltinID("architect"), ScopeID: "project-a"}, e)
	if err != nil || len(got) != 1 || got[0] != BuiltinID("backend") {
		t.Fatalf("candidates = %v, %v", got, err)
	}
}

func TestSquadPresenceAndWholeHandbook(t *testing.T) {
	blank := ""
	global := "global handbook"
	field := Resolve("definition handbook", &global, 2, &blank, 3)
	if field.Value != "" || field.Source != "project" || !field.Present || field.Version != 3 {
		t.Fatalf("blank override = %+v", field)
	}
	field = Resolve("definition handbook", &global, 2, (*string)(nil), 0)
	if field.Value != global || field.Source != "global" || field.Version != 2 {
		t.Fatalf("inheritance = %+v", field)
	}
	field = Resolve("definition handbook", (*string)(nil), 0, (*string)(nil), 0)
	if field.Value != "definition handbook" || field.Source != "default" || field.Present {
		t.Fatalf("default = %+v", field)
	}
}
