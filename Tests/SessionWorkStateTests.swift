import Foundation

func runSessionWorkStateTests() {
group("the coordination address book carries a bounded self-reported peer handoff") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let at = Date(timeIntervalSince1970: 1_800_000_100)
    let session = TargetSession(backend: .tmux, id: "%handoff", name: "delivered work",
        tty: "/dev/ttys071", windowIndex: 0, tabIndex: 0, assistant: .codex,
        cwd: "/Users/me/code/clawdline")
    let publication = SessionWatch.PublishedIdentity(assistant: .codex, tty: session.tty,
        pid: 8_123, processStart: at.addingTimeInterval(-60),
        conversationID: "conversation-handoff", workingDirectory: session.cwd, recordURL: nil,
        observedAt: at, provenance: "coordination_handoff_fixture", conversationSource: .test,
        conversationObservedAt: at)
    let identity = Orchestrator.SessionWorkIdentity(terminalID: session.id, assistant: .codex,
        tty: session.tty, pid: 8_123, processStart: at.addingTimeInterval(-60),
        conversationID: "conversation-handoff")
    guard case .ok = Orchestrator.declareSessionState(identity: identity,
        terminalState: .working("handing off"), claim: "waiting_session",
        note: "finished; waiting for Clawdfather integration", movedBy: "%426",
        personNeeded: false, owed: nil, clearOwed: false, now: at) else {
        check("the fixture records a self-reported peer handoff", false); return
    }

    guard let row = RemoteServer.coordinationSessionRows([session], states: [session.id: .idle],
        publishedIdentities: [session.id: publication]).first else {
        check("the waiting session remains addressable", false); return
    }
    expect("the address book no longer collapses a peer handoff into unknown",
        row["work_state"] as? String, "waiting_session")
    expect("the address book says this is the Session's own account",
        row["work_provenance"] as? String, "self")
    expect("the address book names what is waiting", row["work_note"] as? String,
        "finished; waiting for Clawdfather integration")
    expect("the address book names the exact mover", row["work_moved_by"] as? String, "%426")
    expect("a peer handoff does not masquerade as a user decision",
        row["work_person_needed"] as? Bool, false)
    check("the bounded status still carries no screen or transcript content",
        row["line"] == nil && row["menu"] == nil && row["sessionId"] == nil)
}

group("a peer handoff is bounded across declaration, restart, and projection") {
    suggestedReplyProof()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let at = Date(timeIntervalSince1970: 850)
    let identity = Orchestrator.SessionWorkIdentity(terminalID: "%426", assistant: .codex,
        tty: "/dev/ttys042", pid: 2_000, processStart: Date(timeIntervalSince1970: 800),
        conversationID: "conversation-handoff")

    if case .refused(_, let code, _, _) = Orchestrator.declareSessionState(identity: identity,
        terminalState: .working("wrapping"), claim: "waiting_session", note: "finished",
        movedBy: nil, personNeeded: false, owed: nil, clearOwed: false) {
        expect("a peer wait must name the session that moves it", code,
            "waiting_session_needs_evidence")
    } else { check("an ownerless peer wait must be refused", false) }

    guard case .ok = Orchestrator.declareSessionState(identity: identity,
        terminalState: .working("handing off"), claim: "waiting_session",
        note: "finished; waiting for Clawdfather integration", movedBy: "%426",
        personNeeded: false, owed: nil, clearOwed: false, now: at) else {
        check("a bound session can declare a named peer handoff", false); return
    }
    let waiting = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("the peer handoff is visible instead of unknown", waiting.state, .waitingSession)
    expect("the peer handoff is explicitly self-reported", waiting.provenance, "self")
    expect("the row names what it is waiting for", waiting.note,
        "finished; waiting for Clawdfather integration")
    expect("the row names the exact mover", waiting.movedBy, "%426")
    expect("a peer handoff does not ask the user", waiting.personNeeded, false)
    Orchestrator.saveForTesting(); Orchestrator.forget()
    expect("a named peer handoff survives restart",
        Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle).state,
        .waitingSession)

    let durable = Orchestrator.SessionSelfState(identity: identity, claim: .waitingSession,
        note: "finished; waiting for Clawdfather integration", movedBy: "%426",
        personNeeded: false, claimReportedAt: at, claimSettled: true, owed: nil)
    let encoded = OrchestratorStore.stored(durable)
    expect("the valid peer handoff survives the durable codec",
        OrchestratorStore.sessionSelfState(from: encoded)?.claim, .waitingSession)
    let malformed: [(String, (inout [String: Any]) -> Void)] = [
        ("missing mover", { $0.removeValue(forKey: "moved_by") }),
        ("person-owned wait", { $0["person_needed"] = true }),
        ("missing report time", { $0.removeValue(forKey: "claim_reported_at") }),
        ("unbounded note", {
            $0["note"] = String(repeating: "x", count: Orchestrator.sessionSelfNoteLimit + 1)
        }),
    ]
    for (name, mutate) in malformed {
        var row = encoded; mutate(&row)
        check("a \(name) cannot become a peer wait after restart",
            OrchestratorStore.sessionSelfState(from: row) == nil)
    }
    var hold = encoded
    hold["claim"] = Orchestrator.SessionWorkState.holding.rawValue
    hold.removeValue(forKey: "note")
    check("an evidence-free hold cannot appear after restart",
        OrchestratorStore.sessionSelfState(from: hold) == nil)
    var debt = encoded
    debt["owed"] = ["note": String(repeating: "d",
        count: Orchestrator.sessionSelfNoteLimit + 1), "person_needed": true, "since": 850.0]
    check("an unbounded durable debt cannot enter the coordination address book",
        OrchestratorStore.sessionSelfState(from: debt) == nil)
}
}

