import Foundation

private final class BoardTestDriver {
    let root: URL
    let file: URL
    let store: ProjectBoardStore
    private var request = 0

    init(name: String = UUID().uuidString, now: @escaping () -> Date = Date.init) {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-board-\(name)", isDirectory: true)
        file = root.appendingPathComponent("board.json")
        try? FileManager.default.removeItem(at: root)
        store = ProjectBoardStore(url: file, now: now)
    }

    deinit { try? FileManager.default.removeItem(at: root) }

    var revision: Int {
        ((store.snapshot()["board"] as? [String: Any])?["revision"] as? Int) ?? -1
    }

    @discardableResult
    func send(_ operation: String, _ fields: [String: Any] = [:], actor: String = "owner",
              trusted: Bool = false, expected: Int? = nil,
              requestId: String? = nil) -> ProjectBoardStore.Reply {
        request += 1
        var body = fields
        body["operation"] = operation
        body["requestId"] = requestId ?? "request-\(request)"
        body["expectedRevision"] = expected ?? revision
        return store.command(body, actor: actor, trusted: trusted)
    }

    func createProject(id: String = "project-1", name: String = "Clawdline") {
        _ = store.ensureProject(id: id, name: name)
    }

    @discardableResult
    func create(type: String = "feature", title: String = "Board",
                owner: String = "root", project: String = "project-1",
                parent: String? = nil) -> String {
        var fields: [String: Any] = [
            "projectId": project, "title": title, "type": type, "owner": owner,
        ]
        if let parent { fields["parentId"] = parent }
        return send("create", fields).body["itemId"] as? String ?? ""
    }

    func item(_ id: String) -> [String: Any] {
        let board = store.snapshot(item: id)["board"] as? [String: Any]
        return board?["item"] as? [String: Any] ?? [:]
    }
}

private func boardError(_ reply: ProjectBoardStore.Reply) -> String {
    ((reply.body["error"] as? [String: Any])?["code"] as? String) ?? ""
}

private func boardItems(_ store: ProjectBoardStore) -> [[String: Any]] {
    let board = store.snapshot()["board"] as? [String: Any]
    return board?["items"] as? [[String: Any]] ?? []
}

private func boardProgress(_ item: [String: Any]) -> [String: Any] {
    item["progress"] as? [String: Any] ?? [:]
}

private func boardAdvanceToExecution(_ driver: BoardTestDriver, item: String) {
    _ = driver.send("transition", ["itemId": item, "state": "ready"])
    _ = driver.send("transition", ["itemId": item, "state": "execution"])
}

func runProjectBoardTests() {
group("Project Board defaults, mode boundaries, and corrupt stores fail closed") {
    let d = BoardTestDriver(name: "mode-\(UUID().uuidString)")
    var board = d.store.snapshot()["board"] as? [String: Any]
    expect("a new store defaults enabled", board?["enabled"] as? Bool, true)
    expect("the effective default mode is board", board?["mode"] as? String, "board")
    expect("the schema is explicitly versioned", board?["schemaVersion"] as? Int, 1)
    expect("the current entitlement is free", (board?["entitlement"] as? [String: Any])?["state"] as? String,
           "free_preview")

    d.createProject()
    let item = d.create()
    _ = d.send("span", ["itemId": item, "sessionId": "session-a", "phase": "output"])
    let disabled = d.send("set_enabled", ["enabled": false])
    expect("disabling is durable command success", disabled.status, 200)
    expect("the property follows durable state", d.store.enabled, false)
    board = d.store.snapshot()["board"] as? [String: Any]
    expect("disabled mode is standard", board?["mode"] as? String, "standard")
    let spans = d.item(item)["spans"] as? [[String: Any]]
    check("disable closes active spans at its suspension boundary",
          spans?.first?["endedAt"] is Double)
    let history = d.item(item)["history"] as? [[String: Any]]
    check("disable preserves a typed suspension boundary",
          history?.contains { $0["kind"] as? String == "tracking_suspended" } == true)
    expect("board-only mutation is refused while disabled",
           boardError(d.send("create", ["projectId": "project-1", "title": "no",
                                         "type": "task"])), "board_disabled")
    let disabledProjectCount = (board?["projects"] as? [[String: Any]])?.count ?? 0
    expect("disabled mode refuses automatic Project discovery",
           d.store.ensureProject(id: "project-disabled", name: "Disabled").reason,
           "board_disabled")
    expect("disabled automatic discovery preserves existing history only",
           (((d.store.snapshot()["board"] as? [String: Any])?["projects"]
                as? [[String: Any]])?.count), disabledProjectCount)
    expect("set_enabled remains available while disabled",
           d.send("set_enabled", ["enabled": true]).status, 200)
    let reopened = ProjectBoardStore(url: d.file)
    expect("mode and history survive a new process owner", reopened.enabled, true)
    expect("the same item survives a new process owner", boardItems(reopened).count, 1)

    let corruptRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-board-corrupt-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: corruptRoot, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: corruptRoot) }
    let corruptURL = corruptRoot.appendingPathComponent("board.json")
    let original = Data("not-json".utf8)
    try! original.write(to: corruptURL)
    let corrupt = ProjectBoardStore(url: corruptURL)
    let corruptBoard = corrupt.snapshot()["board"] as? [String: Any]
    expect("corruption switches workflow safely to standard", corruptBoard?["mode"] as? String,
           "standard")
    expect("corruption is typed instead of drawn as an empty board",
           (corruptBoard?["error"] as? [String: Any])?["code"] as? String,
           "board_store_corrupt")
    expect("corrupt state cannot authorize board gates", corrupt.enabled, false)
    _ = corrupt.ensureProject(id: "would-overwrite", name: "No")
    expect("a corrupt store is left byte-for-byte untouched", try? Data(contentsOf: corruptURL),
           original)
    expect("commands fail closed without blocking baseline workflow",
           boardError(corrupt.command(["operation": "set_enabled", "requestId": "x",
                                       "expectedRevision": 0, "enabled": true], actor: "root")),
           "board_store_corrupt")

    try! Data("{\"schemaVersion\":99}".utf8).write(to: corruptURL)
    let future = ProjectBoardStore(url: corruptURL)
    expect("an unknown version is a distinct typed refusal",
           ((future.snapshot()["board"] as? [String: Any])?["error"] as? [String: Any])?["code"] as? String,
           "board_store_version_unsupported")

    let blockedRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-board-blocked-\(UUID().uuidString)")
    try! Data("not-a-directory".utf8).write(to: blockedRoot)
    defer { try? FileManager.default.removeItem(at: blockedRoot) }
    let blocked = ProjectBoardStore(url: blockedRoot.appendingPathComponent("board.json"))
    let failedEnsure = blocked.ensureProject(id: "project", name: "Project")
    expect("automatic persistence failure is typed", failedEnsure.status.rawValue, "unavailable")
    expect("automatic persistence failure is retryable rather than reported persisted",
           failedEnsure.persisted, false)
    expect("automatic persistence failure names its stable reason", failedEnsure.reason,
           "persistence_failed")
    let failedAutomatic = (blocked.snapshot()["board"] as? [String: Any])?["automaticMutation"]
        as? [String: Any]
    expect("automatic persistence failure remains visible to the root snapshot",
           failedAutomatic?["reason"] as? String, "persistence_failed")
}

group("Project Board seed publication cannot roll durable mode or revision backwards") {
    let d = BoardTestDriver(name: "publication-\(UUID().uuidString)")
    d.createProject(id: "first")
    _ = d.store.readSeed(rebuild: true)
    d.createProject(id: "dirty")

    let publicationEntered = DispatchSemaphore(value: 0)
    let publicationRelease = DispatchSemaphore(value: 0)
    let seedFinished = DispatchSemaphore(value: 0)
    let mutationsFinished = DispatchSemaphore(value: 0)
    let pauseLock = NSLock()
    var paused = false
    ProjectBoardStore.seedPublicationPauseForTesting = {
        pauseLock.lock()
        let shouldPause = !paused
        paused = true
        pauseLock.unlock()
        guard shouldPause else { return }
        publicationEntered.signal()
        _ = publicationRelease.wait(timeout: .now() + 4)
    }
    defer { ProjectBoardStore.seedPublicationPauseForTesting = nil }

    DispatchQueue.global(qos: .utility).async {
        _ = d.store.readSeed(rebuild: true)
        seedFinished.signal()
    }
    check("seed reaches the former cross-lock publication window",
          publicationEntered.wait(timeout: .now() + 1) == .success)
    DispatchQueue.global(qos: .utility).async {
        _ = d.store.ensureProject(id: "ordinary", name: "Ordinary")
        let revision = d.store.readHeader().revision
        _ = d.store.command(["operation": "set_enabled", "requestId": "off-after-seed",
                             "expectedRevision": revision, "enabled": false], actor: "root")
        mutationsFinished.signal()
    }
    check("ordinary mutation and OFF cannot pass a seed that still owns the writer",
          mutationsFinished.wait(timeout: .now() + 0.1) == .timedOut)
    publicationRelease.signal()
    check("held seed completes", seedFinished.wait(timeout: .now() + 2) == .success)
    check("interleaved mutations complete", mutationsFinished.wait(timeout: .now() + 2) == .success)
    let final = d.store.readHeader()
    expect("older ON seed cannot restore Board mode", final.enabled, false)
    check("older seed cannot restore its revision", final.revision > 2)
    expect("published header matches durable state", final.revision, d.revision)
}

