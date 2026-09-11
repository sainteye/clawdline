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
        var workItemID: String?
        var workPhase: String?
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

    /// The write set is the one thing every dispatcher knows and 60.7% of them never wrote down.
    ///
    /// **Mandatory means the key is present, and deliberately not that it is non-empty.** The
    /// warning this replaces went out on every undeclared dispatch and changed nothing, because a
    /// warning is free to ignore; a refusal is not. But the field being refused for has to be one
    /// a caller can always fill honestly, or the refusal fires on dispatches nobody could have
    /// written correctly and gets routed around instead of answered. Three answers are always
    /// available: the files, the directories when the files are not decided yet, and `[]` when the
    /// task genuinely writes nothing. Requiring a *non-empty* list would have made `[]` — a
    /// positive read-only declaration, and the shipped meaning of the empty array — unsayable, so
    /// every reviewer would have had to claim a path it does not write.
    ///
    /// **It applies to isolated dispatches too, and that is the half of the fleet this had to get
    /// right.** Over half of one day's dispatches were worktree-isolated, and for those the
    /// *edit-time* meaning of `claims` really is empty: the child edits its own checkout, so
    /// ``prepareClaimsForIsolation`` drops the lease and is right to. Declaring is still
    /// answerable, because since ``OrchestratorLandingQueue/retainLandingPaths(_:)`` the same
    /// list is kept as the landing-time write set. A dispatcher writes one list; the broker
    /// decides which of the two questions it is still answering.
    ///
    /// `writtenForThisDispatch` is false exactly where no caller is holding the answer: a stored
    /// schedule template, whose editor on both surfaces has no `claims` control at all, and a
    /// respawn, which re-enters with a body that was admitted once already. Refusing either would
    /// break work over a field the surface that produced it cannot express — which is this
    /// refusal's own failure mode, not an exception to it. Those two keep the `claims_missing`
    /// warning, and so do records the registry recovered from before this landed.
    static func claimsRequirementRefusal(declared: Bool,
                                         writtenForThisDispatch: Bool) -> Orchestrator.Reply? {
        guard !declared, writtenForThisDispatch else { return nil }
        return .refused(
            status: 422, code: "claims_required",
            message: "claims is required: the relative paths under project_dir this task may "
                + "write. Name the files when you know them, the directories when you do not, "
                + "or send \"claims\": [] to declare that this task writes nothing. An isolated "
                + "task declares the same list — its lease is dropped for the private checkout "
                + "and the paths are kept as its landing write set. Leave out what a repository "
                + "guard rewrites for you, such as a ratcheted line count: that belongs to "
                + "whoever lands the change, not to this task's lease.",
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
        // A standing host needs two launch-time facts: Clawdline opened its tab for a task (as a
        // child role or a retained Root), and that process was given the whole task root. A leaf
        // gets only its original task directory, so it cannot read a new follow-up's sibling
        // CHILD.md even though Clawdline opened it. Persist the actual grant instead of inferring
        // it from depth: the configured floor can change while a tab remains standing, but a
        // process's `--add-dir` cannot.
        let role = roles[sessionID]
        let rootHosts = tasks.filter {
            $0.sessionRoot && $0.childTerminalId == sessionID && $0.childTTY == session.tty
                && $0.assistant == assistant && $0.childTaskRootAccess
        }
        guard role?.taskRootAccess == true || rootHosts.count == 1 else {
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
        return .accepted(session, depth: role?.depth ?? 1)
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
        if let raw = obj["work_item_id"] {
            guard let id = raw as? String, !id.isEmpty, id.count <= 200,
                  !id.contains(where: { $0.isWhitespace }) else {
                return .bad("work_item_id must be a non-empty opaque item id")
            }
            made.workItemID = id
        }
        if let raw = obj["work_phase"] {
            guard let phase = raw as? String,
                  ["planning", "output", "review_testing", "correction", "integration"].contains(phase)
            else { return .bad("work_phase must name a supported board activity") }
            made.workPhase = phase
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
        /// What git wrote to stderr, and **only** where the caller asked for the two streams to
        /// be kept apart. It is empty otherwise — including when git wrote a diagnostic — because
        /// the merged stream puts that text in `output`. Never a substitute for `status`.
        var errorOutput: String = ""
        /// Whether stdout was valid UTF-8. `output` deliberately keeps its historical empty-string
        /// fallback so callers that only log or match git's text do not change behavior, while a
        /// caller that parses stdout as data can fail closed instead of mistaking undecodable bytes
        /// for successful empty output.
        var outputIsUTF8: Bool = true

        init(output: String, status: Int32, errorOutput: String = "",
             outputIsUTF8: Bool = true) {
            self.output = output
            self.status = status
            self.errorOutput = errorOutput
            self.outputIsUTF8 = outputIsUTF8
        }

        /// The production Data-to-answer boundary. Tests that need to exercise decoding use this
        /// initializer too, so an invalid byte reaches the same conversion as subprocess stdout.
        init(outputData: Data, status: Int32, errorData: Data = Data()) {
            let decodedOutput = String(data: outputData, encoding: .utf8)
            self.output = decodedOutput ?? ""
            self.status = status
            self.errorOutput = String(data: errorData, encoding: .utf8) ?? ""
            self.outputIsUTF8 = decodedOutput != nil
        }
    }

    /// The only git execution seam for worktree lifecycle operations. Arguments never pass
    /// through a shell, optional locks are disabled, and a wedged repository cannot hold the
    /// broker queue indefinitely.
    ///
    /// **`separateStandardError` exists because one caller parses this output as data.** git
    /// writes diagnostics to stderr that are not failures — `warning: could not open directory
    /// 'a/nope/'` comes back beside a `0` status — and with both streams in one pipe a
    /// `--porcelain` reader cannot tell that line from a record. It was the reviewer's finding on
    /// 2026-09-06: the landing sweep's `dropFirst(3)` turned that warning into a claimed path
    /// called `ning: could not open directory …`. It is opt-in rather than the default so that no
    /// existing caller's `output` changes shape — several of them log or match on what git said,
    /// and a seam that quietly stopped carrying stderr would make a wedged repository silent.
    static func git(_ arguments: [String], cwd: String,
                    gitDirectory: String? = nil,
                    timeout: TimeInterval = 15,
                    separateStandardError: Bool = false,
                    environment extra: [String: String] = [:]) -> GitAnswer? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = gitDirectory.map { ["--git-dir", $0] + arguments } ?? arguments
        process.currentDirectoryURL = URL(fileURLWithPath: cwd, isDirectory: true)
        var environment = ProcessInfo.processInfo.environment
        // Callers add variables (a private `GIT_INDEX_FILE`, literal pathspecs, the C locale);
        // none of them may switch optional locks back on, so that one is applied last.
        extra.forEach { environment[$0.key] = $0.value }
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        process.environment = environment
        let pipe = Pipe()
        // Two pipes have to be drained concurrently or a chatty stderr fills its buffer while
        // this thread is blocked reading stdout, and the subprocess never exits. The reader below
        // is on a queue of its own for exactly that; `readDataToEndOfFile` on the merged stream
        // has no such hazard, which is why it stays the shape of the single-pipe path.
        let errorPipe = separateStandardError ? Pipe() : pipe
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = pipe
        process.standardError = errorPipe
        do { try process.run() } catch { return nil }
        let killer = DispatchWorkItem { if process.isRunning { process.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout,
                                                       execute: killer)
        var errorData = Data()
        let drained = DispatchGroup()
        if separateStandardError {
            drained.enter()
            DispatchQueue.global(qos: .utility).async {
                errorData = errorPipe.fileHandleForReading.readDataToEndOfFile()
                drained.leave()
            }
        }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        drained.wait()
        process.waitQuietly()
        killer.cancel()
        return GitAnswer(outputData: data, status: process.terminationStatus,
                         errorData: errorData)
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

    /// The main worktree of the repository a checkout belongs to — the directory a repository is
    /// named by everywhere else in this app — or nothing when there is no ordinary one.
    ///
    /// `git rev-parse --show-toplevel` answers with whichever checkout the caller stands in, and
    /// for a linked worktree that is a directory this app made and will delete. Every task in the
    /// registry is filed under the repository it was cut from, so a reading taken in the checkout
    /// matches none of them — which is not an error anything can see, because an empty list is
    /// also what "nothing is going on here" looks like.
    static func mainWorktree(containing cwd: String) -> String? {
        guard let common = gitCommonDirectory(at: cwd) else { return nil }
        let url = URL(fileURLWithPath: common)
        // `<repository>/.git` is the ordinary shape and the only one with an answer here. A bare
        // repository or a `--separate-git-dir` layout has no working tree this name belongs to,
        // and deriving one from the path anyway would be the same guess in the other direction.
        guard url.lastPathComponent == ".git" else { return nil }
        let main = url.deletingLastPathComponent().standardizedFileURL.path
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: main, isDirectory: &isDirectory),
              isDirectory.boolValue else { return nil }
        return main
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

    static func verifyTargetLanding(gitDirectory: String, target: String,
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
        /// `dirty` with ``Orchestrator/worktreeToolNoiseDirectories`` taken out. Only disposal
        /// reads it; `dirty` keeps meaning exactly what `git status` says.
        var disposalDirty: Bool? = nil
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
        // Asked only of a dirty checkout, on a stream of its own: a status that does not parse is
        // not a clean one, so `nil` keeps the checkout.
        let disposalDirty = dirty == true
            ? worktreeStatus(at: worktree.path).map { entries in
                entries.contains { !Orchestrator.isWorktreeToolNoise($0) } }
            : dirty
        let symbolic = git(["symbolic-ref", "--quiet", "--short", "HEAD"], cwd: worktree.path)
        let headOnBranch = symbolic?.status == 0
            ? symbolic!.output.trimmingCharacters(in: .whitespacesAndNewlines) == worktree.branch
            : false
        return WorktreeFacts(head: head, commits: commits, dirty: dirty,
                             disposalDirty: disposalDirty,
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
        let decision = Orchestrator.worktreeDisposal(commits: facts.commits,
                                        dirty: facts.disposalDirty,
                                        headOnBranch: facts.headOnBranch,
                                        branchExists: facts.branchExists)
        guard decision != .keepEverything else {
            let keptWhy: String
            if facts.disposalDirty == true { keptWhy = "dirty" }
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
        // Tool noise is still untracked to git, which refuses to remove a checkout holding it. Read
        // the status again at the last moment and force only while that noise is all there is.
        var removal = ["worktree", "remove", worktree.path]
        if facts.dirty == true {
            guard worktreeStatus(at: worktree.path).map({ entries in
                !entries.contains { !Orchestrator.isWorktreeToolNoise($0) } }) == true else {
                RemoteAuth.audit("orchestrator.worktree.kept", [
                    "task": taskID, "branch": worktree.branch, "why": "dirty",
                ])
                return
            }
            removal.insert("--force", at: 2)
        }
        // Proved from the worktree root down at the removal itself: `git worktree remove` resolves
        // the path it is handed, so a slug directory swapped for a symlink would take it elsewhere.
        let roots = Orchestrator.reclaimRoots
        guard OwnedStorage.containedDirectory(worktree.path, under: roots.worktrees.path) == .proven,
              ownedCheckoutDirectory(worktree.path, taskID: taskID, root: roots.worktrees) else {
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "why": "path_not_owned",
            ])
            return
        }
        let removed = git(removal, cwd: worktree.repository, timeout: 60)
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

    // MARK: - Reclaiming what a finished line no longer needs

    /// This checkout's `git status --porcelain=v1 -z --untracked-files=all`, parsed, on a stream of
    /// its own; `nil` when git could not answer or answered in a shape that does not parse.
    static func worktreeStatus(at path: String) -> [Orchestrator.WorktreeStatusEntry]? {
        guard let answer = git(["status", "--porcelain=v1", "-z", "--untracked-files=all"],
                               cwd: path, timeout: 60, separateStandardError: true),
              answer.status == 0, answer.outputIsUTF8 else { return nil }
        return Orchestrator.worktreeStatusEntries(answer.output)
    }

    /// Whether `path` is a checkout this task owns under `root`: exactly `<root>/<slug>/<task-id>`,
    /// proved by ``OwnedStorage/containment(_:under:)`` — the root as spelled first, then the slug
    /// and the checkout real directories, then the resolved path inside the resolved root. The
    /// spelling is never tidied first: `x/../` or `/./` is refused rather than standardized away,
    /// because the kernel resolves it through whatever `x` is. Registry text is held against the
    /// root it must live under before any git runs inside it.
    static func ownedCheckoutDirectory(_ path: String, taskID: String, root: URL) -> Bool {
        let proof = OwnedStorage.containment(path, under: root.path)
        return isTaskID(taskID) && proof.verdict == .proven && proof.below.count == 2
            && proof.below.last == taskID
    }

    enum LandedCheckoutOutcome: Equatable {
        case removed(preserved: String)
        case kept(String)
    }

    /// Remove a finished task's own checkout because its delivery landed — after its uncommitted
    /// delta is preserved and proved, never before, and never its branch.
    ///
    /// The delta is `git diff --binary --full-index --no-ext-diff HEAD` plus a tar of every
    /// untracked, non-ignored file outside ``Orchestrator/worktreeToolNoiseDirectories``, written to
    /// `<roots.preserved>/<task-id>/<attempt>/` with a manifest naming base, head, branch and each
    /// file's SHA-256. Ignored files are not part of it and go with the checkout. The patch is
    /// proved in two private index files, never the checkout's own: `read-tree <head>`,
    /// `apply --cached --check`, apply, write the tree, and require it to equal the tree the same
    /// tracked paths write from the working files. The archive's listing must name exactly the
    /// untracked set, and the status must not have moved while all of that ran. Only then
    /// `git worktree remove --force` and `git worktree prune`. Every refusal keeps the checkout and
    /// audits a typed reason; a removal git refuses throws the attempt away, because while the
    /// checkout exists it is the copy.
    @discardableResult
    static func disposeLandedWorktree(_ worktree: Orchestrator.Worktree, taskID: String,
                                      roots: Orchestrator.ReclaimRoots = Orchestrator.reclaimRoots,
                                      now: Date = Date()) -> LandedCheckoutOutcome {
        let path = worktree.path
        let manager = FileManager.default
        func kept(_ why: String) -> LandedCheckoutOutcome {
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "path": path, "why": why,
            ])
            return .kept(why)
        }
        func run(_ arguments: [String], _ environment: [String: String] = [:]) -> GitAnswer? {
            let answer = git(arguments, cwd: path, timeout: 120, separateStandardError: true,
                             environment: environment)
            return answer?.status == 0 ? answer : nil
        }
        func line(_ arguments: [String], _ environment: [String: String] = [:]) -> String? {
            guard let text = run(arguments, environment)?.output
                .trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else { return nil }
            return text
        }
        /// A removal inside one attempt, made only once that attempt is proved again from the
        /// preservation root down; otherwise it is left where it is.
        func discard(_ url: URL, inside attempt: URL) {
            guard OwnedStorage.containedDirectory(attempt.path, under: roots.preserved.path) == .proven
            else { return }
            try? manager.removeItem(at: url)
        }
        guard worktree.branch == worktreeBranch(for: taskID),
              ownedCheckoutDirectory(path, taskID: taskID, root: roots.worktrees) else {
            return kept("path_not_owned")
        }
        let branchRef = "refs/heads/\(worktree.branch)"
        guard let head = line(["rev-parse", "--verify", "--quiet", "\(branchRef)^{commit}"]) else {
            return kept("branch_missing")
        }
        guard line(["symbolic-ref", "--quiet", "HEAD"]) == branchRef,
              line(["rev-parse", "--verify", "--quiet", "HEAD^{commit}"]) == head else {
            return kept("head_moved")
        }
        guard let gitDirectory = line(["rev-parse", "--absolute-git-dir"]) else {
            return kept("git_unreadable")
        }
        let operations = ["MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG",
                          "rebase-merge", "rebase-apply"]
        guard !operations.contains(where: { manager.fileExists(atPath: gitDirectory + "/" + $0) })
        else { return kept("operation_in_progress") }
        guard let status = worktreeStatus(at: path) else { return kept("status_unreadable") }
        let delta = status.filter { !Orchestrator.isWorktreeToolNoise($0) }
        let tracked = delta.filter { !$0.untracked }
        guard !tracked.contains(where: { $0.code.contains("U") || $0.code == "AA" || $0.code == "DD" })
        else { return kept("conflicted") }
        // Index bytes that differ from both HEAD and the working file are something a patch of the
        // working tree against HEAD cannot carry.
        guard !tracked.contains(where: { !$0.code.hasPrefix(" ") && !$0.code.hasSuffix(" ") }) else {
            return kept("staged_differs_from_worktree")
        }
        guard let others = run(["ls-files", "--others", "--exclude-standard", "-z"]),
              others.outputIsUTF8 else { return kept("untracked_unreadable") }
        let untracked = others.output.components(separatedBy: "\0").filter {
            !$0.isEmpty && !Orchestrator.isWorktreeToolNoise(.init(code: "??", path: $0))
        }.sorted()
        guard untracked == delta.filter({ $0.untracked }).map({ $0.path }).sorted() else {
            return kept("status_changed")
        }

        let attempt = roots.preserved.appendingPathComponent(taskID, isDirectory: true)
            .appendingPathComponent("\(Int(now.timeIntervalSince1970))-"
                + UUID().uuidString.prefix(8).lowercased(), isDirectory: true)
        do {
            try manager.createDirectory(at: attempt, withIntermediateDirectories: true,
                                        attributes: [.posixPermissions: 0o700])
        } catch {
            return kept("preservation_unwritable")
        }
        var removedCheckout = false
        defer { if !removedCheckout { discard(attempt, inside: attempt) } }
        let patch = attempt.appendingPathComponent("delta.patch")
        guard run(["diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv",
                   "--no-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/",
                   "--output=\(patch.path)", "HEAD"]) != nil,
              let patchData = try? Data(contentsOf: patch) else { return kept("patch_failed") }

        let verify = ["GIT_INDEX_FILE": attempt.appendingPathComponent("verify.index").path]
        let expect = ["GIT_INDEX_FILE": attempt.appendingPathComponent("expect.index").path,
                      "GIT_LITERAL_PATHSPECS": "1"]
        let pathspecs = attempt.appendingPathComponent("tracked.pathspecs")
        defer {
            for file in [verify["GIT_INDEX_FILE"] ?? "", expect["GIT_INDEX_FILE"] ?? "", pathspecs.path] {
                discard(URL(fileURLWithPath: file), inside: attempt)
            }
        }
        guard run(["read-tree", head], verify) != nil else { return kept("patch_unverified") }
        if !patchData.isEmpty {
            guard run(["apply", "--cached", "--check", "--binary", patch.path], verify) != nil,
                  run(["apply", "--cached", "--binary", patch.path], verify) != nil else {
                return kept("patch_unverified")
            }
        }
        guard let applied = line(["write-tree"], verify),
              run(["read-tree", head], expect) != nil else { return kept("patch_unverified") }
        let trackedPaths = tracked.flatMap { [$0.path] + ($0.original.map { [$0] } ?? []) }
        if !trackedPaths.isEmpty {
            let list = Data(trackedPaths.map { $0 + "\0" }.joined().utf8)
            guard (try? list.write(to: pathspecs)) != nil,
                  run(["add", "-A", "--pathspec-from-file=\(pathspecs.path)", "--pathspec-file-nul"],
                      expect) != nil else { return kept("patch_unverified") }
        }
        guard let expected = line(["write-tree"], expect), applied == expected else {
            return kept("patch_unverified")
        }

        var untrackedRecord: [String: Any] = ["count": untracked.count, "paths": untracked,
                                              "file": NSNull(), "sha256": NSNull(), "bytes": 0]
        if !untracked.isEmpty {
            let list = attempt.appendingPathComponent("untracked.list")
            let tarball = attempt.appendingPathComponent("untracked.tar")
            defer { discard(list, inside: attempt) }
            // A UTF-8 locale, because `tar -t` in the C locale escapes every non-ASCII byte and a
            // file named in Chinese would never match its own listing.
            let locale = ["LC_ALL": "en_US.UTF-8", "PATH": "/usr/bin:/bin"]
            guard (try? Data(untracked.map { $0 + "\0" }.joined().utf8).write(to: list)) != nil,
                  Process.collect("/usr/bin/tar", ["-c", "-f", tarball.path, "-C", path, "--null",
                                                   "-T", list.path],
                                  environment: locale, timeout: 600)?.status == 0,
                  let listing = Process.collect("/usr/bin/tar", ["-t", "-f", tarball.path],
                                                environment: locale, timeout: 600),
                  listing.status == 0,
                  let names = String(data: listing.output, encoding: .utf8),
                  Set(names.split(separator: "\n").map(String.init)) == Set(untracked),
                  let digest = sha256Hex(fileAt: tarball) else {
                return kept("archive_incomplete")
            }
            untrackedRecord["file"] = "untracked.tar"
            untrackedRecord["sha256"] = digest.hex
            untrackedRecord["bytes"] = digest.bytes
        }
        let patchDigest = SHA256.hash(data: patchData).map { String(format: "%02x", $0) }.joined()
        let manifest: [String: Any] = [
            "clawdline_reclaimed_checkout": 1, "task": taskID, "branch": worktree.branch,
            "base": worktree.base, "head": head, "repository": worktree.repository,
            "checkout": path, "created_at": Int(now.timeIntervalSince1970),
            "patch": ["file": "delta.patch", "bytes": patchData.count, "sha256": patchDigest,
                      "tree": expected] as [String: Any],
            "untracked": untrackedRecord,
            "tool_noise": Orchestrator.worktreeToolNoiseDirectories.sorted(),
        ]
        guard let manifestData = try? JSONSerialization.data(
                withJSONObject: manifest, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]),
              (try? manifestData.write(to: attempt.appendingPathComponent("manifest.json"),
                                       options: .atomic)) != nil else {
            return kept("manifest_unwritable")
        }
        // Nothing moved while all of that ran; otherwise what was proved is not what would go.
        guard worktreeStatus(at: path)?.filter({ !Orchestrator.isWorktreeToolNoise($0) }) == delta else {
            return kept("status_changed")
        }
        // And the checkout is still where it was proved to be, through real directories from the
        // worktree root down: `git worktree remove` resolves the path it is handed, and every step
        // above took time.
        guard OwnedStorage.containedDirectory(path, under: roots.worktrees.path) == .proven,
              ownedCheckoutDirectory(path, taskID: taskID, root: roots.worktrees) else {
            return kept("path_not_owned")
        }
        guard git(["worktree", "remove", "--force", path], cwd: worktree.repository,
                  timeout: 120)?.status == 0 else {
            pruneWorktrees(in: worktree.repository)
            return kept("remove_failed")
        }
        pruneWorktrees(in: worktree.repository)
        removedCheckout = true
        RemoteAuth.audit("orchestrator.worktree.remove", [
            "task": taskID, "branch": worktree.branch, "why": "landed", "path": path,
            "preserved": attempt.path, "patch_sha256": patchDigest,
            "untracked": String(untracked.count),
        ])
        return .removed(preserved: attempt.path)
    }

    /// A file's SHA-256 read a megabyte at a time, so a large archive is never held in memory.
    private static func sha256Hex(fileAt url: URL) -> (hex: String, bytes: Int)? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        var hasher = SHA256()
        var bytes = 0
        while true {
            let chunk: Data?
            do { chunk = try handle.read(upToCount: 1 << 20) } catch { return nil }
            guard let chunk, !chunk.isEmpty else { break }
            hasher.update(data: chunk)
            bytes += chunk.count
        }
        return (hasher.finalize().map { String(format: "%02x", $0) }.joined(), bytes)
    }

    enum DependencyDirectoryVerdict: Equatable {
        case reclaim
        case refused(String)
    }

    /// Whether one candidate may go: named `node_modules` or `.venv`, reached through real
    /// directories only from the checkout as spelled — itself a real directory, never a symlink to
    /// one — still inside it once resolved, matched by an ignore rule
    /// (`check-ignore --no-index`, so the pattern answers on its own), and holding no tracked path
    /// (`ls-files`, which answers the other half). Every fact is re-proved here even for a path git
    /// itself listed.
    static func dependencyDirectoryVerdict(_ relative: String, checkout: String)
        -> DependencyDirectoryVerdict {
        let components = relative.components(separatedBy: "/")
        guard let last = components.last, Orchestrator.dependencyDirectoryNames.contains(last),
              !components.contains(where: { $0.isEmpty || $0 == "." || $0 == ".." }) else {
            return .refused("not_a_dependency_directory")
        }
        // The walk every removal makes, rooted at the checkout: the checkout as spelled first.
        switch OwnedStorage.containedDirectory(checkout + "/" + relative, under: checkout) {
        case .proven: break
        case .missing: return .refused("unreadable")
        case .refused("outside_root"): return .refused("outside_checkout")
        case .refused(let why): return .refused(why)
        }
        // No literal-pathspec variable for `check-ignore`: it refuses that magic outright (exit 128).
        guard let ignored = git(["check-ignore", "-q", "--no-index", "--", relative], cwd: checkout,
                                separateStandardError: true) else { return .refused("unreadable") }
        guard ignored.status == 0 else {
            return .refused(ignored.status == 1 ? "not_ignored" : "unreadable")
        }
        guard let tracked = git(["ls-files", "-z", "--", relative + "/"], cwd: checkout,
                                separateStandardError: true,
                                environment: ["GIT_LITERAL_PATHSPECS": "1"]),
              tracked.status == 0 else { return .refused("unreadable") }
        return tracked.output.isEmpty ? .reclaim : .refused("tracked")
    }

    /// Reclaim git-ignored `node_modules` and `.venv` directories inside a checkout this task owns.
    /// git lists the candidates (`ls-files --others --ignored --exclude-standard --directory`), so
    /// nothing here walks a checkout by hand, and each one then has to pass
    /// ``dependencyDirectoryVerdict(_:checkout:)``. Every removal and every refusal is audited.
    @discardableResult
    static func reclaimDependencyDirectories(checkout: String, taskID: String,
                                             roots: Orchestrator.ReclaimRoots = Orchestrator.reclaimRoots)
        -> [String] {
        func audit(_ event: String, _ path: String, _ why: String? = nil) {
            var fields = ["task": taskID, "path": path]
            if let why { fields["why"] = why }
            RemoteAuth.audit(event, fields)
        }
        guard ownedCheckoutDirectory(checkout, taskID: taskID, root: roots.worktrees) else {
            audit("orchestrator.dependency.kept", checkout, "path_not_owned")
            return []
        }
        guard let listing = git(["ls-files", "--others", "--ignored", "--exclude-standard",
                                 "--directory", "-z"], cwd: checkout, timeout: 120,
                                separateStandardError: true),
              listing.status == 0, listing.outputIsUTF8 else {
            audit("orchestrator.dependency.kept", checkout, "unreadable")
            return []
        }
        var removed: [String] = []
        for entry in listing.output.components(separatedBy: "\0") where entry.hasSuffix("/") {
            let relative = String(entry.dropLast())
            guard let name = relative.components(separatedBy: "/").last,
                  Orchestrator.dependencyDirectoryNames.contains(name) else { continue }
            let path = checkout + "/" + relative
            switch dependencyDirectoryVerdict(relative, checkout: checkout) {
            case .refused(let why):
                audit("orchestrator.dependency.kept", path, why)
            case .reclaim:
                // Proved again from the worktree root down at the removal itself: the verdict ran
                // two git commands after its own walk, and the listing before it can take minutes.
                if let why = OwnedStorage.containedDirectory(path, under: roots.worktrees.path).refusal {
                    audit("orchestrator.dependency.kept", path, why)
                    continue
                }
                do {
                    try FileManager.default.removeItem(atPath: path)
                    removed.append(path)
                    audit("orchestrator.dependency.reclaimed", path)
                } catch {
                    audit("orchestrator.dependency.kept", path, "remove_failed")
                }
            }
        }
        return removed
    }

    /// `<worktree.cwd>/.build` for a task's own checkout, proved immediately before a removal: a
    /// real directory at every level from the worktree root down, still inside that root once
    /// resolved, and inside the `<root>/<slug>/<task-id>` checkout the task owns.
    static func buildOutputContainment(_ worktree: Orchestrator.Worktree, taskID: String,
                                       roots: Orchestrator.ReclaimRoots)
        -> OwnedStorage.ContainedDirectory {
        let build = worktree.cwd + "/.build"
        let contained = OwnedStorage.containedDirectory(build, under: roots.worktrees.path)
        guard contained == .proven else { return contained }
        guard ownedCheckoutDirectory(worktree.path, taskID: taskID, root: roots.worktrees),
              OwnedStorage.componentsBelow(worktree.path, in: build) != nil else {
            return .refused("path_not_owned")
        }
        return .proven
    }

    /// `.build` on its deadline, answering whether that deadline is now settled. It waits for the
    /// task's process first (``Orchestrator/buildDeadlineDecision(state:buildCleanupAt:owner:now:)``):
    /// a live or unreadable one keeps the deadline outstanding for the next beat. Then it removes
    /// only what ``buildOutputContainment(_:taskID:roots:)`` proves. A path that is not the task's
    /// to remove settles the deadline with nothing removed; a removal the filesystem refuses keeps it.
    static func reclaimBuildAtDeadline(
        _ task: Orchestrator.Task, roots: Orchestrator.ReclaimRoots = Orchestrator.reclaimRoots,
        now: Date,
        ownerStatus: (Orchestrator.Task) -> OwnedStorage.ProcessStatus = {
            OwnedStorage.processStatus(pid: $0.childPID, recordedStart: $0.childProcStart)
        }) -> Bool {
        guard let worktree = task.worktree,
              Orchestrator.buildDeadlineDecision(state: task.state, buildCleanupAt: task.buildCleanupAt,
                                                 owner: ownerStatus(task), now: now).mayCollect
        else { return false }
        let build = worktree.cwd + "/.build"
        switch buildOutputContainment(worktree, taskID: task.id, roots: roots) {
        case .missing:
            break
        case .refused(let why):
            RemoteAuth.audit("orchestrator.build.kept", ["task": task.id, "path": build, "why": why])
            return true
        case .proven:
            guard (try? FileManager.default.removeItem(atPath: build)) != nil else { return false }
        }
        RemoteAuth.audit("orchestrator.build.reclaimed", ["task": task.id, "path": build])
        return true
    }

    /// `<worktree.cwd>/.build` for a task with no build deadline outstanding — one never set, or
    /// set before the checkout was built in again. The same single directory the deadline removes,
    /// proved the same way immediately before it goes.
    private static func reclaimBuildOutput(_ task: Orchestrator.Task, worktree: Orchestrator.Worktree,
                                           roots: Orchestrator.ReclaimRoots) {
        let build = worktree.cwd + "/.build"
        switch buildOutputContainment(worktree, taskID: task.id, roots: roots) {
        case .missing:
            return
        case .refused(let why):
            RemoteAuth.audit("orchestrator.build.kept", ["task": task.id, "path": build, "why": why])
            return
        case .proven:
            break
        }
        do {
            try FileManager.default.removeItem(atPath: build)
            RemoteAuth.audit("orchestrator.build.reclaimed",
                             ["task": task.id, "path": build, "why": "after_finish"])
        } catch {
            RemoteAuth.audit("orchestrator.build.kept", ["task": task.id, "path": build, "why": "remove_failed"])
        }
    }

    /// A `work/` that exists with no deadline outstanding, removed once
    /// ``Orchestrator/afterFinishWorkDecision(state:workCleanupAt:settledAt:owner:graceMinutes:now:)``
    /// allows it — asked twice, so a process that took the pid in between still holds the
    /// directory — and once `<tasksRoot>/<task-id>/work` is proved, immediately before the removal,
    /// a real directory at every level from `tasksRoot` down. `lstat` on `work` alone follows the
    /// task directory above it, so one replaced by a symlink would hand this a real `work/` elsewhere.
    @discardableResult
    static func reclaimAfterFinishWork(_ task: Orchestrator.Task, tasksRoot: URL, graceMinutes: Int,
                                       now: Date,
                                       ownerStatus: (Orchestrator.Task) -> OwnedStorage.ProcessStatus)
        -> Bool {
        let work = tasksRoot.appendingPathComponent(task.id, isDirectory: true)
            .appendingPathComponent("work", isDirectory: true)
        func decision() -> OwnedStorage.Decision {
            Orchestrator.afterFinishWorkDecision(
                state: task.state, workCleanupAt: task.workCleanupAt,
                settledAt: task.finishedAt ?? task.created, owner: ownerStatus(task),
                graceMinutes: graceMinutes, now: now)
        }
        func kept(_ why: String) -> Bool {
            RemoteAuth.audit("orchestrator.work.kept", ["task": task.id, "path": work.path, "why": why])
            return false
        }
        let first = decision()
        if first.state == .unknown { return kept(first.why) }
        guard first.mayCollect, decision().mayCollect else { return false }
        switch OwnedStorage.containedDirectory(work.path, under: tasksRoot.path) {
        case .missing: return false
        case .refused(let why): return kept(why)
        case .proven: break
        }
        do { try FileManager.default.removeItem(at: work) } catch { return kept("remove_failed") }
        RemoteAuth.audit("orchestrator.work.reclaimed",
                         ["task": task.id, "path": work.path, "why": "after_finish"])
        return true
    }

    /// The build deadline's second half, off the main queue: the dependency directories of a
    /// checkout whose `.build` just went, if the task's process is already gone. A live one is not
    /// refused, only left for ``reclaimFinishedStorage(_:roots:graces:now:ownerStatus:)``.
    static func scheduleDependencyReclaim(_ task: Orchestrator.Task) {
        guard let worktree = task.worktree else { return }
        let roots = Orchestrator.reclaimRoots
        let grace = Config.shared.orchestratorBuildGraceMinutes
        Orchestrator.worktreeQueue.async {
            let owner = OwnedStorage.processStatus(pid: task.childPID,
                                                   recordedStart: task.childProcStart)
            guard Orchestrator.dependencyReclaimDecision(
                    state: task.state, buildCleanupAt: nil,
                    settledAt: task.finishedAt ?? task.created, owner: owner,
                    graceMinutes: grace, now: Date()).mayCollect,
                  FileManager.default.fileExists(atPath: worktree.path) else { return }
            reclaimDependencyDirectories(checkout: worktree.path, taskID: task.id, roots: roots)
        }
    }

    /// The settings the six-hourly pass reads, captured on the main queue before it leaves.
    struct FinishedStorageGraces: Equatable {
        var work: Int
        var build: Int
        var landedCheckout: Int
        var scratch: Int
        var preservedRetentionDays: Int

        static var current: FinishedStorageGraces {
            let config = Config.shared
            return FinishedStorageGraces(
                work: config.orchestratorWorkGraceMinutes, build: config.orchestratorBuildGraceMinutes,
                landedCheckout: config.orchestratorLandedCheckoutGraceMinutes,
                scratch: config.orchestratorScratchGraceMinutes,
                preservedRetentionDays: config.orchestratorReclaimedCheckoutRetentionDays)
        }
    }

    /// Which of `tasks` the retention sweep must hold, because the task's recorded process is still
    /// running or could not be ruled out. The caller passes exactly the rows a limit could remove —
    /// `Orchestrator.taskRetentionOwnerQuestions`, the record count's reach included — so the process
    /// table is read for those rows and no others.
    static func liveOwnerTaskIDs(_ tasks: [Orchestrator.Task]) -> Set<String> {
        Set(tasks.filter { task in
            let owner = OwnedStorage.processStatus(pid: task.childPID,
                                                   recordedStart: task.childProcStart)
            return owner == .alive || owner == .unreadable
        }.map { $0.id })
    }

    /// What `cleanup()` hands the worktree queue after its own sweep: one pass off the main queue,
    /// because it runs git and reads directories.
    static func scheduleFinishedStorageReclaim(_ tasks: [Orchestrator.Task]) {
        let roots = Orchestrator.reclaimRoots
        let graces = FinishedStorageGraces.current
        Orchestrator.worktreeQueue.async {
            reclaimFinishedStorage(tasks, roots: roots, graces: graces, now: Date())
        }
    }

    /// Every retained terminal task's after-finish `work/`, its checkout's build output and
    /// dependency directories once the build deadline and the process allow, and its landed
    /// checkout; then the owned scratch root and the preserved deltas past their retention. Each
    /// step decides for itself and keeps what it cannot prove; none waits for another to succeed.
    static func reclaimFinishedStorage(
        _ tasks: [Orchestrator.Task], roots: Orchestrator.ReclaimRoots,
        graces: FinishedStorageGraces, now: Date,
        ownerStatus: (Orchestrator.Task) -> OwnedStorage.ProcessStatus = {
            OwnedStorage.processStatus(pid: $0.childPID, recordedStart: $0.childProcStart)
        }) {
        let manager = FileManager.default
        for task in tasks where task.state.isTerminal && isTaskID(task.id) {
            let work = roots.tasks.appendingPathComponent(task.id, isDirectory: true)
                .appendingPathComponent("work", isDirectory: true)
            if manager.fileExists(atPath: work.path) {
                reclaimAfterFinishWork(task, tasksRoot: roots.tasks, graceMinutes: graces.work,
                                       now: now, ownerStatus: ownerStatus)
            }
            guard let worktree = task.worktree, manager.fileExists(atPath: worktree.path) else {
                continue
            }
            let owner = ownerStatus(task)
            if Orchestrator.dependencyReclaimDecision(
                state: task.state, buildCleanupAt: task.buildCleanupAt,
                settledAt: task.finishedAt ?? task.created, owner: owner,
                graceMinutes: graces.build, now: now).mayCollect {
                reclaimBuildOutput(task, worktree: worktree, roots: roots)
                reclaimDependencyDirectories(checkout: worktree.path, taskID: task.id, roots: roots)
            }
            let landed = Orchestrator.landedCheckoutDecision(
                state: task.state, landing: task.landing, owner: owner,
                graceMinutes: graces.landedCheckout, now: now)
            if landed.mayCollect {
                disposeLandedWorktree(worktree, taskID: task.id, roots: roots, now: now)
            } else if landed.state == .unknown {
                RemoteAuth.audit("orchestrator.worktree.kept", [
                    "task": task.id, "branch": worktree.branch, "path": worktree.path,
                    "why": landed.why,
                ])
            }
        }
        OwnedStorage.sweepScratch(root: roots.scratch, now: now, graceMinutes: graces.scratch)
        pruneReclaimedCheckouts(root: roots.preserved, retentionDays: graces.preservedRetentionDays,
                                now: now)
    }

    /// Preserved deltas past `orchestrator_reclaimed_checkout_retention_days`. A candidate is only
    /// what this app wrote — `<task-id>/<attempt>/manifest.json` at version 1 naming that task — so
    /// anything else under the root, an attempt without a readable manifest included, stays.
    static func pruneReclaimedCheckouts(root: URL, retentionDays: Int, now: Date) {
        let manager = FileManager.default
        func isDirectory(_ path: String) -> Bool {
            var info = stat()
            return lstat(path, &info) == 0 && (info.st_mode & S_IFMT) == S_IFDIR
        }
        guard isDirectory(root.path),
              let tasks = try? manager.contentsOfDirectory(atPath: root.path) else { return }
        let cutoff = now.addingTimeInterval(-Double(retentionDays) * 86_400)
        for task in tasks where isTaskID(task) && isDirectory(root.path + "/" + task) {
            let taskDirectory = root.path + "/" + task
            for attempt in (try? manager.contentsOfDirectory(atPath: taskDirectory)) ?? [] {
                let directory = taskDirectory + "/" + attempt
                guard isDirectory(directory),
                      let data = manager.contents(atPath: directory + "/manifest.json"),
                      let manifest = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
                      manifest["clawdline_reclaimed_checkout"] as? Int == 1,
                      manifest["task"] as? String == task,
                      let created = manifest["created_at"] as? Double,
                      Date(timeIntervalSince1970: created) < cutoff,
                      // Proved from the root down at the removal, not only when it was listed.
                      OwnedStorage.containedDirectory(directory, under: root.path) == .proven,
                      (try? manager.removeItem(atPath: directory)) != nil else { continue }
                RemoteAuth.audit("orchestrator.worktree.preservation_expired",
                                 ["task": task, "path": directory])
            }
            if (try? manager.contentsOfDirectory(atPath: taskDirectory))?.isEmpty == true,
               OwnedStorage.containedDirectory(taskDirectory, under: root.path) == .proven {
                try? manager.removeItem(atPath: taskDirectory)
            }
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
