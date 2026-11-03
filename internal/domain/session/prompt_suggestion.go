package session

import (
	"strconv"
	"strings"
	"unicode"
)

// ClaudePromptSuggestion reads the lowest complete Claude composer frame. It
// deliberately requires SGR dim evidence: plain captures and real drafts cannot
// establish a suggestion. In the native UI the cursor can reverse the first
// glyph instead of dimming it. Every other non-space glyph must remain dim.
// Multiline frames are withheld until their native wrapping can be recovered
// without changing the text the person would accept in Claude itself.
func ClaudePromptSuggestion(screen string) string {
	if state, _ := ReadState(screen, AssistantClaude); state == StateWorking {
		return ""
	}
	lines := promptStyledLines(screen)
	for i := len(lines) - 1; i > 0; i-- {
		line := lines[i]
		plain := strings.TrimSpace(promptPlain(line))
		if !strings.HasPrefix(plain, "❯") || !promptRule(promptPlain(lines[i-1])) {
			continue
		}
		end := i + 1
		for end < len(lines) && !promptRule(promptPlain(lines[end])) {
			if strings.TrimSpace(promptPlain(lines[end])) != "" {
				return ""
			}
			end++
		}
		if end == len(lines) {
			return ""
		}
		caret := -1
		for j, c := range line {
			if c.value == '❯' {
				caret = j
				break
			}
		}
		if caret < 0 {
			return ""
		}
		body := line[caret+1:]
		if len(body) == 0 || !unicode.IsSpace(body[0].value) {
			return ""
		}
		body = body[1:]
		for len(body) > 0 && unicode.IsSpace(body[len(body)-1].value) {
			body = body[:len(body)-1]
		}
		var text strings.Builder
		proved := false
		first := true
		for _, c := range body {
			if !c.valid {
				return ""
			}
			if !unicode.IsSpace(c.value) {
				if !c.dim && !(first && c.reverse) {
					return ""
				}
				proved = proved || c.dim
				first = false
			}
			text.WriteRune(c.value)
		}
		result := text.String()
		if !proved || strings.HasPrefix(result, "Try \"") || strings.HasSuffix(result, "…") {
			return ""
		}
		return result
	}
	return ""
}

type promptGlyph struct {
	value               rune
	dim, reverse, valid bool
}

func promptPlain(line []promptGlyph) string {
	var b strings.Builder
	for _, c := range line {
		b.WriteRune(c.value)
	}
	return b.String()
}
func promptRule(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	for _, c := range line {
		if c != '─' && c != '━' {
			return false
		}
	}
	return true
}

// The capture is already a rendered grid, so only SGR is meaningful here.
// Unknown controls taint the rest of their row instead of being interpreted as
// text. Style persists across rows just as it does in capture-pane -e.
func promptStyledLines(screen string) [][]promptGlyph {
	lines := [][]promptGlyph{{}}
	dim, reverse, valid := false, false, true
	chars := []rune(screen)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c == '\n' {
			lines = append(lines, []promptGlyph{})
			valid = true
			continue
		}
		if c == '\x1b' {
			if i+1 >= len(chars) || chars[i+1] != '[' {
				valid = false
				continue
			}
			j := i + 2
			for j < len(chars) && !(chars[j] >= '@' && chars[j] <= '~') {
				j++
			}
			if j == len(chars) {
				break
			}
			if chars[j] != 'm' {
				valid = false
				i = j
				continue
			}
			params := strings.Split(string(chars[i+2:j]), ";")
			for k := 0; k < len(params); k++ {
				p := 0
				if params[k] != "" {
					n, err := strconv.Atoi(params[k])
					if err != nil {
						valid = false
						continue
					}
					p = n
				}
				switch p {
				case 0:
					dim = false
					reverse = false
				case 2:
					dim = true
				case 22:
					dim = false
				case 7:
					reverse = true
				case 27:
					reverse = false
				case 38, 48, 58:
					// Color operands are not SGR attribute codes (RGB's 2 is not dim).
					if k+1 < len(params) {
						switch params[k+1] {
						case "2":
							k += 4
						case "5":
							k += 2
						default:
							valid = false
						}
					}
				}
			}
			i = j
			continue
		}
		if c < ' ' && c != '\t' || c == '\x7f' {
			valid = false
			continue
		}
		last := len(lines) - 1
		lines[last] = append(lines[last], promptGlyph{c, dim, reverse, valid})
	}
	return lines
}
