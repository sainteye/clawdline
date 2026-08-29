import Foundation

/// This state deliberately remains independent from the `EpochGuardState` computed by
/// `EpochGuard`, so the clock and ledger units can compile and verify in isolation. A future
/// wiring seam must map between them explicitly; until that seam exists, there is no compile-time
/// guarantee that the clock guard's computed state reaches this ledger consumer.
public enum CloudEpochGuardState: Sendable {
    case uncertain
    case ready
}

public enum CloudCommandLedgerState: String, Codable, Sendable {
    case reserved
    case inProgress = "in_progress"
    case completed
}

public struct CloudCommandLedgerKey: Hashable, Codable, Sendable {
    public let viewerSender: String
    public let requestID: String

    public init(viewerSender: String, requestID: String) {
        self.viewerSender = viewerSender
        self.requestID = requestID
    }
}

public enum CloudCommandOutcomeCode: String, Codable, Sendable {
    case succeeded
    case failed
    case internalError = "internal"
    case busy
    case rateLimited = "rate_limited"
    case unavailable
}

public struct CloudCommandNormalizedOutcome: Codable, Equatable, Sendable {
    public let code: CloudCommandOutcomeCode
    public let payload: Data?

    public init(code: CloudCommandOutcomeCode, payload: Data? = nil) {
        self.code = code
        self.payload = payload
    }
}

public struct CloudCommandLedgerRow: Codable, Equatable, Sendable {
    public let key: CloudCommandLedgerKey
    public let requestSHA256: Data
    public let replyKeyID: String
    public let recipientDeviceID: String
    public let deadlineAt: Date
    public var state: CloudCommandLedgerState
    public var normalizedOutcome: CloudCommandNormalizedOutcome?
    public let createdAt: Date
    public var effectNotAfterContinuous: UInt64?
    public var effectStartedAt: Date?
    public var completedAt: Date?
    public let expiresAt: Date
    public var retainedElapsedMilliseconds: UInt64
    public var elapsedBootID: String
    public var elapsedCheckpointContinuous: UInt64?

    public init(
        key: CloudCommandLedgerKey,
        requestSHA256: Data,
        replyKeyID: String,
        recipientDeviceID: String,
        deadlineAt: Date,
        state: CloudCommandLedgerState,
        normalizedOutcome: CloudCommandNormalizedOutcome?,
        createdAt: Date,
        effectNotAfterContinuous: UInt64?,
        effectStartedAt: Date?,
        completedAt: Date?,
        expiresAt: Date,
        retainedElapsedMilliseconds: UInt64,
        elapsedBootID: String,
        elapsedCheckpointContinuous: UInt64?
    ) {
        self.key = key
        self.requestSHA256 = requestSHA256
        self.replyKeyID = replyKeyID
        self.recipientDeviceID = recipientDeviceID
        self.deadlineAt = deadlineAt
        self.state = state
        self.normalizedOutcome = normalizedOutcome
        self.createdAt = createdAt
        self.effectNotAfterContinuous = effectNotAfterContinuous
        self.effectStartedAt = effectStartedAt
        self.completedAt = completedAt
        self.expiresAt = expiresAt
        self.retainedElapsedMilliseconds = retainedElapsedMilliseconds
        self.elapsedBootID = elapsedBootID
        self.elapsedCheckpointContinuous = elapsedCheckpointContinuous
    }
}

public struct CloudCommandLedgerRequest: Sendable {
    public let key: CloudCommandLedgerKey
    public let requestSHA256: Data
    /// Transient authenticated plaintext. The ledger deliberately never persists this value.
    public let rawReplyKey: Data
    public let replyKeyID: String
    public let recipientDeviceID: String
    public let deadlineAt: Date
    public let effectNotAfterContinuous: UInt64

