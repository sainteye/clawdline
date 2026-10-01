package terminals

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

type idleTerminalHost struct {
	ports.OwnedTerminals
	offline atomic.Bool
}

func (h *idleTerminalHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	if h.offline.Load() {
		return terminal.Frame{}, terminal.Refuse(terminal.CodeUnreachable, "terminal server offline")
	}
	return terminal.Frame{Rev: "still", At: time.Now(), Cols: 80, Rows: 24, Lines: []string{"$ "}}, nil
}

func (*idleTerminalHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return make(chan struct{}), func() {}, nil
}

func TestCloudFrameHeartbeatKeepsIdleShellFreshAndStopsOffline(t *testing.T) {
	host := &idleTerminalHost{}
	svc := New(host, func(Principal) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	events := make(chan Event, 32)
	done := make(chan error, 1)
	go func() {
		done <- watch.RunWithFrameHeartbeat(ctx, 3*time.Second, func(e Event) error { events <- e; return nil })
	}()
	var first, last time.Time
	frames := 0
	deadline := time.After(34 * time.Second)
	for frames < 11 {
		select {
		case e := <-events:
			if e.Kind != EventFrame {
				continue
			}
			if e.Frame.Rev != "still" {
				t.Fatalf("idle screen changed revision: %q", e.Frame.Rev)
			}
			if !e.Frame.At.After(last) && frames > 0 {
				t.Fatal("heartbeat did not recapture the complete screen")
			}
			if frames == 0 {
				first = e.Frame.At
			}
			last = e.Frame.At
			frames++
		case <-deadline:
			t.Fatalf("idle shell yielded only %d frames in 34 seconds", frames)
		}
	}
	if last.Sub(first) < 30*time.Second {
		t.Fatalf("did not cover 30 idle seconds: %s", last.Sub(first))
	}
	host.offline.Store(true)
	offlineDeadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == EventFrame {
				t.Fatal("offline terminal emitted a fresh frame")
			}
			if e.Kind == EventState && e.Status == terminal.Unreachable {
				select {
				case e := <-events:
					if e.Kind == EventFrame {
						t.Fatal("offline terminal emitted a heartbeat")
					}
				case <-time.After(3500 * time.Millisecond):
				}
				cancel()
				<-done
				return
			}
		case <-offlineDeadline:
			t.Fatal("offline terminal did not report unreachable")
		}
	}
}
