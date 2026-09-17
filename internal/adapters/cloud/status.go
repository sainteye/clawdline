package cloud

// What the line is doing, in words a person and a status route can both read.
//
// The vocabulary is the Swift app's, `CloudStatus.swift:171-193`: `idle`,
// `connected`, `reconnecting`, `stopped`. There is deliberately no
// `connecting` — the first dial shows `idle` until `ready` lands, because a
// state that flickers for 200 ms is a state nobody can act on.
//
// The counters are process-lifetime and are **not** reset by a reconnect
// (`CloudStatus.swift:23-25`). `CountingSince` says when they started, which
// is the only way "12 drops" means anything.

import (
	"sync"
	"time"
)

// Transport states as the rest of the app sees them.
const (
	StateIdle         = "idle"
	StateConnected    = "connected"
	StateReconnecting = "reconnecting"
	StateStopped      = "stopped"
)

// Status is a snapshot of the line.
type Status struct {
	State string `json:"state"`
	// LastClose is the FailureCode of whatever ended the last connection.
	LastClose string `json:"last_close,omitempty"`
	// ConnectedSince is when the current socket became ready; zero when not
	// connected.
	ConnectedSince time.Time `json:"connected_since,omitempty"`
	// TokenExpiresAt and NextRotationAt make the four-minute reconnect
	// rhythm legible instead of mysterious.
	TokenExpiresAt time.Time `json:"token_expires_at,omitempty"`
	NextRotationAt time.Time `json:"next_rotation_at,omitempty"`

	Account  string `json:"account,omitempty"`
	Machine  string `json:"machine,omitempty"`
	RelayURL string `json:"relay_url,omitempty"`
	// Enabled is false whenever the settings switch is off, whatever else is
	// true. It is first because it is the answer to "why is nothing
	// happening" nine times out of ten.
	Enabled bool `json:"enabled"`

	CountingSince time.Time `json:"counting_since"`
	Connects      int       `json:"connects"`
	Reconnects    int       `json:"reconnects"`
	Published     int       `json:"published"`
	Acked         int       `json:"acked"`
	PublishErrors int       `json:"publish_errors"`
	InboundTotal  int       `json:"inbound_total"`
	// InboundDropped is keyed by the reasons in docs/cloud-wire.md §9.4.
	InboundDropped map[string]int `json:"inbound_dropped,omitempty"`
}

// Inbound drop reasons, docs/cloud-wire.md §9.4
// (`CloudTransport.swift:395-410`). `roster_unreadable` and `key_unreadable`
// are deliberately not folded into `unknown_sender`: a key store that cannot
// be read used to produce an empty roster, which reads exactly like "this
// device was never paired", and the two call for opposite responses.
const (
	DropMalformed        = "envelope_malformed"
	DropWrongChannel     = "wrong_channel"
	DropUnknownSender    = "unknown_sender"
	DropRosterUnreadable = "roster_unreadable"
	DropKeyIDMismatch    = "key_id_mismatch"
	DropKeyUnreadable    = "key_unreadable"
	DropBadSignature     = "bad_signature"
	DropDecryptFailed    = "decrypt_failed"
	DropReplay           = "replay"
	DropReplayWindowFull = "replay_window_full"
)

// StatusRecorder collects the snapshot. It uses a plain mutex rather than a
// channel or a goroutine because the receive loop reports drops inline and has
// to return immediately: 790 drops must not become 790 goroutines
// (`CloudStatus.swift:19-21`).
type StatusRecorder struct {
	mu     sync.Mutex
	status Status
}

// NewStatusRecorder returns a recorder counting from now.
func NewStatusRecorder(now time.Time) *StatusRecorder {
	return &StatusRecorder{status: Status{
		State:          StateIdle,
		CountingSince:  now,
		InboundDropped: map[string]int{},
	}}
}

// Snapshot answers a copy.
func (r *StatusRecorder) Snapshot() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.status
	out.InboundDropped = make(map[string]int, len(r.status.InboundDropped))
	for k, v := range r.status.InboundDropped {
		out.InboundDropped[k] = v
	}
	return out
}

// SetEnabled records whether the settings switch is on.
func (r *StatusRecorder) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Enabled = enabled
}

// SetIdentity records who this machine is on the wire.
func (r *StatusRecorder) SetIdentity(account, machine, relayURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Account, r.status.Machine, r.status.RelayURL = account, machine, relayURL
}

// Ready records a connection becoming usable.
func (r *StatusRecorder) Ready(now, tokenExpiry time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = StateConnected
	r.status.ConnectedSince = now
	r.status.TokenExpiresAt = tokenExpiry
	if !tokenExpiry.IsZero() {
		rotation := tokenExpiry.Add(-RefreshAhead)
		if rotation.Before(now) {
			rotation = tokenExpiry
		}
		r.status.NextRotationAt = rotation
	}
	r.status.Connects++
}

// Reconnecting records a connection ending and another being attempted.
func (r *StatusRecorder) Reconnecting(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = StateReconnecting
	r.status.ConnectedSince = time.Time{}
	r.status.LastClose = reason
	r.status.Reconnects++
}

// Stopped records the line giving up.
func (r *StatusRecorder) Stopped(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = StateStopped
	r.status.ConnectedSince = time.Time{}
	r.status.LastClose = reason
}

// Idle records the line being off.
func (r *StatusRecorder) Idle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = StateIdle
	r.status.ConnectedSince = time.Time{}
}

// Published counts one envelope written to the socket.
func (r *StatusRecorder) Published() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Published++
}

// Acked counts one ack.
func (r *StatusRecorder) Acked() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Acked++
}

// PublishError counts one publish_error.
func (r *StatusRecorder) PublishError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.PublishErrors++
}

// Inbound counts one accepted envelope.
func (r *StatusRecorder) Inbound() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.InboundTotal++
}

// Dropped counts one refused envelope under one of the §9.4 reasons.
func (r *StatusRecorder) Dropped(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.InboundDropped[reason]++
}
