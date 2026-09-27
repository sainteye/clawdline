package persona

import (
	"strings"
	"testing"
)

// A malformed catalog is a failing test, not something a running daemon
// finds out: every file parses, is in Order, has an icon and fits.
func TestTheCatalogLoads(t *testing.T) {
	if loadErr != nil {
		t.Fatalf("the catalog does not load: %v", loadErr)
	}
	want := []string{"architect", "backend", "frontend", "minimal-change",
		"code-reviewer", "reality-checker", "security", "technical-writer",
		"seo", "content-writer", "ai-search", "social-media",
		"instagram", "email", "growth", "pr"}
	// Other teams append after these; the first sixteen keep their order.
	if got := strings.Join(IDs()[:len(want)], ","); got != strings.Join(want, ",") {
		t.Fatalf("catalog order %s, want %s first", strings.Join(IDs(), ","), strings.Join(want, ","))
	}
	if n := catalogFiles(t); len(All()) != n {
		t.Fatalf("%d personas, but catalog/ holds %d files", len(All()), n)
	}
	perTeam := map[string]int{}
	seen := map[string]bool{}
	for _, p := range All() {
		if seen[p.ID] {
			t.Errorf("%s twice", p.ID)
		}
		seen[p.ID] = true
		if !knownTeam(p.Team) {
			t.Errorf("%s has team %q", p.ID, p.Team)
		}
		perTeam[p.Team]++
		raw, err := files.ReadFile("catalog/" + FileName(p.ID))
		if err != nil {
			t.Fatal(err)
		}
		// The drafts' own budget, under the registered bound.
		if len(raw) > 6144 {
			t.Errorf("%s is %d bytes; a persona file is at most 6144", p.ID, len(raw))
		}
		if n := len(p.Text()); n > MaxPersonaBytes {
			t.Errorf("%s injects %d bytes; at most %d", p.ID, n, MaxPersonaBytes)
		}
		if p.Name.En == "" || p.Name.ZhHant == "" || p.Summary.En == "" || p.Summary.ZhHant == "" {
			t.Errorf("%s is missing a name or a summary: %+v", p.ID, p)
		}
		if !strings.HasPrefix(p.Text(), Preamble) || !strings.Contains(p.Text(), p.Source) {
			t.Errorf("%s: the injected text does not open with the preamble and name its source", p.ID)
		}
		if strings.Contains(p.Text(), "\n---\nid:") {
			t.Errorf("%s: the frontmatter leaked into the injected text", p.ID)
		}
		if len(p.Icon.Cells) != iconHeight || p.Icon.Accent == "" {
			t.Errorf("%s: icon %+v", p.ID, p.Icon)
		}
		for _, row := range p.Icon.Cells {
			if len(row) != iconWidth {
				t.Errorf("%s: an icon row is %d wide", p.ID, len(row))
			}
		}
	}
	// At least these many per team: other teams' personas may join one.
	for team, least := range map[string]int{"engineering": 8, "marketing": 9, "quality": 2, "operations": 1, "design": 1} {
		if perTeam[team] < least {
			t.Errorf("personas per team %v, want at least %d in %s", perTeam, least, team)
		}
	}
	if technicalWriter, _ := Known("technical-writer"); strings.Join(technicalWriter.Teams, ",") != "engineering,marketing" {
		t.Errorf("technical-writer is in %v, want engineering then marketing", technicalWriter.Teams)
	}
	if len(All()) > MaxPersonas {
		t.Errorf("%d personas; at most %d", len(All()), MaxPersonas)
	}
	if !strings.Contains(License(), "MIT License") || !strings.Contains(License(), "AgentLand Contributors") {
		t.Error("the upstream licence is not the MIT text it was copied as")
	}
}

