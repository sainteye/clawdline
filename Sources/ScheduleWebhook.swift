import CryptoKit
import Darwin
import Foundation
import Security

enum ScheduleWebhookStoreError: Error, Equatable {
    case unavailable
    case bindingConflict
    case deliveryConflict
    case invalidClaim
}

struct ScheduleWebhookBinding: Codable, Equatable, Sendable {
    let hookID: String
    let scheduleID: String
    let boundAt: Int64

    enum CodingKeys: String, CodingKey {
        case hookID = "hook_id"
        case scheduleID = "schedule_id"
        case boundAt = "bound_at"
    }
}

/// The split-mapping authority. Cloud knows the account and machine; only this mode-0600 file
/// knows which local schedule the opaque hook belongs to.
final class ScheduleWebhookBindingStore: @unchecked Sendable {
    static let shared = ScheduleWebhookBindingStore()
    private let fileURL: URL
    private let lock = NSLock()

    init(fileURL: URL = RemoteAuth.directory.appendingPathComponent(
        "schedule-webhooks-v1.json")) {
        self.fileURL = fileURL
    }

    func all() throws -> [ScheduleWebhookBinding] {
        lock.lock(); defer { lock.unlock() }
        return try readUnlocked()
    }

    func binding(forHookID hookID: String) throws -> ScheduleWebhookBinding? {
        try all().first { $0.hookID == hookID }
    }

    func binding(forScheduleID scheduleID: String) throws -> ScheduleWebhookBinding? {
        try all().first { $0.scheduleID == scheduleID }
    }

    @discardableResult
    func bind(hookID: String, scheduleID: String, replaceHookID: String?,
              at: Int64 = Int64(Date().timeIntervalSince1970)) throws
        -> ScheduleWebhookBinding {
        guard Self.validHookID(hookID), Self.validScheduleID(scheduleID),
              replaceHookID == nil || Self.validHookID(replaceHookID!) else {
            throw ScheduleWebhookStoreError.unavailable
        }
        lock.lock(); defer { lock.unlock() }
        var values = try readUnlocked()
        if let same = values.first(where: { $0.hookID == hookID && $0.scheduleID == scheduleID }) {
            return same
        }
        let current = values.first { $0.scheduleID == scheduleID }
        if let replaceHookID {
            guard current?.hookID == replaceHookID else {
                throw ScheduleWebhookStoreError.bindingConflict
            }
            values.removeAll { $0.hookID == replaceHookID }
        } else if current != nil {
            throw ScheduleWebhookStoreError.bindingConflict
        }
        guard !values.contains(where: { $0.hookID == hookID }) else {
            throw ScheduleWebhookStoreError.bindingConflict
        }
        let made = ScheduleWebhookBinding(hookID: hookID, scheduleID: scheduleID, boundAt: at)
        values.append(made)
        try writeUnlocked(values.sorted { $0.hookID < $1.hookID })
        return made
    }

    private func readUnlocked() throws -> [ScheduleWebhookBinding] {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return [] }
        guard let data = try? Data(contentsOf: fileURL),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              Set(object.keys) == ["clawdline_schedule_webhooks", "bindings"],
              Self.strictInteger(object["clawdline_schedule_webhooks"]) == 1,
              let rows = object["bindings"] as? [[String: Any]] else {
            throw ScheduleWebhookStoreError.unavailable
        }
        var values: [ScheduleWebhookBinding] = []
        var hooks = Set<String>()
        var schedules = Set<String>()
        for row in rows {
            guard Set(row.keys) == ["hook_id", "schedule_id", "bound_at"],
                  let hook = row["hook_id"] as? String, Self.validHookID(hook),
                  let schedule = row["schedule_id"] as? String, Self.validScheduleID(schedule),
                  let at = Self.strictInteger(row["bound_at"]),
                  hooks.insert(hook).inserted, schedules.insert(schedule).inserted else {
                throw ScheduleWebhookStoreError.unavailable
            }
            values.append(.init(hookID: hook, scheduleID: schedule, boundAt: at))
        }
        return values
    }

    private func writeUnlocked(_ values: [ScheduleWebhookBinding]) throws {
        let rows = values.map { ["hook_id": $0.hookID, "schedule_id": $0.scheduleID,
                                 "bound_at": $0.boundAt] as [String: Any] }
        let object: [String: Any] = ["clawdline_schedule_webhooks": 1, "bindings": rows]
        guard let data = try? JSONSerialization.data(
            withJSONObject: object, options: [.prettyPrinted, .sortedKeys,
                                               .withoutEscapingSlashes]) else {
            throw ScheduleWebhookStoreError.unavailable
        }
        try ScheduleWebhookFiles.atomicWrite(data, to: fileURL, parentMode: 0o700,
                                             fileMode: 0o600)
    }

    fileprivate static func validHookID(_ value: String) -> Bool {
        guard value.hasPrefix("swh_"), value.count == 30 else { return false }
        return value.dropFirst(4).allSatisfy { "0123456789abcdefghjkmnpqrstvwxyz".contains($0) }
    }

    fileprivate static func validScheduleID(_ value: String) -> Bool {
        UUID(uuidString: value) != nil && value == value.lowercased()
    }

    fileprivate static func strictInteger(_ raw: Any?) -> Int64? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID(),
              !["f", "d"].contains(String(cString: number.objCType)) else { return nil }
        return number.int64Value
    }
}

extension ScheduleWebhookClaim {
    func validate(identity: CloudMachineIdentity, now: Date = Date()) throws {
        guard deliveryID.hasPrefix("swd_"), deliveryID.count == 30,
              ScheduleWebhookBindingStore.validHookID(hookID), hookGeneration > 0,
              attempt > 0, lease.revision > 0,
              deliveryDigest.count == 64,
              deliveryDigest.allSatisfy({ $0.isNumber || ("a"..."f").contains(String($0)) }),
              lease.token.hasPrefix("swhl_"), lease.token.count == 48,
              let deadline = ScheduleWebhookTime.date(lease.expiresAt), deadline > now else {
            throw ScheduleWebhookStoreError.invalidClaim
        }
        let digestObject: CloudJSONValue = .object([
            "schema": .string("clawdline.schedule_webhook.v1"),
            "delivery_id": .string(deliveryID), "hook_id": .string(hookID),
            "hook_generation": .int(Int64(hookGeneration)),
            "account_id": .string(identity.accountID), "machine_id": .string(identity.machineID),
            "accepted_at": .string(acceptedAt), "expires_at": .string(expiresAt),
        ])
        let computed = RemoteAuth.hex(SHA256.hash(
            data: CloudCanonicalJSON.canonicalData(digestObject)))
        guard computed == deliveryDigest else { throw ScheduleWebhookStoreError.invalidClaim }
    }
}

