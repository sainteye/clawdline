import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// Finding 3. `brief` answers the trusted-folder dialog on a tab this app opened, which is the
// only menu that tab can be showing. The same code met a person's own session the moment
// attached tasks existed, where the first row is a permission grant, a plan approval or an
// overwrite confirmation.


// An immediate tab refusal is carried by the HTTP response its caller is still holding, so it
// records the terminal row without finalization side effects. A serialize pump has no such live
// response: its earlier request returned `queued`, so a later refusal must take the complete
// finalize path, including the root notice and descendant cancellation.


// `spawn_failed` was 34 of 206 dispatches on the machine this was measured on, and the answer
// used to be that the root writes the whole task out again under a fresh id — thirty-four
// rewrites by the most context-loaded session in the tree. The retry belongs to the broker,
// which already holds everything the original said.


// 60.7% of the dispatches measured on this machine declared no claims at all. Twenty output
// tokens against a collision that costs a whole task — three to eighteen million on that same
// record — so the reply says something. It says it about absence only: `"claims": []` is a
// positive declaration that the task writes nothing.




// A task that has said what it is doing has, by definition, read the line typed at it: the
// progress route authenticates with the task secret, and the secret is in that line and nowhere
// else on disk. Calling such a task `spawn_failed` is a record that contradicts its own contents
// — and on 2026-08-28 it was worse than a wrong record, because `finalize` reads an unbriefed
// `spawn_failed` as "nothing was ever done here" and disposes the checkout. One child lost its
// worktree and its delivery branch while its tab was still working inside the deleted directory.

func runOrchestratorRecoveryTests() {
group("a result's verification is read even when the summary and artifacts are already known") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let id = UUID().uuidString.lowercased()
    let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
    defer {
        try? manager.removeItem(at: directory)
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    let secret = String(repeating: "9f", count: 32)
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_protocol": 1, "task_id": id, "task_secret": secret, "status": "success",
        "summary": "the file's own words", "artifacts": ["artifacts/report.md"],
        "verification": ["runs": 2, "seconds": 940, "last": "pass",
                         "scope": "swift suite + web-schedules"],
    ]).write(to: directory.appendingPathComponent("result.json"), options: .atomic)
    let task = Orchestrator.Task(
        id: id, state: .briefed, kind: "custom", title: "reported over HTTP",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: Orchestrator.hash(ofSecret: secret))
    Orchestrator.holdScheduleTaskForTesting(task)
    // Exactly what the `/complete` route hands over: both fields already filled, so the old
    // condition would never have looked at the file.
    Orchestrator.finalize(id, as: .success, summary: "the route's sentence",
                          artifacts: ["artifacts/route.md"])
    let record = Orchestrator.record(id: id)
    let verification = record?["verification"] as? [String: Any]
    expect("the verification record survives a fully reported completion",
           verification?["runs"] as? Int, 2)
    expect("with the scope the child wrote", verification?["scope"] as? String,
           "swift suite + web-schedules")
    expect("and the route's own summary still wins over the file's",
           record?["summary"] as? String, "the route's sentence")
}

group("Clawdline answers a menu only on a tab it opened itself") {
    func task(attachedTo session: String?, answered: Bool) -> Orchestrator.Task {
        var made = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .spawning, kind: "custom",
            title: "menu fixture", assistant: .claude, projectDir: "/tmp",
            timeoutMinutes: 30, created: Date(), secretHash: String(repeating: "0", count: 64))
        made.attachSessionId = session
        made.answeredMenu = answered
        return made
    }
    expect("a fresh tab's trusted-folder dialog takes the default",
           Orchestrator.menuStep(task: task(attachedTo: nil, answered: false), choosing: true),
           Orchestrator.MenuStep.answerFirstRow)
    expect("a menu on a session this task did not open is left standing",
           Orchestrator.menuStep(task: task(attachedTo: "STANDING", answered: false),
                                 choosing: true),
           Orchestrator.MenuStep.leaveToOwner)
    expect("no menu, nothing to decide",
           Orchestrator.menuStep(task: task(attachedTo: nil, answered: false), choosing: false),
           Orchestrator.MenuStep.none)
    expect("and a settled menu does not claim the record changed again",
           Orchestrator.menuStep(task: task(attachedTo: nil, answered: true), choosing: true),
           Orchestrator.MenuStep.none)
    let brief = Orchestrator.childBrief(for: task(attachedTo: "STANDING", answered: false))
    check("the attached briefing says the host grant was recorded and who owns menu decisions",
          brief.contains("launched with access to the whole")
            && brief.contains("leave it for the session's owner"))
}

