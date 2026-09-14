import Foundation
import ClawdlineApplication

// W4-2 owns Linux service state. These records deliberately do not reuse the Mac registry's
// `/tmp` paths: systemd supplies a durable StateDirectory and a separate volatile
// RuntimeDirectory, while the application/host boundaries from W4-1 remain the effect owners.

enum LinuxDurableTerminalState: String, Codable, Equatable {
    case present
    case missing
    case unknown
}

enum LinuxDurableTaskState: String, Codable, Equatable {
    case queued
    case working
    case resultPublished = "result_published"
    case acknowledged
    case reconciling
    case complete
    case failed
    case unknown
}

struct LinuxDurableTaskMessage: Codable, Equatable {
    let requestID: String
    let textDigest: String
    let acceptedAt: String
    var deliveredReceiptDigest: String?
}

enum LinuxDurableQueueState: String, Codable, Equatable {
    case queued
    case complete
    case unknown
}

enum LinuxDurableCommandStage: String, Codable, Equatable {
    case accepted
    case executed
    case delivered
    case observed
    case acknowledged
}

enum LinuxDurableCommandOutcome: String, Codable, Equatable {
    case pending
    case succeeded
    case failed
    case interrupted
    case unknown
}

struct LinuxDurableTerminalRecord: Codable, Equatable {
    let id: String
    var taskID: String?
    var state: LinuxDurableTerminalState
    var lastObservedAt: String?
    var evidenceDigest: String?
}

struct LinuxDurableTaskRecord: Codable, Equatable {
    let id: String
    var terminalID: String?
    var state: LinuxDurableTaskState
    var resultDigest: String?
    var acknowledgedEvidence: [String]
    var projectRoot: String?
    var title: String?
    var claims: [String]
    var secretDigest: String?
    var createdAt: String?
    var resultByteCount: Int?
    var resultPublishedAt: String?
    var resultAcknowledgedAt: String?
    var messages: [LinuxDurableTaskMessage]

    init(id: String, terminalID: String?, state: LinuxDurableTaskState,
         resultDigest: String?, acknowledgedEvidence: [String],
         projectRoot: String? = nil, title: String? = nil, claims: [String] = [],
         secretDigest: String? = nil, createdAt: String? = nil,
         resultByteCount: Int? = nil, resultPublishedAt: String? = nil,
         resultAcknowledgedAt: String? = nil,
         messages: [LinuxDurableTaskMessage] = []) {
        self.id = id
        self.terminalID = terminalID
        self.state = state
        self.resultDigest = resultDigest
        self.acknowledgedEvidence = acknowledgedEvidence
        self.projectRoot = projectRoot
        self.title = title
        self.claims = claims
        self.secretDigest = secretDigest
        self.createdAt = createdAt
        self.resultByteCount = resultByteCount
        self.resultPublishedAt = resultPublishedAt
        self.resultAcknowledgedAt = resultAcknowledgedAt
        self.messages = messages
    }

    private enum CodingKeys: String, CodingKey {
        case id, terminalID, state, resultDigest, acknowledgedEvidence
        case projectRoot, title, claims, secretDigest, createdAt, resultByteCount
        case resultPublishedAt, resultAcknowledgedAt, messages
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        terminalID = try values.decodeIfPresent(String.self, forKey: .terminalID)
        state = try values.decode(LinuxDurableTaskState.self, forKey: .state)
        resultDigest = try values.decodeIfPresent(String.self, forKey: .resultDigest)
        acknowledgedEvidence = try values.decodeIfPresent([String].self,
                                                           forKey: .acknowledgedEvidence) ?? []
        projectRoot = try values.decodeIfPresent(String.self, forKey: .projectRoot)
        title = try values.decodeIfPresent(String.self, forKey: .title)
        claims = try values.decodeIfPresent([String].self, forKey: .claims) ?? []
        secretDigest = try values.decodeIfPresent(String.self, forKey: .secretDigest)
        createdAt = try values.decodeIfPresent(String.self, forKey: .createdAt)
        resultByteCount = try values.decodeIfPresent(Int.self, forKey: .resultByteCount)
        resultPublishedAt = try values.decodeIfPresent(String.self, forKey: .resultPublishedAt)
        resultAcknowledgedAt = try values.decodeIfPresent(String.self,
                                                           forKey: .resultAcknowledgedAt)
        messages = try values.decodeIfPresent([LinuxDurableTaskMessage].self,
                                               forKey: .messages) ?? []
    }
}

struct LinuxDurableQueueRecord: Codable, Equatable {
    let id: String
    let taskID: String
    let commandID: String
    var state: LinuxDurableQueueState
    let payloadDigest: String
    var sealedPayloadRecoverable: Bool
    var sealedPayloadBase64: String?

    init(id: String, taskID: String, commandID: String, state: LinuxDurableQueueState,
         payloadDigest: String, sealedPayloadRecoverable: Bool,
         sealedPayloadBase64: String? = nil) {
        self.id = id
        self.taskID = taskID
        self.commandID = commandID
        self.state = state
        self.payloadDigest = payloadDigest
        self.sealedPayloadRecoverable = sealedPayloadRecoverable
        self.sealedPayloadBase64 = sealedPayloadBase64
    }

    private enum CodingKeys: String, CodingKey {
        case id, taskID, commandID, state, payloadDigest, sealedPayloadRecoverable
        case sealedPayloadBase64
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        taskID = try values.decode(String.self, forKey: .taskID)
        commandID = try values.decode(String.self, forKey: .commandID)
        state = try values.decodeIfPresent(LinuxDurableQueueState.self, forKey: .state) ?? .queued
        payloadDigest = try values.decode(String.self, forKey: .payloadDigest)
        // A schema-1 row without explicit recovery evidence is retained but never replayable.
        sealedPayloadRecoverable = try values.decodeIfPresent(
            Bool.self, forKey: .sealedPayloadRecoverable) ?? false
        sealedPayloadBase64 = try values.decodeIfPresent(String.self, forKey: .sealedPayloadBase64)
    }
}

struct LinuxDurableCommandRecord: Codable, Equatable {
    let id: String
    let taskID: String
    var terminalID: String?
    let operation: String
    var stage: LinuxDurableCommandStage
    var outcome: LinuxDurableCommandOutcome
    var evidenceDigest: String?
    var acknowledgedAt: String?
    var responseBase64: String?

    init(id: String, taskID: String, terminalID: String?, operation: String,
         stage: LinuxDurableCommandStage, outcome: LinuxDurableCommandOutcome,
         evidenceDigest: String?, acknowledgedAt: String?, responseBase64: String? = nil) {
        self.id = id
        self.taskID = taskID
        self.terminalID = terminalID
        self.operation = operation
        self.stage = stage
        self.outcome = outcome
        self.evidenceDigest = evidenceDigest
        self.acknowledgedAt = acknowledgedAt
        self.responseBase64 = responseBase64
    }
}

/// Schema 3 is an additive envelope over schemas 1 and 2. Decoding intentionally accepts missing new
/// fields and JSONDecoder intentionally accepts additional fields. A writer never opens an
/// unknown version, so it cannot erase fields belonging to a future schema it does not know.
struct LinuxDurableState: Codable, Equatable {
    static let schemaVersion = 3
    static let minimumReadableSchemaVersion = 1
    static let maximumReadableSchemaVersion = 3

