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

func boardProgramPlanProof() {
    let d = BoardTestDriver(name: "program-plan-\(UUID().uuidString)")
    d.createProject()
    let program = d.create(type: "epic", title: "Ubuntu runtime program", owner: "program-root")
    let programKey = d.item(program)["key"] as? String ?? ""
    let initialRevision = d.revision
    let initialScope = d.item(program)["scopeRevision"] as? Int
    let initialEvidence = d.item(program)["currentEvidence"] as? NSDictionary
    let documentURL = "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=program-v1.md"
    let firstFields: [String: Any] = [
        "schemaVersion": 1, "itemId": program, "programKey": programKey,
        "planId": "ubuntu-runtime", "planVersion": 1,
        "graphId": "ubuntu-runtime-graph", "destination": "Deliver the Ubuntu runtime",
        "document": ["documentId": "ubuntu-runtime-plan", "version": 1,
                     "title": "Ubuntu runtime plan", "url": documentURL],
        "nodes": [
            ["key": "w0", "graphNodeId": "w0-contract", "title": "Contract baseline",
             "type": "task", "summary": "Freeze the portable contract.", "owner": "architecture",
             "dependsOn": [], "gateKeys": ["architecture"],
             "capabilityKeys": ["linux-core"], "claims": ["Sources/Core.swift"]],
            ["key": "w1", "graphNodeId": "w1-state", "title": "State ownership",
             "type": "refactor", "summary": "Move one state owner.", "owner": "state-owner",
             "dependsOn": ["w0"], "gateKeys": [],
             "capabilityKeys": ["linux-core"], "claims": ["Sources/Orchestrator.swift"]],
        ],
        "gates": [["key": "architecture", "title": "Architecture approval",
                    "authority": "architect"]],
        "capabilities": [["key": "linux-core", "status": "supported", "observedAt": 100.0]],
    ]
    let imported = d.send("plan_structure", firstFields, requestId: "plan-v1")
    expect("a closed Program Plan imports successfully", imported.status, 200)
    expect("the whole Program draft consumes one Board revision", d.revision, initialRevision + 1)
    let first = d.item(program)
    let plan = first["programPlan"] as? [String: Any]
    expect("the canonical Program and plan identity are explicit", plan?["programKey"] as? String,
           programKey)
    expect("the plan persists its exact graph identity", plan?["graphId"] as? String,
           "ubuntu-runtime-graph")
    expect("the planning document is added in the same mutation",
           (first["documentReferences"] as? [[String: Any]])?.first?["documentId"] as? String,
           "ubuntu-runtime-plan")
    expect("plan import does not change lifecycle", first["state"] as? String, "backlog")
    expect("plan import does not change scope revision", first["scopeRevision"] as? Int, initialScope)
    check("plan import does not rewrite verification, landing, or acceptance pointers",
          (first["currentEvidence"] as? NSDictionary) == initialEvidence)
    let firstNodes = plan?["nodes"] as? [[String: Any]] ?? []
    expect("stable logical keys create exactly two bounded children", firstNodes.count, 2)
    expect("an unappproved current gate blocks its node",
           firstNodes.first { $0["key"] as? String == "w0" }?["planningState"] as? String,
           "blocked")
    expect("a dependency blocks its successor",
           firstNodes.first { $0["key"] as? String == "w1" }?["planningState"] as? String,
           "blocked")
    expect("the planning projection declares advisory authority",
           (plan?["frontier"] as? [String: Any])?["authority"] as? String, "advisory_only")
    check("planning output cannot masquerade as broker execution authority",
          (plan?["frontier"] as? [String: Any])?["executionAuthority"] as? Bool == false)

    let approved = d.send("approve_program_gate", [
        "itemId": program, "planId": "ubuntu-runtime", "planVersion": 1,
        "gateKey": "architecture", "decision": "approved", "note": "Approved for W0.",
    ], actor: "architect")
    expect("the named gate authority can approve the exact plan revision", approved.status, 200)
    let approvedW0 = ((d.item(program)["programPlan"] as? [String: Any])?["nodes"]
        as? [[String: Any]])?.first { $0["key"] as? String == "w0" }
    expect("a supported dependency-free node enters only the planning frontier",
           approvedW0?["planningState"] as? String, "planning_ready")

    let documentID = (d.item(program)["documentReferences"] as? [[String: Any]])?.first?["id"]
        as? String ?? ""
    var secondFields = firstFields
    secondFields["planVersion"] = 2
    secondFields["predecessorVersion"] = 1
    secondFields["document"] = ["documentId": "ubuntu-runtime-plan", "version": 2,
        "title": "Ubuntu runtime plan v2",
        "url": documentURL.replacingOccurrences(of: "v1", with: "v2"),
        "supersedesId": documentID]
    secondFields["nodes"] = [
        ["key": "w0", "graphNodeId": "w0-contract", "title": "Contract baseline revised",
         "type": "task", "summary": "Freeze the portable contract.", "owner": "architecture-2",
         "dependsOn": [], "gateKeys": ["architecture"],
         "capabilityKeys": ["linux-core"], "claims": ["Sources/Core.swift"]],
        ["key": "w2", "graphNodeId": "w2-host", "title": "Host boundary",
         "type": "task", "summary": "Add the host seam.", "owner": "platform",
         "dependsOn": [], "gateKeys": [],
         "capabilityKeys": ["linux-core"], "claims": ["Sources/Linux.swift"]],
        ["key": "w3", "graphNodeId": "w3-cloud", "title": "Cloud boundary",
         "type": "task", "summary": "Keep missing observations closed.", "owner": "cloud",
         "dependsOn": [], "gateKeys": [],
         "capabilityKeys": ["unobserved-capability"], "claims": ["Sources/Cloud.swift"]],
    ]
    secondFields["gates"] = []
    secondFields["capabilities"] = []
    let second = d.send("plan_structure", secondFields, requestId: "plan-v2")
    expect("a monotonic successor Program Plan imports", second.status, 200)
    let secondPlan = d.item(program)["programPlan"] as? [String: Any]
    let secondNodes = secondPlan?["nodes"] as? [[String: Any]] ?? []
    expect("omitting an existing logical key never deletes it", secondNodes.count, 4)
    expect("a stable logical-key upsert preserves the child identity",
           secondNodes.first { $0["key"] as? String == "w0" }?["itemId"] as? String,
           firstNodes.first { $0["key"] as? String == "w0" }?["itemId"] as? String)
    expect("a stable logical-key upsert updates its bounded fields",
           secondNodes.first { $0["key"] as? String == "w0" }?["title"] as? String,
           "Contract baseline revised")
    expect("a retained capability omitted by the successor fails closed as unknown",
           secondNodes.first { $0["key"] as? String == "w2" }?["planningState"] as? String,
           "unknown")
    expect("an unobserved required capability defaults to unknown",
           secondNodes.first { $0["key"] as? String == "w3" }?["planningState"] as? String,
           "unknown")
    expect("a new plan revision cannot inherit an old gate approval",
           secondNodes.first { $0["key"] as? String == "w0" }?["planningState"] as? String,
           "blocked")
    expect("a stale gate approval is refused",
           boardError(d.send("approve_program_gate", [
               "itemId": program, "planId": "ubuntu-runtime", "planVersion": 1,
               "gateKey": "architecture", "decision": "approved", "note": "stale",
           ], actor: "architect")), "program_gate_revision_conflict")
    expect("a retained gate omitted by the successor cannot be approved",
           boardError(d.send("approve_program_gate", [
               "itemId": program, "planId": "ubuntu-runtime", "planVersion": 2,
               "gateKey": "architecture", "decision": "approved", "note": "stale evidence",
           ], actor: "architect")), "program_gate_stale")

    let beforeRefusals = d.revision
    var stale = secondFields
    stale["planVersion"] = 3; stale["predecessorVersion"] = 1
    expect("a stale plan predecessor is refused",
           boardError(d.send("plan_structure", stale)), "program_plan_predecessor_conflict")
    var duplicate = secondFields
    duplicate["planVersion"] = 3; duplicate["predecessorVersion"] = 2
    var duplicateNodes = duplicate["nodes"] as! [[String: Any]]
    duplicateNodes.append(duplicateNodes[0]); duplicate["nodes"] = duplicateNodes
    expect("duplicate logical keys are refused before mutation",
           boardError(d.send("plan_structure", duplicate)), "program_plan_duplicate_key")
    var cyclic = duplicate
    var cyclicNodes = secondFields["nodes"] as! [[String: Any]]
    cyclicNodes[0]["dependsOn"] = ["w2"]; cyclicNodes[1]["dependsOn"] = ["w0"]
    cyclic["nodes"] = cyclicNodes
    cyclic["gates"] = [["key": "architecture", "title": "Architecture approval",
                         "authority": "architect"]]
    cyclic["document"] = ["documentId": "ubuntu-runtime-plan", "version": 3,
        "title": "Ubuntu runtime plan v3",
        "url": documentURL.replacingOccurrences(of: "v1", with: "v3"),
        "supersedesId": secondPlan?["documentReferenceId"] as? String ?? ""]
    expect("a Program Plan cycle is refused before mutation",
           boardError(d.send("plan_structure", cyclic)), "program_plan_cycle")
    var lateInvalid = duplicate
    var lateNodes = secondFields["nodes"] as! [[String: Any]]
    lateNodes.append(["key": "late", "graphNodeId": "late", "title": "Late invalid",
        "type": "not-a-board-type", "summary": "invalid after valid rows", "owner": "root",
        "dependsOn": [], "gateKeys": [], "capabilityKeys": [], "claims": []])
    lateInvalid["nodes"] = lateNodes
    expect("a late invalid node rejects the complete draft atomically",
           boardError(d.send("plan_structure", lateInvalid)), "program_plan_node_invalid")
    var importedApproval = duplicate
    importedApproval["gates"] = [["key": "architecture", "title": "Architecture approval",
        "authority": "architect", "status": "approved"]]
    expect("plan import cannot smuggle a gate approval",
           boardError(d.send("plan_structure", importedApproval)), "program_plan_gate_invalid")
    expect("all invalid drafts leave revision and children untouched", d.revision, beforeRefusals)

    let replay = d.send("plan_structure", firstFields, expected: initialRevision,
                        requestId: "plan-v1")
    expect("an old exact replay returns its original Program receipt", replay.status, 200)
    let replayReceipt = replay.body["programPlanReceipt"] as? [String: Any]
    expect("replay does not recompute the receipt from the current plan",
           replayReceipt?["planVersion"] as? Int, 1)
    expect("the original Program receipt is explicitly a replay",
           replayReceipt?["replay"] as? Bool, true)
    expect("replaying a settled import never advances revision", d.revision, beforeRefusals)
    expect("schema-v1 stores without Program fields remain readable",
           ProjectBoardStore(url: d.file).readHeader().available, true)

    let corruptFile = d.root.appendingPathComponent("corrupt-program.json")
    if let data = try? Data(contentsOf: d.file),
       var object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
       var items = object["items"] as? [[String: Any]],
       let position = items.firstIndex(where: { $0["id"] as? String == program }),
       var persistedPlan = items[position]["programPlan"] as? [String: Any],
       var capabilities = persistedPlan["capabilities"] as? [[String: Any]],
       !capabilities.isEmpty {
        capabilities[0]["status"] = "future_status"
        persistedPlan["capabilities"] = capabilities
        items[position]["programPlan"] = persistedPlan
        object["items"] = items
        if let corruptData = try? JSONSerialization.data(withJSONObject: object,
                                                          options: [.sortedKeys]) {
            try? corruptData.write(to: corruptFile, options: .atomic)
        }
    }
    let corruptStore = ProjectBoardStore(url: corruptFile)
    check("a persisted Program Record with unknown closed state makes Board unavailable",
          corruptStore.readHeader().available == false)
    expect("corrupt persisted Program state returns the typed Store failure",
           boardError(corruptStore.command([
               "operation": "set_enabled", "requestId": "corrupt-probe",
               "expectedRevision": 0, "enabled": true,
           ], actor: "owner")), "board_store_corrupt")
}


