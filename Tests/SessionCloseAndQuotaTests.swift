import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3



func runSessionCloseAndQuotaTests() {
group("loading an unavailable stored transcript preserves an unrefuted identity") {
    let missing = "/tmp/transcript-rotated-away-\(UUID().uuidString).jsonl"
    let childSession = "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"
    let row: [String: Any] = [
        "id": taskID, "state": "briefed", "kind": "custom", "title": "a task",
        "assistant": "claude", "project_dir": "/tmp", "timeout_minutes": 30,
        "created": Date().timeIntervalSince1970,
        "secret_hash": String(repeating: "0", count: 64), "artifacts": [],
        "child_session": childSession, "transcript": missing,
    ]
    let loaded = Orchestrator.task(from: row)
    expect("loading preserves the session id it could not disprove",
           loaded?.childSessionId, childSession)
    expect("and preserves the unavailable path for the same reason",
           loaded?.transcriptPath, missing)
}

group("closing a root session takes the work it dispatched with it") {
    // The cascade behind `POST /v1/sessions/:id/end`. What is asserted here is the *selection* —
    // which tasks a closing root takes down — because the acting half ends terminal tabs, and a
    // test that opened tabs to close them would be a test nobody dares run twice.
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let root = "8967a1ee-9718-45ed-94d5-c81178870072"
    let stranger = "1c9a4d55-6f31-4b02-8d77-0a2e3c4b5d61"
    let born = Date().timeIntervalSince1970
    let identityDir = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-stored-identities-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: identityDir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: identityDir) }
    func row(_ id: String, _ state: String, rootSession: String?, at: Double,
             child: String? = nil, childSession: String? = nil,
             transcript: String? = nil, parentTask: String? = nil) -> [String: Any] {
        var out: [String: Any] = ["id": id, "state": state, "kind": "custom", "title": "a task",
                                  "assistant": "claude", "project_dir": "/tmp",
                                  "timeout_minutes": 30, "created": at,
                                  "secret_hash": Orchestrator.hash(ofSecret: String(repeating: "a1", count: 32)),
                                  "artifacts": []]
        if let rootSession { out["root_session"] = rootSession }
        if let child { out["child_terminal"] = child }
        if let childSession { out["child_session"] = childSession }
        if let transcript { out["transcript"] = transcript }
        if let parentTask { out["parent_task"] = parentTask }
        return out
    }
    let live = "0f8fad5b-d9cb-469f-a165-70867728950e"
    let alsoLive = "22222222-3333-4444-5555-666666666666"
    let done = "33333333-4444-5555-6666-777777777777"
    let elsewhere = "44444444-5555-6666-7777-888888888888"
    let orphan = "55555555-6666-7777-8888-999999999999"
    let alsoDone = "66666666-7777-8888-9999-aaaaaaaaaaaa"
    let noTab = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
    // The second level. `live` is a child that got as far as being read, so it has a session id
    // of its own to be named by; `done` reported already, and the work it handed on is still
    // running under it — which is the case that decides whether a grandchild belongs to anybody.
    let liveSession = "aaaa1111-2222-3333-4444-555555555555"
    let liveTranscript = identityDir.appendingPathComponent(liveSession + ".jsonl")
    let liveReceipt = #"{"type":"user","message":{"role":"user","content":"You are a Clawdline CHILD agent for task "#
        + live + #"."}}"#
    try! Data((liveReceipt + "\n").utf8).write(to: liveTranscript)
    let below = "88888888-9999-aaaa-bbbb-cccccccccccd"
    let belowDone = "99999999-aaaa-bbbb-cccc-dddddddddddd"
    let belowDead = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    let staleParent = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
    let staleSession = "cccccccc-dddd-eeee-ffff-000000000000"
    let staleTranscript = identityDir.appendingPathComponent(staleSession + ".jsonl")
    let wrongReceipt = #"{"type":"user","message":{"role":"user","content":"You are a Clawdline CHILD agent for task 11111111-2222-3333-4444-555555555555."}}"#
    try! Data((wrongReceipt + "\n").utf8).write(to: staleTranscript)
    let underStale = "dddddddd-eeee-ffff-0000-111111111111"
    let rows: [[String: Any]] = [
        row(alsoLive, "queued", rootSession: root, at: born + 1),
        row(live, "briefed", rootSession: root, at: born, childSession: liveSession,
            transcript: liveTranscript.path),
        row(done, "success", rootSession: root, at: born + 2, child: "%tab-done%"),
        row(below, "briefed", rootSession: liveSession, at: born + 7),
        row(belowDone, "success", rootSession: nil, at: born + 8, child: "%tab-below%",
            parentTask: live),
        row(belowDead, "briefed", rootSession: nil, at: born + 9, parentTask: done),
        row(elsewhere, "briefed", rootSession: stranger, at: born + 3),
        row(orphan, "briefed", rootSession: nil, at: born + 4),
        row(alsoDone, "failure", rootSession: root, at: born + 5, child: "%tab-also%"),
        row(noTab, "spawn_failed", rootSession: root, at: born + 6),
        row(staleParent, "spawn_failed", rootSession: stranger, at: born + 10,
            childSession: staleSession, transcript: staleTranscript.path),
        row(underStale, "briefed", rootSession: staleSession, at: born + 11),
    ]
    let stored = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])) ?? Data()
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? stored.write(to: store, options: .atomic)

    expect("every live task of this root goes, oldest first",
           Orchestrator.liveTasks(dispatchedBy: root), [live, alsoLive])
    check("a task that already finished is not cancelled — `success` is a fact about work that happened",
          !Orchestrator.liveTasks(dispatchedBy: root).contains(done))

    // The other half: the work is over, but the tab it left behind is still indented under this
    // root on the page, and closing the root has to take those with it or it reads as having done
    // nothing. Keyed on the tab rather than on the linger deadline, which does not survive a
    // restart of the app while the tab plainly does.
    expect("a finished task's tab goes with the root too, oldest first",
           Orchestrator.lingeringTasks(dispatchedBy: root), [done, alsoDone])
    check("a task that never got a tab has nothing left to close",
          !Orchestrator.lingeringTasks(dispatchedBy: root).contains(noTab))
    check("and work still running is not collected twice",
          !Orchestrator.lingeringTasks(dispatchedBy: root).contains(live)
              && !Orchestrator.lingeringTasks(dispatchedBy: root).contains(alsoLive))
    expect("another root's tabs are still nobody else's to close",
           Orchestrator.lingeringTasks(dispatchedBy: stranger), [])
    expect("another root's child is nobody else's to cancel",
           Orchestrator.liveTasks(dispatchedBy: stranger), [elsewhere])
    check("and a task with no root named is not swept up with them",
          !Orchestrator.liveTasks(dispatchedBy: root).contains(orphan)
              && !Orchestrator.liveTasks(dispatchedBy: stranger).contains(orphan))
    expect("a session nobody dispatched from cancels nothing",
           Orchestrator.liveTasks(dispatchedBy: "88888888-9999-aaaa-bbbb-cccccccccccc"), [])

    // The level below, which a root's leaving has to reach as well now that a child may dispatch.
    // Two names for the same parent and either is enough: the session id the child calls itself
    // by, which this app only learns once it has read a transcript, and the task the child named
    // as its own — the one identification a Codex child can make at all, since its session id is
    // in a rollout file rather than in the hook notes.
    expect("a grandchild is reached through the session id its parent goes by",
           Orchestrator.liveTasks(under: [live]), [below])
    expect("an unproved session id restored from disk does not claim another task",
           Orchestrator.liveTasks(under: [staleParent]), [])
    expect("and through the task it named as its parent, even one that has already reported",
           Orchestrator.liveTasks(under: [done]), [belowDead])
    expect("a finished grandchild's tab is collected the same way its parent's is",
           Orchestrator.lingeringTasks(under: [live]), [belowDone])
    expect("nobody's parent collects nobody", Orchestrator.liveTasks(under: []), [])
    expect("and a task that named neither is not filed under a stranger",
           Orchestrator.liveTasks(under: [elsewhere]), [])
    check("a task is never collected as its own grandchild",
          !Orchestrator.liveTasks(under: [live]).contains(live))
    check("and the level below is not swept up by the level above's own selection",
          !Orchestrator.liveTasks(dispatchedBy: root).contains(below))

    // The selection a *finishing* task makes, which is the same one — the point being that it is
    // made at all. A child that reported early, timed out, or failed used to leave its
    // grandchildren running for a session that no longer existed: no one waiting for the answer,
    // no one watching the tab, and a row on the list with a `Child` chip and nothing above it.
    expect("a task that ends takes what it handed on with it",
           Orchestrator.liveTasks(under: [live]), [below])
    check("one that handed nothing on has nothing to take",
          Orchestrator.liveTasks(under: [alsoLive]).isEmpty)

    // The identity half. A session that never left a hook note cannot be matched to a task, and
    // the answer to that is *nothing* — the failure worth guarding against is a nil id quietly
    // matching every task that has no root either.
    let unknown = TargetSession(backend: .iterm, id: "%no-such-tab%", name: "x",
                                tty: "/dev/ttys-nobody", windowIndex: 0, tabIndex: 0,
                                assistant: .claude)
    expect("a session with no note of its own takes nothing down with it",
           Orchestrator.cancelChildren(ofRoot: unknown), [])
    check("and a close of it would lose nothing, so the close gate stays open",
          Orchestrator.lostIfClosed(root: unknown).isEmpty)
}

