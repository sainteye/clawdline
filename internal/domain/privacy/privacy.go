// Package privacy decides whether a line of this repository's text carries
// something that belongs to the person who wrote it rather than to the
// project: their home directory, the ids of sessions and tasks that ran on
// their machine, the private cloud repository, a credential, an email address,
// or a word they listed as their own.
//
// This repository is published. Every one of those got into it the ordinary
// way — a comment that quoted the path a bug was found at, a test that pasted
// the task id it was debugging, a document that named the pane a child ran in
// — and none of it was wrong to write down at the time. It is wrong to publish,
// and nothing could notice the difference. This is the thing that notices.
//
// The rules lean towards a false alarm. A rule that cannot tell a real value
// from a made-up one flags both, and says in its message how to write the
// made-up one so that it passes: fixtures carry a shape no real value has.
//
// It is pure. The caller reads the files, and decides what to scan.
package privacy

import (
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Finding is one thing on one line that should not be published.
type Finding struct {
	// Line is 1-based. Zero means the path itself, not its content.
	Line int
	// Rule names the class, e.g. "home-path"; Rules lists them all.
	Rule string
	// Match is what was found. A credential is masked: the finding is printed,
	// and a checker that republishes the secret it caught into a build log has
	// only moved the leak.
	Match string
}

// Rules describes every rule, with what passes it, in the order Scan checks.
// It is the text `check-private` prints for -rules, so a person who was just
// told "no" can read how to say it instead.
var Rules = []struct{ Name, Catches, Passes string }{
	{"home-path",
		"a home directory with a real account name: /Users/<name>, /home/<name>, C:\\Users\\<name>, and the dashed form a Claude project directory uses (-Users-<name>-)",
		"the fixture names " + strings.Join(sortedKeys(fixtureHomes), ", ") + ", and a placeholder that starts with <, $, {, %, * or . (e.g. /Users/<you>, /home/$USER, /home/...)"},
	{"private-repo",
		"the private cloud repository by name, in a path or in prose",
		"nothing; say \"the cloud service\" and link the public contract instead"},
	{"uuid",
		"a UUID that could be real: a task, conversation, session or device id copied from a machine",
		"a fixture UUID, where one hex digit is at least half of all 32 (c6000001-0000-4000-8000-000000000001, 22222222-2222-4222-8222-222222222222); keep a readable tag at the front and a counter at the end, pad the rest with one digit"},
	{"task-id",
		"the eight hex digits a task is cited by, after the word task: task NNNNNNNN, task-NNNNNNNN, task/NNNNNNNN",
		"a fixture, where one hex digit is at least five of the eight (task-aaaaaaaa, task/c6000001)"},
	{"pane-id",
		"a tmux pane id (%NNN) or window id (@NNN) of three or more digits, which is the size a real one has on a machine that has run for a day; also its URL-encoded form (%25NNN)",
		"one or two digits: %1, %12, @7"},
	{"email",
		"an email address",
		"a reserved domain: example.com/.org/.net, *.example, *.invalid, *.test, *.localhost"},
	{"credential",
		"a private key block, an API key or token by its known prefix (sk-, ghp_, github_pat_, xox*-, AKIA, AIza, eyJ…), a Bearer token, the password in scheme://user:<password>@host, or a long literal assigned to a key named like secret, token, password, api_key, private_key or vapid",
		"a value that says it is fake (contains test, fake, example, dummy, fixture or placeholder), a template (contains <, >, $, { or }), one made of at most four distinct characters or of one short piece repeated, or one that decodes (base64 or hex) to counting or constant bytes"},
	{"hostname",
		"a machine's own name: a MacBook, iMac or Mac mini host name, or a hyphenated or URL-borne .local name",
		"nothing; leave the host out"},
	{"ip-address",
		"an IPv4 address outside loopback and the documentation ranges (a section number after § is not one)",
		"127.0.0.0/8, 0.0.0.0, 255.255.255.255, and RFC 5737: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24"},
	{"private-word",
		"a word from the person's own list (project names, schedule names, a client, their real name), read from outside this repository",
		"anything not on that list"},
}

// fixtureHomes are the account names a test may put after /Users/ or /home/.
// It is a closed list on purpose: a name that is not here is treated as a
// person, because the checker cannot tell a fixture named after a colleague
// from the colleague's own directory.
var fixtureHomes = map[string]bool{
	"a": true, "alice": true, "b": true, "bob": true, "carol": true,
	"example": true, "me": true, "name": true, "runner": true, "sean": true,
	"Shared": true, "someone": true, "test": true, "user": true, "x": true,
	"you": true,
}

// Allowance is a match that a rule gets wrong and the file cannot change: a
// byte-for-byte copy guarded by another check, or a public constant that has
// the shape of a private value. It names the exact text, so the same file can
// still fail on anything else.
type Allowance struct {
	// Path is the repository-relative path, or "" for any file.
	Path string
	Rule string
	Text string
	Why  string
}

// Allowed is every standing exception. Adding one is a decision somebody reads
// in review; the reason is the part they read. A text that is only allowed in
// one file is spelled here in two pieces, or this file would trip on it.
var Allowed = []Allowance{
	{Rule: "uuid", Text: "258EAFA5-E914-47DA-95CA-C5AB0DC85B11",
		Why: "the WebSocket GUID from RFC 6455 §1.3, the same constant in every implementation"},
	{Rule: "uuid", Text: "018f2f7a-7d65-4aa8-8e01-11a8f4257ed1",
		Why: "the envelope id of the cloud protocol's published test vectors; it is signed, so it cannot change without the vectors changing"},
	{Path: "shell/darwin/Browser.swift", Rule: "uuid", Text: "2faf7ef2-488c-47db-" + "98d9-050ec1781455",
		Why: "the shell browser's WKWebsiteDataStore identifier, generated once for the app; changing it discards every sign-in that store holds"},
	{Path: "web/console/src/legacy/js/net/cloud-crypto.js", Rule: "private-repo", Text: "clawdline-" + "cloud-keys",
		Why: "the browser's IndexedDB database name, not the repository; the file is a byte copy the legacy check compares with its source"},
	{Path: "README.md", Rule: "private-repo", Text: "clawdline-" + "cloud-optional",
		Why: "the anchor GitHub generates for the heading \"Turn on Clawdline Cloud (optional)\" — the product's name in a link, not the repository"},
	{Path: "web/console/src/legacy/js/net/cloud-failure.js", Rule: "private-repo", Text: "clawdline-" + "cloud",
		Why: "a byte copy of the original's own file, which the copy guard compares with its source; changing it here would make that guard red instead"},
	{Path: "internal/adapters/swiftstore/legacy_test.go", Rule: "uuid", Text: "0f0e0d0c-0b0a-4908-" + "8706-050403020100",
		Why: "a counted-down fixture id written by hand in this test, not an id from anyone's machine"},
}

// Scanner holds the person's own words; the rest of the rules are fixed.
type Scanner struct {
	words []string
}

// New returns a Scanner that also flags each of words, case-insensitively.
// Blank words and lines starting with # are ignored, so a words file can be
// passed through line by line.
func New(words []string) *Scanner {
	s := &Scanner{}
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		s.words = append(s.words, strings.ToLower(w))
	}
	return s
}

