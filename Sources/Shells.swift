import Foundation

/// The commands a session left running in the background, which its screen stops saying out loud.
///
/// This is the other half of ``Subagents``, and it exists for the same reason: work moved
/// somewhere Claude Code does not draw. A background shell is worse than an agent on that count,
/// because the terminal *does* mention it — once. "Cooked for 1h 25m 13s · 1 shell still running"
/// is printed where the turn ended, above an ordinary prompt, and it scrolls away the moment
/// anything else is said. Everywhere this app looks after that says the same thing an ordinary
/// finished session says: nothing running, nothing asked, idle. **A session with a build still
/// going reads as a session that is done**, which is the one wrong answer a fleet list must not
/// give, and it is the whole reason this file exists.
///
/// The facts are on disk. Every `Bash` call gets an output file of its own, under a directory
/// named for the session:
///
/// ```
/// /tmp/claude-<uid>/<project>/<session>/tasks/
///     <task-id>.output   ← appended to while the command runs
/// ```
///
/// **The file alone does not settle it, and the first version of this file believed it did.** A
/// command that finished in the background has `[exited with code 0]` written under its last line
/// and keeps its file; a foreground one has its file *deleted* when it returns. That reads like a
/// complete rule and is not: a foreground command that was **interrupted** — Esc at the keyboard,
/// a session that went away mid-command — leaves its file behind with no marker under it, looking
/// exactly like a build that is still going. Half an hour after this was first written it was
/// reporting a `curl` somebody had cancelled an hour earlier as running work.
///
/// So two facts are required, and the second one is the transcript's. Claude Code answers a
/// backgrounded `Bash` with *"Command running in background with ID: bvlp3xmku"* and answers a
/// foreground one with its output, so the transcript says which ids were ever backgrounded at
/// all — see ``announced(in:)``. A file is a running command only when its id was announced
/// **and** nothing has written an ending under it.
///
/// Nothing here is promised by anybody and all of it can change. Same rule as everywhere else:
/// recognise a shape, and a session whose files say nothing recognisable has no shells rather
/// than an error.
enum Shells {

    /// One command still running where nobody can see it.
    struct Shell: Equatable {
        /// Claude Code's own id for it — `bf7e0p5v7` — which is also the name of its file and
        /// the word `/bashes` uses for it on the terminal.
        let id: String
        /// When it last printed something. For a command that prints as it goes this is a live
        /// clock; for `sleep 600` it is when it started, and that is the honest answer.
        let at: Date
        /// The last line it printed. The closest thing to a live line something with no screen
        /// has, and the same slot ``Subagents/Agent/doing`` fills for an agent.
        let doing: String?
        /// The command line that started it. Empty only when the two records it is joined from
        /// straddled a read — see ``announced(in:)``.
        let command: String
        /// The description written beside it, when there was one.
        let what: String?
    }

    /// How long after its last write a command is still worth reporting.
    ///
    /// Not a guess at how long a build takes — it is how long a *silent* one stays believable. A
    /// command that prints nothing for twelve hours is either finished in a way that left no
    /// marker (Claude Code killed mid-run, the machine slept through it) or it is the rarest
    /// thing on this list, and a chip that never goes away is worse than a chip that is late.
    private static let liveWindow: TimeInterval = 12 * 60 * 60

    /// At most this many per session, newest first. `/bashes` will list thirty; a strip above a
    /// transcript can show a few, and the honest thing to do with the rest is say how many.
    static let shown = 6

    /// The size of the tail read from each output file. Big enough for the last line of anything
    /// that prints normally, small enough that a session with six of them is not doing I/O
    /// anybody would notice once a second.
    private static let tailBytes = 8 << 10

    /// Every session's running background commands, keyed by session id. Sessions with none are
    /// absent.
    ///
    /// Claude Code only, and `isClaude` rather than `isAssistant` says so on purpose: Codex keeps
    /// no such directory, so asking would be a `stat` that can only come back empty, once per
    /// session per beat.
    static func reading(of sessions: [TargetSession]) -> [String: [Shell]] {
        var out: [String: [Shell]] = [:]
        for session in sessions where session.isClaude {
            let found = shells(of: session)
            if !found.isEmpty { out[session.id] = found }
        }
        return out
    }

