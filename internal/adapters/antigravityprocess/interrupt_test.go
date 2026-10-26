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
