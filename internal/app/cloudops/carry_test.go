package cloudops

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/board"
)

// The console's carry table, and the one thing that notices when it and this
// catalog stop agreeing.
//
// `Implemented()` is what this machine tells every browser it can do — it is
// published in the machine descriptor as `machine.commands`
// (internal/transport/cloud/publish.go) and the copied client refuses to send a
// word that is not in it. Adding a word here therefore changes what a browser
// may ask for, and nothing made a browser ask. The hosted console's seam had a
// hand-written list of four GET paths against a catalog of twenty-four routed
// words, so `info` — the one read the status line under every session is drawn
// from — was answered by this machine and never asked for by the page. The page
// refused it to itself, 501 `cloud_not_carried`, and this machine's log held
// no refusal at all because none had been asked for.
//
// So the console's three lists are read here and compared with the catalog:
//
//	CARRIED           the words the console asks for
//	DEFERRED          the words this machine answers and the console does not ask for
//	NO_MACHINE_ROUTE  the words this machine knows and has no route for
//
// CARRIED ∪ DEFERRED must be exactly Implemented(), and NO_MACHINE_ROUTE exactly
// the rest of Vocabulary(). Either half of a new word — a route added here, or
// a route taken away — fails this by name, and the failure says which list the
// word belongs in.
//
// **What this half cannot check is whether a DEFERRED sentence is still true.**
// It asks only whether a word is classified, and `git` stayed classified,
// correctly, for months while its sentence — "the working tree is not read
// over Clawdline Cloud yet" — was false in front of a Git panel that asked for
// the route on every press. The claim is about the console, so the guard for
// it is in the console: `carry.test.ts` reads every `/v1/…` path this bundle
// spells and holds `DEFERRED_ASKED` to the deferrals a screen here is already
// paying for. Deferring stays legal there; being silent about it does not.
//
// It reads the TypeScript rather than a generated file on purpose. A generator
// would make this tree consistent and prove nothing about the bundle actually
// being served, which is an older build of this repository; the runtime half of
// that question is `RelayReader.drift()`, which asks the machine's live
// descriptor on the page. This half is the one a person can fix before shipping.
const carryTable = "web/console/src/cloud/carry.ts"

func TestTheConsoleCarryTableMatchesThisMachinesVocabulary(t *testing.T) {
	root := moduleRoot(t)
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(carryTable)))
	if err != nil {
		t.Fatalf("%s: %v", carryTable, err)
	}
	text := string(source)

	carried := carryList(t, text, "CARRIED")
	deferred := carryList(t, text, "DEFERRED")
	noRoute := carryList(t, text, "NO_MACHINE_ROUTE")

	for _, pair := range [][2]string{{"CARRIED", "DEFERRED"}, {"CARRIED", "NO_MACHINE_ROUTE"}, {"DEFERRED", "NO_MACHINE_ROUTE"}} {
		for word := range listOf(pair[0], carried, deferred, noRoute) {
			if _, twice := listOf(pair[1], carried, deferred, noRoute)[word]; twice {
				t.Errorf("%s is in both %s and %s; one word, one list", word, pair[0], pair[1])
			}
		}
	}

	// What this machine answers must be asked for or deliberately not asked for.
	answers := set(Implemented())
	asked := union(carried, deferred)
	for _, word := range missing(answers, asked) {
		t.Errorf("this machine answers %q and %s lists it in neither CARRIED nor DEFERRED: "+
			"carry it, or say in DEFERRED why not and where it can be done instead", word, carryTable)
	}
	for _, word := range missing(asked, answers) {
		where := "CARRIED"
		if _, ok := deferred[word]; ok {
			where = "DEFERRED"
		}
		if !Knows(word) {
			t.Errorf("%s lists %q in %s and this machine has no such word at all", carryTable, word, where)
			continue
		}
		t.Errorf("%s lists %q in %s and this machine has no route for it: it belongs in NO_MACHINE_ROUTE", carryTable, word, where)
	}

	// And what this machine knows without answering must be the third list exactly,
	// so a word that gains a route stops being "the machine cannot" and has to be
	// decided again.
	unrouted := map[string]bool{}
	for _, word := range Vocabulary() {
		if !answers[word] {
			unrouted[word] = true
		}
	}
	for _, word := range missing(unrouted, noRoute) {
		t.Errorf("this machine knows %q and has no route for it, and %s does not say so in NO_MACHINE_ROUTE", word, carryTable)
	}
	for _, word := range missing(noRoute, unrouted) {
		t.Errorf("%s says this machine has no route for %q, and it has one now: move it to CARRIED or DEFERRED", carryTable, word)
	}

	// A list entry with nothing in it is a word nobody decided about. The
	// sentence is what a person reads out of a refusal, so it has to say
	// something they can act on.
	for _, list := range []struct {
		name  string
		words map[string]string
	}{{"DEFERRED", deferred}, {"NO_MACHINE_ROUTE", noRoute}} {
		for word, sentence := range list.words {
			if len(sentence) < 40 {
				t.Errorf("%s: %s says %q, which says too little to act on", list.name, word, sentence)
			}
		}
	}
}

