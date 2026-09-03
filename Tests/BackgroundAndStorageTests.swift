import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - The list of background sessions

// Pressing ← opens the list of background sessions, and doing that is what moves the conversation
// you were in to the background — the banner across the middle of this capture is Claude Code
// saying so out loud. So this screen is what a parked tab shows whenever nobody has pressed enter
// to step back into the conversation, which is most of the time: parking is what happens when you
// go to look at something else.
//
// It is worth pinning down because it is nearly the shape of a dialog — a flush-left list, a
// marker in front of every row, a composer underneath — and a wrong reading here would put
// buttons on a phone whose real actions are "cancel this job" and "start a new session", neither
// of which is an answer to anything. It is not a dialog, and the reason is narrow: a row here
// carries no number, and ``SessionState/option(_:)`` counts nothing without one.
//
// The real screen pads some forty blank lines between the last row and the composer, so the
// default window would never reach the rows at all. That padding is dropped here on purpose —
// what is being pinned is the shape of the rows, not the luck of where they sat.


















// The beat is the only thing that reclaims a non-success `work/`, and until this test existed
// nothing walked that path: both grace tests called `reclaimTaskWorkIfDue` themselves, so the
// call site inside `beat` could be deleted outright and stay green. It was, in effect, deleted —
// the scheduling filter admitted a task carrying only a reclaim deadline and the per-item guard
// on the next line dropped it again.


// Review F2, F3 and F7: all three are deadline holes that the earlier contract tests did not
// traverse. Restart wrote a terminal record without deadlines; a subdirectory child built below
// `worktree.cwd`, not `worktree.path`; and a close walk could write its old whole-record snapshot
// back after the same beat had settled both deadlines.


// Nothing reclaimed an isolated checkout's build output before this. Whole-worktree disposal is
// gated on a 24-hour cutoff *and* on `landing?.state != .pending`, so a delivery waiting to be
// landed kept every object file it ever built: 814 MB across five open landings on the machine
// this was written for.




// Everything past `attachmentDecision` was untested: `spawnAttached`, the 502, the single-flight
// check the serialize pump re-runs, and the claims gate the SPEC promises an attached task goes
// through. Deleting `spawnAttached` outright left the suite green. These drive the real
// `dispatch` route with the terminal replaced, which is the only part a test cannot have.


// Finding 9. `readResult` was widened to run on every finalize so that a `verification` object
// could still be picked up when summary and artifacts had already been filled in by the HTTP
// route. Narrowed back to the three fields it can actually fill — this is the case the widening
// was for, and it is what stops the narrowing from going one step too far.

func runBackgroundAndStorageTests() {
group("the list of background sessions is a list of jobs, not a question") {
    let screen = """
     ▐▛███▛█   Claude Code v2.1.245
    ▝▜██████▀  Opus 5 · ~/code/clawdline
      ▝▝ ▝▝    1 awaiting input · 1 working · 2 completed

    Your conversation moved to the background — enter opens it · esc returns to it · ctrl+c twice quits

    Needs input
     ✻ 修正瀏覽器問答               press option 1 on phone, then Return to submit                 2h

    Working
     ✽ sleep 300                    請執行 sleep 300 這個指令                                       2s

    Completed
     ✻ workspace status check       修正沒有停，但卡在一個誤判                                      3m
     ✻ debug dialog text detection  程式碼是對的，寫下來的理由是錯的                                 9m

    ─────────────────────────────────────────────────────────
    ❯ describe a task for a new session
    ─────────────────────────────────────────────────────────
      ⏵⏵ auto mode · enter to return · space to reply · ctrl+x to delete · ? for shortcuts
    """
    check("a job with a spinner in front of it is not an option to choose",
          SessionState.menu(screen) == nil)
    // The gate is what a `waiting` status opens, and a parked tab now carries the status of the
    // conversation that moved into the background — so this is the pairing that matters, and the
    // one that did not exist before a tab could speak for a session it is only mirroring.
    check("nor with the gate a waiting session opens",
          SessionState.menu(screen, hookWaiting: true) == nil)
    check("and the composer under it is still a composer",
          SessionState.isChoosing(screen, hookWaiting: true) == false)
}

group("the Mac's schedule form collects fields and never checks them twice") {
    let fresh = ScheduleFormState()
    let body = fresh.body
    expect("a new form asks for every day", body["days"] as? String, "daily")
    expect("and opens on a round hour rather than on whatever time it is", body["at"] as? String,
           "09:00")
    expect("with the parser's own catch-up default", body["catch_up_hours"] as? Int, 6)
    expect("and the parser's own timeout", body["timeout_minutes"] as? Int, 30)
    check("an empty model is left out, not written as an empty string", body["model"] == nil)
    // `scheduleObject(from:)` refuses the whole request over one field it does not recognise, so
    // a field this form invents is a form that cannot save at all.
    let allowed = Set(["title", "at", "days", "place_id", "assistant", "instructions", "enabled",
                       "close_tab", "catch_up_hours", "notify_on_failure", "timeout_minutes",
                       "model"])
    let unknown = Set(body.keys).subtracting(allowed).sorted()
    check("and nothing is sent that the orchestrator's allowlist would refuse", unknown.isEmpty,
          unknown.joined(separator: ", "))

    var picked = ScheduleFormState()
    picked.toggle(day: "mon")
    check("picking a day while Daily is on replaces it rather than adding to it",
          !picked.daily && picked.weekdays == ["mon"])
    picked.toggle(day: "fri")
    expect("the days are sent in weekday order however they were picked",
           picked.body["days"] as? [String], ["mon", "fri"])
    picked.toggle(day: "fri")
    picked.toggle(day: "mon")
    check("and turning the last one off falls back to daily rather than to nothing",
          picked.daily && (picked.body["days"] as? String) == "daily")

    expect("Calendar's weekday numbers are the codes a schedule file keeps",
           (1...7).compactMap(ScheduleFormState.code(forWeekday:)),
           ["sun", "mon", "tue", "wed", "thu", "fri", "sat"])
    check("and nothing outside that week is a weekday",
          ScheduleFormState.code(forWeekday: 0) == nil
            && ScheduleFormState.code(forWeekday: 8) == nil)

    // The picker is the only thing that writes `at`, so what matters is that the two directions
    // agree — and that a shape `when.at` refuses is one the picker cannot be set from in the
    // first place. A stated calendar rather than the machine's, so this says the same thing on a
    // runner in another time zone.
    var berlin = Calendar(identifier: .gregorian)
    berlin.timeZone = TimeZone(identifier: "Europe/Berlin")!
    let day = Date(timeIntervalSince1970: 1_700_000_000)
    guard let five = ScheduleFormState.date(forTime: "09:05", on: day, calendar: berlin),
          let midnight = ScheduleFormState.date(forTime: "00:00", on: day, calendar: berlin) else {
        check("the picker can be set from a time the parser accepts", false)
        return
    }
    expect("the picker's instant comes back as HH:MM in local time",
           ScheduleFormState.time(from: five, calendar: berlin), "09:05")
    expect("midnight keeps both its pairs of zeros",
           ScheduleFormState.time(from: midnight, calendar: berlin), "00:00")
    check("and the shapes the parser refuses are ones the picker cannot be set from",
          ScheduleFormState.date(forTime: "9:05", on: day, calendar: berlin) == nil
            && ScheduleFormState.date(forTime: "24:00", on: day, calendar: berlin) == nil
            && ScheduleFormState.date(forTime: "09:60", on: day, calendar: berlin) == nil
            && ScheduleFormState.date(forTime: "", on: day, calendar: berlin) == nil)

    expect("a number box holding a number is that number",
           ScheduleFormState.number("  12 ", atLeast: 0, or: 6), 12)
    expect("one holding nothing is somebody who did not want to choose",
           ScheduleFormState.number("", atLeast: 0, or: 6), 6)
    expect("one holding a word is the same answer",
           ScheduleFormState.number("soon", atLeast: 1, or: 30), 30)
    expect("no minutes at all is not a timeout somebody meant",
           ScheduleFormState.number("0", atLeast: 1, or: 30), 30)
    expect("but no catch-up at all is a real answer",
           ScheduleFormState.number("0", atLeast: 0, or: 6), 0)

    let home = "/Users/somebody"
    let mine = StartPoints.Place(id: "aaa", path: "\(home)/code/clawdline", label: "clawdline",
                                 at: Date(timeIntervalSince1970: 2))
    let theirs = StartPoints.Place(id: "bbb", path: "\(home)/work/clawdline", label: "clawdline",
                                   at: Date(timeIntervalSince1970: 1))
    expect("two projects with one name are told apart by where they are",
           ScheduleFormState.placeLabels([mine, theirs], home: home),
           ["clawdline — ~/code/clawdline", "clawdline — ~/work/clawdline"])
    expect("a name nothing shares is left as the name",
           ScheduleFormState.placeLabels([mine], home: home), ["clawdline"])
    expect("a schedule whose project has fallen off the recent list can still be saved",
           ScheduleFormState.placeChoices([mine], including: "\(home)/old/site").map(\.path),
           ["\(home)/code/clawdline", "\(home)/old/site"])
    expect("one still on it is not offered twice",
           ScheduleFormState.placeChoices([mine], including: mine.path).count, 1)
    expect("and a form with no schedule behind it adds nothing",
           ScheduleFormState.placeChoices([mine], including: nil).count, 1)
}

group("the Mac's schedule form and the file it wrote agree about what it says") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-form-\(UUID().uuidString)")
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Orchestrator.forget()
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    Orchestrator.forget()

    let place = StartPoints.Place(id: StartPoints.id(for: "/tmp"), path: "/tmp", label: "tmp",
                                  at: Date(timeIntervalSince1970: 1))
    var made = ScheduleFormState()
    made.title = "publish the blog"
    made.at = "07:05"
    made.toggle(day: "mon")
    made.toggle(day: "thu")
    made.placeID = place.id
    made.assistant = "codex"
    made.instructions = "publish the next ready post"
    made.closeTab = "never"
    made.catchUpHours = 12
    made.notifyOnFailure = false
    made.timeoutMinutes = 45
    made.model = "opus"

    let reply = Orchestrator.createSchedule(from: made.body, places: [place],
                                            isDirectory: { $0 == "/tmp" })
    if case .refused(_, _, let why, _) = reply {
        check("the form's own body is one the orchestrator accepts", false, why)
        return
    }
    guard let written = Orchestrator.schedules().first else {
        check("the form's own body is one the orchestrator accepts", false, "nothing on disk")
        return
    }
    // The whole point of the edit door: what comes back out of the file is what went in, or the
    // form is showing somebody something other than the schedule they asked to change.
    expect("the form opens on exactly what it sent",
           ScheduleFormState(schedule: written, places: [place]), made)

    var edited = ScheduleFormState(schedule: written, places: [place])
    edited.title = "publish, later"
    edited.at = "08:30"
    let saved = Orchestrator.updateSchedule(id: written.id, from: edited.body, places: [place],
                                            isDirectory: { $0 == "/tmp" })
    if case .refused(_, _, let why, _) = saved {
        check("an edit assembled by the form is accepted too", false, why)
    }
    let after = Orchestrator.schedules().first
    expect("the title is the changed one", after?.title, "publish, later")
    expect("the schedule keeps the id it was made under", after?.id, written.id)
    expect("and keeps when it was made, which is what stops it running for this morning",
           after?.createdAt ?? nil, written.createdAt)
    // No control shows it, and a save that dropped it would be this form editing something it
    // never put on screen.
    expect("the model the form never showed is still on the schedule",
           after?.taskTemplate["model"] as? String, "opus")
}

