package projectassistant

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// EstimateRoutineProgressTokens follows CodyNC 9f67eb09 memory/src/tokens.rs:
// one per CJK rune and one per four other runes, rounded up.
func EstimateRoutineProgressTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if r >= 0x1100 && r <= 0x11ff || r >= 0x2e80 && r <= 0x2fdf || r >= 0x3000 && r <= 0x31ff || r >= 0x3400 && r <= 0x4dbf || r >= 0x4e00 && r <= 0x9fff || r >= 0xac00 && r <= 0xd7af || r >= 0xf900 && r <= 0xfaff || r >= 0xff00 && r <= 0xffef || r >= 0x20000 && r <= 0x3ffff {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

// ValidateRoutineProgress keeps the pinned deterministic content checks. A note
// is still untrusted data when it is read; these checks do not establish trust.
// Empty text clears the note. Oversize input is refused, never truncated.
func ValidateRoutineProgress(text string) error {
	bad := func(field string) error { return &Failure{Code: Invalid, Field: "routine_progress_" + field} }
	if !utf8.ValidString(text) {
		return bad("encoding")
	}
	if EstimateRoutineProgressTokens(text) > MaxRoutineProgressTokens {
		return &Failure{Code: CapacityFull, Field: "routine_progress_tokens"}
	}
	for _, r := range text {
		if r == 0xad || r == 0x180e || r >= 0x200b && r <= 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2060 && r <= 0x2064 || r >= 0x2066 && r <= 0x2069 || r == 0xfeff || r >= 0xe0000 && r <= 0xe007f || unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return bad("hidden")
		}
	}
	lower := strings.ToLower(text)
	for _, word := range strings.Fields(lower) {
		if !strings.Contains(word, "http://") && !strings.Contains(word, "https://") {
			continue
		}
		_, query, ok := strings.Cut(word, "?")
		if !ok {
			continue
		}
		for _, pair := range strings.Split(query, "&") {
			key, _, ok := strings.Cut(pair, "=")
			if ok && (key == "q" || key == "prompt" || key == "query" || key == "instructions") {
				return bad("prompt_link")
			}
		}
	}
	for _, phrase := range []string{"ignore previous", "ignore all previous", "ignore the above", "disregard previous", "system prompt", "you are now", "trusted source", "忽略之前", "忽略先前", "忽略以上", "忽略上面", "系統提示", "可信來源", "可信的來源"} {
		if strings.Contains(lower, phrase) {
			return bad("instruction")
		}
	}
	for _, label := range []string{"password", "passwd", "passcode", "密碼", "api key", "api_key", "secret key", "token:"} {
		if strings.Contains(lower, label) {
			return bad("secret")
		}
	}
	for _, word := range strings.Fields(lower) {
		for _, prefix := range []string{"sk-", "ghp_", "gho_", "github_pat_", "xoxb-", "xoxp-", "akia", "-----begin"} {
			if rest, ok := strings.CutPrefix(word, prefix); ok && (len(rest) >= 12 || prefix == "-----begin") {
				return bad("secret")
			}
		}
	}
	chars := []rune(text)
	digit := func(r rune) bool { return r >= '0' && r <= '9' }
	alnum := func(r rune) bool { return digit(r) || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
	for i := range chars {
		if i+10 <= len(chars) {
			w := chars[i : i+10]
			if w[0] >= 'A' && w[0] <= 'Z' && strings.ContainsRune("1289", w[1]) && (i == 0 || !alnum(chars[i-1])) && (i+10 == len(chars) || !alnum(chars[i+10])) {
				all := true
				for _, r := range w[2:] {
					all = all && digit(r)
				}
				if all {
					return bad("secret")
				}
			}
		}
		if i+11 <= len(chars) {
			w := chars[i : i+11]
			all := w[3] == '-' && w[6] == '-'
			for j, r := range w {
				if j != 3 && j != 6 {
					all = all && digit(r)
				}
			}
			if all {
				return bad("secret")
			}
		}
	}
	for _, run := range routineDigitRuns(text) {
		if len(run) >= 13 && len(run) <= 19 {
			sum := 0
			for i := len(run) - 1; i >= 0; i-- {
				d := int(run[i] - '0')
				if (len(run)-1-i)%2 == 1 {
					d *= 2
					if d > 9 {
						d -= 9
					}
				}
				sum += d
			}
			if sum%10 == 0 {
				return bad("secret")
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		for _, label := range []string{"account number", "acct", "帳號", "账号", "卡號", "iban"} {
			if strings.Contains(lower, label) {
				for _, run := range routineDigitRuns(line) {
					if len(run) >= 6 {
						return bad("secret")
					}
				}
			}
		}
	}
	return nil
}

func routineDigitRuns(text string) []string {
	chars := []rune(text)
	runs := []string{}
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			runs = append(runs, run.String())
			run.Reset()
		}
	}
	digit := func(r rune) bool { return r >= '0' && r <= '9' }
	for i, r := range chars {
		if digit(r) {
			run.WriteRune(r)
		} else if !((r == ' ' || r == '-') && i > 0 && i+1 < len(chars) && digit(chars[i-1]) && digit(chars[i+1])) {
			flush()
		}
	}
	flush()
	return runs
}
