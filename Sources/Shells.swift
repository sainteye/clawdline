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
/// **Two shapes tell the whole story, and both were observed rather than documented.** A command
/// that has finished in the background has `[exited with code 0]` written under its last line and
/// keeps its file; a command running in the foreground has its file *deleted* the moment it
/// returns. So a file with no exit marker under it is a command that has not finished — and the
/// only way it can be a foreground one is if the session is working, which is the state this
/// reading refuses to answer for. See ``reading(of:states:)``.
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
    /// **A working session is skipped, and that is a correctness rule rather than a saving.** The
    /// file a foreground `Bash` is writing to right now looks exactly like the file a background
    /// one is writing to right now — no marker under either, because neither has finished. What
    /// separates them is the session: a foreground command *is* the session being busy, so
    /// nothing that is running in the foreground can belong to a session that is not working.
    /// Asking only the sessions that are between turns removes the one ambiguity these files
    /// have, and it removes it in the only state this reading is for. A session that is working
    /// already looks like a session that is working.
    ///
    /// Claude Code only, and `isClaude` rather than `isAssistant` says so on purpose: Codex keeps
    /// no such directory, so asking would be a `stat` that can only come back empty, once per
    /// session per beat.
    static func reading(of sessions: [TargetSession],
                        states: [String: SessionState]) -> [String: [Shell]] {
        var out: [String: [Shell]] = [:]
        for session in sessions where session.isClaude {
            if case .working = states[session.id] ?? .unknown { continue }
            let found = shells(of: session)
            if !found.isEmpty { out[session.id] = found }
        }
        return out
    }

    /// **The fast path is the one with nothing running, and it is nearly all of them.**
    ///
    /// A session between turns has had its foreground output files deleted behind it and has no
    /// background ones, so its `tasks` directory is empty and this costs one `readdir`. Only a
    /// session that actually has a file in there ever reads a byte.
    static func shells(of session: TargetSession) -> [Shell] {
        guard let folder = folder(of: session) else { return [] }
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: folder.path) else { return [] }

        let now = Date()
        var shells: [Shell] = []
        for name in names where name.hasSuffix(".output") {
            let id = String(name.dropLast(".output".count))
            guard !id.isEmpty else { continue }
            let file = folder.appendingPathComponent(name)
            guard let at = modified(file), now.timeIntervalSince(at) < liveWindow else { continue }
            let tail = read(file, signature: "\(at.timeIntervalSince1970)-\(size(file))")
            guard !tail.ended else { continue }
            shells.append(Shell(id: id, at: at, doing: tail.last))
        }

        // Newest first: the one that printed a second ago is the one somebody is waiting on.
        shells.sort { $0.at > $1.at }
        return Array(shells.prefix(shown))
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

    /// The marker Claude Code writes under a background command once it is over. Observed, and
    /// the one string in here that decides anything — so it is matched at the end of the file and
    /// nowhere else. A build that happens to print a line like this in the middle of its own
    /// output says nothing about whether it has finished, and this does not ask.
    private static let exitMarker = "[exited"

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
                if last.hasPrefix(exitMarker), last.hasSuffix("]") {
                    ending.ended = true
                } else {
                    ending.last = last
                }
            }
        }

        lock.lock()
        reads[url.path] = (signature, ending)
        lock.unlock()
        return ending
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
        lock.unlock()
    }
}
