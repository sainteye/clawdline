import CryptoKit
import CoreFoundation
import Foundation

/// Durable per-turn workflow facts for managed assistant Sessions.
///
/// The hot path writes only this small bounded journal. Project Board commands are derived from
/// it later on one utility queue, so a slow Board store cannot become a dependency of terminal
/// input. The journal is intentionally an at-least-once outbox: every Board command is persisted
/// with its exact request id, body and expected revision before execution, and replay relies on
/// the Board store's own durable idempotency receipt.
final class ProjectBoardWorkflow: @unchecked Sendable {
    struct Identity: Codable, Equatable {
        let terminalID: String
        let provider: String
        let conversationID: String
        let projectID: String
        let projectPath: String
        let processGeneration: String

        var actor: String { "workflow:\(provider):\(conversationID)" }
        var requestScope: String {
            [terminalID, provider, conversationID, projectID, projectPath, processGeneration]
                .joined(separator: "\u{0}")
        }
    }

    struct Prepared {
        let runID: String
        let wireText: String
        let replay: Bool
        let epoch: Int
    }

    enum IngressAdmission {
        case bypassed(code: String)
        case refused(code: String)
        case managed(Prepared)
    }

    enum RetryVerdict {
        case unseen
        case same(runID: String)
        case conflict
    }

    struct Outcome {
        let status: Int
        let code: String
        let authority: String
        let runID: String?
        let itemID: String?
        let outboxPending: Int

        var object: [String: Any] {
            [
                "status": status,
                "code": code,
                "authority": authority,
                "run_id": runID ?? NSNull(),
                "item_id": itemID ?? NSNull(),
                "outbox_pending": outboxPending,
            ]
        }
    }

    struct Limits {
        var runs = 256
        var eventsPerRun = 64
        var receipts = 512
        var outbox = 512
        var maximumBytes = 2 * 1_024 * 1_024
    }

    private struct Event: Codable {
        var id: String
        var operation: String
        var at: Double
        var summary: String?
        var classification: String?
        var phase: String?
        var disposition: String?
        var title: String?
        var itemType: String?
        var owner: String?
        var itemSummary: String?
        var nextAction: String?
        var outputs: [Output]
        var remaining: [Remaining]
        var supplement: Supplement?
        var handoffOwner: String?
        var handoffNote: String?
    }

    private struct Output: Codable {
        var title: String
        var url: String
        var kind: String
    }

    private struct Remaining: Codable {
        var title: String
        var owner: String
        var blocking: Bool
    }

    /// A person's later instruction is scope, not progress prose. Keep every decision-bearing
    /// field in the journal even where today's Board command vocabulary has to project it across
    /// a checklist/child plus an obligation.
    private struct Supplement: Codable {
        var version: Int
        var kind: String
        var title: String
        var owner: String
        var acceptance: String
        var required: Bool
        var actorKind: String
        var childType: String?
        var sourceRunID: String
        var sourceSessionID: String
        // Optional for old journal events: never change a prepared legacy replay body.
        var relationID: String? = nil
    }

    private struct Run: Codable {
        var id: String
        var requestID: String
        var fingerprint: String
        var identity: Identity
        var epoch: Int
        var inputKind: String
        var contentReference: String
        var textBytes: Int
        var imageCount: Int
        var createdAt: Double
        var delivery: String
        var deliveredAt: Double?
        var classification: String?
        var itemID: String?
        var phase: String?
        var completion: String?
        var nextAction: String?
        var missingFollowUp: [String]
        var events: [Event]
    }

    private struct Receipt: Codable {
        var actor: String
        var requestScope: String?
        var requestID: String
        var fingerprint: String
        var status: Int
        var code: String
        var runID: String
        var itemID: String?
        var at: Double
    }

    private struct Outbox: Codable {
        var id: String
        var version: Int?
        var runID: String
        var eventID: String
        var kind: String
        var index: Int
        var status: String
        var attempts: Int
        var preparedRequestID: String?
        var preparedRevision: Int?
        var failureCode: String?
        var createdItemID: String?
        var updatedAt: Double
    }

    private struct SettlementFailure: Codable {
        var outboxID: String
        var code: String
        var at: Double
    }

    private struct PreparedOutbox {
        var id: String
        var version: Int
        var runID: String
        var eventID: String
        var kind: String
        var body: [String: Any]
        var actor: String
    }

    private struct State: Codable {
        var schemaVersion = 2
        var enabledEpoch = 0
        var observedEnabled: Bool?
        var observedRevision = 0
        var modeAvailable = true
        var runs: [Run] = []
        var receipts: [Receipt] = []
        var outbox: [Outbox] = []
        var settlementFailures: [SettlementFailure]?
    }

    private static let production = ProjectBoardWorkflow(url: defaultURL())
    private static let sharedLock = NSLock()
    private static var sharedForTesting: ProjectBoardWorkflow?
    static var shared: ProjectBoardWorkflow {
        sharedLock.lock(); defer { sharedLock.unlock() }
        return sharedForTesting ?? production
    }

    static func configureSharedForTesting(_ workflow: ProjectBoardWorkflow?) {
        sharedLock.lock(); sharedForTesting = workflow; sharedLock.unlock()
    }

    private let url: URL
    private let now: () -> Date
    private let boardHeader: () -> (enabled: Bool, revision: Int, available: Bool)
    private let ensureProject: (String, String) -> (persisted: Bool, reason: String?)
    private let boardCommand: ([String: Any], String) -> ProjectBoardStore.Reply
    private let journalSynchronize: (URL) throws -> Void
    private let limits: Limits
    private let autoStart: Bool
    private let lock = NSLock()
    private let workerQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.board.workflow", qos: .utility)
    private var state = State()
    private var storageFailure: String?
    private var workerScheduled = false

    init(url: URL, now: @escaping () -> Date = Date.init,
         boardHeader: @escaping () -> (enabled: Bool, revision: Int, available: Bool) = {
             let header = ProjectBoardStore.shared.readHeader()
             return (header.enabled, header.revision, header.available)
         },
         ensureProject: @escaping (String, String) -> (persisted: Bool, reason: String?) = {
             let result = ProjectBoardStore.shared.ensureProject(id: $0, name: $1)
             return (result.persisted && result.status != .refused,
                     result.reason)
         },
         boardCommand: @escaping ([String: Any], String) -> ProjectBoardStore.Reply = {
             ProjectBoardStore.shared.command($0, actor: $1, workflowOrigin: true)
         },
         journalSynchronize: @escaping (URL) throws -> Void = ProjectBoardWorkflow.syncJournal,
         limits: Limits = Limits(), autoStart: Bool = true) {
        self.url = url
        self.now = now
        self.boardHeader = boardHeader
        self.ensureProject = ensureProject
        self.boardCommand = boardCommand
        self.journalSynchronize = journalSynchronize
        self.limits = limits
        self.autoStart = autoStart
        load()
        if autoStart { kick() }
    }

