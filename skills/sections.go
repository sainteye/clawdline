package skills

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A guide read in parts.
//
// The whole guide is 15,708 tokens in English and 18,619 in Traditional
// Chinese (measured 2026-09-25 by appending each to a system prompt and
// comparing with and without). A root that read it whole carried it on every
// later call; in a week's transcripts twenty roots read it 136 times, most of
// them a section at a time with sed and grep because that is all they needed.
// The board section alone is 30% of it.
//
// So `clawdline guide` prints the core every session needs — who it is, how to
// reach the daemon, reporting its turn, telling the person, what a refusal
// means — and a contents list, and each other section is printed by name when
// it is needed. `clawdline guide all` is the whole text, unchanged.

// section is one printable part: a name, and the heading that opens it in
// each guide. Headings are matched by their order in the file: all guides
// carry the same twenty, and TestBothGuidesHaveTheSameSections holds them
// to it.
type section struct {
	Name    string
	Summary string
	Core    bool
}

// sections are in the guides' own order: every "## " and "### " heading, one
// entry each.
var sections = []section{
	{"swift", "if you learned Clawdline from the retired Swift app", false},
	{"roles", "root or child", true},
	{"connect", "reaching the daemon, and the commands", true},
	{"cloud", "pairing a Cloud browser", false},
	{"project", "setting up a Project's name, icon, progress and servers", false},
	{"feature-root", "a Feature Root's ordinary path, from reading its item to done", false},
	{"inventory", "before you dispatch: read what is already there", false},
	{"dispatch", "dispatching an owned child", false},
	{"running", "while a child runs, and when it finishes", false},
	{"callback", "waiting on a long command, a deploy or CI, without keeping a turn open", false},
	{"landing", "landing, handoff, detached work and root assignments", false},
	{"schedule", "scheduling future work", false},
	{"report", "reporting your own finished turn", true},
	{"send", "talking to another session", false},
	{"notify", "telling the person", true},
	{"note", "leaving a durable human intervention above a Session", true},
	{"board", "the board: items, steps, proposals, decisions, to-dos", false},
	{"epic", "an Epic you own: its plan, the child review of it, implementing, and its child items", false},
	{"coordination", "coordination between sessions", false},
	{"refused", "what to do when something is refused", true},
}

// ErrUnknownSection is a section name this build does not carry.
var ErrUnknownSection = errors.New("no such section")

// ErrUnknownRefusal is a code no part of the guide mentions.
var ErrUnknownRefusal = errors.New("no guide part explains refusal code")

var refusalCode = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// RefusedSection names the part that owns a refusal code, and returns it.
//
// The owner is the first part, in the guide's own order, that mentions the
// code — core parts count like any other — so the answer is the same in every
// language as long as their parts say the same things in the same places,
// which TestEveryRefusalCodeHasTheSameOwnerInBothLanguages holds them to.
// Two parts are passed over in that walk: "swift" describes the retired app's
// words, and "refused" is the summary every code may also appear in; the
// summary owns a code only when no other part mentions it.
func RefusedSection(lang, code string) (string, []byte, error) {
	if !refusalCode.MatchString(code) {
		return "", nil, fmt.Errorf("%w %q", ErrUnknownRefusal, code)
	}
	_, parts, err := guideParts(lang)
	if err != nil {
		return "", nil, err
	}
	mentions := regexp.MustCompile(`(^|[^a-z0-9_])` + code + `([^a-z0-9_]|$)`)
	summary := -1
	for i, s := range sections {
		switch s.Name {
		case "swift":
			continue
		case "refused":
			summary = i
			continue
		}
		if mentions.Match(parts[i]) {
			return s.Name, parts[i], nil
		}
	}
	if summary >= 0 && mentions.Match(parts[summary]) {
		return sections[summary].Name, parts[summary], nil
	}
	return "", nil, fmt.Errorf("%w %q", ErrUnknownRefusal, code)
}

// SectionNames lists every section, in the guide's order.
func SectionNames() []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.Name
	}
	return out
}

// split cuts a guide at every "## " and "### " heading: the preamble before
// the first, then one part per heading.
func split(text []byte) (preamble []byte, parts [][]byte) {
	lines := bytes.SplitAfter(text, []byte("\n"))
	var cur []byte
	started := false
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("## ")) || bytes.HasPrefix(line, []byte("### ")) {
			if started {
				parts = append(parts, cur)
			} else {
				preamble = cur
				started = true
			}
			cur = nil
		}
		cur = append(cur, line...)
	}
	if started {
		parts = append(parts, cur)
	} else {
		preamble = cur
	}
	return preamble, parts
}