func boardProgramGraphBindingProof() {
    let file = FileManager.default.temporaryDirectory
        .appendingPathComponent("program-graph-binding-\(UUID().uuidString).json")
    defer { try? FileManager.default.removeItem(at: file) }
    let store = ProjectBoardStore(url: file)
    _ = store.ensureProject(id: "program-project", name: "Program")
    var serial = 0
    func command(_ operation: String, _ fields: [String: Any]) -> ProjectBoardStore.Reply {
        serial += 1
        var body = fields; body["operation"] = operation
        body["requestId"] = "program-integration-\(serial)"
        body["expectedRevision"] = store.readHeader().revision
        return store.command(body, actor: "program-root")
    }
    let program = command("create", ["projectId": "program-project", "type": "epic",
        "title": "Canonical program", "owner": "program-root"]).body["itemId"] as? String ?? ""
    func item(_ id: String) -> [String: Any] {
        (store.snapshot(item: id)["board"] as? [String: Any])?["item"]
            as? [String: Any] ?? [:]
    }
    let key = item(program)["key"] as? String ?? ""
    let url = "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=integration-plan.md"
    expect("Program integration fixture imports", command("plan_structure", [
        "schemaVersion": 1, "itemId": program, "programKey": key,
        "planId": "integration-plan", "planVersion": 1,
        "graphId": "integration-graph", "destination": "Exact integration",
        "document": ["documentId": "integration-document", "version": 1,
                     "title": "Integration plan", "url": url],
        "nodes": [
            ["key": "first", "graphNodeId": "node-a", "title": "Same title",
             "type": "task", "summary": "First", "owner": "a", "dependsOn": [],
             "gateKeys": [], "capabilityKeys": [], "claims": []],
            ["key": "second", "graphNodeId": "node-b", "title": "Same title",
             "type": "task", "summary": "Second", "owner": "b", "dependsOn": [],
             "gateKeys": [], "capabilityKeys": [], "claims": []],
        ], "gates": [], "capabilities": [],
    ]).status, 200)
    let nodes = (item(program)["programPlan"] as? [String: Any])?["nodes"]
        as? [[String: Any]] ?? []
    let target = nodes.first { $0["key"] as? String == "second" }?["itemId"] as? String ?? ""
    let task: [String: Any] = ["id": "exact-node-task", "title": "unrelated title",
        "state": "briefed", "workItemId": program, "workPhase": "output",
        "graph": ["id": "integration-graph", "destination": "Exact integration",
                  "current_node": "node-b", "kind": "delivery"]]
    expect("broker ingestion accepts the exact Program graph node",
           store.ingest(task: task, projectID: "program-project").status, .accepted)
    let targetLinks = item(target)["links"] as? [[String: Any]] ?? []
    check("graph node identity, never a repeated title, owns the broker attempt",
          targetLinks.contains { $0["kind"] as? String == "task"
              && $0["targetId"] as? String == "exact-node-task" })
    check("the Program remains a container rather than a second execution truth",
          !(item(program)["links"] as? [[String: Any]] ?? []).contains {
              $0["targetId"] as? String == "exact-node-task"
          })
    var unknown = task
    unknown["id"] = "unknown-node-task"
    unknown["graph"] = ["id": "integration-graph", "destination": "Exact integration",
                        "current_node": "not-imported", "kind": "delivery"]
    expect("a Program cannot guess an unimported graph node from title or membership",
           store.ingest(task: unknown, projectID: "program-project").reason,
           "program_node_binding_unresolved")
    check("a refused unknown node creates no Program task relation",
          !(item(program)["links"] as? [[String: Any]] ?? []).contains {
              $0["targetId"] as? String == "unknown-node-task"
          })
}

