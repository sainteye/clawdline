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

func runReadingFreshnessTests() {
    let clock = FrozenReadingClock()
    clock.install()
    defer { FrozenReadingClock.uninstall() }

    let inline: FreshReadings<FakeReading>.Executor = { work in work() }
    let onOwner: (@escaping () -> Void) -> Void = { work in work() }

    func classify(_ reading: FakeReading) -> FreshReadings<FakeReading>.Verdict {
        reading.refusal.map { .refused($0) } ?? .good
    }

    let quick = FreshReadings<FakeReading>.Policy(freshFor: 2, serveFor: 60)

    group("a warm reading is answered without asking the Mac again") {
        let readings = FreshReadings<FakeReading>()
        var reads = 0
        var answers: [FreshReadings<FakeReading>.Answer] = []
        func ask() {
            readings.read("info:A", policy: quick, execute: inline,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          classify: classify, completeOnOwner: onOwner,
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
            readings.read("info:A", policy: quick, execute: inline,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          classify: classify, completeOnOwner: onOwner,
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
            readings.read("info:A", policy: quick, execute: inline,
                          compute: {
                              refusing
                                  ? FakeReading(body: "502", refusal: "iterm_attention_required")
                                  : FakeReading(body: "good card", refusal: nil)
                          },
                          classify: classify, completeOnOwner: onOwner,
                          deliver: { answers.append($0) })
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
            readings.read("info:A", policy: quick, execute: parked,
                          compute: { reads += 1; return FakeReading(body: "one card", refusal: nil) },
                          classify: classify, completeOnOwner: onOwner,
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
            readings.read("info:A", policy: quick, execute: executor,
                          compute: { reads += 1; return FakeReading(body: "card \(reads)", refusal: nil) },
                          classify: classify, completeOnOwner: onOwner,
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
            readings.read("info:A", policy: quick, execute: executor,
                          compute: {
                              refusing ? FakeReading(body: "502", refusal: "busy")
                                       : FakeReading(body: "good card", refusal: nil)
                          },
                          classify: classify, completeOnOwner: onOwner,
                          deliver: { answers.append($0) })
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
        readings.read("info:A", policy: quick, execute: inline,
                      compute: { FakeReading(body: "502", refusal: "busy") },
                      classify: classify, completeOnOwner: onOwner,
                      deliver: { late.append($0) })
        check("but a reading nobody can stand behind any more gives way to the refusal",
              late.last?.value.body == "502")
    }

    group("keys do not read each other's readings") {
        let readings = FreshReadings<FakeReading>()
        var answers: [String: FreshReadings<FakeReading>.Answer] = [:]
        for key in ["info:A", "info:B"] {
            readings.read(key, policy: quick, execute: inline,
                          compute: { FakeReading(body: "card for \(key)", refusal: nil) },
                          classify: classify, completeOnOwner: onOwner,
                          deliver: { answers[key] = $0 })
        }
        check("each key keeps its own reading", answers["info:A"]?.value.body == "card for info:A")
        check("and does not answer with another's", answers["info:B"]?.value.body == "card for info:B")
        check("both are stored", readings.storedKeysForTesting == ["info:A", "info:B"])
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
}
