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
	chars := []rune(text)
	for i := 0; i < len(chars); i++ {
		if chars[i] != '(' {
			continue
		}
		total := 0
		j := i + 1
		for j < len(chars) && chars[j] != ')' {
			if !unicode.IsDigit(chars[j]) {
				j++
				continue
			}
			start := j
			for j < len(chars) && unicode.IsDigit(chars[j]) {
				j++
			}
			if j >= len(chars) {
				break
			}
			value := 0
			for _, d := range chars[start:j] {
				value = value*10 + int(d-'0')
			}
			switch chars[j] {
			case 'h':
				total += value * 3600
			case 'm':
				total += value * 60
			case 's':
				return total + value, true
			default:
				j++
				continue
			}
			j++
		}
	}
	return 0, false
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
