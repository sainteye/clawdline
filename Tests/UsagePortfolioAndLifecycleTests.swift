import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

/// One analytics row without going through a collector. Portfolio tests hold the observed subject
/// still and vary only the reader; this deliberately says nothing about collection behavior.
private func analyticsRow(_ key: String, at: Date, assistant: String = "claude",
                          model: String? = "claude-opus-5",
                          project: String? = "/private/acme/widget",
                          counts: UsageLedger.Counts = .init(inputNew: 1, output: 2,
                                                             cacheRead: 3, cacheWrite: 4),
                          cost: Double? = nil, unit: String? = nil,
                          basis: String = "unknown", missing: String? = "no_cost_recorded",
                          coverage: String = "complete",
                          reasons: [String] = []) -> UsageLedger.Row {
    var row = UsageLedger.Row()
    row.intervalKey = key
    row.assistant = assistant
    row.sessionID = "private-session-\(key)"
    row.boundaryKind = "task"
    row.boundaryID = "task-\(key)"
    row.taskID = "task-\(key)"
    row.origin = "dispatch"
    row.projectKey = project
    row.workingDir = project.map { $0 + "/checkout" }
    row.model = model
    row.counts = counts
    row.total = counts.total
    row.costValue = cost
    row.costUnit = unit
    row.costBasis = basis
    row.priceSnapshotID = cost == nil ? nil : UsageLedger.priceSnapshotID
    row.missingReason = missing
    row.coverage = coverage
    row.coverageReasons = reasons
    row.startedAt = at
    row.updatedAt = at.addingTimeInterval(30)
    row.localDay = UsageLedger.localDay(of: at)
    row.sealed = true
    return row
}

/// Who is in the shared ledger right now, said in the failure rather than left to be guessed.
/// `UsageLedger.shared` is process-wide, so a group asserting on `rows()` or on an unfiltered
/// portfolio is asserting about every writer in the process. When one of those assertions goes
/// red the only question worth answering is *whose row is this*, and a bare check name cannot.
private func ledgerWitness() -> String {
    UsageLedger.shared.rows().map {
        "[\($0.assistant) session=\($0.sessionID) boundary=\($0.boundaryKind):\($0.boundaryID) "
            + "origin=\($0.origin) project=\($0.projectKey ?? "?") "
            + "output=\($0.counts.output ?? -1) coverage=\($0.coverage)]"
    }.joined(separator: " ")
}

