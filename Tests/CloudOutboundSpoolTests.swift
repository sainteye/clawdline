import Foundation

// Tests for CloudOutboundSpool (design §6.1 / §6.2 / §6.6). No @main, no top-level code, no
// sleeps, no network. Persistence and both clocks are injected fakes; every "crash" drops the
// actor and reopens the same store, and every "restart" assertion reads the store, not the
// actor, so it proves durability rather than memory.

private struct CloudOutboundSpoolTestFailure: Error, CustomStringConvertible {
    let check: String
    var description: String { "CloudOutboundSpoolTests failed: \(check)" }
}

private struct SpoolTransportBoom: Error {}
private struct SpoolInjectedStoreFailure: Error {}

// MARK: - Injected fakes

private final class SpoolMemoryStore: CloudSpoolStore, @unchecked Sendable {
    private let lock = NSLock()
    private var saved: CloudSpoolPersistedState
    private var pendingCommitFailures = 0

    init(initial: CloudSpoolPersistedState = CloudSpoolPersistedState()) {
        saved = initial
    }

    func load() throws -> CloudSpoolPersistedState {
        lock.lock(); defer { lock.unlock() }
        return saved
    }

    func commit(_ state: CloudSpoolPersistedState) throws {
        lock.lock(); defer { lock.unlock() }
        if pendingCommitFailures > 0 {
            pendingCommitFailures -= 1
            throw SpoolInjectedStoreFailure()
        }
        saved = state
    }

    var snapshot: CloudSpoolPersistedState {
        lock.lock(); defer { lock.unlock() }
        return saved
    }

    func tamper(_ mutate: (inout CloudSpoolPersistedState) -> Void) {
        lock.lock(); defer { lock.unlock() }
        mutate(&saved)
    }

    func failNextCommits(_ count: Int) {
        lock.lock(); defer { lock.unlock() }
        pendingCommitFailures = count
    }

    func row(_ seq: Int64) -> CloudSpoolRow? {
        snapshot.rows.first { $0.seq == seq }
    }
}

private final class SpoolFakeClock: CloudSpoolClock, @unchecked Sendable {
    private let lock = NSLock()
    private var continuous = Duration.zero
    private var wall = Date(timeIntervalSince1970: 1_756_000_000)

    var continuousNow: Duration {
        lock.lock(); defer { lock.unlock() }
        return continuous
    }

    var wallNow: Date {
        lock.lock(); defer { lock.unlock() }
        return wall
    }

    /// Advances both clocks together, the way real time passes within one run.
    func advance(_ duration: Duration) {
        lock.lock(); defer { lock.unlock() }
        continuous += duration
        let seconds = Double(duration.components.seconds)
            + Double(duration.components.attoseconds) / 1e18
        wall = wall.addingTimeInterval(seconds)
    }
}

private final class SpoolRecordingMetrics: CloudSpoolMetrics, @unchecked Sendable {
    private let lock = NSLock()
    private var rows = [CloudSpoolRuntime: Int]()
    private var bytes = [CloudSpoolRuntime: Int]()
    private var refusals = [String: Int]()
    private var alerts = [String: Int]()
    private var stateStoreGC = [String: Int]()
    private var stateStoreCorrupt = [CloudStateStore: Int]()
    private var lateSettleCount = 0

    func recordRows(_ value: Int, runtime: CloudSpoolRuntime) {
        lock.lock(); defer { lock.unlock() }
        rows[runtime] = value
    }

    func recordBytes(_ value: Int, runtime: CloudSpoolRuntime) {
        lock.lock(); defer { lock.unlock() }
        bytes[runtime] = value
    }

    func recordAdmissionRefusal(runtime: CloudSpoolRuntime,
                                reason: CloudSpoolRefusalReason,
                                scope: CloudSpoolRefusalScope) {
        lock.lock(); defer { lock.unlock() }
        let key = "\(runtime.rawValue)|\(reason.rawValue)|\(scope.rawValue)"
        refusals[key, default: 0] += 1
    }

    func recordOccupancyAlert(runtime: CloudSpoolRuntime,
                              dimension: CloudSpoolOccupancyDimension,
                              level: CloudSpoolOccupancyAlert) {
        lock.lock(); defer { lock.unlock() }
        let key = "\(runtime.rawValue)|\(dimension.rawValue)|\(level.rawValue)"
        alerts[key, default: 0] += 1
    }

    func recordLateSettleTelemetry(runtime: CloudSpoolRuntime) {
        lock.lock(); defer { lock.unlock() }
        lateSettleCount += 1
    }

    func recordStateStoreGC(store: CloudStateStore, reason: CloudStateStoreGCReason) {
        lock.lock(); defer { lock.unlock() }
        stateStoreGC["\(store.rawValue)|\(reason.rawValue)", default: 0] += 1
    }

    func recordStateStoreCorrupt(store: CloudStateStore) {
        lock.lock(); defer { lock.unlock() }
        stateStoreCorrupt[store, default: 0] += 1
    }

    func rowsGauge(_ runtime: CloudSpoolRuntime) -> Int? {
        lock.lock(); defer { lock.unlock() }
        return rows[runtime]
    }

    func bytesGauge(_ runtime: CloudSpoolRuntime) -> Int? {
        lock.lock(); defer { lock.unlock() }
        return bytes[runtime]
    }

    func refusalCount(_ reason: CloudSpoolRefusalReason, _ scope: CloudSpoolRefusalScope,
                      runtime: CloudSpoolRuntime = .mac) -> Int {
        lock.lock(); defer { lock.unlock() }
        return refusals["\(runtime.rawValue)|\(reason.rawValue)|\(scope.rawValue)"] ?? 0
    }

    func alertCount(_ dimension: CloudSpoolOccupancyDimension, _ level: CloudSpoolOccupancyAlert,
                    runtime: CloudSpoolRuntime = .mac) -> Int {
        lock.lock(); defer { lock.unlock() }
        return alerts["\(runtime.rawValue)|\(dimension.rawValue)|\(level.rawValue)"] ?? 0
    }

    var lateSettles: Int {
        lock.lock(); defer { lock.unlock() }
        return lateSettleCount
    }

    func stateStoreGCCount(_ store: CloudStateStore, _ reason: CloudStateStoreGCReason) -> Int {
        lock.lock(); defer { lock.unlock() }
        return stateStoreGC["\(store.rawValue)|\(reason.rawValue)"] ?? 0
    }

    func stateStoreCorruptCount(_ store: CloudStateStore) -> Int {
        lock.lock(); defer { lock.unlock() }
        return stateStoreCorrupt[store] ?? 0
    }
}

private final class SpoolTransportLog: @unchecked Sendable {
    private let lock = NSLock()
    private var recorded = [Data]()

    func record(_ payload: Data) {
        lock.lock(); defer { lock.unlock() }
        recorded.append(payload)
    }

    var payloads: [Data] {
        lock.lock(); defer { lock.unlock() }
        return recorded
    }
}

/// One store+clock+metrics bundle; `open` is the crash/restart seam — dropping a spool and
/// calling `open` again is a process restart over the same durable state.
private struct SpoolWorld {
    let store = SpoolMemoryStore()
    let clock = SpoolFakeClock()
    let metrics = SpoolRecordingMetrics()

    func open(liveOwners: Set<String> = [],
              limits: CloudSpoolLimits = CloudSpoolLimits()) throws -> CloudOutboundSpool {
        try CloudOutboundSpool(store: store, clock: clock, metrics: metrics,
                               runtime: .mac, limits: limits,
                               liveOwnerIDs: liveOwners)
    }
}

// MARK: - Harness

private final class SpoolTestHarness {
    var checks = 0

    private func recordPassed(_ name: String) {
        checks += 1
        if ProcessInfo.processInfo.environment["SPOOL_TEST_TRACE"] == name {
            print("✓ \(name)")
        }
    }