    /// Refresh the mode boundary without deriving it from elapsed time or an unrelated Board
    /// revision. An unavailable Board preserves the last known enabled state; it is a gap, not an
    /// implicit OFF transition.
    @discardableResult
    func syncMode() -> String? {
        let header = boardHeader()
        lock.lock(); defer { lock.unlock() }
        guard storageFailure == nil else { return "workflow_persistence_failed" }
        var draft = state
        draft.modeAvailable = header.available
        if header.available {
            if draft.observedEnabled == nil {
                draft.observedEnabled = header.enabled
                if header.enabled { draft.enabledEpoch = max(1, draft.enabledEpoch) }
            } else if draft.observedEnabled != header.enabled {
                draft.observedEnabled = header.enabled
                draft.enabledEpoch += 1
            }
            draft.observedRevision = header.revision
        }
        guard !Self.sameState(draft, state) else { return nil }
        guard persist(draft) else { return "workflow_persistence_failed" }
        state = draft
        return header.available ? nil : "board_unavailable"
    }

    func retryVerdict(requestID: String, fingerprint: String,
                      identity: Identity) -> RetryVerdict {
        lock.lock(); defer { lock.unlock() }
        guard state.observedEnabled == true else { return .unseen }
        guard let run = state.runs.last(where: {
            $0.identity.requestScope == identity.requestScope && $0.requestID == requestID
        }) else { return .unseen }
        return run.fingerprint == fingerprint ? .same(runID: run.id) : .conflict
    }

    func prepareIngress(requestID: String, fingerprint: String, text: String,
                        imageCount: Int, identity: Identity) -> IngressAdmission {
        let modeGap = syncMode()
        lock.lock(); defer { lock.unlock() }
        guard storageFailure == nil else {
            return .refused(code: "workflow_persistence_failed")
        }
        guard valid(identity), bounded(requestID, 200), bounded(fingerprint, 128),
              imageCount >= 0, imageCount <= 8 else {
            return .refused(code: "workflow_identity_invalid")
        }
        guard state.observedEnabled == true else {
            return .bypassed(code: state.modeAvailable ? "board_disabled"
                                                      : "workflow_mode_unknown")
        }
        if let existing = state.runs.last(where: {
            $0.identity.requestScope == identity.requestScope && $0.requestID == requestID
        }) {
            guard existing.fingerprint == fingerprint else {
                return .refused(code: "workflow_request_conflict")
            }
            return .managed(Prepared(
                runID: existing.id,
                wireText: Self.wireText(original: text, run: existing,
                                        modeGap: modeGap),
                replay: true, epoch: existing.epoch))
        }
        var draft = state
        if draft.runs.count >= limits.runs {
            if let removable = draft.runs.firstIndex(where: {
                $0.delivery != "pending" && $0.missingFollowUp.isEmpty
                    && !Self.outboxProtectsRun($0.id, in: draft)
            }) {
                let removedID = draft.runs.remove(at: removable).id
                draft.outbox.removeAll { $0.runID == removedID }
                draft.receipts.removeAll { $0.runID == removedID }
            } else {
                return .refused(code: "workflow_capacity_reached")
            }
        }
        let runID = "run-" + String(Self.digest(
            "\(identity.requestScope)\u{0}\(requestID)\u{0}\(fingerprint)").prefix(32))
        let kind = imageCount > 0 ? (text.isEmpty ? "image" : "text_and_image")
                                  : "text_or_transcribed_voice"
        let run = Run(
            id: runID, requestID: requestID, fingerprint: fingerprint, identity: identity,
            epoch: draft.enabledEpoch, inputKind: kind,
            contentReference: "terminal-request:\(Self.digest(fingerprint).prefix(24))",
            textBytes: text.utf8.count, imageCount: imageCount,
            createdAt: now().timeIntervalSince1970, delivery: "pending", deliveredAt: nil,
            classification: nil, itemID: nil, phase: nil, completion: nil,
            nextAction: nil, missingFollowUp: ["begin"], events: [])
        draft.runs.append(run)
        guard persist(draft) else {
            return .refused(code: "workflow_persistence_failed")
        }
        state = draft
        return .managed(Prepared(
            runID: runID, wireText: Self.wireText(original: text, run: run, modeGap: modeGap),
            replay: false, epoch: run.epoch))
    }

    func markDelivery(runID: String, identity: Identity, delivered: Bool) -> Outcome {
        lock.lock(); defer { lock.unlock() }
        guard let index = state.runs.firstIndex(where: { $0.id == runID }) else {
            return outcome(404, "workflow_run_not_found", runID: runID,
                           authority: "broker_observed")
        }
        guard state.runs[index].identity == identity else {
            return outcome(409, "workflow_identity_mismatch", runID: runID,
                           authority: "broker_observed")
        }
        var draft = state
        draft.runs[index].delivery = delivered ? "delivered" : "rejected"
        draft.runs[index].deliveredAt = now().timeIntervalSince1970
        if !delivered { draft.runs[index].missingFollowUp.removeAll() }
        guard persist(draft) else {
            return outcome(503, "workflow_persistence_failed", runID: runID,
                           authority: "broker_observed")
        }
        state = draft
        return outcome(200, delivered ? "workflow_delivery_recorded"
                                      : "workflow_delivery_rejected", runID: runID,
                       authority: "broker_observed")
    }

