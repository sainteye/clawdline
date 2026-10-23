package antigravitysession

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// Synthetic values follow the sanitized ACP 1.4.1 shape; no account reading is
// retained. The Gemini and other-model group values deliberately disagree.
const syntheticQuota = "### Session Usage\n- **Session total:** data not available yet\n\n### Quota Limits\n- **Gemini Models**\n  *(Models within this group: Gemini Flash, Gemini Pro)*\n  - **Weekly Limit Remaining:** 62% remaining (resets Oct 29, 2026, 07:51 UTC)\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining (resets Oct 30, 2026, 08:29 UTC)"

func TestQuotaUsesOnlyNativeGeminiNumericWindow(t *testing.T) {
	at := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	q := parseQuota(syntheticQuota, at)
	reset := time.Date(2026, 10, 29, 7, 51, 0, 0, time.UTC).Unix()
	if len(q.Windows) != 1 || q.Windows[0].Name != "7d" || q.Windows[0].UsedPercent != 38 || q.Windows[0].ResetsAt != reset || q.Windows[0].Hit || q.UnknownReason != "" {
		t.Fatalf("quota=%+v windows=%+v", q, q.Windows)
	}
	held := quotaSnapshot(q, at.Add(12*time.Second))
	if held.AgeSeconds == nil || *held.AgeSeconds != 12 || held.At != at.Unix() || held.ReadAtMs != q.ReadAtMs || held.Detail != "Gemini · ACP /usage" {
		t.Fatalf("age/source changed: %+v", held)
	}
	held.Windows[0].UsedPercent = 99
	if q.Windows[0].UsedPercent != 38 {
		t.Fatal("snapshot mutated stored reading")
	}
	if !quotaSnapshot(q, time.Unix(reset, 0)).Stale {
		t.Fatal("expired reset kept reading current")
	}
}

func TestQuotaPreservesZeroAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		known      bool
		used       float64
		hit        bool
	}{
		{"zero-used", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 100% remaining", true, 0, false},
		{"zero-remaining", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 0% remaining", true, 100, true},
		{"fraction", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 62.5% remaining", true, 37.5, false},
		{"other-group-only", "### Quota Limits\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining", false, 0, false},
		{"gemini-empty-other-numeric", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** unavailable\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining", false, 0, false},
		{"context-only", "### Session Usage\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 9% remaining", false, 0, false},
		{"out-of-range", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 101% remaining", false, 0, false},
		{"bad-reset", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 42% remaining (resets tomorrow UTC)", false, 0, false},
		{"duplicate", "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 42% remaining\n  - **Weekly Limit Remaining:** 41% remaining", false, 0, false},
		{"unrecognized", "SECRET provider error /account/path", false, 0, false},
		{"empty", "", false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := parseQuota(tc.text, time.Now())
			if tc.known {
				if len(q.Windows) != 1 || q.Windows[0].UsedPercent != tc.used || q.Windows[0].Hit != tc.hit || q.AgeSeconds == nil {
					t.Fatalf("%+v", q)
				}
			} else if len(q.Windows) != 0 || q.UnknownReason != contract.AssistantUnknownReasonNoReading || q.At != 0 || q.AgeSeconds != nil {
				t.Fatalf("unknown became number: %+v", q)
			}
		})
	}
}