group("dispatch answers an immediate tab refusal; the later pump finalizes its refusal") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? manager.removeItem(at: directory) }
        AssistantQuota.clearOverridesForTesting()
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    AssistantQuota.setOverrideForTesting(
        AssistantQuota(assistant: .claude, installed: true, loggedIn: true, plan: nil,
                       availability: .ok, source: .observed,
                       observedAt: Int(Date().timeIntervalSince1970), resetsAt: nil,
                       detail: "plenty", windows: []),
        for: .claude)
    Orchestrator.taskStarterForTesting = { _, _, _, _, _, _ in
        .refused(status: 409, code: "terminal_closed",
                 message: "no terminal is running", app: "iTerm")
    }
    Orchestrator.workspaceOverlapObserverForTesting = { _, _ in }
    var rootNotifications: [String] = []
    Orchestrator.rootNotificationObserverForTesting = { rootNotifications.append($0.id) }

    let parentID = UUID().uuidString.lowercased()
    let directory = Orchestrator.root.appendingPathComponent(parentID, isDirectory: true)
    made.append(directory)
    try? manager.createDirectory(at: directory, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_protocol": 1, "task_id": parentID, "kind": "custom", "assistant": "claude",
        "project_dir": "/tmp", "title": "a tab that will not open",
        "instructions": "open a tab", "timeout_minutes": 30,
    ]).write(to: directory.appendingPathComponent("task.json"), options: .atomic)

    // Something the dispatching task handed on, alive at the instant the tab fails to open.
    let belowID = UUID().uuidString.lowercased()
    var below = Orchestrator.Task(
        id: belowID, state: .queued, kind: "custom", title: "work handed on",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    below.parentTaskId = parentID
    below.serialize = ["never-released"]
    Orchestrator.holdScheduleTaskForTesting(below)

    let reply = Orchestrator.dispatch(taskID: parentID, secret: String(repeating: "a1", count: 32))
    if case .ok(let payload) = reply {
        expect("the caller is told the tab did not open, in its own response",
               (payload["task"] as? [String: Any])?["state"] as? String, "spawn_failed")
    } else {
        check("a tab-opening failure answers the caller with a record", false, "\(reply)")
    }
    expect("and nothing it handed on is cancelled by a task that never ran",
           Orchestrator.record(id: belowID)?["state"] as? String, "queued")
    check("the failed dispatch still owes its reclaim deadline like every other ending",
          Orchestrator.workCleanupAtForTesting(parentID) != nil)

    // The pump is deliberately different: the HTTP request returned `queued` earlier, so this
    // ending has no other delivery path and takes finalize's complete terminal contract.
    let holderID = UUID().uuidString.lowercased()
    var holder = Orchestrator.Task(
        id: holderID, state: .briefed, kind: "custom", title: "holds pump token",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    holder.serialize = ["pump-spawn-failure"]
    Orchestrator.holdScheduleTaskForTesting(holder)

    let pumpedID = UUID().uuidString.lowercased()
    let pumpedDirectory = Orchestrator.root.appendingPathComponent(pumpedID, isDirectory: true)
    made.append(pumpedDirectory)
    try! manager.createDirectory(at: pumpedDirectory, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_protocol": 1, "task_id": pumpedID, "kind": "custom",
        "assistant": "claude", "project_dir": "/tmp", "title": "pumped refusal",
        "instructions": "open after the token", "timeout_minutes": 30,
        "serialize": ["pump-spawn-failure"],
        // A root to be owed the notice. Without one this fixture had nobody to notify, and the
        // assertion below it — `rootNotifications.contains(pumpedID)` — passed anyway, because
        // the testing observer fires on the first line of `notifyRoot`, ahead of every guard
        // including `guard let rootID = task.rootSessionId`. The check was named for reaching the
        // boundary and that is exactly all it proved: nothing ever crossed it.
        "root": ["session_id": "pumped-root-conversation", "assistant": "claude"],
    ]).write(to: pumpedDirectory.appendingPathComponent("task.json"), options: .atomic)
    let pumpedReply = Orchestrator.dispatch(
        taskID: pumpedID, secret: String(repeating: "b2", count: 32))
    if case .ok(let payload) = pumpedReply {
        expect("the pump refusal starts life as a queued dispatch",
               (payload["task"] as? [String: Any])?["state"] as? String, "queued")
    } else {
        check("the serialized refusal is accepted into its queue", false, "\(pumpedReply)")
    }

    let pumpedChildID = UUID().uuidString.lowercased()
    var childBlocker = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .briefed, kind: "custom",
        title: "holds child token", assistant: .claude, projectDir: "/tmp",
        timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    childBlocker.serialize = ["never-released-below-pump"]
    Orchestrator.holdScheduleTaskForTesting(childBlocker)
    var pumpedChild = Orchestrator.Task(
        id: pumpedChildID, state: .queued, kind: "custom", title: "recorded below refusal",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    pumpedChild.parentTaskId = pumpedID
    pumpedChild.serialize = ["never-released-below-pump"]
    Orchestrator.holdScheduleTaskForTesting(pumpedChild)
    Orchestrator.finalize(holderID, as: .success, summary: "release pump token")
    _ = Orchestrator.drainSerializePumpForTesting(timeout: 5)
    expect("a pumped tab-opening failure cancels work recorded below it",
           Orchestrator.record(id: pumpedChildID)?["state"] as? String, "cancelled")
    // The dispatch reply is long gone, so this ending is still owed its notice. What changed is
    // that the notice is a durable record rather than a send, so "reached the root" is now two
    // facts and both are asserted here. Checking only the first would pass a rewrite that
    // enqueues and never delivers — which is the exact failure this feature exists to make
    // visible, and it would be hiding inside a test that still looked like it covered delivery.
    let owed = Orchestrator.record(id: pumpedID)?["completion_delivery"] as? [String: Any]
    check("a pumped tab-opening failure is owed a durable completion notice",
          owed?["notice_id"] as? String != nil && owed?["state"] as? String == "pending",
          "got \(owed?["state"] as? String ?? "no envelope")")
    // Past this envelope's own retry clock, not `Date()`: the pump has already had a pass at it
    // and there is no terminal in a test to deliver into, so the first failure pushed
    // `next_retry_at` into the future. Asking before then is answered by the due-time guard, and
    // the transport is never reached — which looks exactly like a delivery that did not happen.
    let dueAt = (owed?["next_retry_at"] as? Int).map { Double($0) } ?? 0
    var typedInto: [String] = []
    _ = Orchestrator.completionAttempt(
        taskID: pumpedID, now: Date(timeIntervalSince1970: dueAt + 1),
        deliver: { task, _ in typedInto.append(task.id); return .delivered })
    check("and reaches the root because the notice is delivered, not merely enqueued",
          typedInto == [pumpedID], "typed into \(typedInto)")
    let sent = Orchestrator.record(id: pumpedID)?["completion_delivery"] as? [String: Any]
    check("with the transport delivery recorded as a fact of its own",
          sent?["state"] as? String == "delivered"
            && sent?["transport_delivered_at"] != nil
            && !(sent?["transport_delivered_at"] is NSNull),
          "got \(sent?["state"] as? String ?? "no envelope")")

    let reclaimHolderID = UUID().uuidString.lowercased()
    var reclaimHolder = Orchestrator.Task(
        id: reclaimHolderID, state: .briefed, kind: "custom", title: "reclaim holder",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    reclaimHolder.serialize = ["pump-reclaim-deadlines"]
    Orchestrator.holdScheduleTaskForTesting(reclaimHolder)
    let reclaimID = UUID().uuidString.lowercased()
    let reclaimSecret = String(repeating: "c3", count: 32)
    var reclaim = Orchestrator.Task(
        id: reclaimID, state: .queued, kind: "custom", title: "pumped reclaim refusal",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: Orchestrator.hash(ofSecret: reclaimSecret))
    reclaim.serialize = ["pump-reclaim-deadlines"]
    reclaim.queuedSecret = Orchestrator.sealQueuedSecret(reclaimSecret)!
    reclaim.isolation = .worktree
    let fakePath = Orchestrator.worktreePath(project: "/tmp", taskID: reclaimID)!
    reclaim.worktree = Orchestrator.Worktree(
        path: fakePath, branch: "clawdline/task/\(reclaimID)", base: "deadbeef",
        repository: "/tmp", cwd: fakePath)
    Orchestrator.holdScheduleTaskForTesting(reclaim)
    Orchestrator.finalize(reclaimHolderID, as: .success, summary: "release reclaim token")
    _ = Orchestrator.drainSerializePumpForTesting(timeout: 5)
    check("a pump-promoted tab refusal carries both reclaim deadlines",
          Orchestrator.workCleanupAtForTesting(reclaimID) != nil
            && Orchestrator.buildCleanupAtForTesting(reclaimID) != nil)
}

group("a tab that never opened is retried from its own task file, twice and no further") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? manager.removeItem(at: directory) }
        AssistantQuota.clearOverridesForTesting()
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    AssistantQuota.setOverrideForTesting(
        AssistantQuota(assistant: .claude, installed: true, loggedIn: true, plan: nil,
                       availability: .ok, source: .observed,
                       observedAt: Int(Date().timeIntervalSince1970), resetsAt: nil,
                       detail: "plenty", windows: []),
        for: .claude)
    Orchestrator.workspaceOverlapObserverForTesting = { _, _ in }
    Orchestrator.rootNotificationObserverForTesting = { _ in }
    var tabOpens = false
    Orchestrator.taskStarterForTesting = { _, _, _, _, _, _ in
        tabOpens
            ? .started(id: "TAB-\(UUID().uuidString)", backend: .iterm)
            : .refused(status: 409, code: "terminal_closed",
                       message: "no terminal is running", app: "iTerm")
    }

    func write(_ id: String) {
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try? manager.createDirectory(at: directory, withIntermediateDirectories: true)
        try! JSONSerialization.data(withJSONObject: [
            "clawdline_protocol": 1, "task_id": id, "kind": "code", "assistant": "claude",
            "model": "opus", "project_dir": "/tmp", "title": "the work itself",
            "instructions": "the instructions nothing else wrote down", "timeout_minutes": 45,
            "plan": "one node, retried", "claims": ["Sources/Respawned.swift"],
            "root": ["session_id": "respawn-root"],
        ]).write(to: directory.appendingPathComponent("task.json"), options: .atomic)
    }
    func refusal(_ reply: Orchestrator.Reply) -> (Int, String)? {
        guard case .refused(let status, let code, _, _) = reply else { return nil }
        return (status, code)
    }
    func extra(_ reply: Orchestrator.Reply) -> [String: Any]? {
        guard case .refused(_, _, _, let extra) = reply else { return nil }
        return extra
    }
    func payload(_ reply: Orchestrator.Reply) -> [String: Any]? {
        guard case .ok(let body) = reply else { return nil }
        return body
    }
    func record(_ body: [String: Any]?) -> [String: Any]? { body?["task"] as? [String: Any] }
    // Never `appendingPathComponent("")`: that names the task root itself, and this group's
    // `defer` deletes what it is given.
    func sweep(_ candidate: String?) {
        guard let candidate, Orchestrator.isTaskID(candidate) else { return }
        made.append(Orchestrator.root.appendingPathComponent(candidate, isDirectory: true))
    }

    let originalID = UUID().uuidString.lowercased()
    let originalSecret = String(repeating: "a1", count: 32)
    write(originalID)
    expect("the original dispatch fails to open a tab",
           record(payload(Orchestrator.dispatch(taskID: originalID,
                                                secret: originalSecret)))?["state"] as? String,
           "spawn_failed")

    let firstReply = Orchestrator.respawn(taskID: originalID)
    let first = payload(firstReply)
    let firstRecord = record(first)
    let firstID = firstRecord?["id"] as? String ?? ""
    sweep(firstID)
    check("a spawn_failed task is respawned under a new id", !firstID.isEmpty && firstID != originalID)
    check("and the retry carries everything the original task file said",
          firstRecord?["title"] as? String == "the work itself"
            && firstRecord?["kind"] as? String == "code"
            && firstRecord?["model"] as? String == "opus"
            && firstRecord?["claims"] as? [String] == ["Sources/Respawned.swift"]
            && (firstRecord?["root"] as? [String: Any])?["sessionId"] as? String == "respawn-root")
    // The one field that lives nowhere but the file: the broker never held it, so a retry that
    // did not copy the file would open a tab with nothing in it.
    let copied = try? String(contentsOf: Orchestrator.root
        .appendingPathComponent(firstID, isDirectory: true)
        .appendingPathComponent("task.json"), encoding: .utf8)
    check("including the instructions, which the registry never held",
          copied?.contains("the instructions nothing else wrote down") == true
            && copied?.contains("\"task_id\"") == true && copied?.contains(firstID) == true)
    check("the secret is fresh, and handed back so the caller has what a dispatch would give it",
          (first?["secret"] as? String).map(Orchestrator.isTaskSecret) == true
            && first?["secret"] as? String != originalSecret)
    check("and the chain is visible in the registry rather than three unrelated tasks",
          firstRecord?["respawn_of"] as? String == originalID
            && firstRecord?["respawn_generation"] as? Int == 1
            && first?["original_task"] as? String == originalID)
    // Straight through the registry serializer, because the chain has to outlive a restart: a
    // respawn cap that forgets on relaunch is a cap somebody can wait out.
    var carried = Orchestrator.Task(
        id: firstID, state: .spawnFailed, kind: "code", title: "the work itself",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 45, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    carried.respawnOf = originalID
    carried.respawnGeneration = 2
    let carriedBack = Orchestrator.task(from: Orchestrator.stored(carried))
    check("the chain survives being written down and read back",
          carriedBack?.respawnOf == originalID && carriedBack?.respawnGeneration == 2)
    var orphan = Orchestrator.stored(carried)
    orphan["respawn_of"] = nil
    check("and a generation with no task to descend from counts as no chain at all",
          Orchestrator.task(from: orphan)?.respawnGeneration == 0)

    let secondReply = Orchestrator.respawn(taskID: firstID)
    let secondRecord = record(payload(secondReply))
    let secondID = secondRecord?["id"] as? String ?? ""
    sweep(secondID)
    check("a second retry is allowed, and counts along the chain",
          !secondID.isEmpty && secondRecord?["respawn_generation"] as? Int == 2
            && secondRecord?["respawn_of"] as? String == firstID)
    check("and still names the task the whole chain descends from",
          payload(secondReply)?["original_task"] as? String == originalID)

    // Counting per call rather than along the chain is the mistake this forbids: each of these
    // respawns was the first from *its* immediate parent.
    let third = refusal(Orchestrator.respawn(taskID: secondID))
    expect("the third retry in one chain is refused", third?.0, 409)
    expect("with its own code", third?.1, "respawn_exhausted")

    // The chain is one shape the family can take; the caller falls into the other. The id a root
    // holds is the one that failed, so the natural retry loop asks the *same* original again —
    // and a chain depth cannot see that, because a respawn writes nothing back to the task it
    // retried, leaving a spent original at generation zero for ever. The cap is on the family.
    let spent = refusal(Orchestrator.respawn(taskID: originalID))
    expect("an original whose family is full refuses a further retry", spent?.0, 409)
    expect("whatever shape spent it", spent?.1, "respawn_exhausted")
    check("and the number it reports is the family, not one task's depth",
          extra(Orchestrator.respawn(taskID: originalID))?["respawns"] as? Int == 2)

    // Two calls on the same original, which is what that loop actually does: both are retries of
    // one original, so the second is the family's last and the third is refused by the same count.
    let loopedID = UUID().uuidString.lowercased()
    write(loopedID)
    _ = Orchestrator.dispatch(taskID: loopedID, secret: String(repeating: "e5", count: 32))
    let loopedFirst = record(payload(Orchestrator.respawn(taskID: loopedID)))?["id"] as? String
    sweep(loopedFirst)
    let loopedSecond = record(payload(Orchestrator.respawn(taskID: loopedID)))?["id"] as? String
    sweep(loopedSecond)
    check("two retries may descend from one original by asking that original twice",
          Orchestrator.isTaskID(loopedFirst ?? "") && Orchestrator.isTaskID(loopedSecond ?? "")
            && loopedFirst != loopedSecond)
    let looped = refusal(Orchestrator.respawn(taskID: loopedID))
    expect("and a third from that same original is refused", looped?.0, 409)
    expect("by the code the limit answers with", looped?.1, "respawn_exhausted")

    let successID = UUID().uuidString.lowercased()
    tabOpens = true
    write(successID)
    _ = Orchestrator.dispatch(taskID: successID, secret: String(repeating: "b2", count: 32))
    let live = refusal(Orchestrator.respawn(taskID: successID))
    expect("a task whose tab did open is not respawnable", live?.0, 409)
    expect("and says so in its own code", live?.1, "not_respawnable")
    Orchestrator.finalize(successID, as: .success, summary: "it did the work")
    expect("nor is one that finished — that would be re-running work, not retrying a dispatch",
           refusal(Orchestrator.respawn(taskID: successID))?.1, "not_respawnable")
    expect("and a task nobody has heard of is a 404",
           refusal(Orchestrator.respawn(taskID: UUID().uuidString.lowercased()))?.0, 404)

    // A caller may still choose the secret, exactly as it does for an ordinary dispatch.
    let chosenID = UUID().uuidString.lowercased()
    tabOpens = false
    write(chosenID)
    _ = Orchestrator.dispatch(taskID: chosenID, secret: String(repeating: "c3", count: 32))
    let chosen = String(repeating: "d4", count: 32)
    let chosenReply = Orchestrator.respawn(taskID: chosenID, secret: chosen)
    if let body = payload(chosenReply), let id = record(body)?["id"] as? String {
        sweep(id)
        expect("a supplied secret is used rather than replaced", body["secret"] as? String, chosen)
    } else {
        check("a respawn accepts a caller's own secret", false, "\(chosenReply)")
    }
    expect("and a malformed one is refused before anything is written",
           refusal(Orchestrator.respawn(taskID: chosenID, secret: "too-short"))?.1, "bad_task")
}

group("a dispatch that never said what it writes is warned, and one that said nothing is not") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? manager.removeItem(at: directory) }
        AssistantQuota.clearOverridesForTesting()
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    AssistantQuota.setOverrideForTesting(
        AssistantQuota(assistant: .claude, installed: true, loggedIn: true, plan: nil,
                       availability: .ok, source: .observed,
                       observedAt: Int(Date().timeIntervalSince1970), resetsAt: nil,
                       detail: "plenty", windows: []),
        for: .claude)
    Orchestrator.workspaceOverlapObserverForTesting = { _, _ in }
    Orchestrator.taskStarterForTesting = { _, _, _, _, _, _ in
        .started(id: "TAB-\(UUID().uuidString)", backend: .iterm)
    }

    func dispatch(claims: [String]?, secret pair: String) -> [String: Any]? {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try? manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var obj: [String: Any] = [
            "clawdline_protocol": 1, "task_id": id, "kind": "code", "assistant": "claude",
            "project_dir": "/tmp", "title": "writes something, or says it does not",
            "instructions": "do the work", "timeout_minutes": 30,
        ]
        if let claims { obj["claims"] = claims }
        try! JSONSerialization.data(withJSONObject: obj)
            .write(to: directory.appendingPathComponent("task.json"), options: .atomic)
        guard case .ok(let payload) = Orchestrator.dispatch(taskID: id,
                                                           secret: String(repeating: pair, count: 32))
        else { return nil }
        return payload
    }
    func warns(_ payload: [String: Any]?) -> Bool {
        (payload?["warnings"] as? [[String: Any]] ?? [])
            .contains { $0["code"] as? String == "claims_missing" }
    }

    let silent = dispatch(claims: nil, secret: "a1")
    check("a task.json with no claims field at all is warned about",
          warns(silent))
    check("and it is a warning: the task was still dispatched",
          (silent?["task"] as? [String: Any])?["state"] as? String == "spawning")
    check("the warning says what to add, since the point is to change what the next one sends",
          ((silent?["warnings"] as? [[String: Any]] ?? [])
            .first { $0["code"] as? String == "claims_missing" }?["message"] as? String)?
            .contains("\"claims\": []") == true)
    // The difference between the two is the whole point: warning about an empty list would teach
    // callers that the field is noise, which is how it got to 60.7% in the first place.
    check("a task that declared it writes nothing is not warned about",
          !warns(dispatch(claims: [], secret: "b2")))
    check("nor is one that declared what it writes",
          !warns(dispatch(claims: ["Sources/Declared.swift"], secret: "c3")))

    // The idempotent retry answers with the same record, and the same task is still the one that
    // did not say what it writes.
    let repeatedID = UUID().uuidString.lowercased()
    let repeatedDirectory = Orchestrator.root.appendingPathComponent(repeatedID, isDirectory: true)
    made.append(repeatedDirectory)
    try? manager.createDirectory(at: repeatedDirectory, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_protocol": 1, "task_id": repeatedID, "kind": "code", "assistant": "claude",
        "project_dir": "/tmp", "title": "asked for twice", "instructions": "do the work",
        "timeout_minutes": 30,
    ]).write(to: repeatedDirectory.appendingPathComponent("task.json"), options: .atomic)
    let secret = String(repeating: "d4", count: 32)
    _ = Orchestrator.dispatch(taskID: repeatedID, secret: secret)
    guard case .ok(let again) = Orchestrator.dispatch(taskID: repeatedID, secret: secret) else {
        check("a repeated dispatch answers with the existing record", false)
        return
    }
    check("and the retry of an undeclared task is warned about too", warns(again))
}

