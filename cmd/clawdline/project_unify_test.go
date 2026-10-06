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