    var schemaVersion: Int
    var minimumReaderVersion: Int
    var daemonEpoch: UInt64
    var lastReconciledAt: String?
    var terminals: [LinuxDurableTerminalRecord]
    var tasks: [LinuxDurableTaskRecord]
    var queue: [LinuxDurableQueueRecord]
    var commands: [LinuxDurableCommandRecord]

    init(schemaVersion: Int = LinuxDurableState.schemaVersion,
         minimumReaderVersion: Int = LinuxDurableState.minimumReadableSchemaVersion,
         daemonEpoch: UInt64 = 0, lastReconciledAt: String? = nil,
         terminals: [LinuxDurableTerminalRecord] = [],
         tasks: [LinuxDurableTaskRecord] = [],
         queue: [LinuxDurableQueueRecord] = [],
         commands: [LinuxDurableCommandRecord] = []) {
        self.schemaVersion = schemaVersion
        self.minimumReaderVersion = minimumReaderVersion
        self.daemonEpoch = daemonEpoch
        self.lastReconciledAt = lastReconciledAt
        self.terminals = terminals
        self.tasks = tasks
        self.queue = queue
        self.commands = commands
    }

    private enum CodingKeys: String, CodingKey {
        case schemaVersion, minimumReaderVersion, daemonEpoch, lastReconciledAt
        case terminals, tasks, queue, commands
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        minimumReaderVersion = try values.decodeIfPresent(Int.self, forKey: .minimumReaderVersion) ?? 1
        daemonEpoch = try values.decodeIfPresent(UInt64.self, forKey: .daemonEpoch) ?? 0
        lastReconciledAt = try values.decodeIfPresent(String.self, forKey: .lastReconciledAt)
        terminals = try values.decodeIfPresent([LinuxDurableTerminalRecord].self,
                                               forKey: .terminals) ?? []
        tasks = try values.decodeIfPresent([LinuxDurableTaskRecord].self, forKey: .tasks) ?? []
        queue = try values.decodeIfPresent([LinuxDurableQueueRecord].self, forKey: .queue) ?? []
        commands = try values.decodeIfPresent([LinuxDurableCommandRecord].self,
                                              forKey: .commands) ?? []
    }
}

struct LinuxDurableStateFailure: Error, Codable, Equatable {
    let code: String
    let message: String
}

enum LinuxStateLoadDisposition: String, Codable, Equatable {
    case initialized
    case loaded
    case migrated
    case quarantined
    case unreadable
    case recoveryRequired = "recovery_required"
}

struct LinuxStateLoadOutcome: Equatable {
    var state: LinuxDurableState?
    let authoritative: Bool
    let disposition: LinuxStateLoadDisposition
    let preservedOriginal: String?
    let reason: String?
}

enum LinuxLegacyMigrationFaultPoint: String, Error {
    case afterAuthorityWrite
    case afterLegacyArchiveWrite
    case afterRollbackFenceWrite
}

/// One descriptor-bound, bounded state file. Failed parsing never falls back to authoritative
/// empty state; the original is renamed into quarantine when that can be done safely.
final class LinuxDurableStateStore {
    static let maximumBytes = 8 * 1024 * 1024
    static let maximumRecordsPerKind = 10_000
    static let recoveryAuthorization = "authorize-empty-state-after-preserved-recovery"

    /// W4-3's one authoritative task root. Runtime linkage, task identity, accepted commands,
    /// terminal results and acknowledgement receipts live in one atomic authority file here;
    /// per-task `artifacts/` directories contain only document bytes referenced by that state.
    let recordsDirectory: String
    let tasksDirectory: String
    let legacyRecordsDirectory: String
    let legacyStatePath: String
    let quarantineDirectory: String
    let statePath: String
    let recoveryObligationPath: String
    private let expectedUID: UInt32
    var migrationFaultInjection: ((LinuxLegacyMigrationFaultPoint) throws -> Void)?

    init(stateDirectory: String, expectedUID: UInt32 = geteuid()) throws {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(stateDirectory) else {
            throw LinuxDurableStateFailure(code: "unsafe_state_path",
                                           message: "The durable state path must be canonical and absolute.")
        }
        self.tasksDirectory = stateDirectory + "/tasks"
        self.recordsDirectory = self.tasksDirectory
        self.legacyRecordsDirectory = stateDirectory + "/records"
        self.legacyStatePath = self.legacyRecordsDirectory + "/runtime-state.json"
        self.quarantineDirectory = stateDirectory + "/quarantine"
        self.statePath = recordsDirectory + "/authority.json"
        self.recoveryObligationPath = recordsDirectory + "/recovery-obligation.json"
        self.expectedUID = expectedUID
        try Self.prepareOwnedDirectory(recordsDirectory, expectedUID: expectedUID)
        try Self.prepareOwnedDirectory(legacyRecordsDirectory, expectedUID: expectedUID)
        try Self.prepareOwnedDirectory(quarantineDirectory, expectedUID: expectedUID)
    }

