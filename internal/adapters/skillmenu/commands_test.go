package skillmenu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCommandsJoinSkillsWithoutReadingBodies(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	write(t, filepath.Join(project, ".git"), "worktree")
	write(t, filepath.Join(project, ".claude/commands/frontend/check.md"), "---\nname: ignored\ndescription: Inspect components\n---\nPRIVATE BODY !`do-not-run`\n")
	write(t, filepath.Join(project, ".claude/commands/ship.md"), "---\ndescription: project command\n---")
	write(t, filepath.Join(home, ".claude/commands/ship.md"), "---\ndescription: personal command\n---")
	skill(t, filepath.Join(home, ".claude/skills"), "ship", "---\ndescription: preferred skill\n---")
	write(t, filepath.Join(project, ".claude/commands/hidden.md"), "---\nuser-invocable: false\n---")
	write(t, filepath.Join(project, ".claude/commands/disabled.md"), "---\ndescription: disabled\n---")
	write(t, filepath.Join(project, ".claude/settings.json"), `{"skillOverrides":{"disabled":"off"}}`)
	write(t, filepath.Join(project, ".claude/commands/plain.md"), "PRIVATE BODY")
	// A command can be much longer than the metadata read window.
	write(t, filepath.Join(project, ".claude/commands/long.md"), "---\ndescription: long command\n---\n"+strings.Repeat("body", maxHeadBytes))
	reading := Claude(project, home)
	if got := names(reading); got != "frontend:check=project long=project plain=project ship=personal" {
		t.Fatal(got)
	}
	for _, command := range reading.Skills {
		if strings.Contains(command.Description, "PRIVATE") {
			t.Fatal("command body leaked")
		}
		if command.Name == "frontend:check" && (command.Invocation != "/frontend:check" || command.Description != "Inspect components") {
			t.Fatalf("%+v", command)
		}
		if command.Name == "ship" && command.Description != "preferred skill" {
			t.Fatalf("%+v", command)
		}
	}
}

func TestCodexPromptsKeepTheirPrefixWithoutARollout(t *testing.T) {
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	write(t, filepath.Join(codexHome, "prompts/review.md"), "---\ndescription: Custom review\nargument-hint: $TARGET\n---\nPRIVATE BODY")
	write(t, filepath.Join(codexHome, "prompts/nested/not-loaded.md"), "---\ndescription: nested\n---")
	reading := WithCodexPrompts(Reading{Skills: []Skill{{Name: "review", Source: Personal}}}, home)
	if len(reading.Skills) != 2 {
		t.Fatalf("%+v", reading)
	}
	found := false
	for _, item := range reading.Skills {
		if item.Invocation == "/prompts:review" && item.Name == "prompts:review" && item.Description == "Custom review" {
			found = true
		}
	}
	if !found {
		t.Fatal(reading)
	}
}

func TestCommandDirectoriesDoNotFollowSymlinkCycles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "ok.md"), "---\ndescription: safe\n---")
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		t.Skip(err)
	}
	if got := commandFiles(root, Project, "tools", true); len(got) != 1 || got[0].Invocation != "/tools:ok" {
		t.Fatal(got)
	}
}
