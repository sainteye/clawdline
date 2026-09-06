import Foundation

/// **The verification ledger, per Feature.** What a Feature's reviews found, how severe it was,
/// what proving the work cost in wall-clock, and how the tokens divided between building the
/// thing and reading it.
///
/// Store version 6 put four tables behind this — `task_review_receipts`, `task_review_axes`,
/// `task_review_findings` and `task_verification_receipts` — each carrying the `graph_id` that
/// names the Feature, and gave `usage_intervals.graph_id` the producer it had been waiting for.
/// That was storage. This is the read, and it exists because the numbers were already durable
/// and nothing could ask them a question.
///
/// **Its whole subject is telling three answers apart**, and the reason is written into the
/// finding that started this line: 20.4% of this Mac's tokens could name the Feature they were
/// spent on, and 48 graphs would have reported `0`. So every quantity here is one of
///
/// - ``Presence/present`` — there are records and they say something;
/// - ``Presence/absent`` — this Mac holds no such record for this Feature;
/// - ``Presence/unknown`` — records exist and cannot be read as a number.
///
/// and the last two are **never** serialized as `0`. `UsageLedger.unavailableDimensions` says it
/// in one line: *available is a statement about the producer, not about every row*.
///
/// **And `absent` is deliberately weaker than it looks.** A receipt whose write failed leaves a
/// log line and no column — the receipt tables have no `coverage_reasons` of their own, and
/// giving them one is a store version somebody else owns. So "this Mac holds no review receipt
/// for this Feature" is the whole of what `absent` may be read as; it is not "nobody reviewed
/// it", and no wording on the page in front of it says that.
final class VerificationLedgerService {

    // MARK: - Vocabulary

    /// Which side of the work a stored interval belongs to.
    ///
    /// **`unknown` is a rung and not a default.** Folding it into `implementation` is the same
    /// error as drawing an unknown token count as `0`, one level up: it turns "nothing on this row
    /// says which side it was on" into a claim about the side. The `LEFT JOIN … WHEN NULL THEN
    /// 'implementation'` this read replaces made exactly that claim, and it made it hardest
    /// exactly where a review's receipt had failed to write.
    enum Role: String, CaseIterable {
        case implementation
        case review
        case unknown
    }

    /// The three answers every quantity on this page is allowed to give.
    ///
    /// Spelled `absent` rather than `none` because `.none` beside an optional is two things in
    /// Swift and this one is a rung on a ladder, not the absence of a value.
    enum Presence: String, CaseIterable {
        case present
        case absent
        case unknown
    }

    /// **Whether a stored interval was spent reviewing.**
    ///
    /// The receipt outranks the dispatch kind: a task whose verdict is in the store did review,
    /// whatever its `kind` was called. Below that this asks
    /// ``Orchestrator/kindDenotesReview(_:)`` rather than comparing with a literal, because
    /// `kind` is unvalidated forty-character free text and every closed enumeration of it has
    /// drifted. ``Orchestrator/requiresTypedReview(_:)`` is the same decision one layer up, over
    /// a whole `Task` with its graph node — it is what put the receipt in the store in the first
    /// place, and it is why a receipt is allowed to answer this on its own.
    ///
    /// A row carrying no `kind_raw` at all and no receipt is ``Role/unknown``.
    static func role(kindRaw: String?, hasReviewReceipt: Bool) -> Role {
        if hasReviewReceipt { return .review }
        guard let kindRaw, !kindRaw.trimmingCharacters(in: .whitespaces).isEmpty else {
            return .unknown
        }
        return Orchestrator.kindDenotesReview(kindRaw) ? .review : .implementation
    }

    // MARK: - What one bucket of tokens amounts to

    /// The tokens of one role inside one Feature, with everything the store could not measure
    /// still visible beside them.
    struct TokenReading: Equatable {
        /// Interval rows in this bucket. `0` is the whole of what ``Presence/none`` means.
        var rows = 0
        /// What was actually measured, summed. A floor, never a total.
        var measured = 0
        /// The strict total: nil the moment one row could not measure one of its four parts.
        var total: Int?
        /// Rows that measured nothing at all — the rows nothing may render as a number.
        var unknownRows = 0
        /// Rows that measured something and not everything.
        var incompleteRows = 0
        /// Every mark the store put on the rows in this bucket, sorted and deduplicated.
        var reasons: [String] = []

