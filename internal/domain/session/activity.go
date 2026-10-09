package session

import (
	"strings"
	"unicode"
)

// This file is a port of the Swift app's TerminalSessionPresentation.workingLine
// and its two helpers, kept line for line in behaviour because the line it
// finds is drawn on the replicated screen: a working row with no line is 82
// pixels tall and one with a line is 86, and the text is what a person reads to
// know what the session is doing.

var claudeSpinners = map[rune]bool{
	'✳': true, '✻': true, '✽': true, '✢': true, '✶': true, '✱': true, '✴': true, '·': true, '*': true,
	// Claude Code 2.1.228 introduced the half-circle animation family.
	'◐': true, '◑': true, '◒': true, '◓': true, '◴': true, '◵': true, '◶': true, '◷': true,
}

var codexBullets = map[rune]bool{'•': true, '‣': true, '▪': true, '·': true, '*': true}

// WorkingLine returns the live line a working assistant draws, or "" when
// nothing is running.
//
// Only the tail of the screen is searched, bottom up. Claude Code draws this
// immediately above the input box, and a tall window can still hold older
// spinner lines further up that were scrolled past rather than erased — reading
// one of those would report a session as busy long after it went quiet.
func WorkingLine(screen string, assistant Assistant, tailLines int) string {
	if tailLines <= 0 {
		tailLines = 25
	}
	var lines []string
	for _, l := range strings.Split(Plain(screen), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	start := len(lines) - tailLines
	if start < 0 {
		start = 0
	}
	for i := len(lines) - 1; i >= start; i-- {
		runes := []rune(lines[i])
		if len(runes) == 0 {
			continue
		}
		glyph := runes[0]
		rest := strings.TrimSpace(string(runes[1:]))
		switch assistant {
		case AssistantClaude:
			if !claudeSpinners[glyph] || !strings.Contains(rest, "…") {
				continue
			}
			if _, ok := Elapsed(rest); !ok {
				continue
			}
			return rest
		case AssistantCodex:
			if !codexBullets[glyph] {
				continue
			}
			if _, ok := Elapsed(rest); !ok {
				continue
			}
			// Cut at the closing bracket. What follows is advice for somebody at
			// the keyboard, and this line is drawn on a phone.
			if close := strings.IndexRune(rest, ')'); close >= 0 {
				return rest[:close+1]
			}
			return rest
		}
	}
	return ""
}

// Elapsed reads the provider's own clock out of a line, in seconds.
//
// The unit must immediately follow its number: without that boundary
// "(3 stages)" would look like three seconds. It has to understand the minutes
// and hours forms, because the first version that did not made the line vanish
// exactly when a long wait made it worth having.
func Elapsed(text string) (int, bool) {
	_, _, seconds, ok := ElapsedSpan(text)
	return seconds, ok
}

// ElapsedSpan is Elapsed with where it found the clock: text[start:end] is the
// clock as the provider drew it ("1m 12s"), in bytes.
//
// The span is what makes a working line comparable. The clock moves every
// second the session works and nothing else in the line has to, so a list that
// compared the whole line called every screen refresh a change and published
// it (sessions.go, changeIdentity).
func ElapsedSpan(text string) (start, end, seconds int, ok bool) {
	chars := []rune(text)
	offset := func(i int) int { return len(string(chars[:i])) }
	for i := 0; i < len(chars); i++ {
		if chars[i] != '(' {
			continue
		}
		total, first := 0, -1
		j := i + 1
		for j < len(chars) && chars[j] != ')' {
			if !unicode.IsDigit(chars[j]) {
				j++
				continue
			}
			from := j
			for j < len(chars) && unicode.IsDigit(chars[j]) {
				j++
			}
			if j >= len(chars) {
				break
			}
			value := 0
			for _, d := range chars[from:j] {
				value = value*10 + int(d-'0')
			}
			switch chars[j] {
			case 'h':
				total += value * 3600
			case 'm':
				total += value * 60
			case 's':
				if first < 0 {
					first = from
				}
				return offset(first), offset(j + 1), total + value, true
			default:
				j++
				continue
			}
			if first < 0 {
				first = from
			}
			j++
		}
	}
	return 0, 0, 0, false
}

// WithoutElapsed is a working line with its clock cut out, or the line as it
// is when it has none. Two readings of one turn differ only in the clock, and
// this is what they have in common.
func WithoutElapsed(line string) string {
	start, end, _, ok := ElapsedSpan(line)
	if !ok {
		return line
	}
	return line[:start] + line[end:]
}

// Plain removes terminal controls so a capture can be read as text. CSI and OSC
// are consumed as whole sequences; an unterminated one consumes the remainder.
func Plain(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	needs := strings.ContainsRune(normalized, 0x1b)
	if !needs {
		for _, r := range normalized {
			if r < 0x20 && r != '\n' && r != '\t' {
				needs = true
				break
			}
		}
	}
	if !needs {
		return normalized
	}
	chars := []rune(normalized)
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(chars); {
		c := chars[i]
		if c == 0x1b {
			if i+1 >= len(chars) {
				break
			}
			switch chars[i+1] {
			case '[':
				j := i + 2
				for j < len(chars) && !(chars[j] >= '@' && chars[j] <= '~') {
					j++
				}
				i = j + 1
				if i > len(chars) {
					i = len(chars)
				}
			case ']':
				j := i + 2
				for j < len(chars) {
					if chars[j] == 0x07 {
						j++
						break
					}
					if chars[j] == 0x1b && j+1 < len(chars) && chars[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
			default:
				i += 2
			}
			continue
		}
		if c == '\n' || c == '\t' || (c >= 0x20 && c != 0x7f) {
			out.WriteRune(c)
		}
		i++
	}
	return out.String()
}
