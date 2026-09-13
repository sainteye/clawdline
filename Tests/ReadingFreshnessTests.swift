import Foundation

/// Deterministic seams for the freshness policy. Nothing here sleeps: a policy whose numbers are
/// seconds cannot be covered by a suite that waits them out, and a suite that waits them out is
/// one nobody runs.
private final class FrozenReadingClock {
    private(set) var now: TimeInterval = 1_000

    func install() { ReadingClock.forTesting = { [unowned self] in self.now } }
    func advance(_ seconds: TimeInterval) { now += seconds }
    static func uninstall() { ReadingClock.forTesting = nil }
}

/// A reading whose cost and outcome the test decides, so the service can be asked what it does
/// with a refusal without needing a Mac with a dialog on it.
private struct FakeReading {
    let body: String
    let refusal: String?
}

private final class ReliabilityConnection {}
private final class ReliabilityScheduler {
    final class Job {
        let action: () -> Void
        var cancelled = false
        init(_ action: @escaping () -> Void) { self.action = action }
    }
    let limit: Int
    private(set) var jobs: [Job] = []
    init(limit: Int = .max) { self.limit = limit }
    func schedule(_ after: TimeInterval, _ action: @escaping () -> Void) -> (() -> Void)? {
        _ = after
        guard jobs.count < limit else { return nil }
        let job = Job(action); jobs.append(job)
        return { [weak self, weak job] in
            guard let self, let job, !job.cancelled else { return }
            job.cancelled = true
            self.jobs.removeAll { $0 === job }
        }
    }
    func fireUncancelled() {
        let ready = jobs.filter { !$0.cancelled }; jobs.removeAll()
        for job in ready where !job.cancelled { job.action() }
    }
}
private enum ReliabilityWriteFailure: Error { case injected }
private func reliabilityLimits(connections: Int = 3, connectionRefusals: Int = 2,
                               requestBytes: Int = 10,
                               streams: Int = 2, streamBytes: Int = 100,
                               aggregateStreamBytes: Int = 200) -> HTTPReliability.Limits {
    HTTPReliability.Limits(
        connections: connections, connectionRefusals: connectionRefusals,
        aggregateRequestBytes: requestBytes,
        requestReadSeconds: 1, streams: streams, streamBytes: streamBytes,
        aggregateStreamBytes: aggregateStreamBytes, streamWriteSeconds: 1)
}

