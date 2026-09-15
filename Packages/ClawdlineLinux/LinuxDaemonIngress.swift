import Foundation
import ClawdlineApplication

typealias LinuxIngressOperation = HeadlessApplicationOperation

struct LinuxIngressRequest: Codable, Equatable {
    let authorization: String?
    let operation: LinuxIngressOperation
    let commandID: String
    let taskID: String
    let sessionID: String?
    let projectRoot: String?
    let assistant: String?
    let model: String?
    let text: String?
    let acknowledge: Bool
    let authorizeRecovery: Bool
    let taskSecret: String?
    let title: String?
    let claims: [String]?
    let resultBase64: String?
    let resultDigest: String?
    let documentScope: HeadlessDocumentScope?
    let relativePath: String?

    init(authorization: String? = nil, operation: LinuxIngressOperation,
         commandID: String, taskID: String, sessionID: String? = nil,
         projectRoot: String? = nil, assistant: Assistant? = nil, model: String? = nil,
         text: String? = nil,
         acknowledge: Bool = false, authorizeRecovery: Bool = false,
         taskSecret: String? = nil, title: String? = nil, claims: [String]? = nil,
         resultBase64: String? = nil, resultDigest: String? = nil,
         documentScope: HeadlessDocumentScope? = nil, relativePath: String? = nil) {
        self.authorization = authorization
        self.operation = operation
        self.commandID = commandID
        self.taskID = taskID
        self.sessionID = sessionID
        self.projectRoot = projectRoot
        self.assistant = assistant?.rawValue
        self.model = model
        self.text = text
        self.acknowledge = acknowledge
        self.authorizeRecovery = authorizeRecovery
        self.taskSecret = taskSecret
        self.title = title
        self.claims = claims
        self.resultBase64 = resultBase64
        self.resultDigest = resultDigest
        self.documentScope = documentScope
        self.relativePath = relativePath
    }

    func sealedBytes() throws -> Data {
        if [.create, .send, .observe, .close].contains(operation), model == nil {
            // Schema 2 encoded the complete legacy request with authorization removed and
            // authorizeRecovery pinned false. Keep those exact bytes stable: a schema-2 queue
            // row may be either a succeeded replay or an interrupted operation awaiting explicit
            // recovery when the schema-3 image first opens it.
            let legacy = SchemaTwoSealed(
                operation: operation, commandID: commandID, taskID: taskID,
                sessionID: sessionID, projectRoot: projectRoot, assistant: assistant,
                text: text, acknowledge: acknowledge)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            return try encoder.encode(legacy)
        }
        // The daemon needs stable replay identity, never the plaintext task secret. Its digest
        // binds create/result/message recovery to the same capability without persisting it.
        let sealed = Sealed(
            schemaVersion: 3,
            operation: operation, commandID: commandID, taskID: taskID,
            sessionID: sessionID, projectRoot: projectRoot, assistant: assistant,
            model: model, text: text, acknowledge: acknowledge,
            taskSecretDigest: taskSecret.map(Self.secretDigest), title: title,
            claims: claims, resultBase64: resultBase64, resultDigest: resultDigest,
            documentScope: documentScope, relativePath: relativePath)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(sealed)
    }

    static func secretDigest(_ secret: String) -> String {
        LinuxSHA256.hex(Data(secret.utf8))
    }

    /// Relay callers do not choose durable idempotency identity. The authenticated sender and
    /// envelope sequence are stable across an exact replay and distinct for a new attempt.
    func reboundForCloud(sender: String, sequence: UInt64) -> LinuxIngressRequest {
        let commandID = LinuxSHA256.hex(Data("cloud:\(sender):\(sequence)".utf8))
        return LinuxIngressRequest(
            operation: operation, commandID: commandID, taskID: taskID,
            sessionID: sessionID, projectRoot: projectRoot,
            assistant: assistant.flatMap(Assistant.init(rawValue:)), model: model, text: text,
            acknowledge: acknowledge, authorizeRecovery: authorizeRecovery,
            taskSecret: taskSecret, title: title, claims: claims,
            resultBase64: resultBase64, resultDigest: resultDigest,
            documentScope: documentScope, relativePath: relativePath)
    }

    private struct Sealed: Codable {
        let schemaVersion: Int
        let operation: LinuxIngressOperation
        let commandID: String
        let taskID: String
        let sessionID: String?
        let projectRoot: String?
        let assistant: String?
        let model: String?
        let text: String?
        let acknowledge: Bool
        let taskSecretDigest: String?
        let title: String?
        let claims: [String]?
        let resultBase64: String?
        let resultDigest: String?
        let documentScope: HeadlessDocumentScope?
        let relativePath: String?
    }

    private struct SchemaTwoSealed: Encodable {
        let authorization: String? = nil
        let operation: LinuxIngressOperation
        let commandID: String
        let taskID: String
        let sessionID: String?
        let projectRoot: String?
        let assistant: String?
        let text: String?
        let acknowledge: Bool
        let authorizeRecovery = false
    }
}

struct LinuxIngressResponse: Codable, Equatable {
    let operation: LinuxIngressOperation
    let commandID: String
    let taskID: String
    let receipt: LinuxLifecycleReceipt?
    let task: LinuxTaskSnapshot?
    let board: LinuxBoardSnapshot?
    let session: LinuxSessionSnapshot?
    let documents: LinuxDocumentsSnapshot?
    let document: LinuxDocumentPayload?
    let resultReceipt: LinuxResultReceipt?