    func check(_ condition: Bool, _ name: String) throws {
        guard condition else { throw CloudOutboundSpoolTestFailure(check: name) }
        recordPassed(name)
    }

    func expectSpoolError(_ expected: CloudOutboundSpoolError, _ name: String,
                          _ body: () async throws -> Void) async throws {
        try await expectSpoolError(name, matching: { $0 == expected }, body)
    }

    func expectSpoolError(_ name: String,
                          matching matches: (CloudOutboundSpoolError) -> Bool,
                          _ body: () async throws -> Void) async throws {
        do {
            try await body()
        } catch let error as CloudOutboundSpoolError {
            guard matches(error) else {
                throw CloudOutboundSpoolTestFailure(check: "\(name) (wrong error: \(error))")
            }
            recordPassed(name)
            return
        }
        throw CloudOutboundSpoolTestFailure(check: "\(name) (expected a typed error, none thrown)")
    }
}

// MARK: - Record helpers (exact charged_bytes arithmetic)

/// `{"b":"<payload>"}` canonicalizes to exactly `payload.count + 8` bytes for plain ASCII.
private func payloadRecord(ofChargedBytes charged: Int) -> CloudJSONValue {
    precondition(charged >= 9, "payload record needs at least 9 charged bytes")
    return .object(["b": .string(String(repeating: "a", count: charged - 8))])
}

private func tinyRecord(_ value: Int64) -> CloudJSONValue { .int(value) }

private let sealedEnvelopeStub = Data("sealed-envelope".utf8)

private func reserveSealed(_ spool: CloudOutboundSpool, channel: CloudSpoolChannel,
                           logicalID: String, ownerID: String? = nil, recipient: String,
                           record: CloudJSONValue) async throws -> Int64 {
    let seq = try await spool.reserve(channel: channel, logicalID: logicalID,
                                      ownerID: ownerID, recipient: recipient, record: record)
    try await spool.seal(seq: seq, sealedEnvelope: sealedEnvelopeStub)
    return seq
}

// MARK: - Tests

/// Metric names and label domains are the exact §6.6 strings, closed at the type level; the
/// limits defaults are the exact §6.6 table; charged_bytes excludes its own member.
private func testMetricsDomainsAndLimits(_ h: SpoolTestHarness) throws {
    try h.check(CloudSpoolMetricName.allCases.map { $0.rawValue } == [
        "cloud_spool_rows",
        "cloud_spool_bytes",
        "cloud_spool_admission_refusals_total",
        "state_store_gc_total",
        "state_store_corrupt_total",
    ], "metric names include both required state_store counters")
    try h.check(CloudSpoolMetricName.rows.rawValue == "cloud_spool_rows",
                "metric name cloud_spool_rows")
    try h.check(CloudSpoolMetricName.bytes.rawValue == "cloud_spool_bytes",
                "metric name cloud_spool_bytes")
    try h.check(CloudSpoolMetricName.admissionRefusalsTotal.rawValue
                == "cloud_spool_admission_refusals_total",
                "metric name cloud_spool_admission_refusals_total")
    try h.check(CloudSpoolRuntime.allCases.map { $0.rawValue } == ["mac", "viewer"],
                "runtime label domain is exactly mac|viewer")
    try h.check(CloudSpoolRefusalReason.allCases.map { $0.rawValue }
                == ["row_cap", "byte_cap", "fairness", "corrupt", "gc_failed", "per_request_cap"],
                "reason label domain is exactly the six §6.6 reasons")
    try h.check(CloudSpoolRefusalScope.allCases.map { $0.rawValue }
                == ["global", "actor", "machine", "owner", "recipient"],
                "scope label domain is exactly the five §6.6 scopes")
    try h.check(CloudStateStore.allCases.map { $0.rawValue }
                == ["ctl_seen", "mac_spool", "viewer_spool", "ctlr_pending", "ctlr_seen"],
                "state_store store label domain is exactly the five §6.6 stores")
    try h.check(CloudStateStoreGCReason.allCases.map { $0.rawValue }
                == ["expired", "revoked", "terminal", "cancel", "heartbeat_timeout"],
                "state_store GC reason domain is exactly the five §6.6 reasons")

    let limits = CloudSpoolLimits()
    try h.check(limits.globalRowCap == 2000, "default global row cap 2,000")
    try h.check(limits.globalByteCap == 16_777_216, "default global byte cap 16 MiB")
    try h.check(limits.recipientRowCap == 200, "default recipient row cap 200")
    try h.check(limits.recipientByteCap == 2_097_152, "default recipient byte cap 2 MiB")
    try h.check(limits.fairnessRecipientRowCap == 20, "default fairness recipient row cap 20")
    try h.check(limits.fairnessRecipientByteCap == 262_144,
                "default fairness recipient byte cap 256 KiB")
    try h.check(limits.attemptWindow == .seconds(30), "attempt window 30s")
    try h.check(limits.tombstoneRetention == .seconds(600), "tombstone retention 10 min")
    try h.check(limits.gcInterval == .seconds(60), "gc interval 60s")

    // §6.6: charged_bytes is the canonical record WITHOUT the charged_bytes member itself.
    let withMember: CloudJSONValue = .object(["a": .int(1), "charged_bytes": .int(999)])
    let without: CloudJSONValue = .object(["a": .int(1)])
    try h.check(CloudOutboundSpool.chargedBytes(of: withMember)
                == CloudOutboundSpool.chargedBytes(of: without),
                "charged_bytes excludes its own top-level member")
    try h.check(CloudOutboundSpool.chargedBytes(of: without) == 7,
                "charged_bytes of {\"a\":1} is 7 canonical bytes")
    try h.check(CloudOutboundSpool.chargedBytes(of: payloadRecord(ofChargedBytes: 100)) == 100,
                "payloadRecord helper produces exact charged bytes")
}

/// §6.6 admission must reject records outside the canonical safe-integer domain before
/// canonical bytes, quota accounting, the counter, or a durable row can be produced.
private func testOutOfDomainIntegersRefusedWithoutPersistence(
    _ h: SpoolTestHarness
) async throws {
    let invalidIntegers: [Int64] = [
        CloudCanonicalJSON.maximumSafeInteger + 1,
        Int64.max,
    ]

    for value in invalidIntegers {
        let world = SpoolWorld()
        var spool: CloudOutboundSpool? = try world.open()
        try await h.expectSpoolError(
            .admissionRefused(reason: .corrupt, scope: .global),
            "out-of-domain integer \(value) gets a typed corrupt admission refusal"
        ) {
            _ = try await spool!.reserve(
                channel: .t, logicalID: "invalid-\(value)", ownerID: nil,
                recipient: "viewer", record: .object(["n": .int(value)]))
        }
        try h.check(world.store.snapshot == CloudSpoolPersistedState(),
                    "out-of-domain integer \(value) leaves no durable row or counter movement")
        try h.check(world.metrics.refusalCount(.corrupt, .global) == 1,
                    "out-of-domain integer \(value) increments the typed refusal metric")

        // Drop the actor and reopen the same store: this is the process-restart seam, not
        // an in-memory recover call. A refused input must not poison future opens.
        spool = nil
        spool = try world.open()
        try h.check(world.store.snapshot == CloudSpoolPersistedState(),
                    "store reopens cleanly after refusing out-of-domain integer \(value)")
    }
}

