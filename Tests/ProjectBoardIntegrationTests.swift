import Foundation

private final class BoardCloudStatusBox: @unchecked Sendable {
    private let lock = NSLock()
    private var status: Int?

    func store(_ value: Int) {
        lock.lock(); status = value; lock.unlock()
    }

    func load() -> Int? {
        lock.lock(); defer { lock.unlock() }
        return status
    }
}

func runProjectBoardIntegrationTests() {
group("broker live transitions reach the Board before task completion") {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent("board-live-event-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let registry = root.appendingPathComponent("registry.json")
    let previousRegistry = Orchestrator.storeURLOverrideForTesting
    Orchestrator.forget()
    Orchestrator.storeURLOverrideForTesting = registry
    let store = ProjectBoardStore(url: root.appendingPathComponent("board.json"))
    ProjectBoardIntegration.configureReadStoreForTesting(store) { store, _ in
        let seed = store.readSeed(rebuild: true).seed
        return .success(.init(revision: seed.header.revision, observedAt: seed.header.updatedAt,
                             catalog: seed.envelope(), projects: [:], items: [:]))
    }
    defer {
        ProjectBoardIntegration.drainObservationsForTesting()
        ProjectBoardIntegration.configureReadStoreForTesting(nil)
        Orchestrator.forget()
        Orchestrator.storeURLOverrideForTesting = previousRegistry
        try? FileManager.default.removeItem(at: root)
    }
    let id = UUID().uuidString.lowercased()
    var task = Orchestrator.Task(id: id, state: .queued, kind: "custom", title: "Current work",
        assistant: .codex, projectDir: root.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 100), secretHash: String(repeating: "0", count: 64))
    try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [OrchestratorStore.stored(task)]])
        .write(to: registry)
    Orchestrator.load()
    var seed = OrchestratorStore.stored(task)
    seed.removeValue(forKey: "secret_hash")
    _ = ProjectBoardIntegration.ingest(seed, store: store)
    func attemptState() -> String? {
        let board = store.snapshot()["board"] as? [String: Any]
        let item = (board?["items"] as? [[String: Any]])?.first
        return (item?["links"] as? [[String: Any]])?.first { $0["kind"] as? String == "task" }?["attemptState"] as? String
    }
    expect("initial dispatch is queued", attemptState(), "queued")
    task.state = .spawning
    check("broker accepts the next live stage", Orchestrator.replaceTask(task, expecting: .queued))
    ProjectBoardIntegration.drainObservationsForTesting()
    expect("spawning reaches the durable model without a GET or restart", attemptState(), "spawning")
    task.state = .briefed
    task.childSessionId = "live-child"
    task.briefedAt = Date(timeIntervalSince1970: 110)
    check("broker accepts observed briefing", Orchestrator.replaceTask(task, expecting: .spawning))
    ProjectBoardIntegration.drainObservationsForTesting()
    expect("briefed reaches the same Board task before any terminal receipt", attemptState(), "briefed")
    let revision = store.readHeader().revision
    task.state = .queued
    check("stale transition remains refused", !Orchestrator.replaceTask(task))
    ProjectBoardIntegration.drainObservationsForTesting()
    expect("a refused transition publishes no Board mutation", store.readHeader().revision, revision)
}

