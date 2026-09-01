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
    expect("a transcript receipt advances the independent root to briefed", step(.promptReady, ready: true, delivery: .init(transcriptKnown: true, recorded: true, retryDelayElapsed: true), attempts: 1), .briefed)
    expect("the next observed beat advances a briefed root to active", step(.briefed, ready: true, attempts: 1), .activate)
    expect("the retry ceiling produces delivery_unconfirmed instead of another send", step(.promptReady, ready: true, delivery: .init(transcriptKnown: true, recorded: false, retryDelayElapsed: true), attempts: Orchestrator.briefingAttemptLimit), .fail("delivery_unconfirmed"))
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
    func evidence(_ at: Date?, recorded: Bool = true, known: Bool = true, retried: Bool = true) -> Orchestrator.RootAssignmentDeliveryEvidence {
        .init(transcriptKnown: known, recorded: recorded, recordedAt: at, deadline: promptDeadline, retryDelayElapsed: retried)
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
    expect("a record that never carried the turn still reaches typed prompt_timeout", step(.promptReady, timeout: true, ready: false, delivery: evidence(nil, recorded: false), attempts: 1), .fail("prompt_timeout"))
    expect("a user turn recorded after the window closed is not a pre-deadline delivery", step(.promptReady, timeout: true, ready: false, delivery: evidence(openedAt.addingTimeInterval(Orchestrator.readyLimit + 1)), attempts: 1), .fail("prompt_timeout"))
    expect("an unconfirmed delivery waits rather than typing into a busy composer", step(.promptReady, ready: false, delivery: evidence(nil, recorded: false), attempts: 1), .wait)
    expect("an empty composer with no receipt still retries the ordinary way", step(.promptReady, ready: true, delivery: evidence(nil, recorded: false), attempts: 1), .inject)
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
            opens += 1; return .started(id: "%mismatch", backend: .tmux) })
    expect("a mismatched idempotency header has a typed refusal",
           refusalCode(mismatchReply), "idempotency_mismatch")
    let priorEnabled = Config.shared.orchestratorEnabled; Config.shared.orchestratorEnabled = false
    var disabled = base; let disabledID = UUID().uuidString.lowercased(); disabled["request_id"] = disabledID
    let disabledReply = Orchestrator.rootAssignment(
        disabled, idempotencyKey: disabledID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%disabled", backend: .tmux) })
    expect("the shared orchestrator switch has a typed creation refusal", refusalCode(disabledReply), "orchestrator_disabled")
    Config.shared.orchestratorEnabled = priorEnabled
    var persistence = base; let persistenceID = UUID().uuidString.lowercased(); persistence["request_id"] = persistenceID
    Orchestrator.storeSaveInterceptorForTesting = { _ in false }
    let persistenceReply = Orchestrator.rootAssignment(
        persistence, idempotencyKey: persistenceID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%persistence", backend: .tmux) })
    Orchestrator.storeSaveInterceptorForTesting = nil
    expect("a refused durable write is a typed creation failure", refusalCode(persistenceReply), "persistence_failed")
    var unavailable = base; let unavailableID = UUID().uuidString.lowercased(); unavailable["request_id"] = unavailableID
    let unavailableReply = Orchestrator.rootAssignment(
        unavailable, idempotencyKey: unavailableID,
        assistantAvailable: { _ in false }, projectApproved: { _ in false }) { _, _, _ in
            opens += 1
            return .started(id: "%should-not-open", backend: .tmux)
        }
    expect("an unavailable selected assistant has a typed refusal", refusalCode(unavailableReply), "assistant_unavailable")
    expect("assistant availability is checked before opening a terminal", opens, 0)
    let first = Orchestrator.rootAssignment(base, idempotencyKey: requestID,
        assistantAvailable: { _ in true }, projectApproved: { _ in true }) { _, _, _ in
            opens += 1
            return .started(id: "%feature-root", backend: .tmux)
        }
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
            return .started(id: "%duplicate", backend: .tmux)
        }
    check("a later language setting cannot conflict with or reopen an accepted request",
          { if case .ok = replay { return opens == 1 }; return false }())
    var conflict = base; conflict["label"] = "Different content under one request"
    let conflictReply = Orchestrator.rootAssignment(
        conflict, idempotencyKey: requestID, assistantAvailable: { _ in true },
        start: { _, _, _ in opens += 1; return .started(id: "%conflict", backend: .tmux) })
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
            replayStarts += 1; return .started(id: "%unsafe-retry", backend: .tmux) })
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
    let roundTrip = Orchestrator.rootAssignment(from: Orchestrator.stored(answered))
    var legacyStored = Orchestrator.stored(assignmentFixture(state: .promptReady))
    legacyStored.removeValue(forKey: "language")
    let legacy = Orchestrator.rootAssignment(from: legacyStored)
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
                  delivery: .init(transcriptKnown: true, recorded: legacyReceipt.recorded,
                                  retryDelayElapsed: true), attempts: 1) == .briefed)
    var audited = assignmentFixture(state: .blocked); audited.blocker = "workspace_trust_required"
    Orchestrator.holdRootAssignmentForTesting(audited)
    var notices: [(String, [String: String])] = []
    Orchestrator.rootAssignmentAuditObserverForTesting = { notices.append(($0, $1)) }
    check("the blocked transition emits one persisted operator-visible audit", Orchestrator.reportRootAssignmentTransition(audited.id))
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
    let limitedReply = Orchestrator.rootAssignment(limited, idempotencyKey: limitedID, assistantAvailable: { _ in true }, start: { _, _, _ in .started(id: "%rate-limited", backend: .tmux) })
    expect("the shared launch brake has a typed creation refusal", refusalCode(limitedReply), "rate_limited")
}
}
