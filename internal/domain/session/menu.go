package session

import (
	"strconv"
	"strings"
	"unicode"
)

// Menu is a question on a session's screen, as rows a finger can hit. It is
// SessionState.Menu from the Swift app, read the same way: a reading that is
// not certain returns nothing rather than a guess, because a wrong menu hands
// somebody buttons that answer a question nobody asked.
//
// **The numbers are what gets sent, not the positions.** Claude Code's picker
// takes a digit and acts on it, so a row's own number is the thing that answers
// it — and a row whose number a keystroke cannot carry is shown and not
// offered, rather than quietly renumbered into something that answers a
// different question. Except on a dialog that prints no numbers (Numbered is
// false), where the position is all there is and the same flag that took the
// numbers off also stopped the dialog accepting digits.
type Menu struct {
	// Question is the prose above the rows, or the call's own words once the
	// transcript proves which question is up. Empty when it could not be read.
	Question string
	Options  []MenuOption
	// Selected is the number of the row the caret is on, nil when it is on
	// none of them (a multi-select's button can hold it instead).
	Selected *int
	Numbered bool
	// Submit is the button a multi-select draws under its rows: the rows
	// toggle, and only this sends.
	Submit *MenuSubmit
	// Steps is the picker's own tab bar when one call asks several questions.
	// Empty for a lone question.
	Steps []MenuStep
}

type MenuOption struct {
	// Number is the number as printed. It is the keystroke.
	Number int
	Label  string
	// Detail is the prose drawn under the label, joined.
	Detail   string
	Selected bool
	// Checked is nil for a row that does not tick.
	Checked *bool
}

// Answerable is whether a keystroke can carry this row: 1…9.
func (o MenuOption) Answerable() bool { return o.Number >= 1 && o.Number <= 9 }

type MenuSubmit struct {
	Label    string
	Selected bool
}

type MenuStep struct {
	Label    string
	Answered bool
	// Answer is what was chosen, once the picker's review screen names it.
	Answer string
}

// menuCarets are the glyphs a terminal menu marks its current row with.
// Deliberately not `>`: a markdown quote of a numbered list starts with that.
var menuCarets = runeSet("❯›▸▶")

// menuBoxes are what a dialog's wall is drawn with, so `│ ❯ 1. Yes │` is a row.
var menuBoxes = runeSet("│┃|▌▏╎┆┊")

var horizontalRules = runeSet("─━═╌╍┄┅┈┉")
var boxJoints = runeSet("╭╮╰╯┌┐└┘├┤┬┴┼╞╡╪┏┓┗┛")

// scrollArrows sit in the pointer's cell when a list runs past the window.
var scrollArrows = runeSet("↓↑")

// turnMarkers head a turn of the session's own: an answer, a tool call, or the
// spinner on one still being written.
var turnMarkers = runeSet("⏺✳✻✽✢✶✱✴◐◑◒◓◴◵◶◷")

// stepBoxes mark a question in the picker's tab bar: ☐ open, others answered.
var stepBoxes = runeSet("☐☑☒")

// stepTerminators end a tab bar's last label: Submit and the arrow for more.
var stepTerminators = runeSet("✔✓→")

func runeSet(s string) map[rune]bool {
	out := map[rune]bool{}
	for _, r := range s {
		out[r] = true
	}
	return out
}

// isSpace is Swift's CharacterSet.whitespaces: spaces and tabs, not newlines.
func isSpace(r rune) bool { return r == ' ' || r == '\t' || unicode.Is(unicode.Zs, r) }

func trimSpaces(s string) string { return strings.TrimFunc(s, isSpace) }

func trimSpacesAndNewlines(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }

// ReadMenu is the menu on a session's screen, if there is one — its options and
// which of them the caret is on (SessionState.menu, thirty non-empty lines).
//
// `gate` is something outside the screen saying the session is stopped on a
// question: Claude Code's own registry entry. It opens one parsing rule and
// asserts nothing — AskUserQuestion draws its selected row flush left, the
// same column as the composer's caret, and that shape is trusted only then.
func ReadMenu(screen string, assistant Assistant, gate bool) (Menu, bool) {
	return readMenu(screen, assistant, 30, gate)
}

type tailLine struct {
	offset  int
	element string
}

