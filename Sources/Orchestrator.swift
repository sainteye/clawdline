import AppKit
import CryptoKit
import Foundation

/// Root sessions dispatching child sessions — the broker side. See docs/orchestrator.md.
///
/// A *root* session (somebody's Claude Code, holding the `/clawdline` skill) writes a task under
/// `/tmp/.clawdline/<id>/` and asks this app to run it. The app opens a terminal session for the
/// named assistant, types the briefing in, watches for the child's `result.json`, accounts what
/// the child spent, and tells the root. The app is the only party that talks to everybody, so the
/// caps live here: how many children may run at once, who may dispatch at all, and how a child
/// proves it is the child.
///
/// **Two credentials, deliberately not one.** Dispatching is gated by a token in a `0600` file —
/// the same boundary `remote-token` uses, and for the same reason: through a tunnel every request
/// arrives from 127.0.0.1, so "local" is a thing only the filesystem can prove, and a paired
/// phone must never be able to start sessions. A child allowed to dispatch can read that token;
/// it still cannot exchange it for another task's secret. Every child gets its own per-task
/// secret, typed into its first message and good for exactly one thing: finishing its own task.
/// Only the secret's SHA-256 is kept once the child has been briefed.
enum Orchestrator {

    // MARK: - A task

    enum State: String {
        case queued, spawning, briefed
        case success, failure, timeout, cancelled
        case spawnFailed = "spawn_failed"

        var isTerminal: Bool {
            switch self {
            case .queued, .spawning, .briefed: return false
            default: return true
            }
        }
    }

    /// A stale value copy may add fields, but it may never move a task backwards or resurrect it.
    /// Internal rather than private so the invariant has a pure unit test.
    static func mayReplaceState(_ current: State, with candidate: State) -> Bool {
        if current == candidate { return true }
        if current.isTerminal { return false }
        if candidate.isTerminal { return true }
        switch (current, candidate) {
        case (.queued, .spawning), (.queued, .briefed), (.spawning, .briefed):
            return true
        default:
            return false
        }
    }

    struct Usage {
        var input = 0
        var output = 0
        var cacheRead = 0
        var cacheWrite = 0
        var total = 0
        var model: String?
        var costUsd: Double?
    }

    struct Task {
        let id: String
        var state: State
        var kind: String
        var title: String
        var assistant: Assistant
        /// The model the child was started on, when the task named one. Nil means whatever that
        /// assistant defaults to, which is the answer for most tasks and all older records.
        var model: String?
        /// How far the child may go before stopping to ask — what was actually used, after this
        /// Mac's ceiling was applied to what the task asked for.
        var permission = Permission.ask
        var projectDir: String
        var timeoutMinutes: Int
        var created: Date
        var spawnedAt: Date?
        var briefedAt: Date?
        var finishedAt: Date?
        var rootSessionId: String?
        var rootLabel: String?
        /// How far from the person at the keyboard this task is: `1` for one a human's session
        /// dispatched, `2` for one dispatched by a child of theirs. Stored rather than derived —
        /// the parent may be over and gone by the time anybody asks, and the answer should not
        /// change when it goes.
        var depth = 1
        /// The task whose child dispatched this one, when it said so. Nil at depth 1, and nil at
        /// depth 2 when the parent was recognised by session id instead.
        var parentTaskId: String?
        /// The whole graph this task is one node of, in the dispatcher's own words. Carried into
        /// the briefing so a leaf knows what its output feeds — which is the difference between
        /// a usable answer and an essay.
        var plan: String?
        /// Machine-global operation names acquired together when this task leaves `queued`.
        /// A queued task holds none; a spawning or briefed task holds every name in this list.
        var serialize: [String] = []
        /// Paths this task may write, relative to `projectDir`. Unlike `serialize`, these are
        /// reserved from registration (including while queued) until every terminal outcome.
        var claims: [String] = []
        /// Whether `claims` was present in task.json. An empty declared set means read-only;
        /// absence means the write set is unknown, so L1 must retain its directory warning.
        var claimsDeclared = false
        /// Absolute, canonical-root comparison keys frozen when the lease is registered. These
        /// are persisted because a claim must not change identity as its target comes into being.
        var claimKeys: [String] = []
        var childTerminalId: String?
        var childBackend: Backend?
        var childTTY: String?
        var childPID: Int32?
        var childProcStart: Date?
        var childSessionId: String?
        var transcriptPath: String?
        /// The task marker was observed in `transcriptPath`. Persisted because that fact does not
        /// expire when Claude rotates the file or the file is temporarily unavailable.
        var transcriptProven = false
        var secretHash: String
        /// The task secret encrypted with a key local to this installation. It exists only while
        /// a serialized task waits, so a restart can resume the queue without storing plaintext.
        var queuedSecret: String?
        var summary: String?
        var artifacts: [String] = []
        var usage: Usage?
        var injectAttempts = 0
        /// The most recent time the first message was handed to the terminal. In memory only:
        /// a process restart loses the plaintext secret and fails every spawning task anyway.
        var lastInjectAt: Date?
        /// The registry answer already sampled by the temporary legacy comparison. In memory
        /// only, so a restart may compare once more without imposing per-beat transcript I/O.
        var registryControlSessionID: String?
        var answeredMenu = false
        /// When the child's terminal was last seen in a reading — the difference between a child
        /// that finished and one whose tab was closed under it.
        var lastSeenChild: Date?
        /// When the child's terminal is due to be closed, once it has reported.
        ///
        /// **Written down, because three minutes is longer than this app stays running.**
        /// It used to live in memory only, on the principle that a tab should be closed on the
        /// strength of what this process saw rather than what a previous one believed — and the
        /// principle is right, but the deadline was never the belief. `./build.sh` replaces the
        /// app several times an hour; every child that reported inside the last three minutes
        /// lost its deadline with the process, and nothing ever set one again. Of the eighteen
        /// tabs left standing since the linger was written, seventeen had the app restart inside
        /// their three minutes and the eighteenth had not run out yet — Claude Code's tabs and
        /// Codex's alike, which is why this was not the assistant-specific fault it looked like.
        ///
        /// What crosses the restart is the deadline and nothing else. Whether that tab is still
        /// the child's is asked again, of a reading this process took — see ``closeStep``.
        var closeAt: Date?

        var dir: URL { Orchestrator.root.appendingPathComponent(id, isDirectory: true) }
    }

    /// The directory whose assistant-owned records belong to this child. Kept separate from the
    /// dispatch project so task isolation can choose a different working tree at this seam.
    static func cwd(of task: Task) -> String {
        task.projectDir
    }

    enum BriefingDecision: Equatable {
        case send, wait, accepted, exhausted
    }

    /// Process and registry facts assembled for one identity decision. The process start is read
    /// from `pid` itself, so the pair cannot combine two generations of the same terminal.
    struct ChildObservation: Equatable {
        var pid: Int32?
        var procStart: Date?
        var registrySessionID: String? = nil
        var registryTranscript: URL? = nil
    }

    enum IdentityStep: Equatable {
        case none
        case useRegistry(sessionID: String, transcript: URL)
        case refuseForeignProcess(seen: Int32?)
    }

    struct IdentityComparison: Equatable {
        var registrySessionID: String
        var registryTranscriptPath: String
        var legacySessionID: String?
        var legacyTranscriptPath: String?
    }

    /// Once a Claude process pair has been recorded, a later process on the same tab cannot
    /// contribute identity. An incomplete pair is unverifiable and therefore fails closed.
    static func identityStep(for task: Task, seeing observation: ChildObservation) -> IdentityStep {
        guard task.assistant == .claude else { return .none }
        if let recordedPID = task.childPID {
            guard observation.pid == recordedPID else {
                return .refuseForeignProcess(seen: observation.pid)
            }
            guard let recordedStart = task.childProcStart, let seenStart = observation.procStart,
                  abs(seenStart.timeIntervalSince(recordedStart))
                    <= SessionRegistry.startTolerance else {
                return .refuseForeignProcess(seen: observation.pid)
            }
        }
        if let sessionID = observation.registrySessionID,
           let transcript = observation.registryTranscript {
            return .useRegistry(sessionID: sessionID, transcript: transcript)
        }
        return .none
    }

    /// Record only a start time read directly from the pid beside it. A partial pair would make
    /// every later strict comparison either too permissive or permanently reject the real child.
    static func recordProcessIdentity(from observation: ChildObservation, in task: inout Task)
        -> Bool {
        guard task.childPID == nil, task.childProcStart == nil,
              let pid = observation.pid, let started = observation.procStart else { return false }
        task.childPID = pid
        task.childProcStart = started
        return true
    }

    /// A transcript can legitimately be absent while both sources already name the same session.
    /// Compare the durable identity, while retaining both paths as evidence when the ids diverge.
    static func identityComparison(registrySessionID: String, registryTranscript: URL,
                                   legacyTask: Task) -> IdentityComparison? {
        guard registrySessionID != legacyTask.childSessionId else { return nil }
        return IdentityComparison(registrySessionID: registrySessionID,
                                  registryTranscriptPath: registryTranscript.path,
                                  legacySessionID: legacyTask.childSessionId,
                                  legacyTranscriptPath: legacyTask.transcriptPath)
    }

