import Foundation

/// The agents a session sent off to work in the background, which the terminal never shows.
///
/// Everything else this app knows about a session it learned by looking at a screen. That works
/// because a screen is what Claude Code draws on — and it stops working the moment the work moves
/// somewhere it does not draw. A session that has three agents out searching a codebase paints
/// exactly the same spinner as one thinking about a single sentence: one line, one clock, no hint
/// that the real answer to "what is it doing" is three conversations happening elsewhere.
///
/// Those conversations are on disk. Claude Code gives every subagent its own transcript beside the
/// session's own, and writes a small sidecar next to it at the moment it is spawned:
///
/// ```
/// ~/.claude/projects/<project>/<session>/subagents/
///     agent-<id>.meta.json   ← agentType, description, model, parentAgentId, spawnDepth
///     agent-<id>.jsonl       ← appended to while it works
/// ```
///
/// **There is no record that says "started" and none that says "still going".** The sidecar
/// appears at the spawn and never changes; the only other thing written is a `<task-notification>`
/// in the *parent's* transcript once it stops, carrying a status, what it returned, and what it
/// cost. So "running" is not read anywhere — it is the absence of an ending, and everything below
/// is about establishing that absence cheaply enough to ask once a second.
///
/// **This format is not documented and can change.** Same rule as ``Transcript``: anything
/// unrecognised is skipped, never guessed at, and a session with nothing readable simply has no
/// agents rather than an error.
enum Subagents {

    /// One background agent, as much as the files say about it.
    struct Agent: Equatable {
        enum State: String {
            case running
            case done
            case failed
        }

        /// Claude Code's own id for it, which is also the name of both its files.
        let id: String
        /// The one-line description whoever spawned it wrote — the most useful string here by a
        /// distance, because it is the only one written by somebody explaining their intent.
        let description: String
        /// `general-purpose`, or whichever named agent was asked for.
        let type: String
        let model: String?
        /// 1 for an agent the session spawned, 2 for one an agent spawned, and so on.
        let depth: Int

        var state: State
        /// When its transcript was last appended to. For a finished agent this is when it
        /// finished; for a running one it is how long it has been quiet.
        var at: Date
        /// The tool it last reached for — "Bash: swift build", "Read: Sources/Panel.swift".
        /// The closest thing to a live line that exists for something with no screen.
        var doing: String?
        /// What it handed back, once it has. First line only; the full thing is in the transcript.
        var result: String?
        var tokens: Int?
        var tools: Int?
        var seconds: Double?

        var isRunning: Bool { state == .running }
    }

    // MARK: - The reading

    /// How long after its last write an agent is still worth asking about.
    ///
    /// Not a guess at how long agents run — it is how long a *silent* one stays a candidate. An
    /// agent waiting on a slow build writes nothing for minutes, and dropping it would report it
    /// as gone at exactly the moment somebody wanted to know it was still out there. Half an hour
    /// is longer than anything that is genuinely still running has been quiet for, and the cost of
    /// the window being generous is a `stat` per file.
    private static let liveWindow: TimeInterval = 30 * 60

    /// How long a *finished* agent stays on screen after it lands.
    ///
    /// Long enough to read what it came back with, short enough that a session does not
    /// accumulate a wall of things that already happened. What it returned is in the transcript
    /// either way; this is the notice, not the record.
    private static let settleWindow: TimeInterval = 3 * 60

    /// At most this many per session, newest first. A workflow can spawn dozens; a strip above a
    /// transcript can show a few, and the honest thing to do with the rest is say how many.
    static let shown = 6

    /// Every session's background agents, keyed by session id. Sessions with none are absent.
    ///
    /// Claude Code only, and `isClaude` rather than `isAssistant` says so on purpose. This whole
    /// file reads `~/.claude/projects/…/subagents`, a directory Claude Code writes and nothing
    /// else does — a Codex session has no such thing, so asking would be a stat that can only
    /// come back empty, once per session per beat.
    static func reading(of sessions: [TargetSession]) -> [String: [Agent]] {
        var out: [String: [Agent]] = [:]
        for session in sessions where session.isClaude {
            let found = agents(of: session)
            if !found.isEmpty { out[session.id] = found }
        }
        return out
    }

    /// **The fast path is the one with no agents in it, and it is nearly all of them.**
    ///
    /// A session that has never spawned one has no `subagents` directory, so this costs a cached
    /// transcript lookup and a single `stat` that fails. Only a session that has actually sent
    /// work out ever reads a file.
    static func agents(of session: TargetSession) -> [Agent] {
        guard let transcript = transcript(of: session) else { return [] }
        let folder = transcript.deletingPathExtension().appendingPathComponent("subagents",
                                                                               isDirectory: true)
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: folder.path) else { return [] }

