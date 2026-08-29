import Foundation

/// The Mac's durable outbound spool for the cloud protocol (design §6.1, §6.2, §6.6).
///
/// Every byte the Mac publishes — s/t/orch stream frames and ctlr control responses — passes
/// through one spool with one shared sequence counter, drained by a single publisher expressed
/// as a Swift actor. The spool's three invariants, in the order they tend to be violated:
///
/// 1. **A seq, once reserved, is never reused** (§6.1.1) — not after a burn, not after a crash,
///    not after coalescing. The counter only moves forward, and recovery burns rather than
///    deletes, so the history of what was reserved is never rewritten.
/// 2. **The queue is not per-channel** (§6.1.3). The publisher takes the lowest non-terminal
///    seq; a larger seq must not be sent while any smaller seq is still reserved/ready/sent,
///    even across different channels. Head-of-line release comes only from terminality —
///    the §6.2 30-second burn — never from reordering.
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
public enum CloudSpoolChannel: String, CaseIterable, Equatable, Sendable {
    case s, t, orch, ctl, ctlr

    /// ctl/ctlr — the channels §6.1.5 says never coalesce and §6.2 burns when ownerless.
    public var isControl: Bool { self == .ctl || self == .ctlr }
}

/// §6.1 row states. `acked`, `rejected` and `burned` are terminal; a terminal row never
/// becomes live again (§6.2: late acks for burned rows are telemetry, not resurrection).
public enum CloudSpoolRowState: String, CaseIterable, Equatable, Sendable {
    case reserved, ready, sent, acked, rejected, burned

    public var isTerminal: Bool {
        self == .acked || self == .rejected || self == .burned
    }
}

/// Why a row was burned. Burns are the only path from a live state to `burned`, and every
/// call site names its reason so tests and telemetry can tell a crash-recovery burn from an
/// attempt-cap burn without guessing.
public enum CloudSpoolBurnReason: String, Equatable, Sendable {
    /// §6.1.2 — a crash left a reserved row with no sealed bytes; recovery burns it durably
    /// before any higher seq is processed.
    case recoveredUnsealedReservation
    /// §6.6 GC — a reservation that was never sealed within the stale-reservation window.
    case staleReservation
    /// §6.2 — the attempt window (first_sent + 30s) elapsed with no correlated ack/error.
    case attemptCapExpired
    /// §6.2 — after restart old continuous instants cannot be compared, so every sent row
    /// burns to release head-of-line.
    case restartSentUncertain
    /// §6.2 — a ctl/ctlr ready row whose owner did not survive the restart.
    case ownerlessControlAtRestart
    /// A never-sent ready stream row whose wall-clock ts was no longer fresh at restart
    /// (§6.2 permits normal sending only for still-fresh ready streams; see the freshness
    /// note on `CloudSpoolLimits.readyFreshnessSeconds`).
    case staleReadyAtRestart
    /// §6.1.5 — the old snapshot row burned by coalescing, before the replacement reserve.
    case replacedByCoalescing
}

/// §6.2's attempt marker: a burn at the attempt cap (or the restart burn of a sent row)
/// records that transport delivery is uncertain — the bytes may or may not have arrived.
public enum CloudSpoolAttemptOutcome: String, Equatable, Sendable {
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
    /// §6.2 restart: a never-sent ready *stream* row may still be sent normally only while its
    /// wall-clock ts is fresh. The quoted text names no window; this is a local constant.
    public var readyFreshnessSeconds: TimeInterval = 300
    /// Upper bound on terminal-row deletions per GC batch (bounded batch, §6.6).
    public var gcBatchLimit = 256

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
    /// §6.1.5 — ctl/ctlr never coalesce.
    case coalesceControlChannelForbidden(seq: Int64, channel: CloudSpoolChannel)
    /// Snapshot coalescing is defined only for the snapshot channel `s`.
    case coalesceNotSnapshotChannel(seq: Int64, channel: CloudSpoolChannel)
    /// §6.1.5 — a row that has ever been sent must not coalesce.
    case coalesceAfterSend(seq: Int64)
    /// Only a ready row may coalesce (a reserved or terminal row may not).
    case coalesceNotReady(seq: Int64, state: CloudSpoolRowState)
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
}

