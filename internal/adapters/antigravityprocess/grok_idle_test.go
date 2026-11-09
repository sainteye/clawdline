//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGrokIdleRepliesDoNotBlockNotificationsOrTheNextRequest(t *testing.T) {
	for _, open := range []string{"new", "load"} {
		t.Run(open, func(t *testing.T) {
			t.Setenv("CLW_XIDLE", "control")
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			progress := make(chan struct{}, 8)
			c, dir := grokProcess(t, "ready", func(_ context.Context, raw json.RawMessage) error {
				if strings.Contains(string(raw), "synthetic-idle-reader-alive") {
					progress <- struct{}{}
				}
				return nil
			})
			if _, err := c.AuthenticatePersonal(ctx); err != nil {
				t.Fatal(err)
			}
			var err error
			if open == "new" {
				_, err = c.NewSession(ctx)
			} else {
				_, err = c.LoadSession(ctx, "synthetic-coding-session")
			}
			if err != nil {
				t.Fatal(err)
			}
			idle := func(want int) {
				t.Helper()
				select {
				case <-progress:
				case <-ctx.Done():
					t.Fatal("an idle response blocked subsequent notifications")
				}
				raw, _ := json.Marshal(c.Lifecycle())
				var life map[string]any
				_ = json.Unmarshal(raw, &life)
				if life["orphan_responses"] != float64(want) {
					t.Fatalf("orphan count: %s", raw)
				}
				if strings.Contains(string(raw), "synthetic-idle-reply") || strings.Contains(string(raw), "private body") {
					t.Fatal("orphan identity or body leaked")
				}
			}
			idle(4)
			for n := 0; n < 2; n++ {
				result, e := c.Prompt(ctx, "synthetic continuation")
				if e != nil || result.StopReason != "end_turn" {
					t.Fatal(result, e)
				}
				idle(8 + 4*n)
			}
			if err := c.SetModelChecked(ctx, "synthetic-model", nil); err != nil {
				t.Fatal(err)
			}
			idle(16)
			if l := c.Lifecycle(); l.UnmatchedResponses != 1 || l.Phase != "idle" {
				t.Fatal(l)
			}
			if got := strings.Count(strings.Join(methods(t, dir), ","), "session/prompt"); got != 2 {
				t.Fatal("prompt replayed", got)
			}
		})
	}
}

func TestGrokInvalidIdleReplyStillClosesWithoutSendingAPrompt(t *testing.T) {
	t.Setenv("CLW_XIDLE", "invalid")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, dir := grokProcess(t, "ready", nil)
	if _, err := c.AuthenticatePersonal(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.done:
	case <-ctx.Done():
		t.Fatal("invalid idle reply stranded the reader")
	}
	requireError(t, c.Err(), "invalid_response", Uncertain)
	if strings.Contains(strings.Join(methods(t, dir), ","), "session/prompt") {
		t.Fatal("invalid response triggered model work")
	}
}
