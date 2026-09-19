package session

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MenuFingerprint names the question a menu asks, so an answer can say which
// question it was chosen for.
//
// A digit answers whatever picker is on the screen when it lands. A page that
// read one question and is answered by another — the first answer landed and
// its reply was lost, a second device answered first, a permission prompt for
// the next tool call replaced this one — would otherwise approve something the
// person never saw. So an answer carries this name, and the Mac types nothing
// unless the question on its screen still has it (app.Actions.Key).
//
// **It names the question, not the moment.** The prose above the rows (for a
// permission prompt, the command it asks about), each row's number and words,
// and a set's tab bar are in it. The caret, the ticks, a row's note and the
// button are not: they move while the same question is up — a multi-select's
// tick is the page's own last press — and a second press must still match.
//
// The canonical form length-prefixes every field, so no label can spell its
// way into looking like another row. It is hashed rather than sent whole,
// because a question can be paragraphs long and an answer is one small body.
// `web/console/src/session/fingerprint.ts` computes the same bytes from the
// menu the page was sent; both ends assert the same vector.
func MenuFingerprint(m Menu) string {
	var b strings.Builder
	b.WriteString("menu/1")
	text := func(tag string, s string) {
		s = wireText(s)
		b.WriteString(tag)
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
	}
	text("q", m.Question)
	for _, o := range m.Options {
		text("o"+strconv.Itoa(o.Number)+",", o.Label)
	}
	for _, s := range m.Steps {
		text("s"+boolDigit(s.Answered)+",", s.Label)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ValidFingerprint is the shape MenuFingerprint returns: sixty-four lowercase
// hex digits. Anything else is refused before a screen is read.
func ValidFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// wireText is the string as the page receives it: encoding/json writes every
// byte that is not part of valid UTF-8 as U+FFFD, one for each byte.
func wireText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			out.WriteRune(utf8.RuneError)
		} else {
			out.WriteString(s[i : i+size])
		}
		i += size
	}
	return out.String()
}
