package projectfiles

// Unify makes one Project's rules and skills the same for Claude Code and
// Codex (docs/project-files.md, Unify; docs/shared-project-instructions.md).
//
// What each assistant loads was measured on 2026-10-06 with Claude Code
// 2.1.291 and codex-cli 0.160.1, not assumed:
//
//   - Codex reads AGENTS.md. Claude reads CLAUDE.md, and reads AGENTS.md only
//     when CLAUDE.md is absent or carries an `@AGENTS.md` import line.
//   - Codex finds project skills in .agents/skills, Claude in .claude/skills;
//     a relative link .claude/skills/<n> -> ../../.agents/skills/<n> is seen
//     by both.
//
// Plan reads the repository root and writes nothing. Apply recomputes the
// plan, refuses when its version is not the one the person saw, and performs
// each action beneath bound directory handles. Nothing here moves a rule
// between files on its own, follows or replaces a link it did not make, or
// commits to git.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/contract"
)

var (
	ErrPlanChanged = errors.New("the Project's rules or skills changed since the plan was read")
	ErrPlanUnknown = errors.New("part of the Project's rules or skills could not be read")
	ErrNameTaken   = errors.New("the destination name already exists")
)

// linksAvailable is whether this machine creates symbolic links for skills.
// Windows without developer mode cannot, so skills are copied there instead.
// Focused tests set it to exercise the copy path on any machine.
var linksAvailable = runtime.GOOS != "windows"

const (
	agentsFile   = "AGENTS.md"
	claudeFile   = "CLAUDE.md"
	importLine   = "@AGENTS.md"
	agentsSkills = ".agents/skills"
	claudeSkills = ".claude/skills"
)

// claudeOnlyFiles are read by Claude alone and are listed, never changed.
var claudeOnlyFiles = []string{".claude/CLAUDE.md", "CLAUDE.local.md", ".claude/CLAUDE.local.md"}

// skillLinkTarget is the link .claude/skills/<name> carries: relative, so it
// survives a clone or a moved checkout, and inside the repository.
func skillLinkTarget(name string) string { return "../../.agents/skills/" + name }

// rulesFile is one rules file as the plan read it.
type rulesFile struct {
	state  contract.ProjectUnifyFileState
	text   string
	target string
	large  bool
}

// side is one skills directory as the plan read it.
type side struct {
	dir     string // .agents/skills or .claude/skills
	state   string // missing, plain, link, unreadable
	target  string // the directory's own link target
	large   bool
	entries map[string]*entry
}

type entry struct {
	kind   string // skill (a directory with SKILL.md), link, other
	target string
	tree   string // the tree digest of a skill directory
	err    error
}

// unifyState is the plan plus what apply re-verifies before each action.
type unifyState struct {
	root   string
	plan   contract.ProjectUnifyPlan
	claude rulesFile
	sides  [2]*side
}

// Plan reads the Project's repository root and says what unify would do.
// The only error is a place that is not a directory; anything unreadable
// inside it is a conflict that makes the plan unknown.
func Plan(place string) (contract.ProjectUnifyPlan, error) {
	st, err := planAt(place)
	if err != nil {
		return contract.ProjectUnifyPlan{}, err
	}
	return st.plan, nil
}

