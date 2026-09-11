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
        var projectID: String? = nil

        var object: [String: Any] {
            [
                "status": status,
                "code": code,
                "authority": authority,
                "run_id": runID ?? NSNull(),
                "item_id": itemID ?? NSNull(),
                "outbox_pending": outboxPending,
                "project_id": projectID ?? NSNull(),
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
        var rootLanding: RootLanding? = nil
        var assignmentID: String? = nil
        var assignmentDecision: String? = nil
        var programBinding: ProgramBinding? = nil
        var document: WorkflowDocument? = nil
        var settledIntents: [SettledIntent]? = nil
    }

    private struct SettledIntent: Codable, Equatable {
        var id: String
        var kind: String
        var index: Int
        var createdItemID: String?
    }

    private struct SpanStart: Codable, Equatable {
        var outboxID: String
        var eventID: String
        var requestID: String
        var itemID: String
    }

    private struct RootLanding: Codable {
        let commit: String
        let targetCommit: String
        let target: String
        let at: Double
    }

    private struct ProgramBinding: Codable {
        var programItemID: String
        var programKey: String
        var planID: String
        var planVersion: Int
        var graphID: String
        var nodeID: String
    }

    private struct WorkflowDocument: Codable {
        var version: Int
        var documentID: String
        var documentVersion: Int
        var title: String
        var url: String
        var purpose: String
        var supersedesID: String?
    }

    private struct BindingReceipt: Codable {
        var receiptID: String
        var programItemID: String
        var programKey: String
        var planID: String
        var planVersion: Int
        var graphID: String
        var nodeID: String
        var effectiveItemID: String
        var runID: String
        var sessionID: String
        var provider: String
        var boardRevision: Int
        var settledAt: Double
        var processGeneration: String
        var requestedClassification: String
        var requestedItemID: String
        var resolution: String
        var settlementReplay: Bool
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
        var bindingReceipt: BindingReceipt? = nil
        var spanStart: SpanStart? = nil
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
        var schemaVersion = 3
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
    private let rootLandingCommand: ([String: Any], String) -> ProjectBoardStore.Reply
    private let journalSynchronize: (URL) throws -> Void
    private let limits: Limits
    private let autoStart: Bool
    private let helperPath: String?
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
         rootLandingCommand: @escaping ([String: Any], String) -> ProjectBoardStore.Reply = {
             let reply = ProjectBoardStore.shared.command($0, actor: $1, rootLandingOrigin: true)
             if reply.status == 200 { ProjectBoardIntegration.didMutate() }
             return reply
         },
         journalSynchronize: @escaping (URL) throws -> Void = ProjectBoardWorkflow.syncJournal,
         limits: Limits = Limits(), autoStart: Bool = true,
         bundleResources: URL? = Bundle.main.resourceURL) {
        self.url = url
        self.now = now
        self.boardHeader = boardHeader
        self.ensureProject = ensureProject
        self.boardCommand = boardCommand
        self.rootLandingCommand = rootLandingCommand
        self.journalSynchronize = journalSynchronize
        self.limits = limits
        self.autoStart = autoStart
        self.helperPath = Self.installedHelper(in: bundleResources)
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
                wireText: wireText(original: text, run: existing,
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
            runID: runID, wireText: wireText(original: text, run: run, modeGap: modeGap),
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
        let protectedEvents = Set(draft.outbox.filter {
            $0.runID == runID && $0.status != "complete"
        }.map(\.eventID)).union(draft.runs[runIndex].events.filter {
            !plannedIntentIDs($0, run: draft.runs[runIndex])
                .subtracting(Set(($0.settledIntents ?? []).map(\.id))).isEmpty
        }.map(\.id))
        while draft.runs[runIndex].events.count >= limits.eventsPerRun {
            guard let evictable = draft.runs[runIndex].events.firstIndex(where: {
                !protectedEvents.contains($0.id)
            }) else {
                let answer = outcome(429, "workflow_event_capacity", runID: runID)
                lock.unlock(); return answer
            }
            draft.runs[runIndex].events.remove(at: evictable)
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
        guard reservedOutboxCount(draft) <= limits.outbox else {
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

    /// Called only with the broker's immutable Git-verified delivery, never from semantic POST.
    /// Select the exact last managed ingress before that receipt, not the last *bound* item: a
    /// newer question/unbound run must not borrow yesterday's binding. The persisted event freezes
    /// that choice; replay never retargets it after another ingress or a restart.
    func recordRootLanding(_ delivery: Orchestrator.SessionDelivery) -> Outcome {
        guard let landing = delivery.landing,
              Orchestrator.isBrokerVerifiedSessionLanding(landing),
              landing.landedAt.timeIntervalSince1970.isFinite,
              let provider = delivery.identity.assistant?.rawValue,
              let conversation = delivery.identity.conversationID,
              let pid = delivery.identity.pid, let start = delivery.identity.processStart else {
            return Outcome(status: 409, code: "root_landing_unverified", authority: "broker_observed",
                           runID: nil, itemID: nil, outboxPending: 0)
        }
        let generation = "\(pid):\(start.timeIntervalSince1970)"
        let at = landing.landedAt.timeIntervalSince1970
        let source = [delivery.identity.terminalID, provider, conversation, generation,
                      landing.repositoryCommonDir, landing.target, landing.verifiedCommit,
                      String(at)].joined(separator: "\u{0}")
        let eventID = "root-landing-" + Self.digest(source)
        lock.lock()
        defer { lock.unlock() }
        if storageFailure != nil { return outcome(503, "workflow_persistence_failed") }
        let header = boardHeader()
        guard header.available, header.enabled, state.observedEnabled == true else {
            return outcome(409, "board_disabled")
        }
        if let prior = state.runs.first(where: { $0.events.contains { $0.id == eventID } }) {
            return outcome(200, "root_landing_already_recorded", runID: prior.id,
                           authority: "broker_observed")
        }
        let candidates = state.runs.indices.filter {
            let run = state.runs[$0]
            return run.identity.terminalID == delivery.identity.terminalID
                && run.identity.provider == provider && run.identity.conversationID == conversation
                && run.identity.processGeneration == generation && run.createdAt <= at
        }
        if candidates.isEmpty && !state.runs.contains(where: {
            $0.identity.conversationID == conversation && $0.identity.provider == provider
        }) {
            return outcome(200, "root_landing_unmanaged", authority: "broker_observed")
        }
        guard let latestTime = candidates.map({ state.runs[$0].createdAt }).max(),
              candidates.filter({ state.runs[$0].createdAt == latestTime }).count == 1,
              let index = candidates.first(where: { state.runs[$0].createdAt == latestTime }) else {
            return outcome(409, "root_landing_binding_unresolved")
        }
        let run = state.runs[index]
        guard run.epoch == state.enabledEpoch, run.delivery == "delivered",
              run.completion == "delivered", run.itemID != nil,
              ["new_work", "existing_item"].contains(run.classification ?? ""),
              run.events.contains(where: { $0.operation == "deliver" && $0.at <= at }) else {
            return outcome(409, "root_landing_binding_unresolved", runID: run.id)
        }
        // Git identity lookup can wait on a filesystem; never hold the ingress journal lock.
        lock.unlock()
        let commonDirectory = OrchestratorDraft.gitCommonDirectory(at: run.identity.projectPath)
        lock.lock()
        let currentHeader = boardHeader()
        guard currentHeader.available, currentHeader.enabled,
              commonDirectory == landing.repositoryCommonDir,
              let currentIndex = state.runs.firstIndex(where: { $0.id == run.id }),
              state.runs[currentIndex].identity == run.identity,
              state.runs[currentIndex].itemID == run.itemID,
              state.runs[currentIndex].epoch == state.enabledEpoch,
              state.runs[currentIndex].completion == "delivered", state.observedEnabled == true else {
            return outcome(409, "root_landing_binding_unresolved", runID: run.id)
        }
        if state.runs[currentIndex].events.contains(where: { $0.id == eventID }) {
            return outcome(200, "root_landing_already_recorded", runID: run.id, authority: "broker_observed")
        }
        guard state.runs[currentIndex].events.count < limits.eventsPerRun,
              reservedOutboxCount(state) < limits.outbox else {
            return outcome(429, "workflow_outbox_full", runID: run.id)
        }
        var draft = state
        let event = Event(id: eventID, operation: "broker_root_landing", at: at,
            outputs: [], remaining: [], rootLanding: RootLanding(commit: landing.verifiedCommit,
                targetCommit: landing.verifiedTargetCommit, target: landing.target, at: at))
        draft.runs[currentIndex].events.append(event)
        appendIntent(id: eventID, run: run, event: event, kind: "record_root_landing", index: 0,
                     draft: &draft)
        guard persist(draft) else { return outcome(503, "workflow_persistence_failed", runID: run.id) }
        state = draft
        // Schedule without executing a Board command under this journal lock.
        if autoStart { workerQueue.async { [weak self] in self?.kick() } }
        return outcome(202, "root_landing_recorded", runID: run.id, authority: "broker_observed")
    }

    func snapshot(runID: String) -> [String: Any]? {
        lock.lock(); defer { lock.unlock() }
        guard let run = state.runs.first(where: { $0.id == runID }) else { return nil }
        let rows = state.outbox.filter { $0.runID == runID }
        var answer: [String: Any] = [
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
        if let receipt = run.bindingReceipt {
            answer["binding"] = bindingObject(receipt)
        } else if let binding = run.events.first(where: {
            $0.operation == "begin" && $0.programBinding != nil
        }), let programBinding = binding.programBinding {
            answer["binding"] = ["status": "pending",
                "program_item_id": programBinding.programItemID,
                "program_key": programBinding.programKey, "plan_id": programBinding.planID,
                "plan_version": programBinding.planVersion, "graph_id": programBinding.graphID,
                "node_id": programBinding.nodeID,
                "requested_classification": binding.classification ?? NSNull(),
                "requested_item_id": binding.title ?? NSNull(),
                "resolution": ProjectBoardProgramPlan.reusedNodeResolution,
                "settlement_replay": NSNull(),
                "authority": "advisory_only"] as [String: Any]
        }
        return answer
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
                                   "summary", "owner", "phase", "program_binding"])
        case "document":
            fields = common.union(["version", "document_id", "document_version", "title",
                                   "url", "purpose", "supersedes_id"])
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
        case "assignment_decision":
            fields = common.union(["assignment_id", "decision", "note"])
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
                if let bindingObject = body["program_binding"] as? [String: Any] {
                    let exact = Set(["program_item_id", "program_key", "plan_id",
                                     "plan_version", "graph_id", "node_id"])
                    guard Set(bindingObject.keys) == exact,
                          let programItem = boundedText(bindingObject["program_item_id"], 200),
                          programItem == item,
                          let programKey = boundedText(bindingObject["program_key"], 64),
                          let planID = boundedText(bindingObject["plan_id"], 80),
                          let planVersion = Self.exactInt(bindingObject["plan_version"]),
                          planVersion > 0,
                          let graphID = boundedText(bindingObject["graph_id"], 80),
                          let nodeID = boundedText(bindingObject["node_id"], 80) else {
                        throw WorkflowRefusal(status: 400, code: "workflow_program_binding_invalid")
                    }
                    result.programBinding = ProgramBinding(
                        programItemID: programItem, programKey: programKey,
                        planID: planID, planVersion: planVersion,
                        graphID: graphID, nodeID: nodeID)
                } else if body["program_binding"] != nil {
                    throw WorkflowRefusal(status: 400, code: "workflow_program_binding_invalid")
                }
            } else if classification == "new_work" {
                guard body["item_id"] == nil, body["program_binding"] == nil,
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
                      body["type"] == nil, body["program_binding"] == nil else {
                    throw WorkflowRefusal(status: 400, code: "workflow_nonwork_has_item")
                }
            }
        case "document":
            guard run.itemID != nil else {
                throw WorkflowRefusal(status: 409, code: "workflow_begin_required")
            }
            guard let version = Self.exactInt(body["version"]) else {
                throw WorkflowRefusal(status: 400, code: "workflow_document_invalid")
            }
            guard version == 1 else {
                throw WorkflowRefusal(status: 400, code: "workflow_document_version_unsupported")
            }
            guard let documentID = boundedText(body["document_id"], 200),
                  let documentVersion = Self.exactInt(body["document_version"]),
                  documentVersion > 0,
                  let title = boundedText(body["title"], 300),
                  let url = boundedText(body["url"], 2_000),
                  let purpose = boundedText(body["purpose"], 32),
                  ["plan", "decision", "reference"].contains(purpose),
                  let components = URLComponents(string: url),
                  components.scheme?.lowercased() == "https",
                  components.host?.lowercased() == "app.clawdline.com" else {
                throw WorkflowRefusal(status: 400, code: "workflow_document_invalid")
            }
            let supersedes = optionalText(body["supersedes_id"], 200)
            if body["supersedes_id"] != nil && supersedes == nil {
                throw WorkflowRefusal(status: 400, code: "workflow_document_invalid")
            }
            result.document = WorkflowDocument(
                version: version, documentID: documentID, documentVersion: documentVersion,
                title: title, url: url, purpose: purpose, supersedesID: supersedes)
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
        case "assignment_decision":
            guard run.itemID != nil,
                  let assignmentID = boundedText(body["assignment_id"], 200),
                  let decision = boundedText(body["decision"], 32),
                  ["accepted", "declined"].contains(decision),
                  let note = boundedText(body["note"], 1_000) else {
                throw WorkflowRefusal(status: 400, code: "workflow_assignment_invalid")
            }
            result.assignmentID = assignmentID; result.assignmentDecision = decision
            result.summary = note
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
            if event.classification == "existing_item", event.programBinding == nil {
                run.itemID = event.title
            }
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
            if event.operation == "begin", event.programBinding != nil,
               run.itemID == nil {
                appendIntent(id: "\(event.id)-program-binding", run: run, event: event,
                             kind: "program_binding", index: 0, draft: &draft)
                continue
            }
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
                             kind: "record_output", index: index, draft: &draft)
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
                    })?.createdItemID ?? event.settledIntents?.first(where: {
                        $0.id == createID && $0.kind == "supplement_child_create"
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
            if event.operation == "assignment_decision" {
                appendIntent(id: "\(event.id)-assignment", run: run, event: event,
                             kind: "decide_session_assignment", index: 0, draft: &draft)
            }
            if event.operation == "document", event.document != nil {
                appendIntent(id: "\(event.id)-document", run: run, event: event,
                             kind: "document_reference", index: 0, draft: &draft)
            }
        }
    }

    private func appendIntent(id: String, run: Run, event: Event, kind: String, index: Int,
                              draft: inout State, targetItemID: String? = nil) {
        guard !draft.outbox.contains(where: { $0.id == id }),
              !(event.settledIntents ?? []).contains(where: { $0.id == id }) else { return }
        draft.outbox.append(Outbox(
            id: id, version: 1, runID: run.id, eventID: event.id, kind: kind, index: index,
            status: "pending", attempts: 0, preparedRequestID: nil,
            preparedRevision: nil, failureCode: nil, createdItemID: targetItemID,
            updatedAt: now().timeIntervalSince1970))
    }

    /// Reserve deferred dependency fanout at semantic admission, not after a create has already
    /// succeeded. Completed identities live with their bounded source event, not in the queue.
    private func plannedIntentIDs(_ event: Event, run: Run) -> Set<String> {
        if event.operation == "broker_root_landing" { return [event.id] }
        var ids = Set<String>()
        if event.operation == "begin", event.programBinding != nil { ids.insert("\(event.id)-program-binding") }
        else if event.operation == "begin", event.classification == "new_work" { ids.insert("\(event.id)-create") }
        let willBind = run.itemID != nil || run.events.contains {
            $0.operation == "begin" && ($0.classification == "new_work" || $0.programBinding != nil)
        }
        guard willBind else { return ids }
        if event.operation == "begin", ["new_work", "existing_item"].contains(event.classification ?? "") {
            ids.formUnion(["\(event.id)-link", "\(event.id)-span"])
        }
        if ["deliver", "handoff"].contains(event.operation) { ids.insert("\(event.id)-end-span") }
        for i in event.outputs.indices { ids.insert("\(event.id)-artifact-\(i)") }
        for i in event.remaining.indices { ids.insert("\(event.id)-obligation-\(i)") }
        if let supplement = event.supplement {
            let suffixes = supplement.kind == "checklist"
                ? ["supplement-checklist", "supplement-obligation"]
                : ["supplement-child-create", "supplement-child-link", "supplement-child-obligation"]
            ids.formUnion(suffixes.map { "\(event.id)-\($0)" })
        }
        if event.operation == "handoff" { ids.insert("\(event.id)-handoff") }
        if event.operation == "assignment_decision" { ids.insert("\(event.id)-assignment") }
        if event.operation == "document", event.document != nil { ids.insert("\(event.id)-document") }
        return ids
    }

    private func reservedOutboxCount(_ value: State) -> Int {
        var ids = Set(value.outbox.filter { $0.status != "complete" }.map(\.id))
        for run in value.runs {
            for event in run.events {
                let settled = Set((event.settledIntents ?? []).map(\.id))
                ids.formUnion(plannedIntentIDs(event, run: run).subtracting(settled))
            }
        }
        return ids.count
    }

    private func settledID(_ entry: SettledIntent, eventID: String) -> String? {
        let suffix: String
        switch entry.kind {
        case "record_root_landing": suffix = ""
        case "record_output": suffix = "-artifact-\(entry.index)"
        case "obligation": suffix = "-obligation-\(entry.index)"
        case "program_binding": suffix = "-program-binding"
        case "end_span": suffix = "-end-span"
        case "supplement_checklist": suffix = "-supplement-checklist"
        case "supplement_obligation": suffix = "-supplement-obligation"
        case "supplement_child_create": suffix = "-supplement-child-create"
        case "supplement_child_link": suffix = "-supplement-child-link"
        case "supplement_child_obligation": suffix = "-supplement-child-obligation"
        case "decide_session_assignment": suffix = "-assignment"
        case "document_reference": suffix = "-document"
        case "create", "link", "span", "handoff": suffix = "-\(entry.kind)"
        default: return nil
        }
        guard entry.index >= 0 else { return nil }
        if !["record_output", "obligation"].contains(entry.kind), entry.index != 0 { return nil }
        return eventID + suffix
    }

    private func validSettled(_ entry: SettledIntent, event: Event, run: Run) -> Bool {
        guard entry.id == settledID(entry, eventID: event.id),
              plannedIntentIDs(event, run: run).contains(entry.id) else { return false }
        return entry.kind != "supplement_child_create" || boundedText(entry.createdItemID, 200) != nil
    }

    /// Atomic with settlement/migration. A tombstone retains the exact dependency result so
    /// replay cannot create another child or retarget its link. Unknown identities fail closed.
    private func compactCompleted(_ draft: inout State) -> Bool {
        var settledIDs = Set<String>()
        for run in draft.runs {
            for event in run.events {
                for entry in event.settledIntents ?? [] {
                    guard settledIDs.insert(entry.id).inserted,
                          validSettled(entry, event: event, run: run) else { return false }
                }
            }
        }
        var seen = Set<String>()
        for row in draft.outbox {
            guard seen.insert(row.id).inserted else { return false }
            guard row.status == "complete" else {
                guard !settledIDs.contains(row.id) else { return false }
                continue
            }
            guard row.version == nil || row.version == 1,
                  let r = draft.runs.firstIndex(where: { $0.id == row.runID }) else { return false }
            let entry = SettledIntent(id: row.id, kind: row.kind, index: row.index, createdItemID: row.createdItemID)
            guard row.id == settledID(entry, eventID: row.eventID) else { return false }
            if row.kind == "span" {
                guard let item = draft.runs[r].itemID,
                      let request = boundedText(row.preparedRequestID, 300) else { return false }
                let start = SpanStart(outboxID: row.id, eventID: row.eventID, requestID: request, itemID: item)
                if let prior = draft.runs[r].spanStart, prior != start { return false }
                draft.runs[r].spanStart = start
            }
            // Legacy event retention already removed this completed source. With no event to
            // rematerialize, its payload can leave; preserve a span's exact close identity above.
            guard let e = draft.runs[r].events.firstIndex(where: { $0.id == row.eventID }) else { continue }
            guard validSettled(entry, event: draft.runs[r].events[e], run: draft.runs[r]) else { return false }
            var settled = draft.runs[r].events[e].settledIntents ?? []
            if let old = settled.first(where: { $0.id == row.id }) {
                guard old == entry else { return false }
            } else { settled.append(entry) }
            draft.runs[r].events[e].settledIntents = settled
        }
        draft.outbox.removeAll { $0.status == "complete" }
        return true
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
            let reply = prepared.kind == "record_root_landing"
                ? rootLandingCommand(command, prepared.actor) : boardCommand(command, prepared.actor)
            let code = Self.errorCode(reply.body)
            if reply.status == 200 {
                finishOutbox(prepared, status: "complete", code: nil,
                             createdItemID: reply.body["itemId"] as? String,
                             replyBody: reply.body)
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
            draft.outbox[index].failureCode = !header.available ? "board_unavailable"
                : (!header.enabled ? "board_disabled" : "workflow_epoch_stale")
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
        case "program_binding":
            guard let binding = event.programBinding else { return nil }
            return ["operation": "program_binding", "projectId": run.identity.projectID,
                    "programItemId": binding.programItemID,
                    "programKey": binding.programKey, "planId": binding.planID,
                    "planVersion": binding.planVersion, "graphId": binding.graphID,
                    "nodeId": binding.nodeID, "runId": run.id,
                    "sessionId": run.identity.conversationID,
                    "provider": run.identity.provider,
                    "processGeneration": run.identity.processGeneration,
                    "requestedClassification": event.classification ?? "existing_item",
                    "requestedItemId": event.title ?? binding.programItemID]
        case "document_reference":
            guard let item = run.itemID, let document = event.document else { return nil }
            var body: [String: Any] = ["operation": "document_reference", "itemId": item,
                "documentId": document.documentID, "version": document.documentVersion,
                "title": document.title, "url": document.url, "purpose": document.purpose]
            if let predecessor = document.supersedesID { body["supersedesId"] = predecessor }
            return body
        case "decide_session_assignment":
            guard let item = run.itemID, let assignmentID = event.assignmentID,
                  let decision = event.assignmentDecision, let note = event.summary else { return nil }
            return ["operation": "decide_session_assignment", "itemId": item,
                    "projectId": run.identity.projectID, "assignmentId": assignmentID,
                    "decision": decision, "note": note]
        case "record_root_landing":
            guard let item = run.itemID, let landing = event.rootLanding else { return nil }
            return ["operation": "record_root_landing", "itemId": item,
                    "projectId": run.identity.projectID, "runId": run.id,
                    "sessionId": run.identity.conversationID, "commit": landing.commit,
                    "targetCommit": landing.targetCommit, "target": landing.target,
                    "landedAt": landing.at]
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
                  let start = run.spanStart, start.itemID == item,
                  start.outboxID == start.eventID + "-span" else { return nil }
            return ["operation": "end_span", "itemId": item,
                    "sessionId": run.identity.conversationID,
                    "startRequestId": start.requestID,
                    "note": event.summary ?? event.handoffNote ?? "Session interval ended."]
        case "artifact", "record_output":
            guard let item = run.itemID, event.outputs.indices.contains(outbox.index) else {
                return nil
            }
            let output = event.outputs[outbox.index]
            return ["operation": outbox.kind, "itemId": item, "title": output.title,
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
                              createdItemID: String? = nil,
                              replyBody: [String: Any]? = nil) {
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
        let runIndex = draft.runs.firstIndex(where: { $0.id == runID })
        let settledBinding = (replyBody?["programBindingReceipt"] as? [String: Any])
            .flatMap(bindingReceipt)
        if status == "complete", draft.outbox[index].kind == "program_binding" {
            guard let runIndex, let settledBinding,
                  bindingReceiptMatches(settledBinding, run: draft.runs[runIndex]) else {
                draft.outbox[index].status = "failed"
                draft.outbox[index].failureCode = "workflow_binding_receipt_invalid"
                guard persist(draft) else { return }
                state = draft
                return
            }
        }
        if status == "complete", let runIndex {
            if draft.outbox[index].kind == "program_binding",
               let receipt = settledBinding {
                draft.runs[runIndex].itemID = receipt.effectiveItemID
                draft.runs[runIndex].bindingReceipt = receipt
            } else if let createdItemID {
                if draft.outbox[index].kind == "create" {
                    draft.runs[runIndex].itemID = createdItemID
                } else if draft.outbox[index].kind == "supplement_child_create" {
                    draft.outbox[index].createdItemID = createdItemID
                }
            }
            guard compactCompleted(&draft) else {
                storageFailure = "workflow_store_invalid"; return
            }
            materializeIntents(runIndex: runIndex, draft: &draft)
        }
        guard reservedOutboxCount(draft) <= limits.outbox else {
            storageFailure = "workflow_outbox_reservation_invalid"; return
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
            guard [1, 2, 3].contains(decoded.schemaVersion),
                  decoded.runs.count <= limits.runs,
                  decoded.receipts.count <= limits.receipts,
                  decoded.runs.allSatisfy({ $0.events.count <= limits.eventsPerRun }) else {
                storageFailure = "workflow_store_invalid"; return
            }
            var migrated = decoded.schemaVersion != 3 || decoded.outbox.contains { $0.status == "complete" }
            // Legacy producers could write 514 rows after deferred create fanout. The byte cap
            // still bounds decoding; compact proven completions BEFORE validating live capacity.
            guard compactCompleted(&decoded), reservedOutboxCount(decoded) <= limits.outbox else {
                storageFailure = "workflow_store_invalid"; return
            }
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
            }), decoded.runs.allSatisfy({ run in
                run.bindingReceipt.map { bindingReceiptMatches($0, run: run) } ?? true
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
            decoded.schemaVersion = 3
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
                       runID: runID, itemID: item, outboxPending: pendingCount(state),
                       projectID: runID.flatMap { id in state.runs.first { $0.id == id }?.identity.projectID })
    }

    private func pendingCount(_ value: State) -> Int {
        value.outbox.filter { $0.status == "pending" || $0.status == "inflight" }.count
    }

    private func bindingReceipt(_ object: [String: Any]) -> BindingReceipt? {
        guard let receiptID = boundedText(object["receiptId"], 80),
              UUID(uuidString: receiptID) != nil,
              let programItemID = boundedText(object["programItemId"], 200),
              let programKey = boundedText(object["programKey"], 64),
              let planID = boundedText(object["planId"], 80),
              let planVersion = Self.exactInt(object["planVersion"]), planVersion > 0,
              let graphID = boundedText(object["graphId"], 80),
              let nodeID = boundedText(object["nodeId"], 80),
              let effectiveItemID = boundedText(object["effectiveItemId"], 200),
              let runID = boundedText(object["runId"], 200),
              let sessionID = boundedText(object["sessionId"], 200),
              let provider = boundedText(object["provider"], 32),
              ["codex", "claude"].contains(provider),
              let boardRevision = Self.exactInt(object["boardRevision"]), boardRevision >= 0,
              let settledAt = (object["settledAt"] as? NSNumber)?.doubleValue,
              settledAt.isFinite, settledAt >= 0,
              let processGeneration = boundedText(object["processGeneration"], 300),
              object["requestedClassification"] as? String == "existing_item",
              let requestedItemID = boundedText(object["requestedItemId"], 200),
              object["resolution"] as? String == ProjectBoardProgramPlan.reusedNodeResolution,
              let settlementReplay = object["replay"] as? Bool,
              object["authority"] as? String == "advisory_only" else { return nil }
        return BindingReceipt(receiptID: receiptID,
            programItemID: programItemID, programKey: programKey,
            planID: planID, planVersion: planVersion, graphID: graphID, nodeID: nodeID,
            effectiveItemID: effectiveItemID, runID: runID, sessionID: sessionID,
            provider: provider, boardRevision: boardRevision,
            settledAt: settledAt, processGeneration: processGeneration,
            requestedClassification: "existing_item", requestedItemID: requestedItemID,
            resolution: ProjectBoardProgramPlan.reusedNodeResolution,
            settlementReplay: settlementReplay)
    }

    private func bindingObject(_ receipt: BindingReceipt) -> [String: Any] {
        ["status": "settled", "receipt_id": receipt.receiptID,
         "program_item_id": receipt.programItemID,
         "program_key": receipt.programKey, "plan_id": receipt.planID,
         "plan_version": receipt.planVersion, "graph_id": receipt.graphID,
         "node_id": receipt.nodeID, "effective_item_id": receipt.effectiveItemID,
         "run_id": receipt.runID, "session_id": receipt.sessionID,
         "provider": receipt.provider,
         "board_revision": receipt.boardRevision, "settled_at": receipt.settledAt,
         "requested_classification": receipt.requestedClassification,
         "requested_item_id": receipt.requestedItemID,
         "resolution": receipt.resolution,
         "settlement_replay": receipt.settlementReplay,
         "process": ["generation": receipt.processGeneration] as [String: Any],
         "authority": "advisory_only"]
    }

    private func bindingReceiptMatches(_ receipt: BindingReceipt, run: Run) -> Bool {
        guard UUID(uuidString: receipt.receiptID) != nil,
              receipt.runID == run.id, receipt.sessionID == run.identity.conversationID,
              receipt.provider == run.identity.provider,
              receipt.processGeneration == run.identity.processGeneration,
              receipt.boardRevision >= 0,
              receipt.settledAt.isFinite, receipt.settledAt >= 0,
              let requested = run.events.first(where: {
                  $0.operation == "begin" && $0.programBinding != nil
              })?.programBinding else { return false }
        return receipt.programItemID == requested.programItemID
            && receipt.programKey == requested.programKey
            && receipt.planID == requested.planID
            && receipt.planVersion == requested.planVersion
            && receipt.graphID == requested.graphID
            && receipt.nodeID == requested.nodeID
            && receipt.requestedClassification == "existing_item"
            && receipt.requestedItemID == receipt.programItemID
            && receipt.resolution == ProjectBoardProgramPlan.reusedNodeResolution
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

    // Resolve once at process bootstrap, never through PATH or the source checkout on a send.
    private static func installedHelper(in resources: URL?) -> String? {
        guard let resources, resources.isFileURL else { return nil }
        let helper = resources.appendingPathComponent("clawdline-board-workflow")
        let path = helper.path
        guard path.utf8.count <= 4096,
              !path.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }),
              let values = try? helper.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey]),
              values.isRegularFile == true, values.isSymbolicLink != true,
              FileManager.default.isExecutableFile(atPath: path) else { return nil }
        return path
    }

    private func wireText(original: String, run: Run, modeGap: String?) -> String {
        var object: [String: Any] = [
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
            "mode_gap": modeGap ?? (helperPath == nil ? "helper_unavailable" : nil) ?? NSNull(),
        ]
        if let helperPath { object["helper_path"] = helperPath }
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
