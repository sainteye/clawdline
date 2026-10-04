package skills

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Commands and flags are a language-neutral contract: the two guides name the
// same set of `clawdline <command> <subcommand>` and `--flag`. The language
// argument is the one place they differ by design, so `clawdline guide zh-TW
// x` counts as `clawdline guide x`.
func TestGuideCommandsAndFlagsStayInSync(t *testing.T) {
	ec, ef := commandsAndFlags(t, "en")
	zc, zf := commandsAndFlags(t, "zh-TW")
	if d := setDifference(ec, zc); d != "" {
		t.Errorf("commands differ between en and zh-TW:\n%s", d)
	}
	if d := setDifference(ef, zf); d != "" {
		t.Errorf("flags differ between en and zh-TW:\n%s", d)
	}
}

var (
	guideCommand  = regexp.MustCompile(`clawdline [a-z][a-z-]*(?: [a-z][a-z-]*)?`)
	guideFlag     = regexp.MustCompile(`--[a-z][a-z-]*`)
	guideLanguage = regexp.MustCompile(`clawdline guide (?:` + strings.Join(Topics(), "|") + `)\b ?`)
)

func commandsAndFlags(t *testing.T, lang string) (commands, flags map[string]bool) {
	t.Helper()
	guide, err := Guide(lang)
	if err != nil {
		t.Fatal(err)
	}
	text := guideLanguage.ReplaceAllString(string(guide), "clawdline guide ")
	commands, flags = map[string]bool{}, map[string]bool{}
	for _, m := range guideCommand.FindAllString(text, -1) {
		commands[strings.TrimSuffix(m, " ")] = true
	}
	for _, m := range guideFlag.FindAllString(text, -1) {
		flags[m] = true
	}
	return commands, flags
}

// setDifference says what only en has and what only zh-TW has, or "".
func setDifference(en, zh map[string]bool) string {
	var b strings.Builder
	for _, side := range []struct {
		name      string
		have, not map[string]bool
	}{{"only en", en, zh}, {"only zh-TW", zh, en}} {
		var only []string
		for k := range side.have {
			if !side.not[k] {
				only = append(only, k)
			}
		}
		sort.Strings(only)
		if len(only) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", side.name, strings.Join(only, ", "))
		}
	}
	return b.String()
}

// The core every session prints carries how to call an orchestrator route
// with curl: the credential file, its header, and the content type.
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

// A few codes whose owner is known, the core counting like any other part.
func TestRefusalCodesFindTheirOwningPart(t *testing.T) {
	for _, lang := range Topics() {
		for code, want := range map[string]string{
			"steps_incomplete":       "feature-root",
			"unsupported_media_type": "connect",
			"stale_inventory":        "dispatch",
		} {
			name, part, err := RefusedSection(lang, code)
			if err != nil || name != want || !bytes.Contains(part, []byte(code)) {
				t.Errorf("%s %s: part=%s err=%v, want %s", lang, code, name, err, want)
			}
		}
		for _, bad := range []string{"missing_code_123", "", "Stale_write", "a b"} {
			if _, _, err := RefusedSection(lang, bad); !errors.Is(err, ErrUnknownRefusal) {
				t.Errorf("%s %q: %v", lang, bad, err)
			}
		}
	}
}

// Every code the refusal part names in backticks resolves to a part, and to
// the same part in both languages. `error` and `retry_after` are the answer's
// field names, which that part also backticks; they are not codes.
func TestEveryRefusalCodeHasTheSameOwnerInBothLanguages(t *testing.T) {
	span := regexp.MustCompile("`(?:[0-9]{3} )?([a-z][a-z0-9_]*)`")
	fields := map[string]bool{"error": true, "retry_after": true}
	codes := map[string]bool{}
	for _, lang := range Topics() {
		part, err := Section(lang, "refused")
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range span.FindAllSubmatch(part, -1) {
			if !fields[string(m[1])] {
				codes[string(m[1])] = true
			}
		}
	}
	if len(codes) < 4 {
		t.Fatalf("found only %d codes in the refusal part: %v", len(codes), codes)
	}
	for code := range codes {
		en, _, errEN := RefusedSection("en", code)
		zh, _, errZH := RefusedSection("zh-TW", code)
		if errEN != nil || errZH != nil || en != zh {
			t.Errorf("%s: en=%q (%v) zh-TW=%q (%v)", code, en, errEN, zh, errZH)
		}
	}
}

