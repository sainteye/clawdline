package cloud

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// KeyEpoch is the runtime authority for this machine's content key: one
// ContentKeyState, read whole, and a fence that a key cutover closes.
//
// It exists because a key and its id read separately can be torn by a
// rotation between the two reads — a frame sealed under the new key and
// labelled with the old id, which no reader can open. Everything that seals
// or writes a master-key frame does it inside Use, so the key, its id, the
// spool row and the socket write all belong to one epoch.
//
// Cutover and Recover hold the fence exclusively. sync.RWMutex blocks new
// readers once a writer waits, so a cutover first stops new publishes and
// sends, then waits for the ones already inside Use; the socket I/O has its
// own deadline, but lock acquisition and callbacks need separate bounds from
// their owners. This type does not bound the whole wait. Lock order is
// always KeyEpoch, then whatever the callback locks (the spool): a function
// passed to Use must never call Use, Current, Cutover or Recover again, and a
// rotate, recover or retire callback must never publish.
//
// Any callback failure leaves the epoch **halted**: Use and Current answer
// ErrKeyEpochHalted until Recover succeeds. The durable store may already have
// advanced (ContentKeyStore says a partial write may be durable), so carrying
// on with the old key would be sending under a key the store has retired.
//
// This is preparation for revocation-driven rotation, not that feature: see
// docs/security/content-key-runtime.md for what is wired and what is not.
type KeyEpoch struct {
	mu     sync.RWMutex
	state  ContentKeyState
	halted error
}

var (
	// ErrKeyEpochHalted means a cutover or recovery failed and nothing may be
	// sealed, sent or opened under the master key until Recover succeeds.
	ErrKeyEpochHalted = errors.New("content key epoch is halted until recovery")
	// ErrKeyEpochRegress is a rotation or recovery that answered an epoch
	// below, or a different key at, the one already installed.
	ErrKeyEpochRegress = fmt.Errorf("%w: the content key epoch may not move backwards", ErrContentKeyConflict)
)

// NewKeyEpoch adopts a state loaded from ContentKeyStore.
func NewKeyEpoch(state ContentKeyState) (*KeyEpoch, error) {
	if err := validEpochState(state); err != nil {
		return nil, err
	}
	return &KeyEpoch{state: state}, nil
}

func validEpochState(state ContentKeyState) error {
	if state.Epoch == 0 || !state.Key.Valid() || state.KeyID != rekeyKeyID(state.Epoch, state.Key.Bytes()) {
		return ErrContentKeyState
	}
	return nil
}

// Use runs fn with the current state while the fence is held shared. fn's
// error is returned as it is; a halted epoch answers ErrKeyEpochHalted and
// does not run fn.
func (e *KeyEpoch) Use(fn func(ContentKeyState) error) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.halted != nil {
		return e.halted
	}
	return fn(e.state)
}

// Current is a snapshot of the installed state, for opening an inbound
// envelope. A halted epoch answers ErrKeyEpochHalted.
func (e *KeyEpoch) Current() (ContentKeyState, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.halted != nil {
		return ContentKeyState{}, e.halted
	}
	return e.state, nil
}

// Halted reports why the epoch is halted, or nil.
func (e *KeyEpoch) Halted() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.halted
}

// Sendable reports whether a frame labelled keyID may still be written under
// state: a per-connection reply key (rk-*) is not this authority's, and a
// master-key frame must carry the installed id. An unlabelled frame is
// unknown, and unknown is not current.
func (state ContentKeyState) Sendable(keyID string) bool {
	return strings.HasPrefix(keyID, "rk-") || (keyID != "" && keyID == state.KeyID)
}

// KeyRetire retires everything owed under old once next is the key; it runs
// with the fence held exclusively. It must be atomic from a sender's view —
// the spool does it under its own lock (Spool.RetireKeys).
type KeyRetire func(old, next ContentKeyState) error

// Cutover rotates the key with every publish and send stopped:
//
//  1. close the fence and wait for every Use already running to return;
//  2. rotate(old) — the durable ContentKeyStore.RotateContentKey;
//  3. retire(old, next) — every frame still owed under the old key;
//  4. install next and open the fence.
//
// On any failure the epoch halts with the old state kept and the error
// returned wrapped in ErrKeyEpochHalted; only Recover reopens it. A halted
// epoch refuses Cutover outright, because the store may have advanced and the
// next step is recovery, not a second rotation.
func (e *KeyEpoch) Cutover(rotate func(old ContentKeyState) (ContentKeyState, error), retire KeyRetire) (ContentKeyState, error) {
	if rotate == nil || retire == nil {
		return ContentKeyState{}, errors.New("a cutover needs a rotate and a retire step")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.halted != nil {
		return ContentKeyState{}, e.halted
	}
	old := e.state
	next, err := rotate(old)
	if err == nil {
		err = validEpochState(next)
	}
	if err == nil && next.Epoch <= old.Epoch {
		err = fmt.Errorf("%w: rotation answered epoch %d at %d", ErrKeyEpochRegress, next.Epoch, old.Epoch)
	}
	if err == nil {
		err = retire(old, next)
	}
	if err != nil {
		e.halted = fmt.Errorf("%w: %w", ErrKeyEpochHalted, err)
		return ContentKeyState{}, e.halted
	}
	e.state = next
	return next, nil
}

// Recover reopens a halted epoch from what the durable store says now
// (ContentKeyStore.RecoverContentKey or LoadOrCreateContentKey). The answer
// may be the installed epoch — the rotation never became durable — or a later
// one, whose old frames are then retired before it is installed. An earlier
// epoch, or a different key at the same epoch, is a conflict and the epoch
// stays halted. Recover on an epoch that is not halted is the same check.
func (e *KeyEpoch) Recover(recover func() (ContentKeyState, error), retire KeyRetire) (ContentKeyState, error) {
	if recover == nil || retire == nil {
		return ContentKeyState{}, errors.New("a recovery needs a recover and a retire step")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.state
	next, err := recover()
	if err == nil {
		err = validEpochState(next)
	}
	if err == nil {
		switch {
		case next.Epoch < old.Epoch:
			err = fmt.Errorf("%w: recovery answered epoch %d at %d", ErrKeyEpochRegress, next.Epoch, old.Epoch)
		case next.Epoch == old.Epoch && (next.KeyID != old.KeyID || !hmac.Equal(next.Key.raw, old.Key.raw)):
			err = fmt.Errorf("%w: recovery answered another key at epoch %d", ErrKeyEpochRegress, next.Epoch)
		case next.Epoch > old.Epoch:
			err = retire(old, next)
		}
	}
	if err != nil {
		e.halted = fmt.Errorf("%w: %w", ErrKeyEpochHalted, err)
		return ContentKeyState{}, e.halted
	}
	e.state = next
	e.halted = nil
	return next, nil
}
