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
    private static let maximumReportVersions = 16
    private static let maximumReportSources = 32
    private static let maximumFirstScreenRemaining = 16

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
    private static let terminalAttemptStates = Set([
        "success", "failure", "timeout", "cancelled", "spawn_failed",
    ])
    private static let activeAttemptStates = Set(["queued", "spawning", "briefed"])
    private static let artifactKinds = Set([
        "document", "website", "deployment", "commit", "other",
    ])
    private static let documentPurposes = Set(["plan", "decision", "reference"])
    private static let obligationActorKinds = Set(["user", "agent", "external"])
    private static let linkKinds = Set([
        "session", "task", "worktree", "related", "blocks", "coordinates",
    ])
    private static let itemLinkKinds = Set(["related", "blocks", "coordinates"])
    private static let reportSourceKinds = Set([
        "task", "artifact", "evidence", "handoff", "item", "external",
    ])

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
        var supplementRelationId: String? = nil
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

    /// A document is narrative context, not an output artifact or acceptance record.  The
    /// logical document identity remains stable while immutable rows describe its versions.
    private struct StoredDocumentReference: Codable {
        var id: String
        var documentId: String
        var version: Int
        var title: String
        var url: String
        var purpose: String
        var supersedesId: String?
        var addedAt: Double
        var actor: String
    }

    private struct StoredLink: Codable {
        var id: String
        var kind: String
        var targetId: String
        var label: String
        var source: String?
        var phase: String?
        var head: String?
        /// A deliberately small copy of broker-owned attempt facts used only for the board's
        /// progress projection. The task registry remains authoritative and replay refreshes it.
        var attemptState: String? = nil
        var startedAt: Double? = nil
        var finishedAt: Double? = nil
        var statusObservedAt: Double? = nil
        var sourceTaskId: String? = nil
        var eventAt: Double? = nil
        var eventOrdinal: Int? = nil
        var graphId: String? = nil
        var graphNodeId: String? = nil
        var landingDisposition: String? = nil
        var landingDispositionAt: Double? = nil
    }

    private struct StoredObligation: Codable {
        var id: String
        var title: String
        var owner: String
        var blocking: Bool
        var resolved: Bool
        /// Older rows deliberately remain `unknown` in the read projection rather than being
        /// guessed from a display name such as "user" or "root".
        var actorKind: String? = nil
        var requiredAction: String? = nil
        var blockingScope: String? = nil
        var resolutionEvidence: String? = nil
        var supersededBy: String? = nil
        var supplementRelationId: String? = nil
    }

    private struct StoredHistory: Codable {
        var id: String
        var at: Double
        var actor: String
        var kind: String
        var summary: String
        var sourceTaskId: String? = nil
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
        var eventAt: Double? = nil
        var eventOrdinal: Int? = nil
    }

    private struct StoredIngestionCoverage: Codable {
        var kind: String
        var reason: String
        var sourceDigests: [String]
        var saturated: Bool
        var firstObservedAt: Double
    }

    private struct StoredReportSource: Codable {
        var kind: String
        var targetId: String
        var label: String
        var url: String?
        /// Resolution and authority are issued by the store, never accepted as caller claims.
        var resolution: String
        var authority: String
        /// Historical relation epoch. Older schema-v1 rows fall back to report authored time.
        var resolvedAt: Double? = nil
    }

    private struct StoredReportBoundary: Codable {
        var kind: String
        var label: String
        var startedAt: Double?
        var endedAt: Double?
        var handoffId: String?
    }

    private struct StoredCompletionReport: Codable {
        var id: String
        var version: Int
        var objective: String
        var deliveredOutcomes: String
        var verificationLanding: String
        var remainingWork: String
        var lessons: String
        var sourceReferences: [StoredReportSource]
        var authoredAt: Double
        var actor: String
        var authorship: String
        var model: String?
        var scopeRevision: Int
        var itemStateAtAuthorship: String
        /// Required for Coordination; absent on older and ordinary delivery reports.
        var reportBoundary: StoredReportBoundary? = nil
    }

    private struct StoredPresentation: Codable {
        var locale: String
        var sourceFingerprint: String
        var title: String
        var summary: String
        var outcome: String
        var nextStep: String
        var model: String
        var authoredAt: Double
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
        var ownerSourceTaskId: String? = nil
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
        var scopeEventAt: Double? = nil
        var scopeEventOrdinal: Int? = nil
        var inferredSourceKey: String? = nil
        var ingestionCoverage: [StoredIngestionCoverage]? = nil
        var typeDetails: [String: String]? = nil
        /// Optional keeps schema-v1 stores loadable without a migration rewrite.
        var completionReports: [StoredCompletionReport]? = nil
        /// Reading aids never participate in evidence, scope or lifecycle reconciliation.
        var presentations: [StoredPresentation]? = nil
        /// Optional so schema-v1 stores written before typed document references decode intact.
        var documentReferences: [StoredDocumentReference]? = nil
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
        /// Explicit node ownership is separate from the graph's compatibility fallback item.
        var graphNodeItems: [String: String]? = nil
        /// Project-scoped fallback identities for retained tasks that predate decision graphs.
        var taskItems: [String: String]? = nil
        /// Monotonic tie-breaker for source events whose timestamps are equal or absent.
        var eventSequence: Int? = nil
        var presentationEpoch: Int? = nil
        /// Explicit provider-scoped permission for board-reading-v1. Absent on migration.
        var narrativeConsent: String? = nil

        static func empty(now: Double) -> StoredState {
            StoredState(schemaVersion: ProjectBoardStore.schemaVersion, revision: 0,
                        enabled: true, updatedAt: now, projects: [], items: [], receipts: [],
                        graphItems: [:], receiptEvictions: 0)
        }
    }

    struct ReadHeader {
        let revision: Int
        let enabled: Bool
        let updatedAt: Double
        let available: Bool
        var narrativeConsent: String? = nil
    }

    /// A process-local, typed projection of the durable store.  It is rebuilt on startup and by
    /// the Board reconciliation worker after writes; HTTP reads only copy one already-materialized
    /// scope.  The durable StoredState remains authoritative for every lifecycle decision.
    struct MaterializedReadSeed {
        let header: ReadHeader
        let catalog: [[String: Any]]
        let itemSummariesByProject: [String: [[String: Any]]]
        let itemDetailsByID: [String: [String: Any]]
        let automaticMutation: [String: Any]
        let sourceIngestion: [String: Any]
        let error: [String: Any]?
        var sessionItemIDs: [String: [String]] = [:]
        var sessionActiveItemIDs: [String: Set<String>] = [:]

        func envelope(project: String? = nil, item: String? = nil) -> [String: Any] {
            var board: [String: Any] = [
                "schemaVersion": ProjectBoardStore.schemaVersion,
                "revision": header.revision,
                "enabled": header.enabled,
                "narrativeConsent": header.narrativeConsent as Any? ?? NSNull(),
                "mode": header.enabled ? "board" : "standard",
                "entitlement": ProjectBoardStore.entitlement,
                "projects": catalog,
                "items": [],
                "item": NSNull(),
                "truncated": false,
                "updatedAt": header.updatedAt,
                "available": header.available,
                "snapshotBudgetBytes": ProjectBoardStore.maximumSnapshotBytes,
                "automaticMutation": automaticMutation,
                "sourceIngestion": sourceIngestion,
            ]
            if let error { board["error"] = error }
            if project == nil, let item, item.hasPrefix("session:") {
                let session = ProjectBoardStore.canonicalConversationID(
                    String(item.dropFirst("session:".count)))
                let ids = sessionItemIDs[session] ?? []
                board["sessionId"] = session
                board["items"] = ids.prefix(100).compactMap { id -> [String: Any]? in
                    guard let detail = itemDetailsByID[id] else { return nil }
                    var row: [String: Any] = [:]
                    for key in ["id", "key", "projectId", "title", "type", "state", "updatedAt", "progress"] {
                        row[key] = detail[key]
                    }
                    row["sessionActivity"] = sessionActiveItemIDs[session]?.contains(id) == true
                        ? "declared" : "related"
                    return row
                }
                board["truncated"] = ids.count > 100
                board["relationCoverage"] = ["total": ids.count, "omitted": max(0, ids.count - 100)]
            } else if let item {
                if let detail = itemDetailsByID[item],
                   project == nil || detail["projectId"] as? String == project {
                    board["item"] = detail
                }
            } else if let project {
                board["items"] = itemSummariesByProject[project] ?? []
            }
            return ["board": board]
        }
    }

    /// Known source timestamps form one ordered domain; unknown timestamps sort before it and use
    /// a durable observation ordinal amongst themselves. Equal known timestamps use that same
    /// ordinal. This lexicographic policy is total and replay-stable without treating an ingestion
    /// clock as source chronology. A newly observed unknown attempt is surfaced separately as an
    /// unresolved post-landing event, so it cannot silently erase or hide the established landing.
    private struct StoredEventOrder: Comparable {
        let at: Double?
        let ordinal: Int
        let stableID: String

        static func < (lhs: StoredEventOrder, rhs: StoredEventOrder) -> Bool {
            switch (lhs.at, rhs.at) {
            case let (left?, right?) where left != right:
                return left < right
            case (nil, .some):
                return true
            case (.some, nil):
                return false
            default:
                break
            }
            if lhs.ordinal != rhs.ordinal { return lhs.ordinal < rhs.ordinal }
            return lhs.stableID < rhs.stableID
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
    private let publicationLock = NSLock()
    private var state: StoredState
    private var unavailable: BoardError?
    private var lastAutomaticFailure: AutomaticMutationOutcome?
    private var materializedSeed: MaterializedReadSeed?
    private var materializedSeedStale = false
    private var publishedHeader: ReadHeader
    private var publishedSeed: MaterializedReadSeed?

    static var materializationPauseForTesting: (() -> Void)?
    static var seedPublicationPauseForTesting: (() -> Void)?
    static var persistencePauseForTesting: (() -> Void)?
    static var materializationIndexOperationForTesting: (() -> Void)?

    init(url: URL, now: @escaping () -> Date = Date.init) {
        self.url = url
        self.now = now
        self.state = .empty(now: now().timeIntervalSince1970)
        self.publishedHeader = ReadHeader(revision: 0, enabled: true,
                                          updatedAt: 0, available: true)
        load()
        self.publishedHeader = currentHeaderLocked()
    }

    var enabled: Bool {
        lock.lock(); defer { lock.unlock() }
        // A corrupt or future-version store cannot safely authorize new workflow gates.
        return unavailable == nil && state.enabled
    }

    func readHeader() -> ReadHeader {
        publicationLock.lock(); defer { publicationLock.unlock() }
        return publishedHeader
    }

    func presentationCandidates(locale: String, limit: Int, excluding: Set<String> = []) -> [[String: Any]] {
        lock.lock(); defer { lock.unlock() }
        guard unavailable == nil, state.enabled, Self.validPresentationLocale(locale), limit > 0 else { return [] }
        let ranks = Dictionary(uniqueKeysWithValues: state.items.map { item in
            let group = progressObject(item)["group"] as? String
            return (item.id, group == "active" ? 0 : (group == "waiting" ? 1 : 2))
        })
        let ranked = state.items.sorted { left, right in
            let a = ranks[left.id]!, b = ranks[right.id]!
            return a == b ? (left.updatedAt == right.updatedAt ? left.id < right.id : left.updatedAt > right.updatedAt) : a < b
        }
        var result: [[String: Any]] = []
        for item in ranked {
            guard !excluding.contains(item.id) else { continue }
            let fingerprint = presentationFingerprint(item)
            guard !(item.presentations ?? []).contains(where: {
                $0.locale == locale && $0.sourceFingerprint == fingerprint
            }) else { continue }
            result.append(["id": item.id, "locale": locale, "sourceFingerprint": presentationWorkToken(item),
                           "title": Self.presentationText(item.title, maximumBytes: 1_000),
                           "summary": Self.presentationText(item.summary, maximumBytes: 4_800), "type": item.type,
                           "outcome": Self.presentationText(item.completionReports?.last?.deliveredOutcomes ?? "", maximumBytes: 2_000)])
            if result.count >= min(limit, 8) { break }
        }
        return result
    }

    func permitsNarrative(assistant: Assistant) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return unavailable == nil && state.enabled && state.narrativeConsent == assistant.rawValue
    }

    @discardableResult
    func recordPresentation(itemID: String, locale: String, sourceFingerprint: String,
                            title: String, summary: String, outcome: String,
                            nextStep: String, model: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard unavailable == nil, state.enabled, Self.validPresentationLocale(locale),
              let index = state.items.firstIndex(where: { $0.id == itemID }),
              presentationWorkToken(state.items[index]) == sourceFingerprint,
              let title = Self.boundedText(title, maximum: 320), title.count <= 80,
              let summary = Self.boundedText(summary, maximum: 1200), summary.count <= 300,
              outcome.count <= 300, outcome.utf8.count <= 1200,
              nextStep.count <= 200, nextStep.utf8.count <= 800,
              let model = Self.boundedText(model, maximum: 200) else { return false }
        let contentFingerprint = presentationFingerprint(state.items[index])
        if (state.items[index].presentations ?? []).contains(where: {
            $0.locale == locale && $0.sourceFingerprint == contentFingerprint
        }) { return true }
        var draft = state
        let timestamp = now().timeIntervalSince1970
        var variants = (draft.items[index].presentations ?? []).filter { $0.locale != locale }
        variants.append(StoredPresentation(locale: locale, sourceFingerprint: contentFingerprint,
            title: title, summary: summary, outcome: outcome, nextStep: nextStep,
            model: model, authoredAt: timestamp))
        draft.items[index].presentations = Array(variants.suffix(3))
        // Do not touch item.updatedAt: rewriting a reading aid is not new work activity.
        draft.revision += 1
        draft.updatedAt = timestamp
        guard persist(draft) == nil else { return false }
        state = draft
        return true
    }

    private static func presentationText(_ text: String, maximumBytes: Int) -> String {
        var result = "", bytes = 0
        for scalar in text.unicodeScalars {
            let count = String(scalar).utf8.count
            guard bytes + count <= maximumBytes else { break }
            result.unicodeScalars.append(scalar)
            bytes += count
        }
        return result
    }

    private func presentationObject(_ item: StoredItem) -> [String: Any] {
        let fingerprint = presentationFingerprint(item)
        return ["authority": "narrative_only", "variants": (item.presentations ?? []).map {
            ["locale": $0.locale, "title": $0.title, "summary": $0.summary,
             "outcome": $0.outcome, "nextStep": $0.nextStep, "model": $0.model,
             "authoredAt": $0.authoredAt,
             "status": $0.sourceFingerprint == fingerprint ? "current" : "stale"] as [String: Any]
        }]
    }

    private func presentationFingerprint(_ item: StoredItem) -> String {
        Self.digest(["title": item.title, "summary": item.summary, "type": item.type,
                     "scopeRevision": item.scopeRevision ?? 0,
                     "report": item.completionReports?.last?.id ?? ""])!
    }

    private func presentationWorkToken(_ item: StoredItem) -> String {
        Self.digest(["source": presentationFingerprint(item),
                     "trackingEpoch": state.presentationEpoch ?? 0])!
    }

    private static func validPresentationLocale(_ locale: String) -> Bool {
        !locale.isEmpty && locale.count <= 35 && locale != "auto"
            && locale.unicodeScalars.allSatisfy {
                CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-").contains($0)
            }
    }

    /// Mirrors the hosted Documents locator contract without importing browser code into the
    /// Foundation-only store.  The URL contains an opaque fleet identity and relative inert-text
    /// path after the fragment boundary; it never contains a credential or local filesystem path.
    private static func isCanonicalCloudDocumentURL(_ value: String) -> Bool {
        guard let components = URLComponents(string: value),
              components.scheme?.lowercased() == "https",
              components.host?.lowercased() == "app.clawdline.com",
              components.port == nil, components.user == nil, components.password == nil,
              components.path == "/", components.query == nil,
              let fragment = components.percentEncodedFragment,
              let fields = formFields(fragment) else { return false }
        guard fields["document"] == "1",
              let machine = fields["machine"], validDocumentIdentity(machine),
              machine != "this-mac",
              let session = fields["session"], validDocumentIdentity(session),
              let scope = fields["scope"], ["project", "task"].contains(scope),
              let path = fields["path"], validDocumentPath(path) else { return false }
        let expected = scope == "task"
            ? Set(["document", "machine", "session", "scope", "task", "path"])
            : Set(["document", "machine", "session", "scope", "path"])
        guard Set(fields.keys) == expected else { return false }
        return scope != "task" || validDocumentTaskID(fields["task"] ?? "")
    }

    /// WHATWG form decoding for a fragment payload: split before decoding, `+` means space,
    /// valid percent triplets become bytes, and malformed percent signs remain literal. Each
    /// component is decoded exactly once, matching URLSearchParams in the hosted reader.
    private static func formFields(_ source: String) -> [String: String]? {
        var fields: [String: String] = [:]
        for pair in source.split(separator: "&", omittingEmptySubsequences: false) {
            // URLSearchParams ignores empty pairs, including leading and trailing separators.
            guard !pair.isEmpty else { continue }
            let parts = pair.split(separator: "=", maxSplits: 1,
                                   omittingEmptySubsequences: false)
            let name = formDecode(String(parts[0]))
            let value = formDecode(parts.count == 2 ? String(parts[1]) : "")
            guard fields[name] == nil else { return nil }
            fields[name] = value
        }
        return fields
    }

    private static func formDecode(_ source: String) -> String {
        let bytes = Array(source.utf8)
        var result: [UInt8] = []
        result.reserveCapacity(bytes.count)
        var index = 0
        func hex(_ byte: UInt8) -> UInt8? {
            switch byte {
            case 48...57: return byte - 48
            case 65...70: return byte - 55
            case 97...102: return byte - 87
            default: return nil
            }
        }
        while index < bytes.count {
            if bytes[index] == 43 {
                result.append(32); index += 1
            } else if bytes[index] == 37, index + 2 < bytes.count,
                      let high = hex(bytes[index + 1]), let low = hex(bytes[index + 2]) {
                result.append(high * 16 + low); index += 3
            } else {
                result.append(bytes[index]); index += 1
            }
        }
        return String(decoding: result, as: UTF8.self)
    }

    private static func canonicalConversationID(_ value: String) -> String {
        UUID(uuidString: value)?.uuidString.lowercased() ?? value
    }

    private static func validDocumentIdentity(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.count <= 128 && !value.unicodeScalars.contains {
            CharacterSet.controlCharacters.contains($0)
        }
    }

    private static func validDocumentPath(_ value: String) -> Bool {
        guard !value.isEmpty, value.utf8.count <= 512, !value.hasPrefix("/") else { return false }
        let parts = value.split(separator: "/", omittingEmptySubsequences: false)
        guard (1...6).contains(parts.count), parts.allSatisfy({
            !$0.isEmpty && !$0.hasPrefix(".") && !$0.unicodeScalars.contains {
                CharacterSet.controlCharacters.contains($0)
            }
        }), let name = parts.last, let dot = name.lastIndex(of: ".") else { return false }
        return ["md", "markdown", "txt"].contains(name[name.index(after: dot)...].lowercased())
    }

    private static func validDocumentTaskID(_ value: String) -> Bool {
        let chars = Array(value.lowercased())
        guard chars.count == 36, [8, 13, 18, 23].allSatisfy({ chars[$0] == "-" }),
              ["1", "2", "3", "4", "5"].contains(chars[14]),
              ["8", "9", "a", "b"].contains(chars[19]) else { return false }
        return chars.enumerated().allSatisfy { index, character in
            [8, 13, 18, 23].contains(index) || character.isHexDigit
        }
    }

    /// `rebuild` is used only by the background reconciliation worker.  A request with no current
    /// model receives the startup seed immediately and lets that worker catch it up.
    func readSeed(rebuild: Bool) -> (seed: MaterializedReadSeed, stale: Bool) {
        lock.lock()
        if materializedSeed == nil || (rebuild && materializedSeedStale) {
            materializedSeed = buildMaterializedReadSeedLocked()
            materializedSeedStale = false
        }
        let answer = (materializedSeed!, materializedSeedStale)
        // Keep writer ownership through publication. A command cannot durably publish N+1 in the
        // former unlock window and then be overwritten by this older N seed.
        Self.seedPublicationPauseForTesting?()
        publicationLock.lock()
        if answer.0.header.revision >= publishedHeader.revision {
            publishedSeed = answer.0
            publishedHeader = answer.0.header
        }
        publicationLock.unlock()
        lock.unlock()
        return answer
    }

    func readSeedIfAvailable() -> (seed: MaterializedReadSeed, stale: Bool)? {
        publicationLock.lock(); defer { publicationLock.unlock() }
        return publishedSeed.map { ($0, $0.header.revision != publishedHeader.revision) }
    }

    private func currentHeaderLocked(_ value: StoredState? = nil) -> ReadHeader {
        let value = value ?? state
        return ReadHeader(revision: value.revision, enabled: unavailable == nil && value.enabled,
                          updatedAt: value.updatedAt, available: unavailable == nil,
                          narrativeConsent: value.narrativeConsent)
    }

    func snapshot(project: String? = nil, item: String? = nil) -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        return snapshotLocked(project: project, item: item)
    }

    /// Read one continuation page for a selected detail collection. The durable per-field caps
    /// remain authoritative; this route never expands the all-Project projection.
    func collectionSnapshot(project: String?, item itemID: String,
                            kind: String, offset: Int) -> Reply {
        lock.lock(); defer { lock.unlock() }
        if let unavailable { return Self.errorReply(unavailable) }
        guard ["document_references", "remaining_work", "user_decisions"].contains(kind),
              offset >= 0, offset <= Self.maximumItems,
              let itemID = Self.boundedText(itemID, maximum: 200) else {
            return Self.errorReply(BoardError(
                status: 400, code: "invalid_collection_selector",
                message: "a collection read needs a known kind and bounded offset"))
        }
        guard let selected = state.items.first(where: { $0.id == itemID }) else {
            return Self.errorReply(BoardError(
                status: 404, code: "collection_item_not_found",
                message: "the selected Board item does not exist"))
        }
        if let project, selected.projectId != project {
            return Self.errorReply(BoardError(
                status: 409, code: "collection_project_mismatch",
                message: "the selected item does not belong to the selected Project"))
        }
        let rows: [[String: Any]]
        if kind == "document_references" {
            let references = selected.documentReferences ?? []
            let superseded = Set(references.compactMap(\.supersedesId))
            rows = orderedDocumentReferences(references).map {
                documentReferenceObject($0, all: references, supersededIDs: superseded)
            }
        } else {
            let remaining = remainingWorkRows(selected)
            rows = kind == "user_decisions" ? remaining.decisions : remaining.work
        }
        guard offset <= rows.count else {
            return Self.errorReply(BoardError(
                status: 416, code: "collection_offset_out_of_range",
                message: "the collection offset is beyond the retained rows"))
        }
        let end = min(rows.count, offset + 64)
        let page = Array(rows[offset..<end])
        let board: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "revision": state.revision,
            "enabled": state.enabled, "mode": state.enabled ? "board" : "standard",
            "narrativeConsent": state.narrativeConsent as Any? ?? NSNull(),
            "entitlement": Self.entitlement, "projects": [] as [[String: Any]],
            "items": [] as [[String: Any]], "item": NSNull(), "truncated": end < rows.count,
            "updatedAt": state.updatedAt, "available": true,
            "snapshotBudgetBytes": Self.maximumSnapshotBytes,
            "collection": [
                "kind": kind, "itemId": selected.id, "offset": offset,
                "rows": page, "totalCount": rows.count,
                "nextOffset": end < rows.count ? end as Any : NSNull(),
            ] as [String: Any],
        ]
        return Reply(status: 200, body: ["board": board])
    }

    /// Retrieve exactly one durable report body. Catalog and ordinary item detail keep older
    /// versions metadata-only; this path is admitted only through the bounded read lane.
    func reportSnapshot(project: String? = nil, item itemID: String,
                        report reportID: String) -> Reply {
        lock.lock(); defer { lock.unlock() }
        if let unavailable { return Self.errorReply(unavailable) }
        guard let itemID = Self.boundedText(itemID, maximum: 200),
              let reportID = Self.boundedText(reportID, maximum: 200),
              project.map({ Self.boundedText($0, maximum: 200) != nil }) ?? true else {
            return Self.errorReply(BoardError(
                status: 400, code: "invalid_report_selector",
                message: "item and report must be bounded opaque selectors"))
        }
        guard let selectedIndex = state.items.firstIndex(where: { $0.id == itemID }) else {
            return Self.errorReply(BoardError(
                status: 404, code: "report_item_not_found",
                message: "the selected Board item does not exist"))
        }
        if let project, state.items[selectedIndex].projectId != project {
            return Self.errorReply(BoardError(
                status: 409, code: "report_project_mismatch",
                message: "the selected item does not belong to the selected Project"))
        }
        guard let ownerIndex = state.items.firstIndex(where: { item in
            (item.completionReports ?? []).contains { $0.id == reportID }
        }) else {
            return Self.errorReply(BoardError(
                status: 404, code: "report_not_found",
                message: "the selected report version does not exist"))
        }
        guard ownerIndex == selectedIndex else {
            return Self.errorReply(BoardError(
                status: 409, code: "report_item_mismatch",
                message: "the selected report does not belong to the selected item"))
        }
        let item = state.items[selectedIndex]
        let reports = item.completionReports ?? []
        guard let offset = reports.firstIndex(where: { $0.id == reportID }) else {
            return Self.errorReply(BoardError(
                status: 404, code: "report_not_found",
                message: "the selected report version does not exist"))
        }
        var envelope = snapshotLocked(project: project, item: itemID)
        guard var board = envelope["board"] as? [String: Any] else {
            return Self.errorReply(BoardError(
                status: 503, code: "board_unavailable", message: "Board unavailable"))
        }
        board["items"] = [] as [[String: Any]]
        board["reportSelection"] = reportObject(
            reports[offset], status: reportStatus(reports[offset], item: item,
                                                  latest: offset == reports.count - 1),
            includeBody: true)
        envelope["board"] = board
        guard Self.serializedSize(envelope) <= Self.maximumSnapshotBytes else {
            return Self.errorReply(BoardError(
                status: 503, code: "board_report_response_too_large",
                message: "the selected report exceeds the Board read budget"))
        }
        return Reply(status: 200, body: envelope)
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
        guard state.enabled else {
            return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                            droppedCount: 1, persisted: true,
                                            reason: "board_disabled")
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

    func command(_ body: [String: Any], actor: String, trusted: Bool = false,
                 workflowOrigin: Bool = false) -> Reply {
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

        // This in-process origin is not an HTTP field and does not grant trusted evidence
        // authority. Reject forged display relations even when an old receipt could replay.
        if body["supplementRelationId"] != nil {
            guard workflowOrigin else {
                return Self.errorReply(BoardError(status: 403, code: "workflow_origin_required",
                    message: "Supplement relations are assigned only by the workflow producer"))
            }
            guard ["checklist", "obligation"].contains(operation),
                  let relation = body["supplementRelationId"] as? String,
                  Self.validSupplementRelationID(relation),
                  body["checklistId"] == nil, body["status"] == nil else {
                return Self.errorReply(BoardError(status: 400, code: "invalid_supplement_relation",
                    message: "A supplement relation must identify one newly materialized row"))
            }
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
        if !["set_enabled", "set_ai_consent"].contains(operation) && !state.enabled {
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
            if let itemID = applied.itemId,
               operation != "transition", operation != "create", operation != "record_report",
               operation != "end_span" {
                reconcileLifecycle(around: itemID, actor: "board", timestamp: timestamp,
                                   draft: &draft)
            }
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
        var eventSequence = draft.eventSequence ?? maximumEventOrdinal(in: draft)
        let initialEventSequence = eventSequence

        var itemIndex: Int?
        let taskID = Self.boundedText(task["id"], maximum: 200)
        let taskKey = taskID.map { Self.taskKey(projectID: projectID, taskID: $0) }
        let explicit = Self.boundedText(task["workItemId"] ?? task["work_item_id"], maximum: 200)
        let graph = task["graph"] as? [String: Any]
        let graphID = Self.boundedText(graph?["id"], maximum: 200)
        let graphKey = graphID.map { Self.graphKey(projectID: projectID, graphID: $0) }
        let graphNode = Self.boundedText(graph?["current_node"], maximum: 200)
        let nodeKey = graphKey.flatMap { key in
            graphNode.flatMap { Self.stringDigest(key + "\u{0}" + $0) }
        }
        let nodeOwner = nodeKey.flatMap { draft.graphNodeItems?[$0] }.flatMap { known in
            draft.items.firstIndex { $0.id == known && $0.projectId == projectID }
        }
        if let explicit {
            itemIndex = draft.items.firstIndex { $0.id == explicit && $0.projectId == projectID }
            if let epic = draft.items.first(where: { $0.id == explicit && $0.type == "epic" }),
               let taskID {
                let related = Set(epic.links.filter { $0.kind == "related" }.map(\.targetId))
                let declared = draft.items.indices.filter { index in
                    let item = draft.items[index]
                    return item.projectId == projectID && item.type != "epic"
                        && (item.parentId == epic.id || related.contains(item.id))
                        && item.links.contains { $0.kind == "task" && $0.targetId == taskID
                            && $0.source == "user" }
                }
                if let nodeOwner { itemIndex = nodeOwner }
                else if declared.count == 1 { itemIndex = declared[0] }
                else if declared.count > 1 {
                    return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                        droppedCount: 1, persisted: true, reason: "work_item_binding_ambiguous")
                } else if itemIndex == nil {
                    let members = draft.items.indices.filter {
                        draft.items[$0].projectId == projectID && draft.items[$0].type != "epic"
                            && related.contains(draft.items[$0].id)
                    }
                    if members.count == 1 { itemIndex = members[0] }
                }
            }
            guard itemIndex != nil else {
                return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                                                droppedCount: 1, persisted: true,
                                                reason: "work_item_not_found")
            }
        }
        if itemIndex == nil, let nodeKey, let known = draft.graphNodeItems?[nodeKey] {
            itemIndex = draft.items.firstIndex { $0.id == known && $0.projectId == projectID }
        }
        if let nodeKey, let known = draft.graphNodeItems?[nodeKey],
           let owned = draft.items.firstIndex(where: { $0.id == known && $0.projectId == projectID }),
           let selected = itemIndex, selected != owned {
            if draft.items[selected].type == "epic" {
                // The graph container cannot take ownership away from its established node.
                itemIndex = owned
            } else {
                return AutomaticMutationOutcome(status: .refused, acceptedCount: 0,
                    droppedCount: 1, persisted: true, reason: "graph_node_binding_conflict")
            }
        }
        if itemIndex == nil, let graphID,
           let existingID = draft.graphItems[Self.graphKey(projectID: projectID,
                                                            graphID: graphID)] {
            itemIndex = draft.items.firstIndex {
                $0.id == existingID && $0.projectId == projectID
            }
        }
        if itemIndex == nil, graphID == nil, let taskKey,
           let existingID = draft.taskItems?[taskKey] {
            itemIndex = draft.items.firstIndex {
                $0.id == existingID && $0.projectId == projectID
            }
        }
        // Schema-v1 inferred cards predate their explicit marker. The durable graph/task index is
        // enough to recover that provenance before an explicit attribution replaces the mapping.
        if explicit != nil, let selectedID = itemIndex.map({ draft.items[$0].id }) {
            let legacyInferredIDs = [graphKey.flatMap { draft.graphItems[$0] },
                                     taskKey.flatMap { draft.taskItems?[$0] }]
                .compactMap { $0 }.filter { $0 != selectedID }
            for inferredID in legacyInferredIDs {
                if let index = draft.items.firstIndex(where: { $0.id == inferredID }),
                   !(draft.explicitGraphItems ?? [:]).values.contains(inferredID),
                   !(draft.graphNodeItems ?? [:]).values.contains(inferredID),
                   draft.items[index].inferredSourceKey == nil {
                    draft.items[index].inferredSourceKey = graphKey ?? taskKey
                }
            }
        }
        if itemIndex == nil, let graphID,
           let destination = Self.boundedText(graph?["destination"], maximum: 500),
           draft.items.count < Self.maximumItems {
            var item = makeItem(projectID: projectID, title: destination, type: "feature",
                                summary: "", owner: "", parentID: nil, timestamp: timestamp,
                                projects: draft.projects, items: draft.items)
            item.inferredSourceKey = graphKey
            draft.items.append(item)
            draft.graphItems[Self.graphKey(projectID: projectID, graphID: graphID)] = item.id
            itemIndex = draft.items.count - 1
            changed = true
            acceptedCount += 1
        } else if itemIndex == nil, graphID == nil, let taskID,
                  draft.items.count < Self.maximumItems {
            var item = makeItem(
                projectID: projectID,
                title: Self.boundedText(task["title"], maximum: 300) ?? "Execution \(taskID)",
                type: "task", summary: "Retained broker execution record.",
                owner: Self.brokerOwner(task) ?? "", parentID: nil, timestamp: timestamp,
                projects: draft.projects, items: draft.items)
            item.inferredSourceKey = taskKey
            if !item.owner.isEmpty { item.ownerSourceTaskId = taskID }
            draft.items.append(item)
            var taskItems = draft.taskItems ?? [:]
            if let taskKey { taskItems[taskKey] = item.id }
            draft.taskItems = taskItems
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
        if itemIndex == nil, graphID == nil, let taskID,
           draft.items.count >= Self.maximumItems {
            if recordIngestionDrop(kind: "item", reason: "item_capacity_reached",
                                   sourceID: taskID, coverage: &draft.ingestionCoverage,
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
            let established = nodeKey.flatMap { draft.graphNodeItems?[$0] }
                ?? (nodeKey == nil ? draft.explicitGraphItems?[graphKey] : nil)
            if let established, established != selectedID {
                droppedCount += 1
                droppedReasons.insert("graph_node_binding_conflict")
                if recordIngestionDrop(kind: "graph", reason: "graph_node_binding_conflict",
                                       sourceID: selectedID, item: &draft.items[itemIndex],
                                       timestamp: timestamp) { changed = true }
            } else {
                if let nodeKey, draft.graphNodeItems?[nodeKey] == nil {
                    var bindings = draft.graphNodeItems ?? [:]
                    bindings[nodeKey] = selectedID
                    draft.graphNodeItems = bindings
                    changed = true
                    acceptedCount += 1
                }
                if draft.explicitGraphItems?[graphKey] == nil {
                    draft.graphItems[graphKey] = selectedID
                    var explicitBindings = draft.explicitGraphItems ?? [:]
                    explicitBindings[graphKey] = selectedID
                    draft.explicitGraphItems = explicitBindings
                    changed = true
                    acceptedCount += 1
                }
                // Earlier versions rejected any second item in one graph. Once this exact node
                // binding resolves, that obsolete conflict no longer represents a current gap.
                let oldCount = draft.items[itemIndex].ingestionCoverage?.count ?? 0
                draft.items[itemIndex].ingestionCoverage?.removeAll {
                    $0.kind == "graph" && $0.reason == "graph_binding_conflict"
                }
                if oldCount != (draft.items[itemIndex].ingestionCoverage?.count ?? 0) { changed = true }
            }
        }
        guard let selected = itemIndex else {
            return AutomaticMutationOutcome(status: .unchanged, acceptedCount: 0,
                                            droppedCount: 0, persisted: true,
                                            reason: "irrelevant")
        }

        let child = task["child"] as? [String: Any]
        let sessionID = Self.boundedText(child?["sessionId"] ?? child?["session_id"], maximum: 200)
        let taskOwner = Self.brokerOwner(task)
        let incomingWorktreeTarget: String? = {
            guard let worktree = task["worktree"] as? [String: Any],
                  let path = Self.boundedText(worktree["path"], maximum: 2_000),
                  let opaque = Self.stringDigest("\(projectID)\u{0}\(path)") else { return nil }
            return "worktree:\(opaque.prefix(32))"
        }()
        var reattributedItemIDs: Set<String> = []

        if let taskID {
            if let taskKey, draft.taskItems?[taskKey] != draft.items[selected].id {
                var taskItems = draft.taskItems ?? [:]
                taskItems[taskKey] = draft.items[selected].id
                draft.taskItems = taskItems
                changed = true
                acceptedCount += 1
            }
            if draft.items[selected].owner.isEmpty, let owner = taskOwner {
                draft.items[selected].owner = owner
                draft.items[selected].ownerSourceTaskId = taskID
                changed = true
                acceptedCount += 1
            }
            let declaredPhase = Self.boundedText(
                task["workPhase"] ?? task["work_phase"], maximum: 64
            ).flatMap { Self.phases.contains($0) ? $0 : nil }
            let attemptState = Self.boundedText(task["state"], maximum: 64)
            let attemptStartedAt = Self.canonicalBrokerStart(task)
            let attemptFinishedAt = Self.exactDouble(task["finished_at"] ?? task["finishedAt"])
            let attemptEventAt = attemptFinishedAt ?? attemptStartedAt
            for index in draft.items.indices
            where index != selected && draft.items[index].projectId == projectID {
                if reattributeTaskFacts(taskID: taskID, taskOwner: taskOwner,
                                        sessionID: sessionID,
                                        worktreeTarget: incomingWorktreeTarget,
                                        from: index, to: selected, draft: &draft) {
                    reattributedItemIDs.insert(draft.items[index].id)
                    appendHistory(item: &draft.items[index], actor: "broker",
                                  kind: "task_reattributed",
                                  summary: "Broker execution facts moved to their explicit item.",
                                  at: timestamp)
                    draft.items[index].updatedAt = timestamp
                    changed = true
                    acceptedCount += 1
                }
            }
            if let existing = draft.items[selected].links.firstIndex(where: {
                $0.kind == "task" && $0.targetId == taskID && $0.source == "broker"
            }) {
                let old = draft.items[selected].links[existing]
                if old.graphId == nil, let graphID {
                    draft.items[selected].links[existing].graphId = graphID
                    draft.items[selected].links[existing].graphNodeId = graphNode
                    changed = true
                }
                if shouldReplaceAttempt(old, state: attemptState, at: attemptEventAt) {
                    let ordinal = Self.nextEventOrdinal(&eventSequence)
                    draft.items[selected].links[existing].phase = declaredPhase
                    draft.items[selected].links[existing].attemptState = attemptState
                    draft.items[selected].links[existing].startedAt = attemptStartedAt
                    draft.items[selected].links[existing].finishedAt = attemptFinishedAt
                    draft.items[selected].links[existing].statusObservedAt = attemptEventAt
                    draft.items[selected].links[existing].sourceTaskId = taskID
                    draft.items[selected].links[existing].eventAt = attemptEventAt
                    draft.items[selected].links[existing].eventOrdinal = ordinal
                    changed = true
                    acceptedCount += 1
                } else if old.sourceTaskId == nil || old.eventOrdinal == nil
                            || (attemptEventAt != nil && old.eventAt == nil) {
                    // Schema-v1 rows used observation time as statusObservedAt. A replay supplies
                    // the source boundary and repairs that legacy ordering exactly once.
                    draft.items[selected].links[existing].sourceTaskId = taskID
                    draft.items[selected].links[existing].eventAt = attemptEventAt
                    if old.eventOrdinal == nil {
                        draft.items[selected].links[existing].eventOrdinal =
                            Self.nextEventOrdinal(&eventSequence)
                    }
                    changed = true
                    acceptedCount += 1
                }
            } else if draft.items[selected].links.count < Self.maximumChildren {
                let ordinal = Self.nextEventOrdinal(&eventSequence)
                draft.items[selected].links.append(StoredLink(
                    id: Self.newID(), kind: "task", targetId: taskID,
                    label: Self.boundedText(task["title"], maximum: 300) ?? taskID,
                    source: "broker", phase: declaredPhase, head: nil,
                    attemptState: attemptState, startedAt: attemptStartedAt,
                    finishedAt: attemptFinishedAt,
                    statusObservedAt: attemptEventAt, sourceTaskId: taskID,
                    eventAt: attemptEventAt, eventOrdinal: ordinal,
                    graphId: graphID, graphNodeId: graphNode))
                appendHistory(item: &draft.items[selected], actor: "broker", kind: "task_linked",
                              summary: "Linked execution attempt \(taskID).", at: timestamp,
                              sourceTaskId: taskID)
                changed = true
                acceptedCount += 1
            } else {
                droppedCount += 1
                droppedReasons.insert("link_capacity_reached")
                if recordIngestionDrop(kind: "task", reason: "link_capacity_reached",
                                       sourceID: taskID, item: &draft.items[selected],
                                       timestamp: timestamp) { changed = true }
            }
            let acceptedTaskEventAt = draft.items[selected].links.first(where: {
                $0.kind == "task" && $0.targetId == taskID && $0.source == "broker"
            })?.eventAt
            if let landing = task["landing"] as? [String: Any],
               let disposition = landing["state"] as? String,
               ["pending", "landed", "nothing_to_land", "abandoned"].contains(disposition),
               let since = Self.exactDouble(landing["since"]),
               let index = draft.items[selected].links.firstIndex(where: {
                   $0.kind == "task" && $0.targetId == taskID && $0.source == "broker"
               }) {
                let old = draft.items[selected].links[index]
                if old.landingDispositionAt == nil || since > old.landingDispositionAt!
                    || (since == old.landingDispositionAt && old.landingDisposition == "pending" && disposition != "pending") {
                    draft.items[selected].links[index].landingDisposition = disposition
                    draft.items[selected].links[index].landingDispositionAt = since
                    changed = true
                    acceptedCount += 1
                }
            }
            let evidenceResult = ingestBrokerEvidence(
                task: task, taskID: taskID, graphID: graphID,
                item: &draft.items[selected], timestamp: timestamp,
                taskEventAt: acceptedTaskEventAt, eventSequence: &eventSequence)
            if evidenceResult.changed {
                changed = true
            }
            acceptedCount += evidenceResult.acceptedCount
            droppedCount += evidenceResult.droppedCount
            droppedReasons.formUnion(evidenceResult.reasons)
            if evidenceResult.invalidatedCurrentEvidence {
                reopenAncestors(of: draft.items[selected].id, timestamp: timestamp, draft: &draft)
                eventSequence = max(eventSequence, draft.eventSequence ?? 0)
            }
        }

        if let sessionID, let existing = draft.items[selected].links.firstIndex(where: {
            $0.kind == "session" && $0.targetId == sessionID
                && ($0.sourceTaskId == nil || $0.sourceTaskId == taskID)
        }) {
            if draft.items[selected].links[existing].source == "broker",
               draft.items[selected].links[existing].sourceTaskId == nil {
                draft.items[selected].links[existing].sourceTaskId = taskID
                changed = true
                acceptedCount += 1
            }
        } else if let sessionID, draft.items[selected].links.count < Self.maximumChildren {
            draft.items[selected].links.append(StoredLink(
                id: Self.newID(), kind: "session", targetId: sessionID, label: sessionID,
                source: "broker", phase: nil, head: nil,
                sourceTaskId: taskID))
            changed = true
            acceptedCount += 1
        } else if let sessionID,
                  !draft.items[selected].links.contains(where: {
                      $0.kind == "session" && $0.targetId == sessionID
                          && ($0.sourceTaskId == nil || $0.sourceTaskId == taskID)
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
                    && ($0.sourceTaskId == nil || $0.sourceTaskId == taskID)
            }) {
                if draft.items[selected].links[existing].label != branch
                    || draft.items[selected].links[existing].head != head
                    || draft.items[selected].links[existing].sourceTaskId == nil {
                    draft.items[selected].links[existing].label = branch
                    draft.items[selected].links[existing].head = head
                    draft.items[selected].links[existing].sourceTaskId = taskID
                    changed = true
                    acceptedCount += 1
                }
            } else if draft.items[selected].links.count < Self.maximumChildren {
                draft.items[selected].links.append(StoredLink(
                    id: Self.newID(), kind: "worktree", targetId: target, label: branch,
                    source: "broker", phase: nil, head: head,
                    sourceTaskId: taskID))
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
            // A root may declare the child's interval after the broker started it. Terminal
            // catch-up can settle that matching interval, never a newer Session work round.
            if terminal, let explicitEnd {
                for spanIndex in draft.items[selected].spans.indices {
                    let span = draft.items[selected].spans[spanIndex]
                    if span.sessionId == sessionID && span.source != "broker"
                        && span.endedAt == nil && span.startedAt >= started
                        && span.startedAt <= explicitEnd {
                        draft.items[selected].spans[spanIndex].endedAt = explicitEnd
                        changed = true
                        acceptedCount += 1
                    }
                }
            }
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

        let reconciled = reconcileLifecycle(around: draft.items[selected].id, actor: "board",
                                            timestamp: timestamp, draft: &draft)
        if reconciled > 0 {
            changed = true
            acceptedCount += reconciled
        }
        for itemID in reattributedItemIDs {
            let count = reconcileLifecycle(around: itemID, actor: "board",
                                           timestamp: timestamp, draft: &draft)
            if count > 0 {
                changed = true
                acceptedCount += count
            }
        }
        if eventSequence != initialEventSequence { draft.eventSequence = eventSequence }

        guard changed else {
            lastAutomaticFailure = nil
            let status: AutomaticMutationStatus = droppedCount > 0 ? .refused : .unchanged
            return AutomaticMutationOutcome(status: status, acceptedCount: acceptedCount,
                                            droppedCount: droppedCount, persisted: true,
                                            reason: droppedReasons.sorted().first)
        }
        draft.items[selected].updatedAt = timestamp
        let retiredCount = retireInferredItems(reattributedItemIDs, draft: &draft)
        acceptedCount += retiredCount
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
        case "set_ai_consent":
            guard let enabled = Self.exactBool(body["enabled"]),
                  let provider = body["provider"] as? String, ["codex", "claude"].contains(provider),
                  body["policy"] as? String == "board-reading-v1" else {
                throw BoardError(status: 400, code: "invalid_ai_consent",
                    message: "Board AI requires a named provider and board-reading-v1 consent.")
            }
            let consent = enabled ? provider : nil
            if draft.narrativeConsent != consent {
                draft.narrativeConsent = consent
                draft.presentationEpoch = (draft.presentationEpoch ?? 0) + 1
            }
            return Applied(itemId: nil)
        case "set_enabled":
            guard let value = Self.exactBool(body["enabled"]) else {
                throw BoardError(status: 400, code: "invalid_enabled",
                                 message: "enabled must be a Boolean")
            }
            if draft.enabled != value {
                draft.presentationEpoch = (draft.presentationEpoch ?? 0) + 1
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
            var item = makeItem(projectID: projectID, title: title, type: type,
                                summary: summary, owner: owner, parentID: parent,
                                timestamp: timestamp, projects: draft.projects, items: draft.items)
            item.typeDetails = try parsedTypeDetails(body["typeDetails"], type: type,
                                                     existing: nil)
            draft.items.append(item)
            return Applied(itemId: item.id)

        case "update":
            let index = try itemIndex(body, draft: draft)
            try requireRecordable(index, draft: draft)
            var changed = false
            let oldType = draft.items[index].type
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
                draft.items[index].ownerSourceTaskId = nil
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
            if body.keys.contains("typeDetails") {
                let existing = oldType == draft.items[index].type
                    ? draft.items[index].typeDetails : nil
                draft.items[index].typeDetails = try parsedTypeDetails(
                    body["typeDetails"], type: draft.items[index].type, existing: existing)
                changed = true
            } else if oldType != draft.items[index].type {
                draft.items[index].typeDetails = nil
            }
            guard changed else {
                throw BoardError(status: 400, code: "empty_update",
                                 message: "update must name at least one mutable field")
            }
            reopenForInvalidatedEvidence(index, timestamp: timestamp, draft: &draft)
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
                let scopeOrdinal = nextEventOrdinal(&draft)
                invalidateCurrentEvidence(&draft.items[index], reopen: false,
                                          eventAt: timestamp,
                                          eventOrdinal: scopeOrdinal)
            }
            touch(&draft.items[index], actor: actor, kind: "state_transition",
                  summary: note ?? "State changed from \(old) to \(target).", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "checklist":
            let index = try itemIndex(body, draft: draft)
            try requireRecordable(index, draft: draft)
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
                    evidenceId: nil, supplementRelationId: body["supplementRelationId"] as? String))
            }
            reopenForInvalidatedEvidence(index, timestamp: timestamp, draft: &draft)
            touch(&draft.items[index], actor: actor, kind: "checklist_updated",
                  summary: "Checklist scope or status changed.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "milestone":
            let index = try itemIndex(body, draft: draft)
            try requireRecordable(index, draft: draft)
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
            reopenForInvalidatedEvidence(index, timestamp: timestamp, draft: &draft)
            touch(&draft.items[index], actor: actor, kind: "milestone_updated",
                  summary: "Milestone scope or status changed.", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "artifact":
            let index = try itemIndex(body, draft: draft)
            try requireRecordable(index, draft: draft)
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
            reopenForInvalidatedEvidence(index, timestamp: timestamp, draft: &draft)
            touch(&draft.items[index], actor: actor, kind: "artifact_added",
                  summary: "An artifact reference was added; it is not verification by itself.",
                  at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "document_reference":
            let index = try itemIndex(body, draft: draft)
            var references = draft.items[index].documentReferences ?? []
            guard references.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "document_reference_capacity_reached",
                                 message: "the document reference capacity is exhausted")
            }
            let documentID = try requiredText(body, "documentId", maximum: 200)
            guard let version = Self.exactInt(body["version"]), version > 0 else {
                throw BoardError(status: 400, code: "invalid_document_version",
                                 message: "document version must be a positive integer")
            }
            let purpose = try requiredChoice(body, "purpose", choices: Self.documentPurposes)
            let rawURL = try requiredText(body, "url", maximum: 2_000)
            guard Self.isCanonicalCloudDocumentURL(rawURL) else {
                throw BoardError(status: 400, code: "invalid_document_url",
                                 message: "document references require a canonical app.clawdline.com document URL")
            }
            guard !references.contains(where: {
                $0.documentId == documentID && $0.version == version
            }) else {
                throw BoardError(status: 409, code: "document_version_conflict",
                                 message: "that stable document identity already has this version")
            }
            let earlier = references.filter { $0.documentId == documentID }
            let supersedesID = try optionalText(body, "supersedesId", maximum: 200)
            if earlier.isEmpty {
                guard version == 1, supersedesID == nil else {
                    throw BoardError(status: 409, code: "document_initial_version_required",
                                     message: "a new document identity must begin at version 1 without a predecessor")
                }
            } else {
                let superseded = Set(references.compactMap(\.supersedesId))
                let heads = earlier.filter { !superseded.contains($0.id) }
                guard let predecessorID = supersedesID,
                      heads.count == 1, let predecessor = heads.first,
                      predecessor.id == predecessorID,
                      predecessor.purpose == purpose, predecessor.version < version else {
                    throw BoardError(status: 409, code: "document_supersession_required",
                                     message: "a later document version must supersede the current head of the same purpose")
                }
            }
            references.append(StoredDocumentReference(
                id: Self.newID(), documentId: documentID, version: version,
                title: try requiredText(body, "title", maximum: 300), url: rawURL,
                purpose: purpose, supersedesId: supersedesID, addedAt: timestamp, actor: actor))
            draft.items[index].documentReferences = references
            // This intentionally does not call reopenForInvalidatedEvidence: it changes the
            // reading trail, never the work scope or the authority of existing receipts.
            touch(&draft.items[index], actor: actor, kind: "document_reference_added",
                  summary: "A narrative-only document reference was added.", at: timestamp)
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
            try requireRecordable(index, draft: draft)
            guard draft.items[index].obligations.count < Self.maximumChildren else {
                throw BoardError(status: 409, code: "obligation_capacity_reached",
                                 message: "the obligation capacity is exhausted")
            }
            guard let blocking = Self.exactBool(body["blocking"]) else {
                throw BoardError(status: 400, code: "invalid_blocking",
                                 message: "blocking must be a Boolean")
            }
            let actorKind = try optionalChoice(body, "actorKind",
                                               choices: Self.obligationActorKinds)
            draft.items[index].obligations.append(StoredObligation(
                id: Self.newID(), title: try requiredText(body, "title", maximum: 500),
                owner: try requiredText(body, "owner", maximum: 300),
                blocking: blocking, resolved: false, actorKind: actorKind,
                requiredAction: try optionalText(body, "requiredAction", maximum: 1_000),
                blockingScope: try optionalText(body, "blockingScope", maximum: 300),
                supplementRelationId: body["supplementRelationId"] as? String))
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
            let supersededBy = try optionalText(body, "supersededBy", maximum: 200)
            if let supersededBy {
                guard supersededBy != obligationID,
                      draft.items[index].obligations.contains(where: { $0.id == supersededBy }) else {
                    throw BoardError(status: 400, code: "invalid_obligation_successor",
                                     message: "supersededBy must name another obligation on this item")
                }
            }
            draft.items[index].obligations[child].resolved = true
            draft.items[index].obligations[child].resolutionEvidence = try optionalText(
                body, "resolutionEvidence", maximum: 1_000)
            draft.items[index].obligations[child].supersededBy = supersededBy
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
            let declarationID = try requiredText(body, "requestId", maximum: 200)
            closeActiveSpans(sessionID: sessionID, at: timestamp, items: &draft.items)
            draft.items[index].spans.append(StoredSpan(
                id: Self.declaredSpanID(actor: actor, requestID: declarationID),
                sessionId: sessionID, phase: phase,
                startedAt: timestamp, endedAt: nil))
            touch(&draft.items[index], actor: actor, kind: "span_started",
                  summary: "Session \(sessionID) declared phase \(phase).", at: timestamp)
            return Applied(itemId: draft.items[index].id)

        case "end_span":
            // Interval bookkeeping is permitted after delivery/closure, but cannot reopen,
            // verify, land, or accept anything. Bind to one exact declaration, not whichever
            // interval the Session happens to be running when a delayed receipt arrives.
            let index = try itemIndex(body, draft: draft)
            let sessionID = try requiredText(body, "sessionId", maximum: 300)
            let startRequestID = try requiredText(body, "startRequestId", maximum: 200)
            let note = try requiredText(body, "note", maximum: 1_000)
            let spanID = Self.declaredSpanID(actor: actor, requestID: startRequestID)
            guard let spanIndex = draft.items[index].spans.firstIndex(where: {
                $0.id == spanID && $0.sessionId == sessionID && $0.source != "broker"
            }) else {
                throw BoardError(status: 409, code: "span_identity_unresolved",
                                 message: "No exact declaration matches; legacy or foreign spans are not guessed")
            }
            if draft.items[index].spans[spanIndex].endedAt == nil {
                draft.items[index].spans[spanIndex].endedAt = max(
                    timestamp, draft.items[index].spans[spanIndex].startedAt)
                touch(&draft.items[index], actor: actor, kind: "span_ended",
                      summary: note, at: timestamp)
            }
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
            let eventOrdinal = nextEventOrdinal(&draft)
            draft.items[index].evidence.append(StoredEvidence(
                id: evidenceID, kind: "artifact_acceptance", summary: note,
                subject: artifactID, status: "passed", sourceId: sourceID,
                checklistId: nil, artifactId: artifactID, blocking: false, resolved: true,
                at: timestamp, actor: actor, source: "root_attestation",
                scopeRevision: scopeRevision, eventAt: timestamp,
                eventOrdinal: eventOrdinal))
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
                reopenForInvalidatedEvidence(index, timestamp: timestamp, draft: &draft)
            }
            let scopeRevision = draft.items[index].scopeRevision ?? 0
            let evidenceID = Self.newID()
            let eventOrdinal = nextEventOrdinal(&draft)
            draft.items[index].evidence.append(StoredEvidence(
                id: evidenceID, kind: kind,
                summary: summary, subject: subject, status: status,
                sourceId: sourceID, checklistId: checklistID, artifactId: nil,
                blocking: kind == "finding" && blocking,
                resolved: kind == "finding" ? (resolved || status == "resolved") : true,
                at: timestamp, actor: actor, source: "root_attestation",
                scopeRevision: scopeRevision, eventAt: timestamp,
                eventOrdinal: eventOrdinal))
            if kind == "verification" {
                if status == "passed" {
                    if !usesArtifactAcceptance(draft.items[index]) {
                        draft.items[index].currentVerificationEvidenceId = evidenceID
                        draft.items[index].currentVerificationSubject = subject
                        if subjectChanged { draft.items[index].currentLandingEvidenceId = nil }
                        if let landing = draft.items[index].evidence.last(where: {
                            $0.kind == "landing" && $0.status == "passed"
                                && $0.subject == subject
                                && ($0.scopeRevision ?? 0) == (draft.items[index].scopeRevision ?? 0)
                        }) {
                            draft.items[index].currentLandingEvidenceId = landing.id
                        }
                        draft.items[index].currentArtifactAcceptanceId = nil
                    }
                } else {
                    let scopeOrdinal = nextEventOrdinal(&draft)
                    invalidateCurrentEvidence(&draft.items[index], reopen: true,
                                              eventAt: timestamp,
                                              eventOrdinal: scopeOrdinal)
                    reopenAncestors(of: draft.items[index].id, timestamp: timestamp, draft: &draft)
                }
            } else if blocking && !(resolved || status == "resolved") {
                let scopeOrdinal = nextEventOrdinal(&draft)
                invalidateCurrentEvidence(&draft.items[index], reopen: true,
                                          eventAt: timestamp,
                                          eventOrdinal: scopeOrdinal)
                reopenAncestors(of: draft.items[index].id, timestamp: timestamp, draft: &draft)
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

        case "record_report":
            guard trusted else {
                throw BoardError(status: 403, code: "trusted_report_required",
                                 message: "record_report requires an authenticated local root")
            }
            let index = try itemIndex(body, draft: draft)
            let item = draft.items[index]
            if item.type != "coordination" && !reportEligible(item) {
                throw BoardError(status: 409, code: "report_requires_landed_or_closed_item",
                                 message: "record_report requires closed work or an authoritative current-scope landing")
            }
            let prior = draft.items[index].completionReports ?? []
            guard prior.count < Self.maximumReportVersions else {
                throw BoardError(status: 409, code: "report_history_capacity_reached",
                                 message: "the bounded completion report history is full; no version was discarded")
            }
            let authorship = try requiredChoice(
                body, "authorship", choices: Set(["assistant", "human"]))
            let modelText = try optionalText(body, "model", maximum: 200)
            let model = modelText.flatMap { $0.isEmpty ? nil : $0 }
            if authorship == "human", model != nil {
                throw BoardError(status: 400, code: "invalid_report_model",
                                 message: "model is valid only for assistant-authored narrative")
            }
            let sources = try reportSources(body["sourceReferences"], itemIndex: index,
                                            resolvedAt: timestamp, draft: draft)
            let boundary = try reportBoundary(body["reportBoundary"], itemIndex: index,
                                              sources: sources, draft: draft)
            let report = StoredCompletionReport(
                id: Self.newID(), version: prior.count + 1,
                objective: try requiredText(body, "objective", maximum: Self.maximumUTF8),
                deliveredOutcomes: try requiredText(body, "deliveredOutcomes",
                                                    maximum: Self.maximumUTF8),
                verificationLanding: try requiredText(body, "verificationLanding",
                                                       maximum: Self.maximumUTF8),
                remainingWork: try requiredText(body, "remainingWork",
                                                maximum: Self.maximumUTF8),
                lessons: try requiredText(body, "lessons", maximum: Self.maximumUTF8),
                sourceReferences: sources, authoredAt: timestamp, actor: actor,
                authorship: authorship, model: model,
                scopeRevision: draft.items[index].scopeRevision ?? 0,
                itemStateAtAuthorship: draft.items[index].state,
                reportBoundary: boundary)
            draft.items[index].completionReports = prior + [report]
            touch(&draft.items[index], actor: actor, kind: "completion_report_recorded",
                  summary: "Completion report version \(report.version) was recorded as attributed narrative, not evidence.",
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
            guard item.obligations.allSatisfy(\.resolved),
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

    private func currentChecklistEvidenceIsValid(_ item: StoredItem) -> Bool {
        (try? validateCurrentChecklistEvidence(item)) != nil
    }

    private func closureIsSafe(_ item: StoredItem, allItems: [StoredItem]) -> Bool {
        guard item.obligations.allSatisfy(\.resolved),
              item.milestones.allSatisfy({
                  $0.status == "passed" || $0.status == "not_applicable"
              }), item.pendingHandoff == nil else { return false }
        guard item.type == "epic" else { return true }
        return allItems.filter { $0.parentId == item.id }.allSatisfy {
            ["integrated", "closed"].contains($0.state)
        }
    }

    /// Reconcile the item and its Epic ancestors from durable facts. The loop is bounded by the
    /// two-level hierarchy invariant and lets a child delivery unlock its already-proven Epic.
    @discardableResult
    private func reconcileLifecycle(around itemID: String, actor: String, timestamp: Double,
                                    draft: inout StoredState) -> Int {
        var ids = [itemID]
        var parent = draft.items.first(where: { $0.id == itemID })?.parentId
        while let id = parent {
            ids.append(id)
            parent = draft.items.first(where: { $0.id == id })?.parentId
        }
        var changed = 0
        for id in ids {
            guard let index = draft.items.firstIndex(where: { $0.id == id }),
                  draft.items[index].state != "canceled",
                  draft.items[index].type != "coordination" else { continue }
            let item = draft.items[index]
            let attempts = item.links.filter {
                $0.kind == "task" && $0.source == "broker" && !isSettledNoChange($0)
            }
            let openBlockingFinding = item.evidence.contains {
                $0.kind == "finding" && $0.blocking && !$0.resolved
            }
            let owned = !item.owner.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            let verified = owned && !openBlockingFinding
                && currentChecklistEvidenceIsValid(item) && hasVerificationEvidence(item)
            let delivered = verified && hasDeliveryEvidence(item)
            let currentLanding = item.currentLandingEvidenceId.flatMap { id in
                item.evidence.first(where: { $0.id == id })
            }
            let currentVerification = item.currentVerificationEvidenceId.flatMap { id in
                item.evidence.first(where: { $0.id == id })
            } ?? item.currentArtifactAcceptanceId.flatMap { id in
                item.evidence.first(where: { $0.id == id })
            }
            let newerAttemptAfterLanding = currentLanding.map { landing in
                attempts.contains { attempt in
                    attemptOccurredAfter(attempt, evidence: landing)
                }
            } ?? false
            let attemptAfterCurrentVerification = currentVerification.map { verification in
                attempts.contains { attempt in
                    attemptOccurredAfter(attempt, evidence: verification)
                }
            } ?? !attempts.isEmpty
            let target: String
            if delivered && !newerAttemptAfterLanding {
                target = closureIsSafe(item, allItems: draft.items) ? "closed" : "integrated"
            } else if verified && !attemptAfterCurrentVerification {
                target = "verified"
            } else if !attempts.isEmpty {
                let active = attempts.filter {
                    Self.activeAttemptStates.contains($0.attemptState ?? "")
                }
                if !owned {
                    target = "planning"
                } else if !active.isEmpty,
                          active.allSatisfy({ $0.attemptState == "queued" || $0.attemptState == "spawning" }) {
                    target = "ready"
                } else if !active.isEmpty,
                          active.allSatisfy({ $0.phase == "planning" }) {
                    target = "planning"
                } else {
                    // Success is delivery, while failure/cancellation is an attempt outcome. None
                    // of those facts by itself establishes verification or closes the work item.
                    target = "execution"
                }
            } else {
                target = item.state
            }
            guard target != item.state else { continue }
            draft.items[index].state = target
            touch(&draft.items[index], actor: actor, kind: "automatic_state_reconciled",
                  summary: "Recorded evidence reconciled lifecycle from \(item.state) to \(target).",
                  at: timestamp)
            changed += 1
        }
        return changed
    }

    private func progressObject(_ item: StoredItem) -> [String: Any] {
        let attempts = item.links.filter { $0.kind == "task" && $0.source == "broker" }
        let states = attempts.map { $0.attemptState }
        let activeAttempts = attempts.filter {
            Self.activeAttemptStates.contains($0.attemptState ?? "")
        }
        let queued = attempts.filter {
            $0.attemptState == "queued" || $0.attemptState == "spawning"
        }.count
        let succeeded = attempts.filter { $0.attemptState == "success" }.count
        let failed = attempts.filter {
            $0.attemptState == "failure" || $0.attemptState == "timeout"
                || $0.attemptState == "spawn_failed"
        }.count
        let canceled = attempts.filter { $0.attemptState == "cancelled" }.count
        let unknownAttempts = states.filter {
            guard let value = $0 else { return true }
            return !Self.activeAttemptStates.contains(value)
                && !Self.terminalAttemptStates.contains(value)
        }.count
        let findings = item.evidence.filter { $0.kind == "finding" }
        let openFindings = findings.filter { !$0.resolved }
        let blockingFindings = openFindings.filter(\.blocking)
        let passedVerifications = item.evidence.filter {
            $0.kind == "verification" && $0.status == "passed"
        }
        let failedVerifications = item.evidence.filter {
            $0.kind == "verification" && $0.status == "failed"
        }
        let landings = item.evidence.filter { $0.kind == "landing" && $0.status == "passed" }
        let verificationSummaries = item.evidence.filter { $0.kind == "verification_summary" }
        let artifactAcceptances = item.evidence.filter { $0.kind == "artifact_acceptance" }
        let currentEvidenceVerified = hasVerificationEvidence(item)
            && currentChecklistEvidenceIsValid(item) && blockingFindings.isEmpty
        let latestLanding = landings.max { eventOrder($0) < eventOrder($1) }
        let uncertainAttemptsAfterLanding = latestLanding.map { landing in
            attempts.filter { attempt in
                attempt.eventAt == nil
                    && (attempt.eventOrdinal ?? 0) > (landing.eventOrdinal ?? 0)
            }
        } ?? []
        let roundAttempts = latestLanding.map { landing in
            attempts.filter { attempt in
                !isSettledNoChange(attempt) && (Self.activeAttemptStates.contains(attempt.attemptState ?? "")
                    || eventOrder(attempt) > eventOrder(landing)
                    || uncertainAttemptsAfterLanding.contains { $0.id == attempt.id })
            }
        } ?? attempts
        let roundActive = roundAttempts.filter {
            Self.activeAttemptStates.contains($0.attemptState ?? "")
        }
        // A queued attempt names the phase it will perform, not an observed activity.
        let roundPhases = Set(roundActive.filter { $0.attemptState == "briefed" }.compactMap(\.phase))
        let declaredActivity = item.spans.filter { span in
            span.endedAt == nil && span.source != "broker"
        }.max { $0.startedAt < $1.startedAt }
        let latestRoundAttempt = roundAttempts.max { eventOrder($0) < eventOrder($1) }
        let roundSucceeded = latestRoundAttempt?.attemptState == "success"
        let roundFailed = latestRoundAttempt.map {
            $0.attemptState == "failure" || $0.attemptState == "timeout"
                || $0.attemptState == "spawn_failed"
        } ?? false
        let currentVerification = item.currentVerificationEvidenceId.flatMap { id in
            item.evidence.first(where: { $0.id == id })
        } ?? item.currentArtifactAcceptanceId.flatMap { id in
            item.evidence.first(where: { $0.id == id })
        }
        let currentVerified = currentEvidenceVerified && (currentVerification.map { verification in
            !roundAttempts.contains { attemptOccurredAfter($0, evidence: verification) }
        } ?? false)
        let latestVerification = item.evidence.filter { $0.kind == "verification" }
            .max { eventOrder($0) < eventOrder($1) }
        let failedProofAfterLanding = latestVerification.map { verification in
            verification.status == "failed"
                && (latestLanding.map { eventOrder(verification) > eventOrder($0) } ?? true)
        } ?? false
        let blockingAfterLanding = latestLanding.map { landing in
            blockingFindings.contains { eventOrder($0) > eventOrder(landing) }
        } ?? !blockingFindings.isEmpty
        let scopeChangedAfterLanding = latestLanding.map {
            ($0.scopeRevision ?? 0) < (item.scopeRevision ?? 0)
        } ?? false
        let activeSpanCount = item.spans.filter { $0.endedAt == nil }.count
        var isActive = !activeAttempts.isEmpty || activeSpanCount > 0
        var warningCodes: [String] = []
        if !uncertainAttemptsAfterLanding.isEmpty {
            warningCodes.append("attempt_chronology_unresolved")
        }
        if latestLanding != nil {
            if !hasVerificationEvidence(item) { warningCodes.append("exact_verification_missing") }
            if item.checklist.contains(where: { $0.required })
                && !currentChecklistEvidenceIsValid(item) {
                warningCodes.append("checklist_evidence_incomplete")
            }
            if blockingFindings.contains(where: { finding in
                latestLanding.map { eventOrder(finding) <= eventOrder($0) } ?? false
            }) { warningCodes.append("historical_findings_unresolved") }
            if item.milestones.contains(where: {
                $0.status != "passed" && $0.status != "not_applicable"
            }) { warningCodes.append("milestones_incomplete") }
            if item.obligations.contains(where: { !$0.resolved }) {
                warningCodes.append("obligations_unresolved")
            }
            if item.pendingHandoff != nil { warningCodes.append("handoff_pending") }
        }

        var progress: (state: String, label: String, reason: String, basis: [String])
        if item.type == "coordination" {
            if !roundActive.isEmpty && roundActive.allSatisfy({
                $0.attemptState == "queued" || $0.attemptState == "spawning"
            }) {
                progress = ("queued", "Queued", "Coordination is waiting to begin; lifecycle does not apply.",
                            ["coordination_context", "queued_attempt"])
            } else if isActive {
                progress = ("execution", "Active", "Coordination has an active attempt or declared interval; lifecycle does not apply.",
                            ["coordination_context", "active_interval"])
            } else {
                progress = ("unknown", "Coordination", "Coordination is described by its period, handoff, relations, and outcomes rather than lifecycle status.",
                            ["coordination_context", "lifecycle_not_applicable"])
            }
        } else if item.state == "canceled" {
            progress = ("canceled", "Canceled", "The work item was explicitly canceled.",
                        ["item_canceled"])
        } else if roundPhases.contains("correction") {
            progress = ("correction", "Correction", "A linked broker attempt is correcting the work.",
                        ["active_correction_attempt"])
        } else if roundPhases.contains("review_testing") {
            progress = ("review_testing", "Review & testing", "A linked broker review or test attempt is active.",
                        ["active_review_testing_attempt"])
        } else if roundPhases.contains("planning") {
            progress = ("planning", "Planning", "A linked planning attempt is active.",
                        ["active_planning_attempt"])
        } else if let declaredActivity, roundActive.allSatisfy({ $0.attemptState != "briefed" }) {
            // A declared interval is useful activity information, but never promote it into
            // a broker-observed attempt or verification. In particular, yesterday's successful
            // discovery cannot make today's declared implementation look delivered.
            let phase = declaredActivity.phase
            let declaredState = phase == "planning" ? "planning"
                : (phase == "review_testing" ? "review_testing"
                   : (phase == "correction" ? "correction" : "execution"))
            progress = (declaredState, "Declared activity",
                        "A Session declared an active \(phase) interval; broker execution is not established by this declaration.",
                        ["declared_\(phase)_span"])
        } else if !roundActive.isEmpty && roundActive.allSatisfy({
            $0.attemptState == "queued" || $0.attemptState == "spawning"
        }) {
            progress = ("queued", "Queued", "Linked broker attempts are waiting to execute.",
                        ["queued_attempt"])
        } else if !roundActive.isEmpty {
            progress = ("execution", "In progress", "A linked broker attempt is executing.",
                        ["active_execution_attempt"])
        } else if failedProofAfterLanding || blockingAfterLanding || scopeChangedAfterLanding {
            let basis = failedProofAfterLanding ? "verification_failed_after_landing"
                : (blockingAfterLanding ? "blocking_finding_after_landing"
                                        : "scope_changed_after_landing")
            progress = latestLanding == nil
                ? ("blocked", "Blocked", "Current proof or a blocking finding requires correction.",
                   [failedProofAfterLanding ? "verification_failed" : "open_blocking_finding"])
                : ("correction", "Correction", "New evidence after the last landing opened another work round.",
                   [basis, "historical_landing_retained"])
        } else if let latestLanding, roundAttempts.isEmpty {
            progress = ("landed", "Landed", "Broker ancestry confirms the latest observed delivery landed.",
                        ["authoritative_broker_landing", "landing:\(Int(latestLanding.at))"])
        } else if usesArtifactAcceptance(item) && currentVerified && hasDeliveryEvidence(item) {
            progress = ("landed", "Delivered", "An administrative user accepted the current non-code artifact.",
                        ["authoritative_artifact_acceptance"])
        } else if item.inferredSourceKey != nil && item.type == "task"
                    && !attempts.isEmpty && attempts.allSatisfy(isSettledNoChange)
                    && !isActive && blockingFindings.isEmpty
                    && !item.obligations.contains(where: { $0.blocking && !$0.resolved }) {
            progress = ("settled", "Execution finished",
                        "The root confirmed this execution needs no repository landing; this is not a Feature verification claim.",
                        ["root_settled_no_change_execution"])
        } else if currentVerified {
            progress = ("verified", "Verified", "Current-scope trusted verification passed.",
                        ["current_exact_verification"])
        } else if roundSucceeded {
            progress = ("delivered", "Delivered", "A child delivered output; integration is not yet proven.",
                        [latestLanding == nil ? "task_delivery_only" : "new_delivery_after_landing"])
        } else if roundFailed {
            progress = latestLanding == nil
                ? ("blocked", "Blocked", "The retained attempts ended without delivery.",
                   ["terminal_attempt_failure"])
                : ("correction", "Correction", "A newer attempt ended unsuccessfully after the last landing.",
                   ["attempt_failure_after_landing", "historical_landing_retained"])
        } else if let latestLanding {
            progress = ("landed", "Landed", "Broker ancestry confirms the latest observed delivery landed.",
                        ["authoritative_broker_landing", "landing:\(Int(latestLanding.at))"])
        } else if succeeded > 0 {
            progress = ("delivered", "Delivered", "A child delivered output; integration is not yet proven.",
                        ["task_delivery_only"])
        } else if failed > 0 {
            progress = ("blocked", "Blocked", "The retained attempts ended without delivery.",
                        ["terminal_attempt_failure"])
        } else if canceled > 0 && canceled == attempts.count {
            progress = ("canceled", "Canceled", "All retained attempts were canceled.",
                        ["all_attempts_canceled"])
        } else if item.state == "planning" || item.state == "backlog" {
            progress = ("planning", "Planning", "The item has not begun an execution attempt.",
                        [item.state == "backlog" ? "item_backlog" : "item_planning"])
        } else {
            progress = ("unknown", "Unknown", "Retained facts do not establish a current progress state.",
                        ["insufficient_evidence"])
        }

        if item.type == "epic", item.state != "canceled" {
            let related = Set(item.links.filter { $0.kind == "related" }.map(\.targetId))
            let activeMembers = state.items.filter {
                $0.type != "epic" && ($0.parentId == item.id || related.contains($0.id))
                    && progressObject($0)["group"] as? String == "active"
            }
            if !activeMembers.isEmpty {
                isActive = true
                progress = ("execution", "Delivery lanes active",
                            "Linked implementation items have current activity; each lane retains its own evidence.",
                            ["active_delivery_lanes"])
            }
        }
        let hasHistory = !attempts.isEmpty || !item.evidence.isEmpty
        // Activity and outcome are independent. A retained unresolved result is not evidence
        // that somebody is still working, and must not inflate the Project's active count.
        let group: String
        if item.type == "coordination" { group = "coordination" }
        else if progress.state == "landed" || progress.state == "settled" { group = "completed" }
        else if progress.state == "canceled" { group = "canceled" }
        else if isActive && progress.state != "queued" { group = "active" }
        else if progress.state == "queued" || progress.state == "verified"
            || item.obligations.contains(where: { $0.blocking && !$0.resolved }) {
            group = "waiting"
        } else if hasHistory { group = "history" }
        else { group = "waiting" }
        return [
            "state": progress.state, "label": progress.label, "reason": progress.reason,
            "group": group,
            "basisCodes": progress.basis, "warningCodes": warningCodes,
            "lifecycleApplicable": item.type != "coordination", "active": isActive,
            "historical": !isActive && hasHistory,
            "attemptCounts": [
                "total": attempts.count, "active": activeAttempts.count, "queued": queued,
                "succeeded": succeeded, "failed": failed, "canceled": canceled,
                "unknown": unknownAttempts,
            ] as [String: Any],
            "evidenceCounts": [
                "total": item.evidence.count, "openFindings": openFindings.count,
                "blockingFindings": blockingFindings.count,
                "passedVerifications": passedVerifications.count,
                "failedVerifications": failedVerifications.count,
                "landings": landings.count,
                "verificationSummaries": verificationSummaries.count,
                "artifactAcceptances": artifactAcceptances.count,
            ] as [String: Any],
            "coordinationContext": [
                "activeSpans": activeSpanCount, "totalSpans": item.spans.count,
                "pendingHandoff": item.pendingHandoff != nil,
                "relatedItems": item.links.filter {
                    Self.itemLinkKinds.contains($0.kind)
                }.count,
            ] as [String: Any],
        ]
    }

    // MARK: - Snapshot

    private func isSettledNoChange(_ attempt: StoredLink) -> Bool {
        attempt.attemptState == "success" && attempt.landingDisposition == "nothing_to_land"
    }

    private struct MaterializationIndex {
        let childrenByParentID: [String: [StoredItem]]
        let progressByItemID: [String: [String: Any]]
        let itemsByID: [String: StoredItem]
        let positionByID: [String: Int]
    }

    /// Build the relationships needed by every detail once. The hook counts actual index work so
    /// the capacity proof can distinguish this linear pass from a hidden per-item global filter.
    private func makeMaterializationIndex() -> MaterializationIndex {
        var children: [String: [StoredItem]] = [:]
        var items: [String: StoredItem] = [:]
        var positions: [String: Int] = [:]
        for (position, item) in state.items.enumerated() {
            Self.materializationIndexOperationForTesting?()
            items[item.id] = item
            positions[item.id] = position
            if let parent = item.parentId { children[parent, default: []].append(item) }
        }
        var progress: [String: [String: Any]] = [:]
        for item in state.items { progress[item.id] = progressObject(item) }
        return MaterializationIndex(childrenByParentID: children, progressByItemID: progress,
                                    itemsByID: items, positionByID: positions)
    }

    private func buildMaterializedReadSeedLocked() -> MaterializedReadSeed {
        Self.materializationPauseForTesting?()
        let header = ReadHeader(revision: state.revision,
                                enabled: unavailable == nil && state.enabled,
                                updatedAt: state.updatedAt,
                                available: unavailable == nil,
                                narrativeConsent: state.narrativeConsent)
        if let unavailable {
            return MaterializedReadSeed(
                header: header, catalog: [], itemSummariesByProject: [:], itemDetailsByID: [:],
                automaticMutation: ["status": "unavailable", "persisted": false,
                                    "reason": unavailable.code],
                sourceIngestion: ["status": "unavailable", "droppedCount": 0,
                                  "reasons": [unavailable.code], "issues": []],
                error: ["code": unavailable.code, "message": unavailable.message])
        }

        let grouped = Dictionary(grouping: state.items, by: \.projectId)
        let index = makeMaterializationIndex()
        var summariesByProject: [String: [[String: Any]]] = [:]
        var detailsByID: [String: [String: Any]] = [:]
        var catalog: [[String: Any]] = []
        for project in state.projects {
            let items = (grouped[project.id] ?? []).sorted { lhs, rhs in
                lhs.updatedAt == rhs.updatedAt ? lhs.key < rhs.key : lhs.updatedAt > rhs.updatedAt
            }
            let summaries = items.map { itemObject($0, detail: false, index: index) }
            summariesByProject[project.id] = summaries
            for item in items {
                detailsByID[item.id] = itemObject(item, detail: true, index: index)
            }

            var open = 0, needsClarity = 0, landed = 0, coordination = 0
            var active = 0, waiting = 0, history = 0, settled = 0
            for item in items {
                if item.type == "coordination" { coordination += 1; continue }
                let progress = index.progressByItemID[item.id] ?? progressObject(item)
                let progressState = progress["state"] as? String ?? "unknown"
                switch progress["group"] as? String {
                case "active": active += 1
                case "waiting": waiting += 1
                case "history": history += 1
                default: break
                }
                if progressState == "landed" { landed += 1 }
                else if progressState == "settled" { settled += 1 }
                else if progressState != "canceled" { open += 1 }
                let hasBlockingObligation = item.obligations.contains { $0.blocking && !$0.resolved }
                let hasBlockingFinding = item.evidence.contains {
                    $0.kind == "finding" && $0.blocking && !$0.resolved
                }
                if hasBlockingObligation || hasBlockingFinding
                    || ["blocked", "failed", "unknown"].contains(progressState) {
                    needsClarity += 1
                }
            }
            let updatedAt = items.map(\.updatedAt).max() ?? state.updatedAt
            catalog.append([
                "id": project.id, "name": project.name, "itemCount": items.count,
                "activeItemCount": active, "updatedAt": updatedAt,
                "revision": state.revision, "observedAt": state.updatedAt,
                "isStartPoint": false,
                "summary": ["open": open, "needsClarity": needsClarity,
                            "landed": landed, "coordination": coordination,
                            "active": active, "waiting": waiting, "history": history,
                            "settled": settled],
                "summaryCoverage": ["status": "complete", "retainedCount": items.count,
                                    "omittedCount": 0, "reasons": [] as [String]],
            ])
        }
        catalog.sort {
            ($0["name"] as? String ?? "").localizedCaseInsensitiveCompare(
                $1["name"] as? String ?? "") == .orderedAscending
        }
        let coverage = state.ingestionCoverage ?? []
        let sourceIngestion: [String: Any] = [
            "status": coverage.isEmpty ? "complete" : "partial",
            "droppedCount": coverage.reduce(0) { $0 + $1.sourceDigests.count },
            "reasons": Array(Set(coverage.map(\.reason))).sorted(),
            "issues": coverage.map {
                ["kind": $0.kind, "reason": $0.reason,
                 "droppedCount": $0.sourceDigests.count, "saturated": $0.saturated,
                 "firstObservedAt": $0.firstObservedAt] as [String: Any]
            },
        ]
        let mutation = lastAutomaticFailure?.jsonObject
            ?? ["status": "available", "persisted": true, "reason": NSNull()] as [String: Any]
        var seed = MaterializedReadSeed(header: header, catalog: catalog,
                                    itemSummariesByProject: summariesByProject,
                                    itemDetailsByID: detailsByID,
                                    automaticMutation: mutation,
                                    sourceIngestion: sourceIngestion, error: nil)
        // Full durable associations, not truncated detail links or transcript scans. Build once
        // on the existing reconciliation lane; Session reads only resolve a bounded ID slice.
        for item in state.items.sorted(by: { $0.updatedAt == $1.updatedAt ? $0.id < $1.id : $0.updatedAt > $1.updatedAt }) {
            let sessions = Set(item.links.filter { $0.kind == "session" }.map {
                Self.canonicalConversationID($0.targetId)
            }).union(item.spans.map { Self.canonicalConversationID($0.sessionId) })
            for session in sessions { seed.sessionItemIDs[session, default: []].append(item.id) }
            for span in item.spans where span.endedAt == nil {
                let session = Self.canonicalConversationID(span.sessionId)
                seed.sessionActiveItemIDs[session, default: []].insert(item.id)
            }
        }
        return seed
    }

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
        // One selected detail is its own bounded scope. Sibling summaries belong to the Project
        // read and must not prevent a report-version read from fitting its response budget.
        var matching = selectedID == nil ? state.items : []
        if let project { matching = matching.filter { $0.projectId == project } }
        matching.sort { lhs, rhs in
            lhs.updatedAt == rhs.updatedAt ? lhs.key < rhs.key : lhs.updatedAt > rhs.updatedAt
        }
        let selected = selectedID.flatMap { id in
            state.items.first(where: { candidate in
                candidate.id == id && (project == nil || candidate.projectId == project)
            })
        }
        let itemCounts = Dictionary(grouping: state.items, by: \.projectId).mapValues(\.count)
        let projects = state.projects.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            .map { project in
                ["id": project.id, "name": project.name,
                 "itemCount": itemCounts[project.id] ?? 0] as [String: Any]
            }
        let selectedObject = selected.map { itemObject($0, detail: true) }
        var board: [String: Any] = [
            "schemaVersion": Self.schemaVersion, "revision": state.revision,
            "enabled": state.enabled, "mode": state.enabled ? "board" : "standard",
            "narrativeConsent": state.narrativeConsent as Any? ?? NSNull(),
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
        let baseBytes = Self.serializedSize(["board": board])
        var itemBytes = 0
        for candidate in matching.prefix(Self.maximumSnapshotItems) {
            let candidateObject = itemObject(candidate, detail: false)
            let candidateBytes = Self.serializedSize(["item": candidateObject])
            if candidateBytes == Int.max
                || baseBytes + itemBytes + candidateBytes > Self.maximumSnapshotBytes {
                byteLimited = true
                break
            }
            visible.append(candidateObject)
            itemBytes += candidateBytes
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

    private func reportObject(_ report: StoredCompletionReport, status: String,
                              includeBody: Bool) -> [String: Any] {
        var answer: [String: Any] = [
            "id": report.id, "version": report.version, "status": status,
            "authoredAt": report.authoredAt, "actor": report.actor,
            "authorship": report.authorship, "model": report.model ?? NSNull(),
            "scopeRevision": report.scopeRevision,
            "itemStateAtAuthorship": report.itemStateAtAuthorship,
            "sourceCount": report.sourceReferences.count,
        ]
        if let boundary = report.reportBoundary {
            var object: [String: Any] = ["kind": boundary.kind, "label": boundary.label]
            if let startedAt = boundary.startedAt { object["startedAt"] = startedAt }
            if let endedAt = boundary.endedAt { object["endedAt"] = endedAt }
            if let handoffId = boundary.handoffId { object["handoffId"] = handoffId }
            answer["reportBoundary"] = object
        } else {
            answer["reportBoundary"] = NSNull()
        }
        guard includeBody else { return answer }
        answer["objective"] = report.objective
        answer["deliveredOutcomes"] = report.deliveredOutcomes
        answer["verificationLanding"] = report.verificationLanding
        answer["remainingWork"] = report.remainingWork
        answer["lessons"] = report.lessons
        answer["sourceReferences"] = report.sourceReferences.map { source in
            var row: [String: Any] = [
                "kind": source.kind, "targetId": source.targetId, "label": source.label,
                "resolution": source.resolution, "authority": source.authority,
                "relationship": source.resolution + "_at_authorship",
                "resolvedAt": source.resolvedAt ?? report.authoredAt,
            ]
            row["url"] = source.url ?? NSNull()
            return row
        }
        return answer
    }

    private func itemObject(_ item: StoredItem, detail: Bool,
                            index: MaterializationIndex? = nil) -> [String: Any] {
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
        let documentReferences = item.documentReferences ?? []
        let orderedDocumentReferences = orderedDocumentReferences(documentReferences)
        let retainedDocumentReferences = Array(orderedDocumentReferences.prefix(childLimit))
        let retainedLinks = Array(item.links.prefix(childLimit))
        let retainedObligations = Array(item.obligations.prefix(childLimit))
        let retainedSpans = Array(item.spans.suffix(childLimit))
        let completedStatuses = Set(["passed", "not_applicable"])
        let exactSummary: [String: Any] = [
            "checklist": ["total": item.checklist.count,
                "completed": item.checklist.filter { completedStatuses.contains($0.status) }.count,
                "required": item.checklist.filter(\.required).count,
                "requiredCompleted": item.checklist.filter { $0.required && completedStatuses.contains($0.status) }.count,
                "coverage": "complete"],
            "milestones": ["total": item.milestones.count,
                "completed": item.milestones.filter { completedStatuses.contains($0.status) }.count,
                "coverage": "complete"],
        ]
        let accountingLinks = item.links.filter { $0.kind == "task" && $0.source == "broker" }
        let coverage = item.ingestionCoverage ?? []
        var answer: [String: Any] = [
            "id": item.id, "key": item.key, "projectId": item.projectId,
            "title": item.title, "type": item.type, "state": item.state,
            "summary": item.summary, "owner": item.owner,
            "typeDetails": item.typeDetails ?? [:],
            "parentId": item.parentId ?? NSNull(), "createdAt": item.createdAt,
            "updatedAt": item.updatedAt,
            "scopeRevision": item.scopeRevision ?? 0,
            "cardSummary": exactSummary,
            "checklist": retainedChecklist.map { row in
                var value: [String: Any] = ["id": row.id, "title": row.title,
                                                "status": row.status, "required": row.required]
                if let evidence = row.evidenceId { value["evidenceId"] = evidence }
                value["supplementRelationId"] = row.supplementRelationId ?? NSNull()
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
                if let attemptState = link.attemptState { value["attemptState"] = attemptState }
                if let startedAt = link.startedAt { value["startedAt"] = startedAt }
                if let finishedAt = link.finishedAt { value["finishedAt"] = finishedAt }
                if let sourceTaskId = link.sourceTaskId { value["sourceTaskId"] = sourceTaskId }
                if let graphId = link.graphId { value["graphId"] = graphId }
                if let nodeId = link.graphNodeId { value["graphNodeId"] = nodeId }
                return value
            },
            "accountingTaskLinks": accountingLinks.map { link in
                var value: [String: Any] = [
                    "targetId": link.targetId, "source": "broker",
                ]
                value["phase"] = link.phase ?? NSNull()
                return value
            },
            "obligations": retainedObligations.map { obligation in
                var row: [String: Any] = [
                    "id": obligation.id, "title": obligation.title,
                    "owner": obligation.owner, "blocking": obligation.blocking,
                    "resolved": obligation.resolved,
                    "actorKind": obligation.actorKind ?? "unknown",
                ]
                row["requiredAction"] = obligation.requiredAction ?? NSNull()
                row["blockingScope"] = obligation.blockingScope ?? NSNull()
                row["resolutionEvidence"] = obligation.resolutionEvidence ?? NSNull()
                row["supersededBy"] = obligation.supersededBy ?? NSNull()
                row["supplementRelationId"] = obligation.supplementRelationId ?? NSNull()
                return row
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
            "progress": index?.progressByItemID[item.id] ?? progressObject(item),
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
                "documentReferences": Self.projectionObject(total: documentReferences.count,
                                                             retained: retainedDocumentReferences.count),
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
        if detail {
            let supersededIDs = Set(documentReferences.compactMap(\.supersedesId))
            answer["documentReferences"] = retainedDocumentReferences.map {
                documentReferenceObject($0, all: documentReferences,
                                        supersededIDs: supersededIDs)
            }
            answer["remainingWork"] = remainingWorkObject(item, index: index)
        }
        let reports = item.completionReports ?? []
        answer["presentation"] = presentationObject(item)
        if item.type == "epic" {
            let related = Set(item.links.filter { $0.kind == "related" }.map(\.targetId))
            var members = index.map { $0.childrenByParentID[item.id] ?? [] } ?? state.items.filter {
                Self.materializationIndexOperationForTesting?()
                return $0.parentId == item.id
            }
            let existing = Set(members.map(\.id))
            let relatedItems: [StoredItem]
            if let index {
                relatedItems = related.compactMap { id in
                    Self.materializationIndexOperationForTesting?()
                    return index.itemsByID[id]
                }.sorted { (index.positionByID[$0.id] ?? 0) < (index.positionByID[$1.id] ?? 0) }
            } else {
                relatedItems = state.items.filter {
                    Self.materializationIndexOperationForTesting?()
                    return related.contains($0.id)
                }
            }
            members.append(contentsOf: relatedItems.filter {
                $0.type != "epic" && !existing.contains($0.id)
            })
            members.removeAll { $0.type == "epic" }
            answer["deliveryLaneCount"] = members.count
            answer["deliveryLanes"] = members.prefix(32).map { member -> [String: Any] in
                let required = member.checklist.filter(\.required)
                return ["id": member.id, "projectId": member.projectId,
                        "projectName": state.projects.first { $0.id == member.projectId }?.name ?? member.projectId,
                        "title": member.title, "owner": member.owner,
                        "presentation": presentationObject(member),
                        "progress": index?.progressByItemID[member.id] ?? progressObject(member),
                        "checklistTotal": required.count,
                        "checklistDone": required.filter { $0.status == "passed" || $0.status == "not_applicable" }.count]
            }
        }
        if let report = reports.last {
            let reportStatus = reportStatus(report, item: item, latest: true)
            answer["completionReport"] = reportObject(
                report, status: reportStatus, includeBody: detail)
            if detail {
                answer["completionReportHistory"] = reports.dropLast().map {
                    reportObject($0, status: "superseded", includeBody: false)
                }
            } else {
                answer["completionReportHistory"] = [
                    "retainedCount": reports.count,
                    "maximumVersions": Self.maximumReportVersions,
                ]
            }
        } else {
            answer["completionReport"] = ["status": "absent"] as [String: Any]
            if detail { answer["completionReportHistory"] = [] as [[String: Any]] }
            else {
                answer["completionReportHistory"] = [
                    "retainedCount": 0,
                    "maximumVersions": Self.maximumReportVersions,
                ]
            }
        }
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

    private func orderedDocumentReferences(
        _ references: [StoredDocumentReference]
    ) -> [StoredDocumentReference] {
        let superseded = Set(references.compactMap(\.supersedesId))
        let newer: (StoredDocumentReference, StoredDocumentReference) -> Bool = { lhs, rhs in
            if lhs.addedAt != rhs.addedAt { return lhs.addedAt > rhs.addedAt }
            if lhs.version != rhs.version { return lhs.version > rhs.version }
            return lhs.id < rhs.id
        }
        let heads = references.filter { !superseded.contains($0.id) }.sorted(by: newer)
        let history = references.filter { superseded.contains($0.id) }.sorted(by: newer)
        return heads + history
    }

    private func documentReferenceObject(_ reference: StoredDocumentReference,
                                         all references: [StoredDocumentReference],
                                         supersededIDs: Set<String>) -> [String: Any] {
        var row: [String: Any] = [
            "id": reference.id, "documentId": reference.documentId,
            "version": reference.version, "title": reference.title,
            "url": reference.url, "purpose": reference.purpose,
            "status": supersededIDs.contains(reference.id) ? "superseded" : "current",
            "authority": "narrative_only", "addedAt": reference.addedAt,
            "actor": reference.actor,
        ]
        row["supersedesId"] = reference.supersedesId ?? NSNull()
        if let successor = references.first(where: { $0.supersedesId == reference.id }) {
            row["supersededBy"] = successor.id
        }
        return row
    }

    private func remainingWorkRows(
        _ item: StoredItem, index: MaterializationIndex? = nil
    ) -> (work: [[String: Any]], decisions: [[String: Any]]) {
        let completedChecklist = Set(["passed", "not_applicable"])
        var rankedWork: [(rank: Int, order: Int, row: [String: Any])] = []
        var order = 0
        for row in item.checklist where !completedChecklist.contains(row.status) {
            rankedWork.append((row.required ? 1 : 3, order, [
                "id": row.id, "kind": "checklist", "title": row.title,
                "status": row.status, "disposition": row.required ? "required" : "optional",
                "owner": item.owner,
                "supplementRelationId": row.supplementRelationId ?? NSNull(),
            ]))
            order += 1
        }
        let children = index.map { $0.childrenByParentID[item.id] ?? [] }
            ?? state.items.filter {
                Self.materializationIndexOperationForTesting?()
                return $0.parentId == item.id
            }
        for child in children {
            if index != nil { Self.materializationIndexOperationForTesting?() }
            let progress = index?.progressByItemID[child.id] ?? progressObject(child)
            let group = progress["group"] as? String ?? "history"
            guard group != "completed" else { continue }
            let status = progress["state"] as? String ?? "unknown"
            let disposition = status == "canceled" || group == "canceled"
                ? "canceled" : status == "unknown" ? "unknown" : "required"
            let rank = ["blocked", "unknown"].contains(status) ? 1 : 2
            rankedWork.append((rank, order, [
                "id": child.id, "projectId": child.projectId, "kind": "child",
                "title": child.title, "owner": child.owner, "status": status,
                "disposition": disposition, "progress": progress,
            ]))
            order += 1
        }
        var userDecisions: [[String: Any]] = []
        for obligation in item.obligations where !obligation.resolved {
            let actorKind = obligation.actorKind ?? "unknown"
            var row: [String: Any] = [
                "id": obligation.id, "kind": "obligation", "title": obligation.title,
                "owner": obligation.owner, "status": "waiting", "actorKind": actorKind,
                "blocking": obligation.blocking,
                "disposition": actorKind == "unknown" ? "unknown" : "required",
            ]
            row["requiredAction"] = obligation.requiredAction ?? NSNull()
            row["blockingScope"] = obligation.blockingScope ?? NSNull()
            row["supplementRelationId"] = obligation.supplementRelationId ?? NSNull()
            if actorKind == "user" { userDecisions.append(row) }
            else {
                rankedWork.append((obligation.blocking ? 0 : 3, order, row))
                order += 1
            }
        }
        rankedWork.sort { lhs, rhs in
            lhs.rank == rhs.rank ? lhs.order < rhs.order : lhs.rank < rhs.rank
        }
        return (rankedWork.map(\.row), userDecisions)
    }

    /// A mandatory-first first-screen projection. Full retained rows stay discoverable through
    /// the bounded collection reader rather than turning an omitted count into a dead end.
    private func remainingWorkObject(_ item: StoredItem,
                                     index: MaterializationIndex? = nil) -> [String: Any] {
        let rows = remainingWorkRows(item, index: index)
        let work = rows.work, userDecisions = rows.decisions
        let workCount = work.count
        let decisionCount = userDecisions.count
        return [
            "work": Array(work.prefix(Self.maximumFirstScreenRemaining)),
            "userDecisions": Array(userDecisions.prefix(Self.maximumFirstScreenRemaining)),
            "workCount": workCount, "userDecisionCount": decisionCount,
            "workOmittedCount": max(0, workCount - Self.maximumFirstScreenRemaining),
            "userDecisionOmittedCount": max(0, decisionCount - Self.maximumFirstScreenRemaining),
        ]
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
                                      timestamp: Double,
                                      taskEventAt: Double?,
                                      eventSequence: inout Int) -> BrokerEvidenceResult {
        var result = BrokerEvidenceResult()
        // Derived broker facts share the chronology already accepted for their task link. A stale
        // replay cannot be rejected for the attempt and still rewrite its finding/summary clock.
        let sourceAt = taskEventAt
        if let review = task["review"] as? [String: Any],
           let axes = review["axes"] as? [[String: Any]] {
            for axis in axes {
                for finding in axis["findings"] as? [[String: Any]] ?? [] {
                    guard let findingID = Self.boundedText(finding["id"], maximum: 200),
                          let summary = Self.boundedText(finding["summary"], maximum: 1_000)
                    else { continue }
                    let source = "task:\(taskID):finding:\(findingID)"
                    if let existing = item.evidence.firstIndex(where: {
                        $0.kind == "finding" && $0.sourceId == source
                    }) {
                        if item.evidence[existing].eventAt != sourceAt
                            || item.evidence[existing].eventOrdinal == nil {
                            item.evidence[existing].eventAt = sourceAt
                            if item.evidence[existing].eventOrdinal == nil {
                                item.evidence[existing].eventOrdinal =
                                    Self.nextEventOrdinal(&eventSequence)
                            }
                            result.changed = true
                            result.acceptedCount += 1
                        }
                        continue
                    }
                    guard item.evidence.count < Self.maximumHistory else {
                        result.droppedCount += 1
                        result.reasons.insert("evidence_capacity_reached")
                        if recordIngestionDrop(kind: "evidence", reason: "evidence_capacity_reached",
                                               sourceID: source, item: &item,
                                               timestamp: timestamp) { result.changed = true }
                        continue
                    }
                    let severity = Self.boundedText(finding["severity"], maximum: 64) ?? "blocking"
                    let ordinal = Self.nextEventOrdinal(&eventSequence)
                    item.evidence.append(StoredEvidence(
                        id: Self.newID(), kind: "finding", summary: summary,
                        subject: graphID ?? taskID, status: "open", sourceId: source,
                        checklistId: nil, artifactId: nil, blocking: severity == "blocking",
                        resolved: false, at: sourceAt ?? 0, actor: "broker", source: "broker",
                        eventAt: sourceAt, eventOrdinal: ordinal))
                    let findingOrder = eventOrder(item.evidence[item.evidence.count - 1])
                    let latestLandingOrder = item.evidence.filter {
                        $0.kind == "landing" && $0.status == "passed"
                    }.map(eventOrder).max()
                    if severity == "blocking",
                       latestLandingOrder.map({ findingOrder > $0 }) ?? true {
                        invalidateCurrentEvidence(&item, reopen: true,
                                                  eventAt: sourceAt,
                                                  eventOrdinal: ordinal)
                        result.invalidatedCurrentEvidence = true
                    }
                    result.changed = true
                    result.acceptedCount += 1
                }
            }
        }
        if let verification = task["verification"] as? [String: Any], !verification.isEmpty {
            let sourceID = "task:\(taskID):verification-summary"
            if let existing = item.evidence.firstIndex(where: {
                $0.kind == "verification_summary" && $0.sourceId == sourceID
            }) {
                if item.evidence[existing].eventAt != sourceAt
                    || item.evidence[existing].eventOrdinal == nil {
                    item.evidence[existing].eventAt = sourceAt
                    if item.evidence[existing].eventOrdinal == nil {
                        item.evidence[existing].eventOrdinal =
                            Self.nextEventOrdinal(&eventSequence)
                    }
                    result.changed = true
                    result.acceptedCount += 1
                }
            } else {
                guard item.evidence.count < Self.maximumHistory else {
                    result.droppedCount += 1
                    result.reasons.insert("evidence_capacity_reached")
                    if recordIngestionDrop(kind: "evidence", reason: "evidence_capacity_reached",
                                           sourceID: sourceID, item: &item,
                                           timestamp: timestamp) { result.changed = true }
                    return result
                }
                let ordinal = Self.nextEventOrdinal(&eventSequence)
                item.evidence.append(StoredEvidence(
                    id: Self.newID(), kind: "verification_summary",
                    summary: "Execution attempt carries a verification summary; it is not exact-subject proof.",
                    subject: taskID, status: "reported", sourceId: sourceID,
                    checklistId: nil, artifactId: nil, blocking: false, resolved: true,
                    at: sourceAt ?? 0, actor: "broker", source: "broker",
                    eventAt: sourceAt, eventOrdinal: ordinal))
                appendHistory(item: &item, actor: "broker", kind: "verification_summary_linked",
                              summary: "Execution attempt \(taskID) carries a verification summary; it is not exact-subject proof.",
                              at: timestamp, sourceTaskId: taskID)
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
            let landedAt = Self.exactDouble(landing["landed_at"] ?? landing["landedAt"])
            let landingDisplayAt = landedAt ?? sourceAt ?? 0
            let landingSourceAt = landedAt ?? sourceAt
            let source = "task:\(taskID):landing:\(commit):\(targetCommit)"
            if let existingIndex = item.evidence.firstIndex(where: {
                $0.kind == "landing" && ($0.sourceId == source
                    || $0.sourceId.hasPrefix(source + ":scope:"))
            }) {
                let repairedOrdinal = item.evidence[existingIndex].eventOrdinal
                    ?? Self.nextEventOrdinal(&eventSequence)
                let repairedOrder = StoredEventOrder(at: landingSourceAt,
                                                     ordinal: repairedOrdinal,
                                                     stableID: "evidence:\(source)")
                let currentScopeRevision = item.scopeRevision ?? 0
                let matchesCurrentProof = item.currentVerificationEvidenceId != nil
                    && item.currentVerificationSubject == commit
                let repairedScopeRevision = matchesCurrentProof ? currentScopeRevision
                    : (scopeEventOrder(item).map { repairedOrder < $0 } == true
                        ? max(0, currentScopeRevision - 1) : currentScopeRevision)
                if item.evidence[existingIndex].eventAt != landingSourceAt
                    || item.evidence[existingIndex].eventOrdinal == nil
                    || (item.evidence[existingIndex].scopeRevision ?? 0)
                        != repairedScopeRevision {
                    item.evidence[existingIndex].eventAt = landingSourceAt
                    item.evidence[existingIndex].eventOrdinal = repairedOrdinal
                    item.evidence[existingIndex].scopeRevision = repairedScopeRevision
                    result.changed = true
                    result.acceptedCount += 1
                }
                let existing = item.evidence[existingIndex]
                let scopeRevision = existing.scopeRevision ?? 0
                if item.currentVerificationSubject == commit
                    && scopeRevision == (item.scopeRevision ?? 0)
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
                let ordinal = Self.nextEventOrdinal(&eventSequence)
                let landingOrder = StoredEventOrder(at: landingSourceAt, ordinal: ordinal,
                                                    stableID: "evidence:\(source)")
                let currentScopeRevision = item.scopeRevision ?? 0
                let matchesCurrentProof = item.currentVerificationEvidenceId != nil
                    && item.currentVerificationSubject == commit
                let scopeRevision = matchesCurrentProof ? currentScopeRevision
                    : (scopeEventOrder(item).map { landingOrder < $0 } == true
                        ? max(0, currentScopeRevision - 1) : currentScopeRevision)
                item.evidence.append(StoredEvidence(
                    id: Self.newID(), kind: "landing",
                    summary: "Broker-verified commit \(commit) is contained by target \(targetCommit).",
                    subject: commit, status: "passed", sourceId: source, checklistId: nil,
                    artifactId: nil, blocking: false, resolved: true, at: landingDisplayAt,
                    actor: "broker", source: "broker", scopeRevision: scopeRevision,
                    eventAt: landingSourceAt, eventOrdinal: ordinal))
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
            "set_ai_consent": ["enabled", "provider", "policy"],
            "create": ["projectId", "title", "type", "summary", "owner", "parentId",
                       "typeDetails"],
            "update": ["itemId", "title", "summary", "owner", "type", "typeDetails"],
            "transition": ["itemId", "state", "note"],
            "checklist": ["itemId", "title", "required", "checklistId", "status", "supplementRelationId"],
            "milestone": ["itemId", "title", "milestoneId", "status"],
            "artifact": ["itemId", "title", "url", "kind"],
            "document_reference": ["itemId", "title", "url", "purpose", "documentId",
                                   "version", "supersedesId"],
            "link": ["itemId", "kind", "targetId", "label"],
            "obligation": ["itemId", "title", "owner", "blocking", "actorKind",
                           "requiredAction", "blockingScope", "supplementRelationId"],
            "resolve_obligation": ["itemId", "obligationId", "note",
                                    "resolutionEvidence", "supersededBy"],
            "span": ["itemId", "sessionId", "phase"],
            "end_span": ["itemId", "sessionId", "startRequestId", "note"],
            "handoff": ["itemId", "owner", "note"],
            "accept_handoff": ["itemId", "note"],
            "accept_artifact": ["itemId", "artifactId", "note"],
            "record_evidence": ["itemId", "kind", "summary", "subject", "status",
                                "sourceId", "checklistId", "blocking", "resolved"],
            "record_report": ["itemId", "objective", "deliveredOutcomes",
                              "verificationLanding", "remainingWork", "lessons",
                              "authorship", "model", "sourceReferences", "reportBoundary"],
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

    private func parsedTypeDetails(_ raw: Any?, type: String,
                                   existing: [String: String]?) throws -> [String: String]? {
        guard let raw else { return existing }
        guard let object = raw as? [String: Any] else {
            throw BoardError(status: 400, code: "invalid_type_details",
                             message: "typeDetails must be an object")
        }
        let allowed: Set<String>
        switch type {
        case "bug": allowed = ["rootCause", "lessons"]
        case "coordination": allowed = ["outcomes", "difficulties", "improvements"]
        default: allowed = []
        }
        guard Set(object.keys).isSubset(of: allowed) else {
            throw BoardError(status: 400, code: "invalid_type_details",
                             message: "typeDetails contains fields not supported by this item type")
        }
        var result = existing ?? [:]
        for (key, rawValue) in object {
            guard let value = rawValue as? String,
                  value.lengthOfBytes(using: .utf8) <= Self.maximumUTF8 else {
                throw BoardError(status: 400, code: "invalid_type_details",
                                 message: "typeDetails values must be bounded strings")
            }
            let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.isEmpty { result.removeValue(forKey: key) }
            else { result[key] = trimmed }
        }
        return result.isEmpty ? nil : result
    }

    private func reportSources(_ raw: Any?, itemIndex: Int, resolvedAt: Double,
                               draft: StoredState) throws -> [StoredReportSource] {
        guard let rows = raw as? [[String: Any]], !rows.isEmpty,
              rows.count <= Self.maximumReportSources else {
            throw BoardError(status: 400, code: "invalid_report_sources",
                             message: "sourceReferences must contain 1 to \(Self.maximumReportSources) sources")
        }
        let item = draft.items[itemIndex]
        return try rows.map { row in
            guard Set(row.keys).isSubset(of: ["kind", "targetId", "label", "url"]) else {
                throw BoardError(status: 400, code: "invalid_report_source_fields",
                                 message: "a report source contains an unknown field")
            }
            let kind = try requiredChoice(row, "kind", choices: Self.reportSourceKinds)
            let targetID = try requiredText(row, "targetId", maximum: 300)
            let label = try requiredText(row, "label", maximum: 300)
            let requestedURL = try optionalText(row, "url", maximum: 2_000)
            if let requestedURL {
                guard let components = URLComponents(string: requestedURL),
                      let scheme = components.scheme?.lowercased(),
                      ["http", "https"].contains(scheme), components.host != nil else {
                    throw BoardError(status: 400, code: "invalid_report_source_url",
                                     message: "report source url must use http or https")
                }
            }
            let resolution: String
            var sourceURL: String? = requestedURL
            switch kind {
            case "task":
                resolution = item.links.contains {
                    $0.kind == "task" && $0.targetId == targetID
                } ? "same_item" : "unresolved"
            case "artifact":
                let artifact = item.artifacts.first {
                    $0.id == targetID || $0.url == targetID
                }
                resolution = artifact == nil ? "unresolved" : "same_item"
                if let artifact { sourceURL = artifact.url }
            case "evidence":
                resolution = item.evidence.contains {
                    $0.id == targetID || $0.sourceId == targetID
                } ? "same_item" : "unresolved"
            case "handoff":
                let ownsHandoff = item.pendingHandoff?.id == targetID || item.history.contains {
                    $0.id == targetID && $0.kind.contains("handoff")
                }
                resolution = ownsHandoff ? "same_item" : "unresolved"
            case "item":
                if targetID == item.id { resolution = "same_item" }
                else if draft.items.contains(where: {
                    $0.id == targetID && $0.projectId == item.projectId
                }) { resolution = "same_project" }
                else { resolution = "unresolved" }
            default:
                resolution = "unresolved"
            }
            if requestedURL != nil && !["artifact", "external"].contains(kind) {
                throw BoardError(status: 400, code: "invalid_report_source_url",
                                 message: "only artifact and external report sources accept a url")
            }
            return StoredReportSource(
                kind: kind, targetId: targetID, label: label, url: sourceURL,
                resolution: resolution, authority: "narrative_only", resolvedAt: resolvedAt)
        }
    }

    private func reportBoundary(_ raw: Any?, itemIndex: Int,
                                sources: [StoredReportSource],
                                draft: StoredState) throws -> StoredReportBoundary? {
        let item = draft.items[itemIndex]
        guard item.type == "coordination" else {
            guard raw == nil else {
                throw BoardError(status: 400, code: "report_boundary_not_applicable",
                                 message: "reportBoundary is only valid for Coordination")
            }
            return nil
        }
        guard let object = raw as? [String: Any],
              let kind = Self.boundedText(object["kind"], maximum: 64),
              let label = Self.boundedText(object["label"], maximum: 300) else {
            throw BoardError(status: 400, code: "coordination_report_boundary_required",
                             message: "Coordination reports require a named time interval or same-item handoff")
        }
        switch kind {
        case "time_interval":
            guard Set(object.keys) == Set(["kind", "label", "startedAt", "endedAt"]),
                  let startedAt = Self.exactDouble(object["startedAt"]),
                  let endedAt = Self.exactDouble(object["endedAt"]),
                  endedAt > startedAt else {
                throw BoardError(status: 400, code: "invalid_report_time_interval",
                                 message: "a report time interval needs finite increasing boundaries")
            }
            return StoredReportBoundary(kind: kind, label: label, startedAt: startedAt,
                                        endedAt: endedAt, handoffId: nil)
        case "handoff":
            guard Set(object.keys) == Set(["kind", "label", "handoffId"]),
                  let handoffID = Self.boundedText(object["handoffId"], maximum: 200),
                  sources.contains(where: {
                      $0.kind == "handoff" && $0.targetId == handoffID
                          && $0.resolution == "same_item"
                  }) else {
                throw BoardError(status: 400, code: "invalid_report_handoff_boundary",
                                 message: "a report handoff boundary must name a same-item handoff source")
            }
            return StoredReportBoundary(kind: kind, label: label, startedAt: nil,
                                        endedAt: nil, handoffId: handoffID)
        default:
            throw BoardError(status: 400, code: "invalid_report_boundary_kind",
                             message: "reportBoundary kind must be time_interval or handoff")
        }
    }

    private func reportEligible(_ item: StoredItem) -> Bool {
        item.state == "closed" || progressObject(item)["state"] as? String == "landed"
    }

    private func reportStatus(_ report: StoredCompletionReport, item: StoredItem,
                              latest: Bool) -> String {
        guard latest else { return "superseded" }
        let eligible = item.type == "coordination"
            ? report.reportBoundary != nil : reportEligible(item)
        return eligible && report.scopeRevision == (item.scopeRevision ?? 0)
            ? "current" : "historical_needs_update"
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

    private func optionalChoice(_ body: [String: Any], _ key: String,
                                choices: Set<String>) throws -> String? {
        guard body[key] != nil else { return nil }
        return try requiredChoice(body, key, choices: choices)
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

    /// Conversation-authored facts may reopen delivered work automatically. Cancellation is the
    /// separate terminal decision and still requires an explicit lifecycle reconciliation first.
    private func requireRecordable(_ index: Int, draft: StoredState) throws {
        guard draft.items[index].state != "canceled" else {
            throw BoardError(status: 409, code: "item_locked",
                             message: "reopen canceled work before recording new scope")
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

    private func invalidateCurrentEvidence(_ item: inout StoredItem, reopen: Bool,
                                           eventAt: Double?, eventOrdinal: Int) {
        item.scopeRevision = (item.scopeRevision ?? 0) + 1
        item.scopeEventAt = eventAt
        item.scopeEventOrdinal = eventOrdinal
        item.currentVerificationEvidenceId = nil
        item.currentVerificationSubject = nil
        item.currentLandingEvidenceId = nil
        item.currentArtifactAcceptanceId = nil
        for index in item.checklist.indices { item.checklist[index].evidenceId = nil }
        if reopen && ["verified", "integrated", "closed"].contains(item.state) {
            item.state = "execution"
        }
    }

    private func reopenForInvalidatedEvidence(_ index: Int, timestamp: Double,
                                              draft: inout StoredState) {
        let ordinal = nextEventOrdinal(&draft)
        invalidateCurrentEvidence(&draft.items[index], reopen: true,
                                  eventAt: timestamp, eventOrdinal: ordinal)
        reopenAncestors(of: draft.items[index].id, timestamp: timestamp, draft: &draft)
    }

    private func reopenAncestors(of itemID: String, timestamp: Double,
                                 draft: inout StoredState) {
        var parentID = draft.items.first(where: { $0.id == itemID })?.parentId
        while let current = parentID,
              let index = draft.items.firstIndex(where: { $0.id == current }) {
            if ["verified", "integrated", "closed"].contains(draft.items[index].state) {
                let ordinal = nextEventOrdinal(&draft)
                invalidateCurrentEvidence(&draft.items[index], reopen: true,
                                          eventAt: timestamp, eventOrdinal: ordinal)
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

    private static func taskKey(projectID: String, taskID: String) -> String {
        "project-task-v1:\(stringDigest("\(projectID)\u{0}\(taskID)") ?? "")"
    }

    private static func brokerOwner(_ task: [String: Any]) -> String? {
        let root = task["root"] as? [String: Any]
        return boundedText(root?["label"], maximum: 300)
            ?? boundedText(root?["sessionId"] ?? root?["session_id"], maximum: 300)
            ?? boundedText(task["root_label"] ?? task["rootLabel"], maximum: 300)
            ?? boundedText(task["assistant"], maximum: 300)
    }

    private static func canonicalBrokerStart(_ task: [String: Any]) -> Double? {
        let keys = ["started_at", "startedAt", "briefed_at", "briefedAt",
                    "spawned_at", "spawnedAt", "created_at", "createdAt"]
        for key in keys {
            if let value = exactDouble(task[key]) { return value }
        }
        return nil
    }

    private static func nextEventOrdinal(_ sequence: inout Int) -> Int {
        sequence += 1
        return sequence
    }

    private func nextEventOrdinal(_ draft: inout StoredState) -> Int {
        var sequence = draft.eventSequence ?? maximumEventOrdinal(in: draft)
        let ordinal = Self.nextEventOrdinal(&sequence)
        draft.eventSequence = sequence
        return ordinal
    }

    private func maximumEventOrdinal(in draft: StoredState) -> Int {
        draft.items.reduce(0) { maximum, item in
            let links = item.links.compactMap(\.eventOrdinal).max() ?? 0
            let evidence = item.evidence.compactMap(\.eventOrdinal).max() ?? 0
            return max(max(maximum, links), max(evidence, item.scopeEventOrdinal ?? 0))
        }
    }

    private func eventOrder(_ link: StoredLink) -> StoredEventOrder {
        StoredEventOrder(at: link.eventAt,
                         ordinal: link.eventOrdinal ?? 0, stableID: "link:\(link.targetId)")
    }

    private func eventOrder(_ evidence: StoredEvidence) -> StoredEventOrder {
        let trustedAt = evidence.source == "root_attestation" ? evidence.at : nil
        return StoredEventOrder(at: evidence.eventAt ?? trustedAt,
                         ordinal: evidence.eventOrdinal ?? 0,
                         stableID: "evidence:\(evidence.sourceId)")
    }

    private func scopeEventOrder(_ item: StoredItem) -> StoredEventOrder? {
        guard item.scopeEventAt != nil || item.scopeEventOrdinal != nil else { return nil }
        return StoredEventOrder(at: item.scopeEventAt,
                                ordinal: item.scopeEventOrdinal ?? 0,
                                stableID: "scope:\(item.id)")
    }

    private func attemptOccurredAfter(_ attempt: StoredLink,
                                      evidence: StoredEvidence) -> Bool {
        eventOrder(attempt) > eventOrder(evidence)
            || (attempt.eventAt == nil
                && (attempt.eventOrdinal ?? 0) > (evidence.eventOrdinal ?? 0))
    }

    /// Broker tasks have immutable terminal outcomes. Active snapshots may advance, and a
    /// terminal snapshot may close an active one, but a replay cannot rewrite one terminal fact
    /// into another merely because it was observed later.
    private func shouldReplaceAttempt(_ old: StoredLink, state: String?, at: Double?) -> Bool {
        guard old.attemptState != state || old.eventAt != at else { return false }
        let oldTerminal = Self.terminalAttemptStates.contains(old.attemptState ?? "")
        let newTerminal = Self.terminalAttemptStates.contains(state ?? "")
        if oldTerminal {
            guard old.attemptState == state else { return false }
            guard let at else { return old.eventAt == nil }
            return old.eventAt.map { at > $0 } ?? true
        }
        if newTerminal { return true }
        let rank = ["queued": 0, "spawning": 1, "briefed": 2]
        let oldRank = rank[old.attemptState ?? ""] ?? -1
        let newRank = rank[state ?? ""] ?? -1
        if newRank != oldRank { return newRank > oldRank }
        guard let at else { return old.eventAt == nil }
        return old.eventAt.map { at > $0 } ?? true
    }

    private func appendHistory(item: inout StoredItem, actor: String, kind: String,
                               summary: String, at: Double,
                               sourceTaskId: String? = nil) {
        if item.history.count >= Self.maximumHistory {
            item.history.removeFirst(item.history.count - Self.maximumHistory + 1)
            item.historyDroppedCount = (item.historyDroppedCount ?? 0) + 1
        }
        item.history.append(StoredHistory(id: Self.newID(), at: at, actor: actor,
                                          kind: kind, summary: summary,
                                          sourceTaskId: sourceTaskId))
    }

    /// Move every fact whose provenance names one broker task. This happens in the same draft as
    /// the explicit graph binding, so no persisted revision can expose duplicate accounting or a
    /// half-moved execution history.
    private func reattributeTaskFacts(taskID: String, taskOwner: String?, sessionID: String?,
                                      worktreeTarget: String?, from: Int, to: Int,
                                      draft: inout StoredState) -> Bool {
        guard from != to else { return false }
        let evidencePrefix = "task:\(taskID):"
        func owns(_ link: StoredLink) -> Bool {
            if link.sourceTaskId == taskID { return true }
            if link.kind == "task" && link.targetId == taskID && link.source == "broker" {
                return true
            }
            if link.source == "broker", link.sourceTaskId == nil,
               (link.kind == "session" && link.targetId == sessionID
                || link.kind == "worktree" && link.targetId == worktreeTarget) {
                return true
            }
            return false
        }
        let movedLinks = draft.items[from].links.filter(owns)
        let movedSpans = draft.items[from].spans.filter {
            $0.source == "broker" && $0.sourceId == taskID
        }
        let movedEvidence = draft.items[from].evidence.filter {
            $0.source == "broker" && $0.sourceId.hasPrefix(evidencePrefix)
        }
        func owns(_ history: StoredHistory) -> Bool {
            history.sourceTaskId == taskID
                || (history.actor == "broker" && history.summary.contains(taskID))
        }
        let movedHistory = draft.items[from].history.filter(owns)
        let ownsOwner = draft.items[from].ownerSourceTaskId == taskID
            || (draft.items[from].ownerSourceTaskId == nil
                && draft.items[from].inferredSourceKey != nil
                && movedLinks.contains { $0.kind == "task" && $0.targetId == taskID }
                && taskOwner != nil && draft.items[from].owner == taskOwner)
        guard !movedLinks.isEmpty || !movedSpans.isEmpty || !movedEvidence.isEmpty
                || !movedHistory.isEmpty || ownsOwner else { return false }

        let evidenceIDs = Set(movedEvidence.map(\.id))
        let oldVerification = draft.items[from].currentVerificationEvidenceId
        let oldLanding = draft.items[from].currentLandingEvidenceId
        let oldArtifact = draft.items[from].currentArtifactAcceptanceId
        let oldSubject = draft.items[from].currentVerificationSubject
        draft.items[from].links.removeAll(where: owns)
        draft.items[from].spans.removeAll {
            $0.source == "broker" && $0.sourceId == taskID
        }
        draft.items[from].evidence.removeAll {
            $0.source == "broker" && $0.sourceId.hasPrefix(evidencePrefix)
        }
        draft.items[from].history.removeAll(where: owns)
        if ownsOwner {
            if draft.items[to].owner.isEmpty {
                draft.items[to].owner = draft.items[from].owner
                draft.items[to].ownerSourceTaskId = taskID
            }
            draft.items[from].owner = ""
            draft.items[from].ownerSourceTaskId = nil
        }
        if oldVerification.map(evidenceIDs.contains) == true {
            draft.items[from].currentVerificationEvidenceId = nil
            draft.items[from].currentVerificationSubject = nil
        }
        if oldLanding.map(evidenceIDs.contains) == true {
            draft.items[from].currentLandingEvidenceId = nil
        }
        if oldArtifact.map(evidenceIDs.contains) == true {
            draft.items[from].currentArtifactAcceptanceId = nil
        }

        for var link in movedLinks where !draft.items[to].links.contains(where: {
            $0.kind == link.kind && $0.targetId == link.targetId && $0.source == link.source
                && ($0.sourceTaskId ?? taskID) == (link.sourceTaskId ?? taskID)
        }) {
            link.sourceTaskId = taskID
            draft.items[to].links.append(link)
        }
        for span in movedSpans where !draft.items[to].spans.contains(where: {
            $0.source == span.source && $0.sourceId == span.sourceId
        }) {
            draft.items[to].spans.append(span)
        }
        for evidence in movedEvidence where !draft.items[to].evidence.contains(where: {
            $0.kind == evidence.kind && $0.sourceId == evidence.sourceId
        }) {
            draft.items[to].evidence.append(evidence)
        }
        for history in movedHistory where !draft.items[to].history.contains(where: {
            $0.id == history.id
        }) {
            draft.items[to].history.append(history)
        }
        if draft.items[to].currentVerificationEvidenceId == nil,
           oldVerification.map(evidenceIDs.contains) == true {
            draft.items[to].currentVerificationEvidenceId = oldVerification
            draft.items[to].currentVerificationSubject = oldSubject
        }
        if draft.items[to].currentLandingEvidenceId == nil,
           let oldLanding, evidenceIDs.contains(oldLanding),
           let landing = draft.items[to].evidence.first(where: { $0.id == oldLanding }),
           draft.items[to].currentVerificationEvidenceId != nil,
           draft.items[to].currentVerificationSubject == landing.subject,
           (landing.scopeRevision ?? 0) == (draft.items[to].scopeRevision ?? 0) {
            draft.items[to].currentLandingEvidenceId = oldLanding
        }
        if draft.items[to].currentArtifactAcceptanceId == nil,
           oldArtifact.map(evidenceIDs.contains) == true {
            draft.items[to].currentArtifactAcceptanceId = oldArtifact
        }
        return true
    }

    private func canRetireInferredItem(_ item: StoredItem) -> Bool {
        guard item.inferredSourceKey != nil, item.links.isEmpty, item.spans.isEmpty,
              item.evidence.isEmpty, item.checklist.isEmpty, item.milestones.isEmpty,
              item.artifacts.isEmpty, item.obligations.isEmpty, item.pendingHandoff == nil,
              item.typeDetails == nil, (item.completionReports ?? []).isEmpty,
              (item.documentReferences ?? []).isEmpty else { return false }
        return !item.history.contains { $0.actor != "board" && $0.actor != "broker" }
    }

    private func retireInferredItems(_ ids: Set<String>, draft: inout StoredState) -> Int {
        let referenced = Set(draft.items.flatMap { item -> [String] in
            var targets = item.links.filter {
                Self.itemLinkKinds.contains($0.kind) && ids.contains($0.targetId)
            }.map(\.targetId)
            if let parentID = item.parentId, ids.contains(parentID) { targets.append(parentID) }
            return targets
        })
        let retired = Set(draft.items.filter {
            ids.contains($0.id) && !referenced.contains($0.id) && canRetireInferredItem($0)
        }.map(\.id))
        guard !retired.isEmpty else { return 0 }
        draft.items.removeAll { retired.contains($0.id) }
        draft.graphItems = draft.graphItems.filter { !retired.contains($0.value) }
        draft.graphNodeItems = draft.graphNodeItems.map { $0.filter { !retired.contains($0.value) } }
        draft.explicitGraphItems = draft.explicitGraphItems.map {
            $0.filter { !retired.contains($0.value) }
        }
        draft.taskItems = draft.taskItems.map { $0.filter { !retired.contains($0.value) } }
        return retired.count
    }

    private static func validSupplementRelationID(_ value: String) -> Bool {
        value.utf8.count == 68 && value.hasPrefix("wfs-")
            && value.dropFirst(4).allSatisfy { "0123456789abcdef".contains($0) }
    }

    private static func declaredSpanID(actor: String, requestID: String) -> String {
        "span-" + String(SHA256.hash(data: Data((actor + "\u{0}" + requestID).utf8))
            .map { String(format: "%02x", $0) }.joined())
    }

    private func closeActiveSpans(sessionID: String, at: Double,
                                  items: inout [StoredItem]) {
        for itemIndex in items.indices {
            for spanIndex in items[itemIndex].spans.indices
            where items[itemIndex].spans[spanIndex].sessionId == sessionID
                && items[itemIndex].spans[spanIndex].endedAt == nil
                && items[itemIndex].spans[spanIndex].startedAt <= at {
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

    private static func validTypeDetails(_ details: [String: String]?, for type: String) -> Bool {
        let keys: Set<String>
        switch type {
        case "bug": keys = ["rootCause", "lessons"]
        case "coordination": keys = ["outcomes", "difficulties", "improvements"]
        default: keys = []
        }
        guard let details else { return true }
        return Set(details.keys).isSubset(of: keys) && details.values.allSatisfy {
            !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                && $0.lengthOfBytes(using: .utf8) <= maximumUTF8
        }
    }

    private static func validCompletionReports(_ reports: [StoredCompletionReport]?) -> Bool {
        let reports = reports ?? []
        guard reports.count <= maximumReportVersions else { return false }
        return reports.enumerated().allSatisfy { offset, report in
            let texts = [report.objective, report.deliveredOutcomes,
                         report.verificationLanding, report.remainingWork, report.lessons]
            return report.version == offset + 1
                && report.authoredAt.isFinite && report.authoredAt >= 0
                && report.scopeRevision >= 0
                && boundedText(report.id, maximum: 200) != nil
                && boundedText(report.actor, maximum: 300) != nil
                && ["assistant", "human"].contains(report.authorship)
                && (report.authorship == "assistant" || report.model == nil)
                && (report.model.map { boundedText($0, maximum: 200) != nil } ?? true)
                && boundedText(report.itemStateAtAuthorship, maximum: 64) != nil
                && validReportBoundary(report.reportBoundary)
                && texts.allSatisfy { boundedText($0, maximum: maximumUTF8) != nil }
                && !report.sourceReferences.isEmpty
                && report.sourceReferences.count <= maximumReportSources
                && report.sourceReferences.allSatisfy { source in
                    let validURL: Bool = {
                        guard let value = source.url else { return true }
                        guard let components = URLComponents(string: value),
                              let scheme = components.scheme?.lowercased() else { return false }
                        return ["http", "https"].contains(scheme) && components.host != nil
                    }()
                    return reportSourceKinds.contains(source.kind)
                        && boundedText(source.targetId, maximum: 300) != nil
                        && boundedText(source.label, maximum: 300) != nil
                        && ["same_item", "same_project", "unresolved"].contains(source.resolution)
                        && source.authority == "narrative_only"
                        && (source.resolvedAt.map { $0.isFinite && $0 >= 0 } ?? true)
                        && validURL
                }
        }
    }

    private static func validDocumentReferences(_ references: [StoredDocumentReference]?) -> Bool {
        let references = references ?? []
        guard references.count <= maximumChildren,
              Set(references.map(\.id)).count == references.count else { return false }
        let byID = Dictionary(uniqueKeysWithValues: references.map { ($0.id, $0) })
        let predecessorIDs = references.compactMap(\.supersedesId)
        guard Set(predecessorIDs).count == predecessorIDs.count else { return false }
        for reference in references {
            guard boundedText(reference.id, maximum: 200) != nil,
                  boundedText(reference.documentId, maximum: 200) != nil,
                  boundedText(reference.title, maximum: 300) != nil,
                  documentPurposes.contains(reference.purpose), reference.version > 0,
                  isCanonicalCloudDocumentURL(reference.url),
                  reference.addedAt.isFinite, reference.addedAt >= 0,
                  boundedText(reference.actor, maximum: 300) != nil else { return false }
            if let predecessorID = reference.supersedesId {
                guard let predecessor = byID[predecessorID],
                      predecessor.documentId == reference.documentId,
                      predecessor.purpose == reference.purpose,
                      predecessor.version < reference.version else { return false }
            }
        }
        return Dictionary(grouping: references, by: \.documentId).values.allSatisfy { versions in
            versions.filter { $0.supersedesId == nil }.count == 1
                && versions.map(\.version).min() == 1
                && Set(versions.map(\.version)).count == versions.count
        }
    }

    private static func validReportBoundary(_ boundary: StoredReportBoundary?) -> Bool {
        guard let boundary else { return true }
        guard boundedText(boundary.label, maximum: 300) != nil else { return false }
        if boundary.kind == "time_interval" {
            guard let startedAt = boundary.startedAt, let endedAt = boundary.endedAt else {
                return false
            }
            return startedAt.isFinite && startedAt >= 0 && endedAt.isFinite
                && endedAt > startedAt && boundary.handoffId == nil
        }
        return boundary.kind == "handoff" && boundary.startedAt == nil
            && boundary.endedAt == nil
            && boundary.handoffId.flatMap { boundedText($0, maximum: 200) } != nil
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
              (state.eventSequence ?? 0) >= 0,
              (state.presentationEpoch ?? 0) >= 0,
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
                  Self.validDocumentReferences(item.documentReferences),
                  item.links.count <= maximumChildren,
                  item.obligations.count <= maximumChildren,
                  item.spans.count <= maximumChildren,
                  item.evidence.count <= maximumHistory,
                  item.history.count <= maximumHistory,
                  Self.validTypeDetails(item.typeDetails, for: item.type),
                  Self.validCompletionReports(item.completionReports),
                  (item.presentations ?? []).count <= 3,
                  (item.presentations ?? []).allSatisfy({
                      validPresentationLocale($0.locale) && $0.sourceFingerprint.count == 64
                          && boundedText($0.title, maximum: 320) != nil && $0.title.count <= 80
                          && boundedText($0.summary, maximum: 1200) != nil && $0.summary.count <= 300
                          && $0.outcome.count <= 300 && $0.outcome.utf8.count <= 1200
                          && $0.nextStep.count <= 200 && $0.nextStep.utf8.count <= 800
                          && boundedText($0.model, maximum: 200) != nil
                          && $0.authoredAt.isFinite && $0.authoredAt >= 0
                  }),
                  (item.scopeRevision ?? 0) >= 0,
                  (item.scopeEventAt.map { $0.isFinite && $0 >= 0 } ?? true),
                  (item.scopeEventOrdinal.map { $0 >= 0 } ?? true),
                  (item.ingestionCoverage ?? []).count <= maximumChildren,
                  (item.ingestionCoverage ?? []).allSatisfy({
                      $0.sourceDigests.count <= maximumHistory
                          && $0.firstObservedAt.isFinite && $0.firstObservedAt >= 0
                  }),
                  item.spans.allSatisfy({ span in
                      span.startedAt.isFinite && span.startedAt >= 0
                          && (span.endedAt.map { $0.isFinite && $0 >= span.startedAt } ?? true)
                  }),
                  item.links.allSatisfy({ link in
                      (link.startedAt.map { $0.isFinite && $0 >= 0 } ?? true)
                          && (link.finishedAt.map { $0.isFinite && $0 >= 0 } ?? true)
                          && (link.statusObservedAt.map { $0.isFinite && $0 >= 0 } ?? true)
                          && (link.eventAt.map { $0.isFinite && $0 >= 0 } ?? true)
                          && (link.eventOrdinal.map { $0 >= 0 } ?? true)
                          && (link.landingDisposition.map { ["pending", "landed", "nothing_to_land", "abandoned"].contains($0) } ?? true)
                          && (link.landingDispositionAt.map { $0.isFinite && $0 >= 0 } ?? true)
                  }),
                  item.obligations.allSatisfy({ obligation in
                      (obligation.actorKind.map(obligationActorKinds.contains) ?? true)
                          && (obligation.supplementRelationId.map(validSupplementRelationID) ?? true)
                          && (obligation.requiredAction.map {
                              boundedText($0, maximum: 1_000) != nil
                          } ?? true)
                          && (obligation.blockingScope.map {
                              boundedText($0, maximum: 300) != nil
                          } ?? true)
                          && (obligation.resolutionEvidence.map {
                              boundedText($0, maximum: 1_000) != nil
                          } ?? true)
                          && (obligation.supersededBy.map { successor in
                              successor != obligation.id
                                  && item.obligations.contains { $0.id == successor }
                          } ?? true)
                  }),
                  item.evidence.allSatisfy({ evidence in
                      evidence.at.isFinite && evidence.at >= 0
                          && (evidence.eventAt.map { $0.isFinite && $0 >= 0 } ?? true)
                          && (evidence.eventOrdinal.map { $0 >= 0 } ?? true)
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
                guard item.obligations.allSatisfy(\.resolved),
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
                      (row.evidenceId.map(evidenceIDs.contains) ?? true)
                          && (row.supplementRelationId.map(validSupplementRelationID) ?? true)
                  }) else {
                throw BoardError(status: 503, code: "board_store_corrupt",
                                 message: "the Project Board current evidence index is invalid")
            }
        }
        guard state.graphItems.values.allSatisfy(itemIDs.contains),
              (state.explicitGraphItems ?? [:]).allSatisfy({ key, itemID in
                  itemIDs.contains(itemID) && state.graphItems[key] == itemID
              }), (state.taskItems ?? [:]).values.allSatisfy(itemIDs.contains),
              (state.graphNodeItems ?? [:]).count <= maximumItems * maximumChildren,
              (state.graphNodeItems ?? [:]).allSatisfy({ key, value in
                  key.count == 64 && itemIDs.contains(value)
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
            Self.persistencePauseForTesting?()
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            try data.write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: url.path)
            let handle = try FileHandle(forWritingTo: url)
            try handle.synchronize()
            try handle.close()
            materializedSeedStale = true
            publicationLock.lock()
            publishedHeader = currentHeaderLocked(value)
            publicationLock.unlock()
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
