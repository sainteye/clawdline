import Foundation

private enum WorkflowSyncFixtureError: Error { case refused }

private func workflowLegacyArtifactProof(_ seedData: Data, root: URL, identity: ProjectBoardWorkflow.Identity) {
    do {
        var journal = try JSONSerialization.jsonObject(with: seedData) as! [String: Any]
        var runs = journal["runs"] as! [[String: Any]], rows: [[String: Any]] = []
        var events = runs[0]["events"] as! [[String: Any]]; let start = runs[0]["spanStart"] as! [String: Any]
        for i in events.indices {
            for intent in events[i]["settledIntents"] as? [[String: Any]] ?? [] {
                let kind = intent["kind"] as! String
                rows.append(["id": intent["id"]!, "version": 1, "runID": runs[0]["id"]!,
                    "eventID": events[i]["id"]!, "kind": kind == "record_output" ? "artifact" : kind,
                    "index": intent["index"]!, "status": "complete", "attempts": 1, "updatedAt": 1,
                    "preparedRequestID": kind == "span" ? start["requestID"]! : "legacy-proof-request"])
            }
            events[i].removeValue(forKey: "settledIntents")
        }
        runs[0]["events"] = events; runs[0].removeValue(forKey: "spanStart")
        journal["runs"] = runs; journal["outbox"] = rows; journal["schemaVersion"] = 2
        func verifyCopy(_ data: Data, name: String, expectedRows: Int) throws {
            let file = root.appendingPathComponent(name + ".json"); try data.write(to: file)
            let original = try JSONSerialization.jsonObject(with: data) as! [String: Any]
            let oldRows = original["outbox"] as! [[String: Any]]
            expect("\(name) exact input row count", oldRows.count, expectedRows)
            var calls = 0
            func open() -> ProjectBoardWorkflow {
                ProjectBoardWorkflow(url: file, boardHeader: { (true, 1, true) },
                    ensureProject: { _, _ in (true, nil) },
                    boardCommand: { _, _ in calls += 1; return ProjectBoardStore.Reply(status: 200, body: [:]) }, autoStart: false)
            }
            var reopened = open(); let mode = reopened.syncMode()
            expect("\(name) legacy artifact migration accepts", mode, nil)
            guard mode == nil else { return }
            reopened.drainForTesting(); reopened = open(); reopened.drainForTesting()
            expect("\(name) restart never replays completed artifacts", calls, 0)
            let saved = try JSONSerialization.jsonObject(with: Data(contentsOf: file)) as! [String: Any]
            expect("\(name) migration durably writes schema3", saved["schemaVersion"] as? Int, 3)
            let remaining = saved["outbox"] as! [[String: Any]]
            expect("\(name) preserves exact failed identities", remaining.map { $0["id"] as! String }.sorted(),
                oldRows.filter { $0["status"] as? String != "complete" }.map { $0["id"] as! String }.sorted())
            guard case .managed(let next) = reopened.prepareIngress(requestID: "post-migration-send",
                fingerprint: "post-migration", text: "proof", imageCount: 0, identity: identity) else {
                check("\(name) post-migration ingress accepted", false); return
            }
            _ = reopened.markDelivery(runID: next.runID, identity: identity, delivered: true)
            let body: [String: Any] = ["operation": "begin", "run_id": next.runID,
                "classification": "existing_item", "item_id": "local-proof-item", "phase": "review_testing"]
            expect("\(name) helper begin semantic boundary succeeds", reopened.record(body,
                requestID: "post-migration-begin", fingerprint: "begin", identity: identity).status, 202)
            reopened.drainForTesting(); reopened = open()
            expect("\(name) begin replay retains receipt", reopened.record(body,
                requestID: "post-migration-begin", fingerprint: "begin", identity: identity).status, 202)
            reopened.drainForTesting(); expect("\(name) only new link/span execute once", calls, 2)
        }
        try verifyCopy(JSONSerialization.data(withJSONObject: journal), name: "synthetic", expectedRows: 9)
        if let fixture = ProcessInfo.processInfo.environment["CLAWDLINE_LEGACY_WORKFLOW_FIXTURE"] {
            let original = try Data(contentsOf: URL(fileURLWithPath: fixture))
            try verifyCopy(original, name: "formal514-copy", expectedRows: 514)
            expect("formal source fixture remains byte-identical", try Data(contentsOf: URL(fileURLWithPath: fixture)), original)
        }
        for variant in ["wrong-index", "unknown-kind", "duplicate-id"] {
            var bad = journal, badRows = rows; let i = badRows.firstIndex { $0["kind"] as? String == "artifact" }!
            if variant == "wrong-index" { badRows[i]["index"] = -1 }
            if variant == "unknown-kind" { badRows[i]["kind"] = "artifact_guess" }
            if variant == "duplicate-id" { badRows.append(badRows[i]) }
            bad["outbox"] = badRows; let file = root.appendingPathComponent(variant + ".json")
            let bytes = try JSONSerialization.data(withJSONObject: bad); try bytes.write(to: file)
            let refused = ProjectBoardWorkflow(url: file, boardHeader: { (true, 1, true) }, autoStart: false)
            expect("\(variant) remains fail-closed", refused.syncMode(), "workflow_persistence_failed")
            expect("\(variant) does not rewrite refused journal", try Data(contentsOf: file), bytes)
        }
    } catch { check("legacy artifact fixture completes", false) }
}

private func workflowSettledCapacityProof() {
    let root = workflowTestDirectory("settled-capacity")
    defer { try? FileManager.default.removeItem(at: root) }
    let file = root.appendingPathComponent("journal.json"), identity = workflowIdentity()
    var calls: [String] = [], bodies: [[String: Any]] = []
    func make(_ capacity: Int = 3) -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(url: file, boardHeader: { (true, 1, true) },
            boardCommand: { body, _ in
                calls.append(body["requestId"] as? String ?? "missing")
                bodies.append(body)
                return ProjectBoardStore.Reply(status: 200, body: ["itemId": "item-settled"])
            }, limits: ProjectBoardWorkflow.Limits(runs: 16, eventsPerRun: 64,
                receipts: 128, outbox: capacity, maximumBytes: 2 * 1024 * 1024), autoStart: false)
    }
    var workflow = make()
    guard case .managed(let run) = workflow.prepareIngress(requestID: "settled-turn",
        fingerprint: "settled-turn-body", text: "work", imageCount: 0, identity: identity) else {
        check("capacity fixture admits its run", false); return
    }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    let begin: [String: Any] = ["operation": "begin", "run_id": run.runID,
        "classification": "new_work", "title": "capacity work", "type": "feature", "phase": "output"]
    expect("create reserves its future link and span", workflow.record(begin,
        requestID: "settled-begin", fingerprint: "settled-begin-body", identity: identity).status, 202)
    workflow.drainForTesting()
    expect("create dependency executes exactly three initial commands", calls.count, 3)
    for i in 0..<6 {
        let body: [String: Any] = ["operation": "progress", "run_id": run.runID,
            "summary": "step", "outputs": [["title": "proof", "url": "https://example.test/\(i)", "kind": "website"]]]
        let result = workflow.record(body, requestID: "settled-progress-\(i)",
            fingerprint: "settled-body-\(i)", identity: identity)
        expect("settled capacity admits later work \(i)", result.status, 202)
        workflow.drainForTesting()
        workflow = make()
        expect("same request replays after restart \(i)", workflow.record(body,
            requestID: "settled-progress-\(i)", fingerprint: "settled-body-\(i)", identity: identity).status, 202)
        workflow.drainForTesting()
    }
    expect("compacted intent identities never re-execute", calls.count, 9)
    expect("every executed Board request identity is unique", Set(calls).count, calls.count)
    if let data = try? Data(contentsOf: file),
       let saved = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
        expect("completed payload rows no longer consume outbox", (saved["outbox"] as? [Any])?.count, 0)
        workflowLegacyArtifactProof(data, root: root, identity: identity)
    } else { check("compacted journal remains readable", false) }

    // A capacity of two must reject a create before any of its three dependent writes execute.
    let smallFile = root.appendingPathComponent("small.json")
    var smallCalls = 0
    let small = ProjectBoardWorkflow(url: smallFile, boardHeader: { (true, 1, true) },
        boardCommand: { _, _ in smallCalls += 1; return ProjectBoardStore.Reply(status: 200, body: ["itemId": "small"]) },
        limits: ProjectBoardWorkflow.Limits(runs: 16, eventsPerRun: 64, receipts: 128,
            outbox: 2, maximumBytes: 2 * 1024 * 1024), autoStart: false)
    guard case .managed(let smallRun) = small.prepareIngress(requestID: "small",
        fingerprint: "small", text: "work", imageCount: 0, identity: identity) else {
        check("small capacity fixture admits ingress", false); return
    }
    _ = small.markDelivery(runID: smallRun.runID, identity: identity, delivered: true)
    var smallBegin = begin; smallBegin["run_id"] = smallRun.runID
    expect("fanout capacity refuses before side effects", small.record(smallBegin,
        requestID: "small-begin", fingerprint: "small-begin", identity: identity).code, "workflow_outbox_full")
    small.drainForTesting()
    expect("capacity refusal does not execute partial create", smallCalls, 0)

    // Reconstruct a bounded legacy v2 journal with 514 real identity-linked rows, not a
    // production-file edit. Completed creates carry downstream item identity across migration.
    do {
        var saved = try JSONSerialization.jsonObject(with: Data(contentsOf: file)) as! [String: Any]
        let template = (saved["runs"] as! [[String: Any]])[0]
        let beginEvent = (template["events"] as! [[String: Any]])[0]
        var runs: [[String: Any]] = [], rows: [[String: Any]] = []
        for i in 0..<171 {
            var copy = template, event = beginEvent
            let rid = "legacy-run-\(i)", eid = "legacy-event-\(i)"
            event["id"] = eid; event.removeValue(forKey: "settledIntents")
            // v2 predates the run-level span identity; do not copy this v3 fixture's identity.
            copy.removeValue(forKey: "spanStart")
            copy["id"] = rid; copy["events"] = [event]; runs.append(copy)
            for kind in ["create", "link", "span"] {
                rows.append(["id": "\(eid)-\(kind)", "version": 1, "runID": rid, "eventID": eid,
                    "kind": kind, "index": 0, "status": "complete", "attempts": 1,
                    "createdItemID": "item-settled", "updatedAt": 1,
                    "preparedRequestID": "workflow-\(eid)-\(kind)-1"])
            }
        }
        var failedEvent = beginEvent
        failedEvent["id"] = "failed-event"; failedEvent["operation"] = "progress"
        failedEvent.removeValue(forKey: "settledIntents")
        failedEvent["outputs"] = [["title": "output", "url": "https://example.test/failed", "kind": "website"]]
        var events = runs[0]["events"] as! [[String: Any]]; events.append(failedEvent); runs[0]["events"] = events
        // Real old event retention removes settled sources but leaves their outbox payloads.
        runs[0]["events"] = [failedEvent]
        rows.append(["id": "failed-event-artifact-0", "version": 1, "runID": "legacy-run-0",
            "eventID": "failed-event", "kind": "record_output", "index": 0, "status": "failed",
            "attempts": 3, "failureCode": "retained_refusal", "updatedAt": 1])
        saved["schemaVersion"] = 2; saved["runs"] = runs; saved["outbox"] = rows; saved["receipts"] = []
        let legacyFile = root.appendingPathComponent("legacy.json")
        try JSONSerialization.data(withJSONObject: saved).write(to: legacyFile)
        var legacyCalls = 0
        func legacy() -> ProjectBoardWorkflow {
            ProjectBoardWorkflow(url: legacyFile, boardHeader: { (true, 1, true) },
                boardCommand: { _, _ in legacyCalls += 1; return ProjectBoardStore.Reply(status: 200, body: [:]) }, autoStart: false)
        }
        var reopened = legacy()
        expect("514-row legacy completed overflow migrates before live-capacity validation", reopened.syncMode(), nil)
        reopened.drainForTesting(); reopened = legacy(); reopened.drainForTesting()
        expect("migration and second restart never replay old completed mutations", legacyCalls, 0)
        let migrated = try JSONSerialization.jsonObject(with: Data(contentsOf: legacyFile)) as! [String: Any]
        let retained = migrated["outbox"] as! [[String: Any]]
        expect("failed-visible rows remain pinned, not erased by compaction", retained.count, 1)
        expect("failed identity/reason survives migration", retained.first?["failureCode"] as? String, "retained_refusal")
        let migratedRuns = migrated["runs"] as! [[String: Any]]
        let legacyStart = migratedRuns[0]["spanStart"] as? [String: Any]
        expect("legacy evicted begin retains exact successful span request",
               legacyStart?["requestID"] as? String, "workflow-legacy-event-0-span-1")
        var conflicted = migrated
        var conflictRows = retained
        conflictRows.append(["id": "legacy-event-1-link", "version": 1, "runID": "legacy-run-1",
            "eventID": "legacy-event-1", "kind": "link", "index": 0, "status": "pending",
            "attempts": 0, "updatedAt": 1])
        conflicted["outbox"] = conflictRows
        try JSONSerialization.data(withJSONObject: conflicted).write(to: legacyFile)
        let conflict = legacy()
        expect("pending identity overlapping settled tombstone fails closed", conflict.syncMode(), "workflow_persistence_failed")
        conflict.drainForTesting()
        expect("conflicting pending row cannot replay a settled mutation", legacyCalls, 0)
    } catch { check("legacy overflow fixture is decodable", false) }

    // Event retention must not discard the one exact identity needed to end this run's span.
    for i in 0..<65 {
        _ = workflow.record(["operation": "progress", "run_id": run.runID, "summary": "quiet update"],
            requestID: "trim-\(i)", fingerprint: "trim-\(i)", identity: identity)
    }
    workflow = make()
    expect("delivery after begin eviction is admitted", workflow.record([
        "operation": "deliver", "run_id": run.runID, "disposition": "delivered", "summary": "done", "next_action": "review"
    ], requestID: "trim-deliver", fingerprint: "trim-deliver", identity: identity).status, 202)
    workflow.drainForTesting()
    let start = bodies.first { $0["operation"] as? String == "span" }?["requestId"] as? String
    let end = bodies.last { $0["operation"] as? String == "end_span" }?["startRequestId"] as? String
    check("end_span preserves exact source identity after compaction, eviction and restart", start != nil && end == start)
}