// Scan reports every finding in data, a file at path, and in path itself.
// path is repository-relative with forward slashes; it is what Allowed is
// matched against.
func (s *Scanner) Scan(path string, data []byte) []Finding {
	var out []Finding
	for _, f := range s.line(path, path) {
		f.Line = 0
		out = append(out, f)
	}
	text := string(data)
	n := 0
	for len(text) > 0 {
		n++
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		for _, f := range s.line(path, line) {
			f.Line = n
			out = append(out, f)
		}
	}
	return out
}

func (s *Scanner) line(path, line string) []Finding {
	var out []Finding
	add := func(rule, match string) {
		if allowed(path, rule, match) {
			return
		}
		out = append(out, Finding{Rule: rule, Match: match})
	}
	homePaths(line, add)
	for _, m := range privateRepo.FindAllString(line, -1) {
		add("private-repo", m)
	}
	for _, m := range uuidPattern.FindAllString(line, -1) {
		if !fixtureUUID(m) {
			add("uuid", m)
		}
	}
	taskIDs(line, add)
	paneIDs(line, add)
	for _, loc := range emailPattern.FindAllStringIndex(line, -1) {
		m := line[loc[0]:loc[1]]
		if !userinfo(line, loc[0]) && !reservedEmail(m) {
			add("email", m)
		}
	}
	credentials(line, add)
	for _, m := range macName.FindAllString(line, -1) {
		add("hostname", m)
	}
	for _, loc := range bonjourName.FindAllStringIndex(line, -1) {
		if next := loc[1]; next < len(line) && (line[next] == '.' || isWord(line[next])) {
			continue // settings.local.json is a file name, not a host
		}
		name := line[loc[0]:loc[1]]
		// A Mac names itself with hyphens, from its owner's name; f.local with
		// none is a struct field. Anything after :// or @ is a host either way.
		inURL := strings.HasSuffix(line[:loc[0]], "//") || strings.HasSuffix(line[:loc[0]], "@")
		if !inURL && !strings.Contains(name, "-") {
			continue
		}
		add("hostname", name)
	}
	ipAddresses(line, add)
	if len(s.words) > 0 {
		lower := strings.ToLower(line)
		for _, w := range s.words {
			if i := wordIndex(lower, w); i >= 0 {
				add("private-word", line[i:i+len(w)])
			}
		}
	}
	return out
}

