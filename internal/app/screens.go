package app

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// What a live screen costs, as arithmetic rather than as opinion. Three shapes
// were measured end to end before any of this existed (tmux 3.6a, Claude Code
// on an M4 Pro):
//
//	A  sample capture-pane faster      half the interval + 4.15 ms, and it asks
//	                                   even when nothing moved
//	B  pipe-pane + a VT emulator       0.1 ms, one 432 KB `cat` per pane, and a
//	                                   few hundred lines that must follow an
//	                                   assistant's redraws
//	C  pipe-pane as the signal,        4.3 ms, one `cat`, zero idle captures
//	   capture-pane as the content
//
// B's emulator reproduced three real streams at full fidelity, so that was not
// the hard half. The hard half is that a reader joining mid-stream is wrong for
// half the rows and, at some join points, never converges: an assistant
// repaints only what changed, so a line drawn before you arrived is blank for
// you for ever. Whatever reaches a phone therefore has to be a screen already
// computed on this machine — and once that is true, B and C emit the same thing
// and C does not need the emulator.
const (
	// ScreenWindow is how long this waits for a burst to settle before it
	// captures again.
	//
	// The signal fires about 9.3 times a second while a session is working and
	// one capture is 4.15 ms. Without a window that is 39 ms of tmux per second
	// per watched pane, and a fifth of those captures return bytes identical to
	// the last. With 150 ms the rate is bounded by one capture per window while
	// the *first* change after a quiet moment is still immediate, because the
	// window is measured from the last capture and not from the signal. That
	// asymmetry is the point: a keystroke echoes at once and a scrolling build
	// arrives at six frames a second.
	ScreenWindow = 150 * time.Millisecond

	// ScreenOnDemandFloor is the same idea where there is no signal to
	// coalesce. iTerm2 has no `pipe-pane`, so its screens can only be sampled,
	// and one round of its capture is about 0.16 s — some forty times a tmux
	// capture. One second between captures holds that at roughly a sixth of a
	// core no matter how fast a client asks.
	ScreenOnDemandFloor = time.Second

	// ScreenLease is how long a reading keeps the signal attached without being
	// asked again.
	//
	// **Demand is observed rather than declared, so this is what "somebody is
	// still looking" means.** There is no subscribe route to forget to call and
	// no unsubscribe route to lose in a tunnel: a watcher that stops asking
	// stops being a watcher, and the pipe comes off. Thirty seconds because the
	// page renews every fifteen — twice per lease, so one lost request does not
	// drop the screen.
	ScreenLease = 30 * time.Second

	// ScreenSweep is how often expired leases are swept up. Only a bound on how
	// long a pipe outlives its watcher; the lease itself is compared against
	// the clock, never against this.
	ScreenSweep = 5 * time.Second

	// ScreenLines is the ceiling on how much screen is asked for. On an
	// assistant pane it is inert — the alternate screen has no history to give
	// — and on an ordinary shell pane it is two hundred lines.
	ScreenLines = 200

	// ScreenUnreadable is the revision of a screen that could not be read. A
	// constant rather than a hash, so a pane that stays unreadable stops
	// producing events after the first one: the client is told once that the
	// screen went away, not once every window.
	ScreenUnreadable = "unreadable"
)

// ScreenChannel is how a watcher finds out that a screen changed, which is a
// fact about the backend and not about the watcher.
//
// **Naming it is the whole point.** A view that shows a 4.3 ms live screen on
// tmux and a second-old sample on iTerm2, with the same chrome and no word
// about which, repeats a defect this repository already had. So the channel is
// in every payload and the interface prints it.
type ScreenChannel string

const (
	// ScreenSignalled is tmux: `pipe-pane` says the pane moved and this
	// captures because of it.
	ScreenSignalled ScreenChannel = "signalled"
	// ScreenOnDemand is everything else: nothing says the screen moved, so it
	// is read when somebody asks and no faster than ScreenOnDemandFloor.
	ScreenOnDemand ScreenChannel = "on-demand"
)