    func load() -> LinuxStateLoadOutcome {
        switch secureRead(path: recoveryObligationPath) {
        case .bytes(let bytes):
            let obligation = try? JSONDecoder().decode(RecoveryObligation.self, from: bytes)
            let planned = obligation?.preservedOriginal
            let preserved: String
            if let planned, case .bytes = secureRead(path: planned) { preserved = planned }
            else if case .bytes = secureRead(path: statePath) { preserved = statePath }
            else { preserved = planned ?? statePath }
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false, disposition: .recoveryRequired,
                preservedOriginal: preserved,
                reason: obligation?.reason ?? "durable recovery obligation is unreadable")
        case .unreadable(let reason):
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false, disposition: .recoveryRequired,
                preservedOriginal: statePath, reason: reason)
        case .missing:
            break
        }
        let bytes: Data
        var loadedFromLegacy = false
        switch secureRead(path: statePath) {
        case .missing:
            switch secureRead(path: legacyStatePath) {
            case .missing:
                return LinuxStateLoadOutcome(
                    state: LinuxDurableState(), authoritative: true, disposition: .initialized,
                    preservedOriginal: nil, reason: nil)
            case .unreadable(let reason):
                let preserved = quarantineOriginal(path: legacyStatePath,
                                                   sourceDirectory: legacyRecordsDirectory,
                                                   reason: reason)
                return LinuxStateLoadOutcome(
                    state: nil, authoritative: false,
                    disposition: preserved == nil ? .unreadable : .quarantined,
                    preservedOriginal: preserved ?? legacyStatePath, reason: reason)
            case .bytes(let value):
                bytes = value
                loadedFromLegacy = true
            }
        case .unreadable(let reason):
            let preserved = quarantineOriginal(path: statePath,
                                               sourceDirectory: recordsDirectory,
                                               reason: reason)
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false,
                disposition: preserved == nil ? .unreadable : .quarantined,
                preservedOriginal: preserved ?? statePath, reason: reason)
        case .bytes(let value):
            bytes = value
        }

        let version: Int
        var recordKind: String?
        do {
            guard let object = try JSONSerialization.jsonObject(with: bytes) as? [String: Any],
                  let value = object["schemaVersion"] as? Int else {
                throw LinuxDurableStateFailure(code: "invalid_state",
                                               message: "The durable state envelope has no integer schemaVersion.")
            }
            version = value
            recordKind = object["recordKind"] as? String
        } catch {
            let reason = "durable state is corrupt and was not treated as empty"
            let sourcePath = loadedFromLegacy ? legacyStatePath : statePath
            let sourceDirectory = loadedFromLegacy ? legacyRecordsDirectory : recordsDirectory
            let preserved = quarantineOriginal(path: sourcePath, sourceDirectory: sourceDirectory,
                                               reason: reason)
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false,
                disposition: preserved == nil ? .unreadable : .quarantined,
                preservedOriginal: preserved ?? sourcePath,
                reason: reason)
        }

        if let recordKind {
            let reason = loadedFromLegacy && recordKind == RollbackFence.kindValue
                ? "the authoritative task state is missing; its package rollback fence is not task authority"
                : "a durable task authority carries an unsupported recordKind"
            let preserved = quarantineOriginal(
                path: loadedFromLegacy ? legacyStatePath : statePath,
                sourceDirectory: loadedFromLegacy ? legacyRecordsDirectory : recordsDirectory,
                reason: reason)
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false,
                disposition: preserved == nil ? .unreadable : .quarantined,
                preservedOriginal: preserved ?? (loadedFromLegacy ? legacyStatePath : statePath),
                reason: reason)
        }

        let readableVersions = LinuxDurableState.minimumReadableSchemaVersion...LinuxDurableState.maximumReadableSchemaVersion
        guard readableVersions.contains(version) else {
            let reason = "durable state schema \(version) is not readable by this image"
            let sourcePath = loadedFromLegacy ? legacyStatePath : statePath
            let sourceDirectory = loadedFromLegacy ? legacyRecordsDirectory : recordsDirectory
            let preserved = quarantineOriginal(path: sourcePath, sourceDirectory: sourceDirectory,
                                               reason: reason)
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false,
                disposition: preserved == nil ? .unreadable : .quarantined,
                preservedOriginal: preserved ?? sourcePath,
                reason: reason)
        }

        let state: LinuxDurableState
        do {
            var decoded = try JSONDecoder().decode(LinuxDurableState.self, from: bytes)
            guard decoded.minimumReaderVersion <= LinuxDurableState.schemaVersion else {
                throw LinuxDurableStateFailure(
                    code: "unknown_state_version",
                    message: "The state requires reader \(decoded.minimumReaderVersion).")
            }
            try Self.validateSemantics(decoded)
            decoded.schemaVersion = LinuxDurableState.schemaVersion
            state = decoded
        } catch {
            let reason = "durable records failed closed semantic validation and were not treated as empty"
            let sourcePath = loadedFromLegacy ? legacyStatePath : statePath
            let sourceDirectory = loadedFromLegacy ? legacyRecordsDirectory : recordsDirectory
            let preserved = quarantineOriginal(path: sourcePath, sourceDirectory: sourceDirectory,
                                               reason: reason)
            return LinuxStateLoadOutcome(
                state: nil, authoritative: false,
                disposition: preserved == nil ? .unreadable : .quarantined,
                preservedOriginal: preserved ?? sourcePath,
                reason: reason)
        }
        let migrated = loadedFromLegacy || version != LinuxDurableState.schemaVersion
        var preservedLegacy: String?
        if loadedFromLegacy {
            do {
                preservedLegacy = try migrateLegacyState(state, legacyBytes: bytes)
            } catch let failure as LegacyMigrationFailure {
                try? recordRecoveryObligation(
                    preservedOriginal: failure.preservedOriginal,
                    reason: failure.reason)
                return LinuxStateLoadOutcome(
                    state: nil, authoritative: false, disposition: .recoveryRequired,
                    preservedOriginal: failure.preservedOriginal, reason: failure.reason)
            } catch {
                let reason = "legacy task authority could not be migrated durably"
                try? recordRecoveryObligation(
                    preservedOriginal: legacyStatePath, reason: reason)
                return LinuxStateLoadOutcome(
                    state: nil, authoritative: false, disposition: .recoveryRequired,
                    preservedOriginal: legacyStatePath, reason: reason)
            }
        }
        return LinuxStateLoadOutcome(
            state: state, authoritative: true,
            disposition: migrated ? .migrated : .loaded,
            preservedOriginal: preservedLegacy, reason: nil)
    }

    func save(_ state: LinuxDurableState) throws {
        guard state.schemaVersion == LinuxDurableState.schemaVersion,
              state.minimumReaderVersion <= LinuxDurableState.schemaVersion else {
            throw LinuxDurableStateFailure(code: "unknown_state_version",
                                           message: "Refusing to write an unsupported durable schema.")
        }
        try Self.validateSemantics(state)
        try writeAuthority(state)
        try writeRollbackFence()
    }

    /// Installs the new authority before replacing the schema-1/2 pathname. The old bytes remain
    /// readable by the old image until one atomic fence rename; there is no unlink/rename window in
    /// which the old image can misread migration as an empty authority.
    private func migrateLegacyState(_ state: LinuxDurableState,
                                    legacyBytes: Data) throws -> String {
        do {
            try writeAuthority(state)
            try migrationFaultInjection?(.afterAuthorityWrite)
        } catch {
            throw LegacyMigrationFailure(
                preservedOriginal: legacyStatePath,
                reason: "legacy task authority could not be copied into the durable task root")
        }
        let destination = quarantineDirectory
            + "/runtime-state.migrated.\(UUID().uuidString.lowercased()).json"
        do {
            try Self.atomicWrite(legacyBytes, to: destination,
                                 directory: quarantineDirectory)
            try migrationFaultInjection?(.afterLegacyArchiveWrite)
        } catch {
            throw LegacyMigrationFailure(
                preservedOriginal: legacyStatePath,
                reason: "legacy task authority could not be archived before migration cutover")
        }
        do {
            try writeRollbackFence()
            try migrationFaultInjection?(.afterRollbackFenceWrite)
        } catch {
            throw LegacyMigrationFailure(
                preservedOriginal: destination,
                reason: "the migrated authority is durable but its package rollback fence could not be written")
        }
        return destination
    }

    private struct LegacyMigrationFailure: Error {
        let preservedOriginal: String
        let reason: String
    }

    private struct RollbackFence: Encodable {
        static let kindValue = "clawdline_linux_task_authority_rollback_fence"
        let schemaVersion = LinuxDurableState.schemaVersion
        let minimumReaderVersion = LinuxDurableState.schemaVersion
        let recordKind = Self.kindValue
        let authorityPath = "tasks/authority.json"
    }

    private func writeAuthority(_ state: LinuxDurableState) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let data = try encoder.encode(state)
        guard data.count <= Self.maximumBytes else {
            throw LinuxDurableStateFailure(code: "state_too_large",
                                           message: "Durable state exceeds the bounded record size.")
        }
        try Self.atomicWrite(data, to: statePath, directory: recordsDirectory)
    }

    private func writeRollbackFence() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try Self.atomicWrite(
            encoder.encode(RollbackFence()), to: legacyStatePath,
            directory: legacyRecordsDirectory)
    }

    private func recordRecoveryObligation(preservedOriginal: String,
                                          reason: String) throws {
        let obligation = RecoveryObligation(
            schemaVersion: 1, preservedOriginal: preservedOriginal, reason: reason)
        let data = try JSONEncoder().encode(obligation)
        try Self.atomicWrite(data, to: recoveryObligationPath,
                             directory: recordsDirectory)
    }

    /// Creates only the fixed per-task document root. The task id has already crossed the
    /// Application slug policy; both directories are opened relative to the held task-root
    /// descriptor and are never allowed to redirect through a link.
    @discardableResult
    func prepareTaskArtifacts(taskID: String) throws -> String {
        guard SessionLaunchPolicy.opaqueCommandID(taskID) == taskID else {
            throw LinuxDurableStateFailure(code: "invalid_task_identity",
                                           message: "The task id is not a closed opaque identifier.")
        }
        let root = tasksDirectory.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard root >= 0 else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "The authoritative task root is unavailable.")
        }
        defer { _ = close(root) }
        try prepareDirectory(named: taskID, beneath: root)
        let task = taskID.withCString {
            openat(root, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard task >= 0 else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "The task directory is unavailable.")
        }
        defer { _ = close(task) }
        try prepareDirectory(named: "artifacts", beneath: task)
        guard fsync(task) == 0, fsync(root) == 0 else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "The task root could not be synchronized.")
        }
        return tasksDirectory + "/" + taskID + "/artifacts"
    }

    /// Result bytes are bounded task-owned evidence, not duplicated inside the global authority
    /// envelope or completed replay queue. The authority records their digest/count only after
    /// this file and its parent directory are durable.
    func publishTaskResult(_ bytes: Data, taskID: String, expectedDigest: String) throws {
        guard !bytes.isEmpty, bytes.count <= HeadlessDocumentPathPolicy.maximumBytes,
              LinuxSHA256.hex(bytes) == expectedDigest else {
            throw LinuxDurableStateFailure(code: "invalid_task_result",
                                           message: "Task result bytes do not match their bounded digest.")
        }
        _ = try prepareTaskArtifacts(taskID: taskID)
        let path = taskResultPath(taskID)
        switch secureRead(path: path, maximumBytes: HeadlessDocumentPathPolicy.maximumBytes) {
        case .missing:
            try Self.atomicWrite(bytes, to: path,
                                 directory: tasksDirectory + "/" + taskID)
        case .bytes(let existing):
            guard existing == bytes else {
                throw LinuxDurableStateFailure(
                    code: "task_result_conflict",
                    message: "The task result path already holds different durable bytes.")
            }
        case .unreadable:
            throw LinuxDurableStateFailure(
                code: "task_result_non_authoritative",
                message: "The task result path is not one safe durable file.")
        }
    }

    func readTaskResult(taskID: String, expectedDigest: String,
                        expectedBytes: Int) throws -> Data {
        let path = taskResultPath(taskID)
        guard case .bytes(let bytes) = secureRead(
            path: path, maximumBytes: HeadlessDocumentPathPolicy.maximumBytes),
              bytes.count == expectedBytes, LinuxSHA256.hex(bytes) == expectedDigest else {
            throw LinuxDurableStateFailure(
                code: "task_result_non_authoritative",
                message: "The published task result no longer matches its durable authority.")
        }
        return bytes
    }

    /// Mutation commit points and cached result replay call this in addition to startup/periodic
    /// reconciliation. A missing, replaced or linked result creates a durable typed recovery
    /// obligation before the caller can acknowledge, close or replay its old receipt.
    func revalidatePublishedResult(_ task: LinuxDurableTaskRecord) throws {
        guard let digest = task.resultDigest, let byteCount = task.resultByteCount else {
            _ = resultRecovery(taskID: task.id,
                               reason: "published task result metadata is incomplete")
            throw LinuxDurableStateFailure(
                code: "task_result_non_authoritative",
                message: "The published task result metadata is incomplete.")
        }
        do {
            _ = try readTaskResult(taskID: task.id, expectedDigest: digest,
                                   expectedBytes: byteCount)
        } catch {
            _ = resultRecovery(taskID: task.id,
                               reason: "published task result bytes are missing or unsafe")
            throw LinuxDurableStateFailure(
                code: "task_result_non_authoritative",
                message: "The published task result no longer matches its durable authority.")
        }
    }

    /// Startup invokes this once before admission. Missing, replaced, linked, oversized or
    /// digest-mismatched published results create the same durable recovery obligation as a bad
    /// authority envelope; no later request can silently downgrade them to an empty result.
    func validatePublishedResults(_ state: LinuxDurableState) -> LinuxStateLoadOutcome? {
        for task in state.tasks where task.secretDigest != nil && task.resultDigest != nil {
            guard let digest = task.resultDigest, let byteCount = task.resultByteCount else {
                return resultRecovery(taskID: task.id,
                                      reason: "published task result metadata is incomplete")
            }
            do {
                _ = try readTaskResult(taskID: task.id, expectedDigest: digest,
                                       expectedBytes: byteCount)
            } catch {
                return resultRecovery(taskID: task.id,
                                      reason: "published task result bytes are missing or unsafe")
            }
        }
        return nil
    }

    private func resultRecovery(taskID: String, reason: String) -> LinuxStateLoadOutcome {
        let path = taskResultPath(taskID)
        let preserved: String
        switch secureRead(path: path, maximumBytes: HeadlessDocumentPathPolicy.maximumBytes) {
        case .missing:
            preserved = statePath
            try? recordRecoveryObligation(preservedOriginal: statePath, reason: reason)
        case .bytes, .unreadable:
            preserved = quarantineOriginal(
                path: path, sourceDirectory: tasksDirectory + "/" + taskID,
                reason: reason) ?? path
        }
        return LinuxStateLoadOutcome(
            state: nil, authoritative: false, disposition: .recoveryRequired,
            preservedOriginal: preserved, reason: reason)
    }

    private func taskResultPath(_ taskID: String) -> String {
        tasksDirectory + "/" + taskID + "/result.json"
    }

    /// Empty-state recovery is deliberately impossible through an ordinary load/tick. An
    /// operator must name the explicit authorization token after preserving and inspecting the
    /// quarantined bytes; only then is the fsynced obligation removed.
    func authorizeEmptyRecovery(authorization: String) throws {
        guard authorization == Self.recoveryAuthorization else {
            throw LinuxDurableStateFailure(code: "recovery_not_authorized",
                                           message: "Durable recovery requires explicit authorization.")
        }
        if case .bytes = secureRead(path: statePath) {
            let destination = quarantineDirectory
                + "/runtime-state.authorized.\(UUID().uuidString.lowercased()).json"
            guard rename(statePath, destination) == 0 else {
                throw LinuxDurableStateFailure(
                    code: "recovery_obligation_unavailable",
                    message: "The preserved canonical state could not be quarantined.")
            }
            Self.syncDirectory(quarantineDirectory)
            Self.syncDirectory(recordsDirectory)
        }
        if case .bytes = secureRead(path: legacyStatePath) {
            let destination = quarantineDirectory
                + "/runtime-state.fence-authorized.\(UUID().uuidString.lowercased()).json"
            guard rename(legacyStatePath, destination) == 0 else {
                throw LinuxDurableStateFailure(
                    code: "recovery_obligation_unavailable",
                    message: "The package rollback fence could not be quarantined.")
            }
            Self.syncDirectory(quarantineDirectory)
            Self.syncDirectory(legacyRecordsDirectory)
        }
        guard unlink(recoveryObligationPath) == 0 || errno == ENOENT else {
            throw LinuxDurableStateFailure(code: "recovery_obligation_unavailable",
                                           message: "The recovery obligation could not be cleared safely.")
        }
        Self.syncDirectory(recordsDirectory)
    }

    private enum SecureRead {
        case missing
        case bytes(Data)
        case unreadable(String)
    }

    private func secureRead(path: String,
                            maximumBytes: Int = LinuxDurableStateStore.maximumBytes) -> SecureRead {
        let descriptor = path.withCString { open($0, O_RDONLY | O_NOFOLLOW | O_CLOEXEC) }
        guard descriptor >= 0 else {
            return errno == ENOENT ? .missing : .unreadable("durable state cannot be opened safely")
        }
        defer { _ = close(descriptor) }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFREG,
              metadata.st_uid == expectedUID,
              metadata.st_mode & 0o077 == 0,
              metadata.st_nlink == 1,
              metadata.st_size > 0,
              metadata.st_size <= maximumBytes else {
            return .unreadable("durable state type, owner, mode, or size is unsafe")
        }
        var bytes = [UInt8](repeating: 0, count: Int(metadata.st_size))
        var offset = 0
        while offset < bytes.count {
            let count = bytes.withUnsafeMutableBytes { buffer in
                read(descriptor, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            }
            if count < 0, errno == EINTR { continue }
            guard count > 0 else {
                return .unreadable("durable state ended before its descriptor-bound size")
            }
            offset += count
        }
        var extra: UInt8 = 0
        while true {
            let count = read(descriptor, &extra, 1)
            if count < 0, errno == EINTR { continue }
            guard count == 0 else {
                return .unreadable("durable state changed while it was being read")
            }
            break
        }
        return .bytes(Data(bytes))
    }

    private struct RecoveryObligation: Codable {
        let schemaVersion: Int
        let preservedOriginal: String
        let reason: String
    }

    private func prepareDirectory(named name: String, beneath parent: Int32) throws {
        let made = name.withCString { mkdirat(parent, $0, 0o700) }
        guard made == 0 || errno == EEXIST else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "A protected task directory cannot be created.")
        }
        let descriptor = name.withCString {
            openat(parent, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard descriptor >= 0 else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "A protected task directory cannot be opened safely.")
        }
        defer { _ = close(descriptor) }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFDIR,
              metadata.st_uid == expectedUID,
              metadata.st_mode & 0o077 == 0 else {
            throw LinuxDurableStateFailure(code: "task_root_unavailable",
                                           message: "A task directory has unsafe ownership or mode.")
        }
    }

    private func quarantineOriginal(path: String, sourceDirectory: String,
                                    reason: String) -> String? {
        let suffix = UUID().uuidString.lowercased()
        let destination = quarantineDirectory + "/runtime-state.\(suffix).json"
        guard (try? recordRecoveryObligation(
            preservedOriginal: destination, reason: reason)) != nil else {
            return nil
        }
        guard rename(path, destination) == 0 else { return path }
        Self.syncDirectory(sourceDirectory)
        Self.syncDirectory(quarantineDirectory)
        return destination
    }

    static func validateSemantics(_ state: LinuxDurableState) throws {
        func invalid(_ message: String) throws -> Never {
            throw LinuxDurableStateFailure(code: "invalid_state_semantics", message: message)
        }
        let collections = [state.terminals.count, state.tasks.count,
                           state.queue.count, state.commands.count]
        guard collections.allSatisfy({ $0 <= maximumRecordsPerKind }) else {
            try invalid("A durable collection exceeds its bounded record count.")
        }
        guard state.minimumReaderVersion >= 1,
              state.minimumReaderVersion <= LinuxDurableState.schemaVersion,
              state.daemonEpoch < UInt64.max / 1_000_000 else {
            try invalid("The durable reader floor or daemon epoch is invalid.")
        }
        func validID(_ value: String) -> Bool { !value.isEmpty && value.utf8.count <= 512 }
        func validDigest(_ value: String) -> Bool {
            value.count == 64 && value.allSatisfy {
                ("0"..."9").contains($0) || ("a"..."f").contains($0)
            }
        }
        func validClaim(_ value: String) -> Bool {
            guard !value.isEmpty, value.utf8.count <= 512, !value.hasPrefix("/") else {
                return false
            }
            return value.split(separator: "/", omittingEmptySubsequences: false)
                .allSatisfy { !$0.isEmpty && $0 != "." && $0 != ".." }
        }
        func unique(_ values: [String]) -> Bool { values.allSatisfy(validID) && Set(values).count == values.count }
        let terminalIDs = state.terminals.map(\.id)
        let taskIDs = state.tasks.map(\.id)
        let queueIDs = state.queue.map(\.id)
        let commandIDs = state.commands.map(\.id)
        guard unique(terminalIDs), unique(taskIDs), unique(queueIDs), unique(commandIDs) else {
            try invalid("Durable record IDs must be bounded and unique within each collection.")
        }
        let terminalSet = Set(terminalIDs), taskSet = Set(taskIDs), commandSet = Set(commandIDs)
        let commandTasks = Dictionary(uniqueKeysWithValues: state.commands.map { ($0.id, $0.taskID) })
        guard state.terminals.allSatisfy({ $0.taskID.map(taskSet.contains) ?? true }),
              state.tasks.allSatisfy({ $0.terminalID.map(terminalSet.contains) ?? true }),
              state.queue.allSatisfy({ taskSet.contains($0.taskID) && commandSet.contains($0.commandID)
                  && commandTasks[$0.commandID] == $0.taskID }),
              state.commands.allSatisfy({ taskSet.contains($0.taskID)
                  && ($0.terminalID.map(terminalSet.contains) ?? true) }) else {
            try invalid("A durable record contains a dangling terminal, task, or command reference.")
        }
        for row in state.queue {
            let bytes = row.sealedPayloadBase64.flatMap { Data(base64Encoded: $0) }
            let bound = bytes.map { LinuxSHA256.hex($0) == row.payloadDigest } ?? false
            guard row.sealedPayloadRecoverable == bound,
                  row.sealedPayloadRecoverable || row.sealedPayloadBase64 == nil else {
                try invalid("A recoverable queue row is not bound to its sealed payload bytes.")
            }
        }
        for row in state.tasks where row.state == .complete || row.state == .failed
                || row.state == .resultPublished || row.state == .acknowledged {
            guard row.resultDigest?.isEmpty == false else {
                try invalid("A terminal task has no result evidence.")
            }
        }
        for row in state.tasks {
            let hasPublishedResult = row.resultDigest != nil || row.resultByteCount != nil
                || row.resultPublishedAt != nil || row.resultAcknowledgedAt != nil
            let authenticatedResultIsValid = !hasPublishedResult || (
                row.resultDigest.map(validDigest) == true
                    && row.resultByteCount.map {
                        $0 > 0 && $0 <= HeadlessDocumentPathPolicy.maximumBytes
                    } == true
                    && row.resultPublishedAt != nil
                    && (row.resultAcknowledgedAt == nil
                        || row.state == .acknowledged || row.state == .complete)
            )
            let legacyResultIsValid = row.resultByteCount == nil
                && row.resultPublishedAt == nil && row.resultAcknowledgedAt == nil
            guard row.claims.count <= 256,
                  row.claims.allSatisfy(validClaim),
                  Set(row.claims).count == row.claims.count,
                  row.messages.count <= 1_000,
                  row.title.map({ !$0.isEmpty && $0.utf8.count <= 1_000 }) ?? true,
                  row.secretDigest.map(validDigest) ?? true,
                  (row.secretDigest == nil ? legacyResultIsValid : authenticatedResultIsValid),
                  row.state != .resultPublished || row.resultPublishedAt != nil,
                  row.state != .acknowledged || row.resultAcknowledgedAt != nil,
                  row.secretDigest == nil || row.state != .complete
                    || row.resultAcknowledgedAt != nil else {
                try invalid("A durable task identity, result, or message record is contradictory.")
            }
            if row.secretDigest != nil {
                guard row.projectRoot.map(ProjectRootPolicy.isLexicallySafeAbsolute) == true,
                      row.createdAt != nil else {
                    try invalid("An authenticated task has no exact project or creation identity.")
                }
            }
            guard Set(row.messages.map(\.requestID)).count == row.messages.count else {
                try invalid("A durable task contains duplicate message request identities.")
            }
            guard row.messages.allSatisfy({ message in
                validID(message.requestID) && validDigest(message.textDigest)
                    && !message.acceptedAt.isEmpty
                    && (message.deliveredReceiptDigest.map(validDigest) == true
                        || message.deliveredReceiptDigest == nil)
            }) else {
                try invalid("A durable task message receipt is invalid.")
            }
        }
        for row in state.commands {
            let acknowledged = row.stage == .acknowledged
            guard acknowledged == (row.acknowledgedAt != nil) else {
                try invalid("Acknowledgement time and command stage contradict each other.")
            }
            switch row.outcome {
            case .pending:
                guard row.stage == .accepted || row.stage == .executed || row.stage == .delivered else {
                    try invalid("A pending command cannot be observed or acknowledged.")
                }
            case .succeeded, .failed:
                guard row.stage == .delivered || row.stage == .observed
                        || row.stage == .acknowledged,
                      row.evidenceDigest?.isEmpty == false else {
                    try invalid("A terminal command has no monotonic observed evidence.")
                }
            case .interrupted:
                guard row.stage == .accepted else {
                    try invalid("Only an accepted pre-effect command may be interrupted safely.")
                }
            case .unknown:
                guard row.stage != .acknowledged else {
                    try invalid("Acknowledged evidence cannot have unknown outcome.")
                }
            }
        }
    }

    private static func prepareOwnedDirectory(_ path: String, expectedUID: UInt32) throws {
        do {
            try FileManager.default.createDirectory(
                atPath: path, withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700])
        } catch {
            throw LinuxDurableStateFailure(code: "state_directory_unavailable",
                                           message: "A protected durable-state directory cannot be created.")
        }
        var metadata = stat()
        guard lstat(path, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFDIR,
              metadata.st_uid == expectedUID,
              metadata.st_mode & 0o077 == 0 else {
            throw LinuxDurableStateFailure(code: "unsafe_state_path",
                                           message: "A durable-state directory has unsafe ownership or mode.")
        }
    }

    static func atomicWrite(_ data: Data, to path: String, directory: String) throws {
        let temporary = directory + "/.\((path as NSString).lastPathComponent).\(UUID().uuidString.lowercased()).tmp"
        let descriptor = temporary.withCString {
            open($0, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        }
        guard descriptor >= 0 else {
            throw LinuxDurableStateFailure(code: "atomic_write_failed",
                                           message: "The durable temporary file could not be created.")
        }
        var completed = false
        defer {
            _ = close(descriptor)
            if !completed { _ = unlink(temporary) }
        }
        var offset = 0
        try data.withUnsafeBytes { buffer in
            while offset < buffer.count {
                let count = write(descriptor, buffer.baseAddress!.advanced(by: offset),
                                  buffer.count - offset)
                guard count > 0 else {
                    throw LinuxDurableStateFailure(code: "atomic_write_failed",
                                                   message: "The durable temporary write was incomplete.")
                }
                offset += count
            }
        }
        guard fsync(descriptor) == 0, rename(temporary, path) == 0 else {
            throw LinuxDurableStateFailure(code: "atomic_write_failed",
                                           message: "The durable file could not be synced and promoted.")
        }
        let directoryDescriptor = directory.withCString { open($0, O_RDONLY | O_DIRECTORY | O_CLOEXEC) }
        guard directoryDescriptor >= 0 else {
            throw LinuxDurableStateFailure(code: "atomic_write_failed",
                                           message: "The durable parent directory cannot be synced.")
        }
        let syncStatus = fsync(directoryDescriptor)
        _ = close(directoryDescriptor)
        guard syncStatus == 0 else {
            throw LinuxDurableStateFailure(code: "atomic_write_failed",
                                           message: "The durable parent-directory sync failed.")
        }
        completed = true
    }

    private static func syncDirectory(_ directory: String) {
        let descriptor = directory.withCString { open($0, O_RDONLY | O_DIRECTORY | O_CLOEXEC) }
        guard descriptor >= 0 else { return }
        _ = fsync(descriptor)
        _ = close(descriptor)
    }
}

