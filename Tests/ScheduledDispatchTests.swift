import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Scheduled dispatches

// An empty *complete* inventory: `closeStep` answers `.wait` on it — an inventory with no
// terminals in it at all proves nothing — so a background beat that finds one of this suite's
// fixtures due decides nothing, writes nothing, and above all asks nobody's real terminal.
// Individual groups still install their own walk over this one.

func runScheduledDispatchTests() {
Targets.safeCloseInventoryFallbackForTesting = { Targets.Snapshot() }
group("schedule files are strict and carry an ordinary task template") {
    let id = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let base: [String: Any] = [
        "clawdline_schedule": 1,
        "schedule_id": id,
        "title": "publish the blog",
        "when": ["at": "23:45", "days": ["mon", "wed", "fri"]],
        "task": [
            "assistant": "codex", "model": "gpt-5.6-sol", "project_dir": "/tmp",
            "title": "publish", "instructions": "publish the next post",
            "claims": ["posts"], "timeout_minutes": 45,
            "graph": ["id": "11111111-2222-3333-4444-555555555555",
                      "destination": "The post is published.", "current_node": "publish",
                      "nodes": [["id": "publish", "title": "Publish", "kind": "delivery",
                                 "depends_on": [], "acceptance": ["The post is live."]]],
                      "unknowns": [], "out_of_scope": []],
        ],
        "enabled": true,
    ]
    switch Orchestrator.schedule(from: base, filename: "\(id).json", isDirectory: { $0 == "/tmp" }) {
    case .bad(let why):
        check("a valid schedule parses", false, why)
    case .ok(let schedule):
        expect("the default close policy is on-success", schedule.closeTab, .onSuccess)
        expect("the default catch-up window is six hours", schedule.catchUpHours, 6)
        expect("the default failure notification is on", schedule.notifyOnFailure, true)
        expect("weekday names become Calendar weekday numbers", schedule.when, .weekly([2, 4, 6]))
        // Every schedule file written before this field existed, and every one somebody typed in
        // an editor, is this case. It has to keep meaning "as far back as anyone knows".
        check("a file that never said when it was made parses and says so",
              schedule.createdAt == nil)
    }
    var stamped = base
    stamped["created_at"] = 1_787_000_000
    if case .ok(let schedule) = Orchestrator.schedule(from: stamped, filename: "\(id).json",
                                                      isDirectory: { $0 == "/tmp" }) {
        expect("and a file that did carries the instant back off disk",
               schedule.createdAt, Date(timeIntervalSince1970: 1_787_000_000))
    } else {
        check("and a file that did carries the instant back off disk", false)
    }
    for (name, mutate) in [
        ("wrong schema version", { (value: inout [String: Any]) in value["clawdline_schedule"] = 2 }),
        ("unknown top-level field", { (value: inout [String: Any]) in value["surprise"] = true }),
        ("wrong filename", { (value: inout [String: Any]) in value["schedule_id"] = UUID().uuidString.lowercased() }),
        ("empty title", { (value: inout [String: Any]) in value["title"] = "" }),
        ("oversized title", { (value: inout [String: Any]) in value["title"] = String(repeating: "x", count: 121) }),
        ("unknown when field", { (value: inout [String: Any]) in value["when"] = ["at": "23:45", "days": "daily", "zone": "UTC"] }),
        ("missing days", { (value: inout [String: Any]) in value["when"] = ["at": "23:45"] }),
        ("empty days", { (value: inout [String: Any]) in value["when"] = ["at": "23:45", "days": []] }),
        ("unknown task field", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["shell"] = "oops"; value["task"] = task
        }),
        ("bad minute", { (value: inout [String: Any]) in value["when"] = ["at": "23:60", "days": "daily"] }),
        ("unknown weekday", { (value: inout [String: Any]) in value["when"] = ["at": "23:45", "days": ["monday"]] }),
        ("duplicate day", { (value: inout [String: Any]) in value["when"] = ["at": "23:45", "days": ["mon", "mon"]] }),
        ("numeric enabled", { (value: inout [String: Any]) in value["enabled"] = 1 }),
        ("missing enabled", { (value: inout [String: Any]) in value.removeValue(forKey: "enabled") }),
        ("unknown close policy", { (value: inout [String: Any]) in value["close_tab"] = "later" }),
        ("boolean catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = true }),
        ("oversized catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = 169 }),
        ("negative catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = -1 }),
        ("numeric notification", { (value: inout [String: Any]) in value["notify_on_failure"] = 1 }),
        // `created_at` decides whether an occurrence is dispatched, so it gets the same treatment
        // as every other field on the list rather than being read for whatever it happens to be.
        ("a written date where the made-at goes",
         { (value: inout [String: Any]) in value["created_at"] = "2026-08-26T09:00:00Z" }),
        ("a fractional made-at", { (value: inout [String: Any]) in value["created_at"] = 1.5 }),
        ("a boolean made-at", { (value: inout [String: Any]) in value["created_at"] = true }),
        ("a made-at before 1970", { (value: inout [String: Any]) in value["created_at"] = -1 }),
        ("non-string optional task field", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["model"] = 7; value["task"] = task
        }),
        ("bad assistant", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["assistant"] = "shell"; value["task"] = task
        }),
        ("missing instructions", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task.removeValue(forKey: "instructions"); value["task"] = task
        }),
        ("absolute claim", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["claims"] = ["/tmp"]; value["task"] = task
        }),
        ("duplicate serialization token", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["serialize"] = ["deploy", "deploy"]; value["task"] = task
        }),
        ("bad permission", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["permission_mode"] = "sudo"; value["task"] = task
        }),
        ("zero timeout", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["timeout_minutes"] = 0; value["task"] = task
        }),
        ("malformed deliverables", { (value: inout [String: Any]) in
            var task = value["task"] as! [String: Any]; task["deliverables"] = [7]; value["task"] = task
        }),
    ] {
        var broken = base
        mutate(&broken)
        if case .ok = Orchestrator.schedule(from: broken, filename: "\(id).json",
                                            isDirectory: { $0 == "/tmp" }) {
            check("rejects \(name)", false)
        } else {
            check("rejects \(name)", true)
        }
    }
}

group("schedule fire arithmetic crosses midnight and filters days") {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let formatter = ISO8601DateFormatter()
    func date(_ value: String) -> Date { formatter.date(from: value)! }
    let daily = Orchestrator.Schedule(id: "a", title: "a", hour: 23, minute: 45,
        when: .daily, taskTemplate: [:], enabled: true, closeTab: .onSuccess,
        catchUpHours: 6, notifyOnFailure: true, createdAt: nil, whenChangedAt: nil)
    expect("after midnight sees yesterday's fire",
           Orchestrator.latestFire(of: daily, at: date("2026-08-26T00:15:00Z"), calendar: calendar),
           date("2026-08-25T23:45:00Z"))
    let monday = Orchestrator.Schedule(id: "b", title: "b", hour: 9, minute: 0,
        when: .weekly([2]), taskTemplate: [:], enabled: true, closeTab: .onSuccess,
        catchUpHours: 6, notifyOnFailure: true, createdAt: nil, whenChangedAt: nil)
    expect("Tuesday looks back to the selected Monday",
           Orchestrator.latestFire(of: monday, at: date("2026-08-25T10:00:00Z"), calendar: calendar),
           date("2026-08-24T09:00:00Z"))
}

group("catch-up, active-task and close-tab policies are explicit") {
    let fire = Date(timeIntervalSince1970: 1_000_000)
    expect("a fire inside the catch-up window runs",
           Orchestrator.scheduleAction(now: fire.addingTimeInterval(5 * 3600), fire: fire,
                                       catchUpHours: 6, lastRunCreated: nil,
                                       lastRunTerminal: nil, createdAt: nil,
                                       whenChangedAt: nil), .run)
    expect("a fire outside the window is missed",
           Orchestrator.scheduleAction(now: fire.addingTimeInterval(7 * 3600), fire: fire,
                                       catchUpHours: 6, lastRunCreated: nil,
                                       lastRunTerminal: nil, createdAt: nil,
                                       whenChangedAt: nil), .missed)
    expect("the same occurrence does not run twice",
           Orchestrator.scheduleAction(now: fire, fire: fire, catchUpHours: 6,
                                       lastRunCreated: fire, lastRunTerminal: true,
                                       createdAt: nil, whenChangedAt: nil), .alreadyHandled)
    expect("an older still-running occurrence blocks overlap",
           Orchestrator.scheduleAction(now: fire, fire: fire, catchUpHours: 6,
                                       lastRunCreated: fire.addingTimeInterval(-86400),
                                       lastRunTerminal: false, createdAt: nil,
                                       whenChangedAt: nil), .active)
    let now = Date()
    check("on-success closes a successful child immediately",
          Orchestrator.scheduledCloseAt(policy: .onSuccess, outcome: .success,
                                        now: now, hasChild: true) == now)
    check("on-success leaves a failed child for takeover",
          Orchestrator.scheduledCloseAt(policy: .onSuccess, outcome: .failure,
                                        now: now, hasChild: true) == nil)
    check("always closes every terminal outcome",
          Orchestrator.scheduledCloseAt(policy: .always, outcome: .timeout,
                                        now: now, hasChild: true) == now)
    expect("never uses the existing linger instead of closing immediately",
           Orchestrator.scheduledCloseAt(policy: .never, outcome: .success,
                                         now: now, hasChild: true, linger: 180),
           now.addingTimeInterval(180))
    check("a schedule never closes a tab that was never opened",
          Orchestrator.scheduledCloseAt(policy: .always, outcome: .success,
                                        now: now, hasChild: false) == nil)
    check("never honours the global keep-tabs setting",
          Orchestrator.scheduledCloseAt(policy: .never, outcome: .success,
                                        now: now, hasChild: true, linger: -1) == nil)
    check("never still closes an unbriefed failed spawn when linger is enabled",
          Orchestrator.scheduledCloseAt(policy: .never, outcome: .spawnFailed,
                                        now: now, hasChild: true, linger: 180,
                                        briefed: false) == now)
    expect("zero-hour catch-up still has its one-minute floor",
           Orchestrator.scheduleAction(now: fire.addingTimeInterval(60), fire: fire,
                                       catchUpHours: 0, lastRunCreated: nil,
                                       lastRunTerminal: nil, createdAt: nil,
                                       whenChangedAt: nil), .run)
    expect("and expires immediately after that minute",
           Orchestrator.scheduleAction(now: fire.addingTimeInterval(61), fire: fire,
                                       catchUpHours: 0, lastRunCreated: nil,
                                       lastRunTerminal: nil, createdAt: nil,
                                       whenChangedAt: nil), .missed)

    let scheduleID = "dddddddd-eeee-4fff-8aaa-bbbbbbbbbbbb"
    let row: [String: Any] = [
        "id": "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa", "state": "success",
        "kind": "custom", "title": "scheduled", "assistant": "codex",
        "project_dir": "/tmp", "timeout_minutes": 30,
        "created": now.timeIntervalSince1970, "secret_hash": Orchestrator.hash(ofSecret: "x"),
        "artifacts": [], "schedule_id": scheduleID, "schedule_close_tab": "always",
        "schedule_notify_failure": false,
    ]
    let restored = OrchestratorStore.task(from: row)!
    expect("the optional schedule id survives registry loading", restored.scheduleID, scheduleID)
    expect("and appears in the public task record",
           Orchestrator.recordForTesting(restored)["schedule_id"] as? String, scheduleID)
    expect("the originating close policy survives registry storage",
           OrchestratorStore.stored(restored)["schedule_close_tab"] as? String, "always")
}

