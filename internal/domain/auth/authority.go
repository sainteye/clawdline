package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// PairingLifetime is long enough to walk to the Mac, short enough that a
	// code left on a screen is not a key somebody finds later.
	PairingLifetime = 2 * time.Minute
	// PairingGuesses is how many wrong codes pairing takes in a
	// PairingGuessWindow, counted across every pairing rather than per
	// pairing. A million codes and five tries is not a number anybody grinds
	// through — but only while asking again does not hand out five more, which
	// in the Swift app it does: there, three requests every ten minutes were
	// fifteen guesses every ten minutes, over half the code space in a year.
	PairingGuesses = 5
	// PairingGuessWindow is how long a wrong code counts. A day, so the
	// steady rate is five guesses a day, about one chance in 550 a year.
	// Anybody who can reach the pairing route can use the five up and keep
	// pairing closed; they could already, with three requests every ten
	// minutes, so the long window takes nothing away that was there.
	PairingGuessWindow = 24 * time.Hour
	// PairingWindow and PairingRequests bound the one route that has to be
	// reachable without a token and that puts an alert on somebody's screen.
	PairingWindow   = 10 * time.Minute
	PairingRequests = 3
	// PasswordFailures is how many wrong passwords the password door takes in
	// a PasswordWindow, from everybody together: through a tunnel every
	// request comes from 127.0.0.1, so there is nobody else to count them by.
	// A check under way counts against it until it is known to have passed,
	// which is also what bounds how many can wait for their turn.
	PasswordFailures = 10
	PasswordWindow   = 24 * time.Hour
	// PasswordIterations is PBKDF2-HMAC-SHA256's cost: slow on purpose.
	PasswordIterations = 600_000
	// MinPasswordIterations and MaxPasswordIterations are the counts a stored
	// password may carry. Below, the hash is not doing its job; above, one
	// guess would hold the machine for seconds.
	MinPasswordIterations = 100_000
	MaxPasswordIterations = 10_000_000
	// PasswordHashBytes and PasswordSaltBytes are the stored password's sizes,
	// which both apps write.
	PasswordHashBytes = 32
	PasswordSaltBytes = 16
	// LocalName is what the machine's own device is called.
	LocalName = "This Mac"
	// DefaultName is what an unnamed device is called.
	DefaultName = "A browser"
	nameLimit   = 40
)

// PairingNotice is what a local watcher is told: the code, for the one person
// who may see it.
type PairingNotice struct {
	ID      string
	Name    string
	Code    string
	Expires time.Time
}

// Authority holds the devices, the pairing in progress and the rules for both.
// One per state directory per process: two over one store would be two
// writers.
type Authority struct {
	store  Store
	now    func() time.Time
	random io.Reader

	mu           sync.Mutex
	devices      map[string]Device
	password     *Password
	pending      *pairing
	pairingTimes []time.Time
	// pairingMisses is every wrong code in the last PairingGuessWindow,
	// whichever pairing it was typed into. A new pairing does not clear it.
	pairingMisses []time.Time
	// passwordMisses is every wrong password in the last PasswordWindow, and
	// passwordChecks the checks admitted and not yet finished.
	passwordMisses []time.Time
	passwordChecks int
	watchers       map[int]chan PairingNotice
	nextWatcher    int
	cachedLocal    string

	// derive is one password check at a time. The Swift server answers on one
	// queue, so a second attempt waited for the first; handlers here run
	// concurrently, and without this a burst of guesses would be a burst of
	// 600,000-round hashes at once. A channel rather than a mutex, so a caller
	// who has gone away stops waiting.
	derive chan struct{}
}

// Options replace the clock and the random source, for tests.
type Options struct {
	Now    func() time.Time
	Random io.Reader
}

