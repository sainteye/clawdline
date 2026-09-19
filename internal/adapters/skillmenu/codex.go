package skillmenu

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// codexMarker opens the catalog Codex writes into its initial instructions.
const codexMarker = "### Available skills"

// codexHeading is the marker as a heading: a line of its own. Codex's own
// instructions also mention the marker in a sentence ("listed … under ‘###
// Available skills’"), in an earlier row than the catalog, and the bullets
// after that sentence are how to use skills, not skills. The Swift parser took
// the first mention and offered one skill called "Discovery"; a heading is the
// only mention that opens a list.
var codexHeading = regexp.MustCompile(`(?m)^[ \t]*### Available skills[ \t]*$`)

// codexRoot is one row of the `### Skill roots` table newer Codex writes
// before the catalog, which then names each file by alias: `r1/imagegen/…`.
var codexRoot = regexp.MustCompile("(?m)^[ \\t]*- `([A-Za-z0-9_]+)` = `([^`]+)`[ \\t]*$")

// maxRolloutPrefix bounds what is read of a Codex rollout (read). The catalog
// is part of the first instructions, so opening the menu in a month-long
// conversation never means loading the month-long conversation.
const maxRolloutPrefix = 4 << 20

// Codex is `CodexSkills.available(in:)`: the skills the running Codex session
// was actually started with, as Codex wrote them into its rollout.
//
// That is a better source than walking every cache directory on the machine:
// it already reflects repository scope, disabled skills, bundled skills and
// enabled plugins, and it cannot offer an old plugin that merely happens to
// remain on disk.
func Codex(rollout, home string) Reading {
	f, err := os.Open(rollout)
	if err != nil {
		return cut(nil)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRolloutPrefix))
	if err != nil {
		return cut(nil)
	}
	marker := []byte(codexMarker)
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, marker) {
			continue
		}
		var doc any
		if json.Unmarshal(line, &doc) != nil {
			continue
		}
		// A row that only mentions the marker is passed over for one that
		// holds the list.
		if text, ok := catalogText(doc); ok {
			return CodexCatalog(text, home)
		}
	}
	return cut(nil)
}

// CodexCatalog reads the human-readable block Codex put into its
// instructions. It is kept apart from the file because the rollout's JSON has
// changed shape before; this block is the only contract it needs.
//
// Codex lists the catalog in its own order, and the menu keeps it: a person
// reading `$` in Codex expects the list Codex would show.
func CodexCatalog(instructions, home string) Reading {
	at := codexHeading.FindStringIndex(instructions)
	if at == nil {
		return Reading{Skills: []Skill{}}
	}
	// `(file: r1/imagegen/SKILL.md)` is `(file: <r1's root>/imagegen/SKILL.md)`:
	// where a skill came from is read from its path, so the alias is expanded
	// before the path is looked at.
	var aliases [][2]string
	for _, m := range codexRoot.FindAllStringSubmatch(instructions[:at[0]], -1) {
		aliases = append(aliases, [2]string{"(file: " + m[1] + "/", "(file: " + strings.TrimRight(m[2], "/") + "/"})
	}
	personal := filepath.Join(home, ".agents", "skills") + "/"
	var out []Skill
	for _, raw := range strings.Split(instructions[at[1]:], "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "</skills_instructions>") {
			break
		}
		item, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		command, description, ok := strings.Cut(item, ": ")
		if !ok || !validCodexCommand(command) {
			continue
		}
		cutAt := len(description)
		for _, locator := range []string{" (file:", " (executor package:", " (orchestrator package:", " (custom resource:"} {
			if i := strings.Index(description, locator); i >= 0 && i < cutAt {
				cutAt = i
			}
		}
		description = description[:cutAt]
		for _, a := range aliases {
			item = strings.Replace(item, a[0], a[1], 1)
		}

		var source Source
		switch {
		case strings.Contains(command, ":"):
			source = Plugin
		case strings.Contains(item, "/skills/.system/"):
			source = System
		case strings.Contains(item, "/etc/codex/skills/"):
			source = Admin
		case strings.Contains(item, personal):
			source = Personal
		case strings.Contains(item, "/.agents/skills/"):
			source = Project
		default:
			source = Personal
		}
		out = append(out, Skill{Name: command, Description: clean(description), Source: source})
	}
	r := Reading{Skills: out}
	if len(r.Skills) > MaxSkills {
		r.Skills, r.Truncated = r.Skills[:MaxSkills], true
	}
	if r.Skills == nil {
		r.Skills = []Skill{}
	}
	return r
}

// catalogText finds the one string in a rollout row that holds the catalog,
// wherever in the row Codex put it.
func catalogText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, codexMarker) && codexHeading.MatchString(x) {
			return x, true
		}
	case []any:
		for _, item := range x {
			if s, ok := catalogText(item); ok {
				return s, true
			}
		}
	case map[string]any:
		for _, item := range x {
			if s, ok := catalogText(item); ok {
				return s, true
			}
		}
	}
	return "", false
}

func validCodexCommand(command string) bool {
	n := 0
	for _, r := range command {
		n++
		if !(unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return n >= 1 && n <= 160
}