group("no occurrence from before a schedule was made is an occurrence it missed") {
    // The table a reviewer produced by lifting these functions out and running them: "09:00
    // daily", never run, made from a phone at six times of day. Every row used to be wrong, and
    // wrong within sixty seconds of pressing Create — inside the six-hour catch-up window the
    // minute timer really opened a session for a morning nobody had asked it to catch up on, and
    // outside it the timer pushed "Scheduled run missed its catch-up window" and left "last missed
    // 1 day ago" on the row. Both while the `200` that made the schedule said the next run was
    // tomorrow. Most people arrange tomorrow morning in the afternoon, so this was the first ten
    // minutes of the feature.
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let formatter = ISO8601DateFormatter()
    func date(_ value: String) -> Date { formatter.date(from: value)! }
    func nine(madeAt: Date?, changedAt: Date? = nil) -> Orchestrator.Schedule {
        Orchestrator.Schedule(id: "c", title: "c", hour: 9, minute: 0, when: .daily,
                              taskTemplate: [:], enabled: true, closeTab: .onSuccess,
                              catchUpHours: 6, notifyOnFailure: true, createdAt: madeAt,
                              whenChangedAt: changedAt)
    }
    // The beat's own two steps: the newest occurrence at or before `now`, and what to do about it.
    func beatWould(_ schedule: Orchestrator.Schedule, at now: Date) -> Orchestrator.ScheduleAction? {
        guard let fire = Orchestrator.latestFire(of: schedule, at: now, calendar: calendar)
        else { return nil }
        return Orchestrator.scheduleAction(now: now, fire: fire,
                                           catchUpHours: schedule.catchUpHours,
                                           lastRunCreated: nil, lastRunTerminal: nil,
                                           createdAt: schedule.createdAt,
                                           whenChangedAt: schedule.whenChangedAt)
    }
    let madeAtTimes = ["08:00", "09:30", "13:00", "14:59", "15:01", "20:00"]
    for time in madeAtTimes {
        let made = date("2026-08-26T\(time):00Z")
        let schedule = nine(madeAt: made)
        expect("made at \(time), the next minute's beat leaves this morning alone",
               beatWould(schedule, at: made.addingTimeInterval(60)), .beforeCreation)
        // And the schedule is not thereby dead: the first occurrence it was actually there for is
        // the one the `200` named, and that one runs.
        guard let next = Orchestrator.nextFire(of: schedule, after: made, calendar: calendar) else {
            check("made at \(time), there is a next occurrence at all", false)
            continue
        }
        expect("made at \(time), the first occurrence it existed for runs",
               beatWould(schedule, at: next.addingTimeInterval(60)), .run)
    }
    // The same six, from a file that never said when it was made — a schedule somebody wrote in an
    // editor, and every file written before this field existed. These are the answers the timer
    // has always given, and nothing here is allowed to change them.
    let unstamped: [String: Orchestrator.ScheduleAction] = [
        "08:00": .missed, "09:30": .run, "13:00": .run,
        "14:59": .run, "15:01": .missed, "20:00": .missed,
    ]
    for time in madeAtTimes {
        let now = date("2026-08-26T\(time):00Z").addingTimeInterval(60)
        expect("a hand-written file decides \(time) exactly as it always has",
               beatWould(nine(madeAt: nil), at: now), unstamped[time])
    }
    // The boundary itself, stated once rather than inferred from the table: the occurrence at the
    // very instant of creation belongs to the schedule, and the second before it does not.
    let made = date("2026-08-26T09:00:00Z")
    expect("an occurrence exactly at the made-at is one this schedule was there for",
           beatWould(nine(madeAt: made), at: made.addingTimeInterval(60)), .run)
    expect("and one second earlier is not",
           beatWould(nine(madeAt: made.addingTimeInterval(1)), at: made.addingTimeInterval(60)),
           .beforeCreation)
}

group("an edit does not fire the occurrence it has just invented") {
    // The reviewer's second table, measured by lifting `latestFire` and `scheduleAction` out and
    // running them. A schedule made yesterday at 08:00 to run "21:00 daily", which ran yesterday
    // at 21:00. Move it to "09:00 daily" from a phone at two in the afternoon and the next
    // minute's beat opened a session for this morning's nine o'clock — an occurrence that did not
    // exist until the save invented it — while the same save's `200` said the next run was
    // tomorrow. Do it at five instead, past the six-hour catch-up window, and it pushed
    // "Scheduled run missed its catch-up window" about a run nobody was ever owed.
    //
    // `created_at` cannot answer this and must not try: the schedule really is a day old, and a
    // save that restamped it would break the thing that field does hold. What was missing is a
    // second stamp saying when the firing times last moved.
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let formatter = ISO8601DateFormatter()
    func date(_ value: String) -> Date { formatter.date(from: value)! }
    let madeAt = date("2026-08-25T08:00:00Z")
    let ranYesterday = date("2026-08-25T21:00:00Z")
    func schedule(hour: Int, changedAt: Date?) -> Orchestrator.Schedule {
        Orchestrator.Schedule(id: "d", title: "d", hour: hour, minute: 0, when: .daily,
                              taskTemplate: [:], enabled: true, closeTab: .onSuccess,
                              catchUpHours: 6, notifyOnFailure: true, createdAt: madeAt,
                              whenChangedAt: changedAt)
    }
    func beatWould(_ schedule: Orchestrator.Schedule, at now: Date,
                   ranAt lastRun: Date?) -> Orchestrator.ScheduleAction? {
        guard let fire = Orchestrator.latestFire(of: schedule, at: now, calendar: calendar)
        else { return nil }
        return Orchestrator.scheduleAction(now: now, fire: fire,
                                           catchUpHours: schedule.catchUpHours,
                                           lastRunCreated: lastRun,
                                           lastRunTerminal: lastRun.map { _ in true },
                                           createdAt: schedule.createdAt,
                                           whenChangedAt: schedule.whenChangedAt)
    }
    // The two rows of the table, and beside each one the answer a file with no second stamp still
    // gives: a schedule somebody retimed in a text editor, and every file written before this
    // field existed. Those answers are the old ones and nothing here is allowed to change them.
    for (time, unstamped) in [("14:00", Orchestrator.ScheduleAction.run),
                              ("17:00", Orchestrator.ScheduleAction.missed)] {
        let savedAt = date("2026-08-26T\(time):00Z")
        let beat = savedAt.addingTimeInterval(60)
        expect("moved to 09:00 at \(time) in an editor, the beat answers as it always has",
               beatWould(schedule(hour: 9, changedAt: nil), at: beat, ranAt: ranYesterday),
               unstamped)
        expect("moved to 09:00 at \(time) through a save, this morning is not a run it missed",
               beatWould(schedule(hour: 9, changedAt: savedAt), at: beat, ranAt: ranYesterday),
               .beforeRetiming)
    }
    // The control the reviewer ran beside it: the same schedule created rather than edited at two
    // in the afternoon is already covered, by the stamp this one deliberately leaves alone.
    let savedAt = date("2026-08-26T14:00:00Z")
    let beat = savedAt.addingTimeInterval(60)
    expect("a schedule created at 14:00 was already safe, and still is",
           beatWould(Orchestrator.Schedule(id: "e", title: "e", hour: 9, minute: 0, when: .daily,
                                           taskTemplate: [:], enabled: true, closeTab: .onSuccess,
                                           catchUpHours: 6, notifyOnFailure: true,
                                           createdAt: savedAt, whenChangedAt: nil),
                     at: beat, ranAt: nil),
           .beforeCreation)
    // Moving one *later* invents an occurrence too, and on a schedule that has not run the old
    // answer was a push claiming a run was missed.
    expect("moved to 21:00 at 14:00, yesterday evening is not a run it missed either",
           beatWould(schedule(hour: 21, changedAt: savedAt), at: beat, ranAt: nil),
           .beforeRetiming)
    expect("and in an editor that same move still pushes what it always pushed",
           beatWould(schedule(hour: 21, changedAt: nil), at: beat, ranAt: nil), .missed)
    // The schedule is not thereby dead. The first occurrence under the new terms is the one the
    // save's own answer named, and that one runs.
    guard let next = Orchestrator.nextFire(of: schedule(hour: 9, changedAt: savedAt),
                                           after: savedAt, calendar: calendar) else {
        check("there is a next occurrence at all", false)
        return
    }
    expect("tomorrow's nine o'clock, the first one the new time really has, runs",
           beatWould(schedule(hour: 9, changedAt: savedAt), at: next.addingTimeInterval(60),
                     ranAt: ranYesterday), .run)
    // A stamp from an earlier day gates nothing today, which is what keeps a real catch-up alive
    // for every schedule that was retimed once and has been left alone since.
    expect("a schedule retimed days ago catches up exactly as an untouched one does",
           beatWould(schedule(hour: 9, changedAt: date("2026-08-20T14:00:00Z")),
                     at: date("2026-08-26T13:00:00Z"), ranAt: ranYesterday), .run)
    // The boundary, stated rather than inferred: the occurrence at the instant of the save
    // belongs to the new terms, and the second before it does not.
    let nineToday = date("2026-08-26T09:00:00Z")
    expect("an occurrence exactly at the save is one the new time really has",
           beatWould(schedule(hour: 9, changedAt: nineToday),
                     at: nineToday.addingTimeInterval(60), ranAt: nil), .run)
    expect("and one second earlier is not",
           beatWould(schedule(hour: 9, changedAt: nineToday.addingTimeInterval(1)),
                     at: nineToday.addingTimeInterval(60), ranAt: nil), .beforeRetiming)
}

