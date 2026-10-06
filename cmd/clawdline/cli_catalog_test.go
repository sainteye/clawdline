package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestCLICatalogCoreBaselineAndSafeFallback(t *testing.T) {
	const english = cliLanguageOptionEnglish
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for _, language := range []string{"zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("core", language)
		if translated != 1 || total != 1 {
			t.Errorf("core coverage %s = %d/%d", language, translated, total)
		}
		commandLanguage = language
		if got := cliCopy("core", "language_option", english); got == english || got == "" {
			t.Errorf("core copy %s = %q", language, got)
		}
	}
	commandLanguage = "en"
	if got := cliCopy("core", "language_option", english); got != english {
		t.Fatalf("English copy = %q", got)
	}
	commandLanguage = "fr"
	if got := cliCopy("core", "missing", english); got != english {
		t.Fatalf("missing key = %q", got)
	}
	if got := cliCopy("core", "language_option", "changed English"); got != "changed English" {
		t.Fatalf("stale English key = %q", got)
	}
}

func TestItemCatalogShipsItsInitialNineLanguageBaseline(t *testing.T) {
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("item", language)
		if total < 176 || translated < 176 {
			t.Errorf("item coverage %s = %d/%d, initial baseline is 176", language, translated, total)
		}
	}
}

func TestTaskCatalogShipsItsInitialNineLanguageBaseline(t *testing.T) {
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("task", language)
		if total < 102 || translated < 102 {
			t.Errorf("task coverage %s = %d/%d, initial baseline is 102", language, translated, total)
		}
	}
}

func TestTaskViewTranslatesFixedTextAndPreservesTaskData(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	task := contract.BrokerTask{ID: "task-raw-id", Title: "Person's own title"}
	var english, chinese bytes.Buffer
	commandLanguage = "en"
	writeTaskView(&english, task)
	commandLanguage = "zh-Hant"
	writeTaskView(&chinese, task)
	if english.String() == chinese.String() || !strings.Contains(chinese.String(), "未寫入") {
		t.Fatalf("task view did not translate fixed text: %q", chinese.String())
	}
	for _, raw := range []string{task.ID, task.Title} {
		if !strings.Contains(chinese.String(), raw) {
			t.Errorf("task data %q changed in localized view: %q", raw, chinese.String())
		}
	}
}

func TestCLICatalogRejectsBadExistingKeysAndAllowsLaterMissingKeys(t *testing.T) {
	english := map[string]string{"one": "One %s", "new": "New %d"}
	if !validCLICatalog(english, map[string]string{"one": "Un %s"}, false) {
		t.Fatal("valid secondary partial catalog was rejected")
	}
	for _, selected := range []map[string]string{
		{"one": "Un %d"},
		{"one": "Un %s", "removed": "Ancien"},
		{"one": "Un %s", "new": ""},
	} {
		if validCLICatalog(english, selected, false) {
			t.Errorf("invalid catalog accepted: %#v", selected)
		}
	}
	if validCLICatalog(english, map[string]string{"one": "一個 %s"}, true) {
		t.Fatal("incomplete Traditional Chinese catalog was accepted")
	}
}

func TestCLICatalogKeepsFormatArgumentsInTheirPluralBranch(t *testing.T) {
	english := map[string]string{"count": "One\x1fMany %d"}
	if !validCLICatalog(english, map[string]string{"count": "Uno\x1fMuchos %d"}, true) {
		t.Fatal("valid plural translation was rejected")
	}
	for _, selected := range []string{
		"Uno %d\x1fMuchos", // The same directive in the wrong branch.
		"Uno\x1fMuchos",    // The directive disappeared.
		"Uno\x1f",          // The second branch is empty.
	} {
		if validCLICatalog(english, map[string]string{"count": selected}, true) {
			t.Errorf("accepted invalid plural translation %q", selected)
		}
	}
}