private func workflowAssignmentLifecycleProof() {
    for variant in ["propose", "accepted", "declined", "cancelled"] {
        let root = workflowTestDirectory("assignment-lifecycle-\(variant)")
        defer { try? FileManager.default.removeItem(at: root) }
        let file = root.appendingPathComponent("board.json")
        var store = ProjectBoardStore(url: file)
        let identity = workflowIdentity()
        _ = store.ensureProject(id: identity.projectID, name: "Assignment lifecycle")
        func command(_ op: String, _ fields: [String: Any], trusted: Bool = false) -> ProjectBoardStore.Reply {
            var body = fields; body["operation"] = op; body["requestId"] = UUID().uuidString
            body["expectedRevision"] = store.readHeader().revision
            return store.command(body, actor: identity.actor, trusted: trusted, workflowOrigin: true)
        }
        let made = command("create", ["projectId": identity.projectID, "type": "feature",
            "title": variant, "owner": "previous-owner"])
        guard let id = made.body["itemId"] as? String else {
            check("F1 \(variant) fixture creates item", false); continue
        }
        func item() -> [String: Any] {
            (store.snapshot(item: id)["board"] as? [String: Any])?["item"] as? [String: Any] ?? [:]
        }
        let proposal: [String: Any] = ["itemId": id, "projectId": identity.projectID,
            "sessionId": identity.conversationID, "provider": identity.provider, "note": "bounded work"]
        if variant != "propose" { _ = command("assign_session", proposal) }
        expect("F1 \(variant) installs qualifying evidence", command("record_evidence", [
            "itemId": id, "kind": "verification", "status": "passed", "sourceId": "retained-proof",
            "subject": String(repeating: "a", count: 40), "summary": "real fixture proof"], trusted: true).status, 200)
        expect("F1 \(variant) proof can promote before fixture reset", item()["state"] as? String, "verified")
        do {
            var saved = try JSONSerialization.jsonObject(with: Data(contentsOf: file)) as! [String: Any]
            var rows = saved["items"] as! [[String: Any]]
            rows[0]["state"] = "execution"; saved["items"] = rows
            try JSONSerialization.data(withJSONObject: saved, options: [.sortedKeys]).write(to: file, options: .atomic)
            store = ProjectBoardStore(url: file)
        } catch { check("F1 \(variant) reloads promotable fixture", false); continue }
        let before = item(), assignment = (before["handoff"] as? [String: Any])?["id"] as? String ?? "missing"
        let reply: ProjectBoardStore.Reply
        if variant == "propose" { reply = command("assign_session", proposal) }
        else if variant == "cancelled" { reply = command("cancel_session_assignment", [
            "itemId": id, "assignmentId": assignment, "note": "withdraw"])
        } else { reply = command("decide_session_assignment", ["itemId": id,
            "projectId": identity.projectID, "assignmentId": assignment, "decision": variant, "note": "receiver decision"]) }
        let after = item()
        expect("F1 \(variant) command succeeds", reply.status, 200)
        expect("F1 \(variant) must not reconcile promotable lifecycle", after["state"] as? String, "execution")
        expect("F1 \(variant) preserves scope", after["scopeRevision"] as? Int, before["scopeRevision"] as? Int)
        check("F1 \(variant) preserves all evidence pointers",
              (after["currentEvidence"] as? NSDictionary) == (before["currentEvidence"] as? NSDictionary))
        let history = after["history"] as? [[String: Any]] ?? []
        expect("F1 \(variant) appends only assignment bookkeeping", history.count,
               (before["history"] as? [[String: Any]] ?? []).count + 1)
        expect("F1 \(variant) history is not lifecycle promotion", history.last?["kind"] as? String,
               "session_assignment_" + (variant == "propose" ? "proposed" : variant))
        expect("F1 \(variant) changes only accepted ownership", after["owner"] as? String,
               variant == "accepted" ? identity.conversationID : "previous-owner")
        expect("F1 \(variant) ordinary operation positive control succeeds", command("link", [
            "itemId": id, "kind": "session", "targetId": "observer", "label": "Observer"]).status, 200)
        expect("F1 \(variant) unchanged evidence genuinely promotes", item()["state"] as? String, "verified")
    }
}

private func workflowSessionAssignmentProof() {
    let root = workflowTestDirectory("session-assignment")
    defer { try? FileManager.default.removeItem(at: root) }
    let url = root.appendingPathComponent("board.json")
    var store = ProjectBoardStore(url: url)
    let identity = workflowIdentity()
    _ = store.ensureProject(id: identity.projectID, name: "Assignment")
    var serial = 0
    func send(_ operation: String, _ fields: [String: Any], actor: String = "user",
              internalOrigin: Bool = false) -> ProjectBoardStore.Reply {
        serial += 1
        var body = fields
        body["operation"] = operation; body["requestId"] = "assignment-\(serial)"
        body["expectedRevision"] = store.readHeader().revision
        return store.command(body, actor: actor, workflowOrigin: internalOrigin)
    }
    let made = send("create", ["projectId": identity.projectID, "title": "Explicit work",
        "type": "feature", "owner": "original-owner"])
    let itemID = made.body["itemId"] as? String ?? "missing"
    func item() -> [String: Any] {
        (store.snapshot(item: itemID)["board"] as? [String: Any])?["item"] as? [String: Any] ?? [:]
    }
    func proposal() -> String {
        expect("assignment proposal is durable", send("assign_session", ["itemId": itemID,
            "projectId": identity.projectID, "sessionId": identity.conversationID,
            "provider": identity.provider, "note": "Please take this bounded work"]).status, 200)
        return (item()["handoff"] as? [String: Any])?["id"] as? String ?? "missing"
    }
    let before = item(), first = proposal()
    expect("proposal does not silently transfer responsibility", item()["owner"] as? String, "original-owner")
    expect("proposal does not start execution", item()["state"] as? String, before["state"] as? String)
    expect("proposal preserves evidence scope", item()["scopeRevision"] as? Int, before["scopeRevision"] as? Int)
    store = ProjectBoardStore(url: url)
    expect("pending assignment survives reload", (item()["handoff"] as? [String: Any])?["id"] as? String, first)
    expect("pending assignment names provider", (item()["handoff"] as? [String: Any])?["provider"] as? String, identity.provider)
    expect("only one transfer may be pending", send("assign_session", ["itemId": itemID,
        "projectId": identity.projectID, "sessionId": identity.conversationID,
        "provider": identity.provider, "note": "duplicate"]).status, 409)
    func decision(_ id: String, _ value: String = "accepted", project: String? = nil) -> [String: Any] {
        ["itemId": itemID, "projectId": project ?? identity.projectID,
         "assignmentId": id, "decision": value, "note": "receiver decision"]
    }
    expect("public command cannot forge receiving process", send("decide_session_assignment",
        decision(first), actor: identity.actor).status, 403)
    expect("another process cannot accept", send("decide_session_assignment", decision(first),
        actor: "workflow:codex:ffffffff-ffff-4fff-8fff-ffffffffffff", internalOrigin: true).status, 403)
    expect("wrong provider cannot accept same conversation", send("decide_session_assignment", decision(first),
        actor: "workflow:claude:\(identity.conversationID)", internalOrigin: true).status, 403)
    expect("wrong project cannot accept", send("decide_session_assignment", decision(first, project: "foreign"),
        actor: identity.actor, internalOrigin: true).status, 409)
    expect("superseded proposal cannot accept current pending work", send("decide_session_assignment", decision("old"),
        actor: identity.actor, internalOrigin: true).status, 409)
    expect("legacy accept cannot bypass Session identity", send("accept_handoff", ["itemId": itemID,
        "note": "pretend"], actor: identity.conversationID).status, 403)
    expect("receiver can decline without taking ownership", send("decide_session_assignment", decision(first, "declined"),
        actor: identity.actor, internalOrigin: true).status, 200)
    expect("decline preserves owner", item()["owner"] as? String, "original-owner")
    expect("decline remains a typed durable receipt", (item()["sessionAssignment"] as? [String: Any])?["status"] as? String, "declined")
    let second = proposal()
    expect("requester can cancel exact pending assignment", send("cancel_session_assignment", ["itemId": itemID,
        "assignmentId": second, "note": "withdrawn"]).status, 200)
    expect("cancel cannot be followed by stale acceptance", send("decide_session_assignment", decision(second),
        actor: identity.actor, internalOrigin: true).status, 409)
    let third = proposal()
    _ = send("checklist", ["itemId": itemID, "title": "new required scope", "required": true])
    expect("changed acceptance scope refuses takeover", send("decide_session_assignment", decision(third),
        actor: identity.actor, internalOrigin: true).status, 409)
    expect("stale scope can still be withdrawn", send("cancel_session_assignment", ["itemId": itemID,
        "assignmentId": third, "note": "re-propose current scope"]).status, 200)
    let fourth = proposal()
    _ = send("update", ["itemId": itemID, "owner": "changed-owner"])
    expect("changed owner refuses stale takeover", send("decide_session_assignment", decision(fourth),
        actor: identity.actor, internalOrigin: true).status, 409)
    _ = send("cancel_session_assignment", ["itemId": itemID, "assignmentId": fourth, "note": "withdraw"])
    let current = proposal()
    let workflow = ProjectBoardWorkflow(url: root.appendingPathComponent("workflow.json"),
        boardHeader: { let h = store.readHeader(); return (h.enabled, h.revision, h.available) },
        boardCommand: { store.command($0, actor: $1, workflowOrigin: true) }, autoStart: false)
    guard case .managed(let run) = workflow.prepareIngress(requestID: "assignment-send", fingerprint: "send",
        text: "accept the assignment", imageCount: 0, identity: identity) else {
        check("assignment workflow fixture records ingress", false); return
    }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    _ = workflow.record(["operation": "begin", "run_id": run.runID, "classification": "existing_item",
        "item_id": itemID, "phase": "planning"], requestID: "assignment-begin", fingerprint: "begin", identity: identity)
    workflow.drainForTesting()
    expect("begin does not count as accepting assignment", item()["owner"] as? String, "changed-owner")
    let body: [String: Any] = ["operation": "assignment_decision", "run_id": run.runID,
        "assignment_id": current, "decision": "accepted", "note": "I accept this scope"]
    expect("receiver helper journals explicit decision", workflow.record(body,
        requestID: "assignment-accept", fingerprint: "accepted", identity: identity).status, 202)
    workflow.drainForTesting()
    expect("only explicit receiver decision transfers owner", item()["owner"] as? String, identity.conversationID)
    expect("accepted assignment is not execution or verification", item()["state"] as? String, "backlog")
    expect("accepted proposal is recorded by exact id", (item()["sessionAssignment"] as? [String: Any])?["id"] as? String, current)
    expect("accepted receipt states accepted", (item()["sessionAssignment"] as? [String: Any])?["status"] as? String, "accepted")
    check("accepted transfer clears pending handoff", item()["handoff"] is NSNull)
    let count = (item()["history"] as? [[String: Any]])?.count
    _ = workflow.record(body, requestID: "assignment-accept", fingerprint: "accepted", identity: identity)
    workflow.drainForTesting()
    expect("duplicate acceptance cannot produce a second transfer", (item()["history"] as? [[String: Any]])?.count, count)
    store = ProjectBoardStore(url: url)
    expect("accepted owner survives reload", item()["owner"] as? String, identity.conversationID)
}

