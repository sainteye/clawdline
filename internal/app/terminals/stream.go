package terminals

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// ErrFrameDeferred means the consumer still has a prior frame in flight.
// The watch retains its last delivered revision and retries at its next read.
var ErrFrameDeferred = errors.New("terminal frame deferred")

// EventKind is one of the stream's events (plan v3 D2).
type EventKind string

const (
	EventFrame   EventKind = "frame"
	EventControl EventKind = "control"
	EventState   EventKind = "state"
	EventBeat    EventKind = "beat"
	// EventRefusal is the last event of a stream that ended because its
	// viewer lost access.
	EventRefusal EventKind = "refusal"
)

// Event is one thing a stream sends.
type Event struct {
	Kind    EventKind
	Frame   terminal.Frame
	Control Control
	// Status and ClosedBy are a state event's.
	Status       terminal.Status
	ClosedBy     *Principal
	ClosedClient string
	// Now and LastCapture are a beat's.
	Now, LastCapture time.Time
	Refusal          *Refusal
}

// viewer is one open stream.
type viewer struct {
	who    Principal
	client string
	wake   chan struct{}

	controlDirty atomic.Bool
	endMu        sync.Mutex
	ending       *Event
}

func (v *viewer) kick() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// end asks the stream to send e and stop. The first end wins.
func (v *viewer) end(e Event) {
	v.endMu.Lock()
	if v.ending == nil {
		v.ending = &e
	}
	v.endMu.Unlock()
	v.kick()
}

func (v *viewer) ended() *Event {
	v.endMu.Lock()
	defer v.endMu.Unlock()
	return v.ending
}

// Watch is a stream that was let in and has a place among the viewers. Run
// sends its events; Stop gives its place back, and must be called.
type Watch struct {
	s     *Service
	id    terminal.ID
	v     *viewer
	first terminal.Frame
	wake  <-chan struct{}
	stop  func()
	once  sync.Once
}

// Watch lets p watch id: access, a place among at most MaxViewers of this
// terminal and MaxStreams of the machine, the change signal and a first
// frame, or the refusal that says which of them failed. Nothing has been sent
// yet when it answers, so the caller can still answer with a status.
func (s *Service) Watch(ctx context.Context, p Principal, id terminal.ID, client string) (*Watch, error) {
	if err := s.allowed(p); err != nil {
		return nil, err
	}
	v := &viewer{who: p, client: client, wake: make(chan struct{}, 1)}
	s.mu.Lock()
	if len(s.viewers[id]) >= MaxViewers {
		s.mu.Unlock()
		return nil, refuse(terminal.CodeViewersFull, "this terminal already has as many viewers as it serves")
	}
	if s.streams >= MaxStreams {
		s.mu.Unlock()
		return nil, refuse(terminal.CodeViewersFull, "this machine already serves as many terminal streams as it can")
	}
	if s.viewers[id] == nil {
		s.viewers[id] = map[*viewer]struct{}{}
	}
	s.viewers[id][v] = struct{}{}
	s.streams++
	s.mu.Unlock()
	w := &Watch{s: s, id: id, v: v, stop: func() {}}

	frame, err := s.host.Frame(ctx, id)
	if err != nil {
		w.Stop()
		return nil, err
	}
	w.first = frame
	wake, stop, err := s.host.Changed(ctx, id)
	if err != nil {
		w.Stop()
		return nil, err
	}
	w.wake, w.stop = wake, stop
	return w, nil
}

// Viewer is who is watching and with which client, for describing a holder
// to them.
func (w *Watch) Viewer() (Principal, string) { return w.v.who, w.v.client }

// Stop gives the stream's place back and takes its change signal off.
func (w *Watch) Stop() {
	w.once.Do(func() {
		w.stop()
		s := w.s
		s.mu.Lock()
		defer s.mu.Unlock()
		if vs := s.viewers[w.id]; vs != nil {
			if _, ok := vs[w.v]; ok {
				delete(vs, w.v)
				s.streams--
			}
			if len(vs) == 0 {
				delete(s.viewers, w.id)
				delete(s.closing, w.id)
			}
		}
	})
}

// Run sends the stream's events until ctx ends, the terminal ends, the viewer
// loses access, or send fails. It begins with the lease, the state and the
// newest frame, and after that sends a frame only when the screen changed:
// the newest one, read no more often than CaptureGap, never a queue of them.
// Cloud callers use RunWithFrameHeartbeat to recapture an unchanged screen.
func (w *Watch) Run(ctx context.Context, send func(Event) error) error {
	return w.run(ctx, 0, send)
}

// RunWithFrameHeartbeat also sends a freshly captured complete frame when the
// screen has been still for heartbeat. Cloud viewers use this to distinguish
// an idle shell from a disconnected machine.
func (w *Watch) RunWithFrameHeartbeat(ctx context.Context, heartbeat time.Duration, send func(Event) error) error {
	return w.run(ctx, heartbeat, send)
}

