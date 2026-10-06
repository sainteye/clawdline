package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestWorkflowCatalogHasNineCompleteLanguages(t *testing.T) {
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("workflow", language)
		if total < 118 || translated != total {
			t.Errorf("workflow catalog %s = %d/%d, initial baseline is 118", language, translated, total)
		}
	}
}

func TestWorkflowGermanAndTraditionalChineseHumanCopy(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })

	commandLanguage = "de"
	var german bytes.Buffer
	printTracks(&german, work.Tracks{}, false)
	if !strings.Contains(german.String(), "Regeln") || !strings.Contains(german.String(), "Karten") {
		t.Fatalf("German board output: %q", german.String())
	}
	if strings.Contains(german.String(), "rules     ") || strings.Contains(german.String(), "cards     ") {
		t.Fatalf("English board copy remained: %q", german.String())
	}

	commandLanguage = "zh-Hant"
	var chinese bytes.Buffer
	printCloseAudit(&chinese, sessionCloseAudit{TerminalID: "%47", State: "blocked", Authority: "self"})
	if !strings.Contains(chinese.String(), "關閉身分") || !strings.Contains(chinese.String(), "%47") ||
		!strings.Contains(chinese.String(), "blocked") || !strings.Contains(chinese.String(), "self") {
		t.Fatalf("Traditional Chinese close audit or raw fields changed: %q", chinese.String())
	}
	if strings.Contains(chinese.String(), "closing as") {
		t.Fatalf("English close audit copy remained: %q", chinese.String())
	}
}

func TestWorkflowLocalizedSessionsPreserveJSONAndRawRows(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	commandLanguage = "de"
	const answer = `{"sessions":[{"id":"%47","assistant":"codex","state":"working","work_state":"implementing","taskId":"task-raw","label":"Person's raw label","cwd":"/raw/path","closeability":{}}]}`
	_, b := newStandIn(t, func(*http.Request) (int, string) { return 200, answer })
	var human, errs bytes.Buffer
	if code := sessionsRun(&human, &errs, b, false); code != 0 {
		t.Fatalf("human exit %d: %s", code, errs.String())
	}
	for _, raw := range []string{"ASSISTENT", "%47", "task-raw", "Person's raw label", "/raw/path", "working"} {
		if !strings.Contains(human.String(), raw) {
			t.Errorf("human output lost %q: %q", raw, human.String())
		}
	}
	var machine bytes.Buffer
	if code := sessionsRun(&machine, &errs, b, true); code != 0 {
		t.Fatalf("JSON exit %d: %s", code, errs.String())
	}
	var got, want any
	if err := json.Unmarshal(machine.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(answer), &want); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("localized JSON = %s, want %s", gotJSON, wantJSON)
	}
}