group("what a linger running out decides, one instant at a time") {
    let due = Date(timeIntervalSince1970: 1_787_400_000)
    func child(_ assistant: Assistant?, tty: String = "/dev/ttys7") -> TargetSession {
        TargetSession(backend: .iterm, id: "TAB", name: "x", tty: tty,
                      windowIndex: 0, tabIndex: 0, assistant: assistant)
    }
    func step(now: Date, inventoryComplete: Bool = true,
              inventoryEmpty: Bool = true,
              emptyInventoryAuthoritative: Bool = false, automationReady: Bool = true,
              intervention: Orchestrator.TerminalIntervention? = nil,
              child: TargetSession?, tty: String? = "/dev/ttys7",
              busy: Bool = false) -> Orchestrator.CloseStep {
        Orchestrator.closeStep(now: now, closeAt: due, inventoryComplete: inventoryComplete,
                               inventoryEmpty: inventoryEmpty,
                               emptyInventoryAuthoritative: emptyInventoryAuthoritative,
                               automationReady: automationReady, intervention: intervention,
                               child: child,
                               assistant: .codex, tty: tty, busy: { busy })
    }

    expect("before the deadline, nothing happens",
           step(now: due.addingTimeInterval(-1), child: child(.codex)), .wait)
    expect("at the deadline the tab goes, and the assistant is asked to leave first",
           step(now: due, child: child(.codex)), .close(justTheTab: false))
    expect("an empty tab is taken without a word said into it",
           step(now: due, child: child(nil)), .close(justTheTab: true))
    expect("a child mid-turn is left alone",
           step(now: due.addingTimeInterval(60), child: child(.codex), busy: true), .wait)
    expect("elapsed time never turns a busy assistant into a safe close",
           step(now: due.addingTimeInterval(601), child: child(.codex), busy: true),
           .wait)
    expect("a tab that is somebody else's assistant now is not ours to close",
           step(now: due, child: child(.claude)), .forget)
    expect("nor is one whose tty moved under it",
           step(now: due, child: child(.codex, tty: "/dev/ttys9")), .forget)
    expect("an ordinary empty reading cannot forget the tab",
           step(now: due, child: nil), .wait)
    expect("only an authoritative empty inventory may forget the tab",
           step(now: due, emptyInventoryAuthoritative: true, child: nil), .forget)
    expect("a complete non-empty inventory may prove this one tab absent",
           step(now: due, inventoryEmpty: false, child: nil), .forget)

    // **The one branch that cannot be taken back.** `forget` is permanent — nothing sets the
    // deadline a second time — so it may only be reached from a reading that actually happened.
    // A reading with no terminals in it at all is what the first seconds after launch look like,
    // and what iTerm2 not answering looks like; deciding on one closed nothing and left every
    // finished child's tab standing for the rest of the day.
    expect("an incomplete inventory cannot forget an omitted tab",
           step(now: due, inventoryComplete: false, child: nil), .wait)
    expect("an incomplete inventory cannot repeat an automatic close",
           step(now: due, inventoryComplete: false, child: child(.codex)), .wait)
    expect("a modal-open automation circuit cannot repeat an automatic close",
           step(now: due, automationReady: false, child: child(.codex)), .wait)
    let processFailure = Orchestrator.TerminalIntervention(
        kind: .terminal, message: "Could not read ps")
    expect("a non-modal exact-tty failure cannot retry on a later beat",
           step(now: due, intervention: processFailure, child: child(.codex)), .wait)
    let modalFailure = Orchestrator.TerminalIntervention(
        kind: .iTermModal, message: "iTerm2 needs attention")
    expect("a modal intervention still waits while its circuit is open",
           step(now: due, automationReady: false, intervention: modalFailure,
                child: child(.codex)), .wait)
    expect("a modal intervention gets one decision after fresh inventory recovery",
           step(now: due, automationReady: true, intervention: modalFailure,
                child: child(.codex)), .close(justTheTab: false))
    expect("a later complete inventory re-enables the safe close decision",
           step(now: due, inventoryComplete: true, automationReady: true,
                child: child(.codex)), .close(justTheTab: false))
    expect("unknown capture state never becomes permission to force-close",
           Orchestrator.closeStep(
            now: due, closeAt: due, inventoryComplete: true,
            automationReady: true, child: child(.codex), assistant: .codex,
            tty: "/dev/ttys7", activity: { .unknown }), .wait)
}

group("safe-close activity keeps capture failure distinct from idle") {
    defer { Targets.safeCloseCaptureForTesting = nil }
    let child = TargetSession(backend: .iterm, id: "ACTIVITY-TAB", name: "x",
                              tty: "/dev/ttys79", windowIndex: 0, tabIndex: 0,
                              assistant: .codex)
    Targets.safeCloseCaptureForTesting = { _ in nil }
    expect("capture failure is unknown", Targets.safeCloseActivity(of: child), .unknown)
    Targets.safeCloseCaptureForTesting = { _ in "❯ " }
    expect("a proved prompt is idle", Targets.safeCloseActivity(of: child), .idle)
    Targets.safeCloseCaptureForTesting = { _ in "• Working (3s • esc to interrupt)" }
    expect("a live line is busy", Targets.safeCloseActivity(of: child), .busy)
}

group("linger expiry and explicit root close share one closing admission") {
    let id = "simultaneous-terminal-close"
    defer { Orchestrator.finishTerminalCloseForTesting(id) }
    let resultLock = NSLock()
    var admissions: [Bool] = []
    DispatchQueue.concurrentPerform(iterations: 2) { _ in
        let admitted = Orchestrator.beginTerminalCloseForTesting(id)
        resultLock.lock(); admissions.append(admitted); resultLock.unlock()
    }
    expect("the two simultaneous close paths admit exactly one terminal operation",
           admissions.filter { $0 }.count, 1)
    expect("only one terminal close is in flight",
           Orchestrator.terminalClosesInFlightForTesting(), 1)
    Orchestrator.finishTerminalCloseForTesting(id)
    expect("the closing guard returns to zero", Orchestrator.terminalClosesInFlightForTesting(), 0)
}

group("the automatic close runs closeStep against an inventory it took itself") {
    // The decision table above is only worth having if the close that runs on this Mac is the
    // thing that consults it. It was not: `closeStep` had no production caller at all, and the
    // whole busy/idle/unknown table could be deleted with every check in this file still green.
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    defer {
        Targets.safeCloseInventoryForTesting = nil
        Targets.safeCloseCaptureForTesting = nil
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    let child = TargetSession(backend: .iterm, id: "LINGER-TAB", name: "child",
                              tty: "/dev/ttys81", windowIndex: 0, tabIndex: 0,
                              assistant: .codex, cwd: "/tmp")

    struct Outcome {
        var ends: [Bool] = []
        var closeAt: Date?
        var intervention: String?
    }

    /// One whole automatic close, from the beat's nomination to the record it leaves behind.
    func close(inventory: Targets.Snapshot, screen: String?,
               failure: String? = nil) -> Outcome {
        Orchestrator.forget()
        var task = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
            title: "linger close", assistant: .codex, projectDir: "/tmp",
            timeoutMinutes: 30, created: Date(),
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = Date()
        task.childTerminalId = child.id
        task.childTTY = child.tty
        task.closeAt = Date().addingTimeInterval(-1)
        Orchestrator.holdScheduleTaskForTesting(task)
        // Another group may have left the automation circuit open; this one is about the
        // inventory and the screen, and `closeStep` refuses everything while a modal is up.
        ITerm.completeInventoryForTesting()
        Targets.safeCloseInventoryForTesting = { inventory }
        Targets.safeCloseCaptureForTesting = { _ in screen }
        var ends: [Bool] = []
        _ = Orchestrator.takeChildTab(for: task, childID: child.id,
                                      closeAt: task.closeAt ?? Date(),
                                      end: { _, justTheTab in
                                          ends.append(justTheTab); return failure
                                      })
        check("the close settles", eventually {
            Orchestrator.terminalClosesInFlightForTesting() == 0
        })
        let record = Orchestrator.record(id: task.id)
        let kind = (record?["terminal_intervention"] as? [String: Any])?["code"] as? String
        return Outcome(ends: ends, closeAt: Orchestrator.closeAtForTesting(task.id),
                       intervention: kind)
    }

    var present = Targets.Snapshot()
    present.sessions = [child]
    var incomplete = present
    incomplete.isComplete = false
    incomplete.error = "iTerm would not answer"
    var replaced = Targets.Snapshot()
    replaced.sessions = [TargetSession(backend: .iterm, id: "SOMEBODY-ELSE", name: "other",
                                       tty: "/dev/ttys82", windowIndex: 0, tabIndex: 0,
                                       assistant: .claude, cwd: "/tmp")]
    let emptyButComplete = Targets.Snapshot()

    let busy = close(inventory: present, screen: "• Working (3s • esc to interrupt)")
    check("a child mid-turn is never closed by its own linger", busy.ends.isEmpty)
    check("and its deadline is left standing for a later beat", busy.closeAt != nil)

    let unknown = close(inventory: present, screen: nil)
    check("an unreadable screen is not idle and never reaches the close", unknown.ends.isEmpty)
    check("an unreadable screen leaves the deadline alone", unknown.closeAt != nil)

    let degraded = close(inventory: incomplete, screen: "❯ ")
    check("an incomplete inventory never reaches the close", degraded.ends.isEmpty)
    check("and cannot drop the deadline either", degraded.closeAt != nil)

    let blank = close(inventory: emptyButComplete, screen: "❯ ")
    check("an inventory with no terminals at all proves nothing", blank.ends.isEmpty)
    check("so the tab is not forgotten on it", blank.closeAt != nil)

    let gone = close(inventory: replaced, screen: "❯ ")
    check("a tab absent from a complete inventory is nobody's to close", gone.ends.isEmpty)
    check("and its deadline is dropped rather than retried forever", gone.closeAt == nil)

    let idle = close(inventory: present, screen: "❯ ")
    expect("only a proved idle prompt is closed, and with the quit word", idle.ends, [false])
    check("a successful close clears the deadline exactly then", idle.closeAt == nil)

    let refused = close(inventory: present, screen: "❯ ",
                        failure: "The assistant is still running on /dev/ttys81")
    expect("a refused close still happened once", refused.ends, [false])
    check("a failed close keeps its deadline visible", refused.closeAt != nil)
    expect("and records why a person has to look", refused.intervention,
           "terminal_intervention_required")
}

group("a beat with several lingering children takes one terminal walk, not one each") {
    // The decision has to be fresh, which means a walk; it does not have to be one walk per
    // task. Per task, eight children finishing together was eight iTerm lists plus eight
    // `list-panes` every five seconds, all on the single serial terminal lane a phone's `/send`
    // queues in — the lane this whole change exists to keep clear.
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    defer {
        Targets.safeCloseInventoryForTesting = nil
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    ITerm.completeInventoryForTesting()
    var lingering: [Orchestrator.Task] = []
    for index in 0..<4 {
        var task = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
            title: "batched linger", assistant: .codex, projectDir: "/tmp",
            timeoutMinutes: 30, created: Date(), secretHash: String(repeating: "0", count: 64))
        task.finishedAt = Date()
        task.childTerminalId = "BATCH-TAB-\(index)"
        task.closeAt = Date().addingTimeInterval(-1)
        Orchestrator.holdScheduleTaskForTesting(task)
        lingering.append(task)
    }
    // A complete inventory holding somebody else's tab and none of theirs: every one of them is
    // decided, none of them touches a terminal, and the count below is the whole point.
    var elsewhere = Targets.Snapshot()
    elsewhere.sessions = [TargetSession(backend: .iterm, id: "NOT-OURS", name: "other",
                                        tty: "/dev/ttys95", windowIndex: 0, tabIndex: 0,
                                        assistant: .claude, cwd: "/tmp")]
    let walkLock = NSLock()
    var walks = 0
    Targets.safeCloseInventoryForTesting = {
        walkLock.lock(); walks += 1; walkLock.unlock()
        return elsewhere
    }
    Orchestrator.closeDueChildren(lingering)
    check("every lingering child is decided", eventually {
        Orchestrator.terminalClosesInFlightForTesting() == 0
            && lingering.allSatisfy { Orchestrator.closeAtForTesting($0.id) == nil }
    })
    walkLock.lock(); let taken = walks; walkLock.unlock()
    expect("four due children cost one terminal inventory, not four", taken, 1)
}

group("terminal intervention type follows the returned failure, not unrelated global state") {
    defer { ITerm.completeInventoryForTesting() }
    ITerm.blockAutomationForTesting("iTerm modal timeout")
    let attention = ITerm.automationAttention!
    expect("the actual iTerm circuit refusal is modal",
           Orchestrator.terminalIntervention(for: attention, backend: .iterm).kind,
           .iTermModal)
    expect("a ps failure stays non-modal even while the iTerm circuit is open",
           Orchestrator.terminalIntervention(for: "ps failed", backend: .iterm).kind,
           .terminal)
    expect("a tmux failure never borrows iTerm modal state",
           Orchestrator.terminalIntervention(for: attention, backend: .tmux).kind,
           .terminal)
}

group("a linger survives the restart that lands in the middle of it") {
    let originalLinger = Config.shared.orchestratorChildLinger
    Config.shared.orchestratorChildLinger = 180
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let configuredLinger = Config.shared.orchestratorChildLinger
    Config.shared.orchestratorChildLinger = 180
    defer {
        Config.shared.orchestratorChildLinger = configuredLinger
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Config.shared.orchestratorChildLinger = originalLinger
        Orchestrator.forget()
    }

    // A task exactly as `finalize` leaves one: reported, its tab named, and three minutes on the
    // clock. The app is then replaced under it — which is what ./build.sh does, several times an
    // hour, and is how a child's tab came to stand open until somebody noticed it.
    let id = "cccccccc-cccc-cccc-cccc-cccccccccccc"
    let soon = Date().addingTimeInterval(120)
    let row: [String: Any] = [
        "id": id, "state": "success", "kind": "custom", "title": "a task",
        "assistant": "codex", "project_dir": "/path-that-does-not-exist-clawdline-test",
        "timeout_minutes": 30, "created": Date().timeIntervalSince1970,
        "secret_hash": Orchestrator.hash(ofSecret: "x"), "artifacts": [],
        "child_terminal": "TAB", "child_tty": "/dev/ttys7",
        "finished_at": Date().timeIntervalSince1970,
        "close_at": soon.timeIntervalSince1970,
        "terminal_intervention": [
            "kind": "iterm_modal", "message": "iTerm2 needs attention before safe close",
        ],
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])
    try! data.write(to: store, options: .atomic)
    Orchestrator.forget()
    Orchestrator.resumeAfterRestart()

    expect("the deadline is still the one the last process set",
           Orchestrator.closeAtForTesting(id)?.timeIntervalSince1970, soon.timeIntervalSince1970)
    expect("and a deadline this process sets is written down for the next one",
           Orchestrator.stored(Orchestrator.task(from: row)!)["close_at"] as? Double,
           soon.timeIntervalSince1970)
    let storedIntervention = Orchestrator.stored(
        Orchestrator.task(from: row)!
    )["terminal_intervention"] as? [String: Any]
    expect("terminal intervention kind survives its stored-row round trip",
           storedIntervention?["kind"] as? String, "iterm_modal")
    expect("terminal intervention reason survives its stored-row round trip",
           storedIntervention?["message"] as? String,
           "iTerm2 needs attention before safe close")
    let intervention = (Orchestrator.record(id: id)?["terminal_intervention"]
                        as? [String: Any])?["message"] as? String
    expect("terminal intervention survives restart into the API record", intervention,
           "iTerm2 needs attention before safe close")
    var tmuxRow = row
    tmuxRow["child_backend"] = "tmux"
    tmuxRow["terminal_intervention"] = [
        "kind": "terminal",
        "message": "The assistant is still running; the pane was left open.",
    ]
    let tmuxIntervention = (Orchestrator.recordForTesting(
        Orchestrator.task(from: tmuxRow)!
    )["terminal_intervention"]) as! [String: Any]
    expect("a tmux safe-close failure never names an iTerm dialog",
           tmuxIntervention["code"] as? String, "terminal_intervention_required")
    check("and carries no iTerm app field", tmuxIntervention["app"] == nil)

    // The other half: a deadline that ran out while the app was away. It is not acted on the
    // instant this process starts, because nothing has been read yet — and a tab closed on the
    // strength of an empty reading is a tab closed for no reason.
    let stale = Date().addingTimeInterval(-4 * 3600)
    var staleRow = row
    staleRow["close_at"] = stale.timeIntervalSince1970
    let staleData = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [staleRow]])
    try! staleData.write(to: store, options: .atomic)
    Orchestrator.forget()
    let rearmStartedAt = Date()
    Orchestrator.resumeAfterRestart()
    let rearmFinishedAt = Date()
    let rearmed = Orchestrator.closeAtForTesting(id)
    check("a deadline that ran out while the app was away gets a breath, not the axe",
          rearmed.map {
              $0 >= rearmStartedAt.addingTimeInterval(Orchestrator.restartGrace)
                  && $0 <= rearmFinishedAt.addingTimeInterval(Orchestrator.restartGrace)
          } == true,
          "got \(String(describing: rearmed))")

    // A Mac that has said a child's tab is never closed for it does not inherit one either.
    Config.shared.orchestratorChildLinger = -1
    Orchestrator.forget()
    Orchestrator.resumeAfterRestart()
    check("and none of it happens where the linger has been turned off",
          Orchestrator.closeAtForTesting(id) == nil)

    // A schedule's explicit close policy is not a default. If it created the deadline before the
    // restart, the global keep-tabs preference cannot silently reverse that per-schedule choice.
    var scheduledRow = staleRow
    scheduledRow["schedule_id"] = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
    scheduledRow["schedule_close_tab"] = "always"
    scheduledRow["schedule_notify_failure"] = true
    let scheduledData = try! JSONSerialization.data(
        withJSONObject: ["version": 1, "tasks": [scheduledRow]])
    try! scheduledData.write(to: store, options: .atomic)
    Orchestrator.forget()
    Orchestrator.resumeAfterRestart()
    check("an explicit schedule deadline survives the opposite global preference",
          Orchestrator.closeAtForTesting(id) != nil)
    Config.shared.orchestratorChildLinger = 180

    // And the tab is still only closed on what *this* process can see: the record carries the
    // deadline across, never the belief that the tab is still there.
    let task = Orchestrator.task(from: staleRow)!
    expect("the restored deadline still waits on a reading",
           Orchestrator.closeStep(now: Date(), closeAt: Date().addingTimeInterval(-1),
                                  inventoryComplete: false, automationReady: true, child: nil,
                                  assistant: task.assistant, tty: task.childTTY, busy: { false }),
           .wait)
}

