package limits

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// When a Pro account's weekly window reached 100%, Codex kept answering and
// its records went on saying has_credits with a balance that fell turn by
// turn. A reading that only said "exhausted" left a person spending credits
// without being told. The reading says credits are in use, and with what
// balance, for as long as the spent window is live.
func TestASpentCodexWindowWithCreditsSaysCreditsAreInUse(t *testing.T) {
	now := time.Date(2026, 11, 11, 2, 30, 0, 0, time.UTC)
	reset := now.Add(36 * time.Hour).Unix()
	line := func(at time.Time, used float64, has bool, balance string) string {
		return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":%v,"window_minutes":10080,"resets_at":%d},"secondary":null,"credits":{"has_credits":%v,"unlimited":false,"balance":%q},"plan_type":"pro"}}}`,
			at.Format(time.RFC3339Nano), used, reset, has, balance)
	}
	read := func(t *testing.T, body string, at time.Time) Quota {
		t.Helper()
		home := t.TempDir()
		t.Setenv("CODEX_HOME", "")
		rollout(t, filepath.Join(home, ".codex", "sessions"), now, "a", body+"\n")
		return NewReader(home, nil).Quota("codex", at)
	}

	spent := line(now.Add(-time.Minute), 100, true, "44718.8322010000")
	q := read(t, line(now.Add(-time.Hour), 99, true, "53462.82")+"\n"+spent, now)
	if q.Availability != Exhausted || !q.CreditsInUse || q.CreditBalance == nil ||
		fmt.Sprintf("%.4f", *q.CreditBalance) != "44718.8322" {
		t.Fatalf("spent window with credits: %+v", q)
	}
	if !strings.Contains(q.Detail, "; using credits (balance 44719);") {
		t.Fatalf("the sentence does not say credits are in use: %q", q.Detail)
	}

	// A window not yet spent draws on the plan, not the credits.
	if q := read(t, line(now.Add(-time.Minute), 99, true, "53462.82"), now); q.CreditsInUse || q.CreditBalance != nil {
		t.Fatalf("an unspent window is not credits in use: %+v", q)
	}
	// No credits left is the plan being exhausted, nothing more.
	if q := read(t, line(now.Add(-time.Minute), 100, false, "0"), now); q.CreditsInUse || q.Availability != Exhausted {
		t.Fatalf("a spent window without credits: %+v", q)
	}
	// Once the spent window resets, the next turn is the plan's again.
	if q := read(t, spent, time.Unix(reset+60, 0)); q.CreditsInUse || q.CreditBalance != nil {
		t.Fatalf("a reset window is not credits in use: %+v", q)
	}
}