    init(operation: LinuxIngressOperation, commandID: String, taskID: String,
         receipt: LinuxLifecycleReceipt? = nil, task: LinuxTaskSnapshot? = nil,
         board: LinuxBoardSnapshot? = nil, session: LinuxSessionSnapshot? = nil,
         documents: LinuxDocumentsSnapshot? = nil, document: LinuxDocumentPayload? = nil,
         resultReceipt: LinuxResultReceipt? = nil) {
        self.operation = operation
        self.commandID = commandID
        self.taskID = taskID
        self.receipt = receipt
        self.task = task
        self.board = board
        self.session = session
        self.documents = documents
        self.document = document
        self.resultReceipt = resultReceipt
    }
}

struct LinuxTaskSnapshot: Codable, Equatable {
    let id: String
    let projectRoot: String?
    let sessionID: String?
    let title: String?
    let claims: [String]
    let state: LinuxDurableTaskState
    let createdAt: String?
    let messageCount: Int
    let resultDigest: String?
    let resultBase64: String?
    let resultPublishedAt: String?
    let resultAcknowledgedAt: String?
}

struct LinuxBoardItemSnapshot: Codable, Equatable {
    let taskID: String
    let sessionID: String?
    let title: String
    let state: LinuxDurableTaskState
    let claims: [String]
    let hasResult: Bool
    let acknowledged: Bool
}

struct LinuxBoardSnapshot: Codable, Equatable {
    let schemaVersion: Int
    let authority: String
    let projectRoot: String
    let revision: UInt64
    let enabled: Bool
    let mode: String
    let canWrite: Bool
    let canManage: Bool
    let items: [LinuxBoardItemSnapshot]
}

struct LinuxSessionSnapshot: Codable, Equatable {
    let authority: String
    let projectRoot: String
    let sessionID: String
    let taskID: String
    let taskState: LinuxDurableTaskState
    let terminalState: LinuxDurableTerminalState
    let lastObservedAt: String?
}

struct LinuxResultReceipt: Codable, Equatable {
    let authority: String
    let taskID: String
    let projectRoot: String
    let sessionID: String
    let resultDigest: String
    let publishedAt: String
    let acknowledgedAt: String?
}