group("the Session info card is read off the files, and says unknown rather than 0%") {
    // The porcelain, counted. A partial add is under both headings, as `git status` lists it.
    let porcelain = """
    # branch.oid d5c61e9f91c46a77
    # branch.head main
    # branch.upstream origin/main
    # branch.ab +2 -3
    1 .M N... 100644 100644 100644 aaaaaaa aaaaaaa Sources/RemoteServer.swift
    1 MM N... 100644 100644 100644 bbbbbbb ccccccc Sources/Foo.swift
    1 A. N... 000000 100644 100644 0000000 eeeeeee Sources/Added.swift
    2 R. N... 100644 100644 100644 1111111 2222222 R100 Sources/New.swift\tSources/Old.swift
    ? Notes/new file.txt
    ? Another
    u UU N... 100644 100644 100644 100644 3333333 4444444 5555555 Sources/Conflict.swift
    """
    let files = SessionInfo.parseStatus(porcelain)
    expect("the branch is read", files.branch, "main")
    expect("so is the object id", files.head, "d5c61e9f91c46a77")
    expect("ahead", files.ahead, 2)
    expect("behind", files.behind, 3)
    expect("staged counts the index column", files.staged, 3)        // MM, A., R.
    expect("unstaged counts the worktree column", files.unstaged, 2) // .M, MM
    expect("untracked is a count of files, not a flag", files.untracked, 2)
    expect("a conflict is its own count", files.conflict, 1)
    let fresh = SessionInfo.parseStatus("# branch.oid (initial)\n# branch.head (detached)\n")
    expect("an initial commit has no head", fresh.head, "")
    expect("a detached head has no branch", fresh.branch, "")
    expect("and an empty tree counts nothing", fresh, SessionInfo.Files())

    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
    let now = Date(timeIntervalSince1970: 1_787_400_000)

    // **Claude writes no percentage into a transcript.** What it writes is a `quotaLimits`
    // block on the turn a window ran out, and that turn's model is `<synthetic>`.
    let turn = line(["type": "assistant", "timestamp": "2026-08-22T16:41:00.000Z",
                     "message": ["model": "claude-fable-5", "usage": ["input_tokens": 1]]])
    let refusal = line(["type": "assistant", "timestamp": "2026-08-22T16:41:57.576Z",
                        "message": ["model": "<synthetic>"],
                        "quotaLimits": ["status": "rejected", "resetsAt": 1_787_417_400,
                                        "rateLimitType": "five_hour"]])
    let hit = SessionInfo.claudeLimits(transcript: Data((turn + "\n" + refusal + "\n").utf8), now: now)
    expect("a refusal is a spent window", hit.limits.windows.map(\.name), ["5h"])
    expect("at a hundred percent", hit.limits.windows.first?.usedPercent, 100)
    expect("with when it comes back", hit.limits.windows.first?.resetsAt, 1_787_417_400)
    expect("and marked as hit", hit.limits.windows.first?.hit, true)
    expect("stamped with the record's own time", hit.limits.at, 1787416917)
    expect("the synthetic turn is not the model; the one before it is", hit.model, "claude-fable-5")

    let later = Date(timeIntervalSince1970: 1_787_500_000)
    let past = SessionInfo.claudeLimits(transcript: Data((turn + "\n" + refusal + "\n").utf8), now: later)
    check("a refusal whose reset has passed says nothing about now", past.limits.windows.isEmpty)
    let quiet = SessionInfo.claudeLimits(transcript: Data((turn + "\n").utf8), now: now)
    check("no record at all is unknown, not zero", quiet.limits.windows.isEmpty)
    expect("but the model is still read", quiet.model, "claude-fable-5")
    check("an empty file is unknown too", SessionInfo.claudeLimits(transcript: Data(), now: now).limits.windows.isEmpty)

    // The status line's shape, should a build ever write it down: read without a code change.
    let rates = line(["type": "assistant", "timestamp": "2026-08-22T16:41:00Z",
                      "rate_limits": ["five_hour": ["used_percentage": 24, "resets_at": 1_787_410_000],
                                      "seven_day": ["used_percentage": 12.5, "resets_at": 1_787_900_000]]])
    let pct = SessionInfo.claudeLimits(transcript: Data((rates + "\n").utf8), now: now)
    expect("the status line's shape reads as two windows", pct.limits.windows.map(\.name), ["5h", "7d"])
    expect("with their percentages", pct.limits.windows.map { $0.usedPercent ?? -1 }, [24, 12.5])
    expect("neither of which is hit", pct.limits.windows.map(\.hit), [false, false])
    expect("a timestamp without fractions parses too", pct.limits.at, 1787416860)

    // The same shape, from the file the status line keeps for exactly this reader. A window
    // whose reset has passed is dropped rather than shown — it was true of a window now over.
    let cacheText = """
    {"at": 1787537546, "session_id": "s", "rate_limits": {
      "five_hour": {"used_percentage": 15, "resets_at": 1787545000},
      "seven_day": {"used_percentage": 8, "resets_at": 1788000000}}}
    """
    let kept = SessionInfo.claudeLimits(cache: Data(cacheText.utf8), now: Date(timeIntervalSince1970: 1_787_537_600))
    expect("the status line's file reads as two windows", kept.windows.map(\.name), ["5h", "7d"])
    expect("with the percentages it was handed", kept.windows.map { $0.usedPercent ?? -1 }, [15, 8])
    expect("and when it wrote them", kept.at, 1787537546)
    let stale = SessionInfo.claudeLimits(cache: Data(cacheText.utf8), now: Date(timeIntervalSince1970: 1_787_600_000))
    expect("a window past its reset is gone, the other stays", stale.windows.map(\.name), ["7d"])
    check("a file that is not JSON is unknown, not a crash", SessionInfo.claudeLimits(cache: Data("{".utf8)).windows.isEmpty)
    check("and a directory without one is the same",
          SessionInfo.claudeLimits(cacheDirectory: URL(fileURLWithPath: "/nonexistent-" + UUID().uuidString)).windows.isEmpty)

    // The transcript's refusal is the stronger word about its window; the cache fills the rest.
    let refused = SessionInfo.Limits(windows: [SessionInfo.Window(name: "5h", usedPercent: 100, resetsAt: 1_787_545_000, hit: true)], at: 1_787_540_000)
    let both = SessionInfo.merged(transcript: refused, cache: kept)
    expect("a hit window replaces the cache's row of that name", both.windows.map { $0.usedPercent ?? -1 }, [100, 8])
    expect("and is the one marked hit", both.windows.map(\.hit), [true, false])
    expect("the cache alone is the answer when the transcript says nothing",
           SessionInfo.merged(transcript: SessionInfo.Limits(), cache: kept).windows.map(\.name), ["5h", "7d"])

    // Codex keeps the running answer on every `token_count`; the newest one is the state.
    let tokenCount = line(["timestamp": "2026-08-23T16:49:47.975Z", "type": "event_msg",
                           "payload": ["type": "token_count",
                                       "info": ["total_token_usage": ["input_tokens": 10]],
                                       "rate_limits": ["primary": ["used_percent": 28.0, "window_minutes": 10080,
                                                                   "resets_at": 1_788_104_505],
                                                       "secondary": NSNull()]]])
    let older = line(["timestamp": "2026-08-23T16:00:00.000Z", "type": "event_msg",
                      "payload": ["type": "token_count", "info": [:],
                                  "rate_limits": ["primary": ["used_percent": 3.0, "window_minutes": 300,
                                                              "resets_at": 1]]]])
    let codex = SessionInfo.codexLimits(rollout: Data((older + "\n" + tokenCount + "\n").utf8))
    expect("the newest token_count is the answer", codex.windows.map(\.name), ["7d"])
    expect("with its percentage", codex.windows.first?.usedPercent, 28)
    expect("and its reset", codex.windows.first?.resetsAt, 1_788_104_505)
    expect("a null secondary is not a window", codex.windows.count, 1)
    expect("stamped", codex.at, 1787503787)
    check("a rollout with no token_count is unknown",
          SessionInfo.codexLimits(rollout: Data("{\"type\":\"session_meta\"}\n".utf8)).windows.isEmpty)

    // Context is the current turn against the model's window, not the cumulative token total.
    // The two can be orders of magnitude apart in a long session, which is exactly why the web
    // status line must not substitute one for the other.
    let contextCount = line([
        "timestamp": "2026-08-23T16:50:00.000Z", "type": "event_msg",
        "payload": ["type": "token_count",
                    "info": ["total_token_usage": ["total_tokens": 47_300_000],
                             "last_token_usage": ["total_tokens": 103_360],
                             "model_context_window": 258_400],
                    "rate_limits": [:]]])
    let context = SessionInfo.codexContext(rollout: Data((tokenCount + "\n" + contextCount + "\n").utf8))
    expect("context uses the newest turn, not cumulative tokens", context?.usedTokens, 103_360)
    expect("it keeps the model window for an exact tooltip", context?.windowTokens, 258_400)
    expect("and reports its percentage", context?.usedPercent, 40)
    check("a token count without a model window is unknown context",
          SessionInfo.codexContext(rollout: Data((tokenCount + "\n").utf8)) == nil)

    // Claude's status-line cache owns the window and Claude Code's own cost, while the
    // transcript owns the freshest current-turn usage. A sidechain is a different conversation
    // and must not replace the parent turn merely because it was written later.
    // The fixture's window is 250,000 on purpose: no row of `claudeWindow` can produce it, so an
    // implementation that dropped the cache and fell through to the table would fail these
    // rather than pass them by coincidence.
    let claudeCache = Data("""
    {"at":1787894229,"session_id":"s","context_window":{
      "context_window_size":250000,"total_input_tokens":162752,"used_percentage":65},
      "cost":{"total_cost_usd":6.43}}
    """.utf8)
    let parentTurn = line([
        "type": "assistant", "isSidechain": false,
        "message": ["model": "claude-fable-5", "usage": [
            "input_tokens": 2, "cache_read_input_tokens": 160_655,
            "cache_creation_input_tokens": 1_620]]])
    let laterSidechain = line([
        "type": "assistant", "isSidechain": true,
        "message": ["model": "claude-fable-5", "usage": [
            "input_tokens": 9, "cache_read_input_tokens": 900_000,
            "cache_creation_input_tokens": 90_000]]])
    let claudeTranscript = Data((parentTurn + "\n" + laterSidechain + "\n").utf8)
    let claudeContext = SessionInfo.claudeContext(
        transcript: claudeTranscript, cache: claudeCache, model: "claude-fable-5")
    expect("Claude context uses the parent transcript turn, not a later sidechain",
           claudeContext?.usedTokens, 162_277)
    expect("and takes its exact window from the status-line cache",
           claudeContext?.windowTokens, 250_000)
    expect("and derives the live percentage from those two readings",
           claudeContext?.usedPercent, Double(162_277) * 100 / Double(250_000))

    // `<synthetic>` is the turn Claude Code writes when the provider refused, and its `usage` is
    // a complete set of zeroes. Read as a turn it says the conversation is empty at the exact
    // moment it is full, so the reader has to step over it to the last real one.
    let syntheticTurn = line([
        "type": "assistant", "isSidechain": false,
        "message": ["model": "<synthetic>", "usage": [
            "input_tokens": 0, "cache_read_input_tokens": 0,
            "cache_creation_input_tokens": 0]]])
    expect("a <synthetic> refusal does not read as an empty context",
           SessionInfo.claudeContext(
             transcript: Data((parentTurn + "\n" + syntheticTurn + "\n").utf8),
             cache: claudeCache, model: "claude-fable-5")?.usedTokens,
           162_277)

    let tableContext = SessionInfo.claudeContext(
        transcript: Data((parentTurn + "\n").utf8), cache: nil,
        model: "claude-haiku-4-5-20251001")
    expect("without a cache, a dated known model gets the table's window",
           tableContext?.windowTokens, 200_000)
    check("and that window is marked a guess", tableContext?.windowIsExact == false)
    check("while the cache's window is not", claudeContext?.windowIsExact == true)
    check("without a cache, an unknown model does not invent a window",
          SessionInfo.claudeContext(transcript: Data((parentTurn + "\n").utf8), cache: nil,
                                    model: "claude-unknown-9") == nil)

    let cacheOnly = SessionInfo.claudeContext(
        transcript: Data("{\"type\":\"user\"}\n".utf8), cache: claudeCache,
        model: "claude-fable-5")
    expect("before the first assistant turn, the cache supplies used tokens",
           cacheOnly?.usedTokens, 162_752)
    expect("and its exact window", cacheOnly?.windowTokens, 250_000)
    expect("and a percentage derived from the same pair", cacheOnly?.usedPercent,
           Double(162_752) * 100 / Double(250_000))
    // A known model on purpose: with an unknown one the window guard answers nil before a single
    // cache byte is interpreted, and the assertion would hold against a reader that cannot
    // survive a broken file at all.
    check("a malformed cache neither throws nor fabricates context",
          SessionInfo.claudeContext(transcript: Data(), cache: Data("{".utf8),
                                    model: "claude-fable-5")?.usedTokens == nil)
    check("an empty cache is unknown too",
          SessionInfo.claudeContext(transcript: Data(), cache: Data(),
                                    model: "claude-fable-5")?.usedTokens == nil)
    // A number this reader cannot represent is an absent number, not a dead server: `Int(1e30)`
    // traps, and this file is written by a different project.
    check("a cache window past Int.max is absent rather than fatal",
          SessionInfo.claudeContext(
            transcript: Data((parentTurn + "\n").utf8),
            cache: Data("{\"context_window\":{\"context_window_size\":1e30}}".utf8),
            model: "claude-fable-5")?.windowTokens == 1_000_000)
    expect("the cache exposes Claude Code's own session cost",
           SessionInfo.claudeCost(cache: claudeCache), 6.43)

    // §2.4: once the primary bucket is full, the *next* token_count often falls back to an
    // unnamed "premium" credits bucket with no window at all — `limit_id` turns from "codex" to
    // "premium", `primary`/`secondary` are both null, `credits.balance` is "0". The newest record
    // is still the one with no window in it, so a reader that simply returns the newest record's
    // windows answers "unknown" for an account that is, in fact, still at 100%.
    let fullWindow = line(["timestamp": "2026-08-26T11:13:50.177Z", "type": "event_msg",
                           "payload": ["type": "token_count", "info": [:],
                                       "rate_limits": ["primary": ["used_percent": 100.0,
                                                                   "window_minutes": 10080,
                                                                   "resets_at": 1_788_272_000],
                                                       "secondary": NSNull()]]])
    let depletedLine = line(["timestamp": "2026-08-26T11:13:50.169Z", "type": "event_msg",
                             "payload": ["type": "token_count", "info": [:],
                                         "rate_limits": ["limit_id": "premium",
                                                         "primary": NSNull(), "secondary": NSNull(),
                                                         "credits": ["has_credits": false,
                                                                    "unlimited": false,
                                                                    "balance": "0"]]]])
    let afterDepletion = SessionInfo.codexLimits(
        rollout: Data((fullWindow + "\n" + depletedLine + "\n").utf8))
    expect("a record with no named window does not erase the last real one",
           afterDepletion.windows.map(\.name), ["7d"])
    expect("its percentage is what the window last actually said",
           afterDepletion.windows.first?.usedPercent, 100)
    expect("and it is still marked hit", afterDepletion.windows.first?.hit, true)
    expect("the skipped record's limit_id is remembered rather than discarded",
           afterDepletion.depleted?.limitID, "premium")
    expect("so is whether it still had credits to fall back on",
           afterDepletion.depleted?.hasCredits, false)

    // A record with no named window *and* no `limit_id`/`credits` at all — an older Codex, or a
    // renamed field — reads as nil on both, not the empty-string/false a default used to produce.
    let bareRates = line(["timestamp": "2026-08-26T12:00:00.000Z", "type": "event_msg",
                          "payload": ["type": "token_count", "info": [:],
                                      "rate_limits": ["primary": NSNull(), "secondary": NSNull()]]])
    let bareDepletion = SessionInfo.codexLimits(rollout: Data((bareRates + "\n").utf8))
    expect("no limit_id at all is nil, not \"\"", bareDepletion.depleted?.limitID, nil)
    expect("no credits object at all is nil, not false", bareDepletion.depleted?.hasCredits, nil)

    expect("five hours", SessionInfo.windowName(minutes: 300), "5h")
    expect("seven days", SessionInfo.windowName(minutes: 10080), "7d")
    expect("a day", SessionInfo.windowName(minutes: 1440), "1d")
    expect("an odd number of minutes stays minutes", SessionInfo.windowName(minutes: 90), "90m")
    expect("Claude's names map to the same words", SessionInfo.windowName(claudeType: "seven_day"), "7d")
    expect("and an unknown one is passed through rather than invented", SessionInfo.windowName(claudeType: "monthly"), "monthly")

    // The wire shape. Absent, not zeroed, where nothing was known.
    var usage = Orchestrator.Usage()
    usage.input = 10; usage.output = 20; usage.cacheRead = 30; usage.cacheWrite = 40; usage.total = 100
    usage.model = "claude-fable-5"; usage.costUsd = 0.1234
    let started = Date(timeIntervalSince1970: 1_787_390_000)
    let payload = SessionInfo.payload(
        id: "ABC", assistant: .claude, sessionId: "s-1", model: "claude-fable-5", cwd: "/tmp/x",
        startedAt: started, now: now, usage: usage,
        context: SessionInfo.Context(usedPercent: 40, usedTokens: 103_360, windowTokens: 258_400),
        limits: hit.limits, files: files,
        deploy: [["label": "ci", "url": "https://x", "kind": "deploy", "state": "ok", "local": false]])
    let session = payload["session"] as? [String: Any]
    expect("the session's id", session?["id"] as? String, "ABC")
    expect("its assistant", session?["assistant"] as? String, "claude")
    expect("its model", session?["model"] as? String, "claude-fable-5")
    expect("its directory", session?["cwd"] as? String, "/tmp/x")
    expect("when it started", session?["startedAt"] as? Int, 1_787_390_000)
    expect("and how long that is", session?["seconds"] as? Int, 10_000)
    let counts = payload["usage"] as? [String: Any]
    expect("the totals", counts?["total"] as? Int, 100)
    expect("the cache halves are separate", counts?["cacheWrite"] as? Int, 40)
    expect("and the cost", counts?["costUsd"] as? Double, 0.1234)
    let officialPayload = SessionInfo.payload(
        id: "ABC", assistant: .claude, sessionId: "s-1", model: "claude-fable-5", cwd: nil,
        startedAt: nil, usage: usage,
        costOverrideUsd: SessionInfo.claudeCost(cache: claudeCache),
        limits: SessionInfo.Limits(), files: nil, deploy: [])
    expect("Claude Code's own cache cost replaces the computed estimate",
           (officialPayload["usage"] as? [String: Any])?["costUsd"] as? Double, 6.43)
    let estimatedPayload = SessionInfo.payload(
        id: "ABC", assistant: .claude, sessionId: "s-1", model: "claude-fable-5", cwd: nil,
        startedAt: nil, usage: usage,
        costOverrideUsd: SessionInfo.claudeCost(cache: nil),
        limits: SessionInfo.Limits(), files: nil, deploy: [])
    expect("without that cache figure, the computed estimate stands",
           (estimatedPayload["usage"] as? [String: Any])?["costUsd"] as? Double, 0.1234)
    let contextPayload = payload["context"] as? [String: Any]
    expect("the context percentage is on the wire", contextPayload?["usedPercent"] as? Double, 40)
    expect("with its current token count", contextPayload?["usedTokens"] as? Int, 103_360)
    expect("and model window", contextPayload?["windowTokens"] as? Int, 258_400)
    // A window nobody stated is a table row, and a table row read back as `162,277 / 1,000,000`
    // would be a guess quoted as a measurement. The percentage still goes; the window does not.
    let guessedPayload = SessionInfo.payload(
        id: "ABC", assistant: .claude, sessionId: "s-1", model: "claude-fable-5", cwd: nil,
        startedAt: nil, usage: nil,
        context: SessionInfo.Context(usedPercent: 40, usedTokens: 400_000,
                                     windowTokens: 1_000_000, windowIsExact: false),
        limits: SessionInfo.Limits(), files: nil, deploy: [])
    let guessed = guessedPayload["context"] as? [String: Any]
    expect("a guessed window keeps its percentage", guessed?["usedPercent"] as? Double, 40)
    check("but does not publish the window it guessed", guessed?["windowTokens"] == nil)
    let plan = payload["limits"] as? [String: Any]
    let windows = plan?["windows"] as? [[String: Any]]
    expect("one window", windows?.count, 1)
    expect("named", windows?.first?["name"] as? String, "5h")
    expect("marked hit", windows?.first?["hit"] as? Bool, true)
    expect("and dated", plan?["at"] as? Int, 1787416917)
    let tree = payload["files"] as? [String: Any]
    expect("the tree, counted", tree?["staged"] as? Int, 3)
    expect("with its branch", tree?["branch"] as? String, "main")
    expect("the deploy rows pass through untouched",
           (payload["deploy"] as? [[String: Any]])?.first?["state"] as? String, "ok")
    check("and it is JSON", JSONSerialization.isValidJSONObject(payload))

    let bare = SessionInfo.payload(id: "X", assistant: nil, sessionId: nil, model: nil, cwd: nil,
                                   startedAt: nil, now: now, usage: nil,
                                   limits: SessionInfo.Limits(), files: nil, deploy: [])
    check("no transcript is no usage, not zero usage", bare["usage"] == nil)
    check("no repository is no count, not a clean one", bare["files"] == nil)
    check("no start is no age", (bare["session"] as? [String: Any])?["seconds"] == nil)
    expect("unknown limits are an empty list the page can draw as unknown",
           ((bare["limits"] as? [String: Any])?["windows"] as? [[String: Any]])?.count, 0)
    check("and it is JSON too", JSONSerialization.isValidJSONObject(bare))

    // The route is a read like the others: a token, or nothing.
    expect("the route needs a paired device",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/sessions/nope/info")).status, 401)
    // And the one `git` it runs answers nothing outside a repository rather than a clean tree.
    check("a directory that is not a repository has no count",
          SessionInfo.files(cwd: NSTemporaryDirectory()) == nil)
}

group("an assistant's quota reads as one of four values, and ages by its own rule") {
    let now = Date(timeIntervalSince1970: 1_787_745_138)
    let threshold = 85.0

    func window(_ name: String, _ pct: Double?, resetsAt: Int? = 1_787_900_000,
               hit: Bool = false) -> SessionInfo.Window {
        SessionInfo.Window(name: name, usedPercent: pct, resetsAt: resetsAt, hit: hit)
    }

    // §A.1: the tightest live window decides, and a window whose reset has already passed does
    // not count as live at all.
    expect("no windows at all is unknown",
           AssistantQuota.availability(from: [], now: now, lowThreshold: threshold), .unknown)
    expect("a window under the low threshold is ok",
           AssistantQuota.availability(from: [window("5h", 40)], now: now, lowThreshold: threshold),
           .ok)
    expect("at or over the low threshold is low",
           AssistantQuota.availability(from: [window("5h", 85)], now: now, lowThreshold: threshold),
           .low)
    expect("at 100% is exhausted even with hit left false",
           AssistantQuota.availability(from: [window("5h", 100)], now: now, lowThreshold: threshold),
           .exhausted)
    expect("hit alone is exhausted even at a lower percentage",
           AssistantQuota.availability(from: [window("5h", 60, hit: true)], now: now,
                                       lowThreshold: threshold),
           .exhausted)
    expect("the tighter of two windows decides",
           AssistantQuota.availability(from: [window("5h", 40), window("7d", 90)], now: now,
                                       lowThreshold: threshold),
           .low)
    expect("a window whose own reset has passed is not live, and is not the answer",
           AssistantQuota.availability(from: [window("5h", 100, resetsAt: 1_787_000_000, hit: true)],
                                       now: now, lowThreshold: threshold),
           .unknown)

    // §A.1's Codex-only rule needs both halves: the depleted record's own shape, and a last named
    // window that was already almost full.
    func depleted(_ limitID: String = "premium", hasCredits: Bool = false) -> SessionInfo.Limits.Depleted {
        SessionInfo.Limits.Depleted(limitID: limitID, hasCredits: hasCredits, at: 1_787_745_000)
    }
    check("no depleted record at all is not exhausted",
          AssistantQuota.codexCreditsDepleted(nil, lastNamedUsedPercent: 100) == false)
    check("credits still available is not exhausted",
          AssistantQuota.codexCreditsDepleted(depleted(hasCredits: true), lastNamedUsedPercent: 99) == false)
    check("the ordinary named bucket falling back to itself proves nothing",
          AssistantQuota.codexCreditsDepleted(depleted("codex"), lastNamedUsedPercent: 99) == false)
    check("a last window nowhere near full is not exhausted, whatever the bucket says",
          AssistantQuota.codexCreditsDepleted(depleted(), lastNamedUsedPercent: 40) == false)
    check("no last named window at all is not exhausted — there is nothing to correlate with",
          AssistantQuota.codexCreditsDepleted(depleted(), lastNamedUsedPercent: nil) == false)
    check("premium, no credits, and a last window at 95% or more is exhausted",
          AssistantQuota.codexCreditsDepleted(depleted(), lastNamedUsedPercent: 95) == true)
    check("and comfortably so at 100%",
          AssistantQuota.codexCreditsDepleted(depleted(), lastNamedUsedPercent: 100) == true)
    // A missing `limit_id` or `credits` is "the condition is not established", not "it is
    // established and unfavourable" — the field being absent must not read as the more alarming
    // of the two readings a present-but-different value would allow.
    check("no limit_id at all is not exhausted, whatever credits says",
          AssistantQuota.codexCreditsDepleted(
            SessionInfo.Limits.Depleted(limitID: nil, hasCredits: false, at: 1_787_745_000),
            lastNamedUsedPercent: 100) == false)
    check("no has_credits at all is the same, whatever limit_id says",
          AssistantQuota.codexCreditsDepleted(
            SessionInfo.Limits.Depleted(limitID: "premium", hasCredits: nil, at: 1_787_745_000),
            lastNamedUsedPercent: 100) == false)
    check("neither field present at all is not exhausted",
          AssistantQuota.codexCreditsDepleted(
            SessionInfo.Limits.Depleted(limitID: nil, hasCredits: nil, at: 1_787_745_000),
            lastNamedUsedPercent: 100) == false)

    // §A.2: 5% of a window's own length, clamped 15 minutes–6 hours.
    expect("a five-hour window clamps to the 15-minute floor",
           AssistantQuota.staleAfter(windowMinutes: 300), 15 * 60)
    expect("a seven-day window clamps to the 6-hour ceiling",
           AssistantQuota.staleAfter(windowMinutes: 10_080), 6 * 3_600)
    expect("a one-day window is inside both clamps: 5% of it, unclamped",
           AssistantQuota.staleAfter(windowMinutes: 1_440), 4_320)

    // §A.2's decay: each state ages by its own rule rather than sharing one TTL.
    let recentExhausted = AssistantQuota(assistant: .codex, installed: true, loggedIn: nil, plan: nil,
                                         availability: .exhausted, source: .observed,
                                         observedAt: Int(now.timeIntervalSince1970) - 60,
                                         resetsAt: Int(now.timeIntervalSince1970) + 3_600,
                                         detail: "x", windows: [window("7d", 100, hit: true)])
    let stillExhausted = AssistantQuota.decayed(recentExhausted, now: now)
    expect("exhausted does not expire before its own resets_at",
           stillExhausted.availability, .exhausted)
    check("and it is not marked stale on the way there — exhausted has no such state",
          stillExhausted.stale == false)

    var pastReset = recentExhausted
    pastReset.resetsAt = Int(now.timeIntervalSince1970) - 1
    let afterReset = AssistantQuota.decayed(pastReset, now: now)
    expect("once resets_at has passed, exhausted becomes unknown rather than ok",
           afterReset.availability, .unknown)
    check("with nothing carried over — a new window has no reading of its own yet",
          afterReset.windows.isEmpty && afterReset.observedAt == nil && afterReset.resetsAt == nil)

    // A 5h window's staleAfter is 900s (the 15-minute floor); 4,000s old is well past it.
    let oldLow = AssistantQuota(assistant: .codex, installed: true, loggedIn: nil, plan: nil,
                                availability: .low, source: .observed,
                                observedAt: Int(now.timeIntervalSince1970) - 4_000, resetsAt: nil,
                                detail: "x", windows: [window("5h", 90)])
    let agedLow = AssistantQuota.decayed(oldLow, now: now)
    expect("low stays low however old", agedLow.availability, .low)
    check("but is marked stale past its window's staleAfter", agedLow.stale)

    let freshOk = AssistantQuota(assistant: .claude, installed: true, loggedIn: nil, plan: nil,
                                 availability: .ok, source: .observed,
                                 observedAt: Int(now.timeIntervalSince1970) - 30, resetsAt: nil,
                                 detail: "x", windows: [window("5h", 10)])
    let stillOk = AssistantQuota.decayed(freshOk, now: now)
    expect("a fresh ok stays ok", stillOk.availability, .ok)
    check("and is not stale yet", stillOk.stale == false)

    var oldOk = freshOk
    oldOk.observedAt = Int(now.timeIntervalSince1970) - 4_000
    let decayedOk = AssistantQuota.decayed(oldOk, now: now)
    expect("an old ok decays to unknown rather than staying ok", decayedOk.availability, .unknown)
    expect("keeping what it was, so a client can say so", decayedOk.lastKnown, .ok)

    let noSignal = AssistantQuota(assistant: .claude, installed: true, loggedIn: nil, plan: nil,
                                  availability: .unknown, source: .observed, observedAt: nil,
                                  resetsAt: nil, detail: "x", windows: [])
    expect("unknown with no observedAt is returned unchanged rather than crashing",
           AssistantQuota.decayed(noSignal, now: now).availability, .unknown)

    // The one sentence an API client can print as-is.
    expect("no windows and nothing better to say", AssistantQuota.detail(windows: [], availability: .unknown), "no signal yet")
    expect("no windows but a last known state",
           AssistantQuota.detail(windows: [], availability: .unknown, lastKnown: .ok),
           "no fresh signal; last known ok")
    expect("credits exhausted with no windows names the bucket, not a percentage",
           AssistantQuota.detail(windows: [], availability: .exhausted, creditsExhausted: true),
           "premium credits exhausted; no windows reported")
    expect("windows are joined name and percentage",
           AssistantQuota.detail(windows: [window("5h", 4, resetsAt: nil), window("7d", 69, resetsAt: nil)],
                                 availability: .ok, now: now),
           "5h 4%, 7d 69%")
    expect("an exhausted window's detail says when it resets",
           AssistantQuota.detail(windows: [window("7d", 100, resetsAt: Int(now.timeIntervalSince1970) + 450_000, hit: true)],
                                 availability: .exhausted, now: now),
           "7d 100%; resets in 5d5h")
    expect("credits exhausted alongside real windows still names the bucket",
           AssistantQuota.detail(windows: [window("7d", 100, resetsAt: nil, hit: true)],
                                 availability: .exhausted, creditsExhausted: true, now: now),
           "7d 100%; premium credits exhausted")

    expect("under a minute still reads as a minute rather than nothing",
           AssistantQuota.formatDuration(seconds: 5), "1m")
    expect("minutes alone", AssistantQuota.formatDuration(seconds: 90), "1m")
    expect("hours and minutes", AssistantQuota.formatDuration(seconds: 3_600 * 2 + 60 * 5), "2h5m")
    expect("an exact number of hours drops the minutes", AssistantQuota.formatDuration(seconds: 3_600 * 6), "6h")
    expect("days and hours", AssistantQuota.formatDuration(seconds: 86_400 * 5 + 3_600 * 2), "5d2h")
    expect("an exact number of days drops the hours", AssistantQuota.formatDuration(seconds: 86_400 * 3), "3d")

    // §B.3: identity probes read login state, never a quota number — and the wire shapes are
    // exactly what the CLIs themselves print (Q1 design §1.6/§2.1).
    expect("claude auth status is already JSON",
           AssistantQuota.parseClaudeAuthStatus(
               "{\"loggedIn\":true,\"authMethod\":\"claude.ai\",\"subscriptionType\":\"max\"}"),
           AssistantQuota.Identity(loggedIn: true, plan: "max"))
    expect("logged out carries no plan",
           AssistantQuota.parseClaudeAuthStatus("{\"loggedIn\":false}"),
           AssistantQuota.Identity(loggedIn: false, plan: nil))
    check("text that is not JSON at all answers nothing rather than guessing",
          AssistantQuota.parseClaudeAuthStatus("not json") == nil)
    check("an empty string is the same", AssistantQuota.parseClaudeAuthStatus("") == nil)

    expect("codex login status is plain text, not JSON",
           AssistantQuota.parseCodexLoginStatus("Logged in using ChatGPT\n"),
           AssistantQuota.Identity(loggedIn: true, plan: nil))
    expect("a refusal that contains the same words the confirmation does is still read as a refusal",
           AssistantQuota.parseCodexLoginStatus("Not logged in"),
           AssistantQuota.Identity(loggedIn: false, plan: nil))
    check("an empty string answers nothing", AssistantQuota.parseCodexLoginStatus("") == nil)
}

group("the machine-level providers read the same file shapes /info already reads, "
    + "and the route answers with what they found") {
    let now = Date(timeIntervalSince1970: 1_787_745_138)
    let tmp = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-quota-test-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: tmp, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: tmp) }

    // Claude: the same rate-limits.json shape SessionInfo.claudeLimits(cacheDirectory:) already
    // reads directly — this only checks that the shaping and §A.1/§A.2 on top of it are wired.
    let cacheDir = tmp.appendingPathComponent("claude-cache", isDirectory: true)
    try? FileManager.default.createDirectory(at: cacheDir, withIntermediateDirectories: true)
    let rateLimitsJSON = """
    {"at": 1787745100, "session_id": "s", "rate_limits": {
      "five_hour": {"used_percentage": 4, "resets_at": 1787761200},
      "seven_day": {"used_percentage": 69, "resets_at": 1787860800}}}
    """
    try? rateLimitsJSON.write(to: cacheDir.appendingPathComponent("rate-limits.json"),
                              atomically: true, encoding: .utf8)
    let claudeQuota = AssistantQuota.claude(cacheDirectory: cacheDir, now: now)
    expect("claude reads straight off the status line's cache", claudeQuota.availability, .ok)
    expect("with both windows", claudeQuota.windows.map(\.name), ["5h", "7d"])
    expect("stamped with the cache's own time, not now", claudeQuota.observedAt, 1_787_745_100)
    expect("and a human sentence to match", claudeQuota.detail, "5h 4%, 7d 69%")

    // Codex: §2.3's exact incident shape, replayed from a real rollout file on disk — a named
    // 100% window, then the account falling back to premium credits with none left. This is the
    // end-to-end proof that the §2.4 fix and the §A.1 credits rule actually meet: before the fix,
    // this same file made `/info` answer "unknown" for an account that was, in fact, exhausted.
    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
    let sessionsRoot = tmp.appendingPathComponent("codex-sessions", isDirectory: true)
    let day = sessionsRoot.appendingPathComponent("2026/08/26", isDirectory: true)
    try? FileManager.default.createDirectory(at: day, withIntermediateDirectories: true)
    let fullWindow = line(["timestamp": "2026-08-26T11:13:50.177Z", "type": "event_msg",
                           "payload": ["type": "token_count", "info": [:],
                                       "rate_limits": ["primary": ["used_percent": 100.0,
                                                                   "window_minutes": 10_080,
                                                                   "resets_at": 1_788_272_000],
                                                       "secondary": NSNull()]]])
    let depletedLine = line(["timestamp": "2026-08-26T11:51:28.524Z", "type": "event_msg",
                             "payload": ["type": "token_count", "info": [:],
                                         "rate_limits": ["limit_id": "premium",
                                                         "primary": NSNull(), "secondary": NSNull(),
                                                         "credits": ["has_credits": false,
                                                                    "unlimited": false,
                                                                    "balance": "0"]]]])
    try? (fullWindow + "\n" + depletedLine + "\n").write(
        to: day.appendingPathComponent("rollout-test-incident.jsonl"),
        atomically: true, encoding: .utf8)
    let codexQuota = AssistantQuota.codex(sessionsRoot: sessionsRoot, now: now)
    expect("the account reads as exhausted, not unknown", codexQuota.availability, .exhausted)
    expect("carrying the window that actually named the percentage",
           codexQuota.windows.compactMap(\.usedPercent), [100])
    check("and a detail sentence that names the credits, not just the percentage",
          codexQuota.detail.contains("premium credits exhausted"))

    // The route and /v1/places both answer with what these providers found — real machine state,
    // since neither route takes an injectable path, so only structure is asserted here.
    let phone = RemoteAuth.addDevice(name: "assistant quota reader", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let anon = RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/assistants"))
    expect("an anonymous read stops at the door, same as every other orchestrator GET",
           anon.status, 401)
    let paired = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/assistants", headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired reader may see it", paired.status, 200)
    let json = (try? JSONSerialization.jsonObject(with: paired.body)) as? [String: Any]
    let rows = json?["assistants"] as? [[String: Any]]
    expect("both assistants answer", rows?.map { $0["id"] as? String ?? "" }.sorted(),
           ["claude", "codex"])
    check("availability is always present, the one field a client must read",
          rows?.allSatisfy { $0["availability"] is String } ?? false)

    let placesResponse = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/places", headers: ["Authorization": "Bearer \(phone.token)"]))
    let places = (try? JSONSerialization.jsonObject(with: placesResponse.body)) as? [String: Any]
    let placeAssistants = places?["assistants"] as? [[String: Any]]
    check("/v1/places carries the same availability word, with no window detail alongside it",
          placeAssistants?.allSatisfy { $0["availability"] is String && $0["windows"] == nil } ?? false)
}

