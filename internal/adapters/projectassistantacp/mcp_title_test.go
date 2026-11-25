package projectassistantacp

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Bot whose title is refused must learn why from the tool result, or it
// cannot write a better one without reading this repository's source.
func TestMCPDispatchTitleRefusalCarriesItsReason(t *testing.T) {
	const title = "驗收：應 Bot 請求派工的通知截圖"
	f := newMCPFixture(t, mcpTestTools{dispatch: func(_ context.Context, _, title, _ string, _ []string) (pa.WorkLink, error) {
		return pa.WorkLink{}, pa.ValidateWorkTitle(title)
	}})
	f.initialize(t)
	code, body := f.post(t, mcpCall("dispatch_work", map[string]any{
		"idempotency_key": "titled", "title": title, "brief": "Capture the notice", "claims": []string{"one.go"},
	}), nil)
	reason := work.OutcomeTitleRefusal(title)
	if reason == "" {
		t.Fatal("the fixture title is no longer refused by the title rule")
	}
	if code != http.StatusOK || !bytes.Contains(body, []byte(`"isError":true`)) ||
		!bytes.Contains(body, []byte(`\"field\":\"work_title\"`)) ||
		!bytes.Contains(body, []byte(`\"reason\":`)) || !bytes.Contains(body, []byte("colon")) {
		t.Fatalf("title refusal reached the Bot without its reason: %d %s", code, body)
	}
}
