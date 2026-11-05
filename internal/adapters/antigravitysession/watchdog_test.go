//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
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

// Exercise the actual watchdog with an aged activity timestamp rather than
// spending minutes in a test or calling a model provider.
func TestDefaultWatchdogPreservesTenMinuteSilentTool(t *testing.T) {
	t.Setenv("CLW_SILENT_TOOL_PEER", "yes")
	t.Setenv("CLAWDLINE_ACP_INACTIVITY_TIMEOUT", "")
	h, row, dir := fixture(t)
	if err := h.Send(context.Background(), row, "wait for background work"); err != nil {
		t.Fatal(err)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		active := false
		for _, e := range page.Entries {
			if e.Activity != nil && e.Activity.Status == "inProgress" {
				active = true
			}
		}
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("silent tool never started")
		}
		time.Sleep(time.Millisecond)
	}
	c.lastActivity.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	// Production watchdog checks at most once per second. Cross that interval.
	time.Sleep(1500 * time.Millisecond)
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].State != session.StateWorking {
		t.Fatalf("ten-minute silent tool was cancelled: %+v, %v", inv, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	for _, e := range page.Entries {
		if e.Role == "error" {
			t.Fatalf("silent tool failed: %+v", page.Entries)
		}
		if e.Activity != nil && e.Activity.Status == "completed" {
			completed = true
		}
	}
	if !completed {
		t.Fatalf("tool did not complete: %+v", page.Entries)
	}
}

func TestDefaultWatchdogStillCancelsSilentHungTurn(t *testing.T) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	t.Setenv("CLAWDLINE_ACP_INACTIVITY_TIMEOUT", "")
	h, row, dir := fixture(t)
	if err := h.Send(context.Background(), row, "hung background work"); err != nil {
		t.Fatal(err)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	c.lastActivity.Store(time.Now().Add(-16 * time.Minute).UnixNano())
	idleHost(t, h, row)
	raw, err := os.ReadFile(filepath.Join(dir, "cancelled"))
	if err != nil || string(raw) != row.ConversationID {
		t.Fatalf("missing cancellation receipt: %q, %v", raw, err)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	timeouts := 0
	for _, e := range page.Entries {
		if e.Role == "error" && e.Text == "antigravity_turn_inactivity_timeout" {
			timeouts++
		}
	}
	if timeouts != 1 {
		t.Fatalf("expected one inactivity error: %+v", page.Entries)
	}
}

func TestInactivityWatchdogWaitsForAllToolsThenResumes(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "durable"}[durable], func(t *testing.T) {
			t.Setenv("CLW_WATCHDOG_TOOL_PEER", "yes")
			t.Setenv("CLAWDLINE_ACP_INACTIVITY_TIMEOUT", "100ms")
			var h *Host
			var row session.Session
			var dir string
			if durable {
				var opts Options
				h, row, opts = persistentFixture(t)
				dir = opts.AccountDir
			} else {
				h, row, dir = fixtureWithTimeout(t, 100*time.Millisecond)
			}
			if err := h.Send(context.Background(), row, "wait for both tools"); err != nil {
				t.Fatal(err)
			}
			c, err := h.lookup(row)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				c.mu.Lock()
				count := len(c.activeTools)
				c.mu.Unlock()
				if count == 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("tools never became active")
				}
				time.Sleep(time.Millisecond)
			}
			// Even an hour of silence is valid while a tool is explicitly active.
			c.lastActivity.Store(time.Now().Add(-time.Hour).UnixNano())
			if !durable {
				c.mu.Lock()
				c.append(contract.TranscriptEntry{Role: "assistant", Text: strings.Repeat("x", MaxViewBytes)})
				c.mu.Unlock()
			}
			time.Sleep(250 * time.Millisecond)
			assertWorking := func() {
				t.Helper()
				inv, err := h.Inventory(context.Background())
				if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].State != session.StateWorking {
					t.Fatalf("watchdog cancelled an active tool: %+v, %v", inv, err)
				}
			}
			assertWorking()
			emit := func(status any, id string) {
				t.Helper()
				update := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id}
				if status != nil {
					update["status"] = status
				}
				frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": row.ConversationID, "update": update}})
				if err := c.update(context.Background(), frame); err != nil {
					t.Fatal(err)
				}
			}
			emit("completed", "tool-one")
			emit("completed", "tool-one") // duplicate must not release the second tool
			emit(nil, "tool-one")         // content-only delta must not reopen a completed tool
			time.Sleep(250 * time.Millisecond)
			assertWorking()
			if got := h.ActiveToolBytes(); got != len("tool-two") {
				t.Fatalf("remaining tool bytes=%d", got)
			}
			emit("failed", "tool-two")
			idleHost(t, h, row) // inactivity protection is live again
			raw, err := os.ReadFile(filepath.Join(dir, "cancelled"))
			if err != nil || string(raw) != row.ConversationID {
				t.Fatalf("missing cancellation: %q, %v", raw, err)
			}
			if got := h.ActiveToolBytes(); got != 0 {
				t.Fatalf("finished turn retained %d tool bytes", got)
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := h.Send(context.Background(), row, "follow-up"); err != nil {
				t.Fatal(err)
			}
			idleHost(t, h, row)
		})
	}
}

