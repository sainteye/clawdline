import CryptoKit
import Foundation

/// The adapter from existing broker/accounting facts to durable work items. The
/// board never reads transcripts or adds a task's cumulative usage a second time.
enum ProjectBoardIntegration {
    private static let queue = DispatchQueue(label: "clawdline.board.sources", qos: .utility)
    private static let coverageLock = NSLock()
    private static var ingestionCoverage = IngestionCoverage()
    private static let reads = ProjectBoardReadCache()
    private static var source = MaterializedSources()
    private static var prepared = false
    private static var catalogTimer: DispatchSourceTimer?
    private static var catalogSignal = CatalogSignal()
    private static var configObserver: NSObjectProtocol?
    private static var storeForTesting: ProjectBoardStore?
    private static var refreshForTesting: ((ProjectBoardStore, ProjectBoardReadCache.DirtyReasons)
        -> Result<ProjectBoardReadCache.Model, ProjectBoardReadCache.Failure>)?
    static let sourceLimit = 100_000

    /// An incomplete inventory cannot remove Projects, and ordinary polling of an unchanged
    /// inventory cannot initiate expensive source work. Accessed only on the source queue.
    struct CatalogSignal {
        private var paths: Set<String>?

        mutating func invalidate() { paths = nil }

        mutating func observe(enabled: Bool, complete: Bool, paths next: Set<String>) -> Bool {
            guard enabled, complete, paths != next else { return false }
            paths = next
            return true
        }
    }

    private struct MaterializedSources {
        var presentations: [String: ProjectPresentation] = [:]
        var projectOrder: [String: Int] = [:]
        var rowsByTask: [String: [UsageLedger.Row]] = [:]
        var usageRows = 0
        var usageTruncated = false
        var usageThrough: Date?
        var usageRevision: String?
    }

    struct ProjectPresentation {
        let label: String
        let displayPath: String
        let icon: [String: Any]?

        var jsonObject: [String: Any] {
            var value: [String: Any] = ["label": label, "displayPath": displayPath]
            value["icon"] = icon ?? NSNull()
            return value
        }
    }

    struct IngestionCoverage {
        private struct Issue {
            let projectID: String
            let reason: String
        }

        private var issuesBySource: [String: Issue] = [:]
        private var unscopedReasonsByProject: [String: Set<String>] = [:]
        private var rootIssues: [String: Issue] = [:]
        private(set) var retiredRootGapCount = 0
        private(set) var evictedRootGapCount = 0
        static let rootIssueLimit = 128

        /// Current projection coverage, not an eternal failure ledger. Missing managed identity
        /// is not evidence that any Project lost a write. Retired/evicted gaps remain explicitly
        /// counted as unproved history; consuming a broker receipt is not a successful repair.
        mutating func recordRootLanding(_ outcome: ProjectBoardWorkflow.Outcome, sourceID: String) {
            guard let project = outcome.projectID else { return }
            if outcome.status < 300 || outcome.code == "board_disabled" {
                rootIssues.removeValue(forKey: sourceID)
            } else {
                if rootIssues[sourceID] == nil, rootIssues.count >= Self.rootIssueLimit,
                   let oldest = rootIssues.keys.sorted().first {
                    rootIssues.removeValue(forKey: oldest)
                    evictedRootGapCount += 1
                }
                rootIssues[sourceID] = Issue(projectID: project, reason: outcome.code)
            }
        }

        mutating func retainRootLandings(_ current: Set<String>) {
            let retired = rootIssues.keys.filter { !current.contains($0) }
            for key in retired { rootIssues.removeValue(forKey: key) }
            retiredRootGapCount += retired.count
        }

        var issueCount: Int {
            issuesBySource.count + rootIssues.count
                + unscopedReasonsByProject.values.reduce(0) { $0 + $1.count }
        }

        mutating func record(_ outcome: ProjectBoardStore.AutomaticMutationOutcome,
                             projectID: String?, sourceID: String? = nil) {
            // Standard mode deliberately does not ingest work. That is not a lost write.
            guard outcome.reason != "board_disabled", outcome.reason != "irrelevant" else { return }
            let project = projectID ?? "*"
            let isIssue = outcome.status == .partial || outcome.status == .refused
                || outcome.status == .unavailable || !outcome.persisted || outcome.droppedCount > 0
            if let sourceID {
                let key = project + "\u{0}" + sourceID
                if isIssue {
                    issuesBySource[key] = Issue(
                        projectID: project,
                        reason: outcome.reason ?? "source_ingestion_incomplete")
                } else {
                    // Only a later complete observation of this exact source proves its transient
                    // issue is repaired. Success elsewhere cannot erase a dropped source.
                    issuesBySource.removeValue(forKey: key)
                    if projectID != nil {
                        issuesBySource.removeValue(forKey: "*\u{0}" + sourceID)
                    }
                }
            } else if isIssue {
                unscopedReasonsByProject[project, default: []].insert(
                    outcome.reason ?? "source_ingestion_incomplete")
            }
        }