func boardHistoricalTaskBindingReconciliationProof() {
    let d = BoardTestDriver(name: "historical-task-reconciliation-\(UUID().uuidString)")
    d.createProject()
    let program = d.create(type: "epic", title: "Canonical Program", owner: "program-root")
    let programKey = d.item(program)["key"] as? String ?? ""
    let documentURL = "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=historical-reconciliation.md"
    expect("historical reconciliation fixture imports its canonical Program node",
           d.send("plan_structure", [
               "schemaVersion": 1, "itemId": program, "programKey": programKey,
               "planId": "historical-program", "planVersion": 1,
               "graphId": "canonical-program-graph", "destination": "Canonical delivery",
               "document": ["documentId": "historical-program", "version": 1,
                            "title": "Historical reconciliation", "url": documentURL],
               "nodes": [["key": "w4-3", "graphNodeId": "w4-3",
                           "title": "Canonical W4-3", "type": "feature",
                           "summary": "One canonical Program node.", "owner": "runtime-owner",
                           "dependsOn": [], "gateKeys": [], "capabilityKeys": [],
                           "claims": ["Tests/UbuntuLifecycleTests.swift"]]],
               "gates": [], "capabilities": [],
           ]).status, 200)
    let target = (((d.item(program)["programPlan"] as? [String: Any])?["nodes"]
        as? [[String: Any]])?.first?["itemId"] as? String) ?? ""

    let landed: [String: Any] = [
        "state": "landed", "verification_origin": "local_target_branch",
        "verified_commit": "landed-commit", "verified_target_commit": "landed-target",
        "landed_at": 200.0,
    ]
    let implementationTask: [String: Any] = [
        "id": "historical-implementation", "title": "Inferred implementation",
        "state": "success", "startedAt": 100.0, "finishedAt": 150.0,
        "workPhase": "output", "child": ["sessionId": "historical-session"],
        "graph": ["id": "noncanonical-one-node-graph", "current_node": "w4-3",
                  "destination": "Inferred delivery"], "landing": landed,
    ]
    _ = d.store.ingest(task: implementationTask, projectID: "project-1")
    _ = d.store.ingest(task: [
        "id": "historical-review", "title": "Inferred review", "state": "success",
        "startedAt": 151.0, "finishedAt": 175.0,
        "review": ["axes": [["findings": [["id": "review-finding",
                    "summary": "A retained review finding.", "severity": "blocking"]]]]],
    ], projectID: "project-1")
    _ = d.store.ingest(task: [
        "id": "historical-correction", "title": "Inferred correction", "state": "success",
        "startedAt": 176.0, "finishedAt": 199.0, "landing": landed,
    ], projectID: "project-1")

    func source(_ taskID: String) -> [String: Any] {
        let board = d.store.snapshot(project: "project-1")["board"] as? [String: Any]
        return (board?["items"] as? [[String: Any]])?.first { item in
            (item["links"] as? [[String: Any]])?.contains {
                $0["kind"] as? String == "task" && $0["targetId"] as? String == taskID
            } == true
        } ?? [:]
    }
    func binding(_ taskID: String) -> [String: Any] {
        let item = source(taskID)
        let link = (item["links"] as? [[String: Any]])?.first {
            $0["kind"] as? String == "task" && $0["targetId"] as? String == taskID
        } ?? [:]
        let landing = (item["landings"] as? [[String: Any]])?.first
        return [
            "taskId": taskID, "sourceItemId": item["id"] as Any,
            "taskLinkId": link["id"] as Any,
            "sourceGraphId": link["graphId"] ?? NSNull(),
            "sourceGraphNodeId": link["graphNodeId"] ?? NSNull(),
            "landing": landing.map { row in
                ["evidenceId": row["id"] as Any, "sourceId": row["sourceId"] as Any,
                 "subject": row["subject"] as Any] as [String: Any]
            } ?? NSNull(),
        ]
    }
    let bindings = [binding("historical-implementation"), binding("historical-review"),
                    binding("historical-correction")]
    let sourcePairs: [(String, String)] = bindings.compactMap { row in
        guard let taskID = row["taskId"] as? String,
              let itemID = row["sourceItemId"] as? String else { return nil }
        return (taskID, itemID)
    }
    let sourceItemIDs = Dictionary(uniqueKeysWithValues: sourcePairs)
    let fields: [String: Any] = [
        "projectId": "project-1", "programItemId": program, "programKey": programKey,
        "planId": "historical-program", "planVersion": 1,
        "programGraphId": "canonical-program-graph", "nodeId": "w4-3",
        "targetItemId": target, "bindings": bindings,
    ]
    func injectedConflict(_ kind: String) -> ProjectBoardStore.Reply {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent(
            "historical-\(kind)-conflict-\(UUID().uuidString).json")
        defer { try? FileManager.default.removeItem(at: file) }
        var root = try! JSONSerialization.jsonObject(with: Data(contentsOf: d.file))
            as! [String: Any]
        var items = root["items"] as! [[String: Any]]
        let sourceID = sourceItemIDs[kind == "span" ? "historical-implementation"
                                                    : "historical-review"]!
        let sourceIndex = items.firstIndex { $0["id"] as? String == sourceID }!
        let targetIndex = items.firstIndex { $0["id"] as? String == target }!
        if kind == "span" {
            let exact = (items[sourceIndex]["spans"] as! [[String: Any]]).first!
            var collision = exact
            collision["phase"] = "verification"
            var rows = items[targetIndex]["spans"] as! [[String: Any]]
            rows.append(exact); rows.append(collision); items[targetIndex]["spans"] = rows
        } else if kind == "source-evidence" {
            var rows = items[sourceIndex]["evidence"] as! [[String: Any]]
            var collision = rows.first!
            collision["summary"] = "Different retained source content"
            rows.append(collision); items[sourceIndex]["evidence"] = rows
        } else {
            let exact = (items[sourceIndex]["evidence"] as! [[String: Any]]).first!
            var collision = exact
            collision["summary"] = "Different retained content"
            var rows = items[targetIndex]["evidence"] as! [[String: Any]]
            rows.append(exact); rows.append(collision); items[targetIndex]["evidence"] = rows
        }
        root["items"] = items
        try! JSONSerialization.data(withJSONObject: root, options: [.sortedKeys]).write(to: file)
        let store = ProjectBoardStore(url: file)
        let revision = (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int ?? -1
        var command = fields
        command["operation"] = "reconcile_historical_task_binding"
        command["requestId"] = "historical-\(kind)-conflict"
        command["expectedRevision"] = revision
        let reply = store.command(command, actor: "machine", trusted: true)
        expect("\(kind) conflict leaves the Board revision unchanged",
               (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int, revision)
        let board = store.snapshot(project: "project-1")["board"] as? [String: Any]
        let unchanged = board?["items"] as? [[String: Any]] ?? []
        let sourceLinks = unchanged.first { $0["id"] as? String == sourceID }?["links"]
            as? [[String: Any]] ?? []
        let targetLinks = unchanged.first { $0["id"] as? String == target }?["links"]
            as? [[String: Any]] ?? []
        check("\(kind) conflict leaves source and target accounting unmoved",
              !sourceLinks.isEmpty && targetLinks.isEmpty)
        return reply
    }
    func injectedExactDuplicate() -> ProjectBoardStore.Reply {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("historical-exact-duplicate-\(UUID().uuidString).json")
        defer { try? FileManager.default.removeItem(at: file) }
        var root = try! JSONSerialization.jsonObject(with: Data(contentsOf: d.file)) as! [String: Any]
        var items = root["items"] as! [[String: Any]]
        let sourceID = sourceItemIDs["historical-review"]!
        let sourceIndex = items.firstIndex { $0["id"] as? String == sourceID }!
        let targetIndex = items.firstIndex { $0["id"] as? String == target }!
        let exact = (items[sourceIndex]["evidence"] as! [[String: Any]]).first!
        var targetEvidence = items[targetIndex]["evidence"] as! [[String: Any]]
        targetEvidence.append(exact); items[targetIndex]["evidence"] = targetEvidence
        root["items"] = items
        try! JSONSerialization.data(withJSONObject: root, options: [.sortedKeys]).write(to: file)
        let store = ProjectBoardStore(url: file)
        let revision = (store.snapshot()["board"] as? [String: Any])?["revision"] as? Int ?? -1
        var command = fields
        command["operation"] = "reconcile_historical_task_binding"
        command["requestId"] = "historical-exact-duplicate"
        command["expectedRevision"] = revision
        let reply = store.command(command, actor: "machine", trusted: true)
        let board = store.snapshot(project: "project-1")["board"] as? [String: Any]; let changed = board?["items"] as? [[String: Any]] ?? []
        let sourceEvidence = changed.first { $0["id"] as? String == sourceID }?["findings"] as? [[String: Any]] ?? []
        let targetEvidenceAfter = changed.first { $0["id"] as? String == target }?["findings"] as? [[String: Any]] ?? []; let exactID = exact["id"] as? String
        check("an exact destination duplicate is safely deduplicated",
              !sourceEvidence.contains { $0["id"] as? String == exactID }
                  && targetEvidenceAfter.filter {
                  $0["id"] as? String == exactID
              }.count == 1)
        return reply
    }
    expect("different retained evidence refuses the whole repair",
           boardError(injectedConflict("evidence")), "historical_binding_fact_conflict")
    expect("different retained span refuses the whole repair",
           boardError(injectedConflict("span")), "historical_binding_fact_conflict")
    expect("different same-source evidence refuses before the batch moves",
           boardError(injectedConflict("source-evidence")), "historical_binding_fact_conflict")
    expect("byte-identical retained evidence may deduplicate",
           injectedExactDuplicate().status, 200)
    let before = d.revision
    let result = d.send("reconcile_historical_task_binding", fields, actor: "machine",
                        trusted: true, expected: before, requestId: "historical-rebind")
    expect("an exact retained-facts reconciliation is accepted atomically", result.status, 200)
    let receipt = result.body["historicalTaskBindingReceipt"] as? [String: Any]
    expect("the reconciliation receipt names the canonical Program node", receipt?["nodeId"] as? String,
           "w4-3")
    expect("the reconciliation receipt retains every exact task identity",
           receipt?["taskCount"] as? Int, 3)
    expect("one reconciliation consumes one Board revision", d.revision, before + 1)
    let canonical = d.item(target)
    let canonicalLinks = canonical["links"] as? [[String: Any]] ?? []
    expect("all historical task facts have one canonical accounting owner",
           canonicalLinks.filter { $0["kind"] as? String == "task" }.count, 3)
    expect("broker landing receipts are transferred rather than minted",
           (canonical["landings"] as? [[String: Any]])?.count, 2)
    expect("review findings follow their exact task provenance",
           (canonical["findings"] as? [[String: Any]])?.count, 1)
    expect("historical reconciliation moves facts without promoting lifecycle",
           canonical["state"] as? String, "backlog")
    expect("the selected item exposes one durable reconciliation receipt",
           (canonical["historicalTaskBindingReconciliations"] as? [[String: Any]])?.count, 1)
    for taskID in ["historical-implementation", "historical-review", "historical-correction"] {
        let old = sourceItemIDs[taskID].map(d.item) ?? [:]
        check("reconciled source \(taskID) keeps its inferred row and audit history",
              !old.isEmpty && (old["history"] as? [[String: Any]])?.contains {
                  $0["kind"] as? String == "historical_task_binding_reconciled"
              } == true)
        check("reconciled source \(taskID) no longer owns task accounting",
              (old["links"] as? [[String: Any]])?.contains {
                  $0["kind"] as? String == "task" && $0["targetId"] as? String == taskID
              } == false)
    }
    let reloaded = ProjectBoardStore(url: d.file)
    let reloadedTarget = ((reloaded.snapshot(item: target)["board"] as? [String: Any])?["item"]
        as? [String: Any]) ?? [:]
    expect("the reconciliation receipt survives restart",
           (reloadedTarget["historicalTaskBindingReconciliations"] as? [[String: Any]])?.count, 1)
    let future = reloaded.ingest(task: implementationTask, projectID: "project-1")
    check("a repaired graph-bearing task remains ingestible after restart",
          future.persisted && future.status != .unavailable)
    let futureTarget = ((reloaded.snapshot(item: target)["board"] as? [String: Any])?["item"]
        as? [String: Any]) ?? [:]
    expect("future broker ingestion stays on the canonical Program node",
           (futureTarget["links"] as? [[String: Any]])?.filter {
               $0["kind"] as? String == "task"
                   && $0["targetId"] as? String == "historical-implementation"
           }.count, 1)
    let futureSource = sourceItemIDs["historical-implementation"].map {
        ((reloaded.snapshot(item: $0)["board"] as? [String: Any])?["item"] as? [String: Any]) ?? [:]
    } ?? [:]
    check("future ingestion preserves the inferred source audit row",
          (futureSource["history"] as? [[String: Any]])?.contains {
              $0["kind"] as? String == "historical_task_binding_reconciled"
          } == true)
    let replay = d.send("reconcile_historical_task_binding", fields, actor: "machine",
                        trusted: true, expected: before, requestId: "historical-rebind")
    expect("the exact command replays after its original CAS revision", replay.status, 200)
    expect("replay does not create a second reconciliation", d.revision, before + 1)
    expect("replay is explicit in the returned receipt",
           (replay.body["historicalTaskBindingReceipt"] as? [String: Any])?["replay"] as? Bool,
           true)

    let refusal = BoardTestDriver(name: "historical-task-refusal-\(UUID().uuidString)")
    refusal.createProject()
    let refusedItem = refusal.create(type: "epic", title: "No authority")
    let refusedRevision = refusal.revision
    expect("ordinary Board authority cannot reconcile broker history",
           refusal.send("reconcile_historical_task_binding", fields.merging([
               "programItemId": refusedItem, "targetItemId": refusedItem,
           ]) { _, new in new }, expected: refusedRevision).status, 403)
    expect("authority refusal leaves the Board revision unchanged", refusal.revision, refusedRevision)

    ProjectBoardHTTP.configureStoreForTesting(refusal.store)
    defer { ProjectBoardHTTP.configureStoreForTesting(nil) }
    var publicBody = fields
    publicBody["operation"] = "reconcile_historical_task_binding"
    publicBody["requestId"] = "public-reconciliation"
    publicBody["expectedRevision"] = refusedRevision
    let publicRequest = remoteRequest("POST", "/v1/board", body: String(data:
        try! JSONSerialization.data(withJSONObject: publicBody), encoding: .utf8)!)
    expect("the HTTP door requires the local machine credential before parsing facts",
           ProjectBoardHTTP.route(publicRequest, machine: false,
                                  permission: .allowed(device: "admin", caps: [.read, .send, .admin]))?.status,
           403)
}

private func boardRootLandingStoreProof() {
    let file = FileManager.default.temporaryDirectory.appendingPathComponent("root-landing-store-\(UUID().uuidString).json")
    defer { try? FileManager.default.removeItem(at: file) }
    var clock = 10.0
    let store = ProjectBoardStore(url: file, now: { Date(timeIntervalSince1970: clock) })
    _ = store.ensureProject(id: "root-project", name: "Root")
    func command(_ op: String, _ fields: [String: Any], root: Bool = false) -> ProjectBoardStore.Reply {
        var body = fields
        body["operation"] = op; body["requestId"] = UUID().uuidString
        body["expectedRevision"] = store.readHeader().revision
        return store.command(body, actor: "test-root", trusted: true, rootLandingOrigin: root)
    }
    let item = command("create", ["projectId": "root-project", "type": "feature", "title": "Root scope"]).body["itemId"] as! String
    let sha = String(repeating: "a", count: 40)
    let fields: [String: Any] = ["itemId": item, "projectId": "root-project", "runId": "run-root",
        "sessionId": "11111111-1111-4111-8111-111111111111", "commit": sha,
        "targetCommit": sha, "target": "main", "landedAt": 20.0]
    let forgedItem = command("create", ["projectId": "root-project", "type": "feature", "title": "Forgery target"]).body["itemId"] as! String
    var forged = fields; forged["itemId"] = forgedItem
    expect("root landing cannot be minted by trusted public command", command("record_root_landing", forged).status, 403)
    var foreign = fields; foreign["projectId"] = "foreign-project"
    expect("root landing exact project binding refuses foreign project", command("record_root_landing", foreign, root: true).status, 409)
    clock = 30
    _ = command("update", ["itemId": item, "summary": "scope changed after historical landing"])
    clock = 31
    expect("root Store fixture verification is admitted", command("record_evidence", ["itemId": item, "kind": "verification", "status": "passed",
        "sourceId": "root-exact", "subject": sha, "summary": "verified current scope"]).status, 200)
    func row(_ source: ProjectBoardStore) -> [String: Any] {
        (source.readSeed(rebuild: true).seed.envelope(item: item)["board"] as! [String: Any])["item"] as! [String: Any]
    }
    let before = row(store)
    expect("delayed root proof is retained as history", command("record_root_landing", fields, root: true).status, 200)
    let historical = row(store)
    check("old root landing cannot promote the newer same-SHA scope", (historical["currentEvidence"] as? [String: Any])?["landingId"] is NSNull)
    expect("root history does not change accepted scope", historical["scopeRevision"] as? Int, before["scopeRevision"] as? Int)
    expect("delayed root history does not change lifecycle", historical["state"] as? String, before["state"] as? String)
    clock = 40
    var current = fields; current["landedAt"] = clock; current["runId"] = "run-current"
    expect("current exact root landing can satisfy delivery evidence", command("record_root_landing", current, root: true).status, 200)
    let fresh = row(store)
    check("current root proof has a current landing pointer", (fresh["currentEvidence"] as? [String: Any])?["landingId"] is String)
    let reloaded = row(ProjectBoardStore(url: file))
    expect("root evidence survives Store reload", (reloaded["landings"] as? [[String: Any]])?.count, 2)
    expect("root evidence reload retains current pointer", (reloaded["currentEvidence"] as? [String: Any])?["landingId"] as? String, (fresh["currentEvidence"] as? [String: Any])?["landingId"] as? String)

    var coverage = ProjectBoardIntegration.IngestionCoverage()
    let unmanaged = ProjectBoardWorkflow.Outcome(status: 200, code: "root_landing_unmanaged",
        authority: "broker_observed", runID: nil, itemID: nil, outboxPending: 0)
    coverage.recordRootLanding(unmanaged, sourceID: "unmanaged")
    expect("unmanaged root cannot poison Project A", coverage.reasons(projectID: "project-a"), [])
    expect("unmanaged root cannot poison Project B", coverage.reasons(projectID: "project-b"), [])
    let failed = ProjectBoardWorkflow.Outcome(status: 503, code: "workflow_persistence_failed",
        authority: "broker_observed", runID: "run-managed", itemID: item, outboxPending: 0,
        projectID: "project-a")
    coverage.recordRootLanding(failed, sourceID: "managed")
    expect("managed root failure is scoped to its own Project", coverage.reasons(projectID: "project-a"), ["workflow_persistence_failed"])
    expect("another Project remains unaffected by managed root failure", coverage.reasons(projectID: "project-b"), [])
    coverage.retainRootLandings([])
    expect("consumed receipt leaves no stale current projection issue", coverage.issueCount, 0)
    expect("retired gap is counted as unproved not repaired", coverage.retiredRootGapCount, 1)
    for index in 0...ProjectBoardIntegration.IngestionCoverage.rootIssueLimit {
        coverage.recordRootLanding(failed, sourceID: "bounded-\(index)")
    }
    expect("root projection coverage remains bounded", coverage.issueCount, ProjectBoardIntegration.IngestionCoverage.rootIssueLimit)
    expect("coverage eviction remains an explicit unproved gap", coverage.evictedRootGapCount, 1)
}

private func boardItemKeyPersistenceProof() {
    let d = BoardTestDriver(name: "key-retirement-\(UUID().uuidString)")
    d.createProject()
    let canonical = d.create(title: "Canonical destination")
    let task: [String: Any] = ["id": "key-task", "title": "Historical attempt", "state": "success",
        "graph": ["id": "key-graph", "destination": "Inferred destination", "current_node": "work"]]
    _ = d.store.ingest(task: task, projectID: "project-1")
    var attributed = task; attributed["workItemId"] = canonical
    _ = d.store.ingest(task: attributed, projectID: "project-1")
    let reopened = ProjectBoardStore(url: d.file)
    func create(_ store: ProjectBoardStore, _ request: String, project: String = "project-1", expected: Int? = nil) -> ProjectBoardStore.Reply {
        let revision = expected ?? ((store.snapshot()["board"] as? [String: Any])?["revision"] as? Int ?? -1)
        return store.command(["operation": "create", "requestId": request, "expectedRevision": revision,
                              "projectId": project, "title": "Next work", "type": "feature"], actor: "key-proof")
    }
    func key(_ reply: ProjectBoardStore.Reply) -> String? {
        ((reply.body["board"] as? [String: Any])?["item"] as? [String: Any])?["key"] as? String
    }
    expect("retired highest ordinal survives restart", key(create(reopened, "after-restart")), "CLA-3")
    _ = reopened.ensureProject(id: "project-1", name: "Renamed")
    expect("project rename does not reset its ordinal", key(create(reopened, "after-rename")), "REN-4")
    _ = reopened.ensureProject(id: "project-2", name: "Clawdline")
    expect("different Project keeps its independent key namespace",
           key(create(reopened, "second-project", project: "project-2")), "CLA-1")

    let legacy = BoardTestDriver(name: "key-legacy-\(UUID().uuidString)")
    legacy.createProject()
    let first = legacy.create(title: "Latency")
    let second = legacy.create(title: "Workflow feedback")
    do {
        var json = try JSONSerialization.jsonObject(with: Data(contentsOf: legacy.file)) as! [String: Any]
        var projects = json["projects"] as! [[String: Any]]
        for index in projects.indices { projects[index].removeValue(forKey: "itemKeyHighWater") }
        var items = json["items"] as! [[String: Any]]
        for index in items.indices { items[index]["key"] = "CLA-369" }
        json["projects"] = projects; json["items"] = items
        try JSONSerialization.data(withJSONObject: json).write(to: legacy.file, options: .atomic)
        let originalBytes = try Data(contentsOf: legacy.file)
        let loaded = ProjectBoardStore(url: legacy.file)
        let rows = (loaded.snapshot()["board"] as? [String: Any])?["items"] as? [[String: Any]] ?? []
        expect("legacy duplicate human keys remain two UUID identities", Set(rows.compactMap { $0["id"] as? String }), Set([first, second]))
        check("legacy load never rewrites the journal merely to allocate keys", try Data(contentsOf: legacy.file) == originalBytes)
        check("legacy collisions are surfaced explicitly without title merge",
              rows.count == 2 && rows.allSatisfy { $0["keyStatus"] as? String == "ambiguous" })
        check("compact list transport preserves each ambiguous UUID's key status",
              rows.count == 2 && rows.allSatisfy {
                  ProjectBoardIntegration.compactCard($0)["keyStatus"] as? String == "ambiguous"
              })
        let beforeCreate = (loaded.snapshot()["board"] as? [String: Any])?["revision"] as? Int
        let created = create(loaded, "legacy-create", expected: beforeCreate)
        expect("legacy high key seeds allocation above retained ordinals", key(created), "CLA-370")
        let createdItem = (created.body["board"] as? [String: Any])?["item"] as? [String: Any] ?? [:]
        expect("compact list transport preserves a noncolliding key status",
               ProjectBoardIntegration.compactCard(createdItem)["keyStatus"] as? String, "unique")
        expect("same semantic create replay does not allocate again", key(create(loaded, "legacy-create", expected: beforeCreate)), "CLA-370")
        let restarted = ProjectBoardStore(url: legacy.file)
        expect("migrated high water persists for the next process", key(create(restarted, "next-create")), "CLA-371")
        let existing = ((restarted.snapshot(item: first)["board"] as? [String: Any])?["item"] as? [String: Any]) ?? [:]
        expect("allocation never renumbers existing keys", existing["key"] as? String, "CLA-369")
        expect("allocation never promotes lifecycle", existing["state"] as? String, "backlog")

        json = try JSONSerialization.jsonObject(with: Data(contentsOf: legacy.file)) as! [String: Any]
        projects = json["projects"] as! [[String: Any]]
        projects[0]["itemKeyHighWater"] = Int.max; json["projects"] = projects
        try JSONSerialization.data(withJSONObject: json).write(to: legacy.file, options: .atomic)
        let exhausted = ProjectBoardStore(url: legacy.file)
        expect("exhausted ordinal refuses safely instead of wrapping", boardError(create(exhausted, "overflow")), "item_key_exhausted")
        let automatic = exhausted.ingest(task: ["id": "overflow-task", "title": "No key", "state": "success"], projectID: "project-1")
        expect("broker allocation shares the same exhausted-key refusal", automatic.reason, "item_key_exhausted")
        projects[0]["itemKeyHighWater"] = -1; json["projects"] = projects
        try JSONSerialization.data(withJSONObject: json).write(to: legacy.file, options: .atomic)
        let corrupt = ProjectBoardStore(url: legacy.file)
        expect("negative stored key watermark is corrupt, not reset", boardError(create(corrupt, "negative")), "board_store_corrupt")
    } catch { check("key persistence fixtures remain readable: \(error)", false) }
}

private func boardSessionDeliveryProjectionProof() {
    let d = BoardTestDriver(name: "session-delivery-\(UUID().uuidString)")
    d.createProject()
    let itemID = d.create(title: "Root delivery without broker task")
    let otherID = d.create(title: "Different item")
    let session = "00000000-0000-0000-0000-000000000356"
    let identity = ProjectBoardWorkflow.Identity(terminalID: "%356", provider: "codex",
        conversationID: session, projectID: "project-1", projectPath: "/tmp",
        processGeneration: "356:1.0")
    let journal = d.file.deletingLastPathComponent().appendingPathComponent("delivery-workflow.json")
    var calls: [[String: Any]] = []
    func open(_ file: URL? = nil, storeOverride: ProjectBoardStore? = nil,
              maximumBytes: Int = 2 * 1024 * 1024) -> ProjectBoardWorkflow {
        let store = storeOverride ?? d.store
        return ProjectBoardWorkflow(url: file ?? journal, boardHeader: {
            let h = store.readHeader(); return (h.enabled, h.revision, h.available)
        }, boardCommand: { body, actor in
            calls.append(body)
            return store.command(body, actor: actor, workflowOrigin: true)
        }, limits: ProjectBoardWorkflow.Limits(maximumBytes: maximumBytes), autoStart: false)
    }
    var workflow = open()
    func begin(_ key: String, phase: String = "output") -> String? {
        guard case .managed(let run) = workflow.prepareIngress(requestID: key,
            fingerprint: key, text: "work", imageCount: 0, identity: identity) else { return nil }
        _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
        _ = workflow.record(["operation": "begin", "run_id": run.runID,
            "classification": "existing_item", "item_id": itemID, "phase": phase],
            requestID: key + "-begin", fingerprint: key + "-begin", identity: identity)
        workflow.drainForTesting()
        return run.runID
    }
    func deliver(_ run: String, disposition: String = "delivered") {
        _ = workflow.record(["operation": "deliver", "run_id": run,
            "disposition": disposition, "summary": "Implementation handed back; not deployed.",
            "next_action": "Independent review and runtime acceptance"],
            requestID: run + "-delivery", fingerprint: run + "-delivery", identity: identity)
        workflow.drainForTesting()
    }
    func progress() -> [String: Any] { d.item(itemID)["progress"] as? [String: Any] ?? [:] }
    guard let run = begin("first") else { check("delivery fixture begins", false); return }
    let before = d.item(itemID), scope = before["scopeRevision"] as? Int
    let evidence = before["currentEvidence"] as? NSDictionary
    deliver(run)
    expect("a settled root delivery cannot regress to planning or unknown", progress()["state"] as? String, "delivered")
    expect("root delivery is explicit assistant attestation",
           (progress()["sessionDelivery"] as? [String: Any])?["authority"] as? String, "assistant_attested")
    expect("root delivery retains exact run identity",
           (progress()["sessionDelivery"] as? [String: Any])?["runId"] as? String, run)
    expect("root delivery remains inactive", progress()["active"] as? Bool, false)
    expect("root delivery never alters lifecycle", d.item(itemID)["state"] as? String, before["state"] as? String)
    expect("root delivery never changes scope", d.item(itemID)["scopeRevision"] as? Int, scope)
    check("root delivery never supplies verification or landing", d.item(itemID)["currentEvidence"] as? NSDictionary == evidence)
    expect("root delivery does not become a broker attempt",
           (progress()["attemptCounts"] as? [String: Any])?["total"] as? Int, 0)
    expect("unrelated item remains planning", (d.item(otherID)["progress"] as? [String: Any])?["state"] as? String, "planning")
    let revision = d.revision, callCount = calls.count
    deliver(run); workflow = open(); workflow.drainForTesting()
    expect("settled delivery replay neither rewrites Board nor resends commands", d.revision, revision)
    expect("settled delivery restart sends no duplicate commands", calls.count, callCount)
    let reloaded = ProjectBoardStore(url: d.file)
    let saved = (reloaded.snapshot(item: itemID)["board"] as? [String: Any])?["item"] as? [String: Any]
    expect("root delivery survives a Board restart", (saved?["progress"] as? [String: Any])?["state"] as? String, "delivered")
    do {
        var legacy = try JSONSerialization.jsonObject(with: Data(contentsOf: journal)) as! [String: Any]
        var runs = legacy["runs"] as! [[String: Any]]
        var events = runs[0]["events"] as! [[String: Any]]
        for i in events.indices {
            events[i].removeValue(forKey: "deliveryProjectionVersion")
            events[i]["settledIntents"] = (events[i]["settledIntents"] as? [[String: Any]] ?? [])
                .filter { $0["kind"] as? String != "record_session_delivery" }
        }
        runs[0]["events"] = events; legacy["runs"] = runs
        let file = d.root.appendingPathComponent("legacy-delivery.json")
        try JSONSerialization.data(withJSONObject: legacy).write(to: file)
        let legacyBytes = try Data(contentsOf: file)
        let tightFile = d.root.appendingPathComponent("legacy-tight-budget.json")
        try legacyBytes.write(to: tightFile)
        let beforeTight = calls.count
        let tight = open(tightFile, maximumBytes: legacyBytes.count + 64)
        expect("F1 valid old journal survives optional byte-budget deferral",
               tight.snapshot(runID: run)?["item_id"] as? String, itemID)
        expect("F1 tight journal is not an empty healthy replacement", tight.syncMode(), nil)
        tight.drainForTesting()
        expect("F1 deferred projection sends no old commands", calls.count, beforeTight)
        expect("F1 byte-budget deferral preserves the exact old journal", try Data(contentsOf: tightFile), legacyBytes)
        var oldBoard = try JSONSerialization.jsonObject(with: Data(contentsOf: d.file)) as! [String: Any]
        var oldItems = oldBoard["items"] as! [[String: Any]]
        for i in oldItems.indices { oldItems[i].removeValue(forKey: "sessionDeliveries") }
        oldBoard["items"] = oldItems
        oldBoard["receipts"] = (oldBoard["receipts"] as? [[String: Any]] ?? []).filter {
            !(($0["requestId"] as? String ?? "").contains("-session-delivery-"))
        }
        let oldBoardFile = d.root.appendingPathComponent("legacy-board.json")
        try JSONSerialization.data(withJSONObject: oldBoard).write(to: oldBoardFile)
        let oldStore = ProjectBoardStore(url: oldBoardFile)
        let beforeMigration = calls.count
        var migrated = open(file, storeOverride: oldStore); migrated.drainForTesting()
        migrated = open(file, storeOverride: oldStore); migrated.drainForTesting()
        expect("legacy settled delivery produces only its missing projection", calls.count - beforeMigration, 1)
        expect("legacy migration never replays old span/output/obligation bodies",
               calls.last?["operation"] as? String, "record_session_delivery")
        expect("legacy backfill persists exactly one Board mutation", oldStore.readHeader().revision, revision + 1)
        let recovered = (oldStore.snapshot(item: itemID)["board"] as? [String: Any])?["item"] as? [String: Any]
        expect("legacy root delivery is recovered without inventing landing",
               (recovered?["progress"] as? [String: Any])?["state"] as? String, "delivered")
        let changedBoardFile = d.root.appendingPathComponent("legacy-changed-board.json")
        try JSONSerialization.data(withJSONObject: oldBoard).write(to: changedBoardFile)
        let changedStore = ProjectBoardStore(url: changedBoardFile)
        for i in 0..<2 {
            _ = changedStore.command(["operation": "checklist", "requestId": "later-scope-\(i)",
                "expectedRevision": changedStore.readHeader().revision, "itemId": itemID,
                "title": "New scope \(i)", "required": true], actor: "root")
        }
        let changedJournal = d.root.appendingPathComponent("legacy-changed-workflow.json")
        try legacyBytes.write(to: changedJournal)
        let changedWorkflow = open(changedJournal, storeOverride: changedStore)
        changedWorkflow.drainForTesting()
        let changedItem = (changedStore.snapshot(item: itemID)["board"] as? [String: Any])?["item"] as? [String: Any]
        let historical = (changedItem?["progress"] as? [String: Any])?["sessionDelivery"] as? [String: Any]
        check("F3 backfill never fabricates a historical scope number", historical?["scopeRevision"] is NSNull)
        expect("F3 unresolved scope is explicit", historical?["scopeStatus"] as? String, "unresolved")
        expect("F3 unresolved scope is not current", historical?["current"] as? Bool, false)
        runs[0].removeValue(forKey: "spanStart"); legacy["runs"] = runs
        let unknownFile = d.root.appendingPathComponent("legacy-unknown.json")
        try JSONSerialization.data(withJSONObject: legacy).write(to: unknownFile)
        let beforeUnknown = calls.count
        let unknown = open(unknownFile); unknown.drainForTesting()
        expect("legacy missing identity remains unprojected instead of guessed", calls.count, beforeUnknown)
    } catch { check("legacy delivery fixture remains readable", false) }
    if let source = calls.first(where: { $0["operation"] as? String == "record_session_delivery" }) {
        func refuse(_ name: String, mutate: (inout [String: Any]) -> Void,
                    actor: String = identity.actor, origin: Bool = true) {
            var body = source; body["requestId"] = "refuse-" + name
            body["expectedRevision"] = d.revision; mutate(&body)
            let result = d.store.command(body, actor: actor, workflowOrigin: origin)
            check("delivery refuses \(name)", result.status == 403 || result.status == 409)
            expect("refused \(name) leaves Board unchanged", d.revision, revision)
        }
        refuse("public forgery", mutate: { _ in }, origin: false)
        refuse("foreign actor", mutate: { _ in }, actor: "workflow:codex:other")
        refuse("wrong item", mutate: { $0["itemId"] = otherID })
        refuse("wrong project", mutate: { $0["projectId"] = "other-project" })
        refuse("changed immutable summary", mutate: { $0["summary"] = "pretend deployed" })
    } else { check("workflow produces the typed delivery command", false) }
    guard let newer = begin("second") else { return }
    expect("new activity outranks earlier delivered work", progress()["active"] as? Bool, true)
    expect("F2 old delivery is historical while new declared work is active",
           (progress()["sessionDelivery"] as? [String: Any])?["current"] as? Bool, false)
    _ = workflow.record(["operation": "handoff", "run_id": newer,
        "owner": "receiving-session", "note": "Receiver must accept; no delivery declared"],
        requestID: "second-handoff", fingerprint: "second-handoff", identity: identity)
    workflow.drainForTesting()
    check("F2 old delivery cannot resurface after a newer handoff", progress()["state"] as? String != "delivered")
    expect("F2 ended newer interval still makes old delivery historical",
           (progress()["sessionDelivery"] as? [String: Any])?["current"] as? Bool, false)
    deliver(newer, disposition: "interrupted")
    check("interrupted newer turn does not retain the old delivered claim", progress()["state"] as? String != "delivered")
    guard let plan = begin("plan", phase: "planning") else { return }
    deliver(plan)
    expect("planning delivery is not implementation delivery", progress()["state"] as? String, "planning")
    // Existing known-good proof is not the same thing as accepting all required scope.
    _ = d.send("checklist", ["itemId": itemID, "title": "Runtime acceptance still required", "required": true])
    _ = d.send("record_evidence", ["itemId": itemID, "kind": "verification", "status": "passed",
        "sourceId": "source-focused-pass", "subject": String(repeating: "a", count: 40),
        "summary": "Focused source tests passed, runtime acceptance not observed."], trusted: true)
    expect("known verification with missing acceptance is not unknown", progress()["state"] as? String, "review_testing")
    expect("acceptance debt is waiting, not active", progress()["group"] as? String, "waiting")
    expect("acceptance debt never inflates active count", progress()["active"] as? Bool, false)
    check("acceptance debt has an explicit reason", (progress()["warningCodes"] as? [String] ?? []).contains("checklist_evidence_incomplete"))
    expect("historical Session delivery does not become evidence for newer scope",
           (progress()["sessionDelivery"] as? [String: Any])?["current"] as? Bool, false)
}

func runProjectBoardIntegrationTests() {
group("broker live transitions reach the Board before task completion") {
    boardHistoricalTaskBindingReconciliationProof()
    boardSessionDeliveryProjectionProof()
    boardItemKeyPersistenceProof()
    boardRootLandingStoreProof()
    do {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("board-session-index-\(UUID().uuidString).json")
        defer { try? FileManager.default.removeItem(at: file) }
        let store = ProjectBoardStore(url: file)
        _ = store.ensureProject(id: "project-a", name: "A")
        _ = store.ensureProject(id: "project-b", name: "B")
        func send(_ operation: String, _ fields: [String: Any]) -> ProjectBoardStore.Reply {
            var body = fields
            body["operation"] = operation
            body["requestId"] = UUID().uuidString
            body["expectedRevision"] = store.readHeader().revision
            return store.command(body, actor: "root")
        }
        let conversation = "abcdefab-cdef-4abc-8def-abcdefabcdef"
        let a = send("create", ["projectId": "project-a", "title": "First", "type": "feature"]).body["itemId"] as! String
        let b = send("create", ["projectId": "project-b", "title": "Second", "type": "task"]).body["itemId"] as! String
        _ = send("link", ["itemId": a, "kind": "session", "targetId": conversation, "label": "Owner"])
        _ = send("span", ["itemId": a, "sessionId": conversation, "phase": "planning"])
        _ = send("link", ["itemId": b, "kind": "session", "targetId": conversation, "label": "Related"])
        _ = send("link", ["itemId": b, "kind": "session", "targetId": "other-session", "label": "Other"])
        let revision = store.readHeader().revision
        let seed = store.readSeed(rebuild: true).seed
        let result = seed.envelope(item: "session:" + conversation)["board"] as? [String: Any]
        let rows = result?["items"] as? [[String: Any]] ?? []
        expect("reverse Board lookup echoes exact conversation", result?["sessionId"] as? String, conversation)
        expect("reverse lookup joins cross-Project links once per item", rows.count, 2)
        check("reverse lookup preserves both owning Projects", Set(rows.compactMap { $0["projectId"] as? String }) == ["project-a", "project-b"])
        expect("reverse lookup marks declarations rather than fabricating a primary item",
               rows.first { $0["id"] as? String == a }?["sessionActivity"] as? String, "declared")
        let missing = seed.envelope(item: "session:22222222-2222-4222-8222-222222222222")["board"] as? [String: Any]
        expect("unrelated Session has no guessed association", (missing?["items"] as? [[String: Any]])?.count, 0)
        check("reverse lookup stays compact", rows.allSatisfy { $0["history"] == nil && $0["links"] == nil && $0["spans"] == nil })
        expect("reading relations changes no durable revision", store.readHeader().revision, revision)
        let reloaded = ProjectBoardStore(url: file).readSeed(rebuild: true).seed
        expect("reverse lookup reconstructs from durable links after restart",
               ((reloaded.envelope(item: "session:" + conversation)["board"] as? [String: Any])?["items"] as? [[String: Any]])?.count, 2)
        let uppercase = conversation.uppercased()
        let uppercaseBoard = seed.envelope(item: "session:" + uppercase)["board"] as? [String: Any]
        expect("the same uppercase conversation UUID reaches the canonical reverse-index row",
               (uppercaseBoard?["items"] as? [[String: Any]])?.count, 2)
        expect("reverse-index replies spell a conversation UUID canonically",
               uppercaseBoard?["sessionId"] as? String, conversation)
        let uppercaseAdmission = ProjectBoardHTTP.admit(
            remoteRequest("GET", "/v1/board?item=session:" + uppercase),
            machine: true, permission: .denied)
        if case .some(.read(let read)) = uppercaseAdmission {
            expect("HTTP admission canonicalizes uppercase UUID before lookup", read.item,
                   "session:" + conversation)
        } else { check("uppercase UUID reaches Board read admission", false) }

        let reader = send("create", ["projectId": "project-a", "title": "Reader bounds",
                                      "type": "feature"]).body["itemId"] as! String
        var previousReferenceID: String?
        for version in 1...129 {
            var fields: [String: Any] = [
                "itemId": reader, "title": "Plan v\(version)", "purpose": "plan",
                "documentId": "long-plan", "version": version,
                "url": "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=plan-v\(version).md",
            ]
            if let previousReferenceID { fields["supersedesId"] = previousReferenceID }
            let reply = send("document_reference", fields)
            expect("long document history version \(version) is admitted", reply.status, 200)
            previousReferenceID = (((reply.body["board"] as? [String: Any])?["item"]
                as? [String: Any])?["documentReferences"] as? [[String: Any]])?
                .first { $0["version"] as? Int == version }?["id"] as? String
        }
        for number in 0..<20 {
            _ = send("checklist", ["itemId": reader, "title": "Optional \(number)",
                                    "required": false])
        }
        _ = send("obligation", ["itemId": reader, "title": "Late blocking proof",
                                  "owner": "root", "blocking": true,
                                  "actorKind": "agent", "requiredAction": "Run focused proof"])
        let boundedSeed = store.readSeed(rebuild: true).seed
        let boundedItem = ((boundedSeed.envelope(project: "project-a", item: reader)["board"]
            as? [String: Any])?["item"] as? [String: Any]) ?? [:]
        let visibleDocuments = boundedItem["documentReferences"] as? [[String: Any]] ?? []
        check("a history beyond 128 retains the current logical document head",
              visibleDocuments.count == 128
                && visibleDocuments.contains { $0["version"] as? Int == 129
                    && $0["status"] as? String == "current" })
        let remaining = boundedItem["remainingWork"] as? [String: Any]
        let firstWork = remaining?["work"] as? [[String: Any]] ?? []
        check("blocking remaining work cannot be displaced by sixteen optional rows",
              firstWork.contains { $0["title"] as? String == "Late blocking proof"
                && $0["blocking"] as? Bool == true })
        ProjectBoardHTTP.configureStoreForTesting(store)
        defer { ProjectBoardHTTP.configureStoreForTesting(nil) }
        func collection(_ kind: String, _ offset: Int) -> [String: Any] {
            let selector = "collection:\(reader):\(kind):\(offset)"
            let response = ProjectBoardHTTP.route(
                remoteRequest("GET", "/v1/board?project=project-a&item=" + selector),
                machine: true, permission: .denied)
            let object = response.flatMap {
                try? JSONSerialization.jsonObject(with: $0.body) as? [String: Any]
            }
            return ((object?["board"] as? [String: Any])?["collection"]
                as? [String: Any]) ?? [:]
        }
        let laterDocuments = collection("document_references", 128)
        expect("document continuation is a bounded final page",
               (laterDocuments["rows"] as? [[String: Any]])?.count, 1)
        expect("document continuation names the complete retained count",
               laterDocuments["totalCount"] as? Int, 129)
        let laterWork = collection("remaining_work", 16)
        expect("remaining-work continuation exposes every retained omitted row",
               (laterWork["rows"] as? [[String: Any]])?.count, 5)

        let unicodeItem = send("create", ["projectId": "project-a", "title": "URL parity",
                                            "type": "task"]).body["itemId"] as! String
        func documentStatus(_ documentID: String, url: String) -> Int {
            send("document_reference", [
                "itemId": unicodeItem, "title": documentID, "purpose": "reference",
                "documentId": documentID, "version": 1, "url": url,
            ]).status
        }
        let utf8Limit = "https://app.clawdline.com/#document=1&machine="
            + String(repeating: "😀", count: 32)
            + "&session=s&scope=project&path=" + String(repeating: "界", count: 169) + ".md"
        expect("Swift accepts the same exact UTF-8 document boundary as the browser",
               documentStatus("utf8-limit", url: utf8Limit), 200)
        let utf8Overflow = "https://app.clawdline.com/#document=1&machine="
            + String(repeating: "😀", count: 33)
            + "&session=s&scope=project&path=a.md"
        expect("Swift refuses a UTF-16-short identity beyond the shared UTF-8 bound",
               documentStatus("utf8-overflow", url: utf8Overflow), 400)
        let reservedURL = "https://app.clawdline.com/#document=1&machine=Mac%25356+%26+%23+%2B&session=session%2Bliteral%25&scope=project&path=notes%2F%25356+%26+%23+%2B.md"
        expect("Swift form-decodes percent, ampersand, hash and plus exactly once",
               documentStatus("reserved-once", url: reservedURL), 200)
        expect("Swift refuses traversal after one form decode",
               documentStatus("real-traversal", url: "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=..%2Fa.md"), 400)
        expect("Swift does not decode a literal encoded traversal twice",
               documentStatus("literal-encoded-traversal", url: "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=%252E%252E%252Fa.md"), 200)
        expect("Swift ignores empty form pairs like URLSearchParams",
               documentStatus("empty-form-pairs", url: "https://app.clawdline.com/#&document=1&&machine=m&session=s&scope=project&path=a.md&"), 200)
        expect("an empty document fragment is refused without a trap",
               documentStatus("empty-fragment", url: "https://app.clawdline.com/#"), 400)
    }
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
    // Exercise the real Store -> materialized seed -> compact boundary, not a detail-shaped mock.
    let file = FileManager.default.temporaryDirectory.appendingPathComponent("board-partition-\(UUID().uuidString).json")
    defer { try? FileManager.default.removeItem(at: file) }
    let store = ProjectBoardStore(url: file)
    _ = store.ensureProject(id: "partition", name: "Partition")
    func send(_ op: String, _ fields: [String: Any]) -> ProjectBoardStore.Reply {
        store.command(fields.merging(["operation": op, "requestId": UUID().uuidString,
            "expectedRevision": store.readHeader().revision]) { _, new in new }, actor: "owner")
    }
    var ids: [String] = []
    for title in ["Future", "Required scope", "Blocking", "Decision", "Active planning"] {
        ids.append(send("create", ["projectId": "partition", "title": title,
                                  "type": "feature", "owner": "owner"]).body["itemId"] as! String)
    }
    _ = send("checklist", ["itemId": ids[1], "title": "Future requirement", "required": true])
    // An obligation beyond the bounded detail prefix must still be in the compact counts.
    for i in 0..<20 {
        _ = send("obligation", ["itemId": ids[2], "title": "Optional \(i)",
                               "owner": "owner", "blocking": false, "actorKind": "agent"])
    }
    _ = send("obligation", ["itemId": ids[2], "title": "Actual blocker",
                           "owner": "owner", "blocking": true, "actorKind": "agent"])
    _ = send("obligation", ["itemId": ids[3], "title": "Optional user choice",
                           "owner": "user", "blocking": false, "actorKind": "user"])
    _ = send("span", ["itemId": ids[4], "sessionId": "planning-owner", "phase": "planning"])
    let revision = store.readHeader().revision
    let seed = store.readSeed(rebuild: true).seed
    let snapshot = seed.envelope(project: "partition")["board"] as! [String: Any]
    let modelRows = snapshot["items"] as! [[String: Any]]
    let expectedGroups = ["planning", "planning", "waiting", "waiting", "active"]
    for (i, id) in ids.enumerated() {
        let row = modelRows.first { $0["id"] as? String == id }!
        let compact = ProjectBoardIntegration.compactCard(row)
        let detail = (seed.envelope(project: "partition", item: id)["board"] as! [String: Any])["item"] as! [String: Any]
        let summary = compact["listSummary"] as? [String: Any]
        expect("partition \(i) follows facts across compact projection", summary?["group"] as? String, expectedGroups[i])
        check("partition \(i) compact and detail share exact classification facts",
              summary != nil && NSDictionary(dictionary: summary!).isEqual(to: detail["listSummary"] as? [String: Any] ?? [:]))
        check("partition \(i) does not ship raw obligations or remaining work", compact["obligations"] == nil && compact["remainingWork"] == nil)
    }
    let blockingSummary = modelRows.first { $0["id"] as? String == ids[2] }?["listSummary"] as? [String: Any]
    expect("attention counts include late blockers beyond the display prefix",
           (blockingSummary?["attention"] as? [String: Int])?["blockingObligations"], 1)
    let project = (snapshot["projects"] as! [[String: Any]]).first { $0["id"] as? String == "partition" }!
    let counts = (project["summary"] as? [String: Any])?["listGroups"] as? [String: Int]
    expect("model counts separate all retained future plans", counts?["planning"], 2)
    expect("model counts separate real attention", counts?["waiting"], 2)
    expect("active planning remains actual declared activity", counts?["active"], 1)
    expect("exclusive model partition accounts for every retained row", counts?.values.reduce(0, +), 5)
    expect("classification reads never mutate lifecycle revision", store.readHeader().revision, revision)
    let reloaded = ProjectBoardStore(url: file).readSeed(rebuild: true).seed.envelope(project: "partition")["board"] as! [String: Any]
    let reloadedProject = (reloaded["projects"] as! [[String: Any]]).first!
    check("partition is reconstructible after reload", NSDictionary(dictionary: (reloadedProject["summary"] as? [String: Any])?["listGroups"] as? [String: Int] ?? [:]).isEqual(to: counts ?? [:]))
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
    expect("machine auth preserves baseline local control", call("{}", machine: true)?.status, 400); let catalogHTTP = BoardTestDriver(name: "catalog-http-\(UUID().uuidString)"); catalogHTTP.createProject(); let catalogHTTPItem = catalogHTTP.create(title: "Technical receipt"), catalogEntry: [String: Any] = ["itemId": catalogHTTPItem, "expectedScopeRevision": 0, "audience": "agent", "role": "execution_record", "outcome": "technical_detail", "reason": "HTTP authority proof"], catalogHTTPBody: [String: Any] = ["operation": "reconcile_catalog", "requestId": "catalog-http", "expectedRevision": catalogHTTP.revision, "projectId": "project-1", "auditId": "catalog-http-audit", "items": [catalogEntry]], catalogHTTPJSON = String(data: try! JSONSerialization.data(withJSONObject: catalogHTTPBody), encoding: .utf8)!; Config.shared.remoteWrite = true; ProjectBoardHTTP.configureStoreForTesting(catalogHTTP.store); expect("paired admin cannot forge catalog reconciliation authority from an otherwise valid command", call(catalogHTTPJSON, caps: [.read, .send, .admin])?.status, 403); expect("local machine route admits exact catalog reconciliation", call(catalogHTTPJSON, machine: true)?.status, 200); var largeCatalogBody = catalogHTTPBody; largeCatalogBody["requestId"] = "catalog-http-large"; largeCatalogBody["expectedRevision"] = catalogHTTP.revision; var largeCatalogEntry = catalogEntry; largeCatalogEntry["reason"] = String(repeating: "x", count: 1_000); largeCatalogBody["items"] = Array(repeating: largeCatalogEntry, count: 500); let largeCatalogJSON = String(data: try! JSONSerialization.data(withJSONObject: largeCatalogBody), encoding: .utf8)!; check("the full 500-row catalog surface can exceed the ordinary command body cap", largeCatalogJSON.utf8.count > 64 * 1_024); check("the full 500-row catalog fixture remains within its absolute bounded cap", largeCatalogJSON.utf8.count <= 768 * 1_024); expect("paired admin keeps the ordinary command body cap", call(largeCatalogJSON, caps: [.read, .send, .admin])?.status, 400); let machineLargeCatalog = call(largeCatalogJSON, machine: true); expect("machine catalog reconciliation reaches the exact Store validator above 64 KiB", machineLargeCatalog?.status, 409); check("large machine catalog failure is semantic rather than a transport body refusal", String(data: machineLargeCatalog?.body ?? Data(), encoding: .utf8)?.contains("catalog_item_conflict") == true); let oversizedOrdinaryJSON = "{\"operation\":\"update\",\"requestId\":\"large-update\",\"expectedRevision\":\(catalogHTTP.revision),\"itemId\":\"\(catalogHTTPItem)\",\"summary\":\"" + String(repeating: "x", count: 70 * 1_024) + "\"}", oversizedOrdinary = call(oversizedOrdinaryJSON, machine: true); expect("machine ordinary commands keep the ordinary body cap", oversizedOrdinary?.status, 400); check("ordinary machine rejection identifies the pre-parse 64 KiB boundary", String(data: oversizedOrdinary?.body ?? Data(), encoding: .utf8)?.contains("may exceed 64 KiB") == true); var absoluteOversizeBody = catalogHTTPBody; absoluteOversizeBody["requestId"] = "catalog-http-absolute-oversize"; var absoluteOversizeEntry = catalogEntry; absoluteOversizeEntry["reason"] = String(repeating: "x", count: 800 * 1_024); absoluteOversizeBody["items"] = [absoluteOversizeEntry]; let absoluteOversizeJSON = String(data: try! JSONSerialization.data(withJSONObject: absoluteOversizeBody), encoding: .utf8)!; check("absolute catalog refusal fixture exceeds 768 KiB", absoluteOversizeJSON.utf8.count > 768 * 1_024); let absoluteOversize = call(absoluteOversizeJSON, machine: true); expect("machine catalog reconciliation retains an absolute body cap", absoluteOversize?.status, 400); check("absolute catalog refusal identifies the bounded request limit", String(data: absoluteOversize?.body ?? Data(), encoding: .utf8)?.contains("exceeds the bounded request limit") == true); ProjectBoardHTTP.configureStoreForTesting(nil); Config.shared.remoteWrite = false
    let repeated = remoteRequest("GET", "/v1/board?item=a&item=b")
    expect("duplicate query is refused", ProjectBoardHTTP.route(repeated, machine: true, permission: .denied)?.status, 400)
    for query in ["item=session:%251", "item=session:not-a-uuid",
                  "project=p&item=session:11111111-1111-4111-8111-111111111111",
                  "report=r&item=session:11111111-1111-4111-8111-111111111111"] {
        let reply = ProjectBoardHTTP.route(remoteRequest("GET", "/v1/board?" + query),
                                           machine: true, permission: .denied)
        expect("reverse Session selector refuses malformed or mixed scope: \(query)", reply?.status, 400)
        check("reverse Session selector refusal is typed: \(query)",
              String(data: reply?.body ?? Data(), encoding: .utf8)?.contains("invalid_session_selector") == true)
    }
    let sessionSelector = "session:11111111-1111-4111-8111-111111111111"
    let admitted = ProjectBoardHTTP.admit(remoteRequest("GET", "/v1/board?item=" + sessionSelector),
                                         machine: false, permission: .allowed(device: "reader", caps: [.read]))
    if case .some(.read(let read)) = admitted {
        expect("reverse Session read preserves exact conversation selector", read.item, sessionSelector)
        check("reverse Session read cannot carry a guessed Project", read.project == nil && read.report == nil)
    } else { check("reverse Session read reaches the ordinary bounded read admission", false) }
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

    ProjectBoardStore.materializationPauseForTesting = nil
    let capacityURL = root.appendingPathComponent("capacity.json")
    let capacityStore = ProjectBoardStore(url: capacityURL)
    _ = capacityStore.ensureProject(id: "capacity", name: "Capacity")
    func capacitySend(_ operation: String, _ fields: [String: Any]) -> ProjectBoardStore.Reply {
        var body = fields
        body["operation"] = operation
        body["requestId"] = UUID().uuidString
        body["expectedRevision"] = capacityStore.readHeader().revision
        return capacityStore.command(body, actor: "test")
    }
    let parent = capacitySend("create", ["projectId": "capacity", "title": "Parent",
                                           "type": "epic"]).body["itemId"] as! String
    _ = capacitySend("create", ["projectId": "capacity", "title": "Template",
                                 "type": "task", "parentId": parent])
    var capacityJSON = try! JSONSerialization.jsonObject(with: Data(contentsOf: capacityURL))
        as! [String: Any]
    let templates = capacityJSON["items"] as! [[String: Any]]
    var items = [templates[0]]
    for number in 0..<500 {
        var child = templates[1]
        child["id"] = "capacity-child-\(number)"
        child["key"] = "CAP-\(number)"
        child["title"] = "Child \(number)"
        child["parentId"] = parent
        items.append(child)
    }
    capacityJSON["items"] = items
    try! JSONSerialization.data(withJSONObject: capacityJSON, options: [.sortedKeys])
        .write(to: capacityURL, options: .atomic)
    let indexedStore = ProjectBoardStore(url: capacityURL)
    var indexOperations = 0
    ProjectBoardStore.materializationIndexOperationForTesting = { indexOperations += 1 }
    defer { ProjectBoardStore.materializationIndexOperationForTesting = nil }
    let capacitySeed = indexedStore.readSeed(rebuild: true).seed
    expect("capacity materialization retains every child", capacitySeed.itemDetailsByID.count, 501)
    check("childrenByParent construction and traversal remain linear at capacity",
          indexOperations <= items.count * 2,
          "\(indexOperations) indexed operations for \(items.count) items")
    let searchRevision = indexedStore.readHeader().revision, firstSearch = indexedStore.catalogSearchSnapshot(project: "capacity", query: "Child", offset: 0, expectedRevision: searchRevision), secondSearch = indexedStore.catalogSearchSnapshot(project: "capacity", query: "Child", offset: 64, expectedRevision: searchRevision), identitySearch = indexedStore.catalogSearchSnapshot(project: "capacity", query: "capacity-child-499", offset: 0, expectedRevision: searchRevision), firstSearchBoard = firstSearch.body["board"] as? [String: Any], secondSearchBoard = secondSearch.body["board"] as? [String: Any], firstSearchMeta = firstSearchBoard?["catalogSearch"] as? [String: Any]; check("complete-catalog search reaches beyond the ordinary 500-row projection", firstSearch.status == 200 && (firstSearchBoard?["items"] as? [[String: Any]])?.count == 64 && firstSearchMeta?["totalCount"] as? Int == 500 && firstSearchMeta?["nextOffset"] as? Int == 64); check("complete-catalog continuation is stable, revision-pinned, bounded, and identity-searchable", secondSearch.status == 200 && (secondSearchBoard?["items"] as? [[String: Any]])?.count == 64 && (secondSearchBoard?["catalogSearch"] as? [String: Any])?["revision"] as? Int == searchRevision && (((identitySearch.body["board"] as? [String: Any])?["items"] as? [[String: Any]])?.first?["id"] as? String) == "capacity-child-499"); let mutation = indexedStore.command(["operation": "create", "requestId": "catalog-search-mutation", "expectedRevision": searchRevision, "projectId": "capacity", "title": "Child after first page", "type": "task"], actor: "test"); let staleSearch = indexedStore.catalogSearchSnapshot(project: "capacity", query: "Child", offset: 64, expectedRevision: searchRevision); check("a mutation between catalog pages refuses continuation instead of skipping or duplicating rows", mutation.status == 200 && staleSearch.status == 409 && ((staleSearch.body["error"] as? [String: Any])?["code"] as? String) == "catalog_search_revision_conflict")
}
}
