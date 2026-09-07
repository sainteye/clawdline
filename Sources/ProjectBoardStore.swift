import CryptoKit
import Foundation

/// Durable, serialized ownership for the Project Board domain.
///
/// The store deliberately has no dependency on the broker, configuration singleton, or usage
/// ledger. Those systems submit trusted facts through `ingest`/`record_evidence`; this owner keeps
/// user-authored descriptions separate from evidence that can advance a lifecycle boundary.
final class ProjectBoardStore {
    struct Reply {
        let status: Int
        let body: [String: Any]
    }

    enum AutomaticMutationStatus: String {
        case accepted
        case unchanged
        case partial
        case refused
        case unavailable
    }

    struct AutomaticMutationOutcome {
        let status: AutomaticMutationStatus
        let acceptedCount: Int
        let droppedCount: Int
        let persisted: Bool
        let reason: String?

        var jsonObject: [String: Any] {
            [
                "status": status.rawValue,
                "acceptedCount": acceptedCount,
                "droppedCount": droppedCount,
                "persisted": persisted,
                "reason": reason ?? NSNull(),
            ]
        }
    }

    static let shared = ProjectBoardStore(url: ProjectBoardStore.defaultURL())

    private static let schemaVersion = 1
    private static let maximumProjects = 200
    private static let maximumItems = 2_000
    private static let maximumReceipts = 4_096
    private static let maximumSnapshotItems = 500
    private static let maximumChildren = 256
    private static let maximumHistory = 2_000
    private static let maximumUTF8 = 4_000
    private static let maximumStoreBytes = 32 * 1_024 * 1_024
    private static let maximumSnapshotBytes = 1_000_000
    private static let maximumSelectedHistory = 128
    private static let maximumSelectedEvidence = 256
    private static let maximumSelectedChildren = 128
    private static let maximumSummaryChildren = 8

    private static let itemTypes = Set([
        "feature", "refactor", "task", "bug", "coordination", "epic",
    ])
    private static let states = Set([
        "backlog", "planning", "ready", "execution", "verified", "integrated", "closed",
        "canceled",
    ])
    private static let phases = Set([
        "planning", "output", "review_testing", "correction", "integration",
    ])
    private static let progressStatuses = Set([
        "todo", "doing", "passed", "failed", "not_applicable",
    ])
    private static let terminalStates = Set(["integrated", "closed", "canceled"])
    private static let artifactKinds = Set([
        "document", "website", "deployment", "commit", "other",
    ])
    private static let linkKinds = Set([
        "session", "task", "worktree", "related", "blocks", "coordinates",
    ])
    private static let itemLinkKinds = Set(["related", "blocks", "coordinates"])

    private struct StoredProject: Codable {
        var id: String
        var name: String
    }

    private struct StoredChecklist: Codable {
        var id: String
        var title: String
        var status: String
        var required: Bool
        var evidenceId: String?
    }

    private struct StoredMilestone: Codable {
        var id: String
        var title: String
        var status: String
    }

    private struct StoredArtifact: Codable {
        var id: String
        var title: String
        var url: String
        var kind: String
    }

    private struct StoredLink: Codable {
        var id: String
        var kind: String
        var targetId: String
        var label: String
        var source: String?
        var phase: String?
        var head: String?
    }

    private struct StoredObligation: Codable {
        var id: String
        var title: String
        var owner: String
        var blocking: Bool
        var resolved: Bool
    }

    private struct StoredHistory: Codable {
        var id: String
        var at: Double
        var actor: String
        var kind: String
        var summary: String
    }

    private struct StoredSpan: Codable {
        var id: String
        var sessionId: String
        var phase: String
        var startedAt: Double
        var endedAt: Double?
        var source: String? = nil
        var sourceId: String? = nil
    }

    private struct StoredHandoff: Codable {
        var id: String
        var fromOwner: String
        var proposedOwner: String
        var note: String
        var proposedAt: Double
    }

    private struct StoredEvidence: Codable {
        var id: String
        var kind: String
        var summary: String
        var subject: String
        var status: String
        var sourceId: String
        var checklistId: String?
        var artifactId: String?
        var blocking: Bool
        var resolved: Bool
        var at: Double
        var actor: String
        var source: String? = nil
        var scopeRevision: Int? = nil
    }

    private struct StoredIngestionCoverage: Codable {
        var kind: String
        var reason: String
        var sourceDigests: [String]
        var saturated: Bool
        var firstObservedAt: Double
    }

    private struct StoredItem: Codable {
        var id: String
        var key: String
        var projectId: String
        var title: String
        var type: String
        /// A String rather than a Codable enum so an imported historical value remains visible.
        var state: String
        var summary: String
        var owner: String
        var parentId: String?
        var createdAt: Double
        var updatedAt: Double
        var checklist: [StoredChecklist]
        var milestones: [StoredMilestone]
        var artifacts: [StoredArtifact]
        var links: [StoredLink]
        var obligations: [StoredObligation]
        var history: [StoredHistory]
        var spans: [StoredSpan]
        var evidence: [StoredEvidence]
        var pendingHandoff: StoredHandoff?
        var currentVerificationEvidenceId: String?
        var currentVerificationSubject: String?
        var currentLandingEvidenceId: String?
        var currentArtifactAcceptanceId: String?
        var historyDroppedCount: Int?
        var scopeRevision: Int? = nil
        var ingestionCoverage: [StoredIngestionCoverage]? = nil
    }

    private struct StoredReceipt: Codable {
        var actor: String
        var requestId: String
        var digest: String
        var status: Int
        var code: String?
        var message: String?
        var itemId: String?
        var revision: Int
    }

    private struct StoredState: Codable {
        var schemaVersion: Int
        var revision: Int
        var enabled: Bool
        var updatedAt: Double
        var projects: [StoredProject]
        var items: [StoredItem]
        var receipts: [StoredReceipt]
        var graphItems: [String: String]
        var receiptEvictions: Int?
        var ingestionCoverage: [StoredIngestionCoverage]? = nil
        var explicitGraphItems: [String: String]? = nil

        static func empty(now: Double) -> StoredState {
            StoredState(schemaVersion: ProjectBoardStore.schemaVersion, revision: 0,
                        enabled: true, updatedAt: now, projects: [], items: [], receipts: [],
                        graphItems: [:], receiptEvictions: 0)
        }
    }

    private struct BoardError: Error {
        let status: Int
        let code: String
        let message: String
    }

    private struct Applied {
        var itemId: String?
    }

    private let url: URL
    private let now: () -> Date
    private let lock = NSLock()
    private var state: StoredState
    private var unavailable: BoardError?
    private var lastAutomaticFailure: AutomaticMutationOutcome?

    init(url: URL, now: @escaping () -> Date = Date.init) {
        self.url = url
        self.now = now
        self.state = .empty(now: now().timeIntervalSince1970)
        load()
    }

    var enabled: Bool {
        lock.lock(); defer { lock.unlock() }
        // A corrupt or future-version store cannot safely authorize new workflow gates.
        return unavailable == nil && state.enabled
    }

