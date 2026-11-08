package squad

import (
	"testing"
)

// The released definitions of the roles whose default Skills changed on
// 2026-11-08, read from the catalog at c203b1b5e before the change. They are
// literals, not hashes derived from the new catalog, so a reconstruction that
// drifts from what was released fails here.
var beforeSeveralSkills = map[string]string{
	"technical-writer": "sha256:a02dc5b7c926693e8cf135ec930b6b823ff83160077961ef4b0528eb684dd275",
	"code-reviewer":    "sha256:6324291052765bd9000aca77475a041422bf82bfe8837a33282fd300b1695edf",
	"minimal-change":   "sha256:584a0a46ab4e63c7752e366023763e4966acf5426db76e8ebe1e0a97e97fe0c7",
	"email":            "sha256:6830685782307da4adac245084a05abf6a1340fe0336288667b847265aaa4a81",
	"pr":               "sha256:886cbcc50e4183e9d5d810688f3193c3b2ecfde185b654db6842a7bd4f15829b",
	"devrel":           "sha256:cbb258bf9bec1995cca1d64b55b84357783ed695afebcc16708515d5cf7a43a6",
}

// Released Skill versions a Project override or a Session snapshot may pin.
// A role-mapping change must not move them.
var releasedSkillVersions = map[string]string{
	"clawdline.skill.humanize-prose":             "sha256:318c8d86e7557d633b4c6a2e2f30bbb5bb319f825158933974da4b03e4cedbd8",
	"clawdline.skill.documentation-writer":       "sha256:60a56273fc84cd68398d3e0885904d135dfdb3ed7ff4613dd323360839e7154e",
	"clawdline.skill.two-axis-code-review":       "sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d",
	"clawdline.skill.smallest-sufficient-change": "sha256:b0a174e89e9c2dad7e3d5e9f00f8425177caa5f5fc09cff3aca4ec121ab22bd3",
	"clawdline.skill.golang-concurrency":         "sha256:b34e8fa6b1c083c12688d73801e17bc544ea909128cdd8b19de63ce3e63a248f",
}

func TestBuiltinRolesCarrySeveralDefaultSkills(t *testing.T) {
	c := Builtins()
	defs := map[string]Definition{}
	for _, d := range c.Definitions {
		defs[d.ShortID] = d
	}
	want := map[string][]string{
		"technical-writer": {"clawdline.skill.documentation-writer", "clawdline.skill.humanize-prose"},
		"code-reviewer":    {"clawdline.skill.two-axis-code-review", "clawdline.skill.ponytail-review"},
		"minimal-change":   {"clawdline.skill.smallest-sufficient-change", "clawdline.skill.systematic-debugging"},
		"content-writer":   {"clawdline.skill.humanize-prose"},
		"email":            {"clawdline.skill.humanize-prose"},
		"pr":               {"clawdline.skill.humanize-prose"},
		"devrel":           {"clawdline.skill.humanize-prose"},
	}
	for role, ids := range want {
		got := defs[role].Skills
		if len(got) != len(ids) {
			t.Fatalf("%s carries %+v, want %v", role, got, ids)
		}
		for i := range ids {
			if got[i].ID != ids[i] || !got[i].Enabled {
				t.Fatalf("%s skill %d = %+v, want %s", role, i, got[i], ids[i])
			}
		}
	}
	// One Skill, five roles: the catalog lists it once, with one icon and one
	// version, and every role points at that same version.
	var shared []Skill
	for _, s := range c.Skills {
		if s.SkillID == "clawdline.skill.humanize-prose" {
			shared = append(shared, s)
		}
	}
	if len(shared) != 1 {
		t.Fatalf("humanize-prose is listed %d times", len(shared))
	}
	contentWriter := defs["content-writer"]
	if shared[0].Icon.Accent != contentWriter.Icon.Accent {
		t.Errorf("the shared skill's icon moved from content-writer's %s to %s", contentWriter.Icon.Accent, shared[0].Icon.Accent)
	}
	for _, role := range []string{"content-writer", "technical-writer", "email", "pr", "devrel"} {
		found := false
		for _, ref := range defs[role].Skills {
			found = found || (ref.ID == shared[0].SkillID && ref.Version == shared[0].Version)
		}
		if !found {
			t.Errorf("%s does not reference the shared skill's one version", role)
		}
	}
	for id, version := range releasedSkillVersions {
		var current Skill
		for _, s := range c.Skills {
			if s.SkillID == id {
				current = s
			}
		}
		if current.Version != version {
			t.Errorf("%s moved from released %s to %s when only role mapping changed", id, version, current.Version)
		}
	}
}

func TestDefinitionsReleasedBeforeSeveralSkillsStillResolve(t *testing.T) {
	defs := map[string]Definition{}
	for _, d := range Builtins().Definitions {
		defs[d.ShortID] = d
	}
	for role, version := range beforeSeveralSkills {
		t.Run(role, func(t *testing.T) {
			current := defs[role]
			if current.Version == version {
				t.Fatalf("%s did not get a new version for its new default skills", role)
			}
			var resolved *Definition
			for _, previous := range EarlierBuiltinDefinitions(current) {
				if previous.Version == version {
					resolved = &previous
					break
				}
			}
			if resolved == nil {
				t.Fatalf("the released %s %s no longer resolves", role, version)
			}
			if resolved.Digest != builtinDefinitionDigest(*resolved) || resolved.Body != current.Body {
				t.Fatalf("released %s resolved with different content", role)
			}
			// Every Skill the released definition pinned still launches.
			for _, ref := range resolved.Skills {
				if releasedSkillVersions[ref.ID] != ref.Version {
					t.Errorf("released %s pinned %s %s, which is not a released version", role, ref.ID, ref.Version)
				}
			}
		})
	}
}

// The bound is enforced where the table is compiled in: a fifth default for one
// role, or a name that is not a Skill, stops the catalog from being built.
func TestABuiltinRolePastTheBoundDoesNotBuild(t *testing.T) {
	saved := builtinRoleSkills["code-reviewer"]
	defer func() { builtinRoleSkills["code-reviewer"] = saved }()
	for name, list := range map[string][]string{
		"five":     {"two-axis-code-review", "ponytail-review", "humanize-prose", "decision-record", "write-spec"},
		"unknown":  {"two-axis-code-review", "no-such-skill"},
		"repeated": {"two-axis-code-review", "two-axis-code-review"},
	} {
		t.Run(name, func(t *testing.T) {
			builtinRoleSkills["code-reviewer"] = list
			defer func() {
				if recover() == nil {
					t.Fatalf("a catalog with %v built", list)
				}
			}()
			Builtins()
		})
	}
}