group("board accounting allocates only broker task boundaries and preserves unknowns") {
    var signal = ProjectBoardIntegration.CatalogSignal()
    check("OFF inventory does not start catalog discovery",
          !signal.observe(enabled: false, complete: true, paths: ["/new"]))
    check("incomplete inventory does not establish catalog membership",
          !signal.observe(enabled: true, complete: false, paths: ["/new"]))
    check("a complete new inventory invalidates catalog once",
          signal.observe(enabled: true, complete: true, paths: ["/new"]))
    check("unchanged inventory polling cannot rebuild sources",
          !signal.observe(enabled: true, complete: true, paths: ["/new"]))
    check("another ordinary Session Project invalidates the model",
          signal.observe(enabled: true, complete: true, paths: ["/new", "/second"]))
    signal.invalidate()
    check("configuration invalidation refreshes unchanged Project paths",
          signal.observe(enabled: true, complete: true, paths: ["/new", "/second"]))
    var row = UsageLedger.Row()
    row.intervalKey = "interval"; row.taskID = "task-1"; row.sessionID = "session-1"
    row.counts = .init(inputNew: 10, output: 2, cacheRead: 3, cacheWrite: 0)
    row.costValue = 0.25; row.costUnit = "USD"; row.costBasis = "recorded"
    func item(_ links: [[String: Any]]) -> [String: Any] { ["id": "item", "links": links] }
    func usage(_ links: [[String: Any]], _ rows: [UsageLedger.Row]) -> [String: Any] {
        ProjectBoardIntegration.enriched(item(links), rows: rows)["usage"] as? [String: Any] ?? [:]
    }
    let manual: [[String: Any]] = [["kind": "task", "targetId": "task-1"],
        ["kind": "session", "targetId": "session-1"]]
    expect("relations do not allocate usage", usage(manual, [row])["state"] as? String, "absent")
    let linked: [[String: Any]] = [["kind": "task", "targetId": "task-1", "source": "broker", "phase": "output"]]
    let measured = usage(linked, [row, row])
    expect("duplicate intervals counted once", measured["rows"] as? Int, 1)
    expect("known total remains exact", measured["total"] as? Int, 15)
    expect("output is not all token parts", measured["output"] as? Int, 2)
    expect("declared task boundary classified", measured["undeclaredRows"] as? Int, 0)
    let phases = measured["phases"] as? [String: [String: Any]]
    expect("phase retains same measurement", phases?["output"]?["total"] as? Int, 15)
    let cost = measured["costSeries"] as? [[String: Any]]
    expect("cost not duplicated", cost?.first?["value"] as? Double, 0.25)
    row.counts.cacheWrite = nil
    let partial = usage(linked, [row])
    expect("unknown is partial", partial["state"] as? String, "partial")
    check("unknown total is null, not zero", partial["total"] is NSNull)
    expect("known lower bound survives", partial["measured"] as? Int, 15)
    row.counts.cacheWrite = 0; row.coverageReasons = ["source_regressed"]
    expect("source regression is qualified, not complete", usage(linked, [row])["state"] as? String, "qualified")
    expect("qualified observations retain measured counts", usage(linked, [row])["total"] as? Int, 15)
    expect("coverage reason survives projection", usage(linked, [row])["coverageReasons"] as? [String], ["source_regressed"])
    let truncated = ProjectBoardIntegration.usage([], truncated: true)
    expect("truncated absence is not proof of no usage", truncated["state"] as? String, "partial")
    check("truncated scan cannot publish a full total", truncated["total"] is NSNull)
    var other = row; other.intervalKey = "other"; other.taskID = "task-2"
    expect("other task in same session excluded", usage(linked, [row, other])["rows"] as? Int, 1)
    let historical: [[String: Any]] = [["kind": "task", "targetId": "task-1", "source": "broker"]]
    expect("historical undeclared remains explicit", usage(historical, [row])["undeclaredRows"] as? Int, 1)
    check("absent has no pretend zero", ProjectBoardIntegration.usage([])["total"] is NSNull)
    let failedSync = ProjectBoardIntegration.enriched(item(linked), rows: [row], ingestionReasons: ["link_capacity"])["usage"] as? [String: Any]
    expect("incomplete ingestion qualifies measured usage", failedSync?["state"] as? String, "qualified")
    check("ingestion reason reaches usage", (failedSync?["coverageReasons"] as? [String])?.contains("link_capacity") == true)
    expect("failed ingestion cannot prove absence", ProjectBoardIntegration.usage([], coverageReasons: ["persistence_failed"])["state"] as? String, "unavailable")
    var coverage = ProjectBoardIntegration.IngestionCoverage()
    coverage.record(.init(status: .refused, acceptedCount: 0, droppedCount: 1, persisted: true, reason: "board_disabled"), projectID: "p")
    expect("disabled mode is not failed synchronization", coverage.issueCount, 0)
    coverage.record(.init(status: .partial, acceptedCount: 1, droppedCount: 1, persisted: true, reason: "link_capacity"), projectID: "p")
    coverage.record(.init(status: .unavailable, acceptedCount: 0, droppedCount: 1, persisted: false, reason: "persistence_failed"), projectID: "p")
    expect("partial and failed synchronization counted", coverage.issueCount, 2)
    expect("failure reasons remain project scoped", coverage.reasons(projectID: "other"), [])
    expect("coverage does not collapse failure kinds", coverage.reasons(projectID: "p"), ["link_capacity", "persistence_failed"])
    expect("source failure cannot advertise refresh complete", coverage.jsonObject["status"] as? String, "partial")
    var repaired = ProjectBoardIntegration.IngestionCoverage()
    var identityRepair = ProjectBoardIntegration.IngestionCoverage()
    for source in ["resolved-task", "still-unknown"] {
        identityRepair.record(.init(status: .refused, acceptedCount: 0, droppedCount: 1,
            persisted: true, reason: "project_identity_unavailable"), projectID: nil, sourceID: source)
    }
    identityRepair.record(.init(status: .accepted, acceptedCount: 1, droppedCount: 0,
        persisted: true, reason: nil), projectID: "p", sourceID: "resolved-task")
    expect("resolved identity removes only its own wildcard predecessor", identityRepair.issueCount, 1)
    identityRepair.record(.init(status: .accepted, acceptedCount: 1, droppedCount: 0,
        persisted: true, reason: nil), projectID: "q", sourceID: "still-unknown")
    expect("all resolved sources remove global warning", identityRepair.reasons(projectID: "other"), [])
    repaired.record(.init(status: .unavailable, acceptedCount: 0, droppedCount: 1,
                          persisted: false, reason: "persistence_failed"),
                    projectID: "p", sourceID: "task-1")
    repaired.record(.init(status: .accepted, acceptedCount: 1, droppedCount: 0,
                          persisted: true, reason: nil),
                    projectID: "p", sourceID: "task-1")
    expect("the same repaired source clears its transient current issue", repaired.issueCount, 0)
    repaired.record(.init(status: .partial, acceptedCount: 1, droppedCount: 1,
                          persisted: true, reason: "link_capacity"),
                    projectID: "p", sourceID: "task-lost")
    repaired.record(.init(status: .accepted, acceptedCount: 1, droppedCount: 0,
                          persisted: true, reason: nil),
                    projectID: "p", sourceID: "other-task")
    expect("success for another source cannot erase proven loss",
           repaired.reasons(projectID: "p"), ["link_capacity"])
    let compact: [String: Any] = ["id": "item", "links": [],
        "accountingTaskLinks": [["targetId": "task-1", "source": "broker", "phase": "output"]],
        "sourceIngestion": ["status": "partial", "reasons": ["span_capacity"]]]
    let compactUsage = ProjectBoardIntegration.enriched(compact, rows: [row])["usage"] as? [String: Any]
    expect("summary accounting survives omitted display links", compactUsage?["measured"] as? Int, 15)
    check("durable ingestion warning survives source retention", (compactUsage?["coverageReasons"] as? [String])?.contains("span_capacity") == true)
}