// ScreenRevision is what a client compares to decide whether it already has
// this screen.
//
// FNV-1a over UTF-8, which is enough for "did these bytes change" and is
// deliberately not a promise about anything else. The fifth of samples that
// come back byte-identical are dropped against this before anybody is told a
// screen changed.
func ScreenRevision(text string) string {
	hash := uint64(0xcbf2_9ce4_8422_2325)
	for i := 0; i < len(text); i++ {
		hash ^= uint64(text[i])
		hash *= 0x0000_0100_0000_01b3
	}
	return strconv.FormatUint(hash, 16)
}

// ScreenReading is one session's answer, the same shape whether it came from a
// signal or from being asked. Text is nil until the first capture completes.
type ScreenReading struct {
	SessionID     string
	Backend       session.Backend
	Channel       ScreenChannel
	Text          *string
	Revision      string
	At            time.Time
	Readable      bool
	WatchingUntil time.Time
	Captures      int
	Signals       int
}

// Pending is "nothing has been captured for this session yet".
func (r ScreenReading) Pending() bool { return r.Text == nil && !r.Readable && r.At.IsZero() }

// ScreenWatchRow is one row of what this daemon is watching, for /v1/screens.
type ScreenWatchRow struct {
	ID        string
	Backend   session.Backend
	Channel   ScreenChannel
	Watching  bool
	ExpiresIn int
	Signalled bool
	Captures  int
	Signals   int
	Readable  bool
}

// ScreenInventoryReading is the whole of what this feature has done to the
// machine, including the panes the backend says are piped that this daemon
// cannot account for.
type ScreenInventoryReading struct {
	Screens      []ScreenWatchRow
	Attached     []string
	Piped        []string
	Unattributed []string
}

// screenCoalescer is the window, as arithmetic rather than as a timer.
//
// Separated from everything that owns a clock so the thing deciding how often
// this daemon touches somebody's terminal can be read on its own. The rule is
// one line: **the window is measured from the last capture, not from the last
// signal**, so a burst is bounded at one capture per window while an isolated
// change is captured immediately.
type screenCoalescer struct {
	window    time.Duration
	last      map[string]time.Time
	scheduled map[string]bool
}

func newScreenCoalescer(window time.Duration) screenCoalescer {
	return screenCoalescer{window: window, last: map[string]time.Time{}, scheduled: map[string]bool{}}
}

// signal answers what to do about a change signal arriving at now: capture
// immediately, capture at the returned instant, or nothing because a capture is
// already on its way.
func (c *screenCoalescer) signal(id string, now time.Time) (due time.Time, capture bool, wait bool) {
	if c.scheduled[id] {
		return time.Time{}, false, false
	}
	last, seen := c.last[id]
	if !seen {
		return time.Time{}, true, false
	}
	at := last.Add(c.window)
	if !now.Before(at) {
		return time.Time{}, true, false
	}
	c.scheduled[id] = true
	return at, false, true
}

func (c *screenCoalescer) captured(id string, now time.Time) {
	c.last[id] = now
	delete(c.scheduled, id)
}

func (c *screenCoalescer) isScheduled(id string) bool { return c.scheduled[id] }

func (c *screenCoalescer) forget(id string) {
	delete(c.last, id)
	delete(c.scheduled, id)
}

type screenWatch struct {
	session   session.Session
	until     time.Time
	text      *string
	revision  string
	at        time.Time
	readable  bool
	capturing bool
	captures  int
	signals   int
}

