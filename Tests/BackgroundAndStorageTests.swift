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

    // Scratch contract v1 over this binary's own scratch root: every direct child is listed with
    // its decision and reason, and a sweep takes only what is releasable.
    let manager = FileManager.default
    let scratchRoot = Orchestrator.reclaimRoots.scratch
    try? manager.removeItem(atPath: scratchRoot)
    try! manager.createDirectory(atPath: scratchRoot, withIntermediateDirectories: true)
    defer { try? manager.removeItem(atPath: scratchRoot) }
    let scratchNow = Date()
    func scratchEntry(_ name: String, _ contents: [String: Any]?) {
        let path = scratchRoot + "/" + name
        try! manager.createDirectory(atPath: path + "/tree", withIntermediateDirectories: true)
        try! manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: path)
        if let contents {
            try! JSONSerialization.data(withJSONObject: contents)
                .write(to: URL(fileURLWithPath: path + "/" + OwnedStorage.scratchMarkerName))
        }
    }
    func marker(_ purpose: String, owner: Any = NSNull(), keepUntil: Any = NSNull(),
                version: Int = 1) -> [String: Any] {
        ["clawdline_scratch": version, "created_at": Int(scratchNow.timeIntervalSince1970) - 7_200,
         "purpose": purpose, "owner": owner, "keep_until": keepUntil]
    }
    scratchEntry("snapshot-worktree.a1B2c3D4", marker("snapshot-worktree"))
    scratchEntry("session.e5F6g7H8", marker("session", owner: [
        "pid": 4242, "process_start": 1_700_000_000, "command": "claude"] as [String: Any]))
    scratchEntry("ttl.i9J0k1L2", marker("ttl", keepUntil: Int(scratchNow.timeIntervalSince1970) + 600))
    scratchEntry("orphan.m3N4o5P6", marker("orphan"))
    scratchEntry("nomarker.q7R8s9T0", nil)
    scratchEntry("future.u1V2w3X4", marker("future", version: 2))
    scratchEntry("other.y5Z6a7B8", marker("different"))
    scratchEntry("broken.c9D0e1F2", ["clawdline_scratch": 1, "created_at": 1, "purpose": "broken"])
    try! Data("stray".utf8).write(to: URL(fileURLWithPath: scratchRoot + "/Not A Contract Name"))
    var ownerAnswer = OwnedStorage.ProcessStatus.alive
    let working = Set([scratchRoot + "/orphan.m3N4o5P6/tree"])
    let decisions = Dictionary(uniqueKeysWithValues: OwnedStorage.scratchListing(
        root: scratchRoot, now: scratchNow, graceMinutes: 60, ownerStatus: { _ in ownerAnswer },
        workingDirectories: { working }).rows.map {
            ($0.name, "\($0.decision.state.rawValue):\($0.decision.why)")
        })
    expect("every direct child of the owned root is listed with its decision and reason", decisions, [
        "snapshot-worktree.a1B2c3D4": "releasable:eligible", "session.e5F6g7H8": "held:owner_alive",
        "ttl.i9J0k1L2": "held:keep_until", "orphan.m3N4o5P6": "held:cwd_in_use",
        "nomarker.q7R8s9T0": "unknown:marker_missing", "future.u1V2w3X4": "unknown:marker_version",
        "other.y5Z6a7B8": "unknown:marker_purpose_mismatch",
        "broken.c9D0e1F2": "unknown:marker_invalid", "Not A Contract Name": "unknown:name_not_contract",
    ])
    expect("the sweep removes only the releasable entry",
           OwnedStorage.sweepScratch(root: scratchRoot, now: scratchNow, graceMinutes: 60,
                                     ownerStatus: { _ in ownerAnswer }, workingDirectories: { working }),
           [scratchRoot + "/snapshot-worktree.a1B2c3D4"])
    check("and holds a live owner's entry, one a process works in, and every unknown one",
          ["session.e5F6g7H8", "ttl.i9J0k1L2", "orphan.m3N4o5P6", "nomarker.q7R8s9T0",
           "future.u1V2w3X4", "other.y5Z6a7B8", "broken.c9D0e1F2", "Not A Contract Name"]
            .allSatisfy { manager.fileExists(atPath: scratchRoot + "/" + $0) })
    ownerAnswer = .reused
    expect("a pid that now belongs to another process releases its owner's entry",
           OwnedStorage.sweepScratch(root: scratchRoot, now: scratchNow, graceMinutes: 60,
                                     ownerStatus: { _ in ownerAnswer }, workingDirectories: { working }),
           [scratchRoot + "/session.e5F6g7H8"])
    expect("an unreadable process table makes a releasable entry unknown, never collected",
           OwnedStorage.sweepScratch(root: scratchRoot, now: scratchNow, graceMinutes: 60,
                                     ownerStatus: { _ in .dead }, workingDirectories: { nil }), [])

    let linkedRoot = manager.temporaryDirectory
        .appendingPathComponent("clawdline-scratch-link-\(UUID().uuidString)").path
    try! manager.createSymbolicLink(atPath: linkedRoot, withDestinationPath: scratchRoot)
    defer { try? manager.removeItem(atPath: linkedRoot) }
    expect("a symlinked root is refused, never followed, even spelled with a trailing slash",
           [OwnedStorage.scratchRootState(linkedRoot), OwnedStorage.scratchRootState(linkedRoot + "/")],
           [.refused("root_symlink"), .refused("root_symlink")])
    expect("a relative root is refused rather than resolved",
           OwnedStorage.scratchRootState("clawdline-scratch"), .refused("root_not_absolute"))
    expect("an absent root is nothing to sweep",
           OwnedStorage.scratchRootState(scratchRoot + "-absent"), .absent)
    check("and a refused root sweeps nothing, whatever its target holds",
          OwnedStorage.sweepScratch(root: linkedRoot, now: scratchNow, graceMinutes: 0,
                                    ownerStatus: { _ in .dead },
                                    workingDirectories: { Set<String>() }).isEmpty
            && manager.fileExists(atPath: scratchRoot + "/ttl.i9J0k1L2"))

    let me = getpid()
    if let started = Targets.processStart(ofPID: me) {
        func status(_ offset: TimeInterval) -> OwnedStorage.ProcessStatus {
            OwnedStorage.scratchOwnerStatus(.init(pid: me, processStart: started.addingTimeInterval(offset),
                                                  command: "clawdline-tests"))
        }
        expect("a running pid with its recorded start is alive, to the contract's one second",
               [status(0), status(1), status(-1)], [.alive, .alive, .alive])
        expect("two seconds off is another process that took the pid", status(2), .reused)
    } else {
        check("this process's own start time is readable", false)
    }
    let ownerless = OwnedStorage.ScratchEntryFacts(
        name: "landing.a1b2c3d4", kind: .directory(uid: getuid(), mode: 0o700),
        marker: .marker(.init(createdAt: scratchNow.addingTimeInterval(-600), purpose: "landing",
                              owner: nil, keepUntil: nil)), owner: .absent)
    expect("the broker's grace counts from created_at",
           OwnedStorage.evaluateScratch(ownerless, now: scratchNow, graceMinutes: 60).why, "grace")
    expect("-1 lists an entry and collects nothing",
           OwnedStorage.evaluateScratch(ownerless, now: scratchNow, graceMinutes: -1).why, "grace_disabled")
    var foreign = ownerless
    foreign.kind = .directory(uid: getuid() + 1, mode: 0o700)
    expect("an entry another uid owns is unknown",
           OwnedStorage.evaluateScratch(foreign, now: scratchNow, graceMinutes: 0).why, "entry_not_owned")

    // Every direct child is judged, however many sort ahead of it: a thousand names that are not
    // the contract's come first, and one releasable entry after them.
    let crowdedRoot = scratchRoot + "-crowded"
    try? manager.removeItem(atPath: crowdedRoot)
    try! manager.createDirectory(atPath: crowdedRoot, withIntermediateDirectories: true)
    defer { try? manager.removeItem(atPath: crowdedRoot) }
    for index in 0..<OwnedStorage.scratchListedEntryLimit {
        try! Data().write(to: URL(fileURLWithPath: crowdedRoot + "/" + String(format: "%04d", index)))
    }
    let last = crowdedRoot + "/zz-last.a1B2c3D4"
    try! manager.createDirectory(atPath: last + "/tree", withIntermediateDirectories: true)
    try! manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: last)
    try! JSONSerialization.data(withJSONObject: marker("zz-last"))
        .write(to: URL(fileURLWithPath: last + "/" + OwnedStorage.scratchMarkerName))
    let crowded = OwnedStorage.scratchInventory(root: crowdedRoot, now: scratchNow, graceMinutes: 60,
                                                workingDirectories: { Set<String>() })
    let crowdedTotals = crowded["totals"] as? [String: Int]
    check("a body past its limit lists the first thousand by name and says it was cut",
          (crowded["entries"] as? [[String: Any]])?.count == OwnedStorage.scratchListedEntryLimit
            && crowded["truncated"] as? Bool == true)
    check("while its totals judge every entry, the releasable one past the limit included",
          crowdedTotals?["items"] == OwnedStorage.scratchListedEntryLimit + 1
            && crowdedTotals?["releasable_items"] == 1
            && crowdedTotals?["unknown_items"] == OwnedStorage.scratchListedEntryLimit)
    expect("and the sweep reaches the releasable entry a thousand unknown ones sort ahead of",
           OwnedStorage.sweepScratch(root: crowdedRoot, now: scratchNow, graceMinutes: 60,
                                     ownerStatus: { _ in .dead },
                                     workingDirectories: { Set<String>() }), [last])

    // The removal seam: what the listing read is read again at the removal itself. First the
    // working directories, which a process can enter between the two readings.
    let seamRoot = scratchRoot + "-seam"
    try? manager.removeItem(atPath: seamRoot)
    defer { try? manager.removeItem(atPath: seamRoot) }
    let entered = seamRoot + "/entered.e1F2g3H4"
    try! manager.createDirectory(atPath: entered + "/tree", withIntermediateDirectories: true)
    try! manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: entered)
    try! JSONSerialization.data(withJSONObject: marker("entered"))
        .write(to: URL(fileURLWithPath: entered + "/" + OwnedStorage.scratchMarkerName))
    var readings = 0
    var secondReading: () -> Set<String>? = { Set<String>() }
    func sweepSeam() -> [String] {
        readings = 0
        return OwnedStorage.sweepScratch(root: seamRoot, now: scratchNow, graceMinutes: 60,
                                         ownerStatus: { _ in .dead }, workingDirectories: {
            readings += 1
            return readings == 1 ? Set<String>() : secondReading()
        })
    }
    secondReading = { [entered + "/tree"] }
    expect("a process that entered the entry after the listing looked keeps it at the removal",
           sweepSeam(), [])
    check("so the working directories were read twice and the entry is intact",
          readings == 2 && manager.fileExists(atPath: entered + "/tree"))
    secondReading = { nil }
    expect("and a process table that cannot be read at the removal keeps it too", sweepSeam(), [])
    secondReading = { Set<String>() }
    expect("with nobody working in it the entry goes", sweepSeam(), [entered])

    // Then the entry's path from the root down: a root swapped for a symlink between the listing
    // and the removal — here while the owner is asked about the second time — is refused there,
    // never followed.
    let swapRoot = scratchRoot + "-swap"
    let movedRoot = swapRoot + "-moved"
    let outside = scratchRoot + "-outside"
    for directory in [swapRoot, movedRoot, outside] { try? manager.removeItem(atPath: directory) }
    defer { for directory in [swapRoot, movedRoot, outside] { try? manager.removeItem(atPath: directory) } }
    try! manager.createDirectory(atPath: outside, withIntermediateDirectories: true)
    try! Data("not the entry's".utf8).write(to: URL(fileURLWithPath: outside + "/kept"))
    let swapped = swapRoot + "/swapped.s1T2u3V4"
    try! manager.createDirectory(atPath: swapped + "/tree", withIntermediateDirectories: true)
    try! manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: swapped)
    try! JSONSerialization.data(withJSONObject: marker("swapped", owner: [
        "pid": 4243, "process_start": 1_700_000_000, "command": "claude"] as [String: Any]))
        .write(to: URL(fileURLWithPath: swapped + "/" + OwnedStorage.scratchMarkerName))
    try! Data("payload".utf8).write(to: URL(fileURLWithPath: swapped + "/tree/payload"))
    try! manager.createSymbolicLink(atPath: swapped + "/tree/outside", withDestinationPath: outside)
    var ownerAsked = 0
    expect("a root swapped for a symlink after the listing is refused at the removal, never followed",
           OwnedStorage.sweepScratch(root: swapRoot, now: scratchNow, graceMinutes: 60, ownerStatus: { _ in
               ownerAsked += 1
               if ownerAsked == 2 {
                   try? manager.moveItem(atPath: swapRoot, toPath: movedRoot)
                   try? manager.createSymbolicLink(atPath: swapRoot, withDestinationPath: movedRoot)
               }
               return .dead
           }, workingDirectories: { Set<String>() }), [])
    check("so the directory the symlink points at keeps every byte of the entry",
          ownerAsked == 2 && manager.fileExists(atPath: movedRoot + "/swapped.s1T2u3V4/tree/payload"))
    try? manager.removeItem(atPath: swapRoot)
    try? manager.moveItem(atPath: movedRoot, toPath: swapRoot)
    expect("with the root real again the entry goes",
           OwnedStorage.sweepScratch(root: swapRoot, now: scratchNow, graceMinutes: 60,
                                     ownerStatus: { _ in .dead },
                                     workingDirectories: { Set<String>() }), [swapped])
    check("and a symlink in its payload goes as a link, leaving what it pointed at",
          manager.fileExists(atPath: outside + "/kept"))
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

