import CryptoKit
import Foundation

/// What a dispatch body says before anything is spawned from it, and every admission answer that
/// can be given without reading the registry.
///
/// `Orchestrator` owns the collections and the lock. This namespace owns the layer in front of
/// them: the ``Draft`` a `task.json` decodes into, the four ingress refusals, the git worktree a
/// draft asks for, and the claim and workspace scans that compare one candidate against a table
/// of tasks **handed to them**. Nothing here touches a shared mutable collection and nothing here
/// takes a lock, which is the whole of why it can live in its own file.
///
/// Three declarations from the same section stayed behind in `Orchestrator` because they fail
/// exactly that test: `serializeBlockersLocked` and `claimsOverlapsLocked` read `tasks` under the
/// `…Locked()` contract, and `workspaceOverlaps(for:)` takes `lock` itself. They call into this
/// file for the pure half and keep the ownership, which is the boundary being honest rather than
/// convenient.
///
/// It is a separate namespace rather than an `extension Orchestrator` in another file on purpose:
/// an extension would move the text without moving the dependency. Writing `Orchestrator.Task`,
/// `Orchestrator.Reply` and `Orchestrator.Worktree` in full at every crossing is that boundary
/// becoming visible, and the verbosity is the point rather than a cost of it.
///
/// **The wire contract is here.** Every refusal code, message string, field name and legacy-shape
/// branch below is what an already-running root and an already-briefed child are speaking; a
/// tidier spelling is a protocol break, not a cleanup.
enum OrchestratorDraft {

    /// Everything a task.json has to say before anything is spawned from it. Pure, so a test can
    /// hand it a dictionary and a pretend filesystem.
    struct Draft: Equatable {
        var id = ""
        var kind = "custom"
        var assistant = Assistant.claude
        var model: String?
        /// A per-dispatch Codex override. Nil deliberately means no CLI config flag, preserving
        /// both Codex's model default and the user's own configuration.
        var reasoningEffort: ReasoningEffort?
        /// What the task asked for, before the ceiling. Nil means it did not ask, and takes the
        /// ceiling itself — the setting is the default as well as the limit.
        var permission: Permission?
        var projectDir = ""
        var title = ""
        var instructions = ""
        var timeoutMinutes = 30
        var rootSessionId: String?
        var rootAssistant: Assistant?
        var rootLabel: String?
        var parentTaskId: String?
        /// An explicit detached API dispatch. Without this opt-in, a root-less HTTP request is
        /// almost certainly a caller that forgot to identify itself and would never receive the
        /// completion notice or own the child row.
        var pollOnly = false
        var plan: String?
        var graph: Orchestrator.PlanningGraph?
        var serialize: [String] = []
        var claims: [String] = []
        var claimsDeclared = false
        var isolation = Orchestrator.Isolation.none
        var isolationBase: String?
        /// The Q1 design's §D.3 override: send this task even though the assistant it named
        /// reads `exhausted`. A reading can be stale, wrong, or about to be moot — a task whose
        /// window opens after the account's own reset has nothing to lose. Never widens anything
        /// else: `unknown` and `ok` already dispatch without this, and `low` only ever warns.
        var ignoreQuota = false
        var attachSessionId: String?
    }

    /// Positive evidence that a caller put a physical terminal id where the task protocol needs
    /// the assistant process's conversation id. Its assistant is an observed fact, not filtered
    /// through the caller's label. Empty or conflicting evidence is deliberately inconclusive;
    /// the owned-child HTTP boundary refuses an unresolved owner rather than changing modes.
    struct RootIdentityEvidence: Equatable {
        let source: String
        let terminalID: String
        let canonicalSessionID: String
        let assistant: Assistant
    }

    static func rootIdentityRefusal(claimed: String?,
                                    evidence: [RootIdentityEvidence]) -> Orchestrator.Reply? {
        guard let claimed, !claimed.isEmpty else { return nil }
        let matching = evidence.filter {
            $0.terminalID == claimed && $0.canonicalSessionID != claimed
        }
        let tuples = Set(matching.map {
            $0.canonicalSessionID + "\u{0}" + $0.assistant.rawValue
        })
        guard tuples.count == 1, let proof = matching.first else { return nil }
        return .refused(
            status: 422, code: "root_identity_is_terminal",
            message: "root.session_id is a physical terminal id; use the assistant process-bound "
                + "conversation id returned as canonical_root_session_id, verify it through "
                + "GET /v1/orchestrator/whoami, and resend the owned child dispatch.",
            extra: [
                "supplied_root_session_id": claimed,
                "canonical_root_session_id": proof.canonicalSessionID,
                "canonical_root_assistant": proof.assistant.rawValue,
                "evidence": matching.map(\.source).sorted(),
            ])
    }

    /// After the ingress door selects owned-child or detached mode, enforce the remaining owner
    /// requirement. Kept pure so refusal is proved without registration or terminal open.
    static func rootSessionRequirementRefusal(sessionID: String?, pollOnly: Bool)
        -> Orchestrator.Reply? {
        guard sessionID == nil, !pollOnly else { return nil }
        return .refused(
            status: 422, code: "root_session_required",
            message: "root.session_id is required for API dispatch so the child can be "
                + "grouped, closed and reported back to its owner. Resolve this interactive "
                + "Root with GET /v1/orchestrator/whoami, then resend with its current "
                + "process-bound conversation id and assistant.",
            extra: [:])
    }