        let now = Date()
        // Everything the directory claims exists, narrowed to what could still matter, before a
        // single byte of any transcript is read.
        var candidates: [(id: String, meta: URL, jsonl: URL, at: Date)] = []
        for name in names where name.hasPrefix("agent-") && name.hasSuffix(".meta.json") {
            let id = String(name.dropFirst("agent-".count).dropLast(".meta.json".count))
            guard !id.isEmpty else { continue }
            let jsonl = folder.appendingPathComponent("agent-\(id).jsonl")
            // Its own transcript rather than the sidecar: the sidecar is written once, at the
            // spawn, and never touched again — so its date says when this started and nothing
            // about whether it is still going.
            guard let at = modified(jsonl), now.timeIntervalSince(at) < liveWindow else { continue }
            candidates.append((id, folder.appendingPathComponent(name), jsonl, at))
        }
        guard !candidates.isEmpty else { return [] }

        // Only now is a transcript opened, and only the part of it nobody has read yet.
        var verdicts = notices(in: transcript)

        var agents: [Agent] = []
        for candidate in candidates {
            guard let meta = meta(at: candidate.meta) else { continue }

            // A nested agent reports to whoever spawned it, and that is another agent's
            // transcript rather than the session's. Asked for only when the session's own
            // transcript did not settle it, so an ordinary depth-1 agent never opens one.
            var verdict = verdicts[candidate.id]
            if verdict == nil, let parent = meta.parent {
                let file = folder.appendingPathComponent("agent-\(parent).jsonl")
                let inParent = notices(in: file)
                verdict = inParent[candidate.id]
                verdicts.merge(inParent) { existing, _ in existing }
            }

            let state: Agent.State
            switch verdict?.status {
            case "completed": state = .done
            case nil:         state = .running
            default:          state = .failed   // "failed", and anything new that is not success
            }
            // A finished agent is news for a few minutes and clutter after that.
            if state != .running, now.timeIntervalSince(candidate.at) > settleWindow { continue }

            agents.append(Agent(id: candidate.id,
                                description: meta.description,
                                type: meta.type,
                                model: meta.model,
                                depth: meta.depth,
                                state: state,
                                at: candidate.at,
                                doing: state == .running ? doing(in: candidate.jsonl) : nil,
                                result: verdict?.result,
                                tokens: verdict?.tokens,
                                tools: verdict?.tools,
                                seconds: verdict?.seconds))
        }