func planAt(place string) (*unifyState, error) {
	root, _, err := projectLayers(place)
	if err != nil {
		return nil, err
	}
	base, err := os.OpenRoot(root)
	if err != nil {
		return nil, ErrUnknown
	}
	defer base.Close()
	digest := sha256.New()
	fmt.Fprintf(digest, "unify/1 links=%t\n", linksAvailable)
	st := &unifyState{root: root}
	p := contract.ProjectUnifyPlan{LinksAvailable: linksAvailable, Skills: []contract.ProjectUnifySkill{},
		Actions: []contract.ProjectUnifyAction{}, Conflicts: []contract.ProjectUnifyConflict{}}
	conflict := func(kind contract.ProjectUnifyConflictKind, path, detail string) {
		p.Conflicts = append(p.Conflicts, contract.ProjectUnifyConflict{Kind: kind, Path: path, Detail: detail})
	}

	agents := readRules(base, agentsFile, digest)
	claude := readRules(base, claudeFile, digest)
	st.claude = claude
	p.Rules = planRules(agents, claude, &p, conflict)
	for _, rel := range claudeOnlyFiles {
		if _, err := lstatBeneath(base, rel); err == nil {
			p.Rules.ClaudeOnlyFiles = append(p.Rules.ClaudeOnlyFiles, rel)
			p.Rules.Now.Claude = append(p.Rules.Now.Claude, rel)
			p.Rules.After.Claude = append(p.Rules.After.Claude, rel)
			fmt.Fprintf(digest, "claude-only %s\n", rel)
		} else if !errors.Is(err, os.ErrNotExist) {
			conflict(contract.ProjectUnifyConflictKindUnreadable, rel, "This Claude rules file could not be checked.")
		}
	}

	a := readSide(base, agentsSkills, digest)
	c := readSide(base, claudeSkills, digest)
	st.sides = [2]*side{a, c}
	planSkills(a, c, &p, conflict)

	p.Status = contract.ProjectUnifyStatusUnified
	if len(p.Actions) > 0 || len(p.Conflicts) > 0 {
		p.Status = contract.ProjectUnifyStatusDrifting
	}
	for _, c := range p.Conflicts {
		if c.Kind == contract.ProjectUnifyConflictKindUnreadable || c.Kind == contract.ProjectUnifyConflictKindTooLarge {
			p.Status = contract.ProjectUnifyStatusUnknown
		}
	}
	p.Version = hex.EncodeToString(digest.Sum(nil))
	st.plan = p
	return st, nil
}

// lstatBeneath checks a path's directory components are plain directories
// and returns the final name's Lstat.
func lstatBeneath(base *os.Root, rel string) (os.FileInfo, error) {
	rel = filepath.FromSlash(rel)
	dir, err := openDirectory(base, filepath.Dir(rel))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.Lstat(filepath.Base(rel))
}

func readRules(base *os.Root, rel string, digest hash.Hash) rulesFile {
	info, err := base.Lstat(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(digest, "rules %s missing\n", rel)
		return rulesFile{state: contract.ProjectUnifyFileStateMissing}
	case err != nil:
		fmt.Fprintf(digest, "rules %s unreadable\n", rel)
		return rulesFile{state: contract.ProjectUnifyFileStateUnreadable}
	case info.Mode()&os.ModeSymlink != 0:
		target, err := base.Readlink(rel)
		if err != nil {
			return rulesFile{state: contract.ProjectUnifyFileStateUnreadable}
		}
		fmt.Fprintf(digest, "rules %s link %q\n", rel, target)
		return rulesFile{state: contract.ProjectUnifyFileStateLink, target: target}
	}
	content, err := read(candidate(base.Name(), rel, "project", "", "instruction"))
	if err != nil {
		fmt.Fprintf(digest, "rules %s unreadable\n", rel)
		return rulesFile{state: contract.ProjectUnifyFileStateUnreadable, large: errors.Is(err, ErrLarge)}
	}
	fmt.Fprintf(digest, "rules %s %s\n", rel, content.Version)
	return rulesFile{state: contract.ProjectUnifyFileStatePresent, text: content.Text}
}

func isImport(line string) bool {
	line = strings.TrimSpace(line)
	return line == importLine || line == "@./"+agentsFile
}

// splitRules returns whether text imports AGENTS.md and its other non-blank
// lines: the rules Codex does not see.
func splitRules(text string) (bool, []string) {
	imports, others := false, []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case isImport(line):
			imports = true
		case strings.TrimSpace(line) != "":
			others = append(others, line)
		}
	}
	return imports, others
}

// withoutImports is CLAUDE.md's text minus its import lines: what becomes
// AGENTS.md, which must not import itself.
func withoutImports(text string) string {
	lines := strings.SplitAfter(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isImport(strings.TrimRight(line, "\r\n")) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "")
}