/// §6.6 hard caps measure the durable store, including terminal transport rows that are
/// still inside their tombstone-retention window.
private func testTerminalRowsOccupyHardCap(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.globalRowCap = 1
    limits.recipientRowCap = 10
    limits.fairnessRecipientRowCap = 10
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let terminal = try await reserveSealed(spool, channel: .s, logicalID: "terminal",
                                           recipient: "A", record: tinyRecord(1))
    _ = try await spool.sendNext { _ in }
    _ = try await spool.settle(seq: terminal, .delivered)
    try h.check(world.store.snapshot.rows.count == 1
                && world.store.row(terminal)?.state == .acked,
                "terminal transport row remains physically present during retention")
    try h.check(world.metrics.rowsGauge(.mac) == 1
                && world.metrics.bytesGauge(.mac) == 1,
                "both occupancy gauges include a retained terminal row")
    try await h.expectSpoolError(.admissionRefused(reason: .rowCap, scope: .global),
                                 "retained terminal row occupies the global hard cap") {
        _ = try await spool.reserve(channel: .t, logicalID: "blocked", ownerID: nil,
                                    recipient: "B", record: tinyRecord(2))
    }
    try await spool.recordLogicalTombstone(seq: terminal)
    world.clock.advance(.seconds(599))
    try await h.expectSpoolError(.admissionRefused(reason: .rowCap, scope: .global),
                                 "terminal row still occupies cap just before retention expires") {
        _ = try await spool.reserve(channel: .t, logicalID: "still-blocked", ownerID: nil,
                                    recipient: "B", record: tinyRecord(2))
    }
    world.clock.advance(.seconds(1))
    let replacement = try await spool.reserve(channel: .t, logicalID: "released", ownerID: nil,
                                               recipient: "B", record: tinyRecord(2))
    try h.check(replacement == 1 && world.store.row(terminal) == nil
                && world.store.snapshot.rows.count == 1,
                "pre-admission GC releases cap space exactly when retention expires")
}

/// The byte half of the hard cap also measures retained terminal rows, not only live work.
private func testTerminalBytesOccupyHardCap(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.globalByteCap = 1
    limits.recipientByteCap = 10
    limits.fairnessRecipientByteCap = 10
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let terminal = try await reserveSealed(spool, channel: .s, logicalID: "terminal-byte",
                                           recipient: "A", record: tinyRecord(1))
    _ = try await spool.sendNext { _ in }
    _ = try await spool.settle(seq: terminal, .delivered)
    try await h.expectSpoolError(.admissionRefused(reason: .byteCap, scope: .global),
                                 "retained terminal bytes occupy the global byte hard cap") {
        _ = try await spool.reserve(channel: .t, logicalID: "byte-blocked", ownerID: nil,
                                    recipient: "B", record: tinyRecord(2))
    }
}

/// §6.1: publisher head selection identifies the lowest non-terminal seq rather than
/// trusting array position. This pins the selector independently of load validation.
private func testHeadSelectorUsesLowestSeq(_ h: SpoolTestHarness) async throws {
    let source = SpoolWorld()
    let sourceSpool = try source.open()
    let low = try await reserveSealed(sourceSpool, channel: .s, logicalID: "low",
                                      recipient: "R", record: tinyRecord(0))
    _ = try await reserveSealed(sourceSpool, channel: .t, logicalID: "high",
                                recipient: "R", record: tinyRecord(1))
    var seeded = source.store.snapshot
    seeded.rows.reverse()
    let selected = CloudOutboundSpool.lowestNonterminalIndex(in: seeded.rows)
    try h.check(selected.map { seeded.rows[$0].seq } == low,
                "publisher head selector chooses the lowest non-terminal seq")
}

/// The persisted-state ascending-seq invariant is enforced at load; malformed ordering is
/// corruption and fails closed before recovery or publishing can normalize it.
private func testUnsortedPersistedStateFailsClosed(_ h: SpoolTestHarness) async throws {
    let source = SpoolWorld()
    let sourceSpool = try source.open()
    _ = try await reserveSealed(sourceSpool, channel: .s, logicalID: "low",
                                recipient: "R", record: tinyRecord(0))
    _ = try await reserveSealed(sourceSpool, channel: .t, logicalID: "high",
                                recipient: "R", record: tinyRecord(1))
    var seeded = source.store.snapshot
    seeded.rows.reverse()
    let store = SpoolMemoryStore(initial: seeded)
    let metrics = SpoolRecordingMetrics()
    try await h.expectSpoolError("unsorted persisted rows fail closed at initializer", matching: {
        if case .corruptRow(let seq, let detail) = $0 {
            return seq == 0 && detail == "rows are not strictly ascending by seq"
        }
        return false
    }) {
        _ = try CloudOutboundSpool(store: store, clock: SpoolFakeClock(), metrics: metrics)
    }
    try h.check(metrics.stateStoreCorruptCount(.macSpool) == 1,
                "unsorted mac spool increments state_store_corrupt_total")
}

/// §6.1.1: the counter is shared across channels and a seq, once reserved, is never reused —
/// not even when every earlier row has burned across a crash.
private func testSeqNeverReusedAndSharedCounter(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    var spool = try world.open()
    let s0 = try await spool.reserve(channel: .s, logicalID: "snap-a", ownerID: nil,
                                     recipient: "viewer-1", record: tinyRecord(1))
    let s1 = try await spool.reserve(channel: .ctlr, logicalID: "resp-a", ownerID: "viewer-1",
                                     recipient: "viewer-1", record: tinyRecord(2))
    try h.check(s0 == 0 && s1 == 1, "response and stream channels share one counter")

    // Crash with both rows still reserved; recovery burns them, and the counter refuses to
    // hand their seqs out again.
    spool = try world.open(liveOwners: ["viewer-1"])
    try h.check(world.store.row(0)?.state == .burned
                && world.store.row(0)?.burnReason == .recoveredUnsealedReservation,
                "crash-recovered reservation seq 0 is burned, not deleted")
    try h.check(world.store.row(1)?.state == .burned,
                "crash-recovered reservation seq 1 is burned, not deleted")
    let s2 = try await spool.reserve(channel: .s, logicalID: "snap-b", ownerID: nil,
                                     recipient: "viewer-1", record: tinyRecord(3))
    try h.check(s2 == 2, "post-burn reserve gets a strictly larger seq — never reused")
    try h.check(world.store.snapshot.nextSeq == 3, "counter is durable and monotonic")
}

/// §6.1.2: a crash between reserve and seal leaves a byteless reserved row; recovery burns it
/// durably before any higher seq is processed.
private func testCrashBeforeSealRecovery(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    var spool = try world.open()
    let unsealed = try await spool.reserve(channel: .s, logicalID: "snap-crash", ownerID: nil,
                                           recipient: "v", record: tinyRecord(1))
    let sealed = try await reserveSealed(spool, channel: .s, logicalID: "snap-ok",
                                         recipient: "v", record: tinyRecord(2))

    spool = try world.open()
    // The burn is already in the store before anything touches the higher seq: reading the
    // durable snapshot, not the actor, is the point.
    let recovered = world.store.row(unsealed)
    try h.check(recovered?.state == .burned
                && recovered?.burnReason == .recoveredUnsealedReservation
                && recovered?.sealedEnvelopeBytes == nil,
                "byteless reservation is durably burned at open")
    let log = SpoolTransportLog()
    let disposition = try await spool.sendNext { log.record($0) }
    try h.check(disposition == .sent(seq: sealed),
                "higher seq is processed only after the recovery burn")
    try h.check(log.payloads == [sealedEnvelopeStub], "recovered ready row sends its exact bytes")
}

/// §6.1.3: the queue is not per-channel — a smaller reserved seq on one channel blocks a
/// ready larger seq on a different channel.
private func testHeadOfLineAcrossChannels(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let ctlSeq = try await spool.reserve(channel: .ctl, logicalID: "cmd-1", ownerID: "viewer-1",
                                         recipient: "machine", record: tinyRecord(1))
    let snapSeq = try await reserveSealed(spool, channel: .s, logicalID: "snap-1",
                                          recipient: "viewer-1", record: tinyRecord(2))
    try h.check(ctlSeq < snapSeq, "the blocked head is the smaller seq")

    let log = SpoolTransportLog()
    let blocked = try await spool.sendNext { log.record($0) }
    try h.check(blocked == .blockedAwaitingSeal(headSeq: ctlSeq),
                "a ready larger seq on another channel must not send past a reserved smaller seq")
    try h.check(log.payloads.isEmpty, "nothing reached the transport while blocked")

    try await spool.seal(seq: ctlSeq, sealedEnvelope: sealedEnvelopeStub)
    let first = try await spool.sendNext { log.record($0) }
    try h.check(first == .sent(seq: ctlSeq), "the lowest non-terminal seq sends first")
    _ = try await spool.settle(seq: ctlSeq, .delivered)
    let second = try await spool.sendNext { log.record($0) }
    try h.check(second == .sent(seq: snapSeq),
                "the larger seq sends only after the smaller one is terminal")
}

