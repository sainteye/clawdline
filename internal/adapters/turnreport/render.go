package turnreport

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
)

//go:embed template.html
var template string

// Status is what the turn says about itself: a title, an optional line under
// it, and one card per finding.
type Status struct {
	Title    string
	Subtitle string
	Cards    []Card
}

// Card is one status card: a short heading, usually opening with an emoji,
// and a Markdown body.
type Card struct {
	Title string
	Body  string
}

// ParseStatus reads a status file:
//
//	# Title
//	A line under the title (optional)
//
//	## ✅ First card
//	Its body, in Markdown.
//
//	## 🟡 Second card
//	…
//
// The "# " line is optional too; text before the first "## " that is not the
// title is the subtitle.
func ParseStatus(text string) (Status, error) {
	var st Status
	var sub []string
	var cur *Card
	var body []string
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
			st.Cards = append(st.Cards, *cur)
		}
		cur, body = nil, nil
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	fence := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
		}
		switch {
		case !fence && strings.HasPrefix(line, "## "):
			flush()
			cur = &Card{Title: strings.TrimSpace(line[3:])}
		case !fence && cur == nil && st.Title == "" && len(sub) == 0 && strings.HasPrefix(line, "# "):
			st.Title = strings.TrimSpace(line[2:])
		case cur == nil:
			if s := strings.TrimSpace(line); s != "" {
				sub = append(sub, s)
			}
		default:
			body = append(body, line)
		}
	}
	if err := sc.Err(); err != nil {
		return st, err
	}
	flush()
	st.Subtitle = strings.Join(sub, " ")
	if len(st.Cards) == 0 {
		return st, errors.New("the status has no cards: each card is a \"## \" heading and its text")
	}
	return st, nil
}

// ParseNotes reads one note per line, `path: sentence`. Blank lines are
// skipped; a line without ": " is an error that names it.
func ParseNotes(text string) (map[string]string, error) {
	out := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, note, ok := strings.Cut(line, ": ")
		if !ok || strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("notes line %d: want `path: sentence`", i+1)
		}
		out[strings.Trim(strings.TrimSpace(p), "`")] = strings.TrimSpace(note)
	}
	return out, nil
}

type page struct {
	Lang     string     `json:"lang"`
	Title    string     `json:"title"`
	Subtitle string     `json:"subtitle"`
	Date     string     `json:"date"`
	Project  string     `json:"project"`
	Cards    []cardHTML `json:"cards"`
	*Collection
}

type cardHTML struct {
	Title string `json:"title"`
	HTML  string `json:"html"`
}

// Render writes the report. lang is "en" or "zh-TW"; date and project are
// shown under the title.
func Render(c *Collection, st Status, lang, date, project string) ([]byte, error) {
	if lang != "zh-TW" {
		lang = "en"
	}
	p := page{Lang: lang, Title: st.Title, Subtitle: st.Subtitle, Date: date, Project: project, Collection: c}
	if p.Title == "" {
		p.Title = map[string]string{"en": "Status at the end of the turn", "zh-TW": "收尾狀態"}[lang]
	}
	for _, card := range st.Cards {
		h, err := renderMarkdown([]byte(card.Body))
		if err != nil {
			return nil, err
		}
		p.Cards = append(p.Cards, cardHTML{Title: card.Title, HTML: h})
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	// json.Marshal already writes <, > and & as \u escapes, so nothing in the
	// data can close the <script> element that carries it.
	out := template
	script := scriptBody(out)
	sum := sha256.Sum256([]byte(script))
	out = strings.Replace(out, "__SCRIPT_HASH__", base64.StdEncoding.EncodeToString(sum[:]), 1)
	out = strings.Replace(out, "__LANG__", map[string]string{"en": "en", "zh-TW": "zh-Hant-TW"}[lang], 1)
	out = strings.Replace(out, "__TITLE__", html.EscapeString(p.Title), 1)
	var buf bytes.Buffer
	before, after, ok := strings.Cut(out, "/*__DATA__*/null")
	if !ok {
		return nil, errors.New("the report template has no data slot")
	}
	buf.WriteString(before)
	buf.Write(data)
	buf.WriteString(after)
	return buf.Bytes(), nil
}

// scriptBody is the text of the template's one executable script, which the
// page's Content-Security-Policy names by hash: no other script, inline
// handler or remote resource runs or loads, whatever a quoted file contains.
func scriptBody(t string) string {
	const open = "<script>"
	i := strings.Index(t, open)
	j := strings.Index(t[i:], "</script>")
	return t[i+len(open) : i+j]
}
