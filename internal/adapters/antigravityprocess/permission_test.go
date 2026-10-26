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

func pipePermission(t *testing.T, requests <-chan *PermissionRequest) *PermissionRequest {
	t.Helper()
	select {
	case p := <-requests:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("native offer was cancelled before publication")
		return nil
	}
}

func TestPermissionPipeSelectedAndCancelled(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "cancelled"}[cancel], func(t *testing.T) {
			requests := make(chan *PermissionRequest, 1)
			mode := "permission-select"
			if cancel {
				mode = "permission-cancel"
			}
			c, dir := fakePermission(t, mode, nil, func(_ context.Context, p *PermissionRequest) error { requests <- p; return nil })
			ready(t, c)
			done := make(chan error, 1)
			go func() { _, err := c.Prompt(context.Background(), "synthetic input"); done <- err }()
			p := pipePermission(t, requests)
			if !p.Active() {
				t.Fatal("offer not active")
			}
			if err := p.Select(context.Background(), "invented", nil); err == nil {
				t.Fatal("unknown native option accepted")
			}
			denied := errors.New("synthetic revoke")
			if err := p.Select(context.Background(), "reject-exact", func() error { return denied }); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			var err error
			if cancel {
				err = p.Cancel(context.Background(), nil)
			} else {
				err = p.Select(context.Background(), "reject-exact", nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Select(context.Background(), "only-real-offer", nil); err == nil {
				t.Fatal("duplicate reply accepted")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			wire, _ := os.ReadFile(filepath.Join(dir, "wire"))
			if strings.Count(string(wire), `"id":"permission-exact"`) != 1 {
				t.Fatalf("native reply count: %s", wire)
			}
		})
	}
}

func TestPermissionPipeTimeoutInvalidatesOffer(t *testing.T) {
	requests := make(chan *PermissionRequest, 1)
	c, _ := fakePermission(t, "permission-select", nil, func(_ context.Context, p *PermissionRequest) error { requests <- p; return nil })
	ready(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Prompt(ctx, "synthetic input"); done <- err }()
	p := pipePermission(t, requests)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled prompt succeeded")
	}
	if p.Active() {
		t.Fatal("cancelled offer remained active")
	}
	if err := p.Select(context.Background(), "only-real-offer", nil); err == nil {
		t.Fatal("late reply accepted")
	}
}

func TestPermissionPipeDeadlineAndNoCallbackCancel(t *testing.T) {
	t.Run("no callback", func(t *testing.T) {
		c, dir := fake(t, "permission-cancel", nil)
		ready(t, c)
		if _, err := c.Prompt(context.Background(), "synthetic input"); err != nil {
			t.Fatal(err)
		}
		wire, _ := os.ReadFile(filepath.Join(dir, "wire"))
		if !strings.Contains(string(wire), `"outcome":"cancelled"`) {
			t.Fatal("missing cancellation")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		requests := make(chan *PermissionRequest, 1)
		c, _ := fakePermission(t, "permission-select", nil, func(_ context.Context, p *PermissionRequest) error { requests <- p; return nil })
		ready(t, c)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := c.Prompt(ctx, "synthetic input"); done <- err }()
		p := pipePermission(t, requests)
		if err := <-done; err == nil {
			t.Fatal("deadline succeeded")
		}
		if p.Active() {
			t.Fatal("expired offer remained active")
		}
	})
}

// Waiting for the person must not consume the setup/RPC timeout.
func TestPermissionWaitOutlivesCallTimeout(t *testing.T) {
	for _, choice := range []struct{ mode, option string }{
		{"permission-allow", "only-real-offer"},
		{"permission-select", "reject-exact"},
	} {
		t.Run(choice.mode, func(t *testing.T) {
			requests := make(chan *PermissionRequest, 1)
			c, dir := fakePermission(t, choice.mode, nil, func(_ context.Context, p *PermissionRequest) error { requests <- p; return nil })
			ready(t, c)
			done := make(chan error, 1)
			go func() { _, err := c.Prompt(context.Background(), "synthetic input"); done <- err }()
			p := pipePermission(t, requests)
			select {
			case err := <-done:
				t.Fatalf("prompt ended while waiting for a person: %v", err)
			case <-time.After(c.opts.CallTimeout + 100*time.Millisecond):
			}
			if !p.Active() {
				t.Fatal("offer expired while waiting for the person")
			}
			if err := p.Select(context.Background(), choice.option, nil); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			wire, err := os.ReadFile(filepath.Join(dir, "wire"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(wire), `"id":"permission-exact"`) != 1 {
				t.Fatal("permission was not answered exactly once")
			}
		})
	}
}
