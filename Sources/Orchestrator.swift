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
/// phone must never be able to start sessions. A child never sees that token. It gets a per-task
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
        var childTerminalId: String?
        var childBackend: Backend?
        var childTTY: String?
        var childSessionId: String?
        var transcriptPath: String?
        var secretHash: String
        var summary: String?
        var artifacts: [String] = []
        var usage: Usage?
        var injectAttempts = 0
        /// The most recent time the first message was handed to the terminal. In memory only:
        /// a process restart loses the plaintext secret and fails every spawning task anyway.
        var lastInjectAt: Date?
        var answeredMenu = false
        /// When the child's terminal was last seen in a reading — the difference between a child
        /// that finished and one whose tab was closed under it.
        var lastSeenChild: Date?
        /// When the child's terminal is due to be closed, once it has reported. In memory only:
        /// a tab is closed on the strength of what this process saw, never on what a previous
        /// one wrote down.
        var closeAt: Date?

        var dir: URL { Orchestrator.root.appendingPathComponent(id, isDirectory: true) }
    }

    enum BriefingDecision: Equatable {
        case send, wait, accepted, exhausted
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

    /// The task marker in a user turn is the delivery receipt. Looking for this specific turn,
    /// rather than any user-shaped bookkeeping row, also proves that the transcript belongs to
    /// this task before it is allowed to close the retry gate.
    static func transcriptContainsBriefing(_ transcript: String?, assistant: Assistant,
                                           taskID: String) -> Bool {
        guard let transcript else { return false }
        let marker = "Clawdline CHILD agent for task \(taskID)"
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(marker)
        }
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

    private static let lock = NSLock()
    private static var loaded = false
    private static var tasks: [String: Task] = [:]
    /// How many `beat` walks are inside the loop, and which walk this is. Both exist to catch the
    /// overlap that should not be possible; neither changes what a walk does.
    private static var beatsInFlight = 0
    private static var beatSequence = 0
    /// Plaintext secrets, held only between dispatch and briefing. Never on disk.
    private static var secrets: [String: String] = [:]
    private static var dispatchTimes: [Date] = []
    /// Child terminal id → task title, rebuilt whenever the tasks change. Read on every redraw
    /// of every session row, which is why it is a dictionary and not a walk over the tasks.
    private static var titlesByTerminal: [String: String] = [:]

    /// What a terminal this app opened for a task is called. Nil for every other session.
    static func title(forTerminal id: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return titlesByTerminal[id]
    }

    /// Under the lock.
    private static func reindex() {
        var found: [String: String] = [:]
        for task in tasks.values {
            if let terminal = task.childTerminalId { found[terminal] = task.title }
        }
        titlesByTerminal = found
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
        if let existing = existingRecord(taskID) { return .ok(["ok": true, "task": existing]) }
        guard secret.count == 64, secret.allSatisfy({ ("a"..."f").contains($0) || $0.isNumber }) else {
            return .refused(422, "bad_task", "secret must be 64 hex characters.")
        }
        guard rateAllowed() else {
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
                        secretHash: hash(ofSecret: secret))
        lock.lock()
        tasks[taskID] = task
        secrets[taskID] = secret
        lock.unlock()
        RemoteAuth.audit("orchestrator.dispatch", ["task": taskID, "assistant": made.assistant.rawValue,
                                                   "cwd": made.projectDir, "kind": made.kind,
                                                   "depth": String(depth),
                                                   "model": made.model ?? "default",
                                                   "permission": permission.rawValue])

        // Straight away rather than on the next beat: the root is holding its breath on this
        // request, and the answer should already say whether a terminal actually opened.
        task = spawn(task)
        _ = replaceTask(task, expecting: .queued, discardSecret: task.state.isTerminal)
        save()
        DispatchQueue.main.async { SessionWatch.shared.nudge() }
        RemoteServer.shared.broadcastOrchestrator()
        guard let record = existingRecord(taskID) else {
            return .refused(500, "internal", "The task was lost while being made.")
        }
        return .ok(["ok": true, "task": record])
    }

    private static func spawn(_ task: Task) -> Task {
        var task = task
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

    /// Ten dispatches in ten minutes, or one full tree's worth, whichever is more.
    ///
    /// The window is a brake on a loop, not a second capacity cap — and once a child can dispatch
    /// too, filling the allowed tree legitimately takes more than ten calls. A limit that refuses
    /// the work the caps just permitted teaches people to retry, which is the behaviour it exists
    /// to discourage.
    private static func rateAllowed() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let now = Date()
        let allowed = max(10, Config.shared.orchestratorMaxDescendants)
        dispatchTimes = dispatchTimes.filter { now.timeIntervalSince($0) < 600 }
        guard dispatchTimes.count < allowed else { return false }
        dispatchTimes.append(now)
        return true
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
            cancelInPlace(below)
        }
        cancelInPlace(task)
        // The record it answers with still says the old state — finalize runs on main a moment
        // later — so the state is overridden here for the reply alone.
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
        let sessions = Set(above.compactMap { $0.childSessionId })
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
        // Anything the previous process was mid-way through briefing is unbriefable now: the
        // plaintext secret died with that process, and typing a secret we no longer hold is not
        // a thing that can be retried.
        var orphaned: [String] = []
        lock.lock()
        for (id, task) in tasks where task.state == .queued || task.state == .spawning {
            var dead = task
            dead.state = .spawnFailed
            dead.summary = "The app restarted before the child was briefed."
            dead.finishedAt = Date()
            tasks[id] = dead
            orphaned.append(id)
        }
        lock.unlock()
        if !orphaned.isEmpty { save() }
        cleanup()
        SessionWatch.shared.observers["orchestrator"] = { beat(fromTimer: false) }
        let t = Timer(timeInterval: 5, repeats: true) { _ in beat(fromTimer: true) }
        RunLoop.main.add(t, forMode: .common)
        timer = t
        let c = Timer(timeInterval: 6 * 3600, repeats: true) { _ in cleanup() }
        RunLoop.main.add(c, forMode: .common)
        cleanupTimer = c
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
        // from here is on the main thread, so that overlap should be impossible — and it happened
        // anyway, which means the list of callers or the assumption is wrong.
        //
        // So this counts rather than blocks. Blocking would make the next occurrence invisible,
        // and invisible is what made the first one take a day to reason about; the record can no
        // longer be damaged by a stale copy either way, because `replaceTask` refuses to move a
        // task backwards. What is missing is evidence of who the second walker is, and a walker
        // that is quietly dropped never leaves any.
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

    /// Try to put the first message in front of a child that has just opened. True when the task
    /// record changed.
    private static func brief(_ snapshot: Task) -> Bool {
        // A snapshot only nominates an id. Never act on its state or fields after another writer
        // may have advanced the record.
        guard var task = held(snapshot.id), task.state == .spawning else { return false }
        guard let spawnedAt = task.spawnedAt else { return false }
        if Date().timeIntervalSince(spawnedAt) > 120 {
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .spawnFailed,
                     summary: "The child session did not become ready within two minutes.")
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
        switch task.assistant {
        case .claude:
            if task.childSessionId == nil, let noted = HookBridge.note(for: child)?.session {
                task.childSessionId = noted
                changed = true
            }
            if task.transcriptPath == nil, let sessionID = task.childSessionId {
                task.transcriptPath = StartPoints.projectsRoot
                    .appendingPathComponent(StartPoints.slug(of: task.projectDir), isDirectory: true)
                    .appendingPathComponent(sessionID + ".jsonl").path
                changed = true
            }
            // With hooks disabled the file itself is the first durable identity available. A
            // delivered first turn creates it, so it can still confirm the ordinary one-send
            // path; absence without an identity is intentionally not enough to trigger a retry.
            if task.transcriptPath == nil,
               let found = Transcript.locate(cwd: task.projectDir, tabTitle: child.name,
                                               startedAt: task.spawnedAt) {
                task.transcriptPath = found.path
                task.childSessionId = found.deletingPathExtension().lastPathComponent
                changed = true
            }
        case .codex:
            if task.transcriptPath == nil,
               let rollout = Codex.locate(cwd: task.projectDir, startedAt: task.spawnedAt,
                                           pid: Targets.pid(of: child)) {
                task.transcriptPath = rollout.path
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
        }
        return changed
    }

    /// Close a reported child's terminal once its linger has run out. True when the record changed.
    private static func closeChild(_ task: Task) -> Bool {
        var task = task
        guard let closeAt = task.closeAt, let childID = task.childTerminalId else { return false }
        let now = Date()
        guard now >= closeAt else { return false }
        guard let child = SessionWatch.shared.targets.first(where: { $0.id == childID }),
              child.assistant == nil || child.assistant == task.assistant,
              task.childTTY == nil || child.tty == task.childTTY else {
            // Gone already, or the terminal is somebody else's now: nothing here is ours to close.
            task.closeAt = nil
            guard replaceTask(task, expecting: task.state) else { return false }
            return true
        }
        // A child still mid-turn is left alone — result.json was meant to be the last thing it
        // wrote, but a tab closed under a running turn is a mess, not an exit. Ten minutes of
        // patience, then the tab goes without the courtesy of `/exit`, because typing into a
        // menu confirms whatever is highlighted. An assistant that already left on its own
        // gets the same treatment: there is nobody in that tab to say the word to.
        let busy = childIsBusy(child)
        let overdue = now.timeIntervalSince(closeAt) > 600
        if busy, !overdue { return false }
        let justTheTab = busy || child.assistant == nil
        task.closeAt = nil
        guard replaceTask(task, expecting: task.state) else { return false }
        RemoteAuth.audit("orchestrator.close", ["task": task.id, "child": childID,
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
    private static func finalize(_ taskID: String, as outcome: State,
                                 summary: String?, artifacts: [String] = []) {
        lock.lock()
        guard var task = tasks[taskID], !task.state.isTerminal else { lock.unlock(); return }
        task.state = outcome
        task.finishedAt = Date()
        if let summary { task.summary = summary }
        if !artifacts.isEmpty { task.artifacts = artifacts }
        secrets.removeValue(forKey: taskID)
        // Only a child that reported gets its tab closed for it. One that timed out, or never
        // came up, has something on its screen worth reading, and stays.
        let linger = Config.shared.orchestratorChildLinger
        if outcome == .success || outcome == .failure, linger >= 0, task.childTerminalId != nil {
            task.closeAt = Date().addingTimeInterval(TimeInterval(linger))
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
    }

    /// One line typed into the root session, so the conversation that asked for the work is the
    /// conversation that hears it finished.
    private static func notifyRoot(_ task: Task) {
        guard Config.shared.orchestratorNotifyRoot, let rootID = task.rootSessionId else { return }
        guard let root = SessionWatch.shared.targets.first(where: {
            HookBridge.note(for: $0)?.session == rootID
        }) else { return }
        // Words into a menu confirm the highlighted row instead of typing. Skip rather than risk
        // answering a question on the root's behalf; the record is still in the app and the page.
        guard !Targets.isChoosing(root) else { return }
        let short = String(task.id.prefix(8))
        let line = "[clawdline] task \(short) (\(task.title)) finished: \(task.state.rawValue)"
            + " — read /tmp/.clawdline/\(task.id)/result.json"
        if let failure = Targets.send(line, to: root) {
            Log.write("orchestrator: could not notify the root — \(failure)")
        }
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
        - Do not read any directory under /tmp/.clawdline/ other than the ones named here.
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

    /// The maximum this app will carry into a briefing. Generous for house rules and small
    /// enough that a file somebody pasted a novel into cannot push the actual task off the
    /// bottom of a child's attention.
    static let policyLimit = 4096

    /// The same ceiling for the graph a task carries. Both end up in one briefing beside 16 KiB
    /// of instructions, and a briefing a child skims is worse than a shorter one it reads.
    static let planLimit = 4096

    /// What this Mac says about how work should be handed out, or nil when nobody has said
    /// anything. Read at dispatch rather than at launch, so an edit takes effect on the next
    /// task instead of on the next restart.
    static func policy() -> String? {
        guard let raw = try? String(contentsOf: policyURL, encoding: .utf8) else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        return String(text.prefix(policyLimit))
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

    Clawdline reads this file every time a task is dispatched and copies it into the briefing of
    every child that is allowed to dispatch in turn. Edit it freely. Delete everything and the
    feature switches itself off — an empty file means there are no house rules.

    ## Which assistant

    - **Codex** for *making* something you can then look at: writing code, generating an SVG or
      an image, running a build until it goes green, mechanical edits across many files.
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

    private static func harvestUsage(_ task: Task) -> Usage? {
        guard let path = task.transcriptPath.map(URL.init(fileURLWithPath:))
            ?? locateTranscript(task) else { return nil }
        switch task.assistant {
        case .claude: return claudeUsage(transcript: path)
        case .codex:  return codexUsage(rollout: path)
        }
    }

    private static func locateTranscript(_ task: Task) -> URL? {
        switch task.assistant {
        case .claude:
            guard let sessionID = task.childSessionId else { return nil }
            return StartPoints.projectsRoot
                .appendingPathComponent(StartPoints.slug(of: task.projectDir), isDirectory: true)
                .appendingPathComponent(sessionID + ".jsonl")
        case .codex:
            return Codex.locate(cwd: task.projectDir, startedAt: task.spawnedAt, pid: nil)
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

    private static func stored(_ task: Task) -> [String: Any] {
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
        if let v = task.childTerminalId { out["child_terminal"] = v }
        if let v = task.childBackend { out["child_backend"] = v.rawValue }
        if let v = task.childTTY { out["child_tty"] = v }
        if let v = task.childSessionId { out["child_session"] = v }
        if let v = task.transcriptPath { out["transcript"] = v }
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

    private static func task(from obj: [String: Any]) -> Task? {
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
        // A registry written before tasks had a depth holds only tasks a root dispatched, which
        // is exactly what 1 means.
        task.depth = (obj["depth"] as? Int).map { min(max($0, 1), 9) } ?? 1
        task.childTerminalId = obj["child_terminal"] as? String
        task.childBackend = (obj["child_backend"] as? String).flatMap(Backend.init(rawValue:))
        task.childTTY = obj["child_tty"] as? String
        task.childSessionId = obj["child_session"] as? String
        task.transcriptPath = obj["transcript"] as? String
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

    /// Test seam: forget everything in memory.
    static func forget() {
        lock.lock()
        tasks = [:]
        secrets = [:]
        dispatchTimes = []
        titlesByTerminal = [:]
        loaded = false
        lock.unlock()
    }
}