    /// Once the briefing receipt has proved this pair, a later registry answer describes a
    /// `/clear` or parked conversation in the same process, not a correction to this task.
    static func adoptRegistryIdentity(sessionID: String, transcript: URL,
                                      in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven) else { return false }
        guard task.childSessionId != sessionID || task.transcriptPath != transcript.path
        else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = transcript.path
        task.transcriptProven = false
        return true
    }

    /// A current hook may correct a provisional pair only when its note is no older than this
    /// spawn and the recorded Claude process still matches this beat; the caller enforces both
    /// before reaching this seam. The first note remains a fallback when process inspection is
    /// unavailable, but an unverified later note never replaces identity. A correction drops the
    /// old provisional path so locate can resolve the new session. Once the briefing receipt pins
    /// the pair, even the same process may be describing `/clear` or a parked chat.
    static func adoptHookIdentity(sessionID: String, in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven),
              task.childSessionId != sessionID else { return false }
        let isFirstIdentity = task.childSessionId == nil && task.transcriptPath == nil
        let hasProcessIdentity = task.childPID != nil && task.childProcStart != nil
        guard isFirstIdentity || hasProcessIdentity else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = nil
        task.transcriptProven = false
        return true
    }

    /// The transition control samples each distinct registry answer once. The flag is transient,
    /// so a restart earns one fresh comparison without restoring per-beat I/O.
    static func beginRegistryControl(for sessionID: String, in task: inout Task) -> Bool {
        guard task.registryControlSessionID != sessionID else { return false }
        task.registryControlSessionID = sessionID
        return true
    }

    static let briefingAttemptLimit = 5
    static let briefingReceiptDelay: TimeInterval = 15

    /// Whether the assistant has drawn the empty composer that can accept a new turn.
    ///
    /// This is deliberately narrower than `SessionState.idle`. That state is the absence of a
    /// recognised menu or live line; a startup banner while slow MCP servers are still loading
    /// has exactly that absence. The composer is positive evidence that startup has completed.
    static func briefingInputReady(_ screen: String?, assistant: Assistant) -> Bool {
        guard let screen, !screen.isEmpty else { return false }
        let text = Ansi.plain(screen)
        guard SessionState.read(text, assistant: assistant) == .idle else { return false }
        // Claude Code puts U+00A0 between its caret and the composer, not a space: a real capture
        // reads `❯\u{00A0}` when empty and `❯\u{00A0}/deploy` with a draft in it. Trimming does
        // hide that — `.whitespaces` is Unicode Zs, which U+00A0 belongs to — but only at the ends
        // of a line, so the bare-caret test below passes while a prefix written with a plain space
        // never fires at all. Fold it first, once, rather than spell it into every comparison.
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.replacingOccurrences(of: "\u{00A0}", with: " ")
                     .trimmingCharacters(in: .whitespaces) }

        switch assistant {
        case .claude:
            // Claude's empty composer is either a bare caret or carries its grey `Try "…"`
            // suggestion — the bundle renders it as `Try "` around a bolded example, so a brand
            // new session is far more likely to show that than a bare caret. A submitted message
            // is echoed after the same caret, so accepting an arbitrary suffix here would turn
            // the receipt check below back into a race.
            return lines.contains { $0 == "❯" || $0.hasPrefix("❯ Try \"") }
        case .codex:
            // Codex writes sent messages after the same glyph; its empty composer is the one
            // whose placeholder says what can be entered, not merely any `›` in the scrollback.
            return lines.contains { $0 == "›" || $0.hasPrefix("› Ask Codex to do anything") }
        }
    }

    /// The words in ``firstLine(id:secret:announce:)`` that say what the session is, with the
    /// task id left off.
    ///
    /// Two things read it and they read it for different reasons. The delivery receipt below
    /// wants it *with* a particular task's id after it, which is what proves a transcript
    /// belongs to that task. ``StartPoints/front(inText:limit:)`` wants it without one, because
    /// the question there is only whether this app opened the session at all — a list of
    /// conversations to pick back up is a list of the ones you had, and a child is this app's
    /// own plumbing.
    ///
    /// Not shared with `firstLine` itself, which still spells the sentence out where it is
    /// written. The test *"the mark the list filters on is the line a child is actually given"*
    /// is what keeps the two from drifting apart.
    static let briefingMark = "Clawdline CHILD agent for task"

    /// The same words with the clause `firstLine` opens on, which is what a child's very first
    /// turn *begins* with.
    ///
    /// The narrower one above can appear anywhere in a user turn, and a receipt wants that: it is
    /// looking for delivery, and a briefing that arrived after something else was typed still
    /// arrived. A list of conversations wants the opposite. This conversation's own transcript
    /// opens with a sentence asking why the matching lives in `Orchestrator` — a question about
    /// the mark, containing the mark — and under the loose test it would have filtered itself out
    /// of the list somebody was reading it in.
    static let briefingOpening = "You are a " + briefingMark

    /// The task marker in a user turn is the delivery receipt. Looking for this specific turn,
    /// rather than any user-shaped bookkeeping row, also proves that the transcript belongs to
    /// this task before it is allowed to close the retry gate.
    static func transcriptContainsBriefing(_ transcript: String?, assistant: Assistant,
                                           taskID: String) -> Bool {
        guard let transcript else { return false }
        let marker = "\(briefingMark) \(taskID)"
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(marker)
        }
    }

    enum TranscriptOwnership: Equatable {
        case belongs, other, unavailable
    }

    private static let ownershipLock = NSLock()
    private static var ownershipCache: [String: (signature: String,
                                                  ownership: TranscriptOwnership)] = [:]
    private static let ownershipCacheLimit = 1_024
    /// A child is a fresh conversation and the briefing is its first user turn. One MiB leaves
    /// ample room for startup bookkeeping while putting a hard ceiling on every main-thread
    /// ownership check.
    private static let ownershipScanBytes = 1_048_576

    /// A guessed or restored path becomes identity only when the child's own first turn names
    /// this task. Timestamps narrow the files worth opening; they do not prove ownership.
    static func transcriptOwnership(_ url: URL, assistant: Assistant,
                                    taskID: String) -> TranscriptOwnership {
        let key = "\(assistant.rawValue)\u{0}\(taskID)\u{0}\(url.path)"
        let signature = Transcript.signature(of: url)
        ownershipLock.lock()
        if let cached = ownershipCache[key], cached.signature == signature {
            ownershipLock.unlock()
            return cached.ownership
        }
        ownershipLock.unlock()

        guard let handle = try? FileHandle(forReadingFrom: url) else { return .unavailable }
        defer { try? handle.close() }
        let data: Data
        do {
            data = try handle.read(upToCount: ownershipScanBytes) ?? Data()
        } catch {
            return .unavailable
        }
        let marker = Data("\(briefingMark) \(taskID)".utf8)
        var from = data.startIndex
        while from < data.endIndex,
              let hit = data.range(of: marker, in: from..<data.endIndex) {
            var low = hit.lowerBound
            while low > data.startIndex, data[data.index(before: low)] != 0x0A {
                low = data.index(before: low)
            }
            var high = hit.upperBound
            while high < data.endIndex, data[high] != 0x0A { high = data.index(after: high) }
            if let row = String(data: data[low..<high], encoding: .utf8),
               transcriptContainsBriefing(row, assistant: assistant, taskID: taskID) {
                cacheOwnership(.belongs, key: key, signature: signature)
                return .belongs
            }
            from = hit.upperBound
        }
        let text = String(decoding: data, as: UTF8.self)
        if Transcript.containsUserTurn(text, assistant: assistant) {
            // The briefing is the fresh child's first user turn. Finding another completed user
            // turn in the bounded prefix is positive disproof even when a long file continues;
            // its expected marker cannot first appear beyond a conversation already in progress.
            cacheOwnership(.other, key: key, signature: signature)
            return .other
        }
        // An empty file, startup metadata, or a prefix cut before any complete user row says
        // nothing about ownership. Cache that answer by signature so an unchanged large prefix
        // is not reread every polling beat; growth naturally invalidates it.
        cacheOwnership(.unavailable, key: key, signature: signature)
        return .unavailable
    }

    private static func cacheOwnership(_ ownership: TranscriptOwnership, key: String,
                                       signature: String) {
        ownershipLock.lock(); defer { ownershipLock.unlock() }
        if ownershipCache[key] == nil, ownershipCache.count >= ownershipCacheLimit,
           let oldest = ownershipCache.keys.first {
            ownershipCache.removeValue(forKey: oldest)
        }
        ownershipCache[key] = (signature, ownership)
    }

    static func transcriptBelongsToTask(_ url: URL, assistant: Assistant,
                                        taskID: String) -> Bool {
        if case .belongs = transcriptOwnership(url, assistant: assistant, taskID: taskID) {
            return true
        }
        return false
    }

    /// Pure policy for the asynchronous hand-off. A retry needs all three facts: enough time has
    /// passed, the named transcript still lacks this task's turn, and the empty composer is back.
    /// Claude and Codex append the user turn before beginning it, so an accepted first send puts
    /// the marker in the transcript before it can execute. That closes this gate before another
    /// copy can be sent; a missing transcript alone is never grounds for a retry.
    static func briefingDecision(screen: String?, assistant: Assistant, transcript: String?,
                                 transcriptKnown: Bool, taskID: String, attempts: Int,
                                 secondsSinceAttempt: TimeInterval?) -> BriefingDecision {
        if transcriptContainsBriefing(transcript, assistant: assistant, taskID: taskID) {
            return .accepted
        }
        guard briefingInputReady(screen, assistant: assistant) else { return .wait }
        if attempts == 0 { return .send }
        guard transcriptKnown else { return .wait }
        if let secondsSinceAttempt, secondsSinceAttempt < briefingReceiptDelay { return .wait }
        return attempts >= briefingAttemptLimit ? .exhausted : .send
    }

    /// What a route handler gets back; `RemoteServer` turns it into a `Response`.
    enum Reply {
        case ok([String: Any])
        case refused(status: Int, code: String, message: String, extra: [String: Any])

        static func refused(_ status: Int, _ code: String, _ message: String) -> Reply {
            .refused(status: status, code: code, message: message, extra: [:])
        }
    }

    // MARK: - Where everything lives

    static let root = URL(fileURLWithPath: "/tmp/.clawdline", isDirectory: true)

    static var storeURL: URL { RemoteAuth.directory.appendingPathComponent("orchestrator.json") }
    static var tokenURL: URL { RemoteAuth.directory.appendingPathComponent("orchestrator-token") }
    static var archiveKeyURL: URL {
        RemoteAuth.directory.appendingPathComponent("orchestrator-archive-key")
    }

    private static let lock = NSLock()
    /// Opening a terminal is bounded, not cheap: iTerm automation alone may wait 15 seconds.
    /// Pumps arrive from main-thread finalization and startup as well as the remote server, so
    /// they get the same off-main serial shape as ordinary dispatch without competing pumps.
    private static let serializePumpQueue = DispatchQueue(
        label: "dev.sainteye.clawdline.orchestrator.serialize")
    /// A background pump and a new remote dispatch may both persist. Serializing the whole
    /// snapshot-and-write prevents an older snapshot from winning the atomic rename last.
    private static let storeSaveLock = NSLock()
    private static var loaded = false
    private static var tasks: [String: Task] = [:]
    /// How many `beat` walks are inside the loop, and which walk this is. Both exist to catch the
    /// overlap that should not be possible; neither changes what a walk does.
    private static var beatsInFlight = 0
    private static var beatSequence = 0
    /// Plaintext secrets, held only between dispatch and briefing. Never on disk.
    private static var secrets: [String: String] = [:]
    private static var dispatchTimes: [Date] = []
    /// Test seam: observes the warning decision before optional terminal delivery.
    static var workspaceOverlapObserverForTesting: ((Task, [WorkspaceOverlap]) -> Void)?
    /// Child terminal id → task title, rebuilt whenever the tasks change. Read on every redraw
    /// of every session row, which is why it is a dictionary and not a walk over the tasks.
    private static var titlesByTerminal: [String: String] = [:]

    /// Terminal id → where that tab sits in the tree. Rebuilt beside ``titlesByTerminal``.
    private static var rolesByTerminal: [String: Role] = [:]

    /// Where a session sits in the tree, for anything that has to decide who to tell.
    ///
    /// **Nil is the definition of a root.** A session this app did not open for a task is one a
    /// person opened for themselves, and a person is the only audience that gets interrupted for
    /// its own sake. Everything below one is working for somebody, and that somebody is a
    /// program that can be told directly.
    struct Role: Equatable {
        let taskID: String
        /// 1 for a task a person's session dispatched, 2 for one dispatched by a child of theirs.
        let depth: Int
        let title: String
        /// When the task gives up on itself, so a notification can say how long is left to
        /// answer it. Nil before the child has been briefed — there is no clock yet.
        let deadline: Date?
        /// Whether anything is still waiting on this tab. A child's terminal lingers for three
        /// minutes after its task ends (see `orchestratorChildLinger`), and for those three
        /// minutes it is a child's tab with nobody behind it.
        let live: Bool
    }

    /// What a terminal this app opened for a task is called. Nil for every other session.
    static func title(forTerminal id: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return titlesByTerminal[id]
    }

    /// Where that terminal sits in the tree. Nil for every session a person opened themselves.
    ///
    /// `load()` first, unlike ``title(forTerminal:)`` beside it, and the asymmetry is deliberate:
    /// a title that is briefly missing is a row drawn with the tab's own name, while a role that
    /// is briefly missing is a child mistaken for a person — which is the one wrong answer this
    /// whole arrangement exists to avoid. It costs a flag check after the first call.
    static func role(forTerminal id: String) -> Role? {
        load()
        lock.lock(); defer { lock.unlock() }
        return rolesByTerminal[id]
    }

    /// Under the lock.
    private static func reindex() {
        var found: [String: String] = [:]
        var roles: [String: Role] = [:]
        for task in tasks.values {
            guard let terminal = task.childTerminalId else { continue }
            found[terminal] = task.title
            let role = Role(taskID: task.id, depth: task.depth, title: task.title,
                            deadline: task.briefedAt?
                                .addingTimeInterval(Double(task.timeoutMinutes) * 60),
                            live: !task.state.isTerminal)
            // A tab is normally one task's for its whole life. When two records name the same
            // one — a terminal id reused after a tab closed and another opened in its place —
            // the live task is the one anything asking this question means.
            if let existing = roles[terminal], existing.live, !role.live { continue }
            roles[terminal] = role
        }
        titlesByTerminal = found
        rolesByTerminal = roles
    }

    /// Commit a value copy only while the record is still the state the caller worked from.
    /// The monotonic check is the second belt: callers without a narrow expectation still cannot
    /// turn `.briefed` back into `.spawning`, or a terminal result back into a live task.
    @discardableResult
    private static func replaceTask(_ candidate: Task, expecting expected: State? = nil,
                                    discardSecret: Bool = false) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard let current = tasks[candidate.id] else { return false }
        if let expected, current.state != expected { return false }
        guard mayReplaceState(current.state, with: candidate.state) else {
            RemoteAuth.audit("orchestrator.stale_write", [
                "task": candidate.id,
                "current": current.state.rawValue,
                "candidate": candidate.state.rawValue,
            ])
            return false
        }
        tasks[candidate.id] = candidate
        if discardSecret { secrets.removeValue(forKey: candidate.id) }
        return true
    }

    // MARK: - The dispatch token

    /// Minted on first use and kept. Reading the file is the only way to hold this credential,
    /// which is the entire point — a page cannot, and a paired device was never given it.
    static func dispatchToken() -> String {
        lock.lock(); defer { lock.unlock() }
        if let onDisk = try? String(contentsOf: tokenURL, encoding: .utf8) {
            let token = onDisk.trimmingCharacters(in: .whitespacesAndNewlines)
            if !token.isEmpty { return token }
        }
        let made = RemoteAuth.newToken()
        try? FileManager.default.createDirectory(at: tokenURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(made.utf8).write(to: tokenURL, options: .atomic)
        // Re-applied after every write: an atomic write replaces the file, and the replacement
        // does not inherit the mode of what it replaced.
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: tokenURL.path)
        return made
    }

    /// Hashes both sides so the comparison is constant-time over equal lengths, like every other
    /// secret check in the app.
    static func verifyDispatch(token: String?) -> Bool {
        guard let token, !token.isEmpty else { return false }
        let expected = RemoteAuth.hex(SHA256.hash(data: Data(dispatchToken().utf8)))
        let presented = RemoteAuth.hex(SHA256.hash(data: Data(token.utf8)))
        return RemoteAuth.constantTimeEquals(expected, presented)
    }

    static func hash(ofSecret secret: String) -> String {
        RemoteAuth.hex(SHA256.hash(data: Data(secret.utf8)))
    }

    /// The at-rest key is not a request credential and is deliberately unrelated to every one.
    /// It is minted lazily, persists so queued work survives a restart, and is replaced only when
    /// the file is missing or no longer parses as exactly 32 random bytes.
    static func archiveKey() -> SymmetricKey {
        lock.lock(); defer { lock.unlock() }
        let encoded = try? String(contentsOf: archiveKeyURL, encoding: .utf8)
        if let encoded,
           let seed = Data(base64Encoded:
                encoded.trimmingCharacters(in: .whitespacesAndNewlines)),
           seed.count == 32 {
            return SymmetricKey(data: seed)
        }
        if encoded != nil {
            Log.write("orchestrator: the stored archive key will not parse — minting a new one; "
                + "serialized tasks queued under the old key will fail closed")
        }
        let made = SymmetricKey(size: .bits256)
        let seed = made.withUnsafeBytes { Data($0) }
        try? FileManager.default.createDirectory(at: archiveKeyURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(seed.base64EncodedString().utf8).write(to: archiveKeyURL, options: .atomic)
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: archiveKeyURL.path)
        return made
    }

    /// A serialized waiter has to survive an app restart, but the secret eventually typed into
    /// its child must not be stored as plaintext. The sealed value is removed before the task
    /// starts opening.
    static func sealQueuedSecret(_ secret: String) -> String? {
        return (try? ChaChaPoly.seal(Data(secret.utf8), using: archiveKey()).combined)?
            .base64EncodedString()
    }

    static func openQueuedSecret(_ sealed: String) -> String? {
        guard let data = Data(base64Encoded: sealed),
              let box = try? ChaChaPoly.SealedBox(combined: data) else { return nil }
        guard let clear = try? ChaChaPoly.open(box, using: archiveKey()) else { return nil }
        return String(data: clear, encoding: .utf8)
    }

    // MARK: - Reading the task a root wrote

    /// Everything a task.json has to say before anything is spawned from it. Pure, so a test can
    /// hand it a dictionary and a pretend filesystem.
    struct Draft: Equatable {
        var id = ""
        var kind = "custom"
        var assistant = Assistant.claude
        var model: String?
        /// What the task asked for, before the ceiling. Nil means it did not ask, and takes the
        /// ceiling itself — the setting is the default as well as the limit.
        var permission: Permission?
        var projectDir = ""
        var title = ""
        var instructions = ""
        var timeoutMinutes = 30
        var rootSessionId: String?
        var rootLabel: String?
        var parentTaskId: String?
        var plan: String?
        var serialize: [String] = []
        var claims: [String] = []
        var claimsDeclared = false
    }

    /// A live task whose working directory intersects the one being dispatched. The task is a
    /// value snapshot: warning is advisory, so a task finishing while the new tab opens does not
    /// turn a truthful observation at dispatch time into a reason to change the reply.
    struct WorkspaceOverlap {
        let task: Task
        let sharedDir: String

        func warning(for newTaskID: String) -> [String: Any] {
            [
                "code": "workspace_overlap",
                "task": task.id,
                "dir": sharedDir,
                "message": "Task \(newTaskID) overlaps active task \(task.id) at \(sharedDir).",
            ]
        }
    }

    /// One live task whose declared write set intersects the candidate's. Claim paths are
    /// absolute here so nested project directories compare in one namespace. `paths` names the
    /// shared descendant for each conflicting pair, deduplicated in declaration order.
    struct ClaimsOverlap {
        let task: Task
        let paths: [String]
        let sameRoot: Bool
        let rootsKnown: Bool
        let rootLabel: String?

        var blocks: Bool { rootsKnown && !sameRoot }

        func warning(for newTaskID: String) -> [String: Any] {
            [
                "code": rootsKnown ? "claims_overlap" : "claims_overlap_unknown_root",
                "task": task.id,
                "paths": paths,
                "message": "Task \(newTaskID) shares claimed paths with task \(task.id): "
                    + paths.joined(separator: ", ") + ".",
            ]
        }
    }

    /// One best-effort terminal delivery, separated from the AppleScript side so aggregation and
    /// missing-root decisions can be tested without a live terminal.
    struct WorkspaceOverlapNotice {
        let rootSessionID: String
        let taskID: String
        let line: String
    }

    enum DraftOutcome: Equatable {
        case ok(Draft)
        case bad(String)
    }

    static func draft(from obj: [String: Any], expecting id: String,
                      isDirectory: (String) -> Bool = StartPoints.isDirectory) -> DraftOutcome {
        guard obj["clawdline_protocol"] as? Int == 1 else {
            return .bad("clawdline_protocol must be 1")
        }
        guard isTaskID(id), obj["task_id"] as? String == id else {
            return .bad("task_id must be a lowercase UUID and match the dispatch")
        }
        guard let name = obj["assistant"] as? String, let assistant = Assistant(rawValue: name) else {
            return .bad("assistant must be claude or codex")
        }
        guard let dir = obj["project_dir"] as? String, StartPoints.usable(dir),
              isDirectory(dir) else {
            return .bad("project_dir must be an absolute path to a directory")
        }
        guard let instructions = obj["instructions"] as? String, !instructions.isEmpty,
              instructions.utf8.count <= 16_384 else {
            return .bad("instructions must be non-empty and at most 16 KiB")
        }
        // Loud here, quiet at the tab. `StartPoints.modelName` runs again on the way to the
        // command line and answers "no flag" — but somebody is still holding this request, and a
        // typo they can be told about is worth more than a session quietly on the wrong model.
        var model: String?
        if let named = obj["model"] as? String, !named.isEmpty {
            guard let ok = StartPoints.modelName(named) else {
                return .bad("model must be a model name: lower-case letters, digits, . _ -, "
                          + "at most 64 characters")
            }
            model = ok
        }
        if let plan = obj["plan"] as? String, plan.utf8.count > planLimit {
            return .bad("plan must be at most \(planLimit / 1024) KiB")
        }
        var serialize: [String] = []
        if let raw = obj["serialize"] {
            guard let values = raw as? [Any] else {
                return .bad("serialize must be an array of at most 4 tokens")
            }
            var errors: [String] = []
            if values.count > 4 { errors.append("serialize must contain at most 4 tokens") }
            var seen: Set<String> = []
            for (index, value) in values.enumerated() {
                guard let token = value as? String else {
                    errors.append("serialize[\(index)] must be a string")
                    continue
                }
                let duplicate = !seen.insert(token).inserted
                if StartPoints.modelName(token) != token {
                    errors.append("serialize[\(index)] must be 1–64 lower-case letters, digits, "
                                + ". _ -, and not begin with -")
                }
                if duplicate {
                    errors.append("serialize[\(index)] duplicates \(token)")
                }
                serialize.append(token)
            }
            if !errors.isEmpty { return .bad(errors.joined(separator: "; ")) }
        }
        var claims: [String] = []
        var claimsDeclared = false
        if let raw = obj["claims"] {
            guard let values = raw as? [Any] else {
                return .bad("claims must be an array of 0–32 relative POSIX paths")
            }
            var errors: [String] = []
            if values.count > 32 {
                errors.append("claims must contain 0–32 paths")
            }
            var seen: Set<String> = []
            for (index, value) in values.enumerated() {
                guard let path = value as? String else {
                    errors.append("claims[\(index)] must be a string")
                    continue
                }
                let duplicate = !seen.insert(path).inserted
                if path.isEmpty || path.count > 1_024 {
                    errors.append("claims[\(index)] must be 1–1024 characters")
                }
                if path.hasPrefix("/") {
                    errors.append("claims[\(index)] must be relative to project_dir")
                }
                if path.split(separator: "/", omittingEmptySubsequences: false)
                    .contains(where: { $0 == ".." }) {
                    errors.append("claims[\(index)] must not contain a .. component")
                }
                if path.unicodeScalars.contains(where: { $0.value == 0 }) {
                    errors.append("claims[\(index)] must be a POSIX path without NUL")
                }
                if duplicate {
                    errors.append("claims[\(index)] duplicates \(path)")
                }
                claims.append(path)
            }
            if !errors.isEmpty { return .bad(errors.joined(separator: "; ")) }
            claimsDeclared = true
        }
        var permission: Permission?
        if let named = obj["permission_mode"] as? String, !named.isEmpty {
            guard let ok = Permission(rawValue: named) else {
                return .bad("permission_mode must be one of: "
                          + Permission.allCases.map(\.rawValue).joined(separator: ", "))
            }
            permission = ok
        }
        var made = Draft()
        made.id = id
        made.assistant = assistant
        made.model = model
        made.permission = permission
        made.serialize = serialize
        made.claims = claims
        made.claimsDeclared = claimsDeclared
        made.plan = (obj["plan"] as? String).flatMap {
            let text = $0.trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? nil : text
        }
        made.projectDir = dir
        made.instructions = instructions
        made.kind = (obj["kind"] as? String).flatMap { $0.isEmpty ? nil : String($0.prefix(40)) } ?? "custom"
        made.title = String(((obj["title"] as? String) ?? "task").prefix(200))
        if let minutes = obj["timeout_minutes"] as? Int {
            guard (1...240).contains(minutes) else { return .bad("timeout_minutes must be 1…240") }
            made.timeoutMinutes = minutes
        }
        let rootObj = obj["root"] as? [String: Any] ?? [:]
        made.rootSessionId = rootObj["session_id"] as? String
        made.rootLabel = (rootObj["label"] as? String).map { String($0.prefix(120)) }
        // A child knows its own task id — it is in the first line it was ever sent — long before
        // this app has worked out what the session inside that tab calls itself. Naming it here
        // is how a dispatch from one level down is recognised as such on the first try, and for
        // a Codex child it is the only way: its session id lives in a rollout file rather than in
        // the hook notes `rootSessionId` is matched against.
        made.parentTaskId = (rootObj["parent_task"] as? String).flatMap { isTaskID($0) ? $0 : nil }
        return .ok(made)
    }

    /// Lowercase UUID, which is also the directory name — so the id can never carry a path.
    static func isTaskID(_ id: String) -> Bool {
        guard id.count == 36 else { return false }
        return id.allSatisfy { ("a"..."f").contains($0) || $0.isNumber || $0 == "-" }
    }

    /// Current holders followed by older waiters entitled to a shared token first. Roots are
    /// deliberately absent from this comparison: the namespace covers the whole machine. Every
    /// token in a request is considered together, so a queued multi-token task holds none of them.
    static func serializeBlockers(for candidate: Task, among existing: [Task]) -> [Task] {
        guard candidate.state == .queued, !candidate.serialize.isEmpty else { return [] }
        let wanted = Set(candidate.serialize)
        func earlier(_ task: Task) -> Bool {
            task.created < candidate.created
                || (task.created == candidate.created && task.id < candidate.id)
        }
        return existing.filter { task in
            guard task.id != candidate.id,
                  !wanted.isDisjoint(with: task.serialize) else { return false }
            if task.state == .spawning || task.state == .briefed { return true }
            return task.state == .queued && earlier(task)
        }.sorted { left, right in
            if left.created == right.created { return left.id < right.id }
            return left.created < right.created
        }
    }

    private static func serializeBlockersLocked(for candidate: Task) -> [Task] {
        serializeBlockers(for: candidate, among: Array(tasks.values))
    }

    /// Freeze claims into one namespace while the validated project directory exists. Only the
    /// root touches the filesystem; relative claims are then normalised and joined literally so
    /// creating a target (or a symlink below the root) can never change a lease's identity.
    static func freezeClaims(_ claims: [String], projectDir: String) -> [String] {
        guard !claims.isEmpty else { return [] }
        let root = URL(fileURLWithPath: projectDir, isDirectory: true)
            .standardizedFileURL.resolvingSymlinksInPath().path
        let separator = root == "/" ? "" : "/"
        return claims.map { claim in
            let relative = claim.split(separator: "/", omittingEmptySubsequences: true)
                .filter { $0 != "." }.joined(separator: "/")
            return relative.isEmpty ? root : root + separator + relative
        }
    }

    /// The shared descendant of two already-frozen claim keys. This is deliberately only string
    /// work: dispatch holds the global orchestrator lock while it compares every live lease.
    private static func sharedClaimPath(_ first: String, _ second: String) -> String? {
        if first == second { return first }
        let firstPrefix = first == "/" ? "/" : first + "/"
        if second.hasPrefix(firstPrefix) { return second }
        let secondPrefix = second == "/" ? "/" : second + "/"
        if first.hasPrefix(secondPrefix) { return first }
        return nil
    }

    /// Two explicit write declarations make L1 redundant when their frozen scopes do not meet.
    /// The empty declaration is the useful edge: it positively says the task is read-only.
    private static func declaredClaimsAreDisjoint(_ first: Task, _ second: Task) -> Bool {
        guard first.claimsDeclared, second.claimsDeclared else { return false }
        return !first.claimKeys.contains { claimed in
            second.claimKeys.contains { sharedClaimPath(claimed, $0) != nil }
        }
    }

    /// Pure dispatch-time claims scan. Unlike L1, queued tasks participate: a claim is a
    /// reservation made at dispatch, not evidence that a tab has started touching files.
    static func claimsOverlaps(for newTask: Task, among existing: [Task]) -> [ClaimsOverlap] {
        guard !newTask.claimKeys.isEmpty, !newTask.state.isTerminal else { return [] }
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = resolvedRootKey(of: newTask, among: indexed)
        return existing.compactMap { task -> ClaimsOverlap? in
            guard task.id != newTask.id, !task.state.isTerminal, !task.claimKeys.isEmpty else {
                return nil
            }
            var paths: [String] = []
            var seen: Set<String> = []
            for claimed in newTask.claimKeys {
                for other in task.claimKeys {
                    if let shared = sharedClaimPath(claimed, other),
                       seen.insert(shared).inserted {
                        paths.append(shared)
                    }
                }
            }
            guard !paths.isEmpty else { return nil }
            let root = rootTask(of: task, among: indexed)
            let otherRoot = resolvedRootKey(of: task, among: indexed)
            let rootsKnown = newRoot != nil && otherRoot != nil
            return ClaimsOverlap(task: task, paths: paths,
                                 sameRoot: rootsKnown && otherRoot == newRoot,
                                 rootsKnown: rootsKnown,
                                 rootLabel: root.rootLabel ?? task.rootLabel)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    private static func claimsOverlapsLocked(for candidate: Task) -> [ClaimsOverlap] {
        claimsOverlaps(for: candidate, among: Array(tasks.values))
    }

    /// The directory both tasks may write, or nil when their paths are merely string prefixes.
    ///
    /// Paths are resolved the way the rest of the project resolves working directories:
    /// standardise first, then follow symlinks, and compare the resulting spelling exactly.
    /// Comparing components is what keeps `/a/b` separate from `/a/bc`, and also handles `/`
    /// without a special string-prefix case.
    static func sharedWorkspaceDirectory(_ first: String, _ second: String) -> String? {
        let first = URL(fileURLWithPath: first).standardizedFileURL
            .resolvingSymlinksInPath().path
        let second = URL(fileURLWithPath: second).standardizedFileURL
            .resolvingSymlinksInPath().path
        let firstParts = URL(fileURLWithPath: first).pathComponents
        let secondParts = URL(fileURLWithPath: second).pathComponents
        let common = min(firstParts.count, secondParts.count)
        for index in 0..<common where firstParts[index] != secondParts[index] { return nil }
        if firstParts.count == common { return second }
        if secondParts.count == common { return first }
        return nil
    }

    /// The root key used throughout the orchestrator, with the task table supplied explicitly so
    /// the dispatch-time overlap rules remain a pure unit-test seam.
    private static func rootTask(of task: Task, among existing: [String: Task]) -> Task {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId, let above = existing[parentID], hops < depthFloor {
            at = above
            hops += 1
        }
        return at
    }

    private static func rootKey(of task: Task, among existing: [String: Task]) -> String {
        let at = rootTask(of: task, among: existing)
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// Claims are a hard gate only when both trees can actually be identified. A task with an
    /// unresolved parent and no independently supplied root session is unknown, not a new root.
    private static func resolvedRootKey(of task: Task,
                                        among existing: [String: Task]) -> String? {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId {
            guard hops < depthFloor, let above = existing[parentID] else {
                return at.rootSessionId
            }
            at = above
            hops += 1
        }
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// Pure half of the dispatch-time scan, kept visible to the unit suite so path boundaries,
    /// root identity and terminal-state filtering do not need a live terminal to exercise them.
    static func workspaceOverlaps(for newTask: Task,
                                  among existing: [Task]) -> [WorkspaceOverlap] {
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = rootKey(of: newTask, among: indexed)
        let rooted = existing.map { (task: $0, rootKey: rootKey(of: $0, among: indexed)) }
        return workspaceOverlaps(for: newTask, rootKey: newRoot, among: rooted)
    }

    private static func workspaceOverlaps(for newTask: Task, rootKey newRoot: String,
                                          among existing: [(task: Task, rootKey: String)])
        -> [WorkspaceOverlap] {
        guard newTask.state == .spawning || newTask.state == .briefed else { return [] }
        return existing.compactMap { item -> WorkspaceOverlap? in
            let task = item.task
            guard task.id != newTask.id,
                  task.state == .spawning || task.state == .briefed,
                  item.rootKey != newRoot,
                  !declaredClaimsAreDisjoint(newTask, task),
                  let shared = sharedWorkspaceDirectory(newTask.projectDir, task.projectDir)
            else { return nil }
            return WorkspaceOverlap(task: task, sharedDir: shared)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    private static func workspaceOverlaps(for newTask: Task) -> [WorkspaceOverlap] {
        lock.lock()
        let newRoot = rootKeyLocked(of: newTask)
        let existing = tasks.values.map { (task: $0, rootKey: rootKeyLocked(of: $0)) }
        lock.unlock()
        return workspaceOverlaps(for: newTask, rootKey: newRoot, among: existing)
    }

    /// Wire payload shared by first dispatches and idempotent retries. Keeping the optional field
    /// here makes "absent, not an empty array" explicit and independently testable.
    static func dispatchPayload(record: [String: Any], taskID: String,
                                overlaps: [WorkspaceOverlap],
                                claimsOverlaps: [ClaimsOverlap] = []) -> [String: Any] {
        var reply: [String: Any] = ["ok": true, "task": record]
        let warnings = overlaps.map { $0.warning(for: taskID) }
            + claimsOverlaps.filter { !$0.blocks }.map { $0.warning(for: taskID) }
        if !warnings.isEmpty {
            reply["warnings"] = warnings
        }
        return reply
    }

    /// The actionable context returned when another root already reserved a write path.
    static func workspaceBusyExtra(_ overlap: ClaimsOverlap) -> [String: Any] {
        [
            "blocking_task": overlap.task.id,
            "title": overlap.task.title,
            "root_label": overlap.rootLabel as Any? ?? NSNull(),
            "created": Int(overlap.task.created.timeIntervalSince1970),
            "conflict_paths": overlap.paths,
            "retry_after": 60,
        ]
    }

    // MARK: - Dispatch

    /// Runs on the server queue. Everything filesystem- and process-shaped is safe there — the
    /// `/start` route has always called `StartPoints.start` from it.
    static func dispatch(taskID: String, secret: String) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
        }
        guard isTaskID(taskID) else {
            return .refused(422, "bad_task", "task_id must be a lowercase UUID.")
        }
        // Same task again is the same answer again: the root retrying a dispatch that already
        // landed must not spawn a second child.
        if let existing = held(taskID) { return successfulDispatchReply(for: existing) }
        guard secret.count == 64, secret.allSatisfy({ ("a"..."f").contains($0) || $0.isNumber }) else {
            return .refused(422, "bad_task", "secret must be 64 hex characters.")
        }
        guard let rateTicket = takeDispatchRate() else {
            return .refused(429, "rate_limited", "Too many dispatches; wait a few minutes.")
        }

        let file = root.appendingPathComponent(taskID, isDirectory: true)
            .appendingPathComponent("task.json")
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return .refused(422, "bad_task", "No readable task.json under /tmp/.clawdline/<task_id>/.")
        }
        let made: Draft
        switch draft(from: obj, expecting: taskID) {
        case .bad(let why): return .refused(422, "bad_task", why)
        case .ok(let ok): made = ok
        }

        // How deep this one sits. A dispatch names who asked, and if who asked is itself a live
        // child then this task is one level below that child's. Best-effort in the sense that a
        // caller can lie about its identity — but lying only ever moves a task *down* (the two
        // signals are combined by taking the deeper answer) or into somebody else's bucket, and
        // `orchestratorMaxDescendants` sits over the whole machine either way.
        let depth = depthOfNew(parentTask: made.parentTaskId, rootSession: made.rootSessionId)
        let floor = depthFloor
        if depth > floor {
            return .refused(409, "depth_exceeded",
                            floor == 1
                            ? "A child session cannot dispatch tasks of its own."
                            : "Tasks go two levels deep; a child of a child cannot dispatch.")
        }
        let cap = depth == 1 ? Config.shared.orchestratorMaxChildren
                             : Config.shared.orchestratorMaxGrandchildren
        if activeCount(dispatchedBy: made.rootSessionId, parentTask: made.parentTaskId) >= cap {
            return .refused(status: 429, code: "over_capacity",
                            message: "All \(cap) child slots for this session are busy; "
                                   + "retry when one finishes.",
                            extra: ["retry_after": 60])
        }
        // And the ceiling over everyone. The per-dispatcher caps are what one session may spend;
        // this is what the Mac may, and it is the one a caller cannot talk its way around by
        // claiming to be somebody else.
        let ceiling = Config.shared.orchestratorMaxDescendants
        if activeCount() >= ceiling {
            return .refused(status: 429, code: "over_capacity",
                            message: "All \(ceiling) child sessions on this Mac are busy; "
                                   + "retry when one finishes.",
                            extra: ["retry_after": 60])
        }

        // The ceiling is the default as well as the limit: a task that said nothing gets it, and
        // one that asked for more is quietly given it instead. Quietly on purpose — the work is
        // still worth doing more carefully, and a refusal here would only teach callers to ask
        // for less than they need. The record says what was actually used.
        let allowed = Config.shared.orchestratorPermissionCeiling
        let permission = min(made.permission ?? allowed, allowed)

        var task = Task(id: taskID, state: .queued, kind: made.kind, title: made.title,
                        assistant: made.assistant, model: made.model,
                        permission: permission, projectDir: made.projectDir,
                        timeoutMinutes: made.timeoutMinutes, created: Date(),
                        rootSessionId: made.rootSessionId, rootLabel: made.rootLabel,
                        depth: depth, parentTaskId: made.parentTaskId, plan: made.plan,
                        serialize: made.serialize, claims: made.claims,
                        claimsDeclared: made.claimsDeclared,
                        secretHash: hash(ofSecret: secret))
        task.claimKeys = freezeClaims(task.claims, projectDir: task.projectDir)
        if !task.serialize.isEmpty {
            guard let sealed = sealQueuedSecret(secret) else {
                return .refused(500, "internal", "Could not protect the queued task secret.")
            }
            task.queuedSecret = sealed
        }

        // Claims are checked and registered under the same lock. If those were separate steps,
        // two concurrent dispatches could both observe a free path and then both reserve it.
        // Queued serialized work enters here too: reservation starts at dispatch, not promotion.
        lock.lock()
        let claimsOverlaps = claimsOverlapsLocked(for: task)
        if let blocker = claimsOverlaps.first(where: \.blocks) {
            lock.unlock()
            refundDispatchRate(rateTicket)
            RemoteAuth.audit("orchestrator.claims.blocked", [
                "task": taskID,
                "blocking_task": blocker.task.id,
                "root": made.rootLabel ?? made.rootSessionId ?? "unknown",
                "conflicts": blocker.paths.joined(separator: ","),
            ])
            return .refused(status: 409, code: "workspace_busy",
                            message: "Another dispatch tree has reserved a path this task claims.",
                            extra: workspaceBusyExtra(blocker))
        }
        tasks[taskID] = task
        secrets[taskID] = secret
        lock.unlock()
        RemoteAuth.audit("orchestrator.dispatch", ["task": taskID, "assistant": made.assistant.rawValue,
                                                   "cwd": made.projectDir, "kind": made.kind,
                                                   "depth": String(depth),
                                                   "model": made.model ?? "default",
                                                   "permission": permission.rawValue])

        // Straight away rather than on the next beat: the root is holding its breath on this
        // request, and the answer should already say whether a terminal opened or which older
        // serialized work left this task queued.
        let needsPump = !task.serialize.isEmpty
        if !needsPump {
            task = spawn(task)
            _ = replaceTask(task, expecting: .queued, discardSecret: task.state.isTerminal)
        }
        save()
        DispatchQueue.main.async { SessionWatch.shared.nudge() }
        RemoteServer.shared.broadcastOrchestrator()
        let reply = successfulDispatchReply(for: task, notify: true,
                                             claimsOverlaps: claimsOverlaps)
        if needsPump { scheduleSerializePump() }
        return reply
    }

    /// One response builder for both the first request and an idempotent retry. The scan happens
    /// after spawn so a task that failed to open is already terminal and produces no warning.
    private static func successfulDispatchReply(for task: Task, notify: Bool = false,
                                                claimsOverlaps: [ClaimsOverlap]? = nil) -> Reply {
        guard let record = existingRecord(task.id) else {
            return .refused(500, "internal", "The task was lost while being made.")
        }
        let overlaps = workspaceOverlaps(for: task)
        if notify { notifyWorkspaceOverlaps(newTask: task, overlaps: overlaps) }
        let claimWarnings: [ClaimsOverlap]
        if let claimsOverlaps {
            claimWarnings = claimsOverlaps
        } else {
            lock.lock()
            claimWarnings = claimsOverlapsLocked(for: task)
            lock.unlock()
        }
        return .ok(dispatchPayload(record: record, taskID: task.id, overlaps: overlaps,
                                   claimsOverlaps: claimWarnings))
    }

    private static func spawn(_ task: Task) -> Task {
        var task = task
        task.queuedSecret = nil
        let place = StartPoints.Place(id: StartPoints.id(for: task.projectDir),
                                      path: task.projectDir,
                                      label: task.title, at: Date())
        // A place this session may reach, because everything it was sent to do is outside the
        // project its tab was opened in. The briefing, the task file, the artifacts: all of it
        // lives under /tmp/.clawdline, and reaching outside the working directory is a boundary
        // an assistant asks about before crossing. Nobody is watching a child's tab to answer,
        // so without this the first question is where the task stops — and the first question is
        // *"may I read my own instructions?"*, before a single line of the work.
        //
        // **How much of it depends on whether this one may dispatch.** A leaf only ever touches
        // its own directory, so that is all it is given. A child that may hand work on has to
        // make, brief and read back directories that do not exist yet and whose names it invents,
        // which no per-task grant can name in advance — so it gets the parent. That is the whole
        // of the difference, and it is why the second level felt so much worse than the first:
        // every grandchild was another question nobody was there to answer.
        let mayDispatch = task.depth < depthFloor
            && Config.shared.orchestratorMaxGrandchildren > 0
        switch StartPoints.start(place, assistant: task.assistant, model: task.model,
                                 permission: task.permission,
                                 addDir: mayDispatch ? root.path : task.dir.path) {
        case .refused(_, let code, let message, _):
            task.state = .spawnFailed
            task.summary = "\(code): \(message)"
            task.finishedAt = Date()
        case .started(let id, let backend):
            task.state = .spawning
            task.spawnedAt = Date()
            task.childTerminalId = id
            task.childBackend = backend
        }
        return task
    }

    /// Move one eligible waiter to `spawning` under the scheduling lock before opening anything.
    /// That state change is the atomic acquisition of all its names. Persisting it first also
    /// makes a crash fail closed: startup will not open a second tab for an operation that may
    /// already have crossed the external side-effect boundary.
    private static func startQueuedTaskIfEligible(_ id: String) -> Task? {
        lock.lock()
        guard let snapshot = tasks[id], snapshot.state == .queued,
              serializeBlockersLocked(for: snapshot).isEmpty else {
            lock.unlock()
            return nil
        }
        let inMemorySecret = secrets[id]
        lock.unlock()

        let clear = inMemorySecret ?? snapshot.queuedSecret.flatMap(openQueuedSecret)
        guard let clear,
              RemoteAuth.constantTimeEquals(snapshot.secretHash, hash(ofSecret: clear)) else {
            let fail = {
                finalize(id, as: .spawnFailed,
                         summary: "The queued task secret could not be recovered.",
                         pumpQueue: false)
            }
            if Thread.isMainThread { fail() } else { DispatchQueue.main.sync(execute: fail) }
            return held(id)
        }

        lock.lock()
        guard var starting = tasks[id], starting.state == .queued,
              serializeBlockersLocked(for: starting).isEmpty else {
            lock.unlock()
            return nil
        }
        starting.state = .spawning
        starting.queuedSecret = nil
        tasks[id] = starting
        secrets[id] = clear
        lock.unlock()
        save()

        // A queued task has not touched the workspace and is excluded from L1. Promotion is the
        // first moment its warning is meaningful, so queued dispatches receive it as a typed line
        // instead of in the HTTP response that has already gone back.
        let overlaps = workspaceOverlaps(for: starting)
        notifyWorkspaceOverlaps(newTask: starting, overlaps: overlaps)

        let opened = spawn(starting)
        if opened.state == .spawnFailed {
            let fail = {
                finalize(id, as: .spawnFailed, summary: opened.summary, pumpQueue: false)
            }
            if Thread.isMainThread { fail() } else { DispatchQueue.main.sync(execute: fail) }
            return held(id)
        }
        guard replaceTask(opened, expecting: .spawning) else { return nil }
        return held(id)
    }

    /// Start every queued serialized task that can acquire all of its names together. A pass may
    /// start several disjoint operations; FIFO keeps a later user behind every older shared name.
    @discardableResult
    private static func pumpSerializeQueue() -> Bool {
        var changed = false
        while true {
            lock.lock()
            let next = tasks.values
                .filter { $0.state == .queued && !$0.serialize.isEmpty }
                .sorted {
                    if $0.created == $1.created { return $0.id < $1.id }
                    return $0.created < $1.created
                }
                .first { serializeBlockersLocked(for: $0).isEmpty }
            lock.unlock()
            guard let next else { break }
            guard startQueuedTaskIfEligible(next.id) != nil else { continue }
            changed = true
        }
        if changed {
            save()
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            RemoteServer.shared.broadcastOrchestrator()
        }
        return changed
    }

    /// Serialize pumps rather than opening on their callers. A pump-triggered spawn failure
    /// finalizes on main with `pumpQueue: false`; the outer pass is already responsible for the
    /// next waiter, so it cannot recurse back into itself.
    private static func scheduleSerializePump() {
        serializePumpQueue.async { _ = pumpSerializeQueue() }
    }

    /// Ten dispatches in ten minutes, or one full tree's worth, whichever is more.
    ///
    /// The window is a brake on a loop, not a second capacity cap — and once a child can dispatch
    /// too, filling the allowed tree legitimately takes more than ten calls. A limit that refuses
    /// the work the caps just permitted teaches people to retry, which is the behaviour it exists
    /// to discourage.
    static func takeDispatchRate() -> Date? {
        lock.lock(); defer { lock.unlock() }
        let now = Date()
        let allowed = max(10, Config.shared.orchestratorMaxDescendants)
        dispatchTimes = dispatchTimes.filter { now.timeIntervalSince($0) < 600 }
        guard dispatchTimes.count < allowed else { return nil }
        dispatchTimes.append(now)
        return now
    }

    /// A claims refusal registered no work, so its provisional rate entry is returned. Dispatch
    /// is served on one serial queue; matching the exact timestamp also keeps this safe in tests.
    static func refundDispatchRate(_ ticket: Date) {
        lock.lock(); defer { lock.unlock() }
        if let index = dispatchTimes.lastIndex(of: ticket) {
            dispatchTimes.remove(at: index)
        }
    }

    private static func activeCount() -> Int {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.filter { !$0.state.isTerminal }.count
    }

    /// The live tasks already dispatched by whoever is asking now.
    ///
    /// Two ways of naming the same session, either of which is enough: the task it is the child
    /// of, and the session id it calls itself by. A dispatch that gives neither is nobody's in
    /// particular, and shares a bucket with every other anonymous one — which is the right answer
    /// for a caller that declined to say who it is, and the ceiling covers the rest.
    private static func activeCount(dispatchedBy session: String?, parentTask: String?) -> Int {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.filter { task in
            guard !task.state.isTerminal else { return false }
            if let parentTask, task.parentTaskId == parentTask { return true }
            if let session { return task.rootSessionId == session }
            return task.rootSessionId == nil && task.parentTaskId == nil
        }.count
    }

    /// The depth a task dispatched right now would sit at: one below its parent, or 1 when the
    /// caller is not a child of anything this app is running.
    ///
    /// **The deeper of the two answers wins.** A child names its parent task and, when it can,
    /// its own session id; either alone identifies it. Taking the maximum means a caller that
    /// gets one of them wrong — or invents one — can only end up further down, never nearer the
    /// top, so the mistake costs it capacity instead of buying any.
    private static func depthOfNew(parentTask: String?, rootSession: String?) -> Int {
        lock.lock(); defer { lock.unlock() }
        var parent = 0
        for task in tasks.values where !task.state.isTerminal {
            let isParent = (parentTask != nil && task.id == parentTask)
                || (rootSession != nil && task.childSessionId == rootSession)
            if isParent { parent = max(parent, task.depth) }
        }
        return parent + 1
    }

    // MARK: - Completion over HTTP

    static func complete(taskID: String, secret: String, status: String, summary: String) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !secret.isEmpty,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            RemoteAuth.audit("orchestrator.complete", ["task": taskID, "ok": "0", "why": "bad_secret"])
            return .refused(403, "forbidden", "That is not this task's secret.")
        }
        guard !task.state.isTerminal else {
            return .refused(409, "already_done", "That task already finished.")
        }
        let outcome: State = status == "success" ? .success : .failure
        let words = summary.isEmpty ? nil : String(summary.prefix(2000))
        DispatchQueue.main.async { finalize(taskID, as: outcome, summary: words) }
        return .ok(["ok": true])
    }

    // MARK: - Cancel

    static func cancel(taskID: String) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !task.state.isTerminal else {
            return .refused(409, "already_done", "That task already finished.")
        }
        RemoteAuth.audit("orchestrator.cancel", ["task": taskID])
        // Deepest first, for the same reason the root cascade goes that way: whatever this child
        // handed on is work nobody asked for any more, and left alone it would carry on reporting
        // into a tab that is being ended right now.
        for id in liveTasks(under: [taskID]) {
            guard let below = held(id) else { continue }
            RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_cancelled",
                                                     "parent": taskID])
            if below.state == .queued {
                let finish = {
                    finalize(below.id, as: .cancelled, summary: "Cancelled with its parent.",
                             pumpQueue: false)
                }
                if Thread.isMainThread { finish() }
                else { DispatchQueue.main.sync(execute: finish) }
            } else {
                cancelInPlace(below)
            }
        }
        if task.state == .queued {
            let finish = { finalize(task.id, as: .cancelled, summary: "Cancelled.") }
            if Thread.isMainThread { finish() } else { DispatchQueue.main.sync(execute: finish) }
        } else {
            cancelInPlace(task)
        }
        // A running child's polite exit finalizes asynchronously. A waiter has no child to end
        // and is finalized synchronously, so cancelling queued work is immediately observable.
        var record = existingRecord(taskID) ?? [:]
        record["state"] = State.cancelled.rawValue
        return .ok(["ok": true, "task": record])
    }

    /// Cancelling with no HTTP answer wrapped around it: the child's tab ended the polite way,
    /// then the task written down. Shared with the cascade below so there is one way to cancel a
    /// task rather than two that drift.
    ///
    /// Not on the main thread: `Targets.end` types the quit word and then waits for the child to
    /// actually be gone — a few hundred milliseconds when it leaves on the word, and a bounded
    /// five and a bit when it has to be made to. Both callers arrive on the server's queue.
    private static func cancelInPlace(_ task: Task) {
        if let childID = task.childTerminalId,
           let child = target(withID: childID) {
            _ = Targets.end(child)
        }
        DispatchQueue.main.async { finalize(task.id, as: .cancelled, summary: "Cancelled.") }
    }

    /// The live tasks a root session dispatched, oldest first.
    ///
    /// Apart from the cascade because it is the half worth testing on its own, and *live* only:
    /// what a root's leaving does to work still running is not what it does to work that
    /// finished, so the two are chosen separately — `lingeringTasks` is the other half. Matched
    /// on `root_session`, which is the assistant's own id for itself and not any name this app
    /// has for a tab.
    static func liveTasks(dispatchedBy rootSessionId: String) -> [String] {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks.values
            .filter { !$0.state.isTerminal && $0.rootSessionId == rootSessionId }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    /// The finished tasks a root dispatched that still name a child tab, oldest first.
    ///
    /// A finished child's tab is kept for a while so somebody can read what it did. The somebody
    /// was the root, and when it goes the tab is a window onto a conversation that has ended —
    /// still drawn indented under a session no longer in the list.
    ///
    /// **Keyed on the tab, not on `closeAt`.** The linger deadline lives only in memory, so every
    /// task that outlived a restart of the app has none, while its tab is still very much on
    /// screen: closing the root would then walk straight past exactly the rows somebody is
    /// looking at. `child_terminal` is written down, which is also what the page reads to decide
    /// a row still belongs under its root — the same rule on both sides, so what a close takes is
    /// what a reader sees.
    ///
    /// Whether the tab is really still there is `closeChildTab`'s question; it answers false when
    /// the tab has gone, or has become somebody else's since.
    static func lingeringTasks(dispatchedBy rootSessionId: String) -> [String] {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks.values
            .filter { $0.state.isTerminal && $0.childTerminalId != nil
                        && $0.rootSessionId == rootSessionId }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    /// The live tasks dispatched from inside the tabs these tasks opened — the level below a
    /// cascade's first.
    ///
    /// A child is recognised by whichever of the two names it managed to give: the parent task
    /// it declared, or the session id it calls itself by, which this app only learns once it has
    /// read the child's transcript. Either is enough, and a child that gave neither is not
    /// collected — it is not filed under anyone, so there is no one whose leaving takes it.
    static func liveTasks(under parents: [String]) -> [String] {
        tasksUnder(parents) { !$0.state.isTerminal }
    }

    /// The finished-but-still-tabbed tasks one level below these — `lingeringTasks`' other half.
    static func lingeringTasks(under parents: [String]) -> [String] {
        tasksUnder(parents) { $0.state.isTerminal && $0.childTerminalId != nil }
    }

    private static func tasksUnder(_ parents: [String], where keep: (Task) -> Bool) -> [String] {
        guard !parents.isEmpty else { return [] }
        load()
        lock.lock(); defer { lock.unlock() }
        let above = parents.compactMap { tasks[$0] }
        guard !above.isEmpty else { return [] }
        let ids = Set(above.map { $0.id })
        // `childSessionId` can survive from a registry written before ownership proofs were
        // persisted. It may describe a sibling, so it becomes a cancellation key only after the
        // task marker has proved the paired path. This lazy check touches at most the requested
        // parents, never every historical transcript during app startup.
        let sessions = Set(above.compactMap { provenChildSessionID(of: $0) })
        return tasks.values
            .filter { task in
                guard keep(task), !ids.contains(task.id) else { return false }
                if let parent = task.parentTaskId, ids.contains(parent) { return true }
                if let root = task.rootSessionId, sessions.contains(root) { return true }
                return false
            }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    private static func provenChildSessionID(of task: Task) -> String? {
        guard let sessionID = task.childSessionId,
              let path = task.transcriptPath else { return nil }
        if task.transcriptProven { return sessionID }
        let transcript = URL(fileURLWithPath: path)
        guard case .belongs = transcriptOwnership(transcript, assistant: task.assistant,
                                                  taskID: task.id) else { return nil }
        switch task.assistant {
        case .claude:
            let fromPath = transcript.deletingPathExtension().lastPathComponent
            return fromPath == sessionID ? sessionID : nil
        case .codex:
            return Codex.head(of: transcript)?.id == sessionID ? sessionID : nil
        }
    }

    /// Ending a root session ends the work it dispatched. Two things happen, and they are not the
    /// same thing: a task still running is cancelled and its child's tab ended, while a task that
    /// already finished keeps its record — `success` is a fact about work that happened — and
    /// loses only the linger holding its tab open. Answers with both sets.
    ///
    /// **Runs before the root's own tab goes, and the ordering is the whole of it.** A task knows
    /// its root by the session id in that session's hook note, and the note is reached through
    /// the tty of a tab that is a second away from leaving the reading. Close the root first and
    /// there is nothing left to match against: the children run on, reporting into a conversation
    /// that ended.
    ///
    /// **Explicitly closing a session, and nothing else.** A tab closed by hand leaves the
    /// children alone: the app does not watch a root for signs of death, because "not in this
    /// reading" is also true of a terminal that lost its accessibility permission for a moment,
    /// and being wrong about that would kill somebody's work mid-turn.
    ///
    /// **Two levels, deepest first.** A child may dispatch in turn, so a root's leaving has to
    /// reach the tasks its children asked for as well. They are collected before anything is
    /// cancelled — a grandchild is found through its parent's `child_session`, which stops being
    /// a useful thing to match on the moment that parent's tab goes — and ended from the bottom
    /// up, so no tab is closed while something it is still holding open is being read for.
    ///
    /// The level below is gathered from the finished children too, not only the live ones: a
    /// child that reported while the work it handed on is still running leaves a grandchild that
    /// belongs to nobody otherwise.
    ///
    /// A busy child gets `cancel`'s decisiveness rather than `closeChild`'s ten minutes of
    /// patience. Those are different moments: one is tidying up after work that finished, this is
    /// somebody pressing close, and making them wait on a child they cannot see is not the thing
    /// they asked for.
    ///
    /// **A finished child's tab goes too.** It is standing there because somebody might still
    /// want to read what it did, and the page draws it indented under the root that asked for it.
    /// Leave it and closing a root leaves its children on screen, filed under a session that is
    /// no longer in the list — which is the shape of the bug this was written to fix, and reads
    /// as the close having done nothing at all.
    @discardableResult
    static func cancelChildren(ofRoot session: TargetSession) -> [String] {
        guard let rootSession = rootIdentity(of: session) else { return [] }
        let mine = liveTasks(dispatchedBy: rootSession)
        let lingering = lingeringTasks(dispatchedBy: rootSession)
        let below = liveTasks(under: mine + lingering)
        let lingeringBelow = lingeringTasks(under: mine + lingering)
        let live = below + mine
        for id in live {
            guard let task = held(id) else { continue }
            // `why` is what tells this row apart from a task somebody cancelled on purpose: this
            // one did nothing, its root left.
            RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "root_ended",
                                                     "root": session.id])
            cancelInPlace(task)
        }
        var closed: [String] = []
        for id in lingeringBelow + lingering where closeChildTab(ofTask: id, root: session.id) {
            closed.append(id)
        }
        if !closed.isEmpty {
            save()
            RemoteServer.shared.broadcastOrchestrator()
        }
        return live + closed
    }

    /// End a finished task's linger now, leaving the record exactly as it stands. True when the
    /// tab was still ours to take.
    ///
    /// Deliberately not `cancelInPlace`: that writes `cancelled` over the record, and a task that
    /// succeeded still succeeded — the result file is there and worth keeping. What the root's
    /// leaving takes away is the reason to hold the tab open, not the work.
    ///
    /// The courtesy is `closeChild`'s: a quit word when there is an assistant in there to hear
    /// it, the tab alone when the child is mid-turn or has already left. What it does not inherit
    /// is the ten minutes of patience — that is for a linger running out on its own, and this is
    /// somebody pressing close.
    private static func closeChildTab(ofTask id: String, root: String) -> Bool {
        guard var task = held(id), let childID = task.childTerminalId,
              let child = target(withID: childID),
              child.assistant == nil || child.assistant == task.assistant,
              task.childTTY == nil || child.tty == task.childTTY else { return false }
        let justTheTab = childIsBusy(child) || child.assistant == nil
        task.closeAt = nil
        guard replaceTask(task, expecting: task.state) else { return false }
        RemoteAuth.audit("orchestrator.close", ["task": id, "child": childID,
                                                "how": justTheTab ? "tab" : "exit",
                                                "why": "root_ended", "root": root])
        if let failure = endChildTab(child, justTheTab: justTheTab) {
            Log.write("orchestrator: could not close the child — \(failure)")
        }
        return true
    }

    /// Whether there is a turn running in the child, read the way `closeChild` reads it.
    ///
    /// The screen reading is left where it is called from — it is the slow half, and neither
    /// caller is on the main thread by accident — but the states table belongs to main, so that
    /// half takes the same hop `target(withID:)` takes.
    private static func childIsBusy(_ child: TargetSession) -> Bool {
        if Targets.isChoosing(child) { return true }
        if Thread.isMainThread {
            if case .working? = SessionWatch.shared.states[child.id] { return true }
            return false
        }
        return DispatchQueue.main.sync { () -> Bool in
            if case .working? = SessionWatch.shared.states[child.id] { return true }
            return false
        }
    }

    /// What a session calls itself, which is what `rootSessionId` was written from — the terminal
    /// id is this app's name for a tab and means nothing to the assistant inside it. Same
    /// main-thread hop as `target(withID:)`, because that is where the notes are reloaded.
    private static func rootIdentity(of session: TargetSession) -> String? {
        if Thread.isMainThread { return HookBridge.note(for: session)?.session }
        return DispatchQueue.main.sync { HookBridge.note(for: session)?.session }
    }

    // MARK: - Lifecycle: the beat

    private static var timer: Timer?
    private static var cleanupTimer: Timer?

    /// Wired once at launch, alongside the other observers.
    static func start() {
        load()
        // Minted now rather than on first use: the skill tells a root to read this file before
        // its first dispatch, and a file that appears only after a request nobody can make yet
        // is a door that opens from the inside.
        _ = dispatchToken()
        resumeAfterRestart()
        cleanup()
        SessionWatch.shared.observers["orchestrator"] = { beat(fromTimer: false) }
        let t = Timer(timeInterval: 5, repeats: true) { _ in beat(fromTimer: true) }
        RunLoop.main.add(t, forMode: .common)
        timer = t
        let c = Timer(timeInterval: 6 * 3600, repeats: true) { _ in cleanup() }
        RunLoop.main.add(c, forMode: .common)
        cleanupTimer = c
    }

    /// Recover waiters and fail tasks whose pre-briefing secret died with the previous process.
    /// Separate from timer wiring so the restart handoff can be exercised without opening a live
    /// app lifecycle in the unit suite.
    static func resumeAfterRestart() {
        load()
        // Anything mid-way through briefing is unbriefable now: its plaintext secret died with
        // the process. A serialized task that never left queued is different — its temporary
        // sealed copy can be opened with this installation's at-rest key and pumped below.
        var orphaned: [String] = []
        var recovered: [String: String] = [:]
        lock.lock()
        let restartRows = tasks.values.filter { $0.state == .queued || $0.state == .spawning }
        lock.unlock()
        for task in restartRows where task.state == .queued && !task.serialize.isEmpty {
            if let sealed = task.queuedSecret, let secret = openQueuedSecret(sealed),
               RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) {
                recovered[task.id] = secret
            }
        }
        lock.lock()
        for (id, task) in tasks where task.state == .queued || task.state == .spawning {
            if task.state == .queued, let secret = recovered[id] {
                secrets[id] = secret
                continue
            }
            var dead = task
            dead.state = .spawnFailed
            dead.summary = task.state == .queued && !task.serialize.isEmpty
                ? "The app restarted but could not recover the queued task secret."
                : "The app restarted before the child was briefed."
            dead.finishedAt = Date()
            dead.queuedSecret = nil
            tasks[id] = dead
            orphaned.append(id)
        }
        lock.unlock()
        let rearmed = rearmLingers()
        if !orphaned.isEmpty || rearmed { save() }
        scheduleSerializePump()
    }

    /// How long a restored linger waits before this process is willing to act on it.
    ///
    /// Long enough for the first reading to land, and no longer. `SessionWatch` takes one the
    /// moment it starts, but it is a round trip to every terminal and the app may well have
    /// opened in the background, where the cadence is twenty seconds. A deadline that already
    /// ran out while the app was away would otherwise be judged against an empty list — and
    /// ``closeStep`` now waits on one of those anyway, so this is the belt to that pair of braces.
    /// It also keeps the ten-minute patience for a busy child measured from *this* process
    /// rather than from a deadline that expired overnight.
    static let restartGrace: TimeInterval = 20

    /// Carry each reported child's linger across the restart that interrupted it.
    ///
    /// Only the deadline is restored, and only where a tab was named: whether that tab is still
    /// the child's is a question for a reading this process took. A deadline already spent gets
    /// ``restartGrace`` rather than being acted on the instant the app opens.
    @discardableResult
    private static func rearmLingers() -> Bool {
        let linger = Config.shared.orchestratorChildLinger
        let floor = Date().addingTimeInterval(restartGrace)
        var changed = false
        lock.lock()
        for (id, task) in tasks where task.closeAt != nil {
            var carried = task
            // A tab with nothing named to close it, or a Mac that has since said tabs are never
            // to be closed for a child: the deadline is not this process's to keep.
            if task.childTerminalId == nil || linger < 0 {
                carried.closeAt = nil
            } else if let at = task.closeAt, at < floor {
                carried.closeAt = floor
            } else {
                continue
            }
            tasks[id] = carried
            changed = true
        }
        lock.unlock()
        return changed
    }

    /// Main thread. Advances every live task one step; cheap when nothing is live.
    ///
    /// The observer only fires when a reading *changed*, and a hung child looks exactly like a
    /// quiet one — so the timer path exists to notice timeouts and a `result.json` that appeared
    /// without the screen moving. Only the timer path asks for fresh readings, so an observer
    /// firing cannot ask for the reading that fires it.
    static func beat(fromTimer: Bool) {
        // Two walkers at once is the shape a task once failed in: one of them had copied a record
        // before the other advanced it, and acted on that copy afterwards. Every caller reachable
        // from here is on the main thread, which was taken to mean the overlap was impossible —
        // it is not, and this counter is what proved it. `Process.waitUntilExit()` polls the run
        // loop, so typing into a terminal from here let the five-second timer fire *inside* the
        // walk. `waitQuietly()` is the fix and lives in ``Subprocess``; this stays because a
        // second cause would look exactly like the first one did, and silence is not evidence.
        //
        // It counts rather than blocks, and that is still deliberate. A dropped walker leaves
        // nothing to read, and the record cannot be damaged by a stale copy anyway: `replaceTask`
        // refuses to move a task backwards.
        lock.lock()
        beatSequence += 1
        let sequence = beatSequence
        let overlapping = beatsInFlight > 0
        beatsInFlight += 1
        let liveIDs = tasks.values
            .filter { !$0.state.isTerminal || $0.closeAt != nil }
            .map(\.id)
        lock.unlock()
        defer {
            lock.lock(); beatsInFlight -= 1; lock.unlock()
        }
        if overlapping {
            RemoteAuth.audit("orchestrator.beat_overlap",
                             ["beat": String(sequence),
                              "from": fromTimer ? "timer" : "reading",
                              "main": String(Thread.isMainThread),
                              "thread": Thread.current.description])
        }
        // Before the early return on purpose. With `orchestratorChildLinger` set to never keep a
        // tab, the last task of a fan-out leaves nothing in `liveIDs` at all — and a batch that
        // announces only while something is still on the list is one that never announces.
        sweepBatches()

        guard !liveIDs.isEmpty else { return }

        var changed = false
        var sawSpawning = false
        for id in liveIDs {
            // The list is only scheduling. State is read at the instant this task is advanced, so
            // an earlier item in a long beat cannot leave a stale state decision behind it.
            guard let task = held(id), !task.state.isTerminal || task.closeAt != nil else { continue }
            switch task.state {
            case .spawning:
                sawSpawning = true
                changed = brief(task) || changed
            case .briefed:  changed = watch(task) || changed
            default: changed = closeChild(task) || changed
            }
        }
        if fromTimer, sawSpawning {
            // Away from the panel the watch reads every twenty seconds, which is a long time to
            // leave a freshly opened terminal unbriefed.
            SessionWatch.shared.nudge()
        }
        if changed {
            save()
            RemoteServer.shared.broadcastOrchestrator()
        }
    }

    /// How long a tab has to reach a prompt before the task gives up on it.
    ///
    /// Four minutes, and the number is set by the worst case rather than the usual one. A single
    /// assistant starting in a warm project reaches its composer in a few seconds; four of them
    /// starting at once — which is exactly what a two-level dispatch does — are competing for the
    /// same CPU, the same disk, and a status line that may be shelling out to git or to a CI API
    /// before it paints. Two minutes was enough for one and not for four, and the failure it
    /// produced was indistinguishable from a child that was never going to come up.
    ///
    /// The cost of being generous is a row that says `spawning` for longer before admitting
    /// defeat. The cost of being tight is work that silently did not happen, which is worse.
    static let readyLimit: TimeInterval = 240

    /// Try to put the first message in front of a child that has just opened. True when the task
    /// record changed.
    private static func brief(_ snapshot: Task) -> Bool {
        // A snapshot only nominates an id. Never act on its state or fields after another writer
        // may have advanced the record.
        guard var task = held(snapshot.id), task.state == .spawning else { return false }
        guard let spawnedAt = task.spawnedAt else { return false }
        if Date().timeIntervalSince(spawnedAt) > readyLimit {
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .spawnFailed,
                     summary: "The child session did not reach a prompt within "
                            + "\(Int(readyLimit / 60)) minutes. If several sessions were starting "
                            + "at once, they were competing for this Mac.")
            return false // finalize saved and broadcast already
        }
        guard let childID = task.childTerminalId,
              let child = SessionWatch.shared.targets.first(where: { $0.id == childID }),
              child.assistant == task.assistant else { return false }
        if task.childTTY == nil {
            task.childTTY = child.tty
            guard replaceTask(task, expecting: .spawning) else { return false }
        }
        let changed = noteChildIdentity(child, in: &task)
        let screen = Targets.capture(child)
        // A brand-new session in a directory the assistant has not been told to trust opens on a
        // dialog, and text sent into a dialog confirms whatever is highlighted. The root asked
        // for work in exactly this directory, so the default answer is taken — once, and written
        // down.
        if let screen, SessionState.isChoosing(screen, assistant: task.assistant) {
            if !task.answeredMenu {
                task.answeredMenu = true
                guard replaceTask(task, expecting: .spawning) else { return false }
                _ = Targets.answer(0x31, to: child)
                RemoteAuth.audit("orchestrator.menu", ["task": task.id, "answer": "1"])
            }
            return true
        }

        let transcript: String?
        if let path = task.transcriptPath {
            transcript = try? String(contentsOfFile: path, encoding: .utf8)
        } else {
            transcript = nil
        }
        let elapsed = task.lastInjectAt.map { Date().timeIntervalSince($0) }
        let decision = briefingDecision(screen: screen, assistant: task.assistant,
                                        transcript: transcript,
                                        transcriptKnown: task.transcriptPath != nil,
                                        taskID: task.id, attempts: task.injectAttempts,
                                        secondsSinceAttempt: elapsed)
        if decision == .accepted {
            // Acceptance means this exact transcript contained the delivered task marker. Record
            // the receipt before crossing the state boundary so this pair becomes pinned.
            task.transcriptProven = task.transcriptPath != nil
            task.state = .briefed
            task.briefedAt = task.lastInjectAt ?? Date()
            task.lastSeenChild = Date()
            guard replaceTask(task, expecting: .spawning, discardSecret: true) else { return false }
            RemoteAuth.audit("orchestrator.brief", ["task": task.id,
                                                      "child": task.childTerminalId ?? "?",
                                                      "attempts": String(task.injectAttempts)])
            return true
        }
        if decision == .exhausted {
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .spawnFailed,
                     summary: "The child did not record the briefing after \(briefingAttemptLimit) attempts.")
            return false // finalize saved and broadcast already
        }
        guard decision == .send else {
            if changed, !replaceTask(task, expecting: .spawning) { return false }
            return changed
        }
        guard let secret = heldSecret(task.id) else {
            // Secret absence is an error only while the current record is still awaiting briefing;
            // a stale walker arriving after acceptance has nothing left to do.
            guard held(task.id)?.state == .spawning else { return false }
            finalize(task.id, as: .spawnFailed, summary: "The task's secret was lost before briefing.")
            return false
        }
        writeChildBrief(for: task)
        let line = firstLine(id: task.id, secret: secret, announce: L.t.childAnnounce(task.title))
        task.injectAttempts += 1
        task.lastInjectAt = Date()
        if let failure = Targets.send(line, to: child) {
            // A reported transport failure did not enter the receipt window; the next beat may
            // try again immediately, still under the same total-attempt ceiling.
            task.lastInjectAt = nil
            if task.injectAttempts >= briefingAttemptLimit {
                guard replaceTask(task, expecting: .spawning) else { return false }
                finalize(task.id, as: .spawnFailed, summary: "Could not type into the child: \(failure)")
                return false
            }
            guard replaceTask(task, expecting: .spawning) else { return false }
            return true
        }
        // `Targets.send` proves only that bytes reached the tty. Keep the secret and remain in
        // `spawning` until the assistant's own transcript proves those bytes became a user turn.
        guard replaceTask(task, expecting: .spawning) else { return false }
        RemoteAuth.audit("orchestrator.brief.inject", ["task": task.id,
                                                        "child": task.childTerminalId ?? "?",
                                                        "attempt": String(task.injectAttempts)])
        return true
    }

    /// Watch a briefed child for its result, its death, or its deadline. True when the record
    /// changed in a way worth persisting.
    private static func watch(_ task: Task) -> Bool {
        var task = task

        // The result file is the completion signal a sandboxed child can always give — writing
        // to /tmp needs no network approval, where a curl to loopback does.
        if let result = readResult(of: task) {
            guard replaceTask(task, expecting: .briefed) else { return false }
            finalize(task.id, as: result.status == "success" ? .success : .failure,
                     summary: result.summary, artifacts: result.artifacts)
            return false
        }

        if let briefedAt = task.briefedAt,
           Date().timeIntervalSince(briefedAt) > Double(task.timeoutMinutes) * 60 {
            guard replaceTask(task, expecting: .briefed) else { return false }
            finalize(task.id, as: .timeout,
                     summary: "No result within \(task.timeoutMinutes) minutes.")
            return false
        }

        var changed = false
        if let childID = task.childTerminalId,
           let child = SessionWatch.shared.targets.first(where: { $0.id == childID }) {
            task.lastSeenChild = Date()
            // The child's own identity, once its assistant has written it down. Free for Claude
            // (a dictionary the hooks fill), one cached lsof for Codex.
            changed = noteChildIdentity(child, in: &task) || changed
            if !replaceTask(task, expecting: .briefed) { return false }
        } else if let seen = task.lastSeenChild {
            if Date().timeIntervalSince(seen) > 60 {
                guard replaceTask(task, expecting: .briefed) else { return false }
                finalize(task.id, as: .failure,
                         summary: "The child session ended without reporting a result.")
                return false
            }
        } else {
            task.lastSeenChild = Date()
            if !replaceTask(task, expecting: .briefed) { return false }
        }
        return changed
    }

    /// Fill in the assistant's own durable identity as soon as it exists. Briefing and watching
    /// share this because the transcript is now the boundary between those two states.
    private static func noteChildIdentity(_ child: TargetSession, in task: inout Task) -> Bool {
        var changed = false
        // Claude writes a process registry, so its pid pair and registry supply identity before
        // delivery pins the pair. Codex writes no registry, so it deliberately uses lsof to name
        // a rollout and its task marker to prove delivery. In either branch the marker is a
        // delivery receipt, not proof that two different processes share an identity.
        switch task.assistant {
        case .claude:
            let observedPID = Targets.pid(of: child)
            let observedStart = observedPID.flatMap { Targets.processStart(ofPID: $0) }
            let registrySessionID = SessionRegistry.sessionID(of: child)
            let registryTranscript = registrySessionID.flatMap { sessionID in
                // Match Transcript.record(of:): the id names a candidate, while locate proves
                // its file exists in the child's cwd and postdates this process.
                Transcript.locate(cwd: Targets.workingDirectory(of: child) ?? cwd(of: task),
                                  tabTitle: child.name, startedAt: task.spawnedAt,
                                  sessionID: sessionID)
            }
            let observation = ChildObservation(pid: observedPID, procStart: observedStart,
                                               registrySessionID: registrySessionID,
                                               registryTranscript: registryTranscript)
            changed = recordProcessIdentity(from: observation, in: &task) || changed
            let step = identityStep(for: task, seeing: observation)
            if case let .refuseForeignProcess(seen) = step {
                RemoteAuth.audit("orchestrator.identity.foreign", [
                    "task": task.id,
                    "recorded_pid": task.childPID.map(String.init) ?? "?",
                    "seen_pid": seen.map(String.init) ?? "?",
                    "recorded_proc_start": task.childProcStart
                        .map { String($0.timeIntervalSince1970) } ?? "?",
                    "seen_proc_start": observation.procStart
                        .map { String($0.timeIntervalSince1970) } ?? "?",
                ])
                return false
            }
            switch step {
            case let .useRegistry(sessionID, transcript):
                // Keep the old ladder as a transition control, but run it only once per distinct
                // answer. A growing transcript otherwise defeats its signature cache every beat.
                if beginRegistryControl(for: sessionID, in: &task) {
                    var legacyTask = task
                    legacyTask.childSessionId = nil
                    legacyTask.transcriptPath = nil
                    legacyTask.transcriptProven = false
                    _ = noteClaudeIdentityFromLegacySources(child, in: &legacyTask)
                    changed = true
                    if let comparison = identityComparison(registrySessionID: sessionID,
                                                           registryTranscript: transcript,
                                                           legacyTask: legacyTask) {
                        RemoteAuth.audit("orchestrator.identity.mismatch", [
                            "task": task.id,
                            "registry_session": comparison.registrySessionID,
                            "registry_transcript": comparison.registryTranscriptPath,
                            "legacy_session": comparison.legacySessionID ?? "?",
                            "legacy_transcript": comparison.legacyTranscriptPath ?? "?",
                        ])
                    } else {
                        RemoteAuth.audit("orchestrator.identity.agreement", [
                            "task": task.id,
                            "session": sessionID,
                        ])
                    }
                }
                changed = adoptRegistryIdentity(sessionID: sessionID, transcript: transcript,
                                                in: &task) || changed
                changed = noteTranscriptProof(in: &task) || changed
            case .none:
                // A missing or not-yet-verifiable registry answer is only a source miss. The
                // complete note + locate ladder remains the fallback and decides normally.
                changed = noteClaudeIdentityFromLegacySources(child, in: &task) || changed
            case .refuseForeignProcess:
                break
            }
        case .codex:
            if task.transcriptPath == nil,
               let rollout = Codex.locate(cwd: cwd(of: task), startedAt: task.spawnedAt,
                                           pid: Targets.pid(of: child)) {
                task.transcriptPath = rollout.path
                task.transcriptProven = false
                changed = true
            }
            if task.childSessionId == nil, let path = task.transcriptPath,
               let threadID = Codex.head(of: URL(fileURLWithPath: path))?.id {
                task.childSessionId = threadID
                changed = true
                // The thread gets the task's name too, so `codex resume` lists it by what it did
                // rather than by the first line it was handed.
                if !threadID.isEmpty {
                    CodexNaming.shared.name(task.title, thread: threadID, target: child)
                }
            }
            changed = noteTranscriptProof(in: &task) || changed
        }
        return changed
    }

    /// The complete pre-registry Claude identity ladder. A registry miss runs it normally; a new
    /// registry answer runs an independent copy once so disagreements leave an audit receipt.
    private static func noteClaudeIdentityFromLegacySources(_ child: TargetSession,
                                                            in task: inout Task) -> Bool {
        var changed = noteTranscriptProof(in: &task)
        if let noted = childSessionID(from: HookBridge.note(for: child),
                                      spawnedAt: task.spawnedAt) {
            changed = adoptHookIdentity(sessionID: noted, in: &task) || changed
        }
        // Without an id, timestamps and titles only rank candidates, so the delivered task
        // marker must prove the winner. A current hook instead names the exact file needed to
        // verify delivery before that marker has appeared; proof is recorded once it does.
        let accepting: ((URL) -> Bool)?
        if task.childSessionId == nil {
            let taskID = task.id
            accepting = { url in
                transcriptBelongsToTask(url, assistant: .claude, taskID: taskID)
            }
        } else {
            accepting = nil
        }
        if task.transcriptPath == nil,
           let found = Transcript.locate(cwd: cwd(of: task), tabTitle: child.name,
                                         startedAt: task.spawnedAt,
                                         sessionID: task.childSessionId,
                                         accepting: accepting) {
            task.transcriptPath = found.path
            task.childSessionId = found.deletingPathExtension().lastPathComponent
            task.transcriptProven = accepting != nil
            changed = true
        }
        changed = noteTranscriptProof(in: &task) || changed
        return changed
    }

    /// Promote a candidate path only after its immutable first user turn proves ownership.
    /// Every result is cached while the file signature is unchanged; growth after delivery
    /// naturally causes the pre-briefing candidate to be read and proved again.
    static func noteTranscriptProof(in task: inout Task) -> Bool {
        guard !task.transcriptProven, let path = task.transcriptPath else { return false }
        let transcript = URL(fileURLWithPath: path)
        let ownership = transcriptOwnership(transcript, assistant: task.assistant, taskID: task.id)
        return applyTranscriptOwnership(ownership, transcript: transcript, to: &task)
    }

    /// Consume the three-state read separately from doing it, so restoration policy can be
    /// exercised without depending on a polling beat or a live terminal.
    static func applyTranscriptOwnership(_ ownership: TranscriptOwnership, transcript: URL,
                                         to task: inout Task) -> Bool {
        switch ownership {
        case .unavailable:
            return false
        case .other:
            // Before delivery, another first turn merely means the nominated file was observed
            // too early; clearing it here would also erase the exact path needed to verify and
            // retry this child's briefing. Once briefed, Codex still needs this disproof to clear
            // an lsof rollout from another thread because it has no process registry to replace it.
            guard task.state == .briefed else { return false }
            task.childSessionId = nil
            task.transcriptPath = nil
            task.transcriptProven = false
            return true
        case .belongs:
            break
        }
        switch task.assistant {
        case .claude:
            task.childSessionId = transcript.deletingPathExtension().lastPathComponent
        case .codex:
            guard let id = Codex.head(of: transcript)?.id else { return false }
            task.childSessionId = id
        }
        task.transcriptProven = true
        return true
    }

    /// A tty belongs to the tab, not to the assistant process inside it. Until the new process
    /// emits a hook, the note keyed by that tty can still identify the session that used the tab
    /// before this task was spawned.
    static func childSessionID(from note: HookBridge.Note?, spawnedAt: Date?) -> String? {
        // The hook writes integer epoch seconds while `Date()` retains subsecond precision. `>=`
        // deliberately accepts the exact-second boundary; a genuinely later note truncated to
        // just before `spawnedAt` is rejected safely until a subsequent hook refreshes it.
        guard let note, let spawnedAt, note.at >= spawnedAt else { return nil }
        return note.session
    }

    /// What to do about a finished child's tab, at one instant.
    ///
    /// Split out from the beat that runs it for the same reason ``Targets/Farewell/step(elapsed:pid:termed:killed:)``
    /// is: the decisions are here, the terminal is there, and a decision that can only be
    /// exercised by closing somebody's real tab is a decision with no tests. Every branch below
    /// is one, and one of them is irreversible — see `.forget`.
    enum CloseStep: Equatable {
        /// Not yet. The deadline stands and the next beat asks again.
        case wait
        /// That tab has gone, or belongs to somebody else now. Drop the deadline: nothing here
        /// is ours to close, and nothing will be.
        case forget
        /// Take it. `justTheTab` when there is nobody in there to say the quit word to.
        case close(justTheTab: Bool)
    }

    /// `busy` is a closure because answering it costs a screen capture, and it is only worth
    /// paying for once everything cheaper has already said the tab is ours and due.
    static func closeStep(now: Date, closeAt: Date, sawTerminals: Bool, child: TargetSession?,
                          assistant: Assistant, tty: String?,
                          busy: () -> Bool) -> CloseStep {
        guard now >= closeAt else { return .wait }
        guard let child else {
            // **`forget` is permanent**, and a reading with no terminals in it at all is not a
            // reading that found this one gone: it is the first seconds after launch, or iTerm2
            // not answering. Deciding on one closed nothing and left the tab standing for good.
            return sawTerminals ? .forget : .wait
        }
        guard child.assistant == nil || child.assistant == assistant,
              tty == nil || child.tty == tty else { return .forget }
        // A child still mid-turn is left alone — result.json was meant to be the last thing it
        // wrote, but a tab closed under a running turn is a mess, not an exit. Ten minutes of
        // patience, then the tab goes without the courtesy of `/exit`, because typing into a
        // menu confirms whatever is highlighted. An assistant that already left on its own
        // gets the same treatment: there is nobody in that tab to say the word to.
        let busyNow = busy()
        let overdue = now.timeIntervalSince(closeAt) > 600
        if busyNow, !overdue { return .wait }
        return .close(justTheTab: busyNow || child.assistant == nil)
    }

    /// Close a reported child's terminal once its linger has run out. True when the record changed.
    private static func closeChild(_ task: Task) -> Bool {
        var task = task
        guard let closeAt = task.closeAt, let childID = task.childTerminalId else { return false }
        let now = Date()
        let seen = SessionWatch.shared.targets
        let child = seen.first { $0.id == childID }
        let step = closeStep(now: now, closeAt: closeAt, sawTerminals: !seen.isEmpty,
                             child: child, assistant: task.assistant, tty: task.childTTY,
                             busy: { child.map(childIsBusy) ?? false })
        switch step {
        case .wait:
            return false
        case .forget:
            task.closeAt = nil
            guard replaceTask(task, expecting: task.state) else { return false }
            Log.write("orchestrator: nothing left to close for \(task.id) — dropping its linger")
            return true
        case .close(let justTheTab):
            guard let child else { return false }
            return takeChildTab(child, justTheTab: justTheTab, for: task)
        }
    }

    /// The half of `closeChild` that touches a terminal, once the decision is made.
    private static func takeChildTab(_ child: TargetSession, justTheTab: Bool,
                                     for task: Task) -> Bool {
        var task = task
        task.closeAt = nil
        guard replaceTask(task, expecting: task.state) else { return false }
        RemoteAuth.audit("orchestrator.close", ["task": task.id, "child": child.id,
                                                 "how": justTheTab ? "tab" : "exit"])
        // Off the main thread: `end` types the quit word, waits for it to land, then closes the
        // tab, and a second of that on the main thread is a second the panel does not draw.
        DispatchQueue.global(qos: .utility).async {
            if let failure = endChildTab(child, justTheTab: justTheTab) {
                Log.write("orchestrator: could not close the child — \(failure)")
            }
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
        }
        return true
    }

    /// Take a child's tab away: the quit word first when there is an assistant in there to hear
    /// it, otherwise the tab on its own.
    ///
    /// Shared by the linger and by the cascade so the two cannot drift into closing a tab two
    /// different ways. Blocks while the child leaves — `Targets.end` types the word and waits for
    /// the process to go rather than assuming it did — so both callers are somewhere that can
    /// afford a wait measured in seconds when the child is busy.
    private static func endChildTab(_ child: TargetSession, justTheTab: Bool) -> String? {
        if justTheTab {
            switch child.backend {
            case .iterm: return ITerm.close(child.id)
            case .tmux:  return Tmux.close(child.id)
            }
        }
        return Targets.end(child)
    }

    private struct ChildResult {
        var status: String
        var summary: String?
        var artifacts: [String]
    }

    /// The child's `result.json`, if it exists and can prove it came from the child.
    private static func readResult(of task: Task) -> ChildResult? {
        let file = task.dir.appendingPathComponent("result.json")
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return nil
        }
        guard let secret = obj["task_secret"] as? String,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            // Somebody wrote a result they could not have been asked for. Once in the log is
            // enough — the file is left alone so the evidence is where the log says it is.
            if !badResults.contains(task.id) {
                badResults.insert(task.id)
                RemoteAuth.audit("orchestrator.result", ["task": task.id, "ok": "0", "why": "bad_secret"])
            }
            return nil
        }
        return ChildResult(status: obj["status"] as? String ?? "failure",
                           summary: (obj["summary"] as? String).map { String($0.prefix(2000)) },
                           artifacts: (obj["artifacts"] as? [String] ?? []).map { String($0.prefix(300)) })
    }

    private static var badResults: Set<String> = []

    // MARK: - Finalize

    /// Main thread. The one place a task ends, whatever ended it.
    static func finalize(_ taskID: String, as outcome: State,
                         summary: String?, artifacts: [String] = [],
                         pumpQueue: Bool = true) {
        lock.lock()
        guard var task = tasks[taskID], !task.state.isTerminal else { lock.unlock(); return }
        task.state = outcome
        task.finishedAt = Date()
        task.queuedSecret = nil
        if let summary { task.summary = summary }
        if !artifacts.isEmpty { task.artifacts = artifacts }
        secrets.removeValue(forKey: taskID)
        // Only a child that reported gets its tab held open and then closed for it. One that
        // timed out has something on its screen worth reading, and stays.
        let linger = Config.shared.orchestratorChildLinger
        if outcome == .success || outcome == .failure, linger >= 0, task.childTerminalId != nil {
            task.closeAt = Date().addingTimeInterval(TimeInterval(linger))
        }
        // A spawn that never reached briefing is the exception, and it goes now rather than
        // staying. There is nothing of this task on that screen — the session was opened and
        // never spoken to, so what is on it is a fresh prompt, which explains nothing that the
        // summary does not say better.
        //
        // **And leaving it is not free.** Each one is a live assistant holding a slot, and the
        // usual cause of failing to reach a prompt is that too many sessions were starting at
        // once. Keep them and the next spawn is slower for exactly the reason the last one
        // failed, which is a failure that feeds itself: four dead tabs were still running when
        // this was written, and the two spawns after them timed out too.
        if outcome == .spawnFailed, task.briefedAt == nil, linger >= 0,
           task.childTerminalId != nil {
            task.closeAt = Date()
        }
        tasks[taskID] = task
        lock.unlock()

        // The result file can carry words the finalizer was not handed — the HTTP route sends
        // only a sentence, the file has the artifact list too.
        if task.summary == nil || task.artifacts.isEmpty,
           let result = readResult(of: task) {
            lock.lock()
            if task.summary == nil { task.summary = result.summary }
            if task.artifacts.isEmpty { task.artifacts = result.artifacts }
            tasks[taskID] = task
            lock.unlock()
        }

        if let usage = harvestUsage(task) {
            lock.lock()
            task.usage = usage
            tasks[taskID] = task
            lock.unlock()
        }
        save()
        RemoteAuth.audit("orchestrator.finish", ["task": taskID, "state": outcome.rawValue])
        RemoteServer.shared.broadcastOrchestrator()
        notifyRoot(task)
        noteEnded(task)
        endWorkHandedOnBy(task)
        if pumpQueue { scheduleSerializePump() }
    }

    /// A finished task takes whatever it handed on with it.
    ///
    /// The briefing tells a child to wait for its own children before reporting, and a child that
    /// followed it leaves nothing here to do. This is for the other endings: a `timeout`, a
    /// `failure`, a child that reported early. What those leave behind is a grandchild still
    /// running for a session that no longer exists — nobody is waiting for its answer, nobody is
    /// watching its tab, and on the list it sits at the top level with a `Child` chip and no row
    /// above it, which is the shape somebody reported as a bug in the grouping.
    ///
    /// Queued descendants are finalized on main before the serialize pump can open them. Running
    /// descendants move off the main thread because `cancelInPlace` types a quit word and waits
    /// for the tab to actually go.
    private static func endWorkHandedOnBy(_ task: Task) {
        let below = liveTasks(under: [task.id])
        guard !below.isEmpty else { return }
        var running: [String] = []
        for id in below {
            guard let child = held(id) else { continue }
            if child.state == .queued {
                RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_finished",
                                                         "parent": task.id,
                                                         "parentState": task.state.rawValue])
                finalize(id, as: .cancelled, summary: "Cancelled because its parent finished.",
                         pumpQueue: false)
            } else {
                running.append(id)
            }
        }
        guard !running.isEmpty else { return }
        DispatchQueue.global(qos: .utility).async {
            for id in running {
                guard let child = held(id) else { continue }
                RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_finished",
                                                         "parent": task.id,
                                                         "parentState": task.state.rawValue])
                cancelInPlace(child)
            }
        }
    }

    /// One line typed into whatever is waiting for this task, so the conversation that asked for
    /// the work is the conversation that hears it finished.
    ///
    /// **Two readers, told different things.** A root is a conversation with a person behind it,
    /// and what it needs is enough to go and look: the id, the state, the file. A child is a
    /// program on a deadline with siblings still running, and it has exactly one question — may I
    /// write my own result.json yet — so it is told the answer to that instead.
    ///
    /// **Finding the reader only ever worked one level down, and that was a bug rather than a
    /// design.** A root writes `root.session_id` into the task it dispatches, so a depth-1 task
    /// can be traced back to a tab through the hook notes. The briefing tells a child to dispatch
    /// with `root.parent_task` and nothing else — deliberately, because a Codex child has no hook
    /// note to be found by — so at depth 2 this guard failed on the first line and the line was
    /// dropped in silence. What was left was the polling loop the briefing prescribes, which is a
    /// child spending turns on `sleep`. The parent task's own terminal is the answer, and
    /// ``record(of:)`` had been resolving it that way all along.
    static func timeoutClaimNotice(for task: Task) -> String {
        guard task.state == .timeout, !task.claims.isEmpty else { return "" }
        return " — claims released; child tab may still be writing"
    }

    private static func notifyRoot(_ task: Task) {
        guard Config.shared.orchestratorNotifyRoot else { return }
        let short = String(task.id.prefix(8))
        let file = "read /tmp/.clawdline/\(task.id)/result.json"

        if let parentID = task.parentTaskId, let parent = held(parentID),
           !parent.state.isTerminal, let terminalID = parent.childTerminalId,
           let terminal = target(withID: terminalID) {
            // Words into a menu confirm the highlighted row instead of typing.
            guard !Targets.isChoosing(terminal) else { return }
            let outstanding = liveTasks(under: [parentID]).count
            let rest = outstanding == 0
                ? "nothing else you handed on is still running"
                : "\(outstanding) more of yours still running"
            let line = "[clawdline] your task \(short) (\(task.title)) finished:"
                + " \(task.state.rawValue) — \(file) — \(rest)"
                + timeoutClaimNotice(for: task)
            if let failure = Targets.send(line, to: terminal) {
                Log.write("orchestrator: could not notify the parent task — \(failure)")
            }
            return
        }

        guard let rootID = task.rootSessionId else { return }
        guard let root = SessionWatch.shared.targets.first(where: {
            HookBridge.note(for: $0)?.session == rootID
        }) else { return }
        // Words into a menu confirm the highlighted row instead of typing. Skip rather than risk
        // answering a question on the root's behalf; the record is still in the app and the page.
        guard !Targets.isChoosing(root) else { return }
        let line = "[clawdline] task \(short) (\(task.title)) finished:"
            + " \(task.state.rawValue) — \(file)" + timeoutClaimNotice(for: task)
        if let failure = Targets.send(line, to: root) {
            Log.write("orchestrator: could not notify the root — \(failure)")
        }
    }

    /// Tell both roots that two live tasks can touch the same directory. Advisory and one-shot:
    /// like completion notification, a missing root, a menu, or a failed send leaves the API
    /// record as the durable answer and never changes the dispatch outcome.
    private static func notifyWorkspaceOverlaps(newTask: Task,
                                                overlaps: [WorkspaceOverlap]) {
        workspaceOverlapObserverForTesting?(newTask, overlaps)
        guard Config.shared.orchestratorNotifyRoot else { return }
        let notices = workspaceOverlapNotices(newTask: newTask, overlaps: overlaps)
        guard !notices.isEmpty else { return }
        DispatchQueue.global(qos: .utility).async {
            for notice in notices {
                sendWorkspaceOverlap(notice.line, toRootSession: notice.rootSessionID,
                                     taskID: notice.taskID)
            }
        }
    }

    /// The pure notification decision. The new task gets one aggregate line; each identifiable
    /// opposing root gets the one overlap that concerns it. Nil root ids produce no delivery.
    static func workspaceOverlapNotices(newTask: Task,
                                        overlaps: [WorkspaceOverlap]) -> [WorkspaceOverlapNotice] {
        guard !overlaps.isEmpty else { return [] }
        var notices: [WorkspaceOverlapNotice] = []
        if let root = newTask.rootSessionId {
            let details = overlaps.map { overlap in
                let other = overlap.task
                return "task \(other.id.prefix(8)) (\(other.title)) at \(overlap.sharedDir)"
            }.joined(separator: "; ")
            let line = "[clawdline] workspace overlap: task \(newTask.id.prefix(8)) "
                + "(\(newTask.title)) overlaps \(overlaps.count) active "
                + (overlaps.count == 1 ? "task: " : "tasks: ") + details
            notices.append(WorkspaceOverlapNotice(rootSessionID: root, taskID: newTask.id,
                                                   line: line))
        }
        for overlap in overlaps {
            let other = overlap.task
            guard let root = other.rootSessionId else { continue }
            let line = "[clawdline] workspace overlap: task \(newTask.id.prefix(8)) "
                + "(\(newTask.title)) and task \(other.id.prefix(8)) (\(other.title)) "
                + "share \(overlap.sharedDir)"
            notices.append(WorkspaceOverlapNotice(rootSessionID: root, taskID: other.id,
                                                   line: line))
        }
        return notices
    }

    private static func sendWorkspaceOverlap(_ line: String, toRootSession sessionID: String,
                                             taskID: String) {
        guard let root = target(forRootSession: sessionID),
              !Targets.isChoosing(root) else { return }
        if let failure = Targets.send(line, to: root) {
            Log.write("orchestrator: could not notify task \(taskID) root of workspace overlap"
                      + " — \(failure)")
        }
    }

    // MARK: - Telling the person, once

    /// What one root's fan-out has come to, accumulated as its tasks end.
    ///
    /// **A phone hears about a batch and never about a task, and the arithmetic is the argument.**
    /// Five children with three of their own is twenty sessions, every one of them a terminal
    /// that goes idle when it is done — so before this existed a fan-out was up to twenty
    /// identical "finished a long run" notifications, none of which said which tree it belonged
    /// to or whether anything was still outstanding. That is the same mistake ``StateHook``'s own
    /// comment argues against for root sessions, made one level down where nobody had looked.
    ///
    /// The one fact a person actually wants out of all twenty is that the work they asked for has
    /// come back, and how much of it failed. That is one sentence, and it arrives once.
    private struct Batch {
        var done = 0
        var failed = 0
        var rootLabel: String?
        var projectDir: String?
    }

    /// Root key → tally. In memory only: a process that restarts in the middle of a fan-out has
    /// already interrupted it, and inventing a count for work it did not watch would be worse
    /// than saying nothing.
    private static var batches: [String: Batch] = [:]

    /// Under the lock. The session a whole tree hangs from, so a grandchild is counted in the
    /// same batch as the child that dispatched it.
    ///
    /// A task that named nobody gets a key of its own rather than sharing one with every other
    /// anonymous dispatch — the alternative is two unrelated fan-outs waiting for each other.
    private static func rootKeyLocked(of task: Task) -> String {
        rootKey(of: task, among: tasks)
    }

    /// Main thread, from ``finalize(_:as:summary:artifacts:)``. Every ending goes through there —
    /// success, failure, timeout, cancellation, a tab that closed under a child — so this counts
    /// all of them and not only the tidy ones.
    private static func noteEnded(_ task: Task) {
        lock.lock()
        let key = rootKeyLocked(of: task)
        var batch = batches[key] ?? Batch()
        batch.done += 1
        if task.state != .success { batch.failed += 1 }
        if batch.projectDir == nil { batch.projectDir = task.projectDir }
        if batch.rootLabel == nil { batch.rootLabel = task.rootLabel }
        batches[key] = batch
        lock.unlock()
    }

    /// Announce any batch that has nothing left running. Called from the beat rather than from
    /// `finalize`, and that is a correctness point rather than tidiness: `finalize` cancels the
    /// work its task handed on **asynchronously**, so at the moment it ends, a grandchild about
    /// to be taken down still counts as live. Asking again a beat later is asking after the dust
    /// has settled, and it covers the same ground for cancellation, timeouts, and a tab somebody
    /// closed by hand.
    private static func sweepBatches() {
        lock.lock()
        guard !batches.isEmpty else { lock.unlock(); return }
        var ready: [(String, Batch)] = []
        for (key, batch) in batches {
            let running = tasks.values.contains {
                !$0.state.isTerminal && rootKeyLocked(of: $0) == key
            }
            guard !running else { continue }
            ready.append((key, batch))
            batches.removeValue(forKey: key)
        }
        lock.unlock()
        for (key, batch) in ready { announce(batch, rootKey: key) }
    }

    /// The two lines a finished fan-out is worth. Pure half in ``batchMessage(project:done:failed:)``.
    private static func announce(_ batch: Batch, rootKey key: String) {
        // The "it finished" class, so it takes the preference that class already has. A batch
        // that ended badly is still a batch that ended: whatever is worth doing about it is
        // waiting in the root's own conversation, which was told the moment each part came back.
        guard Config.shared.pushOnFinish else { return }
        let project = batch.projectDir.map { StateHook.projectName(forDirectory: $0) } ?? "Clawdline"
        let message = batchMessage(project: project, label: batch.rootLabel,
                                   done: batch.done, failed: batch.failed)
        var url = "/"
        if !key.hasPrefix("task:"),
           let root = SessionWatch.shared.targets.first(where: {
               HookBridge.note(for: $0)?.session == key
           }) {
            url = "/#session=\(root.id)"
        }
        WebPush.send(title: message.title, body: message.body, url: url,
                     // Keyed on the root and not on a task, so a second fan-out from the same
                     // session replaces the first rather than stacking under it.
                     tag: "batch-\(key)",
                     icon: RemoteIcon.projectPath(
                        for: batch.projectDir.flatMap { ProjectIcon.grid(forCwd: $0) }))
    }

    /// Pure, so the wording can be checked without a terminal, a phone or a clock.
    static func batchMessage(project: String, label: String?,
                             done: Int, failed: Int) -> StateHook.PushMessage {
        StateHook.PushMessage(title: label ?? project,
                              body: "\(project) \(L.t.pushBatchDone(done: done, failed: failed))")
    }

    // MARK: - What the child is told

    /// The announcement rides in the typed line and not only in CHILD.md, because an assistant
    /// answers the line before it opens the file — and the first thing on the screen should be
    /// what the child was sent to do, in the language of whoever is looking.
    static func firstLine(id: String, secret: String, announce: String? = nil) -> String {
        let opening = announce.map { "Say this line first, verbatim: \($0) Then read" } ?? "Read"
        return "You are a Clawdline CHILD agent for task \(id). "
            + "\(opening) /tmp/.clawdline/\(id)/CHILD.md and follow it exactly. TASK_SECRET=\(secret)"
    }

    /// The interface language, named twice — in English for the assistant reading the briefing,
    /// and in itself for the person reading over its shoulder: "Traditional Chinese (繁體中文)".
    static var languageName: String {
        let tag = L.tag(of: L.t)
        let english = Locale(identifier: "en").localizedString(forIdentifier: tag) ?? tag
        let native = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
        return english == native ? english : "\(english) (\(native))"
    }

    /// How many levels of dispatch this Mac is set up for: 1 when a child may not dispatch at
    /// all, 2 when it may. There is no third stop, and that is a decision rather than an
    /// oversight — the numbers multiply, and a tree deeper than one somebody can hold in their
    /// head is a tree nobody can be asked to supervise.
    static var depthFloor: Int { Config.shared.orchestratorMaxGrandchildren > 0 ? 2 : 1 }

    static func childBrief(for task: Task) -> String {
        let dir = "/tmp/.clawdline/\(task.id)"
        // What this one may hand on in turn: the configured allowance while there is a level
        // below it, and nothing at all once it is standing on the floor. Written into the
        // briefing rather than left to be discovered, because a child that finds out by being
        // refused has already spent a turn on it.
        let allowance = task.depth < depthFloor ? Config.shared.orchestratorMaxGrandchildren : 0
        let handOnRule = allowance > 0
            ? "You may hand parts of this on to at most \(allowance) child sessions of your own, "
                + "which cannot hand anything on further — see \"Handing work on\" below."
            : "Do not dispatch Clawdline tasks of your own."
        return """
        # Clawdline child briefing — task \(task.id)

        You are a CHILD session working for a Clawdline root session. Your one job is the task
        described in \(dir)/task.json — read that file now.
        \(planSection(for: task))
        ## Language, and the first thing you say

        The person watching this terminal reads \(languageName). Everything you say in this
        session, and the "summary" you write into result.json, is in that language — this
        briefing is in English only so that every assistant reads it the same way.

        Before you read task.json or touch anything, say exactly this line, on its own:

        \(L.t.childAnnounce(task.title))

        Then, once you have read task.json, one more line in the same language saying in your
        own words what you are about to do and where the output will go.

        ## Rules

        - Work inside \(task.projectDir). Put every file you produce in \(dir)/artifacts/
          (create the directory if it is missing).
        - \(handOnRule)
        - Do not read any directory under /tmp/.clawdline/ except your own, any you dispatched,
          and any your instructions name explicitly. That last one is how a reviewing node works:
          it is sent to read what other nodes produced, so its instructions list those paths.
        - Do not do work the task did not ask for.
        - You have \(task.timeoutMinutes) minutes before the task is marked timed out.
        \(handOnSection(for: task, allowance: allowance))\(policySection(allowance: allowance))
        ## Reporting — this is the completion signal, do it exactly

        When the work is done (or has failed for good), write \(dir)/result.json:

        ```json
        {"clawdline_protocol": 1,
         "task_id": "\(task.id)",
         "task_secret": "<the TASK_SECRET value from your first message>",
         "status": "success",
         "summary": "<one paragraph: what you did, or why it failed>",
         "artifacts": ["artifacts/<file>", "..."],
         "finished_at": "<ISO8601 UTC>"}
        ```

        Use "status": "failure" when you could not do it. Write it LAST — the moment it exists
        your work is considered finished.

        **If you handed work on and it did not arrive, say so in the summary.** Doing it yourself
        instead is usually right — the answer is what was asked for, not who produced it. What is
        not right is a summary that reads as though the sessions you dispatched did the work when
        they never started. Whoever reads this is deciding how much to trust the result, and
        "both halves came back" and "both halves failed and I did it myself" are different amounts
        of evidence behind the same answer.

        **Write it with your file-writing tool, not with a shell command.** A shell line that
        builds JSON and moves it into place gets refused by command screening on its own shape —
        quotes inside braces, a redirect it cannot analyse statically — and that refusal is a
        prompt with no "always allow" on a tab nobody is watching. Atomicity is not yours to
        arrange: a half-written file simply fails to parse and is read again a few seconds later.

        Optionally, if outbound network is permitted in your sandbox, you may ALSO announce it:
        `curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/complete \\
           -H "X-Clawdline-Task-Secret: <TASK_SECRET>" -H 'Content-Type: application/json' \\
           -d '{"status":"success","summary":"..."}'`
        This is never required; the file alone is enough.
        """
    }

    // MARK: - House rules

    /// Where the dispatch policy lives — beside the token and the registry, in the directory
    /// `CLAWDLINE_REMOTE_DIR` moves when it is set.
    ///
    /// A file rather than a string in `config.json`, because it is paragraphs: multi-line prose
    /// survives being hand-edited in a file and does not survive being hand-edited as a JSON
    /// string with `\n` in it.
    static var policyURL: URL { RemoteAuth.directory.appendingPathComponent("dispatch-policy.md") }

    /// The maximum this app will carry into a briefing.
    ///
    /// It was 4096, which was set against "house rules" meaning a paragraph about which assistant
    /// to use. Once the file also had to carry the decision of *whether to dispatch at all* and a
    /// library of graph shapes, that number cut the last rule off mid-word. Twelve thousand is
    /// room for a policy somebody has actually thought about, and still small enough that a file
    /// with a novel pasted into it cannot push the task itself off the bottom of a child's
    /// attention — which is what the limit is for.
    static let policyLimit = 12_000

    /// The same ceiling for the graph a task carries. Both end up in one briefing beside 16 KiB
    /// of instructions, and a briefing a child skims is worse than a shorter one it reads.
    static let planLimit = 4096

    /// What this Mac says about how work should be handed out, or nil when nobody has said
    /// anything. Read at dispatch rather than at launch, so an edit takes effect on the next
    /// task instead of on the next restart.
    static func policy() -> String? { policy(reading: try? String(contentsOf: policyURL, encoding: .utf8)) }

    /// The reading half, separated so a test can hand it text instead of a file.
    ///
    /// **Cutting is announced, and it happens at a paragraph.** The first version took a plain
    /// `prefix`, which put the knife wherever 4096 characters landed — and the first policy long
    /// enough to hit it lost its last rule mid-word, silently, with the briefing reading as
    /// though the file simply ended there. That is the failure this whole app keeps meeting: a
    /// thing that did not happen, wearing the shape of a thing that did. So the cut falls on the
    /// last paragraph break before the limit, and says out loud that it fell.
    static func policy(reading raw: String?) -> String? {
        guard let raw else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        guard text.count > policyLimit else { return text }
        let head = String(text.prefix(policyLimit))
        let body = head.range(of: "\n\n", options: .backwards)
            .map { String(head[..<$0.lowerBound]) } ?? head
        return body + "\n\n**[This policy was cut here.** It is longer than \(policyLimit) "
            + "characters, and everything after this point was not included — so if a rule you "
            + "expected is missing, that is why. Shorten the file.**]**"
    }

    /// Write the starting policy, once, if there is no file yet — and answer where it is either
    /// way. Never overwrites: what is in there is somebody's, and a default that came back after
    /// being deleted would be a setting that does not stay set.
    @discardableResult
    static func ensurePolicyFile() -> URL {
        let url = policyURL
        guard !FileManager.default.fileExists(atPath: url.path) else { return url }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(defaultPolicy.utf8).write(to: url, options: .atomic)
        return url
    }

    /// Opinionated on purpose. An empty file with a comment saying "put your rules here" is a
    /// feature nobody uses; a file with defensible rules already in it is one somebody edits.
    /// English because it is copied into a briefing every other line of which is English, and
    /// because it is read by an assistant before it is read by a person — but nothing stops it
    /// being rewritten in any language, and a child will follow it just the same.
    static let defaultPolicy = """
    # How work is handed out on this Mac

    Clawdline reads this file every time a task is dispatched and copies it into the briefing of every
    child that is allowed to dispatch in turn. Edit it freely. Delete everything and the feature
    switches itself off — an empty file means there are no house rules.

    ## First: should this be dispatched at all?

    There is a measurement behind this and it is sharp in both directions: on work that splits into
    independent pieces, coordinating several agents beat a single one by **80.9%**; on work where
    every step depends on the one before it, *every* multi-agent arrangement tested was **39–70%
    worse** than a single agent, because the handoffs break a chain that needed to stay whole.

    So the question is one sentence: **can this be cut into pieces that do not need to talk to each
    other, and joined at the end?**

    **When the answer is no, that is a recommendation and not a refusal.** Say so, give the reason in
    a sentence, and ask — then do whatever they answer. The person has reasons this file cannot see:
    they may want Codex to take this one, or their own context left free for something else, or
    simply to watch it happen in a tab they can step into. **Their yes settles it**, and does not need
    to be argued with or hedged. What is owed is the reason, once, before the work starts — not after
    it went badly.

    These are the shapes that look dispatchable and are not, and the ones worth saying it about:

    - **Diagnosis and debugging.** Every step is chosen because of what the last step found. Handing
      that to a fresh session throws away the reasoning that made the next step obvious.
    - **Anything needing dozens of small parallel jobs.** Every node is a real assistant cold-starting
      and holding a terminal tab. A hundred of them is slower and dearer than the batch tool for it,
      and nobody can read a hundred tabs.
    - **Anything on a path where somebody is waiting.** A node takes tens of seconds to reach a prompt.
    - **Agents that need to talk back and forth.** Dispatch is one-way: brief, wait, collect. Every
      round trip means a whole new task.
    - **Output that has to be typed data for a program to consume.** What comes back is a paragraph
      and some file paths.
    - **Work smaller than its own briefing.** If writing the instructions takes longer than doing it,
      that is the answer.

    ## Then: pick a shape

    Named shapes, so a graph is chosen rather than improvised. Every one of them ends the same way —
    see the last section.

    - **Split and join** — one question, several independent pieces. Leaves gather (`haiku`), one node
      joins and judges (`sonnet`+). The default for research, audits, and surveys.
    - **Build then read** — anything producing code or a decision somebody acts on. Workers build;
      a separate node reads what they built and reports what is wrong. Never the same node, never the
      same session.
    - **Decide then do** — one node reads and writes the plan without touching anything; a person
      passes it; a second node (usually Codex) implements it. The value is the gap in the middle,
      where a person can still say no cheaply.
    - **Batch with takeover** — the same mechanical change across independent modules, one node each.
      When one dies its tab survives with its state on screen, and a person finishes it by typing.
    - **Candidates** — the same problem to several nodes with different instructions, each producing a
      complete working answer. A person picks. No judging node: what is being compared is taste, and a
      judge model has its own.

    ## Which assistant

    - **Codex** for *making* something you can then look at: writing code, drawing an image with
      the image model it has built in, hand-writing an SVG, running a build until it goes green,
      mechanical edits across many files. It cannot be told where to save a drawing, so a task
      that wants one has to say: generate it, then copy the file into the task's `artifacts/`.
    - **Claude** for reading and judging: reviewing a diff, working out why something behaves the
      way it does, searching and weighing what it found, writing prose somebody will read.

    ## Which model

    Name one when the default is the wrong size for the job, and say why in the plan.

    - `haiku` — mechanical, single-source work. Fetch a page and pull three facts out of it.
      Reformat a list. Anything where being wrong is obvious.
    - `sonnet` — ordinary work with judgement in it, and the default choice for a leaf.
    - `opus` — a decision somebody will act on without checking, and any synthesis of several
      children's answers.

    **A verdict runs on a model at least as strong as what produced what it is judging.** A
    review by something smaller than the thing reviewed is a rubber stamp with a token cost.

    ## The shape of the graph

    - Plan the whole graph before dispatching any of it: what each leaf produces, who joins those
      answers together, and what the top hands back.
    - **Breadth before depth.** Two children splitting a job beat one child that will hand half
      of it on. Go a level deeper only when the second level's work cannot be named until the
      first level has answered.
    - **Every node is told the whole graph**, not just its own job — that is what `plan` in
      task.json is for. A leaf that knows what its output feeds writes a usable output; one that
      does not writes an essay.
    - Leaves are narrow enough to state in a sentence. If a child's instructions need three
      paragraphs to say what "done" means, it is two children.

    ## Dispatching itself

    - **Stagger them.** Wait 30–45 seconds between one dispatch and the next. Every child is a
      real assistant cold-starting on this Mac, and started together they compete — a tab that
      has not reached a prompt in four minutes is `spawn_failed`. A minute of waiting buys the
      whole batch.
    - **A `spawn_failed` retry needs a fresh id and a fresh secret.** That task id is terminal.
    - **Say when you did it yourself.** If a dispatch failed twice and you did the work instead,
      that is usually right — but the summary has to say so.

    ## Somebody has to check the work

    Every graph that produces code, or a decision anybody will act on, ends with a node whose only
    job is to find what is wrong with it. Not a fifth worker — a reader.

    - **It produces nothing.** It reads the other nodes' artifacts and writes findings. Fixing is
      the next round's job, or a person's — and that holds even when it is sure it knows the fix,
      because a repair is a fast way to bury the judgement somebody needed to see.
    - **It did not help build the thing.** Self-review is measurably bad at this: a model judging
      its own output misses about a third of its own semantic drift, and the mechanism is
      structural rather than a capability gap — a judge favours low-perplexity text, and a
      model's own output is low-perplexity to it by construction. (Giving it the outputs rather
      than the conversation behind them is the same idea extended one step; that half is
      reasoning, not a measured result.)
    - **A different assistant helps, and does not solve it.** Codex wrote it, Claude reads it —
      but do not mistake that for independence. A panel of nine frontier models was measured to
      carry only about two votes' worth of independent information, because different models get
      the same items wrong. Where a review really matters, use *several* reviewers and take the
      majority, and pick them for being complementary rather than merely different.
    - **Opus-class, always.** Not "no weaker than what it judges" — an absolute floor. A review is
      worth exactly what the reviewer's judgement is worth, and a missed finding travels all the
      way to the end. Measured here: a Sonnet reviewer, in the middle of correctly explaining that
      judging is prone to hallucination, invented a specific citation — it claimed a document
      disputed a term the document never mentions.
    - **Name the paths it may read.** List the exact `/tmp/.clawdline/<id>/artifacts/`
      directories. This is how the rule above is enforced rather than merely stated: without it a
      reviewer can wander into the production conversation it was supposed to be kept out of.
    - **A verdict, with its receipts.** "What is wrong, worst first, is it safe to ship" — and
      every finding names the artifact and the passage it rests on. A verdict without sources is
      the exact shape a hallucinating judge produces, and it costs nothing to require.
    """

    /// The graph this task is one node of, when the dispatcher wrote one down.
    ///
    /// Near the top, above even the language rule, because it is the context every other line is
    /// read in: a child that knows its answer is one of four being joined together writes
    /// something joinable, and one that does not writes a report.
    private static func planSection(for task: Task) -> String {
        guard let plan = task.plan, !plan.isEmpty else { return "" }
        return """


        ## The plan this is part of

        Written by the session that dispatched you. You are one node of it — find yourself in it
        before you start, and hand back what the node after you needs rather than everything you
        found.

        \(plan)

        """
    }

    /// This Mac's house rules for handing work out, when there are any.
    ///
    /// Only for a child that may dispatch, because that is what the rules are about — a leaf
    /// reading somebody's model-selection policy is reading noise it has no decision to spend it
    /// on. Read from disk at briefing time, so an edit reaches the next child rather than the
    /// next launch.
    private static func policySection(allowance: Int) -> String {
        guard allowance > 0, let policy = policy() else { return "" }
        return """


        ## What this Mac says about handing work out

        House rules, from \(policyURL.path). They are the person's, not this app's; where they
        and your own judgement disagree, follow them and say so in your summary.

        \(policy)

        """
    }

    /// The paragraph that makes a child a dispatcher, or nothing at all when it is not one.
    ///
    /// Spelled out in full rather than pointed at a skill, because half the sessions this is
    /// written for are Codex and Codex has no skills — and because the one field that matters,
    /// `root.parent_task`, is the one nothing else would tell it. A child knows its own task id
    /// from the first line it was ever sent, so naming its parent is the one identification it
    /// can always make correctly, whatever this app has managed to work out about it.
    private static func handOnSection(for task: Task, allowance: Int) -> String {
        guard allowance > 0 else { return "" }
        return """

        ## Handing work on

        Parts of this task that stand entirely on their own may go to child sessions of yours —
        at most \(allowance) alive at once. They are the last level: nothing opens under what
        they open. Only do it where a part really is separate work, since briefing a session
        costs more than most of what you would hand it, and never for something you could do in
        the time it takes to write the instructions.

        For each one, a fresh id, a fresh secret and its own directory:

        ```bash
        TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
        sub=$(uuidgen | tr '[:upper:]' '[:lower:]'); sub_secret=$(openssl rand -hex 32)
        umask 077 && mkdir -p "/tmp/.clawdline/$sub/artifacts"
        cat > /tmp/.clawdline/$sub/task.json <<JSON
        {"clawdline_protocol": 1,
         "task_id": "$sub",
         "assistant": "claude",
         "model": "haiku",
         "permission_mode": "edits",
         "project_dir": "\(task.projectDir)",
         "title": "<short title>",
         "timeout_minutes": 30,
         "instructions": "<the instructions, as one JSON string>",
         "plan": "<the whole graph, the same text for every one of them>",
         "root": {"parent_task": "\(task.id)"}}
        JSON
        curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks \\
          -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \\
          -d "{\\"task_id\\":\\"$sub\\",\\"secret\\":\\"$sub_secret\\"}"
        ```

        - `root.parent_task` must be exactly `\(task.id)` — your own task id. It is how the app
          knows where the new task sits; get it wrong and the dispatch is refused or filed under
          somebody else.
        - `assistant` is `claude` or `codex`; `model` is optional and takes lower-case letters,
          digits and `. _ -` only. Pick both against the rules below, and say in the plan why.
        - `permission_mode` is `ask`, `auto` or `full`, and leaving it out is right almost always
          — it takes this Mac's own setting, which is `\(Config.shared.orchestratorPermission)`.
          Nobody watches a child's tab, so `ask` is a session that stops until it times out.
        - `plan` is the graph, not this leaf's job — the same text in every task you dispatch,
          extended with what you have added to it. It is how a leaf knows what its answer feeds.
        - The instructions have to stand on their own. That session cannot see this one, so
          "as described above" reaches it as an empty file.
        - Branch on the reply's `code`: `over_capacity` means wait or ask for fewer,
          `depth_exceeded` means this Mac goes no deeper and the work is yours to do.
        - The orchestrator token is this Mac's credential, not yours to pass on. Do not write it
          into a file, do not put it in /tmp, do not hand it to anything you dispatch.
        - Its answer arrives as `/tmp/.clawdline/$sub/result.json`. The file appearing is the
          completion signal — poll for it, then read it. Those directories are the only ones
          besides your own you may look inside.
        - **Build that task.json with a heredoc, as above, not with `jq -n` and a quoted filter.**
          A brace next to a quote reads as obfuscation to command screening, which stops with a
          prompt that has no "always allow" — and there is nobody on this tab to answer it.
        - Wait for everything you handed on before writing your own result.json. Yours finishing
          is what ends theirs.
        - A dispatch that comes back `spawn_failed` can be retried, but **only with a fresh id and
          a fresh secret** — that task id is finished, and re-sending it just returns the record.
          If it fails again, do the work yourself and say in your summary that you did.

        """
    }

    private static func writeChildBrief(for task: Task) {
        let url = task.dir.appendingPathComponent("CHILD.md")
        try? Data(childBrief(for: task).utf8).write(to: url, options: .atomic)
    }

    // MARK: - Usage and cost

    /// Per million tokens, US dollars. Codex bills against a plan rather than per token, so an
    /// unknown model honestly costs "we cannot say" instead of a made-up number.
    static func price(forModel model: String?) -> (input: Double, output: Double)? {
        guard let model else { return nil }
        if model.hasPrefix("claude-fable-5") || model.hasPrefix("claude-mythos-5") { return (10, 50) }
        if model.hasPrefix("claude-opus") { return (5, 25) }
        if model.hasPrefix("claude-sonnet") { return (3, 15) }
        if model.hasPrefix("claude-haiku-4-5") { return (1, 5) }
        return nil
    }

    static func cost(of usage: Usage) -> Double? {
        guard let price = price(forModel: usage.model) else { return nil }
        let dollars = Double(usage.input) * price.input
            + Double(usage.output) * price.output
            + Double(usage.cacheRead) * price.input * 0.1
            + Double(usage.cacheWrite) * price.input * 1.25
        return (dollars / 1_000_000 * 10_000).rounded() / 10_000
    }

    static func harvestUsage(_ task: Task) -> Usage? {
        // Usage is accounting, so an absent proven path is an absent answer. Reconstructing a
        // Claude filename from an unverified stored session id charged a sibling's transcript to
        // the wrong task; Codex's time-only lookup has the same identity weakness after a restart.
        guard let path = task.transcriptPath.map(URL.init(fileURLWithPath:)) else { return nil }
        if !task.transcriptProven,
           transcriptOwnership(path, assistant: task.assistant, taskID: task.id) != .belongs {
            return nil
        }
        switch task.assistant {
        case .claude: return claudeUsage(transcript: path)
        case .codex:  return codexUsage(rollout: path)
        }
    }

    /// Sum of every assistant turn's `message.usage` in a Claude transcript.
    static func claudeUsage(transcript: URL) -> Usage? {
        guard let data = try? Data(contentsOf: transcript), !data.isEmpty else { return nil }
        var usage = Usage()
        var found = false
        for line in data.split(separator: 0x0A) {
            guard let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  obj["type"] as? String == "assistant",
                  let message = obj["message"] as? [String: Any],
                  let counts = message["usage"] as? [String: Any] else { continue }
            found = true
            usage.input += counts["input_tokens"] as? Int ?? 0
            usage.output += counts["output_tokens"] as? Int ?? 0
            usage.cacheRead += counts["cache_read_input_tokens"] as? Int ?? 0
            usage.cacheWrite += counts["cache_creation_input_tokens"] as? Int ?? 0
            if let model = message["model"] as? String { usage.model = model }
        }
        guard found else { return nil }
        usage.total = usage.input + usage.output + usage.cacheRead + usage.cacheWrite
        usage.costUsd = cost(of: usage)
        return usage
    }

    /// The last cumulative `token_count` event in a Codex rollout — Codex keeps the running
    /// total itself, so the newest event is the whole answer.
    static func codexUsage(rollout: URL) -> Usage? {
        guard let data = try? Data(contentsOf: rollout), !data.isEmpty else { return nil }
        var usage: Usage?
        var model: String?
        for line in data.split(separator: 0x0A) {
            // A byte scan before a parse: rollouts run to tens of thousands of lines and only a
            // few say either of the words this reader is looking for.
            guard let text = String(data: Data(line), encoding: .utf8) else { continue }
            if model == nil || text.contains("turn_context") {
                if text.contains("\"model\""),
                   let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any] {
                    let payload = obj["payload"] as? [String: Any] ?? obj
                    if let named = payload["model"] as? String { model = named }
                }
            }
            guard text.contains("token_count"),
                  let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  obj["type"] as? String == "event_msg",
                  let payload = obj["payload"] as? [String: Any],
                  payload["type"] as? String == "token_count",
                  let info = payload["info"] as? [String: Any],
                  let totals = info["total_token_usage"] as? [String: Any] else { continue }
            var made = Usage()
            made.input = totals["input_tokens"] as? Int ?? 0
            made.output = totals["output_tokens"] as? Int ?? 0
            made.cacheRead = totals["cached_input_tokens"] as? Int ?? 0
            made.cacheWrite = totals["cache_write_input_tokens"] as? Int ?? 0
            made.total = totals["total_tokens"] as? Int ?? (made.input + made.output)
            usage = made
        }
        guard var made = usage else { return nil }
        made.model = model
        made.costUsd = cost(of: made)
        return made
    }

    // MARK: - What the API answers with

    /// Every task, newest first, in the wire shape. Hops to the main thread for the live
    /// resolutions (which terminal is the root, right now) the way `session(withID:)` does.
    static func records() -> [[String: Any]] {
        if !Thread.isMainThread { return DispatchQueue.main.sync { records() } }
        lock.lock()
        let all = tasks.values.sorted { $0.created > $1.created }
        lock.unlock()
        return all.map { record(of: $0) }
    }

    static func record(id: String) -> [String: Any]? {
        if !Thread.isMainThread { return DispatchQueue.main.sync { record(id: id) } }
        guard let task = held(id) else { return nil }
        return record(of: task)
    }

    /// The stored fields only — safe off the main thread, used where a route already holds the
    /// answer and only needs its shape.
    private static func existingRecord(_ id: String) -> [String: Any]? {
        guard let task = held(id) else { return nil }
        return shape(task, rootTerminal: nil)
    }

    /// Test seam for the GET representation, including optional fields whose absence is semantic.
    static func recordForTesting(_ task: Task) -> [String: Any] {
        shape(task, rootTerminal: nil)
    }

    /// Test seam: the linger a held record is carrying. Not in the GET representation, which
    /// describes the work rather than the housekeeping waiting on it.
    static func closeAtForTesting(_ id: String) -> Date? { held(id)?.closeAt }

    private static func record(of task: Task) -> [String: Any] {
        var rootTerminal: String?
        // The parent task first, when there is one. A dispatcher one level down is sitting in a
        // tab this app opened, so its terminal id is written in that task's record — whereas the
        // hook notes below only know sessions that write them, which a Codex child never does.
        if let parent = task.parentTaskId, let above = held(parent) {
            rootTerminal = above.childTerminalId
        }
        if rootTerminal == nil, let rootID = task.rootSessionId {
            rootTerminal = SessionWatch.shared.targets.first {
                HookBridge.note(for: $0)?.session == rootID
            }?.id
        }
        return shape(task, rootTerminal: rootTerminal)
    }

    private static func shape(_ task: Task, rootTerminal: String?) -> [String: Any] {
        var out: [String: Any] = [
            "id": task.id,
            "state": task.state.rawValue,
            "kind": task.kind,
            "title": task.title,
            "assistant": task.assistant.rawValue,
            "projectDir": task.projectDir,
            "created": Int(task.created.timeIntervalSince1970),
            "depth": task.depth,
            "dir": "/tmp/.clawdline/\(task.id)",
        ]
        if let model = task.model { out["model"] = model }
        out["permission"] = task.permission.rawValue
        if task.state == .queued, !task.serialize.isEmpty {
            lock.lock()
            let waiting = serializeBlockersLocked(for: task).map(\.id)
            lock.unlock()
            if !waiting.isEmpty { out["waiting_on"] = waiting }
        }
        if let at = task.spawnedAt { out["spawnedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.briefedAt { out["briefedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.finishedAt { out["finishedAt"] = Int(at.timeIntervalSince1970) }
        var root: [String: Any] = [:]
        if let id = task.rootSessionId { root["sessionId"] = id }
        if let label = task.rootLabel { root["label"] = label }
        if let terminal = rootTerminal { root["terminalId"] = terminal }
        if let parent = task.parentTaskId { root["taskId"] = parent }
        if !root.isEmpty { out["root"] = root }
        var child: [String: Any] = [:]
        if let id = task.childTerminalId { child["terminalId"] = id }
        if let backend = task.childBackend { child["backend"] = backend.rawValue }
        if let id = task.childSessionId { child["sessionId"] = id }
        if !child.isEmpty { out["child"] = child }
        if let summary = task.summary { out["summary"] = summary }
        if !task.artifacts.isEmpty { out["artifacts"] = task.artifacts }
        if task.claimsDeclared { out["claims"] = task.claims }
        if let usage = task.usage {
            var counts: [String: Any] = [
                "input": usage.input, "output": usage.output,
                "cacheRead": usage.cacheRead, "cacheWrite": usage.cacheWrite,
                "total": usage.total,
            ]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["costUsd"] = cost }
            out["usage"] = counts
        }
        return out
    }

    // MARK: - The store

    static func load(force: Bool = false) {
        lock.lock()
        if loaded, !force { lock.unlock(); return }
        loaded = true
        lock.unlock()
        guard let data = try? Data(contentsOf: storeURL),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return }
        var found: [String: Task] = [:]
        for row in obj["tasks"] as? [[String: Any]] ?? [] {
            guard let task = task(from: row) else { continue }
            found[task.id] = task
        }
        lock.lock()
        tasks = found
        reindex()
        lock.unlock()
    }

    private static func save() {
        storeSaveLock.lock(); defer { storeSaveLock.unlock() }
        lock.lock()
        reindex()
        let rows = tasks.values.sorted { $0.created < $1.created }.map { stored($0) }
        lock.unlock()
        let obj: [String: Any] = ["version": 1, "tasks": rows]
        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys,
                                                               .withoutEscapingSlashes]) else {
            Log.write("orchestrator: could not serialise the store, nothing written")
            return
        }
        try? FileManager.default.createDirectory(at: RemoteAuth.directory,
                                                 withIntermediateDirectories: true)
        try? data.write(to: storeURL, options: .atomic)
        // Every time, not only at creation — same reason as `RemoteAuth.save`.
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: storeURL.path)
    }

    static func stored(_ task: Task) -> [String: Any] {
        var out: [String: Any] = [
            "id": task.id,
            "state": task.state.rawValue,
            "kind": task.kind,
            "title": task.title,
            "assistant": task.assistant.rawValue,
            "project_dir": task.projectDir,
            "timeout_minutes": task.timeoutMinutes,
            "created": task.created.timeIntervalSince1970,
            "depth": task.depth,
            "secret_hash": task.secretHash,
            "artifacts": task.artifacts,
        ]
        if let at = task.spawnedAt { out["spawned_at"] = at.timeIntervalSince1970 }
        if let at = task.briefedAt { out["briefed_at"] = at.timeIntervalSince1970 }
        if let at = task.finishedAt { out["finished_at"] = at.timeIntervalSince1970 }
        if let v = task.rootSessionId { out["root_session"] = v }
        if let v = task.rootLabel { out["root_label"] = v }
        if let v = task.parentTaskId { out["parent_task"] = v }
        if let v = task.model { out["model"] = v }
        out["permission"] = task.permission.rawValue
        if let v = task.plan { out["plan"] = v }
        if !task.serialize.isEmpty { out["serialize"] = task.serialize }
        if task.claimsDeclared { out["claims"] = task.claims }
        if !task.claimKeys.isEmpty { out["claim_keys"] = task.claimKeys }
        if let v = task.queuedSecret { out["queued_secret"] = v }
        if let v = task.childTerminalId { out["child_terminal"] = v }
        if let v = task.childBackend { out["child_backend"] = v.rawValue }
        if let v = task.childTTY { out["child_tty"] = v }
        if let v = task.childPID { out["child_pid"] = Int(v) }
        if let v = task.childProcStart { out["child_proc_start"] = v.timeIntervalSince1970 }
        if let v = task.childSessionId { out["child_session"] = v }
        if let at = task.closeAt { out["close_at"] = at.timeIntervalSince1970 }
        if let v = task.transcriptPath { out["transcript"] = v }
        if task.transcriptProven { out["transcript_proven"] = true }
        if let v = task.summary { out["summary"] = v }
        if let usage = task.usage {
            var counts: [String: Any] = ["input": usage.input, "output": usage.output,
                                         "cache_read": usage.cacheRead,
                                         "cache_write": usage.cacheWrite, "total": usage.total]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["cost_usd"] = cost }
            out["usage"] = counts
        }
        return out
    }

    static func task(from obj: [String: Any]) -> Task? {
        guard let id = obj["id"] as? String, isTaskID(id),
              let state = (obj["state"] as? String).flatMap(State.init(rawValue:)),
              let assistant = (obj["assistant"] as? String).flatMap(Assistant.init(rawValue:)),
              let projectDir = obj["project_dir"] as? String,
              let created = obj["created"] as? Double,
              let secretHash = obj["secret_hash"] as? String else { return nil }
        var task = Task(id: id, state: state,
                        kind: obj["kind"] as? String ?? "custom",
                        title: obj["title"] as? String ?? "task",
                        assistant: assistant, projectDir: projectDir,
                        timeoutMinutes: obj["timeout_minutes"] as? Int ?? 30,
                        created: Date(timeIntervalSince1970: created),
                        secretHash: secretHash)
        task.spawnedAt = (obj["spawned_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.briefedAt = (obj["briefed_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.finishedAt = (obj["finished_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.rootSessionId = obj["root_session"] as? String
        task.rootLabel = obj["root_label"] as? String
        task.parentTaskId = obj["parent_task"] as? String
        task.model = StartPoints.modelName(obj["model"] as? String)
        task.permission = (obj["permission"] as? String).flatMap(Permission.init(rawValue:)) ?? .ask
        task.plan = obj["plan"] as? String
        task.serialize = (obj["serialize"] as? [String] ?? []).filter {
            StartPoints.modelName($0) == $0
        }
        let rawClaims = obj["claims"] as? [String]
        task.claims = (rawClaims ?? []).filter { path in
            !path.isEmpty && path.count <= 1_024 && !path.hasPrefix("/")
                && !path.split(separator: "/", omittingEmptySubsequences: false)
                    .contains(where: { $0 == ".." })
                && !path.unicodeScalars.contains(where: { $0.value == 0 })
        }
        task.claimsDeclared = rawClaims.map { $0.count == task.claims.count } ?? false
        let storedClaimKeys = (obj["claim_keys"] as? [String] ?? []).filter {
            $0.hasPrefix("/")
        }
        task.claimKeys = storedClaimKeys.count == task.claims.count
            ? storedClaimKeys
            : freezeClaims(task.claims, projectDir: task.projectDir)
        task.queuedSecret = obj["queued_secret"] as? String
        // A registry written before tasks had a depth holds only tasks a root dispatched, which
        // is exactly what 1 means.
        task.depth = (obj["depth"] as? Int).map { min(max($0, 1), 9) } ?? 1
        task.childTerminalId = obj["child_terminal"] as? String
        task.childBackend = (obj["child_backend"] as? String).flatMap(Backend.init(rawValue:))
        task.childTTY = obj["child_tty"] as? String
        task.childPID = (obj["child_pid"] as? Int).flatMap(Int32.init(exactly:))
        task.childProcStart = (obj["child_proc_start"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.childSessionId = obj["child_session"] as? String
        task.closeAt = (obj["close_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.transcriptPath = obj["transcript"] as? String
        task.transcriptProven = obj["transcript_proven"] as? Bool == true
            && task.childSessionId != nil && task.transcriptPath != nil
        if task.transcriptProven, task.assistant == .claude,
           let path = task.transcriptPath, let sessionID = task.childSessionId,
           URL(fileURLWithPath: path).deletingPathExtension().lastPathComponent != sessionID {
            // A proof belongs to the path/session pair, not merely to the row. A malformed pair
            // is retained for diagnosis but cannot participate in cascade ownership; usage may
            // still independently re-prove the path from its task marker.
            task.transcriptProven = false
        }
        task.summary = obj["summary"] as? String
        task.artifacts = obj["artifacts"] as? [String] ?? []
        if let counts = obj["usage"] as? [String: Any] {
            var usage = Usage()
            usage.input = counts["input"] as? Int ?? 0
            usage.output = counts["output"] as? Int ?? 0
            usage.cacheRead = counts["cache_read"] as? Int ?? 0
            usage.cacheWrite = counts["cache_write"] as? Int ?? 0
            usage.total = counts["total"] as? Int ?? 0
            usage.model = counts["model"] as? String
            usage.costUsd = counts["cost_usd"] as? Double
            task.usage = usage
        }
        return task
    }

    // MARK: - Cleanup

    /// Task directories are working files, not the archive — the record survives here, the
    /// directory goes once it is a day old and its task is over. The registry itself is capped so
    /// a year of use cannot grow the file without bound.
    static func cleanup() {
        load()
        let cutoff = Date().addingTimeInterval(-24 * 3600)
        lock.lock()
        let done = tasks.values.filter { task in
            task.state.isTerminal && (task.finishedAt ?? task.created) < cutoff
        }
        lock.unlock()
        for task in done {
            try? FileManager.default.removeItem(at: task.dir)
        }
        lock.lock()
        let all = tasks.values.sorted { $0.created > $1.created }
        if all.count > 200 {
            for task in all.dropFirst(200) { tasks.removeValue(forKey: task.id) }
        }
        lock.unlock()
        if all.count > 200 { save() }
    }

    // MARK: - Small lookups

    private static func held(_ id: String) -> Task? {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks[id]
    }

    private static func heldSecret(_ id: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return secrets[id]
    }

    private static func target(withID id: String) -> TargetSession? {
        if Thread.isMainThread { return SessionWatch.shared.targets.first { $0.id == id } }
        return DispatchQueue.main.sync { SessionWatch.shared.targets.first { $0.id == id } }
    }

    private static func target(forRootSession sessionID: String) -> TargetSession? {
        let find = {
            SessionWatch.shared.targets.first {
                HookBridge.note(for: $0)?.session == sessionID
            }
        }
        if Thread.isMainThread { return find() }
        return DispatchQueue.main.sync(execute: find)
    }

    /// Test seam: forget everything in memory.
    static func forget() {
        lock.lock()
        tasks = [:]
        secrets = [:]
        dispatchTimes = []
        workspaceOverlapObserverForTesting = nil
        titlesByTerminal = [:]
        loaded = false
        lock.unlock()
        ownershipLock.lock()
        ownershipCache = [:]
        ownershipLock.unlock()
    }

    /// Test seam: wait until every pump already scheduled has finished. Callers first keep the
    /// main run loop moving until any main-thread finalization has happened, so this cannot wait
    /// on a pump that is itself waiting on main.
    @discardableResult
    static func drainSerializePumpForTesting(timeout: TimeInterval = 3) -> Bool {
        let done = DispatchSemaphore(value: 0)
        serializePumpQueue.async { done.signal() }
        let deadline = Date().addingTimeInterval(timeout)
        repeat {
            if done.wait(timeout: .now()) == .success { return true }
            if Thread.isMainThread {
                RunLoop.main.run(until: Date().addingTimeInterval(0.01))
            } else {
                Thread.sleep(forTimeInterval: 0.01)
            }
        } while Date() < deadline
        return done.wait(timeout: .now()) == .success
    }
}