// New loads the store. A store that exists and cannot be read is a refusal,
// not an empty device list: the Swift app treats it as empty and its next save
// throws the unreadable file away, which is the one outcome worth refusing. A
// store that reads and does not make sense is refused the same way
// (ErrInvalidState), before anything could be written over it.
func New(store Store, opts Options) (*Authority, error) {
	a := &Authority{
		store:    store,
		now:      opts.Now,
		random:   opts.Random,
		devices:  map[string]Device{},
		watchers: map[int]chan PairingNotice{},
		derive:   make(chan struct{}, 1),
	}
	if a.now == nil {
		a.now = time.Now
	}
	if a.random == nil {
		a.random = defaultRandom
	}
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	if err := checkState(state); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	for _, d := range state.Devices {
		a.devices[d.ID] = d
	}
	a.password = state.Password
	return a, nil
}

// checkState is what a loaded state must be before anything acts on it: every
// device named once and keyed by a digest no other device has, at most one of
// them this machine's own, and a password this package could have written.
func checkState(s State) error {
	ids := map[string]bool{}
	hashes := map[string]bool{}
	locals := 0
	for _, d := range s.Devices {
		if d.ID == "" {
			return errors.New("a device has no id")
		}
		if ids[d.ID] {
			return fmt.Errorf("device id %q appears twice", d.ID)
		}
		ids[d.ID] = true
		if !isDigest(d.Hash) {
			return fmt.Errorf("device %q has no SHA-256 digest", d.ID)
		}
		if hashes[d.Hash] {
			return fmt.Errorf("device %q shares its digest with another device", d.ID)
		}
		hashes[d.Hash] = true
		for _, c := range d.Caps {
			if _, ok := ParseCapability(string(c)); !ok {
				return fmt.Errorf("device %q has an unknown capability", d.ID)
			}
		}
		if d.Local {
			locals++
			if !d.Approved {
				return fmt.Errorf("this machine's device %q is not approved", d.ID)
			}
		}
	}
	if locals > 1 {
		return fmt.Errorf("%d devices say they are this machine's own", locals)
	}
	if p := s.Password; p != nil {
		if len(p.Hash) != PasswordHashBytes || len(p.Salt) != PasswordSaltBytes {
			return fmt.Errorf("the password record is %d hash and %d salt bytes, not %d and %d",
				len(p.Hash), len(p.Salt), PasswordHashBytes, PasswordSaltBytes)
		}
		if p.Iterations < MinPasswordIterations || p.Iterations > MaxPasswordIterations {
			return fmt.Errorf("the password record's %d iterations are outside %d to %d",
				p.Iterations, MinPasswordIterations, MaxPasswordIterations)
		}
	}
	return nil
}

// isDigest is a SHA-256 in lowercase hex, the only spelling Hash produces.
func isDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !('0' <= s[i] && s[i] <= '9' || 'a' <= s[i] && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// recent keeps the times within window of now, in place.
func recent(times []time.Time, now time.Time, window time.Duration) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	return kept
}

// snapshot is the state as it would be saved, from a given device map.
func (a *Authority) snapshot(devices map[string]Device, pw *Password) State {
	out := State{Password: pw}
	for _, d := range devices {
		out.Devices = append(out.Devices, d)
	}
	sort.Slice(out.Devices, func(i, j int) bool {
		if !out.Devices[i].Created.Equal(out.Devices[j].Created) {
			return out.Devices[i].Created.Before(out.Devices[j].Created)
		}
		return out.Devices[i].ID < out.Devices[j].ID
	})
	return out
}

// commit saves the next state and only then makes it the state in memory, so
// a failed write leaves memory and disk agreeing. Called with mu held.
func (a *Authority) commit(devices map[string]Device, pw *Password) error {
	if err := a.store.Save(a.snapshot(devices, pw)); err != nil {
		return fmt.Errorf("store_unavailable: %w", err)
	}
	a.devices = devices
	a.password = pw
	return nil
}

func (a *Authority) copyDevices() map[string]Device {
	out := make(map[string]Device, len(a.devices))
	for k, v := range a.devices {
		out[k] = v
	}
	return out
}

// IsConfigured is true when a person has set this up: a paired device or a
// password. The local token does not count — if it did, merely starting the
// daemon would satisfy the interlock that keeps a tunnel closed until somebody
// has decided to be reachable. No tunnel exists in this daemon yet; this is the
// predicate it will ask.
func (a *Authority) IsConfigured() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.password != nil {
		return true
	}
	for _, d := range a.devices {
		if d.Approved && !d.Local {
			return true
		}
	}
	return false
}

