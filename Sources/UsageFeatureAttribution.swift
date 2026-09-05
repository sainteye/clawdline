import Foundation

/// The bridge between the pure classifier in `Sources/UsageFeatureClassifier.swift` and the two
/// things it has to touch: the append-only attribution ledger, and the durable task registry the
/// evidence is assembled from.
///
/// It lives in its own file rather than in `Sources/Orchestrator.swift` because that file's line
/// ceiling in `tools/check-architecture-boundaries.sh` is set to the measured value with no
/// headroom, so one added line there turns the guard red.

/// Everything this pass needs to remember between calls. `SessionWatch` calls the producer on
/// every reading — the same way it calls `UsageLedger.checkpoint(sessions:)`, whose five-minute
/// cadence also lives in the callee rather than at the call site.
private enum FeatureAttributionSchedule {
    static let lock = NSLock()
    static var lastRunAt: Date?
    /// One pass per five minutes, the same cadence a usage checkpoint runs on.
    static let interval: TimeInterval = 300
    /// The bounded recent window a production pass classifies. Rung 3 asks whether a label is
    /// carried by two or more tasks *in this batch*, so the window is what "a work line" means
    /// here: long enough that the two tasks of one line are usually both inside it, short enough
    /// that the pass stays a small read.
    static let window: TimeInterval = 30 * 24 * 60 * 60
    /// A ceiling on one pass, so a large store can never turn a five-minute background tick into
    /// an unbounded scan. The reader is newest-first, so a window that fills this cap has dropped
    /// its oldest rows — and a work line whose second task fell off the end then declines as
    /// `solitary_declared_label`, a reason that would not be true. The receipt says when the cap
    /// was reached rather than leaving that reading silent.
    static let rowLimit = 5_000
}

extension UsageLedger {
    /// **What the registry says about one task now**, as against the copy a ledger row froze when
    /// that task ended.
    ///
    /// The ledger's `landing_state` is a point-in-time sample taken at `collect(taskRecord:)`,
    /// which runs when a task reaches a terminal state — and a landing is closed *after* the work
    /// ends, so the field is almost always absent at sampling time. The only thing that ever
    /// filled it in was the backfill import on launch. Measured at 11:06 on 2026-09-05 over this
    /// Mac's own store: of the tasks whose landing had closed before that launch, 79 of 79 carried
    /// the copy; of the 6 closed after it, 0 did, and two of those were sitting in the Projects
    /// page's "done, never landed" block while the broker held a verified `landed` for each. The
    /// app restarted at 11:16 with no landing record changing, and those two then read `landed`.
    ///
    /// So this is the live half of a read-time join, and the stored copy stays what it always
    /// was: history. Same shape and same locking discipline as ``UsageLedger/TaskFacts``.
    struct LiveTaskRecord: Equatable {
        /// The obligation's state right now, or nil when the record carries no landing at all.
        var landingState: String?
        /// The task's own title — what the work *was*, as against which root owned it.
        var title: String?
        /// Whether the task is still running, so one reader answers both questions from one
        /// registry snapshot.
        var isLive: Bool
        /// Whether this Mac holds no durable evidence that the task wrote to a repository, by
        /// ``Orchestrator/nothingToLandAdmission(for:)`` — the same predicate the landing route
        /// admits `nothing_to_land` by, so a row cannot advise a close the route would refuse.
        var nothingToLand: Bool

        init(landingState: String? = nil, title: String? = nil, isLive: Bool = false,
             nothingToLand: Bool = false) {
            self.landingState = landingState
            self.title = title
            self.isLive = isLive
            self.nothingToLand = nothingToLand
        }
    }

    /// What the durable broker record contributes to one row's evidence. Six fields, and no
    /// prompt, instruction body, transcript, working directory or file path among them.
    struct TaskFacts: Equatable {
        var title: String?
        var declaredWorkLine: String?
        var planHeadline: String?
        var kind: String?
        var parentTaskID: String?
        var retryOf: String?

        init(title: String? = nil, declaredWorkLine: String? = nil, planHeadline: String? = nil,
             kind: String? = nil, parentTaskID: String? = nil, retryOf: String? = nil) {
            self.title = title
            self.declaredWorkLine = declaredWorkLine
            self.planHeadline = planHeadline
            self.kind = kind
            self.parentTaskID = parentTaskID
            self.retryOf = retryOf
        }
    }