group("Project Board commands are closed, CAS-serialized, and durably idempotent") {
    let d = BoardTestDriver(name: "cas-\(UUID().uuidString)")
    d.createProject()
    let revision = d.revision
    let first = d.send("create", ["projectId": "project-1", "title": "One",
                                   "type": "feature", "owner": "root"],
                       expected: revision, requestId: "same")
    expect("a typed create succeeds", first.status, 200)
    let item = first.body["itemId"] as? String ?? ""
    let afterCreate = d.revision
    let replay = d.send("create", ["projectId": "project-1", "title": "One",
                                    "type": "feature", "owner": "root"],
                        expected: revision, requestId: "same")
    expect("identical actor request replays success", replay.status, 200)
    expect("a replay returns the same item identity", replay.body["itemId"] as? String, item)
    expect("a replay does not advance revision", d.revision, afterCreate)
    expect("same request id with a different body conflicts",
           boardError(d.send("create", ["projectId": "project-1", "title": "Different",
                                         "type": "feature", "owner": "root"],
                             expected: revision, requestId: "same")), "request_id_conflict")

    let stale = d.send("update", ["itemId": item, "title": "stale"], expected: revision,
                       requestId: "stale")
    expect("stale revision is a typed conflict", boardError(stale), "revision_conflict")
    expect("stale refusal replay is stable",
           boardError(d.send("update", ["itemId": item, "title": "stale"], expected: revision,
                             requestId: "stale")), "revision_conflict")
    expect("unknown operation is typed",
           boardError(d.send("invent", [:])), "unknown_operation")
    expect("unknown fields are refused rather than discarded",
           boardError(d.send("update", ["itemId": item, "title": "x", "surprise": true])),
           "invalid_command_fields")
    expect("a Boolean is not accepted as a revision",
           boardError(d.store.command(["operation": "update", "requestId": "bool-revision",
                                       "expectedRevision": true, "itemId": item,
                                       "title": "x"], actor: "root")), "invalid_command")
    expect("a rounded Double beyond Int.max is refused instead of trapping",
           boardError(d.store.command(["operation": "update", "requestId": "huge-revision",
                                       "expectedRevision": 9.223372036854776e18,
                                       "itemId": item, "title": "x"], actor: "root")),
           "invalid_command")

    let racedRevision = d.revision
    let raceLock = NSLock()
    var statuses: [Int] = []
    let group = DispatchGroup()
    for number in 1...2 {
        group.enter()
        DispatchQueue.global().async {
            let reply = d.store.command([
                "operation": "update", "requestId": "race-\(number)",
                "expectedRevision": racedRevision, "itemId": item,
                "summary": "writer \(number)",
            ], actor: "root")
            raceLock.lock(); statuses.append(reply.status); raceLock.unlock()
            group.leave()
        }
    }
    group.wait()
    expect("one concurrent CAS writer wins", statuses.filter { $0 == 200 }.count, 1)
    expect("one concurrent CAS writer observes conflict", statuses.filter { $0 == 409 }.count, 1)
    let persisted = ProjectBoardStore(url: d.file)
    expect("the winning write survives reload", boardItems(persisted).count, 1)
    let retention = (persisted.snapshot()["board"] as? [String: Any])?["idempotencyRetention"]
        as? [String: Any]
    expect("bounded idempotency retention is explicit", retention?["maximumReceipts"] as? Int,
           4096)

    let bounded = BoardTestDriver(name: "bounded-\(UUID().uuidString)")
    bounded.createProject()
    let boundedItem = bounded.create(type: "feature", title: "Bounded evidence")
    _ = bounded.send("record_evidence", [
        "itemId": boundedItem, "kind": "finding", "summary": String(repeating: "x", count: 900),
        "subject": "subject", "status": "open", "sourceId": "seed",
        "blocking": true,
    ], trusted: true)
    var persistedObject = try! JSONSerialization.jsonObject(with: Data(contentsOf: bounded.file))
        as! [String: Any]
    var persistedItems = persistedObject["items"] as! [[String: Any]]
    var evidenceRows = persistedItems[0]["evidence"] as! [[String: Any]]
    let seedEvidence = evidenceRows[0]
    evidenceRows = (0..<2_000).map { number in
        var row = seedEvidence
        row["id"] = "evidence-\(number)"
        row["sourceId"] = "source-\(number)"
        return row
    }
    persistedItems[0]["evidence"] = evidenceRows
    persistedObject["items"] = persistedItems
    try! JSONSerialization.data(withJSONObject: persistedObject, options: [.sortedKeys])
        .write(to: bounded.file, options: .atomic)
    let atCapacity = ProjectBoardStore(url: bounded.file)
    let capacityRevision = ((atCapacity.snapshot()["board"] as? [String: Any])?["revision"]
        as? Int) ?? -1
    let capacityReply = atCapacity.command([
        "operation": "record_evidence", "requestId": "capacity-plus-one",
        "expectedRevision": capacityRevision, "itemId": boundedItem,
        "kind": "finding", "summary": "one too many", "subject": "subject",
        "status": "open", "sourceId": "source-overflow", "blocking": true,
    ], actor: "root", trusted: true)
    expect("evidence capacity plus one is a typed refusal", boardError(capacityReply),
           "evidence_capacity_reached")
    let reloadedCapacityBoard = ProjectBoardStore(url: bounded.file).snapshot()["board"]
        as? [String: Any]
    expect("a refused evidence append leaves a reloadable store",
           reloadedCapacityBoard?["available"] as? Bool, true)
    expect("a selected record whose mandatory blockers exceed the byte budget is typed unavailable",
           (((atCapacity.snapshot(item: boundedItem)["board"] as? [String: Any])?["error"]
                as? [String: Any])?["code"] as? String), "board_snapshot_too_large")

    var reportClock = 1_800_000_000.0
    let reports = BoardTestDriver(name: "reports-\(UUID().uuidString)", now: {
        Date(timeIntervalSince1970: reportClock)
    })
    reports.createProject()
    let closed = reports.create(type: "task", title: "Closure narrative")
    let open = reports.create(type: "task", title: "Still open")
    let artifactReply = reports.send("artifact", [
        "itemId": closed, "title": "Report", "url": "https://example.test/report",
        "kind": "document",
    ])
    let artifactID = ((((artifactReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["artifacts"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    boardAdvanceToExecution(reports, item: closed)
    _ = reports.send("accept_artifact", [
        "itemId": closed, "artifactId": artifactID, "note": "accepted",
    ], trusted: true)
    expect("a missing report does not block evidence-driven closure",
           reports.item(closed)["state"] as? String, "closed")
    expect("report absence is explicit rather than an empty claimed report",
           (reports.item(closed)["completionReport"] as? [String: Any])?["status"] as? String,
           "absent")
    let reportFields: [String: Any] = [
        "itemId": closed,
        "objective": "Explain what the work set out to accomplish.",
        "deliveredOutcomes": "A durable report and readable detail.",
        "verificationLanding": "Artifact accepted; this prose is not the receipt.",
        "remainingWork": "Historical pilot remains separate.",
        "lessons": "Keep narrative authority separate from evidence authority.",
        "authorship": "assistant",
        "model": "low-cost-test-model",
        "sourceReferences": [
            ["kind": "artifact", "targetId": artifactID, "label": "Accepted report"],
            ["kind": "item", "targetId": open, "label": "Related same-project item"],
            ["kind": "external", "targetId": "missing-transcript", "label": "Unavailable source"],
        ],
    ]
    let coordination = reports.create(type: "coordination", title: "Bounded coordination")
    var coordinationReport = reportFields
    coordinationReport["itemId"] = coordination
    coordinationReport["reportBoundary"] = [
        "kind": "time_interval", "label": "September integration window",
        "startedAt": reportClock - 3_600, "endedAt": reportClock,
    ]
    coordinationReport["sourceReferences"] = [
        ["kind": "item", "targetId": coordination, "label": "Coordination record"],
    ]
    let coordinationState = reports.item(coordination)["state"] as? String
    expect("Coordination narrative without a typed boundary is refused",
           boardError(reports.send("record_report", reportFields.merging(
                ["itemId": coordination]) { _, new in new },
                actor: "owning-root", trusted: true)),
           "coordination_report_boundary_required")
    expect("a named bounded Coordination period can receive narrative without lifecycle",
           reports.send("record_report", coordinationReport, actor: "owning-root",
                        trusted: true).status, 200)
    expect("recording a Coordination report never mutates lifecycle state",
           reports.item(coordination)["state"] as? String, coordinationState)
    let coordinationBoundary = (reports.item(coordination)["completionReport"]
        as? [String: Any])?["reportBoundary"] as? [String: Any]
    expect("Coordination report exposes its typed time boundary",
           coordinationBoundary?["kind"] as? String, "time_interval")

    let handedCoordination = reports.create(type: "coordination", title: "Handoff boundary")
    _ = reports.send("handoff", ["itemId": handedCoordination, "owner": "receiver",
                                  "note": "continue the next period"])
    let handoffID = (reports.item(handedCoordination)["handoff"]
        as? [String: Any])?["id"] as? String ?? ""
    var handoffReport = reportFields
    handoffReport["itemId"] = handedCoordination
    handoffReport["reportBoundary"] = [
        "kind": "handoff", "label": "Root to receiver", "handoffId": handoffID,
    ]
    handoffReport["sourceReferences"] = [
        ["kind": "handoff", "targetId": handoffID, "label": "Pending handoff"],
    ]
    expect("a same-item handoff can bound a Coordination report",
           reports.send("record_report", handoffReport, actor: "owning-root",
                        trusted: true).status, 200)

    let inferredTask: [String: Any] = [
        "id": "historical-report-task", "title": "Historical report delivery",
        "state": "success", "assistant": "codex", "startedAt": reportClock - 200,
        "finishedAt": reportClock - 100,
        "graph": ["id": "historical-report-graph", "destination": "Historical report",
                  "current_node": "delivery", "kind": "delivery"],
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "historical-report-commit",
            "verified_target_commit": "historical-report-target",
            "landed_at": reportClock - 50,
        ],
    ]
    _ = reports.store.ingest(task: inferredTask, projectID: "project-1")
    let inferred = boardItems(reports.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["kind"] as? String == "task"
                && $0["targetId"] as? String == "historical-report-task"
        } == true
    } ?? [:]
    let inferredID = inferred["id"] as? String ?? ""
    expect("authoritative historical landing is report-eligible despite missing older process facts",
           boardProgress(inferred)["state"] as? String, "landed")
    check("historical landing does not counterfeit formal closure",
          inferred["state"] as? String != "closed")
    var inferredReport = reportFields
    inferredReport["itemId"] = inferredID
    inferredReport["sourceReferences"] = [[
        "kind": "task", "targetId": "historical-report-task", "label": "Delivered task",
    ]]
    let inferredState = inferred["state"] as? String
    expect("an authoritative landed projection can receive its report",
           reports.send("record_report", inferredReport, actor: "owning-root",
                        trusted: true).status, 200)
    expect("report admission does not advance a landed historical item's lifecycle",
           reports.item(inferredID)["state"] as? String, inferredState)
    expect("landed-not-closed report is current for its unchanged scope",
           (reports.item(inferredID)["completionReport"] as? [String: Any])?["status"]
                as? String,
           "current")

    let rebound = reports.create(type: "feature", title: "Explicit report owner")
    var reboundTask = inferredTask
    reboundTask["workItemId"] = rebound
    _ = reports.store.ingest(task: reboundTask, projectID: "project-1")
    let retainedReport = reports.item(inferredID)["completionReport"] as? [String: Any]
    expect("inferred-to-explicit rebind preserves the authored report body",
           retainedReport?["objective"] as? String,
           "Explain what the work set out to accomplish.")
    expect("rebound report is qualified rather than presented as current truth",
           retainedReport?["status"] as? String, "historical_needs_update")
    let retainedSource = (retainedReport?["sourceReferences"] as? [[String: Any]])?.first
    expect("rebound source relationship is explicitly authoring-time provenance",
           retainedSource?["relationship"] as? String, "same_item_at_authorship")
    expect("rebound source relationship retains its resolution epoch",
           retainedSource?["resolvedAt"] as? Double, reportClock)

    expect("an open item cannot receive a closure report",
           boardError(reports.send("record_report", reportFields.merging(["itemId": open]) { _, new in new },
                                   trusted: true)),
           "report_requires_landed_or_closed_item")
    expect("ordinary send authority cannot author an item closure report",
           boardError(reports.send("record_report", reportFields)), "trusted_report_required")
    expect("unsafe report source URLs are refused before persistence",
           boardError(reports.send("record_report", reportFields.merging([
                "sourceReferences": [["kind": "external", "targetId": "x", "label": "bad",
                                      "url": "javascript:alert(1)"]],
           ]) { _, new in new }, trusted: true)),
           "invalid_report_source_url")
    expect("report failure never reopens or blocks the closed item",
           reports.item(closed)["state"] as? String, "closed")
    expect("a trusted closed report is accepted",
           reports.send("record_report", reportFields, actor: "owning-root", trusted: true).status,
           200)
    var report = reports.item(closed)["completionReport"] as? [String: Any]
    expect("the report has a server-issued version", report?["version"] as? Int, 1)
    expect("the report is current only for its closed scope", report?["status"] as? String,
           "current")
    expect("the report actor comes from authenticated command context", report?["actor"] as? String,
           "owning-root")
    expect("the authored time comes from the store clock", report?["authoredAt"] as? Double,
           reportClock)
    expect("assistant model provenance is retained", report?["model"] as? String,
           "low-cost-test-model")
    let sources = report?["sourceReferences"] as? [[String: Any]]
    expect("same-item source resolution is server-issued", sources?[0]["resolution"] as? String,
           "same_item")
    expect("same-project source resolution is explicit", sources?[1]["resolution"] as? String,
           "same_project")
    expect("missing sources remain explicitly unresolved", sources?[2]["resolution"] as? String,
           "unresolved")
    check("report sources can never mint evidentiary authority",
          sources?.allSatisfy { $0["authority"] as? String == "narrative_only" } == true)
    let card = boardItems(reports.store).first { $0["id"] as? String == closed }
    let cardReport = card?["completionReport"] as? [String: Any]
    check("catalog payload has report metadata but not the report body",
          cardReport?["version"] as? Int == 1 && cardReport?["objective"] == nil)

    reportClock += 10
    var replacement = reportFields
    replacement["lessons"] = "Replacement keeps the earlier version."
    _ = reports.send("record_report", replacement, actor: "owning-root", trusted: true)
    report = reports.item(closed)["completionReport"] as? [String: Any]
    expect("a replacement increments report version", report?["version"] as? Int, 2)
    var durableReportState = try! JSONSerialization.jsonObject(with: Data(contentsOf: reports.file))
        as! [String: Any]
    var durableReportItems = durableReportState["items"] as! [[String: Any]]
    var durableVersions = durableReportItems.first { $0["id"] as? String == closed }?["completionReports"]
        as! [[String: Any]]
    expect("prior report prose remains durably stored after replacement",
           durableVersions.first?["lessons"] as? String,
           "Keep narrative authority separate from evidence authority.")
    check("detail exposes superseded provenance without repeating full reports",
          ((reports.item(closed)["completionReportHistory"] as? [[String: Any]])?.first)?["version"] as? Int == 1
            && ((reports.item(closed)["completionReportHistory"] as? [[String: Any]])?.first)?["lessons"] == nil)
    let firstReportID = ((reports.item(closed)["completionReportHistory"]
        as? [[String: Any]])?.first)?["id"] as? String ?? ""
    let selectedPrior = reports.store.reportSnapshot(
        project: "project-1", item: closed, report: firstReportID)
    let selectedPriorBoard = selectedPrior.body["board"] as? [String: Any]
    let selectedPriorReport = selectedPriorBoard?["reportSelection"] as? [String: Any]
    expect("one selected superseded report body is retrievable through a bounded read",
           selectedPriorReport?["lessons"] as? String,
           "Keep narrative authority separate from evidence authority.")
    let selectedPriorSource = (selectedPriorReport?["sourceReferences"]
        as? [[String: Any]])?.first
    expect("selected report roundtrips its authoring-time source relation",
           selectedPriorSource?["relationship"] as? String, "same_item_at_authorship")
    check("lazy report response remains inside the Store byte budget",
          (try? JSONSerialization.data(withJSONObject: selectedPrior.body).count) ?? Int.max
              <= 1_000_000)
    expect("an unknown report selector is typed",
           boardError(reports.store.reportSnapshot(
                project: "project-1", item: closed, report: "missing-report")),
           "report_not_found")
    let coordinationReportID = (reports.item(coordination)["completionReport"]
        as? [String: Any])?["id"] as? String ?? ""
    expect("a report selector cannot cross item ownership",
           boardError(reports.store.reportSnapshot(
                project: "project-1", item: closed, report: coordinationReportID)),
           "report_item_mismatch")
    for _ in 3...16 {
        _ = reports.send("record_report", reportFields, actor: "owning-root", trusted: true)
    }
    expect("report version capacity refuses overflow without eviction",
           boardError(reports.send("record_report", reportFields, actor: "owning-root",
                                   trusted: true)),
           "report_history_capacity_reached")
    expect("all bounded report versions remain after overflow refusal",
           (reports.item(closed)["completionReportHistory"] as? [[String: Any]])?.count, 15)
    durableReportState = try! JSONSerialization.jsonObject(with: Data(contentsOf: reports.file))
        as! [String: Any]
    durableReportItems = durableReportState["items"] as! [[String: Any]]
    durableVersions = durableReportItems.first { $0["id"] as? String == closed }?["completionReports"]
        as! [[String: Any]]
    expect("overflow refusal leaves every report body on disk", durableVersions.count, 16)
    _ = reports.send("update", ["itemId": closed, "summary": "new scope after closure"])
    report = reports.item(closed)["completionReport"] as? [String: Any]
    expect("new scope preserves but qualifies the prior report",
           report?["status"] as? String, "historical_needs_update")
    let reloadedReport = (ProjectBoardStore(url: reports.file).snapshot(item: closed)["board"]
        as? [String: Any])?["item"] as? [String: Any]
    expect("report provenance and historical qualification survive reload",
           (reloadedReport?["completionReport"] as? [String: Any])?["status"] as? String,
           "historical_needs_update")
    var oldSchemaState = try! JSONSerialization.jsonObject(with: Data(contentsOf: reports.file))
        as! [String: Any]
    var oldSchemaItems = oldSchemaState["items"] as! [[String: Any]]
    let oldSchemaIndex = oldSchemaItems.firstIndex { $0["id"] as? String == closed }!
    var oldSchemaReports = oldSchemaItems[oldSchemaIndex]["completionReports"] as! [[String: Any]]
    var oldSchemaSources = oldSchemaReports[0]["sourceReferences"] as! [[String: Any]]
    oldSchemaSources[0].removeValue(forKey: "resolvedAt")
    oldSchemaReports[0]["sourceReferences"] = oldSchemaSources
    oldSchemaItems[oldSchemaIndex]["completionReports"] = oldSchemaReports
    oldSchemaState["items"] = oldSchemaItems
    try! JSONSerialization.data(withJSONObject: oldSchemaState, options: [.sortedKeys])
        .write(to: reports.file, options: .atomic)
    let oldSchemaSelection = ProjectBoardStore(url: reports.file).reportSnapshot(
        project: "project-1", item: closed, report: firstReportID)
    let oldSchemaBoard = oldSchemaSelection.body["board"] as? [String: Any]
    let oldSchemaReport = oldSchemaBoard?["reportSelection"] as? [String: Any]
    let oldSchemaSource = (oldSchemaReport?["sourceReferences"] as? [[String: Any]])?.first
    expect("older source rows inherit the report authorship epoch",
           oldSchemaSource?["resolvedAt"] as? Double, reportClock - 10)
    expect("older source rows still expose the explicit authoring-time discriminator",
           oldSchemaSource?["relationship"] as? String, "same_item_at_authorship")
}

group("six item types, hierarchy, relations, handoff, and spans retain their distinct meanings") {
    let d = BoardTestDriver(name: "domain-\(UUID().uuidString)")
    d.createProject()
    let types = ["feature", "refactor", "task", "bug", "coordination", "epic"]
    var ids: [String] = []
    for type in types { ids.append(d.create(type: type, title: type)) }
    expect("all six stable item types are stored", Set(boardItems(d.store).compactMap {
        $0["type"] as? String
    }), Set(types))
    let epic = ids[5]
    let child = d.create(type: "feature", title: "child", parent: epic)
    expect("a Feature may be a direct Epic child", d.item(child)["parentId"] as? String, epic)
    expect("nesting below a non-Epic is refused",
           boardError(d.send("create", ["projectId": "project-1", "title": "too deep",
                                         "type": "task", "owner": "root",
                                         "parentId": child])), "invalid_parent")
    expect("an Epic with children cannot lose its container type",
           boardError(d.send("update", ["itemId": epic, "type": "feature"])),
           "container_has_children")

    expect("a first directed item relation is accepted",
           d.send("link", ["itemId": ids[0], "kind": "blocks", "targetId": ids[1],
                           "label": "first"]).status, 200)
    expect("the reverse relation cycle is refused",
           boardError(d.send("link", ["itemId": ids[1], "kind": "blocks",
                                       "targetId": ids[0], "label": "cycle"])),
           "relation_cycle")
    let userLinks = d.item(ids[0])["links"] as? [[String: Any]]
    expect("user-created relations cannot masquerade as broker accounting links",
           userLinks?.first?["source"] as? String, "user")

    let handed = ids[4]
    expect("handoff proposal succeeds without transferring owner",
           d.send("handoff", ["itemId": handed, "owner": "receiver", "note": "take it"]).status,
           200)
    expect("proposal preserves effective owner", d.item(handed)["owner"] as? String, "root")
    expect("someone else cannot ACK the handoff",
           boardError(d.send("accept_handoff", ["itemId": handed, "note": "mine"],
                             actor: "intruder")), "handoff_receiver_required")
    expect("the proposed receiver can atomically take ownership",
           d.send("accept_handoff", ["itemId": handed, "note": "accepted"],
                  actor: "receiver").status, 200)
    expect("accepted handoff changes owner", d.item(handed)["owner"] as? String, "receiver")
    check("accepted handoff clears pending transfer", d.item(handed)["handoff"] is NSNull)

    _ = d.send("span", ["itemId": ids[0], "sessionId": "shared-session", "phase": "planning"])
    _ = d.send("span", ["itemId": ids[1], "sessionId": "shared-session", "phase": "output"])
    let oldSpan = (d.item(ids[0])["spans"] as? [[String: Any]])?.first
    let newSpan = (d.item(ids[1])["spans"] as? [[String: Any]])?.first
    check("switching item closes the session's prior span", oldSpan?["endedAt"] is Double)
    check("the new declared span remains active", newSpan?["endedAt"] is NSNull)
    expect("phase vocabulary is closed",
           boardError(d.send("span", ["itemId": ids[0], "sessionId": "s",
                                       "phase": "graph_delivery"])), "invalid_phase")

    let closedEpic = d.create(type: "epic", title: "closed epic")
    boardAdvanceToExecution(d, item: closedEpic)
    _ = d.send("record_evidence", ["itemId": closedEpic, "kind": "verification",
                                    "summary": "exact", "subject": "epic-commit",
                                    "status": "passed", "sourceId": "epic-proof"], trusted: true)
    _ = d.store.ingest(task: [
        "id": "epic-landing", "workItemId": closedEpic,
        "landing": ["state": "landed", "verification_origin": "git",
                    "verified_commit": "epic-commit", "verified_target_commit": "target"],
    ], projectID: "project-1")
    _ = d.send("transition", ["itemId": closedEpic, "state": "verified"])
    _ = d.send("transition", ["itemId": closedEpic, "state": "integrated"])
    _ = d.send("transition", ["itemId": closedEpic, "state": "closed"])
    expect("a closed Epic requires explicit reopening before accepting a new child",
           boardError(d.send("create", ["projectId": "project-1", "title": "late child",
                                         "type": "task", "owner": "root",
                                         "parentId": closedEpic])), "parent_locked")
    expect("a closed item requires explicit reopening before a handoff",
           boardError(d.send("handoff", ["itemId": closedEpic, "owner": "receiver",
                                          "note": "late"])), "item_locked")
}

group("lifecycle promotion requires current trusted evidence of the right delivery type") {
    let d = BoardTestDriver(name: "lifecycle-\(UUID().uuidString)")
    d.createProject()
    let item = d.create(type: "feature", title: "Code feature")
    let checklistReply = d.send("checklist", ["itemId": item, "title": "exact tests",
                                               "required": true])
    let checklist = ((checklistReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["checklist"] as? [[String: Any]]
    let checklistID = checklist?.first?["id"] as? String ?? ""
    _ = d.send("checklist", ["itemId": item, "checklistId": checklistID, "status": "passed"])
    boardAdvanceToExecution(d, item: item)
    expect("a checked box with no evidence cannot verify code",
           boardError(d.send("transition", ["itemId": item, "state": "verified"])),
           "evidence_required")
    let finding: [String: Any] = [
        "itemId": item, "kind": "finding", "summary": "blocking defect",
        "subject": "commit-a", "status": "open", "sourceId": "finding-a",
        "blocking": true,
    ]
    _ = d.send("record_evidence", finding, trusted: true)
    var resolvedFinding = finding
    resolvedFinding["summary"] = "blocking defect corrected"
    resolvedFinding["status"] = "resolved"
    expect("trusted source can resolve its durable finding without minting a second row",
           d.send("record_evidence", resolvedFinding, trusted: true).status, 200)
    expect("finding resolution preserves one source row",
           (d.item(item)["findings"] as? [[String: Any]])?.count, 1)
    let evidenceFields: [String: Any] = [
        "itemId": item, "kind": "verification", "summary": "focused and exact proof",
        "subject": "commit-a", "status": "passed", "sourceId": "receipt-a",
        "checklistId": checklistID,
    ]
    expect("ordinary callers cannot mint verification",
           boardError(d.send("record_evidence", evidenceFields)), "trusted_evidence_required")
    expect("a root attestation can record an exact nonempty subject",
           d.send("record_evidence", evidenceFields, trusted: true).status, 200)
    let verification = (d.item(item)["verifications"] as? [[String: Any]])?.first
    expect("root evidence is labelled as attestation", verification?["source"] as? String,
           "root_attestation")
    expect("current evidence advances to verified",
           d.send("transition", ["itemId": item, "state": "verified"]).status, 200)
    expect("verification alone cannot integrate code",
           boardError(d.send("transition", ["itemId": item, "state": "integrated"])),
           "evidence_required")

    d.store.ingest(task: [
        "id": "task-wrong", "workItemId": item, "state": "success",
        "landing": ["state": "landed", "verification_origin": "git",
                    "verified_commit": "commit-old", "verified_target_commit": "target-a"],
    ], projectID: "project-1")
    expect("mismatched but broker-verified landing remains historical",
           (d.item(item)["landings"] as? [[String: Any]])?.count, 1)
    check("mismatched historical landing is not the current integration proof",
          ((d.item(item)["currentEvidence"] as? [String: Any])?["landingId"] is NSNull))
    expect("a newer delivery attempt after verification automatically reopens code",
           d.item(item)["state"] as? String, "execution")
    d.store.ingest(task: [
        "id": "task-right", "workItemId": item, "state": "success",
        "landing": ["state": "landed", "verification_origin": "git",
                    "verified_commit": "commit-a", "verified_target_commit": "target-a"],
    ], projectID: "project-1")
    let landing = (d.item(item)["landings"] as? [[String: Any]])?.first
    expect("matching landing ancestry is labelled broker", landing?["source"] as? String, "broker")
    expect("matching broker-verified landing closes safe code automatically",
           d.item(item)["state"] as? String, "closed")
    expect("conversation-recorded scope can reopen integrated work automatically",
           d.send("update", ["itemId": item, "summary": "new scope"]).status, 200)
    let afterEdit = d.item(item)
    expect("scope edit keeps immutable historical verification", (afterEdit["verifications"] as? [[String: Any]])?.count, 1)
    expect("scope edit invalidates checklist evidence pointer",
           ((afterEdit["checklist"] as? [[String: Any]])?.first?["evidenceId"] as? String), nil)
    expect("stale evidence cannot immediately re-verify edited scope",
           boardError(d.send("transition", ["itemId": item, "state": "verified"])),
           "evidence_required")

    let document = d.create(type: "task", title: "Guide")
    let milestoneReply = d.send("milestone", ["itemId": document, "title": "published"])
    let milestoneID = ((((milestoneReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["milestones"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    _ = d.send("milestone", ["itemId": document, "milestoneId": milestoneID,
                              "status": "passed"])
    let artifactReply = d.send("artifact", ["itemId": document, "title": "Guide",
                                               "url": "https://example.com/guide", "kind": "document"])
    let artifactID = ((((artifactReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["artifacts"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    expect("executable artifact schemes are refused",
           boardError(d.send("artifact", ["itemId": document, "title": "bad",
                                           "url": "javascript:alert(1)", "kind": "other"])),
           "invalid_artifact_url")
    boardAdvanceToExecution(d, item: document)
    expect("a document URL alone cannot verify its Task",
           boardError(d.send("transition", ["itemId": document, "state": "verified"])),
           "evidence_required")
    expect("artifact acceptance also requires the trusted boundary",
           boardError(d.send("accept_artifact", ["itemId": document,
                                                  "artifactId": artifactID])),
           "trusted_evidence_required")
    _ = d.send("accept_artifact", ["itemId": document, "artifactId": artifactID,
                                    "note": "content checked"], trusted: true)
    expect("accepted document Task automatically closes without counterfeit Git landing",
           d.item(document)["state"] as? String, "closed")
    expect("accepted non-code artifact is visibly complete",
           boardProgress(d.item(document))["state"] as? String, "landed")
    expect("artifact completion basis cannot masquerade as Git ancestry",
           (boardProgress(d.item(document))["basisCodes"] as? [String])?.first,
           "authoritative_artifact_acceptance")
    let obligationReply = d.send("obligation", ["itemId": document, "title": "notify owner",
                                                   "owner": "root", "blocking": true])
    let obligationID = ((((obligationReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["obligations"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    expect("new obligation automatically reopens otherwise delivered work",
           d.item(document)["state"] as? String, "integrated")
    expect("open blocking obligation prevents closure",
           boardError(d.send("transition", ["itemId": document, "state": "closed"])),
           "closure_obligations_open")
    _ = d.send("resolve_obligation", ["itemId": document, "obligationId": obligationID,
                                       "note": "owner notified"])
    expect("settled non-code Task closes automatically",
           d.item(document)["state"] as? String, "closed")

    let renewed = d.create(type: "task", title: "Renewed guide")
    let requiredReply = d.send("checklist", ["itemId": renewed, "title": "manager review",
                                              "required": true])
    let requiredID = ((((requiredReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["checklist"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    _ = d.send("checklist", ["itemId": renewed, "checklistId": requiredID,
                              "status": "passed"])
    let renewedArtifactReply = d.send("artifact", ["itemId": renewed, "title": "Guide",
                                                     "url": "https://example.com/renewed",
                                                     "kind": "document"])
    let renewedArtifact = ((((renewedArtifactReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["artifacts"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    boardAdvanceToExecution(d, item: renewed)
    expect("manager acceptance can attest a passed non-code checklist without Git evidence",
           d.send("accept_artifact", ["itemId": renewed, "artifactId": renewedArtifact,
                                       "note": "manager checked artifact and checklist"],
                  trusted: true).status, 200)
    expect("the accepted non-code checklist closes on one artifact subject",
           d.item(renewed)["state"] as? String, "closed")
    _ = d.send("update", ["itemId": renewed, "summary": "scope revision two"])
    check("renewed Task scope is no longer visibly delivered before re-acceptance",
          boardProgress(d.item(renewed))["state"] as? String != "landed")
    expect("the same artifact can be renewed after scope invalidation",
           d.send("accept_artifact", ["itemId": renewed, "artifactId": renewedArtifact,
                                       "note": "manager rechecked revised scope"],
                  trusted: true).status, 200)
    expect("artifact renewal keeps both immutable acceptance events",
           (d.item(renewed)["artifactAcceptances"] as? [[String: Any]])?.count, 2)
    expect("renewed artifact returns the Task to visible delivery",
           boardProgress(d.item(renewed))["state"] as? String, "landed")

    let mixed = d.create(type: "task", title: "Mixed delivery")
    let mixedDocumentReply = d.send("artifact", ["itemId": mixed, "title": "Guide",
                                                   "url": "https://example.com/mixed-guide",
                                                   "kind": "document"])
    let mixedDocument = ((((mixedDocumentReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["artifacts"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    _ = d.send("artifact", ["itemId": mixed, "title": "Commit",
                             "url": "https://example.com/commit", "kind": "commit"])
    boardAdvanceToExecution(d, item: mixed)
    _ = d.send("accept_artifact", ["itemId": mixed, "artifactId": mixedDocument,
                                    "note": "document checked"], trusted: true)
    check("a mixed commit and document Task does not treat document acceptance as current code proof",
          ((d.item(mixed)["currentEvidence"] as? [String: Any])?["artifactAcceptanceId"]
              is NSNull))
    expect("a mixed Task requires code verification under the same predicate used for delivery",
           boardError(d.send("transition", ["itemId": mixed, "state": "verified"])),
           "evidence_required")

    let nonCodeCurrent = d.create(type: "task", title: "Current artifact guard")
    let currentArtifactReply = d.send("artifact", ["itemId": nonCodeCurrent, "title": "Guide",
                                                     "url": "https://example.com/current-guide",
                                                     "kind": "document"])
    let currentArtifact = ((((currentArtifactReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["artifacts"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    boardAdvanceToExecution(d, item: nonCodeCurrent)
    _ = d.send("accept_artifact", ["itemId": nonCodeCurrent, "artifactId": currentArtifact,
                                    "note": "manager accepted"], trusted: true)
    _ = d.send("record_evidence", ["itemId": nonCodeCurrent, "kind": "verification",
                                    "summary": "supplemental proof", "subject": currentArtifact,
                                    "status": "passed", "sourceId": "supplemental-proof"],
               trusted: true)
    let nonCodeEvidence = d.item(nonCodeCurrent)["currentEvidence"] as? [String: Any]
    check("supplemental verification cannot clear a non-code Task's current artifact acceptance",
          !(nonCodeEvidence?["artifactAcceptanceId"] is NSNull))
    expect("closed rechecks coherent current verification and delivery predicates",
           d.send("transition", ["itemId": nonCodeCurrent, "state": "closed"]).status, 200)

    let split = d.create(type: "feature", title: "Split proof")
    let rowAReply = d.send("checklist", ["itemId": split, "title": "A", "required": true])
    let rowAItem = (rowAReply.body["board"] as? [String: Any])?["item"] as? [String: Any]
    let rowA = ((rowAItem?["checklist"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    let rowBReply = d.send("checklist", ["itemId": split, "title": "B", "required": true])
    let rowBItem = (rowBReply.body["board"] as? [String: Any])?["item"] as? [String: Any]
    let rows = (rowBItem?["checklist"] as? [[String: Any]]) ?? []
    let rowB = rows.first { $0["id"] as? String != rowA }?["id"] as? String ?? ""
    _ = d.send("checklist", ["itemId": split, "checklistId": rowA, "status": "passed"])
    _ = d.send("checklist", ["itemId": split, "checklistId": rowB, "status": "passed"])
    boardAdvanceToExecution(d, item: split)
    _ = d.send("record_evidence", ["itemId": split, "kind": "verification",
                                    "summary": "A proof", "subject": "subject-a",
                                    "status": "passed", "sourceId": "split-a",
                                    "checklistId": rowA], trusted: true)
    _ = d.send("record_evidence", ["itemId": split, "kind": "verification",
                                    "summary": "B proof", "subject": "subject-b",
                                    "status": "passed", "sourceId": "split-b",
                                    "checklistId": rowB], trusted: true)
    expect("required checklist proofs from different subjects cannot compose",
           boardError(d.send("transition", ["itemId": split, "state": "verified"])),
           "evidence_subject_mismatch")

    let failed = d.create(type: "feature", title: "Failed after integration")
    boardAdvanceToExecution(d, item: failed)
    _ = d.send("record_evidence", ["itemId": failed, "kind": "verification",
                                    "summary": "passed", "subject": "failed-commit",
                                    "status": "passed", "sourceId": "failed-pass"], trusted: true)
    _ = d.store.ingest(task: [
        "id": "failed-landing", "workItemId": failed,
        "landing": ["state": "landed", "verification_origin": "git",
                    "verified_commit": "failed-commit", "verified_target_commit": "target"],
    ], projectID: "project-1")
    _ = d.send("transition", ["itemId": failed, "state": "verified"])
    _ = d.send("transition", ["itemId": failed, "state": "integrated"])
    _ = d.send("record_evidence", ["itemId": failed, "kind": "verification",
                                    "summary": "regressed", "subject": "failed-commit",
                                    "status": "failed", "sourceId": "failed-red"], trusted: true)
    expect("failed current verification reopens integrated work", d.item(failed)["state"] as? String,
           "execution")
    check("failed verification clears the current landing",
          ((d.item(failed)["currentEvidence"] as? [String: Any])?["landingId"] is NSNull))
}

group("board progress reconciles broker attempts and immutable evidence without human transitions") {
    var clock = 1_800_000_000.0
    let d = BoardTestDriver(name: "automatic-progress-\(UUID().uuidString)",
                            now: { Date(timeIntervalSince1970: clock) })
    d.createProject()
    let graph: [String: Any] = [
        "id": "automatic-graph", "destination": "Automatic feature",
        "current_node": "delivery", "kind": "delivery",
    ]
    let base = clock
    let delivered: [String: Any] = [
        "id": "attempt-delivered", "title": "delivery", "state": "success",
        "workPhase": "output", "assistant": "codex", "graph": graph,
        "startedAt": base, "finishedAt": base + 5,
    ]
    _ = d.store.ingest(task: delivered, projectID: "project-1")
    let itemID = boardItems(d.store).first?["id"] as? String ?? ""
    let deliveredItem = d.item(itemID)
    expect("task success automatically starts lifecycle without claiming verification",
           deliveredItem["state"] as? String, "execution")
    let deliveredProgress = boardProgress(deliveredItem)
    expect("task success is displayed as delivery rather than completion",
           deliveredProgress["state"] as? String, "delivered")
    expect("finished delivery is historical rather than active",
           deliveredProgress["historical"] as? Bool, true)
    expect("attempt counters retain the delivered fact",
           (deliveredProgress["attemptCounts"] as? [String: Any])?["succeeded"] as? Int, 1)

    var landed = delivered
    landed["landing"] = [
        "state": "landed", "verification_origin": "local_target_branch",
        "verified_commit": "commit-auto", "verified_target_commit": "target-auto",
        "landed_at": base + 10,
    ]
    clock = base + 10
    _ = d.store.ingest(task: landed, projectID: "project-1")
    let historicalLanding = d.item(itemID)
    expect("trusted historical landing is visible even without exact item verification",
           boardProgress(historicalLanding)["state"] as? String, "landed")
    expect("historical landing does not forge stronger lifecycle completion",
           historicalLanding["state"] as? String, "execution")
    check("historical landing is not installed as current exact proof",
          ((historicalLanding["currentEvidence"] as? [String: Any])?["landingId"] is NSNull))

    clock = base + 15
    _ = d.send("record_evidence", [
        "itemId": itemID, "kind": "verification", "summary": "exact candidate passed",
        "subject": "commit-auto", "status": "passed", "sourceId": "exact-auto",
    ], trusted: true)
    expect("verification observed after matching landing closes safe work without controls",
           d.item(itemID)["state"] as? String, "closed")
    check("out-of-order proof binds the pre-existing matching landing",
          !((d.item(itemID)["currentEvidence"] as? [String: Any])?["landingId"] is NSNull))

    let activeCorrection: [String: Any] = [
        "id": "attempt-correction", "title": "correction", "state": "briefed",
        "workPhase": "correction", "assistant": "codex", "graph": graph,
        "startedAt": base + 20,
    ]
    clock = base + 20
    _ = d.store.ingest(task: activeCorrection, projectID: "project-1")
    let mixed = boardProgress(d.item(itemID))
    expect("new active correction outranks older landing history",
           mixed["state"] as? String, "correction")
    expect("mixed landed and active attempts remain visibly active", mixed["active"] as? Bool, true)
    expect("multi-attempt counts retain both attempts",
           (mixed["attemptCounts"] as? [String: Any])?["total"] as? Int, 2)

    var finishedCorrection = activeCorrection
    finishedCorrection["state"] = "success"
    finishedCorrection["finishedAt"] = base + 30
    clock = base + 30
    _ = d.store.ingest(task: finishedCorrection, projectID: "project-1")
    expect("new delivered attempt after an old landing remains a new round",
           boardProgress(d.item(itemID))["state"] as? String, "delivered")
    clock = base + 35
    _ = d.send("record_evidence", [
        "itemId": itemID, "kind": "verification", "summary": "corrected candidate passed",
        "subject": "commit-next", "status": "passed", "sourceId": "exact-next",
    ], trusted: true)
    expect("new exact proof advances the new round only to verified",
           d.item(itemID)["state"] as? String, "verified")
    var newlyLanded = finishedCorrection
    newlyLanded["landing"] = [
        "state": "landed", "verification_origin": "local_target_branch",
        "verified_commit": "commit-next", "verified_target_commit": "target-next",
        "landed_at": base + 40,
    ]
    clock = base + 40
    _ = d.store.ingest(task: newlyLanded, projectID: "project-1")
    expect("newer authoritative landing completes the newer round",
           d.item(itemID)["state"] as? String, "closed")

    clock = base + 45
    expect("conversation-recorded new scope reopens closed work without a state control",
           d.send("update", ["itemId": itemID, "summary": "new scope"]).status, 200)
    let reopened = d.item(itemID)
    expect("new scope cannot remain complete", reopened["state"] as? String, "execution")
    expect("old landing after new scope is visibly correction, not landed",
           boardProgress(reopened)["state"] as? String, "correction")
    check("new scope retains immutable historical landing evidence",
          (reopened["landings"] as? [[String: Any]])?.isEmpty == false)
    let replayRevision = d.revision
    let replayLandingCount = (reopened["landings"] as? [[String: Any]])?.count
    _ = d.store.ingest(task: newlyLanded, projectID: "project-1")
    expect("replaying an old landing after new scope does not mint current-scope evidence",
           (d.item(itemID)["landings"] as? [[String: Any]])?.count, replayLandingCount)
    expect("old landing replay does not churn the durable board", d.revision, replayRevision)
    expect("old landing replay cannot regress the new round to landed",
           boardProgress(d.item(itemID))["state"] as? String, "correction")

    let failed = d.create(type: "feature", title: "Failed proof")
    boardAdvanceToExecution(d, item: failed)
    clock = base + 46
    _ = d.send("record_evidence", [
        "itemId": failed, "kind": "verification", "summary": "first pass",
        "subject": "failed-subject", "status": "passed", "sourceId": "failed-pass-auto",
    ], trusted: true)
    clock = base + 47
    _ = d.send("record_evidence", [
        "itemId": failed, "kind": "verification", "summary": "regression",
        "subject": "failed-subject", "status": "failed", "sourceId": "failed-red-auto",
    ], trusted: true)
    expect("failed current proof reopens lifecycle automatically",
           d.item(failed)["state"] as? String, "execution")
    expect("failed proof without a finding is blocked rather than landed",
           boardProgress(d.item(failed))["state"] as? String, "blocked")

    clock = base + 50
    _ = d.store.ingest(task: [
        "id": "ungraphed-canceled", "title": "Historic maintenance",
        "state": "cancelled", "assistant": "claude", "finishedAt": 300.0,
    ], projectID: "project-1")
    let ungraphed = boardItems(d.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["targetId"] as? String == "ungraphed-canceled"
        } == true
    } ?? [:]
    expect("ungraphed retained execution becomes a Task, never a guessed Feature",
           ungraphed["type"] as? String, "task")
    expect("attempt cancellation stays separate from item closure",
           ungraphed["state"] as? String, "execution")
    expect("all-canceled attempt history is visibly canceled",
           boardProgress(ungraphed)["state"] as? String, "canceled")

    let landedOverOldFinding = d.create(type: "feature", title: "Historical finding")
    clock = base + 55
    _ = d.store.ingest(task: [
        "id": "old-review-and-landing", "title": "old delivery", "state": "success",
        "workItemId": landedOverOldFinding, "assistant": "codex",
        "startedAt": base + 50, "finishedAt": base + 55,
        "review": ["axes": [["findings": [[
            "id": "old-blocker", "summary": "administrative finding", "severity": "blocking",
        ]]]]],
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "historical-commit", "verified_target_commit": "historical-target",
            "landed_at": base + 60,
        ],
    ], projectID: "project-1")
    let authoritative = boardProgress(d.item(landedOverOldFinding))
    expect("newer authoritative landing outranks an older unresolved finding",
           authoritative["state"] as? String, "landed")
    check("older procedural finding remains a visible warning",
          (authoritative["warningCodes"] as? [String])?.contains("historical_findings_unresolved") == true)
    clock = base + 65
    _ = d.send("record_evidence", [
        "itemId": landedOverOldFinding, "kind": "verification",
        "summary": "later regression", "subject": "historical-commit",
        "status": "failed", "sourceId": "post-landing-failure",
    ], trusted: true)
    expect("failed proof newer than landing opens correction without erasing delivery",
           boardProgress(d.item(landedOverOldFinding))["state"] as? String, "correction")
    clock = base + 70
    _ = d.store.ingest(task: [
        "id": "post-landing-active", "title": "new correction", "state": "briefed",
        "workItemId": landedOverOldFinding, "workPhase": "correction",
        "assistant": "codex", "startedAt": base + 70,
    ], projectID: "project-1")
    expect("newer correction remains visible over retained authoritative landing",
           boardProgress(d.item(landedOverOldFinding))["state"] as? String, "correction")
    let reloaded = ProjectBoardStore(url: d.file)
    _ = reloaded.ingest(task: [
        "id": "old-review-and-landing", "title": "old delivery", "state": "success",
        "workItemId": landedOverOldFinding, "assistant": "codex",
        "startedAt": base + 50, "finishedAt": base + 55,
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "historical-commit", "verified_target_commit": "historical-target",
            "landed_at": base + 60,
        ],
    ], projectID: "project-1")
    expect("restart and old-record replay cannot hide the newer active round",
           boardProgress(((reloaded.snapshot(item: landedOverOldFinding)["board"]
                as? [String: Any])?["item"] as? [String: Any]) ?? [:])["state"] as? String,
           "correction")

    let terminalOrder = d.create(type: "feature", title: "Terminal chronology")
    clock = base + 80
    _ = d.store.ingest(task: [
        "id": "terminal-success", "title": "older success", "state": "success",
        "workItemId": terminalOrder, "assistant": "codex",
        "startedAt": base + 75, "finishedAt": base + 80,
    ], projectID: "project-1")
    clock = base + 90
    _ = d.store.ingest(task: [
        "id": "terminal-failure", "title": "newer failure", "state": "failure",
        "workItemId": terminalOrder, "assistant": "codex",
        "startedAt": base + 85, "finishedAt": base + 90,
    ], projectID: "project-1")
    expect("a later failed attempt outranks an older successful attempt",
           boardProgress(d.item(terminalOrder))["state"] as? String, "blocked")

    let tiedOrder = d.create(type: "feature", title: "Tied chronology")
    clock = base + 100
    _ = d.store.ingest(task: [
        "id": "tied-success", "title": "first observation", "state": "success",
        "workItemId": tiedOrder, "assistant": "codex", "finishedAt": base + 100,
    ], projectID: "project-1")
    _ = d.store.ingest(task: [
        "id": "tied-failure", "title": "second observation", "state": "failure",
        "workItemId": tiedOrder, "assistant": "codex", "finishedAt": base + 100,
    ], projectID: "project-1")
    expect("same-time terminal attempts use their persisted observation order",
           boardProgress(d.item(tiedOrder))["state"] as? String, "blocked")

    let staleTask = d.create(type: "feature", title: "Stale snapshot")
    clock = base + 110
    let latestFailure: [String: Any] = [
        "id": "same-task-snapshot", "title": "one immutable attempt", "state": "failure",
        "workItemId": staleTask, "assistant": "codex",
        "startedAt": base + 105, "finishedAt": base + 110,
    ]
    _ = d.store.ingest(task: latestFailure, projectID: "project-1")
    let staleStableRevision = d.revision
    clock = base + 120
    _ = d.store.ingest(task: [
        "id": "same-task-snapshot", "title": "stale terminal snapshot", "state": "success",
        "workItemId": staleTask, "assistant": "codex",
        "startedAt": base + 95, "finishedAt": base + 100,
    ], projectID: "project-1")
    expect("an older snapshot of one task cannot overwrite its terminal facts",
           boardProgress(d.item(staleTask))["state"] as? String, "blocked")
    expect("a stale snapshot does not churn the durable board", d.revision, staleStableRevision)

    let staleDerivedFacts = d.create(type: "feature", title: "Stale derived facts")
    var newestReviewedTask: [String: Any] = [
        "id": "reviewed-snapshot", "title": "review execution", "state": "success",
        "workItemId": staleDerivedFacts, "assistant": "codex",
        "startedAt": base + 195, "finishedAt": base + 200,
        "review": ["axes": [["findings": [[
            "id": "newer-finding", "summary": "finding belongs at task finish 200",
            "severity": "blocking",
        ]]]]],
    ]
    clock = base + 200
    _ = d.store.ingest(task: newestReviewedTask, projectID: "project-1")
    clock = base + 205
    _ = d.store.ingest(task: [
        "id": "separate-older-landing", "title": "older landing", "state": "success",
        "workItemId": staleDerivedFacts, "assistant": "codex", "finishedAt": base + 150,
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "separate-old-commit",
            "verified_target_commit": "separate-old-target", "landed_at": base + 155,
        ],
    ], projectID: "project-1")
    expect("a task finding newer than a separate landing opens correction",
           boardProgress(d.item(staleDerivedFacts))["state"] as? String, "correction")
    let derivedStableRevision = d.revision
    newestReviewedTask["startedAt"] = base + 95
    newestReviewedTask["finishedAt"] = base + 100
    clock = base + 210
    _ = d.store.ingest(task: newestReviewedTask, projectID: "project-1")
    expect("a rejected stale task clock cannot rewrite its derived finding chronology",
           boardProgress(d.item(staleDerivedFacts))["state"] as? String, "correction")
    expect("stale derived facts replay without durable churn", d.revision, derivedStableRevision)

    let lateReview = d.create(type: "feature", title: "Late historical review")
    var historicalReceipt: [String: Any] = [
        "id": "late-review-receipt", "title": "historical receipt", "state": "success",
        "workItemId": lateReview, "assistant": "codex",
        "startedAt": base + 125, "finishedAt": base + 130,
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "late-review-commit", "verified_target_commit": "late-review-target",
            "landed_at": base + 140,
        ],
    ]
    clock = base + 150
    _ = d.store.ingest(task: historicalReceipt, projectID: "project-1")
    historicalReceipt["review"] = ["axes": [["findings": [[
        "id": "late-old-finding", "summary": "old review arrived during backfill",
        "severity": "blocking",
    ]]]]]
    clock = base + 500
    _ = d.store.ingest(task: historicalReceipt, projectID: "project-1")
    let lateReviewProgress = boardProgress(d.item(lateReview))
    expect("late ingestion cannot make an old review newer than its landing",
           lateReviewProgress["state"] as? String, "landed")
    check("the old review remains an explicit historical warning",
          (lateReviewProgress["warningCodes"] as? [String])?
            .contains("historical_findings_unresolved") == true)

    let lateLanding = d.create(type: "feature", title: "Late historical landing")
    var lateHistoricalLanding: [String: Any] = [
        "id": "late-landing-receipt", "title": "historical attempt", "state": "success",
        "workItemId": lateLanding, "assistant": "codex",
        "startedAt": base + 155, "finishedAt": base + 160,
    ]
    clock = base + 165
    _ = d.store.ingest(task: lateHistoricalLanding, projectID: "project-1")
    clock = base + 170
    _ = d.send("update", ["itemId": lateLanding, "summary": "scope after the old attempt"])
    lateHistoricalLanding["landing"] = [
        "state": "landed", "verification_origin": "local_target_branch",
        "verified_commit": "late-landing-commit", "verified_target_commit": "late-landing-target",
        "landed_at": base + 162,
    ]
    clock = base + 600
    _ = d.store.ingest(task: lateHistoricalLanding, projectID: "project-1")
    expect("a first-seen historical landing cannot close a newer scope",
           boardProgress(d.item(lateLanding))["state"] as? String, "correction")

    let mixedChronologyRecords: [[String: Any]] = [
        ["id": "mixed-known-new", "title": "known newest", "state": "failure",
         "assistant": "codex", "finishedAt": base + 900],
        ["id": "mixed-missing", "title": "missing boundary", "state": "success",
         "assistant": "codex"],
        ["id": "mixed-known-old", "title": "known older", "state": "success",
         "assistant": "codex", "finishedAt": base + 800],
    ]
    let mixedPermutations = [
        [0, 1, 2], [0, 2, 1], [1, 0, 2],
        [1, 2, 0], [2, 0, 1], [2, 1, 0],
    ]
    for (number, order) in mixedPermutations.enumerated() {
        let item = d.create(type: "feature", title: "Mixed chronology \(number)")
        for index in order {
            var record = mixedChronologyRecords[index]
            record["id"] = "\(record["id"] as! String)-\(number)"
            record["workItemId"] = item
            clock += 1
            _ = d.store.ingest(task: record, projectID: "project-1")
        }
        expect("mixed known/missing order is permutation-stable \(number)",
               boardProgress(d.item(item))["state"] as? String, "blocked")
    }

    let unknownAfterLanding = d.create(type: "feature", title: "Unknown post-landing time")
    clock = base + 930
    _ = d.store.ingest(task: [
        "id": "known-landed-receipt", "title": "known landing", "state": "success",
        "workItemId": unknownAfterLanding, "assistant": "codex",
        "finishedAt": base + 920,
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "unknown-boundary-commit",
            "verified_target_commit": "unknown-boundary-target", "landed_at": base + 925,
        ],
    ], projectID: "project-1")
    clock = base + 940
    _ = d.store.ingest(task: [
        "id": "unknown-time-active", "title": "newly observed correction", "state": "briefed",
        "workItemId": unknownAfterLanding, "workPhase": "correction", "assistant": "codex",
    ], projectID: "project-1")
    let unknownProgress = boardProgress(d.item(unknownAfterLanding))
    expect("a newly observed unknown-time active attempt is not hidden by a known landing",
           unknownProgress["state"] as? String, "correction")
    check("unknown post-landing chronology is explicit rather than manufactured",
          (unknownProgress["warningCodes"] as? [String])?
            .contains("attempt_chronology_unresolved") == true)

    let legacy = BoardTestDriver(name: "legacy-event-order-\(UUID().uuidString)", now: { Date(timeIntervalSince1970: clock) })
    legacy.createProject()
    let legacyItem = legacy.create(type: "feature", title: "Legacy observation clock")
    var legacyReceipt: [String: Any] = [
        "id": "legacy-receipt", "title": "legacy source", "state": "success",
        "workItemId": legacyItem, "assistant": "codex",
        "startedAt": base + 700, "finishedAt": base + 710,
        "review": ["axes": [["findings": [[
            "id": "legacy-finding", "summary": "source-old finding", "severity": "blocking",
        ]]]]],
        "landing": [
            "state": "landed", "verification_origin": "local_target_branch",
            "verified_commit": "legacy-commit", "verified_target_commit": "legacy-target",
            "landed_at": base + 720,
        ],
    ]
    clock = base + 1_000
    _ = legacy.store.ingest(task: legacyReceipt, projectID: "project-1")
    var legacyJSON = try! JSONSerialization.jsonObject(with: Data(contentsOf: legacy.file))
        as! [String: Any]
    var legacyItems = legacyJSON["items"] as! [[String: Any]]
    var legacyEvidence = legacyItems[0]["evidence"] as! [[String: Any]]
    for index in legacyEvidence.indices {
        legacyEvidence[index].removeValue(forKey: "eventAt")
        legacyEvidence[index].removeValue(forKey: "eventOrdinal")
        if legacyEvidence[index]["kind"] as? String == "finding" {
            legacyEvidence[index]["at"] = base + 1_000
        }
    }
    legacyItems[0]["evidence"] = legacyEvidence
    var legacyLinks = legacyItems[0]["links"] as! [[String: Any]]
    for index in legacyLinks.indices where legacyLinks[index]["kind"] as? String == "task" {
        legacyLinks[index].removeValue(forKey: "eventAt")
        legacyLinks[index].removeValue(forKey: "eventOrdinal")
        legacyLinks[index]["statusObservedAt"] = base + 1_000
    }
    legacyItems[0]["links"] = legacyLinks
    legacyJSON["items"] = legacyItems
    try! JSONSerialization.data(withJSONObject: legacyJSON, options: [.sortedKeys])
        .write(to: legacy.file, options: .atomic)
    let upgradedLegacy = ProjectBoardStore(url: legacy.file, now: { Date(timeIntervalSince1970: clock) })
    _ = upgradedLegacy.ingest(task: legacyReceipt, projectID: "project-1")
    let upgradedBoard = (upgradedLegacy.snapshot(item: legacyItem)["board"] as? [String: Any]) ?? [:]
    expect("schema-v1 observation timestamps are repaired from replayed source chronology",
           boardProgress((upgradedBoard["item"] as? [String: Any]) ?? [:])["state"] as? String,
           "landed")
    let upgradedRevision = upgradedBoard["revision"] as? Int
    _ = upgradedLegacy.ingest(task: legacyReceipt, projectID: "project-1")
    expect("legacy chronology repair is durable and replay-idempotent",
           (upgradedLegacy.snapshot()["board"] as? [String: Any])?["revision"] as? Int,
           upgradedRevision)
}

group("type-specific narratives stay typed and coordination has no lifecycle") {
    let d = BoardTestDriver(name: "type-details-\(UUID().uuidString)")
    d.createProject()
    let bugReply = d.send("create", [
        "projectId": "project-1", "title": "Intermittent failure", "type": "bug",
        "owner": "root", "typeDetails": [
            "rootCause": "A stale projection won the race.",
            "lessons": "Hold the observed row still across surfaces.",
        ],
    ])
    let bug = bugReply.body["itemId"] as? String ?? ""
    let bugDetails = d.item(bug)["typeDetails"] as? [String: String]
    expect("Bug retains a named root-cause field", bugDetails?["rootCause"],
           "A stale projection won the race.")
    expect("Bug retains a named lessons field", bugDetails?["lessons"],
           "Hold the observed row still across surfaces.")
    _ = d.send("update", ["itemId": bug,
                            "typeDetails": ["lessons": "Compare the same id."]])
    expect("partial narrative update preserves the other typed Bug field",
           (d.item(bug)["typeDetails"] as? [String: String])?["rootCause"],
           "A stale projection won the race.")
    expect("Bug rejects Coordination narrative keys",
           boardError(d.send("update", ["itemId": bug,
                                         "typeDetails": ["outcomes": "wrong type"]])),
           "invalid_type_details")
    expect("typed narrative strings remain bounded",
           boardError(d.send("update", ["itemId": bug,
                                         "typeDetails": ["lessons": String(repeating: "x", count: 4_001)]])),
           "invalid_type_details")

    let coordinationReply = d.send("create", [
        "projectId": "project-1", "title": "Release coordination", "type": "coordination",
        "owner": "moderator", "typeDetails": [
            "outcomes": "Owners aligned.", "difficulties": "One stale receipt.",
            "improvements": "Persist acknowledgement.",
        ],
    ])
    let coordination = coordinationReply.body["itemId"] as? String ?? ""
    _ = d.store.ingest(task: [
        "id": "coordination-attempt", "title": "coordinate", "state": "briefed",
        "workItemId": coordination, "workPhase": "output", "assistant": "claude",
    ], projectID: "project-1")
    let coordinated = d.item(coordination)
    expect("Coordination broker activity does not enter Feature lifecycle",
           coordinated["state"] as? String, "backlog")
    let coordinationProgress = boardProgress(coordinated)
    expect("Coordination explicitly opts out of lifecycle", coordinationProgress["lifecycleApplicable"] as? Bool,
           false)
    expect("Coordination still exposes active interval context",
           coordinationProgress["active"] as? Bool, true)
    expect("Coordination narrative fields remain named",
           (coordinated["typeDetails"] as? [String: String])?["improvements"],
           "Persist acknowledgement.")
    _ = d.send("record_evidence", [
        "itemId": coordination, "kind": "verification", "summary": "administrative check",
        "subject": "coordination-note", "status": "passed", "sourceId": "coord-proof",
    ], trusted: true)
    expect("Coordination verification does not trigger lifecycle gates",
           d.item(coordination)["state"] as? String, "backlog")
}

group("broker ingestion is idempotent, phase-explicit, and has one accounting owner") {
    let d = BoardTestDriver(name: "ingest-\(UUID().uuidString)")
    d.createProject()
    let task: [String: Any] = [
        "id": "broker-task", "title": "delivery", "state": "success", "workPhase": "correction",
        "graph": ["id": "graph-a", "destination": "Durable Feature",
                  "current_node": "delivery", "kind": "delivery"],
        "child": ["sessionId": "child-session", "terminalId": "%1"],
        "started_at": 1_789_100_000.0, "finished_at": 1_789_100_100.0,
        "worktree": ["path": "/private/worktrees/secret", "branch": "feature/board",
                     "head": "abcdef"],
        "verification": ["runs": 1, "last": "pass"],
    ]
    d.store.ingest(task: task, projectID: "project-1")
    let afterFirst = d.revision
    let fallback = boardItems(d.store).first ?? [:]
    expect("accepted graph identity creates one Feature rather than one card per task",
           fallback["type"] as? String, "feature")
    expect("task success without an attributable owner cannot pass planning safety",
           fallback["state"] as? String, "planning")
    let links = fallback["links"] as? [[String: Any]]
    let taskLink = links?.first { $0["kind"] as? String == "task" }
    expect("ingested task link carries broker provenance", taskLink?["source"] as? String,
           "broker")
    expect("only an explicit workPhase is persisted for accounting", taskLink?["phase"] as? String,
           "correction")
    expect("graph node kind does not overwrite declared phase", taskLink?["phase"] as? String,
           "correction")
    let worktree = links?.first { $0["kind"] as? String == "worktree" }
    expect("worktree link keeps its readable branch", worktree?["label"] as? String,
           "feature/board")
    expect("worktree link retains the observed head", worktree?["head"] as? String, "abcdef")
    check("worktree target is opaque and never exposes its path",
          (worktree?["targetId"] as? String)?.contains("secret") == false)
    let ingestedSpan = (fallback["spans"] as? [[String: Any]])?.first
    expect("broker span keeps the task start boundary", ingestedSpan?["startedAt"] as? Double,
           1_789_100_000.0)
    expect("historical finished task has a closed span", ingestedSpan?["endedAt"] as? Double,
           1_789_100_100.0)
    expect("broker span names its task source", ingestedSpan?["sourceId"] as? String,
           "broker-task")
    expect("child verification is retained only as a summary",
           (fallback["evidenceSummaries"] as? [[String: Any]])?.count, 1)
    expect("child summary does not mint trusted verification",
           (fallback["verifications"] as? [[String: Any]])?.count, 0)
    let accountingTaskLinks = fallback["accountingTaskLinks"] as? [[String: Any]]
    expect("summary projection retains the broker task accounting identity",
           accountingTaskLinks?.first?["targetId"] as? String, "broker-task")
    expect("summary projection retains the declared accounting phase",
           accountingTaskLinks?.first?["phase"] as? String, "correction")
    check("the pre-enrichment snapshot stays inside its explicit one MiB budget",
          (try? JSONSerialization.data(withJSONObject: d.store.snapshot()).count) ?? Int.max
              <= 1_000_000)
    d.store.ingest(task: task, projectID: "project-1")
    expect("unchanged ingest does not advance revision", d.revision, afterFirst)

    let explicit = d.create(type: "feature", title: "Canonical Feature")
    var explicitTask = task
    explicitTask["workItemId"] = explicit
    d.store.ingest(task: explicitTask, projectID: "project-1")
    let all = boardItems(d.store)
    let brokerOwners = all.filter { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["kind"] as? String == "task" && $0["targetId"] as? String == "broker-task"
                && $0["source"] as? String == "broker"
        } == true
    }
    expect("later explicit identity leaves one primary broker allocation", brokerOwners.count, 1)
    expect("the explicit item is the accounting owner", brokerOwners.first?["id"] as? String,
           explicit)
    let fallbackAfter = all.first { $0["id"] as? String == fallback["id"] as? String }
    check("an empty inferred graph card retires after explicit reattribution", fallbackAfter == nil)
    let explicitlyOwned = d.item(explicit)
    expect("reattribution moves the task's session fact exactly once",
           (explicitlyOwned["links"] as? [[String: Any]])?.filter {
               $0["kind"] as? String == "session" && $0["targetId"] as? String == "child-session"
           }.count, 1)
    expect("reattribution moves the task's worktree fact exactly once",
           (explicitlyOwned["links"] as? [[String: Any]])?.filter {
               $0["kind"] as? String == "worktree"
           }.count, 1)
    expect("reattribution moves the task's execution span exactly once",
           (explicitlyOwned["spans"] as? [[String: Any]])?.filter {
               $0["sourceId"] as? String == "broker-task"
           }.count, 1)
    expect("reattribution moves the task's evidence summary exactly once",
           (explicitlyOwned["evidenceSummaries"] as? [[String: Any]])?.filter {
               $0["sourceId"] as? String == "task:broker-task:verification-summary"
           }.count, 1)

    var preservedFallbackTask = task
    preservedFallbackTask["id"] = "preserved-fallback-task"
    preservedFallbackTask["graph"] = [
        "id": "preserved-graph", "destination": "Preserved inferred card",
        "current_node": "delivery", "kind": "delivery",
    ]
    d.store.ingest(task: preservedFallbackTask, projectID: "project-1")
    let preservedFallback = boardItems(d.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["targetId"] as? String == "preserved-fallback-task"
        } == true
    } ?? [:]
    let preservedFallbackID = preservedFallback["id"] as? String ?? ""
    _ = d.send("update", ["itemId": preservedFallbackID,
                           "summary": "Independent operator note must survive."])
    let preservedExplicit = d.create(type: "feature", title: "Preserved canonical card")
    preservedFallbackTask["workItemId"] = preservedExplicit
    d.store.ingest(task: preservedFallbackTask, projectID: "project-1")
    let preservedOld = boardItems(d.store).first { $0["id"] as? String == preservedFallbackID }
    expect("independent manual content preserves the old inferred card",
           preservedOld?["summary"] as? String, "Independent operator note must survive.")
    check("a preserved old card keeps no reattributed task-scoped links",
          (preservedOld?["links"] as? [[String: Any]])?.contains {
              $0["sourceTaskId"] as? String == "preserved-fallback-task"
                  || $0["targetId"] as? String == "preserved-fallback-task"
          } == false)
    check("a preserved old card keeps no reattributed task-scoped spans",
          (preservedOld?["spans"] as? [[String: Any]])?.contains {
              $0["sourceId"] as? String == "preserved-fallback-task"
          } == false)
    check("a preserved old card keeps no reattributed task-scoped evidence",
          (preservedOld?["evidenceSummaries"] as? [[String: Any]])?.contains {
              ($0["sourceId"] as? String)?.hasPrefix("task:preserved-fallback-task:") == true
          } == false)

    var referencedFallbackTask = task
    referencedFallbackTask["id"] = "referenced-fallback-task"
    referencedFallbackTask["graph"] = [
        "id": "referenced-graph", "destination": "Referenced inferred card",
        "current_node": "delivery", "kind": "delivery",
    ]
    d.store.ingest(task: referencedFallbackTask, projectID: "project-1")
    let referencedFallbackID = boardItems(d.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["targetId"] as? String == "referenced-fallback-task"
        } == true
    }?["id"] as? String ?? ""
    let coordinator = d.create(type: "coordination", title: "Incoming relation owner")
    _ = d.send("link", ["itemId": coordinator, "kind": "coordinates",
                         "targetId": referencedFallbackID, "label": "Coordinates"])
    let referencedExplicit = d.create(type: "feature", title: "Referenced canonical card")
    referencedFallbackTask["workItemId"] = referencedExplicit
    d.store.ingest(task: referencedFallbackTask, projectID: "project-1")
    check("an incoming item relation preserves an otherwise-empty inferred card",
          boardItems(d.store).contains { $0["id"] as? String == referencedFallbackID })
    check("safe cleanup never leaves the incoming relation dangling",
          (d.item(coordinator)["links"] as? [[String: Any]])?.contains {
              $0["kind"] as? String == "coordinates"
                  && $0["targetId"] as? String == referencedFallbackID
          } == true)
    check("the relation-preserved card still loses reattributed execution facts",
          (d.item(referencedFallbackID)["links"] as? [[String: Any]])?.contains {
              $0["sourceTaskId"] as? String == "referenced-fallback-task"
                  || $0["targetId"] as? String == "referenced-fallback-task"
          } == false)

    var sharedReferenceA = task
    sharedReferenceA["id"] = "shared-reference-a"
    sharedReferenceA["graph"] = [
        "id": "shared-reference-graph", "destination": "Shared reference fallback",
        "current_node": "delivery", "kind": "delivery",
    ]
    sharedReferenceA["child"] = ["sessionId": "shared-session", "terminalId": "%shared"]
    sharedReferenceA["worktree"] = ["path": "/private/worktrees/shared",
                                     "branch": "shared", "head": "shared-head"]
    d.store.ingest(task: sharedReferenceA, projectID: "project-1")
    var sharedReferenceB = sharedReferenceA
    sharedReferenceB["id"] = "shared-reference-b"
    d.store.ingest(task: sharedReferenceB, projectID: "project-1")
    let sharedFallbackID = boardItems(d.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["targetId"] as? String == "shared-reference-a"
        } == true
    }?["id"] as? String ?? ""
    let sharedExplicit = d.create(type: "feature", title: "Shared reference canonical")
    sharedReferenceA["workItemId"] = sharedExplicit
    d.store.ingest(task: sharedReferenceA, projectID: "project-1")
    let sharedFallbackLinks = d.item(sharedFallbackID)["links"] as? [[String: Any]]
    check("reattributing A preserves B's shared Session provenance without replaying B",
          sharedFallbackLinks?.contains {
              $0["kind"] as? String == "session" && $0["targetId"] as? String == "shared-session"
                  && $0["sourceTaskId"] as? String == "shared-reference-b"
          } == true)
    check("reattributing A preserves B's shared worktree provenance without replaying B",
          sharedFallbackLinks?.contains {
              $0["kind"] as? String == "worktree"
                  && $0["sourceTaskId"] as? String == "shared-reference-b"
          } == true)
    check("the shared fallback retains only B's task allocation",
          sharedFallbackLinks?.contains {
              $0["kind"] as? String == "task" && $0["targetId"] as? String == "shared-reference-b"
          } == true && sharedFallbackLinks?.contains {
              $0["kind"] as? String == "task" && $0["targetId"] as? String == "shared-reference-a"
          } == false)

    var pointerTask = task
    pointerTask["id"] = "pointer-task"
    pointerTask["graph"] = [
        "id": "pointer-graph", "destination": "Pointer fallback",
        "current_node": "delivery", "kind": "delivery",
    ]
    pointerTask["landing"] = [
        "state": "landed", "verification_origin": "local_target_branch",
        "verified_commit": "fallback-commit", "verified_target_commit": "fallback-target",
        "landed_at": 1_789_100_200.0,
    ]
    d.store.ingest(task: pointerTask, projectID: "project-1")
    let pointerFallbackID = boardItems(d.store).first { item in
        (item["links"] as? [[String: Any]])?.contains {
            $0["targetId"] as? String == "pointer-task"
        } == true
    }?["id"] as? String ?? ""
    _ = d.send("record_evidence", [
        "itemId": pointerFallbackID, "kind": "verification", "summary": "fallback exact proof",
        "subject": "fallback-commit", "status": "passed", "sourceId": "fallback-exact",
    ], trusted: true)
    d.store.ingest(task: pointerTask, projectID: "project-1")
    let pointerExplicit = d.create(type: "feature", title: "Pointer canonical")
    _ = d.send("record_evidence", [
        "itemId": pointerExplicit, "kind": "verification", "summary": "destination proof",
        "subject": "destination-commit", "status": "passed", "sourceId": "destination-exact",
    ], trusted: true)
    pointerTask["workItemId"] = pointerExplicit
    d.store.ingest(task: pointerTask, projectID: "project-1")
    let pointerDestination = d.item(pointerExplicit)
    let destinationCurrent = pointerDestination["currentEvidence"] as? [String: Any]
    expect("reattribution preserves the destination's exact verification subject",
           destinationCurrent?["subject"] as? String, "destination-commit")
    check("a moved historical landing cannot become current for a mismatched destination proof",
          destinationCurrent?["landingId"] is NSNull)
    check("the mismatched historical landing remains visible without becoming current proof",
          (pointerDestination["landings"] as? [[String: Any]])?.contains {
              $0["subject"] as? String == "fallback-commit"
          } == true)

    var laterFallback = task
    laterFallback["id"] = "broker-task-later"
    laterFallback.removeValue(forKey: "workItemId")
    d.store.ingest(task: laterFallback, projectID: "project-1")
    let canonical = d.item(explicit)["links"] as? [[String: Any]]
    check("explicit identity rebinds the graph's later fallback target",
          canonical?.contains { $0["targetId"] as? String == "broker-task-later" } == true)

    let conflictingExplicit = d.create(type: "feature", title: "Conflicting explicit Feature")
    var conflictTask = task
    conflictTask["id"] = "broker-task-conflict"
    conflictTask["workItemId"] = conflictingExplicit
    let conflict = d.store.ingest(task: conflictTask, projectID: "project-1")
    expect("a second explicit item cannot toggle an established graph canonical binding",
           conflict.reason, "graph_binding_conflict")
    var afterConflictFallback = task
    afterConflictFallback["id"] = "broker-task-after-conflict"
    afterConflictFallback.removeValue(forKey: "workItemId")
    d.store.ingest(task: afterConflictFallback, projectID: "project-1")
    let canonicalAfterConflict = d.item(explicit)["accountingTaskLinks"] as? [[String: Any]]
    check("fallback remains on the first explicit canonical item after a conflict",
          canonicalAfterConflict?.contains {
              $0["targetId"] as? String == "broker-task-after-conflict"
          } == true)
    let afterConflictRevision = d.revision
    _ = d.store.ingest(task: conflictTask, projectID: "project-1")
    _ = d.store.ingest(task: afterConflictFallback, projectID: "project-1")
    // A root coordinates several unrelated objectives. Its identity alone cannot transfer
    // their owner facts: production backfill otherwise rewrites every card on every read.
    let sharedRoot = BoardTestDriver(name: "shared-root-replay-\(UUID().uuidString)")
    sharedRoot.createProject()
    let unrelated: [[String: Any]] = (0..<3).map { index in [
        "id": "unrelated-\(index)", "title": "Independent work \(index)",
        "state": "success", "root": ["sessionId": "one-root"],
        "startedAt": 100.0, "finishedAt": 200.0,
    ] }
    for record in unrelated { sharedRoot.store.ingest(task: record, projectID: "project-1") }
    let sharedRootRevision = sharedRoot.revision
    for record in unrelated { sharedRoot.store.ingest(task: record, projectID: "project-1") }
    let retainedOwners = boardItems(sharedRoot.store).allSatisfy {
        $0["owner"] as? String == "one-root"
    }
    expect("batch replay preserves explicit identity and independent cards sharing one root",
           [d.revision, sharedRoot.revision, retainedOwners ? 1 : 0],
           [afterConflictRevision, sharedRootRevision, 1])

    d.createProject(id: "project-2", name: "Other")
    var otherProjectTask = task
    otherProjectTask["id"] = "other-project-task"
    otherProjectTask.removeValue(forKey: "workItemId")
    d.store.ingest(task: otherProjectTask, projectID: "project-2")
    let otherBoard = d.store.snapshot(project: "project-2")["board"] as? [String: Any]
    let otherItems = (otherBoard?["items"] as? [[String: Any]]) ?? []
    expect("the same graph id has a distinct Project-scoped item", otherItems.count, 1)
    expect("the other Project retains its own task link",
           ((otherItems.first?["links"] as? [[String: Any]])?.first {
               $0["kind"] as? String == "task"
           })?["targetId"] as? String, "other-project-task")

    let missingStart: [String: Any] = [
        "id": "missing-start", "title": "historical", "state": "success",
        "workPhase": "output", "workItemId": explicit,
        "child": ["sessionId": "historical-session"], "finished_at": 500.0,
    ]
    d.store.ingest(task: missingStart, projectID: "project-1")
    let stableRevision = d.revision
    let stableSpan = (d.item(explicit)["spans"] as? [[String: Any]])?.first {
        $0["sourceId"] as? String == "missing-start"
    }
    expect("a missing start uses a canonical non-inverted terminal boundary",
           stableSpan?["startedAt"] as? Double, 500.0)
    d.store.ingest(task: missingStart, projectID: "project-1")
    expect("reobserving a missing-start terminal task is idempotent", d.revision, stableRevision)

    let capacity = BoardTestDriver(name: "ingest-capacity-\(UUID().uuidString)")
    capacity.createProject()
    let capacityItem = capacity.create(type: "feature", title: "Capacity")
    var capacityObject = try! JSONSerialization.jsonObject(with: Data(contentsOf: capacity.file))
        as! [String: Any]
    var capacityItems = capacityObject["items"] as! [[String: Any]]
    capacityItems[0]["links"] = (0..<256).map { number in
        ["id": "link-\(number)", "kind": "task", "targetId": "task-\(number)",
         "label": "task", "source": "broker"] as [String: Any]
    }
    capacityObject["items"] = capacityItems
    try! JSONSerialization.data(withJSONObject: capacityObject, options: [.sortedKeys])
        .write(to: capacity.file, options: .atomic)
    let capacityStore = ProjectBoardStore(url: capacity.file)
    let overflowTask: [String: Any] = [
        "id": "task-overflow", "title": "overflow", "workItemId": capacityItem,
    ]
    let overflow = capacityStore.ingest(task: overflowTask, projectID: "project-1")
    expect("automatic capacity loss with a retained lifecycle change is typed partial",
           overflow.status.rawValue, "partial")
    expect("automatic capacity loss reports one dropped source", overflow.droppedCount, 1)
    expect("automatic capacity loss is durably represented", overflow.persisted, true)
    let overflowItem = (capacityStore.snapshot(item: capacityItem)["board"]
        as? [String: Any])?["item"] as? [String: Any]
    let sourceIngestion = overflowItem?["sourceIngestion"] as? [String: Any]
    expect("capacity loss remains visible with a stable reason",
           (sourceIngestion?["reasons"] as? [String])?.first, "link_capacity_reached")
    let capacityAfter = ((capacityStore.snapshot()["board"] as? [String: Any])?["revision"]
        as? Int) ?? -1
    let repeatedOverflow = capacityStore.ingest(task: overflowTask, projectID: "project-1")
    expect("repeated capacity refusal keeps the same typed outcome",
           repeatedOverflow.status.rawValue, "refused")
    expect("repeated capacity refusal does not churn revision",
           ((capacityStore.snapshot()["board"] as? [String: Any])?["revision"] as? Int),
           capacityAfter)

    let beforeUnknown = boardItems(d.store).count
    d.store.ingest(task: ["id": "unknown-task", "state": "success"], projectID: "project-1")
    expect("ungraphed history becomes a stable execution record",
           boardItems(d.store).count, beforeUnknown + 1)
    let retainedUnknown = boardItems(d.store).first { $0["title"] as? String == "Execution unknown-task" }
    expect("ungraphed history is never guessed to be a Feature",
           retainedUnknown?["type"] as? String, "task")
    _ = d.send("set_enabled", ["enabled": false])
    let beforeDisabled = boardItems(d.store).count
    d.store.ingest(task: ["id": "disabled-task",
                          "graph": ["id": "graph-disabled", "destination": "No card"]],
                   projectID: "project-1")
    expect("standard mode creates no inferred cards", boardItems(d.store).count, beforeDisabled)
}
}