        func reasons(projectID: String?) -> [String] {
            let project = projectID ?? "*"
            let scoped = (Array(issuesBySource.values) + Array(rootIssues.values)).filter {
                $0.projectID == project || $0.projectID == "*"
            }.map(\.reason)
            return Array(Set(scoped)
                .union(unscopedReasonsByProject[project] ?? [])
                .union(unscopedReasonsByProject["*"] ?? [])).sorted()
        }

        var jsonObject: [String: Any] {
            let reasons = Set((Array(issuesBySource.values) + Array(rootIssues.values)).map(\.reason)
                + unscopedReasonsByProject.values.flatMap { $0 })
            return ["status": issueCount == 0 ? "complete" : "partial",
                    "issueCount": issueCount, "reasons": reasons.sorted(),
                    "rootProjection": ["retainedGapCount": rootIssues.count,
                        "retiredUnprovenGapCount": retiredRootGapCount,
                        "evictedUnprovenGapCount": evictedRootGapCount,
                        "limit": Self.rootIssueLimit]]
        }
    }

    static func projectID(_ canonical: String) -> String {
        "project-" + SHA256.hash(data: Data(canonical.utf8)).prefix(12)
            .map { String(format: "%02x", $0) }.joined()
    }

    static func persistentProjectName(_ canonical: String,
                                      presentationLabel: String? = nil) -> String {
        // Presentation labels are joined only at read time. Persisting them here would alternate
        // with ingest(), which always has the canonical repository basename.
        _ = presentationLabel
        return URL(fileURLWithPath: canonical).lastPathComponent
    }

    /// Join display metadata only after resolving each Start Point to the exact canonical Project
    /// identity used by board ingestion. A nearby path or a matching label is never a join key.
    static func projectPresentations(_ places: [StartPoints.Place]) -> [String: ProjectPresentation] {
        var result: [String: ProjectPresentation] = [:]
        for place in places {
            guard let canonical = UsageLedger.canonicalProjectKey(projectDir: place.path) else {
                continue
            }
            let id = projectID(canonical)
            guard result[id] == nil else { continue }
            result[id] = ProjectPresentation(
                label: StartPoints.label(for: canonical), displayPath: canonical,
                icon: ProjectIcon.grid(forCwd: canonical).map(ProjectIcon.gridJSON))
        }
        return result
    }

    /// Start Points are newest-first. Canonical repository identity may collapse a main checkout
    /// and one or more linked worktrees, so retain the first position deterministically instead of
    /// feeding duplicate keys to Dictionary's trapping initializer.
    static func projectOrder(_ places: [StartPoints.Place]) -> [String: Int] {
        var result: [String: Int] = [:]
        for (index, place) in places.enumerated() {
            guard let canonical = UsageLedger.canonicalProjectKey(projectDir: place.path) else {
                continue
            }
            let id = projectID(canonical)
            if result[id] == nil { result[id] = index }
        }
        return result
    }

    static func normalized(_ record: [String: Any]) -> [String: Any] {
        var row = record
        row["workItemId"] = record["work_item_id"] ?? record["workItemId"]
        row["workPhase"] = record["work_phase"] ?? record["workPhase"]
        row["projectDir"] = record["project_dir"] ?? record["projectDir"]
        row["finishedAt"] = record["finished_at"] ?? record["finishedAt"]
        row["startedAt"] = record["started_at"] ?? record["startedAt"]
            ?? record["briefed_at"] ?? record["briefedAt"] ?? record["spawned_at"]
            ?? record["spawnedAt"] ?? record["created"] ?? record["createdAt"]
        if row["child"] == nil {
            row["child"] = ["sessionId": record["child_session"] ?? "",
                            "terminalId": record["child_terminal"] ?? ""]
        }
        return row
    }

    @discardableResult
    static func ingest(_ record: [String: Any], store: ProjectBoardStore = .shared)
        -> (projectID: String?, outcome: ProjectBoardStore.AutomaticMutationOutcome) {
        guard let canonical = UsageLedger.canonicalProjectKey(
            projectDir: record["project_dir"] as? String ?? record["projectDir"] as? String,
            repositoryCommonDir: record["repository_common_dir"] as? String) else {
            return (nil, .init(status: .refused, acceptedCount: 0, droppedCount: 1,
                               persisted: false, reason: "project_identity_unavailable"))
        }
        let id = projectID(canonical)
        let project = store.ensureProject(id: id, name: URL(fileURLWithPath: canonical).lastPathComponent)
        guard project.persisted, project.status == .accepted || project.status == .unchanged else {
            return (id, project)
        }
        return (id, store.ingest(task: normalized(record), projectID: id))
    }