group("resetsAt names the tightest live window rather than the earliest of any of them, and "
    + "Codex's credits rule only trusts a depleted record that is actually the newer one") {
    let now = Date(timeIntervalSince1970: 1_787_745_138)
    let tmp = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-resetsat-test-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: tmp, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: tmp) }
    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }

    // §1: the Claude-side shape — nothing has expired, but two live windows disagree on which
    // reset matters. The account is exhausted on the 7-day window; the 5-hour one merely resets
    // sooner. `resetsAt` picking the earliest of the two rather than the spent one's own points
    // `retry_after` and the 409's "resets in …" at the wrong window entirely — the account is
    // still out long after a root that trusted that number would have retried.
    let cacheDir = tmp.appendingPathComponent("claude-cache", isDirectory: true)
    try? FileManager.default.createDirectory(at: cacheDir, withIntermediateDirectories: true)
    let fiveHourReset = Int(now.timeIntervalSince1970) + 1_800       // 30 minutes — soonest, not tight
    let sevenDayReset = Int(now.timeIntervalSince1970) + 345_600     // 4 days — the one actually spent
    let claudeJSON = """
    {"at": \(Int(now.timeIntervalSince1970) - 30), "session_id": "s", "rate_limits": {
      "five_hour": {"used_percentage": 20, "resets_at": \(fiveHourReset)},
      "seven_day": {"used_percentage": 100, "resets_at": \(sevenDayReset)}}}
    """
    try? claudeJSON.write(to: cacheDir.appendingPathComponent("rate-limits.json"),
                          atomically: true, encoding: .utf8)
    let claudeQuota = AssistantQuota.claude(cacheDirectory: cacheDir, now: now)
    expect("the account is exhausted on its tightest window", claudeQuota.availability, .exhausted)
    expect("resets_at names the spent 7-day window, not the soonest-to-reset 5-hour one",
           claudeQuota.resetsAt, sevenDayReset)

    // §2: the Codex incident this design was written for — one record naming both an *already
    // expired* 5-hour window and a still-live, fully spent 7-day one (the real shape: a sampled
    // rollout with `primary.resets_at` hours in the past and `secondary.resets_at` five days out).
    // Before the fix this must be RED: `resetsAt` took the expired window's own past reset, and
    // `decayed(_:)` read that as "the exhausted window has since reset" and silently downgraded
    // the account to `unknown` — the dispatch gate's `case .ok, .unknown: break` then let a task
    // through to an account that was, in fact, still completely out.
    let windowsRoot = tmp.appendingPathComponent("codex-sessions-windows", isDirectory: true)
    let windowsDay = windowsRoot.appendingPathComponent("2026/08/26", isDirectory: true)
    try? FileManager.default.createDirectory(at: windowsDay, withIntermediateDirectories: true)
    let expiredPrimaryResetsAt = Int(now.timeIntervalSince1970) - 45_138   // already passed
    let liveSecondaryResetsAt = 1_788_264_238                              // five days out
    let bothWindows = line(["timestamp": "2026-08-26T05:00:00.000Z", "type": "event_msg",
                            "payload": ["type": "token_count", "info": [:],
                                        "rate_limits": ["primary": ["used_percent": 20.0,
                                                                    "window_minutes": 300,
                                                                    "resets_at": expiredPrimaryResetsAt],
                                                        "secondary": ["used_percent": 100.0,
                                                                     "window_minutes": 10_080,
                                                                     "resets_at": liveSecondaryResetsAt]]]])
    try? (bothWindows + "\n").write(to: windowsDay.appendingPathComponent("rollout-both-windows.jsonl"),
                                    atomically: true, encoding: .utf8)
    let codexQuota = AssistantQuota.codex(sessionsRoot: windowsRoot, now: now)
    expect("the weekly window is spent, so the account is exhausted — an expired sibling window "
         + "must not demote this to unknown", codexQuota.availability, .exhausted)
    expect("resets_at names the still-live 7-day window that is actually spent",
           codexQuota.resetsAt, liveSecondaryResetsAt)
    expect("the expired 5-hour window is dropped from the windows list too, the same way "
         + "SessionInfo.claudeLimits already drops one of its own",
           codexQuota.windows.map(\.name), ["7d"])

    // §3: the design's credits rule is keyed to *the previous named window*, and within-file that
    // ordering is guaranteed — but `AssistantQuota.codex()` takes the newest depleted record and
    // the newest named window from up to 5 *different* files, and their `at`s were never compared.
    // An old depleted record from an already-reset cycle, paired across files with a later, fresh
    // 96% reading that merely happens to still be within the last 5 rollouts, must not be read as
    // "still depleted" for a window that in fact recovered.
    let orderRoot = tmp.appendingPathComponent("codex-sessions-order", isDirectory: true)
    let orderDay = orderRoot.appendingPathComponent("2026/08/26", isDirectory: true)
    try? FileManager.default.createDirectory(at: orderDay, withIntermediateDirectories: true)
    let oldDepletedLine = line(["timestamp": "2026-08-20T00:00:00.000Z", "type": "event_msg",
                                "payload": ["type": "token_count", "info": [:],
                                            "rate_limits": ["limit_id": "premium",
                                                            "primary": NSNull(), "secondary": NSNull(),
                                                            "credits": ["has_credits": false,
                                                                       "unlimited": false,
                                                                       "balance": "0"]]]])
    try? (oldDepletedLine + "\n").write(to: orderDay.appendingPathComponent("rollout-old-depleted.jsonl"),
                                        atomically: true, encoding: .utf8)
    let newNamedLine = line(["timestamp": "2026-08-26T11:13:50.177Z", "type": "event_msg",
                             "payload": ["type": "token_count", "info": [:],
                                         "rate_limits": ["primary": ["used_percent": 96.0,
                                                                     "window_minutes": 300,
                                                                     "resets_at": 1_788_000_000],
                                                         "secondary": NSNull()]]])
    try? (newNamedLine + "\n").write(to: orderDay.appendingPathComponent("rollout-new-named.jsonl"),
                                     atomically: true, encoding: .utf8)
    let orderedQuota = AssistantQuota.codex(sessionsRoot: orderRoot, now: now)
    expect("an old depleted record does not reach across files to condemn a fresh, unrelated "
         + "96% window", orderedQuota.availability, .low)
    check("and the detail names the percentage, not stale credits language",
          !orderedQuota.detail.contains("premium credits exhausted"))

    // §4: the same cross-file ordering problem, but with the fields-missing shape from §3 of the
    // codexCreditsDepleted tests above — a depleted record with neither `limit_id` nor `credits`
    // at all, newer than a 96% named window in a different file. Before the `SessionInfo.swift`
    // fix, the missing fields defaulted to `""`/`false`, which satisfied every condition
    // `codexCreditsDepleted` checks just as fully as a genuine `"premium"`/`false` pair would.
    let bareRoot = tmp.appendingPathComponent("codex-sessions-bare-fields", isDirectory: true)
    let bareDay = bareRoot.appendingPathComponent("2026/08/26", isDirectory: true)
    try? FileManager.default.createDirectory(at: bareDay, withIntermediateDirectories: true)
    let namedThenBare = line(["timestamp": "2026-08-26T11:13:50.177Z", "type": "event_msg",
                              "payload": ["type": "token_count", "info": [:],
                                          "rate_limits": ["primary": ["used_percent": 96.0,
                                                                      "window_minutes": 300,
                                                                      "resets_at": 1_788_000_000],
                                                          "secondary": NSNull()]]])
    try? (namedThenBare + "\n").write(to: bareDay.appendingPathComponent("rollout-named.jsonl"),
                                      atomically: true, encoding: .utf8)
    let bareAfter = line(["timestamp": "2026-08-26T11:51:28.524Z", "type": "event_msg",
                          "payload": ["type": "token_count", "info": [:],
                                      "rate_limits": ["primary": NSNull(), "secondary": NSNull()]]])
    try? (bareAfter + "\n").write(to: bareDay.appendingPathComponent("rollout-bare.jsonl"),
                                  atomically: true, encoding: .utf8)
    let bareQuota = AssistantQuota.codex(sessionsRoot: bareRoot, now: now)
    expect("a newer record with neither field at all does not condemn the last real window either",
           bareQuota.availability, .low)
}