group("task retention answers its two limits separately") {
    let now = Date(timeIntervalSince1970: 1_800_000_000)
    func candidate(_ id: String, terminal: Bool = true, pending: Bool = false,
                   hoursAgo: Double) -> Orchestrator.TaskRetentionCandidate {
        let at = now.addingTimeInterval(-hoursAgo * 3600)
        return Orchestrator.TaskRetentionCandidate(id: id, terminal: terminal,
                                                   landingPending: pending,
                                                   created: at, settledAt: at)
    }

    // The directory window on its own. Both rows are minutes-fresh against the record limits, so
    // nothing here can be the count or the age answering instead.
    let day = [candidate("stale", hoursAgo: 25), candidate("recent", hoursAgo: 23)]
    let atTheDefault = Orchestrator.taskRetentionSweep(day, now: now, directoryHours: 24,
                                                       recordLimit: 1350, recordDays: 30)
    expect("the directory window sweeps what is past it", atTheDefault.directories, ["stale"])
    expect("and the same pass keeps both registry rows", atTheDefault.records, [])
    let shortWindow = Orchestrator.taskRetentionSweep(day, now: now, directoryHours: 1,
                                                      recordLimit: 1350, recordDays: 30)
    expect("a one-hour window sweeps both directories", shortWindow.directories.sorted(),
           ["recent", "stale"])
    expect("and still keeps both records", shortWindow.records, [])

    // The count on its own: every row is hours old, so no age window is near firing.
    let three = [candidate("newest", hoursAgo: 1), candidate("middle", hoursAgo: 2),
                 candidate("oldest", hoursAgo: 3)]
    let counted = Orchestrator.taskRetentionSweep(three, now: now, directoryHours: 24,
                                                  recordLimit: 2, recordDays: 30)
    expect("the count drops what is past it, ordered newest first by created",
           counted.records, ["oldest"])
    expect("and a count is not the directory's business", counted.directories, [])
    let wideCount = Orchestrator.taskRetentionSweep(three, now: now, directoryHours: 24,
                                                    recordLimit: 1350, recordDays: 30)
    expect("widening only the count keeps all three, so the count is what dropped one",
           wideCount.records, [])

    // The age on its own: two rows against a limit of 1350, so no count is near firing.
    let aged = [candidate("kept", hoursAgo: 29 * 24), candidate("expired", hoursAgo: 31 * 24)]
    let byAge = Orchestrator.taskRetentionSweep(aged, now: now, directoryHours: 24,
                                                recordLimit: 1350, recordDays: 30)
    expect("the age drops a record no count would have reached", byAge.records, ["expired"])
    expect("while the directories of both went a month ago", byAge.directories.sorted(),
           ["expired", "kept"])
    let wideAge = Orchestrator.taskRetentionSweep(aged, now: now, directoryHours: 24,
                                                  recordLimit: 1350, recordDays: 3650)
    expect("widening only the age keeps both, so the age is what dropped one", wideAge.records, [])

    // A pending landing is exempt from both, at any age and past any count.
    let obliged = [candidate("pending", pending: true, hoursAgo: 400 * 24),
                   candidate("a", hoursAgo: 300 * 24), candidate("b", hoursAgo: 200 * 24)]
    let exempt = Orchestrator.taskRetentionSweep(obliged, now: now, directoryHours: 1,
                                                 recordLimit: 1, recordDays: 1)
    check("a pending landing survives both record limits and keeps its directory",
          !exempt.records.contains("pending") && !exempt.directories.contains("pending"))
    expect("while everything beside it is swept on both", exempt.records.sorted(), ["a", "b"])

    // A live owner holds its directory and the record that names it, as a pending landing does:
    // the day a Root Session's task ended is not the day that Session stopped writing there.
    var owned = candidate("owned", hoursAgo: 400 * 24)
    owned.ownerLive = true
    let ownedSweep = Orchestrator.taskRetentionSweep([owned, candidate("gone", hoursAgo: 300 * 24)],
                                                     now: now, directoryHours: 1, recordLimit: 1,
                                                     recordDays: 1)
    check("a finished task whose process still runs keeps its directory and its record",
          !ownedSweep.directories.contains("owned") && !ownedSweep.records.contains("owned"))
    expect("while the same finish without a live owner goes on both",
           [ownedSweep.directories, ownedSweep.records], [["gone"], ["gone"]])

    // The owner question reaches every row a limit could remove, not only the rows past the
    // directory's hours: the count takes a task that finished an hour ago as readily as an old one.
    let justFinished = [candidate("newest", hoursAgo: 1), candidate("running", hoursAgo: 2),
                        candidate("unfinished", terminal: false, hoursAgo: 3)]
    expect("a count-only eviction asks after the owner of a task inside the directory window",
           Orchestrator.taskRetentionOwnerQuestions(justFinished, now: now, directoryHours: 24,
                                                    recordLimit: 1, recordDays: 30), ["running"])
    var stillRunning = justFinished
    stillRunning[1].ownerLive = true
    expect("so a live owner the count alone reaches keeps its record",
           Orchestrator.taskRetentionSweep(stillRunning, now: now, directoryHours: 24,
                                           recordLimit: 1, recordDays: 30).records, ["unfinished"])

    // A task that has not finished is never aged out; the count still reaches it, as it always did.
    // Distinct ages on purpose. The first version of this gave both rows the same `created`, and
    // `sorted { $0.created > $1.created }` is not stable, so which one the count reached was the
    // sort's business rather than the sweep's — it went red on the tie, not on the behaviour.
    let live = [candidate("live", terminal: false, hoursAgo: 400 * 24),
                candidate("done", hoursAgo: 300 * 24)]
    let running = Orchestrator.taskRetentionSweep(live, now: now, directoryHours: 24,
                                                  recordLimit: 1350, recordDays: 30)
    expect("age never removes a task that has not finished", running.records, ["done"])
    expect("and never removes its directory either", running.directories, ["done"])
    let squeezed = Orchestrator.taskRetentionSweep(live, now: now, directoryHours: 24,
                                                   recordLimit: 1, recordDays: 3650)
    expect("but the count reaches an unfinished task exactly as the fixed 200 did",
           squeezed.records, ["live"])

    // The fixed world this replaces, said as settings: the same answer, row for row.
    let many = (0..<205).map { candidate("t\($0)", hoursAgo: Double($0) + 1) }
    let asItWas = Orchestrator.taskRetentionSweep(many, now: now, directoryHours: 24,
                                                  recordLimit: 200, recordDays: 3650)
    expect("the previous fixed count, said as a setting, drops exactly the same five",
           asItWas.records.sorted(), ["t200", "t201", "t202", "t203", "t204"])
    expect("and the previous fixed day sweeps every directory past it",
           asItWas.directories.count, 181)

    // The settings themselves: what an absent key means, what a written one means, and that a
    // value outside the range is ignored rather than clamped.
    let manager = FileManager.default
    let configDirectory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-task-retention-\(UUID().uuidString)", isDirectory: true)
    defer { try? manager.removeItem(at: configDirectory) }
    try! manager.createDirectory(at: configDirectory, withIntermediateDirectories: true)
    let absent = Config(directoryForTesting: configDirectory)
    expect("an absent directory key is the day this sweep always used",
           absent.orchestratorTaskDirRetentionHours, 24)
    expect("an absent record limit is the classifier's window at this machine's measured rate",
           absent.orchestratorTaskRecordLimit, 1350)
    expect("an absent record window is the classifier's own thirty days",
           absent.orchestratorTaskRecordRetentionDays, 30)
    let writable = Config(directoryForTesting: configDirectory)
    writable.orchestratorTaskDirRetentionHours = 2
    writable.orchestratorTaskRecordLimit = 60
    writable.orchestratorTaskRecordRetentionDays = 7
    writable.save()
    let reread = Config(directoryForTesting: configDirectory)
    expect("all three round-trip through config.json",
           [reread.orchestratorTaskDirRetentionHours, reread.orchestratorTaskRecordLimit,
            reread.orchestratorTaskRecordRetentionDays], [2, 60, 7])
    let bounds: [(String, KeyPath<Config, Int>, [Int], Int)] = [
        ("orchestrator_task_dir_retention_hours", \.orchestratorTaskDirRetentionHours,
         [0, 8761], 24),
        ("orchestrator_task_record_limit", \.orchestratorTaskRecordLimit, [49, 10_001], 1350),
        ("orchestrator_task_record_retention_days", \.orchestratorTaskRecordRetentionDays,
         [0, 3651], 30),
    ]
    for (key, path, invalids, fallback) in bounds {
        for invalid in invalids {
            let data = try! JSONSerialization.data(withJSONObject: [key: invalid])
            try! data.write(to: configDirectory.appendingPathComponent("config.json"),
                            options: .atomic)
            expect("out-of-range \(key) \(invalid) falls back to the default",
                   Config(directoryForTesting: configDirectory)[keyPath: path], fallback)
        }
    }
}