    /// Capture the record before leaving the broker owner; never call back into
    /// Orchestrator while holding the board store's lock.
    static func observe(_ record: [String: Any]) {
        let suppliedStore = storeForTesting, suppliedRefresh = refreshForTesting
        queue.async {
            let store = suppliedStore ?? ProjectBoardStore.shared
            let result = ingest(record, store: store)
            coverageLock.lock()
            ingestionCoverage.record(result.outcome, projectID: result.projectID,
                                     sourceID: sourceID(record))
            coverageLock.unlock()
            let header = store.readHeader()
            reads.refresh(header: header,
                          reasons: [.catalog, .durableModel],
                          work: { suppliedRefresh?(store, $0) ?? reconcile(store: store, reasons: $0) })
        }
    }

    static func drainObservationsForTesting() { queue.sync {} }

    /// Root receipts are not Tasks. Journal the binding before returning the landing response,
    /// after releasing the broker lock. Board writes still run only on the workflow worker.
    /// A next turn can consume the broker receipt, so an unjournaled async callback is not durable.
    @discardableResult
    static func observeRootLanding(_ delivery: Orchestrator.SessionDelivery) -> [String: Any] {
        let outcome = ProjectBoardWorkflow.shared.recordRootLanding(delivery)
        queue.async { recordRootLandingCoverage(delivery, outcome: outcome) }
        return outcome.object
    }

    private static func ingestRootLanding(_ delivery: Orchestrator.SessionDelivery) {
        recordRootLandingCoverage(delivery,
            outcome: ProjectBoardWorkflow.shared.recordRootLanding(delivery))
    }

    private static func recordRootLandingCoverage(_ delivery: Orchestrator.SessionDelivery,
                                                  outcome: ProjectBoardWorkflow.Outcome) {
        guard let landing = delivery.landing,
              Orchestrator.isBrokerVerifiedSessionLanding(landing) else { return }
        let source = rootLandingSourceID(delivery)
        coverageLock.lock()
        let previousCount = ingestionCoverage.issueCount
        ingestionCoverage.recordRootLanding(outcome, sourceID: source)
        let changed = previousCount != ingestionCoverage.issueCount
        coverageLock.unlock()
        if changed { didMutate() }
    }

    private static func rootLandingSourceID(_ delivery: Orchestrator.SessionDelivery) -> String {
        "root:" + delivery.identity.terminalID + ":"
            + String(delivery.landing?.landedAt.timeIntervalSince1970 ?? 0)
    }

    private static func reconcileRootLandings() {
        let deliveries = Orchestrator.boardRootLandingSnapshot()
        coverageLock.lock()
        let before = ingestionCoverage.issueCount
        ingestionCoverage.retainRootLandings(Set(deliveries.map(rootLandingSourceID)))
        let changed = before != ingestionCoverage.issueCount
        coverageLock.unlock()
        for delivery in deliveries { ingestRootLanding(delivery) }
        if changed { didMutate() }
    }

    private static func sourceID(_ record: [String: Any]) -> String? {
        record["id"] as? String ?? record["task_id"] as? String
            ?? record["taskId"] as? String
    }

    static func read(project: String?, item: String?) -> [String: Any] {
        let store = storeForTesting ?? ProjectBoardStore.shared
        let header = store.readHeader()
        return reads.read(project: project, item: item, header: header,
                          loading: store.readSeedIfAvailable()?.seed.envelope(
                            project: project, item: item) ?? loadingEnvelope(header: header)) { reasons in
            refreshForTesting?(store, reasons) ?? reconcile(store: store, reasons: reasons)
        }
    }

    static func configureReadStoreForTesting(_ store: ProjectBoardStore?,
        refresh: ((ProjectBoardStore, ProjectBoardReadCache.DirtyReasons)
            -> Result<ProjectBoardReadCache.Model,
            ProjectBoardReadCache.Failure>)? = nil) {
        storeForTesting = store
        refreshForTesting = refresh
        reads.resetForTesting()
    }

    static var readCacheStateForTesting: ProjectBoardReadCache.State { reads.stateForTesting }

