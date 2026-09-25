package cloud

// The outbound spool: one queue, one sequence counter, one drain worker.
//
// Ported from `Sources/CloudOutboundSpool.swift` (1,353 lines). Its three
// invariants, in the order they tend to be broken (:10-21):
//
//  1. **A sequence, once reserved, is never reused** — not after a burn, not
//     after a restart, not after coalescing. The counter only moves forward.
//  2. **The queue is not per-channel.** Writes take ready rows in global
//     sequence order; a lower reserved row is never skipped.
//  3. **Sending is durable-first.** A row is marked `sent` *before* the socket
//     write, and a write that throws leaves it `sent`. Whether the peer saw the
//     bytes is unknown, and the row has to say so until an ack correlates it or
//     the attempt window expires. Rolling back to `ready` is the tempting bug:
//     it turns "we do not know" into "it did not happen", and the viewer gets
//     the command twice.
//
// **What this wave does not carry over**: the rows are in memory. The Swift
// spool persists them and then, at open, *burns every one of them* — a reserved
// row because it was never sealed, a sent row because its monotonic instants
// cannot be compared across runs, a stale ready row because its authenticated
// `ts` is past the relay's skew window (:515-546). So the observable difference
// of an in-memory spool is only that a restart loses rows that would have been
// burned anyway, and one that matters: the sequence counter. That one is kept
// on disk by the fence in fence.go, because a restart that reused sequence 0
// would have every viewer's replay window refuse this machine's snapshots.
// docs/cloud-wire.md §15 records this.

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// SpoolChannel is the wire channel kind a row belongs to.
type SpoolChannel string

const (
	SpoolChannelS    SpoolChannel = "s"
	SpoolChannelT    SpoolChannel = "t"
	SpoolChannelOrch SpoolChannel = "orch"
	SpoolChannelCtl  SpoolChannel = "ctl"
	SpoolChannelCtlr SpoolChannel = "ctlr"
	SpoolChannelHo   SpoolChannel = "ho"
)

// IsControl reports the channels that never coalesce and whose ready rows burn
// at restart when their owner is gone (`CloudOutboundSpool.swift:35-36`).
func (c SpoolChannel) IsControl() bool { return c == SpoolChannelCtl || c == SpoolChannelCtlr }

// IsLatestValue reports the channels that carry the whole current value rather
// than a step. Keeping an older never-sent snapshot has no delivery value and
// can only hold the newer one behind it (`CloudOutboundSpool.swift:38-40`).
func (c SpoolChannel) IsLatestValue() bool { return c == SpoolChannelS || c == SpoolChannelOrch }

// SpoolState is a row's place in its life.
type SpoolState string

const (
	SpoolReserved SpoolState = "reserved"
	SpoolReady    SpoolState = "ready"
	SpoolSent     SpoolState = "sent"
	SpoolAcked    SpoolState = "acked"
	SpoolRejected SpoolState = "rejected"
	SpoolBurned   SpoolState = "burned"
)

// IsTerminal reports the states a row never comes back from. A late ack for a
// burned row is telemetry, not a resurrection.
func (s SpoolState) IsTerminal() bool {
	return s == SpoolAcked || s == SpoolRejected || s == SpoolBurned
}

// Burn reasons, `CloudOutboundSpool.swift:56-77`. Every burn names one so that
// a crash-recovery burn can be told from an attempt-window burn without
// guessing.
const (
	BurnStaleReservation   = "stale_reservation"
	BurnAttemptCapExpired  = "attempt_cap_expired"
	BurnStaleReadyAtSend   = "stale_ready_at_restart"
	BurnReplacedByCoalesce = "replaced_by_coalescing"
)

// AttemptTransportUncertain is the marker a burn at the attempt window records:
// the bytes may or may not have arrived (`CloudOutboundSpool.swift:81-83`).
const AttemptTransportUncertain = "transport_uncertain"

