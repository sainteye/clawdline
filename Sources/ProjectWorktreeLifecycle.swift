import CryptoKit
import Foundation

/// **The one owner of Project worktree lifecycle evidence, and the one cleanup executor.**
///
/// Everything that reads a Project's registered worktrees for lifecycle purposes — the Git and
/// filesystem probe, the seven-class classifier, recovery preservation, the pinned cleanup preview
/// and the confirmed apply — lives in this type. `ProjectWorktreeHTTP` is only its authenticated
/// codec; the Board and Web UI consume the read model and never gain a probe or executor of their
/// own. `OrchestratorDraft.disposeWorktree` remains the task-ending disposal of a checkout that
/// provably holds nothing, and is not reachable from here: a person-confirmed cleanup of residue
/// is a different authority with a different proof.
///
/// Rules this owner keeps, each of which a test names:
///
/// - **Reads never mutate Git and never probe.** `snapshot` serves the bounded cache; only an
///   explicit refresh, preview or apply runs git, always with optional locks off and never `fetch`.
/// - **Unknown and error never mean clean.** A count that could not be read is `nil` on the wire,
///   an unreadable row carries `unknown_incomplete_evidence`, and neither can make a row eligible.
/// - **Classes accumulate.** A row that is two things at once reaches every reader as both.
/// - **No caller-selected filesystem target.** A preview names opaque worktree ids from a fresh
///   observation; every path, branch and ref an action touches is re-derived from Git's own
///   registration and rechecked against the preview's pins immediately before it is used.
/// - **Preservation comes first and proves itself.** Dirty bytes are written as a binary patch and
///   reapplied to their named base in a private index; the resulting tree must equal the snapshot
///   tree before any destructive step runs. Preservation never grants deletion authority by itself.
final class ProjectWorktreeLifecycleService {
    static let schemaVersion = 1
    static let rowLimit = 200
    static let dirtyPathLimit = 500
    static let statusEntryLimit = 5000
    static let changedPathLimit = 1000
    static let projectCacheLimit = 32
    static let previewLifetime: TimeInterval = 300
    static let previewLimit = 16
    static let ledgerLimit = 256
    static let ledgerRetention: TimeInterval = 30 * 24 * 3600
    /// How long one local observation reads as `current` before it is `stale`.
    static let localFreshness: TimeInterval = 300
    /// How old the last observation of the canonical remote may be before it is `stale`.
    static let canonicalFreshness: TimeInterval = 6 * 3600
    static let refreshCoalesceWindow: TimeInterval = 5
    static let observationBudget: TimeInterval = 120

    enum Classification: String, CaseIterable {
        case activeInUse = "active_in_use"
        case landedIdenticalResidue = "landed_identical_residue"
        case genuinelyUnlanded = "genuinely_unlanded"
        case mixedConflicted = "mixed_conflicted"
        case taskOwnedTemporary = "task_owned_temporary"
        case prunableStaleMetadata = "prunable_stale_metadata"
        case unknownIncompleteEvidence = "unknown_incomplete_evidence"
    }

    /// A typed refusal. `status` and `code` are the wire contract; `extra` carries evidence.
    struct Refusal: Error {
        let status: Int
        let code: String
        let message: String
        var nextOwner: String? = nil
        var extra: [String: Any] = [:]
    }

    struct Issue: Error, Equatable {
        let code: String
        let message: String
        var json: [String: Any] { ["code": code, "message": message] }
    }

    struct ProjectIdentity: Equatable {
        let id: String
        let label: String
        let canonicalPath: String
    }

    struct TaskEvidence {
        let authoritative: Bool
        let tasks: [Orchestrator.Task]
    }

    struct LiveSession: Equatable {
        let terminalID: String
        let conversationID: String?
        let cwd: String
    }

    struct LiveEvidence {
        let complete: Bool
        let observedAt: Date?
        let sessions: [LiveSession]
    }

    struct StorageEvidence: Equatable {
        let bytes: Int?
        let observedAt: Date?
        let error: Issue?

        var complete: Bool { bytes != nil && error == nil }
    }

    /// Every dependency this owner reads, so a test can replace each one without a global seam.
    struct Ports {
        var stateDirectory: URL
        var managedWorktreeRoot: URL
        var projectDirectories: () -> [String]
        var tasks: () -> TaskEvidence
        var live: () -> LiveEvidence
        var now: () -> Date
        /// Allocated bytes for one checkout. This stays inside the sole lifecycle probe and is
        /// injected so tests never depend on a machine's `du` implementation.
        var storageBytes: (String, TimeInterval) -> Result<Int, Issue>
        /// Deterministic race injection for the focused lifecycle tests. Production is a no-op;
        /// the executor still performs every last-look from Git and the registries themselves.
        var beforeApplyAction: (ActionKind, String) -> Void = { _, _ in }

        static func production() -> Ports {
            Ports(
                stateDirectory: FileManager.default.homeDirectoryForCurrentUser
                    .appendingPathComponent("Library/Application Support/Clawdline/worktree-lifecycle",
                                            isDirectory: true),
                managedWorktreeRoot: OrchestratorDraft.worktreeRoot,
                projectDirectories: {
                    let registry = OrchestratorRegistry.withTaskRecords { records in
                        records.taskValues().map(\.projectDir)
                    }
                    return ProjectIcon.knownPaths() + registry
                },
                tasks: {
                    let authoritative = Orchestrator.storeIsAuthoritative()
                    let rows = OrchestratorRegistry.withTaskRecords { $0.taskValues() }
                    return TaskEvidence(authoritative: authoritative, tasks: rows)
                },
                live: {
                    let publication = SessionWatch.shared.publishedInventory()
                    let sessions = publication.targets.compactMap { target -> LiveSession? in
                        let identity = publication.identities[target.id]
                        guard let cwd = identity?.workingDirectory ?? target.cwd, !cwd.isEmpty else {
                            return nil
                        }
                        return LiveSession(terminalID: target.id,
                                           conversationID: identity?.conversationID, cwd: cwd)
                    }
                    return LiveEvidence(complete: publication.complete,
                                        observedAt: publication.observedAt, sessions: sessions)
                },
                now: Date.init,
                storageBytes: { path, timeout in
                    ProjectWorktreeLifecycleService.measureStorageBytes(path, timeout: timeout)
                },
                beforeApplyAction: { _, _ in })
        }
    }

    static let shared = ProjectWorktreeLifecycleService(ports: .production())

    let ports: Ports
    private let stateLock = NSLock()
    private let applyLock = NSLock()
    /// One observation at a time for this owner. A refresh that arrives while one is running for
    /// the same Project waits for that one rather than starting a second.
    private let observationLock = NSLock()
    /// Read only while `observationLock` is held. Every Git subprocess in one observation gets
    /// at most the time still left in the public two-minute budget.
    private var observationDeadline: Date?
    private var cache: [String: CachedSnapshot] = [:]
    private var cacheOrder: [String] = []
    private var previews: [String: Preview] = [:]

    init(ports: Ports) {
        self.ports = ports
    }

    // MARK: - Observation model

    struct Owner: Equatable {
        enum Kind: String { case task, repository, foreign, unknown }
        let kind: Kind
        let evidence: String
        var taskID: String? = nil
        var sessionID: String? = nil
        var terminalID: String? = nil
        var title: String? = nil
        var taskState: Orchestrator.State? = nil
        var rootLabel: String? = nil
        var landing: String? = nil
        var recordedBase: String? = nil
        var recordedBranch: String? = nil
        var purpose: String? = nil
        var note: String? = nil
        var currentStatus: String? = nil
        var createdAt: Date? = nil
        var startedAt: Date? = nil
        var finishedAt: Date? = nil
        var originSessionID: String? = nil
        var originTitle: String? = nil

        var json: [String: Any] {
            ["taskId": taskID ?? NSNull(), "sessionId": sessionID ?? NSNull(),
             "terminalId": terminalID ?? NSNull(), "title": title ?? NSNull(),
             "evidence": evidence]
        }

        var nextOwner: String {
            switch kind {
            case .task:
                return rootLabel.map { "\($0) (the root that dispatched task \(taskID ?? "?"))" }
                    ?? "the root that dispatched task \(taskID ?? "?")"
            case .repository: return "the repository owner working in the main checkout"
            case .foreign: return "the person or tool that registered this worktree"
            case .unknown: return "the Project root: ownership evidence for this checkout is missing"
            }
        }
    }

    struct DirtyEntry: Equatable {
        let path: String
        let staged: Bool
        let modified: Bool
        let untracked: Bool
        let conflicted: Bool
        /// Staged bytes that differ from both HEAD and the working file cannot be carried by a
        /// working-tree patch, so preservation refuses rather than silently dropping them.
        let indexDiffersFromWorktree: Bool
        let deleted: Bool
        let worktreeMode: String?
        var worktreeBlob: String? = nil
        var identicalToTarget: Bool? = nil
    }

    struct Row {
        let worktreeID: String
        let path: String
        let isMain: Bool
        let exists: Bool
        let locked: Bool
        let prunable: Bool
        let branch: String?
        let head: String?
        let owner: Owner
        var active: Bool?
        var activeEvidence: [String] = []
        var statusComplete = false
        var staged: Int? = nil
        var modified: Int? = nil
        var untracked: Int? = nil
        var ignored = 0
        var dirty: [DirtyEntry] = []
        var conflicted = false
        var aheadOfTarget: Int? = nil
        var mergeBase: String? = nil
        var identicalPaths: [String] = []
        var unlandedPaths: [String] = []
        var unknownPaths: [String] = []
        var errors: [Issue] = []
        var classifications: [Classification] = []
        var blockers: [Issue] = []
        var actions: [ActionKind] = []
        var statusDigest = ""
        var contentDigest = ""
        var storage = StorageEvidence(bytes: nil, observedAt: nil,
                                      error: Issue(code: "storage_not_observed",
                                                   message: "Disk usage has not been observed."))

        var base: String? { owner.recordedBase ?? mergeBase }
        var dirtyTracked: Bool { dirty.contains { !$0.untracked } }
        var hasDirty: Bool { !dirty.isEmpty }
    }

    struct CanonicalTarget {
        let branch: String?
        let localRef: String?
        let localOID: String?
        let remoteRef: String?
        let remoteOID: String?
        let remoteObservedAt: Date?
        let published: Bool?
        let error: Issue?
    }

