import Foundation

/// Answers a reading immediately from the last good one while the next is being taken.
///
/// **What this replaces, and why the replacement is not "a cache".** `/info` and `/places` are
/// recomputed on every request: five back-to-back `/info` measured 0.51, 0.46, 0.41, 0.42 and
/// 0.43 s, five `/places` 0.121 through 0.108 — no memory of each other at all. Under the load
/// the app is actually used in, the same two routes measured 1.8–2.3 s and 0.69–1.18 s. That is
/// the same code four to ten times slower because iTerm2's main thread was busy, and it is the
/// shape of every reading here: they cost what the Mac is doing, not what was asked for.
///
/// A plain cache would answer the second request from the first. It would not touch the part that
/// actually reaches the phone, which is that **a reading nobody has finished becomes a refusal**:
/// a full lane is `429 busy`, a modal sheet in iTerm2 is `502 iterm_attention_required`, and an
/// inventory that could not complete is `work_state: "unknown"`. All three mean *I do not have a
/// current answer*, and all three are delivered as *I have no answer*. On a phone that is a blank
/// card and a red toast, and the reader cannot tell it from the Mac being gone.
///
/// So the rule here is not "answer faster". It is **never make the reader wait on a reading, and
/// never refuse one you could have answered**. The last good reading goes out at once with its
/// age attached, and the refresh happens behind it. What was a stall becomes a number the client
/// can draw — eight seconds old — and the only requests that wait are the ones with nothing to
/// serve at all.
///
/// **Age is published rather than hidden**, which is the half that makes this honest. A stale
/// reading served silently is a lie with a short shelf life; served with `age` on it, it is a
/// reading the client can dim, label, or decide to re-ask for. `serveFor` is the other half: past
/// it, no age label is enough and the request waits for the truth.
///
/// Transport-neutral on purpose, like ``TranscriptReadCoordinator``: it never sees a request, a
/// status code or a socket. The caller supplies the expensive read and a rule for telling a good
/// answer from a refusal, and gets back a value, its age, and why it is stale if it is.
///
/// **Called on the caller's owner queue and nowhere else.** Every mutation of `readings`,
/// `inFlight` and `waiters` happens there; the expensive read happens on whatever `execute` hands
/// it to, and comes back through `completeOnOwner` before any of that is touched.
final class FreshReadings<Value> {

    typealias Executor = (@escaping () -> Void) -> Void

    /// How old a reading may be before it is refreshed, and before it stops being servable.
    ///
    /// The two numbers answer different questions. `freshFor` is *when is it worth asking again*
    /// — set it by how fast the underlying thing actually moves. `serveFor` is *how wrong may the
    /// reader be* — set it by what a stale answer costs, which for a status card is "it says
    /// working and the session ended". Between them the reader gets an instant answer and the Mac
    /// gets a background read; past `serveFor` the request waits, because there is an age beyond
    /// which no label rescues a reading.
    struct Policy {
        let freshFor: TimeInterval
        let serveFor: TimeInterval

        init(freshFor: TimeInterval, serveFor: TimeInterval) {
            precondition(freshFor >= 0, "a reading cannot be fresh for negative time")
            precondition(serveFor >= freshFor,
                         "a reading that may not be served cannot be worth refreshing")
            self.freshFor = freshFor
            self.serveFor = serveFor
        }
    }

    /// Whether a completed read may become the reading everything else is served from.
    ///
    /// **The caller decides, and it must not decide by "did it return".** A `502` from a modal
    /// sheet and a `429` from a full lane are both values that arrive successfully and both mean
    /// the read did not happen. Stored as good readings they would replace a usable card with a
    /// refusal and then serve *that* for the whole of `serveFor` — the failure would outlive the
    /// dialog that caused it. So a refusal keeps its reason and leaves the previous reading in
    /// place, ageing.
    struct Verdict {
        let usable: Bool
        let reason: String?

        static var good: Verdict { Verdict(usable: true, reason: nil) }
        static func refused(_ reason: String) -> Verdict {
            Verdict(usable: false, reason: reason)
        }
    }