group("a save keeps the task fields no form has a control for") {
    // Written by hand, with three fields the form cannot show. Opening such a file and pressing
    // Save used to return it with `claims` and `permission_mode` gone — measured, not inferred,
    // and on both surfaces. The Mac is where files like this actually live, so its Edit button
    // is where somebody meets it first.
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-carry-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory

    let id = "aaaaaaaa-1111-4222-8333-444444444444"
    let handWritten: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": id, "title": "posts",
        "when": ["at": "09:00", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": "/tmp",
                 "instructions": "publish today", "claims": ["posts"],
                 "permission_mode": "edits", "reasoning_effort": "high",
                 "kind": "custom", "model": "opus"],
        "enabled": true,
    ]
    try! JSONSerialization.data(withJSONObject: handWritten)
        .write(to: directory.appendingPathComponent("\(id).json"))

    let place = StartPoints.Place(id: "p1", path: "/tmp", label: "tmp", at: Date())
    let saved = Orchestrator.updateSchedule(
        id: id,
        from: ["title": "posts", "at": "10:00", "days": "daily", "place_id": "p1",
               "assistant": "codex", "instructions": "publish today", "enabled": true],
        places: [place], isDirectory: { _ in true })
    check("the save was accepted", { if case .refused = saved { return false }; return true }())

    let after = (try? JSONSerialization.jsonObject(
        with: Data(contentsOf: directory.appendingPathComponent("\(id).json")))) as? [String: Any]
    let task = after?["task"] as? [String: Any] ?? [:]
    expect("claims survived a save that never mentioned them", task["claims"] as? [String], ["posts"])
    expect("and so did permission_mode", task["permission_mode"] as? String, "edits")
    expect("and so did the hand-written Codex reasoning effort",
           task["reasoning_effort"] as? String, "high")
    expect("and kind", task["kind"] as? String, "custom")
    // The ninth field, and the one the first pass at this missed. The page's form has no model
    // control and its body has no `model` key at all, so a save from a phone used to hand the
    // schedule back running whatever that assistant runs by default. The Mac's form kept it,
    // which is the worse half: the same schedule came back different depending on which screen
    // pressed Save.
    expect("and the model no form has a control for", task["model"] as? String, "opus")
    expect("while the field the form did change is the new one",
           (after?["when"] as? [String: Any])?["at"] as? String, "10:00")

    let changedAssistant = Orchestrator.updateSchedule(
        id: id,
        from: ["title": "posts", "at": "10:00", "days": "daily", "place_id": "p1",
               "assistant": "claude", "instructions": "publish today", "enabled": true],
        places: [place], isDirectory: { _ in true })
    check("changing a Codex schedule to Claude is not trapped by its hidden effort",
          { if case .refused = changedAssistant { return false }; return true }())
    let changedFile = (try? JSONSerialization.jsonObject(
        with: Data(contentsOf: directory.appendingPathComponent("\(id).json"))))
        as? [String: Any]
    let changedTask = changedFile?["task"] as? [String: Any] ?? [:]
    check("the assistant change removes the now-incompatible hidden override",
          changedTask["assistant"] as? String == "claude"
            && changedTask["reasoning_effort"] == nil)

    // **A body that never mentions the model and one that sends an empty one are different
    // requests.** Carrying it on the same terms as `claims` would make the first work and the
    // second impossible, and then no request could ever take a model off a schedule again.
    func saveModel(_ model: Any?) -> [String: Any] {
        var body: [String: Any] = ["title": "posts", "at": "10:00", "days": "daily",
                                   "place_id": "p1", "assistant": "claude",
                                   "instructions": "publish today", "enabled": true]
        if let model { body["model"] = model }
        _ = Orchestrator.updateSchedule(id: id, from: body, places: [place],
                                        isDirectory: { _ in true })
        let file = (try? JSONSerialization.jsonObject(
            with: Data(contentsOf: directory.appendingPathComponent("\(id).json"))))
            as? [String: Any]
        return file?["task"] as? [String: Any] ?? [:]
    }
    expect("naming a different model replaces it", saveModel("sonnet")["model"] as? String,
           "sonnet")
    expect("a body that never mentions it leaves it exactly where it was",
           saveModel(nil)["model"] as? String, "sonnet")
    check("and an empty one is how a form asks for that assistant's default",
          saveModel("")["model"] == nil)
    check("which the next save that says nothing does not undo",
          saveModel(nil)["model"] == nil)
    // A create has nothing to carry and must not invent any of it.
    let made = Orchestrator.createSchedule(
        from: ["title": "fresh", "at": "07:00", "days": "daily", "place_id": "p1",
               "assistant": "claude", "instructions": "something new", "enabled": true],
        places: [place], isDirectory: { _ in true })
    check("a create still writes a task with no carried fields", { if case .refused = made { return false }; return true }())
    let fresh = Orchestrator.schedules().first { $0.title == "fresh" }
    let freshTask = fresh?.taskTemplate ?? [:]
    check("a new schedule has no claims of its own", freshTask["claims"] == nil)
}

