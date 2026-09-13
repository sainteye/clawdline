import Foundation

func runRootAssignmentCoordinationTests() {
group("root assignments are a closed durable fourth primitive") {
    Orchestrator.forget()
    let enabled = Config.shared.orchestratorEnabled
    let configuredLanguage = Config.shared.language
    defer {
        Config.shared.language = configuredLanguage
        L.reload()
    }
    Config.shared.orchestratorEnabled = true
    let requestID = UUID().uuidString.lowercased()
    let base: [String: Any] = [
        "request_id": requestID,
        "assistant": "codex",
        "model": "default",
        "project_dir": "/tmp",
        "label": "Launch durable root assignment",
        "assignment": [
            "objective": "Ship the independent feature root.",
            "scope": "The orchestrator and public API.",
            "constraints": "Do not create child lineage.",
            "relevant_references": "docs/orchestrator.md",
            "acceptance": "A restart preserves one stable assignment id."
        ]
    ]
    guard case .ok(let draft) = Orchestrator.rootAssignmentDraft(
        from: base, isDirectory: { $0 == "/tmp" }, canonicalize: { $0 }) else {
        check("the documented closed assignment is accepted", false); return
    }
    expect("the project path is canonical", draft.projectDir, "/tmp")
    expect("default is an explicit stored model", draft.model, "default")
    check("unknown top-level input, including a caller-supplied language, is refused",
          Orchestrator.rootAssignmentDraft(from: base.merging(["task_id": "nope"]) { _, new in new },
              isDirectory: { _ in true }, canonicalize: { $0 }).isBad
          && Orchestrator.rootAssignmentDraft(
              from: base.merging(["language": "en"]) { _, new in new },
              isDirectory: { _ in true }, canonicalize: { $0 }).isBad)
    var widened = base
    var fields = widened["assignment"] as! [String: String]
    fields["open_threads"] = "this would turn the primitive into a handoff"
    widened["assignment"] = fields
    check("unknown envelope input is refused",
          Orchestrator.rootAssignmentDraft(from: widened, isDirectory: { _ in true },
              canonicalize: { $0 }).isBad)
    var oversized = base
    var oversizedFields = oversized["assignment"] as! [String: String]
    oversizedFields["objective"] = String(repeating: "x", count: 8_193)
    oversized["assignment"] = oversizedFields
    check("an oversized assignment field is refused before terminal work",
          Orchestrator.rootAssignmentDraft(from: oversized, isDirectory: { _ in true },
              canonicalize: { $0 }).isBad)

    Config.shared.language = "zh-Hant"
    L.reload()
    let language = Orchestrator.rootAssignmentLanguage()
    let line = Orchestrator.rootAssignmentLine(
        id: "assignment-1", draft: draft, language: language)
    var catalogLanguagesResolve = true
    for configuredTag in L.catalog.map(\.tag) {
        Config.shared.language = configuredTag
        L.reload()
        let resolved = Orchestrator.rootAssignmentLanguage()
        let catalogLine = Orchestrator.rootAssignmentLine(
            id: "assignment-1", draft: draft, language: resolved)
        catalogLanguagesResolve = catalogLanguagesResolve
            && resolved.tag == L.tag(of: L.t) && catalogLine.contains(resolved.name)
    }
    Config.shared.language = "zh-Hant"
    L.reload()
    for heading in ["OBJECTIVE", "SCOPE", "CONSTRAINTS", "RELEVANT REFERENCES", "ACCEPTANCE"] {
        let languageContractIsComplete = language.tag == "zh-Hant"
            && line.contains("Traditional Chinese (繁體中文)")
            && line.contains("commentary") && line.contains("final response")
            && line.contains("all other user-facing communication")
            && line.contains("including the very first response")
            && catalogLanguagesResolve
        check("the launch prompt carries the closed \(heading) field and resolved language contract",
              line.contains(heading) && languageContractIsComplete)
    }
    for forbidden in ["TASK_SECRET", "result.json", "CHILD.md", "handoff.md", "parent_task"] {
        check("the launch prompt excludes child/handoff lifecycle spelling \(forbidden)",
              !line.contains(forbidden))
    }
    let transcript = """
    {"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":\(String(data: try! JSONEncoder().encode(line), encoding: .utf8)!)}]}}}
    """
    check("the exact independent root turn is a delivery receipt",
          Orchestrator.transcriptContainsRootAssignment(transcript, assistant: .codex,
                                                        assignmentID: "assignment-1",
                                                        line: line))
    check("a similar but incomplete first turn is not a delivery receipt",
          !Orchestrator.transcriptContainsRootAssignment(
            transcript.replacingOccurrences(of: "ACCEPTANCE", with: "ACCEPT"),
            assistant: .codex, assignmentID: "assignment-1", line: line))
    let trustScreen = """
    ────────────────────────────────────────────────────────────────
    Accessing workspace:
    /tmp

    Quick safety check: Is this a project you created or one you trust?

     ❯ No, exit
       Yes, I trust this folder

    Enter to confirm · Esc to cancel
    """
    let trustMenu = SessionState.menu(trustScreen, assistant: .claude, hookWaiting: true)
    expect("a fresh project leaves workspace trust to the root owner", Orchestrator.rootAssignmentTrustDecision(projectApproved: false, menu: trustMenu), .block)
    expect("an already approved canonical project may cross the trust picker", Orchestrator.rootAssignmentTrustDecision(projectApproved: true, menu: trustMenu), .accept(row: 2))
    expect("a durable trust receipt prevents a second picker answer", Orchestrator.rootAssignmentTrustDecision(projectApproved: true, menu: trustMenu, answeredTrustMenu: true), .none)
    let openedAt = Date(timeIntervalSince1970: 1_000)
    func step(_ state: Orchestrator.RootAssignmentState, timeout: Bool = false, trust: Orchestrator.RootAssignmentTrustDecision = .none,
              ready: Bool = false, delivery: Orchestrator.RootAssignmentDeliveryEvidence? = nil, attempts: Int = 0) -> Orchestrator.RootAssignmentStepDecision {
        Orchestrator.rootAssignmentStepDecision(state: state, promptTimedOut: timeout, trust: trust, answeredTrustMenu: false, inputReady: ready, delivery: delivery, injectAttempts: attempts)
    }
    check("a composer may remain unready inside the bounded prompt window", !Orchestrator.rootAssignmentPromptTimedOut(state: .terminalOpened, openedAt: openedAt, now: openedAt.addingTimeInterval(239), briefed: false))
    check("a composer that never becomes ready reaches typed timeout work", Orchestrator.rootAssignmentPromptTimedOut(state: .terminalOpened, openedAt: openedAt, now: openedAt.addingTimeInterval(241), briefed: false))
    check("workspace trust has no four-minute expiry while a person owns the picker", !Orchestrator.rootAssignmentPromptTimedOut(state: .blocked, openedAt: openedAt, now: openedAt.addingTimeInterval(24 * 3600), briefed: false))
    expect("answering trust restarts the ordinary pre-brief clock without rewriting terminal time", Orchestrator.rootAssignmentPromptTimeoutAnchor(terminalOpenedAt: openedAt, trustResumedAt: openedAt.addingTimeInterval(24 * 3600)), openedAt.addingTimeInterval(24 * 3600))
    expect("the step seam turns an expired ordinary prompt into typed failure", step(.terminalOpened, timeout: true), .fail("prompt_timeout"))
    expect("the step seam holds an unapproved workspace at the trust boundary", step(.terminalOpened, trust: .block), .block)
    expect("answering trust exposes a prompt-ready transition after the picker leaves", step(.blocked, ready: true), .promptReady)
    expect("a transcript receipt advances the independent root to briefed", step(.promptReady, ready: true, delivery: .init(recorded: true), attempts: 1), .briefed)
    expect("the next observed beat advances a briefed root to active", step(.briefed, ready: true, attempts: 1), .activate)
    expect("the first send needs only an empty composer inside the window", step(.promptReady, ready: true, delivery: .init(recorded: false), attempts: 0), .inject)
    for attempts in [1, 2, Orchestrator.briefingAttemptLimit] {
        expect("a counted attempt (\(attempts)) with no receipt waits instead of sending again", step(.promptReady, ready: true, delivery: .init(recorded: false), attempts: attempts), .wait)
    }
    // The delivery observation false negative, as a fixture. Codex took the prompt four seconds
    // after the tab opened, and stopped being an empty composer at that instant — so the broker
    // first read the record long after the four-minute window had closed. Comparing the deadline
    // against that observation rather than against the user turn wrote failed/prompt_timeout over
    // two Feature Roots that were already dispatching children of their own.
    let deliveredAt = openedAt.addingTimeInterval(4)
    let promptDeadline = Orchestrator.rootAssignmentPromptDeadline(openedAt: openedAt)
    func rollout(recordedAt moment: Date) -> String {
        let stamp = ISO8601DateFormatter()
        stamp.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        stamp.timeZone = TimeZone(identifier: "UTC")
        return """
        {"timestamp":"\(stamp.string(from: moment))","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":\(String(data: try! JSONEncoder().encode(line), encoding: .utf8)!)}]}}}
        """
    }
    func evidence(_ at: Date?, recorded: Bool = true, observed: Bool = false,
                  sendFailed: Bool = false) -> Orchestrator.RootAssignmentDeliveryEvidence {
        .init(recorded: recorded, recordedAt: at, deadline: promptDeadline, observed: observed,
              sendFailed: sendFailed)
    }
    expect("the pre-brief deadline is arithmetic on the anchor the timeout already reads", promptDeadline, openedAt.addingTimeInterval(Orchestrator.readyLimit))
    expect("a delivery receipt carries the user turn's own event time", Orchestrator.rootAssignmentTranscriptReceipt(rollout(recordedAt: deliveredAt), assistant: .codex, assignmentID: "assignment-1", line: line).at, deliveredAt)
    let newerActivity = (1...101).map { index in
        let text = String(data: try! JSONEncoder().encode("newer activity \(index)"), encoding: .utf8)!
        return """
        {"timestamp":"2026-08-31T13:26:19.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"commentary","content":[{"type":"Text","text":\(text)}]}}}
        """
    }.joined(separator: "\n")
    let crowdedRollout = rollout(recordedAt: deliveredAt) + "\n" + newerActivity
    expect("late observation still finds a receipt behind more than one UI transcript window", Orchestrator.rootAssignmentTranscriptReceipt(crowdedRollout, assistant: .codex, assignmentID: "assignment-1", line: line).at, deliveredAt)
    check("a near-miss turn is no receipt and offers no time to compare", Orchestrator.rootAssignmentTranscriptReceipt(rollout(recordedAt: deliveredAt).replacingOccurrences(of: "ACCEPTANCE", with: "ACCEPT"), assistant: .codex, assignmentID: "assignment-1", line: line) == Orchestrator.RootAssignmentTranscriptReceipt(recorded: false, at: nil))
    expect("a prompt the assistant has already taken is still read for a receipt", step(.promptReady, ready: false), .inspectDelivery)
    expect("a delivery inside the window outranks the clock that observed it late", step(.promptReady, timeout: true, ready: false, delivery: evidence(deliveredAt), attempts: 1), .briefed)
    expect("an undated receipt is a delivery whose moment is unknown, not a timeout", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil), attempts: 1), .briefed)
    // The deadline names what the broker knows about the one attempt, not only that time ran out.
    expect("a typed attempt whose record was read without the turn fails as delivery_unconfirmed", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil, recorded: false, observed: true), attempts: 1), .fail("delivery_unconfirmed"))
    expect("a typed attempt whose record could not be read fails as delivery_unobserved", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil, recorded: false), attempts: 1), .fail("delivery_unobserved"))
    expect("an attempt the terminal refused fails as delivery_failed whatever was read", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil, recorded: false, observed: true, sendFailed: true), attempts: 1), .fail("delivery_failed"))
    expect("a prompt-ready record never typed into still reaches typed prompt_timeout", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil, recorded: false, observed: true), attempts: 0), .fail("prompt_timeout"))
    expect("a user turn recorded after the window closed is not a pre-deadline delivery", step(.promptReady, timeout: true, ready: false, delivery: evidence(openedAt.addingTimeInterval(Orchestrator.readyLimit + 1)), attempts: 1), .fail("prompt_timeout"))
    expect("and a late turn outranks the terminal's refusal: the text did arrive", step(.promptReady, timeout: true, ready: false, delivery: evidence(openedAt.addingTimeInterval(Orchestrator.readyLimit + 1), sendFailed: true), attempts: 1), .fail("prompt_timeout"))
    expect("an unconfirmed delivery waits rather than typing into a busy composer", step(.promptReady, ready: false, delivery: evidence(nil, recorded: false), attempts: 1), .wait)
    expect("an empty composer with no receipt is not typed into a second time", step(.promptReady, ready: true, delivery: evidence(nil, recorded: false), attempts: 1), .wait)
    expect("the beat after briefing activates instead of briefing one receipt twice", step(.briefed, ready: false, delivery: evidence(deliveredAt), attempts: 1), .activate)
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Config.shared.orchestratorEnabled = enabled
        Orchestrator.forget()
    }
    var opens = 0
    func refusalCode(_ reply: Orchestrator.Reply) -> String? { if case .refused(_, let code, _, _) = reply { return code }; return nil }
    let mismatchReply = Orchestrator.rootAssignment(
        base, idempotencyKey: UUID().uuidString.lowercased(),
        assistantAvailable: { _ in true }, start: { _, _, _ in
            opens += 1; return .started(id: "%mismatch", backend: .tmux, attach: nil) })
    expect("a mismatched idempotency header has a typed refusal",
           refusalCode(mismatchReply), "idempotency_mismatch")
    let priorEnabled = Config.shared.orchestratorEnabled; Config.shared.orchestratorEnabled = false
    var disabled = base; let disabledID = UUID().uuidString.lowercased(); disabled["request_id"] = disabledID
    let disabledReply = Orchestrator.rootAssignment(
        disabled, idempotencyKey: disabledID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%disabled", backend: .tmux, attach: nil) })
    expect("the shared orchestrator switch has a typed creation refusal", refusalCode(disabledReply), "orchestrator_disabled")
    Config.shared.orchestratorEnabled = priorEnabled
    var persistence = base; let persistenceID = UUID().uuidString.lowercased(); persistence["request_id"] = persistenceID
    Orchestrator.storeSaveInterceptorForTesting = { _ in false }
    let persistenceReply = Orchestrator.rootAssignment(
        persistence, idempotencyKey: persistenceID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%persistence", backend: .tmux, attach: nil) })
    Orchestrator.storeSaveInterceptorForTesting = nil
    expect("a refused durable write is a typed creation failure", refusalCode(persistenceReply), "persistence_failed")
    var unavailable = base; let unavailableID = UUID().uuidString.lowercased(); unavailable["request_id"] = unavailableID
    let unavailableReply = Orchestrator.rootAssignment(
        unavailable, idempotencyKey: unavailableID,
        assistantAvailable: { _ in false }, projectApproved: { _ in false }) { _, _, _ in
            opens += 1
            return .started(id: "%should-not-open", backend: .tmux, attach: nil)
        }
    expect("an unavailable selected assistant has a typed refusal", refusalCode(unavailableReply), "assistant_unavailable")
    expect("assistant availability is checked before opening a terminal", opens, 0)
    // The terminal effect is observed from inside itself: the registry lock must already be
    // free, and the accepted record must already be on disk, before any tab is requested.
    var launchEffectFoundLockFree = false
    var launchEffectFoundDurableAcceptance = false
    let first = Orchestrator.rootAssignment(base, idempotencyKey: requestID,
        assistantAvailable: { _ in true }, projectApproved: { _ in true }) { _, _, _ in
            opens += 1
            if OrchestratorRegistry.lock.try() {
                launchEffectFoundLockFree = true
                OrchestratorRegistry.lock.unlock()
            }
            let onDisk = (try? Data(contentsOf: Orchestrator.storeURL)).flatMap {
                (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
            }
            launchEffectFoundDurableAcceptance =
                (onDisk?["root_assignments"] as? [[String: Any]] ?? []).contains {
                    $0["request_id"] as? String == requestID
                        && $0["state"] as? String == "accepted"
                }
            return .started(id: "%feature-root", backend: .tmux, attach: nil)
        }
    check("the tab is requested outside the registry lock", launchEffectFoundLockFree)
    check("and only after the accepted assignment reached disk",
          launchEffectFoundDurableAcceptance)
    guard case .ok(let firstPayload) = first,
          let firstRecord = firstPayload["root_assignment"] as? [String: Any],
          let stableID = firstRecord["id"] as? String else {
        check("the assignment is durably accepted", false); return
    }
    expect("acceptance opens exactly one ordinary root tab", opens, 1)
    expect("its first durable state names the opened terminal",
           firstRecord["state"] as? String, "terminal_opened")
    let acceptedAssignment = Orchestrator.rootAssignmentForTesting(stableID)
    let acceptedLine = acceptedAssignment.map { Orchestrator.rootAssignmentLine(for: $0) }
    Config.shared.language = "en"
    L.reload()
    let replay = Orchestrator.rootAssignment(base, idempotencyKey: requestID,
        assistantAvailable: { _ in true }, projectApproved: { _ in true }) { _, _, _ in
            opens += 1
            return .started(id: "%duplicate", backend: .tmux, attach: nil)
        }
    check("a later language setting cannot conflict with or reopen an accepted request",
          { if case .ok = replay { return opens == 1 }; return false }())
    var conflict = base; conflict["label"] = "Different content under one request"
    let conflictReply = Orchestrator.rootAssignment(
        conflict, idempotencyKey: requestID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%conflict", backend: .tmux, attach: nil) })
    expect("different content under one request id is a typed conflict", refusalCode(conflictReply), "request_conflict")
    var terminalFailure = base; let terminalFailureID = UUID().uuidString.lowercased(); terminalFailure["request_id"] = terminalFailureID
    let refusedOpen = Orchestrator.rootAssignment(
        terminalFailure, idempotencyKey: terminalFailureID,
        assistantAvailable: { _ in true }, start: { _, _, _ in
            .refused(status: 409, code: "terminal_closed", message: "closed", app: "iTerm2") })
    expect("terminal opening failure remains typed", refusalCode(refusedOpen), "terminal_closed")
    var replayStarts = 0
    let refusedReplay = Orchestrator.rootAssignment(
        terminalFailure, idempotencyKey: terminalFailureID,
        assistantAvailable: { _ in true }, start: { _, _, _ in
            replayStarts += 1; return .started(id: "%unsafe-retry", backend: .tmux, attach: nil) })
    expect("a terminal assignment replay requires a new request id", refusalCode(refusedReplay), "request_terminated")
    expect("terminal replay cannot risk a duplicate tab", replayStarts, 0)
    let assigningRoot = TargetSession(
        backend: .tmux, id: "%assigner", name: "assigner", tty: "/dev/null",
        windowIndex: 0, tabIndex: 0, assistant: .codex)
    _ = Orchestrator.cancelChildren(ofRoot: assigningRoot)
    expect("closing the assigning Root cannot cancel its independent Feature Root",
           Orchestrator.rootAssignmentRecord(id: stableID)?["state"] as? String,
           "terminal_opened")
    Orchestrator.forget(); Orchestrator.load(force: true)
    let restartedAssignment = Orchestrator.rootAssignmentForTesting(stableID)
    check("the stable assignment and exact briefing bytes survive restart and config change",
          Orchestrator.rootAssignmentRecord(id: stableID)?["request_id"] as? String == requestID
          && restartedAssignment?.language?.tag == "zh-Hant"
          && restartedAssignment?.language?.name == "Traditional Chinese (繁體中文)"
          && restartedAssignment.map { Orchestrator.rootAssignmentLine(for: $0) } == acceptedLine)
    Orchestrator.resumeAfterRestart()
    expect("restart fails closed when no exact process tuple had reached the durable receipt",
           Orchestrator.rootAssignmentRecord(id: stableID)?["state"] as? String, "failed")
    let restartFailure = Orchestrator.rootAssignmentRecord(id: stableID)?["failure"]
        as? [String: Any]
    expect("the typed restart failure names missing identity rather than guessing a reused tab",
           restartFailure?["code"] as? String, "restart_identity_incomplete")
    check("an independently owned root never enters the child Role index",
          Orchestrator.role(forTerminal: "%feature-root") == nil)
    let publicKeys = Orchestrator.rootAssignmentRecord(id: stableID)
        .map { Set($0.keys) } ?? Set<String>()
    check("the public row has no task, handoff, detached or result lifecycle fields",
          publicKeys.isDisjoint(with: [
              "task_id", "parent_task", "handoff_id", "detached", "result", "secret_hash"
          ]))
    let machine = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let anonymousList = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/root-assignments"))
    expect("assignment inventory is not anonymously readable", anonymousList.status, 401)
    let phone = RemoteAuth.addDevice(name: "paired but not machine", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let pairedHeaders = ["Authorization": "Bearer \(phone.token)",
                         "Idempotency-Key": requestID]
    let pairedList = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/root-assignments", headers: pairedHeaders))
    expect("a paired device still cannot enumerate assignments", pairedList.status, 403)
    expect("the list refusal names the machine credential boundary",
           remoteErrorCode(pairedList), "forbidden")
    let list = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/root-assignments", headers: machine))
    expect("machine auth reads the durable inventory", list.status, 200)
    let one = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/root-assignments/\(stableID)", headers: machine))
    expect("machine auth reads one stable assignment", one.status, 200)
    check("launch enters the bounded terminal-worker lane",
          RemoteServer.isOrchestratorTerminalWorkerRoute(
            "/v1/orchestrator/root-assignments"))
    let requestData = try! JSONSerialization.data(withJSONObject: base)
    let requestBody = String(data: requestData, encoding: .utf8)!
    let pairedLaunch = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/root-assignments", headers: pairedHeaders,
        body: requestBody))
    expect("a paired device cannot launch an independent root", pairedLaunch.status, 403)
    expect("the launch refusal names the machine credential boundary",
           remoteErrorCode(pairedLaunch), "forbidden")
    let queued = remoteRequest("POST", "/v1/orchestrator/root-assignments",
        headers: ["Idempotency-Key": requestID],
        body: requestBody)
    expect("duplicate launch attempts serialize on the durable request identity", RemoteServer.terminalChannelsForTesting(queued), ["root-assignment:\(requestID)"])
    let exact = Orchestrator.RootAssignmentIdentity(
        terminalID: "%feature-root", assistant: .codex, tty: "/dev/ttys001",
        pid: 100, processStart: 200, conversationID: "conversation-one")
    expect("one exact process/conversation tuple may be rebound after restart",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [exact], inventoryComplete: true,
              absenceConfirmed: false, delivered: false), .rebind(exact))
    let second = Orchestrator.RootAssignmentIdentity(
        terminalID: "%other", assistant: .codex, tty: "/dev/ttys002",
        pid: 100, processStart: 200, conversationID: "conversation-one")
    expect("two exact candidates fail closed instead of guessing",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [exact, second], inventoryComplete: true,
              absenceConfirmed: false, delivered: false), .fail("ambiguous_identity"))
    expect("a changed terminal id is adopted only from the same exact process/conversation",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [second], inventoryComplete: true,
              absenceConfirmed: false, delivered: false), .rebind(second))
    expect("a stale inventory cannot manufacture process loss",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [], inventoryComplete: false,
              absenceConfirmed: true, delivered: false), .wait("stale_inventory"))
    expect("confirmed loss before briefing is a launch failure",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [], inventoryComplete: true,
              absenceConfirmed: true, delivered: false),
           .fail("process_lost_before_briefing"))
    expect("confirmed loss after briefing leaves an inactive independent Root record",
           Orchestrator.rootAssignmentReconciliation(
              stored: exact, candidates: [], inventoryComplete: true,
              absenceConfirmed: true, delivered: true),
           .inactive("process_lost_after_briefing"))
    let partial = Orchestrator.RootAssignmentIdentity(terminalID: "%opening", assistant: .codex, tty: nil, pid: nil, processStart: nil, conversationID: nil)
    let adopted = Orchestrator.RootAssignmentIdentity(terminalID: "%opening", assistant: .codex, tty: "/dev/ttys003", pid: 300, processStart: 400, conversationID: "conversation-two")
    expect("an incomplete inventory cannot settle an opening identity", Orchestrator.rootAssignmentInitialIdentityReconciliation(candidates: [adopted], inventoryComplete: false, absenceConfirmed: true), .wait("stale_inventory"))
    expect("one complete exact terminal observation seeds process identity", Orchestrator.rootAssignmentInitialIdentityReconciliation(candidates: [adopted], inventoryComplete: true, absenceConfirmed: false), .rebind(adopted))
    expect("two opening identities fail closed", Orchestrator.rootAssignmentInitialIdentityReconciliation(candidates: [adopted, partial], inventoryComplete: true, absenceConfirmed: false), .fail("ambiguous_identity"))
    expect("one missing observation cannot settle pre-brief loss", Orchestrator.rootAssignmentInitialIdentityReconciliation(candidates: [], inventoryComplete: true, absenceConfirmed: false), .wait("process_missing_unconfirmed"))
    expect("two complete absence observations settle pre-brief loss", Orchestrator.rootAssignmentInitialIdentityReconciliation(candidates: [], inventoryComplete: true, absenceConfirmed: true), .fail("process_lost_before_briefing"))
    func assignmentFixture(id: String = UUID().uuidString.lowercased(), state: Orchestrator.RootAssignmentState = .active) -> Orchestrator.RootAssignment {
        var row = Orchestrator.RootAssignment(id: id, requestID: UUID().uuidString.lowercased(), requestDigest: String(repeating: "a", count: 64), assistant: .codex,
            model: "default", projectDir: "/tmp", label: "Exact Feature Root", objective: "objective", scope: "scope", constraints: "constraints",
            relevantReferences: "references", acceptance: "acceptance", projectApproved: false,
            created: openedAt, state: state, language: nil)
        row.identity = exact; return row
    }
    let projected = Orchestrator.rootAssignmentSessionProjection(assignments: [assignmentFixture()], identity: .init(terminalID: exact.terminalID, assistant: .codex,
        tty: exact.tty ?? "", pid: exact.pid, processStart: exact.processStart.map(Date.init(timeIntervalSince1970:)), conversationID: exact.conversationID))
    check("the paired-device Session projection is an exact five-field allowlist", projected.map { Set($0.keys) } == Set(["id", "label", "state", "ownership", "explanation"]))
    check("ambiguous assignment identity produces no Session projection", Orchestrator.rootAssignmentSessionProjection(assignments: [assignmentFixture(), assignmentFixture()], identity: .init(terminalID: exact.terminalID, assistant: .codex, tty: exact.tty ?? "", pid: exact.pid, processStart: exact.processStart.map(Date.init(timeIntervalSince1970:)), conversationID: exact.conversationID)) == nil)
    let cleanupRows = (0..<201).map { Orchestrator.RootAssignmentCleanupCandidate(id: "assignment-\($0)", state: .failed, created: Date(timeIntervalSince1970: Double($0))) }
    expect("cleanup retains the newest 200 terminal assignment records", Orchestrator.rootAssignmentCleanupIDs(cleanupRows), ["assignment-0"])
    var answered = assignmentFixture(state: .blocked)
    answered.language = Orchestrator.RootAssignmentLanguage(
        tag: "zh-Hant", name: "Traditional Chinese (繁體中文)")
    answered.answeredTrustMenu = true
    let roundTrip = OrchestratorStore.rootAssignment(from: OrchestratorStore.stored(answered))
    var legacyStored = OrchestratorStore.stored(assignmentFixture(state: .promptReady))
    legacyStored.removeValue(forKey: "language")
    let legacy = OrchestratorStore.rootAssignment(from: legacyStored)
    let legacyLine = legacy.map { Orchestrator.rootAssignmentLine(for: $0) } ?? ""
    let legacyTranscript = """
    {"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":\(String(data: try! JSONEncoder().encode(legacyLine), encoding: .utf8)!)}]}}}
    """
    let legacyReceipt = Orchestrator.rootAssignmentTranscriptReceipt(
        legacyTranscript, assistant: .codex, assignmentID: legacy?.id ?? "", line: legacyLine)
    check("durable language round-trips while a delivered legacy row keeps old bytes",
          roundTrip?.answeredTrustMenu == true && roundTrip?.language == answered.language
          && roundTrip.map { Orchestrator.rootAssignmentLine(for: $0) }
              == Optional(Orchestrator.rootAssignmentLine(for: answered))
          && legacy?.language == nil && !legacyLine.contains("LANGUAGE CONTRACT")
          && step(.promptReady, ready: true,
                  delivery: .init(recorded: legacyReceipt.recorded), attempts: 1) == .briefed)
    var audited = assignmentFixture(state: .blocked); audited.blocker = "workspace_trust_required"
    Orchestrator.holdRootAssignmentForTesting(audited)
    var notices: [(String, [String: String])] = []
    var auditFoundLockFree = true
    Orchestrator.rootAssignmentAuditObserverForTesting = { event, fields in
        if OrchestratorRegistry.lock.try() { OrchestratorRegistry.lock.unlock() }
        else { auditFoundLockFree = false }
        notices.append((event, fields))
    }
    // Failure injection: a receipt that cannot reach disk must neither announce itself nor stay
    // behind in memory, or the next beat would believe the operator had already been told.
    Orchestrator.storeSaveInterceptorForTesting = { _ in false }
    let refusedReport = Orchestrator.reportRootAssignmentTransition(audited.id)
    Orchestrator.storeSaveInterceptorForTesting = nil
    check("a transition receipt whose save is refused emits no audit",
          !refusedReport && notices.isEmpty)
    check("and is rolled back to the receipt it replaced",
          Orchestrator.rootAssignmentForTesting(audited.id)?.reportedTransition == nil)
    check("the blocked transition emits one persisted operator-visible audit", Orchestrator.reportRootAssignmentTransition(audited.id))
    check("the audit is emitted after the registry lock is released",
          auditFoundLockFree && notices.count == 1)
    check("the durable transition receipt suppresses a repeat beat", !Orchestrator.reportRootAssignmentTransition(audited.id))
    expect("the audit names the typed blocked event", notices.first?.0, "root_assignment.blocked")
    expect("the audit names the exact terminal identity", notices.first?.1["terminal"], exact.terminalID)
    expect("a failed transition has the typed failure audit event", Orchestrator.rootAssignmentTransitionNotice(state: .failed, blocker: nil, failure: "prompt_timeout")?.event, "root_assignment.failed")
    expect("an inactive transition has its own typed audit event", Orchestrator.rootAssignmentTransitionNotice(state: .inactive, blocker: nil, failure: "process_lost_after_briefing")?.event, "root_assignment.inactive")
    Orchestrator.saveForTesting(); Orchestrator.forget(); Orchestrator.load(force: true)
    var restartNotices = 0
    Orchestrator.rootAssignmentAuditObserverForTesting = { _, _ in restartNotices += 1 }
    check("restart recovery cannot re-emit an audited transition", !Orchestrator.reportRootAssignmentTransition(audited.id) && restartNotices == 0)
    var timedOut = assignmentFixture(id: UUID().uuidString.lowercased(), state: .failed)
    timedOut.failure = "prompt_timeout"
    Orchestrator.holdRootAssignmentForTesting(timedOut)
    var timeoutNotices: [(String, [String: String])] = []
    Orchestrator.rootAssignmentAuditObserverForTesting = { timeoutNotices.append(($0, $1)) }
    check("a prompt that genuinely never arrived writes one durable failure transition", Orchestrator.reportRootAssignmentTransition(timedOut.id))
    check("a repeated beat on the same timeout adds no second audit event", !Orchestrator.reportRootAssignmentTransition(timedOut.id))
    expect("the timeout audit is emitted exactly once", timeoutNotices.map(\.0), ["root_assignment.failed"])
    expect("and it names the typed reason rather than a guessed title", timeoutNotices.first?.1["why"], "prompt_timeout")
    let labelled = assignmentFixture(id: UUID().uuidString.lowercased()); Orchestrator.holdRootAssignmentForTesting(labelled)
    let exactObserved = Orchestrator.SessionWorkIdentity(terminalID: exact.terminalID, assistant: .codex, tty: exact.tty ?? "", pid: exact.pid,
        processStart: exact.processStart.map(Date.init(timeIntervalSince1970:)), conversationID: exact.conversationID)
    Orchestrator.pruneClosedHandoffTitles(visible: [exact.terminalID], identities: [exactObserved])
    expect("an exactly visible Feature Root keeps its assignment label", Orchestrator.title(forTerminal: exact.terminalID), labelled.label)
    var reused = exactObserved; reused.pid = 999
    Orchestrator.pruneClosedHandoffTitles(visible: [exact.terminalID], identities: [reused])
    let reuseForgot = Orchestrator.title(forTerminal: exact.terminalID) == nil; Orchestrator.pruneClosedHandoffTitles(visible: [exact.terminalID], identities: [exactObserved])
    Orchestrator.pruneClosedHandoffTitles(visible: [], identities: [])
    check("a closed or reused terminal id cannot inherit a stale assignment label", reuseForgot && Orchestrator.title(forTerminal: exact.terminalID) == nil)
    while Orchestrator.takeDispatchRate() != nil {}
    var limited = base; let limitedID = UUID().uuidString.lowercased(); limited["request_id"] = limitedID
    let limitedReply = Orchestrator.rootAssignment(limited, idempotencyKey: limitedID, assistantAvailable: { _ in true }, start: { _, _, _ in .started(id: "%rate-limited", backend: .tmux, attach: nil) })
    expect("the shared launch brake has a typed creation refusal", refusalCode(limitedReply), "rate_limited")

    // Delivery on the production path. `rootAssignmentDelivery` is the half of every beat that
    // reads the record, decides, writes the durable receipt and types. Here its terminal is a
    // counted sender and its transcript is exactly what the record held at that beat.
    let incidentOpenedAt = Date(timeIntervalSince1970: 1_789_284_598.684)
    func jsonRow(_ fields: [String: Any]) -> String {
        String(data: try! JSONSerialization.data(withJSONObject: fields), encoding: .utf8)!
    }
    func stamp(_ seconds: TimeInterval) -> String {
        let format = ISO8601DateFormatter()
        format.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return format.string(from: incidentOpenedAt.addingTimeInterval(seconds))
    }
    func codexTurn(_ text: String, after seconds: TimeInterval) -> String {
        let item: [String: Any] = ["type": "UserMessage",
                                   "content": [["type": "text", "text": text]]]
        let payload: [String: Any] = ["type": "item_completed", "item": item]
        return jsonRow(["timestamp": stamp(seconds), "type": "event_msg", "payload": payload])
    }
    let plainFields = ["objective", "scope", "constraints", "references", "acceptance"]
    func heldDelivery(_ assistant: Assistant, fields: [String], attempts: Int = 0,
                      lastInjectAt: Date? = nil) -> Orchestrator.RootAssignment {
        var row = Orchestrator.RootAssignment(
            id: UUID().uuidString.lowercased(), requestID: UUID().uuidString.lowercased(),
            requestDigest: String(repeating: "b", count: 64), assistant: assistant, model: "opus",
            projectDir: "/tmp", label: "Delivered Feature Root", objective: fields[0],
            scope: fields[1], constraints: fields[2], relevantReferences: fields[3],
            acceptance: fields[4], projectApproved: false, created: incidentOpenedAt,
            state: .promptReady,
            language: Orchestrator.RootAssignmentLanguage(
                tag: "zh-Hant", name: "Traditional Chinese (繁體中文)"))
        row.identity = Orchestrator.RootAssignmentIdentity(
            terminalID: "%delivery-\(row.id.prefix(8))", assistant: assistant,
            tty: "/dev/ttys015", pid: 51_037, processStart: 1_789_284_602,
            conversationID: UUID().uuidString.lowercased())
        row.terminalOpenedAt = incidentOpenedAt
        row.promptReadyAt = incidentOpenedAt.addingTimeInterval(9)
        row.injectAttempts = attempts
        row.lastInjectAt = lastInjectAt
        Orchestrator.holdRootAssignmentForTesting(row)
        return row
    }
    var typed: [String] = []
    func deliveryBeat(_ id: String, after seconds: TimeInterval, record: String?,
                      snapshot: Orchestrator.RootAssignment? = nil,
                      refusal: String? = nil) -> Bool {
        guard let current = snapshot ?? Orchestrator.rootAssignmentForTesting(id) else {
            return false
        }
        return Orchestrator.rootAssignmentDelivery(
            current, now: incidentOpenedAt.addingTimeInterval(seconds), inputReady: true,
            transcripts: { offer in if let record { _ = offer(record) } },
            send: { typed.append($0); return refusal })
    }

    // (a) Root Assignment 8cd9479d on 2026-09-13, reduced to its shape. Every field its caller
    // wrote ended in a newline, so the briefing did; Claude recorded the turn byte for byte as a
    // string `message.content` (jsonl line 6), and the trimmed entry the reader returns could
    // never contain the untrimmed line. The Root ended that first turn at 07:33:32.122Z with an
    // empty composer, and at 07:33:33.670Z the broker typed the whole briefing again (line 195).
    let incidentFields = [
        "讓 Cloud 的錯誤與認證問題對使用者和除錯的 agent 都透明。\n", "先規劃再實作。\n",
        "共用工作樹：只 stage 自己的檔案。\n", "Clawdline.log 只寫 status 與 code。\n",
        "畫面或診斷讀取都能說出層級、代碼與 request id。\n"]
    let incident = heldDelivery(.claude, fields: incidentFields, attempts: 1,
                                lastInjectAt: incidentOpenedAt.addingTimeInterval(9.6))
    let incidentLine = Orchestrator.rootAssignmentLine(for: incident)
    let incidentRecord = [
        jsonRow(["parentUuid": NSNull(), "isSidechain": false,
                 "promptId": "e90d1db8-8046-4fce-bc2b-348738a10f99", "type": "user",
                 "uuid": "f6b8faf6-80a2-4703-bce5-c15297f04bbd",
                 "timestamp": "2026-09-13T07:30:08.509Z", "permissionMode": "auto",
                 "origin": ["kind": "human"], "promptSource": "typed", "userType": "external",
                 "entrypoint": "cli", "cwd": "/Users/sainteye/code/clawdline",
                 "sessionId": incident.identity?.conversationID ?? "", "version": "2.1.270",
                 "gitBranch": "main", "message": ["role": "user", "content": incidentLine]]),
        jsonRow(["type": "assistant", "timestamp": "2026-09-13T07:33:32.122Z",
                 "isSidechain": false, "message": [
                    "role": "assistant", "stop_reason": "end_turn",
                    "content": [["type": "text", "text": "三路盤點都還在背景跑。"]]] as [String: Any]]),
        jsonRow(["type": "system", "subtype": "turn_duration",
                 "timestamp": "2026-09-13T07:33:32.303Z"]),
    ].joined(separator: "\n") + "\n"
    check("(a) the incident fixture keeps what defeated the receipt: a line ending in a newline",
          incidentLine.hasSuffix("\n"))
    typed = []
    let incidentBeat = deliveryBeat(incident.id, after: 214.986, record: incidentRecord)
    let incidentAfter = Orchestrator.rootAssignmentForTesting(incident.id)
    check("(a) a delivered briefing ending in a newline is briefed on the delivery path",
          incidentBeat && incidentAfter?.state == .briefed)
    check("(a) and the Root that finished its first turn is not typed the briefing again",
          typed.isEmpty && incidentAfter?.injectAttempts == 1)
    let codexIncident = heldDelivery(.codex, fields: incidentFields)
    let codexLine = Orchestrator.rootAssignmentLine(for: codexIncident)
    check("(a) a Codex rollout of the same newline-ended briefing is a receipt as well",
          Orchestrator.rootAssignmentTranscriptReceipt(
            codexTurn(codexLine, after: 10), assistant: .codex,
            assignmentID: codexIncident.id, line: codexLine).recorded)
    // The same class, a different byte: the reader also takes a dropped screenshot's path out of
    // a user turn, and a references field is exactly where somebody cites one.
    let screenshot = Drop.directory.path + "/clawdline-\(UUID().uuidString).png"
    let cited = heldDelivery(.claude, fields: [
        "objective", "scope", "constraints", "The refusal is in \(screenshot) today.", "acceptance"])
    let citedLine = Orchestrator.rootAssignmentLine(for: cited)
    check("a briefing citing a dropped screenshot is still its own receipt",
          Orchestrator.rootAssignmentTranscriptReceipt(
            jsonRow(["type": "user", "timestamp": stamp(10),
                     "message": ["role": "user", "content": citedLine]]),
            assistant: .claude, assignmentID: cited.id, line: citedLine).recorded)

    // (b) A receipt that lands after the beats that looked for it. Plain fields, so only the send
    // rule is under test: every beat before the row lands sees an empty composer and no receipt,
    // which the old retry read as a lost prompt once fifteen seconds had passed.
    let late = heldDelivery(.codex, fields: plainFields)
    let lateLine = Orchestrator.rootAssignmentLine(for: late)
    let lateMeta = jsonRow(["timestamp": stamp(9), "type": "session_meta",
                            "payload": ["id": late.identity?.conversationID ?? "", "cwd": "/tmp"]])
    typed = []
    _ = deliveryBeat(late.id, after: 9, record: lateMeta)
    expect("(b) the first beat with an empty composer types the briefing once", typed, [lateLine])
    expect("(b) and has counted that one attempt durably",
           Orchestrator.rootAssignmentForTesting(late.id)?.injectAttempts, 1)
    for seconds in [30.0, 60, 215] { _ = deliveryBeat(late.id, after: seconds, record: lateMeta) }
    expect("(b) beats that observe before the receipt lands never type it again", typed.count, 1)
    _ = deliveryBeat(late.id, after: 220, record: lateMeta + "\n" + codexTurn(lateLine, after: 10))
    expect("(b) the receipt that lands later still briefs the Root",
           Orchestrator.rootAssignmentForTesting(late.id)?.state, .briefed)
    expect("(b) with exactly one send in total", typed.count, 1)

    // (c) A briefing that genuinely never arrived.
    let lost = heldDelivery(.claude, fields: plainFields)
    let lostRecord = jsonRow(["type": "system", "subtype": "turn_duration", "timestamp": stamp(5)])
    typed = []
    for seconds in [9.0, 30, 120, 239] { _ = deliveryBeat(lost.id, after: seconds, record: lostRecord) }
    expect("(c) a briefing that never arrives is typed once and not again inside the window",
           typed.count, 1)
    let expired = deliveryBeat(lost.id, after: 241, record: lostRecord)
    let lostAfter = Orchestrator.rootAssignmentForTesting(lost.id)
    check("(c) past the deadline, with the record read and no receipt, it is delivery_unconfirmed",
          expired && lostAfter?.state == .failed && lostAfter?.failure == "delivery_unconfirmed")
    _ = deliveryBeat(lost.id, after: 300, record: lostRecord)
    expect("(c) and neither the failure nor a later beat sends it again", typed.count, 1)

    // A beat that began from a snapshot taken before another beat counted its attempt: the
    // durable count, not the snapshot, decides whether anything may be typed.
    let raced = heldDelivery(.claude, fields: plainFields)
    typed = []
    _ = deliveryBeat(raced.id, after: 9, record: nil)
    _ = deliveryBeat(raced.id, after: 30, record: nil, snapshot: raced)
    expect("a beat holding a snapshot from before the counted attempt cannot type again",
           typed.count, 1)

    // (d) A send the terminal refuses. `Tmux.send` can refuse after the paste or before the Enter
    // lands, so the refusal is still the one attempt and never a licence to type again — but it
    // is the broker's own knowledge, so it has to reach the disk, the audit and the deadline.
    let refused = heldDelivery(.claude, fields: plainFields)
    var refusalNotices: [(String, [String: String])] = []
    Orchestrator.rootAssignmentAuditObserverForTesting = { refusalNotices.append(($0, $1)) }
    typed = []
    _ = deliveryBeat(refused.id, after: 9, record: lostRecord, refusal: "that tmux pane is gone")
    _ = Orchestrator.load(force: true)
    let refusedAfter = Orchestrator.rootAssignmentForTesting(refused.id)
    check("(d) a refused send is kept on the durable record with the terminal's error",
          refusedAfter?.injectFailure == "that tmux pane is gone"
              && refusedAfter?.injectAttempts == 1 && typed.count == 1)
    expect("(d) and audited when it happens", refusalNotices.map { [$0.0, $0.1["error"] ?? ""] },
           [["root_assignment.inject_failed", "that tmux pane is gone"]])
    for seconds in [30.0, 120, 239] { _ = deliveryBeat(refused.id, after: seconds, record: lostRecord) }
    check("(d) the refusal neither ends the record early nor sends it again",
          typed.count == 1 && Orchestrator.rootAssignmentForTesting(refused.id)?.state == .promptReady)
    _ = deliveryBeat(refused.id, after: 241, record: lostRecord)
    expect("(d) at the deadline the typed failure says the terminal refused it",
           Orchestrator.rootAssignmentForTesting(refused.id)?.failure, "delivery_failed")
    expect("(d) and the failure audit carries the terminal's error",
           refusalNotices.last.map { [$0.0, $0.1["inject_failure"] ?? ""] },
           ["root_assignment.failed", "that tmux pane is gone"])
    let refusedButArrived = heldDelivery(.codex, fields: plainFields)
    _ = deliveryBeat(refusedButArrived.id, after: 9, record: nil,
                     refusal: "tmux typed the line but Enter did not land.")
    _ = deliveryBeat(refusedButArrived.id, after: 250, record: codexTurn(
        Orchestrator.rootAssignmentLine(for: refusedButArrived), after: 10))
    expect("(d) a refused send whose turn was recorded inside the window is still briefed",
           Orchestrator.rootAssignmentForTesting(refusedButArrived.id)?.state, .briefed)

    // (e) A typed attempt whose conversation record the broker could never read.
    let unread = heldDelivery(.claude, fields: plainFields)
    for seconds in [9.0, 241] { _ = deliveryBeat(unread.id, after: seconds, record: nil) }
    expect("(e) an unrefused attempt with no readable record fails as delivery_unobserved",
           Orchestrator.rootAssignmentForTesting(unread.id)?.failure, "delivery_unobserved")

    // (f) cleanup() answers a refused save with load(force: true), which installs whatever the
    // store holds. Landing between a beat's count and that beat's save, it hands the save the
    // image from before the count, which the save writes; typing then would leave the next beat
    // an uncounted record and an empty composer to type the same briefing into again.
    let reloaded = heldDelivery(.claude, fields: plainFields)
    Orchestrator.saveForTesting()
    typed = []
    Orchestrator.rootAssignmentInjectionCountedForTesting = { _ in _ = Orchestrator.load(force: true) }
    _ = deliveryBeat(reloaded.id, after: 9, record: nil)
    Orchestrator.rootAssignmentInjectionCountedForTesting = nil
    expect("(f) a reload between the count and its save leaves that beat typing nothing",
           typed.count, 0)
    for seconds in [30.0, 60, 120] { _ = deliveryBeat(reloaded.id, after: seconds, record: nil) }
    expect("(f) so across the beats that follow the briefing is typed exactly once",
           typed.count, 1)
}
}