func allowed(path, rule, match string) bool {
	for _, a := range Allowed {
		if a.Rule == rule && (a.Path == "" || a.Path == path) && strings.EqualFold(a.Text, match) {
			return true
		}
	}
	return false
}

// --- home-path

// homeDir finds the name after a home root. The name stops at anything that
// cannot be part of one, so `/Users/you"` and `/Users/you/code` both read
// "you".
var homeDir = regexp.MustCompile(`(?:/Users/|/home/|[A-Za-z]:\\\\?Users\\\\?|[A-Za-z]:/Users/)([^/\\\s"'` + "`" + `)<>\],;:|]*)`)

// dashedHome is how Claude Code names a project directory: the path with every
// separator turned into a dash, so /Users/you/code/app is -Users-you-code-app.
var dashedHome = regexp.MustCompile(`-(?:Users|home)-([A-Za-z0-9._]+)-`)

func homePaths(line string, add func(rule, match string)) {
	for _, m := range homeDir.FindAllStringSubmatchIndex(line, -1) {
		if strings.HasPrefix(line[m[0]:], "/home/") && m[0] > 0 && isWord(line[m[0]-1]) {
			continue // /api/v1/home/x is a route, not a home directory
		}
		name := line[m[2]:m[3]]
		if placeholderHome(name) {
			continue
		}
		add("home-path", line[m[0]:m[1]])
	}
	for _, m := range dashedHome.FindAllStringSubmatchIndex(line, -1) {
		if name := line[m[2]:m[3]]; !placeholderHome(name) {
			add("home-path", line[m[0]:m[1]])
		}
	}
}

func placeholderHome(name string) bool {
	if name == "" || fixtureHomes[name] || fixtureHomes[strings.ToLower(name)] {
		return true
	}
	switch name[0] {
	case '<', '$', '{', '%', '*', '.':
		return true
	}
	return strings.HasPrefix(name, "…")
}

// --- private-repo

// privateRepo takes the rest of an identifier with it, so an allowance can name
// one identifier that merely starts with the name without allowing the name.
var privateRepo = regexp.MustCompile(`(?i)clawdline[-_]cloud[A-Za-z0-9_-]*`)

// --- uuid

var uuidPattern = regexp.MustCompile(`\b[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\b`)

// fixtureUUID is true when one hex digit makes up at least half of the 32. A
// random UUID has about two of each, and the chance that one digit of its 30
// random ones repeats sixteen times is below one in a billion. A
// hand-typed "random-looking" UUID fails too, which is the point: the checker
// cannot tell it from a pasted one, and neither can a reader.
func fixtureUUID(s string) bool { return mostlyOneDigit(s, 16) }

// mostlyOneDigit is true when one hex digit occurs at least n times in s.
func mostlyOneDigit(s string, n int) bool {
	var count [16]int
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= '0' && r <= '9':
			count[r-'0']++
		case r >= 'a' && r <= 'f':
			count[r-'a'+10]++
		}
	}
	for _, c := range count {
		if c >= n {
			return true
		}
	}
	return false
}

// --- task-id

var taskPrefix = regexp.MustCompile(`(?i)\btask[` + "`" + ` /_-]{1,3}([0-9a-f]{8})\b`)