func runUsagePortfolioAndLifecycleTests() {
group("usage portfolio ranks canonical projects and keeps every unavailable dimension honest") {
    let current = ISO8601DateFormatter().date(from: "2026-08-20T12:00:00Z")!
    let previous = ISO8601DateFormatter().date(from: "2026-08-18T12:00:00Z")!
    var alphaRoot = analyticsRow("alpha-root", at: current,
                                 project: "/private/team-a/widget",
                                 counts: .init(inputNew: 20, output: 100,
                                               cacheRead: 80, cacheWrite: 0),
                                 cost: 1, unit: "USD", basis: "provider_actual", missing: nil)
    alphaRoot.boundaryKind = "session"
    alphaRoot.boundaryID = alphaRoot.sessionID
    alphaRoot.taskID = nil
    alphaRoot.origin = "manual"
    alphaRoot.depth = nil
    var alphaRootSegment = analyticsRow("alpha-root-segment",
                                        at: current.addingTimeInterval(30),
                                        project: "/private/team-a/widget",
                                        counts: .init(inputNew: 2, output: 10,
                                                      cacheRead: 8, cacheWrite: 0),
                                        cost: 0.1, unit: "USD", basis: "provider_actual",
                                        missing: nil)
    alphaRootSegment.boundaryKind = "session"
    alphaRootSegment.sessionID = alphaRoot.sessionID
    alphaRootSegment.boundaryID = alphaRoot.boundaryID
    alphaRootSegment.taskID = nil
    alphaRootSegment.origin = "manual"
    alphaRootSegment.depth = nil
    var alphaChild = analyticsRow("alpha-child", at: current.addingTimeInterval(60),
                                  project: "/private/team-a/widget",
                                  counts: .init(inputNew: 10, output: 40,
                                                cacheRead: 90, cacheWrite: 0),
                                  cost: 0.5, unit: "USD", basis: "provider_actual", missing: nil)
    alphaChild.depth = Orchestrator.depthFloor
    alphaChild.parentTaskID = "task-alpha-root"
    var alphaSchedule = analyticsRow("alpha-schedule", at: current.addingTimeInterval(90),
                                     project: "/private/team-a/widget",
                                     counts: .init(inputNew: 4, output: 20,
                                                   cacheRead: 6, cacheWrite: 0),
                                     cost: 0.2, unit: "USD", basis: "provider_actual",
                                     missing: nil)
    alphaSchedule.depth = Orchestrator.depthFloor
    alphaSchedule.origin = "schedule"
    alphaSchedule.scheduleID = "nightly-health"
    var beta = analyticsRow("beta", at: current.addingTimeInterval(120),
                            project: "/private/team-b/widget",
                            counts: .init(inputNew: 30, output: 220,
                                          cacheRead: 70, cacheWrite: 0))
    beta.depth = nil
    var unknown = analyticsRow("unknown-project", at: current.addingTimeInterval(180),
                               project: nil,
                               counts: .init(inputNew: 5, output: nil,
                                             cacheRead: 15, cacheWrite: 0),
                               coverage: "partial", reasons: ["source_regressed"])
    unknown.depth = nil
    var alphaPrevious = analyticsRow("alpha-previous", at: previous,
                                     project: "/private/team-a/widget",
                                     counts: .init(inputNew: 10, output: 70,
                                                   cacheRead: 20, cacheWrite: 0),
                                     cost: 0.8, unit: "USD", basis: "provider_actual",
                                     missing: nil)
    alphaPrevious.depth = 1
    var betaPrevious = analyticsRow("beta-previous", at: previous,
                                    project: "/private/team-b/widget",
                                    counts: .init(inputNew: 20, output: 250,
                                                  cacheRead: 60, cacheWrite: 0))
    betaPrevious.depth = 1

    let payload = UsageQueryService(rows: {
        [alphaRoot, alphaRootSegment, alphaChild, alphaSchedule, beta, unknown,
         alphaPrevious, betaPrevious]
    }, scheduleLabels: { ["nightly-health": "Nightly health"] }).query(
        .init(from: "2026-08-20", to: "2026-08-21", timezoneID: "UTC"),
             now: current.addingTimeInterval(300)).payload
    let portfolio = payload["portfolio"] as? [String: Any]
    check("the complete Portfolio projection is JSON serializable",
          JSONSerialization.isValidJSONObject(payload))
    let projects = portfolio?["projects"] as? [[String: Any]] ?? []
    expect("three canonical Projects plus Unknown are present", projects.count, 3)
    expect("Projects are ranked by generated output, not input or arrival",
           projects.compactMap { $0["output"] as? Int }, [220, 170])
    let widgets = projects.filter { $0["label"] as? String == "widget" }
    check("same-basename repositories remain separate canonical Projects",
          widgets.count == 2 && Set(widgets.compactMap { $0["id"] as? String }).count == 2)
    let alpha = widgets.first { $0["output"] as? Int == 170 }
    expect("session segments deduplicate on stable boundary identity", alpha?["runs"] as? Int, 3)
    expect("scheduled contribution is named on the Project", alpha?["scheduledRuns"] as? Int, 1)
    let alphaLineage = alpha?["lineage"] as? [String: Any]
    check("production-reachable root, child and scheduled roles stay distinct",
          alphaLineage?["status"] as? String == "available"
            && alphaLineage?["rootRuns"] as? Int == 1
            && alphaLineage?["childRuns"] as? Int == 1
            && alphaLineage?["scheduledRuns"] as? Int == 1)
    let betaProject = widgets.first { $0["output"] as? Int == 220 }
    expect("missing depth is unavailable rather than called root",
           (betaProject?["lineage"] as? [String: Any])?["status"] as? String,
           "unavailable")
    let alphaCost = alpha?["cost"] as? [String: Any]
    check("one fully covered cost series is comparable",
          alphaCost?["status"] as? String == "available"
            && abs((alphaCost?["value"] as? Double ?? 0) - 1.8) < 0.000_001
            && alphaCost?["unit"] as? String == "USD"
            && alphaCost?["basis"] as? String == "provider_actual"
            && alphaCost?["assistant"] as? String == "claude")
    let alphaChange = alpha?["comparison"] as? [String: Any]
    check("equal adjacent local-day ranges produce a real output delta",
          alphaChange?["status"] as? String == "comparable"
            && alphaChange?["absolute"] as? Int == 100
            && alphaChange?["previous"] as? Int == 70)
    let schedules = ((portfolio?["scheduledWork"] as? [String: Any])?["schedules"]
        as? [[String: Any]]) ?? []
    check("Scheduled Work is aggregated by explicit schedule identity",
          schedules.count == 1 && schedules.first?["id"] as? String == "nightly-health"
            && schedules.first?["label"] as? String == "Nightly health"
            && schedules.first?["runs"] as? Int == 1
            && schedules.first?["output"] as? Int == 20)
    let scheduledWork = portfolio?["scheduledWork"] as? [String: Any]
    check("Scheduled KPI metadata keeps measured output beside Unknown contribution",
          scheduledWork?["output"] as? Int == 20
            && scheduledWork?["runs"] as? Int == 1
            && scheduledWork?["unknownOutputRuns"] as? Int == 0)
    let unattributedFeatures = portfolio?["features"] as? [String: Any]
    check("an empty Feature table says automatic attribution is not configured",
          unattributedFeatures?["status"] as? String == "no_accepted_attribution"
            && unattributedFeatures?["automaticAttribution"] as? Bool == false)
    let unknownProject = projects.first { $0["id"] as? String == "unknown-project" }
    check("Unknown Project and unknown output stay visibly partial",
          unknownProject?["output"] is NSNull
            && (unknownProject?["coverage"] as? [String: Any])?["status"] as? String
                == "partial")

    let unbounded = UsageQueryService(rows: { [alphaRoot] })
        .query(.init(timezoneID: "UTC"), now: current).payload
    expect("comparison without a closed current range says why it is unavailable",
           ((unbounded["portfolio"] as? [String: Any])?["comparison"]
                as? [String: Any])?["reason"] as? String,
           "closed_range_required")

    var pricedClaude = analyticsRow("claude-spend", at: current,
                                    cost: 1.25, unit: "USD",
                                    basis: "provider_actual", missing: nil)
    pricedClaude.depth = Orchestrator.depthFloor
    var unpricedCodex = analyticsRow("codex-plan", at: current, assistant: "codex",
                                    model: "gpt-5.6-sol")
    unpricedCodex.depth = Orchestrator.depthFloor
    unpricedCodex.missingReason = "plan_billed"
    let mixedProject = ((UsageQueryService(rows: { [pricedClaude, unpricedCodex] })
        .query(.init(timezoneID: "UTC")).payload["portfolio"] as? [String: Any])?["projects"]
        as? [[String: Any]])?.first
    let mixedSpend = mixedProject?["cost"] as? [String: Any]
    check("unpriced Codex usage does not hide Claude Code's estimated spending",
          mixedSpend?["status"] as? String == "available"
            && mixedSpend?["assistant"] as? String == "claude"
            && abs((mixedSpend?["value"] as? Double ?? 0) - 1.25) < 0.000_001)
}

group("legacy managed-worktree Projects migrate through auditable append-only evidence") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let fixture = FileManager.default.temporaryDirectory
        .appendingPathComponent("usage-project-migration-\(UUID().uuidString)")
    let repository = fixture.appendingPathComponent("repository")
    let taskID = UUID().uuidString.lowercased()
    let worktree = fixture.appendingPathComponent("Clawdline/worktrees/repository-fixture")
        .appendingPathComponent(taskID)
    try! FileManager.default.createDirectory(at: repository, withIntermediateDirectories: true)
    try! FileManager.default.createDirectory(at: worktree.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: fixture) }
    func git(_ arguments: [String]) -> Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = arguments
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do { try process.run() } catch { return false }
        process.waitQuietly()
        return process.terminationStatus == 0
    }
    check("migration fixture repository initializes", git(["init", "-q", repository.path]))
    check("migration fixture receives an accepted head",
          git(["-C", repository.path, "-c", "user.name=Usage Test",
               "-c", "user.email=usage@example.invalid", "commit", "-q", "--allow-empty",
               "-m", "fixture"]))
    check("migration fixture creates a real managed worktree",
          git(["-C", repository.path, "worktree", "add", "-q", "-b", "migration-fixture",
               worktree.path]))

    // **This group's three portfolio assertions used to be about the whole process.** They read
    // `projects.first` out of an unranged query, and `UsageLedger.shared` is process-wide: what
    // that returned was whichever project had generated the most output anywhere in this binary,
    // not this fixture's. A row left in the store by another writer between `freshUsageLedger()`
    // and the query took first place and all three went red — the same shape as
    // `B-LEDGER-QUIET-WINDOW-IS-NOT-QUIET`, and measured the same way: on one binary, four full
    // runs, two red and two green, with nothing changed between them.
    //
    // The subject is pinned instead of the window widened. A legacy row *is* historical, so the
    // fixture observes on a fixed day and every read below asks for that closed range. Rows any
    // other writer leaves behind are written at `Date()` and fall outside it, so the answer no
    // longer depends on the process being quiet — and the assertions say `count == 1`, which is
    // that isolation asserting itself rather than being assumed.
    let observedAt = ISO8601DateFormatter().date(from: "2026-08-20T12:00:00Z")!
    func migrationDay(project: String? = nil) -> [String: Any] {
        UsageQueryService().query(.init(from: "2026-08-19", to: "2026-08-21",
                                        timezoneID: "UTC", project: project)).payload
    }
    func migrationProjects(_ payload: [String: Any]) -> [[String: Any]] {
        (payload["portfolio"] as? [String: Any])?["projects"] as? [[String: Any]] ?? []
    }
    var sample = ledgerSample(.codex, session: "migration-session", boundary: .task,
                              id: "migration-task", origin: .dispatch,
                              usage: ["input_tokens": 1, "output_tokens": 12,
                                      "cached_input_tokens": 0],
                              model: "gpt-5.6-sol", at: observedAt)
    sample.taskID = "migration-task"
    sample.projectKey = worktree.path
    sample.depth = Orchestrator.depthFloor
    sample.seal = true
    let interval = UsageLedger.shared.observeNow(sample)
    let row = UsageLedger.shared.rows(taskID: "migration-task").first
    check("legacy Project fixture is a stored interval", interval != nil && row != nil)

    let liveProof = UsageLedger.liveWorktreeProjectMigrationEvidence(projectKey: worktree.path)
    check("a live worktree is resolved only through Git common-dir proof",
          liveProof?.repositoryRoot == repository.path
            && liveProof?.source == .gitCommonDirectory
            && liveProof?.evidenceDigest.count == 64)
    check("the fixture worktree can be removed before durable-receipt proof",
          git(["-C", repository.path, "worktree", "remove", "--force", worktree.path]))
    let receipt: [[String: Any]] = [[
        "id": taskID, "project_dir": repository.path,
        "worktree": ["path": worktree.path, "cwd": worktree.path,
                     "repository": repository.path, "base": "fixture-head"],
    ]]
    let receiptProof = UsageLedger.taskReceiptProjectMigrationEvidence(
        projectKey: worktree.path, taskRecords: receipt)
    check("a deleted worktree resolves only through an exact durable task receipt",
          receiptProof?.repositoryRoot == repository.path
            && receiptProof?.source == .taskWorktreeReceipt)
    let mismatched = UsageLedger.taskReceiptProjectMigrationEvidence(
        projectKey: worktree.path,
        taskRecords: [["id": taskID, "project_dir": repository.path,
                       "worktree": ["path": worktree.path + "-other",
                                    "cwd": worktree.path, "repository": repository.path]]])
    check("a basename, UUID, or mismatched receipt never guesses a repository", mismatched == nil)

    let plan = UsageLedger.planLegacyProjectMigration(
        rows: [row!], evidence: [receiptProof!], assignedAt: Date())
    check("the dry-run manifest resolves one row through a deterministic accepted Project chain",
          plan.events.count == 2 && plan.audit.count == 1
            && plan.audit.first?.status == "resolved"
            && plan.events.first?.decision == .proposed
            && plan.events.last?.decision == .accepted
            && plan.events.last?.supersedesEventID == plan.events.first?.eventID
            && JSONSerialization.isValidJSONObject(plan.payload))
    let beforeProjects = migrationProjects(migrationDay())
    check("before apply the disposable UUID is suppressed into Unknown Project",
          beforeProjects.count == 1
            && beforeProjects.first?["label"] as? String == "Unknown Project"
            && !String(describing: beforeProjects).contains(taskID),
          "projects \(beforeProjects.map { ($0["label"] ?? "?", $0["output"] ?? "?") }) "
            + "— ledger \(ledgerWitness())")

    let backup = String(repeating: "a", count: 64)
    let first = UsageLedger.shared.applyLegacyProjectMigration(plan, backupDigest: backup)
    let second = UsageLedger.shared.applyLegacyProjectMigration(plan, backupDigest: backup)
    check("apply requires a backup receipt and reruns without side effects",
          first == .init(applied: 2, alreadyPresent: 0, failed: 0, backupDigest: backup)
            && second == .init(applied: 0, alreadyPresent: 2, failed: 0,
                              backupDigest: backup))
    let afterProjects = migrationProjects(migrationDay())
    check("accepted migration attribution changes Project identity without rewriting tokens",
          afterProjects.count == 1
            && afterProjects.first?["label"] as? String == repository.lastPathComponent
            && afterProjects.first?["output"] as? Int == 12
            && UsageLedger.shared.rows(taskID: "migration-task").first?.projectKey
                == worktree.path,
          "projects \(afterProjects.map { ($0["label"] ?? "?", $0["output"] ?? "?") }) "
            + "— ledger \(ledgerWitness())")
    let filteredAfter = migrationDay(project: repository.lastPathComponent)
    check("the bounded Project filter sees accepted migration identity instead of the legacy key",
          (filteredAfter["rowCount"] as? Int) == 1
            && ((filteredAfter["portfolio"] as? [String: Any])?["projects"]
                as? [[String: Any]])?.first?["label"] as? String
                == repository.lastPathComponent)

    let rollback = UsageLedger.shared.rollbackLegacyProjectMigration(
        plan, backupDigest: backup, assignedAt: Date())
    let rolledProjects = migrationProjects(migrationDay())
    check("append-only rollback returns the legacy row to Unknown",
          rollback.applied == 1
            && rolledProjects.count == 1
            && rolledProjects.first?["label"] as? String == "Unknown Project",
          "applied \(rollback.applied) — projects "
            + "\(rolledProjects.map { ($0["label"] ?? "?", $0["output"] ?? "?") }) "
            + "— ledger \(ledgerWitness())")

    var unresolved = row!
    unresolved.intervalKey = "unresolved-legacy-row"
    unresolved.projectKey = worktree.deletingLastPathComponent()
        .appendingPathComponent(UUID().uuidString.lowercased()).path
    let unresolvedPlan = UsageLedger.planLegacyProjectMigration(
        rows: [unresolved], evidence: [], assignedAt: Date())
    check("evidence-free deleted rows remain explicit unresolved audit entries",
          unresolvedPlan.events.isEmpty
            && unresolvedPlan.audit.first?.status == "unresolved"
            && unresolvedPlan.audit.first?.reason == "migration_evidence_missing")
}

