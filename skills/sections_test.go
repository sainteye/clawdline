package skills

import (
	"bytes"
	"errors"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Commands and flags are a language-neutral contract in the two guides.
func TestGuideCommandsAndFlagsStayInSync(t *testing.T) {
	command := regexp.MustCompile(`clawdline [a-z][a-z-]*(?: [a-z][a-z-]*)?`)
	flag := regexp.MustCompile(`--[a-z][a-z-]*`)
	collect := func(lang string) (commands, flags []string) {
		guide, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		guide = []byte(strings.ReplaceAll(string(guide), "clawdline guide zh-TW", "clawdline guide"))
		for _, spec := range []struct {
			re  *regexp.Regexp
			out *[]string
		}{{command, &commands}, {flag, &flags}} {
			seen := map[string]bool{}
			for _, match := range spec.re.FindAllString(string(guide), -1) {
				seen[match] = true
			}
			for match := range seen {
				*spec.out = append(*spec.out, match)
			}
			sort.Strings(*spec.out)
		}
		return
	}
	ec, ef := collect("en")
	zc, zf := collect("zh-TW")
	if strings.Join(ec, "\n") != strings.Join(zc, "\n") {
		t.Errorf("command/subcommand mismatch:\nen=%v\nzh-TW=%v", ec, zc)
	}
	if strings.Join(ef, "\n") != strings.Join(zf, "\n") {
		t.Errorf("flag mismatch:\nen=%v\nzh-TW=%v", ef, zf)
	}
}

func TestCoreCarriesCurlAuthenticationAndContentType(t *testing.T) {
	for _, lang := range Topics() {
		core, err := Core(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"orchestrator-token", "X-Clawdline-Orchestrator", "Content-Type: application/json", "--fail-with-body"} {
			if !bytes.Contains(core, []byte(want)) {
				t.Errorf("%s core missing %q", lang, want)
			}
		}
	}
}

func TestRefusalCodesFindTheirOwningPart(t *testing.T) {
	for _, lang := range Topics() {
		for code, want := range map[string]string{"steps_incomplete": "feature-root", "unsupported_media_type": "cloud", "stale_inventory": "dispatch"} {
			name, part, err := RefusedSection(lang, code)
			if err != nil || name != want || !bytes.Contains(part, []byte(code)) {
				t.Errorf("%s %s: part=%s err=%v", lang, code, name, err)
			}
		}
	}
	if _, _, err := RefusedSection("en", "missing_code_123"); !errors.Is(err, ErrUnknownRefusal) {
		t.Errorf("unknown code: %v", err)
	}
}

func TestGuideUsesOneOutwardNameForEachConcept(t *testing.T) {
	for _, lang := range Topics() {
		guide, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		text := string(guide)
		for _, term := range []string{"**step**", "**writes**", "**assignment**", "**part**"} {
			if !strings.Contains(text, term) {
				t.Errorf("%s missing terminology %s", lang, term)
			}
		}
	}
}

// Both guides cut into the same nineteen parts, and the parts put back
// together are the whole guide, byte for byte: printing the guide in parts
// must not lose a sentence that printing it whole carried.
func TestThePartsAreTheWholeGuide(t *testing.T) {
	for _, lang := range Topics() {
		whole, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		preamble, parts, err := guideParts(lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		joined := append([]byte{}, preamble...)
		for i, p := range parts {
			if len(bytes.TrimSpace(p)) == 0 {
				t.Errorf("%s: part %s is empty", lang, sections[i].Name)
			}
			joined = append(joined, p...)
		}
		if !bytes.Equal(joined, whole) {
			t.Errorf("%s: the parts put together are %d bytes and the guide is %d", lang, len(joined), len(whole))
		}
	}
}

// Each named part opens with the heading it is named for, in both languages:
// the table is matched by order, so a heading added to one guide only would
// shift every name after it.
func TestBothGuidesHaveTheSameSections(t *testing.T) {
	want := map[string]string{"swift": "## 0.", "roles": "## 1.", "connect": "## 2.", "feature-root": "## 2a.", "inventory": "## 3.",
		"dispatch": "## 4.", "running": "## 5.", "landing": "## 6.", "report": "## 7.", "send": "## 8.",
		"notify": "## 9.", "board": "## 10.", "coordination": "## 11.", "refused": "## 12.",
		"cloud": "### ", "project": "### ", "schedule": "### "}
	for _, lang := range Topics() {
		for _, name := range SectionNames() {
			text, err := Section(lang, name)
			if err != nil {
				t.Fatalf("%s %s: %v", lang, name, err)
			}
			if !strings.HasPrefix(string(text), want[name]) {
				t.Errorf("%s %s opens with %.30q, want %q", lang, name, text, want[name])
			}
		}
	}
}

// The default print is the core — much shorter than the guide — and it names
// every other part with the command that prints it, in the reader's language.
func TestTheCoreNamesEveryOtherPart(t *testing.T) {
	for _, lang := range Topics() {
		core, err := Core(lang)
		if err != nil {
			t.Fatal(err)
		}
		whole, _ := Guide(lang)
		if len(core)*4 > len(whole) {
			t.Errorf("%s: the core is %d of the guide's %d bytes", lang, len(core), len(whole))
		}
		prefix := "clawdline guide "
		if lang != DefaultTopic {
			prefix += lang + " "
		}
		for _, s := range sections {
			part, _ := Section(lang, s.Name)
			carried := bytes.Contains(core, part)
			named := strings.Contains(string(core), "`"+prefix+s.Name+"`")
			if s.Core != carried || s.Core == named {
				t.Errorf("%s %s: core=%v, carried=%v, named=%v", lang, s.Name, s.Core, carried, named)
			}
		}
		if !strings.Contains(string(core), "`"+prefix+"all`") {
			t.Errorf("%s: the core does not say how to print the whole guide", lang)
		}
	}
	if _, err := Section("en", "nope"); !errors.Is(err, ErrUnknownSection) {
		t.Errorf("unknown part = %v", err)
	}
}

// A Feature Root reads one short part for its ordinary lifecycle instead of
// the board part. Measured on 2026-10-02, one Feature Root spent about a
// quarter of its re-read tool output on guide text, most of it `guide board`'s
// Proposal, Epic and gate-purge procedure it never used. The part stays at
// most a third of the board part, carries every command of the ordinary path,
// points at the rarer parts by name, and is reached from the core.
func TestTheFeatureRootPathIsShortAndComplete(t *testing.T) {
	wants := []string{
		"clawdline item steps <item id>",
		"clawdline item name <item id>",
		"clawdline dispatch --title",
		"--work-id <item id>",
		"file:line",
		"clawdline task show <task id>",
		"clawdline task ack <task id> <notice id>",
		"clawdline item step-done <item id> <step id>",
		"clawdline item phase <item id> implementing",
		"clawdline item phase <item id> verifying",
		"merging --verification",
		"deploying --commit <sha> --target main --remote origin",
		"done --deployment",
		"--no-deployment-reason",
		"clawdline item doc <item id> --role completion_report",
		"clawdline session report --summary",
	}
	for _, lang := range Topics() {
		part, err := Section(lang, "feature-root")
		if err != nil {
			t.Fatal(err)
		}
		board, _ := Section(lang, "board")
		if len(part)*3 > len(board) {
			t.Errorf("%s: the feature-root part is %d bytes, more than a third of board's %d", lang, len(part), len(board))
		}
		prefix := "clawdline guide "
		if lang != DefaultTopic {
			prefix += lang + " "
		}
		for _, want := range append(wants, prefix+"board", prefix+"epic", prefix+"landing", prefix+"dispatch") {
			if !strings.Contains(string(part), want) {
				t.Errorf("%s: the feature-root part does not carry %q", lang, want)
			}
		}
		// After `done` the item is unassigned and a completion report from
		// the Session that closed it answers 409 not_item_owner (seen on five
		// items, 2026-10-02), so the path reaches the report before `done`.
		report := strings.Index(string(part), "--role completion_report")
		done := strings.Index(string(part), "phase <item id> done")
		if report < 0 || done < 0 || report > done {
			t.Errorf("%s: the feature-root part reaches the completion report (at %d) after `done` (at %d)", lang, report, done)
		}
		roles, _ := Section(lang, "roles")
		if !strings.Contains(string(roles), "`"+prefix+"feature-root`") {
			t.Errorf("%s: the roles part does not send a Feature Root to %sfeature-root", lang, prefix)
		}
	}
}