        // Running first — they are the ones with a future — then whatever moved most recently.
        agents.sort { a, b in
            if a.isRunning != b.isRunning { return a.isRunning }
            return a.at > b.at
        }
        return Array(agents.prefix(shown))
    }

    // MARK: - Where a session's transcript is

    /// Remembered per session, because working it out costs a directory listing and up to twelve
    /// title reads, and the answer is a fact about a process that was started in a directory and
    /// has not moved since. Thirty seconds so that a session resumed into a new file is picked up
    /// without anybody having to restart anything.
    private static let lock = NSLock()
    private static var files: [String: (at: CFAbsoluteTime, url: URL?)] = [:]

    private static func transcript(of session: TargetSession) -> URL? {
        lock.lock()
        if let hit = files[session.id], CFAbsoluteTimeGetCurrent() - hit.at < 30 {
            defer { lock.unlock() }
            return hit.url
        }
        lock.unlock()

        let url = Targets.workingDirectory(of: session).flatMap {
            Transcript.locate(cwd: $0, tabTitle: session.name,
                              startedAt: Targets.processStart(of: session),
                              sessionID: HookBridge.note(for: session)?.session)
        }
        lock.lock()
        files[session.id] = (CFAbsoluteTimeGetCurrent(), url)
        lock.unlock()
        return url
    }

    // MARK: - The sidecar

    private struct Meta {
        var description = ""
        var type = "agent"
        var model: String?
        var parent: String?
        var depth = 1
    }

    /// Read once and kept forever: this file is written at the spawn and never rewritten, so a
    /// second read could only ever return the same bytes. Bounded by the directory it comes
    /// from, which a person's own machine keeps a few hundred of.
    private static var metas: [String: Meta] = [:]

    private static func meta(at url: URL) -> Meta? {
        lock.lock()
        if let hit = metas[url.path] { lock.unlock(); return hit }
        lock.unlock()

        guard let data = try? Data(contentsOf: url),
              let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return nil }
        var meta = Meta()
        meta.description = (row["description"] as? String) ?? ""
        meta.type = (row["agentType"] as? String) ?? "agent"
        meta.model = row["model"] as? String
        meta.parent = row["parentAgentId"] as? String
        meta.depth = (row["spawnDepth"] as? Int) ?? 1

        lock.lock()
        metas[url.path] = meta
        lock.unlock()
        return meta
    }

    // MARK: - The ending, if there is one

    struct Verdict: Equatable {
        var status: String
        var result: String?
        var tokens: Int?
        var tools: Int?
        var seconds: Double?
    }

    /// Every `<task-notification>` a transcript has recorded, by the agent it is about.
    ///
    /// **Read forward from where we stopped last time, not from the end.** A transcript is
    /// append-only and runs to tens of megabytes; tailing a fixed window would be cheaper per
    /// call and wrong in exactly the case that matters — a busy session can push an agent's
    /// ending out of any window while the agent's own file is still recent, and a missing ending
    /// reads as *still running*, forever. So the whole file is read once and the new bytes after
    /// that, which on a live session is a few kilobytes a second.
    ///
    /// A file that has shrunk was replaced rather than appended to, and the offset is dropped.
    private static var notes: [String: (offset: UInt64, found: [String: Verdict])] = [:]

    static func notices(in url: URL) -> [String: Verdict] {
        lock.lock()
        let seen = notes[url.path]
        lock.unlock()

        guard let handle = try? FileHandle(forReadingFrom: url) else { return seen?.found ?? [:] }
        defer { try? handle.close() }
        let size = (try? handle.seekToEnd()) ?? 0

        var offset = seen?.offset ?? 0
        var found = seen?.found ?? [:]
        if size < offset { offset = 0; found = [:] }
        guard size > offset else { return found }

        try? handle.seek(toOffset: offset)
        guard let data = try? handle.readToEnd(), !data.isEmpty else { return found }
        let text = String(decoding: data, as: UTF8.self)

        // Stop at the last complete line. The writer may be mid-append, and half a JSON object
        // parsed now would be skipped now and never looked at again.
        guard let end = text.lastIndex(of: "\n") else { return found }
        let whole = text[text.startIndex...end]

        for line in whole.split(separator: "\n") {
            // The cheap test first, by a long way: nearly every line in a transcript is an
            // ordinary message, and this decides without parsing any of them.
            guard line.contains("task-notification") else { continue }
            guard let data = line.data(using: .utf8),
                  let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { continue }
            // Two shapes carry the same text — the queued command and the attachment recording
            // it — and either will do. Keyed by agent, so seeing both is not seeing two endings.
            let blob = (row["content"] as? String)
                ?? ((row["attachment"] as? [String: Any])?["prompt"] as? String)
            guard let blob, let id = tag("task-id", in: blob),
                  let status = tag("status", in: blob) else { continue }
            found[id] = Verdict(status: status,
                                result: tag("result", in: blob).map(firstLine),
                                tokens: tag("subagent_tokens", in: blob).flatMap { Int($0) },
                                tools: tag("tool_uses", in: blob).flatMap { Int($0) },
                                seconds: tag("duration_ms", in: blob).flatMap { Double($0) }
                                    .map { $0 / 1000 })
        }

        let advanced = offset + UInt64(whole.utf8.count)
        lock.lock()
        notes[url.path] = (advanced, found)
        lock.unlock()
        return found
    }

    /// `<name>…</name>`, first one only. Not a parser: this is a fixed set of tags written by one
    /// program, and anything that does not match simply is not there.
    private static func tag(_ name: String, in text: String) -> String? {
        guard let open = text.range(of: "<\(name)>"),
              let close = text.range(of: "</\(name)>", range: open.upperBound..<text.endIndex)
        else { return nil }
        let inner = text[open.upperBound..<close.lowerBound]
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return inner.isEmpty ? nil : inner
    }

    // MARK: - What it is doing right now

    /// The tool an agent last reached for, from the tail of its own transcript.
    ///
    /// Sixty-four kilobytes, which is a handful of turns — far more than the one line wanted, and
    /// small enough that asking every second about several agents is not a thing anybody would
    /// notice. Remembered against the file's signature, so an agent that is between turns is not
    /// re-read on every beat.
    private static var doings: [String: (signature: String, line: String?)] = [:]

    static func doing(in url: URL) -> String? {
        let signature = Transcript.signature(of: url)
        lock.lock()
        if let hit = doings[url.path], hit.signature == signature {
            defer { lock.unlock() }
            return hit.line
        }
        lock.unlock()

        var line: String?
        if let text = Transcript.tail(of: url, bytes: 64 << 10) {
            for row in text.split(separator: "\n") {
                guard row.contains("tool_use") else { continue }
                guard let data = row.data(using: .utf8),
                      let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      obj["type"] as? String == "assistant",
                      let message = obj["message"] as? [String: Any],
                      let blocks = message["content"] as? [[String: Any]] else { continue }
                for block in blocks where block["type"] as? String == "tool_use" {
                    let name = block["name"] as? String ?? "tool"
                    let what = Transcript.summarise(input: block["input"])
                    line = what.isEmpty ? name : "\(name): \(what)"
                }
            }
        }

        lock.lock()
        doings[url.path] = (signature, line)
        lock.unlock()
        return line
    }

    // MARK: - Odds and ends

    private static func modified(_ url: URL) -> Date? {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        return attrs?[.modificationDate] as? Date
    }

    private static func firstLine(_ text: String) -> String {
        let line = text.split(separator: "\n").first.map(String.init) ?? text
        return line.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Everything remembered, let go of. Called when the panel goes down, alongside the other
    /// caches that only exist to make an open panel cheap.
    static func forget() {
        lock.lock()
        files = [:]
        doings = [:]
        lock.unlock()
    }
}