// HasPassword says whether the password door exists at all.
func (a *Authority) HasPassword() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.password != nil
}

// Devices is every approved device a person paired, oldest first. The machine's
// own is not in it.
func (a *Authority) Devices() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Device
	for _, d := range a.devices {
		if d.Approved && !d.Local {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Verify turns a bearer token into what it may do.
//
// Constant time all the way through, including the lookup: every approved
// device is compared, so a token naming no device takes the same path as one
// naming a device with the wrong secret. No token, no answer — with no
// exception for a request that came from this machine.
func (a *Authority) Verify(bearer string) Verdict {
	if bearer == "" {
		return Verdict{}
	}
	digest := Hash(bearer)
	a.mu.Lock()
	defer a.mu.Unlock()
	var matched *Device
	for id := range a.devices {
		d := a.devices[id]
		if !d.Approved {
			continue
		}
		if ConstantTimeEquals(d.Hash, digest) {
			found := d
			matched = &found
		}
	}
	if matched == nil {
		return Verdict{}
	}
	// Remembered in memory and written with the next change, as the Swift
	// store does: a read is not a reason to rewrite the file.
	matched.LastSeen = a.now()
	a.devices[matched.ID] = *matched
	return Verdict{Allowed: true, Device: matched.ID, Caps: matched.Caps, Local: matched.Local}
}

// Started is what the asker is told about a pairing: which one, and until
// when. The code is not in it, so no caller can put it in a response by
// mistake; it goes to watchers and nowhere else.
type Started struct {
	ID      string
	Expires time.Time
}

// BeginPairing opens a pairing.
//
// One at a time, and a new request replaces the old rather than joining it:
// two live codes are two chances to guess, and the person at the Mac is
// looking at one alert. Three requests in ten minutes, then ErrRateLimited
// until the window rolls; a refused request does not count against it. While
// the wrong codes have used up PairingGuesses, ErrPairingLocked: a code nobody
// may try is not worth putting on anybody's screen.
func (a *Authority) BeginPairing(name string) (Started, error) {
	now := a.now()
	a.mu.Lock()
	a.pairingMisses = recent(a.pairingMisses, now, PairingGuessWindow)
	if len(a.pairingMisses) >= PairingGuesses {
		a.mu.Unlock()
		return Started{}, ErrPairingLocked
	}
	a.pairingTimes = recent(a.pairingTimes, now, PairingWindow)
	if len(a.pairingTimes) >= PairingRequests {
		a.mu.Unlock()
		return Started{}, ErrRateLimited
	}
	a.mu.Unlock()

	id, err := newID(a.random)
	if err != nil {
		return Started{}, err
	}
	code, err := newCode(a.random)
	if err != nil {
		return Started{}, err
	}
	token, err := NewToken(a.random)
	if err != nil {
		return Started{}, err
	}
	entry := pairing{
		ID:      id,
		Name:    DeviceName(name),
		Code:    code,
		token:   token,
		Expires: now.Add(PairingLifetime),
	}

	a.mu.Lock()
	// Checked again: another request may have taken the last place, or the
	// last guess, while this one was drawing random numbers.
	if len(a.pairingMisses) >= PairingGuesses {
		a.mu.Unlock()
		return Started{}, ErrPairingLocked
	}
	if len(a.pairingTimes) >= PairingRequests {
		a.mu.Unlock()
		return Started{}, ErrRateLimited
	}
	a.pairingTimes = append(a.pairingTimes, now)
	a.pending = &entry
	a.store.Audit("pair.begin", map[string]string{"device": entry.Name})
	notice := PairingNotice{ID: entry.ID, Name: entry.Name, Code: entry.Code, Expires: entry.Expires}
	for _, ch := range a.watchers {
		offer(ch, notice)
	}
	a.mu.Unlock()
	return Started{ID: entry.ID, Expires: entry.Expires}, nil
}

// ConfirmPairing checks a code. The wrong ones are counted on the authority,
// not on the pairing and not on a connection, so neither asking again nor
// retrying from somewhere else hands out more: the fifth wrong code in a
// PairingGuessWindow ends the open pairing and keeps pairing closed until the
// oldest of them is a window old. (The Swift app counts on the pairing, and a
// new request starts the count again; see PairingGuesses.) A pairing that is
// not the open one, or has lapsed, is Expired.
func (a *Authority) ConfirmPairing(id, code string) (PairResult, error) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pairingMisses = recent(a.pairingMisses, now, PairingGuessWindow)
	entry := a.pending
	if entry == nil || entry.ID != id || !entry.Expires.After(now) || len(a.pairingMisses) >= PairingGuesses {
		if entry != nil && entry.ID == id {
			a.pending = nil
		}
		return PairResult{Kind: Expired}, nil
	}
	if !ConstantTimeEquals(entry.Code, trimSpaces(code)) {
		a.pairingMisses = append(a.pairingMisses, now)
		left := PairingGuesses - len(a.pairingMisses)
		if left <= 0 {
			a.pending = nil
			a.store.Audit("pair.locked", map[string]string{"device": entry.Name})
			return PairResult{Kind: Expired}, nil
		}
		return PairResult{Kind: WrongCode, Left: left}, nil
	}
	next := a.copyDevices()
	next[entry.ID] = Device{
		ID:   entry.ID,
		Name: entry.Name,
		Hash: Hash(entry.token),
		// Reading only. Sending is granted separately, because it is a
		// different risk entirely.
		Caps:     NewCaps(Read),
		Created:  now,
		Approved: true,
	}
	if err := a.commit(next, a.password); err != nil {
		// The pairing stays open: the person typed the right code and the
		// failure was this machine's, so a retry is allowed to succeed.
		return PairResult{}, err
	}
	a.pending = nil
	a.store.Audit("pair.done", map[string]string{"device": entry.Name, "id": entry.ID})
	return PairResult{Kind: Paired, Token: entry.token, Device: entry.ID}, nil
}

// AddDevice approves a device from this machine directly — "Open in a
// browser" and a password exchange both end here.
func (a *Authority) AddDevice(name string, caps Caps, local bool) (id, token string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.addDevice(name, caps, local)
}

func (a *Authority) addDevice(name string, caps Caps, local bool) (string, string, error) {
	id, err := newID(a.random)
	if err != nil {
		return "", "", err
	}
	token, err := NewToken(a.random)
	if err != nil {
		return "", "", err
	}
	next := a.copyDevices()
	next[id] = Device{
		ID: id, Name: name, Hash: Hash(token), Caps: caps,
		Created: a.now(), Approved: true, Local: local,
	}
	if err := a.commit(next, a.password); err != nil {
		return "", "", err
	}
	a.store.Audit("device.add", map[string]string{"device": name, "id": id})
	return id, token, nil
}

// Revoke removes one paired device. The machine's own device is not revoked
// here: the shell and every script running as this user hold it, and taking it
// away from under them is not what removing a phone means. Deleting the token
// file and restarting the daemon replaces it.
func (a *Authority) Revoke(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.devices[id]
	if !ok {
		return ErrNotFound
	}
	if d.Local {
		return ErrLocalDevice
	}
	return a.revoke(d)
}

func (a *Authority) revoke(d Device) error {
	next := a.copyDevices()
	delete(next, d.ID)
	if err := a.commit(next, a.password); err != nil {
		return err
	}
	a.store.Audit("device.revoke", map[string]string{"device": d.Name, "id": d.ID})
	return nil
}

// RevokeAll removes every paired device, the password and the open pairing, at
// once — the control that exists because the moment somebody wants it is the
// moment they do not want to be reading a list.
//
// Unlike the Swift app, the machine's own device survives it, for the reason
// Revoke gives. The count is of what was removed. The wrong codes and wrong
// passwords already counted stay counted: taking every key away is no reason
// to let more guesses in.
func (a *Authority) RevokeAll() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	next := map[string]Device{}
	removed := 0
	for id, d := range a.devices {
		if d.Local {
			next[id] = d
			continue
		}
		removed++
	}
	if err := a.commit(next, nil); err != nil {
		return 0, err
	}
	a.pending = nil
	a.store.Audit("device.revoke_all", map[string]string{"count": fmt.Sprint(removed)})
	return removed, nil
}

