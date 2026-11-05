package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestCancelledStepsSettleWorkWithoutBecomingCompleted(t *testing.T) {
	ctx := context.Background()
	w := newWorkV2Test(t)
	v := implementingWorkV2Test(t, w)
	for _, title := range []string{"完成必要修改", "取消不再需要的處理"} {
		var err error
		v, err = w.AddStep(ctx, v.Item.ID, AddStepV2{ExpectedVersion: v.Item.Version, SessionID: "session-a", Title: title}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	full, err := w.Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, second := full.Steps[0], full.Steps[1]
	v, err = w.CompleteStep(ctx, v.Item.ID, first.ID, "session-a", AnyVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	landedTaskFor(t, w, "7a5c0000-0000-4000-8000-0000000000f8", v.Item.ID)
	finish := func() error {
		_, err := w.Finish(ctx, v.Item.ID, FinishWorkV2{ExpectedVersion: AnyVersion, SessionID: "session-a", Actor: "session-a", Verification: "focused tests passed", Deployment: "synthetic deployment verified"}, nil)
		return err
	}
	if err := finish(); err == nil || !strings.Contains(err.Error(), "steps_incomplete") {
		t.Fatalf("open step did not block finish: %v", err)
	}
	for _, tc := range []struct {
		session, reason, code string
		version               int64
	}{
		{"session-a", " ", "reason_required", AnyVersion},
		{"session-a", strings.Repeat("a", workV2CompletionReasonLimit+1), "reason_too_large", AnyVersion},
		{"other-session", "No longer needed", "not_item_owner", AnyVersion},
		{"session-a", "No longer needed", "version_conflict", 1},
	} {
		_, err := w.ChangeStep(ctx, v.Item.ID, second.ID, tc.session, tc.version, "cancel", tc.reason, nil)
		if err == nil || !strings.Contains(err.Error(), tc.code) {
			t.Fatalf("%s: %v", tc.code, err)
		}
	}
	reason := "已與使用者確認不再需要"
	v, err = w.ChangeStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, "cancel", reason, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := v.Steps[0]
	if cancelled.Open() || cancelled.Done || !cancelled.CompletedAt.IsZero() || cancelled.CompletedBy != "" || cancelled.Status() != "cancelled" || cancelled.CancelReason != reason || cancelled.CancelledBy != "session-a" {
		t.Fatalf("cancellation became completion: %+v", cancelled)
	}
	// Repeating the intent preserves the original cancellation attribution/reason.
	again, err := w.ChangeStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, "cancel", "Another reason", nil)
	if err != nil || again.Steps[0].CancelReason != reason || again.Steps[0].Version != cancelled.Version {
		t.Fatalf("repeat: %+v %v", again.Steps, err)
	}
	if _, err := w.CompleteStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, nil); err == nil || !strings.Contains(err.Error(), "step_cancelled") {
		t.Fatalf("completed cancellation: %v", err)
	}
	if _, err := w.ChangeStep(ctx, v.Item.ID, first.ID, "session-a", AnyVersion, "cancel", reason, nil); err == nil || !strings.Contains(err.Error(), "step_completed") {
		t.Fatalf("cancelled completion: %v", err)
	}
	v, err = w.ChangeStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, "reopen", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Steps[0].Open() || v.Steps[0].CancelReason != "" || v.Steps[0].CancelledBy != "" {
		t.Fatalf("not reopened: %+v", v.Steps)
	}
	if err := finish(); err == nil || !strings.Contains(err.Error(), "steps_incomplete") {
		t.Fatalf("reopened step did not block finish: %v", err)
	}
	if _, err := w.ChangeStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, "cancel", reason, nil); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatalf("cancelled step blocked finish: %v", err)
	}
	full, err = w.Item(ctx, v.Item.ID)
	if err != nil || full.Item.Phase != work.PhaseDone || full.Steps[1].Done || full.Steps[1].CancelReason != reason {
		t.Fatalf("closed history: %+v %v", full, err)
	}
	if _, err := w.ChangeStep(ctx, v.Item.ID, second.ID, "session-a", AnyVersion, "reopen", "", nil); err == nil {
		t.Fatal("changed a closed item's step")
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "step.cancelled" && strings.Contains(event.Payload, reason) {
			found = true
		}
	}
	if !found {
		t.Fatal("cancellation reason missing from history")
	}
}
