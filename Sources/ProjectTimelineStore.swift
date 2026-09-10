import CryptoKit
import Foundation

/// The Mac-owned append-only authority for Project Timeline facts.
final class ProjectTimelineStore {
    static let schemaVersion = 1
    static let maximumEntries = 2_000
    static let maximumEvents = 20_000
    static let maximumReceipts = 4_096
    static let maximumStoreBytes = 8 * 1024 * 1024
    static let shared = ProjectTimelineStore(url: defaultURL())

    struct ReadHeader { let enabled: Bool; let revision: Int }
    struct Reply { let status: Int; let body: [String: Any] }
    struct MutationOutcome {
        let status: String
        let accepted: Int
        let dropped: Int
        let persisted: Bool
        let reason: String?
    }

    private struct RequestReceipt: Codable {
        let actor: String
        let requestID: String
        let digest: String
        let revision: Int
    }

    private struct EventReceipt: Codable {
        let producer: String
        let sourceID: String
        let digest: String
        let eventID: String
    }

    private struct StoredState: Codable {
        var schemaVersion: Int
        var revision: Int
        var enabled: Bool
        var updatedAt: Double
        var entries: [ProjectTimelineEntry]
        var events: [ProjectTimelineEvent]
        var requestReceipts: [RequestReceipt]
        var eventReceipts: [EventReceipt]
        var checkpoints: [ProjectTimelineCheckpoint]
        var coverageIssues: [String]
    }

    private struct IngestCommand: Decodable {
        let operation: String
        let requestId: String
        let expectedRevision: Int
        let producer: String
        let sourceVersion: Int
        let entry: ProjectTimelineEntry
        let events: [ProjectTimelineEvent]
    }

    private let lock = NSLock()
    private let url: URL
    private let now: () -> Date
    private var state: StoredState
    private var unavailable: (code: String, message: String)?