group("board list cards are compact summaries rather than hidden detail envelopes") {
    let checklist: [[String: Any]] = [
        ["id": "c1", "title": "Required", "status": "passed", "required": true,
         "evidenceId": "e1"],
        ["id": "c2", "title": "Optional", "status": "doing", "required": false],
    ]
    let milestones: [[String: Any]] = [
        ["id": "m1", "title": "One", "status": "not_applicable"],
        ["id": "m2", "title": "Two", "status": "todo"],
    ]
    var verbose: [String: Any] = [
        "id": "item", "key": "F-1", "projectId": "project", "title": "Compact card",
        "type": "feature", "state": "execution", "summary": "A realistic card summary.",
        "owner": "root", "parentId": "epic", "createdAt": 1.0, "updatedAt": 2.0,
        "scopeRevision": 3,
        "progress": ["state": "execution", "futureProgressKey": "preserved"],
        "presentation": [
            "authority": "narrative_only",
            "variants": [["locale": "zh-Hant", "title": "精簡", "summary": "摘要",
                           "outcome": "更快讀取", "nextStep": "審查", "model": "worker",
                           "authoredAt": 2.0, "status": "current"]],
        ],
        "deliveryLanes": [["id": "child", "state": "execution"]],
        "deliveryLaneCount": 3,
        "checklist": checklist, "milestones": milestones,
        "artifacts": [["id": "a", "title": String(repeating: "artifact", count: 12)]],
        "links": [["id": "l", "kind": "task", "targetId": "task", "source": "broker"]],
        "accountingTaskLinks": [["targetId": "task", "source": "broker"]],
        "obligations": [["id": "o", "title": "hidden", "blocking": true]],
        "history": [["id": "h", "summary": String(repeating: "history", count: 30)]],
        "findings": [["id": "f"]], "verifications": [["id": "v"]],
        "landings": [["id": "l"]], "evidenceSummaries": [["id": "s"]],
        "projection": [
            "checklist": ["retainedCount": 2, "omittedCount": 0],
            "milestones": ["retainedCount": 2, "omittedCount": 0],
        ],
    ]
    var usage = ProjectBoardIntegration.usage([])
    usage["state"] = "present"; usage["rows"] = 1; usage["measured"] = 42
    usage["total"] = 42; usage["output"] = 7
    verbose["usage"] = usage

    let card = ProjectBoardIntegration.compactCard(verbose)
    let detail = ProjectBoardIntegration.enriched(verbose, rows: [])
    check("detail enrichment keeps the arrays excluded only from cards",
          detail["checklist"] != nil && detail["history"] != nil && detail["links"] != nil
            && detail["accountingTaskLinks"] != nil && detail["findings"] != nil)
    let keys = Set(card.keys)
    for leaked in ["checklist", "milestones", "artifacts", "links", "accountingTaskLinks",
                   "obligations", "history", "findings", "verifications", "landings",
                   "evidenceSummaries", "projection"] {
        check("list card excludes \(leaked)", !keys.contains(leaked))
    }
    expect("unknown progress keys survive list projection",
           (card["progress"] as? [String: Any])?["futureProgressKey"] as? String, "preserved")
    check("localized presentation survives list projection", card["presentation"] != nil)
    check("Epic delivery lanes survive list projection", card["deliveryLanes"] != nil)
    expect("Epic lane total survives bounded list projection", card["deliveryLaneCount"] as? Int, 3)
    expect("list keeps nullable parent identity", card["parentId"] as? String, "epic")
    let cardSummary = card["cardSummary"] as? [String: [String: Any]]
    expect("checklist total is compact", cardSummary?["checklist"]?["total"] as? Int, 2)
    expect("required completion is compact", cardSummary?["checklist"]?["requiredCompleted"] as? Int, 1)
    expect("milestone completion is compact", cardSummary?["milestones"]?["completed"] as? Int, 1)
    check("list usage drops per-phase detail",
          (card["usage"] as? [String: Any])?["phases"] == nil)

    let cards = (0..<277).map { index -> [String: Any] in
        var row = verbose
        row["id"] = "item-\(index)"; row["key"] = "F-\(index)"
        row["title"] = "Realistic work item \(index)"
        return ProjectBoardIntegration.compactCard(row)
    }
    let bytes = cards.compactMap { try? JSONSerialization.data(withJSONObject: $0).count }
    check("realistic cards average at most one KiB",
          bytes.count == 277 && bytes.reduce(0, +) / bytes.count <= 1_024)
    let board: [String: Any] = ["items": [] as [[String: Any]], "projects": [] as [[String: Any]]]
    expect("all 277 modest cards fit the scoped snapshot budget",
           ProjectBoardIntegration.boundedSummaries(cards, board: board).count, 277)
}