    private static func loadingEnvelope(header: ProjectBoardStore.ReadHeader) -> [String: Any] {
        ["board": [
            "schemaVersion": 1, "revision": header.revision, "enabled": header.enabled,
            "narrativeConsent": header.narrativeConsent as Any? ?? NSNull(),
            "mode": header.enabled ? "board" : "standard",
            "entitlement": ["state": "free_preview", "label": "Currently free"],
            "projects": [] as [[String: Any]], "items": [] as [[String: Any]],
            "item": NSNull(), "truncated": false, "updatedAt": header.updatedAt,
            "available": header.available,
        ] as [String: Any]]
    }

    /// Called during app startup before either HTTP or Cloud admission opens. Durable decoding and
    /// the first materialized Store seed therefore never become the first GET's hidden work.
    static func prepare() {
        coverageLock.lock()
        guard !prepared else { coverageLock.unlock(); return }
        prepared = true
        coverageLock.unlock()
        // New ordinary Sessions may introduce a Project without dispatching a broker task.
        // Observe the existing immutable inventory cheaply; never rescan transcripts on GET.
        configObserver = NotificationCenter.default.addObserver(forName: .clawdlineConfigChanged,
            object: nil, queue: nil) { _ in
                queue.async { catalogSignal.invalidate() }
            }
        queue.async {
            let timer = DispatchSource.makeTimerSource(queue: queue)
            timer.schedule(deadline: .now() + 10, repeating: 20, leeway: .seconds(3))
            timer.setEventHandler {
                let header = ProjectBoardStore.shared.readHeader()
                guard header.enabled else { return }
                reconcileRootLandings()
                let inventory = SessionWatch.shared.publishedInventory()
                let paths = Set(inventory.identities.values.compactMap(\.workingDirectory))
                guard catalogSignal.observe(enabled: header.enabled,
                    complete: inventory.complete, paths: paths) else { return }
                reads.refresh(header: header, reasons: [.catalog, .durableModel],
                              work: { reconcile(store: .shared, reasons: $0) })
            }
            catalogTimer = timer
            timer.resume()
        }
        let store = ProjectBoardStore.shared
        let header = store.readHeader()
        reads.setEnabled(header.enabled)
        queue.async {
            _ = store.readSeed(rebuild: true)
            let current = store.readHeader()
            // Durable OFF history is a settled read-only projection. Startup never infers a
            // Project, replays broker history, or scans usage merely to make retained facts visible.
            if case .success(let retained) = materializeReadModel(store: store) {
                reads.seed(retained)
            }
            guard current.enabled else { return }
            reconcileRootLandings()
            reads.refresh(header: current,
                          reasons: [.modeCatchUp, .catalog, .durableModel, .usage],
                          work: { reconcile(store: store, reasons: $0) })
        }
    }

    static func modeDidChange(enabled: Bool) {
        reads.setEnabled(enabled)
        guard enabled else { return }
        let store = ProjectBoardStore.shared
        let header = store.readHeader()
        reads.refresh(header: header,
                      reasons: [.modeCatchUp, .catalog, .durableModel, .usage],
                      work: { reconcile(store: store, reasons: $0) })
    }

    /// A successful Board mutation dirties the durable seed in ProjectBoardStore. Reconcile it
    /// here, on the bounded worker, instead of making the next GET discover and rebuild the model.
    static func didMutate() {
        let store = ProjectBoardStore.shared
        let header = store.readHeader()
        reads.refresh(header: header, reasons: [.catalog, .durableModel],
                      work: { reconcile(store: store, reasons: $0) })
    }

    /// A committed ledger checkpoint emits this cheap invalidation after leaving the ledger queue.
    /// The Board neither owns the ledger nor waits on its SQLite work.
    static func usageDidCommit() {
        let store = storeForTesting ?? ProjectBoardStore.shared
        let header = store.readHeader()
        let testRefresh = refreshForTesting
        reads.refresh(header: header, reasons: [.usage],
                      work: { testRefresh?(store, $0) ?? reconcile(store: store, reasons: $0) })
    }

    /// One running worker consumes one typed reason union. Event floods can add at most one
    /// successor union, and each source is refreshed at most once in that generation.
    private static func reconcile(store: ProjectBoardStore,
                                  reasons: ProjectBoardReadCache.DirtyReasons)
        -> Result<ProjectBoardReadCache.Model, ProjectBoardReadCache.Failure> {
        guard store.enabled else {
            return .failure(.init(code: "board_disabled",
                                  message: "Project Board reconciliation is disabled."))
        }
        if reasons.contains(.modeCatchUp) { catchUpBrokerHistory(store: store) }
        if reasons.contains(.catalog) { refreshCatalogSources(store: store) }
        if reasons.contains(.usage) { refreshUsageSources() }
        return materializeReadModel(store: store)
    }