func planRules(agents, claude rulesFile, p *contract.ProjectUnifyPlan,
	conflict func(contract.ProjectUnifyConflictKind, string, string)) contract.ProjectUnifyRules {
	r := contract.ProjectUnifyRules{Agents: agents.state, Claude: claude.state, ClaudeOnlyLines: []string{},
		ClaudeOnlyFiles: []string{}, Now: contract.ProjectUnifyReads{Claude: []string{}, Codex: []string{}},
		After: contract.ProjectUnifyReads{Claude: []string{}, Codex: []string{}}}
	for _, f := range []struct {
		name string
		file rulesFile
	}{{agentsFile, agents}, {claudeFile, claude}} {
		if f.file.state != contract.ProjectUnifyFileStateUnreadable {
			continue
		}
		if f.file.large {
			conflict(contract.ProjectUnifyConflictKindTooLarge, f.name, "This file is larger than unify reads; check it on the machine.")
		} else {
			conflict(contract.ProjectUnifyConflictKindUnreadable, f.name, "This file could not be read as UTF-8 text.")
		}
	}
	if agents.state == contract.ProjectUnifyFileStateLink {
		conflict(contract.ProjectUnifyConflictKindRulesLink, agentsFile,
			"AGENTS.md is a link to "+agents.target+"; unify does not follow or change it.")
	}
	switch claude.state {
	case contract.ProjectUnifyFileStatePresent:
		r.ClaudeImportsAgents, r.ClaudeOnlyLines = splitRules(claude.text)
	case contract.ProjectUnifyFileStateLink:
		target := filepath.ToSlash(filepath.Clean(claude.target))
		r.ClaudeImportsAgents = target == agentsFile
		if !r.ClaudeImportsAgents {
			conflict(contract.ProjectUnifyConflictKindRulesLink, claudeFile,
				"CLAUDE.md is a link to "+claude.target+"; unify does not follow or change it.")
		}
	}
	agentsThere := agents.state == contract.ProjectUnifyFileStatePresent || agents.state == contract.ProjectUnifyFileStateLink
	claudeThere := claude.state != contract.ProjectUnifyFileStateMissing

	if agentsThere {
		r.Now.Codex = append(r.Now.Codex, agentsFile)
	}
	if claudeThere {
		r.Now.Claude = append(r.Now.Claude, claudeFile)
	}
	if agentsThere && (!claudeThere || r.ClaudeImportsAgents) {
		r.Now.Claude = append(r.Now.Claude, agentsFile)
	}
	r.After.Codex = append(r.After.Codex, r.Now.Codex...)
	r.After.Claude = append(r.After.Claude, r.Now.Claude...)

	if claude.state != contract.ProjectUnifyFileStatePresent || agents.state == contract.ProjectUnifyFileStateUnreadable {
		return r
	}
	switch {
	case agents.state == contract.ProjectUnifyFileStateMissing && len(r.ClaudeOnlyLines) > 0:
		shared := withoutImports(claude.text)
		after := importLine + "\n"
		p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: contract.ProjectUnifyActionKindRulesCreateAgents,
			Paths:       []string{agentsFile, claudeFile},
			Description: "Move CLAUDE.md's rules into a new AGENTS.md, and leave CLAUDE.md as one line that imports it.",
			Edits: []contract.ProjectUnifyFileEdit{{Path: agentsFile, After: shared},
				{Path: claudeFile, Before: &claude.text, After: after}}})
		r.After.Codex = []string{agentsFile}
		r.After.Claude = []string{claudeFile, agentsFile}
		r.ClaudeOnlyLines = []string{}
	case agents.state == contract.ProjectUnifyFileStateMissing && r.ClaudeImportsAgents:
		conflict(contract.ProjectUnifyConflictKindImportWithoutAgents, claudeFile,
			"CLAUDE.md imports AGENTS.md, which does not exist; write the shared rules there.")
	case agents.state == contract.ProjectUnifyFileStatePresent && !r.ClaudeImportsAgents:
		after := importLine + "\n" + claude.text
		p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: contract.ProjectUnifyActionKindRulesAddImport,
			Paths:       []string{claudeFile},
			Description: "Add an @AGENTS.md line at the top of CLAUDE.md so Claude reads AGENTS.md too; the rest of CLAUDE.md stays as it is.",
			Edits:       []contract.ProjectUnifyFileEdit{{Path: claudeFile, Before: &claude.text, After: after}}})
		r.After.Claude = append(r.After.Claude, agentsFile)
		if len(r.ClaudeOnlyLines) > 0 {
			conflict(contract.ProjectUnifyConflictKindClaudeOnlyLines, claudeFile,
				"Codex does not see these lines; move the shared ones into AGENTS.md.")
		}
	}
	return r
}