// SpoolLimits is the §6.6 table, `CloudOutboundSpool.swift:183-220`. Production
// takes every default.
type SpoolLimits struct {
	GlobalRowCap  int
	GlobalByteCap int

	// RecipientRowCap and RecipientByteCap are what one wire channel may hold.
	// They measure **live** rows only — reserved, ready and sent — because
	// that is what is owed to that recipient. A terminal row's payload is
	// released the moment it becomes terminal (Settle, BurnExpired), so
	// charging its bytes to the channel charges for memory nobody holds, and
	// holds a channel shut for the whole tombstone window (limits N22).
	RecipientRowCap  int
	RecipientByteCap int

	// ReceiptCap is how many terminal rows — tombstones — this spool keeps at
	// once, across every channel. They are receipts, not queue: see
	// TombstoneRetention. Past the cap the oldest is let go early, which is
	// counted, because a receipt that arrives after its tombstone has gone
	// reads as a sequence this machine never sent.
	ReceiptCap int

	// FairnessRecipientRowCap and FairnessRecipientByteCap apply once the
	// spool is 90% full: past that the remaining room is a reserve, and one
	// loud recipient may not take all of it.
	FairnessRecipientRowCap  int
	FairnessRecipientByteCap int

	// AttemptWindow is how long a `sent` row waits for its receipt before it
	// is burned as uncertain. It is the only thing that ever releases a row
	// the socket swallowed.
	AttemptWindow time.Duration
	// StaleReservedAfter is how long a reservation may sit unsealed.
	StaleReservedAfter time.Duration
	// TombstoneRetention is how long a *terminal* row is kept after it is
	// answered. It is not bookkeeping: a relay that answers the same sequence
	// twice, or an ack that overtakes a burn, arrives for a row that is
	// already finished, and a spool that deleted it on the spot cannot tell
	// that from a receipt for a sequence it never sent. The first is a late
	// ack to be counted and ignored; the second is a correlation failure
	// (`CloudOutboundSpool.swift:190`, settle at :946-948).
	//
	// So what it protects is the **sequence number's identity**, and that
	// costs one struct. It is not delivery capacity and is not charged as
	// any: the payload went at Settle, and a tombstone that kept a channel's
	// byte budget for ten minutes was charging a receipt for bytes it had
	// already let go.
	TombstoneRetention time.Duration
	// SealedFrameFreshness is deliberately tighter than the relay's 300-second
	// skew window: a frame sealed 4 minutes ago and sent now would arrive
	// inside the relay's window only by luck (`CloudOutboundSpool.swift:206-208`).
	SealedFrameFreshness time.Duration

	// OutboundWindowRowCap and OutboundWindowByteCap bound how much may be in
	// flight — rows in state `sent`, measured in sealed frame bytes. This is a
	// different budget from the caps above, which are measured in the logical
	// record's canonical byte length.
	OutboundWindowRowCap  int
	OutboundWindowByteCap int

	// RecipientRefusalCap is how many refusals one wire channel may have live
	// at once (the register's `cloud.spool_refusals`). Each refusal answers
	// one specific request — its payload names that request's own read id —
	// so a second refusal on a channel is not a repeat of the first: it is
	// the only answer the second waiter is going to get. The cap is what
	// keeps a flood of refused reads from growing memory; only past it is a
	// refusal dropped, and the caller logs that drop.
	RecipientRefusalCap int
}

// DefaultSpoolLimits is what production uses. The four registered caps — the
// two global ones, the per-channel byte cap and the receipt table — are the
// capacity register's `cloud.spool`, `cloud.spool_bytes`,
// `cloud.spool_channel_bytes` and `cloud.spool_receipts` rows, so each number
// has one spelling and an override can only lower it (LinkOptions).
func DefaultSpoolLimits() SpoolLimits {
	return SpoolLimits{
		GlobalRowCap:             int(capacity.Default(capacity.CloudSpool)),
		GlobalByteCap:            int(capacity.Default(capacity.CloudSpoolBytes)),
		RecipientRowCap:          200,
		RecipientByteCap:         int(capacity.Default(capacity.CloudSpoolChannelBytes)),
		ReceiptCap:               int(capacity.Default(capacity.CloudSpoolReceipts)),
		FairnessRecipientRowCap:  20,
		FairnessRecipientByteCap: 256 << 10,
		AttemptWindow:            30 * time.Second,
		StaleReservedAfter:       60 * time.Second,
		TombstoneRetention:       600 * time.Second,
		SealedFrameFreshness:     240 * time.Second,
		OutboundWindowRowCap:     8,
		OutboundWindowByteCap:    4 << 20,
		RecipientRefusalCap:      int(capacity.Default(capacity.CloudSpoolRefusals)),
	}
}

// spoolRefusalByteLimit is the reserve that lets a full channel say it is
// full. A refusal is the one answer whose delivery cannot be postponed until
// the channel that is blocked has drained, because the channel drains only
// when somebody stops waiting on it — so a row no larger than this is
// admitted past the recipient's own caps, up to RecipientRefusalCap live at
// a time.
//
// The number is the largest typed refusal this transport seals: the payload
// is `{"read","status","error":{code,message,layer,seq,detail}}`, whose every
// field is bounded, and a kibibyte is four times the largest one measured.
const spoolRefusalByteLimit = 4 << 10

// RefusalByteLimit is the reserve a refusal is admitted under. It is not a
// configured field: a limit that could be lowered to nothing would make the
// silence this reserve exists to prevent a setting.
func (l SpoolLimits) RefusalByteLimit() int { return spoolRefusalByteLimit }

// largeAnswerByteLimit and smallAnswerReserveLimit keep the small answers on a
// channel from being starved by the large ones.
//
// Measured on 2026-09-25: every read a phone makes of this machine as a whole
// — the Board, a Session's to-dos, and each reference image on them — answers
// on one channel, `t/<machine>/__clawdline_machine__`. A few full-size
// reference images (971,344 bytes as PNG, more as base64) in flight took that
// channel to 3.2–4.0 MB of its 4 MiB, and the few-kilobyte to-do list behind
// them was refused, again and again, for as long as the pictures were owed.
//
// So an answer larger than largeAnswerByteLimit is refused when admitting it
// would leave less than smallAnswerReserveLimit of the channel free, unless
// the channel holds nothing else (a picture as large as the channel itself
// must still be deliverable on an idle one). An answer at or below the
// threshold may use the reserve. Only a reservation that asks for it
// (ReserveAnswer with headroom) is held to this: a transcript channel's own
// answers are one Session's, and its cap was sized to hold two of its largest
// (limits N22), which a reserve would halve.
const (
	largeAnswerByteLimit    = 256 << 10
	smallAnswerReserveLimit = 1 << 20
)

