import Foundation

private struct CloudCommandLedgerTestFailure: Error, CustomStringConvertible {
    let group: String
    let check: String

    var description: String { "FAIL [\(group)] \(check)" }
}

private final class CloudCommandLedgerTestChecks {
    private let selectedGroup = ProcessInfo.processInfo.environment["CLOUD_LEDGER_TEST_FILTER"]
    private(set) var count = 0

    func includes(_ group: String) -> Bool {
        selectedGroup == nil || selectedGroup == group
    }

    func expect(_ condition: @autoclosure () -> Bool, _ check: String, group: String) throws {
        count += 1
        guard condition() else {
            throw CloudCommandLedgerTestFailure(group: group, check: check)
        }
    }

    func expectError(
        _ expected: CloudCommandLedgerError,
        _ check: String,
        group: String,
        operation: () async throws -> Void
    ) async throws {
        count += 1
        do {
            try await operation()
        } catch let error as CloudCommandLedgerError {
            guard error == expected else {
                throw CloudCommandLedgerTestFailure(
                    group: group,
                    check: "\(check)（收到 \(error)，預期 \(expected)）"
                )
            }
            return
        } catch {
            throw CloudCommandLedgerTestFailure(
                group: group,
                check: "\(check)（收到非 typed error：\(error)）"
            )
        }
        throw CloudCommandLedgerTestFailure(group: group, check: "\(check)（沒有拋錯）")
    }
}

private final class CloudLedgerTestClock: @unchecked Sendable {
    var wall: Date
    var continuous: UInt64
    var boot: String

    init(wall: Date, continuous: UInt64 = 0, boot: String = "boot-A") {
        self.wall = wall
        self.continuous = continuous
        self.boot = boot
    }

    var clocks: CloudCommandLedgerClocks {
        CloudCommandLedgerClocks(
            wallNow: { [self] in wall },
            continuousNow: { [self] in continuous },
            bootID: { [self] in boot }
        )
    }

    func advance(wall seconds: TimeInterval = 0, continuousMilliseconds: UInt64 = 0) {
        wall = wall.addingTimeInterval(seconds)
        continuous += continuousMilliseconds * 1_000_000
    }
}

private final class CountingCloudCommandLedgerStore: CloudCommandLedgerStore {
    private let underlying: InMemoryCloudCommandLedgerStore
    private(set) var transactionCount = 0

    init(rows: [CloudCommandLedgerRow]) {
        underlying = InMemoryCloudCommandLedgerStore(rows: rows)
    }

    func resetCount() {
        transactionCount = 0
    }