    /// Record one assistant-attested semantic boundary. This vocabulary intentionally contains
    /// no verification, landing or lifecycle transition operation.
    func record(_ body: [String: Any], requestID: String, fingerprint: String,
                identity: Identity) -> Outcome {
        let modeGap = syncMode()
        lock.lock()
        guard storageFailure == nil else {
            let answer = outcome(503, "workflow_persistence_failed")
            lock.unlock(); return answer
        }
        guard modeGap != "workflow_mode_unknown", state.observedEnabled == true else {
            let answer = outcome(409, state.modeAvailable ? "board_disabled"
                                                        : "workflow_mode_unknown")
            lock.unlock(); return answer
        }
        guard let operation = boundedText(body["operation"], 32),
              let runID = boundedText(body["run_id"], 80),
              bounded(requestID, 200), bounded(fingerprint, 128) else {
            let answer = outcome(400, "workflow_command_invalid")
            lock.unlock(); return answer
        }
        let callerIdentityFields: Set<String> = [
            "terminal_id", "provider", "conversation_id", "project_id", "project_path",
            "process_generation", "board_epoch", "epoch",
        ]
        guard callerIdentityFields.isDisjoint(with: body.keys) else {
            let answer = outcome(400, "workflow_command_fields", runID: runID)
            lock.unlock(); return answer
        }
        let receiptScope = Self.receiptScope(identity: identity, runID: runID)
        if let prior = state.receipts.last(where: {
            $0.requestScope == receiptScope && $0.requestID == requestID
        }) {
            let answer = prior.fingerprint == fingerprint
                ? Outcome(status: prior.status, code: prior.code,
                          authority: "assistant_attested", runID: prior.runID,
                          itemID: prior.itemID, outboxPending: pendingCount(state))
                : outcome(409, "workflow_request_conflict", runID: runID)
            lock.unlock(); return answer
        }
        guard let runIndex = state.runs.firstIndex(where: { $0.id == runID }) else {
            let answer = outcome(404, "workflow_run_not_found", runID: runID)
            lock.unlock(); return answer
        }
        guard state.runs[runIndex].identity == identity else {
            let answer = outcome(409, "workflow_identity_mismatch", runID: runID)
            lock.unlock(); return answer
        }
        guard state.runs[runIndex].epoch == state.enabledEpoch else {
            let answer = outcome(409, "workflow_epoch_stale", runID: runID)
            lock.unlock(); return answer
        }
        guard state.runs[runIndex].delivery == "delivered" else {
            let answer = outcome(409, "workflow_delivery_unconfirmed", runID: runID)
            lock.unlock(); return answer
        }
        let parsed: Event
        do {
            parsed = try event(operation: operation, body: body, run: state.runs[runIndex],
                               requestID: requestID, fingerprint: fingerprint)
        } catch let refusal as WorkflowRefusal {
            let answer = outcome(refusal.status, refusal.code, runID: runID)
            lock.unlock(); return answer
        } catch {
            let answer = outcome(400, "workflow_command_invalid", runID: runID)
            lock.unlock(); return answer
        }

        var draft = state
        apply(parsed, to: &draft.runs[runIndex])
        if draft.runs[runIndex].events.count >= limits.eventsPerRun {
            draft.runs[runIndex].events.removeFirst(
                draft.runs[runIndex].events.count - limits.eventsPerRun + 1)
        }
        draft.runs[runIndex].events.append(parsed)
        materializeIntents(runIndex: runIndex, draft: &draft)
        let code = operation == "deliver" ? "workflow_receipt_recorded"
                                          : "workflow_\(operation)_recorded"
        let receipt = Receipt(
            actor: identity.actor, requestScope: receiptScope,
            requestID: requestID, fingerprint: fingerprint,
            status: 202, code: code, runID: runID,
            itemID: draft.runs[runIndex].itemID, at: now().timeIntervalSince1970)
        if draft.receipts.count >= limits.receipts {
            draft.receipts.removeFirst(draft.receipts.count - limits.receipts + 1)
        }
        draft.receipts.append(receipt)
        guard draft.outbox.count <= limits.outbox else {
            let answer = outcome(429, "workflow_outbox_full", runID: runID)
            lock.unlock(); return answer
        }
        guard persist(draft) else {
            let answer = outcome(503, "workflow_persistence_failed", runID: runID)
            lock.unlock(); return answer
        }
        state = draft
        let answer = Outcome(status: 202, code: code, authority: "assistant_attested",
                             runID: runID, itemID: draft.runs[runIndex].itemID,
                             outboxPending: pendingCount(draft))
        lock.unlock()
        if autoStart { kick() }
        return answer
    }

    func snapshot(runID: String) -> [String: Any]? {
        lock.lock(); defer { lock.unlock() }
        guard let run = state.runs.first(where: { $0.id == runID }) else { return nil }
        let rows = state.outbox.filter { $0.runID == runID }
        return [
            "run_id": run.id,
            "epoch": run.epoch,
            "delivery": run.delivery,
            "classification": run.classification ?? NSNull(),
            "item_id": run.itemID ?? NSNull(),
            "completion": run.completion ?? NSNull(),
            "next_action": run.nextAction ?? NSNull(),
            "missing_follow_up": run.missingFollowUp,
            "outbox": [
                "pending": rows.filter { $0.status == "pending" }.count,
                "failed": rows.filter { $0.status == "failed" }.count,
                "failures": rows.compactMap(\.failureCode),
            ] as [String: Any],
            "identity": [
                "terminal_id": run.identity.terminalID,
                "provider": run.identity.provider,
                "conversation_id": run.identity.conversationID,
                "project_id": run.identity.projectID,
                "process_generation": run.identity.processGeneration,
            ],
            "authority": "mixed_observed_and_assistant_attested",
        ]
    }