    /// One run, one receipt. The counts are what actually happened in the store, not what the
    /// classifier proposed: a second pass over the same window inserts nothing and moves the
    /// `alreadyPresent` numbers instead, which is the observable form of "this is idempotent".
    struct FeatureAttributionRun: Equatable {
        var classifierID: String
        var classifierVersion: String
        var threshold: Double
        /// False for the backfill path, which proposes without ever taking a policy decision.
        var accepting: Bool
        var scanned: Int
        var proposed: Int
        var proposalsAlreadyPresent: Int
        /// Proposal events the store **refused**: an event `valid(_:)` will not take, a database
        /// that would not open, a statement that would not prepare or step. `record(_:)` returns
        /// false for all of those and for a duplicate id alike, so a receipt that counts them as
        /// `alreadyPresent` reads byte-identically to a clean idempotent re-run — which is the
        /// one thing this receipt exists to be able to say.
        var proposalsRefused: Int
        var accepted: Int
        var acceptancesAlreadyPresent: Int
        /// Acceptance events the store refused, counted apart from the ones already present for
        /// the same reason.
        var acceptancesRefused: Int
        /// Proposals the policy left for manual review.
        var belowThreshold: Int
        /// Proposals at or above the threshold whose interval **already carries an accepted head
        /// this pass's own proposal does not supersede**. The policy appends the proposal and
        /// stops there rather than appending a rival acceptance; see `runFeatureAttribution`.
        var heldExistingAcceptedHead: Int
        /// Whether the bounded read this run classified came back holding its whole row cap. A
        /// newest-first reader cannot tell "exactly the cap" from "more than the cap", so this is
        /// the strongest honest statement: past the cap the batch loses its oldest members, and
        /// rung 3 asks whether a label is carried by two tasks *in this batch*.
        var windowTruncated: Bool
        /// `UsageFeatureClassifier.DeclineReason.rawValue` -> count. A run receipt, never a
        /// ledger field: `no_unambiguous_accepted_head` is the Portfolio's own, different word.
        var declined: [String: Int]

        init(classifierID: String = UsageFeatureClassifier.classifierID,
             classifierVersion: String = UsageFeatureClassifier.classifierVersion,
             threshold: Double = UsageFeatureClassifier.defaultAcceptanceThreshold,
             accepting: Bool = false, scanned: Int = 0, proposed: Int = 0,
             proposalsAlreadyPresent: Int = 0, proposalsRefused: Int = 0, accepted: Int = 0,
             acceptancesAlreadyPresent: Int = 0, acceptancesRefused: Int = 0,
             belowThreshold: Int = 0, heldExistingAcceptedHead: Int = 0,
             windowTruncated: Bool = false, declined: [String: Int] = [:]) {
            self.classifierID = classifierID
            self.classifierVersion = classifierVersion
            self.threshold = threshold
            self.accepting = accepting
            self.scanned = scanned
            self.proposed = proposed
            self.proposalsAlreadyPresent = proposalsAlreadyPresent
            self.proposalsRefused = proposalsRefused
            self.accepted = accepted
            self.acceptancesAlreadyPresent = acceptancesAlreadyPresent
            self.acceptancesRefused = acceptancesRefused
            self.belowThreshold = belowThreshold
            self.heldExistingAcceptedHead = heldExistingAcceptedHead
            self.windowTruncated = windowTruncated
            self.declined = declined
        }
    }

    /// Whether a Feature producer is configured on this Mac, and which one. The Portfolio reports
    /// this instead of the literal `false` it used to carry, and reports **no** threshold when
    /// nothing is applying one.
    struct FeatureClassifierState: Equatable {
        var configured: Bool
        var classifierID: String
        var classifierVersion: String
        var threshold: Double

        init(configured: Bool = false,
             classifierID: String = UsageFeatureClassifier.classifierID,
             classifierVersion: String = UsageFeatureClassifier.classifierVersion,
             threshold: Double = UsageFeatureClassifier.defaultAcceptanceThreshold) {
            self.configured = configured
            self.classifierID = classifierID
            self.classifierVersion = classifierVersion
            self.threshold = threshold
        }