/// §6.2: the attempt window is first_sent + 30s; within it reconnect resends the same sealed
/// bytes; at the cap a single transaction burns the row transport_uncertain and releases N+1.
private func testAttemptCapBurnReleasesHeadOfLine(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let n = try await reserveSealed(spool, channel: .s, logicalID: "snap-n",
                                    recipient: "v", record: tinyRecord(1))
    let log = SpoolTransportLog()
    _ = try await spool.sendNext { log.record($0) }
    let n1 = try await reserveSealed(spool, channel: .t, logicalID: "term-n1",
                                     recipient: "v", record: tinyRecord(2))

    let resent = try await spool.sendNext { log.record($0) }
    try h.check(resent == .resent(seq: n) && log.payloads.count == 2
                && log.payloads[1] == log.payloads[0],
                "reconnect within the window resends the same sealed bytes")

    world.clock.advance(.seconds(29))
    let stillResent = try await spool.sendNext { log.record($0) }
    try h.check(stillResent == .resent(seq: n), "29s after first send the row still resends")

    world.clock.advance(.seconds(1))
    let released = try await spool.sendNext { log.record($0) }
    let burned = world.store.row(n)
    try h.check(burned?.state == .burned
                && burned?.attemptOutcome == .transportUncertain
                && burned?.burnReason == .attemptCapExpired,
                "at the cap the sent row burns marked transport_uncertain")
    try h.check(released == .sent(seq: n1), "seq N+1 is allowed only after the terminal burn")
}

/// §6.2: a late ack for burned N is telemetry only — it revives nothing, settles nothing, and
/// blocks nothing.
private func testLateAckAfterBurn(_ h: SpoolTestHarness) async throws {
    // Rebuild the burned-N shape locally: N burned at the attempt cap, N+1 sent.
    let world = SpoolWorld()
    let spool = try world.open()
    let n = try await reserveSealed(spool, channel: .s, logicalID: "snap-n",
                                    recipient: "v", record: tinyRecord(1))
    let log = SpoolTransportLog()
    _ = try await spool.sendNext { log.record($0) }
    let n1 = try await reserveSealed(spool, channel: .t, logicalID: "term-n1",
                                     recipient: "v", record: tinyRecord(2))
    world.clock.advance(.seconds(30))
    _ = try await spool.sendNext { log.record($0) } // burns N, sends N+1

    let lateBefore = world.metrics.lateSettles
    let disposition = try await spool.settle(seq: n, .delivered)
    try h.check(disposition == .lateIgnored && world.metrics.lateSettles == lateBefore + 1,
                "late ack for burned N does not settle — telemetry only")
    try h.check(world.store.row(n)?.state == .burned
                && world.store.row(n)?.attemptOutcome == .transportUncertain,
                "late ack for burned N does not revive the row")
    _ = try await spool.settle(seq: n1, .delivered)
    let n2 = try await reserveSealed(spool, channel: .s, logicalID: "snap-n2",
                                     recipient: "v", record: tinyRecord(3))
    let next = try await spool.sendNext { log.record($0) }
    try h.check(next == .sent(seq: n2), "late ack for burned N does not block later seqs")
}

/// §6.1.4: the row is durably sent before the socket write, and a throwing send leaves it
/// sent — not back to ready.
private func testSendThrowLeavesRowSent(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let seq = try await reserveSealed(spool, channel: .s, logicalID: "snap-1",
                                      recipient: "v", record: tinyRecord(1))
    var thrown = false
    do {
        _ = try await spool.sendNext { _ in throw SpoolTransportBoom() }
    } catch is SpoolTransportBoom {
        thrown = true
    }
    try h.check(thrown, "the transport error propagates to the caller")
    let row = world.store.row(seq)
    try h.check(row?.state == .sent, "a throwing send leaves the row sent, not ready")
    try h.check(row?.firstSentContinuous != nil
                && row?.attemptNotAfterContinuous
                == row!.firstSentContinuous! + CloudSpoolLimits().attemptWindow,
                "first send durably fixed attempt_not_after = first_sent + 30s")
    let log = SpoolTransportLog()
    let resent = try await spool.sendNext { log.record($0) }
    try h.check(resent == .resent(seq: seq) && log.payloads == [sealedEnvelopeStub],
                "after the throw the same sealed bytes are resent")
}

/// A ready row has never been sent, so it cannot already carry first_sent. Persistence can
/// expose that inconsistent shape after restart; publishing must throw a typed invariant error
/// before committing `sent` or handing any frame to the transport.
private func testReadyRowWithFirstSentFailsClosed(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    var spool: CloudOutboundSpool? = try world.open()
    let seq = try await reserveSealed(spool!, channel: .s, logicalID: "bad-ready",
                                      recipient: "viewer", record: tinyRecord(1))
    spool = nil
    world.store.tamper { $0.rows[0].firstSentContinuous = .seconds(7) }
    spool = try world.open()

    let log = SpoolTransportLog()
    try await h.expectSpoolError(.readyRowAlreadyHasFirstSent(seq: seq),
                                 "ready row carrying first_sent fails closed before transport") {
        _ = try await spool!.sendNext { log.record($0) }
    }
    try h.check(world.store.row(seq)?.state == .ready && log.payloads.isEmpty,
                "ready/first_sent invariant failure neither commits sent nor emits a frame")
}

/// §6.1.5: snapshot coalescing burns the never-sent ready snapshot and reserves a fresh seq;
/// sent rows never coalesce; ctl/ctlr never coalesce.
private func testCoalescing(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let old = try await reserveSealed(spool, channel: .s, logicalID: "snap",
                                      recipient: "viewer-1", record: tinyRecord(1))
    let replacement = try await spool.coalesceSnapshot(replacing: old, with: tinyRecord(22))
    try h.check(replacement > old, "coalescing reserves a strictly newer seq")
    let oldRow = world.store.row(old)
    try h.check(oldRow?.state == .burned && oldRow?.burnReason == .replacedByCoalescing,
                "coalescing burns the old row first")
    let newRow = world.store.row(replacement)
    try h.check(newRow?.state == .reserved
                && newRow?.channel == .s
                && newRow?.recipient == "viewer-1"
                && newRow?.chargedBytes == CloudOutboundSpool.chargedBytes(of: tinyRecord(22)),
                "the replacement inherits the row identity and recharges the new record")

    try await spool.seal(seq: replacement, sealedEnvelope: sealedEnvelopeStub)
    let log = SpoolTransportLog()
    _ = try await spool.sendNext { log.record($0) }
    try await h.expectSpoolError(.coalesceAfterSend(seq: replacement),
                                 "a sent snapshot must not coalesce") {
        _ = try await spool.coalesceSnapshot(replacing: replacement, with: tinyRecord(33))
    }

    _ = try await spool.settle(seq: replacement, .delivered)
    let ctl = try await reserveSealed(spool, channel: .ctl, logicalID: "cmd", ownerID: "v",
                                      recipient: "machine", record: tinyRecord(4))
    try await h.expectSpoolError(.coalesceControlChannelForbidden(seq: ctl, channel: .ctl),
                                 "ctl never coalesces") {
        _ = try await spool.coalesceSnapshot(replacing: ctl, with: tinyRecord(5))
    }
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: ctl, .delivered)
    let ctlr = try await reserveSealed(spool, channel: .ctlr, logicalID: "resp", ownerID: "v",
                                       recipient: "v", record: tinyRecord(6))
    try await h.expectSpoolError(.coalesceControlChannelForbidden(seq: ctlr, channel: .ctlr),
                                 "ctlr never coalesces") {
        _ = try await spool.coalesceSnapshot(replacing: ctlr, with: tinyRecord(7))
    }
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: ctlr, .delivered)
    let stream = try await reserveSealed(spool, channel: .t, logicalID: "term",
                                         recipient: "v", record: tinyRecord(8))
    try await h.expectSpoolError(.coalesceNotSnapshotChannel(seq: stream, channel: .t),
                                 "coalescing is defined only for the snapshot channel") {
        _ = try await spool.coalesceSnapshot(replacing: stream, with: tinyRecord(9))
    }
}