group("an attached briefing is delivered work, not a tab still trying to open") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    func oldSpawning(attached: Bool, age: TimeInterval? = nil) -> Orchestrator.Task {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var task = Orchestrator.Task(
            id: id, state: .spawning, kind: "custom", title: "old briefing",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: String(repeating: "0", count: 64))
        let reached = age ?? (attached ? TimeInterval(task.timeoutMinutes * 60 + 1)
                                       : Orchestrator.readyLimit + 1)
        task.spawnedAt = Date().addingTimeInterval(-reached)
        task.attachSessionId = attached ? "STANDING" : nil
        return task
    }
    // The window the tab deadline used to swallow: past the four-minute limit for a tab that
    // never reached a prompt, but nowhere near this task's own timeout. Nothing else in the suite
    // puts an attached fixture in it, so without a case here the `attachSessionId == nil` guard on
    // that deadline can be deleted and every remaining assertion stays green — the owner is still
    // reading a menu and the task is called spawn_failed anyway.
    let answering = oldSpawning(attached: true, age: Orchestrator.readyLimit + 1)
    Orchestrator.holdScheduleTaskForTesting(answering)
    Orchestrator.beat(fromTimer: true)
    expect("an attached task is not called spawn_failed while its owner answers a menu",
           Orchestrator.record(id: answering.id)?["state"] as? String, "spawning")

    let attached = oldSpawning(attached: true)
    Orchestrator.holdScheduleTaskForTesting(attached)
    Orchestrator.beat(fromTimer: true)
    expect("an attached briefing reaches the task timeout while its owner leaves a menu open",
           Orchestrator.record(id: attached.id)?["state"] as? String, "timeout")

    let opening = oldSpawning(attached: false)
    Orchestrator.holdScheduleTaskForTesting(opening)
    Orchestrator.beat(fromTimer: true)
    expect("the same deadline still rejects a fresh tab that never reached a prompt",
           Orchestrator.record(id: opening.id)?["state"] as? String, "spawn_failed")

    // Both deadlines are record decisions and must not travel with the briefing, which reads a
    // screen and types and therefore belongs in the terminal broker. An iTerm sheet nobody has
    // answered fills every lane; if expiry queued behind that, a task nobody can brief would hold
    // its claims and a slot on this Mac for as long as the dialog stayed up.
    let heldLane = DispatchSemaphore(value: 0)
    var lanes = 0
    while RemoteServer.shared.enqueueTerminalCommand(channel: "expiry-lane-\(lanes)", {
        _ = heldLane.wait(timeout: .now() + 5)
    }) { lanes += 1 }
    // Counted rather than asserted at eight: another group's terminal work may still be in
    // flight, and what this needs is only that the broker is full at the moment of the beat.
    check("no terminal lane is left for a briefing", lanes > 0
            && !RemoteServer.shared.enqueueTerminalCommand(channel: "expiry-probe") {})
    let blocked = oldSpawning(attached: true)
    Orchestrator.holdScheduleTaskForTesting(blocked)
    Orchestrator.beat(fromTimer: true)
    expect("a task still expires while every terminal lane is blocked",
           Orchestrator.record(id: blocked.id)?["state"] as? String, "timeout")
    let blockedTab = oldSpawning(attached: false)
    Orchestrator.holdScheduleTaskForTesting(blockedTab)
    Orchestrator.beat(fromTimer: true)
    expect("and a tab that never reached a prompt is refused on the same blocked beat",
           Orchestrator.record(id: blockedTab.id)?["state"] as? String, "spawn_failed")
    for _ in 0..<lanes { heldLane.signal() }
    check("the broker drains again",
          eventually { RemoteServer.shared.terminalOutstandingForTesting().total == 0 })
}