group("usage portfolio accepts exactly one Feature head and leaves proposals or conflicts unknown") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let at = ISO8601DateFormatter().date(from: "2026-08-20T12:00:00Z")!
    func interval(_ name: String) -> String {
        var sample = ledgerSample(.claude, session: "feature-\(name)", boundary: .task,
                                  id: "task-\(name)", origin: .dispatch,
                                  usage: ["input": 1, "output": 10, "cache_read": 0,
                                          "cache_write": 0, "total": 11],
                                  model: "claude-opus-5", at: at)
        sample.taskID = "task-\(name)"
        sample.projectKey = "/private/acme/portfolio"
        sample.depth = 1
        sample.seal = true
        return UsageLedger.shared.observeNow(sample)!
    }
    func event(_ id: String, _ interval: String, value: String,
               decision: UsageLedger.AttributionDecision,
               supersedes: String? = nil) -> UsageLedger.AttributionEvent {
        UsageLedger.AttributionEvent(
            eventID: id, intervalKey: interval, dimension: .feature,
            valueID: value, valueLabel: value == "portfolio" ? "Usage Portfolio" : "Other",
            source: .manual, confidence: nil, classifierID: nil, classifierVersion: nil,
            evidenceDigest: nil, decision: decision, decisionSource: "focused-test",
            assignedAt: at, supersedesEventID: supersedes)
    }
    let accepted = interval("accepted")
    let acceptedOther = interval("accepted-other")
    let proposed = interval("proposed")
    let conflicted = interval("conflicted")
    check("accepted fixture records", UsageLedger.shared.record(
        event("accepted-head", accepted, value: "portfolio", decision: .accepted)))
    check("equal-output accepted fixture records", UsageLedger.shared.record(
        event("accepted-other-head", acceptedOther, value: "other", decision: .accepted)))
    check("proposal fixture records", UsageLedger.shared.record(
        event("proposal-head", proposed, value: "portfolio", decision: .proposed)))
    check("first conflict head records", UsageLedger.shared.record(
        event("conflict-a", conflicted, value: "portfolio", decision: .accepted)))
    check("second conflict head records", UsageLedger.shared.record(
        event("conflict-b", conflicted, value: "other", decision: .accepted)))

    let payload = UsageQueryService().query(
        .init(from: "2026-08-20", to: "2026-08-20", timezoneID: "UTC"), now: at).payload
    let features = ((payload["portfolio"] as? [String: Any])?["features"]
        as? [String: Any])
    let groups = features?["groups"] as? [[String: Any]] ?? []
    check("accepted Features use a stable id tie-break when output is equal",
          groups.compactMap { $0["id"] as? String } == ["other", "portfolio"]
            && groups.allSatisfy { $0["runs"] as? Int == 1 })
    let unknown = features?["unknown"] as? [String: Any]
    check("proposal-only and conflicting accepted heads remain Unknown Feature",
          unknown?["runs"] as? Int == 2
            && unknown?["reason"] as? String == "no_unambiguous_accepted_head")
    let acceptedEvents = [event("accepted-head", accepted, value: "portfolio",
                                decision: .accepted)]
    check("the store and analytics share the one accepted-head implementation",
          UsageLedger.acceptedHead(from: acceptedEvents)?.valueID
            == UsageLedger.shared.resolvedAttribution(intervalKey: accepted,
                                                       dimension: .feature)?.valueID)
}

group("a 60k-row SQLite-to-DTO usage query stays bounded and measured") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let url = store.appendingPathComponent("usage.sqlite3")
    _ = UsageLedger.shared.rows() // create and migrate the production schema first
    let inserted = usageStoreExec(url, """
        BEGIN IMMEDIATE;
        WITH RECURSIVE counter(n) AS (
          VALUES(0) UNION ALL SELECT n + 1 FROM counter WHERE n < 59999
        )
        INSERT INTO usage_intervals
          (interval_key, schema_version, assistant, session_id, boundary_kind, boundary_id,
           segment_no, segment_reason, origin, project_key, model, billing_mode,
           input_new, output, cache_read, cache_write, total,
           cost_value, cost_unit, cost_basis, price_snapshot_id, missing_reason,
           coverage, sealed, started_at, local_day, observed_at, updated_at)
        SELECT printf('perf-%05d', n), 1,
               CASE WHEN n % 2 = 0 THEN 'claude' ELSE 'codex' END,
               printf('session-%05d', n), 'task', printf('task-%05d', n), 0, 'start',
               'dispatch', '/private/perf/project-' || (n % 20),
               CASE WHEN n % 2 = 0 THEN 'claude-opus-5' ELSE 'gpt-5.6-sol' END,
               CASE WHEN n % 2 = 0 THEN 'unknown' ELSE 'plan' END,
               1, 2, 3, 4, 10,
               CASE WHEN n % 3 = 0 THEN 0.001 ELSE NULL END,
               CASE WHEN n % 3 = 0 THEN 'USD' ELSE NULL END,
               CASE WHEN n % 3 = 0 THEN 'list_price_estimate' ELSE 'unknown' END,
               CASE WHEN n % 3 = 0 THEN '\(UsageLedger.priceSnapshotID)' ELSE NULL END,
               CASE WHEN n % 3 = 0 THEN NULL ELSE 'plan_billed' END,
               'complete', 1, 1800000000.0 + n, '2027-01-15',
               1800000000.0 + n, 1800000000.0 + n
          FROM counter;
        COMMIT;
        """)
    check("the performance fixture inserted all rows in one transaction", inserted)
    let started = Date()
    let result = UsageQueryService().query(.init(from: "2027-01-15", to: "2027-01-16",
                                                  timezoneID: "UTC", groupBy: .model,
                                                  bucket: .day, limit: 50),
                                           now: Date(timeIntervalSince1970: 1_800_100_000))
    let seconds = Date().timeIntervalSince(started)
    expect("the measured query read all 60k rows", result.payload["rowCount"] as? Int, 60_000)
    expect("while returning only the bounded page", result.rows.count, 50)
    check("without reaching the explicit scan ceiling", !result.scanTruncated)
    expect("the shipped closed-range path performs the previous read",
           ((result.payload["portfolio"] as? [String: Any])?["comparison"]
                as? [String: Any])?["reason"] as? String,
           "no_previous_data")
    check("and completes inside a generous interactive ceiling", seconds < 10,
          String(format: "%.3f seconds", seconds))
    print(String(format: "USAGE_ANALYTICS_PERF rows=60000 seconds=%.3f subject=sqlite_to_dto",
                 seconds))
}

group("the SQLite analytics bound belongs to the complete matching query") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let url = store.appendingPathComponent("usage.sqlite3")
    _ = UsageLedger.shared.rows()
    let old = ISO8601DateFormatter().date(from: "2026-01-15T00:00:00Z")!
        .timeIntervalSince1970
    let newer = ISO8601DateFormatter().date(from: "2026-03-15T00:00:00Z")!
        .timeIntervalSince1970
    let inserted = usageStoreExec(url, """
        BEGIN IMMEDIATE;
        WITH RECURSIVE counter(n) AS (
          VALUES(0) UNION ALL SELECT n + 1 FROM counter WHERE n < 119999
        )
        INSERT INTO usage_intervals
          (interval_key, schema_version, assistant, session_id, boundary_kind, boundary_id,
           segment_no, segment_reason, origin, project_key, model, billing_mode,
           input_new, output, cache_read, cache_write, total, cost_basis, missing_reason,
           coverage, sealed, started_at, local_day, observed_at, updated_at)
        SELECT printf('bound-%06d', n), 1, CASE WHEN n % 2 = 0 THEN 'claude' ELSE 'codex' END,
               printf('session-%06d', n), 'task', printf('task-%06d', n), 0, 'start',
               CASE WHEN n % 3 = 0 THEN 'schedule' ELSE 'dispatch' END,
               '/private/bounded/project-' || (n % 4), 'model-' || (n % 2), 'unknown',
               1, 2, 3, 4, 10, 'unknown', 'no_cost_recorded', 'complete', 1,
               CASE WHEN n < 20000 THEN \(old) + n ELSE \(newer) + n END,
               CASE WHEN n < 20000 THEN '2026-01-15' ELSE '2026-03-15' END,
               CASE WHEN n < 20000 THEN \(old) + n ELSE \(newer) + n END,
               CASE WHEN n < 20000 THEN \(old) + n ELSE \(newer) + n END
          FROM counter;
        INSERT INTO usage_corrections(interval_key, reason, proposed, written_at)
          VALUES ('bound-000001', 'old-range', '{}', \(old)),
                 ('bound-110000', 'new-range', '{}', \(newer));
        COMMIT;
        """)
    check("the 120k fixture inserted through the production SQLite schema", inserted)

    let oldMonth = UsageQueryService().query(
        .init(from: "2026-01-01", to: "2026-01-31", timezoneID: "UTC", limit: 20),
        now: Date(timeIntervalSince1970: newer + 120_000))
    expect("an old narrow month outside the newest global 100k remains visible",
           oldMonth.payload["rowCount"] as? Int, 20_000)
    check("that narrow matching query clears truncation", !oldMonth.scanTruncated)
    expect("corrections are scoped to the same requested subject",
           oldMonth.payload["corrections"] as? Int, 1)
    let fullyFiltered = UsageQueryService().query(
        .init(from: "2026-01-01", to: "2026-01-31", timezoneID: "UTC",
              assistant: "claude", model: "model-0", origin: "schedule",
              project: "project-0", limit: 20))
    expect("assistant/model/origin/project are all applied inside the bounded SQLite query",
           fullyFiltered.payload["rowCount"] as? Int, 1_667)
    check("the complete narrow filter does not inherit whole-store truncation",
          !fullyFiltered.scanTruncated)
    expect("top-level freshness describes the newest ledger observation",
           (oldMonth.payload["freshness"] as? [String: Any])?["status"] as? String,
           "current")
    expect("the historical range reports its own data-through separately",
           (oldMonth.payload["rangeFreshness"] as? [String: Any])?["status"] as? String,
           "historical")

    let all = UsageQueryService().query(.init(timezoneID: "UTC", limit: 20))
    check("more than 100k matching rows is explicitly partial", all.scanTruncated)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let refused = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics.csv?timezone=UTC", headers: auth))
    check("an over-bound matching export is a typed 413",
          refused.status == 413 && remoteErrorCode(refused) == "export_too_large")
    let narrowed = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics.csv?timezone=UTC&from=2026-01-01&to=2026-01-31",
        headers: auth))
    check("narrowing the matching range makes the export available",
          narrowed.status == 200 && narrowed.headers["Content-Type"] == "text/csv; charset=utf-8")
}

group("usage exports are spreadsheet-safe and JSON-lossless") {
    let at = Date(timeIntervalSince1970: 1_800_000_000)
    var dangerous = analyticsRow("=WEBSERVICE(\"https://bad\")", at: at,
                                 model: "+cmd|' /C calc'!A0",
                                 project: "/private/customer/@evil")
    dangerous.endedAt = at.addingTimeInterval(30)
    dangerous.sourceTotal = 10
    dangerous.reconciliation = "parts_match"
    dangerous.inputBasis = "exclusive"
    let service = UsageQueryService(rows: { [dangerous] })
    let result = service.query(.init(timezoneID: "UTC", limit: 10), now: at)
    let csv = service.exportCSV(result)
    check("every spreadsheet formula introducer is neutralized",
          csv.contains("'=WEBSERVICE") && csv.contains("'+cmd") && csv.contains("'@evil"), csv)
    check("safe CSV omits raw filesystem and session data",
          !csv.contains("/private/") && !csv.contains("private-session"), csv)
    check("safe CSV keeps non-sensitive row reconciliation facts",
          csv.hasPrefix("interval_id,task_id,started_at,ended_at,")
            && csv.contains("unknown_token_parts,source_total,reconciliation,input_basis")
            && csv.contains("parts_match,exclusive"), csv.prefix(500).description)
    let exported = try! service.exportJSON(result)
    let object = (try? JSONSerialization.jsonObject(with: exported)) as? [String: Any]
    let row = (object?["rows"] as? [[String: Any]])?.first
    check("JSON preserves null separately from a measured zero",
          (row?["strictTotal"] as? Int) == 10 && row?["cost"] is NSNull)
    check("JSON export declares that it is complete",
          object?["truncated"] as? Bool == false && object?["rowCount"] as? Int == 1)

    let failing = UsageQueryService(rows: { [dangerous] }, jsonEncoder: { _ in nil })
    let failingResult = failing.query(.init(timezoneID: "UTC"), now: at)
    do {
        _ = try failing.exportJSON(failingResult)
        check("JSON serialization failure is never an empty successful file", false)
    } catch UsageQueryService.ExportError.jsonSerialization {
        check("JSON serialization failure becomes a typed failure", true)
    } catch {
        check("JSON serialization failure has the expected type", false, "\(error)")
    }
}