    /// Interactive ownership and unattended automation are different ingress primitives. A
    /// generic `poll_only` switch on the ordinary child route made an identity lookup failure look
    /// like a valid detached decision; by the time the caller noticed, a real executor was already
    /// working with no completion owner. The route selects the mode now, before registration or a
    /// terminal starter can run.
    static func dispatchDoorRefusal(sessionID: String?, pollOnly: Bool,
                                    allowDetachedAutomation: Bool) -> Orchestrator.Reply? {
        if allowDetachedAutomation {
            guard sessionID != nil || !pollOnly else { return nil }
            return .refused(
                status: 422, code: "detached_task_required",
                message: "The detached automation route requires root.session_id null and "
                    + "root.poll_only true. Owned Root-to-Child work belongs on "
                    + "POST /v1/orchestrator/tasks after resolving the Root with "
                    + "GET /v1/orchestrator/whoami.",
                extra: [:])
        }
        guard pollOnly else { return nil }
        return .refused(
            status: 422, code: "detached_route_required",
            message: "POST /v1/orchestrator/tasks creates an owned Child and never accepts "
                + "root.poll_only. Resolve this interactive Root with "
                + "GET /v1/orchestrator/whoami and resend with root.session_id plus "
                + "root.assistant. Only unattended automation may use poll-only, through "
                + "POST /v1/orchestrator/detached-tasks.",
            extra: [:])
    }

    /// New ordinary HTTP dispatch must bind both halves of its owner tuple. A missing assistant
    /// used to fall through ``canonicalRootSession``'s historical Claude default, silently
    /// turning an omitted wire field into ownership. Persisted rows keep that compatibility;
    /// this is an ingress-only refusal and detached polling remains explicitly ownerless.
    static func rootAssistantRequirementRefusal(sessionID: String?, pollOnly: Bool,
                                                 assistant: Assistant?) -> Orchestrator.Reply? {
        guard sessionID != nil, !pollOnly, assistant == nil else { return nil }
        return .refused(
            status: 422, code: "root_assistant_required",
            message: "root.assistant is required when root.session_id names an owner; send "
                + "claude or codex. The historical Claude fallback exists only in persisted "
                + "legacy compatibility readers, not ownership decisions.",
            extra: [:])
    }

    /// A live task whose working directory intersects the one being dispatched. The task is a
    /// value snapshot: warning is advisory, so a task finishing while the new tab opens does not
    /// turn a truthful observation at dispatch time into a reason to change the reply.
    struct WorkspaceOverlap {
        let task: Orchestrator.Task
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
        let task: Orchestrator.Task
        let paths: [String]
        let sameRoot: Bool
        let rootsKnown: Bool
        let rootLabel: String?
        /// The blocking task's own canonical root key, before hashing — nil exactly when that
        /// task's root could not itself be resolved, independent of whether the *pair* counts
        /// as `rootsKnown`. See `OrchestratorDraft.rootKeyDigest`.
        let rootKey: String?

        var blocks: Bool { rootsKnown && !sameRoot }