private func workflowRootLandingProof() {
    let repository = makeLandingRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    let root = workflowTestDirectory("root-landing")
    defer { try? FileManager.default.removeItem(at: root) }
    var clock = 1000.0
    let store = ProjectBoardStore(url: root.appendingPathComponent("board.json"),
                                 now: { Date(timeIntervalSince1970: clock) })
    let path = repository.url.resolvingSymlinksInPath().path
    let identity = ProjectBoardWorkflow.Identity(terminalID: "%root-proof", provider: "codex",
        conversationID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
        projectID: ProjectBoardIntegration.projectID(path), projectPath: path,
        processGeneration: "100:200.0")
    _ = store.ensureProject(id: identity.projectID, name: "Root projection")
    let made = store.command(["operation": "create", "requestId": "root-item",
        "expectedRevision": store.readHeader().revision, "projectId": identity.projectID,
        "title": "Root work", "type": "feature", "owner": identity.conversationID], actor: "root")
    let item = made.body["itemId"] as! String
    let journal = root.appendingPathComponent("workflow.json")
    var commands: [[String: Any]] = []
    var loseFirstReply = true
    func makeWorkflow(_ journalURL: URL? = nil, eventLimit: Int = 64,
                      targetStore: ProjectBoardStore? = nil) -> ProjectBoardWorkflow {
        var limits = ProjectBoardWorkflow.Limits(); limits.eventsPerRun = eventLimit
        let subject = targetStore ?? store
        return ProjectBoardWorkflow(url: journalURL ?? journal, now: { Date(timeIntervalSince1970: clock) },
            boardHeader: { let h = subject.readHeader(); return (h.enabled, h.revision, h.available) },
            boardCommand: { subject.command($0, actor: $1, workflowOrigin: true) },
            rootLandingCommand: { body, actor in
                commands.append(body)
                let reply = subject.command(body, actor: actor, rootLandingOrigin: true)
                if loseFirstReply { loseFirstReply = false
                    return .init(status: 503, body: ["error": ["code": "lost_after_commit"]]) }
                return reply
            }, limits: limits, autoStart: false)
    }
    var workflow = makeWorkflow()
    guard case .managed(let run) = workflow.prepareIngress(requestID: "root-send", fingerprint: "send",
        text: "implement", imageCount: 0, identity: identity) else {
        check("root projection fixture records ingress", false); return
    }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    expect("root fixture begin is admitted", workflow.record(["operation": "begin", "run_id": run.runID,
        "classification": "existing_item", "item_id": item, "phase": "output"],
        requestID: "root-begin", fingerprint: "begin", identity: identity).status, 202)
    workflow.drainForTesting()
    clock += 1
    expect("root fixture delivery is admitted", workflow.record(["operation": "deliver", "run_id": run.runID,
        "disposition": "delivered", "summary": "code ready", "next_action": "record Git landing"],
        requestID: "root-deliver", fingerprint: "deliver", identity: identity).status, 202)
    workflow.drainForTesting()
    _ = store.command(["operation": "record_evidence", "requestId": "root-verification",
        "expectedRevision": store.readHeader().revision, "itemId": item,
        "kind": "verification", "status": "passed", "sourceId": "exact-proof",
        "subject": repository.commit, "summary": "exact tree checked"], actor: "root", trusted: true)
    clock += 1
    let landing = Orchestrator.SessionLanding(repositoryCommonDir:
        OrchestratorDraft.gitCommonDirectory(at: path)!, verificationOrigin: "local_target_branch",
        target: "main", verifiedCommit: repository.commit, verifiedTargetCommit: repository.commit,
        landedAt: Date(timeIntervalSince1970: clock))
    let session = Orchestrator.SessionWorkIdentity(terminalID: identity.terminalID, assistant: .codex,
        tty: "ttys-proof", pid: 100, processStart: Date(timeIntervalSince1970: 200),
        conversationID: identity.conversationID)
    let delivery = Orchestrator.SessionDelivery(identity: session, summary: "landed",
        reportedAt: landing.landedAt, settled: false, landing: landing)
    let brokerURL = Orchestrator.storeURL
    let oldBroker = try? Data(contentsOf: brokerURL)
    try? FileManager.default.removeItem(at: brokerURL)
    Orchestrator.forget()
    ProjectBoardWorkflow.configureSharedForTesting(workflow)
    defer {
        ProjectBoardIntegration.drainObservationsForTesting()
        ProjectBoardWorkflow.configureSharedForTesting(nil)
        if let oldBroker { try? oldBroker.write(to: brokerURL) }
        else { try? FileManager.default.removeItem(at: brokerURL) }
        Orchestrator.forget()
    }
    let observation = Orchestrator.SessionLandingObservation(identity: session,
        terminalState: .working("root proof"), repositoryDirectory: path,
        publicationGeneration: 1, publicationEpoch: "root-proof")
    let brokerReply = Orchestrator.reportSessionLanding(
        observation: observation, summary: "landed", target: "main", commit: repository.commit,
        reobserve: { observation }, now: landing.landedAt)
    if case .ok(let response) = brokerReply {
        check("actual broker root landing is accepted", true)
        expect("landing response names durable Board projection before acknowledgment",
               (response["boardProjection"] as? [String: Any])?["code"] as? String,
               "root_landing_recorded")
    }
    else { check("actual broker root landing is accepted", false) }
    ProjectBoardIntegration.drainObservationsForTesting()
    expect("actual broker callback enters a durable workflow outbox",
        (workflow.snapshot(runID: run.runID)?["outbox"] as? [String: Any])?["pending"] as? Int, 1)
    expect("root landing does not synchronously call the Board", commands.count, 0)
    let pendingJournal = try! Data(contentsOf: journal)
    let pendingBoard = try! Data(contentsOf: root.appendingPathComponent("board.json"))
    workflow = makeWorkflow() // pending survives restart before it reaches the Board
    workflow.drainForTesting()
    let board = store.readSeed(rebuild: true).seed.envelope(item: item)["board"] as! [String: Any]
    let row = board["item"] as! [String: Any]
    let evidence = row["landings"] as? [[String: Any]] ?? []
    expect("one root landing survives a committed-write lost reply", evidence.count, 1)
    check("root landing is not a fabricated Task", (row["links"] as? [[String: Any]] ?? []).allSatisfy { $0["kind"] as? String != "task" })
    check("root landing retains exact verification subject", (row["currentEvidence"] as? [String: Any])?["landingId"] is String)
    check("lost reply reuses the same prepared root request", commands.count >= 2 && commands[0]["requestId"] as? String == commands[1]["requestId"] as? String)
    let revision = store.readHeader().revision
    expect("replayed root receipt is a no-op", workflow.recordRootLanding(delivery).code, "root_landing_already_recorded")
    workflow.drainForTesting()
    expect("replayed root does not change Board revision", store.readHeader().revision, revision)
    if let body = commands.first {
        expect("public trusted callers cannot replay broker root authority", store.command(body, actor: identity.actor, trusted: true).status, 403)
        expect("ordinary workflow origin cannot forge root authority", store.command(body, actor: identity.actor, workflowOrigin: true).status, 403)
    }
    var wrong = session; wrong.pid = 101
    let wrongDelivery = Orchestrator.SessionDelivery(identity: wrong, summary: "wrong process",
        reportedAt: landing.landedAt, settled: false, landing: landing)
    expect("same conversation on another process cannot bind", workflow.recordRootLanding(wrongDelivery).code, "root_landing_binding_unresolved")
    var unmanagedSession = session
    unmanagedSession.conversationID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let unmanagedDelivery = Orchestrator.SessionDelivery(identity: unmanagedSession, summary: "native root",
        reportedAt: landing.landedAt, settled: false, landing: landing)
    expect("unmanaged native root is not a failed Board write", workflow.recordRootLanding(unmanagedDelivery).code, "root_landing_unmanaged")
    let unproved = Orchestrator.SessionDelivery(identity: session, summary: "merely delivered",
        reportedAt: landing.landedAt, settled: false)
    expect("plain delivery never becomes Git landing", workflow.recordRootLanding(unproved).code, "root_landing_unverified")
    let secondLanding = Orchestrator.SessionLanding(repositoryCommonDir: landing.repositoryCommonDir,
        verificationOrigin: landing.verificationOrigin, target: "main", verifiedCommit: repository.commit,
        verifiedTargetCommit: repository.commit, landedAt: Date(timeIntervalSince1970: clock + 1))
    let secondDelivery = Orchestrator.SessionDelivery(identity: session, summary: "retry boundary",
        reportedAt: secondLanding.landedAt, settled: false, landing: secondLanding)
    let failingJournal = root.appendingPathComponent("failed-workflow.json")
    try! FileManager.default.copyItem(at: journal, to: failingJournal)
    let failing = ProjectBoardWorkflow(url: failingJournal,
        boardHeader: { let h = store.readHeader(); return (h.enabled, h.revision, h.available) },
        journalSynchronize: { _ in throw WorkflowSyncFixtureError.refused }, autoStart: false)
    expect("root projection cannot acknowledge failed journal synchronization",
           failing.recordRootLanding(secondDelivery).code, "workflow_persistence_failed")
    // The failed fsync may have atomically replaced bytes, so it uses a separate journal.
    let persisted = try! JSONSerialization.jsonObject(with: Data(contentsOf: journal)) as! [String: Any]
    let persistedRun = (persisted["runs"] as! [[String: Any]]).first { $0["id"] as? String == run.runID }!
    var small = ProjectBoardWorkflow.Limits()
    small.eventsPerRun = (persistedRun["events"] as! [[String: Any]]).count
    let bounded = ProjectBoardWorkflow(url: journal,
        boardHeader: { let h = store.readHeader(); return (h.enabled, h.revision, h.available) },
        limits: small, autoStart: false)
    let thirdLanding = Orchestrator.SessionLanding(repositoryCommonDir: landing.repositoryCommonDir,
        verificationOrigin: landing.verificationOrigin, target: "main", verifiedCommit: repository.commit,
        verifiedTargetCommit: repository.commit, landedAt: Date(timeIntervalSince1970: clock + 2))
    let thirdDelivery = Orchestrator.SessionDelivery(identity: session, summary: "capacity",
        reportedAt: thirdLanding.landedAt, settled: false, landing: thirdLanding)
    expect("root projection capacity is a typed refusal", bounded.recordRootLanding(thirdDelivery).status, 429)
    _ = store.command(["operation": "set_enabled", "enabled": false,
        "requestId": "root-disable", "expectedRevision": store.readHeader().revision], actor: "root")
    expect("Board OFF does not record root evidence", workflow.recordRootLanding(thirdDelivery).code, "board_disabled")
    _ = store.command(["operation": "set_enabled", "enabled": true,
        "requestId": "root-enable", "expectedRevision": store.readHeader().revision], actor: "root")
    let retainedURL = root.appendingPathComponent("retained-workflow.json")
    let retainedBoardURL = root.appendingPathComponent("retained-board.json")
    try! pendingJournal.write(to: retainedURL)
    try! pendingBoard.write(to: retainedBoardURL)
    let retainedStore = ProjectBoardStore(url: retainedBoardURL,
        now: { Date(timeIntervalSince1970: clock) })
    var retaining = makeWorkflow(retainedURL, eventLimit: 3, targetStore: retainedStore)
    for index in 0..<8 {
        expect("bounded progress keeps pending landing event \(index)", retaining.record([
            "operation": "progress", "run_id": run.runID, "summary": "progress \(index)"],
            requestID: "retained-\(index)", fingerprint: "retained-\(index)", identity: identity).status, 202)
    }
    retaining = makeWorkflow(retainedURL, eventLimit: 3, targetStore: retainedStore)
    retaining.drainForTesting()
    let retainedOutbox = retaining.snapshot(runID: run.runID)?["outbox"] as? [String: Any]
    expect("pending root event survives retention and reload", retainedOutbox?["pending"] as? Int, 0)
    expect("retention never loses an acknowledged landing subject", retainedOutbox?["failed"] as? Int, 0)
    expect("retention has no hidden failure code", retainedOutbox?["failures"] as? [String], [])
    let retainedRow = (retainedStore.readSeed(rebuild: true).seed.envelope(item: item)["board"] as? [String: Any])?["item"] as? [String: Any]
    expect("retention retries keep exactly one root landing", (retainedRow?["landings"] as? [[String: Any]])?.count, 1)
    clock += 10
    guard case .managed(let newer) = workflow.prepareIngress(requestID: "question", fingerprint: "question",
        text: "a question", imageCount: 0, identity: identity) else { return }
    _ = workflow.markDelivery(runID: newer.runID, identity: identity, delivered: true)
    let later = Orchestrator.SessionLanding(repositoryCommonDir: landing.repositoryCommonDir,
        verificationOrigin: landing.verificationOrigin, target: "main", verifiedCommit: repository.commit,
        verifiedTargetCommit: repository.commit, landedAt: Date(timeIntervalSince1970: clock + 1))
    let latest = Orchestrator.SessionDelivery(identity: session, summary: "new turn",
        reportedAt: later.landedAt, settled: false, landing: later)
    expect("unbound latest ingress never falls back to an older item", workflow.recordRootLanding(latest).code, "root_landing_binding_unresolved")
}

private func workflowTestDirectory(_ name: String) -> URL {
    let url = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
        .appendingPathComponent("clawdline-workflow-\(name)-\(UUID().uuidString)",
                                isDirectory: true)
    try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
}

private func workflowIdentity(_ suffix: String = "1") -> ProjectBoardWorkflow.Identity {
    ProjectBoardWorkflow.Identity(
        terminalID: "%workflow-\(suffix)", provider: "codex",
        conversationID: "11111111-1111-4111-8111-11111111111\(suffix)",
        projectID: "project-\(suffix)", projectPath: "/Users/me/code/project-\(suffix)",
        processGeneration: "pid-100-start-200")
}

