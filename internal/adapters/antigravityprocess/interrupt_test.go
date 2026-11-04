//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterruptCancelsNativeTurnAndKeepsConversation(t *testing.T) {
	for _, mode := range []string{"interrupt", "interrupt-permission"} {
		t.Run(mode, func(t *testing.T) {
			c, dir := fake(t, mode, nil)
			permissionReady := make(chan struct{})
			c.opts.OnPermission = func(context.Context, *PermissionRequest) error { close(permissionReady); return nil }
			ready(t, c)
			pid, sessionID := c.PID(), c.SessionID()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			accepted := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				r, err := c.PromptAccepted(ctx, "synthetic first", func() { close(accepted) })
				if err == nil && r.StopReason != "cancelled" {
					err = errors.New("native turn did not acknowledge cancellation")
				}
				done <- err
			}()
			<-accepted
			if mode == "interrupt-permission" {
				<-permissionReady
				if got := c.Lifecycle(); got.Phase != "permission_pending" {
					t.Fatal(got)
				}
			}
			denied := errors.New("write access revoked")
			if err := c.Interrupt(ctx, func() error { return denied }); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			if err := c.Interrupt(ctx, nil); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if got := c.Lifecycle(); got.Phase != "idle" || got.StopReason != "cancelled" || got.CompletedAt == 0 {
				t.Fatal(got)
			}
			if c.PID() != pid || c.SessionID() != sessionID || c.Err() != nil {
				t.Fatal("interruption closed or replaced the conversation")
			}
			if err := c.Interrupt(ctx, nil); err != nil {
				t.Fatal(err)
			}
			r, err := c.Prompt(ctx, "synthetic second")
			if err != nil || r.StopReason != "end_turn" {
				t.Fatal(r.StopReason, err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "wire"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(raw), `"method":"session/cancel"`) != 1 {
				t.Fatalf("cancel writes: %s", raw)
			}
		})
	}
}

func TestInterruptForceConvergenceOnUnresponsiveWorker(t *testing.T) {
	c, _ := fake(t, "lifecycle-held", nil)
	c.opts.StopGraceTimeout = 100 * time.Millisecond
	ready(t, c)
	ctx := context.Background()
	accepted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.PromptAccepted(ctx, "hangs on cancel", func() { close(accepted) })
		done <- err
	}()
	<-accepted
	if err := c.Interrupt(ctx, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from forced teardown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt did not converge within grace window")
	}
	if c.Err() == nil {
		t.Fatal("expected client to be closed after forced teardown")
	}
}

func TestRepeatedInterruptForcesImmediateTeardown(t *testing.T) {
	c, _ := fake(t, "lifecycle-held", nil)
	c.opts.StopGraceTimeout = 10 * time.Second // long grace timeout
	ready(t, c)
	ctx := context.Background()
	accepted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.PromptAccepted(ctx, "hangs on cancel", func() { close(accepted) })
		done <- err
	}()
	<-accepted
	if err := c.Interrupt(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Second interrupt forces immediate teardown without waiting for 10s grace
	if err := c.Interrupt(ctx, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from forced teardown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("repeated interrupt did not force immediate teardown")
	}
	if c.Err() == nil {
		t.Fatal("expected client to be closed after forced teardown")
	}
}
