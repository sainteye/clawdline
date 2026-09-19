// Package skillmenu reads the skills one assistant session can invoke, for
// the composer's slash menu and the input bar's.
//
// It is the Swift app's `ClaudeSkills.swift` and `CodexSkills.swift`, rule for
// rule. Only the menu metadata is read — a name, one line of description, and
// where it came from. The body of a SKILL.md is Claude Code's to read when the
// command is invoked; loading it here would make a prompt browser into a second
// skill runtime with a second set of rules to get wrong, and nothing a skill
// contains is ever run by reading the menu.
package skillmenu

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Source is where a skill came from.
type Source string

const (
	Project  Source = "project"
	Personal Source = "personal"
	Plugin   Source = "plugin"
	Admin    Source = "admin"
	System   Source = "system"
)

// Skill is one command the menu offers.
type Skill struct {
	// Name is what follows the prefix. A plugin's skill keeps its namespace.
	Name        string
	Description string
	Source      Source
}

// Reading is one session's menu.
type Reading struct {
	Skills []Skill
	// Truncated says more skills were found than MaxSkills, and the rest are
	// not in Skills.
	Truncated bool
}

// MaxSkills bounds one answer (page). The menu draws nine rows; a person with
// more than this many skills is offered the first MaxSkills by name, and the
// answer says it cut.
const MaxSkills = 512

// maxDirectoryEntries bounds one skills directory's listing (read). Past it
// the rest of that directory is not looked at.
const maxDirectoryEntries = 1024

// maxHeadBytes bounds what is read of one SKILL.md (read). Only the
// frontmatter at the top is wanted; a description that runs past this is cut
// with it, and one line of 240 characters is all the menu shows of it anyway.
const maxHeadBytes = 16 << 10

// maxSettingsBytes bounds one settings or plugin registry file (read). A file
// larger than this is treated as one that could not be read, which leaves its
// overrides and plugins out rather than half of them in.
const maxSettingsBytes = 1 << 20

// maxDescriptionRunes is the menu's one line (page), as the Swift app cuts it.
const maxDescriptionRunes = 240

// Claude is `ClaudeSkills.available(cwd:)`: the file-backed skills available
// from one Claude Code working directory.
//
// Claude Code has no read-only command that asks a running session for its
// slash menu, so this reads the stable, local half: project skills, personal
// skills and installed plugin skills, after the precedence that matters to a
// typed command. A personal skill replaces a project skill of the same name, a
// deeper project directory replaces an ancestor, and plugin skills keep their
// namespace and so cannot collide with either.
func Claude(cwd, home string) Reading {
	work := filepath.Clean(cwd)
	homeDir := filepath.Clean(home)
	root, inRepo := repositoryRoot(work)
	project := work
	if inRepo {
		project = root
	}
	settings := readSettings(homeDir, project)

	effective := map[string]Skill{}
	for _, dir := range projectDirectories(work, root, inRepo) {
		for _, s := range skillsIn(filepath.Join(dir, ".claude", "skills"), Project, "") {
			if settings.overrides[s.Name] != "off" {
				effective[s.Name] = s
			}
		}
	}
	for _, s := range skillsIn(filepath.Join(homeDir, ".claude", "skills"), Personal, "") {
		if settings.overrides[s.Name] != "off" {
			effective[s.Name] = s
		}
	}
	for _, s := range pluginSkills(homeDir, settings.plugins) {
		effective[s.Name] = s
	}

	out := make([]Skill, 0, len(effective))
	for _, s := range effective {
		out = append(out, s)
	}
	return cut(out)
}

// cut sorts by name, the way the menu is read, and keeps MaxSkills.
func cut(skills []Skill) Reading {
	sort.Slice(skills, func(i, j int) bool {
		a, b := strings.ToLower(skills[i].Name), strings.ToLower(skills[j].Name)
		if a != b {
			return a < b
		}
		return skills[i].Name < skills[j].Name
	})
	r := Reading{Skills: skills}
	if len(r.Skills) > MaxSkills {
		r.Skills, r.Truncated = r.Skills[:MaxSkills], true
	}
	if r.Skills == nil {
		r.Skills = []Skill{}
	}
	return r
}

// repositoryRoot walks up from dir to the first directory holding `.git`,
// file or directory: a linked worktree's `.git` is a file.
func repositoryRoot(dir string) (string, bool) {
	here := dir
	for {
		if _, err := os.Lstat(filepath.Join(here, ".git")); err == nil {
			return here, true
		}
		parent := filepath.Dir(here)
		if parent == here {
			return "", false
		}
		here = parent
	}
}

// projectDirectories is every directory from the repository root down to the
// working directory, root first, so the deeper one is applied last and wins.
// Outside a repository it is the working directory alone.
func projectDirectories(work, root string, inRepo bool) []string {
	if !inRepo {
		return []string{work}
	}
	var out []string
	here := work
	for len(here) >= len(root) {
		out = append(out, here)
		if here == root {
			break
		}
		parent := filepath.Dir(here)
		if parent == here {
			break
		}
		here = parent
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// skillsIn is every `<dir>/<name>/SKILL.md` a menu may offer. Hidden entries
// are skipped, and a link to a directory is followed, as Claude Code follows
// it.
func skillsIn(dir string, source Source, prefix string) []Skill {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	names, _ := f.Readdirnames(maxDirectoryEntries)
	f.Close()
	var out []Skill
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		entry := filepath.Join(dir, name)
		if st, err := os.Stat(entry); err != nil || !st.IsDir() {
			continue
		}
		if s, ok := skillAt(filepath.Join(entry, "SKILL.md"), name, source, prefix); ok {
			out = append(out, s)
		}
	}
	return out
}

// skillAt reads one SKILL.md's frontmatter. A project or personal skill is
// named by its directory; a plugin's by its frontmatter `name`, falling back
// to the directory, under the plugin's namespace.
func skillAt(file, directoryName string, source Source, prefix string) (Skill, bool) {
	text, ok := readHead(file, maxHeadBytes)
	if !ok {
		return Skill{}, false
	}
	meta := frontmatter(text)
	if !meta.userInvocable {
		return Skill{}, false
	}
	name := directoryName
	if prefix != "" && meta.name != "" {
		name = meta.name
	}
	if !validName(name) {
		return Skill{}, false
	}
	if prefix != "" {
		name = prefix + ":" + name
	}
	return Skill{Name: name, Description: meta.description, Source: source}, true
}

// readHead is the first limit bytes of a regular file.
func readHead(path string, limit int64) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// readWhole is a regular file of at most limit bytes; a larger one is not read.
func readWhole(path string, limit int64) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, false
	}
	return data, true
}