private func workflowProgramBindingAndDocumentProof() {
    let root = workflowTestDirectory("program-binding")
    defer { try? FileManager.default.removeItem(at: root) }
    let boardURL = root.appendingPathComponent("board.json")
    let journalURL = root.appendingPathComponent("workflow.json")
    let replayJournalURL = root.appendingPathComponent("workflow-after-lost-reply.json")
    var store = ProjectBoardStore(url: boardURL)
    let identity = workflowIdentity("7")
    _ = store.ensureProject(id: identity.projectID, name: "Program binding")
    var serial = 0
    func storeCommand(_ operation: String, _ fields: [String: Any],
                      actor: String = "program-root") -> ProjectBoardStore.Reply {
        serial += 1
        var body = fields; body["operation"] = operation
        body["requestId"] = "program-store-\(serial)"
        body["expectedRevision"] = store.readHeader().revision
        return store.command(body, actor: actor, workflowOrigin: true)
    }
    let made = storeCommand("create", ["projectId": identity.projectID, "type": "epic",
        "title": "Canonical program", "owner": "program-root"])
    let program = made.body["itemId"] as? String ?? "missing"
    let programKey = ((store.snapshot(item: program)["board"] as? [String: Any])?["item"]
        as? [String: Any])?["key"] as? String ?? ""
    let planURL = "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=workflow-program.md"
    expect("workflow binding fixture imports its exact Program Plan", storeCommand("plan_structure", [
        "schemaVersion": 1, "itemId": program, "programKey": programKey,
        "planId": "workflow-plan", "planVersion": 1, "graphId": "workflow-graph",
        "destination": "Bind exact managed work",
        "document": ["documentId": "workflow-plan-document", "version": 1,
                     "title": "Workflow plan", "url": planURL],
        "nodes": [["key": "delivery", "graphNodeId": "delivery", "title": "Delivery node",
            "type": "task", "summary": "Implement the node.", "owner": "implementer",
            "dependsOn": [], "gateKeys": [], "capabilityKeys": [],
            "claims": ["Sources/ProjectBoardWorkflow.swift"]]],
        "gates": [], "capabilities": [],
    ]).status, 200)
    let programPlan = (((store.snapshot(item: program)["board"] as? [String: Any])?["item"]
        as? [String: Any])?["programPlan"] as? [String: Any])
    let effectiveItem = (programPlan?["nodes"] as? [[String: Any]])?.first?["itemId"]
        as? String ?? "missing"
    var captureCommittedBinding = true
    var originalStoreReceipt: [String: Any]?
    func makeWorkflow(_ url: URL = journalURL) -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(url: url,
            boardHeader: { let h = store.readHeader(); return (h.enabled, h.revision, h.available) },
            boardCommand: { body, actor in
                let reply = store.command(body, actor: actor, workflowOrigin: true)
                if captureCommittedBinding,
                   body["operation"] as? String == "program_binding", reply.status == 200 {
                    captureCommittedBinding = false
                    originalStoreReceipt = reply.body["programBindingReceipt"] as? [String: Any]
                    try? FileManager.default.removeItem(at: replayJournalURL)
                    try? FileManager.default.copyItem(at: journalURL, to: replayJournalURL)
                }
                return reply
            }, autoStart: false)
    }
    var workflow = makeWorkflow()
    guard case .managed(let prepared) = workflow.prepareIngress(
        requestID: "program-turn", fingerprint: "program-turn-body", text: "implement node",
        imageCount: 0, identity: identity) else {
        check("program workflow fixture creates a managed run", false); return
    }
    _ = workflow.markDelivery(runID: prepared.runID, identity: identity, delivered: true)
    let binding: [String: Any] = [
        "program_item_id": program, "program_key": programKey,
        "plan_id": "workflow-plan", "plan_version": 1,
        "graph_id": "workflow-graph", "node_id": "delivery",
    ]
    let admitted = workflow.record(["operation": "begin", "run_id": prepared.runID,
        "classification": "existing_item", "item_id": program, "phase": "output",
        "program_binding": binding], requestID: "program-begin", fingerprint: "program-begin-body",
        identity: identity)
    expect("Program binding initially reports durable admission, not settlement", admitted.status, 202)
    expect("an undrained Program binding remains pending",
           (workflow.snapshot(runID: prepared.runID)?["binding"] as? [String: Any])?["status"]
            as? String, "pending")
    let pendingBinding = workflow.snapshot(runID: prepared.runID)?["binding"] as? [String: Any]
    expect("a pending binding preserves the requested classification",
           pendingBinding?["requested_classification"] as? String, "existing_item")
    expect("a pending binding preserves the requested Program item",
           pendingBinding?["requested_item_id"] as? String, program)
    workflow.drainForTesting()
    let settled = workflow.snapshot(runID: prepared.runID)?["binding"] as? [String: Any]
    expect("the settled receipt names the exact effective node item",
           settled?["effective_item_id"] as? String, effectiveItem)
    expect("the settled receipt retains the requested canonical Program",
           settled?["program_item_id"] as? String, program)
    expect("the binding receipt is process-bound",
           (settled?["process"] as? [String: Any])?["generation"] as? String,
           identity.processGeneration)
    expect("the binding receipt explicitly resolves to a reused imported node",
           settled?["resolution"] as? String, "reused_imported_program_node")
    expect("a direct first settlement is not mislabeled as replay",
           settled?["settlement_replay"] as? Bool, false)
    expect("the workflow preserves the Store's original receipt identity",
           settled?["receipt_id"] as? String,
           originalStoreReceipt?["receiptId"] as? String)
    expect("the binding receipt cannot claim broker execution authority",
           settled?["authority"] as? String, "advisory_only")

    var replayWorkflow = makeWorkflow(replayJournalURL)
    replayWorkflow.drainForTesting()
    let replaySettled = replayWorkflow.snapshot(runID: prepared.runID)?["binding"]
        as? [String: Any]
    expect("a restart replay preserves the original binding receipt identity",
           replaySettled?["receipt_id"] as? String, settled?["receipt_id"] as? String)
    expect("a restart replay durably records Store settlement provenance",
           replaySettled?["settlement_replay"] as? Bool, true)
    replayWorkflow = makeWorkflow(replayJournalURL)
    expect("settlement replay provenance survives another workflow restart",
           (replayWorkflow.snapshot(runID: prepared.runID)?["binding"]
            as? [String: Any])?["settlement_replay"] as? Bool, true)

    let v1DocumentReference = programPlan?["documentReferenceId"] as? String ?? "missing"
    expect("a bound v1 Program accepts its exact v2 successor", storeCommand("plan_structure", [
        "schemaVersion": 1, "itemId": program, "programKey": programKey,
        "planId": "workflow-plan", "planVersion": 2, "predecessorVersion": 1,
        "graphId": "workflow-graph", "destination": "Bind exact managed work",
        "document": ["documentId": "workflow-plan-document", "version": 2,
                     "title": "Workflow plan v2",
                     "url": planURL.replacingOccurrences(of: ".md", with: "-v2.md"),
                     "supersedesId": v1DocumentReference],
        "nodes": [["key": "delivery", "graphNodeId": "delivery", "title": "Delivery node",
            "type": "task", "summary": "Implement the node.", "owner": "implementer",
            "dependsOn": [], "gateKeys": [], "capabilityKeys": [],
            "claims": ["Sources/ProjectBoardWorkflow.swift"]]],
        "gates": [], "capabilities": [],
    ]).status, 200)
    store = ProjectBoardStore(url: boardURL)
    let restartedPlan = (((store.snapshot(item: program)["board"] as? [String: Any])?["item"]
        as? [String: Any])?["programPlan"] as? [String: Any])
    expect("restart keeps the successor as the current Program Plan",
           restartedPlan?["planVersion"] as? Int, 2)
    let historicalBinding = (restartedPlan?["bindingReceipts"] as? [[String: Any]])?.first
    expect("restart retains the historical v1 binding under the v2 Plan",
           historicalBinding?["planVersion"] as? Int, 1)
    expect("successor import and Store restart preserve the receipt identity",
           historicalBinding?["receiptId"] as? String, settled?["receipt_id"] as? String)
    workflow = makeWorkflow()
    expect("workflow restart preserves its original settled receipt after Plan evolution",
           (workflow.snapshot(runID: prepared.runID)?["binding"]
            as? [String: Any])?["receipt_id"] as? String, settled?["receipt_id"] as? String)

    let documentBody: [String: Any] = ["operation": "document", "run_id": prepared.runID,
        "version": 1, "document_id": "node-notes", "document_version": 1,
        "title": "Delivery notes", "url": planURL.replacingOccurrences(of: "program", with: "node"),
        "purpose": "reference"]
    let document = workflow.record(documentBody, requestID: "document-v1",
        fingerprint: "document-v1-body", identity: identity)
    expect("a version-negotiated document is only journal-admitted initially", document.status, 202)
    workflow = makeWorkflow()
    expect("a pending document intent survives workflow restart",
           (workflow.snapshot(runID: prepared.runID)?["outbox"] as? [String: Any])?["pending"]
            as? Int, 1)
    workflow.drainForTesting()
    let node = (store.snapshot(item: effectiveItem)["board"] as? [String: Any])?["item"]
        as? [String: Any]
    expect("the durable outbox settles the typed document on the effective item",
           (node?["documentReferences"] as? [[String: Any]])?.first?["documentId"] as? String,
           "node-notes")
    let documentReplay = workflow.record(documentBody, requestID: "document-v1",
        fingerprint: "document-v1-body", identity: identity)
    expect("document replay returns the original admission receipt", documentReplay.status, 202)
    expect("document replay preserves the original typed code", documentReplay.code, document.code)
    var newerProtocol = documentBody; newerProtocol["version"] = 2
    expect("an unsupported document protocol version is refused",
           workflow.record(newerProtocol, requestID: "document-v2", fingerprint: "document-v2-body",
                           identity: identity).code, "workflow_document_version_unsupported")
    var escalated = documentBody; escalated["authority"] = "trusted"
    expect("document metadata cannot escalate its authority",
           workflow.record(escalated, requestID: "document-authority",
                           fingerprint: "document-authority-body", identity: identity).code,
           "workflow_command_fields")
    var forgedProcess = documentBody; forgedProcess["process_generation"] = "forged"
    expect("document metadata cannot replace process identity",
           workflow.record(forgedProcess, requestID: "document-process",
                           fingerprint: "document-process-body", identity: identity).code,
           "workflow_command_fields")

    guard case .managed(let second) = workflow.prepareIngress(
        requestID: "program-turn-two", fingerprint: "program-turn-two-body", text: "continue",
        imageCount: 0, identity: identity) else {
        check("second Program workflow run is managed", false); return
    }
    _ = workflow.markDelivery(runID: second.runID, identity: identity, delivered: true)
    var successorBinding = binding
    successorBinding["plan_version"] = 2
    expect("the second exact binding is admitted", workflow.record([
        "operation": "begin", "run_id": second.runID, "classification": "existing_item",
        "item_id": program, "phase": "correction", "program_binding": successorBinding,
    ], requestID: "program-begin-two", fingerprint: "program-begin-two-body",
       identity: identity).status, 202)
    workflow.drainForTesting()
    let repeatedNode = (store.snapshot(item: effectiveItem)["board"] as? [String: Any])?["item"]
        as? [String: Any]
    expect("repeated begin boundaries render one canonical Session relation",
           (repeatedNode?["links"] as? [[String: Any]])?.filter {
               $0["kind"] as? String == "session" && $0["targetId"] as? String == identity.conversationID
           }.count, 1)
    expect("repeated begin boundaries retain distinct activity spans",
           (repeatedNode?["spans"] as? [[String: Any]])?.filter {
               $0["sessionId"] as? String == identity.conversationID
           }.count, 2)

    guard case .managed(let disabledRun) = workflow.prepareIngress(
        requestID: "program-turn-disabled", fingerprint: "program-turn-disabled-body",
        text: "wait for Board", imageCount: 0, identity: identity) else {
        check("disabled Board fixture creates a managed run before the mode change", false); return
    }
    _ = workflow.markDelivery(runID: disabledRun.runID, identity: identity, delivered: true)
    expect("a Program binding can be admitted before Board is disabled", workflow.record([
        "operation": "begin", "run_id": disabledRun.runID,
        "classification": "existing_item", "item_id": program, "phase": "correction",
        "program_binding": successorBinding,
    ], requestID: "program-begin-disabled", fingerprint: "program-begin-disabled-body",
       identity: identity).status, 202)
    expect("the Board disable command succeeds after workflow admission", storeCommand(
        "set_enabled", ["enabled": false]).status, 200)
    workflow.drainForTesting()
    let disabledOutbox = workflow.snapshot(runID: disabledRun.runID)?["outbox"] as? [String: Any]
    expect("post-admission Board disable is classified separately",
           disabledOutbox?["failures"] as? [String], ["board_disabled"])
}

private func workflowJSON(_ response: RemoteServer.Response) -> [String: Any] {
    (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any] ?? [:]
}

/// F01 integration: exercise the actual closed Store vocabulary, not a command-accepting fake.
private func workflowSupplementStoreProof() {
    let root = workflowTestDirectory("supplement-store")
    defer { try? FileManager.default.removeItem(at: root) }
    let boardURL = root.appendingPathComponent("board.json")
    let store = ProjectBoardStore(url: boardURL)
    let identity = workflowIdentity()
    _ = store.ensureProject(id: identity.projectID, name: "Workflow integration")
    let created = store.command([
        "operation": "create", "requestId": "integration-parent",
        "expectedRevision": store.readHeader().revision,
        "projectId": identity.projectID, "title": "Parent", "type": "epic",
    ], actor: "integration-root")
    guard created.status == 200, let parent = created.body["itemId"] as? String else {
        check("the supplement integration fixture creates a real parent", false); return
    }
    var failures: [String] = []
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: {
            let h = store.readHeader(); return (h.enabled, h.revision, h.available)
        },
        boardCommand: { body, actor in
            let reply = store.command(body, actor: actor, workflowOrigin: true)
            if reply.status >= 400 {
                failures.append("\(body["operation"] ?? "?"):\(reply.status)")
            }
            return reply
        }, autoStart: false)
    guard case .managed(let run) = workflow.prepareIngress(
        requestID: "integration-send", fingerprint: "integration-body", text: "follow up",
        imageCount: 0, identity: identity) else {
        check("the supplement integration creates a managed run", false); return
    }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    _ = workflow.record([
        "operation": "begin", "run_id": run.runID, "classification": "existing_item",
        "item_id": parent, "phase": "output",
    ], requestID: "integration-begin", fingerprint: "begin", identity: identity)
    workflow.drainForTesting()
    for actor in ["assistant", "user", "external"] {
        let body: [String: Any] = [
            "operation": "supplement", "run_id": run.runID, "version": 1,
            "kind": "checklist", "title": "Same visible title", "owner": actor,
            "acceptance": "Observable outcome \(actor)", "disposition": "required",
            "actor_kind": actor,
        ]
        expect("\(actor) supplement is accepted before Store projection",
               workflow.record(body, requestID: "supplement-\(actor)", fingerprint: actor,
                               identity: identity).status, 202)
        workflow.drainForTesting()
        _ = workflow.record(body, requestID: "supplement-\(actor)", fingerprint: actor,
                            identity: identity)
        workflow.drainForTesting()
    }
    expect("every supplemental command is accepted by the actual Store", failures, [])
    let reloaded = ProjectBoardStore(url: boardURL)
    let item = (reloaded.snapshot(item: parent)["board"] as? [String: Any])?["item"]
        as? [String: Any] ?? [:]
    let obligations = item["obligations"] as? [[String: Any]] ?? []
    expect("replayed supplements create exactly three persistent obligations", obligations.count, 3)
    expect("the actual Store keeps its canonical actor vocabulary",
           Set(obligations.compactMap { $0["actorKind"] as? String }),
           Set(["agent", "user", "external"]))
    check("persisted acceptance and source survive the Workflow to Store boundary",
          obligations.count == 3 && obligations.allSatisfy {
              ($0["requiredAction"] as? String)?.hasPrefix("Observable outcome") == true
                  && ($0["blockingScope"] as? String)?.contains(run.runID) == true
          })
    expect("checklist retries create no duplicate rows",
           (item["checklist"] as? [[String: Any]])?.count, 3)
    let remaining = item["remainingWork"] as? [String: Any] ?? [:]
    expect("the real read model puts the user action in its own decision list",
           (remaining["userDecisions"] as? [[String: Any]])?.count, 1)
    let checklist = item["checklist"] as? [[String: Any]] ?? []
    let relationIDs = checklist.compactMap { $0["supplementRelationId"] as? String }
    expect("F2 same-title supplements in one run have distinct event identities",
           Set(relationIDs).count, 3)
    check("F2 persisted counterpart relations use the closed system ID format",
          relationIDs.count == 3 && relationIDs.allSatisfy {
              $0.hasPrefix("wfs-") && $0.utf8.count == 68
          })
    expect("F2 checklist and obligation have exactly the same relation identities",
           Set(obligations.compactMap { $0["supplementRelationId"] as? String }), Set(relationIDs))
    let remainingRows = (remaining["work"] as? [[String: Any]] ?? [])
        + (remaining["userDecisions"] as? [[String: Any]] ?? [])
    expect("F2 both remaining projections preserve all six counterpart IDs",
           remainingRows.compactMap { $0["supplementRelationId"] as? String }.count, 6)
    expect("F2 remaining projections preserve the exact same identities",
           Set(remainingRows.compactMap { $0["supplementRelationId"] as? String }), Set(relationIDs))
    let attemptedRelation = relationIDs.first ?? "wfs-" + String(repeating: "a", count: 64)
    for operation in ["checklist", "obligation"] {
        var body: [String: Any] = ["operation": operation, "itemId": parent,
            "requestId": "forged-\(operation)", "expectedRevision": reloaded.readHeader().revision,
            "title": "Same visible title", "supplementRelationId": attemptedRelation]
        if operation == "checklist" { body["required"] = true }
        else { body["owner"] = "user"; body["blocking"] = true }
        let beforeRevision = reloaded.readHeader().revision
        let forged = reloaded.command(body, actor: identity.actor, trusted: true)
        expect("F2 even evidence authority cannot forge a \(operation) workflow relation",
               forged.status, 403)
        expect("F2 \(operation) relation refusal names the missing producer origin",
               (forged.body["error"] as? [String: Any])?["code"] as? String,
               "workflow_origin_required")
        expect("F2 forged \(operation) relation cannot change the durable revision",
               reloaded.readHeader().revision, beforeRevision)
    }
    expect("F2 manual same-title checklist is still allowed without a relation", reloaded.command([
        "operation": "checklist", "itemId": parent, "requestId": "manual-same-title",
        "expectedRevision": reloaded.readHeader().revision, "title": "Same visible title",
        "required": true,
    ], actor: "human").status, 200)
    let manualItem = (reloaded.snapshot(item: parent)["board"] as? [String: Any])?["item"]
        as? [String: Any] ?? [:]
    let manualRows = manualItem["checklist"] as? [[String: Any]] ?? []
    check("F2 manual same-title row has no inferred supplement identity",
          manualRows.count == 4 && manualRows.last?["supplementRelationId"] is NSNull)

    // Simulate an old persisted journal whose pending commands never carried relation IDs.
    // Retain its prepared keys: a software upgrade must not change the replay body.
    do {
        let journalURL = root.appendingPathComponent("workflow.json")
        var journal = try JSONSerialization.jsonObject(with: Data(contentsOf: journalURL)) as! [String: Any]
        var runs = journal["runs"] as! [[String: Any]]
        var events = runs[0]["events"] as! [[String: Any]]
        for index in events.indices {
            if var supplement = events[index]["supplement"] as? [String: Any] {
                supplement.removeValue(forKey: "relationID")
                events[index]["supplement"] = supplement
            }
        }
        var outbox = journal["outbox"] as! [[String: Any]]
        var preparedKeys = Set<String>()
        // Build genuine legacy pending rows explicitly: current journals compact successful
        // payload rows, so flipping whatever rows remain would test zero replays.
        for index in events.indices {
            let settled = events[index]["settledIntents"] as? [[String: Any]] ?? []
            let replay = settled.filter { ["supplement_checklist", "supplement_obligation"].contains($0["kind"] as? String ?? "") }
            for entry in replay {
                let id = entry["id"] as! String
                outbox.append(["id": id, "version": 1, "runID": runs[0]["id"]!,
                    "eventID": events[index]["id"]!, "kind": entry["kind"]!, "index": entry["index"]!,
                    "status": "pending", "attempts": 1, "preparedRequestID": "legacy-prepared-\(id)",
                    "preparedRevision": reloaded.readHeader().revision, "updatedAt": 1])
            }
            events[index]["settledIntents"] = settled.filter { !["supplement_checklist", "supplement_obligation"].contains($0["kind"] as? String ?? "") }
        }
        runs[0]["events"] = events; journal["runs"] = runs
        for index in outbox.indices where ["supplement_checklist", "supplement_obligation"].contains(outbox[index]["kind"] as? String ?? "") {
            outbox[index]["status"] = "pending"
            if let key = outbox[index]["preparedRequestID"] as? String { preparedKeys.insert(key) }
        }
        journal["outbox"] = outbox
        let legacyURL = root.appendingPathComponent("legacy-workflow.json")
        try JSONSerialization.data(withJSONObject: journal).write(to: legacyURL, options: .atomic)
        var replayBodies: [[String: Any]] = []
        let legacy = ProjectBoardWorkflow(url: legacyURL,
            boardHeader: { (true, reloaded.readHeader().revision, true) },
            boardCommand: { body, _ in
                replayBodies.append(body)
                return ProjectBoardStore.Reply(status: 200, body: ["itemId": parent])
            }, autoStart: false)
        legacy.drainForTesting()
        expect("F2 legacy pending supplements replay all six prepared commands", replayBodies.count, 6)
        check("F2 legacy pending replay does not invent relation fields",
              replayBodies.count == 6 && replayBodies.allSatisfy { $0["supplementRelationId"] == nil })
        expect("F2 legacy replay retains its exact prepared request IDs",
               Set(replayBodies.compactMap { $0["requestId"] as? String }), preparedKeys)
    } catch { check("F2 legacy journal fixture remains decodable", false) }
}