// Spool refusals. Nothing is evicted to make room for anything
// (`CloudOutboundSpool.swift:597-600`): a live row is somebody's unsent
// instruction, and dropping it to admit a newer one is a silent data loss that
// the sender has no way to notice.
var (
	ErrSpoolCapacity = errors.New("the outbound spool is full")
	ErrSpoolFairness = errors.New("the outbound spool's reserve is for other recipients")
	ErrSpoolRow      = errors.New("no such spool row")
	ErrSpoolReseal   = errors.New("that row is already sealed")
	ErrSpoolEmpty    = errors.New("a sealed envelope may not be empty")
	ErrSpoolSettle   = errors.New("that row has not been sent")
	ErrSpoolMismatch = errors.New("the receipt does not correlate with the row")
	ErrSpoolSequence = errors.New("the sequence counter is outside the safe-integer domain")
	// ErrSpoolRefusalSize and ErrSpoolRefusalPending are the reserve's own
	// two refusals. They are separate from ErrSpoolCapacity because a caller
	// that cannot get a refusal out has a different problem from one that
	// cannot get an answer out, and telling them apart is the difference
	// between "nobody was told" and "somebody already was".
	ErrSpoolRefusalSize = errors.New("that is too large for the refusal reserve")
	// ErrSpoolRefusalPending is a channel that already has
	// RecipientRefusalCap refusals owed to it. It used to be returned for the
	// second one, which left every waiter after the first with no answer.
	ErrSpoolRefusalPending = errors.New("this channel already has as many refusals owed as it may hold")
	// ErrSpoolHeadroom is a large answer refused so that a small one behind
	// it can still get through. It wraps ErrSpoolCapacity, because to the
	// waiter it is the same fact: this channel may not take that now.
	ErrSpoolHeadroom = fmt.Errorf("%w: the rest of this channel is reserved for small answers", ErrSpoolCapacity)
)

// SpoolRow is one outbound record.
type SpoolRow struct {
	Seq     uint64
	Channel SpoolChannel
	// Recipient is the full wire channel string, e.g. "s/mac-01/session-01".
	// Coalescing keys on (Channel, Recipient), not on the kind alone.
	Recipient string
	// LogicalID is what the record is about — a session id, a request id —
	// so a later reader can tell which thing a burned row was.
	LogicalID string
	// ChargedBytes is the logical record's canonical byte length, the figure
	// the account is billed in. It is *not* the sealed frame's size.
	ChargedBytes int
	// Refusal marks a row admitted through the reserve (ReserveRefusal): the
	// small typed answer that tells a waiter its channel is full. It is kept
	// on the row so the next reservation can see that this channel is already
	// being told.
	Refusal bool
	// Sealed is the exact envelope JSON that went, or will go, on the wire.
	// It is dropped once the row is terminal: a 6.5 MB snapshot kept for an
	// answered row is 6.5 MB of nothing.
	Sealed []byte
	// SealedAt is the authenticated `ts` inside Sealed, which is what
	// staleness is measured from — not when this process queued it.
	SealedAt time.Time

	State      SpoolState
	ReservedAt time.Time
	FirstSent  time.Time
	AttemptEnd time.Time
	Outcome    string
	BurnReason string
	// Tombstoned is when the row became terminal. It is what the retention
	// window is measured from.
	Tombstoned time.Time
}

// SettleKind is what a receipt said.
type SettleKind string

const (
	// SettleDelivered is an `ack` with status `delivered`.
	SettleDelivered SettleKind = "delivered"
	// SettleViewerOffline is an `ack` saying nobody was listening. It is only
	// legal on a `ctlr` row: a stream snapshot nobody heard is not an error,
	// and a command response nobody heard is.
	SettleViewerOffline SettleKind = "viewer_offline"
	// SettlePeerError is a `publish_error`, or an `ack` with a refused status.
	SettlePeerError SettleKind = "peer_error"
)

// SettleResult is what settling did.
type SettleResult string

const (
	SettleResultAcked SettleResult = "acked"
	// SettleResultRejected is a terminal refusal from the peer.
	SettleResultRejected SettleResult = "rejected"
	// SettleResultLateIgnored is a receipt for a row that is already terminal.
	// It is counted and ignored; nothing is revived.
	SettleResultLateIgnored SettleResult = "late_ignored"
)

// Disposition is what SendNext found to do.
type Disposition struct {
	// Row is the row to write, when there is one.
	Row *SpoolRow
	// BlockedOnSeal is set when the head of the queue is a reservation nobody
	// has sealed yet. Skipping it would publish sequences out of order.
	BlockedOnSeal bool
	// BlockedOnAck is set when everything ready is behind the in-flight
	// window.
	BlockedOnAck bool
	// HeadSeq names the row that is blocking, when one is.
	HeadSeq uint64
	// Idle is set when there is nothing to do at all.
	Idle bool
}

