package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestNativeConfigInvalidSnapshotsPreservePreviousState(t *testing.T) {
	valid := json.RawMessage(`{"configOptions":[{"id":"model","type":"select","currentValue":"synthetic-flash","options":[{"value":"synthetic-flash","name":"Synthetic Flash"}]},{"id":"thinking_level","type":"select","currentValue":"high","options":[{"value":"low"},{"value":"high"}]}]}`)
	c := &conversation{}
	if err := c.readMetadata(valid, true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{}`, `{"configOptions":null}`, `{"configOptions":{}}`,
		`{"configOptions":[{"id":"thinking_level","type":"select","currentValue":"low","options":[{"value":"low"},{"value":"low"}]}]}`,
		`{"configOptions":[{"id":"model","type":"select","currentValue":"new","options":[]},{"id":"thinking_level","type":"select","currentValue":false}]}`,
	} {
		if err := c.readMetadata(json.RawMessage(raw), false); err == nil {
			t.Fatalf("accepted %s", raw)
		}
		if c.model != "synthetic-flash" || c.effort != "high" || len(c.efforts) != 2 {
			t.Fatal("invalid snapshot partially applied", c.model, c.effort, c.efforts)
		}
	}
	if err := c.readMetadata(json.RawMessage(`{"configOptions":[]}`), false); err != nil {
		t.Fatal(err)
	}
	if c.model != "" || c.effort != "" || len(c.models) != 0 || len(c.efforts) != 0 {
		t.Fatal("removed native controls remained offered")
	}
}

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

func TestQuotaReadsBothNativeGeminiWindows(t *testing.T) {
	text := "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 100% remaining (resets Nov 06, 2026, 01:26 UTC)\n  - **Five Hour Limit Remaining:** 75% remaining (resets Oct 30, 2026, 06:26 UTC)\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining"
	q := parseQuota(text, time.Now())
	if len(q.Windows) != 2 {
		t.Fatalf("windows=%+v", q.Windows)
	}
	if q.Windows[0].Name != "5h" || q.Windows[0].UsedPercent != 25 || q.Windows[1].Name != "7d" || q.Windows[1].UsedPercent != 0 {
		t.Fatalf("windows=%+v", q.Windows)
	}
}

// Separate model buckets must remain visible even when the general group is healthy.
func TestQuotaReadsSeparateArgonBucket(t *testing.T) {
	text := "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 87% remaining\n  - **Five Hour Limit Remaining:** 100% remaining\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 100% remaining\n- **Gemini 4 Argon**\n  *(For paid Pro plans, Argon quota is separate.)*\n  - **Gemini 4 Argon Weekly Limit:** 0% remaining (resets Nov 6, 2026, 01:57 UTC)"
	q := parseQuota(text, time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC))
	if q.UnknownReason != "" || len(q.Windows) != 3 {
		t.Fatalf("separate bucket lost: %+v", q)
	}
	want := map[string]float64{"5h": 0, "7d": 13, "Argon 7d": 100}
	for _, w := range q.Windows {
		used, ok := want[w.Name]
		if !ok || w.UsedPercent != used || w.Hit != (used == 100) {
			t.Fatalf("wrong quota window: %+v", w)
		}
		if w.Name == "Argon 7d" && w.ResetsAt != time.Date(2026, 11, 6, 1, 57, 0, 0, time.UTC).Unix() {
			t.Fatalf("wrong Argon reset: %+v", w)
		}
		delete(want, w.Name)
	}
	if len(want) != 0 {
		t.Fatal("missing windows", want)
	}
}

func TestQuotaArgonPreservesZeroAndUnknown(t *testing.T) {
	prefix := "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 95% remaining\n- **Gemini 4 Argon**\n"
	for _, tc := range []struct {
		name, line string
		known      bool
		used       float64
	}{
		{"unused", "  - **Gemini 4 Argon Weekly Limit:** 100% remaining", true, 0},
		{"fraction", "  - **Gemini 4 Argon Weekly Limit:** 62.5% remaining", true, 37.5},
		{"unavailable", "  - **Gemini 4 Argon Weekly Limit:** unavailable", false, 0},
		{"out-of-range", "  - **Gemini 4 Argon Weekly Limit:** 101% remaining", false, 0},
		{"bad-reset", "  - **Gemini 4 Argon Weekly Limit:** 42% remaining (resets tomorrow UTC)", false, 0},
		{"duplicate", "  - **Gemini 4 Argon Weekly Limit:** 42% remaining\n  - **Gemini 4 Argon Weekly Limit:** 41% remaining", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := parseQuota(prefix+tc.line, time.Now())
			if !tc.known {
				if len(q.Windows) != 0 || q.UnknownReason != contract.AssistantUnknownReasonNoReading {
					t.Fatalf("unknown Argon became healthy: %+v", q)
				}
				return
			}
			if len(q.Windows) != 2 || q.Windows[1].Name != "Argon 7d" || q.Windows[1].UsedPercent != tc.used {
				t.Fatalf("Argon=%+v", q)
			}
		})
	}
	q := parseQuota("### Quota Limits\n- **Claude and GPT models**\n  - **Gemini 4 Argon Weekly Limit:** 0% remaining", time.Now())
	if len(q.Windows) != 0 {
		t.Fatal("read Argon from unrelated group", q)
	}
}

func TestNativeConfigKeepsOpaqueOfferedValues(t *testing.T) {
	c := &conversation{}
	raw := json.RawMessage(`{"configOptions":[{"id":"model","type":"select","currentValue":"future model","options":[{"value":"future model","name":"Future"}]},{"id":"thinking_level","type":"select","currentValue":"adaptive high","options":[{"value":"adaptive high"}]}]}`)
	if err := c.readMetadata(raw, true); err != nil {
		t.Fatal(err)
	}
	if c.model != "future model" || c.effort != "adaptive high" || len(c.models) != 1 || len(c.efforts) != 1 {
		t.Fatal("opaque native IDs were discarded")
	}
}

// Opt-in metadata acceptance only: four ACP RPCs, fixed /usage, no model input.
func TestNativeAccountQuotaMetadataOnly(t *testing.T) {
	path := os.Getenv("CLAWDLINE_GEMINI_NATIVE_QUOTA_TEST_CONFIG")
	if path == "" {
		t.Skip("explicit native quota configuration required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("native configuration unavailable")
	}
	var cfg Options
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("invalid native configuration")
	}
	h, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q, err := h.AccountQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Windows) == 0 || q.UnknownReason != "" {
		t.Fatalf("quota unknown: %v", q.UnknownReason)
	}
	for _, w := range q.Windows {
		t.Logf("%s used=%g%% reset=%d", w.Name, w.UsedPercent, w.ResetsAt)
	}
	inv, err := h.Inventory(ctx)
	if err != nil || len(inv.Sessions) != 0 {
		t.Fatal("metadata created a user Session")
	}
	t.Log("metadata RPCs=4; model inputs=0; usage-priced API calls=0")
}
