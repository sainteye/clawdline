package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
)

func TestATaskRowCarriesTheQuotaDecisionThatDispatchedIt(t *testing.T) {
	readAt := time.Date(2026, 9, 21, 4, 30, 0, 0, time.UTC)
	observed, age, fresh, used := readAt.Add(-2*time.Minute).Unix(), int64(120), int64(900), 87.0
	in := &orchestrator.AssistantQuotaDecision{ReadAt: readAt, Assistants: []orchestrator.AssistantQuotaSnapshot{
		{ID: "claude", Label: "Claude Code", Installed: true, Availability: "low",
			ObservedAt: &observed, AgeSeconds: &age, FreshForSeconds: &fresh,
			Detail: "7d 87%; read 2m ago", Windows: []orchestrator.AssistantQuotaWindow{
				{Name: "7d", UsedPercent: &used},
			}},
	}}
	out := brokerAssistantQuotaDecision(in)
	if out.ReadAt != readAt.Unix() || len(out.Assistants) != 1 {
		t.Fatalf("wire decision = %+v", out)
	}
	row := out.Assistants[0]
	if row.Availability != "low" || row.AgeSeconds == nil || *row.AgeSeconds != 120 ||
		len(row.Windows) != 1 || row.Windows[0].UsedPercent != 87 {
		t.Fatalf("wire quota row = %+v", row)
	}
}