// Spool is the outbound queue.
type Spool struct {
	mu      sync.Mutex
	limits  SpoolLimits
	rows    map[uint64]*SpoolRow
	nextSeq uint64
	fence   SequenceFence
	now     func() time.Time
	// tally is what the spool did at its limits and with the rows it burned,
	// since this process began (limits N22): a refusal used to reach only the
	// log line of whoever called Publish, and a burn nothing at all.
	tally spoolTally
}

// spoolTally is the spool's account for the capacity register.
type spoolTally struct {
	// refused is reservations turned away at a cap: global, per recipient,
	// or the reserve past 90%.
	refused int64
	// dropped is rows burned before they were ever written: a reservation
	// nobody sealed, a ready row too old to send.
	dropped int64
	// coalesced is never-sent snapshots a newer one for the same recipient
	// replaced. Nothing was lost.
	coalesced int64
	// uncertain is rows written and never answered within their window.
	// Whether they arrived is unknown, so they are neither sent nor dropped.
	uncertain int64
	// expired is tombstones let go before their retention window was up,
	// because the receipt table was full. Each one is a sequence whose late
	// receipt will now read as one this machine never sent.
	expired int64
	// refusalsDropped is refusals turned away because their channel already
	// had RecipientRefusalCap of them owed. Each is a waiter nobody told.
	refusalsDropped int64
	lastAt          time.Time
}

// SequenceFence is the durable high-water mark for the sequence counter. It is
// advanced *before* a reservation is handed out, so a crash between the two
// wastes sequence numbers and never reuses one.
type SequenceFence interface {
	// Reserve promises that no sequence below and including this one will ever
	// be handed out again.
	Reserve(sequence uint64) error
	// Ceiling is the lowest sequence this fence will allow, read at open.
	Ceiling() (uint64, error)
}

// NewSpool returns an empty spool. A nil fence means sequences are not durable,
// which is right for a test and wrong for a machine that reconnects.
func NewSpool(limits SpoolLimits, fence SequenceFence, now func() time.Time) (*Spool, error) {
	if now == nil {
		now = time.Now
	}
	s := &Spool{limits: limits, rows: map[uint64]*SpoolRow{}, fence: fence, now: now}
	if fence != nil {
		ceiling, err := fence.Ceiling()
		if err != nil {
			return nil, err
		}
		s.nextSeq = ceiling
	}
	return s, nil
}

// NextSequence is the sequence the next reservation will take.
func (s *Spool) NextSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq
}

// Reserve takes the next sequence for a record, after the capacity checks.
//
// Every check, the counter move and the insert happen together under one lock,
// as they happen inside one store transaction in the Swift original
// (`CloudOutboundSpool.swift:642-715`). Counting occupancy in one step and
// inserting in another leaves a window where two reservations each see room for
// one.
func (s *Spool) Reserve(channel SpoolChannel, recipient, logicalID string, chargedBytes int) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserveLocked(channel, recipient, logicalID, chargedBytes)
}

// ReserveLatestValue is Reserve for a snapshot channel: every older never-sent
// row for the same channel *and the same exact recipient* is removed first, in
// the same step (`CloudOutboundSpool.swift:619-638`).
//
// `sent` rows are never coalesced. Those bytes are already on the wire and a
// receipt is owed for them.
func (s *Spool) ReserveLatestValue(channel SpoolChannel, recipient, logicalID string, chargedBytes int) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !channel.IsLatestValue() {
		return 0, fmt.Errorf("%s does not carry a latest value", channel)
	}
	for seq, row := range s.rows {
		if row.State == SpoolReady && row.Channel == channel && row.Recipient == recipient {
			delete(s.rows, seq)
			s.tally.coalesced++
		}
	}
	return s.reserveLocked(channel, recipient, logicalID, chargedBytes)
}

// ReserveAnswer is Reserve for an answer to one read, with the small-answer
// reserve held when headroom is set (largeAnswerByteLimit).
func (s *Spool) ReserveAnswer(channel SpoolChannel, recipient, logicalID string, chargedBytes int, headroom bool) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admitLocked(channel, recipient, logicalID, chargedBytes, admission{headroom: headroom})
}

// ReserveRefusal takes a sequence for the answer that says a channel is full.
//
// It is the one admission that may pass the recipient's own caps, and the
// reason is that nothing else can get through them. A channel fills because
// its answers are not being delivered; the person waiting on it is told
// nothing; and the only thing that would make them stop waiting is the
// sentence that cannot be sent. So a row no larger than spoolRefusalByteLimit
// is admitted past them.
//
// **Every refusal, not the first one.** Each carried read waits on its own
// read id, and a refusal's payload names that id; a second refusal on the
// same channel is a different waiter's only answer. Admitting one live
// refusal per channel (the rule until 2026-09-25) told the first waiter and
// left every later one to its sixty-second timeout — measured as "did not fit
// its channel and the refusal did not either" in the daemon log. What bounds
// them now is RecipientRefusalCap live refusals per channel, each at most
// spoolRefusalByteLimit, so a flood of refused reads holds at most
// cap × 4 KiB for one channel.
//
// The global caps still hold. Past those there is no memory to put it in, and
// a machine that admitted one more row to explain why it could not admit a
// row would be spending the last of the budget on the explanation.
func (s *Spool) ReserveRefusal(channel SpoolChannel, recipient, logicalID string, chargedBytes int) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chargedBytes > s.limits.RefusalByteLimit() {
		return 0, fmt.Errorf("%w: a refusal of %d bytes is past the %d-byte reserve",
			ErrSpoolRefusalSize, chargedBytes, s.limits.RefusalByteLimit())
	}
	return s.admitLocked(channel, recipient, logicalID, chargedBytes, admission{refusal: true})
}