    private static func refreshCatalogSources(store: ProjectBoardStore) {
        let places = StartPoints.places()
        let presentations = projectPresentations(places)
        var outcomes: [(String, ProjectBoardStore.AutomaticMutationOutcome, String)] = []
        for place in places {
            guard let canonical = UsageLedger.canonicalProjectKey(projectDir: place.path) else { continue }
            let id = projectID(canonical)
            let outcome = store.ensureProject(
                id: id, name: persistentProjectName(canonical,
                    presentationLabel: presentations[id]?.label))
            outcomes.append((id, outcome, "start-point:" + canonical))
        }
        let order = projectOrder(places)
        coverageLock.lock()
        source.presentations = presentations
        source.projectOrder = order
        for (projectID, outcome, sourceID) in outcomes {
            ingestionCoverage.record(outcome, projectID: projectID, sourceID: sourceID)
        }
        coverageLock.unlock()
    }

    /// OFF intentionally refuses inference. Re-enabling therefore catches up the bounded broker
    /// registry once on the background read worker; ordinary GET never enters this replay path.
    private static func catchUpBrokerHistory(store: ProjectBoardStore) {
        var outcomes: [(String?, ProjectBoardStore.AutomaticMutationOutcome, String?)] = []
        for record in Orchestrator.ledgerBackfillRecords() {
            let result = ingest(record)
            outcomes.append((result.projectID, result.outcome, sourceID(record)))
        }
        coverageLock.lock()
        for (projectID, outcome, sourceID) in outcomes {
            ingestionCoverage.record(outcome, projectID: projectID, sourceID: sourceID)
        }
        coverageLock.unlock()
    }

    /// Usage is refreshed only from a broker/ledger event (or mode-on bootstrap), never because a
    /// GET crossed a TTL. The indexed rows are then reused by every scoped materialization.
    private static func refreshUsageSources() {
        let usageRead = UsageLedger.shared.analyticsRead(.init(limit: sourceLimit + 1))
        let sourceTruncated = usageRead.rows.count > sourceLimit
        let rows = Array(usageRead.rows.prefix(sourceLimit))
        var revisionHasher = SHA256()
        for row in rows {
            var fields: [String] = [row.intervalKey, row.taskID ?? "", row.coverage,
                          row.coverageReasons.joined(separator: ","), row.costUnit ?? "",
                          row.costBasis, row.costValue.map { String($0) } ?? ""]
            fields.append(contentsOf: UsageLedger.Part.allCases.map {
                row.counts[$0].map(String.init) ?? "?"
            })
            revisionHasher.update(data: Data(fields.joined(separator: "\u{0}").utf8))
        }
        let usageRevision = revisionHasher.finalize().map { String(format: "%02x", $0) }.joined()
            + ":\(usageRead.corrections):\(sourceTruncated)"
        coverageLock.lock()
        let usageUnchanged = source.usageRevision == usageRevision
        coverageLock.unlock()
        if usageUnchanged { return }
        var rowsByTask: [String: [UsageLedger.Row]] = [:]
        for row in rows {
            if let taskID = row.taskID { rowsByTask[taskID, default: []].append(row) }
        }
        coverageLock.lock()
        source.rowsByTask = rowsByTask
        source.usageRows = rows.count
        source.usageTruncated = sourceTruncated
        source.usageThrough = usageRead.latestLedgerObservation
        source.usageRevision = usageRevision
        coverageLock.unlock()
    }