group("usage analytics routes keep authentication, validation and privacy aligned") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    UsageLedger.shared.importTaskRecord([
        "id": "route-private", "assistant": "claude", "state": "success", "kind": "code",
        "project_dir": "/private/customer/secret-project", "timeout_minutes": 30, "depth": 1,
        "child_session": "secret-conversation-id",
        "usage": ["input": 1, "output": 2, "cache_read": 3, "cache_write": 4,
                  "total": 10, "model": "claude-opus-5"],
        "finished_at": Date().timeIntervalSince1970,
    ])
    expect("anonymous analytics remains behind the read gate",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/usage/analytics")).status, 401)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    for invalid in ["?timezone=Mars%2FOlympus", "?limit=0", "?group=model&group=task",
                    "?unknown=true"] {
        let response = RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/usage/analytics" + invalid, headers: auth))
        check("invalid analytics query is typed 400: \(invalid)",
              response.status == 400 && remoteErrorCode(response) == "bad_request")
    }
    let response = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics?timezone=UTC&limit=1", headers: auth))
    let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    let usage = object?["usage"] as? [String: Any]
    check("the JSON reading route carries the complete metadata contract",
          response.status == 200 && usage?["freshness"] is [String: Any]
            && usage?["capabilities"] is [String: Any]
            && usage?["coverage"] is [String: Any]
            && usage?["unavailableDimensions"] is [String: Any])
    let download = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics.json?timezone=UTC", headers: auth))
    let downloadText = String(decoding: download.body, as: UTF8.self)
    check("the JSON export is authenticated, downloadable and path-free",
          download.status == 200 && download.headers["Content-Disposition"]?.contains(".json") == true
            && !downloadText.contains("/private/")
            && !downloadText.contains("secret-conversation-id"), downloadText.prefix(300).description)
    let csv = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics.csv?timezone=UTC", headers: auth))
    let csvText = String(decoding: csv.body, as: UTF8.self)
    check("the CSV route uses the same private projection",
          csv.status == 200 && csv.headers["Content-Type"] == "text/csv; charset=utf-8"
            && !csvText.contains("/private/") && !csvText.contains("secret-conversation-id"),
          csvText.prefix(300).description)

    let legacyJSON = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage?group=model", headers: auth))
    let legacyObject = ((try? JSONSerialization.jsonObject(with: legacyJSON.body))
        as? [String: Any])?["usage"] as? [String: Any]
    check("the legacy forensic JSON keeps its aggregate schema on its old URL",
          legacyJSON.status == 200 && legacyObject?["groups"] is [[String: Any]]
            && legacyObject?["freshness"] == nil)
    let legacyUnavailable = (legacyObject?["unavailable"] as? [String: Any])?["columns"]
        as? [String]
    let analyticsUnavailable = usage?["unavailableDimensions"] as? [String: Any]
    check("legacy and Portfolio capability surfaces share the Feature availability answer",
          legacyUnavailable == ["graph_id", "disposition"]
            && analyticsUnavailable?["dimensions"] as? [String] == legacyUnavailable
            && analyticsUnavailable?["featureView"] as? Bool == true)
    let legacyCSV = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage.csv", headers: auth))
    check("the legacy forensic CSV route is byte-compatible with the ledger exporter",
          legacyCSV.body == Data(UsageLedger.shared.exportCSV().utf8))
    check("the public safe CSV lives only on the explicit analytics URL",
          csvText.hasPrefix("interval_id,task_id,started_at,ended_at,")
            && String(decoding: legacyCSV.body, as: UTF8.self)
                .hasPrefix(UsageLedger.exportColumns.joined(separator: ",")))
    UsageQueryService.jsonEncoderForTesting = { _ in nil }
    defer { UsageQueryService.jsonEncoderForTesting = nil }
    let failedJSON = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/analytics.json?timezone=UTC", headers: auth))
    check("route-level JSON serialization failure is typed 500, never 200 empty",
          failedJSON.status == 500 && remoteErrorCode(failedJSON) == "json_serialization_failed")
}

group("the shipped page contains the accessible Usage Project Portfolio") {
    let page = try! String(contentsOfFile: "Resources/web/index.html", encoding: .utf8)
    let css = try! String(contentsOfFile: "Resources/web/app/css/usage.css", encoding: .utf8)
    let script = try! String(contentsOfFile: "Resources/web/app/js/view/usage.js",
                             encoding: .utf8)
    for evidence in ["usage-analytics", "usage-overview", "usage-agent-work", "usage-range",
                     "usage-timezone", "usage-project-list", "usage-project-detail",
                     "usage-schedule-body", "usage-feature-body", "usage-insights",
                     "usage-coverage-panel", "usage-detail", "usage-export-csv",
                     "usage-export-json"] {
        check("the analytics DOM contains #\(evidence)", page.contains("id=\"\(evidence)\""))
    }
    check("the ranked Project surface is a semantic table with a named primary signal",
          page.contains("<caption>Project portfolio</caption>")
            && page.contains("role=\"table\"")
            && page.contains("role=\"columnheader\">Generated output</th>"))
    check("the page carries explicit 1280px, 390px and 320px proofs",
          css.contains("@media (max-width: 1280px)")
            && css.contains("@media (max-width: 390px)")
            && css.contains("@media (max-width: 320px)"))
    check("partial state and ledger-versus-range freshness are visible",
          page.contains("id=\"usage-availability\"")
            && script.contains("Partial result:")
            && script.contains("Ledger freshness:")
            && script.contains("Range data through:"))
    check("exports fetch first and build a Blob only after a 2xx response",
          script.contains("if (!response.ok) return errorFrom(response);")
            && script.contains("return response.blob();")
            && script.contains("URL.createObjectURL(blob)"))
    check("tabs use roving tabindex and arrow keys while view changes the request",
          script.contains("event.key !== \"ArrowLeft\"")
            && script.contains("tabIndex = overview ? 0 : -1")
            && script.contains("query.set(\"view\", state.view)"))
    check("sub-cent costs use significant digits instead of two-decimal zero",
          script.contains("maximumSignificantDigits: 6")
            && script.contains("significant(cost.value)"))
    check("loading lifecycle queues refreshes instead of dropping them",
          script.contains("state.pending = { cursor: cursor, append: append }")
            && script.contains("Refresh queued…"))
    check("refresh errors mark the retained range stale rather than relabelling old data",
          script.contains("Showing stale data for ")
            && script.contains("setAttribute(\"data-stale\", \"true\")"))
    let ledgerSource = try! String(contentsOfFile: "Sources/UsageLedger.swift", encoding: .utf8)
    check("root Session collection resolves the actual working directory before attribution",
          ledgerSource.contains("cwd: Targets.workingDirectory(of: session)")
            && !ledgerSource.contains("terminalID: session.id, cwd: session.cwd"))

    let mutationGuard = Process()
    mutationGuard.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    mutationGuard.arguments = ["node", "Tests/web-usage-analytics.mjs"]
    let mutationOutput = Pipe()
    mutationGuard.standardOutput = mutationOutput
    mutationGuard.standardError = mutationOutput
    do {
        try mutationGuard.run()
        let output = String(decoding: mutationOutput.fileHandleForReading.readDataToEndOfFile(),
                            as: UTF8.self)
        mutationGuard.waitQuietly()
        check("the permanent web guard executes real behavior and named mutations",
              mutationGuard.terminationStatus == 0
                && output.range(
                    of: #"web usage portfolio guards: [1-9][0-9]* assertions executed"#,
                    options: .regularExpression) != nil, output)
    } catch {
        check("the permanent web mutation guard starts", false, "\(error)")
    }
}

group("the ledger agrees with the registry and the route, row by row on one task id") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let id = "01999e7f-aaaa-4bbb-8ccc-dddddddddddd"
    var task = Orchestrator.Task(id: id, state: .success, kind: "code",
                                 title: "reconciliation fixture", assistant: .claude,
                                 projectDir: "/tmp/project", timeoutMinutes: 30, created: Date(),
                                 secretHash: String(repeating: "0", count: 64))
    task.childSessionId = "sess-reconcile"
    task.finishedAt = Date()
    var usage = Orchestrator.Usage()
    usage.input = 2
    usage.output = 3
    usage.cacheRead = 160_655
    usage.cacheWrite = 5
    usage.total = 160_665
    usage.model = "claude-opus-5"
    usage.costUsd = 5.469
    task.usage = usage
    Orchestrator.holdScheduleTaskForTesting(task)

    // Three surfaces, one fixed task id. Comparing totals, or comparing "the first row with
    // usage" from two places, is how both the disk-versus-HTTP mechanism and its correction were
    // got wrong in one afternoon: an unaligned comparison looks like an experiment and is not one.
    let onDisk = OrchestratorStore.stored(task)["usage"] as? [String: Any] ?? [:]
    let overHTTP = Orchestrator.recordForTesting(task)["usage"] as? [String: Any] ?? [:]
    check("the two surfaces really do spell this differently",
          onDisk["cache_read"] != nil && overHTTP["cacheRead"] != nil
            && onDisk["cacheRead"] == nil && overHTTP["cache_read"] == nil)

    expect("the record imports", UsageLedger.shared.importTaskRecord(OrchestratorStore.stored(task)),
           true)
    let rows = UsageLedger.shared.rows(taskID: id)
    expect("as exactly one row", rows.count, 1)
    let stored = (rows.first?.rawUsage).flatMap {
        (try? JSONSerialization.jsonObject(with: Data($0.utf8))) as? [String: Any]
    } ?? [:]
    let fromDisk = UsageLedger.normalize(raw: onDisk, assistant: .claude)
    let fromHTTP = UsageLedger.normalize(raw: overHTTP, assistant: .claude)
    let fromLedger = UsageLedger.normalize(raw: stored, assistant: .claude)
    expect("this task's usage is the same on disk and in the ledger",
           fromLedger.counts, fromDisk.counts)
    expect("and the same over HTTP", fromLedger.counts, fromHTTP.counts)
    expect("with the cache read intact rather than dropped by a spelling",
           fromLedger.counts.cacheRead, 160_655)
    expect("and the stored row holding the same number", rows.first?.counts.cacheRead, 160_655)
    expect("and the same cost on all three", [fromDisk.cost, fromHTTP.cost, fromLedger.cost],
           [5.469, 5.469, 5.469])
    expect("and the row's own copy of it", rows.first?.costValue, 5.469)
}

