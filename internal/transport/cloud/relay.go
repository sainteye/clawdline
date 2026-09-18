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
//     refuses the new request and keeps every one it already took (limits
//     N20). It used to drop the oldest instead, and the person who sent that
//     one was never told: a request is somebody's instruction, and the only
//     honest answer to one that cannot be taken is "busy, try again", sent to
//     the person who asked. The refusal is answered by Service, off this
//     loop, through a small lane of its own; a refusal that cannot get even
//     into that lane is the one case nobody hears about, and it is counted.
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
	// Log is one line per refused request. Nil is silence.
	Log func(format string, args ...any)
	// Depth is how many requests may wait; zero is the register's default.
	Depth int

	once     sync.Once
	requests chan Inbound
	// refusals is the requests the queue turned away, waiting to be told so.
	refusals chan Inbound

	mu sync.Mutex
	// refused is requests turned away at a full queue; unanswered is those
	// of them that could not be told, which are the only ones that reached
	// nobody.
	refused    int
	unanswered int
	lastAt     time.Time
}

// refusalLane is how many turned-away requests may wait for their refusal to
// be sealed and sent. A refusal is quick to answer — no route is asked — so
// this fills only when the relay is sending nothing at all, and then one more
// busy answer would not leave either.
const refusalLane = 16

// ErrRelayNotReady is what Publish answers before Start has run.
var ErrRelayNotReady = errors.New("this relay has no queue yet")

func (r *Relay) start() {
	r.once.Do(func() {
		r.requests = make(chan Inbound, r.depth())
		r.refusals = make(chan Inbound, refusalLane)
	})
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
	// The queue is full. Nothing already in it is let go: the new request is
	// refused, and Service tells its sender so.
	r.mu.Lock()
	r.refused++
	r.lastAt = r.now()
	r.mu.Unlock()
	select {
	case r.refusals <- in:
		r.logf("cloud: the request queue is full (%d waiting); refused a request from %s seq=%d, and its sender is told to retry",
			cap(r.requests), in.Sender, in.Sequence)
	default:
		r.Unanswered(in, "the refusal lane is full as well")
	}
}

// Refused is the requests the queue turned away, for Service to answer.
func (r *Relay) Refused() <-chan Inbound {
	r.start()
	return r.refusals
}

// QueueDepth is how many requests may wait, which a refusal's detail says.
func (r *Relay) QueueDepth() int {
	r.start()
	return cap(r.requests)
}

// Unanswered counts a refused request whose sender could not be told, and
// says why: the one way a request here still reaches nobody.
func (r *Relay) Unanswered(in Inbound, why string) {
	r.mu.Lock()
	r.unanswered++
	r.lastAt = r.now()
	r.mu.Unlock()
	r.logf("cloud: a request from %s seq=%d was refused at a full queue and its sender could not be told: %s",
		in.Sender, in.Sequence, why)
}

// Close ends Service.Run.
func (r *Relay) Close() {
	r.start()
	close(r.requests)
	close(r.refusals)
}

// Refusals is how many requests the queue turned away, and how many of those
// senders were never told, for the status route.
func (r *Relay) Refusals() (refused, unanswered int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refused, r.unanswered
}

// Queue is the `cloud.relay_queue` row: requests waiting now, how many may,
// and what the queue did at its limit — refused, and dropped for the refusals
// nobody heard. A length is a moment's reading of a queue another goroutine
// drains; it is what a gauge is.
func (r *Relay) Queue() (waiting, depth int, counters capacity.Counters) {
	r.start()
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests), cap(r.requests), capacity.Counters{
		Refused: int64(r.refused), Dropped: int64(r.unanswered), LastActionAt: r.lastAt,
	}
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