    private static func materializeReadModel(store: ProjectBoardStore)
        -> Result<ProjectBoardReadCache.Model, ProjectBoardReadCache.Failure> {
        coverageLock.lock()
        let sources = source
        let coverage = ingestionCoverage
        coverageLock.unlock()
        let seed = store.readSeed(rebuild: true).seed
        guard seed.header.available else {
            let error = seed.error ?? [:]
            return .failure(.init(code: error["code"] as? String ?? "board_unavailable",
                                  message: error["message"] as? String ?? "Board store unavailable."))
        }
        let observedAt = Date().timeIntervalSince1970
        var catalog = seed.catalog.map { row -> [String: Any] in
            var value = row
            let id = row["id"] as? String ?? ""
            if let presentation = sources.presentations[id] {
                value.merge(presentation.jsonObject) { _, new in new }
                value["isStartPoint"] = true
            } else {
                value["isStartPoint"] = false
            }
            value["observedAt"] = observedAt
            return value
        }
        catalog.sort { lhs, rhs in
            let left = sources.projectOrder[lhs["id"] as? String ?? ""]
            let right = sources.projectOrder[rhs["id"] as? String ?? ""]
            if let left, let right { return left < right }
            if left != nil { return true }
            if right != nil { return false }
            return (lhs["name"] as? String ?? "").localizedCaseInsensitiveCompare(
                rhs["name"] as? String ?? "") == .orderedAscending
        }

        let catalogEnvelope = projected(seed.envelope(), catalog: catalog,
            coverage: coverage, sources: sources, observedAt: observedAt)
        var projectModels: [String: [String: Any]] = [:]
        for project in catalog {
            guard let projectID = project["id"] as? String else { continue }
            let reasons = coverage.reasons(projectID: projectID)
            let summaries = (seed.itemSummariesByProject[projectID] ?? []).map {
                compactCard(enrichFromIndex($0, rowsByTask: sources.rowsByTask,
                                            truncated: sources.usageTruncated,
                                            ingestionReasons: reasons))
            }
            var envelope = seed.envelope(project: projectID)
            if var board = envelope["board"] as? [String: Any] {
                board["projects"] = catalog
                // The budget is for the scoped response, so measure the fixed shell without the
                // seed's complete item array. Otherwise every item is counted once in `base` and
                // again while selecting summaries, making a large but valid project look empty.
                board["items"] = [] as [[String: Any]]
                let retained = boundedSummaries(summaries, board: board)
                let omitted = summaries.count - retained.count
                board["items"] = retained
                board["truncated"] = omitted > 0
                board["truncation"] = [
                    "reason": omitted == 0 ? NSNull()
                        : (retained.count == 500 ? "item_count_limit" : "snapshot_byte_budget"),
                    "itemsOmittedCount": omitted,
                ] as [String: Any]
                envelope["board"] = board
            }
            projectModels[projectID] = projected(envelope, catalog: catalog,
                coverage: coverage, sources: sources, observedAt: observedAt)
        }
        return .success(.init(revision: seed.header.revision, observedAt: observedAt,
                              catalog: catalogEnvelope, projects: projectModels,
                              items: [:], resolveItem: { itemID, requestedProject in
            guard let detail = seed.itemDetailsByID[itemID],
                  let projectID = detail["projectId"] as? String,
                  requestedProject == nil || requestedProject == projectID else { return nil }
            var itemEnvelope = seed.envelope(project: projectID, item: itemID)
            if var board = itemEnvelope["board"] as? [String: Any] {
                board["projects"] = catalog
                board["items"] = [] as [[String: Any]]
                board["item"] = enrichFromIndex(
                    detail, rowsByTask: sources.rowsByTask,
                    truncated: sources.usageTruncated,
                    ingestionReasons: coverage.reasons(projectID: projectID))
                itemEnvelope["board"] = board
            }
            return projected(itemEnvelope, catalog: catalog,
                             coverage: coverage, sources: sources, observedAt: observedAt)
        }, resolveSession: { sessionID in
            projected(seed.envelope(item: "session:" + sessionID), catalog: catalog,
                      coverage: coverage, sources: sources, observedAt: observedAt)
        }))
    }

    private static func enrichFromIndex(_ item: [String: Any],
                                        rowsByTask: [String: [UsageLedger.Row]],
                                        truncated: Bool, ingestionReasons: [String]) -> [String: Any] {
        let links = item["accountingTaskLinks"] as? [[String: Any]] ?? []
        let rows = links.compactMap { $0["targetId"] as? String }.flatMap { rowsByTask[$0] ?? [] }
        return enriched(item, rows: rows, truncated: truncated,
                        ingestionReasons: ingestionReasons)
    }

    static func boundedSummaries(_ items: [[String: Any]], board: [String: Any])
        -> [[String: Any]] {
        let budget = 1_000_000
        let base = (try? JSONSerialization.data(withJSONObject: ["board": board]).count) ?? budget
        var used = base
        var retained: [[String: Any]] = []
        for item in items.prefix(500) {
            let size = (try? JSONSerialization.data(withJSONObject: item).count) ?? budget
            let separator = retained.isEmpty ? 0 : 1
            guard used + size + separator <= budget else { break }
            retained.append(item); used += size + separator
        }
        return retained
    }