        /// Add one stored row, through ``UsageLedger/Row/measurement`` and nothing else. Reading
        /// the token columns here instead would be the fourth reader to do it; see that type.
        mutating func add(_ row: UsageLedger.Row) {
            let measurement = row.measurement
            rows += 1
            measured += measurement.measured
            if measurement.unknown { unknownRows += 1 }
            if measurement.incomplete { incompleteRows += 1 }
            for reason in measurement.reasons where !reasons.contains(reason) {
                reasons.append(reason)
            }
            // Strict, and it stays nil once it has gone nil: a range holding one unmeasured part
            // has no total, only a floor.
            if measurement.incomplete { total = nil }
            else if incompleteRows == 0 { total = (total ?? 0) + (measurement.total ?? 0) }
        }

        /// **The three states, decided in one place.**
        ///
        /// `rows == 0` is "nothing was filed here", which is not a quantity at all. Every row
        /// unmeasurable is "there was spending and this Mac cannot say how much". Neither is `0`,
        /// and the payload below refuses to write one for either.
        var presence: Presence {
            if rows == 0 { return .absent }
            if unknownRows == rows { return .unknown }
            return .present
        }
    }

    /// What a Feature's reviews found.
    struct FindingsReading: Equatable {
        var reviewReceipts = 0
        var total = 0
        /// `blocking` / `important` / `minor` and whatever else a reviewer actually wrote, with
        /// its count. Not a closed enumeration: `severity` is free text at the receipt boundary.
        var severities: [String: Int] = [:]
        var truncated = false

        /// `present` covers "reviewed and found nothing", which is a positive answer and the one
        /// worth reaching. `absent` is the absence of a *receipt* and says nothing about whether
        /// a review happened — see the note on this type's owner.
        var presence: Presence { reviewReceipts == 0 ? .absent : .present }
    }

    /// What a Feature's tasks said they ran to prove their own work.
    struct VerificationReading: Equatable {
        var receipts = 0
        var runs = 0
        var seconds = 0
        var endedRed = 0
        var scopes: [String] = []

        var presence: Presence { receipts == 0 ? .absent : .present }
    }

    /// One review verdict, and the axes it answered on.
    struct AxisReading: Equatable {
        var taskID: String
        var axis: String
        var status: String
        var findingCount: Int
    }

    /// One Feature's whole row.
    struct Feature: Equatable {
        /// nil for the unattributed block — see ``Answer``.
        var graphID: String?
        var findings = FindingsReading()
        var verification = VerificationReading()
        var verdicts: [String: Int] = [:]
        var tokens: [Role: TokenReading] = [:]
        var rows = 0
        var tasks = 0
        var firstSeenAt: Date?
        var lastSeenAt: Date?
    }

    // MARK: - The query

    struct Query: Equatable {
        /// nil is the list of every Feature; a value is one Feature with its findings and axes.
        var graphID: String?

        init(graphID: String? = nil) { self.graphID = graphID }
    }

    struct ParseResult {
        var query: Query?
        var error: String?
    }

    struct Refusal: Equatable {
        var status: Int
        var code: String
        var message: String
    }

    /// One or the other, never both and never neither — the same contract the Projects read has,
    /// and for the same reason: an empty list is an answer this type is allowed to give, and it
    /// must not be reachable by accident from a read that never happened.
    enum Answer {
        case reading([String: Any])
        case refused(Refusal)

        var payload: [String: Any]? {
            if case .reading(let payload) = self { return payload }
            return nil
        }

        var refusal: Refusal? {
            if case .refused(let refusal) = self { return refusal }
            return nil
        }
    }

    static let schemaVersion = 1
    /// Features listed at once. The store holds a few hundred graphs on the busiest Mac measured;
    /// this is the wall that says so out loud rather than the wall a browser finds.
    static let maxFeatures = 500
    /// Findings returned with one Feature's detail.
    static let maxFindings = 200