    struct Observation {
        let project: ProjectIdentity
        let repositoryID: String
        let repositoryLabel: String
        let commonDirectory: String?
        let mainPath: String?
        let observedAt: Date
        let target: CanonicalTarget
        var rows: [Row]
        let truncated: Bool
        let error: Issue?
        let liveComplete: Bool
    }

    private struct CachedSnapshot {
        let observation: Observation
        let generation: Int
    }

    enum ActionKind: String {
        case preservePatch = "preserve_patch"
        case removeCheckout = "remove_checkout"
        case deleteBranch = "delete_branch"
        case pruneMetadata = "prune_metadata"
    }

    // MARK: - Project resolution

    static func projectIdentity(forDirectory directory: String) -> ProjectIdentity? {
        guard let canonical = UsageLedger.canonicalProjectKey(projectDir: directory) else { return nil }
        return ProjectIdentity(id: ProjectBoardIntegration.projectID(canonical),
                               label: URL(fileURLWithPath: canonical).lastPathComponent,
                               canonicalPath: canonical)
    }

    static func isProjectID(_ value: String) -> Bool {
        guard value.hasPrefix("project-") else { return false }
        let digest = value.dropFirst("project-".count)
        return digest.count == 24 && digest.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
    }

    func resolveProject(_ id: String) -> Result<ProjectIdentity, Refusal> {
        guard Self.isProjectID(id) else {
            return .failure(Refusal(status: 400, code: "bad_project_id",
                message: "A Project is named by its Board id: project- and 24 lowercase hex digits."))
        }
        var seen = Set<String>()
        for directory in ports.projectDirectories() where seen.insert(directory).inserted {
            if let identity = Self.projectIdentity(forDirectory: directory), identity.id == id {
                return .success(identity)
            }
        }
        return .failure(Refusal(status: 404, code: "project_not_found",
            message: "No Project known to this Mac has that id."))
    }

    // MARK: - Reads

    /// The cached snapshot, or an explicit `not_observed` one. Never runs git.
    func snapshot(projectID: String) -> Result<[String: Any], Refusal> {
        switch resolveProject(projectID) {
        case .failure(let refusal): return .failure(refusal)
        case .success(let project):
            stateLock.lock()
            let cached = cache[project.id]
            stateLock.unlock()
            guard let cached else { return .success(notObservedSnapshot(project)) }
            return .success(Self.snapshotJSON(cached.observation, now: ports.now()))
        }
    }

    /// Run one bounded local observation now and return it. Coalesces with an observation that
    /// finished inside the coalescing window; never fetches.
    func refresh(projectID: String) -> Result<[String: Any], Refusal> {
        switch resolveProject(projectID) {
        case .failure(let refusal): return .failure(refusal)
        case .success(let project):
            observationLock.lock(); defer { observationLock.unlock() }
            let now = ports.now()
            stateLock.lock()
            let cached = cache[project.id]
            stateLock.unlock()
            if let cached, now.timeIntervalSince(cached.observation.observedAt) < Self.refreshCoalesceWindow,
               cached.observation.error == nil {
                return .success(Self.snapshotJSON(cached.observation, now: now))
            }
            var observation = observe(project, only: nil)
            if observation.error != nil, let cached {
                // An observation that could not read the repository keeps the last good rows,
                // which the serve-time freshness then reports as stale rather than current.
                observation = Observation(
                    project: cached.observation.project, repositoryID: cached.observation.repositoryID,
                    repositoryLabel: cached.observation.repositoryLabel,
                    commonDirectory: cached.observation.commonDirectory,
                    mainPath: cached.observation.mainPath, observedAt: cached.observation.observedAt,
                    target: cached.observation.target, rows: cached.observation.rows,
                    truncated: cached.observation.truncated, error: observation.error,
                    liveComplete: cached.observation.liveComplete)
            }
            store(observation)
            return .success(Self.snapshotJSON(observation, now: now))
        }
    }

    private func store(_ observation: Observation) {
        stateLock.lock(); defer { stateLock.unlock() }
        let generation = (cache[observation.project.id]?.generation ?? 0) + 1
        cache[observation.project.id] = CachedSnapshot(observation: observation, generation: generation)
        cacheOrder.removeAll { $0 == observation.project.id }
        cacheOrder.append(observation.project.id)
        while cacheOrder.count > Self.projectCacheLimit {
            cache[cacheOrder.removeFirst()] = nil
        }
    }

    private func notObservedSnapshot(_ project: ProjectIdentity) -> [String: Any] {
        [
            "schemaVersion": Self.schemaVersion,
            "project": ["id": project.id, "label": project.label],
            "repository": ["id": Self.repositoryID(project.canonicalPath), "label": project.label,
                           "canonicalPath": project.canonicalPath],
            "observedAt": NSNull(), "complete": false,
            "error": ["code": "not_observed",
                      "message": "This Mac has not observed these worktrees yet; request a refresh."],
            "rows": [], "truncated": false,
            "counts": ["rows": NSNull(), "active": NSNull(), "staged": NSNull(),
                       "modified": NSNull(), "untracked": NSNull(), "storageBytes": NSNull(),
                       "unknown": NSNull()],
        ]
    }

    // MARK: - Wire encoding

    static func rfc3339(_ date: Date?) -> Any {
        guard let date else { return NSNull() }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.string(from: date)
    }

    static func repositoryID(_ canonical: String) -> String {
        "repository-" + hex(Data(canonical.utf8)).prefix(24)
    }