/// §6.2 restart: every sent row burns first (transport uncertain); a still-fresh never-sent
/// ready stream row sends as usual; ownerless ctl/ctlr ready rows burn; an owned ctlr ready
/// row survives and viewer_offline settles it as acked.
private func testRestartNormalization(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    var spool = try world.open(liveOwners: ["owner-live"])
    let sentSeq = try await reserveSealed(spool, channel: .s, logicalID: "s0",
                                          recipient: "rA", record: tinyRecord(1))
    let log = SpoolTransportLog()
    _ = try await spool.sendNext { log.record($0) }
    let staleReady = try await reserveSealed(spool, channel: .s, logicalID: "s1",
                                             recipient: "rA", record: tinyRecord(2))

    world.clock.advance(.seconds(360)) // beyond the 5-minute ready freshness window

    let freshReady = try await reserveSealed(spool, channel: .s, logicalID: "s2",
                                             recipient: "rA", record: tinyRecord(3))
    let ownerlessCtl = try await reserveSealed(spool, channel: .ctl, logicalID: "c1",
                                               ownerID: "owner-gone", recipient: "machine",
                                               record: tinyRecord(4))
    let ownedCtlr = try await reserveSealed(spool, channel: .ctlr, logicalID: "c2",
                                            ownerID: "owner-live", recipient: "viewer",
                                            record: tinyRecord(5))

    spool = try world.open(liveOwners: ["owner-live"])
    let sentRow = world.store.row(sentSeq)
    try h.check(sentRow?.state == .burned
                && sentRow?.burnReason == .restartSentUncertain
                && sentRow?.attemptOutcome == .transportUncertain,
                "restart burns every sent row — old continuous instants are incomparable")
    try h.check(world.store.row(staleReady)?.state == .burned
                && world.store.row(staleReady)?.burnReason == .staleReadyAtRestart,
                "restart burns a no-longer-fresh ready stream row")
    try h.check(world.store.row(freshReady)?.state == .ready,
                "restart keeps a still-fresh never-sent ready stream row")
    try h.check(world.store.row(ownerlessCtl)?.state == .burned
                && world.store.row(ownerlessCtl)?.burnReason == .ownerlessControlAtRestart,
                "restart burns an ownerless ctl ready row")
    try h.check(world.store.row(ownedCtlr)?.state == .ready,
                "restart keeps a ctlr ready row whose owner is still live")

    let sendFresh = try await spool.sendNext { log.record($0) }
    try h.check(sendFresh == .sent(seq: freshReady),
                "the fresh ready stream row sends as usual after restart")
    _ = try await spool.settle(seq: freshReady, .delivered)
    let sendCtlr = try await spool.sendNext { log.record($0) }
    try h.check(sendCtlr == .sent(seq: ownedCtlr), "the owned ctlr row sends after restart")
    let offline = try await spool.settle(seq: ownedCtlr, .viewerOffline)
    try h.check(offline == .settled(.acked)
                && world.store.row(ownedCtlr)?.state == .acked,
                "viewer_offline settles a ctlr response as terminal acked")
}

/// §6.6 read-time integrity: a charged_bytes recomputation mismatch, non-canonical stored
/// record bytes, or a counter that does not dominate the rows all fail closed at open.
private func testCorruptionFailsClosed(_ h: SpoolTestHarness) async throws {
    let emptyWorld = SpoolWorld()
    var zeroCounterOpened = false
    do {
        _ = try emptyWorld.open()
        zeroCounterOpened = true
    } catch {}
    try h.check(zeroCounterOpened && emptyWorld.store.snapshot.nextSeq == 0
                && emptyWorld.store.snapshot.rows.isEmpty,
                "an empty store accepts nextSeq exactly zero")

    let chargedWorld = SpoolWorld()
    _ = try await reserveSealed(try chargedWorld.open(), channel: .s, logicalID: "a",
                                recipient: "v", record: tinyRecord(1))
    chargedWorld.store.tamper { $0.rows[0].chargedBytes += 1 }
    try await h.expectSpoolError("stored charged_bytes that fails recomputation is corrupt",
                                 matching: {
        if case .corruptRow(let seq, _) = $0 { return seq == 0 }
        return false
    }) {
        _ = try chargedWorld.open()
    }
    try h.check(chargedWorld.metrics.stateStoreCorruptCount(.macSpool) == 1,
                "corrupt mac spool increments state_store_corrupt_total exactly once")

    let bytesWorld = SpoolWorld()
    _ = try await reserveSealed(try bytesWorld.open(), channel: .s, logicalID: "b",
                                recipient: "v", record: tinyRecord(2))
    bytesWorld.store.tamper { $0.rows[0].logicalRecordCanonicalBytes.append(0x20) }
    try await h.expectSpoolError("non-canonical stored record bytes are corrupt", matching: {
        if case .corruptRow(let seq, _) = $0 { return seq == 0 }
        return false
    }) {
        _ = try bytesWorld.open()
    }

    let counterWorld = SpoolWorld()
    _ = try await reserveSealed(try counterWorld.open(), channel: .s, logicalID: "c",
                                recipient: "v", record: tinyRecord(3))
    counterWorld.store.tamper { $0.nextSeq = 0 }
    try await h.expectSpoolError(.corruptCounter(nextSeq: 0, maxRowSeq: 0),
                                 "a counter that does not dominate the rows is corrupt") {
        _ = try counterWorld.open()
    }
}

/// §6.6 GC: deletes only terminal rows at least 10 minutes past their logical tombstone;
/// ready and sent rows are never dropped, and a tombstone cannot land on a live row.
private func testGCDeletesOnlyExpiredTombstonedTerminals(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let log = SpoolTransportLog()

    let ackedOld = try await reserveSealed(spool, channel: .s, logicalID: "a0",
                                           recipient: "v", record: tinyRecord(1))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: ackedOld, .delivered)
    let ackedRecent = try await reserveSealed(spool, channel: .s, logicalID: "a1",
                                              recipient: "v", record: tinyRecord(2))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: ackedRecent, .delivered)
    let rejectedNoTombstone = try await reserveSealed(spool, channel: .t, logicalID: "r0",
                                                      recipient: "v", record: tinyRecord(3))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: rejectedNoTombstone, .peerError)
    let stillSent = try await reserveSealed(spool, channel: .t, logicalID: "s0",
                                            recipient: "v", record: tinyRecord(4))
    _ = try await spool.sendNext { log.record($0) }
    let stillReady = try await reserveSealed(spool, channel: .s, logicalID: "rdy",
                                             recipient: "v", record: tinyRecord(5))

    try await spool.recordLogicalTombstone(seq: ackedOld)
    world.clock.advance(.seconds(120))
    try await spool.recordLogicalTombstone(seq: ackedRecent)
    world.clock.advance(.seconds(510)) // ackedOld: 10.5 min past tombstone; ackedRecent: 8.5

    try await spool.gcTick()
    try h.check(world.store.row(ackedOld) == nil,
                "GC deletes a terminal row 10+ minutes past its logical tombstone")
    try h.check(world.metrics.stateStoreGCCount(.macSpool, .terminal) == 1,
                "terminal-row deletion increments mac_spool state_store_gc_total")
    try h.check(world.store.row(ackedRecent)?.state == .acked,
                "GC keeps a terminal row whose tombstone is younger than 10 minutes")
    try h.check(world.store.row(rejectedNoTombstone)?.state == .rejected,
                "GC keeps a terminal row that has no logical tombstone yet")
    try h.check(world.store.row(stillSent)?.state == .sent,
                "GC never drops a sent row to make room")
    try h.check(world.store.row(stillReady)?.state == .ready,
                "GC never drops a ready row to make room")
    try await h.expectSpoolError(.tombstoneOnLiveRow(seq: stillReady, state: .ready),
                                 "a logical tombstone cannot land on a live row") {
        try await spool.recordLogicalTombstone(seq: stillReady)
    }
}