func guideParts(lang string) ([]byte, [][]byte, error) {
	text, err := Guide(lang)
	if err != nil {
		return nil, nil, err
	}
	preamble, parts := split(text)
	if len(parts) != len(sections) {
		return nil, nil, fmt.Errorf("the %s guide has %d sections and this build names %d", lang, len(parts), len(sections))
	}
	return preamble, parts, nil
}

// Section is one named part of the guide in lang ("" is DefaultTopic).
func Section(lang, name string) ([]byte, error) {
	_, parts, err := guideParts(lang)
	if err != nil {
		return nil, err
	}
	for i, s := range sections {
		if s.Name == name {
			return parts[i], nil
		}
	}
	return nil, fmt.Errorf("%w named %q; this build carries %s", ErrUnknownSection, name, strings.Join(SectionNames(), ", "))
}

// Core is what `clawdline guide` prints: the preamble, the sections every
// session needs, and a list of the others with the command that prints each.
func Core(lang string) ([]byte, error) {
	preamble, parts, err := guideParts(lang)
	if err != nil {
		return nil, err
	}
	resolved := ResolveTopic(lang)
	prefix := "clawdline guide "
	if resolved != DefaultTopic {
		prefix += resolved + " "
	}
	var b bytes.Buffer
	b.Write(preamble)
	for i, s := range sections {
		if s.Core {
			b.Write(parts[i])
		}
	}
	copy := coreIndexCopy[resolved]
	b.WriteString("## " + copy.heading + "\n\n")
	b.WriteString(copy.instruction + "\n\n")
	for i, s := range sections {
		if !s.Core {
			fmt.Fprintf(&b, "- `%s%s` — %s\n", prefix, s.Name, sectionTitle(parts[i]))
		}
	}
	if resolved == "zh-Hant" {
		fmt.Fprintf(&b, "\n`%sroutes` 列出此 build 已註冊的路由、起始 API 等級與對應的指南部分；路由不是授權。\n", prefix)
	} else {
		fmt.Fprintf(&b, "\n`%sroutes` lists this build's registered routes, API levels, and related guide parts; a route is not authorization.\n", prefix)
	}
	if resolved == "zh-Hant" {
		fmt.Fprintf(&b, "`%scapacity` 列出此 build 的預設容量上限；執行時以目標機器的即時讀數為準。\n", prefix)
	} else {
		fmt.Fprintf(&b, "`%scapacity` lists this build's default capacity bounds; use the target machine's live reading for an action.\n", prefix)
	}
	if resolved == "zh-Hant" {
		fmt.Fprintf(&b, "`%srefusals` 列出此 build 直接宣告的具型別拒絕碼；未知代碼要停止。\n", prefix)
	} else {
		fmt.Fprintf(&b, "`%srefusals` lists this build's directly declared typed refusal codes; stop on an unknown code.\n", prefix)
	}
	fmt.Fprintf(&b, "\n`%sall` %s\n", prefix, copy.all)
	return b.Bytes(), nil
}

type coreIndexWords struct {
	heading, instruction, all string
}

// The index is generated from each translated guide's headings. Only its
// surrounding instructions need separate copy, so a heading edit cannot
// silently leave a stale summary here.
var coreIndexCopy = map[string]coreIndexWords{
	"en":      {"The rest of this guide, one part at a time", "Print a part when the work in front of you needs it, not before:", "prints the whole guide."},
	"zh-Hant": {"本指南其餘部分，按需閱讀", "需要處理某項工作時，再印出對應的部分：", "會印出完整指南。"},
	"ja":      {"このガイドの残りを必要な部分ごとに読む", "作業に必要になった部分だけを表示してください：", "を実行するとガイド全体を表示します。"},
	"zh-Hans": {"本指南的其余部分，按需阅读", "处理某项工作时，再显示对应的部分：", "会显示完整指南。"},
	"ko":      {"필요할 때 읽는 나머지 안내", "작업에 필요한 부분만 그때 출력하세요:", "명령은 전체 안내를 출력합니다."},
	"es":      {"El resto de esta guía, una parte cada vez", "Muestra una parte cuando la necesites para tu trabajo:", "muestra la guía completa."},
	"pt-BR":   {"O restante deste guia, uma parte por vez", "Mostre uma parte quando precisar dela para o trabalho:", "mostra o guia completo."},
	"fr":      {"La suite de ce guide, une partie à la fois", "Affichez une partie lorsque votre travail l'exige :", "affiche le guide complet."},
	"de":      {"Der Rest dieses Leitfadens, Abschnitt für Abschnitt", "Gib einen Abschnitt erst aus, wenn du ihn für deine Arbeit brauchst:", "gibt den vollständigen Leitfaden aus."},
}

func sectionTitle(part []byte) string {
	line := string(part)
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "### "), "## "))
}