group("bad schedule files are isolated and the routes use orchestrator envelopes") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedules-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
    }
    let id = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
    let valid: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": id, "title": "nightly",
        "when": ["at": "01:30", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": "/tmp",
                 "instructions": "do the nightly work"],
        "enabled": true,
    ]
    let data = try! JSONSerialization.data(withJSONObject: valid)
    try! data.write(to: directory.appendingPathComponent("\(id).json"))
    try! Data("not json".utf8).write(to: directory.appendingPathComponent("broken.json"))
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    expect("one bad file does not hide its valid neighbor", Orchestrator.schedules().count, 1)

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let listed = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules", headers: auth))
    expect("the schedule list route answers", listed.status, 200)
    let listedBody = (try? JSONSerialization.jsonObject(with: listed.body)) as? [String: Any]
    let listedRows = listedBody?["schedules"] as? [[String: Any]] ?? []
    let validRow = listedRows.first { $0["id"] as? String == id }
    check("the GET body carries every documented schedule field",
          validRow?["title"] as? String == "nightly"
            && validRow?["enabled"] as? Bool == true
            && validRow?["next_fire"] is Int)
    let settingsRows = ScheduleSettingsRow.rows(from: listedRows)
    check("the real inventory feeds a writable settings row with the enforced filename",
          settingsRows.first?.id == id && settingsRows.first?.file == "\(id).json")
    let invalidRow = listedRows.first { $0["file"] as? String == "broken.json" }
    check("an invalid file remains visible with a useful error",
          invalidRow?["state"] as? String == "invalid"
            && !(invalidRow?["error"] as? String ?? "").isEmpty)
    Orchestrator.scheduleRunnerForTesting = { schedule in
        .ok(["ok": true, "task": ["id": "generated", "schedule_id": schedule.id]])
    }
    let manual = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/schedules/\(id)/run", headers: auth))
    expect("manual run returns the ordinary dispatch envelope", manual.status, 200)
    let manualBody = (try? JSONSerialization.jsonObject(with: manual.body)) as? [String: Any]
    expect("and dispatches the named source file",
           (manualBody?["task"] as? [String: Any])?["schedule_id"] as? String, id)
    Orchestrator.scheduleRunnerForTesting = nil
    let missing = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/schedules/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/run",
                      headers: auth))
    expect("manual run uses the ordinary not-found envelope", missing.status, 404)
    expect("and its typed code", remoteErrorCode(missing), "not_found")

    let wasEnabled = Config.shared.orchestratorEnabled
    Config.shared.orchestratorEnabled = false
    let disabled = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/schedules/\(id)/run", headers: auth))
    Config.shared.orchestratorEnabled = wasEnabled
    expect("manual run reports the disabled orchestrator", disabled.status, 403)
    expect("and keeps the typed disabled code", remoteErrorCode(disabled), "orchestrator_disabled")

    let calendar = Calendar.autoupdatingCurrent
    let timerNow = calendar.date(bySettingHour: 2, minute: 0, second: 0, of: Date())!
    let timerFire = calendar.date(bySettingHour: 1, minute: 30, second: 0, of: timerNow)!
    func scheduledTask(_ taskID: String, state: Orchestrator.State,
                       created: Date) -> Orchestrator.Task {
        var task = Orchestrator.Task(id: taskID, state: state, kind: "custom", title: "nightly",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: created,
            secretHash: Orchestrator.hash(ofSecret: "test"))
        task.scheduleID = id
        return task
    }

    var disabledSource = valid
    disabledSource["enabled"] = false
    try! JSONSerialization.data(withJSONObject: disabledSource)
        .write(to: directory.appendingPathComponent("\(id).json"))
    Orchestrator.forget()
    var disabledRuns = 0
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    Orchestrator.scheduleRunnerForTesting = { _ in disabledRuns += 1; return .ok(["ok": true]) }
    Orchestrator.scheduleBeat(now: timerNow)
    expect("a disabled schedule never reaches the dispatch queue", disabledRuns, 0)

    var impatientSource = valid
    impatientSource["catch_up_hours"] = 0
    try! JSONSerialization.data(withJSONObject: impatientSource)
        .write(to: directory.appendingPathComponent("\(id).json"))
    Orchestrator.forget()
    var missedRuns = 0
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    Orchestrator.scheduleRunnerForTesting = { _ in missedRuns += 1; return .ok(["ok": true]) }
    func missedAuditCount() -> Int {
        RemoteAuth.recentAudit(limit: 2_000).filter {
            $0["event"] as? String == "orchestrator.schedule.skipped"
                && $0["schedule"] as? String == id && $0["why"] as? String == "missed"
        }.count
    }
    let auditsBeforeMiss = missedAuditCount()
    let late = timerFire.addingTimeInterval(61)
    Orchestrator.scheduleBeat(now: late)
    Orchestrator.scheduleBeat(now: late)
    check("a missed occurrence is audited once and never dispatched",
          missedRuns == 0 && missedAuditCount() == auditsBeforeMiss + 1)
    let missedRecord = Orchestrator.scheduleRecords(now: late)
        .first { $0["id"] as? String == id }
    expect("the inventory records the missed occurrence as its own fact",
           missedRecord?["last_missed_at"] as? Int,
           Int(timerFire.timeIntervalSince1970))
    let missedResponse = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules", headers: auth))
    let missedBody = (try? JSONSerialization.jsonObject(with: missedResponse.body)) as? [String: Any]
    let missedRows = missedBody?["schedules"] as? [[String: Any]] ?? []
    expect("GET carries last_missed_at without inventing a run state",
           missedRows.first { $0["id"] as? String == id }?["last_missed_at"] as? Int,
           Int(timerFire.timeIntervalSince1970))

    try! data.write(to: directory.appendingPathComponent("\(id).json"))

    Orchestrator.forget()
    var queued: [() -> Void] = []
    var timerRuns = 0
    Orchestrator.scheduleDispatchEnqueuerForTesting = { queued.append($0) }
    Orchestrator.scheduleRunnerForTesting = { _ in
        timerRuns += 1
        return .ok(["ok": true])
    }
    Orchestrator.scheduleBeat(now: timerNow)
    check("the timer only decides off the server queue",
          timerRuns == 0 && queued.count == 1)
    let pendingManual = Orchestrator.runSchedule(id: id)
    expect("a queued timer fire blocks a racing manual run", { () -> Int? in
        guard case .refused(let status, _, _, _) = pendingManual else { return nil }
        return status
    }(), 409)
    queued.removeFirst()()
    expect("the dispatch transaction runs after entering the serial gate", timerRuns, 1)
    Orchestrator.scheduleBeat(now: timerNow)
    check("the same timer occurrence is handled once", queued.isEmpty && timerRuns == 1)

    Orchestrator.forget()
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    var retries = 0
    Orchestrator.scheduleRunnerForTesting = { _ in
        retries += 1
        return retries == 1
            ? .refused(status: 429, code: "over_capacity", message: "busy",
                       extra: ["retry_after": 60])
            : .ok(["ok": true])
    }
    Orchestrator.scheduleBeat(now: timerNow)
    check("over-capacity leaves the occurrence available for retry",
          retries == 1 && Orchestrator.handledScheduleFireForTesting(id) == nil)
    Orchestrator.scheduleBeat(now: timerNow)
    check("the next beat retries and then consumes the occurrence",
          retries == 2 && Orchestrator.handledScheduleFireForTesting(id) == timerFire)

    Orchestrator.forget()
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    var blockedRuns = 0
    Orchestrator.scheduleRunnerForTesting = { _ in blockedRuns += 1; return .ok(["ok": true]) }
    let active = scheduledTask("11111111-2222-4333-8444-555555555555", state: .briefed,
                               created: timerFire.addingTimeInterval(-120))
    let newerTerminal = scheduledTask("22222222-3333-4444-8555-666666666666", state: .success,
                                      created: timerFire.addingTimeInterval(-60))
    Orchestrator.holdScheduleTaskForTesting(active)
    Orchestrator.holdScheduleTaskForTesting(newerTerminal)
    Orchestrator.scheduleBeat(now: timerNow)
    check("any non-terminal task blocks overlap even when a newer task is terminal",
          blockedRuns == 0 && Orchestrator.handledScheduleFireForTesting(id) == timerFire)
    let conflict = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/schedules/\(id)/run", headers: auth))
    expect("manual run uses the same any-active rule", conflict.status, 409)
    expect("and returns the typed active envelope", remoteErrorCode(conflict), "schedule_active")
    let recordsResponse = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules", headers: auth))
    let recordsBody = (try? JSONSerialization.jsonObject(with: recordsResponse.body))
        as? [String: Any]
    let recordRows = recordsBody?["schedules"] as? [[String: Any]] ?? []
    let record = recordRows.first { $0["id"] as? String == id }
    let lastRun = record?["last_run"] as? [String: Any]
    check("GET reports the newest task and timestamp one field at a time",
          recordsBody?["at"] is Int
            && record?["title"] as? String == "nightly"
            && record?["enabled"] as? Bool == true
            && record?["next_fire"] is Int
            && lastRun?["task_id"] as? String == newerTerminal.id
            && lastRun?["state"] as? String == "success"
            && lastRun?["at"] is Int)

    Orchestrator.forget()
    Orchestrator.scheduleRunnerForTesting = { _ in .ok(["ok": true]) }
    _ = Orchestrator.runSchedule(id: id)
    check("a successful manual run records the current occurrence",
          Orchestrator.handledScheduleFireForTesting(id) != nil)
    Orchestrator.forget()
}