    init(url: URL, now: @escaping () -> Date = Date.init) {
        self.url = url
        self.now = now
        let empty = StoredState(schemaVersion: Self.schemaVersion, revision: 0, enabled: true,
            updatedAt: now().timeIntervalSince1970, entries: [], events: [], requestReceipts: [],
            eventReceipts: [], checkpoints: [], coverageIssues: [])
        guard FileManager.default.fileExists(atPath: url.path) else { state = empty; return }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= Self.maximumStoreBytes else {
                state = empty; unavailable = ("timeline_store_too_large", "Timeline store exceeds its byte capacity."); return
            }
            let decoded = try JSONDecoder().decode(StoredState.self, from: data)
            guard decoded.schemaVersion == Self.schemaVersion,
                  decoded.entries.count <= Self.maximumEntries,
                  decoded.events.count <= Self.maximumEvents,
                  decoded.requestReceipts.count <= Self.maximumReceipts else {
                state = empty; unavailable = ("timeline_store_invalid", "Timeline store violates its schema or capacity."); return
            }
            state = decoded
        } catch {
            state = empty
            unavailable = ("timeline_store_corrupt", "Timeline store is not valid versioned JSON and was left untouched.")
        }
    }

    static func defaultURL() -> URL {
        let root = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline", isDirectory: true)
        return root.appendingPathComponent("project-timeline.json")
    }

    func readHeader() -> ReadHeader {
        lock.lock(); defer { lock.unlock() }
        return ReadHeader(enabled: state.enabled, revision: state.revision)
    }

    var enabled: Bool { readHeader().enabled }

    func materializedEnvelope() -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        if let unavailable {
            return ["timeline": ["schemaVersion": Self.schemaVersion, "available": false,
                "enabled": state.enabled, "revision": state.revision,
                "error": ["code": unavailable.code, "message": unavailable.message]]]
        }
        let eventsByEntry = Dictionary(grouping: state.events, by: \.entryID)
        let entries = state.entries.map { entryObject($0, events: eventsByEntry[$0.id] ?? []) }
            .sorted(by: entryOrder)
        return ["timeline": [
            "schemaVersion": Self.schemaVersion, "available": true,
            "enabled": state.enabled, "revision": state.revision,
            "updatedAt": state.updatedAt, "entries": entries,
            "coverage": ["status": state.coverageIssues.isEmpty ? "complete" : "partial",
                         "reasons": state.coverageIssues.sorted()],
            "checkpoints": state.checkpoints.map(checkpointObject),
        ]]
    }

    func command(_ body: [String: Any], actor: String, trusted: Bool) -> Reply {
        lock.lock(); defer { lock.unlock() }
        if let unavailable { return error(503, unavailable.code, unavailable.message) }
        guard let operation = bounded(body["operation"], 64),
              let requestID = bounded(body["requestId"], 200),
              let expectedRevision = exactInt(body["expectedRevision"]), expectedRevision >= 0,
              let canonical = try? JSONSerialization.data(withJSONObject: body, options: [.sortedKeys]),
              let digest = Self.digest(canonical), let actor = bounded(actor, 300) else {
            return error(400, "invalid_timeline_command", "Timeline commands need operation, requestId and expectedRevision.")
        }
        if let receipt = state.requestReceipts.first(where: { $0.actor == actor && $0.requestID == requestID }) {
            guard receipt.digest == digest else {
                return error(409, "request_id_conflict", "requestId was already used with different Timeline content.")
            }
            return Reply(status: 200, body: ["ok": true, "replayed": true,
                "revision": receipt.revision, "timeline": headerObject()])
        }
        guard expectedRevision == state.revision else {
            return error(409, "revision_conflict", "Timeline revision changed; read it again before writing.",
                         ["currentRevision": state.revision])
        }
        guard state.requestReceipts.count < Self.maximumReceipts else {
            return error(507, "timeline_receipt_capacity", "Timeline request receipt capacity is full.")
        }

        switch operation {
        case "set_enabled":
            guard Set(body.keys) == ["operation", "requestId", "expectedRevision", "enabled"],
                  let value = body["enabled"] as? Bool else {
                return error(400, "invalid_timeline_command", "set_enabled accepts only one Boolean enabled value.")
            }
            var draft = state
            if draft.enabled != value { draft.enabled = value; draft.revision += 1 }
            draft.updatedAt = now().timeIntervalSince1970
            draft.requestReceipts.append(RequestReceipt(actor: actor, requestID: requestID,
                                                        digest: digest, revision: draft.revision))
            return persistAndReply(draft)
        case "ingest":
            guard trusted else {
                return error(403, "timeline_ingest_forbidden", "Only a trusted local producer may ingest Timeline evidence.")
            }
            guard state.enabled else {
                return error(409, "timeline_disabled", "Timeline history is retained, but new ingestion is disabled.")
            }
            guard Set(body.keys) == ["operation", "requestId", "expectedRevision", "producer",
                                      "sourceVersion", "entry", "events"],
                  let command = try? JSONDecoder().decode(IngestCommand.self, from: canonical),
                  command.operation == operation, command.requestId == requestID,
                  command.expectedRevision == expectedRevision else {
                return error(400, "invalid_timeline_receipt", "A closed versioned Timeline receipt is required.")
            }
            return ingest(command, actor: actor, commandDigest: digest)
        default:
            return error(400, "unknown_timeline_operation", "Unknown Timeline operation.")
        }
    }

    @discardableResult
    func ingest(entry: ProjectTimelineEntry, events: [ProjectTimelineEvent],
                producer: String, sourceVersion: Int) -> MutationOutcome {
        lock.lock(); defer { lock.unlock() }
        guard unavailable == nil else {
            return MutationOutcome(status: "unavailable", accepted: 0, dropped: events.count,
                                   persisted: false, reason: unavailable?.code)
        }
        guard state.enabled else {
            return MutationOutcome(status: "refused", accepted: 0, dropped: events.count,
                                   persisted: true, reason: "timeline_disabled")
        }
        return ingestLocked(entry: entry, events: events, producer: producer,
                            sourceVersion: sourceVersion)
    }

    func recordCheckpoint(_ checkpoint: ProjectTimelineCheckpoint) -> MutationOutcome {
        lock.lock(); defer { lock.unlock() }
        guard unavailable == nil, state.enabled else {
            return MutationOutcome(status: "refused", accepted: 0, dropped: 1, persisted: unavailable == nil,
                                   reason: unavailable?.code ?? "timeline_disabled")
        }
        var draft = state
        draft.checkpoints.removeAll { $0.source == checkpoint.source && $0.repositoryID == checkpoint.repositoryID }
        draft.checkpoints.append(checkpoint)
        if let reason = checkpoint.reason { draft.coverageIssues = Array(Set(draft.coverageIssues + [reason])) }
        draft.revision += 1; draft.updatedAt = now().timeIntervalSince1970
        if let failure = persist(draft) {
            return MutationOutcome(status: "unavailable", accepted: 0, dropped: 1, persisted: false, reason: failure.code)
        }
        state = draft
        return MutationOutcome(status: "accepted", accepted: 1, dropped: 0, persisted: true, reason: checkpoint.reason)
    }

    private func ingest(_ command: IngestCommand, actor: String, commandDigest: String) -> Reply {
        let outcome = ingestLocked(entry: command.entry, events: command.events,
                                   producer: command.producer, sourceVersion: command.sourceVersion,
                                   actor: actor, requestID: command.requestId,
                                   requestDigest: commandDigest)
        switch outcome.status {
        case "accepted", "unchanged":
            return Reply(status: 200, body: ["ok": true, "accepted": outcome.accepted,
                "replayed": outcome.status == "unchanged", "timeline": headerObject()])
        case "conflict": return error(409, outcome.reason ?? "timeline_event_conflict", "A source identity already names different immutable evidence.")
        case "capacity": return error(507, outcome.reason ?? "timeline_capacity", "Timeline capacity refused this receipt without dropping history.")
        default: return error(503, outcome.reason ?? "timeline_store_failed", "Timeline receipt was not persisted.")
        }
    }

    private func ingestLocked(entry: ProjectTimelineEntry, events: [ProjectTimelineEvent],
                              producer: String, sourceVersion: Int,
                              actor: String? = nil, requestID: String? = nil,
                              requestDigest: String? = nil) -> MutationOutcome {
        guard valid(entry: entry), !events.isEmpty, sourceVersion > 0,
              events.allSatisfy({ valid(event: $0, entry: entry, producer: producer,
                                        sourceVersion: sourceVersion) }) else {
            return MutationOutcome(status: "refused", accepted: 0, dropped: events.count,
                                   persisted: true, reason: "invalid_timeline_receipt")
        }
        if let existing = state.entries.first(where: { $0.id == entry.id }), existing != entry {
            return MutationOutcome(status: "conflict", accepted: 0, dropped: events.count,
                                   persisted: true, reason: "timeline_entry_conflict")
        }
        var newEvents: [(ProjectTimelineEvent, String)] = []
        var batchReceipts: [String: String] = [:]
        for event in events {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            guard let data = try? encoder.encode(event), let fingerprint = Self.digest(data) else {
                return MutationOutcome(status: "refused", accepted: 0, dropped: events.count,
                                       persisted: true, reason: "invalid_timeline_receipt")
            }
            let identity = event.producer + "\u{1f}" + event.sourceID
            if let prior = state.eventReceipts.first(where: {
                $0.producer == event.producer && $0.sourceID == event.sourceID
            }) {
                guard prior.digest == fingerprint else {
                    return MutationOutcome(status: "conflict", accepted: 0, dropped: events.count,
                                           persisted: true, reason: "timeline_event_conflict")
                }
            } else if let prior = batchReceipts[identity] {
                guard prior == fingerprint else {
                    return MutationOutcome(status: "conflict", accepted: 0, dropped: events.count,
                                           persisted: true, reason: "timeline_event_conflict")
                }
            } else {
                batchReceipts[identity] = fingerprint
                newEvents.append((event, fingerprint))
            }
        }
        let addsEntry = !state.entries.contains { $0.id == entry.id }
        guard state.entries.count + (addsEntry ? 1 : 0) <= Self.maximumEntries,
              state.events.count + newEvents.count <= Self.maximumEvents else {
            return MutationOutcome(status: "capacity", accepted: 0, dropped: events.count,
                                   persisted: true, reason: "timeline_capacity_reached")
        }
        var draft = state
        if addsEntry { draft.entries.append(entry) }
        for (event, fingerprint) in newEvents {
            draft.events.append(event)
            draft.eventReceipts.append(EventReceipt(producer: event.producer,
                sourceID: event.sourceID, digest: fingerprint, eventID: event.id))
        }
        let changed = addsEntry || !newEvents.isEmpty
        if changed { draft.revision += 1; draft.updatedAt = now().timeIntervalSince1970 }
        if let actor, let requestID, let requestDigest {
            draft.requestReceipts.append(RequestReceipt(actor: actor, requestID: requestID,
                                                        digest: requestDigest, revision: draft.revision))
        }
        if let failure = persist(draft) {
            return MutationOutcome(status: "unavailable", accepted: 0, dropped: events.count,
                                   persisted: false, reason: failure.code)
        }
        state = draft
        return MutationOutcome(status: changed ? "accepted" : "unchanged",
                               accepted: newEvents.count, dropped: 0, persisted: true, reason: nil)
    }

    private func valid(entry: ProjectTimelineEntry) -> Bool {
        bounded(entry.id, 200) != nil && bounded(entry.projectID, 200) != nil
            && bounded(entry.deliveryKey, 300) != nil && bounded(entry.originalTitle, 300) != nil
            && entry.summary.utf8.count <= 1_000 && entry.createdAt.isFinite
            && entry.tags.count <= 16 && entry.requiredTargets.count <= 64
            && entry.boardItemIDs.count <= 32 && entry.sourceRevisions.count <= 64
            && entry.tags.allSatisfy { bounded($0, 100) != nil }
            && entry.requiredTargets.allSatisfy(valid(target:))
            && entry.boardItemIDs.allSatisfy { bounded($0, 200) != nil }
            && entry.relations.count <= 64 && entry.relations.allSatisfy {
                bounded($0.kind, 64) != nil && bounded($0.targetEntryID, 200) != nil
            }
            && entry.sourceRevisions.allSatisfy {
                bounded($0.repositoryID, 300) != nil && ProjectTimelineRemote.validSHA($0.commit)
                    && bounded($0.role, 100) != nil
            }
            && Set(["deploy", "server", "architecture", "feature", "operation"]).contains(entry.primaryCategory)
    }

    private func valid(event: ProjectTimelineEvent, entry: ProjectTimelineEntry,
                       producer: String, sourceVersion: Int) -> Bool {
        let base = event.entryID == entry.id && event.producer == producer && event.sourceVersion == sourceVersion
            && bounded(event.id, 200) != nil && bounded(event.sourceID, 300) != nil
            && bounded(event.producer, 200) != nil && bounded(event.authority, 200) != nil
            && event.observedAt.isFinite && event.effectiveAt.map(\.isFinite) != false
            && Set(["landed_to_git", "git_history_observed", "deploy_succeeded", "deploy_failed", "availability_verified",
                    "rollback", "operation_applied", "operation_verified", "planned", "revert"]).contains(event.kind)
            && Set(["passed", "failed", "unknown"]).contains(event.result)
            && event.target.map(valid(target:)) != false
            && event.revision.map(ProjectTimelineRemote.validSHA) != false
            && event.previousRevision.map(ProjectTimelineRemote.validSHA) != false
            && event.beforeDigest.map { $0.utf8.count <= 300 } != false
            && event.afterDigest.map { $0.utf8.count <= 300 } != false
            && event.verification.map { $0.utf8.count <= 500 } != false
            && event.sourceURL.map { $0.utf8.count <= 2_000 } != false
        guard base else { return false }
        switch event.kind {
        case "landed_to_git", "git_history_observed", "revert":
            return event.revision != nil
        case "deploy_succeeded", "availability_verified":
            return event.target != nil && event.revision != nil
        case "deploy_failed":
            return event.target != nil
        case "rollback":
            return event.target != nil && event.previousRevision != nil
        case "operation_applied":
            return event.target != nil && event.beforeDigest != nil && event.afterDigest != nil
        case "operation_verified":
            return event.target != nil && event.beforeDigest != nil && event.afterDigest != nil
                && event.verification != nil
        default:
            return true
        }
    }

    private func valid(target: ProjectTimelineTarget) -> Bool {
        Set(["production", "staging", "preview", "development"]).contains(target.environment)
            && bounded(target.component, 100) != nil && bounded(target.audience, 100) != nil
    }

    private func persistAndReply(_ draft: StoredState) -> Reply {
        if let failure = persist(draft) { return error(503, failure.code, failure.message) }
        state = draft
        return Reply(status: 200, body: ["ok": true, "revision": draft.revision,
                                         "timeline": headerObject()])
    }

    private func persist(_ draft: StoredState) -> (code: String, message: String)? {
        do {
            let data = try JSONEncoder().encode(draft)
            guard data.count <= Self.maximumStoreBytes else {
                return ("timeline_store_capacity", "Timeline store exceeds its durable byte capacity.")
            }
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            try data.write(to: url, options: .atomic)
            return nil
        } catch {
            return ("timeline_store_write_failed", "Timeline mutation was not persisted.")
        }
    }

    private func headerObject() -> [String: Any] {
        ["schemaVersion": Self.schemaVersion, "revision": state.revision, "enabled": state.enabled]
    }

    private func entryObject(_ entry: ProjectTimelineEntry,
                             events: [ProjectTimelineEvent]) -> [String: Any] {
        let projection = ProjectTimelineReconciler.project(entry, events: events)
        let environments = Set(["production"] + entry.requiredTargets.map(\.environment)
            + events.compactMap { $0.target?.environment })
        let projections = Dictionary(uniqueKeysWithValues: environments.map {
            ($0, ProjectTimelineReconciler.project(entry, events: events, environment: $0).jsonObject)
        })
        return ["id": entry.id, "projectId": entry.projectID, "deliveryKey": entry.deliveryKey,
            "primaryCategory": entry.primaryCategory, "tags": entry.tags,
            "originalTitle": entry.originalTitle, "summary": entry.summary, "createdAt": entry.createdAt,
            "requiredTargets": entry.requiredTargets.map(\.jsonObject),
            "boardItemIds": entry.boardItemIDs, "relations": entry.relations.map(\.jsonObject),
            "sourceRevisions": entry.sourceRevisions.map(\.jsonObject),
            "projection": projection.jsonObject, "projections": projections,
            "events": events.sorted(by: ProjectTimelineReconciler.stableOrder).map(eventObject)]
    }

    private func eventObject(_ event: ProjectTimelineEvent) -> [String: Any] {
        ["id": event.id, "kind": event.kind,
         "target": event.target?.jsonObject as Any? ?? NSNull(),
         "effectiveAt": event.effectiveAt as Any? ?? NSNull(), "observedAt": event.observedAt,
         "authority": event.authority, "sourceId": event.sourceID,
         "sourceVersion": event.sourceVersion,
         "revision": event.revision as Any? ?? NSNull(),
         "previousRevision": event.previousRevision as Any? ?? NSNull(),
         "result": event.result, "cohortPercent": event.cohortPercent as Any? ?? NSNull(),
         "beforeDigest": event.beforeDigest as Any? ?? NSNull(),
         "afterDigest": event.afterDigest as Any? ?? NSNull(),
         "verification": event.verification as Any? ?? NSNull(),
         "sourceUrl": event.sourceURL as Any? ?? NSNull()]
    }

    private func checkpointObject(_ value: ProjectTimelineCheckpoint) -> [String: Any] {
        ["source": value.source, "repositoryId": value.repositoryID,
         "throughRevision": value.throughRevision as Any? ?? NSNull(),
         "observedAt": value.observedAt, "imported": value.imported, "omitted": value.omitted,
         "reason": value.reason as Any? ?? NSNull()]
    }

    private func entryOrder(_ lhs: [String: Any], _ rhs: [String: Any]) -> Bool {
        func time(_ row: [String: Any]) -> Double {
            let projection = row["projection"] as? [String: Any]
            return projection?["effectiveAt"] as? Double ?? projection?["observedAt"] as? Double ?? 0
        }
        let left = time(lhs), right = time(rhs)
        return left == right ? (lhs["id"] as? String ?? "") < (rhs["id"] as? String ?? "") : left > right
    }

    private func exactInt(_ value: Any?) -> Int? {
        guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let double = number.doubleValue, integer = number.intValue
        return double.isFinite && Double(integer) == double ? integer : nil
    }

    private func bounded(_ value: Any?, _ maximum: Int) -> String? {
        guard let value = value as? String, !value.isEmpty, value.utf8.count <= maximum else { return nil }
        return value
    }

    private func bounded(_ value: String, _ maximum: Int) -> String? {
        !value.isEmpty && value.utf8.count <= maximum ? value : nil
    }

    private static func digest(_ data: Data) -> String? {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private func error(_ status: Int, _ code: String, _ message: String,
                       _ extra: [String: Any] = [:]) -> Reply {
        var value: [String: Any] = ["code": code, "message": message]
        extra.forEach { value[$0.key] = $0.value }
        return Reply(status: status, body: ["error": value])
    }
}