group("the retention settings reach the sweep that reads them") {
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let hoursBefore = Config.shared.orchestratorTaskDirRetentionHours
    let limitBefore = Config.shared.orchestratorTaskRecordLimit
    let daysBefore = Config.shared.orchestratorTaskRecordRetentionDays
    var made: [URL] = []
    defer {
        Config.shared.orchestratorTaskDirRetentionHours = hoursBefore
        Config.shared.orchestratorTaskRecordLimit = limitBefore
        Config.shared.orchestratorTaskRecordRetentionDays = daysBefore
        for directory in made { try? manager.removeItem(at: directory) }
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }

    // The directory window is only ever *widened* here, never narrowed. `cleanup()` hands the same
    // cutoff to `cleanupOrphanWorktrees`, which once scanned the real `~/Library/Application
    // Support/Clawdline/worktrees` with only this test's fixtures as its known ids. It now reads
    // `Orchestrator.reclaimRoots`, which this binary points at a temporary root, and the habit stays:
    // the narrow direction is proved on `taskRetentionSweep` above, where nothing touches a filesystem.
    func fixture(_ hoursAgo: Double) -> (String, URL) {
        let id = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
        try! Data("retention receipt".utf8).write(to: directory.appendingPathComponent("receipt"))
        let at = Date().addingTimeInterval(-hoursAgo * 3600)
        Orchestrator.holdScheduleTaskForTesting(Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: "retention fixture",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: at,
            finishedAt: at, secretHash: String(repeating: "0", count: 64)))
        return (id, directory)
    }

    Orchestrator.forget()
    Config.shared.orchestratorTaskDirRetentionHours = 24
    Config.shared.orchestratorTaskRecordLimit = 1350
    Config.shared.orchestratorTaskRecordRetentionDays = 30
    let (dayOldID, dayOldDirectory) = fixture(25)
    let (freshID, freshDirectory) = fixture(23)
    Orchestrator.cleanup()
    check("at the shipped defaults a day-old task directory still goes, exactly as before",
          !manager.fileExists(atPath: dayOldDirectory.path))
    check("and one inside the window is kept", manager.fileExists(atPath: freshDirectory.path))
    check("while both records stay, which is the change: the row outlives the directory",
          Orchestrator.record(id: dayOldID) != nil && Orchestrator.record(id: freshID) != nil)

    Config.shared.orchestratorTaskDirRetentionHours = 8760
    let (keptID, keptDirectory) = fixture(200)
    Orchestrator.cleanup()
    check("a widened directory window keeps a directory the fixed day would have swept",
          manager.fileExists(atPath: keptDirectory.path))

    Config.shared.orchestratorTaskRecordRetentionDays = 1
    Orchestrator.cleanup()
    check("a one-day record window drops the record of a two-hundred-hour-old task",
          Orchestrator.record(id: keptID) == nil)
    check("without touching the directory, which the other setting still keeps",
          manager.fileExists(atPath: keptDirectory.path))
    check("and a task inside that window keeps its record",
          Orchestrator.record(id: freshID) != nil)

    // The count reaches a row long before the directory's hours do. A task that finished two hours
    // ago whose process still runs — this test binary's own pid — keeps its row past a count of
    // one, while the finished row beside it that the same count reaches without a live owner goes.
    Orchestrator.forget()
    Config.shared.orchestratorTaskDirRetentionHours = 8760
    Config.shared.orchestratorTaskRecordRetentionDays = 30
    Config.shared.orchestratorTaskRecordLimit = 1
    let (newestID, _) = fixture(1)
    let (runningID, _) = fixture(2)
    let (countedID, _) = fixture(3)
    Orchestrator.mutateTaskForTesting(runningID) { $0.childPID = getpid() }
    Orchestrator.cleanup()
    check("a count-only eviction keeps the row of a finished task whose process still runs",
          Orchestrator.record(id: runningID) != nil)
    check("while the count takes the finished row beside it that has no live owner",
          Orchestrator.record(id: countedID) == nil && Orchestrator.record(id: newestID) != nil)
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

    // Work written after the task ended, or by a task that ended before deadlines existed: no
    // deadline is outstanding, so the six-hourly pass decides — on the same grace, and never while
    // the task's recorded process runs. The fixture lives under this binary's reclaim roots only.
    let reclaimRoots = Orchestrator.reclaimRoots
    check("every reclaim root this binary can reach is inside its own temporary boundary",
          [reclaimRoots.worktrees.path, reclaimRoots.tasks.path, reclaimRoots.scratch,
           reclaimRoots.preserved.path].allSatisfy { $0.hasPrefix(isolatedTestStoreDirectory.path) })
    var rootSession = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
        title: "root session scratch", assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date().addingTimeInterval(-7_200), secretHash: String(repeating: "0", count: 64))
    rootSession.sessionRoot = true
    rootSession.finishedAt = Date().addingTimeInterval(-3_600)
    let laterWork = reclaimRoots.tasks.appendingPathComponent(rootSession.id, isDirectory: true)
        .appendingPathComponent("work", isDirectory: true)
    madeDirectories.append(laterWork.deletingLastPathComponent())
    try! manager.createDirectory(at: laterWork.appendingPathComponent("deploy-main"),
                                 withIntermediateDirectories: true)
    let credential = laterWork.appendingPathComponent("deploy-main/credentials.json")
    try! Data("written after the task ended".utf8).write(to: credential)
    var owner = OwnedStorage.ProcessStatus.alive
    func reclaimLaterWork() -> Bool {
        OrchestratorDraft.reclaimAfterFinishWork(rootSession, tasksRoot: reclaimRoots.tasks,
                                                 graceMinutes: 60, now: Date(),
                                                 ownerStatus: { _ in owner })
    }
    check("work written after the task ended is held while the Session's process runs",
          !reclaimLaterWork() && manager.fileExists(atPath: credential.path))
    owner = .unreadable
    check("and while that process cannot be read",
          !reclaimLaterWork() && manager.fileExists(atPath: credential.path))
    owner = .reused
    check("and goes once that pid belongs to another process",
          reclaimLaterWork() && !manager.fileExists(atPath: laterWork.path))
    let settled = Date(timeIntervalSince1970: 1_000_000)
    func laterWhy(_ state: Orchestrator.State, deadline: Date? = nil,
                  owner: OwnedStorage.ProcessStatus = .dead, grace: Int = 60,
                  after seconds: TimeInterval = 7_200) -> String {
        Orchestrator.afterFinishWorkDecision(state: state, workCleanupAt: deadline,
                                             settledAt: settled, owner: owner, graceMinutes: grace,
                                             now: settled.addingTimeInterval(seconds)).why
    }
    expect("a deadline still outstanding belongs to the beat, not to this pass",
           laterWhy(.failure, deadline: settled), "deadline_pending")
    expect("a failure's later work keeps the diagnostic grace", laterWhy(.failure, after: 60),
           "grace")
    expect("and -1 still means the day-old sweep", laterWhy(.failure, grace: -1), "grace_disabled")
    expect("a task that never recorded a process has nobody left writing there",
           laterWhy(.success, owner: .absent, after: 0), "after_finish")

    // Failure injection: the task directory itself replaced by a symlink to somewhere else. `lstat`
    // on `work` alone follows it and finds a real directory; the walk from the tasks root down sees
    // the symlink, so the payload it points at stays whole.
    let elsewhere = manager.temporaryDirectory
        .appendingPathComponent("clawdline-work-elsewhere-\(UUID().uuidString)", isDirectory: true)
    madeDirectories.append(elsewhere)
    try! manager.createDirectory(at: elsewhere.appendingPathComponent("work/deploy-main"),
                                 withIntermediateDirectories: true)
    let foreignPayload = elsewhere.appendingPathComponent("work/deploy-main/credentials.json")
    try! Data("somebody else's".utf8).write(to: foreignPayload)
    var escaped = Orchestrator.Task(
        id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
        title: "task directory replaced by a symlink", assistant: .codex, projectDir: "/tmp",
        timeoutMinutes: 30, created: Date().addingTimeInterval(-7_200),
        secretHash: String(repeating: "0", count: 64))
    escaped.finishedAt = Date().addingTimeInterval(-3_600)
    let escapedDirectory = reclaimRoots.tasks.appendingPathComponent(escaped.id, isDirectory: true)
    madeDirectories.append(escapedDirectory)
    try! manager.createSymbolicLink(at: escapedDirectory, withDestinationURL: elsewhere)
    check("a task directory replaced by a symlink is never followed out of the tasks root",
          !OrchestratorDraft.reclaimAfterFinishWork(escaped, tasksRoot: reclaimRoots.tasks,
                                                    graceMinutes: 60, now: Date(),
                                                    ownerStatus: { _ in .dead })
            && manager.fileExists(atPath: foreignPayload.path))
    let linkedTasksRoot = manager.temporaryDirectory
        .appendingPathComponent("clawdline-tasks-root-link-\(UUID().uuidString)", isDirectory: true)
    madeDirectories.append(linkedTasksRoot)
    try! manager.createSymbolicLink(at: linkedTasksRoot, withDestinationURL: reclaimRoots.tasks)
    let tasksRootPath = reclaimRoots.tasks.path
    let realWork = tasksRootPath + "/" + rootSession.id + "/work"
    try! manager.createDirectory(atPath: realWork, withIntermediateDirectories: true)
    expect("the walk proves a real path and names a symlink below the root or at it, a gap, and a way out",
           [OwnedStorage.containedDirectory(realWork, under: tasksRootPath),
            OwnedStorage.containedDirectory(escapedDirectory.path + "/work", under: tasksRootPath),
            OwnedStorage.containedDirectory(linkedTasksRoot.path + "/" + rootSession.id + "/work",
                                            under: linkedTasksRoot.path),
            OwnedStorage.containedDirectory(tasksRootPath + "/absent/work", under: tasksRootPath),
            OwnedStorage.containedDirectory(tasksRootPath + "/../work", under: tasksRootPath),
            OwnedStorage.containedDirectory(elsewhere.path + "/work", under: tasksRootPath)],
           [.proven, .refused("symlink"), .refused("root_symlink"), .missing,
            .refused("outside_root"), .refused("outside_root")])

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
        let id = checkout?.lastPathComponent ?? UUID().uuidString.lowercased()
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
    /// `<worktree root>/<slug>/<task-id>` under this binary's own root, the task id being the
    /// checkout's last component: the only shape a build deadline removes anything from.
    func checkoutURL() -> URL {
        Orchestrator.reclaimRoots.worktrees
            .appendingPathComponent("beat-fixture", isDirectory: true)
            .appendingPathComponent(UUID().uuidString.lowercased(), isDirectory: true)
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
    // Every checkout below is shaped as the broker makes them, `<worktree root>/<slug>/<task-id>`,
    // under this binary's own worktree root: a build deadline removes nothing from any other.
    let restartCheckout = Orchestrator.reclaimRoots.worktrees
        .appendingPathComponent("restart-fixture", isDirectory: true)
        .appendingPathComponent(restartID, isDirectory: true)
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
    let nestedCheckout = Orchestrator.reclaimRoots.worktrees
        .appendingPathComponent("nested-fixture", isDirectory: true)
        .appendingPathComponent(nestedID, isDirectory: true)
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

    // Failure injection: the checkout's slug directory is a symlink to a directory outside the
    // worktree root holding the same `<task-id>/.build`. `lstat` on `.build` alone would follow it;
    // the walk from the worktree root down refuses it, and the deadline settles rather than
    // retrying on every beat.
    let slugID = UUID().uuidString.lowercased()
    let slugDirectory = Orchestrator.root.appendingPathComponent(slugID, isDirectory: true)
    made.append(slugDirectory)
    try! manager.createDirectory(at: slugDirectory, withIntermediateDirectories: true)
    let outsideSlug = manager.temporaryDirectory
        .appendingPathComponent("clawdline-slug-elsewhere-\(UUID().uuidString)", isDirectory: true)
    made.append(outsideSlug)
    try! manager.createDirectory(at: outsideSlug.appendingPathComponent("\(slugID)/.build/debug"),
                                 withIntermediateDirectories: true)
    let outsideObject = outsideSlug.appendingPathComponent("\(slugID)/.build/debug/Elsewhere.o")
    try! Data("somebody else's object".utf8).write(to: outsideObject)
    let linkedSlug = Orchestrator.reclaimRoots.worktrees
        .appendingPathComponent("linked-slug-\(UUID().uuidString)", isDirectory: true)
    made.append(linkedSlug)
    try! manager.createSymbolicLink(at: linkedSlug, withDestinationURL: outsideSlug)
    let linkedCheckout = linkedSlug.appendingPathComponent(slugID, isDirectory: true)
    var linked = Orchestrator.Task(
        id: slugID, state: .briefed, kind: "custom", title: "checkout under a symlinked slug",
        assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: Date(),
        secretHash: String(repeating: "0", count: 64))
    linked.isolation = .worktree
    linked.worktree = Orchestrator.Worktree(
        path: linkedCheckout.path, branch: "clawdline/task/\(slugID)", base: "d6781a8",
        repository: linkedCheckout.path, cwd: linkedCheckout.path)
    Orchestrator.holdScheduleTaskForTesting(linked)
    Orchestrator.finalize(slugID, as: .success, summary: "delivered under a swapped slug")
    check("a build deadline never follows a symlinked parent out of the worktree root",
          manager.fileExists(atPath: outsideObject.path))
    check("and that refusal settles the deadline rather than retrying it on every beat",
          Orchestrator.buildCleanupAtForTesting(slugID) == nil)

    let staleID = UUID().uuidString.lowercased()
    let staleDirectory = Orchestrator.root.appendingPathComponent(staleID, isDirectory: true)
    made.append(staleDirectory)
    try! manager.createDirectory(at: staleDirectory.appendingPathComponent("work"),
                                 withIntermediateDirectories: true)
    let staleCheckout = Orchestrator.reclaimRoots.worktrees
        .appendingPathComponent("stale-fixture", isDirectory: true)
        .appendingPathComponent(staleID, isDirectory: true)
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

    // A landed checkout: its delta preserved and proved, then the checkout removed through git with
    // its branch kept — and any failure before the removal keeps every byte. Under this binary's
    // reclaim roots only.
    let roots = Orchestrator.reclaimRoots
    func landedCheckout() -> (repository: URL, checkout: URL, worktree: Orchestrator.Worktree,
                              id: String) {
        let id = UUID().uuidString.lowercased()
        let repository = roots.worktrees.deletingLastPathComponent()
            .appendingPathComponent("landed-repository-\(id)", isDirectory: true)
        let checkout = roots.worktrees.appendingPathComponent("landed-fixture", isDirectory: true)
            .appendingPathComponent(id, isDirectory: true)
        made.append(repository)
        made.append(checkout)
        try! manager.createDirectory(at: repository.appendingPathComponent("src"),
                                     withIntermediateDirectories: true)
        try! Data("one\n".utf8).write(to: repository.appendingPathComponent("src/a.txt"))
        try! Data("gone\n".utf8).write(to: repository.appendingPathComponent("src/gone.txt"))
        check("the landed fixture repository and its linked checkout are made", [
            ["init", "-q", "-b", "main"], ["add", "."],
            ["-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
             "-c", "commit.gpgsign=false", "commit", "-qm", "base"],
            ["worktree", "add", "-q", "-b", "clawdline/task/\(id)", checkout.path, "main"],
        ].allSatisfy { testGit($0, cwd: repository).status == 0 })
        // A Codex delivery: dirty bytes only, landed by root and still dirty.
        try! Data("one\ntwo\n".utf8).write(to: checkout.appendingPathComponent("src/a.txt"))
        try! manager.removeItem(at: checkout.appendingPathComponent("src/gone.txt"))
        try! Data("brand new\n".utf8).write(to: checkout.appendingPathComponent("src/new.txt"))
        _ = testGit(["add", "src/new.txt"], cwd: checkout)
        try! Data("notes\n".utf8).write(to: checkout.appendingPathComponent("notes 中文.txt"))
        try! manager.createDirectory(at: checkout.appendingPathComponent(".serena/cache"),
                                     withIntermediateDirectories: true)
        try! Data("tool cache".utf8).write(to: checkout.appendingPathComponent(".serena/cache/symbols"))
        let base = testGit(["rev-parse", "HEAD"], cwd: repository).output
        return (repository, checkout,
                Orchestrator.Worktree(path: checkout.path, branch: "clawdline/task/\(id)", base: base,
                                      repository: repository.path, cwd: checkout.path), id)
    }

    // Failure injection: the preservation directory cannot be written, so nothing may go.
    let failing = landedCheckout()
    var unwritable = roots
    unwritable.preserved = manager.temporaryDirectory
        .appendingPathComponent("clawdline-preserved-is-a-file-\(UUID().uuidString)")
    made.append(unwritable.preserved)
    try! Data("not a directory".utf8).write(to: unwritable.preserved)
    expect("a preservation that cannot be written keeps the landed checkout",
           OrchestratorDraft.disposeLandedWorktree(failing.worktree, taskID: failing.id,
                                                   roots: unwritable),
           .kept("preservation_unwritable"))
    check("with every uncommitted byte still in it",
          manager.fileExists(atPath: failing.checkout.appendingPathComponent("src/new.txt").path)
            && manager.fileExists(atPath: failing.checkout.appendingPathComponent("notes 中文.txt").path))
    let mixed = landedCheckout()
    try! Data("brand new, then edited\n".utf8).write(to: mixed.checkout.appendingPathComponent("src/new.txt"))
    expect("index bytes a working-tree patch cannot carry keep the checkout",
           OrchestratorDraft.disposeLandedWorktree(mixed.worktree, taskID: mixed.id, roots: roots),
           .kept("staged_differs_from_worktree"))

    let landed = landedCheckout()
    let outcome = OrchestratorDraft.disposeLandedWorktree(landed.worktree, taskID: landed.id,
                                                          roots: roots)
    var preserved = ""
    if case .removed(let path) = outcome { preserved = path }
    check("a landed checkout is removed only after its delta was preserved",
          !preserved.isEmpty && !manager.fileExists(atPath: landed.checkout.path)
            && manager.fileExists(atPath: preserved + "/manifest.json"), "\(outcome)")
    check("its delivery branch is kept, and git no longer lists the checkout",
          testGit(["rev-parse", "--verify", "refs/heads/clawdline/task/\(landed.id)"],
                  cwd: landed.repository).status == 0
            && !testGit(["worktree", "list", "--porcelain"], cwd: landed.repository).output
                .contains("landed-fixture/\(landed.id)"))
    let manifest = (try? JSONSerialization.jsonObject(
        with: Data(contentsOf: URL(fileURLWithPath: preserved + "/manifest.json")))) as? [String: Any]
    check("the manifest names base, head, branch and each preserved file's SHA-256",
          manifest?["base"] as? String == landed.worktree.base
            && manifest?["branch"] as? String == landed.worktree.branch
            && (manifest?["head"] as? String)?.count == 40
            && ((manifest?["patch"] as? [String: Any])?["sha256"] as? String)?.count == 64
            && ((manifest?["untracked"] as? [String: Any])?["sha256"] as? String)?.count == 64)
    // Rebuild the delivery from nothing but what was preserved.
    let restored = manager.temporaryDirectory
        .appendingPathComponent("clawdline-restored-\(UUID().uuidString)", isDirectory: true)
    made.append(restored)
    let patched = testGit(["worktree", "add", "-q", "--detach", restored.path,
                           "clawdline/task/\(landed.id)"], cwd: landed.repository).status == 0
        && testGit(["apply", "--index", preserved + "/delta.patch"], cwd: restored).status == 0
    let extract = Process()
    extract.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
    extract.arguments = ["-x", "-f", preserved + "/untracked.tar", "-C", restored.path]
    var extracted = false
    if patched, (try? extract.run()) != nil {
        extract.waitQuietly()
        extracted = extract.terminationStatus == 0
    }
    func restoredText(_ name: String) -> String? {
        try? String(contentsOf: restored.appendingPathComponent(name), encoding: .utf8)
    }
    check("the preserved patch and archive rebuild exactly the delivery that was removed",
          extracted && restoredText("src/a.txt") == "one\ntwo\n"
            && restoredText("src/new.txt") == "brand new\n" && restoredText("notes 中文.txt") == "notes\n"
            && !manager.fileExists(atPath: restored.appendingPathComponent("src/gone.txt").path)
            && !manager.fileExists(atPath: restored.appendingPathComponent(".serena").path))

    // Tool noise does not make a checkout dirty for the ordinary sweep either.
    let noisy = landedCheckout()
    check("a committed checkout whose only untracked directory is tool noise is made", [
        ["add", "-A", "--", "src", "notes 中文.txt"],
        ["-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
         "-c", "commit.gpgsign=false", "commit", "-qm", "delivered"],
    ].allSatisfy { testGit($0, cwd: noisy.checkout).status == 0 })
    OrchestratorDraft.disposeWorktree(noisy.worktree, taskID: noisy.id, why: "swept")
    check("and the sweep removes it through git, keeping its branch",
          !manager.fileExists(atPath: noisy.checkout.path)
            && testGit(["rev-parse", "--verify", "refs/heads/clawdline/task/\(noisy.id)"],
                       cwd: noisy.repository).status == 0)

    // Retention: an attempt past its days goes; a directory this app did not write stays.
    let expiredTask = UUID().uuidString.lowercased()
    let expired = roots.preserved.appendingPathComponent("\(expiredTask)/1-old", isDirectory: true)
    let foreignAttempt = roots.preserved.appendingPathComponent("\(expiredTask)/not-ours",
                                                                isDirectory: true)
    for directory in [expired, foreignAttempt] {
        try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    }
    try! JSONSerialization.data(withJSONObject: [
        "clawdline_reclaimed_checkout": 1, "task": expiredTask, "created_at": 1,
    ]).write(to: expired.appendingPathComponent("manifest.json"))
    OrchestratorDraft.pruneReclaimedCheckouts(root: roots.preserved, retentionDays: 30, now: Date())
    check("a preserved delta past its retention goes, and a directory without this app's manifest stays",
          !manager.fileExists(atPath: expired.path) && manager.fileExists(atPath: foreignAttempt.path)
            && manager.fileExists(atPath: preserved + "/manifest.json"))
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
        // Under this binary's worktree root, shaped `<slug>/<task-id>`: the only kind of checkout a
        // build deadline removes anything from.
        let checkout = Orchestrator.reclaimRoots.worktrees
            .appendingPathComponent("build-fixture", isDirectory: true)
            .appendingPathComponent(id, isDirectory: true)
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

    // A Session still building in its checkout: the task's recorded process is this test binary,
    // running. Its success keeps the build output and the deadline, and the reclaim goes through on
    // the first ask after that pid has come to belong to another process.
    var (building, buildingCheckout) = fixture()
    building.childPID = getpid()
    Orchestrator.holdScheduleTaskForTesting(building)
    Orchestrator.finalize(building.id, as: .success, summary: "delivered, still building")
    check("a success keeps the build output of a checkout whose task's process still runs",
          manager.fileExists(atPath: objectFile(buildingCheckout!))
            && Orchestrator.buildCleanupAtForTesting(building.id) != nil)
    Orchestrator.mutateTaskForTesting(building.id) {
        $0.childProcStart = Date(timeIntervalSince1970: 1)
    }
    check("and the next ask takes it once that pid belongs to a process that started at another time",
          Orchestrator.reclaimTaskBuildIfDue(building.id)
            && !manager.fileExists(atPath: buildingCheckout!.appendingPathComponent(".build").path)
            && Orchestrator.buildCleanupAtForTesting(building.id) == nil)

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

    // Dependency directories: a real repository and a linked checkout under this binary's worktree
    // root, never the live one.
    let reclaimRoots = Orchestrator.reclaimRoots
    let dependencyID = UUID().uuidString.lowercased()
    let dependencyRepository = reclaimRoots.worktrees.deletingLastPathComponent()
        .appendingPathComponent("dependency-repository-\(dependencyID)", isDirectory: true)
    let dependencyCheckout = reclaimRoots.worktrees
        .appendingPathComponent("dependency-fixture", isDirectory: true)
        .appendingPathComponent(dependencyID, isDirectory: true)
    made.append(dependencyRepository)
    made.append(dependencyCheckout.deletingLastPathComponent())
    try! manager.createDirectory(at: dependencyRepository.appendingPathComponent("vendor/node_modules"),
                                 withIntermediateDirectories: true)
    try! Data("node_modules/\n.venv/\n".utf8)
        .write(to: dependencyRepository.appendingPathComponent(".gitignore"))
    try! Data("vendored on purpose".utf8)
        .write(to: dependencyRepository.appendingPathComponent("vendor/node_modules/kept.js"))
    check("the dependency fixture repository and its linked checkout are made", [
        ["init", "-q", "-b", "main"], ["add", "-f", "."],
        ["-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
         "-c", "commit.gpgsign=false", "commit", "-qm", "base"],
        ["worktree", "add", "-q", "-b", "clawdline/task/\(dependencyID)", dependencyCheckout.path, "main"],
    ].allSatisfy { testGit($0, cwd: dependencyRepository).status == 0 })
    for directory in ["relay/node_modules/pkg", "packages/a/node_modules", ".venv/bin",
                      "vendor/node_modules/extra"] {
        try! manager.createDirectory(at: dependencyCheckout.appendingPathComponent(directory),
                                     withIntermediateDirectories: true)
        try! Data("installed".utf8)
            .write(to: dependencyCheckout.appendingPathComponent(directory + "/file"))
    }
    try! manager.createDirectory(at: dependencyRepository.appendingPathComponent("elsewhere/node_modules"),
                                 withIntermediateDirectories: true)
    try! manager.createSymbolicLink(at: dependencyCheckout.appendingPathComponent("linked"),
                                    withDestinationURL: dependencyRepository.appendingPathComponent("elsewhere"))
    let reclaimed = OrchestratorDraft.reclaimDependencyDirectories(checkout: dependencyCheckout.path,
                                                                    taskID: dependencyID)
    expect("ignored node_modules and .venv inside the task's own checkout go, and nothing else",
           Set(reclaimed.map { String($0.dropFirst(dependencyCheckout.path.count + 1)) }),
           ["relay/node_modules", "packages/a/node_modules", ".venv"])
    check("a node_modules holding a tracked file stays, with what was installed beside it",
          manager.fileExists(atPath: dependencyCheckout.appendingPathComponent("vendor/node_modules/kept.js").path)
            && manager.fileExists(atPath: dependencyCheckout
                .appendingPathComponent("vendor/node_modules/extra/file").path))
    expect("and is refused for what it holds",
           OrchestratorDraft.dependencyDirectoryVerdict("vendor/node_modules",
                                                        checkout: dependencyCheckout.path),
           .refused("tracked"))
    expect("a dependency directory reached through a symlink is refused, never followed",
           OrchestratorDraft.dependencyDirectoryVerdict("linked/node_modules",
                                                        checkout: dependencyCheckout.path),
           .refused("symlink"))
    check("so what the symlink points at is untouched",
          manager.fileExists(atPath: dependencyRepository.appendingPathComponent("elsewhere/node_modules").path))
    let foreignCheckout = manager.temporaryDirectory
        .appendingPathComponent("clawdline-foreign-checkout-\(UUID().uuidString)", isDirectory: true)
    made.append(foreignCheckout)
    try! manager.createDirectory(at: foreignCheckout.appendingPathComponent("node_modules"),
                                 withIntermediateDirectories: true)
    check("a checkout outside the worktree root is never read, whatever it holds",
          OrchestratorDraft.reclaimDependencyDirectories(checkout: foreignCheckout.path,
                                                         taskID: dependencyID).isEmpty
            && manager.fileExists(atPath: foreignCheckout.appendingPathComponent("node_modules").path))
    func dependencyWhy(_ owner: OwnedStorage.ProcessStatus, deadline: Date? = nil) -> String {
        Orchestrator.dependencyReclaimDecision(state: .success, buildCleanupAt: deadline,
                                               settledAt: Date(timeIntervalSince1970: 100),
                                               owner: owner, graceMinutes: 60,
                                               now: Date(timeIntervalSince1970: 200)).why
    }
    expect("dependency directories wait for the task's process, not only for the build deadline",
           [dependencyWhy(.alive), dependencyWhy(.unreadable), dependencyWhy(.dead),
            dependencyWhy(.dead, deadline: Date(timeIntervalSince1970: 150))],
           ["owner_alive", "owner_unreadable", "build_deadline", "deadline_pending"])

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

    // The three reclaim settings reclaim with nobody configuring anything, and a value outside
    // their range is ignored rather than clamped.
    let reclaimKnobs: [(String, KeyPath<Config, Int>, [Int], Int)] = [
        ("orchestrator_landed_checkout_grace_minutes", \.orchestratorLandedCheckoutGraceMinutes,
         [-2, 1_441], 60),
        ("orchestrator_scratch_grace_minutes", \.orchestratorScratchGraceMinutes, [-2, 1_441], 60),
        ("orchestrator_reclaimed_checkout_retention_days",
         \.orchestratorReclaimedCheckoutRetentionDays, [0, 366], 30),
    ]
    for (key, path, invalids, fallback) in reclaimKnobs {
        for invalid in invalids {
            try! JSONSerialization.data(withJSONObject: [key: invalid])
                .write(to: configDirectory.appendingPathComponent("config.json"), options: .atomic)
            expect("out-of-range \(key) \(invalid) falls back to its default",
                   Config(directoryForTesting: configDirectory)[keyPath: path], fallback)
        }
    }
    let knobs = Config(directoryForTesting: configDirectory)
    knobs.orchestratorLandedCheckoutGraceMinutes = -1
    knobs.orchestratorScratchGraceMinutes = 0
    knobs.orchestratorReclaimedCheckoutRetentionDays = 365
    knobs.save()
    let rereadKnobs = Config(directoryForTesting: configDirectory)
    expect("all three reclaim settings round-trip through config.json",
           [rereadKnobs.orchestratorLandedCheckoutGraceMinutes,
            rereadKnobs.orchestratorScratchGraceMinutes,
            rereadKnobs.orchestratorReclaimedCheckoutRetentionDays], [-1, 0, 365])

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
}