    func transaction<T>(
        _ body: (inout [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws -> T
    ) rethrows -> T {
        transactionCount += 1
        return try underlying.transaction(body)
    }

    func persistedBytes() throws -> Data {
        try underlying.persistedBytes()
    }
}

private final class SignallingCloudCommandLedgerStore: CloudCommandLedgerStore, @unchecked Sendable {
    private struct TransactionWaiter {
        let target: Int
        let continuation: CheckedContinuation<Void, Never>
    }

    private let underlying = InMemoryCloudCommandLedgerStore()
    private let lock = NSLock()
    private var transactionCount = 0
    private var transactionWaiters: [TransactionWaiter] = []

    func transaction<T>(
        _ body: (inout [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws -> T
    ) rethrows -> T {
        lock.lock()
        transactionCount += 1
        let currentCount = transactionCount
        let ready = transactionWaiters.filter { $0.target <= currentCount }
        transactionWaiters.removeAll { $0.target <= currentCount }
        lock.unlock()
        ready.forEach { $0.continuation.resume() }
        return try underlying.transaction(body)
    }

    func persistedBytes() throws -> Data {
        try underlying.persistedBytes()
    }

    func waitUntilTransactionCount(_ target: Int) async {
        await withCheckedContinuation { continuation in
            lock.lock()
            if transactionCount >= target {
                lock.unlock()
                continuation.resume()
            } else {
                transactionWaiters.append(TransactionWaiter(target: target, continuation: continuation))
                lock.unlock()
            }
        }
    }
}

private func ledgerRequest(
    sender: String = "viewer-A",
    id: String = "request-A",
    digest: String = "digest-A",
    rawReplyKey: Data = Data("raw-key-A".utf8),
    deadline: Date,
    effectDeadline: UInt64 = UInt64.max
) -> CloudCommandLedgerRequest {
    CloudCommandLedgerRequest(
        viewerSender: sender,
        requestID: id,
        requestSHA256: Data(digest.utf8),
        rawReplyKey: rawReplyKey,
        replyKeyID: "reply-key-id-A",
        recipientDeviceID: "device-A",
        deadlineAt: deadline,
        effectNotAfterContinuous: effectDeadline
    )
}

private func completedRow(
    sender: String,
    id: String,
    createdAt: Date,
    retainedMilliseconds: UInt64 = 0,
    digest: String? = nil
) -> CloudCommandLedgerRow {
    let key = CloudCommandLedgerKey(viewerSender: sender, requestID: id)
    return CloudCommandLedgerRow(
        key: key,
        requestSHA256: Data((digest ?? "digest-\(sender)-\(id)").utf8),
        replyKeyID: "reply-id",
        recipientDeviceID: "device",
        deadlineAt: createdAt.addingTimeInterval(3_600),
        state: .completed,
        normalizedOutcome: CloudCommandNormalizedOutcome(code: .succeeded),
        createdAt: createdAt,
        effectNotAfterContinuous: nil,
        effectStartedAt: createdAt,
        completedAt: createdAt,
        expiresAt: createdAt.addingTimeInterval(CloudCommandLedger.retentionSeconds),
        retainedElapsedMilliseconds: retainedMilliseconds,
        elapsedBootID: "boot-A",
        elapsedCheckpointContinuous: 0
    )
}

private func admissionRows(
    total: Int,
    targetSender: String,
    targetCount: Int,
    createdAt: Date
) -> [CloudCommandLedgerRow] {
    precondition(targetCount <= total)
    var rows: [CloudCommandLedgerRow] = []
    rows.reserveCapacity(total)
    for index in 0..<targetCount {
        rows.append(completedRow(sender: targetSender, id: "target-\(index)", createdAt: createdAt))
    }
    var remaining = total - targetCount
    var actor = 0
    while remaining > 0 {
        let count = min(999, remaining)
        for index in 0..<count {
            rows.append(completedRow(sender: "seed-\(actor)", id: "row-\(index)", createdAt: createdAt))
        }
        remaining -= count
        actor += 1
    }
    return rows
}

private func exerciseCompletedRow(
    ledger: CloudCommandLedger,
    request: CloudCommandLedgerRequest,
    outcome: CloudCommandNormalizedOutcome = CloudCommandNormalizedOutcome(code: .succeeded)
) async throws {
    _ = try await ledger.reserve(request)
    try await ledger.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
    try await ledger.complete(request, outcome: outcome)
}

private func exhaustiveBucketValue(_ value: CloudLedgerActorRowsBucket) -> Int {
    switch value {
    case .le10: return 10
    case .le100: return 100
    case .le500: return 500
    case .le1000: return 1_000
    }
}

private func exhaustiveScopeValue(_ value: CloudLedgerCapacityScope) -> String {
    switch value {
    case .global: return "global"
    case .actor: return "actor"
    }
}

private func exhaustiveCapacityReasonValue(_ value: CloudLedgerCapacityReason) -> String {
    switch value {
    case .rowCap: return "row_cap"
    case .fairness: return "fairness"
    case .corrupt: return "corrupt"
    }
}

private func exhaustiveGCReasonValue(_ value: CloudLedgerGCReason) -> String {
    switch value {
    case .expired: return "expired"
    case .revoked: return "revoked"
    }
}

func runCloudCommandLedgerTests() async throws -> Int {
    let checks = CloudCommandLedgerTestChecks()
    let base = Date(timeIntervalSince1970: 2_000_000_000)

    if checks.includes("crash_before_reserved") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCrashBeforeReserved() async throws {
            let group = "crash_before_reserved"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            var first: CloudCommandLedger? = CloudCommandLedger(store: store, clocks: clock.clocks)
            first = nil
            try checks.expect(first == nil, "第一個實例確實已丟棄", group: group)
            let reopened = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(deadline: base.addingTimeInterval(60))
            let result = try await reopened.reserve(request)
            try checks.expect(result == .reserved(request.key), "reserved commit 前 crash 可 retry", group: group)
        }
        try await runCrashBeforeReserved()
    }

    if checks.includes("crash_after_reserved") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCrashAfterReserved() async throws {
            let group = "crash_after_reserved"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let request = ledgerRequest(id: "reserved-crash", deadline: base.addingTimeInterval(60))
            var first: CloudCommandLedger? = CloudCommandLedger(store: store, clocks: clock.clocks)
            _ = try await first!.reserve(request)
            first = nil
            let reopened = CloudCommandLedger(store: store, clocks: clock.clocks)
            let result = try await reopened.reserve(request)
            try checks.expect(result == .reserved(request.key), "重開實例原子清掉 reserved 並允許 retry", group: group)
            let recoveredCount = await reopened.rowCount()
            try checks.expect(recoveredCount == 1, "reserved recovery 不留下重複 row", group: group)
        }
        try await runCrashAfterReserved()
    }

    if checks.includes("crash_in_progress") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCrashInProgress() async throws {
            let group = "crash_in_progress"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let request = ledgerRequest(id: "effect-return-crash", deadline: base.addingTimeInterval(60))
            var first: CloudCommandLedger? = CloudCommandLedger(store: store, clocks: clock.clocks)
            _ = try await first!.reserve(request)
            try await first!.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
            var effectCalls = 0
            effectCalls += 1
            first = nil
            let reopened = CloudCommandLedger(store: store, clocks: clock.clocks)
            try await checks.expectError(.outcomeUnknown, "effect 後、completed 前 crash 永遠 unknown", group: group) {
                _ = try await reopened.reserve(request)
            }
            try checks.expect(effectCalls == 1, "reopen 沒有自動重做 effect", group: group)
        }
        try await runCrashInProgress()
    }

    if checks.includes("crash_false_unknown") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCrashFalseUnknown() async throws {
            let group = "crash_false_unknown"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let request = ledgerRequest(id: "false-unknown", deadline: base.addingTimeInterval(60))
            var first: CloudCommandLedger? = CloudCommandLedger(store: store, clocks: clock.clocks)
            _ = try await first!.reserve(request)
            try await first!.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
            let effectCalls = 0
            first = nil
            let reopened = CloudCommandLedger(store: store, clocks: clock.clocks)
            try await checks.expectError(.outcomeUnknown, "in_progress commit 後、effect call 前也保守 unknown", group: group) {
                _ = try await reopened.reserve(request)
            }
            try checks.expect(effectCalls == 0, "測試確實沒有呼叫 effect", group: group)
        }
        try await runCrashFalseUnknown()
    }

