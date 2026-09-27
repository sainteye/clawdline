// Package persona is the closed catalog of built-in session personas: a role
// definition written into a Session's system prompt when it is launched, so
// it works the way that role works (docs/personas.md).
//
// The word is `persona` everywhere in code and on the wire. `role` already
// names the machine's coordinator role, a work document's `--role` and the
// dispatch role contract, and one name for two concepts is the defect
// docs/design-guidelines.md DG-5 exists to stop. The console says "Role".
//
// The catalog is compiled in. Each persona is one Markdown file under
// catalog/, adapted from github.com/msitarzewski/agency-agents (MIT, the
// commit in Upstream); the upstream licence travels beside them. The files are
// parsed when the package loads, and TestTheCatalogLoads makes a malformed one
// a failing test rather than something a running daemon finds out.
//
// Nothing here reads a file or the environment: the daemon writes the texts
// to disk (internal/app, WritePersonaFiles) and the launch code names them.
package persona

import (
	"embed"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The catalog's bounds, registered in internal/domain/capacity
// (`personas.catalog` and `personas.text_bytes`).
const (
	// MaxPersonas is how many personas the catalog may hold. A catalog past
	// it does not load.
	MaxPersonas = 64
	// MaxPersonaBytes is the most one persona's injected text — the
	// precedence preamble, the body and the source line — may be. It is read
	// on every turn of the session it is given to.
	MaxPersonaBytes = 8192
)

// Upstream is where the texts were adapted from.
const (
	UpstreamRepository = "https://github.com/msitarzewski/agency-agents"
	UpstreamCommit     = "053ddbbf392a1688fc7043d81529f47ef2cf86c8"
	UpstreamLicense    = "MIT"
)

// Order is the catalog in the order every list shows it. A file under
// catalog/ that is not named here, or a name here with no file, is a catalog
// that does not load.
var Order = []string{
	"architect", "backend", "frontend", "minimal-change",
	"code-reviewer", "reality-checker", "security", "technical-writer",
	"seo", "content-writer", "ai-search", "social-media",
	"instagram", "email", "growth", "pr",
}

// Teams are the closed set a persona belongs to, in the order the console's
// team switcher lists them. The console names them; the catalog only says
// which. A persona may belong to several: the same role is at home in more
// than one team.
var Teams = []string{"engineering", "marketing", "product", "quality", "operations", "design", "business"}

func knownTeam(team string) bool {
	for _, t := range Teams {
		if t == team {
			return true
		}
	}
	return false
}

// Preamble opens every persona's injected text. It says where the persona
// stands among the instructions a session already has, so a persona and a
// project's rules never have to be reconciled by the session guessing.
const Preamble = "# Clawdline persona\n\n" +
	"This persona shapes how you think and what you look for in this session. " +
	"It never overrides CLAUDE.md, AGENTS.md or any other project instruction file, " +
	"the task brief, CHILD.md, or the Clawdline protocol: where any of them says " +
	"something different, they win and this persona gives way.\n\n"

// Names is a persona's name or summary in the two languages the console
// speaks.
type Names struct {
	En     string
	ZhHant string
}

// Persona is one entry of the catalog.
type Persona struct {
	ID string
	// Teams are the groups the console's switcher shows it in: at least one,
	// each of Teams, none twice, in the order the file lists them.
	Teams   []string
	Name    Names
	Summary Names
	// SuggestedKinds are the Board item kinds this persona is offered for
	// first (epic, feature, issue). A suggestion, never a default: nothing
	// is given a persona nobody asked for.
	SuggestedKinds []string
	// Source is the upstream file this one was adapted from.
	Source string
	Icon   Icon
	// Body is the Markdown after the frontmatter.
	Body string
}

// Text is what is written to disk and injected: the preamble, the body and
// the attribution.
func (p Persona) Text() string {
	return Preamble + strings.TrimSpace(p.Body) + "\n\n---\nAdapted from " + p.Source +
		" (" + UpstreamLicense + " License).\n"
}

// FileName is the persona's file under the personas directory.
func FileName(id string) string { return id + ".md" }

// DirName is the directory under CLAWDLINE_NEXT_DIR the texts are written to.
const DirName = "personas"

// Dir is the personas directory for a state directory.
func Dir(nextDir string) string { return filepath.Join(nextDir, DirName) }

// Path is one persona's file for a state directory.
func Path(nextDir, id string) string { return filepath.Join(Dir(nextDir), FileName(id)) }

//go:embed catalog/*.md catalog/LICENSE.agency-agents
var files embed.FS

// LicenseFileName is the upstream licence's name, in the catalog and beside
// the written texts.
const LicenseFileName = "LICENSE.agency-agents"

// License is the upstream licence, word for word.
func License() string {
	raw, _ := files.ReadFile("catalog/" + LicenseFileName)
	return string(raw)
}

var catalog, loadErr = load()

// All is the catalog in Order. It is a copy.
func All() []Persona {
	return append([]Persona(nil), catalog...)
}

// Known answers the persona named id, exactly.
func Known(id string) (Persona, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Persona{}, false
}

// IDs is the catalog's ids in Order, for a refusal that lists them.
func IDs() []string {
	out := make([]string, 0, len(catalog))
	for _, p := range catalog {
		out = append(out, p.ID)
	}
	return out
}

// idShape is what an id may be: it is a path segment, a file name and a
// marker in a command line, so it is lower-case letters and inner hyphens.
var idShape = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

func load() ([]Persona, error) {
	entries, err := files.ReadDir("catalog")
	if err != nil {
		return nil, err
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".md") {
			onDisk[strings.TrimSuffix(name, ".md")] = true
		}
	}
	if len(Order) > MaxPersonas {
		return nil, fmt.Errorf("the catalog has %d personas; at most %d", len(Order), MaxPersonas)
	}
	out := make([]Persona, 0, len(Order))
	seen := map[string]bool{}
	for _, id := range Order {
		if seen[id] {
			return nil, fmt.Errorf("%s is listed twice", id)
		}
		seen[id] = true
		if !onDisk[id] {
			return nil, fmt.Errorf("%s has no file under catalog/", id)
		}
		raw, err := files.ReadFile("catalog/" + FileName(id))
		if err != nil {
			return nil, err
		}
		p, err := parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if p.ID != id {
			return nil, fmt.Errorf("catalog/%s names itself %q", FileName(id), p.ID)
		}
		icon, ok := icons[id]
		if !ok {
			return nil, fmt.Errorf("%s has no icon", id)
		}
		if p.Icon, err = icon.grid(); err != nil {
			return nil, fmt.Errorf("%s: icon: %w", id, err)
		}
		if n := len(p.Text()); n > MaxPersonaBytes {
			return nil, fmt.Errorf("%s: the injected text is %d bytes; at most %d", id, n, MaxPersonaBytes)
		}
		out = append(out, p)
		delete(onDisk, id)
	}
	for id := range onDisk {
		return nil, fmt.Errorf("catalog/%s is not in Order", FileName(id))
	}
	return out, nil
}

