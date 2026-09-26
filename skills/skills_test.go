package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// routeInGuide is a path as the guides write one: from /v1/ up to the first
// character that cannot be in a path or a placeholder.
var routeInGuide = regexp.MustCompile(`/v1/[A-Za-z0-9_./<>{}%-]*`)

// registered is every pattern a ServeMux in the transport registers, read from
// its source: `mux.HandleFunc("…"` and `mux.Handle("…"`.
var registered = regexp.MustCompile(`mux\.Handle(?:Func)?\("([^"]+)"`)

// transportPatterns reads the patterns the daemon serves. The catch-all "/"
// is left out: it is the fallback that proxies or refuses, not a route.
func transportPatterns(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "internal", "transport", "http", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no transport sources found: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range registered.FindAllSubmatch(data, -1) {
			if p := string(m[1]); p != "/" {
				seen[p] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// served is ServeMux's rule: a pattern ending in "/" is a prefix, any other is
// the exact path. A guide that names a prefix itself ("everything under
// /v1/orchestrator/") names something served when a pattern lies under it.
func served(path string, patterns []string) bool {
	for _, p := range patterns {
		if strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) || path == p {
			return true
		}
		if strings.HasSuffix(path, "/") && strings.HasPrefix(p, path) {
			return true
		}
	}
	return false
}

// guideRoutes is every route a guide names, trimmed of the punctuation a
// sentence puts after it.
func guideRoutes(text []byte) []string {
	seen := map[string]bool{}
	for _, m := range routeInGuide.FindAll(text, -1) {
		p := strings.TrimRight(string(m), ".,")
		seen[p] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Every route a guide names is one this daemon registers. A route renamed or
// removed in internal/transport/http turns this red until the guide follows;
// that is the whole reason the guide is compiled into the binary it
// describes.
func TestTheGuideNamesOnlyRoutesThisDaemonServes(t *testing.T) {
	patterns := transportPatterns(t)
	if len(patterns) < 50 {
		t.Fatalf("only %d patterns read from the transport; the reader is broken", len(patterns))
	}
	for _, topic := range Topics() {
		text, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		routes := guideRoutes(text)
		if len(routes) < 20 {
			t.Fatalf("%s names only %d routes; the reader is broken", topic, len(routes))
		}
		for _, r := range routes {
			if !served(r, patterns) {
				t.Errorf("guide %s names %s, which this daemon does not register", topic, r)
			}
		}
	}
}

// The translation names the same routes as the reference, so neither can
// teach a route the other has dropped.
func TestBothGuidesNameTheSameRoutes(t *testing.T) {
	en, err := Guide("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range Topics() {
		text, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		if a, b := strings.Join(guideRoutes(en), "\n"), strings.Join(guideRoutes(text), "\n"); a != b {
			t.Errorf("guide %s names different routes from en:\n--- en\n%s\n--- %s\n%s", topic, a, topic, b)
		}
	}
}

// The default is English, the list is fixed, and an unknown topic is refused
// by name.
func TestTopics(t *testing.T) {
	if got := strings.Join(Topics(), ","); got != "en,zh-TW" {
		t.Fatalf("topics = %s", got)
	}
	def, err := Guide("")
	if err != nil {
		t.Fatal(err)
	}
	en, _ := Guide("en")
	if !bytes.Equal(def, en) {
		t.Fatal("the default guide is not the English one")
	}
	if _, err := Guide("fr"); err == nil || !strings.Contains(err.Error(), "en, zh-TW") {
		t.Fatalf("unknown topic = %v", err)
	}
}

// The stub names the command that reads the guide, and no route: routes in a
// file installed outside the binary go stale against it.
func TestTheStubCarriesNoRoutes(t *testing.T) {
	stub := Stub()
	if !bytes.HasPrefix(stub, []byte("---\nname: clawdline\n")) {
		t.Fatal("the stub does not start with its front matter")
	}
	if routes := guideRoutes(stub); len(routes) != 0 {
		t.Fatalf("the stub names routes: %v", routes)
	}
	for _, want := range []string{"guide zh-TW", "CLAWDLINE_SKILL_READER", "bin/clawdline"} {
		if !bytes.Contains(stub, []byte(want)) {
			t.Errorf("the stub does not say %q", want)
		}
	}
}

// Pairing authorizes a browser to read this machine (and to drive it when
// commands are enabled). Both guides must therefore carry the complete,
// security-relevant operator path instead of leaving an assistant to infer it
// from CLI help or source code.
func TestEveryGuideExplainsCloudPairing(t *testing.T) {
	wants := []string{
		"clawdline cloud pair",
		"clawdline cloud pair -offer '<code>'",
		"clawdline cloud devices",
		"clawdline cloud revoke <device-id>",
		"browser fingerprint",
		"machine fingerprint",
		"commands",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain pairing with %q", topic, want)
			}
		}
	}
}

// Scheduled work is a daemon capability an assistant cannot safely infer from
// route names alone. Both compiled guides must teach the one-time door, the
// run-backed repeating door, and the proof/refusal vocabulary that keeps a
// machine credential from silently becoming cron authority.
func TestEveryGuideExplainsScheduledWork(t *testing.T) {
	wants := []string{
		"GET /v1/orchestrator/schedules",
		"GET /v1/orchestrator/schedules/<id>",
		"GET /v1/orchestrator/sessions/<conversation>/run",
		"POST /v1/orchestrator/schedules",
		"PATCH /v1/orchestrator/schedules/<id>",
		"DELETE /v1/orchestrator/schedules/<id>",
		"GET /v1/places",
		"Idempotency-Key",
		`"session_id"`,
		`"via"`,
		`"run"`,
		"run_unknown",
		"run_expired",
		"run_other_session",
		"invalid_user_authorization",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain scheduled work with %q", topic, want)
			}
		}
	}
}

