//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"errors"
	"reflect"
	"syscall"
	"testing"
)

func TestPromptAcceptancePrecedesCompletionWithoutReplay(t *testing.T) {
	c, dir := fake(t, "stall-read", nil)
	ready(t, c)
	if err := syscall.Kill(c.PID(), 0); err != nil {
		t.Fatal("PID does not name the owned live peer", err)
	}
	accepted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.PromptAccepted(context.Background(), "one turn", func() { close(accepted) })
		done <- err
	}()
	select {
	case <-accepted:
	case err := <-done:
		t.Fatalf("request ended before pipe acceptance: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("acceptance was mistaken for completion: %v", err)
	default:
	}
	if err := c.Cancel(); err != nil {
		t.Fatal(err)
	}
	var typed *Error
	if err := <-done; !errors.As(err, &typed) || typed.Delivery != Uncertain {
		t.Fatalf("cancelled accepted write outcome=%v", err)
	}
	if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/new", "session/prompt"}) {
		t.Fatalf("accepted prompt was replayed: %v", got)
	}
}

func TestPromptAcceptanceDoesNotSignalForLocalRefusal(t *testing.T) {
	c, dir := fake(t, "normal", nil)
	ready(t, c)
	settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
	accepted := false
	_, err := c.PromptAccepted(context.Background(), "must not be sent", func() { accepted = true })
	requireError(t, err, "personal_settings_required", NotWritten)
	if accepted {
		t.Fatal("local refusal was acknowledged as a pipe write")
	}
	if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/new"}) {
		t.Fatalf("refused request reached the peer: %v", got)
	}
}