    /// Where the answer came from, for the client and for the trace.
    enum Provenance: String {
        /// Inside `freshFor`. Nothing was started.
        case fresh
        /// Past `freshFor`, still inside `serveFor`. A refresh is running behind this answer.
        case stale
        /// Nothing was servable; this request took the reading itself.
        case read
        /// Nothing was servable and another request was already taking the reading; this one
        /// waited for that single read rather than starting a second.
        case joined
    }

    struct Answer {
        let value: Value
        /// Seconds since the reading was taken. Zero for a value this request read itself.
        let age: TimeInterval
        /// Why the newest attempt did not replace this reading — a refusal code, when the value
        /// being served is older than the last thing that happened to it.
        let staleReason: String?
        let provenance: Provenance
    }

    private struct Reading {
        var value: Value
        var bornAt: TimeInterval
        var staleReason: String?
    }

    private var readings: [String: Reading] = [:]
    private var inFlight: Set<String> = []
    private var waiters: [String: [(Answer) -> Void]] = [:]

    init() {}

    /// Answer `key`, taking the reading only when there is nothing servable.
    ///
    /// **`admit` is the lane's existing backpressure, moved to the only place it can still do
    /// harm.** It used to be asked first, so a busy Mac refused requests it could have answered
    /// out of a reading it already had. Now it is asked only when a read is actually about to
    /// start, and `refusal` is reached only when there was nothing to serve *and* no room to go
    /// and get it. A stale answer never becomes a 429 because the lane happened to be full.
    ///
    /// `release` balances an admitted read and is called on the owner queue once it settles.
    /// `deliver` is always called exactly once, also on the owner queue.
    func read(_ key: String,
              policy: Policy,
              admit: () -> Bool,
              refusal: () -> Value,
              execute: @escaping Executor,
              compute: @escaping () -> Value,
              classify: @escaping (Value) -> Verdict,
              completeOnOwner: @escaping (@escaping () -> Void) -> Void,
              release: @escaping () -> Void,
              deliver: @escaping (Answer) -> Void) {
        let now = Self.clock()
        if let reading = readings[key] {
            let age = max(0, now - reading.bornAt)
            if age <= policy.serveFor {
                // Answer first, start the refresh second. The other order works and reads the
                // same, but it puts the dispatch of a background read — and whatever `execute`
                // does with it — between the reader and an answer that was already in hand.
                deliver(Answer(value: reading.value, age: age,
                               staleReason: reading.staleReason,
                               provenance: age <= policy.freshFor ? .fresh : .stale))
                // A refresh that cannot be admitted is simply not taken. The reader already has
                // an answer, and the next ask will try again — dropping it is what keeps a full
                // lane from turning into a queue of refreshes nobody is waiting for.
                if age > policy.freshFor {
                    revalidate(key, admit: admit, execute: execute, compute: compute,
                               classify: classify, completeOnOwner: completeOnOwner,
                               release: release)
                }
                return
            }
        }
        // Nothing servable. One read, however many requests are asking: a cold `/info` asked by
        // eight tabs at once is eight `lsof` runs, eight `git status` and eight Apple events for
        // one answer, and the eighth of those is the one that makes the other seven slow.
        waiters[key, default: []].append(deliver)
        guard revalidate(key, admit: admit, execute: execute, compute: compute,
                         classify: classify, completeOnOwner: completeOnOwner,
                         release: release) else {
            // Nothing to serve and no room to read. This is the one path that still refuses, and
            // it takes every waiter with it rather than leaving them parked on a read that was
            // never started.
            let refused = refusal()
            for hand in waiters.removeValue(forKey: key) ?? [] {
                hand(Answer(value: refused, age: 0, staleReason: nil, provenance: .read))
            }
            return
        }
    }

    /// Start a read unless one is already running for this key. Waiters, if any, are settled when
    /// it lands; a refresh behind a served answer has none and simply updates the reading.
    ///
    /// Returns false only when there is no read running *and* the lane refused to start one.
    @discardableResult
    private func revalidate(_ key: String,
                            admit: () -> Bool,
                            execute: @escaping Executor,
                            compute: @escaping () -> Value,
                            classify: @escaping (Value) -> Verdict,
                            completeOnOwner: @escaping (@escaping () -> Void) -> Void,
                            release: @escaping () -> Void) -> Bool {
        guard !inFlight.contains(key) else { return true }
        guard admit() else { return false }
        inFlight.insert(key)
        execute { [weak self] in
            let value = compute()
            completeOnOwner {
                release()
                self?.settle(key, value: value, verdict: classify(value))
            }
        }
        return true
    }

