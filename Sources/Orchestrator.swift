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
        var projectDir: String
        var timeoutMinutes: Int
        var created: Date
        var spawnedAt: Date?
        var briefedAt: Date?
        var finishedAt: Date?
        var rootSessionId: String?
        var rootLabel: String?
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
        var projectDir = ""
        var title = ""
        var instructions = ""
        var timeoutMinutes = 30
        var rootSessionId: String?
        var rootLabel: String?
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
        var made = Draft()
        made.id = id
        made.assistant = assistant
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

        // Depth 1. A session this app briefed as a child is not allowed to be anybody's root:
        // its declared identity is checked against every live child. Best-effort — a caller can
        // lie about who it is — but a child following its briefing declares itself truthfully,
        // and the global cap bounds what lying buys.
        if let declared = made.rootSessionId, isActiveChild(sessionID: declared) {
            return .refused(409, "depth_exceeded",
                            "A child session cannot dispatch tasks of its own.")
        }
        let cap = Config.shared.orchestratorMaxChildren
        if activeCount() >= cap {
            return .refused(status: 429, code: "over_capacity",
                            message: "All \(cap) child slots are busy; retry when one finishes.",
                            extra: ["retry_after": 60])
        }

        var task = Task(id: taskID, state: .queued, kind: made.kind, title: made.title,
                        assistant: made.assistant, projectDir: made.projectDir,
                        timeoutMinutes: made.timeoutMinutes, created: Date(),
                        rootSessionId: made.rootSessionId, rootLabel: made.rootLabel,
                        secretHash: hash(ofSecret: secret))
        lock.lock()
        tasks[taskID] = task
        secrets[taskID] = secret
        lock.unlock()
        RemoteAuth.audit("orchestrator.dispatch", ["task": taskID, "assistant": made.assistant.rawValue,
                                                   "cwd": made.projectDir, "kind": made.kind])

        // Straight away rather than on the next beat: the root is holding its breath on this
        // request, and the answer should already say whether a terminal actually opened.
        task = spawn(task)
        lock.lock()
        tasks[taskID] = task
        if task.state.isTerminal { secrets.removeValue(forKey: taskID) }
        lock.unlock()
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
        switch StartPoints.start(place, assistant: task.assistant) {
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

    private static func rateAllowed() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let now = Date()
        dispatchTimes = dispatchTimes.filter { now.timeIntervalSince($0) < 600 }
        guard dispatchTimes.count < 10 else { return false }
        dispatchTimes.append(now)
        return true
    }

    private static func activeCount() -> Int {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.filter { !$0.state.isTerminal }.count
    }

    private static func isActiveChild(sessionID: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.contains { !$0.state.isTerminal && $0.childSessionId == sessionID }
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
        if let childID = task.childTerminalId,
           let child = target(withID: childID) {
            _ = Targets.end(child)
        }
        DispatchQueue.main.async { finalize(taskID, as: .cancelled, summary: "Cancelled.") }
        // The record it answers with still says the old state — finalize runs on main a moment
        // later — so the state is overridden here for the reply alone.
        var record = existingRecord(taskID) ?? [:]
        record["state"] = State.cancelled.rawValue
        return .ok(["ok": true, "task": record])
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
        lock.lock()
        let live = tasks.values.filter { !$0.state.isTerminal || $0.closeAt != nil }
        lock.unlock()
        guard !live.isEmpty else { return }

        var changed = false
        for task in live {
            switch task.state {
            case .spawning: changed = brief(task) || changed
            case .briefed:  changed = watch(task) || changed
            default: changed = closeChild(task) || changed
            }
        }
        if fromTimer, live.contains(where: { $0.state == .spawning }) {
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
    private static func brief(_ task: Task) -> Bool {
        var task = task
        guard let spawnedAt = task.spawnedAt else { return false }
        if Date().timeIntervalSince(spawnedAt) > 120 {
            lock.lock(); tasks[task.id] = task; lock.unlock()
            finalize(task.id, as: .spawnFailed,
                     summary: "The child session did not become ready within two minutes.")
            return false // finalize saved and broadcast already
        }
        guard let childID = task.childTerminalId,
              let child = SessionWatch.shared.targets.first(where: { $0.id == childID }),
              child.assistant == task.assistant else { return false }
        if task.childTTY == nil {
            task.childTTY = child.tty
            lock.lock(); tasks[task.id] = task; lock.unlock()
        }
        // A brand-new session in a directory the assistant has not been told to trust opens on a
        // dialog, and text sent into a dialog confirms whatever is highlighted. The root asked
        // for work in exactly this directory, so the default answer is taken — once, and written
        // down.
        if Targets.isChoosing(child) {
            if !task.answeredMenu {
                task.answeredMenu = true
                lock.lock(); tasks[task.id] = task; lock.unlock()
                _ = Targets.answer(0x31, to: child)
                RemoteAuth.audit("orchestrator.menu", ["task": task.id, "answer": "1"])
            }
            return true
        }
        guard SessionWatch.shared.states[childID] == .idle else { return false }
        guard let secret = heldSecret(task.id) else {
            finalize(task.id, as: .spawnFailed, summary: "The task's secret was lost before briefing.")
            return false
        }
        writeChildBrief(for: task)
        let line = firstLine(id: task.id, secret: secret, announce: L.t.childAnnounce(task.title))
        if let failure = Targets.send(line, to: child) {
            task.injectAttempts += 1
            if task.injectAttempts >= 5 {
                lock.lock(); tasks[task.id] = task; lock.unlock()
                finalize(task.id, as: .spawnFailed, summary: "Could not type into the child: \(failure)")
                return false
            }
            lock.lock(); tasks[task.id] = task; lock.unlock()
            return true
        }
        task.state = .briefed
        task.briefedAt = Date()
        task.lastSeenChild = Date()
        lock.lock()
        tasks[task.id] = task
        secrets.removeValue(forKey: task.id)
        lock.unlock()
        RemoteAuth.audit("orchestrator.brief", ["task": task.id, "child": task.childTerminalId ?? "?"])
        return true
    }

    /// Watch a briefed child for its result, its death, or its deadline. True when the record
    /// changed in a way worth persisting.
    private static func watch(_ task: Task) -> Bool {
        var task = task

        // The result file is the completion signal a sandboxed child can always give — writing
        // to /tmp needs no network approval, where a curl to loopback does.
        if let result = readResult(of: task) {
            lock.lock(); tasks[task.id] = task; lock.unlock()
            finalize(task.id, as: result.status == "success" ? .success : .failure,
                     summary: result.summary, artifacts: result.artifacts)
            return false
        }

        if let briefedAt = task.briefedAt,
           Date().timeIntervalSince(briefedAt) > Double(task.timeoutMinutes) * 60 {
            lock.lock(); tasks[task.id] = task; lock.unlock()
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
            if task.childSessionId == nil {
                switch task.assistant {
                case .claude:
                    if let noted = HookBridge.note(for: child)?.session {
                        task.childSessionId = noted
                        task.transcriptPath = StartPoints.projectsRoot
                            .appendingPathComponent(StartPoints.slug(of: task.projectDir), isDirectory: true)
                            .appendingPathComponent(noted + ".jsonl").path
                        changed = true
                    }
                case .codex:
                    if let rollout = Codex.locate(cwd: task.projectDir, startedAt: task.spawnedAt,
                                                  pid: Targets.pid(of: child)) {
                        task.transcriptPath = rollout.path
                        task.childSessionId = Codex.head(of: rollout)?.id
                        changed = true
                        // The thread gets the task's name too, so `codex resume` lists it by
                        // what it did rather than by the first line it was handed.
                        if let threadID = task.childSessionId, !threadID.isEmpty {
                            CodexNaming.shared.name(task.title, thread: threadID, target: child)
                        }
                    }
                }
            }
            lock.lock(); tasks[task.id] = task; lock.unlock()
        } else if let seen = task.lastSeenChild {
            if Date().timeIntervalSince(seen) > 60 {
                lock.lock(); tasks[task.id] = task; lock.unlock()
                finalize(task.id, as: .failure,
                         summary: "The child session ended without reporting a result.")
                return false
            }
        } else {
            task.lastSeenChild = Date()
            lock.lock(); tasks[task.id] = task; lock.unlock()
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
            lock.lock(); tasks[task.id] = task; lock.unlock()
            return true
        }
        // A child still mid-turn is left alone — result.json was meant to be the last thing it
        // wrote, but a tab closed under a running turn is a mess, not an exit. Ten minutes of
        // patience, then the tab goes without the courtesy of `/exit`, because typing into a
        // menu confirms whatever is highlighted. An assistant that already left on its own
        // gets the same treatment: there is nobody in that tab to say the word to.
        var busy = Targets.isChoosing(child)
        if case .working? = SessionWatch.shared.states[childID] { busy = true }
        let overdue = now.timeIntervalSince(closeAt) > 600
        if busy, !overdue { return false }
        let justTheTab = busy || child.assistant == nil
        task.closeAt = nil
        lock.lock(); tasks[task.id] = task; lock.unlock()
        RemoteAuth.audit("orchestrator.close", ["task": task.id, "child": childID,
                                                 "how": justTheTab ? "tab" : "exit"])
        // Off the main thread: `end` types the quit word, waits for it to land, then closes the
        // tab, and a second of that on the main thread is a second the panel does not draw.
        DispatchQueue.global(qos: .utility).async {
            let failure: String?
            if justTheTab {
                switch child.backend {
                case .iterm: failure = ITerm.close(child.id)
                case .tmux:  failure = Tmux.close(child.id)
                }
            } else {
                failure = Targets.end(child)
            }
            if let failure { Log.write("orchestrator: could not close the child — \(failure)") }
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
        }
        return true
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

    static func childBrief(for task: Task) -> String {
        let dir = "/tmp/.clawdline/\(task.id)"
        return """
        # Clawdline child briefing — task \(task.id)

        You are a CHILD session working for a Clawdline root session. Your one job is the task
        described in \(dir)/task.json — read that file now.

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
        - Do not dispatch Clawdline tasks of your own. Do not read any other directory under
          /tmp/.clawdline/. Do not do work the task did not ask for.
        - You have \(task.timeoutMinutes) minutes before the task is marked timed out.

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

        Use "status": "failure" when you could not do it. Write the file atomically: write to
        result.json.tmp first, then `mv result.json.tmp result.json`. Write it LAST — the moment
        it exists your work is considered finished.

        Optionally, if outbound network is permitted in your sandbox, you may ALSO announce it:
        `curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/complete \\
           -H "X-Clawdline-Task-Secret: <TASK_SECRET>" -H 'Content-Type: application/json' \\
           -d '{"status":"success","summary":"..."}'`
        This is never required; the file alone is enough.
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
        if let rootID = task.rootSessionId {
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
            "dir": "/tmp/.clawdline/\(task.id)",
        ]
        if let at = task.spawnedAt { out["spawnedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.briefedAt { out["briefedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.finishedAt { out["finishedAt"] = Int(at.timeIntervalSince1970) }
        var root: [String: Any] = [:]
        if let id = task.rootSessionId { root["sessionId"] = id }
        if let label = task.rootLabel { root["label"] = label }
        if let terminal = rootTerminal { root["terminalId"] = terminal }
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
            "secret_hash": task.secretHash,
            "artifacts": task.artifacts,
        ]
        if let at = task.spawnedAt { out["spawned_at"] = at.timeIntervalSince1970 }
        if let at = task.briefedAt { out["briefed_at"] = at.timeIntervalSince1970 }
        if let at = task.finishedAt { out["finished_at"] = at.timeIntervalSince1970 }
        if let v = task.rootSessionId { out["root_session"] = v }
        if let v = task.rootLabel { out["root_label"] = v }
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
