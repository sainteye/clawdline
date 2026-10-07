package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/skills"
)

func entryLanguage(t *testing.T, language string) {
	t.Helper()
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	t.Setenv("CLAWDLINE_LANG", "")
	commandLanguage = language
}

func TestEntryCatalogNineLanguagesAndFormatBranches(t *testing.T) {
	bundle := cliCatalogGroups["entry"]
	english := bundle.english
	if len(english) < 120 {
		t.Fatalf("entry catalog has %d keys; expected the entry, guide, skill, and usage copy", len(english))
	}
	if len(bundle.baseline) != 126 {
		t.Fatalf("entry initial baseline has %d keys, expected 126", len(bundle.baseline))
	}
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("entry", language)
		minimum := len(bundle.baseline)
		if language == "en" || language == "zh-Hant" {
			minimum = len(english)
		}
		if translated < minimum || total != len(english) {
			t.Errorf("%s coverage is %d/%d, expected at least %d/%d", language, translated, total, minimum, len(english))
		}
		if language == "en" {
			continue
		}
		catalog := bundle.locales[language]
		for key, value := range catalog {
			source := english[key]
			if strings.TrimSpace(value) == "" || !sameCLIVerbs(source, value) {
				t.Errorf("%s key %q is blank or changes format branches", language, key)
			}
		}
		for _, key := range bundle.baseline {
			source := english[key]
			value, ok := catalog[key]
			if !ok || strings.TrimSpace(value) == "" || !sameCLIVerbs(source, value) {
				t.Errorf("%s key %q is absent, blank, or changes format branches", language, key)
			}
		}
	}
}

func TestEntryHumanOutputKeepsFactsAndUncertainty(t *testing.T) {
	for _, tc := range []struct {
		language string
		words    []string
	}{
		{"de", []string{"noch", "keine"}},
		{"zh-Hant", []string{"尚", "未"}},
	} {
		t.Run(tc.language, func(t *testing.T) {
			entryLanguage(t, tc.language)
			got := usageHead("opaque-session-id", contract.UsageReasonNotYetRead, 0, 0, contract.UsageBill{})
			if !strings.Contains(got, "opaque-session-id") || !strings.Contains(got, "not_yet_read") {
				t.Fatalf("usage heading changed data or reason code: %q", got)
			}
			found := false
			for _, word := range tc.words {
				found = found || strings.Contains(got, word)
			}
			if !found {
				t.Fatalf("usage heading lost the no-reading meaning in %s: %q", tc.language, got)
			}
			copy := cliCopy("entry", "main_task_wait_task_id_timeout_9m", cliCatalogGroups["entry"].english["main_task_wait_task_id_timeout_9m"])
			for _, token := range []string{"task wait", "--timeout", "--any", "0", "1", "3", "4"} {
				if !strings.Contains(copy, token) {
					t.Errorf("translated command help changed %q: %q", token, copy)
				}
			}
		})
	}
	entryLanguage(t, "en")
	if got := usageHead("opaque-session-id", contract.UsageReasonNotYetRead, 0, 0, contract.UsageBill{}); got != "opaque-session-id: not_yet_read — the ledger has no reading of it yet" {
		t.Fatalf("English default changed: %q", got)
	}
}

func TestEntryGuideRoutingAndMachineWords(t *testing.T) {
	entryLanguage(t, "en")
	read := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		code := printGuide(&out, &errs, args)
		return code, out.String(), errs.String()
	}
	englishCode, english, _ := read("en")
	if englishCode != 0 || !strings.HasPrefix(english, "guide-version: ") {
		t.Fatalf("English guide failed: %d %q", englishCode, english)
	}
	if code, got, _ := read("ru"); code != 0 || got != english {
		t.Fatalf("unknown language did not fall back to English: %d %q", code, got)
	}
	for _, language := range []string{"zh-TW", "zh-Hans-CN"} {
		if code, got, _ := read(language); code != 0 || !strings.HasPrefix(got, "guide-version: ") {
			t.Errorf("guide %s failed: %d %q", language, code, got)
		}
	}
	if code, got, _ := read("de", "board"); code != 0 || !strings.Contains(got, "guide-version: ") {
		t.Errorf("German board guide failed: %d %q", code, got)
	}
	if code, got, _ := read("-list"); code != 0 || got != strings.Join(skills.Topics(), "\n")+"\n" {
		t.Errorf("machine-readable topic list changed: %d %q", code, got)
	}
	if code, _, err := read("en", "nosuchpart"); code != 1 || !strings.Contains(err, "no such section") {
		t.Errorf("unknown section was not reported: %d %q", code, err)
	}
	if code, got, _ := read("child"); code != 0 || !strings.Contains(got, "How a Clawdline child works") {
		t.Errorf("child protocol changed: %d %q", code, got)
	}
	if code, _, err := read("child", "--since", strings.Repeat("0", 64)); code != 1 || !strings.Contains(err, "no such section") {
		t.Errorf("child with --since changed its existing section parsing: %d %q", code, err)
	}
	// Language selection must not consume the machine-readable JSON flag or
	// replace the command name before main reaches its unchanged encoder.
	args, language, err := commandLanguagePrefix([]string{"clawdline", "--lang", "de", "version", "--json"})
	if err != nil || language != "de" || strings.Join(args, " ") != "clawdline version --json" {
		t.Fatalf("language prefix changed version JSON invocation: %q, %q, %v", args, language, err)
	}
}