        func warning(for newTaskID: String, now: Date = Date()) -> [String: Any] {
            [
                "code": rootsKnown ? "claims_overlap" : "claims_overlap_unknown_root",
                "task": task.id,
                "paths": paths,
                "message": "Task \(newTaskID) shares claimed paths with task \(task.id): "
                    + paths.joined(separator: ", ") + ".",
                "age_seconds": max(0, Int(now.timeIntervalSince(task.created))),
                "root_key": rootKey.map(OrchestratorDraft.rootKeyDigest) as Any? ?? NSNull(),
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

    enum AttachmentDecision: Equatable {
        case accepted(TargetSession, depth: Int)
        case refused(status: Int, code: String, message: String)
    }

    /// Resolve against the full watched Session inventory, which is intentionally wider than the
    /// terminal-neutral address book published by the orchestrator route. Every refusal happens
    /// before registration or terminal input.
    ///
    /// `excluding` is the id of the task this decision is *for*. Single-flight is a rule about
    /// two tasks, and a task is not the other one: the serialize queue writes a task into the
    /// registry as `spawning` before it opens anything, so re-resolving without this made every
    /// attached task that also named a `serialize` token refuse itself with
    /// `attach_session_occupied` — at a moment when the HTTP response that would have carried
    /// the error was already sent.
    static func attachmentDecision(
        sessionID: String, assistant: Assistant,
        sessions: [TargetSession], states: [String: SessionState],
        tasks: [Orchestrator.Task], roles: [String: Orchestrator.Role],
        isChoosing: (TargetSession) -> Bool,
        excluding excludedTaskID: String? = nil
    ) -> AttachmentDecision {
        guard let session = sessions.first(where: { $0.id == sessionID }) else {
            return .refused(status: 404, code: "attach_session_not_found",
                            message: "No session named by attach_session is currently available.")
        }
        guard let resident = session.assistant else {
            return .refused(status: 409, code: "attach_unsupported",
                            message: "attach_session names a plain shell with no assistant.")
        }
        // A standing host needs two launch-time facts: Clawdline opened its tab for a task, and
        // that process was given the whole task root. A leaf gets only its original task
        // directory, so it cannot read a new follow-up's sibling CHILD.md even though Clawdline
        // opened it. Persist the actual grant instead of inferring it from depth: the configured
        // floor can change while a tab remains standing, but a process's `--add-dir` cannot.
        guard let role = roles[sessionID], role.taskRootAccess else {
            return .refused(status: 409, code: "attach_not_managed",
                            message: "attach_session names a session without Clawdline's "
                                   + "launch-time task-root access; it cannot read a new "
                                   + "follow-up task's CHILD.md.")
        }
        guard resident == assistant else {
            return .refused(status: 409, code: "attach_assistant_mismatch",
                            message: "The task assistant differs from the attached session's assistant.")
        }
        if tasks.contains(where: {
            $0.id != excludedTaskID && !$0.state.isTerminal
                && ($0.childTerminalId == sessionID || $0.attachSessionId == sessionID)
        }) {
            return .refused(status: 409, code: "attach_session_occupied",
                            message: "That session already has a live Clawdline task.")
        }
        if states[sessionID] == .waiting, isChoosing(session) {
            return .refused(status: 409, code: "attach_session_busy",
                            message: "That session is showing a menu; no briefing was typed.")
        }
        return .accepted(session, depth: role.depth)
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
        var reasoningEffort: ReasoningEffort?
        if let raw = obj["reasoning_effort"] {
            guard assistant == .codex else {
                return .bad("reasoning_effort is only valid when assistant is codex")
            }
            guard let name = raw as? String,
                  let value = ReasoningEffort(rawValue: name) else {
                return .bad("reasoning_effort must be one of: "
                          + ReasoningEffort.allCases.map(\.rawValue).joined(separator: ", "))
            }
            reasoningEffort = value
        }
        if let plan = obj["plan"] as? String, plan.utf8.count > Orchestrator.planLimit {
            return .bad("plan must be at most \(Orchestrator.planLimit / 1024) KiB")
        }
        var graph: Orchestrator.PlanningGraph?
        if let rawGraph = obj["graph"] {
            let parsed = Orchestrator.planningGraph(from: rawGraph)
            if let error = parsed.error { return .bad(error) }
            graph = parsed.graph
        }
        var isolation = Orchestrator.Isolation.none
        if let raw = obj["isolation"] {
            guard let name = raw as? String,
                  let value = Orchestrator.Isolation(rawValue: name) else {
                return .bad("isolation must be one of: none, worktree")
            }
            isolation = value
        }
        var isolationBase: String?
        if let raw = obj["isolation_base"] {
            guard isolation == .worktree else {
                return .bad("isolation_base is only valid when isolation is worktree")
            }
            guard let name = raw as? String, validIsolationBase(name) else {
                return .bad("isolation_base must be a 1–200 character Git revision using "
                          + "letters, digits, . _ / - or ~; it cannot begin with - or contain ..")
            }
            isolationBase = name
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
        var attachSessionId: String?
        if let raw = obj["attach_session"] {
            guard let id = raw as? String, !id.isEmpty, id.count <= 512 else {
                return .bad("attach_session must be a non-empty session id of at most 512 characters")
            }
            attachSessionId = id
        }
        var made = Draft()
        made.id = id
        made.assistant = assistant
        made.model = model
        made.reasoningEffort = reasoningEffort
        made.permission = permission
        made.serialize = serialize
        made.claims = claims
        made.claimsDeclared = claimsDeclared
        made.isolation = isolation
        made.isolationBase = isolationBase
        made.ignoreQuota = obj["ignore_quota"] as? Bool ?? false
        made.attachSessionId = attachSessionId
        made.plan = (obj["plan"] as? String).flatMap {
            let text = $0.trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? nil : text
        }
        made.graph = graph
        made.projectDir = dir
        made.instructions = instructions
        made.kind = (obj["kind"] as? String).flatMap { $0.isEmpty ? nil : String($0.prefix(40)) } ?? "custom"
        made.title = String(((obj["title"] as? String) ?? "task").prefix(200))
        if let minutes = obj["timeout_minutes"] as? Int {
            guard (1...240).contains(minutes) else { return .bad("timeout_minutes must be 1…240") }
            made.timeoutMinutes = minutes
        }
        let rootObj = obj["root"] as? [String: Any] ?? [:]
        made.rootSessionId = (rootObj["session_id"] as? String).flatMap {
            $0.isEmpty ? nil : $0
        }
        if let rawAssistant = rootObj["assistant"], !(rawAssistant is NSNull) {
            guard let name = rawAssistant as? String,
                  let assistant = Assistant(rawValue: name) else {
                return .bad("root.assistant must be claude or codex")
            }
            made.rootAssistant = assistant
        }
        made.rootLabel = (rootObj["label"] as? String).map { String($0.prefix(120)) }
        if let raw = rootObj["poll_only"] {
            guard let pollOnly = raw as? Bool else {
                return .bad("root.poll_only must be true or false")
            }
            made.pollOnly = pollOnly
        }
        if made.pollOnly, made.rootSessionId != nil {
            return .bad("root.poll_only is only valid when root.session_id is null")
        }
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

    /// 32 bytes written as lower-case hex, which is what every task secret on this Mac is —
    /// `openssl rand -hex 32` in the briefing, `freshTaskSecret()` when the broker mints one.
    static func isTaskSecret(_ secret: String) -> Bool {
        secret.count == 64 && secret.allSatisfy { ("a"..."f").contains($0) || $0.isNumber }
    }

    static func validIsolationBase(_ value: String) -> Bool {
        guard !value.isEmpty, value.count <= 200, !value.hasPrefix("-"), !value.contains("..")
        else { return false }
        return value.allSatisfy {
            ("a"..."z").contains($0) || ("A"..."Z").contains($0)
                || ("0"..."9").contains($0) || $0 == "." || $0 == "_" || $0 == "/"
                || $0 == "-" || $0 == "~"
        }
    }

    static func worktreeBranch(for taskID: String) -> String? {
        isTaskID(taskID) ? "clawdline/task/\(taskID)" : nil
    }

    /// The same identity spelling used by frozen claims: standardise, then follow symlinks once
    /// while the repository exists. Every worktree path and stored repository value starts here.
    static func canonicalFilesystemPath(_ path: String) -> String {
        URL(fileURLWithPath: path).standardizedFileURL.resolvingSymlinksInPath().path
    }

    private static func worktreeRepositorySlug(_ project: String) -> String {
        let canonical = canonicalFilesystemPath(project)
        let basename = URL(fileURLWithPath: canonical).lastPathComponent.lowercased()
        var readable = String(basename.unicodeScalars.map { scalar -> Character in
            let value = scalar.value
            if (97...122).contains(value) || (48...57).contains(value) {
                return Character(String(scalar))
            }
            return "-"
        }.prefix(32))
        if readable.isEmpty { readable = "repo" }
        let digest = SHA256.hash(data: Data(canonical.utf8))
            .map { String(format: "%02x", $0) }.joined()
        return readable + "-" + String(digest.prefix(8))
    }

    static var worktreeRoot: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/Clawdline/worktrees",
                                    isDirectory: true)
    }

    static func worktreePath(project: String, taskID: String) -> String? {
        guard isTaskID(taskID) else { return nil }
        return worktreeRoot
            .appendingPathComponent(worktreeRepositorySlug(project), isDirectory: true)
            .appendingPathComponent(taskID, isDirectory: true).path
    }

    struct GitAnswer {
        var output: String
        var status: Int32
    }

    /// The only git execution seam for worktree lifecycle operations. Arguments never pass
    /// through a shell, optional locks are disabled, and a wedged repository cannot hold the
    /// broker queue indefinitely.
    static func git(_ arguments: [String], cwd: String,
                    gitDirectory: String? = nil,
                    timeout: TimeInterval = 15) -> GitAnswer? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = gitDirectory.map { ["--git-dir", $0] + arguments } ?? arguments
        process.currentDirectoryURL = URL(fileURLWithPath: cwd, isDirectory: true)
        var environment = ProcessInfo.processInfo.environment
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        process.environment = environment
        let pipe = Pipe()
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = pipe
        process.standardError = pipe
        do { try process.run() } catch { return nil }
        let killer = DispatchWorkItem { if process.isRunning { process.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout,
                                                       execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        process.waitQuietly()
        killer.cancel()
        return GitAnswer(output: String(data: data, encoding: .utf8) ?? "",
                         status: process.terminationStatus)
    }

    /// Stable repository identity for linked worktrees. The returned path is Git's common
    /// directory, not whichever disposable checkout happened to be the caller's cwd.
    static func gitCommonDirectory(at cwd: String) -> String? {
        guard let answer = git(["rev-parse", "--git-common-dir"], cwd: cwd),
              answer.status == 0 else { return nil }
        let raw = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty else { return nil }
        let absolute = raw.hasPrefix("/") ? raw
            : URL(fileURLWithPath: cwd, isDirectory: true).appendingPathComponent(raw).path
        let canonical = canonicalFilesystemPath(absolute)
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: canonical, isDirectory: &isDirectory),
              isDirectory.boolValue else { return nil }
        return canonical
    }

    private static func usableGitDirectory(_ stored: String) -> String? {
        let canonical = canonicalFilesystemPath(stored)
        guard git(["rev-parse", "--git-dir"], cwd: "/",
                  gitDirectory: canonical)?.status == 0 else { return nil }
        return canonical
    }

    /// A deleted legacy project_dir may still name one of the broker's own worktree slots. Only
    /// that exact bounded shape is eligible for migration; an arbitrary missing path is not.
    private static func brokerWorktreeRepositorySlug(containing path: String) -> String? {
        let root = canonicalFilesystemPath(worktreeRoot.path)
        let candidate = canonicalFilesystemPath(path)
        let rootParts = URL(fileURLWithPath: root).pathComponents
        let parts = URL(fileURLWithPath: candidate).pathComponents
        guard parts.count >= rootParts.count + 2,
              Array(parts.prefix(rootParts.count)) == rootParts else { return nil }
        let suffix = parts.dropFirst(rootParts.count)
        guard let slug = suffix.first, !slug.isEmpty,
              let taskID = suffix.dropFirst().first, isTaskID(String(taskID)) else { return nil }
        return String(slug)
    }

    /// Independently re-derive repository identities from still-readable local evidence in the
    /// same 0600 registry. The broker slug binds a deleted worktree path to the canonical
    /// repository path that created it; collisions or contradictory candidates fail closed.
    private static func legacyRepositoryCommonDirectory(
        forDeletedProjectDir projectDir: String, among evidence: [Orchestrator.Task]
    ) -> String? {
        guard let wantedSlug = brokerWorktreeRepositorySlug(containing: projectDir) else {
            return nil
        }
        var candidates: Set<String> = []
        for peer in evidence {
            guard let worktree = peer.worktree,
                  worktree.path == worktreePath(project: worktree.repository, taskID: peer.id),
                  worktree.branch == worktreeBranch(for: peer.id),
                  worktreeRepositorySlug(worktree.repository) == wantedSlug,
                  let derived = gitCommonDirectory(at: worktree.repository) else { continue }
            let stored = peer.repositoryCommonDir ?? worktree.repositoryCommonDir
            if let stored, let usable = usableGitDirectory(stored), usable != derived { return nil }
            candidates.insert(derived)
        }
        guard candidates.count == 1 else { return nil }
        return candidates.first
    }

    /// Select the repository identity that a landing may trust. New tasks persist one receipt
    /// regardless of isolation. Live project/worktree paths and the bounded legacy derivation are
    /// independent evidence: every available source must agree, and an obsolete stored absolute
    /// path may fall back only when one of those sources proves the same repository locally.
    static func landingGitDirectory(for task: Orchestrator.Task,
                                    among evidence: [Orchestrator.Task]) -> String? {
        var projectIsDirectory: ObjCBool = false
        let projectExists = FileManager.default.fileExists(
            atPath: task.projectDir, isDirectory: &projectIsDirectory)
        let projectCommon = projectExists && projectIsDirectory.boolValue
            ? gitCommonDirectory(at: task.projectDir) : nil
        if projectExists, projectCommon == nil { return nil }

        var derived: Set<String> = []
        if let projectCommon { derived.insert(projectCommon) }

        if task.isolation == .worktree {
            guard let worktree = task.worktree,
                  worktree.path == worktreePath(project: worktree.repository, taskID: task.id),
                  worktree.branch == worktreeBranch(for: task.id) else { return nil }
            var repositoryIsDirectory: ObjCBool = false
            let repositoryExists = FileManager.default.fileExists(
                atPath: worktree.repository, isDirectory: &repositoryIsDirectory)
            let repositoryCommon = repositoryExists && repositoryIsDirectory.boolValue
                ? gitCommonDirectory(at: worktree.repository) : nil
            if repositoryExists, repositoryCommon == nil { return nil }
            if let repositoryCommon { derived.insert(repositoryCommon) }
        }

        if !projectExists,
           let legacy = legacyRepositoryCommonDirectory(
                forDeletedProjectDir: task.projectDir, among: evidence) {
            derived.insert(legacy)
        }

        guard derived.count <= 1 else { return nil }
        let independentlyDerived = derived.first
        let storedPath = task.repositoryCommonDir ?? task.worktree?.repositoryCommonDir
        let stored = storedPath.flatMap(usableGitDirectory)
        if let stored, let independentlyDerived, stored != independentlyDerived { return nil }
        if let stored { return stored }
        // A stale absolute receipt is not itself fallback evidence. Reaching this return requires
        // a live project/repository or the uniquely matched broker-owned legacy slug above.
        return independentlyDerived
    }

    /// Resolve a landing inside the task's own repository and prove that the named commit is in
    /// the named *local* target branch. All arguments reach git without a shell; canonical object
    /// ids are what survive into the registry, not caller-supplied revision expressions.
    static func verifyTargetLanding(projectDir: String, target: String,
                                    commit: String) -> Orchestrator.LandingVerification? {
        guard let common = gitCommonDirectory(at: projectDir) else { return nil }
        return verifyTargetLanding(gitDirectory: common, target: target, commit: commit)
    }

    private static func verifyTargetLanding(gitDirectory: String, target: String,
                                            commit: String) -> Orchestrator.LandingVerification? {
        guard let branchCheck = git(["check-ref-format", "--branch", target], cwd: "/",
                                    gitDirectory: gitDirectory),
              branchCheck.status == 0 else { return nil }
        let targetRef = "refs/heads/\(target)"
        guard let resolvedCommit = git(
                ["rev-parse", "--verify", "--end-of-options", "\(commit)^{commit}"],
                cwd: "/", gitDirectory: gitDirectory), resolvedCommit.status == 0,
              let resolvedTarget = git(
                ["rev-parse", "--verify", "--end-of-options", "\(targetRef)^{commit}"],
                cwd: "/", gitDirectory: gitDirectory), resolvedTarget.status == 0 else { return nil }
        let commitID = resolvedCommit.output.trimmingCharacters(in: .whitespacesAndNewlines)
        let targetID = resolvedTarget.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard [commitID, targetID].allSatisfy({ id in
            (id.count == 40 || id.count == 64)
                && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }) else { return nil }
        guard let contained = git(["merge-base", "--is-ancestor", commitID, targetID],
                                  cwd: "/", gitDirectory: gitDirectory),
              contained.status == 0 else { return nil }
        return Orchestrator.LandingVerification(origin: "local_target_branch", commit: commitID,
                                   targetCommit: targetID)
    }

    enum WorktreePreparation {
        case ready(Orchestrator.Worktree, warnings: [[String: Any]])
        case bad(String)
        case unavailable(String)
    }

    private static func filesystemFreeBytes(at path: String) -> Int64? {
        if let value = try? URL(fileURLWithPath: path).resourceValues(
            forKeys: [.volumeAvailableCapacityForImportantUsageKey]
        ).volumeAvailableCapacityForImportantUsage {
            return value
        }
        let attributes = try? FileManager.default.attributesOfFileSystem(forPath: path)
        return (attributes?[.systemFreeSize] as? NSNumber)?.int64Value
    }

    static func relativePath(from root: String, to child: String) -> String? {
        let rootParts = URL(fileURLWithPath: canonicalFilesystemPath(root)).pathComponents
        let childParts = URL(fileURLWithPath: canonicalFilesystemPath(child)).pathComponents
        guard childParts.count >= rootParts.count,
              Array(childParts.prefix(rootParts.count)) == rootParts else { return nil }
        return childParts.dropFirst(rootParts.count).joined(separator: "/")
    }

    static func prepareWorktree(for draft: Draft, taskID: String,
                                queued: Bool) -> WorktreePreparation {
        guard let top = git(["rev-parse", "--show-toplevel"], cwd: draft.projectDir),
              top.status == 0 else {
            return .bad("isolation:\"worktree\" needs project_dir to be inside a Git repository.")
        }
        let repository = top.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard StartPoints.usable(repository) else {
            return .bad("isolation:\"worktree\" could not resolve the repository containing project_dir.")
        }
        let requested = draft.isolationBase ?? "HEAD"
        guard let resolved = git(["rev-parse", "--verify", "\(requested)^{commit}"], cwd: repository),
              resolved.status == 0 else {
            if draft.isolationBase == nil {
                return .unavailable("This repository has no commit to use as a worktree base.")
            }
            return .bad("isolation_base does not resolve to a commit in project_dir.")
        }
        let base = resolved.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let branch = worktreeBranch(for: taskID),
              let path = worktreePath(project: repository, taskID: taskID) else {
            return .bad("task_id cannot name a worktree branch or path.")
        }
        let canonicalProject = canonicalFilesystemPath(draft.projectDir)
        let canonicalRepository = canonicalFilesystemPath(repository)
        guard let relative = relativePath(from: canonicalRepository, to: canonicalProject) else {
            return .bad("project_dir is not inside its resolved Git repository.")
        }
        let childCwd = relative.isEmpty ? path
            : URL(fileURLWithPath: path, isDirectory: true).appendingPathComponent(relative).path

        let home = FileManager.default.homeDirectoryForCurrentUser.path
        guard let free = filesystemFreeBytes(at: home), free >= 2_000_000_000 else {
            return .unavailable("The worktree volume needs at least 2 GB of available space.")
        }
        let status = git(["status", "--porcelain", "--untracked-files=all"], cwd: repository)
        let dirty = status?.status == 0
            ? status!.output.split(whereSeparator: \.isNewline).count : 0
        var warnings: [[String: Any]] = []
        if dirty > 0 {
            let message = queued
                ? "The base tree currently has \(dirty) uncommitted files; the clean checkout "
                    + "created when this queued task starts will not contain them."
                : "The base tree has \(dirty) uncommitted files; the worktree starts from commit "
                    + "\(String(base.prefix(7))) and does not contain them."
            warnings.append([
                "code": "dirty_worktree_base",
                "message": message,
            ])
        }
        let gitDirectory = git(["rev-parse", "--git-dir"], cwd: repository)?.output
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if let gitDirectory, !gitDirectory.isEmpty {
            let absoluteGit = gitDirectory.hasPrefix("/") ? gitDirectory
                : URL(fileURLWithPath: repository).appendingPathComponent(gitDirectory).path
            let markers = ["MERGE_HEAD", "rebase-merge", "rebase-apply", "BISECT_LOG"]
            let inProgress = markers.filter {
                FileManager.default.fileExists(atPath:
                    URL(fileURLWithPath: absoluteGit).appendingPathComponent($0).path)
            }
            if !inProgress.isEmpty {
                warnings.append([
                    "code": "git_operation_in_progress",
                    "message": "The base repository has an operation in progress ("
                        + inProgress.joined(separator: ", ") + "); integrating the branch may wait.",
                ])
            }
        }
        guard let repositoryCommonDir = gitCommonDirectory(at: canonicalRepository) else {
            return .bad("isolation:\"worktree\" could not resolve the repository's durable "
                + "Git identity.")
        }
        var worktree = Orchestrator.Worktree(path: path, branch: branch, base: base,
                                repository: canonicalRepository, cwd: childCwd)
        worktree.repositoryCommonDir = repositoryCommonDir
        worktree.baseDirty = dirty
        worktree.requestedBase = requested
        return .ready(worktree, warnings: warnings)
    }

    static func resolveSpawnBase(in worktree: Orchestrator.Worktree) -> Orchestrator.Worktree? {
        guard let answer = git(["rev-parse", "--verify",
                                "\(worktree.requestedBase)^{commit}"],
                               cwd: worktree.repository), answer.status == 0 else { return nil }
        var resolved = worktree
        resolved.base = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
        return resolved
    }

    private static func pruneWorktrees(in repository: String) {
        _ = git(["worktree", "prune"], cwd: repository)
    }

    /// Materialise only when the task is actually leaving the queue. A prepared task holds no
    /// checkout and no branch while a serialization token is busy; spawn resolves its base again
    /// and the resulting SHA becomes the immutable receipt.
    static func addWorktree(_ worktree: Orchestrator.Worktree, taskID: String) -> String? {
        let parent = URL(fileURLWithPath: worktree.path, isDirectory: true)
            .deletingLastPathComponent()
        do {
            try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true)
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                   ofItemAtPath: worktreeRoot.path)
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                   ofItemAtPath: parent.path)
        } catch {
            return "Could not create the private worktree directory: \(error.localizedDescription)"
        }
        guard let answer = git(["worktree", "add", "-b", worktree.branch,
                                worktree.path, worktree.base], cwd: worktree.repository,
                               timeout: 60) else {
            pruneWorktrees(in: worktree.repository)
            return "git worktree add could not be started"
        }
        guard answer.status == 0 else {
            pruneWorktrees(in: worktree.repository)
            let detail = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
            return String((detail.isEmpty ? "git worktree add failed" : detail).prefix(500))
        }
        let currentStatus = git(["status", "--porcelain", "--untracked-files=all"],
                                cwd: worktree.repository)
        let dirty = currentStatus?.status == 0
            ? currentStatus!.output.split(whereSeparator: \.isNewline).count
            : worktree.baseDirty
        RemoteAuth.audit("orchestrator.worktree.add", [
            "task": taskID, "base": worktree.base, "branch": worktree.branch,
            "path": worktree.path, "dirty": String(dirty),
        ])
        guard FileManager.default.fileExists(atPath: worktree.cwd) else {
            disposeWorktree(worktree, taskID: taskID, why: "spawn_failed")
            return "The requested project_dir does not exist in base commit \(worktree.base)."
        }
        return nil
    }