// Screens is demand-driven live screens: who is watching what, what their last
// screen was, and the one place that decides this daemon may touch a terminal
// because of it.
//
// **Nobody watching is the whole design.** No lease means no pipe, which means
// no signal, which means no capture — an idle session measured 0 bytes in 45
// seconds, so a watched session sitting at its prompt costs nothing either. The
// lease is refreshed by reading rather than by a subscribe route, so there is no
// state a disconnected phone can leave behind.
//
// **Nothing here waits on a terminal on a request's goroutine.** A read answers
// out of the held state and posts its demand behind the answer, so a wedged
// backend costs a stale screen and never a stalled connection. Every subprocess
// — attaching a pipe, taking one off, a capture — happens on a goroutine of its
// own with the lock released.
type Screens struct {
	hosts   []ports.TerminalHost
	signal  ports.PaneSignal
	changed func(id, revision string)

	mu        sync.Mutex
	watches   map[string]*screenWatch
	coalescer screenCoalescer
	sweeper   *time.Ticker
	stopped   bool
	done      chan struct{}
}

// NewScreens builds the one owner of this daemon's pipes.
//
// `signal` may be nil, and that is a supported shape rather than a degraded
// one: a platform with no FIFO cannot be told when a pane moved, and the screens
// it publishes say `on-demand`, which is what the panel then draws.
func NewScreens(hosts []ports.TerminalHost, signal ports.PaneSignal, changed func(id, revision string)) *Screens {
	s := &Screens{
		hosts:     hosts,
		signal:    signal,
		changed:   changed,
		watches:   map[string]*screenWatch{},
		coalescer: newScreenCoalescer(ScreenWindow),
		done:      make(chan struct{}),
	}
	if signal != nil {
		signal.OnMoved(s.paneMoved)
	}
	return s
}

// Channel is which channel this backend can offer here, and therefore what the
// interface may claim.
func (s *Screens) Channel(backend session.Backend) ScreenChannel {
	if backend == session.BackendTmux && s.signal != nil {
		return ScreenSignalled
	}
	return ScreenOnDemand
}

// Read is the current screen for a session, and the demand that reading it
// declares.
//
// **Reading is the subscription.** There is no route that attaches a pipe and
// none that takes one off: a read renews a lease, and a lease nobody renews
// expires and takes the pipe with it. That is the only shape which also covers
// the phone that went into a tunnel, and it means no client can leave machine
// state behind by crashing.
func (s *Screens) Read(item session.Session, now time.Time) ScreenReading {
	until := now.Add(ScreenLease)
	go s.demand(item, until, now)
	return s.snapshot(item, until)
}

func (s *Screens) snapshot(item session.Session, until time.Time) ScreenReading {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ScreenReading{
		SessionID:     item.ID,
		Backend:       item.Backend,
		Channel:       s.Channel(item.Backend),
		Revision:      ScreenUnreadable,
		WatchingUntil: until,
	}
	if held := s.watches[item.ID]; held != nil {
		out.Text = held.text
		out.Revision = held.revision
		out.At = held.at
		out.Readable = held.readable
		out.Captures = held.captures
		out.Signals = held.signals
	}
	return out
}

// demand records the lease, attaches the signal, and starts the first capture
// if reading is what has to cause one.
func (s *Screens) demand(item session.Session, until, now time.Time) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	watch := s.watches[item.ID]
	if watch == nil {
		watch = &screenWatch{revision: ScreenUnreadable}
		s.watches[item.ID] = watch
	}
	watch.session = item
	watch.until = until
	wantSignal := item.Backend == session.BackendTmux && s.signal != nil
	capture := s.shouldCaptureOnRead(watch, now)
	if capture {
		s.beginCaptureLocked(item.ID, now)
	}
	s.startSweepingLocked()
	s.mu.Unlock()

	// Outside the lock: attaching is a subprocess, and nothing that can start
	// one may hold the lock a request reads under.
	if wantSignal {
		s.signal.Attach(item.ID)
	}
}