func (s *Spool) reserveLocked(channel SpoolChannel, recipient, logicalID string, chargedBytes int) (uint64, error) {
	return s.admitLocked(channel, recipient, logicalID, chargedBytes, admission{})
}

// admission is what kind of reservation admitLocked is deciding.
type admission struct {
	// refusal is a row admitted under the refusal reserve.
	refusal bool
	// headroom holds a large answer to the small-answer reserve.
	headroom bool
}

// admitLocked is every capacity check, the counter move and the insert, under
// one lock, as they happen inside one store transaction in the Swift original.
//
// **What is counted is what is owed.** A row that is reserved, ready or sent
// is an answer somebody is waiting for and holds its bytes; a terminal row is
// a receipt whose payload went at Settle. Counting the second against a cap
// meant for the first is what shut one transcript channel for ten minutes at
// a time while the global budget was at 18% (measured 2026-09-21).
func (s *Spool) admitLocked(channel SpoolChannel, recipient, logicalID string, chargedBytes int, kind admission) (uint64, error) {
	refusal := kind.refusal
	now := s.now()
	s.collectLocked(now)

	rowsBefore, bytesBefore := 0, 0
	recipientRows, recipientBytes, recipientRefusals := 0, 0, 0
	for _, row := range s.rows {
		if row.State.IsTerminal() {
			continue
		}
		rowsBefore++
		bytesBefore += row.ChargedBytes
		if row.Recipient == recipient {
			recipientRows++
			recipientBytes += row.ChargedBytes
			if row.Refusal {
				recipientRefusals++
			}
		}
	}

	if refusal {
		// Every refusal is one waiter's answer, so they are all admitted up
		// to the cap; past it the refusal is refused and counted, and the
		// caller logs whose it was.
		if recipientRefusals >= s.limits.RecipientRefusalCap {
			s.refusedLocked(now)
			s.tally.refusalsDropped++
			return 0, fmt.Errorf("%w: %s holds %d of %d", ErrSpoolRefusalPending, recipient,
				recipientRefusals, s.limits.RecipientRefusalCap)
		}
	} else {
		// The reserve opens at 90% of either global dimension. The threshold
		// is the Swift `(9*cap+9)/10` — a ceiling, so the reserve is never
		// one row wider than it says.
		reserveOpen := rowsBefore >= (9*s.limits.GlobalRowCap+9)/10 ||
			bytesBefore >= (9*s.limits.GlobalByteCap+9)/10
		if reserveOpen {
			if recipientRows+1 > s.limits.FairnessRecipientRowCap ||
				recipientBytes+chargedBytes > s.limits.FairnessRecipientByteCap {
				s.refusedLocked(now)
				return 0, fmt.Errorf("%w: %s holds %d rows", ErrSpoolFairness, recipient, recipientRows)
			}
		}
		if recipientRows+1 > s.limits.RecipientRowCap {
			s.refusedLocked(now)
			return 0, fmt.Errorf("%w: %s holds %d rows of %d", ErrSpoolCapacity,
				recipient, recipientRows, s.limits.RecipientRowCap)
		}
		if recipientBytes+chargedBytes > s.limits.RecipientByteCap {
			s.refusedLocked(now)
			return 0, fmt.Errorf("%w: %s holds %d bytes of %d", ErrSpoolCapacity,
				recipient, recipientBytes, s.limits.RecipientByteCap)
		}
		if kind.headroom && chargedBytes > largeAnswerByteLimit && recipientBytes > 0 &&
			recipientBytes+chargedBytes > s.limits.RecipientByteCap-smallAnswerReserveLimit {
			s.refusedLocked(now)
			return 0, fmt.Errorf("%w: %s holds %d bytes and an answer of %d would leave less than %d free",
				ErrSpoolHeadroom, recipient, recipientBytes, chargedBytes, smallAnswerReserveLimit)
		}
	}
	if rowsBefore+1 > s.limits.GlobalRowCap {
		s.refusedLocked(now)
		return 0, fmt.Errorf("%w: %d rows", ErrSpoolCapacity, rowsBefore)
	}
	if bytesBefore+chargedBytes > s.limits.GlobalByteCap {
		s.refusedLocked(now)
		return 0, fmt.Errorf("%w: %d bytes", ErrSpoolCapacity, bytesBefore)
	}

	seq := s.nextSeq
	// The relay refuses a sequence outside the ECMAScript safe-integer domain
	// (docs/cloud-wire.md §2.1), so running past it is a refusal here rather
	// than a stream of rejected envelopes.
	if seq > 9007199254740991 {
		return 0, ErrSpoolSequence
	}
	if s.fence != nil {
		if err := s.fence.Reserve(seq); err != nil {
			return 0, err
		}
	}
	s.nextSeq = seq + 1
	s.rows[seq] = &SpoolRow{
		Seq:          seq,
		Channel:      channel,
		Recipient:    recipient,
		LogicalID:    logicalID,
		ChargedBytes: chargedBytes,
		Refusal:      refusal,
		State:        SpoolReserved,
		ReservedAt:   now,
	}
	return seq, nil
}

