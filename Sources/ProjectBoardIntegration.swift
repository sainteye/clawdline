import CryptoKit
import Foundation

/// The adapter from existing broker/accounting facts to durable work items. The
/// board never reads transcripts or adds a task's cumulative usage a second time.
enum ProjectBoardIntegration {
    private static let queue = DispatchQueue(label: "clawdline.board.sources", qos: .utility)
    private static var refreshed = Date.distantPast
    private static var lastAttempt = Date.distantPast
    private static var ingestionCoverage = IngestionCoverage()
    private static var sourceRows: [UsageLedger.Row] = []
    private static var sourceProjects: [String: ProjectPresentation] = [:]
    private static var sourceTruncated = false
    private static var latestObservation: Date?
    static let sourceLimit = 100_000

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
        private(set) var issueCount = 0
        private(set) var reasonsByProject: [String: Set<String>] = [:]

        mutating func record(_ outcome: ProjectBoardStore.AutomaticMutationOutcome, projectID: String?) {
            // Standard mode deliberately does not ingest work. That is not a lost write.
            guard outcome.reason != "board_disabled", outcome.reason != "irrelevant",
                  outcome.status == .partial || outcome.status == .refused || outcome.status == .unavailable
                    || !outcome.persisted || outcome.droppedCount > 0 else { return }
            issueCount += 1
            reasonsByProject[projectID ?? "*", default: []].insert(outcome.reason ?? "source_ingestion_incomplete")
        }

        func reasons(projectID: String?) -> [String] {
            Array((reasonsByProject[projectID ?? "*"] ?? []).union(reasonsByProject["*"] ?? [])).sorted()
        }

        var jsonObject: [String: Any] {
            ["status": issueCount == 0 ? "complete" : "partial", "issueCount": issueCount,
             "reasons": Set(reasonsByProject.values.flatMap { $0 }).sorted()]
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
        queue.async {
            let result = ingest(record)
            ingestionCoverage.record(result.outcome, projectID: result.projectID)
            lastAttempt = .distantPast
        }
    }

    static func read(project: String?, item: String?) -> [String: Any] {
        queue.sync {
            if Date().timeIntervalSince(lastAttempt) >= 10 {
                var coverage = IngestionCoverage()
                let places = StartPoints.places()
                let presentations = projectPresentations(places)
                sourceProjects = presentations
                for place in places {
                    guard let canonical = UsageLedger.canonicalProjectKey(projectDir: place.path)
                    else { continue }
                    let id = projectID(canonical)
                    let result = ProjectBoardStore.shared.ensureProject(
                        id: id,
                        name: persistentProjectName(
                            canonical, presentationLabel: presentations[id]?.label))
                    coverage.record(result, projectID: id)
                }
                for record in Orchestrator.ledgerBackfillRecords() {
                    let result = ingest(record)
                    coverage.record(result.outcome, projectID: result.projectID)
                }
                let read = UsageLedger.shared.analyticsRead(.init(limit: sourceLimit + 1))
                sourceTruncated = read.rows.count > sourceLimit
                sourceRows = Array(read.rows.prefix(sourceLimit))
                latestObservation = read.latestLedgerObservation
                lastAttempt = Date()
                ingestionCoverage = coverage
                if coverage.issueCount == 0 { refreshed = lastAttempt }
            }
            var envelope = ProjectBoardStore.shared.snapshot(project: project, item: item)
            guard var board = envelope["board"] as? [String: Any] else { return envelope }
            if let projects = board["projects"] as? [[String: Any]] {
                board["projects"] = projects.map { project -> [String: Any] in
                    guard let id = project["id"] as? String,
                          let presentation = sourceProjects[id] else { return project }
                    return project.merging(presentation.jsonObject) { _, presentation in presentation }
                }
            }
            let durableCoverage = board["sourceIngestion"] as? [String: Any] ?? [:]
            let durableReasons = durableCoverage["reasons"] as? [String] ?? []
            let items = board["items"] as? [[String: Any]] ?? []
            board["items"] = items.map { enriched($0, rows: sourceRows, truncated: sourceTruncated,
                ingestionReasons: durableReasons + ingestionCoverage.reasons(projectID: $0["projectId"] as? String)) }
            if let selected = board["item"] as? [String: Any] {
                board["item"] = enriched(selected, rows: sourceRows, truncated: sourceTruncated,
                    ingestionReasons: durableReasons + ingestionCoverage.reasons(projectID: selected["projectId"] as? String))
            }
            board["truncated"] = sourceTruncated || (board["truncated"] as? Bool ?? false)
            var coverage = ingestionCoverage.jsonObject
            coverage["historicalDroppedCount"] = durableCoverage["droppedCount"] ?? 0
            coverage["issueCount"] = ingestionCoverage.issueCount + durableReasons.count
            coverage["reasons"] = Set((coverage["reasons"] as? [String] ?? []) + durableReasons).sorted()
            coverage["status"] = ingestionCoverage.issueCount == 0 && durableReasons.isEmpty ? "complete" : "partial"
            board["source"] = ["observedAt": refreshed == .distantPast ? NSNull() : Int(refreshed.timeIntervalSince1970) as Any,
                "attemptedAt": Int(lastAttempt.timeIntervalSince1970), "ingestion": coverage,
                "usageThrough": latestObservation.map { Int($0.timeIntervalSince1970) } as Any? ?? NSNull(),
                "usageRows": sourceRows.count, "truncated": sourceTruncated]
            envelope["board"] = board
            return envelope
        }
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
        """
    }
}