    static func hex(_ data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    static func digest(_ parts: [String]) -> String {
        hex(Data(parts.joined(separator: "\u{1f}").utf8))
    }

    static func boundedNarrative(_ value: String?, limit: Int) -> String? {
        guard let value else { return nil }
        let compact = value.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
        guard !compact.isEmpty else { return nil }
        return compact.count <= limit ? compact : String(compact.prefix(limit - 1)) + "…"
    }

    /// `du -sk` is the platform's bounded allocated-size observation below the checkout path.
    /// Linked worktrees do not duplicate the shared object store; the primary checkout's `.git`
    /// directory is part of that path and is therefore included.
    static func measureStorageBytes(_ path: String, timeout: TimeInterval) -> Result<Int, Issue> {
        guard timeout > 0,
              let answer = Process.collect("/usr/bin/du", ["-sk", path], timeout: timeout),
              answer.status == 0,
              let text = String(data: answer.output, encoding: .utf8),
              let token = text.split(whereSeparator: { $0 == " " || $0 == "\t" || $0 == "\n" }).first,
              let kibibytes = Int(token), kibibytes >= 0 else {
            return .failure(Issue(code: "storage_unavailable",
                                  message: "The bounded disk-usage observation did not complete."))
        }
        let (bytes, overflow) = kibibytes.multipliedReportingOverflow(by: 1024)
        guard !overflow, bytes <= 9_007_199_254_740_991 else {
            return .failure(Issue(code: "storage_out_of_range",
                                  message: "The observed disk usage is outside JSON's safe integer range."))
        }
        return .success(bytes)
    }

    static func snapshotJSON(_ observation: Observation, now: Date) -> [String: Any] {
        let rows = observation.rows
        let stale = now.timeIntervalSince(observation.observedAt) > localFreshness
        let complete = observation.error == nil && !observation.truncated && !stale
            && rows.allSatisfy { $0.statusComplete && $0.errors.isEmpty && $0.active != nil
                && $0.storage.complete }
        func sum(_ value: (Row) -> Int?) -> Any {
            guard observation.error == nil, !observation.truncated else { return NSNull() }
            var total = 0
            for row in rows {
                guard let count = value(row) else { return NSNull() }
                let (next, overflow) = total.addingReportingOverflow(count)
                guard !overflow, next <= 9_007_199_254_740_991 else { return NSNull() }
                total = next
            }
            return total
        }
        let counts: [String: Any] = [
            "rows": observation.error == nil && !observation.truncated ? rows.count : NSNull(),
            "active": observation.error == nil && !observation.truncated
                && rows.allSatisfy({ $0.active != nil }) ? rows.filter { $0.active == true }.count : NSNull(),
            "staged": sum { $0.staged }, "modified": sum { $0.modified },
            "untracked": sum { $0.untracked },
            "storageBytes": sum { $0.storage.bytes },
            "unknown": observation.error == nil && !observation.truncated
                ? rows.filter { $0.classifications.contains(.unknownIncompleteEvidence) }.count : NSNull(),
        ]
        return [
            "schemaVersion": schemaVersion,
            "project": ["id": observation.project.id, "label": observation.project.label],
            "repository": ["id": observation.repositoryID, "label": observation.repositoryLabel,
                           "canonicalPath": observation.project.canonicalPath],
            "observedAt": rfc3339(observation.observedAt),
            "complete": complete,
            "error": observation.error?.json ?? NSNull(),
            "rows": rows.map { rowJSON($0, observation: observation, now: now) },
            "truncated": observation.truncated,
            "counts": counts,
        ]
    }

    static func rowJSON(_ row: Row, observation: Observation, now: Date) -> [String: Any] {
        let age = now.timeIntervalSince(observation.observedAt)
        let localState: String
        let localError: Any
        if let failure = row.errors.first(where: { $0.code != "worktree_path_missing" }) {
            localState = "failed"; localError = failure.json
        } else if let missing = row.errors.first {
            localState = "unknown"; localError = missing.json
        } else if let error = observation.error {
            localState = "stale"; localError = error.json
        } else {
            localState = age > localFreshness ? "stale" : "current"; localError = NSNull()
        }
        let target = observation.target
        let canonical: [String: Any]
        if let error = target.error {
            canonical = ["state": "failed", "observedAt": NSNull(), "ref": target.localRef ?? NSNull(),
                         "oid": target.localOID ?? NSNull(), "error": error.json]
        } else if let remoteRef = target.remoteRef {
            let fresh = target.remoteObservedAt.map { now.timeIntervalSince($0) <= canonicalFreshness }
            canonical = ["state": fresh == true ? "current" : (fresh == false ? "stale" : "unknown"),
                         "observedAt": rfc3339(target.remoteObservedAt), "ref": remoteRef,
                         "oid": target.remoteOID ?? NSNull(),
                         "error": fresh == nil
                            ? Issue(code: "canonical_remote_unobserved",
                                    message: "No fetch of the canonical remote has been recorded.").json
                            : NSNull()]
        } else {
            canonical = ["state": age > localFreshness ? "stale" : "current",
                         "observedAt": rfc3339(observation.observedAt),
                         "ref": target.localRef ?? NSNull(), "oid": target.localOID ?? NSNull(),
                         "error": NSNull()]
        }
        let status: [String: Any] = row.statusComplete
            ? ["complete": true, "staged": row.staged ?? NSNull(), "modified": row.modified ?? NSNull(),
               "untracked": row.untracked ?? NSNull()]
            : ["complete": false, "staged": NSNull(), "modified": NSNull(), "untracked": NSNull()]
        let purpose: Any = row.isMain
            ? "Canonical checkout for \(observation.repositoryLabel)"
            : row.owner.purpose.map { $0 as Any } ?? NSNull()
        let origin: [String: Any] = [
            "sessionId": row.owner.originSessionID.map { $0 as Any } ?? NSNull(),
            "title": row.owner.originTitle.map { $0 as Any } ?? NSNull(),
        ]
        let context: [String: Any] = [
            "purpose": purpose,
            "note": row.owner.note ?? NSNull(),
            "currentStatus": row.owner.currentStatus ?? NSNull(),
            "state": row.owner.taskState?.rawValue ?? (row.isMain ? "repository" : "unknown"),
            "createdAt": rfc3339(row.owner.createdAt),
            "startedAt": rfc3339(row.owner.startedAt),
            "finishedAt": rfc3339(row.owner.finishedAt),
            "originSession": origin,
            "evidence": row.owner.evidence,
        ]
        let storage: [String: Any] = [
            "complete": row.storage.complete,
            "bytes": row.storage.bytes ?? NSNull(),
            "observedAt": rfc3339(row.storage.observedAt),
            "error": row.storage.error?.json ?? NSNull(),
        ]
        return [
            "worktreeId": row.worktreeID, "path": row.path,
            "branch": row.branch ?? NSNull(), "base": row.base ?? NSNull(), "head": row.head ?? NSNull(),
            "target": target.branch ?? NSNull(),
            "owner": row.owner.json,
            "active": row.active.map { $0 as Any } ?? NSNull(),
            "status": status,
            "classifications": row.classifications.map(\.rawValue),
            "localObservation": ["state": localState, "observedAt": rfc3339(observation.observedAt),
                                 "head": row.head ?? NSNull(), "error": localError],
            "canonicalTargetObservation": canonical,
            "context": context,
            "storage": storage,
            "cleanup": ["eligible": !row.actions.isEmpty && row.blockers.isEmpty,
                        "blockers": row.blockers.map(\.json),
                        "nextOwner": row.blockers.isEmpty ? NSNull() : row.owner.nextOwner as Any],
        ]
    }

    // MARK: - Git

    private static let gitEnvironment = ["GIT_LITERAL_PATHSPECS": "1", "LC_ALL": "C"]

    private func git(_ arguments: [String], cwd: String, environment: [String: String] = [:],
                     timeout: TimeInterval = 15) -> OrchestratorDraft.GitAnswer? {
        let bounded: TimeInterval
        if let deadline = observationDeadline {
            let remaining = deadline.timeIntervalSince(ports.now())
            guard remaining > 0 else { return nil }
            bounded = min(timeout, remaining)
        } else {
            bounded = timeout
        }
        return OrchestratorDraft.git(arguments, cwd: cwd, timeout: bounded, separateStandardError: true,
                              environment: Self.gitEnvironment.merging(environment) { $1 })
    }

    /// stdout as data, or nil when git failed, timed out or wrote bytes that are not UTF-8.
    private func gitText(_ arguments: [String], cwd: String, environment: [String: String] = [:],
                         timeout: TimeInterval = 15) -> String? {
        guard let answer = git(arguments, cwd: cwd, environment: environment, timeout: timeout),
              answer.status == 0, answer.outputIsUTF8 else { return nil }
        return answer.output
    }

    private func oid(_ revision: String, cwd: String) -> String? {
        guard let text = gitText(["rev-parse", "--verify", "--quiet", "--end-of-options",
                                  "\(revision)^{commit}"], cwd: cwd) else { return nil }
        let value = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return Self.isObjectID(value) ? value : nil
    }

    static func isObjectID(_ value: String) -> Bool {
        (value.count == 40 || value.count == 64)
            && value.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
    }

    static func comparablePath(_ path: String) -> String {
        var canonical = OrchestratorDraft.canonicalFilesystemPath(path)
        for prefix in ["/private/var/", "/private/tmp/", "/private/etc/"] where canonical.hasPrefix(prefix) {
            canonical = String(canonical.dropFirst("/private".count))
        }
        return canonical
    }

    // MARK: - Probe and classifier

    struct RegisteredWorktree {
        let path: String
        let head: String?
        let branch: String?
        let locked: Bool
        let prunable: Bool
    }

    static func parseWorktreeList(_ text: String) -> [RegisteredWorktree] {
        var result: [RegisteredWorktree] = []
        var path: String?
        var head: String?
        var branch: String?
        var locked = false
        var prunable = false
        func flush() {
            if let path { result.append(RegisteredWorktree(path: path, head: head, branch: branch,
                                                           locked: locked, prunable: prunable)) }
            path = nil; head = nil; branch = nil; locked = false; prunable = false
        }
        for field in text.components(separatedBy: "\0") {
            if field.isEmpty { flush(); continue }
            if field.hasPrefix("worktree ") { flush(); path = String(field.dropFirst("worktree ".count)) }
            else if field.hasPrefix("HEAD ") { head = String(field.dropFirst("HEAD ".count)) }
            else if field.hasPrefix("branch refs/heads/") {
                branch = String(field.dropFirst("branch refs/heads/".count))
            }
            else if field == "locked" || field.hasPrefix("locked ") { locked = true }
            else if field == "prunable" || field.hasPrefix("prunable ") { prunable = true }
        }
        flush()
        return result
    }

    /// Porcelain v2, NUL separated, renames off. Returns nil when the output is not the shape
    /// this parser knows, which the caller reports as incomplete rather than as clean.
    static func parseStatus(_ text: String, limit: Int = statusEntryLimit)
        -> (entries: [DirtyEntry], ignored: Int, truncated: Bool)? {
        var entries: [DirtyEntry] = []
        var ignored = 0
        var tokens = text.components(separatedBy: "\0")
        if tokens.last == "" { tokens.removeLast() }
        var index = 0
        while index < tokens.count {
            let token = tokens[index]
            index += 1
            if entries.count + ignored >= limit { return (entries, ignored, true) }
            guard let kind = token.first else { return nil }
            switch kind {
            case "?":
                entries.append(DirtyEntry(path: String(token.dropFirst(2)), staged: false, modified: false,
                    untracked: true, conflicted: false, indexDiffersFromWorktree: false, deleted: false,
                    worktreeMode: nil))
            case "!":
                ignored += 1
            case "1", "2", "u":
                let fieldCount = kind == "1" ? 8 : (kind == "2" ? 9 : 10)
                let parts = token.split(separator: " ", maxSplits: fieldCount, omittingEmptySubsequences: false)
                guard parts.count == fieldCount + 1, parts[1].count == 2 else { return nil }
                let xy = Array(parts[1])
                let path = String(parts[fieldCount])
                if kind == "2" { index += 1 }
                let conflicted = kind == "u"
                let staged = xy[0] != "."
                let modified = xy[1] != "."
                let mode = conflicted ? String(parts[6]) : String(parts[5])
                entries.append(DirtyEntry(path: path, staged: staged, modified: modified, untracked: false,
                    conflicted: conflicted, indexDiffersFromWorktree: conflicted || (staged && modified),
                    deleted: xy[1] == "D" || (xy[0] == "D" && xy[1] == "."), worktreeMode: mode))
            default:
                return nil
            }
        }
        return (entries, ignored, false)
    }

    func observe(_ project: ProjectIdentity, only: Set<String>?) -> Observation {
        let started = ports.now()
        let deadline = started.addingTimeInterval(Self.observationBudget)
        observationDeadline = deadline
        defer { observationDeadline = nil }
        let tasks = ports.tasks()
        let live = ports.live()
        let repositoryID = Self.repositoryID(project.canonicalPath)
        func failed(_ code: String, _ message: String) -> Observation {
            Observation(project: project, repositoryID: repositoryID, repositoryLabel: project.label,
                        commonDirectory: nil, mainPath: nil, observedAt: started,
                        target: CanonicalTarget(branch: nil, localRef: nil, localOID: nil, remoteRef: nil,
                                                remoteOID: nil, remoteObservedAt: nil, published: nil,
                                                error: Issue(code: code, message: message)),
                        rows: [], truncated: false, error: Issue(code: code, message: message),
                        liveComplete: live.complete)
        }
        guard let common = OrchestratorDraft.gitCommonDirectory(at: project.canonicalPath) else {
            return failed("repository_unreadable", "Git could not name this Project's repository.")
        }
        guard let listing = gitText(["worktree", "list", "--porcelain", "-z"], cwd: project.canonicalPath) else {
            return failed("worktree_list_failed", "git worktree list did not answer.")
        }
        let registered = Self.parseWorktreeList(listing)
        guard let main = registered.first else {
            return failed("worktree_list_empty", "git worktree list named no checkout at all.")
        }
        let target = canonicalTarget(cwd: project.canonicalPath, commonDirectory: common)
        let truncated = registered.count > Self.rowLimit
        var rows: [Row] = []
        var budgetExceeded = false
        for (offset, entry) in registered.prefix(Self.rowLimit).enumerated() {
            if ports.now() >= deadline { budgetExceeded = true; break }
            let id = Self.worktreeID(commonDirectory: common, path: entry.path)
            if let only, !only.contains(id) { continue }
            var row = observeRow(entry, id: id, isMain: offset == 0, target: target, tasks: tasks, live: live)
            if ports.now() >= deadline {
                budgetExceeded = true
                row.errors.append(Issue(code: "observation_budget_exceeded",
                    message: "The bounded observation ran out of time before this row was complete."))
            }
            classify(&row, target: target, tasksAuthoritative: tasks.authoritative, live: live)
            rows.append(row)
        }
        return Observation(project: project, repositoryID: repositoryID, repositoryLabel: project.label,
                           commonDirectory: common, mainPath: main.path, observedAt: started,
                           target: target, rows: rows, truncated: truncated,
                           error: budgetExceeded ? Issue(code: "observation_budget_exceeded",
                               message: "The bounded observation stopped at its two-minute deadline.") : nil,
                           liveComplete: live.complete)
    }

    static func worktreeID(commonDirectory: String, path: String) -> String {
        "wt-" + digest([comparablePath(commonDirectory), comparablePath(path)]).prefix(24)
    }

    private func canonicalTarget(cwd: String, commonDirectory: String) -> CanonicalTarget {
        var branch: String?
        if let symbolic = gitText(["symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"], cwd: cwd) {
            let ref = symbolic.trimmingCharacters(in: .whitespacesAndNewlines)
            if ref.hasPrefix("refs/remotes/origin/") { branch = String(ref.dropFirst("refs/remotes/origin/".count)) }
        }
        if branch == nil {
            branch = ["main", "master"].first { oid("refs/heads/\($0)", cwd: cwd) != nil }
        }
        guard let branch, let localOID = oid("refs/heads/\(branch)", cwd: cwd) else {
            return CanonicalTarget(branch: branch, localRef: branch.map { "refs/heads/\($0)" }, localOID: nil,
                                   remoteRef: nil, remoteOID: nil, remoteObservedAt: nil, published: nil,
                                   error: Issue(code: "target_unresolved",
                                                message: "No local main or master branch resolves to a commit."))
        }
        let remoteRef = "refs/remotes/origin/\(branch)"
        guard let remoteOID = oid(remoteRef, cwd: cwd) else {
            return CanonicalTarget(branch: branch, localRef: "refs/heads/\(branch)", localOID: localOID,
                                   remoteRef: nil, remoteOID: nil, remoteObservedAt: nil, published: nil,
                                   error: nil)
        }
        let fetchHead = URL(fileURLWithPath: commonDirectory).appendingPathComponent("FETCH_HEAD").path
        let fetchedAt = (try? FileManager.default.attributesOfItem(atPath: fetchHead))?[.modificationDate] as? Date
        let published = git(["merge-base", "--is-ancestor", localOID, remoteOID], cwd: cwd).map { $0.status == 0 }
        return CanonicalTarget(branch: branch, localRef: "refs/heads/\(branch)", localOID: localOID,
                               remoteRef: remoteRef, remoteOID: remoteOID, remoteObservedAt: fetchedAt,
                               published: published, error: nil)
    }

    private func observeRow(_ entry: RegisteredWorktree, id: String, isMain: Bool, target: CanonicalTarget,
                            tasks: TaskEvidence, live: LiveEvidence) -> Row {
        var isDirectory: ObjCBool = false
        let exists = FileManager.default.fileExists(atPath: entry.path, isDirectory: &isDirectory)
            && isDirectory.boolValue
        let owner = resolveOwner(entry, isMain: isMain, tasks: tasks)
        var row = Row(worktreeID: id, path: entry.path, isMain: isMain, exists: exists, locked: entry.locked,
                      prunable: entry.prunable, branch: entry.branch,
                      head: entry.head.flatMap { Self.isObjectID($0) ? $0 : nil }, owner: owner, active: nil)
        resolveActivity(&row, live: live)
        guard exists else {
            row.errors.append(Issue(code: "worktree_path_missing",
                                    message: "The registered checkout directory does not exist."))
            row.statusDigest = Self.digest(["missing", entry.path])
            return row
        }
        let storageObservedAt = ports.now()
        let storageTimeout = min(5, max(0, observationDeadline?.timeIntervalSince(storageObservedAt) ?? 5))
        if storageTimeout > 0 {
            switch ports.storageBytes(entry.path, storageTimeout) {
            case .success(let bytes):
                row.storage = StorageEvidence(bytes: bytes, observedAt: storageObservedAt, error: nil)
            case .failure(let issue):
                row.storage = StorageEvidence(bytes: nil, observedAt: storageObservedAt, error: issue)
            }
        } else {
            row.storage = StorageEvidence(bytes: nil, observedAt: storageObservedAt,
                error: Issue(code: "storage_observation_budget_exceeded",
                             message: "The bounded observation ended before disk usage could be read."))
        }
        guard let statusText = gitText(["status", "--porcelain=v2", "-z", "--no-renames",
                                        "--untracked-files=all", "--ignored=matching"],
                                       cwd: entry.path, timeout: 30),
              let parsed = Self.parseStatus(statusText) else {
            row.errors.append(Issue(code: "status_unreadable", message: "git status did not answer in a known shape."))
            row.statusDigest = Self.digest(["unreadable", entry.path])
            return row
        }
        row.ignored = parsed.ignored
        row.dirty = parsed.entries
        row.conflicted = parsed.entries.contains { $0.conflicted } || operationInProgress(at: entry.path)
        if parsed.truncated || parsed.entries.count > Self.dirtyPathLimit {
            row.errors.append(Issue(code: "status_truncated",
                message: "More than \(Self.dirtyPathLimit) changed paths; this row is not compared path by path."))
        } else {
            row.statusComplete = true
            row.staged = parsed.entries.filter { $0.staged }.count
            row.modified = parsed.entries.filter { $0.modified && !$0.untracked }.count
            row.untracked = parsed.entries.filter { $0.untracked }.count
        }
        hashWorktreeFiles(&row)
        row.statusDigest = Self.digest(["status", row.head ?? "-", row.branch ?? "-"]
            + row.dirty.map { [$0.path, $0.staged ? "S" : "-", $0.modified ? "M" : "-", $0.untracked ? "U" : "-",
                               $0.conflicted ? "C" : "-", $0.worktreeMode ?? "-", $0.worktreeBlob ?? "-"]
                .joined(separator: ":") }.sorted())
        row.contentDigest = Self.contentDigest(row.dirty.map { ($0.path, $0.deleted ? nil : $0.worktreeMode,
                                                                 $0.deleted ? nil : $0.worktreeBlob) })
        compare(&row, target: target)
        return row
    }

    static func contentDigest(_ entries: [(path: String, mode: String?, blob: String?)]) -> String {
        digest(entries.map { entry in
            [entry.path, entry.mode ?? "deleted", entry.blob ?? "deleted"].joined(separator: "\u{0}")
        }.sorted())
    }

    private func operationInProgress(at path: String) -> Bool {
        guard let gitDirectory = gitText(["rev-parse", "--absolute-git-dir"], cwd: path)?
                .trimmingCharacters(in: .whitespacesAndNewlines), !gitDirectory.isEmpty else { return true }
        return ["MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"]
            .contains { FileManager.default.fileExists(atPath: URL(fileURLWithPath: gitDirectory)
                .appendingPathComponent($0).path) }
    }

    private func hashWorktreeFiles(_ row: inout Row) {
        var paths: [String] = []
        for index in row.dirty.indices {
            let entry = row.dirty[index]
            guard !entry.deleted, !entry.conflicted else { continue }
            let file = URL(fileURLWithPath: row.path).appendingPathComponent(entry.path).path
            guard let attributes = try? FileManager.default.attributesOfItem(atPath: file),
                  let type = attributes[.type] as? FileAttributeType else {
                row.dirty[index] = DirtyEntry(path: entry.path, staged: entry.staged, modified: entry.modified,
                    untracked: entry.untracked, conflicted: entry.conflicted,
                    indexDiffersFromWorktree: entry.indexDiffersFromWorktree, deleted: true,
                    worktreeMode: entry.worktreeMode)
                continue
            }
            guard type == .typeRegular else {
                row.errors.append(Issue(code: "unsupported_dirty_entry",
                    message: "A changed path is not a regular file and cannot be preserved as verified bytes."))
                continue
            }
            let permissions = (attributes[.posixPermissions] as? NSNumber)?.intValue ?? 0
            let mode = permissions & 0o111 != 0 ? "100755" : "100644"
            row.dirty[index] = DirtyEntry(path: entry.path, staged: entry.staged, modified: entry.modified,
                untracked: entry.untracked, conflicted: entry.conflicted,
                indexDiffersFromWorktree: entry.indexDiffersFromWorktree, deleted: false, worktreeMode: mode)
            paths.append(entry.path)
        }
        guard !paths.isEmpty else { return }
        guard let text = gitText(["hash-object", "--"] + paths, cwd: row.path, timeout: 60) else {
            row.errors.append(Issue(code: "hash_failed", message: "git hash-object could not read the changed files."))
            return
        }
        let hashes = text.split(separator: "\n").map(String.init)
        guard hashes.count == paths.count else {
            row.errors.append(Issue(code: "hash_failed", message: "git hash-object answered for a different set of files."))
            return
        }
        let byPath = Dictionary(uniqueKeysWithValues: zip(paths, hashes))
        for index in row.dirty.indices {
            if let blob = byPath[row.dirty[index].path] { row.dirty[index].worktreeBlob = blob }
        }
    }

    /// Final bytes of every path this checkout changed — committed on its branch or dirty in its
    /// checkout — compared with the local target. Dirty bytes decide a path they touch.
    private func compare(_ row: inout Row, target: CanonicalTarget) {
        guard target.error == nil, let targetOID = target.localOID else {
            row.errors.append(Issue(code: "comparison_unavailable",
                                    message: "There is no resolvable target to compare this checkout with."))
            return
        }
        guard let head = row.head else {
            row.errors.append(Issue(code: "head_unresolved", message: "This checkout has no resolvable HEAD commit."))
            return
        }
        guard let aheadText = gitText(["rev-list", "--count", "\(targetOID)..\(head)"], cwd: row.path),
              let ahead = Int(aheadText.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            row.errors.append(Issue(code: "ancestry_unreadable", message: "git could not count commits beyond the target."))
            return
        }
        row.aheadOfTarget = ahead
        row.mergeBase = gitText(["merge-base", head, targetOID], cwd: row.path)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        var committedChanged: [String] = []
        var committedDiffering = Set<String>()
        if ahead > 0 {
            guard let base = row.mergeBase, Self.isObjectID(base),
                  let changed = gitText(["diff", "--name-only", "-z", "--no-renames", base, head], cwd: row.path) else {
                row.errors.append(Issue(code: "branch_diff_unreadable",
                                        message: "git could not list what this branch changed."))
                return
            }
            committedChanged = changed.components(separatedBy: "\0").filter { !$0.isEmpty }
            guard committedChanged.count <= Self.changedPathLimit else {
                row.errors.append(Issue(code: "branch_diff_truncated",
                    message: "The branch changed more than \(Self.changedPathLimit) paths; it is not compared path by path."))
                return
            }
            if !committedChanged.isEmpty {
                guard let differing = gitText(["diff", "--name-only", "-z", "--no-renames", head, targetOID, "--"]
                                                + committedChanged, cwd: row.path, timeout: 30) else {
                    row.errors.append(Issue(code: "target_diff_unreadable",
                                            message: "git could not compare the branch with its target."))
                    return
                }
                committedDiffering = Set(differing.components(separatedBy: "\0").filter { !$0.isEmpty })
            }
        }
        var targetEntries: [String: (mode: String, blob: String)] = [:]
        let dirtyPaths = row.dirty.map(\.path)
        if !dirtyPaths.isEmpty {
            guard let listing = gitText(["ls-tree", "-z", "--full-tree", targetOID, "--"] + dirtyPaths,
                                        cwd: row.path, timeout: 30) else {
                row.errors.append(Issue(code: "target_tree_unreadable",
                                        message: "git could not read the target's version of the changed paths."))
                return
            }
            for record in listing.components(separatedBy: "\0") where !record.isEmpty {
                guard let tab = record.firstIndex(of: "\t") else { continue }
                let fields = record[record.startIndex..<tab].split(separator: " ")
                guard fields.count == 3, fields[1] == "blob" else { continue }
                targetEntries[String(record[record.index(after: tab)...])] = (String(fields[0]), String(fields[2]))
            }
        }
        var identical: [String] = []
        var unlanded: [String] = []
        var unknown: [String] = []
        var dirtyDecided = Set<String>()
        for index in row.dirty.indices {
            let entry = row.dirty[index]
            dirtyDecided.insert(entry.path)
            let answer: Bool?
            if entry.conflicted || entry.indexDiffersFromWorktree { answer = nil }
            else if entry.deleted { answer = targetEntries[entry.path] == nil }
            else if let blob = entry.worktreeBlob, let mode = entry.worktreeMode {
                answer = targetEntries[entry.path].map { $0.blob == blob && $0.mode == mode } ?? false
            } else { answer = nil }
            row.dirty[index].identicalToTarget = answer
            switch answer {
            case .some(true): identical.append(entry.path)
            case .some(false): unlanded.append(entry.path)
            case .none: unknown.append(entry.path)
            }
        }
        for path in committedChanged where !dirtyDecided.contains(path) {
            if committedDiffering.contains(path) { unlanded.append(path) } else { identical.append(path) }
        }
        row.identicalPaths = identical.sorted()
        row.unlandedPaths = unlanded.sorted()
        row.unknownPaths = unknown.sorted()
    }

    private func resolveOwner(_ entry: RegisteredWorktree, isMain: Bool, tasks: TaskEvidence) -> Owner {
        if isMain { return Owner(kind: .repository, evidence: "main_worktree") }
        let path = Self.comparablePath(entry.path)
        let root = Self.comparablePath(ports.managedWorktreeRoot.path)
        let relative = OrchestratorDraft.relativePath(from: root, to: path)
        let components = relative.map { $0.split(separator: "/").map(String.init) } ?? []
        let managedTaskID = components.count == 2 && OrchestratorDraft.isTaskID(components[1]) ? components[1] : nil
        guard let managedTaskID else { return Owner(kind: .foreign, evidence: "not_a_clawdline_managed_worktree") }
        guard tasks.authoritative else { return Owner(kind: .unknown, evidence: "task_registry_unavailable") }
        let matches = tasks.tasks.filter { task in
            guard let worktree = task.worktree else { return false }
            return Self.comparablePath(worktree.path) == path
        }
        guard !matches.isEmpty else { return Owner(kind: .unknown, evidence: "managed_path_without_task_record") }
        guard matches.count == 1, let task = matches.first, task.id == managedTaskID,
              let worktree = task.worktree, worktree.branch == OrchestratorDraft.worktreeBranch(for: task.id),
              entry.branch == nil || entry.branch == worktree.branch else {
            let task = matches[0]
            return Owner(kind: .unknown, evidence: "identity_conflict", taskID: task.id, title: task.title,
                         taskState: task.state, rootLabel: task.rootLabel)
        }
        let landing = task.landing.map { [$0.state.rawValue, $0.target ?? "-", $0.commit ?? "-"].joined(separator: "|") }
        return Owner(kind: .task, evidence: "exact_task_worktree_record", taskID: task.id,
                     sessionID: task.childSessionId.flatMap { UUID(uuidString: $0) != nil ? $0.lowercased() : nil },
                     terminalID: task.childTerminalId, title: task.title, taskState: task.state,
                     rootLabel: task.rootLabel, landing: landing, recordedBase: worktree.base,
                     recordedBranch: worktree.branch,
                     purpose: Self.boundedNarrative(task.title, limit: 240),
                     note: Self.boundedNarrative(task.plan, limit: 600),
                     currentStatus: Self.boundedNarrative(task.progress.last?.note ?? task.summary, limit: 300),
                     createdAt: task.created, startedAt: task.briefedAt ?? task.spawnedAt,
                     finishedAt: task.finishedAt,
                     originSessionID: task.rootSessionId.flatMap {
                         UUID(uuidString: $0) != nil ? $0.lowercased() : nil
                     },
                     originTitle: Self.boundedNarrative(task.rootLabel, limit: 120))
    }

    private func resolveActivity(_ row: inout Row, live: LiveEvidence) {
        var evidence: [String] = []
        if row.owner.kind == .task, row.owner.taskState?.isTerminal == false { evidence.append("task_live") }
        if row.owner.evidence == "identity_conflict", row.owner.taskState?.isTerminal == false {
            evidence.append("task_live")
        }
        let root = Self.comparablePath(row.path)
        for session in live.sessions {
            let cwd = Self.comparablePath(session.cwd)
            if cwd == root || cwd.hasPrefix(root + "/") { evidence.append("session:\(session.terminalID)") }
        }
        row.activeEvidence = evidence.sorted()
        if !evidence.isEmpty { row.active = true }
        else { row.active = live.complete && live.observedAt != nil ? false : nil }
    }

    private func classify(_ row: inout Row, target: CanonicalTarget, tasksAuthoritative: Bool, live: LiveEvidence) {
        var classes = Set<Classification>()
        let ownerLive = row.owner.taskState.map { !$0.isTerminal } ?? false
        if row.active == true { classes.insert(.activeInUse) }
        if row.active == nil { classes.insert(.unknownIncompleteEvidence) }
        if !row.exists || row.prunable {
            classes.insert(ownerLive || row.active == true ? .unknownIncompleteEvidence : .prunableStaleMetadata)
        }
        let evidenceGaps = row.errors.filter { $0.code != "worktree_path_missing" }
        if !evidenceGaps.isEmpty || (row.exists && !row.statusComplete) || !row.unknownPaths.isEmpty {
            classes.insert(.unknownIncompleteEvidence)
        }
        if row.owner.kind == .foreign || row.owner.kind == .unknown { classes.insert(.unknownIncompleteEvidence) }
        if row.conflicted || row.owner.evidence == "identity_conflict" { classes.insert(.mixedConflicted) }
        if !row.unlandedPaths.isEmpty { classes.insert(.genuinelyUnlanded) }
        if !row.identicalPaths.isEmpty { classes.insert(.landedIdenticalResidue) }
        if !row.unlandedPaths.isEmpty && !row.identicalPaths.isEmpty { classes.insert(.mixedConflicted) }
        if row.isMain, row.hasDirty { classes.insert(.unknownIncompleteEvidence) }
        let settledTask = row.owner.kind == .task && row.owner.taskState?.isTerminal == true
        if !row.isMain, row.exists, settledTask, row.statusComplete, evidenceGaps.isEmpty, row.unlandedPaths.isEmpty,
           row.unknownPaths.isEmpty, !row.conflicted, let ahead = row.aheadOfTarget, ahead == 0 {
            if row.identicalPaths.isEmpty, row.head != nil, row.head == row.owner.recordedBase {
                classes.insert(.taskOwnedTemporary)
            } else {
                classes.insert(.landedIdenticalResidue)
            }
        }
        row.classifications = Classification.allCases.filter { classes.contains($0) }
        plan(&row, target: target, tasksAuthoritative: tasksAuthoritative)
    }

    /// Cleanup eligibility: the blockers that make a destructive step unsafe, and the actions a
    /// preview may propose when there are none. Preservation-only is offered for unlanded bytes.
    private func plan(_ row: inout Row, target: CanonicalTarget, tasksAuthoritative: Bool) {
        var blockers: [Issue] = []
        let classes = Set(row.classifications)
        if row.isMain { blockers.append(Issue(code: "main_worktree", message: "The main checkout is never cleaned up here.")) }
        if row.active == true {
            blockers.append(Issue(code: "worktree_in_use",
                message: "A live task or Session is using this checkout (\(row.activeEvidence.joined(separator: ", ")))."))
        }
        if row.active == nil {
            blockers.append(Issue(code: "live_inventory_incomplete",
                message: "The Session inventory is incomplete, so liveness cannot be ruled out."))
        }
        if !tasksAuthoritative {
            blockers.append(Issue(code: "task_registry_unavailable", message: "The task registry is not authoritative."))
        }
        switch row.owner.kind {
        case .foreign:
            blockers.append(Issue(code: "foreign_worktree", message: "This checkout was not created by Clawdline."))
        case .unknown:
            blockers.append(Issue(code: row.owner.evidence == "identity_conflict" ? "owner_identity_conflict" : "owner_unknown",
                                  message: "Ownership of this checkout cannot be proved (\(row.owner.evidence))."))
        case .task, .repository: break
        }
        if row.locked { blockers.append(Issue(code: "worktree_locked", message: "git has this worktree locked.")) }
        if row.ignored > 0 {
            blockers.append(Issue(code: "ignored_entries_present",
                message: "Ignored entries are not byte-pinned, so this checkout cannot be removed."))
        }
        for gap in row.errors where gap.code != "worktree_path_missing" { blockers.append(gap) }
        if classes.contains(.mixedConflicted) {
            blockers.append(Issue(code: "mixed_or_conflicted", message: "Landed and unlanded work, a conflict, or an operation in progress share this checkout."))
        }
        if classes.contains(.genuinelyUnlanded) {
            blockers.append(Issue(code: "unlanded_work", message: "\(row.unlandedPaths.count) path(s) are not in the target."))
        }
        if classes.contains(.unknownIncompleteEvidence), !blockers.contains(where: { $0.code == "owner_unknown" }),
           row.unknownPaths.count > 0 {
            blockers.append(Issue(code: "evidence_incomplete", message: "\(row.unknownPaths.count) path(s) could not be compared."))
        }
        if let landing = row.owner.landing, landing.hasPrefix("pending|"), let branch = target.branch,
           !landing.hasPrefix("pending|-|"), !landing.hasPrefix("pending|\(branch)|") {
            blockers.append(Issue(code: "landing_target_mismatch",
                message: "The landing record names a different target than \(branch)."))
        }
        if target.remoteRef != nil, !row.isMain, classes.contains(.landedIdenticalResidue) {
            let fresh = target.remoteObservedAt.map { ports.now().timeIntervalSince($0) <= Self.canonicalFreshness } ?? false
            if !fresh {
                blockers.append(Issue(code: "canonical_target_stale",
                    message: "The canonical remote has not been fetched recently; refresh it before cleanup."))
            } else if target.published != true {
                blockers.append(Issue(code: "landing_not_published",
                    message: "The local target has commits the canonical remote does not."))
            }
        }
        let preservable = !row.isMain && row.exists && row.owner.kind != .foreign && row.active == false
            && row.statusComplete && !row.conflicted && row.hasDirty && row.errors.isEmpty
            && !row.dirty.contains { $0.indexDiffersFromWorktree }
            && OrchestratorDraft.relativePath(from: Self.comparablePath(ports.managedWorktreeRoot.path),
                                              to: Self.comparablePath(row.path)) != nil
        var actions: [ActionKind] = []
        let destructiveClasses: Set<Classification> = [.landedIdenticalResidue, .taskOwnedTemporary]
        if !classes.isEmpty, classes.isSubset(of: destructiveClasses) {
            if row.hasDirty { actions.append(.preservePatch) }
            actions.append(.removeCheckout)
            if let branch = row.branch, branch == row.owner.recordedBranch, row.aheadOfTarget == 0 {
                actions.append(.deleteBranch)
            }
        } else if classes == [.prunableStaleMetadata] && row.owner.kind == .task {
            actions.append(.pruneMetadata)
        }
        if classes.contains(.prunableStaleMetadata), row.owner.kind != .task,
           !blockers.contains(where: { $0.code == "foreign_worktree" || $0.code.hasPrefix("owner_") }) {
            blockers.append(Issue(code: "owner_unknown", message: "Stale metadata without a task record is not pruned here."))
        }
        if blockers.isEmpty {
            row.actions = actions
        } else if classes.contains(.genuinelyUnlanded), preservable,
                  !blockers.contains(where: { ["worktree_in_use", "live_inventory_incomplete", "mixed_or_conflicted",
                                                "task_registry_unavailable", "worktree_locked"].contains($0.code) }) {
            // Preserving bytes does not delete anything, so it is offered even where cleanup is not.
            row.actions = [.preservePatch]
        } else {
            row.actions = []
        }
        row.blockers = blockers
    }

    // MARK: - Pins

    struct Pins: Equatable {
        var repository: String
        var comparison: String
        var worktrees: [String: String]
        var owners: [String: String]
        var live: [String: String]
        var status: [String: String]

        var json: [String: Any] {
            ["repository": repository, "comparison": comparison, "worktrees": worktrees,
             "owners": owners, "live": live, "status": status]
        }
    }

    static func pins(_ observation: Observation, rows ids: [String]) -> Pins {
        var pins = Pins(
            repository: digest([observation.commonDirectory ?? "-", observation.mainPath ?? "-"]),
            comparison: digest([observation.target.branch ?? "-", observation.target.localOID ?? "-",
                                observation.target.remoteOID ?? "-", observation.target.published.map { "\($0)" } ?? "-"]),
            worktrees: [:], owners: [:], live: [:], status: [:])
        for row in observation.rows where ids.contains(row.worktreeID) {
            pins.worktrees[row.worktreeID] = digest([row.path, row.branch ?? "-", row.head ?? "-",
                                                     "\(row.locked)", "\(row.prunable)", "\(row.exists)"])
            let owner = row.owner
            pins.owners[row.worktreeID] = digest([owner.kind.rawValue, owner.evidence, owner.taskID ?? "-",
                                                  owner.taskState?.rawValue ?? "-", owner.landing ?? "-",
                                                  owner.recordedBase ?? "-", owner.recordedBranch ?? "-"])
            pins.live[row.worktreeID] = digest([row.active.map { "\($0)" } ?? "unknown"] + row.activeEvidence)
            pins.status[row.worktreeID] = digest([row.statusDigest, row.aheadOfTarget.map(String.init) ?? "-",
                                                  row.mergeBase ?? "-"] + row.classifications.map(\.rawValue))
        }
        return pins
    }

    // MARK: - Preview

    struct PlannedAction {
        let worktreeID: String
        let kind: ActionKind
        let path: String
        let branch: String?
        let head: String?
        let contentDigest: String
        let dirtyPaths: [String]
        let force: Bool
        let targetRef: String?
        let targetOID: String?
    }

    struct Preview {
        let id: String
        let project: ProjectIdentity
        let createdAt: Date
        let expiresAt: Date
        let pins: Pins
        let pinDigest: String
        let rows: [String]
        let actions: [PlannedAction]
        let prunable: [String]
        let json: [String: Any]
    }

    func preview(projectID: String, worktreeIDs: [String]?) -> Result<[String: Any], Refusal> {
        let project: ProjectIdentity
        switch resolveProject(projectID) {
        case .failure(let refusal): return .failure(refusal)
        case .success(let value): project = value
        }
        observationLock.lock()
        let observation = observe(project, only: worktreeIDs.map(Set.init))
        observationLock.unlock()
        if let error = observation.error {
            return .failure(Refusal(status: 409, code: error.code, message: error.message))
        }
        if let worktreeIDs {
            let found = Set(observation.rows.map(\.worktreeID))
            let missing = worktreeIDs.filter { !found.contains($0) }
            guard missing.isEmpty else {
                return .failure(Refusal(status: 404, code: "worktree_not_found",
                    message: "No registered worktree has that id in a fresh observation.",
                    extra: ["worktreeIds": missing]))
            }
        }
        let now = ports.now()
        let id = "wtp-" + UUID().uuidString.lowercased()
        let rows = observation.rows.map(\.worktreeID)
        let pins = Self.pins(observation, rows: rows)
        let expiresAt = now.addingTimeInterval(Self.previewLifetime)
        let pinDigest = Self.digest([id, Self.canonicalJSON(pins.json), "\(expiresAt.timeIntervalSince1970)"])
        var planned: [PlannedAction] = []
        var actionsJSON: [[String: Any]] = []
        var refusedJSON: [[String: Any]] = []
        let prunable = observation.rows.filter { $0.actions.contains(.pruneMetadata) }
            .map { Self.comparablePath($0.path) }.sorted()
        let preservedRoot = ports.stateDirectory.appendingPathComponent("preserved/\(id)", isDirectory: true)
        for row in observation.rows {
            if row.actions.isEmpty || !row.blockers.isEmpty {
                refusedJSON.append(["worktreeId": row.worktreeID, "classifications": row.classifications.map(\.rawValue),
                                    "blockers": (row.blockers.isEmpty
                                        ? [Issue(code: "nothing_to_clean", message: "No cleanup applies to this row.")]
                                        : row.blockers).map(\.json),
                                    "nextOwner": row.owner.nextOwner])
            }
            if row.actions.isEmpty { continue }
            let patch = preservedRoot.appendingPathComponent("\(row.worktreeID)/patch.diff").path
            for kind in row.actions {
                let action = PlannedAction(worktreeID: row.worktreeID, kind: kind, path: row.path, branch: row.branch,
                                           head: row.head, contentDigest: row.contentDigest,
                                           dirtyPaths: row.dirty.map(\.path), force: row.hasDirty,
                                           targetRef: observation.target.localRef,
                                           targetOID: observation.target.localOID)
                planned.append(action)
                let recovery: [String: Any]
                let removes: [String]
                let preserves: [String]
                let reason: String
                switch kind {
                case .preservePatch:
                    recovery = ["method": "git_binary_patch", "artifact": patch, "base": row.head ?? NSNull(),
                                "digest": row.contentDigest]
                    removes = []; preserves = row.dirty.map(\.path)
                    reason = row.actions == [.preservePatch]
                        ? "Unlanded bytes are preserved first; nothing is removed for this row."
                        : "Residue is preserved as a verified patch before the checkout is removed."
                case .removeCheckout:
                    recovery = row.hasDirty
                        ? ["method": "git_binary_patch", "artifact": patch, "base": row.head ?? NSNull(), "digest": row.contentDigest]
                        : ["method": row.aheadOfTarget == 0 && row.head != row.owner.recordedBase ? "target_contains_commit" : "none_required",
                           "artifact": observation.target.localRef ?? NSNull(), "base": row.head ?? NSNull(),
                           "digest": row.head ?? NSNull()]
                    removes = [row.path] + (row.ignored > 0 ? ["\(row.ignored) ignored entr\(row.ignored == 1 ? "y" : "ies")"] : [])
                    preserves = row.hasDirty ? row.dirty.map(\.path) : []
                    reason = row.classifications.contains(.taskOwnedTemporary)
                        ? "The finished task's checkout holds nothing beyond its base."
                        : "Every change in this checkout is already byte-identical in \(observation.target.branch ?? "the target")."
                case .deleteBranch:
                    recovery = ["method": "target_contains_commit", "artifact": observation.target.localRef ?? NSNull(),
                                "base": observation.target.localOID ?? NSNull(), "digest": row.head ?? NSNull()]
                    removes = ["refs/heads/\(row.branch ?? "")"]; preserves = []
                    reason = "The branch tip is already contained by the target; the ref is deleted only at that exact commit."
                case .pruneMetadata:
                    recovery = ["method": "none_required", "artifact": NSNull(), "base": NSNull(),
                                "digest": Self.digest(prunable)]
                    removes = ["git worktree metadata for \(row.path)"]; preserves = []
                    reason = "The registered directory is gone and the finished task no longer uses it."
                }
                actionsJSON.append(["worktreeId": row.worktreeID, "action": kind.rawValue, "reason": reason,
                                    "removes": removes, "preserves": preserves, "recovery": recovery])
            }
        }
        let json: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "previewId": id,
            "project": ["id": project.id, "label": project.label],
            "repository": ["id": observation.repositoryID, "label": observation.repositoryLabel,
                           "canonicalPath": project.canonicalPath],
            "createdAt": Self.rfc3339(now), "expiresAt": Self.rfc3339(expiresAt),
            "pinDigest": pinDigest, "pins": pins.json,
            "actions": actionsJSON, "refused": refusedJSON,
            "apply": ["method": "POST", "path": "/v1/projects/\(project.id)/worktrees/cleanup/apply",
                      "requires": ["preview_id", "pin_digest", "confirm", "idempotency_key"]],
        ]
        let preview = Preview(id: id, project: project, createdAt: now, expiresAt: expiresAt, pins: pins,
                              pinDigest: pinDigest, rows: rows, actions: planned, prunable: prunable, json: json)
        stateLock.lock()
        previews = previews.filter { $0.value.expiresAt > now }
        if previews.count >= Self.previewLimit,
           let oldest = previews.values.min(by: { $0.createdAt < $1.createdAt }) { previews[oldest.id] = nil }
        previews[id] = preview
        stateLock.unlock()
        // A preview of selected rows is not a whole observation, so it must not replace the cache.
        if worktreeIDs == nil { store(observation) }
        return .success(json)
    }

