//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestInactivityWatchdogInterruptsHungPrompt(t *testing.T) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	h, row, dir := fixtureWithTimeout(t, 100*time.Millisecond)
	ctx := context.Background()

	// Send a prompt that will hang with no response from the peer
	if err := h.Send(ctx, row, "hung input"); err != nil {
		t.Fatal(err)
	}

	// Wait for watchdog to trigger interrupt and settle the session to idle
	idleHost(t, h, row)

	// Verify the cancellation frame was received by peer
	raw, err := os.ReadFile(filepath.Join(dir, "cancelled"))
	if err != nil || string(raw) != row.ConversationID {
		t.Fatalf("expected cancel receipt for %s, got %q (err: %v)", row.ConversationID, string(raw), err)
	}

	// Verify Inventory reports StateIdle so UI and Composer unblock queued messages
	inv, err := h.Inventory(ctx)
	if err != nil || len(inv.Sessions) != 1 {
		t.Fatalf("unexpected inventory: %+v, err: %v", inv, err)
	}
	if inv.Sessions[0].State != session.StateIdle {
		t.Fatalf("expected session state %q, got %q", session.StateIdle, inv.Sessions[0].State)
	}

	// Verify Transcript contains error entry indicating inactivity timeout
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	hasTimeoutNotice := false
	for _, e := range page.Entries {
		if e.Role == "error" && e.Text == "antigravity_turn_inactivity_timeout" {
			hasTimeoutNotice = true
			break
		}
	}
	if !hasTimeoutNotice {
		t.Fatalf("expected transcript to contain inactivity timeout error, got: %+v", page.Entries)
	}

	// Now send a follow-up message to ensure session can continue accepting inputs
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(ctx, row, "follow-up input"); err != nil {
		t.Fatalf("follow-up send failed: %v", err)
	}
	idleHost(t, h, row)

	invAfter, err := h.Inventory(ctx)
	if err != nil || len(invAfter.Sessions) != 1 {
		t.Fatalf("inventory after follow-up: %+v, err: %v", invAfter, err)
	}
	if invAfter.Sessions[0].State != session.StateIdle {
		t.Fatalf("expected idle state after follow-up, got %q", invAfter.Sessions[0].State)
	}
}

func TestInactivityWatchdogPreservesActiveStreaming(t *testing.T) {
	t.Setenv("CLW_STREAMING_WATCHDOG_PEER", "yes")
	// 120ms timeout: streaming peer sends 4 chunks spaced 50ms apart (total 200ms > 120ms)
	// Because updates arrive every 50ms (< 120ms), the watchdog must not trigger.
	h, row, _ := fixtureWithTimeout(t, 120*time.Millisecond)
	ctx := context.Background()

	if err := h.Send(ctx, row, "streaming prompt"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)

	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.Role == "error" && e.Text == "antigravity_turn_inactivity_timeout" {
			t.Fatalf("unexpected inactivity timeout during active streaming: %+v", page.Entries)
		}
	}
}

func TestInactivityWatchdogPreservesWaitingPermission(t *testing.T) {
	t.Setenv("CLW_PERMISSION_PEER", "yes")
	h, row, _ := fixtureWithTimeout(t, 100*time.Millisecond)
	ctx := context.Background()

	if err := h.Send(ctx, row, "prompt requesting permission"); err != nil {
		t.Fatal(err)
	}

	// Wait for permission menu to be offered
	deadline := time.Now().Add(2 * time.Second)
	var offeredMenu *session.Menu
	for time.Now().Before(deadline) {
		inv, err := h.Inventory(ctx)
		if err == nil && len(inv.Sessions) == 1 && inv.Sessions[0].State == session.StateWaiting && inv.Sessions[0].Menu != nil {
			offeredMenu = inv.Sessions[0].Menu
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if offeredMenu == nil {
		t.Fatal("permission menu was not offered in time")
	}

	// Sleep 250ms, well beyond the 100ms watchdog timeout!
	time.Sleep(250 * time.Millisecond)

	// State must still be StateWaiting (NOT cancelled by watchdog)
	inv, err := h.Inventory(ctx)
	if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].State != session.StateWaiting {
		t.Fatalf("session should still be waiting on permission, got: %+v", inv)
	}

	// Select first option for the first offer
	fingerprint := session.MenuFingerprint(*offeredMenu)
	if err := h.SelectProtocol(ctx, row, 1, fingerprint, nil); err != nil {
		t.Fatalf("SelectProtocol failed: %v", err)
	}

	// The peer sends a second permission request in CLW_PERMISSION_PEER
	deadline = time.Now().Add(2 * time.Second)
	var secondMenu *session.Menu
	for time.Now().Before(deadline) {
		inv, err := h.Inventory(ctx)
		if err == nil && len(inv.Sessions) == 1 && inv.Sessions[0].State == session.StateWaiting && inv.Sessions[0].Menu != nil && session.MenuFingerprint(*inv.Sessions[0].Menu) != fingerprint {
			secondMenu = inv.Sessions[0].Menu
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if secondMenu != nil {
		_ = h.SelectProtocol(ctx, row, 1, session.MenuFingerprint(*secondMenu), nil)
	}

	idleHost(t, h, row)
	invEnd, err := h.Inventory(ctx)
	if err != nil || len(invEnd.Sessions) != 1 || invEnd.Sessions[0].State != session.StateIdle {
		t.Fatalf("expected session to finish idle after permissions answered, got: %+v", invEnd)
	}
}
