package whisper

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// What `auto` means, and who it asks.
//
// **Whisper can choose a language and cannot choose a Chinese script.** Given
// `-l auto` it hears Mandarin correctly and writes it in Simplified, because
// that is what most of its Chinese training text was — not because anybody on
// this machine asked for it. So `auto` was never "let whisper decide"; it was
// "let whisper's training data decide", and for a person whose whole system is
// Traditional that is the wrong answer every time.
//
// So `auto` asks, in this order, and stops at the first answer:
//
//  1. Clawdline's own language — the `language` key the settings window's
//     General tab writes, and the one a dispatched child is briefed in. The
//     person said it plainly: dictation follows *our system*, not a constant.
//  2. This machine's own languages, when that key is `auto` too, which is what
//     `auto` means for the interface as well (`Settings.swift` resolved it the
//     same way): macOS's ordered `AppleLanguages`, then `AppleLocale`; the
//     POSIX variables everywhere else, and on Windows its user locale.
//  3. The catalog this daemon ships, when nothing answered. That is the
//     language every page it serves is already written in when nothing narrows
//     it, so it is the one guess that agrees with what the person is reading.
//
// The first answer decides the language whisper is given — but only when it is
// Chinese. Any other language is left to whisper's own detection, because for
// every other language detection already writes the right script, and forcing
// it would turn the sentences somebody says in their second language into a
// translation. The script is then taken from the first Chinese answer on the
// same list, so a machine set to English with Traditional Chinese second
// still gets Traditional when it is spoken to in Chinese. **Simplified is
// written only when something on that list asked for it.**

// Source names who answered, as the settings window says it.
const (
	FromVoiceLanguage = "voice_language"
	FromLanguage      = "language"
	FromAppleLangs    = "AppleLanguages"
	FromAppleLocale   = "AppleLocale"
	FromWindows       = "Windows"
	FromCatalog       = "catalog"
)

// Answer is one language somebody on the list gave, and who gave it.
type Answer struct {
	// Tag is as its source spelled it: `zh-Hant-TW`, `zh_CN.UTF-8`, `en`.
	Tag string
	// Source is one of the From constants, or an environment variable's name.
	Source string
}

// Plan is what one run of whisper is given, and why.
type Plan struct {
	// Code is `-l`: a two-letter code, `zh`, or `auto`.
	Code string
	// Seed is the initial prompt, or empty.
	Seed string
	// Script is how Chinese in the answer is written: "Hant", "Hans", or empty
	// when nothing on the list said — which is whisper's own habit, Simplified.
	Script string
	// From is the answer that decided Code; zero when nothing did.
	From Answer
	// ScriptFrom is the answer that decided Script; zero when nothing did.
	ScriptFrom Answer
}

// Decide is Language for a whole setting: the configured voice language, or,
// when that is `auto`, the first answer in `follow`.
func Decide(language string, follow []Answer) Plan {
	if !isAuto(language) {
		code, seed := Language(language)
		plan := Plan{Code: code, Seed: seed, From: Answer{language, FromVoiceLanguage}}
		if code == "zh" {
			plan.Script, plan.ScriptFrom = ScriptOf(language), plan.From
		}
		return plan
	}
	usable := []Answer{}
	for _, a := range follow {
		if Usable(a.Tag) {
			usable = append(usable, a)
		}
	}
	if len(usable) == 0 {
		return Plan{Code: "auto"}
	}
	first := usable[0]
	if chinese(first.Tag) {
		code, seed := Language(normalTag(first.Tag))
		return Plan{Code: code, Seed: seed, Script: ScriptOf(first.Tag), From: first, ScriptFrom: first}
	}
	plan := Plan{Code: "auto", From: first}
	for _, a := range usable[1:] {
		if chinese(a.Tag) {
			plan.Script, plan.ScriptFrom = ScriptOf(a.Tag), a
			break
		}
	}
	return plan
}

// ScriptOf is "Hant" or "Hans" for a Chinese tag. A bare `zh` is Simplified,
// which is what CLDR's likely subtags make of it and what whisper does anyway.
func ScriptOf(tag string) string {
	if WantsTraditional(tag) {
		return "Hant"
	}
	return "Hans"
}

// Usable is whether a tag says anything. `C` and `POSIX` are the absence of a
// locale rather than one, and `auto` is a question, not an answer.
func Usable(tag string) bool {
	t := normalTag(tag)
	if t == "" || isAuto(t) {
		return false
	}
	lower := strings.ToLower(t)
	if lower == "c" || lower == "posix" {
		return false
	}
	return unicode.IsLetter(rune(t[0]))
}

