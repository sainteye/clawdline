package squad

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSkillFilesKeepRelativePathsAndExactBytes(t *testing.T) {
	good := []SkillFile{
		{Path: "references/example.md", ContentBase64: base64.StdEncoding.EncodeToString([]byte("reference\n"))},
		{Path: "assets/icon.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte{0, 255, 1})},
	}
	if !ValidSkillFiles("# Skill\n", good) {
		t.Fatal("valid skill folder refused")
	}
	for _, bad := range []string{"../secret", "references/../../secret", "/secret", "C:/secret", "references\\secret", "SKILL.md", "references/./example.md"} {
		if ValidSkillFiles("# Skill\n", []SkillFile{{Path: bad, ContentBase64: good[0].ContentBase64}}) {
			t.Errorf("unsafe path accepted: %q", bad)
		}
	}
	if ValidSkillFiles("# Skill\n", []SkillFile{{Path: "a", ContentBase64: "%%%"}}) {
		t.Fatal("invalid base64 accepted")
	}
	if ValidSkillFiles("# Skill\n", []SkillFile{{Path: "a", ContentBase64: good[0].ContentBase64}, {Path: "A", ContentBase64: good[0].ContentBase64}}) {
		t.Fatal("case-folded duplicate accepted")
	}
	if ValidSkillFiles(strings.Repeat("x", MaxSquadBodyBytes), good) {
		t.Fatal("oversize skill folder accepted")
	}
}