group("a sealed row is corrected rather than rewritten") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let record: [String: Any] = ["id": "task-sealed", "assistant": "claude", "state": "success",
                                 "kind": "code", "project_dir": "/tmp", "timeout_minutes": 30,
                                 "depth": 1, "child_session": "sess-sealed",
                                 "usage": ["input": 1, "output": 2, "cache_read": 3,
                                           "cache_write": 4, "total": 10,
                                           "model": "claude-opus-5"]]
    UsageLedger.shared.importTaskRecord(record)
    expect("the finished task is sealed", UsageLedger.shared.rows().first?.sealed, true)
    expect("importing the identical record again changes nothing",
           UsageLedger.shared.importTaskRecord(record), false)
    expect("and leaves one row", UsageLedger.shared.rows().count, 1)

    var later = record
    later["usage"] = ["input": 1, "output": 2, "cache_read": 3_000, "cache_write": 4,
                      "total": 3_007, "model": "claude-opus-5"]
    expect("a disagreeing re-measurement is accepted",
           UsageLedger.shared.importTaskRecord(later), true)
    expect("as new metadata", UsageLedger.shared.correctionCount(), 1)
    expect("and never as a rewrite of a number a month's total may already carry",
           UsageLedger.shared.rows().first?.total, 10)
    expect("the aggregate reports that a correction is outstanding",
           UsageLedger.shared.aggregate().corrections, 1)
}

group("each kind of work is counted exactly once, and the routes answer for the range") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    func usage(_ total: Int, model: String = "claude-opus-5") -> [String: Any] {
        ["input": total, "output": 0, "cache_read": 0, "cache_write": 0, "total": total,
         "model": model]
    }

    // A hand-opened session, watched.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "hand", usage: usage(100),
                                               model: "claude-opus-5"))
    // An ordinary task in a tab this app opened, seen by the watcher first and then finalized.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "sess-ordinary",
                                               boundary: .task, id: "task-ordinary",
                                               origin: .dispatch, usage: usage(300),
                                               model: "claude-opus-5"))
    UsageLedger.shared.importTaskRecord([
        "id": "task-ordinary", "assistant": "claude", "state": "success", "kind": "code",
        "project_dir": "/tmp", "timeout_minutes": 30, "depth": 1,
        "child_session": "sess-ordinary",
        "usage": usage(300),
    ])
    // A scheduled task.
    UsageLedger.shared.importTaskRecord([
        "id": "task-scheduled", "assistant": "claude", "state": "success", "kind": "custom",
        "project_dir": "/tmp", "timeout_minutes": 30, "depth": 1,
        "child_session": "sess-scheduled", "schedule_id": "nightly",
        "usage": usage(50),
    ])
    // A follow-up attached into a session that was already being watched.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "hand", boundary: .task,
                                               id: "task-attached", origin: .followUp,
                                               usage: usage(180), model: "claude-opus-5"))
    UsageLedger.shared.importTaskRecord([
        "id": "task-attached", "assistant": "claude", "state": "success", "kind": "code",
        "project_dir": "/tmp", "timeout_minutes": 30, "depth": 1, "child_session": "hand",
        "attach_session": "standing-tab",
        "usage": usage(180),
    ])

    let byOrigin = UsageLedger.shared.aggregate(groupBy: .origin)
    func total(_ origin: String) -> Int? {
        byOrigin.groups.first { $0.key == origin }?.bucket.total
    }
    expect("the hand-opened session is counted once", total("manual"), 100)
    expect("the ordinary task once, by whichever collector saw it first",
           total("dispatch"), 300)
    expect("the scheduled task once", total("schedule"), 50)
    expect("and the attached follow-up only its own increment", total("follow_up"), 80)
    expect("so the whole machine's spend is the sum of them and nothing more",
           byOrigin.totals.total, 530)

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let answered = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage?group=origin", headers: auth))
    expect("the legacy forensic aggregate route answers", answered.status, 200)
    let body = ((try? JSONSerialization.jsonObject(with: answered.body)) as? [String: Any])?[
        "usage"] as? [String: Any]
    expect("under the grouping it was asked for", body?["groupBy"] as? String, "origin")
    expect("with the same measured total",
           ((body?["totals"] as? [String: Any])?["total"]) as? Int, 530)
    expect("and it names the columns it has no answer for",
           (body?["unavailable"] as? [String: Any])?["columns"] as? [String],
           ["graph_id", "disposition"])
    expect("the export answers as CSV",
           RemoteServer.shared.route(
            remoteRequest("GET", "/v1/orchestrator/usage.csv", headers: auth))
            .headers["Content-Type"], "text/csv; charset=utf-8")
    let exported = String(decoding: RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage.csv", headers: auth)).body, as: UTF8.self)
    expect("one header and one line per row",
           exported.split(separator: "\n").count, 1 + UsageLedger.shared.rows().count)
    expect("a range outside every row is empty rather than wrong",
           UsageLedger.shared.aggregate(from: "2001-01-01", to: "2001-01-02").totals.rows, 0)
    expect("a group nobody defined is refused", RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage?group=nonsense", headers: auth)).status, 400)
    expect("and so is a range bound that is not a local day", RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage?from=yesterday", headers: auth)).status, 400)
    expect("an unpaired caller with no orchestrator token is refused",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/usage")).status, 401)
}

group("finalize files a task in the ledger without being told anybody heard about it") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let id = "77779e7f-1111-4222-8333-444444444444"
    var task = Orchestrator.Task(id: id, state: .briefed, kind: "code", title: "finalize fixture",
                                 assistant: .codex, projectDir: "/tmp/project",
                                 timeoutMinutes: 30, created: Date(),
                                 secretHash: String(repeating: "0", count: 64))
    task.childSessionId = "sess-finalize"
    var usage = Orchestrator.Usage()
    usage.input = 8_190_546
    usage.output = 16_956
    usage.cacheRead = 7_978_752
    usage.total = 8_207_502
    usage.model = "gpt-5.6-sol"
    task.usage = usage
    Orchestrator.holdScheduleTaskForTesting(task)
    Orchestrator.finalize(id, as: .success, summary: "done")

    check("the ledger has the row shortly after the task ends",
          eventually { UsageLedger.shared.rows(taskID: id).count == 1 })
    let row = UsageLedger.shared.rows(taskID: id).first
    expect("with the state it ended in", row?.taskState, "success")
    expect("sealed, because the work is over", row?.sealed, true)
    expect("and the Codex cache kept out of the new input", row?.counts.inputNew, 211_794)
    expect("with the parts summing back to the source's total", row?.total, 8_207_502)
    check("and no dollar figure invented for a plan-billed assistant", row?.costValue == nil)
    expect("naming that as the reason", row?.missingReason, "plan_billed")
    expect("the ledger's own copy is a task boundary", row?.boundaryKind, "task")
    expect("filed under the session the work ran in", row?.sessionID, "sess-finalize")
}

group("a row with one part unknown keeps its unknown through every reader") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }

    // Three parts measured, one absent. Not reachable through today's two collectors — both hand
    // over an `Orchestrator.Usage` whose four fields are non-optional — and reachable the moment
    // anything hands `normalize` a source object directly, which is what the archived registry
    // snapshot does. This is the `1137M tokens, $0.00` shape with a different field in it.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "partial",
                                               usage: ["input": 500, "output": 250],
                                               model: "claude-opus-5"))
    let row = UsageLedger.shared.rows().first
    let measurement = row?.measurement
    expect("the seam keeps the parts that were measured", measurement?.counts.inputNew, 500)
    check("and leaves the one that was not unknown", measurement?.counts.cacheRead == nil)
    check("it refuses a total it cannot compute", measurement?.total == nil)
    expect("while still saying what was measured", measurement?.measured, 750)
    check("it is not the same thing as a row nobody could read",
          measurement?.unknown == false && measurement?.incomplete == true)
    expect("and it names which parts are missing", measurement?.unknownParts,
           [UsageLedger.Part.cacheRead, .cacheWrite])

    let aggregate = UsageLedger.shared.aggregate()
    check("the aggregate never renders the unknown part as a zero",
          aggregate.totals.tokens?.cacheRead == nil,
          "got \(String(describing: aggregate.totals.tokens?.cacheRead))")
    expect("it keeps the parts that were measured", aggregate.totals.tokens?.inputNew, 500)
    expect("it does not drop the row's measured tokens out of the total",
           aggregate.totals.total, 750)
    expect("and it says the row could not be fully counted",
           aggregate.totals.tokenRowsUnknown, 1)
    expect("naming the column that is short, and on how many rows",
           aggregate.totals.partsUnknown["cacheRead"], 1)

    let totals = UsageLedger.payload(of: aggregate)["totals"] as? [String: Any]
    let tokens = totals?["tokens"] as? [String: Any]
    check("the wire says null where nothing was measured", tokens?["cacheRead"] is NSNull,
          "got \(String(describing: tokens?["cacheRead"]))")
    expect("and carries the measured part beside it", tokens?["inputNew"] as? Int, 500)
    expect("with the short column named on the wire too",
           (totals?["tokenPartsUnknown"] as? [String: Int])?["cacheWrite"], 1)

    // The CSV was already honest here. It stays honest because it asks the same type, not
    // because it remembers to.
    let csv = UsageLedger.shared.exportCSV()
    let columns = (csv.split(separator: "\n").first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    let values = (csv.split(separator: "\n").dropFirst().first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    func field(_ name: String) -> String? {
        guard let index = columns.firstIndex(of: name), index < values.count else { return nil }
        return values[index]
    }
    expect("the export leaves the unknown part empty", field("cache_read"), "")
    expect("keeps the measured one", field("input_new"), "500")
    expect("and refuses the total, exactly as the aggregate does", field("total"), "")
    expect("while carrying the lower bound the route reports, which the strict total is not",
           field("measured"), "750")

    // Sorting is a reader too: a row three-quarters measured biases a total the same way a row
    // nobody could read does, only by less.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "whole",
                                               usage: ["input": 1, "output": 1, "cache_read": 1,
                                                       "cache_write": 1, "total": 4],
                                               model: "claude-opus-5"))
    expect("a partly measured row still sorts above a fully measured one",
           UsageLedger.shared.rows().first?.sessionID, "partial")

    // **An export has to add up to the number the route gave for the same range.** The seam
    // made the aggregate's total a measured lower bound and left the CSV's strict — one range,
    // two quantities, and no column in the file that could reproduce the other. A figure that
    // cannot be reconciled with the figure beside it is what this store exists to stop, so the
    // export carries both and this is the arithmetic that says so.
    let whole = UsageLedger.shared.exportCSV()
    let wholeColumns = (whole.split(separator: "\n").first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    let measuredColumn = wholeColumns.firstIndex(of: "measured")
    let exportedMeasured = whole.split(separator: "\n").dropFirst().compactMap { line -> Int? in
        guard let measuredColumn else { return nil }
        let cells = line.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
        guard measuredColumn < cells.count else { return nil }
        return Int(cells[measuredColumn])
    }
    expect("every row that measured something says how much", exportedMeasured.count, 2)
    expect("and the export sums to exactly the total the route publishes",
           exportedMeasured.reduce(0, +), UsageLedger.shared.aggregate().totals.total)

    // **The ranking has to move a row for this to be a guard at all.** With the incomplete rows
    // arriving first, any reader that returned them in the order the store holds them would pass
    // — which is what happened when the ordering stopped being decided in the `ORDER BY` and the
    // assertion above went on passing. So: one more row that could not measure everything,
    // arriving last, which has to be lifted over the row that measured all four.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "partial-late",
                                               usage: ["input": 7, "output": 3],
                                               model: "claude-opus-5"))
    expect("a row that measured less is lifted over one that measured all four, whenever it came",
           UsageLedger.shared.rows().map(\.sessionID), ["partial", "partial-late", "whole"])
}