enum LinuxTerminalInventoryEvidence: Equatable {
    case complete(Set<String>)
    case incomplete(String)
}

struct LinuxStartupReconciliationReceipt: Codable, Equatable {
    let authoritative: Bool
    let stateDisposition: LinuxStateLoadDisposition
    let schemaVersion: Int?
    let daemonEpoch: UInt64?
    let status: String
    let terminalPresent: Int
    let terminalMissing: Int
    let terminalUnknown: Int
    let taskTerminal: Int
    let taskReconciling: Int
    let taskUnknown: Int
    let queueRecoverable: Int
    let queueUnknown: Int
    let commandSucceeded: Int
    let commandInterrupted: Int
    let commandUnknown: Int
    let preservedOriginal: String?
    let reason: String?
}

enum LinuxStartupReconciler {
    static func reconcile(store: LinuxDurableStateStore,
                          inventory: LinuxTerminalInventoryEvidence,
                          now: String = ISO8601DateFormatter().string(from: Date())) throws
        -> LinuxStartupReconciliationReceipt {
        let loaded = store.load()
        guard var state = loaded.state, loaded.authoritative else {
            return receipt(for: nil, loaded: loaded, status: "state_non_authoritative")
        }
        if let invalidResult = store.validatePublishedResults(state) {
            return receipt(for: nil, loaded: invalidResult,
                           status: "state_non_authoritative")
        }

        switch inventory {
        case .complete(let observedIDs):
            var known = Set<String>()
            for index in state.terminals.indices {
                known.insert(state.terminals[index].id)
                state.terminals[index].state = observedIDs.contains(state.terminals[index].id)
                    ? .present : .missing
                state.terminals[index].lastObservedAt = now
            }
            for id in observedIDs.subtracting(known).sorted() {
                state.terminals.append(LinuxDurableTerminalRecord(
                    id: id, taskID: nil, state: .present, lastObservedAt: now,
                    evidenceDigest: nil))
            }
        case .incomplete:
            for index in state.terminals.indices {
                state.terminals[index].state = .unknown
            }
        }

        let terminals = Dictionary(uniqueKeysWithValues: state.terminals.map { ($0.id, $0.state) })
        for index in state.tasks.indices {
            switch state.tasks[index].state {
            case .complete, .failed, .resultPublished, .acknowledged:
                break // terminal evidence is immutable across restart
            case .working, .reconciling:
                state.tasks[index].state = .reconciling
            case .queued:
                let recoverable = state.queue.contains {
                    $0.taskID == state.tasks[index].id && $0.sealedPayloadRecoverable
                }
                if !recoverable { state.tasks[index].state = .unknown }
            case .unknown:
                break
            }
            let resultTerminalIsImmutable = state.tasks[index].state == .complete
                || state.tasks[index].state == .failed
                || state.tasks[index].state == .resultPublished
                || state.tasks[index].state == .acknowledged
            if let terminalID = state.tasks[index].terminalID,
               terminals[terminalID] == .unknown,
               !resultTerminalIsImmutable {
                state.tasks[index].state = .reconciling
            }
        }

        for index in state.queue.indices
            where !state.queue[index].sealedPayloadRecoverable
                && state.queue[index].state != .complete {
            state.queue[index].state = .unknown
        }

        for index in state.commands.indices {
            if state.commands[index].stage == .acknowledged
                || state.commands[index].acknowledgedAt != nil {
                // Acknowledged terminal evidence is never weakened by loss of the process that
                // originally produced it.
                continue
            }
            switch state.commands[index].outcome {
            case .succeeded, .failed:
                break
            case .pending, .interrupted, .unknown:
                switch state.commands[index].stage {
                case .accepted:
                    state.commands[index].outcome = .interrupted
                case .executed, .delivered, .observed:
                    state.commands[index].outcome = .unknown
                case .acknowledged:
                    break
                }
            }
        }

        state.daemonEpoch += 1
        state.lastReconciledAt = now
        try store.save(state)
        let status: String
        switch inventory {
        case .complete: status = "complete"
        case .incomplete: status = "inventory_incomplete"
        }
        return receipt(for: state, loaded: loaded, status: status)
    }