group("the settings schedule switch changes only enabled") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-settings-schedule-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let file = directory.appendingPathComponent("schedule.json")
    let original = """
    {
      "clawdline_schedule": 1,
      "schedule_id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
      "title": "publish",
      "when": { "at": "09:30", "days": ["mon", "fri"] },
      "task": { "assistant": "codex", "project_dir": "/tmp", "instructions": "publish",
                "claims": ["posts"], "enabled": "neighbor stays byte-for-byte" },
      "enabled": true,
      "catch_up_hours": 12
    }
    """
    try! Data(original.utf8).write(to: file)
    do {
        try ScheduleSettingsRow.setEnabled(false, at: file)
    } catch {
        check("the enabled edit succeeds", false, String(describing: error))
    }
    let changed = try! String(contentsOf: file, encoding: .utf8)
    let expected = original.replacingOccurrences(of: "\n  \"enabled\": true,",
                                                  with: "\n  \"enabled\": false,")
    expect("only the top-level enabled token changes", changed, expected)

    try! Data("{\"title\":\"missing enabled\"}".utf8).write(to: file)
    var missingEnabled = false
    do { try ScheduleSettingsRow.setEnabled(true, at: file) }
    catch let error as ScheduleSettingsRow.EditError {
        if case .missingEnabled = error { missingEnabled = true }
    }
    catch { }
    check("a missing enabled field has its typed soft failure", missingEnabled)

    try! Data("not json".utf8).write(to: file)
    check("a malformed source reports a soft write failure", {
        do { try ScheduleSettingsRow.setEnabled(true, at: file); return false }
        catch { return true }
    }())
}

group("schedule records become valid and warning rows for settings") {
    let records: [[String: Any]] = [
        ["id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "title": "Publish",
         "enabled": true, "next_fire": 2_000,
         "last_run": ["state": "success", "at": 1_000]],
        ["file": "broken.json", "state": "invalid", "error_kind": "schema",
         "error": "enabled must be a boolean"],
    ]
    let rows = ScheduleSettingsRow.rows(from: records)
    expect("both inventory shapes stay visible", rows.count, 2)
    check("the valid row carries every reading the control draws",
          rows[0].id == "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
            && rows[0].file == "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee.json"
            && rows[0].title == "Publish" && rows[0].enabled == true
            && rows[0].nextFire == Date(timeIntervalSince1970: 2_000)
            && rows[0].lastState == "success"
            && rows[0].lastAt == Date(timeIntervalSince1970: 1_000)
            && rows[0].error == nil)
    check("an invalid file becomes a non-toggleable warning row with its summary",
          rows[1].id == nil && rows[1].file == "broken.json"
            && rows[1].title == "broken.json" && rows[1].enabled == nil
            && rows[1].error == "enabled must be a boolean")
    expect("a valid-looking row without the orchestrator id is not writable",
           ScheduleSettingsRow.rows(from: [["title": "orphan", "enabled": true]]).count, 0)
    expect("all eight orchestrator truths share the explicit settings vocabulary",
           ["queued", "spawning", "briefed", "success", "failure", "timeout", "cancelled",
            "spawn_failed"].compactMap { ScheduleSettingsRow.presentation(for: $0) },
           [.running, .running, .running, .success, .failure, .timeout, .cancelled, .spawnFailed])
}