enum ScheduleWebhookDeliveryState: String, Codable, Sendable {
    case prepared
    case durableAccepted = "mac_durable_accepted"
    case dispatchDeferred = "schedule_dispatch_deferred"
    case dispatchAccepted = "schedule_dispatch_accepted"
    case dispatchRefused = "schedule_dispatch_refused"
    case taskTerminal = "task_execution_terminal"
    case outcomeUnknown = "outcome_unknown"
    case expired
    case canceled

    var isTerminal: Bool {
        self == .dispatchRefused || self == .taskTerminal || self == .outcomeUnknown
            || self == .expired || self == .canceled
    }
}

struct ScheduleWebhookDeliveryRow: Codable, Equatable, Sendable {
    let version: Int
    let deliveryID: String
    let deliveryDigest: String
    let hookID: String
    let hookGeneration: Int
    let scheduleID: String?
    var preflightOutcome: String?
    var state: ScheduleWebhookDeliveryState
    var taskID: String?
    var taskSecret: String?
    var taskFileSHA256: String?
    var lastReceiptVersion: Int
    var cloudAcknowledgedThrough: Int
    var leaseToken: String
    var leaseRevision: Int
    var pendingReceipt: ScheduleWebhookReceipt?
    var retryAt: Int64?
    let createdAt: Int64
    var updatedAt: Int64
    var terminalAt: Int64?

    enum CodingKeys: String, CodingKey {
        case version = "clawdline_schedule_webhook_delivery"
        case deliveryID = "delivery_id"
        case deliveryDigest = "delivery_digest"
        case hookID = "hook_id"
        case hookGeneration = "hook_generation"
        case scheduleID = "schedule_id"
        case preflightOutcome = "preflight_outcome"
        case state
        case taskID = "task_id"
        case taskSecret = "task_secret"
        case taskFileSHA256 = "task_file_sha256"
        case lastReceiptVersion = "last_receipt_version"
        case cloudAcknowledgedThrough = "cloud_acknowledged_through"
        case leaseToken = "lease_token"
        case leaseRevision = "lease_revision"
        case pendingReceipt = "pending_receipt"
        case retryAt = "retry_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case terminalAt = "terminal_at"
    }

    func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(version, forKey: .version)
        try values.encode(deliveryID, forKey: .deliveryID)
        try values.encode(deliveryDigest, forKey: .deliveryDigest)
        try values.encode(hookID, forKey: .hookID)
        try values.encode(hookGeneration, forKey: .hookGeneration)
        try values.encode(scheduleID, forKey: .scheduleID)
        try values.encode(preflightOutcome, forKey: .preflightOutcome)
        try values.encode(state, forKey: .state)
        try values.encode(taskID, forKey: .taskID)
        try values.encode(taskSecret, forKey: .taskSecret)
        try values.encode(taskFileSHA256, forKey: .taskFileSHA256)
        try values.encode(lastReceiptVersion, forKey: .lastReceiptVersion)
        try values.encode(cloudAcknowledgedThrough, forKey: .cloudAcknowledgedThrough)
        try values.encode(leaseToken, forKey: .leaseToken)
        try values.encode(leaseRevision, forKey: .leaseRevision)
        try values.encode(pendingReceipt, forKey: .pendingReceipt)
        try values.encode(retryAt, forKey: .retryAt)
        try values.encode(createdAt, forKey: .createdAt)
        try values.encode(updatedAt, forKey: .updatedAt)
        try values.encode(terminalAt, forKey: .terminalAt)
    }
}

/// One file per delivery makes crash recovery a directory scan and prevents an unrelated corrupt
/// row from turning a durable acceptance into an empty global store.
final class ScheduleWebhookDeliveryStore: @unchecked Sendable {
    struct Reservation { let row: ScheduleWebhookDeliveryRow; let created: Bool }
    private let directory: URL
    private let lock = NSLock()
    private static let keys = Set([
        "clawdline_schedule_webhook_delivery", "delivery_id", "delivery_digest", "hook_id",
        "hook_generation", "schedule_id", "preflight_outcome", "state", "task_id", "task_secret",
        "task_file_sha256", "last_receipt_version", "cloud_acknowledged_through",
        "lease_token", "lease_revision", "pending_receipt", "retry_at", "created_at",
        "updated_at", "terminal_at"
    ])

    init(directory: URL = RemoteAuth.directory.appendingPathComponent(
        "schedule-webhook-deliveries", isDirectory: true)) {
        self.directory = directory
    }

    func reserve(claim: ScheduleWebhookClaim, scheduleID: String?, now: Int64) throws -> Reservation {
        lock.lock(); defer { lock.unlock() }
        try ScheduleWebhookFiles.ensureDirectory(directory, mode: 0o700)
        let target = url(claim.deliveryID)
        if FileManager.default.fileExists(atPath: target.path) {
            let existing = try read(target)
            guard existing.deliveryDigest == claim.deliveryDigest,
                  existing.hookID == claim.hookID else {
                throw ScheduleWebhookStoreError.deliveryConflict
            }
            return Reservation(row: existing, created: false)
        }
        let row = ScheduleWebhookDeliveryRow(
            version: 1, deliveryID: claim.deliveryID, deliveryDigest: claim.deliveryDigest,
            hookID: claim.hookID, hookGeneration: claim.hookGeneration,
            scheduleID: scheduleID, preflightOutcome: nil, state: .prepared,
            taskID: nil, taskSecret: nil,
            taskFileSHA256: nil, lastReceiptVersion: 0, cloudAcknowledgedThrough: 0,
            leaseToken: claim.lease.token, leaseRevision: claim.lease.revision,
            pendingReceipt: nil, retryAt: nil, createdAt: now, updatedAt: now, terminalAt: nil)
        let data = try encode(row)
        if try ScheduleWebhookFiles.exclusiveWrite(data, to: target, parentMode: 0o700,
                                                   fileMode: 0o600) {
            return Reservation(row: row, created: true)
        }
        let raced = try read(target)
        guard raced.deliveryDigest == claim.deliveryDigest, raced.hookID == claim.hookID else {
            throw ScheduleWebhookStoreError.deliveryConflict
        }
        return Reservation(row: raced, created: false)
    }