group("a session the store had to name for itself says so on the wire") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int) -> [String: Any] {
        ["input": input, "output": output, "cache_read": cacheRead, "cache_write": 0,
         "total": input + output + cacheRead]
    }

    // A session already being watched, and then a task record that ran inside it without ever
    // naming it. The synthetic identity gets its own cursor, so the same cumulative counters are
    // attributed a second time — a bounded gap the delivery documented, and one that reached the
    // route as an ordinary, healthy-looking number because `payload(of:)` had no field for it.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "real", usage: usage(100, 100, 800),
                                               model: "claude-opus-5"))
    UsageLedger.shared.importTaskRecord([
        "id": "orphan", "assistant": "claude", "state": "success", "kind": "code",
        "project_dir": "/tmp", "timeout_minutes": 30, "depth": 1,
        "usage": usage(100, 100, 800), "model": "claude-opus-5",
        "finished_at": Date().timeIntervalSince1970,
    ])
    let invented = UsageLedger.shared.rows(taskID: "orphan").first
    check("a task whose session nobody knew is filed under an invented identity",
          invented?.sessionID.hasPrefix(UsageLedger.unresolvedSessionPrefix) == true,
          invented?.sessionID ?? "no row")
    let aggregate = UsageLedger.shared.aggregate()
    expect("which the aggregate reports as a reason, not as a healthy row",
           aggregate.totals.coverageReasons["session_unresolved"], 1)
    let payload = UsageLedger.payload(of: aggregate)
    let json = String(decoding: (try? JSONSerialization.data(withJSONObject: payload,
                                                            options: [.sortedKeys])) ?? Data(),
                      as: UTF8.self)
    check("and which is therefore visible to whoever reads the route",
          json.contains("session_unresolved"), json.prefix(400).description)

    // Read off the row rather than trusted to whoever wrote it: the mark has to survive at the
    // reader, which is the only place every consumer passes through.
    var unwritten = UsageLedger.Row()
    unwritten.sessionID = UsageLedger.unresolvedSessionPrefix + "task-x"
    unwritten.counts = UsageLedger.Counts(inputNew: 1, output: 1, cacheRead: 1, cacheWrite: 1)
    expect("even a row nobody remembered to annotate reports the invented identity",
           unwritten.measurement.reasons, ["session_unresolved"])

    // And it is *added* to what the row already says. Reading it back only for an otherwise
    // unmarked row is how the mark underneath disappeared: the number spans a replaced source
    // and the session was invented, and a consumer needs both to know what it is holding.
    var alsoRotated = unwritten
    alsoRotated.coverageReasons = ["source_regressed"]
    expect("without displacing the mark the row already carried",
           alsoRotated.measurement.reasons, ["source_regressed", "session_unresolved"])
}

group("a source that went backwards re-anchors instead of quietly under-counting") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int) -> [String: Any] {
        ["input": input, "output": output, "cache_read": cacheRead, "cache_write": 0,
         "total": input + output + cacheRead]
    }

    // 950 on one transcript; then the same session id answering out of a rotated or rewritten
    // file that starts near zero; then that replacement genuinely growing to 1300. Never
    // subtract — but never keep measuring against a high-water mark the new source will not
    // reach, because everything it records below that mark simply disappears.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "rot", usage: usage(100, 50, 800),
                                               model: "claude-opus-5"))
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "rot", usage: usage(1, 1, 1),
                                               model: "claude-opus-5"))
    expect("the reading that went backwards is never subtracted",
           UsageLedger.shared.rows().first?.total, 950)
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "rot",
                                               usage: usage(200, 100, 1000),
                                               model: "claude-opus-5"))
    expect("and the replacement's growth is counted from where it was re-anchored",
           UsageLedger.shared.rows().first?.total, 950 + 1_297)
    expect("the row says the number was measured across a seam",
           UsageLedger.shared.rows().first?.coverageReasons, ["source_regressed"])
    let aggregate = UsageLedger.shared.aggregate()
    expect("and so does the aggregate, which used to report it as an ordinary session",
           aggregate.totals.coverageReasons["source_regressed"], 1)
    let totals = UsageLedger.payload(of: aggregate)["totals"] as? [String: Any]
    expect("on the wire, beside the coverage that says nothing about it",
           (totals?["coverageReasons"] as? [String: Int])?["source_regressed"], 1)
}

group("a row marked twice keeps both marks, and both reach every reader") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    UsageLedger.forgetWatchedForTesting()
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int) -> [String: Any] {
        ["input": input, "output": output, "cache_read": cacheRead, "cache_write": 0,
         "total": input + output + cacheRead]
    }

    // A. A task neither collector could name a session for, whose source is then replaced. Every
    // import of it is marked `session_unresolved`, in the same call that marks the regression —
    // so with one slot `source_regressed` never survived its own `apply()` even once. The total
    // below spans a source that was swapped out, and nothing anywhere said so.
    func orphan(_ raw: [String: Any]) -> [String: Any] {
        ["id": "orphan-rotated", "assistant": "claude", "state": "briefed", "kind": "code",
         "project_dir": "/tmp", "timeout_minutes": 30, "depth": 1, "usage": raw,
         "model": "claude-opus-5"]
    }
    UsageLedger.shared.importTaskRecord(orphan(usage(100, 100, 800)))
    UsageLedger.shared.importTaskRecord(orphan(usage(1, 1, 1)))
    UsageLedger.shared.importTaskRecord(orphan(usage(200, 100, 1_200)))
    let rotated = UsageLedger.shared.rows(taskID: "orphan-rotated").first
    check("the invented identity is on the row",
          rotated?.sessionID.hasPrefix(UsageLedger.unresolvedSessionPrefix) == true,
          rotated?.sessionID ?? "no row")
    expect("and its number spans the source that was replaced", rotated?.total, 1_000 + 1_497)
    expect("so the row carries both marks, in the order they were decided",
           rotated?.coverageReasons, ["session_unresolved", "source_regressed"])
    expect("and the seam hands a reader both of them",
           rotated?.measurement.reasons, ["session_unresolved", "source_regressed"])

    let aggregate = UsageLedger.shared.aggregate()
    expect("the aggregate counts the invented identity",
           aggregate.totals.coverageReasons["session_unresolved"], 1)
    expect("and the replaced source, on that same one row",
           aggregate.totals.coverageReasons["source_regressed"], 1)
    expect("which is one row counted under two marks, never two rows", aggregate.totals.rows, 1)
    let wire = (UsageLedger.payload(of: aggregate)["totals"] as? [String: Any])?["coverageReasons"]
        as? [String: Int]
    check("and both travel to whoever reads the route",
          wire?["session_unresolved"] == 1 && wire?["source_regressed"] == 1,
          "\(wire ?? [:])")

    let csv = UsageLedger.shared.exportCSV()
    let columns = (csv.split(separator: "\n").first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    let values = (csv.split(separator: "\n").dropFirst().first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    func field(_ name: String) -> String? {
        guard let index = columns.firstIndex(of: name), index < values.count else { return nil }
        return values[index]
    }
    expect("and the export carries both in one field, never only the newest",
           field("coverage_reasons"), "session_unresolved source_regressed")

    // B. A watched session that rotated, and then could not be read at the moment it closed.
    // Two stages of one accident: the close used to overwrite the rotation with its own word,
    // and a row read across a seam went back to looking like an ordinary unreadable session.
    let now = Date()
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "rot-closed",
                                               usage: usage(100, 50, 800),
                                               model: "claude-opus-5"))
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "rot-closed",
                                               usage: usage(1, 1, 1), model: "claude-opus-5"))
    expect("the rotation is on the row first",
           UsageLedger.shared.rows().first { $0.sessionID == "rot-closed" }?.coverageReasons,
           ["source_regressed"])
    UsageLedger.remember(terminalID: "TERM-ROT-CLOSED", assistant: .claude,
                         sessionID: "rot-closed",
                         record: store.appendingPathComponent("rot-closed.jsonl"),
                         bytes: nil, at: now)
    UsageLedger.departed(["TERM-ROT-CLOSED"], now: now)
    check("and the close adds its own mark rather than replacing it",
          eventually {
              UsageLedger.shared.rows().first { $0.sessionID == "rot-closed" }?.coverageReasons
                  == ["source_regressed", "source_unreadable_at_close"]
          },
          "\(UsageLedger.shared.rows().first { $0.sessionID == "rot-closed" }?.coverageReasons ?? [])")
    let closed = UsageLedger.shared.rows().first { $0.sessionID == "rot-closed" }
    expect("sealed with no readable source", closed?.coverage, "source_missing")
    expect("keeping the number it had measured before that source went", closed?.total, 950)
    expect("and the aggregate reports both stages of it",
           UsageLedger.shared.aggregate().totals
               .coverageReasons["source_unreadable_at_close"], 1)
    expect("beside the rotation it used to erase", UsageLedger.shared.aggregate().totals
               .coverageReasons["source_regressed"], 2)
}

