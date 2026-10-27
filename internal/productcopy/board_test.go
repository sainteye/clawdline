package productcopy

import (
	"regexp"
	"strings"
	"testing"
)

func TestBoardInstructionsKeepCLIExamplesAndAuthoredValues(t *testing.T) {
	command := regexp.MustCompile("`(clawdline [^`]+|--permission-mode full)`")
	values := map[string]string{"id": "item-1", "title": "Original {id} 原文", "phase": "implementing", "kind": "Feature", "planning": "true", "verify": "false", "language": "zh-Hant", "path": "/tmp/task/ASSIGNMENT.md"}
	for key, source := range boardInstructions["en"] {
		translated := boardInstructions["zh-Hant"][key]
		if !sameHTTPRefusalForms(source, translated) {
			t.Fatalf("%s placeholders differ", key)
		}
		if strings.Join(command.FindAllString(source, -1), "\n") != strings.Join(command.FindAllString(translated, -1), "\n") {
			t.Errorf("%s changed CLI examples", key)
		}
		for _, language := range []string{"en", "zh-Hant-TW", "ja", "invalid"} {
			text := BoardInstruction(language, key, values)
			if text == "" {
				t.Errorf("%s/%s is empty", key, language)
			}
			if strings.Contains(source, "{title}") && !strings.Contains(text, values["title"]) {
				t.Errorf("%s/%s rewrote authored title: %s", key, language, text)
			}
			if language == "ja" && text != BoardInstruction("en", key, values) {
				t.Errorf("missing Japanese %s did not fall back", key)
			}
		}
	}
}

func TestBoardInstructionCoverageNamesSecondaryLanguageFallback(t *testing.T) {
	for _, language := range Languages {
		translated, missing := BoardInstructionCoverage(language)
		if translated+missing != len(boardInstructions["en"]) {
			t.Fatalf("%s lost coverage entries", language)
		}
		if (language == "en" || language == "zh-Hant") && missing != 0 {
			t.Errorf("%s has %d missing templates", language, missing)
		}
		if language != "en" && language != "zh-Hant" && translated != 0 {
			t.Errorf("%s claims translations absent from the catalog", language)
		}
		t.Logf("Board instructions %s: %d translated, %d English fallbacks", language, translated, missing)
	}
}

func TestEveryRenderedRootOpeningRetainsItsAssignmentID(t *testing.T) {
	const id = "10000000-0000-4000-8000-00000000a001"
	for _, language := range Languages {
		text := BoardInstruction(language, "root_line", map[string]string{"id": id, "path": "/tmp/ASSIGNMENT.md"})
		if got := RootAssignmentOpeningID(text); got != id {
			t.Errorf("%s Root opening ID = %q", language, got)
		}
	}
	for _, text := range []string{"An ordinary request", "Please quote: 你是 Clawdline Root Assignment " + id, "你是 Clawdline Root Assignment "} {
		if got := RootAssignmentOpeningID(text); got != "" {
			t.Errorf("ordinary/incomplete request became Root %q", got)
		}
	}
}

func TestBoardInstructionsAcceptTheAgentMachineLocale(t *testing.T) {
	for _, language := range []string{"zh_TW.UTF-8", "zh_TW.UTF-8@x", "zh-Hant-TW", " zh_TW "} {
		if got := BoardInstruction(language, "assign", map[string]string{"id": "item-1", "title": "Original"}); !strings.Contains(got, "指派給你") {
			t.Errorf("machine locale %q fell back: %s", language, got)
		}
	}
}