    func row(deliveryID: String) throws -> ScheduleWebhookDeliveryRow? {
        lock.lock(); defer { lock.unlock() }
        let target = url(deliveryID)
        guard FileManager.default.fileExists(atPath: target.path) else { return nil }
        return try read(target)
    }

    func rows() throws -> [ScheduleWebhookDeliveryRow] {
        lock.lock(); defer { lock.unlock() }
        guard FileManager.default.fileExists(atPath: directory.path) else { return [] }
        let files = try FileManager.default.contentsOfDirectory(
            at: directory, includingPropertiesForKeys: nil, options: [.skipsHiddenFiles])
        var valid: [ScheduleWebhookDeliveryRow] = []
        for file in files where file.pathExtension == "json" {
            do { valid.append(try read(file)) }
            catch {
                quarantine(file)
                RemoteAuth.audit("schedule_webhook.delivery.quarantined", [
                    "file": String(file.lastPathComponent.prefix(96)),
                    "result": "corrupt_row",
                ])
            }
        }
        return valid
    }

    func save(_ row: ScheduleWebhookDeliveryRow) throws {
        lock.lock(); defer { lock.unlock() }
        try ScheduleWebhookFiles.atomicWrite(try encode(row), to: url(row.deliveryID),
                                             parentMode: 0o700, fileMode: 0o600)
    }

    @discardableResult
    func cleanup(now: Int64, retention: Int64 = 30 * 24 * 3600) throws -> Int {
        lock.lock(); defer { lock.unlock() }
        guard FileManager.default.fileExists(atPath: directory.path) else { return 0 }
        var removed = 0
        for file in try FileManager.default.contentsOfDirectory(
            at: directory, includingPropertiesForKeys: nil, options: [.skipsHiddenFiles])
            where file.pathExtension == "json" {
            guard let row = try? read(file) else {
                quarantine(file)
                continue
            }
            guard row.state.isTerminal, row.cloudAcknowledgedThrough >= row.lastReceiptVersion,
                  let terminal = row.terminalAt, terminal + retention <= now else { continue }
            try FileManager.default.removeItem(at: file)
            removed += 1
        }
        return removed
    }

    private func quarantine(_ file: URL) {
        let quarantine = directory.appendingPathComponent("quarantine", isDirectory: true)
        do {
            try ScheduleWebhookFiles.ensureDirectory(quarantine, mode: 0o700)
            var target = quarantine.appendingPathComponent(file.lastPathComponent)
            if FileManager.default.fileExists(atPath: target.path) {
                target = quarantine.appendingPathComponent(
                    file.deletingPathExtension().lastPathComponent + "-" + UUID().uuidString.lowercased()
                        + ".json")
            }
            try FileManager.default.moveItem(at: file, to: target)
        } catch {
            // The unreadable bytes stay in place if quarantine itself is unavailable. Enumeration
            // still isolates this row so it cannot stop unrelated deliveries.
        }
    }

    private func url(_ deliveryID: String) -> URL {
        directory.appendingPathComponent(deliveryID + ".json")
    }

    private func encode(_ row: ScheduleWebhookDeliveryRow) throws -> Data {
        guard row.version == 1, row.deliveryID.hasPrefix("swd_"),
              row.deliveryDigest.count == 64,
              ScheduleWebhookBindingStore.validHookID(row.hookID),
              row.scheduleID == nil || ScheduleWebhookBindingStore.validScheduleID(row.scheduleID!)
        else { throw ScheduleWebhookStoreError.unavailable }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return try encoder.encode(row)
    }

    private func read(_ file: URL) throws -> ScheduleWebhookDeliveryRow {
        guard let data = try? Data(contentsOf: file),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              Set(object.keys) == Self.keys,
              let row = try? JSONDecoder().decode(ScheduleWebhookDeliveryRow.self, from: data),
              row.version == 1, row.deliveryID + ".json" == file.lastPathComponent,
              row.deliveryDigest.count == 64,
              ScheduleWebhookBindingStore.validHookID(row.hookID),
              row.scheduleID == nil || ScheduleWebhookBindingStore.validScheduleID(row.scheduleID!)
        else { throw ScheduleWebhookStoreError.unavailable }
        return row
    }
}

enum ScheduleWebhookBackoff {
    static func emptyPollMilliseconds(server: Int, unitRandom: Double) -> Int {
        let bounded = min(max(server, 0), 300_000)
        let factor = 0.8 + min(max(unitRandom, 0), 1) * 0.4
        return Int((Double(bounded) * factor).rounded())
    }

    static func networkMilliseconds(failure: Int, unitRandom: Double) -> Int {
        let exponent = min(max(failure - 1, 0), 5)
        let cap = min(30_000, 1_000 * (1 << exponent))
        return Int((Double(cap) * min(max(unitRandom, 0), 1)).rounded())
    }

    static func deferredMilliseconds(attempt: Int, unitRandom: Double) -> Int {
        let exponent = min(max(attempt - 1, 0), 5)
        let cap = min(60_000, 1_000 * (1 << exponent))
        return Int((Double(cap) * min(max(unitRandom, 0), 1)).rounded())
    }
}

protocol ScheduleWebhookCloudAPI: Sendable {
    func activateScheduleWebhook(hookID: String, expectedRevision: Int,
                                 idempotencyKey: String) async throws
        -> ScheduleWebhookActivation
    func claimScheduleWebhookDelivery(waitSeconds: Int) async throws
        -> ScheduleWebhookClaimResult
    func sendScheduleWebhookReceipt(deliveryID: String, receipt: ScheduleWebhookReceipt)
        async throws -> ScheduleWebhookReceiptAck
}

