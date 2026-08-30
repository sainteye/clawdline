import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Session closeability

func runSessionCloseabilityTests() {
group("closeability is four states, and doubt about the evidence outranks the list") {
    typealias C = Orchestrator.SessionCloseability
    typealias R = Orchestrator.CloseabilityReason
    expect("the closeability vocabulary is closed",
           C.allCases.map(\.rawValue), ["blocked", "needs_attestation", "safe", "unknown"])

    let started = Date(timeIntervalSince1970: 900)
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "CLOSE-TAB", assistant: .claude, tty: "/dev/ttys40", pid: 4000,
        processStart: started, conversationID: "conversation-close")
    let attestation = Orchestrator.ClosureAttestation(
        id: "attestation-1", identity: identity, activityGeneration: 7,
        obligationGeneration: 11, note: "nothing local left", auditID: nil,
        created: Date(timeIntervalSince1970: 1_000))
    let landingDebt = R(.pendingLandingOwned, subjectKind: "task", subjectID: "task-1")

    func project(_ terminal: SessionState = .idle,
                 obligations: [R] = [],
                 attestation: Orchestrator.ClosureAttestation? = nil,
                 complete: Bool = true, observed: Date? = Date(timeIntervalSince1970: 1_990),
                 matches: Int = 1, bound: Bool = true,
                 activity: Int = 7, obligation: Int = 11)
        -> Orchestrator.SessionCloseabilityProjection {
        Orchestrator.projectCloseability(
            Orchestrator.CloseabilityInput(
                terminalState: terminal, identity: identity, identityBound: bound,
                inventoryComplete: complete, inventoryObservedAt: observed,
                identityMatches: matches, obligations: obligations,
                attestation: attestation, activityGeneration: activity,
                obligationGeneration: obligation),
            now: Date(timeIntervalSince1970: 2_000))
    }
    func codes(_ p: Orchestrator.SessionCloseabilityProjection) -> [String] {
        p.reasons.map(\.code.rawValue)
    }

    // The four-state table: every value is reachable, and each by exactly the evidence its
    // contract names.
    expect("no obligation and a current attestation is the only way to safe",
           project(attestation: attestation).state, .safe)
    expect("no obligation and no attestation asks the session",
           project().state, .needsAttestation)
    expect("a positive obligation blocks", project(obligations: [landingDebt]).state, .blocked)
    expect("an incomplete reading is unknown", project(complete: false).state, .unknown)
    let aged = project(attestation: attestation,
                       observed: Date(timeIntervalSince1970: 1_900))
    expect("a complete but over-age inventory is unknown", aged.state, .unknown)
    expect("the source publishes the inventory's own observation time",
           (aged.wire["source"] as? [String: Any])?["observed_at"] as? Int, 1_900)

    // Fail-closed: nothing doubtful may pass through safe, whatever else is true.
    for (name, projected) in [
        ("a stale reading", project(attestation: attestation, complete: false)),
        ("a reading that never happened", project(attestation: attestation, observed: nil)),
        ("two rows claiming one conversation", project(attestation: attestation, matches: 2)),
        ("a row missing from its own reading", project(attestation: attestation, matches: 0)),
        ("an unbound process", project(attestation: attestation, bound: false)),
        ("a screen that could not be read", project(.unknown, attestation: attestation)),
    ] {
        expect("\(name) is unknown, never safe", projected.state, .unknown)
        check("\(name) still names its evidence problem",
              projected.reasons.contains { $0.code.kind == .evidence })
    }

    let doubted = project(obligations: [landingDebt], complete: false)
    expect("doubt outranks the obligation list", doubted.state, .unknown)
    check("and the obligations seen are still listed underneath, not swallowed",
          codes(doubted) == ["session_inventory_stale", "pending_landing_owned"])

    expect("a working terminal is itself an obligation",
           codes(project(.working("compiling"))), ["terminal_working"])
    expect("and a question on screen is one whose mover is a person",
           project(.waiting).reasons.first?.mover, .person)
    expect("a question on screen blocks rather than asking for an attestation",
           project(.waiting, attestation: attestation).state, .blocked)

    // The attestation half: identity and both clocks, each on its own.
    var reused = attestation
    expect("an attestation from an earlier process is not this process's",
           project(attestation: Orchestrator.ClosureAttestation(
                    id: "attestation-old",
                    identity: Orchestrator.SessionWorkIdentity(
                        terminalID: "CLOSE-TAB", assistant: .claude, tty: "/dev/ttys40",
                        pid: 4001, processStart: started,
                        conversationID: "conversation-close"),
                    activityGeneration: 7, obligationGeneration: 11, note: nil,
                    auditID: nil, created: Date(timeIntervalSince1970: 1_000))).state,
           .needsAttestation)
    reused = Orchestrator.ClosureAttestation(
        id: "attestation-1", identity: identity, activityGeneration: 6,
        obligationGeneration: 11, note: nil, auditID: nil,
        created: Date(timeIntervalSince1970: 1_000))
    expect("a later turn supersedes the attestation it was written for",
           codes(project(attestation: reused)), ["attestation_superseded"])
    reused = Orchestrator.ClosureAttestation(
        id: "attestation-1", identity: identity, activityGeneration: 7,
        obligationGeneration: 10, note: nil, auditID: nil,
        created: Date(timeIntervalSince1970: 1_000))
    expect("and so does any later obligation change",
           project(attestation: reused).state, .needsAttestation)
    check("a superseded attestation is still the session's own word on the record",
          project(attestation: reused).provenance == ["broker", "self"])
    check("a safe projection carries the attestation that earned it",
          project(attestation: attestation).attestationID == "attestation-1")
    check("and a safe one has nothing left to name a mover for",
          project(attestation: attestation).mover == nil)

    // The unique mover: one when there is one, and honestly absent when there are several.
    expect("one outstanding reason names the one thing that moves it",
           project(obligations: [landingDebt]).mover, .thisSession)
    expect("a peer wait names the session that must release it",
           project(obligations: [R(.coordinationWaitWaiting, subjectKind: "wait",
                                   subjectID: "wait-1",
                                   mover: .otherSession("OTHER-TAB"))]).mover,
           .otherSession("OTHER-TAB"))
    check("two different movers is no unique mover, and saying so is the answer",
          project(obligations: [landingDebt,
                                R(.owedDecision, mover: .person)]).mover == nil)

    // ready and safe are independent axes, and this is the pair that used to be read off each
    // other: a session inviting work while it still owns a landing nobody has closed.
    expect("ready is about taking work",
           Orchestrator.projectSessionWorkState(
            terminalState: .idle, task: nil, hasCoordinationWait: false,
            hasOpenHandoff: false, assignmentKnownAbsent: false,
            selfClaim: .ready), .ready)
    expect("and says nothing at all about being able to end",
           project(obligations: [landingDebt]).state, .blocked)

    let unreadableTwin = Orchestrator.SessionWorkIdentity(
        terminalID: "CLOSE-TWIN", assistant: .claude, tty: "/dev/ttys41", pid: 4001,
        processStart: started, conversationID: nil)
    let matchCounts = RemoteServer.identityMatchCounts([identity, unreadableTwin])
    expect("an unreadable competing assistant makes a bound identity ambiguous",
           matchCounts[identity.terminalID], 0)
    let codex = Orchestrator.SessionWorkIdentity(
        terminalID: "CLOSE-CODEX", assistant: .codex, tty: "/dev/ttys42", pid: 4002,
        processStart: started, conversationID: "conversation-codex")
    let providerCounts = RemoteServer.identityMatchCounts([identity, unreadableTwin, codex])
    expect("an unreadable Claude identity cannot make a bound Codex identity ambiguous",
           providerCounts[codex.terminalID], 1)

    // The CAS token: opaque, stable for one situation, and different for every input it covers.
    let version = Orchestrator.closeabilityVersion(
        identity: identity, activityGeneration: 7, obligationGeneration: 11, state: .safe)
    expect("the version is stable for the same identity, clocks and state",
           Orchestrator.closeabilityVersion(identity: identity, activityGeneration: 7,
                                            obligationGeneration: 11, state: .safe), version)
    for (name, other) in [
        ("a later turn", Orchestrator.closeabilityVersion(
            identity: identity, activityGeneration: 8, obligationGeneration: 11, state: .safe)),
        ("a later obligation", Orchestrator.closeabilityVersion(
            identity: identity, activityGeneration: 7, obligationGeneration: 12, state: .safe)),
        ("a different outcome", Orchestrator.closeabilityVersion(
            identity: identity, activityGeneration: 7, obligationGeneration: 11,
            state: .blocked)),
        ("a different process", Orchestrator.closeabilityVersion(
            identity: Orchestrator.SessionWorkIdentity(
                terminalID: "CLOSE-TAB", assistant: .claude, tty: "/dev/ttys40", pid: 4002,
                processStart: started, conversationID: "conversation-close"),
            activityGeneration: 7, obligationGeneration: 11, state: .safe)),
    ] {
        check("\(name) produces a different version", other != version)
    }
    check("the version is opaque and carries no identity in the clear",
          version.hasPrefix("cl1_") && !version.contains("CLOSE-TAB")
              && !version.contains("conversation-close"))
}