/// A terminal turn finishing is an interval boundary, not Feature verification or landing.
private func workflowActivityBoundaryProof() {
    let root = workflowTestDirectory("activity-boundary")
    defer { try? FileManager.default.removeItem(at: root) }
    let boardURL = root.appendingPathComponent("board.json")
    let journalURL = root.appendingPathComponent("workflow.json")
    let store = ProjectBoardStore(url: boardURL)
    let identity = workflowIdentity()
    _ = store.ensureProject(id: identity.projectID, name: "Activity boundary")
    let created = store.command([
        "operation": "create", "requestId": "boundary-item",
        "expectedRevision": store.readHeader().revision, "projectId": identity.projectID,
        "title": "Still needs acceptance", "type": "feature",
    ], actor: "root")
    guard let itemID = created.body["itemId"] as? String else {
        check("activity boundary fixture creates its item", false); return
    }
    func makeWorkflow() -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(url: journalURL, boardHeader: {
            let h = store.readHeader(); return (h.enabled, h.revision, h.available)
        }, boardCommand: { store.command($0, actor: $1) }, autoStart: false)
    }
    var workflow = makeWorkflow()
    func item() -> [String: Any] {
        (store.snapshot(item: itemID)["board"] as? [String: Any])?["item"]
            as? [String: Any] ?? [:]
    }
    func activeCount() -> Int {
        store.readSeed(rebuild: true).seed.catalog.first?["activeItemCount"] as? Int ?? -1
    }
    func begin(_ key: String) -> String? {
        guard case .managed(let run) = workflow.prepareIngress(
            requestID: key, fingerprint: key, text: key, imageCount: 0, identity: identity)
        else { return nil }
        _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
        _ = workflow.record([
            "operation": "begin", "run_id": run.runID, "classification": "existing_item",
            "item_id": itemID, "phase": "integration",
        ], requestID: key + "-begin", fingerprint: key + "-begin", identity: identity)
        workflow.drainForTesting()
        return run.runID
    }
    guard let old = begin("old"), let current = begin("current") else {
        check("activity boundary creates two distinct runs", false); return
    }
    expect("a live declared run appears in the Project activity count", activeCount(), 1)
    func deliver(_ run: String, _ disposition: String = "delivered") {
        let body: [String: Any] = [
            "operation": "deliver", "run_id": run, "disposition": disposition,
            "summary": "This turn ended; acceptance is separate", "next_action": "review",
        ]
        _ = workflow.record(body, requestID: run + "-finish", fingerprint: run + "-finish",
                            identity: identity)
        workflow.drainForTesting()
    }
    deliver(old)
    expect("an old run's late delivery cannot close a newer run", activeCount(), 1)
    let scopeBefore = item()["scopeRevision"] as? Int
    deliver(current)
    expect("delivery ends its exact span and clears the Project activity count", activeCount(), 0)
    expect("ending a turn does not mutate the Feature scope", item()["scopeRevision"] as? Int,
           scopeBefore)
    check("ending a turn never grants verified integrated or closed lifecycle",
          !["verified", "integrated", "closed"].contains(item()["state"] as? String ?? ""))
    let spansBefore = item()["spans"] as? [[String: Any]] ?? []
    check("all delivered turn intervals retain concrete end boundaries",
          spansBefore.count == 2 && spansBefore.allSatisfy { $0["endedAt"] is Double })
    deliver(current)
    expect("an identical delivery replay neither starts nor adds another span",
           (item()["spans"] as? [[String: Any]])?.count, spansBefore.count)
    workflow = makeWorkflow()
    workflow.drainForTesting()
    expect("restart cannot reactivate a completed interval", activeCount(), 0)
    let reloaded = ProjectBoardStore(url: boardURL)
    let saved = (reloaded.snapshot(item: itemID)["board"] as? [String: Any])?["item"]
        as? [String: Any] ?? [:]
    expect("the interval end is durable in the Board model",
           (saved["progress"] as? [String: Any])?["active"] as? Bool, false)
    for disposition in ["waiting_user", "waiting_external", "interrupted", "cancelled"] {
        guard let run = begin(disposition) else { continue }
        deliver(run, disposition)
        expect("\(disposition) stops activity without completing the Feature", activeCount(), 0)
    }
    guard let handoff = begin("handoff") else { return }
    _ = workflow.record([
        "operation": "handoff", "run_id": handoff,
        "owner": "next-owner", "note": "receiver must accept",
    ], requestID: "handoff-boundary", fingerprint: "handoff-boundary", identity: identity)
    workflow.drainForTesting()
    expect("a proposed handoff ends only the sender's active interval", activeCount(), 0)
    check("handoff keeps transfer pending instead of accepting on behalf of the receiver",
          item()["handoff"] is [String: Any])
}

/// Sealed review F1/F2: real Store refusals and a state that *would* promote if reconciled.
private func workflowEndSpanAuthorityProof() {
    for variant in ["foreign_actor", "wrong_session", "broker", "legacy", "promotable"] {
        let root = workflowTestDirectory("end-span-\(variant)")
        defer { try? FileManager.default.removeItem(at: root) }
        let file = root.appendingPathComponent("board.json")
        var store = ProjectBoardStore(url: file)
        _ = store.ensureProject(id: "project", name: "End span authority")
        func command(_ operation: String, _ fields: [String: Any],
                     actor: String = "declaring-actor", key: String = UUID().uuidString,
                     trusted: Bool = false) -> ProjectBoardStore.Reply {
            var body = fields
            body["operation"] = operation
            body["requestId"] = key
            body["expectedRevision"] = store.readHeader().revision
            return store.command(body, actor: actor, trusted: trusted)
        }
        let created = command("create", ["projectId": "project", "type": "feature",
                                         "title": variant, "owner": "owner"])
        guard let id = created.body["itemId"] as? String else {
            check("\(variant) fixture creates its item", false); continue
        }
        func item() -> [String: Any] {
            (store.snapshot(item: id)["board"] as? [String: Any])?["item"]
                as? [String: Any] ?? [:]
        }
        expect("\(variant) fixture creates one declared interval", command("span", [
            "itemId": id, "sessionId": "declaring-session", "phase": "output",
        ], key: "same-start-request").status, 200)
        if variant == "promotable" {
            expect("F2 fixture has real qualifying verification", command("record_evidence", [
                "itemId": id, "kind": "verification", "status": "passed",
                "summary": "exact fixture proof", "sourceId": "fixture-proof",
                "subject": String(repeating: "a", count: 40),
            ], trusted: true).status, 200)
            expect("F2 evidence is strong enough to promote the fixture",
                   item()["state"] as? String, "verified")
        }
        // Persisted input fixtures isolate each guard: broker keeps the matching declaration ID,
        // legacy keeps actor/session but lacks that ID, and F2 retains all proof with stale state.
        if ["broker", "legacy", "promotable"].contains(variant) {
            do {
                var saved = try JSONSerialization.jsonObject(with: Data(contentsOf: file))
                    as! [String: Any]
                var items = saved["items"] as! [[String: Any]]
                if variant == "promotable" {
                    items[0]["state"] = "execution"
                } else {
                    var spans = items[0]["spans"] as! [[String: Any]]
                    if variant == "broker" { spans[0]["source"] = "broker" }
                    else { spans[0]["id"] = "legacy-unidentifiable-span" }
                    items[0]["spans"] = spans
                }
                saved["items"] = items
                try JSONSerialization.data(withJSONObject: saved, options: [.sortedKeys])
                    .write(to: file, options: .atomic)
                store = ProjectBoardStore(url: file)
            } catch {
                check("\(variant) persists a valid isolated input fixture", false); continue
            }
        }
        let before = item()
        let reply = command("end_span", [
            "itemId": id,
            "sessionId": variant == "wrong_session" ? "other-session" : "declaring-session",
            "startRequestId": "same-start-request", "note": "only this interval may end",
        ], actor: variant == "foreign_actor" ? "foreign-actor" : "declaring-actor")
        let after = item()
        let historyBefore = before["history"] as? [[String: Any]] ?? []
        let historyAfter = after["history"] as? [[String: Any]] ?? []
        if variant == "promotable" {
            expect("F2 exact end is accepted on the promotable fixture", reply.status, 200)
            expect("F2 end_span does not reconcile even when evidence can promote",
                   after["state"] as? String, "execution")
            expect("F2 end_span leaves current scope unchanged",
                   after["scopeRevision"] as? Int, before["scopeRevision"] as? Int)
            expect("F2 end_span adds only interval bookkeeping history",
                   historyAfter.count, historyBefore.count + 1)
            expect("F2 bookkeeping names an interval end, not lifecycle promotion",
                   historyAfter.last?["kind"] as? String, "span_ended")
            expect("F2 positive control invokes ordinary reconciliation", command("link", [
                "itemId": id, "kind": "session", "targetId": "observer", "label": "Observer",
            ]).status, 200)
            expect("F2 the unchanged evidence really can still promote via an ordinary command",
                   item()["state"] as? String, "verified")
        } else {
            expect("F1 \(variant) cannot close an interval", reply.status, 409)
            expect("F1 \(variant) refusal names unresolved declaration identity",
                   (reply.body["error"] as? [String: Any])?["code"] as? String,
                   "span_identity_unresolved")
            let spans = after["spans"] as? [[String: Any]] ?? []
            check("F1 \(variant) leaves the target interval active",
                  spans.count == 1 && spans[0]["endedAt"] is NSNull)
            expect("F1 \(variant) leaves item history untouched", historyAfter.count,
                   historyBefore.count)
            expect("F1 \(variant) leaves scope untouched", after["scopeRevision"] as? Int,
                   before["scopeRevision"] as? Int)
        }
    }
}