extension CloudAccountClient: ScheduleWebhookCloudAPI {}

enum ScheduleWebhookProjection {
    static func apply(scheduleID: String, to record: inout [String: Any],
                      store: ScheduleWebhookBindingStore = .shared) {
        do {
            if let binding = try store.binding(forScheduleID: scheduleID) {
                record["webhook_hook_id"] = binding.hookID
                record["webhook_binding_availability"] = "active"
            } else {
                record.removeValue(forKey: "webhook_hook_id")
                record["webhook_binding_availability"] = "unbound"
            }
        } catch {
            record.removeValue(forKey: "webhook_hook_id")
            record["webhook_binding_availability"] = "binding_store_unavailable"
        }
    }
}

final class ScheduleWebhookBindLedger: @unchecked Sendable {
    enum Begin {
        case fresh
        case pending
        case completed(CloudCommandResult)
        case conflict
    }

    private let directory: URL
    private let lock = NSLock()

    init(directory: URL = RemoteAuth.directory.appendingPathComponent(
        "schedule-webhook-bind-requests", isDirectory: true)) {
        self.directory = directory
    }

    func begin(requestID: String, digest: String, now: Int64) throws -> Begin {
        guard ScheduleWebhookBindingStore.validScheduleID(requestID), digest.count == 64,
              digest.allSatisfy({ "0123456789abcdef".contains($0) }) else {
            throw ScheduleWebhookStoreError.unavailable
        }
        lock.lock(); defer { lock.unlock() }
        try ScheduleWebhookFiles.ensureDirectory(directory, mode: 0o700)
        let target = url(requestID)
        if FileManager.default.fileExists(atPath: target.path) {
            let row = try read(target)
            guard row.digest == digest else { return .conflict }
            guard let status = row.status, let body = row.body else { return .pending }
            return .completed(.init(status: status, code: row.code, body: body))
        }
        try write(Row(requestID: requestID, digest: digest, status: nil, code: nil,
                      body: nil, createdAt: now, completedAt: nil), to: target)
        return .fresh
    }

    func complete(requestID: String, digest: String, result: CloudCommandResult,
                  now: Int64) throws {
        lock.lock(); defer { lock.unlock() }
        let target = url(requestID)
        let row = try read(target)
        guard row.digest == digest else { throw ScheduleWebhookStoreError.deliveryConflict }
        try write(Row(requestID: requestID, digest: digest, status: result.status,
                      code: result.code, body: result.body, createdAt: row.createdAt,
                      completedAt: now), to: target)
    }

    private struct Row {
        let requestID: String
        let digest: String
        let status: Int?
        let code: String?
        let body: Data?
        let createdAt: Int64
        let completedAt: Int64?
    }

    private func url(_ requestID: String) -> URL {
        directory.appendingPathComponent(requestID + ".json")
    }

    private func read(_ file: URL) throws -> Row {
        guard let data = try? Data(contentsOf: file),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              Set(object.keys) == ["clawdline_schedule_webhook_bind", "request_id", "digest",
                                   "status", "code", "body", "created_at", "completed_at"],
              ScheduleWebhookBindingStore.strictInteger(
                object["clawdline_schedule_webhook_bind"]) == 1,
              let requestID = object["request_id"] as? String,
              ScheduleWebhookBindingStore.validScheduleID(requestID),
              requestID + ".json" == file.lastPathComponent,
              let digest = object["digest"] as? String, digest.count == 64,
              digest.allSatisfy({ "0123456789abcdef".contains($0) }),
              let createdAt = ScheduleWebhookBindingStore.strictInteger(object["created_at"])
        else { throw ScheduleWebhookStoreError.unavailable }
        let status = object["status"] is NSNull ? nil
            : ScheduleWebhookBindingStore.strictInteger(object["status"]).map(Int.init)
        let code = object["code"] is NSNull ? nil : object["code"] as? String
        let body: Data?
        if object["body"] is NSNull { body = nil }
        else if let encoded = object["body"] as? String { body = Data(base64Encoded: encoded) }
        else { throw ScheduleWebhookStoreError.unavailable }
        let completedAt = object["completed_at"] is NSNull ? nil
            : ScheduleWebhookBindingStore.strictInteger(object["completed_at"])
        guard (object["status"] is NSNull) || status != nil,
              status == nil || (100...599).contains(status!),
              (object["code"] is NSNull) || code != nil,
              (object["completed_at"] is NSNull) || completedAt != nil,
              (status == nil) == (body == nil),
              (status == nil) == (completedAt == nil) else {
            throw ScheduleWebhookStoreError.unavailable
        }
        return Row(requestID: requestID, digest: digest, status: status, code: code,
                   body: body, createdAt: createdAt, completedAt: completedAt)
    }

