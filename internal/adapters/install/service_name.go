package install

import (
	"bytes"
	"encoding/xml"
	"errors"
	"html"
	"regexp"
)

var launchdExecutable = regexp.MustCompile(`(<key>ProgramArguments</key>\s*<array>\s*<string>)([^<]*)(</string>)`)

// RenameLaunchdExecutable changes only the first program argument in a known
// managed plist. Unknown launch commands are refused rather than overwritten.
func RenameLaunchdExecutable(body []byte, allowed []string, target string) ([]byte, bool, error) {
	var document any
	// Validate well-formed XML before using offsets into its original bytes.
	if err := xml.Unmarshal(body, &document); err != nil {
		return nil, false, err
	}
	matches := launchdExecutable.FindAllSubmatchIndex(body, -1)
	if len(matches) != 1 {
		return nil, false, errors.New("the service plist must have one ProgramArguments executable")
	}
	m := matches[0]
	raw := html.UnescapeString(string(body[m[4]:m[5]]))
	if raw == target {
		return body, false, nil
	}
	known := false
	for _, path := range allowed {
		if raw == path {
			known = true
		}
	}
	if !known {
		return nil, false, errors.New("the service plist names an unmanaged executable")
	}
	var out bytes.Buffer
	out.Write(body[:m[4]])
	out.WriteString(plistEscape(target))
	out.Write(body[m[5]:])
	return out.Bytes(), true, nil
}
