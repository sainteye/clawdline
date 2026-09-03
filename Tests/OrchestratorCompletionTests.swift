import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3



func runOrchestratorCompletionTests() {
group("task completion ingress and delivery are durable protocol facts") {
    let physical = OrchestratorDraft.RootIdentityEvidence(
        source: "active_terminal", terminalID: "terminal-root",
        canonicalSessionID: "conversation-root", assistant: .codex)
    guard case .refused(let status, let code, _, let extra)? =
        OrchestratorDraft.rootIdentityRefusal(
            claimed: "terminal-root", evidence: [physical]) else {
        check("an active physical root id is refused", false)
        return
    }
    expect("the physical root refusal is actionable", status, 422)
    expect("the physical root refusal is typed", code, "root_identity_is_terminal")
    expect("the physical root refusal names the canonical conversation id",
           extra["canonical_root_session_id"] as? String, "conversation-root")
    check("an unknown or offline identity is not guessed",
          OrchestratorDraft.rootIdentityRefusal(
            claimed: "unknown-root", evidence: []) == nil)
    check("a caller-mislabeled assistant cannot hide a proved physical terminal tuple",
          OrchestratorDraft.rootIdentityRefusal(
            claimed: "terminal-root", evidence: [physical]) != nil)
    if case .refused(_, _, _, let mislabeledExtra)? = OrchestratorDraft.rootIdentityRefusal(
        claimed: "terminal-root", evidence: [physical]) {
        expect("the correction returns the actual assistant, not the caller label",
               mislabeledExtra["canonical_root_assistant"] as? String, "codex")
    } else {
        check("the correction returns the actual assistant, not the caller label", false)
    }
    let conflicting = OrchestratorDraft.RootIdentityEvidence(
        source: "conflict", terminalID: "terminal-root",
        canonicalSessionID: "conversation-other", assistant: .codex)
    check("conflicting positive tuples remain inconclusive",
          OrchestratorDraft.rootIdentityRefusal(
            claimed: "terminal-root", evidence: [physical, conflicting]) == nil)
    check("a null claimed identity remains polling-compatible",
          OrchestratorDraft.rootIdentityRefusal(
            claimed: nil, evidence: [physical]) == nil)

    // New ordinary HTTP work must never choose Claude merely because the owner assistant was
    // omitted. This exercises the real dispatch boundary, including its terminal starter seam;
    // a pure draft check would not prove registration and opening stayed untouched.
    Orchestrator.forget()
    let missingAssistantID = UUID().uuidString.lowercased()
    let missingAssistantDir = Orchestrator.root
        .appendingPathComponent(missingAssistantID, isDirectory: true)
    defer {
        try? FileManager.default.removeItem(at: missingAssistantDir)
        Orchestrator.forget()
    }
    try! FileManager.default.createDirectory(at: missingAssistantDir,
                                             withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_protocol": 1, "task_id": missingAssistantID, "kind": "custom",
        "assistant": "codex", "project_dir": "/tmp", "title": "owner assistant gate",
        "instructions": "must refuse before spawn", "timeout_minutes": 5,
        "root": ["session_id": "conversation-root"],
    ]).write(to: missingAssistantDir.appendingPathComponent("task.json"), options: .atomic)
    var openedWithoutOwnerAssistant = 0
    Orchestrator.taskStarterForTesting = { _, _, _, _, _, _ in
        openedWithoutOwnerAssistant += 1
        return .started(id: "SHOULD-NOT-OPEN", backend: .iterm)
    }
    Orchestrator.rootIdentityEvidenceForTesting = []
    let missingRateBefore = Orchestrator.dispatchRateCountForTesting()
    guard case .refused(let missingStatus, let missingCode, _, _) = Orchestrator.dispatch(
            taskID: missingAssistantID, secret: String(repeating: "9a", count: 32),
            requireRootSession: true) else {
        check("ordinary dispatch refuses an omitted root assistant", false)
        return
    }
    expect("omitted root assistant is an actionable ingress refusal", missingStatus, 422)
    expect("omitted root assistant has its own typed error", missingCode,
           "root_assistant_required")
    expect("the owner-assistant refusal opens no terminal", openedWithoutOwnerAssistant, 0)
    check("the owner-assistant refusal registers no task",
          Orchestrator.record(id: missingAssistantID) == nil)
    expect("the owner-assistant refusal refunds its provisional rate ticket",
           Orchestrator.dispatchRateCountForTesting(), missingRateBefore)

    let claudeRoot = TargetSession(
        backend: .iterm, id: "CLAUDE-ROOT", name: "Claude root", tty: "/dev/ttys201",
        windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: "/tmp")
    let codexRoot = TargetSession(
        backend: .iterm, id: "CODEX-ROOT", name: "Codex root", tty: "/dev/ttys202",
        windowIndex: 0, tabIndex: 1, assistant: .codex, cwd: "/tmp")
    let exactConversations = ["CLAUDE-ROOT": "claude-conversation",
                              "CODEX-ROOT": "codex-conversation"]
    let explicitClaude = Orchestrator.canonicalRootSession(
        "claude-conversation", assistant: .claude, among: [claudeRoot, codexRoot],
        sessionID: { exactConversations[$0.id] })
    let explicitCodex = Orchestrator.canonicalRootSession(
        "codex-conversation", assistant: .codex, among: [claudeRoot, codexRoot],
        sessionID: { exactConversations[$0.id] })
    check("explicit Claude and Codex owners continue through the exact process-bound resolver",
          explicitClaude.sessionID == "claude-conversation" && explicitClaude.warning == nil
            && explicitCodex.sessionID == "codex-conversation" && explicitCodex.warning == nil)

    // Exercise the real ingress gate. This refusal occurs before registration or spawn, so no
    // terminal is opened; the fixture also proves the provisional rate ticket is refunded.
    Orchestrator.forget()
    let dispatchID = UUID().uuidString.lowercased()
    let dispatchDir = Orchestrator.root.appendingPathComponent(dispatchID, isDirectory: true)
    defer {
        try? FileManager.default.removeItem(at: dispatchDir)
        Orchestrator.forget()
    }
    let dispatchObject: [String: Any] = [
        "clawdline_protocol": 1, "task_id": dispatchID, "kind": "custom",
        "assistant": "codex", "project_dir": "/tmp", "title": "identity gate probe",
        "instructions": "must refuse before spawn", "timeout_minutes": 5,
        "root": ["session_id": "terminal-root", "assistant": "codex"],
    ]
    try! FileManager.default.createDirectory(at: dispatchDir, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: dispatchObject)
        .write(to: dispatchDir.appendingPathComponent("task.json"), options: .atomic)
    Orchestrator.rootIdentityEvidenceForTesting = [physical]
    let rateBefore = Orchestrator.dispatchRateCountForTesting()
    guard case .refused(let dispatchStatus, let dispatchCode, _, let dispatchExtra) =
        Orchestrator.dispatch(taskID: dispatchID, secret: String(repeating: "ab", count: 32)) else {
        check("the real dispatch gate rejects a proved physical root id", false)
        return
    }
    expect("the real dispatch gate returns the actionable status", dispatchStatus, 422)
    expect("the real dispatch gate returns the typed identity code", dispatchCode,
           "root_identity_is_terminal")
    expect("the real dispatch gate returns its process-bound correction",
           dispatchExtra["canonical_root_session_id"] as? String, "conversation-root")
    expect("identity refusal refunds the provisional dispatch rate ticket",
           Orchestrator.dispatchRateCountForTesting(), rateBefore)
    check("identity refusal registers no task", Orchestrator.record(id: dispatchID) == nil)

    // The dispatch-level negative probe repeats the same lie. A serialization holder makes the
    // pre-fix path safe: if the identity gate misses it, the candidate is queued and no terminal
    // is opened. The fixed gate must still refuse before that registration.
    var blocker = Orchestrator.Task(
        id: "abababab-cdcd-4efe-8123-abcdefabcdef", state: .briefed,
        kind: "custom", title: "identity gate blocker", assistant: .codex,
        projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    blocker.serialize = ["identity-gate-probe"]
    Orchestrator.holdScheduleTaskForTesting(blocker)
    let mislabeledID = UUID().uuidString.lowercased()
    let mislabeledDir = Orchestrator.root.appendingPathComponent(mislabeledID, isDirectory: true)
    defer { try? FileManager.default.removeItem(at: mislabeledDir) }
    var mislabeledObject = dispatchObject
    mislabeledObject["task_id"] = mislabeledID
    mislabeledObject["serialize"] = ["identity-gate-probe"]
    mislabeledObject["root"] = ["session_id": "terminal-root", "assistant": "claude"]
    try! FileManager.default.createDirectory(at: mislabeledDir, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: mislabeledObject)
        .write(to: mislabeledDir.appendingPathComponent("task.json"), options: .atomic)
    let mislabeledRateBefore = Orchestrator.dispatchRateCountForTesting()
    if case .refused(let mismatchStatus, let mismatchCode, _, let mismatchExtra) =
        Orchestrator.dispatch(taskID: mislabeledID, secret: String(repeating: "cd", count: 32)) {
        check("the real dispatch gate ignores a false caller assistant label",
              mismatchStatus == 422 && mismatchCode == "root_identity_is_terminal")
        expect("the real dispatch gate returns the actual canonical assistant",
               mismatchExtra["canonical_root_assistant"] as? String, "codex")
    } else {
        check("the real dispatch gate ignores a false caller assistant label", false)
    }
    expect("mislabeled identity refusal also refunds its provisional rate ticket",
           Orchestrator.dispatchRateCountForTesting(), mislabeledRateBefore)
    check("mislabeled identity refusal registers no task",
          Orchestrator.record(id: mislabeledID) == nil)

    let noticeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let made = Orchestrator.CompletionDelivery(
        noticeID: noticeID, created: Date(timeIntervalSince1970: 100),
        state: .pending, attempts: 0,
        nextRetryAt: Date(timeIntervalSince1970: 100))
    let stored = OrchestratorStore.stored(made)
    let roundTrip = OrchestratorStore.completionDelivery(from: stored)
    expect("a pending completion outbox survives a persisted round trip", roundTrip, made)

    let noticeTask = Orchestrator.Task(
        id: "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff", state: .success,
        kind: "custom", title: "durable completion", assistant: .codex,
        projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    let notice = Orchestrator.taskFinishedNotice(
        for: noticeTask, audience: .root, noticeID: noticeID)
    let decoded = notice.map(ClawdlineMessage.encode).flatMap(ClawdlineMessage.decode)
    expect("task_finished v2 carries the stable notice id",
           decoded?.completionAcknowledgement?.noticeID, noticeID)
    check("task_finished v2 carries an explicit ACK route",
          decoded?.completionAcknowledgement?.path.hasSuffix("/completion/ack") == true
              && decoded?.body.contains("after observing, ACK") == true)
}

group("owned child dispatch and detached automation use different doors") {
    Orchestrator.forget()
    let wasEnabled = Config.shared.orchestratorEnabled
    Config.shared.orchestratorEnabled = true
    let store = Orchestrator.storeURL
    let storedBefore = try? Data(contentsOf: store)
    let ownedID = UUID().uuidString.lowercased()
    let detachedID = UUID().uuidString.lowercased()
    let taskIDs = [ownedID, detachedID]
    defer {
        for id in taskIDs {
            try? FileManager.default.removeItem(
                at: Orchestrator.root.appendingPathComponent(id, isDirectory: true))
        }
        if let storedBefore {
            try? storedBefore.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Config.shared.orchestratorEnabled = wasEnabled
        Orchestrator.forget()
    }

    func writeDetachedTask(_ id: String) {
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        try! FileManager.default.createDirectory(at: directory,
                                                 withIntermediateDirectories: true)
        let task: [String: Any] = [
            "clawdline_protocol": 1, "task_id": id, "kind": "custom",
            "assistant": "codex", "project_dir": "/tmp", "title": "poll-only probe",
            "instructions": "the test starter prevents a real terminal", "timeout_minutes": 5,
            "root": ["session_id": NSNull(), "poll_only": true,
                     "label": "unattended fixture"],
        ]
        try! JSONSerialization.data(withJSONObject: task)
            .write(to: directory.appendingPathComponent("task.json"), options: .atomic)
    }
    taskIDs.forEach(writeDetachedTask)

    var opened = 0
    Orchestrator.taskStarterForTesting = { _, _, _, _, _, _ in
        opened += 1
        return .started(id: "TEST-DETACHED-\(opened)", backend: .iterm)
    }
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let secret = String(repeating: "da", count: 32)
    func body(_ id: String) -> String {
        "{\"task_id\":\"\(id)\",\"secret\":\"\(secret)\"}"
    }

    let ordinary = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/tasks", headers: auth, body: body(ownedID)))
    expect("ordinary child dispatch refuses poll-only mode", ordinary.status, 422)
    expect("the wrong door has a typed correction", remoteErrorCode(ordinary),
           "detached_route_required")
    expect("the ordinary door opens no detached executor", opened, 0)
    check("the ordinary refusal registers no detached task",
          Orchestrator.record(id: ownedID) == nil)

    let detached = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/detached-tasks", headers: auth, body: body(detachedID)))
    expect("the dedicated automation door accepts an explicit poll-only task",
           detached.status, 200)
    expect("the dedicated door opens exactly one executor", opened, 1)
    check("the detached task remains deliberately ownerless",
          (Orchestrator.record(id: detachedID)?["root"] as? [String: Any])?["sessionId"] == nil)
    check("detached automation uses the bounded terminal-worker lane",
          RemoteServer.isOrchestratorTerminalWorkerRoute(
            "/v1/orchestrator/detached-tasks"))
}

group("completion transport failure injection is typed and bounded") {
    let codes: [Orchestrator.CompletionFailureCode] = [
        .rootMissing, .conversationAmbiguous, .rootChoosing, .itermModal, .terminalTimeout,
        .identityStale,
    ]
    for code in codes {
        let original = Orchestrator.CompletionDelivery(
            noticeID: UUID().uuidString.lowercased(),
            created: Date(timeIntervalSince1970: 100), state: .pending, attempts: 0,
            nextRetryAt: Date(timeIntervalSince1970: 100))
        let advanced = Orchestrator.completionTransition(
            original, at: Date(timeIntervalSince1970: 101),
            result: .failed(code, "injected \(code.rawValue)"))
        expect("\(code.rawValue) remains a typed retryable failure",
               advanced.lastError?.code, code)
        check("\(code.rawValue) gets a bounded next retry",
              advanced.state == .pending && advanced.nextRetryAt != nil
                && advanced.nextRetryAt!.timeIntervalSince1970 <= 101
                    + Orchestrator.completionRetryMaximum)
    }
    var penultimate = Orchestrator.CompletionDelivery(
        noticeID: UUID().uuidString.lowercased(), created: Date(timeIntervalSince1970: 100),
        state: .pending, attempts: Orchestrator.completionAttemptLimit - 2,
        nextRetryAt: Date(timeIntervalSince1970: 100))
    penultimate = Orchestrator.completionTransition(
        penultimate, at: Date(timeIntervalSince1970: 101),
        result: .failed(.rootMissing, "one retry remains"))
    var last = Orchestrator.CompletionDelivery(
        noticeID: UUID().uuidString.lowercased(), created: Date(timeIntervalSince1970: 100),
        state: .pending, attempts: Orchestrator.completionAttemptLimit - 1,
        nextRetryAt: Date(timeIntervalSince1970: 100))
    last = Orchestrator.completionTransition(
        last, at: Date(timeIntervalSince1970: 101),
        result: .failed(.rootMissing, "still missing"))
    check("the cells immediately inside and at the dead-letter boundary stay distinct",
          penultimate.attempts == Orchestrator.completionAttemptLimit - 1
            && penultimate.state == .pending && penultimate.deadLetterAt == nil
            && penultimate.nextRetryAt != nil
            && last.attempts == Orchestrator.completionAttemptLimit
            && last.state == .deadLetter && last.deadLetterAt != nil
            && last.nextRetryAt == nil)
    check("the retry budget ends in a typed dead letter",
          last.state == .deadLetter && last.deadLetterAt != nil && last.nextRetryAt == nil)
}

group("the three ways a completion fails to arrive are each typed and each visible") {
    // The existing injection group drives `completionTransition`, a pure function: given a code,
    // does the state machine react correctly. That leaves the question this group asks — does the
    // real path ever produce those codes, and can anybody see the ending it settles on. Each case
    // is asserted against its own opposite, because an injection that would pass whatever the
    // code did is not a probe, it is a shape that happens to be green.
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let root = TargetSession(
        backend: .iterm, id: "ROOT-TAB", name: "root", tty: "/dev/ttys900",
        windowIndex: 0, tabIndex: 0, assistant: .claude)
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: root.id, assistant: .claude, tty: root.tty, pid: 900,
        processStart: Date(timeIntervalSince1970: 1_800_000_000),
        conversationID: "root-conversation")
    var owed = Orchestrator.Task(
        id: "30303030-4040-4050-8060-707070707070", state: .success, kind: "custom",
        title: "owes a notice", assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(), secretHash: String(repeating: "0", count: 64))
    owed.rootSessionId = "root-conversation"
    owed.rootAssistant = .claude

    // 1. The root's tab is gone. The control is the same task against an inventory that has it.
    var found = false
    if case .found = Orchestrator.completionRecipient(
        owed, targets: [root], identity: { _ in identity }) { found = true }
    check("with the root's tab present the recipient resolves", found)
    var goneCode: Orchestrator.CompletionFailureCode?
    if case .refused(.failed(let code, _)) = Orchestrator.completionRecipient(
        owed, targets: [], identity: { _ in identity }) { goneCode = code }
    expect("and once its tab is gone the refusal is typed, not silent",
           goneCode, Orchestrator.CompletionFailureCode.rootMissing)

    // 2. Nothing acknowledges. Delivery succeeding is not the end of the obligation.
    let delivered = Orchestrator.completionTransition(
        Orchestrator.CompletionDelivery(
            noticeID: UUID().uuidString.lowercased(), created: Date(),
            state: .pending, attempts: 0, nextRetryAt: Date(), persisted: true),
        at: Date(), result: .delivered)
    check("a delivered notice with no acknowledgement is still owed a retry",
          delivered.state == .delivered && delivered.nextRetryAt != nil
            && delivered.acknowledgedAt == nil,
          "state \(delivered.state.rawValue)")

    // 3. The budget runs out. The point is not the state, it is that somebody can see it: a dead
    // letter nobody can read is the same as a notice that was never owed.
    var exhausted = Orchestrator.CompletionDelivery(
        noticeID: UUID().uuidString.lowercased(), created: Date(),
        state: .pending, attempts: Orchestrator.completionAttemptLimit - 1,
        nextRetryAt: Date(), persisted: true)
    exhausted = Orchestrator.completionTransition(
        exhausted, at: Date(), result: .failed(.rootMissing, "still gone"))
    owed.completionDelivery = exhausted
    Orchestrator.holdScheduleTaskForTesting(owed)
    check("the exhausted budget settles as a dead letter with no retry left",
          exhausted.state == .deadLetter && exhausted.nextRetryAt == nil)
    let hidden = Orchestrator.completionRecords(pendingOnly: true)
        .contains { $0["task_id"] as? String == owed.id }
    let shown = Orchestrator.completionRecords(pendingOnly: false)
        .contains { $0["task_id"] as? String == owed.id }
    check("and it is reportable rather than merely stored: absent from the pending view, "
            + "present when dead letters are asked for",
          !hidden && shown, "pending \(hidden), all \(shown)")
}

group("grandchild completion delivery is bound to the parent's exact process tuple") {
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let started = Date(timeIntervalSince1970: 1_800_000_100)
    let target = TargetSession(
        backend: .iterm, id: "PARENT-TAB", name: "parent", tty: "/dev/ttys210",
        windowIndex: 0, tabIndex: 0, assistant: .codex)
    let exact = Orchestrator.SessionWorkIdentity(
        terminalID: target.id, assistant: .codex, tty: target.tty, pid: 2_100,
        processStart: started, conversationID: "parent-conversation")
    func parent(transcriptProven: Bool = true) -> Orchestrator.Task {
        var task = Orchestrator.Task(
            id: "10101010-2020-4030-8040-505050505050", state: .briefed,
            kind: "custom", title: "parent", assistant: .codex,
            projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: String(repeating: "0", count: 64))
        task.childTerminalId = target.id
        task.childTTY = target.tty
        task.childPID = 2_100
        task.childProcStart = started
        task.childSessionId = "parent-conversation"
        task.transcriptProven = transcriptProven
        return task
    }
    let grandchild = Orchestrator.Task(
        id: "20202020-3030-4040-8050-606060606060", state: .success,
        kind: "custom", title: "grandchild", assistant: .codex,
        projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        parentTaskId: "10101010-2020-4030-8040-505050505050",
        secretHash: String(repeating: "0", count: 64))

    func attempt(_ observed: Orchestrator.SessionWorkIdentity,
                 transcriptProven: Bool = true)
        -> (result: Orchestrator.CompletionTransportResult, sends: Int) {
        Orchestrator.holdScheduleTaskForTesting(parent(transcriptProven: transcriptProven))
        var sends = 0
        let result = Orchestrator.completionDelivery(
            grandchild, "completion", targets: [target], identity: { _ in observed },
            isChoosing: { _ in false }, send: { _, _ in sends += 1; return nil })
        return (result, sends)
    }
    func checkStale(_ name: String, _ outcome: (
        result: Orchestrator.CompletionTransportResult, sends: Int)) {
        if case .failed(let code, _) = outcome.result {
            check(name, code == .identityStale && outcome.sends == 0)
        } else {
            check(name, false, "transport was reached \(outcome.sends) time(s)")
        }
    }

    let positive = attempt(exact)
    check("the exact parent tuple reaches transport once",
          positive.result == .delivered && positive.sends == 1)
    var stale = exact
    stale.pid = 2_101
    checkStale("a reused terminal and tty with another PID is identity_stale and sends nothing",
               attempt(stale))
    stale = exact
    stale.processStart = started.addingTimeInterval(30)
    checkStale("a recycled PID with another process start is identity_stale and sends nothing",
               attempt(stale))
    stale = exact
    stale.conversationID = "replacement-conversation"
    checkStale("another conversation in the same process tuple is identity_stale and sends nothing",
               attempt(stale))
    checkStale("a parent without transcript marker proof is identity_stale and sends nothing",
               attempt(exact, transcriptProven: false))
}

group("the wait for a completion notice has an upper bound, and something re-arms it") {
    // "Eventually" is not a contract a person can hold. What a root actually waits is: the first
    // attempt is scheduled by finalize itself, every later one is picked up by the five-second
    // beat, and the gap between attempts is capped. Each of those three is asserted here, because
    // between them they are the bound — and the middle one had nothing holding it at all.
    let pumped = Orchestrator.completionPumpEnqueuerForTesting
    defer { Orchestrator.completionPumpEnqueuerForTesting = pumped }

    var rearmed = 0
    Orchestrator.completionPumpEnqueuerForTesting = { _ in rearmed += 1 }
    Orchestrator.beat(fromTimer: true)
    check("the beat re-arms the completion pump, which is what makes a retry ever happen",
          rearmed >= 1, "beat scheduled the pump \(rearmed) times")
    // Deleting `scheduleCompletionPump()` from `beat` leaves every other completion assertion
    // green: they all drive `completionAttempt` directly. The retry ladder below would still be
    // correct arithmetic about a retry nothing would ever come back for.

    let ladder = (1...Orchestrator.completionAttemptLimit).map {
        Orchestrator.completionRetryDelay(after: $0)
    }
    check("no gap between attempts exceeds the cap",
          ladder.allSatisfy { $0 <= Orchestrator.completionRetryMaximum },
          "ladder \(ladder)")
    check("and the gaps grow rather than repeating the shortest one",
          ladder.first == 5 && ladder[1] == 10 && ladder.last == Orchestrator.completionRetryMaximum,
          "ladder \(ladder)")
    let worst = ladder.dropLast().reduce(0, +)
    check("so the whole budget is bounded, not open-ended",
          worst > 0 && worst <= 3600, "worst case \(Int(worst))s over "
            + "\(Orchestrator.completionAttemptLimit) attempts")
    expect("a zeroth attempt waits for nothing", Orchestrator.completionRetryDelay(after: 0), 0)
}

group("finalize saves before send, retries one notice after restart, and ACK stops it") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let notifyWasEnabled = Config.shared.orchestratorNotifyRoot
    defer {
        Config.shared.orchestratorNotifyRoot = notifyWasEnabled
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Config.shared.orchestratorNotifyRoot = true
    let id = "cccccccc-dddd-4eee-8fff-000000000001"
    let secret = String(repeating: "9a", count: 32)
    let row: [String: Any] = [
        "id": id, "state": "briefed", "kind": "custom", "title": "atomic finish",
        "assistant": "codex", "project_dir": "/tmp", "timeout_minutes": 30,
        "created": Date().addingTimeInterval(-30).timeIntervalSince1970,
        "root_session": "root-conversation", "root_assistant": "codex",
        "secret_hash": Orchestrator.hash(ofSecret: secret), "artifacts": [],
    ]
    try! FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])
        .write(to: store, options: .atomic)
    Orchestrator.load(force: true)
    var sawAtomicStoreBeforeEnqueue = false
    Orchestrator.completionPumpEnqueuerForTesting = { _ in
        let object = (try? Data(contentsOf: store)).flatMap {
            try? JSONSerialization.jsonObject(with: $0) as? [String: Any]
        }
        let saved = (object?["tasks"] as? [[String: Any]])?.first
        sawAtomicStoreBeforeEnqueue = saved?["state"] as? String == "success"
            && saved?["completion_delivery"] is [String: Any]
    }
    Orchestrator.finalize(id, as: .success, summary: "finished")
    check("the terminal outcome and outbox are durable before delivery is enqueued",
          sawAtomicStoreBeforeEnqueue)

    guard let firstRecord = Orchestrator.record(id: id),
          let firstDelivery = firstRecord["completion_delivery"] as? [String: Any],
          let noticeID = firstDelivery["notice_id"] as? String,
          let next = firstDelivery["next_retry_at"] as? Int else {
        check("finalize exposes its durable completion envelope", false)
        return
    }
    var wires: [String] = []
    check("the first injected transport succeeds",
          Orchestrator.completionAttempt(
            taskID: id, now: Date(timeIntervalSince1970: Double(next) + 1),
            deliver: { _, line in wires.append(line); return .delivered }))
    Orchestrator.forget()
    Orchestrator.completionPumpEnqueuerForTesting = { _ in }
    Orchestrator.load(force: true)
    let restarted = Orchestrator.record(id: id)?["completion_delivery"] as? [String: Any]
    let retryAt = restarted?["next_retry_at"] as? Int ?? 0
    check("send-before-ACK restart retries the persisted envelope",
          Orchestrator.completionAttempt(
            taskID: id, now: Date(timeIntervalSince1970: Double(retryAt) + 1),
            deliver: { _, line in wires.append(line); return .delivered }))
    let deliveredIDs = wires.compactMap(ClawdlineMessage.decode)
        .compactMap(\.completionAcknowledgement?.noticeID)
    check("duplicate transport keeps one consumption identity",
          deliveredIDs == [noticeID, noticeID])

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let path = "/v1/orchestrator/tasks/\(id)/completion/ack"
    let body = "{\"notice_id\":\"\(noticeID)\"}"
    expect("an anonymous ACK cannot enter the machine route",
           RemoteServer.shared.route(remoteRequest("POST", path, body: body)).status, 401)
    let ack = RemoteServer.shared.route(remoteRequest("POST", path, headers: auth, body: body))
    expect("the matching root ACK succeeds", ack.status, 200)
    let again = RemoteServer.shared.route(remoteRequest("POST", path, headers: auth, body: body))
    expect("repeating the same ACK is idempotent", again.status, 200)
    let acknowledged = Orchestrator.record(id: id)?["completion_delivery"] as? [String: Any]
    check("observation and acknowledgement are explicit persisted facts",
          acknowledged?["state"] as? String == "acknowledged"
            && acknowledged?["transport_delivered_at"] != nil
            && acknowledged?["observed_at"] != nil
            && acknowledged?["acknowledged_at"] != nil)
    check("an acknowledged notice is never consumed again",
          !Orchestrator.completionAttempt(
            taskID: id, now: Date().addingTimeInterval(10_000),
            deliver: { _, _ in return .delivered }))

    let list = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/completions", headers: auth))
    expect("the authenticated delivery ledger is exposed", list.status, 200)
    let listBody = (try? JSONSerialization.jsonObject(with: list.body)) as? [String: Any]
    let listed = (listBody?["completions"] as? [[String: Any]])?.first {
        $0["task_id"] as? String == id
    }
    check("the ledger keeps accepted, executed and result verification distinct",
          listed?["accepted_at"] != nil && listed?["executed_at"] != nil
            && listed?["result_verified_at"] is NSNull)
}