// Seal attaches the exact bytes that will go on the wire and makes the row
// sendable. sealedAt is the envelope's own `ts`, which is what staleness is
// measured from.
func (s *Spool) Seal(seq uint64, sealed []byte, sealedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[seq]
	if !ok {
		return fmt.Errorf("%w: %d", ErrSpoolRow, seq)
	}
	if row.State != SpoolReserved {
		return fmt.Errorf("%w: %d is %s", ErrSpoolReseal, seq, row.State)
	}
	if len(sealed) == 0 {
		return ErrSpoolEmpty
	}
	row.Sealed = sealed
	row.SealedAt = sealedAt
	row.State = SpoolReady
	return nil
}

// SendNext picks the row to write, in global sequence order.
//
// The row is marked `sent` here, before the caller writes it. That is
// invariant 3 and it is the opposite of the instinct to mark it after a
// successful write: a write that fails halfway has still put bytes on a socket
// somebody else may have read.
func (s *Spool) SendNext() Disposition {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.collectLocked(now)
	s.normalizeReadyLocked(now)

	sequences := s.sortedSequencesLocked()

	inflightRows, inflightBytes := 0, 0
	lowestSent := uint64(0)
	haveSent := false
	for _, seq := range sequences {
		if s.rows[seq].State == SpoolSent {
			inflightRows++
			inflightBytes += len(s.rows[seq].Sealed)
			if !haveSent {
				lowestSent, haveSent = seq, true
			}
		}
	}

	for _, seq := range sequences {
		row := s.rows[seq]
		switch row.State {
		case SpoolReserved:
			// A reservation nobody has sealed stops everything below it:
			// publishing a higher sequence first would leave a hole the
			// receiver's replay window can never fill.
			return Disposition{BlockedOnSeal: true, HeadSeq: seq}
		case SpoolReady:
			if inflightRows+1 > s.limits.OutboundWindowRowCap ||
				inflightBytes+len(row.Sealed) > s.limits.OutboundWindowByteCap {
				return Disposition{BlockedOnAck: true, HeadSeq: lowestSent}
			}
			row.State = SpoolSent
			row.FirstSent = now
			row.AttemptEnd = now.Add(s.limits.AttemptWindow)
			copyRow := *row
			return Disposition{Row: &copyRow}
		}
	}
	if haveSent {
		return Disposition{BlockedOnAck: true, HeadSeq: lowestSent}
	}
	return Disposition{Idle: true}
}

// Resend hands back the exact bytes of a row that is already `sent`, for the
// re-send after a reconnect. It changes nothing: no new timestamp, no new
// attempt window, no new sequence.
//
// That is the whole point. A re-send with fresh bytes would be a second
// identity for one command, and the relay's replay window would accept both.
func (s *Spool) Resend(seq uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[seq]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrSpoolRow, seq)
	}
	if row.State != SpoolSent {
		return nil, fmt.Errorf("%w: %d is %s", ErrSpoolSettle, seq, row.State)
	}
	return row.Sealed, nil
}

// InFlight lists, in ascending sequence order, the rows that were written and
// have not been answered. A reconnect re-sends exactly this list, snapshotted
// once so that a row settling mid-drain cannot cause a double write.
func (s *Spool) InFlight() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint64
	for _, seq := range s.sortedSequencesLocked() {
		if s.rows[seq].State == SpoolSent {
			out = append(out, seq)
		}
	}
	return out
}

// Settle records a receipt.
//
// The channel and the full recipient must both match the row. A receipt that
// names a sequence this machine sent on a *different* channel is not a late
// ack, it is a correlation failure, and accepting it would let one channel's
// receipts settle another's rows.
func (s *Spool) Settle(seq uint64, fullChannel string, kind SettleKind) (SettleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[seq]
	if !ok {
		return "", fmt.Errorf("%w: %d", ErrSpoolRow, seq)
	}
	if fullChannel != row.Recipient {
		return "", fmt.Errorf("%w: %d is %s, the receipt says %s", ErrSpoolMismatch, seq, row.Recipient, fullChannel)
	}
	if row.State.IsTerminal() {
		return SettleResultLateIgnored, nil
	}
	if row.State != SpoolSent {
		return "", fmt.Errorf("%w: %d is %s", ErrSpoolSettle, seq, row.State)
	}
	if kind == SettleViewerOffline && row.Channel != SpoolChannelCtlr {
		return "", fmt.Errorf("%w: viewer_offline is not an answer for %s", ErrSpoolMismatch, row.Channel)
	}
	// The sealed bytes go here and not a moment later: a 6.5 MB snapshot held
	// for a row that has been answered is 6.5 MB of nothing
	// (`CloudOutboundSpool.swift:934-945`).
	row.Sealed = nil
	row.Tombstoned = s.now()
	if kind == SettlePeerError {
		row.State = SpoolRejected
		return SettleResultRejected, nil
	}
	row.State = SpoolAcked
	return SettleResultAcked, nil
}

