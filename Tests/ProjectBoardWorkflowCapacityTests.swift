import Foundation

func workflowAbandonedCapacityRecoveryProof() {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-workflow-capacity-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let file = root.appendingPathComponent("journal.json")
    let identity = ProjectBoardWorkflow.Identity(
        terminalID: "%capacity", provider: "codex",
        conversationID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
        projectID: "project-capacity", projectPath: root.path,
        processGeneration: "capacity:1")
    var clock = 1_000.0
    let limits = ProjectBoardWorkflow.Limits(
        runs: 2, eventsPerRun: 64, receipts: 64, outbox: 64,
        maximumBytes: 1_024 * 1_024)
    func make() -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(
            url: file, now: { Date(timeIntervalSince1970: clock) },
            boardHeader: { (true, 1, true) },
            boardCommand: { _, _ in ProjectBoardStore.Reply(
                status: 404, body: ["error": ["code": "item_not_found"]]) },
            limits: limits, autoStart: false)
    }

    let recentFile = root.appendingPathComponent("recent.json")
    let recentLimits = ProjectBoardWorkflow.Limits(
        runs: 1, eventsPerRun: 64, receipts: 64, outbox: 64,
        maximumBytes: 1_024 * 1_024)
    let recent = ProjectBoardWorkflow(
        url: recentFile, now: { Date(timeIntervalSince1970: clock) },
        boardHeader: { (true, 1, true) }, limits: recentLimits, autoStart: false)
    recent.syncMode()
    guard case .managed(let recentRun) = recent.prepareIngress(
        requestID: "recent", fingerprint: "recent", text: "recent work",
        imageCount: 0, identity: identity) else {
        check("capacity recovery creates its recent run", false); return
    }
    _ = recent.markDelivery(runID: recentRun.runID, identity: identity, delivered: true)
    clock += 86_399
    if case .refused(let code) = recent.prepareIngress(
        requestID: "too-early", fingerprint: "too-early", text: "not yet",
        imageCount: 0, identity: identity) {
        expect("recent abandoned workflow history remains protected", code,
               "workflow_capacity_reached")
    } else { check("recent abandoned workflow history remains protected", false) }
    expect("recent old run records a non-work begin", recent.record([
        "operation": "begin", "run_id": recentRun.runID,
        "classification": "question", "phase": "output",
    ], requestID: "recent-begin", fingerprint: "recent-begin", identity: identity).status, 202)
    clock += 3
    expect("recent old run records fresh progress activity", recent.record([
        "operation": "progress", "run_id": recentRun.runID, "summary": "still active",
    ], requestID: "recent-progress", fingerprint: "recent-progress", identity: identity).status, 202)
    if case .refused(let code) = recent.prepareIngress(
        requestID: "old-created-recent-active", fingerprint: "old-created-recent-active",
        text: "must wait", imageCount: 0, identity: identity) {
        expect("recent activity watermark prevents retirement of an old run", code,
               "workflow_capacity_reached")
    } else { check("recent activity watermark prevents retirement of an old run", false) }
    clock = 1_000

    var workflow = make(); workflow.syncMode()
    guard case .managed(let abandoned) = workflow.prepareIngress(
        requestID: "abandoned", fingerprint: "abandoned", text: "old work",
        imageCount: 0, identity: identity) else {
        check("capacity recovery creates its abandoned run", false); return
    }
    _ = workflow.markDelivery(runID: abandoned.runID, identity: identity, delivered: true)
    clock += 1
    guard case .managed(let failed) = workflow.prepareIngress(
        requestID: "terminal-failure", fingerprint: "terminal-failure", text: "failed work",
        imageCount: 0, identity: identity) else {
        check("capacity recovery creates its terminal-failure run", false); return
    }
    _ = workflow.markDelivery(runID: failed.runID, identity: identity, delivered: true)
    _ = workflow.record([
        "operation": "begin", "run_id": failed.runID,
        "classification": "existing_item", "item_id": "item-missing", "phase": "output",
    ], requestID: "failed-begin", fingerprint: "failed-begin", identity: identity)
    workflow.drainForTesting()
    _ = workflow.record([
        "operation": "deliver", "run_id": failed.runID,
        "disposition": "delivered", "summary": "done", "next_action": "none",
    ], requestID: "failed-deliver", fingerprint: "failed-deliver", identity: identity)
    workflow.drainForTesting()
    expect("terminal Board refusals do not leave assistant follow-up missing",
           workflow.snapshot(runID: failed.runID)?["missing_follow_up"] as? [String], [])

    clock = 1_000 + 86_401
    guard case .managed(let admitted) = workflow.prepareIngress(
        requestID: "after-expiry", fingerprint: "after-expiry", text: "new work",
        imageCount: 0, identity: identity) else {
        check("expired abandoned history yields capacity to a new ingress", false); return
    }
    check("oldest abandoned run is retired before a newer terminal failure",
          workflow.snapshot(runID: abandoned.runID) == nil
            && workflow.snapshot(runID: failed.runID) != nil)
    _ = workflow.markDelivery(runID: admitted.runID, identity: identity, delivered: true)
    clock += 1
    guard case .managed = workflow.prepareIngress(
        requestID: "after-failed", fingerprint: "after-failed", text: "more work",
        imageCount: 0, identity: identity) else {
        check("terminal failed outbox rows cannot pin run capacity", false); return
    }
    check("the failed outbox subject is retired without replay",
          workflow.snapshot(runID: failed.runID) == nil)

    let retired = workflow.historicalGaps()["retired"] as? [String: Any]
    expect("retired gaps preserve the missing-begin run count",
           retired?["missing_begin_runs"] as? Int, 1)
    expect("retired gaps preserve terminal-failed run count",
           retired?["terminal_failed_outbox_runs"] as? Int, 1)
    check("retired gaps preserve the number of failed command rows",
          (retired?["terminal_failed_outbox_rows"] as? Int ?? 0) > 0)
    expect("retired gaps preserve terminal failure reasons",
           (retired?["failure_codes"] as? [String: Int])?["item_not_found"],
           2)
    expect("retired failure reasons account for every failed command row",
           (retired?["failure_codes"] as? [String: Int])?.values.reduce(0, +),
           retired?["terminal_failed_outbox_rows"] as? Int)
    expect("retired gaps preserve provider accountability",
           (retired?["providers"] as? [String: Int])?[identity.provider], 2)
    workflow = make()
    expect("retired gap counts survive restart",
           (workflow.historicalGaps()["retired"] as? [String: Any])?["runs"] as? Int, 2)
}