// shouldCaptureOnRead is whether reading should itself start a capture.
//
// On a signalled backend this is only ever the first read: after that the
// signal is what causes a capture, and asking again while nothing has moved is
// the whole waste the sampling shape pays and this one does not. Where there is
// no signal a read is the only thing that can cause one, bounded by the floor.
func (s *Screens) shouldCaptureOnRead(watch *screenWatch, now time.Time) bool {
	if watch.capturing {
		return false
	}
	if s.Channel(watch.session.Backend) == ScreenSignalled {
		return watch.at.IsZero() && !s.coalescer.isScheduled(watch.session.ID)
	}
	if watch.at.IsZero() {
		return true
	}
	return now.Sub(watch.at) >= ScreenOnDemandFloor
}

// paneMoved is a pane writing something. It arrives on the signal's own
// goroutine.
func (s *Screens) paneMoved(paneID string) {
	now := time.Now()
	s.mu.Lock()
	watch := s.watches[paneID]
	if watch == nil || s.stopped {
		s.mu.Unlock()
		return
	}
	watch.signals++
	due, capture, waiting := s.coalescer.signal(paneID, now)
	if capture {
		s.beginCaptureLocked(paneID, now)
	}
	s.mu.Unlock()
	if waiting {
		time.AfterFunc(time.Until(due), func() { s.flush(paneID) })
	}
}

// flush is the trailing edge of the window: whatever the pane has done since
// the last capture, in one capture rather than one per redraw.
func (s *Screens) flush(paneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.watches[paneID] == nil || !s.coalescer.isScheduled(paneID) {
		return
	}
	s.beginCaptureLocked(paneID, time.Now())
}

// beginCaptureLocked starts one capture. The subprocess runs on its own
// goroutine with the lock released; only the bookkeeping is done here.
func (s *Screens) beginCaptureLocked(id string, now time.Time) {
	watch := s.watches[id]
	if watch == nil || watch.capturing {
		return
	}
	watch.capturing = true
	watch.captures++
	s.coalescer.captured(id, now)
	item := watch.session
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		text, ok := s.capture(ctx, item)
		cancel()
		if ok {
			s.finish(id, &text)
			return
		}
		s.finish(id, nil)
	}()
}

// capture is the one reader. A second one here would be a second answer to the
// same question.
func (s *Screens) capture(ctx context.Context, item session.Session) (string, bool) {
	for _, h := range s.hosts {
		if h.Name() != string(item.Backend) {
			continue
		}
		return h.Screen(ctx, item, ScreenLines)
	}
	return "", false
}

// finish takes one capture's answer.
//
// **This is where the byte-identical fifth goes.** Ten samples a second of a
// working session produced bytes identical to the previous sample about a fifth
// of the time, and a client told about those would have fetched the same screen
// again for nothing. A capture whose text and readability both match what is
// already held bumps no revision and sends no event.
func (s *Screens) finish(id string, text *string) {
	s.mu.Lock()
	watch := s.watches[id]
	if watch == nil {
		s.mu.Unlock()
		return
	}
	watch.capturing = false
	readable := text != nil
	unchanged := readable == watch.readable &&
		((text == nil && watch.text == nil) || (text != nil && watch.text != nil && *text == *watch.text))
	if unchanged {
		s.mu.Unlock()
		return
	}
	watch.readable = readable
	watch.text = text
	watch.at = time.Now()
	if text != nil {
		watch.revision = ScreenRevision(*text)
	} else {
		watch.revision = ScreenUnreadable
	}
	revision := watch.revision
	changed := s.changed
	s.mu.Unlock()
	if changed != nil {
		changed(id, revision)
	}
}

func (s *Screens) startSweepingLocked() {
	if s.sweeper != nil || s.stopped {
		return
	}
	ticker := time.NewTicker(ScreenSweep)
	s.sweeper = ticker
	go func() {
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.Sweep(time.Now())
			}
		}
	}()
}