    private func write(_ row: Row, to file: URL) throws {
        let object: [String: Any] = [
            "clawdline_schedule_webhook_bind": 1, "request_id": row.requestID,
            "digest": row.digest, "status": row.status.map { $0 as Any } ?? NSNull(),
            "code": row.code.map { $0 as Any } ?? NSNull(),
            "body": row.body.map { $0.base64EncodedString() as Any } ?? NSNull(),
            "created_at": row.createdAt,
            "completed_at": row.completedAt.map { $0 as Any } ?? NSNull(),
        ]
        let data = try JSONSerialization.data(withJSONObject: object,
            options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        try ScheduleWebhookFiles.atomicWrite(data, to: file, parentMode: 0o700, fileMode: 0o600)
    }
}

/// Completes the E2EE bind command without ever accepting a trigger token or borrowing the
/// local orchestrator credential. The local mapping is durable before Cloud activation, so an
/// HTTP response loss is repaired by replaying the same request id.
actor ScheduleWebhookBindingCoordinator {
    static let shared = ScheduleWebhookBindingCoordinator()

    private let store: ScheduleWebhookBindingStore
    private let cloud: any ScheduleWebhookCloudAPI
    private let ledger: ScheduleWebhookBindLedger

    init(store: ScheduleWebhookBindingStore = .shared,
         cloud: any ScheduleWebhookCloudAPI = CloudAccountClient(),
         ledger: ScheduleWebhookBindLedger = ScheduleWebhookBindLedger()) {
        self.store = store
        self.cloud = cloud
        self.ledger = ledger
    }

    func bind(requestID: String, hookID: String, scheduleID: String,
              replaceHookID: String?, sender: String = "unknown") async -> CloudCommandResult {
        let digest = RemoteAuth.hex(SHA256.hash(data: CloudCanonicalJSON.canonicalData(.object([
            "hook_id": .string(hookID), "schedule_id": .string(scheduleID),
            "replace_hook_id": replaceHookID.map(CloudJSONValue.string) ?? .null,
        ]))))
        do {
            switch try ledger.begin(requestID: requestID, digest: digest,
                                    now: Int64(Date().timeIntervalSince1970)) {
            case .completed(let result):
                Self.audit(result, requestID, sender)
                return result
            case .conflict: return Self.auditedFailure(409, "idempotency_conflict", requestID,
                                                       sender)
            case .fresh, .pending: break
            }
        } catch {
            return Self.auditedFailure(503, "binding_store_unavailable", requestID, sender)
        }
        guard Orchestrator.scheduleRecord(id: scheduleID) != nil else {
            let result = Self.failure(404, "schedule_not_found")
            do {
                try ledger.complete(requestID: requestID, digest: digest, result: result,
                                    now: Int64(Date().timeIntervalSince1970))
            } catch {
                return Self.auditedFailure(503, "binding_store_unavailable", requestID, sender)
            }
            Self.audit(result, requestID, sender)
            return result
        }
        let result: CloudCommandResult
        do {
            _ = try store.bind(hookID: hookID, scheduleID: scheduleID,
                               replaceHookID: replaceHookID)
            let activated = try await cloud.activateScheduleWebhook(
                hookID: hookID, expectedRevision: 0, idempotencyKey: requestID)
            result = Self.answer(200, [
                "accepted": true, "hook_id": hookID, "schedule_id": scheduleID,
                "binding_state": "active", "hook_revision": activated.revision,
            ])
        } catch ScheduleWebhookStoreError.bindingConflict {
            result = Self.failure(409, "binding_conflict")
        } catch ScheduleWebhookStoreError.unavailable {
            result = Self.failure(503, "binding_store_unavailable")
        } catch let error as CloudAccountError {
            switch error {
            case .http(let status, let code):
                result = Self.failure(status, code ?? "temporarily_unavailable")
            case .missingMachineCredential:
                result = Self.failure(401, "no_machine_credential")
            default:
                result = Self.failure(503, "temporarily_unavailable")
            }
        } catch {
            result = Self.failure(503, "temporarily_unavailable")
        }
        if result.status < 500 {
            do {
                try ledger.complete(requestID: requestID, digest: digest, result: result,
                                    now: Int64(Date().timeIntervalSince1970))
            } catch {
                return Self.auditedFailure(503, "binding_store_unavailable", requestID, sender)
            }
        }
        Self.audit(result, requestID, sender)
        return result
    }

    private static func answer(_ status: Int, _ object: [String: Any],
                               code: String? = nil) -> CloudCommandResult {
        let body = (try? JSONSerialization.data(withJSONObject: object,
            options: [.sortedKeys, .withoutEscapingSlashes])) ?? Data()
        return CloudCommandResult(status: status, code: code, body: body)
    }

    private static func failure(_ status: Int, _ code: String) -> CloudCommandResult {
        answer(status, ["error": ["code": code, "message": code]], code: code)
    }

    private static func auditedFailure(_ status: Int, _ code: String, _ requestID: String,
                                       _ sender: String) -> CloudCommandResult {
        let result = failure(status, code)
        audit(result, requestID, sender)
        return result
    }

    private static func audit(_ result: CloudCommandResult, _ requestID: String,
                              _ sender: String) {
        RemoteAuth.audit("schedule_webhook.bind", [
            "request": String(requestID.prefix(64)), "sender": String(sender.prefix(64)),
            "status": String(result.status), "result": result.code ?? "accepted",
        ])
    }
}

enum ScheduleWebhookPreflight: Equatable {
    case eligible(Orchestrator.Schedule)
    case refused(String)

    static func == (lhs: ScheduleWebhookPreflight, rhs: ScheduleWebhookPreflight) -> Bool {
        switch (lhs, rhs) {
        case (.eligible(let left), .eligible(let right)): return left.id == right.id
        case (.refused(let left), .refused(let right)): return left == right
        default: return false
        }
    }
}

/// The only bridge from a webhook delivery into ordinary scheduling. It materializes the same
/// task.json shape as a clock/manual schedule, and enters the existing dispatch gate with the
/// journal's stable task id and secret.
enum ScheduleWebhookOrchestratorBridge {
    static func preflight(scheduleID: String) -> ScheduleWebhookPreflight {
        guard Config.shared.orchestratorEnabled else { return .refused("orchestrator_disabled") }
        guard let schedule = Orchestrator.schedules().first(where: { $0.id == scheduleID }) else {
            let invalid = Orchestrator.scheduleRecords().contains {
                ($0["file"] as? String) == scheduleID + ".json"
                    && ($0["state"] as? String) == "invalid"
            }
            return .refused(invalid ? "schedule_invalid" : "schedule_not_found")
        }
        guard schedule.enabled else { return .refused("schedule_disabled") }
        guard schedule.firedAt == nil else { return .refused("schedule_spent") }
        if let record = Orchestrator.scheduleRecord(id: scheduleID),
           let runs = record["runs"] as? [[String: Any]],
           runs.contains(where: { !Self.terminalStates.contains($0["state"] as? String ?? "") }) {
            return .refused("schedule_active")
        }
        return .eligible(schedule)
    }