private func workflowOutputScopeProof() {
    let root = workflowTestDirectory("output-scope")
    defer { try? FileManager.default.removeItem(at: root) }
    let boardURL = root.appendingPathComponent("board.json")
    var store = ProjectBoardStore(url: boardURL)
    let identity = workflowIdentity()
    _ = store.ensureProject(id: identity.projectID, name: "Output scope")
    func command(_ op: String, _ fields: [String: Any], trusted: Bool = false,
                 internalOrigin: Bool = false) -> ProjectBoardStore.Reply {
        var body = fields
        body["operation"] = op; body["requestId"] = UUID().uuidString
        body["expectedRevision"] = store.readHeader().revision
        return store.command(body, actor: "root", trusted: trusted, workflowOrigin: internalOrigin)
    }
    let created = command("create", ["projectId": identity.projectID, "title": "Verified work", "type": "feature", "owner": "root"])
    guard let id = created.body["itemId"] as? String else { check("output fixture creates item", false); return }
    func item() -> [String: Any] {
        (store.snapshot(item: id)["board"] as? [String: Any])?["item"] as? [String: Any] ?? [:]
    }
    let workflowURL = root.appendingPathComponent("workflow.json")
    func makeWorkflow() -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(url: workflowURL, boardHeader: {
            let h = store.readHeader(); return (h.enabled, h.revision, h.available)
        }, boardCommand: { store.command($0, actor: $1, workflowOrigin: true) }, autoStart: false)
    }
    var workflow = makeWorkflow()
    guard case .managed(let run) = workflow.prepareIngress(requestID: "scope-run", fingerprint: "scope-run",
        text: "deliver report", imageCount: 0, identity: identity) else { return }
    _ = workflow.markDelivery(runID: run.runID, identity: identity, delivered: true)
    _ = workflow.record(["operation": "begin", "run_id": run.runID,
        "classification": "existing_item", "item_id": id, "phase": "output"],
        requestID: "begin", fingerprint: "begin", identity: identity)
    workflow.drainForTesting()
    expect("output fixture installs qualifying verification", command("record_evidence", [
        "itemId": id, "kind": "verification", "status": "passed", "summary": "Exact proof",
        "sourceId": "output-proof", "subject": String(repeating: "b", count: 40),
    ], trusted: true).status, 200)
    let before = item()
    expect("output fixture actually reaches verified", before["state"] as? String, "verified")
    let delivery: [String: Any] = ["operation": "deliver", "run_id": run.runID,
        "disposition": "delivered", "summary": "Report, not new scope", "next_action": "landing",
        "outputs": [["title": "Report", "url": "https://app.clawdline.com/#document=1", "kind": "document"]]]
    _ = workflow.record(delivery, requestID: "delivery", fingerprint: "delivery", identity: identity)
    workflow.drainForTesting()
    let after = item()
    expect("delivery output never clears verified state", after["state"] as? String, "verified")
    expect("delivery output preserves exact scope", after["scopeRevision"] as? Int, before["scopeRevision"] as? Int)
    check("delivery output retains exact current evidence", NSDictionary(dictionary: after["currentEvidence"] as? [String: Any] ?? [:])
        .isEqual(to: before["currentEvidence"] as? [String: Any] ?? [:]))
    expect("delivery output closes activity", (after["progress"] as? [String: Any])?["active"] as? Bool, false)
    let artifacts = after["artifacts"] as? [[String: Any]] ?? []
    check("delivery reference is durable and not an acceptance artifact", artifacts.count == 1 && artifacts.first?["referenceOnly"] as? Bool == true)
    let reference: [String: Any] = ["itemId": id, "title": "forged", "url": "https://example.test", "kind": "document"]
    expect("public caller cannot forge output bookkeeping", command("record_output", reference, trusted: true).status, 403)
    expect("a reference cannot become trusted artifact acceptance", command("accept_artifact", [
        "itemId": id, "artifactId": artifacts.first?["id"] as? String ?? "missing",
    ], trusted: true).status, 409)
    workflow = makeWorkflow(); workflow.drainForTesting()
    _ = workflow.record(delivery, requestID: "delivery", fingerprint: "delivery", identity: identity)
    workflow.drainForTesting()
    store = ProjectBoardStore(url: boardURL)
    expect("reload and duplicate receipt retain exactly one output", (item()["artifacts"] as? [[String: Any]])?.count, 1)
    expect("reload retains verified state", item()["state"] as? String, "verified")
    // A promotable persisted fixture detects accidental lifecycle reconciliation by record_output.
    do {
        var saved = try JSONSerialization.jsonObject(with: Data(contentsOf: boardURL)) as! [String: Any]
        var items = saved["items"] as! [[String: Any]]
        items[0]["state"] = "execution"; saved["items"] = items
        try JSONSerialization.data(withJSONObject: saved).write(to: boardURL)
        store = ProjectBoardStore(url: boardURL)
        expect("internal output bookkeeping succeeds on promotable fixture", command("record_output", reference, internalOrigin: true).status, 200)
        expect("output bookkeeping does not reconcile lifecycle", item()["state"] as? String, "execution")
    } catch { check("output fixture remains decodable", false) }
    expect("ordinary scope artifact remains supported", command("artifact", reference).status, 200)
    check("ordinary artifact still invalidates scope and proof", (item()["scopeRevision"] as? Int ?? 0) > (before["scopeRevision"] as? Int ?? 0)
        && (item()["currentEvidence"] as? [String: Any])?["verificationId"] is NSNull)
    // Unidentifiable legacy declarations are retained, not guessed closed or counted as live.
    do {
        var saved = try JSONSerialization.jsonObject(with: Data(contentsOf: boardURL)) as! [String: Any]
        var items = saved["items"] as! [[String: Any]]
        var spans = items[0]["spans"] as! [[String: Any]]
        spans[0]["id"] = "legacy-span"; spans[0].removeValue(forKey: "endedAt")
        items[0]["spans"] = spans; saved["items"] = items
        try JSONSerialization.data(withJSONObject: saved).write(to: boardURL)
        store = ProjectBoardStore(url: boardURL)
        let progress = item()["progress"] as? [String: Any] ?? [:]
        expect("legacy unproved interval does not claim current activity", progress["active"] as? Bool, false)
        check("legacy interval has an explicit uncertainty warning", (progress["warningCodes"] as? [String] ?? []).contains("legacy_span_identity_unresolved"))
        expect("legacy interval remains open rather than guessed complete", ((item()["spans"] as? [[String: Any]])?.first?["endedAt"] is NSNull), true)
        expect("catalog activity agrees with detail after reload", store.readSeed(rebuild: true).seed.catalog.first?["activeItemCount"] as? Int, 0)
        expect("a fresh declaration for the legacy Session is accepted", command("span", [
            "itemId": id, "sessionId": identity.conversationID, "phase": "output",
        ]).status, 200)
        let freshSpans = item()["spans"] as? [[String: Any]] ?? []
        check("F2 new activity never guesses an end for the legacy interval", freshSpans.first { $0["id"] as? String == "legacy-span" }?["endedAt"] is NSNull)
        let freshProgress = item()["progress"] as? [String: Any] ?? [:]
        expect("F2 only the new identifiable declaration counts as active", (freshProgress["coordinationContext"] as? [String: Any])?["activeSpans"] as? Int, 1)
        check("F2 active work retains its legacy uncertainty", (freshProgress["warningCodes"] as? [String] ?? []).contains("legacy_span_identity_unresolved"))
    } catch { check("legacy activity fixture remains decodable", false) }
    for mode in ["code", "artifact"] {
        let created = command("create", ["projectId": identity.projectID, "title": mode, "type": "task", "owner": "root"])
        guard let taskID = created.body["itemId"] as? String else { continue }
        func task() -> [String: Any] {
            (store.snapshot(item: taskID)["board"] as? [String: Any])?["item"] as? [String: Any] ?? [:]
        }
        if mode == "code" {
            expect("F1 code task installs verification", command("record_evidence", [
                "itemId": taskID, "kind": "verification", "status": "passed", "summary": "Code proof",
                "sourceId": "task-proof", "subject": String(repeating: "c", count: 40),
            ], trusted: true).status, 200)
        } else {
            _ = command("artifact", ["itemId": taskID, "title": "Accepted document", "url": "https://example.test/doc", "kind": "document"])
            let artifactID = (task()["artifacts"] as? [[String: Any]])?.first?["id"] as? String ?? "missing"
            expect("F1 document task installs artifact acceptance", command("accept_artifact", [
                "itemId": taskID, "artifactId": artifactID,
            ], trusted: true).status, 200)
        }
        let beforeTask = task()
        expect("F1 task has a meaningful progress regime before recording", (beforeTask["progress"] as? [String: Any])?["state"] as? String, mode == "code" ? "verified" : "landed")
        expect("F1 opposite-kind reference records", command("record_output", [
            "itemId": taskID, "title": "Informational output", "url": "https://example.test/reference",
            "kind": mode == "code" ? "document" : "commit",
        ], internalOrigin: true).status, 200)
        store = ProjectBoardStore(url: boardURL)
        expect("F1 reference cannot switch the task verification regime after reload", (task()["progress"] as? [String: Any])?["state"] as? String, (beforeTask["progress"] as? [String: Any])?["state"] as? String)
        expect("F1 reference preserves task lifecycle", task()["state"] as? String, beforeTask["state"] as? String)
    }
}

func runProjectBoardWorkflowTests() {
group("managed Board ingress is durable, bounded and identity-bound") {
    let root = workflowTestDirectory("ingress")
    defer { try? FileManager.default.removeItem(at: root) }
    var enabled = true
    var revision = 7
    let resources = root.appendingPathComponent("Moved App.app/Contents/Resources")
    try! FileManager.default.createDirectory(at: resources, withIntermediateDirectories: true)
    let helper = resources.appendingPathComponent("clawdline-board-workflow")
    try! Data("#!/bin/sh\nexit 0\n".utf8).write(to: helper)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: helper.path)
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (enabled, revision, true) }, autoStart: false, bundleResources: resources)
    workflow.syncMode()
    let identity = workflowIdentity()
    let first = workflow.prepareIngress(
        requestID: "phone-send-1", fingerprint: "body-a",
        text: "請修正原始內容", imageCount: 1, identity: identity)
    guard case .managed(let prepared) = first else {
        check("enabled valid ingress is managed", false)
        return
    }
    check("the original user content remains the payload prefix",
          prepared.wireText.hasPrefix("請修正原始內容"))
    check("the same payload carries bounded explicit metadata",
          prepared.wireText.contains("<clawdline-workflow")
            && prepared.wireText.utf8.count < 4_096)
    check("metadata contains no credential", !prepared.wireText.contains("orchestrator-token"))
    let metadataLine = prepared.wireText.components(separatedBy: "\n").first { $0.hasPrefix("{") } ?? "{}"
    let metadata = try! JSONSerialization.jsonObject(with: Data(metadataLine.utf8)) as! [String: Any]
    expect("managed bootstrap supplies the installed absolute helper path", metadata["helper_path"] as? String, helper.path)
    check("available bundle helper does not declare an installation gap", metadata["mode_gap"] is NSNull)
    for variant in ["missing", "not_executable", "symlink", "directory"] {
        let candidate = root.appendingPathComponent(variant)
        try! FileManager.default.createDirectory(at: candidate, withIntermediateDirectories: true)
        let file = candidate.appendingPathComponent("clawdline-board-workflow")
        if variant == "not_executable" {
            try! Data("not executable".utf8).write(to: file)
            try! FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        } else if variant == "symlink" {
            try! FileManager.default.createSymbolicLink(at: file, withDestinationURL: helper)
        } else if variant == "directory" {
            try! FileManager.default.createDirectory(at: file, withIntermediateDirectories: true)
        }
        let absent = ProjectBoardWorkflow(url: candidate.appendingPathComponent("journal.json"),
            boardHeader: { (true, 1, true) }, autoStart: false, bundleResources: candidate)
        if case .managed(let value) = absent.prepareIngress(requestID: variant, fingerprint: variant,
            text: "still send", imageCount: 0, identity: workflowIdentity()) {
            check("\(variant) helper is a typed nonblocking bootstrap gap", value.wireText.hasPrefix("still send")
                && value.wireText.contains("helper_unavailable") && !value.wireText.contains("helper_path"))
        } else { check("\(variant) helper must not block the message", false) }
    }

    let replay = workflow.prepareIngress(
        requestID: "phone-send-1", fingerprint: "body-a",
        text: "請修正原始內容", imageCount: 1, identity: identity)
    if case .managed(let same) = replay {
        expect("an identical transport retry reuses one run", same.runID, prepared.runID)
    } else {
        check("an identical transport retry remains managed", false)
    }
    if case .refused(let code) = workflow.prepareIngress(
        requestID: "phone-send-1", fingerprint: "body-b",
        text: "不同內容", imageCount: 1, identity: identity) {
        expect("the same request id with another fingerprint conflicts", code,
               "workflow_request_conflict")
    } else {
        check("a conflicting retry is refused", false)
    }
    let restartedIdentity = ProjectBoardWorkflow.Identity(
        terminalID: identity.terminalID, provider: identity.provider,
        conversationID: identity.conversationID, projectID: identity.projectID,
        projectPath: identity.projectPath, processGeneration: "pid-101-start-300")
    if case .managed(let nextProcess) = workflow.prepareIngress(
        requestID: "phone-send-1", fingerprint: "body-a", text: "same request",
        imageCount: 1, identity: restartedIdentity) {
        check("a new exact process identity receives its own run",
              nextProcess.runID != prepared.runID && !nextProcess.replay)
    } else {
        check("a new exact process identity receives its own run", false)
    }
    expect("delivery is bound to the observed process identity",
           workflow.markDelivery(runID: prepared.runID, identity: workflowIdentity("2"),
                                 delivered: true).code,
           "workflow_identity_mismatch")
    expect("the accepted terminal handoff is durably distinguished",
           workflow.markDelivery(runID: prepared.runID, identity: identity,
                                 delivered: true).code,
           "workflow_delivery_recorded")

    let reloaded = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (enabled, revision, true) }, autoStart: false)
    expect("a restart keeps the run's delivered state",
           reloaded.snapshot(runID: prepared.runID)?["delivery"] as? String,
           "delivered")
    enabled = false; revision += 1; reloaded.syncMode()
    if case .bypassed = reloaded.prepareIngress(
        requestID: "off-send", fingerprint: "off-body", text: "ordinary",
        imageCount: 0, identity: identity) {
        check("Board OFF bypasses managed ingress", true)
    } else {
        check("Board OFF bypasses managed ingress", false)
    }
    enabled = true; revision += 1; reloaded.syncMode()
    let stale = reloaded.record(
        ["operation": "progress", "run_id": prepared.runID, "summary": "late"],
        requestID: "late-progress", fingerprint: "late-progress-body", identity: identity)
    expect("an OFF ON boundary rejects an old epoch receipt", stale.code,
           "workflow_epoch_stale")
    let gaps = ProjectBoardWorkflowHTTP.route(
        remoteRequest("GET", ProjectBoardWorkflowHTTP.historicalGapsPath),
        machine: true, identity: nil, workflow: reloaded)
    let gapBody = gaps.map(workflowJSON) ?? [:]
    let gapProjection = gapBody["workflow_gaps"] as? [String: Any]
    let gapRuns = gapProjection?["runs"] as? [[String: Any]] ?? []
    expect("machine authority can read bounded gaps without a live process identity",
           gaps?.status, 200)
    check("historical gaps retain old process identity and missing follow-up",
          gapRuns.contains { row in
            let found = row["identity"] as? [String: Any]
            return found?["process_generation"] as? String == identity.processGeneration
                && ((row["missing_follow_up"] as? [String]) ?? []).contains("begin")
          })
    expect("paired authority cannot read the machine-wide historical journal",
           ProjectBoardWorkflowHTTP.route(
            remoteRequest("GET", ProjectBoardWorkflowHTTP.historicalGapsPath),
            machine: false, identity: nil, workflow: reloaded)?.status, 403)

    let blocked = root.appendingPathComponent("not-a-directory")
    try? Data("x".utf8).write(to: blocked)
    let unwritable = ProjectBoardWorkflow(
        url: blocked.appendingPathComponent("workflow.json"),
        boardHeader: { (true, 1, true) }, autoStart: false)
    unwritable.syncMode()
    if case .refused(let code) = unwritable.prepareIngress(
        requestID: "disk-full", fingerprint: "body", text: "still send me",
        imageCount: 0, identity: identity) {
        expect("an ingress persistence failure is typed", code,
               "workflow_persistence_failed")
    } else {
        check("a broken journal cannot claim reliable ingress", false)
    }

    var synchronizations = 0
    let syncURL = root.appendingPathComponent("sync-failure.json")
    let unsynchronized = ProjectBoardWorkflow(
        url: syncURL,
        boardHeader: { (true, 1, true) },
        journalSynchronize: { _ in
            synchronizations += 1
            if synchronizations > 1 { throw WorkflowSyncFixtureError.refused }
        }, autoStart: false)
    unsynchronized.syncMode()
    let syncFailure = unsynchronized.prepareIngress(
        requestID: "sync-failure", fingerprint: "sync-body", text: "must remain ordinary",
        imageCount: 0, identity: identity)
    if case .refused(let code) = syncFailure {
        expect("a journal sync failure is a typed ingress refusal", code,
               "workflow_persistence_failed")
    } else {
        check("a journal sync failure cannot return managed metadata", false)
    }
    expect("a sync failure does not install the unproved run in memory",
           ((unsynchronized.historicalGaps()["runs"] as? [[String: Any]]) ?? []).count, 0)
    let syncMode = ((try? FileManager.default.attributesOfItem(atPath: syncURL.path)[.posixPermissions])
        as? NSNumber)?.intValue ?? 0
    expect("the journal keeps mode 0600 even when synchronization fails", syncMode & 0o777, 0o600)
}