type settings struct {
	overrides map[string]string
	plugins   map[string]bool
}

// readSettings merges `skillOverrides` and `enabledPlugins` from the personal
// settings and then the project's two files, later files winning key by key.
func readSettings(home, project string) settings {
	out := settings{overrides: map[string]string{}, plugins: map[string]bool{}}
	for _, file := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.local.json"),
	} {
		data, ok := readWhole(file, maxSettingsBytes)
		if !ok {
			continue
		}
		var doc struct {
			SkillOverrides map[string]any `json:"skillOverrides"`
			EnabledPlugins map[string]any `json:"enabledPlugins"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for k, v := range doc.SkillOverrides {
			if s, ok := v.(string); ok {
				out.overrides[k] = s
			}
		}
		for k, v := range doc.EnabledPlugins {
			if b, ok := v.(bool); ok {
				out.plugins[k] = b
			}
		}
	}
	return out
}

// pluginSkills reads `~/.claude/plugins/installed_plugins.json`. A plugin that
// the settings turn off is left out; a project-scoped install only counts when
// these settings turn it on, because the registry does not say which project it
// was installed for and including every one would leak skills installed for an
// unrelated repository into this menu.
func pluginSkills(home string, enabled map[string]bool) []Skill {
	data, ok := readWhole(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), maxSettingsBytes)
	if !ok {
		return nil
	}
	var doc struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	identities := make([]string, 0, len(doc.Plugins))
	for id := range doc.Plugins {
		identities = append(identities, id)
	}
	sort.Strings(identities)
	var out []Skill
	for _, identity := range identities {
		on, said := enabled[identity]
		if said && !on {
			continue
		}
		prefix, _, _ := strings.Cut(identity, "@")
		if !validPluginPrefix(prefix) {
			continue
		}
		var installs []struct {
			Scope       string `json:"scope"`
			InstallPath string `json:"installPath"`
		}
		if json.Unmarshal(doc.Plugins[identity], &installs) != nil {
			continue
		}
		for _, install := range installs {
			scope := install.Scope
			if scope == "" {
				scope = "user"
			}
			if (scope != "user" && !on) || install.InstallPath == "" {
				continue
			}
			root := install.InstallPath
			out = append(out, skillsIn(filepath.Join(root, "skills"), Plugin, prefix)...)
			// A plugin may also be one skill at its root. There is no
			// directory to name it, so its frontmatter `name` is required.
			if s, ok := skillAt(filepath.Join(root, "SKILL.md"), "", Plugin, prefix); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

type metadata struct {
	name          string
	description   string
	userInvocable bool
}

// frontmatter is `ClaudeSkills.metadata(in:)`: the keys between the first two
// `---` lines, with `>` and `|` block scalars. No frontmatter means no safe
// menu metadata — in particular, the first line of a skill's instructions is
// never turned into text a paired phone may read.
func frontmatter(text string) metadata {
	out := metadata{userInvocable: true}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	values := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == ">" || value == "|" {
			var block []string
			i++
			for i < len(lines) {
				next := lines[i]
				if strings.TrimSpace(next) == "---" {
					i--
					break
				}
				if next != "" && !startsWithSpace(next) {
					i--
					break
				}
				block = append(block, strings.TrimSpace(next))
				i++
			}
			joiner := "\n"
			if value == ">" {
				joiner = " "
			}
			value = strings.Join(block, joiner)
		}
		values[key] = unquoted(value)
	}
	out.name = values["name"]
	out.userInvocable = strings.ToLower(values["user-invocable"]) != "false"
	out.description = clean(values["description"])
	return out
}

func startsWithSpace(s string) bool {
	for _, r := range s {
		return unicode.IsSpace(r)
	}
	return false
}

func unquoted(v string) string {
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return v[1 : len(v)-1]
	}
	return v
}

// clean is one line: control characters become spaces, runs of whitespace
// become one, and it stops at maxDescriptionRunes.
func clean(text string) string {
	mapped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	one := strings.Join(strings.Fields(mapped), " ")
	runes := []rune(one)
	if len(runes) > maxDescriptionRunes {
		runes = runes[:maxDescriptionRunes]
	}
	return string(runes)
}

// validName is a skill name Claude Code accepts: 1 to 64 lower-case letters,
// digits and hyphens.
func validName(name string) bool {
	n := 0
	for _, r := range name {
		n++
		if !(unicode.IsLower(r) || unicode.IsNumber(r) || r == '-') {
			return false
		}
	}
	return n >= 1 && n <= 64
}

func validPluginPrefix(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("-_.", r)) {
			return false
		}
	}
	return true
}