/// §6.6: GC runs before every admission and a GC failure fails the admission closed with a
/// typed gc_failed refusal — and clears on the next healthy pass.
private func testGCFailureFailsAdmissionClosed(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    let log = SpoolTransportLog()
    let doomed = try await reserveSealed(spool, channel: .s, logicalID: "gone",
                                         recipient: "v", record: tinyRecord(1))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: doomed, .delivered)
    try await spool.recordLogicalTombstone(seq: doomed)
    world.clock.advance(.seconds(660))

    world.store.failNextCommits(1)
    try await h.expectSpoolError(.admissionRefused(reason: .gcFailed, scope: .global),
                                 "a failing pre-admission GC refuses the admission") {
        _ = try await spool.reserve(channel: .s, logicalID: "new", ownerID: nil,
                                    recipient: "v", record: tinyRecord(2))
    }
    try h.check(world.metrics.refusalCount(.gcFailed, .global) == 1,
                "the gc_failed refusal is counted")
    try h.check(world.store.row(doomed) != nil,
                "the failed GC deleted nothing — the store is unchanged")

    let seq = try await spool.reserve(channel: .s, logicalID: "new", ownerID: nil,
                                      recipient: "v", record: tinyRecord(2))
    try h.check(seq > doomed, "admission succeeds once GC can commit again")
    try h.check(world.store.row(doomed) == nil, "the healthy GC pass deleted the expired row")
}

/// §6.6 global row cap: exactly 2,000 rows succeeds, 2,001 refuses, and nothing live is
/// evicted to make room. The fill also crosses the 80%/90% occupancy alerts exactly once each.
private func testGlobalRowCapBoundaryAndNoEviction(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    var lastSeq: Int64 = -1
    for index in 0..<2000 {
        lastSeq = try await spool.reserve(channel: .t, logicalID: "m\(index)", ownerID: nil,
                                          recipient: "r\(index % 100)",
                                          record: tinyRecord(Int64(index)))
    }
    try h.check(lastSeq == 1999, "row occupancy exactly equal to the 2,000 cap succeeds")
    try h.check(world.metrics.rowsGauge(.mac) == 2000, "cloud_spool_rows gauge tracks occupancy")
    try h.check(world.metrics.alertCount(.rows, .warning80) == 1,
                "the 80% row occupancy warning fired exactly once")
    try h.check(world.metrics.alertCount(.rows, .page90) == 1,
                "the 90% row occupancy page fired exactly once")

    try await h.expectSpoolError(.admissionRefused(reason: .rowCap, scope: .global),
                                 "row 2,001 refuses with a typed global row_cap error") {
        _ = try await spool.reserve(channel: .t, logicalID: "overflow", ownerID: nil,
                                    recipient: "fresh-recipient", record: tinyRecord(0))
    }
    try h.check(world.metrics.refusalCount(.rowCap, .global) == 1,
                "the global row_cap refusal is counted")
    let snapshot = world.store.snapshot
    try h.check(snapshot.rows.count == 2000
                && snapshot.rows.allSatisfy { $0.state == .reserved }
                && snapshot.rows.first?.seq == 0,
                "the full spool refused the newcomer instead of evicting a live row")
}

/// §6.6 global byte cap: exactly 16 MiB succeeds (via eight recipients each at exactly their
/// 2 MiB recipient cap), one more byte refuses, and nothing is evicted.
private func testGlobalByteCapBoundary(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    for index in 0..<8 {
        _ = try await spool.reserve(channel: .t, logicalID: "big\(index)", ownerID: nil,
                                    recipient: "b\(index)",
                                    record: payloadRecord(ofChargedBytes: 2_097_152))
    }
    try h.check(world.metrics.bytesGauge(.mac) == 16_777_216,
                "byte occupancy exactly equal to the 16 MiB cap succeeds — and each recipient "
                + "sits exactly at its 2 MiB cap")
    try h.check(world.metrics.alertCount(.bytes, .warning80) == 1,
                "the 80% byte occupancy warning fired exactly once")
    try h.check(world.metrics.alertCount(.bytes, .page90) == 1,
                "the 90% byte occupancy page fired exactly once")

    try await h.expectSpoolError(.admissionRefused(reason: .byteCap, scope: .global),
                                 "one byte past 16 MiB refuses with a typed global byte_cap error") {
        _ = try await spool.reserve(channel: .t, logicalID: "tiny", ownerID: nil,
                                    recipient: "b8", record: tinyRecord(7))
    }
    try h.check(world.metrics.bytesGauge(.mac) == 16_777_216
                && world.store.snapshot.rows.count == 8,
                "the byte-full spool refused the newcomer instead of evicting a live row")
}

/// §6.6 per-recipient (actor) hard caps, both sides: 200 rows / 2 MiB exactly succeed, one
/// more of either refuses — while other recipients keep being admitted.
private func testRecipientCapBoundaries(_ h: SpoolTestHarness) async throws {
    let rowsWorld = SpoolWorld()
    let rowsSpool = try rowsWorld.open()
    for index in 0..<200 {
        _ = try await rowsSpool.reserve(channel: .t, logicalID: "x\(index)", ownerID: nil,
                                        recipient: "X", record: tinyRecord(Int64(index)))
    }
    try h.check(rowsWorld.metrics.rowsGauge(.mac) == 200,
                "a recipient at exactly its 200-row cap succeeds")
    try await h.expectSpoolError(.admissionRefused(reason: .rowCap, scope: .recipient),
                                 "row 201 for one recipient refuses at recipient scope") {
        _ = try await rowsSpool.reserve(channel: .t, logicalID: "x200", ownerID: nil,
                                        recipient: "X", record: tinyRecord(200))
    }
    let other = try await rowsSpool.reserve(channel: .t, logicalID: "z", ownerID: nil,
                                            recipient: "Z", record: tinyRecord(0))
    try h.check(other == 200, "the recipient cap does not block other recipients")

    let bytesWorld = SpoolWorld()
    let bytesSpool = try bytesWorld.open()
    _ = try await bytesSpool.reserve(channel: .t, logicalID: "y0", ownerID: nil,
                                     recipient: "Y",
                                     record: payloadRecord(ofChargedBytes: 2_097_152))
    try h.check(bytesWorld.metrics.bytesGauge(.mac) == 2_097_152,
                "a recipient at exactly its 2 MiB cap succeeds")
    try await h.expectSpoolError(.admissionRefused(reason: .byteCap, scope: .recipient),
                                 "one byte past 2 MiB for one recipient refuses at recipient scope") {
        _ = try await bytesSpool.reserve(channel: .t, logicalID: "y1", ownerID: nil,
                                        recipient: "Y", record: tinyRecord(1))
    }
}