group("owned storage evaluation is three-valued and fail-closed") {
    let now = Date(timeIntervalSince1970: 2_000_000)
    let taskID = "11111111-2222-4333-8444-555555555555"
    let sessionID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let path = "/private/tmp/claude-501/-Users-me-code-repo/\(sessionID)"
    let entry = OwnedStorage.Entry(at: now.addingTimeInterval(-30 * 3600),
                                   taskID: taskID, assistant: "claude",
                                   sessionID: sessionID, path: path,
                                   proof: "briefing_marker", projectDir: "/Users/me/code/repo")
    let settled = OwnedStorage.TaskFacts(isTerminal: true,
                                         createdAt: now.addingTimeInterval(-30 * 3600),
                                         finishedAt: now.addingTimeInterval(-13 * 3600),
                                         childPID: 4242,
                                         childProcStart: now.addingTimeInterval(-30 * 3600))
    func input(task: OwnedStorage.Source<OwnedStorage.TaskFacts> = .known(settled),
               landing: OwnedStorage.Source<OwnedStorage.Landing> = .known(.none),
               retained: OwnedStorage.Source<Bool> = .known(false),
               sessions: OwnedStorage.Liveness = .known([]),
               process: OwnedStorage.ProcessStatus = .dead,
               path: OwnedStorage.PathStatus = .valid) -> OwnedStorage.EvaluationInput {
        OwnedStorage.EvaluationInput(entry: entry, task: task, landing: landing,
                                     retained: retained,
                                     sessions: sessions, process: process, path: path, now: now)
    }

    expect("a fully eligible owned path becomes releasable",
           OwnedStorage.evaluate(input()).state, .releasable)
    expect("an unreadable session source is unknown rather than an empty live set",
           OwnedStorage.evaluate(input(sessions: .unreadable)).state, .unknown)
    expect("unknown is conservatively held by the collection boundary",
           OwnedStorage.evaluate(input(sessions: .unreadable)).mayCollect, false)
    expect("a live child session holds its storage",
           OwnedStorage.evaluate(input(sessions: .known([sessionID]))).state, .held)
    expect("a pending landing holds without a time limit",
           OwnedStorage.evaluate(input(landing: .known(.pending))).state, .held)
    expect("an active retain marker holds otherwise eligible storage",
           OwnedStorage.evaluate(input(retained: .known(true))).state, .held)
    expect("an unreadable retain source fails closed",
           OwnedStorage.evaluate(input(retained: .unreadable)).state, .unknown)

    var young = settled
    young.finishedAt = now.addingTimeInterval(-11 * 3600)
    expect("no landing uses the twelve-hour floor",
           OwnedStorage.evaluate(input(task: .known(young))).state, .held)
    young.finishedAt = now.addingTimeInterval(-2 * 3600)
    expect("a settled landing uses the one-hour floor",
           OwnedStorage.evaluate(input(task: .known(young), landing: .known(.settled))).state,
           .releasable)

    var untracked = settled
    untracked.finishedAt = now.addingTimeInterval(-13 * 3600)
    untracked.childProcStart = nil
    expect("missing process start stretches the floor to twenty-four hours",
           OwnedStorage.evaluate(input(task: .known(untracked))).state, .held)
    untracked.finishedAt = now.addingTimeInterval(-25 * 3600)
    expect("the extended floor eventually expires",
           OwnedStorage.evaluate(input(task: .known(untracked))).state, .releasable)
    expect("a still-running original child process holds storage",
           OwnedStorage.evaluate(input(process: .alive)).state, .held)
    expect("a reused pid does not impersonate the original process",
           OwnedStorage.evaluate(input(process: .reused)).state, .releasable)
    expect("an unreadable process fact fails closed",
           OwnedStorage.evaluate(input(process: .unreadable)).state, .unknown)
    expect("a path that does not match its reconstructed canonical path is unknown",
           OwnedStorage.evaluate(input(path: .invalid)).state, .unknown)
}

group("owned storage ledger records proof durably and never claims a failed append") {
    let manager = FileManager.default
    let base = manager.temporaryDirectory
        .appendingPathComponent("clawdline-owned-ledger-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: base, withIntermediateDirectories: true)
    defer {
        OwnedStorage.ledgerURLOverrideForTesting = nil
        OwnedStorage.scratchRootOverrideForTesting = nil
        try? manager.removeItem(at: base)
    }
    OwnedStorage.scratchRootOverrideForTesting = base.appendingPathComponent("scratch",
                                                                             isDirectory: true)
    let taskID = "22222222-3333-4444-8555-666666666666"
    let sessionID = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"

    let blocked = base.appendingPathComponent("not-a-directory")
    try! Data("x".utf8).write(to: blocked)
    OwnedStorage.ledgerURLOverrideForTesting = blocked.appendingPathComponent("owned.jsonl")
    check("a ledger append failure is reported instead of being called registered",
          !OwnedStorage.register(taskID: taskID, assistant: .claude,
                                 sessionID: sessionID, projectDir: "/Users/me/code/repo",
                                 at: Date(timeIntervalSince1970: 100)))

    let ledger = base.appendingPathComponent("owned-storage.jsonl")
    OwnedStorage.ledgerURLOverrideForTesting = ledger
    check("the same proof registers once storage is writable",
          OwnedStorage.register(taskID: taskID, assistant: .claude,
                                sessionID: sessionID, projectDir: "/Users/me/code/repo",
                                at: Date(timeIntervalSince1970: 101)))
    check("re-registering the same proof is idempotent",
          OwnedStorage.register(taskID: taskID, assistant: .claude,
                                sessionID: sessionID, projectDir: "/Users/me/code/repo",
                                at: Date(timeIntervalSince1970: 102)))
    guard case .known(let rows, let malformed) = OwnedStorage.readLedger() else {
        check("the ledger can be read back", false); return
    }
    check("one durable row carries every ownership field",
          rows.count == 1 && malformed.isEmpty
            && rows[0].taskID == taskID && rows[0].sessionID == sessionID
            && rows[0].assistant == "claude" && rows[0].proof == "briefing_marker"
            && rows[0].projectDir == "/Users/me/code/repo")
    let provenTaskID = "33333333-4444-4555-8666-777777777777"
    let provenSessionID = "eeeeeeee-ffff-4aaa-8bbb-cccccccccccc"
    var proven = Orchestrator.Task(
        id: provenTaskID, state: .briefed, kind: "custom", title: "proof integration",
        assistant: .claude, projectDir: "/Users/me/code/repo", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 100),
        secretHash: String(repeating: "0", count: 64))
    let transcript = base.appendingPathComponent("\(provenSessionID).jsonl")
    check("the transcript ownership transition appends its ledger receipt immediately",
          Orchestrator.applyTranscriptOwnership(.belongs, transcript: transcript, to: &proven)
            && proven.transcriptProven && proven.childSessionId == provenSessionID)
    guard case .known(let afterProof, _) = OwnedStorage.readLedger() else {
        check("the proof-appended ledger can be read", false); return
    }
    check("proof integration added exactly the newly owned task and path",
          afterProof.count == 2 && afterProof.contains { row in
              row.taskID == provenTaskID && row.sessionID == provenSessionID
                && row.path == OwnedStorage.scratchpadPath(
                    projectDir: proven.projectDir, sessionID: provenSessionID)
          })
    let attrs = try? manager.attributesOfItem(atPath: ledger.path)
    expect("the ownership ledger stays private", attrs?[.posixPermissions] as? Int, 0o600)
    let handle = try! FileHandle(forWritingTo: ledger)
    try! handle.seekToEnd()
    try! handle.write(contentsOf: Data("not-json\n".utf8))
    try! handle.close()
    guard case .known(let readableRows, let badLines) = OwnedStorage.readLedger() else {
        check("one malformed line does not hide independent ownership rows", false); return
    }
    check("a malformed ledger line is reported while valid rows remain visible",
          readableRows.count == 2 && badLines == [3])
    let beforeCompaction = try! Data(contentsOf: ledger)
    check("compaction fails closed instead of rewriting around unknown ownership evidence",
          !OwnedStorage.compact(now: Date(timeIntervalSince1970: 10_000)))
    expect("failed-closed compaction leaves every ledger byte untouched",
           try? Data(contentsOf: ledger), beforeCompaction)
}

group("the Claude live-session source distinguishes empty from unreadable") {
    let manager = FileManager.default
    let base = manager.temporaryDirectory
        .appendingPathComponent("clawdline-owned-liveness-\(UUID().uuidString)", isDirectory: true)
    defer { try? manager.removeItem(at: base) }
    try! manager.createDirectory(at: base, withIntermediateDirectories: true)
    expect("a readable empty directory is a known empty live set",
           OwnedStorage.liveSessions(in: base), .known([]))
    let live = "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa"
    let row = try! JSONSerialization.data(withJSONObject: ["sessionId": live])
    try! row.write(to: base.appendingPathComponent("123.json"))
    expect("a readable registry exposes its named sessions",
           OwnedStorage.liveSessions(in: base), .known([live]))
    try! Data("not json".utf8).write(to: base.appendingPathComponent("456.json"))
    expect("one malformed live-session row makes the source unreadable",
           OwnedStorage.liveSessions(in: base), .unreadable)
    expect("a missing registry directory is unreadable, never an empty set",
           OwnedStorage.liveSessions(in: base.appendingPathComponent("missing")), .unreadable)
}