// readSide lists one skills directory without following any link in it.
func readSide(base *os.Root, rel string, digest hash.Hash) *side {
	s := &side{dir: rel, entries: map[string]*entry{}}
	parentRel, name := filepath.Dir(filepath.FromSlash(rel)), filepath.Base(rel)
	parent, err := openDirectory(base, parentRel)
	if errors.Is(err, os.ErrNotExist) {
		s.state = "missing"
		fmt.Fprintf(digest, "skills %s missing\n", rel)
		return s
	}
	if err != nil {
		s.state = "unreadable"
		if errors.Is(err, ErrUnsafe) {
			s.state, s.target = "link", "(the directory above it)"
		}
		fmt.Fprintf(digest, "skills %s %s\n", rel, s.state)
		return s
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.state = "missing"
	case err != nil || !info.IsDir() && info.Mode()&os.ModeSymlink == 0:
		s.state = "unreadable"
	case info.Mode()&os.ModeSymlink != 0:
		s.state = "link"
		s.target, err = parent.Readlink(name)
		if err != nil {
			s.state = "unreadable"
		}
	}
	if s.state != "" {
		fmt.Fprintf(digest, "skills %s %s %q\n", rel, s.state, s.target)
		return s
	}
	dir, err := openDirectory(parent, name)
	if err != nil {
		s.state = "unreadable"
		fmt.Fprintf(digest, "skills %s unreadable\n", rel)
		return s
	}
	defer dir.Close()
	names, err := readNames(dir)
	if err != nil {
		s.state = "unreadable"
		s.large = errors.Is(err, ErrLarge)
		fmt.Fprintf(digest, "skills %s unreadable\n", rel)
		return s
	}
	s.state = "plain"
	fmt.Fprintf(digest, "skills %s plain\n", rel)
	for _, n := range names {
		if strings.HasPrefix(n, ".") {
			continue
		}
		e := &entry{kind: "other"}
		info, err := dir.Lstat(n)
		switch {
		case err != nil:
			e.err = err
		case info.Mode()&os.ModeSymlink != 0:
			e.kind = "link"
			e.target, e.err = dir.Readlink(n)
		case info.IsDir():
			if _, err := lstatBeneath(dir, filepath.Join(n, "SKILL.md")); err == nil {
				e.kind = "skill"
				e.tree, e.err = treeDigest(dir, n)
			} else if !errors.Is(err, os.ErrNotExist) {
				e.err = err
			}
		}
		s.entries[n] = e
		fmt.Fprintf(digest, "entry %s/%s %s %q %s %t\n", rel, n, e.kind, e.target, e.tree, e.err != nil)
	}
	return s
}