    /// Stable, bounded list projection. Selected item detail continues to use `enriched` directly;
    /// a card never pays to carry the detail arrays merely so accounting can find its task links.
    static func compactCard(_ item: [String: Any]) -> [String: Any] {
        let stableFields = [
            "id", "key", "projectId", "title", "type", "state", "summary", "owner",
            "parentId", "createdAt", "updatedAt", "scopeRevision", "progress",
            "presentation", "deliveryLanes", "deliveryLaneCount",
            "listSummary", // Complete bounded attention facts, never raw obligation/detail arrays.
        ]
        var card: [String: Any] = [:]
        for key in stableFields where item[key] != nil { card[key] = item[key] }

        if let usage = item["usage"] as? [String: Any] {
            let usageFields = [
                "state", "rows", "measured", "total", "output", "incompleteRows",
                "coverageReasons", "truncated", "costSeries", "missingCostRows",
            ]
            var summary: [String: Any] = [:]
            for key in usageFields where usage[key] != nil { summary[key] = usage[key] }
            card["usage"] = summary
        }

        let projection = item["projection"] as? [String: [String: Any]] ?? [:]
        func collectionSummary(_ name: String, rows: [[String: Any]], checklist: Bool)
            -> [String: Any] {
            let retained = projection[name]?["retainedCount"] as? Int ?? rows.count
            let omitted = projection[name]?["omittedCount"] as? Int ?? 0
            let isComplete: ([String: Any]) -> Bool = {
                ["passed", "not_applicable"].contains($0["status"] as? String ?? "")
            }
            var value: [String: Any] = [
                "total": retained + omitted,
                "completed": rows.filter(isComplete).count,
                "retained": retained,
                "omitted": omitted,
                "coverage": omitted == 0 ? "complete" : "partial",
            ]
            if checklist {
                let required = rows.filter { $0["required"] as? Bool == true }
                value["required"] = required.count
                value["requiredCompleted"] = required.filter(isComplete).count
            }
            return value
        }
        card["cardSummary"] = [
            "checklist": collectionSummary(
                "checklist", rows: item["checklist"] as? [[String: Any]] ?? [], checklist: true),
            "milestones": collectionSummary(
                "milestones", rows: item["milestones"] as? [[String: Any]] ?? [], checklist: false),
        ] as [String: Any]
        if let exact = item["cardSummary"] as? [String: Any] {
            card["cardSummary"] = exact
        }
        return card
    }

    private static func projected(_ envelope: [String: Any], catalog: [[String: Any]],
                                  coverage: IngestionCoverage,
                                  sources: MaterializedSources,
                                  observedAt: Double) -> [String: Any] {
        var envelope = envelope
        guard var board = envelope["board"] as? [String: Any] else { return envelope }
        board["projects"] = catalog
        let durable = board["sourceIngestion"] as? [String: Any] ?? [:]
        let durableReasons = durable["reasons"] as? [String] ?? []
        var ingestion = coverage.jsonObject
        ingestion["historicalDroppedCount"] = durable["droppedCount"] ?? 0
        ingestion["issueCount"] = coverage.issueCount + durableReasons.count
        ingestion["reasons"] = Set((ingestion["reasons"] as? [String] ?? []) + durableReasons).sorted()
        ingestion["status"] = coverage.issueCount == 0 && durableReasons.isEmpty ? "complete" : "partial"
        board["source"] = [
            "observedAt": observedAt, "attemptedAt": observedAt, "ingestion": ingestion,
            "usageThrough": sources.usageThrough.map {
                Int($0.timeIntervalSince1970)
            } as Any? ?? NSNull(),
            "usageRows": sources.usageRows, "truncated": sources.usageTruncated,
        ]
        envelope["board"] = board
        return envelope
    }

    static func enriched(_ item: [String: Any], rows: [UsageLedger.Row], truncated: Bool = false,
                         ingestionReasons: [String] = []) -> [String: Any] {
        var out = item
        var reasons = ingestionReasons
        let accounting = item["accountingTaskLinks"] as? [[String: Any]]
        if let persisted = item["sourceIngestion"] as? [String: Any] {
            reasons.append(contentsOf: persisted["reasons"] as? [String] ?? [])
        }
        if accounting == nil, let projection = item["projection"] as? [String: [String: Any]],
           let omitted = projection["links"]?["omittedCount"] as? Int, omitted > 0 {
            reasons.append("item_links_projection_incomplete")
        }
        let links = item["links"] as? [[String: Any]] ?? []
        let taskLinks = accounting?.filter { $0["source"] as? String == "broker" }
            ?? links.filter { $0["kind"] as? String == "task" && $0["source"] as? String == "broker" }
        let tasks = Set(taskLinks
            .compactMap { $0["targetId"] as? String })
        // A session relation is not accounting allocation. In particular a session
        // may have worked on several items before it was linked from this card.
        let matched = rows.filter { $0.taskID.map(tasks.contains) ?? false }
        var summary = usage(matched, truncated: truncated, coverageReasons: reasons)
        var phases: [String: Any] = [:]
        var declared = Set<String>()
        for phase in ["planning", "output", "review_testing", "correction", "integration"] {
            let ids = Set(taskLinks.filter { $0["phase"] as? String == phase }
                .compactMap { $0["targetId"] as? String })
            declared.formUnion(ids)
            let phaseRows = matched.filter { $0.taskID.map(ids.contains) ?? false }
            if !phaseRows.isEmpty { phases[phase] = usage(phaseRows, truncated: truncated, coverageReasons: reasons) }
        }
        summary["phases"] = phases
        summary["undeclaredRows"] = Set(matched.filter { !($0.taskID.map(declared.contains) ?? false) }
            .map(\.intervalKey)).count
        summary["phaseReason"] = phases.isEmpty ? "historical_phase_not_measured" : "dispatch_declared_task_boundary"
        out["usage"] = summary
        return out
    }