// taskIDs finds a task cited by the first eight digits of its id, the way a
// branch (clawdline/task/…), a tmux session (clawdline-task-…) and a sentence
// ("task `…`") all spell it. The whole UUID is the uuid rule's, and is not
// reported twice. The fixture test is the UUID one scaled to eight digits: a
// random prefix has one digit five times about once in 1,400.
func taskIDs(line string, add func(rule, match string)) {
	for _, m := range taskPrefix.FindAllStringSubmatchIndex(line, -1) {
		if end := m[2] + 36; end <= len(line) && uuidPattern.MatchString(line[m[2]:end]) {
			continue
		}
		if !mostlyOneDigit(line[m[2]:m[3]], 5) {
			add("task-id", line[m[0]:m[1]])
		}
	}
}

// --- pane-id

var paneOrWindow = regexp.MustCompile(`[%@][0-9]+`)

// paneIDs finds %NNN and @NNN. A match glued to a word or a closing bracket
// before it is arithmetic or a version (`n%100`, `react@1000`), and one glued
// to a word after it is a format verb (`%100d`). `%25` in front of digits is an
// escaped percent sign, the way a pane id travels in a URL, so `%25NNN` is
// pane NNN and `%2519` is pane 19; a real pane whose number starts with 25 is
// read the same way, which is the one miss this rule accepts.
func paneIDs(line string, add func(rule, match string)) {
	for _, m := range paneOrWindow.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 {
			switch c := line[start-1]; {
			case isWord(c), c == ')', c == ']', c == '%':
				continue
			}
		}
		if end < len(line) && isWord(line[end]) {
			continue
		}
		digits := line[start+1 : end]
		if line[start] == '%' && strings.HasPrefix(digits, "25") && len(digits) > 2 {
			digits = digits[2:]
		}
		if len(digits) >= 3 {
			add("pane-id", line[start:end])
		}
	}
}

// --- email

var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@(?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,}`)

// fileExtensions end what looks like a domain but is a file: icon@2x.png.
var fileExtensions = map[string]bool{
	"css": true, "gif": true, "go": true, "html": true, "ico": true, "jpeg": true,
	"jpg": true, "js": true, "json": true, "md": true, "mjs": true, "png": true,
	"svg": true, "swift": true, "ts": true, "tsx": true, "webp": true,
}

// userinfo is true when the "address" at start is the password half of a
// URL's user:password@host. That is not a mailbox; the credential rule reads
// it as the password it is.
func userinfo(line string, start int) bool {
	if start == 0 || line[start-1] != ':' {
		return false
	}
	scheme := strings.LastIndex(line[:start], "://")
	return scheme >= 0 && !strings.ContainsAny(line[scheme+3:start-1], " \t/@")
}

func reservedEmail(address string) bool {
	domain := strings.ToLower(address[strings.LastIndexByte(address, '@')+1:])
	if fileExtensions[domain[strings.LastIndexByte(domain, '.')+1:]] {
		return true
	}
	switch domain {
	case "example.com", "example.org", "example.net", "localhost":
		return true
	}
	for _, suffix := range []string{".example", ".invalid", ".test", ".localhost",
		".example.com", ".example.org", ".example.net"} {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	// git@github.com is the account every SSH remote logs in as, not a person.
	return address == "git@github.com"
}

// --- credential

var (
	privateKeyBlock = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	knownPrefix     = regexp.MustCompile(`\b(?:sk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})`)
	bearer          = regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{20,})`)
	urlPassword     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]*:([^/\s@]+)@`)
	assigned        = regexp.MustCompile(`(?i)(?:secret|token|password|passwd|api[_-]?key|private[_-]?key|vapid[a-z_]*)["']?\s*[:=]+\s*["'` + "`" + `]([A-Za-z0-9+/_=.-]{24,})["'` + "`" + `]`)
)

func credentials(line string, add func(rule, match string)) {
	if m := privateKeyBlock.FindString(line); m != "" {
		add("credential", m)
	}
	for _, m := range knownPrefix.FindAllString(line, -1) {
		if !fakeSecret(m) {
			add("credential", mask(m))
		}
	}
	for _, re := range []*regexp.Regexp{bearer, urlPassword, assigned} {
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			if !fakeSecret(m[1]) {
				add("credential", mask(m[1]))
			}
		}
	}
}