group("every broker blocker has a record that produces it, and a closure that clears it") {
    typealias R = Orchestrator.CloseabilityReason
    let started = Date(timeIntervalSince1970: 900)
    let root = Orchestrator.SessionWorkIdentity(
        terminalID: "ROOT-TAB", assistant: .claude, tty: "/dev/ttys41", pid: 4100,
        processStart: started, conversationID: "conversation-root")

    func child(_ id: String, _ state: Orchestrator.State,
               rootSession: String? = "conversation-root") -> Orchestrator.Task {
        var task = Orchestrator.Task(
            id: id, state: state, kind: "code", title: "delivery", assistant: .codex,
            projectDir: "/repo", timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 5),
            secretHash: String(repeating: "0", count: 64))
        task.rootSessionId = rootSession
        task.rootAssistant = .claude
        if state.isTerminal {
            task.finishedAt = Date(timeIntervalSince1970: 50)
            task.resultVerifiedAt = Date(timeIntervalSince1970: 50)
            task.summary = "done"
        }
        return task
    }
    func landing(_ state: Orchestrator.LandingState) -> Orchestrator.Landing {
        Orchestrator.Landing(state: state, target: "main", delivery: "branch",
                             ownerRootKey: "00000000",
                             since: Date(timeIntervalSince1970: 60), commit: nil, note: nil)
    }
    func obligations(tasks: [Orchestrator.Task] = [],
                     waits: [Orchestrator.CoordinationWait] = [],
                     handoffs: [Orchestrator.HandoffEnvelope] = [],
                     owed: Orchestrator.OwedDebt? = nil,
                     identity: Orchestrator.SessionWorkIdentity? = nil) -> [String] {
        Orchestrator.closeabilityObligations(
            identity: identity ?? root, tasks: tasks, waits: waits, handoffs: handoffs,
            owed: owed).map(\.code.rawValue)
    }

    expect("a live child is a live descendant",
           obligations(tasks: [child("t1", .briefed)]), ["live_descendant_task"])
    var withTab = child("t1", .briefed)
    withTab.childTerminalId = "CHILD-TAB"
    expect("named by the tab that has to finish, when the broker knows it",
           Orchestrator.closeabilityObligations(
            identity: root, tasks: [withTab], waits: [], handoffs: [], owed: nil).first?.mover,
           .otherSession("CHILD-TAB"))
    expect("somebody else's child is not this session's obligation",
           obligations(tasks: [child("t1", .briefed, rootSession: "conversation-other")]), [])

    var unreported = child("t2", .failure)
    unreported.resultVerifiedAt = nil
    unreported.summary = nil
    expect("a child that ended without reporting is the root's to account for",
           obligations(tasks: [unreported]), ["task_without_result"])
    unreported.landing = landing(.abandoned)
    expect("and an explicit abandoned landing is what closes it",
           obligations(tasks: [unreported]), [])

    var pending = child("t3", .success)
    pending.landing = landing(.pending)
    expect("a pending landing is an obligation this session owns",
           obligations(tasks: [pending]), ["pending_landing_owned"])
    pending.landing = landing(.landed)
    expect("a landed one is not", obligations(tasks: [pending]), [])

    var undelivered = child("t4", .success)
    undelivered.completionDelivery = Orchestrator.CompletionDelivery(
        noticeID: "notice", created: Date(timeIntervalSince1970: 70), state: .deadLetter,
        attempts: 8, nextRetryAt: nil)
    expect("a completion nobody acknowledged is an obligation",
           obligations(tasks: [undelivered]), ["completion_undelivered"])
    undelivered.completionDelivery = Orchestrator.CompletionDelivery(
        noticeID: "notice", created: Date(timeIntervalSince1970: 70), state: .acknowledged,
        attempts: 1, nextRetryAt: nil)
    expect("an acknowledged one is not", obligations(tasks: [undelivered]), [])

    var dirty = child("t5", .success)
    dirty.worktree = Orchestrator.Worktree(
        path: "/tmp/wt", branch: "clawdline/task/t5", base: "HEAD", repository: "/repo",
        cwd: "/repo", head: "abc", commits: 1, dirty: true)
    expect("dirty bytes in an isolated checkout are ownership nobody has closed",
           obligations(tasks: [dirty]), ["dirty_isolated_worktree"])
    dirty.landing = landing(.landed)
    expect("a landed delivery closes that ownership too", obligations(tasks: [dirty]), [])

    var touched = child("t6", .success)
    touched.claims = ["Sources/A.swift", "Sources/B.swift"]
    touched.untouchedClaims = ["Sources/B.swift"]
    expect("a claim the audit found touched, with no landing decision, is an obligation",
           obligations(tasks: [touched]), ["touched_claims_without_closure"])
    touched.untouchedClaims = ["Sources/A.swift", "Sources/B.swift"]
    expect("a task that touched nothing it claimed leaves nothing behind",
           obligations(tasks: [touched]), [])

    let owned = Orchestrator.CoordinationWait(
        id: "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa", repository: "/repo",
        paths: ["/repo/Sources"], ownerSessionID: "ROOT-TAB",
        releaseCondition: "after the commit", created: Date(timeIntervalSince1970: 80),
        waiters: [Orchestrator.CoordinationWaiter(
            sessionID: "OTHER-TAB", reason: "needs the file",
            created: Date(timeIntervalSince1970: 80))])
    expect("waiters stranded on files this session owns are an obligation",
           obligations(waits: [owned]), ["coordination_wait_owned"])
    let waiting = Orchestrator.CoordinationWait(
        id: "bbbbbbbb-1111-4111-8111-bbbbbbbbbbbb", repository: "/repo",
        paths: ["/repo/Sources"], ownerSessionID: "OWNER-TAB",
        releaseCondition: "after the commit", created: Date(timeIntervalSince1970: 80),
        waiters: [Orchestrator.CoordinationWaiter(
            sessionID: "ROOT-TAB", reason: "needs the file",
            created: Date(timeIntervalSince1970: 80))])
    expect("and being the one parked is an obligation with somebody else's name on it",
           Orchestrator.closeabilityObligations(
            identity: root, tasks: [], waits: [waiting], handoffs: [], owed: nil).first?.mover,
           .otherSession("OWNER-TAB"))
    let released = Orchestrator.CoordinationWait(
        id: "cccccccc-1111-4111-8111-cccccccccccc", repository: "/repo",
        paths: ["/repo/Sources"], ownerSessionID: "ROOT-TAB",
        releaseCondition: "after the commit", created: Date(timeIntervalSince1970: 80),
        waiters: [Orchestrator.CoordinationWaiter(
            sessionID: "OTHER-TAB", reason: "needs the file",
            created: Date(timeIntervalSince1970: 80),
            releaseDeliveredAt: Date(timeIntervalSince1970: 90))])
    expect("a released wait is not one", obligations(waits: [released]), [])

    expect("a handoff this session opened and nobody has taken is an obligation",
           obligations(handoffs: [Orchestrator.HandoffEnvelope(
            id: "dddddddd-1111-4111-8111-dddddddddddd", projectDir: "/repo",
            title: "carry on", fromSession: "ROOT-TAB",
            created: Date(timeIntervalSince1970: 95), state: .opening)]),
           ["open_handoff"])
    expect("a delivered one is not",
           obligations(handoffs: [Orchestrator.HandoffEnvelope(
            id: "dddddddd-1111-4111-8111-dddddddddddd", projectDir: "/repo",
            title: "carry on", fromSession: "ROOT-TAB",
            created: Date(timeIntervalSince1970: 95), state: .delivered)]),
           [])

    expect("an unpaid debt is an obligation whose mover is a person",
           Orchestrator.closeabilityObligations(
            identity: root, tasks: [], waits: [], handoffs: [],
            owed: Orchestrator.OwedDebt(note: "which design wins", movedBy: "the user",
                                        personNeeded: true,
                                        since: Date(timeIntervalSince1970: 100))).first?.mover,
           .person)

    // The child half of the same rule: a session that *is* a task cannot close mid-task.
    var own = Orchestrator.Task(
        id: "eeeeeeee-1111-4111-8111-eeeeeeeeeeee", state: .briefed, kind: "code",
        title: "the work", assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 5), secretHash: String(repeating: "0", count: 64))
    own.childTerminalId = "ROOT-TAB"
    own.childTTY = "/dev/ttys41"
    own.childPID = 4100
    own.childProcStart = started
    own.childSessionId = "conversation-root"
    own.transcriptProven = true
    expect("a session executing an unfinished task of its own is not closeable",
           obligations(tasks: [own]), ["own_task_unfinished"])
    own.state = .success
    own.finishedAt = Date(timeIntervalSince1970: 50)
    own.resultVerifiedAt = Date(timeIntervalSince1970: 50)
    own.summary = "done"
    own.landing = landing(.pending)
    own.worktree = Orchestrator.Worktree(
        path: "/tmp/child-wt", branch: "clawdline/task/own", base: "HEAD",
        repository: "/repo", cwd: "/repo", head: "abc", commits: 1, dirty: true)
    own.claims = ["Sources/A.swift"]
    own.untouchedClaims = []
    let executorAfterDelivery = Orchestrator.closeabilityObligations(
        identity: root, tasks: [own], waits: [], handoffs: [], owed: nil)
    expect("the executor does not inherit the dispatching root's landing obligations",
           executorAfterDelivery.map(\.code.rawValue), [])
}