    static func materialize(schedule: Orchestrator.Schedule, taskID: String) throws -> String {
        var object = schedule.taskTemplate
        object["clawdline_protocol"] = 1
        object["task_id"] = taskID
        object["root"] = ["session_id": NSNull(), "label": schedule.title]
        let directory = Orchestrator.root.appendingPathComponent(taskID, isDirectory: true)
        try ScheduleWebhookFiles.ensureDirectory(directory, mode: 0o700)
        let data = try JSONSerialization.data(withJSONObject: object,
            options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        try ScheduleWebhookFiles.atomicWrite(data,
            to: directory.appendingPathComponent("task.json"), parentMode: 0o700, fileMode: 0o600)
        return RemoteAuth.hex(SHA256.hash(data: data))
    }

    static func dispatch(schedule: Orchestrator.Schedule, taskID: String,
                         taskSecret: String) -> (accepted: Bool, code: String?) {
        switch Orchestrator.dispatch(taskID: taskID, secret: taskSecret, schedule: schedule) {
        case .ok: return (true, nil)
        case .refused(_, let code, _, _):
            if code == "over_capacity" { return (false, "over_capacity") }
            if code == "terminal_busy" { return (false, "terminal_busy") }
            if code == "orchestrator_disabled" { return (false, code) }
            return (false, "dispatch_failed")
        }
    }

    static func taskState(scheduleID: String, taskID: String) -> String? {
        guard let record = Orchestrator.scheduleRecord(id: scheduleID),
              let runs = record["runs"] as? [[String: Any]] else { return nil }
        return runs.first { $0["task_id"] as? String == taskID }?["state"] as? String
    }

    static let terminalStates = Set(["success", "failure", "timed_out", "cancelled",
                                     "spawn_failed"])
}

struct ScheduleWebhookPreparation: Equatable, Sendable {
    let taskFileSHA256: String?
    let outcome: String?
}

protocol ScheduleWebhookEffecting: Sendable {
    func prepare(scheduleID: String, taskID: String) throws -> ScheduleWebhookPreparation
    func dispatch(scheduleID: String, taskID: String, taskSecret: String)
        -> (accepted: Bool, code: String?)
    func taskState(scheduleID: String, taskID: String) -> String?
    func remove(taskID: String)
}

struct ScheduleWebhookOrchestratorEffect: ScheduleWebhookEffecting {
    func prepare(scheduleID: String, taskID: String) throws -> ScheduleWebhookPreparation {
        switch ScheduleWebhookOrchestratorBridge.preflight(scheduleID: scheduleID) {
        case .refused(let code): return .init(taskFileSHA256: nil, outcome: code)
        case .eligible(let schedule):
            return .init(taskFileSHA256: try ScheduleWebhookOrchestratorBridge.materialize(
                schedule: schedule, taskID: taskID), outcome: nil)
        }
    }

    func dispatch(scheduleID: String, taskID: String, taskSecret: String)
        -> (accepted: Bool, code: String?) {
        guard case .eligible(let schedule) = ScheduleWebhookOrchestratorBridge.preflight(
            scheduleID: scheduleID) else { return (false, "outcome_unknown") }
        return ScheduleWebhookOrchestratorBridge.dispatch(
            schedule: schedule, taskID: taskID, taskSecret: taskSecret)
    }

    func taskState(scheduleID: String, taskID: String) -> String? {
        ScheduleWebhookOrchestratorBridge.taskState(scheduleID: scheduleID, taskID: taskID)
    }

