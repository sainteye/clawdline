// Package terminals decides who may type into an ordinary shell this daemon
// holds, and carries what the shell draws to the people watching it (plan v3
// §5, D3 and D4).
//
// **One controller at a time.** A terminal has one control lease: a holder (a
// device and the browser tab it names as its client), an epoch, and a time it
// lapses unless renewed. Anybody let in may watch; only the holder types,
// pastes, resizes or closes. A second person asking for control while it is
// held is refused with the holder's name, and may take it over, which ends the
// first lease: the old holder is told on its stream and its next input is
// refused lease_superseded, never typed. Leases live in memory: a daemon that
// restarts holds none, and the shells are still there.
//
// **Every input is decided and typed inside the terminal's lane.** Two copies
// of the same numbered input arriving together are decided one after the
// other, so the second finds the first applied and types nothing. The applied
// number moves only after tmux answered that it typed; a tmux that did not
// answer leaves the lease's input state unknown, and nothing is typed again
// under it until somebody acquires a new epoch and looks at the screen first.
// The lanes are this package's own, never the Agent lanes' machine-wide
// sixteen: a person's terminal full of keystrokes cannot hold up a briefing.
//
// **Access is asked again, not remembered.** The gate judges each request
// once. A lease and a stream outlive the request that began them, so the
// service asks Access again whenever the daemon says devices or grants
// changed (Revalidate), and a stream asks again before every frame and every
// beat. Losing access voids the lease and ends the stream with
// terminal_access_revoked.
package terminals

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// The bounds and clocks of this package (docs/limits.md N60). Each bound is a
// registered row in internal/domain/capacity.
const (
	// LaneLimit is how many inputs, pastes and waiters for them the whole
	// machine's terminals admit at once (`terminal.lane`): twice the eight
	// terminals one machine may hold, so every terminal can have one input
	// typing and one waiting. The next is refused terminal_busy, and the
	// sender retries the same number.
	LaneLimit = 2 * terminal.MaxTerminals
	// MaxViewers is how many streams one terminal serves at once
	// (`terminal.viewers`).
	MaxViewers = 8
	// MaxStreams is how many terminal streams the machine serves at once
	// (`terminal.streams`).
	MaxStreams = 16
	// MaxLeaseSeconds is how long a lease lasts unrenewed
	// (`terminal.lease_seconds`). A holder renews every RenewEvery, so a
	// lease outlives two missed renewals.
	MaxLeaseSeconds = 30
	LeaseTTL        = MaxLeaseSeconds * time.Second
	RenewEvery      = 10 * time.Second
	// BeatEvery is the stream's `beat`: the viewer is still let in and the
	// stream is alive.
	BeatEvery = 5 * time.Second
	// CaptureGap is the least time between two screen reads for one stream. A
	// burst of output is read at most this often and the frames in between
	// are skipped, never queued.
	CaptureGap = 33 * time.Millisecond
	// pollEvery is how often a stream reads the screen on a platform whose
	// adapter cannot signal a change.
	pollEvery = 250 * time.Millisecond
	// inputTimeout is how long one keystroke batch or paste may take before
	// its outcome is unknown.
	inputTimeout = 5 * time.Second
)

// Principal is who is asking: a device the gate let in.
type Principal struct {
	// Device is the device id; this machine's own token has one too.
	Device string
	// Name is the device's name, which a holder is described by.
	Name string
	// Local is this machine's own token.
	Local bool
	// Cloud is set only by the signed-envelope ingress. HTTP requests cannot
	// choose it; the service asks the Cloud link to revalidate this principal.
	Cloud bool
}

// Access is whether p may still see and operate terminals: nil is yes, and an
// error is why not.
type Access func(p Principal) error