// BurnExpired burns every `sent` row whose attempt window has passed. It is
// the only thing that releases a row the socket swallowed, and the row is
// marked `transport_uncertain` rather than failed: nobody knows whether those
// bytes arrived, and saying either would be a guess.
func (s *Spool) BurnExpired() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var burned []uint64
	for _, seq := range s.sortedSequencesLocked() {
		row := s.rows[seq]
		if row.State != SpoolSent || now.Before(row.AttemptEnd) {
			continue
		}
		row.State = SpoolBurned
		row.BurnReason = BurnAttemptCapExpired
		row.Outcome = AttemptTransportUncertain
		row.Sealed = nil
		row.Tombstoned = now
		burned = append(burned, seq)
	}
	if len(burned) > 0 {
		s.tally.uncertain += int64(len(burned))
		s.tally.lastAt = now
	}
	return burned
}

// Row answers a copy of one row, and whether there is one.
func (s *Spool) Row(seq uint64) (SpoolRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[seq]
	if !ok {
		return SpoolRow{}, false
	}
	return *row, true
}

// ChannelByteLimit is what one wire channel may have owed to it: the
// register's `cloud.spool_channel_bytes`, as this spool is running it.
func (s *Spool) ChannelByteLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits.RecipientByteCap
}

// Rows is how many rows are held, terminal ones included until they are
// collected.
func (s *Spool) Rows() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// normalizeReadyLocked coalesces snapshot rows and drops stale ones, just
// before a send picks a row (`CloudOutboundSpool.swift:1127-1165`).
//
// Coalescing at reserve is not enough on its own: a snapshot may have been
// reserved while a *newer* one was already queued behind an unsealed row, and
// this is where the pair is seen together.
func (s *Spool) normalizeReadyLocked(now time.Time) {
	// Highest sequence wins per (channel, recipient).
	keep := map[string]uint64{}
	sequences := s.sortedSequencesLocked()
	for _, seq := range sequences {
		row := s.rows[seq]
		if row.State != SpoolReady || !row.Channel.IsLatestValue() {
			continue
		}
		key := string(row.Channel) + "\x00" + row.Recipient
		if prior, ok := keep[key]; ok {
			s.rows[prior].State = SpoolBurned
			s.rows[prior].BurnReason = BurnReplacedByCoalesce
			s.rows[prior].Sealed = nil
			s.rows[prior].Tombstoned = now
			s.tally.coalesced++
		}
		keep[key] = seq
	}
	for _, seq := range sequences {
		row := s.rows[seq]
		if row.State != SpoolReady {
			continue
		}
		if !row.SealedAt.IsZero() && now.Sub(row.SealedAt) > s.limits.SealedFrameFreshness {
			// The relay would measure this frame's `ts` against its own clock
			// and refuse it; refusing it here costs one round trip less and
			// says why.
			row.State = SpoolBurned
			row.BurnReason = BurnStaleReadyAtSend
			row.Sealed = nil
			row.Tombstoned = now
			s.tally.dropped++
			s.tally.lastAt = now
		}
	}
}

// collectLocked drops rows that are past their tombstone retention and burns
// reservations nobody sealed.
//
// A terminal row is **not** deleted on the spot. It is a tombstone for
// TombstoneRetention, so that a second receipt for the same sequence is
// answered "late, ignored" rather than "no such row" — which is what a
// receipt for a sequence this machine never sent should mean, and only that.
func (s *Spool) collectLocked(now time.Time) {
	held := 0
	for seq, row := range s.rows {
		switch {
		case row.State.IsTerminal():
			if !row.Tombstoned.IsZero() && now.Sub(row.Tombstoned) > s.limits.TombstoneRetention {
				delete(s.rows, seq)
				continue
			}
			held++
		case row.State == SpoolReserved && now.Sub(row.ReservedAt) > s.limits.StaleReservedAfter:
			row.State = SpoolBurned
			row.BurnReason = BurnStaleReservation
			row.Tombstoned = now
			s.tally.dropped++
			s.tally.lastAt = now
			held++
		}
	}
	s.expireReceiptsLocked(now, held)
}

