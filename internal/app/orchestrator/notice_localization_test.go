package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/productcopy"
)

func TestDeadLetterPushTextHasNineLanguagesAndPreservesData(t *testing.T) {
	for _, language := range productcopy.Languages {
		for _, reason := range []string{"", holdComposer.Code, holdChoosing.Code, holdLane.Code, holdQueued.Code} {
			title, body := deadLetterText(language, "使用者寫的標題", "failed", 8, reason)
			if title == "" || body == "" || !strings.Contains(body, "使用者寫的標題") ||
				!strings.Contains(body, "failed") || !strings.Contains(body, "result.json") ||
				!strings.Contains(body, "POST /v1/orchestrator/completions/reconcile") {
				t.Errorf("%s %s: %q / %q", language, reason, title, body)
			}
			if reason != "" && !strings.Contains(body, heldReasonLanguage(language, reason)) {
				t.Errorf("%s %s: missing hold explanation: %q", language, reason, body)
			}
		}
	}
}

func TestDeadLetterEffectCarriesChosenLanguageAcrossRetry(t *testing.T) {
	before := deadLetterEffect{Notice: "notice-1", Attempts: 8, Reason: holdComposer.Code, Language: "ja"}
	wire, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var after deadLetterEffect
	if err := json.Unmarshal(wire, &after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("effect changed: %+v", after)
	}
	_, japanese := deadLetterText(after.Language, "Title", "failed", after.Attempts, after.Reason)
	_, english := deadLetterText("en", "Title", "failed", after.Attempts, after.Reason)
	if japanese == english {
		t.Fatal("stored language was ignored")
	}
}