// Control is a terminal's lease as it stands.
type Control struct {
	Held bool
	// Holder and Client are the lease's holder, when Held.
	Holder Principal
	Client string
	Epoch  uint64
	// Expires is when it lapses unless renewed.
	Expires time.Time
	// Applied is the highest input number typed under Epoch.
	Applied uint64
	// Unknown means a host call may have typed bytes without acknowledging
	// them. A reconnect must acquire a new epoch instead of continuing at
	// Applied, even when that high-water mark matches the browser's copy.
	Unknown bool
}

// Action is a control request.
type Action string

const (
	Acquire  Action = "acquire"
	Renew    Action = "renew"
	Release  Action = "release"
	Takeover Action = "takeover"
)

// Refusal is terminal.Refusal with the two things a refusal here may name.
type Refusal struct {
	terminal.Refusal
	// Holder is who holds the lease, with terminal_controlled.
	Holder *Control
	// Applied is the applied number, with input_gap.
	Applied uint64
}

func refuse(code terminal.RefusalCode, detail string) *Refusal {
	return &Refusal{Refusal: terminal.Refusal{Code: code, Detail: detail}}
}

// Service is the machine's terminals, their leases and their viewers.
type Service struct {
	host   ports.OwnedTerminals
	access Access
	lanes  *lane.Lanes
	now    func() time.Time

	// InputTimeout replaces inputTimeout, for tests.
	InputTimeout time.Duration

	mu      sync.Mutex
	leases  map[terminal.ID]*lease
	epochs  map[terminal.ID]uint64
	viewers map[terminal.ID]map[*viewer]struct{}
	streams int
	// closing is who closed a terminal, kept while it still has viewers to
	// tell, so a stream that finds it gone says `closed` and names them rather
	// than `exited`.
	closing map[terminal.ID]closer
	resizes map[terminal.ID]*resizeSlot

	revalidations atomic.Int64
}

type lease struct {
	holder  Principal
	client  string
	epoch   uint64
	applied uint64
	expires time.Time
	// unknown is a lease an input's outcome could not be read under: nothing
	// more is typed under it.
	unknown bool
	// prev is the holder a takeover ended, so that its next input is told
	// lease_superseded rather than not_controller.
	prev *heldBy
}

type heldBy struct {
	device, client string
	epoch          uint64
}

// closer is who closed a terminal, and from which client.
type closer struct {
	who    Principal
	client string
}

type resizeSlot struct {
	mu  sync.Mutex
	gen uint64
}

// New is the service over host. access is asked whenever a lease or a stream
// begins and again whenever it may have changed.
func New(host ports.OwnedTerminals, access Access) *Service {
	return &Service{
		host: host, access: access, lanes: lane.New(LaneLimit), now: time.Now,
		leases: map[terminal.ID]*lease{}, epochs: map[terminal.ID]uint64{},
		viewers: map[terminal.ID]map[*viewer]struct{}{}, closing: map[terminal.ID]closer{},
		resizes: map[terminal.ID]*resizeSlot{},
	}
}

// Lanes is the service's own lanes, for diagnostics and for the test that
// fills them.
func (s *Service) Lanes() *lane.Lanes { return s.lanes }

// Stats is what diagnostics says: leases held now and streams open now.
func (s *Service) Stats() (leases, streams int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, l := range s.leases {
		if l.expires.After(now) {
			leases++
		}
	}
	return leases, s.streams
}

func (s *Service) allowed(p Principal) error {
	if s.access == nil {
		return refuse(terminal.CodeForbidden, "nobody is let in to terminals")
	}
	if err := s.access(p); err != nil {
		return err
	}
	return nil
}

// Allow checks a principal without asking a terminal to exist. The signed
// Cloud ingress uses it before accepting a new per-viewer connection key.
func (s *Service) Allow(p Principal) error { return s.allowed(p) }

// Open starts a shell in dir.
func (s *Service) Open(ctx context.Context, p Principal, req ports.OpenTerminal) (terminal.Terminal, error) {
	if err := s.allowed(p); err != nil {
		return terminal.Terminal{}, err
	}
	return s.host.Open(ctx, req)
}

// Listed is one terminal and its lease.
type Listed struct {
	terminal.Terminal
	Control Control
}