    private struct WorktreeFacts {
        var head: String?
        var commits: Int?
        var dirty: Bool?
        var headOnBranch: Bool?
        var branchExists: Bool
    }

    private static func inspectWorktree(_ worktree: Orchestrator.Worktree) -> WorktreeFacts {
        let branchRef = "refs/heads/\(worktree.branch)"
        let branchAnswer = git(["rev-parse", "--verify", "\(branchRef)^{commit}"],
                               cwd: worktree.repository)
        let branchExists = branchAnswer?.status == 0
        let head = branchExists
            ? branchAnswer?.output.trimmingCharacters(in: .whitespacesAndNewlines) : nil
        let countAnswer = branchExists
            ? git(["rev-list", "--count", "\(worktree.base)..\(branchRef)"],
                  cwd: worktree.repository) : nil
        let commits = countAnswer?.status == 0
            ? Int(countAnswer!.output.trimmingCharacters(in: .whitespacesAndNewlines)) : nil
        guard FileManager.default.fileExists(atPath: worktree.path) else {
            return WorktreeFacts(head: head, commits: commits, dirty: nil,
                                 headOnBranch: nil, branchExists: branchExists)
        }
        let status = git(["status", "--porcelain", "--untracked-files=all"], cwd: worktree.path)
        let dirty = status?.status == 0 ? !status!.output.isEmpty : nil
        let symbolic = git(["symbolic-ref", "--quiet", "--short", "HEAD"], cwd: worktree.path)
        let headOnBranch = symbolic?.status == 0
            ? symbolic!.output.trimmingCharacters(in: .whitespacesAndNewlines) == worktree.branch
            : false
        return WorktreeFacts(head: head, commits: commits, dirty: dirty,
                             headOnBranch: headOnBranch, branchExists: branchExists)
    }