group("pending landing storage survives task-directory cleanup") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let pendingID = UUID().uuidString.lowercased()
    let ordinaryID = UUID().uuidString.lowercased()
    let pendingDir = Orchestrator.root.appendingPathComponent(pendingID, isDirectory: true)
    let ordinaryDir = Orchestrator.root.appendingPathComponent(ordinaryID, isDirectory: true)
    defer {
        try? manager.removeItem(at: pendingDir)
        try? manager.removeItem(at: ordinaryDir)
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    try! manager.createDirectory(at: pendingDir, withIntermediateDirectories: true)
    try! manager.createDirectory(at: ordinaryDir, withIntermediateDirectories: true)
    try! Data("pending receipt".utf8).write(to: pendingDir.appendingPathComponent("receipt"))
    try! Data("ordinary receipt".utf8).write(to: ordinaryDir.appendingPathComponent("receipt"))
    let old = Date().addingTimeInterval(-25 * 3600)
    func task(_ id: String) -> Orchestrator.Task {
        Orchestrator.Task(id: id, state: .success, kind: "custom", title: "cleanup fixture",
                          assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
                          created: old, finishedAt: old,
                          secretHash: String(repeating: "0", count: 64))
    }
    var pending = task(pendingID)
    pending.landing = Orchestrator.Landing(state: .pending, target: "main", delivery: nil,
                                           ownerRootKey: "12345678", since: old,
                                           commit: nil, note: nil)
    Orchestrator.holdScheduleTaskForTesting(pending)
    Orchestrator.holdScheduleTaskForTesting(task(ordinaryID))
    Orchestrator.cleanup()
    check("cleanup preserves the receipt directory behind a pending landing",
          manager.fileExists(atPath: pendingDir.path))
    check("the ordinary day-old task directory still expires",
          !manager.fileExists(atPath: ordinaryDir.path))
}

group("task-owned work is reclaimed on the terminal-state schedule") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let graceBefore = Config.shared.orchestratorWorkGraceMinutes
    var madeDirectories: [URL] = []
    defer {
        Config.shared.orchestratorWorkGraceMinutes = graceBefore
        for directory in madeDirectories { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }

    func fixture(_ state: Orchestrator.State = .briefed) -> (Orchestrator.Task, URL) {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        madeDirectories.append(directory)
        try! manager.createDirectory(at: directory.appendingPathComponent("work"),
                                     withIntermediateDirectories: true)
        try! Data("temporary build".utf8).write(
            to: directory.appendingPathComponent("work/output.log"))
        try! manager.createDirectory(at: directory.appendingPathComponent("artifacts"),
                                     withIntermediateDirectories: true)
        try! Data("keep".utf8).write(to: directory.appendingPathComponent("artifacts/receipt"))
        try! Data("{}".utf8).write(to: directory.appendingPathComponent("task.json"))
        try! Data("{}".utf8).write(to: directory.appendingPathComponent("result.json"))
        return (Orchestrator.Task(
            id: id, state: state, kind: "custom", title: "work cleanup fixture",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: String(repeating: "0", count: 64)), directory)
    }

    Orchestrator.forget()
    Config.shared.orchestratorWorkGraceMinutes = 60
    let (success, successDirectory) = fixture()
    Orchestrator.holdScheduleTaskForTesting(success)
    Orchestrator.finalize(success.id, as: .success, summary: "done")
    check("success deletes only work immediately",
          !manager.fileExists(atPath: successDirectory.appendingPathComponent("work").path)
            && manager.fileExists(atPath: successDirectory.appendingPathComponent("artifacts/receipt").path)
            && manager.fileExists(atPath: successDirectory.appendingPathComponent("task.json").path)
            && manager.fileExists(atPath: successDirectory.appendingPathComponent("result.json").path))

    let (failure, failureDirectory) = fixture()
    Orchestrator.holdScheduleTaskForTesting(failure)
    let failureFinished = Date()
    Orchestrator.finalize(failure.id, as: .failure, summary: "failed")
    check("failed work survives while its grace period is live",
          manager.fileExists(atPath: failureDirectory.appendingPathComponent("work/output.log").path))
    check("and disappears after the grace period",
          Orchestrator.reclaimTaskWorkIfDue(failure.id,
              now: failureFinished.addingTimeInterval(60 * 60 + 2))
            && !manager.fileExists(atPath: failureDirectory.appendingPathComponent("work").path))

    let (timedOut, timeoutDirectory) = fixture()
    Orchestrator.holdScheduleTaskForTesting(timedOut)
    let timeoutFinished = Date()
    Orchestrator.finalize(timedOut.id, as: .timeout, summary: "timed out")
    check("timeout work has the same diagnostic grace",
          manager.fileExists(atPath: timeoutDirectory.appendingPathComponent("work").path))
    check("and the timeout work is reclaimed when that grace expires",
          Orchestrator.reclaimTaskWorkIfDue(timedOut.id,
              now: timeoutFinished.addingTimeInterval(60 * 60 + 2))
            && !manager.fileExists(atPath: timeoutDirectory.appendingPathComponent("work").path))

    Config.shared.orchestratorWorkGraceMinutes = 0
    let (immediateFailure, immediateDirectory) = fixture()
    Orchestrator.holdScheduleTaskForTesting(immediateFailure)
    Orchestrator.finalize(immediateFailure.id, as: .failure, summary: "failed")
    check("zero grace deletes failed work immediately",
          !manager.fileExists(atPath: immediateDirectory.appendingPathComponent("work").path))

    Config.shared.orchestratorWorkGraceMinutes = 60
    let absentID = UUID().uuidString.lowercased()
    let absent = Orchestrator.Task(
        id: absentID, state: .briefed, kind: "custom", title: "no work directory",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    Orchestrator.holdScheduleTaskForTesting(absent)
    Orchestrator.finalize(absentID, as: .success, summary: "done")
    expect("a missing work directory never delays the terminal state",
           Orchestrator.record(id: absentID)?["state"] as? String, "success")

    let configDirectory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-work-grace-config-\(UUID().uuidString)",
                                isDirectory: true)
    defer { try? manager.removeItem(at: configDirectory) }
    let writable = Config(directoryForTesting: configDirectory)
    writable.orchestratorWorkGraceMinutes = 1440
    writable.save()
    expect("work grace round-trips through config.json",
           Config(directoryForTesting: configDirectory).orchestratorWorkGraceMinutes, 1440)
    for invalid in [-2, 1441] {
        let data = try! JSONSerialization.data(
            withJSONObject: ["orchestrator_work_grace_minutes": invalid])
        try! manager.createDirectory(at: configDirectory, withIntermediateDirectories: true)
        try! data.write(to: configDirectory.appendingPathComponent("config.json"), options: .atomic)
        expect("out-of-range work grace \(invalid) falls back to the default",
               Config(directoryForTesting: configDirectory).orchestratorWorkGraceMinutes, 60)
    }
}