    /// **The fast path is the one with nothing running, and it is nearly all of them.**
    ///
    /// A session between turns has had its foreground output files deleted behind it and has no
    /// background ones, so its `tasks` directory is empty and this costs one `readdir`. Only a
    /// session with a file in there and no ending under it opens a transcript.
    static func shells(of session: TargetSession) -> [Shell] {
        guard let folder = folder(of: session) else { return [] }
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: folder.path) else { return [] }

        let now = Date()
        var unfinished: [Shell] = []
        for name in names where name.hasSuffix(".output") {
            let id = String(name.dropLast(".output".count))
            guard !id.isEmpty else { continue }
            let file = folder.appendingPathComponent(name)
            guard let at = modified(file), now.timeIntervalSince(at) < liveWindow else { continue }
            let tail = read(file, signature: "\(at.timeIntervalSince1970)-\(size(file))")
            guard !tail.ended else { continue }
            unfinished.append(Shell(id: id, at: at, doing: tail.last, command: "", what: nil))
        }
        guard !unfinished.isEmpty else { return [] }

        // Only now is a transcript opened, and only for what the files cannot say: of these
        // unfinished commands, which were ever backgrounded rather than interrupted — and, for
        // the ones that were, what somebody actually asked for.
        guard let transcript = Subagents.transcript(of: session) else { return [] }
        let asked = announced(in: transcript)