        /// What every reader gets until somebody turns the classifier on, which is the truth the
        /// dashboard has been telling all along.
        static let notConfigured = FeatureClassifierState(configured: false)

        var payload: [String: Any] {
            guard configured else { return ["configured": false] }
            return ["configured": true, "id": classifierID, "version": classifierVersion,
                    "threshold": threshold]
        }
    }

    /// The production reading of the two settings keys.
    static func featureClassifierState(config: Config = .shared) -> FeatureClassifierState {
        FeatureClassifierState(configured: config.usageFeatureClassifier,
                               threshold: config.usageFeatureAcceptanceThreshold)
    }

    /// The first line of a plan, and never more of it. A plan is a design brief; what a
    /// classifier may look at is its headline.
    static func featurePlanHeadline(_ plan: String?) -> String? {
        guard let plan else { return nil }
        let headline = plan.split(whereSeparator: \.isNewline).first
            .map { String($0).trimmingCharacters(in: .whitespaces) } ?? ""
        return headline.isEmpty ? nil : String(headline.prefix(200))
    }

    /// One evidence set from rows and durable records, used by both the producer and the
    /// backfill so the two cannot drift.
    ///
    /// **The Project scope is resolved here, not in the classifier**, which stays self-contained:
    /// `acceptedProjects` is the same accepted-head map the Portfolio groups by, so a Feature and
    /// a Project answer "which Project is this row?" with one rule and one answer.
    static func featureEvidence(rows: [UsageLedger.Row], taskFacts: [String: TaskFacts],
                                scheduleTitles: [String: String],
                                acceptedProjects: [String: AcceptedAttribution] = [:])
        -> [UsageFeatureClassifier.Evidence] {
        rows.map { row in
            let taskID = featureNonEmpty(row.taskID)
            let facts = taskID.flatMap { taskFacts[$0] }
            let scheduleID = featureNonEmpty(row.scheduleID)
            return UsageFeatureClassifier.Evidence(
                intervalKey: row.intervalKey,
                projectKey: UsageFeatureClassifier.resolveProject(
                    projectKey: row.projectKey,
                    acceptedProjectIdentity: acceptedProjects[row.intervalKey]?.id).identity,
                taskID: taskID,
                hasDurableTaskRecord: facts != nil,
                taskKind: featureNonEmpty(row.kindRaw) ?? featureNonEmpty(facts?.kind),
                taskTitle: featureNonEmpty(facts?.title),
                declaredWorkLine: featureNonEmpty(facts?.declaredWorkLine),
                planHeadline: featurePlanHeadline(featureNonEmpty(facts?.planHeadline)),
                scheduleID: scheduleID,
                scheduleTitle: scheduleID.flatMap { featureNonEmpty(scheduleTitles[$0]) },
                parentTaskID: featureNonEmpty(row.parentTaskID)
                    ?? featureNonEmpty(facts?.parentTaskID),
                retryOf: featureNonEmpty(row.retryOf) ?? featureNonEmpty(facts?.retryOf))
        }
    }