group("workflow receipts stay attested and reconcile through a bounded durable outbox") {
    workflowSettledCapacityProof()
    workflowProgramBindingAndDocumentProof()
    workflowSessionAssignmentProof()
    workflowAssignmentLifecycleProof()
    workflowRootLandingProof()
    workflowOutputScopeProof()
    workflowEndSpanAuthorityProof()
    workflowActivityBoundaryProof()
    workflowSupplementStoreProof()
    let root = workflowTestDirectory("outbox")
    defer { try? FileManager.default.removeItem(at: root) }
    let lock = NSLock()
    var commands: [[String: Any]] = []
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (true, 10, true) },
        ensureProject: { _, _ in (true, nil) },
        boardCommand: { body, _ in
            lock.lock(); commands.append(body); lock.unlock()
            return ProjectBoardStore.Reply(
                status: 200,
                body: ["itemId": body["itemId"] as? String ?? "created-item"])
        }, autoStart: false)
    workflow.syncMode()
    let identity = workflowIdentity()
    guard case .managed(let prepared) = workflow.prepareIngress(
        requestID: "turn-1", fingerprint: "turn-body", text: "build it",
        imageCount: 0, identity: identity) else {
        check("the fixture creates a run", false); return
    }
    _ = workflow.markDelivery(runID: prepared.runID, identity: identity, delivered: true)
    let began = workflow.record([
        "operation": "begin", "run_id": prepared.runID,
        "classification": "existing_item", "item_id": "item-1", "phase": "output",
    ], requestID: "begin-1", fingerprint: "begin-body", identity: identity)
    expect("begin is an attested receipt rather than a lifecycle claim", began.authority,
           "assistant_attested")
    expect("begin is durably accepted before reconciliation", began.status, 202)
    let progress = workflow.record([
        "operation": "progress", "run_id": prepared.runID, "summary": "implemented",
        "outputs": [["title": "preview", "url": "https://example.test/preview", "kind": "website"]],
        "remaining": [["title": "owner review", "owner": "root", "blocking": true]],
    ], requestID: "progress-1", fingerprint: "progress-body", identity: identity)
    expect("progress queues bounded Board facts", progress.status, 202)
    let checklistSupplement: [String: Any] = [
        "operation": "supplement", "run_id": prepared.runID, "version": 1,
        "kind": "checklist", "title": "Confirm the export", "owner": "root",
        "acceptance": "The exported document opens from its canonical Cloud link.",
        "disposition": "required", "actor_kind": "assistant",
    ]
    let supplementalChecklist = workflow.record(
        checklistSupplement, requestID: "supplement-checklist-1",
        fingerprint: "supplement-checklist-body", identity: identity)
    expect("a versioned checklist supplement is durably accepted",
           supplementalChecklist.code, "workflow_supplement_recorded")
    let childSupplement: [String: Any] = [
        "operation": "supplement", "run_id": prepared.runID, "version": 1,
        "kind": "child", "type": "task", "title": "Publish the follow-up",
        "owner": "next-root", "acceptance": "The new child has an acknowledged owner.",
        "disposition": "optional", "actor_kind": "external",
    ]
    expect("a versioned child supplement is durably accepted",
           workflow.record(childSupplement, requestID: "supplement-child-1",
                           fingerprint: "supplement-child-body", identity: identity).status, 202)
    var changedSupplement = checklistSupplement
    changedSupplement["acceptance"] = "A different acceptance contract."
    expect("a changed supplement cannot reuse its semantic receipt key",
           workflow.record(changedSupplement, requestID: "supplement-checklist-1",
                           fingerprint: "supplement-checklist-changed", identity: identity).code,
           "workflow_request_conflict")
    var widenedSupplement = checklistSupplement
    widenedSupplement["unexpected"] = true
    expect("supplement commands use an exact versioned key set",
           workflow.record(widenedSupplement, requestID: "supplement-extra",
                           fingerprint: "supplement-extra-body", identity: identity).code,
           "workflow_command_fields")
    let delivered = workflow.record([
        "operation": "deliver", "run_id": prepared.runID, "disposition": "delivered",
        "summary": "implementation delivered", "next_action": "root review",
    ], requestID: "deliver-1", fingerprint: "deliver-body", identity: identity)
    expect("deliver records a receipt without claiming completion", delivered.code,
           "workflow_receipt_recorded")
    workflow.drainForTesting()
    lock.lock(); let operations = commands.compactMap { $0["operation"] as? String }; lock.unlock()
    check("the worker uses only existing factual Board commands",
          Set(operations).isSubset(of: ["create", "checklist", "link", "span", "end_span", "record_output", "record_session_delivery",
                                       "obligation"]))
    check("no assistant receipt can forge verification or landing",
          !operations.contains("record_evidence") && !operations.contains("transition"))
    check("the output and unresolved obligation reach reconciliation",
          operations.contains("record_output") && operations.contains("obligation"))
    let checklistCommand = commands.first { $0["operation"] as? String == "checklist" }
    check("checklist supplements preserve required scope in the Board command",
          checklistCommand?["title"] as? String == "Confirm the export"
            && checklistCommand?["required"] as? Bool == true)
    let childCreate = commands.first {
        $0["operation"] as? String == "create" && $0["parentId"] as? String == "item-1"
    }
    check("child supplements use create-with-parent and preserve their owner and acceptance",
          childCreate?["owner"] as? String == "next-root"
            && (childCreate?["summary"] as? String)?.contains("The new child") == true)
    check("the supplemental child is linked back to its source Session",
          commands.contains { $0["operation"] as? String == "link"
            && $0["itemId"] as? String == "created-item"
            && $0["targetId"] as? String == identity.conversationID })
    check("supplement acceptance and actor kind reach a durable obligation",
          commands.contains { $0["operation"] as? String == "obligation"
            && $0["requiredAction"] as? String == "The new child has an acknowledged owner."
            && $0["actorKind"] as? String == "external"
            && ($0["blockingScope"] as? String)?.contains(prepared.runID) == true })
    let journalText = (try? String(contentsOf: root.appendingPathComponent("workflow.json"),
                                   encoding: .utf8)) ?? ""
    check("the journal itself preserves supplement kind source acceptance and optional actor",
          journalText.contains("\"kind\":\"child\"")
            && journalText.contains("\"sourceRunID\":\"\(prepared.runID)\"")
            && journalText.contains("\"sourceSessionID\":\"\(identity.conversationID)\"")
            && journalText.contains("\"acceptance\":\"The new child")
            && journalText.contains("\"required\":false")
            && journalText.contains("\"actorKind\":\"external\""))
    expect("the run remains delivered-only after its receipt",
           workflow.snapshot(runID: prepared.runID)?["completion"] as? String,
           "delivered")

    let duplicate = workflow.record([
        "operation": "deliver", "run_id": prepared.runID, "disposition": "delivered",
        "summary": "implementation delivered", "next_action": "root review",
    ], requestID: "deliver-1", fingerprint: "deliver-body", identity: identity)
    expect("an identical receipt retry replays its answer", duplicate.code, delivered.code)
    let conflict = workflow.record([
        "operation": "deliver", "run_id": prepared.runID, "disposition": "cancelled",
        "summary": "different", "next_action": "none",
    ], requestID: "deliver-1", fingerprint: "different-body", identity: identity)
    expect("a changed receipt under the same id conflicts", conflict.code,
           "workflow_request_conflict")
    let stop = workflow.record(
        ["operation": "stop", "run_id": prepared.runID], requestID: "stop-1",
        fingerprint: "stop-body", identity: identity)
    expect("stop is not a completion operation", stop.code, "workflow_operation_unknown")

    let nextIdentity = ProjectBoardWorkflow.Identity(
        terminalID: identity.terminalID, provider: identity.provider,
        conversationID: identity.conversationID, projectID: identity.projectID,
        projectPath: identity.projectPath, processGeneration: "pid-101-start-300")
    guard case .managed(let nextRun) = workflow.prepareIngress(
        requestID: "turn-next", fingerprint: "turn-next-body", text: "new process",
        imageCount: 0, identity: nextIdentity) else {
        check("the receipt scope fixture creates a new-process run", false); return
    }
    _ = workflow.markDelivery(runID: nextRun.runID, identity: nextIdentity, delivered: true)
    let sameSemanticKey = workflow.record([
        "operation": "begin", "run_id": nextRun.runID,
        "classification": "existing_item", "item_id": "item-2", "phase": "output",
    ], requestID: "begin-1", fingerprint: "begin-body-new-process", identity: nextIdentity)
    expect("semantic receipt keys are scoped to the exact terminal project process run",
           sameSemanticKey.code, "workflow_begin_recorded")
    guard case .managed(let sameProcessNextRun) = workflow.prepareIngress(
        requestID: "turn-next-2", fingerprint: "turn-next-body-2", text: "another turn",
        imageCount: 0, identity: nextIdentity) else {
        check("the receipt scope fixture creates another run in one process", false); return
    }
    _ = workflow.markDelivery(
        runID: sameProcessNextRun.runID, identity: nextIdentity, delivered: true)
    let sameKeyAnotherRun = workflow.record([
        "operation": "begin", "run_id": sameProcessNextRun.runID,
        "classification": "existing_item", "item_id": "item-3", "phase": "output",
    ], requestID: "begin-1", fingerprint: "begin-body-another-run", identity: nextIdentity)
    expect("semantic receipt keys include the exact workflow run",
           sameKeyAnotherRun.code, "workflow_begin_recorded")
    let journalURL = root.appendingPathComponent("workflow.json")
    if var legacy = (try? JSONSerialization.jsonObject(with: Data(contentsOf: journalURL)))
        as? [String: Any] {
        legacy["schemaVersion"] = 1
        legacy["receipts"] = ((legacy["receipts"] as? [[String: Any]]) ?? []).map { row in
            var old = row; old.removeValue(forKey: "requestScope"); return old
        }
        let bytes = try! JSONSerialization.data(withJSONObject: legacy, options: [.sortedKeys])
        try! bytes.write(to: journalURL, options: .atomic)
    }
    let migrated = ProjectBoardWorkflow(
        url: journalURL, boardHeader: { (true, 10, true) }, autoStart: false)
    let migratedReplay = migrated.record([
        "operation": "begin", "run_id": nextRun.runID,
        "classification": "existing_item", "item_id": "item-2", "phase": "output",
    ], requestID: "begin-1", fingerprint: "begin-body-new-process", identity: nextIdentity)
    expect("schema-1 receipts migrate into their exact durable request scope",
           migratedReplay.code, "workflow_begin_recorded")
    let migratedText = (try? String(contentsOf: journalURL, encoding: .utf8)) ?? ""
    check("receipt migration is explicit and writes schema 2 with requestScope",
          migratedText.contains("\"schemaVersion\":3")
            && migratedText.contains("\"requestScope\":"))

    var transientBodies: [[String: Any]] = []
    let transient = ProjectBoardWorkflow(
        url: root.appendingPathComponent("transient.json"),
        boardHeader: { (true, 10, true) },
        ensureProject: { _, _ in (true, nil) },
        boardCommand: { body, _ in
            transientBodies.append(body)
            return ProjectBoardStore.Reply(
                status: transientBodies.count == 1 ? 500 : 200, body: [:])
        }, autoStart: false)
    transient.syncMode()
    guard case .managed(let transientRun) = transient.prepareIngress(
        requestID: "transient-turn", fingerprint: "transient-body", text: "retry",
        imageCount: 0, identity: identity) else {
        check("the transient fixture creates a run", false); return
    }
    _ = transient.markDelivery(runID: transientRun.runID, identity: identity, delivered: true)
    _ = transient.record([
        "operation": "begin", "run_id": transientRun.runID,
        "classification": "existing_item", "item_id": "item-1", "phase": "output",
    ], requestID: "transient-begin", fingerprint: "transient-begin-body", identity: identity)
    transient.drainForTesting()
    check("an ambiguous Board 5xx retries the exact durable request rather than mutating twice",
          transientBodies.count >= 2
            && transientBodies[0]["requestId"] as? String
                == transientBodies[1]["requestId"] as? String)
}