private func runHTTPReliabilityTests() {
    group("HTTP admission bounds connections, bytes, and slow request lifetime") {
        let scheduler = ReliabilityScheduler()
        let owner = HTTPReliability(
            limits: reliabilityLimits(connections: 2, connectionRefusals: 1),
            schedule: scheduler.schedule)
        let first = ReliabilityConnection(), second = ReliabilityConnection()
        let firstID = ObjectIdentifier(first), secondID = ObjectIdentifier(second)
        var timedOut: [String] = []
        expect("two connections reach the ceiling",
               owner.admitConnection(firstID) { timedOut.append("first") }, .admitted)
        _ = owner.admitConnection(secondID) { timedOut.append("second") }
        expect("the connection above it is refused",
               owner.admitConnection(ObjectIdentifier(ReliabilityConnection())) {},
               .refused(limit: 2))
        let refusalOne = ReliabilityConnection(), refusalTwo = ReliabilityConnection()
        expect("one over-capacity response enters its own bounded lane",
               owner.admitCapacityResponse(ObjectIdentifier(refusalOne)), .admitted)
        expect("the refusal lane drops work beyond its hard cap",
               owner.admitCapacityResponse(ObjectIdentifier(refusalTwo)), .dropped(limit: 1))
        check("the refusal lane is accounted independently",
              owner.metrics.capacityResponsesCurrent == 1
                && owner.metrics.capacityResponseDrops == 1)
        owner.closeCapacityResponse(ObjectIdentifier(refusalOne))
        expect("one request body is charged", owner.retainRequestBytes(6, for: firstID), .retained)
        expect("aggregate debt refuses before retaining the chunk",
               owner.retainRequestBytes(5, for: secondID), .refused(limit: 10, current: 6))
        check("a complete read cancels only its slowloris timer", owner.finishRequestRead(for: firstID))
        expect("normal completion removes deadline closure retention immediately", scheduler.jobs.count, 1)
        scheduler.fireUncancelled()
        expect("only the incomplete read expires", timedOut, ["second"])
        owner.finishRequest(for: firstID); owner.closeConnection(firstID)
        check("response and close reclaim charged debt",
              owner.metrics.requestBytesCurrent == 0 && owner.metrics.connectionsCurrent == 1)
        expect("missing identity fails closed", owner.retainRequestBytes(1, for: nil), .unknownIdentity)
    }

    group("SSE output coalesces snapshots while unrelated HTTP work continues") {
        let scheduler = ReliabilityScheduler()
        let owner = HTTPReliability(
            limits: reliabilityLimits(requestBytes: 32, streamBytes: 256,
                                      aggregateStreamBytes: 512), schedule: scheduler.schedule)
        let stream = ReliabilityConnection(), health = ReliabilityConnection()
        let id = ObjectIdentifier(stream)
        var writes: [String] = [], completions: [(Error?) -> Void] = []
        _ = owner.admitConnection(id) {}
        _ = owner.openStream(id, send: { bytes, done in
            writes.append(String(data: bytes, encoding: .utf8) ?? ""); completions.append(done)
        }, evict: { _ in })
        _ = owner.enqueueStreamFrame(Data("hello".utf8), kind: .control, for: id)
        _ = owner.enqueueStreamFrame(Data("sessions-old".utf8),
                                     kind: .fullSnapshot("sessions"), for: id)
        expect("new current sessions replaces the pending old snapshot",
               owner.enqueueStreamFrame(Data("sessions-current".utf8),
                                        kind: .fullSnapshot("sessions"), for: id), .coalesced)
        _ = owner.enqueueStreamFrame(Data("orchestrator-current".utf8),
                                     kind: .fullSnapshot("orchestrator"), for: id)
        expect("a suspended completion keeps one write in flight", writes, ["hello"])
        check("health/other lanes still admit and retain independently",
              owner.admitConnection(ObjectIdentifier(health)) {} == .admitted
                && owner.retainRequestBytes(4, for: ObjectIdentifier(health)) == .retained)
        completions.removeFirst()(nil); completions.removeFirst()(nil); completions.removeFirst()(nil)
        expect("drain sends only newest full snapshots",
               writes, ["hello", "sessions-current", "orchestrator-current"])
        check("completion reclaims output debt and records coalescing",
              owner.metrics.streamBytesCurrent == 0 && owner.metrics.snapshotCoalesces == 1)
        check("completed frames leave no scheduled closure debt",
              scheduler.jobs.count == 1 && owner.metrics.deadlineTasksCurrent == 1)
        owner.closeConnection(ObjectIdentifier(health))
        check("closing the last read releases the deadline owner", scheduler.jobs.isEmpty
              && owner.metrics.deadlineTasksCurrent == 0)
    }

    group("SSE completion failures and ceilings evict only the slow consumer") {
        let capacityScheduler = ReliabilityScheduler()
        let capacity = HTTPReliability(
            limits: reliabilityLimits(streams: 1, streamBytes: 8, aggregateStreamBytes: 8),
            schedule: capacityScheduler.schedule)
        let slow = ReliabilityConnection(), extra = ReliabilityConnection()
        let slowID = ObjectIdentifier(slow), extraID = ObjectIdentifier(extra)
        var evictions: [HTTPReliability.EvictionReason] = []
        _ = capacity.admitConnection(slowID) {}; _ = capacity.admitConnection(extraID) {}
        _ = capacity.openStream(slowID, send: { _, _ in }, evict: { evictions.append($0) })
        expect("the SSE count ceiling refuses only the extra consumer",
               capacity.openStream(extraID, send: { _, _ in }, evict: { _ in }),
               .refused(limit: 1))
        expect("oversized outstanding bytes evict their consumer",
               capacity.enqueueStreamFrame(Data(repeating: 1, count: 9), kind: .event, for: slowID),
               .evicted)
        expect("capacity eviction reclaims bytes", (evictions.first?.logName ?? "")
               + ":" + String(capacity.metrics.streamBytesCurrent), "capacity:0")

        let errorScheduler = ReliabilityScheduler()
        let errorOwner = HTTPReliability(
            limits: reliabilityLimits(streams: 1), schedule: errorScheduler.schedule)
        let broken = ReliabilityConnection(), brokenID = ObjectIdentifier(broken)
        var completion: ((Error?) -> Void)?, errorReason: HTTPReliability.EvictionReason?
        _ = errorOwner.admitConnection(brokenID) {}
        _ = errorOwner.openStream(brokenID, send: { _, done in completion = done },
                                  evict: { errorReason = $0 })
        _ = errorOwner.enqueueStreamFrame(Data("frame".utf8), kind: .event, for: brokenID)
        completion?(ReliabilityWriteFailure.injected)
        expect("completion error evicts instead of retaining silently", errorReason, .writeError)

        let timeoutScheduler = ReliabilityScheduler()
        let timeoutOwner = HTTPReliability(
            limits: reliabilityLimits(streams: 1), schedule: timeoutScheduler.schedule)
        let stalled = ReliabilityConnection(), stalledID = ObjectIdentifier(stalled)
        var timeoutReason: HTTPReliability.EvictionReason?
        _ = timeoutOwner.admitConnection(stalledID) {}
        _ = timeoutOwner.openStream(stalledID, send: { _, _ in }, evict: { timeoutReason = $0 })
        _ = timeoutOwner.enqueueStreamFrame(Data("frame".utf8), kind: .event, for: stalledID)
        timeoutScheduler.fireUncancelled()
        expect("missing completion evicts at the slow-consumer deadline",
               timeoutReason, .completionTimeout)
        check("eviction leaves that connection's unrelated HTTP accounting live",
              timeoutOwner.metrics.connectionsCurrent == 1 && timeoutOwner.metrics.streamsCurrent == 0)

        let aggregateScheduler = ReliabilityScheduler()
        let aggregate = HTTPReliability(
            limits: reliabilityLimits(streams: 2, streamBytes: 10, aggregateStreamBytes: 12),
            schedule: aggregateScheduler.schedule)
        let slowDebt = ReliabilityConnection(), healthy = ReliabilityConnection()
        let slowDebtID = ObjectIdentifier(slowDebt), healthyID = ObjectIdentifier(healthy)
        var aggregateEvictions: [String] = [], healthyWrites = 0
        _ = aggregate.admitConnection(slowDebtID) {}
        _ = aggregate.admitConnection(healthyID) {}
        _ = aggregate.openStream(slowDebtID, send: { _, _ in },
                                 evict: { aggregateEvictions.append("slow:" + $0.logName) })
        _ = aggregate.openStream(healthyID, send: { _, done in
            healthyWrites += 1; done(nil)
        }, evict: { aggregateEvictions.append("healthy:" + $0.logName) })
        _ = aggregate.enqueueStreamFrame(Data(repeating: 1, count: 8),
                                         kind: .event, for: slowDebtID)
        expect("aggregate overflow evicts the actual largest slow debt owner",
               aggregate.enqueueStreamFrame(Data(repeating: 2, count: 5),
                                            kind: .event, for: healthyID), .enqueued)
        expect("the healthy producer survives after the slow owner is accounted",
               aggregateEvictions, ["slow:aggregate_capacity"])
        check("the healthy frame drains and aggregate debt is reclaimed",
              healthyWrites == 1 && aggregate.metrics.streamBytesCurrent == 0
                && aggregate.metrics.streamAggregateCapacityEvictions == 1)
    }

    group("HTTP deadlines are one cancellable bounded owner") {
        let queue = DispatchQueue(label: "clawdline.tests.http-deadlines")
        queue.sync {
            let deadlines = BoundedHTTPDeadlineScheduler(queue: queue, limit: 2)
            let first = deadlines.schedule(after: 60) {}
            let second = deadlines.schedule(after: 60) {}
            check("the single timer owner accounts its explicit ceiling",
                  first != nil && second != nil && deadlines.pendingCount == 2)
            check("work above the deadline ceiling is refused",
                  deadlines.schedule(after: 60) {} == nil)
            first?()
            expect("cancellation removes the retained closure immediately",
                   deadlines.pendingCount, 1)
            second?()
            expect("normal cleanup leaves no deadline closure debt", deadlines.pendingCount, 0)
        }

        let refusing = ReliabilityScheduler(limit: 1)
        let owner = HTTPReliability(
            limits: reliabilityLimits(connections: 2), schedule: refusing.schedule)
        let first = ReliabilityConnection(), second = ReliabilityConnection()
        _ = owner.admitConnection(ObjectIdentifier(first)) {}
        expect("scheduler exhaustion fails closed without admitting another connection",
               owner.admitConnection(ObjectIdentifier(second)) {}, .refused(limit: 2))
        check("deadline refusal rolls back connection accounting",
              owner.metrics.connectionsCurrent == 1 && owner.metrics.deadlineTaskRefusals == 1)
    }

    group("SSE reconnect starts from hello and current full snapshots without replay") {
        let scheduler = ReliabilityScheduler()
        let owner = HTTPReliability(
            limits: reliabilityLimits(streams: 1), schedule: scheduler.schedule)
        func connect(_ connection: ReliabilityConnection, generation: String) -> [String] {
            let id = ObjectIdentifier(connection); var frames: [String] = []
            _ = owner.admitConnection(id) {}
            _ = owner.openStream(id, send: { bytes, done in
                frames.append(String(data: bytes, encoding: .utf8) ?? ""); done(nil)
            }, evict: { _ in })
            for (name, kind) in [("hello-" + generation, HTTPReliability.FrameKind.control),
                                 ("sessions-" + generation, .fullSnapshot("sessions")),
                                 ("orchestrator-" + generation, .fullSnapshot("orchestrator"))] {
                _ = owner.enqueueStreamFrame(Data(name.utf8), kind: kind, for: id)
            }
            owner.closeConnection(id); return frames
        }
        _ = connect(ReliabilityConnection(), generation: "one")
        let reconnected = connect(ReliabilityConnection(), generation: "two")
        expect("reconnect realigns hello then current full snapshots",
               reconnected, ["hello-two", "sessions-two", "orchestrator-two"])
        check("it invents no replay from the disconnected generation",
              !reconnected.contains { $0.contains("one") })
    }
}