/// What one publisher step did. The publisher only ever touches the lowest non-terminal seq
/// (§6.1.3), so "blocked" names the head that is in the way rather than offering a detour.
public enum CloudSpoolSendDisposition: Equatable, Sendable {
    case idle
    case sent(seq: Int64)
    /// §6.2 reconnect: the same sealed bytes were handed to the transport again.
    case resent(seq: Int64)
    /// The head seq is reserved and unsealed; nothing may send past it.
    case blockedAwaitingSeal(headSeq: Int64)
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
    public let runtime: CloudSpoolRuntime
    private let store: CloudSpoolStore
    private let clock: CloudSpoolClock
    private let metrics: CloudSpoolMetrics
    private let limits: CloudSpoolLimits
    private var state: CloudSpoolPersistedState
    private var lastGCContinuous: Duration
    private var alertLevels: [CloudSpoolOccupancyDimension: CloudSpoolOccupancyAlert] = [:]

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
                runtime: CloudSpoolRuntime = .mac,
                limits: CloudSpoolLimits = CloudSpoolLimits(),
                liveOwnerIDs: Set<String> = []) throws {
        self.store = store
        self.clock = clock
        self.metrics = metrics
        self.runtime = runtime
        self.limits = limits

        var working = try store.load()
        let openContinuous = clock.continuousNow
        let openWall = clock.wallNow
        func corrupt(_ error: CloudOutboundSpoolError) -> CloudOutboundSpoolError {
            metrics.recordStateStoreCorrupt(store: .macSpool)
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
            do {
                parsed = try CloudCanonicalJSON.parseStrict(row.logicalRecordCanonicalBytes)
            } catch {
                throw corrupt(.corruptRow(
                    seq: row.seq, detail: "logical record bytes are not canonical JSON"))
            }
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
            Self.burn(&working.rows[index], reason: .recoveredUnsealedReservation, outcome: nil)
        }
        // §6.2: old continuous instants are incomparable after restart — every sent row
        // burns (transport uncertain) to release head-of-line.
        for index in working.rows.indices where working.rows[index].state == .sent {
            Self.burn(&working.rows[index], reason: .restartSentUncertain,
                      outcome: .transportUncertain)
        }
        // §6.2: ownerless ctl/ctlr ready rows burn.
        for index in working.rows.indices
        where working.rows[index].state == .ready && working.rows[index].channel.isControl {
            let owner = working.rows[index].ownerID
            if owner == nil || !liveOwnerIDs.contains(owner!) {
                Self.burn(&working.rows[index], reason: .ownerlessControlAtRestart, outcome: nil)
            }
        }
        // §6.2: a never-sent ready stream row may continue only while its wall-clock ts is
        // still fresh; anything staler burns rather than shipping a stale snapshot.
        for index in working.rows.indices
        where working.rows[index].state == .ready && !working.rows[index].channel.isControl {
            if openWall.timeIntervalSince(working.rows[index].reservedAt)
                > limits.readyFreshnessSeconds {
                Self.burn(&working.rows[index], reason: .staleReadyAtRestart, outcome: nil)
            }
        }
        // Tombstone instants from the previous run are incomparable; restart retention now.
        for index in working.rows.indices
        where working.rows[index].logicalTombstoneContinuous != nil {
            working.rows[index].logicalTombstoneContinuous = openContinuous
        }

        try store.commit(working)
        self.state = working
        self.lastGCContinuous = openContinuous

        let rows = Self.storedRowCount(of: working)
        let bytes = Self.storedByteCount(of: working)
        metrics.recordRows(rows, runtime: runtime)
        metrics.recordBytes(bytes, runtime: runtime)
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
        metrics.recordAdmissionRefusal(runtime: runtime, reason: reason, scope: scope)
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
        try store.commit(working)
        state = working
    }

    // MARK: Publish (the single lease)

    /// One publisher step. The actor is the unique publisher lease (§6.1.6): it takes the
    /// lowest non-terminal seq and nothing else, regardless of channel (§6.1.3). Before
    /// looking at the head it burns any sent row whose attempt window has expired — the §6.2
    /// terminal burn that keeps a lost ack from blocking head-of-line forever.
    ///
    /// For a ready head the row is durably committed `sent` (first_sent and
    /// attempt_not_after = first_sent + 30s fixed on the first send) *before* the transport
    /// closure runs; a throwing transport leaves the row sent and rethrows (§6.1.4). For a
    /// still-in-window sent head, the same sealed bytes are handed to the transport again
    /// (§6.2 reconnect).
    static func lowestNonterminalIndex(in rows: [CloudSpoolRow]) -> Int? {
        rows.indices
            .filter { !rows[$0].state.isTerminal }
            .min { rows[$0].seq < rows[$1].seq }
    }

    public func sendNext(via transport: (Data) throws -> Void) throws -> CloudSpoolSendDisposition {
        try gcIfDue()
        try burnExpiredSentRows()
        guard let index = Self.lowestNonterminalIndex(in: state.rows)
        else { return .idle }
        let head = state.rows[index]
        switch head.state {
        case .reserved:
            return .blockedAwaitingSeal(headSeq: head.seq)
        case .ready:
            guard head.firstSentContinuous == nil else {
                throw CloudOutboundSpoolError.readyRowAlreadyHasFirstSent(seq: head.seq)
            }
            guard let sealedEnvelope = head.sealedEnvelopeBytes, !sealedEnvelope.isEmpty else {
                throw CloudOutboundSpoolError.missingSealedEnvelopeForSend(
                    seq: head.seq, state: head.state)
            }
            var working = state
            let firstSent = clock.continuousNow
            working.rows[index].firstSentContinuous = firstSent
            working.rows[index].attemptNotAfterContinuous = firstSent + limits.attemptWindow
            working.rows[index].state = .sent
            try store.commit(working)
            state = working
            do {
                try transport(sealedEnvelope)
            } catch {
                // §6.1.4: a throwing send leaves the row sent. The attempt window, not the
                // transport error, decides when it stops holding head-of-line.
                throw error
            }
            return .sent(seq: head.seq)
        case .sent:
            guard let sealedEnvelope = head.sealedEnvelopeBytes, !sealedEnvelope.isEmpty else {
                throw CloudOutboundSpoolError.missingSealedEnvelopeForSend(
                    seq: head.seq, state: head.state)
            }
            try transport(sealedEnvelope)
            return .resent(seq: head.seq)
        case .acked, .rejected, .burned:
            return .idle // unreachable: terminal rows were filtered out above
        }
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
                          outcome: .transportUncertain)
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
    public func settle(seq: Int64, _ kind: CloudSpoolSettleKind) throws -> CloudSpoolSettleDisposition {
        try gcIfDue()
        guard let index = state.rows.firstIndex(where: { $0.seq == seq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: seq)
        }
        let row = state.rows[index]
        switch row.state {
        case .sent:
            if kind == .viewerOffline && row.channel != .ctlr {
                throw CloudOutboundSpoolError.settleKindInvalidForChannel(seq: seq,
                                                                          channel: row.channel)
            }
            let terminal: CloudSpoolRowState = kind == .peerError ? .rejected : .acked
            try updateRow(at: index) { $0.state = terminal }
            publishOccupancy()
            return .settled(terminal)
        case .acked, .rejected, .burned:
            metrics.recordLateSettleTelemetry(runtime: runtime)
            return .lateIgnored
        case .reserved, .ready:
            throw CloudOutboundSpoolError.settleBeforeSend(seq: seq, state: row.state)
        }
    }

    // MARK: Coalescing

    /// §6.1.5: snapshot coalescing — burn the old row, then reserve the new one, in one
    /// committed transaction, only for a never-sent ready snapshot on the `s` channel. The
    /// replacement inherits channel, logical id, owner and recipient; the reserve runs the
    /// full admission (with the old row already burned, so its charge no longer occupies).
    /// Sent rows never coalesce; ctl/ctlr never coalesce.
    public func coalesceSnapshot(replacing oldSeq: Int64,
                                 with record: CloudJSONValue) throws -> Int64 {
        try gcBeforeAdmission()
        guard let index = state.rows.firstIndex(where: { $0.seq == oldSeq }) else {
            throw CloudOutboundSpoolError.rowNotFound(seq: oldSeq)
        }
        let old = state.rows[index]
        guard !old.channel.isControl else {
            throw CloudOutboundSpoolError.coalesceControlChannelForbidden(seq: oldSeq,
                                                                          channel: old.channel)
        }
        guard old.channel == .s else {
            throw CloudOutboundSpoolError.coalesceNotSnapshotChannel(seq: oldSeq,
                                                                     channel: old.channel)
        }
        guard old.state == .ready, old.firstSentContinuous == nil else {
            throw old.state == .sent || old.firstSentContinuous != nil
                ? CloudOutboundSpoolError.coalesceAfterSend(seq: oldSeq)
                : CloudOutboundSpoolError.coalesceNotReady(seq: oldSeq, state: old.state)
        }
        var working = state
        Self.burn(&working.rows[index], reason: .replacedByCoalescing, outcome: nil)
        let seq = try admitReservation(into: &working, channel: old.channel,
                                       logicalID: old.logicalID, ownerID: old.ownerID,
                                       recipient: old.recipient, record: record)
        try store.commit(working)
        state = working
        publishOccupancy()
        return seq
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
                Self.burn(&working.rows[index], reason: .staleReservation, outcome: nil)
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
                metrics.recordStateStoreGC(store: .macSpool, reason: .expired)
            }
            for _ in 0..<deleted {
                metrics.recordStateStoreGC(store: .macSpool, reason: .terminal)
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

    // MARK: Internals

    private func updateRow(at index: Int, _ mutate: (inout CloudSpoolRow) -> Void) throws {
        var working = state
        mutate(&working.rows[index])
        try store.commit(working)
        state = working
    }

    private static func burn(_ row: inout CloudSpoolRow,
                             reason: CloudSpoolBurnReason,
                             outcome: CloudSpoolAttemptOutcome?) {
        row.state = .burned
        row.burnReason = reason
        if let outcome { row.attemptOutcome = outcome }
    }

    private static func storedRowCount(of state: CloudSpoolPersistedState) -> Int {
        state.rows.count
    }

    private static func storedByteCount(of state: CloudSpoolPersistedState) -> Int {
        state.rows.reduce(0) { $0 + $1.chargedBytes }
    }

    /// Publishes the two gauges and raises the §6.6 occupancy alerts (80% warning, 90% page)
    /// on upward crossings, once per crossing.
    private func publishOccupancy() {
        let rows = Self.storedRowCount(of: state)
        let bytes = Self.storedByteCount(of: state)
        metrics.recordRows(rows, runtime: runtime)
        metrics.recordBytes(bytes, runtime: runtime)
        raiseAlertIfCrossed(.rows, occupancy: rows, cap: limits.globalRowCap)
        raiseAlertIfCrossed(.bytes, occupancy: bytes, cap: limits.globalByteCap)
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
        if let level, previous == nil || previous! < level {
            metrics.recordOccupancyAlert(runtime: runtime, dimension: dimension, level: level)
        }
        alertLevels[dimension] = level
    }
}