group("a store written under version 1 is upgraded in place, and never orphaned") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let url = store.appendingPathComponent("usage.sqlite3")
    let today = UsageLedger.localDay(of: Date())
    writeVersionOneUsageStore(at: url, today: today)
    expect("the fixture is a version 1 store before anything opens it",
           usageStoreScalar(url, "PRAGMA user_version;"), "1")

    // Opening it through the ledger's own door is what runs the migration.
    let rows = UsageLedger.shared.rows()
    expect("both stored rows survive the upgrade", rows.count, 2)
    expect("and the store says which version it is now",
           usageStoreScalar(url, "PRAGMA user_version;"), String(UsageLedger.storeVersion))

    let marked = UsageLedger.shared.rows(taskID: "task-v1").first
    expect("a v1 row keeps the number it was written with", marked?.total, 1000)
    expect("under the key it was written with", marked?.intervalKey, versionOneMarkedKey)
    expect("which is still the key today's code computes for that identity",
           UsageLedger.intervalKey(assistant: .claude, sessionID: "sess-v1", boundaryKind: .task,
                                   boundaryID: "task-v1", segmentNo: 0),
           versionOneMarkedKey)
    expect("and the mark it was written with reaches every reader as a set of one",
           marked?.coverageReasons, ["source_regressed"])
    check("the column added by the upgrade is NULL on a row written before it existed",
          marked?.inputBasis == nil, marked?.inputBasis ?? "not nil")
    expect("a v1 row that measured nothing still measures nothing",
           UsageLedger.shared.rows(taskID: "task-v1-gone").first?.counts,
           UsageLedger.Counts())
    expect("and carries no mark it never had",
           UsageLedger.shared.rows(taskID: "task-v1-gone").first?.coverageReasons, [])

    expect("the single-mark column is gone rather than left behind to be read",
           usageStoreScalar(url, """
               SELECT COUNT(*) FROM pragma_table_info('usage_intervals')
                WHERE name = 'coverage_reason';
               """), "0")
    expect("replaced by the set that carries the same values",
           usageStoreScalar(url, """
               SELECT COUNT(*) FROM pragma_table_info('usage_intervals')
                WHERE name = 'coverage_reasons';
               """), "1")

    expect("six identical corrections written under v1 are de-duplicated to three",
           UsageLedger.shared.correctionCount(), 3)
    expect("and the index that stops them coming back exists",
           usageStoreScalar(url, """
               SELECT name FROM sqlite_master
                WHERE type = 'index' AND name = 'usage_corrections_once';
               """), "usage_corrections_once")

    // The upgraded store is still a store: the next reading lands on the row that was already
    // there rather than opening a second one beside it, which is what a changed key recipe or a
    // lost cursor would look like — and it looks like a quiet doubling, not like an error.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "sess-v1", boundary: .task,
                                               id: "task-v1", origin: .dispatch,
                                               usage: ["input": 20, "output": 20,
                                                       "cache_read": 1_960, "cache_write": 0,
                                                       "total": 2_000],
                                               model: "claude-opus-5"))
    expect("a later reading lands on the v1 row rather than beside it",
           UsageLedger.shared.rows(taskID: "task-v1").count, 1)
    expect("carrying its increment and nothing else",
           UsageLedger.shared.rows(taskID: "task-v1").first?.total, 2_000)
    expect("with the mark it was migrated with still on it",
           UsageLedger.shared.rows(taskID: "task-v1").first?.coverageReasons,
           ["source_regressed"])
}

group("the backfill files work that happened, and re-importing it changes nothing") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int) -> [String: Any] {
        ["input": input, "output": output, "cache_read": cacheRead, "cache_write": 0,
         "total": input + output + cacheRead]
    }

    // A queued task: no session, no counters, nothing observed. A row for it describes work that
    // was *anticipated*, and it does not stay harmless — it is a permanent unmeasured row that
    // the real row later joins, and it drowns the coverage view it pollutes. Five queued tasks
    // beside one genuinely unreadable session read as six unmeasured rows.
    var record: [String: Any] = ["id": "task-q", "assistant": "claude", "state": "queued",
                                 "kind": "code", "project_dir": "/tmp", "timeout_minutes": 30,
                                 "depth": 1]
    for _ in 1...3 { UsageLedger.shared.importTaskRecord(record) }
    check("a task that has never run leaves no row at all",
          UsageLedger.shared.rows().isEmpty,
          "\(UsageLedger.shared.rows().map(\.sessionID))")
    expect("and the import says it wrote nothing", UsageLedger.shared.importTaskRecord(record),
           false)

    record["state"] = "success"
    record["child_session"] = "sess-q"
    record["usage"] = usage(10, 10, 480)
    record["finished_at"] = Date().timeIntervalSince1970
    expect("the same task, once it has actually run, is one row",
           UsageLedger.shared.importTaskRecord(record), true)
    expect("and only one", UsageLedger.shared.rows(taskID: "task-q").count, 1)
    expect("with no phantom left to explain on the coverage view",
           UsageLedger.shared.aggregate().totals.tokenRowsUnknown, 0)

    // A record that has a session but no counters yet is a different thing: something was
    // observed, so the skeleton row the spec asks for is exactly right.
    UsageLedger.shared.importTaskRecord(["id": "task-live", "assistant": "claude",
                                         "state": "briefed", "kind": "code",
                                         "project_dir": "/tmp", "timeout_minutes": 30,
                                         "depth": 1, "child_session": "sess-live"])
    expect("a running task whose session is known still opens its row",
           UsageLedger.shared.rows(taskID: "task-live").count, 1)
}

group("a correction is written once, however many times the evidence is re-read") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int) -> [String: Any] {
        ["input": input, "output": output, "cache_read": cacheRead, "cache_write": 0,
         "total": input + output + cacheRead]
    }

    // Sealed at one number by whichever collector closed it, and the registry's own harvest read
    // one round more. An ordinary interleaving, not a corner: `SessionWatch` seals on process
    // disappearance while finalize is still reading.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: "sess-i", boundary: .task,
                                               id: "task-i", origin: .dispatch,
                                               usage: usage(10, 10, 480),
                                               model: "claude-opus-5"))
    var close = ledgerSample(.claude, session: "sess-i", boundary: .task, id: "task-i",
                             origin: .dispatch, usage: usage(20, 20, 960),
                             model: "claude-opus-5")
    close.seal = true
    close.sealCoverage = .complete
    UsageLedger.shared.observeNow(close)
    let record: [String: Any] = ["id": "task-i", "assistant": "claude", "state": "success",
                                 "kind": "code", "project_dir": "/tmp", "timeout_minutes": 30,
                                 "depth": 1, "child_session": "sess-i",
                                 "usage": usage(21, 21, 990),
                                 "finished_at": Date().timeIntervalSince1970]
    expect("the first re-measurement that disagrees is a correction",
           UsageLedger.shared.importTaskRecord(record), true)
    for _ in 1...3 { UsageLedger.shared.importTaskRecord(record) }
    expect("and four identical passes leave exactly one",
           UsageLedger.shared.correctionCount(), 1)
    expect("which is what the route publishes as a correction outstanding",
           UsageLedger.shared.aggregate().corrections, 1)
    expect("a later pass says it wrote nothing", UsageLedger.shared.importTaskRecord(record),
           false)

    // The case that could never converge: a row sealed with no readable source has no stored
    // object to compare against, so every launch found it changed.
    var gone = ledgerSample(.claude, session: "sess-gone", boundary: .task, id: "task-gone",
                            origin: .dispatch, usage: nil, model: "claude-opus-5")
    gone.seal = true
    gone.sealCoverage = .sourceMissing
    UsageLedger.shared.observeNow(gone)
    let recovered: [String: Any] = ["id": "task-gone", "assistant": "claude", "state": "success",
                                    "kind": "code", "project_dir": "/tmp",
                                    "timeout_minutes": 30, "depth": 1,
                                    "child_session": "sess-gone", "usage": usage(1, 2, 3),
                                    "finished_at": Date().timeIntervalSince1970]
    for _ in 1...4 { UsageLedger.shared.importTaskRecord(recovered) }
    expect("a sealed source_missing row is corrected once, not once per launch",
           UsageLedger.shared.correctionCount() - 1, 1)

    // A genuinely different measurement is still a new fact and still lands.
    var later = recovered
    later["usage"] = usage(1, 2, 4)
    expect("and a re-measurement that says something new is still accepted",
           UsageLedger.shared.importTaskRecord(later), true)
    expect("as its own note", UsageLedger.shared.correctionCount(), 3)
}