protocol LinuxLifecyclePerforming: AnyObject {
    func create(commandID: String, projectRoot: String, assistant: Assistant,
                model: String?, reasoningEffort: ReasoningEffort?, permission: Permission,
                additionalDirectory: String?, resumeSessionID: String?) throws -> LinuxLifecycleReceipt
    func send(commandID: String, sessionID: String, text: String) throws -> LinuxLifecycleReceipt
    func observe(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt
    func close(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt
}

extension LinuxProviderRuntime: LinuxLifecyclePerforming {}

enum LinuxIngressFaultPoint: String, Error {
    case afterQueueSeal
    case afterEffectBeforeReceipt
    case afterReceiptBeforeResponse
}

/// The daemon has exactly one instance of this owner. Its lock covers loading, queue sealing,
/// terminal effect dispatch and receipt persistence, so no second local ingress path can race the
/// durable ledger. Tests inject faults only at the three named crash boundaries.
final class LinuxDaemonIngressOwner {
    private let store: LinuxDurableStateStore
    private let runtime: any LinuxLifecyclePerforming
    private let documents: LinuxDocumentReader
    /// Retaining this value retains both Application stores and their single-writer locks for
    /// the daemon lifetime. Local-only fixtures omit it; production composition never does.
    private let durableCloud: LinuxDurableCloudRuntime?
    private let lock = NSLock()
    private var admissionOpen = false
    var faultInjection: ((LinuxIngressFaultPoint) throws -> Void)?

    init(store: LinuxDurableStateStore, runtime: any LinuxLifecyclePerforming,
         durableCloud: LinuxDurableCloudRuntime? = nil) {
        self.store = store
        self.runtime = runtime
        self.durableCloud = durableCloud
        self.documents = LinuxDocumentReader(tasksDirectory: store.tasksDirectory,
                                             expectedUID: geteuid())
    }

    func completeStartup(_ receipt: LinuxStartupReconciliationReceipt) {
        lock.lock()
        admissionOpen = receipt.authoritative && receipt.status == "complete"
        lock.unlock()
    }

    func observeInventory(_ inventory: LinuxTerminalInventoryEvidence) throws
        -> LinuxStartupReconciliationReceipt {
        lock.lock()
        defer { lock.unlock() }
        return try LinuxStartupReconciler.observe(store: store, inventory: inventory)
    }

    func perform(_ request: LinuxIngressRequest) throws -> Data {
        try perform(request, effectAuthorization: nil)
    }

    /// The authenticated Relay enters the same serialized owner as local ingress. Roster and
    /// write authority are re-read under this lock immediately before an effect; a refused gate
    /// releases the just-reserved row durably and never invokes the terminal runtime.
    func performCloud(
        _ request: LinuxIngressRequest, sender: String, sequence: UInt64,
        effectAuthorization: @escaping (_ requiresWriteGate: Bool) throws -> Bool
    ) throws -> Data {
        try perform(
            request.reboundForCloud(sender: sender, sequence: sequence),
            effectAuthorization: effectAuthorization)
    }

    /// Resolve the durable task that owns one live terminal without exposing task identity to a
    /// browser. The Relay authenticates the viewer before calling this method; the exact task id
    /// is then fed back through `performCloud`, which rechecks authorization at the effect edge.
    func taskIDForCloudSession(_ sessionID: String) throws -> String {
        lock.lock()
        defer { lock.unlock() }
        guard admissionOpen else {
            throw LinuxDurableStateFailure(code: "ingress_closed",
                                           message: "Startup reconciliation has not opened admission.")
        }
        let state = try authoritativeState()
        guard let terminal = state.terminals.first(where: {
            $0.id == sessionID && $0.state == .present
        }), let taskID = terminal.taskID,
              state.tasks.contains(where: { $0.id == taskID && $0.terminalID == sessionID }) else {
            throw LinuxDurableStateFailure(code: "session_not_found",
                                           message: "That Linux Session is no longer present.")
        }
        return taskID
    }

    /// Capture one live terminal for a paired Cloud viewer without manufacturing a durable
    /// command row. Transcript/screen reads are observations, not task mutations; they still run
    /// under the sole ingress lock and recheck viewer authority immediately before the effect.
    func observeCloudSession(
        _ sessionID: String, commandID: String,
        effectAuthorization: () throws -> Bool
    ) throws -> LinuxLifecycleReceipt {
        lock.lock()
        defer { lock.unlock() }
        guard admissionOpen else {
            throw LinuxDurableStateFailure(code: "ingress_closed",
                                           message: "Startup reconciliation has not opened admission.")
        }
        guard !sessionID.isEmpty, sessionID.utf8.count <= 512,
              !sessionID.unicodeScalars.contains(where: { $0.value < 0x20 || $0.value == 0x7f }),
              SessionLaunchPolicy.opaqueCommandID(commandID) == commandID else {
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "The Session observation identity is malformed.")
        }
        guard try effectAuthorization() else {
            throw LinuxDurableStateFailure(code: "cloud_read_refused",
                                           message: "The paired sender is no longer authorized for this read.")
        }
        return try runtime.observe(commandID: commandID, sessionID: sessionID)
    }

    private func perform(
        _ request: LinuxIngressRequest,
        effectAuthorization: ((_ requiresWriteGate: Bool) throws -> Bool)?
    ) throws -> Data {
        lock.lock()
        defer { lock.unlock() }
        guard admissionOpen else {
            throw LinuxDurableStateFailure(code: "ingress_closed",
                                           message: "Startup reconciliation has not opened admission.")
        }
        guard SessionLaunchPolicy.opaqueCommandID(request.commandID) == request.commandID,
              SessionLaunchPolicy.opaqueCommandID(request.taskID) == request.taskID else {
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "Ingress command and task IDs must be closed opaque identifiers.")
        }
        try validateShape(request)
        let sealed = try request.sealedBytes()
        let payloadDigest = LinuxSHA256.hex(sealed)
        var state = try authoritativeState()

        // Secret-bearing task operations authenticate before command lookup. Missing tasks,
        // missing secrets, wrong secrets and wrong exact identity all take the same digest compare
        // and return the same refusal, so command existence cannot become a capability oracle.
        try authenticateSecretBearingMutation(request, state: state)

        // Cloud authority is checked once before any task/artifact/queue mutation and, for writes,
        // again immediately before the terminal effect. The first check prevents an unpaired or
        // disabled sender from manufacturing durable task/file state; the second closes the race
        // where pairing, revocation or the protected write gate changes after queue sealing.
        if let effectAuthorization,
           try !effectAuthorization(!request.operation.isRead) {
            throw LinuxDurableStateFailure(
                code: "cloud_effect_refused",
                message: request.operation.isRead
                    ? "The paired sender is no longer authorized for this read."
                    : "The paired sender or protected write gate does not authorize this effect.")
        }

        // Reads consume the same authoritative task/session projection under this owner's lock,
        // but never enter the command queue and never persist a byte. A read capability therefore
        // cannot be promoted into dispatch, mutation, cleanup, or a wider filesystem operation.
        if request.operation.isRead {
            return try performRead(request, state: state)
        }

        if let existing = state.commands.firstIndex(where: { $0.id == request.commandID }) {
            guard let queued = state.queue.first(where: { $0.commandID == request.commandID }) else {
                throw LinuxDurableStateFailure(
                    code: "command_id_conflict",
                    message: "That command id is already bound to different request bytes.")
            }
            let sealedMatches = queued.sealedPayloadBase64 == sealed.base64EncodedString()
                || (queued.state == .complete && !queued.sealedPayloadRecoverable
                    && queued.payloadDigest == payloadDigest)
            guard queued.payloadDigest == payloadDigest, sealedMatches else {
                throw LinuxDurableStateFailure(
                    code: "command_id_conflict",
                    message: "That command id is already bound to different request bytes.")
            }
            if state.commands[existing].outcome == .succeeded,
               let encoded = state.commands[existing].responseBase64,
               let response = Data(base64Encoded: encoded) {
                if [.taskResult, .taskAcknowledge, .taskClose].contains(request.operation) {
                    try revalidatePublishedResult(for: request, state: state)
                }
                return response
            }
            guard request.authorizeRecovery,
                  state.commands[existing].outcome == .interrupted
                    || state.commands[existing].outcome == .unknown,
                  queued.sealedPayloadRecoverable,
                  queued.payloadDigest == payloadDigest else {
                throw LinuxDurableStateFailure(
                    code: "command_requires_reconciliation",
                    message: "A prior command boundary is unresolved; explicit matching recovery is required.")
            }
            state.commands[existing].stage = .accepted
            state.commands[existing].outcome = .pending
            state.commands[existing].evidenceDigest = nil
            state.commands[existing].acknowledgedAt = nil
            state.commands[existing].responseBase64 = nil
            if let queueIndex = state.queue.firstIndex(where: { $0.commandID == request.commandID }) {
                state.queue[queueIndex].state = .queued
            }
        } else {
            try prepareTask(for: request, state: &state)
            state.commands.append(.init(
                id: request.commandID, taskID: request.taskID, terminalID: request.sessionID,
                operation: request.operation.rawValue, stage: .accepted, outcome: .pending,
                evidenceDigest: nil, acknowledgedAt: nil))
            state.queue.append(.init(
                id: "queue-\(request.commandID)", taskID: request.taskID,
                commandID: request.commandID, state: .queued, payloadDigest: payloadDigest,
                sealedPayloadRecoverable: true,
                sealedPayloadBase64: sealed.base64EncodedString()))
            if request.operation == .taskMessage,
               let taskIndex = state.tasks.firstIndex(where: { $0.id == request.taskID }),
               let text = request.text {
                state.tasks[taskIndex].messages.append(.init(
                    requestID: request.commandID,
                    textDigest: LinuxSHA256.hex(Data(text.utf8)),
                    acceptedAt: timestamp(), deliveredReceiptDigest: nil))
            }
        }
        try store.save(state)
        try faultInjection?(.afterQueueSeal)

        if let effectAuthorization,
           try !effectAuthorization(true) {
            state = try authoritativeState()
            state.commands.removeAll { $0.id == request.commandID && $0.outcome == .pending }
            state.queue.removeAll { $0.commandID == request.commandID && $0.state == .queued }
            try store.save(state)
            throw LinuxDurableStateFailure(
                code: "cloud_effect_refused",
                message: "The paired sender or protected write gate no longer authorizes this effect.")
        }

        let receipt: LinuxLifecycleReceipt?
        do {
            receipt = try invoke(request)
        } catch {
            // Accepted+sealed is intentionally retained. Startup reconciliation will type it as
            // interrupted/unknown rather than guessing whether a terminal effect happened.
            throw error
        }
        try faultInjection?(.afterEffectBeforeReceipt)

        state = try authoritativeState()
        guard let commandIndex = state.commands.firstIndex(where: { $0.id == request.commandID }),
              let taskIndex = state.tasks.firstIndex(where: { $0.id == request.taskID }) else {
            throw LinuxDurableStateFailure(code: "ledger_identity_lost",
                                           message: "The serialized ledger lost its command identity.")
        }
        if [.taskAcknowledge, .taskClose].contains(request.operation) {
            try store.revalidatePublishedResult(state.tasks[taskIndex])
        }
        let now = timestamp()
        var resultReceipt: LinuxResultReceipt?
        if let receipt {
            state.commands[commandIndex].terminalID = receipt.sessionID ?? request.sessionID
            if let sessionID = receipt.sessionID ?? request.sessionID {
                state.tasks[taskIndex].terminalID = sessionID
            }
        }
        switch request.operation {
        case .create, .send, .observe:
            state.tasks[taskIndex].state = .working
        case .close:
            state.tasks[taskIndex].state = .complete
        case .taskCreate:
            state.tasks[taskIndex].state = .working
        case .taskMessage:
            if let messageIndex = state.tasks[taskIndex].messages.firstIndex(where: {
                $0.requestID == request.commandID
            }), let receipt {
                let bytes = try encodeReceipt(receipt)
                state.tasks[taskIndex].messages[messageIndex].deliveredReceiptDigest =
                    LinuxSHA256.hex(bytes)
            }
        case .taskResult:
            guard let resultBase64 = request.resultBase64,
                  let resultBytes = Data(base64Encoded: resultBase64),
                  let resultDigest = request.resultDigest,
                  let projectRoot = request.projectRoot,
                  let sessionID = request.sessionID else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "The result identity disappeared before persistence.")
            }
            try store.publishTaskResult(
                resultBytes, taskID: request.taskID, expectedDigest: resultDigest)
            state.tasks[taskIndex].state = .resultPublished
            state.tasks[taskIndex].resultDigest = resultDigest
            state.tasks[taskIndex].resultByteCount = resultBytes.count
            state.tasks[taskIndex].resultPublishedAt = now
            resultReceipt = LinuxResultReceipt(
                authority: "linux_daemon_local", taskID: request.taskID,
                projectRoot: projectRoot, sessionID: sessionID,
                resultDigest: resultDigest, publishedAt: now,
                acknowledgedAt: nil)
        case .taskAcknowledge:
            guard let projectRoot = request.projectRoot,
                  let sessionID = request.sessionID,
                  let resultDigest = state.tasks[taskIndex].resultDigest,
                  let publishedAt = state.tasks[taskIndex].resultPublishedAt else {
                throw LinuxDurableStateFailure(code: "result_not_published",
                                               message: "No authenticated result is available to acknowledge.")
            }
            state.tasks[taskIndex].state = .acknowledged
            state.tasks[taskIndex].resultAcknowledgedAt = now
            resultReceipt = LinuxResultReceipt(
                authority: "linux_daemon_local", taskID: request.taskID,
                projectRoot: projectRoot, sessionID: sessionID,
                resultDigest: resultDigest, publishedAt: publishedAt,
                acknowledgedAt: now)
        case .taskClose:
            state.tasks[taskIndex].state = .complete
        case .taskRead, .boardRead, .sessionRead, .documentsRead, .documentRead:
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "A read entered the mutation lane.")
        }
        let response = try encodeResponse(LinuxIngressResponse(
            operation: request.operation, commandID: request.commandID,
            taskID: request.taskID, receipt: receipt,
            task: try taskSnapshot(state.tasks[taskIndex]), resultReceipt: resultReceipt))
        let stage = receipt.map {
            Self.durableStage($0.progress.stages.last ?? .accepted)
        } ?? .observed
        let isAcknowledgement = request.operation == .taskAcknowledge || request.acknowledge
        state.commands[commandIndex].stage = isAcknowledgement ? .acknowledged : stage
        state.commands[commandIndex].outcome = .succeeded
        state.commands[commandIndex].evidenceDigest = LinuxSHA256.hex(response)
        state.commands[commandIndex].acknowledgedAt = isAcknowledgement ? now : nil
        state.commands[commandIndex].responseBase64 = response.base64EncodedString()
        if let queueIndex = state.queue.firstIndex(where: { $0.commandID == request.commandID }) {
            state.queue[queueIndex].state = .complete
            if request.operation == .taskResult {
                // The digest retains exact replay identity after publication; the up-to-2 MiB
                // result bytes now live once at the task root, not again in the global queue.
                state.queue[queueIndex].sealedPayloadRecoverable = false
                state.queue[queueIndex].sealedPayloadBase64 = nil
            }
        }
        if request.operation == .close, state.tasks[taskIndex].resultDigest == nil {
            // Legacy W4-2 close had no separately published result. Keep its compatibility
            // receipt as terminal evidence without weakening authenticated W4-3 task closure.
            state.tasks[taskIndex].resultDigest = LinuxSHA256.hex(response)
        }
        if isAcknowledgement,
           let evidence = state.commands[commandIndex].evidenceDigest,
           !state.tasks[taskIndex].acknowledgedEvidence.contains(evidence) {
            state.tasks[taskIndex].acknowledgedEvidence.append(evidence)
        }
        if (request.operation == .create || request.operation == .taskCreate),
           let sessionID = receipt?.sessionID {
            let evidenceDigest = LinuxSHA256.hex(response)
            if let terminalIndex = state.terminals.firstIndex(where: { $0.id == sessionID }) {
                let currentOwner = state.terminals[terminalIndex].taskID
                guard currentOwner == nil || currentOwner == request.taskID else {
                    // The terminal effect has happened, but a durable record already assigns the
                    // returned id to another task. Keep the sealed command unresolved rather than
                    // silently stealing that terminal or returning an untrustworthy receipt.
                    throw LinuxDurableStateFailure(
                        code: "terminal_identity_conflict",
                        message: "The created Linux Session is already assigned to another task.")
                }
                state.terminals[terminalIndex].taskID = request.taskID
                state.terminals[terminalIndex].state = .present
                state.terminals[terminalIndex].lastObservedAt = now
                state.terminals[terminalIndex].evidenceDigest = evidenceDigest
            } else {
                state.terminals.append(.init(
                    id: sessionID, taskID: request.taskID, state: .present,
                    lastObservedAt: now, evidenceDigest: evidenceDigest))
            }
        } else if (request.operation == .close || request.operation == .taskClose),
                  let sessionID = request.sessionID,
                  let terminalIndex = state.terminals.firstIndex(where: { $0.id == sessionID }) {
            state.terminals[terminalIndex].state = .missing
            state.terminals[terminalIndex].evidenceDigest = LinuxSHA256.hex(response)
        }
        try store.save(state)
        try faultInjection?(.afterReceiptBeforeResponse)
        return response
    }

    private func performRead(_ request: LinuxIngressRequest,
                             state: LinuxDurableState) throws -> Data {
        let task = try exactTask(for: request, state: state, unauthorized: false)
        switch request.operation {
        case .taskRead:
            return try encodeResponse(.init(
                operation: request.operation, commandID: request.commandID,
                taskID: request.taskID, task: try taskSnapshot(task, includeResult: true)))
        case .boardRead:
            let projectRoot = try required(request.projectRoot, "projectRoot")
            let items = state.tasks.filter { $0.projectRoot == projectRoot }.map { row in
                LinuxBoardItemSnapshot(
                    taskID: row.id, sessionID: row.terminalID,
                    title: row.title ?? row.id, state: row.state, claims: row.claims,
                    hasResult: row.resultDigest != nil,
                    acknowledged: row.resultAcknowledgedAt != nil)
            }.sorted { $0.taskID < $1.taskID }
            return try encodeResponse(.init(
                operation: request.operation, commandID: request.commandID,
                taskID: request.taskID,
                board: LinuxBoardSnapshot(
                    schemaVersion: 1, authority: "linux_task_projection",
                    projectRoot: projectRoot,
                    revision: state.daemonEpoch * 1_000_000 + UInt64(state.commands.count),
                    enabled: true, mode: "board", canWrite: false, canManage: false,
                    items: items)))
        case .sessionRead:
            let sessionID = try required(request.sessionID, "sessionID")
            guard let terminal = state.terminals.first(where: { $0.id == sessionID }) else {
                throw LinuxDurableStateFailure(code: "read_not_found",
                                               message: "No read exists for that exact identity.")
            }
            return try encodeResponse(.init(
                operation: request.operation, commandID: request.commandID,
                taskID: request.taskID,
                session: LinuxSessionSnapshot(
                    authority: "linux_daemon_local",
                    projectRoot: try required(request.projectRoot, "projectRoot"),
                    sessionID: sessionID, taskID: task.id, taskState: task.state,
                    terminalState: terminal.state,
                    lastObservedAt: terminal.lastObservedAt)))
        case .documentsRead:
            let snapshot = try documents.list(
                projectRoot: try required(request.projectRoot, "projectRoot"),
                sessionID: try required(request.sessionID, "sessionID"),
                taskID: task.id,
                scope: try required(request.documentScope, "documentScope"))
            return try encodeResponse(.init(
                operation: request.operation, commandID: request.commandID,
                taskID: request.taskID, documents: snapshot))
        case .documentRead:
            let payload = try documents.document(
                projectRoot: try required(request.projectRoot, "projectRoot"),
                sessionID: try required(request.sessionID, "sessionID"),
                taskID: task.id,
                scope: try required(request.documentScope, "documentScope"),
                relativePath: try required(request.relativePath, "relativePath"))
            return try encodeResponse(.init(
                operation: request.operation, commandID: request.commandID,
                taskID: request.taskID, document: payload))
        default:
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "A mutation entered the read-only lane.")
        }
    }

    private func prepareTask(for request: LinuxIngressRequest,
                             state: inout LinuxDurableState) throws {
        if request.operation == .taskCreate {
            guard !state.tasks.contains(where: { $0.id == request.taskID }) else {
                throw LinuxDurableStateFailure(code: "task_identity_conflict",
                                               message: "That task identity is already authoritative.")
            }
            let projectRoot = try required(request.projectRoot, "projectRoot")
            let secret = try required(request.taskSecret, "taskSecret")
            _ = try store.prepareTaskArtifacts(taskID: request.taskID)
            state.tasks.append(.init(
                id: request.taskID, terminalID: nil, state: .queued,
                resultDigest: nil, acknowledgedEvidence: [],
                projectRoot: projectRoot, title: request.title,
                claims: request.claims ?? [],
                secretDigest: LinuxIngressRequest.secretDigest(secret),
                createdAt: timestamp()))
            return
        }
        if !state.tasks.contains(where: { $0.id == request.taskID }) {
            guard request.operation == .create else {
                throw LinuxDurableStateFailure(code: "task_unauthorized",
                                               message: "Task authentication failed.")
            }
            state.tasks.append(.init(id: request.taskID, terminalID: request.sessionID,
                                     state: .queued, resultDigest: nil,
                                     acknowledgedEvidence: []))
        }
        guard let index = state.tasks.firstIndex(where: { $0.id == request.taskID }) else {
            throw LinuxDurableStateFailure(code: "task_unauthorized",
                                           message: "Task authentication failed.")
        }
        if [.send, .observe, .close].contains(request.operation) {
            guard state.tasks[index].terminalID == request.sessionID,
                  request.sessionID != nil else {
                throw LinuxDurableStateFailure(
                    code: "task_identity_mismatch",
                    message: "The command task and Session identity no longer match.")
            }
        }
        if [.taskMessage, .taskResult, .taskAcknowledge, .taskClose]
            .contains(request.operation) {
            try requireExactIdentity(state.tasks[index], request: request)
        }
        if [.taskAcknowledge, .taskClose].contains(request.operation) {
            try store.revalidatePublishedResult(state.tasks[index])
        }
        if request.operation == .taskResult,
           state.tasks[index].resultDigest != nil {
            throw LinuxDurableStateFailure(code: "result_already_published",
                                           message: "This task already has a durable result receipt.")
        }
        if request.operation == .taskAcknowledge {
            guard let expected = state.tasks[index].resultDigest,
                  expected == request.resultDigest,
                  state.tasks[index].resultPublishedAt != nil else {
                throw LinuxDurableStateFailure(code: "result_receipt_mismatch",
                                               message: "The acknowledgement does not name the published result receipt.")
            }
        }
        if request.operation == .taskClose,
           state.tasks[index].resultAcknowledgedAt == nil {
            throw LinuxDurableStateFailure(code: "result_not_acknowledged",
                                           message: "Task close requires the exact result receipt acknowledgement.")
        }
    }

    private func exactTask(for request: LinuxIngressRequest, state: LinuxDurableState,
                           unauthorized: Bool) throws -> LinuxDurableTaskRecord {
        guard let task = state.tasks.first(where: { $0.id == request.taskID }) else {
            throw LinuxDurableStateFailure(
                code: unauthorized ? "task_unauthorized" : "read_not_found",
                message: unauthorized ? "Task authentication failed."
                    : "No read exists for that exact identity.")
        }
        do {
            try requireExactIdentity(task, request: request)
        } catch {
            throw LinuxDurableStateFailure(code: "read_not_found",
                                           message: "No read exists for that exact identity.")
        }
        return task
    }

    private func requireExactIdentity(_ task: LinuxDurableTaskRecord,
                                      request: LinuxIngressRequest) throws {
        guard task.projectRoot == request.projectRoot,
              task.terminalID == request.sessionID,
              task.projectRoot != nil, task.terminalID != nil else {
            throw LinuxDurableStateFailure(code: "task_identity_mismatch",
                                           message: "Project, Session, and task identity must match exactly.")
        }
    }

    private func validateShape(_ request: LinuxIngressRequest) throws {
        func invalid(_ message: String) throws -> Never {
            throw LinuxDurableStateFailure(code: "invalid_ingress", message: message)
        }
        func validSession(_ value: String?) -> Bool {
            guard let value else { return false }
            return !value.isEmpty && value.utf8.count <= 512
                && !value.unicodeScalars.contains(where: { $0.value < 0x20 || $0.value == 0x7f })
        }
        func validProject(_ value: String?) -> Bool {
            value.map(ProjectRootPolicy.isLexicallySafeAbsolute) == true
        }
        func validClaims(_ values: [String]?) -> Bool {
            guard let values, values.count <= 256, Set(values).count == values.count else {
                return false
            }
            return values.allSatisfy { value in
                guard !value.isEmpty, value.utf8.count <= 512, !value.hasPrefix("/") else {
                    return false
                }
                let parts = value.split(separator: "/", omittingEmptySubsequences: false)
                return parts.allSatisfy { !$0.isEmpty && $0 != "." && $0 != ".." }
            }
        }
        func validDigest(_ value: String?) -> Bool {
            guard let value, value.count == 64 else { return false }
            return value.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }
        if request.operation != .create, request.model != nil {
            try invalid("Only create may carry a model.")
        }
        switch request.operation {
        case .taskCreate:
            guard validProject(request.projectRoot), request.sessionID == nil,
                  request.assistant.flatMap(Assistant.init(rawValue:)) != nil,
                  request.taskSecret?.isEmpty == false,
                  (request.taskSecret?.utf8.count ?? 4_097) <= 4_096,
                  request.title?.isEmpty == false,
                  (request.title?.utf8.count ?? 1_001) <= 1_000,
                  validClaims(request.claims), request.text == nil,
                  request.resultBase64 == nil, request.resultDigest == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  !request.acknowledge else {
                try invalid("Task create needs one bounded project, assistant, secret, title, and claims set.")
            }
        case .taskMessage:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.taskSecret?.isEmpty == false,
                  request.text?.isEmpty == false,
                  (request.text?.utf8.count ?? 65_537) <= 65_536,
                  request.title == nil, request.claims == nil,
                  request.resultBase64 == nil, request.resultDigest == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  request.assistant == nil, !request.acknowledge else {
                try invalid("Task message needs exact project, Session, task secret, and bounded text.")
            }
        case .taskResult:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.taskSecret?.isEmpty == false,
                  let encoded = request.resultBase64,
                  let bytes = Data(base64Encoded: encoded), !bytes.isEmpty,
                  bytes.count <= 2 * 1_048_576,
                  validDigest(request.resultDigest),
                  LinuxSHA256.hex(bytes) == request.resultDigest,
                  request.text == nil, request.title == nil, request.claims == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  request.assistant == nil, !request.acknowledge else {
                try invalid("Task result needs exact identity, authentication, bounded bytes, and their SHA-256.")
            }
        case .taskAcknowledge:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  validDigest(request.resultDigest), request.taskSecret == nil,
                  request.resultBase64 == nil, request.text == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  request.assistant == nil, request.title == nil, request.claims == nil,
                  !request.acknowledge else {
                try invalid("Task acknowledgement needs exact identity and result receipt digest.")
            }
        case .taskClose:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.taskSecret == nil, request.resultBase64 == nil,
                  request.resultDigest == nil, request.text == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  request.assistant == nil, request.title == nil, request.claims == nil,
                  !request.acknowledge else {
                try invalid("Task close needs exact project, Session, and task identity.")
            }
        case .taskRead, .boardRead, .sessionRead:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.taskSecret == nil, request.text == nil,
                  request.resultBase64 == nil, request.resultDigest == nil,
                  request.documentScope == nil, request.relativePath == nil,
                  request.assistant == nil, request.title == nil, request.claims == nil,
                  !request.acknowledge, !request.authorizeRecovery else {
                try invalid("The read needs exact project, Session, and task identity only.")
            }
        case .documentsRead:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.documentScope != nil, request.relativePath == nil,
                  request.taskSecret == nil, request.text == nil,
                  request.resultBase64 == nil, request.resultDigest == nil,
                  request.assistant == nil, request.title == nil, request.claims == nil,
                  !request.acknowledge, !request.authorizeRecovery else {
                try invalid("Document listing needs exact project, Session, task, and scope identity.")
            }
        case .documentRead:
            guard validProject(request.projectRoot), validSession(request.sessionID),
                  request.documentScope != nil,
                  request.relativePath.flatMap(HeadlessDocumentPathPolicy.relativePath) != nil,
                  request.taskSecret == nil, request.text == nil,
                  request.resultBase64 == nil, request.resultDigest == nil,
                  request.assistant == nil, request.title == nil, request.claims == nil,
                  !request.acknowledge, !request.authorizeRecovery else {
                try invalid("Document read needs exact identity, scope, and a bounded relative path.")
            }
        case .create:
            guard validProject(request.projectRoot),
                  request.assistant.flatMap(Assistant.init(rawValue:)) != nil,
                  request.model == nil || ["haiku", "sonnet", "opus"].contains(request.model!) else {
                try invalid("Create requires a closed project, assistant, and optional model.")
            }
        case .send:
            guard validSession(request.sessionID), request.text != nil else {
                try invalid("Send requires sessionID and text.")
            }
        case .observe, .close:
            guard validSession(request.sessionID) else {
                try invalid("Observe and close require sessionID.")
            }
        }
    }

    private func taskSnapshot(_ task: LinuxDurableTaskRecord,
                              includeResult: Bool = false) throws -> LinuxTaskSnapshot {
        let resultBase64: String?
        if includeResult, let digest = task.resultDigest,
           let byteCount = task.resultByteCount {
            resultBase64 = try store.readTaskResult(
                taskID: task.id, expectedDigest: digest,
                expectedBytes: byteCount).base64EncodedString()
        } else {
            resultBase64 = nil
        }
        return LinuxTaskSnapshot(
            id: task.id, projectRoot: task.projectRoot, sessionID: task.terminalID,
            title: task.title, claims: task.claims, state: task.state,
            createdAt: task.createdAt, messageCount: task.messages.count,
            resultDigest: task.resultDigest, resultBase64: resultBase64,
            resultPublishedAt: task.resultPublishedAt,
            resultAcknowledgedAt: task.resultAcknowledgedAt)
    }

    private func timestamp() -> String {
        ISO8601DateFormatter().string(from: Date())
    }

    private func required<T>(_ value: T?, _ name: String) throws -> T {
        guard let value else {
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "The accepted request lost \(name).")
        }
        return value
    }

    private func encodeResponse(_ response: LinuxIngressResponse) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(response)
    }

    private func encodeReceipt(_ receipt: LinuxLifecycleReceipt) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(receipt)
    }

    private func constantTimeEqual(_ lhs: String, _ rhs: String) -> Bool {
        let left = Array(lhs.utf8), right = Array(rhs.utf8)
        var difference = left.count ^ right.count
        for index in 0..<max(left.count, right.count) {
            difference |= Int((index < left.count ? left[index] : 0)
                ^ (index < right.count ? right[index] : 0))
        }
        return difference == 0
    }

    private func authenticateSecretBearingMutation(_ request: LinuxIngressRequest,
                                                    state: LinuxDurableState) throws {
        guard request.operation.requiresTaskSecret else { return }
        let supplied = request.taskSecret.map(LinuxIngressRequest.secretDigest) ?? ""
        let task = state.tasks.first { $0.id == request.taskID }
        let expected = task?.secretDigest ?? String(repeating: "0", count: 64)
        let secretMatches = constantTimeEqual(supplied, expected)
        let identityMatches = task?.projectRoot == request.projectRoot
            && task?.terminalID == request.sessionID
            && task?.projectRoot != nil && task?.terminalID != nil
        guard task != nil, secretMatches, identityMatches else {
            throw LinuxDurableStateFailure(code: "task_unauthorized",
                                           message: "Task authentication failed.")
        }
    }

    private func revalidatePublishedResult(for request: LinuxIngressRequest,
                                           state: LinuxDurableState) throws {
        guard let task = state.tasks.first(where: { $0.id == request.taskID }) else {
            throw LinuxDurableStateFailure(code: "task_result_non_authoritative",
                                           message: "Published result authority is unavailable.")
        }
        try store.revalidatePublishedResult(task)
    }

    private func authoritativeState() throws -> LinuxDurableState {
        let loaded = store.load()
        guard loaded.authoritative, let state = loaded.state else {
            throw LinuxDurableStateFailure(
                code: "state_non_authoritative",
                message: loaded.reason ?? "Durable state requires recovery before ingress.")
        }
        return state
    }

    private func invoke(_ request: LinuxIngressRequest) throws -> LinuxLifecycleReceipt? {
        switch request.operation {
        case .create, .taskCreate:
            guard let root = request.projectRoot, let assistantName = request.assistant,
                  let assistant = Assistant(rawValue: assistantName) else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Create requires projectRoot and assistant.")
            }
            return try runtime.create(
                commandID: request.commandID, projectRoot: root, assistant: assistant,
                model: request.model, reasoningEffort: nil, permission: .ask,
                additionalDirectory: nil, resumeSessionID: nil)
        case .send, .taskMessage:
            guard let session = request.sessionID, let text = request.text else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Send requires sessionID and text.")
            }
            return try runtime.send(commandID: request.commandID, sessionID: session, text: text)
        case .observe:
            guard let session = request.sessionID else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Observe requires sessionID.")
            }
            return try runtime.observe(commandID: request.commandID, sessionID: session)
        case .close, .taskClose:
            guard let session = request.sessionID else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Close requires sessionID.")
            }
            return try runtime.close(commandID: request.commandID, sessionID: session)
        case .taskResult, .taskAcknowledge:
            return nil
        case .taskRead, .boardRead, .sessionRead, .documentsRead, .documentRead:
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "A read cannot invoke the effect executor.")
        }
    }

    private static func durableStage(_ stage: TerminalEffectStage) -> LinuxDurableCommandStage {
        switch stage {
        case .accepted: return .accepted
        case .executed: return .executed
        case .delivered: return .delivered
        case .observed: return .observed
        }
    }
}