// readNames lists a directory's names, sorted, refusing one past
// MaxScanEntries rather than planning on part of it.
func readNames(dir *os.Root) ([]string, error) {
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(MaxScanEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > MaxScanEntries {
		return nil, ErrLarge
	}
	sort.Strings(names)
	return names, nil
}

// treeDigest is one skill directory's content: every name, its type, the
// executable bit and the bytes of each file, and each link's target as it is
// spelled. Links are recorded, never followed. The walk stops with ErrLarge
// past MaxScanEntries entries in the whole tree.
func treeDigest(parent *os.Root, name string) (string, error) {
	h := sha256.New()
	budget := MaxScanEntries
	var walk func(dir *os.Root, prefix string) error
	walk = func(dir *os.Root, prefix string) error {
		names, err := readNames(dir)
		if err != nil {
			return err
		}
		for _, n := range names {
			if budget--; budget < 0 {
				return ErrLarge
			}
			rel := prefix + n
			info, err := dir.Lstat(n)
			if err != nil {
				return err
			}
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, err := dir.Readlink(n)
				if err != nil {
					return err
				}
				fmt.Fprintf(h, "l %q %q\n", rel, target)
			case info.IsDir():
				fmt.Fprintf(h, "d %q\n", rel)
				child, err := openDirectory(dir, n)
				if err != nil {
					return err
				}
				err = walk(child, rel+"/")
				child.Close()
				if err != nil {
					return err
				}
			case info.Mode().IsRegular():
				sum, err := fileDigest(dir, n)
				if err != nil {
					return err
				}
				fmt.Fprintf(h, "f %q %t %s\n", rel, info.Mode().Perm()&0111 != 0, sum)
			default:
				fmt.Fprintf(h, "o %q %s\n", rel, info.Mode().Type())
			}
		}
		return nil
	}
	dir, err := openDirectory(parent, name)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := walk(dir, ""); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileDigest(dir *os.Root, name string) (string, error) {
	parent, f, _, err := openPlain(dir, name)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func planSkills(a, c *side, p *contract.ProjectUnifyPlan,
	conflict func(contract.ProjectUnifyConflictKind, string, string)) {
	for _, s := range []*side{a, c} {
		switch s.state {
		case "unreadable":
			if s.large {
				conflict(contract.ProjectUnifyConflictKindTooLarge, s.dir, "This skills directory has more entries than unify reads.")
			} else {
				conflict(contract.ProjectUnifyConflictKindUnreadable, s.dir, "This skills directory could not be read.")
			}
		case "link":
			if s == c && filepath.ToSlash(filepath.Clean(s.target)) == "../.agents/skills" {
				continue // the whole directory is the shared one
			}
			conflict(contract.ProjectUnifyConflictKindSkillsDirectoryLink, s.dir,
				"This skills directory, or the one above it, is a link; unify does not follow or change it.")
		}
	}
	wholeLinked := c.state == "link" && filepath.ToSlash(filepath.Clean(c.target)) == "../.agents/skills"
	if a.state != "plain" && a.state != "missing" || c.state != "plain" && c.state != "missing" && !wholeLinked {
		return
	}
	names := map[string]bool{}
	for n, e := range a.entries {
		if e.kind != "other" || e.err != nil {
			names[n] = true
		}
	}
	for n, e := range c.entries {
		if e.kind != "other" || e.err != nil {
			names[n] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		ae, ce := a.entries[n], c.entries[n]
		if wholeLinked {
			ce = nil
		}
		row := contract.ProjectUnifySkill{Name: n}
		aPath, cPath := agentsSkills+"/"+n, claudeSkills+"/"+n
		bad := false
		for _, x := range []struct {
			e    *entry
			path string
		}{{ae, aPath}, {ce, cPath}} {
			if x.e == nil || x.e.err == nil {
				continue
			}
			bad = true
			if errors.Is(x.e.err, ErrLarge) {
				conflict(contract.ProjectUnifyConflictKindTooLarge, x.path, "This skill has more files than unify compares.")
			} else {
				conflict(contract.ProjectUnifyConflictKindUnreadable, x.path, "This skill could not be read.")
			}
		}
		if bad {
			continue
		}
		aSkill := ae != nil && ae.kind == "skill"
		cSkill := ce != nil && ce.kind == "skill"
		cLinked := ce != nil && ce.kind == "link" && ce.target != "" && !filepath.IsAbs(ce.target) &&
			filepath.ToSlash(filepath.Clean(filepath.Join(claudeSkills, ce.target))) == aPath
		row.Now = contract.ProjectUnifySeen{Codex: aSkill, Claude: cSkill || cLinked && aSkill || wholeLinked && aSkill}
		row.After = row.Now
		switch {
		case ae != nil && ae.kind == "link":
			conflict(contract.ProjectUnifyConflictKindSkillLinkElsewhere, aPath,
				"This skill is a link to "+ae.target+"; unify does not follow or change it.")
			row.Place = contract.ProjectUnifySkillPlaceAgents
		case ce != nil && ce.kind == "link" && !(cLinked && aSkill):
			conflict(contract.ProjectUnifyConflictKindSkillLinkElsewhere, cPath,
				"This skill is a link to "+ce.target+", not to "+aPath+"; unify does not follow or change it.")
			row.Place = contract.ProjectUnifySkillPlaceClaude
		case aSkill && (cLinked || wholeLinked):
			row.Place, row.Linked = contract.ProjectUnifySkillPlaceBothSame, true
		case aSkill && cSkill && ae.tree == ce.tree:
			row.Place = contract.ProjectUnifySkillPlaceBothSame
			if linksAvailable {
				row.Action = contract.ProjectUnifyActionKindSkillReplaceCopyWithLink
				p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: row.Action, Paths: []string{cPath, aPath},
					LinkTarget: skillLinkTarget(n), Edits: []contract.ProjectUnifyFileEdit{},
					Description: "Replace the identical copy " + cPath + " with a link to " + aPath + "."})
			} else {
				row.Copy = true
			}
		case aSkill && cSkill:
			row.Place = contract.ProjectUnifySkillPlaceBothDifferent
			conflict(contract.ProjectUnifyConflictKindSkillDiffers, cPath,
				"This skill differs from "+aPath+"; keep one version in "+aPath+" and remove the other.")
		case aSkill && ce != nil:
			row.Place = contract.ProjectUnifySkillPlaceAgents
			conflict(contract.ProjectUnifyConflictKindNameTaken, cPath,
				"Something that is not this skill already uses this name; move it before linking the skill here.")
		case aSkill:
			row.Place = contract.ProjectUnifySkillPlaceAgents
			row.After.Claude = true
			if linksAvailable {
				row.Action = contract.ProjectUnifyActionKindSkillLink
				p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: row.Action, Paths: []string{cPath},
					LinkTarget: skillLinkTarget(n), Edits: []contract.ProjectUnifyFileEdit{},
					Description: "Link " + cPath + " to " + aPath + " so Claude sees the skill Codex already sees."})
			} else {
				row.Action, row.Copy = contract.ProjectUnifyActionKindSkillCopy, true
				p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: row.Action, Paths: []string{aPath, cPath},
					Edits:       []contract.ProjectUnifyFileEdit{},
					Description: "Copy " + aPath + " to " + cPath + " so Claude sees it; this machine cannot make links, so the check compares the copies."})
			}
		case cSkill && ae != nil:
			row.Place = contract.ProjectUnifySkillPlaceClaude
			conflict(contract.ProjectUnifyConflictKindNameTaken, aPath,
				"Something that is not this skill already uses this name; move it before sharing the skill.")
		case cSkill:
			row.Place = contract.ProjectUnifySkillPlaceClaude
			row.After.Codex = true
			if linksAvailable {
				row.Action = contract.ProjectUnifyActionKindSkillMoveAndLink
				p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: row.Action, Paths: []string{cPath, aPath},
					LinkTarget: skillLinkTarget(n), Edits: []contract.ProjectUnifyFileEdit{},
					Description: "Move " + cPath + " to " + aPath + " and leave a link in its place, so Codex sees it too."})
			} else {
				row.Action, row.Copy = contract.ProjectUnifyActionKindSkillCopy, true
				p.Actions = append(p.Actions, contract.ProjectUnifyAction{Kind: row.Action, Paths: []string{cPath, aPath},
					Edits:       []contract.ProjectUnifyFileEdit{},
					Description: "Copy " + cPath + " to " + aPath + " so Codex sees it; this machine cannot make links, so the check compares the copies."})
			}
		default:
			continue // neither side holds a skill under this name
		}
		p.Skills = append(p.Skills, row)
	}
}