    static func refreshedWorktree(_ original: Orchestrator.Worktree) -> Orchestrator.Worktree {
        var worktree = original
        let facts = inspectWorktree(worktree)
        worktree.head = facts.head
        worktree.commits = facts.commits
        worktree.dirty = facts.dirty
        return worktree
    }

    /// Remove through git or not at all. Even failed removals are followed by prune, while branch
    /// deletion happens only after git removed a provably empty checkout successfully.
    static func disposeWorktree(_ worktree: Orchestrator.Worktree, taskID: String, why: String,
                                allowCommitted: Bool = true) {
        let facts = inspectWorktree(worktree)
        let decision = Orchestrator.worktreeDisposal(commits: facts.commits, dirty: facts.dirty,
                                        headOnBranch: facts.headOnBranch,
                                        branchExists: facts.branchExists)
        guard decision != .keepEverything else {
            let keptWhy: String
            if facts.dirty == true { keptWhy = "dirty" }
            else if facts.commits == 0 && facts.headOnBranch == false { keptWhy = "head_moved" }
            else { keptWhy = "unreadable" }
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "why": keptWhy,
            ])
            return
        }
        if decision == .removeTreeKeepBranch && !allowCommitted { return }
        guard FileManager.default.fileExists(atPath: worktree.path) else {
            // The checkout is already gone. The branch remains the delivery; never infer that a
            // missing directory authorizes deleting it.
            pruneWorktrees(in: worktree.repository)
            return
        }
        let removed = git(["worktree", "remove", worktree.path], cwd: worktree.repository,
                          timeout: 60)
        pruneWorktrees(in: worktree.repository)
        guard removed?.status == 0 else {
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "why": "remove_failed",
            ])
            return
        }
        if decision == .removeAll {
            let deleted = git(["branch", "-D", worktree.branch], cwd: worktree.repository)
            guard deleted?.status == 0 else {
                RemoteAuth.audit("orchestrator.worktree.kept", [
                    "task": taskID, "branch": worktree.branch, "why": "remove_failed",
                ])
                return
            }
        }
        RemoteAuth.audit("orchestrator.worktree.remove", [
            "task": taskID, "branch": worktree.branch, "why": why,
            "commits": String(facts.commits ?? 0),
        ])
    }

    /// Enqueue only; callers on the main thread never execute a git subprocess themselves.
    static func scheduleWorktreeDisposal(_ worktree: Orchestrator.Worktree, taskID: String,
                                         why: String,
                                         allowCommitted: Bool = true) {
        Orchestrator.worktreeQueue.async {
            disposeWorktree(worktree, taskID: taskID, why: why,
                            allowCommitted: allowCommitted)
        }
    }

    /// Current holders followed by older waiters entitled to a shared token first. Roots are
    /// deliberately absent from this comparison: the namespace covers the whole machine. Every
    /// token in a request is considered together, so a queued multi-token task holds none of them.
    static func serializeBlockers(for candidate: Orchestrator.Task,
                                  among existing: [Orchestrator.Task]) -> [Orchestrator.Task] {
        guard candidate.state == .queued, !candidate.serialize.isEmpty else { return [] }
        let wanted = Set(candidate.serialize)
        func earlier(_ task: Orchestrator.Task) -> Bool {
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

    /// Freeze claims into one namespace while the validated project directory exists. Only the
    /// root touches the filesystem; relative claims are then normalised and joined literally so
    /// creating a target (or a symlink below the root) can never change a lease's identity.
    static func freezeClaims(_ claims: [String], projectDir: String) -> [String] {
        guard !claims.isEmpty else { return [] }
        let root = canonicalFilesystemPath(projectDir)
        let separator = root == "/" ? "" : "/"
        return claims.map { claim in
            let relative = claim.split(separator: "/", omittingEmptySubsequences: true)
                .filter { $0 != "." }.joined(separator: "/")
            return relative.isEmpty ? root : root + separator + relative
        }
    }

    /// Relative claims name the shared checkout. An isolated child cannot touch those paths at
    /// that spelling, so retaining the lease would only block useful work in the base tree.
    static func prepareClaimsForIsolation(_ task: inout Orchestrator.Task) -> [[String: Any]] {
        guard task.isolation == .worktree, !task.claims.isEmpty else { return [] }
        let ignored = task.claims
        task.claims = []
        task.claimKeys = []
        return [[
            "code": "claims_ignored_for_worktree",
            "paths": ignored,
            "message": "Claims inside project_dir were ignored because this task uses an isolated worktree.",
        ]]
    }

    /// The shared descendant of two already-frozen claim keys. This is deliberately only string
    /// work: dispatch holds the global orchestrator lock while it compares every live lease.
    static func sharedClaimPath(_ first: String, _ second: String) -> String? {
        if first == second { return first }
        let firstPrefix = first == "/" ? "/" : first + "/"
        if second.hasPrefix(firstPrefix) { return second }
        let secondPrefix = second == "/" ? "/" : second + "/"
        if first.hasPrefix(secondPrefix) { return first }
        return nil
    }

    /// Two explicit write declarations make L1 redundant when their frozen scopes do not meet.
    /// The empty declaration is the useful edge: it positively says the task is read-only.
    private static func declaredClaimsAreDisjoint(_ first: Orchestrator.Task,
                                                  _ second: Orchestrator.Task) -> Bool {
        guard first.claimsDeclared, second.claimsDeclared else { return false }
        return !first.activeClaimKeys.contains { claimed in
            second.activeClaimKeys.contains { sharedClaimPath(claimed, $0) != nil }
        }
    }

    /// Pure dispatch-time claims scan. Unlike L1, queued tasks participate: a claim is a
    /// reservation made at dispatch, not evidence that a tab has started touching files.
    /// Compares `activeClaimKeys` rather than `claimKeys` so a path either side already gave
    /// back through `claims/release` no longer conflicts.
    static func claimsOverlaps(for newTask: Orchestrator.Task,
                               among existing: [Orchestrator.Task]) -> [ClaimsOverlap] {
        guard !newTask.activeClaimKeys.isEmpty, !newTask.state.isTerminal else { return [] }
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = resolvedRootKey(of: newTask, among: indexed)
        return existing.compactMap { task -> ClaimsOverlap? in
            guard task.id != newTask.id, !task.state.isTerminal, !task.activeClaimKeys.isEmpty
            else {
                return nil
            }
            var paths: [String] = []
            var seen: Set<String> = []
            for claimed in newTask.activeClaimKeys {
                for other in task.activeClaimKeys {
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
                                 rootLabel: root.rootLabel ?? task.rootLabel,
                                 rootKey: otherRoot)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    /// The directory both tasks may write, or nil when their paths are merely string prefixes.
    ///
    /// Paths are resolved the way the rest of the project resolves working directories:
    /// standardise first, then follow symlinks, and compare the resulting spelling exactly.
    /// Comparing components is what keeps `/a/b` separate from `/a/bc`, and also handles `/`
    /// without a special string-prefix case.
    static func sharedWorkspaceDirectory(_ first: String, _ second: String) -> String? {
        let first = canonicalFilesystemPath(first)
        let second = canonicalFilesystemPath(second)
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
    static func rootTask(of task: Orchestrator.Task,
                         among existing: [String: Orchestrator.Task]) -> Orchestrator.Task {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId, let above = existing[parentID],
              hops < Orchestrator.depthFloor {
            at = above
            hops += 1
        }
        return at
    }

    static func rootKey(of task: Orchestrator.Task,
                        among existing: [String: Orchestrator.Task]) -> String {
        let at = rootTask(of: task, among: existing)
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// Claims are a hard gate only when both trees can actually be identified. A task with an
    /// unresolved parent and no independently supplied root session is unknown, not a new root.
    private static func resolvedRootKey(of task: Orchestrator.Task,
                                        among existing: [String: Orchestrator.Task]) -> String? {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId {
            guard hops < Orchestrator.depthFloor, let above = existing[parentID] else {
                return at.rootSessionId
            }
            at = above
            hops += 1
        }
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// The stable short identifier for a root tree, independent of its self-reported label:
    /// SHA-256 of the canonical root key — a live root's session id, or `task:<id>` for a task
    /// resolved back to itself — truncated to its first 8 hex characters. Two roots that both
    /// call themselves the same thing still hash differently, because the input is the session
    /// identity underneath the label rather than the label itself; the same tree always hashes
    /// the same way.
    static func rootKeyDigest(_ canonicalRootKey: String) -> String {
        String(RemoteAuth.hex(SHA256.hash(data: Data(canonicalRootKey.utf8))).prefix(8))
    }

    /// Pure half of the dispatch-time scan, kept visible to the unit suite so path boundaries,
    /// root identity and terminal-state filtering do not need a live terminal to exercise them.
    static func workspaceOverlaps(for newTask: Orchestrator.Task,
                                  among existing: [Orchestrator.Task]) -> [WorkspaceOverlap] {
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = rootKey(of: newTask, among: indexed)
        let rooted = existing.map { (task: $0, rootKey: rootKey(of: $0, among: indexed)) }
        return workspaceOverlaps(for: newTask, rootKey: newRoot, among: rooted)
    }

    static func workspaceOverlaps(for newTask: Orchestrator.Task, rootKey newRoot: String,
                                  among existing: [(task: Orchestrator.Task, rootKey: String)])
        -> [WorkspaceOverlap] {
        guard newTask.state == .spawning || newTask.state == .briefed else { return [] }
        return existing.compactMap { item -> WorkspaceOverlap? in
            let task = item.task
            guard task.id != newTask.id,
                  task.state == .spawning || task.state == .briefed,
                  item.rootKey != newRoot,
                  !declaredClaimsAreDisjoint(newTask, task),
                  let shared = sharedWorkspaceDirectory(Orchestrator.cwd(of: newTask),
                                                        Orchestrator.cwd(of: task))
            else { return nil }
            return WorkspaceOverlap(task: task, sharedDir: shared)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    /// Wire payload shared by first dispatches and idempotent retries. Keeping the optional field
    /// here makes "absent, not an empty array" explicit and independently testable.
    static func dispatchPayload(record: [String: Any], taskID: String,
                                overlaps: [WorkspaceOverlap],
                                claimsOverlaps: [ClaimsOverlap] = [],
                                additionalWarnings: [[String: Any]] = [],
                                now: Date = Date()) -> [String: Any] {
        var reply: [String: Any] = ["ok": true, "task": record]
        let warnings = overlaps.map { $0.warning(for: taskID) }
            + claimsOverlaps.filter { !$0.blocks }.map { $0.warning(for: taskID, now: now) }
            + additionalWarnings
        if !warnings.isEmpty {
            reply["warnings"] = warnings
        }
        return reply
    }

    /// The one age formula used by workspace conflicts and landing records alike. A wall clock
    /// moving backwards never turns an API duration negative.
    static func ageSeconds(since: Date, now: Date) -> Int {
        max(0, Int(now.timeIntervalSince(since)))
    }

    /// The actionable context returned when another root already reserved a write path.
    /// `age_seconds` and `root_key` make the error self-sufficient without a follow-up GET:
    /// `root_label` is self-reported prose that can be stale or shared by two unrelated roots
    /// (two different trees both calling themselves "clawdline schedules" is a real case), while
    /// `root_key` is the same tree's identity every time, hashed rather than handed over raw.
    static func workspaceBusyExtra(_ overlap: ClaimsOverlap, now: Date = Date()) -> [String: Any] {
        let extra: [String: Any] = [
            "blocking_task": overlap.task.id,
            "title": overlap.task.title,
            "root_label": overlap.rootLabel as Any? ?? NSNull(),
            "created": Int(overlap.task.created.timeIntervalSince1970),
            "conflict_paths": overlap.paths,
            "retry_after": 60,
            "age_seconds": ageSeconds(since: overlap.task.created, now: now),
            "root_key": overlap.rootKey.map(rootKeyDigest) as Any? ?? NSNull(),
        ]
        return extra
    }

}