group("ACK save failure rolls back only its delivery transition") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Orchestrator.storeSaveInterceptorForTesting = nil
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    let id = "30303030-4040-4050-8060-707070707070"
    let noticeID = "40404040-5050-4060-8070-808080808080"
    var task = Orchestrator.Task(
        id: id, state: .success, kind: "custom", title: "ACK rollback",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 100),
        spawnedAt: Date(timeIntervalSince1970: 110),
        finishedAt: Date(timeIntervalSince1970: 120), rootSessionId: "root-conversation",
        rootAssistant: .codex, secretHash: String(repeating: "0", count: 64))
    task.completionDelivery = Orchestrator.CompletionDelivery(
        noticeID: noticeID, created: Date(timeIntervalSince1970: 120),
        state: .pending, attempts: 1, nextRetryAt: Date(timeIntervalSince1970: 130),
        persisted: true)
    Orchestrator.holdScheduleTaskForTesting(task)
    Orchestrator.saveForTesting()

    let concurrentClose = Date(timeIntervalSince1970: 900)
    let concurrentRepository = "/tmp/concurrent-repository"
    let concurrentBranch = OrchestratorDraft.worktreeBranch(for: id)!
    let concurrentPath = OrchestratorDraft.worktreePath(project: concurrentRepository, taskID: id)!
    Orchestrator.storeSaveInterceptorForTesting = { _ in
        Orchestrator.mutateTaskForTesting(id) { latest in
            latest.worktree = Orchestrator.Worktree(
                path: concurrentPath, branch: concurrentBranch,
                base: String(repeating: "a", count: 40),
                repository: concurrentRepository, cwd: concurrentPath)
            latest.landing = Orchestrator.Landing(
                state: .pending, target: "main", delivery: "delivery/concurrent",
                ownerRootKey: "12345678", since: Date(timeIntervalSince1970: 800),
                commit: nil, note: "concurrent landing")
            latest.closeAt = concurrentClose
        }
        return false
    }
    guard case .refused(let status, let code, _, _) = Orchestrator.acknowledgeCompletion(
        taskID: id, noticeID: noticeID, now: Date(timeIntervalSince1970: 700)) else {
        check("the injected ACK save failure is reported", false)
        return
    }
    check("the injected ACK save failure is typed",
          status == 500 && code == "completion_store_failed")
    Orchestrator.storeSaveInterceptorForTesting = nil
    let inMemory = Orchestrator.record(id: id)
    let delivery = inMemory?["completion_delivery"] as? [String: Any]
    let worktree = inMemory?["worktree"] as? [String: Any]
    let landing = inMemory?["landing"] as? [String: Any]
    check("rollback restores only the prior unacknowledged delivery",
          delivery?["state"] as? String == "pending"
            && delivery?["acknowledged_at"] is NSNull)
    check("rollback preserves concurrent worktree, landing and close fields",
          worktree?["branch"] as? String == concurrentBranch
            && landing?["state"] as? String == "pending"
            && Orchestrator.closeAtForTesting(id) == concurrentClose)

    Orchestrator.saveForTesting()
    Orchestrator.forget()
    Orchestrator.load(force: true)
    let reloaded = Orchestrator.record(id: id)
    let reloadedDelivery = reloaded?["completion_delivery"] as? [String: Any]
    let reloadedWorktree = reloaded?["worktree"] as? [String: Any]
    let reloadedLanding = reloaded?["landing"] as? [String: Any]
    expect("the narrow delivery rollback survives a persistent round trip",
           reloadedDelivery?["state"] as? String, "pending")
    expect("the concurrent worktree survives a persistent round trip",
           reloadedWorktree?["branch"] as? String, concurrentBranch)
    expect("the concurrent landing survives a persistent round trip",
           reloadedLanding?["state"] as? String, "pending")
    expect("the concurrent close deadline survives a persistent round trip",
           Orchestrator.closeAtForTesting(id), concurrentClose)
}

