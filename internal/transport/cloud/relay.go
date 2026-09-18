package cloud

// The relay, behind the seam.
//
// `Service` (cloud.go) was written against an interface so that the operation
// half could be built before a socket existed, and `Fake` is the other
// implementation. This is the real one: requests arrive as decrypted envelopes
// from `internal/adapters/cloud`'s transport, and an answer is reserved on the
// spool, sealed once, and handed to the drain worker that owns the socket.
//
// Two rules are carried over from the connection half and are the reason this
// file is not three lines:
//
//   - **The inbound callback must not block.** It is called on the socket's
//     read loop, so a slow bridge would stop the line reading acks and
//     keepalives. Requests go into a buffered channel, and a full channel
//     drops the oldest rather than the newest: a viewer that re-sends is
//     asking for the *latest* thing it wanted, and a queue that answers a
//     minute of stale commands after a stall is worse than one that says it
//     dropped them.
//   - **A sequence is spent when it is sealed.** The spool hands one out, the
//     envelope is sealed against it, and a re-send re-sends those exact bytes.
//     Nothing here re-seals.

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// How many decrypted requests may wait for the bridge is the capacity
// register's `cloud.relay_queue` row: 64 unless an override lowers it.
//
// It is small on purpose. A Cloud request is a person tapping something, and a
// hundred of them behind a bridge that has stopped answering is not a backlog
// worth keeping — it is a minute of stale taps that will all fire at once.

// Relay is the Transport implementation backed by a real relay connection.
type Relay struct {
	// Transport is the live line. Publish writes through it.
	Transport *adaptercloud.Transport
	// Spool hands out sequences and owns the re-send window.
	Spool *adaptercloud.Spool
	// MachineID is the `sender` every answer is signed as.
	MachineID string
	// Signer and Secret seal the answer: the device key signs, the account
	// master secret encrypts. The two roles are separate and must not be
	// conflated (`domain/cloud/keys.go`).
	Signer domaincloud.DeviceKey
	Secret domaincloud.ContentKey
	// KeyID is the content key's id on the wire, `ms-1`.
	KeyID string
	// Now exists so a test can drive the clock.
	Now func() time.Time
	// Log is one line per dropped request. Nil is silence.
	Log func(format string, args ...any)
	// Depth is how many requests may wait; zero is the register's default.
	Depth int

	once     sync.Once
	requests chan Inbound

	mu      sync.Mutex
	dropped int
}

// ErrRelayNotReady is what Publish answers before Start has run.
var ErrRelayNotReady = errors.New("this relay has no queue yet")

func (r *Relay) start() {
	r.once.Do(func() { r.requests = make(chan Inbound, r.depth()) })
}

func (r *Relay) depth() int {
	if r.Depth > 0 {
		return r.Depth
	}
	return int(capacity.Default(capacity.CloudRelayQueue))
}

// Requests is the channel Service.Run reads.
func (r *Relay) Requests() <-chan Inbound {
	r.start()
	return r.requests
}

// Deliver is what the transport's Inbound callback calls. It never blocks.
func (r *Relay) Deliver(envelope domaincloud.Envelope, plaintext []byte) {
	r.start()
	in := Inbound{
		Channel:   envelope.Ch,
		Class:     string(envelope.Class),
		Sender:    envelope.Sender,
		Sequence:  envelope.Seq,
		Plaintext: plaintext,
	}
	select {
	case r.requests <- in:
		return
	default:
	}
	// The queue is full. Make room by discarding the oldest, then take the new
	// one; if another goroutine emptied it in between, the send below still
	// succeeds without blocking because this is the only producer.
	select {
	case old := <-r.requests:
		r.count()
		r.logf("cloud: the request queue was full; dropped a request from %s seq=%d", old.Sender, old.Sequence)
	default:
	}
	select {
	case r.requests <- in:
	default:
		r.count()
		r.logf("cloud: the request queue was full; dropped a request from %s seq=%d", in.Sender, in.Sequence)
	}
}

// Close ends Service.Run.
func (r *Relay) Close() {
	r.start()
	close(r.requests)
}

// Dropped is how many requests never reached the bridge, for the status route.
func (r *Relay) Dropped() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Queue is the `cloud.relay_queue` row: requests waiting now, how many may,
// and how many never reached the bridge. A length is a moment's reading of a
// queue another goroutine drains; it is what a gauge is.
func (r *Relay) Queue() (waiting, depth, dropped int) {
	r.start()
	return len(r.requests), cap(r.requests), r.Dropped()
}

func (r *Relay) count() {
	r.mu.Lock()
	r.dropped++
	r.mu.Unlock()
}

// Publish seals one answer and queues it on the spool.
//
// It does not write to the socket: the spool's drain worker does, and it is the
// one writer so that a reconnect re-sends what was never answered in sequence
// order. What this returns is whether the answer was *accepted for delivery*,
// which is a different fact from whether it arrived — the relay's ack is, and
// the status counters carry it.
func (r *Relay) Publish(ctx context.Context, out Outbound) error {
	if r.Transport == nil || r.Spool == nil {
		return ErrRelayNotReady
	}
	classes, ok := domaincloud.ChannelClasses(out.Channel)
	if !ok || len(classes) == 0 {
		return errors.New("that is not a channel this build knows: " + out.Channel)
	}
	if err := domaincloud.ProducibleChannel(out.Channel); err != nil {
		return err
	}
	class := domaincloud.Class(out.Class)
	if out.Class == "" {
		class = classes[0]
	}

	kind := adaptercloud.SpoolChannel(strings.SplitN(out.Channel, "/", 2)[0])
	var (
		seq uint64
		err error
	)
	// A `t/` answer is a command's reply, not a snapshot: two answers to two
	// different reads on one session are two facts, and coalescing them would
	// settle one waiter with the other's payload. Only `s/` and `orch/` carry
	// a whole current value, and only those are reserved as latest-value.
	if kind.IsLatestValue() {
		seq, err = r.Spool.ReserveLatestValue(kind, out.Channel, out.Channel, len(out.Payload))
	} else {
		seq, err = r.Spool.Reserve(kind, out.Channel, out.Channel, len(out.Payload))
	}
	if err != nil {
		return err
	}

	now := r.now()
	envelope, err := domaincloud.Seal(out.Payload, domaincloud.SealParams{
		Ch:     out.Channel,
		Seq:    seq,
		Ts:     uint64(now.UnixMilli()),
		Class:  class,
		KeyID:  r.keyID(),
		Sender: r.MachineID,
		Key:    r.Secret,
		Signer: r.Signer,
		Rand:   rand.Reader,
	})
	if err != nil {
		return err
	}
	sealed, err := envelope.CanonicalJSON()
	if err != nil {
		return err
	}
	return r.Spool.Seal(seq, sealed, now)
}

func (r *Relay) keyID() string {
	if r.KeyID != "" {
		return r.KeyID
	}
	return adaptercloud.MasterKeyID
}

func (r *Relay) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Relay) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}