/// §6.6 fairness reserve: activation is pre-commit occupancy at exactly ceil(0.9*2000)=1800
/// (1799 does not activate), and while active a candidate whose recipient would exceed 20
/// rows or 256 KiB refuses — small recipients keep being admitted.
private func testFairnessActivationBoundary(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    for index in 0..<30 {
        _ = try await spool.reserve(channel: .t, logicalID: "r\(index)", ownerID: nil,
                                    recipient: "R", record: tinyRecord(Int64(index)))
    }
    _ = try await spool.reserve(channel: .s, logicalID: "s0", ownerID: nil, recipient: "S",
                                record: payloadRecord(ofChargedBytes: 256_000)) // 250 KiB
    for index in 0..<1768 {
        _ = try await spool.reserve(channel: .t, logicalID: "f\(index)", ownerID: nil,
                                    recipient: "F\(index % 9)",
                                    record: tinyRecord(Int64(index)))
    }
    try h.check(world.metrics.rowsGauge(.mac) == 1799, "occupancy staged at exactly 1,799")

    // Pre-commit occupancy 1,799: fairness is NOT active, so R may take its 31st row even
    // though 31 > 20.
    _ = try await spool.reserve(channel: .t, logicalID: "r30", ownerID: nil,
                                recipient: "R", record: tinyRecord(30))
    try h.check(world.metrics.rowsGauge(.mac) == 1800,
                "at occupancy 1,799 fairness is inactive — the 21+-row recipient is admitted")

    // Pre-commit occupancy 1,800 = ceil(0.9*2000): fairness activates and the same shape
    // now refuses.
    try await h.expectSpoolError(.admissionRefused(reason: .fairness, scope: .recipient),
                                 "at occupancy 1,800 fairness refuses a recipient beyond 20 rows") {
        _ = try await spool.reserve(channel: .t, logicalID: "r31", ownerID: nil,
                                    recipient: "R", record: tinyRecord(31))
    }
    try await h.expectSpoolError(.admissionRefused(reason: .fairness, scope: .recipient),
                                 "fairness refuses a recipient that would exceed 256 KiB") {
        _ = try await spool.reserve(channel: .s, logicalID: "s1", ownerID: nil, recipient: "S",
                                    record: payloadRecord(ofChargedBytes: 10_240))
    }
    _ = try await spool.reserve(channel: .s, logicalID: "s2", ownerID: nil, recipient: "S",
                                record: payloadRecord(ofChargedBytes: 1_024))
    try h.check(world.metrics.rowsGauge(.mac) == 1801,
                "fairness admits the same recipient when it stays within 256 KiB")
    _ = try await spool.reserve(channel: .t, logicalID: "fresh", ownerID: nil,
                                recipient: "NEW", record: tinyRecord(0))
    try h.check(world.metrics.rowsGauge(.mac) == 1802,
                "fairness admits small recipients while the spool is nearly full")
    try h.check(world.metrics.refusalCount(.fairness, .recipient) == 2,
                "both fairness refusals are counted at recipient scope")
}

/// §6.6 fairness uses ceil(0.9*cap), not floor. A non-multiple-of-ten byte cap makes the
/// one-byte distinction observable: occupancy 900 is below ceil(0.9*1001)=901, while 901
/// is exactly the activation point.
private func testFairnessByteActivationCeilingBoundary(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.globalByteCap = 1001
    limits.recipientRowCap = 200
    limits.fairnessRecipientRowCap = 1
    limits.fairnessRecipientByteCap = 1001

    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    _ = try await spool.reserve(channel: .t, logicalID: "floor", ownerID: nil,
                                recipient: "R", record: payloadRecord(ofChargedBytes: 900))
    var floorBoundaryAdmitted = false
    do {
        _ = try await spool.reserve(channel: .t, logicalID: "ceil", ownerID: nil,
                                    recipient: "R", record: tinyRecord(0))
        floorBoundaryAdmitted = true
    } catch {}
    try h.check(floorBoundaryAdmitted && world.metrics.bytesGauge(.mac) == 901,
                "at byte occupancy 900 (the floor threshold) fairness is still inactive")
    try await h.expectSpoolError(.admissionRefused(reason: .fairness, scope: .recipient),
                                 "at byte occupancy 901 (the ceil threshold) fairness activates") {
        _ = try await spool.reserve(channel: .t, logicalID: "active", ownerID: nil,
                                    recipient: "R", record: tinyRecord(1))
    }
}

/// The two fairness post-commit caps use inclusive boundaries: a recipient may land exactly
/// on its row or byte reserve, while the next unit is refused.
private func testFairnessRecipientCapExactBoundaries(_ h: SpoolTestHarness) async throws {
    var rowLimits = CloudSpoolLimits()
    rowLimits.globalByteCap = 1001
    rowLimits.fairnessRecipientRowCap = 1
    rowLimits.fairnessRecipientByteCap = 1001
    let rowWorld = SpoolWorld()
    let rowSpool = try rowWorld.open(limits: rowLimits)
    _ = try await rowSpool.reserve(channel: .t, logicalID: "stage", ownerID: nil,
                                   recipient: "stage", record: payloadRecord(ofChargedBytes: 901))
    var exactRowAdmitted = false
    do {
        _ = try await rowSpool.reserve(channel: .t, logicalID: "row-exact", ownerID: nil,
                                       recipient: "R", record: tinyRecord(0))
        exactRowAdmitted = true
    } catch {}
    try h.check(exactRowAdmitted
                && rowWorld.store.snapshot.rows.filter { $0.recipient == "R" }.count == 1,
                "active fairness admits a recipient exactly at its row cap")
    try await h.expectSpoolError(.admissionRefused(reason: .fairness, scope: .recipient),
                                 "active fairness refuses one row past its recipient cap") {
        _ = try await rowSpool.reserve(channel: .t, logicalID: "row-over", ownerID: nil,
                                       recipient: "R", record: tinyRecord(1))
    }

    var byteLimits = CloudSpoolLimits()
    byteLimits.globalByteCap = 2001
    byteLimits.fairnessRecipientRowCap = 20
    byteLimits.fairnessRecipientByteCap = 100
    let byteWorld = SpoolWorld()
    let byteSpool = try byteWorld.open(limits: byteLimits)
    _ = try await byteSpool.reserve(channel: .t, logicalID: "stage", ownerID: nil,
                                    recipient: "stage",
                                    record: payloadRecord(ofChargedBytes: 1801))
    var exactBytesAdmitted = false
    do {
        _ = try await byteSpool.reserve(channel: .t, logicalID: "byte-exact", ownerID: nil,
                                        recipient: "B",
                                        record: payloadRecord(ofChargedBytes: 100))
        exactBytesAdmitted = true
    } catch {}
    try h.check(exactBytesAdmitted && byteWorld.store.snapshot.rows.last?.chargedBytes == 100,
                "active fairness admits a recipient exactly at its byte cap")
    try await h.expectSpoolError(.admissionRefused(reason: .fairness, scope: .recipient),
                                 "active fairness refuses one byte past its recipient cap") {
        _ = try await byteSpool.reserve(channel: .t, logicalID: "byte-over", ownerID: nil,
                                        recipient: "B", record: tinyRecord(0))
    }
}

/// §6.2 restart freshness is strict: exactly five minutes old is still fresh; any amount
/// beyond the boundary burns the ready stream row.
private func testReadyFreshnessExactBoundary(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    var spool = try world.open()
    let over = try await reserveSealed(spool, channel: .s, logicalID: "over",
                                       recipient: "R", record: tinyRecord(0))
    world.clock.advance(.milliseconds(1))
    let exact = try await reserveSealed(spool, channel: .s, logicalID: "exact",
                                        recipient: "R", record: tinyRecord(1))
    world.clock.advance(.seconds(300))
    spool = try world.open()
    _ = spool
    try h.check(world.store.row(exact)?.state == .ready,
                "a ready stream exactly 300 seconds old remains fresh at restart")
    try h.check(world.store.row(over)?.state == .burned
                && world.store.row(over)?.burnReason == .staleReadyAtRestart,
                "a ready stream 1 millisecond past 300 seconds burns at restart")
}