// normalTag turns a POSIX locale into a BCP 47 tag: `zh_TW.UTF-8@x` → `zh-TW`.
func normalTag(tag string) string {
	t := strings.TrimSpace(tag)
	if i := strings.IndexAny(t, ".@"); i >= 0 {
		t = t[:i]
	}
	return strings.ReplaceAll(t, "_", "-")
}

func isAuto(tag string) bool {
	t := strings.ToLower(strings.TrimSpace(tag))
	return t == "" || t == "auto"
}

func chinese(tag string) bool {
	lower := strings.ToLower(normalTag(tag))
	return lower == "zh" || strings.HasPrefix(lower, "zh-")
}

// LooksChinese is whether whisper's answer, given `-l auto`, came back in
// Chinese: Han characters with no kana in them. Kana is what tells Japanese
// apart — a Japanese sentence with none is rare, and converting one would
// change its kanji — and a sentence with no Han in it has nothing to convert.
func LooksChinese(text string) bool {
	han := false
	for _, r := range text {
		switch {
		case r >= 0x3040 && r <= 0x30FF, r >= 0x31F0 && r <= 0x31FF, r >= 0xFF66 && r <= 0xFF9D:
			return false
		case isHan(r):
			han = true
		}
	}
	return han
}

// Machine reads this machine's languages. Every field has a working zero
// value; the fields exist so that a test can be a machine it is not.
type Machine struct {
	// GOOS is runtime.GOOS unless set.
	GOOS string
	// Getenv is os.Getenv unless set.
	Getenv func(string) string
	// Defaults reads one global macOS default (`defaults read -g <key>`).
	Defaults func(ctx context.Context, key string) (string, error)
	// UserLocale is Windows' user default locale name, or empty.
	UserLocale func() string
}

// Languages is this machine's languages, most preferred first, each with who
// said it. Empty when the machine said nothing a language can be read from.
//
// On macOS the person's choice is `AppleLanguages`, the ordered list in
// Language & Region. `LANG` comes after it because on a Mac it is a terminal's
// encoding setting as often as a language — `en_US.UTF-8` is what a great many
// people set to get UTF-8 — and a daemon started from the desktop has none.
// Everywhere else the POSIX variables are the setting, read in gettext's order.
func (m Machine) Languages(ctx context.Context) []Answer {
	goos := m.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	getenv := m.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	out := []Answer{}
	posix := func() {
		// gettext's order: LANGUAGE is a list and outranks the rest, but only
		// when a locale is set at all.
		if lang := getenv("LANGUAGE"); lang != "" && Usable(firstOf(getenv("LC_ALL"), getenv("LC_MESSAGES"), getenv("LANG"))) {
			for _, part := range strings.Split(lang, ":") {
				if Usable(part) {
					out = append(out, Answer{part, "LANGUAGE"})
				}
			}
		}
		for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := getenv(key); Usable(v) {
				out = append(out, Answer{v, key})
			}
		}
	}
	switch goos {
	case "darwin":
		read := m.Defaults
		if read == nil {
			read = readDefault
		}
		if raw, err := read(ctx, "AppleLanguages"); err == nil {
			for _, tag := range parsePlistArray(raw) {
				if Usable(tag) {
					out = append(out, Answer{tag, FromAppleLangs})
				}
			}
		}
		if raw, err := read(ctx, "AppleLocale"); err == nil && Usable(strings.TrimSpace(raw)) {
			out = append(out, Answer{strings.TrimSpace(raw), FromAppleLocale})
		}
		posix()
	case "windows":
		posix()
		locale := m.UserLocale
		if locale == nil {
			locale = userLocale
		}
		if v := locale(); Usable(v) {
			out = append(out, Answer{v, FromWindows})
		}
	default:
		posix()
	}
	return out
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// readDefault asks `defaults`, which is what reads a preference through the
// same daemon System Settings writes it through; the plist on disk can be
// behind it. Two seconds, because a transcription is waiting on this.
func readDefault(ctx context.Context, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", key).Output()
	return string(out), err
}

// parsePlistArray reads what `defaults read` prints for an array of strings:
//
//	(
//	    "zh-Hant-TW",
//	    en
//	)
func parsePlistArray(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		item := strings.Trim(strings.TrimSpace(line), `,"`)
		if item == "" || item == "(" || item == ")" {
			continue
		}
		out = append(out, item)
	}
	return out
}