group("a blocked Board consumer cannot block managed terminal send admission") {
    let root = workflowTestDirectory("blocked-consumer")
    defer { try? FileManager.default.removeItem(at: root) }
    let entered = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (true, 20, true) },
        ensureProject: { _, _ in (true, nil) },
        boardCommand: { body, _ in
            entered.signal(); _ = release.wait(timeout: .now() + 2)
            return ProjectBoardStore.Reply(status: 200,
                body: ["itemId": body["itemId"] as? String ?? "created-item"])
        })
    workflow.syncMode()
    let identity = workflowIdentity()
    guard case .managed(let first) = workflow.prepareIngress(
        requestID: "blocked-1", fingerprint: "blocked-body-1", text: "one",
        imageCount: 0, identity: identity) else {
        check("the first run is admitted", false); return
    }
    _ = workflow.markDelivery(runID: first.runID, identity: identity, delivered: true)
    _ = workflow.record([
        "operation": "begin", "run_id": first.runID,
        "classification": "existing_item", "item_id": "item-1", "phase": "output",
    ], requestID: "blocked-begin", fingerprint: "blocked-begin-body", identity: identity)
    check("the background consumer entered its blocked command",
          entered.wait(timeout: .now() + 1) == .success)
    let started = Date()
    let second = workflow.prepareIngress(
        requestID: "blocked-2", fingerprint: "blocked-body-2", text: "two",
        imageCount: 0, identity: identity)
    check("new send ingress does not queue behind Board consumption",
          Date().timeIntervalSince(started) < 0.25)
    if case .managed = second { check("the second run is still managed", true) }
    else { check("the second run is still managed", false) }
    release.signal()
    workflow.drainForTesting()

    // Capacity pressure is the deterministic form of the index race: the first Board call is
    // outside the workflow lock while another ingress attempts to evict its completed run.
    let capacityRoot = workflowTestDirectory("outbox-capacity-race")
    defer { try? FileManager.default.removeItem(at: capacityRoot) }
    let capacityEntered = DispatchSemaphore(value: 0)
    let capacityRelease = DispatchSemaphore(value: 0)
    let drained = DispatchSemaphore(value: 0)
    let capacityBlockLock = NSLock()
    var capacityBlocksRemaining = 1
    let capacityWorkflow = ProjectBoardWorkflow(
        url: capacityRoot.appendingPathComponent("workflow.json"),
        boardHeader: { (true, 22, true) },
        ensureProject: { _, _ in (true, nil) },
        boardCommand: { body, _ in
            capacityBlockLock.lock()
            let shouldBlock = capacityBlocksRemaining > 0
            if shouldBlock { capacityBlocksRemaining -= 1 }
            capacityBlockLock.unlock()
            if shouldBlock {
                capacityEntered.signal(); _ = capacityRelease.wait(timeout: .now() + 2)
            }
            return ProjectBoardStore.Reply(status: 200,
                body: ["itemId": body["itemId"] as? String ?? "created-item"])
        }, limits: ProjectBoardWorkflow.Limits(runs: 1, eventsPerRun: 64,
                                                receipts: 32, outbox: 32,
                                                maximumBytes: 1_024 * 1_024),
        autoStart: false)
    capacityWorkflow.syncMode()
    let capacityIdentity = workflowIdentity("3")
    guard case .managed(let first) = capacityWorkflow.prepareIngress(
        requestID: "capacity-first", fingerprint: "capacity-first-body", text: "one",
        imageCount: 0, identity: capacityIdentity) else {
        check("the capacity fixture creates its first run", false); return
    }
    _ = capacityWorkflow.markDelivery(
        runID: first.runID, identity: capacityIdentity, delivered: true)
    _ = capacityWorkflow.record([
        "operation": "begin", "run_id": first.runID,
        "classification": "existing_item", "item_id": "item-capacity", "phase": "output",
    ], requestID: "capacity-begin", fingerprint: "capacity-begin-body",
       identity: capacityIdentity)
    _ = capacityWorkflow.record([
        "operation": "deliver", "run_id": first.runID, "disposition": "delivered",
        "summary": "done", "next_action": "none",
    ], requestID: "capacity-deliver", fingerprint: "capacity-deliver-body",
       identity: capacityIdentity)
    DispatchQueue.global().async { capacityWorkflow.drainForTesting(); drained.signal() }
    check("the capacity fixture blocks after persisting one inflight row",
          capacityEntered.wait(timeout: .now() + 1) == .success)
    let pressure = capacityWorkflow.prepareIngress(
        requestID: "capacity-second", fingerprint: "capacity-second-body", text: "two",
        imageCount: 0, identity: capacityIdentity)
    if case .refused(let code) = pressure {
        expect("an inflight run is protected instead of evicted", code,
               "workflow_capacity_reached")
    } else {
        check("capacity pressure cannot evict an inflight outbox subject", false)
    }
    capacityRelease.signal()
    check("the immutable-id settlement finishes after capacity pressure",
          drained.wait(timeout: .now() + 2) == .success)
    if case .managed = capacityWorkflow.prepareIngress(
        requestID: "capacity-third", fingerprint: "capacity-third-body", text: "three",
        imageCount: 0, identity: capacityIdentity) {
        check("a fully settled run becomes evictable", true)
    } else {
        check("a fully settled run becomes evictable", false)
    }
}

group("managed send carries one workflow payload and reports delivery truthfully") {
    let root = workflowTestDirectory("send-integration")
    defer { try? FileManager.default.removeItem(at: root) }
    var boardEnabled = true
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (boardEnabled, 30, true) },
        ensureProject: { _, _ in (true, nil) },
        boardCommand: { _, _ in ProjectBoardStore.Reply(status: 200, body: [:]) },
        autoStart: false)
    workflow.syncMode()
    let identity = workflowIdentity()
    let session = TargetSession(
        backend: .tmux, id: identity.terminalID, name: "workflow send",
        tty: "/dev/ttys095", windowIndex: 0, tabIndex: 0,
        assistant: .codex, cwd: identity.projectPath)
    let identityTwo = workflowIdentity("4")
    let sessionTwo = TargetSession(
        backend: .tmux, id: identityTwo.terminalID, name: "workflow send two",
        tty: "/dev/ttys096", windowIndex: 0, tabIndex: 1,
        assistant: .codex, cwd: identityTwo.projectPath)
    let writer = RemoteAuth.addDevice(name: "workflow send test", caps: [.read, .send])
    let wasWriting = Config.shared.remoteWrite
    defer {
        Config.shared.remoteWrite = wasWriting
        RemoteAuth.revoke(id: writer.id)
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.workflowIdentityForTesting = nil
        RemoteServer.terminalSendForTesting = nil
        ProjectBoardWorkflow.configureSharedForTesting(nil)
    }
    Config.shared.remoteWrite = true
    ProjectBoardWorkflow.configureSharedForTesting(workflow)
    RemoteServer.sessionPayloadForTesting = (
        [session, sessionTwo], [session.id: .idle, sessionTwo.id: .idle])
    RemoteServer.workflowIdentityForTesting = {
        $0.id == sessionTwo.id ? identityTwo : identity
    }
    let sentLock = NSLock()
    var sent: [String] = []
    var sentTargets: [String] = []
    RemoteServer.terminalSendForTesting = { text, target in
        sentLock.lock(); sent.append(text); sentTargets.append(target.id); sentLock.unlock()
        return nil
    }

    func sendTo(_ target: TargetSession, _ text: String,
                key: String) -> RemoteServer.Response? {
        let done = DispatchSemaphore(value: 0)
        var answer: RemoteServer.Response?
        RemoteServer.shared.sendTerminalForTesting(remoteRequest(
            "POST", "/v1/sessions/\(target.id)/send",
            headers: ["Authorization": "Bearer \(writer.token)",
                      "Idempotency-Key": key],
            body: try! String(data: JSONSerialization.data(
                withJSONObject: ["text": text], options: [.sortedKeys]), encoding: .utf8)!)) {
            answer = $0; done.signal()
        }
        _ = done.wait(timeout: .now() + 2)
        return answer
    }
    func send(_ text: String, key: String) -> RemoteServer.Response? {
        sendTo(session, text, key: key)
    }

    let key = UUID().uuidString
    let accepted = send("keep these exact words", key: key)
    sentLock.lock(); let firstWire = sent; sentLock.unlock()
    check("one user action performs exactly one terminal send", firstWire.count == 1)
    check("the one payload preserves original content and appends bounded metadata",
          firstWire.first?.hasPrefix("keep these exact words\n\n<clawdline-workflow") == true
            && firstWire.first?.contains("\"coverage\":\"managed_ingress\"") == true)
    let acceptedBody = accepted.flatMap {
        try? JSONSerialization.jsonObject(with: $0.body) as? [String: Any]
    } ?? [:]
    expect("the response says the terminal delivery was recorded",
           (acceptedBody["workflow"] as? [String: Any])?["code"] as? String,
           "workflow_delivery_recorded")
    expect("terminal acceptance is broker-observed rather than assistant-attested",
           (acceptedBody["workflow"] as? [String: Any])?["authority"] as? String,
           "broker_observed")
    let acceptedRunID = ((acceptedBody["workflow"] as? [String: Any])?["run_id"] as? String) ?? ""
    _ = send("keep these exact words", key: key)
    sentLock.lock(); let afterReplay = sent.count; sentLock.unlock()
    expect("an exact transport retry does not type a second prompt", afterReplay, 1)

    let crossTargetKey = UUID().uuidString
    let namespaceSamples = Set([
        RemoteServer.terminalSendIdempotencyKey(
            principal: "device-a", method: "POST", target: session.id, rawKey: crossTargetKey),
        RemoteServer.terminalSendIdempotencyKey(
            principal: "device-b", method: "POST", target: session.id, rawKey: crossTargetKey),
        RemoteServer.terminalSendIdempotencyKey(
            principal: "device-a", method: "PATCH", target: session.id, rawKey: crossTargetKey),
        RemoteServer.terminalSendIdempotencyKey(
            principal: "device-a", method: "POST", target: sessionTwo.id, rawKey: crossTargetKey),
    ])
    expect("send idempotency scopes principal, method and target", namespaceSamples.count, 4)
    _ = send("first target", key: crossTargetKey)
    _ = sendTo(sessionTwo, "second target", key: crossTargetKey)
    sentLock.lock(); let crossTargets = Array(sentTargets.suffix(2)); sentLock.unlock()
    check("the same bare key on different Sessions cannot replay another target's response",
          crossTargets == [session.id, sessionTwo.id])

    let concurrentKey = UUID().uuidString
    let firstEntered = DispatchSemaphore(value: 0)
    let firstRelease = DispatchSemaphore(value: 0)
    let bothDone = DispatchSemaphore(value: 0)
    let responsesLock = NSLock()
    var concurrentResponses: [Int] = []
    RemoteServer.terminalSendForTesting = { text, target in
        sentLock.lock(); sent.append(text); sentTargets.append(target.id); sentLock.unlock()
        if target.id == session.id {
            firstEntered.signal(); _ = firstRelease.wait(timeout: .now() + 2)
        }
        return nil
    }
    func sendConcurrent(_ target: TargetSession, _ text: String) {
        RemoteServer.shared.sendTerminalForTesting(remoteRequest(
            "POST", "/v1/sessions/\(target.id)/send",
            headers: ["Authorization": "Bearer \(writer.token)",
                      "Idempotency-Key": concurrentKey],
            body: "{\"text\":\"\(text)\"}")) {
            responsesLock.lock(); concurrentResponses.append($0.status); responsesLock.unlock()
            bothDone.signal()
        }
    }
    sendConcurrent(session, "blocked first target")
    check("the first cross-target send is in flight",
          firstEntered.wait(timeout: .now() + 1) == .success)
    sendConcurrent(sessionTwo, "independent second target")
    let queued = DispatchSemaphore(value: 0)
    RemoteServer.shared.heartbeatTurnForTesting { queued.signal() }
    check("the second target is admitted before the first settles",
          queued.wait(timeout: .now() + 1) == .success)
    firstRelease.signal()
    check("both cross-target callers settle independently",
          bothDone.wait(timeout: .now() + 2) == .success
            && bothDone.wait(timeout: .now() + 2) == .success)
    responsesLock.lock(); let responseStatuses = concurrentResponses; responsesLock.unlock()
    sentLock.lock(); let concurrentTargets = Array(sentTargets.suffix(2)); sentLock.unlock()
    check("concurrent same-key cross-Session sends execute once per target",
          responseStatuses.count == 2 && responseStatuses.allSatisfy { $0 == 200 }
            && concurrentTargets == [session.id, sessionTwo.id])

    RemoteServer.terminalSendForTesting = { text, target in
        sentLock.lock(); sent.append(text); sentTargets.append(target.id); sentLock.unlock()
        return nil
    }

    boardEnabled = false
    workflow.syncMode()
    _ = send("Board OFF stays byte-for-byte", key: UUID().uuidString)
    sentLock.lock(); let offWire = sent.last; sentLock.unlock()
    expect("Board OFF bypasses metadata and sends the original text", offWire,
           "Board OFF stays byte-for-byte")

    boardEnabled = true
    workflow.syncMode()
    RemoteServer.terminalSendForTesting = { _, _ in "fixture terminal rejected the send" }
    let failed = send("do not call this delivered", key: UUID().uuidString)
    expect("terminal rejection stays a terminal rejection", failed?.status, 502)
    let failedBody = failed.flatMap {
        try? JSONSerialization.jsonObject(with: $0.body) as? [String: Any]
    } ?? [:]
    expect("the workflow response records rejected rather than delivered",
           (failedBody["workflow"] as? [String: Any])?["code"] as? String,
           "workflow_delivery_rejected")

    let workflowPath = "/v1/orchestrator/sessions/"
        + session.id.replacingOccurrences(of: "%", with: "%25") + "/workflow"
    let pairedOnly = ProjectBoardWorkflowHTTP.route(
        remoteRequest("GET", workflowPath),
        machine: false, identity: identity, workflow: workflow)
    expect("the receipt route refuses paired-device authority", pairedOnly?.status, 403)
    let forgedBody = try! JSONSerialization.data(withJSONObject: [
        "operation": "begin", "run_id": acceptedRunID, "terminal_id": "caller-choice",
    ], options: [.sortedKeys])
    var forged = remoteRequest(
        "POST", workflowPath,
        headers: ["Idempotency-Key": UUID().uuidString])
    forged.body = forgedBody
    let forgedAnswer = ProjectBoardWorkflowHTTP.route(
        forged, machine: true, identity: identity, workflow: workflow)
    expect("caller-supplied identity fields are refused", forgedAnswer?.status, 400)
}
}