// Apply performs the plan the person read. It refuses ErrPlanChanged when
// disk no longer matches version and ErrPlanUnknown when part of it cannot be
// read. A failing action stops the run: the answer lists the actions that
// ran and the one that failed, with the plan read again from disk.
func Apply(place, version string) (contract.ProjectUnifyApplied, error) {
	root, _, err := projectLayers(place)
	if err != nil {
		return contract.ProjectUnifyApplied{}, err
	}
	mu := lockFor(root + "\x00unify")
	mu.Lock()
	defer mu.Unlock()
	st, err := planAt(place)
	if err != nil {
		return contract.ProjectUnifyApplied{}, err
	}
	if st.plan.Version != version {
		return contract.ProjectUnifyApplied{}, ErrPlanChanged
	}
	if st.plan.Status == contract.ProjectUnifyStatusUnknown {
		return contract.ProjectUnifyApplied{}, ErrPlanUnknown
	}
	out := contract.ProjectUnifyApplied{Outcome: contract.ProjectUnifyOutcomeApplied, Ran: []contract.ProjectUnifyAction{}}
	var failure error
	for _, action := range st.plan.Actions {
		if err := st.perform(action); err != nil {
			failed := action
			out.Outcome, out.Failed, failure = contract.ProjectUnifyOutcomeStopped, &failed, err
			break
		}
		out.Ran = append(out.Ran, action)
	}
	after, err := planAt(place)
	if err != nil {
		return out, err
	}
	out.Plan = after.plan
	return out, failure
}

func (st *unifyState) skillEntry(dir, name string) *entry {
	for _, s := range st.sides {
		if s.dir == dir {
			return s.entries[name]
		}
	}
	return nil
}

