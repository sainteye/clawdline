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

type changingTerminalHost struct {
	idleTerminalHost
	revision atomic.Int32
	wake     chan struct{}
}

func (h *changingTerminalHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	rev := "first"
	if h.revision.Load() > 0 {
		rev = "second"
	}
	return terminal.Frame{Rev: rev, At: time.Now(), Cols: 80, Rows: 24, Lines: []string{rev}}, nil
}

func (h *changingTerminalHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return h.wake, func() {}, nil
}

func TestDeferredCloudFrameIsRetriedBeforeHeartbeat(t *testing.T) {
	host := &changingTerminalHost{wake: make(chan struct{}, 1)}
	svc := New(host, func(Principal) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	frames := make(chan string, 4)
	done := make(chan error, 1)
	var deferred atomic.Bool
	go func() {
		done <- watch.RunWithFrameHeartbeat(ctx, 3*time.Second, func(e Event) error {
			if e.Kind != EventFrame {
				return nil
			}
			if e.Frame.Rev == "second" && !deferred.Swap(true) {
				return ErrFrameDeferred
			}
			frames <- e.Frame.Rev
			return nil
		})
	}()
	select {
	case <-frames:
	case <-time.After(time.Second):
		t.Fatal("first frame missing")
	}
	host.revision.Store(1)
	host.wake <- struct{}{}
	select {
	case rev := <-frames:
		if rev != "second" {
			t.Fatalf("unexpected frame: %s", rev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deferred frame waited for heartbeat")
	}
	cancel()
	<-done
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

func TestUnverifiedAccessPausesAStreamInsteadOfRevokingIt(t *testing.T) {
	host := &changingTerminalHost{wake: make(chan struct{}, 1)}
	var unverified atomic.Bool
	svc := New(host, func(Principal) error {
		if unverified.Load() {
			return terminal.Refuse(terminal.CodeBusy, "the device roster could not be read")
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	unverified.Store(true)
	events := make(chan Event, 16)
	done := make(chan error, 1)
	go func() { done <- watch.Run(ctx, func(e Event) error { events <- e; return nil }) }()
	svc.Revalidate()
	host.revision.Store(1)
	host.wake <- struct{}{}
	paused := time.After(700 * time.Millisecond)
waiting:
	for {
		select {
		case e := <-events:
			if e.Kind == EventRefusal || e.Kind == EventFrame {
				t.Fatalf("an unverified viewer got a %s event: %+v", e.Kind, e)
			}
		case err := <-done:
			t.Fatalf("an unverified viewer's stream ended: %v", err)
		case <-paused:
			break waiting
		}
	}
	unverified.Store(false)
	host.wake <- struct{}{}
	for {
		select {
		case e := <-events:
			if e.Kind == EventRefusal {
				t.Fatalf("a verified viewer was refused: %+v", e)
			}
			if e.Kind == EventFrame {
				if e.Frame.Rev != "second" {
					t.Fatalf("resumed with %q, want the newest screen", e.Frame.Rev)
				}
				cancel()
				<-done
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the stream did not resume once access was verified")
		}
	}
}