    func snapshot(project: String? = nil, item: String? = nil) -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        return snapshotLocked(project: project, item: item)
    }

    @discardableResult
    func ensureProject(id: String, name: String) -> AutomaticMutationOutcome {
        guard let id = Self.boundedText(id, maximum: 200),
              let name = Self.boundedText(name, maximum: 300) else {
            return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                            droppedCount: 1, persisted: true,
                                            reason: "invalid_project")
        }
        lock.lock(); defer { lock.unlock() }
        if let unavailable {
            return AutomaticMutationOutcome(status: .unavailable, acceptedCount: 0,
                                            droppedCount: 1, persisted: false,
                                            reason: unavailable.code)
        }
        var draft = state
        let timestamp = now().timeIntervalSince1970
        if let index = draft.projects.firstIndex(where: { $0.id == id }) {
            guard draft.projects[index].name != name else {
                lastAutomaticFailure = nil
                return AutomaticMutationOutcome(status: .unchanged, acceptedCount: 0,
                                                droppedCount: 0, persisted: true, reason: nil)
            }
            draft.projects[index].name = name
        } else {
            guard draft.projects.count < Self.maximumProjects else {
                let changed = recordIngestionDrop(
                    kind: "project", reason: "project_capacity_reached", sourceID: id,
                    coverage: &draft.ingestionCoverage, timestamp: timestamp)
                if changed {
                    draft.revision += 1
                    draft.updatedAt = timestamp
                    if let persistenceError = persist(draft) {
                        let outcome = AutomaticMutationOutcome(
                            status: persistenceError.status >= 500 ? .unavailable : .refused,
                            acceptedCount: 0, droppedCount: 1, persisted: false,
                            reason: persistenceError.code)
                        lastAutomaticFailure = outcome
                        return outcome
                    }
                    state = draft
                }
                return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                                droppedCount: 1, persisted: true,
                                                reason: "project_capacity_reached")
            }
            draft.projects.append(StoredProject(id: id, name: name))
        }
        draft.revision += 1
        draft.updatedAt = timestamp
        if let persistenceError = persist(draft) {
            let outcome = AutomaticMutationOutcome(
                status: persistenceError.status >= 500 ? .unavailable : .refused,
                acceptedCount: 0,
                                                   droppedCount: 1, persisted: false,
                                                   reason: persistenceError.code)
            lastAutomaticFailure = outcome
            return outcome
        }
        state = draft
        lastAutomaticFailure = nil
        return AutomaticMutationOutcome(status: .accepted, acceptedCount: 1,
                                        droppedCount: 0, persisted: true, reason: nil)
    }

    func command(_ body: [String: Any], actor: String, trusted: Bool = false) -> Reply {
        lock.lock(); defer { lock.unlock() }
        if let unavailable { return Self.errorReply(unavailable) }
        guard let actor = Self.boundedText(actor, maximum: 300) else {
            return Self.errorReply(BoardError(status: 400, code: "invalid_actor",
                                              message: "actor must be a non-empty bounded string"))
        }
        guard JSONSerialization.isValidJSONObject(body),
              let operation = Self.boundedText(body["operation"], maximum: 64),
              let requestId = Self.boundedText(body["requestId"], maximum: 200),
              let expectedRevision = Self.exactInt(body["expectedRevision"]),
              expectedRevision >= 0,
              let digest = Self.digest(body) else {
            return Self.errorReply(BoardError(status: 400, code: "invalid_command",
                                              message: "operation, requestId and expectedRevision are required"))
        }

        if let prior = state.receipts.first(where: {
            $0.actor == actor && $0.requestId == requestId
        }) {
            guard prior.digest == digest else {
                return Self.errorReply(BoardError(status: 409, code: "request_id_conflict",
                                                  message: "requestId was already used with a different command"))
            }
            return replayLocked(prior)
        }
        guard let allowed = Self.allowedKeys(for: operation), Set(body.keys).isSubset(of: allowed) else {
            return rememberErrorLocked(actor: actor, requestId: requestId, digest: digest,
                BoardError(status: 400, code: "invalid_command_fields",
                           message: "the operation contains missing or unknown fields"))
        }
        if operation != "set_enabled" && !state.enabled {
            return rememberErrorLocked(actor: actor, requestId: requestId, digest: digest,
                BoardError(status: 409, code: "board_disabled",
                           message: "Project Board is disabled; ordinary workflow remains available"))
        }
        guard expectedRevision == state.revision else {
            return rememberErrorLocked(actor: actor, requestId: requestId, digest: digest,
                BoardError(status: 409, code: "revision_conflict",
                           message: "expectedRevision does not match the current board revision"))
        }

        var draft = state
        let timestamp = now().timeIntervalSince1970
        do {
            let applied = try apply(operation: operation, body: body, actor: actor,
                                    trusted: trusted, timestamp: timestamp, draft: &draft)
            draft.revision += 1
            draft.updatedAt = timestamp
            appendReceipt(StoredReceipt(
                actor: actor, requestId: requestId, digest: digest, status: 200,
                code: nil, message: nil, itemId: applied.itemId, revision: draft.revision),
                to: &draft)
            if let persistenceError = persist(draft) {
                return Self.errorReply(persistenceError)
            }
            state = draft
            var answer = snapshotLocked(project: nil, item: applied.itemId)
            if let itemId = applied.itemId { answer["itemId"] = itemId }
            return Reply(status: 200, body: answer)
        } catch let error as BoardError {
            return rememberErrorLocked(actor: actor, requestId: requestId, digest: digest, error)
        } catch {
            return rememberErrorLocked(actor: actor, requestId: requestId, digest: digest,
                BoardError(status: 500, code: "board_internal_error",
                           message: "the board command could not be applied"))
        }
    }

    /// Link a trusted broker record to a durable item without equating task success with closure.
    @discardableResult
    func ingest(task: [String: Any], projectID: String) -> AutomaticMutationOutcome {
        lock.lock(); defer { lock.unlock() }
        guard unavailable == nil else {
            return AutomaticMutationOutcome(status: .unavailable, acceptedCount: 0,
                                            droppedCount: 1, persisted: false,
                                            reason: unavailable?.code)
        }
        guard state.enabled else {
            return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                            droppedCount: 1, persisted: true,
                                            reason: "board_disabled")
        }
        guard state.projects.contains(where: { $0.id == projectID }) else {
            return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                            droppedCount: 1, persisted: true,
                                            reason: "project_not_found")
        }
        var draft = state
        let timestamp = now().timeIntervalSince1970
        var changed = false
        var acceptedCount = 0
        var droppedCount = 0
        var droppedReasons: Set<String> = []

        var itemIndex: Int?
        let explicit = Self.boundedText(task["workItemId"] ?? task["work_item_id"], maximum: 200)
        if let explicit {
            itemIndex = draft.items.firstIndex { $0.id == explicit && $0.projectId == projectID }
            guard itemIndex != nil else {
                return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                                droppedCount: 1, persisted: true,
                                                reason: "work_item_not_found")
            }
        }
        let graph = task["graph"] as? [String: Any]
        let graphID = Self.boundedText(graph?["id"], maximum: 200)
        let graphKey = graphID.map { Self.graphKey(projectID: projectID, graphID: $0) }
        if itemIndex == nil, let graphID,
           let existingID = draft.graphItems[Self.graphKey(projectID: projectID,
                                                            graphID: graphID)] {
            itemIndex = draft.items.firstIndex {
                $0.id == existingID && $0.projectId == projectID
            }
        }
        if itemIndex == nil, let graphID,
           let destination = Self.boundedText(graph?["destination"], maximum: 500),
           draft.items.count < Self.maximumItems {
            let item = makeItem(projectID: projectID, title: destination, type: "feature",
                                summary: "", owner: "", parentID: nil, timestamp: timestamp,
                                projects: draft.projects, items: draft.items)
            draft.items.append(item)
            draft.graphItems[Self.graphKey(projectID: projectID, graphID: graphID)] = item.id
            itemIndex = draft.items.count - 1
            changed = true
            acceptedCount += 1
        } else if itemIndex == nil, graphID != nil,
                  Self.boundedText(graph?["destination"], maximum: 500) != nil,
                  draft.items.count >= Self.maximumItems {
            if recordIngestionDrop(kind: "item", reason: "item_capacity_reached",
                                   sourceID: graphID ?? "unknown",
                                   coverage: &draft.ingestionCoverage,
                                   timestamp: timestamp) {
                draft.revision += 1
                draft.updatedAt = timestamp
                if let persistenceError = persist(draft) {
                    let outcome = AutomaticMutationOutcome(
                        status: persistenceError.status >= 500 ? .unavailable : .refused,
                        acceptedCount: 0, droppedCount: 1, persisted: false,
                        reason: persistenceError.code)
                    lastAutomaticFailure = outcome
                    return outcome
                }
                state = draft
            }
            return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                            droppedCount: 1, persisted: true,
                                            reason: "item_capacity_reached")
        }
        if explicit != nil, let graphKey, let itemIndex {
            let selectedID = draft.items[itemIndex].id
            if let established = draft.explicitGraphItems?[graphKey], established != selectedID {
                droppedCount += 1
                droppedReasons.insert("graph_binding_conflict")
                if recordIngestionDrop(kind: "graph", reason: "graph_binding_conflict",
                                       sourceID: selectedID, item: &draft.items[itemIndex],
                                       timestamp: timestamp) { changed = true }
            } else if draft.explicitGraphItems?[graphKey] == nil {
                draft.graphItems[graphKey] = selectedID
                var explicitBindings = draft.explicitGraphItems ?? [:]
                explicitBindings[graphKey] = selectedID
                draft.explicitGraphItems = explicitBindings
                changed = true
                acceptedCount += 1
            }
        }
        guard let selected = itemIndex else {
            return AutomaticMutationOutcome(status: .unchanged, acceptedCount: 0,
                                            droppedCount: 0, persisted: true,
                                            reason: "irrelevant")
        }

        if let taskID = Self.boundedText(task["id"], maximum: 200) {
            let declaredPhase = Self.boundedText(
                task["workPhase"] ?? task["work_phase"], maximum: 64
            ).flatMap { Self.phases.contains($0) ? $0 : nil }
            for index in draft.items.indices
            where index != selected && draft.items[index].projectId == projectID {
                let before = draft.items[index].links.count
                draft.items[index].links.removeAll {
                    $0.kind == "task" && $0.targetId == taskID && $0.source == "broker"
                }
                if draft.items[index].links.count != before {
                    appendHistory(item: &draft.items[index], actor: "broker",
                                  kind: "task_reattributed",
                                  summary: "Execution attempt \(taskID) moved to its explicit item.",
                                  at: timestamp)
                    draft.items[index].updatedAt = timestamp
                    changed = true
                    acceptedCount += 1
                }
            }
            if let existing = draft.items[selected].links.firstIndex(where: {
                $0.kind == "task" && $0.targetId == taskID && $0.source == "broker"
            }) {
                if let declaredPhase,
                   draft.items[selected].links[existing].phase != declaredPhase {
                    draft.items[selected].links[existing].phase = declaredPhase
                    changed = true
                    acceptedCount += 1
                }
            } else if draft.items[selected].links.count < Self.maximumChildren {
                draft.items[selected].links.append(StoredLink(
                    id: Self.newID(), kind: "task", targetId: taskID,
                    label: Self.boundedText(task["title"], maximum: 300) ?? taskID,
                    source: "broker", phase: declaredPhase, head: nil))
                appendHistory(item: &draft.items[selected], actor: "broker", kind: "task_linked",
                              summary: "Linked execution attempt \(taskID).", at: timestamp)
                changed = true
                acceptedCount += 1
            } else {
                droppedCount += 1
                droppedReasons.insert("link_capacity_reached")
                if recordIngestionDrop(kind: "task", reason: "link_capacity_reached",
                                       sourceID: taskID, item: &draft.items[selected],
                                       timestamp: timestamp) { changed = true }
            }
            let evidenceResult = ingestBrokerEvidence(
                task: task, taskID: taskID, graphID: graphID,
                item: &draft.items[selected], timestamp: timestamp)
            if evidenceResult.changed {
                changed = true
            }
            acceptedCount += evidenceResult.acceptedCount
            droppedCount += evidenceResult.droppedCount
            droppedReasons.formUnion(evidenceResult.reasons)
            if evidenceResult.invalidatedCurrentEvidence {
                reopenAncestors(of: draft.items[selected].id, draft: &draft)
            }
        }

        let child = task["child"] as? [String: Any]
        let sessionID = Self.boundedText(child?["sessionId"] ?? child?["session_id"], maximum: 200)
        if let sessionID,
           !draft.items[selected].links.contains(where: {
               $0.kind == "session" && $0.targetId == sessionID
           }), draft.items[selected].links.count < Self.maximumChildren {
            draft.items[selected].links.append(StoredLink(
                id: Self.newID(), kind: "session", targetId: sessionID, label: sessionID,
                source: "broker", phase: nil, head: nil))
            changed = true
            acceptedCount += 1
        } else if let sessionID,
                  !draft.items[selected].links.contains(where: {
                      $0.kind == "session" && $0.targetId == sessionID
                  }) {
            droppedCount += 1
            droppedReasons.insert("link_capacity_reached")
            if recordIngestionDrop(kind: "session", reason: "link_capacity_reached",
                                   sourceID: sessionID, item: &draft.items[selected],
                                   timestamp: timestamp) { changed = true }
        }
        if let worktree = task["worktree"] as? [String: Any],
           let path = Self.boundedText(worktree["path"], maximum: 2_000),
           let branch = Self.boundedText(worktree["branch"], maximum: 300),
           let opaque = Self.stringDigest("\(projectID)\u{0}\(path)") {
            let target = "worktree:\(opaque.prefix(32))"
            let head = Self.boundedText(worktree["head"], maximum: 200)
            if let existing = draft.items[selected].links.firstIndex(where: {
                $0.kind == "worktree" && $0.targetId == target && $0.source == "broker"
            }) {
                if draft.items[selected].links[existing].label != branch
                    || draft.items[selected].links[existing].head != head {
                    draft.items[selected].links[existing].label = branch
                    draft.items[selected].links[existing].head = head
                    changed = true
                    acceptedCount += 1
                }
            } else if draft.items[selected].links.count < Self.maximumChildren {
                draft.items[selected].links.append(StoredLink(
                    id: Self.newID(), kind: "worktree", targetId: target, label: branch,
                    source: "broker", phase: nil, head: head))
                changed = true
                acceptedCount += 1
            } else {
                droppedCount += 1
                droppedReasons.insert("link_capacity_reached")
                if recordIngestionDrop(kind: "worktree", reason: "link_capacity_reached",
                                       sourceID: target, item: &draft.items[selected],
                                       timestamp: timestamp) { changed = true }
            }
        }
        if let phase = Self.boundedText(task["workPhase"] ?? task["work_phase"], maximum: 64),
           Self.phases.contains(phase), let sessionID {
            let taskID = Self.boundedText(task["id"], maximum: 200)
            let existing = taskID.flatMap { source in
                draft.items[selected].spans.firstIndex { $0.sourceId == source }
            }
            let terminal = Set(["success", "failure", "timeout", "cancelled", "spawn_failed"])
                .contains(Self.boundedText(task["state"], maximum: 64) ?? "")
            let explicitStart = Self.canonicalBrokerStart(task)
            let explicitEnd = Self.exactDouble(task["finished_at"] ?? task["finishedAt"])
            let priorStart = existing.map { draft.items[selected].spans[$0].startedAt }
            let priorEnd = existing.flatMap { draft.items[selected].spans[$0].endedAt }
            let provisionalEnd = explicitEnd ?? (terminal ? (priorEnd ?? explicitStart ?? timestamp) : nil)
            let provisionalStart = explicitStart ?? priorStart ?? provisionalEnd ?? timestamp
            let started = provisionalEnd.map { min(provisionalStart, $0) } ?? provisionalStart
            let ended = provisionalEnd
            if let existing {
                if draft.items[selected].spans[existing].phase != phase
                    || draft.items[selected].spans[existing].startedAt != started
                    || draft.items[selected].spans[existing].endedAt != ended {
                    draft.items[selected].spans[existing].phase = phase
                    draft.items[selected].spans[existing].startedAt = started
                    draft.items[selected].spans[existing].endedAt = ended
                    changed = true
                    acceptedCount += 1
                }
            } else {
            if ended == nil {
                closeActiveSpans(sessionID: sessionID, at: started, items: &draft.items)
            }
            if draft.items[selected].spans.count < Self.maximumChildren {
                draft.items[selected].spans.append(StoredSpan(
                    id: Self.newID(), sessionId: sessionID, phase: phase,
                    startedAt: started, endedAt: ended, source: "broker", sourceId: taskID))
                changed = true
                acceptedCount += 1
            } else {
                droppedCount += 1
                droppedReasons.insert("span_capacity_reached")
                if recordIngestionDrop(kind: "span", reason: "span_capacity_reached",
                                       sourceID: taskID ?? sessionID,
                                       item: &draft.items[selected], timestamp: timestamp) {
                    changed = true
                }
            }
            }
        }

        guard changed else {
            lastAutomaticFailure = nil
            let status: AutomaticMutationStatus = droppedCount > 0 ? .refused : .unchanged
            return AutomaticMutationOutcome(status: status, acceptedCount: acceptedCount,
                                            droppedCount: droppedCount, persisted: true,
                                            reason: droppedReasons.sorted().first)
        }
        draft.items[selected].updatedAt = timestamp
        draft.revision += 1
        draft.updatedAt = timestamp
        if let persistenceError = persist(draft) {
            let outcome = AutomaticMutationOutcome(
                status: persistenceError.status >= 500 ? .unavailable : .refused,
                acceptedCount: 0,
                                                   droppedCount: 1, persisted: false,
                                                   reason: persistenceError.code)
            lastAutomaticFailure = outcome
            return outcome
        }
        state = draft
        lastAutomaticFailure = nil
        let status: AutomaticMutationStatus
        if droppedCount > 0 {
            status = acceptedCount > 0 ? .partial : .refused
        } else {
            status = .accepted
        }
        return AutomaticMutationOutcome(status: status, acceptedCount: acceptedCount,
                                        droppedCount: droppedCount, persisted: true,
                                        reason: droppedReasons.sorted().first)
    }

    // MARK: - Commands

    private func apply(operation: String, body: [String: Any], actor: String, trusted: Bool,
                       timestamp: Double, draft: inout StoredState) throws -> Applied {
        switch operation {
        case "set_enabled":
            guard let value = Self.exactBool(body["enabled"]) else {
                throw BoardError(status: 400, code: "invalid_enabled",
                                 message: "enabled must be a Boolean")
            }
            if draft.enabled != value {
                draft.enabled = value
                if !value {
                    for index in draft.items.indices {
                        let open = draft.items[index].spans.indices.filter {
                            draft.items[index].spans[$0].endedAt == nil
                        }
                        guard !open.isEmpty else { continue }
                        for spanIndex in open { draft.items[index].spans[spanIndex].endedAt = timestamp }
                        appendHistory(item: &draft.items[index], actor: actor,
                                      kind: "tracking_suspended",
                                      summary: "Board tracking was suspended; active spans ended at the boundary.",
                                      at: timestamp)
                        draft.items[index].updatedAt = timestamp
                    }
                }
            }
            return Applied(itemId: nil)

        case "create":
            guard draft.items.count < Self.maximumItems else {
                throw BoardError(status: 409, code: "item_capacity_reached",
                                 message: "the board item capacity is exhausted")
            }
            let projectID = try requiredText(body, "projectId", maximum: 200)
            guard draft.projects.contains(where: { $0.id == projectID }) else {
                throw BoardError(status: 404, code: "project_not_found",
                                 message: "projectId does not name a registered Project")
            }
            let title = try requiredText(body, "title", maximum: 300)
            let type = try requiredChoice(body, "type", choices: Self.itemTypes)
            let summary = try optionalText(body, "summary", maximum: Self.maximumUTF8) ?? ""
            let owner = try optionalText(body, "owner", maximum: 300) ?? ""
            let parent = try optionalText(body, "parentId", maximum: 200)
            if let parent { try validateParent(parent, projectID: projectID, draft: draft) }
            let item = makeItem(projectID: projectID, title: title, type: type,
                                summary: summary, owner: owner, parentID: parent,
                                timestamp: timestamp, projects: draft.projects, items: draft.items)
            draft.items.append(item)
            return Applied(itemId: item.id)

        case "update":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            var changed = false
            if body.keys.contains("title") {
                draft.items[index].title = try requiredText(body, "title", maximum: 300)
                changed = true
            }
            if body.keys.contains("summary") {
                draft.items[index].summary = try optionalText(body, "summary",
                                                               maximum: Self.maximumUTF8) ?? ""
                changed = true
            }
            if body.keys.contains("owner") {
                draft.items[index].owner = try optionalText(body, "owner", maximum: 300) ?? ""
                changed = true
            }
            if body.keys.contains("type") {
                let type = try requiredChoice(body, "type", choices: Self.itemTypes)
                if draft.items[index].type == "epic" && type != "epic"
                    && draft.items.contains(where: { $0.parentId == draft.items[index].id }) {
                    throw BoardError(status: 409, code: "container_has_children",
                                     message: "an Epic with children cannot become a non-container type")
                }
                draft.items[index].type = type
                changed = true
            }
            guard changed else {
                throw BoardError(status: 400, code: "empty_update",
                                 message: "update must name at least one mutable field")
            }
            invalidateCurrentEvidence(&draft.items[index], reopen: true)
            touch(&draft.items[index], actor: actor, kind: "item_updated",
                  summary: "Item details were updated.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "transition":
            let index = try itemIndex(body, draft: draft)
            let target = try requiredChoice(body, "state", choices: Self.states)
            let note = try optionalText(body, "note", maximum: 1_000)
            try validateTransition(item: draft.items[index], target: target, allItems: draft.items)
            let old = draft.items[index].state
            draft.items[index].state = target
            if target == "planning" || target == "execution" {
                invalidateCurrentEvidence(&draft.items[index], reopen: false)
            }
            touch(&draft.items[index], actor: actor, kind: "state_transition",
                  summary: note ?? "State changed from \(old) to \(target).", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "checklist":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            if body["checklistId"] != nil || body["status"] != nil {
                guard body["title"] == nil, body["required"] == nil else {
                    throw BoardError(status: 400, code: "invalid_checklist_command",
                                     message: "checklist add and update fields cannot be mixed")
                }
                let checklistID = try requiredText(body, "checklistId", maximum: 200)
                let status = try requiredChoice(body, "status", choices: Self.progressStatuses)
                guard let child = draft.items[index].checklist.firstIndex(where: {
                    $0.id == checklistID
                }) else {
                    throw BoardError(status: 404, code: "checklist_not_found",
                                     message: "checklistId does not name an item checklist row")
                }
                draft.items[index].checklist[child].status = status
            } else {
                guard draft.items[index].checklist.count < Self.maximumChildren else {
                    throw BoardError(status: 409, code: "checklist_capacity_reached",
                                     message: "the checklist capacity is exhausted")
                }
                let title = try requiredText(body, "title", maximum: 500)
                let required = body["required"].map { Self.exactBool($0) } ?? false
                guard let required else {
                    throw BoardError(status: 400, code: "invalid_required",
                                     message: "required must be a Boolean")
                }
                draft.items[index].checklist.append(StoredChecklist(
                    id: Self.newID(), title: title, status: "todo", required: required,
                    evidenceId: nil))
            }
            invalidateCurrentEvidence(&draft.items[index], reopen: true)
            touch(&draft.items[index], actor: actor, kind: "checklist_updated",
                  summary: "Checklist scope or status changed.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "milestone":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            if body["milestoneId"] != nil || body["status"] != nil {
                guard body["title"] == nil else {
                    throw BoardError(status: 400, code: "invalid_milestone_command",
                                     message: "milestone add and update fields cannot be mixed")
                }
                let milestoneID = try requiredText(body, "milestoneId", maximum: 200)
                let status = try requiredChoice(body, "status", choices: Self.progressStatuses)
                guard let child = draft.items[index].milestones.firstIndex(where: {
                    $0.id == milestoneID
                }) else {
                    throw BoardError(status: 404, code: "milestone_not_found",
                                     message: "milestoneId does not name an item milestone")
                }
                draft.items[index].milestones[child].status = status
            } else {
                guard draft.items[index].milestones.count < Self.maximumChildren else {
                    throw BoardError(status: 409, code: "milestone_capacity_reached",
                                     message: "the milestone capacity is exhausted")
                }
                draft.items[index].milestones.append(StoredMilestone(
                    id: Self.newID(), title: try requiredText(body, "title", maximum: 500),
                    status: "todo"))
            }
            invalidateCurrentEvidence(&draft.items[index], reopen: true)
            touch(&draft.items[index], actor: actor, kind: "milestone_updated",
                  summary: "Milestone scope or status changed.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "artifact":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            guard draft.items[index].artifacts.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "artifact_capacity_reached",
                                 message: "the artifact capacity is exhausted")
            }
            let rawURL = try requiredText(body, "url", maximum: 2_000)
            guard let components = URLComponents(string: rawURL),
                  let scheme = components.scheme?.lowercased(),
                  ["http", "https"].contains(scheme), components.host != nil else {
                throw BoardError(status: 400, code: "invalid_artifact_url",
                                 message: "artifact url must use http or https")
            }
            draft.items[index].artifacts.append(StoredArtifact(
                id: Self.newID(), title: try requiredText(body, "title", maximum: 300),
                url: rawURL, kind: try requiredChoice(body, "kind", choices: Self.artifactKinds)))
            invalidateCurrentEvidence(&draft.items[index], reopen: true)
            touch(&draft.items[index], actor: actor, kind: "artifact_added",
                  summary: "An artifact reference was added; it is not verification by itself.",
                  at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "link":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            guard draft.items[index].links.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "link_capacity_reached",
                                 message: "the link capacity is exhausted")
            }
            let kind = try requiredChoice(body, "kind", choices: Self.linkKinds)
            let target = try requiredText(body, "targetId", maximum: 300)
            if Self.itemLinkKinds.contains(kind) {
                guard target != draft.items[index].id,
                      draft.items.contains(where: { $0.id == target }) else {
                    throw BoardError(status: 400, code: "invalid_item_relation",
                                     message: "item relations require another existing item")
                }
                guard !wouldCreateRelationCycle(source: draft.items[index].id,
                                                target: target, items: draft.items) else {
                    throw BoardError(status: 409, code: "relation_cycle",
                                     message: "the item relation would create a cycle")
                }
            }
            draft.items[index].links.append(StoredLink(
                id: Self.newID(), kind: kind, targetId: target,
                label: try requiredText(body, "label", maximum: 300),
                source: "user", phase: nil, head: nil))
            touch(&draft.items[index], actor: actor, kind: "link_added",
                  summary: "A \(kind) relation was added.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "obligation":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            guard draft.items[index].obligations.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "obligation_capacity_reached",
                                 message: "the obligation capacity is exhausted")
            }
            guard let blocking = Self.exactBool(body["blocking"]) else {
                throw BoardError(status: 400, code: "invalid_blocking",
                                 message: "blocking must be a Boolean")
            }
            draft.items[index].obligations.append(StoredObligation(
                id: Self.newID(), title: try requiredText(body, "title", maximum: 500),
                owner: try requiredText(body, "owner", maximum: 300),
                blocking: blocking, resolved: false))
            touch(&draft.items[index], actor: actor, kind: "obligation_added",
                  summary: "A durable obligation was recorded.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "resolve_obligation":
            let index = try itemIndex(body, draft: draft)
            let obligationID = try requiredText(body, "obligationId", maximum: 200)
            let note = try requiredText(body, "note", maximum: 1_000)
            guard let child = draft.items[index].obligations.firstIndex(where: {
                $0.id == obligationID
            }) else {
                throw BoardError(status: 404, code: "obligation_not_found",
                                 message: "obligationId does not name an item obligation")
            }
            draft.items[index].obligations[child].resolved = true
            touch(&draft.items[index], actor: actor, kind: "obligation_resolved",
                  summary: note, at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "span":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            guard draft.items[index].spans.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "span_capacity_reached",
                                 message: "the execution span capacity is exhausted")
            }
            let sessionID = try requiredText(body, "sessionId", maximum: 300)
            let phase = try requiredChoice(body, "phase", choices: Self.phases)
            closeActiveSpans(sessionID: sessionID, at: timestamp, items: &draft.items)
            draft.items[index].spans.append(StoredSpan(
                id: Self.newID(), sessionId: sessionID, phase: phase,
                startedAt: timestamp, endedAt: nil))
            touch(&draft.items[index], actor: actor, kind: "span_started",
                  summary: "Session \(sessionID) declared phase \(phase).", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "handoff":
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            guard draft.items[index].pendingHandoff == nil else {
                throw BoardError(status: 409, code: "handoff_pending",
                                 message: "the item already has a pending handoff")
            }
            let proposed = try requiredText(body, "owner", maximum: 300)
            guard proposed != draft.items[index].owner else {
                throw BoardError(status: 409, code: "handoff_same_owner",
                                 message: "the proposed receiver already owns the item")
            }
            let note = try requiredText(body, "note", maximum: 1_000)
            draft.items[index].pendingHandoff = StoredHandoff(
                id: Self.newID(), fromOwner: draft.items[index].owner,
                proposedOwner: proposed, note: note, proposedAt: timestamp)
            touch(&draft.items[index], actor: actor, kind: "handoff_proposed",
                  summary: note, at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "accept_handoff":
            let index = try itemIndex(body, draft: draft)
            let note = try requiredText(body, "note", maximum: 1_000)
            guard let handoff = draft.items[index].pendingHandoff else {
                throw BoardError(status: 409, code: "handoff_not_pending",
                                 message: "the item has no pending handoff")
            }
            guard actor == handoff.proposedOwner else {
                throw BoardError(status: 403, code: "handoff_receiver_required",
                                 message: "only the proposed receiver can accept the handoff")
            }
            draft.items[index].owner = handoff.proposedOwner
            draft.items[index].pendingHandoff = nil
            touch(&draft.items[index], actor: actor, kind: "handoff_accepted",
                  summary: note, at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "accept_artifact":
            guard trusted else { throw Self.trustedEvidenceError() }
            let index = try itemIndex(body, draft: draft)
            try requireMutable(index, draft: draft)
            let artifactID = try requiredText(body, "artifactId", maximum: 200)
            guard draft.items[index].artifacts.contains(where: { $0.id == artifactID }) else {
                throw BoardError(status: 404, code: "artifact_not_found",
                                 message: "artifactId does not name an item artifact")
            }
            let note = try optionalText(body, "note", maximum: 1_000) ?? "Artifact accepted."
            let scopeRevision = draft.items[index].scopeRevision ?? 0
            let sourceID = "artifact:\(artifactID):scope:\(scopeRevision)"
            guard !draft.items[index].evidence.contains(where: {
                $0.kind == "artifact_acceptance" && $0.sourceId == sourceID
            }) else {
                throw BoardError(status: 409, code: "evidence_already_recorded",
                                 message: "that artifact already has an acceptance record")
            }
            try requireEvidenceCapacity(draft.items[index])
            let evidenceID = Self.newID()
            draft.items[index].evidence.append(StoredEvidence(
                id: evidenceID, kind: "artifact_acceptance", summary: note,
                subject: artifactID, status: "passed", sourceId: sourceID,
                checklistId: nil, artifactId: artifactID, blocking: false, resolved: true,
                at: timestamp, actor: actor, source: "root_attestation",
                scopeRevision: scopeRevision))
            if usesArtifactAcceptance(draft.items[index]) {
                draft.items[index].currentArtifactAcceptanceId = evidenceID
                draft.items[index].currentVerificationSubject = artifactID
                for child in draft.items[index].checklist.indices
                where draft.items[index].checklist[child].required
                    && draft.items[index].checklist[child].status == "passed" {
                    draft.items[index].checklist[child].evidenceId = evidenceID
                }
            }
            touch(&draft.items[index], actor: actor, kind: "artifact_accepted",
                  summary: note, at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "record_evidence":
            guard trusted else { throw Self.trustedEvidenceError() }
            let index = try itemIndex(body, draft: draft)
            let kind = try requiredChoice(body, "kind",
                                          choices: Set(["verification", "finding"]))
            let statusChoices = kind == "finding" ? Set(["open", "resolved"])
                                                    : Set(["passed", "failed"])
            let status = try requiredChoice(body, "status", choices: statusChoices)
            let sourceID = try requiredText(body, "sourceId", maximum: 300)
            let existingEvidence = draft.items[index].evidence.firstIndex(where: {
                $0.kind == kind && $0.sourceId == sourceID
            })
            let summary = try requiredText(body, "summary", maximum: 1_000)
            let subject = try requiredText(body, "subject", maximum: 500)
            let checklistID = try optionalText(body, "checklistId", maximum: 200)
            if let checklistID,
               !draft.items[index].checklist.contains(where: { $0.id == checklistID }) {
                throw BoardError(status: 404, code: "checklist_not_found",
                                 message: "checklistId does not name an item checklist row")
            }
            let blocking = body["blocking"].map { Self.exactBool($0) } ?? false
            let resolved = body["resolved"].map { Self.exactBool($0) } ?? (status == "resolved")
            guard let blocking, let resolved else {
                throw BoardError(status: 400, code: "invalid_evidence_flags",
                                 message: "blocking and resolved must be Boolean values")
            }
            if let existingEvidence {
                if kind == "finding", status == "resolved",
                   !draft.items[index].evidence[existingEvidence].resolved {
                    draft.items[index].evidence[existingEvidence].resolved = true
                    draft.items[index].evidence[existingEvidence].status = "resolved"
                    touch(&draft.items[index], actor: actor, kind: "finding_resolved",
                          summary: summary, at: timestamp)
                    return Applied(itemId: draft.items[index].id)
                }
                throw BoardError(status: 409, code: "evidence_already_recorded",
                                 message: "sourceId already identifies evidence of this kind")
            }
            try requireEvidenceCapacity(draft.items[index])
            let subjectChanged = draft.items[index].currentVerificationSubject != subject
            if kind == "verification", status == "passed", subjectChanged,
               Self.terminalStates.contains(draft.items[index].state) {
                reopenForInvalidatedEvidence(index, draft: &draft)
            }
            let scopeRevision = draft.items[index].scopeRevision ?? 0
            let evidenceID = Self.newID()
            draft.items[index].evidence.append(StoredEvidence(
                id: evidenceID, kind: kind,
                summary: summary, subject: subject, status: status,
                sourceId: sourceID, checklistId: checklistID, artifactId: nil,
                blocking: kind == "finding" && blocking,
                resolved: kind == "finding" ? (resolved || status == "resolved") : true,
                at: timestamp, actor: actor, source: "root_attestation",
                scopeRevision: scopeRevision))
            if kind == "verification" {
                if status == "passed" {
                    if !usesArtifactAcceptance(draft.items[index]) {
                        draft.items[index].currentVerificationEvidenceId = evidenceID
                        draft.items[index].currentVerificationSubject = subject
                        if subjectChanged { draft.items[index].currentLandingEvidenceId = nil }
                        draft.items[index].currentArtifactAcceptanceId = nil
                    }
                } else {
                    invalidateCurrentEvidence(&draft.items[index], reopen: true)
                    reopenAncestors(of: draft.items[index].id, draft: &draft)
                }
            } else if blocking && !(resolved || status == "resolved") {
                invalidateCurrentEvidence(&draft.items[index], reopen: true)
                reopenAncestors(of: draft.items[index].id, draft: &draft)
            }
            if kind == "verification", status == "passed", let checklistID,
               let child = draft.items[index].checklist.firstIndex(where: {
                   $0.id == checklistID
               }) {
                draft.items[index].checklist[child].evidenceId = evidenceID
            }
            touch(&draft.items[index], actor: actor, kind: "trusted_evidence_recorded",
                  summary: "Trusted \(kind) evidence was recorded for the named subject.",
                  at: timestamp)
            return Applied(itemId: draft.items[index].id)

        default:
            throw BoardError(status: 400, code: "unknown_operation",
                             message: "operation is not supported")
        }
    }

    // MARK: - Lifecycle policy

    private func validateTransition(item: StoredItem, target: String,
                                    allItems: [StoredItem]) throws {
        if item.state == target { return }
        if !Self.terminalStates.contains(target), let parentID = item.parentId,
           let parent = allItems.first(where: { $0.id == parentID }),
           Self.terminalStates.contains(parent.state) {
            throw BoardError(status: 409, code: "ancestor_locked",
                             message: "reopen the parent Epic before reopening its child")
        }
        if target == "canceled" && item.state != "closed" { return }
        if (item.state == "closed" || item.state == "canceled") && target == "planning" { return }
        let next: [String: Set<String>] = [
            "backlog": ["planning", "ready"],
            "planning": ["backlog", "ready"],
            "ready": ["planning", "execution"],
            "execution": ["ready", "verified"],
            "verified": ["execution", "integrated"],
            "integrated": ["execution", "closed"],
        ]
        // Unknown imported states remain visible and can only be explicitly reconciled.
        if !Self.states.contains(item.state) && ["planning", "canceled"].contains(target) { return }
        guard next[item.state]?.contains(target) == true else {
            throw BoardError(status: 409, code: "invalid_transition",
                             message: "the requested lifecycle transition is not allowed")
        }
        if ["ready", "execution", "verified", "integrated", "closed"].contains(target) {
            guard !item.owner.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                throw BoardError(status: 409, code: "owner_required",
                                 message: "an owner is required beyond planning")
            }
        }
        if target == "verified" || target == "integrated" || target == "closed" {
            try validateCurrentChecklistEvidence(item)
            guard !item.evidence.contains(where: {
                $0.kind == "finding" && $0.blocking && !$0.resolved
            }) else {
                throw BoardError(status: 409, code: "blocking_findings",
                                 message: "open blocking findings prevent lifecycle advancement")
            }
        }
        if target == "verified" {
            guard hasVerificationEvidence(item) else {
                throw BoardError(status: 409, code: "evidence_required",
                                 message: "trusted verification or artifact acceptance is required")
            }
        }
        if target == "integrated" {
            guard hasVerificationEvidence(item), hasDeliveryEvidence(item) else {
                throw BoardError(status: 409, code: "evidence_required",
                                 message: "coherent current verification and delivery evidence are required")
            }
            if item.type == "epic" {
                let children = allItems.filter { $0.parentId == item.id }
                guard children.allSatisfy({ ["integrated", "closed"].contains($0.state) }) else {
                    throw BoardError(status: 409, code: "children_incomplete",
                                     message: "all Epic children must reach a terminal delivery state")
                }
            }
        }
        if target == "closed" {
            guard hasVerificationEvidence(item), hasDeliveryEvidence(item) else {
                throw BoardError(status: 409, code: "evidence_required",
                                 message: "closure requires coherent current verification and delivery evidence")
            }
            guard item.obligations.filter({ $0.blocking }).allSatisfy(\.resolved),
                  item.milestones.allSatisfy({
                      $0.status == "passed" || $0.status == "not_applicable"
                  }), item.pendingHandoff == nil else {
                throw BoardError(status: 409, code: "closure_obligations_open",
                                 message: "blocking obligations, milestones, and handoff must be settled")
            }
        }
    }

    private func hasVerificationEvidence(_ item: StoredItem) -> Bool {
        let scopeRevision = item.scopeRevision ?? 0
        if usesArtifactAcceptance(item) {
            guard let current = item.currentArtifactAcceptanceId,
                  let subject = item.currentVerificationSubject else { return false }
            return item.evidence.contains {
                $0.id == current && $0.kind == "artifact_acceptance" && $0.status == "passed"
                    && $0.subject == subject && ($0.scopeRevision ?? 0) == scopeRevision
            }
        }
        guard let current = item.currentVerificationEvidenceId,
              let subject = item.currentVerificationSubject else { return false }
        return item.evidence.contains {
            $0.id == current && $0.kind == "verification" && $0.status == "passed"
                && $0.subject == subject && ($0.scopeRevision ?? 0) == scopeRevision
        }
    }

    private func hasDeliveryEvidence(_ item: StoredItem) -> Bool {
        if usesArtifactAcceptance(item) {
            return hasVerificationEvidence(item)
        }
        if item.type == "coordination" {
            return item.pendingHandoff == nil && hasVerificationEvidence(item)
        }
        guard let current = item.currentLandingEvidenceId else { return false }
        return item.evidence.contains {
            $0.id == current && $0.kind == "landing" && $0.status == "passed"
                && $0.subject == item.currentVerificationSubject
                && ($0.scopeRevision ?? 0) == (item.scopeRevision ?? 0)
        }
    }

    private func usesArtifactAcceptance(_ item: StoredItem) -> Bool {
        item.type == "task" && !item.artifacts.isEmpty
            && !item.artifacts.contains(where: { $0.kind == "commit" })
    }

    private func validateCurrentChecklistEvidence(_ item: StoredItem) throws {
        let required = item.checklist.filter(\.required)
        guard required.allSatisfy({ $0.status == "passed" && $0.evidenceId != nil }) else {
            throw BoardError(status: 409, code: "evidence_required",
                             message: "required checklist rows need passed status and trusted evidence")
        }
        guard !required.isEmpty else { return }
        let scopeRevision = item.scopeRevision ?? 0
        guard let subject = item.currentVerificationSubject else {
            throw BoardError(status: 409, code: "evidence_required",
                             message: "required checklist rows need one current evidence subject")
        }
        let coherent = required.allSatisfy { row in
            item.evidence.contains { evidence in
                evidence.id == row.evidenceId && evidence.status == "passed"
                    && evidence.subject == subject
                    && (evidence.scopeRevision ?? 0) == scopeRevision
                    && (evidence.kind == "verification" || evidence.kind == "artifact_acceptance")
            }
        }
        guard coherent else {
            throw BoardError(status: 409, code: "evidence_subject_mismatch",
                             message: "all required checklist rows must attest one current subject and scope revision")
        }
    }

    // MARK: - Snapshot

    private func snapshotLocked(project: String?, item selectedID: String?) -> [String: Any] {
        if let unavailable {
            return ["board": [
                "schemaVersion": Self.schemaVersion, "revision": 0, "enabled": false,
                "mode": "standard", "entitlement": Self.entitlement,
                "projects": [], "items": [], "item": NSNull(), "truncated": false,
                "updatedAt": 0.0, "available": false,
                "error": ["code": unavailable.code, "message": unavailable.message],
            ] as [String: Any]]
        }
        var matching = state.items
        if let project { matching = matching.filter { $0.projectId == project } }
        matching.sort { lhs, rhs in
            lhs.updatedAt == rhs.updatedAt ? lhs.key < rhs.key : lhs.updatedAt > rhs.updatedAt
        }
        let selected = selectedID.flatMap { id in
            state.items.first(where: { candidate in
                candidate.id == id && (project == nil || candidate.projectId == project)
            })
        }
        let projects = state.projects.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            .map { project in
                ["id": project.id, "name": project.name,
                 "itemCount": state.items.filter { $0.projectId == project.id }.count] as [String: Any]
            }
        let selectedObject = selected.map { itemObject($0, detail: true) }
        var board: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "revision": state.revision,
            "enabled": state.enabled, "mode": state.enabled ? "board" : "standard",
            "entitlement": Self.entitlement, "projects": projects, "items": [],
            "item": selectedObject ?? NSNull(), "truncated": false,
            "updatedAt": state.updatedAt, "available": true,
            "snapshotBudgetBytes": Self.maximumSnapshotBytes,
            "truncation": [
                "reason": NSNull(), "itemsOmittedCount": 0,
            ] as [String: Any],
            "idempotencyRetention": [
                "maximumReceipts": Self.maximumReceipts,
                "retainedReceipts": state.receipts.count,
                "evictedReceipts": state.receiptEvictions ?? 0,
                "oldestRetainedRevision": state.receipts.first?.revision ?? state.revision,
            ] as [String: Any],
        ]
        if let failure = lastAutomaticFailure {
            board["automaticMutation"] = failure.jsonObject
        } else {
            board["automaticMutation"] = [
                "status": "available", "persisted": true, "reason": NSNull(),
            ] as [String: Any]
        }
        let boardCoverage = state.ingestionCoverage ?? []
        board["sourceIngestion"] = [
            "status": boardCoverage.isEmpty ? "complete" : "partial",
            "droppedCount": boardCoverage.reduce(0) { $0 + $1.sourceDigests.count },
            "reasons": Array(Set(boardCoverage.map(\.reason))).sorted(),
            "issues": boardCoverage.map {
                ["kind": $0.kind, "reason": $0.reason,
                 "droppedCount": $0.sourceDigests.count, "saturated": $0.saturated,
                 "firstObservedAt": $0.firstObservedAt] as [String: Any]
            },
        ] as [String: Any]
        guard Self.serializedSize(["board": board]) <= Self.maximumSnapshotBytes else {
            return snapshotTooLargeLocked(reason: "selected_item_exceeds_snapshot_budget")
        }

        var visible: [[String: Any]] = []
        var byteLimited = false
        for candidate in matching.prefix(Self.maximumSnapshotItems) {
            var trialBoard = board
            let candidateObject = itemObject(candidate, detail: false)
            trialBoard["items"] = visible + [candidateObject]
            if Self.serializedSize(["board": trialBoard]) > Self.maximumSnapshotBytes {
                byteLimited = true
                break
            }
            visible.append(candidateObject)
        }
        let omitted = matching.count - visible.count
        board["items"] = visible
        board["truncated"] = omitted > 0
        board["truncation"] = [
            "reason": omitted == 0 ? NSNull()
                : (byteLimited ? "snapshot_byte_budget" : "item_count_limit"),
            "itemsOmittedCount": omitted,
        ] as [String: Any]
        while !visible.isEmpty,
              Self.serializedSize(["board": board]) > Self.maximumSnapshotBytes {
            visible.removeLast()
            board["items"] = visible
            board["truncated"] = true
            board["truncation"] = [
                "reason": "snapshot_byte_budget",
                "itemsOmittedCount": matching.count - visible.count,
            ] as [String: Any]
        }
        guard Self.serializedSize(["board": board]) <= Self.maximumSnapshotBytes else {
            return snapshotTooLargeLocked(reason: "snapshot_metadata_exceeds_budget")
        }
        return ["board": board]
    }

    private static let entitlement: [String: Any] = [
        "state": "free_preview", "label": "Currently free",
    ]

    private func itemObject(_ item: StoredItem, detail: Bool) -> [String: Any] {
        let childLimit = detail ? Self.maximumSelectedChildren : Self.maximumSummaryChildren
        let historyLimit = detail ? Self.maximumSelectedHistory : Self.maximumSummaryChildren
        let currentIDs = Set([
            item.currentVerificationEvidenceId, item.currentLandingEvidenceId,
            item.currentArtifactAcceptanceId,
        ].compactMap { $0 } + item.checklist.compactMap(\.evidenceId))
        let mandatoryEvidence = item.evidence.filter {
            currentIDs.contains($0.id) || ($0.kind == "finding" && $0.blocking && !$0.resolved)
        }
        var retainedEvidence = mandatoryEvidence
        let evidenceLimit = detail ? Self.maximumSelectedEvidence : Self.maximumSummaryChildren
        if retainedEvidence.count < evidenceLimit {
            let retainedIDs = Set(retainedEvidence.map(\.id))
            for row in item.evidence.reversed()
            where retainedEvidence.count < evidenceLimit && !retainedIDs.contains(row.id) {
                retainedEvidence.append(row)
            }
        }
        retainedEvidence.sort { $0.at == $1.at ? $0.id < $1.id : $0.at < $1.at }
        let grouped = Dictionary(grouping: retainedEvidence, by: \.kind)
        let retainedHistory = Array(item.history.suffix(historyLimit))
        let retainedChecklist = Array(item.checklist.prefix(childLimit))
        let retainedMilestones = Array(item.milestones.prefix(childLimit))
        let retainedArtifacts = Array(item.artifacts.prefix(childLimit))
        let retainedLinks = Array(item.links.prefix(childLimit))
        let retainedObligations = Array(item.obligations.prefix(childLimit))
        let retainedSpans = Array(item.spans.suffix(childLimit))
        let accountingLinks = item.links.filter { $0.kind == "task" && $0.source == "broker" }
        let coverage = item.ingestionCoverage ?? []
        var answer: [String: Any] = [
            "id": item.id, "key": item.key, "projectId": item.projectId,
            "title": item.title, "type": item.type, "state": item.state,
            "summary": item.summary, "owner": item.owner,
            "parentId": item.parentId ?? NSNull(), "createdAt": item.createdAt,
            "updatedAt": item.updatedAt,
            "scopeRevision": item.scopeRevision ?? 0,
            "checklist": retainedChecklist.map { row in
                var value: [String: Any] = ["id": row.id, "title": row.title,
                                                "status": row.status, "required": row.required]
                if let evidence = row.evidenceId { value["evidenceId"] = evidence }
                return value
            },
            "milestones": retainedMilestones.map {
                ["id": $0.id, "title": $0.title, "status": $0.status]
            },
            "artifacts": retainedArtifacts.map {
                ["id": $0.id, "title": $0.title, "url": $0.url, "kind": $0.kind]
            },
            "links": retainedLinks.map { link in
                var value: [String: Any] = [
                    "id": link.id, "kind": link.kind, "targetId": link.targetId,
                    "label": link.label, "source": link.source ?? "unknown",
                ]
                if let phase = link.phase { value["phase"] = phase }
                if let head = link.head { value["head"] = head }
                return value
            },
            "accountingTaskLinks": accountingLinks.map { link in
                var value: [String: Any] = [
                    "targetId": link.targetId, "source": "broker",
                ]
                value["phase"] = link.phase ?? NSNull()
                return value
            },
            "obligations": retainedObligations.map {
                ["id": $0.id, "title": $0.title, "owner": $0.owner,
                 "blocking": $0.blocking, "resolved": $0.resolved]
            },
            "history": retainedHistory.map {
                ["id": $0.id, "at": $0.at, "actor": $0.actor,
                 "kind": $0.kind, "summary": $0.summary]
            },
            "historyTruncated": (item.historyDroppedCount ?? 0) > 0
                || retainedHistory.count < item.history.count,
            "historyDroppedCount": (item.historyDroppedCount ?? 0)
                + max(0, item.history.count - retainedHistory.count),
            "spans": retainedSpans.map { span in
                var value: [String: Any] = [
                    "id": span.id, "sessionId": span.sessionId, "phase": span.phase,
                    "startedAt": span.startedAt, "source": span.source ?? "user",
                ]
                value["endedAt"] = span.endedAt ?? NSNull()
                if let sourceID = span.sourceId { value["sourceId"] = sourceID }
                return value
            },
            "findings": (grouped["finding"] ?? []).map(evidenceObject),
            "verifications": (grouped["verification"] ?? []).map(evidenceObject),
            "landings": (grouped["landing"] ?? []).map(evidenceObject),
            "artifactAcceptances": (grouped["artifact_acceptance"] ?? []).map(evidenceObject),
            "evidenceSummaries": (grouped["verification_summary"] ?? []).map(evidenceObject),
            "currentEvidence": [
                "verificationId": item.currentVerificationEvidenceId ?? NSNull(),
                "subject": item.currentVerificationSubject ?? NSNull(),
                "landingId": item.currentLandingEvidenceId ?? NSNull(),
                "artifactAcceptanceId": item.currentArtifactAcceptanceId ?? NSNull(),
                "scopeRevision": item.scopeRevision ?? 0,
            ] as [String: Any],
            "sourceIngestion": [
                "status": coverage.isEmpty ? "complete" : "partial",
                "droppedCount": coverage.reduce(0) { $0 + $1.sourceDigests.count },
                "reasons": Array(Set(coverage.map(\.reason))).sorted(),
                "issues": coverage.map {
                    ["kind": $0.kind, "reason": $0.reason,
                     "droppedCount": $0.sourceDigests.count,
                     "saturated": $0.saturated,
                     "firstObservedAt": $0.firstObservedAt] as [String: Any]
                },
            ] as [String: Any],
            "projection": [
                "checklist": Self.projectionObject(total: item.checklist.count,
                                                    retained: retainedChecklist.count),
                "milestones": Self.projectionObject(total: item.milestones.count,
                                                     retained: retainedMilestones.count),
                "artifacts": Self.projectionObject(total: item.artifacts.count,
                                                    retained: retainedArtifacts.count),
                "links": Self.projectionObject(total: item.links.count,
                                                retained: retainedLinks.count),
                "obligations": Self.projectionObject(total: item.obligations.count,
                                                      retained: retainedObligations.count),
                "spans": Self.projectionObject(total: item.spans.count,
                                                retained: retainedSpans.count),
                "history": Self.projectionObject(total: item.history.count,
                                                  retained: retainedHistory.count),
                "evidence": Self.projectionObject(total: item.evidence.count,
                                                   retained: retainedEvidence.count),
            ] as [String: Any],
        ]
        if let handoff = item.pendingHandoff {
            answer["handoff"] = [
                "id": handoff.id, "fromOwner": handoff.fromOwner,
                "proposedOwner": handoff.proposedOwner, "note": handoff.note,
                "proposedAt": handoff.proposedAt,
            ] as [String: Any]
        } else {
            answer["handoff"] = NSNull()
        }
        return answer
    }

    private func evidenceObject(_ row: StoredEvidence) -> [String: Any] {
        var answer: [String: Any] = [
            "id": row.id, "kind": row.kind, "summary": row.summary,
            "subject": row.subject, "status": row.status, "sourceId": row.sourceId,
            "blocking": row.blocking, "resolved": row.resolved,
            "at": row.at, "actor": row.actor, "source": row.source ?? "unknown",
        ]
        if let checklist = row.checklistId { answer["checklistId"] = checklist }
        if let artifact = row.artifactId { answer["artifactId"] = artifact }
        answer["scopeRevision"] = row.scopeRevision ?? 0
        return answer
    }

    private static func projectionObject(total: Int, retained: Int) -> [String: Any] {
        let omitted = max(0, total - retained)
        return [
            "retainedCount": retained, "omittedCount": omitted,
            "reason": omitted == 0 ? NSNull() : "snapshot_detail_limit",
        ]
    }

    private func snapshotTooLargeLocked(reason: String) -> [String: Any] {
        ["board": [
            "schemaVersion": Self.schemaVersion, "revision": state.revision,
            "enabled": false, "mode": "standard", "entitlement": Self.entitlement,
            "projects": [], "items": [], "item": NSNull(), "truncated": true,
            "updatedAt": state.updatedAt, "available": false,
            "snapshotBudgetBytes": Self.maximumSnapshotBytes,
            "error": ["code": "board_snapshot_too_large", "message": reason],
        ] as [String: Any]]
    }

    private static func serializedSize(_ object: [String: Any]) -> Int {
        (try? JSONSerialization.data(withJSONObject: object).count) ?? Int.max
    }

    // MARK: - Trusted broker ingestion

    private struct BrokerEvidenceResult {
        var changed = false
        var acceptedCount = 0
        var droppedCount = 0
        var reasons: Set<String> = []
        var invalidatedCurrentEvidence = false
    }

    private func ingestBrokerEvidence(task: [String: Any], taskID: String, graphID: String?,
                                      item: inout StoredItem,
                                      timestamp: Double) -> BrokerEvidenceResult {
        var result = BrokerEvidenceResult()
        if let review = task["review"] as? [String: Any],
           let axes = review["axes"] as? [[String: Any]] {
            for axis in axes {
                for finding in axis["findings"] as? [[String: Any]] ?? [] {
                    guard let findingID = Self.boundedText(finding["id"], maximum: 200),
                          let summary = Self.boundedText(finding["summary"], maximum: 1_000)
                    else { continue }
                    let source = "task:\(taskID):finding:\(findingID)"
                    guard !item.evidence.contains(where: {
                        $0.kind == "finding" && $0.sourceId == source
                    }) else { continue }
                    guard item.evidence.count < Self.maximumHistory else {
                        result.droppedCount += 1
                        result.reasons.insert("evidence_capacity_reached")
                        if recordIngestionDrop(kind: "evidence", reason: "evidence_capacity_reached",
                                               sourceID: source, item: &item,
                                               timestamp: timestamp) { result.changed = true }
                        continue
                    }
                    let severity = Self.boundedText(finding["severity"], maximum: 64) ?? "blocking"
                    item.evidence.append(StoredEvidence(
                        id: Self.newID(), kind: "finding", summary: summary,
                        subject: graphID ?? taskID, status: "open", sourceId: source,
                        checklistId: nil, artifactId: nil, blocking: severity == "blocking",
                        resolved: false, at: timestamp, actor: "broker", source: "broker"))
                    if severity == "blocking" {
                        invalidateCurrentEvidence(&item, reopen: true)
                        result.invalidatedCurrentEvidence = true
                    }
                    result.changed = true
                    result.acceptedCount += 1
                }
            }
        }
        if let verification = task["verification"] as? [String: Any], !verification.isEmpty {
            let sourceID = "task:\(taskID):verification-summary"
            if !item.evidence.contains(where: {
                $0.kind == "verification_summary" && $0.sourceId == sourceID
            }) {
                guard item.evidence.count < Self.maximumHistory else {
                    result.droppedCount += 1
                    result.reasons.insert("evidence_capacity_reached")
                    if recordIngestionDrop(kind: "evidence", reason: "evidence_capacity_reached",
                                           sourceID: sourceID, item: &item,
                                           timestamp: timestamp) { result.changed = true }
                    return result
                }
                item.evidence.append(StoredEvidence(
                    id: Self.newID(), kind: "verification_summary",
                    summary: "Execution attempt carries a verification summary; it is not exact-subject proof.",
                    subject: taskID, status: "reported", sourceId: sourceID,
                    checklistId: nil, artifactId: nil, blocking: false, resolved: true,
                    at: timestamp, actor: "broker", source: "broker"))
                appendHistory(item: &item, actor: "broker", kind: "verification_summary_linked",
                              summary: "Execution attempt \(taskID) carries a verification summary; it is not exact-subject proof.",
                              at: timestamp)
                result.changed = true
                result.acceptedCount += 1
            }
        }
        if let landing = task["landing"] as? [String: Any],
           landing["state"] as? String == "landed",
           let commit = Self.boundedText(landing["verified_commit"]
                                         ?? landing["verifiedCommit"], maximum: 200),
           let targetCommit = Self.boundedText(landing["verified_target_commit"]
                                               ?? landing["verifiedTargetCommit"], maximum: 200),
           Self.boundedText(landing["verification_origin"]
                            ?? landing["verificationOrigin"], maximum: 200) != nil {
            let scopeRevision = item.scopeRevision ?? 0
            let source = "task:\(taskID):landing:\(commit):\(targetCommit):scope:\(scopeRevision)"
            if let existing = item.evidence.first(where: {
                $0.kind == "landing" && $0.sourceId == source
            }) {
                if item.currentVerificationSubject == commit
                    && item.currentLandingEvidenceId != existing.id {
                    item.currentLandingEvidenceId = existing.id
                    result.changed = true
                    result.acceptedCount += 1
                }
            } else {
                guard item.evidence.count < Self.maximumHistory else {
                    result.droppedCount += 1
                    result.reasons.insert("evidence_capacity_reached")
                    if recordIngestionDrop(kind: "evidence", reason: "evidence_capacity_reached",
                                           sourceID: source, item: &item,
                                           timestamp: timestamp) { result.changed = true }
                    return result
                }
                item.evidence.append(StoredEvidence(
                    id: Self.newID(), kind: "landing",
                    summary: "Broker-verified commit \(commit) is contained by target \(targetCommit).",
                    subject: commit, status: "passed", sourceId: source, checklistId: nil,
                    artifactId: nil, blocking: false, resolved: true, at: timestamp,
                    actor: "broker", source: "broker", scopeRevision: scopeRevision))
                if item.currentVerificationSubject == commit {
                    item.currentLandingEvidenceId = item.evidence.last?.id
                }
                result.changed = true
                result.acceptedCount += 1
            }
        }
        return result
    }

    // MARK: - Validation and mutation helpers

    private static func allowedKeys(for operation: String) -> Set<String>? {
        let common = Set(["operation", "requestId", "expectedRevision"])
        let specific: [String: Set<String>] = [
            "set_enabled": ["enabled"],
            "create": ["projectId", "title", "type", "summary", "owner", "parentId"],
            "update": ["itemId", "title", "summary", "owner", "type"],
            "transition": ["itemId", "state", "note"],
            "checklist": ["itemId", "title", "required", "checklistId", "status"],
            "milestone": ["itemId", "title", "milestoneId", "status"],
            "artifact": ["itemId", "title", "url", "kind"],
            "link": ["itemId", "kind", "targetId", "label"],
            "obligation": ["itemId", "title", "owner", "blocking"],
            "resolve_obligation": ["itemId", "obligationId", "note"],
            "span": ["itemId", "sessionId", "phase"],
            "handoff": ["itemId", "owner", "note"],
            "accept_handoff": ["itemId", "note"],
            "accept_artifact": ["itemId", "artifactId", "note"],
            "record_evidence": ["itemId", "kind", "summary", "subject", "status",
                                "sourceId", "checklistId", "blocking", "resolved"],
        ]
        guard let fields = specific[operation] else { return common }
        return common.union(fields)
    }

    private func itemIndex(_ body: [String: Any], draft: StoredState) throws -> Int {
        let id = try requiredText(body, "itemId", maximum: 200)
        guard let index = draft.items.firstIndex(where: { $0.id == id }) else {
            throw BoardError(status: 404, code: "item_not_found",
                             message: "itemId does not name a board item")
        }
        return index
    }

    private func requiredText(_ body: [String: Any], _ key: String,
                              maximum: Int) throws -> String {
        guard let value = Self.boundedText(body[key], maximum: maximum) else {
            throw BoardError(status: 400, code: "invalid_\(key)",
                             message: "\(key) must be a non-empty bounded string")
        }
        return value
    }

    private func optionalText(_ body: [String: Any], _ key: String,
                              maximum: Int) throws -> String? {
        guard let raw = body[key] else { return nil }
        guard let value = raw as? String, value.lengthOfBytes(using: .utf8) <= maximum else {
            throw BoardError(status: 400, code: "invalid_\(key)",
                             message: "\(key) must be a bounded string")
        }
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func requiredChoice(_ body: [String: Any], _ key: String,
                                choices: Set<String>) throws -> String {
        let value = try requiredText(body, key, maximum: 64)
        guard choices.contains(value) else {
            throw BoardError(status: 400, code: "invalid_\(key)",
                             message: "\(key) is not a supported value")
        }
        return value
    }

    private func validateParent(_ parentID: String, projectID: String,
                                draft: StoredState) throws {
        guard let parent = draft.items.first(where: { $0.id == parentID }),
              parent.projectId == projectID, parent.type == "epic", parent.parentId == nil else {
            throw BoardError(status: 409, code: "invalid_parent",
                             message: "parentId must name a top-level Epic in the same Project")
        }
        guard !Self.terminalStates.contains(parent.state) else {
            throw BoardError(status: 409, code: "parent_locked",
                             message: "reopen the parent Epic before adding a child")
        }
    }

    private func requireMutable(_ index: Int, draft: StoredState) throws {
        guard !Self.terminalStates.contains(draft.items[index].state) else {
            throw BoardError(status: 409, code: "item_locked",
                             message: "explicitly reopen terminal work before mutating it")
        }
        if let parentID = draft.items[index].parentId,
           let parent = draft.items.first(where: { $0.id == parentID }),
           Self.terminalStates.contains(parent.state) {
            throw BoardError(status: 409, code: "ancestor_locked",
                             message: "explicitly reopen the parent Epic before mutating its child")
        }
    }

    private func requireEvidenceCapacity(_ item: StoredItem) throws {
        guard item.evidence.count < Self.maximumHistory else {
            throw BoardError(status: 409, code: "evidence_capacity_reached",
                             message: "the immutable evidence history capacity is exhausted")
        }
    }

    private func wouldCreateRelationCycle(source: String, target: String,
                                          items: [StoredItem]) -> Bool {
        let edges = Dictionary(uniqueKeysWithValues: items.map { item in
            (item.id, item.links.filter { Self.itemLinkKinds.contains($0.kind) }.map(\.targetId))
        })
        var waiting = [target]
        var seen: Set<String> = []
        while let next = waiting.popLast() {
            if next == source { return true }
            if seen.insert(next).inserted { waiting.append(contentsOf: edges[next] ?? []) }
        }
        return false
    }

    private func makeItem(projectID: String, title: String, type: String, summary: String,
                          owner: String, parentID: String?, timestamp: Double,
                          projects: [StoredProject], items: [StoredItem]) -> StoredItem {
        let projectName = projects.first(where: { $0.id == projectID })?.name ?? "Project"
        let letters = projectName.uppercased().unicodeScalars.filter {
            CharacterSet.alphanumerics.contains($0)
        }
        let prefix = String(String.UnicodeScalarView(letters.prefix(3)))
        let keyPrefix = prefix.isEmpty ? "PRJ" : prefix
        let sequence = items.filter { $0.projectId == projectID }.count + 1
        var item = StoredItem(
            id: Self.newID(), key: "\(keyPrefix)-\(sequence)", projectId: projectID,
            title: title, type: type, state: "backlog", summary: summary, owner: owner,
            parentId: parentID, createdAt: timestamp, updatedAt: timestamp, checklist: [],
            milestones: [], artifacts: [], links: [], obligations: [], history: [], spans: [],
            evidence: [], pendingHandoff: nil, currentVerificationEvidenceId: nil,
            currentVerificationSubject: nil, currentLandingEvidenceId: nil,
            currentArtifactAcceptanceId: nil, historyDroppedCount: 0)
        appendHistory(item: &item, actor: "board", kind: "item_created",
                      summary: "Work item was created.", at: timestamp)
        return item
    }

    private func touch(_ item: inout StoredItem, actor: String, kind: String,
                       summary: String, at: Double) {
        item.updatedAt = at
        appendHistory(item: &item, actor: actor, kind: kind, summary: summary, at: at)
    }

    private func invalidateCurrentEvidence(_ item: inout StoredItem, reopen: Bool) {
        item.scopeRevision = (item.scopeRevision ?? 0) + 1
        item.currentVerificationEvidenceId = nil
        item.currentVerificationSubject = nil
        item.currentLandingEvidenceId = nil
        item.currentArtifactAcceptanceId = nil
        for index in item.checklist.indices { item.checklist[index].evidenceId = nil }
        if reopen && ["verified", "integrated", "closed"].contains(item.state) {
            item.state = "execution"
        }
    }

    private func reopenForInvalidatedEvidence(_ index: Int, draft: inout StoredState) {
        invalidateCurrentEvidence(&draft.items[index], reopen: true)
        reopenAncestors(of: draft.items[index].id, draft: &draft)
    }

    private func reopenAncestors(of itemID: String, draft: inout StoredState) {
        var parentID = draft.items.first(where: { $0.id == itemID })?.parentId
        while let current = parentID,
              let index = draft.items.firstIndex(where: { $0.id == current }) {
            if ["verified", "integrated", "closed"].contains(draft.items[index].state) {
                invalidateCurrentEvidence(&draft.items[index], reopen: true)
            }
            parentID = draft.items[index].parentId
        }
    }

    private func recordIngestionDrop(kind: String, reason: String, sourceID: String,
                                     item: inout StoredItem, timestamp: Double) -> Bool {
        recordIngestionDrop(kind: kind, reason: reason, sourceID: sourceID,
                            coverage: &item.ingestionCoverage, timestamp: timestamp)
    }

    private func recordIngestionDrop(kind: String, reason: String, sourceID: String,
                                     coverage: inout [StoredIngestionCoverage]?,
                                     timestamp: Double) -> Bool {
        let digest = Self.stringDigest(sourceID).map { String($0.prefix(32)) } ?? sourceID
        var records = coverage ?? []
        if let index = records.firstIndex(where: { $0.kind == kind && $0.reason == reason }) {
            if records[index].sourceDigests.contains(digest) { return false }
            if records[index].sourceDigests.count < Self.maximumHistory {
                records[index].sourceDigests.append(digest)
                coverage = records
                return true
            }
            guard !records[index].saturated else { return false }
            records[index].saturated = true
            coverage = records
            return true
        }
        records.append(StoredIngestionCoverage(
            kind: kind, reason: reason, sourceDigests: [digest], saturated: false,
            firstObservedAt: timestamp))
        coverage = records
        return true
    }

    private static func graphKey(projectID: String, graphID: String) -> String {
        "project-graph-v1:\(stringDigest("\(projectID)\u{0}\(graphID)") ?? "")"
    }

    private static func canonicalBrokerStart(_ task: [String: Any]) -> Double? {
        let keys = ["started_at", "startedAt", "briefed_at", "briefedAt",
                    "spawned_at", "spawnedAt", "created_at", "createdAt"]
        for key in keys {
            if let value = exactDouble(task[key]) { return value }
        }
        return nil
    }

    private func appendHistory(item: inout StoredItem, actor: String, kind: String,
                               summary: String, at: Double) {
        if item.history.count >= Self.maximumHistory {
            item.history.removeFirst(item.history.count - Self.maximumHistory + 1)
            item.historyDroppedCount = (item.historyDroppedCount ?? 0) + 1
        }
        item.history.append(StoredHistory(id: Self.newID(), at: at, actor: actor,
                                          kind: kind, summary: summary))
    }

    private func closeActiveSpans(sessionID: String, at: Double,
                                  items: inout [StoredItem]) {
        for itemIndex in items.indices {
            for spanIndex in items[itemIndex].spans.indices
            where items[itemIndex].spans[spanIndex].sessionId == sessionID
                && items[itemIndex].spans[spanIndex].endedAt == nil {
                items[itemIndex].spans[spanIndex].endedAt = at
                items[itemIndex].updatedAt = at
            }
        }
    }

    private static func boundedText(_ raw: Any?, maximum: Int) -> String? {
        guard let raw = raw as? String else { return nil }
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value.lengthOfBytes(using: .utf8) <= maximum else { return nil }
        return value
    }

    private static func exactBool(_ raw: Any?) -> Bool? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) == CFBooleanGetTypeID() else { return nil }
        return number.boolValue
    }

    private static func exactInt(_ raw: Any?) -> Int? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.doubleValue
        guard value.isFinite, value.rounded() == value else { return nil }
        return Int(exactly: value)
    }

    private static func digest(_ body: [String: Any]) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: body,
                                                      options: [.sortedKeys]) else { return nil }
        return SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private static func stringDigest(_ value: String) -> String? {
        let data = Data(value.utf8)
        return SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private static func exactDouble(_ raw: Any?) -> Double? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.doubleValue
        return value.isFinite && value >= 0 ? value : nil
    }

    private static func newID() -> String { UUID().uuidString.lowercased() }

    private static func trustedEvidenceError() -> BoardError {
        BoardError(status: 403, code: "trusted_evidence_required",
                   message: "this evidence operation requires an authenticated trusted caller")
    }

    // MARK: - Persistence and receipts

    private func load() {
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        do {
            let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
            let byteCount = (attributes[.size] as? NSNumber)?.intValue ?? Int.max
            guard byteCount <= Self.maximumStoreBytes else {
                throw BoardError(status: 503, code: "board_store_too_large",
                                 message: "the Project Board store exceeds its bounded read limit")
            }
            let data = try Data(contentsOf: url)
            guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let version = Self.exactInt(object["schemaVersion"]) else {
                throw BoardError(status: 503, code: "board_store_corrupt",
                                 message: "the Project Board store is not valid JSON state")
            }
            guard version == Self.schemaVersion else {
                throw BoardError(status: 503, code: "board_store_version_unsupported",
                                 message: "the Project Board store uses an unsupported version")
            }
            var decoded = try JSONDecoder().decode(StoredState.self, from: data)
            var migratedGraphItems: [String: String] = [:]
            for (key, itemID) in decoded.graphItems {
                if key.hasPrefix("project-graph-v1:") {
                    migratedGraphItems[key] = itemID
                } else if let item = decoded.items.first(where: { $0.id == itemID }) {
                    migratedGraphItems[Self.graphKey(projectID: item.projectId, graphID: key)] = itemID
                }
            }
            decoded.graphItems = migratedGraphItems
            if let explicit = decoded.explicitGraphItems {
                var migratedExplicit: [String: String] = [:]
                for (key, itemID) in explicit {
                    if key.hasPrefix("project-graph-v1:") {
                        migratedExplicit[key] = itemID
                    } else if let item = decoded.items.first(where: { $0.id == itemID }) {
                        migratedExplicit[Self.graphKey(projectID: item.projectId, graphID: key)] = itemID
                    }
                }
                decoded.explicitGraphItems = migratedExplicit
            }
            try Self.validateStoredState(decoded)
            state = decoded
        } catch let error as BoardError {
            unavailable = error
        } catch {
            unavailable = BoardError(status: 503, code: "board_store_corrupt",
                                     message: "the Project Board store is corrupt and was left untouched")
        }
    }

    private static func validateStoredState(_ state: StoredState) throws {
        guard state.schemaVersion == schemaVersion, state.revision >= 0,
              state.projects.count <= maximumProjects, state.items.count <= maximumItems,
              state.receipts.count <= maximumReceipts,
              (state.ingestionCoverage ?? []).count <= maximumChildren,
              (state.ingestionCoverage ?? []).allSatisfy({
                  $0.sourceDigests.count <= maximumHistory
                      && $0.firstObservedAt.isFinite && $0.firstObservedAt >= 0
              }),
              state.updatedAt.isFinite, state.updatedAt >= 0,
              Set(state.projects.map(\.id)).count == state.projects.count,
              Set(state.items.map(\.id)).count == state.items.count else {
            throw BoardError(status: 503, code: "board_store_corrupt",
                             message: "the Project Board store violates its bounds")
        }
        let projectIDs = Set(state.projects.map(\.id))
        let itemIDs = Set(state.items.map(\.id))
        for item in state.items {
            guard projectIDs.contains(item.projectId), Self.itemTypes.contains(item.type),
                  item.checklist.count <= maximumChildren,
                  item.milestones.count <= maximumChildren,
                  item.artifacts.count <= maximumChildren,
                  item.links.count <= maximumChildren,
                  item.obligations.count <= maximumChildren,
                  item.spans.count <= maximumChildren,
                  item.evidence.count <= maximumHistory,
                  item.history.count <= maximumHistory,
                  (item.scopeRevision ?? 0) >= 0,
                  (item.ingestionCoverage ?? []).count <= maximumChildren,
                  (item.ingestionCoverage ?? []).allSatisfy({
                      $0.sourceDigests.count <= maximumHistory
                          && $0.firstObservedAt.isFinite && $0.firstObservedAt >= 0
                  }),
                  item.spans.allSatisfy({ span in
                      span.startedAt.isFinite && span.startedAt >= 0
                          && (span.endedAt.map { $0.isFinite && $0 >= span.startedAt } ?? true)
                  }),
                  item.createdAt.isFinite, item.updatedAt.isFinite,
                  item.parentId.map(itemIDs.contains) ?? true else {
                throw BoardError(status: 503, code: "board_store_corrupt",
                             message: "the Project Board store contains an invalid item")
            }
            if let parentID = item.parentId {
                guard let parent = state.items.first(where: { $0.id == parentID }),
                      parent.projectId == item.projectId, parent.type == "epic",
                      parent.parentId == nil,
                      !terminalStates.contains(parent.state)
                        || ["integrated", "closed"].contains(item.state) else {
                    throw BoardError(status: 503, code: "board_store_corrupt",
                                     message: "the Project Board hierarchy violates its lifecycle boundary")
                }
            }
            if item.state == "closed" {
                guard item.obligations.filter({ $0.blocking }).allSatisfy(\.resolved),
                      item.milestones.allSatisfy({
                          $0.status == "passed" || $0.status == "not_applicable"
                      }), item.pendingHandoff == nil else {
                    throw BoardError(status: 503, code: "board_store_corrupt",
                                     message: "a closed Project Board item retains open obligations")
                }
            }
            let evidenceIDs = Set(item.evidence.map(\.id))
            guard item.currentVerificationEvidenceId.map(evidenceIDs.contains) ?? true,
                  item.currentLandingEvidenceId.map(evidenceIDs.contains) ?? true,
                  item.currentArtifactAcceptanceId.map(evidenceIDs.contains) ?? true,
                  item.checklist.allSatisfy({ row in
                      row.evidenceId.map(evidenceIDs.contains) ?? true
                  }) else {
                throw BoardError(status: 503, code: "board_store_corrupt",
                                 message: "the Project Board current evidence index is invalid")
            }
        }
        guard state.graphItems.values.allSatisfy(itemIDs.contains),
              (state.explicitGraphItems ?? [:]).allSatisfy({ key, itemID in
                  itemIDs.contains(itemID) && state.graphItems[key] == itemID
              }) else {
            throw BoardError(status: 503, code: "board_store_corrupt",
                             message: "the Project Board graph index is invalid")
        }
    }

    private func persist(_ value: StoredState) -> BoardError? {
        do {
            try Self.validateStoredState(value)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            let data = try encoder.encode(value)
            guard data.count <= Self.maximumStoreBytes else {
                return BoardError(status: 409, code: "board_store_capacity_reached",
                                  message: "the Project Board store reached its durable byte capacity")
            }
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            try data.write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: url.path)
            let handle = try FileHandle(forWritingTo: url)
            try handle.synchronize()
            try handle.close()
            return nil
        } catch let error as BoardError {
            return error
        } catch {
            return BoardError(status: 503, code: "persistence_failed",
                              message: "the Project Board mutation was not persisted")
        }
    }

    private func rememberErrorLocked(actor: String, requestId: String, digest: String,
                                     _ error: BoardError) -> Reply {
        var draft = state
        appendReceipt(StoredReceipt(
            actor: actor, requestId: requestId, digest: digest, status: error.status,
            code: error.code, message: error.message, itemId: nil, revision: state.revision),
            to: &draft)
        if let persistenceError = persist(draft) {
            return Self.errorReply(persistenceError)
        }
        state = draft
        return Self.errorReply(error)
    }

    private func replayLocked(_ receipt: StoredReceipt) -> Reply {
        if receipt.status == 200 {
            var answer = snapshotLocked(project: nil, item: receipt.itemId)
            if let item = receipt.itemId { answer["itemId"] = item }
            return Reply(status: 200, body: answer)
        }
        return Self.errorReply(BoardError(status: receipt.status,
                                          code: receipt.code ?? "request_refused",
                                          message: receipt.message ?? "the request was refused"))
    }

    private func appendReceipt(_ receipt: StoredReceipt, to draft: inout StoredState) {
        if draft.receipts.count >= Self.maximumReceipts {
            draft.receipts.removeFirst(draft.receipts.count - Self.maximumReceipts + 1)
            draft.receiptEvictions = (draft.receiptEvictions ?? 0) + 1
        }
        draft.receipts.append(receipt)
    }

    private static func errorReply(_ error: BoardError) -> Reply {
        Reply(status: error.status, body: [
            "error": ["code": error.code, "message": error.message] as [String: Any],
        ])
    }

    private static func defaultURL() -> URL {
        if let override = ProcessInfo.processInfo.environment["CLAWDLINE_BOARD_STORE"],
           !override.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return URL(fileURLWithPath: override)
        }
        let applicationSupport = FileManager.default.urls(for: .applicationSupportDirectory,
                                                           in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support", isDirectory: true)
        return applicationSupport.appendingPathComponent("Clawdline", isDirectory: true)
            .appendingPathComponent("project-board.json")
    }
}
