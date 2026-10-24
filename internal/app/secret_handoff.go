package app

import (
	"errors"
	"sync"
	"time"
)

// Secret Handoff keeps a person's value outside the store, Note, transcript,
// and receipts. A daemon restart intentionally loses every pending value.
const (
	MaxSecretHandoffBytes           = 16 << 10
	MaxSecretHandoffPending         = 128
	MaxSecretHandoffSeconds         = 15 * 60
	MaxSecretHandoffDeliverySeconds = 5
)

var (
	ErrSecretTooLarge    = errors.New("secret_too_large")
	ErrSecretEmpty       = errors.New("secret_empty")
	ErrSecretFull        = errors.New("secret_handoffs_full")
	ErrSecretUnavailable = errors.New("secret_unavailable")
)

type secretHandoffValue struct {
	conversation string
	value        []byte
	expires      time.Time
}

type SecretHandoffs struct {
	mu      sync.Mutex
	values  map[string]secretHandoffValue
	claimed map[string]bool
	Now     func() time.Time
}

func NewSecretHandoffs() *SecretHandoffs {
	return &SecretHandoffs{values: make(map[string]secretHandoffValue), claimed: make(map[string]bool), Now: time.Now}
}

func wipeSecret(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func (h *SecretHandoffs) expire(now time.Time) {
	for id, entry := range h.values {
		if !now.Before(entry.expires) {
			wipeSecret(entry.value)
			delete(h.values, id)
		}
	}
}

// Put replaces an unclaimed value. The caller has checked that the Note is an
// open Secret Handoff addressed to conversation. No value is returned.
func (h *SecretHandoffs) Put(id, conversation string, value []byte) error {
	return h.PutAndCommit(id, conversation, value, nil)
}

// PutAndCommit publishes a value only after commit succeeds. The callback runs
// under the handoff lock so a competing submission or claim cannot replace or
// consume a value before its Note and receipt are committed. It must not call h.
func (h *SecretHandoffs) PutAndCommit(id, conversation string, value []byte, commit func() error) error {
	if len(value) == 0 {
		return ErrSecretEmpty
	}
	if len(value) > MaxSecretHandoffBytes {
		return ErrSecretTooLarge
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	if h.claimed[id] {
		return ErrSecretUnavailable
	}
	if _, exists := h.values[id]; !exists && len(h.values) >= MaxSecretHandoffPending {
		return ErrSecretFull
	}
	if commit != nil {
		if err := commit(); err != nil {
			return err
		}
	}
	if old, exists := h.values[id]; exists {
		wipeSecret(old.value)
	}
	h.values[id] = secretHandoffValue{conversation: conversation, value: append([]byte(nil), value...), expires: h.Now().Add(MaxSecretHandoffSeconds * time.Second)}
	time.AfterFunc(MaxSecretHandoffSeconds*time.Second, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.expire(h.Now())
	})
	return nil
}

// Pending reports only whether an unclaimed value is waiting for this
// conversation. It never exposes the value or its size.
func (h *SecretHandoffs) Pending(id, conversation string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	entry, ok := h.values[id]
	return ok && entry.conversation == conversation && !h.claimed[id]
}

// Take is a single-use claim. It returns a detached byte slice for the local
// command to pipe to a tool; that command must wipe it after writing.
func (h *SecretHandoffs) Take(id, conversation string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	entry, exists := h.values[id]
	if !exists || entry.conversation != conversation || h.claimed[id] {
		return nil, ErrSecretUnavailable
	}
	delete(h.values, id)
	h.claimed[id] = true
	return entry.value, nil
}

// ForgetClaim drops the concurrency guard only after the durable Note has
// been marked handled. A handled Note cannot accept another submission.
func (h *SecretHandoffs) ForgetClaim(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.claimed, id)
}

// Usage reports counts only; the values and their identifiers stay private.
func (h *SecretHandoffs) Usage() (pending int64, largest int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	for _, entry := range h.values {
		pending++
		if int64(len(entry.value)) > largest {
			largest = int64(len(entry.value))
		}
	}
	return pending, largest
}