// The guide calls each concept by one outward name: a checklist entry on an
// item is a step, the paths a task may change are its writes, handing an item
// to a Session is 指派 in zh-TW, and a printable piece of this guide is a part.
// Each retired synonym below fails the test wherever it stands in prose.
//
// Code is not prose: fenced blocks and backticked spans are removed first,
// because the `--claims` flag, the `claims_*` warnings, the "claims" JSON key,
// `-sections` and the root-assignments route are contracts other code reads,
// and they are not renamed here. The person's own words, quoted so a session
// recognizes them, are the only other exception, listed per language.
func TestGuideUsesOneOutwardNameForEachConcept(t *testing.T) {
	banned := map[string][]string{
		"en": {
			`\bTODO\b`, `\bsub-?tasks?\b`, // step
			`\bclaims\b`, `(?i)\bdeclared writes\b`, // writes
			`(?i)\bsections?\b`, `(?i)\btopics?\b`, // part
		},
		"zh-TW": {
			`TODO`, `子任務`, `待辦事項`, // step
			`\bclaims\b`, `宣告的寫入`, // writes
			`(?i)assignment`, `分派`, `分配`, // 指派
			`章節`, `小節`, `(?i)\bsections?\b`, // part
		},
	}
	quoted := map[string][]string{
		"en":    {"TODO / 待辦 / 土度", "TODO: draft"},
		"zh-TW": {"TODO／待辦／土度", "TODO：起草"},
	}
	for _, lang := range Topics() {
		guide, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range proseLines(string(guide)) {
			for _, q := range quoted[lang] {
				line = strings.ReplaceAll(line, q, "")
			}
			for _, word := range banned[lang] {
				if regexp.MustCompile(word).MatchString(line) {
					t.Errorf("%s guide line %d uses retired %s: %s", lang, i+1, word, line)
				}
			}
		}
	}
}

var codeSpan = regexp.MustCompile("`[^`]*`")

// proseLines is each line outside fenced blocks (indented ones included) with
// its backticked spans removed; index i is line i+1, and code lines are "".
func proseLines(text string) []string {
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			lines[i] = ""
			continue
		}
		if fenced {
			lines[i] = ""
		} else {
			lines[i] = codeSpan.ReplaceAllString(line, "")
		}
	}
	return lines
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
		"clawdline task wait <task id>",
		"clawdline item step-done <item id> <step id>",
		"clawdline item phase <item id> implementing",
		"deploying --no-landing-reason",
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
		if lang == DefaultTopic {
			for _, want := range []string{"Work in this Session by default", "Dispatch only when a concrete need", "--commit <sha> --target main --remote origin"} {
				if !strings.Contains(string(part), want) {
					t.Errorf("%s: direct Feature path lacks %q", lang, want)
				}
			}
		} else {
			for _, want := range []string{"預設由本 Session 完成", "只有具體需要", "--commit <sha> --target main --remote origin"} {
				if !strings.Contains(string(part), want) {
					t.Errorf("%s: direct Feature path lacks %q", lang, want)
				}
			}
		}
		// With the verify gate off, implementing goes straight to deploying;
		// the gated line lives in board and `item steps` points there.
		for _, gone := range []string{"verifying", "`merging`", " merging ", "verification gate", "驗證 gate"} {
			if strings.Contains(string(part), gone) {
				t.Errorf("%s: the feature-root part still names %q", lang, gone)
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