        // Newest first: the one that printed a second ago is the one somebody is waiting on.
        return Array(unfinished.compactMap { shell -> Shell? in
            guard let was = asked[shell.id] else { return nil }
            return Shell(id: shell.id, at: shell.at, doing: shell.doing,
                         command: was.command, what: was.what)
        }.sorted { $0.at > $1.at }.prefix(shown))
    }

    // MARK: - Reading one of them

    /// What one command has printed, as the tail of its own output file.
    ///
    /// **The whole of what there is to show.** A background command has no conversation and no
    /// transcript — it has a file it is appending to, which is what the terminal's own `/bashes`
    /// reads and what `Read` on the path in the tool result reads. So this is that file, from the
    /// end, and `ended` says whether anything more is coming.
    ///
    /// **The id is checked rather than trusted**, exactly as ``Subagents/transcript(of:agent:)``
    /// checks one: it arrives from a URL and is about to become a path component. An id Claude
    /// Code wrote is letters and digits; anything with a dot or a slash in it is somebody asking
    /// for a file somewhere else on this disk, and the answer to that is nothing at all.
    static func output(of session: TargetSession, id: String,
                       bytes: Int) -> (text: String, ended: Bool, at: Date, signature: String)? {
        guard isID(id), let folder = folder(of: session) else { return nil }
        let file = folder.appendingPathComponent("\(id).output")
        guard FileManager.default.fileExists(atPath: file.path), let at = modified(file) else {
            return nil
        }
        let signature = Transcript.signature(of: file)
        let text = Transcript.tail(of: file, bytes: bytes) ?? ""
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        return (text, lines.last.map(isEnding) ?? false, at, signature)
    }

    /// Whether a string could be one of Claude Code's task ids. Deliberately narrower than "does
    /// not escape the directory": a name that cannot be an id is not worth a `stat`.
    static func isID(_ id: String) -> Bool {
        !id.isEmpty && id.count <= 128 && id.allSatisfy {
            $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_")
        }
    }

    // MARK: - Stopping one

    /// What happened when somebody asked for a command to stop.
    enum Stop: Equatable {
        /// A signal went to it.
        case stopped
        /// No such command, or it had already finished.
        case gone
        /// **It could not be proved which process this is, so nothing was signalled.** The one
        /// answer this must be able to give: everything below identifies a process before it
        /// signals anything, and a kill sent on a guess is a kill sent at whatever else on the
        /// machine happens to hold that number.
        case unidentified
    }

    /// Stop one of a session's background commands.
    ///
    /// **Three facts have to line up before a signal goes anywhere**, and the shape of this is
    /// the whole of its safety:
    ///
    /// 1. The id is one this session announced as a background command — the same test that
    ///    decides whether it is listed at all, so nothing that is not on screen can be stopped.
    /// 2. Something is holding its output file open. A finished command holds nothing, so this
    ///    is also the check that it is still running.
    /// 3. **That holder is a child of this session's Claude Code.** This is the ownership proof.
    ///    Claude Code runs a background command as `zsh -c …` under itself, so the process whose
    ///    parent is this session's pid is the command and nothing else is.
    ///
    /// The signal goes to its **process group**, not to the one process. A one-liner is a shell
    /// and whatever it is currently running; signalling only the shell leaves the `sleep` or the
    /// compiler in the middle of it with nobody waiting on it. The group is the shell's own —
    /// checked against Claude Code's, because signalling *that* group would take the session down
    /// with the command.
    ///
    /// `SIGTERM` first, and `SIGKILL` five seconds later only if the file is still held. A
    /// background command is somebody's build, and a build that cleans up after itself should be
    /// allowed to; one that ignores the ask should not get to stay.
    ///
    /// **Nothing here has to be told to Claude Code.** It is waiting on that process, so it sees
    /// the exit, writes `[killed]` under the output and posts its own notification — which is
    /// exactly what a command stopped from the Mac already looks like, and what ``read(_:signature:)``
    /// already recognises as an ending.
    static func stop(_ id: String, of session: TargetSession) -> Stop {
        guard isID(id), let folder = folder(of: session) else { return .gone }
        let file = folder.appendingPathComponent("\(id).output")
        guard FileManager.default.fileExists(atPath: file.path),
              let transcript = Subagents.transcript(of: session),
              announced(in: transcript)[id] != nil else { return .gone }

        guard let claude = Targets.pid(of: session),
              let lineage = ITerm.lineage(ofPID: claude) else { return .unidentified }
        let holders = ITerm.holders(ofPath: file.path)
        guard !holders.isEmpty else { return .gone }
        guard let group = group(among: holders, under: claude, avoiding: lineage.group) else {
            return .unidentified
        }

        kill(-group, SIGTERM)
        // Only if it is still there. Re-checked rather than assumed, because the ordinary case is
        // that this second signal is never sent and the first one was enough.
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 5) {
            guard ITerm.holders(ofPath: file.path).contains(where: { holder in
                ITerm.lineage(ofPID: holder)?.group == group
            }) else { return }
            kill(-group, SIGKILL)
        }
        return .stopped
    }

    /// Which process group among a file's holders belongs to this session's Claude Code.
    ///
    /// Split out from the `ps` and the `lsof` so a test can hand it a list, the way
    /// ``Codex/rollout(among:)`` is. Every rule that could get somebody's work killed is in here
    /// and in nothing that needs a machine to be in a particular state to exercise.
    ///
    /// `lineage` is a parameter rather than a call so this stays a decision: a caller with a list
    /// of processes and their parents gets an answer, and nothing here can go and find another
    /// one it likes better.
    static func group(among holders: [Int32], under claude: Int32, avoiding own: Int32,
                      lineage: (Int32) -> (parent: Int32, group: Int32)?
                          = { ITerm.lineage(ofPID: $0) }) -> Int32? {
        for holder in holders {
            guard let line = lineage(holder), line.parent == claude else { continue }
            // A group of 0, 1, or Claude Code's own is not a command — it is everything, the
            // init process, or the session itself. None of the three is a thing to signal.
            guard line.group > 1, line.group != own, line.group != claude else { return nil }
            return line.group
        }
        return nil
    }

    // MARK: - Which of them were ever backgrounded, and what each one was asked to do

    /// What a session asked for when it started one.
    struct Asked: Equatable {
        /// The command line itself, which is what `/bashes` shows and the only thing here that
        /// somebody can match against what they remember asking for.
        var command: String
        /// The one-line description written beside it, when there was one. Prose rather than
        /// shell, and the same field an agent's row calls `what`.
        var what: String?
    }

    /// Every background command this transcript has announced, by id.
    ///
    /// **Read forward from where we stopped last time, not from the end** — the same rule, and the
    /// same reason, as ``Subagents/notices(in:)``. A transcript is append-only and runs to tens of
    /// megabytes; tailing a fixed window would be cheaper per call and wrong in exactly the case
    /// that matters, because the command still running after ninety minutes is the one whose
    /// announcement is furthest back. A file that has shrunk was replaced rather than appended to,
    /// and the offset is dropped.
    ///
    /// **Two records, joined by the tool call between them.** The assistant's record carries the
    /// command and is marked `run_in_background`; the reply to it carries the id Claude Code
    /// minted, and points back with `tool_use_id`. The assistant's comes first in an append-only
    /// file, so one forward pass does it.
    ///
    /// **Almost nothing is parsed.** Every line is tested with two substrings before anything is
    /// decoded, and both are rare — a transcript of a thousand records had fifteen backgrounded
    /// commands in it. The alternative is decoding forty megabytes of JSON to read one line in a
    /// hundred. A message quoting either sentence at Claude adds an id that names no file, which
    /// changes nothing.
    private static let announcement = "Command running in background with ID: "
    private static let backgrounded = "run_in_background"
    private static var starts: [String: (offset: UInt64, found: [String: Asked])] = [:]

    static func announced(in url: URL) -> [String: Asked] {
        lock.lock()
        let seen = starts[url.path]
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
        // Stop at the last complete line. The writer may be mid-append, and half a record read
        // now would be skipped now and never looked at again.
        guard let end = text.lastIndex(of: "\n") else { return found }
        let whole = text[text.startIndex...end]

        // Only within this pass. A command whose two records straddle the boundary between two
        // reads loses its command line and keeps its id, which is the right way round to fail:
        // the id is what decides whether anything is running at all.
        var asked: [String: Asked] = [:]

        for line in whole.split(separator: "\n") {
            if line.contains(backgrounded), let row = record(line) {
                for block in blocks(of: row) where block["type"] as? String == "tool_use" {
                    guard let id = block["id"] as? String,
                          let input = block["input"] as? [String: Any],
                          input[backgrounded] as? Bool == true,
                          let command = input["command"] as? String else { continue }
                    asked[id] = Asked(command: clipped(oneLine(command)),
                                      what: (input["description"] as? String)
                                          .map { clipped(oneLine($0)) })
                }
            }
            guard line.contains(announcement), let row = record(line) else { continue }
            for block in blocks(of: row) where block["type"] as? String == "tool_result" {
                let text = (block["content"] as? String) ?? ""
                guard let hit = text.range(of: announcement) else { continue }
                let id = text[hit.upperBound...].prefix { $0.isASCII && ($0.isLetter || $0.isNumber) }
                guard !id.isEmpty else { continue }
                let call = block["tool_use_id"] as? String
                found[String(id)] = call.flatMap { asked[$0] } ?? Asked(command: "", what: nil)
            }
        }

        let advanced = offset + UInt64(whole.utf8.count)
        lock.lock()
        starts[url.path] = (advanced, found)
        lock.unlock()
        return found
    }

    private static func record(_ line: Substring) -> [String: Any]? {
        guard let data = line.data(using: .utf8) else { return nil }
        return try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    }

    private static func blocks(of row: [String: Any]) -> [[String: Any]] {
        ((row["message"] as? [String: Any])?["content"] as? [[String: Any]]) ?? []
    }

    /// A command as one line. Somebody writes a `for` loop or a heredoc, and this goes into a row
    /// one line tall — so the newlines become the spaces they read as anyway.
    private static func oneLine(_ text: String) -> String {
        text.split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
    }

    // MARK: - Where the files are

    /// Where Claude Code keeps a session's command output, whether or not anything is in there.
    ///
    /// Derived from the transcript rather than guessed at, because the two paths are built from
    /// the same two names: `~/.claude/projects/<project>/<session>.jsonl` beside
    /// `/tmp/claude-<uid>/<project>/<session>/tasks`. Finding the transcript is
    /// ``Subagents/transcript(of:)``'s job and it is already cached per session, so this adds no
    /// directory listing of its own.
    ///
    /// `/tmp` literally, not `NSTemporaryDirectory()`: on macOS that answers the per-process
    /// `/var/folders/…` sandbox, which is not where the writer of these files put them.
    static func folder(of session: TargetSession) -> URL? {
        guard let transcript = Subagents.transcript(of: session) else { return nil }
        let name = transcript.deletingPathExtension().lastPathComponent
        let project = transcript.deletingLastPathComponent().lastPathComponent
        guard !name.isEmpty, !project.isEmpty else { return nil }
        return URL(fileURLWithPath: "/tmp/claude-\(getuid())", isDirectory: true)
            .appendingPathComponent(project, isDirectory: true)
            .appendingPathComponent(name, isDirectory: true)
            .appendingPathComponent("tasks", isDirectory: true)
    }

    // MARK: - What one file says

    /// What the tail of an output file says: whether the command has ended, and the last thing it
    /// printed before it did or did not.
    struct Ending: Equatable {
        var ended: Bool
        var last: String?
    }

    /// The two markers Claude Code writes under a background command once it is over: `[exited
    /// with code 0]` for one that ended on its own and `[killed]` for one that was stopped. Both
    /// observed — 114 output files off this machine had one of these two under them and nothing
    /// else — and both matched at the end of the file and nowhere else.
    ///
    /// **Narrower than "a line in brackets" on purpose.** One of those files ends with
    /// `[53966:5720383:…:ERROR:…] Network service crashed`, which is Chrome logging and not an
    /// ending; a rule that read the brackets rather than the words would have called it one.
    private static func isEnding(_ line: String) -> Bool {
        (line.hasPrefix("[exited") && line.hasSuffix("]")) || line == "[killed]"
    }

    /// Read once per version of the file. A command that has printed nothing since the last beat
    /// has the same size and the same mtime, and re-reading it would be eight kilobytes of I/O
    /// per session per second to arrive at the answer already held.
    private static let lock = NSLock()
    private static var reads: [String: (signature: String, ending: Ending)] = [:]

    static func read(_ url: URL, signature: String) -> Ending {
        lock.lock()
        if let hit = reads[url.path], hit.signature == signature {
            defer { lock.unlock() }
            return hit.ending
        }
        lock.unlock()

        var ending = Ending(ended: false, last: nil)
        if let text = Transcript.tail(of: url, bytes: tailBytes) {
            let lines = text
                .split(separator: "\n", omittingEmptySubsequences: false)
                .map { $0.trimmingCharacters(in: .whitespaces) }
                .filter { !$0.isEmpty }
            if let last = lines.last {
                // The marker is the last line or it is not the marker. Anything under it would be
                // output printed after the command ended, which cannot happen.
                if isEnding(last) {
                    ending.ended = true
                } else {
                    ending.last = clipped(last)
                }
            }
        }

        lock.lock()
        reads[url.path] = (signature, ending)
        lock.unlock()
        return ending
    }

    /// How much of a command's last line is worth carrying.
    ///
    /// **A line is not a sentence.** `curl` of an ordinary web page prints a hundred and fifty
    /// kilobytes without a newline in it, and the first live reading of this put the whole of one
    /// on the wire and into a row that is one line tall. This is the width of a phone rather than
    /// a guess at what output looks like: past it, nothing is read anyway.
    private static let room = 160

    private static func clipped(_ line: String) -> String {
        line.count > room ? String(line.prefix(room - 1)) + "…" : line
    }

    // MARK: - Odds and ends

    private static func modified(_ url: URL) -> Date? {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        return attrs?[.modificationDate] as? Date
    }

    private static func size(_ url: URL) -> Int {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        return (attrs?[.size] as? Int) ?? 0
    }

    /// Everything remembered, let go of. Called alongside the other caches that only exist to
    /// make an open panel cheap.
    static func forget() {
        lock.lock()
        reads = [:]
        starts = [:]
        lock.unlock()
    }
}