// SetCapabilities replaces what a paired device may do. Reading is always
// included; admin belongs to the machine's own device and is not granted here.
func (a *Authority) SetCapabilities(id string, caps Caps) (Caps, error) {
	want := Caps{Read}
	for _, c := range caps {
		switch c {
		case Read:
		case Send:
			want = append(want, Send)
		default:
			return nil, ErrBadCaps
		}
	}
	want = NewCaps(want...)
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.devices[id]
	if !ok || !d.Approved {
		return nil, ErrNotFound
	}
	if d.Local {
		return nil, ErrLocalDevice
	}
	next := a.copyDevices()
	d.Caps = want
	next[id] = d
	if err := a.commit(next, a.password); err != nil {
		return nil, err
	}
	a.store.Audit("device.caps", map[string]string{"id": id, "caps": want.Joined()})
	return want, nil
}

// LocalToken is the token this machine keeps for itself, made the first time
// the daemon starts.
//
// There is no unauthenticated local port here. Every process on the machine
// can reach loopback, and so can a web page the user is visiting — with a DNS
// rebinding, as same-origin. A token in a 0600 file is a real boundary against
// that, because a page cannot read files. It is not a boundary against
// software already running as the user, and nothing at this layer is.
//
// The plaintext lives in the token file and only its hash in the store, so the
// two are checked against each other. Reading it back matters: otherwise every
// start would revoke and re-mint, and the file would change under any script
// that had read it. A mismatch is the only reason to start again.
func (a *Authority) LocalToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var existing *Device
	for id := range a.devices {
		d := a.devices[id]
		if d.Local {
			existing = &d
			break
		}
	}
	if existing != nil {
		if onDisk, err := a.store.ReadLocalToken(); err == nil {
			token := strings.TrimSpace(onDisk)
			if token != "" && ConstantTimeEquals(Hash(token), existing.Hash) {
				a.cachedLocal = token
				return token, nil
			}
		}
		if a.cachedLocal != "" && ConstantTimeEquals(Hash(a.cachedLocal), existing.Hash) {
			if err := a.store.WriteLocalToken(a.cachedLocal); err != nil {
				return "", err
			}
			return a.cachedLocal, nil
		}
		if err := a.revoke(*existing); err != nil {
			return "", err
		}
	}
	_, token, err := a.addDevice(LocalName, NewCaps(Read, Send, Admin), true)
	if err != nil {
		return "", err
	}
	a.cachedLocal = token
	if err := a.store.WriteLocalToken(token); err != nil {
		return "", err
	}
	return token, nil
}