// fakeSecret is a value that announces it is not real, or has too little in
// it to be: "aaaa…", "5ec2e75ec2e7…" and the base64 of 00 01 02 … were typed
// or counted, not generated. Test vectors are made that way on purpose, so
// that anyone can see the key is not one.
func fakeSecret(v string) bool {
	if strings.ContainsAny(v, "<>${}") {
		return true // a template: user:<password>@host, token=${TOKEN}
	}
	lower := strings.ToLower(v)
	for _, w := range []string{"test", "fake", "example", "dummy", "fixture", "placeholder"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	distinct := map[rune]bool{}
	for _, r := range v {
		distinct[r] = true
	}
	if len(distinct) <= 4 || repeats(v) {
		return true
	}
	for _, decode := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString, base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString, base64.RawURLEncoding.DecodeString,
		hex.DecodeString,
	} {
		if b, err := decode(v); err == nil && counted(b) {
			return true
		}
	}
	return false
}

// repeats is true when v is one piece of at most eight characters, repeated.
func repeats(v string) bool {
	for period := 1; period <= 8 && period*2 <= len(v); period++ {
		same := true
		for i := period; i < len(v); i++ {
			if v[i] != v[i-period] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}

// counted is true for at least eight bytes that each step by the same amount
// (0 or 1) from the one before: a constant or a counter.
func counted(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	step := b[1] - b[0]
	if step > 1 {
		return false
	}
	for i := 2; i < len(b); i++ {
		if b[i]-b[i-1] != step {
			return false
		}
	}
	return true
}

// mask keeps the first four characters and the length, which is enough to
// find the line and not enough to use the value.
func mask(v string) string {
	if utf8.RuneCountInString(v) <= 4 {
		return "****"
	}
	return v[:4] + "…(" + strconv.Itoa(len(v)) + " chars)"
}

// --- hostname

var (
	macName     = regexp.MustCompile(`\b[A-Za-z]+-(?:MacBook(?:-Pro|-Air)?|iMac|Mac-mini|Mac-Studio|Mac-Pro)(?:-[0-9]+)?\b`)
	bonjourName = regexp.MustCompile(`\b[A-Za-z0-9][A-Za-z0-9-]*\.local\b`)
)

// --- ip-address

var ipv4 = regexp.MustCompile(`\b[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\b`)

func ipAddresses(line string, add func(rule, match string)) {
	for _, m := range ipv4.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		// 1.2.3.4.5 and v1.2.3.4 are versions, and §3.2.2.3 is a section.
		if start > 0 && (line[start-1] == '.' || isWord(line[start-1])) {
			continue
		}
		before := strings.TrimRight(line[:start], " ")
		if strings.HasSuffix(before, "§") || strings.HasSuffix(strings.ToLower(before), "section") {
			continue
		}
		if end < len(line) && line[end] == '.' && end+1 < len(line) && isDigit(line[end+1]) {
			continue
		}
		ip := line[start:end]
		var octets [4]int
		ok := true
		for i, part := range strings.Split(ip, ".") {
			n, _ := strconv.Atoi(part)
			if n > 255 {
				ok = false
			}
			octets[i] = n
		}
		if !ok || publicSafeIP(octets) {
			continue
		}
		add("ip-address", ip)
	}
}

func publicSafeIP(o [4]int) bool {
	switch {
	case o[0] == 127:
		return true
	case o == [4]int{0, 0, 0, 0}, o == [4]int{255, 255, 255, 255}:
		return true
	case o[0] == 192 && o[1] == 0 && o[2] == 2,
		o[0] == 198 && o[1] == 51 && o[2] == 100,
		o[0] == 203 && o[1] == 0 && o[2] == 113:
		return true
	}
	return false
}

// --- private-word

// wordIndex finds w in lower as a whole word: "acme" is in "acme-app" but not
// in "acmeish". Both are lower-case.
func wordIndex(lower, w string) int {
	from := 0
	for {
		i := strings.Index(lower[from:], w)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(w)
		before := i == 0 || !isAlnum(lower[i-1])
		after := end == len(lower) || !isAlnum(lower[end])
		if before && after {
			return i
		}
		from = i + 1
	}
}

// --- small helpers

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isWord(c byte) bool { return isAlnum(c) || c == '_' }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
