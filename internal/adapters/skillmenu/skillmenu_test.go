package skillmenu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skill(t *testing.T, dir, name, front string) {
	t.Helper()
	write(t, filepath.Join(dir, name, "SKILL.md"), front+"\n\nThe body, which the menu never reads.\n")
}

func names(r Reading) string {
	var out []string
	for _, s := range r.Skills {
		out = append(out, s.Name+"="+string(s.Source))
	}
	return strings.Join(out, " ")
}

func TestClaudeFollowsTheTypedCommandsPrecedence(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(repo, "web", "app")
	// The root and a deeper directory both publish `deploy`; the deeper wins.
	skill(t, filepath.Join(repo, ".claude", "skills"), "deploy", "---\ndescription: root deploy\n---")
	skill(t, filepath.Join(repo, "web", ".claude", "skills"), "deploy", "---\ndescription: web deploy\n---")
	skill(t, filepath.Join(repo, ".claude", "skills"), "lint", "---\ndescription: lint it\n---")
	// A personal skill replaces a project skill of the same name.
	skill(t, filepath.Join(repo, ".claude", "skills"), "review", "---\ndescription: project review\n---")
	skill(t, filepath.Join(home, ".claude", "skills"), "review", "---\ndescription: my review\n---")
	// Turned off in the project's local settings.
	skill(t, filepath.Join(home, ".claude", "skills"), "noisy", "---\ndescription: off\n---")
	write(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"skillOverrides":{"noisy":"off"}}`)
	// Not offered: not user-invocable, a name Claude Code refuses, hidden.
	skill(t, filepath.Join(home, ".claude", "skills"), "internal", "---\nuser-invocable: false\n---")
	skill(t, filepath.Join(home, ".claude", "skills"), "Bad_Name", "---\ndescription: x\n---")
	skill(t, filepath.Join(home, ".claude", "skills"), ".hidden", "---\ndescription: x\n---")
	// No frontmatter: offered, with no description rather than its first line.
	write(t, filepath.Join(home, ".claude", "skills", "plain", "SKILL.md"), "Run rm -rf on everything.\n")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	r := Claude(work, home)
	if got, want := names(r), "deploy=project lint=project plain=personal review=personal"; got != want {
		t.Fatalf("menu = %q, want %q", got, want)
	}
	for _, s := range r.Skills {
		switch s.Name {
		case "deploy":
			if s.Description != "web deploy" {
				t.Errorf("deploy came from %q, want the deeper directory", s.Description)
			}
		case "review":
			if s.Description != "my review" {
				t.Errorf("review = %q, want the personal one", s.Description)
			}
		case "plain":
			if s.Description != "" {
				t.Errorf("a file with no frontmatter offered %q", s.Description)
			}
		}
	}
}

func TestClaudeReadsPluginsTheSettingsTurnOn(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	user := filepath.Join(home, "plugins", "design")
	scoped := filepath.Join(home, "plugins", "scoped")
	off := filepath.Join(home, "plugins", "off")
	skill(t, filepath.Join(user, "skills"), "frontend", "---\nname: frontend-design\ndescription: >\n  Distinctive\n  design.\n---")
	write(t, filepath.Join(user, "SKILL.md"), "---\nname: root-skill\ndescription: \"quoted\"\n---\n")
	skill(t, filepath.Join(scoped, "skills"), "only-here", "---\ndescription: project scoped\n---")
	skill(t, filepath.Join(off, "skills"), "gone", "---\ndescription: disabled\n---")
	registry := map[string]any{"plugins": map[string]any{
		"design@official": []any{map[string]any{"scope": "user", "installPath": user}},
		"scoped@official": []any{map[string]any{"scope": "project", "installPath": scoped}},
		"off@official":    []any{map[string]any{"scope": "user", "installPath": off}},
	}}
	data, _ := json.Marshal(registry)
	write(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), string(data))
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"off@official":false}}`)

	r := Claude(work, home)
	if got, want := names(r), "design:frontend-design=plugin design:root-skill=plugin"; got != want {
		t.Fatalf("menu = %q, want %q", got, want)
	}
	if r.Skills[0].Description != "Distinctive design." || r.Skills[1].Description != "quoted" {
		t.Errorf("descriptions = %q, %q", r.Skills[0].Description, r.Skills[1].Description)
	}

	// The project's own settings turn the project-scoped install on.
	write(t, filepath.Join(work, ".claude", "settings.json"), `{"enabledPlugins":{"scoped@official":true}}`)
	if got := names(Claude(work, home)); !strings.Contains(got, "scoped:only-here=plugin") {
		t.Errorf("an enabled project-scoped plugin is missing: %q", got)
	}
}

func TestDescriptionIsOneShortLine(t *testing.T) {
	long := strings.Repeat("字", 300)
	m := frontmatter("---\ndescription: |\n  one\n  two\t" + long + "\n---\n")
	if strings.ContainsAny(m.description, "\n\t") {
		t.Errorf("not one line: %q", m.description)
	}
	if n := len([]rune(m.description)); n != maxDescriptionRunes {
		t.Errorf("%d runes, want %d", n, maxDescriptionRunes)
	}
}