func (st *unifyState) perform(action contract.ProjectUnifyAction) error {
	base, err := os.OpenRoot(st.root)
	if err != nil {
		return err
	}
	defer base.Close()
	switch action.Kind {
	case contract.ProjectUnifyActionKindRulesCreateAgents:
		claude := candidate(st.root, claudeFile, "project", "claude", "instruction")
		current, err := read(claude)
		if err != nil {
			return err
		}
		if current.Text != st.claude.text {
			return ErrPlanChanged
		}
		info, err := base.Lstat(claudeFile)
		if err != nil {
			return err
		}
		if err := createText(base, agentsFile, action.Edits[0].After, info.Mode().Perm()); err != nil {
			return err
		}
		_, err = replaceText(claude, current.Version, action.Edits[1].After)
		return err
	case contract.ProjectUnifyActionKindRulesAddImport:
		claude := candidate(st.root, claudeFile, "project", "claude", "instruction")
		current, err := read(claude)
		if err != nil {
			return err
		}
		if current.Text != st.claude.text {
			return ErrPlanChanged
		}
		_, err = replaceText(claude, current.Version, action.Edits[0].After)
		return err
	}
	name := filepath.Base(action.Paths[0])
	aRel, cRel := filepath.FromSlash(agentsSkills+"/"+name), filepath.FromSlash(claudeSkills+"/"+name)
	switch action.Kind {
	case contract.ProjectUnifyActionKindSkillLink:
		return st.link(base, name)
	case contract.ProjectUnifyActionKindSkillMoveAndLink:
		planned := st.skillEntry(claudeSkills, name)
		if err := st.verify(base, claudeSkills, name, planned); err != nil {
			return err
		}
		if err := ensureDirectory(base, agentsSkills); err != nil {
			return err
		}
		if err := absent(base, aRel); err != nil {
			return err
		}
		// One rename inside the repository moves the whole tree at once:
		// there is no moment where half of it is in either place.
		if err := base.Rename(cRel, aRel); err != nil {
			return err
		}
		if err := st.verify(base, agentsSkills, name, planned); err != nil {
			return errors.Join(err, base.Rename(aRel, cRel))
		}
		if err := st.link(base, name); err != nil {
			return errors.Join(err, base.Rename(aRel, cRel))
		}
		return nil
	case contract.ProjectUnifyActionKindSkillReplaceCopyWithLink:
		if err := st.verify(base, agentsSkills, name, st.skillEntry(agentsSkills, name)); err != nil {
			return err
		}
		if err := st.verify(base, claudeSkills, name, st.skillEntry(claudeSkills, name)); err != nil {
			return err
		}
		aside := filepath.FromSlash(claudeSkills + "/." + name + "." + randomName() + ".unify")
		if err := base.Rename(cRel, aside); err != nil {
			return err
		}
		if err := st.link(base, name); err != nil {
			return errors.Join(err, base.Rename(aside, cRel))
		}
		if err := base.RemoveAll(aside); err != nil {
			return fmt.Errorf("the link is in place; the old copy is left at %s: %w", filepath.ToSlash(aside), err)
		}
		return nil
	case contract.ProjectUnifyActionKindSkillCopy:
		from, to := action.Paths[0], action.Paths[1]
		planned := st.skillEntry(filepath.ToSlash(filepath.Dir(from)), name)
		if err := st.verify(base, filepath.ToSlash(filepath.Dir(from)), name, planned); err != nil {
			return err
		}
		return copySkill(base, filepath.FromSlash(from), filepath.FromSlash(to), planned.tree)
	}
	return fmt.Errorf("unknown unify action %q", action.Kind)
}

// verify re-reads one skill directory and compares it with the tree the plan
// read, so an edit made after the person looked is never moved or removed.
func (st *unifyState) verify(base *os.Root, dir, name string, planned *entry) error {
	if planned == nil || planned.kind != "skill" {
		return ErrPlanChanged
	}
	parent, err := openDirectory(base, filepath.FromSlash(dir))
	if err != nil {
		return err
	}
	defer parent.Close()
	tree, err := treeDigest(parent, name)
	if err != nil {
		return err
	}
	if tree != planned.tree {
		return ErrPlanChanged
	}
	return nil
}

// link creates .claude/skills/<name> -> ../../.agents/skills/<name> and
// checks it resolves to that directory. Symlink never replaces an existing
// name, so a name that appeared since the plan is refused, not overwritten.
func (st *unifyState) link(base *os.Root, name string) error {
	if err := ensureDirectory(base, claudeSkills); err != nil {
		return err
	}
	dir, err := openDirectory(base, filepath.FromSlash(claudeSkills))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := absent(dir, name); err != nil {
		return err
	}
	target := skillLinkTarget(name)
	if err := dir.Symlink(filepath.FromSlash(target), name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrNameTaken
		}
		return err
	}
	got, err := dir.Readlink(name)
	linked, statErr := base.Stat(filepath.FromSlash(claudeSkills + "/" + name))
	want, wantErr := base.Stat(filepath.FromSlash(agentsSkills + "/" + name))
	if err != nil || filepath.ToSlash(got) != target || statErr != nil || wantErr != nil || !os.SameFile(linked, want) {
		return errors.Join(ErrUnsafe, dir.Remove(name))
	}
	return nil
}