    /// Deterministic and idempotent. Appends one proposal per classified interval, then — only
    /// when `accepting`, and only where the interval carries no accepted head of its own already
    /// — one `accepted` event per proposal at or above `threshold`, superseding that proposal and
    /// carrying its `valueID`.
    ///
    /// **Nothing here writes a token row.** Attribution is a second table; `usage_intervals` is
    /// never opened for writing by this path at all.
    @discardableResult
    func runFeatureAttribution(evidence: [UsageFeatureClassifier.Evidence], now: Date,
                               threshold: Double, accepting: Bool,
                               windowTruncated: Bool = false) -> FeatureAttributionRun {
        let outcome = UsageFeatureClassifier.classify(evidence)
        var run = FeatureAttributionRun(threshold: threshold, accepting: accepting,
                                        scanned: evidence.count,
                                        windowTruncated: windowTruncated)
        for proposal in outcome.proposals {
            // The proposal goes in first even when it is already there, because the acceptance
            // below names it as its predecessor and `record(_:)` refuses an acceptance whose
            // predecessor is absent. A `false` here is a duplicate event id, which is what makes
            // a re-run a no-op rather than a second opinion.
            let proposed = AttributionEvent(
                eventID: proposal.proposalEventID, intervalKey: proposal.intervalKey,
                dimension: .feature, valueID: proposal.featureID,
                valueLabel: proposal.featureLabel, source: .heuristic,
                confidence: proposal.confidence,
                classifierID: UsageFeatureClassifier.classifierID,
                classifierVersion: UsageFeatureClassifier.classifierVersion,
                evidenceDigest: proposal.evidenceDigest, decision: .proposed,
                decisionSource: "local-feature-classifier-v"
                    + UsageFeatureClassifier.classifierVersion,
                assignedAt: now, supersedesEventID: nil)
            // Three answers, not two. `record(_:)` returns false for a duplicate id *and* for an
            // event the store refused, and calling a refusal "already present" is the legacy
            // Project migration's mistake this file does not repeat.
            if record(proposed) {
                run.proposed += 1
            } else if containsAttributionEvent(proposal.proposalEventID) {
                run.proposalsAlreadyPresent += 1
            } else {
                // Nothing to accept: an acceptance names its proposal as predecessor, and
                // `record(_:)` refuses one whose predecessor is absent. Attempting it anyway
                // would report the same refusal twice.
                run.proposalsRefused += 1
                continue
            }
            guard accepting else { continue }
            guard proposal.confidence >= threshold else {
                run.belowThreshold += 1
                continue
            }
            // **A machine never overwrites an accepted head.** Event ids are derived from the
            // classifier version, the interval, the Feature id and the rung, so a version bump —
            // which §3.2 *mandates* whenever a rung, a confidence, a normalization rule or the
            // digest recipe changes — or a record that has since gained a plan headline moves the
            // seed. This pass's acceptance supersedes only its own proposal, so appending it
            // beside the head already there leaves two active accepted heads, `acceptedHead(from:)`
            // returns nil, and the interval silently leaves its Feature for `Unknown`.
            //
            // So the proposal is appended and the acceptance is held, leaving the disagreement
            // where a person can see it while the old head keeps the interval in its Feature.
            // Superseding that old head instead is not available: `record(_:)` checks a
            // `policy` + `accepted` predecessor with `requireSameValue: true`, which refuses
            // exactly when the Feature id changed — the case that matters.
            let rivals = activeAcceptedAttribution(intervalKey: proposal.intervalKey,
                                                   dimension: .feature)
                .filter { $0.eventID != proposal.acceptanceEventID }
            guard rivals.isEmpty else {
                run.heldExistingAcceptedHead += 1
                continue
            }
            let accepted = AttributionEvent(
                eventID: proposal.acceptanceEventID, intervalKey: proposal.intervalKey,
                dimension: .feature, valueID: proposal.featureID,
                valueLabel: proposal.featureLabel, source: .policy,
                confidence: proposal.confidence,
                classifierID: UsageFeatureClassifier.classifierID,
                classifierVersion: UsageFeatureClassifier.classifierVersion,
                evidenceDigest: proposal.evidenceDigest, decision: .accepted,
                decisionSource: "confidence-threshold-v1", assignedAt: now,
                supersedesEventID: proposal.proposalEventID)
            if record(accepted) {
                run.accepted += 1
            } else if containsAttributionEvent(proposal.acceptanceEventID) {
                run.acceptancesAlreadyPresent += 1
            } else {
                run.acceptancesRefused += 1
            }
        }
        for decline in outcome.declined {
            run.declined[decline.reason.rawValue, default: 0] += 1
        }
        return run
    }

    /// The backfill. Proposals only, so a historical range can be classified without a policy
    /// decision ever being taken on old rows.
    @discardableResult
    func backfillFeatureProposals(evidence: [UsageFeatureClassifier.Evidence], now: Date)
        -> FeatureAttributionRun {
        runFeatureAttribution(evidence: evidence, now: now,
                              threshold: UsageFeatureClassifier.defaultAcceptanceThreshold,
                              accepting: false)
    }

