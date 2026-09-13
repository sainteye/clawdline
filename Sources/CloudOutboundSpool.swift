import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

/// The Mac's durable outbound spool for the cloud protocol (design §6.1, §6.2, §6.6).
///
/// Every byte the Mac publishes — s/t/orch stream frames and ctlr control responses — passes
/// through one spool with one shared sequence counter, drained by one independently owned
/// publisher worker. The spool's three invariants, in the order they tend to be violated:
///
/// 1. **A seq, once reserved, is never reused** (§6.1.1) — not after a burn, not after a crash,
///    not after coalescing. The counter only moves forward, and recovery burns rather than
///    deletes, so the history of what was reserved is never rewritten.
/// 2. **The queue is not per-channel** (§6.1.3). Socket writes take ready rows in global seq
///    order. A reserved/ready lower seq is never skipped; an already-written `sent` row may wait
///    for its receipt while later ready rows fill the bounded durable window.
/// 3. **Sending is durable-first** (§6.1.4): the row is committed as `sent` before the socket
///    write, and a throwing send leaves it `sent`. Whether the peer saw the bytes is unknown,
///    and the row's state must say so until an ack correlates or the attempt window expires.
///
/// Persistence and both clocks are injected; the unit performs no I/O of its own beyond the
/// store protocol, and depends only on Foundation plus `CloudCanonicalJSON` (compiled
/// alongside) for the §6.6 `charged_bytes` arithmetic.

// MARK: - Channels and row lifecycle

/// Wire channels the spool can carry. `s`/`t`/`orch` are stream channels; `ctl` and `ctlr`
/// are the control request/response channels, which never coalesce (§6.1.5) and whose ready
/// rows burn at restart when their owner is gone (§6.2). Snapshot coalescing is defined only
/// for the state-snapshot channel `s`.
public enum CloudSpoolChannel: String, CaseIterable, Codable, Equatable, Sendable {
    case s, t, orch, ctl, ctlr

    /// ctl/ctlr — the channels §6.1.5 says never coalesce and §6.2 burns when ownerless.
    public var isControl: Bool { self == .ctl || self == .ctlr }

    /// State and orchestrator snapshots carry the complete current value. Keeping an older
    /// never-sent value has no delivery value and can only delay the newer snapshot behind it.
    public var isLatestValue: Bool { self == .s || self == .orch }
}

/// §6.1 row states. `acked`, `rejected` and `burned` are terminal; a terminal row never
/// becomes live again (§6.2: late acks for burned rows are telemetry, not resurrection).
public enum CloudSpoolRowState: String, CaseIterable, Codable, Equatable, Sendable {
    case reserved, ready, sent, acked, rejected, burned

    public var isTerminal: Bool {
        self == .acked || self == .rejected || self == .burned
    }
}

/// Why a row was burned. Burns are the only path from a live state to `burned`, and every
/// call site names its reason so tests and telemetry can tell a crash-recovery burn from an
/// attempt-cap burn without guessing.
public enum CloudSpoolBurnReason: String, Codable, Equatable, Sendable {
    /// §6.1.2 — a crash left a reserved row with no sealed bytes; recovery burns it durably
    /// before any higher seq is processed.
    case recoveredUnsealedReservation
    /// §6.6 GC — a reservation that was never sealed within the stale-reservation window.
    case staleReservation
    /// §6.2 — the attempt window (first_sent + 30s) elapsed with no correlated ack/error.
    case attemptCapExpired
    /// §6.2 — after restart old continuous instants cannot be compared, so every sent row
    /// burns to release its durable window slot.
    case restartSentUncertain
    /// §6.2 — a ctl/ctlr ready row whose owner did not survive the restart.
    case ownerlessControlAtRestart
    /// A never-sent ready row whose authenticated timestamp is no longer fresh. The historical
    /// raw-value name is retained so a pre-window binary can still decode a store after rollback;
    /// the reason now applies both at restart and immediately before a live socket send.
    case staleReadyAtRestart
    /// §6.1.5 — the old snapshot row burned by coalescing, before the replacement reserve.
    case replacedByCoalescing
}

/// §6.2's attempt marker: a burn at the attempt cap (or the restart burn of a sent row)
/// records that transport delivery is uncertain — the bytes may or may not have arrived.
public enum CloudSpoolAttemptOutcome: String, Codable, Equatable, Sendable {
    case transportUncertain = "transport_uncertain"
}

/// One `outbound` row (§6.1). `logicalRecordCanonicalBytes` is the RFC 8785 canonical
/// serialization of the exact logical record (minus `charged_bytes` itself) fixed at reserve
/// time; `chargedBytes` is its byteLength and is recomputed on every read of the store —
/// a mismatch is corruption and fails closed (§6.6).
public struct CloudSpoolRow: Equatable, Sendable {
    public let seq: Int64
    public let channel: CloudSpoolChannel
    public let logicalID: String
    public let ownerID: String?
    public let recipient: String
    public internal(set) var logicalRecordCanonicalBytes: Data
    public internal(set) var chargedBytes: Int
    public internal(set) var sealedEnvelopeBytes: Data?
    public internal(set) var state: CloudSpoolRowState
    /// Wall-clock reservation timestamp — the only cross-restart-comparable time on the row.
    public let reservedAt: Date
    /// Continuous-clock reservation instant; meaningful only within the process run that
    /// reserved it (recovery burns every reserved row precisely because this cannot be
    /// compared across restarts).
    public internal(set) var reservedAtContinuous: Duration
    public internal(set) var firstSentContinuous: Duration?
    public internal(set) var attemptNotAfterContinuous: Duration?
    public internal(set) var attemptOutcome: CloudSpoolAttemptOutcome?
    public internal(set) var burnReason: CloudSpoolBurnReason?
    /// §6.2 — set when the logical layer tombstones the record; the terminal transport row
    /// is retained for `tombstoneRetention` after this instant, then GC may delete it.
    public internal(set) var logicalTombstoneContinuous: Duration?

    public init(
        seq: Int64, channel: CloudSpoolChannel, logicalID: String, ownerID: String?,
        recipient: String, logicalRecordCanonicalBytes: Data, chargedBytes: Int,
        sealedEnvelopeBytes: Data?, state: CloudSpoolRowState, reservedAt: Date,
        reservedAtContinuous: Duration, firstSentContinuous: Duration?,
        attemptNotAfterContinuous: Duration?, attemptOutcome: CloudSpoolAttemptOutcome?,
        burnReason: CloudSpoolBurnReason?, logicalTombstoneContinuous: Duration?
    ) {
        self.seq = seq
        self.channel = channel
        self.logicalID = logicalID
        self.ownerID = ownerID
        self.recipient = recipient
        self.logicalRecordCanonicalBytes = logicalRecordCanonicalBytes
        self.chargedBytes = chargedBytes
        self.sealedEnvelopeBytes = sealedEnvelopeBytes
        self.state = state
        self.reservedAt = reservedAt
        self.reservedAtContinuous = reservedAtContinuous
        self.firstSentContinuous = firstSentContinuous
        self.attemptNotAfterContinuous = attemptNotAfterContinuous
        self.attemptOutcome = attemptOutcome
        self.burnReason = burnReason
        self.logicalTombstoneContinuous = logicalTombstoneContinuous
    }
}

// MARK: - Injected persistence, clock, limits

/// The durable state: the never-reused counter plus every outbound row, ascending by seq.
public struct CloudSpoolPersistedState: Equatable, Sendable {
    public var nextSeq: Int64
    public var rows: [CloudSpoolRow]

    public init(nextSeq: Int64 = 0, rows: [CloudSpoolRow] = []) {
        self.nextSeq = nextSeq
        self.rows = rows
    }
}