private func suggestedReplyProof() {
    let store = Orchestrator.storeURL, before = try? Data(contentsOf: Orchestrator.storeURL)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let now = Date(), expires = floor(now.timeIntervalSince1970) + 600
    let identity = Orchestrator.SessionWorkIdentity(terminalID: "%reply", assistant: .codex,
        tty: "/dev/ttys071", pid: 8_123, processStart: now.addingTimeInterval(-60),
        conversationID: "conversation-reply")
    let suggestion: [String: Any] = ["version": 1, "id": "decision-1",
        "text": "允許這個 exact candidate\n保留我的補充。", "conversationId": "conversation-reply", "expiresAt": expires]
    func declare(_ reply: Any?, person: Bool = true) -> Orchestrator.Reply {
        var owed: [String: Any] = ["note": "請檢查候選", "person_needed": person]
        if let reply { owed["suggestedReply"] = reply }
        return Orchestrator.declareSessionState(identity: identity, terminalState: .working("requesting"),
            claim: nil, note: nil, movedBy: nil, personNeeded: nil, owed: owed, clearOwed: false, now: now)
    }
    guard case .ok = declare(suggestion) else { check("typed reply is admitted", false); return }
    let projected = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("typed literal suggested reply is projected without note inference",
        (projected.owed?["suggestedReply"] as? [String: Any])?["text"] as? String, suggestion["text"] as? String)
    expect("declaring a suggestion keeps the decision owed", projected.owed?["person_needed"] as? Bool, true)
    Orchestrator.saveForTesting(); Orchestrator.forget()
    let restored = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("suggestion identity survives registry persistence and restart",
        (restored.owed?["suggestedReply"] as? [String: Any])?["id"] as? String, "decision-1")
    if let decoded = OrchestratorStore.suggestedReply(from: suggestion, conversationID: identity.conversationID) {
        let debt = Orchestrator.OwedDebt(note: "still owed", movedBy: nil, personNeeded: true,
            since: now, suggestedReply: decoded)
        let expired = OrchestratorStore.owedPayload(debt, conversationID: identity.conversationID,
            now: Date(timeIntervalSince1970: expires))
        check("expired suggestions disappear without clearing debt",
            expired["suggestedReply"] == nil && expired["note"] as? String == "still owed")
        let row = Orchestrator.SessionSelfState(identity: identity, claim: nil, note: nil,
            movedBy: nil, personNeeded: nil, claimReportedAt: nil, claimSettled: false, owed: debt)
        expect("durable codec retains the exact literal suggestion",
            OrchestratorStore.sessionSelfState(from: OrchestratorStore.stored(row))?.owed?.suggestedReply, decoded)
    } else { check("valid suggestion decodes for durable expiry proof", false) }
    var foreign = identity; foreign.conversationID = "another-conversation"
    check("suggestions never cross a reused terminal identity",
        Orchestrator.sessionWorkProjection(identity: foreign, terminalState: .idle).owed == nil)
    let invalid: [(String, Any)] = [
        ("untyped", "允許"), ("unknown version", suggestion.merging(["version": 2]) { _, n in n }),
        ("boolean version", suggestion.merging(["version": true]) { _, n in n }),
        ("empty text", suggestion.merging(["text": " "]) { _, n in n }),
        ("unbounded text", suggestion.merging(["text": String(repeating: "中", count: 1400)]) { _, n in n }),
        ("control text", suggestion.merging(["text": "allow\u{0}"]) { _, n in n }),
        ("foreign conversation", suggestion.merging(["conversationId": "another"]) { _, n in n }),
        ("expired", suggestion.merging(["expiresAt": now.timeIntervalSince1970 - 1]) { _, n in n }),
        ("too distant", suggestion.merging(["expiresAt": expires + 86_400]) { _, n in n }),
        ("fractional expiry", suggestion.merging(["expiresAt": expires + 0.5]) { _, n in n }),
        ("unknown field", suggestion.merging(["autoSend": true]) { _, n in n }),
    ]
    for (name, raw) in invalid {
        if case .refused(_, let code, _, _) = declare(raw) {
            expect("\(name) typed suggestion is refused", code, "suggested_reply_invalid")
        } else { check("\(name) typed suggestion is refused", false) }
    }
    if case .refused(_, let code, _, _) = declare(suggestion, person: false) {
        expect("non-person debt cannot offer approval text", code, "suggested_reply_invalid")
    } else { check("non-person debt cannot offer approval text", false) }
    _ = declare(nil)
    let legacy = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    check("old debt without a typed suggestion remains visible without a fabricated action",
        legacy.owed?["note"] as? String == "請檢查候選" && legacy.owed?["suggestedReply"] == nil)
}
