package http

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// TestSendToMacWritesTheReportAReadOnlyPhoneSent: the console's "Send to Mac"
// posts to this route, and a read-only phone is exactly who presses it.
func TestSendToMacWritesTheReportAReadOnlyPhoneSent(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	_, token := f.device("phone", false)
	first := f.do(http.MethodPost, diagnosticReportRoute, token, `{"completeness":{"whole":true},"current":{"page":"one"}}`)
	if first.Code != http.StatusOK {
		t.Fatalf("send: %d %s", first.Code, first.Body)
	}
	var receipt diagnosticReportReceipt
	if err := json.Unmarshal(first.Body.Bytes(), &receipt); err != nil || !receipt.OK || receipt.Path == "" || !receipt.CompletenessStated || receipt.Previous != "" {
		t.Fatalf("receipt: %s (%v)", first.Body, err)
	}
	info, err := os.Stat(receipt.Path)
	if err != nil || info.Size() != receipt.Bytes || info.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", info, err)
	}
	var envelope struct {
		Version   int    `json:"clawdline_diagnostic_report"`
		WrittenBy string `json:"written_by"`
		Report    struct {
			Current struct{ Page string } `json:"current"`
		} `json:"report"`
	}
	raw, _ := os.ReadFile(receipt.Path)
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 || envelope.WrittenBy == "" || envelope.Report.Current.Page != "one" {
		t.Fatalf("envelope: %s", raw)
	}

	second := f.do(http.MethodPost, diagnosticReportRoute, token, `{"current":{"page":"two"}}`)
	var next diagnosticReportReceipt
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &next) != nil || next.Path != receipt.Path || next.Previous == "" || next.CompletenessStated {
		t.Fatalf("second: %d %s", second.Code, second.Body)
	}
	if kept, _ := os.ReadFile(next.Previous); !strings.Contains(string(kept), `"one"`) {
		t.Fatalf("the press before was not kept: %s", kept)
	}
}

func TestSendToMacRefusesWhatItCannotKeepWhole(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	_, token := f.device("phone", false)
	for _, c := range []struct{ body, code string }{
		{"", "empty_report"},
		{"[1]", "report_not_json"},
		{"not json", "report_not_json"},
		{`{"x":"` + strings.Repeat("a", int(CapacityLimit(capacity.DiagnosticsReportBytes))) + `"}`, "report_too_large"},
	} {
		if got := f.do(http.MethodPost, diagnosticReportRoute, token, c.body); refusalCode(got) != c.code {
			t.Fatalf("%.20q: %d %s", c.body, got.Code, got.Body)
		}
	}
	if got := f.do(http.MethodPost, diagnosticReportRoute, "", `{}`); got.Code != http.StatusUnauthorized {
		t.Fatalf("unpaired: %d", got.Code)
	}
}