    func snapshot(identity: Identity) -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        let runs = state.runs.filter { $0.identity == identity }.suffix(16)
        return [
            "schema_version": 2,
            "enabled": state.observedEnabled == true,
            "epoch": state.enabledEpoch,
            "mode_available": state.modeAvailable,
            "runs": runs.map { run in
                let rows = state.outbox.filter { $0.runID == run.id }
                return [
                    "run_id": run.id, "delivery": run.delivery,
                    "classification": run.classification ?? NSNull(),
                    "item_id": run.itemID ?? NSNull(),
                    "completion": run.completion ?? NSNull(),
                    "missing_follow_up": run.missingFollowUp,
                    "outbox_pending": rows.filter { $0.status == "pending" }.count,
                    "outbox_failures": rows.compactMap(\.failureCode),
                ] as [String: Any]
            },
        ]
    }

    /// Machine-authenticated recovery view for gaps whose original terminal process no longer
    /// exists. It is deliberately fixed-size and reads only the durable journal.
    func historicalGaps() -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        let allGapRuns = state.runs.filter { run in
            let rows = state.outbox.filter { $0.runID == run.id }
            return run.delivery == "pending" || !run.missingFollowUp.isEmpty
                || rows.contains { $0.status != "complete" }
        }
        let gapRuns = allGapRuns.suffix(32)
        return [
            "schema_version": 2,
            "authority": "machine_authenticated_durable_journal",
            "limit": 32,
            "storage_failure": storageFailure ?? NSNull(),
            "omitted_count": max(0, allGapRuns.count - gapRuns.count),
            "runs": gapRuns.map { run in
                let rows = state.outbox.filter { $0.runID == run.id }
                return [
                    "run_id": run.id,
                    "delivery": run.delivery,
                    "missing_follow_up": run.missingFollowUp,
                    "outbox_pending": rows.filter {
                        $0.status == "pending" || $0.status == "inflight"
                    }.count,
                    "outbox_failures": rows.compactMap(\.failureCode),
                    "identity": [
                        "terminal_id": run.identity.terminalID,
                        "provider": run.identity.provider,
                        "conversation_id": run.identity.conversationID,
                        "project_id": run.identity.projectID,
                        "process_generation": run.identity.processGeneration,
                    ],
                ] as [String: Any]
            },
            "settlement_failures": (state.settlementFailures ?? []).map {
                ["outbox_id": $0.outboxID, "code": $0.code, "at": $0.at]
            },
        ]
    }

    func drainForTesting() {
        workerQueue.sync { self.drainOutbox() }
    }

    // MARK: - Parsing and projection

    private struct WorkflowRefusal: Error {
        let status: Int
        let code: String
    }

    private func event(operation: String, body: [String: Any], run: Run,
                       requestID: String, fingerprint: String) throws -> Event {
        let common: Set<String> = ["operation", "run_id"]
        let fields: Set<String>
        switch operation {
        case "begin":
            fields = common.union(["classification", "item_id", "title", "type",
                                   "summary", "owner", "phase"])
        case "progress":
            fields = common.union(["summary", "outputs", "remaining"])
        case "deliver":
            fields = common.union(["disposition", "summary", "outputs", "remaining",
                                   "next_action"])
        case "supplement":
            let kind = body["kind"] as? String
            fields = common.union(["version", "kind", "title", "owner", "acceptance",
                                   "disposition", "actor_kind"])
                .union(kind == "child" ? ["type"] : [])
        case "handoff":
            fields = common.union(["owner", "note"])
        default:
            throw WorkflowRefusal(status: 400, code: "workflow_operation_unknown")
        }
        let suppliedFields = Set(body.keys)
        guard operation == "supplement" ? suppliedFields == fields
                                        : suppliedFields.isSubset(of: fields) else {
            throw WorkflowRefusal(status: 400, code: "workflow_command_fields")
        }
        let eventID = "event-" + String(Self.digest(
            "\(run.id)\u{0}\(requestID)\u{0}\(fingerprint)").prefix(32))
        var result = Event(
            id: eventID, operation: operation, at: now().timeIntervalSince1970,
            summary: nil, classification: nil, phase: nil, disposition: nil, title: nil,
            itemType: nil, owner: nil, itemSummary: nil, nextAction: nil,
            outputs: [], remaining: [], supplement: nil,
            handoffOwner: nil, handoffNote: nil)
        switch operation {
        case "begin":
            guard run.classification == nil else {
                throw WorkflowRefusal(status: 409, code: "workflow_already_begun")
            }
            guard let classification = boundedText(body["classification"], 32),
                  ["existing_item", "new_work", "question", "clarification"]
                    .contains(classification),
                  let phase = boundedText(body["phase"], 32),
                  ["planning", "output", "review_testing", "correction", "integration"]
                    .contains(phase) else {
                throw WorkflowRefusal(status: 400, code: "workflow_begin_invalid")
            }
            result.classification = classification
            result.phase = phase
            if classification == "existing_item" {
                guard let item = boundedText(body["item_id"], 200),
                      body["title"] == nil, body["type"] == nil else {
                    throw WorkflowRefusal(status: 400, code: "workflow_item_required")
                }
                result.title = item // carried as the selected opaque item until apply()
            } else if classification == "new_work" {
                guard body["item_id"] == nil,
                      let title = boundedText(body["title"], 300),
                      let type = boundedText(body["type"], 32),
                      ["feature", "refactor", "task", "bug", "coordination", "epic"]
                        .contains(type) else {
                    throw WorkflowRefusal(status: 400, code: "workflow_new_work_invalid")
                }
                result.title = title
                result.itemType = type
                result.itemSummary = optionalText(body["summary"], 4_000) ?? ""
                result.owner = optionalText(body["owner"], 300) ?? run.identity.conversationID
            } else {
                guard body["item_id"] == nil, body["title"] == nil,
                      body["type"] == nil else {
                    throw WorkflowRefusal(status: 400, code: "workflow_nonwork_has_item")
                }
            }
        case "progress":
            guard run.classification != nil,
                  let summary = boundedText(body["summary"], 1_000) else {
                throw WorkflowRefusal(status: 409, code: "workflow_begin_required")
            }
            result.summary = summary
            result.outputs = try outputs(body["outputs"])
            result.remaining = try remaining(body["remaining"])
        case "deliver":
            guard run.classification != nil,
                  let disposition = boundedText(body["disposition"], 32),
                  ["delivered", "waiting_user", "waiting_external", "interrupted", "cancelled"]
                    .contains(disposition),
                  let summary = boundedText(body["summary"], 1_000),
                  let next = boundedText(body["next_action"], 500) else {
                throw WorkflowRefusal(status: 400, code: "workflow_deliver_invalid")
            }
            result.disposition = disposition
            result.summary = summary
            result.nextAction = next
            result.outputs = try outputs(body["outputs"])
            result.remaining = try remaining(body["remaining"])
        case "supplement":
            guard run.itemID != nil,
                  let version = Self.exactInt(body["version"]), version == 1,
                  let kind = boundedText(body["kind"], 32),
                  ["checklist", "child"].contains(kind),
                  let title = boundedText(body["title"], 500),
                  let owner = boundedText(body["owner"], 300),
                  let acceptance = boundedText(body["acceptance"], 1_000),
                  let disposition = boundedText(body["disposition"], 32),
                  ["required", "optional"].contains(disposition),
                  let actorKind = boundedText(body["actor_kind"], 32),
                  ["assistant", "user", "external"].contains(actorKind) else {
                throw WorkflowRefusal(status: 400, code: "workflow_supplement_invalid")
            }
            var childType: String?
            if kind == "child" {
                guard let type = boundedText(body["type"], 32),
                      ["feature", "refactor", "task", "bug", "coordination"].contains(type)
                else {
                    throw WorkflowRefusal(status: 400, code: "workflow_supplement_invalid")
                }
                childType = type
            }
            result.supplement = Supplement(
                version: version, kind: kind, title: title, owner: owner,
                acceptance: acceptance, required: disposition == "required",
                actorKind: actorKind, childType: childType,
                sourceRunID: run.id, sourceSessionID: run.identity.conversationID,
                relationID: "wfs-" + Self.digest("\(run.identity.actor)\u{0}\(run.id)\u{0}\(eventID)"))
        case "handoff":
            guard run.itemID != nil,
                  let owner = boundedText(body["owner"], 300),
                  let note = boundedText(body["note"], 1_000) else {
                throw WorkflowRefusal(status: 400, code: "workflow_handoff_invalid")
            }
            result.handoffOwner = owner
            result.handoffNote = note
        default: break
        }
        return result
    }

    private func outputs(_ raw: Any?) throws -> [Output] {
        guard let raw else { return [] }
        guard let rows = raw as? [[String: Any]], rows.count <= 16 else {
            throw WorkflowRefusal(status: 400, code: "workflow_outputs_invalid")
        }
        return try rows.map { row in
            guard Set(row.keys) == Set(["title", "url", "kind"]),
                  let title = boundedText(row["title"], 300),
                  let url = boundedText(row["url"], 2_000),
                  let kind = boundedText(row["kind"], 32),
                  ["document", "website", "deployment", "commit", "other"].contains(kind),
                  let components = URLComponents(string: url),
                  ["http", "https"].contains(components.scheme?.lowercased() ?? ""),
                  components.host != nil else {
                throw WorkflowRefusal(status: 400, code: "workflow_outputs_invalid")
            }
            return Output(title: title, url: url, kind: kind)
        }
    }

    private func remaining(_ raw: Any?) throws -> [Remaining] {
        guard let raw else { return [] }
        guard let rows = raw as? [[String: Any]], rows.count <= 16 else {
            throw WorkflowRefusal(status: 400, code: "workflow_remaining_invalid")
        }
        return try rows.map { row in
            guard Set(row.keys) == Set(["title", "owner", "blocking"]),
                  let title = boundedText(row["title"], 500),
                  let owner = boundedText(row["owner"], 300),
                  let blocking = row["blocking"] as? Bool else {
                throw WorkflowRefusal(status: 400, code: "workflow_remaining_invalid")
            }
            return Remaining(title: title, owner: owner, blocking: blocking)
        }
    }

    private func apply(_ event: Event, to run: inout Run) {
        switch event.operation {
        case "begin":
            run.classification = event.classification
            run.phase = event.phase
            if event.classification == "existing_item" { run.itemID = event.title }
            run.missingFollowUp.removeAll { $0 == "begin" }
            if !run.missingFollowUp.contains("deliver") { run.missingFollowUp.append("deliver") }
        case "deliver":
            run.completion = event.disposition
            run.nextAction = event.nextAction
            run.missingFollowUp.removeAll { $0 == "deliver" }
        case "handoff":
            run.nextAction = "handoff proposed to \(event.handoffOwner ?? "receiver")"
        default: break
        }
    }

    // MARK: - Durable outbox

    private func materializeIntents(runIndex: Int, draft: inout State) {
        let run = draft.runs[runIndex]
        for event in run.events {
            if event.operation == "begin", event.classification == "new_work",
               run.itemID == nil {
                appendIntent(id: "\(event.id)-create", run: run, event: event,
                             kind: "create", index: 0, draft: &draft)
                continue
            }
            guard run.itemID != nil else { continue }
            if event.operation == "begin",
               ["new_work", "existing_item"].contains(event.classification ?? "") {
                appendIntent(id: "\(event.id)-link", run: run, event: event,
                             kind: "link", index: 0, draft: &draft)
                appendIntent(id: "\(event.id)-span", run: run, event: event,
                             kind: "span", index: 0, draft: &draft)
            }
            if event.operation == "deliver" || event.operation == "handoff" {
                appendIntent(id: "\(event.id)-end-span", run: run, event: event,
                             kind: "end_span", index: 0, draft: &draft)
            }
            for (index, _) in event.outputs.enumerated() {
                appendIntent(id: "\(event.id)-artifact-\(index)", run: run, event: event,
                             kind: "artifact", index: index, draft: &draft)
            }
            for (index, _) in event.remaining.enumerated() {
                appendIntent(id: "\(event.id)-obligation-\(index)", run: run, event: event,
                             kind: "obligation", index: index, draft: &draft)
            }
            if let supplement = event.supplement {
                if supplement.kind == "checklist" {
                    appendIntent(id: "\(event.id)-supplement-checklist", run: run, event: event,
                                 kind: "supplement_checklist", index: 0, draft: &draft)
                    appendIntent(id: "\(event.id)-supplement-obligation", run: run, event: event,
                                 kind: "supplement_obligation", index: 0, draft: &draft)
                } else {
                    let createID = "\(event.id)-supplement-child-create"
                    appendIntent(id: createID, run: run, event: event,
                                 kind: "supplement_child_create", index: 0, draft: &draft)
                    if let childID = draft.outbox.first(where: {
                        $0.id == createID && $0.status == "complete"
                    })?.createdItemID {
                        appendIntent(id: "\(event.id)-supplement-child-link", run: run,
                                     event: event, kind: "supplement_child_link", index: 0,
                                     draft: &draft, targetItemID: childID)
                        appendIntent(id: "\(event.id)-supplement-child-obligation", run: run,
                                     event: event, kind: "supplement_child_obligation", index: 0,
                                     draft: &draft, targetItemID: childID)
                    }
                }
            }
            if event.operation == "handoff" {
                appendIntent(id: "\(event.id)-handoff", run: run, event: event,
                             kind: "handoff", index: 0, draft: &draft)
            }
        }
    }

    private func appendIntent(id: String, run: Run, event: Event, kind: String, index: Int,
                              draft: inout State, targetItemID: String? = nil) {
        guard !draft.outbox.contains(where: { $0.id == id }) else { return }
        draft.outbox.append(Outbox(
            id: id, version: 1, runID: run.id, eventID: event.id, kind: kind, index: index,
            status: "pending", attempts: 0, preparedRequestID: nil,
            preparedRevision: nil, failureCode: nil, createdItemID: targetItemID,
            updatedAt: now().timeIntervalSince1970))
    }

    private func kick() {
        lock.lock()
        guard !workerScheduled, state.outbox.contains(where: { $0.status == "pending" }) else {
            lock.unlock(); return
        }
        workerScheduled = true
        lock.unlock()
        workerQueue.async { [weak self] in
            guard let self else { return }
            self.drainOutbox()
            self.lock.lock(); self.workerScheduled = false
            let more = self.state.outbox.contains { $0.status == "pending" }
            self.lock.unlock()
            if more { self.kick() }
        }
    }

    private func drainOutbox() {
        while true {
            let prepared: PreparedOutbox?
            lock.lock()
            if let index = state.outbox.firstIndex(where: { $0.status == "pending" }) {
                prepared = prepareOutbox(index: index)
            } else {
                prepared = nil
            }
            lock.unlock()
            guard let prepared else { return }
            if ["create", "supplement_child_create"].contains(prepared.kind),
               let project = prepared.body["projectId"] as? String,
               let name = prepared.body["projectName"] as? String {
                let ensured = ensureProject(project, name)
                if !ensured.persisted {
                    finishOutbox(prepared, status: "failed",
                                 code: ensured.reason ?? "board_project_unavailable")
                    continue
                }
            }
            var command = prepared.body
            command.removeValue(forKey: "projectName")
            let reply = boardCommand(command, prepared.actor)
            let code = Self.errorCode(reply.body)
            if reply.status == 200 {
                finishOutbox(prepared, status: "complete", code: nil,
                             createdItemID: reply.body["itemId"] as? String)
            } else if code == "revision_conflict" {
                retryOutbox(prepared,
                            terminalCode: code ?? "revision_conflict", resetPrepared: true)
            } else if reply.status >= 500 {
                // A 5xx may have arrived after the Board committed. Keep the exact request/body
                // so its durable idempotency receipt, rather than a second mutation, settles us.
                retryOutbox(prepared,
                            terminalCode: code ?? "board_unavailable", resetPrepared: false)
            } else {
                finishOutbox(prepared, status: "failed",
                             code: code ?? "board_command_refused")
            }
        }
    }

    /// Persist the exact Board command identity before it can execute. Returning nil means this
    /// row was settled as a visible failure while preparing it.
    private func prepareOutbox(index: Int) -> PreparedOutbox? {
        guard state.outbox.indices.contains(index),
              let runIndex = state.runs.firstIndex(where: {
                  $0.id == state.outbox[index].runID
              }),
              let event = state.runs[runIndex].events.first(where: {
                  $0.id == state.outbox[index].eventID
              }) else {
            if state.outbox.indices.contains(index) {
                state.outbox[index].status = "failed"
                state.outbox[index].failureCode = "workflow_outbox_subject_missing"
                _ = persist(state)
            }
            return nil
        }
        let header = boardHeader()
        guard header.available, header.enabled,
              state.runs[runIndex].epoch == state.enabledEpoch else {
            var draft = state
            draft.outbox[index].status = "failed"
            draft.outbox[index].failureCode = header.available
                ? "workflow_epoch_stale" : "board_unavailable"
            draft.outbox[index].updatedAt = now().timeIntervalSince1970
            if persist(draft) { state = draft }
            return nil
        }
        var draft = state
        if draft.outbox[index].preparedRequestID == nil {
            draft.outbox[index].attempts += 1
            draft.outbox[index].preparedRequestID =
                "workflow-\(draft.outbox[index].id)-\(draft.outbox[index].attempts)"
            draft.outbox[index].preparedRevision = header.revision
            draft.outbox[index].updatedAt = now().timeIntervalSince1970
        }
        draft.outbox[index].status = "inflight"
        guard persist(draft) else {
            if state.outbox.indices.contains(index) {
                state.outbox[index].failureCode = "workflow_persistence_failed"
            }
            return nil
        }
        state = draft
        guard var body = boardBody(outbox: draft.outbox[index], run: draft.runs[runIndex],
                                   event: event),
              let requestID = draft.outbox[index].preparedRequestID,
              let revision = draft.outbox[index].preparedRevision else {
            var invalid = state
            invalid.outbox[index].status = "failed"
            invalid.outbox[index].failureCode = "workflow_outbox_invalid"
            if persist(invalid) { state = invalid }
            return nil
        }
        body["requestId"] = requestID
        body["expectedRevision"] = revision
        let row = draft.outbox[index]
        return PreparedOutbox(
            id: row.id, version: row.version ?? 1, runID: row.runID,
            eventID: row.eventID, kind: row.kind, body: body,
            actor: draft.runs[runIndex].identity.actor)
    }

    private func boardBody(outbox: Outbox, run: Run, event: Event) -> [String: Any]? {
        switch outbox.kind {
        case "create":
            guard let title = event.title, let type = event.itemType else { return nil }
            return [
                "operation": "create", "projectId": run.identity.projectID,
                "projectName": StartPoints.label(for: run.identity.projectPath),
                "title": title, "type": type, "summary": event.itemSummary ?? "",
                "owner": event.owner ?? run.identity.conversationID,
            ]
        case "link":
            guard let item = run.itemID else { return nil }
            return [
                "operation": "link", "itemId": item, "kind": "session",
                "targetId": run.identity.conversationID,
                "label": "Managed \(run.identity.provider) Session",
            ]
        case "span":
            guard let item = run.itemID, let phase = event.phase else { return nil }
            return ["operation": "span", "itemId": item,
                    "sessionId": run.identity.conversationID, "phase": phase]
        case "end_span":
            guard let item = run.itemID,
                  let start = state.outbox.first(where: {
                      $0.runID == run.id && $0.kind == "span" && $0.status == "complete"
                  }), let startRequestID = start.preparedRequestID else { return nil }
            return ["operation": "end_span", "itemId": item,
                    "sessionId": run.identity.conversationID,
                    "startRequestId": startRequestID,
                    "note": event.summary ?? event.handoffNote ?? "Session interval ended."]
        case "artifact":
            guard let item = run.itemID, event.outputs.indices.contains(outbox.index) else {
                return nil
            }
            let output = event.outputs[outbox.index]
            return ["operation": "artifact", "itemId": item, "title": output.title,
                    "url": output.url, "kind": output.kind]
        case "obligation":
            guard let item = run.itemID, event.remaining.indices.contains(outbox.index) else {
                return nil
            }
            let remaining = event.remaining[outbox.index]
            return ["operation": "obligation", "itemId": item,
                    "title": remaining.title, "owner": remaining.owner,
                    "blocking": remaining.blocking]
        case "supplement_checklist":
            guard let item = run.itemID, let supplement = event.supplement else { return nil }
            var body: [String: Any] = ["operation": "checklist", "itemId": item,
                    "title": supplement.title, "required": supplement.required]
            if let relation = supplement.relationID { body["supplementRelationId"] = relation }
            return body
        case "supplement_child_create":
            guard let parent = run.itemID, let supplement = event.supplement,
                  let type = supplement.childType else { return nil }
            return [
                "operation": "create", "projectId": run.identity.projectID,
                "projectName": StartPoints.label(for: run.identity.projectPath),
                "title": supplement.title, "type": type,
                "summary": "Acceptance: \(supplement.acceptance)\nSource workflow run: \(supplement.sourceRunID)",
                "owner": supplement.owner, "parentId": parent,
            ]
        case "supplement_child_link":
            guard let child = outbox.createdItemID else { return nil }
            return ["operation": "link", "itemId": child, "kind": "session",
                    "targetId": event.supplement?.sourceSessionID
                        ?? run.identity.conversationID,
                    "label": "Supplement source \(run.identity.provider) Session"]
        case "supplement_obligation", "supplement_child_obligation":
            guard let supplement = event.supplement,
                  let item = outbox.kind == "supplement_obligation"
                    ? run.itemID : outbox.createdItemID else { return nil }
            var body: [String: Any] = [
                "operation": "obligation", "itemId": item,
                "title": supplement.title, "owner": supplement.owner,
                "blocking": supplement.required,
                "actorKind": supplement.actorKind == "assistant" ? "agent" : supplement.actorKind,
                "requiredAction": supplement.acceptance,
                "blockingScope": Self.supplementSourceScope(supplement),
            ]
            // Only checklist supplements have a counterpart on this same item. Child
            // supplements retain their source trail without implying a checklist relation.
            if outbox.kind == "supplement_obligation", let relation = supplement.relationID {
                body["supplementRelationId"] = relation
            }
            return body
        case "handoff":
            guard let item = run.itemID, let owner = event.handoffOwner,
                  let note = event.handoffNote else { return nil }
            return ["operation": "handoff", "itemId": item,
                    "owner": owner, "note": note]
        default: return nil
        }
    }

    private func finishOutbox(_ prepared: PreparedOutbox, status: String, code: String?,
                              createdItemID: String? = nil) {
        lock.lock(); defer { lock.unlock() }
        var draft = state
        guard let index = settlementIndex(prepared, draft: &draft) else {
            if persist(draft) { state = draft }
            return
        }
        let runID = prepared.runID
        draft.outbox[index].status = status
        draft.outbox[index].failureCode = code
        draft.outbox[index].updatedAt = now().timeIntervalSince1970
        if status == "complete", let createdItemID,
           let runIndex = draft.runs.firstIndex(where: { $0.id == runID }) {
            if draft.outbox[index].kind == "create" {
                draft.runs[runIndex].itemID = createdItemID
            } else if draft.outbox[index].kind == "supplement_child_create" {
                draft.outbox[index].createdItemID = createdItemID
            }
            materializeIntents(runIndex: runIndex, draft: &draft)
        }
        guard persist(draft) else { return }
        state = draft
    }

    private func retryOutbox(_ prepared: PreparedOutbox, terminalCode: String,
                             resetPrepared: Bool) {
        lock.lock(); defer { lock.unlock() }
        var draft = state
        guard let index = settlementIndex(prepared, draft: &draft) else {
            if persist(draft) { state = draft }
            return
        }
        if draft.outbox[index].attempts >= 3 {
            draft.outbox[index].status = "failed"
            draft.outbox[index].failureCode = terminalCode
        } else if resetPrepared {
            draft.outbox[index].status = "pending"
            draft.outbox[index].preparedRequestID = nil
            draft.outbox[index].preparedRevision = nil
            draft.outbox[index].failureCode = terminalCode
        } else {
            draft.outbox[index].status = "pending"
            draft.outbox[index].attempts += 1
            draft.outbox[index].failureCode = terminalCode
        }
        draft.outbox[index].updatedAt = now().timeIntervalSince1970
        guard persist(draft) else { return }
        state = draft
    }

    private func settlementIndex(_ prepared: PreparedOutbox, draft: inout State) -> Int? {
        guard let index = draft.outbox.firstIndex(where: { $0.id == prepared.id }) else {
            appendSettlementFailure("workflow_outbox_settlement_missing", prepared: prepared,
                                    draft: &draft)
            return nil
        }
        let row = draft.outbox[index]
        guard (row.version ?? 1) == prepared.version,
              row.runID == prepared.runID, row.eventID == prepared.eventID,
              row.kind == prepared.kind, row.status == "inflight" else {
            draft.outbox[index].status = "failed"
            draft.outbox[index].failureCode = "workflow_outbox_settlement_identity_mismatch"
            appendSettlementFailure("workflow_outbox_settlement_identity_mismatch",
                                    prepared: prepared, draft: &draft)
            return nil
        }
        return index
    }

    private func appendSettlementFailure(_ code: String, prepared: PreparedOutbox,
                                         draft: inout State) {
        var rows = draft.settlementFailures ?? []
        if rows.count >= 64 { rows.removeFirst(rows.count - 63) }
        rows.append(SettlementFailure(outboxID: prepared.id, code: code,
                                      at: now().timeIntervalSince1970))
        draft.settlementFailures = rows
    }

    // MARK: - Storage and small helpers

    private func load() {
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= limits.maximumBytes else {
                storageFailure = "workflow_store_too_large"; return
            }
            var decoded = try JSONDecoder().decode(State.self, from: data)
            guard [1, 2].contains(decoded.schemaVersion),
                  decoded.runs.count <= limits.runs,
                  decoded.receipts.count <= limits.receipts,
                  decoded.outbox.count <= limits.outbox,
                  decoded.runs.allSatisfy({ $0.events.count <= limits.eventsPerRun }) else {
                storageFailure = "workflow_store_invalid"; return
            }
            var migrated = decoded.schemaVersion == 1
            for index in decoded.receipts.indices where decoded.receipts[index].requestScope == nil {
                let candidates = decoded.runs.filter {
                    $0.id == decoded.receipts[index].runID
                        && $0.identity.actor == decoded.receipts[index].actor
                }
                guard candidates.count == 1 else {
                    storageFailure = "workflow_store_migration_failed"; return
                }
                decoded.receipts[index].requestScope = Self.receiptScope(
                    identity: candidates[0].identity, runID: candidates[0].id)
                migrated = true
            }
            guard decoded.receipts.allSatisfy({ receipt in
                guard let run = decoded.runs.first(where: { $0.id == receipt.runID }) else {
                    return false
                }
                return receipt.actor == run.identity.actor
                    && receipt.requestScope == Self.receiptScope(
                        identity: run.identity, runID: run.id)
            }) else {
                storageFailure = "workflow_store_invalid"; return
            }
            for index in decoded.outbox.indices {
                if decoded.outbox[index].version == nil {
                    decoded.outbox[index].version = 1; migrated = true
                }
                // A process may have exited after persisting admission and before settlement.
                // The prepared request id/body remain the at-least-once identity on restart.
                if decoded.outbox[index].status == "inflight" {
                    decoded.outbox[index].status = "pending"; migrated = true
                }
            }
            decoded.schemaVersion = 2
            if migrated, !persist(decoded) { return }
            state = decoded
        } catch {
            storageFailure = "workflow_store_invalid"
        }
    }

    private func persist(_ value: State) -> Bool {
        do {
            let parent = url.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true)
            let data = try JSONEncoder.sorted.encode(value)
            guard data.count <= limits.maximumBytes else { return false }
            try data.write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: url.path)
            try journalSynchronize(url)
            return true
        } catch {
            storageFailure = "workflow_persistence_failed"
            return false
        }
    }

    private func outcome(_ status: Int, _ code: String, runID: String? = nil,
                         authority: String = "assistant_attested") -> Outcome {
        let item = runID.flatMap { id in state.runs.first(where: { $0.id == id })?.itemID }
        return Outcome(status: status, code: code, authority: authority,
                       runID: runID, itemID: item, outboxPending: pendingCount(state))
    }

    private func pendingCount(_ value: State) -> Int {
        value.outbox.filter { $0.status == "pending" || $0.status == "inflight" }.count
    }

    private func valid(_ identity: Identity) -> Bool {
        bounded(identity.terminalID, 200)
            && ["claude", "codex"].contains(identity.provider)
            && StartPoints.sessionName(identity.conversationID) != nil
            && bounded(identity.projectID, 200)
            && StartPoints.usable(identity.projectPath)
            && bounded(identity.processGeneration, 300)
    }

    private func bounded(_ value: String, _ maximum: Int) -> Bool {
        !value.isEmpty && value.utf8.count <= maximum
    }

    private static func exactInt(_ raw: Any?) -> Int? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.intValue
        return number.doubleValue == Double(value) ? value : nil
    }

    private static func outboxProtectsRun(_ runID: String, in state: State) -> Bool {
        state.outbox.contains { $0.runID == runID && $0.status != "complete" }
    }

    private static func supplementSourceScope(_ supplement: Supplement) -> String {
        "workflow_run=\(supplement.sourceRunID);session=\(supplement.sourceSessionID);"
            + "disposition=\(supplement.required ? "required" : "optional")"
    }

    private static func receiptScope(identity: Identity, runID: String) -> String {
        identity.requestScope + "\u{0}" + runID
    }

    static func syncJournal(_ url: URL) throws {
        let handle = try FileHandle(forWritingTo: url)
        defer { try? handle.close() }
        try handle.synchronize()
    }

    private func boundedText(_ raw: Any?, _ maximum: Int) -> String? {
        guard let value = raw as? String else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return bounded(trimmed, maximum) ? trimmed : nil
    }

    private func optionalText(_ raw: Any?, _ maximum: Int) -> String? {
        guard let raw else { return nil }
        guard let value = raw as? String, value.utf8.count <= maximum else { return nil }
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    static func fingerprint(_ data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private static func digest(_ text: String) -> String {
        fingerprint(Data(text.utf8))
    }

    private static func wireText(original: String, run: Run, modeGap: String?) -> String {
        let object: [String: Any] = [
            "version": 1,
            "authority": "clawdline_metadata_not_user_authorization",
            "run_id": run.id,
            "board_epoch": run.epoch,
            "project_id": run.identity.projectID,
            "provider": run.identity.provider,
            "conversation_id": run.identity.conversationID,
            "terminal_id": run.identity.terminalID,
            "process_generation": run.identity.processGeneration,
            "input_kind": run.inputKind,
            "content_reference": run.contentReference,
            "coverage": StartPoints.workflowCoverage(
                boardEnabled: true, managedIngress: true,
                adapterHandshake: false).rawValue,
            "required_first_action": "begin",
            "helper": "clawdline-board-workflow <conversation-id> <stable-idempotency-key>",
            "mode_gap": modeGap ?? NSNull(),
        ]
        let data = (try? JSONSerialization.data(withJSONObject: object,
                                                options: [.sortedKeys])) ?? Data("{}".utf8)
        let metadata = String(data: data, encoding: .utf8) ?? "{}"
        let separator = original.isEmpty ? "" : "\n\n"
        return original + separator
            + "<clawdline-workflow version=\"1\" authority=\"metadata-not-user\">\n"
            + metadata + "\n</clawdline-workflow>"
    }

    private static func errorCode(_ body: [String: Any]) -> String? {
        (body["error"] as? [String: Any])?["code"] as? String
    }

    private static func sameState(_ left: State, _ right: State) -> Bool {
        guard let lhs = try? JSONEncoder.sorted.encode(left),
              let rhs = try? JSONEncoder.sorted.encode(right) else { return false }
        return lhs == rhs
    }

    private static func defaultURL() -> URL {
        if let override = ProcessInfo.processInfo.environment["CLAWDLINE_BOARD_WORKFLOW_STORE"],
           !override.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return URL(fileURLWithPath: override)
        }
        let support = FileManager.default.urls(for: .applicationSupportDirectory,
                                                in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support", isDirectory: true)
        return support.appendingPathComponent("Clawdline", isDirectory: true)
            .appendingPathComponent("project-board-workflow.json")
    }
}

private extension JSONEncoder {
    static var sorted: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }
}