    /// Periodic inventory observation is intentionally not startup reconciliation. It may update
    /// terminal presence and health, but it never increments the daemon epoch or rewrites live
    /// task/command restart classifications.
    static func observe(store: LinuxDurableStateStore,
                        inventory: LinuxTerminalInventoryEvidence,
                        now: String = ISO8601DateFormatter().string(from: Date())) throws
        -> LinuxStartupReconciliationReceipt {
        let loaded = store.load()
        guard var state = loaded.state, loaded.authoritative else {
            return receipt(for: nil, loaded: loaded, status: "state_non_authoritative")
        }
        if let invalidResult = store.validatePublishedResults(state) {
            return receipt(for: nil, loaded: invalidResult,
                           status: "state_non_authoritative")
        }
        switch inventory {
        case .complete(let observedIDs):
            var known = Set<String>()
            for index in state.terminals.indices {
                known.insert(state.terminals[index].id)
                state.terminals[index].state = observedIDs.contains(state.terminals[index].id)
                    ? .present : .missing
                state.terminals[index].lastObservedAt = now
            }
            for id in observedIDs.subtracting(known).sorted() {
                state.terminals.append(.init(
                    id: id, taskID: nil, state: .present, lastObservedAt: now,
                    evidenceDigest: nil))
            }
            try store.save(state)
            return receipt(for: state, loaded: loaded, status: "complete")
        case .incomplete:
            for index in state.terminals.indices { state.terminals[index].state = .unknown }
            try store.save(state)
            return receipt(for: state, loaded: loaded, status: "inventory_incomplete")
        }
    }