func readMenu(screen string, assistant Assistant, tailLines int, gate bool) (Menu, bool) {
	// The colour comes off here, not at the caller: an escape in front of a
	// caret puts it in a column no row is looked for in.
	lines := strings.Split(Plain(screen), "\n")
	// Physical line numbers are kept beside the non-empty tail. Empty rows are
	// ignored for detection and are boundaries when reading prose.
	visible := make([]tailLine, 0, len(lines))
	for i, l := range lines {
		if trimSpaces(l) != "" {
			visible = append(visible, tailLine{offset: i, element: l})
		}
	}
	tail := visible
	if len(tail) > tailLines {
		tail = tail[len(tail)-tailLines:]
	}
	tailText := make([]string, len(tail))
	for i, t := range tail {
		tailText[i] = t.element
	}
	if assistant == AssistantCodex {
		return codexMenu(tailText)
	}

	// AskUserQuestion's selected row is flush left. That row is trusted only
	// behind the gate, only when no indented caret is on screen, and only as
	// the last flush-left caret that heads a numbered row — the composer sits
	// below a Claude Code dialog, so the very last caret may be somebody's
	// half-typed message.
	flushLeftSelection := -1
	if gate {
		hasIndentedSelection := false
		flushCaret := -1
		for i, line := range tailText {
			row, ok := menuRow(line)
			if !ok || !row.caret {
				continue
			}
			if row.indented {
				hasIndentedSelection = true
			} else {
				flushCaret = i
			}
		}
		if !hasIndentedSelection && flushCaret >= 0 {
			flushLeftSelection = flushCaret
		}
	}

	// Where the dialog starts: the rule or header above the caret. Numbered
	// prose above the frame is the conversation, not options. The caret may
	// be on a multi-select's button rather than on a row.
	selectedCaret := flushLeftSelection
	if selectedCaret < 0 {
		for i := len(tailText) - 1; i >= 0; i-- {
			if row, ok := menuRow(tailText[i]); ok && row.caret && row.indented {
				selectedCaret = i
				break
			}
		}
	}
	if selectedCaret < 0 {
		if sr, ok := submitRow(tail, 0); ok && sr.selected {
			selectedCaret = sr.row
		}
	}
	dialogStart := 0
	if selectedCaret >= 0 {
		for scan := selectedCaret - 1; scan >= 0; scan-- {
			if isBoxRule(tailText[scan]) || isQuestionHeader(dialogText(tailText[scan])) {
				dialogStart = scan + 1
				break
			}
		}
	}

	// An unframed flush-left caret is text, not a dialog: auto mode keeps the
	// gate open while the session prints whatever it prints.
	if flushLeftSelection >= 0 && dialogStart == 0 {
		flushLeftSelection = -1
	}

	// Found before the options, because the button is otherwise read as the
	// last row's description.
	submit, hasSubmit := submitRow(tail, dialogStart)
	submitLine := -1
	if hasSubmit {
		submitLine = submit.line
	}

	var options []MenuOption
	carets := 0
	firstOptionLine := -1
	lastOptionLine := -1
	for index, captured := range tail {
		if index < dialogStart {
			continue
		}
		row, ok := menuRow(captured.element)
		if !ok {
			continue
		}
		// A caret at column zero is the prompt, not a selection, unless the
		// gate above made it one.
		selected := row.caret && (row.indented || index == flushLeftSelection)
		if selected {
			carets++
		}
		if firstOptionLine < 0 {
			firstOptionLine = captured.offset
		}
		lastOptionLine = index
		label, checked := checkbox(withoutSidePanel(row.label))
		detail := detailUnder(captured.offset, lines, submitLine)
		if detail != "" {
			detail = withoutSidePanel(detail)
		}
		options = append(options, MenuOption{
			Number: row.number, Label: label, Detail: detail,
			Selected: selected, Checked: checked,
		})
	}
	if !(carets >= 1 || (hasSubmit && submit.selected)) || len(options) < 2 {
		return plainMenu(tail, lines, gate)
	}

	// Rows with the session's own output under them are scrollback: nothing
	// is printed below a dialog until it is answered.
	if lastOptionLine >= 0 {
		for _, c := range tail[lastOptionLine+1:] {
			if isTurnMarker(c.element) {
				return Menu{}, false
			}
		}
	}

	menu := Menu{Options: options, Numbered: true}
	if firstOptionLine >= 0 {
		lower := 0
		if len(tail) > 0 {
			lower = tail[0].offset
		}
		menu.Question = questionAbove(lines, firstOptionLine, lower)
		menu.Steps = StepsAbove(lines, firstOptionLine)
	}
	menu.Selected = firstSelected(options)
	if hasSubmit {
		menu.Submit = &MenuSubmit{Label: submit.label, Selected: submit.selected}
	}
	return menu, true
}

