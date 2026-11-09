//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestGrokUnmatchedRepliesWaitForTheExactPromptAndPermission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	requests := make(chan *PermissionRequest, 1)
	c, dir := fakePermission(t, "permission-unmatched", nil, func(_ context.Context, p *PermissionRequest) error { requests <- p; return nil })
	ready(t, c)
	// Authentication is covered by the provider tests. Only transport routing
	// differs here; all three setup RPCs use the existing synthetic peer.
	c.opts.Assistant = session.AssistantGrok
	for turn := 0; turn < 2; turn++ {
		done := make(chan error, 1)
		go func() { _, err := c.Prompt(ctx, "synthetic request"); done <- err }()
		var p *PermissionRequest
		select {
		case err := <-done:
			t.Fatalf("an unrelated response ended the prompt: %v", err)
		case p = <-requests:
		case <-ctx.Done():
			t.Fatal("permission not reached")
		}
		select {
		case err := <-done:
			t.Fatalf("completed before exact permission and response: %v", err)
		default:
		}
		raw, _ := json.Marshal(c.Lifecycle())
		var life map[string]any
		_ = json.Unmarshal(raw, &life)
		if life["unmatched_responses"] != float64(3) {
			t.Fatalf("missing sanitized mismatch count: %s", raw)
		}
		if strings.Contains(string(raw), "synthetic-private-response") {
			t.Fatal("provider ID leaked")
		}
		if err := p.Select(ctx, "reject-exact", nil); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if life := c.Lifecycle(); life.Phase != "idle" || life.StopReason != "end_turn" {
			t.Fatal(life)
		}
		if got := methods(t, dir); strings.Count(strings.Join(got, ","), "session/prompt") != turn+1 {
			t.Fatal("prompt replayed", got)
		}
		wire := readTestWire(t, filepath.Join(dir, "wire"))
		if strings.Count(wire, `"id":"permission-exact"`) != turn+1 {
			t.Fatal("permission repeated")
		}
	}
}

func readTestWire(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGrokUnmatchedRepliesDoNotBlockCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	progress := make(chan struct{}, 1)
	c, dir := fakePermission(t, "permission-unmatched-held", func(context.Context, json.RawMessage) error { progress <- struct{}{}; return nil }, nil)
	ready(t, c)
	c.opts.Assistant = session.AssistantGrok
	type outcome struct {
		result PromptResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := c.Prompt(ctx, "synthetic input"); done <- outcome{r, e} }()
	select {
	case <-progress:
	case o := <-done:
		t.Fatalf("unrelated response ended turn: %v", o.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if l := c.Lifecycle(); l.Phase != "prompt_pending" || l.UnmatchedResponses != 3 || l.CompletedAt != 0 {
		t.Fatal(l)
	}
	if err := c.Interrupt(ctx, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case o := <-done:
		if o.err != nil || o.result.StopReason != "cancelled" {
			t.Fatal(o)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := methods(t, dir); strings.Count(strings.Join(got, ","), "session/prompt") != 1 {
		t.Fatal(got)
	}
}

func TestGrokRoutingStillRejectsInvalidRepliesAndMatchingRPCErrors(t *testing.T) {
	for _, tc := range []struct {
		mode, code string
		grok       bool
		delivery   Delivery
	}{
		{"permission-unmatched-invalid", "invalid_response", true, Uncertain},
		{"permission-unmatched", "unexpected_id", false, Uncertain},
		{"prompt-rpc-error", "rpc_error", true, Acknowledged},
	} {
		t.Run(tc.mode+tc.code, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			c, dir := fakePermission(t, tc.mode, nil, nil)
			ready(t, c)
			if tc.grok {
				c.opts.Assistant = session.AssistantGrok
			}
			_, err := c.Prompt(ctx, "synthetic input")
			requireError(t, err, tc.code, tc.delivery)
			if got := methods(t, dir); strings.Count(strings.Join(got, ","), "session/prompt") != 1 {
				t.Fatal(got)
			}
		})
	}
}