group("a closure attestation is bound to one process and one turn, and survives a restart") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let started = Date(timeIntervalSince1970: 700)
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "ATTEST-TAB", assistant: .claude, tty: "/dev/ttys50", pid: 5000,
        processStart: started, conversationID: "conversation-attest")
    let neighbour = Orchestrator.SessionWorkIdentity(
        terminalID: "NEIGHBOUR-TAB", assistant: .claude, tty: "/dev/ttys51", pid: 5100,
        processStart: started, conversationID: "conversation-neighbour")

    func closeability() -> Orchestrator.SessionCloseabilityProjection {
        Orchestrator.sessionCloseability(
            identity: identity, terminalState: .idle,
            inventoryObservedAt: Date())
    }

    if case .refused(let status, let code, _, _) = Orchestrator.attestClosure(
        identity: Orchestrator.SessionWorkIdentity(
            terminalID: "ATTEST-TAB", assistant: nil, tty: "/dev/ttys50", pid: nil,
            processStart: nil, conversationID: nil),
        status: "clear", activityGeneration: 0, note: nil, auditID: nil) {
        expect("an unbound process cannot attest anything", status, 409)
        expect("with the same typed reason the other session routes use", code, "session_unbound")
    } else { check("an unbound attestation must be refused", false) }

    if case .refused(_, let code, _, _) = Orchestrator.attestClosure(
        identity: identity, status: "blocked", activityGeneration: 0, note: nil, auditID: nil) {
        expect("the route accepts exactly one status, by name", code,
               "closure_status_unsupported")
    } else { check("an unsupported status must be refused", false) }

    if case .refused(let status, let code, _, let extra) = Orchestrator.attestClosure(
        identity: identity, status: "clear", activityGeneration: 4, note: nil, auditID: nil) {
        expect("naming somebody else's turn is refused", status, 409)
        expect("by name", code, "closure_generation_stale")
        expect("and the answer says which turn the broker is on",
               extra["activity_generation"] as? Int, 0)
    } else { check("a stale generation must be refused", false) }

    expect("with no attestation the broker asks for one", closeability().state,
           .needsAttestation)
    guard case .ok(let receipt) = Orchestrator.attestClosure(
        identity: identity, status: "clear", activityGeneration: 0,
        note: "all owned work landed", auditID: "audit-1") else {
        check("a bound session naming this turn can attest", false); return
    }
    let attestationID = receipt["attestation_id"] as? String ?? ""
    check("the receipt names the attestation it created",
          receipt["created"] as? Bool == true && !attestationID.isEmpty)
    let safe = closeability()
    expect("and only the broker merge turns that into safe", safe.state, .safe)
    expect("the projection says both said so", safe.provenance, ["broker", "self"])
    let provenVersion = safe.version

    guard case .ok(let again) = Orchestrator.attestClosure(
        identity: identity, status: "clear", activityGeneration: 0,
        note: "all owned work landed", auditID: "audit-1") else {
        check("re-attesting the same turn is idempotent", false); return
    }
    check("a repeated attestation is the same receipt, not a second one",
          again["created"] as? Bool == false
              && again["attestation_id"] as? String == attestationID)

    Orchestrator.saveForTesting()
    Orchestrator.forget()
    let restarted = closeability()
    expect("a restart neither invents a tick nor loses one: still safe", restarted.state, .safe)
    expect("and the CAS token a client is holding still compares equal",
           restarted.version, provenVersion)

    // The CAS race, with the cheapest obligation change there is: somebody else's debt. It is
    // not this session's obligation, so `blocked` cannot be what refuses the close — only the
    // clock moving can.
    _ = Orchestrator.declareSessionState(
        identity: neighbour, terminalState: .working("still going"), claim: nil, note: nil,
        movedBy: nil, personNeeded: nil,
        owed: ["note": "which of the two designs"], clearOwed: false)
    let raced = closeability()
    expect("an obligation registered anywhere supersedes the attestation", raced.state,
           .needsAttestation)
    expect("and it is named as superseded rather than missing",
           raced.reasons.map(\.code.rawValue), ["attestation_superseded"])
    check("so the version a client copied a moment ago no longer compares equal",
          raced.version != provenVersion)
    check("and the close gate refuses it, with accept_loss set or not",
          !RemoteServer.closeIsProven(raced, expected: provenVersion, acceptLoss: true)
              && !RemoteServer.closeIsProven(raced, expected: provenVersion, acceptLoss: false))

    guard case .ok = Orchestrator.attestClosure(
        identity: identity, status: "clear", activityGeneration: 0, note: nil,
        auditID: nil) else {
        check("the session can attest again against the new clock", false); return
    }
    let reproven = closeability()
    expect("which returns it to safe", reproven.state, .safe)
    check("under a version that is not the old one",
          RemoteServer.closeIsProven(reproven, expected: reproven.version, acceptLoss: false)
              && reproven.version != provenVersion)

    // A new turn is the other invalidation, and it is per terminal rather than machine-wide.
    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID, to: .idle)
    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID,
                                        to: .working("the next turn"))
    expect("the turn clock advanced", Orchestrator.activityGeneration(ofTerminal: "ATTEST-TAB"),
           1)
    expect("so the attestation written for the previous turn no longer holds",
           closeability().state, .needsAttestation)

    // Identity reuse: a later process in the same tab inherits nothing.
    guard case .ok = Orchestrator.attestClosure(
        identity: identity, status: "clear", activityGeneration: 1, note: nil,
        auditID: nil) else {
        check("the session attests for the new turn", false); return
    }
    expect("the current process is safe again", closeability().state, .safe)
    var reused = identity
    reused.pid = 5001
    expect("a later process in the same terminal cannot borrow the attestation",
           Orchestrator.sessionCloseability(
            identity: reused, terminalState: .idle,
            inventoryObservedAt: Date()).state, .needsAttestation)
    var resumed = identity
    resumed.conversationID = "conversation-resumed"
    expect("nor can a different conversation in the same process",
           Orchestrator.sessionCloseability(
            identity: resumed, terminalState: .idle,
            inventoryObservedAt: Date()).state, .needsAttestation)

    // The route's own half of the same boundary, and it is a separate line of code from the
    // projection's. A later process in this tab, naming the same turn clock, must be issued its
    // own receipt: handing back the previous process's id would tell it that a claim it never
    // made is already on the record under its name.
    var relaunched = identity
    relaunched.pid = 5002
    guard case .ok(let issued) = Orchestrator.attestClosure(
        identity: relaunched, status: "clear",
        activityGeneration: Orchestrator.activityGeneration(ofTerminal: "ATTEST-TAB"),
        note: nil, auditID: nil) else {
        check("a later process in the same tab can attest for itself", false); return
    }
    check("and is issued its own attestation rather than the previous process's",
          issued["created"] as? Bool == true
              && (issued["attestation_id"] as? String).map { $0 != attestationID } == true)
}