// expireReceiptsLocked bounds the receipt table, oldest first.
//
// The tombstones are the only thing here that is not queue, so they are the
// only thing here that may be let go while it is still wanted: past the cap
// the oldest receipt goes, and what is lost is the ability to tell a late ack
// for that sequence from a receipt for a sequence never sent. That is counted
// as an expiry rather than passed over, because it is the one case where this
// spool answers a correlation question worse than it did a moment ago.
func (s *Spool) expireReceiptsLocked(now time.Time, held int) {
	over := held - s.limits.ReceiptCap
	if s.limits.ReceiptCap <= 0 || over <= 0 {
		return
	}
	terminal := make([]uint64, 0, held)
	for seq, row := range s.rows {
		if row.State.IsTerminal() {
			terminal = append(terminal, seq)
		}
	}
	// Tombstoned order is sequence order: a row becomes terminal after it was
	// reserved, and a sequence is never reused.
	sort.Slice(terminal, func(i, j int) bool {
		return s.rows[terminal[i]].Tombstoned.Before(s.rows[terminal[j]].Tombstoned)
	})
	for _, seq := range terminal[:min(over, len(terminal))] {
		delete(s.rows, seq)
		s.tally.expired++
	}
	s.tally.lastAt = now
}

func (s *Spool) refusedLocked(now time.Time) {
	s.tally.refused++
	s.tally.lastAt = now
}

// Readings are the spool's two global capacity rows, `cloud.spool` and
// `cloud.spool_bytes`: what a reservation is measured against — the rows
// still owed delivery and their charged bytes — and what the spool did at its
// limits. The counters are the same on both rows, because one refusal or burn
// is one event whichever cap it met. A written row burned at its attempt
// window is said in the note and counted nowhere else: whether it arrived is
// unknown, and unknown is neither.
//
// **Tombstones are not in this figure any more.** They were, and the gauge
// therefore read 18% full while one channel had been shut for ten minutes;
// they are `cloud.spool_receipts` now, which is what they are.
func (s *Spool) Readings() (rows, bytes capacity.Reading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var live, charged, tombstones int64
	for _, row := range s.rows {
		if row.State.IsTerminal() {
			tombstones++
			continue
		}
		live++
		charged += int64(row.ChargedBytes)
	}
	counters := capacity.Counters{
		Refused: s.tally.refused, Dropped: s.tally.dropped, Coalesced: s.tally.coalesced,
		LastActionAt: s.tally.lastAt,
	}
	note := fmt.Sprintf("%d answered row(s) are held as receipts and are not counted here", tombstones)
	if s.tally.uncertain > 0 {
		note += fmt.Sprintf("; %d written answer(s) went unanswered past their window and were let go, and whether they arrived is unknown",
			s.tally.uncertain)
	}
	rows = capacity.Reading{Known: true, Used: live, Counters: counters, Note: note}
	bytes = capacity.Reading{Known: true, Used: charged, Counters: counters, Note: note}
	return rows, bytes
}

// ChannelReadings are the spool's other two rows.
//
// `cloud.spool_channel_bytes` is the bound that actually refuses a Cloud
// answer in practice, and it is per channel, so the gauge is the **fullest**
// channel: the one number that predicts the next refusal. The channel is not
// named — a `t/<machine>/<session>` names a session of the person's, and this
// reading is read by a notice as well as by diagnostics — so the note says
// how many channels are holding anything and how full the worst of them is.
//
// `cloud.spool_receipts` is the tombstone table: terminal rows kept for
// TombstoneRetention so that a second receipt for a sequence is answered
// "late, ignored" rather than "no such row". Its `Expired` counter is the
// receipts let go early because the table was full.
func (s *Spool) ChannelReadings() (channelBytes, receipts capacity.Reading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	perChannel := map[string]int64{}
	var tombstones int64
	for _, row := range s.rows {
		if row.State.IsTerminal() {
			tombstones++
			continue
		}
		perChannel[row.Recipient] += int64(row.ChargedBytes)
	}
	var fullest int64
	for _, held := range perChannel {
		if held > fullest {
			fullest = held
		}
	}
	counters := capacity.Counters{
		Refused: s.tally.refused, LastActionAt: s.tally.lastAt,
	}
	channelBytes = capacity.Reading{Known: true, Used: fullest, Counters: counters,
		Note: fmt.Sprintf("the fullest of %d channel(s) with anything owed", len(perChannel))}
	receipts = capacity.Reading{Known: true, Used: tombstones,
		WindowSeconds: int64(s.limits.TombstoneRetention / time.Second),
		Counters:      capacity.Counters{Expired: s.tally.expired, LastActionAt: s.tally.lastAt}}
	return channelBytes, receipts
}

// RefusalReading is `cloud.spool_refusals`: the live refusals owed to the
// fullest channel, which is the number that predicts the next dropped one,
// and how many were dropped at the cap. The channel is not named, for the
// reason ChannelReadings gives.
func (s *Spool) RefusalReading() capacity.Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	perChannel := map[string]int64{}
	for _, row := range s.rows {
		if row.Refusal && !row.State.IsTerminal() {
			perChannel[row.Recipient]++
		}
	}
	var fullest int64
	for _, held := range perChannel {
		fullest = max(fullest, held)
	}
	return capacity.Reading{Known: true, Used: fullest,
		Counters: capacity.Counters{Refused: s.tally.refusalsDropped, LastActionAt: s.tally.lastAt},
		Note:     fmt.Sprintf("the fullest of %d channel(s) with a refusal owed", len(perChannel))}
}

func (s *Spool) sortedSequencesLocked() []uint64 {
	out := make([]uint64, 0, len(s.rows))
	for seq := range s.rows {
		out = append(out, seq)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