    /// The production producer, called from `SessionWatch` beside the usage checkpoint.
    ///
    /// **Does nothing at all when the setting is off**, which is the default: until somebody
    /// turns it on the dashboard keeps saying that automatic attribution is not configured, and
    /// that stays true.
    ///
    /// It must not run inside a `queue.sync` block. `analyticsRead` and `record(_:)` each take
    /// that queue, and re-entering it deadlocks the process — which is what `exit 133` was here.
    @discardableResult
    static func classifyFeaturesIfConfigured(now: Date = Date()) -> FeatureAttributionRun? {
        let state = featureClassifierState()
        guard state.configured else { return nil }
        FeatureAttributionSchedule.lock.lock()
        let last = FeatureAttributionSchedule.lastRunAt
        let due = last.map { now.timeIntervalSince($0) >= FeatureAttributionSchedule.interval }
            ?? true
        if due { FeatureAttributionSchedule.lastRunAt = now }
        FeatureAttributionSchedule.lock.unlock()
        guard due else { return nil }
        let reading = shared.analyticsRead(AnalyticsFilter(
            start: now.addingTimeInterval(-FeatureAttributionSchedule.window),
            limit: FeatureAttributionSchedule.rowLimit, includeFeatureAttribution: false))
        guard !reading.rows.isEmpty else { return nil }
        let evidence = featureEvidence(rows: reading.rows,
                                       taskFacts: Orchestrator.usageFeatureTaskFacts(),
                                       scheduleTitles: Orchestrator.usageScheduleLabels(),
                                       acceptedProjects: reading.acceptedProjects)
        return shared.runFeatureAttribution(
            evidence: evidence, now: now, threshold: state.threshold, accepting: true,
            windowTruncated: reading.rows.count >= FeatureAttributionSchedule.rowLimit)
    }

    static func featureNonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else { return nil }
        return value
    }
}

extension Orchestrator {
    /// The six durable facts the Feature classifier may see, one entry per known task record.
    /// Same shape and same locking discipline as ``Orchestrator/usageScheduleLabels()``: load the
    /// registry, take one snapshot under the lock, and do every projection outside it.
    static func usageFeatureTaskFacts() -> [String: UsageLedger.TaskFacts] {
        load()
        lock.lock()
        let snapshots = Array(tasks.values)
        lock.unlock()
        var facts: [String: UsageLedger.TaskFacts] = [:]
        for task in snapshots {
            facts[task.id] = UsageLedger.TaskFacts(
                title: UsageLedger.featureNonEmpty(task.title),
                declaredWorkLine: UsageLedger.featureNonEmpty(task.rootLabel),
                planHeadline: UsageLedger.featurePlanHeadline(task.plan),
                kind: UsageLedger.featureNonEmpty(task.kind),
                parentTaskID: UsageLedger.featureNonEmpty(task.parentTaskId),
                retryOf: UsageLedger.featureNonEmpty(task.respawnOf))
        }
        return facts
    }

    /// **The live half of the Projects read's join**, one entry per task the registry still
    /// holds: its landing obligation, its title, whether it is running, and whether anything it
    /// wrote is on record.
    ///
    /// Absence means the registry no longer holds that task — it is capped and swept — not that
    /// the task never landed. A reader that finds no entry here must fall back to the row's own
    /// frozen copy and say which of the two it is showing; ``UsageProjectWorktreeService`` does
    /// both. `isLive` is the half of the liveness answer that is safe to trust in the other
    /// direction too: the registry holds every live task, so one it does not hold is certainly
    /// not running.
    ///
    /// Same shape and same locking discipline as ``Orchestrator/usageFeatureTaskFacts()`` above,
    /// and it lives beside it for the same reason that one is not in `Sources/Orchestrator.swift`.
    static func usageLiveTaskRecords() -> [String: UsageLedger.LiveTaskRecord] {
        // Both stores are read outside the other's lock, and the declared write sets are read
        // once for the whole snapshot rather than once per task.
        let retained = OrchestratorLandingQueue.retainedLandingPaths()
        load()
        lock.lock()
        let snapshots = Array(tasks.values)
        lock.unlock()
        var records: [String: UsageLedger.LiveTaskRecord] = [:]
        for task in snapshots {
            records[task.id] = UsageLedger.LiveTaskRecord(
                landingState: task.landing?.state.rawValue,
                title: UsageLedger.featureNonEmpty(task.title),
                isLive: !task.state.isTerminal,
                nothingToLand: nothingToLandAdmission(
                    for: task,
                    declaredWritePaths: OrchestratorLandingQueue.landingPaths(
                        of: task, retainedPaths: retained)).isAdmitted)
        }
        return records
    }
}
