package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

// unifyDaemon answers /v1/places with one Project at dir and its plan with
// plan; it records each apply it receives.
func unifyDaemon(t *testing.T, dir string, plan contract.ProjectUnifyPlan, applyStatus int, applied any) (*unifyClient, *[]string) {
	t.Helper()
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer local" {
			t.Errorf("credential %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/v1/places":
			_ = json.NewEncoder(w).Encode(contract.StartPlaceList{Places: []contract.StartPlace{{ID: "p1", Path: dir}}})
		case r.URL.Path == "/v1/projects/p1/unify" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(plan)
		case r.URL.Path == "/v1/projects/p1/unify" && r.Method == http.MethodPost:
			var in contract.ProjectUnifyApply
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.Version != plan.Version || r.Header.Get("Idempotency-Key") == "" {
				t.Errorf("apply sent %+v key %q", in, r.Header.Get("Idempotency-Key"))
			}
			w.WriteHeader(applyStatus)
			_ = json.NewEncoder(w).Encode(applied)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return &unifyClient{base: srv.URL, token: "local", client: srv.Client()}, &calls
}

func unifyCheckout(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProjectUnifyCatalogShipsItsNineLanguageBaseline(t *testing.T) {
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("unify", language)
		if total != 72 || translated != total {
			t.Errorf("unify coverage %s = %d/%d, want 72/72", language, translated, total)
		}
	}
}

func TestProjectUnifyConflictCopyRequiresExactKnownSource(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	other := ".agents/skills/example"
	for _, tc := range []contract.ProjectUnifyConflict{
		{Kind: contract.ProjectUnifyConflictKindUnreadable, Path: ".claude/CLAUDE.local.md", Detail: "This Claude rules file could not be checked."},
		{Kind: contract.ProjectUnifyConflictKindUnreadable, Path: "AGENTS.md", Detail: "This file could not be read as UTF-8 text."},
		{Kind: contract.ProjectUnifyConflictKindUnreadable, Path: ".agents/skills", Detail: "This skills directory could not be read."},
		{Kind: contract.ProjectUnifyConflictKindUnreadable, Path: other, Detail: "This skill could not be read."},
		{Kind: contract.ProjectUnifyConflictKindTooLarge, Path: "AGENTS.md", Detail: "This file is larger than unify reads; check it on the machine."},
		{Kind: contract.ProjectUnifyConflictKindTooLarge, Path: ".claude/skills", Detail: "This skills directory has more entries than unify reads."},
		{Kind: contract.ProjectUnifyConflictKindTooLarge, Path: other, Detail: "This skill has more files than unify compares."},
		{Kind: contract.ProjectUnifyConflictKindRulesLink, Path: "AGENTS.md", Detail: "AGENTS.md is a link to outside; unify does not follow or change it."},
		{Kind: contract.ProjectUnifyConflictKindRulesLink, Path: "CLAUDE.md", Detail: "CLAUDE.md is a link to outside; unify does not follow or change it."},
		{Kind: contract.ProjectUnifyConflictKindImportWithoutAgents, Path: "CLAUDE.md", Detail: "CLAUDE.md imports AGENTS.md, which does not exist; write the shared rules there."},
		{Kind: contract.ProjectUnifyConflictKindClaudeOnlyLines, Path: "CLAUDE.md", Detail: "Codex does not see these lines; move the shared ones into AGENTS.md."},
		{Kind: contract.ProjectUnifyConflictKindSkillsDirectoryLink, Path: ".claude/skills", Detail: "This skills directory, or the one above it, is a link; unify does not follow or change it."},
		{Kind: contract.ProjectUnifyConflictKindSkillLinkElsewhere, Path: other, Detail: "This skill is a link to outside; unify does not follow or change it."},
		{Kind: contract.ProjectUnifyConflictKindSkillLinkElsewhere, Path: ".claude/skills/example", Detail: "This skill is a link to outside, not to " + other + "; unify does not follow or change it."},
		{Kind: contract.ProjectUnifyConflictKindSkillDiffers, Path: ".claude/skills/example", Detail: "This skill differs from " + other + "; keep one version in " + other + " and remove the other."},
		{Kind: contract.ProjectUnifyConflictKindNameTaken, Path: ".claude/skills/example", Detail: "Something that is not this skill already uses this name; move it before linking the skill here."},
		{Kind: contract.ProjectUnifyConflictKindNameTaken, Path: other, Detail: "Something that is not this skill already uses this name; move it before sharing the skill."},
	} {
		if got := unifyConflictDescription(tc); got == tc.Detail || got == "" {
			t.Errorf("known %s conflict was not translated: %q", tc.Kind, got)
		}
		changed := tc
		changed.Detail += " New server clause."
		if got := unifyConflictDescription(changed); got != changed.Detail {
			t.Errorf("unknown %s sentence was guessed: %q", tc.Kind, got)
		}
	}
}

func TestProjectUnifyHumanEnumsLeaveUnknownWireValuesRaw(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	if got := unifyStatusText(contract.ProjectUnifyStatusDrifting); got != "mit Abweichungen" {
		t.Errorf("German human status = %q", got)
	}
	if got := unifyPlaceText(contract.ProjectUnifySkillPlaceBothDifferent); got != "auf beiden Seiten unterschiedlich" {
		t.Errorf("German human place = %q", got)
	}
	if got := unifyStatusText(contract.ProjectUnifyStatus("future")); got != "future" {
		t.Errorf("unknown status = %q", got)
	}
	if got := unifyPlaceText(contract.ProjectUnifySkillPlace("future")); got != "future" {
		t.Errorf("unknown place = %q", got)
	}
}

func TestProjectUnifyActionCopyRequiresExactKnownSource(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	c, a := ".claude/skills/example", ".agents/skills/example"
	for _, tc := range []contract.ProjectUnifyAction{
		{Kind: contract.ProjectUnifyActionKindRulesCreateAgents, Paths: []string{"AGENTS.md", "CLAUDE.md"},
			Description: "Move CLAUDE.md's rules into a new AGENTS.md, and leave CLAUDE.md as one line that imports it."},
		{Kind: contract.ProjectUnifyActionKindRulesAddImport, Paths: []string{"CLAUDE.md"},
			Description: "Add an @AGENTS.md line at the top of CLAUDE.md so Claude reads AGENTS.md too; the rest of CLAUDE.md stays as it is."},
		{Kind: contract.ProjectUnifyActionKindSkillReplaceCopyWithLink, Paths: []string{c, a},
			Description: "Replace the identical copy " + c + " with a link to " + a + "."},
		{Kind: contract.ProjectUnifyActionKindSkillLink, Paths: []string{c}, LinkTarget: "../../.agents/skills/example",
			Description: "Link " + c + " to " + a + " so Claude sees the skill Codex already sees."},
		{Kind: contract.ProjectUnifyActionKindSkillMoveAndLink, Paths: []string{c, a},
			Description: "Move " + c + " to " + a + " and leave a link in its place, so Codex sees it too."},
		{Kind: contract.ProjectUnifyActionKindSkillCopy, Paths: []string{a, c},
			Description: "Copy " + a + " to " + c + " so Claude sees it; this machine cannot make links, so the check compares the copies."},
		{Kind: contract.ProjectUnifyActionKindSkillCopy, Paths: []string{c, a},
			Description: "Copy " + c + " to " + a + " so Codex sees it; this machine cannot make links, so the check compares the copies."},
	} {
		if got := unifyActionDescription(tc); got == tc.Description || got == "" {
			t.Errorf("known %s action was not translated: %q", tc.Kind, got)
		}
		changed := tc
		changed.Description += " New server clause."
		if got := unifyActionDescription(changed); got != changed.Description {
			t.Errorf("unknown %s sentence was guessed: %q", tc.Kind, got)
		}
	}
}

func TestProjectUnifyGermanHumanCopyKeepsMachineOutputsAndPlanData(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	dir := unifyCheckout(t)
	plan := contract.ProjectUnifyPlan{Status: "drifting", Version: "v1",
		Actions: []contract.ProjectUnifyAction{{Kind: "skill_link", Description: "Link it."}},
		Conflicts: []contract.ProjectUnifyConflict{{Kind: contract.ProjectUnifyConflictKindImportWithoutAgents,
			Path: "CLAUDE.md", Detail: "CLAUDE.md imports AGENTS.md, which does not exist; write the shared rules there."}}}
	client, _ := unifyDaemon(t, dir, plan, 200, nil)
	var out, errs bytes.Buffer
	if code := runProjectUnify(&out, &errs, client, dir, false, false, false); code != unifyExitDrifting ||
		!strings.Contains(out.String(), "Aktionen in Ausführungsreihenfolge") ||
		!strings.Contains(out.String(), "Link it.") || !strings.Contains(out.String(), "Project: "+dir) ||
		!strings.Contains(out.String(), "Status: mit Abweichungen") ||
		!strings.Contains(out.String(), "CLAUDE.md bindet AGENTS.md ein") {
		t.Fatalf("localized plan changed source data: exit %d, output %q, errors %q", code, out.String(), errs.String())
	}
	out.Reset()
	if code := runProjectUnify(&out, &errs, client, dir, false, true, false); code != unifyExitDrifting ||
		out.String() != "status: drifting\ndrift: Link it.\nconflict: CLAUDE.md: CLAUDE.md imports AGENTS.md, which does not exist; write the shared rules there.\n" {
		t.Fatalf("--check machine output changed: exit %d, output %q", code, out.String())
	}
	out.Reset()
	if code := runProjectUnify(&out, &errs, client, dir, false, false, true); code != unifyExitDrifting {
		t.Fatalf("--json exit = %d", code)
	}
	var got contract.ProjectUnifyPlan
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Status != plan.Status ||
		len(got.Actions) != 1 || got.Actions[0].Description != plan.Actions[0].Description ||
		len(got.Conflicts) != 1 || got.Conflicts[0].Detail != plan.Conflicts[0].Detail {
		t.Fatalf("--json data changed: %#v, %v", got, err)
	}
}

func TestProjectUnifyCheckExitCodes(t *testing.T) {
	dir := unifyCheckout(t)
	drift := contract.ProjectUnifyPlan{Status: "drifting", Version: "v1",
		Actions:   []contract.ProjectUnifyAction{{Kind: "skill_link", Description: "Link it."}},
		Conflicts: []contract.ProjectUnifyConflict{{Kind: "skill_differs", Path: ".claude/skills/x", Detail: "Differs."}}}
	for _, c := range []struct {
		plan contract.ProjectUnifyPlan
		code int
		want string
	}{
		{contract.ProjectUnifyPlan{Status: "unified", Version: "v"}, 0, "status: unified\n"},
		{drift, 1, "status: drifting\ndrift: Link it.\nconflict: .claude/skills/x: Differs.\n"},
		{contract.ProjectUnifyPlan{Status: "unknown", Version: "v"}, 3, "status: unknown\n"},
	} {
		client, calls := unifyDaemon(t, dir, c.plan, 200, nil)
		var out, errs bytes.Buffer
		if code := runProjectUnify(&out, &errs, client, dir, false, true, false); code != c.code || out.String() != c.want {
			t.Fatalf("%s: code %d output %q errors %q", c.plan.Status, code, out.String(), errs.String())
		}
		if len(*calls) != 2 {
			t.Fatalf("--check made calls %v", *calls)
		}
	}
}

func TestProjectUnifyCheckIsUnknownWhenTheDaemonCannotSay(t *testing.T) {
	dir := unifyCheckout(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	var out, errs bytes.Buffer
	if code := runProjectUnify(&out, &errs, &unifyClient{base: srv.URL, token: "local", client: srv.Client()}, dir, false, true, false); code != 3 {
		t.Fatalf("a refused read gave %d", code)
	}
	client, _ := unifyDaemon(t, unifyCheckout(t), contract.ProjectUnifyPlan{Status: "unified"}, 200, nil)
	errs.Reset()
	if code := runProjectUnify(&out, &errs, client, dir, false, true, false); code != 3 || !strings.Contains(errs.String(), "project add") {
		t.Fatalf("an unlisted Project gave %d %q", code, errs.String())
	}
}

func TestProjectUnifyPrintsThePlanAndAppliesOnlyWithApply(t *testing.T) {
	dir := unifyCheckout(t)
	plan := contract.ProjectUnifyPlan{Status: "drifting", Version: "v9",
		Rules: contract.ProjectUnifyRules{Now: contract.ProjectUnifyReads{Claude: []string{"CLAUDE.md"}, Codex: []string{"AGENTS.md"}},
			After:           contract.ProjectUnifyReads{Claude: []string{"CLAUDE.md", "AGENTS.md"}, Codex: []string{"AGENTS.md"}},
			ClaudeOnlyLines: []string{"only for Claude"}},
		Skills: []contract.ProjectUnifySkill{{Name: "zebra", Place: "claude", Now: contract.ProjectUnifySeen{Claude: true},
			After: contract.ProjectUnifySeen{Claude: true, Codex: true}, Action: "skill_move_and_link"}},
		Actions: []contract.ProjectUnifyAction{{Kind: "rules_add_import", Description: "Add the import."},
			{Kind: "skill_move_and_link", Description: "Move zebra."}}}
	after := plan
	after.Status, after.Actions = "unified", nil
	client, calls := unifyDaemon(t, dir, plan, 200, contract.ProjectUnifyApplied{Outcome: "applied", Ran: plan.Actions, Plan: after})
	var out, errs bytes.Buffer
	if code := runProjectUnify(&out, &errs, client, dir, false, false, false); code != 1 {
		t.Fatalf("plan read exit %d", code)
	}
	for _, want := range []string{"Claude will read:  CLAUDE.md, AGENTS.md", "| only for Claude", "zebra", "Claude only", "Claude and Codex",
		"1. Add the import.", "2. Move zebra.", "Nothing has been changed."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan output lacks %q:\n%s", want, out.String())
		}
	}
	if len(*calls) != 2 {
		t.Fatalf("a plan read sent %v", *calls)
	}
	out.Reset()
	if code := runProjectUnify(&out, &errs, client, dir, true, false, false); code != 0 || !strings.Contains(out.String(), "Applied 2 of 2 actions") ||
		!strings.Contains(out.String(), "status: unified") {
		t.Fatalf("apply exit %d output %s errors %s", code, out.String(), errs.String())
	}
	if (*calls)[len(*calls)-1] != "POST /v1/projects/p1/unify" {
		t.Fatalf("calls %v", *calls)
	}
}

func TestProjectUnifyApplyReportsAStopAndARefusal(t *testing.T) {
	dir := unifyCheckout(t)
	plan := contract.ProjectUnifyPlan{Status: "drifting", Version: "v1",
		Actions: []contract.ProjectUnifyAction{{Kind: "rules_add_import", Description: "Add."}, {Kind: "skill_link", Description: "Link."}}}
	stopped := contract.ProjectUnifyApplied{Outcome: "stopped", Ran: plan.Actions[:1], Failed: &plan.Actions[1], Plan: plan,
		Error: "file_permission", Detail: "Unify stopped: no permission."}
	client, _ := unifyDaemon(t, dir, plan, 500, stopped)
	var out, errs bytes.Buffer
	if code := runProjectUnify(&out, &errs, client, dir, true, false, false); code != 1 || !strings.Contains(out.String(), "Applied 1 of 2") ||
		!strings.Contains(out.String(), "Stopped at: Link.") {
		t.Fatalf("stopped exit %d output %s", code, out.String())
	}
	client, _ = unifyDaemon(t, dir, plan, 409, contract.Refusal{Error: "plan_changed", Detail: "Read the plan again."})
	out.Reset()
	if code := runProjectUnify(&out, &errs, client, dir, true, false, false); code != 1 || !strings.Contains(errs.String(), "plan_changed") {
		t.Fatalf("refused exit %d errors %s", code, errs.String())
	}
}