func listOf(name string, carried, deferred, noRoute map[string]string) map[string]string {
	switch name {
	case "CARRIED":
		return carried
	case "DEFERRED":
		return deferred
	}
	return noRoute
}

// carryList reads one `export const NAME = { … } as const` out of the console's
// table: the keys, and the sentence each one carries.
//
// It is deliberately strict. A table it cannot read is a failure and not an
// empty list, because an empty list would agree with nothing and pass by
// looking like a catalog with no words in it.
func carryList(t *testing.T, source, name string) map[string]string {
	t.Helper()
	open := "export const " + name + " = {"
	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("%s: no `%s`; this test reads that table and cannot be satisfied without it", carryTable, open)
	}
	rest := source[start+len(open):]
	end := strings.Index(rest, "\n} as const")
	if end < 0 {
		t.Fatalf("%s: `%s` is not closed by `} as const`", carryTable, open)
	}
	entry := regexp.MustCompile(`^\s*"?([A-Za-z0-9_.-]+)"?:\s*"(.*)",?\s*$`)
	out := map[string]string{}
	for _, line := range strings.Split(rest[:end], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		found := entry.FindStringSubmatch(line)
		if found == nil {
			t.Fatalf("%s: %s has a line this test cannot read: %q", carryTable, name, trimmed)
		}
		if _, twice := out[found[1]]; twice {
			t.Fatalf("%s: %s names %s twice", carryTable, name, found[1])
		}
		out[found[1]] = found[2]
	}
	if len(out) == 0 {
		t.Fatalf("%s: %s is empty", carryTable, name)
	}
	return out
}

func set(words []string) map[string]bool {
	out := map[string]bool{}
	for _, word := range words {
		out[word] = true
	}
	return out
}

func union(lists ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, list := range lists {
		for word, sentence := range list {
			out[word] = sentence
		}
	}
	return out
}

// missing is every key of `want` that `have` does not hold, sorted, whichever
// of the two map shapes each side is.
func missing[A any, B any](want map[string]A, have map[string]B) []string {
	out := []string{}
	for word := range want {
		if _, ok := have[word]; !ok {
			out = append(out, word)
		}
	}
	sort.Strings(out)
	return out
}

// moduleRoot is the directory holding go.mod, found from this package — the
// same walk `internal/config/retiredapp_test.go` makes for the same reason.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// The board's page size, which is written down twice and must not be.
//
// `board.items` carries two integers and no absence: the Cloud word's body is
// a fixed key set, so "the page named no limit" cannot travel. A browser on
// this machine's own network leaves the field off and the route fills it in
// (`clampInt`, internal/transport/http/board.go); a browser on the relay must
// send the number the route would have chosen, or a phone pages a Project
// differently from the machine it is reading.
//
// So the seam holds the route's own default, and this is what notices when the
// route changes its mind.
const readerSeam = "web/console/src/cloud/relay-reader.ts"

func TestTheBoardsCloudPageSizeIsTheRoutesOwn(t *testing.T) {
	root := moduleRoot(t)
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(readerSeam)))
	if err != nil {
		t.Fatalf("%s: %v", readerSeam, err)
	}
	found := regexp.MustCompile(`(?m)^const BOARD_PAGE_DEFAULT = (\d+)$`).FindSubmatch(source)
	if found == nil {
		t.Fatalf("%s: no `const BOARD_PAGE_DEFAULT = <n>`; this test reads that number "+
			"and cannot be satisfied without it", readerSeam)
	}
	carried, err := strconv.Atoi(string(found[1]))
	if err != nil {
		t.Fatalf("%s: BOARD_PAGE_DEFAULT is %q", readerSeam, found[1])
	}
	if carried != board.DefaultPageLimit {
		t.Errorf("%s carries %d cards a page and %s answers %d: a phone and this machine's own "+
			"browser would page one Project two ways", readerSeam, carried,
			"internal/adapters/board", board.DefaultPageLimit)
	}
	if board.DefaultPageLimit < 1 || board.DefaultPageLimit > board.MaximumPageLimit {
		t.Errorf("the route's own default (%d) is outside what the word may carry (1..%d)",
			board.DefaultPageLimit, board.MaximumPageLimit)
	}
}