group("the close route asks to be proven only when a client says so") {
    typealias P = RemoteServer.CloseProofRequest
    expect("a body without the field leaves the existing gate exactly as it was",
           RemoteServer.closeProofRequest(nil), P.notRequested)
    expect("a version string opts in",
           RemoteServer.closeProofRequest("cl1_abc"), P.expecting("cl1_abc"))
    expect("an empty string is malformed rather than an opt-out",
           RemoteServer.closeProofRequest(""), P.malformed)
    expect("and so is a value of the wrong type",
           RemoteServer.closeProofRequest(7), P.malformed)

    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "GATE-TAB", assistant: .claude, tty: "/dev/ttys60", pid: 6000,
        processStart: Date(timeIntervalSince1970: 700), conversationID: "conversation-gate")
    func projection(_ state: Orchestrator.SessionCloseability,
                    reasons: [Orchestrator.CloseabilityReason] = [],
                    attestation: String? = nil)
        -> Orchestrator.SessionCloseabilityProjection {
        Orchestrator.SessionCloseabilityProjection(
            state: state, reasons: reasons, observedAt: Date(timeIntervalSince1970: 1),
            sessionGeneration: 3, sourceFreshness: "current",
            sourceObservedAt: Date(timeIntervalSince1970: 1), activityGeneration: 1,
            obligationGeneration: 2,
            version: Orchestrator.closeabilityVersion(
                identity: identity, activityGeneration: 1, obligationGeneration: 2,
                state: state),
            provenance: ["broker"], attestationID: attestation, mover: nil)
    }
    let safe = projection(.safe, attestation: "attestation-1")
    check("a matching version on a safe projection is the one thing that proves a close",
          RemoteServer.closeIsProven(safe, expected: safe.version, acceptLoss: false))
    check("a version from an older situation does not",
          !RemoteServer.closeIsProven(safe, expected: "cl1_stale", acceptLoss: false))
    // The whole of the accept_loss boundary, in the four states it has to hold in.
    for state in Orchestrator.SessionCloseability.allCases where state != .safe {
        let projected = projection(state)
        check("accept_loss cannot buy a close the broker cannot prove (\(state.rawValue))",
              !RemoteServer.closeIsProven(projected, expected: projected.version,
                                          acceptLoss: true))
    }
    check("and it cannot buy one whose version moved either",
          !RemoteServer.closeIsProven(safe, expected: "cl1_stale", acceptLoss: true))
}

