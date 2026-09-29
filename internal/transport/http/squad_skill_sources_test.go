package http

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSquadSkillSourcesStayInChosenProjectAndRequireSendForContent(t *testing.T) {
	f := newSquadFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	projectSkill := filepath.Join(f.paths["a"], ".claude", "skills", "writer")
	write(filepath.Join(projectSkill, "SKILL.md"), "---\nname: Project Writer\ndescription: >\n  Write project pages\n  and check evidence\n---\nUse these steps.")
	write(filepath.Join(projectSkill, "references", "notes.txt"), "private attachment")
	write(filepath.Join(f.paths["b"], ".claude", "skills", "other", "SKILL.md"), "other project")
	write(filepath.Join(home, ".codex", "skills", ".system", "builder", "SKILL.md"), "---\nname: Codex Builder\n---\nBuild.")
	write(filepath.Join(home, ".claude", "skills", "synced", "account", "helper", "SKILL.md"), "---\nname: Claude Helper\n---\nHelp.")

	path := "/v1/squad/skill-sources?provider=project&place_id=a"
	if status, _ := f.ask("GET", path, f.reader, "", ""); status != 200 {
		t.Fatalf("remote reader = %d", status)
	}
	if status, _ := f.ask("GET", path, f.sender, "", ""); status != 200 {
		t.Fatalf("remote sender = %d", status)
	}
	status, raw := f.ask("GET", path, f.local, "", "")
	var list struct {
		Skills []skillSource `json:"skills"`
	}
	if status != 200 || json.Unmarshal([]byte(raw), &list) != nil || len(list.Skills) != 1 || list.Skills[0].Name != "Project Writer" || list.Skills[0].Purpose != "Write project pages and check evidence" || strings.Contains(raw, f.paths["a"]) {
		t.Fatalf("project list = %d, %s", status, raw)
	}
	id := list.Skills[0].ID
	if status, _ := f.ask("GET", path+"&id="+id+"&folder=true", f.reader, "", ""); status != 403 {
		t.Fatalf("remote reader detail = %d", status)
	}
	status, raw = f.ask("GET", path+"&id="+id+"&folder=true", f.local, "", "")
	var detail skillSourceDetail
	if status != 200 || json.Unmarshal([]byte(raw), &detail) != nil || len(detail.Files) != 1 || detail.Files[0].Path != "references/notes.txt" {
		t.Fatalf("folder detail = %d, %s", status, raw)
	}
	if body, err := base64.StdEncoding.DecodeString(detail.Files[0].ContentBase64); err != nil || string(body) != "private attachment" {
		t.Fatalf("attachment = %q, %v", body, err)
	}
	status, raw = f.ask("GET", path+"&id="+id+"&folder=false", f.local, "", "")
	if status != 200 || json.Unmarshal([]byte(raw), &detail) != nil || len(detail.Files) != 0 {
		t.Fatalf("text detail = %d, %s", status, raw)
	}
	if status, _ := f.ask("GET", "/v1/squad/skill-sources?provider=project", f.local, "", ""); status != 400 {
		t.Fatalf("project without scope = %d", status)
	}
	if status, _ := f.ask("GET", "/v1/squad/skill-sources?provider=project&place_id=b&id="+id+"&folder=false", f.local, "", ""); status != 404 {
		t.Fatalf("foreign project ID = %d", status)
	}
	for _, check := range []struct{ provider, name string }{{"codex", "Codex Builder"}, {"claude-code", "Claude Helper"}} {
		status, raw = f.ask("GET", "/v1/squad/skill-sources?provider="+check.provider, f.local, "", "")
		if status != 200 || !strings.Contains(raw, check.name) {
			t.Fatalf("%s personal source = %d, %s", check.provider, status, raw)
		}
	}
}

func TestSquadSkillSourceFolderLimitKeepsTextChoice(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("Hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.bin"), make([]byte, 64<<10), 0600); err != nil {
		t.Fatal(err)
	}
	source := skillSource{Name: "Large", Path: filepath.Join(root, "SKILL.md")}
	detail, err := readSkillSource(source, true, 64<<10)
	if err != nil || detail.FolderError == "" || len(detail.Files) != 0 || detail.Content != "Hello" {
		t.Fatalf("folder limit = %+v, %v", detail, err)
	}
	detail, err = readSkillSource(source, false, 64<<10)
	if err != nil || detail.FolderError != "" {
		t.Fatalf("text choice = %+v, %v", detail, err)
	}
}

func TestSkillSourceSummaryBoundsLongMetadata(t *testing.T) {
	got := sourceSummary(strings.Repeat("word\n", 100))
	if len([]rune(got)) != maxSkillSourceSummaryRunes || strings.Contains(got, "\n") {
		t.Fatalf("summary length = %d, body %q", len([]rune(got)), got)
	}
}
