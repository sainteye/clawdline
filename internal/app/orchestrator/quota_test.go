package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAConstrainedAssistantWarnsAndDispatchesWithItsEvidence(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 21, 4, 30, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	claudeAt, codexAt := now.Add(-20*time.Minute).Unix(), now.Add(-30*time.Second).Unix()
	claudeAge, codexAge := int64(1200), int64(30)
	claudeFresh, codexFresh := int64(900), int64(21600)
	reads := 0
	b.AssistantQuotas = func(at time.Time) ([]AssistantQuotaSnapshot, error) {
		reads++
		if !at.Equal(now) {
			t.Fatalf("quota read at %v, want %v", at, now)
		}
		return []AssistantQuotaSnapshot{
			{ID: "claude", Label: "Claude Code", Installed: true, Availability: "low",
				ObservedAt: &claudeAt, AgeSeconds: &claudeAge, FreshForSeconds: &claudeFresh,
				Stale: true, Detail: "5h 13%, 7d 87%; read 20m ago"},
			{ID: "codex", Label: "Codex", Installed: true, Availability: "ok",
				ObservedAt: &codexAt, AgeSeconds: &codexAge, FreshForSeconds: &codexFresh,
				Detail: "7d 15%; read 30s ago"},
		}, nil
	}

	id := "a1100000-0000-4000-8000-000000000001"
	writeBrief(t, b, id, b.Dir, nil)
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	if err != nil {
		t.Fatalf("quota refused the dispatch: %v", err)
	}
	warning := warningWithCode(out.Warnings, "assistant_quota_choice")
	if warning == nil {
		t.Fatalf("warnings = %+v, want assistant_quota_choice", out.Warnings)
	}
	for _, fact := range []string{"5h 13%, 7d 87%", "7d 15%", "2026-09-21T04:10:00Z",
		"age_seconds=1200", "fresh_for_seconds=900", "stale=true", "quota never refuses work"} {
		if !strings.Contains(warning.Message, fact) {
			t.Errorf("warning %q does not name %q", warning.Message, fact)
		}
	}

	stored, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AssistantQuota == nil || !stored.AssistantQuota.ReadAt.Equal(now) || len(stored.AssistantQuota.Assistants) != 2 {
		t.Fatalf("stored quota evidence = %+v", stored.AssistantQuota)
	}
	if got := stored.AssistantQuota.Assistants[0]; got.Availability != "low" || got.AgeSeconds == nil || *got.AgeSeconds != 1200 {
		t.Fatalf("stored selected row = %+v", got)
	}

	// A caller retrying an answer it did not receive gets the warning made
	// from the stored evidence. It does not re-read accounts that have moved.
	replayed, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: "not-used-on-replay"})
	if err != nil || !replayed.Replayed || warningWithCode(replayed.Warnings, "assistant_quota_choice") == nil {
		t.Fatalf("replay = %+v, err %v", replayed, err)
	}
	if reads != 1 {
		t.Fatalf("quota was read %d times; replay must use the recorded decision", reads)
	}
}

func TestAnUnreadableQuotaSaysSoWithoutBecomingAGate(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 21, 5, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.AssistantQuotas = func(time.Time) ([]AssistantQuotaSnapshot, error) {
		return nil, errors.New("quota files could not be read")
	}
	id := "a1100000-0000-4000-8000-000000000002"
	writeBrief(t, b, id, b.Dir, nil)
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	if err != nil {
		t.Fatalf("an unreadable quota refused the dispatch: %v", err)
	}
	warning := warningWithCode(out.Warnings, "assistant_quota_unreadable")
	if warning == nil || !strings.Contains(warning.Message, "quota files could not be read") ||
		!strings.Contains(warning.Message, "continued") {
		t.Fatalf("warnings = %+v", out.Warnings)
	}
	stored, _, err := b.Record(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AssistantQuota == nil || stored.AssistantQuota.ReadError != "quota files could not be read" ||
		!stored.AssistantQuota.ReadAt.Equal(now) {
		t.Fatalf("stored quota failure = %+v", stored.AssistantQuota)
	}
}

func TestAnUnknownInstalledReadingIsNotSilent(t *testing.T) {
	decision := &AssistantQuotaDecision{ReadAt: time.Now(), Assistants: []AssistantQuotaSnapshot{
		{ID: "claude", Label: "Claude Code", Installed: true, Availability: "ok", Detail: "5h 20%"},
		{ID: "codex", Label: "Codex", Installed: true, Availability: "unknown",
			Detail: "unknown: a record is there and could not be read", UnknownReason: "unreadable"},
	}}
	warning := warningWithCode(quotaWarnings("claude", decision), "assistant_quota_unknown")
	if warning == nil || !strings.Contains(warning.Message, "could not be read") {
		t.Fatalf("warning = %+v", warning)
	}
}

func warningWithCode(warnings []Warning, code string) *Warning {
	for i := range warnings {
		if warnings[i].Code == code {
			return &warnings[i]
		}
	}
	return nil
}