    func remove(taskID: String) {
        try? FileManager.default.removeItem(
            at: Orchestrator.root.appendingPathComponent(taskID, isDirectory: true))
    }
}

/// Durable admission and receipt state machine. Every receipt is written into the row before it
/// is sent; a response-loss retry therefore resends byte-for-byte the same Codable value/version.
actor ScheduleWebhookDeliveryProcessor {
    private let identity: CloudMachineIdentity
    private let cloud: any ScheduleWebhookCloudAPI
    private let bindings: ScheduleWebhookBindingStore
    private let deliveries: ScheduleWebhookDeliveryStore
    private let effect: any ScheduleWebhookEffecting
    private let now: @Sendable () -> Date
    private let unitRandom: @Sendable () -> Double
    private let afterDispatch: @Sendable () throws -> Void

    init(identity: CloudMachineIdentity, cloud: any ScheduleWebhookCloudAPI,
         bindings: ScheduleWebhookBindingStore = ScheduleWebhookBindingStore(),
         deliveries: ScheduleWebhookDeliveryStore = ScheduleWebhookDeliveryStore(),
         effect: any ScheduleWebhookEffecting = ScheduleWebhookOrchestratorEffect(),
         now: @escaping @Sendable () -> Date = { Date() },
         unitRandom: @escaping @Sendable () -> Double = { Double.random(in: 0...1) },
         afterDispatch: @escaping @Sendable () throws -> Void = {}) {
        self.identity = identity
        self.cloud = cloud
        self.bindings = bindings
        self.deliveries = deliveries
        self.effect = effect
        self.now = now
        self.unitRandom = unitRandom
        self.afterDispatch = afterDispatch
    }

    func pollOnce(waitSeconds: Int = 20) async throws -> Int {
        try await resumeRows()
        let result = try await cloud.claimScheduleWebhookDelivery(waitSeconds: waitSeconds)
        guard let claim = result.delivery else { return result.pollAfterMilliseconds }
        try claim.validate(identity: identity, now: now())
        try await admit(claim)
        return 0
    }

    func resumeRows() async throws {
        for row in try deliveries.rows()
            where !row.state.isTerminal || row.pendingReceipt != nil {
            try await advance(row)
        }
        _ = try deliveries.cleanup(now: epoch())
    }

    private func admit(_ claim: ScheduleWebhookClaim) async throws {
        let binding: ScheduleWebhookBinding?
        do { binding = try bindings.binding(forHookID: claim.hookID) }
        catch {
            try await reserveAndRefuse(claim, scheduleID: nil,
                                      outcome: "binding_store_unavailable")
            return
        }
        guard let binding else {
            try await reserveAndRefuse(claim, scheduleID: nil, outcome: "hook_unbound")
            return
        }
        let reservation = try deliveries.reserve(
            claim: claim, scheduleID: binding.scheduleID, now: epoch())
        var row = reservation.row
        if row.cloudAcknowledgedThrough == 0 {
            row.leaseToken = claim.lease.token
            row.leaseRevision = claim.lease.revision
            if let pending = row.pendingReceipt, pending.receiptVersion == 1 {
                row.pendingReceipt = ScheduleWebhookReceipt(
                    schema: pending.schema, receiptVersion: pending.receiptVersion,
                    previousReceiptVersion: pending.previousReceiptVersion, kind: pending.kind,
                    occurredAt: pending.occurredAt, macBuild: pending.macBuild,
                    leaseToken: claim.lease.token, taskID: pending.taskID,
                    outcomeCode: pending.outcomeCode,
                    taskTerminalState: pending.taskTerminalState, retryAt: pending.retryAt)
            }
            try deliveries.save(row)
        }
        if reservation.created {
            let taskID = UUID().uuidString.lowercased()
            guard let secret = Self.taskSecret() else {
                row.preflightOutcome = "dispatch_failed"
                try deliveries.save(row)
                try await advance(row)
                return
            }
            row.taskID = taskID
            row.taskSecret = secret
            try deliveries.save(row)
            do {
                let prepared = try effect.prepare(scheduleID: binding.scheduleID, taskID: taskID)
                row.taskFileSHA256 = prepared.taskFileSHA256
                row.preflightOutcome = prepared.outcome
            } catch {
                row.preflightOutcome = "dispatch_failed"
            }
            try deliveries.save(row)
        }
        try await advance(row)
    }

    private func reserveAndRefuse(_ claim: ScheduleWebhookClaim, scheduleID: String?,
                                  outcome: String) async throws {
        var row = try deliveries.reserve(claim: claim, scheduleID: scheduleID,
                                         now: epoch()).row
        row.preflightOutcome = outcome
        row.leaseToken = claim.lease.token
        row.leaseRevision = claim.lease.revision
        try deliveries.save(row)
        try await advance(row)
    }

    private func advance(_ original: ScheduleWebhookDeliveryRow) async throws {
        var row = original
        if let pending = row.pendingReceipt {
            do {
                let ack = try await cloud.sendScheduleWebhookReceipt(
                    deliveryID: row.deliveryID, receipt: pending)
                row.cloudAcknowledgedThrough = ack.receiptVersion
                row.pendingReceipt = nil
                try deliveries.save(row)
            } catch let error as CloudAccountError {
                if case .http(_, let code) = error, code == "delivery_canceled" {
                    row.state = .canceled
                    row.terminalAt = epoch()
                    row.pendingReceipt = nil
                    try deliveries.save(row)
                    removePreparedTask(row)
                    return
                }
                if case .http(_, let code) = error, code == "stale_lease",
                   pending.receiptVersion == 1 {
                    return
                }
                if case .http(_, let code) = error, code == "delivery_expired" {
                    row.state = .expired
                    row.terminalAt = epoch()
                    row.pendingReceipt = nil
                    try deliveries.save(row)
                    removePreparedTask(row)
                    return
                }
                throw error
            }
        }
        guard row.pendingReceipt == nil else { return }
        if row.cloudAcknowledgedThrough == 0 {
            try queueReceipt(&row, kind: "mac_durable_accepted", state: .durableAccepted)
            try await advance(row)
            return
        }
        if row.state == .durableAccepted || row.state == .dispatchDeferred {
            if row.state == .dispatchDeferred, let retryAt = row.retryAt,
               retryAt > epoch() { return }
            row.retryAt = nil
            if let refusal = row.preflightOutcome {
                try queueReceipt(&row, kind: "schedule_dispatch_refused",
                                 state: .dispatchRefused, outcome: refusal, terminal: true)
                try await advance(row)
                return
            }
            guard let scheduleID = row.scheduleID, let taskID = row.taskID,
                  let secret = row.taskSecret else {
                try queueReceipt(&row, kind: "schedule_dispatch_refused",
                                 state: .outcomeUnknown, outcome: "outcome_unknown", terminal: true)
                try await advance(row)
                return
            }
            if effect.taskState(scheduleID: scheduleID, taskID: taskID) != nil {
                try queueReceipt(&row, kind: "schedule_dispatch_accepted",
                                 state: .dispatchAccepted)
                try await advance(row)
                return
            }
            let dispatched = effect.dispatch(
                scheduleID: scheduleID, taskID: taskID, taskSecret: secret)
            try afterDispatch()
            if dispatched.accepted {
                try queueReceipt(&row, kind: "schedule_dispatch_accepted",
                                 state: .dispatchAccepted)
            } else if dispatched.code == "over_capacity" || dispatched.code == "terminal_busy" {
                let delay = ScheduleWebhookBackoff.deferredMilliseconds(
                    attempt: max(1, row.lastReceiptVersion), unitRandom: unitRandom())
                row.retryAt = epoch() + Int64((delay + 999) / 1000)
                try queueReceipt(&row, kind: "schedule_dispatch_deferred",
                                 state: .dispatchDeferred, outcome: dispatched.code,
                                 retryAt: ScheduleWebhookTime.string(
                                    Date(timeIntervalSince1970: TimeInterval(row.retryAt!))))
            } else {
                try queueReceipt(&row, kind: "schedule_dispatch_refused",
                                 state: .dispatchRefused,
                                 outcome: dispatched.code ?? "dispatch_failed", terminal: true)
            }
            try await advance(row)
            return
        }
        if row.state == .dispatchAccepted, let scheduleID = row.scheduleID,
           let taskID = row.taskID,
           let state = effect.taskState(scheduleID: scheduleID, taskID: taskID),
           ScheduleWebhookOrchestratorBridge.terminalStates.contains(state) {
            try queueReceipt(&row, kind: "task_execution_terminal", state: .taskTerminal,
                             terminalState: state, terminal: true)
            try await advance(row)
        }
    }

    private func queueReceipt(_ row: inout ScheduleWebhookDeliveryRow, kind: String,
                              state: ScheduleWebhookDeliveryState, outcome: String? = nil,
                              terminalState: String? = nil, retryAt: String? = nil,
                              terminal: Bool = false) throws {
        let next = row.lastReceiptVersion + 1
        row.pendingReceipt = ScheduleWebhookReceipt(
            schema: "clawdline.schedule_webhook.receipt.v1", receiptVersion: next,
            previousReceiptVersion: row.lastReceiptVersion, kind: kind,
            occurredAt: ScheduleWebhookTime.string(now()), macBuild: Self.macBuild,
            leaseToken: next == 1 ? row.leaseToken : nil,
            taskID: row.taskID, outcomeCode: outcome,
            taskTerminalState: terminalState, retryAt: retryAt)
        row.lastReceiptVersion = next
        row.state = state
        row.updatedAt = epoch()
        if terminal { row.terminalAt = row.updatedAt }
        try deliveries.save(row)
    }

    private func removePreparedTask(_ row: ScheduleWebhookDeliveryRow) {
        guard let taskID = row.taskID else { return }
        effect.remove(taskID: taskID)
    }

    private func epoch() -> Int64 { Int64(now().timeIntervalSince1970) }

    private static func taskSecret() -> String? {
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            return nil
        }
        return RemoteAuth.hex(bytes)
    }