func firstSelected(options []MenuOption) *int {
	for _, o := range options {
		if o.Selected {
			n := o.Number
			return &n
		}
	}
	return nil
}

// plainMenu is the same dialog with its numbers taken off (`hideIndexes`), the
// weakest evidence in this file and therefore the most gated: the gate, and a
// frame above the rows. Its rows are numbered here by position.
func plainMenu(tail []tailLine, lines []string, gate bool) (Menu, bool) {
	if !gate {
		return Menu{}, false
	}
	text := make([]string, len(tail))
	for i, t := range tail {
		text[i] = t.element
	}

	// The caret nearest the bottom that is not at the left margin.
	anchor := -1
	for i := len(text) - 1; i >= 0; i-- {
		if row, ok := plainRow(text[i]); ok && row.caret && row.indent > 0 {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return Menu{}, false
	}
	head, _ := plainRow(text[anchor])

	dialogStart := 0
	for scan := anchor - 1; scan >= 0; scan-- {
		if isBoxRule(text[scan]) || isQuestionHeader(dialogText(text[scan])) {
			dialogStart = scan + 1
			break
		}
	}
	if dialogStart == 0 {
		return Menu{}, false
	}

	// Outward from the caret, keeping the lines in the caret's own column;
	// deeper lines are descriptions and are stepped over.
	rows := []int{anchor}
	for scanUp := anchor - 1; scanUp >= dialogStart; scanUp-- {
		row, ok := plainRow(text[scanUp])
		if !ok || row.column < head.column || row.caret {
			break
		}
		if row.column == head.column {
			rows = append([]int{scanUp}, rows...)
		}
	}
	for scanDown := anchor + 1; scanDown < len(text); scanDown++ {
		row, ok := plainRow(text[scanDown])
		if !ok || row.column < head.column || row.caret {
			break
		}
		if row.column == head.column {
			rows = append(rows, scanDown)
		}
	}
	if len(rows) < 2 {
		return Menu{}, false
	}

	var options []MenuOption
	for _, index := range rows {
		row, ok := plainRow(text[index])
		if !ok {
			continue
		}
		options = append(options, MenuOption{
			Number:   len(options) + 1,
			Label:    row.label,
			Detail:   plainDetail(tail[index].offset, head.column, lines),
			Selected: index == anchor,
		})
	}
	return Menu{
		Question: questionAbove(lines, tail[rows[0]].offset, tail[0].offset),
		Options:  options,
		Selected: firstSelected(options),
		Numbered: false,
	}, true
}

type submitReading struct {
	row      int
	line     int
	label    string
	selected bool
}

// submitRow is the button a multi-select draws under its rows. A multi-select
// is told apart by a box in front of at least two rows; the button is then the
// line that is not a row, not a checkbox and starts in the labels' column.
func submitRow(tail []tailLine, dialogStart int) (submitReading, bool) {
	column := -1
	checkboxes := 0
	for index, captured := range tail {
		if index < dialogStart {
			continue
		}
		row, ok := menuRow(captured.element)
		if !ok {
			continue
		}
		if column < 0 {
			column = row.column
		}
		if isCheckbox(row.label) {
			checkboxes++
		}
	}
	if checkboxes < 2 || column < 0 {
		return submitReading{}, false
	}
	for index, captured := range tail {
		if index < dialogStart {
			continue
		}
		line := captured.element
		if _, ok := menuRow(line); ok {
			continue
		}
		if isBoxRule(line) {
			continue
		}
		row, ok := plainRow(line)
		if !ok || row.column != column || isCheckbox(row.label) || isQuestionHeader(row.label) {
			continue
		}
		return submitReading{row: index, line: captured.offset, label: row.label, selected: row.caret}, true
	}
	return submitReading{}, false
}

// checkbox is a row's label with its box taken off, and what the box said.
func checkbox(label string) (string, *bool) {
	if !isCheckbox(label) {
		return label, nil
	}
	chars := []rune(label)
	var checked bool
	var rest string
	if stepBoxes[chars[0]] {
		rest = trimSpaces(string(chars[1:]))
		checked = chars[0] != '☐'
	} else {
		rest = trimSpaces(string(chars[3:]))
		checked = chars[1] != ' '
	}
	if rest == "" {
		rest = label
	}
	return rest, &checked
}

// isCheckbox: `[ ]`, `[✔]`, or a ☐ glyph at the front of a label.
func isCheckbox(label string) bool {
	chars := []rune(label)
	if len(chars) > 0 && stepBoxes[chars[0]] {
		return true
	}
	return len(chars) >= 3 && chars[0] == '[' && chars[2] == ']'
}

// plainDetail is the rows drawn under one row of an unnumbered picker, which
// are indented further in than the labels.
func plainDetail(optionIndex, column int, lines []string) string {
	var parts []string
	for index := optionIndex + 1; index < len(lines); index++ {
		row, ok := plainRow(lines[index])
		if !ok || row.column <= column || row.caret || isBoxRule(lines[index]) {
			break
		}
		parts = append(parts, row.label)
	}
	return trimSpacesAndNewlines(strings.Join(parts, " "))
}

type plainReading struct {
	indent int
	column int
	label  string
	caret  bool
}

// plainRow is a line as a row of a picker that prints no numbers: where the
// caret sat, where the label starts, the label, and whether the pointer is on
// it. A scroll arrow in the pointer's cell is not a caret, but the row is a row.
func plainRow(raw string) (plainReading, bool) {
	chars := []rune(raw)
	i := 0
	for i < len(chars) && (chars[i] == ' ' || chars[i] == '\t' || menuBoxes[chars[i]]) {
		i++
	}
	if i >= len(chars) {
		return plainReading{}, false
	}
	indent := i
	caret := false
	if menuCarets[chars[i]] || scrollArrows[chars[i]] {
		caret = menuCarets[chars[i]]
		i++
		if i >= len(chars) || chars[i] != ' ' {
			return plainReading{}, false
		}
		for i < len(chars) && chars[i] == ' ' {
			i++
		}
		if i >= len(chars) {
			return plainReading{}, false
		}
	}
	label := trimTrailingWall(chars[i:])
	if label == "" {
		return plainReading{}, false
	}
	return plainReading{indent: indent, column: i, label: label, caret: caret}, true
}

func trimTrailingWall(chars []rune) string {
	end := len(chars)
	for end > 0 && (chars[end-1] == ' ' || chars[end-1] == '\t' || menuBoxes[chars[end-1]]) {
		end--
	}
	return string(chars[:end])
}

func isTurnMarker(raw string) bool {
	t := []rune(trimSpaces(raw))
	return len(t) > 0 && turnMarkers[t[0]]
}

// codexMenu: Codex puts the selected row's caret in column zero, where its
// composer caret also sits, but takes the composer away while a dialog is up —
// so the last caret on screen decides.
func codexMenu(lines []string) (Menu, bool) {
	caret := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if hasCaret(lines[i]) {
			caret = i
			break
		}
	}
	if caret < 0 {
		return Menu{}, false
	}
	head, ok := menuRow(lines[caret])
	if !ok || !head.caret {
		return Menu{}, false
	}
	options := []MenuOption{{Number: head.number, Label: head.label, Selected: true}}
	for i := caret - 1; i >= 0; i-- {
		row, ok := menuRow(lines[i])
		if !ok {
			break
		}
		options = append([]MenuOption{{Number: row.number, Label: row.label}}, options...)
	}
	for i := caret + 1; i < len(lines); i++ {
		row, ok := menuRow(lines[i])
		if !ok {
			break
		}
		options = append(options, MenuOption{Number: row.number, Label: row.label})
	}
	if len(options) < 2 {
		return Menu{}, false
	}
	return Menu{Options: options, Selected: firstSelected(options), Numbered: true}, true
}