group("a schedule made over HTTP is one the parser above would have accepted") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-writes-\(UUID().uuidString)")
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Orchestrator.forget()
    }
    // Deliberately not created here: the first write has to make it, and it has to make it 0700.
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    Orchestrator.forget()

    let place = StartPoints.Place(id: StartPoints.id(for: "/tmp"), path: "/tmp",
                                  label: "tmp", at: Date(timeIntervalSince1970: 1))
    let good: [String: Any] = [
        "title": "publish the blog", "at": "09:30", "days": ["wed", "mon"],
        "place_id": place.id, "assistant": "codex",
        "instructions": "publish the next ready post", "enabled": true,
    ]
    func create(_ body: [String: Any]) -> Orchestrator.Reply {
        Orchestrator.createSchedule(from: body, places: [place], isDirectory: { $0 == "/tmp" })
    }
    func files() -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []).sorted()
    }
    func refusal(_ reply: Orchestrator.Reply) -> (status: Int, code: String, message: String)? {
        guard case .refused(let status, let code, let message, _) = reply else { return nil }
        return (status, code, message)
    }

    var madeID = ""
    switch create(good) {
    case .refused(_, _, let message, _):
        check("an ordinary request is written", false, message)
    case .ok(let payload):
        let schedule = payload["schedule"] as? [String: Any]
        madeID = schedule?["id"] as? String ?? ""
        check("the answer names the schedule it made",
              payload["ok"] as? Bool == true && UUID(uuidString: madeID) != nil
                && madeID == madeID.lowercased())
        expect("and carries the title back", schedule?["title"] as? String, "publish the blog")
        expect("and whether it is on", schedule?["enabled"] as? Bool, true)
        check("and when it will next fire", schedule?["next_fire"] is Int)
    }

    // The whole of the round-trip: what came out is a file the parser reads, which is the only
    // definition of a valid schedule this app has.
    expect("the file is named after the id it generated", files(), ["\(madeID).json"])
    let loaded = Orchestrator.schedules()
    expect("and it is loaded back off disk by the ordinary inventory", loaded.count, 1)
    expect("with the weekdays it was asked for", loaded.first?.when,
           Orchestrator.ScheduleWhen.weekly([2, 4]))
    expect("the hour survived the round trip", loaded.first?.hour, 9)
    expect("and the minute", loaded.first?.minute, 30)
    expect("the place id became the project directory, and never came from the request",
           loaded.first?.taskTemplate["project_dir"] as? String, "/tmp")
    check("the defaults are the parser's rather than written into the file twice", {
        guard let data = try? Data(contentsOf: directory
            .appendingPathComponent("\(madeID).json")),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return false }
        return obj["close_tab"] == nil && obj["catch_up_hours"] == nil
            && obj["notify_on_failure"] == nil
    }())

    // Task files are 0600 inside a 0700 directory; a schedule carries the same first message and
    // the same absolute path, so it is worth exactly as much to somebody else on this Mac.
    func mode(_ path: String) -> Int? {
        (try? FileManager.default.attributesOfItem(atPath: path))?[.posixPermissions] as? Int
    }
    expect("the directory it had to create is private", mode(directory.path), 0o700)
    expect("and the file in it is too",
           mode(directory.appendingPathComponent("\(madeID).json").path), 0o600)

    // Every one of these is a rejection the parser above already enumerates. A serialiser that
    // could produce one of them would be a route that writes files this app then reports as
    // invalid, audits, and pushes about — with nobody able to say which request left it there.
    for (name, mutate) in [
        ("empty title", { (value: inout [String: Any]) in value["title"] = "" }),
        ("oversized title", { (value: inout [String: Any]) in
            value["title"] = String(repeating: "x", count: 121) }),
        ("numeric title", { (value: inout [String: Any]) in value["title"] = 7 }),
        ("missing title", { (value: inout [String: Any]) in value.removeValue(forKey: "title") }),
        ("missing time", { (value: inout [String: Any]) in value.removeValue(forKey: "at") }),
        ("bad minute", { (value: inout [String: Any]) in value["at"] = "23:60" }),
        ("bad hour", { (value: inout [String: Any]) in value["at"] = "24:00" }),
        ("unpadded time", { (value: inout [String: Any]) in value["at"] = "9:30" }),
        ("missing days", { (value: inout [String: Any]) in value.removeValue(forKey: "days") }),
        ("empty days", { (value: inout [String: Any]) in value["days"] = [] }),
        ("unknown weekday", { (value: inout [String: Any]) in value["days"] = ["monday"] }),
        ("duplicate day", { (value: inout [String: Any]) in value["days"] = ["mon", "mon"] }),
        ("numeric enabled", { (value: inout [String: Any]) in value["enabled"] = 1 }),
        ("unknown close policy", { (value: inout [String: Any]) in value["close_tab"] = "later" }),
        ("boolean catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = true }),
        ("oversized catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = 169 }),
        ("negative catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = -1 }),
        ("numeric notification", { (value: inout [String: Any]) in
            value["notify_on_failure"] = 1 }),
        ("numeric model", { (value: inout [String: Any]) in value["model"] = 7 }),
        ("bad model name", { (value: inout [String: Any]) in value["model"] = "GPT 5!" }),
        ("bad assistant", { (value: inout [String: Any]) in value["assistant"] = "shell" }),
        ("missing assistant", { (value: inout [String: Any]) in
            value.removeValue(forKey: "assistant") }),
        ("missing instructions", { (value: inout [String: Any]) in
            value.removeValue(forKey: "instructions") }),
        ("empty instructions", { (value: inout [String: Any]) in value["instructions"] = "" }),
        ("zero timeout", { (value: inout [String: Any]) in value["timeout_minutes"] = 0 }),
        ("boolean timeout", { (value: inout [String: Any]) in value["timeout_minutes"] = true }),
        // The four the request cannot even name. These are the fields the parser owns, and a
        // route that let one through would be a route that writes its own schema version.
        ("a schema version of its own", { (value: inout [String: Any]) in
            value["clawdline_schedule"] = 2 }),
        ("an id of its own", { (value: inout [String: Any]) in
            value["schedule_id"] = UUID().uuidString.lowercased() }),
        ("a when object of its own", { (value: inout [String: Any]) in
            value["when"] = ["at": "23:45", "days": "daily", "zone": "UTC"] }),
        ("a task template of its own", { (value: inout [String: Any]) in
            value["task"] = ["assistant": "codex", "project_dir": "/etc",
                             "instructions": "print the hosts file"] }),
        // And the one this whole route is shaped around: a directory, by any name.
        ("a project directory", { (value: inout [String: Any]) in value["project_dir"] = "/etc" }),
        ("claims", { (value: inout [String: Any]) in value["claims"] = ["posts"] }),
        ("a permission mode", { (value: inout [String: Any]) in value["permission_mode"] = "full" }),
    ] {
        var broken = good
        mutate(&broken)
        let reply = refusal(create(broken))
        check("rejects \(name)", reply?.status == 400 && reply?.code == "bad_request",
              String(describing: reply))
        check("and leaves nothing behind for \(name)", files() == ["\(madeID).json"])
    }

    // The id is the whole of the argument that a device cannot name a directory it was never
    // shown, so it is checked before anything else in the body is read.
    for (name, body) in [
        ("no place at all", [:] as [String: Any]),
        ("an id nobody was handed", ["place_id": "0123456789abcdef"] as [String: Any]),
        ("an empty one", ["place_id": ""] as [String: Any]),
        ("a path where the id goes", ["place_id": "/tmp"] as [String: Any]),
        ("a number", ["place_id": 1] as [String: Any]),
    ] {
        var attempt = good
        attempt.removeValue(forKey: "place_id")
        for (key, value) in body { attempt[key] = value }
        let reply = refusal(create(attempt))
        check("\(name) is a bad request and never a guess",
              reply?.status == 400 && reply?.code == "bad_request"
                && reply?.message.contains("place_id") == true,
              String(describing: reply))
    }

    // Everything optional, and the answer still parses back — the shape a form sends when
    // somebody opened the extra fields and filled them in.
    var full = good
    full["days"] = "daily"
    full["close_tab"] = "always"
    full["catch_up_hours"] = 12
    full["notify_on_failure"] = false
    full["timeout_minutes"] = 45
    full["model"] = "gpt-5.6-sol"
    full["enabled"] = false
    if case .refused(_, _, let why, _) = create(full) {
        check("a request that sets every field is written too", false, why)
    }
    let both = Orchestrator.schedules()
    expect("both schedules are on disk", both.count, 2)
    let second = both.first { $0.id != madeID }
    check("daily is a schedule with no weekday filter",
          second != nil && second?.when == .daily)
    expect("the close policy came through", second?.closeTab, .always)
    expect("the catch-up window came through", second?.catchUpHours, 12)
    expect("the failure notification came through", second?.notifyOnFailure, false)
    expect("a disabled schedule can be made", second?.enabled, false)
    expect("the model came through", second?.taskTemplate["model"] as? String, "gpt-5.6-sol")
    expect("and so did the timeout",
           (second?.taskTemplate["timeout_minutes"] as? NSNumber)?.intValue, 45)

    // An empty model is a form saying "whatever that assistant runs by default", and a file
    // carrying `"model": ""` is a field whoever opens it later has to read and dismiss.
    var blankModel = good
    blankModel["model"] = ""
    if case .ok(let payload) = create(blankModel),
       let id = (payload["schedule"] as? [String: Any])?["id"] as? String {
        let written = Orchestrator.schedules().first { $0.id == id }
        check("an empty model is left out of the file rather than written down",
              written != nil && written?.taskTemplate["model"] == nil)
    } else {
        check("an empty model is left out of the file rather than written down", false)
    }

    // **The read-back, which is the whole reason this is a serialiser and not a formatter.** The
    // directory going away between the parse and the read is the honest way to produce a file
    // this app cannot load: everything about the request was valid when it was checked. What must
    // not happen is a 200 — that file would come back as an `invalid` row, audit itself, push
    // about itself, and give nobody a way to say which request left it there.
    var checked = false
    let before = files()
    let vanishing = Orchestrator.createSchedule(from: good, places: [place], isDirectory: { _ in
        defer { checked = true }
        return !checked
    })
    if case .refused(let status, let code, _, _) = vanishing {
        check("a file that cannot be read back is a write failure",
              status == 500 && code == "write_failed")
    } else {
        check("a file that cannot be read back is a write failure", false, "it answered 200")
    }
    check("and it does not survive the request that made it", files() == before)

    // The other half: nowhere to put the file at all.
    let blocked = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-blocked-\(UUID().uuidString)")
    try! Data("this is a file, not a directory".utf8).write(to: blocked)
    Orchestrator.scheduleDirectoryOverrideForTesting = blocked
    if case .refused(let status, let code, _, _) = create(good) {
        check("a directory that cannot be made is a write failure",
              status == 500 && code == "write_failed")
    } else {
        check("a directory that cannot be made is a write failure", false, "it answered 200")
    }
    try? FileManager.default.removeItem(at: blocked)
    Orchestrator.scheduleDirectoryOverrideForTesting = directory

    // The brake. Ten in ten minutes, and it exists for the client that retries in a loop with a
    // fresh key each time rather than for anybody filling in a form.
    //
    // The window is emptied first so the loop below counts what its name says. Five writes have
    // already gone through this group — including the two that ended in a `write_failed`, which
    // still spend a ticket — and the assertion used to be "the sixth attempt is refused", which
    // was only true if you counted those five. Anybody adding a create above would have turned it
    // red for a reason that has nothing to do with the brake.
    Orchestrator.forget()
    var refusedAt = 0
    for attempt in 1...12 {
        if case .refused(let status, let code, _, _) = create(good) {
            if refusedAt == 0 { refusedAt = attempt }
            // A sliding window of counted attempts, not a queue with something already in it:
            // `busy` in this app means depth that drains in seconds, and this does not.
            check("the brake refuses with the code the page knows",
                  status == 429 && code == "rate_limited")
        }
    }
    expect("ten writes get through a ten-minute window and the eleventh does not", refusedAt, 11)
    Orchestrator.forget()
}

group("the minute timer and the answer the write route gave agree with each other") {
    // The whole round trip of the fix, with its own control group in the same directory: two
    // "09:00 daily" schedules, one made through the route at one in the afternoon and one
    // hand-written with no made-at at all. A minute later the timer runs the hand-written one —
    // that is the behaviour a file without the field has always had and still has — and leaves
    // the new one alone, because nine this morning is not an occurrence it slept through.
    // Made here rather than left to the first write — the group above is the one that proves the
    // route creates it. Here it means a create that comes back refused shows up as a red check
    // rather than as a `try!` blowing the suite up on the hand-written file next to it.
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-fresh-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let wasEnabled = Config.shared.orchestratorEnabled
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Config.shared.orchestratorEnabled = wasEnabled
        Orchestrator.scheduleDispatchEnqueuerForTesting = nil
        Orchestrator.scheduleRunnerForTesting = nil
        Orchestrator.forget()
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    Config.shared.orchestratorEnabled = true
    Orchestrator.forget()

    let calendar = Calendar.autoupdatingCurrent
    // One in the afternoon, arranging tomorrow morning: the sentence the whole feature is for,
    // and the one the timer used to answer by opening a session on the spot.
    let madeAt = calendar.date(bySettingHour: 13, minute: 0, second: 0, of: Date())!
    let place = StartPoints.Place(id: StartPoints.id(for: "/tmp"), path: "/tmp",
                                  label: "tmp", at: Date(timeIntervalSince1970: 1))
    let reply = Orchestrator.createSchedule(
        from: ["title": "publish the blog", "at": "09:00", "days": "daily",
               "place_id": place.id, "assistant": "codex",
               "instructions": "publish the next ready post"],
        places: [place], now: madeAt, isDirectory: { $0 == "/tmp" })
    var madeID = ""
    var announcedNext = 0
    if case .ok(let payload) = reply, let row = payload["schedule"] as? [String: Any] {
        madeID = row["id"] as? String ?? ""
        announcedNext = row["next_fire"] as? Int ?? 0
    }
    check("the afternoon request is written", !madeID.isEmpty, String(describing: reply))
    let onDisk = (try? Data(contentsOf: directory.appendingPathComponent("\(madeID).json")))
        .flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] } ?? [:]
    expect("and the file says when it was made", onDisk["created_at"] as? Int,
           Int(madeAt.timeIntervalSince1970))

    let unstampedID = "eeeeeeee-ffff-4aaa-8bbb-cccccccccccc"
    let unstamped: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": unstampedID, "title": "the one in the editor",
        "when": ["at": "09:00", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": "/tmp", "instructions": "do the work"],
        "enabled": true,
    ]
    try! JSONSerialization.data(withJSONObject: unstamped)
        .write(to: directory.appendingPathComponent("\(unstampedID).json"))
    Orchestrator.forget()

    var ran: [String] = []
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    Orchestrator.scheduleRunnerForTesting = { schedule in
        ran.append(schedule.id)
        return .ok(["ok": true])
    }
    func missedAudits(_ id: String) -> Int {
        RemoteAuth.recentAudit(limit: 2_000).filter {
            $0["event"] as? String == "orchestrator.schedule.skipped"
                && $0["schedule"] as? String == id && $0["why"] as? String == "missed"
        }.count
    }
    let missedBefore = missedAudits(madeID)
    let aMinuteLater = madeAt.addingTimeInterval(60)
    Orchestrator.scheduleBeat(now: aMinuteLater)
    check("the timer runs the file that never said when it was made, and only that one",
          ran == [unstampedID], ran.joined(separator: ", "))
    // The other half of the bug, and the one a phone would actually have seen: no push, and no
    // "last missed 1 day ago" under a schedule made ten seconds ago.
    expect("nothing is audited as missed for a morning it was not there for",
           missedAudits(madeID), missedBefore)
    let row = Orchestrator.scheduleRecords(now: aMinuteLater).first { $0["id"] as? String == madeID }
    check("and the row carries no missed occurrence", row?["last_missed_at"] == nil)
    expect("while the next fire is still the one the 200 announced",
           row?["next_fire"] as? Int, announcedNext)

    // And it is not switched off, which is the failure this fix could have introduced instead.
    ran.removeAll()
    let firstMorning = Date(timeIntervalSince1970: TimeInterval(announcedNext))
    Orchestrator.scheduleBeat(now: firstMorning.addingTimeInterval(60))
    check("the first morning it was there for is dispatched like any other",
          ran.contains(madeID), ran.joined(separator: ", "))

    // The other half of the same bug. Made in the evening rather than the afternoon, this
    // morning's nine o'clock is outside the six-hour window, and the timer used to audit a
    // skipped/missed occurrence, push "Scheduled run missed its catch-up window", and leave
    // `last_missed_at` on a row somebody had made a minute earlier — "last missed 1 day ago".
    // The hand-written file in the same directory is the control: at this hour it *is* a missed
    // occurrence, it is audited as one, and it keeps its `last_missed_at`.
    let evening = calendar.date(bySettingHour: 20, minute: 0, second: 0, of: Date())!
    var eveningID = ""
    if case .ok(let payload) = Orchestrator.createSchedule(
        from: ["title": "read the overnight mail", "at": "09:00", "days": "daily",
               "place_id": place.id, "assistant": "codex",
               "instructions": "read what came in overnight"],
        places: [place], now: evening, isDirectory: { $0 == "/tmp" }) {
        eveningID = (payload["schedule"] as? [String: Any])?["id"] as? String ?? ""
    }
    check("an evening request is written too", !eveningID.isEmpty)
    Orchestrator.forget()
    ran.removeAll()
    Orchestrator.scheduleDispatchEnqueuerForTesting = { $0() }
    Orchestrator.scheduleRunnerForTesting = { schedule in
        ran.append(schedule.id)
        return .ok(["ok": true])
    }
    let eveningMissedBefore = missedAudits(eveningID)
    let eveningBeat = evening.addingTimeInterval(60)
    Orchestrator.scheduleBeat(now: eveningBeat)
    check("nothing is dispatched for a morning nobody arranged", ran.isEmpty,
          ran.joined(separator: ", "))
    expect("and nothing is audited as missed, so nothing is pushed about it",
           missedAudits(eveningID), eveningMissedBefore)
    let rows = Orchestrator.scheduleRecords(now: eveningBeat)
    check("the row of a schedule made a minute ago says nothing about a missed run",
          rows.first { $0["id"] as? String == eveningID }?["last_missed_at"] == nil)
    check("while the file that never said when it was made is missed, as it always was",
          rows.first { $0["id"] as? String == unstampedID }?["last_missed_at"] != nil)
}