    /// Called on the owner queue with a completed read. Never `deinit`-sensitive: a settle that
    /// arrives after the owner is gone simply has nobody to hand it to.
    private func settle(_ key: String, value: Value, verdict: Verdict) {
        inFlight.remove(key)
        let at = Self.clock()
        if verdict.usable {
            readings[key] = Reading(value: value, bornAt: at, staleReason: nil)
        } else if readings[key] != nil {
            // **The refusal ages the reading; it does not replace it.** `bornAt` is left where it
            // was, so a Mac with a dialog on it walks steadily toward `serveFor` instead of
            // resetting its clock every twenty seconds and serving the same stale card forever.
            readings[key]?.staleReason = verdict.reason
        }
        guard let parked = waiters.removeValue(forKey: key), !parked.isEmpty else { return }
        // Whoever was waiting gets the value that was just read, even when it was a refusal:
        // there was nothing to serve, so the refusal is the honest answer for them.
        let answer: Answer
        if verdict.usable {
            answer = Answer(value: value, age: 0, staleReason: nil, provenance: .read)
        } else if let reading = readings[key], max(0, at - reading.bornAt) <= Self.servableOnFailure {
            answer = Answer(value: reading.value, age: max(0, at - reading.bornAt),
                            staleReason: verdict.reason, provenance: .stale)
        } else {
            answer = Answer(value: value, age: 0, staleReason: verdict.reason, provenance: .read)
        }
        // The first waiter is the one that asked for the read; the rest joined it. They get the
        // same bytes and a different provenance, so a trace can tell one expensive read shared by
        // eight requests from eight expensive reads.
        for (index, hand) in parked.enumerated() {
            hand(index == 0 ? answer
                            : Answer(value: answer.value, age: answer.age,
                                     staleReason: answer.staleReason, provenance: .joined))
        }
    }

    /// A reading old enough to have been refused service is still better than the refusal itself
    /// for a request that arrived while the refresh was running — but only just, so this is a
    /// bound rather than a licence. Beyond it the refusal goes out.
    private static var servableOnFailure: TimeInterval { ReadingClock.servableOnFailure }

    private static func clock() -> TimeInterval { ReadingClock.now() }

    // MARK: - What a test may ask

    var inFlightKeysForTesting: Set<String> { inFlight }
    var storedKeysForTesting: Set<String> { Set(readings.keys) }
    func waiterCountForTesting(_ key: String) -> Int { waiters[key]?.count ?? 0 }
    func forgetForTesting() {
        readings.removeAll(); inFlight.removeAll(); waiters.removeAll()
    }
}

/// The freshness policy for the optional reads — `/info`, `/live`, `/places`.
///
/// It lives here rather than in ``RemoteServer`` because that file is at its architecture
/// boundary, and because none of this is transport: it is a set of judgements about how fast the
/// things behind those routes actually move, which is exactly the kind of decision that should be
/// readable in one place instead of inline at a call site.
enum SlowReadings {

    typealias Readings = FreshReadings<RemoteServer.Response>

    /// **Set by how fast the thing behind the route moves, not by how often it is asked.**
    ///
    /// `/info` is a status card: a working tree, a token count, a permission mode. Two seconds is
    /// shorter than a person can read the card in, and its `serveFor` is where the cost of being
    /// wrong lands — a card that says *working* about a session that ended. Sixty seconds is long
    /// enough to ride out a sheet in iTerm2 and short enough that nobody plans around a minute-old
    /// reading, and the age goes out with it either way.
    ///
    /// `/places` is a list of directories and their start points. It changes when somebody makes a
    /// project, which is not something that happens between two taps, so it is allowed to be much
    /// older before anyone goes and looks again.
    static func policy(for path: String) -> Readings.Policy {
        path == "/v1/places"
            ? Readings.Policy(freshFor: 20, serveFor: 600)
            : Readings.Policy(freshFor: 2, serveFor: 60)
    }