func absent(dir *os.Root, rel string) error {
	_, err := dir.Lstat(rel)
	switch {
	case err == nil:
		return ErrNameTaken
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return err
	}
}

// ensureDirectory makes rel (a two-level path) as plain directories, and
// refuses one that exists as a link or a file.
func ensureDirectory(base *os.Root, rel string) error {
	rel = filepath.FromSlash(rel)
	if err := base.MkdirAll(rel, 0o755); err != nil {
		return err
	}
	dir, err := openDirectory(base, rel)
	if err != nil {
		return err
	}
	return dir.Close()
}

func randomName() string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return hex.EncodeToString(random)
}

// createText writes a new file that must not exist: a temporary file in the
// same directory, synced, then hard-linked to the name, which fails rather
// than replaces when the name was taken in between.
func createText(base *os.Root, name, text string, perm os.FileMode) error {
	if len(text) > MaxFileBytes {
		return ErrLarge
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return ErrText
	}
	if err := absent(base, name); err != nil {
		return err
	}
	tmpName := "." + name + "." + randomName() + ".tmp"
	tmp, err := base.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer base.Remove(tmpName)
	if _, err := io.WriteString(tmp, text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := base.Link(tmpName, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrNameTaken
		}
		return err
	}
	return nil
}

// copySkill copies a skill tree into a hidden sibling of its destination,
// checks the copy against the planned digest, and renames it into place. A
// failure removes the partial copy; the destination name is never replaced.
func copySkill(base *os.Root, from, to, tree string) error {
	parentRel, name := filepath.Dir(to), filepath.Base(to)
	if err := ensureDirectory(base, filepath.ToSlash(parentRel)); err != nil {
		return err
	}
	if err := absent(base, to); err != nil {
		return err
	}
	staging := filepath.Join(parentRel, "."+name+"."+randomName()+".unify")
	if err := base.Mkdir(staging, 0o755); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			base.RemoveAll(staging)
		}
	}()
	src, err := openDirectory(base, from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := openDirectory(base, staging)
	if err != nil {
		return err
	}
	budget := MaxScanEntries
	err = copyTree(src, dst, &budget)
	dst.Close()
	if err != nil {
		return err
	}
	parent, err := openDirectory(base, parentRel)
	if err != nil {
		return err
	}
	got, err := treeDigest(parent, filepath.Base(staging))
	parent.Close()
	if err != nil {
		return err
	}
	if got != tree {
		return ErrChanged
	}
	if err := absent(base, to); err != nil {
		return err
	}
	if err := base.Rename(staging, to); err != nil {
		return err
	}
	done = true
	return nil
}

func copyTree(src, dst *os.Root, budget *int) error {
	names, err := readNames(src)
	if err != nil {
		return err
	}
	for _, n := range names {
		if *budget--; *budget < 0 {
			return ErrLarge
		}
		info, err := src.Lstat(n)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := dst.Mkdir(n, info.Mode().Perm()|0o700); err != nil {
				return err
			}
			from, err := openDirectory(src, n)
			if err != nil {
				return err
			}
			to, err := openDirectory(dst, n)
			if err != nil {
				from.Close()
				return err
			}
			err = copyTree(from, to, budget)
			from.Close()
			to.Close()
			if err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(src, dst, n, info.Mode().Perm()); err != nil {
				return err
			}
		default:
			// A link or a device inside a skill cannot be copied faithfully
			// where links are unavailable; the person resolves it.
			return ErrUnsafe
		}
	}
	return nil
}

func copyFile(src, dst *os.Root, name string, perm os.FileMode) error {
	parent, in, _, err := openPlain(src, name)
	if err != nil {
		return err
	}
	defer parent.Close()
	defer in.Close()
	out, err := dst.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// UnifyRefusal maps an Apply error to the route's status, code and sentence.
func UnifyRefusal(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrPlanChanged):
		return 409, "plan_changed", "The Project's rules or skills changed since this plan was read. Read the plan again."
	case errors.Is(err, ErrPlanUnknown):
		return 503, "plan_unknown", "Part of the Project's rules or skills could not be read, so nothing was changed. Read the plan for what."
	case errors.Is(err, ErrNameTaken):
		return 409, "name_taken", "A file or link already uses a name unify would create; nothing there was replaced."
	}
	return Refusal(err)
}