func workflowTerminalFailureCompactionProof() {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-workflow-failure-compaction-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let file = root.appendingPathComponent("journal.json")
    let identity = ProjectBoardWorkflow.Identity(
        terminalID: "%failure-capacity", provider: "codex",
        conversationID: "11111111-2222-4333-8444-555555555555",
        projectID: "project-failure-capacity", projectPath: root.path,
        processGeneration: "failure-capacity:1")
    func make() -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(
            url: file, boardHeader: { (true, 1, true) },
            boardCommand: { _, _ in ProjectBoardStore.Reply(
                status: 404, body: ["error": ["code": "item_not_found"]]) },
            limits: ProjectBoardWorkflow.Limits(
                runs: 4, eventsPerRun: 4, receipts: 64, outbox: 4,
                maximumBytes: 1_024 * 1_024), autoStart: false)
    }
    var workflow = make(); workflow.syncMode()
    guard case .managed(let run) = workflow.prepareIngress(
        requestID: "failure-run", fingerprint: "failure-run", text: "work",
        imageCount: 0, identity: identity) else {
        check("failure compaction creates a run", false); return
    }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    expect("failure compaction records begin", workflow.record([
        "operation": "begin", "run_id": run.runID,
        "classification": "existing_item", "item_id": "missing-item", "phase": "output",
    ], requestID: "failure-begin", fingerprint: "failure-begin", identity: identity).status, 202)
    workflow.drainForTesting()
    for index in 0..<12 {
        expect("terminal failure history does not consume event capacity \(index)", workflow.record([
            "operation": "progress", "run_id": run.runID, "summary": "failure \(index)",
            "outputs": [["title": "proof \(index)",
                         "url": "https://example.test/failure/\(index)", "kind": "website"]],
        ], requestID: "failure-progress-\(index)", fingerprint: "failure-progress-\(index)",
           identity: identity).status, 202)
        workflow.drainForTesting()
    }
    let snapshot = workflow.snapshot(runID: run.runID)
    let outbox = snapshot?["outbox"] as? [String: Any]
    expect("terminal failures remain visible as an aggregate", outbox?["failed"] as? Int, 14)
    expect("terminal failure codes remain accountable",
           (outbox?["failure_counts"] as? [String: Int])?["item_not_found"], 14)
    if let saved = try? JSONSerialization.jsonObject(with: Data(contentsOf: file))
        as? [String: Any] {
        expect("terminal raw rows are removed from executable storage",
               (saved["outbox"] as? [Any])?.count, 0)
        expect("terminal evidence raises the rollback schema boundary",
               saved["schemaVersion"] as? Int, 4)
    } else { check("terminal failure journal remains decodable", false) }
    workflow = make()
    let reopened = workflow.snapshot(runID: run.runID)?["outbox"] as? [String: Any]
    expect("terminal failure aggregate survives restart", reopened?["failed"] as? Int, 14)
    expect("restart does not recreate terminal executable rows",
           reopened?["pending"] as? Int, 0)
}