func hasCaret(raw string) bool {
	for _, c := range raw {
		if c == ' ' || c == '\t' || menuBoxes[c] {
			continue
		}
		return menuCarets[c]
	}
	return false
}

// withoutSidePanel takes off whatever was drawn beside a label: two or more
// spaces followed by a box-drawing character is the seam.
func withoutSidePanel(label string) string {
	chars := []rune(label)
	gap := 0
	for index, c := range chars {
		if c == ' ' {
			gap++
			continue
		}
		if gap >= 2 && (menuBoxes[c] || horizontalRules[c] || boxJoints[c]) {
			return trimSpaces(string(chars[:index-gap]))
		}
		gap = 0
	}
	return label
}

// detailUnder is the prose under one option, on the physical lines, up to the
// next option, a frame, a caret, a blank, or the multi-select's button.
func detailUnder(optionIndex int, lines []string, submitLine int) string {
	var parts []string
	for index := optionIndex + 1; index < len(lines); index++ {
		if index == submitLine {
			break
		}
		raw := lines[index]
		if _, ok := menuRow(raw); ok || isBoxRule(raw) || hasCaret(raw) {
			break
		}
		text := dialogText(raw)
		if text == "" {
			break
		}
		parts = append(parts, text)
	}
	return trimSpacesAndNewlines(strings.Join(parts, " "))
}