    static func usage(_ rows: [UsageLedger.Row], truncated: Bool = false, coverageReasons: [String] = []) -> [String: Any] {
        var seen = Set<String>()
        let unique = rows.filter { seen.insert($0.intervalKey).inserted }
        var measured = 0, output = 0, incomplete = 0, unknownOutput = 0
        var parts: [String: Int] = [:]
        var costs: [String: (unit: String, basis: String, value: Double, rows: Int)] = [:]
        var missingCosts = 0
        var reasons = Set(coverageReasons)
        for row in unique {
            let reading = row.measurement
            reasons.formUnion(reading.reasons)
            if reading.incomplete { incomplete += 1 }
            for part in UsageLedger.Part.allCases {
                if let count = reading.counts[part] {
                    measured += count; parts[part.rawValue, default: 0] += count
                }
            }
            if let count = reading.counts.output { output += count } else { unknownOutput += 1 }
            if let amount = row.costValue, let unit = row.costUnit {
                let key = unit + ":" + row.costBasis
                var series = costs[key] ?? (unit, row.costBasis, 0, 0)
                series.value += amount; series.rows += 1; costs[key] = series
            } else { missingCosts += 1 }
        }
        if truncated { reasons.insert("source_scan_truncated") }
        let state = truncated ? "partial" : (unique.isEmpty ? (reasons.isEmpty ? "absent" : "unavailable") :
            (!reasons.isEmpty ? "qualified" : (incomplete > 0 ? "partial" : "present")))
        return ["state": state,
            "rows": unique.count, "measured": measured,
            "total": truncated || unique.isEmpty || incomplete > 0 ? NSNull() : measured as Any,
            "output": truncated || unique.isEmpty || unknownOutput > 0 ? NSNull() : output as Any,
            "parts": parts, "incompleteRows": incomplete, "coverageReasons": reasons.sorted(), "truncated": truncated,
            "phases": [:] as [String: Any], "undeclaredRows": unique.count,
            "phaseReason": "historical_phase_not_measured",
            "costSeries": costs.keys.sorted().compactMap { key -> [String: Any]? in
                guard let value = costs[key] else { return nil }
                return ["unit": value.unit, "basis": value.basis,
                        "value": value.value, "rows": value.rows]
            }, "missingCostRows": missingCosts]
    }

    static func workflowContext(itemID: String? = nil, phase: String? = nil) -> String {
        let enabled = ProjectBoardStore.shared.enabled
        guard enabled else {
            return "Workflow mode: standard. Board is disabled; no board item, phase, checklist or board-only verification gate is required. Existing task, permission, claims, landing and machine-lock safeguards still apply."
        }
        return """
        Workflow mode: board (currently free). Re-read GET /v1/board before starting a new board action; the user can disable this mode at any time.
        Work item: \(itemID ?? "unassigned; retain explicit task/graph lineage, do not guess from a title"). Activity phase: \(phase ?? "undeclared").
        Record the objective, checklist progress, output references, next action and unresolved obligations in your task progress/result artifacts for the owning root to attach to this item. Project Board status is computed automatically from recorded broker, checklist, obligation, finding, verification and landing facts; users do not operate lifecycle controls. Task success means delivery, not item closure; handoff means transfer, not completion. Never fabricate a token phase split from elapsed time. The owning root uses the machine-authenticated board API; a child must not read that credential. A network failure must not prevent the existing task result/progress file protocol.
        Once ordinary work is closed or its current scope has an authoritative landed projection, the owning root may consolidate those ordinary conversation, result and handoff facts into record_report. Coordination instead requires a named bounded time interval or same-item handoff and never gains lifecycle from its report. The report names objective, delivered outcomes, verification/landing limits, remaining work, lessons and authoring-time source references. It is attributed narrative, never verification or landing evidence; missing or failed report generation does not hold or advance lifecycle. Children never call record_report.
        """
    }
}