group("a task that has posted progress is not a tab that never opened") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    /// A tab this app opened, past the four-minute deadline for reaching a prompt.
    func pastTheDeadline(secret: String = "unused") -> Orchestrator.Task {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var task = Orchestrator.Task(
            id: id, state: .spawning, kind: "custom", title: "working, and saying so",
            assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: Orchestrator.hash(ofSecret: secret))
        task.spawnedAt = Date().addingTimeInterval(-(Orchestrator.readyLimit + 1))
        return task
    }
    func directory(of task: Orchestrator.Task) -> URL {
        Orchestrator.root.appendingPathComponent(task.id, isDirectory: true)
    }

    // The control, first: without a receipt the deadline still ends the task. If this ever goes
    // green for the wrong reason the two assertions below prove nothing.
    let silent = pastTheDeadline()
    Orchestrator.holdScheduleTaskForTesting(silent)
    Orchestrator.beat(fromTimer: true)
    expect("a tab that never said anything still fails the deadline",
           Orchestrator.record(id: silent.id)?["state"] as? String, "spawn_failed")

    let noteAt = Date().addingTimeInterval(-120)
    var overHTTP = pastTheDeadline()
    overHTTP.progress = [Orchestrator.ProgressNote(note: "reading the briefing now", at: noteAt)]
    Orchestrator.holdScheduleTaskForTesting(overHTTP)
    Orchestrator.beat(fromTimer: true)
    expect("a task that posted a note is briefed rather than spawn_failed",
           Orchestrator.record(id: overHTTP.id)?["state"] as? String, "briefed")
    // Without this the promotion would be half done: `briefedAt == nil` is the second half of
    // what `finalize` reads as an empty checkout, and it is what starts the task's own timeout.
    expect("and its briefing is dated by the receipt, not by the deadline",
           Orchestrator.record(id: overHTTP.id)?["briefedAt"] as? Int,
           Int(noteAt.timeIntervalSince1970))

    // The file half of the channel exists for a child whose sandbox has no loopback, and no beat
    // collects it while the task is `spawning`. Without a collection at the deadline its note is
    // invisible for exactly the window in which it is needed.
    let sandboxedSecret = String(repeating: "e5", count: 32)
    let sandboxed = pastTheDeadline(secret: sandboxedSecret)
    try! JSONSerialization.data(withJSONObject: [
        "task_secret": sandboxedSecret, "note": "no loopback here, so this is the only channel",
    ]).write(to: directory(of: sandboxed).appendingPathComponent(Orchestrator.progressFileName),
             options: .atomic)
    Orchestrator.holdScheduleTaskForTesting(sandboxed)
    Orchestrator.beat(fromTimer: true)
    expect("a note that could only arrive as a file is collected before the deadline fires",
           Orchestrator.record(id: sandboxed.id)?["state"] as? String, "briefed")

    // The receipt dates the briefing at the earlier of the two moments that bound it: the line
    // was on the tty by `lastInjectAt`, and had certainly been read by the first note.
    var typedThenAnswered = pastTheDeadline()
    typedThenAnswered.lastInjectAt = noteAt.addingTimeInterval(-30)
    typedThenAnswered.progress = [Orchestrator.ProgressNote(note: "started", at: noteAt)]
    expect("the briefing is dated from when the line was typed when that is known",
           Orchestrator.briefingProvenByProgress(typedThenAnswered),
           noteAt.addingTimeInterval(-30) as Date?)
    check("and nothing is proven by a task that has said nothing",
          Orchestrator.briefingProvenByProgress(pastTheDeadline()) == nil)

    // The disposal half. `worktreeDisposal` already refuses to erase commits or dirty bytes; the
    // window it cannot see is a child that is working and has not written a byte yet.
    var spokenTo = pastTheDeadline()
    spokenTo.childTerminalId = "TAB"
    spokenTo.progress = [Orchestrator.ProgressNote(note: "working", at: noteAt)]
    check("a checkout is not reclaimed as empty when its child answered",
          !Orchestrator.reclaimsEmptyWorktree(spokenTo, outcome: .spawnFailed))
    var typedAt = pastTheDeadline()
    typedAt.childTerminalId = "TAB"
    typedAt.injectAttempts = 1
    check("nor when the first line was typed at a composer this app saw was ready",
          !Orchestrator.reclaimsEmptyWorktree(typedAt, outcome: .spawnFailed))
    // Liveness is deliberately not the test: the tab is alive in both readings, because a session
    // that never reached a prompt is also a live assistant sitting at a fresh composer.
    var neverSpokenTo = pastTheDeadline()
    neverSpokenTo.childTerminalId = "TAB"
    check("a tab that was never spoken to is still reclaimed",
          Orchestrator.reclaimsEmptyWorktree(neverSpokenTo, outcome: .spawnFailed))
    check("and so is a task whose tab was never opened at all",
          Orchestrator.reclaimsEmptyWorktree(pastTheDeadline(), outcome: .cancelled))
    var briefed = pastTheDeadline()
    briefed.childTerminalId = "TAB"
    briefed.briefedAt = noteAt
    check("an ending that is not an unbriefed spawn failure never reclaims",
          !Orchestrator.reclaimsEmptyWorktree(briefed, outcome: .timeout))
}