group("completion HTTP schemas are closed and machine-authenticated") {
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let list = "/v1/orchestrator/completions"
    expect("anonymous completion ledger access is still forbidden",
           RemoteServer.shared.route(remoteRequest("GET", list + "?unexpected=true")).status,
           401)
    let validQueries = [
        (list, false),
        (list + "?pending=true", true),
        (list + "?pending=1", true),
        (list + "?pending=false", false),
        (list + "?pending=0", false),
    ]
    for (valid, pendingOnly) in validQueries {
        let response = RemoteServer.shared.route(remoteRequest("GET", valid, headers: auth))
        let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
        check("a valid closed completion query preserves its exact boolean meaning: \(valid)",
              response.status == 200 && object?["pending_only"] as? Bool == pendingOnly)
    }
    for invalid in [list + "?unexpected=true", list + "?pending=yes",
                    list + "?pending=", list + "?pending=true&pending=true",
                    list + "?pending=true&pending=1"] {
        let response = RemoteServer.shared.route(remoteRequest("GET", invalid, headers: auth))
        check("an unknown completion query key/value is typed 400: \(invalid)",
              response.status == 400 && remoteErrorCode(response) == "bad_request")
    }

    let reconcile = "/v1/orchestrator/completions/reconcile"
    expect("authentication precedes reconcile body disclosure",
           RemoteServer.shared.route(remoteRequest("POST", reconcile, body: "{")).status, 401)
    for valid in ["{}", #"{"include_dead_letter":false}"#] {
        expect("a valid closed reconcile body remains accepted: \(valid)",
               RemoteServer.shared.route(remoteRequest(
                "POST", reconcile, headers: auth, body: valid)).status, 200)
    }
    let invalidBodies = [
        "", "{", "[]", "null", #"{"extra":true}"#,
        #"{"task_id":7}"#, #"{"include_dead_letter":"false"}"#,
        #"{"include_dead_letter":1}"#,
    ]
    for body in invalidBodies {
        let response = RemoteServer.shared.route(remoteRequest(
            "POST", reconcile, headers: auth, body: body))
        check("a malformed/non-object/extra/wrong reconcile body is typed 400: \(body)",
              response.status == 400 && remoteErrorCode(response) == "bad_request")
    }
}