group("which reading of input won is a stored fact, not one to be re-derived") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }

    // Codex's cumulative input includes its cached input; Claude's does not. The shape is decided
    // by arithmetic — both readings are computed and the one that reconciles with the source's
    // own total wins — and until this column existed the only way to tell which had happened was
    // to re-derive it from `usage_raw`. A determination nobody can audit is the same shape as the
    // unknowns this store exists to keep visible.
    let overlapped = UsageLedger.normalize(raw: ["input": 1000, "output": 10, "cache_read": 900,
                                                 "cache_write": 0, "total": 1010],
                                           assistant: .codex)
    let disjoint = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 30,
                                               "cache_write": 0, "total": 140],
                                         assistant: .codex)
    expect("the reading that took the cache out of input says so",
           overlapped.inputBasis, .includesCache)
    expect("and it is the one that reshaped the parts", overlapped.counts.inputNew, 100)
    expect("the reading that left input alone says that instead",
           disjoint.inputBasis, .excludesCache)
    check("the two decisions are distinguishable on the row",
          overlapped.inputBasis != disjoint.inputBasis)
    check("without either being called unreconciled, because both reconciled",
          overlapped.reconciliation == nil && disjoint.reconciliation == nil)

    let benign = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 0,
                                             "cache_write": 0, "total": 110], assistant: .codex)
    expect("and where there is no cache to move, both readings agree and it is recorded",
           benign.inputBasis, .readingsAgree)

    let assumed = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 30,
                                              "cache_write": 0], assistant: .codex)
    expect("a source with no total is decided on the assistant's shape, and says which",
           assumed.inputBasis, .includesCacheAssumed)
    let unstated = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 30,
                                               "cache_write": 0], assistant: .claude)
    expect("and where nothing was reshaped, that is a different word",
           unstated.inputBasis, .unstated)

    let broken = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 30,
                                             "cache_write": 0, "total": 999], assistant: .codex)
    expect("a Codex row that still does not sum after the cache came out says both halves",
           broken.inputBasis, .includesCacheUnreconciled)
    expect("beside the reconciliation flag it always had", broken.reconciliation,
           "parts_do_not_sum")
    let brokenClaude = UsageLedger.normalize(raw: ["input": 100, "output": 10, "cache_read": 30,
                                                   "cache_write": 0, "total": 999],
                                             assistant: .claude)
    expect("and a row nothing reshaped is the other word", brokenClaude.inputBasis, .unreconciled)

    UsageLedger.shared.importTaskRecord(["id": "task-basis", "assistant": "codex",
                                         "state": "success", "kind": "code",
                                         "project_dir": "/tmp", "timeout_minutes": 30,
                                         "depth": 1, "child_session": "sess-basis",
                                         "usage": ["input": 1000, "output": 10,
                                                   "cache_read": 900, "cache_write": 0,
                                                   "total": 1010],
                                         "finished_at": Date().timeIntervalSince1970])
    expect("the determination is on the stored row",
           UsageLedger.shared.rows(taskID: "task-basis").first?.inputBasis, "includes_cache")
    let csv = UsageLedger.shared.exportCSV()
    let columns = (csv.split(separator: "\n").first.map(String.init) ?? "")
        .split(separator: ",").map(String.init)
    let values = (csv.split(separator: "\n").dropFirst().first.map(String.init) ?? "")
        .split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    check("and in the export, where a month of rows can be audited against it",
          columns.firstIndex(of: "input_basis").map { values[$0] } == "includes_cache",
          values.joined(separator: ","))
    do {
    let store = freshUsageLedger(); defer { forgetUsageLedger(store) }
    let root = FileManager.default.temporaryDirectory.appendingPathComponent("clawdline-project-identity-\(UUID().uuidString)", isDirectory: true), repository = root.appendingPathComponent("clawdline", isDirectory: true), common = repository.appendingPathComponent(".git", isDirectory: true), worktree = root.appendingPathComponent("worktrees/task-uuid", isDirectory: true)
    try! FileManager.default.createDirectory(at: common, withIntermediateDirectories: true); try! FileManager.default.createDirectory(at: worktree, withIntermediateDirectories: true); defer { try? FileManager.default.removeItem(at: root) }
    let taskID = "11111111-2222-4333-8444-555555555555", parentID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", retryID = "99999999-8888-4777-8666-555555555555"
    check("the lineage-rich task imports", UsageLedger.shared.importTaskRecord([
        "id": taskID, "assistant": "codex", "state": "success", "kind": "code", "project_dir": worktree.path, "repository_common_dir": common.path, "timeout_minutes": 30, "depth": 2, "parent_task": parentID, "respawn_of": retryID, "respawn_generation": 1, "landing": ["state": "landed"], "child_session": "feature-session", "usage": ["input": 120, "output": 20, "cache_read": 100, "cache_write": 0, "total": 140]
    ]))
    let row = UsageLedger.shared.rows(taskID: taskID).first; check("canonical project and lineage survive registry eviction", row?.projectKey == repository.path && row?.parentTaskID == parentID && row?.retryOf == retryID && row?.attempt == 1 && row?.landingState == "landed")
    let before = row?.measurement
    let proposal = UsageLedger.AttributionEvent(eventID: "feature-proposal-1", intervalKey: row!.intervalKey, dimension: .feature, valueID: "usage-insights", valueLabel: "Usage insights", source: .llm, confidence: 0.91, classifierID: "small-feature-merger", classifierVersion: "2026-08-30", evidenceDigest: String(repeating: "a", count: 64), decision: .proposed, decisionSource: "classifier", assignedAt: Date(timeIntervalSince1970: 1_788_060_000), supersedesEventID: nil)
    check("an auditable LLM proposal is appended", UsageLedger.shared.record(proposal))
    check("the same event id is idempotent", !UsageLedger.shared.record(proposal))
    check("a proposal alone never enters accepted Feature totals", UsageLedger.shared.resolvedAttribution(intervalKey: row!.intervalKey, dimension: .feature) == nil)
    let acceptance = UsageLedger.AttributionEvent(eventID: "feature-acceptance-1", intervalKey: row!.intervalKey, dimension: .feature, valueID: "usage-insights", valueLabel: "Usage insights", source: .policy, confidence: 0.91, classifierID: "small-feature-merger", classifierVersion: "2026-08-30", evidenceDigest: String(repeating: "a", count: 64), decision: .accepted, decisionSource: "confidence-threshold-v1", assignedAt: Date(timeIntervalSince1970: 1_788_060_001), supersedesEventID: proposal.eventID)
    check("acceptance resolves Feature without rewriting usage", UsageLedger.shared.record(acceptance) && UsageLedger.shared.resolvedAttribution(intervalKey: row!.intervalKey, dimension: .feature)?.valueID == "usage-insights" && UsageLedger.shared.rows(taskID: taskID).first?.measurement == before)
    var invalid = proposal; invalid.eventID = "feature-proposal-invalid"; invalid.confidence = 1.2; check("invalid confidence is refused", !UsageLedger.shared.record(invalid))
    }
}

group("the watcher decides when to read, and what a session leaves behind when it goes") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    UsageLedger.forgetWatchedForTesting()
    let now = Date()

    // Asked before anything is opened, because the panel takes this reading every 1.2 seconds
    // and resolving a record can cost a working-directory lookup.
    check("a session read a moment ago is not opened again",
          UsageLedger.checkpointThrottled(since: now.addingTimeInterval(-10), now: now))
    check("one last read longer ago than the interval is",
          !UsageLedger.checkpointThrottled(
            since: now.addingTimeInterval(-UsageLedger.checkpointInterval - 1), now: now))
    check("and one nothing has ever read is taken immediately",
          !UsageLedger.checkpointThrottled(since: nil, now: now))

    check("a file that has not moved since the last checkpoint says nothing new",
          UsageLedger.checkpointUnchanged(lastSessionID: "s", lastBytes: 100,
                                          sessionID: "s", bytes: 100))
    check("a file that grew does",
          !UsageLedger.checkpointUnchanged(lastSessionID: "s", lastBytes: 100,
                                           sessionID: "s", bytes: 140))
    check("and a second conversation in the same tab is never skipped for being the same length",
          !UsageLedger.checkpointUnchanged(lastSessionID: "s", lastBytes: 100,
                                           sessionID: "s2", bytes: 100))
    check("a reading with no size to compare is taken rather than assumed unchanged",
          !UsageLedger.checkpointUnchanged(lastSessionID: "s", lastBytes: nil,
                                           sessionID: "s", bytes: nil))

    // A tab with no assistant in it is not a session anything spends in: nothing is opened and
    // nothing is remembered, which is what makes its later disappearance mean nothing either.
    let shell = TargetSession(backend: .iterm, id: "TERM-SHELL", name: "zsh",
                              tty: "/dev/ttys099", windowIndex: 0, tabIndex: 0, assistant: nil,
                              cwd: store.path)
    // **Not `eventually`, because there is nothing here to wait for.** `checkpoint` skips a tab
    // with no assistant before it opens anything, and `departed` returns synchronously when the
    // terminal was never remembered — the subject's whole effect on the ledger has happened by
    // the time these two calls return. The 0.4-second window that used to stand here was not a
    // wait: `eventually` pumps `RunLoop.main`, so the group spent 0.4 s running every other
    // writer's pending main-queue work and then asserted that nobody had written anything.
    // `UsageLedger.shared` is process-wide, so `rows().isEmpty` was never this subject's
    // question. What is asked now is the delta across this subject's own two calls, which is.
    // Widening the timeout would have moved the coin flip; removing the window removes it.
    // See `B-LEDGER-QUIET-WINDOW-IS-NOT-QUIET`.
    func ledgerIntervals() -> Set<String> {
        Set(UsageLedger.shared.rows().map(\.intervalKey))
    }
    let beforeShell = ledgerIntervals()
    UsageLedger.checkpoint(sessions: [shell], now: now)
    UsageLedger.departed([shell.id], now: now)
    check("an ordinary shell leaves the ledger alone",
          ledgerIntervals() == beforeShell, ledgerWitness())

    // An assistant tab whose record cannot be found is the documented blind spot: no ranked
    // guess at which transcript belongs to it, so no row rather than somebody else's tokens.
    let unfindable = TargetSession(backend: .iterm, id: "TERM-UNFINDABLE", name: "claude",
                                   tty: "/dev/ttys098", windowIndex: 0, tabIndex: 1,
                                   assistant: .claude,
                                   cwd: store.appendingPathComponent("nowhere").path)
    let beforeUnfindable = ledgerIntervals()
    UsageLedger.checkpoint(sessions: [unfindable], now: now)
    UsageLedger.departed([unfindable.id], now: now)
    check("and neither does a session whose transcript nothing can name",
          ledgerIntervals() == beforeUnfindable, ledgerWitness())

    // The close. A process that has gone is one of the three forced checkpoints, and it is the
    // one that decides `complete` against `source_missing` — so the record is read once more.
    let transcript = store.appendingPathComponent("sess-departed.jsonl")
    let turn = """
        {"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":10,\
        "output_tokens":5,"cache_read_input_tokens":85,"cache_creation_input_tokens":0}}}
        """
    try! Data(turn.utf8).write(to: transcript)
    UsageLedger.remember(terminalID: "TERM-DEPARTED", assistant: .claude,
                         sessionID: "sess-departed", record: transcript, bytes: turn.count,
                         at: now)
    UsageLedger.departed(["TERM-DEPARTED"], now: now)
    check("a session whose process disappears is read once more and sealed",
          eventually { UsageLedger.shared.rows().first { $0.sessionID == "sess-departed" }?
              .sealed == true })
    let departed = UsageLedger.shared.rows().first { $0.sessionID == "sess-departed" }
    expect("with what the transcript held at the end", departed?.total, 100)
    expect("sealed complete, because the source was still readable", departed?.coverage,
           "complete")
    expect("and filed as a session a person opened", departed?.origin, "manual")
    UsageLedger.departed(["TERM-DEPARTED"], now: now)
    check("a terminal that has already gone is not read a second time",
          !eventually(timeout: 0.4) {
              UsageLedger.shared.rows().filter { $0.sessionID == "sess-departed" }.count > 1
          })

    // The other outcome of the same moment: the record is unreadable by the time it closes.
    UsageLedger.remember(terminalID: "TERM-VANISHED", assistant: .claude,
                         sessionID: "sess-vanished",
                         record: store.appendingPathComponent("gone.jsonl"), bytes: nil, at: now)
    UsageLedger.departed(["TERM-VANISHED"], now: now)
    check("a session whose record has gone is a state, never a zero",
          eventually {
              let row = UsageLedger.shared.rows().first { $0.sessionID == "sess-vanished" }
              return row?.coverage == "source_missing" && row?.counts.isEmpty == true
                  && row?.total == nil
          })
    expect("saying which kind of unavailable it is",
           UsageLedger.shared.rows().first { $0.sessionID == "sess-vanished" }?.coverageReasons,
           ["source_unreadable_at_close"])
}
}
