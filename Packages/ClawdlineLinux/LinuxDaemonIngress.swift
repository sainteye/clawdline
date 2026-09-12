import Foundation
import ClawdlineApplication

enum LinuxIngressOperation: String, Codable {
    case create, send, observe, close
}

struct LinuxIngressRequest: Codable, Equatable {
    let authorization: String?
    let operation: LinuxIngressOperation
    let commandID: String
    let taskID: String
    let sessionID: String?
    let projectRoot: String?
    let assistant: String?
    let text: String?
    let acknowledge: Bool
    let authorizeRecovery: Bool

    init(authorization: String? = nil, operation: LinuxIngressOperation,
         commandID: String, taskID: String, sessionID: String? = nil,
         projectRoot: String? = nil, assistant: Assistant? = nil, text: String? = nil,
         acknowledge: Bool = false, authorizeRecovery: Bool = false) {
        self.authorization = authorization
        self.operation = operation
        self.commandID = commandID
        self.taskID = taskID
        self.sessionID = sessionID
        self.projectRoot = projectRoot
        self.assistant = assistant?.rawValue
        self.text = text
        self.acknowledge = acknowledge
        self.authorizeRecovery = authorizeRecovery
    }

    func sealedBytes() throws -> Data {
        let sealed = LinuxIngressRequest(
            operation: operation, commandID: commandID, taskID: taskID,
            sessionID: sessionID, projectRoot: projectRoot,
            assistant: assistant.flatMap(Assistant.init(rawValue:)), text: text,
            acknowledge: acknowledge, authorizeRecovery: false)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(sealed)
    }
}

struct LinuxIngressResponse: Codable, Equatable {
    let operation: LinuxIngressOperation
    let commandID: String
    let taskID: String
    let receipt: LinuxLifecycleReceipt
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
    private let lock = NSLock()
    private var admissionOpen = false
    var faultInjection: ((LinuxIngressFaultPoint) throws -> Void)?

