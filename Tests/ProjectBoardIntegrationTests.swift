import Foundation

func runProjectBoardIntegrationTests() {
group("board accounting allocates only broker task boundaries and preserves unknowns") {
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
    let compact: [String: Any] = ["id": "item", "links": [],
        "accountingTaskLinks": [["targetId": "task-1", "source": "broker", "phase": "output"]],
        "sourceIngestion": ["status": "partial", "reasons": ["span_capacity"]]]
    let compactUsage = ProjectBoardIntegration.enriched(compact, rows: [row])["usage"] as? [String: Any]
    expect("summary accounting survives omitted display links", compactUsage?["measured"] as? Int, 15)
    check("durable ingestion warning survives source retention", (compactUsage?["coverageReasons"] as? [String])?.contains("span_capacity") == true)
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
    check("board expensive read takes bounded worker", RemoteServer.isUsageAnalyticsReading("/v1/board"))
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
    expect("send does not accept artifact", call(#"{"operation":"accept_artifact"}"#)?.status, 403)
    expect("send cannot mint verification", call(#"{"operation":"record_evidence","kind":"verification","trusted":true}"#)?.status, 403)
    expect("even root cannot forge broker landing", call(#"{"operation":"record_evidence","kind":"landing"}"#, machine: true)?.status, 403)
    expect("root verification reaches schema validator", call(#"{"operation":"record_evidence","kind":"verification"}"#, machine: true)?.status, 400)
    expect("admin acceptance reaches schema validator", call(#"{"operation":"accept_artifact"}"#, caps: [.read, .send, .admin])?.status, 400)
    Config.shared.remoteWrite = false
    expect("remote write switch preserved", call("{}")?.status, 403)
    expect("machine auth preserves baseline local control", call("{}", machine: true)?.status, 400)
    let repeated = remoteRequest("GET", "/v1/board?item=a&item=b")
    expect("duplicate query is refused", ProjectBoardHTTP.route(repeated, machine: true, permission: .denied)?.status, 400)
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
}
