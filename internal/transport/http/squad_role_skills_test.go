package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// Released before built-in roles carried several default Skills (c203b1b5e).
var oneSkillRoleVersions = map[string]struct{ version, skill, skillVersion string }{
	"technical-writer": {"sha256:a02dc5b7c926693e8cf135ec930b6b823ff83160077961ef4b0528eb684dd275",
		"clawdline.skill.documentation-writer", "sha256:60a56273fc84cd68398d3e0885904d135dfdb3ed7ff4613dd323360839e7154e"},
	"code-reviewer": {"sha256:6324291052765bd9000aca77475a041422bf82bfe8837a33282fd300b1695edf",
		"clawdline.skill.two-axis-code-review", "sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d"},
	"minimal-change": {"sha256:584a0a46ab4e63c7752e366023763e4966acf5426db76e8ebe1e0a97e97fe0c7",
		"clawdline.skill.smallest-sufficient-change", "sha256:b0a174e89e9c2dad7e3d5e9f00f8425177caa5f5fc09cff3aca4ec121ab22bd3"},
	"pr": {"sha256:886cbcc50e4183e9d5d810688f3193c3b2ecfde185b654db6842a7bd4f15829b", "", ""},
}

func TestDefinitionsFromBeforeSeveralSkillsResolveAndLaunch(t *testing.T) {
	f := newSquadFixture(t)
	for role, old := range oneSkillRoleVersions {
		t.Run(role, func(t *testing.T) {
			status, raw := f.ask("GET", "/v1/squad/definitions/"+role+"?version="+old.version, f.reader, "", "")
			var got squad.Definition
			if status != 200 || json.Unmarshal([]byte(raw), &got) != nil || got.Version != old.version {
				t.Fatalf("released %s = %d %s", role, status, raw)
			}
			if old.skill == "" {
				if len(got.Skills) != 0 {
					t.Fatalf("released %s had no skills, now %+v", role, got.Skills)
				}
				return
			}
			if len(got.Skills) != 1 || got.Skills[0].ID != old.skill || got.Skills[0].Version != old.skillVersion {
				t.Fatalf("released %s skills = %+v", role, got.Skills)
			}
			// A Project override saved before the change pins the one released
			// Skill; it keeps launching exactly that Skill and no new default.
			body := fmt.Sprintf(`{"place_id":"a","definition_id":%q,"expected_version":0,"overrides":{"skills":{"present":true,"value":[{"id":%q,"version":%q,"enabled":true}]}}}`,
				role, old.skill, old.skillVersion)
			if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, body, "pin-one-skill-"+role); status != 200 {
				t.Fatalf("pin released skill = %d %s", status, raw)
			}
			launch, err := f.s.ResolveSquadLaunch(context.Background(), role, f.paths["a"])
			if err != nil || len(launch.Skills) != 1 || launch.Skills[0].ID != old.skill || launch.Skills[0].Version != old.skillVersion {
				t.Fatalf("pinned launch = %+v, %v", launch.Skills, err)
			}
		})
	}
}

func TestARoleWithSeveralDefaultSkillsLaunchesThemAllInOrder(t *testing.T) {
	f := newSquadFixture(t)
	launch, err := f.s.ResolveSquadLaunch(context.Background(), "technical-writer", "")
	if err != nil || len(launch.Skills) != 2 ||
		launch.Skills[0].ID != "clawdline.skill.documentation-writer" ||
		launch.Skills[1].ID != "clawdline.skill.humanize-prose" ||
		!launch.Skills[0].Enabled || !launch.Skills[1].Enabled {
		t.Fatalf("technical-writer launch = %+v, %v", launch.Skills, err)
	}
	document, _ := json.Marshal(launch)
	hash := sha256.Sum256(document)
	files, err := squadfiles.Publish(t.TempDir(), "ABCDEFGHIJKLMNOPQRSTUV", hex.EncodeToString(hash[:]), "test-capability", document)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(files.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(prompt)
	first := strings.Index(text, "clawdline.skill.documentation-writer")
	second := strings.Index(text, "clawdline.skill.humanize-prose")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the launch prompt does not list both skills in order:\n%s", text)
	}
	// The shared Skill is one catalog entry, not one per role.
	status, raw := f.ask("GET", "/v1/squad/catalog", f.reader, "", "")
	if status != 200 || strings.Count(raw, `"skill_id":"clawdline.skill.humanize-prose"`) != 1 {
		t.Fatalf("catalog lists humanize-prose %d times (status %d)", strings.Count(raw, `"skill_id":"clawdline.skill.humanize-prose"`), status)
	}
}
