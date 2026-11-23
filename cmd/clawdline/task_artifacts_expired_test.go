package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestTaskViewSaysArtifactsExpiredBesideTheSummary(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local).Unix()
	task := contract.BrokerTask{ID: "task-id", Title: "Old delivery", State: contract.TaskStateSuccess,
		Result:           &contract.BrokerResult{Status: "success", Summary: "The delivered summary", Artifacts: []string{"artifacts/report.md"}},
		ArtifactsExpired: &contract.BrokerArtifactsExpired{At: at, Reason: "artifact_retention_expired", RetentionSeconds: 30 * 86400}}
	for language, want := range map[string]string{
		"en":      "artifacts:    removed 2026-11-01, past the 30-day retention; the result and summary are kept",
		"zh-Hant": "成果：        已於 2026-11-01 回收（超過 30 天保留期限）；結果與摘要仍保留",
	} {
		commandLanguage = language
		var out bytes.Buffer
		writeTaskView(&out, task)
		if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "The delivered summary") {
			t.Errorf("%s view:\n%s\nwant line %q and the summary", language, out.String(), want)
		}
	}
	task.ArtifactsExpired = nil
	commandLanguage = "en"
	var out bytes.Buffer
	writeTaskView(&out, task)
	if strings.Contains(out.String(), "removed") {
		t.Errorf("a task whose artifacts never expired says they did:\n%s", out.String())
	}
}