func TestActiveToolTrackingIsBoundedAndReleased(t *testing.T) {
	c := &conversation{}
	emit := func(id string, status *string, initial bool) error {
		return c.trackActiveTool(&antigravityacp.ToolCallDelta{ToolCallID: id, Status: status}, initial)
	}
	id := strings.Repeat("x", MaxActiveToolBytes)
	if err := emit(id, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := emit(id, nil, true); err != nil {
		t.Fatal(err)
	}
	if c.activeToolBytes != MaxActiveToolBytes || len(c.activeTools) != 1 {
		t.Fatal("duplicate tool consumed capacity")
	}
	var refusal *Error
	if err := emit("overflow", nil, true); !errors.As(err, &refusal) || refusal.Code != "view_too_large" {
		t.Fatalf("expected typed refusal, got %v", err)
	}
	completed := "completed"
	if err := emit(id, &completed, false); err != nil {
		t.Fatal(err)
	}
	if err := emit(id, nil, false); err != nil {
		t.Fatal(err)
	}
	if c.activeToolBytes != 0 || len(c.activeTools) != 0 {
		t.Fatal("terminal tool retained or reopened")
	}
	if err := emit("next", nil, true); err != nil {
		t.Fatal(err)
	}
}

func TestStaleTimedOutFlagDoesNotMaskWriteFailure(t *testing.T) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	h, row, dir := fixtureWithTimeout(t, 100*time.Millisecond)
	ctx := context.Background()

	// 1. First turn times out
	if err := h.Send(ctx, row, "hung input"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)

	// Verify it settled to idle
	inv, _ := h.Inventory(ctx)
	if inv.Sessions[0].State != session.StateIdle {
		t.Fatalf("expected idle, got %s", inv.Sessions[0].State)
	}

	// 2. Second turn fails on before() check (simulating write failure before write/acceptance)
	expectedErr := errors.New("simulated before write failure")
	err := h.SendProtocol(ctx, row, "failing input", func() error {
		return expectedErr
	})
	if err == nil || !strings.Contains(err.Error(), "simulated before write failure") {
		t.Fatalf("expected simulated before write failure, got: %v", err)
	}

	// 3. Transcript must not have duplicate or falsely attributed inactivity timeout for second turn
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	timeoutCount := 0
	for _, e := range page.Entries {
		if e.Role == "error" && e.Text == "antigravity_turn_inactivity_timeout" {
			timeoutCount++
		}
	}
	if timeoutCount != 1 {
		t.Fatalf("expected exactly 1 timeout error from turn 1, got %d", timeoutCount)
	}
	_ = dir
}

func TestWatchdogRaceWithEndTurnDoesNotEmitTimeoutError(t *testing.T) {
	// If stopReason is "end_turn", even if timedOut flag was set at the very last moment,
	// the completed turn must not append an inactivity timeout error.
	h, row, dir := fixtureWithTimeout(t, 0)
	ctx := context.Background()

	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	// Manually set timedOut before sending a prompt that succeeds normally
	c.timedOut.Store(true)

	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(ctx, row, "prompt with stale timedOut"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)

	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.Role == "error" && e.Text == "antigravity_turn_inactivity_timeout" {
			t.Fatalf("successful end_turn must not have inactivity timeout error: %+v", page.Entries)
		}
	}
}