func TestClaudeOnAnEmptyMachineIsAnEmptyList(t *testing.T) {
	r := Claude(t.TempDir(), t.TempDir())
	if r.Skills == nil || len(r.Skills) != 0 || r.Truncated {
		t.Fatalf("%+v", r)
	}
}

const catalog = `Some instructions.
### Available skills
- imagegen: Generate images. (file: /Users/x/.codex/skills/.system/imagegen/SKILL.md)
- deploy: Ship it (file: /Users/x/.agents/skills/deploy/SKILL.md)
- repo-lint: Lint (file: /work/repo/.agents/skills/repo-lint/SKILL.md)
- admin-tool: Managed (file: /etc/codex/skills/admin-tool/SKILL.md)
- figma:implement: From a plugin (executor package: figma)
- not valid!: skipped
</skills_instructions>
- after: not part of it`

func TestCodexReadsTheCatalogItWasStartedWith(t *testing.T) {
	r := CodexCatalog(catalog, "/Users/x")
	want := "imagegen=system deploy=personal repo-lint=project admin-tool=admin figma:implement=plugin"
	if got := names(r); got != want {
		t.Fatalf("menu = %q, want %q", got, want)
	}
	if r.Skills[0].Description != "Generate images." || r.Skills[4].Description != "From a plugin" {
		t.Errorf("locators left in: %q, %q", r.Skills[0].Description, r.Skills[4].Description)
	}

	// From a rollout: the catalog is a string somewhere in one JSON row.
	dir := t.TempDir()
	row, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{
		"instructions": []any{"other", catalog},
	}})
	rollout := filepath.Join(dir, "rollout.jsonl")
	write(t, rollout, `{"type":"note"}`+"\n"+string(row)+"\n")
	if got := names(Codex(rollout, "/Users/x")); got != want {
		t.Errorf("from the rollout = %q", got)
	}
	if r := Codex(filepath.Join(dir, "missing.jsonl"), "/Users/x"); r.Skills == nil || len(r.Skills) != 0 {
		t.Errorf("a missing rollout = %+v", r)
	}
}

// Newer Codex mentions the marker in a sentence of its own instructions, in an
// earlier row, and names each file by an alias from a roots table.
func TestCodexReadsTheHeadingAndExpandsItsAliases(t *testing.T) {
	prose := "# Using skills\nThe skills available to you will be listed under \u201c### Available skills\u201d.\n\n" +
		"### How to use skills\n- Discovery: When a section is present, it lists the skills.\n"
	listed := "## Skills\n### Skill roots\n- `r0` = `/Users/x/.codex/skills`\n- `r1` = `/Users/x/.codex/skills/.system`\n" +
		"### Available skills\n- imagegen: Images. (file: r1/imagegen/SKILL.md)\n" +
		"- mine: Mine. (file: r0/mine/SKILL.md)\n- visualize:visualize: Charts. (file: r3/visualize/SKILL.md)\n</skills_instructions>"
	dir := t.TempDir()
	first, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"base_instructions": prose}})
	second, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"content": []any{listed}}})
	rollout := filepath.Join(dir, "rollout.jsonl")
	write(t, rollout, string(first)+"\n"+string(second)+"\n")
	if got, want := names(Codex(rollout, "/Users/x")), "imagegen=system mine=personal visualize:visualize=plugin"; got != want {
		t.Fatalf("menu = %q, want %q", got, want)
	}
	if r := CodexCatalog(prose, "/Users/x"); len(r.Skills) != 0 {
		t.Errorf("a sentence naming the marker was read as a catalog: %q", names(r))
	}
}

func TestCacheServesFreshReadingsAndLetsTheOldestGo(t *testing.T) {
	c := NewCache()
	if c.limit != int(capacity.Default(capacity.CacheSessionSkills)) || c.limit <= 0 {
		t.Fatalf("limit %d is not the register's", c.limit)
	}
	clock := time.Unix(1_000, 0)
	c.now = func() time.Time { return clock }
	reads := 0
	read := func() Reading { reads++; return Reading{Skills: []Skill{{Name: "x"}}} }

	c.Get("a", read)
	c.Get("a", read)
	if reads != 1 {
		t.Fatalf("a fresh reading was read again: %d reads", reads)
	}
	clock = clock.Add(FreshFor)
	if _, at := c.Get("a", read); reads != 2 || !at.Equal(clock) {
		t.Fatalf("a stale reading was served: %d reads, at %v", reads, at)
	}

	c.SetLimit(2)
	c.Get("b", read)
	c.Get("a", read) // a is now the most recent
	c.Get("c", read) // b goes
	r := c.Reading()
	if !r.Known || r.Used != 2 || r.Counters.Evicted != 1 {
		t.Fatalf("reading = %+v", r)
	}
	if _, ok := c.items["b"]; ok {
		t.Error("the entry used longest ago was kept")
	}
	c.SetLimit(0)
	if c.limit != 2 {
		t.Error("an override of zero changed the limit")
	}
}
