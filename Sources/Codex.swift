import Foundation

/// Reading a Codex session from the record it keeps on disk.
///
/// Codex calls these rollouts and writes one per session as it goes, at
/// `~/.codex/sessions/YYYY/MM/DD/rollout-<started>-<id>.jsonl` — one JSON object per line, the
/// same idea as Claude Code's transcripts and none of the same fields. So this is
/// ``Transcript``'s shape rather than its code: find the file that belongs to a session on
/// screen, read the tail of it, and turn what is recognised into ``Transcript/Entry`` so that
/// everything downstream — the pane, the phone, the search — keeps working on one kind of thing.
///
/// **The format is not documented and can change.** Every unexpected field is a reason to skip a
/// record and never a reason to fail, and a session whose file cannot be found falls back to the
/// terminal capture exactly as it did before any of this existed.
enum Codex {

    // MARK: - Where it keeps things

    /// Codex's home. `CODEX_HOME` when it is set, which is what Codex itself honours, and the
    /// config can name it outright for the case this app cannot see that variable — it is
    /// launched from Finder and inherits no login shell, which is the same reason
    /// ``Tmux/binary`` cannot simply look on `PATH`.
    static var home: URL {
        if let configured = Paths.resolve(Config.shared.codexHome) { return configured }
        if let env = ProcessInfo.processInfo.environment["CODEX_HOME"], !env.isEmpty {
            return URL(fileURLWithPath: Paths.expand(env))
        }
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".codex", isDirectory: true)
    }

    static var sessionsRoot: URL { home.appendingPathComponent("sessions", isDirectory: true) }
    static var archiveRoot: URL {
        home.appendingPathComponent("archived_sessions", isDirectory: true)
    }

    /// Every rollout under `sessions/`, newest day first.
    ///
    /// The tree is `YYYY/MM/DD`, and those names sort chronologically as text — which is the
    /// whole reason this walks the names rather than stat'ing the directories. A machine with a
    /// year of Codex on it has three hundred day folders in that tree, and reading one number
    /// off each of them to sort by would be three hundred round trips to answer a question the
    /// filenames already answer.
    ///
    /// `days` caps how far back it is willing to look. A session running right now wrote its
    /// first line today or, over a midnight, yesterday; the rest of the tree is history that only
    /// ``workedIn(limit:)`` cares about, and it asks for more.
    static func rollouts(days: Int = 3, root: URL? = nil) -> [URL] {
        let base = root ?? sessionsRoot
        let fm = FileManager.default
        var out: [URL] = []
        for day in dayFolders(under: base, limit: days) {
            let names = (try? fm.contentsOfDirectory(atPath: day.path)) ?? []
            out += names.filter { $0.hasPrefix("rollout-") && $0.hasSuffix(".jsonl") }
                .map { day.appendingPathComponent($0) }
        }
        return out
    }

    /// The newest `limit` day folders in a `YYYY/MM/DD` tree, newest first.
    static func dayFolders(under base: URL, limit: Int) -> [URL] {
        var out: [URL] = []
        for year in children(of: base).prefix(2) {
            for month in children(of: year).prefix(2) {
                out += children(of: month)
                if out.count >= limit { return Array(out.prefix(limit)) }
            }
            if out.count >= limit { return Array(out.prefix(limit)) }
        }
        return Array(out.prefix(limit))
    }

    /// A directory's subdirectories, newest name first. Names only — no stat, see ``rollouts``.
    private static func children(of url: URL) -> [URL] {
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: url.path) else { return [] }
        return names.filter { !$0.hasPrefix(".") }
            .sorted(by: >)
            .map { url.appendingPathComponent($0, isDirectory: true) }
    }

    // MARK: - The head of a rollout

    /// What the first line of a rollout says about itself: where the session is working, what
    /// Codex calls it, whether it is a conversation with a person, and which Codex made it.
    struct Head: Equatable {
        var cwd: String
        var id: String
        /// `thread_source` being `user`.
        ///
        /// **A Codex process writes more than one rollout.** Its own conversation is one; every
        /// subagent it sends off gets a file of its own, in the same directory, started within
        /// seconds, indistinguishable by anything else — and one of those is what the pane
        /// showed before this field existed. Codex says which is which in the first line, so
        /// this is read rather than guessed at.
        ///
        /// A rollout that does not say counts as one, because the field is newer than the
        /// format: absent means nobody wrote it down, not "this is a subagent".
        var isUser: Bool
        /// A terminal conversation rather than a Codex Desktop workspace.
        ///
        /// Both have `thread_source: user`, but Desktop creates working directories under
        /// `~/.codex/.chatgpt-projects` and `~/Documents/Codex` on its own. Those are records of
        /// the desktop app, not projects Clawdline should offer to start another terminal in.
        /// Current rollouts distinguish them with `source: cli` and `source: vscode`.
        var isInteractiveCLI: Bool
    }

    /// Read that cheaply, and remember it once it exists.
    ///
    /// Only the first few kilobytes, because `session_meta` is the first record and the part
    /// worth having is at the front of it — the rest is the system prompt, which is thousands of
    /// words nobody here is going to read. Remembered against the path alone rather than against
    /// a stamp: a rollout's first line is written when the session starts and never rewritten, so
    /// there is nothing for a stamp to catch. Absence is not stable: `lsof` can reveal a newly
    /// opened rollout before Codex has appended `session_meta`, so a nil result must be retried.
    private static let headLock = NSLock()
    private static var heads: [String: Head?] = [:]

    static func head(of url: URL) -> Head? {
        headLock.lock()
        if let hit = heads[url.path] { headLock.unlock(); return hit }
        headLock.unlock()

        let found = readHead(of: url)
        if let found {
            headLock.lock()
            heads[url.path] = found
            headLock.unlock()
        }
        return found
    }

    private static func readHead(of url: URL, bytes: Int = 16 << 10) -> Head? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        guard let data = try? handle.read(upToCount: bytes), !data.isEmpty else { return nil }
        return head(inText: String(decoding: data, as: UTF8.self))
    }

    /// Split out so a test can hand it a line instead of a file.
    ///
    /// The `cwd` is pulled textually by ``StartPoints/cwds(in:)`` — the same reader the project
    /// list uses, for the same reason: this is a short string near the front of a record that
    /// can be a megabyte long, and parsing the JSON to reach it would be the most expensive
    /// possible way to get it. The **first** one, which is `session_meta`'s; later records carry
    /// a `cwd` of their own and one of them is a `file://` URL rather than a path.
    static func head(inText text: String) -> Head? {
        guard let cwd = StartPoints.cwds(in: text).first, !cwd.isEmpty else { return nil }
        let threadSource = string(after: "\"thread_source\":", in: text)
        let source = string(after: "\"source\":", in: text)
        return Head(cwd: cwd, id: string(after: "\"session_id\":", in: text) ?? "",
                    isUser: threadSource == nil || threadSource == "user",
                    // Before `source` existed, every rollout Clawdline could see here came from
                    // the CLI. Preserve those old sessions rather than erasing their projects.
                    isInteractiveCLI: source == nil || source == "cli")
    }

    /// The string value after a key, read textually for the same reason the `cwd` is.
    private static func string(after needle: String, in text: String) -> String? {
        guard let hit = text.range(of: needle) else { return nil }
        var i = hit.upperBound
        while i < text.endIndex, text[i] == " " { i = text.index(after: i) }
        guard i < text.endIndex, text[i] == "\"" else { return nil }
        i = text.index(after: i)
        var out = ""
        while i < text.endIndex, text[i] != "\"", text[i] != "\n" {
            out.append(text[i])
            i = text.index(after: i)
        }
        return out.isEmpty ? nil : out
    }

    // MARK: - Matching a session on screen to its file

    /// The rollout a session is writing, or nothing.
    ///
    /// **The process is asked first, and it knows.** Codex holds its rollout open for as long as
    /// the session lasts, so `lsof` on its pid names the file outright — no clocks, no
    /// directories, no ambiguity between two sessions open in the same folder at the same
    /// minute. That case was not hypothetical: two tabs in this repository, started seconds
    /// apart, were both shown the same conversation before this existed.
    ///
    /// A pid usually has more than one rollout open, because a session's subagents each get one.
    /// ``Head/isUser`` is what separates them, and it is read out of the file rather than
    /// inferred from which was opened first.
    ///
    /// Everything below that is the fallback for when there is no pid to ask — narrow to the
    /// rollouts started in this working directory, then pick the one that started when this
    /// process did. **The directory is the strong half and the clock is the weak one.** A file
    /// whose creation time is nowhere near the process is not this session's, so it is dropped
    /// rather than offered, which is how a brand-new tab avoids showing somebody else's
    /// conversation with its own name on it.
    static func locate(cwd: String, startedAt: Date? = nil, pid: Int32? = nil,
                       days: Int = 3) -> URL? {
        // A pid is a stronger answer even while its rollout is still being created. Falling
        // through when `lsof` has not seen the new file yet lets the clock-based lookup borrow
        // another Codex session's rollout from the same directory. That is exactly the moment a
        // session started from the browser is first opened, so the reader briefly lands in an
        // older conversation despite the new row being the one selected. Empty until this
        // process names its own file is the only honest answer.
        if let pid { return heldRollout(ofPID: pid) }

        let mine = rollouts(days: days)
            .filter { head(of: $0)?.cwd == cwd }
            .map { (url: $0, at: Transcript.created($0)) }
        guard !mine.isEmpty else { return nil }
        guard let startedAt else {
            return mine.max { modified($0.url) < modified($1.url) }?.url
        }
        // The same slack ``Transcript/locate`` allows: the file is created as the session starts,
        // and the two clocks are read by different means a moment apart.
        let plausible = mine.filter {
            abs($0.at.timeIntervalSince(startedAt)) < 120 || $0.at > startedAt
        }
        return plausible.min {
            abs($0.at.timeIntervalSince(startedAt)) < abs($1.at.timeIntervalSince(startedAt))
        }?.url
    }

    /// The rollout that process is writing, out of the files it has open.
    ///
    /// Remembered for a few seconds against the pid: an `lsof` is the most expensive thing on
    /// this path and the pane asks once a beat, for an answer that is a fact about a session
    /// rather than about a moment. Held rather than kept, because a session can be told to start
    /// a new thread and the process then writes somewhere else.
    private static let heldLock = NSLock()
    private static var held: [Int32: (at: CFAbsoluteTime, url: URL?)] = [:]

    static func heldRollout(ofPID pid: Int32) -> URL? {
        heldLock.lock()
        if let hit = held[pid],
           CFAbsoluteTimeGetCurrent() - hit.at < (hit.url == nil ? 2 : 20) {
            defer { heldLock.unlock() }
            return hit.url
        }
        heldLock.unlock()

        let found = rollout(among: ITerm.openFiles(ofPID: pid))
        heldLock.lock()
        held[pid] = (CFAbsoluteTimeGetCurrent(), found)
        heldLock.unlock()
        return found
    }

    /// The one rollout in a list of open files that is a person's conversation.
    ///
    /// Split out from the `lsof` so a test can hand it a list. Two filters and they are both
    /// necessary: the path has to look like a rollout — a process has a hundred files open and
    /// most of them are SQLite — and the file has to say it is a user's thread rather than a
    /// subagent's.
    static func rollout(among paths: [String]) -> URL? {
        for path in paths where isRollout(path) {
            let url = URL(fileURLWithPath: path)
            if head(of: url)?.isUser == true { return url }
        }
        return nil
    }

    /// The conversation identity in one already-observed open-file list. Unlike the ordinary
    /// pane path, a whoami pass can ask for an uncached head: the same rollout path may be
    /// rewritten or replaced when a process resumes, and a permanent path-keyed head would make
    /// two resolution passes the same observation in disguise.
    static func identity(among paths: [String], fresh: Bool) -> (url: URL, id: String)? {
        for path in paths where isRollout(path) {
            let url = URL(fileURLWithPath: path)
            let found = fresh ? readHead(of: url) : head(of: url)
            guard let found, found.isUser, !found.id.isEmpty else { continue }
            return (url, found.id)
        }
        return nil
    }

    /// Whether a path is one of Codex's session files, by shape rather than by prefix — the home
    /// directory can be moved with `CODEX_HOME`, and `lsof` answers with the resolved path
    /// anyway, which on a Mac is `/private/…` where the setting said `/tmp/…`.
    static func isRollout(_ path: String) -> Bool {
        let name = (path as NSString).lastPathComponent
        return name.hasPrefix("rollout-") && name.hasSuffix(".jsonl")
            && path.contains("/sessions/")
    }

    private static func modified(_ url: URL) -> Date {
        (try? FileManager.default.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
            ?? .distantPast
    }

    // MARK: - Where Codex has been run

    /// Every directory Codex has a rollout for, newest first, deduped.
    ///
    /// This is the Codex half of ``StartPoints/recorded(root:folders:)`` — where a session can be
    /// started, because it is somewhere one has already run. Archived sessions count: archiving a
    /// conversation says nothing about whether you still work in that folder.
    static func workedIn(days: Int = 60, limit: Int = 40) -> [(path: String, at: Date)] {
        var files = rollouts(days: days)
        let fm = FileManager.default
        if let names = try? fm.contentsOfDirectory(atPath: archiveRoot.path) {
            files += names.filter { $0.hasSuffix(".jsonl") }
                .map { archiveRoot.appendingPathComponent($0) }
        }
        var best: [String: Date] = [:]
        for file in files {
            guard let head = head(of: file), head.isUser, head.isInteractiveCLI else { continue }
            let cwd = head.cwd
            let at = modified(file)
            if let had = best[cwd], had >= at { continue }
            best[cwd] = at
        }
        var out: [(path: String, at: Date)] = []
        for (path, at) in best { out.append((path: path, at: at)) }
        out.sort { $0.at == $1.at ? $0.path < $1.path : $0.at > $1.at }
        return Array(out.prefix(limit))
    }

    // MARK: - Parsing

    private static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    /// Turn a rollout into the same entries a Claude Code transcript yields.
    ///
    /// **Read from the end, and stop.** Rollouts reach tens of megabytes for the same reason
    /// transcripts do — every tool result is in there in full — and only the last few hundred
    /// entries are ever shown. See ``Transcript/forEachLineFromEnd(_:_:)``, which is the same
    /// walk over the same problem.
    ///
    /// Finished `item_completed` records are the ordinary conversation. One deliberately narrow
    /// `response_item` is also admitted: current Codex wraps `update_plan` in an exec tool call and
    /// does not repeat the checklist as a finished item. Its input is parsed as inert literals by
    /// ``literalPlan(in:)``; no JavaScript is evaluated and every other response item stays out.
    static func parse(_ jsonl: String, limit: Int = 400) -> [Transcript.Entry] {
        var newestFirst: [Transcript.Entry] = []
        Transcript.forEachLineFromEnd(jsonl) { line in
            for entry in entries(inRow: line).reversed() { newestFirst.append(entry) }
            return newestFirst.count < limit
        }
        return Array(newestFirst.reversed().suffix(limit))
    }

    /// The first human request in the early part of a rollout, for the optional session namer.
    ///
    /// Read from the front rather than by asking ``parse`` for a very large limit: the title is
    /// about how a conversation began, and a long conversation should not cost a full-file read
    /// merely because somebody turned naming on later. A partial last line is harmless — every
    /// unrecognised row is skipped — and a brand-new rollout is reconsidered at the next reading.
    static func firstUserMessage(of url: URL, bytes: Int = 2_000_000) -> String? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        guard let data = try? handle.read(upToCount: bytes), !data.isEmpty else { return nil }
        return firstUserMessage(in: String(decoding: data, as: UTF8.self))
    }

    static func firstUserMessage(in jsonl: String) -> String? {
        for line in jsonl.split(separator: "\n") {
            if let entry = entries(inRow: line).first(where: { $0.kind == .user }) {
                return entry.text
            }
        }
        return nil
    }

    private static func entries(inRow line: Substring) -> [Transcript.Entry] {
        guard let data = line.data(using: .utf8),
              let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let payload = row["payload"] as? [String: Any] else { return [] }

        let time = (row["timestamp"] as? String).flatMap { iso.date(from: $0) }
        if row["type"] as? String == "event_msg",
           payload["type"] as? String == "item_completed",
           let item = payload["item"] as? [String: Any] {
            return entries(ofItem: item, at: time)
        }
        guard row["type"] as? String == "response_item",
              payload["type"] as? String == "custom_tool_call",
              payload["name"] as? String == "exec",
              let input = payload["input"] as? String,
              let steps = literalPlan(in: input), !steps.isEmpty else { return [] }
        return [Transcript.Entry(kind: .tool, text: "Updated Plan", tool: "plan", time: time,
                                 plan: steps)]
    }

    /// One finished item as entries. Split out from the line so a test can describe an item
    /// rather than a whole envelope.
    static func entries(ofItem item: [String: Any], at time: Date?) -> [Transcript.Entry] {
        func entry(_ kind: Transcript.Entry.Kind, _ text: String,
                   tool: String? = nil) -> Transcript.Entry? {
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return nil }
            return Transcript.Entry(kind: kind, text: trimmed, tool: tool, time: time)
        }

        switch item["type"] as? String {
        case "UserMessage":
            if let raw = exactText(inContent: item["content"]) {
                if let message = Transcript.clawdlineSessionMessage(in: raw, at: time) {
                    return [message]
                }
                if let notice = ClawdlineMessage.decode(raw) {
                    return [Transcript.Entry(kind: .notice, text: notice.body, tool: nil,
                                             time: time, notice: notice)]
                }
            }
            let canonical = Transcript.canonicalImageContent(text(inContent: item["content"]))
            if canonical.imageCount > 0 {
                return [Transcript.Entry(kind: .user, text: canonical.text, tool: nil,
                                         time: time, imageCount: canonical.imageCount)]
            }
            return [entry(.user, canonical.text)].compactMap { $0 }

        case "AgentMessage":
            return [entry(.assistant, text(inContent: item["content"]))].compactMap { $0 }

        case "CommandExecution":
            if let actions = parsedActions(item["parsed_cmd"]), !actions.isEmpty {
                var explored = entry(.tool, exploredTitle(actions), tool: "shell")
                explored?.activity = Transcript.ToolActivity(
                    kind: "explored", status: normalizedStatus(item["status"] as? String),
                    durationMilliseconds: durationMilliseconds(item["duration"]),
                    actions: actions)
                return [explored].compactMap { $0 }
            }
            var out = [entry(.tool, command(item["command"]), tool: "shell")].compactMap { $0 }
            if let result = entry(.toolResult, outcome(of: item)) { out.append(result) }
            return out

        case "McpToolCall":
            let name = [item["server"] as? String, item["tool"] as? String]
                .compactMap { $0 }.joined(separator: ".")
            let title = mcpTitle(item["arguments"])
            if !title.isEmpty {
                var called = entry(.tool, title, tool: name.isEmpty ? "mcp" : name)
                let result = mcpResultDetail(item["result"])
                let isError = (item["result"] as? [String: Any])?["isError"] as? Bool == true
                called?.activity = Transcript.ToolActivity(
                    kind: "called", title: title,
                    status: isError ? "failed" : normalizedStatus(item["status"] as? String),
                    durationMilliseconds: durationMilliseconds(item["duration"]),
                    result: result.isEmpty ? nil : result)
                return [called].compactMap { $0 }
            }
            var out = [entry(.tool, arguments(item["arguments"]),
                             tool: name.isEmpty ? "mcp" : name)].compactMap { $0 }
            if let result = entry(.toolResult, mcpResult(item["result"])) { out.append(result) }
            return out

        case "FileChange":
            guard var edit = entry(.tool, changed(item["changes"]), tool: "edit") else {
                return []
            }
            edit.fileChanges = fileChanges(item["changes"])
            return [edit]

        // Codex's plugins arrive under one name with the kind next to it — `web.search` today.
        // The kind is the tool as far as a reader is concerned.
        case "Extension":
            let kind = item["kind"] as? String ?? "extension"
            let what = item["query"] as? String ?? kind
            return [entry(.tool, what, tool: kind)].compactMap { $0 }

        // Reasoning is the model thinking, and by the time it reaches a rollout the summary is
        // usually empty — Codex encrypts the content. Nothing to show beats a row that says so.
        default:
            return []
        }
    }

    // MARK: - Fields

    /// The text out of a content array. Codex spells the block type `text` on the way in and
    /// `Text` on the way out, so the type is not what is matched — the presence of text is.
    static func text(inContent content: Any?) -> String {
        if let string = content as? String { return string }
        guard let blocks = content as? [[String: Any]] else { return "" }
        return blocks.compactMap { $0["text"] as? String }
            .filter { !$0.isEmpty }
            .joined(separator: "\n")
    }

    /// A whole user item containing exactly one textual body. Structured Clawdline input is
    /// recognised only at this boundary; joining several blocks first would allow one valid
    /// fragment inside a larger, user-authored turn to impersonate the whole message.
    private static func exactText(inContent content: Any?) -> String? {
        if let string = content as? String { return string }
        guard let blocks = content as? [[String: Any]], blocks.count == 1,
              let text = blocks[0]["text"] as? String else { return nil }
        return text
    }

    /// A command as somebody would read it.
    ///
    /// Codex runs everything through a login shell, so the array is `["/bin/zsh", "-lc", "…"]`
    /// and the third element is the whole of what was asked for. Unwrapped rather than joined,
    /// because `/bin/zsh -lc` in front of every row is nine characters of nothing said over and
    /// over — and left alone when the shape is anything else, which is the honest answer for a
    /// command this does not recognise.
    static func command(_ raw: Any?) -> String {
        guard let parts = raw as? [String] else { return raw as? String ?? "" }
        if parts.count == 3, parts[1] == "-lc" || parts[1] == "-c",
           let shell = parts.first?.split(separator: "/").last,
           ["zsh", "bash", "sh", "fish"].contains(String(shell)) {
            return parts[2]
        }
        return parts.joined(separator: " ")
    }

    /// What a command did, in one line: its output, or the code it failed with.
    static func outcome(of item: [String: Any]) -> String {
        let output = (item["aggregated_output"] as? String)
            ?? (item["stdout"] as? String)
            ?? (item["stderr"] as? String) ?? ""
        let first = output.split(separator: "\n", omittingEmptySubsequences: true).first.map(String.init)
        if let first, !first.isEmpty { return first }
        guard let code = item["exit_code"] as? Int else { return "" }
        return code == 0 ? "" : "exit \(code)"
    }

    /// An MCP call's arguments as one line. The title when the call carries one — Codex's own
    /// plugins put a sentence there, and it is better than any summary this could write.
    static func arguments(_ raw: Any?) -> String {
        if let string = raw as? String { return firstLine(of: string) }
        guard let dict = raw as? [String: Any] else { return "" }
        for key in ["title", "query", "cmd", "command", "path", "code"] {
            if let value = dict[key] as? String, !value.isEmpty { return firstLine(of: value) }
        }
        return ""
    }

    private static func mcpTitle(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any], let title = dict["title"] as? String else {
            return ""
        }
        return firstLine(of: title)
    }

    static func mcpResult(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any] else { return "" }
        return firstLine(of: text(inContent: dict["content"]))
    }

    /// More than the old one-line compatibility summary, but still bounded: MCP results can
    /// contain entire pages or encoded media and the transcript endpoint must not mirror those.
    private static func mcpResultDetail(_ raw: Any?, limit: Int = 4_000) -> String {
        guard let dict = raw as? [String: Any] else { return "" }
        let value = text(inContent: dict["content"])
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard value.count > limit else { return value }
        return String(value.prefix(limit)) + "…"
    }

    private static func normalizedStatus(_ raw: String?) -> String? {
        switch raw {
        case "in_progress", "inProgress", "running": return "inProgress"
        case "pending": return "pending"
        case "completed", "success": return "completed"
        case "failed", "error": return "failed"
        default: return nil
        }
    }

    private static func durationMilliseconds(_ raw: Any?) -> Int? {
        guard let value = raw as? [String: Any] else { return nil }
        let seconds = (value["secs"] as? NSNumber)?.int64Value ?? 0
        let nanos = (value["nanos"] as? NSNumber)?.int64Value ?? 0
        guard seconds >= 0, nanos >= 0, nanos < 1_000_000_000 else { return nil }
        let total = seconds.multipliedReportingOverflow(by: 1_000)
        guard !total.overflow else { return nil }
        let milliseconds = total.partialValue.addingReportingOverflow(nanos / 1_000_000)
        guard !milliseconds.overflow, milliseconds.partialValue <= Int64(Int.max) else { return nil }
        return Int(milliseconds.partialValue)
    }

    /// Codex has already classified simple shell commands. Preserve only the two classes whose
    /// meaning is stable enough to present: a mixed or unknown command falls back as a shell run.
    private static func parsedActions(_ raw: Any?) -> [Transcript.ToolAction]? {
        guard let values = raw as? [[String: Any]], !values.isEmpty else { return nil }
        var actions: [Transcript.ToolAction] = []
        for value in values {
            guard let kind = value["type"] as? String, kind == "read" || kind == "search" else {
                return nil
            }
            let action = Transcript.ToolAction(
                kind: kind, command: value["cmd"] as? String,
                name: value["name"] as? String, path: value["path"] as? String,
                query: value["query"] as? String)
            let visible = [action.name, action.path, action.query, action.command]
                .compactMap { $0 }.contains { !$0.isEmpty }
            guard visible else { return nil }
            actions.append(action)
        }
        return actions
    }

    private static func exploredTitle(_ actions: [Transcript.ToolAction]) -> String {
        let labels = actions.prefix(3).map { action -> String in
            if action.kind == "search" { return "Search " + (action.query ?? action.path ?? "") }
            return "Read " + (action.name ?? action.path ?? "")
        }
        return labels.joined(separator: ", ") + (actions.count > 3 ? " +\(actions.count - 3)" : "")
    }

    /// Extract only a literal plan array from the generated exec wrapper. Both the direct form
    /// (`plan:[…]`) and Codex's current `const p=[…]; …plan:p` form are accepted. The small parser
    /// beneath this function accepts punctuation, identifiers and JSON strings only.
    private static func literalPlan(in source: String) -> [Transcript.PlanStep]? {
        guard source.utf8.count <= 100_000,
              let call = source.range(of: "tools.update_plan(") else { return nil }
        let suffix = String(source[call.upperBound...])
        let pattern = #"\bplan\s*:\s*(\[|[A-Za-z_$][A-Za-z0-9_$]*)"#
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: suffix,
                                           range: NSRange(suffix.startIndex..., in: suffix)),
              let valueRange = Range(match.range(at: 1), in: suffix) else { return nil }
        let token = String(suffix[valueRange])
        let array: String?
        if token == "[" {
            array = bracketedArray(in: suffix, from: valueRange.lowerBound)
        } else {
            let prefix = String(source[..<call.lowerBound])
            let assignment = #"(?:const|let|var)\s+"#
                + NSRegularExpression.escapedPattern(for: token) + #"\s*=\s*\["#
            guard let assignmentRegex = try? NSRegularExpression(pattern: assignment),
                  let found = assignmentRegex.matches(
                    in: prefix, range: NSRange(prefix.startIndex..., in: prefix)).last,
                  let foundRange = Range(found.range, in: prefix),
                  let open = prefix[..<foundRange.upperBound].lastIndex(of: "[") else { return nil }
            array = bracketedArray(in: prefix, from: open)
        }
        guard let array else { return nil }
        var parser = LiteralPlanParser(array)
        return parser.parse()
    }

    /// Find the matching bracket without treating brackets inside a JSON string as structure.
    private static func bracketedArray(in text: String, from open: String.Index) -> String? {
        var index = open, depth = 0, quoted = false, escaped = false
        while index < text.endIndex {
            let character = text[index]
            if quoted {
                if escaped { escaped = false }
                else if character == "\\" { escaped = true }
                else if character == "\"" { quoted = false }
            } else if character == "\"" { quoted = true }
            else if character == "[" { depth += 1 }
            else if character == "]" {
                depth -= 1
                if depth == 0 { return String(text[open...index]) }
                if depth < 0 { return nil }
            }
            index = text.index(after: index)
        }
        return nil
    }

    private struct LiteralPlanParser {
        let bytes: [UInt8]
        var index = 0

        init(_ source: String) { bytes = Array(source.utf8) }

        mutating func parse() -> [Transcript.PlanStep]? {
            guard take(0x5B) else { return nil } // [
            var out: [Transcript.PlanStep] = []
            skipSpace()
            if take(0x5D) { return out }
            while out.count < 100 {
                guard let step = object() else { return nil }
                out.append(step)
                skipSpace()
                if take(0x5D) { return index == bytes.count ? out : nil }
                guard take(0x2C) else { return nil } // ,
            }
            return nil
        }

        mutating func object() -> Transcript.PlanStep? {
            guard take(0x7B) else { return nil } // {
            var step: String?, status: String?
            while true {
                guard let key = identifier(), take(0x3A), let value = string() else { return nil }
                if key == "step", step == nil { step = value }
                else if key == "status", status == nil { status = value }
                else { return nil }
                skipSpace()
                if take(0x7D) { break }
                guard take(0x2C) else { return nil }
            }
            guard let step = step?.trimmingCharacters(in: .whitespacesAndNewlines), !step.isEmpty,
                  let status = normalizedStatus(status),
                  ["pending", "inProgress", "completed"].contains(status) else { return nil }
            return Transcript.PlanStep(step: step, status: status)
        }

        mutating func identifier() -> String? {
            skipSpace()
            let start = index
            while index < bytes.count {
                let byte = bytes[index]
                guard (byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122)
                        || (index > start && byte >= 48 && byte <= 57) || byte == 95 else { break }
                index += 1
            }
            guard index > start else { return nil }
            return String(decoding: bytes[start..<index], as: UTF8.self)
        }

        mutating func string() -> String? {
            skipSpace()
            guard index < bytes.count, bytes[index] == 0x22 else { return nil }
            let start = index
            index += 1
            var escaped = false
            while index < bytes.count {
                let byte = bytes[index]
                index += 1
                if escaped { escaped = false; continue }
                if byte == 0x5C { escaped = true; continue }
                if byte == 0x22 {
                    let data = Data(bytes[start..<index])
                    return try? JSONSerialization.jsonObject(with: data,
                        options: [.fragmentsAllowed]) as? String
                }
            }
            return nil
        }

        mutating func take(_ byte: UInt8) -> Bool {
            skipSpace()
            guard index < bytes.count, bytes[index] == byte else { return false }
            index += 1
            return true
        }

        mutating func skipSpace() {
            while index < bytes.count, [9, 10, 13, 32].contains(bytes[index]) { index += 1 }
        }
    }

    /// The files a change touched, with how many there were when it is more than a couple.
    static func changed(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any], !dict.isEmpty else { return "" }
        let names = dict.keys.sorted().map { ($0 as NSString).lastPathComponent }
        if names.count <= 3 { return names.joined(separator: ", ") }
        return names.prefix(3).joined(separator: ", ") + " +\(names.count - 3)"
    }

    /// The lossless half of the same `FileChange`: updates carry a unified diff, while additions
    /// and deletions carry the file content. Paths are sorted to keep the transcript wire stable
    /// across dictionary implementations and re-reads of the same rollout.
    private static func fileChanges(_ raw: Any?) -> [Transcript.FileChange] {
        guard let changes = raw as? [String: Any] else { return [] }
        return changes.keys.sorted().compactMap { path in
            guard let change = changes[path] as? [String: Any] else { return nil }
            let kind = (change["type"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "change"
            return Transcript.FileChange(
                path: path,
                kind: kind,
                unifiedDiff: change["unified_diff"] as? String,
                content: change["content"] as? String,
                movePath: change["move_path"] as? String)
        }
    }

    private static func firstLine(of text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: true).first.map(String.init) ?? ""
    }
}
