package squad

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

// The zero-based review lead carries its own skill, written for this catalog
// rather than adapted, and a review child declares no writes of a code kind:
// the skill applies whatever the task writes, or nothing at all.
func TestTheZeroBasedReviewSkillIsOriginalAndAlwaysApplies(t *testing.T) {
	skills := builtinSkills()
	skill, ok := skills["zero-review-lead"]
	if !ok || skill.SkillID != "clawdline.skill.zero-based-review" {
		t.Fatalf("zero-review-lead carries %+v", skill)
	}
	if skill.Source != persona.Original || skill.License != "MIT" {
		t.Errorf("source %q, licence %q; want %q and the repository's MIT", skill.Source, skill.License, persona.Original)
	}
	for _, claims := range [][]string{nil, {}, {"docs/review.md"}, {"internal/x.go"}, {"web/console/src/a.tsx"}} {
		if !SkillAppliesToWrites(skill.SkillID, claims) {
			t.Errorf("the skill is left out for writes %v", claims)
		}
	}
	// Control: a skill about a kind of code is left out for docs-only writes.
	if SkillAppliesToWrites("clawdline.skill.golang-concurrency", []string{"docs/review.md"}) {
		t.Error("the control skill applied to docs-only writes; the check above proves nothing")
	}
}
