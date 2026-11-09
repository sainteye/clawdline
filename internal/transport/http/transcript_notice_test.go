package http

import (
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"testing"
)

func TestCoordinationNoticeCardFieldsSurviveProjection(t *testing.T) {
	for _, kind := range []string{"pause", "wake"} {
		n := &transcript.Notice{Kind: kind, RequestID: "request", RequesterSessionID: "requester", Reason: "Maintenance", WakeCondition: "wait:ready", Body: "Receiver instructions"}
		row := transcriptEntry(transcript.Entry{Kind: transcript.KindNotice, Text: n.Body, Notice: n})
		if row.Role != "notice" || row.Text != n.Body || row.Notice == nil || row.Notice.Kind != kind || row.Notice.RequestID != n.RequestID || row.Notice.RequesterSessionID != n.RequesterSessionID || row.Notice.Reason != n.Reason || row.Notice.WakeCondition != n.WakeCondition {
			t.Fatalf("coordination card projection: %+v, %+v", row, row.Notice)
		}
	}
}
