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
)

const handoffSummary = `## Goal
Pages for the export route.

## Verified decisions
- 500 rows a page; internal/export/page_test.go.

## Blockers
None.

## Evidence
- internal/export/page.go

## Next step
Write the pager against internal/export/page.go.
`

func writeSummary(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHandoffCheckNamesEachProblemAndPassesAGoodSummary(t *testing.T) {
	var out, errs bytes.Buffer
	if code := checkMilestoneSummary(&out, &errs, writeSummary(t, handoffSummary)); code != 0 {
		t.Fatalf("a good summary: exit %d, %s", code, out.String())
	}
	out.Reset()
	bad := strings.Replace(handoffSummary, "## Blockers\nNone.\n", "User: and the blockers?\n", 1)
	if code := checkMilestoneSummary(&out, &errs, writeSummary(t, bad)); code != 1 {
		t.Fatalf("a bad summary: exit %d", code)
	}
	for _, want := range []string{"section_missing", "transcript"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("--check output leaves out %s:\n%s", want, out.String())
		}
	}
}

// The command checks before it writes: a summary the daemon would refuse
// sends nothing and writes no package.
func TestHandoffRefusesABadSummaryBeforeAskingTheDaemon(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked++ }))
	defer srv.Close()
	b := &broker{base: srv.URL, token: "machine-secret", client: srv.Client()}
	var out, errs bytes.Buffer
	code := openMilestoneHandoff(&out, &errs, b, handoffOptions{summary: writeSummary(t, "## Goal\nx\n")},
		func(string) string { return "" }, func() (string, error) { return "/p", nil })
	if code != 1 || asked != 0 || !strings.Contains(errs.String(), "--check") {
		t.Fatalf("exit %d, %d requests, %q", code, asked, errs.String())
	}
}

// A good summary is written as the package's handoff.md under the daemon's
// package root, and the handoff is opened as a milestone one from this
// conversation and project.
func TestHandoffWritesThePackageAndOpensAMilestoneHandoff(t *testing.T) {
	packages := t.TempDir()
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"handoffs": []any{}, "package_root": packages, "at": 1})
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&posted)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "handoff": map[string]any{"handoff_id": posted["handoff_id"]}})
		}
	}))
	defer srv.Close()
	b := &broker{base: srv.URL, token: "machine-secret", client: srv.Client()}
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": "c0000000-0000-4000-8000-000000000001"}
	var out, errs bytes.Buffer
	code := openMilestoneHandoff(&out, &errs, b, handoffOptions{summary: writeSummary(t, handoffSummary), title: "Export pages"},
		func(k string) string { return env[k] }, func() (string, error) { return "/p", nil })
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	id, _ := posted["handoff_id"].(string)
	if posted["milestone"] != true || posted["coordinator_plain_handoff"] != true ||
		posted["from_session"] != env["CLAUDE_CODE_SESSION_ID"] || posted["project_dir"] != "/p" ||
		posted["assistant"] != "claude" || posted["title"] != "Export pages" || id == "" {
		t.Fatalf("posted %+v", posted)
	}
	data, err := os.ReadFile(filepath.Join(packages, id, "handoff.md"))
	if err != nil || string(data) != handoffSummary {
		t.Fatalf("package handoff.md = %q, %v", data, err)
	}
}