/// Current-run reservation staleness is also strict: equality is retained, and only time
/// beyond the configured limit burns the unsealed reservation.
private func testStaleReservationExactBoundary(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.gcInterval = .seconds(10_000)
    limits.staleReservedAfter = .seconds(60)
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let seq = try await spool.reserve(channel: .s, logicalID: "reserved", ownerID: nil,
                                      recipient: "R", record: tinyRecord(0))
    world.clock.advance(.seconds(60))
    try await spool.gcTick()
    try h.check(world.store.row(seq)?.state == .reserved,
                "an unsealed reservation exactly 60 seconds old is not stale")
    world.clock.advance(.milliseconds(1))
    try await spool.gcTick()
    try h.check(world.store.row(seq)?.state == .burned
                && world.store.row(seq)?.burnReason == .staleReservation,
                "an unsealed reservation 1 millisecond past 60 seconds burns as stale")
    try h.check(world.metrics.stateStoreGCCount(.macSpool, .expired) == 1,
                "stale reservation GC increments mac_spool expired exactly once")
}

/// Tombstone retention is inclusive. Two tombstones one millisecond apart hold the observed
/// clock still across the same GC pass: exactly ten minutes deletes, just under does not.
private func testTombstoneRetentionExactBoundary(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.gcInterval = .seconds(10_000)
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let log = SpoolTransportLog()

    let exact = try await reserveSealed(spool, channel: .s, logicalID: "exact",
                                        recipient: "R", record: tinyRecord(0))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: exact, .delivered)
    try await spool.recordLogicalTombstone(seq: exact)
    world.clock.advance(.milliseconds(1))
    let under = try await reserveSealed(spool, channel: .s, logicalID: "under",
                                        recipient: "R", record: tinyRecord(1))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: under, .delivered)
    try await spool.recordLogicalTombstone(seq: under)
    world.clock.advance(.milliseconds(599_999))

    try await spool.gcTick()
    try h.check(world.store.row(exact) == nil,
                "GC deletes a terminal row exactly 10 minutes after its tombstone")
    try h.check(world.store.row(under)?.state == .acked,
                "GC keeps a terminal row 1 millisecond short of tombstone retention")
}

/// Periodic GC itself is due inclusively at its interval, independently of an explicit tick.
private func testGCIntervalExactBoundary(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.gcInterval = .seconds(60)
    limits.tombstoneRetention = .seconds(60)
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let log = SpoolTransportLog()
    let seq = try await reserveSealed(spool, channel: .s, logicalID: "due",
                                      recipient: "R", record: tinyRecord(0))
    _ = try await spool.sendNext { log.record($0) }
    _ = try await spool.settle(seq: seq, .delivered)
    try await spool.recordLogicalTombstone(seq: seq)

    world.clock.advance(.milliseconds(59_999))
    _ = try await spool.sendNext { _ in }
    try h.check(world.store.row(seq) != nil,
                "automatic GC does not run 1 millisecond before its interval")
    world.clock.advance(.milliseconds(1))
    _ = try await spool.sendNext { _ in }
    try h.check(world.store.row(seq) == nil,
                "automatic GC runs exactly at its 60-second interval")
}

/// A GC batch deletes at most its configured count: equality is allowed, and the next
/// eligible row waits for the next batch.
private func testGCBatchLimitExactBoundary(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.gcInterval = .seconds(10_000)
    limits.gcBatchLimit = 256
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    let log = SpoolTransportLog()
    for index in 0..<257 {
        let seq = try await reserveSealed(spool, channel: .s, logicalID: "g\(index)",
                                          recipient: "R\(index)",
                                          record: tinyRecord(Int64(index)))
        _ = try await spool.sendNext { log.record($0) }
        _ = try await spool.settle(seq: seq, .delivered)
        try await spool.recordLogicalTombstone(seq: seq)
    }
    world.clock.advance(.seconds(600))
    try await spool.gcTick()
    try h.check(world.store.snapshot.rows.count == 1,
                "one GC batch deletes exactly its 256-row limit, not the 257th row")
    try await spool.gcTick()
    try h.check(world.store.snapshot.rows.isEmpty,
                "the row just beyond the batch boundary deletes on the next GC tick")
}

/// A GC pass with zero burns and zero deletions is a true no-op and must not manufacture a
/// persistence commit merely because the deletion count is exactly zero.
private func testGCZeroChangeBoundary(_ h: SpoolTestHarness) async throws {
    let world = SpoolWorld()
    let spool = try world.open()
    world.store.failNextCommits(1)
    var noOpSucceeded = false
    do {
        try await spool.gcTick()
        noOpSucceeded = true
    } catch {}
    try h.check(noOpSucceeded,
                "GC with exactly zero changes performs no persistence commit")
}

/// Occupancy alerts use inclusive integer thresholds: 79% is quiet, exactly 80% warns,
/// and exactly 90% pages without emitting another warning.
private func testByteOccupancyAlertExactBoundaries(_ h: SpoolTestHarness) async throws {
    var limits = CloudSpoolLimits()
    limits.globalByteCap = 1000
    let world = SpoolWorld()
    let spool = try world.open(limits: limits)
    _ = try await spool.reserve(channel: .t, logicalID: "below", ownerID: nil,
                                recipient: "R", record: payloadRecord(ofChargedBytes: 790))
    try h.check(world.metrics.alertCount(.bytes, .warning80) == 0
                && world.metrics.alertCount(.bytes, .page90) == 0,
                "byte occupancy at 79% raises no alert")
    _ = try await spool.reserve(channel: .t, logicalID: "warning", ownerID: nil,
                                recipient: "R", record: payloadRecord(ofChargedBytes: 10))
    try h.check(world.metrics.alertCount(.bytes, .warning80) == 1
                && world.metrics.alertCount(.bytes, .page90) == 0,
                "byte occupancy exactly at 80% raises warning, not page")
    _ = try await spool.reserve(channel: .t, logicalID: "page", ownerID: nil,
                                recipient: "R", record: payloadRecord(ofChargedBytes: 100))
    try h.check(world.metrics.alertCount(.bytes, .warning80) == 1
                && world.metrics.alertCount(.bytes, .page90) == 1,
                "byte occupancy exactly at 90% raises page, not another warning")
}

// MARK: - Runner

public func runCloudOutboundSpoolTests() async throws -> Int {
    let h = SpoolTestHarness()
    try await testTerminalRowsOccupyHardCap(h)
    try await testTerminalBytesOccupyHardCap(h)
    try testMetricsDomainsAndLimits(h)
    try await testHeadSelectorUsesLowestSeq(h)
    try await testUnsortedPersistedStateFailsClosed(h)
    try await testCorruptionFailsClosed(h)
    try await testSeqNeverReusedAndSharedCounter(h)
    try await testCrashBeforeSealRecovery(h)
    try await testHeadOfLineAcrossChannels(h)
    try await testAttemptCapBurnReleasesHeadOfLine(h)
    try await testLateAckAfterBurn(h)
    try await testSendThrowLeavesRowSent(h)
    try await testReadyRowWithFirstSentFailsClosed(h)
    try await testOutOfDomainIntegersRefusedWithoutPersistence(h)
    try await testCoalescing(h)
    try await testRestartNormalization(h)
    try await testGCDeletesOnlyExpiredTombstonedTerminals(h)
    try await testGCFailureFailsAdmissionClosed(h)
    try await testFairnessByteActivationCeilingBoundary(h)
    try await testFairnessRecipientCapExactBoundaries(h)
    try await testGlobalRowCapBoundaryAndNoEviction(h)
    try await testGlobalByteCapBoundary(h)
    try await testRecipientCapBoundaries(h)
    try await testFairnessActivationBoundary(h)
    try await testReadyFreshnessExactBoundary(h)
    try await testStaleReservationExactBoundary(h)
    try await testTombstoneRetentionExactBoundary(h)
    try await testGCIntervalExactBoundary(h)
    try await testGCBatchLimitExactBoundary(h)
    try await testGCZeroChangeBoundary(h)
    try await testByteOccupancyAlertExactBoundaries(h)
    return h.checks
}