group("owned storage is visible through the read-only orchestrator route") {
    let manager = FileManager.default
    let base = manager.temporaryDirectory
        .appendingPathComponent("clawdline-storage-route-\(UUID().uuidString)", isDirectory: true)
    let ledgerBefore = try? Data(contentsOf: OwnedStorage.ledgerURL)
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let taskID = "44444444-5555-4666-8777-888888888888"
    let sessionID = "dddddddd-eeee-4fff-8aaa-bbbbbbbbbbbb"
    defer {
        if let ledgerBefore { try? ledgerBefore.write(to: OwnedStorage.ledgerURL, options: .atomic) }
        else { try? manager.removeItem(at: OwnedStorage.ledgerURL) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        OwnedStorage.ledgerURLOverrideForTesting = nil
        OwnedStorage.scratchRootOverrideForTesting = nil
        OwnedStorage.sessionsDirectoryOverrideForTesting = nil
        try? manager.removeItem(at: base)
        Orchestrator.forget()
    }
    try! manager.createDirectory(at: base, withIntermediateDirectories: true)
    OwnedStorage.ledgerURLOverrideForTesting = base.appendingPathComponent("owned-storage.jsonl")
    OwnedStorage.scratchRootOverrideForTesting = base.appendingPathComponent("scratch",
                                                                             isDirectory: true)
    OwnedStorage.sessionsDirectoryOverrideForTesting = base.appendingPathComponent("sessions",
                                                                                     isDirectory: true)
    try! manager.createDirectory(at: OwnedStorage.sessionsDirectory,
                                 withIntermediateDirectories: true)
    let path = OwnedStorage.scratchpadPath(projectDir: "/Users/me/code/repo",
                                           sessionID: sessionID)!
    try! manager.createDirectory(at: URL(fileURLWithPath: path), withIntermediateDirectories: true)
    try! Data("owned bytes".utf8).write(to: URL(fileURLWithPath: path)
        .appendingPathComponent("receipt"))
    check("the route fixture is entered only through the ledger",
          OwnedStorage.register(taskID: taskID, assistant: .claude,
                                sessionID: sessionID, projectDir: "/Users/me/code/repo",
                                at: Date().addingTimeInterval(-30 * 3600)))
    Orchestrator.forget()
    var task = Orchestrator.Task(id: taskID, state: .success, kind: "custom",
                                 title: "owned storage route", assistant: .claude,
                                 projectDir: "/Users/me/code/repo", timeoutMinutes: 30,
                                 created: Date().addingTimeInterval(-30 * 3600),
                                 finishedAt: Date().addingTimeInterval(-25 * 3600),
                                 secretHash: String(repeating: "0", count: 64))
    task.childSessionId = sessionID
    task.transcriptProven = true
    task.transcriptPath = "/tmp/\(sessionID).jsonl"
    Orchestrator.holdScheduleTaskForTesting(task)
    Orchestrator.saveForTesting()

    let anonymous = RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/storage"))
    expect("anonymous storage inventory is refused", anonymous.status, 401)
    let phone = RemoteAuth.addDevice(name: "storage inventory reader", caps: [.read])
    defer { RemoteAuth.revoke(id: phone.id) }
    let listed = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/storage",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired reader may inspect owned storage", listed.status, 200)
    let body = (try? JSONSerialization.jsonObject(with: listed.body)) as? [String: Any]
    let owned = body?["owned"] as? [[String: Any]]
    let totals = body?["totals"] as? [String: Any]
    check("the route reports the ledger path, decision and reason",
          owned?.count == 1 && owned?[0]["task"] as? String == taskID
            && owned?[0]["state"] as? String == "releasable"
            && owned?[0]["why"] as? String == "eligible"
            && owned?[0]["bytes"] is Int)
    check("the route totals the same owned and releasable item",
          totals?["owned_items"] as? Int == 1
            && totals?["releasable_items"] as? Int == 1
            && totals?["releasable_bytes"] as? Int != nil)
}

