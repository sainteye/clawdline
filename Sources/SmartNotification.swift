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

    /// Completion is useful and not urgent. Eight seconds leaves room for Claude Code startup and
    /// one low-effort Haiku turn; after it, the old notification is strictly better than silence.
    static let timeout: TimeInterval = 8
    private static let grace: TimeInterval = 1
    private static let settleDelay: TimeInterval = 0.35
    private static let maxPending = 4
    private static let queue = DispatchQueue(label: "dev.sainteye.clawdline.smart-notification",
                                             qos: .utility)
    private static let lock = NSLock()
    private static var pending = 0

    struct Delivery {
        let title: String
        let project: String
        let fallbackBody: String
        let url: String
        let tag: String
        let icon: String?
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

    /// Send exactly one notification. Smart output replaces the fallback before delivery; a
    /// missing executable, a full queue, malformed output and every subprocess failure all take
    /// the old path instead of turning an optional improvement into a dropped event.
    static func send(_ delivery: Delivery, source: @escaping () -> String?) {
        guard Config.shared.smartNotifications,
              Planner.executable(named: "claude") != nil else {
            fallback(delivery)
            return
        }
        lock.lock()
        guard pending < maxPending else {
            lock.unlock()
            Log.write("smart notification: queue full — using ordinary notification")
            fallback(delivery)
            return
        }
        pending += 1
        lock.unlock()

        queue.asyncAfter(deadline: .now() + settleDelay) {
            let made = source().flatMap(generate)
            let notificationBody = made.map {
                body(project: delivery.project, summary: $0)
            } ?? delivery.fallbackBody
            WebPush.send(title: delivery.title, body: notificationBody,
                         url: delivery.url, tag: delivery.tag, icon: delivery.icon)
            lock.lock()
            pending = max(0, pending - 1)
            lock.unlock()
        }
    }

    private static func fallback(_ delivery: Delivery) {
        WebPush.send(title: delivery.title, body: delivery.fallbackBody,
                     url: delivery.url, tag: delivery.tag, icon: delivery.icon)
    }

    /// One tool-free Haiku turn. The output schema is also checked on the way back because CLI
    /// failure modes include plain text, truncated JSON and a successful envelope with no value.
    private static func generate(_ source: String) -> String? {
        guard let executable = Planner.executable(named: "claude") else { return nil }
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
        guard let raw = run(executable: executable,
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
                            stdin: source) else { return nil }
        return summary(fromClaudeOutput: raw)
    }

    private static func run(executable: URL, arguments: [String], stdin: String) -> String? {
        let fm = FileManager.default
        let directory = fm.temporaryDirectory
            .appendingPathComponent("clawdline-smart-notification-\(UUID().uuidString)",
                                    isDirectory: true)
        guard (try? fm.createDirectory(at: directory, withIntermediateDirectories: true)) != nil
        else { return nil }
        defer { try? fm.removeItem(at: directory) }
        let output = directory.appendingPathComponent("output.json")
        guard fm.createFile(atPath: output.path, contents: nil),
              let sink = try? FileHandle(forWritingTo: output) else { return nil }
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
            return nil
        }
        if let data = stdin.data(using: .utf8) {
            try? input.fileHandleForWriting.write(contentsOf: data)
        }
        try? input.fileHandleForWriting.close()

        let killer = DispatchWorkItem {
            guard process.isRunning else { return }
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
        guard process.terminationStatus == 0,
              let raw = try? String(contentsOf: output, encoding: .utf8), !raw.isEmpty else {
            Log.write("smart notification: Claude failed (status \(process.terminationStatus))")
            return nil
        }
        return raw
    }

    private static func clean(_ value: String, limit: Int) -> String {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > limit else { return trimmed }
        return String(trimmed.prefix(limit - 1)) + "…"
    }
}