// SetPassword sets the password, or clears it when plain is empty.
func (a *Authority) SetPassword(plain string) error {
	if plain == "" {
		a.mu.Lock()
		defer a.mu.Unlock()
		if err := a.commit(a.copyDevices(), nil); err != nil {
			return err
		}
		a.store.Audit("password.clear", map[string]string{})
		return nil
	}
	salt := make([]byte, PasswordSaltBytes)
	if _, err := io.ReadFull(a.random, salt); err != nil {
		return fmt.Errorf("no randomness for a salt: %w", err)
	}
	hash, err := derive(plain, salt, PasswordIterations)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	pw := &Password{Hash: hash, Salt: salt, Iterations: PasswordIterations}
	if err := a.commit(a.copyDevices(), pw); err != nil {
		return err
	}
	a.store.Audit("password.set", map[string]string{})
	return nil
}

// Exchange turns a correct password into a new device token. The password is
// not itself a credential the client keeps, so a password that leaks later
// cannot be replayed against a device already paired, and revoking a device
// does not mean changing it. ok is false for a wrong password and for no
// password at all.
//
// It is the one route that runs a deliberately slow hash for anybody who asks,
// so what it will do is decided before the hash runs:
//
//   - ErrRateLimited once the wrong passwords of the last PasswordWindow and
//     the checks under way together reach PasswordFailures. Nothing is hashed
//     and nothing is written for a refused attempt. The Swift app has no such
//     limit; there, one queue made guesses wait, and waiting is not a limit.
//   - The checks admitted wait for one another, and a caller whose ctx ends
//     while waiting leaves without hashing and without counting.
//   - The device name is cut to what DeviceName allows before it is written
//     anywhere, the audit included.
func (a *Authority) Exchange(ctx context.Context, plain, deviceName string) (token string, ok bool, err error) {
	name := DeviceName(deviceName)
	a.mu.Lock()
	stored := a.password
	if stored == nil {
		a.mu.Unlock()
		return "", false, nil
	}
	a.passwordMisses = recent(a.passwordMisses, a.now(), PasswordWindow)
	if len(a.passwordMisses)+a.passwordChecks >= PasswordFailures {
		a.mu.Unlock()
		return "", false, ErrRateLimited
	}
	a.passwordChecks++
	a.mu.Unlock()

	missed := false
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.passwordChecks--
		if !missed {
			return
		}
		now := a.now()
		a.passwordMisses = append(recent(a.passwordMisses, now, PasswordWindow), now)
		if len(a.passwordMisses) == PasswordFailures {
			// Once per closing, not once per refusal: a refused attempt
			// writes nothing.
			a.store.Audit("password.locked", map[string]string{})
		}
	}()

	select {
	case a.derive <- struct{}{}:
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-a.derive
		return "", false, err
	}
	attempt, err := derive(plain, stored.Salt, stored.Iterations)
	<-a.derive
	if err != nil {
		return "", false, err
	}
	if !ConstantTimeEquals(string(attempt), string(stored.Hash)) {
		missed = true
		a.store.Audit("password.fail", map[string]string{"device": name})
		return "", false, nil
	}
	_, token, err = a.AddDevice(name, NewCaps(Read), false)
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

