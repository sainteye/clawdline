package squad

import (
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

func TestSquadBuiltinsAndPolicy(t *testing.T) {
	c := Builtins()
	if len(c.Definitions) != len(persona.IDs()) || len(c.Skills) != len(builtinSkillSpecs) || len(c.Skills) != 26 {
		t.Fatalf("builtins: %d definitions, %d skills", len(c.Definitions), len(c.Skills))
	}
	skills := map[string]Skill{}
	for _, s := range c.Skills {
		if !s.Builtin || s.SkillID == "" || !strings.HasPrefix(s.Version, "sha256:") ||
			len(s.Digest) != 64 || s.Source == "" || s.License == "" || s.Content == "" ||
			s.Icon.Accent == "" || len(s.Icon.Cells) != 7 || skills[s.SkillID].SkillID != "" {
			t.Fatalf("incomplete or duplicate skill: %+v", s)
		}
		for _, install := range []string{"npx ", "npm install", "pip install", "/plugin ", "curl ", "brew install"} {
			if strings.Contains(s.Content, install) {
				t.Errorf("skill %s tells a role to install or fetch something: %q", s.SkillID, install)
			}
		}
		if !strings.Contains(s.Content, "Use this skill") {
			t.Errorf("skill %s has no sentence saying when it applies", s.SkillID)
		}
		for _, projectSpecific := range []string{"Clawdline", "CloudGate", "BUILD.json", "tools/heavy.sh"} {
			if strings.Contains(s.Content, projectSpecific) {
				t.Errorf("current skill %s assumes the host project is %s", s.SkillID, projectSpecific)
			}
		}
		skills[s.SkillID] = s
	}
	carried := map[string]bool{}
	ids := map[string]bool{}
	for _, d := range c.Definitions {
		want := builtinRoleSkills[d.ShortID]
		if !d.Builtin || d.ShortID == "" || d.DefinitionID != BuiltinID(d.ShortID) ||
			!strings.HasPrefix(d.Version, "sha256:") || len(d.Digest) != 64 ||
			d.Body == "" || d.Source == "" || d.License == "" || d.Skills == nil ||
			len(d.Skills) > MaxBuiltinRoleSkills || len(d.Skills) != len(want) {
			t.Fatalf("incomplete builtin: %+v", d)
		}
		for i, ref := range d.Skills {
			if ref.ID != "clawdline.skill."+want[i] {
				t.Fatalf("%s skill %d is %s, want %s in the stated order", d.ShortID, i, ref.ID, want[i])
			}
			if skill := skills[ref.ID]; skill.SkillID == "" || skill.Version != ref.Version || !ref.Enabled {
				t.Fatalf("unresolved builtin skill %s: %+v", d.ShortID, ref)
			}
			carried[ref.ID] = true
		}
		earlier := EarlierBuiltinDefinitions(d)
		if len(d.Skills) == 0 && earlier != nil {
			t.Fatalf("a role with no skills has earlier skill versions: %s", d.ShortID)
		}
		for _, previous := range earlier {
			if previous.Version == d.Version || len(previous.Skills) > len(d.Skills) || previous.Body != d.Body {
				t.Fatalf("earlier built-in definition %s = %+v", d.ShortID, previous)
			}
			for _, ref := range previous.Skills {
				if skills[ref.ID].Version != ref.Version {
					if _, ok := HistoricalBuiltinSkill(ref.ID, ref.Version); !ok {
						t.Fatalf("earlier skill %s of %s cannot resolve", ref.ID, d.ShortID)
					}
				}
			}
		}
		if len(d.Skills) == 0 {
			if previous := PreSkillBuiltinVersion(d); previous.Version != d.Version {
				t.Fatalf("unaffected builtin changed version %s", d.ShortID)
			}
		}
		if id, ok := ResolveID(d.ShortID); !ok || id != d.DefinitionID {
			t.Fatalf("alias %s => %q %t", d.ShortID, id, ok)
		}
		ids[d.DefinitionID] = true
	}
	for id := range skills {
		if !carried[id] {
			t.Errorf("built-in skill %s is carried by no role", id)
		}
	}
	if len(ids) != len(persona.IDs()) || ValidCustomID(BuiltinID("backend")) || !ValidCustomID("example.persona.writer") {
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