group("child briefings put heavyweight temporary work in owned task storage") {
    let id = "55555555-6666-4777-8888-999999999999"
    let task = Orchestrator.Task(id: id, state: .queued, kind: "custom", title: "heavy work",
                                 assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    let brief = Orchestrator.childBrief(for: task)
    check("the briefing names the task-owned work directory",
          brief.contains("/tmp/.clawdline/\(id)/work/"))
    check("and names the heavyweight examples that belong there",
          brief.contains("repo copies") && brief.contains("build outputs")
            && brief.contains("scratchpad"))
}

group("verification reports are optional, bounded metadata rather than a success gate") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var directories: [URL] = []
    defer {
        for directory in directories { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    func finish(_ verification: Any?, suffix: String) -> [String: Any]? {
        let id = UUID().uuidString.lowercased()
        let secret = String(repeating: suffix, count: 64)
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        directories.append(directory)
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var result: [String: Any] = [
            "clawdline_protocol": 1, "task_id": id, "task_secret": secret,
            "status": "success", "summary": "verified", "artifacts": [],
        ]
        if let verification { result["verification"] = verification }
        let data = try! JSONSerialization.data(withJSONObject: result)
        try! data.write(to: directory.appendingPathComponent("result.json"), options: .atomic)
        let task = Orchestrator.Task(
            id: id, state: .briefed, kind: "code", title: "verification fixture",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: Orchestrator.hash(ofSecret: secret))
        Orchestrator.holdScheduleTaskForTesting(task)
        Orchestrator.finalize(id, as: .success, summary: nil)
        return Orchestrator.record(id: id)
    }

    let reported = finish([
        "runs": 2, "seconds": 940, "last": "pass",
        "scope": "swift suite + web-schedules",
    ], suffix: "a")
    let verification = reported?["verification"] as? [String: Any]
    check("a well-formed report is stored and surfaced",
          verification?["runs"] as? Int == 2
            && verification?["seconds"] as? Int == 940
            && verification?["last"] as? String == "pass"
            && verification?["scope"] as? String == "swift suite + web-schedules")

    let omitted = finish(nil, suffix: "b")
    check("an older result without verification succeeds exactly as before",
          omitted?["state"] as? String == "success" && omitted?["verification"] == nil)
    let malformed = finish([
        "runs": "many", "seconds": -1, "last": "maybe", "scope": 7,
    ], suffix: "c")
    check("a malformed report is ignored without turning success into failure",
          malformed?["state"] as? String == "success" && malformed?["verification"] == nil)

    let briefTask = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .queued, kind: "code",
        title: "verification briefing", assistant: .codex, projectDir: "/tmp",
        timeoutMinutes: 90, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    let brief = Orchestrator.childBrief(for: briefTask)
    check("the briefing requires one relevant compile-and-test proof plus red-before-green",
          brief.contains("one verification that actually proves the change")
            && brief.contains("red-before-green"))
    check("the briefing forbids ritual, unrelated and flake-hunting full runs",
          brief.contains("ritual after every small edit")
            && brief.contains("unrelated to the paths this task claimed")
            && brief.contains("only purpose is to see whether something is flaky"))
    check("the briefing gives the focused-runner exception, one-third budget and reclaimed TMPDIR",
          brief.contains("focused_runner_unavailable") && brief.contains("one full-suite run")
            && !brief.contains("three full-suite runs")
            && brief.contains("30 minutes") && brief.contains("/work/tmp"))
    check("the result example carries the optional verification shape",
          brief.contains(#""verification": {"runs": 2, "seconds": 940, "last": "pass""#))
}
}