/// Injected persistence. Each `commit` is one atomic transaction: the caller hands over the
/// complete next state, and the store either persists all of it or throws leaving the previous
/// state intact. That is what lets every §6.6 cap check, counter movement and insert share a
/// single transaction, and what makes a commit failure fail closed rather than half-open.
public protocol CloudSpoolStore: AnyObject, Sendable {
    func load() throws -> CloudSpoolPersistedState
    func commit(_ state: CloudSpoolPersistedState) throws
}

/// A rollback-compatible predecessor may own a second on-disk high-water mark. The fence is
/// advanced before the new spool commits a reservation, so a failed new-store commit can waste
/// sequence numbers but can never let an older image reuse one the new image returned.
public protocol CloudSpoolSequenceFence: AnyObject, Sendable {
    func prepareToReserve(sequence: Int64) throws
}

public final class CloudNoopSpoolSequenceFence: CloudSpoolSequenceFence, @unchecked Sendable {
    public init() {}
    public func prepareToReserve(sequence _: Int64) throws {}
}

/// Injected clocks (§6.1: the Mac's `continuous` is an injected ContinuousClock, never read
/// directly). `continuousNow` is monotonic within one process run and incomparable across
/// runs; `wallNow` is the wall clock used for `reservedAt` freshness at restart.
public protocol CloudSpoolClock: Sendable {
    var continuousNow: Duration { get }
    var wallNow: Date { get }
}

/// The §6.6 table for the Mac outbound spool, as defaults, plus the constants the quoted
/// design text uses without numbering. Caps compare in `charged_bytes` (the §6.6 figure),
/// not sealed-envelope size.
public struct CloudSpoolLimits: Sendable {
    /// Global hard cap: 2,000 rows.
    public var globalRowCap = 2000
    /// Global hard cap: 16 MiB of charged bytes.
    public var globalByteCap = 16 * 1024 * 1024
    /// Per-recipient (actor) hard cap: 200 rows.
    public var recipientRowCap = 200
    /// Per-recipient (actor) hard cap: 2 MiB.
    public var recipientByteCap = 2 * 1024 * 1024
    /// Fairness reserve once global occupancy reaches 90%: a candidate's recipient may hold
    /// at most 20 rows after commit.
    public var fairnessRecipientRowCap = 20
    /// Fairness reserve: at most 256 KiB per recipient after commit.
    public var fairnessRecipientByteCap = 256 * 1024
    /// §6.2: attempt_not_after_continuous = first_sent + 30s, fixed at first send.
    public var attemptWindow = Duration.seconds(30)
    /// §6.2: terminal transport rows are retained until 10 minutes after the logical tombstone.
    public var tombstoneRetention = Duration.seconds(600)
    /// §6.6: GC runs a bounded batch before each admission and at least every 60 seconds.
    public var gcInterval = Duration.seconds(60)
    /// §6.6 "stale reserved先burn": how long a reservation may sit unsealed within one run
    /// before GC burns it. The quoted design text names no number; this is a local constant.
    public var staleReservedAfter = Duration.seconds(60)
    /// The relay defaults to a 300-second authenticated-envelope skew window. Selection uses a
    /// deliberately tighter local limit so socket write and relay queueing cannot consume the
    /// whole peer allowance. A typed clock_skew refusal remains the deployment-drift signal.
    public var sealedFrameFreshnessSeconds: TimeInterval = 240
    /// Upper bound on terminal-row deletions per GC batch (bounded batch, §6.6).
    public var gcBatchLimit = 256
    /// Maximum durable sent rows awaiting settlement. This is an implementation default pending
    /// W6 measurement, not an approved production budget.
    public var outboundWindowRowCap = 8
    /// Maximum exact sealed-frame bytes across durable sent rows. This is an implementation
    /// default pending W6 measurement, not an approved production budget.
    public var outboundWindowByteCap = 4 * 1024 * 1024

    public init() {}
}

// MARK: - Metrics (label domains closed at the type level)

/// The five §6.6 metric names, exactly. The protocol methods below each map to one of these;
/// the enum exists so the names live in one place and the test can pin their spelling.
public enum CloudSpoolMetricName: String, CaseIterable, Sendable {
    case rows = "cloud_spool_rows"
    case bytes = "cloud_spool_bytes"
    case admissionRefusalsTotal = "cloud_spool_admission_refusals_total"
    case stateStoreGCTotal = "state_store_gc_total"
    case stateStoreCorruptTotal = "state_store_corrupt_total"
}

/// `store` label domain shared by the §6.6 state-store counters.
public enum CloudStateStore: String, CaseIterable, Equatable, Sendable {
    case ctlSeen = "ctl_seen"
    case macSpool = "mac_spool"
    case viewerSpool = "viewer_spool"
    case ctlrPending = "ctlr_pending"
    case ctlrSeen = "ctlr_seen"
}

/// `reason` label domain for `state_store_gc_total`.
public enum CloudStateStoreGCReason: String, CaseIterable, Equatable, Sendable {
    case expired, revoked, terminal, cancel
    case heartbeatTimeout = "heartbeat_timeout"
}

/// `runtime` label domain — exactly mac|viewer.
public enum CloudSpoolRuntime: String, CaseIterable, Sendable {
    case mac, viewer
}

/// `reason` label domain — exactly row_cap|byte_cap|fairness|corrupt|gc_failed|per_request_cap.
/// (`per_request_cap` is part of the closed domain; the Mac spool's own admission path never
/// emits it.)
public enum CloudSpoolRefusalReason: String, CaseIterable, Equatable, Sendable {
    case rowCap = "row_cap"
    case byteCap = "byte_cap"
    case fairness
    case corrupt
    case gcFailed = "gc_failed"
    case perRequestCap = "per_request_cap"
}

/// `scope` label domain — exactly global|actor|machine|owner|recipient.
public enum CloudSpoolRefusalScope: String, CaseIterable, Equatable, Sendable {
    case global, actor, machine, owner, recipient
}

/// Which occupancy dimension crossed a threshold.
public enum CloudSpoolOccupancyDimension: String, CaseIterable, Sendable {
    case rows, bytes
}

/// §6.6: 80% of either cap is a warning, 90% is a page.
public enum CloudSpoolOccupancyAlert: Int, Equatable, Comparable, Sendable {
    case warning80 = 80
    case page90 = 90

    public static func < (lhs: Self, rhs: Self) -> Bool { lhs.rawValue < rhs.rawValue }
}

/// Injected metrics sink. Labels are enums on purpose: an out-of-domain label — a raw or
/// hashed account, device, recipient, owner_tab_id or request/key id — is unrepresentable
/// at the call site rather than merely discouraged (§6.6).
public protocol CloudSpoolMetrics: AnyObject, Sendable {
    /// Gauge `cloud_spool_rows{runtime}` — every row still present in this store.
    func recordRows(_ value: Int, runtime: CloudSpoolRuntime)
    /// Gauge `cloud_spool_bytes{runtime}` — charged bytes across every stored row.
    func recordBytes(_ value: Int, runtime: CloudSpoolRuntime)
    /// Counter `cloud_spool_admission_refusals_total{runtime,reason,scope}`.
    func recordAdmissionRefusal(runtime: CloudSpoolRuntime,
                                reason: CloudSpoolRefusalReason,
                                scope: CloudSpoolRefusalScope)
    /// Occupancy threshold crossing (80% warning / 90% page) on rows or bytes.
    func recordOccupancyAlert(runtime: CloudSpoolRuntime,
                              dimension: CloudSpoolOccupancyDimension,
                              level: CloudSpoolOccupancyAlert)
    /// §6.2 — a late ack/error arrived for an already-terminal row; telemetry only.
    func recordLateSettleTelemetry(runtime: CloudSpoolRuntime)
    /// Counter `state_store_gc_total{store,reason}`.
    func recordStateStoreGC(store: CloudStateStore, reason: CloudStateStoreGCReason)
    /// Counter `state_store_corrupt_total{store}`.
    func recordStateStoreCorrupt(store: CloudStateStore)
}