    public init(
        viewerSender: String,
        requestID: String,
        requestSHA256: Data,
        rawReplyKey: Data,
        replyKeyID: String,
        recipientDeviceID: String,
        deadlineAt: Date,
        effectNotAfterContinuous: UInt64
    ) {
        self.key = CloudCommandLedgerKey(viewerSender: viewerSender, requestID: requestID)
        self.requestSHA256 = requestSHA256
        self.rawReplyKey = rawReplyKey
        self.replyKeyID = replyKeyID
        self.recipientDeviceID = recipientDeviceID
        self.deadlineAt = deadlineAt
        self.effectNotAfterContinuous = effectNotAfterContinuous
    }
}

public protocol CloudCommandLedgerStore: AnyObject {
    func transaction<T>(_ body: (inout [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws -> T) rethrows -> T
    func persistedBytes() throws -> Data
}

public final class InMemoryCloudCommandLedgerStore: CloudCommandLedgerStore, @unchecked Sendable {
    private let lock = NSLock()
    private var rows: [CloudCommandLedgerKey: CloudCommandLedgerRow]

    public init(rows: [CloudCommandLedgerRow] = []) {
        self.rows = Dictionary(uniqueKeysWithValues: rows.map { ($0.key, $0) })
    }

    public func transaction<T>(
        _ body: (inout [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws -> T
    ) rethrows -> T {
        lock.lock()
        defer { lock.unlock() }
        return try body(&rows)
    }

    public func persistedBytes() throws -> Data {
        lock.lock()
        defer { lock.unlock() }
        let encoder = PropertyListEncoder()
        encoder.outputFormat = .binary
        let ordered = rows.values.sorted {
            if $0.key.viewerSender != $1.key.viewerSender {
                return $0.key.viewerSender < $1.key.viewerSender
            }
            return $0.key.requestID < $1.key.requestID
        }
        return try encoder.encode(ordered)
    }
}

public enum CloudLedgerActorRowsBucket: Int, CaseIterable, Sendable {
    case le10 = 10
    case le100 = 100
    case le500 = 500
    case le1000 = 1000
}

public enum CloudLedgerCapacityScope: String, CaseIterable, Sendable {
    case global
    case actor
}

public enum CloudLedgerCapacityReason: String, CaseIterable, Sendable {
    case rowCap = "row_cap"
    case fairness
    case corrupt
}

public enum CloudLedgerGCReason: String, CaseIterable, Sendable {
    case expired
    case revoked
}

public struct CloudLedgerCapacityLabel: Hashable, Sendable {
    public let scope: CloudLedgerCapacityScope
    public let reason: CloudLedgerCapacityReason

    public init(scope: CloudLedgerCapacityScope, reason: CloudLedgerCapacityReason) {
        self.scope = scope
        self.reason = reason
    }
}

public struct CloudCommandLedgerMetricsSnapshot: Sendable {
    public static let entriesName = "ledger_entries_total"
    public static let actorRowsBucketName = "ledger_actor_rows_bucket"
    public static let oldestAgeName = "ledger_oldest_age_seconds"
    public static let capacityRefusalsName = "ledger_capacity_refusals_total"
    public static let fairnessReserveUsedName = "ledger_fairness_reserve_used_total"
    public static let inProgressName = "ledger_in_progress_total"
    public static let gcName = "ledger_gc_total"
    public static let retentionOverdueName = "retention_overdue_seconds"
    public static let clockUncertainName = "clock_uncertain_seconds"

    public let ledgerEntriesTotal: Int
    public let actorRowsBuckets: [CloudLedgerActorRowsBucket: Int]
    public let oldestAgeSeconds: TimeInterval
    public let capacityRefusals: [CloudLedgerCapacityLabel: UInt64]
    public let fairnessReserveUsedTotal: UInt64
    public let inProgressTotal: Int
    public let gcTotal: [CloudLedgerGCReason: UInt64]
    public let retentionOverdueSeconds: TimeInterval
    public let clockUncertainSeconds: TimeInterval
}

public enum CloudCommandPreEffectRefusal: Equatable, Sendable {
    case busy
    case rateLimited
    case unavailable
}

public enum CloudCommandLedgerError: Error, Equatable, Sendable {
    case idempotencyCapacity(scope: CloudLedgerCapacityScope, reason: CloudLedgerCapacityReason)
    case idempotencyConflict
    case outcomeUnknown
    case deadlineExpired
    case epochUncertain
    case gateUnavailable
    case reservationReleased(CloudCommandPreEffectRefusal)
    case invalidTransition
    case corrupt
    case notFound
}

public enum CloudCommandReservationResult: Equatable, Sendable {
    case reserved(CloudCommandLedgerKey)
    case cached(CloudCommandNormalizedOutcome)
}

public struct CloudCommandLedgerClocks: Sendable {
    public let wallNow: @Sendable () -> Date
    public let continuousNow: @Sendable () -> UInt64
    public let bootID: @Sendable () -> String

    public init(
        wallNow: @escaping @Sendable () -> Date,
        continuousNow: @escaping @Sendable () -> UInt64,
        bootID: @escaping @Sendable () -> String
    ) {
        self.wallNow = wallNow
        self.continuousNow = continuousNow
        self.bootID = bootID
    }
}

public actor CloudCommandLedger {
    public static let retentionMilliseconds: UInt64 = 86_400_000
    public static let retentionSeconds: TimeInterval = 86_400
    public static let globalHardLimit = 10_000
    public static let fairnessReserveStart = 9_000
    public static let normalActorLimit = 1_000
    public static let fairnessActorLimit = 100
    public static let admissionGCBatchLimit = 100

    // These constants make the safety decisions explicit and easy to mutation-test.
    private static let checksDeadlineBeforeLookup = true
    private static let permitsEvictionAtCapacity = false
    private static let retriesRecoveredInProgress = false

    private enum AdmissionDecision {
        case reserved
        case cached(CloudCommandNormalizedOutcome)
        case wait
    }

    private enum WaiterWake {
        case retryAdmission
        case completed(CloudCommandNormalizedOutcome)
    }

    private let store: any CloudCommandLedgerStore
    private let clocks: CloudCommandLedgerClocks
    private var activeEffects: Set<CloudCommandLedgerKey> = []
    private var waiters: [CloudCommandLedgerKey: [CheckedContinuation<WaiterWake, Never>]] = [:]
    private var retentionCheckpointCursor: CloudCommandLedgerKey?
    private var capacityRefusals: [CloudLedgerCapacityLabel: UInt64] = [:]
    private var fairnessReserveUsedTotal: UInt64 = 0
    private var gcTotals: [CloudLedgerGCReason: UInt64] = [:]

    /// Opening a new process instance atomically recovers rows whose effect point-of-no-return
    /// was never committed. In-progress rows are deliberately left untouched.
    public init(store: any CloudCommandLedgerStore, clocks: CloudCommandLedgerClocks) {
        self.store = store
        self.clocks = clocks
        store.transaction { rows in
            rows = rows.filter { $0.value.state != .reserved }
        }
    }

    public func reserve(
        _ request: CloudCommandLedgerRequest,
        epochState: CloudEpochGuardState = .uncertain,
        permanentlyRevokedSenders: Set<String> = []
    ) async throws -> CloudCommandReservationResult {
        while true {
            let wallNow = clocks.wallNow()
            if Self.checksDeadlineBeforeLookup && wallNow >= request.deadlineAt {
                throw CloudCommandLedgerError.deadlineExpired
            }

            var gcReasons: [CloudLedgerGCReason: UInt64] = [:]
            // The inout store commits mutations even when a later capacity check throws.
            // Account for those already-committed removals on every exit path.
            defer { recordGC(gcReasons) }
            let protectedActiveEffects = activeEffects
            let decision: AdmissionDecision = try store.transaction { rows in
                if let existing = rows[request.key] {
                    guard existing.key == request.key else {
                        throw CloudCommandLedgerError.corrupt
                    }
                    if existing.requestSHA256 != request.requestSHA256 {
                        throw CloudCommandLedgerError.idempotencyConflict
                    }
                    if !Self.checksDeadlineBeforeLookup && wallNow >= request.deadlineAt {
                        throw CloudCommandLedgerError.deadlineExpired
                    }
                    switch existing.state {
                    case .reserved:
                        return .wait
                    case .inProgress:
                        if Self.retriesRecoveredInProgress {
                            rows.removeValue(forKey: request.key)
                        } else {
                            throw CloudCommandLedgerError.outcomeUnknown
                        }
                    case .completed:
                        guard let outcome = existing.normalizedOutcome else {
                            throw CloudCommandLedgerError.corrupt
                        }
                        return .cached(outcome)
                    }
                } else if !Self.checksDeadlineBeforeLookup && wallNow >= request.deadlineAt {
                    throw CloudCommandLedgerError.deadlineExpired
                }

                // §8.3: the bounded GC batch, capacity counts and candidate insert are one
                // store transaction. A separate garbageCollect() call would leave a stale-count
                // window between the two commits.
                _ = Self.collectEligibleRows(
                    in: &rows,
                    wallNow: wallNow,
                    epochState: epochState,
                    permanentlyRevokedSenders: permanentlyRevokedSenders,
                    protectedActiveEffects: protectedActiveEffects,
                    batchLimit: Self.admissionGCBatchLimit,
                    reasons: &gcReasons
                )

                let actorCount = rows.values.reduce(into: 0) {
                    if $1.key.viewerSender == request.key.viewerSender { $0 += 1 }
                }
                if actorCount >= Self.normalActorLimit {
                    try refuseCapacity(scope: .actor, reason: .rowCap)
                }
                if rows.count >= Self.globalHardLimit {
                    if Self.permitsEvictionAtCapacity, let oldest = rows.min(by: { $0.value.createdAt < $1.value.createdAt })?.key {
                        rows.removeValue(forKey: oldest)
                    } else {
                        try refuseCapacity(scope: .global, reason: .rowCap)
                    }
                }
                if rows.count >= Self.fairnessReserveStart && actorCount >= Self.fairnessActorLimit {
                    try refuseCapacity(scope: .actor, reason: .fairness)
                }

                let row = CloudCommandLedgerRow(
                    key: request.key,
                    requestSHA256: request.requestSHA256,
                    replyKeyID: request.replyKeyID,
                    recipientDeviceID: request.recipientDeviceID,
                    deadlineAt: request.deadlineAt,
                    state: .reserved,
                    normalizedOutcome: nil,
                    createdAt: wallNow,
                    effectNotAfterContinuous: request.effectNotAfterContinuous,
                    effectStartedAt: nil,
                    completedAt: nil,
                    expiresAt: wallNow.addingTimeInterval(Self.retentionSeconds),
                    retainedElapsedMilliseconds: 0,
                    elapsedBootID: clocks.bootID(),
                    elapsedCheckpointContinuous: clocks.continuousNow()
                )
                rows[request.key] = row
                if rows.count > Self.fairnessReserveStart {
                    fairnessReserveUsedTotal += 1
                }
                return .reserved
            }

            switch decision {
            case .reserved:
                return .reserved(request.key)
            case .cached(let outcome):
                return .cached(outcome)
            case .wait:
                let wake = await withCheckedContinuation { continuation in
                    waiters[request.key, default: []].append(continuation)
                }
                switch wake {
                case .completed(let outcome):
                    return .cached(outcome)
                case .retryAdmission:
                    // The previous owner removed the row before the effect point of no return.
                    // Re-run the complete admission path so reserved recovery, current deadline
                    // and capacity all apply afresh.
                    continue
                }
            }
        }
    }

    public func beginEffect(
        _ request: CloudCommandLedgerRequest,
        epochState: CloudEpochGuardState,
        latestRosterAndGateAllow: Bool
    ) throws {
        let wallNow = clocks.wallNow()
        if wallNow >= request.deadlineAt {
            try releaseReserved(request.key, digest: request.requestSHA256)
            throw CloudCommandLedgerError.deadlineExpired
        }
        if epochState == .uncertain {
            try releaseReserved(request.key, digest: request.requestSHA256)
            throw CloudCommandLedgerError.epochUncertain
        }
        if !latestRosterAndGateAllow {
            try releaseReserved(request.key, digest: request.requestSHA256)
            throw CloudCommandLedgerError.gateUnavailable
        }
        let continuousNow = clocks.continuousNow()
        let beganEffect: Bool
        do {
            beganEffect = try store.transaction { rows -> Bool in
                guard var row = rows[request.key] else {
                    throw CloudCommandLedgerError.notFound
                }
                guard row.requestSHA256 == request.requestSHA256 else {
                    throw CloudCommandLedgerError.idempotencyConflict
                }
                guard row.state == .reserved else {
                    throw row.state == .inProgress
                        ? CloudCommandLedgerError.outcomeUnknown
                        : CloudCommandLedgerError.invalidTransition
                }
                if let limit = row.effectNotAfterContinuous, continuousNow >= limit {
                    rows.removeValue(forKey: request.key)
                    return false
                }
                row.state = .inProgress
                row.effectNotAfterContinuous = nil
                row.effectStartedAt = wallNow
                rows[request.key] = row
                return true
            }
        } catch {
            resumeWaiters(for: request.key, wake: .retryAdmission)
            throw error
        }
        guard beganEffect else {
            resumeWaiters(for: request.key, wake: .retryAdmission)
            throw CloudCommandLedgerError.deadlineExpired
        }
        activeEffects.insert(request.key)
    }

    public func complete(
        _ request: CloudCommandLedgerRequest,
        outcome: CloudCommandNormalizedOutcome
    ) throws {
        let completedAt = clocks.wallNow()
        try store.transaction { rows in
            guard var row = rows[request.key] else {
                throw CloudCommandLedgerError.notFound
            }
            guard row.requestSHA256 == request.requestSHA256 else {
                throw CloudCommandLedgerError.idempotencyConflict
            }
            guard row.state == .inProgress else {
                throw CloudCommandLedgerError.invalidTransition
            }
            row.state = .completed
            row.normalizedOutcome = outcome
            row.completedAt = completedAt
            rows[request.key] = row
        }
        activeEffects.remove(request.key)
        resumeWaiters(for: request.key, wake: .completed(outcome))
    }

    public func cancelReservation(
        _ request: CloudCommandLedgerRequest,
        refusal: CloudCommandPreEffectRefusal
    ) throws {
        try releaseReserved(request.key, digest: request.requestSHA256)
        throw CloudCommandLedgerError.reservationReleased(refusal)
    }

    public func checkpointRetention(batchLimit: Int = .max) throws -> Int {
        guard batchLimit > 0 else { return 0 }
        let bootID = clocks.bootID()
        let continuousNow = clocks.continuousNow()
        let result = store.transaction { rows -> (count: Int, lastKey: CloudCommandLedgerKey?) in
            let orderedKeys = Self.orderedKeys(in: rows)
            guard !orderedKeys.isEmpty else { return (0, nil) }
            let startIndex: Int
            if let cursor = retentionCheckpointCursor,
               let cursorIndex = orderedKeys.firstIndex(of: cursor) {
                startIndex = (cursorIndex + 1) % orderedKeys.count
            } else {
                startIndex = 0
            }
            let count = min(batchLimit, orderedKeys.count)
            let keys = (0..<count).map { orderedKeys[(startIndex + $0) % orderedKeys.count] }
            for key in keys {
                guard var row = rows[key] else { continue }
                if row.elapsedBootID != bootID {
                    row.elapsedBootID = bootID
                    row.elapsedCheckpointContinuous = nil
                    row.retainedElapsedMilliseconds += 0
                } else if let checkpoint = row.elapsedCheckpointContinuous {
                    if continuousNow >= checkpoint {
                        let deltaMilliseconds = (continuousNow - checkpoint) / 1_000_000
                        row.retainedElapsedMilliseconds = row.retainedElapsedMilliseconds.addingClamped(deltaMilliseconds)
                    }
                    row.elapsedCheckpointContinuous = continuousNow
                } else {
                    row.elapsedCheckpointContinuous = continuousNow
                }
                rows[key] = row
            }
            return (keys.count, keys.last)
        }
        retentionCheckpointCursor = result.lastKey
        return result.count
    }

    @discardableResult
    public func garbageCollect(
        epochState: CloudEpochGuardState,
        permanentlyRevokedSenders: Set<String> = [],
        batchLimit: Int
    ) throws -> Int {
        guard batchLimit > 0 else { return 0 }
        let wallNow = clocks.wallNow()
        var reasons: [CloudLedgerGCReason: UInt64] = [:]
        let protectedActiveEffects = activeEffects
        let removed = store.transaction { rows -> Int in
            Self.collectEligibleRows(
                in: &rows,
                wallNow: wallNow,
                epochState: epochState,
                permanentlyRevokedSenders: permanentlyRevokedSenders,
                protectedActiveEffects: protectedActiveEffects,
                batchLimit: batchLimit,
                reasons: &reasons
            )
        }
        recordGC(reasons)
        return removed
    }

    public func row(for key: CloudCommandLedgerKey) -> CloudCommandLedgerRow? {
        store.transaction { $0[key] }
    }

    public func rowCount() -> Int {
        store.transaction { $0.count }
    }

    /// Internal observation seam used by deterministic concurrency tests. This exposes only
    /// cardinality, never continuations or request contents.
    func coalescedWaiterCount(for key: CloudCommandLedgerKey) -> Int {
        waiters[key]?.count ?? 0
    }

    public func metricsSnapshot() -> CloudCommandLedgerMetricsSnapshot {
        let wallNow = clocks.wallNow()
        let continuousNow = clocks.continuousNow()
        let bootID = clocks.bootID()
        return store.transaction { rows in
            let actorCounts = Dictionary(grouping: rows.values, by: { $0.key.viewerSender }).mapValues(\.count)
            var buckets: [CloudLedgerActorRowsBucket: Int] = [:]
            for bucket in CloudLedgerActorRowsBucket.allCases {
                buckets[bucket] = actorCounts.values.filter { $0 <= bucket.rawValue }.count
            }
            let oldest = rows.values.map(\.createdAt).min()
            let retentionOverdue = rows.values
                .filter { $0.state != .reserved }
                .map { max(0, wallNow.timeIntervalSince($0.expiresAt)) }
                .max() ?? 0
            let clockUncertain = rows.values.map { row -> TimeInterval in
                let wallElapsed = max(0, wallNow.timeIntervalSince(row.createdAt))
                var verifiedMilliseconds = row.retainedElapsedMilliseconds
                if row.elapsedBootID == bootID,
                   let checkpoint = row.elapsedCheckpointContinuous,
                   continuousNow >= checkpoint {
                    verifiedMilliseconds = verifiedMilliseconds.addingClamped(
                        (continuousNow - checkpoint) / 1_000_000
                    )
                }
                return max(0, wallElapsed - TimeInterval(verifiedMilliseconds) / 1_000)
            }.max() ?? 0
            return CloudCommandLedgerMetricsSnapshot(
                ledgerEntriesTotal: rows.count,
                actorRowsBuckets: buckets,
                oldestAgeSeconds: oldest.map { max(0, wallNow.timeIntervalSince($0)) } ?? 0,
                capacityRefusals: capacityRefusals,
                fairnessReserveUsedTotal: fairnessReserveUsedTotal,
                inProgressTotal: rows.values.filter { $0.state == .inProgress }.count,
                gcTotal: gcTotals,
                retentionOverdueSeconds: retentionOverdue,
                clockUncertainSeconds: clockUncertain
            )
        }
    }

    private func refuseCapacity(
        scope: CloudLedgerCapacityScope,
        reason: CloudLedgerCapacityReason
    ) throws -> Never {
        capacityRefusals[CloudLedgerCapacityLabel(scope: scope, reason: reason), default: 0] += 1
        throw CloudCommandLedgerError.idempotencyCapacity(scope: scope, reason: reason)
    }

    private func removeReserved(_ key: CloudCommandLedgerKey, digest: Data) throws {
        try store.transaction { rows in
            guard let row = rows[key] else {
                throw CloudCommandLedgerError.notFound
            }
            guard row.requestSHA256 == digest else {
                throw CloudCommandLedgerError.idempotencyConflict
            }
            guard row.state == .reserved else {
                throw CloudCommandLedgerError.invalidTransition
            }
            rows.removeValue(forKey: key)
        }
    }

    private func releaseReserved(_ key: CloudCommandLedgerKey, digest: Data) throws {
        defer { resumeWaiters(for: key, wake: .retryAdmission) }
        try removeReserved(key, digest: digest)
    }

    private func recordGC(_ reasons: [CloudLedgerGCReason: UInt64]) {
        for (reason, count) in reasons {
            gcTotals[reason, default: 0] += count
        }
    }

    private func resumeWaiters(for key: CloudCommandLedgerKey, wake: WaiterWake) {
        let continuations = waiters.removeValue(forKey: key) ?? []
        for continuation in continuations {
            continuation.resume(returning: wake)
        }
    }

    private static func orderedKeys(
        in rows: [CloudCommandLedgerKey: CloudCommandLedgerRow]
    ) -> [CloudCommandLedgerKey] {
        rows.keys.sorted { lhs, rhs in
            let left = rows[lhs]!
            let right = rows[rhs]!
            if left.createdAt != right.createdAt { return left.createdAt < right.createdAt }
            if lhs.viewerSender != rhs.viewerSender { return lhs.viewerSender < rhs.viewerSender }
            return lhs.requestID < rhs.requestID
        }
    }

    private static func collectEligibleRows(
        in rows: inout [CloudCommandLedgerKey: CloudCommandLedgerRow],
        wallNow: Date,
        epochState: CloudEpochGuardState,
        permanentlyRevokedSenders: Set<String>,
        protectedActiveEffects: Set<CloudCommandLedgerKey>,
        batchLimit: Int,
        reasons: inout [CloudLedgerGCReason: UInt64]
    ) -> Int {
        var removedCount = 0
        for key in orderedKeys(in: rows) where removedCount < batchLimit {
            guard let row = rows[key], !protectedActiveEffects.contains(key) else { continue }
            if permanentlyRevokedSenders.contains(key.viewerSender) {
                rows.removeValue(forKey: key)
                reasons[.revoked, default: 0] += 1
                removedCount += 1
                continue
            }
            guard epochState == .ready, row.state != .reserved else { continue }
            let wallEligible = wallNow >= row.expiresAt
            let elapsedEligible = row.retainedElapsedMilliseconds >= retentionMilliseconds
            if wallEligible && elapsedEligible {
                rows.removeValue(forKey: key)
                reasons[.expired, default: 0] += 1
                removedCount += 1
            }
        }
        return removedCount
    }
}

private extension UInt64 {
    func addingClamped(_ other: UInt64) -> UInt64 {
        let (sum, overflow) = addingReportingOverflow(other)
        return overflow ? .max : sum
    }
}