    /// One key per distinct answer. The query is part of it because `?parts=summary` is a
    /// genuinely different, cheaper card, and serving one where the other was asked for would be
    /// the cache lying about which question it answered.
    static func key(for request: RemoteServer.Request) -> String {
        let query = request.query.keys.sorted()
            .map { "\($0)=\(request.query[$0] ?? "")" }
            .joined(separator: "&")
        return query.isEmpty ? request.path : "\(request.path)?\(query)"
    }

    /// Which answers may become the reading everything is served from.
    ///
    /// A `5xx` or a `429` arrives as a perfectly well-formed response and means the read did not
    /// happen — a sheet in iTerm2, a full lane, a subprocess that timed out. Storing one would
    /// replace a usable card with a refusal and then serve that refusal for the whole of
    /// `serveFor`. A `4xx` that is not `429` is about the request rather than the Mac, so it is
    /// stored: asking for a session that does not exist has a stable answer.
    static func classify(_ response: RemoteServer.Response) -> Readings.Verdict {
        guard response.status >= 500 || response.status == 429 else { return .good }
        return .refused(errorCode(in: response) ?? "read_failed")
    }

    private static func errorCode(in response: RemoteServer.Response) -> String? {
        guard let object = try? JSONSerialization.jsonObject(with: response.body),
              let error = (object as? [String: Any])?["error"] as? [String: Any]
        else { return nil }
        return error["code"] as? String
    }

    /// Nothing to serve and no room to read — the one answer this lane still refuses with. Its
    /// wording is the one the route gave before there was a reading to fall back on.
    static func busy(depth: Int) -> RemoteServer.Response {
        var response = RemoteServer.Response.error(
            429, "busy",
            "This Mac already has \(depth) of these to read. Try again in a moment.")
        response.headers["Cache-Control"] = "no-store"
        return response
    }

    /// Put the age on the wire and the timings in the trace.
    ///
    /// `Age` is the standard header for exactly this and needs no client change to be correct;
    /// `X-Clawdline-Reading` is the part a client can branch on — whether it is looking at a
    /// reading taken for it, one taken a moment ago, or one that could not be replaced and why.
    static func stamp(_ answer: Readings.Answer,
                      arrived: Date,
                      lane: String,
                      key: String,
                      trace: ReadingTrace) -> RemoteServer.Response {
        var response = answer.value
        response.headers["Age"] = String(Int(answer.age.rounded()))
        response.headers["X-Clawdline-Reading"] = answer.provenance.rawValue
        if let reason = answer.staleReason {
            response.headers["X-Clawdline-Stale-Reason"] = reason
        }
        let total = Date().timeIntervalSince(arrived)
        // A served reading did no work, so all of its elapsed time is the queue it stood in; a
        // read did the work, so none of it is. Splitting them at the door is the whole reason
        // this exists — the two numbers ask for opposite fixes.
        let served = answer.provenance == .fresh || answer.provenance == .stale
        trace.record(ReadingTrace.Span(
            at: arrived, lane: lane, key: key,
            queueWaitMs: served ? Int(total * 1000) : 0,
            executeMs: served ? 0 : Int(total * 1000),
            provenance: answer.provenance.rawValue,
            ageSeconds: Int(answer.age.rounded()),
            outcome: response.status,
            staleReason: answer.staleReason))
        return response
    }
}

/// The clock readings are stamped with, and the one bound they share.
///
/// It lives outside ``FreshReadings`` because Swift has no static storage in a generic type, and
/// beside it rather than inside the app's other clocks because what a test needs to do here is
/// **age a reading without sleeping** — a policy measured in seconds cannot be covered by a suite
/// that waits them out.
enum ReadingClock {
    /// Replaceable for tests; production never sets it.
    static var forTesting: (() -> TimeInterval)?

    static func now() -> TimeInterval {
        forTesting?() ?? ProcessInfo.processInfo.systemUptime
    }

    /// How stale a reading may be and still be handed to a request that was waiting on a refresh
    /// that then failed. Past this the refusal is the honest answer.
    static let servableOnFailure: TimeInterval = 120
}

