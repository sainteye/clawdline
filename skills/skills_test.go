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
