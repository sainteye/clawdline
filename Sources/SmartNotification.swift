import Foundation

/// One short account of what a finished turn did, for the lock screen.
///
/// The model never reads a repository and has no tools. Its only source is the bounded end of the
/// conversation the user was already having, or the summaries dispatched tasks wrote when they
/// finished. Keeping that boundary here makes the input, fallback and output independently
/// testable without starting Claude Code or sending a push.
enum SmartNotification {
    struct Context: Equatable {
        let request: String
        let outcome: String
    }

    struct TaskLine: Equatable {
        let title: String
        let state: String
        let summary: String?
    }

    /// Enough to describe a turn without making a second model read the turn's working set.
    /// Character caps, rather than token estimates, keep the boundary independent of whichever
    /// tokenizer the `haiku` alias points at next.
    static let requestLimit = 2_000
    static let outcomeLimit = 4_000
    static let taskSummaryLimit = 1_000
    static let outputLimit = 180

    /// Completion is useful and not urgent — but the deadline still has to clear the work.
    /// Measured on this Mac on 2026-08-28 with several sessions live: five cold runs of the exact
    /// invocation below took 8.7–12.9 s and every one succeeded, so the old 8 s deadline killed
    /// every attempt that reached the model (8 of 8, status 143). Thirty seconds is about twice
    /// the slowest measured run, room for a compile hogging the machine; past it, the ordinary
    /// notification is strictly better than more waiting.
    static let timeout: TimeInterval = 30
    private static let grace: TimeInterval = 1
    /// How long a finished turn's transcript gets to flush its final row before it is read.
    private static let settleDelay: TimeInterval = 0.35
    /// A burst is taken whole once its newest member has settled, but a steady stream of finishes
    /// must not postpone the batch forever — after this long the oldest has waited enough.
    private static let settleCap: TimeInterval = 1.0
    /// Far above the worst burst observed (44 finishes inside 120 ms on 2026-08-28), affordable
    /// because the queue no longer costs one model turn per entry: everything waiting when the
    /// worker wakes becomes one coalesced turn and one push.
    static let maxQueued = 64
    private static let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.smart-notification",
                                             qos: .utility)
    private static let lock = NSLock()

    private struct Job {
        let delivery: Delivery
        let source: () -> String?
        let enqueued: Date
    }

    private static var jobs: [Job] = []
    private static var draining = false
    private static var currentHealth = Health()

    struct Delivery {
        let title: String
        let project: String
        let fallbackBody: String
        let url: String
        let tag: String
        let icon: String?
    }

    /// Why an attempt went out with the ordinary wording instead of a model sentence. These are
    /// the words the Settings row shows, so each names the actual event — a deadline is not a
    /// generic failure, because "the model is timing out" is the difference between raising the
    /// deadline and filing a bug.
    enum FallbackReason: Error, Equatable {
        case queueFull
        case timedOut
        case modelFailed
        case noSource
        case missing
    }

    /// A rolling, in-memory account of the feature: enough for the Settings row to say whether it
    /// has ever worked since launch, nothing that needs to survive a restart. It exists because
    /// this feature once failed 784 times in three hours and the only evidence was a log line.
    struct Health: Equatable {
        private(set) var attempts = 0
        private(set) var successes = 0
        private(set) var lastFailure: FallbackReason?
        private(set) var lastFailureAt: Date?
        private(set) var lastSuccessAt: Date?

        mutating func attempt(_ count: Int = 1) { attempts += count }
        mutating func success(_ count: Int = 1, at date: Date = Date()) {
            successes += count
            lastSuccessAt = date
        }
        mutating func failure(_ reason: FallbackReason, at date: Date = Date()) {
            lastFailure = reason
            lastFailureAt = date
        }

        /// Whether the most recent resolved attempt worked; nil before anything has resolved.
        var lastResolvedWasSuccess: Bool? {
            switch (lastSuccessAt, lastFailureAt) {
            case (nil, nil): return nil
            case (_?, nil): return true
            case (nil, _?): return false
            case (let success?, let failure?): return success >= failure
            }
        }
    }

    static func healthSnapshot() -> Health {
        lock.lock()
        defer { lock.unlock() }
        return currentHealth
    }

    /// The Settings row: counts first, then the last failure in words and when — so somebody who
    /// turned the switch on in the morning can see by lunch whether it has ever produced a
    /// sentence, without opening a log.
    static func healthLine(_ health: Health, copy: Copy) -> String {
        guard health.attempts > 0 else { return copy.settingsSmartHealthIdle }
        var line = copy.settingsSmartHealth(attempts: health.attempts,
                                            successes: health.successes)
        if let reason = health.lastFailure, let at = health.lastFailureAt {
            line += "\n" + copy.settingsSmartHealthFailure(reason: word(for: reason, copy: copy),
                                                           time: failureClock.string(from: at))
        }
        return line
    }

    static func word(for reason: FallbackReason, copy: Copy) -> String {
        switch reason {
        case .queueFull: return copy.settingsSmartQueueFull
        case .timedOut: return copy.settingsSmartTimeout(seconds: Int(timeout))
        case .modelFailed: return copy.settingsSmartModelFailed
        case .noSource: return copy.settingsSmartNoSource
        case .missing: return copy.settingsSmartMissing
        }
    }

    private static let failureClock: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm"
        return formatter
    }()

    private static func note(successes: Int = 0, failure: FallbackReason? = nil,
                             attempts: Int = 0) {
        lock.lock()
        if attempts > 0 { currentHealth.attempt(attempts) }
        if successes > 0 { currentHealth.success(successes) }
        if let failure { currentHealth.failure(failure) }
        lock.unlock()
    }

    static func context(from entries: [Transcript.Entry]) -> Context? {
        // Start with the newest request, not the newest answer. If that request has no answer yet,
        // falling backwards would produce a perfectly fluent notification about the previous
        // turn — worse than the generic fallback because every word of it would be real and old.
        guard let requestIndex = entries.lastIndex(where: { $0.kind == .user }),
              let answerIndex = entries[requestIndex...]
                .lastIndex(where: { $0.kind == .assistant }) else { return nil }
        let request = clean(entries[requestIndex].text, limit: requestLimit)
        guard !request.isEmpty else { return nil }
        let outcome = clean(entries[answerIndex].text, limit: outcomeLimit)
        guard !outcome.isEmpty else { return nil }
        return Context(request: request, outcome: outcome)
    }

    static func source(for tasks: [TaskLine]) -> String? {
        let rows: [[String: Any]] = tasks.prefix(20).map { task in
            var row: [String: Any] = [
                "title": clean(task.title, limit: 200),
                "state": clean(task.state, limit: 40),
            ]
            if let summary = task.summary {
                let value = clean(summary, limit: taskSummaryLimit)
                if !value.isEmpty { row["summary"] = value }
            }
            return row
        }
        guard !rows.isEmpty,
              let data = try? JSONSerialization.data(withJSONObject: ["tasks": rows],
                                                     options: [.sortedKeys, .withoutEscapingSlashes])
        else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func summary(fromClaudeOutput raw: String) -> String? {
        guard let object = Planner.object(inClaudeOutput: raw),
              let value = object["summary"] as? String else { return nil }
        let words = value.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
        guard !words.isEmpty else { return nil }
        if words.count <= outputLimit { return words }
        return String(words.prefix(outputLimit - 1)) + "…"
    }

    static func body(project: String, summary: String) -> String {
        "\(project) · \(summary)"
    }

    /// A bounded conversation object, encoded rather than interpolated into instructions so the
    /// model sees authored prose as data even when that prose happens to look like a prompt.
    static func source(for context: Context) -> String? {
        let object = ["request": context.request, "outcome": context.outcome]
        guard let data = try? JSONSerialization.data(withJSONObject: object,
                                                     options: [.sortedKeys, .withoutEscapingSlashes])
        else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// Read only the bounded transcript tail after the Stop event has had a fraction of a second
    /// to flush its final assistant row. Claude and Codex already meet at `Transcript.Entry`, so
    /// the summarizer has no provider-specific branch.
    static func source(from record: (url: URL, assistant: Assistant)) -> String? {
        guard let tail = Transcript.tail(of: record.url, bytes: 256_000) else { return nil }
        let entries = Transcript.parse(tail, assistant: record.assistant, limit: 40)
        return context(from: entries).flatMap(source(for:))
    }

    /// Send at least one notification per finish. Smart output replaces the fallback before
    /// delivery; a missing executable, a full queue, malformed output and every subprocess
    /// failure all take the old path instead of turning an optional improvement into a dropped
    /// event — and every one of those detours is counted where ``healthSnapshot()`` can show it.
    static func send(_ delivery: Delivery, source: @escaping () -> String?) {
        guard Config.shared.smartNotifications else {
            fallback(delivery)
            return
        }
        guard Planner.executable(named: "claude") != nil else {
            note(failure: .missing, attempts: 1)
            Log.write("smart notification: no claude executable — using ordinary notification")
            fallback(delivery)
            return
        }
        lock.lock()
        guard jobs.count < maxQueued else {
            currentHealth.attempt()
            currentHealth.failure(.queueFull)
            lock.unlock()
            Log.write("smart notification: queue full — using ordinary notification")
            fallback(delivery)
            return
        }
        jobs.append(Job(delivery: delivery, source: source, enqueued: Date()))
        currentHealth.attempt()
        let start = !draining
        draining = true
        lock.unlock()
        if start { queue.async { drain() } }
    }

    /// When a queue whose oldest job arrived at `oldest` and newest at `newest` may be taken.
    static func batchReadyAt(oldest: Date, newest: Date) -> Date {
        min(newest.addingTimeInterval(settleDelay), oldest.addingTimeInterval(settleCap))
    }

    /// The single worker. It wakes once the batch has settled, takes everything waiting, and only
    /// then looks again — so a burst costs one model turn and one push however wide it is, and at
    /// most one `claude` process exists at a time on a machine that is already busy.
    private static func drain() {
        lock.lock()
        guard let oldest = jobs.first, let newest = jobs.last else {
            draining = false
            lock.unlock()
            return
        }
        let wait = batchReadyAt(oldest: oldest.enqueued,
                                newest: newest.enqueued).timeIntervalSinceNow
        guard wait <= 0.01 else {
            lock.unlock()
            queue.asyncAfter(deadline: .now() + wait) { drain() }
            return
        }
        let batch = jobs
        jobs.removeAll()
        lock.unlock()
        deliver(batch)
        queue.async { drain() }
    }

    private static func deliver(_ batch: [Job]) {
        guard let first = batch.first else { return }
        if batch.count == 1 {
            switch first.source().map(generate) ?? .failure(.noSource) {
            case .success(let summary):
                WebPush.send(title: first.delivery.title,
                             body: body(project: first.delivery.project, summary: summary),
                             url: first.delivery.url, tag: first.delivery.tag,
                             icon: first.delivery.icon)
                note(successes: 1)
            case .failure(let reason):
                note(failure: reason)
                fallback(first.delivery)
            }
            return
        }
        // Eleven finishes are one event to the person holding the phone, so a burst becomes one
        // model turn and one push. The fallback is coalesced too: eleven copies of the ordinary
        // wording is the burst problem, not a solution to it, and the count in the title keeps
        // every member represented even when the body is cut at the payload ceiling.
        let title = L.t.pushCoalesced(count: batch.count)
        let icons = Set(batch.map { $0.delivery.icon })
        let icon = icons.count == 1 ? first.delivery.icon : nil
        let parts = batch.map { (project: $0.delivery.project, source: $0.source()) }
        switch coalescedSource(parts).map(generate) ?? .failure(.noSource) {
        case .success(let summary):
            WebPush.send(title: title, body: summary, url: "/", tag: coalescedTag, icon: icon)
            note(successes: batch.count)
        case .failure(let reason):
            note(failure: reason)
            WebPush.send(title: title,
                         body: coalescedBody(batch.map { $0.delivery.fallbackBody }),
                         url: "/", tag: coalescedTag, icon: icon)
        }
    }

    private static let coalescedTag = "smart-coalesced"

    /// Several finishes, one bounded object. Each member's own source — a conversation or a task
    /// list — nests under the project it belongs to, so one sentence can tell two projects apart.
    static func coalescedSource(_ parts: [(project: String, source: String?)]) -> String? {
        let rows: [[String: Any]] = parts.prefix(20).map { part in
            var row: [String: Any] = ["project": part.project]
            if let source = part.source,
               let data = source.data(using: .utf8),
               let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                row["detail"] = object
            }
            return row
        }
        guard !rows.isEmpty,
              let data = try? JSONSerialization.data(withJSONObject: ["finished": rows],
                                                     options: [.sortedKeys, .withoutEscapingSlashes])
        else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// The ordinary wording for a burst the model could not describe: every member, head first —
    /// the payload ceiling in WebPush keeps whatever fits.
    static func coalescedBody(_ bodies: [String]) -> String {
        bodies.joined(separator: " / ")
    }

    private static func fallback(_ delivery: Delivery) {
        WebPush.send(title: delivery.title, body: delivery.fallbackBody,
                     url: delivery.url, tag: delivery.tag, icon: delivery.icon)
    }

    /// One tool-free Haiku turn. The output schema is also checked on the way back because CLI
    /// failure modes include plain text, truncated JSON and a successful envelope with no value.
    private static func generate(_ source: String) -> Result<String, FallbackReason> {
        guard let executable = Planner.executable(named: "claude") else { return .failure(.missing) }
        let language = L.tag(of: L.t)
        let system = """
        Write one concise push-notification sentence in language \(language) describing what the \
        finished work claims it delivered, changed, found, or failed to do. Use only the supplied \
        data. Do not infer that code is correct, reviewed, merged, or deployed. Preserve a failure \
        when one is present. No Markdown, paths, preamble, or more than one sentence. Keep it under \
        \(outputLimit) characters.
        """
        let schema = """
        {"type":"object","properties":{"summary":{"type":"string","maxLength":\(outputLimit)}},\
        "required":["summary"],"additionalProperties":false}
        """
        let outcome = run(executable: executable,
                          arguments: ["-p", "--model", "haiku", "--effort", "low",
                                      "--system-prompt", system,
                                      "--output-format", "json",
                                      "--json-schema", schema,
                                      "--max-turns", "1",
                                      "--tools", "",
                                      "--permission-mode", "dontAsk",
                                      "--strict-mcp-config",
                                      "--mcp-config", "{\"mcpServers\":{}}",
                                      "--disable-slash-commands"],
                          stdin: source)
        if let failure = outcome.failure { return .failure(failure) }
        guard let raw = outcome.output, let made = summary(fromClaudeOutput: raw) else {
            return .failure(.modelFailed)
        }
        return .success(made)
    }

    /// The working directory every one of these runs shares — see ``Scratch`` for why it must not
    /// be a fresh one per call. This feature now succeeds rather than timing out, so a per-call
    /// directory was producing a permanent `~/.claude/projects` folder several times an hour.
    static var scratchDirectory: URL { Scratch.directory(for: "smart-notification") }

    private static func run(executable: URL, arguments: [String],
                            stdin: String) -> (output: String?, failure: FallbackReason?) {
        let fm = FileManager.default
        let directory = scratchDirectory
        guard let output = Scratch.prepare(directory, output: "output", extension: "json")
        else { return (nil, .modelFailed) }
        defer { try? fm.removeItem(at: output) }
        guard fm.createFile(atPath: output.path, contents: nil),
              let sink = try? FileHandle(forWritingTo: output) else { return (nil, .modelFailed) }
        defer { try? sink.close() }

        let process = Process()
        process.executableURL = executable
        process.arguments = arguments
        process.currentDirectoryURL = directory
        process.environment = ProcessInfo.processInfo.environment
        let input = Pipe()
        process.standardInput = input
        process.standardOutput = sink
        process.standardError = FileHandle.nullDevice
        do { try process.run() } catch {
            Log.write("smart notification: Claude did not start — \(error.localizedDescription)")
            return (nil, .modelFailed)
        }
        if let data = stdin.data(using: .utf8) {
            try? input.fileHandleForWriting.write(contentsOf: data)
        }
        try? input.fileHandleForWriting.close()

        var deadlineFired = false
        let killer = DispatchWorkItem {
            guard process.isRunning else { return }
            lock.lock()
            deadlineFired = true
            lock.unlock()
            let pid = process.processIdentifier
            process.terminate()
            DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + grace) {
                if process.isRunning { kill(pid, SIGKILL) }
            }
        }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout,
                                                       execute: killer)
        process.waitQuietly()
        killer.cancel()
        if process.terminationStatus == 0,
           let raw = try? String(contentsOf: output, encoding: .utf8), !raw.isEmpty {
            return (raw, nil)
        }
        lock.lock()
        let timedOut = deadlineFired
        lock.unlock()
        if timedOut {
            Log.write("smart notification: Claude hit the \(Int(timeout)) s deadline"
                      + " — using ordinary notification")
            return (nil, .timedOut)
        }
        Log.write("smart notification: Claude failed (status \(process.terminationStatus))")
        return (nil, .modelFailed)
    }

    private static func clean(_ value: String, limit: Int) -> String {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > limit else { return trimmed }
        return String(trimmed.prefix(limit - 1)) + "…"
    }
}