    static func canonicalJSON(_ object: Any) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: object,
                                                     options: [.sortedKeys, .withoutEscapingSlashes]) else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    // MARK: - Apply

    static func isIdempotencyKey(_ value: String) -> Bool {
        (16...128).contains(value.count) && value.unicodeScalars.allSatisfy {
            CharacterSet.alphanumerics.contains($0) && $0.isASCII || "-_.:".unicodeScalars.contains($0)
        }
    }

    func apply(projectID: String, previewID: String, pinDigest: String, confirm: Bool,
               idempotencyKey: String) -> Result<[String: Any], Refusal> {
        guard confirm else {
            return .failure(Refusal(status: 400, code: "confirmation_required",
                                    message: "Apply needs confirm:true; a preview alone never removes anything."))
        }
        guard Self.isIdempotencyKey(idempotencyKey) else {
            return .failure(Refusal(status: 400, code: "bad_idempotency_key",
                                    message: "idempotency_key must be 16–128 characters of letters, digits and -_.:"))
        }
        guard applyLock.try() else {
            return .failure(Refusal(status: 409, code: "cleanup_in_progress", message: "Another cleanup is being applied."))
        }
        defer { applyLock.unlock() }
        var ledger: Ledger
        switch loadLedger() {
        case .failure(let refusal): return .failure(refusal)
        case .success(let value): ledger = value
        }
        if let entry = ledger.entries[idempotencyKey] {
            guard entry.previewID == previewID, entry.pinDigest == pinDigest else {
                return .failure(Refusal(status: 409, code: "idempotency_key_reused",
                    message: "That idempotency key already belongs to a different preview."))
            }
            guard entry.state == "finished", var receipt = entry.receipt else {
                return .failure(Refusal(status: 409, code: "apply_outcome_unknown",
                    message: "An earlier apply with this key started and never recorded its outcome; take a fresh preview.",
                    nextOwner: "the machine operator: refresh and preview again"))
            }
            receipt["replayed"] = true
            if let status = receipt["httpStatus"] as? Int,
               let code = receipt["errorCode"] as? String {
                return .failure(Refusal(status: status, code: code,
                    message: "The recorded cleanup attempt failed; replay returns the same failure.",
                    extra: ["receipt": receipt]))
            }
            return .success(receipt)
        }
        let now = ports.now()
        stateLock.lock()
        let preview = previews[previewID]
        stateLock.unlock()
        guard let preview else {
            return .failure(Refusal(status: 404, code: "preview_not_found", message: "No preview with that id is held."))
        }
        guard preview.project.id == projectID else {
            return .failure(Refusal(status: 409, code: "preview_project_mismatch", message: "That preview belongs to another Project."))
        }
        guard now < preview.expiresAt else {
            return .failure(Refusal(status: 409, code: "preview_expired", message: "That preview has expired; take a fresh one."))
        }
        guard preview.pinDigest == pinDigest else {
            return .failure(Refusal(status: 409, code: "pin_digest_mismatch", message: "pin_digest does not match that preview."))
        }
        observationLock.lock()
        let observation = observe(preview.project, only: Set(preview.rows))
        observationLock.unlock()
        if let refusal = Self.pinRefusal(expected: preview.pins, observation: observation, rows: preview.rows) {
            return .failure(refusal)
        }
        let receiptID = "wtr-" + UUID().uuidString.lowercased()
        ledger.entries[idempotencyKey] = LedgerEntry(previewID: previewID, pinDigest: pinDigest, state: "started",
                                                     at: now, receipt: nil)
        guard saveLedger(ledger) else {
            return .failure(Refusal(status: 503, code: "cleanup_ledger_unavailable",
                                    message: "The cleanup ledger could not record the start of this apply; nothing was changed."))
        }
        var results: [[String: Any]] = []
        var failed = false
        let preserveFirst = preview.actions.filter { $0.kind == .preservePatch }
            + preview.actions.filter { $0.kind != .preservePatch }
        var preservedRows = Set<String>()
        for action in preserveFirst {
            if failed {
                results.append(Self.result(action, outcome: "skipped",
                    error: Issue(code: "earlier_action_failed", message: "An earlier action failed; nothing further ran.")))
                continue
            }
            ports.beforeApplyAction(action.kind, action.worktreeID)
            switch action.kind {
            case .preservePatch:
                switch preserve(action, previewID: previewID) {
                case .success(let recovery):
                    preservedRows.insert(action.worktreeID)
                    results.append(Self.result(action, outcome: "done", recovery: recovery))
                case .failure(let issue):
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: issue))
                }
            case .removeCheckout:
                if action.force, !preservedRows.contains(action.worktreeID) {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "preservation_missing",
                        message: "Dirty residue was not preserved, so the checkout was kept.")))
                    continue
                }
                if let issue = recheckRemoval(action, preview: preview) {
                    failed = true; results.append(Self.result(action, outcome: "failed", error: issue)); continue
                }
                var arguments = ["worktree", "remove"]
                if action.force { arguments.append("--force") }
                arguments.append(action.path)
                let answer = git(arguments, cwd: preview.project.canonicalPath, timeout: 60)
                if answer?.status == 0 {
                    results.append(Self.result(action, outcome: "done"))
                } else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "remove_failed",
                        message: String((answer?.errorOutput ?? "git worktree remove did not start").prefix(300)))))
                }
            case .deleteBranch:
                guard let branch = action.branch, let head = action.head,
                      let targetRef = action.targetRef, let targetOID = action.targetOID else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "branch_identity_missing",
                        message: "The branch, target ref, or its commit was not pinned.")))
                    continue
                }
                guard oid(targetRef, cwd: preview.project.canonicalPath) == targetOID,
                      git(["merge-base", "--is-ancestor", head, targetOID],
                          cwd: preview.project.canonicalPath)?.status == 0 else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(
                        code: "comparison_changed_during_apply",
                        message: "The canonical target moved or no longer contains the branch tip.")))
                    continue
                }
                let answer = git(["update-ref", "-d", "refs/heads/\(branch)", head], cwd: preview.project.canonicalPath)
                if answer?.status == 0 { results.append(Self.result(action, outcome: "done")) }
                else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "branch_delete_failed",
                        message: "The branch was not at its pinned commit, or git refused to delete it.")))
                }
            case .pruneMetadata:
                guard let listing = gitText(["worktree", "list", "--porcelain", "-z"],
                                            cwd: preview.project.canonicalPath),
                      Self.parseWorktreeList(listing).filter(\.prunable)
                        .map({ Self.comparablePath($0.path) }).sorted() == preview.prunable else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "prune_set_changed",
                        message: "git would prune a different set of worktrees than this preview pinned.")))
                    continue
                }
                // Never invoke repository-wide prune: it can delete corrupt admin records that
                // `worktree list` did not expose and the preview therefore could not pin.
                let answer = git(["worktree", "remove", action.path], cwd: preview.project.canonicalPath)
                if answer?.status == 0 { results.append(Self.result(action, outcome: "done")) }
                else {
                    failed = true
                    results.append(Self.result(action, outcome: "failed", error: Issue(code: "prune_failed",
                        message: "Git could not remove the exact pinned stale worktree record.")))
                }
            }
        }
        let done = results.filter { $0["outcome"] as? String == "done" }.count
        let finishedAt = ports.now()
        var receipt: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "receiptId": receiptID, "previewId": previewID,
            "idempotencyKey": idempotencyKey, "pinDigest": pinDigest,
            "state": failed ? (done > 0 ? "partial" : "failed") : "applied", "replayed": false,
            "startedAt": Self.rfc3339(now), "finishedAt": Self.rfc3339(finishedAt), "actions": results,
        ]
        if failed, results.contains(where: { ($0["error"] as? [String: Any])?["code"] as? String == "preservation_failed" }) {
            receipt["httpStatus"] = 500
            receipt["errorCode"] = "preservation_failed"
        }
        ledger.entries[idempotencyKey] = LedgerEntry(previewID: previewID, pinDigest: pinDigest, state: "finished",
                                                     at: finishedAt, receipt: receipt)
        let recorded = saveLedger(ledger)
        stateLock.lock(); previews[previewID] = nil; cache[preview.project.id] = nil; stateLock.unlock()
        var answer = receipt
        if !recorded { answer["ledger"] = Issue(code: "cleanup_ledger_unavailable",
                                                message: "The outcome could not be recorded; a replay will refuse.").json }
        if failed, results.contains(where: { ($0["error"] as? [String: Any])?["code"] as? String == "preservation_failed" }) {
            return .failure(Refusal(status: 500, code: "preservation_failed",
                message: "Preservation did not verify, so no destructive step ran.", extra: ["receipt": answer]))
        }
        return .success(answer)
    }

    static func result(_ action: PlannedAction, outcome: String, error: Issue? = nil,
                       recovery: [String: Any]? = nil) -> [String: Any] {
        ["worktreeId": action.worktreeID, "action": action.kind.rawValue, "outcome": outcome,
         "error": error?.json ?? NSNull(), "recovery": recovery ?? NSNull()]
    }

    static func pinRefusal(expected: Pins, observation: Observation, rows: [String]) -> Refusal? {
        if let error = observation.error {
            return Refusal(status: 409, code: "repository_changed", message: error.message)
        }
        let current = pins(observation, rows: rows)
        if current.repository != expected.repository {
            return Refusal(status: 409, code: "repository_changed", message: "The repository identity changed since the preview.")
        }
        if current.worktrees != expected.worktrees {
            return Refusal(status: 409, code: "worktree_changed", message: "A worktree's path, branch or HEAD changed since the preview.")
        }
        if current.owners != expected.owners {
            return Refusal(status: 409, code: "owner_changed", message: "A worktree's owner or landing record changed since the preview.")
        }
        if current.live != expected.live {
            return Refusal(status: 409, code: "live_changed", message: "A worktree became live, or its liveness became unknown.")
        }
        if current.comparison != expected.comparison {
            return Refusal(status: 409, code: "comparison_changed", message: "The target or canonical remote moved since the preview.")
        }
        if current.status != expected.status {
            return Refusal(status: 409, code: "status_changed", message: "Staged, modified or untracked content changed since the preview.")
        }
        return nil
    }

    /// The last look before removing a checkout repeats the complete row observation. Comparing
    /// path names alone is insufficient: a file can change bytes while retaining the same name.
    private func recheckRemoval(_ action: PlannedAction, preview: Preview) -> Issue? {
        observationLock.lock()
        let observation = observe(preview.project, only: Set([action.worktreeID]))
        observationLock.unlock()
        let expected = Pins(
            repository: preview.pins.repository,
            comparison: preview.pins.comparison,
            worktrees: preview.pins.worktrees.filter { $0.key == action.worktreeID },
            owners: preview.pins.owners.filter { $0.key == action.worktreeID },
            live: preview.pins.live.filter { $0.key == action.worktreeID },
            status: preview.pins.status.filter { $0.key == action.worktreeID })
        guard let refusal = Self.pinRefusal(
            expected: expected, observation: observation, rows: [action.worktreeID]) else { return nil }
        let code: String
        switch refusal.code {
        case "comparison_changed": code = "comparison_changed_during_apply"
        case "status_changed": code = "status_changed_during_apply"
        default: code = refusal.code + "_during_apply"
        }
        return Issue(code: code, message: refusal.message)
    }

    // MARK: - Preservation

    /// Write the checkout's staged, modified and untracked bytes as one binary patch against its
    /// HEAD, then prove it: applied to that base in a second private index it must produce the
    /// exact snapshot tree, and that tree must hold the bytes the preview hashed.
    func preserve(_ action: PlannedAction, previewID: String) -> Result<[String: Any], Issue> {
        let failure = { (message: String) -> Result<[String: Any], Issue> in
            .failure(Issue(code: "preservation_failed", message: message))
        }
        guard let head = action.head else { return failure("The checkout has no HEAD to name as the patch base.") }
        let directory = ports.stateDirectory.appendingPathComponent("preserved/\(previewID)/\(action.worktreeID)",
                                                                   isDirectory: true)
        let scratch = ports.stateDirectory.appendingPathComponent("tmp", isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
        } catch {
            return failure("The preservation directory could not be created: \(error.localizedDescription)")
        }
        let snapshotIndex = scratch.appendingPathComponent(UUID().uuidString + ".index").path
        let verifyIndex = scratch.appendingPathComponent(UUID().uuidString + ".index").path
        defer {
            try? FileManager.default.removeItem(atPath: snapshotIndex)
            try? FileManager.default.removeItem(atPath: verifyIndex)
        }
        let snapshotEnvironment = ["GIT_INDEX_FILE": snapshotIndex]
        guard git(["read-tree", head], cwd: action.path, environment: snapshotEnvironment)?.status == 0,
              git(["add", "-A"], cwd: action.path, environment: snapshotEnvironment, timeout: 60)?.status == 0,
              let treeText = gitText(["write-tree"], cwd: action.path, environment: snapshotEnvironment) else {
            return failure("A private snapshot index of the checkout could not be written.")
        }
        let tree = treeText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard Self.isObjectID(tree),
              let listing = gitText(["ls-tree", "-r", "-z", "--full-tree", tree, "--"] + action.dirtyPaths,
                                    cwd: action.path, timeout: 30) else {
            return failure("The snapshot tree could not be read back.")
        }
        var present: [String: (String, String)] = [:]
        for record in listing.components(separatedBy: "\0") where !record.isEmpty {
            guard let tab = record.firstIndex(of: "\t") else { continue }
            let fields = record[record.startIndex..<tab].split(separator: " ")
            guard fields.count == 3 else { continue }
            present[String(record[record.index(after: tab)...])] = (String(fields[0]), String(fields[2]))
        }
        let snapshotDigest = Self.contentDigest(action.dirtyPaths.map { path in
            (path, present[path]?.0, present[path]?.1)
        })
        guard snapshotDigest == action.contentDigest else {
            return failure("The checkout's bytes are not the bytes the preview hashed.")
        }
        guard let patch = git(["diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--no-renames",
                               head, tree], cwd: action.path, timeout: 60),
              patch.status == 0, patch.outputIsUTF8 else {
            return failure("git diff could not write the patch as text this owner can verify.")
        }
        let patchURL = directory.appendingPathComponent("patch.diff")
        let patchData = Data(patch.output.utf8)
        do {
            try patchData.write(to: patchURL, options: [.atomic])
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: patchURL.path)
        } catch {
            return failure("The patch could not be written: \(error.localizedDescription)")
        }
        let verifyEnvironment = ["GIT_INDEX_FILE": verifyIndex]
        guard git(["read-tree", head], cwd: action.path, environment: verifyEnvironment)?.status == 0,
              git(["apply", "--cached", "--binary", "--whitespace=nowarn", patchURL.path], cwd: action.path,
                  environment: verifyEnvironment, timeout: 60)?.status == 0,
              let verified = gitText(["write-tree"], cwd: action.path, environment: verifyEnvironment)?
                .trimmingCharacters(in: .whitespacesAndNewlines), verified == tree else {
            return failure("The patch did not reapply to \(head) as the exact snapshot tree.")
        }
        let patchDigest = Self.hex(patchData)
        let manifest: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "method": "git_binary_patch", "worktreeId": action.worktreeID,
            "worktreePath": action.path, "base": head, "tree": tree, "patchSha256": patchDigest,
            "contentDigest": action.contentDigest, "paths": action.dirtyPaths.sorted(),
            "createdAt": Self.rfc3339(ports.now()),
        ]
        guard let manifestData = try? JSONSerialization.data(withJSONObject: manifest,
                                                             options: [.prettyPrinted, .sortedKeys]),
              (try? manifestData.write(to: directory.appendingPathComponent("manifest.json"), options: [.atomic])) != nil else {
            return failure("The preservation manifest could not be written.")
        }
        return .success(["method": "git_binary_patch", "artifact": patchURL.path, "base": head,
                         "digest": action.contentDigest, "tree": tree, "patchSha256": patchDigest, "verified": true])
    }

    // MARK: - Idempotency ledger

    struct LedgerEntry {
        let previewID: String
        let pinDigest: String
        let state: String
        let at: Date
        let receipt: [String: Any]?
    }

    struct Ledger {
        var entries: [String: LedgerEntry]
    }

    var ledgerURL: URL { ports.stateDirectory.appendingPathComponent("cleanup-ledger.json") }

    /// Absent is empty; present and unreadable is a refusal that leaves the file untouched.
    func loadLedger() -> Result<Ledger, Refusal> {
        let unavailable = Refusal(status: 503, code: "cleanup_ledger_unavailable",
            message: "The cleanup ledger exists but cannot be read; its bytes were left in place.")
        guard FileManager.default.fileExists(atPath: ledgerURL.path) else { return .success(Ledger(entries: [:])) }
        guard let data = try? Data(contentsOf: ledgerURL),
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              object["version"] as? Int == 1, let raw = object["entries"] as? [String: Any] else {
            return .failure(unavailable)
        }
        var entries: [String: LedgerEntry] = [:]
        for (key, value) in raw {
            guard let row = value as? [String: Any], let preview = row["previewId"] as? String,
                  let pin = row["pinDigest"] as? String, let state = row["state"] as? String,
                  let at = row["at"] as? Double else { return .failure(unavailable) }
            entries[key] = LedgerEntry(previewID: preview, pinDigest: pin, state: state,
                                       at: Date(timeIntervalSince1970: at), receipt: row["receipt"] as? [String: Any])
        }
        return .success(Ledger(entries: entries))
    }

    func saveLedger(_ ledger: Ledger) -> Bool {
        let now = ports.now()
        let kept = ledger.entries
            .filter { $0.value.state == "started" || now.timeIntervalSince($0.value.at) < Self.ledgerRetention }
            .sorted { $0.value.at > $1.value.at }
            .prefix(Self.ledgerLimit)
        var rows: [String: Any] = [:]
        for (key, entry) in kept {
            rows[key] = ["previewId": entry.previewID, "pinDigest": entry.pinDigest, "state": entry.state,
                         "at": entry.at.timeIntervalSince1970,
                         "receipt": entry.receipt.map { $0 as Any } ?? NSNull()]
        }
        do {
            try FileManager.default.createDirectory(at: ports.stateDirectory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            let data = try JSONSerialization.data(withJSONObject: ["version": 1, "entries": rows],
                                                  options: [.sortedKeys, .withoutEscapingSlashes])
            try data.write(to: ledgerURL, options: [.atomic])
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: ledgerURL.path)
            return true
        } catch {
            return false
        }
    }
}
