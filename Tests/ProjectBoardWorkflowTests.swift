import Foundation

private enum WorkflowSyncFixtureError: Error { case refused }

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
            let reply = store.command(body, actor: actor)
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
            "kind": "checklist", "title": "Follow up \(actor)", "owner": actor,
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
}

func runProjectBoardWorkflowTests() {
group("managed Board ingress is durable, bounded and identity-bound") {
    let root = workflowTestDirectory("ingress")
    defer { try? FileManager.default.removeItem(at: root) }
    var enabled = true
    var revision = 7
    let workflow = ProjectBoardWorkflow(
        url: root.appendingPathComponent("workflow.json"),
        boardHeader: { (enabled, revision, true) }, autoStart: false)
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
          Set(operations).isSubset(of: ["create", "checklist", "link", "span", "artifact",
                                       "obligation"]))
    check("no assistant receipt can forge verification or landing",
          !operations.contains("record_evidence") && !operations.contains("transition"))
    check("the output and unresolved obligation reach reconciliation",
          operations.contains("artifact") && operations.contains("obligation"))
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
          migratedText.contains("\"schemaVersion\":2")
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
