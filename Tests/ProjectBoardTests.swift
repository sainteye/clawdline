import Foundation

private final class BoardTestDriver {
    let root: URL
    let file: URL
    let store: ProjectBoardStore
    private var request = 0

    init(name: String = UUID().uuidString) {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-board-\(name)", isDirectory: true)
        file = root.appendingPathComponent("board.json")
        try? FileManager.default.removeItem(at: root)
        store = ProjectBoardStore(url: file)
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
    expect("a historical verification plus a different landing cannot integrate",
           boardError(d.send("transition", ["itemId": item, "state": "integrated"])),
           "evidence_required")
    d.store.ingest(task: [
        "id": "task-right", "workItemId": item, "state": "success",
        "landing": ["state": "landed", "verification_origin": "git",
                    "verified_commit": "commit-a", "verified_target_commit": "target-a"],
    ], projectID: "project-1")
    let landing = (d.item(item)["landings"] as? [[String: Any]])?.first
    expect("matching landing ancestry is labelled broker", landing?["source"] as? String, "broker")
    expect("matching broker-verified landing integrates code",
           d.send("transition", ["itemId": item, "state": "integrated"]).status, 200)
    expect("an integrated item must be explicitly reopened before editing",
           boardError(d.send("update", ["itemId": item, "summary": "new scope"])), "item_locked")
    _ = d.send("transition", ["itemId": item, "state": "execution"])
    _ = d.send("update", ["itemId": item, "summary": "new scope"])
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
    expect("accepted document Task can verify",
           d.send("transition", ["itemId": document, "state": "verified"]).status, 200)
    expect("accepted document Task can deliver without counterfeit Git landing",
           d.send("transition", ["itemId": document, "state": "integrated"]).status, 200)
    expect("integrated work requires explicit reopening before a new obligation",
           boardError(d.send("obligation", ["itemId": document, "title": "late blocker",
                                             "owner": "root", "blocking": true])), "item_locked")
    _ = d.send("transition", ["itemId": document, "state": "execution"])
    let obligationReply = d.send("obligation", ["itemId": document, "title": "notify owner",
                                                   "owner": "root", "blocking": true])
    let obligationID = ((((obligationReply.body["board"] as? [String: Any])?["item"]
        as? [String: Any])?["obligations"] as? [[String: Any]])?.first?["id"] as? String) ?? ""
    _ = d.send("accept_artifact", ["itemId": document, "artifactId": artifactID,
                                    "note": "manager rechecked reopened work"], trusted: true)
    _ = d.send("transition", ["itemId": document, "state": "verified"])
    _ = d.send("transition", ["itemId": document, "state": "integrated"])
    expect("open blocking obligation prevents closure",
           boardError(d.send("transition", ["itemId": document, "state": "closed"])),
           "closure_obligations_open")
    _ = d.send("resolve_obligation", ["itemId": document, "obligationId": obligationID,
                                       "note": "owner notified"])
    expect("settled non-code Task closes",
           d.send("transition", ["itemId": document, "state": "closed"]).status, 200)

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
    expect("the accepted non-code checklist can advance on one artifact subject",
           d.send("transition", ["itemId": renewed, "state": "verified"]).status, 200)
    _ = d.send("transition", ["itemId": renewed, "state": "execution"])
    _ = d.send("update", ["itemId": renewed, "summary": "scope revision two"])
    expect("the same artifact can be renewed after scope invalidation",
           d.send("accept_artifact", ["itemId": renewed, "artifactId": renewedArtifact,
                                       "note": "manager rechecked revised scope"],
                  trusted: true).status, 200)
    expect("artifact renewal keeps both immutable acceptance events",
           (d.item(renewed)["artifactAcceptances"] as? [[String: Any]])?.count, 2)

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
    _ = d.send("transition", ["itemId": nonCodeCurrent, "state": "verified"])
    _ = d.send("transition", ["itemId": nonCodeCurrent, "state": "integrated"])
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
    expect("task success alone leaves lifecycle at backlog", fallback["state"] as? String,
           "backlog")
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
    check("graph fallback remains durable after reattribution", fallbackAfter != nil)

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
    expect("batch replay of conflicting explicit identity is revision-idempotent",
           d.revision, afterConflictRevision)

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
    expect("automatic capacity loss is a typed refusal", overflow.status.rawValue, "refused")
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

    let beforeUnknown = d.revision
    d.store.ingest(task: ["id": "unknown-task", "state": "success"], projectID: "project-1")
    expect("unknown Feature does not become a per-task card", d.revision, beforeUnknown)
    _ = d.send("set_enabled", ["enabled": false])
    let beforeDisabled = boardItems(d.store).count
    d.store.ingest(task: ["id": "disabled-task",
                          "graph": ["id": "graph-disabled", "destination": "No card"]],
                   projectID: "project-1")
    expect("standard mode creates no inferred cards", boardItems(d.store).count, beforeDisabled)
}
}
