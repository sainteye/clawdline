package squad

import (
	"strings"
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

// The released definitions of the design and frontend roles before their
// reviewed upstream Skills were added, read from the catalog at 16cfd8960.
// The last three had no default Skill.
var beforeDesignSkills = map[string]string{
	"ui-designer":    "sha256:d2fc6d2f7c0d1fbb624ebe8dfe6ffa216c7d2924714b2881ab82f53daef09978",
	"frontend":       "sha256:abc387bcedf75bb5f69fea7ff8d3986b2e54ae12b0d93276feb01ebe178dd222",
	"accessibility":  "sha256:96b116bb7dfc86849237c409c7a646dede4e4ff96dae2a2b756a763fe12dfbc1",
	"ui-finish-gate": "sha256:261df8b77b77a681d3e94276b52bf4e8a161e98ef82aa5e5b76b6227df3259da",
	"ux-architect":   "sha256:53289efeaa46b781d3f4de7396af0bf6453f1fe2339c39140b99101d72aebe13",
	"brand-guardian": "sha256:99813f20f764303228d1113064c8be3be9e0fdc1cc081d787f079a71feb90be5",
}

// Released Skill versions a Project override or a Session snapshot may pin.
// A role-mapping change must not move them.
var releasedSkillVersions = map[string]string{
	"clawdline.skill.humanize-prose":             "sha256:318c8d86e7557d633b4c6a2e2f30bbb5bb319f825158933974da4b03e4cedbd8",
	"clawdline.skill.documentation-writer":       "sha256:60a56273fc84cd68398d3e0885904d135dfdb3ed7ff4613dd323360839e7154e",
	"clawdline.skill.two-axis-code-review":       "sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d",
	"clawdline.skill.smallest-sufficient-change": "sha256:b0a174e89e9c2dad7e3d5e9f00f8425177caa5f5fc09cff3aca4ec121ab22bd3",
	"clawdline.skill.golang-concurrency":         "sha256:b34e8fa6b1c083c12688d73801e17bc544ea909128cdd8b19de63ce3e63a248f",
	"clawdline.skill.frontend-design":            "sha256:79bcf617093ac4bfbcd1462ebccd8a0cf143789edcecca3a2f9c440f8e93ed38",
	"clawdline.skill.frontend-component-build":   "sha256:951e6cf0254f5aa7731d299911993fc0120751523314f45628e562ed3cc730f9",
	"clawdline.skill.accessibility-audit":        "sha256:13d1d0b4c68759abbe066b0f7f2229f6821f1a59025daf55d28ce50a51340192",
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
		"content-writer":   {"clawdline.skill.humanize-prose", "clawdline.skill.copywriting"},
		"email":            {"clawdline.skill.humanize-prose"},
		"pr":               {"clawdline.skill.humanize-prose"},
		"devrel":           {"clawdline.skill.humanize-prose"},
		"ui-designer": {"clawdline.skill.frontend-design", "clawdline.skill.interface-design",
			"clawdline.skill.visual-hierarchy-audit", "clawdline.skill.better-typography"},
		"ui-finish-gate": {"clawdline.skill.better-interface", "clawdline.skill.motion-review",
			"clawdline.skill.better-layout", "clawdline.skill.web-interface-review"},
		"ux-architect":   {"clawdline.skill.better-layout"},
		"frontend":       {"clawdline.skill.frontend-component-build", "clawdline.skill.better-accessibility", "clawdline.skill.mobile-native"},
		"accessibility":  {"clawdline.skill.accessibility-audit", "clawdline.skill.better-accessibility"},
		"brand-guardian": {"clawdline.skill.extract-design-md"},
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
	released := map[string]string{}
	for role, version := range beforeSeveralSkills {
		released[role] = version
	}
	for role, version := range beforeDesignSkills {
		released[role] = version
	}
	for role, version := range released {
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

// The released definitions of the roles that gained reviewed engineering and
// marketing Skills on 2026-11-08, read from the catalog at 16cfd8960 before the
// change. growth, seo and ai-search carried no Skill then.
var beforeEngineeringMarketingSkills = map[string]string{
	"architect":          "sha256:ce8dfeeb28dfcc1c471b7b1fb97c6cb39f52bf68a7857f87b308ee0340ba91a2",
	"backend":            "sha256:a7a3a682727600c6615d4bed97573e444507c33453d0e7c0d6470b92b5ea8c4d",
	"performance":        "sha256:c1e262e0137037b60095c5bab932355a2212216fa2e196740e44dd83e54303d0",
	"security":           "sha256:27deb5f3f95ffe3e2eed4e83243a5ea8db1d2683d3912d88a6da9eaaef7df165",
	"content-writer":     "sha256:fc1cccc8b61e69924b9cb34591204e3a21cd73824d28a5bc9385a1f5a953236b",
	"sprint-prioritizer": "sha256:a8b36f218531da8b6fcc49aaa401a04a52abd6b06d4622591cdbe560922c5c35",
	"growth":             "sha256:0b17f2e0e0a9c002e30473849a049e50fcac3ba36813678a05dbcfced2cb6115",
	"seo":                "sha256:feb4404d9cf0af2f85c60cdb8423c62ad78c79a880c4df2232a6cad8a8c97f6b",
	"ai-search":          "sha256:d4ac4c8750eddc4dd7a46188b6939573076e61e990569f6f0c31505d25fe1759",
}

func TestEngineeringAndMarketingRolesGainReviewedSkills(t *testing.T) {
	c := Builtins()
	defs := map[string]Definition{}
	for _, d := range c.Definitions {
		defs[d.ShortID] = d
	}
	want := map[string][]string{
		"architect":          {"decision-record", "improve-codebase-architecture", "domain-modeling"},
		"backend":            {"golang-concurrency", "api-and-interface-design"},
		"performance":        {"binding-constraint", "performance-optimization"},
		"security":           {"exploit-path-review", "security-review"},
		"content-writer":     {"humanize-prose", "copywriting"},
		"sprint-prioritizer": {"opportunity-cost", "prioritize-features"},
		"growth":             {"cro"},
		"seo":                {"seo-schema", "seo-hreflang"},
		"ai-search":          {"seo-schema"},
	}
	for role, names := range want {
		got := defs[role].Skills
		if len(got) != len(names) {
			t.Fatalf("%s carries %+v, want %v", role, got, names)
		}
		for i, name := range names {
			if got[i].ID != "clawdline.skill."+name || !got[i].Enabled {
				t.Errorf("%s skill %d = %+v, want %s", role, i, got[i], name)
			}
		}
	}
	if defs["seo"].Skills[0] != defs["ai-search"].Skills[0] {
		t.Errorf("seo and ai-search do not share one seo-schema version: %+v %+v", defs["seo"].Skills[0], defs["ai-search"].Skills[0])
	}
	// Existing defaults keep their place and version, so a role that gained a
	// Skill still resolves the definition it released before.
	for role, version := range beforeEngineeringMarketingSkills {
		current := defs[role]
		if current.Version == version {
			t.Errorf("%s did not get a new version for its new default skills", role)
			continue
		}
		resolved := false
		for _, previous := range EarlierBuiltinDefinitions(current) {
			if previous.Version == version {
				resolved = previous.Body == current.Body && previous.Digest == builtinDefinitionDigest(previous)
			}
		}
		if !resolved {
			t.Errorf("the released %s %s no longer resolves", role, version)
		}
	}
	for id, version := range map[string]string{
		"clawdline.skill.decision-record":     "sha256:4016b4e2c5f2e2945492b08cdf3e35ec0e9616c90af7f67f530afeb20d9c9f1e",
		"clawdline.skill.binding-constraint":  "sha256:da71d6b7a798810c8bd20fffcdff41c079e8469a22db4ab73941a8fc8c2d1566",
		"clawdline.skill.exploit-path-review": "sha256:628ea3eaa12c96257a9adb3e73e000588806ecc6655d2f917bfd17d6a61d37fd",
		"clawdline.skill.opportunity-cost":    "sha256:128852a6319c4246156222e75ddf515d9877d41aafdbe44f124348866747a70f",
		"clawdline.skill.humanize-prose":      releasedSkillVersions["clawdline.skill.humanize-prose"],
		"clawdline.skill.golang-concurrency":  releasedSkillVersions["clawdline.skill.golang-concurrency"],
	} {
		for _, s := range c.Skills {
			if s.SkillID == id && s.Version != version {
				t.Errorf("%s moved from released %s to %s", id, version, s.Version)
			}
		}
	}
}

// The launch prompt quotes a skill's own "Use this skill" sentence, clipped at
// 240 runes; a sentence that long would lose the part saying when not to use it.
func TestEveryBuiltinSkillSaysWhenInOneShortSentence(t *testing.T) {
	for _, s := range Builtins().Skills {
		text := strings.Join(strings.Fields(s.Content), " ")
		at := strings.Index(text, "Use this skill")
		if at < 0 {
			t.Errorf("%s has no \"Use this skill\" sentence", s.SkillID)
			continue
		}
		sentence := text[at:]
		if end := strings.Index(sentence, ". "); end >= 0 {
			sentence = sentence[:end+1]
		}
		if n := len([]rune(sentence)); n > 240 {
			t.Errorf("%s says when in %d runes, over the 240 the launch prompt keeps: %q", s.SkillID, n, sentence)
		}
	}
}