// List is every terminal, narrowed to project when it is not "".
func (s *Service) List(ctx context.Context, p Principal, project string) ([]Listed, error) {
	if err := s.allowed(p); err != nil {
		return nil, err
	}
	terms, err := s.host.List(ctx)
	if err != nil {
		return nil, err
	}
	present := map[terminal.ID]bool{}
	out := make([]Listed, 0, len(terms))
	for _, t := range terms {
		present[t.ID] = true
		if project != "" && t.ProjectID != project {
			continue
		}
		out = append(out, Listed{Terminal: t, Control: s.Control(t.ID)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	s.prune(present)
	return out, nil
}

// Find is one terminal and its lease.
func (s *Service) Find(ctx context.Context, p Principal, id terminal.ID) (Listed, error) {
	if err := s.allowed(p); err != nil {
		return Listed{}, err
	}
	t, err := s.find(ctx, id)
	if err != nil {
		return Listed{}, err
	}
	return Listed{Terminal: t, Control: s.Control(id)}, nil
}

func (s *Service) find(ctx context.Context, id terminal.ID) (terminal.Terminal, error) {
	terms, err := s.host.List(ctx)
	if err != nil {
		return terminal.Terminal{}, err
	}
	for _, t := range terms {
		if t.ID == id {
			return t, nil
		}
	}
	return terminal.Terminal{}, terminal.Refuse(terminal.CodeClosed, "there is no such terminal")
}

// prune forgets the leases of terminals a complete list no longer has.
func (s *Service) prune(present map[terminal.ID]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.leases {
		if !present[id] {
			delete(s.leases, id)
		}
	}
	for id := range s.epochs {
		if !present[id] {
			delete(s.epochs, id)
		}
	}
	for id := range s.resizes {
		if !present[id] {
			delete(s.resizes, id)
		}
	}
}

// History is the terminal's scrollback and screen, up to lines lines.
func (s *Service) History(ctx context.Context, p Principal, id terminal.ID, lines int) ([]string, error) {
	if err := s.allowed(p); err != nil {
		return nil, err
	}
	if lines <= 0 || lines > terminal.MaxHistoryLines {
		lines = terminal.MaxHistoryLines
	}
	return s.host.History(ctx, id, lines)
}

// HistoryBounded limits captured bytes for a Cloud reply while preserving the
// local History path's existing behavior.
func (s *Service) HistoryBounded(ctx context.Context, p Principal, id terminal.ID, lines, maxBytes int) ([]string, error) {
	if err := s.allowed(p); err != nil {
		return nil, err
	}
	if lines <= 0 || lines > terminal.MaxHistoryLines {
		lines = terminal.MaxHistoryLines
	}
	if maxBytes <= 0 {
		return nil, terminal.Refuse(terminal.CodeInvalid, "history byte limit is invalid")
	}
	return s.host.HistoryBounded(ctx, id, lines, maxBytes)
}

// Control is the lease on id as it stands.
func (s *Service) Control(id terminal.ID) Control {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controlLocked(id)
}

func (s *Service) controlLocked(id terminal.ID) Control {
	l := s.leases[id]
	if l == nil || !l.expires.After(s.now()) {
		return Control{Epoch: s.epochs[id]}
	}
	return Control{Held: true, Holder: l.holder, Client: l.client, Epoch: l.epoch,
		Expires: l.expires, Applied: l.applied, Unknown: l.unknown}
}

func (l *lease) heldBy(p Principal, client string) bool {
	return l.holder.Device == p.Device && l.client == client
}

// Control acquires, renews, releases or takes over id's lease for p's client.
func (s *Service) SetControl(ctx context.Context, p Principal, id terminal.ID, client string, action Action) (Control, error) {
	if err := s.allowed(p); err != nil {
		return Control{}, err
	}
	switch action {
	case Acquire, Takeover:
		// A lease on a terminal that is not there would be a lease on nothing.
		if _, err := s.find(ctx, id); err != nil {
			return Control{}, err
		}
	case Renew, Release:
	default:
		return Control{}, refuse(terminal.CodeInvalid, "action is acquire, renew, release or takeover")
	}
	s.mu.Lock()
	now := s.now()
	l := s.leases[id]
	active := l != nil && l.expires.After(now)
	switch action {
	case Acquire, Takeover:
		if action == Acquire && active && !l.heldBy(p, client) {
			c := s.controlLocked(id)
			s.mu.Unlock()
			r := refuse(terminal.CodeControlled, "somebody else holds this terminal's control; take it over to type")
			r.Holder = &c
			return Control{}, r
		}
		next := &lease{holder: p, client: client, epoch: s.epochs[id] + 1, expires: now.Add(LeaseTTL)}
		if active && !l.heldBy(p, client) {
			next.prev = &heldBy{device: l.holder.Device, client: l.client, epoch: l.epoch}
		}
		s.epochs[id] = next.epoch
		s.leases[id] = next
	case Renew:
		if err := s.holderLocked(id, p, client, 0, false); err != nil {
			s.mu.Unlock()
			return Control{}, err
		}
		l.expires = now.Add(LeaseTTL)
	case Release:
		if err := s.holderLocked(id, p, client, 0, false); err != nil {
			s.mu.Unlock()
			return Control{}, err
		}
		delete(s.leases, id)
	}
	c := s.controlLocked(id)
	s.controlChangedLocked(id)
	s.mu.Unlock()
	return c, nil
}

// holderLocked is whether p's client holds id's lease, and the refusal that
// says why not. With epoch > 0 it must be the lease's epoch as well. With
// s.mu held.
func (s *Service) holderLocked(id terminal.ID, p Principal, client string, epoch uint64, checkEpoch bool) *Refusal {
	l := s.leases[id]
	if l == nil {
		return refuse(terminal.CodeNotController, "nobody holds this terminal's control; acquire it first")
	}
	if !l.heldBy(p, client) {
		if prev := l.prev; prev != nil && prev.device == p.Device && prev.client == client &&
			(!checkEpoch || prev.epoch == epoch) {
			return refuse(terminal.CodeLeaseSuperseded, "somebody took this terminal's control over; nothing was typed")
		}
		return refuse(terminal.CodeNotController, "somebody else holds this terminal's control")
	}
	if !l.expires.After(s.now()) {
		return refuse(terminal.CodeLeaseExpired, "this lease lapsed without a renewal; acquire it again")
	}
	if checkEpoch && epoch != l.epoch {
		return refuse(terminal.CodeLeaseSuperseded, "that input belongs to an earlier lease; nothing was typed")
	}
	return nil
}

// closedInstead is refusal, or terminal_closed when the terminal is not there
// any more: a person told "somebody else holds it" of a shell that ended
// would go looking for somebody.
func (s *Service) closedInstead(ctx context.Context, id terminal.ID, refusal *Refusal) error {
	switch refusal.Code {
	case terminal.CodeNotController, terminal.CodeLeaseExpired:
		if _, err := s.find(ctx, id); err != nil {
			if code, _ := terminal.CodeOf(err); code == terminal.CodeClosed {
				return err
			}
		}
	}
	return refusal
}

// Input is one numbered keystroke batch from the lease's holder.
func (s *Service) Input(ctx context.Context, p Principal, id terminal.ID, client string, epoch, seq uint64, data []byte) (applied uint64, duplicate bool, err error) {
	if len(data) > terminal.MaxInputBytes {
		return 0, false, refuse(terminal.CodeInputTooLarge, "one keystroke batch is at most 4 KiB; nothing was typed")
	}
	return s.typed(ctx, p, id, client, epoch, seq, func(ctx context.Context) error {
		return s.host.Keys(ctx, id, data)
	})
}

// Paste is one numbered paste from the lease's holder, numbered with its
// inputs.
func (s *Service) Paste(ctx context.Context, p Principal, id terminal.ID, client string, epoch, seq uint64, text string) (applied uint64, duplicate bool, err error) {
	if len(text) > terminal.MaxPasteBytes {
		return 0, false, refuse(terminal.CodeInputTooLarge, "one paste is at most 1 MiB; nothing was typed")
	}
	return s.typed(ctx, p, id, client, epoch, seq, func(ctx context.Context) error {
		return s.host.Paste(ctx, id, text)
	})
}

// typed is the one critical section every numbered input goes through: the
// terminal's lane first, then the decision, the typing and the applied number
// — nothing between them can interleave another input for the same terminal.
func (s *Service) typed(ctx context.Context, p Principal, id terminal.ID, client string, epoch, seq uint64,
	typeIt func(context.Context) error) (uint64, bool, error) {
	if err := s.allowed(p); err != nil {
		return 0, false, err
	}
	release, err := s.lanes.Acquire(ctx, string(id))
	if err != nil {
		var busy lane.Busy
		if errors.As(err, &busy) {
			return 0, false, &Refusal{Refusal: terminal.Refusal{Code: terminal.CodeBusy,
				Detail: busy.Error(), Err: err}}
		}
		return 0, false, err
	}
	defer release()
	// A request can wait in the lane while a pin or grant is revoked. The
	// earlier check is only admission to the queue, not admission to the host.
	if err := s.allowed(p); err != nil {
		return 0, false, err
	}

	s.mu.Lock()
	if r := s.holderLocked(id, p, client, epoch, false); r != nil {
		s.mu.Unlock()
		return 0, false, s.closedInstead(ctx, id, r)
	}
	l := s.leases[id]
	if l.unknown {
		s.mu.Unlock()
		return 0, false, refuse(terminal.CodeInputStateUnknown,
			"an earlier input's outcome is unknown; look at the screen, then acquire control again")
	}
	switch terminal.DecideInput(l.applied, l.epoch, epoch, seq) {
	case terminal.InputSuperseded:
		s.mu.Unlock()
		return 0, false, refuse(terminal.CodeLeaseSuperseded, "that input belongs to an earlier lease; nothing was typed")
	case terminal.InputInvalid:
		s.mu.Unlock()
		return 0, false, refuse(terminal.CodeInvalid, "inputs are numbered from 1")
	case terminal.InputDuplicate:
		applied := l.applied
		s.mu.Unlock()
		return applied, true, nil
	case terminal.InputGap:
		r := refuse(terminal.CodeInputGap, "an earlier input has not arrived; resend from the one after applied_through")
		r.Applied = l.applied
		s.mu.Unlock()
		return 0, false, r
	}
	s.mu.Unlock()
	// The host effect begins after this last access verdict. An effect already
	// running cannot be rolled back; its receipt reports applied or unknown.
	if err := s.allowed(p); err != nil {
		return 0, false, err
	}

	// Typed outside s.mu and inside the lane: a slow tmux holds up this
	// terminal's next input and nobody else's control or stream. A caller
	// that goes away does not cut the typing short, because a cut-short
	// typing is exactly the outcome nobody can read afterwards.
	timeout := s.InputTimeout
	if timeout <= 0 {
		timeout = inputTimeout
	}
	typeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	err = typeIt(typeCtx)
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if definite(err) {
			return 0, false, err
		}
		l.unknown = true
		return 0, false, &Refusal{Refusal: terminal.Refusal{Code: terminal.CodeInputStateUnknown,
			Detail: "the terminal did not say whether it typed that; nothing more is typed under this lease", Err: err}}
	}
	// l is the lease this input was decided under. A takeover while it was
	// typing replaced it in the map; the bytes were typed under l, and l is
	// where they are counted.
	if seq > l.applied {
		l.applied = seq
	}
	return l.applied, false, nil
}

// definite is an input failure that says nothing was typed.
func definite(err error) bool {
	code, ok := terminal.CodeOf(err)
	if !ok {
		return false
	}
	switch code {
	case terminal.CodeClosed, terminal.CodeInputTooLarge, terminal.CodeInvalid, terminal.CodeUnsupported,
		terminal.CodeSocketPathTooLong:
		return true
	}
	return false
}

// Resize sets id's size for the lease's holder. It does not wait in the lane:
// a size is not an input, and the newest one asked for wins.
func (s *Service) Resize(ctx context.Context, p Principal, id terminal.ID, client string, epoch uint64, cols, rows int) error {
	if err := s.allowed(p); err != nil {
		return err
	}
	s.mu.Lock()
	if r := s.holderLocked(id, p, client, epoch, true); r != nil {
		s.mu.Unlock()
		return s.closedInstead(ctx, id, r)
	}
	slot := s.resizes[id]
	if slot == nil {
		slot = &resizeSlot{}
		s.resizes[id] = slot
	}
	slot.gen++
	mine := slot.gen
	s.mu.Unlock()

	slot.mu.Lock()
	defer slot.mu.Unlock()
	s.mu.Lock()
	newer := slot.gen != mine
	s.mu.Unlock()
	if newer {
		// A newer size arrived while this one waited; it is the one that
		// counts, and it is applied by its own request.
		return nil
	}
	return s.host.Resize(ctx, id, cols, rows)
}

// Close ends id for the lease's holder, and tells every viewer who closed it.
func (s *Service) Close(ctx context.Context, p Principal, id terminal.ID, client string, epoch uint64) error {
	if err := s.allowed(p); err != nil {
		return err
	}
	s.mu.Lock()
	if r := s.holderLocked(id, p, client, epoch, true); r != nil {
		s.mu.Unlock()
		return s.closedInstead(ctx, id, r)
	}
	s.closing[id] = closer{who: p, client: client}
	s.mu.Unlock()
	if err := s.host.Close(ctx, id); err != nil {
		s.mu.Lock()
		delete(s.closing, id)
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, id)
	delete(s.resizes, id)
	by := p
	for v := range s.viewers[id] {
		v.end(Event{Kind: EventState, Status: terminal.Closed, ClosedBy: &by, ClosedClient: client})
	}
	if len(s.viewers[id]) == 0 {
		delete(s.closing, id)
	}
	return nil
}

// Revalidate asks Access again of every lease holder and every viewer. A
// holder that lost access loses its lease; a viewer that lost access has its
// stream ended with terminal_access_revoked. The daemon calls it whenever
// devices or grants change.
func (s *Service) Revalidate() {
	s.revalidations.Add(1)
	s.mu.Lock()
	holders := map[terminal.ID]Principal{}
	for id, l := range s.leases {
		holders[id] = l.holder
	}
	var watching []*viewer
	for _, vs := range s.viewers {
		for v := range vs {
			watching = append(watching, v)
		}
	}
	s.mu.Unlock()

	// Asked outside s.mu: Access reads the device list and the grants file.
	voided := map[terminal.ID]bool{}
	for id, p := range holders {
		if s.allowed(p) != nil {
			voided[id] = true
		}
	}
	var revoked []*viewer
	for _, v := range watching {
		if s.allowed(v.who) != nil {
			revoked = append(revoked, v)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range voided {
		if l := s.leases[id]; l != nil && l.holder == holders[id] {
			delete(s.leases, id)
			s.controlChangedLocked(id)
		}
	}
	for _, v := range revoked {
		v.end(Event{Kind: EventRefusal, Refusal: refuse(terminal.CodeAccessRevoked,
			"this device may no longer see this machine's terminals")})
	}
}

// Revalidations is how many sweeps have run, for tests.
func (s *Service) Revalidations() int64 { return s.revalidations.Load() }

// controlChangedLocked tells every viewer of id that its lease changed. With
// s.mu held.
func (s *Service) controlChangedLocked(id terminal.ID) {
	for v := range s.viewers[id] {
		v.controlDirty.Store(true)
		v.kick()
	}
}