group("the beat reclaims what a terminal task still owes") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let workGraceBefore = Config.shared.orchestratorWorkGraceMinutes
    let buildGraceBefore = Config.shared.orchestratorBuildGraceMinutes
    var made: [URL] = []
    defer {
        Config.shared.orchestratorWorkGraceMinutes = workGraceBefore
        Config.shared.orchestratorBuildGraceMinutes = buildGraceBefore
        for directory in made { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    Config.shared.orchestratorWorkGraceMinutes = 60
    Config.shared.orchestratorBuildGraceMinutes = 60

    /// The record a restart reloads an hour after a child died: terminal, no `closeAt` — the
    /// linger is cleared by `closeChild` and by `rearmLingers` long before an hour is up — and
    /// nothing left but the reclaim deadline itself.
    func lapsed(_ state: Orchestrator.State, workDue: Date?, buildDue: Date? = nil,
                checkout: URL? = nil) -> Orchestrator.Task {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try! manager.createDirectory(at: directory.appendingPathComponent("work"),
                                     withIntermediateDirectories: true)
        try! Data("the failing build log".utf8).write(
            to: directory.appendingPathComponent("work/build.log"))
        var task = Orchestrator.Task(
            id: id, state: state, kind: "custom", title: "beat reclaim fixture",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
            created: Date().addingTimeInterval(-7_200),
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = Date().addingTimeInterval(-3_600)
        task.workCleanupAt = workDue
        task.buildCleanupAt = buildDue
        if let checkout {
            made.append(checkout)
            try! manager.createDirectory(at: checkout.appendingPathComponent(".build/o"),
                                         withIntermediateDirectories: true)
            try! Data("object".utf8).write(to: checkout.appendingPathComponent(".build/o/a.o"))
            try! manager.createDirectory(at: checkout.appendingPathComponent("Sources"),
                                         withIntermediateDirectories: true)
            try! Data("the delivery".utf8).write(
                to: checkout.appendingPathComponent("Sources/Kept.swift"))
            task.isolation = .worktree
            task.worktree = Orchestrator.Worktree(
                path: checkout.path, branch: "clawdline/task/\(id)", base: "d6781a8",
                repository: checkout.path, cwd: checkout.path)
        }
        return task
    }
    func checkoutURL() -> URL {
        manager.temporaryDirectory
            .appendingPathComponent("clawdline-beat-checkout-\(UUID().uuidString)",
                                    isDirectory: true)
    }

    // Every non-success ending: none of them has a `closeAt` by the time the grace expires, so
    // each one reaches the beat carrying nothing but `workCleanupAt`.
    for ending in [Orchestrator.State.failure, .timeout, .cancelled, .spawnFailed] {
        let expired = lapsed(ending, workDue: Date().addingTimeInterval(-2))
        Orchestrator.holdScheduleTaskForTesting(expired)
        Orchestrator.beat(fromTimer: true)
        check("the beat reclaims \(ending.rawValue) work once its grace has expired",
              !manager.fileExists(atPath: expired.dir.appendingPathComponent("work").path))
        check("and settles the \(ending.rawValue) deadline it acted on",
              Orchestrator.workCleanupAtForTesting(expired.id) == nil)
    }

    // The other half of the same guard: scheduling a task is not permission to reclaim it.
    let waiting = lapsed(.failure, workDue: Date().addingTimeInterval(3_600))
    Orchestrator.holdScheduleTaskForTesting(waiting)
    Orchestrator.beat(fromTimer: true)
    check("work still inside its grace survives the beat",
          manager.fileExists(atPath: waiting.dir.appendingPathComponent("work/build.log").path))
    check("and keeps its deadline for a later one",
          Orchestrator.workCleanupAtForTesting(waiting.id) != nil)

    // A task that has not ended must not be dragged into the reclaim branch by a deadline a
    // stale registry left on it.
    var live = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .queued, kind: "custom",
        title: "still working", assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(), secretHash: String(repeating: "0", count: 64))
    live.workCleanupAt = Date().addingTimeInterval(-2)
    let liveDirectory = Orchestrator.root.appendingPathComponent(live.id, isDirectory: true)
    made.append(liveDirectory)
    try! manager.createDirectory(at: liveDirectory.appendingPathComponent("work"),
                                 withIntermediateDirectories: true)
    Orchestrator.holdScheduleTaskForTesting(live)
    Orchestrator.beat(fromTimer: true)
    check("a task that has not ended keeps its work whatever the record says",
          manager.fileExists(atPath: liveDirectory.appendingPathComponent("work").path))

    // And the same walk carries the build deadline.
    let checkout = checkoutURL()
    let build = lapsed(.timeout, workDue: nil, buildDue: Date().addingTimeInterval(-2),
                       checkout: checkout)
    Orchestrator.holdScheduleTaskForTesting(build)
    Orchestrator.beat(fromTimer: true)
    check("the beat reclaims an expired checkout build directory",
          !manager.fileExists(atPath: checkout.appendingPathComponent(".build").path))
    check("and leaves the delivery in that checkout alone",
          manager.fileExists(atPath: checkout.appendingPathComponent("Sources/Kept.swift").path))
}

group("every terminal path keeps the reclaim contract at its real filesystem boundary") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let workGraceBefore = Config.shared.orchestratorWorkGraceMinutes
    let buildGraceBefore = Config.shared.orchestratorBuildGraceMinutes
    var made: [URL] = []
    defer {
        Config.shared.orchestratorWorkGraceMinutes = workGraceBefore
        Config.shared.orchestratorBuildGraceMinutes = buildGraceBefore
        for directory in made { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    Config.shared.orchestratorWorkGraceMinutes = 0
    Config.shared.orchestratorBuildGraceMinutes = 0

    let restartID = UUID().uuidString.lowercased()
    let restartDirectory = Orchestrator.root.appendingPathComponent(restartID,
                                                                    isDirectory: true)
    made.append(restartDirectory)
    try! manager.createDirectory(at: restartDirectory.appendingPathComponent("work"),
                                 withIntermediateDirectories: true)
    try! Data("heavy".utf8).write(
        to: restartDirectory.appendingPathComponent("work/restart.bin"))
    let restartCheckout = manager.temporaryDirectory
        .appendingPathComponent("clawdline-restart-reclaim-\(UUID().uuidString)",
                                isDirectory: true)
    made.append(restartCheckout)
    try! manager.createDirectory(at: restartCheckout.appendingPathComponent(".build/debug"),
                                 withIntermediateDirectories: true)
    try! Data("object".utf8).write(
        to: restartCheckout.appendingPathComponent(".build/debug/Restart.o"))
    var restarting = Orchestrator.Task(
        id: restartID, state: .spawning, kind: "custom", title: "restart orphan",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    restarting.spawnedAt = Date()
    restarting.isolation = .worktree
    restarting.worktree = Orchestrator.Worktree(
        path: restartCheckout.path, branch: "clawdline/task/\(restartID)", base: "d6781a8",
        repository: restartCheckout.path, cwd: restartCheckout.path)
    Orchestrator.holdScheduleTaskForTesting(restarting)
    Orchestrator.resumeAfterRestart()
    expect("a restart orphan is terminal", Orchestrator.record(id: restartID)?["state"] as? String,
           "spawn_failed")
    check("and the restart path assigns both reclaim deadlines before the beat",
          Orchestrator.workCleanupAtForTesting(restartID) != nil
            && Orchestrator.buildCleanupAtForTesting(restartID) != nil)
    Orchestrator.beat(fromTimer: true)
    check("so its work and build output are reclaimed on the ordinary terminal path",
          !manager.fileExists(atPath: restartDirectory.appendingPathComponent("work").path)
            && !manager.fileExists(atPath: restartCheckout.appendingPathComponent(".build").path))

    let nestedID = UUID().uuidString.lowercased()
    let nestedDirectory = Orchestrator.root.appendingPathComponent(nestedID, isDirectory: true)
    made.append(nestedDirectory)
    try! manager.createDirectory(at: nestedDirectory, withIntermediateDirectories: true)
    let nestedCheckout = manager.temporaryDirectory
        .appendingPathComponent("clawdline-subdirectory-reclaim-\(UUID().uuidString)",
                                isDirectory: true)
    made.append(nestedCheckout)
    let nestedCwd = nestedCheckout.appendingPathComponent("Packages/App", isDirectory: true)
    try! manager.createDirectory(at: nestedCwd.appendingPathComponent(".build/debug"),
                                 withIntermediateDirectories: true)
    try! Data("object".utf8).write(
        to: nestedCwd.appendingPathComponent(".build/debug/App.o"))
    try! Data("source".utf8).write(to: nestedCwd.appendingPathComponent("Kept.swift"))
    var nested = Orchestrator.Task(
        id: nestedID, state: .briefed, kind: "custom", title: "subdirectory build",
        assistant: .codex, projectDir: nestedCwd.path, timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    nested.isolation = .worktree
    nested.worktree = Orchestrator.Worktree(
        path: nestedCheckout.path, branch: "clawdline/task/\(nestedID)", base: "d6781a8",
        repository: nestedCheckout.path, cwd: nestedCwd.path)
    Orchestrator.holdScheduleTaskForTesting(nested)
    Orchestrator.finalize(nestedID, as: .failure, summary: "the package did not compile")
    check("a subdirectory project's actual build output is reclaimed",
          !manager.fileExists(atPath: nestedCwd.appendingPathComponent(".build").path))
    check("and reclaiming its build output leaves the working source in place",
          manager.fileExists(atPath: nestedCwd.appendingPathComponent("Kept.swift").path))

    let staleID = UUID().uuidString.lowercased()
    let staleDirectory = Orchestrator.root.appendingPathComponent(staleID, isDirectory: true)
    made.append(staleDirectory)
    try! manager.createDirectory(at: staleDirectory.appendingPathComponent("work"),
                                 withIntermediateDirectories: true)
    let staleCheckout = manager.temporaryDirectory
        .appendingPathComponent("clawdline-stale-close-\(UUID().uuidString)", isDirectory: true)
    made.append(staleCheckout)
    try! manager.createDirectory(at: staleCheckout.appendingPathComponent(".build/debug"),
                                 withIntermediateDirectories: true)
    var stale = Orchestrator.Task(
        id: staleID, state: .failure, kind: "custom", title: "stale close snapshot",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    stale.finishedAt = Date()
    stale.childTerminalId = "TAB"
    stale.closeAt = Date().addingTimeInterval(-1)
    stale.workCleanupAt = Date().addingTimeInterval(-1)
    stale.buildCleanupAt = Date().addingTimeInterval(-1)
    stale.isolation = .worktree
    stale.worktree = Orchestrator.Worktree(
        path: staleCheckout.path, branch: "clawdline/task/\(staleID)", base: "d6781a8",
        repository: staleCheckout.path, cwd: staleCheckout.path)
    Orchestrator.holdScheduleTaskForTesting(stale)
    check("the reclaim half of a beat settles both deadlines",
          Orchestrator.reclaimTaskWorkIfDue(staleID)
            && Orchestrator.reclaimTaskBuildIfDue(staleID))
    // Safe close no longer acts on a cached inventory at all: what a beat holds may nominate a
    // task, and absence, identity and activity are all decided on a fresh walk inside the
    // terminal broker. So the walk is seamed from here on — a nomination that reaches the broker
    // must never take a real inventory of this Mac from inside the suite — and the circuit is
    // cleared, because `closeStep` refuses everything while an iTerm modal is up.
    var inventory = Targets.Snapshot()
    inventory.sessions = [TargetSession(backend: .iterm, id: "OTHER-TAB", name: "other",
                                        tty: "/dev/ttys099", windowIndex: 0, tabIndex: 0,
                                        assistant: .codex, cwd: "/tmp")]
    // Degraded for the first half: the nomination reaches the broker and the walk it takes there
    // proves nothing, so this half is exactly "a close that decided nothing wrote nothing".
    inventory.isComplete = false
    inventory.error = "the walk was degraded"
    Targets.safeCloseInventoryForTesting = { inventory }
    defer { Targets.safeCloseInventoryForTesting = nil }
    ITerm.completeInventoryForTesting()
    // The deadline guard the reclaim needs is unchanged: a nomination must not write the stale
    // record back.
    check("and closeChild's stale snapshot cannot resurrect either deadline",
          !Orchestrator.closeChild(stale)
            && Orchestrator.workCleanupAtForTesting(staleID) == nil
            && Orchestrator.buildCleanupAtForTesting(staleID) == nil)
    // Whether it entered the broker depends on whether a terminal scan has completed in this
    // process yet, and both halves of this test name the same task. One close at a time is the
    // whole point of the closing guard, so wait for it rather than racing it.
    check("the nomination settles before the next half takes the same task id",
          eventually { Orchestrator.terminalClosesInFlightForTesting() == 0 })

    var staleTake = stale
    staleTake.closeAt = Date().addingTimeInterval(-1)
    staleTake.workCleanupAt = Date().addingTimeInterval(-1)
    staleTake.buildCleanupAt = Date().addingTimeInterval(-1)
    Orchestrator.holdScheduleTaskForTesting(staleTake)
    check("the second reclaim settles both deadlines before takeChildTab",
          Orchestrator.reclaimTaskWorkIfDue(staleID)
            && Orchestrator.reclaimTaskBuildIfDue(staleID))
    // A shell-only tab: `safeCloseActivity` answers idle without a screen capture, so what this
    // exercises is the record write and nothing else.
    let child = TargetSession(backend: .iterm, id: "TAB", name: "child",
                              tty: "/dev/ttys098", windowIndex: 0, tabIndex: 0,
                              assistant: nil, cwd: "/tmp")
    inventory.sessions = [child]
    inventory.isComplete = true
    inventory.error = nil
    // The close is asynchronous now — it is admitted to the terminal broker and settles on main
    // — so the synchronous answer is false and the record is read again before it is written.
    // Only the record question is asked here. A beat fired by a real terminal scan nominates the
    // same task, and whichever of the two reaches the broker first is the one that closes — so
    // what the close *decides* is asserted in its own group, on tasks nothing else can see, and
    // what is left for this one is that neither writer puts the stale snapshot back.
    Orchestrator.takeChildTab(for: staleTake, childID: child.id,
                              closeAt: staleTake.closeAt ?? Date(),
                              end: { _, _ in nil })
    check("the close settles", eventually {
        Orchestrator.terminalClosesInFlightForTesting() == 0
    })
    check("and takeChildTab's stale snapshot cannot resurrect either deadline",
          Orchestrator.workCleanupAtForTesting(staleID) == nil
            && Orchestrator.buildCleanupAtForTesting(staleID) == nil)
}

group("an isolated checkout's build output is reclaimed on its own deadline") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let graceBefore = Config.shared.orchestratorBuildGraceMinutes
    var made: [URL] = []
    defer {
        Config.shared.orchestratorBuildGraceMinutes = graceBefore
        for directory in made {
            try? manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
            try? manager.removeItem(at: directory)
        }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    /// A finished isolated task with a real checkout on disk: object files to reclaim, and a
    /// tracked source file that proves what was not touched.
    func fixture(withCheckout: Bool = true) -> (Orchestrator.Task, URL?) {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var task = Orchestrator.Task(
            id: id, state: .briefed, kind: "custom", title: "build reclaim fixture",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
            secretHash: String(repeating: "0", count: 64))
        guard withCheckout else { return (task, nil) }
        let checkout = manager.temporaryDirectory
            .appendingPathComponent("clawdline-build-checkout-\(UUID().uuidString)",
                                    isDirectory: true)
        made.append(checkout)
        try! manager.createDirectory(at: checkout.appendingPathComponent(".build/debug"),
                                     withIntermediateDirectories: true)
        try! Data(String(repeating: "o", count: 4_096).utf8).write(
            to: checkout.appendingPathComponent(".build/debug/Clawdline.o"))
        try! manager.createDirectory(at: checkout.appendingPathComponent("Sources"),
                                     withIntermediateDirectories: true)
        try! Data("the delivery".utf8).write(
            to: checkout.appendingPathComponent("Sources/Kept.swift"))
        task.isolation = .worktree
        task.worktree = Orchestrator.Worktree(
            path: checkout.path, branch: "clawdline/task/\(id)", base: "d6781a8",
            repository: checkout.path, cwd: checkout.path)
        return (task, checkout)
    }
    func objectFile(_ checkout: URL) -> String {
        checkout.appendingPathComponent(".build/debug/Clawdline.o").path
    }

    Config.shared.orchestratorBuildGraceMinutes = 60
    let (success, successCheckout) = fixture()
    Orchestrator.holdScheduleTaskForTesting(success)
    Orchestrator.finalize(success.id, as: .success, summary: "delivered")
    check("a success reclaims its checkout's build output immediately",
          !manager.fileExists(atPath: successCheckout!.appendingPathComponent(".build").path))
    check("and never touches the source or the checkout itself",
          manager.fileExists(atPath: successCheckout!.appendingPathComponent("Sources/Kept.swift").path)
            && manager.fileExists(atPath: successCheckout!.path))
    check("a reclaimed build settles its deadline",
          Orchestrator.buildCleanupAtForTesting(success.id) == nil)

    let (failure, failureCheckout) = fixture()
    Orchestrator.holdScheduleTaskForTesting(failure)
    let failedAt = Date()
    Orchestrator.finalize(failure.id, as: .failure, summary: "did not compile")
    check("a failure keeps its build output while the grace is live",
          manager.fileExists(atPath: objectFile(failureCheckout!)))
    check("and it goes when that grace expires",
          Orchestrator.reclaimTaskBuildIfDue(failure.id,
              now: failedAt.addingTimeInterval(60 * 60 + 2))
            && !manager.fileExists(atPath: failureCheckout!.appendingPathComponent(".build").path))

    // The gap this closed. Whole-checkout disposal waits for `landing.state != pending`; this
    // deliberately does not ask, because a landing under review needs the source and the branch
    // and has never needed the object files.
    Config.shared.orchestratorBuildGraceMinutes = 0
    var (pending, pendingCheckout) = fixture()
    pending.landing = Orchestrator.Landing(
        state: .pending, target: "main", delivery: "clawdline/task/\(pending.id)",
        ownerRootKey: "12345678", since: Date(), commit: nil, note: nil)
    Orchestrator.holdScheduleTaskForTesting(pending)
    Orchestrator.finalize(pending.id, as: .failure, summary: "waiting to land")
    check("a pending landing does not keep the object files it never needed",
          !manager.fileExists(atPath: pendingCheckout!.appendingPathComponent(".build").path))
    check("the branch's working files are exactly as the landing left them",
          manager.fileExists(atPath: pendingCheckout!.appendingPathComponent("Sources/Kept.swift").path))

    // A task working in a shared tree has no build output of its own, and must never be handed
    // somebody else's.
    let sharedTree = manager.temporaryDirectory
        .appendingPathComponent("clawdline-shared-tree-\(UUID().uuidString)", isDirectory: true)
    made.append(sharedTree)
    try! manager.createDirectory(at: sharedTree.appendingPathComponent(".build"),
                                 withIntermediateDirectories: true)
    try! Data("somebody else's".utf8).write(
        to: sharedTree.appendingPathComponent(".build/theirs.o"))
    var (shared, _) = fixture(withCheckout: false)
    shared.projectDir = sharedTree.path
    Orchestrator.holdScheduleTaskForTesting(shared)
    Orchestrator.finalize(shared.id, as: .success, summary: "done in the shared tree")
    check("a task without a checkout of its own takes no build deadline",
          Orchestrator.buildCleanupAtForTesting(shared.id) == nil)
    check("and the shared tree's build output is untouched",
          manager.fileExists(atPath: sharedTree.appendingPathComponent(".build/theirs.o").path))

    // The same contract `reclaimTaskWorkIfDue` already keeps, in both directions.
    Config.shared.orchestratorBuildGraceMinutes = 0
    let (absent, absentCheckout) = fixture()
    try! manager.removeItem(at: absentCheckout!.appendingPathComponent(".build"))
    Orchestrator.holdScheduleTaskForTesting(absent)
    Orchestrator.finalize(absent.id, as: .failure, summary: "never built anything")
    expect("a missing build directory never delays the terminal state",
           Orchestrator.record(id: absent.id)?["state"] as? String, "failure")
    check("and is treated as already reclaimed",
          Orchestrator.buildCleanupAtForTesting(absent.id) == nil)

    // Locking the directory the object file is *in* is what makes the removal refuse outright.
    // Locking the checkout, or `.build` itself, only stops the last unlink: `removeItem` walks
    // depth-first, so the contents go and the directory stays — measured, not assumed.
    let (refused, refusedCheckout) = fixture()
    Orchestrator.holdScheduleTaskForTesting(refused)
    let lockedDirectory = refusedCheckout!.appendingPathComponent(".build/debug",
                                                                  isDirectory: true)
    try! manager.setAttributes([.posixPermissions: 0o500],
                               ofItemAtPath: lockedDirectory.path)
    Orchestrator.finalize(refused.id, as: .failure, summary: "read-only build directory")
    let keptDeadline = Orchestrator.buildCleanupAtForTesting(refused.id)
    try! manager.setAttributes([.posixPermissions: 0o700],
                               ofItemAtPath: lockedDirectory.path)
    check("a removal the filesystem refuses keeps its deadline for a later beat",
          keptDeadline != nil && manager.fileExists(atPath: objectFile(refusedCheckout!)))
    check("and the retry after that refusal succeeds",
          Orchestrator.reclaimTaskBuildIfDue(refused.id)
            && Orchestrator.buildCleanupAtForTesting(refused.id) == nil)

    check("the shared deadline rule sends every success now",
          Orchestrator.reclaimDeadline(minutes: 1_440, outcome: .success,
                                       now: Date(timeIntervalSince1970: 100))
            == Date(timeIntervalSince1970: 100))
    check("a negative grace defers to the ordinary sweep",
          Orchestrator.reclaimDeadline(minutes: -1, outcome: .failure) == nil)

    let configDirectory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-build-grace-config-\(UUID().uuidString)",
                                isDirectory: true)
    defer { try? manager.removeItem(at: configDirectory) }
    let writable = Config(directoryForTesting: configDirectory)
    writable.orchestratorBuildGraceMinutes = 1_440
    writable.save()
    expect("build grace round-trips through config.json",
           Config(directoryForTesting: configDirectory).orchestratorBuildGraceMinutes, 1_440)
    for invalid in [-2, 1_441] {
        let data = try! JSONSerialization.data(
            withJSONObject: ["orchestrator_build_grace_minutes": invalid])
        try! manager.createDirectory(at: configDirectory, withIntermediateDirectories: true)
        try! data.write(to: configDirectory.appendingPathComponent("config.json"), options: .atomic)
        expect("out-of-range build grace \(invalid) falls back to the default",
               Config(directoryForTesting: configDirectory).orchestratorBuildGraceMinutes, 60)
    }

    // A reclaim nobody documented is a reclaim somebody reports as data loss — and the one
    // thing a reader has to be told is which wait it does *not* observe.
    let landingSentence = ["docs/api.md": "landing.state == pending",
                           "docs/orchestrator.md": "pending landing does not exempt"]
    for page in ["docs/api.md", "docs/orchestrator.md"] {
        let text = try! String(contentsOfFile: page, encoding: .utf8)
        check("\(page) names the build grace setting, its deadline, and the landing it ignores",
              text.contains("orchestrator_build_grace_minutes")
                && text.contains("build_cleanup_at")
                && text.contains(landingSentence[page]!))
    }
}

group("cleanup documentation describes the API and runtime contract, not registry spelling") {
    var task = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .failure, kind: "custom",
        title: "deadline shape", assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(), secretHash: String(repeating: "0", count: 64))
    task.workCleanupAt = Date(timeIntervalSince1970: 100)
    task.buildCleanupAt = Date(timeIntervalSince1970: 200)
    let publicRecord = Orchestrator.recordForTesting(task)
    let registryRecord = OrchestratorStore.stored(task)
    let api = try! String(contentsOfFile: "docs/api.md", encoding: .utf8)
    let guide = try! String(contentsOfFile: "docs/orchestrator.md", encoding: .utf8)
    // `attachmentDecision` and its comment live in `OrchestratorDraft` since the draft/refusal
    // extraction; the assertion is about the comment, so it follows the function.
    let implementation = try! String(contentsOfFile: "Sources/OrchestratorDraft.swift",
                                     encoding: .utf8)
    check("cleanup deadlines are registry-internal and absent from the public task shape",
          publicRecord["work_cleanup_at"] == nil && publicRecord["build_cleanup_at"] == nil
            && registryRecord["work_cleanup_at"] != nil
            && registryRecord["build_cleanup_at"] != nil
            && api.contains("registry-internal"))
    let now = Date(timeIntervalSince1970: 300)
    check("zero grace reclaims every terminal outcome inside finalize, as the guide says",
          Orchestrator.reclaimDeadline(minutes: 0, outcome: .failure, now: now) == now
            && guide.contains("zero grace")
            && guide.contains("every terminal outcome inside `finalize`"))
    check("both pages name the child cwd as the build-output boundary",
          api.contains("<worktree.cwd>/.build") && guide.contains("<worktree.cwd>/.build"))
    check("the attachment resolver comment names its wider watched-session inventory",
          implementation.contains("full watched Session inventory, which is intentionally wider"))
}

group("an attached follow-up goes through dispatch, and survives its own single-flight check") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        Orchestrator.drainSerializePumpForTesting()
        for directory in made { try? manager.removeItem(at: directory) }
        AssistantQuota.clearOverridesForTesting()
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    func session(_ id: String) -> TargetSession {
        TargetSession(backend: .iterm, id: id, name: "odd jobs", tty: "/dev/ttys0\(id.count)",
                      windowIndex: 0, tabIndex: 1, assistant: .codex, cwd: "/tmp")
    }
    let standing = session("STANDING-ONE")
    let second = session("STANDING-TWO")
    let third = session("STANDING-THREE")
    @discardableResult
    func keepAsStandingChild(_ session: TargetSession, depth: Int,
                             taskRootAccess: Bool) -> String {
        var opener = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
            title: "standing child \(session.id)", assistant: .codex, projectDir: "/tmp",
            timeoutMinutes: 30, created: Date().addingTimeInterval(-3_600),
            secretHash: String(repeating: "0", count: 64))
        opener.depth = depth
        opener.finishedAt = Date().addingTimeInterval(-3_000)
        opener.childTerminalId = session.id
        opener.childTaskRootAccess = taskRootAccess
        Orchestrator.holdScheduleTaskForTesting(opener)
        return opener.id
    }
    let standingOpenerID = keepAsStandingChild(standing, depth: 1, taskRootAccess: true)
    _ = keepAsStandingChild(second, depth: 1, taskRootAccess: true)
    _ = keepAsStandingChild(third, depth: 2, taskRootAccess: false)
    Orchestrator.saveForTesting()
    Orchestrator.attachmentInventoryForTesting = ([standing, second, third], [:])
    AssistantQuota.setOverrideForTesting(
        AssistantQuota(assistant: .codex, installed: true, loggedIn: true, plan: nil,
                       availability: .ok, source: .observed,
                       observedAt: Int(Date().timeIntervalSince1970), resetsAt: nil,
                       detail: "plenty", windows: []),
        for: .codex)
    var typed: [(String, String)] = []
    var deliveryFails = false
    Orchestrator.attachedSenderForTesting = { line, target in
        if deliveryFails { return "the session went away" }
        typed.append((target.id, line))
        return nil
    }
    Orchestrator.workspaceOverlapObserverForTesting = { _, _ in }

    func write(_ id: String, attach: String?, serialize: [String] = [],
               claims: [String] = [], root: String) {
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try? manager.createDirectory(at: directory, withIntermediateDirectories: true)
        var obj: [String: Any] = [
            "clawdline_protocol": 1, "task_id": id, "kind": "custom", "assistant": "codex",
            "project_dir": "/tmp", "title": "standing follow-up",
            "instructions": "the follow-up work", "timeout_minutes": 30,
            "root": ["session_id": root],
        ]
        if let attach { obj["attach_session"] = attach }
        if !serialize.isEmpty { obj["serialize"] = serialize }
        if !claims.isEmpty { obj["claims"] = claims }
        try! JSONSerialization.data(withJSONObject: obj)
            .write(to: directory.appendingPathComponent("task.json"), options: .atomic)
    }
    func secret(_ pair: String) -> String { String(repeating: pair, count: 32) }
    func refusal(_ reply: Orchestrator.Reply) -> (Int, String)? {
        guard case .refused(let status, let code, _, _) = reply else { return nil }
        return (status, code)
    }

    // `attach_session` is parsed out of task.json at all — every existing test built the field
    // by hand on an `Orchestrator.Task`, so the parser could be deleted without a red.
    let parsedID = UUID().uuidString.lowercased()
    switch OrchestratorDraft.draft(from: [
        "clawdline_protocol": 1, "task_id": parsedID, "assistant": "codex",
        "project_dir": "/tmp", "instructions": "read", "attach_session": standing.id,
    ], expecting: parsedID, isDirectory: { _ in true }) {
    case .ok(let made):
        check("task.json's attach_session reaches the draft",
              made.attachSessionId == standing.id)
    case .bad(let why):
        check("task.json's attach_session reaches the draft", false, why)
    }

    let attachedID = UUID().uuidString.lowercased()
    write(attachedID, attach: standing.id, claims: ["Sources/Attached.swift"], root: "root-a")
    let accepted = Orchestrator.dispatch(taskID: attachedID, secret: secret("a1"))
    if case .ok(let payload) = accepted {
        let task = payload["task"] as? [String: Any]
        expect("an attached dispatch reaches spawning without opening a tab",
               task?["state"] as? String, "spawning")
        check("and the record says it is attached, and to which session",
              task?["attached"] as? Bool == true
                && task?["attachSession"] as? String == standing.id)
        check("and nothing was opened: the child block names the standing session's own tab",
              ((task?["child"] as? [String: Any])?["terminalId"] as? String) == standing.id)
    } else {
        check("an attached dispatch is accepted", false, "\(accepted)")
    }
    check("the ordinary first line was typed into the standing session",
          typed.count == 1 && typed[0].0 == standing.id
            && typed[0].1.contains(attachedID) && typed[0].1.contains("CHILD.md"))
    check("and the attached briefing was written where the child will look for it",
          manager.fileExists(atPath: Orchestrator.root
            .appendingPathComponent(attachedID, isDirectory: true)
            .appendingPathComponent("CHILD.md").path))

    // Single-flight, through the route rather than through the pure decision.
    let secondID = UUID().uuidString.lowercased()
    write(secondID, attach: standing.id, root: "root-a")
    let occupied = refusal(Orchestrator.dispatch(taskID: secondID, secret: secret("b2")))
    expect("a second task cannot be typed into an occupied session", occupied?.0, 409)
    expect("and the refusal is typed", occupied?.1, "attach_session_occupied")
    check("nothing was typed for the refused task", typed.count == 1)
    check("and no record was made for it", Orchestrator.record(id: secondID) == nil)

    // What SPEC asks for by name: an accepted attached task reserves its claims, and a
    // conflicting one is refused by the ordinary workspace gate — through `dispatch`, with a
    // 409 at the end of it.
    let conflictID = UUID().uuidString.lowercased()
    write(conflictID, attach: second.id, claims: ["Sources/Attached.swift"], root: "root-b")
    let busy = refusal(Orchestrator.dispatch(taskID: conflictID, secret: secret("c3")))
    expect("an attached task's claims are reserved against another root", busy?.0, 409)
    expect("with the ordinary workspace refusal", busy?.1, "workspace_busy")

    let unknownID = UUID().uuidString.lowercased()
    write(unknownID, attach: "NO-SUCH-SESSION", root: "root-b")
    expect("an unknown session is refused before anything is typed",
           refusal(Orchestrator.dispatch(taskID: unknownID, secret: secret("d4")))?.1,
           "attach_session_not_found")

    let leafID = UUID().uuidString.lowercased()
    write(leafID, attach: third.id, root: "root-b")
    expect("a Clawdline leaf without the launch-time task-root grant is refused before typing",
           refusal(Orchestrator.dispatch(taskID: leafID, secret: secret("d5")))?.1,
           "attach_not_managed")

    // The one typed refusal with no test at all, and its seam was already in the file.
    deliveryFails = true
    let deadID = UUID().uuidString.lowercased()
    write(deadID, attach: second.id, root: "root-b")
    let failed = refusal(Orchestrator.dispatch(taskID: deadID, secret: secret("e5")))
    expect("a briefing that cannot be typed is a 502", failed?.0, 502)
    expect("with the typed delivery code", failed?.1, "attach_delivery_failed")
    expect("and the task record exists, terminal, exactly as a tab that never opened",
           Orchestrator.record(id: deadID)?["state"] as? String, "spawn_failed")
    deliveryFails = false

    // Finding 2, end to end. A task combining attach_session with serialize registers queued,
    // and the pump writes it back as `spawning` before calling spawn — so the attachment is
    // re-resolved with the task itself in the registry. Without the self-exclusion this refuses
    // itself, at a moment when the HTTP response that could have said so has already gone.
    let holderID = UUID().uuidString.lowercased()
    var holder = Orchestrator.Task(
        id: holderID, state: .briefed, kind: "custom", title: "holds the token",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    holder.serialize = ["attach-and-serialize"]
    Orchestrator.holdScheduleTaskForTesting(holder)
    let queuedID = UUID().uuidString.lowercased()
    write(queuedID, attach: second.id, serialize: ["attach-and-serialize"], root: "root-c")
    let queued = Orchestrator.dispatch(taskID: queuedID, secret: secret("f6"))
    if case .ok(let payload) = queued {
        expect("an attached serialized task waits for its token like any other",
               (payload["task"] as? [String: Any])?["state"] as? String, "queued")
    } else {
        check("an attached serialized task is accepted", false, "\(queued)")
    }
    Orchestrator.finalize(holderID, as: .success, summary: "token released")
    _ = Orchestrator.drainSerializePumpForTesting(timeout: 5)
    expect("and is briefed, not refused for being itself",
           Orchestrator.record(id: queuedID)?["state"] as? String, "spawning")
    check("its briefing reached the standing session it named",
          typed.contains { $0.0 == second.id && $0.1.contains(queuedID) })

    // Finding 6. A guest does not rename its host, and takes its role with it when it leaves.
    Orchestrator.saveForTesting()
    check("an attached task never renames the session it is a guest in",
          Orchestrator.title(forTerminal: standing.id) == "standing child \(standing.id)")
    expect("while it runs, the session says which broker task has it",
           Orchestrator.role(forTerminal: standing.id)?.taskID, attachedID)
    Orchestrator.finalize(attachedID, as: .success, summary: "follow-up done")
    expect("and when it ends the standing session recovers its exact opener role",
           Orchestrator.role(forTerminal: standing.id)?.taskID, standingOpenerID)
    expect("and it keeps the standing child's title",
           Orchestrator.title(forTerminal: standing.id), "standing child \(standing.id)")
}
}