group("board adapters preserve identities and share the closed Cloud route") {
    let same = ProjectBoardIntegration.projectID("/repo")
    expect("project identity deterministic", ProjectBoardIntegration.projectID("/repo"), same)
    check("different repositories do not share an item namespace", same != ProjectBoardIntegration.projectID("/elsewhere"))
    check("public project identity contains no path", !same.contains("/"))
    let normalized = ProjectBoardIntegration.normalized(["id": "t", "work_item_id": "i", "work_phase": "correction",
        "child_session": "s", "child_terminal": "%1"])
    expect("store metadata normalized", normalized["workItemId"] as? String, "i")
    expect("declared phase normalized", normalized["workPhase"] as? String, "correction")
    expect("session is provider conversation", (normalized["child"] as? [String: String])?["sessionId"], "s")
    let wire = ProjectBoardIntegration.normalized(["child": ["sessionId": "wire", "terminalId": "%2"]])
    expect("wire identity not erased", (wire["child"] as? [String: String])?["sessionId"], "wire")
    expect("stored start uses briefing boundary", ProjectBoardIntegration.normalized(["briefed_at": 120, "spawned_at": 110, "created": 100])["startedAt"] as? Int, 120)
    expect("unbriefed start uses spawn boundary", ProjectBoardIntegration.normalized(["spawned_at": 110, "created": 100])["startedAt"] as? Int, 110)
    expect("historical start retains creation boundary", ProjectBoardIntegration.normalized(["created": 100])["startedAt"] as? Int, 100)
    let read = CloudHeadlessRead.board(session: CloudAppBridge.machineReplySession, request: "r", project: "p", item: "i")
    let route = CloudLocalRoute(read: read)
    expect("cloud read same endpoint", route.path, "/v1/board")
    expect("cloud read exact project", route.query["project"], "p")
    expect("cloud read exact item", route.query["item"], "i")
    expect("reply routed to request", read.name, "read:r")
    let bytes = Data(#"{"operation":"set_enabled","enabled":false}"#.utf8)
    let command = CloudLocalRoute(command: .board(body: bytes))
    expect("cloud command same endpoint", command.path, "/v1/board")
    expect("cloud command remains POST", command.method, "POST")
    expect("command body preserved", command.body, bytes)
    check("materialized board read never enters the analytics lane",
          !RemoteServer.isUsageAnalyticsReading("/v1/board"))
    let taskID = "eeeeeeee-1111-4222-8333-444444444444"
    var draftBody: [String: Any] = ["clawdline_protocol": 1, "task_id": taskID,
        "assistant": "codex", "project_dir": "/tmp/project", "instructions": "bounded work",
        "work_item_id": "board-item", "work_phase": "review_testing"]
    if case .ok(let draft) = OrchestratorDraft.draft(from: draftBody, expecting: taskID, isDirectory: { _ in true }) {
        expect("dispatch accepts explicit work item", draft.workItemID, "board-item")
        expect("dispatch accepts explicit phase", draft.workPhase, "review_testing")
    } else { check("board dispatch draft decodes", false) }
    draftBody["work_phase"] = "done"
    if case .bad = OrchestratorDraft.draft(from: draftBody, expecting: taskID, isDirectory: { _ in true }) {
        check("lifecycle cannot masquerade as token phase", true)
    } else { check("lifecycle cannot masquerade as token phase", false) }
    let stored: [String: Any] = ["id": taskID, "state": "success", "assistant": "codex",
        "project_dir": "/tmp/project", "created": 100.0, "secret_hash": "test-only",
        "work_item_id": "board-item", "work_phase": "correction"]
    if let task = OrchestratorStore.task(from: stored) {
        expect("restart retains item identity", task.workItemID, "board-item")
        expect("restart retains phase", task.workPhase, "correction")
        let encoded = OrchestratorStore.stored(task)
        expect("codec persists item", encoded["work_item_id"] as? String, "board-item")
        expect("codec persists phase", encoded["work_phase"] as? String, "correction")
    } else { check("stored board task decodes", false) }
}


group("board Project presentation joins only the same canonical Start Point") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-board-presentation-\(UUID().uuidString)", isDirectory: true)
    let linked = root.appendingPathComponent("linked-worktree", isDirectory: true)
    let linkedGit = root.appendingPathComponent(".git/worktrees/linked-worktree", isDirectory: true)
    try! FileManager.default.createDirectory(at: root.appendingPathComponent(".git"),
                                              withIntermediateDirectories: true)
    try! FileManager.default.createDirectory(at: linked, withIntermediateDirectories: true)
    try! FileManager.default.createDirectory(at: linkedGit, withIntermediateDirectories: true)
    try! Data("gitdir: \(linkedGit.path)\n".utf8)
        .write(to: linked.appendingPathComponent(".git"))
    try! Data("../..\n".utf8).write(to: linkedGit.appendingPathComponent("commondir"))
    defer { try? FileManager.default.removeItem(at: root) }
    let places = [
        StartPoints.Place(id: "linked", path: linked.path, label: "Wrong linked label", at: Date()),
        StartPoints.Place(id: "root", path: root.path, label: "Wrong root label", at: Date.distantPast),
    ]
    let presentations = ProjectBoardIntegration.projectPresentations(places)
    let order = ProjectBoardIntegration.projectOrder(places)
    let id = ProjectBoardIntegration.projectID(root.path)
    expect("linked Start Points join the exact canonical Project id", presentations.count, 1)
    expect("main checkout and linked path produce one deterministic Project order", order.count, 1)
    expect("newest canonical Start Point wins presentation order", order[id], 0)
    expect("Project display path is canonical rather than the nested Session cwd",
           presentations[id]?.displayPath, root.path)
    expect("Project label is resolved from that same canonical place",
           presentations[id]?.label, StartPoints.label(for: root.path))
    check("canonical Project metadata carries a stable icon payload",
          presentations[id]?.icon?["cells"] is [[Any]])

    let storeRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-board-project-name-\(UUID().uuidString)", isDirectory: true)
    defer { try? FileManager.default.removeItem(at: storeRoot) }
    let store = ProjectBoardStore(url: storeRoot.appendingPathComponent("board.json"))
    let persistentName = ProjectBoardIntegration.persistentProjectName(
        root.path, presentationLabel: "Friendly Start Point label")
    _ = store.ensureProject(id: id, name: persistentName)
    let stableRevision = (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int
    _ = store.ensureProject(id: id, name: persistentName)
    expect("presentation labels never alternate the durable Project name",
           (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int,
           stableRevision)
    expect("the durable Project name is the canonical repository basename",
           ((store.snapshot()["board"] as? [String: Any])?["projects"] as? [[String: Any]])?
                .first?["name"] as? String,
           root.lastPathComponent)
    _ = ProjectBoardIntegration.ingest([
        "id": "project-name-idempotence", "title": "retained task", "state": "success",
        "project_dir": root.path, "finished_at": 100.0,
    ], store: store)
    let afterIngestRevision = (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int
    _ = store.ensureProject(id: id, name: persistentName)
    expect("the Start Point refresh cannot rename and churn an ingest-owned Project",
           (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int,
           afterIngestRevision)
    let seed = store.readSeed(rebuild: true).seed
    let catalog = seed.envelope()["board"] as? [String: Any]
    expect("materialized global read contains no item reconstruction",
           (catalog?["items"] as? [[String: Any]])?.count, 0)
    let catalogRow = (catalog?["projects"] as? [[String: Any]])?.first
    expect("Project counters come from the complete materialized source",
           catalogRow?["itemCount"] as? Int, 1)
    expect("Project counter completeness is explicit",
           (catalogRow?["summaryCoverage"] as? [String: Any])?["status"] as? String,
           "complete")
    let projectItems = (seed.envelope(project: id)["board"] as? [String: Any])?["items"]
        as? [[String: Any]]
    expect("Project scope contains its item summaries", projectItems?.count, 1)
    let selectedID = projectItems?.first?["id"] as? String
    let detail = seed.envelope(project: id, item: selectedID)["board"] as? [String: Any]
    expect("single-item scope omits sibling summaries",
           (detail?["items"] as? [[String: Any]])?.count, 0)
    expect("single-item scope carries its detail",
           (detail?["item"] as? [String: Any])?["id"] as? String, selectedID)
    let mismatched = seed.envelope(project: "other", item: selectedID)["board"]
        as? [String: Any]
    check("single-item scope validates Project membership", mismatched?["item"] is NSNull)
}

group("board HTTP authority cannot be supplied by command content") {
    let oldWrite = Config.shared.remoteWrite
    Config.shared.remoteWrite = true
    defer { Config.shared.remoteWrite = oldWrite }
    func call(_ body: String, machine: Bool = false, caps: Set<RemoteAuth.Capability> = [.read, .send]) -> RemoteServer.Response? {
        let request = remoteRequest("POST", "/v1/board", body: body)
        return ProjectBoardHTTP.route(request, machine: machine, permission: .allowed(device: "test-viewer", caps: caps))
    }
    expect("reader cannot send", call("{}", caps: [.read])?.status, 403)
    expect("send does not change global mode", call(#"{"operation":"set_enabled"}"#)?.status, 403)
    expect("send cannot consent to external Board AI", call(#"{"operation":"set_ai_consent"}"#)?.status, 403)
    expect("send does not accept artifact", call(#"{"operation":"accept_artifact"}"#)?.status, 403)
    expect("send cannot mint verification", call(#"{"operation":"record_evidence","kind":"verification","trusted":true}"#)?.status, 403)
    expect("send cannot author a closure report", call(#"{"operation":"record_report"}"#)?.status, 403)
    expect("even root cannot forge broker landing", call(#"{"operation":"record_evidence","kind":"landing"}"#, machine: true)?.status, 403)
    expect("root verification reaches schema validator", call(#"{"operation":"record_evidence","kind":"verification"}"#, machine: true)?.status, 400)
    expect("root report reaches schema validator", call(#"{"operation":"record_report"}"#, machine: true)?.status, 400)
    expect("admin acceptance reaches schema validator", call(#"{"operation":"accept_artifact"}"#, caps: [.read, .send, .admin])?.status, 400)
    Config.shared.remoteWrite = false
    expect("remote write switch preserved", call("{}")?.status, 403)
    expect("machine auth preserves baseline local control", call("{}", machine: true)?.status, 400)
    let repeated = remoteRequest("GET", "/v1/board?item=a&item=b")
    expect("duplicate query is refused", ProjectBoardHTTP.route(repeated, machine: true, permission: .denied)?.status, 400)
    let reportWithoutItem = remoteRequest("GET", "/v1/board?report=report-a")
    expect("report reads require an independently selected item",
           ProjectBoardHTTP.route(reportWithoutItem, machine: true, permission: .denied)?.status,
           400)
    let selectorRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-board-http-report-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: selectorRoot) }
    let selectorStore = ProjectBoardStore(url: selectorRoot.appendingPathComponent("board.json"))
    ProjectBoardHTTP.configureStoreForTesting(selectorStore)
    defer { ProjectBoardHTTP.configureStoreForTesting(nil) }
    let unknownReport = remoteRequest(
        "GET", "/v1/board?project=project-a&item=item-a&report=report-a")
    let unknownReportReply = ProjectBoardHTTP.route(
        unknownReport, machine: true, permission: .denied)
    expect("direct report selector enters the authenticated bounded read lane",
           unknownReportReply?.status, 404)
    let unknownReportObject = (try? JSONSerialization.jsonObject(
        with: unknownReportReply?.body ?? Data())) as? [String: Any]
    expect("unknown report item refusal remains typed",
           (unknownReportObject?["error"] as? [String: Any])?["code"] as? String,
           "report_item_not_found")
    let cloudEncodedReport = remoteRequest(
        "GET", "/v1/board?project=project-a&item=report:item-a:report-a")
    expect("closed Cloud selector spelling reaches the same report reader",
           ProjectBoardHTTP.route(cloudEncodedReport, machine: true, permission: .denied)?.status,
           404)
    let disabled = ProjectBoardHTTP.viewer(machine: false, permission: .allowed(device: "writer", caps: [.read, .send, .admin]), source: .http, remoteWrite: false)
    expect("viewer reflects remote write switch", disabled["canWrite"] as? Bool, false)
    expect("viewer cannot administer through disabled transport", disabled["canManage"] as? Bool, false)
    let machine = ProjectBoardHTTP.viewer(machine: true, permission: .denied, source: .http, remoteWrite: false)
    expect("machine viewer retains local authority", machine["canManage"] as? Bool, true)
    let reader = ProjectBoardHTTP.viewer(machine: false, permission: .allowed(device: "reader", caps: [.read]), source: .http, remoteWrite: true)
    expect("reader viewer is read only", reader["canWrite"] as? Bool, false)
    let oversized = ProjectBoardHTTP.response(["padding": String(repeating: "x", count: ProjectBoardHTTP.maximumResponseBytes)])
    expect("final wire size bounded after enrichment", oversized.status, 503)
    check("oversize refusal is typed", String(data: oversized.body, encoding: .utf8)?.contains("board_response_too_large") == true)
    expect("bounded response keeps status", ProjectBoardHTTP.response(["ok": true], status: 201).status, 201)
    let unavailable = ProjectBoardHTTP.response(["board": ["available": false,
        "error": ["code": "board_snapshot_too_large", "message": "selected view exceeds budget"]]], commandApplied: true)
    expect("unavailable command projection is not mode-off success", unavailable.status, 503)
    check("projection failure distinguishes a saved command", String(data: unavailable.body, encoding: .utf8)?.contains("command was saved") == true)
}

group("Board commands and reads use independent bounded request lanes") {
    let coordinator = ProjectBoardRequestCoordinator()
    var heldCommands: [() -> Void] = []
    var joinedStatuses: [Int] = []
    for index in 0..<ProjectBoardRequestCoordinator.commandDepth {
        coordinator.startCommand(
            identity: .init(actor: "actor", requestID: "request-\(index)"),
            fingerprint: "same-\(index)", executor: { heldCommands.append($0) },
            work: { .json(["index": index]) }, completeOnOwner: { $0() },
            deliver: { joinedStatuses.append($0.status) })
    }
    var busy: RemoteServer.Response?
    coordinator.startCommand(
        identity: .init(actor: "actor", requestID: "overflow"), fingerprint: "overflow",
        executor: { heldCommands.append($0) }, work: { .json(["wrong": true]) },
        completeOnOwner: { $0() }, deliver: { busy = $0 })
    expect("the bounded writer returns typed saturation", busy?.status, 429)
    check("saturation names the Board command lane",
          String(data: busy?.body ?? Data(), encoding: .utf8)?.contains("board_command_busy") == true)
    coordinator.startCommand(
        identity: .init(actor: "actor", requestID: "request-0"), fingerprint: "same-0",
        executor: { heldCommands.append($0) }, work: { .json(["wrong": true]) },
        completeOnOwner: { $0() }, deliver: { joinedStatuses.append($0.status) })
    var conflict: RemoteServer.Response?
    coordinator.startCommand(
        identity: .init(actor: "actor", requestID: "request-0"), fingerprint: "different",
        executor: { heldCommands.append($0) }, work: { .json(["wrong": true]) },
        completeOnOwner: { $0() }, deliver: { conflict = $0 })
    expect("same request joins without consuming another slot", coordinator.state.commands,
           ProjectBoardRequestCoordinator.commandDepth)
    expect("same in-flight request with different content conflicts", conflict?.status, 409)
    var readStatus: Int?
    coordinator.startRead(executor: { $0() }, work: { .json(["read": true]) },
                          completeOnOwner: { $0() }, deliver: { readStatus = $0.status })
    expect("Board read lane runs while every command slot is held", readStatus, 200)
    while !heldCommands.isEmpty { heldCommands.removeFirst()() }
    expect("joined identical request receives the durable result", joinedStatuses.filter { $0 == 200 }.count,
           ProjectBoardRequestCoordinator.commandDepth + 1)

    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("board-held-persist-\(UUID().uuidString)")
    let store = ProjectBoardStore(url: root.appendingPathComponent("board.json"))
    _ = store.ensureProject(id: "p", name: "Project")
    ProjectBoardHTTP.configureStoreForTesting(store)
    ProjectBoardIntegration.configureReadStoreForTesting(store) { store, _ in
        let seed = store.readSeed(rebuild: true).seed
        let envelope = seed.envelope()
        return .success(.init(revision: seed.header.revision,
                              observedAt: Date().timeIntervalSince1970,
                              catalog: envelope, projects: [:], items: [:]))
    }
    defer {
        ProjectBoardStore.persistencePauseForTesting = nil
        ProjectBoardHTTP.configureStoreForTesting(nil)
        ProjectBoardIntegration.configureReadStoreForTesting(nil)
        try? FileManager.default.removeItem(at: root)
    }
    let persistEntered = DispatchSemaphore(value: 0)
    let persistRelease = DispatchSemaphore(value: 0)
    let pauseLock = NSLock()
    var didPause = false
    ProjectBoardStore.persistencePauseForTesting = {
        pauseLock.lock()
        let shouldPause = !didPause
        didPause = true
        pauseLock.unlock()
        guard shouldPause else { return }
        persistEntered.signal()
        _ = persistRelease.wait(timeout: .now() + 4)
    }
    let revision = store.readHeader().revision
    func commandBody(_ requestID: String, title: String) -> Data {
        try! JSONSerialization.data(withJSONObject: [
            "operation": "create", "requestId": requestID,
            "expectedRevision": revision, "projectId": "p", "title": title,
            "type": "feature", "owner": "root",
        ], options: [.sortedKeys])
    }
    let token = Orchestrator.dispatchToken()
    let local = remoteRequest("POST", "/v1/board",
                              headers: ["X-Clawdline-Orchestrator": token],
                              body: String(data: commandBody("held", title: "Local"), encoding: .utf8)!)
    let localDone = DispatchSemaphore(value: 0)
    let duplicateDone = DispatchSemaphore(value: 0)
    let cloudDone = DispatchSemaphore(value: 0)
    let answerLock = NSLock()
    var localStatuses: [Int] = []
    let cloudStatus = BoardCloudStatusBox()
    RemoteServer.shared.routeOnServerQueueForTesting(local) { response in
        answerLock.lock(); localStatuses.append(response.status); answerLock.unlock()
        localDone.signal()
    }
    check("production Board command reaches held persistence",
          persistEntered.wait(timeout: .now() + 1) == .success)
    RemoteServer.shared.routeOnServerQueueForTesting(local) { response in
        answerLock.lock(); localStatuses.append(response.status); answerLock.unlock()
        duplicateDone.signal()
    }
    let cloudBytes = commandBody("cloud-held", title: "Cloud")
    Task {
        let response = await RemoteServer.shared.routeVerifiedCloudCommand(
            .board(body: cloudBytes), sender: "device", idempotencyKey: "cloud-sequence")
        cloudStatus.store(response.status)
        cloudDone.signal()
    }
    let healthDone = DispatchSemaphore(value: 0), sessionsDone = DispatchSemaphore(value: 0)
    let heartbeatDone = DispatchSemaphore(value: 0), messageAdmissionDone = DispatchSemaphore(value: 0)
    RemoteServer.shared.routeOnServerQueueForTesting(remoteRequest("GET", "/v1/health")) { response in
        if response.status == 200 { healthDone.signal() }
    }
    RemoteServer.shared.routeOnServerQueueForTesting(remoteRequest(
        "GET", "/v1/sessions", headers: ["X-Clawdline-Orchestrator": token])) { _ in
        sessionsDone.signal()
    }
    RemoteServer.shared.heartbeatTurnForTesting { heartbeatDone.signal() }
    let absentSend = remoteRequest(
        "POST", "/v1/sessions/board-isolation-missing/send",
        headers: ["X-Clawdline-Orchestrator": token,
                  "Idempotency-Key": "board-isolation-probe"], body: #"{"text":"probe"}"#)
    RemoteServer.shared.sendTerminalForTesting(absentSend) { _ in messageAdmissionDone.signal() }
    check("held Board fsync does not block health on the shared owner",
          healthDone.wait(timeout: .now() + 0.25) == .success)
    check("held Board fsync does not block the Sessions inventory route turn",
          sessionsDone.wait(timeout: .now() + 0.25) == .success)
    check("held Board fsync does not block an SSE heartbeat owner turn",
          heartbeatDone.wait(timeout: .now() + 0.25) == .success)
    check("held Board fsync does not block message admission for an absent Session",
          messageAdmissionDone.wait(timeout: .now() + 0.25) == .success)
    persistRelease.signal()
    check("local durable command completes", localDone.wait(timeout: .now() + 2) == .success)
    check("identical in-flight local request joins", duplicateDone.wait(timeout: .now() + 2) == .success)
    check("verified Cloud command completes on the same lane", cloudDone.wait(timeout: .now() + 2) == .success)
    answerLock.lock()
    let savedLocal = localStatuses
    answerLock.unlock()
    let savedCloud = cloudStatus.load()
    expect("both joined local calls receive one durable success", savedLocal, [200, 200])
    expect("Cloud preserves CAS after waiting behind local durability", savedCloud, 409)
}

group("a committed usage checkpoint cheaply invalidates the bounded Board model") {
    let ledgerRoot = freshUsageLedger()
    let boardStore = ProjectBoardStore(url: ledgerRoot.appendingPathComponent("board.json"))
    _ = boardStore.ensureProject(id: "p", name: "Project")
    let invalidated = DispatchSemaphore(value: 0), refreshed = DispatchSemaphore(value: 0)
    let unexpected = DispatchSemaphore(value: 0), reasonLock = NSLock()
    var observedReasons: ProjectBoardReadCache.DirtyReasons = []
    ProjectBoardIntegration.configureReadStoreForTesting(boardStore) { store, reasons in
        reasonLock.lock(); observedReasons.formUnion(reasons); reasonLock.unlock()
        let seed = store.readSeed(rebuild: true).seed, envelope = seed.envelope()
        refreshed.signal()
        return .success(.init(revision: seed.header.revision,
                              observedAt: Date().timeIntervalSince1970,
                              catalog: envelope, projects: [:], items: [:]))
    }
    var expectFailure = false
    UsageLedger.committedObserverForTesting = {
        if expectFailure { unexpected.signal(); return }
        ProjectBoardIntegration.usageDidCommit(); invalidated.signal()
    }
    defer {
        UsageLedger.committedObserverForTesting = nil
        ProjectBoardIntegration.configureReadStoreForTesting(nil)
        forgetUsageLedger(ledgerRoot)
    }
    let key = UsageLedger.shared.observeNow(ledgerSample(
        .codex, session: "final-checkpoint", boundary: .task, id: "task-final",
        usage: ["input": 7, "output": 3, "cache_read": 0, "cache_write": 0]))
    check("successful checkpoint commits an interval", key != nil)
    check("commit invalidation leaves the ledger owner asynchronously",
          invalidated.wait(timeout: .now() + 1) == .success)
    check("final checkpoint converges without a later broker event",
          refreshed.wait(timeout: .now() + 2) == .success)
    reasonLock.lock(); let reasons = observedReasons; reasonLock.unlock()
    check("the final checkpoint contributes the typed usage reason", reasons.contains(.usage))
    UsageLedger.shared.closeForTesting()
    let blocked = ledgerRoot.appendingPathComponent("not-a-directory")
    try! Data("blocked".utf8).write(to: blocked)
    UsageLedger.storeURLOverrideForTesting = blocked.appendingPathComponent("usage.sqlite3")
    expectFailure = true
    let failed = UsageLedger.shared.observeNow(ledgerSample(
        .codex, session: "failed-checkpoint", boundary: .task, id: "task-failed"))
    check("failed checkpoint reports no committed interval", failed == nil)
    check("failed checkpoint emits no Board invalidation",
          unexpected.wait(timeout: .now() + 0.15) == .timedOut)
}

group("board materialization is single-flight and mode storms stay bounded") {
    var now = Date(timeIntervalSince1970: 100)
    var jobs: [() -> Void] = []
    let cache = ProjectBoardReadCache(freshFor: 10, clock: { now }, execute: { jobs.append($0) })
    let on = ProjectBoardStore.ReadHeader(revision: 7, enabled: true,
                                          updatedAt: 90, available: true)
    let off = ProjectBoardStore.ReadHeader(revision: 8, enabled: false,
                                           updatedAt: 101, available: true)
    let loading: [String: Any] = ["board": [
        "revision": 7, "projects": [["id": "p", "name": "Persisted"]],
        "items": [] as [[String: Any]], "item": NSNull(),
    ] as [String: Any]]
    let catalog: [String: Any] = ["board": [
        "revision": 7, "projects": [["id": "p", "isStartPoint": true]],
        "items": [] as [[String: Any]], "item": NSNull(),
    ] as [String: Any]]
    let detail: [String: Any] = ["board": [
        "revision": 7, "projects": [] as [[String: Any]],
        "items": [] as [[String: Any]], "item": ["id": "i", "projectId": "p"],
    ] as [String: Any]]
    func model(_ observedAt: Double = 100) -> ProjectBoardReadCache.Model {
        .init(revision: 7, observedAt: observedAt, catalog: catalog,
              projects: [:], items: ["i": detail])
    }
    func status(_ envelope: [String: Any]) -> String? {
        ((envelope["board"] as? [String: Any])?["readState"] as? [String: Any])?["status"] as? String
    }
    let first = cache.read(project: nil, item: nil, header: on, loading: loading) { _ in
        .success(model())
    }
    _ = cache.read(project: nil, item: nil, header: on, loading: loading) { _ in .success(model()) }
    expect("cold reads expose loading", status(first), "loading")
    expect("cold reads distinguish active refresh from retained failure",
           ((first["board"] as? [String: Any])?["readState"] as? [String: Any])?["refreshing"] as? Bool, true)
    expect("duplicate reads start one materialization", jobs.count, 1)
    expect("one refresh admission is recorded", cache.stateForTesting.refreshStarts, 1)
    jobs.removeFirst()()
    expect("the materialized catalog becomes ready",
           status(cache.read(project: nil, item: nil, header: on, loading: loading) { _ in
               .success(model())
           }), "ready")
    var startupEnvelopeCalls = 0
    func countedLoading() -> [String: Any] {
        startupEnvelopeCalls += 1
        return loading
    }
    _ = cache.read(project: nil, item: nil, header: on, loading: countedLoading()) { _ in
        .success(model())
    }
    expect("a current cache does not assemble its startup envelope", startupEnvelopeCalls, 0)
    let wrongScope = cache.read(project: "other", item: "i", header: on, loading: loading) { _ in
        .success(model())
    }
    check("item lookup validates Project and item together",
          ((wrongScope["board"] as? [String: Any])?["item"] is NSNull))

    var detailResolutions = 0
    let lazyDetails = ProjectBoardReadCache.Model(
        revision: 7, observedAt: 100, catalog: catalog, projects: [:], items: [:],
        resolveItem: { item, project in
            detailResolutions += 1
            return item == "i" && (project == nil || project == "p") ? detail : nil
        })
    let detailCache = ProjectBoardReadCache(freshFor: 10, clock: { now }, execute: { _ in })
    detailCache.seed(lazyDetails)
    _ = detailCache.read(project: nil, item: nil, header: on, loading: loading) { _ in
        .success(lazyDetails)
    }
    expect("catalog reads do not materialize item detail", detailResolutions, 0)
    let selected = detailCache.read(project: "p", item: "i", header: on, loading: loading) { _ in
        .success(lazyDetails)
    }
    expect("one selected item materializes exactly one detail", detailResolutions, 1)
    expect("lazy detail preserves selected item semantics",
           (((selected["board"] as? [String: Any])?["item"] as? [String: Any])?["id"] as? String),
           "i")

    now = Date(timeIntervalSince1970: 120)
    expect("elapsed TTL alone does not stale or rebuild an unchanged revision",
           status(cache.read(project: nil, item: nil, header: on, loading: loading) { _ in
               .failure(.init(code: "held_source", message: "fixture"))
           }), "ready")
    expect("unchanged hot reads schedule no background rebuild", jobs.count, 0)

    var recoveryJobs: [() -> Void] = []
    let recovery = ProjectBoardReadCache(freshFor: 10, clock: { now },
                                         execute: { recoveryJobs.append($0) })
    recovery.seed(model())
    recovery.refresh(header: on, reasons: [.catalog]) { _ in
        .failure(.init(code: "held_source", message: "fixture"))
    }
    recoveryJobs.removeFirst()()
    let failed = recovery.read(project: nil, item: nil, header: on, loading: loading) { _ in
        .success(model(120))
    }
    expect("failed refresh keeps old data stale", status(failed), "stale")
    expect("a failed source is not falsely still updating",
           ((failed["board"] as? [String: Any])?["readState"] as? [String: Any])?["refreshing"] as? Bool, false)
    expect("failed refresh names its source",
           ((((failed["board"] as? [String: Any])?["readState"] as? [String: Any])?["error"]
                as? [String: Any])?["code"] as? String), "held_source")
    expect("failure retry is bounded before its interval", recoveryJobs.count, 0)
    now = Date(timeIntervalSince1970: 130)
    _ = recovery.read(project: nil, item: nil, header: on, loading: loading) { _ in
        .success(model(130))
    }
    expect("failure recovery retries after its bounded interval", recoveryJobs.count, 1)
    recoveryJobs.removeFirst()()
    expect("successful retry restores ready data",
           status(recovery.read(project: nil, item: nil, header: on, loading: loading) { _ in
               .success(model(130))
           }), "ready")

    now = Date(timeIntervalSince1970: 140)
    cache.refresh(header: on, reasons: [.catalog]) { _ in .success(model(140)) }
    expect("an explicit source invalidation starts one held generation", jobs.count, 1)
    for _ in 0..<20 {
        cache.setEnabled(false)
        cache.setEnabled(true)
        cache.refresh(header: on, reasons: [.catalog]) { _ in .success(model(140)) }
    }
    expect("mode-toggle storm retains no more than the running worker", jobs.count, 1)
    cache.setEnabled(false)
    jobs.removeFirst()()
    let disabled = cache.read(project: nil, item: nil, header: off, loading: loading) { _ in
        .success(model(140))
    }
    expect("OFF is immediate", (disabled["board"] as? [String: Any])?["mode"] as? String,
           "standard")
    expect("OFF starts no inferred refresh", jobs.count, 0)

    var retainedJobs: [() -> Void] = []
    let retained = ProjectBoardReadCache(freshFor: 10, clock: { now },
                                         execute: { retainedJobs.append($0) })
    retained.seed(model(140))
    retained.setEnabled(false)
    let retainedReply = retained.read(project: nil, item: nil, header: .init(
        revision: 7, enabled: false, updatedAt: 140, available: true), loading: loading) { _ in
            .failure(.init(code: "must_not_run", message: "OFF cannot infer"))
        }
    expect("fresh-process OFF seed is settled retained history", status(retainedReply), "ready")
    expect("settled OFF history starts no automatic mutation", retainedJobs.count, 0)

    let stale = ProjectBoardReadCache(freshFor: 100, clock: { Date(timeIntervalSince1970: 140) },
                                      execute: { retainedJobs.append($0) })
    stale.seed(model(140))
    let newerHeader = ProjectBoardStore.ReadHeader(revision: 8, enabled: true,
                                                   updatedAt: 141, available: true)
    let staleReply = stale.read(project: nil, item: nil, header: newerHeader, loading: loading) { _ in
        .success(model(141))
    }
    let staleBoard = staleReply["board"] as? [String: Any]
    let staleState = staleBoard?["readState"] as? [String: Any]
    expect("CAS header exposes the latest durable revision", staleBoard?["revision"] as? Int, 8)
    expect("stale coverage names the body model revision", staleState?["revision"] as? Int, 7)
    expect("revision mismatch is explicitly stale", staleState?["status"] as? String, "stale")
    expect("a newly observed durable revision schedules refresh", retainedJobs.count, 1)

    let scoped = ProjectBoardReadCache(freshFor: 100, clock: { Date(timeIntervalSince1970: 140) },
                                       execute: { retainedJobs.append($0) })
    scoped.seed(.init(revision: 7, observedAt: 140, catalog: catalog,
                      projects: ["p": catalog], items: [:]))
    let unknown = scoped.read(project: "missing", item: nil, header: on, loading: loading) { _ in
        .success(model(140))
    }
    let unknownState = ((unknown["board"] as? [String: Any])?["readState"] as? [String: Any])
    expect("unknown Project is a typed read error", unknownState?["status"] as? String, "error")
    expect("unknown Project never masquerades as authoritative empty",
           (unknownState?["error"] as? [String: Any])?["code"] as? String,
           "project_not_found")

    var unionJobs: [() -> Void] = []
    var consumed: [ProjectBoardReadCache.DirtyReasons] = []
    let union = ProjectBoardReadCache(freshFor: 10, clock: { now },
                                      execute: { unionJobs.append($0) })
    let unionWork: ProjectBoardReadCache.Refresh = { reasons in
        consumed.append(reasons)
        return .success(model(140))
    }
    union.refresh(header: on, reasons: [.catalog], work: unionWork)
    union.refresh(header: on, reasons: [.usage], work: unionWork)
    union.refresh(header: on, reasons: [.durableModel], work: unionWork)
    expect("held refresh keeps one running worker", unionJobs.count, 1)
    check("pending work is the union of usage and durable-model reasons",
          union.stateForTesting.pendingReasons.contains([.usage, .durableModel]))
    unionJobs.removeFirst()()
    expect("one consolidated successor consumes the pending union", unionJobs.count, 1)
    unionJobs.removeFirst()()
    check("the final worker consumed usage", consumed.last?.contains(.usage) == true)
    check("the final worker consumed the durable model", consumed.last?.contains(.durableModel) == true)
}

group("a held Store materialization cannot block Board HTTP or the interactive owner") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("board-held-materialization-\(UUID().uuidString)")
    defer {
        ProjectBoardStore.materializationPauseForTesting = nil
        ProjectBoardIntegration.configureReadStoreForTesting(nil)
        try? FileManager.default.removeItem(at: root)
    }
    let store = ProjectBoardStore(url: root.appendingPathComponent("board.json"))
    _ = store.ensureProject(id: "p", name: "Project")
    ProjectBoardIntegration.configureReadStoreForTesting(store) { store, _ in
        let seed = store.readSeed(rebuild: true).seed
        let envelope = seed.envelope()
        return .success(.init(revision: seed.header.revision,
                              observedAt: Date().timeIntervalSince1970,
                              catalog: envelope, projects: [:], items: [:]))
    }
    let buildEntered = DispatchSemaphore(value: 0)
    let buildRelease = DispatchSemaphore(value: 0)
    let buildFinished = DispatchSemaphore(value: 0)
    let pauseLock = NSLock()
    var didPause = false
    ProjectBoardStore.materializationPauseForTesting = {
        pauseLock.lock()
        let shouldPause = !didPause
        if shouldPause { didPause = true }
        pauseLock.unlock()
        if shouldPause {
            buildEntered.signal()
            _ = buildRelease.wait(timeout: .now() + 4)
        }
    }
    DispatchQueue.global(qos: .utility).async {
        _ = store.readSeed(rebuild: true)
        buildFinished.signal()
    }
    check("the real Store model build holds its writer seam",
          buildEntered.wait(timeout: .now() + 1) == .success)

    let boardAnswered = DispatchSemaphore(value: 0)
    let interactiveTurn = DispatchSemaphore(value: 0)
    let responseLock = NSLock()
    var boardResponse: RemoteServer.Response?
    let request = remoteRequest("GET", "/v1/board",
        headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()])
    RemoteServer.shared.routeOnServerQueueForTesting(request) { response in
        responseLock.lock(); boardResponse = response; responseLock.unlock()
        boardAnswered.signal()
    }
    RemoteServer.shared.routeOnServerQueueForTesting(remoteRequest("GET", "/v1/health")) { response in
        if response.status == 200 { interactiveTurn.signal() }
    }
    check("Board HTTP returns while the Store writer is held",
          boardAnswered.wait(timeout: .now() + 0.25) == .success)
    check("the same interactive owner executes the following route turn",
          interactiveTurn.wait(timeout: .now() + 0.25) == .success)
    responseLock.lock(); let response = boardResponse; responseLock.unlock()
    let object = response.flatMap { try? JSONSerialization.jsonObject(with: $0.body) as? [String: Any] }
    let readState = ((object?["board"] as? [String: Any])?["readState"] as? [String: Any])
    expect("the nonblocking cold answer is truthfully loading", readState?["status"] as? String,
           "loading")

    buildRelease.signal()
    check("the held Store materialization completes after release",
          buildFinished.wait(timeout: .now() + 2) == .success)
    let deadline = Date().addingTimeInterval(3)
    while ProjectBoardIntegration.readCacheStateForTesting.inFlight && Date() < deadline {
        Thread.sleep(forTimeInterval: 0.01)
    }
    check("the background projection settles before fixture teardown",
          !ProjectBoardIntegration.readCacheStateForTesting.inFlight)
}
}