group("one schedule can be read in full, and the write route is behind the write gate") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-routes-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let wasWriting = Config.shared.remoteWrite
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Config.shared.remoteWrite = wasWriting
        Orchestrator.forget()
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    let id = "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa"
    let source: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": id, "title": "nightly",
        "when": ["at": "01:30", "days": ["mon", "fri"]],
        "task": ["assistant": "codex", "project_dir": "/tmp",
                 "instructions": "do the nightly work", "claims": ["posts"]],
        "enabled": true, "close_tab": "always", "catch_up_hours": 12,
    ]
    try! JSONSerialization.data(withJSONObject: source)
        .write(to: directory.appendingPathComponent("\(id).json"))

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let one = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules/\(id)", headers: auth))
    expect("one schedule has a route of its own", one.status, 200)
    let oneBody = (try? JSONSerialization.jsonObject(with: one.body)) as? [String: Any]
    let record = oneBody?["schedule"] as? [String: Any]
    check("it carries what the list already said",
          record?["id"] as? String == id && record?["title"] as? String == "nightly"
            && record?["enabled"] as? Bool == true && record?["next_fire"] is Int)
    check("and the task template the list deliberately leaves out",
          (record?["task"] as? [String: Any])?["instructions"] as? String == "do the nightly work"
            && (record?["task"] as? [String: Any])?["project_dir"] as? String == "/tmp")
    check("the weekdays come back in the file's own spelling, not Calendar numbers",
          (record?["when"] as? [String: Any])?["days"] as? [String] == ["mon", "fri"]
            && (record?["when"] as? [String: Any])?["at"] as? String == "01:30")
    check("with the three policies a person set", record?["close_tab"] as? String == "always"
            && record?["catch_up_hours"] as? Int == 12
            && record?["notify_on_failure"] as? Bool == true)
    expect("an id nobody has is a 404", RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
                      headers: auth)).status, 404)
    expect("and so is something that is not an id at all", RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/schedules/..%2F..%2Fetc", headers: auth)).status,
        404)

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }
    expect("a read-only device may read one schedule, like it may read the list",
           RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/schedules/\(id)",
            headers: ["Authorization": "Bearer \(reader.token)"])).status, 200)

    let body = "{\"title\":\"x\",\"at\":\"09:00\",\"days\":\"daily\",\"place_id\":\"nope\","
        + "\"assistant\":\"claude\",\"instructions\":\"do a thing\"}"
    func post(_ token: String?, key: String?,
              header: [String: String] = [:]) -> RemoteServer.Response {
        var headers = header
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if let key { headers["Idempotency-Key"] = key }
        headers["Content-Type"] = "application/json"
        return RemoteServer.shared.route(remoteRequest("POST", "/v1/orchestrator/schedules",
                                                       headers: headers, body: body))
    }
    Config.shared.remoteWrite = true
    expect("making a schedule with no token at all is refused",
           post(nil, key: UUID().uuidString).status, 401)
    // The one that would quietly undo the argument in the plan: this route is for the phone, so
    // the local credential is not a way past the gate the phone has to pass.
    let orchestratorOnly = post(nil, key: UUID().uuidString, header: auth)
    expect("and the orchestrator token is not a way around the write gate",
           orchestratorOnly.status, 403)
    expect("it is refused as a device that may not send",
           remoteErrorCode(orchestratorOnly), "forbidden")
    expect("a device that may read and not send is refused",
           remoteErrorCode(post(reader.token, key: UUID().uuidString)), "forbidden")
    Config.shared.remoteWrite = false
    expect("the write switch is checked first",
           remoteErrorCode(post(writer.token, key: UUID().uuidString)), "write_disabled")
    Config.shared.remoteWrite = true
    // Checked by its sentence and not only by its status: this route has a `400` of its own for a
    // body it cannot use, so a check that only counted to 400 would stay green with the gate gone.
    let noKey = post(writer.token, key: nil)
    expect("a retryable write with no idempotency key is refused", noKey.status, 400)
    check("and it is refused for the header rather than for the body",
          remoteErrorMessage(noKey).contains("Idempotency-Key"), remoteErrorMessage(noKey))
    let unknownPlace = post(writer.token, key: UUID().uuidString)
    expect("past all three gates, an id nobody was handed is a bad request",
           unknownPlace.status, 400)
    expect("and it is the typed one", remoteErrorCode(unknownPlace), "bad_request")
    expect("nothing was written", (try? FileManager.default
        .contentsOfDirectory(atPath: directory.path))?.count, 1)
    // The create route now says whether anything will run what it made. A refusal made nothing,
    // so it says nothing about it — a client reading that field off a `400` would be reading it
    // off a schedule that does not exist.
    // The whole body rather than one key of it: the create's extra fields sit beside `schedule`
    // and a refusal's sit inside `error`, so a check that looked in one place would stay green
    // while the field arrived in the other.
    let refusedBody = String(data: unknownPlace.body, encoding: .utf8) ?? ""
    check("and a refused create says nothing about whether dispatch is on",
          !refusedBody.contains("dispatch_enabled"), refusedBody)
    Orchestrator.forget()
}

group("a schedule that was written says whether anything on this Mac will run it") {
    // **Making one is deliberately not gated on the dispatch switch.** Writing a file is not
    // dispatching, and a create that refused would be this route deciding what somebody may
    // arrange for later. What was missing is the other half: with the switch off, the answer
    // said `Created.` and the minute timer then returned before it looked at any schedule at
    // all — no session, and no sentence anywhere saying why.
    func made(_ dispatching: Bool) -> [String: Any] {
        let reply = RemoteServer.scheduleAnswer(
            .ok(["ok": true,
                 "schedule": ["id": "4d2f54ce-77e2-4a1b-9f30-5c2d81aa6b04",
                              "title": "publish the blog", "enabled": true]]),
            dispatchEnabled: dispatching)
        guard case .ok(let payload) = reply else { return [:] }
        return payload
    }
    expect("with dispatch on, the answer says so", made(true)["dispatch_enabled"] as? Bool, true)
    expect("with it off, the answer says that instead",
           made(false)["dispatch_enabled"] as? Bool, false)
    expect("the schedule is made either way", made(false)["ok"] as? Bool, true)
    expect("and comes back whole beside the flag",
           (made(false)["schedule"] as? [String: Any])?["title"] as? String, "publish the blog")

    let refused = RemoteServer.scheduleAnswer(
        .refused(400, "bad_request", "days must be daily or a list of weekdays"),
        dispatchEnabled: false)
    guard case .refused(let status, let code, _, let extra) = refused else {
        check("a refusal is still a refusal", false)
        return
    }
    expect("a refusal keeps its status", status, 400)
    expect("and its code", code, "bad_request")
    check("and is handed back with nothing added to it", extra.isEmpty)
}