func TestEveryGuideExplainsDeferredBoardAssignments(t *testing.T) {
	wants := []string{
		"GET /v1/work/v2/agent/session-todos/<conversation id>",
		"GET /v1/work/v2/items/<id>",
		"PATCH /v1/work/v2/agent/items/<id>/edit",
		"POST /v1/work/v2/agent/items/<id>/reopen",
		"assigned_items",
		"recent_items",
		"direct_todos",
		`"reason"`,
		"waiting_user",
		"user_action",
		"completion_report",
		"root cause",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain deferred Board assignments with %q", topic, want)
			}
		}
	}
}

// A Session adds its own to-dos only when asked, and proposes a Board item
// through the route whose rows the Board's Agent proposals queue shows. Both
// guides say so; the older proposal route alone would put a proposal where
// the Board never reads.
func TestEveryGuideExplainsSessionOwnTodosAndBoardProposals(t *testing.T) {
	wants := []string{
		"clawdline todo add",
		"clawdline todo done <to-do id>",
		"POST /v1/work/v2/agent/session-todos/<conversation id>",
		"POST /v1/work/v2/agent/proposals",
		`"source_todo_id"`,
		"source_todo_id",
		"suggested_acceptance",
		"child_session",
		"direct_todos_full",
		"`description`",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain a Session's own to-dos and Board proposals with %q", topic, want)
			}
		}
	}
}

// A to-do the person sends arrives as ordinary words with one last line that
// names it. Both guides say what that line means, that the row is checked
// off before the turn is reported, and that the report lists any still open
// — otherwise the work gets done and the row stays open (2026-09-25).
func TestEveryGuideExplainsCheckingOffASentToDo(t *testing.T) {
	wants := []string{
		"(Clawdline to-do <id>. When it is done: clawdline todo done <id>)",
		"clawdline todo done <id>",
		"clawdline session report",
		"open_todos",
		"open_todos_unknown",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain checking off a sent to-do with %q", topic, want)
			}
		}
	}
}

// A Session creates a Board item itself only on the person's message through
// Clawdline, with the person's list as the item's steps — never also as its
// own to-dos — and falls back to a proposal when there is no run. Both guides
// say so, and each carries the rule in its own language in one sentence, so
// "an item with TODOs" cannot be read as "an item and some to-dos".
func TestEveryGuideExplainsCreatingABoardItemFromThePersonsMessage(t *testing.T) {
	wants := []string{
		"clawdline item add",
		"--step",
		"clawdline item steps <item id>",
		"clawdline item step-add",
		"clawdline item step-done",
		"POST /v1/work/v2/agent/items",
		`"via": {"run"}`,
		"run_unknown",
		"run_other_session",
		"run_items_exhausted",
		"planning_has_no_steps",
		"no_run",
	}
	rule := map[string]string{
		"en":    "TODO / 待辦 / 土度 said together with a Board item means that item's steps.",
		"zh-TW": "TODO／待辦／土度跟看板項目一起講，指的就是那個項目的 steps。",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain creating a Board item from the person's message with %q", topic, want)
			}
		}
		phrase, ok := rule[topic]
		if !ok {
			t.Errorf("guide %s has no TODO-means-steps rule on record here", topic)
			continue
		}
		if !bytes.Contains(guide, []byte(phrase)) {
			t.Errorf("guide %s does not state %q", topic, phrase)
		}
	}
}

// Every guide tells the owner of an item how it breaks multi-stage work into
// the item's steps by itself, by command and by route, and that a simple
// change takes none. Without it no brief or guide named the route, and
// complex work was never broken down unless the person wrote the list
// (2026-09-26).
func TestEveryGuideExplainsBreakingYourOwnItemIntoSteps(t *testing.T) {
	wants := []string{
		"clawdline item step-add <item id>",
		"POST /v1/work/v2/agent/items/<id>/steps",
		`"position"`,
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain breaking an owned item into steps with %q", topic, want)
			}
		}
	}
}

// Every guide tells the owning Session how it moves its item's phase: the
// command, the route, the one-step graph and the evidence each step takes. A
// guide that only said "advance the phases" left a Session that had finished
// its work with an item stuck in assigned (2026-09-26).
func TestEveryGuideExplainsAdvancingAnItemsPhase(t *testing.T) {
	wants := []string{
		"clawdline item phase <item id> implementing",
		"POST /v1/work/v2/agent/items/<id>/phase",
		"assigned → implementing → verifying → merging → deploying → done",
		"--verification",
		"--commit <sha> --target main --remote origin",
		"--no-deployment-reason",
		"phase_not_editable",
		"invalid_transition",
		"landing_not_published",
		"--landing-project <place id>",
		"landing_project_not_found",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain advancing an item's phase with %q", topic, want)
			}
		}
	}
}

// A Session claims a Board item only when the person's message through
// Clawdline names it, with one command, and both guides name every refusal.
func TestEveryGuideExplainsClaimingABoardItemFromThePersonsMessage(t *testing.T) {
	wants := []string{
		"clawdline item claim <item id>",
		"POST /v1/work/v2/agent/items/<id>/claim",
		`{"expected_version", "session_id", "via": {"run"}}`,
		"run_unknown", "run_expired", "run_other_session", "session_not_found", "child_session",
		"work_not_found", "project_mismatch", "item_assigned", "item_terminal",
		"planning_not_assignable", "version_conflict", "run_claims_exhausted", "no_run",
		"session_cannot_create_item",
	}
	for _, topic := range Topics() {
		guide, err := Guide(topic)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !bytes.Contains(guide, []byte(want)) {
				t.Errorf("guide %s does not explain claiming a Board item with %q", topic, want)
			}
		}
	}
}