// kinds a persona may suggest itself for: the Board's three item kinds.
var kinds = map[string]bool{"epic": true, "feature": true, "issue": true}

// parse reads one file: `---`, `key: value` lines, `---`, then the body.
func parse(raw string) (Persona, error) {
	if !utf8.ValidString(raw) {
		return Persona{}, errors.New("not UTF-8")
	}
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return Persona{}, errors.New("no frontmatter")
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return Persona{}, errors.New("the frontmatter is not closed")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return Persona{}, fmt.Errorf("frontmatter line %q is not key: value", line)
		}
		if _, dup := fields[key]; dup {
			return Persona{}, fmt.Errorf("frontmatter key %s twice", key)
		}
		fields[key] = value
	}
	want := []string{"id", "teams", "name_en", "name_zh", "summary_en", "summary_zh", "suggested_kinds", "source"}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			return Persona{}, fmt.Errorf("frontmatter has no %s", key)
		}
		if key != "suggested_kinds" && fields[key] == "" {
			return Persona{}, fmt.Errorf("frontmatter %s is empty", key)
		}
	}
	if len(fields) != len(want) {
		return Persona{}, fmt.Errorf("frontmatter has %d keys; want exactly %v", len(fields), want)
	}
	p := Persona{
		ID:      fields["id"],
		Name:    Names{En: fields["name_en"], ZhHant: fields["name_zh"]},
		Summary: Names{En: fields["summary_en"], ZhHant: fields["summary_zh"]},
		Source:  fields["source"],
		Body:    body,
	}
	if !idShape.MatchString(p.ID) {
		return Persona{}, fmt.Errorf("id %q is not lower-case words joined by hyphens", p.ID)
	}
	var err error
	if p.Teams, err = list(fields["teams"]); err != nil {
		return Persona{}, fmt.Errorf("teams: %w", err)
	}
	if len(p.Teams) == 0 {
		return Persona{}, errors.New("teams is empty; a persona belongs to at least one")
	}
	inTeam := map[string]bool{}
	for _, team := range p.Teams {
		if !knownTeam(team) {
			return Persona{}, fmt.Errorf("team %q is not one of %v", team, Teams)
		}
		if inTeam[team] {
			return Persona{}, fmt.Errorf("team %s is listed twice", team)
		}
		inTeam[team] = true
	}
	// The English name goes into a Codex command line, which `ps` renders
	// under LC_ALL=C: anything but printable ASCII would come back escaped.
	if !printableASCII(p.Name.En) {
		return Persona{}, fmt.Errorf("name_en %q is not printable ASCII", p.Name.En)
	}
	if !strings.HasPrefix(p.Source, UpstreamRepository+"/blob/"+UpstreamCommit+"/") {
		return Persona{}, fmt.Errorf("source %q is not a file of %s at %s", p.Source, UpstreamRepository, UpstreamCommit)
	}
	suggested, err := list(fields["suggested_kinds"])
	if err != nil {
		return Persona{}, fmt.Errorf("suggested_kinds: %w", err)
	}
	p.SuggestedKinds = []string{}
	for _, k := range suggested {
		if !kinds[k] {
			return Persona{}, fmt.Errorf("suggested_kinds names %q, which is not epic, feature or issue", k)
		}
		p.SuggestedKinds = append(p.SuggestedKinds, k)
	}
	if strings.TrimSpace(p.Body) == "" {
		return Persona{}, errors.New("the body is empty")
	}
	return p, nil
}

// list reads a frontmatter `[a, b]` value; `[]` is an empty list.
func list(value string) ([]string, error) {
	inner, ok := strings.CutPrefix(value, "[")
	if !ok {
		return nil, fmt.Errorf("%q is not a [list]", value)
	}
	if inner, ok = strings.CutSuffix(inner, "]"); !ok {
		return nil, fmt.Errorf("%q is not a [list]", value)
	}
	out := []string{}
	for _, item := range strings.Split(inner, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out, nil
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