    if checks.includes("crash_completed") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCrashCompleted() async throws {
            let group = "crash_completed"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let request = ledgerRequest(id: "completed-crash", deadline: base.addingTimeInterval(60))
            let outcome = CloudCommandNormalizedOutcome(code: .failed, payload: Data("normalized".utf8))
            var first: CloudCommandLedger? = CloudCommandLedger(store: store, clocks: clock.clocks)
            try await exerciseCompletedRow(ledger: first!, request: request, outcome: outcome)
            first = nil
            let reopened = CloudCommandLedger(store: store, clocks: clock.clocks)
            let duplicate = try await reopened.reserve(request)
            try checks.expect(duplicate == .cached(outcome), "completed 後、publish 前 crash 回 cached outcome", group: group)
        }
        try await runCrashCompleted()
    }

    if checks.includes("coalesced_duplicate") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCoalescedDuplicate() async throws {
            let group = "coalesced_duplicate"
            let clock = CloudLedgerTestClock(wall: base)
            let store = SignallingCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "coalesce", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let duplicate = Task { try await ledger.reserve(request) }
            // Wait for the duplicate to reach the store, not for one turn of the executor:
            // `clock.advance(wall: 61)` below crosses this request's deadline, and a duplicate
            // that has not yet read `wallNow()` reads the advanced value and throws.
            await store.waitUntilTransactionCount(3)
            try await ledger.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
            let outcome = CloudCommandNormalizedOutcome(code: .succeeded, payload: Data("one-effect".utf8))
            clock.advance(wall: 61)
            try await ledger.complete(request, outcome: outcome)
            let duplicateResult = try await duplicate.value
            try checks.expect(duplicateResult == .cached(outcome), "active reserved duplicate 即使等待跨過 deadline 仍 coalesce 到 owner outcome", group: group)
        }
        try await runCoalescedDuplicate()
    }

    if checks.includes("waiter_recovery_deadline") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runWaiterRecoveryDeadline() async throws {
            let group = "waiter_recovery_deadline"
            let clock = CloudLedgerTestClock(wall: base)
            let store = SignallingCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "waiter-deadline", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let duplicate = Task { try await ledger.reserve(request) }
            await store.waitUntilTransactionCount(3)
            let waitersBeforeRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersBeforeRefusal == 1, "deadline 拒絕前 duplicate 已確定 coalesce", group: group)
            clock.advance(wall: 60)
            try await checks.expectError(.deadlineExpired, "owner 收到 deadlineExpired", group: group) {
                try await ledger.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
            }
            let waitersAfterRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersAfterRefusal == 0, "deadline 拒絕不留下 waiter", group: group)
            try await checks.expectError(.deadlineExpired, "deadline waiter 有限步內重新 admission 並收到 deadlineExpired", group: group) {
                _ = try await duplicate.value
            }
            let rowAfterRecovery = await ledger.row(for: request.key)
            try checks.expect(rowAfterRecovery == nil, "deadline 已到時 recovery 不重建 row", group: group)
        }
        try await runWaiterRecoveryDeadline()
    }

    if checks.includes("waiter_recovery_epoch") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runWaiterRecoveryEpoch() async throws {
            let group = "waiter_recovery_epoch"
            let clock = CloudLedgerTestClock(wall: base)
            let store = SignallingCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "waiter-epoch", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let duplicate = Task { try await ledger.reserve(request) }
            await store.waitUntilTransactionCount(3)
            let waitersBeforeRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersBeforeRefusal == 1, "epoch 拒絕前 duplicate 已確定 coalesce", group: group)
            try await checks.expectError(.epochUncertain, "owner 收到 epochUncertain", group: group) {
                try await ledger.beginEffect(request, epochState: .uncertain, latestRosterAndGateAllow: true)
            }
            let waitersAfterRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersAfterRefusal == 0, "epoch 拒絕不留下 waiter", group: group)
            let retry = try await duplicate.value
            try checks.expect(retry == .reserved(request.key), "epoch waiter 重新 admission 成為新 owner", group: group)
            let recoveredRow = await ledger.row(for: request.key)
            try checks.expect(recoveredRow?.state == .reserved, "epoch recovery 留下新的 reserved row", group: group)
        }
        try await runWaiterRecoveryEpoch()
    }

    if checks.includes("waiter_recovery_gate") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runWaiterRecoveryGate() async throws {
            let group = "waiter_recovery_gate"
            let clock = CloudLedgerTestClock(wall: base)
            let store = SignallingCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "waiter-gate", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let duplicate = Task { try await ledger.reserve(request) }
            await store.waitUntilTransactionCount(3)
            let waitersBeforeRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersBeforeRefusal == 1, "gate 拒絕前 duplicate 已確定 coalesce", group: group)
            try await checks.expectError(.gateUnavailable, "owner 收到 gateUnavailable", group: group) {
                try await ledger.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: false)
            }
            let waitersAfterRefusal = await ledger.coalescedWaiterCount(for: request.key)
            try checks.expect(waitersAfterRefusal == 0, "gate 拒絕不留下 waiter", group: group)
            let retry = try await duplicate.value
            try checks.expect(retry == .reserved(request.key), "gate waiter 重新 admission 成為新 owner", group: group)
            let recoveredRow = await ledger.row(for: request.key)
            try checks.expect(recoveredRow?.state == .reserved, "gate recovery 留下新的 reserved row", group: group)
        }
        try await runWaiterRecoveryGate()
    }

    if checks.includes("waiter_revoked_gc_not_found") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runWaiterRevokedGcNotFound() async throws {
            let group = "waiter_revoked_gc_not_found"

            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = SignallingCloudCommandLedgerStore()
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "revoked-cancel", id: "parked-cancel", deadline: base.addingTimeInterval(60))
                _ = try await ledger.reserve(request, epochState: .uncertain)
                let duplicate = Task { try await ledger.reserve(request, epochState: .uncertain) }
                await store.waitUntilTransactionCount(3)
                let waitersBeforeGC = await ledger.coalescedWaiterCount(for: request.key)
                try checks.expect(waitersBeforeGC == 1,
                                  "cancel fixture 的 duplicate 已確定 parked", group: group)
                let removed = try await ledger.garbageCollect(
                    epochState: .uncertain,
                    permanentlyRevokedSenders: [request.key.viewerSender],
                    batchLimit: 1
                )
                try checks.expect(removed == 1, "revoked GC 刪掉 cancel fixture 的 reserved row", group: group)
                try await checks.expectError(.notFound, "GC 後 owner cancelReservation 收到 notFound", group: group) {
                    try await ledger.cancelReservation(request, refusal: .busy)
                }
                let waitersAfterNotFound = await ledger.coalescedWaiterCount(for: request.key)
                try checks.expect(waitersAfterNotFound == 0,
                                  "cancelReservation 的 notFound 仍同步喚醒 waiter", group: group)
                let retried = try await duplicate.value
                try checks.expect(retried == .reserved(request.key),
                                  "cancel fixture 的 waiter 有限步內重新 admission", group: group)
            }

            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = SignallingCloudCommandLedgerStore()
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "revoked-begin", id: "parked-begin", deadline: base.addingTimeInterval(60))
                _ = try await ledger.reserve(request, epochState: .uncertain)
                let duplicate = Task { try await ledger.reserve(request, epochState: .uncertain) }
                await store.waitUntilTransactionCount(3)
                let waitersBeforeGC = await ledger.coalescedWaiterCount(for: request.key)
                try checks.expect(waitersBeforeGC == 1,
                                  "begin fixture 的 duplicate 已確定 parked", group: group)
                let removed = try await ledger.garbageCollect(
                    epochState: .uncertain,
                    permanentlyRevokedSenders: [request.key.viewerSender],
                    batchLimit: 1
                )
                try checks.expect(removed == 1, "revoked GC 刪掉 begin fixture 的 reserved row", group: group)
                try await checks.expectError(.notFound, "GC 後 owner beginEffect 收到 notFound", group: group) {
                    try await ledger.beginEffect(request, epochState: .ready, latestRosterAndGateAllow: true)
                }
                let waitersAfterNotFound = await ledger.coalescedWaiterCount(for: request.key)
                try checks.expect(waitersAfterNotFound == 0,
                                  "beginEffect 的 notFound 仍同步喚醒 waiter", group: group)
                let retried = try await duplicate.value
                try checks.expect(retried == .reserved(request.key),
                                  "begin fixture 的 waiter 有限步內重新 admission", group: group)
            }
        }
        try await runWaiterRevokedGcNotFound()
    }

    if checks.includes("gc_dual_clock") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runGcDualClock() async throws {
            let group = "gc_dual_clock"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "dual-clock", deadline: base.addingTimeInterval(60))
            try await exerciseCompletedRow(ledger: ledger, request: request)
            clock.advance(wall: CloudCommandLedger.retentionSeconds + 1)
            let wallOnly = try await ledger.garbageCollect(epochState: .ready, batchLimit: 10)
            try checks.expect(wallOnly == 0, "wall 到期但 elapsed 未滿不得刪", group: group)
            clock.advance(continuousMilliseconds: CloudCommandLedger.retentionMilliseconds)
            _ = try await ledger.checkpointRetention()
            let both = try await ledger.garbageCollect(epochState: .ready, batchLimit: 10)
            try checks.expect(both == 1, "wall 與 elapsed 都滿才刪", group: group)
            let remainingCount = await ledger.rowCount()
            try checks.expect(remainingCount == 0, "雙時鐘到期 row 已移除", group: group)

            let exactExpiry = completedRow(
                sender: "wall-boundary",
                id: "wall-equals-expiry",
                createdAt: base.addingTimeInterval(-CloudCommandLedger.retentionSeconds),
                retainedMilliseconds: CloudCommandLedger.retentionMilliseconds
            )
            let exactStore = InMemoryCloudCommandLedgerStore(rows: [exactExpiry])
            let exactLedger = CloudCommandLedger(store: exactStore, clocks: CloudLedgerTestClock(wall: base).clocks)
            let exactRemoved = try await exactLedger.garbageCollect(epochState: .ready, batchLimit: 10)
            try checks.expect(exactRemoved == 1, "wall_now 精確等於 expires_at 時符合 GC 資格", group: group)
        }
        try await runGcDualClock()
    }

    if checks.includes("gc_wall_jump") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runGcWallJump() async throws {
            let group = "gc_wall_jump"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "wall-attack", deadline: base.addingTimeInterval(60))
            try await exerciseCompletedRow(ledger: ledger, request: request)
            clock.advance(wall: CloudCommandLedger.retentionSeconds * 2, continuousMilliseconds: 300_000)
            _ = try await ledger.checkpointRetention()
            let removed = try await ledger.garbageCollect(epochState: .ready, batchLimit: 10)
            try checks.expect(removed == 0, "五分鐘後 wall forward jump 不得提早刪", group: group)
            let retainedRow = await ledger.row(for: request.key)
            try checks.expect(retainedRow != nil, "forward jump 後 row 仍保留", group: group)
        }
        try await runGcWallJump()
    }

    if checks.includes("boot_change") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runBootChange() async throws {
            let group = "boot_change"
            let clock = CloudLedgerTestClock(wall: base, continuous: 1_000_000_000)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "boot", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            clock.advance(continuousMilliseconds: 36_000_000)
            _ = try await ledger.checkpointRetention()
            let beforeReboot = await ledger.row(for: request.key)!
            try checks.expect(beforeReboot.retainedElapsedMilliseconds == 36_000_000, "同 boot 累積 continuous delta", group: group)
            clock.boot = "boot-B"
            clock.continuous = 100
            clock.advance(wall: 99_999)
            _ = try await ledger.checkpointRetention()
            let afterReboot = await ledger.row(for: request.key)!
            try checks.expect(afterReboot.retainedElapsedMilliseconds == 36_000_000, "boot 改變保留累積值", group: group)
            try checks.expect(afterReboot.elapsedCheckpointContinuous == nil, "boot 改變清 checkpoint", group: group)
            clock.advance(continuousMilliseconds: 3_600_000)
            _ = try await ledger.checkpointRetention()
            let established = await ledger.row(for: request.key)!
            try checks.expect(established.retainedElapsedMilliseconds == 36_000_000, "reboot downtime 與首段不計入", group: group)
            clock.advance(continuousMilliseconds: 3_600_000)
            _ = try await ledger.checkpointRetention()
            let resumed = await ledger.row(for: request.key)!
            try checks.expect(resumed.retainedElapsedMilliseconds == 39_600_000, "新 boot checkpoint 後重新累積", group: group)
        }
        try await runBootChange()
    }

    if checks.includes("admission_boundaries") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runAdmissionBoundaries() async throws {
            let group = "admission_boundaries"
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 5_000, targetSender: "target", targetCount: 999, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "new-5000-actor-999", deadline: base.addingTimeInterval(60))
                let admission = try await ledger.reserve(request)
                try checks.expect(admission == .reserved(request.key), "5,000 且 sender 999 可 admission", group: group)
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 5_000, targetSender: "target", targetCount: 1_000, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "reject-5000-actor-1000", deadline: base.addingTimeInterval(60))
                try await checks.expectError(.idempotencyCapacity(scope: .actor, reason: .rowCap), "5,000 且 sender 1,000 觸發 actor row cap", group: group) {
                    _ = try await ledger.reserve(request)
                }
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 8_999, targetSender: "target", targetCount: 999, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "new-8999", deadline: base.addingTimeInterval(60))
                let admission = try await ledger.reserve(request)
                try checks.expect(admission == .reserved(request.key), "8,999 且 sender 999 可 admission", group: group)
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 9_000, targetSender: "target", targetCount: 99, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "new-9000", deadline: base.addingTimeInterval(60))
                let admission = try await ledger.reserve(request)
                try checks.expect(admission == .reserved(request.key), "9,000 fairness reserve 接受 sender 99", group: group)
                let fairnessUsed = await ledger.metricsSnapshot().fairnessReserveUsedTotal
                try checks.expect(fairnessUsed == 1, "fairness reserve admission 精確累加一次", group: group)
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 9_000, targetSender: "target", targetCount: 100, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "reject-100", deadline: base.addingTimeInterval(60))
                try await checks.expectError(.idempotencyCapacity(scope: .actor, reason: .fairness), "fairness reserve 拒絕 sender 100", group: group) {
                    _ = try await ledger.reserve(request)
                }
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let store = InMemoryCloudCommandLedgerStore(rows: admissionRows(total: 9_999, targetSender: "target", targetCount: 99, createdAt: base))
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "new-9999", deadline: base.addingTimeInterval(60))
                let admission = try await ledger.reserve(request)
                try checks.expect(admission == .reserved(request.key), "9,999 fairness reserve 最後一格接受 sender 99", group: group)
            }
            do {
                let clock = CloudLedgerTestClock(wall: base)
                let rows = admissionRows(total: 10_000, targetSender: "target", targetCount: 0, createdAt: base)
                let oldestKey = rows[0].key
                let store = InMemoryCloudCommandLedgerStore(rows: rows)
                let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
                let request = ledgerRequest(sender: "target", id: "reject-10000", deadline: base.addingTimeInterval(60))
                try await checks.expectError(.idempotencyCapacity(scope: .global, reason: .rowCap), "10,000 global hard cap 拒絕", group: group) {
                    _ = try await ledger.reserve(request)
                }
                let countAfterRefusal = await ledger.rowCount()
                let oldestAfterRefusal = await ledger.row(for: oldestKey)
                try checks.expect(countAfterRefusal == 10_000, "滿載拒絕不改 row 數", group: group)
                try checks.expect(oldestAfterRefusal != nil, "滿載不 eviction 未過期 active row", group: group)
            }
        }
        try await runAdmissionBoundaries()
    }

    if checks.includes("admission_gc_atomic") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runAdmissionGcAtomic() async throws {
            let group = "admission_gc_atomic"
            let old = base.addingTimeInterval(-CloudCommandLedger.retentionSeconds * 2)
            let rows = admissionRows(total: CloudCommandLedger.globalHardLimit, targetSender: "target", targetCount: 0, createdAt: old)
                .map { row -> CloudCommandLedgerRow in
                    var eligible = row
                    eligible.retainedElapsedMilliseconds = CloudCommandLedger.retentionMilliseconds
                    return eligible
                }
            let store = CountingCloudCommandLedgerStore(rows: rows)
            let ledger = CloudCommandLedger(store: store, clocks: CloudLedgerTestClock(wall: base).clocks)
            store.resetCount()
            let request = ledgerRequest(sender: "target", id: "atomic-gc-admission", deadline: base.addingTimeInterval(60))
            do {
                let admission = try await ledger.reserve(request, epochState: .ready)
                try checks.expect(admission == .reserved(request.key), "滿載但可回收的 store 在首次 admission 即接受", group: group)
            } catch {
                try checks.expect(false, "滿載但可回收的 store 在首次 admission 即接受（收到 \(error)）", group: group)
            }
            try checks.expect(store.transactionCount == 1, "bounded GC、capacity count、insert 共用唯一 transaction", group: group)
            let remainingCount = await ledger.rowCount()
            try checks.expect(remainingCount < CloudCommandLedger.globalHardLimit, "bounded batch 回收後新 row 已原子寫入", group: group)
            let metrics = await ledger.metricsSnapshot()
            try checks.expect(metrics.gcTotal[.expired] == UInt64(CloudCommandLedger.admissionGCBatchLimit),
                              "admission GC 精確累加整個 bounded batch", group: group)
        }
        try await runAdmissionGcAtomic()
    }

    if checks.includes("admission_gc_counter_on_refusal") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runAdmissionGcCounterOnRefusal() async throws {
            let group = "admission_gc_counter_on_refusal"
            let old = base.addingTimeInterval(-CloudCommandLedger.retentionSeconds * 2)
            let expired = (0..<5).map {
                completedRow(
                    sender: "expired-\($0)",
                    id: "collect-before-refusal-\($0)",
                    createdAt: old,
                    retainedMilliseconds: CloudCommandLedger.retentionMilliseconds
                )
            }
            let actorAtCap = admissionRows(total: 1_000, targetSender: "target", targetCount: 1_000, createdAt: base)
            let ledger = CloudCommandLedger(
                store: InMemoryCloudCommandLedgerStore(rows: actorAtCap + expired),
                clocks: CloudLedgerTestClock(wall: base).clocks
            )
            let request = ledgerRequest(sender: "target", id: "refused-after-gc", deadline: base.addingTimeInterval(60))
            try await checks.expectError(.idempotencyCapacity(scope: .actor, reason: .rowCap),
                                         "GC commit 後仍可因 actor cap 拒絕", group: group) {
                _ = try await ledger.reserve(request, epochState: .ready)
            }
            let rowsAfterRefusal = await ledger.rowCount()
            try checks.expect(rowsAfterRefusal == 1_000,
                              "capacity 拒絕前的五筆 GC 刪除確實已 commit", group: group)
            let metrics = await ledger.metricsSnapshot()
            try checks.expect(metrics.gcTotal[.expired] == 5,
                              "後續 capacity throw 不遺失已 commit 的五筆 GC counter", group: group)
            let actorRowCap = CloudLedgerCapacityLabel(scope: .actor, reason: .rowCap)
            try checks.expect(metrics.capacityRefusals[actorRowCap] == 1,
                              "actor row-cap refusal 精確累加一次", group: group)
        }
        try await runAdmissionGcCounterOnRefusal()
    }

    if checks.includes("checkpoint_cursor") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runCheckpointCursor() async throws {
            let group = "checkpoint_cursor"
            let rows = [
                completedRow(sender: "cursor", id: "a", createdAt: base),
                completedRow(sender: "cursor", id: "b", createdAt: base),
                completedRow(sender: "cursor", id: "c", createdAt: base),
            ]
            let clock = CloudLedgerTestClock(wall: base)
            let ledger = CloudCommandLedger(store: InMemoryCloudCommandLedgerStore(rows: rows), clocks: clock.clocks)
            for _ in rows.indices {
                clock.advance(continuousMilliseconds: 60_000)
                let processed = try await ledger.checkpointRetention(batchLimit: 1)
                try checks.expect(processed == 1, "每輪 bounded checkpoint 恰處理一列", group: group)
            }
            var retained: [UInt64] = []
            for row in rows {
                if let value = await ledger.row(for: row.key)?.retainedElapsedMilliseconds {
                    retained.append(value)
                }
            }
            try checks.expect(retained.count == rows.count, "三列仍都存在", group: group)
            try checks.expect(retained.allSatisfy { $0 > 0 }, "呼叫 N 輪後全部 N 列都有 checkpoint 累積", group: group)
        }
        try await runCheckpointCursor()
    }

    if checks.includes("retention_metrics") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runRetentionMetrics() async throws {
            let group = "retention_metrics"
            let old = base.addingTimeInterval(-CloudCommandLedger.retentionSeconds * 2)
            let row = completedRow(
                sender: "metrics",
                id: "over-retained",
                createdAt: old,
                retainedMilliseconds: CloudCommandLedger.retentionMilliseconds
            )
            let ledger = CloudCommandLedger(
                store: InMemoryCloudCommandLedgerStore(rows: [row]),
                clocks: CloudLedgerTestClock(wall: base).clocks
            )
            let snapshot = await ledger.metricsSnapshot()
            let fields = Dictionary(uniqueKeysWithValues: Mirror(reflecting: snapshot).children.compactMap { child in
                child.label.map { ($0, child.value) }
            })
            let overdue = fields["retentionOverdueSeconds"] as? TimeInterval
            let uncertain = fields["clockUncertainSeconds"] as? TimeInterval
            try checks.expect(overdue != nil, "snapshot 存在 retention_overdue_seconds 對應值", group: group)
            try checks.expect(uncertain != nil, "snapshot 存在 clock_uncertain_seconds 對應值", group: group)
            try checks.expect(overdue == CloudCommandLedger.retentionSeconds, "overdue 精確量出超過 wall retention 的一日", group: group)
            try checks.expect(uncertain == CloudCommandLedger.retentionSeconds, "clock uncertain 精確量出 wall 與 verified elapsed 差的一日", group: group)
        }
        try await runRetentionMetrics()
    }

    if checks.includes("digest_conflict") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runDigestConflict() async throws {
            let group = "digest_conflict"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let original = ledgerRequest(id: "digest", digest: "digest-original", deadline: base.addingTimeInterval(60))
            let outcome = CloudCommandNormalizedOutcome(code: .succeeded, payload: Data("cached-secret".utf8))
            try await exerciseCompletedRow(ledger: ledger, request: original, outcome: outcome)
            let changed = ledgerRequest(id: "digest", digest: "digest-changed", deadline: base.addingTimeInterval(60))
            try await checks.expectError(.idempotencyConflict, "digest 不同只回 conflict", group: group) {
                _ = try await ledger.reserve(changed)
            }
        }
        try await runDigestConflict()
    }

    if checks.includes("raw_reply_key") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runRawReplyKey() async throws {
            let group = "raw_reply_key"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let raw = Data([0x91, 0x22, 0xE7, 0x04, 0xAC, 0x6D, 0x53, 0xB8, 0x19, 0xFA, 0x30, 0xC1, 0x72, 0xD5, 0x08, 0xEE])
            let request = ledgerRequest(id: "raw-key", digest: "digest-without-raw-key", rawReplyKey: raw, deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let allPersistedBytes = try store.persistedBytes()
            try checks.expect(allPersistedBytes.range(of: raw) == nil, "持久層所有 bytes 都不含 raw reply key", group: group)
            let persistedRow = await ledger.row(for: request.key)
            try checks.expect(persistedRow?.replyKeyID == "reply-key-id-A", "只持久化 reply key id", group: group)
        }
        try await runRawReplyKey()
    }

    if checks.includes("deadline_before_lookup") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runDeadlineBeforeLookup() async throws {
            let group = "deadline_before_lookup"
            let expired = ledgerRequest(id: "expired-duplicate", digest: "expired-digest", deadline: base.addingTimeInterval(-1))
            let existing = completedRow(sender: expired.key.viewerSender, id: expired.key.requestID, createdAt: base.addingTimeInterval(-100), digest: "expired-digest")
            let store = CountingCloudCommandLedgerStore(rows: [existing])
            let clock = CloudLedgerTestClock(wall: base)
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            store.resetCount()
            try await checks.expectError(.deadlineExpired, "過期 duplicate 不得取得 cached outcome", group: group) {
                _ = try await ledger.reserve(expired)
            }
            try checks.expect(store.transactionCount == 0, "deadline check 發生在任何 ledger lookup 前", group: group)

            let exact = ledgerRequest(id: "deadline-equal", deadline: base)
            let exactStore = CountingCloudCommandLedgerStore(rows: [])
            let exactLedger = CloudCommandLedger(store: exactStore, clocks: clock.clocks)
            exactStore.resetCount()
            try await checks.expectError(.deadlineExpired, "wall_now 精確等於 deadline_at 時拒絕", group: group) {
                _ = try await exactLedger.reserve(exact)
            }
            try checks.expect(exactStore.transactionCount == 0, "deadline 相等的拒絕也發生在 lookup 前", group: group)
        }
        try await runDeadlineBeforeLookup()
    }

    if checks.includes("epoch_gc") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runEpochGc() async throws {
            let group = "epoch_gc"
            let old = base.addingTimeInterval(-CloudCommandLedger.retentionSeconds * 2)
            let normal = completedRow(sender: "normal", id: "normal", createdAt: old, retainedMilliseconds: CloudCommandLedger.retentionMilliseconds)
            let revoked = completedRow(sender: "revoked", id: "revoked", createdAt: old, retainedMilliseconds: CloudCommandLedger.retentionMilliseconds)
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore(rows: [normal, revoked])
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let uncertainRemoved = try await ledger.garbageCollect(epochState: .uncertain, permanentlyRevokedSenders: ["revoked"], batchLimit: 10)
            try checks.expect(uncertainRemoved == 1, "EpochGuard uncertain 仍可 permanent-revoke GC", group: group)
            let revokedAfterUncertain = await ledger.row(for: revoked.key)
            let normalAfterUncertain = await ledger.row(for: normal.key)
            try checks.expect(revokedAfterUncertain == nil, "revoked row 已刪", group: group)
            try checks.expect(normalAfterUncertain != nil, "EpochGuard uncertain 停止 normal TTL GC", group: group)
            let readyRemoved = try await ledger.garbageCollect(epochState: .ready, batchLimit: 10)
            try checks.expect(readyRemoved == 1, "EpochGuard ready 恢復 normal TTL GC", group: group)
            let gcTotal = await ledger.metricsSnapshot().gcTotal
            try checks.expect(gcTotal[.revoked] == 1, "explicit GC 精確累加一筆 revoked", group: group)
            try checks.expect(gcTotal[.expired] == 1, "explicit GC 精確累加一筆 expired", group: group)
        }
        try await runEpochGc()
    }

    if checks.includes("reserve_default_epoch_uncertain") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runReserveDefaultEpochUncertain() async throws {
            let group = "reserve_default_epoch_uncertain"
            let old = base.addingTimeInterval(-CloudCommandLedger.retentionSeconds * 2)
            let stale = completedRow(
                sender: "default-epoch",
                id: "stale",
                createdAt: old,
                retainedMilliseconds: CloudCommandLedger.retentionMilliseconds
            )
            let ledger = CloudCommandLedger(
                store: InMemoryCloudCommandLedgerStore(rows: [stale]),
                clocks: CloudLedgerTestClock(wall: base).clocks
            )
            let request = ledgerRequest(sender: "default-epoch", id: "new", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            let staleAfterAdmission = await ledger.row(for: stale.key)
            let rowsAfterAdmission = await ledger.rowCount()
            try checks.expect(staleAfterAdmission != nil,
                              "省略 epochState 時採 fail-closed，不執行 TTL GC", group: group)
            try checks.expect(rowsAfterAdmission == 2,
                              "省略 epochState 時保留 stale row 並新增 reservation", group: group)
        }
        try await runReserveDefaultEpochUncertain()
    }

    if checks.includes("metrics_labels") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runMetricsLabels() async throws {
            let group = "metrics_labels"
            try checks.expect(CloudLedgerActorRowsBucket.allCases.map(exhaustiveBucketValue) == [10, 100, 500, 1_000], "le label 只有 10|100|500|1000", group: group)
            try checks.expect(CloudLedgerCapacityScope.allCases.map(exhaustiveScopeValue) == ["global", "actor"], "scope label 只有 global|actor", group: group)
            try checks.expect(CloudLedgerCapacityReason.allCases.map(exhaustiveCapacityReasonValue) == ["row_cap", "fairness", "corrupt"], "capacity reason label domain 封閉", group: group)
            try checks.expect(CloudLedgerGCReason.allCases.map(exhaustiveGCReasonValue) == ["expired", "revoked"], "GC reason label domain 封閉", group: group)
            let names = [
                CloudCommandLedgerMetricsSnapshot.entriesName,
                CloudCommandLedgerMetricsSnapshot.actorRowsBucketName,
                CloudCommandLedgerMetricsSnapshot.oldestAgeName,
                CloudCommandLedgerMetricsSnapshot.capacityRefusalsName,
                CloudCommandLedgerMetricsSnapshot.fairnessReserveUsedName,
                CloudCommandLedgerMetricsSnapshot.inProgressName,
                CloudCommandLedgerMetricsSnapshot.gcName,
                CloudCommandLedgerMetricsSnapshot.retentionOverdueName,
                CloudCommandLedgerMetricsSnapshot.clockUncertainName,
            ]
            try checks.expect(names == [
                "ledger_entries_total",
                "ledger_actor_rows_bucket",
                "ledger_oldest_age_seconds",
                "ledger_capacity_refusals_total",
                "ledger_fairness_reserve_used_total",
                "ledger_in_progress_total",
                "ledger_gc_total",
                "retention_overdue_seconds",
                "clock_uncertain_seconds",
            ], "metric 名稱精確", group: group)
            let senderLabelWouldRequireAString = Mirror(reflecting: CloudLedgerCapacityLabel(scope: .global, reason: .rowCap)).children.count
            try checks.expect(senderLabelWouldRequireAString == 2, "capacity label 型別沒有 sender/device 欄位", group: group)

            let actorBoundaryRows = admissionRows(total: 21, targetSender: "actor-10", targetCount: 10, createdAt: base)
                .enumerated()
                .map { index, row in
                    guard row.key.viewerSender != "actor-10" else { return row }
                    return completedRow(sender: "actor-11", id: "actor-11-\(index)", createdAt: base)
                }
            let boundaryLedger = CloudCommandLedger(
                store: InMemoryCloudCommandLedgerStore(rows: actorBoundaryRows),
                clocks: CloudLedgerTestClock(wall: base).clocks
            )
            let boundaryBuckets = await boundaryLedger.metricsSnapshot().actorRowsBuckets
            try checks.expect(boundaryBuckets[.le10] == 1, "剛好 10 列的 actor 計入 le=10，11 列的 actor 不計入", group: group)
            try checks.expect(boundaryBuckets[.le100] == 2, "10 與 11 列的 actor 都累積計入 le=100", group: group)
            try checks.expect(boundaryBuckets[.le500] == 2, "10 與 11 列的 actor 都累積計入 le=500", group: group)
            try checks.expect(boundaryBuckets[.le1000] == 2, "10 與 11 列的 actor 都累積計入 le=1000", group: group)
        }
        try await runMetricsLabels()
    }

    if checks.includes("pre_effect_release") {
        // Each group becomes its own coroutine. The statements below are unchanged;
        // only the function they live in is new, which is the whole of this change.
        func runPreEffectRelease() async throws {
            let group = "pre_effect_release"
            let clock = CloudLedgerTestClock(wall: base)
            let store = InMemoryCloudCommandLedgerStore()
            let ledger = CloudCommandLedger(store: store, clocks: clock.clocks)
            let request = ledgerRequest(id: "busy", deadline: base.addingTimeInterval(60))
            _ = try await ledger.reserve(request)
            try await checks.expectError(.reservationReleased(.busy), "pre-effect release 回報具體 busy refusal", group: group) {
                try await ledger.cancelReservation(request, refusal: .busy)
            }
            let rowAfterBusy = await ledger.row(for: request.key)
            try checks.expect(rowAfterBusy == nil, "busy 在 PONR 前原子刪 reserved", group: group)
            let retry = try await ledger.reserve(request)
            try checks.expect(retry == .reserved(request.key), "pre-effect refusal 後可 retry", group: group)
        }
        try await runPreEffectRelease()
    }

    if checks.count <= 0 {
        throw CloudCommandLedgerTestFailure(group: "harness", check: "沒有執行任何 check")
    }
    return checks.count
}