group("both shipped skills teach the durable completion contract") {
    let variants = [
        ("English", "skills/clawdline/SKILL.md"),
        ("Traditional Chinese", "skills/clawdline/SKILL.zh-TW.md"),
    ]
    for (language, path) in variants {
        let skill = try! String(contentsOfFile: path, encoding: .utf8)
        check("the \(language) skill corrects a proved physical root tuple",
              skill.contains("root_identity_is_terminal")
                && skill.contains("canonical_root_session_id")
                && skill.contains("canonical_root_assistant"))
        check("the \(language) skill identifies the durable task_finished notice",
              skill.contains("task_finished") && skill.contains("notice_id")
                && skill.contains("completion/ack"))
        check("the \(language) ACK snippet defines every fresh-shell input",
              skill.contains(#"PORT="${CLAWDLINE_PORT:-7717}""#)
                && skill.contains(#"TOKEN=$(cat ~/.config/clawdline/orchestrator-token)"#)
                && skill.contains("TASK_ID='<task-id from the completion line>'")
                && skill.contains("NOTICE_ID='<notice-id from the completion line>'"))
        check("the \(language) ACK call consumes its uppercase task and notice ids",
              skill.contains("tasks/$TASK_ID/completion/ack")
                && skill.contains(#"{\"notice_id\":\"$NOTICE_ID\"}"#))
        check("the \(language) skill explains bounded dead-letter reconciliation",
              skill.contains("dead_letter") && skill.contains("completions/reconcile")
                && skill.contains("include_dead_letter"))
    }
}

group("both shipped skills trigger on cross-session reports") {
    let variants = [
        ("English", "skills/clawdline/SKILL.md", "another live session"),
        ("Traditional Chinese", "skills/clawdline/SKILL.zh-TW.md", "另一個 live session"),
    ]
    let triggerCases = ["send", "message", "report", "status", "finding", "coordination"]
    for (language, path, destination) in variants {
        let skill = try! String(contentsOfFile: path, encoding: .utf8)
        let sections = skill.components(separatedBy: "---")
        let frontmatter = sections.count > 2 ? sections[1].lowercased() : ""
        check("the \(language) trigger metadata addresses another live session",
              frontmatter.contains(destination.lowercased()))
        for trigger in triggerCases {
            check("the \(language) trigger metadata includes \(trigger)",
                  frontmatter.contains(trigger))
        }
    }
}

group("the protocol page carries the durable completion contract") {
    // This used to assert against the gitignored artifact, and to demand metadata that named this
    // delivery's own diff hash and a candidate state of "pending". Neither is a contract: the hash
    // changes on every commit, so the check could only be kept green by feeding it, and "pending"
    // becomes false the moment the work lands — a test built to eventually lie. The protocol's
    // authority is `docs/clawdline-protocol.html` now, so the substance is asserted there and the
    // moment-in-time metadata is gone rather than maintained.
    let page = try! String(contentsOfFile: "docs/clawdline-protocol.html", encoding: .utf8)
    // The codec moved to its own namespace; the guard follows the bytes it reads rather than
    // the file it used to be in.
    let storeSource = try! String(
        contentsOfFile: "Sources/OrchestratorStore.swift", encoding: .utf8)
    let completionStorageBody = storeSource
        .components(separatedBy: "static func stored(_ delivery: Orchestrator.CompletionDelivery)")
        .dropFirst().first?.components(separatedBy: "static func completionDelivery(from").first
        ?? ""
    let completionFieldPattern = try! NSRegularExpression(
        pattern: #""([^"\n]+)": delivery\.noticeID"#)
    let completionFieldRange = NSRange(
        completionStorageBody.startIndex..<completionStorageBody.endIndex,
        in: completionStorageBody)
    let completionFields = completionFieldPattern.matches(
        in: completionStorageBody, range: completionFieldRange).compactMap { match -> String? in
            guard let range = Range(match.range(at: 1), in: completionStorageBody) else {
                return nil
            }
            return String(completionStorageBody[range])
        }
    check("the page names production's one authoritative completion id and its ACK route",
          completionFields.count == 1 && completionFields.first == "notice_id"
            && page.contains("<dt><code>\(completionFields.first ?? "")</code></dt>")
            && page.contains("completion/ack"),
          "source fields \(completionFields)")
    check("and says a send is not an observation",
          page.contains("a successful send is not an observation"))
    check("the page names the dead letter and how it is read back",
          page.contains("dead_letter") && page.contains("include_dead_letter"))
    check("and says a dead letter nobody can read is worth nothing",
          page.contains("a dead letter nobody can read"))
    check("the page names the six facts a completion is decomposed into",
          page.contains("six separate facts")
            && page.contains("transport-delivered") && page.contains("acknowledged"))
    check("the page names the physical-root refusal and what it returns instead",
          page.contains("root_identity_is_terminal")
            && page.contains("canonical_root_session_id")
            && page.contains("canonical_root_assistant"))
}

group("legacy completion reconciliation is bounded and preserves polling compatibility") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    let now = Date()
    func legacy(_ id: String, age: TimeInterval, root: String?) -> [String: Any] {
        var row: [String: Any] = [
            "id": id, "state": "success", "kind": "custom", "title": "legacy",
            "assistant": "claude", "project_dir": "/tmp", "timeout_minutes": 30,
            "created": now.addingTimeInterval(-age - 10).timeIntervalSince1970,
            "finished_at": now.addingTimeInterval(-age).timeIntervalSince1970,
            "secret_hash": String(repeating: "0", count: 64), "artifacts": [],
        ]
        if let root { row["root_session"] = root }
        return row
    }
    let recent = "dddddddd-eeee-4fff-8000-000000000001"
    let historical = "dddddddd-eeee-4fff-8000-000000000002"
    let manual = "dddddddd-eeee-4fff-8000-000000000003"
    let rows = [legacy(recent, age: 60, root: "known-root"),
                legacy(historical, age: 8 * 24 * 3600, root: "old-root"),
                legacy(manual, age: 60, root: nil)]
    try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])
        .write(to: store, options: .atomic)
    Orchestrator.load(force: true)
    Orchestrator.completionPumpEnqueuerForTesting = { _ in }
    guard case .ok(let outcome) = Orchestrator.reconcileCompletions(
        taskID: nil, includeDeadLetters: false, now: now) else {
        check("legacy completion reconciliation succeeds", false)
        return
    }
    expect("only the recent identifiable legacy task is reconciled",
           outcome["created"] as? Int, 1)
    check("historical and null-root rows retain polling without guessed identities",
          Orchestrator.record(id: recent)?["completion_delivery"] != nil
            && Orchestrator.record(id: historical)?["completion_delivery"] == nil
            && Orchestrator.record(id: manual)?["completion_delivery"] == nil)
    guard case .ok(let repeated) = Orchestrator.reconcileCompletions(
        taskID: nil, includeDeadLetters: false, now: now) else {
        check("repeating legacy reconciliation succeeds", false)
        return
    }
    expect("legacy reconciliation is idempotent", repeated["created"] as? Int, 0)
}

group("Coordinator evidence rejects physical ingress and follows only proved rebind aliases") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-completion-coordinator-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? FileManager.default.removeItem(at: directory)
    }
    let old = coordinatorFixture(
        "physical-old", assistant: .codex, tty: "/dev/ttys201", pid: 2_001,
        processStart: Date(timeIntervalSince1970: 100), conversation: "conversation-old")
    let replacement = coordinatorFixture(
        "physical-new", assistant: .codex, tty: "/dev/ttys202", pid: 2_002,
        processStart: Date(timeIntervalSince1970: 200), conversation: "conversation-new")
    _ = Coordinator.register(
        old, among: [old], now: Date(timeIntervalSince1970: 100),
        makeID: { UUID(uuidString: "eeeeeeee-ffff-4000-8111-111111111111")! })
    Coordinator.forgetForTesting()
    let durable = Coordinator.rootIdentityEvidence(claimed: "physical-old")
    check("an offline durable physical binding still supplies canonical ingress evidence",
          durable.first?.canonicalSessionID == "conversation-old")
    check("an unknown physical id supplies no guess",
          Coordinator.rootIdentityEvidence(claimed: "unknown").isEmpty)
    guard case .ok = Coordinator.rebind(
        expectedCoordinatorID: "eeeeeeee-ffff-4000-8111-111111111111",
        expectedGeneration: 1, to: replacement, among: [replacement], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 200),
        now: Date(timeIntervalSince1970: 201)) else {
        check("the Coordinator fixture rebinds", false)
        return
    }
    expect("a task from the old binding follows the proved Coordinator rebind",
           Coordinator.deliveryBinding(
            for: "conversation-old", historicalAssistant: .codex,
            taskCreated: Date(timeIntervalSince1970: 150))?.conversationID,
           "conversation-new")
    expect("a same-assistant rebind returns the canonical assistant too",
           Coordinator.deliveryBinding(
            for: "conversation-old", historicalAssistant: .codex,
            taskCreated: Date(timeIntervalSince1970: 150))?.assistant, .codex)
    check("an unrelated historical identity is never rewritten",
          Coordinator.deliveryBinding(
            for: "some-other-root", historicalAssistant: .codex,
            taskCreated: Date(timeIntervalSince1970: 150)) == nil)

    func proveCrossAssistantDelivery(from oldAssistant: Assistant,
                                     to currentAssistant: Assistant,
                                     coordinatorID: String) {
        try? FileManager.default.removeItem(at: Coordinator.storeURLOverrideForTesting!)
        Coordinator.forgetForTesting()
        let old = coordinatorFixture(
            "\(oldAssistant.rawValue)-stale-terminal", assistant: oldAssistant,
            tty: "/dev/ttys211", pid: 2_101,
            processStart: Date(timeIntervalSince1970: 100),
            conversation: "\(oldAssistant.rawValue)-historical-conversation")
        let current = coordinatorFixture(
            "\(currentAssistant.rawValue)-current-terminal", assistant: currentAssistant,
            tty: "/dev/ttys212", pid: 2_102,
            processStart: Date(timeIntervalSince1970: 200),
            conversation: "\(currentAssistant.rawValue)-canonical-conversation")
        guard case .ok = Coordinator.register(
            old, among: [old], now: Date(timeIntervalSince1970: 100),
            makeID: { UUID(uuidString: coordinatorID)! }),
              case .ok = Coordinator.rebind(
                expectedCoordinatorID: coordinatorID, expectedGeneration: 1,
                to: current, among: [current], sessionsFresh: true,
                sessionsObservedAt: Date(timeIntervalSince1970: 200),
                now: Date(timeIntervalSince1970: 201)) else {
            check("the \(oldAssistant.rawValue)-to-\(currentAssistant.rawValue) fixture rebinds",
                  false)
            return
        }

        let staleTarget = TargetSession(
            backend: .iterm, id: old.identity.terminalID, name: "stale root",
            tty: old.identity.tty, windowIndex: 0, tabIndex: 0,
            assistant: oldAssistant)
        let currentTarget = TargetSession(
            backend: .iterm, id: current.identity.terminalID, name: "current root",
            tty: current.identity.tty, windowIndex: 0, tabIndex: 1,
            assistant: currentAssistant)
        let identities = [old.identity.terminalID: old.identity,
                          current.identity.terminalID: current.identity]
        let task = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
            title: "cross-assistant completion", assistant: .codex,
            projectDir: "/tmp", timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 150),
            rootSessionId: old.identity.conversationID, rootAssistant: oldAssistant,
            secretHash: String(repeating: "0", count: 64))
        var recipients: [String] = []
        let delivered = Orchestrator.completionDelivery(
            task, "completion", targets: [staleTarget, currentTarget],
            identity: { identities[$0.id]! }, isChoosing: { _ in false },
            send: { _, target in recipients.append(target.id); return nil })
        check("\(oldAssistant.rawValue)-to-\(currentAssistant.rawValue) uses the canonical current tuple",
              delivered == .delivered && recipients == [currentTarget.id])

        var staleSends = 0
        let staleOnly = Orchestrator.completionDelivery(
            task, "completion", targets: [staleTarget],
            identity: { identities[$0.id]! }, isChoosing: { _ in false },
            send: { _, _ in staleSends += 1; return nil })
        if case .failed(let code, _) = staleOnly {
            check("\(oldAssistant.rawValue)-to-\(currentAssistant.rawValue) never transports to the stale assistant",
                  (code == .rootMissing || code == .identityStale) && staleSends == 0)
        } else {
            check("\(oldAssistant.rawValue)-to-\(currentAssistant.rawValue) never transports to the stale assistant",
                  false, "transport was reached \(staleSends) time(s)")
        }
    }

    proveCrossAssistantDelivery(
        from: .codex, to: .claude,
        coordinatorID: "aaaaaaaa-1111-4222-8333-bbbbbbbbbbbb")
    proveCrossAssistantDelivery(
        from: .claude, to: .codex,
        coordinatorID: "cccccccc-4444-4555-8666-dddddddddddd")
}
}