    private static func receipt(for state: LinuxDurableState?, loaded: LinuxStateLoadOutcome,
                                status: String) -> LinuxStartupReconciliationReceipt {
        let terminals = state?.terminals ?? []
        let tasks = state?.tasks ?? []
        let queue = state?.queue ?? []
        let commands = state?.commands ?? []
        return LinuxStartupReconciliationReceipt(
            authoritative: loaded.authoritative,
            stateDisposition: loaded.disposition,
            schemaVersion: state?.schemaVersion,
            daemonEpoch: state?.daemonEpoch,
            status: status,
            terminalPresent: terminals.filter { $0.state == .present }.count,
            terminalMissing: terminals.filter { $0.state == .missing }.count,
            terminalUnknown: terminals.filter { $0.state == .unknown }.count,
            taskTerminal: tasks.filter {
                $0.state == .complete || $0.state == .failed
                    || $0.state == .resultPublished || $0.state == .acknowledged
            }.count,
            taskReconciling: tasks.filter { $0.state == .reconciling }.count,
            taskUnknown: tasks.filter { $0.state == .unknown }.count,
            queueRecoverable: queue.filter {
                $0.state == .queued && $0.sealedPayloadRecoverable
            }.count,
            queueUnknown: queue.filter { $0.state == .unknown }.count,
            commandSucceeded: commands.filter { $0.outcome == .succeeded }.count,
            commandInterrupted: commands.filter { $0.outcome == .interrupted }.count,
            commandUnknown: commands.filter { $0.outcome == .unknown }.count,
            preservedOriginal: loaded.preservedOriginal,
            reason: loaded.reason)
    }
}