// MARK: - Typed errors and dispositions

public enum CloudOutboundSpoolError: Error, Equatable {
    case invalidWindowLimits
    case latestValuePolicyRequired(channel: CloudSpoolChannel)
    /// §6.6 — the candidate transaction was refused; reason and scope name which cap.
    case admissionRefused(reason: CloudSpoolRefusalReason, scope: CloudSpoolRefusalScope)
    /// Fail closed: the persisted counter does not dominate the persisted rows.
    case corruptCounter(nextSeq: Int64, maxRowSeq: Int64)
    /// Fail closed: a row failed its read-time integrity recomputation (§6.6).
    case corruptRow(seq: Int64, detail: String)
    case rowNotFound(seq: Int64)
    /// §6.1.4 — the same seq must never be sealed twice; only a reserved row may seal.
    case resealForbidden(seq: Int64, state: CloudSpoolRowState)
    /// A sealed envelope with no bytes is indistinguishable from a crash marker; refused.
    case emptySealedEnvelope(seq: Int64)
    /// An ack/error correlated to a row that was never sent is a protocol violation.
    case settleBeforeSend(seq: Int64, state: CloudSpoolRowState)
    /// viewer_offline is a ctlr-response outcome (§6.2); it does not settle other channels.
    case settleKindInvalidForChannel(seq: Int64, channel: CloudSpoolChannel)
    /// The logical tombstone applies to terminal transport rows (§6.2), not live ones.
    case tombstoneOnLiveRow(seq: Int64, state: CloudSpoolRowState)
    /// A ready/sent row must carry the non-empty sealed frame fixed by `seal`.
    case missingSealedEnvelopeForSend(seq: Int64, state: CloudSpoolRowState)
    /// A ready row has never been sent and therefore cannot already carry `first_sent`.
    case readyRowAlreadyHasFirstSent(seq: Int64)
    /// A receipt named a real sequence but not the channel sealed into that row.
    case settlementCorrelationMismatch(seq: Int64)
}

/// What one publisher step did. A normal publisher may pass already-written `sent` rows, but it
/// never passes the lowest `reserved` or `ready` sequence; blocked dispositions name that boundary.
public enum CloudSpoolSendDisposition: Equatable, Sendable {
    case idle
    case sent(seq: Int64)
    /// §6.2 reconnect: the same sealed bytes were handed to the transport again.
    case resent(seq: Int64)
    /// A normal drain found only in-window sent work. Only reconnect may resend it.
    case blockedAwaitingAcknowledgement(headSeq: Int64)
    /// The next globally ordered ready row remains durable because admitting it would exceed the
    /// implementation-default row or byte window.
    case blockedWindowFull(currentRows: Int, currentBytes: Int)
    /// The head seq is reserved and unsealed; nothing may send past it.
    case blockedAwaitingSeal(headSeq: Int64)
}

/// A bounded, identity-free observation for W6. Numeric limits remain
/// `implementation_default_pending_w6` until the rollout owner measures and approves them.
public struct CloudOutboundWindowSnapshot: Equatable, Sendable {
    public let budgetStatus: String
    public let maximumRows: Int
    public let maximumBytes: Int
    public let currentRows: Int
    public let currentBytes: Int
    public let processPeakRows: Int
    public let processPeakBytes: Int
    public let readyWaitingRows: Int
    public let readyWaitingBytes: Int
    public let oldestReadyAgeMilliseconds: UInt64
    public let oldestReceiptWaitMilliseconds: UInt64
    public let socketWritesStarted: UInt64
    public let socketWritesCompleted: UInt64
    public let socketWritesFailed: UInt64
    public let windowAdmissionRefusalAttempts: UInt64
}

/// How the peer settled a sent row.
public enum CloudSpoolSettleKind: Equatable, Sendable {
    case delivered
    /// §6.2 — for a Mac ctlr response, viewer_offline is terminal `acked` exactly like a
    /// delivered ack; the effect is never redone.
    case viewerOffline
    case peerError
}

public enum CloudSpoolSettleDisposition: Equatable, Sendable {
    case settled(CloudSpoolRowState)
    /// §6.2 — the row was already terminal; recorded as telemetry, nothing revived, nothing
    /// blocked.
    case lateIgnored
}

// MARK: - The spool

