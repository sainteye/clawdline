package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

// Which language a Claude Code session this machine opens answers in.
//
// Claude Code answers in English unless its `language` setting says
// otherwise, and nothing about the machine moves it: on a Linux server whose
// locale is `C`, a person who reads Traditional Chinese got English in every
// session Clawdline opened (measured 2026-09-26 against claude 2.1.283, whose
// /config reads "Default (English)" when the key is absent). The setting is
// honoured when passed as `--settings '{"language":…}'` on the command line —
// a fresh `claude -p` with an empty settings file answered an English prompt
// in English without it and in Traditional Chinese with it.
//
// So a Claude session this machine opens is given Clawdline's own language
// (its `language` setting, then the machine's, then the catalog it ships),
// unless the person already chose one in Claude Code's settings file: flag
// settings outrank the user's file, and a choice made there is theirs.

// claudeLanguages is the closed list of names a launch line may carry, by the
// primary subtag of a BCP 47 tag. English is absent because it is what Claude
// Code already does; a language not on the list is left to Claude Code rather
// than spelled into a command line from free text.
var claudeLanguages = map[string]string{
	"ja": "Japanese (日本語)",
	"ko": "Korean (한국어)",
	"fr": "French (français)",
	"de": "German (Deutsch)",
	"es": "Spanish (español)",
	"pt": "Portuguese (português)",
	"it": "Italian (italiano)",
}

const (
	claudeTraditionalChinese = "Traditional Chinese (繁體中文)"
	claudeSimplifiedChinese  = "Simplified Chinese (简体中文)"
)

// ClaudeLanguage is the value Claude Code's `language` setting is given for a
// BCP 47 tag or POSIX locale, or "" when the session should be left as it is.
// A Chinese tag is split by script as dictation splits it (whisper.ScriptOf).
func ClaudeLanguage(tag string) string {
	t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	primary, _, _ := strings.Cut(t, "-")
	if i := strings.IndexAny(primary, ".@"); i >= 0 {
		primary = primary[:i]
	}
	if primary == "zh" {
		if whisper.ScriptOf(t) == "Hant" {
			return claudeTraditionalChinese
		}
		return claudeSimplifiedChinese
	}
	return claudeLanguages[primary]
}

// knownClaudeLanguage is whether name is one ClaudeLanguage answers.
func knownClaudeLanguage(name string) bool {
	if name == claudeTraditionalChinese || name == claudeSimplifiedChinese {
		return true
	}
	for _, v := range claudeLanguages {
		if v == name {
			return true
		}
	}
	return false
}

// ClaudeSettingsPath is the person's own Claude Code settings file, where
// Claude Code looks for it.
func ClaudeSettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// ClaudeSetsLanguage is whether the settings file at path already names a
// language. A missing file names none. A file that cannot be read or parsed
// answers true: what it says is unknown, and a launch that overrode it could
// be overriding the person's choice.
func ClaudeSetsLanguage(path string) bool {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return true
	}
	language, ok := settings["language"].(string)
	return ok && strings.TrimSpace(language) != ""
}

// claudeLanguageArgs is the `--settings` a Claude launch line carries for
// name. The JSON is quoted for the shell the line is typed into.
func claudeLanguageArgs(name string) []string {
	raw, _ := json.Marshal(map[string]string{"language": name})
	return []string{"--settings", ShellQuoted(string(raw))}
}