// Sweep drops leases that have run out and takes their pipes with them.
// Exposed so a caller can move the clock instead of waiting for one.
func (s *Screens) Sweep(now time.Time) {
	s.mu.Lock()
	var expired []string
	for id, watch := range s.watches {
		if !watch.until.After(now) {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(s.watches, id)
		s.coalescer.forget(id)
	}
	s.mu.Unlock()
	sort.Strings(expired)
	for _, id := range expired {
		if s.signal != nil {
			s.signal.Detach(id)
		}
	}
}

// Inventory is what this daemon has attached, what the backend says about it,
// and the difference between the two.
//
// **`#{pane_pipe}` is a boolean, so tmux cannot say whose pipe it is.** A pane
// that is piped and not on this daemon's list is therefore `unattributed`
// rather than a leak — it may be somebody's own `pipe-pane`, and this daemon
// does not take other people's pipes off.
func (s *Screens) Inventory(ctx context.Context) ScreenInventoryReading {
	now := time.Now()
	s.mu.Lock()
	ids := make([]string, 0, len(s.watches))
	for id := range s.watches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := ScreenInventoryReading{Screens: make([]ScreenWatchRow, 0, len(ids))}
	ours := map[string]bool{}
	for _, id := range ids {
		watch := s.watches[id]
		attached := s.signal != nil && s.signal.IsAttached(id)
		if attached {
			ours[id] = true
		}
		expires := int(watch.until.Sub(now).Round(time.Second) / time.Second)
		if expires < 0 {
			expires = 0
		}
		out.Screens = append(out.Screens, ScreenWatchRow{
			ID:        id,
			Backend:   watch.session.Backend,
			Channel:   s.Channel(watch.session.Backend),
			Watching:  watch.until.After(now),
			ExpiresIn: expires,
			Signalled: attached,
			Captures:  watch.captures,
			Signals:   watch.signals,
			Readable:  watch.readable,
		})
	}
	s.mu.Unlock()

	out.Attached = make([]string, 0, len(ours))
	for id := range ours {
		out.Attached = append(out.Attached, id)
	}
	sort.Strings(out.Attached)
	out.Piped = []string{}
	out.Unattributed = []string{}
	for _, h := range s.hosts {
		pipes, ok := h.(ports.PipeHost)
		if !ok {
			continue
		}
		for pane, piped := range pipes.PipedPanes(ctx) {
			if !piped {
				continue
			}
			out.Piped = append(out.Piped, pane)
			if !ours[pane] {
				out.Unattributed = append(out.Unattributed, pane)
			}
		}
	}
	sort.Strings(out.Piped)
	sort.Strings(out.Unattributed)
	return out
}

// Reclaim takes back the panes piped by a previous run of this daemon.
//
// Called once at start. The FIFO left in this daemon's own directory is the
// record — no second file to fall out of step — and the backend is asked before
// anything is undone, so a pane that has already cleaned itself up costs one
// forgotten file rather than a `pipe-pane` aimed at somebody else's pane id.
func (s *Screens) Reclaim(ctx context.Context) []string {
	if s.signal == nil {
		return nil
	}
	abandoned := s.signal.Abandoned()
	if len(abandoned) == 0 {
		return nil
	}
	piped := map[string]bool{}
	for _, h := range s.hosts {
		if pipes, ok := h.(ports.PipeHost); ok {
			for pane, on := range pipes.PipedPanes(ctx) {
				piped[pane] = on
			}
		}
	}
	var taken []string
	for _, pane := range abandoned {
		if piped[pane] {
			for _, h := range s.hosts {
				if pipes, ok := h.(ports.PipeHost); ok {
					pipes.Unpipe(ctx, pane)
				}
			}
			taken = append(taken, pane)
		}
		s.signal.Forget(pane)
	}
	return taken
}

// Stop takes everything off, now. The daemon closing, or a test finishing.
// Synchronous on purpose: this is the one path where the pipes must be gone
// before the caller carries on.
func (s *Screens) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.sweeper != nil {
		s.sweeper.Stop()
		s.sweeper = nil
	}
	close(s.done)
	s.watches = map[string]*screenWatch{}
	s.mu.Unlock()
	if s.signal != nil {
		s.signal.DetachAll()
	}
}