group("a schedule can be changed and taken away, and an edit is not a way past the parser") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-edits-\(UUID().uuidString)")
    let wasEnabled = Config.shared.orchestratorEnabled
    defer {
        // Put the mode back first: one check below takes the directory's write bit away to make
        // a removal fail, and a 0500 directory is one nothing can clean up afterwards.
        try? FileManager.default.setAttributes([.posixPermissions: 0o700],
                                               ofItemAtPath: directory.path)
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Orchestrator.scheduleDispatchEnqueuerForTesting = nil
        Orchestrator.scheduleRunnerForTesting = nil
        Config.shared.orchestratorEnabled = wasEnabled
        Orchestrator.forget()
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    Orchestrator.forget()

    let place = StartPoints.Place(id: StartPoints.id(for: "/tmp"), path: "/tmp",
                                  label: "tmp", at: Date(timeIntervalSince1970: 1))
    let elsewhere = StartPoints.Place(id: StartPoints.id(for: "/usr"), path: "/usr",
                                      label: "usr", at: Date(timeIntervalSince1970: 1))
    let known: (String) -> Bool = { $0 == "/tmp" || $0 == "/usr" }
    let madeAt = Date(timeIntervalSince1970: 1_787_000_000)
    let made: [String: Any] = [
        "title": "publish the blog", "at": "09:30", "days": ["wed", "mon"],
        "place_id": place.id, "assistant": "codex",
        "instructions": "publish the next ready post", "enabled": true,
    ]
    func create(_ body: [String: Any], at when: Date = madeAt) -> String {
        guard case .ok(let payload) = Orchestrator.createSchedule(
            from: body, places: [place, elsewhere], now: when, isDirectory: known),
              let id = (payload["schedule"] as? [String: Any])?["id"] as? String else { return "" }
        return id
    }
    // **An hour after the file was made, and that hour is the whole test.** With both instants
    // equal, a route that restamped `created_at` on every save would write the same number back
    // and the check below would pass while the bug was there — it was born green exactly that
    // way, and only the deliberate break found it.
    let savedAt = madeAt.addingTimeInterval(3_600)
    func update(_ id: String, _ body: [String: Any]) -> Orchestrator.Reply {
        Orchestrator.updateSchedule(id: id, from: body, places: [place, elsewhere],
                                    now: savedAt, isDirectory: known)
    }
    func bytes(_ id: String) -> Data? {
        try? Data(contentsOf: directory.appendingPathComponent("\(id).json"))
    }
    func source(_ id: String) -> [String: Any] {
        bytes(id).flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] } ?? [:]
    }
    func refusal(_ reply: Orchestrator.Reply) -> (status: Int, code: String, message: String)? {
        guard case .refused(let status, let code, let message, _) = reply else { return nil }
        return (status, code, message)
    }

    let id = create(made)
    check("a schedule to change is made first", !id.isEmpty)
    expect("and it carries the instant it was made", source(id)["created_at"] as? Int,
           Int(madeAt.timeIntervalSince1970))
    // Nothing has been retimed, so there is nothing to say about it. A create writing this field
    // would be answering a question nobody asked and putting a second stamp in every file.
    check("and says nothing about having been retimed", source(id)["when_changed_at"] == nil)

    var changed = made
    changed["title"] = "publish the newsletter"
    changed["at"] = "07:05"
    changed["days"] = "daily"
    changed["place_id"] = elsewhere.id
    changed["close_tab"] = "always"
    changed["enabled"] = false
    switch update(id, changed) {
    case .refused(_, _, let why, _): check("an ordinary edit is written", false, why)
    case .ok(let payload):
        let row = payload["schedule"] as? [String: Any]
        expect("and the answer names the schedule it changed", row?["id"] as? String, id)
        expect("with the new title", row?["title"] as? String, "publish the newsletter")
        expect("and says it is now off", row?["enabled"] as? Bool, false)
    }
    // The two fields the request may not name, read off the bytes rather than off the answer.
    // `created_at` is the one that matters: a save that restamped it would make a schedule
    // arranged at lunchtime run for this morning, which is the bug that field exists to stop.
    expect("the id survives an edit", source(id)["schedule_id"] as? String, id)
    expect("and so does the instant it was made",
           source(id)["created_at"] as? Int, Int(madeAt.timeIntervalSince1970))
    // And the field the request may not name either, written *because* the time moved. Without
    // it the next minute's beat opens a session for 07:05 this morning, an occurrence that did
    // not exist until this save, while the answer above says the next run is tomorrow.
    expect("a save that moved the time records when it moved",
           source(id)["when_changed_at"] as? Int, Int(savedAt.timeIntervalSince1970))
    let reloaded = Orchestrator.schedules().first { $0.id == id }
    check("the rewritten file is one the ordinary inventory reads back", reloaded != nil)
    expect("the new hour is on disk", reloaded?.hour, 7)
    expect("and the new minute", reloaded?.minute, 5)
    check("daily replaced the two weekdays", reloaded?.when == .daily)
    expect("the new place became the project directory, and never came from the request",
           reloaded?.taskTemplate["project_dir"] as? String, "/usr")
    expect("and the close policy came through", reloaded?.closeTab, .always)
    expect("a saved file is still private", (try? FileManager.default.attributesOfItem(
            atPath: directory.appendingPathComponent("\(id).json").path))?[.posixPermissions]
            as? Int, 0o600)

    // A second save an hour later that leaves the firing times exactly where they are. The stamp
    // must not move: a schedule that really was missed at nine is still missed after somebody
    // fixes its title at eleven, and a save that restamped on every write would swallow that run
    // — which is the one way this fix could be worse than the bug it closes.
    var renamed = changed
    renamed["title"] = "publish the newsletter, later"
    let renamedAt = savedAt.addingTimeInterval(3_600)
    if case .refused(_, _, let why, _) = Orchestrator.updateSchedule(
        id: id, from: renamed, places: [place, elsewhere], now: renamedAt, isDirectory: known) {
        check("a save that changes only the title is written", false, why)
    }
    expect("the title change landed", source(id)["title"] as? String,
           "publish the newsletter, later")
    expect("and a save that did not move the time did not move the stamp",
           source(id)["when_changed_at"] as? Int, Int(savedAt.timeIntervalSince1970))
    // Which days it runs on is as much a part of when it fires as the hour is, and the comparison
    // is on the parsed schedule rather than on the body, so `"daily"` against `["mon","tue",…]`
    // is answered by what those two mean rather than by how they are spelled.
    var fewerDays = renamed
    fewerDays["days"] = ["mon", "thu"]
    let daysChangedAt = renamedAt.addingTimeInterval(3_600)
    if case .refused(_, _, let why, _) = Orchestrator.updateSchedule(
        id: id, from: fewerDays, places: [place, elsewhere], now: daysChangedAt,
        isDirectory: known) {
        check("a save that changes only the days is written", false, why)
    }
    expect("dropping to two weekdays is a retiming too",
           source(id)["when_changed_at"] as? Int, Int(daysChangedAt.timeIntervalSince1970))

    // Every rejection the create route is held to, aimed at an edit. Letting one through here
    // would make saving a schedule the way to write a file that creating one refuses to write.
    let untouched = bytes(id)
    for (name, mutate) in [
        ("empty title", { (value: inout [String: Any]) in value["title"] = "" }),
        ("oversized title", { (value: inout [String: Any]) in
            value["title"] = String(repeating: "x", count: 121) }),
        ("numeric title", { (value: inout [String: Any]) in value["title"] = 7 }),
        ("missing title", { (value: inout [String: Any]) in value.removeValue(forKey: "title") }),
        ("missing time", { (value: inout [String: Any]) in value.removeValue(forKey: "at") }),
        ("bad minute", { (value: inout [String: Any]) in value["at"] = "23:60" }),
        ("bad hour", { (value: inout [String: Any]) in value["at"] = "24:00" }),
        ("unpadded time", { (value: inout [String: Any]) in value["at"] = "9:30" }),
        ("missing days", { (value: inout [String: Any]) in value.removeValue(forKey: "days") }),
        ("empty days", { (value: inout [String: Any]) in value["days"] = [] }),
        ("unknown weekday", { (value: inout [String: Any]) in value["days"] = ["monday"] }),
        ("duplicate day", { (value: inout [String: Any]) in value["days"] = ["mon", "mon"] }),
        ("numeric enabled", { (value: inout [String: Any]) in value["enabled"] = 1 }),
        ("unknown close policy", { (value: inout [String: Any]) in value["close_tab"] = "later" }),
        ("boolean catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = true }),
        ("oversized catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = 169 }),
        ("negative catch-up", { (value: inout [String: Any]) in value["catch_up_hours"] = -1 }),
        ("numeric notification", { (value: inout [String: Any]) in
            value["notify_on_failure"] = 1 }),
        ("numeric model", { (value: inout [String: Any]) in value["model"] = 7 }),
        ("bad model name", { (value: inout [String: Any]) in value["model"] = "GPT 5!" }),
        ("bad assistant", { (value: inout [String: Any]) in value["assistant"] = "shell" }),
        ("missing assistant", { (value: inout [String: Any]) in
            value.removeValue(forKey: "assistant") }),
        ("missing instructions", { (value: inout [String: Any]) in
            value.removeValue(forKey: "instructions") }),
        ("empty instructions", { (value: inout [String: Any]) in value["instructions"] = "" }),
        ("zero timeout", { (value: inout [String: Any]) in value["timeout_minutes"] = 0 }),
        ("boolean timeout", { (value: inout [String: Any]) in value["timeout_minutes"] = true }),
        ("a schema version of its own", { (value: inout [String: Any]) in
            value["clawdline_schedule"] = 2 }),
        ("a when object of its own", { (value: inout [String: Any]) in
            value["when"] = ["at": "23:45", "days": "daily", "zone": "UTC"] }),
        ("a task template of its own", { (value: inout [String: Any]) in
            value["task"] = ["assistant": "codex", "project_dir": "/etc",
                             "instructions": "print the hosts file"] }),
        ("a project directory", { (value: inout [String: Any]) in value["project_dir"] = "/etc" }),
        ("claims", { (value: inout [String: Any]) in value["claims"] = ["posts"] }),
        ("a permission mode", { (value: inout [String: Any]) in value["permission_mode"] = "full" }),
        // The two an edit adds to that list, and the reason this route takes an `id` in its path
        // rather than in its body: a save that could name either of them would be a save that
        // renames somebody else's schedule, or asks for the pre-`created_at` behaviour on purpose.
        ("an id of its own", { (value: inout [String: Any]) in
            value["schedule_id"] = UUID().uuidString.lowercased() }),
        ("a made-at of its own", { (value: inout [String: Any]) in value["created_at"] = 0 }),
        ("a retiming stamp of its own", { (value: inout [String: Any]) in
            value["when_changed_at"] = 0 }),
    ] {
        var broken = changed
        mutate(&broken)
        let reply = refusal(update(id, broken))
        check("an edit rejects \(name)", reply?.status == 400 && reply?.code == "bad_request",
              String(describing: reply))
        check("and leaves the schedule exactly as it was after \(name)", bytes(id) == untouched)
    }

    expect("an id nobody has is a not-found rather than a new file",
           refusal(update("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", changed))?.code, "not_found")
    expect("and so is something that is not an id at all",
           refusal(update("../../etc/passwd", changed))?.code, "not_found")

    // A file somebody typed in an editor has no `created_at` and means "as far back as anyone
    // knows". An edit must not quietly give it one: that would change what the beat decides
    // about every occurrence before today, on a file whose owner only renamed it.
    let plainID = "eeeeeeee-1111-4222-8333-444444444444"
    let plain: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": plainID, "title": "the one in the editor",
        "when": ["at": "09:00", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": "/tmp", "instructions": "do the work"],
        "enabled": true,
    ]
    try! JSONSerialization.data(withJSONObject: plain)
        .write(to: directory.appendingPathComponent("\(plainID).json"))
    var plainEdit = made
    plainEdit["title"] = "renamed in the app"
    if case .refused(_, _, let why, _) = update(plainID, plainEdit) {
        check("a hand-written schedule can be edited too", false, why)
    }
    check("and an edit does not stamp a made-at onto a file that never had one",
          source(plainID)["created_at"] == nil)
    // The other stamp goes the other way, and the difference is what each of them claims.
    // `created_at` would be a guess about a past this app was not there for; this save really did
    // move 09:00 daily to 09:30 on two weekdays, and it is the one thing standing between that
    // move and a session opening for a 09:30 that did not exist a second ago.
    expect("but retiming a hand-written file does write when it was retimed",
           source(plainID)["when_changed_at"] as? Int, Int(savedAt.timeIntervalSince1970))
    expect("while the change itself landed", source(plainID)["title"] as? String,
           "renamed in the app")

    // The same shape as "one bad file does not hide its valid neighbor": a removal is about one
    // file, and the proof is that the one beside it is still there byte for byte.
    let neighbour = bytes(plainID)
    if case .refused(let status, let code, _, _) = Orchestrator.deleteSchedule(id: id) {
        check("an ordinary removal succeeds", false, "\(status) \(code)")
    }
    check("the file it named is gone", bytes(id) == nil)
    check("and the schedule beside it is untouched", bytes(plainID) == neighbour)
    expect("only the neighbour is left in the inventory",
           Orchestrator.schedules().map { $0.id }, [plainID])
    // "There was no such schedule" and "the file would not go" are different answers, and a
    // caller that cannot tell them apart is told a schedule is gone while it is still firing.
    expect("removing it again is a not-found",
           refusal(Orchestrator.deleteSchedule(id: id))?.code, "not_found")
    expect("and so is removing one nobody ever made", refusal(
        Orchestrator.deleteSchedule(id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))?.code,
        "not_found")
    expect("and something that is not an id at all",
           refusal(Orchestrator.deleteSchedule(id: "../../etc/passwd"))?.code, "not_found")
    try! FileManager.default.setAttributes([.posixPermissions: 0o500],
                                           ofItemAtPath: directory.path)
    let stuck = refusal(Orchestrator.deleteSchedule(id: plainID))
    check("a file that will not go is not reported as one that was never there",
          stuck?.status == 500 && stuck?.code == "delete_failed", String(describing: stuck))
    check("and it is still on disk", bytes(plainID) != nil)
    try! FileManager.default.setAttributes([.posixPermissions: 0o700],
                                           ofItemAtPath: directory.path)

    // The read-back, aimed at an edit rather than at a create. The directory going away between
    // the parse and the read is the honest way to produce a file this app cannot load, and what
    // must not happen here is worse than a stray file: the schedule somebody already had is not
    // a failed save's to lose.
    let survivor = bytes(plainID)
    var vanished = false
    let rolledBack = Orchestrator.updateSchedule(
        id: plainID, from: plainEdit, places: [place, elsewhere], now: savedAt,
        isDirectory: { _ in defer { vanished = true }; return !vanished })
    if case .refused(let status, let code, _, _) = rolledBack {
        check("an edit that cannot be read back is a write failure",
              status == 500 && code == "write_failed")
    } else {
        check("an edit that cannot be read back is a write failure", false, "it answered 200")
    }
    check("and the schedule that was already there is put back", bytes(plainID) == survivor)

    // The brake counts an edit, because a client retrying a save in a loop writes a file per
    // attempt exactly as a client retrying a create does.
    Orchestrator.forget()
    let braked = create(made)
    check("there is something to save repeatedly", !braked.isEmpty)
    var refusedAt = 0
    for attempt in 1...12 {
        if case .refused(let status, let code, _, _) = update(braked, changed) {
            if refusedAt == 0 { refusedAt = attempt }
            check("a braked edit is refused with the code the page already knows",
                  status == 429 && code == "rate_limited")
        }
    }
    expect("the create spent one ticket of the ten and nine edits spent the rest", refusedAt, 10)

    // What a task that is running right now does and does not stop. A manual run refuses with
    // `409 schedule_active` because a second session from one schedule would stack on the first.
    // An edit changes a file nothing in flight reads again — a task is materialised from the
    // template when it is dispatched — and a removal is what somebody reaches for precisely
    // when work is running and should not be.
    Orchestrator.forget()
    let busy = create(made)
    var running = Orchestrator.Task(id: "33333333-4444-4555-8666-777777777777", state: .briefed,
        kind: "custom", title: "nightly", assistant: .codex, projectDir: "/tmp",
        timeoutMinutes: 30, created: madeAt, secretHash: Orchestrator.hash(ofSecret: "test"))
    running.scheduleID = busy
    Orchestrator.holdScheduleTaskForTesting(running)
    Config.shared.orchestratorEnabled = true
    expect("a manual run still refuses while a task from it is live",
           refusal(Orchestrator.runSchedule(id: busy))?.code, "schedule_active")
    var whileBusy = made
    whileBusy["title"] = "changed while it runs"
    if case .refused(_, _, let why, _) = update(busy, whileBusy) {
        check("an edit is not refused for the same reason", false, why)
    }
    expect("and the change is on disk", source(busy)["title"] as? String, "changed while it runs")
    if case .refused(let status, let code, _, _) = Orchestrator.deleteSchedule(id: busy) {
        check("nor is a removal", false, "\(status) \(code)")
    }
    check("the file is gone while its task keeps running", bytes(busy) == nil)
    expect("and removing the schedule is not cancelling the task it already dispatched",
           Orchestrator.record(id: running.id)?["state"] as? String, "briefed")

    // A removal that lands after the beat has already chosen an occurrence. The dispatch is
    // waiting on the serial queue holding a copy of the schedule, so without a second look it
    // opens a session out of a file that is no longer there — which is the one way pressing
    // delete is not enough.
    Orchestrator.forget()
    for file in (try? FileManager.default.contentsOfDirectory(
        at: directory, includingPropertiesForKeys: nil)) ?? [] {
        try? FileManager.default.removeItem(at: file)
    }
    let calendar = Calendar.autoupdatingCurrent
    let beatNow = calendar.date(bySettingHour: 2, minute: 0, second: 0, of: Date())!
    let doomed = create(["title": "nightly", "at": "01:30", "days": "daily",
                         "place_id": place.id, "assistant": "codex",
                         "instructions": "do the nightly work"],
                        at: beatNow.addingTimeInterval(-86_400))
    check("a schedule the timer will pick up is made", !doomed.isEmpty)
    var queuedWork: [() -> Void] = []
    var opened: [String] = []
    Orchestrator.scheduleDispatchEnqueuerForTesting = { queuedWork.append($0) }
    Orchestrator.scheduleRunnerForTesting = { opened.append($0.id); return .ok(["ok": true]) }
    Orchestrator.scheduleBeat(now: beatNow)
    check("the beat queued that occurrence and opened nothing itself",
          queuedWork.count == 1 && opened.isEmpty)
    _ = Orchestrator.deleteSchedule(id: doomed)
    queuedWork.removeFirst()()
    check("a fire already decided opens nothing for a schedule that has been removed",
          opened.isEmpty, opened.joined(separator: ", "))
    Orchestrator.forget()
}

group("changing and removing a schedule pass the same three gates as making one") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-edit-routes-\(UUID().uuidString)")
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let wasWriting = Config.shared.remoteWrite
    defer {
        try? FileManager.default.removeItem(at: directory)
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Config.shared.remoteWrite = wasWriting
        Orchestrator.forget()
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory
    let id = "cccccccc-dddd-4eee-8fff-bbbbbbbbbbbb"
    let source: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": id, "title": "nightly",
        "when": ["at": "01:30", "days": ["mon", "fri"]],
        "task": ["assistant": "codex", "project_dir": "/tmp",
                 "instructions": "do the nightly work"],
        "enabled": true,
    ]
    func restore() {
        try! JSONSerialization.data(withJSONObject: source)
            .write(to: directory.appendingPathComponent("\(id).json"))
    }
    restore()

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }
    let body = "{\"title\":\"x\",\"at\":\"09:00\",\"days\":\"daily\",\"place_id\":\"nope\","
        + "\"assistant\":\"claude\",\"instructions\":\"do a thing\"}"
    func call(_ method: String, _ token: String?, key: String?, target: String = id,
              header: [String: String] = [:]) -> RemoteServer.Response {
        var headers = header
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if let key { headers["Idempotency-Key"] = key }
        headers["Content-Type"] = "application/json"
        return RemoteServer.shared.route(remoteRequest(
            method, "/v1/orchestrator/schedules/\(target)", headers: headers,
            body: method == "PATCH" ? body : nil))
    }
    func exists() -> Bool {
        FileManager.default.fileExists(
            atPath: directory.appendingPathComponent("\(id).json").path)
    }

    Config.shared.remoteWrite = true
    for method in ["PATCH", "DELETE"] {
        expect("\(method) with no token at all is refused",
               call(method, nil, key: UUID().uuidString).status, 401)
        // The one that would quietly undo the argument these routes are built on: they are for
        // the phone, so the local credential is not a way past the gate the phone has to pass.
        let orchestratorOnly = call(method, nil, key: UUID().uuidString, header: auth)
        expect("\(method) with the orchestrator token is not a way around the write gate",
               orchestratorOnly.status, 403)
        expect("and it is refused as a device that may not send",
               remoteErrorCode(orchestratorOnly), "forbidden")
        expect("\(method) from a device that may read and not send is refused",
               remoteErrorCode(call(method, reader.token, key: UUID().uuidString)), "forbidden")
        Config.shared.remoteWrite = false
        expect("\(method) checks the write switch first",
               remoteErrorCode(call(method, writer.token, key: UUID().uuidString)),
               "write_disabled")
        Config.shared.remoteWrite = true
        // Checked by its sentence and not only by its status: both routes have `400`s of their
        // own, so a check that counted to 400 would stay green with the gate gone.
        let noKey = call(method, writer.token, key: nil)
        expect("\(method) with no idempotency key is refused", noKey.status, 400)
        check("and it is refused for the header rather than for the body",
              remoteErrorMessage(noKey).contains("Idempotency-Key"), remoteErrorMessage(noKey))
        check("nothing reached the file through any of those", exists())
    }

    // Past all three gates. PATCH still answers to the parser, and a `place_id` nobody was
    // handed is the same bad request it is on the route that makes one.
    let unknownPlace = call("PATCH", writer.token, key: UUID().uuidString)
    expect("past the gates, an id nobody was handed is a bad request", unknownPlace.status, 400)
    expect("and it is the typed one", remoteErrorCode(unknownPlace), "bad_request")
    expect("PATCH on an id nobody has is a not-found",
           call("PATCH", writer.token, key: UUID().uuidString,
                target: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa").status, 404)
    check("and the schedule is still there", exists())

    let removalKey = UUID().uuidString
    let removed = call("DELETE", writer.token, key: removalKey)
    expect("DELETE past the gates answers 200", removed.status, 200)
    expect("and names what it removed", ((try? JSONSerialization.jsonObject(with: removed.body))
            as? [String: Any])?["deleted"] as? String, id)
    check("the file is gone", !exists())
    // The whole point of requiring a key: a phone that changed networks mid-request retries,
    // and the retry must be the same answer rather than a second 404.
    expect("a retry under the same key replays that answer rather than looking again",
           call("DELETE", writer.token, key: removalKey).status, 200)
    expect("while a fresh key is told there is nothing there",
           call("DELETE", writer.token, key: UUID().uuidString).status, 404)
    Orchestrator.forget()
}
}