struct LinuxReleaseIdentity: Codable, Equatable {
    let packageVersion: String
    let buildIdentity: String
    let sourceCommit: String
    let packageDigest: String

    static var current: LinuxReleaseIdentity {
        let environment = ProcessInfo.processInfo.environment
        return LinuxReleaseIdentity(
            packageVersion: environment["CLAWDLINE_PACKAGE_VERSION"] ?? "unknown",
            buildIdentity: environment["CLAWDLINE_BUILD_IDENTITY"] ?? "unknown",
            sourceCommit: environment["CLAWDLINE_SOURCE_COMMIT"] ?? "unknown",
            packageDigest: environment["CLAWDLINE_PACKAGE_DIGEST"] ?? "unknown")
    }
}

struct LinuxDaemonHealth: Codable, Equatable {
    let service: String
    let serviceReady: Bool
    let ready: Bool
    let readinessCode: String
    let protocolIdentity: String
    let configurationSchemaVersion: Int
    let configurationReadableMinimum: Int
    let durableSchemaVersion: Int
    let durableReadableMinimum: Int
    let durableReadableMaximum: Int
    let release: LinuxReleaseIdentity
    let reconciliation: LinuxStartupReconciliationReceipt
    let providers: [LinuxProviderCapability]
}

enum LinuxDaemonService {
    static let healthFilename = "health.json"