func (w *Watch) run(ctx context.Context, heartbeat time.Duration, send func(Event) error) error {
	s, id := w.s, w.id
	// revoked is whether an access answer ends the stream. An unverified
	// answer (Unverified) only pauses it: nothing is sent while it lasts, and
	// the screen is offered again once access is confirmed.
	revoked := func(err error) bool { return err != nil && !Unverified(err) }
	revokedEvent := func() Event {
		return Event{Kind: EventRefusal, Refusal: refuse(terminal.CodeAccessRevoked,
			"this device may no longer see this machine's terminals")}
	}
	if err := s.allowed(w.v.who); revoked(err) {
		return send(revokedEvent())
	}
	if err := send(Event{Kind: EventControl, Control: s.Control(id)}); err != nil {
		return err
	}
	if err := send(Event{Kind: EventState, Status: terminal.Running}); err != nil {
		return err
	}
	last := w.first
	lastSentAt := last.At
	// Watch may have spent time capturing the first frame while access was
	// revoked. Do not release that frame merely because Watch was admitted.
	// owed is set while the newest frame has not been delivered (paused or
	// deferred), so the next read offers it even when the screen is still.
	owed := false
	if err := s.allowed(w.v.who); revoked(err) {
		return send(revokedEvent())
	} else if err != nil {
		owed = true
	} else if err := send(Event{Kind: EventFrame, Frame: last}); errors.Is(err, ErrFrameDeferred) {
		owed = true
	} else if err != nil {
		return err
	}
	var poll <-chan time.Time
	if w.wake == nil || heartbeat > 0 {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		poll = t.C
	}
	beat := time.NewTicker(BeatEvery)
	defer beat.Stop()
	unreachable := false

	// look reads the screen and sends it when it moved, and says whether the
	// stream is over.
	look := func() (bool, error) {
		if err := s.allowed(w.v.who); revoked(err) {
			return true, send(revokedEvent())
		} else if err != nil {
			owed = true
			return false, nil
		}
		if wait := CaptureGap - s.now().Sub(last.At); wait > 0 && !last.At.IsZero() {
			select {
			case <-ctx.Done():
				return true, nil
			case <-time.After(wait):
			}
		}
		frame, err := s.host.Frame(ctx, id)
		if err != nil {
			code, _ := terminal.CodeOf(err)
			switch {
			case ctx.Err() != nil:
				return true, nil
			case code == terminal.CodeClosed:
				return true, send(s.goneEvent(id))
			case !unreachable:
				unreachable = true
				return false, send(Event{Kind: EventState, Status: terminal.Unreachable})
			}
			return false, nil
		}
		if unreachable {
			unreachable = false
			if err := send(Event{Kind: EventState, Status: terminal.Running}); err != nil {
				return true, err
			}
		}
		if owed || frame.Rev != last.Rev || (heartbeat > 0 && frame.At.Sub(lastSentAt) >= heartbeat) {
			if err := s.allowed(w.v.who); revoked(err) {
				return true, send(revokedEvent())
			} else if err != nil {
				owed = true
				return false, nil
			}
			if err := send(Event{Kind: EventFrame, Frame: frame}); errors.Is(err, ErrFrameDeferred) {
				return false, nil
			} else if err != nil {
				return true, err
			}
			owed = false
			last = frame
			lastSentAt = frame.At
		} else {
			last.At = frame.At
		}
		if frame.Dead {
			return true, send(Event{Kind: EventState, Status: terminal.Exited})
		}
		return false, nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.v.wake:
			if e := w.v.ended(); e != nil {
				return send(*e)
			}
			if w.v.controlDirty.Swap(false) {
				if err := send(Event{Kind: EventControl, Control: s.Control(id)}); err != nil {
					return err
				}
			}
		case <-w.wake:
			if done, err := look(); done || err != nil {
				return err
			}
		case <-poll:
			if done, err := look(); done || err != nil {
				return err
			}
		case <-beat.C:
			// Asked again before every beat as before every frame: a grant
			// taken away by hand, which no writer announced, still ends the
			// stream within one beat. The screen is read too, so a change the
			// signal missed is at most one beat late.
			if done, err := look(); done || err != nil {
				return err
			}
			// A paused stream does not say it is still let in.
			if s.allowed(w.v.who) != nil {
				continue
			}
			if err := send(Event{Kind: EventBeat, Now: s.now(), LastCapture: last.At}); err != nil {
				return err
			}
		}
	}
}

// goneEvent is what a viewer of a terminal that is no longer there is told:
// closed, naming who, when somebody closed it here; exited otherwise — its
// shell ended on its own and took the terminal with it.
func (s *Service) goneEvent(id terminal.ID) Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if by, ok := s.closing[id]; ok {
		who := by.who
		return Event{Kind: EventState, Status: terminal.Closed, ClosedBy: &who, ClosedClient: by.client}
	}
	return Event{Kind: EventState, Status: terminal.Exited}
}
