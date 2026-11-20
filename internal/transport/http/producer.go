package http

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

// The session list and the task list are built once per tick, by one producer,
// for everybody who reads them.
//
// Until this file each `/v1/events` connection kept its own two-second clock
// and rebuilt the whole sessions payload and the first task page on every
// tick, and the Cloud publisher built the sessions payload a third way through
// an in-process `GET /v1/sessions` every five seconds. Building a sessions
// payload is about eight SQLite reads and a write per row (observeExecutions),
// so a console open in three tabs with the Cloud line up was four builds every
// tick of the same machine at the same moment. Measured on 2026-10-09 the
// scan generation advanced by two to three per two-second frame on one stream.
//
// Each reader still decides for itself what to send: a stream compares the
// product against what it last told its own client, exactly as before, so the
// frames a client sees are the frames it saw. Only the builds are shared.

// productBuildSeconds bounds one shared build. It is the longer of the two
// deadlines the readers it replaces had: the route's eight seconds (the
// stream's was five).
const productBuildSeconds = 8

// latest is one value built on demand and shared while it is fresh.
//
// A reader that finds no value younger than the age it can accept joins the
// build in flight, or starts one; it never starts a second. A failed build is
// not remembered, so the next reader tries again.
type latest[T any] struct {
	build func(context.Context) (T, error)

	mu     sync.Mutex
	at     time.Time
	value  T
	ok     bool
	flight chan struct{}
	err    error
	// flightStarted is when the build in flight began, and valueStarted when
	// the build behind value did: what `since` compares a moment against.
	flightStarted time.Time
	valueStarted  time.Time
	// builds counts the builds that ran, for the tests that prove sharing.
	builds atomic.Int64
}

// get answers a value no older than maxAge, building one when there is none.
func (l *latest[T]) get(maxAge time.Duration) (T, error) {
	l.mu.Lock()
	if l.ok && time.Since(l.at) < maxAge {
		value := l.value
		l.mu.Unlock()
		return value, nil
	}
	l.mu.Unlock()
	return l.refresh()
}

// peek is the newest value there is, however old, without building.
func (l *latest[T]) peek() (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value, l.ok
}

// refresh builds now, or waits for the build that is already running.
func (l *latest[T]) refresh() (T, error) {
	return l.refreshFrom(context.Background(), false)
}

// refreshFrom is refresh with base's values, never its cancellation, on the
// build's context: how `since` tells the build which scan it may read. `own`
// waits out a build in flight and then builds, instead of taking its answer.
func (l *latest[T]) refreshFrom(base context.Context, own bool) (T, error) {
	l.mu.Lock()
	for own && l.flight != nil {
		flight := l.flight
		l.mu.Unlock()
		<-flight
		l.mu.Lock()
	}
	if flight := l.flight; flight != nil {
		l.mu.Unlock()
		<-flight
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.value, l.err
	}
	flight := make(chan struct{})
	l.flight = flight
	started := time.Now()
	l.flightStarted = started
	l.mu.Unlock()

	// The build has a clock of its own and not a reader's: a tab that closes
	// while the build runs must not cancel it for every other reader waiting
	// on it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(base), productBuildSeconds*time.Second)
	value, err := l.build(ctx)
	cancel()
	l.builds.Add(1)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
	if err == nil {
		l.value, l.at, l.ok, l.valueStarted = value, time.Now(), true, started
	} else if l.ok {
		// A failed build leaves the last good value where peek finds it, and
		// get will try again rather than serve it as fresh.
		l.at = time.Time{}
	}
	l.flight = nil
	close(flight)
	if err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

// since answers a value from a build of its own, begun at or after `t`, whose
// readings come from scans begun at or after `t` too (app.WithReadingSince). A
// build in flight is waited out, never joined: it was not asked for `t`, and
// its readings may be the held scan from before it.
//
// Measured on 2026-10-12: the Cloud publisher's looks 246 ms and 1242 ms after a
// press both asked for a build now, both joined a build that had begun before
// the press, and the pressed Session's row went out six seconds later on the
// next ordinary pass. With the build taken fresh the looks still sent another
// Session's row: the build read the inventory's held scan, which was older.
func (l *latest[T]) since(t time.Time) (T, error) {
	return l.refreshFrom(app.WithReadingSince(context.Background(), t), true)
}

// listProducer is the one clock behind every session-list and task-list reader.
//
// It runs only while somebody subscribes: with no stream open and no Cloud
// line, a `GET /v1/sessions` builds on demand and the machine is not scanned
// for nobody.
type listProducer struct {
	every    time.Duration
	sessions latest[sessionsSnapshotWire]
	tasks    latest[contract.TaskList]

	mu          sync.Mutex
	subs        map[int]chan struct{}
	next        int
	cancel      context.CancelFunc
	launchCheck *time.Timer
}

// lists is this server's producer, made on first use so that a Server built
// as a literal in a test has one too.
func (s *Server) lists() *listProducer {
	s.listsOnce.Do(func() {
		p := &listProducer{every: streamTick(), subs: map[int]chan struct{}{}}
		p.sessions.build = func(ctx context.Context) (sessionsSnapshotWire, error) {
			return s.sessionsPayload(ctx), nil
		}
		p.tasks.build = func(ctx context.Context) (contract.TaskList, error) {
			return s.tasksPayload(ctx, 0, 50)
		}
		s.listsProducer = p
	})
	return s.listsProducer
}

// subscribe asks to be woken after every tick's build. The channel holds one
// wake: a reader that is still writing its last frame when the next tick lands
// reads the newest product once, not every product it missed.
func (p *listProducer) subscribe() (int, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	id := p.next
	wake := make(chan struct{}, 1)
	p.subs[id] = wake
	if p.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		go p.run(ctx)
	}
	return id, wake
}