// questionAbove is the prose immediately above a menu's first row. One blank is
// padding and two are an edge; only prose closed off by a frame, a header or a
// caret is the question.
func questionAbove(lines []string, optionLine, lowerBound int) string {
	if optionLine <= lowerBound {
		return ""
	}
	var parts []string
	blanks := 0
	reach := 12
	closed := false
	for index := optionLine - 1; index >= lowerBound && reach > 0; {
		raw := lines[index]
		text := dialogText(raw)
		if isBoxRule(raw) || isQuestionHeader(text) || hasCaret(raw) {
			closed = true
			break
		}
		if text == "" {
			blanks++
			if blanks > 1 {
				break
			}
			index--
			reach--
			continue
		}
		blanks = 0
		parts = append([]string{text}, parts...)
		index--
		reach--
	}
	if !closed {
		return ""
	}
	return trimSpacesAndNewlines(strings.Join(parts, " "))
}

// dialogText is a line inside a dialog wall, without its padding or far edge.
func dialogText(raw string) string {
	text := []rune(trimSpaces(raw))
	for len(text) > 0 && menuBoxes[text[0]] {
		text = []rune(trimSpaces(string(text[1:])))
	}
	for len(text) > 0 && menuBoxes[text[len(text)-1]] {
		text = []rune(trimSpaces(string(text[:len(text)-1])))
	}
	return string(text)
}

// isQuestionHeader: a checkbox anywhere on the line is chrome above a question.
func isQuestionHeader(text string) bool {
	return strings.ContainsAny(text, "☐☑☒")
}

// isBoxRule is a horizontal rule, corners included; at least one stroke.
func isBoxRule(raw string) bool {
	saw := false
	for _, c := range raw {
		if c == ' ' || c == '\t' || menuBoxes[c] || boxJoints[c] {
			continue
		}
		if horizontalRules[c] {
			saw = true
			continue
		}
		return false
	}
	return saw
}

type rowReading struct {
	number   int
	label    string
	caret    bool
	indented bool
	column   int
}

// menuRow is a line as a numbered option: its number, words, whether a caret
// is on it and whether the line was indented. The indentation is reported, not
// judged: Claude Code's dialogs are indented and Codex's are flush left.
func menuRow(raw string) (rowReading, bool) {
	chars := []rune(raw)
	i := 0
	for i < len(chars) && (chars[i] == ' ' || chars[i] == '\t' || menuBoxes[chars[i]]) {
		i++
	}
	indented := i > 0
	caret := false
	if i < len(chars) && menuCarets[chars[i]] {
		caret = true
		i++
		for i < len(chars) && chars[i] == ' ' {
			i++
		}
	}
	start := i
	for i < len(chars) && unicode.IsNumber(chars[i]) {
		i++
	}
	if i == start || i >= len(chars) || chars[i] != '.' {
		return rowReading{}, false
	}
	number, err := strconv.Atoi(string(chars[start:i]))
	if err != nil {
		return rowReading{}, false
	}
	i++
	if i >= len(chars) || chars[i] != ' ' {
		return rowReading{}, false
	}
	for i < len(chars) && chars[i] == ' ' {
		i++
	}
	if i >= len(chars) {
		return rowReading{}, false
	}
	label := trimTrailingWall(chars[i:])
	if label == "" {
		return rowReading{}, false
	}
	return rowReading{number: number, label: label, caret: caret, indented: indented, column: i}, true
}