    static func run(configuration: LinuxDaemonConfiguration, authorization: Data) throws -> Never {
        let runtime = try LinuxProviderRuntime.compose(configuration: configuration)
        let stateStore = try LinuxDurableStateStore(
            stateDirectory: runtime.layout.state, expectedUID: runtime.layout.uid)
        // Cloud durability is a startup dependency, not a best-effort accessory. A corrupt,
        // unsafe or multiply-owned ledger/spool leaves the daemon unstarted; no in-memory store
        // can admit effects or publications in its place.
        let durableCloud = try LinuxDurableCloudRuntime(
            stateDirectory: runtime.layout.state, expectedUID: runtime.layout.uid,
            secrets: runtime.secrets)
        let requestID = "startup-reconciliation"
        runtime.scheduling.setRestartMaintenance(active: true, requestID: requestID)
        let startup = try LinuxStartupReconciler.reconcile(
            store: stateStore, inventory: inventory(from: runtime))
        let owner = LinuxDaemonIngressOwner(
            store: stateStore, runtime: runtime, durableCloud: durableCloud)
        owner.completeStartup(startup)
        // One Relay owner consumes the already-open Application ledger/spool and the same
        // serialized ingress owner. Missing enrollment stays detached; no second state owner is
        // constructed and no candidate W0-E emission permission is consulted or promoted.
        let relayStatus = LinuxRelayRuntimeStatus()
        let relayDiagnostic: @Sendable (String) -> Void = { message in
            FileHandle.standardError.write(Data((message + "\n").utf8))
        }
        let relayOwner = try durableCloud.makeRelayOwner(
            ingress: owner,
            commandsEnabled: { configuration.runtime?.cloudCommandsEnabled == true },
            presentation: LinuxRelayMachinePresentation(
                displayName: configuration.runtime?.displayName ?? "Clawdline Linux",
                provider: configuration.runtime?.infrastructureProvider),
            places: (configuration.runtime?.projectRoots ?? []).map { path in
                LinuxRelayPlace(
                    id: LinuxSHA256.hex(Data(path.utf8)),
                    label: URL(fileURLWithPath: path).lastPathComponent,
                    path: path)
            },
            inventory: {
                (try? runtime.terminal.inventory()) ?? TerminalInventory(
                    error: "tmux inventory was unavailable", isComplete: false)
            },
            diagnostic: relayDiagnostic,
            stateObserver: { relayStatus.record($0) })
        let relaySupervisor = relayOwner.map {
            LinuxRelayRuntimeSupervisor(
                owner: $0, status: relayStatus, diagnostic: relayDiagnostic)
        }
        relaySupervisor?.start()
        let health = makeHealth(
            receipt: startup, providerIdentity: runtime.compositionReceipt.identity,
            cloudReadiness: durableCloud.readiness,
            relayReadinessCode: relaySupervisor?.status.readinessCode)
        try writeHealth(health, runtimeDirectory: runtime.layout.runtime)
        if health.serviceReady {
            runtime.scheduling.setRestartMaintenance(active: false, requestID: requestID)
        }

        // Observation is a separate serialized path. It may refresh terminal presence/health,
        // but can never replay the one startup epoch transition.
        let timer = DispatchSource.makeTimerSource(queue: DispatchQueue(
            label: "app.clawdline.linux-observation"))
        timer.schedule(deadline: .now() + 10, repeating: 10)
        timer.setEventHandler {
            let snapshot = (try? runtime.terminal.inventory()) ?? TerminalInventory(
                error: "tmux inventory could not be read", isComplete: false)
            let evidence = snapshot.isComplete
                ? LinuxTerminalInventoryEvidence.complete(Set(
                    snapshot.sessions.filter { $0.assistant != nil }.map(\.id)))
                : .incomplete(snapshot.error ?? "terminal inventory is incomplete")
            guard let receipt = try? owner.observeInventory(evidence) else { return }
            relaySupervisor?.publishInventory(snapshot)
            let observedHealth = makeHealth(
                receipt: receipt, providerIdentity: runtime.compositionReceipt.identity,
                cloudReadiness: durableCloud.readiness,
                relayReadinessCode: relaySupervisor?.status.readinessCode)
            try? writeHealth(observedHealth, runtimeDirectory: runtime.layout.runtime)
        }
        timer.resume()
        defer { timer.cancel() }
        return try LinuxLocalIngressServer(
            configuration: configuration.listen, authorization: authorization,
            owner: owner, relaySupervisor: relaySupervisor).run()
    }

    static func health(configuration: LinuxDaemonConfiguration) throws -> Data {
        let runtimeDirectory = configuration.runtimeDirectory
            ?? configuration.stateDirectory + "/runtime"
        let path = runtimeDirectory + "/" + healthFilename
        let data: Data
        do {
            data = try Data(contentsOf: URL(fileURLWithPath: path),
                            options: [.mappedIfSafe])
        } catch {
            throw LinuxCompositionError.configuration("daemon health is unavailable")
        }
        let health: LinuxDaemonHealth
        do {
            health = try JSONDecoder().decode(LinuxDaemonHealth.self, from: data)
        } catch {
            throw LinuxCompositionError.configuration("daemon health is unreadable")
        }
        guard health.release == LinuxReleaseIdentity.current else {
            throw LinuxCompositionError.configuration(
                "daemon health belongs to a different release identity")
        }
        var output = data
        if output.last != 0x0a { output.append(0x0a) }
        return output
    }

    static func makeHealth(receipt: LinuxStartupReconciliationReceipt,
                           providerIdentity: LinuxRuntimeIdentity,
                           cloudReadiness: LinuxDurableCloudRuntime.Readiness? = nil,
                           relayReadinessCode: String? = nil)
        -> LinuxDaemonHealth {
        let serviceReady = receipt.authoritative && receipt.status == "complete"
        return LinuxDaemonHealth(
            service: "clawdline-daemon",
            serviceReady: serviceReady,
            ready: false,
            readinessCode: serviceReady
                ? (relayReadinessCode ?? cloudReadiness?.code
                    ?? "w4_provider_authentication_not_proven")
                : "startup_reconciliation_incomplete",
            protocolIdentity: "clawdline-linux-local-health-v1",
            configurationSchemaVersion: LinuxDaemonConfiguration.schemaVersion,
            configurationReadableMinimum: LinuxDaemonConfiguration.readableSchemaVersions.lowerBound,
            durableSchemaVersion: LinuxDurableState.schemaVersion,
            durableReadableMinimum: LinuxDurableState.minimumReadableSchemaVersion,
            durableReadableMaximum: LinuxDurableState.maximumReadableSchemaVersion,
            release: .current,
            reconciliation: receipt,
            providers: providerIdentity.providers)
    }

    static func writeHealth(_ health: LinuxDaemonHealth,
                            runtimeDirectory: String) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let data = try encoder.encode(health)
        try LinuxDurableStateStore.atomicWrite(
            data, to: runtimeDirectory + "/" + healthFilename,
            directory: runtimeDirectory)
    }

    private static func inventory(from runtime: LinuxProviderRuntime)
        -> LinuxTerminalInventoryEvidence {
        do {
            let observed = try runtime.terminal.inventory()
            return observed.isComplete
                ? .complete(Set(observed.sessions.filter { $0.assistant != nil }.map(\.id)))
                : .incomplete(observed.error ?? "terminal inventory is incomplete")
        } catch {
            return .incomplete("terminal inventory could not be read")
        }
    }
}