func runReadingFreshnessTests() {
    runHTTPReliabilityTests()
    let clock = FrozenReadingClock()
    clock.install()
    defer { FrozenReadingClock.uninstall() }

    let inline: FreshReadings<FakeReading>.Executor = { work in work() }
    let onOwner: (@escaping () -> Void) -> Void = { work in work() }

    func classify(_ reading: FakeReading) -> FreshReadings<FakeReading>.Verdict {
        reading.refusal.map { .refused($0) } ?? .good
    }

    let quick = FreshReadings<FakeReading>.Policy(freshFor: 2, serveFor: 60)

    /// Every read in this suite goes through here, so the admission trio is stated once and any
    /// group that cares about backpressure can replace just that part.
    @discardableResult
    func read(_ readings: FreshReadings<FakeReading>,
              _ key: String = "info:A",
              policy: FreshReadings<FakeReading>.Policy? = nil,
              admit: @escaping () -> Bool = { true },
              refusal: @escaping () -> FakeReading = { FakeReading(body: "429", refusal: "busy") },
              execute: @escaping FreshReadings<FakeReading>.Executor,
              release: @escaping () -> Void = {},
              compute: @escaping () -> FakeReading,
              deliver: @escaping (FreshReadings<FakeReading>.Answer) -> Void)
        -> FreshReadings<FakeReading>.WaiterToken? {
        readings.read(key, policy: policy ?? quick, admit: admit, refusal: refusal,
                      execute: execute, compute: compute, classify: classify,
                      completeOnOwner: onOwner, release: release, deliver: deliver)
    }

    group("a warm reading is answered without asking the Mac again") {
        let readings = FreshReadings<FakeReading>()
        var reads = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        func ask() {
            read(readings, "info:A", execute: inline,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          deliver: { answers.append($0) })
        }
        ask()
        check("the first ask with nothing stored takes the reading itself", reads == 1)
        check("and it is delivered", answers.count == 1)
        check("a value this request read has no age", answers.last?.age == 0)
        check("a value this request read is marked as read", answers.last?.provenance == .read)

        clock.advance(1)
        ask()
        check("inside freshFor nothing is read again", reads == 1)
        check("the stored reading is served", answers.last?.value.body == "card 1")
        check("and it is marked fresh", answers.last?.provenance == .fresh)
        check("with its real age, not zero", answers.last?.age == 1)
    }

    group("a stale reading goes out first and the refresh happens behind it") {
        let readings = FreshReadings<FakeReading>()
        var reads = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var deliveredBeforeRead: [Int] = []
        func ask() {
            read(readings, "info:A", execute: inline,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          deliver: { answers.append($0); deliveredBeforeRead.append(reads) })
        }
        ask()
        clock.advance(10)
        ask()
        check("past freshFor the stored reading is still what goes out", answers.last?.value.body == "card 1")
        check("it is marked stale rather than fresh", answers.last?.provenance == .stale)
        check("its age is published", answers.last?.age == 10)
        check("the reader was answered before the refresh ran, not after",
              deliveredBeforeRead.last == 1)
        check("and a refresh did run behind it", reads == 2)

        clock.advance(1)
        ask()
        check("the refresh replaced the reading", answers.last?.value.body == "card 2")
        check("and the replacement is fresh again", answers.last?.provenance == .fresh)
    }

    group("a refused refresh ages the reading it could not replace") {
        let readings = FreshReadings<FakeReading>()
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var refusing = false
        func ask() {
            read(readings, execute: inline, compute: {
                refusing ? FakeReading(body: "502", refusal: "iterm_attention_required")
                         : FakeReading(body: "good card", refusal: nil)
            }, deliver: { answers.append($0) })
        }
        ask()
        refusing = true
        clock.advance(10)
        ask()   // serves the good card, refresh refuses behind it
        clock.advance(1)
        ask()
        check("a refusal never becomes the reading everything is served from",
              answers.last?.value.body == "good card")
        check("the refusal is published as the reason the reading is stale",
              answers.last?.staleReason == "iterm_attention_required")
        check("a refusal does not reset the age — the reading keeps walking toward serveFor",
              answers.last?.age == 11)

        refusing = false
        clock.advance(1)
        ask()
        clock.advance(1)
        ask()
        check("a reading that succeeds again clears the stale reason",
              answers.last?.staleReason == nil)
    }

    group("one expensive read serves every request that arrived without one") {
        let readings = FreshReadings<FakeReading>()
        var reads = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var release: (() -> Void)?
        // A worker that holds the read open, so eight requests really are in flight together
        // rather than merely being written down that way.
        let parked: FreshReadings<FakeReading>.Executor = { work in release = work }

        for _ in 0..<8 {
            read(readings, "info:A", execute: parked,
                          compute: { reads += 1; return FakeReading(body: "one card", refusal: nil) },
                          deliver: { answers.append($0) })
        }
        check("eight cold requests park rather than answer", answers.isEmpty)
        check("and exactly one read is in flight for them", readings.inFlightKeysForTesting == ["info:A"])
        check("all eight are waiting on it", readings.waiterCountForTesting("info:A") == 8)
        release?()
        check("the read ran once for all eight", reads == 1)
        check("and all eight were answered", answers.count == 8)
        check("every one of them got the same reading",
              answers.allSatisfy { $0.value.body == "one card" })
        check("the request that asked for the read is recorded as having read it",
              answers.first?.provenance == .read)
        check("the seven that shared it are recorded as having joined it",
              answers.dropFirst().allSatisfy { $0.provenance == .joined })
        check("nothing is left waiting", readings.waiterCountForTesting("info:A") == 0)
        check("and nothing is left in flight", readings.inFlightKeysForTesting.isEmpty)
    }

    group("past serveFor a reading stops being an answer") {
        let readings = FreshReadings<FakeReading>()
        var reads = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var release: (() -> Void)?
        let parked: FreshReadings<FakeReading>.Executor = { work in release = work }
        func ask(_ executor: @escaping FreshReadings<FakeReading>.Executor) {
            read(readings, "info:A", execute: executor,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          deliver: { answers.append($0) })
        }
        ask(inline)
        clock.advance(61)
        ask(parked)
        check("a reading older than serveFor is not served, however well labelled",
              answers.count == 1)
        check("the request waits for the truth instead", readings.waiterCountForTesting("info:A") == 1)
        release?()
        check("and gets it", answers.last?.value.body == "card 2")
        check("with no age on it", answers.last?.age == 0)
    }

    group("a request waiting on a refresh that fails is answered, not refused") {
        let readings = FreshReadings<FakeReading>()
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var refusing = false
        var release: (() -> Void)?
        let parked: FreshReadings<FakeReading>.Executor = { work in release = work }
        func ask(_ executor: @escaping FreshReadings<FakeReading>.Executor) {
            read(readings, execute: executor, compute: {
                refusing ? FakeReading(body: "502", refusal: "busy")
                         : FakeReading(body: "good card", refusal: nil)
            }, deliver: { answers.append($0) })
        }
        ask(inline)
        refusing = true
        clock.advance(61)          // too old to serve
        ask(parked)                // so this one waits
        release?()
        check("a waiter whose read was refused is handed the last good reading",
              answers.last?.value.body == "good card")
        check("labelled with why it could not be replaced", answers.last?.staleReason == "busy")
        check("and with the age that makes that judgeable", answers.last?.age == 61)

        clock.advance(200)         // now past even the failure grace
        var late: [FreshReadings<FakeReading>.Answer] = []
        read(readings, "info:A", execute: inline,
                      compute: { FakeReading(body: "502", refusal: "busy") },
                      deliver: { late.append($0) })
        check("but a reading nobody can stand behind any more gives way to the refusal",
              late.last?.value.body == "502")
    }

    group("keys do not read each other's readings") {
        let readings = FreshReadings<FakeReading>()
        var answers: [String: FreshReadings<FakeReading>.Answer] = [:]
        for key in ["info:A", "info:B"] {
            read(readings, key, execute: inline,
                          compute: { FakeReading(body: "card for \(key)", refusal: nil) },
                          deliver: { answers[key] = $0 })
        }
        check("each key keeps its own reading", answers["info:A"]?.value.body == "card for info:A")
        check("and does not answer with another's", answers["info:B"]?.value.body == "card for info:B")
        check("both are stored", readings.storedKeysForTesting == ["info:A", "info:B"])
    }

    group("finished readings have a lifetime memory bound") {
        let readings = FreshReadings<FakeReading>(capacity: 2)
        for key in ["info:A", "info:B", "info:C"] {
            read(readings, key, execute: inline,
                 compute: { FakeReading(body: key, refusal: nil) }, deliver: { _ in })
        }
        check("the newest two readings remain and the oldest key is reclaimed",
              readings.storedKeysForTesting == ["info:B", "info:C"])
    }

    group("a full lane refuses only a request it had nothing to answer") {
        let readings = FreshReadings<FakeReading>()
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var reads = 0
        var laneOpen = true
        func ask() {
            read(readings, admit: { laneOpen }, execute: inline,
                 compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                 deliver: { answers.append($0) })
        }
        ask()
        check("a reading is taken while the lane has room", reads == 1)

        // **This is the whole point of moving the admission.** The lane is full and the reading
        // is stale: before, that combination was a 429 for a card the Mac was already holding.
        laneOpen = false
        clock.advance(10)
        ask()
        check("a full lane does not refuse a reader who could be served",
              answers.last?.value.body == "card 1")
        check("the stale answer goes out as a stale answer, not as a refusal",
              answers.last?.provenance == .stale)
        check("and the refresh it would have started is dropped rather than queued", reads == 1)

        // With nothing servable, the lane's answer is the only honest one left.
        clock.advance(100)
        ask()
        check("past serveFor a full lane is the one case that still refuses",
              answers.last?.value.body == "429")
        check("and it did not sneak a read past the closed lane", reads == 1)

        laneOpen = true
        ask()
        check("the lane reopening is enough to get a real reading again",
              answers.last?.value.body == "card 2")
    }

    group("an admitted read is released exactly once") {
        let readings = FreshReadings<FakeReading>()
        var releases = 0
        var admissions = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        var laneOpen = true
        func ask(_ executor: @escaping FreshReadings<FakeReading>.Executor) {
            read(readings, admit: { admissions += 1; return laneOpen }, execute: executor,
                 release: { releases += 1 },
                 compute: { FakeReading(body: "card", refusal: nil) },
                 deliver: { answers.append($0) })
        }
        ask(inline)
        check("one admission for one read", admissions == 1)
        check("and one release for it", releases == 1)

        clock.advance(1)
        ask(inline)
        check("a fresh answer asks the lane for nothing", admissions == 1)
        check("so it releases nothing", releases == 1)

        laneOpen = false
        clock.advance(100)
        ask(inline)
        check("a refused admission is not released — the count would go negative", releases == 1)

        // Eight waiters, one admission: a lane whose depth was spent per *request* rather than
        // per *read* would run out on a page with eight cards on it.
        laneOpen = true
        readings.forgetForTesting()
        var release: (() -> Void)?
        let parked: FreshReadings<FakeReading>.Executor = { work in release = work }
        let admittedBefore = admissions
        for _ in 0..<8 { ask(parked) }
        check("eight cold requests spend one admission between them",
              admissions == admittedBefore + 1)
        release?()
        check("and return it once", releases == 2)
    }

    group("same-key retries have a bounded parked waiter set") {
        let readings = FreshReadings<FakeReading>(
            waiterLimits: .init(perKey: 2, total: 3))
        var finish: (() -> Void)?
        let parked: FreshReadings<FakeReading>.Executor = { work in finish = work }
        var answers: [String] = []
        func ask() -> FreshReadings<FakeReading>.WaiterToken? {
            read(readings, execute: parked,
                 compute: { FakeReading(body: "card", refusal: nil) },
                 deliver: { answers.append($0.value.body) })
        }

        let owner = ask(), duplicate = ask(), refused = ask()
        check("one owner and one duplicate retry are parked",
              owner != nil && duplicate != nil && readings.waiterCountForTesting("info:A") == 2)
        check("the retry above the same-key ceiling is refused immediately", refused == nil)
        expect("the bounded refusal uses the lane's existing answer", answers, ["429"])
        expect("waiter refusal is observable", readings.waiterMetrics.refused, 1)
        finish?()
        expect("the admitted owner and joiner share the completed reading",
               answers, ["429", "card", "card"])
        expect("settlement reclaims all parked waiter debt", readings.waiterMetrics.current, 0)
    }

    group("cancelling a disconnected fresh-read waiter reclaims its debt") {
        let readings = FreshReadings<FakeReading>(
            waiterLimits: .init(perKey: 2, total: 2))
        var finish: (() -> Void)?
        let parked: FreshReadings<FakeReading>.Executor = { work in finish = work }
        var disconnectedDelivered = false
        var retryAnswer: String?
        let disconnected = read(
            readings, execute: parked,
            compute: { FakeReading(body: "card", refusal: nil) },
            deliver: { _ in disconnectedDelivered = true })

        check("a cold connection receives a cancellation identity", disconnected != nil)
        let cancelled = disconnected.map { readings.cancel($0) } ?? false
        check("disconnect removes the closure it would otherwise retain", cancelled)
        expect("disconnect accounting reaches zero", readings.waiterMetrics.current, 0)
        let retry = read(
            readings, execute: parked,
            compute: { FakeReading(body: "card", refusal: nil) },
            deliver: { retryAnswer = $0.value.body })
        check("the reclaimed same-key place admits a duplicate retry", retry != nil)
        finish?()
        check("the disconnected callback is never invoked", !disconnectedDelivered)
        expect("the live retry receives the shared result", retryAnswer, "card")
        expect("cancellation remains observable", readings.waiterMetrics.cancelled, 1)
    }

    group("the trace separates waiting from working") {
        let trace = ReadingTrace(capacity: 4)
        func span(_ lane: String, wait: Int, run: Int, outcome: Int = 200,
                  provenance: String = "read") -> ReadingTrace.Span {
            ReadingTrace.Span(at: Date(), lane: lane, key: "k", queueWaitMs: wait,
                              executeMs: run, provenance: provenance, ageSeconds: 0,
                              outcome: outcome, staleReason: nil)
        }
        trace.record(span("reading", wait: 0, run: 100))
        trace.record(span("reading", wait: 1200, run: 90))
        trace.record(span("reading", wait: 40, run: 95, outcome: 429))
        trace.record(span("transcript", wait: 5, run: 3000, provenance: "stale"))
        let shape = Dictionary(uniqueKeysWithValues: trace.shape().map { ($0.lane, $0) })
        check("a lane's readings are counted", shape["reading"]?.count == 3)
        check("a request that waited 1.2 s is visible as waiting, not as cost",
              shape["reading"]?.queueWaitP99 == 1200)
        check("and its own cost is reported separately", shape["reading"]?.executeP99 == 100)
        check("refusals are counted rather than averaged away", shape["reading"]?.refusals == 1)
        check("a lane that was slow on its own is not blamed for queueing",
              shape["transcript"]?.queueWaitP99 == 5 && shape["transcript"]?.executeP99 == 3000)
        check("stale service is counted", shape["transcript"]?.servedStale == 1)

        trace.record(span("late", wait: 7, run: 7))
        let recent = trace.recent()
        check("the ring keeps its capacity and no more", recent.count == 4)
        check("newest first, because that is what an incident is read from",
              recent.first?.lane == "late")
        check("and the oldest span was the one dropped",
              !recent.contains { $0.lane == "reading" && $0.queueWaitMs == 0 })
    }

    group("percentiles answer for an empty lane instead of crashing") {
        check("an empty lane is zero, not a trap", ReadingTrace.percentile([], 0.99) == 0)
        check("one reading is its own p99", ReadingTrace.percentile([42], 0.99) == 42)
        check("p50 of an even count takes the upper of the two middles",
              ReadingTrace.percentile([1, 2, 3, 4], 0.5) == 2)
        check("p99 is the slowest when the slowest is 1% of the readings",
              ReadingTrace.percentile(Array(1...100), 0.99) == 99)
        check("p100 is the slowest", ReadingTrace.percentile(Array(1...100), 1.0) == 100)
    }

    group("uncached project reads have bounded parallelism, queueing and deadlines") {
        var now: TimeInterval = 1_000
        var workers: [() -> Void] = []
        var answers: [ProjectReadCoordinator.Outcome] = []
        let reads = ProjectReadCoordinator(
            limits: .init(active: 2, outstanding: 3, queueWaitSeconds: 1,
                          requestSeconds: 4),
            now: { now }, execute: { workers.append($0) },
            deliverOnOwner: { $0() }, startTimer: false)
        for _ in 0..<3 {
            _ = reads.start(work: { .status(200) }, deliver: { answers.append($0) })
        }
        expect("only the active limit is launched", workers.count, 2)
        expect("all active and queued work is accounted", reads.metrics.current, 3)
        _ = reads.start(work: { .status(200) }, deliver: { answers.append($0) })
        expect("work above the explicit outstanding cap is refused",
               answers, [.capacity(limit: 3, current: 3)])

        now += 2
        reads.expireForTesting()
        check("the queued request expires without ever executing",
              answers.contains(.queueTimeout(seconds: 1)) && workers.count == 2)
        workers.removeFirst()()
        check("one completed worker returns its response", answers.contains(.response(.status(200))))

        now += 3
        reads.expireForTesting()
        check("an active read answers once at the whole-request deadline",
              answers.contains(.requestTimeout(seconds: 4)))
        let before = answers.count
        workers.removeFirst()()
        expect("late worker completion cannot answer the same request twice", answers.count, before)
        check("all actual workers finishing reclaims the lane", reads.metrics.current == 0,
              "current=\(reads.metrics.current) active=\(reads.metrics.active) workers=\(workers.count)")
    }
}
