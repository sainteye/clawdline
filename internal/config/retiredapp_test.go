package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Swift Clawdline was stopped on 2026-09-19, taken out of launch-at-login,
// and nothing has answered 7717 since. This file is the guard that keeps the
// repository from quietly going on as if that had not happened.
//
// **It is not a ban on mentioning the old app.** The opposite: the old code and
// the measurements taken against it are the record of what is still to migrate
// and how it was done the first time, and every sentence of that is meant to
// stay. What is banned is one narrow thing — writing in the present tense that
// the retired app is running, holds a port, or is somewhere this daemon sends
// traffic. Those sentences were true when they were written and are false now,
// and the reason to catch them mechanically is that they fail silently: a
// reader follows them to a port nobody holds and gets `502` instead of an
// answer, and a daemon configured from them refuses work it could have done.
//
// Two ways to say the same thing legitimately, both of which this guard lets
// through:
//
//   - Put it in the past, with its date: "the Swift app held 7717 until
//     2026-09-19", "measured on 2026-09-18 against the running app".
//   - Mark the whole file as a dated record of the old app, by putting
//     `retired-app-record` with a date in its first `markerLines` lines. A
//     measurement is only worth keeping if a reader can tell what it measured
//     and when, so the marker is the same discipline the ban is.
//
// A source file that genuinely has to ask, at runtime, whether somebody
// re-opened the old app is in `deliberate` below, by path and with its reason.

// markerLines is how far into a file the record marker is looked for: a header,
// not a footnote.
const markerLines = 60

const recordMarker = "retired-app-record"

// subject is the retired app, however it is spelled here.
var subject = regexp.MustCompile(`(?i)7717|swift app|swift daemon|swift clawdline|clawdline\.app|com\.tsunamiworks\.clawdline|舊 ?app|舊版 ?app|舊的 ?app|舊 daemon`)

// liveness is a claim about right now: it is running, it holds something, or it
// is a destination. Anything here beside a subject is the sentence this guard
// exists for.
// dated is a date on the same line. A claim that carries one has said when it
// was true, which is the first of the two ways out the failure message offers —
// "the Swift app held 7717 until 2026-09-19" is a fact, not a stale assumption,
// and a guard that rejected it would be teaching people to delete the record.
var dated = regexp.MustCompile(`\b20\d\d-\d\d-\d\d\b`)

var liveness = regexp.MustCompile(`(?i)\bowns\b|\bis running\b|\bare running\b|\bstill runs\b|\bstill answers\b|\bforwards\b|\bforwarding\b|\bproxies\b|\bproxying\b|\bis being retired\b|\bwill be retired\b|都在跑|同時在跑|同時跑|還在跑|仍在跑|還在聽|有人在聽|還沒退役|尚未退役|正在退役|即將退役|至今仍`)

// deliberate is the short list of files that say one of those things on
// purpose, each with the reason. A file belongs here only when the claim is
// conditional — it asks the machine, at runtime, whether somebody opened the
// retired app again — or when the file's whole job is to be a copy of that
// app's bytes.
var deliberate = map[string]string{
	"tools/check-legacy-css.sh": "byte-for-byte comparison against the old app's own Resources/web; the point is that it reads that repository",
	"web/console/src/legacy/":   "byte-for-byte copies of the old app's files, registered in MANIFEST.json with where and when each was taken",
	"tools/package-macos.sh":    "bundle identity: it has to name the id the old app registered in order not to claim it",

	// Conditional, not assumed: each of these asks the machine whether
	// somebody opened the retired app again, and is right either way.
	"shell/darwin/HotKey.swift":                       "asks NSRunningApplication whether the old bundle id is running before claiming the hotkey collision",
	"shell/darwin/main.swift":                         "the same check's log line, and the URL scheme the old bundle registered",
	"web/console/src/pages/settings/window/bridge.ts": "the wire shape of that same runtime check; it already says `the retired Swift app`",

	// The `owns` and the old app are in different sentences.
	"internal/adapters/board/board.go": "`This daemon owns its own board file` is about this daemon; the Swift app is the next sentence",
}

var scanned = map[string]bool{
	".go": true, ".md": true, ".ts": true, ".tsx": true, ".json": true,
	".sh": true, ".py": true, ".swift": true, ".yml": true, ".yaml": true,
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "bin": true,
	".serena": true, "testdata": true,
}

// repoRoot is the directory holding go.mod, found from this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

func hasRecordMarker(body string) bool {
	lines := strings.SplitN(body, "\n", markerLines+1)
	if len(lines) > markerLines {
		lines = lines[:markerLines]
	}
	return strings.Contains(strings.Join(lines, "\n"), recordMarker)
}

func isDeliberate(rel string) (string, bool) {
	for prefix, why := range deliberate {
		if rel == prefix || strings.HasPrefix(rel, prefix) {
			return why, true
		}
	}
	return "", false
}

func TestNothingSaysTheRetiredAppIsStillRunning(t *testing.T) {
	root := repoRoot(t)
	var found []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !scanned[filepath.Ext(path)] {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		// This file states every banned spelling in order to ban it.
		if rel == "internal/config/retiredapp_test.go" {
			return nil
		}
		if _, ok := isDeliberate(rel); ok {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(body)
		if hasRecordMarker(text) {
			return nil
		}
		for i, line := range strings.Split(text, "\n") {
			if dated.MatchString(line) {
				continue
			}
			if subject.MatchString(line) && liveness.MatchString(line) {
				found = append(found, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(found) > 0 {
		t.Errorf("%d line(s) say the retired Swift app is running, holds a port, or is somewhere traffic goes.\n"+
			"It was stopped on 2026-09-19 and nothing answers 7717.\n"+
			"Put the claim in the past with its date, or — if the whole file is a dated record of that app —\n"+
			"put %q with a date in its first %d lines. A runtime check for a re-opened app goes in `deliberate`.\n\n%s",
			len(found), recordMarker, markerLines, strings.Join(found, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