group("the dispatch gate actually reads the quota it computed, rather than only its helpers") {
    // §4 of the review: `grep -n "Orchestrator\.dispatch(" Tests/main.swift` found nothing —
    // `assistantExhaustedReply`/`assistantOverrideWarning`/`assistantLowWarning` were tested in
    // isolation, but never whether `dispatch()` itself reads `made.assistant`, stays quiet on
    // `unknown`, folds `quotaWarnings` into the reply it actually returns, or refunds the rate
    // ticket on the refusal path. `setOverrideForTesting` exists for exactly this and had no
    // caller at all. This calls the real gate rather than re-testing the helpers a second time.
    //
    // Only case (a) below is safe to run all the way through `Orchestrator.dispatch()`: it
    // returns before `spawn(task)` is ever reached, so nothing opens a real terminal or spends
    // real quota. Cases (b) and (c) pass the gate and register a task that *would* spawn — so
    // each is given its own `serialize` name already held forever by a "briefed" holder task
    // written straight into the store (the same technique
    // "a queued serialized task reports its blockers and cancels immediately" uses above): the
    // probe stays `queued` and the pump can never promote it, while the reply `dispatch()` already
    // returned — built before spawn would ever run — carries the real warnings under test.
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    defer {
        Orchestrator.drainSerializePumpForTesting()
        if let storeBefore {
            try? storeBefore.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        AssistantQuota.clearOverridesForTesting()
        Orchestrator.forget()
    }

    func writeTaskFile(id: String, assistant: String, ignoreQuota: Bool, serialize: [String]) {
        var obj: [String: Any] = [
            "clawdline_protocol": 1, "task_id": id, "kind": "custom",
            "assistant": assistant, "project_dir": "/tmp",
            "title": "quota gate probe", "instructions": "quota gate probe",
            "timeout_minutes": 5,
        ]
        if ignoreQuota { obj["ignore_quota"] = true }
        if !serialize.isEmpty { obj["serialize"] = serialize }
        let dir = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let data = try! JSONSerialization.data(withJSONObject: obj)
        try! data.write(to: dir.appendingPathComponent("task.json"), options: .atomic)
    }
    func exhaustedQuota(_ assistant: Assistant) -> AssistantQuota {
        AssistantQuota(assistant: assistant, installed: true, loggedIn: nil, plan: nil,
                       availability: .exhausted, source: .observed,
                       observedAt: Int(Date().timeIntervalSince1970) - 60,
                       resetsAt: Int(Date().timeIntervalSince1970) + 3_600,
                       detail: "7d 100%", windows: [])
    }
    func holderRow(_ id: String, serializeName: String) -> [String: Any] {
        ["id": id, "state": "briefed", "kind": "custom", "title": "holder that never finishes",
         "assistant": "claude", "project_dir": "/tmp", "timeout_minutes": 30,
         "created": Date().timeIntervalSince1970,
         "secret_hash": String(repeating: "0", count: 64),
         "serialize": [serializeName], "artifacts": []]
    }

    let holderB = "b0000000-0000-0000-0000-000000000001"
    let holderC = "c0000000-0000-0000-0000-000000000001"
    let holderRows = [holderRow(holderB, serializeName: "quota-gate-test-b"),
                      holderRow(holderC, serializeName: "quota-gate-test-c")]
    try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": holderRows])
        .write(to: store, options: .atomic)
    Orchestrator.load()

    // (a) exhausted, no override: refuses before the task is ever registered, and gives back the
    // dispatch-rate ticket it provisionally took.
    let idA = UUID().uuidString.lowercased()
    let dirA = Orchestrator.root.appendingPathComponent(idA, isDirectory: true)
    defer { try? FileManager.default.removeItem(at: dirA) }
    writeTaskFile(id: idA, assistant: "claude", ignoreQuota: false, serialize: [])
    AssistantQuota.setOverrideForTesting(exhaustedQuota(.claude), for: .claude)
    let rateBefore = Orchestrator.dispatchRateCountForTesting()
    let replyA = Orchestrator.dispatch(taskID: idA, secret: String(repeating: "a1", count: 32))
    if case .refused(let status, let code, _, _) = replyA {
        expect("exhausted with no override refuses the dispatch", status, 409)
        expect("with the assistant_exhausted code", code, "assistant_exhausted")
    } else {
        check("exhausted with no override refuses the dispatch", false)
    }
    expect("and its provisional dispatch-rate ticket is given back, not spent",
           Orchestrator.dispatchRateCountForTesting(), rateBefore)
    AssistantQuota.clearOverridesForTesting()

    // (b) exhausted + ignore_quota:true: dispatches anyway, warning under assistant_exhausted with
    // a message that names the override honored.
    let idB = UUID().uuidString.lowercased()
    let dirB = Orchestrator.root.appendingPathComponent(idB, isDirectory: true)
    defer { try? FileManager.default.removeItem(at: dirB) }
    writeTaskFile(id: idB, assistant: "codex", ignoreQuota: true, serialize: ["quota-gate-test-b"])
    AssistantQuota.setOverrideForTesting(exhaustedQuota(.codex), for: .codex)
    let replyB = Orchestrator.dispatch(taskID: idB, secret: String(repeating: "b2", count: 32))
    if case .ok(let payload) = replyB {
        let task = payload["task"] as? [String: Any]
        let warnings = payload["warnings"] as? [[String: Any]] ?? []
        expect("ignore_quota dispatches rather than refusing", task?["state"] as? String, "queued")
        check("and warns under assistant_exhausted, naming the override honored",
              warnings.contains { ($0["code"] as? String) == "assistant_exhausted"
                  && (($0["message"] as? String)?.contains("ignore_quota") ?? false) })
    } else {
        check("ignore_quota dispatches rather than refusing", false)
    }
    AssistantQuota.clearOverridesForTesting()

    // (c) unknown: dispatches quietly — no quota warning of either kind.
    let idC = UUID().uuidString.lowercased()
    let dirC = Orchestrator.root.appendingPathComponent(idC, isDirectory: true)
    defer { try? FileManager.default.removeItem(at: dirC) }
    writeTaskFile(id: idC, assistant: "claude", ignoreQuota: false, serialize: ["quota-gate-test-c"])
    let unknownQuota = AssistantQuota(assistant: .claude, installed: true, loggedIn: nil, plan: nil,
                                      availability: .unknown, source: .observed, observedAt: nil,
                                      resetsAt: nil, detail: "no signal yet", windows: [])
    AssistantQuota.setOverrideForTesting(unknownQuota, for: .claude)
    let replyC = Orchestrator.dispatch(taskID: idC, secret: String(repeating: "c3", count: 32))
    if case .ok(let payload) = replyC {
        let task = payload["task"] as? [String: Any]
        let warnings = payload["warnings"] as? [[String: Any]] ?? []
        expect("unknown dispatches rather than refusing", task?["state"] as? String, "queued")
        check("and carries no assistant_exhausted or assistant_low warning",
              !warnings.contains { let code = $0["code"] as? String
                  return code == "assistant_exhausted" || code == "assistant_low" })
    } else {
        check("unknown dispatches rather than refusing", false)
    }
}

group("the models a session can be moved to, and the word that moves each") {
    // What Codex's own picker would list, as its cache on disk says it. The hidden row and the
    // one without a slug are the two ways a row is not a button.
    let cache = """
    {"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","visibility":"list"},
               {"slug":"gpt-5.4-mini","display_name":"","visibility":"list"},
               {"slug":"codex-auto-review","display_name":"Codex Auto Review","visibility":"hide"},
               {"display_name":"no slug","visibility":"list"}]}
    """
    let models = SessionInfo.codexModels(cache: Data(cache.utf8))
    expect("the rows the picker lists, in its order", models.map { $0.id }, ["gpt-5.6-sol", "gpt-5.4-mini"])
    expect("a display name is the name", models[0].name, "GPT-5.6-Sol")
    expect("and an empty one falls back to the slug", models[1].name, "gpt-5.4-mini")
    expect("Codex is told the slug itself", models[0].command, "gpt-5.6-sol")
    expect("a cache that is not JSON is no models, not a crash", SessionInfo.codexModels(cache: Data("{".utf8)), [])
    expect("and a home without one is the same",
           SessionInfo.codexModels(home: URL(fileURLWithPath: "/nonexistent-" + UUID().uuidString)), [])

    // Claude Code is told an alias — `/model sonnet` — and the page finds the current row by the
    // prefix of a full id, which is what survives a dated release like `claude-haiku-4-5-20251001`.
    let claude = SessionInfo.models(for: .claude)
    check("there are models for Claude", !claude.isEmpty)
    check("Claude Code is told an alias, never a dated id", claude.allSatisfy { !$0.command.hasPrefix("claude-") })
    check("and every row has an id the current model matches by prefix", claude.allSatisfy { $0.id.hasPrefix("claude-") })
    expect("no assistant, no models", SessionInfo.models(for: nil), [])

    let payload = SessionInfo.payload(id: "X", assistant: .claude, sessionId: nil, model: "claude-sonnet-5",
                                      cwd: nil, startedAt: nil, usage: nil, limits: SessionInfo.Limits(),
                                      files: nil, deploy: [], models: claude)
    let rows = payload["models"] as? [[String: Any]] ?? []
    expect("the card gets one row per model", rows.count, claude.count)
    expect("with the three words the page needs", rows.first.map { Set($0.keys) } ?? [], ["id", "name", "command"])
    let bare = SessionInfo.payload(id: "X", assistant: nil, sessionId: nil, model: nil, cwd: nil, startedAt: nil,
                                   usage: nil, limits: SessionInfo.Limits(), files: nil, deploy: [])
    expect("and none is an empty list rather than an absent key", (bare["models"] as? [[String: Any]])?.count, 0)
}

group("Claude Code permission modes come from the screen and cycle in wire order") {
    expect("auto mode is read from its exact status phrase",
           SessionInfo.permissionMode(screen:
            "work\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n"), .auto)
    expect("accept edits mode is read from its exact status phrase",
           SessionInfo.permissionMode(screen:
            "work\n  ⏵⏵ accept edits on (shift+tab to cycle) · ← for agents\n"), .acceptEdits)
    expect("plan mode is read from its exact status phrase",
           SessionInfo.permissionMode(screen:
            "work\n  ⏸ plan mode on (shift+tab to cycle) · ← for agents\n"), .plan)
    expect("a readable screen without a mode line is manual",
           SessionInfo.permissionMode(screen: "Claude Code\n❯ "), .manual)
    expect("conversation text cannot impersonate the status line",
           SessionInfo.permissionMode(screen: "The words auto mode on appeared in a reply.\n❯ "), .manual)
    expect("tmux colour does not hide the status line",
           SessionInfo.permissionMode(screen:
            "\u{1b}[2m  ⏸ plan mode on (shift+tab to cycle) · ← for agents\u{1b}[0m"), .plan)
    expect("a failed capture is unknown", SessionInfo.permissionMode(screen: nil), .unknown)
    expect("an empty capture is unknown", SessionInfo.permissionMode(screen: " \n\t"), .unknown)

    expect("one step moves to the next position",
           SessionInfo.permissionSteps(from: .manual, to: .acceptEdits), 1)
    expect("several steps move forward through the cycle",
           SessionInfo.permissionSteps(from: .auto, to: .plan), 3)
    expect("the distance wraps around to the beginning",
           SessionInfo.permissionSteps(from: .plan, to: .manual), 2)
    expect("staying on the same mode sends no keys",
           SessionInfo.permissionSteps(from: .acceptEdits, to: .acceptEdits), 0)
    check("unknown never invents a starting position",
          SessionInfo.permissionSteps(from: .unknown, to: .auto) == nil)

    let claude = SessionInfo.payload(
        id: "X", assistant: .claude, sessionId: nil, model: nil, cwd: nil, startedAt: nil,
        usage: nil, limits: SessionInfo.Limits(), files: nil, deploy: [], permission: .acceptEdits)
    let permission = claude["permission"] as? [String: Any]
    expect("the card gets the current permission mode",
           permission?["current"] as? String, "acceptEdits")
    expect("and the options in cycle order", permission?["options"] as? [String],
           ["auto", "manual", "acceptEdits", "plan"])

    let codex = SessionInfo.payload(
        id: "Y", assistant: .codex, sessionId: nil, model: nil, cwd: nil, startedAt: nil,
        usage: nil, limits: SessionInfo.Limits(), files: nil, deploy: [], permission: .auto)
    check("a Codex card has no permission field", codex["permission"] == nil)
}

group("the model a `/model` names, before the reply that proves it") {
    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
    func user(_ content: String) -> String {
        line(["type": "user", "message": ["role": "user", "content": content]])
    }
    func file(_ rows: [String]) -> Data { Data((rows.joined(separator: "\n") + "\n").utf8) }

    // Exactly the three rows Claude Code writes for one `/model`, caveat and all. None of them is
    // an assistant turn, so before this the file's last word on the model was the one the session
    // had just left — and for a session that has only ever been switched, there was no word at all.
    let caveat = line(["type": "user", "isMeta": true,
                       "message": ["role": "user", "content": "<local-command-caveat>Caveat: …</local-command-caveat>"]])
    let asked = user("<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>opus</command-args>")
    let printed = user("<local-command-stdout>Set model to \u{1b}[1mOpus 5\u{1b}[22m and saved as your default for new sessions</local-command-stdout>")
    let reply = line(["type": "assistant", "message": ["model": "claude-sonnet-5", "usage": ["input_tokens": 1]]])

    expect("a switch is the newest thing anybody said about the model",
           SessionInfo.claudeLimits(transcript: file([reply, caveat, asked, printed])).model, "claude-opus-5")
    expect("and a reply after the switch is newer still",
           SessionInfo.claudeLimits(transcript: file([caveat, asked, printed, reply])).model, "claude-sonnet-5")
    expect("a session that has done nothing but switch still knows what it is on",
           SessionInfo.claudeLimits(transcript: file([caveat, asked, printed])).model, "claude-opus-5")
    // The word typed becomes the id a reply would have carried, so the card says one thing about
    // one session either side of that reply and the page's row matching keeps working.
    expect("the word typed is turned into the id a reply would have carried",
           SessionInfo.claudeLimits(transcript: file([asked])).model, "claude-opus-5")

    // `/model` with nothing after it opens a picker: the row records that a switch happened
    // without recording what to, and the line it printed is the only place the choice appears.
    let picked = user("<command-name>/model</command-name>\n<command-args></command-args>")
    expect("a picker's choice is read off what it printed",
           SessionInfo.claudeLimits(transcript: file([picked, printed])).model, "claude-opus-5")
    check("and a picker that printed nothing says nothing",
          SessionInfo.claudeLimits(transcript: file([picked])).model == nil)
    // Read from the end, the printed line arrives first — and is only believed where a `/model`
    // asked for it. On its own it is somebody's terminal output.
    check("a stray line about models is not a switch",
          SessionInfo.claudeLimits(transcript: file([printed])).model == nil)

    expect("the word a row asked for", SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name><command-args>haiku</command-args>"), "haiku")
    expect("only the first word of it", SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name><command-args>opus and then some</command-args>"), "opus")
    expect("no argument is the empty string, which is not the same as no row",
           SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name>"), "")
    check("another command is not one", SessionInfo.modelSwitch(inRow: "<command-name>/recap</command-name>") == nil)
    check("and neither is ordinary prose", SessionInfo.modelSwitch(inRow: "switch to opus please") == nil)

    expect("the name it printed, without the terminal's bold",
           SessionInfo.modelPrinted(inRow: "<local-command-stdout>Set model to \u{1b}[1mHaiku 4.5\u{1b}[22m and saved as your default for new sessions</local-command-stdout>"),
           "Haiku 4.5")
    expect("and without a trailing clause when there is none",
           SessionInfo.modelPrinted(inRow: "<local-command-stdout>Set model to Sonnet 5</local-command-stdout>"), "Sonnet 5")
    check("a wording this build does not know comes back nil rather than wrong",
          SessionInfo.modelPrinted(inRow: "<local-command-stdout>Model unchanged</local-command-stdout>") == nil)

    expect("an alias becomes the id", SessionInfo.claudeModelID(word: "sonnet", printed: nil), "claude-sonnet-5")
    expect("a printed name becomes the same id", SessionInfo.claudeModelID(word: "", printed: "Sonnet 5"), "claude-sonnet-5")
    expect("a model released after this build is passed through as it was written",
           SessionInfo.claudeModelID(word: "opus-6", printed: nil), "opus-6")
    expect("and so is a name it has never heard printed",
           SessionInfo.claudeModelID(word: "", printed: "Default (recommended)"), "Default (recommended)")
    check("nothing said is nothing answered", SessionInfo.claudeModelID(word: "", printed: nil) == nil)
}
}