group("the closure route is closed at its edges") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let wasWriting = Config.shared.remoteWrite
    try? FileManager.default.removeItem(at: store)
    Orchestrator.forget()
    Config.shared.remoteWrite = true
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken(),
                "Content-Type": "application/json"]
    let session = TargetSession(
        backend: .iterm, id: "CLOSURE-ROUTE", name: "closure route", tty: "/dev/ttys70",
        windowIndex: 0, tabIndex: 0, assistant: .claude)
    RemoteServer.sessionPayloadForTesting = ([session], ["CLOSURE-ROUTE": .idle])
    defer {
        Config.shared.remoteWrite = wasWriting
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.sessionWorkIdentityForTesting = nil
        RemoteServer.sessionEndForTesting = nil
        RemoteServer.coordinatorObservationEvidenceForTesting = nil
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }

    expect("an attestation with no credential at all never reaches the route",
           RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/orchestrator/sessions/CLOSURE-ROUTE/closure",
            body: "{\"status\":\"clear\"}")).status, 401)
    expect("a session nobody is running is a 404",
           RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/orchestrator/sessions/NOBODY/closure", headers: auth,
            body: "{\"status\":\"clear\",\"activity_generation\":0}")).status, 404)
    let extraKey = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/CLOSURE-ROUTE/closure", headers: auth,
        body: "{\"status\":\"clear\",\"activity_generation\":0,\"force\":true}"))
    expect("the body is a closed set of keys", extraKey.status, 400)
    let empty = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/CLOSURE-ROUTE/closure", headers: auth, body: "{}"))
    expect("and an empty body is not an attestation", empty.status, 400)
    // The runner has no live assistant process behind that terminal, which is exactly the
    // unbound case: the route must refuse rather than attest something it cannot identify.
    let unbound = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/CLOSURE-ROUTE/closure", headers: auth,
        body: "{\"status\":\"clear\",\"activity_generation\":0}"))
    expect("a terminal whose process cannot be bound cannot attest", unbound.status, 409)
    expect("by name", remoteErrorCode(unbound), "session_unbound")

    let peer = TargetSession(
        backend: .iterm, id: "CLOSURE-PEER", name: "closure peer", tty: "/dev/ttys71",
        windowIndex: 0, tabIndex: 1, assistant: .claude)
    let identities: [String: Orchestrator.SessionWorkIdentity] = [
        session.id: .init(
            terminalID: session.id, assistant: .claude, tty: session.tty, pid: 7_000,
            processStart: Date(timeIntervalSince1970: 7_000),
            conversationID: "conversation-closure-route"),
        peer.id: .init(
            terminalID: peer.id, assistant: .claude, tty: peer.tty, pid: 7_001,
            processStart: Date(timeIntervalSince1970: 7_001),
            conversationID: "conversation-closure-peer"),
    ]
    RemoteServer.sessionWorkIdentityForTesting = { identities[$0.id]! }
    RemoteServer.sessionPayloadForTesting = ([session, peer], [session.id: .idle, peer.id: .idle])

    let made = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/\(session.id)/closure", headers: auth,
        body: "{\"status\":\"clear\",\"activity_generation\":0}"))
    expect("the machine token is the real authorization boundary for an attestation",
           made.status, 200)
    let madeBody = (try? JSONSerialization.jsonObject(with: made.body)) as? [String: Any]
    let madeCloseability = madeBody?["closeability"] as? [String: Any]
    expect("the route receipt is a reread of the real idle projection",
           madeCloseability?["state"] as? String, "safe")

    RemoteServer.sessionPayloadForTesting = (
        [session, peer], [session.id: .working("still checking"), peer.id: .idle])
    let workingReceipt = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/\(session.id)/closure", headers: auth,
        body: "{\"status\":\"clear\",\"activity_generation\":0}"))
    let workingBody = (try? JSONSerialization.jsonObject(with: workingReceipt.body))
        as? [String: Any]
    expect("an attestation receipt cannot fabricate idle over a working terminal",
           (workingBody?["closeability"] as? [String: Any])?["state"] as? String, "blocked")

    RemoteServer.sessionPayloadForTesting = ([session, peer], [session.id: .idle, peer.id: .idle])
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1), generation: 8, complete: true)
    let agedReceipt = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/sessions/\(session.id)/closure", headers: auth,
        body: "{\"status\":\"clear\",\"activity_generation\":0}"))
    let agedBody = (try? JSONSerialization.jsonObject(with: agedReceipt.body)) as? [String: Any]
    let agedCloseability = agedBody?["closeability"] as? [String: Any]
    expect("an over-age complete inventory fails the route receipt closed",
           agedCloseability?["state"] as? String, "unknown")
    expect("and the receipt publishes the inventory observation, not response time",
           (agedCloseability?["source"] as? [String: Any])?["observed_at"] as? Int, 1)
    RemoteServer.coordinatorObservationEvidenceForTesting = nil

    let reader = RemoteAuth.addDevice(name: "closeability route reader", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id) }
    Orchestrator.resetCloseabilityRegistryReadCountForTesting()
    let publicRead = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions", headers: ["Authorization": "Bearer \(reader.token)"]))
    expect("the public read route executes its real serializer", publicRead.status, 200)
    let publicBody = (try? JSONSerialization.jsonObject(with: publicRead.body)) as? [String: Any]
    let publicRows = publicBody?["sessions"] as? [[String: Any]] ?? []
    check("the public read route carries one typed closeability object per row",
          publicRows.count == 2 && publicRows.allSatisfy { $0["closeability"] is [String: Any] })
    expect("one list request takes one amortized registry snapshot",
           Orchestrator.closeabilityRegistryReadsForTesting(), 1)

    let orchestratorRead = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/sessions", headers: auth))
    expect("the orchestrator read route executes its real serializer", orchestratorRead.status, 200)
    let orchestratorBody = (try? JSONSerialization.jsonObject(with: orchestratorRead.body))
        as? [String: Any]
    let orchestratorRows = orchestratorBody?["sessions"] as? [[String: Any]] ?? []
    let orchestratorShapes = orchestratorRows.map {
        "\($0["id"] as? String ?? "?"):\(type(of: $0["closeability"] as Any))"
    }
    check("the orchestrator read route uses the same typed closeability schema",
          orchestratorRows.count == 2
            && orchestratorRows.allSatisfy { $0["closeability"] is [String: Any] },
          "rows=\(orchestratorShapes)")

    var ended: [String] = []
    RemoteServer.sessionEndForTesting = { ended.append($0.id); return nil }
    func end(_ target: TargetSession, body: String) -> RemoteServer.Response {
        let headers = [
            "Authorization": "Bearer \(reader.token)",
            "Idempotency-Key": UUID().uuidString,
            "Content-Type": "application/json",
        ]
        return RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/sessions/\(target.id)/end", headers: headers, body: body))
    }
    let safeVersion = madeCloseability?["version"] as? String ?? ""
    let refused = end(session, body:
        "{\"expected_closeability_version\":\"cl1_stale\",\"accept_loss\":true}")
    expect("the actual POST end wiring refuses a moved proof", refused.status, 409)
    expect("the route names the typed proof refusal", remoteErrorCode(refused), "close_not_proven")
    let refusedBody = (try? JSONSerialization.jsonObject(with: refused.body)) as? [String: Any]
    check("the refusal carries the target's current typed projection",
          ((refusedBody?["error"] as? [String: Any])?["closeability"]
            as? [String: Any])?["state"] as? String == "safe")
    check("accept_loss never reached the destructive handoff", ended.isEmpty)
    check("the route writes its named refusal audit",
          RemoteAuth.recentAudit(limit: 20).contains {
              $0["event"] as? String == "session.end.refused"
                && $0["id"] as? String == session.id
          })

    let wrongTarget = end(peer, body:
        "{\"expected_closeability_version\":\"\(safeVersion)\"}")
    expect("a proof for another exact target identity is refused", wrongTarget.status, 409)
    expect("by the same typed gate", remoteErrorCode(wrongTarget), "close_not_proven")

    let compatibility = end(peer, body: "{}")
    expect("omitting expected_closeability_version preserves the old close contract",
           compatibility.status, 200)
    expect("the optional compatibility path reaches only its named target", ended, [peer.id])

    let proven = end(session, body:
        "{\"expected_closeability_version\":\"\(safeVersion)\"}")
    expect("the exact safe version reaches the final close handoff", proven.status, 200)
    expect("and no other route call reached it", ended, [peer.id, session.id])
}

}