    init(store: LinuxDurableStateStore, runtime: any LinuxLifecyclePerforming) {
        self.store = store
        self.runtime = runtime
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
        lock.lock()
        defer { lock.unlock() }
        guard admissionOpen else {
            throw LinuxDurableStateFailure(code: "ingress_closed",
                                           message: "Startup reconciliation has not opened admission.")
        }
        guard !request.commandID.isEmpty, !request.taskID.isEmpty else {
            throw LinuxDurableStateFailure(code: "invalid_ingress",
                                           message: "Ingress command and task IDs are required.")
        }
        let sealed = try request.sealedBytes()
        let payloadDigest = LinuxSHA256.hex(sealed)
        var state = try authoritativeState()

        if let existing = state.commands.firstIndex(where: { $0.id == request.commandID }) {
            if state.commands[existing].outcome == .succeeded,
               let encoded = state.commands[existing].responseBase64,
               let response = Data(base64Encoded: encoded) {
                return response
            }
            guard request.authorizeRecovery,
                  state.commands[existing].outcome == .interrupted
                    || state.commands[existing].outcome == .unknown,
                  let queued = state.queue.first(where: { $0.commandID == request.commandID }),
                  queued.sealedPayloadRecoverable,
                  queued.payloadDigest == payloadDigest,
                  queued.sealedPayloadBase64 == sealed.base64EncodedString() else {
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
            if !state.tasks.contains(where: { $0.id == request.taskID }) {
                state.tasks.append(.init(id: request.taskID, terminalID: request.sessionID,
                                         state: .queued, resultDigest: nil,
                                         acknowledgedEvidence: []))
            }
            state.commands.append(.init(
                id: request.commandID, taskID: request.taskID, terminalID: request.sessionID,
                operation: request.operation.rawValue, stage: .accepted, outcome: .pending,
                evidenceDigest: nil, acknowledgedAt: nil))
            state.queue.append(.init(
                id: "queue-\(request.commandID)", taskID: request.taskID,
                commandID: request.commandID, state: .queued, payloadDigest: payloadDigest,
                sealedPayloadRecoverable: true,
                sealedPayloadBase64: sealed.base64EncodedString()))
        }
        try store.save(state)
        try faultInjection?(.afterQueueSeal)

        let receipt: LinuxLifecycleReceipt
        do {
            receipt = try invoke(request)
        } catch {
            // Accepted+sealed is intentionally retained. Startup reconciliation will type it as
            // interrupted/unknown rather than guessing whether a terminal effect happened.
            throw error
        }
        try faultInjection?(.afterEffectBeforeReceipt)

        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let response = try encoder.encode(LinuxIngressResponse(
            operation: request.operation, commandID: request.commandID,
            taskID: request.taskID, receipt: receipt))
        state = try authoritativeState()
        guard let commandIndex = state.commands.firstIndex(where: { $0.id == request.commandID }),
              let taskIndex = state.tasks.firstIndex(where: { $0.id == request.taskID }) else {
            throw LinuxDurableStateFailure(code: "ledger_identity_lost",
                                           message: "The serialized ledger lost its command identity.")
        }
        let stage = Self.durableStage(receipt.progress.stages.last ?? .accepted)
        state.commands[commandIndex].stage = request.acknowledge ? .acknowledged : stage
        state.commands[commandIndex].outcome = .succeeded
        state.commands[commandIndex].terminalID = receipt.sessionID ?? request.sessionID
        state.commands[commandIndex].evidenceDigest = LinuxSHA256.hex(response)
        state.commands[commandIndex].acknowledgedAt = request.acknowledge
            ? ISO8601DateFormatter().string(from: Date()) : nil
        state.commands[commandIndex].responseBase64 = response.base64EncodedString()
        if let queueIndex = state.queue.firstIndex(where: { $0.commandID == request.commandID }) {
            state.queue[queueIndex].state = .complete
        }
        if request.operation == .close {
            state.tasks[taskIndex].state = .complete
            state.tasks[taskIndex].resultDigest = LinuxSHA256.hex(response)
        } else {
            state.tasks[taskIndex].state = .working
            if let sessionID = receipt.sessionID ?? request.sessionID {
                state.tasks[taskIndex].terminalID = sessionID
            }
        }
        if request.acknowledge,
           let evidence = state.commands[commandIndex].evidenceDigest,
           !state.tasks[taskIndex].acknowledgedEvidence.contains(evidence) {
            state.tasks[taskIndex].acknowledgedEvidence.append(evidence)
        }
        if request.operation == .create, let sessionID = receipt.sessionID,
           !state.terminals.contains(where: { $0.id == sessionID }) {
            state.terminals.append(.init(
                id: sessionID, taskID: request.taskID, state: .present,
                lastObservedAt: ISO8601DateFormatter().string(from: Date()),
                evidenceDigest: LinuxSHA256.hex(response)))
        } else if request.operation == .close, let sessionID = request.sessionID,
                  let terminalIndex = state.terminals.firstIndex(where: { $0.id == sessionID }) {
            state.terminals[terminalIndex].state = .missing
            state.terminals[terminalIndex].evidenceDigest = LinuxSHA256.hex(response)
        }
        try store.save(state)
        try faultInjection?(.afterReceiptBeforeResponse)
        return response
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

    private func invoke(_ request: LinuxIngressRequest) throws -> LinuxLifecycleReceipt {
        switch request.operation {
        case .create:
            guard let root = request.projectRoot, let assistantName = request.assistant,
                  let assistant = Assistant(rawValue: assistantName) else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Create requires projectRoot and assistant.")
            }
            return try runtime.create(
                commandID: request.commandID, projectRoot: root, assistant: assistant,
                model: nil, reasoningEffort: nil, permission: .ask,
                additionalDirectory: nil, resumeSessionID: nil)
        case .send:
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
        case .close:
            guard let session = request.sessionID else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "Close requires sessionID.")
            }
            return try runtime.close(commandID: request.commandID, sessionID: session)
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
