package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline project unify` shows, and on --apply performs, the plan that
// gives one Project's Claude and Codex sessions the same rules file and the
// same skills (docs/project-files.md, Unify). The daemon computes the plan;
// this command only reads it, prints it for a person, and sends back the
// version the person saw.

// Exit codes of --check, and of a plan read: 0 unified, 1 drifting, 3 unknown
// (the plan or the daemon could not be read).
const (
	unifyExitUnified  = 0
	unifyExitDrifting = 1
	unifyExitUnknown  = 3
)

type unifyClient struct {
	base   string
	token  string
	client *http.Client
}

func openUnifyClient() (*unifyClient, error) {
	port, err := daemonPort()
	if err != nil {
		return nil, err
	}
	token, err := localToken(config.Load())
	if err != nil {
		return nil, err
	}
	return &unifyClient{base: "http://127.0.0.1:" + strconv.Itoa(port), token: token,
		client: &http.Client{Timeout: 30 * time.Second}}, nil
}

// call returns the status and body; a transport failure is an error, a
// refusal is a status the caller reads.
func (c *unifyClient) call(method, path string, body []byte, key string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf(cliCopy("unify", "daemon_no_answer", "the daemon did not answer: %w"), err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf(cliCopy("unify", "daemon_unreadable", "the daemon's answer could not be read: %w"), err)
	}
	return res.StatusCode, data, nil
}

func refusalOf(status int, data []byte) error {
	if refusal, ok := parseCLIHTTPRefusal(data); ok {
		return fmt.Errorf("%s (%s)", refusal.humanDetail(currentCLILanguage()), refusal.Code)
	}
	return fmt.Errorf(cliCopy("unify", "daemon_answered_status", "the daemon answered %d"), status)
}

// placeFor finds the daemon's Project whose repository is dir's.
func (c *unifyClient) placeFor(dir string) (string, error) {
	want, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		return "", fmt.Errorf(cliCopy("unify", "not_git_repository", "%s is not inside a Git repository"), dir)
	}
	status, data, err := c.call(http.MethodGet, "/v1/places", nil, "")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", refusalOf(status, data)
	}
	var list contract.StartPlaceList
	if err := json.Unmarshal(data, &list); err != nil {
		return "", fmt.Errorf(cliCopy("unify", "project_list_unreadable", "the daemon's Project list was not readable: %w"), err)
	}
	for _, p := range list.Places {
		if got, ok := projects.CanonicalProjectKey(p.Path); ok && got == want {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf(cliCopy("unify", "project_not_listed", "this machine does not list %s as a Project; run `clawdline project add %s` first"), want, want)
}

func gitTopLevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf(cliCopy("unify", "not_git_repository", "%s is not inside a Git repository"), dir)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func projectUnifyCommand(args []string) {
	fs := flag.NewFlagSet("project unify", flag.ExitOnError)
	apply := fs.Bool("apply", false, cliCopy("unify", "flag_apply", "apply the plan just printed"))
	check := fs.Bool("check", false, cliCopy("unify", "flag_check", "print only the status and drift; exit 0 unified, 1 drifting, 3 unknown"))
	asJSON := fs.Bool("json", false, cliCopy("unify", "flag_json", "print the daemon's answer"))
	_ = fs.Parse(args)
	if fs.NArg() > 1 || *apply && *check {
		fmt.Fprintln(os.Stderr, cliCopy("unify", "usage", "usage: clawdline project unify [--apply | --check] [--json] [directory]"))
		os.Exit(2)
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir, err = gitTopLevel(abs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(unifyExitUnknown)
	}
	c, err := openUnifyClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(unifyExitUnknown)
	}
	os.Exit(runProjectUnify(os.Stdout, os.Stderr, c, dir, *apply, *check, *asJSON))
}

func runProjectUnify(stdout, stderr io.Writer, c *unifyClient, dir string, apply, check, asJSON bool) int {
	place, err := c.placeFor(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline:", err)
		return unifyExitUnknown
	}
	path := "/v1/projects/" + url.PathEscape(place) + "/unify"
	status, data, err := c.call(http.MethodGet, path, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline:", err)
		return unifyExitUnknown
	}
	if status != http.StatusOK {
		fmt.Fprintln(stderr, "clawdline:", refusalOf(status, data))
		return unifyExitUnknown
	}
	var plan contract.ProjectUnifyPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(stderr, cliCopy("unify", "plan_unreadable", "clawdline: the plan was not readable:"), err)
		return unifyExitUnknown
	}
	switch {
	case check:
		if asJSON {
			stdout.Write(indentJSON(data))
		} else {
			printUnifyCheck(stdout, plan)
		}
		return unifyExit(plan.Status)
	case !apply:
		if asJSON {
			stdout.Write(indentJSON(data))
		} else {
			printUnifyPlan(stdout, dir, plan)
		}
		return unifyExit(plan.Status)
	}
	if !asJSON {
		printUnifyPlan(stdout, dir, plan)
	}
	if plan.Status == contract.ProjectUnifyStatusUnknown {
		fmt.Fprintln(stderr, cliCopy("unify", "plan_part_unreadable", "clawdline: part of the plan could not be read, so nothing was applied."))
		return unifyExitUnknown
	}
	if len(plan.Actions) == 0 {
		if !asJSON {
			fmt.Fprintln(stdout, cliCopy("unify", "nothing_to_apply", "\nNothing to apply."))
		} else {
			stdout.Write(indentJSON(data))
		}
		return unifyExit(plan.Status)
	}
	body, _ := json.Marshal(contract.ProjectUnifyApply{Version: plan.Version})
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	status, data, err = c.call(http.MethodPost, path, body, "unify-"+hex.EncodeToString(key))
	if err != nil {
		// The request may have run. Reading the plan again says what disk holds.
		fmt.Fprintln(stderr, "clawdline:", err, cliCopy("unify", "apply_unknown", "— whether it applied is unknown; run `clawdline project unify` to see."))
		return unifyExitUnknown
	}
	var out contract.ProjectUnifyApplied
	decoded := json.Unmarshal(data, &out) == nil
	switch {
	case status == http.StatusOK && decoded:
	case status == http.StatusOK:
		fmt.Fprintln(stderr, cliCopy("unify", "answer_unreadable", "clawdline: the answer was not readable; run `clawdline project unify` to see what disk holds."))
		return unifyExitUnknown
	case decoded && out.Outcome == contract.ProjectUnifyOutcomeStopped:
		// Stopped part-way: printed below with what ran.
	default:
		fmt.Fprintln(stderr, cliCopy("unify", "nothing_applied", "clawdline: nothing was applied:"), refusalOf(status, data))
		return unifyExitDrifting
	}
	if asJSON {
		stdout.Write(indentJSON(data))
	} else {
		fmt.Fprintf(stdout, cliCopy("unify", "applied_count", "\nApplied %d of %d actions:\n"), len(out.Ran), len(plan.Actions))
		for i, a := range out.Ran {
			fmt.Fprintf(stdout, "  %d. %s\n", i+1, unifyActionDescription(a))
		}
		if out.Failed != nil {
			fmt.Fprintf(stdout, cliCopy("unify", "stopped_at", "Stopped at: %s\n  %s (%s)\n"), unifyActionDescription(*out.Failed), out.Detail, out.Error)
		}
		fmt.Fprintln(stdout, cliCopy("unify", "nothing_committed", "Nothing was committed to git."))
		fmt.Fprintln(stdout)
		printUnifyCheck(stdout, out.Plan)
	}
	if out.Outcome == contract.ProjectUnifyOutcomeStopped {
		return unifyExitDrifting
	}
	return unifyExit(out.Plan.Status)
}

func unifyExit(status contract.ProjectUnifyStatus) int {
	switch status {
	case contract.ProjectUnifyStatusUnified:
		return unifyExitUnified
	case contract.ProjectUnifyStatusDrifting:
		return unifyExitDrifting
	}
	return unifyExitUnknown
}

func indentJSON(data []byte) []byte {
	var b bytes.Buffer
	if json.Indent(&b, data, "", "  ") != nil {
		return append(data, '\n')
	}
	b.WriteByte('\n')
	return b.Bytes()
}

func printUnifyCheck(w io.Writer, p contract.ProjectUnifyPlan) {
	fmt.Fprintf(w, "status: %s\n", p.Status)
	for _, a := range p.Actions {
		fmt.Fprintf(w, "drift: %s\n", a.Description)
	}
	for _, c := range p.Conflicts {
		fmt.Fprintf(w, "conflict: %s: %s\n", c.Path, c.Detail)
	}
}

func seenText(s contract.ProjectUnifySeen) string {
	switch {
	case s.Claude && s.Codex:
		return cliCopy("unify", "seen_both", "Claude and Codex")
	case s.Claude:
		return cliCopy("unify", "seen_claude", "Claude only")
	case s.Codex:
		return cliCopy("unify", "seen_codex", "Codex only")
	}
	return cliCopy("unify", "seen_neither", "neither")
}

func unifyStatusText(status contract.ProjectUnifyStatus) string {
	switch status {
	case contract.ProjectUnifyStatusUnified:
		return cliCopy("unify", "status_unified", "unified")
	case contract.ProjectUnifyStatusDrifting:
		return cliCopy("unify", "status_drifting", "drifting")
	case contract.ProjectUnifyStatusUnknown:
		return cliCopy("unify", "status_unknown", "unknown")
	}
	return string(status)
}

func unifyPlaceText(place contract.ProjectUnifySkillPlace) string {
	switch place {
	case contract.ProjectUnifySkillPlaceAgents:
		return cliCopy("unify", "place_agents", "agents")
	case contract.ProjectUnifySkillPlaceClaude:
		return cliCopy("unify", "place_claude", "claude")
	case contract.ProjectUnifySkillPlaceBothSame:
		return cliCopy("unify", "place_both_same", "both, same")
	case contract.ProjectUnifySkillPlaceBothDifferent:
		return cliCopy("unify", "place_both_different", "both, different")
	}
	return string(place)
}

func listText(paths []string) string {
	if len(paths) == 0 {
		return cliCopy("unify", "list_nothing", "nothing")
	}
	return strings.Join(paths, ", ")
}

func printUnifyPlan(w io.Writer, dir string, p contract.ProjectUnifyPlan) {
	fmt.Fprintf(w, cliCopy("unify", "plan_header", "Project: %s\nStatus: %s\n\n"), dir, unifyStatusText(p.Status))
	fmt.Fprintln(w, cliCopy("unify", "rules", "Rules"))
	t := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(t, cliCopy("unify", "claude_reads_now", "  Claude reads now:\t%s\n"), listText(p.Rules.Now.Claude))
	fmt.Fprintf(t, cliCopy("unify", "claude_will_read", "  Claude will read:\t%s\n"), listText(p.Rules.After.Claude))
	fmt.Fprintf(t, cliCopy("unify", "codex_reads_now", "  Codex reads now:\t%s\n"), listText(p.Rules.Now.Codex))
	fmt.Fprintf(t, cliCopy("unify", "codex_will_read", "  Codex will read:\t%s\n"), listText(p.Rules.After.Codex))
	_ = t.Flush()
	if len(p.Rules.ClaudeOnlyLines) > 0 {
		fmt.Fprintln(w, cliCopy("unify", "claude_only_lines", "  Lines in CLAUDE.md that Codex does not see:"))
		for _, line := range p.Rules.ClaudeOnlyLines {
			fmt.Fprintf(w, "    | %s\n", line)
		}
	}
	if len(p.Rules.ClaudeOnlyFiles) > 0 {
		fmt.Fprintf(w, cliCopy("unify", "claude_only_files", "  Read by Claude only, never changed here: %s\n"), strings.Join(p.Rules.ClaudeOnlyFiles, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, cliCopy("unify", "skills", "Skills"))
	if len(p.Skills) == 0 {
		fmt.Fprintln(w, cliCopy("unify", "skills_none", "  none in .agents/skills or .claude/skills"))
	} else {
		t = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(t, cliCopy("unify", "skills_header", "  NAME\tSEEN NOW BY\tWILL BE SEEN BY\tWHERE"))
		for _, s := range p.Skills {
			where := unifyPlaceText(s.Place)
			if s.Linked {
				where += cliCopy("unify", "linked", ", linked")
			}
			if s.Copy {
				where += cliCopy("unify", "copies", ", copies")
			}
			fmt.Fprintf(t, "  %s\t%s\t%s\t%s\n", s.Name, seenText(s.Now), seenText(s.After), where)
		}
		_ = t.Flush()
	}
	fmt.Fprintln(w)
	if len(p.Actions) == 0 {
		fmt.Fprintln(w, cliCopy("unify", "actions_none", "Actions: none"))
	} else {
		fmt.Fprintln(w, cliCopy("unify", "actions_order", "Actions, in order"))
		for i, a := range p.Actions {
			fmt.Fprintf(w, "  %d. %s\n", i+1, unifyActionDescription(a))
		}
	}
	if len(p.Conflicts) > 0 {
		fmt.Fprintln(w, cliCopy("unify", "conflicts_heading", "\nFor you to resolve (unify does not change these)"))
		for _, c := range p.Conflicts {
			fmt.Fprintf(w, "  - %s: %s\n", c.Path, unifyConflictDescription(c))
		}
	}
	if len(p.Actions) > 0 {
		fmt.Fprintf(w, cliCopy("unify", "apply_hint", "\nNothing has been changed. `clawdline project unify --apply` performs the %d actions above.\n"), len(p.Actions))
	}
}

// The plan's description remains the English wire value. The human view may
// translate it only when the action kind, paths and exact source sentence all
// agree with an action this build knows. A newer server sentence is shown raw.
func unifyActionDescription(a contract.ProjectUnifyAction) string {
	var key, english string
	var args []any
	switch a.Kind {
	case contract.ProjectUnifyActionKindRulesCreateAgents:
		key, english = "action_create_agents", "Move CLAUDE.md's rules into a new AGENTS.md, and leave CLAUDE.md as one line that imports it."
		if len(a.Paths) != 2 || a.Paths[0] != "AGENTS.md" || a.Paths[1] != "CLAUDE.md" {
			return a.Description
		}
	case contract.ProjectUnifyActionKindRulesAddImport:
		key, english = "action_add_import", "Add an @AGENTS.md line at the top of CLAUDE.md so Claude reads AGENTS.md too; the rest of CLAUDE.md stays as it is."
		if len(a.Paths) != 1 || a.Paths[0] != "CLAUDE.md" {
			return a.Description
		}
	case contract.ProjectUnifyActionKindSkillReplaceCopyWithLink:
		key, english = "action_replace_copy", "Replace the identical copy %s with a link to %s."
		if len(a.Paths) != 2 {
			return a.Description
		}
		args = []any{a.Paths[0], a.Paths[1]}
	case contract.ProjectUnifyActionKindSkillLink:
		key, english = "action_link_skill", "Link %s to %s so Claude sees the skill Codex already sees."
		if len(a.Paths) != 1 || a.LinkTarget == "" {
			return a.Description
		}
		args = []any{a.Paths[0], path.Clean(path.Join(path.Dir(a.Paths[0]), a.LinkTarget))}
	case contract.ProjectUnifyActionKindSkillMoveAndLink:
		key, english = "action_move_skill", "Move %s to %s and leave a link in its place, so Codex sees it too."
		if len(a.Paths) != 2 {
			return a.Description
		}
		args = []any{a.Paths[0], a.Paths[1]}
	case contract.ProjectUnifyActionKindSkillCopy:
		if len(a.Paths) != 2 {
			return a.Description
		}
		args = []any{a.Paths[0], a.Paths[1]}
		if strings.HasPrefix(a.Paths[0], ".agents/skills/") {
			key, english = "action_copy_to_claude", "Copy %s to %s so Claude sees it; this machine cannot make links, so the check compares the copies."
		} else if strings.HasPrefix(a.Paths[0], ".claude/skills/") {
			key, english = "action_copy_to_codex", "Copy %s to %s so Codex sees it; this machine cannot make links, so the check compares the copies."
		} else {
			return a.Description
		}
	default:
		return a.Description
	}
	if fmt.Sprintf(english, args...) != a.Description {
		return a.Description
	}
	return fmt.Sprintf(cliCopy("unify", key, english), args...)
}

// Conflict detail is daemon-authored English wire text. Translate only the
// source sentences and typed paths this CLI knows; a newer server's sentence
// stays raw, and --check/--json keep the original wire value.
func unifyConflictDescription(c contract.ProjectUnifyConflict) string {
	known := func(key, english string) string {
		if c.Detail == english {
			return cliCopy("unify", key, english)
		}
		return c.Detail
	}
	target := func(prefix, suffix, key, english string) string {
		value, ok := strings.CutPrefix(c.Detail, prefix)
		if !ok {
			return c.Detail
		}
		value, ok = strings.CutSuffix(value, suffix)
		if !ok || value == "" || fmt.Sprintf(english, value) != c.Detail {
			return c.Detail
		}
		return fmt.Sprintf(cliCopy("unify", key, english), value)
	}
	switch c.Kind {
	case contract.ProjectUnifyConflictKindUnreadable:
		switch {
		case c.Path == ".claude/CLAUDE.md" || c.Path == "CLAUDE.local.md" || c.Path == ".claude/CLAUDE.local.md":
			return known("conflict_claude_rules_unchecked", "This Claude rules file could not be checked.")
		case c.Path == "CLAUDE.md":
			return known("conflict_utf8_unreadable", "This file could not be read as UTF-8 text.")
		case c.Path == "AGENTS.md":
			return known("conflict_utf8_unreadable", "This file could not be read as UTF-8 text.")
		case c.Path == ".agents/skills" || c.Path == ".claude/skills":
			return known("conflict_skills_unreadable", "This skills directory could not be read.")
		case strings.HasPrefix(c.Path, ".agents/skills/") || strings.HasPrefix(c.Path, ".claude/skills/"):
			return known("conflict_skill_unreadable", "This skill could not be read.")
		}
	case contract.ProjectUnifyConflictKindTooLarge:
		switch {
		case c.Path == "AGENTS.md" || c.Path == "CLAUDE.md":
			return known("conflict_file_too_large", "This file is larger than unify reads; check it on the machine.")
		case c.Path == ".agents/skills" || c.Path == ".claude/skills":
			return known("conflict_skills_too_large", "This skills directory has more entries than unify reads.")
		case strings.HasPrefix(c.Path, ".agents/skills/") || strings.HasPrefix(c.Path, ".claude/skills/"):
			return known("conflict_skill_too_large", "This skill has more files than unify compares.")
		}
	case contract.ProjectUnifyConflictKindRulesLink:
		if c.Path == "AGENTS.md" {
			return target("AGENTS.md is a link to ", "; unify does not follow or change it.", "conflict_agents_link", "AGENTS.md is a link to %s; unify does not follow or change it.")
		}
		if c.Path == "CLAUDE.md" {
			return target("CLAUDE.md is a link to ", "; unify does not follow or change it.", "conflict_claude_link", "CLAUDE.md is a link to %s; unify does not follow or change it.")
		}
	case contract.ProjectUnifyConflictKindImportWithoutAgents:
		if c.Path == "CLAUDE.md" {
			return known("conflict_import_without_agents", "CLAUDE.md imports AGENTS.md, which does not exist; write the shared rules there.")
		}
	case contract.ProjectUnifyConflictKindClaudeOnlyLines:
		if c.Path == "CLAUDE.md" {
			return known("conflict_claude_only_lines", "Codex does not see these lines; move the shared ones into AGENTS.md.")
		}
	case contract.ProjectUnifyConflictKindSkillsDirectoryLink:
		if c.Path == ".agents/skills" || c.Path == ".claude/skills" {
			return known("conflict_skills_link", "This skills directory, or the one above it, is a link; unify does not follow or change it.")
		}
	case contract.ProjectUnifyConflictKindSkillLinkElsewhere:
		if strings.HasPrefix(c.Path, ".agents/skills/") {
			return target("This skill is a link to ", "; unify does not follow or change it.", "conflict_skill_link", "This skill is a link to %s; unify does not follow or change it.")
		}
		if strings.HasPrefix(c.Path, ".claude/skills/") {
			other := ".agents/skills/" + strings.TrimPrefix(c.Path, ".claude/skills/")
			value, ok := strings.CutPrefix(c.Detail, "This skill is a link to ")
			if !ok {
				return c.Detail
			}
			value, ok = strings.CutSuffix(value, ", not to "+other+"; unify does not follow or change it.")
			if !ok || value == "" {
				return c.Detail
			}
			return fmt.Sprintf(cliCopy("unify", "conflict_skill_link_elsewhere", "This skill is a link to %s, not to %s; unify does not follow or change it."), value, other)
		}
	case contract.ProjectUnifyConflictKindSkillDiffers:
		if strings.HasPrefix(c.Path, ".claude/skills/") {
			other := ".agents/skills/" + strings.TrimPrefix(c.Path, ".claude/skills/")
			if c.Detail == fmt.Sprintf("This skill differs from %s; keep one version in %s and remove the other.", other, other) {
				return fmt.Sprintf(cliCopy("unify", "conflict_skill_differs", "This skill differs from %s; keep one version in %s and remove the other."), other, other)
			}
		}
	case contract.ProjectUnifyConflictKindNameTaken:
		if strings.HasPrefix(c.Path, ".claude/skills/") {
			return known("conflict_name_before_link", "Something that is not this skill already uses this name; move it before linking the skill here.")
		}
		if strings.HasPrefix(c.Path, ".agents/skills/") {
			return known("conflict_name_before_share", "Something that is not this skill already uses this name; move it before sharing the skill.")
		}
	}
	return c.Detail
}
