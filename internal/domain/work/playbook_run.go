package work

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The console starts a Playbook by writing its launch message as a Session
// to-do and appending this marker (web/console/src/session/playbook-run.ts).
// The daemon reads the same marker to keep one Playbook running per Session.
const (
	playbookRunMarker = "\n\n[Clawdline-Playbook "
	playbookRunEnd    = "]"
)

var playbookRunIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// PlaybookRunTitle is the title of the Playbook a to-do text starts, and
// false when the text is an ordinary to-do. It accepts exactly what the
// console's playbookRunOf accepts.
func PlaybookRunTitle(text string) (string, bool) {
	at := strings.LastIndex(text, playbookRunMarker)
	if at < 0 || !strings.HasSuffix(text, playbookRunEnd) || at+len(playbookRunMarker) > len(text)-len(playbookRunEnd) {
		return "", false
	}
	data := text[at+len(playbookRunMarker) : len(text)-len(playbookRunEnd)]
	split := strings.IndexByte(data, ' ')
	if split < 1 || !playbookRunIDPattern.MatchString(data[:split]) {
		return "", false
	}
	// decodeURIComponent also refuses escapes that do not spell UTF-8.
	title, err := url.PathUnescape(data[split+1:])
	if err != nil || title == "" || !utf8.ValidString(title) {
		return "", false
	}
	return title, true
}
