package session

import (
	"strconv"
	"strings"
	"unicode"
)

// The Swift app's QuestionSteps: a multi-question picker's own progress bar,
// and which of the asked questions a menu on screen is.
//
// AskUserQuestion can ask several questions in one call and Claude Code shows
// them one at a time, drawing the whole set above the options:
//
//	←  ☒ 算到哪層  ☒ 填格權限  ☐ 表格結構  ☐ 事後對帳  ✔ Submit  →
//
// `☒` answered, `☐` not. Which tab is focused is drawn with styling, not a
// character, so it is not claimed here.

// stepsInLine is the steps a tab bar names, in order. Empty for a line that is
// not one, including a lone question's `☐ build`.
func stepsInLine(line string) []MenuStep {
	var out []MenuStep
	open := false
	answered := false
	var label []rune
	closeStep := func() {
		if !open {
			return
		}
		if l := trimSpaces(string(label)); l != "" {
			out = append(out, MenuStep{Label: l, Answered: answered})
		}
		open = false
	}
	for _, c := range line {
		switch {
		case stepBoxes[c]:
			closeStep()
			open, answered, label = true, c != '☐', nil
		case stepTerminators[c]:
			closeStep()
		case open:
			label = append(label, c)
		}
	}
	closeStep()
	if len(out) < 2 {
		return nil
	}
	return out
}

// reviewAnswers is what the picker's review screen lists, in order: a bullet
// line, then its answer on the next line after `→`.
func reviewAnswers(lines []string) []string {
	var out []string
	awaiting := false
	for _, line := range lines {
		trimmed := []rune(trimSpaces(line))
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] == '●' {
			awaiting = true
			continue
		}
		if !awaiting || trimmed[0] != '→' {
			continue
		}
		if answer := trimSpaces(string(trimmed[1:])); answer != "" {
			out = append(out, answer)
		}
		awaiting = false
	}
	return out
}

// StepsAbove is the tab bar above a menu, searched a little way up from its
// first option. Answers from a review screen are paired by position, and only
// when the counts agree.
func StepsAbove(lines []string, firstOption int) []MenuStep {
	floor := firstOption - 12
	if floor < 0 {
		floor = 0
	}
	for index := firstOption - 1; index >= floor; index-- {
		found := stepsInLine(lines[index])
		if len(found) == 0 {
			continue
		}
		answers := reviewAnswers(lines)
		if len(answers) != len(found) {
			return found
		}
		for i := range found {
			found[i].Answer = answers[i]
		}
		return found
	}
	return nil
}

// AskedQuestion is one question as an AskUserQuestion call asked it, reduced to
// what matching it against a screen needs.
type AskedQuestion struct {
	Text    string
	Options []AskedOption
}

type AskedOption struct {
	Label string
	Note  string
}

// ShowingQuestion is which of `asked` the menu on screen is — only when the
// screen proves it. Every row the screen numbers within a question's options
// must be the start of that option's label (a narrow pane wraps), at least two
// of them; two questions that both fit are told apart by the question read
// above them, and otherwise nothing is. An unknown never authorises a
// replacement: the screen's words are the ones the keystroke acts on.
func ShowingQuestion(menu Menu, asked []AskedQuestion) (int, bool) {
	var fits []int
	for i, q := range asked {
		if rowsFit(menu, q) {
			fits = append(fits, i)
		}
	}
	if len(fits) == 1 {
		return fits[0], true
	}
	read := squeezed(menu.Question)
	if len(fits) <= 1 || read == "" {
		return 0, false
	}
	var named []int
	for _, i := range fits {
		if strings.Contains(squeezed(asked[i].Text), read) {
			named = append(named, i)
		}
	}
	if len(named) == 1 {
		return named[0], true
	}
	return 0, false
}

// RefillMenu gives the menu the words the screen had no room for, from the
// question it is showing, by number. Everything that decides what a press does
// — numbers, caret, Submit, tab bar — stays the screen's.
func RefillMenu(menu Menu, asked []AskedQuestion) Menu {
	index, ok := ShowingQuestion(menu, asked)
	if !ok {
		return menu
	}
	question := asked[index]
	out := menu
	out.Options = append([]MenuOption(nil), menu.Options...)
	for row := range out.Options {
		number := out.Options[row].Number
		if number < 1 || number > len(question.Options) {
			continue
		}
		option := question.Options[number-1]
		out.Options[row].Label = option.Label
		out.Options[row].Detail = option.Note
	}
	if question.Text != "" {
		out.Question = question.Text
	}
	return out
}

func rowsFit(menu Menu, question AskedQuestion) bool {
	compared := 0
	for _, row := range menu.Options {
		if row.Number < 1 || row.Number > len(question.Options) {
			continue
		}
		seen := squeezed(row.Label)
		// An empty prefix is a prefix of everything, which is not agreement.
		if seen == "" || !strings.HasPrefix(squeezed(question.Options[row.Number-1].Label), seen) {
			return false
		}
		compared++
	}
	return compared >= 2
}

// squeezed is text without whitespace and without a clipping ellipsis at the
// end: Chinese wraps between any two characters, so spaces are no evidence.
func squeezed(text string) string {
	out := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
	for _, tail := range []string{"…", "..."} {
		out = strings.TrimSuffix(out, tail)
	}
	return out
}

// MenuRevision is stable content for the row's `line` while it waits: the
// Swift page's transcript revision watches `line`, and answering one question
// of several changes only the bar, so the steps are in it.
func MenuRevision(menu Menu) string {
	parts := []string{menu.Question, ""}
	if menu.Submit != nil {
		parts[1] = menu.Submit.Label + "\x1f" + boolDigit(menu.Submit.Selected)
	}
	for _, o := range menu.Options {
		parts = append(parts, strconv.Itoa(o.Number)+"\x1f"+o.Label+"\x1f"+boolDigit(o.Selected))
	}
	for _, s := range menu.Steps {
		parts = append(parts, s.Label+"\x1f"+boolDigit(s.Answered)+"\x1f"+s.Answer)
	}
	return strings.Join(parts, "\x1e")
}

func boolDigit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