    private static var macBuild: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString")
            as? String ?? "unknown"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String
            ?? "unknown"
        return version + "+" + build
    }
}

actor ScheduleWebhookPoller {
    private var loopTask: Task<Void, Never>?

    func start(identity: CloudMachineIdentity, cloud: any ScheduleWebhookCloudAPI,
               onUnauthorized: @escaping @Sendable () -> Void) {
        start(processor: ScheduleWebhookDeliveryProcessor(identity: identity, cloud: cloud),
              onUnauthorized: onUnauthorized)
    }

    func start(processor: ScheduleWebhookDeliveryProcessor,
               onUnauthorized: @escaping @Sendable () -> Void) {
        loopTask?.cancel()
        loopTask = Task {
            var failures = 0
            while !Task.isCancelled {
                do {
                    let delay = try await processor.pollOnce()
                    failures = 0
                    try await Self.sleep(milliseconds:
                        ScheduleWebhookBackoff.emptyPollMilliseconds(
                            server: delay, unitRandom: Double.random(in: 0...1)))
                } catch let error as CloudAccountError {
                    if case .http(let status, let code) = error,
                       status == 401 && code == "no_machine_credential" {
                        onUnauthorized()
                        return
                    }
                    failures += 1
                    try? await Self.sleep(milliseconds:
                        ScheduleWebhookBackoff.networkMilliseconds(
                            failure: failures, unitRandom: Double.random(in: 0...1)))
                } catch is CancellationError {
                    return
                } catch {
                    failures += 1
                    try? await Self.sleep(milliseconds:
                        ScheduleWebhookBackoff.networkMilliseconds(
                            failure: failures, unitRandom: Double.random(in: 0...1)))
                }
            }
        }
    }

    func stop() { loopTask?.cancel(); loopTask = nil }

    private static func sleep(milliseconds: Int) async throws {
        guard milliseconds > 0 else { return }
        try await Task.sleep(nanoseconds: UInt64(milliseconds) * 1_000_000)
    }
}

actor ScheduleWebhookRuntime {
    static let shared = ScheduleWebhookRuntime()
    private let poller = ScheduleWebhookPoller()
    private let cloud: any ScheduleWebhookCloudAPI

    init(cloud: any ScheduleWebhookCloudAPI = CloudAccountClient()) { self.cloud = cloud }

    func start(identity: CloudMachineIdentity,
               onUnauthorized: @escaping @Sendable () -> Void) async {
        await poller.start(identity: identity, cloud: cloud, onUnauthorized: onUnauthorized)
    }

    func stop() async { await poller.stop() }
}

private enum ScheduleWebhookTime {
    static let formatter: ISO8601DateFormatter = {
        let value = ISO8601DateFormatter()
        value.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return value
    }()

    static func date(_ value: String) -> Date? {
        formatter.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }

    static func string(_ date: Date) -> String { formatter.string(from: date) }
}

private enum ScheduleWebhookFiles {
    static func ensureDirectory(_ directory: URL, mode: Int) throws {
        let manager = FileManager.default
        if !manager.fileExists(atPath: directory.path) {
            try manager.createDirectory(at: directory, withIntermediateDirectories: true,
                                        attributes: [.posixPermissions: mode])
        }
        try manager.setAttributes([.posixPermissions: mode], ofItemAtPath: directory.path)
    }

    static func atomicWrite(_ data: Data, to target: URL, parentMode: Int,
                            fileMode: Int) throws {
        try ensureDirectory(target.deletingLastPathComponent(), mode: parentMode)
        let staging = target.deletingLastPathComponent().appendingPathComponent(
            ".\(target.lastPathComponent).\(UUID().uuidString).new")
        try data.write(to: staging, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: fileMode],
                                              ofItemAtPath: staging.path)
        if FileManager.default.fileExists(atPath: target.path) {
            _ = try FileManager.default.replaceItemAt(target, withItemAt: staging)
        } else {
            try FileManager.default.moveItem(at: staging, to: target)
        }
        try FileManager.default.setAttributes([.posixPermissions: fileMode],
                                              ofItemAtPath: target.path)
    }

    static func exclusiveWrite(_ data: Data, to target: URL, parentMode: Int,
                               fileMode: Int) throws -> Bool {
        try ensureDirectory(target.deletingLastPathComponent(), mode: parentMode)
        let descriptor = Darwin.open(target.path, O_WRONLY | O_CREAT | O_EXCL, mode_t(fileMode))
        if descriptor < 0 {
            if errno == EEXIST { return false }
            throw ScheduleWebhookStoreError.unavailable
        }
        var success = false
        defer {
            if !success { _ = Darwin.unlink(target.path) }
            _ = Darwin.close(descriptor)
        }
        try data.withUnsafeBytes { buffer in
            guard let base = buffer.baseAddress else { return }
            var written = 0
            while written < buffer.count {
                let count = Darwin.write(descriptor, base.advanced(by: written),
                                         buffer.count - written)
                guard count > 0 else { throw ScheduleWebhookStoreError.unavailable }
                written += count
            }
        }
        guard Darwin.fsync(descriptor) == 0 else { throw ScheduleWebhookStoreError.unavailable }
        success = true
        return true
    }
}