// catalogFiles counts the persona files under catalog/.
func catalogFiles(t *testing.T) int {
	t.Helper()
	entries, err := files.ReadDir("catalog")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

// The preamble names what it never overrides, and is ASCII: it is the one
// part of the text written by this repository.
func TestThePreambleSaysWhatItNeverOverrides(t *testing.T) {
	for _, s := range []string{"CLAUDE.md", "AGENTS.md", "task brief", "CHILD.md", "Clawdline protocol"} {
		if !strings.Contains(Preamble, s) {
			t.Errorf("the preamble does not name %s", s)
		}
	}
	if !printableASCII(strings.ReplaceAll(Preamble, "\n", " ")) {
		t.Error("the preamble is not ASCII")
	}
}

func TestParseRefusesAMalformedFile(t *testing.T) {
	good := "---\nid: x\nteam: marketing\nname_en: X\nname_zh: 叉\nsummary_en: s\nsummary_zh: 說\nsuggested_kinds: [epic]\n" +
		"source: " + UpstreamRepository + "/blob/" + UpstreamCommit + "/a.md\n---\n# X\nbody\n"
	if _, err := parse(good); err != nil {
		t.Fatalf("the control file is refused: %v", err)
	}
	for name, raw := range map[string]string{
		"no frontmatter": "# X\n",
		"unclosed":       "---\nid: x\n",
		"missing key":    strings.Replace(good, "name_zh: 叉\n", "", 1),
		"extra key":      strings.Replace(good, "id: x\n", "id: x\ncolour: red\n", 1),
		"unknown team":   strings.Replace(good, "team: marketing", "team: sales", 1),
		"empty team":     strings.Replace(good, "team: marketing", "team:", 1),
		"no team":        strings.Replace(good, "team: marketing\n", "", 1),
		"unknown kind":   strings.Replace(good, "[epic]", "[epic, chore]", 1),
		"not a list":     strings.Replace(good, "[epic]", "epic", 1),
		"bad id":         strings.Replace(good, "id: x", "id: X_1", 1),
		"other upstream": strings.Replace(good, UpstreamCommit, "main", 1),
		"non-ascii name": strings.Replace(good, "name_en: X", "name_en: Xé", 1),
		"empty body":     strings.Replace(good, "# X\nbody\n", "  \n", 1),
		"duplicated key": strings.Replace(good, "id: x\n", "id: x\nid: y\n", 1),
		"not key: value": strings.Replace(good, "id: x\n", "id x\n", 1),
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFromCommandLine(t *testing.T) {
	architect, _ := Known("architect")
	for _, c := range []struct{ command, want string }{
		{"claude --append-system-prompt-file /home/a/.config/clawdline-next/personas/architect.md", "architect"},
		{"claude --resume 0f1e2d3c-0000-4000-8000-000000000001 --append-system-prompt-file /Users/a b/x y/personas/code-reviewer.md --add-dir /tmp/t",
			"code-reviewer"},
		{"claude --append-system-prompt-file=/x/personas/security.md", "security"},
		{"codex -c developer_instructions=\"" + CodexInstruction(architect, "/a b/personas/architect.md") + "\"", "architect"},
		// Not in the catalog: nothing.
		{"claude --append-system-prompt-file /x/personas/wizard.md", ""},
		{"codex -c developer_instructions=clawdline-persona:wizard - hi", ""},
		// A file of the same name somewhere else is not ours.
		{"claude --append-system-prompt-file /x/prompts/architect.md", ""},
		{"claude --model opus", ""},
	} {
		if got := FromCommandLine(c.command); got != c.want {
			t.Errorf("%q: %q, want %q", c.command, got, c.want)
		}
	}
}

func TestTheCodexInstructionIsASCIIApartFromThePath(t *testing.T) {
	for _, p := range All() {
		text := CodexInstruction(p, "/p")
		if !printableASCII(text) {
			t.Errorf("%s: %q", p.ID, text)
		}
		if !strings.HasPrefix(text, Marker+p.ID+" - ") || !strings.Contains(text, `"/p"`) {
			t.Errorf("%s: %q", p.ID, text)
		}
	}
}