// Holds reports whether id is still an approved device, and which.
// A long-lived stream asks before each thing it sends, so a device revoked
// while the stream was open stops hearing from it.
func (a *Authority) Holds(id string) (Device, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.devices[id]
	return d, ok && d.Approved
}

// Watch delivers each new pairing to the caller, starting with the one open
// now, if any. Only the newest pairing matters — a new one replaces the old —
// so a watcher that falls behind is handed the newest rather than blocking the
// route. cancel must be called.
func (a *Authority) Watch() (<-chan PairingNotice, func()) {
	ch := make(chan PairingNotice, 1)
	a.mu.Lock()
	id := a.nextWatcher
	a.nextWatcher++
	a.watchers[id] = ch
	if p := a.pending; p != nil && p.Expires.After(a.now()) {
		offer(ch, PairingNotice{ID: p.ID, Name: p.Name, Code: p.Code, Expires: p.Expires})
	}
	a.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			a.mu.Lock()
			delete(a.watchers, id)
			a.mu.Unlock()
		})
	}
}

// offer puts n on ch, replacing whatever is waiting there.
func offer(ch chan PairingNotice, n PairingNotice) {
	for {
		select {
		case ch <- n:
			return
		default:
		}
		select {
		case <-ch:
		default:
		}
	}
}

func derive(plain string, salt []byte, iterations int) ([]byte, error) {
	if iterations < MinPasswordIterations || iterations > MaxPasswordIterations {
		return nil, fmt.Errorf("the password record's %d iterations are outside %d to %d",
			iterations, MinPasswordIterations, MaxPasswordIterations)
	}
	return pbkdf2.Key(sha256.New, plain, salt, iterations, PasswordHashBytes)
}

// DeviceName is what a device asking to be let in is called: trimmed, never
// empty, at most forty characters.
func DeviceName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return DefaultName
	}
	if r := []rune(name); len(r) > nameLimit {
		name = string(r[:nameLimit])
	}
	return name
}

// trimSpaces drops the characters Foundation's `.whitespaces` names — spaces
// and tabs, not newlines — from both ends.
func trimSpaces(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) })
}