    // MARK: - Where the numbers come from

    private let readRows: (UsageLedger.AnalyticsFilter) -> UsageLedger.AnalyticsRead
    private let readReviews: (UsageLedger.ReceiptScope) -> [UsageLedger.StoredReviewReceipt]
    private let readVerifications: (UsageLedger.ReceiptScope)
        -> [UsageLedger.StoredVerificationReceipt]

    /// Injected the way every other read of this store is, so a test can say what the store holds
    /// without a store existing. Production hands over `UsageLedger.shared`.
    init(readRows: @escaping (UsageLedger.AnalyticsFilter) -> UsageLedger.AnalyticsRead
            = { UsageLedger.shared.analyticsRead($0) },
         readReviews: @escaping (UsageLedger.ReceiptScope) -> [UsageLedger.StoredReviewReceipt]
            = { UsageLedger.shared.reviewReceipts($0) },
         readVerifications: @escaping (UsageLedger.ReceiptScope)
            -> [UsageLedger.StoredVerificationReceipt]
            = { UsageLedger.shared.verificationReceipts($0) }) {
        self.readRows = readRows
        self.readReviews = readReviews
        self.readVerifications = readVerifications
    }

    /// A closed query, refused on an unknown or repeated key for the same reason the analytics
    /// query is: a misspelled filter must not quietly widen an accounting read.
    static func parse(_ values: [String: String], repeatedKeys: Set<String>) -> ParseResult {
        let allowed: Set<String> = ["graph"]
        let unknown = Set(values.keys).subtracting(allowed)
        guard unknown.isEmpty else {
            return ParseResult(error: "Unknown verification-ledger query field: "
                                 + unknown.sorted().joined(separator: ", ") + ".")
        }
        guard repeatedKeys.isEmpty else {
            return ParseResult(error: "Verification-ledger query fields may appear only once.")
        }
        let graph = values["graph"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return ParseResult(query: Query(graphID: graph.isEmpty ? nil : graph))
    }

    // MARK: - The read

    func read(_ query: Query, now: Date = Date()) -> Answer {
        let reading = readRows(UsageLedger.AnalyticsFilter(
            limit: UsageQueryService.maxScannedRows + 1, includeFeatureAttribution: false))
        let truncated = reading.rows.count > UsageQueryService.maxScannedRows
        let rows = Array(reading.rows.prefix(UsageQueryService.maxScannedRows))
        let reviews = readReviews(.all)
        let verifications = readVerifications(.all)

        // Which tasks are known to have reviewed, whatever their dispatch kind was called. Built
        // once from every receipt in the store rather than per Feature, because a receipt filed
        // under no graph still proves its own task's role.
        let reviewed = Set(reviews.map { $0.taskID })

        var features: [String: Feature] = [:]
        var unattributed = Feature(graphID: nil)

        func feature(_ graphID: String?) -> Feature {
            guard let graphID else { return unattributed }
            return features[graphID] ?? Feature(graphID: graphID)
        }
        func store(_ value: Feature) {
            guard let id = value.graphID else { unattributed = value; return }
            features[id] = value
        }

        var taskIDs: [String: Set<String>] = [:]
        var unattributedTasks: Set<String> = []
        for row in rows {
            var value = feature(row.graphID)
            let role = Self.role(kindRaw: row.kindRaw, hasReviewReceipt: row.taskID
                                    .map { reviewed.contains($0) } ?? false)
            var bucket = value.tokens[role] ?? TokenReading()
            bucket.add(row)
            value.tokens[role] = bucket
            value.rows += 1
            value.firstSeenAt = [value.firstSeenAt, row.startedAt].compactMap { $0 }.min()
            value.lastSeenAt = [value.lastSeenAt, row.endedAt ?? row.startedAt]
                .compactMap { $0 }.max()
            store(value)
            if let taskID = row.taskID {
                if let graphID = row.graphID { taskIDs[graphID, default: []].insert(taskID) }
                else { unattributedTasks.insert(taskID) }
            }
        }

        for receipt in reviews {
            var value = feature(receipt.graphID)
            value.findings.reviewReceipts += 1
            value.verdicts[receipt.verdict, default: 0] += 1
            value.findings.total += receipt.findings.count
            for finding in receipt.findings {
                value.findings.severities[finding.severity, default: 0] += 1
            }
            store(value)
            if let graphID = receipt.graphID { taskIDs[graphID, default: []].insert(receipt.taskID) }
            else { unattributedTasks.insert(receipt.taskID) }
        }

        for receipt in verifications {
            var value = feature(receipt.graphID)
            value.verification.receipts += 1
            value.verification.runs += receipt.runs
            value.verification.seconds += receipt.seconds
            if receipt.last == "fail" { value.verification.endedRed += 1 }
            if !receipt.scope.isEmpty, !value.verification.scopes.contains(receipt.scope) {
                value.verification.scopes.append(receipt.scope)
            }
            store(value)
            if let graphID = receipt.graphID { taskIDs[graphID, default: []].insert(receipt.taskID) }
            else { unattributedTasks.insert(receipt.taskID) }
        }

        for (graphID, tasks) in taskIDs where features[graphID] != nil {
            features[graphID]?.tasks = tasks.count
        }
        unattributed.tasks = unattributedTasks.count

        if let wanted = query.graphID {
            guard let matched = features[wanted] else {
                // **A Feature nothing in this store mentions is a refusal, not an empty row.**
                // The two are one grey rectangle on a screen and completely different facts, and
                // only one of them is something a reader can act on.
                return .refused(Refusal(
                    status: 404, code: "graph_not_found",
                    message: "No stored receipt or interval names the graph \(wanted)."))
            }
            // Filtered from the receipts already in hand rather than asked for again. A second
            // query would be a second reading of a store two other sessions are writing to, and
            // the summary beside it would then be counted from a different instant than the
            // findings under it.
            let detail = reviews.filter { $0.graphID == wanted }
            return .reading(Self.detailPayload(matched, reviews: detail, truncated: truncated,
                                               rowsScanned: rows.count, at: now))
        }

        let ordered = features.values.sorted { left, right in
            let leftAt = left.lastSeenAt ?? .distantPast
            let rightAt = right.lastSeenAt ?? .distantPast
            if leftAt != rightAt { return leftAt > rightAt }
            return (left.graphID ?? "") < (right.graphID ?? "")
        }
        let listed = Array(ordered.prefix(Self.maxFeatures))
        return .reading([
            "schemaVersion": Self.schemaVersion,
            "features": listed.map { Self.payload(of: $0) },
            // Drawn as its own block and never merged into a Feature: these are the records that
            // carry no Feature key, which on a Mac that has not relaunched since the backfill
            // landed is most of them.
            "unattributed": Self.payload(of: unattributed),
            "read": [
                "rowsScanned": rows.count,
                "featuresFound": features.count,
                "featuresListed": listed.count,
                "truncated": truncated,
                "at": ISO8601DateFormatter().string(from: now),
            ],
        ])
    }

    // MARK: - On the wire

    /// **`measured` and `total` are `null` for every state but ``Presence/present``.**
    ///
    /// This is the one line the whole Feature exists to protect. A bucket with no rows spent an
    /// amount nobody knows, and a bucket whose rows all failed to measure spent an amount nobody
    /// knows; writing `0` for either is the defect this ledger was built to make visible, printed
    /// by the ledger itself. `rows` is still a number in every state, because *how many records
    /// there are* is a fact this side really does hold.
    static func payload(of reading: TokenReading) -> [String: Any] {
        let known = reading.presence == .present
        return [
            "state": reading.presence.rawValue,
            "rows": reading.rows,
            "unknownRows": reading.unknownRows,
            "incompleteRows": reading.incompleteRows,
            "reasons": reading.reasons.sorted(),
            "measured": known ? reading.measured as Any : NSNull(),
            "total": known ? (reading.total as Any? ?? NSNull()) : NSNull(),
        ]
    }

    /// Worst first, with an unrecognised severity last rather than dropped.
    static func severityPayload(_ severities: [String: Int]) -> [[String: Any]] {
        severities.keys.sorted { left, right in
            let leftRank = severityRank(left), rightRank = severityRank(right)
            return leftRank == rightRank ? left < right : leftRank < rightRank
        }.map { ["severity": $0, "count": severities[$0] ?? 0] }
    }

    static func payload(of reading: FindingsReading) -> [String: Any] {
        let known = reading.presence == .present
        return [
            "state": reading.presence.rawValue,
            "reviewReceipts": reading.reviewReceipts,
            "total": known ? reading.total as Any : NSNull(),
            "severities": severityPayload(reading.severities),
            "truncated": reading.truncated,
        ]
    }

    static func payload(of reading: VerificationReading) -> [String: Any] {
        let known = reading.presence == .present
        return [
            "state": reading.presence.rawValue,
            "receipts": reading.receipts,
            "runs": known ? reading.runs as Any : NSNull(),
            "seconds": known ? reading.seconds as Any : NSNull(),
            "endedRed": known ? reading.endedRed as Any : NSNull(),
            "scopes": reading.scopes,
        ]
    }

    /// Worst first, and an unrecognised word last rather than dropped: `severity` is free text at
    /// the receipt boundary and a reviewer who invents one still wrote a finding down.
    static func severityRank(_ severity: String) -> Int {
        switch severity {
        case "blocking": return 0
        case "important": return 1
        case "minor": return 2
        default: return 3
        }
    }

    static func payload(of feature: Feature) -> [String: Any] {
        var tokens: [String: Any] = [:]
        for role in Role.allCases {
            tokens[role.rawValue] = payload(of: feature.tokens[role] ?? TokenReading())
        }
        let formatter = ISO8601DateFormatter()
        return [
            "graphId": feature.graphID as Any? ?? NSNull(),
            "rows": feature.rows,
            "tasks": feature.tasks,
            "findings": payload(of: feature.findings),
            "verification": payload(of: feature.verification),
            "verdicts": feature.verdicts.map { ["verdict": $0.key, "count": $0.value] }
                .sorted { ($0["verdict"] as? String ?? "") < ($1["verdict"] as? String ?? "") },
            "tokens": tokens,
            "firstSeenAt": feature.firstSeenAt.map { formatter.string(from: $0) } as Any?
                ?? NSNull(),
            "lastSeenAt": feature.lastSeenAt.map { formatter.string(from: $0) } as Any? ?? NSNull(),
        ]
    }

    /// One Feature, with the findings themselves and the axes each review answered on.
    static func detailPayload(_ feature: Feature, reviews: [UsageLedger.StoredReviewReceipt],
                              truncated: Bool, rowsScanned: Int, at now: Date) -> [String: Any] {
        var findings: [UsageLedger.StoredReviewFinding] = []
        var axes: [AxisReading] = []
        for receipt in reviews {
            findings.append(contentsOf: receipt.findings)
            for axis in receipt.axes {
                axes.append(AxisReading(taskID: receipt.taskID, axis: axis.axis,
                                        status: axis.status, findingCount: axis.findingCount))
            }
        }
        let sorted = findings.sorted { left, right in
            let leftRank = severityRank(left.severity), rightRank = severityRank(right.severity)
            if leftRank != rightRank { return leftRank < rightRank }
            return left.findingID < right.findingID
        }
        var summary = feature
        summary.findings.truncated = sorted.count > maxFindings
        var out = payload(of: summary)
        out["items"] = sorted.prefix(maxFindings).map { finding in
            [
                "findingId": finding.findingID,
                "severity": finding.severity,
                "summary": finding.summary,
                "axis": finding.axis,
                "taskId": finding.taskID,
                "evidence": finding.evidence,
            ] as [String: Any]
        }
        out["axes"] = axes.map {
            [
                "taskId": $0.taskID, "axis": $0.axis, "status": $0.status,
                "findingCount": $0.findingCount,
            ] as [String: Any]
        }
        out["read"] = [
            "rowsScanned": rowsScanned,
            "truncated": truncated,
            "at": ISO8601DateFormatter().string(from: now),
        ]
        return ["schemaVersion": schemaVersion, "feature": out]
    }
}