public actor CloudOutboundSpool {
    /// `nil` is the typed W5-1 state for a platform that has no accepted runtime metric label.
    /// It suppresses all spool metric publication instead of mislabelling Linux as Mac.
    public let runtime: CloudSpoolRuntime?
    private let store: CloudSpoolStore
    private let clock: CloudSpoolClock
    private let metrics: CloudSpoolMetrics
    private let limits: CloudSpoolLimits
    private let sequenceFence: any CloudSpoolSequenceFence
    private let strictPersistedFrameValidation: Bool
    private var state: CloudSpoolPersistedState
    private var lastGCContinuous: Duration
    private var alertLevels: [CloudSpoolOccupancyDimension: CloudSpoolOccupancyAlert] = [:]
    private var peakWindowRows = 0
    private var peakWindowBytes = 0
    private var socketWritesStarted: UInt64 = 0
    private var socketWritesCompleted: UInt64 = 0
    private var socketWritesFailed: UInt64 = 0
    private var windowAdmissionRefusalAttempts: UInt64 = 0

    /// Opening the spool IS recovery. In order, all inside one committed transaction:
    /// integrity verification fails closed (corrupt counter, §6.6 charged_bytes
    /// recomputation, state/bytes coherence); every reserved row burns durably before any
    /// higher seq is processed (§6.1.2); every sent row burns because old continuous
    /// instants cannot be compared after restart (§6.2); ownerless ctl/ctlr ready rows burn;
    /// stale ready stream rows burn; fresh ready stream rows remain sendable as usual.
    /// Terminal rows keep their states, with tombstone instants conservatively restarted at
    /// the open instant (their old continuous values are incomparable too, so retention
    /// begins again rather than guessing).
    public init(store: CloudSpoolStore,
                clock: CloudSpoolClock,
                metrics: CloudSpoolMetrics,
                runtime: CloudSpoolRuntime? = .mac,
                limits: CloudSpoolLimits = CloudSpoolLimits(),
                liveOwnerIDs: Set<String> = [],
                sequenceFence: any CloudSpoolSequenceFence = CloudNoopSpoolSequenceFence(),
                strictPersistedFrameValidation: Bool = false) throws {
        self.store = store
        self.clock = clock
        self.metrics = metrics
        self.runtime = runtime
        self.limits = limits
        self.sequenceFence = sequenceFence
        self.strictPersistedFrameValidation = strictPersistedFrameValidation

        guard limits.outboundWindowRowCap > 0, limits.outboundWindowByteCap > 0 else {
            throw CloudOutboundSpoolError.invalidWindowLimits
        }

        var working = try store.load()
        let openContinuous = clock.continuousNow
        let openWall = clock.wallNow
        func corrupt(_ error: CloudOutboundSpoolError) -> CloudOutboundSpoolError {
            if runtime != nil { metrics.recordStateStoreCorrupt(store: .macSpool) }
            return error
        }

        // Fail closed before trusting anything (§6.6): the counter must dominate every seq.
        let maxRowSeq = working.rows.map { $0.seq }.max() ?? -1
        guard working.nextSeq >= 0, working.nextSeq > maxRowSeq else {
            throw corrupt(.corruptCounter(nextSeq: working.nextSeq, maxRowSeq: maxRowSeq))
        }
        var seen = Set<Int64>()
        var previousSeq: Int64?
        for row in working.rows {
            guard seen.insert(row.seq).inserted else {
                throw corrupt(.corruptRow(seq: row.seq, detail: "duplicate seq"))
            }
            if let previousSeq, row.seq <= previousSeq {
                throw corrupt(.corruptRow(
                    seq: row.seq, detail: "rows are not strictly ascending by seq"))
            }
            previousSeq = row.seq
            let parsed: CloudJSONValue
            do { parsed = try Self.validatePersistedRow(
                row, strictFrame: strictPersistedFrameValidation) }
            catch let error as CloudOutboundSpoolError { throw corrupt(error) }
            catch { throw corrupt(.corruptRow(
                seq: row.seq, detail: "logical record bytes are not canonical JSON")) }
            // §6.6: recompute charged_bytes on read; a mismatch is corruption, fail closed.
            guard CloudCanonicalJSON.chargedBytes(record: parsed) == row.chargedBytes else {
                throw corrupt(.corruptRow(
                    seq: row.seq, detail: "charged_bytes does not match recomputation"))
            }
            switch row.state {
            case .ready, .sent:
                guard let bytes = row.sealedEnvelopeBytes, !bytes.isEmpty else {
                    throw corrupt(.corruptRow(
                        seq: row.seq, detail: "\(row.state.rawValue) row without sealed bytes"))
                }
            case .reserved:
                guard row.sealedEnvelopeBytes == nil else {
                    throw corrupt(.corruptRow(
                        seq: row.seq, detail: "reserved row carrying sealed bytes"))
                }
            case .acked, .rejected, .burned:
                break
            }
        }

        // §6.1.2: a crash leaves reserved rows with no bytes; burn them durably before any
        // higher seq is processed. The single commit below lands before this init returns,
        // so no send, seal or admission can observe the pre-burn state.
        for index in working.rows.indices where working.rows[index].state == .reserved {
            Self.burn(&working.rows[index], reason: .recoveredUnsealedReservation, outcome: nil,
                      tombstone: openContinuous)
        }
        // §6.2: old continuous instants are incomparable after restart — every sent row
        // burns (transport uncertain) to release its durable window slot.
        for index in working.rows.indices where working.rows[index].state == .sent {
            Self.burn(&working.rows[index], reason: .restartSentUncertain,
                      outcome: .transportUncertain, tombstone: openContinuous)
        }
        // §6.2: ownerless ctl/ctlr ready rows burn.
        for index in working.rows.indices
        where working.rows[index].state == .ready && working.rows[index].channel.isControl {
            let owner = working.rows[index].ownerID
            if owner == nil || !liveOwnerIDs.contains(owner!) {
                Self.burn(&working.rows[index], reason: .ownerlessControlAtRestart, outcome: nil,
                          tombstone: openContinuous)
            }
        }
        // Every relay publish class is timestamp-validated. Apply the same explicit local policy
        // at open and live selection, using the authenticated timestamp inside the exact frame.
        // Control and transcript rows are not coalesced, but impossible-to-accept bytes are still
        // terminal locally instead of being sent into a deterministic peer refusal loop.
        // A never-sent row has no peer observation or late ACK to correlate. Retaining a burned
        // tombstone for the full terminal window made repeated snapshots consume the global row
        // cap even though every superseded byte was locally certain. Remove these rows in the
        // same recovery commit; sent rows above remain durable tombstones.
        working.rows.removeAll { row in
            row.state == .ready && Self.isStaleForSend(
                row, wallNow: openWall,
                freshnessSeconds: limits.sealedFrameFreshnessSeconds)
        }
        // A store written before settling dropped the frame still carries one on every terminal
        // row, and would carry it through every commit of this run until the tombstone expired.
        // Dropping it here costs nothing extra: the burns above have already made this commit
        // necessary, and nothing below sends a terminal row.
        for index in working.rows.indices
        where working.rows[index].state.isTerminal
            && working.rows[index].sealedEnvelopeBytes != nil {
            working.rows[index].sealedEnvelopeBytes = nil
        }
        // Tombstone instants from the previous run are incomparable; restart retention now.
        for index in working.rows.indices
        where working.rows[index].logicalTombstoneContinuous != nil {
            working.rows[index].logicalTombstoneContinuous = openContinuous
        }

        try store.commit(working)
        self.state = working
        self.lastGCContinuous = openContinuous
        let initialWindow = Self.windowOccupancy(of: working)
        self.peakWindowRows = initialWindow.rows
        self.peakWindowBytes = initialWindow.bytes

        let rows = Self.storedRowCount(of: working)
        let bytes = Self.storedByteCount(of: working)
        if let runtime {
            metrics.recordRows(rows, runtime: runtime)
            metrics.recordBytes(bytes, runtime: runtime)
        }
    }

    // MARK: Reserve (admission)

    /// §6.6: the byteLength of the exact logical record — minus a top-level `charged_bytes`
    /// member, which the figure must not include — serialized as RFC 8785 canonical UTF-8.
    /// Binary fields must already be canonical padded base64 (`CloudJSONValue.base64(_:)`).
    public static func canonicalRecordBytes(of record: CloudJSONValue) -> Data {
        let stripped: CloudJSONValue
        if case .object(var members) = record {
            members.removeValue(forKey: "charged_bytes")
            stripped = .object(members)
        } else {
            stripped = record
        }
        return CloudCanonicalJSON.canonicalData(stripped)
    }

    public static func chargedBytes(of record: CloudJSONValue) -> Int {
        canonicalRecordBytes(of: record).count
    }

    /// §6.1.1: atomically advance the counter and insert a reserved row. GC's bounded batch
    /// runs first (§6.6), and the cap checks, counter movement and insert share the single
    /// commit at the end — there is no insert-then-count, and no live row is ever evicted to
    /// make room.
    public func reserve(channel: CloudSpoolChannel,
                        logicalID: String,
                        ownerID: String?,
                        recipient: String,
                        record: CloudJSONValue) throws -> Int64 {
        try gcBeforeAdmission()
        var working = state
        let seq = try admitReservation(into: &working, channel: channel, logicalID: logicalID,
                                       ownerID: ownerID, recipient: recipient, record: record)
        try store.commit(working)
        state = working
        publishOccupancy()
        return seq
    }

    /// Latest-value admission for `s` and `orch`. Every older never-sent ready value for the
    /// same exact wire recipient is burned before the replacement reservation, and both changes
    /// share one commit. Sent rows retain their exact-frame uncertainty and are never coalesced.
    public func reserveLatestValue(
        channel: CloudSpoolChannel, logicalID: String, ownerID: String?, recipient: String,
        record: CloudJSONValue
    ) throws -> Int64 {
        guard channel.isLatestValue else {
            throw CloudOutboundSpoolError.latestValuePolicyRequired(channel: channel)
        }
        try gcBeforeAdmission()
        var working = state
        working.rows.removeAll {
            $0.state == .ready && $0.channel == channel && $0.recipient == recipient
        }
        let seq = try admitReservation(
            into: &working, channel: channel, logicalID: logicalID, ownerID: ownerID,
            recipient: recipient, record: record)
        try store.commit(working)
        state = working
        publishOccupancy()
        return seq
    }

    /// The candidate transaction body shared by `reserve` and the coalescing replacement
    /// reserve. Mutates `working` only; the caller owns the commit.
    private func admitReservation(into working: inout CloudSpoolPersistedState,
                                  channel: CloudSpoolChannel,
                                  logicalID: String,
                                  ownerID: String?,
                                  recipient: String,
                                  record: CloudJSONValue) throws -> Int64 {
        do {
            try CloudCanonicalJSON.validateNumberDomain(record)
        } catch {
            // `corrupt` is the existing closed-domain reason for a record that cannot be
            // represented in the spool's canonical persistence domain. Refuse before any
            // canonical bytes, quota arithmetic, counter movement, or row mutation occurs.
            throw refuseAdmission(.corrupt, .global)
        }
        let recordBytes = Self.canonicalRecordBytes(of: record)
        let charged = recordBytes.count

        // Pre-commit occupancy — the §6.6 fairness activation point reads the store as it is
        // before the candidate.
        var rowsBefore = 0, bytesBefore = 0, recipientRows = 0, recipientBytes = 0
        // Hard caps protect the durable store itself. Terminal rows still inside their
        // tombstone-retention window remain persisted and therefore occupy both quota
        // dimensions until GC actually deletes them.
        for row in working.rows {
            rowsBefore += 1
            bytesBefore += row.chargedBytes
            if row.recipient == recipient {
                recipientRows += 1
                recipientBytes += row.chargedBytes
            }
        }

        // §6.6 fairness reserve: activates when pre-commit occupancy reaches ceil(0.9*cap)
        // on either dimension, and then requires the candidate's recipient to stay within
        // the fairness caps after commit.
        let fairnessActive = rowsBefore >= Self.fairnessActivationThreshold(limits.globalRowCap)
            || bytesBefore >= Self.fairnessActivationThreshold(limits.globalByteCap)
        if fairnessActive {
            let withinFairness = recipientRows + 1 <= limits.fairnessRecipientRowCap
                && recipientBytes + charged <= limits.fairnessRecipientByteCap
            if !withinFairness { throw refuseAdmission(.fairness, .recipient) }
        }

        // Hard caps compare only after the candidate commit: `<=` — equal to cap succeeds,
        // exceeding refuses (§6.6). The per-recipient pair is the table's "actor hard cap".
        let withinRecipientRows = recipientRows + 1 <= limits.recipientRowCap
        if !withinRecipientRows { throw refuseAdmission(.rowCap, .recipient) }
        let withinRecipientBytes = recipientBytes + charged <= limits.recipientByteCap
        if !withinRecipientBytes { throw refuseAdmission(.byteCap, .recipient) }
        let withinGlobalRows = rowsBefore + 1 <= limits.globalRowCap
        if !withinGlobalRows { throw refuseAdmission(.rowCap, .global) }
        let withinGlobalBytes = bytesBefore + charged <= limits.globalByteCap
        if !withinGlobalBytes { throw refuseAdmission(.byteCap, .global) }

        let seq = working.nextSeq
        // The wire is consumed by JavaScript, so the persisted next value must remain in the
        // injective ECMAScript safe-integer domain too. Refuse before reserving the last value
        // whose increment could no longer be represented exactly by both runtimes.
        guard seq >= 0, seq < CloudCanonicalJSON.maximumSafeInteger else {
            throw refuseAdmission(.corrupt, .global)
        }
        // Rollback safety is ordered before the new authority commit. Advancing the predecessor
        // and then failing below creates a harmless gap; the reverse order could create reuse.
        try sequenceFence.prepareToReserve(sequence: seq)
        working.nextSeq += 1
        working.rows.append(CloudSpoolRow(
            seq: seq, channel: channel, logicalID: logicalID, ownerID: ownerID,
            recipient: recipient, logicalRecordCanonicalBytes: recordBytes,
            chargedBytes: charged, sealedEnvelopeBytes: nil, state: .reserved,
            reservedAt: clock.wallNow, reservedAtContinuous: clock.continuousNow,
            firstSentContinuous: nil, attemptNotAfterContinuous: nil,
            attemptOutcome: nil, burnReason: nil, logicalTombstoneContinuous: nil))
        return seq
    }

    /// ceil(0.9 * cap), in integers.
    private static func fairnessActivationThreshold(_ cap: Int) -> Int { (9 * cap + 9) / 10 }

    private func refuseAdmission(_ reason: CloudSpoolRefusalReason,
                                 _ scope: CloudSpoolRefusalScope) -> CloudOutboundSpoolError {
        if let runtime {
            metrics.recordAdmissionRefusal(runtime: runtime, reason: reason, scope: scope)
        }
        return .admissionRefused(reason: reason, scope: scope)
    }

    // MARK: Seal

    /// §6.1.2: seal writes the immutable exact bytes onto an already-reserved seq and makes
    /// it ready, in one transaction. Only a reserved row may seal — the same seq is never
    /// sealed twice (§6.1.4).
    public func seal(seq: Int64, sealedEnvelope: Data) throws {
        try gcIfDue()
        guard let index = state.rows.firstIndex(where: { $0.seq == seq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: seq)
        }
        guard state.rows[index].state == .reserved else {
            throw CloudOutboundSpoolError.resealForbidden(seq: seq, state: state.rows[index].state)
        }
        guard !sealedEnvelope.isEmpty else {
            throw CloudOutboundSpoolError.emptySealedEnvelope(seq: seq)
        }
        var working = state
        working.rows[index].sealedEnvelopeBytes = sealedEnvelope
        working.rows[index].state = .ready
        if strictPersistedFrameValidation {
            _ = try Self.validatePersistedRow(working.rows[index], strictFrame: true)
        }
        try store.commit(working)
        state = working
    }

    // MARK: Publish (the single lease)

    /// One publisher step under the composition's unique worker lease (§6.1.6). Normal drain
    /// skips already-written `sent` rows, but never a lower reserved/ready row, and stops before
    /// the configured row or exact-frame byte window would be exceeded. The composition snapshots
    /// reconnect work and calls `resendPersisted` for each sent sequence in ascending order.
    ///
    /// For a ready head the row is durably committed `sent` (first_sent and
    /// attempt_not_after = first_sent + 30s fixed on the first send) *before* the transport
    /// closure runs; a throwing transport leaves the row sent and rethrows (§6.1.4). For a
    /// Reconnect replay has one implementation, `resendPersisted`, and is never selected by this
    /// normal-drain method.
    static func lowestNonterminalIndex(in rows: [CloudSpoolRow]) -> Int? {
        rows.indices
            .filter { !rows[$0].state.isTerminal }
            .min { rows[$0].seq < rows[$1].seq }
    }

    public func sendNext(
        via transport: @Sendable (Data) async throws -> Void
    ) async throws -> CloudSpoolSendDisposition {
        try gcIfDue()
        try burnExpiredSentRows()
        try normalizeReadyRowsForSend()
        let occupancy = Self.windowOccupancy(of: state)
        let ordered = state.rows.indices.sorted { state.rows[$0].seq < state.rows[$1].seq }

        var candidateIndex: Int?
        for index in ordered {
            switch state.rows[index].state {
            case .reserved:
                return .blockedAwaitingSeal(headSeq: state.rows[index].seq)
            case .ready:
                candidateIndex = index
            case .sent, .acked, .rejected, .burned:
                continue
            }
            if candidateIndex != nil { break }
        }
        guard let index = candidateIndex else {
            if let head = state.rows.filter({ $0.state == .sent }).min(by: { $0.seq < $1.seq }) {
                return .blockedAwaitingAcknowledgement(headSeq: head.seq)
            }
            return .idle
        }
        let head = state.rows[index]
        guard head.firstSentContinuous == nil else {
            throw CloudOutboundSpoolError.readyRowAlreadyHasFirstSent(seq: head.seq)
        }
        guard let sealedEnvelope = head.sealedEnvelopeBytes, !sealedEnvelope.isEmpty else {
            throw CloudOutboundSpoolError.missingSealedEnvelopeForSend(
                seq: head.seq, state: head.state)
        }
        guard sealedEnvelope.count <= limits.outboundWindowByteCap,
              occupancy.rows < limits.outboundWindowRowCap,
              occupancy.bytes <= limits.outboundWindowByteCap - sealedEnvelope.count else {
            windowAdmissionRefusalAttempts = Self.incremented(windowAdmissionRefusalAttempts)
            return .blockedWindowFull(
                currentRows: occupancy.rows, currentBytes: occupancy.bytes)
        }
        var working = state
        let firstSent = clock.continuousNow
        working.rows[index].firstSentContinuous = firstSent
        working.rows[index].attemptNotAfterContinuous = firstSent + limits.attemptWindow
        working.rows[index].state = .sent
        try store.commit(working)
        state = working
        updateWindowPeaks()
        socketWritesStarted = Self.incremented(socketWritesStarted)
        do {
            try await transport(sealedEnvelope)
            socketWritesCompleted = Self.incremented(socketWritesCompleted)
        } catch {
            // §6.1.4: a throwing send leaves the row sent. The attempt window, not the
            // transport error, decides when it stops holding a durable window slot.
            socketWritesFailed = Self.incremented(socketWritesFailed)
            throw error
        }
        return .sent(seq: head.seq)
    }

    /// Reconnect-only exact replay. The caller snapshots sent sequences once per authenticated
    /// connection and invokes this in ascending order, so rows that settle while reconnect drain
    /// is running cannot make the sender fall through into a newly-ready row or replay one frame
    /// twice. A terminal row is already settled and therefore needs no replay.
    public func resendPersisted(
        seq: Int64,
        via transport: @Sendable (Data) async throws -> Void
    ) async throws -> CloudSpoolSendDisposition {
        try gcIfDue()
        try burnExpiredSentRows()
        guard let row = state.rows.first(where: { $0.seq == seq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: seq)
        }
        guard row.state == .sent else {
            return row.state.isTerminal ? .idle : .blockedAwaitingAcknowledgement(headSeq: seq)
        }
        guard let sealedEnvelope = row.sealedEnvelopeBytes, !sealedEnvelope.isEmpty else {
            throw CloudOutboundSpoolError.missingSealedEnvelopeForSend(
                seq: row.seq, state: row.state)
        }
        socketWritesStarted = Self.incremented(socketWritesStarted)
        do {
            try await transport(sealedEnvelope)
            socketWritesCompleted = Self.incremented(socketWritesCompleted)
        } catch {
            socketWritesFailed = Self.incremented(socketWritesFailed)
            throw error
        }
        return .resent(seq: row.seq)
    }

    /// Deadline-only maintenance, deliberately separate from socket ownership. It lets the
    /// composition release expired durable window slots even while its sole socket write is
    /// suspended. The same committed burn transition is used by normal drain.
    @discardableResult
    public func expireAttemptDeadlines() throws -> Int {
        try gcIfDue()
        let before = state.rows.reduce(0) { $0 + ($1.state == .sent ? 1 : 0) }
        try burnExpiredSentRows()
        let after = state.rows.reduce(0) { $0 + ($1.state == .sent ? 1 : 0) }
        return before - after
    }

    /// §6.2: at the attempt cap with no correlated ack/error, one transaction moves the row
    /// sent→burned and marks the attempt transport_uncertain; only then is seq N+1 allowed.
    private func burnExpiredSentRows() throws {
        let now = clock.continuousNow
        var working = state
        var changed = false
        for index in working.rows.indices where working.rows[index].state == .sent {
            if let notAfter = working.rows[index].attemptNotAfterContinuous, now >= notAfter {
                Self.burn(&working.rows[index], reason: .attemptCapExpired,
                          outcome: .transportUncertain, tombstone: now)
                changed = true
            }
        }
        if changed {
            try store.commit(working)
            state = working
            publishOccupancy()
        }
    }

    // MARK: Settle

    /// Correlated peer outcome for a sent row. `delivered` acks; for a ctlr response,
    /// `viewerOffline` is terminal `acked` exactly like a delivered ack and the effect is
    /// never redone (§6.2); `peerError` rejects. A late outcome for an already-terminal row
    /// is telemetry only — it revives nothing, settles nothing, and blocks nothing (§6.2).
    public func settle(
        seq: Int64, channel: CloudSpoolChannel? = nil, fullChannel: String? = nil,
        _ kind: CloudSpoolSettleKind
    ) throws -> CloudSpoolSettleDisposition {
        try gcIfDue()
        guard let index = state.rows.firstIndex(where: { $0.seq == seq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: seq)
        }
        let row = state.rows[index]
        if let channel, channel != row.channel {
            throw CloudOutboundSpoolError.settlementCorrelationMismatch(seq: seq)
        }
        if let fullChannel, fullChannel != row.recipient {
            throw CloudOutboundSpoolError.settlementCorrelationMismatch(seq: seq)
        }
        switch row.state {
        case .sent:
            if kind == .viewerOffline && row.channel != .ctlr {
                throw CloudOutboundSpoolError.settleKindInvalidForChannel(seq: seq,
                                                                          channel: row.channel)
            }
            let terminal: CloudSpoolRowState = kind == .peerError ? .rejected : .acked
            try updateRow(at: index) {
                $0.state = terminal
                $0.logicalTombstoneContinuous = self.clock.continuousNow
                // The frame dies with the row's last use of it. A terminal row is never handed to
                // the transport again — `sendNext` only ever looks at ready and sent — so from
                // here the sealed bytes are payload this store rewrites on every later commit and
                // can never send. Holding them until the ten-minute tombstone expiry is what made
                // one acked 6.5 MB orchestrator snapshot cost 6.5 MB on every subsequent publish:
                // five of them put this file at 33 MB, and each new envelope had to rewrite and
                // fsync all of it. Quota accounting is unaffected — `chargedBytes` is computed
                // from the logical record, not from this field.
                $0.sealedEnvelopeBytes = nil
            }
            publishOccupancy()
            return .settled(terminal)
        case .acked, .rejected, .burned:
            if let runtime { metrics.recordLateSettleTelemetry(runtime: runtime) }
            return .lateIgnored
        case .reserved, .ready:
            throw CloudOutboundSpoolError.settleBeforeSend(seq: seq, state: row.state)
        }
    }

    // MARK: Tombstones and GC

    /// §6.2: the logical layer reports the record's tombstone; the terminal transport row is
    /// then retained for `tombstoneRetention` before GC may delete it. Live rows have no
    /// logical tombstone.
    public func recordLogicalTombstone(seq: Int64) throws {
        guard let index = state.rows.firstIndex(where: { $0.seq == seq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: seq)
        }
        guard state.rows[index].state.isTerminal else {
            throw CloudOutboundSpoolError.tombstoneOnLiveRow(seq: seq,
                                                             state: state.rows[index].state)
        }
        try updateRow(at: index) { $0.logicalTombstoneContinuous = self.clock.continuousNow }
    }

    /// §6.6: a bounded GC batch. Burns stale reservations first; deletes only terminal rows
    /// whose logical tombstone is at least `tombstoneRetention` old; never touches ready or
    /// sent rows — the spool does not drop live work to make room.
    public func gcTick() throws {
        try runGC(batchLimit: limits.gcBatchLimit)
    }

    private func gcIfDue() throws {
        if clock.continuousNow - lastGCContinuous >= limits.gcInterval {
            try runGC(batchLimit: limits.gcBatchLimit)
        }
    }

    /// §6.6: GC runs before every admission; a GC failure fails the admission closed.
    private func gcBeforeAdmission() throws {
        do {
            try runGC(batchLimit: limits.gcBatchLimit)
        } catch {
            throw refuseAdmission(.gcFailed, .global)
        }
    }

    private func runGC(batchLimit: Int) throws {
        let now = clock.continuousNow
        var working = state
        var changed = false
        var expiredCount = 0
        for index in working.rows.indices where working.rows[index].state == .reserved {
            if now - working.rows[index].reservedAtContinuous > limits.staleReservedAfter {
                Self.burn(&working.rows[index], reason: .staleReservation, outcome: nil,
                          tombstone: now)
                changed = true
                expiredCount += 1
            }
        }
        var deleted = 0
        working.rows.removeAll { row in
            guard deleted < batchLimit else { return false }
            let deletable = row.state.isTerminal && tombstoneExpired(row, now: now)
            if deletable { deleted += 1 }
            return deletable
        }
        if changed || deleted > 0 {
            try store.commit(working)
            state = working
            publishOccupancy()
            for _ in 0..<expiredCount {
                if runtime != nil { metrics.recordStateStoreGC(store: .macSpool, reason: .expired) }
            }
            for _ in 0..<deleted {
                if runtime != nil { metrics.recordStateStoreGC(store: .macSpool, reason: .terminal) }
            }
        }
        lastGCContinuous = now
    }

    private func tombstoneExpired(_ row: CloudSpoolRow, now: Duration) -> Bool {
        guard let tombstone = row.logicalTombstoneContinuous else { return false }
        return now - tombstone >= limits.tombstoneRetention
    }

    // MARK: Introspection (tests and callers)

    public func rowsSnapshot() -> [CloudSpoolRow] { state.rows }

    public func row(seq: Int64) -> CloudSpoolRow? {
        state.rows.first { $0.seq == seq }
    }

    public func outboundWindowSnapshot() -> CloudOutboundWindowSnapshot {
        let occupancy = Self.windowOccupancy(of: state)
        let ready = state.rows.filter { $0.state == .ready }
        let now = clock.continuousNow
        let oldestReadyMilliseconds = state.rows
            .filter { $0.state == .ready }
            .map { Self.milliseconds(max(0, clock.wallNow.timeIntervalSince($0.reservedAt))) }
            .max() ?? 0
        let oldestReceiptMilliseconds = state.rows.compactMap { row -> UInt64? in
            guard row.state == .sent, let first = row.firstSentContinuous else { return nil }
            return Self.milliseconds(max(.zero, now - first))
        }.max() ?? 0
        return CloudOutboundWindowSnapshot(
            budgetStatus: "implementation_default_pending_w6",
            maximumRows: limits.outboundWindowRowCap,
            maximumBytes: limits.outboundWindowByteCap,
            currentRows: occupancy.rows,
            currentBytes: occupancy.bytes,
            processPeakRows: peakWindowRows,
            processPeakBytes: peakWindowBytes,
            readyWaitingRows: ready.count,
            readyWaitingBytes: ready.reduce(0) { $0 + ($1.sealedEnvelopeBytes?.count ?? 0) },
            oldestReadyAgeMilliseconds: oldestReadyMilliseconds,
            oldestReceiptWaitMilliseconds: oldestReceiptMilliseconds,
            socketWritesStarted: socketWritesStarted,
            socketWritesCompleted: socketWritesCompleted,
            socketWritesFailed: socketWritesFailed,
            windowAdmissionRefusalAttempts: windowAdmissionRefusalAttempts)
    }

    /// The lifecycle-owned maintenance task wakes both attempt expiry and a stale unsealed
    /// reservation. A reservation failure therefore cannot strand every higher ready row merely
    /// because no later producer happens to arrive.
    public func nextMaintenanceWakeDelay() -> Duration? {
        let deadlines = state.rows.compactMap { row -> Duration? in
            if row.state == .sent { return row.attemptNotAfterContinuous }
            if row.state == .reserved {
                return row.reservedAtContinuous + limits.staleReservedAfter + .milliseconds(1)
            }
            return nil
        }
        guard let deadline = deadlines.min() else { return nil }
        return max(.zero, deadline - clock.continuousNow)
    }

    // MARK: Internals

    private func updateRow(at index: Int, _ mutate: (inout CloudSpoolRow) -> Void) throws {
        var working = state
        mutate(&working.rows[index])
        try store.commit(working)
        state = working
    }

    private static func burn(_ row: inout CloudSpoolRow,
                             reason: CloudSpoolBurnReason,
                             outcome: CloudSpoolAttemptOutcome?,
                             tombstone: Duration) {
        row.state = .burned
        row.burnReason = reason
        if let outcome { row.attemptOutcome = outcome }
        row.logicalTombstoneContinuous = tombstone
        // Terminal, and for the same reason as an ack: nothing sends a burned row, so its frame
        // is dead weight in every commit until the tombstone expires.
        row.sealedEnvelopeBytes = nil
    }

    private static func storedRowCount(of state: CloudSpoolPersistedState) -> Int {
        state.rows.count
    }

    private static func storedByteCount(of state: CloudSpoolPersistedState) -> Int {
        state.rows.reduce(0) { $0 + $1.chargedBytes }
    }

    private static func windowOccupancy(
        of state: CloudSpoolPersistedState
    ) -> (rows: Int, bytes: Int) {
        let sent = state.rows.filter { $0.state == .sent }
        return (sent.count, sent.reduce(0) { $0 + ($1.sealedEnvelopeBytes?.count ?? 0) })
    }

    /// Before every live socket selection, collapse ready latest-value backlogs and remove any
    /// never-sent row whose authenticated sealed timestamp is outside the explicit local policy.
    /// One commit makes the whole normalization durable before any later row can be selected.
    private func normalizeReadyRowsForSend() throws {
        var working = state
        let wallNow = clock.wallNow
        var keepLatest = Set<String>()
        var removeSequences = Set<Int64>()
        var changed = false
        for index in working.rows.indices.reversed()
        where working.rows[index].state == .ready && working.rows[index].channel.isLatestValue {
            let row = working.rows[index]
            let key = row.channel.rawValue + "\u{0}" + row.recipient
            if !keepLatest.insert(key).inserted {
                removeSequences.insert(row.seq)
                changed = true
            }
        }
        for index in working.rows.indices where working.rows[index].state == .ready {
            if Self.isStaleForSend(
                working.rows[index], wallNow: wallNow,
                freshnessSeconds: limits.sealedFrameFreshnessSeconds
            ) {
                removeSequences.insert(working.rows[index].seq)
                changed = true
            }
        }
        if changed {
            working.rows.removeAll { removeSequences.contains($0.seq) }
            try store.commit(working)
            state = working
            publishOccupancy()
        }
    }

    private static func isStaleForSend(
        _ row: CloudSpoolRow, wallNow: Date, freshnessSeconds: TimeInterval
    ) -> Bool {
        let sealedMilliseconds = authenticatedSealedTimestampMilliseconds(row)
        let sealedAt = sealedMilliseconds.map {
            Date(timeIntervalSince1970: TimeInterval($0) / 1_000)
        } ?? row.reservedAt
        return wallNow.timeIntervalSince(sealedAt) > freshnessSeconds
    }

    /// Production opens with strict frame validation, so this value is authenticated by the
    /// retained signature and identity checks. The reservation-time fallback exists only for
    /// deliberately non-wire unit fixtures that open with strict validation disabled.
    private static func authenticatedSealedTimestampMilliseconds(_ row: CloudSpoolRow) -> Int64? {
        guard let bytes = row.sealedEnvelopeBytes,
              let frame = try? CloudCanonicalJSON.parseStrict(bytes),
              case .object(let fields) = frame,
              case .object(let envelope)? = fields["envelope"],
              case .int(let timestamp)? = envelope["ts"], timestamp >= 0 else { return nil }
        return timestamp
    }

    private func updateWindowPeaks() {
        let occupancy = Self.windowOccupancy(of: state)
        peakWindowRows = max(peakWindowRows, occupancy.rows)
        peakWindowBytes = max(peakWindowBytes, occupancy.bytes)
    }

    private static func incremented(_ value: UInt64) -> UInt64 {
        let (next, overflow) = value.addingReportingOverflow(1)
        return overflow ? .max : next
    }

    private static func milliseconds(_ duration: Duration) -> UInt64 {
        let parts = duration.components
        guard parts.seconds >= 0 else { return 0 }
        let seconds = UInt64(parts.seconds)
        let millis = UInt64(max(0, parts.attoseconds / 1_000_000_000_000_000))
        let (whole, overflow) = seconds.multipliedReportingOverflow(by: 1_000)
        return overflow || whole > UInt64.max - millis ? .max : whole + millis
    }

    private static func milliseconds(_ seconds: TimeInterval) -> UInt64 {
        guard seconds.isFinite, seconds > 0 else { return 0 }
        let value = seconds * 1_000
        return value >= Double(UInt64.max) ? .max : UInt64(value)
    }

    /// Publishes the two gauges and raises the §6.6 occupancy alerts (80% warning, 90% page)
    /// on upward crossings, once per crossing.
    private func publishOccupancy() {
        guard let runtime else { return }
        let rows = Self.storedRowCount(of: state)
        let bytes = Self.storedByteCount(of: state)
        metrics.recordRows(rows, runtime: runtime)
        metrics.recordBytes(bytes, runtime: runtime)
        raiseAlertIfCrossed(.rows, occupancy: rows, cap: limits.globalRowCap)
        raiseAlertIfCrossed(.bytes, occupancy: bytes, cap: limits.globalByteCap)
    }

    /// Strict production validation is intentionally implemented without importing the Mac-only
    /// envelope type, so the same Application actor compiles on Linux. The exact published JSON
    /// shape, signature-sized fields, sequence, full channel and nonsecret logical metadata are
    /// all bound before socket admission.
    private static func validatePersistedRow(
        _ row: CloudSpoolRow, strictFrame: Bool
    ) throws -> CloudJSONValue {
        guard row.seq >= 0, !row.logicalID.isEmpty, !row.recipient.isEmpty,
              row.chargedBytes >= 0 else {
            throw CloudOutboundSpoolError.corruptRow(seq: row.seq, detail: "invalid row identity")
        }
        let logical = try CloudCanonicalJSON.parseStrict(row.logicalRecordCanonicalBytes)
        guard CloudCanonicalJSON.canonicalData(logical) == row.logicalRecordCanonicalBytes else {
            throw CloudOutboundSpoolError.corruptRow(
                seq: row.seq, detail: "logical record is not canonical")
        }
        switch row.state {
        case .reserved:
            guard row.sealedEnvelopeBytes == nil, row.firstSentContinuous == nil,
                  row.attemptNotAfterContinuous == nil, row.attemptOutcome == nil,
                  row.burnReason == nil, row.logicalTombstoneContinuous == nil else {
                throw CloudOutboundSpoolError.corruptRow(
                    seq: row.seq, detail: "reserved row has contradictory fields")
            }
        case .ready:
            guard row.sealedEnvelopeBytes != nil, row.firstSentContinuous == nil,
                  row.attemptNotAfterContinuous == nil, row.attemptOutcome == nil,
                  row.burnReason == nil, row.logicalTombstoneContinuous == nil else {
                throw CloudOutboundSpoolError.corruptRow(
                    seq: row.seq, detail: "ready row has contradictory fields")
            }
        case .sent:
            guard row.sealedEnvelopeBytes != nil, let first = row.firstSentContinuous,
                  let deadline = row.attemptNotAfterContinuous, deadline >= first,
                  row.attemptOutcome == nil, row.burnReason == nil,
                  row.logicalTombstoneContinuous == nil else {
                throw CloudOutboundSpoolError.corruptRow(
                    seq: row.seq, detail: "sent row has contradictory fields")
            }
        case .acked, .rejected:
            // The frame is deliberately absent once a row settles, and a file written before that
            // change still carries one, so this says nothing about the field either way — the
            // restart path at `open` has always read terminal rows without requiring it, and a
            // rule that contradicted its own reader could only ever quarantine a healthy store.
            // Everything that does order a settled row is still bound here.
            guard row.firstSentContinuous != nil,
                  row.attemptNotAfterContinuous != nil, row.burnReason == nil else {
                throw CloudOutboundSpoolError.corruptRow(
                    seq: row.seq, detail: "settled row has contradictory fields")
            }
        case .burned:
            guard row.burnReason != nil else {
                throw CloudOutboundSpoolError.corruptRow(
                    seq: row.seq, detail: "burned row has no reason")
            }
        }
        guard strictFrame else { return logical }
        guard case .object(let metadata) = logical,
              Set(metadata.keys) == ["v", "ch", "class", "key_id", "logical_id",
                                      "payload_bytes", "payload_sha256", "sender"],
              case .int(let metadataVersion)? = metadata["v"], metadataVersion == 2,
              case .string(let channel)? = metadata["ch"], channel == row.recipient,
              case .string(let logicalID)? = metadata["logical_id"], logicalID == row.logicalID,
              case .string(let sender)? = metadata["sender"], !sender.isEmpty,
              case .string(let keyID)? = metadata["key_id"], !keyID.isEmpty,
              case .string(let envelopeClass)? = metadata["class"], envelopeClass == "stream",
              case .int(let payloadBytes)? = metadata["payload_bytes"], payloadBytes >= 0,
              case .string(let payloadDigest)? = metadata["payload_sha256"],
              payloadDigest.count == 64,
              payloadDigest.allSatisfy({ "0123456789abcdef".contains($0) }) else {
            throw CloudOutboundSpoolError.corruptRow(
                seq: row.seq, detail: "logical metadata is incomplete")
        }
        guard let frameBytes = row.sealedEnvelopeBytes else { return logical }
        let frame = try CloudCanonicalJSON.parseStrict(frameBytes)
        guard case .object(let frameFields) = frame,
            Set(frameFields.keys) == ["type", "envelope"],
            case .string(let type)? = frameFields["type"], type == "publish",
            case .object(let envelope)? = frameFields["envelope"],
            Set(envelope.keys) == ["v", "ch", "seq", "ts", "class", "key_id", "nonce",
                                   "ct", "sender", "sig"] else {
            throw CloudOutboundSpoolError.corruptRow(seq: row.seq, detail: "invalid publish frame")
        }
        guard case .int(let version)? = envelope["v"], version == 1,
              case .int(let sequence)? = envelope["seq"], sequence == row.seq,
              case .int(let timestamp)? = envelope["ts"], timestamp >= 0,
              case .string(let frameChannel)? = envelope["ch"], frameChannel == channel,
              case .string(let frameSender)? = envelope["sender"], frameSender == sender,
              case .string(let frameKeyID)? = envelope["key_id"], frameKeyID == keyID,
              case .string(let frameClass)? = envelope["class"], frameClass == envelopeClass,
              case .string(let nonce)? = envelope["nonce"], canonicalBase64(nonce, bytes: 12),
              case .string(let ciphertext)? = envelope["ct"], canonicalBase64(ciphertext, minimumBytes: 16),
              case .string(let signature)? = envelope["sig"], canonicalBase64(signature, bytes: 64)
        else {
            throw CloudOutboundSpoolError.corruptRow(
                seq: row.seq, detail: "publish frame does not match row identity")
        }
        return logical
    }

    private static func canonicalBase64(
        _ value: String, bytes: Int? = nil, minimumBytes: Int = 0
    ) -> Bool {
        guard let decoded = Data(base64Encoded: value),
              decoded.base64EncodedString() == value,
              bytes.map({ decoded.count == $0 }) ?? true,
              decoded.count >= minimumBytes else { return false }
        return true
    }

    private func raiseAlertIfCrossed(_ dimension: CloudSpoolOccupancyDimension,
                                     occupancy: Int, cap: Int) {
        let level: CloudSpoolOccupancyAlert?
        if occupancy * 10 >= cap * 9 {
            level = .page90
        } else if occupancy * 10 >= cap * 8 {
            level = .warning80
        } else {
            level = nil
        }
        let previous = alertLevels[dimension]
        if let level, previous == nil || previous! < level, let runtime {
            metrics.recordOccupancyAlert(runtime: runtime, dimension: dimension, level: level)
        }
        alertLevels[dimension] = level
    }
}