/// Where the time actually went, per reading, in a fixed amount of memory.
///
/// **The app could not answer "was it slow, or was it queued" about itself.** `Clawdline.log` is
/// 19,695 lines with no duration in any of them, and `remote-audit.jsonl` carries `ms` on exactly
/// one event — `voice.transcribe` — out of 489 rows in a day. Everything else records that a
/// thing happened, at one-second resolution, which cannot distinguish a route that took two
/// seconds from a route that waited two seconds behind somebody else's.
///
/// Those are opposite bugs with opposite fixes: one wants the work made cheaper, the other wants
/// the lane widened or the work moved off it. Guessing which is which is how a queue-shaped
/// problem gets a timeout bolted onto it. So both numbers are kept, separately, at the one place
/// every bounded reading passes through.
///
/// A ring buffer rather than a file: this records at request rate, and ``Log/write`` opens,
/// seeks and closes its file on every single line. Diagnostics that cost more than the thing they
/// measure end up switched off, and a diagnostic that is off during the incident is not one.
final class ReadingTrace {

    struct Span {
        let at: Date
        /// Which bounded lane answered it — `reading`, `transcript`, `terminal`, `analytics`.
        let lane: String
        let key: String
        /// Between arriving and starting: contention, not cost.
        let queueWaitMs: Int
        /// Between starting and finishing: cost, not contention.
        let executeMs: Int
        /// `fresh`, `stale`, `read`, `joined` — or `direct` for a lane with no freshness policy.
        let provenance: String
        /// Seconds of age on the value that went out.
        let ageSeconds: Int
        let outcome: Int
        let staleReason: String?
    }

    private let lock = NSLock()
    private var spans: [Span] = []
    private var next = 0
    private let capacity: Int

    init(capacity: Int = 2048) {
        precondition(capacity > 0)
        self.capacity = capacity
        spans.reserveCapacity(capacity)
    }

    func record(_ span: Span) {
        lock.lock(); defer { lock.unlock() }
        if spans.count < capacity {
            spans.append(span)
        } else {
            spans[next] = span
            next = (next + 1) % capacity
        }
    }

    /// Newest first, which is the order somebody reading it during an incident wants.
    func recent(limit: Int = 200) -> [Span] {
        lock.lock(); defer { lock.unlock() }
        let ordered = spans.count < capacity
            ? spans
            : Array(spans[next...]) + Array(spans[..<next])
        return Array(ordered.suffix(limit).reversed())
    }

    /// The shape of a lane over the last `window` seconds: how many, how long they waited, how
    /// long they took. Percentiles rather than a mean, because the complaint being investigated
    /// is "sometimes it hangs" and a mean is exactly the statistic that hides that.
    struct Shape {
        let lane: String
        let count: Int
        let queueWaitP50: Int
        let queueWaitP99: Int
        let executeP50: Int
        let executeP99: Int
        let refusals: Int
        let servedStale: Int
    }

    func shape(window: TimeInterval = 300, now: Date = Date()) -> [Shape] {
        lock.lock()
        let all = spans
        lock.unlock()
        let cutoff = now.addingTimeInterval(-window)
        var byLane: [String: [Span]] = [:]
        for span in all where span.at >= cutoff {
            byLane[span.lane, default: []].append(span)
        }
        return byLane.keys.sorted().map { lane in
            let rows = byLane[lane] ?? []
            let waits = rows.map(\.queueWaitMs).sorted()
            let runs = rows.map(\.executeMs).sorted()
            return Shape(lane: lane,
                         count: rows.count,
                         queueWaitP50: Self.percentile(waits, 0.50),
                         queueWaitP99: Self.percentile(waits, 0.99),
                         executeP50: Self.percentile(runs, 0.50),
                         executeP99: Self.percentile(runs, 0.99),
                         refusals: rows.filter { $0.outcome >= 400 }.count,
                         servedStale: rows.filter { $0.provenance == "stale" }.count)
        }
    }

    /// Nearest-rank, and `0` for an empty lane rather than a crash: this is read by a diagnostics
    /// route that must answer during exactly the incident that leaves a lane empty.
    static func percentile(_ sorted: [Int], _ fraction: Double) -> Int {
        guard !sorted.isEmpty else { return 0 }
        let rank = Int((fraction * Double(sorted.count)).rounded(.up))
        return sorted[min(max(rank, 1), sorted.count) - 1]
    }
}