func (p *listProducer) unsubscribe(id int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.subs, id)
	if len(p.subs) == 0 && p.cancel != nil {
		p.cancel()
		p.cancel = nil
		if p.launchCheck != nil {
			p.launchCheck.Stop()
			p.launchCheck = nil
		}
	}
}

// startConfirmed advances one discovery pass for watching consoles. The
// launch receipt proves that the terminal was opened, but the assistant
// needs time to appear in process discovery. A pending pass also covers
// intervening launches. Late assistants still arrive on the ordinary tick.
func (p *listProducer) startConfirmed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.subs) == 0 || p.launchCheck != nil {
		return
	}
	var check *time.Timer
	check = time.AfterFunc(p.every/2, func() {
		p.mu.Lock()
		if p.launchCheck != check {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		// The last shared snapshot may have read the machine before the
		// launch. Reuse the single build lane with a fresh inventory fence.
		_, err := p.sessions.since(time.Now())
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.launchCheck == check {
			p.launchCheck = nil
			if err == nil {
				p.wakeSubscribersLocked()
			}
		}
	})
	p.launchCheck = check
}

func (p *listProducer) wakeSubscribersLocked() {
	for _, wake := range p.subs {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (p *listProducer) run(ctx context.Context) {
	ticker := time.NewTicker(p.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A failed task build is the stream's to skip, as it always was;
			// it does not hold back the session list.
			_, _ = p.sessions.refresh()
			_, _ = p.tasks.refresh()
			p.mu.Lock()
			p.wakeSubscribersLocked()
			p.mu.Unlock()
		}
	}
}

// snapshot is the session list no older than one tick.
func (p *listProducer) snapshot() sessionsSnapshotWire {
	value, _ := p.sessions.get(p.every)
	return value
}

// taskList is the first task page no older than one tick.
func (p *listProducer) taskList() (contract.TaskList, error) {
	return p.tasks.get(p.every)
}

// CloudSessionsFresh is CloudSessions built now: the publisher's look right
// after a press from a page (cloud.Publisher.Fresh), when the shared product
// may still be the screen from before the press.
func (s *Server) CloudSessionsFresh(context.Context) ([]byte, bool) {
	value, err := s.lists().sessions.since(time.Now())
	if err != nil {
		return nil, false
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return body, true
}

// CloudSessionLook is the publisher's way to make one Session's next screen
// reading a capture (cloud.Publisher.Look): the same window a press opens
// (app.HeldScreens.Pressed), opened by a page asking for that row again.
func (s *Server) CloudSessionLook(id string) {
	s.inventory.Held.Pressed(id)
}

// CloudSessions is the session list as the Cloud publisher reads it: the
// product every stream reads, encoded as `GET /v1/sessions` answers it, so the
// publisher no longer builds one of its own through the route.
func (s *Server) CloudSessions(context.Context) ([]byte, bool) {
	body, err := json.Marshal(s.lists().snapshot())
	if err != nil {
		return nil, false
	}
	return body, true
}
