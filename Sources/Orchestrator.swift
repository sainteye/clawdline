import AppKit
import CryptoKit
import Foundation
import Security

/// Root sessions dispatching child sessions — the broker side. See docs/orchestrator.md.
///
/// A *root* session (somebody's Claude Code, holding the `/clawdline` skill) writes a task under
/// `/tmp/.clawdline/<id>/` and asks this app to run it. The app opens a terminal session for the
/// named assistant, types the briefing in, watches for the child's `result.json`, accounts what
/// the child spent, and tells the root. The app is the only party that talks to everybody, so the
/// caps live here: how many children may run at once, who may dispatch at all, and how a child
/// proves it is the child.
///
/// **Two credentials, deliberately not one.** Dispatching is gated by a token in a `0600` file —
/// the same boundary `remote-token` uses, and for the same reason: through a tunnel every request
/// arrives from 127.0.0.1, so "local" is a thing only the filesystem can prove, and a paired
/// phone must never be able to start sessions. Nothing this app opens may dispatch, so no child
/// is handed that token at all, and one that found it could still not exchange it for another
/// task's secret. Every child gets its own per-task secret, typed into its first message and good
/// for finishing its own task or sending one of its tightly limited timely notifications. Only
/// the secret's SHA-256 is kept once the child has been briefed.
enum Orchestrator {

    // MARK: - Scheduled dispatches

    enum ScheduleCloseTab: String {
        case onSuccess = "on_success", always, never
    }

    struct Schedule {
        let id: String
        let title: String
        let hour: Int
        let minute: Int
        /// Calendar weekday numbers (1 = Sunday ... 7 = Saturday), or nil for every day.
        let weekdays: Set<Int>?
        let taskTemplate: [String: Any]
        let enabled: Bool
        let closeTab: ScheduleCloseTab
        let catchUpHours: Int
        let notifyOnFailure: Bool
        /// When the schedule itself was made, or nil for a file that never said.
        ///
        /// Without it nothing can tell "the Mac was asleep and missed this" from "this did not
        /// exist yet", and a schedule made at lunchtime for nine in the morning either dispatches
        /// a minute later or pushes that it missed a morning it was not there for. Nil is what
        /// every hand-written file has always meant: as far back as anyone knows.
        let createdAt: Date?
        /// When `when` last changed, or nil for a file whose firing times have never been edited.
        ///
        /// ``createdAt`` answers "did this schedule exist yet"; this answers the question an edit
        /// asks instead, which is "did this *occurrence* exist yet". Moving a `21:00` schedule to
        /// `09:00` at two in the afternoon invents an occurrence six hours old, and without a
        /// second stamp the timer cannot tell it from a morning the Mac slept through: measured,
        /// it dispatched within the minute while the save's own answer said tomorrow.
        ///
        /// Only a save that really changes the hour, the minute or the days moves it. A save that
        /// changes a title must not, or fixing a typo at eleven would swallow the nine o'clock run
        /// that was genuinely missed at nine.
        let whenChangedAt: Date?
    }

    enum ScheduleDraftOutcome {
        case ok(Schedule)
        case bad(String)
    }

    private struct InvalidSchedule {
        let file: String
        let error: String
        let kind: String
        let fingerprint: String
        let title: String
        let projectDir: String?
        let notifyOnFailure: Bool

        var record: [String: Any] {
            ["file": file, "state": "invalid", "error": error, "error_kind": kind]
        }
    }

    private struct ScheduleInventory {
        var valid: [Schedule]
        var invalid: [InvalidSchedule]
    }

    enum ScheduleAction: Equatable {
        /// `beforeCreation` is "the schedule did not exist yet"; `beforeRetiming` is "this
        /// occurrence did not exist yet". They are one line apart in the timer and identical in
        /// what they do — nothing — but they are different sentences about why, and a reader of an
        /// audit or a test is owed the right one.
        case run, alreadyHandled, active, missed, beforeCreation, beforeRetiming
    }

    private static func scheduleBool(_ raw: Any?) -> Bool? {
        guard let raw else { return nil }
        if let number = raw as? NSNumber {
            guard CFGetTypeID(number) == CFBooleanGetTypeID() else { return nil }
            return number.boolValue
        }
        return raw as? Bool
    }

    private static func scheduleInt(_ raw: Any?) -> Int? {
        guard let raw else { return nil }
        if let number = raw as? NSNumber {
            guard CFGetTypeID(number) != CFBooleanGetTypeID(),
                  !["f", "d"].contains(String(cString: number.objCType)) else { return nil }
            return Int(exactly: number.int64Value)
        }
        return raw as? Int
    }

    static func schedule(from obj: [String: Any], filename: String,
                         isDirectory: (String) -> Bool = StartPoints.isDirectory)
        -> ScheduleDraftOutcome {
        let allowed = Set(["clawdline_schedule", "schedule_id", "title", "when", "task",
                           "enabled", "close_tab", "catch_up_hours", "notify_on_failure",
                           "created_at", "when_changed_at"])
        let unknown = Set(obj.keys).subtracting(allowed).sorted()
        guard unknown.isEmpty else { return .bad("unknown field: \(unknown.joined(separator: ", "))") }
        guard scheduleInt(obj["clawdline_schedule"]) == 1 else {
            return .bad("clawdline_schedule must be 1")
        }
        guard let id = obj["schedule_id"] as? String, UUID(uuidString: id) != nil,
              id == id.lowercased(), filename == "\(id).json" else {
            return .bad("schedule_id must be a lowercase UUID matching the filename")
        }
        guard let title = obj["title"] as? String, !title.isEmpty, title.count <= 120 else {
            return .bad("title must be a non-empty string of at most 120 characters")
        }
        guard let when = obj["when"] as? [String: Any],
              Set(when.keys).isSubset(of: Set(["at", "days"])), when.count == 2,
              let at = when["at"] as? String else {
            return .bad("when must contain exactly at and days")
        }
        let parts = at.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2, parts[0].count == 2, parts[1].count == 2,
              let hour = Int(parts[0]), let minute = Int(parts[1]),
              (0...23).contains(hour), (0...59).contains(minute) else {
            return .bad("when.at must be HH:MM in local time")
        }
        let weekdayNumbers = ["sun": 1, "mon": 2, "tue": 3, "wed": 4,
                              "thu": 5, "fri": 6, "sat": 7]
        let weekdays: Set<Int>?
        if let days = when["days"] as? String, days == "daily" {
            weekdays = nil
        } else if let days = when["days"] as? [Any], !days.isEmpty {
            var found: Set<Int> = []
            for (index, raw) in days.enumerated() {
                guard let name = raw as? String, let number = weekdayNumbers[name] else {
                    return .bad("when.days[\(index)] must be sun, mon, tue, wed, thu, fri or sat")
                }
                guard found.insert(number).inserted else {
                    return .bad("when.days must not contain duplicates")
                }
            }
            weekdays = found
        } else {
            return .bad("when.days must be daily or a non-empty weekday array")
        }
        guard let task = obj["task"] as? [String: Any] else {
            return .bad("task must be an object")
        }
        let taskAllowed = Set(["assistant", "model", "reasoning_effort", "project_dir", "title", "instructions",
                               "claims", "serialize", "isolation", "isolation_base",
                               "permission_mode", "timeout_minutes", "deliverables", "kind", "plan"])
        let taskUnknown = Set(task.keys).subtracting(taskAllowed).sorted()
        guard taskUnknown.isEmpty else {
            return .bad("unknown task field: \(taskUnknown.joined(separator: ", "))")
        }
        if let deliverables = task["deliverables"] {
            guard let values = deliverables as? [Any], values.count <= 32,
                  values.allSatisfy({ ($0 as? String).map { !$0.isEmpty && $0.count <= 300 } == true })
            else { return .bad("task.deliverables must be an array of at most 32 non-empty strings") }
        }
        for key in ["model", "reasoning_effort", "title", "permission_mode", "kind", "plan"]
            where task[key] != nil && !(task[key] is String) {
            return .bad("task.\(key) must be a string")
        }
        if task["timeout_minutes"] != nil,
           scheduleInt(task["timeout_minutes"]) == nil {
            return .bad("task.timeout_minutes must be an integer")
        }
        var validation = task
        validation["clawdline_protocol"] = 1
        validation["task_id"] = id
        validation["root"] = ["session_id": NSNull(), "label": title]
        if case .bad(let why) = draft(from: validation, expecting: id,
                                      isDirectory: isDirectory) {
            return .bad("task.\(why)")
        }
        guard let enabled = scheduleBool(obj["enabled"]) else {
            return .bad("enabled must be a boolean")
        }
        let closeTab: ScheduleCloseTab
        if let raw = obj["close_tab"] {
            guard let name = raw as? String, let value = ScheduleCloseTab(rawValue: name) else {
                return .bad("close_tab must be on_success, always or never")
            }
            closeTab = value
        } else {
            closeTab = .onSuccess
        }
        let catchUpHours: Int
        if let raw = obj["catch_up_hours"] {
            guard let value = scheduleInt(raw), (0...168).contains(value) else {
                return .bad("catch_up_hours must be an integer from 0 through 168")
            }
            catchUpHours = value
        } else {
            catchUpHours = 6
        }
        let notify: Bool
        if let raw = obj["notify_on_failure"] {
            guard let value = scheduleBool(raw) else {
                return .bad("notify_on_failure must be a boolean")
            }
            notify = value
        } else {
            notify = true
        }
        // Unix seconds, like every other instant this app writes into JSON, and **optional on
        // purpose**. Files written before these fields existed do not have them and neither does
        // one somebody typed in an editor; both keep working exactly as they did, because a
        // missing stamp means "as far back as anyone knows" rather than "made just now".
        //
        // Both are read the same way and refused the same way. They decide whether a session
        // opens, so a value that is not a whole number of seconds is a bad file rather than
        // something to make the best of.
        var badStamp: String?
        func stamp(_ key: String) -> Date? {
            guard let raw = obj[key] else { return nil }
            guard let value = scheduleInt(raw), value >= 0 else {
                badStamp = "\(key) must be a whole number of seconds since 1970"
                return nil
            }
            return Date(timeIntervalSince1970: TimeInterval(value))
        }
        let createdAt = stamp("created_at")
        let whenChangedAt = stamp("when_changed_at")
        if let badStamp { return .bad(badStamp) }
        return .ok(Schedule(id: id, title: title, hour: hour, minute: minute,
                            weekdays: weekdays, taskTemplate: task, enabled: enabled,
                            closeTab: closeTab, catchUpHours: catchUpHours,
                            notifyOnFailure: notify, createdAt: createdAt,
                            whenChangedAt: whenChangedAt))
    }

    /// What ``Orchestrator/scheduleObject(from:id:createdAt:places:isDirectory:)`` decided.
    ///
    /// `made` is the object above after the parser has read it back, and it is handed out so that
    /// an edit can ask what the *new* firing times are without parsing the same object twice. It
    /// is the only way to compare them honestly: `09:00` and `9:00` are different strings and the
    /// second is not a time this parser accepts, so a comparison of raw bodies would be answering
    /// a question about spelling.
    private enum ScheduleObject {
        case built([String: Any], made: Schedule, place: StartPoints.Place)
        case refused(Reply)
    }

    /// The flat body a form sends, assembled into the nested object the parser above expects.
    ///
    /// **The parser is still the authority.** This only arranges fields and hands the result
    /// straight back to it, so every rule it enforces is enforced once rather than twice, and a
    /// refusal carries the parser's own sentence — those sentences were written for somebody to
    /// read, and a second wording of them is a second thing to keep in step.
    ///
    /// Shared by create and edit for the same reason, one step up: an edit that assembled its
    /// object a second way would be a second place those rules drift, and every rejection the
    /// create path is held to would need holding again somewhere else to stop an edit being the
    /// way past them.
    ///
    /// `id` and `createdAt` are parameters precisely because they are the two fields a request
    /// may not name. On a create they are a fresh UUID and this instant. On an edit they are
    /// read off the file being replaced — carrying `created_at` over is what stops an edit from
    /// handing back the bug that field exists to prevent, and a `nil` writes no stamp at all,
    /// which is the meaning a hand-written file has always had rather than one invented for it.
    ///
    /// **A place id, never a path.** The directory comes from ``StartPoints/places()`` on this
    /// side, which is the argument `POST /v1/places/:id/start` makes and it is worth making
    /// twice: a device can only name a project this Mac has already shown it. `places` is a
    /// parameter for the reason ``Planner/draft(for:places:assistants:timeout:)`` has one — a
    /// test can describe a Mac rather than being run on one.
    private static func scheduleObject(from body: [String: Any], id: String, createdAt: Int?,
                                       carrying previous: [String: Any] = [:],
                                       places: [StartPoints.Place],
                                       isDirectory: (String) -> Bool) -> ScheduleObject {
        let allowed = Set(["title", "at", "days", "place_id", "assistant", "instructions",
                           "enabled", "close_tab", "catch_up_hours", "notify_on_failure",
                           "timeout_minutes", "model"])
        let unknown = Set(body.keys).subtracting(allowed).sorted()
        guard unknown.isEmpty else {
            return .refused(.refused(400, "bad_request",
                                     "unknown field: \(unknown.joined(separator: ", "))"))
        }
        // Refused rather than guessed at, and it is the same road as an id nobody was handed: a
        // `project_dir` is not on the list of things a request may carry, so there is nowhere
        // else for the directory to come from.
        guard let placeID = body["place_id"] as? String,
              let place = places.first(where: { $0.id == placeID }) else {
            return .refused(.refused(400, "bad_request",
                                     "place_id must be one of the ids GET /v1/places lists."))
        }
        var task: [String: Any] = [
            "assistant": body["assistant"] ?? "",
            "project_dir": place.path,
            "title": body["title"] ?? "",
            "instructions": body["instructions"] ?? "",
        ]
        // An empty model is how a form says "whatever that assistant runs by default", and a
        // file carrying `"model": ""` is a field whoever opens it later has to read and dismiss.
        //
        // **A body that never mentions `model` and a body that sends an empty one are different
        // requests**, which is why this is not on the carried list below. The page's form has no
        // model control and sends no `model` key at all, so folding the two together would mean
        // either that a phone save silently drops somebody's `"model": "opus"` — measured, and
        // exactly the class of bug the carried list exists to close — or that no request can ever
        // take a model off a schedule again.
        if let model = body["model"] {
            if (model as? String)?.isEmpty != true { task["model"] = model }
        } else if let kept = previous["model"] {
            task["model"] = kept
        }
        if let timeout = body["timeout_minutes"] { task["timeout_minutes"] = timeout }
        // **Fields no form has a control for are carried, not dropped.** A schedule written by
        // hand with `claims` in it — and the Mac is where those live — would otherwise lose them
        // the first time somebody opened it and pressed Save, silently, on either surface.
        // Measured before this existed: claims and permission_mode were gone after one save.
        // A create passes nothing here and so is unaffected; only a save over an existing file
        // has anything to carry.
        for key in ["claims", "permission_mode", "serialize", "isolation", "isolation_base",
                    "deliverables", "kind", "plan"] where task[key] == nil {
            if let kept = previous[key] { task[key] = kept }
        }
        // Reasoning is the one hidden field whose validity depends on a visible field. Keep it
        // through an ordinary Codex edit, but an explicit assistant switch to Claude must not
        // trap the form behind a value it has no control with which to remove.
        if task["assistant"] as? String == Assistant.codex.rawValue,
           let kept = previous["reasoning_effort"] {
            task["reasoning_effort"] = kept
        }
        var obj: [String: Any] = [
            "clawdline_schedule": 1,
            "schedule_id": id,
            "title": body["title"] ?? "",
            // `days` is deliberately not defaulted. A missing time or a missing set of days is a
            // request that did not say when, and the parser has a sentence for each; picking
            // `daily` on their behalf would be this route quietly choosing how often somebody
            // else's work runs. `enabled` is the opposite and is defaulted below: a schedule
            // somebody has just asked for is on.
            "when": ["at": body["at"] ?? "", "days": body["days"] ?? ""],
            "task": task,
            "enabled": body["enabled"] ?? true,
        ]
        // Written by the Mac and by nothing else, and the request cannot name it — it is on the
        // parser's list, not on the list above. It is what stops the minute timer from treating
        // this morning's nine o'clock as an occurrence this schedule slept through: the 200 says
        // the next fire is tomorrow, and the beat agrees. An edit hands back the instant it read
        // off the file it is replacing, so saving a schedule at lunchtime cannot resurrect that
        // bug on a file that has been right about its own age since the day it was made.
        if let createdAt { obj["created_at"] = createdAt }
        // Written only when they were asked for, so the parser's defaults stay the one place
        // those three numbers are decided.
        for key in ["close_tab", "catch_up_hours", "notify_on_failure"] {
            if let value = body[key] { obj[key] = value }
        }
        switch schedule(from: obj, filename: "\(id).json", isDirectory: isDirectory) {
        case .bad(let why):
            return .refused(.refused(400, "bad_request", why))
        case .ok(let made):
            return .built(obj, made: made, place: place)
        }
    }

    /// Put one assembled schedule object on disk under `<id>.json`, and read it back **off disk,
    /// through the same parser**, before the caller is told it worked.
    ///
    /// A file this app cannot itself parse must not survive the request that made it: it would
    /// come back as an `invalid` row, audit itself and send a push, and nobody would know which
    /// request had left it there.
    ///
    /// `previous` is the bytes that were under this name a moment ago, or nil for a file being
    /// made. It is the only thing the two callers do differently at the end, and the reason they
    /// can still share this: a create that cannot be read back deletes what it wrote, while an
    /// edit that cannot be read back puts the old file back — the schedule somebody already had
    /// is not a failed save's to take away.
    private static func writeSchedule(_ obj: [String: Any], id: String, previous: Data?,
                                      event: String, extra: [String: String], now: Date,
                                      isDirectory: (String) -> Bool) -> Reply {
        let filename = "\(id).json"
        let file = scheduleDirectory.appendingPathComponent(filename)
        // Named out here so the failure path can take it away again. A `.new` left behind is
        // hidden and does not end in `.json`, so the inventory never sees it — which is exactly
        // why it would sit there forever if nothing swept it.
        let staging = scheduleDirectory.appendingPathComponent(".\(filename).new")
        do {
            let manager = FileManager.default
            // Created 0o700 when it is not there, and **left exactly as it is when it is**. The
            // directory may predate this route and belongs to whoever made it; a write route
            // that also tightened permissions on somebody's existing directory would be doing a
            // second thing nobody asked for.
            if !manager.fileExists(atPath: scheduleDirectory.path) {
                try manager.createDirectory(at: scheduleDirectory, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
            }
            let data = try JSONSerialization.data(withJSONObject: obj,
                                                  options: [.prettyPrinted, .sortedKeys,
                                                            .withoutEscapingSlashes])
            // Through a neighbouring file rather than straight to the name, so the mode is 0o600
            // before the schedule exists under a name anything reads. `Data.write(_:.atomic)`
            // renames a temporary file into place too, but the mode it lands with is the
            // process umask's, and the minute timer reads this directory while the request is
            // still in flight. Task files are 0o600; a schedule carries the same first message.
            try data.write(to: staging, options: .atomic)
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: staging.path)
            if previous == nil {
                try manager.moveItem(at: staging, to: file)
            } else {
                // The name is already taken, and `moveItem` refuses rather than replaces. This
                // swaps the two in one step so the minute timer, which reads this directory on
                // its own beat, never finds the schedule briefly absent — removing and then
                // renaming would open exactly that window. The mode is set again on the far side
                // because a replace carries the *replaced* file's metadata forward, and 0600 is
                // this app's promise about a schedule rather than a fact about what was there.
                _ = try manager.replaceItemAt(file, withItemAt: staging)
                try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
            }
        } catch {
            try? FileManager.default.removeItem(at: staging)
            RemoteAuth.audit(event, ["schedule": id, "ok": "0", "why": "write_failed"])
            return .refused(500, "write_failed", "The schedule file could not be written.")
        }
        guard let written = try? Data(contentsOf: file),
              let readBack = (try? JSONSerialization.jsonObject(with: written)) as? [String: Any],
              case .ok(let made) = schedule(from: readBack, filename: filename,
                                            isDirectory: isDirectory) else {
            if let previous {
                try? previous.write(to: file, options: .atomic)
                try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                       ofItemAtPath: file.path)
            } else {
                try? FileManager.default.removeItem(at: file)
            }
            RemoteAuth.audit(event, ["schedule": id, "ok": "0", "why": "unreadable"])
            return .refused(500, "write_failed",
                            previous == nil
                                ? "The schedule was written and could not be read back, so it "
                                    + "has been removed."
                                : "The change was written and could not be read back, so the "
                                    + "schedule you already had has been put back.")
        }
        var line = ["schedule": id, "ok": "1"]
        for (key, value) in extra { line[key] = value }
        RemoteAuth.audit(event, line)
        var record: [String: Any] = ["id": made.id, "title": made.title, "enabled": made.enabled]
        if let next = nextFire(of: made, after: now) {
            record["next_fire"] = Int(next.timeIntervalSince1970)
        }
        return .ok(["ok": true, "schedule": record])
    }

    /// The one way anything other than a text editor makes a schedule file.
    static func createSchedule(from body: [String: Any],
                               places: [StartPoints.Place] = StartPoints.places(),
                               now: Date = Date(),
                               isDirectory: (String) -> Bool = StartPoints.isDirectory) -> Reply {
        let id = UUID().uuidString.lowercased()
        let obj: [String: Any]
        let place: StartPoints.Place
        // No `when_changed_at` is written here on purpose. Nothing has changed yet, and
        // `created_at` — this same instant — already answers everything the timer needs to ask
        // about a schedule's first day.
        switch scheduleObject(from: body, id: id, createdAt: Int(now.timeIntervalSince1970),
                              places: places, isDirectory: isDirectory) {
        case .refused(let reply): return reply
        case .built(let built, _, let chosen): obj = built; place = chosen
        }
        // `rate_limited` rather than `busy`: everywhere else in this app `busy` is queue depth —
        // something already in hand that drains in seconds — and `rate_limited` is a sliding
        // window of counted attempts. This brake is the second kind, the same shape as
        // `takeDispatchRate()`, and the two codes tell a client different things about waiting.
        guard takeScheduleWriteRate() else {
            return .refused(429, "rate_limited",
                            "This Mac has been asked for several schedules in the last few "
                            + "minutes. Try again shortly.")
        }
        return writeSchedule(obj, id: id, previous: nil, event: "orchestrator.schedule.created",
                             extra: ["place": place.id, "cwd": place.path], now: now,
                             isDirectory: isDirectory)
    }

    /// Change one schedule that already exists, from the same body a create takes.
    ///
    /// The Mac's Settings calls this directly rather than over HTTP, which is why it takes a
    /// `places` list and an `id` instead of a request — there is one implementation of what an
    /// edit means, and the route in `RemoteServer` is a door onto it rather than a second one.
    ///
    /// **`schedule_id` and `created_at` come off the file being replaced and never off the
    /// request.** Neither is a field the body may carry — they are refused as unknown fields
    /// like `project_dir` is — and carrying `created_at` over is not bookkeeping. It is what
    /// keeps a schedule from running for an occurrence that predates it: a route that stamped
    /// the current instant on every save would make saving a `09:00` schedule at lunchtime open
    /// a session for this morning, which is the bug that field was added to stop, handed back
    /// through a different door.
    ///
    /// **A schedule this app cannot read is not one it will edit.** The `404` is the same
    /// sentence a manual run gives, and the reason is that an edit replaces the whole file: an
    /// invalid one has no id and no `created_at` worth carrying, and the list does not give an
    /// invalid row an id to address in the first place. Removing it and making a new one is the
    /// repair, and ``Orchestrator/deleteSchedule(id:)`` does not need to understand a file to
    /// take it away.
    ///
    /// **A running task does not block an edit**, unlike a manual run's `409 schedule_active`.
    /// That refusal is about stacking a second session on top of a first; this changes a file
    /// nothing in flight will read again, because a task is materialised from the template when
    /// it is dispatched. The occurrence a change lands in the middle of keeps the terms it was
    /// dispatched under; the next one uses the new file.
    static func updateSchedule(id: String, from body: [String: Any],
                               places: [StartPoints.Place] = StartPoints.places(),
                               now: Date = Date(),
                               isDirectory: (String) -> Bool = StartPoints.isDirectory) -> Reply {
        guard let existing = scheduleNamed(id),
              let previous = try? Data(contentsOf: scheduleDirectory
                  .appendingPathComponent("\(id).json")) else {
            return .refused(404, "not_found", "No schedule named that")
        }
        // The old task template, read back out of the file being replaced. It is the only place
        // the carried fields exist.
        let carried = ((try? JSONSerialization.jsonObject(with: previous)) as? [String: Any])?["task"]
            as? [String: Any] ?? [:]
        var obj: [String: Any]
        let made: Schedule
        switch scheduleObject(from: body, id: existing.id,
                              createdAt: existing.createdAt.map { Int($0.timeIntervalSince1970) },
                              carrying: carried,
                              places: places, isDirectory: isDirectory) {
        case .refused(let reply): return reply
        case .built(let built, let parsed, _): obj = built; made = parsed
        }
        // The second stamp, and the whole of the rule: an occurrence that only exists because of
        // this save is not one this Mac slept through. Moving `21:00` to `09:00` at two in the
        // afternoon used to dispatch today's nine o'clock within the minute — while this route's
        // own answer said tomorrow — or, past the catch-up window, push that a run was missed.
        //
        // **Only a change to the firing times moves it.** Every other save carries the old stamp
        // across untouched, for the reason `created_at` is carried rather than restamped: a
        // schedule that really was missed at nine is still missed after somebody fixes its title
        // at eleven, and a save that swallowed that would be this route deciding a run did not
        // matter because an unrelated word changed.
        //
        // A file that never had one is stamped here, unlike `created_at`, and the difference is
        // what the two fields claim. `created_at` would be a guess about a past this app was not
        // there for; this is a fact it is watching happen.
        if made.hour == existing.hour, made.minute == existing.minute,
           made.weekdays == existing.weekdays {
            if let unchanged = existing.whenChangedAt {
                obj["when_changed_at"] = Int(unchanged.timeIntervalSince1970)
            }
        } else {
            obj["when_changed_at"] = Int(now.timeIntervalSince1970)
        }
        // The same brake as a create, and for the same reason: what it bounds is a client
        // retrying in a loop with a fresh key each time, and a loop of saves writes a file per
        // attempt exactly as a loop of creates does. Deleting is deliberately not braked — it
        // leaves nothing behind to sweep up, and it is the one thing somebody reaches for when
        // they want work to stop.
        guard takeScheduleWriteRate() else {
            return .refused(429, "rate_limited",
                            "This Mac has been asked for several schedules in the last few "
                            + "minutes. Try again shortly.")
        }
        return writeSchedule(obj, id: id, previous: previous,
                             event: "orchestrator.schedule.updated", extra: [:], now: now,
                             isDirectory: isDirectory)
    }

    /// Take one schedule away.
    ///
    /// **Two different failures, and a caller can tell them apart.** `404 not_found` is "there
    /// was no such schedule", and it is also the answer for an id that is not an id — the file
    /// is addressed as `<id>.json` and nothing else, which is the whole of the path handling
    /// here. `500 delete_failed` is "the file would not go", which is a fact about this Mac and
    /// a thing somebody has to go and look at, and answering it as a `404` would tell them the
    /// schedule is gone while it is still on disk and still firing.
    ///
    /// Content is not read. A file named after a UUID whose contents this app cannot parse is
    /// exactly the file somebody most wants to remove, and needing to understand it first would
    /// be a rule with no purpose. A file whose *name* is not a UUID — `broken.json` — has no id
    /// to address it by and still goes from the Finder.
    ///
    /// **A running task does not block this either.** It is not a cancel: the task keeps its own
    /// id, its own tab and its own record, and `POST /v1/orchestrator/tasks/:id/cancel` is what
    /// stops it. Refusing while something is running would be refusing precisely when somebody
    /// most wants the schedule gone, and would send them back to the Mac and a text editor —
    /// which is the thing this route exists to end.
    static func deleteSchedule(id: String) -> Reply {
        guard isTaskID(id) else {
            return .refused(404, "not_found", "No schedule named that")
        }
        let filename = "\(id).json"
        let file = scheduleDirectory.appendingPathComponent(filename)
        guard FileManager.default.fileExists(atPath: file.path) else {
            return .refused(404, "not_found", "No schedule named that")
        }
        do {
            try FileManager.default.removeItem(at: file)
        } catch {
            RemoteAuth.audit("orchestrator.schedule.deleted",
                             ["schedule": id, "ok": "0", "why": "remove_failed"])
            return .refused(500, "delete_failed", "The schedule file could not be removed.")
        }
        // The three tables the beat keeps are keyed by schedule id, and nothing else ever takes
        // an entry out of them — the id was a filename, and filenames used to only appear. An id
        // that comes back later, from a file somebody restores, would otherwise inherit a
        // handled occurrence and a "last missed" from a schedule that is not this one.
        lock.lock()
        handledScheduleFires.removeValue(forKey: id)
        pendingScheduleFires.removeValue(forKey: id)
        lastMissedScheduleFires.removeValue(forKey: id)
        invalidScheduleFingerprints.removeValue(forKey: filename)
        lock.unlock()
        RemoteAuth.audit("orchestrator.schedule.deleted", ["schedule": id, "ok": "1"])
        return .ok(["ok": true, "deleted": id])
    }

    /// Ten new schedules in ten minutes, and the eleventh is told to come back.
    ///
    /// A brake on a loop rather than a cap on how many schedules a Mac may hold — there is no
    /// number of files that is too many, and somebody filling a form cannot reach this. What it
    /// bounds is a client that retries in a tight loop with a fresh `Idempotency-Key` each time,
    /// which would otherwise leave one repeating dispatch per attempt behind it.
    ///
    /// Deliberately **not** ``takeDispatchRate()``: making a schedule is not a dispatch, and
    /// spending the tree's tickets on it would have a phone's form quietly stopping a root
    /// session from opening children.
    private static func takeScheduleWriteRate() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let now = Date()
        scheduleWriteTimes = scheduleWriteTimes.filter { now.timeIntervalSince($0) < 600 }
        guard scheduleWriteTimes.count < 10 else { return false }
        scheduleWriteTimes.append(now)
        return true
    }

    /// Everything one schedule is, including the task template and retained run history the list
    /// route leaves out.
    ///
    /// The list is a list — it says what exists, when it next fires and how the last run went,
    /// and it says nothing about what any of them actually *does*. That is the right amount for
    /// a row, and the wrong amount for the only screen where somebody can check what they just
    /// made, which is why this exists and why it is the same read-level door the list is behind.
    static func scheduleRecord(id: String, now: Date = Date()) -> [String: Any]? {
        guard isTaskID(id), let schedule = schedules().first(where: { $0.id == id }) else {
            return nil
        }
        var out: [String: Any] = ["id": schedule.id, "title": schedule.title,
                                  "enabled": schedule.enabled,
                                  "file": "\(schedule.id).json",
                                  "task": schedule.taskTemplate,
                                  "close_tab": schedule.closeTab.rawValue,
                                  "catch_up_hours": schedule.catchUpHours,
                                  "notify_on_failure": schedule.notifyOnFailure]
        let names = [1: "sun", 2: "mon", 3: "tue", 4: "wed", 5: "thu", 6: "fri", 7: "sat"]
        let time = String(format: "%02d:%02d", schedule.hour, schedule.minute)
        // Back in the shape the file used and the form sends, rather than the Calendar numbers
        // this holds in memory: a page that had to know Sunday is 1 would be a second place the
        // weekday order is written down.
        out["when"] = ["at": time,
                       "days": schedule.weekdays.map { found in
                           (1...7).compactMap { found.contains($0) ? names[$0] : nil }
                       } ?? "daily"]
        if let next = nextFire(of: schedule, after: now) {
            out["next_fire"] = Int(next.timeIntervalSince1970)
        }
        lock.lock()
        let snapshots = Array(tasks.values)
        let missed = lastMissedScheduleFires[schedule.id]
        lock.unlock()
        let runs = snapshots.filter { $0.scheduleID == schedule.id }
            .sorted { $0.created > $1.created }
        if let last = runs.first {
            out["last_run"] = ["task_id": last.id, "state": last.state.rawValue,
                               "at": Int(last.created.timeIntervalSince1970)]
        }
        // A schedule run is an ordinary orchestrator task, but this is the one screen where its
        // child conversation is product data rather than broker plumbing. Only an identity already
        // proved against that task's own briefing is returned. A guessed or stale session id never
        // becomes a resume flag merely because it survived in the registry.
        out["runs"] = runs.map(scheduleRunRecord)
        // Cleanup keeps two hundred task records for the whole Mac. At that boundary this schedule
        // may have older runs the registry can no longer associate with it, and the client must say
        // so rather than drawing the bottom of the list as the beginning of history.
        if snapshots.count >= 200 { out["runs_may_be_truncated"] = true }
        if let missed { out["last_missed_at"] = Int(missed.timeIntervalSince1970) }
        return out
    }

    /// The compact occurrence shape used only inside one schedule detail response.
    ///
    /// `terminal_id` is useful while the tab is still visible; `session_id` is useful after it has
    /// gone. The latter is emitted only for terminal work with a transcript/rollout that still
    /// exists and is proven to belong to this exact task. That is the authorization fact the
    /// existing place-resume route rechecks below before turning the id into a CLI flag.
    private static func scheduleRunRecord(_ task: Task) -> [String: Any] {
        var out: [String: Any] = [
            "task_id": task.id,
            "state": task.state.rawValue,
            "assistant": task.assistant.rawValue,
            "project_dir": task.projectDir,
            "created": Int(task.created.timeIntervalSince1970),
        ]
        if let finished = task.finishedAt {
            out["finished_at"] = Int(finished.timeIntervalSince1970)
        }
        if let terminal = task.childTerminalId { out["terminal_id"] = terminal }
        if task.state.isTerminal, let session = availableScheduledSessionID(of: task) {
            out["session_id"] = session
        }
        if let summary = task.summary { out["summary"] = summary }
        // snake_case, unlike `shape()`'s `attachSession` — every other key in a schedule run
        // record is spelled this way and a record is read as one thing.
        if let session = task.attachSessionId {
            out["attached"] = true
            out["attach_session"] = session
        }
        return out
    }

    /// Whether the existing place-resume route may accept one conversation that the ordinary
    /// project history deliberately hides because it began as Clawdline plumbing.
    ///
    /// The exception is schedule-only, terminal-only, project- and assistant-exact, and backed by
    /// the same task/transcript ownership proof used for usage accounting. This keeps dispatched
    /// children out of the general history picker while allowing the schedule detail that disclosed
    /// the run to pick that exact conversation back up.
    static func scheduledResumeAllowed(sessionID: String, assistant: Assistant,
                                       projectDir: String) -> Bool {
        load()
        lock.lock()
        let candidates = tasks.values.filter {
            $0.scheduleID != nil && $0.state.isTerminal && $0.assistant == assistant
                && $0.projectDir == projectDir && $0.childSessionId == sessionID
        }
        lock.unlock()
        return candidates.contains { availableScheduledSessionID(of: $0) == sessionID }
    }

    private static func availableScheduledSessionID(of task: Task) -> String? {
        guard task.scheduleID != nil, let path = task.transcriptPath,
              FileManager.default.fileExists(atPath: path) else { return nil }
        guard let session = provenChildSessionID(of: task), isTaskID(session) else { return nil }
        return session
    }

    static func latestFire(of schedule: Schedule, at now: Date,
                           calendar: Calendar = .autoupdatingCurrent) -> Date? {
        let start = calendar.startOfDay(for: now)
        for daysAgo in 0...7 {
            guard let day = calendar.date(byAdding: .day, value: -daysAgo, to: start),
                  let candidate = calendar.date(bySettingHour: schedule.hour,
                                                minute: schedule.minute, second: 0, of: day),
                  candidate <= now else { continue }
            if let weekdays = schedule.weekdays,
               !weekdays.contains(calendar.component(.weekday, from: candidate)) { continue }
            return candidate
        }
        return nil
    }

    /// What the minute timer should do about one occurrence of one schedule.
    ///
    /// `createdAt` is the schedule's own age, and it is read first: an occurrence from before the
    /// file existed is not a run this Mac slept through, because there was nothing there to sleep.
    /// Without it, "09:00 daily" made at lunchtime is inside the six-hour catch-up window and
    /// dispatches within the minute — while the 200 that created it said the next run was tomorrow
    /// — and made in the evening it instead pushes that a run was missed and shows "last missed 1
    /// day ago". A nil `createdAt` is a file that never said when it was made, and keeps the
    /// behaviour every hand-written schedule has always had.
    ///
    /// `whenChangedAt` is the same gate one question further along, and it is read second because
    /// the two answer different things: the first is "was there a schedule here", the second is
    /// "was there an occurrence here". A save that moves `21:00` to `09:00` at two in the
    /// afternoon leaves `created_at` correctly alone — the schedule is days old — and invents an
    /// occurrence six hours in the past, which is inside the default catch-up window and so was
    /// dispatched within the minute. Nobody missed a nine o'clock that did not exist at nine.
    static func scheduleAction(now: Date, fire: Date, catchUpHours: Int,
                               lastRunCreated: Date?, lastRunTerminal: Bool?,
                               createdAt: Date?, whenChangedAt: Date?) -> ScheduleAction {
        if let createdAt, fire < createdAt { return .beforeCreation }
        if let whenChangedAt, fire < whenChangedAt { return .beforeRetiming }
        if let created = lastRunCreated, created >= fire { return .alreadyHandled }
        if lastRunTerminal == false { return .active }
        let window = TimeInterval(max(60, catchUpHours * 3600))
        return now.timeIntervalSince(fire) <= window ? .run : .missed
    }

    static func scheduledCloseAt(policy: ScheduleCloseTab, outcome: State,
                                 now: Date, hasChild: Bool, linger: TimeInterval = 180,
                                 briefed: Bool = true) -> Date? {
        guard hasChild else { return nil }
        switch policy {
        case .always: return now
        case .onSuccess: return outcome == .success ? now : nil
        case .never:
            guard linger >= 0 else { return nil }
            if outcome == .success || outcome == .failure {
                return now.addingTimeInterval(linger)
            }
            return outcome == .spawnFailed && !briefed ? now : nil
        }
    }

    static func nextFire(of schedule: Schedule, after now: Date,
                         calendar: Calendar = .autoupdatingCurrent) -> Date? {
        let start = calendar.startOfDay(for: now)
        for daysAhead in 0...7 {
            guard let day = calendar.date(byAdding: .day, value: daysAhead, to: start),
                  let candidate = calendar.date(bySettingHour: schedule.hour,
                                                minute: schedule.minute, second: 0, of: day),
                  candidate > now else { continue }
            if let weekdays = schedule.weekdays,
               !weekdays.contains(calendar.component(.weekday, from: candidate)) { continue }
            return candidate
        }
        return nil
    }

    /// Read every source file independently. A malformed neighbor cannot hide a valid schedule.
    /// Invalid content is fingerprinted so the minute timer and polling GETs do not turn one bad
    /// file into an append-only audit flood; changing or reintroducing it reports it once again.
    private static func scheduleInventory() -> ScheduleInventory {
        let manager = FileManager.default
        guard let files = try? manager.contentsOfDirectory(at: scheduleDirectory,
            includingPropertiesForKeys: [.isRegularFileKey], options: [.skipsHiddenFiles]) else {
            lock.lock(); invalidScheduleFingerprints = [:]; lock.unlock()
            return ScheduleInventory(valid: [], invalid: [])
        }
        var found: [Schedule] = []
        var invalid: [InvalidSchedule] = []
        for file in files where file.pathExtension == "json" {
            guard let data = try? Data(contentsOf: file) else {
                let stamp = (try? file.resourceValues(forKeys: [.contentModificationDateKey]))?
                    .contentModificationDate?.timeIntervalSince1970 ?? 0
                invalid.append(InvalidSchedule(file: file.lastPathComponent,
                    error: "The file could not be read as JSON.", kind: "unreadable_json",
                    fingerprint: "unreadable:\(stamp)", title: file.deletingPathExtension().lastPathComponent,
                    projectDir: nil, notifyOnFailure: true))
                continue
            }
            let digest = RemoteAuth.hex(SHA256.hash(data: data))
            guard let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
                invalid.append(InvalidSchedule(file: file.lastPathComponent,
                    error: "The file does not contain a JSON object.", kind: "unreadable_json",
                    fingerprint: digest, title: file.deletingPathExtension().lastPathComponent,
                    projectDir: nil, notifyOnFailure: true))
                continue
            }
            switch schedule(from: obj, filename: file.lastPathComponent) {
            case .ok(let value): found.append(value)
            case .bad(let why):
                let transient = why.contains("project_dir must be an absolute path to a directory")
                invalid.append(InvalidSchedule(file: file.lastPathComponent,
                    error: String(why.prefix(500)),
                    kind: transient ? "project_unavailable" : "schema",
                    fingerprint: digest, title: (obj["title"] as? String).map {
                        String($0.prefix(120))
                    } ?? file.deletingPathExtension().lastPathComponent,
                    projectDir: (obj["task"] as? [String: Any])?["project_dir"] as? String,
                    notifyOnFailure: scheduleBool(obj["notify_on_failure"]) ?? true))
            }
        }
        let fingerprints = Dictionary(uniqueKeysWithValues: invalid.map { ($0.file, $0.fingerprint) })
        lock.lock()
        let newlyInvalid = invalid.filter { invalidScheduleFingerprints[$0.file] != $0.fingerprint }
        invalidScheduleFingerprints = fingerprints
        lock.unlock()
        for item in newlyInvalid {
            RemoteAuth.audit("orchestrator.schedule.invalid",
                             ["file": item.file, "why": item.error, "kind": item.kind])
            guard item.notifyOnFailure else { continue }
            WebPush.send(title: item.title,
                         body: "Schedule file \(item.file) is invalid: \(item.error)",
                         url: "/", tag: "schedule-invalid-\(item.file)",
                         icon: RemoteIcon.projectPath(
                            for: item.projectDir.flatMap { ProjectIcon.grid(forCwd: $0) }))
        }
        return ScheduleInventory(valid: found.sorted { $0.id < $1.id },
                                 invalid: invalid.sorted { $0.file < $1.file })
    }

    static func schedules() -> [Schedule] {
        scheduleInventory().valid
    }

    static func scheduleRecords(now: Date = Date()) -> [[String: Any]] {
        let inventory = scheduleInventory()
        lock.lock()
        let snapshots = Array(tasks.values)
        let missedSnapshots = lastMissedScheduleFires
        lock.unlock()
        let valid = inventory.valid.map { schedule -> [String: Any] in
            var out: [String: Any] = ["id": schedule.id, "title": schedule.title,
                                      "enabled": schedule.enabled]
            if let next = nextFire(of: schedule, after: now) {
                out["next_fire"] = Int(next.timeIntervalSince1970)
            }
            if let last = snapshots.filter({ $0.scheduleID == schedule.id })
                .max(by: { $0.created < $1.created }) {
                out["last_run"] = ["task_id": last.id, "state": last.state.rawValue,
                                   "at": Int(last.created.timeIntervalSince1970)]
            }
            if let missed = missedSnapshots[schedule.id] {
                out["last_missed_at"] = Int(missed.timeIntervalSince1970)
            }
            return out
        }
        return valid + inventory.invalid.map(\.record)
    }

    private static func scheduleNamed(_ id: String) -> Schedule? {
        guard isTaskID(id) else { return nil }
        return schedules().first { $0.id == id }
    }

    private static func hasActiveScheduleTaskLocked(_ id: String) -> Bool {
        tasks.values.contains { $0.scheduleID == id && !$0.state.isTerminal }
    }

    /// 32 random bytes as hex. Every task secret this app mints itself comes from here — the
    /// scheduled dispatch below, and a respawn retrying a tab that would not open.
    private static func freshTaskSecret() -> String? {
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            return nil
        }
        return RemoteAuth.hex(bytes)
    }

    /// Materialise the template as an ordinary task file, then enter through the same dispatch
    /// gate as every root request. No claims, capacity, trust or serialization rule is duplicated.
    private static func dispatch(_ schedule: Schedule) -> Reply {
        let id = UUID().uuidString.lowercased()
        guard let secret = freshTaskSecret() else {
            return .refused(500, "internal", "Could not create the scheduled task secret.")
        }
        var obj = schedule.taskTemplate
        obj["clawdline_protocol"] = 1
        obj["task_id"] = id
        obj["root"] = ["session_id": NSNull(), "label": schedule.title]
        let directory = root.appendingPathComponent(id, isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                  ofItemAtPath: directory.path)
            let data = try JSONSerialization.data(withJSONObject: obj,
                                                   options: [.prettyPrinted, .sortedKeys,
                                                             .withoutEscapingSlashes])
            let file = directory.appendingPathComponent("task.json")
            try data.write(to: file, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: file.path)
        } catch {
            return .refused(500, "internal", "Could not create the scheduled task file.")
        }
        return dispatch(taskID: id, secret: secret, schedule: schedule)
    }

    static func runSchedule(id: String) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled",
                            "Task dispatch is switched off in Settings.")
        }
        guard let schedule = scheduleNamed(id) else {
            return .refused(404, "not_found", "No schedule named that")
        }
        lock.lock()
        if hasActiveScheduleTaskLocked(id) || dispatchingSchedules.contains(id)
            || pendingScheduleFires[id] != nil {
            lock.unlock()
            return .refused(409, "schedule_active",
                            "The previous task from this schedule is still active.")
        }
        dispatchingSchedules.insert(id)
        lock.unlock()
        RemoteAuth.audit("orchestrator.schedule.run", ["schedule": id, "how": "manual"])
        let reply = scheduleRunnerForTesting?(schedule) ?? dispatch(schedule)
        lock.lock()
        dispatchingSchedules.remove(id)
        if case .ok = reply, let fire = latestFire(of: schedule, at: Date()) {
            handledScheduleFires[id] = fire
        }
        lock.unlock()
        return reply
    }

    static func scheduleBeat(now: Date = Date()) {
        guard Config.shared.orchestratorEnabled else { return }
        for schedule in schedules() where schedule.enabled {
            guard let fire = latestFire(of: schedule, at: now) else { continue }
            lock.lock()
            if handledScheduleFires[schedule.id] == fire || pendingScheduleFires[schedule.id] == fire {
                lock.unlock()
                continue
            }
            let matching = tasks.values.filter { $0.scheduleID == schedule.id }
            let latest = matching.max(by: { $0.created < $1.created })
            let active = matching.contains { !$0.state.isTerminal }
                || dispatchingSchedules.contains(schedule.id)
            let action = scheduleAction(now: now, fire: fire,
                                        catchUpHours: schedule.catchUpHours,
                                        lastRunCreated: latest?.created,
                                        lastRunTerminal: active ? false : latest.map { _ in true },
                                        createdAt: schedule.createdAt,
                                        whenChangedAt: schedule.whenChangedAt)
            // Only the two outcomes that *consume* an occurrence write it down. An occurrence
            // from before the schedule was made — or from before the save that invented it — is
            // not one of them: nothing missed it, so it leaves no `last_missed_at` behind for the
            // list to draw and nothing to audit.
            if action == .run {
                pendingScheduleFires[schedule.id] = fire
            } else if action == .active || action == .missed {
                handledScheduleFires[schedule.id] = fire
                if action == .missed { lastMissedScheduleFires[schedule.id] = fire }
            }
            lock.unlock()
            switch action {
            case .run:
                let work = { runScheduledFire(schedule, fire: fire) }
                if let enqueue = scheduleDispatchEnqueuerForTesting { enqueue(work) }
                else {
                    let admitted = RemoteServer.shared.enqueueTerminalCommand(
                        channel: "schedule:\(schedule.id)", work)
                    if !admitted {
                        lock.lock()
                        if pendingScheduleFires[schedule.id] == fire {
                            pendingScheduleFires.removeValue(forKey: schedule.id)
                        }
                        lock.unlock()
                        RemoteAuth.audit("orchestrator.schedule.retry",
                                         ["schedule": schedule.id, "why": "terminal_busy"])
                    }
                }
            case .active:
                RemoteAuth.audit("orchestrator.schedule.skipped",
                                 ["schedule": schedule.id, "why": "active"])
            case .missed:
                RemoteAuth.audit("orchestrator.schedule.skipped",
                                 ["schedule": schedule.id, "why": "missed"])
                if schedule.notifyOnFailure {
                    sendSchedulePush(schedule, body: "Scheduled run missed its catch-up window.",
                                     tag: "schedule-\(schedule.id)-missed")
                }
            case .alreadyHandled, .beforeCreation, .beforeRetiming: break
            }
        }
    }

    /// Runs only on the remote server's serial queue in production. The second active check is
    /// the handoff between the timer's decision and the dispatch transaction: a manual run that
    /// reached the queue first wins, and this occurrence becomes an audited active skip.
    private static func runScheduledFire(_ schedule: Schedule, fire: Date) {
        // Read off disk again rather than trusted, because the occurrence was decided on the
        // timer and is only reaching this queue now. `DELETE /v1/orchestrator/schedules/:id` is
        // a route somebody presses when they want work to stop, and a session opening out of a
        // schedule that was removed a second earlier would be the one way pressing it is not
        // enough. Asked outside the lock: the inventory takes it for itself.
        guard scheduleNamed(schedule.id) != nil else {
            lock.lock()
            pendingScheduleFires.removeValue(forKey: schedule.id)
            lock.unlock()
            RemoteAuth.audit("orchestrator.schedule.skipped",
                             ["schedule": schedule.id, "why": "removed"])
            return
        }
        lock.lock()
        pendingScheduleFires.removeValue(forKey: schedule.id)
        if hasActiveScheduleTaskLocked(schedule.id) || dispatchingSchedules.contains(schedule.id) {
            handledScheduleFires[schedule.id] = fire
            lock.unlock()
            RemoteAuth.audit("orchestrator.schedule.skipped",
                             ["schedule": schedule.id, "why": "active"])
            return
        }
        dispatchingSchedules.insert(schedule.id)
        lock.unlock()

        RemoteAuth.audit("orchestrator.schedule.run",
                         ["schedule": schedule.id, "how": "timer",
                          "fire": String(Int(fire.timeIntervalSince1970))])
        let reply = scheduleRunnerForTesting?(schedule) ?? dispatch(schedule)
        let overCapacity: Bool
        if case .refused(_, let code, _, _) = reply { overCapacity = code == "over_capacity" }
        else { overCapacity = false }

        lock.lock()
        dispatchingSchedules.remove(schedule.id)
        if !overCapacity { handledScheduleFires[schedule.id] = fire }
        lock.unlock()

        guard case .refused(_, let code, let message, _) = reply else { return }
        if overCapacity {
            RemoteAuth.audit("orchestrator.schedule.retry",
                             ["schedule": schedule.id, "why": code])
            return
        }
        RemoteAuth.audit("orchestrator.schedule.failed",
                         ["schedule": schedule.id, "why": code])
        if schedule.notifyOnFailure {
            sendSchedulePush(schedule, body: "Dispatch failed: \(message)",
                             tag: "schedule-\(schedule.id)-failed")
        }
    }

    private static func sendSchedulePush(_ schedule: Schedule, body: String, tag: String) {
        WebPush.send(title: schedule.title, body: body, url: "/", tag: tag,
                     icon: RemoteIcon.projectPath(
                        for: (schedule.taskTemplate["project_dir"] as? String)
                            .flatMap { ProjectIcon.grid(forCwd: $0) }))
    }

    // MARK: - A task

    enum State: String {
        case queued, spawning, briefed
        case success, failure, timeout, cancelled
        case spawnFailed = "spawn_failed"

        var isTerminal: Bool {
            switch self {
            case .queued, .spawning, .briefed: return false
            case .success, .failure, .timeout, .cancelled, .spawnFailed: return true
            }
        }
    }

    enum Isolation: String {
        case none, worktree
    }

    enum LandingState: String {
        case pending, landed, abandoned
    }

    /// The root-owned obligation after a child has delivered. This is deliberately observational:
    /// it never extends a claim or participates in dispatch arbitration.
    struct Landing: Equatable {
        let state: LandingState
        let target: String?
        let delivery: String?
        let ownerRootKey: String
        let since: Date
        let commit: String?
        let note: String?
        /// When the target commit was verified. `since` remains when the obligation first opened.
        let landedAt: Date?

        /// Durable broker evidence captured only after resolving both objects in the task's
        /// project repository and proving the commit is contained by the named local target.
        /// Their absence on legacy rows is intentional fail-closed compatibility.
        let verificationOrigin: String?
        let verifiedCommit: String?
        let verifiedTargetCommit: String?

        init(state: LandingState, target: String?, delivery: String?, ownerRootKey: String,
             since: Date, commit: String?, note: String?, landedAt: Date? = nil,
             verificationOrigin: String? = nil, verifiedCommit: String? = nil,
             verifiedTargetCommit: String? = nil) {
            self.state = state
            self.target = target
            self.delivery = delivery
            self.ownerRootKey = ownerRootKey
            self.since = since
            self.commit = commit
            self.note = note
            self.landedAt = landedAt
            self.verificationOrigin = verificationOrigin
            self.verifiedCommit = verifiedCommit
            self.verifiedTargetCommit = verifiedTargetCommit
        }
    }

    struct LandingVerification: Equatable {
        let origin: String
        let commit: String
        let targetCommit: String
    }

    static func isBrokerVerifiedTargetLanding(_ landing: Landing) -> Bool {
        guard landing.state == .landed, landing.landedAt != nil,
              landing.verificationOrigin == "local_target_branch",
              let commit = landing.commit, !commit.isEmpty,
              landing.verifiedCommit == commit,
              let targetCommit = landing.verifiedTargetCommit, !targetCommit.isEmpty,
              landing.target?.isEmpty == false else { return false }
        return [commit, targetCommit].allSatisfy { id in
            (id.count == 40 || id.count == 64)
                && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }
    }

    /// The checkout is disposable; the branch is the delivery. Repository and cwd are internal
    /// facts needed to operate a monorepo worktree and are stored beside the six public facts.
    struct Worktree {
        var path: String
        var branch: String
        var base: String
        var repository: String
        var cwd: String
        var head: String? = nil
        var commits: Int? = nil
        var dirty: Bool? = nil
        var baseDirty = 0
        var requestedBase = "HEAD"
    }

    enum WorktreeDisposal: Equatable {
        case removeAll, removeTreeKeepBranch, keepEverything
    }

    /// The deletion policy is pure and fail-safe. Missing git facts are not permission to erase.
    static func worktreeDisposal(commits: Int?, dirty: Bool?, headOnBranch: Bool?,
                                 branchExists: Bool) -> WorktreeDisposal {
        guard branchExists, let commits, let dirty, !dirty else { return .keepEverything }
        if commits == 0 {
            return headOnBranch == true ? .removeAll : .keepEverything
        }
        return .removeTreeKeepBranch
    }

    /// Whether an ending may reclaim this task's checkout as one nothing was ever done in.
    ///
    /// ``worktreeDisposal(commits:dirty:headOnBranch:branchExists:)`` already refuses to erase
    /// commits or dirty bytes. This is the guard for the window it cannot see: a child working in
    /// the directory right now that has not written a byte yet. Measured on 2026-08-28 — one
    /// child had committed a minute in and kept everything; its sibling had not, and lost the
    /// checkout *and* the delivery branch while its tab was still `working` inside the deleted
    /// directory.
    ///
    /// **Liveness cannot answer this question**, which is why neither the tab nor the child
    /// process appears below. A session that never reached a prompt is also a live assistant in a
    /// live tab — that *is* the case this reclaim exists for. What separates the two readings is
    /// whether this task's own first message was ever put in front of that session.
    ///
    /// A checkout kept by mistake costs a directory until `cleanup` sweeps it a day later. A
    /// checkout deleted by mistake costs the work inside it, and this app has no copy.
    static func reclaimsEmptyWorktree(_ task: Task, outcome: State) -> Bool {
        guard !childWasSpokenTo(task) else { return false }
        if task.childTerminalId == nil { return true }
        return outcome == .spawnFailed && task.briefedAt == nil
    }

    /// Any receipt that this task's first message and a child ever met, in either direction:
    /// the briefing was accepted, its marker was proved in a transcript, the child answered with
    /// a note, or the line was typed at a composer this app had already seen was ready.
    static func childWasSpokenTo(_ task: Task) -> Bool {
        task.briefedAt != nil
            || task.transcriptProven
            || !task.progress.isEmpty
            || task.progressFileNote != nil
            || task.injectAttempts > 0
    }

    /// A stale value copy may add fields, but it may never move a task backwards or resurrect it.
    /// Internal rather than private so the invariant has a pure unit test.
    static func mayReplaceState(_ current: State, with candidate: State) -> Bool {
        if current == candidate { return true }
        if current.isTerminal { return false }
        if candidate.isTerminal { return true }
        switch (current, candidate) {
        case (.queued, .spawning), (.queued, .briefed), (.spawning, .briefed):
            return true
        default:
            return false
        }
    }

    struct Usage {
        var input = 0
        var output = 0
        var cacheRead = 0
        var cacheWrite = 0
        var total = 0
        var model: String?
        var costUsd: Double?
    }

    struct Verification: Equatable {
        enum Last: String {
            case pass, fail, skipped
        }

        let runs: Int
        let seconds: Int
        let last: Last
        let scope: String
    }

    /// One claim path given back early through `claims/release`, and when — see
    /// `Orchestrator.releaseClaims`. `Task.claimKeys` stays the full original reservation;
    /// `Task.activeClaimKeys` is what this subtracts from it.
    struct ReleasedClaim: Equatable {
        let path: String
        let releasedAt: Date
    }

    /// Durable state for the terminal completion postman. A terminal task outcome and this
    /// envelope are written in the same orchestrator-store snapshot before any terminal API is
    /// called. `delivered` means only that the terminal transport accepted the line; observation
    /// and acknowledgement remain separate facts until the root explicitly ACKs `noticeID`.
    enum CompletionDeliveryState: String, Equatable {
        case pending, delivered, acknowledged
        case deadLetter = "dead_letter"
    }

    enum CompletionFailureCode: String, Equatable {
        case rootMissing = "root_missing"
        case rootChoosing = "root_choosing"
        case itermModal = "iterm_modal"
        case terminalTimeout = "terminal_timeout"
        case identityStale = "identity_stale"
        case transportFailed = "transport_failed"
        case acknowledgementTimeout = "acknowledgement_timeout"
    }

    struct CompletionFailure: Equatable {
        let code: CompletionFailureCode
        let message: String
        let at: Date
    }

    struct CompletionDelivery: Equatable {
        let noticeID: String
        let created: Date
        var state: CompletionDeliveryState
        var attempts: Int
        var nextRetryAt: Date?
        var lastAttemptAt: Date? = nil
        var transportDeliveredAt: Date? = nil
        var observedAt: Date? = nil
        var acknowledgedAt: Date? = nil
        var lastError: CompletionFailure? = nil
        var deadLetterAt: Date? = nil
        var legacyReconciled = false
        /// Process-local eligibility barrier. New envelopes stay false until the exact registry
        /// snapshot containing them reaches disk; decoded envelopes default true.
        var persisted: Bool

        init(noticeID: String, created: Date, state: CompletionDeliveryState,
             attempts: Int, nextRetryAt: Date?, lastAttemptAt: Date? = nil,
             transportDeliveredAt: Date? = nil, observedAt: Date? = nil,
             acknowledgedAt: Date? = nil, lastError: CompletionFailure? = nil,
             deadLetterAt: Date? = nil, legacyReconciled: Bool = false,
             persisted: Bool = true) {
            self.noticeID = noticeID
            self.created = created
            self.state = state
            self.attempts = attempts
            self.nextRetryAt = nextRetryAt
            self.lastAttemptAt = lastAttemptAt
            self.transportDeliveredAt = transportDeliveredAt
            self.observedAt = observedAt
            self.acknowledgedAt = acknowledgedAt
            self.lastError = lastError
            self.deadLetterAt = deadLetterAt
            self.legacyReconciled = legacyReconciled
            self.persisted = persisted
        }
    }

    enum CompletionTransportResult: Equatable {
        case delivered
        case failed(CompletionFailureCode, String)
    }

    struct Task {
        let id: String
        var state: State
        var kind: String
        var title: String
        var assistant: Assistant
        /// The model the child was started on, when the task named one. Nil means whatever that
        /// assistant defaults to, which is the answer for most tasks and all older records.
        var model: String?
        var reasoningEffort: ReasoningEffort?
        /// How far the child may go before stopping to ask — what was actually used, after this
        /// Mac's ceiling was applied to what the task asked for.
        var permission = Permission.ask
        var projectDir: String
        var timeoutMinutes: Int
        var created: Date
        var spawnedAt: Date?
        var briefedAt: Date?
        var finishedAt: Date?
        /// Set only after `result.json` passed the task-secret check. A terminal state reported by
        /// `/complete` may exist without it, which is why this is not derived from `finishedAt`.
        var resultVerifiedAt: Date?
        var rootSessionId: String?
        /// Which assistant owns `rootSessionId`. Older registry rows predate the field and are
        /// Claude roots, so nil retains that spelling rather than making them unresolvable.
        var rootAssistant: Assistant?
        var rootLabel: String?
        /// How far from the person at the keyboard this task is: `1` for one a human's session
        /// dispatched, `2` for one dispatched by a child of theirs. Stored rather than derived —
        /// the parent may be over and gone by the time anybody asks, and the answer should not
        /// change when it goes.
        var depth = 1
        /// The task whose child dispatched this one, when it said so. Nil at depth 1, and nil at
        /// depth 2 when the parent was recognised by session id instead.
        var parentTaskId: String?
        /// The `spawn_failed` task this one was retried from, when it was. Nil for every
        /// ordinary dispatch. Recorded so the chain is visible in the registry rather than being
        /// three unrelated-looking tasks with the same title.
        var respawnOf: String?
        /// How far down a respawn chain this task sits: `0` for an original, `1` for the retry of
        /// one, `2` for the retry of that. It is a description of where this task came from and
        /// nothing more: ``Orchestrator/respawnLimit`` is enforced over the whole family
        /// descending from one original, by ``Orchestrator/respawnFamily(of:)``, because a depth
        /// held on the retried task is a number no respawn ever updates.
        var respawnGeneration = 0
        /// The whole graph this task is one node of, in the dispatcher's own words. Carried into
        /// the briefing so a leaf knows what its output feeds — which is the difference between
        /// a usable answer and an essay.
        var plan: String?
        /// Present only for work created from a schedule file. The public registry exposes the
        /// id; the two policy values stay internal so an edit to the source file cannot rewrite
        /// what should happen to a task already in flight.
        var scheduleID: String?
        var scheduleCloseTab = ScheduleCloseTab.onSuccess
        var scheduleNotifyFailure = true
        /// Machine-global operation names acquired together when this task leaves `queued`.
        /// A queued task holds none; a spawning or briefed task holds every name in this list.
        var serialize: [String] = []
        /// Paths this task may write, relative to `projectDir`. Unlike `serialize`, these are
        /// reserved from registration (including while queued) until every terminal outcome.
        var claims: [String] = []
        /// Whether `claims` was present in task.json. An empty declared set means read-only;
        /// absence means the write set is unknown, so L1 must retain its directory warning.
        var claimsDeclared = false
        /// Absolute, canonical-root comparison keys frozen when the lease is registered. These
        /// are persisted because a claim must not change identity as its target comes into being.
        var claimKeys: [String] = []
        /// Claim keys given back early through `claims/release`, each with when it happened.
        var releasedClaims: [ReleasedClaim] = []
        /// Claim keys still actually held: `claimKeys` minus anything already given back. This
        /// is what dispatch-time arbitration and L1's disjoint-claims silence compare against;
        /// `claimKeys` itself remains the full original reservation for the record and the audit.
        var activeClaimKeys: [String] {
            guard !releasedClaims.isEmpty else { return claimKeys }
            let released = Set(releasedClaims.map(\.path))
            return claimKeys.filter { !released.contains($0) }
        }
        /// Claimed paths the terminal-state audit found untouched — see
        /// `Orchestrator.untouchedClaims`. Purely observational, computed once at finalize and
        /// persisted so a later read shows the same verdict rather than re-checking a
        /// filesystem that has since moved on.
        var untouchedClaims: [String] = []
        /// A root's durable declaration that delivered work is waiting to land, has landed, or
        /// was abandoned. This is a signpost for other roots, never a write-path lease.
        var landing: Landing?
        /// What this session has said it is doing since it was briefed, oldest first, newest
        /// ``Orchestrator/progressKept`` kept. The title is fixed at dispatch; this is not.
        var progress: [ProgressNote] = []
        /// The last note collected from the task directory's `progress.json` — the file half
        /// of the progress channel, for a child whose sandbox cannot reach loopback. Persisted
        /// so a beat cannot replay the file's old sentence after a newer note arrived over
        /// HTTP, and a restart cannot replay it either.
        var progressFileNote: String?
        var isolation = Isolation.none
        var worktree: Worktree?
        /// The existing standing Session this task was delivered into. Nil means Clawdline
        /// opened `childTerminalId` for this task and therefore owns that tab's lifecycle.
        var attachSessionId: String?
        var childTerminalId: String?
        var childBackend: Backend?
        /// Whether this task's terminal was actually launched with access to the whole task
        /// root. Persisted because depth settings can change while the tab remains standing.
        var childTaskRootAccess = false
        var childTTY: String?
        var childPID: Int32?
        var childProcStart: Date?
        var childSessionId: String?
        var transcriptPath: String?
        /// The task marker was observed in `transcriptPath`. Persisted because that fact does not
        /// expire when Claude rotates the file or the file is temporarily unavailable.
        var transcriptProven = false
        var secretHash: String
        /// Agent-authored pushes already accepted for this task. Unlike the process-wide hourly
        /// brake, this survives a restart: a task gets five messages for its whole lifetime, not
        /// five every time the app is rebuilt.
        var notifyCount = 0
        /// The task secret encrypted with a key local to this installation. It exists only while
        /// a serialized task waits, so a restart can resume the queue without storing plaintext.
        var queuedSecret: String?
        var summary: String?
        var artifacts: [String] = []
        var usage: Usage?
        var verification: Verification?
        var injectAttempts = 0
        /// The most recent time the first message was handed to the terminal. In memory only:
        /// a process restart loses the plaintext secret and fails every spawning task anyway.
        var lastInjectAt: Date?
        /// The registry answer already sampled by the temporary legacy comparison. In memory
        /// only, so a restart may compare once more without imposing per-beat transcript I/O.
        var registryControlSessionID: String?
        /// Whether the one menu decision this task is allowed to make has already been made —
        /// either the default was taken on a tab this app opened, or the menu was recognised as
        /// somebody else's and left alone. See ``Orchestrator/menuStep(task:choosing:)``.
        var answeredMenu = false
        /// When the child's terminal was last seen in a reading — the difference between a child
        /// that finished and one whose tab was closed under it.
        var lastSeenChild: Date?
        /// When the child's terminal is due to be closed, once it has reported.
        ///
        /// **Written down, because three minutes is longer than this app stays running.**
        /// It used to live in memory only, on the principle that a tab should be closed on the
        /// strength of what this process saw rather than what a previous one believed — and the
        /// principle is right, but the deadline was never the belief. `./build.sh` replaces the
        /// app several times an hour; every child that reported inside the last three minutes
        /// lost its deadline with the process, and nothing ever set one again. Of the eighteen
        /// tabs left standing since the linger was written, seventeen had the app restart inside
        /// their three minutes and the eighteenth had not run out yet — Claude Code's tabs and
        /// Codex's alike, which is why this was not the assistant-specific fault it looked like.
        ///
        /// What crosses the restart is the deadline and nothing else. Whether that tab is still
        /// the child's is asked again, of a reading this process took — see ``closeStep``.
        var closeAt: Date?
        /// The idempotent completion envelope. Nil on live tasks, manual-poll tasks and legacy
        /// terminal rows which have not yet passed bounded reconciliation.
        var completionDelivery: CompletionDelivery?
        /// Why an automatic terminal cleanup is deliberately still pending. The kind is durable:
        /// an iTerm modal may be tried once after a fresh list proves recovery, while a process
        /// scan/still-running/tmux failure must stay stopped for a person instead of sending the
        /// quit word and signals again on every five-second beat.
        var terminalIntervention: TerminalIntervention?
        /// When the task-owned heavyweight `work/` directory may be reclaimed. Nil means either
        /// there is nothing left to do or the `-1` setting leaves it to the 24-hour root sweep.
        var workCleanupAt: Date?
        /// When the isolated checkout's build output may be reclaimed. Nil for every task without
        /// a worktree of its own: the build directory of a shared checkout belongs to the person
        /// working in it, and this deadline must never be able to name it.
        ///
        /// Separate from ``workCleanupAt`` because the two directories are on opposite sides of
        /// the repository line — `work/` is Clawdline's own scratch under `/tmp`, this is a
        /// gitignored directory inside somebody's checkout — and because whole-worktree disposal
        /// is not allowed to be the only thing that frees it. `.build/` regenerates from the
        /// source; the source and the branch are the delivery and are never touched here.
        var buildCleanupAt: Date?

        var dir: URL { Orchestrator.root.appendingPathComponent(id, isDirectory: true) }
    }

    enum TerminalInterventionKind: String {
        case iTermModal = "iterm_modal"
        case terminal = "terminal"
    }

    struct TerminalIntervention: Equatable {
        let kind: TerminalInterventionKind
        let message: String
    }

    // MARK: - A handoff envelope

    enum HandoffState: String {
        case opening, delivered
        case spawnFailed = "spawn_failed"

        var isTerminal: Bool { self != .opening }
    }

    /// The durable postman's memory. It deliberately contains no assistant transcript, package
    /// path, terminal id, model, or byte from handoff.md; those are delivery mechanics rather
    /// than the envelope the protocol permits the registry to remember.
    struct HandoffEnvelope {
        let id: String
        let projectDir: String
        let title: String?
        let fromSession: String?
        let created: Date
        var state: HandoffState

        var dir: URL {
            Orchestrator.handoffRoot.appendingPathComponent(id, isDirectory: true)
        }
    }

    struct HandoffDraft: Equatable {
        let id: String
        let projectDir: String
        let assistant: Assistant
        let model: String?
        let title: String?
        let fromSession: String?
    }

    enum HandoffDraftOutcome: Equatable {
        case ok(HandoffDraft)
        case bad(String)

        var isBad: Bool {
            if case .bad = self { return true }
            return false
        }
    }

    /// Everything needed only while the line is in flight. Losing this on restart is deliberate:
    /// the durable row prevents a second tab; startup settles an interrupted opening as failed.
    private struct HandoffDelivery {
        let id: String
        let assistant: Assistant
        let model: String?
        let terminalID: String
        let backend: Backend
        let spawnedAt: Date
        var attempts = 0
        var lastInjectAt: Date?
        var answeredMenu = false
    }

    /// The directory whose assistant-owned records belong to this child. Kept separate from the
    /// dispatch project so task isolation can choose a different working tree at this seam.
    static func cwd(of task: Task) -> String {
        task.worktree?.cwd ?? task.projectDir
    }

    enum BriefingDecision: Equatable {
        case send, wait, accepted, exhausted
    }

    /// Process and registry facts assembled for one identity decision. The process start is read
    /// from `pid` itself, so the pair cannot combine two generations of the same terminal.
    struct ChildObservation: Equatable {
        var pid: Int32?
        var procStart: Date?
        var registrySessionID: String? = nil
        var registryTranscript: URL? = nil
    }

    enum IdentityStep: Equatable {
        case none
        case useRegistry(sessionID: String, transcript: URL)
        case refuseForeignProcess(seen: Int32?)
    }

    struct IdentityComparison: Equatable {
        var registrySessionID: String
        var registryTranscriptPath: String
        var legacySessionID: String?
        var legacyTranscriptPath: String?
    }

    /// Once a Claude process pair has been recorded, a later process on the same tab cannot
    /// contribute identity. An incomplete pair is unverifiable and therefore fails closed.
    static func identityStep(for task: Task, seeing observation: ChildObservation) -> IdentityStep {
        guard task.assistant == .claude else { return .none }
        if let recordedPID = task.childPID {
            guard observation.pid == recordedPID else {
                return .refuseForeignProcess(seen: observation.pid)
            }
            guard let recordedStart = task.childProcStart, let seenStart = observation.procStart,
                  abs(seenStart.timeIntervalSince(recordedStart))
                    <= SessionRegistry.startTolerance else {
                return .refuseForeignProcess(seen: observation.pid)
            }
        }
        if let sessionID = observation.registrySessionID,
           let transcript = observation.registryTranscript {
            return .useRegistry(sessionID: sessionID, transcript: transcript)
        }
        return .none
    }

    /// Record only a start time read directly from the pid beside it. A partial pair would make
    /// every later strict comparison either too permissive or permanently reject the real child.
    static func recordProcessIdentity(from observation: ChildObservation, in task: inout Task)
        -> Bool {
        guard task.childPID == nil, task.childProcStart == nil,
              let pid = observation.pid, let started = observation.procStart else { return false }
        task.childPID = pid
        task.childProcStart = started
        return true
    }

    /// A transcript can legitimately be absent while both sources already name the same session.
    /// Compare the durable identity, while retaining both paths as evidence when the ids diverge.
    static func identityComparison(registrySessionID: String, registryTranscript: URL,
                                   legacyTask: Task) -> IdentityComparison? {
        guard registrySessionID != legacyTask.childSessionId else { return nil }
        return IdentityComparison(registrySessionID: registrySessionID,
                                  registryTranscriptPath: registryTranscript.path,
                                  legacySessionID: legacyTask.childSessionId,
                                  legacyTranscriptPath: legacyTask.transcriptPath)
    }

    /// Once the briefing receipt has proved this pair, a later registry answer describes a
    /// `/clear` or parked conversation in the same process, not a correction to this task.
    static func adoptRegistryIdentity(sessionID: String, transcript: URL,
                                      in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven) else { return false }
        guard task.childSessionId != sessionID || task.transcriptPath != transcript.path
        else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = transcript.path
        task.transcriptProven = false
        return true
    }

    /// A current hook may correct a provisional pair only when its note is no older than this
    /// spawn and the recorded Claude process still matches this beat; the caller enforces both
    /// before reaching this seam. The first note remains a fallback when process inspection is
    /// unavailable, but an unverified later note never replaces identity. A correction drops the
    /// old provisional path so locate can resolve the new session. Once the briefing receipt pins
    /// the pair, even the same process may be describing `/clear` or a parked chat.
    static func adoptHookIdentity(sessionID: String, in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven),
              task.childSessionId != sessionID else { return false }
        let isFirstIdentity = task.childSessionId == nil && task.transcriptPath == nil
        let hasProcessIdentity = task.childPID != nil && task.childProcStart != nil
        guard isFirstIdentity || hasProcessIdentity else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = nil
        task.transcriptProven = false
        return true
    }

    /// The transition control samples each distinct registry answer once. The flag is transient,
    /// so a restart earns one fresh comparison without restoring per-beat I/O.
    static func beginRegistryControl(for sessionID: String, in task: inout Task) -> Bool {
        guard task.registryControlSessionID != sessionID else { return false }
        task.registryControlSessionID = sessionID
        return true
    }

    static let briefingAttemptLimit = 5
    static let briefingReceiptDelay: TimeInterval = 15

    /// Whether the assistant has drawn the empty composer that can accept a new turn.
    ///
    /// This is deliberately narrower than `SessionState.idle`. That state is the absence of a
    /// recognised menu or live line; a startup banner while slow MCP servers are still loading
    /// has exactly that absence. The composer is positive evidence that startup has completed.
    static func briefingInputReady(_ screen: String?, assistant: Assistant) -> Bool {
        guard let screen, !screen.isEmpty else { return false }
        let text = Ansi.plain(screen)
        guard SessionState.read(text, assistant: assistant) == .idle else { return false }
        // Claude Code puts U+00A0 between its caret and the composer, not a space: a real capture
        // reads `❯\u{00A0}` when empty and `❯\u{00A0}/deploy` with a draft in it. Trimming does
        // hide that — `.whitespaces` is Unicode Zs, which U+00A0 belongs to — but only at the ends
        // of a line, so the bare-caret test below passes while a prefix written with a plain space
        // never fires at all. Fold it first, once, rather than spell it into every comparison.
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.replacingOccurrences(of: "\u{00A0}", with: " ")
                     .trimmingCharacters(in: .whitespaces) }

        switch assistant {
        case .claude:
            // Claude's empty composer is either a bare caret or carries its grey `Try "…"`
            // suggestion — the bundle renders it as `Try "` around a bolded example, so a brand
            // new session is far more likely to show that than a bare caret. A submitted message
            // is echoed after the same caret, so accepting an arbitrary suffix here would turn
            // the receipt check below back into a race.
            return lines.contains { $0 == "❯" || $0.hasPrefix("❯ Try \"") }
        case .codex:
            // Codex writes sent messages after the same glyph; its empty composer is the one
            // whose placeholder says what can be entered, not merely any `›` in the scrollback.
            return lines.contains { $0 == "›" || $0.hasPrefix("› Ask Codex to do anything") }
        }
    }

    /// The words in ``firstLine(id:secret:announce:)`` that say what the session is, with the
    /// task id left off.
    ///
    /// Two things read it and they read it for different reasons. The delivery receipt below
    /// wants it *with* a particular task's id after it, which is what proves a transcript
    /// belongs to that task. ``StartPoints/front(inText:limit:)`` wants it without one, because
    /// the question there is only whether this app opened the session at all — a list of
    /// conversations to pick back up is a list of the ones you had, and a child is this app's
    /// own plumbing.
    ///
    /// Not shared with `firstLine` itself, which still spells the sentence out where it is
    /// written. The test *"the mark the list filters on is the line a child is actually given"*
    /// is what keeps the two from drifting apart.
    static let briefingMark = "Clawdline CHILD agent for task"

    /// The same words with the clause `firstLine` opens on, which is what a child's very first
    /// turn *begins* with.
    ///
    /// The narrower one above can appear anywhere in a user turn, and a receipt wants that: it is
    /// looking for delivery, and a briefing that arrived after something else was typed still
    /// arrived. A list of conversations wants the opposite. This conversation's own transcript
    /// opens with a sentence asking why the matching lives in `Orchestrator` — a question about
    /// the mark, containing the mark — and under the loose test it would have filtered itself out
    /// of the list somebody was reading it in.
    static let briefingOpening = "You are a " + briefingMark

    /// The task marker in a user turn is the delivery receipt. Looking for this specific turn,
    /// rather than any user-shaped bookkeeping row, also proves that the transcript belongs to
    /// this task before it is allowed to close the retry gate.
    static func transcriptContainsBriefing(_ transcript: String?, assistant: Assistant,
                                           taskID: String) -> Bool {
        guard let transcript else { return false }
        let marker = "\(briefingMark) \(taskID)"
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(marker)
        }
    }

    static func transcriptContainsHandoff(_ transcript: String?, assistant: Assistant,
                                          handoffID: String) -> Bool {
        guard let transcript else { return false }
        let line = handoffLine(id: handoffID)
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(line)
        }
    }

    /// A first send needs only a ready composer. Every retry additionally needs a known
    /// transcript, exactly as a dispatched briefing does: absence is not evidence of rejection.
    static func handoffRetryAllowed(attempts: Int, transcriptKnown: Bool) -> Bool {
        attempts == 0 || transcriptKnown
    }

    enum TranscriptOwnership: Equatable {
        case belongs, other, unavailable
    }

    private static let ownershipLock = NSLock()
    private static var ownershipCache: [String: (signature: String,
                                                  ownership: TranscriptOwnership)] = [:]
    private static let ownershipCacheLimit = 1_024
    /// A child is a fresh conversation and the briefing is its first user turn. One MiB leaves
    /// ample room for startup bookkeeping while putting a hard ceiling on every main-thread
    /// ownership check.
    private static let ownershipScanBytes = 1_048_576

    /// A guessed or restored path becomes identity only when the child's own first turn names
    /// this task. Timestamps narrow the files worth opening; they do not prove ownership.
    static func transcriptOwnership(_ url: URL, assistant: Assistant,
                                    taskID: String) -> TranscriptOwnership {
        let key = "\(assistant.rawValue)\u{0}\(taskID)\u{0}\(url.path)"
        let signature = Transcript.signature(of: url)
        ownershipLock.lock()
        if let cached = ownershipCache[key], cached.signature == signature {
            ownershipLock.unlock()
            return cached.ownership
        }
        ownershipLock.unlock()

        guard let handle = try? FileHandle(forReadingFrom: url) else { return .unavailable }
        defer { try? handle.close() }
        let data: Data
        do {
            data = try handle.read(upToCount: ownershipScanBytes) ?? Data()
        } catch {
            return .unavailable
        }
        let marker = Data("\(briefingMark) \(taskID)".utf8)
        var from = data.startIndex
        while from < data.endIndex,
              let hit = data.range(of: marker, in: from..<data.endIndex) {
            var low = hit.lowerBound
            while low > data.startIndex, data[data.index(before: low)] != 0x0A {
                low = data.index(before: low)
            }
            var high = hit.upperBound
            while high < data.endIndex, data[high] != 0x0A { high = data.index(after: high) }
            if let row = String(data: data[low..<high], encoding: .utf8),
               transcriptContainsBriefing(row, assistant: assistant, taskID: taskID) {
                cacheOwnership(.belongs, key: key, signature: signature)
                return .belongs
            }
            from = hit.upperBound
        }
        let text = String(decoding: data, as: UTF8.self)
        if Transcript.containsUserTurn(text, assistant: assistant) {
            // The briefing is the fresh child's first user turn. Finding another completed user
            // turn in the bounded prefix is positive disproof even when a long file continues;
            // its expected marker cannot first appear beyond a conversation already in progress.
            cacheOwnership(.other, key: key, signature: signature)
            return .other
        }
        // An empty file, startup metadata, or a prefix cut before any complete user row says
        // nothing about ownership. Cache that answer by signature so an unchanged large prefix
        // is not reread every polling beat; growth naturally invalidates it.
        cacheOwnership(.unavailable, key: key, signature: signature)
        return .unavailable
    }

    private static func cacheOwnership(_ ownership: TranscriptOwnership, key: String,
                                       signature: String) {
        ownershipLock.lock(); defer { ownershipLock.unlock() }
        if ownershipCache[key] == nil, ownershipCache.count >= ownershipCacheLimit,
           let oldest = ownershipCache.keys.first {
            ownershipCache.removeValue(forKey: oldest)
        }
        ownershipCache[key] = (signature, ownership)
    }

    static func transcriptBelongsToTask(_ url: URL, assistant: Assistant,
                                        taskID: String) -> Bool {
        if case .belongs = transcriptOwnership(url, assistant: assistant, taskID: taskID) {
            return true
        }
        return false
    }

    /// Pure policy for the asynchronous hand-off. A retry needs all three facts: enough time has
    /// passed, the named transcript still lacks this task's turn, and the empty composer is back.
    /// Claude and Codex append the user turn before beginning it, so an accepted first send puts
    /// the marker in the transcript before it can execute. That closes this gate before another
    /// copy can be sent; a missing transcript alone is never grounds for a retry.
    static func briefingDecision(screen: String?, assistant: Assistant, transcript: String?,
                                 transcriptKnown: Bool, taskID: String, attempts: Int,
                                 secondsSinceAttempt: TimeInterval?) -> BriefingDecision {
        if transcriptContainsBriefing(transcript, assistant: assistant, taskID: taskID) {
            return .accepted
        }
        guard briefingInputReady(screen, assistant: assistant) else { return .wait }
        if attempts == 0 { return .send }
        guard transcriptKnown else { return .wait }
        if let secondsSinceAttempt, secondsSinceAttempt < briefingReceiptDelay { return .wait }
        return attempts >= briefingAttemptLimit ? .exhausted : .send
    }

    /// What a route handler gets back; `RemoteServer` turns it into a `Response`.
    enum Reply {
        case ok([String: Any])
        case refused(status: Int, code: String, message: String, extra: [String: Any])

        static func refused(_ status: Int, _ code: String, _ message: String) -> Reply {
            .refused(status: status, code: code, message: message, extra: [:])
        }
    }

    // MARK: - Where everything lives

    static let root = URL(fileURLWithPath: "/tmp/.clawdline", isDirectory: true)
    static var scheduleDirectoryOverrideForTesting: URL?
    static var scheduleDirectory: URL {
        scheduleDirectoryOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("schedules", isDirectory: true)
    }
    static var handoffRootOverrideForTesting: URL?
    static var handoffRoot: URL {
        handoffRootOverrideForTesting
            ?? root.appendingPathComponent("handoffs", isDirectory: true)
    }

    static var storeURLOverrideForTesting: URL?
    static var storeURL: URL {
        storeURLOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("orchestrator.json")
    }
    static var tokenURL: URL { RemoteAuth.directory.appendingPathComponent("orchestrator-token") }
    static var archiveKeyURL: URL {
        RemoteAuth.directory.appendingPathComponent("orchestrator-archive-key")
    }

    private static let lock = NSLock()
    /// Opening a terminal is bounded, not cheap: iTerm automation alone may wait 15 seconds.
    /// Pumps arrive from main-thread finalization and startup as well as the remote server, so
    /// they get the same off-main serial shape as ordinary dispatch without competing pumps.
    private static let serializePumpQueue = DispatchQueue(
        label: "dev.sainteye.clawdline.orchestrator.serialize")
    /// Worktree inspection and disposal can each wait on several bounded git subprocesses.
    /// Keeping them on one utility queue both keeps the panel responsive and prevents two close
    /// paths from racing to dispose the same checkout.
    private static let worktreeQueue = DispatchQueue(
        label: "dev.sainteye.clawdline.orchestrator.worktree", qos: .utility)
    /// A background pump and a new remote dispatch may both persist. Serializing the whole
    /// snapshot-and-write prevents an older snapshot from winning the atomic rename last.
    private static let storeSaveLock = NSLock()
    /// Terminal delivery leaves the registry lock before it types. Serializing coordination
    /// mutations closes the otherwise possible gap where two retries both see a missing receipt.
    private static let coordinationDeliveryLock = NSLock()
    private static let completionDeliveryQueue = DispatchQueue(
        label: "dev.sainteye.clawdline.orchestrator.completion", qos: .utility)
    static let completionAttemptLimit = 8
    static let completionRetryMaximum: TimeInterval = 300
    static let legacyCompletionLookback: TimeInterval = 7 * 24 * 3600
    static let legacyCompletionBatchLimit = 25
    private static var loaded = false
    private static var tasks: [String: Task] = [:]
    private static var handoffs: [String: HandoffEnvelope] = [:]
    private static var handoffDeliveries: [String: HandoffDelivery] = [:]
    private static var coordinationWaits: [String: CoordinationWait] = [:]
    /// Root terminal id → the last turn that root explicitly delivered. Unlike a child result,
    /// this receipt is consumed when the same tab begins another observed turn.
    private static var sessionDeliveries: [String: SessionDelivery] = [:]
    private static var sessionSelfStates: [String: SessionSelfState] = [:]
    /// How many `beat` walks are inside the loop, and which walk this is. Both exist to catch the
    /// overlap that should not be possible; neither changes what a walk does.
    private static var beatsInFlight = 0
    private static var beatSequence = 0
    /// Plaintext secrets, held only between dispatch and briefing. Never on disk.
    private static var secrets: [String: String] = [:]
    private static var dispatchTimes: [Date] = []
    /// Accepted agent-authored notifications in the last hour. Deliberately separate from the
    /// dispatch brake: telling somebody what a child found must not consume a child slot ticket.
    private static var notifyTimes: [Date] = []
    /// Failed task-secret attempts share the public pairing route's three-in-ten-minutes shape.
    /// Successful notifications never enter this window.
    private static var notifyCredentialFailureTimes: [Date] = []
    /// A skipped or missed occurrence has no task row to remember it. This prevents one audit and
    /// push per minute while the process stays up; a restart deliberately re-evaluates catch-up.
    private static var handledScheduleFires: [String: Date] = [:]
    private static var pendingScheduleFires: [String: Date] = [:]
    private static var lastMissedScheduleFires: [String: Date] = [:]
    private static var dispatchingSchedules: Set<String> = []
    private static var invalidScheduleFingerprints: [String: String] = [:]
    /// When the last ten schedules were written through the HTTP route — see
    /// ``takeScheduleWriteRate()``.
    private static var scheduleWriteTimes: [Date] = []
    private static let scheduleQueue = DispatchQueue(
        label: "dev.sainteye.clawdline.orchestrator.schedules", qos: .utility)
    static var scheduleRunnerForTesting: ((Schedule) -> Reply)?
    static var scheduleDispatchEnqueuerForTesting: ((@escaping () -> Void) -> Void)?
    static var completionPumpEnqueuerForTesting: ((@escaping () -> Void) -> Void)?
    /// Deterministic persistence seam. Returning non-nil replaces the filesystem write with that
    /// result; production leaves it nil. The serialized snapshot is handed over after the task
    /// lock is released so an injected concurrent mutation exercises the real save window.
    static var storeSaveInterceptorForTesting: ((Data) -> Bool?)?
    /// Overrides only the positive ingress evidence. Nil uses current process-bound terminal and
    /// Coordinator facts; an empty array is a deliberate unknown/offline fixture.
    static var rootIdentityEvidenceForTesting: [RootIdentityEvidence]?
    /// Test seam: observes the warning decision before optional terminal delivery.
    static var workspaceOverlapObserverForTesting: ((Task, [WorkspaceOverlap]) -> Void)?
    /// Test receipt for the semantic root-notification boundary. The terminal transport itself
    /// is exercised elsewhere; this proves a terminal path reached finalization and its notice.
    static var rootNotificationObserverForTesting: ((Task) -> Void)?
    static var attachedSenderForTesting: ((String, TargetSession) -> String?)?
    /// The session inventory an attachment resolves against, and the starter a tab-opening
    /// dispatch uses.
    ///
    /// Production reads `SessionWatch` and opens a real terminal; a suite can do neither, which
    /// is how everything past ``attachmentDecision(sessionID:assistant:sessions:states:tasks:roles:isChoosing:excluding:)``
    /// — `spawnAttached`, `502 attach_delivery_failed`, the single-flight check the serialize
    /// pump re-runs, and every tab-opening failure at dispatch — came to have no test that could
    /// go red. Both are cleared by ``forget()``.
    static var attachmentInventoryForTesting: ([TargetSession], [String: SessionState])?
    static var taskStarterForTesting: TaskStarter?
    /// Test seam for the final display sentence; production always enters WebPush below.
    static var agentPushForTesting:
        ((String, String, String, String?, String?) -> WebPush.Delivery)?
    /// Child terminal id → task title, rebuilt whenever the tasks change. Read on every redraw
    /// of every session row, which is why it is a dictionary and not a walk over the tasks.
    private static var titlesByTerminal: [String: String] = [:]
    /// Handoff tabs are roots, not task roles, but still keep the protocol's requested label.
    private static var handoffTitlesByTerminal: [String: String] = [:]

    /// Terminal id → where that tab sits in the tree. Rebuilt beside ``titlesByTerminal``.
    private static var rolesByTerminal: [String: Role] = [:]

    /// Where a session sits in the tree, for anything that has to decide who to tell.
    ///
    /// **Nil is the definition of a root.** A session this app did not open for a task is one a
    /// person opened for themselves, and a person is the only audience that gets interrupted for
    /// its own sake. Everything below one is working for somebody, and that somebody is a
    /// program that can be told directly.
    struct Role: Equatable {
        let taskID: String
        /// 1 for a task a person's session dispatched, 2 for one dispatched by a child of theirs.
        let depth: Int
        let title: String
        /// When the task gives up on itself, so a notification can say how long is left to
        /// answer it. Nil before the child has been briefed — there is no clock yet.
        let deadline: Date?
        /// Whether anything is still waiting on this tab. A child's terminal lingers for three
        /// minutes after its task ends (see `orchestratorChildLinger`), and for those three
        /// minutes it is a child's tab with nobody behind it.
        let live: Bool
        /// The launch-time grant, not an inference from the task's current depth setting.
        var taskRootAccess = false
    }

    /// One session blocked on a coordination group. Request delivery and release delivery are
    /// separate receipts: a retry must never type either message twice after it succeeded.
    struct CoordinationWaiter {
        let sessionID: String
        let reason: String
        let created: Date
        var requestDeliveredAt: Date?
        var releaseDeliveredAt: Date?
    }

    /// A release condition shared by every session waiting on the same owner, repository and
    /// canonical path set. It is intentionally independent of ``SessionState``: this means an
    /// agent is waiting on a peer, while `SessionState.waiting` means a person must answer.
    struct CoordinationWait {
        let id: String
        let repository: String
        let paths: [String]
        let ownerSessionID: String
        let releaseCondition: String
        let created: Date
        var waiters: [CoordinationWaiter]
    }

    /// The two quiet overlays a session row may draw without changing its terminal state.
    struct Coordination {
        let waitingOn: [[String: Any]]
        let waitedOnBy: [[String: Any]]
    }

    /// The one user-facing answer every live Session row carries.
    ///
    /// This is a projection, not a replacement for any source axis. `SessionState` still says
    /// what the terminal shows; task results, landings, handoffs, and coordination waits remain
    /// separate durable receipts. Keeping the enum closed makes a missing or future value a
    /// visible `unknown` at the client instead of another kind of blank idle row.
    ///
    /// Each state exists to be looked at by one person, so that after looking they know what to
    /// do next — a value that does not change what the reader does is a field, not a state. The
    /// per-state contract lives in docs/session-states.md. Two names deserve their history:
    /// `waiting_you` was `waiting_human`, renamed because the state is an instruction to the
    /// reader, not a taxonomy of blockers; `unknown` was `needs_triage`, renamed because the
    /// projection's fail-closed default is the *broker's* ignorance and must never read as the
    /// person's to-do — five quiet idle rows once all demanded "triage" when none needed anything.
    enum SessionWorkState: String, CaseIterable {
        case ready, working, holding
        case waitingYou = "waiting_you"
        case waitingSession = "waiting_session"
        case unknown
        case milestoneComplete = "milestone_complete"
        case workComplete = "work_complete"
    }

    struct SessionWorkProjection {
        let state: SessionWorkState
        /// `broker` when the leading state was projected from evidence, `self` when it is the
        /// session's own declared claim. The check states are evidence-only, so they are always
        /// `broker`; a row can therefore show a person the difference between a proven state and
        /// a stated one.
        let provenance: String
        /// One line in the session's own words, behind a self-claimed `ready` or `holding`.
        let note: String?
        /// When the leading evidence was recorded. Self claims and debts carry clocks; live
        /// terminal observations do not, so this is honest and absent rather than invented.
        let since: Date?
        /// Who or what will move this state, and whether that mover is a person. "Your build;
        /// nobody" and "the user's decision; the user" are the same colour of idle and opposite
        /// calls to action.
        let movedBy: String?
        let personNeeded: Bool?
        /// The second axis: an unpaid debt owed to this session's line of work. Independent of
        /// `state` on purpose — the most common real combination is "my main line waits on a
        /// person, my side work proceeds", which a single value cannot spell.
        let owed: [String: Any]?
        /// Only check states carry this. It describes the receipt and its deliberately narrow
        /// task scope; it is never accepted back as a source of truth.
        let disposition: [String: Any]?

        init(state: SessionWorkState, provenance: String = "broker", note: String? = nil,
             since: Date? = nil, movedBy: String? = nil, personNeeded: Bool? = nil,
             owed: [String: Any]? = nil, disposition: [String: Any]?) {
            self.state = state
            self.provenance = provenance
            self.note = note
            self.since = since
            self.movedBy = movedBy
            self.personNeeded = personNeeded
            self.owed = owed
            self.disposition = disposition
        }
    }

    /// Facts proved for the process occupying one terminal *now*. A terminal id alone is not
    /// identity: iTerm and tmux both reuse it after the old process has gone. The conversation
    /// is likewise accepted only from the process-bound Transcript seam used by the public
    /// Session row, never from a tty hook left by an earlier assistant.
    struct SessionWorkIdentity: Equatable {
        var terminalID: String
        var assistant: Assistant?
        var tty: String
        var pid: Int32?
        var processStart: Date?
        var conversationID: String?
    }

    /// A root's authenticated claim that its current turn delivered what it was assigned.
    ///
    /// This is deliberately weaker than a broker-verified landing and therefore produces only
    /// the single-check milestone. `settled` means the report has subsequently been observed at
    /// an idle prompt; the next working/waiting transition consumes it so an older turn cannot
    /// reappear as complete after a newer one ends without reporting.
    struct SessionDelivery {
        let identity: SessionWorkIdentity
        let summary: String
        let reportedAt: Date
        var settled: Bool
    }

    static let sessionDeliverySummaryLimit = 500
    /// One line, the brief's own bound: a note is the session's words on a row, not a report.
    static let sessionSelfNoteLimit = 200

    /// The unpaid half of the two axes: somebody owes this session's line of work an answer,
    /// while the session itself may keep moving. Its failure mode is being forgotten, not being
    /// stale, so `since` is kept from the first declaration and re-declaring the same debt does
    /// not reset the clock.
    struct OwedDebt: Equatable {
        let note: String
        let movedBy: String?
        let personNeeded: Bool
        let since: Date
    }

    /// A session's authenticated declaration about its own quiet state, bound to the exact
    /// process like ``SessionDelivery``. Two independent halves with two lifecycles: `claim`
    /// (`ready` or `holding`) describes one stopped turn and is consumed when the next turn
    /// starts; `owed` survives turns until the session clears it, because a debt that vanished
    /// the moment its owner did side work would be the axis collapse this record exists to fix.
    /// A self declaration may never produce a check state; that boundary is enforced at the
    /// route, in the projection, and by test.
    struct SessionSelfState {
        let identity: SessionWorkIdentity
        var claim: SessionWorkState?
        var note: String?
        var movedBy: String?
        var personNeeded: Bool?
        var claimReportedAt: Date?
        var claimSettled: Bool
        var owed: OwedDebt?
    }

    /// The one process-binding rule shared by every session-scoped self record: exact assistant,
    /// terminal, tty, pid, process start within tolerance, and conversation. Missing fields fail
    /// closed so an old record cannot decorate an unrelated later process.
    static func recordedIdentityMatchesCurrentSession(_ recorded: SessionWorkIdentity,
                                                      identity: SessionWorkIdentity) -> Bool {
        guard let assistant = identity.assistant,
              recorded.assistant == assistant,
              recorded.terminalID == identity.terminalID,
              recorded.tty == identity.tty,
              let recordedPID = recorded.pid, recordedPID == identity.pid,
              let recordedStart = recorded.processStart,
              let currentStart = identity.processStart,
              abs(recordedStart.timeIntervalSince(currentStart))
                <= SessionRegistry.startTolerance,
              let recordedConversation = recorded.conversationID,
              recordedConversation == identity.conversationID else { return false }
        return true
    }

    static func sessionDeliveryMatchesCurrentSession(_ delivery: SessionDelivery,
                                                      identity: SessionWorkIdentity) -> Bool {
        recordedIdentityMatchesCurrentSession(delivery.identity, identity: identity)
    }

    /// A terminal task receipt belongs to the current Session only when every durable child
    /// identity agrees with every process-bound fact. Missing legacy fields fail closed: they
    /// remain useful task history, but cannot put a check on an unrelated later process.
    static func taskMatchesCurrentSession(_ task: Task, identity: SessionWorkIdentity) -> Bool {
        guard let assistant = identity.assistant,
              task.assistant == assistant,
              task.childTerminalId == identity.terminalID,
              task.childTTY == identity.tty,
              let recordedPID = task.childPID, recordedPID == identity.pid,
              let recordedStart = task.childProcStart,
              let currentStart = identity.processStart,
              abs(recordedStart.timeIntervalSince(currentStart))
                <= SessionRegistry.startTolerance,
              task.transcriptProven,
              let recordedConversation = task.childSessionId,
              recordedConversation == identity.conversationID else { return false }
        return true
    }

    /// Select the receipt the projection may use. Contradictory duplicate matches are no more
    /// trustworthy than a missing one, so exact-count-one is part of the fail-closed contract.
    static func taskForCurrentSession(_ candidates: [Task], identity: SessionWorkIdentity) -> Task? {
        let matches = candidates.filter { taskMatchesCurrentSession($0, identity: identity) }
        return matches.count == 1 ? matches[0] : nil
    }

    /// `from_session` has two historical namespaces. Compare each one exactly and independently:
    /// a watched terminal id, or the conversation id proved for the process currently in it.
    /// There is deliberately no prefix, title, tty, or fallback matching here.
    static func handoffSource(_ source: String?, matches identity: SessionWorkIdentity) -> Bool {
        guard let source else { return false }
        let terminalNamespaceMatch = source == identity.terminalID
        let conversationNamespaceMatch = identity.conversationID.map { source == $0 } ?? false
        return terminalNamespaceMatch || conversationNamespaceMatch
    }

    /// Pure precedence shared by production and tests.
    ///
    /// A newly active terminal outranks an older completion receipt. That conflict is possible
    /// during the child's linger (or if somebody keeps using its tab), and continuing to draw a
    /// check would claim the *current* work is over on evidence from the previous phase.
    ///
    /// `selfClaim` is the session's own declared quiet state and is honoured only as `ready` or
    /// `holding` — never a check, and never ahead of a question, a wait, live activity, or a
    /// finished receipt. `holding` in particular has no other entrance and is no branch's
    /// default: it demands a declared next step with a mover that is not a person, because the
    /// old vocabulary's defect was precisely a fallback case (`needs_triage`) that anything
    /// unmatched fell into.
    static func projectSessionWorkState(
        terminalState: SessionState,
        task: Task?,
        hasCoordinationWait: Bool,
        hasOpenHandoff: Bool,
        assignmentKnownAbsent: Bool,
        hasSessionDelivery: Bool = false,
        hasOutstandingChild: Bool = false,
        selfClaim: SessionWorkState? = nil
    ) -> SessionWorkState {
        if terminalState == .waiting { return .waitingYou }
        if hasCoordinationWait { return .waitingSession }
        if terminalState == .unknown { return .unknown }
        if case .working = terminalState { return .working }
        // Waiting on one's own child is waiting on another session, not on an event: a child can
        // wedge, and the reader may have to go and unwedge it. That keeps it out of `holding`.
        if hasOutstandingChild { return .waitingSession }

        if let task {
            if task.state == .success, task.finishedAt != nil {
                if task.landing.map(isBrokerVerifiedTargetLanding) == true,
                   !hasOpenHandoff {
                    return .workComplete
                }
                return .milestoneComplete
            }
            // A finished non-success receipt outranks any self claim: a failure the session
            // talks past is exactly what fail-closed exists for. A still-live assignment falls
            // through — its child may honestly declare its own quiet state mid-task.
            if task.state.isTerminal { return .unknown }
        }
        if hasSessionDelivery { return .milestoneComplete }
        if selfClaim == .ready || selfClaim == .holding { return selfClaim ?? .unknown }
        if assignmentKnownAbsent { return .ready }
        // Idle is intentionally absent from the success rules. A prompt proves that activity
        // stopped; without a matching receipt it proves neither completion nor readiness. And
        // `unknown` means exactly that: the broker has no positive evidence. It is an absence,
        // not a demand — nothing here asks the reader to do anything.
        return .unknown
    }

    /// Broker projection for the process currently occupying a terminal. The role/title index is
    /// presentation-only; terminal reuse means it is never sufficient completion evidence.
    static func sessionWorkProjection(identity: SessionWorkIdentity,
                                      terminalState: SessionState) -> SessionWorkProjection {
        load()
        lock.lock(); defer { lock.unlock() }
        return sessionWorkProjectionLocked(identity: identity, terminalState: terminalState)
    }

    /// The caller owns `lock`. Bearings uses this beside coordination and aggregate facts so a
    /// registry mutation cannot split one response into mutually impossible before/after rows.
    private static func sessionWorkProjectionLocked(identity: SessionWorkIdentity,
                                                    terminalState: SessionState)
        -> SessionWorkProjection {
        let task = taskForCurrentSession(Array(tasks.values), identity: identity)
        let sessionDelivery = sessionDeliveries[identity.terminalID].flatMap {
            sessionDeliveryMatchesCurrentSession($0, identity: identity) ? $0 : nil
        }
        let hasOutstandingChild = tasks.values.contains { child in
            guard !child.state.isTerminal else { return false }
            if let currentTaskID = task?.id, child.parentTaskId == currentTaskID { return true }
            guard let conversation = identity.conversationID,
                  child.rootSessionId == conversation,
                  (child.rootAssistant ?? .claude) == identity.assistant else { return false }
            return true
        }
        let hasWait = coordinationWaits.values.contains { wait in
            wait.waiters.contains { waiter in
                waiter.releaseDeliveredAt == nil
                    && (waiter.sessionID == identity.terminalID
                        || wait.ownerSessionID == identity.terminalID)
            }
        }
        let hasOpenHandoff = handoffs.values.contains {
            $0.state != .delivered && handoffSource($0.fromSession, matches: identity)
        }
        let selfState = sessionSelfStates[identity.terminalID].flatMap {
            recordedIdentityMatchesCurrentSession($0.identity, identity: identity) ? $0 : nil
        }
        let owed: [String: Any]? = selfState?.owed.map { debt in
            var row: [String: Any] = [
                "note": debt.note,
                "since": Int(debt.since.timeIntervalSince1970),
                "person_needed": debt.personNeeded,
                "provenance": "self",
            ]
            if let movedBy = debt.movedBy { row["moved_by"] = movedBy }
            return row
        }

        // A non-assistant prompt is a terminal waiting for a command. For an assistant, absence
        // of a broker task is not proof that its human-authored assignment ended; fail closed.
        let state = projectSessionWorkState(
            terminalState: terminalState, task: task,
            hasCoordinationWait: hasWait, hasOpenHandoff: hasOpenHandoff,
            assignmentKnownAbsent: identity.assistant == nil,
            hasSessionDelivery: sessionDelivery != nil,
            hasOutstandingChild: hasOutstandingChild,
            selfClaim: selfState?.claim)
        // The claim led exactly when leaving it out changes the answer; everything above it in
        // the precedence is broker evidence and keeps `broker` provenance.
        let claimLed = selfState?.claim != nil && state != projectSessionWorkState(
            terminalState: terminalState, task: task,
            hasCoordinationWait: hasWait, hasOpenHandoff: hasOpenHandoff,
            assignmentKnownAbsent: identity.assistant == nil,
            hasSessionDelivery: sessionDelivery != nil,
            hasOutstandingChild: hasOutstandingChild)
        guard state == .milestoneComplete || state == .workComplete else {
            if claimLed, let selfState {
                return SessionWorkProjection(
                    state: state, provenance: "self", note: selfState.note,
                    since: selfState.claimReportedAt, movedBy: selfState.movedBy,
                    personNeeded: selfState.personNeeded, owed: owed, disposition: nil)
            }
            return SessionWorkProjection(state: state, owed: owed, disposition: nil)
        }
        if let delivery = sessionDelivery, task == nil {
            return SessionWorkProjection(state: state, owed: owed, disposition: [
                "scope": "session",
                "title": delivery.summary,
                "evidence": "authenticated_session_delivery",
                "receiptAt": Int(delivery.reportedAt.timeIntervalSince1970),
            ])
        }
        guard let task else {
            return SessionWorkProjection(state: .unknown, owed: owed, disposition: nil)
        }
        var disposition: [String: Any] = [
            "scope": "task",
            "taskId": task.id,
            "title": task.title,
            "evidence": state == .workComplete
                ? "broker_verified_target_landing" : "authenticated_task_delivery",
        ]
        if let finished = task.finishedAt {
            disposition["receiptAt"] = Int(finished.timeIntervalSince1970)
        }
        if state == .workComplete, let landing = task.landing {
            if let commit = landing.verifiedCommit { disposition["commit"] = commit }
            if let target = landing.target { disposition["target"] = target }
            if let targetCommit = landing.verifiedTargetCommit {
                disposition["targetCommit"] = targetCommit
            }
            if let landedAt = landing.landedAt {
                disposition["landedAt"] = Int(landedAt.timeIntervalSince1970)
            }
        }
        return SessionWorkProjection(state: state, owed: owed, disposition: disposition)
    }

    /// Record one root turn's explicit delivery claim. The route supplies identity from the
    /// current watched process rather than accepting it from JSON, and reporting is allowed only
    /// while that turn is visibly active. Children already have the stronger task/result path.
    static func reportSessionDelivery(identity: SessionWorkIdentity,
                                      terminalState: SessionState,
                                      summary rawSummary: String,
                                      now: Date = Date()) -> Reply {
        load()
        guard case .working = terminalState else {
            return .refused(409, "session_not_working",
                            "A root may report delivery only while its current turn is working.")
        }
        guard identity.assistant != nil, identity.pid != nil, identity.processStart != nil,
              identity.conversationID != nil else {
            return .refused(409, "session_unbound",
                            "The current assistant process and conversation could not be bound.")
        }
        let summary = rawSummary.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !summary.isEmpty, summary.count <= sessionDeliverySummaryLimit,
              !summary.unicodeScalars.contains(where: { $0.value == 0 }) else {
            return .refused(400, "bad_request",
                            "summary must be 1–\(sessionDeliverySummaryLimit) characters without NUL.")
        }

        lock.lock()
        if taskForCurrentSession(Array(tasks.values), identity: identity) != nil {
            lock.unlock()
            return .refused(409, "child_session",
                            "A Clawdline child reports through its task result, not this route.")
        }
        if let existing = sessionDeliveries[identity.terminalID],
           sessionDeliveryMatchesCurrentSession(existing, identity: identity),
           existing.summary == summary, !existing.settled {
            let disposition = sessionDeliveryDisposition(existing)
            lock.unlock()
            return .ok(["ok": true, "created": false, "disposition": disposition])
        }
        let made = SessionDelivery(identity: identity, summary: summary,
                                   reportedAt: now, settled: false)
        sessionDeliveries[identity.terminalID] = made
        let disposition = sessionDeliveryDisposition(made)
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.session.delivered", [
            "session": identity.terminalID, "assistant": identity.assistant?.rawValue ?? "?",
        ])
        return .ok(["ok": true, "created": true, "disposition": disposition])
    }

    private static func sessionDeliveryDisposition(_ delivery: SessionDelivery) -> [String: Any] {
        [
            "scope": "session", "title": delivery.summary,
            "evidence": "authenticated_session_delivery",
            "receiptAt": Int(delivery.reportedAt.timeIntervalSince1970),
        ]
    }

    /// One bounded line of the session's own words, or a typed refusal reason via nil.
    private static func selfNote(_ raw: String?) -> String?? {
        guard let raw else { return .some(nil) }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= sessionSelfNoteLimit,
              !text.contains("\n"), !text.unicodeScalars.contains(where: { $0.value == 0 })
        else { return .none }
        return .some(text)
    }

    /// Record a session's declaration about its own quiet state — the `self` half of the
    /// provenance boundary. The route supplies identity from the current watched process, like
    /// ``reportSessionDelivery``, and only while the declaring turn is observably working.
    ///
    /// What may be declared: `claim` of `ready` (an invitation: you can hand this session work)
    /// or `holding` (it moves by itself). `holding` is deliberately hard to enter — it needs the
    /// declared next step in `note`, a `movedBy`, and `personNeeded == false`, because a mover
    /// who is a person or another session makes the truth a wait, not a hold. The check states
    /// are refused by name: a self declaration may never produce ☑︎ or ✅.
    ///
    /// The `owed` half is the second axis and survives turns. Redeclaring the same debt keeps
    /// its original `since` — age is the debt's whole risk, and a clock that reset on every
    /// mention would hide exactly the three-day-old decision this field exists to surface.
    static func declareSessionState(identity: SessionWorkIdentity,
                                    terminalState: SessionState,
                                    claim rawClaim: String?,
                                    note rawNote: String?,
                                    movedBy rawMovedBy: String?,
                                    personNeeded: Bool?,
                                    owed rawOwed: [String: Any]?,
                                    clearOwed: Bool,
                                    now: Date = Date()) -> Reply {
        load()
        guard case .working = terminalState else {
            return .refused(409, "session_not_working",
                            "A session may declare its state only while its current turn is working.")
        }
        guard identity.assistant != nil, identity.pid != nil, identity.processStart != nil,
              identity.conversationID != nil else {
            return .refused(409, "session_unbound",
                            "The current assistant process and conversation could not be bound.")
        }
        var claim: SessionWorkState?
        if let rawClaim {
            switch SessionWorkState(rawValue: rawClaim) {
            case .some(.ready): claim = .ready
            case .some(.holding): claim = .holding
            case .some(.milestoneComplete), .some(.workComplete):
                return .refused(403, "self_completion_refused",
                                "The check states are evidence-only: a session cannot declare "
                                    + "milestone_complete or work_complete about itself.")
            default:
                return .refused(400, "bad_request",
                                "state must be \"ready\" or \"holding\"; the broker projects "
                                    + "every other state from evidence.")
            }
        }
        guard case .some(let note) = selfNote(rawNote) else {
            return .refused(400, "bad_request",
                            "note must be one line of 1–\(sessionSelfNoteLimit) characters.")
        }
        guard case .some(let movedBy) = selfNote(rawMovedBy) else {
            return .refused(400, "bad_request",
                            "moved_by must be one line of 1–\(sessionSelfNoteLimit) characters.")
        }
        if claim == .holding {
            guard note != nil, movedBy != nil, personNeeded == false else {
                return .refused(422, "holding_needs_evidence",
                                "holding requires its declared next step (note), a mover "
                                    + "(moved_by), and person_needed: false. A mover who is a "
                                    + "person or another session is a wait, not a hold.")
            }
        }
        var owed: OwedDebt?
        if let rawOwed {
            guard case .some(let owedNoteValue) = selfNote(rawOwed["note"] as? String),
                  let owedNote = owedNoteValue,
                  case .some(let owedMovedBy) = selfNote(rawOwed["moved_by"] as? String) else {
                return .refused(400, "bad_request",
                                "owed.note (required) and owed.moved_by must each be one line "
                                    + "of 1–\(sessionSelfNoteLimit) characters.")
            }
            owed = OwedDebt(note: owedNote, movedBy: owedMovedBy,
                            personNeeded: rawOwed["person_needed"] as? Bool ?? true,
                            since: now)
        }
        guard claim != nil || owed != nil || clearOwed else {
            return .refused(400, "bad_request",
                            "The declaration is empty: give state, owed, or owed: null.")
        }

        lock.lock()
        let existing = sessionSelfStates[identity.terminalID].flatMap {
            recordedIdentityMatchesCurrentSession($0.identity, identity: identity) ? $0 : nil
        }
        var made = SessionSelfState(
            identity: identity, claim: claim, note: note, movedBy: movedBy,
            personNeeded: personNeeded, claimReportedAt: claim != nil ? now : nil,
            claimSettled: false, owed: nil)
        if clearOwed {
            made.owed = nil
        } else if let owed {
            // The same debt keeps its first clock; only a different note is a new debt.
            if let held = existing?.owed, held.note == owed.note {
                made.owed = OwedDebt(note: owed.note, movedBy: owed.movedBy,
                                     personNeeded: owed.personNeeded, since: held.since)
            } else {
                made.owed = owed
            }
        } else {
            made.owed = existing?.owed
        }
        if claim == nil {
            // An owed-only declaration leaves the current turn's claim half alone.
            made.claim = existing?.claim
            made.note = existing?.note
            made.movedBy = existing?.movedBy
            made.personNeeded = existing?.personNeeded
            made.claimReportedAt = existing?.claimReportedAt
            made.claimSettled = existing?.claimSettled ?? false
        }
        if made.claim == nil, made.owed == nil {
            sessionSelfStates.removeValue(forKey: identity.terminalID)
        } else {
            sessionSelfStates[identity.terminalID] = made
        }
        var payload: [String: Any] = ["ok": true]
        if let claim = made.claim { payload["state"] = claim.rawValue }
        if let debt = made.owed {
            payload["owed"] = ["note": debt.note,
                               "since": Int(debt.since.timeIntervalSince1970)]
        }
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.session.declared", [
            "session": identity.terminalID, "state": claim?.rawValue ?? "-",
            "owed": made.owed == nil ? "none" : "held",
        ])
        return .ok(payload)
    }

    /// Advance the one-turn receipt lifecycle from observed terminal transitions. The first idle
    /// after reporting arms consumption; the next active state removes the old receipt before a
    /// later idle prompt could display it again.
    ///
    /// A self-declared claim (`ready`/`holding`) lives on the same clock: it described one
    /// stopped turn, and the turn after it starts a different story. The `owed` half is exempt
    /// on purpose — a debt is not consumed by its owner doing side work; only an explicit
    /// declaration (or a different process in the terminal) clears it.
    static func noteSessionStateChange(terminalID: String, to state: SessionState) {
        load()
        var changed = false
        lock.lock()
        if var delivery = sessionDeliveries[terminalID] {
            switch state {
            case .idle where !delivery.settled:
                delivery.settled = true
                sessionDeliveries[terminalID] = delivery
                changed = true
            case .working where delivery.settled, .waiting where delivery.settled:
                sessionDeliveries.removeValue(forKey: terminalID)
                changed = true
            default:
                break
            }
        }
        if var selfState = sessionSelfStates[terminalID], selfState.claim != nil {
            switch state {
            case .idle where !selfState.claimSettled:
                selfState.claimSettled = true
                sessionSelfStates[terminalID] = selfState
                changed = true
            case .working where selfState.claimSettled, .waiting where selfState.claimSettled:
                selfState.claim = nil
                selfState.note = nil
                selfState.movedBy = nil
                selfState.personNeeded = nil
                selfState.claimReportedAt = nil
                selfState.claimSettled = false
                if selfState.owed == nil {
                    sessionSelfStates.removeValue(forKey: terminalID)
                } else {
                    sessionSelfStates[terminalID] = selfState
                }
                changed = true
            default:
                break
            }
        }
        lock.unlock()
        if changed { save() }
    }

    /// On app launch there is no transition into an already-idle prompt. Settle those receipts
    /// from the initial authoritative reading so their next turn still consumes them.
    static func reconcileSessionDeliveryStates(_ states: [String: SessionState]) {
        for (terminalID, state) in states where state == .idle {
            noteSessionStateChange(terminalID: terminalID, to: state)
        }
    }

    /// What a terminal this app opened for a task is called. Nil for every other session.
    ///
    /// `load()` first, like ``role(forTerminal:)`` below. It used not to, and the comment there
    /// explained the asymmetry: *a title that is briefly missing is a row drawn with the tab's
    /// own name*. That sentence stopped being true when ``TargetSession/displayLabel`` stopped
    /// reading tab titles — a title missing because nothing had loaded the records yet is now a
    /// row that has quietly dropped a rung, which is the same shape as the defect that change
    /// was made for. It costs a flag check after the first call.
    static func title(forTerminal id: String) -> String? {
        load()
        lock.lock(); defer { lock.unlock() }
        return titlesByTerminal[id]
    }

    /// Where that terminal sits in the tree. Nil for every session a person opened themselves.
    ///
    /// `load()` first, because a role that is briefly missing is a child mistaken for a person —
    /// which is the one wrong answer this whole arrangement exists to avoid.
    static func role(forTerminal id: String) -> Role? {
        load()
        lock.lock(); defer { lock.unlock() }
        return rolesByTerminal[id]
    }

    /// Under the lock.
    private static func reindex() {
        var found = handoffTitlesByTerminal
        var roles: [String: Role] = [:]
        for task in tasks.values {
            guard let terminal = task.childTerminalId else { continue }
            let role = Role(taskID: task.id, depth: task.depth, title: task.title,
                            deadline: task.briefedAt?
                                .addingTimeInterval(Double(task.timeoutMinutes) * 60),
                            live: !task.state.isTerminal,
                            taskRootAccess: task.childTaskRootAccess)
            // An attached task is a guest in a session somebody else owns. It may say that the
            // session is busy with broker work while it is running, and that is all: it never
            // renames the session, and it leaves nothing behind when it ends. A tab this app
            // opened is that task's for the record's whole life; a standing session wearing a
            // finished task's name and a `live: false` role is the one shape it must not have,
            // because "standing" is the whole reason it exists.
            if task.attachSessionId != nil {
                guard role.live else { continue }
                if let existing = roles[terminal], existing.live { continue }
                roles[terminal] = role
                continue
            }
            found[terminal] = task.title
            // A tab is normally one task's for its whole life. When two records name the same
            // one — a terminal id reused after a tab closed and another opened in its place —
            // the live task is the one anything asking this question means.
            if let existing = roles[terminal], existing.live, !role.live { continue }
            roles[terminal] = role
        }
        titlesByTerminal = found
        rolesByTerminal = roles
    }

    /// Handoff labels are transient UI state. Keep one while its tab is visible or its first line
    /// is still in flight; once a closed tab disappears from the reading, its reusable id must
    /// not carry the old root's label into a later session.
    static func pruneClosedHandoffTitles(visible: Set<String>) {
        let delivering = Set(handoffDeliveries.values.map(\.terminalID))
        handoffTitlesByTerminal = handoffTitlesByTerminal.filter {
            visible.contains($0.key) || delivering.contains($0.key)
        }
        reindex()
    }

    /// Commit a value copy only while the record is still the state the caller worked from.
    /// The monotonic check is the second belt: callers without a narrow expectation still cannot
    /// turn `.briefed` back into `.spawning`, or a terminal result back into a live task.
    @discardableResult
    private static func replaceTask(_ candidate: Task, expecting expected: State? = nil,
                                    discardSecret: Bool = false) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard let current = tasks[candidate.id] else { return false }
        if let expected, current.state != expected { return false }
        guard mayReplaceState(current.state, with: candidate.state) else {
            RemoteAuth.audit("orchestrator.stale_write", [
                "task": candidate.id,
                "current": current.state.rawValue,
                "candidate": candidate.state.rawValue,
            ])
            return false
        }
        tasks[candidate.id] = candidate
        if discardSecret { secrets.removeValue(forKey: candidate.id) }
        return true
    }

    // MARK: - The dispatch token

    /// Minted on first use and kept. Reading the file is the only way to hold this credential,
    /// which is the entire point — a page cannot, and a paired device was never given it.
    static func dispatchToken() -> String {
        lock.lock(); defer { lock.unlock() }
        if let onDisk = try? String(contentsOf: tokenURL, encoding: .utf8) {
            let token = onDisk.trimmingCharacters(in: .whitespacesAndNewlines)
            if !token.isEmpty { return token }
        }
        let made = RemoteAuth.newToken()
        try? FileManager.default.createDirectory(at: tokenURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(made.utf8).write(to: tokenURL, options: .atomic)
        // Re-applied after every write: an atomic write replaces the file, and the replacement
        // does not inherit the mode of what it replaced.
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: tokenURL.path)
        return made
    }

    /// Hashes both sides so the comparison is constant-time over equal lengths, like every other
    /// secret check in the app.
    static func verifyDispatch(token: String?) -> Bool {
        guard let token, !token.isEmpty else { return false }
        let expected = RemoteAuth.hex(SHA256.hash(data: Data(dispatchToken().utf8)))
        let presented = RemoteAuth.hex(SHA256.hash(data: Data(token.utf8)))
        return RemoteAuth.constantTimeEquals(expected, presented)
    }

    static func hash(ofSecret secret: String) -> String {
        RemoteAuth.hex(SHA256.hash(data: Data(secret.utf8)))
    }

    /// The at-rest key is not a request credential and is deliberately unrelated to every one.
    /// It is minted lazily, persists so queued work survives a restart, and is replaced only when
    /// the file is missing or no longer parses as exactly 32 random bytes.
    static func archiveKey() -> SymmetricKey {
        lock.lock(); defer { lock.unlock() }
        let encoded = try? String(contentsOf: archiveKeyURL, encoding: .utf8)
        if let encoded,
           let seed = Data(base64Encoded:
                encoded.trimmingCharacters(in: .whitespacesAndNewlines)),
           seed.count == 32 {
            return SymmetricKey(data: seed)
        }
        if encoded != nil {
            Log.write("orchestrator: the stored archive key will not parse — minting a new one; "
                + "serialized tasks queued under the old key will fail closed")
        }
        let made = SymmetricKey(size: .bits256)
        let seed = made.withUnsafeBytes { Data($0) }
        try? FileManager.default.createDirectory(at: archiveKeyURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(seed.base64EncodedString().utf8).write(to: archiveKeyURL, options: .atomic)
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: archiveKeyURL.path)
        return made
    }

    /// A serialized waiter has to survive an app restart, but the secret eventually typed into
    /// its child must not be stored as plaintext. The sealed value is removed before the task
    /// starts opening.
    static func sealQueuedSecret(_ secret: String) -> String? {
        return (try? ChaChaPoly.seal(Data(secret.utf8), using: archiveKey()).combined)?
            .base64EncodedString()
    }

    static func openQueuedSecret(_ sealed: String) -> String? {
        guard let data = Data(base64Encoded: sealed),
              let box = try? ChaChaPoly.SealedBox(combined: data) else { return nil }
        guard let clear = try? ChaChaPoly.open(box, using: archiveKey()) else { return nil }
        return String(data: clear, encoding: .utf8)
    }

    // MARK: - Reading the task a root wrote

    /// Everything a task.json has to say before anything is spawned from it. Pure, so a test can
    /// hand it a dictionary and a pretend filesystem.
    struct Draft: Equatable {
        var id = ""
        var kind = "custom"
        var assistant = Assistant.claude
        var model: String?
        /// A per-dispatch Codex override. Nil deliberately means no CLI config flag, preserving
        /// both Codex's model default and the user's own configuration.
        var reasoningEffort: ReasoningEffort?
        /// What the task asked for, before the ceiling. Nil means it did not ask, and takes the
        /// ceiling itself — the setting is the default as well as the limit.
        var permission: Permission?
        var projectDir = ""
        var title = ""
        var instructions = ""
        var timeoutMinutes = 30
        var rootSessionId: String?
        var rootAssistant: Assistant?
        var rootLabel: String?
        var parentTaskId: String?
        var plan: String?
        var serialize: [String] = []
        var claims: [String] = []
        var claimsDeclared = false
        var isolation = Isolation.none
        var isolationBase: String?
        /// The Q1 design's §D.3 override: send this task even though the assistant it named
        /// reads `exhausted`. A reading can be stale, wrong, or about to be moot — a task whose
        /// window opens after the account's own reset has nothing to lose. Never widens anything
        /// else: `unknown` and `ok` already dispatch without this, and `low` only ever warns.
        var ignoreQuota = false
        var attachSessionId: String?
    }

    /// Positive evidence that a caller put a physical terminal id where the task protocol needs
    /// the assistant process's conversation id. Its assistant is an observed fact, not filtered
    /// through the caller's label. Empty or conflicting evidence is deliberately inconclusive:
    /// new dispatches keep the nullable/manual-poll compatibility instead of guessing an identity.
    struct RootIdentityEvidence: Equatable {
        let source: String
        let terminalID: String
        let canonicalSessionID: String
        let assistant: Assistant
    }

    static func rootIdentityRefusal(claimed: String?, evidence: [RootIdentityEvidence]) -> Reply? {
        guard let claimed, !claimed.isEmpty else { return nil }
        let matching = evidence.filter {
            $0.terminalID == claimed && $0.canonicalSessionID != claimed
        }
        let tuples = Set(matching.map {
            $0.canonicalSessionID + "\u{0}" + $0.assistant.rawValue
        })
        guard tuples.count == 1, let proof = matching.first else { return nil }
        return .refused(
            status: 422, code: "root_identity_is_terminal",
            message: "root.session_id is a physical terminal id; use the assistant process-bound "
                + "conversation id returned as canonical_root_session_id, or null to poll.",
            extra: [
                "supplied_root_session_id": claimed,
                "canonical_root_session_id": proof.canonicalSessionID,
                "canonical_root_assistant": proof.assistant.rawValue,
                "evidence": matching.map(\.source).sorted(),
            ])
    }

    /// A live task whose working directory intersects the one being dispatched. The task is a
    /// value snapshot: warning is advisory, so a task finishing while the new tab opens does not
    /// turn a truthful observation at dispatch time into a reason to change the reply.
    struct WorkspaceOverlap {
        let task: Task
        let sharedDir: String

        func warning(for newTaskID: String) -> [String: Any] {
            [
                "code": "workspace_overlap",
                "task": task.id,
                "dir": sharedDir,
                "message": "Task \(newTaskID) overlaps active task \(task.id) at \(sharedDir).",
            ]
        }
    }

    /// One live task whose declared write set intersects the candidate's. Claim paths are
    /// absolute here so nested project directories compare in one namespace. `paths` names the
    /// shared descendant for each conflicting pair, deduplicated in declaration order.
    struct ClaimsOverlap {
        let task: Task
        let paths: [String]
        let sameRoot: Bool
        let rootsKnown: Bool
        let rootLabel: String?
        /// The blocking task's own canonical root key, before hashing — nil exactly when that
        /// task's root could not itself be resolved, independent of whether the *pair* counts
        /// as `rootsKnown`. See `Orchestrator.rootKeyDigest`.
        let rootKey: String?

        var blocks: Bool { rootsKnown && !sameRoot }

        func warning(for newTaskID: String, now: Date = Date()) -> [String: Any] {
            [
                "code": rootsKnown ? "claims_overlap" : "claims_overlap_unknown_root",
                "task": task.id,
                "paths": paths,
                "message": "Task \(newTaskID) shares claimed paths with task \(task.id): "
                    + paths.joined(separator: ", ") + ".",
                "age_seconds": max(0, Int(now.timeIntervalSince(task.created))),
                "root_key": rootKey.map(Orchestrator.rootKeyDigest) as Any? ?? NSNull(),
            ]
        }
    }

    /// One best-effort terminal delivery, separated from the AppleScript side so aggregation and
    /// missing-root decisions can be tested without a live terminal.
    struct WorkspaceOverlapNotice {
        let rootSessionID: String
        let taskID: String
        let line: String
    }

    enum DraftOutcome: Equatable {
        case ok(Draft)
        case bad(String)
    }

    enum AttachmentDecision: Equatable {
        case accepted(TargetSession, depth: Int)
        case refused(status: Int, code: String, message: String)
    }

    /// Resolve against the full watched Session inventory, which is intentionally wider than the
    /// terminal-neutral address book published by the orchestrator route. Every refusal happens
    /// before registration or terminal input.
    ///
    /// `excluding` is the id of the task this decision is *for*. Single-flight is a rule about
    /// two tasks, and a task is not the other one: the serialize queue writes a task into the
    /// registry as `spawning` before it opens anything, so re-resolving without this made every
    /// attached task that also named a `serialize` token refuse itself with
    /// `attach_session_occupied` — at a moment when the HTTP response that would have carried
    /// the error was already sent.
    static func attachmentDecision(
        sessionID: String, assistant: Assistant,
        sessions: [TargetSession], states: [String: SessionState],
        tasks: [Task], roles: [String: Role],
        isChoosing: (TargetSession) -> Bool,
        excluding excludedTaskID: String? = nil
    ) -> AttachmentDecision {
        guard let session = sessions.first(where: { $0.id == sessionID }) else {
            return .refused(status: 404, code: "attach_session_not_found",
                            message: "No session named by attach_session is currently available.")
        }
        guard let resident = session.assistant else {
            return .refused(status: 409, code: "attach_unsupported",
                            message: "attach_session names a plain shell with no assistant.")
        }
        // A standing host needs two launch-time facts: Clawdline opened its tab for a task, and
        // that process was given the whole task root. A leaf gets only its original task
        // directory, so it cannot read a new follow-up's sibling CHILD.md even though Clawdline
        // opened it. Persist the actual grant instead of inferring it from depth: the configured
        // floor can change while a tab remains standing, but a process's `--add-dir` cannot.
        guard let role = roles[sessionID], role.taskRootAccess else {
            return .refused(status: 409, code: "attach_not_managed",
                            message: "attach_session names a session without Clawdline's "
                                   + "launch-time task-root access; it cannot read a new "
                                   + "follow-up task's CHILD.md.")
        }
        guard resident == assistant else {
            return .refused(status: 409, code: "attach_assistant_mismatch",
                            message: "The task assistant differs from the attached session's assistant.")
        }
        if tasks.contains(where: {
            $0.id != excludedTaskID && !$0.state.isTerminal
                && ($0.childTerminalId == sessionID || $0.attachSessionId == sessionID)
        }) {
            return .refused(status: 409, code: "attach_session_occupied",
                            message: "That session already has a live Clawdline task.")
        }
        if states[sessionID] == .waiting, isChoosing(session) {
            return .refused(status: 409, code: "attach_session_busy",
                            message: "That session is showing a menu; no briefing was typed.")
        }
        return .accepted(session, depth: role.depth)
    }

    static func draft(from obj: [String: Any], expecting id: String,
                      isDirectory: (String) -> Bool = StartPoints.isDirectory) -> DraftOutcome {
        guard obj["clawdline_protocol"] as? Int == 1 else {
            return .bad("clawdline_protocol must be 1")
        }
        guard isTaskID(id), obj["task_id"] as? String == id else {
            return .bad("task_id must be a lowercase UUID and match the dispatch")
        }
        guard let name = obj["assistant"] as? String, let assistant = Assistant(rawValue: name) else {
            return .bad("assistant must be claude or codex")
        }
        guard let dir = obj["project_dir"] as? String, StartPoints.usable(dir),
              isDirectory(dir) else {
            return .bad("project_dir must be an absolute path to a directory")
        }
        guard let instructions = obj["instructions"] as? String, !instructions.isEmpty,
              instructions.utf8.count <= 16_384 else {
            return .bad("instructions must be non-empty and at most 16 KiB")
        }
        // Loud here, quiet at the tab. `StartPoints.modelName` runs again on the way to the
        // command line and answers "no flag" — but somebody is still holding this request, and a
        // typo they can be told about is worth more than a session quietly on the wrong model.
        var model: String?
        if let named = obj["model"] as? String, !named.isEmpty {
            guard let ok = StartPoints.modelName(named) else {
                return .bad("model must be a model name: lower-case letters, digits, . _ -, "
                          + "at most 64 characters")
            }
            model = ok
        }
        var reasoningEffort: ReasoningEffort?
        if let raw = obj["reasoning_effort"] {
            guard assistant == .codex else {
                return .bad("reasoning_effort is only valid when assistant is codex")
            }
            guard let name = raw as? String,
                  let value = ReasoningEffort(rawValue: name) else {
                return .bad("reasoning_effort must be one of: "
                          + ReasoningEffort.allCases.map(\.rawValue).joined(separator: ", "))
            }
            reasoningEffort = value
        }
        if let plan = obj["plan"] as? String, plan.utf8.count > planLimit {
            return .bad("plan must be at most \(planLimit / 1024) KiB")
        }
        var isolation = Isolation.none
        if let raw = obj["isolation"] {
            guard let name = raw as? String, let value = Isolation(rawValue: name) else {
                return .bad("isolation must be one of: none, worktree")
            }
            isolation = value
        }
        var isolationBase: String?
        if let raw = obj["isolation_base"] {
            guard isolation == .worktree else {
                return .bad("isolation_base is only valid when isolation is worktree")
            }
            guard let name = raw as? String, validIsolationBase(name) else {
                return .bad("isolation_base must be a 1–200 character Git revision using "
                          + "letters, digits, . _ / - or ~; it cannot begin with - or contain ..")
            }
            isolationBase = name
        }
        var serialize: [String] = []
        if let raw = obj["serialize"] {
            guard let values = raw as? [Any] else {
                return .bad("serialize must be an array of at most 4 tokens")
            }
            var errors: [String] = []
            if values.count > 4 { errors.append("serialize must contain at most 4 tokens") }
            var seen: Set<String> = []
            for (index, value) in values.enumerated() {
                guard let token = value as? String else {
                    errors.append("serialize[\(index)] must be a string")
                    continue
                }
                let duplicate = !seen.insert(token).inserted
                if StartPoints.modelName(token) != token {
                    errors.append("serialize[\(index)] must be 1–64 lower-case letters, digits, "
                                + ". _ -, and not begin with -")
                }
                if duplicate {
                    errors.append("serialize[\(index)] duplicates \(token)")
                }
                serialize.append(token)
            }
            if !errors.isEmpty { return .bad(errors.joined(separator: "; ")) }
        }
        var claims: [String] = []
        var claimsDeclared = false
        if let raw = obj["claims"] {
            guard let values = raw as? [Any] else {
                return .bad("claims must be an array of 0–32 relative POSIX paths")
            }
            var errors: [String] = []
            if values.count > 32 {
                errors.append("claims must contain 0–32 paths")
            }
            var seen: Set<String> = []
            for (index, value) in values.enumerated() {
                guard let path = value as? String else {
                    errors.append("claims[\(index)] must be a string")
                    continue
                }
                let duplicate = !seen.insert(path).inserted
                if path.isEmpty || path.count > 1_024 {
                    errors.append("claims[\(index)] must be 1–1024 characters")
                }
                if path.hasPrefix("/") {
                    errors.append("claims[\(index)] must be relative to project_dir")
                }
                if path.split(separator: "/", omittingEmptySubsequences: false)
                    .contains(where: { $0 == ".." }) {
                    errors.append("claims[\(index)] must not contain a .. component")
                }
                if path.unicodeScalars.contains(where: { $0.value == 0 }) {
                    errors.append("claims[\(index)] must be a POSIX path without NUL")
                }
                if duplicate {
                    errors.append("claims[\(index)] duplicates \(path)")
                }
                claims.append(path)
            }
            if !errors.isEmpty { return .bad(errors.joined(separator: "; ")) }
            claimsDeclared = true
        }
        var permission: Permission?
        if let named = obj["permission_mode"] as? String, !named.isEmpty {
            guard let ok = Permission(rawValue: named) else {
                return .bad("permission_mode must be one of: "
                          + Permission.allCases.map(\.rawValue).joined(separator: ", "))
            }
            permission = ok
        }
        var attachSessionId: String?
        if let raw = obj["attach_session"] {
            guard let id = raw as? String, !id.isEmpty, id.count <= 512 else {
                return .bad("attach_session must be a non-empty session id of at most 512 characters")
            }
            attachSessionId = id
        }
        var made = Draft()
        made.id = id
        made.assistant = assistant
        made.model = model
        made.reasoningEffort = reasoningEffort
        made.permission = permission
        made.serialize = serialize
        made.claims = claims
        made.claimsDeclared = claimsDeclared
        made.isolation = isolation
        made.isolationBase = isolationBase
        made.ignoreQuota = obj["ignore_quota"] as? Bool ?? false
        made.attachSessionId = attachSessionId
        made.plan = (obj["plan"] as? String).flatMap {
            let text = $0.trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? nil : text
        }
        made.projectDir = dir
        made.instructions = instructions
        made.kind = (obj["kind"] as? String).flatMap { $0.isEmpty ? nil : String($0.prefix(40)) } ?? "custom"
        made.title = String(((obj["title"] as? String) ?? "task").prefix(200))
        if let minutes = obj["timeout_minutes"] as? Int {
            guard (1...240).contains(minutes) else { return .bad("timeout_minutes must be 1…240") }
            made.timeoutMinutes = minutes
        }
        let rootObj = obj["root"] as? [String: Any] ?? [:]
        made.rootSessionId = rootObj["session_id"] as? String
        if let rawAssistant = rootObj["assistant"], !(rawAssistant is NSNull) {
            guard let name = rawAssistant as? String,
                  let assistant = Assistant(rawValue: name) else {
                return .bad("root.assistant must be claude or codex")
            }
            made.rootAssistant = assistant
        }
        made.rootLabel = (rootObj["label"] as? String).map { String($0.prefix(120)) }
        // A child knows its own task id — it is in the first line it was ever sent — long before
        // this app has worked out what the session inside that tab calls itself. Naming it here
        // is how a dispatch from one level down is recognised as such on the first try, and for
        // a Codex child it is the only way: its session id lives in a rollout file rather than in
        // the hook notes `rootSessionId` is matched against.
        made.parentTaskId = (rootObj["parent_task"] as? String).flatMap { isTaskID($0) ? $0 : nil }
        return .ok(made)
    }

    /// Lowercase UUID, which is also the directory name — so the id can never carry a path.
    static func isTaskID(_ id: String) -> Bool {
        guard id.count == 36 else { return false }
        return id.allSatisfy { ("a"..."f").contains($0) || $0.isNumber || $0 == "-" }
    }

    /// 32 bytes written as lower-case hex, which is what every task secret on this Mac is —
    /// `openssl rand -hex 32` in the briefing, `freshTaskSecret()` when the broker mints one.
    static func isTaskSecret(_ secret: String) -> Bool {
        secret.count == 64 && secret.allSatisfy { ("a"..."f").contains($0) || $0.isNumber }
    }

    static func validIsolationBase(_ value: String) -> Bool {
        guard !value.isEmpty, value.count <= 200, !value.hasPrefix("-"), !value.contains("..")
        else { return false }
        return value.allSatisfy {
            ("a"..."z").contains($0) || ("A"..."Z").contains($0)
                || ("0"..."9").contains($0) || $0 == "." || $0 == "_" || $0 == "/"
                || $0 == "-" || $0 == "~"
        }
    }

    static func worktreeBranch(for taskID: String) -> String? {
        isTaskID(taskID) ? "clawdline/task/\(taskID)" : nil
    }

    /// The same identity spelling used by frozen claims: standardise, then follow symlinks once
    /// while the repository exists. Every worktree path and stored repository value starts here.
    static func canonicalFilesystemPath(_ path: String) -> String {
        URL(fileURLWithPath: path).standardizedFileURL.resolvingSymlinksInPath().path
    }

    private static func worktreeRepositorySlug(_ project: String) -> String {
        let canonical = canonicalFilesystemPath(project)
        let basename = URL(fileURLWithPath: canonical).lastPathComponent.lowercased()
        var readable = String(basename.unicodeScalars.map { scalar -> Character in
            let value = scalar.value
            if (97...122).contains(value) || (48...57).contains(value) {
                return Character(String(scalar))
            }
            return "-"
        }.prefix(32))
        if readable.isEmpty { readable = "repo" }
        let digest = SHA256.hash(data: Data(canonical.utf8))
            .map { String(format: "%02x", $0) }.joined()
        return readable + "-" + String(digest.prefix(8))
    }

    static var worktreeRoot: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/Clawdline/worktrees",
                                    isDirectory: true)
    }

    static func worktreePath(project: String, taskID: String) -> String? {
        guard isTaskID(taskID) else { return nil }
        return worktreeRoot
            .appendingPathComponent(worktreeRepositorySlug(project), isDirectory: true)
            .appendingPathComponent(taskID, isDirectory: true).path
    }

    private struct GitAnswer {
        var output: String
        var status: Int32
    }

    /// The only git execution seam for worktree lifecycle operations. Arguments never pass
    /// through a shell, optional locks are disabled, and a wedged repository cannot hold the
    /// broker queue indefinitely.
    private static func git(_ arguments: [String], cwd: String,
                            timeout: TimeInterval = 15) -> GitAnswer? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = arguments
        process.currentDirectoryURL = URL(fileURLWithPath: cwd, isDirectory: true)
        var environment = ProcessInfo.processInfo.environment
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        process.environment = environment
        let pipe = Pipe()
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = pipe
        process.standardError = pipe
        do { try process.run() } catch { return nil }
        let killer = DispatchWorkItem { if process.isRunning { process.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout,
                                                       execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        process.waitQuietly()
        killer.cancel()
        return GitAnswer(output: String(data: data, encoding: .utf8) ?? "",
                         status: process.terminationStatus)
    }

    /// Resolve a landing inside the task's own repository and prove that the named commit is in
    /// the named *local* target branch. All arguments reach git without a shell; canonical object
    /// ids are what survive into the registry, not caller-supplied revision expressions.
    static func verifyTargetLanding(projectDir: String, target: String,
                                    commit: String) -> LandingVerification? {
        guard let branchCheck = git(["check-ref-format", "--branch", target], cwd: projectDir),
              branchCheck.status == 0 else { return nil }
        let targetRef = "refs/heads/\(target)"
        guard let resolvedCommit = git(
                ["rev-parse", "--verify", "--end-of-options", "\(commit)^{commit}"],
                cwd: projectDir), resolvedCommit.status == 0,
              let resolvedTarget = git(
                ["rev-parse", "--verify", "--end-of-options", "\(targetRef)^{commit}"],
                cwd: projectDir), resolvedTarget.status == 0 else { return nil }
        let commitID = resolvedCommit.output.trimmingCharacters(in: .whitespacesAndNewlines)
        let targetID = resolvedTarget.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard [commitID, targetID].allSatisfy({ id in
            (id.count == 40 || id.count == 64)
                && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }) else { return nil }
        guard let contained = git(["merge-base", "--is-ancestor", commitID, targetID],
                                  cwd: projectDir), contained.status == 0 else { return nil }
        return LandingVerification(origin: "local_target_branch", commit: commitID,
                                   targetCommit: targetID)
    }

    private enum WorktreePreparation {
        case ready(Worktree, warnings: [[String: Any]])
        case bad(String)
        case unavailable(String)
    }

    private static func filesystemFreeBytes(at path: String) -> Int64? {
        if let value = try? URL(fileURLWithPath: path).resourceValues(
            forKeys: [.volumeAvailableCapacityForImportantUsageKey]
        ).volumeAvailableCapacityForImportantUsage {
            return value
        }
        let attributes = try? FileManager.default.attributesOfFileSystem(forPath: path)
        return (attributes?[.systemFreeSize] as? NSNumber)?.int64Value
    }

    private static func relativePath(from root: String, to child: String) -> String? {
        let rootParts = URL(fileURLWithPath: canonicalFilesystemPath(root)).pathComponents
        let childParts = URL(fileURLWithPath: canonicalFilesystemPath(child)).pathComponents
        guard childParts.count >= rootParts.count,
              Array(childParts.prefix(rootParts.count)) == rootParts else { return nil }
        return childParts.dropFirst(rootParts.count).joined(separator: "/")
    }

    private static func prepareWorktree(for draft: Draft, taskID: String,
                                        queued: Bool) -> WorktreePreparation {
        guard let top = git(["rev-parse", "--show-toplevel"], cwd: draft.projectDir),
              top.status == 0 else {
            return .bad("isolation:\"worktree\" needs project_dir to be inside a Git repository.")
        }
        let repository = top.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard StartPoints.usable(repository) else {
            return .bad("isolation:\"worktree\" could not resolve the repository containing project_dir.")
        }
        let requested = draft.isolationBase ?? "HEAD"
        guard let resolved = git(["rev-parse", "--verify", "\(requested)^{commit}"], cwd: repository),
              resolved.status == 0 else {
            if draft.isolationBase == nil {
                return .unavailable("This repository has no commit to use as a worktree base.")
            }
            return .bad("isolation_base does not resolve to a commit in project_dir.")
        }
        let base = resolved.output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let branch = worktreeBranch(for: taskID),
              let path = worktreePath(project: repository, taskID: taskID) else {
            return .bad("task_id cannot name a worktree branch or path.")
        }
        let canonicalProject = canonicalFilesystemPath(draft.projectDir)
        let canonicalRepository = canonicalFilesystemPath(repository)
        guard let relative = relativePath(from: canonicalRepository, to: canonicalProject) else {
            return .bad("project_dir is not inside its resolved Git repository.")
        }
        let childCwd = relative.isEmpty ? path
            : URL(fileURLWithPath: path, isDirectory: true).appendingPathComponent(relative).path

        let home = FileManager.default.homeDirectoryForCurrentUser.path
        guard let free = filesystemFreeBytes(at: home), free >= 2_000_000_000 else {
            return .unavailable("The worktree volume needs at least 2 GB of available space.")
        }
        let status = git(["status", "--porcelain", "--untracked-files=all"], cwd: repository)
        let dirty = status?.status == 0
            ? status!.output.split(whereSeparator: \.isNewline).count : 0
        var warnings: [[String: Any]] = []
        if dirty > 0 {
            let message = queued
                ? "The base tree currently has \(dirty) uncommitted files; the clean checkout "
                    + "created when this queued task starts will not contain them."
                : "The base tree has \(dirty) uncommitted files; the worktree starts from commit "
                    + "\(String(base.prefix(7))) and does not contain them."
            warnings.append([
                "code": "dirty_worktree_base",
                "message": message,
            ])
        }
        let gitDirectory = git(["rev-parse", "--git-dir"], cwd: repository)?.output
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if let gitDirectory, !gitDirectory.isEmpty {
            let absoluteGit = gitDirectory.hasPrefix("/") ? gitDirectory
                : URL(fileURLWithPath: repository).appendingPathComponent(gitDirectory).path
            let markers = ["MERGE_HEAD", "rebase-merge", "rebase-apply", "BISECT_LOG"]
            let inProgress = markers.filter {
                FileManager.default.fileExists(atPath:
                    URL(fileURLWithPath: absoluteGit).appendingPathComponent($0).path)
            }
            if !inProgress.isEmpty {
                warnings.append([
                    "code": "git_operation_in_progress",
                    "message": "The base repository has an operation in progress ("
                        + inProgress.joined(separator: ", ") + "); integrating the branch may wait.",
                ])
            }
        }
        var worktree = Worktree(path: path, branch: branch, base: base,
                                repository: canonicalRepository, cwd: childCwd)
        worktree.baseDirty = dirty
        worktree.requestedBase = requested
        return .ready(worktree, warnings: warnings)
    }

    private static func resolveSpawnBase(in worktree: Worktree) -> Worktree? {
        guard let answer = git(["rev-parse", "--verify",
                                "\(worktree.requestedBase)^{commit}"],
                               cwd: worktree.repository), answer.status == 0 else { return nil }
        var resolved = worktree
        resolved.base = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
        return resolved
    }

    private static func pruneWorktrees(in repository: String) {
        _ = git(["worktree", "prune"], cwd: repository)
    }

    /// Materialise only when the task is actually leaving the queue. A prepared task holds no
    /// checkout and no branch while a serialization token is busy; spawn resolves its base again
    /// and the resulting SHA becomes the immutable receipt.
    private static func addWorktree(_ worktree: Worktree, taskID: String) -> String? {
        let parent = URL(fileURLWithPath: worktree.path, isDirectory: true)
            .deletingLastPathComponent()
        do {
            try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true)
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                   ofItemAtPath: worktreeRoot.path)
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                   ofItemAtPath: parent.path)
        } catch {
            return "Could not create the private worktree directory: \(error.localizedDescription)"
        }
        guard let answer = git(["worktree", "add", "-b", worktree.branch,
                                worktree.path, worktree.base], cwd: worktree.repository,
                               timeout: 60) else {
            pruneWorktrees(in: worktree.repository)
            return "git worktree add could not be started"
        }
        guard answer.status == 0 else {
            pruneWorktrees(in: worktree.repository)
            let detail = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
            return String((detail.isEmpty ? "git worktree add failed" : detail).prefix(500))
        }
        let currentStatus = git(["status", "--porcelain", "--untracked-files=all"],
                                cwd: worktree.repository)
        let dirty = currentStatus?.status == 0
            ? currentStatus!.output.split(whereSeparator: \.isNewline).count
            : worktree.baseDirty
        RemoteAuth.audit("orchestrator.worktree.add", [
            "task": taskID, "base": worktree.base, "branch": worktree.branch,
            "path": worktree.path, "dirty": String(dirty),
        ])
        guard FileManager.default.fileExists(atPath: worktree.cwd) else {
            disposeWorktree(worktree, taskID: taskID, why: "spawn_failed")
            return "The requested project_dir does not exist in base commit \(worktree.base)."
        }
        return nil
    }

    private struct WorktreeFacts {
        var head: String?
        var commits: Int?
        var dirty: Bool?
        var headOnBranch: Bool?
        var branchExists: Bool
    }

    private static func inspectWorktree(_ worktree: Worktree) -> WorktreeFacts {
        let branchRef = "refs/heads/\(worktree.branch)"
        let branchAnswer = git(["rev-parse", "--verify", "\(branchRef)^{commit}"],
                               cwd: worktree.repository)
        let branchExists = branchAnswer?.status == 0
        let head = branchExists
            ? branchAnswer?.output.trimmingCharacters(in: .whitespacesAndNewlines) : nil
        let countAnswer = branchExists
            ? git(["rev-list", "--count", "\(worktree.base)..\(branchRef)"],
                  cwd: worktree.repository) : nil
        let commits = countAnswer?.status == 0
            ? Int(countAnswer!.output.trimmingCharacters(in: .whitespacesAndNewlines)) : nil
        guard FileManager.default.fileExists(atPath: worktree.path) else {
            return WorktreeFacts(head: head, commits: commits, dirty: nil,
                                 headOnBranch: nil, branchExists: branchExists)
        }
        let status = git(["status", "--porcelain", "--untracked-files=all"], cwd: worktree.path)
        let dirty = status?.status == 0 ? !status!.output.isEmpty : nil
        let symbolic = git(["symbolic-ref", "--quiet", "--short", "HEAD"], cwd: worktree.path)
        let headOnBranch = symbolic?.status == 0
            ? symbolic!.output.trimmingCharacters(in: .whitespacesAndNewlines) == worktree.branch
            : false
        return WorktreeFacts(head: head, commits: commits, dirty: dirty,
                             headOnBranch: headOnBranch, branchExists: branchExists)
    }

    private static func refreshedWorktree(_ original: Worktree) -> Worktree {
        var worktree = original
        let facts = inspectWorktree(worktree)
        worktree.head = facts.head
        worktree.commits = facts.commits
        worktree.dirty = facts.dirty
        return worktree
    }

    /// Remove through git or not at all. Even failed removals are followed by prune, while branch
    /// deletion happens only after git removed a provably empty checkout successfully.
    private static func disposeWorktree(_ worktree: Worktree, taskID: String, why: String,
                                        allowCommitted: Bool = true) {
        let facts = inspectWorktree(worktree)
        let decision = worktreeDisposal(commits: facts.commits, dirty: facts.dirty,
                                        headOnBranch: facts.headOnBranch,
                                        branchExists: facts.branchExists)
        guard decision != .keepEverything else {
            let keptWhy: String
            if facts.dirty == true { keptWhy = "dirty" }
            else if facts.commits == 0 && facts.headOnBranch == false { keptWhy = "head_moved" }
            else { keptWhy = "unreadable" }
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "why": keptWhy,
            ])
            return
        }
        if decision == .removeTreeKeepBranch && !allowCommitted { return }
        guard FileManager.default.fileExists(atPath: worktree.path) else {
            // The checkout is already gone. The branch remains the delivery; never infer that a
            // missing directory authorizes deleting it.
            pruneWorktrees(in: worktree.repository)
            return
        }
        let removed = git(["worktree", "remove", worktree.path], cwd: worktree.repository,
                          timeout: 60)
        pruneWorktrees(in: worktree.repository)
        guard removed?.status == 0 else {
            RemoteAuth.audit("orchestrator.worktree.kept", [
                "task": taskID, "branch": worktree.branch, "why": "remove_failed",
            ])
            return
        }
        if decision == .removeAll {
            let deleted = git(["branch", "-D", worktree.branch], cwd: worktree.repository)
            guard deleted?.status == 0 else {
                RemoteAuth.audit("orchestrator.worktree.kept", [
                    "task": taskID, "branch": worktree.branch, "why": "remove_failed",
                ])
                return
            }
        }
        RemoteAuth.audit("orchestrator.worktree.remove", [
            "task": taskID, "branch": worktree.branch, "why": why,
            "commits": String(facts.commits ?? 0),
        ])
    }

    /// Enqueue only; callers on the main thread never execute a git subprocess themselves.
    private static func scheduleWorktreeDisposal(_ worktree: Worktree, taskID: String,
                                                 why: String,
                                                 allowCommitted: Bool = true) {
        worktreeQueue.async {
            disposeWorktree(worktree, taskID: taskID, why: why,
                            allowCommitted: allowCommitted)
        }
    }

    /// Current holders followed by older waiters entitled to a shared token first. Roots are
    /// deliberately absent from this comparison: the namespace covers the whole machine. Every
    /// token in a request is considered together, so a queued multi-token task holds none of them.
    static func serializeBlockers(for candidate: Task, among existing: [Task]) -> [Task] {
        guard candidate.state == .queued, !candidate.serialize.isEmpty else { return [] }
        let wanted = Set(candidate.serialize)
        func earlier(_ task: Task) -> Bool {
            task.created < candidate.created
                || (task.created == candidate.created && task.id < candidate.id)
        }
        return existing.filter { task in
            guard task.id != candidate.id,
                  !wanted.isDisjoint(with: task.serialize) else { return false }
            if task.state == .spawning || task.state == .briefed { return true }
            return task.state == .queued && earlier(task)
        }.sorted { left, right in
            if left.created == right.created { return left.id < right.id }
            return left.created < right.created
        }
    }

    private static func serializeBlockersLocked(for candidate: Task) -> [Task] {
        serializeBlockers(for: candidate, among: Array(tasks.values))
    }

    /// Freeze claims into one namespace while the validated project directory exists. Only the
    /// root touches the filesystem; relative claims are then normalised and joined literally so
    /// creating a target (or a symlink below the root) can never change a lease's identity.
    static func freezeClaims(_ claims: [String], projectDir: String) -> [String] {
        guard !claims.isEmpty else { return [] }
        let root = canonicalFilesystemPath(projectDir)
        let separator = root == "/" ? "" : "/"
        return claims.map { claim in
            let relative = claim.split(separator: "/", omittingEmptySubsequences: true)
                .filter { $0 != "." }.joined(separator: "/")
            return relative.isEmpty ? root : root + separator + relative
        }
    }

    /// Relative claims name the shared checkout. An isolated child cannot touch those paths at
    /// that spelling, so retaining the lease would only block useful work in the base tree.
    static func prepareClaimsForIsolation(_ task: inout Task) -> [[String: Any]] {
        guard task.isolation == .worktree, !task.claims.isEmpty else { return [] }
        let ignored = task.claims
        task.claims = []
        task.claimKeys = []
        return [[
            "code": "claims_ignored_for_worktree",
            "paths": ignored,
            "message": "Claims inside project_dir were ignored because this task uses an isolated worktree.",
        ]]
    }

    /// The shared descendant of two already-frozen claim keys. This is deliberately only string
    /// work: dispatch holds the global orchestrator lock while it compares every live lease.
    private static func sharedClaimPath(_ first: String, _ second: String) -> String? {
        if first == second { return first }
        let firstPrefix = first == "/" ? "/" : first + "/"
        if second.hasPrefix(firstPrefix) { return second }
        let secondPrefix = second == "/" ? "/" : second + "/"
        if first.hasPrefix(secondPrefix) { return first }
        return nil
    }

    /// Two explicit write declarations make L1 redundant when their frozen scopes do not meet.
    /// The empty declaration is the useful edge: it positively says the task is read-only.
    private static func declaredClaimsAreDisjoint(_ first: Task, _ second: Task) -> Bool {
        guard first.claimsDeclared, second.claimsDeclared else { return false }
        return !first.activeClaimKeys.contains { claimed in
            second.activeClaimKeys.contains { sharedClaimPath(claimed, $0) != nil }
        }
    }

    /// Pure dispatch-time claims scan. Unlike L1, queued tasks participate: a claim is a
    /// reservation made at dispatch, not evidence that a tab has started touching files.
    /// Compares `activeClaimKeys` rather than `claimKeys` so a path either side already gave
    /// back through `claims/release` no longer conflicts.
    static func claimsOverlaps(for newTask: Task, among existing: [Task]) -> [ClaimsOverlap] {
        guard !newTask.activeClaimKeys.isEmpty, !newTask.state.isTerminal else { return [] }
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = resolvedRootKey(of: newTask, among: indexed)
        return existing.compactMap { task -> ClaimsOverlap? in
            guard task.id != newTask.id, !task.state.isTerminal, !task.activeClaimKeys.isEmpty
            else {
                return nil
            }
            var paths: [String] = []
            var seen: Set<String> = []
            for claimed in newTask.activeClaimKeys {
                for other in task.activeClaimKeys {
                    if let shared = sharedClaimPath(claimed, other),
                       seen.insert(shared).inserted {
                        paths.append(shared)
                    }
                }
            }
            guard !paths.isEmpty else { return nil }
            let root = rootTask(of: task, among: indexed)
            let otherRoot = resolvedRootKey(of: task, among: indexed)
            let rootsKnown = newRoot != nil && otherRoot != nil
            return ClaimsOverlap(task: task, paths: paths,
                                 sameRoot: rootsKnown && otherRoot == newRoot,
                                 rootsKnown: rootsKnown,
                                 rootLabel: root.rootLabel ?? task.rootLabel,
                                 rootKey: otherRoot)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    private static func claimsOverlapsLocked(for candidate: Task) -> [ClaimsOverlap] {
        claimsOverlaps(for: candidate, among: Array(tasks.values))
    }

    /// The directory both tasks may write, or nil when their paths are merely string prefixes.
    ///
    /// Paths are resolved the way the rest of the project resolves working directories:
    /// standardise first, then follow symlinks, and compare the resulting spelling exactly.
    /// Comparing components is what keeps `/a/b` separate from `/a/bc`, and also handles `/`
    /// without a special string-prefix case.
    static func sharedWorkspaceDirectory(_ first: String, _ second: String) -> String? {
        let first = canonicalFilesystemPath(first)
        let second = canonicalFilesystemPath(second)
        let firstParts = URL(fileURLWithPath: first).pathComponents
        let secondParts = URL(fileURLWithPath: second).pathComponents
        let common = min(firstParts.count, secondParts.count)
        for index in 0..<common where firstParts[index] != secondParts[index] { return nil }
        if firstParts.count == common { return second }
        if secondParts.count == common { return first }
        return nil
    }

    /// The root key used throughout the orchestrator, with the task table supplied explicitly so
    /// the dispatch-time overlap rules remain a pure unit-test seam.
    private static func rootTask(of task: Task, among existing: [String: Task]) -> Task {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId, let above = existing[parentID], hops < depthFloor {
            at = above
            hops += 1
        }
        return at
    }

    private static func rootKey(of task: Task, among existing: [String: Task]) -> String {
        let at = rootTask(of: task, among: existing)
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// Claims are a hard gate only when both trees can actually be identified. A task with an
    /// unresolved parent and no independently supplied root session is unknown, not a new root.
    private static func resolvedRootKey(of task: Task,
                                        among existing: [String: Task]) -> String? {
        var at = task
        var hops = 0
        while let parentID = at.parentTaskId {
            guard hops < depthFloor, let above = existing[parentID] else {
                return at.rootSessionId
            }
            at = above
            hops += 1
        }
        return at.rootSessionId ?? "task:\(at.id)"
    }

    /// The stable short identifier for a root tree, independent of its self-reported label:
    /// SHA-256 of the canonical root key — a live root's session id, or `task:<id>` for a task
    /// resolved back to itself — truncated to its first 8 hex characters. Two roots that both
    /// call themselves the same thing still hash differently, because the input is the session
    /// identity underneath the label rather than the label itself; the same tree always hashes
    /// the same way.
    static func rootKeyDigest(_ canonicalRootKey: String) -> String {
        String(RemoteAuth.hex(SHA256.hash(data: Data(canonicalRootKey.utf8))).prefix(8))
    }

    /// Pure half of the dispatch-time scan, kept visible to the unit suite so path boundaries,
    /// root identity and terminal-state filtering do not need a live terminal to exercise them.
    static func workspaceOverlaps(for newTask: Task,
                                  among existing: [Task]) -> [WorkspaceOverlap] {
        let indexed = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        let newRoot = rootKey(of: newTask, among: indexed)
        let rooted = existing.map { (task: $0, rootKey: rootKey(of: $0, among: indexed)) }
        return workspaceOverlaps(for: newTask, rootKey: newRoot, among: rooted)
    }

    private static func workspaceOverlaps(for newTask: Task, rootKey newRoot: String,
                                          among existing: [(task: Task, rootKey: String)])
        -> [WorkspaceOverlap] {
        guard newTask.state == .spawning || newTask.state == .briefed else { return [] }
        return existing.compactMap { item -> WorkspaceOverlap? in
            let task = item.task
            guard task.id != newTask.id,
                  task.state == .spawning || task.state == .briefed,
                  item.rootKey != newRoot,
                  !declaredClaimsAreDisjoint(newTask, task),
                  let shared = sharedWorkspaceDirectory(cwd(of: newTask), cwd(of: task))
            else { return nil }
            return WorkspaceOverlap(task: task, sharedDir: shared)
        }.sorted { left, right in
            if left.task.created == right.task.created { return left.task.id < right.task.id }
            return left.task.created < right.task.created
        }
    }

    private static func workspaceOverlaps(for newTask: Task) -> [WorkspaceOverlap] {
        lock.lock()
        let newRoot = rootKeyLocked(of: newTask)
        let existing = tasks.values.map { (task: $0, rootKey: rootKeyLocked(of: $0)) }
        lock.unlock()
        return workspaceOverlaps(for: newTask, rootKey: newRoot, among: existing)
    }

    /// Wire payload shared by first dispatches and idempotent retries. Keeping the optional field
    /// here makes "absent, not an empty array" explicit and independently testable.
    static func dispatchPayload(record: [String: Any], taskID: String,
                                overlaps: [WorkspaceOverlap],
                                claimsOverlaps: [ClaimsOverlap] = [],
                                additionalWarnings: [[String: Any]] = [],
                                now: Date = Date()) -> [String: Any] {
        var reply: [String: Any] = ["ok": true, "task": record]
        let warnings = overlaps.map { $0.warning(for: taskID) }
            + claimsOverlaps.filter { !$0.blocks }.map { $0.warning(for: taskID, now: now) }
            + additionalWarnings
        if !warnings.isEmpty {
            reply["warnings"] = warnings
        }
        return reply
    }

    /// The one age formula used by workspace conflicts and landing records alike. A wall clock
    /// moving backwards never turns an API duration negative.
    static func ageSeconds(since: Date, now: Date) -> Int {
        max(0, Int(now.timeIntervalSince(since)))
    }

    /// The actionable context returned when another root already reserved a write path.
    /// `age_seconds` and `root_key` make the error self-sufficient without a follow-up GET:
    /// `root_label` is self-reported prose that can be stale or shared by two unrelated roots
    /// (two different trees both calling themselves "clawdline schedules" is a real case), while
    /// `root_key` is the same tree's identity every time, hashed rather than handed over raw.
    static func workspaceBusyExtra(_ overlap: ClaimsOverlap, now: Date = Date()) -> [String: Any] {
        let extra: [String: Any] = [
            "blocking_task": overlap.task.id,
            "title": overlap.task.title,
            "root_label": overlap.rootLabel as Any? ?? NSNull(),
            "created": Int(overlap.task.created.timeIntervalSince1970),
            "conflict_paths": overlap.paths,
            "retry_after": 60,
            "age_seconds": ageSeconds(since: overlap.task.created, now: now),
            "root_key": overlap.rootKey.map(rootKeyDigest) as Any? ?? NSNull(),
        ]
        return extra
    }

    // MARK: - Assistant quota at the dispatch gate — Q1 design §D

    /// `age_seconds` for an `AssistantQuota`, in the identical shape `ClaimsOverlap.warning(for:)`
    /// and `workspaceBusyExtra` above already use: an integer number of seconds, `max(0, now -
    /// observed)`. Not a second formula for the same idea.
    private static func assistantAgeSeconds(_ quota: AssistantQuota, now: Date) -> Int? {
        quota.observedAt.map { max(0, Int(now.timeIntervalSince1970) - $0) }
    }

    /// §D.4: the 409 a dispatch gets when the assistant it named has no quota left.
    /// `alternatives` is the whole value of this over a bare refusal — it names who else to
    /// dispatch to in the same response that said no, so a root can act on it without a
    /// follow-up read. The message names `ignore_quota` outright: §D.3 requires that a caller who
    /// hits this can see its way out without having to already know this field exists.
    static func assistantExhaustedReply(_ quota: AssistantQuota, now: Date = Date()) -> Reply {
        let ageSeconds = assistantAgeSeconds(quota, now: now)
        let secondsToReset = quota.resetsAt.map { $0 - Int(now.timeIntervalSince1970) } ?? 3_600
        let retryAfter = max(0, min(secondsToReset, 3_600))
        let others = AssistantQuota.all(now: now).filter { $0.assistant != quota.assistant }
        let alternatives = others.map { alt -> [String: Any] in
            ["id": alt.assistant.rawValue, "availability": alt.availability.rawValue,
             "detail": alt.detail]
        }
        // Naming who to send it to instead costs nothing with only two assistants on this Mac —
        // "another assistant" makes a root re-read `alternatives` to learn what this sentence
        // already knows. Only when nobody else has anything left does the message fall back to
        // waiting: naming an assistant that is itself exhausted would just move the same 409
        // one hop later.
        let redirect = others.first(where: { $0.availability != .exhausted })
            .map { "Dispatch to \($0.assistant.rawValue) instead, wait for the reset" }
            ?? "Wait for the reset"
        let message = "\(quota.assistant.label) has no quota left (\(quota.detail)). \(redirect), "
            + "or set \"ignore_quota\": true in task.json to send it anyway."
        let extra: [String: Any] = [
            "assistant": quota.assistant.rawValue,
            "availability": quota.availability.rawValue,
            "source": quota.source.rawValue,
            "observed_at": quota.observedAt as Any? ?? NSNull(),
            "age_seconds": ageSeconds as Any? ?? NSNull(),
            "resets_at": quota.resetsAt as Any? ?? NSNull(),
            "retry_after": retryAfter,
            "alternatives": alternatives,
        ]
        return .refused(status: 409, code: "assistant_exhausted", message: message, extra: extra)
    }

    /// §D.3/§D.4's last paragraph: `ignore_quota` sent an exhausted assistant anyway, so this is
    /// a warning rather than the 409 above — but it still says so, and it says one more thing the
    /// 409 could not: whether the window this task is betting on resets before the task's own
    /// `timeout_minutes` runs out.
    static func assistantOverrideWarning(_ quota: AssistantQuota, timeoutMinutes: Int,
                                         now: Date = Date()) -> [String: Any] {
        var message = "\(quota.assistant.label) has no quota left (\(quota.detail)), but "
            + "ignore_quota was set; sending it anyway."
        if let resets = quota.resetsAt {
            let deadline = now.addingTimeInterval(Double(timeoutMinutes) * 60)
            if Double(resets) > deadline.timeIntervalSince1970 {
                message += " Its window will not reset before this task's timeout."
            }
        }
        return [
            "code": "assistant_exhausted",
            "assistant": quota.assistant.rawValue,
            "message": message,
            "availability": quota.availability.rawValue,
            "observed_at": quota.observedAt as Any? ?? NSNull(),
            "age_seconds": assistantAgeSeconds(quota, now: now) as Any? ?? NSNull(),
            "resets_at": quota.resetsAt as Any? ?? NSNull(),
        ]
    }

    /// §D.6: `low` never refuses, only warns — reusing the same `warnings` array
    /// `dispatchPayload(record:taskID:overlaps:)` already fills with `workspace_overlap` and
    /// `claims_overlap` rows, so a root reading that one array sees every reason to be careful in
    /// one place.
    static func assistantLowWarning(_ quota: AssistantQuota, now: Date = Date()) -> [String: Any] {
        let ageSeconds = assistantAgeSeconds(quota, now: now)
        let ageText = ageSeconds.map { seconds -> String in
            let minutes = max(1, seconds / 60)
            return "\(minutes) minute\(minutes == 1 ? "" : "s") ago"
        } ?? "a while ago"
        var message = "\(quota.assistant.label) is at \(quota.detail) (observed \(ageText))."
        if let resets = quota.resetsAt, Double(resets) > now.timeIntervalSince1970 {
            message += " Resets in "
                + AssistantQuota.formatDuration(seconds: Double(resets) - now.timeIntervalSince1970) + "."
        }
        message += " A long task may not finish."
        return [
            "code": "assistant_low",
            "assistant": quota.assistant.rawValue,
            "message": message,
            "availability": quota.availability.rawValue,
            "observed_at": quota.observedAt as Any? ?? NSNull(),
            "age_seconds": ageSeconds as Any? ?? NSNull(),
            "resets_at": quota.resetsAt as Any? ?? NSNull(),
        ]
    }

    // MARK: - Handoff

    static func handoffPackageReady(id: String) -> Bool {
        let manager = FileManager.default
        let parent = handoffRoot.deletingLastPathComponent()
        do {
            for directory in [parent, handoffRoot] {
                try manager.createDirectory(at: directory, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
                try manager.setAttributes([.posixPermissions: 0o700],
                                          ofItemAtPath: directory.path)
            }
        } catch {
            return false
        }
        let directory = handoffRoot.appendingPathComponent(id, isDirectory: true)
        var isDirectory: ObjCBool = false
        guard manager.fileExists(atPath: directory.path, isDirectory: &isDirectory),
              isDirectory.boolValue else { return false }
        let letter = directory.appendingPathComponent("handoff.md")
        guard let attributes = try? manager.attributesOfItem(atPath: letter.path),
              attributes[.type] as? FileAttributeType == .typeRegular,
              let size = attributes[.size] as? NSNumber else { return false }
        // Size is the only fact read. The postman never opens the letter to judge its contents.
        return size.int64Value > 0
    }

    static func handoffDraft(from obj: [String: Any],
                             isDirectory: (String) -> Bool = StartPoints.isDirectory,
                             packageIsReady: ((String) -> Bool)? = nil)
        -> HandoffDraftOutcome {
        guard let id = obj["handoff_id"] as? String, isTaskID(id) else {
            return .bad("handoff_id must be a lowercase UUID")
        }
        guard let projectDir = obj["project_dir"] as? String,
              StartPoints.usable(projectDir), isDirectory(projectDir) else {
            return .bad("project_dir must be an absolute path to a directory")
        }
        let assistant: Assistant
        if let raw = obj["assistant"] {
            guard let name = raw as? String, let selected = Assistant(rawValue: name) else {
                return .bad("assistant must be claude or codex")
            }
            assistant = selected
        } else {
            assistant = .claude
        }
        var model: String?
        if let raw = obj["model"] {
            guard let name = raw as? String, StartPoints.modelName(name) == name else {
                return .bad("model must be a model name: lower-case letters, digits, . _ -, "
                          + "at most 64 characters")
            }
            model = name
        }
        func optionalString(_ key: String) -> String?? {
            guard let raw = obj[key] else { return .some(nil) }
            guard let value = raw as? String, value.count <= 200 else { return nil }
            return .some(.some(value))
        }
        guard let title = optionalString("title") else {
            return .bad("title must be a string of at most 200 characters")
        }
        guard let fromSession = optionalString("from_session") else {
            return .bad("from_session must be a string of at most 200 characters")
        }
        let ready = packageIsReady?(id) ?? handoffPackageReady(id: id)
        guard ready else {
            return .bad("No non-empty regular handoff.md under "
                      + "/tmp/.clawdline/handoffs/<handoff_id>/")
        }
        return .ok(HandoffDraft(id: id, projectDir: projectDir, assistant: assistant,
                                model: model, title: title, fromSession: fromSession))
    }

    /// The one protocol sentence typed into every receiving assistant. Keep it assembled here so
    /// tests compare the exact bytes rather than searching for a convenient fragment.
    static func handoffLine(id: String) -> String {
        "You are picking up a Clawdline handoff. Read /tmp/.clawdline/handoffs/\(id)/handoff.md "
            + "before anything else and follow it: walk its REFERENCES, answer its VERIFICATION "
            + "questions from those sources, say plainly what you could not reach, then continue "
            + "from OPEN THREADS."
    }

    static func handoffReceipt(id: String, title: String?, assistant: Assistant,
                               projectDir: String, delivered: Bool)
        -> ClawdlineMessage.Notice {
        let short = String(id.prefix(8))
        let named = title.map { " (\($0))" } ?? ""
        let body = delivered
            ? "[clawdline] handoff \(short)\(named) picked up by \(assistant.rawValue) "
                + "in \(projectDir)"
            : "[clawdline] handoff \(short) opened a tab but the first line never landed "
                + "— type it in by hand"
        return ClawdlineMessage.Notice(
            event: .handoffReceipt(
                handoffID: id, title: title,
                assistant: ClawdlineMessage.HandoffAssistant(assistant),
                projectDir: projectDir,
                state: delivered ? .pickedUp : .firstLineFailed),
            body: body)
    }

    private static func successfulHandoffReply(for envelope: HandoffEnvelope,
                                               draft: HandoffDraft? = nil,
                                               terminalID: String? = nil,
                                               backend: Backend? = nil) -> Reply {
        var record: [String: Any] = [
            "id": envelope.id,
            "state": envelope.state.rawValue,
            "projectDir": envelope.projectDir,
            "dir": envelope.dir.path,
        ]
        if let title = envelope.title { record["title"] = title }
        if let from = envelope.fromSession { record["fromSession"] = from }
        if let draft { record["assistant"] = draft.assistant.rawValue }
        if let terminalID, let backend {
            record["opened"] = ["terminalId": terminalID, "backend": backend.rawValue]
        }
        return .ok(["ok": true, "handoff": record])
    }

    typealias HandoffStarter = (StartPoints.Place, Assistant, String?, String?)
        -> StartPoints.Outcome

    /// Register and synchronously open one receiving tab. Waiting for a composer and confirming
    /// the first turn happen on the ordinary orchestrator beat after the HTTP connection closes.
    static func handoff(_ obj: [String: Any],
                        start: HandoffStarter = { place, assistant, model, addDir in
                            StartPoints.start(place, assistant: assistant, model: model,
                                              addDir: addDir)
                        }) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled",
                            "Task dispatch is switched off in Settings.")
        }
        guard let id = obj["handoff_id"] as? String, isTaskID(id) else {
            return .refused(422, "bad_task", "handoff_id must be a lowercase UUID.")
        }
        if let existing = heldHandoff(id) { return successfulHandoffReply(for: existing) }
        guard takeDispatchRate() != nil else {
            return .refused(429, "rate_limited", "Too many dispatches; wait a few minutes.")
        }
        let draft: HandoffDraft
        switch handoffDraft(from: obj) {
        case .bad(let why): return .refused(422, "bad_task", why)
        case .ok(let valid): draft = valid
        }
        let envelope = HandoffEnvelope(id: draft.id, projectDir: draft.projectDir,
                                       title: draft.title, fromSession: draft.fromSession,
                                       created: Date(), state: .opening)
        lock.lock()
        // The server queue is serial, but the lock keeps direct test callers and any future
        // entry point from crossing the open-tab side effect together.
        if let existing = handoffs[id] {
            lock.unlock()
            return successfulHandoffReply(for: existing)
        }
        handoffs[id] = envelope
        lock.unlock()
        save()
        RemoteAuth.audit("handoff.open", ["handoff": id, "assistant": draft.assistant.rawValue,
                                           "cwd": draft.projectDir,
                                           "title": draft.title ?? "handoff \(id.prefix(8))"])

        let place = StartPoints.Place(id: StartPoints.id(for: draft.projectDir),
                                      path: draft.projectDir,
                                      label: draft.title ?? "handoff \(id.prefix(8))", at: Date())
        switch start(place, draft.assistant, draft.model, envelope.dir.path) {
        case .refused(let status, let code, let message, let app):
            lock.lock()
            var failed = handoffs[id] ?? envelope
            failed.state = .spawnFailed
            handoffs[id] = failed
            lock.unlock()
            save()
            RemoteAuth.audit("handoff.undelivered", ["handoff": id, "why": code])
            return .refused(status: status, code: code, message: message,
                            extra: app.map { ["app": $0] } ?? [:])
        case .started(let terminalID, let backend):
            let delivery = HandoffDelivery(id: id, assistant: draft.assistant,
                                           model: draft.model, terminalID: terminalID,
                                           backend: backend, spawnedAt: Date())
            lock.lock()
            handoffDeliveries[id] = delivery
            handoffTitlesByTerminal[terminalID] = place.label
            reindex()
            lock.unlock()
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return successfulHandoffReply(for: envelope, draft: draft,
                                          terminalID: terminalID, backend: backend)
        }
    }

    // MARK: - Dispatch

    private static func resolveAttachment(sessionID: String, assistant: Assistant,
                                          excluding excludedTaskID: String? = nil)
        -> AttachmentDecision {
        let inventory: ([TargetSession], [String: SessionState])
        if let supplied = attachmentInventoryForTesting {
            inventory = supplied
        } else if Thread.isMainThread {
            inventory = (SessionWatch.shared.targets, SessionWatch.shared.states)
        } else {
            inventory = DispatchQueue.main.sync {
                (SessionWatch.shared.targets, SessionWatch.shared.states)
            }
        }
        lock.lock()
        let live = Array(tasks.values)
        let roles = rolesByTerminal
        lock.unlock()
        return attachmentDecision(sessionID: sessionID, assistant: assistant,
                                  sessions: inventory.0, states: inventory.1,
                                  tasks: live, roles: roles,
                                  isChoosing: Targets.isChoosing,
                                  excluding: excludedTaskID)
    }

    /// Runs on the server queue. Everything filesystem- and process-shaped is safe there — the
    /// `/start` route has always called `StartPoints.start` from it.
    /// Where a respawned task came from, handed to `dispatch` rather than read out of
    /// `task.json`. Deliberately not a public field: the chain position is the broker's own
    /// count, and a caller writing it into a task file could reset it to zero.
    struct RespawnOrigin: Equatable {
        let taskID: String
        let generation: Int
    }

    static func dispatch(taskID: String, secret: String, schedule: Schedule? = nil,
                         respawn: RespawnOrigin? = nil) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
        }
        guard isTaskID(taskID) else {
            return .refused(422, "bad_task", "task_id must be a lowercase UUID.")
        }
        // Same task again is the same answer again: the root retrying a dispatch that already
        // landed must not spawn a second child.
        if let existing = held(taskID) { return successfulDispatchReply(for: existing) }
        guard isTaskSecret(secret) else {
            return .refused(422, "bad_task", "secret must be 64 hex characters.")
        }
        guard let rateTicket = takeDispatchRate() else {
            return .refused(429, "rate_limited", "Too many dispatches; wait a few minutes.")
        }

        let file = root.appendingPathComponent(taskID, isDirectory: true)
            .appendingPathComponent("task.json")
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return .refused(422, "bad_task", "No readable task.json under /tmp/.clawdline/<task_id>/.")
        }
        var made: Draft
        switch draft(from: obj, expecting: taskID) {
        case .bad(let why): return .refused(422, "bad_task", why)
        case .ok(let ok): made = ok
        }
        let identityEvidence = rootIdentityEvidenceForTesting
            ?? activeRootIdentityEvidence(claimed: made.rootSessionId)
                + Coordinator.rootIdentityEvidence(claimed: made.rootSessionId)
        if let refusal = rootIdentityRefusal(claimed: made.rootSessionId,
                                             evidence: identityEvidence) {
            refundDispatchRate(rateTicket)
            return refusal
        }

        let rootBinding = canonicalRootSession(
            made.rootSessionId, assistant: made.rootAssistant,
            among: rootTargets(), sessionID: Transcript.sessionID(of:))
        made.rootSessionId = rootBinding.sessionID
        let rootWarnings = rootBinding.warning.map { [$0] } ?? []

        var attachedSession: TargetSession?
        var attachedDepth: Int?
        if let sessionID = made.attachSessionId {
            switch resolveAttachment(sessionID: sessionID, assistant: made.assistant,
                                     excluding: taskID) {
            case .refused(let status, let code, let message):
                refundDispatchRate(rateTicket)
                return .refused(status, code, message)
            case .accepted(let session, let depth):
                attachedSession = session
                attachedDepth = depth
            }
        }
        // How deep this one sits. A dispatch names who asked, and if who asked is itself a live
        // child then this task is one level below that child's. Best-effort in the sense that a
        // caller can lie about its identity — but lying only ever moves a task *down* (the two
        // signals are combined by taking the deeper answer) or into somebody else's bucket, and
        // `orchestratorMaxDescendants` sits over the whole machine either way.
        let depth = attachedDepth
            ?? depthOfNew(parentTask: made.parentTaskId, rootSession: made.rootSessionId)
        if attachedSession == nil, !depthIsAllowed(depth) {
            return .refused(409, "depth_exceeded",
                            "A child session cannot dispatch tasks of its own. Work that needs "
                          + "to run in parallel belongs to your assistant's own subagents.")
        }
        // One level, so the only dispatcher a new tab can hang under is a root.
        let cap = Config.shared.orchestratorMaxChildren
        if attachedSession == nil,
           activeCount(dispatchedBy: made.rootSessionId, parentTask: made.parentTaskId) >= cap {
            return .refused(status: 429, code: "over_capacity",
                            message: "All \(cap) child slots for this session are busy; "
                                   + "retry when one finishes.",
                            extra: ["retry_after": 60])
        }
        // And the ceiling over everyone. The per-dispatcher caps are what one session may spend;
        // this is what the Mac may, and it is the one a caller cannot talk its way around by
        // claiming to be somebody else.
        let ceiling = Config.shared.orchestratorMaxDescendants
        if attachedSession == nil, activeCount() >= ceiling {
            return .refused(status: 429, code: "over_capacity",
                            message: "All \(ceiling) child sessions on this Mac are busy; "
                                   + "retry when one finishes.",
                            extra: ["retry_after": 60])
        }

        // The named assistant's own quota is a fact the broker already knows too — one
        // 5-second-cached read of at most two local files (`AssistantQuota.current(for:)`),
        // cheaper than the git subprocesses right after this. `exhausted` is the one answer that
        // is not "maybe trouble" but "this tab is opening to die": the night this was built, that
        // cost a tab, a cold start, a `spawn_failed`/timeout wait, and thirteen files frozen on a
        // shared index waiting for somebody else to commit them. So it refuses rather than warns,
        // with the one override a stale or soon-to-be-moot reading needs — see
        // docs/orchestrator.md and the Q1 design's §D.
        var quotaWarnings: [[String: Any]] = []
        let quota = AssistantQuota.current(for: made.assistant)
        switch quota.availability {
        case .exhausted where !made.ignoreQuota:
            refundDispatchRate(rateTicket)
            return assistantExhaustedReply(quota)
        case .exhausted:
            // `ignore_quota` was set: the reading stands, but the caller said to send it anyway.
            quotaWarnings.append(assistantOverrideWarning(quota, timeoutMinutes: made.timeoutMinutes))
        case .low:
            quotaWarnings.append(assistantLowWarning(quota))
        case .ok, .unknown:
            break   // `unknown` dispatches quietly — no signal is not bad news. See §D.1.
        }

        // Git validation costs several subprocesses. Capacity and depth are facts the broker
        // already knows, so answer those refusals before asking a repository anything.
        var preparedWorktree: Worktree?
        var worktreeWarnings: [[String: Any]] = []
        if made.isolation == .worktree {
            switch prepareWorktree(for: made, taskID: taskID,
                                   queued: !made.serialize.isEmpty) {
            case .ready(let worktree, let warnings):
                preparedWorktree = worktree
                worktreeWarnings = warnings
            case .bad(let why):
                return .refused(422, "bad_task", why)
            case .unavailable(let why):
                return .refused(409, "worktree_unavailable", why)
            }
        }

        // The ceiling is the default as well as the limit: a task that said nothing gets it, and
        // one that asked for more is quietly given it instead. Quietly on purpose — the work is
        // still worth doing more carefully, and a refusal here would only teach callers to ask
        // for less than they need. The record says what was actually used.
        let allowed = Config.shared.orchestratorPermissionCeiling
        let permission = min(made.permission ?? allowed, allowed)

        var task = Task(id: taskID, state: .queued, kind: made.kind, title: made.title,
                        assistant: made.assistant, model: made.model,
                        reasoningEffort: made.reasoningEffort,
                        permission: permission, projectDir: made.projectDir,
                        timeoutMinutes: made.timeoutMinutes, created: Date(),
                        rootSessionId: made.rootSessionId,
                        rootAssistant: made.rootAssistant,
                        rootLabel: schedule?.title ?? made.rootLabel,
                        depth: depth, parentTaskId: made.parentTaskId, plan: made.plan,
                        serialize: made.serialize, claims: made.claims,
                        claimsDeclared: made.claimsDeclared,
                        secretHash: hash(ofSecret: secret))
        task.scheduleID = schedule?.id
        task.scheduleCloseTab = schedule?.closeTab ?? .onSuccess
        task.scheduleNotifyFailure = schedule?.notifyOnFailure ?? true
        task.respawnOf = respawn?.taskID
        task.respawnGeneration = respawn?.generation ?? 0
        task.isolation = made.isolation
        task.worktree = preparedWorktree
        task.attachSessionId = made.attachSessionId
        // `resolveAttachment` accepts only a host whose launch-time grant covers the task root.
        // A guest inherits that property while it temporarily supplies the session's live role.
        task.childTaskRootAccess = made.attachSessionId != nil
        worktreeWarnings += prepareClaimsForIsolation(&task)
        task.claimKeys = freezeClaims(task.claims, projectDir: task.projectDir)
        if !task.serialize.isEmpty {
            guard let sealed = sealQueuedSecret(secret) else {
                return .refused(500, "internal", "Could not protect the queued task secret.")
            }
            task.queuedSecret = sealed
        }

        // Claims are checked and registered under the same lock. If those were separate steps,
        // two concurrent dispatches could both observe a free path and then both reserve it.
        // Queued serialized work enters here too: reservation starts at dispatch, not promotion.
        lock.lock()
        if let sessionID = made.attachSessionId,
           tasks.values.contains(where: {
               $0.id != taskID && !$0.state.isTerminal
                   && ($0.childTerminalId == sessionID || $0.attachSessionId == sessionID)
           }) {
            lock.unlock()
            refundDispatchRate(rateTicket)
            return .refused(409, "attach_session_occupied",
                            "That session already has a live Clawdline task.")
        }
        let claimsOverlaps = claimsOverlapsLocked(for: task)
        if let blocker = claimsOverlaps.first(where: \.blocks) {
            lock.unlock()
            refundDispatchRate(rateTicket)
            RemoteAuth.audit("orchestrator.claims.blocked", [
                "task": taskID,
                "blocking_task": blocker.task.id,
                "root": made.rootLabel ?? made.rootSessionId ?? "unknown",
                "conflicts": blocker.paths.joined(separator: ","),
            ])
            return .refused(status: 409, code: "workspace_busy",
                            message: "Another dispatch tree has reserved a path this task claims.",
                            extra: workspaceBusyExtra(blocker))
        }
        tasks[taskID] = task
        secrets[taskID] = secret
        lock.unlock()
        RemoteAuth.audit("orchestrator.dispatch", ["task": taskID, "assistant": made.assistant.rawValue,
                                                   "cwd": made.projectDir, "kind": made.kind,
                                                   "depth": String(depth),
                                                   "model": made.model ?? "default",
                                                   "reasoning_effort": made.reasoningEffort?.rawValue ?? "default",
                                                   "permission": permission.rawValue,
                                                   "isolation": made.isolation.rawValue,
                                                   "attach_session": made.attachSessionId ?? "new_tab"])
        if let session = made.attachSessionId {
            RemoteAuth.audit("orchestrator.attach", ["task": taskID, "session": session,
                                                       "assistant": made.assistant.rawValue])
        }

        // Straight away rather than on the next beat: the root is holding its breath on this
        // request, and the answer should already say whether a terminal opened or which older
        // serialized work left this task queued.
        let needsPump = !task.serialize.isEmpty
        var attachDeliveryFailed = false
        if !needsPump {
            let opened = spawn(task, attachedSession: attachedSession)
            // Attached delivery is the one failure here that goes through `finalize`, because
            // `502 attach_delivery_failed` carries the finished record back in its own reply.
            //
            // A tab that would not open keeps the recording it always had. `finalize` is the
            // right place for a task that ran, and the wrong one for a dispatch that failed in
            // the caller's own request: it types a "task finished (spawn_failed)" line into the
            // root's terminal, mid-turn, about the very answer the root is at this moment waiting
            // for in the HTTP response — and then cancels descendants that cannot exist yet and
            // disposes a worktree the refusal has already disposed.
            if opened.state == .spawnFailed, made.attachSessionId != nil {
                let fail = {
                    finalize(taskID, as: .spawnFailed, summary: opened.summary)
                }
                if Thread.isMainThread { fail() } else { DispatchQueue.main.sync(execute: fail) }
                task = held(taskID) ?? opened
                attachDeliveryFailed = true
            } else {
                task = opened
                // This refusal does not run task-finalization side effects, but the terminal
                // record still participates in the two ordinary reclaim schedules. A tab that
                // never opened normally has no `work/`, and `spawn` already attempted to dispose
                // its worktree; retaining the deadlines keeps the terminal-state contract whole
                // when either directory nevertheless exists.
                if task.state == .spawnFailed {
                    task.workCleanupAt = reclaimDeadline(
                        minutes: Config.shared.orchestratorWorkGraceMinutes,
                        outcome: .spawnFailed)
                    task.buildCleanupAt = task.worktree == nil ? nil : reclaimDeadline(
                        minutes: Config.shared.orchestratorBuildGraceMinutes,
                        outcome: .spawnFailed)
                }
                _ = replaceTask(task, expecting: .queued,
                                discardSecret: task.state.isTerminal)
            }
        }
        save()
        DispatchQueue.main.async { SessionWatch.shared.nudge() }
        RemoteServer.shared.broadcastOrchestrator()
        if attachDeliveryFailed {
            return .refused(status: 502, code: "attach_delivery_failed",
                            message: task.summary ?? "The attached briefing could not be typed.",
                            extra: ["task": existingRecord(taskID) ?? [:]])
        }
        let reply = successfulDispatchReply(for: task, notify: true,
                                             claimsOverlaps: claimsOverlaps,
                                             additionalWarnings: rootWarnings + quotaWarnings
                                                + worktreeWarnings)
        if needsPump { scheduleSerializePump() }
        return reply
    }

    /// How many retries may descend from one original dispatch.
    ///
    /// `spawn_failed` was 16.5% of every dispatch on the machine this was measured on — 34 of
    /// 206, and 33 of those were Codex — and until this route existed the protocol's answer was
    /// that the root must write the whole `task.json` out again under a fresh id. That is
    /// thirty-four rewrites by the most context-loaded session in the tree. Two is enough to get
    /// past a terminal that would not open, and few enough that a tab failing for a real reason
    /// stops being retried instead of looping.
    static let respawnLimit = 2

    /// Retry a dispatch whose tab never opened, without making the root write the task out again.
    ///
    /// A fresh id and a fresh secret, because the old id is finished — re-sending it returns the
    /// terminal record and opens nothing. Everything else comes from the original `task.json`,
    /// which is the only place `instructions` was ever written down: it is copied with the id
    /// swapped, so plan, claims, serialize, assistant, model, project directory, isolation and
    /// the root binding all arrive unchanged.
    ///
    /// `secret` may be supplied by the caller exactly as it is for an ordinary dispatch; when it
    /// is not, the broker mints one and the reply carries it.
    static func respawn(taskID: String, secret supplied: String? = nil) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
        }
        guard isTaskID(taskID) else {
            return .refused(422, "bad_task", "task_id must be a lowercase UUID.")
        }
        guard let origin = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        // Only the one terminal state that means "nothing ran". A `failure` is an answer, a
        // `timeout` had a session that read the briefing, and a `cancelled` was somebody's
        // decision — copying any of those into a new tab would be re-running work, not retrying
        // a dispatch.
        guard origin.state == .spawnFailed else {
            return .refused(status: 409, code: "not_respawnable",
                            message: "Only a spawn_failed task may be respawned; task \(taskID) "
                                   + "is \(origin.state.rawValue).",
                            extra: ["state": origin.state.rawValue])
        }
        let family = respawnFamily(of: origin)
        guard family.descendants < respawnLimit else {
            return .refused(status: 409, code: "respawn_exhausted",
                            message: "Task \(family.original) has already been respawned "
                                   + "\(family.descendants) times; the limit is "
                                   + "\(respawnLimit). Dispatch a new task, or find out why the "
                                   + "tab will not open.",
                            extra: ["original_task": family.original,
                                    "respawns": family.descendants,
                                    "limit": respawnLimit])
        }
        if let supplied, !isTaskSecret(supplied) {
            return .refused(422, "bad_task", "secret must be 64 hex characters.")
        }
        guard let data = try? Data(contentsOf: origin.dir.appendingPathComponent("task.json")),
              var obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return .refused(422, "bad_task",
                            "The original task.json is gone, so there is nothing to respawn from; "
                            + "write a new one.")
        }
        guard let secret = supplied ?? freshTaskSecret() else {
            return .refused(500, "internal", "Could not create the respawned task secret.")
        }
        let fresh = UUID().uuidString.lowercased()
        obj["task_id"] = fresh
        let directory = root.appendingPathComponent(fresh, isDirectory: true)
        do {
            try FileManager.default.createDirectory(
                at: directory.appendingPathComponent("artifacts", isDirectory: true),
                withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                  ofItemAtPath: directory.path)
            let file = directory.appendingPathComponent("task.json")
            try JSONSerialization.data(withJSONObject: obj,
                                       options: [.prettyPrinted, .sortedKeys,
                                                 .withoutEscapingSlashes]).write(to: file,
                                                                                 options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: file.path)
        } catch {
            return .refused(500, "internal", "Could not write the respawned task file.")
        }
        let reply = dispatch(taskID: fresh, secret: secret,
                             respawn: RespawnOrigin(taskID: taskID,
                                                    generation: origin.respawnGeneration + 1))
        // The caller needs the secret it did not choose. A dispatch that was refused leaves the
        // directory behind for the ordinary sweep, exactly as a root's own abandoned attempt does.
        guard case .ok(var payload) = reply else { return reply }
        // Audited after the dispatch rather than before it. Written first, the line records
        // retries that never happened — an `over_capacity` refusal opens no tab — and an audit
        // that reads bigger than the thing it audits is worse than none.
        RemoteAuth.audit("orchestrator.respawn", ["task": fresh, "from": taskID,
                                                  "original": family.original,
                                                  "generation": String(origin.respawnGeneration + 1)])
        payload["secret"] = secret
        payload["respawn_of"] = taskID
        payload["original_task"] = family.original
        return .ok(payload)
    }

    /// The first task in `task`'s respawn chain, and how many tasks the registry holds that
    /// descend from it.
    ///
    /// The cap is on the family, not on any one chain: *at most two respawns descend from one
    /// original*. A chain depth cannot enforce that, because `respawn` writes nothing back to the
    /// task it retried — a `spawn_failed` original stays at generation zero however many times it
    /// is respawned, so counting its own depth lets the same original be retried for ever. That
    /// is also the shape the caller falls into, since the id a root has in hand is the one that
    /// failed: `curl …/$FAILED_ID/respawn`, again, and again.
    ///
    /// Both walks stop at a record the registry no longer holds and at a cycle it should never
    /// contain, so a swept ancestor shortens the chain rather than hanging the walk, and a swept
    /// retry is not counted — the count is over what is still known, exactly as the chain is.
    private static func respawnFamily(of task: Task) -> (original: String, descendants: Int) {
        load()
        lock.lock()
        let parents = tasks.mapValues(\.respawnOf)
        lock.unlock()
        // `parents[id]` is doubly optional on purpose: `.some(nil)` is a held original, `nil` is a
        // task the registry has forgotten, and only the first may be walked through.
        func originOf(_ id: String) -> String {
            var current = id
            var seen: Set<String> = [current]
            while let previous = parents[current] ?? nil, !seen.contains(previous),
                  parents[previous] != nil {
                seen.insert(previous)
                current = previous
            }
            return current
        }
        let original = originOf(task.id)
        let descendants = parents.keys.filter { $0 != original && originOf($0) == original }.count
        return (original, descendants)
    }

    /// One response builder for both the first request and an idempotent retry. The scan happens
    /// after spawn so a task that failed to open is already terminal and produces no warning.
    /// Said when `claims` was absent from `task.json` — never when it was present and empty.
    ///
    /// 60.7% of the dispatches measured on this machine declared nothing at all. Declaring costs
    /// the root about twenty output tokens; a collision costs a whole task, which on that same
    /// record is three to eighteen million. **The difference between absent and `[]` is the
    /// whole point**: an empty list is a positive declaration that the task writes nothing, and
    /// warning about it would teach callers that the field is noise. A warning either way, never
    /// a refusal — a root that has not worked out its write set yet should still be able to
    /// dispatch.
    static func claimsMissingWarning() -> [String: Any] {
        [
            "code": "claims_missing",
            "message": "This task declared no claims, so nothing reserves the paths it is about "
                + "to write and no other root can be told to stay off them. Add \"claims\" to "
                + "task.json — the relative paths this task may write — or \"claims\": [] to say "
                + "it writes nothing.",
        ]
    }

    private static func successfulDispatchReply(for task: Task, notify: Bool = false,
                                                claimsOverlaps: [ClaimsOverlap]? = nil,
                                                additionalWarnings: [[String: Any]] = []) -> Reply {
        guard let record = existingRecord(task.id) else {
            return .refused(500, "internal", "The task was lost while being made.")
        }
        // On the idempotent retry as well as the first request: the same task is still the one
        // that did not say what it writes.
        let additionalWarnings = task.claimsDeclared
            ? additionalWarnings
            : additionalWarnings + [claimsMissingWarning()]
        let overlaps = workspaceOverlaps(for: task)
        if notify { notifyWorkspaceOverlaps(newTask: task, overlaps: overlaps) }
        let claimWarnings: [ClaimsOverlap]
        if let claimsOverlaps {
            claimWarnings = claimsOverlaps
        } else {
            lock.lock()
            claimWarnings = claimsOverlapsLocked(for: task)
            lock.unlock()
        }
        return .ok(dispatchPayload(record: record, taskID: task.id, overlaps: overlaps,
                                   claimsOverlaps: claimWarnings,
                                   additionalWarnings: additionalWarnings))
    }

    typealias TaskStarter = (StartPoints.Place, Assistant, String?, ReasoningEffort?,
                             Permission, String?) -> StartPoints.Outcome

    /// Internal so the final task-to-terminal wiring can be mutation-tested without opening a
    /// real tab. The default remains the single production path into `StartPoints.start`.
    static func spawn(_ task: Task, attachedSession: TargetSession? = nil,
                      start: TaskStarter? = nil) -> Task {
        let start = start ?? taskStarterForTesting
            ?? { place, assistant, model, effort, permission, addDir in
                StartPoints.start(place, assistant: assistant, model: model,
                                  reasoningEffort: effort, permission: permission,
                                  addDir: addDir)
            }
        var task = task
        task.queuedSecret = nil
        if task.attachSessionId != nil {
            return spawnAttached(task, resolvedSession: attachedSession)
        }
        if let prepared = task.worktree {
            guard let worktree = resolveSpawnBase(in: prepared) else {
                task.state = .spawnFailed
                task.summary = "The worktree base no longer resolves to a commit."
                task.finishedAt = Date()
                return task
            }
            task.worktree = worktree
            if let failure = addWorktree(worktree, taskID: task.id) {
                task.state = .spawnFailed
                task.summary = String(failure.prefix(500))
                task.finishedAt = Date()
                return task
            }
        }
        let workingDirectory = cwd(of: task)
        let place = StartPoints.Place(id: StartPoints.id(for: workingDirectory),
                                      path: workingDirectory,
                                      label: task.title, at: Date())
        // A place this session may reach, because everything it was sent to do is outside the
        // project its tab was opened in. The briefing, the task file, the artifacts: all of it
        // lives under /tmp/.clawdline, and reaching outside the working directory is a boundary
        // an assistant asks about before crossing. Nobody is watching a child's tab to answer,
        // so without this the first question is where the task stops — and the first question is
        // *"may I read my own instructions?"*, before a single line of the work.
        //
        // **How much of it is the launch-time grant that decides whether this tab can ever be
        // handed a second task.** A child dispatches nothing now, so the wider grant is no
        // longer about opening sessions: it is what lets a standing session read the sibling
        // `CHILD.md` of a follow-up task whose directory did not exist when this tab opened —
        // see `attachmentDecision`, which refuses a session that was launched without it, and
        // `--add-dir` cannot be added to a running process. A task deeper than the floor never
        // opens a tab at all, so it never reaches this line with anything to grant.
        let taskRootGrant = depthIsAllowed(task.depth)
        switch start(place, task.assistant, task.model, task.reasoningEffort, task.permission,
                     taskRootGrant ? root.path : task.dir.path) {
        case .refused(_, let code, let message, _):
            task.state = .spawnFailed
            task.summary = "\(code): \(message)"
            task.finishedAt = Date()
            if let worktree = task.worktree {
                disposeWorktree(worktree, taskID: task.id, why: "spawn_failed")
            }
        case .started(let id, let backend):
            task.state = .spawning
            task.spawnedAt = Date()
            task.childTerminalId = id
            task.childBackend = backend
            task.childTaskRootAccess = taskRootGrant
        }
        return task
    }

    /// Deliver the ordinary first line into a tab this task did not open. The task remains in
    /// `spawning` until the same transcript receipt as a new-tab dispatch proves the turn landed.
    private static func spawnAttached(_ snapshot: Task,
                                      resolvedSession: TargetSession?) -> Task {
        var task = snapshot
        guard let sessionID = task.attachSessionId else { return task }
        let session: TargetSession
        if let resolvedSession {
            session = resolvedSession
        } else {
            switch resolveAttachment(sessionID: sessionID, assistant: task.assistant,
                                     excluding: task.id) {
            case .accepted(let found, _): session = found
            case .refused(_, let code, let message):
                task.state = .spawnFailed
                task.summary = "\(code): \(message)"
                task.finishedAt = Date()
                return task
            }
        }
        guard let secret = heldSecret(task.id) else {
            task.state = .spawnFailed
            task.summary = "The task's secret was lost before attached delivery."
            task.finishedAt = Date()
            return task
        }
        writeChildBrief(for: task)
        let line = firstLine(id: task.id, secret: secret, announce: L.t.childAnnounce(task.title))
        let sentAt = Date()
        let failure: String?
        if let sender = attachedSenderForTesting { failure = sender(line, session) }
        else { failure = Targets.send(line, to: session) }
        guard failure == nil else {
            task.state = .spawnFailed
            task.summary = "Could not type into the attached session: \(failure!)"
            task.finishedAt = Date()
            return task
        }
        task.state = .spawning
        task.spawnedAt = sentAt
        task.childTerminalId = session.id
        task.childBackend = session.backend
        task.childTTY = session.tty
        task.injectAttempts = 1
        task.lastInjectAt = sentAt
        RemoteAuth.audit("orchestrator.attach.inject", ["task": task.id,
                                                          "session": session.id,
                                                          "attempt": "1"])
        return task
    }

    /// Move one eligible waiter to `spawning` under the scheduling lock before opening anything.
    /// That state change is the atomic acquisition of all its names. Persisting it first also
    /// makes a crash fail closed: startup will not open a second tab for an operation that may
    /// already have crossed the external side-effect boundary.
    private static func startQueuedTaskIfEligible(_ id: String) -> Task? {
        lock.lock()
        guard let snapshot = tasks[id], snapshot.state == .queued,
              serializeBlockersLocked(for: snapshot).isEmpty else {
            lock.unlock()
            return nil
        }
        let inMemorySecret = secrets[id]
        lock.unlock()

        let clear = inMemorySecret ?? snapshot.queuedSecret.flatMap(openQueuedSecret)
        guard let clear,
              RemoteAuth.constantTimeEquals(snapshot.secretHash, hash(ofSecret: clear)) else {
            let fail = {
                finalize(id, as: .spawnFailed,
                         summary: "The queued task secret could not be recovered.",
                         pumpQueue: false)
            }
            if Thread.isMainThread { fail() } else { DispatchQueue.main.sync(execute: fail) }
            return held(id)
        }

        lock.lock()
        guard var starting = tasks[id], starting.state == .queued,
              serializeBlockersLocked(for: starting).isEmpty else {
            lock.unlock()
            return nil
        }
        starting.state = .spawning
        starting.queuedSecret = nil
        tasks[id] = starting
        secrets[id] = clear
        lock.unlock()
        save()

        // A queued task has not touched the workspace and is excluded from L1. Promotion is the
        // first moment its warning is meaningful, so queued dispatches receive it as a typed line
        // instead of in the HTTP response that has already gone back.
        let overlaps = workspaceOverlaps(for: starting)
        notifyWorkspaceOverlaps(newTask: starting, overlaps: overlaps)

        let opened = spawn(starting)
        if opened.state == .spawnFailed {
            // The dispatch that returned `202 queued` is long over. Unlike an immediate
            // tab-opening refusal, nobody is holding an HTTP response that reports this ending;
            // run the ordinary finalization tail so audit, batch accounting and the typed root
            // notice all happen. The outer pump pass, not finalize, chooses the next waiter.
            let fail = {
                finalize(id, as: .spawnFailed, summary: opened.summary, pumpQueue: false)
            }
            if Thread.isMainThread { fail() } else { DispatchQueue.main.sync(execute: fail) }
            return held(id)
        }
        guard replaceTask(opened, expecting: .spawning) else { return nil }
        return held(id)
    }

    /// Start every queued serialized task that can acquire all of its names together. A pass may
    /// start several disjoint operations; FIFO keeps a later user behind every older shared name.
    @discardableResult
    private static func pumpSerializeQueue() -> Bool {
        var changed = false
        while true {
            lock.lock()
            let next = tasks.values
                .filter { $0.state == .queued && !$0.serialize.isEmpty }
                .sorted {
                    if $0.created == $1.created { return $0.id < $1.id }
                    return $0.created < $1.created
                }
                .first { serializeBlockersLocked(for: $0).isEmpty }
            lock.unlock()
            guard let next else { break }
            guard startQueuedTaskIfEligible(next.id) != nil else { continue }
            changed = true
        }
        if changed {
            save()
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            RemoteServer.shared.broadcastOrchestrator()
        }
        return changed
    }

    /// Serialize pumps rather than opening on their callers. The outer pass is responsible for
    /// the next waiter; a pump-triggered tab-opening refusal still finalizes because the queued
    /// dispatch response is already gone and root otherwise receives no completion signal.
    private static func scheduleSerializePump() {
        serializePumpQueue.async {
            let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: "serialize-promotion") {
                _ = pumpSerializeQueue()
            }
            if !admitted {
                lock.lock()
                let waiting = tasks.values.contains { $0.state == .queued && !$0.serialize.isEmpty }
                lock.unlock()
                if waiting {
                    serializePumpQueue.asyncAfter(deadline: .now() + 0.25) {
                        scheduleSerializePump()
                    }
                }
            }
        }
    }

    /// Ten dispatches in ten minutes, or one full tree's worth, whichever is more.
    ///
    /// The window is a brake on a loop, not a second capacity cap — and once a child can dispatch
    /// too, filling the allowed tree legitimately takes more than ten calls. A limit that refuses
    /// the work the caps just permitted teaches people to retry, which is the behaviour it exists
    /// to discourage.
    static func takeDispatchRate() -> Date? {
        lock.lock(); defer { lock.unlock() }
        let now = Date()
        let allowed = max(10, Config.shared.orchestratorMaxDescendants)
        dispatchTimes = dispatchTimes.filter { now.timeIntervalSince($0) < 600 }
        guard dispatchTimes.count < allowed else { return nil }
        dispatchTimes.append(now)
        return now
    }

    /// A claims refusal registered no work, so its provisional rate entry is returned. Every real
    /// dispatch, including a scheduled one, is served on the remote serial queue; matching the
    /// exact timestamp also keeps direct unit-test calls safe.
    static func refundDispatchRate(_ ticket: Date) {
        lock.lock(); defer { lock.unlock() }
        if let index = dispatchTimes.lastIndex(of: ticket) {
            dispatchTimes.remove(at: index)
        }
    }

    /// Test-only: how many dispatch-rate tickets are currently held, so a test can confirm a
    /// refusal actually gave its ticket back rather than trusting the code path ran unobserved.
    static func dispatchRateCountForTesting() -> Int {
        lock.lock(); defer { lock.unlock() }
        return dispatchTimes.count
    }

    private static func activeCount() -> Int {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.filter { !$0.state.isTerminal }.count
    }

    struct CoordinatorSessionObservation {
        let identity: SessionWorkIdentity
        let terminalState: SessionState
    }

    struct CoordinatorSessionFacts {
        let work: SessionWorkProjection
        let coordination: Coordination
    }

    /// One Orchestrator-registry observation for Bearings. Facts stay positional so a malformed
    /// duplicate terminal id cannot make one SessionWatch row silently replace another.
    struct CoordinatorSnapshot {
        let observedAt: Date
        let sessions: [CoordinatorSessionFacts]
        let activeTasks: Int
        let pendingLandings: Int
        let openWaits: Int
    }

    /// Build every Orchestrator-derived Bearings fact during one registry lock window. The
    /// SessionWatch inventory was observed separately by the caller; that cross-source boundary
    /// is represented by distinct timestamps/provenance in the response rather than called
    /// transactional.
    static func coordinatorSnapshot(_ observations: [CoordinatorSessionObservation],
                                    now: Date = Date()) -> CoordinatorSnapshot {
        load()
        lock.lock(); defer { lock.unlock() }
        return CoordinatorSnapshot(
            observedAt: now,
            sessions: observations.map {
                CoordinatorSessionFacts(
                    work: sessionWorkProjectionLocked(
                        identity: $0.identity, terminalState: $0.terminalState),
                    coordination: coordinationLocked(forTerminal: $0.identity.terminalID))
            },
            activeTasks: tasks.values.filter { !$0.state.isTerminal }.count,
            pendingLandings: tasks.values.filter { $0.landing?.state == .pending }.count,
            openWaits: coordinationWaits.values.filter { wait in
                wait.waiters.contains { $0.releaseDeliveredAt == nil }
            }.count)
    }

    /// The live tasks already dispatched by whoever is asking now.
    ///
    /// Two ways of naming the same session, either of which is enough: the task it is the child
    /// of, and the session id it calls itself by. A dispatch that gives neither is nobody's in
    /// particular, and shares a bucket with every other anonymous one — which is the right answer
    /// for a caller that declined to say who it is, and the ceiling covers the rest.
    private static func activeCount(dispatchedBy session: String?, parentTask: String?) -> Int {
        lock.lock(); defer { lock.unlock() }
        return tasks.values.filter { task in
            guard !task.state.isTerminal else { return false }
            if let parentTask, task.parentTaskId == parentTask { return true }
            if let session { return task.rootSessionId == session }
            return task.rootSessionId == nil && task.parentTaskId == nil
        }.count
    }

    /// The depth a task dispatched right now would sit at: one below its parent, or 1 when the
    /// caller is not a child of anything this app is running.
    ///
    /// **The deeper of the two answers wins.** A child names its parent task and, when it can,
    /// its own session id; either alone identifies it. Taking the maximum means a caller that
    /// gets one of them wrong — or invents one — can only end up further down, never nearer the
    /// top, so the mistake costs it capacity instead of buying any.
    private static func depthOfNew(parentTask: String?, rootSession: String?) -> Int {
        lock.lock(); defer { lock.unlock() }
        var parent = 0
        for task in tasks.values where !task.state.isTerminal {
            let isParent = (parentTask != nil && task.id == parentTask)
                || (rootSession != nil && task.childSessionId == rootSession)
            if isParent { parent = max(parent, task.depth) }
        }
        return parent + 1
    }

    // MARK: - Completion over HTTP

    static func complete(taskID: String, secret: String, status: String, summary: String) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !secret.isEmpty,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            RemoteAuth.audit("orchestrator.complete", ["task": taskID, "ok": "0", "why": "bad_secret"])
            return .refused(403, "forbidden", "That is not this task's secret.")
        }
        guard !task.state.isTerminal else {
            return .refused(409, "already_done", "That task already finished.")
        }
        let outcome: State = status == "success" ? .success : .failure
        let words = summary.isEmpty ? nil : String(summary.prefix(2000))
        DispatchQueue.main.async { finalize(taskID, as: outcome, summary: words) }
        return .ok(["ok": true])
    }

    // MARK: - Agent-authored push notifications

    private static let notifyTaskLimit = 5
    private static let notifyHourlyLimit = 30
    private static let notifyWindow: TimeInterval = 60 * 60
    private static let notifyTerminalGrace: TimeInterval = 60
    private static let notifyCredentialFailureLimit = 3
    private static let notifyCredentialFailureWindow: TimeInterval = 10 * 60

    /// One audit row per attempted agent notification. `task_id` and `root` are deliberately
    /// mutually exclusive so a reader can distinguish a child's narrow credential from a local
    /// script holding the machine credential without parsing the event name.
    private static func auditAgentNotify(taskID: String?, title: String, result: String,
                                         delivery: WebPush.Delivery? = nil) {
        var fields = ["title": String(title.prefix(80)), "result": result]
        if let taskID { fields["task_id"] = taskID } else { fields["root"] = "1" }
        if let delivery {
            fields["sent"] = String(delivery.sent)
            fields["failed"] = String(delivery.failed)
        }
        RemoteAuth.audit("orchestrator.notify", fields)
    }

    /// Before the task secret is proved, no caller-controlled prose reaches the append-only log.
    /// The short digest groups a repeated attempt without preserving its task id or credential.
    private static func unverifiedAgentNotify(taskID: String, secret: String, result: String,
                                              now: Date, status: Int, code: String,
                                              message: String) -> Reply {
        lock.lock()
        notifyCredentialFailureTimes = notifyCredentialFailureTimes.filter {
            now.timeIntervalSince($0) < notifyCredentialFailureWindow
        }
        let allowed = notifyCredentialFailureTimes.count < notifyCredentialFailureLimit
        if allowed { notifyCredentialFailureTimes.append(now) }
        lock.unlock()

        let finalResult = allowed ? result : "rate_limited"
        RemoteAuth.audit("orchestrator.notify", [
            "source": "task_secret",
            "attempt": String(hash(ofSecret: "\(taskID)\u{0}\(secret)").prefix(12)),
            "result": finalResult,
        ])
        guard allowed else {
            return .refused(429, "rate_limited",
                            "Too many task-secret notification attempts. Try again later.")
        }
        return .refused(status, code, message)
    }

    private static func notificationFields(title: String, body: String) -> Reply? {
        guard !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              title.count <= 80 else {
            return .refused(400, "bad_request",
                            "title must be non-empty and at most 80 characters.")
        }
        guard !body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              body.count <= 500 else {
            return .refused(400, "bad_request",
                            "body must be non-empty and at most 500 characters.")
        }
        return nil
    }

    private static func agentNotifyPreferenceRefusal(taskID: String?, title: String) -> Reply? {
        guard !Config.shared.orchestratorAgentNotify else { return nil }
        auditAgentNotify(taskID: taskID, title: title, result: "agent_notify_disabled")
        return .refused(409, "agent_notify_disabled",
                        "Agent notifications were turned off by the user. Turn them on in "
                            + "Settings → Remote.")
    }

    private static func sendAgentPush(source: String, title: String, body: String,
                                      projectDir: String?, tag: String) -> WebPush.Delivery {
        let displayedTitle = "\(source): \(title)"
        let icon = projectDir.flatMap { RemoteIcon.projectPath(for: ProjectIcon.grid(forCwd: $0)) }
        if let observer = agentPushForTesting {
            return observer(displayedTitle, body, "/", tag, icon)
        }
        // This bypasses pushOnFinish because the dedicated orchestratorAgentNotify preference
        // already gates agent-authored content; it is not an automatic finished-state notice.
        return WebPush.sendAndWait(title: displayedTitle, body: body, url: "/", tag: tag,
                                   icon: icon)
    }

    private static func refundAgentNotify(taskID: String?, ticket: Date) {
        lock.lock()
        if let index = notifyTimes.lastIndex(of: ticket) { notifyTimes.remove(at: index) }
        if let taskID, var task = tasks[taskID] {
            task.notifyCount = max(0, task.notifyCount - 1)
            tasks[taskID] = task
        }
        lock.unlock()
        if taskID != nil { save() }
    }

    private static func agentDeliveryReply(taskID: String?, title: String,
                                           delivery: WebPush.Delivery,
                                           ticket: Date) -> Reply {
        guard delivery.total > 0 else {
            refundAgentNotify(taskID: taskID, ticket: ticket)
            auditAgentNotify(taskID: taskID, title: title, result: "not_subscribed",
                             delivery: delivery)
            return .refused(409, "not_subscribed",
                            "No device has asked for notifications yet.")
        }
        let result = delivery.failed == 0 ? "sent" : (delivery.sent == 0 ? "failed" : "partial")
        auditAgentNotify(taskID: taskID, title: title, result: result, delivery: delivery)
        guard delivery.failed == 0 else {
            return .refused(status: 502, code: "push_failed",
                            message: "One or more push services did not accept the notification.",
                            extra: ["sent": delivery.sent, "failed": delivery.failed])
        }
        return .ok(["ok": true, "sent": delivery.sent, "failed": delivery.failed])
    }

    /// A child may speak while it is working and for one short grace period after it reports.
    /// Secret checking intentionally matches `/complete`: both sides are SHA-256 digests and the
    /// equal-length comparison is constant-time.
    static func agentNotify(taskID: String, secret: String, title: String, body: String,
                            now: Date = Date()) -> Reply {
        guard let snapshot = held(taskID) else {
            return unverifiedAgentNotify(taskID: taskID, secret: secret, result: "not_found",
                                         now: now, status: 404, code: "not_found",
                                         message: "No task named that")
        }
        guard !secret.isEmpty,
              RemoteAuth.constantTimeEquals(snapshot.secretHash, hash(ofSecret: secret)) else {
            return unverifiedAgentNotify(taskID: taskID, secret: secret, result: "bad_secret",
                                         now: now, status: 403, code: "forbidden",
                                         message: "That is not this task's secret.")
        }
        if snapshot.state.isTerminal {
            guard let finished = snapshot.finishedAt,
                  now.timeIntervalSince(finished) <= notifyTerminalGrace else {
                auditAgentNotify(taskID: taskID, title: title, result: "expired")
                return .refused(409, "notify_expired",
                                "That task's notification window has expired.")
            }
        }
        if let refusal = agentNotifyPreferenceRefusal(taskID: taskID, title: title) {
            return refusal
        }
        if let refusal = notificationFields(title: title, body: body) {
            auditAgentNotify(taskID: taskID, title: title, result: "invalid")
            return refusal
        }
        if agentPushForTesting == nil, WebPush.subscriptions.isEmpty {
            let delivery = WebPush.Delivery(sent: 0, failed: 0)
            auditAgentNotify(taskID: taskID, title: title, result: "not_subscribed",
                             delivery: delivery)
            return .refused(409, "not_subscribed",
                            "No device has asked for notifications yet.")
        }

        lock.lock()
        guard var current = tasks[taskID] else {
            lock.unlock()
            auditAgentNotify(taskID: taskID, title: title, result: "not_found")
            return .refused(404, "not_found", "No task named that")
        }
        if current.state.isTerminal,
           current.finishedAt == nil
                || now.timeIntervalSince(current.finishedAt ?? .distantPast) > notifyTerminalGrace {
            lock.unlock()
            auditAgentNotify(taskID: taskID, title: title, result: "expired")
            return .refused(409, "notify_expired",
                            "That task's notification window has expired.")
        }
        notifyTimes = notifyTimes.filter { now.timeIntervalSince($0) < notifyWindow }
        if current.notifyCount >= notifyTaskLimit {
            lock.unlock()
            auditAgentNotify(taskID: taskID, title: title, result: "notify_limit")
            return .refused(429, "notify_limit", "That task has sent its five notifications.")
        }
        if notifyTimes.count >= notifyHourlyLimit {
            lock.unlock()
            auditAgentNotify(taskID: taskID, title: title, result: "rate_limited")
            return .refused(429, "rate_limited",
                            "Too many agent notifications; wait for the hourly window.")
        }
        current.notifyCount += 1
        tasks[taskID] = current
        let ticket = now
        notifyTimes.append(ticket)
        lock.unlock()
        save()

        let source = current.scheduleID == nil ? current.title : (current.rootLabel ?? current.title)
        let delivery = sendAgentPush(source: source, title: title, body: body,
                                     projectDir: current.projectDir,
                                     tag: "agent-task-\(taskID)")
        return agentDeliveryReply(taskID: taskID, title: title, delivery: delivery,
                                  ticket: ticket)
    }

    /// Local roots and scripts already proved they are this Mac's user at RemoteServer's token
    /// gate. They share the hourly brake but do not consume any task's five-message allowance.
    static func agentNotify(title: String, body: String, now: Date = Date()) -> Reply {
        if let refusal = agentNotifyPreferenceRefusal(taskID: nil, title: title) {
            return refusal
        }
        if let refusal = notificationFields(title: title, body: body) {
            auditAgentNotify(taskID: nil, title: title, result: "invalid")
            return refusal
        }
        if agentPushForTesting == nil, WebPush.subscriptions.isEmpty {
            let delivery = WebPush.Delivery(sent: 0, failed: 0)
            auditAgentNotify(taskID: nil, title: title, result: "not_subscribed",
                             delivery: delivery)
            return .refused(409, "not_subscribed",
                            "No device has asked for notifications yet.")
        }
        lock.lock()
        notifyTimes = notifyTimes.filter { now.timeIntervalSince($0) < notifyWindow }
        guard notifyTimes.count < notifyHourlyLimit else {
            lock.unlock()
            auditAgentNotify(taskID: nil, title: title, result: "rate_limited")
            return .refused(429, "rate_limited",
                            "Too many agent notifications; wait for the hourly window.")
        }
        let ticket = now
        notifyTimes.append(ticket)
        lock.unlock()
        let delivery = sendAgentPush(source: "Clawdline", title: title, body: body,
                                     projectDir: nil, tag: "agent-root")
        return agentDeliveryReply(taskID: nil, title: title, delivery: delivery,
                                  ticket: ticket)
    }

    // MARK: - Cancel

    static func cancel(taskID: String) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !task.state.isTerminal else {
            return .refused(409, "already_done", "That task already finished.")
        }
        RemoteAuth.audit("orchestrator.cancel", ["task": taskID])
        // Deepest first, for the same reason the root cascade goes that way: whatever this child
        // handed on is work nobody asked for any more, and left alone it would carry on reporting
        // into a tab that is being ended right now.
        for id in liveTasks(under: [taskID]) {
            guard let below = held(id) else { continue }
            RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_cancelled",
                                                     "parent": taskID])
            if below.state == .queued {
                let finish = {
                    finalize(below.id, as: .cancelled, summary: "Cancelled with its parent.",
                             pumpQueue: false)
                }
                if Thread.isMainThread { finish() }
                else { DispatchQueue.main.sync(execute: finish) }
            } else {
                cancelInPlace(below)
            }
        }
        if task.state == .queued {
            let finish = { finalize(task.id, as: .cancelled, summary: "Cancelled.") }
            if Thread.isMainThread { finish() } else { DispatchQueue.main.sync(execute: finish) }
        } else {
            cancelInPlace(task)
        }
        // A running child's polite exit finalizes asynchronously. A waiter has no child to end
        // and is finalized synchronously, so cancelling queued work is immediately observable.
        var record = existingRecord(taskID) ?? [:]
        record["state"] = State.cancelled.rawValue
        return .ok(["ok": true, "task": record])
    }

    // MARK: - Releasing claims early

    /// A root that finished editing some of what it claimed can hand those paths back before its
    /// task reaches a terminal state, so a `409 workspace_busy` blocked on them can retry
    /// immediately instead of waiting for the whole task to end — the fix for a circular wait
    /// that can only break if one side lands early. `paths` names the original relative
    /// declarations to give back; empty or omitted releases everything still held. Idempotent: a
    /// path already released, or one this task never declared, is silently a no-op rather than
    /// an error, so a retried release cannot fail on its own success. Comparison is ancestor and
    /// descendant, exactly the way dispatch-time arbitration compares claims (`sharedClaimPath`):
    /// releasing `Sources` frees every declared claim key under it, and naming a path inside a
    /// directory-shaped claim frees that whole claim key, because a directory claim is one atomic
    /// reservation rather than a set of the files under it. `queued` refuses: that task has not
    /// started writing yet, so giving up its lease while it is still going to run `instructions`
    /// unmodified would open the exact hole claims exist to close — cancel it instead.
    static func releaseClaims(taskID: String, paths: [String], now: Date = Date()) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !task.state.isTerminal else {
            return .refused(409, "already_done", "That task already finished.")
        }
        guard task.state != .queued else {
            return .refused(409, "not_started", "That task has not started writing yet; to "
                            + "stop it, use cancel instead of release.")
        }
        if let invalid = paths.first(where: {
            $0.split(separator: "/", omittingEmptySubsequences: false).contains(where: { $0 == ".." })
        }) {
            return .refused(400, "bad_request", "paths[] must not contain a .. component: \(invalid)")
        }
        let alreadyReleased = Set(task.releasedClaims.map(\.path))
        let requested: Set<String>
        if paths.isEmpty {
            requested = Set(task.claimKeys)
        } else {
            let frozen = freezeClaims(paths, projectDir: task.projectDir)
            requested = Set(task.claimKeys.filter { key in
                frozen.contains { sharedClaimPath($0, key) != nil }
            })
        }
        let newlyReleased = requested.subtracting(alreadyReleased).sorted()
        if !newlyReleased.isEmpty {
            var updated = task
            updated.releasedClaims += newlyReleased.map { ReleasedClaim(path: $0, releasedAt: now) }
            guard replaceTask(updated, expecting: task.state) else {
                return .refused(409, "already_done", "That task already finished.")
            }
            save()
            RemoteServer.shared.broadcastOrchestrator()
            RemoteAuth.audit("orchestrator.claims.released", [
                "task": taskID, "paths": newlyReleased.joined(separator: ","),
            ])
        }
        return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
    }

    // MARK: - Root-owned landing record

    /// Record the obligation that begins after a child delivers and ends only when root lands or
    /// abandons it. A child secret may open/update `pending`; only the machine/root credential may
    /// assert `landed`, and even it must pass repository verification below.
    static func updateLanding(taskID: String, secret: String, orchestratorToken: String? = nil,
                              raw: [String: Any],
                              now: Date = Date()) -> Reply {
        guard let snapshot = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        let taskSecretMatches = !secret.isEmpty
            && RemoteAuth.constantTimeEquals(snapshot.secretHash, hash(ofSecret: secret))
        let machineMatches = verifyDispatch(token: orchestratorToken)
        let requestsLanded = raw["state"] as? String == LandingState.landed.rawValue
        guard requestsLanded ? machineMatches : (taskSecretMatches || machineMatches) else {
            RemoteAuth.audit("orchestrator.landing", ["task": taskID, "ok": "0",
                                                       "why": "bad_credential"])
            let required = requestsLanded
                ? "Only the orchestrator token may record a landed target."
                : "Use this task's secret or the orchestrator token."
            return .refused(403, "forbidden", required)
        }

        let allowed: Set<String> = ["state", "target", "delivery", "commit", "note"]
        let unknown = raw.keys.filter { !allowed.contains($0) }.sorted()
        guard unknown.isEmpty else {
            return .refused(400, "bad_request",
                            "Unknown landing field(s): \(unknown.joined(separator: ", ")).")
        }
        guard let rawState = raw["state"] as? String,
              let requestedState = LandingState(rawValue: rawState) else {
            return .refused(400, "bad_request",
                            "state must be pending, landed, or abandoned.")
        }

        var fields: [String: String] = [:]
        for (name, limit) in [("target", 200), ("delivery", 500),
                              ("commit", 200), ("note", 500)] {
            guard let value = raw[name] else { continue }
            guard let text = value as? String,
                  !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  text.count <= limit else {
                return .refused(400, "bad_request",
                                "\(name) must be a non-empty string of at most \(limit) characters.")
            }
            fields[name] = text
        }
        if requestedState != .landed, fields["commit"] != nil {
            return .refused(400, "bad_request", "commit is valid only when state is landed.")
        }

        load()
        lock.lock()
        guard var current = tasks[taskID] else {
            lock.unlock()
            return .refused(404, "not_found", "No task named that")
        }
        let existing = current.landing
        if existing?.state == .landed, requestedState == .landed {
            lock.unlock()
            return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
        }
        if let existing, (existing.state == .landed || existing.state == .abandoned),
           existing.state != requestedState {
            lock.unlock()
            return .refused(409, "invalid_transition",
                            "A settled obligation cannot move to another state; open a new task.")
        }
        if requestedState != .pending, !current.state.isTerminal {
            lock.unlock()
            return .refused(409, "not_terminal",
                            "Only a terminal task can be marked landed or abandoned.")
        }
        if requestedState == .landed, fields["commit"] == nil {
            lock.unlock()
            return .refused(400, "bad_request", "commit is required when state is landed.")
        }

        let target = fields["target"] ?? existing?.target
        if requestedState == .landed, target == nil {
            lock.unlock()
            return .refused(400, "bad_request",
                            "target is required when state is landed.")
        }

        // Pending and abandoned are declarations, not verification claims; they retain the
        // existing state-machine behaviour and never persist verification-shaped fields.
        if requestedState != .landed {
            current.landing = Landing(
                state: requestedState,
                target: target,
                delivery: fields["delivery"] ?? existing?.delivery,
                ownerRootKey: existing?.ownerRootKey ?? rootKeyDigest(rootKeyLocked(of: current)),
                since: existing?.since ?? now,
                commit: nil,
                note: fields["note"] ?? existing?.note,
                landedAt: nil)
            tasks[taskID] = current
            lock.unlock()

            save()
            RemoteServer.shared.broadcastOrchestrator()
            RemoteAuth.audit("orchestrator.landing", [
                "task": taskID, "ok": "1", "state": requestedState.rawValue,
                "target": current.landing?.target ?? "",
            ])
            return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
        }

        // Git subprocesses must not hold the registry lock. Remember exactly the landing state
        // they verify; the equality check after the subprocesses is the CAS preventing a
        // concurrent pending edit from being overwritten with evidence for its old target.
        let expectedState = current.state
        let expectedLanding = existing
        let projectDir = current.projectDir
        let requestedCommit = fields["commit"]!
        let requestedTarget = target!
        lock.unlock()

        guard let verification = verifyTargetLanding(
                projectDir: projectDir, target: requestedTarget, commit: requestedCommit) else {
            RemoteAuth.audit("orchestrator.landing", [
                "task": taskID, "ok": "0", "why": "target_not_verified",
                "target": requestedTarget,
            ])
            return .refused(409, "unverified_landing",
                            "The commit must resolve in the task repository and be contained "
                            + "by the named local target branch.")
        }

        lock.lock()
        guard var verifiedCurrent = tasks[taskID] else {
            lock.unlock()
            return .refused(404, "not_found", "No task named that")
        }
        if verifiedCurrent.landing?.state == .landed {
            lock.unlock()
            return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
        }
        guard verifiedCurrent.state == expectedState,
              verifiedCurrent.landing == expectedLanding else {
            lock.unlock()
            return .refused(409, "stale_write",
                            "The landing changed while its target was being verified; retry.")
        }

        verifiedCurrent.landing = Landing(
            state: .landed,
            target: requestedTarget,
            delivery: fields["delivery"] ?? existing?.delivery,
            ownerRootKey: existing?.ownerRootKey
                ?? rootKeyDigest(rootKeyLocked(of: verifiedCurrent)),
            since: existing?.since ?? now,
            commit: verification.commit,
            note: fields["note"] ?? existing?.note,
            landedAt: now,
            verificationOrigin: verification.origin,
            verifiedCommit: verification.commit,
            verifiedTargetCommit: verification.targetCommit)
        tasks[taskID] = verifiedCurrent
        lock.unlock()

        save()
        RemoteServer.shared.broadcastOrchestrator()
        RemoteAuth.audit("orchestrator.landing", [
            "task": taskID, "ok": "1", "state": requestedState.rawValue,
            "target": verifiedCurrent.landing?.target ?? "",
            "verified_commit": verification.commit,
            "verified_target_commit": verification.targetCommit,
        ])
        return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
    }

    /// Every unresolved root-owned landing obligation, oldest first. This is a dashboard, not a
    /// gate: reading or ignoring it changes no claim and blocks no dispatch.
    static func landingRecords(now: Date = Date()) -> [[String: Any]] {
        load()
        lock.lock()
        let indexed = tasks
        let rows = tasks.values.compactMap { task -> [String: Any]? in
            guard let landing = task.landing, landing.state == .pending else { return nil }
            let root = rootTask(of: task, among: indexed)
            return [
                "id": task.id,
                "title": task.title,
                "root_key": landing.ownerRootKey,
                "root_label": (root.rootLabel ?? task.rootLabel) as Any? ?? NSNull(),
                "paths": task.claims,
                "since": Int(landing.since.timeIntervalSince1970),
                "age_seconds": ageSeconds(since: landing.since, now: now),
                "target": landing.target as Any? ?? NSNull(),
                "note": landing.note as Any? ?? NSNull(),
            ]
        }.sorted { left, right in
            let first = left["since"] as? Int ?? 0
            let second = right["since"] as? Int ?? 0
            if first == second {
                return (left["id"] as? String ?? "") < (right["id"] as? String ?? "")
            }
            return first < second
        }
        lock.unlock()
        return rows
    }

    // MARK: - Owned storage inventory

    /// The dry-run view for owned storage. It enumerates only ledger receipts; an interactive
    /// session or an unowned directory cannot enter this list by resembling one of our paths.
    static func storageInventory(now: Date = Date()) -> [String: Any] {
        load()
        let ledger = OwnedStorage.readLedger()
        guard case .known(let entries, let malformedLines) = ledger else {
            return [
                "at": Int(now.timeIntervalSince1970),
                "source_state": OwnedStorage.DecisionState.unknown.rawValue,
                "why": "ledger_unreadable",
                "totals": storageTotals(),
                "owned": [],
                "warnings": ["ledger_unreadable"],
                "config": storageConfig(),
            ]
        }

        lock.lock()
        let taskSnapshot = tasks
        lock.unlock()
        let registryReadable: Bool = {
            guard let data = try? Data(contentsOf: storeURL),
                  let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
                  object["tasks"] is [[String: Any]] else {
                return taskSnapshot.isEmpty && !FileManager.default.fileExists(atPath: storeURL.path)
            }
            return true
        }()
        let sessions = OwnedStorage.liveSessions()
        var totals = storageTotals()
        var rows: [[String: Any]] = []

        for entry in entries.sorted(by: {
            if $0.at == $1.at { return $0.path < $1.path }
            return $0.at < $1.at
        }) {
            let task = registryReadable ? taskSnapshot[entry.taskID] : nil
            let taskFacts: OwnedStorage.Source<OwnedStorage.TaskFacts>
            let landing: OwnedStorage.Source<OwnedStorage.Landing>
            let process: OwnedStorage.ProcessStatus
            if let task {
                taskFacts = .known(OwnedStorage.TaskFacts(
                    isTerminal: task.state.isTerminal, createdAt: task.created,
                    finishedAt: task.finishedAt, childPID: task.childPID,
                    childProcStart: task.childProcStart))
                switch task.landing?.state {
                case .pending: landing = .known(.pending)
                case .landed, .abandoned: landing = .known(.settled)
                case nil: landing = .known(.none)
                }
                process = OwnedStorage.processStatus(pid: task.childPID,
                                                     recordedStart: task.childProcStart)
            } else {
                taskFacts = .unreadable
                landing = .unreadable
                process = .unreadable
            }
            let path = OwnedStorage.pathStatus(for: entry)
            var decision = OwnedStorage.evaluate(.init(
                entry: entry, task: taskFacts, landing: landing, sessions: sessions,
                process: process, path: path, now: now))
            let bytes: Int?
            if path == .valid {
                switch OwnedStorage.directorySize(at: entry.path) {
                case .known(let value): bytes = value
                case .unreadable:
                    bytes = nil
                    decision = .init(state: .unknown, why: "size_unreadable", eligibleAt: nil)
                }
            } else {
                bytes = nil
            }

            totals["owned_items", default: 0] += 1
            if let bytes { totals["owned_bytes", default: 0] += bytes }
            else { totals["unknown_size_items", default: 0] += 1 }
            totals["\(decision.state.rawValue)_items", default: 0] += 1
            if let bytes { totals["\(decision.state.rawValue)_bytes", default: 0] += bytes }

            let finished = task?.finishedAt ?? task?.created ?? entry.at
            rows.append([
                "task": entry.taskID,
                "assistant": entry.assistant,
                "session": entry.sessionID,
                "kind": "scratchpad",
                "path": entry.path,
                "proof": entry.proof,
                "registered_at": Int(entry.at.timeIntervalSince1970),
                "bytes": bytes as Any? ?? NSNull(),
                "state": decision.state.rawValue,
                "why": decision.why,
                "age_seconds": ageSeconds(since: finished, now: now),
                "eligible_at": decision.eligibleAt.map {
                    Int($0.timeIntervalSince1970)
                } as Any? ?? NSNull(),
            ])
        }
        totals["malformed_ledger_lines"] = malformedLines.count
        var warnings = malformedLines.map { "ledger_line_\($0)_malformed" }
        if !registryReadable { warnings.append("registry_unreadable") }
        if case .unreadable = sessions { warnings.append("sessions_unreadable") }
        let anyUnknown = rows.contains { $0["state"] as? String == "unknown" }
            || !warnings.isEmpty
        return [
            "at": Int(now.timeIntervalSince1970),
            "source_state": anyUnknown ? OwnedStorage.DecisionState.unknown.rawValue : "known",
            "totals": totals,
            "owned": rows,
            "warnings": warnings,
            "config": storageConfig(),
        ]
    }

    private static func storageTotals() -> [String: Int] {
        [
            "owned_items": 0, "owned_bytes": 0,
            "held_items": 0, "held_bytes": 0,
            "releasable_items": 0, "releasable_bytes": 0,
            "unknown_items": 0, "unknown_bytes": 0,
            "unknown_size_items": 0, "malformed_ledger_lines": 0,
        ]
    }

    private static func storageConfig() -> [String: Any] {
        // GC2 owns the switch and collector settings. GC1 publishes the policy it evaluates and
        // is explicitly non-mutating.
        ["enabled": false, "floor_hours": 12, "untracked_process_floor_hours": 24]
    }

    // MARK: - Work in flight

    /// Why a piece of work is on the in-flight list, or why it is not.
    ///
    /// The list exists because a worktree makes work invisible: a delivery sitting finished on
    /// `clawdline/task/…` shows up in no `git status`, no `git diff` and no file listing of the
    /// shared checkout, and its claims were released the moment the task ended. A session asking
    /// "has anyone done this?" looked at the tree, saw nothing, and did it again.
    enum WorkVisibility: String {
        /// A session is on it now.
        case live
        /// Finished, and its delivery is still on a branch nobody has merged.
        case unmerged
        /// Nothing outstanding — landed, abandoned, merged, or never isolated in the first place.
        case settled
    }

    /// **How an entry stops being live, decided from git rather than from anybody's memory.**
    ///
    /// A registry that fills with work nobody is doing is worse than no registry, because it
    /// still looks authoritative. So nothing here is a flag somebody has to remember to clear:
    /// a delivery leaves the list when its branch is merged or deleted, which is the same moment
    /// the work stops being invisible. The two declared answers — a root marking its landing
    /// `landed` or `abandoned` — are honoured first because a root saying so is better evidence
    /// than a branch this side has to guess about.
    ///
    /// **Unknown git facts keep the entry visible**, the same fail-safe direction
    /// ``worktreeDisposal(commits:dirty:headOnBranch:branchExists:)`` takes with deletion. The
    /// costs are not symmetric: showing a delivery that turns out to be merged costs a glance,
    /// and hiding one that is not costs somebody a day of rebuilding it.
    ///
    /// A terminal task with no worktree is `settled` here, and that is a boundary rather than an
    /// oversight: its edits are in the shared tree where `git status` already shows them. The way
    /// such a delivery stays visible is the root declaring `landing: pending` — see
    /// ``updateLanding(taskID:secret:orchestratorToken:raw:now:)``, which is the declared half of
    /// this and is not
    /// duplicated by the derived half.
    static func workVisibility(state: State, landing: Landing?, isolated: Bool,
                               branchExists: Bool?, branchMerged: Bool?) -> WorkVisibility {
        guard state.isTerminal else { return .live }
        if let landing {
            switch landing.state {
            case .landed, .abandoned: return .settled
            case .pending: return .unmerged
            }
        }
        guard isolated else { return .settled }
        if branchExists == false { return .settled }
        if branchMerged == true { return .settled }
        return .unmerged
    }

    /// What a repository can say about its own task branches, in two `for-each-ref` calls rather
    /// than two per task. Thirteen delivery branches was the real count on the night this was
    /// written; a `rev-list` each would be twenty-six subprocesses on a route a child calls
    /// before it starts work.
    struct RepositoryBranches: Equatable {
        /// Branch name to the commit it currently points at.
        var heads: [String: String] = [:]
        /// Branches already contained in the repository's HEAD.
        var merged: Set<String> = []
        /// Whether git answered at all. False means every branch fact below is unknown, which
        /// ``workVisibility(state:landing:isolated:branchExists:branchMerged:)`` reads as "keep
        /// it visible" rather than as "it is gone".
        var known = false
    }

    static func repositoryBranches(in repository: String) -> RepositoryBranches {
        var found = RepositoryBranches()
        guard let listed = git(["for-each-ref", "--format=%(refname:short) %(objectname)",
                                "refs/heads/clawdline/task/"], cwd: repository),
              listed.status == 0 else { return found }
        found.known = true
        for line in listed.output.split(separator: "\n") {
            let parts = line.split(separator: " ", maxSplits: 1)
            guard parts.count == 2 else { continue }
            found.heads[String(parts[0])] = String(parts[1])
        }
        guard let contained = git(["for-each-ref", "--format=%(refname:short)", "--merged", "HEAD",
                                   "refs/heads/clawdline/task/"], cwd: repository),
              contained.status == 0 else { return found }
        for line in contained.output.split(separator: "\n") where !line.isEmpty {
            found.merged.insert(String(line))
        }
        return found
    }

    /// The repository a project directory belongs to, or nothing when it is not in one. The
    /// caller never writes this path: it is resolved here from what the task already said.
    static func inflightRepository(_ project: String) -> String? {
        guard let answer = git(["rev-parse", "--show-toplevel"], cwd: project),
              answer.status == 0 else { return nil }
        let path = answer.output.trimmingCharacters(in: .whitespacesAndNewlines)
        return path.hasPrefix("/") ? URL(fileURLWithPath: path).standardizedFileURL.path : nil
    }

    /// Every piece of work outstanding in a repository, newest first — live sessions and
    /// delivered-but-unmerged branches alike, in one answer.
    ///
    /// The branch facts are a parameter so the whole shape can be exercised against a described
    /// repository instead of a real one; production passes ``repositoryBranches(in:)``.
    static func inflightRecords(repository: String, now: Date = Date(),
                                branches: RepositoryBranches,
                                excluding excluded: String? = nil) -> [[String: Any]] {
        load()
        lock.lock()
        let all = tasks.values.sorted { $0.created > $1.created }
        lock.unlock()
        let prefix = repository.hasSuffix("/") ? repository : repository + "/"
        return all.compactMap { task -> [String: Any]? in
            guard task.id != excluded else { return nil }
            let home = task.worktree?.repository ?? task.projectDir
            guard home == repository || home.hasPrefix(prefix) else { return nil }
            let branch = task.worktree?.branch
            let visibility = workVisibility(
                state: task.state, landing: task.landing, isolated: task.worktree != nil,
                branchExists: branch.flatMap { branches.known ? branches.heads[$0] != nil : nil },
                branchMerged: branch.flatMap { branches.known ? branches.merged.contains($0) : nil })
            guard visibility != .settled else { return nil }
            return inflightRow(task, visibility: visibility, branches: branches, now: now)
        }
    }

    /// One row: the facts a reader needs to decide "is this my work?" and nothing else — what it
    /// is, who has it, what state it is in, what it said about itself, and where the code lives.
    ///
    /// This is deliberately the *only* shape for that answer. When something reads this list in
    /// natural language later it reads these rows rather than asking the broker again, so a fact
    /// a reader needs belongs here and not in a second projection of the same records.
    static func inflightRow(_ task: Task, visibility: WorkVisibility,
                            branches: RepositoryBranches, now: Date) -> [String: Any] {
        var row: [String: Any] = [
            "id": task.id,
            "title": task.title,
            "state": task.state.rawValue,
            "visibility": visibility.rawValue,
            "assistant": task.assistant.rawValue,
            "project_dir": task.projectDir,
            "created": Int(task.created.timeIntervalSince1970),
            "age_seconds": ageSeconds(since: task.created, now: now),
            "claims": task.claims,
        ]
        if let label = task.rootLabel { row["root_label"] = label }
        if let session = task.rootSessionId { row["root_key"] = rootKeyDigest(session) }
        if !task.progress.isEmpty {
            row["progress"] = task.progress.map {
                ["note": $0.note, "at": Int($0.at.timeIntervalSince1970)] as [String: Any]
            }
        }
        if let landing = task.landing { row["landing"] = landingRecord(landing) }
        if let worktree = task.worktree {
            var delivery: [String: Any] = ["branch": worktree.branch, "base": worktree.base]
            // The ref listing is what the branch points at *now*; the stored head is what this
            // app last recorded. Preferring the live one is the difference between reporting a
            // delivery and reporting a memory of one.
            if let head = branches.heads[worktree.branch] ?? worktree.head { delivery["head"] = head }
            if let dirty = worktree.dirty { delivery["dirty"] = dirty }
            if branches.known {
                delivery["branch_exists"] = branches.heads[worktree.branch] != nil
                delivery["merged"] = branches.merged.contains(worktree.branch)
            }
            row["worktree"] = delivery
        }
        return row
    }

    /// The list, answered to a task that names itself with its own secret.
    ///
    /// **A child has no orchestrator token and should not be taught to read one** — that file is
    /// this Mac's credential, and a leaf that never dispatches has no other reason to touch it.
    /// So the door a child uses is its own task secret, and the repository is not a parameter:
    /// it is read off the task, which means a child cannot ask this about a tree it is not
    /// working in. The same argument ``Planner`` makes for numbering places rather than taking
    /// paths, made again at a smaller door.
    ///
    /// The asking task is left out of its own answer. A session reading a row about itself and
    /// concluding somebody else is already on it is a loop with a very silly ending.
    static func inflightReply(taskID: String, secret: String, now: Date = Date()) -> Reply {
        guard let task = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !secret.isEmpty,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            return .refused(403, "forbidden", "That is not this task's secret.")
        }
        guard let repository = inflightRepository(cwd(of: task)) else {
            return .refused(409, "not_a_repository",
                            "This task's directory is not inside a Git repository.")
        }
        let branches = repositoryBranches(in: repository)
        return .ok(["repository": repository,
                    "inflight": inflightRecords(repository: repository, now: now,
                                                branches: branches, excluding: taskID),
                    "at": Int(now.timeIntervalSince1970)])
    }

    // MARK: - What a session says it is doing

    /// One line a session wrote about its own work while it was doing it.
    ///
    /// The title is fixed at dispatch and is thin evidence: a child that decides mid-task to also
    /// rewrite the fixture, or that finds the real problem is somewhere else, has nowhere to say
    /// so until `result.json` — by which time somebody else may have spent an hour on the same
    /// thing. This is deliberately one sentence and one curl, because a session that has to stop
    /// and compose a status report will not do it.
    struct ProgressNote: Equatable {
        let note: String
        let at: Date
    }

    /// A sentence, not a report.
    static let progressLimit = 300
    /// How many are kept. The newest few are what somebody deciding "is this my work?" reads;
    /// the whole history of a task is its transcript's job, not the registry's.
    static let progressKept = 5

    /// Record what this task is actually doing now. Authenticated by the task secret, like
    /// `complete` and `notify`: the child already has it, and it names exactly one task.
    static func recordProgress(taskID: String, secret: String, note raw: String,
                               now: Date = Date()) -> Reply {
        guard let snapshot = held(taskID) else {
            return .refused(404, "not_found", "No task named that")
        }
        guard !secret.isEmpty,
              RemoteAuth.constantTimeEquals(snapshot.secretHash, hash(ofSecret: secret)) else {
            RemoteAuth.audit("orchestrator.progress", ["task": taskID, "ok": "0",
                                                       "why": "bad_secret"])
            return .refused(403, "forbidden", "That is not this task's secret.")
        }
        let note = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !note.isEmpty, note.count <= progressLimit else {
            return .refused(400, "bad_request",
                            "note must be a non-empty sentence of at most \(progressLimit) "
                            + "characters.")
        }
        guard !snapshot.state.isTerminal else {
            return .refused(409, "not_live",
                            "This task is over; what it did belongs in its summary.")
        }

        load()
        lock.lock()
        guard var current = tasks[taskID] else {
            lock.unlock()
            return .refused(404, "not_found", "No task named that")
        }
        // The same sentence twice is a loop, not news. Refusing it costs the caller nothing and
        // keeps a retrying script from rewriting the registry to disk every second.
        if current.progress.last?.note == note {
            lock.unlock()
            return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
        }
        current.progress.append(ProgressNote(note: note, at: now))
        if current.progress.count > progressKept {
            current.progress.removeFirst(current.progress.count - progressKept)
        }
        tasks[taskID] = current
        lock.unlock()

        save()
        RemoteServer.shared.broadcastOrchestrator()
        RemoteAuth.audit("orchestrator.progress", ["task": taskID, "ok": "1"])
        return .ok(["ok": true, "task": existingRecord(taskID) ?? [:]])
    }

    /// The name of the progress channel's file half, in the task's own directory.
    static let progressFileName = "progress.json"

    /// Collect the file half of the progress channel: `progress.json` in the task directory,
    /// carrying the latest note and the task secret.
    ///
    /// **This channel exists because the curl one measurably does not, for most children.** A
    /// Codex child's sandbox sets `CODEX_SANDBOX_NETWORK_DISABLED=1`; a curl to loopback exits
    /// 7 after 0 ms, DNS itself is off, and no approval prompt ever appears — measured on this
    /// machine by task be9a54c0, where 133 codex children were briefed to send a note over
    /// HTTP and 0 notes ever arrived, against 26 of 40 claude children. `result.json` never
    /// had the problem, because it is a file the broker picks up. So progress gets the same
    /// shape: the child replaces one file, the watch beat collects it, and the same secret
    /// authenticates it. The route stays the fast path for whoever can reach it — a curl lands
    /// immediately, the file on the next beat.
    ///
    /// One file holding one sentence, replaced whole, rather than a log appended to or a file
    /// per note: the registry keeps only the newest ``progressKept`` — history is the
    /// transcript's job — and a whole-file replace is the write pattern `result.json` already
    /// proved survives a sandboxed child's file tool. A half-written file fails to parse and
    /// is simply read again a few seconds later. `progressFileNote` on the record is what
    /// tells a new sentence from one already collected, and it is persisted so neither a later
    /// HTTP note nor a restart makes the beat replay the file's old sentence. The same
    /// sentence through both channels is one piece of news: whichever lands second is dropped
    /// by the same newest-note rule the route uses.
    ///
    /// Refreshes `task` to the record as committed, so a caller that goes on to replace the
    /// record cannot write a pre-collection snapshot over the note.
    private static func collectProgressFile(of task: inout Task) -> Bool {
        let file = task.dir.appendingPathComponent(progressFileName)
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return false
        }
        guard let secret = obj["task_secret"] as? String,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            // Somebody wrote a note they could not have been asked for. Once in the log is
            // enough — the file is left alone so the evidence is where the log says it is.
            if !badProgressFiles.contains(task.id) {
                badProgressFiles.insert(task.id)
                RemoteAuth.audit("orchestrator.progress",
                                 ["task": task.id, "ok": "0", "via": "file",
                                  "why": "bad_secret"])
            }
            return false
        }
        let note = ((obj["note"] as? String) ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !note.isEmpty, note.count <= progressLimit else {
            // The route answers a bad note with a 400; a file has nobody holding the reply,
            // so the refusal goes to the audit log instead — once, like a bad secret.
            if !badProgressFiles.contains(task.id) {
                badProgressFiles.insert(task.id)
                RemoteAuth.audit("orchestrator.progress",
                                 ["task": task.id, "ok": "0", "via": "file", "why": "bad_note"])
            }
            return false
        }
        guard note != task.progressFileNote else { return false }
        lock.lock()
        guard var current = tasks[task.id], !current.state.isTerminal,
              current.progressFileNote != note else {
            lock.unlock()
            return false
        }
        current.progressFileNote = note
        // The same sentence as the newest note is the other channel delivering the same news.
        if current.progress.last?.note != note {
            current.progress.append(ProgressNote(note: note, at: Date()))
            if current.progress.count > progressKept {
                current.progress.removeFirst(current.progress.count - progressKept)
            }
        }
        tasks[task.id] = current
        lock.unlock()
        task = current
        save()
        RemoteServer.shared.broadcastOrchestrator()
        RemoteAuth.audit("orchestrator.progress", ["task": task.id, "ok": "1", "via": "file"])
        return true
    }

    private static var badProgressFiles: Set<String> = []

    /// Cancelling with no HTTP answer wrapped around it: the child's tab ended the polite way,
    /// then the task written down. Shared with the cascade below so there is one way to cancel a
    /// task rather than two that drift.
    ///
    /// Not on the main thread: `Targets.end` types the quit word and then waits for the child to
    /// actually be gone — a few hundred milliseconds when it leaves on the word, and a bounded
    /// five and a bit when it has to be made to. Both callers arrive on the server's queue.
    private static func cancelInPlace(_ task: Task) {
        let finish: (TerminalIntervention?) -> Void = { intervention in
            DispatchQueue.main.async {
                finalize(task.id, as: .cancelled, summary: "Cancelled.")
                if let intervention, var current = held(task.id) {
                    current.terminalIntervention = intervention
                    if replaceTask(current, expecting: current.state) {
                        save(); RemoteServer.shared.broadcastOrchestrator()
                    }
                } else if let worktree = task.worktree {
                    scheduleWorktreeDisposal(worktree, taskID: task.id, why: "empty",
                                             allowCommitted: false)
                }
            }
        }
        // An attached follow-up task borrows a standing session's tab. Cancelling the task must
        // never end that tab, so it takes the no-terminal path straight to the record.
        guard task.attachSessionId == nil, let childID = task.childTerminalId,
              let child = target(withID: childID) else {
            finish(nil); return
        }
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: child.id) {
            let intervention = Targets.end(child).map {
                terminalIntervention(for: $0, backend: child.backend)
            }
            finish(intervention)
        }
        if !admitted {
            finish(TerminalIntervention(
                kind: .terminal, message: "The terminal broker is full; the tab was left open."))
        }
    }

    /// Preserve the source of a failed close as data, not a phrase inferred later by the API.
    /// Only a real iTerm automation circuit has a recovery probe; every other backend/process
    /// failure needs a person and is never eligible for timer-driven retry.
    static func terminalIntervention(for failure: String,
                                     backend: Backend?) -> TerminalIntervention {
        if backend == .iterm, let attention = ITerm.automationAttention,
           failure == attention {
            return TerminalIntervention(kind: .iTermModal, message: attention)
        }
        return TerminalIntervention(kind: .terminal, message: failure)
    }

    /// The live tasks a root session dispatched, oldest first.
    ///
    /// Apart from the cascade because it is the half worth testing on its own, and *live* only:
    /// what a root's leaving does to work still running is not what it does to work that
    /// finished, so the two are chosen separately — `lingeringTasks` is the other half. Matched
    /// on `root_session`, which is the assistant's own id for itself and not any name this app
    /// has for a tab.
    static func liveTasks(dispatchedBy rootSessionId: String) -> [String] {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks.values
            .filter { !$0.state.isTerminal && $0.rootSessionId == rootSessionId }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    /// The finished tasks a root dispatched that still name a child tab, oldest first.
    ///
    /// A finished child's tab is kept for a while so somebody can read what it did. The somebody
    /// was the root, and when it goes the tab is a window onto a conversation that has ended —
    /// still drawn indented under a session no longer in the list.
    ///
    /// **Keyed on the tab, not on `closeAt`.** The linger deadline lives only in memory, so every
    /// task that outlived a restart of the app has none, while its tab is still very much on
    /// screen: closing the root would then walk straight past exactly the rows somebody is
    /// looking at. `child_terminal` is written down, which is also what the page reads to decide
    /// a row still belongs under its root — the same rule on both sides, so what a close takes is
    /// what a reader sees.
    ///
    /// Whether the tab is really still there is `closeChildTab`'s question; it answers false when
    /// the tab has gone, or has become somebody else's since.
    static func lingeringTasks(dispatchedBy rootSessionId: String) -> [String] {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks.values
            .filter { $0.state.isTerminal && $0.childTerminalId != nil
                        && $0.attachSessionId == nil
                        && $0.rootSessionId == rootSessionId }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    /// The live tasks dispatched from inside the tabs these tasks opened — the level below a
    /// cascade's first.
    ///
    /// A child is recognised by whichever of the two names it managed to give: the parent task
    /// it declared, or the session id it calls itself by, which this app only learns once it has
    /// read the child's transcript. Either is enough, and a child that gave neither is not
    /// collected — it is not filed under anyone, so there is no one whose leaving takes it.
    static func liveTasks(under parents: [String]) -> [String] {
        tasksUnder(parents) { !$0.state.isTerminal }
    }

    /// The finished-but-still-tabbed tasks one level below these — `lingeringTasks`' other half.
    static func lingeringTasks(under parents: [String]) -> [String] {
        tasksUnder(parents) {
            $0.state.isTerminal && $0.childTerminalId != nil && $0.attachSessionId == nil
        }
    }

    private static func tasksUnder(_ parents: [String], where keep: (Task) -> Bool) -> [String] {
        guard !parents.isEmpty else { return [] }
        load()
        lock.lock(); defer { lock.unlock() }
        let above = parents.compactMap { tasks[$0] }
        guard !above.isEmpty else { return [] }
        let ids = Set(above.map { $0.id })
        // `childSessionId` can survive from a registry written before ownership proofs were
        // persisted. It may describe a sibling, so it becomes a cancellation key only after the
        // task marker has proved the paired path. This lazy check touches at most the requested
        // parents, never every historical transcript during app startup.
        let sessions = Set(above.compactMap { provenChildSessionID(of: $0) })
        return tasks.values
            .filter { task in
                guard keep(task), !ids.contains(task.id) else { return false }
                if let parent = task.parentTaskId, ids.contains(parent) { return true }
                if let root = task.rootSessionId, sessions.contains(root) { return true }
                return false
            }
            .sorted { $0.created < $1.created }
            .map { $0.id }
    }

    private static func provenChildSessionID(of task: Task) -> String? {
        guard let sessionID = task.childSessionId,
              let path = task.transcriptPath else { return nil }
        if task.transcriptProven { return sessionID }
        let transcript = URL(fileURLWithPath: path)
        guard case .belongs = transcriptOwnership(transcript, assistant: task.assistant,
                                                  taskID: task.id) else { return nil }
        switch task.assistant {
        case .claude:
            let fromPath = transcript.deletingPathExtension().lastPathComponent
            return fromPath == sessionID ? sessionID : nil
        case .codex:
            return Codex.head(of: transcript)?.id == sessionID ? sessionID : nil
        }
    }

    /// Ending a root session ends the work it dispatched. Two things happen, and they are not the
    /// same thing: a task still running is cancelled and its child's tab ended, while a task that
    /// already finished keeps its record — `success` is a fact about work that happened — and
    /// loses only the linger holding its tab open. Answers with both sets.
    ///
    /// **Runs before the root's own tab goes, and the ordering is the whole of it.** A task knows
    /// its root by the session id in that session's hook note, and the note is reached through
    /// the tty of a tab that is a second away from leaving the reading. Close the root first and
    /// there is nothing left to match against: the children run on, reporting into a conversation
    /// that ended.
    ///
    /// **Explicitly closing a session, and nothing else.** A tab closed by hand leaves the
    /// children alone: the app does not watch a root for signs of death, because "not in this
    /// reading" is also true of a terminal that lost its accessibility permission for a moment,
    /// and being wrong about that would kill somebody's work mid-turn.
    ///
    /// **Deepest first, even though the tree is one level deep.** A root's children are the
    /// bottom — nothing this app opens may dispatch — so in an ordinary tree this walks one
    /// level and stops. The collection is still done before anything is cancelled and still ends
    /// from the bottom up, because a task below a task is a shape a stored record from an older
    /// build can still have, and closing a tab while something it is holding open is being read
    /// for is the failure this order exists to avoid.
    ///
    /// What is below is gathered from the finished children too, not only the live ones: a child
    /// that reported while something it opened is still running would otherwise leave that task
    /// belonging to nobody.
    ///
    /// A busy child gets `cancel`'s decisiveness rather than `closeChild`'s ten minutes of
    /// patience. Those are different moments: one is tidying up after work that finished, this is
    /// somebody pressing close, and making them wait on a child they cannot see is not the thing
    /// they asked for.
    ///
    /// **A finished child's tab goes too.** It is standing there because somebody might still
    /// want to read what it did, and the page draws it indented under the root that asked for it.
    /// Leave it and closing a root leaves its children on screen, filed under a session that is
    /// no longer in the list — which is the shape of the bug this was written to fix, and reads
    /// as the close having done nothing at all.
    /// What pressing close would take with it, read at the moment of the press.
    ///
    /// Deliberately not a list column and not a state: `hasOutstandingChild` was already in the
    /// projection the night a root with four live children was closed anyway. A label read
    /// earlier cannot stop a close — only the close itself can — so this is computed for the
    /// gate in `POST /v1/sessions/:id/end` and shown at the confirming press. It names live
    /// descendant tasks (the exact set `cancelChildren` would cancel) and open coordination
    /// waits this session owns, whose waiters a close would strand unreleased.
    static func lostIfClosed(root session: TargetSession) -> [[String: Any]] {
        load()
        var lost: [[String: Any]] = []
        if let rootSession = rootIdentity(of: session, sessionID: Transcript.sessionID(of:)) {
            let mine = liveTasks(dispatchedBy: rootSession)
            let below = liveTasks(under: mine + lingeringTasks(dispatchedBy: rootSession))
            for id in mine + below {
                guard let task = held(id) else { continue }
                lost.append(["task": task.id, "title": task.title,
                             "state": task.state.rawValue])
            }
        }
        lock.lock()
        let ownedWaits = coordinationWaits.values.filter { wait in
            wait.ownerSessionID == session.id
                && wait.waiters.contains { $0.releaseDeliveredAt == nil }
        }.sorted { $0.created < $1.created }
        lock.unlock()
        for wait in ownedWaits {
            lost.append(["wait": wait.id, "release_condition": wait.releaseCondition,
                         "waiters": wait.waiters.filter { $0.releaseDeliveredAt == nil }.count])
        }
        return lost
    }

    @discardableResult
    static func cancelChildren(ofRoot session: TargetSession) -> [String] {
        guard let rootSession = rootIdentity(
                of: session, sessionID: Transcript.sessionID(of:)) else { return [] }
        let mine = liveTasks(dispatchedBy: rootSession)
        let lingering = lingeringTasks(dispatchedBy: rootSession)
        let below = liveTasks(under: mine + lingering)
        let lingeringBelow = lingeringTasks(under: mine + lingering)
        let live = below + mine
        for id in live {
            guard let task = held(id) else { continue }
            // `why` is what tells this row apart from a task somebody cancelled on purpose: this
            // one did nothing, its root left.
            RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "root_ended",
                                                     "root": session.id])
            cancelInPlace(task)
        }
        var closed: [String] = []
        for id in lingeringBelow + lingering where closeChildTab(ofTask: id, root: session.id) {
            closed.append(id)
        }
        if !closed.isEmpty {
            save()
            RemoteServer.shared.broadcastOrchestrator()
        }
        return live + closed
    }

    /// End a finished task's linger now, leaving the record exactly as it stands. True when the
    /// tab was still ours to take.
    ///
    /// Deliberately not `cancelInPlace`: that writes `cancelled` over the record, and a task that
    /// succeeded still succeeded — the result file is there and worth keeping. What the root's
    /// leaving takes away is the reason to hold the tab open, not the work.
    ///
    /// The courtesy is `closeChild`'s: a quit word when there is an assistant in there to hear
    /// it, the tab alone when the child is mid-turn or has already left. What it does not inherit
    /// is the ten minutes of patience — that is for a linger running out on its own, and this is
    /// somebody pressing close.
    private static func closeChildTab(ofTask id: String, root: String) -> Bool {
        // An attached follow-up task has no tab of its own — it borrows the standing session's,
        // and ending that would take the session with it.
        guard let task = held(id), task.attachSessionId == nil,
              let childID = task.childTerminalId else { return false }
        // Identity, activity and the close itself are all read inside the broker against one
        // fresh inventory. The cached row this beat holds may already be somebody else's tab.
        RemoteAuth.audit("orchestrator.close.requested",
                         ["task": id, "child": childID, "why": "root_ended", "root": root])
        // Explicit root close and linger expiry share the same admission, closing guard and
        // success-only deadline clearing. A failed safe-close therefore remains visible and can
        // never be mistaken for completed cleanup.
        return takeChildTab(for: task, childID: childID)
    }

    /// What a session calls itself, which is what `rootSessionId` was written from. The unified
    /// transcript lookup validates Claude against its current process/transcript and Codex
    /// against the rollout held open by its current pid, so a reused tty cannot lend an old
    /// conversation identity to the new process.
    static func rootIdentity(of session: TargetSession,
                             sessionID: (TargetSession) -> String?) -> String? {
        sessionID(session)
    }

    // MARK: - Lifecycle: the beat

    private static var timer: Timer?
    private static var cleanupTimer: Timer?
    private static var scheduleTimer: Timer?

    /// Wired once at launch, alongside the other observers.
    static func start() {
        load()
        // Minted now rather than on first use: the skill tells a root to read this file before
        // its first dispatch, and a file that appears only after a request nobody can make yet
        // is a door that opens from the inside.
        _ = dispatchToken()
        resumeAfterRestart()
        cleanup()
        scheduleQueue.async { scheduleBeat() }
        SessionWatch.shared.observers["orchestrator"] = { beat(fromTimer: false) }
        let t = Timer(timeInterval: 5, repeats: true) { _ in beat(fromTimer: true) }
        RunLoop.main.add(t, forMode: .common)
        timer = t
        let c = Timer(timeInterval: 6 * 3600, repeats: true) { _ in cleanup() }
        RunLoop.main.add(c, forMode: .common)
        cleanupTimer = c
        let s = Timer(timeInterval: 60, repeats: true) { _ in
            scheduleQueue.async { scheduleBeat() }
        }
        RunLoop.main.add(s, forMode: .common)
        scheduleTimer = s
    }

    /// Recover waiters and fail tasks whose pre-briefing secret died with the previous process.
    /// Separate from timer wiring so the restart handoff can be exercised without opening a live
    /// app lifecycle in the unit suite.
    static func resumeAfterRestart() {
        load()
        // Anything mid-way through briefing is unbriefable now: its plaintext secret died with
        // the process. A serialized task that never left queued is different — its temporary
        // sealed copy can be opened with this installation's at-rest key and pumped below.
        var orphaned: [String] = []
        var recovered: [String: String] = [:]
        lock.lock()
        let restartRows = tasks.values.filter { $0.state == .queued || $0.state == .spawning }
        lock.unlock()
        for task in restartRows where task.state == .queued && !task.serialize.isEmpty {
            if let sealed = task.queuedSecret, let secret = openQueuedSecret(sealed),
               RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) {
                recovered[task.id] = secret
            }
        }
        lock.lock()
        for (id, task) in tasks where task.state == .queued || task.state == .spawning {
            if task.state == .queued, let secret = recovered[id] {
                secrets[id] = secret
                continue
            }
            var dead = task
            dead.state = .spawnFailed
            dead.summary = task.state == .queued && !task.serialize.isEmpty
                ? "The app restarted but could not recover the queued task secret."
                : "The app restarted before the child was briefed."
            dead.finishedAt = Date()
            dead.queuedSecret = nil
            dead.workCleanupAt = reclaimDeadline(
                minutes: Config.shared.orchestratorWorkGraceMinutes, outcome: .spawnFailed)
            dead.buildCleanupAt = dead.worktree == nil ? nil : reclaimDeadline(
                minutes: Config.shared.orchestratorBuildGraceMinutes, outcome: .spawnFailed)
            tasks[id] = dead
            orphaned.append(id)
        }
        var interruptedHandoffs: [String] = []
        for (id, envelope) in handoffs where envelope.state == .opening {
            var failed = envelope
            failed.state = .spawnFailed
            handoffs[id] = failed
            interruptedHandoffs.append(id)
        }
        lock.unlock()
        for id in interruptedHandoffs {
            RemoteAuth.audit("handoff.undelivered", ["handoff": id, "why": "app_restarted"])
        }
        let rearmed = rearmLingers()
        if !orphaned.isEmpty || !interruptedHandoffs.isEmpty || rearmed { save() }
        lock.lock()
        let beforeCompletionRecovery = tasks
        lock.unlock()
        let completionRecovery = reconcileCompletionOutbox(
            taskID: nil, includeDeadLetters: false, now: Date())
        let resultRecovery = reconcileResultReceipts(taskID: nil,
                                                     limit: legacyCompletionBatchLimit)
        let recoveryChanged = completionRecovery.changed || !resultRecovery.isEmpty
        let recoveryPersisted = !recoveryChanged || save()
        if !recoveryPersisted {
            // A due envelope that exists only in memory must never become eligible on the next
            // timer beat. Startup owns this phase, so restoring the just-loaded snapshot is safe.
            lock.lock()
            tasks = beforeCompletionRecovery
            reindex()
            lock.unlock()
            RemoteAuth.audit("orchestrator.completion.defer", [
                "why": "startup_store_failed",
            ])
        } else if recoveryChanged {
            markCompletionEnvelopesPersisted(completionRecovery.changedTaskIDs)
        }
        if recoveryPersisted { scheduleCompletionPump() }
        scheduleSerializePump()
    }

    /// How long a restored linger waits before this process is willing to act on it.
    ///
    /// Long enough for the first reading to land, and no longer. `SessionWatch` takes one the
    /// moment it starts, but it is a round trip to every terminal and the app may well have
    /// opened in the background, where the cadence is twenty seconds. A deadline that already
    /// ran out while the app was away would otherwise be judged against an empty list — and
    /// ``closeStep`` now waits on one of those anyway, so this is the belt to that pair of braces.
    /// It also keeps the ten-minute patience for a busy child measured from *this* process
    /// rather than from a deadline that expired overnight.
    static let restartGrace: TimeInterval = 20

    /// Carry each reported child's linger across the restart that interrupted it.
    ///
    /// Only the deadline is restored, and only where a tab was named: whether that tab is still
    /// the child's is a question for a reading this process took. A deadline already spent gets
    /// ``restartGrace`` rather than being acted on the instant the app opens.
    @discardableResult
    private static func rearmLingers() -> Bool {
        let linger = Config.shared.orchestratorChildLinger
        let floor = Date().addingTimeInterval(restartGrace)
        var changed = false
        lock.lock()
        for (id, task) in tasks where task.closeAt != nil {
            var carried = task
            // An explicit per-schedule close policy wins over the global default after restart in
            // exactly the same way it did when finalize created this deadline. Ordinary tasks
            // still honour a Mac that has since said child tabs are never to be closed.
            if task.childTerminalId == nil || (task.scheduleID == nil && linger < 0) {
                carried.closeAt = nil
            } else if let at = task.closeAt, at < floor {
                carried.closeAt = floor
            } else {
                continue
            }
            tasks[id] = carried
            changed = true
        }
        lock.unlock()
        return changed
    }

    /// Main thread. Advances every live task one step; cheap when nothing is live.
    ///
    /// The observer only fires when a reading *changed*, and a hung child looks exactly like a
    /// quiet one — so the timer path exists to notice timeouts and a `result.json` that appeared
    /// without the screen moving. Only the timer path asks for fresh readings, so an observer
    /// firing cannot ask for the reading that fires it.
    static func beat(fromTimer: Bool) {
        // Two walkers at once is the shape a task once failed in: one of them had copied a record
        // before the other advanced it, and acted on that copy afterwards. Every caller reachable
        // from here is on the main thread, which was taken to mean the overlap was impossible —
        // it is not, and this counter is what proved it. `Process.waitUntilExit()` polls the run
        // loop, so typing into a terminal from here let the five-second timer fire *inside* the
        // walk. `waitQuietly()` is the fix and lives in ``Subprocess``; this stays because a
        // second cause would look exactly like the first one did, and silence is not evidence.
        //
        // It counts rather than blocks, and that is still deliberate. A dropped walker leaves
        // nothing to read, and the record cannot be damaged by a stale copy anyway: `replaceTask`
        // refuses to move a task backwards.
        let visibleTerminals = Set(SessionWatch.shared.targets.map(\.id))
        lock.lock()
        pruneClosedHandoffTitles(visible: visibleTerminals)
        beatSequence += 1
        let sequence = beatSequence
        let overlapping = beatsInFlight > 0
        beatsInFlight += 1
        let liveIDs = tasks.values
            .filter {
                !$0.state.isTerminal || $0.closeAt != nil
                    || $0.workCleanupAt != nil || $0.buildCleanupAt != nil
            }
            .map(\.id)
        let liveHandoffs = Array(handoffDeliveries.keys)
        lock.unlock()
        defer {
            lock.lock(); beatsInFlight -= 1; lock.unlock()
        }
        if overlapping {
            RemoteAuth.audit("orchestrator.beat_overlap",
                             ["beat": String(sequence),
                              "from": fromTimer ? "timer" : "reading",
                              "main": String(Thread.isMainThread),
                              "thread": Thread.current.description])
        }
        // Before the early return on purpose. With `orchestratorChildLinger` set to never keep a
        // tab, the last task of a fan-out leaves nothing in `liveIDs` at all — and a batch that
        // announces only while something is still on the list is one that never announces.
        sweepBatches()
        scheduleCompletionPump()

        for id in liveHandoffs { scheduleHandoffStep(id) }
        if fromTimer, !liveHandoffs.isEmpty { SessionWatch.shared.nudge() }

        guard !liveIDs.isEmpty else { return }

        var changed = false
        var sawSpawning = false
        var closing: [Task] = []
        for id in liveIDs {
            // The list is only scheduling. State is read at the instant this task is advanced, so
            // an earlier item in a long beat cannot leave a stale state decision behind it.
            //
            // The three reasons a record is on that list are the three reasons to advance it, and
            // this guard has to name all of them. It once named only the first two, so a terminal
            // task carrying nothing but a reclaim deadline was scheduled and then dropped on the
            // next line — which is every ending except `success`, because `finalize` reclaims that
            // one in place and only the *other* endings ever wait for a beat.
            guard let task = held(id),
                  !task.state.isTerminal || task.closeAt != nil
                    || task.workCleanupAt != nil || task.buildCleanupAt != nil else { continue }
            switch task.state {
            case .spawning:
                // The record-only deadlines first, on this thread. Everything after them needs a
                // terminal, and a task must be able to expire while the terminal is stuck.
                if expireSpawningIfDue(task) { continue }
                sawSpawning = true
                scheduleBriefStep(task)
            case .briefed:  changed = watch(task) || changed
            default:
                changed = reclaimTaskWorkIfDue(task.id) || changed
                changed = reclaimTaskBuildIfDue(task.id) || changed
                // Collected, not closed: one walk answers the whole batch below.
                if let current = held(id) { closing.append(current) }
            }
        }
        closeDueChildren(closing)
        if fromTimer, sawSpawning {
            // Away from the panel the watch reads every twenty seconds, which is a long time to
            // leave a freshly opened terminal unbriefed.
            SessionWatch.shared.nudge()
        }
        if changed {
            save()
            RemoteServer.shared.broadcastOrchestrator()
        }
    }

    /// Capture/menu/injection work is terminal I/O and therefore never runs on the main-thread
    /// beat. The in-flight sets prevent a five-second beat from stacking duplicates while an
    /// Apple Event is blocked.
    private static var briefingStepsInFlight: Set<String> = []
    private static var handoffStepsInFlight: Set<String> = []

    private static func scheduleBriefStep(_ task: Task) {
        guard !briefingStepsInFlight.contains(task.id) else { return }
        briefingStepsInFlight.insert(task.id)
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: task.childTerminalId) {
            let changed = brief(task)
            DispatchQueue.main.async {
                briefingStepsInFlight.remove(task.id)
                if changed { save(); RemoteServer.shared.broadcastOrchestrator() }
            }
        }
        if !admitted { briefingStepsInFlight.remove(task.id) }
    }

    private static func scheduleHandoffStep(_ id: String) {
        guard !handoffStepsInFlight.contains(id) else { return }
        handoffStepsInFlight.insert(id)
        lock.lock()
        let channel = handoffDeliveries[id]?.terminalID
        lock.unlock()
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: channel) {
            handoffStep(id)
            DispatchQueue.main.async { handoffStepsInFlight.remove(id) }
        }
        if !admitted { handoffStepsInFlight.remove(id) }
    }

    /// How long a tab has to reach a prompt before the task gives up on it.
    ///
    /// Four minutes, and the number is set by the worst case rather than the usual one. A single
    /// assistant starting in a warm project reaches its composer in a few seconds; four of them
    /// starting at once — which is exactly what a two-level dispatch does — are competing for the
    /// same CPU, the same disk, and a status line that may be shelling out to git or to a CI API
    /// before it paints. Two minutes was enough for one and not for four, and the failure it
    /// produced was indistinguishable from a child that was never going to come up.
    ///
    /// The cost of being generous is a row that says `spawning` for longer before admitting
    /// defeat. The cost of being tight is work that silently did not happen, which is worse.
    static let readyLimit: TimeInterval = 240

    /// What to do about a menu on a briefing task's screen.
    enum MenuStep: Equatable {
        /// No menu, or nothing left to decide.
        case none
        /// Press the first row. Only ever on a tab this app opened for this task.
        case answerFirstRow
        /// A menu on a session this task did not open. Leave it standing.
        case leaveToOwner
    }

    /// Whether Clawdline may take the default on the menu in front of a spawning task.
    ///
    /// Answering was always justified by *whose screen it is*. A tab this app opened for a task
    /// in a directory the root named has exactly one menu to show — the trusted-folder dialog —
    /// and the root already answered it by asking for work there.
    ///
    /// An attached task is running in a standing child session this task did not open, and the
    /// menu on that screen can be anything: a permission prompt, a plan approval, an overwrite
    /// confirmation. The first row is usually "yes". Nothing about this follow-up dispatch is
    /// consent to that, so the menu is left for the session's owner. See `docs/orchestrator.md`,
    /// "Attached follow-up tasks".
    static func menuStep(task: Task, choosing: Bool) -> MenuStep {
        guard choosing else { return .none }
        guard !task.answeredMenu else { return .none }
        return task.attachSessionId == nil ? .answerFirstRow : .leaveToOwner
    }

    /// The two deadlines that end a `spawning` task, decided on the record alone.
    ///
    /// **Deliberately not inside ``brief``.** Briefing reads a screen and types, so it belongs in
    /// the terminal broker — and a deadline that travelled with it would be a deadline a modal
    /// sheet could suspend. A tab that never reached a prompt, and an attached briefing nobody
    /// answered, both have to expire while the terminal is exactly the thing that is stuck.
    /// True when the task was finalized and there is nothing left to brief.
    private static func expireSpawningIfDue(_ snapshot: Task) -> Bool {
        guard let task = held(snapshot.id), task.state == .spawning,
              let spawnedAt = task.spawnedAt else { return false }
        let spawningAge = Date().timeIntervalSince(spawnedAt)
        // `readyLimit` measures whether a tab this dispatch opened ever reached a prompt. An
        // attached task has no tab-opening phase: `spawnAttached` typed its first line before
        // registration returned, so four minutes would be a false `spawn_failed` while its owner
        // considers a menu. It is still bounded by the task's own timeout, measured from that
        // delivery, so a permanently unanswered menu cannot retain claims or the host forever.
        if task.attachSessionId != nil
            && spawningAge > TimeInterval(task.timeoutMinutes * 60) {
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .timeout,
                     summary: "The attached briefing was not accepted within the task's "
                            + "\(task.timeoutMinutes)-minute timeout.")
            return true // finalize saved and broadcast already
        }
        if task.attachSessionId == nil && spawningAge > readyLimit {
            // A task that has already said what it is doing cannot be a tab that never reached a
            // prompt. Give the file half of the progress channel its last chance to land — a
            // sandboxed child's note arrives only when a beat collects it, and no beat collects
            // it while the task is still `spawning` — and then take whatever receipt is there.
            var refreshed = task
            _ = collectProgressFile(of: &refreshed)
            if let provenAt = briefingProvenByProgress(refreshed),
               acceptProgressAsBriefing(refreshed, provenAt: provenAt) {
                save()
                RemoteServer.shared.broadcastOrchestrator()
                return true
            }
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .spawnFailed,
                     summary: "The child session did not reach a prompt within "
                            + "\(Int(readyLimit / 60)) minutes. If several sessions were starting "
                            + "at once, they were competing for this Mac.")
            return true // finalize saved and broadcast already
        }
        return false
    }

    /// When a `spawning` task's own progress proves the briefing arrived, or nil when nothing
    /// does.
    ///
    /// A progress note is authenticated with the task secret, and that secret reaches the child
    /// in exactly one place: the line typed into its composer. `task.json` does not carry it and
    /// `CHILD.md` only names it. So a note is a delivery receipt through a second channel — the
    /// same fact ``briefingDecision(screen:assistant:transcript:transcriptKnown:taskID:attempts:secondsSinceAttempt:)``
    /// looks for in the transcript, arriving by a route that does not depend on finding the
    /// transcript file at all. That independence is the whole point: this is the guard for
    /// failures nobody has diagnosed yet, including ones in transcript resolution itself.
    ///
    /// **What it prevents is not a cosmetic record error.** `finalize` reads `spawn_failed` with
    /// no `briefedAt` as "nothing was ever done here" and disposes the checkout with
    /// `allowCommitted: false`. On 2026-08-28 that deleted a live child's worktree *and* its
    /// delivery branch while the child was still working in the directory; the sibling task that
    /// had committed once survived the same misjudgement untouched.
    ///
    /// Proven at the earlier of the two moments that bound it, because `briefedAt` starts the
    /// task's own timeout: the line was on the tty by `lastInjectAt`, and had certainly been read
    /// by the first note.
    static func briefingProvenByProgress(_ task: Task) -> Date? {
        guard let firstNote = task.progress.first?.at else { return nil }
        guard let injected = task.lastInjectAt else { return firstNote }
        return min(injected, firstNote)
    }

    /// Cross `spawning` → `briefed` on a progress receipt rather than a transcript one. True when
    /// the record moved. Deliberately mutates the caller's snapshot instead of re-reading, so a
    /// briefing step does not lose the identity fields it just filled in.
    ///
    /// The transcript stays unproven: this receipt says the child read the line, not which file
    /// it is writing. `watch` keeps looking, and `applyTranscriptOwnership` still gets to reject
    /// a wrong candidate afterwards.
    private static func acceptProgressAsBriefing(_ snapshot: Task, provenAt: Date) -> Bool {
        var task = snapshot
        guard task.state == .spawning else { return false }
        task.state = .briefed
        task.briefedAt = provenAt
        task.lastSeenChild = Date()
        guard replaceTask(task, expecting: .spawning, discardSecret: true) else { return false }
        RemoteAuth.audit("orchestrator.brief.progress", [
            "task": task.id,
            "child": task.childTerminalId ?? "?",
            "notes": String(task.progress.count),
        ])
        return true
    }

    static func expireSpawningIfDueForTesting(_ task: Task) -> Bool { expireSpawningIfDue(task) }

    /// Try to put the first message in front of a child that has just opened. True when the task
    /// record changed.
    private static func brief(_ snapshot: Task) -> Bool {
        // A snapshot only nominates an id. Never act on its state or fields after another writer
        // may have advanced the record.
        guard var task = held(snapshot.id), task.state == .spawning else { return false }
        guard let childID = task.childTerminalId,
              let child = target(withID: childID),
              child.assistant == task.assistant else { return false }
        if task.childTTY == nil {
            task.childTTY = child.tty
            guard replaceTask(task, expecting: .spawning) else { return false }
        }
        let changed = noteChildIdentity(child, in: &task)
        let screen = Targets.capture(child)
        let choosing = screen.map { SessionState.isChoosing($0, assistant: task.assistant) }
            ?? false
        switch menuStep(task: task, choosing: choosing) {
        case .none:
            break
        case .answerFirstRow:
            task.answeredMenu = true
            guard replaceTask(task, expecting: .spawning) else { return false }
            _ = Targets.answer(0x31, to: child)
            RemoteAuth.audit("orchestrator.menu", ["task": task.id, "answer": "1"])
            return true
        case .leaveToOwner:
            task.answeredMenu = true
            guard replaceTask(task, expecting: .spawning) else { return false }
            RemoteAuth.audit("orchestrator.menu.left",
                             ["task": task.id, "session": childID])
            return true
        }

        // The other delivery receipt, read before the transcript one and before another copy of
        // the first line can be typed into a child that is already working from the first. See
        // ``briefingProvenByProgress(_:)``; the acceptance standard in `briefingDecision` is
        // untouched, because this is a different proof and not a weaker version of that one.
        if let provenAt = briefingProvenByProgress(task) {
            return acceptProgressAsBriefing(task, provenAt: provenAt)
        }

        let transcript: String?
        if let path = task.transcriptPath {
            transcript = try? String(contentsOfFile: path, encoding: .utf8)
        } else {
            transcript = nil
        }
        let elapsed = task.lastInjectAt.map { Date().timeIntervalSince($0) }
        let decision = briefingDecision(screen: screen, assistant: task.assistant,
                                        transcript: transcript,
                                        transcriptKnown: task.transcriptPath != nil,
                                        taskID: task.id, attempts: task.injectAttempts,
                                        secondsSinceAttempt: elapsed)
        if decision == .accepted {
            // Acceptance means this exact transcript contained the delivered task marker. Record
            // the receipt before crossing the state boundary so this pair becomes pinned.
            task.transcriptProven = task.transcriptPath != nil
            task.state = .briefed
            task.briefedAt = task.lastInjectAt ?? Date()
            task.lastSeenChild = Date()
            guard replaceTask(task, expecting: .spawning, discardSecret: true) else { return false }
            RemoteAuth.audit("orchestrator.brief", ["task": task.id,
                                                      "child": task.childTerminalId ?? "?",
                                                      "attempts": String(task.injectAttempts)])
            return true
        }
        if decision == .exhausted {
            guard replaceTask(task, expecting: .spawning) else { return false }
            finalize(task.id, as: .spawnFailed,
                     summary: "The child did not record the briefing after \(briefingAttemptLimit) attempts.")
            return false // finalize saved and broadcast already
        }
        guard decision == .send else {
            if changed, !replaceTask(task, expecting: .spawning) { return false }
            return changed
        }
        guard let secret = heldSecret(task.id) else {
            // Secret absence is an error only while the current record is still awaiting briefing;
            // a stale walker arriving after acceptance has nothing left to do.
            guard held(task.id)?.state == .spawning else { return false }
            finalize(task.id, as: .spawnFailed, summary: "The task's secret was lost before briefing.")
            return false
        }
        writeChildBrief(for: task)
        let line = firstLine(id: task.id, secret: secret, announce: L.t.childAnnounce(task.title))
        task.injectAttempts += 1
        task.lastInjectAt = Date()
        if let failure = Targets.send(line, to: child) {
            // A reported transport failure did not enter the receipt window; the next beat may
            // try again immediately, still under the same total-attempt ceiling.
            task.lastInjectAt = nil
            if task.injectAttempts >= briefingAttemptLimit {
                guard replaceTask(task, expecting: .spawning) else { return false }
                finalize(task.id, as: .spawnFailed, summary: "Could not type into the child: \(failure)")
                return false
            }
            guard replaceTask(task, expecting: .spawning) else { return false }
            return true
        }
        // `Targets.send` proves only that bytes reached the tty. Keep the secret and remain in
        // `spawning` until the assistant's own transcript proves those bytes became a user turn.
        guard replaceTask(task, expecting: .spawning) else { return false }
        RemoteAuth.audit("orchestrator.brief.inject", ["task": task.id,
                                                        "child": task.childTerminalId ?? "?",
                                                        "attempt": String(task.injectAttempts)])
        return true
    }

    /// Advance one handoff while it is waiting for a composer or a transcript receipt. This is
    /// deliberately not a task watcher: the entry disappears the instant delivery settles.
    private static func handoffStep(_ id: String) {
        lock.lock()
        guard var delivery = handoffDeliveries[id], let envelope = handoffs[id],
              envelope.state == .opening else {
            handoffDeliveries.removeValue(forKey: id)
            lock.unlock()
            return
        }
        lock.unlock()

        if Date().timeIntervalSince(delivery.spawnedAt) > readyLimit {
            settleHandoff(id, delivered: false, assistant: delivery.assistant,
                          why: "prompt_timeout")
            return
        }
        guard let child = target(withID: delivery.terminalID),
              child.assistant == delivery.assistant else { return }

        let line = handoffLine(id: id)
        let recorded: Bool
        let transcriptKnown: Bool
        switch delivery.assistant {
        case .claude:
            var sawTranscript = false
            let url = Transcript.locate(cwd: envelope.projectDir, tabTitle: child.name,
                                        startedAt: delivery.spawnedAt,
                                        sessionID: SessionRegistry.sessionID(of: child),
                                        accepting: { url in
                sawTranscript = true
                let text = try? String(contentsOf: url, encoding: .utf8)
                return transcriptContainsHandoff(text, assistant: .claude, handoffID: id)
            })
            transcriptKnown = sawTranscript
            recorded = url != nil
        case .codex:
            if let rollout = Codex.locate(cwd: envelope.projectDir,
                                          startedAt: delivery.spawnedAt,
                                          pid: Targets.pid(of: child)),
               let text = try? String(contentsOf: rollout, encoding: .utf8) {
                transcriptKnown = true
                recorded = transcriptContainsHandoff(text, assistant: .codex, handoffID: id)
            } else {
                transcriptKnown = false
                recorded = false
            }
        }
        if recorded {
            settleHandoff(id, delivered: true, assistant: delivery.assistant, why: nil)
            return
        }

        let screen = Targets.capture(child)
        if let screen, SessionState.isChoosing(screen, assistant: delivery.assistant) {
            if !delivery.answeredMenu {
                delivery.answeredMenu = true
                lock.lock()
                if handoffDeliveries[id] != nil { handoffDeliveries[id] = delivery }
                lock.unlock()
                _ = Targets.answer(0x31, to: child)
                RemoteAuth.audit("handoff.menu", ["handoff": id, "answer": "1"])
            }
            return
        }
        guard briefingInputReady(screen, assistant: delivery.assistant) else { return }
        guard handoffRetryAllowed(attempts: delivery.attempts,
                                  transcriptKnown: transcriptKnown) else { return }
        if let last = delivery.lastInjectAt,
           Date().timeIntervalSince(last) < briefingReceiptDelay { return }
        if delivery.attempts >= briefingAttemptLimit {
            settleHandoff(id, delivered: false, assistant: delivery.assistant,
                          why: "delivery_unconfirmed")
            return
        }
        delivery.attempts += 1
        delivery.lastInjectAt = Date()
        let failure = Targets.send(line, to: child)
        if failure != nil { delivery.lastInjectAt = nil }
        lock.lock()
        if handoffDeliveries[id] != nil { handoffDeliveries[id] = delivery }
        lock.unlock()
        RemoteAuth.audit("handoff.inject", ["handoff": id,
                                             "attempt": String(delivery.attempts),
                                             "ok": failure == nil ? "1" : "0"])
        if failure != nil, delivery.attempts >= briefingAttemptLimit {
            settleHandoff(id, delivered: false, assistant: delivery.assistant,
                          why: "send_failed")
        }
    }

    static func settleHandoff(_ id: String, delivered: Bool, assistant: Assistant,
                              why: String?) {
        lock.lock()
        guard var envelope = handoffs[id], envelope.state == .opening else {
            lock.unlock()
            return
        }
        envelope.state = delivered ? .delivered : .spawnFailed
        handoffs[id] = envelope
        handoffDeliveries.removeValue(forKey: id)
        lock.unlock()
        save()
        RemoteAuth.audit(delivered ? "handoff.delivered" : "handoff.undelivered",
                         ["handoff": id, "why": why ?? "delivered"])

        guard Config.shared.orchestratorNotifyRoot,
              let from = envelope.fromSession,
              let sender = target(forRootSession: from, assistant: nil,
                                  resolution: .handoff,
                                  among: rootTargets(),
                                  sessionID: Transcript.sessionID(of:)) else { return }
        let receipt = handoffReceipt(id: id, title: envelope.title, assistant: assistant,
                                     projectDir: envelope.projectDir, delivered: delivered)
        deliverTerminalNotice(ClawdlineMessage.encode(receipt), to: sender,
                              label: "handoff receipt")
    }

    /// Watch a briefed child for its result, its death, or its deadline. True when the record
    /// changed in a way worth persisting.
    private static func watch(_ task: Task) -> Bool {
        var task = task

        // The file half of the progress channel first, before anything below can finalize the
        // task or write an older snapshot back: collection refreshes this copy of the record,
        // and a last note written moments before result.json still lands.
        _ = collectProgressFile(of: &task)

        // The result file is the completion signal a sandboxed child can always give — writing
        // to /tmp needs no network approval, where a curl to loopback does.
        if let result = readResult(of: task) {
            guard replaceTask(task, expecting: .briefed) else { return false }
            finalize(task.id, as: result.status == "success" ? .success : .failure,
                     summary: result.summary, artifacts: result.artifacts,
                     verification: result.verification)
            return false
        }

        if let briefedAt = task.briefedAt,
           Date().timeIntervalSince(briefedAt) > Double(task.timeoutMinutes) * 60 {
            guard replaceTask(task, expecting: .briefed) else { return false }
            finalize(task.id, as: .timeout,
                     summary: "No result within \(task.timeoutMinutes) minutes.")
            return false
        }

        var changed = false
        if let childID = task.childTerminalId,
           let child = SessionWatch.shared.targets.first(where: { $0.id == childID }) {
            task.lastSeenChild = Date()
            // The child's own identity, once its assistant has written it down. Free for Claude
            // (a dictionary the hooks fill), one cached lsof for Codex.
            changed = noteChildIdentity(child, in: &task) || changed
            if !replaceTask(task, expecting: .briefed) { return false }
        } else if let seen = task.lastSeenChild {
            if Date().timeIntervalSince(seen) > 60 {
                guard replaceTask(task, expecting: .briefed) else { return false }
                finalize(task.id, as: .failure,
                         summary: "The child session ended without reporting a result.")
                return false
            }
        } else {
            task.lastSeenChild = Date()
            if !replaceTask(task, expecting: .briefed) { return false }
        }
        return changed
    }

    /// Fill in the assistant's own durable identity as soon as it exists. Briefing and watching
    /// share this because the transcript is now the boundary between those two states.
    private static func noteChildIdentity(_ child: TargetSession, in task: inout Task) -> Bool {
        var changed = false
        // Claude writes a process registry, so its pid pair and registry supply identity before
        // delivery pins the pair. Codex writes no registry, so it deliberately uses lsof to name
        // a rollout and its task marker to prove delivery. In either branch the marker is a
        // delivery receipt, not proof that two different processes share an identity.
        switch task.assistant {
        case .claude:
            let observedPID = Targets.pid(of: child)
            let observedStart = observedPID.flatMap { Targets.processStart(ofPID: $0) }
            let registrySessionID = SessionRegistry.sessionID(of: child)
            let registryTranscript = registrySessionID.flatMap { sessionID in
                // Match Transcript.record(of:): the id names a candidate, while locate proves
                // its file exists in the child's cwd and postdates this process.
                Transcript.locate(cwd: Targets.workingDirectory(of: child) ?? cwd(of: task),
                                  tabTitle: child.name, startedAt: task.spawnedAt,
                                  sessionID: sessionID)
            }
            let observation = ChildObservation(pid: observedPID, procStart: observedStart,
                                               registrySessionID: registrySessionID,
                                               registryTranscript: registryTranscript)
            changed = recordProcessIdentity(from: observation, in: &task) || changed
            let step = identityStep(for: task, seeing: observation)
            if case let .refuseForeignProcess(seen) = step {
                RemoteAuth.audit("orchestrator.identity.foreign", [
                    "task": task.id,
                    "recorded_pid": task.childPID.map(String.init) ?? "?",
                    "seen_pid": seen.map(String.init) ?? "?",
                    "recorded_proc_start": task.childProcStart
                        .map { String($0.timeIntervalSince1970) } ?? "?",
                    "seen_proc_start": observation.procStart
                        .map { String($0.timeIntervalSince1970) } ?? "?",
                ])
                return false
            }
            switch step {
            case let .useRegistry(sessionID, transcript):
                // Keep the old ladder as a transition control, but run it only once per distinct
                // answer. A growing transcript otherwise defeats its signature cache every beat.
                if beginRegistryControl(for: sessionID, in: &task) {
                    var legacyTask = task
                    legacyTask.childSessionId = nil
                    legacyTask.transcriptPath = nil
                    legacyTask.transcriptProven = false
                    _ = noteClaudeIdentityFromLegacySources(child, in: &legacyTask)
                    changed = true
                    if let comparison = identityComparison(registrySessionID: sessionID,
                                                           registryTranscript: transcript,
                                                           legacyTask: legacyTask) {
                        RemoteAuth.audit("orchestrator.identity.mismatch", [
                            "task": task.id,
                            "registry_session": comparison.registrySessionID,
                            "registry_transcript": comparison.registryTranscriptPath,
                            "legacy_session": comparison.legacySessionID ?? "?",
                            "legacy_transcript": comparison.legacyTranscriptPath ?? "?",
                        ])
                    } else {
                        RemoteAuth.audit("orchestrator.identity.agreement", [
                            "task": task.id,
                            "session": sessionID,
                        ])
                    }
                }
                changed = adoptRegistryIdentity(sessionID: sessionID, transcript: transcript,
                                                in: &task) || changed
                changed = noteTranscriptProof(in: &task) || changed
            case .none:
                // A missing or not-yet-verifiable registry answer is only a source miss. The
                // complete note + locate ladder remains the fallback and decides normally.
                changed = noteClaudeIdentityFromLegacySources(child, in: &task) || changed
            case .refuseForeignProcess:
                break
            }
        case .codex:
            let observedPID = Targets.pid(of: child)
            let observedStart = observedPID.flatMap { Targets.processStart(ofPID: $0) }
            let observation = ChildObservation(pid: observedPID, procStart: observedStart)
            changed = recordProcessIdentity(from: observation, in: &task) || changed
            guard let recordedPID = task.childPID, recordedPID == observedPID,
                  let recordedStart = task.childProcStart, let observedStart,
                  abs(recordedStart.timeIntervalSince(observedStart))
                    <= SessionRegistry.startTolerance else {
                RemoteAuth.audit("orchestrator.identity.foreign", [
                    "task": task.id,
                    "recorded_pid": task.childPID.map(String.init) ?? "?",
                    "seen_pid": observedPID.map(String.init) ?? "?",
                ])
                return false
            }
            if task.transcriptPath == nil,
               let rollout = Codex.locate(cwd: cwd(of: task), startedAt: task.spawnedAt,
                                           pid: observedPID) {
                task.transcriptPath = rollout.path
                task.transcriptProven = false
                changed = true
            }
            if task.childSessionId == nil, let path = task.transcriptPath,
               let threadID = Codex.head(of: URL(fileURLWithPath: path))?.id {
                task.childSessionId = threadID
                changed = true
                // The thread gets the task's name too, so `codex resume` lists it by what it did
                // rather than by the first line it was handed.
                if !threadID.isEmpty {
                    CodexNaming.shared.name(task.title, thread: threadID, target: child)
                }
            }
            changed = noteTranscriptProof(in: &task) || changed
        }
        return changed
    }

    /// The complete pre-registry Claude identity ladder. A registry miss runs it normally; a new
    /// registry answer runs an independent copy once so disagreements leave an audit receipt.
    private static func noteClaudeIdentityFromLegacySources(_ child: TargetSession,
                                                            in task: inout Task) -> Bool {
        var changed = noteTranscriptProof(in: &task)
        if let noted = childSessionID(from: HookBridge.note(for: child),
                                      spawnedAt: task.spawnedAt) {
            changed = adoptHookIdentity(sessionID: noted, in: &task) || changed
        }
        // Without an id, timestamps and titles only rank candidates, so the delivered task
        // marker must prove the winner. A current hook instead names the exact file needed to
        // verify delivery before that marker has appeared; proof is recorded once it does.
        let accepting: ((URL) -> Bool)?
        if task.childSessionId == nil {
            let taskID = task.id
            accepting = { url in
                transcriptBelongsToTask(url, assistant: .claude, taskID: taskID)
            }
        } else {
            accepting = nil
        }
        if task.transcriptPath == nil,
           let found = Transcript.locate(cwd: cwd(of: task), tabTitle: child.name,
                                         startedAt: task.spawnedAt,
                                         sessionID: task.childSessionId,
                                         accepting: accepting) {
            task.transcriptPath = found.path
            task.childSessionId = found.deletingPathExtension().lastPathComponent
            task.transcriptProven = accepting != nil
            changed = true
        }
        changed = noteTranscriptProof(in: &task) || changed
        return changed
    }

    /// Promote a candidate path only after its immutable first user turn proves ownership.
    /// Every result is cached while the file signature is unchanged; growth after delivery
    /// naturally causes the pre-briefing candidate to be read and proved again.
    static func noteTranscriptProof(in task: inout Task) -> Bool {
        if task.transcriptProven {
            _ = registerOwnedScratchpad(for: task)
            return false
        }
        guard let path = task.transcriptPath else { return false }
        let transcript = URL(fileURLWithPath: path)
        let ownership = transcriptOwnership(transcript, assistant: task.assistant, taskID: task.id)
        return applyTranscriptOwnership(ownership, transcript: transcript, to: &task)
    }

    /// Consume the three-state read separately from doing it, so restoration policy can be
    /// exercised without depending on a polling beat or a live terminal.
    static func applyTranscriptOwnership(_ ownership: TranscriptOwnership, transcript: URL,
                                         to task: inout Task) -> Bool {
        switch ownership {
        case .unavailable:
            return false
        case .other:
            // Before delivery, another first turn merely means the nominated file was observed
            // too early; clearing it here would also erase the exact path needed to verify and
            // retry this child's briefing. Once briefed, Codex still needs this disproof to clear
            // an lsof rollout from another thread because it has no process registry to replace it.
            guard task.state == .briefed else { return false }
            task.childSessionId = nil
            task.transcriptPath = nil
            task.transcriptProven = false
            return true
        case .belongs:
            break
        }
        switch task.assistant {
        case .claude:
            task.childSessionId = transcript.deletingPathExtension().lastPathComponent
        case .codex:
            guard let id = Codex.head(of: transcript)?.id else { return false }
            task.childSessionId = id
        }
        task.transcriptProven = true
        _ = registerOwnedScratchpad(for: task)
        return true
    }

    /// Transcript proof and ledger registration are separate facts. A failed append never turns
    /// into a positive ownership receipt; ``noteTranscriptProof(in:)`` retries it on later beats.
    @discardableResult
    static func registerOwnedScratchpad(for task: Task, at: Date = Date()) -> Bool {
        guard task.transcriptProven, task.assistant == .claude,
              let sessionID = task.childSessionId else { return false }
        return OwnedStorage.register(taskID: task.id, assistant: task.assistant,
                                     sessionID: sessionID, projectDir: task.projectDir, at: at)
    }

    /// A tty belongs to the tab, not to the assistant process inside it. Until the new process
    /// emits a hook, the note keyed by that tty can still identify the session that used the tab
    /// before this task was spawned.
    static func childSessionID(from note: HookBridge.Note?, spawnedAt: Date?) -> String? {
        // The hook writes integer epoch seconds while `Date()` retains subsecond precision. `>=`
        // deliberately accepts the exact-second boundary; a genuinely later note truncated to
        // just before `spawnedAt` is rejected safely until a subsequent hook refreshes it.
        guard let note, let spawnedAt, note.at >= spawnedAt else { return nil }
        return note.session
    }

    /// What to do about a finished child's tab, at one instant.
    ///
    /// Split out from the beat that runs it for the same reason ``Targets/Farewell/step(elapsed:pid:termed:killed:)``
    /// is: the decisions are here, the terminal is there, and a decision that can only be
    /// exercised by closing somebody's real tab is a decision with no tests. Every branch below
    /// is one, and one of them is irreversible — see `.forget`.
    enum CloseStep: Equatable {
        /// Not yet. The deadline stands and the next beat asks again.
        case wait
        /// That tab has gone, or belongs to somebody else now. Drop the deadline: nothing here
        /// is ours to close, and nothing will be.
        case forget
        /// Take it. `justTheTab` when there is nobody in there to say the quit word to.
        case close(justTheTab: Bool)
    }

    /// `busy` is a closure because answering it costs a screen capture, and it is only worth
    /// paying for once everything cheaper has already said the tab is ours and due.
    static func closeStep(now: Date, closeAt: Date, inventoryComplete: Bool,
                          inventoryEmpty: Bool = false,
                          emptyInventoryAuthoritative: Bool = false,
                          automationReady: Bool,
                          intervention: TerminalIntervention? = nil,
                          child: TargetSession?, assistant: Assistant, tty: String?,
                          activity: () -> Targets.SafeCloseActivity) -> CloseStep {
        guard now >= closeAt else { return .wait }
        // An omitted row has meaning only in a complete inventory, and no terminal command is
        // safe while the previous Apple event is waiting on a modal answer. Both conditions are
        // re-evaluated on every beat, so one later complete scan is the recovery signal.
        guard inventoryComplete,
              terminalCloseRetryAllowed(intervention: intervention,
                                        automationReady: automationReady) else { return .wait }
        guard let child else {
            // **`forget` is permanent**, and a reading with no terminals in it at all is not a
            // reading that found this one gone: it is the first seconds after launch, or iTerm2
            // not answering. Deciding on one closed nothing and left the tab standing for good.
            return !inventoryEmpty || emptyInventoryAuthoritative ? .forget : .wait
        }
        guard child.assistant == nil || child.assistant == assistant,
              tty == nil || child.tty == tty else { return .forget }
        // A child still mid-turn is left alone. Elapsed time is never evidence that a process is
        // gone, so there is no force-after-ten-minutes escape hatch.
        // Unknown is not idle. Capture failure, an unreadable screen, or a classifier that
        // cannot prove what it saw therefore has exactly the same irreversible authority as a
        // positively busy child: none.
        guard activity() == .idle else { return .wait }
        return .close(justTheTab: child.assistant == nil)
    }

    /// Compatibility seam for older pure decision callers. Production uses the tri-state form
    /// above so an unreadable screen cannot collapse into `false == idle`.
    static func closeStep(now: Date, closeAt: Date, inventoryComplete: Bool,
                          inventoryEmpty: Bool = false,
                          emptyInventoryAuthoritative: Bool = false,
                          automationReady: Bool,
                          intervention: TerminalIntervention? = nil,
                          child: TargetSession?, assistant: Assistant, tty: String?,
                          busy: () -> Bool) -> CloseStep {
        closeStep(now: now, closeAt: closeAt, inventoryComplete: inventoryComplete,
                  inventoryEmpty: inventoryEmpty,
                  emptyInventoryAuthoritative: emptyInventoryAuthoritative,
                  automationReady: automationReady, intervention: intervention,
                  child: child, assistant: assistant, tty: tty,
                  activity: { busy() ? .busy : .idle })
    }

    /// A modal failure has exactly one automatic recovery edge: a later well-formed iTerm list
    /// clears the global circuit. Non-modal failures have no equivalent positive recovery proof,
    /// so another timer beat must not repeat `/exit`, TERM or KILL.
    static func terminalCloseRetryAllowed(intervention: TerminalIntervention?,
                                          automationReady: Bool) -> Bool {
        guard automationReady else { return false }
        return intervention == nil || intervention?.kind == .iTermModal
    }

    /// Nominate a finished child's tab for closing once its linger has run out.
    ///
    /// **Nothing is decided here.** The cached inventory a beat reads may be stale, partial, or
    /// the empty list the app carries for its first few seconds, so this thread is allowed to say
    /// "look at that one" and nothing else. ``closeStep`` decides, inside the terminal broker,
    /// against an inventory taken there. Returns true only when this thread changed the record,
    /// which it never does — the answer arrives from the broker, on a later beat.
    @discardableResult
    static func closeChild(_ task: Task) -> Bool {
        closeDueChildren([task])
        return false
    }

    /// Every lingering child a beat found due, decided together against **one** inventory.
    ///
    /// Batched because the inventory is the expensive half and the broker has one serial lane.
    /// Per task it was one iTerm list plus one `list-panes` every five seconds, so eight
    /// lingering children — an ordinary fan-out finishing — would have spent most of that lane on
    /// re-reading the same terminals, and that lane is the one a phone's `/send` waits in.
    static func closeDueChildren(_ candidates: [Task]) {
        let now = Date()
        // The cheap refusals only: due, and a previous failure that has not earned another
        // attempt. Everything costlier is terminal work, and happens in the broker.
        //
        // There is deliberately no `SessionWatch.scanComplete` gate any more. It was a cached
        // reading standing in front of a decision that no longer consults one: emptiness and
        // incompleteness are `closeStep`'s to refuse, from the walk it takes itself, and a gate
        // that cannot be set is also a gate no test can traverse.
        let automationReady = ITerm.automationReady
        let due = candidates.filter { task in
            guard let closeAt = task.closeAt, task.childTerminalId != nil,
                  now >= closeAt else { return false }
            return terminalCloseRetryAllowed(intervention: task.terminalIntervention,
                                             automationReady: automationReady)
        }
        let admitted = due.filter { beginClosing($0.id) }
        guard !admitted.isEmpty else { return }
        let ok = RemoteServer.shared.enqueueTerminalCommand(
            channels: admitted.compactMap(\.childTerminalId)
        ) {
            let inventory = Targets.safeCloseInventory()
            for task in admitted {
                guard let childID = task.childTerminalId, let closeAt = task.closeAt else {
                    DispatchQueue.main.async { finishClosing(task.id) }
                    continue
                }
                decideChildClose(task, childID: childID, closeAt: closeAt,
                                 inventory: inventory, end: endChildTab)
            }
        }
        if !ok { for task in admitted { finishClosing(task.id) } }
    }

    /// The one terminal operation that ends a child's tab, shared by the linger running out and
    /// by a root pressing close.
    ///
    /// Every input it acts on is taken inside the broker: a fresh complete inventory, and the
    /// screen classification read from it. `closeStep` then answers with the whole decision —
    /// wait, forget, or close — so the branch table that has tests is the branch table production
    /// runs. Always returns false: the record moves on the main thread, afterwards.
    @discardableResult
    static func takeChildTab(for task: Task, childID: String,
                             closeAt: Date = .distantPast,
                             end: @escaping (TargetSession, Bool) -> String? = endChildTab)
        -> Bool {
        guard beginClosing(task.id) else { return false }
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: childID) {
            decideChildClose(task, childID: childID, closeAt: closeAt,
                             inventory: Targets.safeCloseInventory(), end: end)
        }
        if !admitted { finishClosing(task.id) }
        return false
    }

    /// One task's whole close decision, run on the terminal queue against an inventory the caller
    /// took there. Releases that task's closing membership on main whichever way it goes.
    private static func decideChildClose(_ task: Task, childID: String, closeAt: Date,
                                         inventory: Targets.Snapshot,
                                         end: (TargetSession, Bool) -> String?) {
        let observed = inventory.sessions.first { $0.id == childID }
        let step = closeStep(now: Date(), closeAt: closeAt,
                             inventoryComplete: inventory.isComplete,
                             inventoryEmpty: inventory.sessions.isEmpty,
                             emptyInventoryAuthoritative: false,
                             automationReady: ITerm.automationReady,
                             intervention: task.terminalIntervention,
                             child: observed, assistant: task.assistant, tty: task.childTTY,
                             activity: { observed.map(Targets.safeCloseActivity) ?? .unknown })
        switch step {
        case .wait:
            DispatchQueue.main.async { finishClosing(task.id) }
        case .forget:
            DispatchQueue.main.async {
                finishClosing(task.id)
                settleNothingLeftToClose(task, childID: childID)
            }
        case .close(let justTheTab):
            guard let observed else {
                DispatchQueue.main.async { finishClosing(task.id) }
                return
            }
            RemoteAuth.audit("orchestrator.close", ["task": task.id, "child": childID,
                                                    "how": justTheTab ? "tab" : "exit"])
            let failure = end(observed, justTheTab)
            // Typed here, while the returned failure and the circuit that produced it are one
            // observation. A later main callback must not mistake an unrelated process failure
            // for a modal another terminal operation happened to open since.
            let intervention = failure.map {
                terminalIntervention(for: $0, backend: observed.backend)
            }
            DispatchQueue.main.async {
                finishClosing(task.id)
                settleClosedChild(task, childID: childID, intervention: intervention)
            }
        }
    }

    /// A fresh complete inventory proved the tab gone, or somebody else's. The deadline is
    /// permanent to drop, which is why only this reading may drop it.
    private static func settleNothingLeftToClose(_ task: Task, childID: String) {
        guard var current = held(task.id), current.closeAt != nil else { return }
        current.closeAt = nil
        current.terminalIntervention = nil
        guard replaceTask(current, expecting: current.state) else { return }
        Log.write("orchestrator: fresh inventory found nothing left to close for "
                  + "\(task.id) — dropping its linger")
        save()
        RemoteServer.shared.broadcastOrchestrator()
        if let worktree = current.worktree {
            scheduleWorktreeDisposal(worktree, taskID: current.id, why: "empty",
                                     allowCommitted: false)
        }
    }

    /// The record is read again rather than written from the snapshot the beat carried: a reclaim
    /// may have settled a deadline while the terminal was busy, and this must not put it back.
    /// The deadline is cleared only on success, so a failed close stays visible and retryable.
    private static func settleClosedChild(_ task: Task, childID: String,
                                          intervention: TerminalIntervention?) {
        guard var current = held(task.id), current.childTerminalId == childID else { return }
        if let intervention {
            current.terminalIntervention = intervention
            Log.write("orchestrator: could not close the child — \(intervention.message)")
        } else {
            current.closeAt = nil
            current.terminalIntervention = nil
        }
        guard replaceTask(current, expecting: current.state) else { return }
        save()
        RemoteServer.shared.broadcastOrchestrator()
        if intervention == nil, let worktree = current.worktree {
            scheduleWorktreeDisposal(worktree, taskID: current.id, why: "empty",
                                     allowCommitted: false)
        }
        SessionWatch.shared.nudge()
    }

    /// Main-thread membership: one automatic close per task, even when a five-second beat lands
    /// while the previous Apple event is still in flight.
    private static let closingTasksLock = NSLock()
    private static var closingTasks: Set<String> = []

    private static func beginClosing(_ id: String) -> Bool {
        closingTasksLock.lock(); defer { closingTasksLock.unlock() }
        return closingTasks.insert(id).inserted
    }

    private static func finishClosing(_ id: String) {
        closingTasksLock.lock(); closingTasks.remove(id); closingTasksLock.unlock()
    }

    static func beginTerminalCloseForTesting(_ id: String) -> Bool { beginClosing(id) }
    static func finishTerminalCloseForTesting(_ id: String) { finishClosing(id) }
    static func terminalClosesInFlightForTesting() -> Int {
        closingTasksLock.lock(); defer { closingTasksLock.unlock() }
        return closingTasks.count
    }

    /// Take a child's tab away: the quit word first when there is an assistant in there to hear
    /// it, otherwise the tab on its own.
    ///
    /// Shared by the linger and by the cascade so the two cannot drift into closing a tab two
    /// different ways. Blocks while the child leaves — `Targets.end` types the word and waits for
    /// the process to go rather than assuming it did — so both callers are somewhere that can
    /// afford a wait measured in seconds when the child is busy.
    private static func endChildTab(_ child: TargetSession, justTheTab: Bool) -> String? {
        if justTheTab {
            return Targets.closeIfAssistantGone(child)
        }
        return Targets.end(child)
    }

    private struct ChildResult {
        var status: String
        var summary: String?
        var artifacts: [String]
        var verification: Verification?
    }

    static func verification(from raw: Any?) -> Verification? {
        guard let obj = raw as? [String: Any],
              let runs = obj["runs"] as? Int, runs >= 0,
              let seconds = obj["seconds"] as? Int, seconds >= 0,
              let lastRaw = obj["last"] as? String,
              let last = Verification.Last(rawValue: lastRaw),
              let scope = obj["scope"] as? String,
              !scope.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              scope.count <= 300 else { return nil }
        return Verification(runs: runs, seconds: seconds, last: last, scope: scope)
    }

    /// The child's `result.json`, if it exists and can prove it came from the child.
    private static func readResult(of task: Task) -> ChildResult? {
        let file = task.dir.appendingPathComponent("result.json")
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return nil
        }
        guard let secret = obj["task_secret"] as? String,
              RemoteAuth.constantTimeEquals(task.secretHash, hash(ofSecret: secret)) else {
            // Somebody wrote a result they could not have been asked for. Once in the log is
            // enough — the file is left alone so the evidence is where the log says it is.
            lock.lock()
            let first = badResults.insert(task.id).inserted
            lock.unlock()
            if first {
                RemoteAuth.audit("orchestrator.result", ["task": task.id, "ok": "0", "why": "bad_secret"])
            }
            return nil
        }
        return ChildResult(status: obj["status"] as? String ?? "failure",
                           summary: (obj["summary"] as? String).map { String($0.prefix(2000)) },
                           artifacts: (obj["artifacts"] as? [String] ?? []).map { String($0.prefix(300)) },
                           verification: verification(from: obj["verification"]))
    }

    private static var badResults: Set<String> = []

    // MARK: - Finalize

    /// Main thread. The one place a task ends, whatever ended it.
    static func finalize(_ taskID: String, as outcome: State,
                         summary: String?, artifacts: [String] = [],
                         verification: Verification? = nil,
                         pumpQueue: Bool = true) {
        lock.lock()
        guard var task = tasks[taskID], !task.state.isTerminal else { lock.unlock(); return }
        task.state = outcome
        task.finishedAt = Date()
        task.queuedSecret = nil
        if let summary { task.summary = summary }
        if !artifacts.isEmpty { task.artifacts = artifacts }
        if task.completionDelivery == nil,
           task.parentTaskId != nil || task.rootSessionId != nil {
            let now = task.finishedAt ?? Date()
            task.completionDelivery = CompletionDelivery(
                noticeID: UUID().uuidString.lowercased(), created: now,
                state: .pending, attempts: 0, nextRetryAt: now, persisted: false)
        }
        if let verification { task.verification = verification }
        secrets.removeValue(forKey: taskID)
        let linger = Config.shared.orchestratorChildLinger
        if task.attachSessionId != nil {
            task.closeAt = nil
        } else if task.scheduleID != nil {
            task.closeAt = scheduledCloseAt(policy: task.scheduleCloseTab, outcome: outcome,
                                             now: Date(), hasChild: task.childTerminalId != nil,
                                             linger: TimeInterval(linger),
                                             briefed: task.briefedAt != nil)
        } else {
            // Only a child that reported gets its tab held open and then closed for it. One that
            // timed out has something on its screen worth reading, and stays.
            if outcome == .success || outcome == .failure, linger >= 0,
               task.childTerminalId != nil {
                task.closeAt = Date().addingTimeInterval(TimeInterval(linger))
            }
            // A spawn that never reached briefing is the exception, and it goes now rather than
            // staying. There is nothing of this task on that screen — the session was opened and
            // never spoken to, so what is on it is a fresh prompt, which explains nothing that the
            // summary does not say better.
            //
            // **And leaving it is not free.** Each one is a live assistant holding a slot, and the
            // usual cause of failing to reach a prompt is that too many sessions were starting at
            // once. Keep them and the next spawn is slower for exactly the reason the last one
            // failed, which is a failure that feeds itself: four dead tabs were still running when
            // this was written, and the two spawns after them timed out too.
            if outcome == .spawnFailed, task.briefedAt == nil, linger >= 0,
               task.childTerminalId != nil {
                task.closeAt = Date()
            }
        }
        tasks[taskID] = task
        lock.unlock()

        // Purely observational, and deliberately outside the lock like the result and usage
        // reads below: one `stat` per claimed path is not the kind of latency that should
        // serialize every other request in flight.
        let untouched = untouchedClaims(task)
        if !untouched.isEmpty {
            lock.lock()
            if var current = tasks[taskID], current.state == outcome {
                current.untouchedClaims = untouched
                tasks[taskID] = current
            }
            lock.unlock()
            task.untouchedClaims = untouched
        }

        // The result file can carry words the finalizer was not handed — the HTTP route sends
        // only a sentence, the file has the artifact list and the verification record too. Asked
        // only when one of the three is still missing: `readResult` is a disk read and a secret
        // comparison that files a `badResults` entry, and running it for a `cancelled` task that
        // never wrote a result is a cost with no answer at the end of it.
        if task.summary == nil || task.artifacts.isEmpty || task.verification == nil,
           let result = readResult(of: task) {
            lock.lock()
            if task.summary == nil { task.summary = result.summary }
            if task.artifacts.isEmpty { task.artifacts = result.artifacts }
            if task.verification == nil { task.verification = result.verification }
            task.resultVerifiedAt = task.resultVerifiedAt ?? Date()
            tasks[taskID] = task
            lock.unlock()
        }

        if let usage = harvestUsage(task) {
            lock.lock()
            task.usage = usage
            tasks[taskID] = task
            lock.unlock()
        }
        task.workCleanupAt = reclaimDeadline(
            minutes: Config.shared.orchestratorWorkGraceMinutes, outcome: outcome)
        // Only a task with its own disposable checkout has build output of its own to reclaim. A
        // task working in a shared tree would otherwise be handed the person's `.build/`.
        task.buildCleanupAt = task.worktree == nil ? nil : reclaimDeadline(
            minutes: Config.shared.orchestratorBuildGraceMinutes, outcome: outcome)
        lock.lock()
        tasks[taskID] = task
        lock.unlock()
        if task.workCleanupAt.map({ $0 <= Date() }) == true {
            _ = reclaimTaskWorkIfDue(taskID)
            task.workCleanupAt = held(taskID)?.workCleanupAt
        }
        if task.buildCleanupAt.map({ $0 <= Date() }) == true {
            _ = reclaimTaskBuildIfDue(taskID)
            task.buildCleanupAt = held(taskID)?.buildCleanupAt
        }
        guard let worktree = task.worktree else {
            completeFinalization(task, outcome: outcome, pumpQueue: pumpQueue)
            return
        }

        // Git inspection is deliberately not a main-thread prerequisite for the terminal state,
        // notification, cascade, or serialize pump. Merge only the worktree field back when its
        // best-effort receipt arrives, so a simultaneous closeStep cannot have its closeAt
        // decision resurrected by this older snapshot.
        let removeEmpty = reclaimsEmptyWorktree(task, outcome: outcome)
        worktreeQueue.async {
            let refreshed = refreshedWorktree(worktree)
            if removeEmpty {
                disposeWorktree(refreshed, taskID: task.id, why: "empty",
                                allowCommitted: false)
            }
            DispatchQueue.main.async {
                var changed = false
                lock.lock()
                if var current = tasks[taskID], current.state == outcome {
                    if current.worktree?.path == refreshed.path {
                        current.worktree = refreshed
                        tasks[taskID] = current
                        changed = true
                    }
                }
                lock.unlock()
                if changed {
                    save()
                    RemoteServer.shared.broadcastOrchestrator()
                }
            }
        }
        completeFinalization(task, outcome: outcome, pumpQueue: pumpQueue)
    }

    /// Main-thread tail of finalization, after any worktree receipt has arrived.
    private static func completeFinalization(_ task: Task, outcome: State,
                                             pumpQueue: Bool) {
        let persisted = save()
        RemoteAuth.audit("orchestrator.finish", ["task": task.id, "state": outcome.rawValue])
        RemoteServer.shared.broadcastOrchestrator()
        if persisted {
            if let noticeID = task.completionDelivery?.noticeID {
                markCompletionEnvelopePersisted(taskID: task.id, noticeID: noticeID)
            }
            scheduleCompletionPump()
        } else if task.completionDelivery != nil {
            // The terminal outcome and its envelope were one attempted snapshot. If that write
            // failed, leave no in-memory-only envelope for a later beat to deliver. Explicit
            // reconciliation (or the next restart) can create and persist a fresh envelope.
            lock.lock()
            if var current = tasks[task.id],
               current.completionDelivery?.noticeID == task.completionDelivery?.noticeID {
                current.completionDelivery = nil
                tasks[task.id] = current
            }
            lock.unlock()
            RemoteAuth.audit("orchestrator.completion.defer", [
                "task": task.id, "why": "store_failed",
            ])
        }
        let scheduleFailure = task.scheduleID != nil && task.scheduleNotifyFailure
            && (outcome == .failure || outcome == .timeout || outcome == .spawnFailed)
        // A schedule failure has its own immediate, policy-controlled notification. Do not also
        // register the one-task anonymous batch that would announce the same ending next beat.
        if !scheduleFailure { noteEnded(task) }
        if scheduleFailure {
            let title = task.rootLabel ?? task.title
            WebPush.send(title: title,
                         body: "Scheduled task finished \(outcome.rawValue).",
                         url: "/", tag: "schedule-\(task.scheduleID ?? task.id)-failed",
                         icon: RemoteIcon.projectPath(
                            for: ProjectIcon.grid(forCwd: task.projectDir)))
        }
        endWorkHandedOnBy(task)
        if pumpQueue { scheduleSerializePump() }
    }

    /// When a reclaimable directory falls due, from one grace setting and one ending.
    ///
    /// Shared by `work/` and by the isolated checkout's build output so the two settings cannot
    /// drift into meaning different things: `0` and every success go now, a positive number is
    /// minutes of diagnostic grace, and `-1` hands the directory to the 24-hour sweep.
    static func reclaimDeadline(minutes: Int, outcome: State, now: Date = Date()) -> Date? {
        if outcome == .success || minutes == 0 { return now }
        if minutes > 0 { return now.addingTimeInterval(TimeInterval(minutes * 60)) }
        return nil
    }

    /// Remove only the heavyweight task-owned scratch directory. A missing directory is already
    /// the desired result; a real filesystem refusal keeps the deadline so a later beat retries.
    @discardableResult
    static func reclaimTaskWorkIfDue(_ taskID: String, now: Date = Date()) -> Bool {
        guard let snapshot = held(taskID), snapshot.state.isTerminal,
              let due = snapshot.workCleanupAt, due <= now else { return false }
        let work = snapshot.dir.appendingPathComponent("work", isDirectory: true)
        let manager = FileManager.default
        if manager.fileExists(atPath: work.path) { try? manager.removeItem(at: work) }
        guard !manager.fileExists(atPath: work.path) else { return false }
        lock.lock()
        guard var current = tasks[taskID], current.state.isTerminal,
              current.workCleanupAt == due else {
            lock.unlock()
            return false
        }
        current.workCleanupAt = nil
        tasks[taskID] = current
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.work.reclaimed", ["task": taskID])
        return true
    }

    /// Remove only the reproducible build output inside an isolated checkout, on the same
    /// contract as ``reclaimTaskWorkIfDue``: a missing directory is the desired result, and a
    /// real filesystem refusal keeps the deadline so a later beat retries.
    ///
    /// **Why it is not the worktree sweep's job.** Disposing a whole checkout waits for the
    /// 24-hour cutoff *and* for `landing?.state != .pending`, so a delivery waiting to be landed
    /// keeps every object file it built for as long as the landing is open — which on this Mac
    /// was 814 MB across five open landings. A pending landing needs the source and the branch;
    /// it has never needed the object files, and this deliberately does not consult it.
    ///
    /// The checkout, its tracked files and the delivery branch are untouched: the only path this
    /// will ever remove is `<child cwd>/.build`, and only for a task that owns that checkout. The
    /// registry decoder proves `worktree.path` is the task-id-derived checkout and `cwd` is inside
    /// it before a restored record can reach this function.
    @discardableResult
    static func reclaimTaskBuildIfDue(_ taskID: String, now: Date = Date()) -> Bool {
        guard let snapshot = held(taskID), snapshot.state.isTerminal,
              let due = snapshot.buildCleanupAt, due <= now,
              let worktree = snapshot.worktree else { return false }
        let build = URL(fileURLWithPath: worktree.cwd, isDirectory: true)
            .appendingPathComponent(".build", isDirectory: true)
        let manager = FileManager.default
        if manager.fileExists(atPath: build.path) { try? manager.removeItem(at: build) }
        guard !manager.fileExists(atPath: build.path) else { return false }
        lock.lock()
        guard var current = tasks[taskID], current.state.isTerminal,
              current.buildCleanupAt == due else {
            lock.unlock()
            return false
        }
        current.buildCleanupAt = nil
        tasks[taskID] = current
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.build.reclaimed",
                         ["task": taskID, "path": build.path])
        return true
    }

    /// A finished task takes whatever it handed on with it.
    ///
    /// A child hands nothing on now — the tree is one level deep — so in a live tree this finds
    /// nothing, and that is the point: it is the cleanup that keeps a record from an older build,
    /// or a task that somehow named a parent below the floor, from outliving whoever was waiting
    /// for it. What that leaves behind otherwise is a session running for a task that no longer
    /// exists — nobody waiting for its answer, nobody watching its tab, and a row on the list at
    /// the top level with a `Child` chip and nothing above it, which is the shape somebody
    /// reported as a bug in the grouping.
    ///
    /// Queued descendants are finalized on main before the serialize pump can open them. Running
    /// descendants move off the main thread because `cancelInPlace` types a quit word and waits
    /// for the tab to actually go.
    private static func endWorkHandedOnBy(_ task: Task) {
        let below = liveTasks(under: [task.id])
        guard !below.isEmpty else { return }
        var running: [String] = []
        for id in below {
            guard let child = held(id) else { continue }
            if child.state == .queued {
                RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_finished",
                                                         "parent": task.id,
                                                         "parentState": task.state.rawValue])
                finalize(id, as: .cancelled, summary: "Cancelled because its parent finished.",
                         pumpQueue: false)
            } else {
                running.append(id)
            }
        }
        guard !running.isEmpty else { return }
        DispatchQueue.global(qos: .utility).async {
            for id in running {
                guard let child = held(id) else { continue }
                RemoteAuth.audit("orchestrator.cancel", ["task": id, "why": "parent_finished",
                                                         "parent": task.id,
                                                         "parentState": task.state.rawValue])
                cancelInPlace(child)
            }
        }
    }

    /// One compact semantic message typed into whatever is waiting for this task, so the
    /// conversation that asked for the work is the conversation that hears it finished.
    ///
    /// **Two readers, told different things.** A root is a conversation with a person behind it,
    /// and what it needs is enough to go and look: the id, the state, the file. A child is a
    /// program on a deadline with siblings still running, and it has exactly one question — may I
    /// write my own result.json yet — so it is told the answer to that instead.
    ///
    /// **Finding the reader only ever worked one level down, and that was a bug rather than a
    /// design.** A root writes `root.session_id` into the task it dispatches, so a depth-1 task
    /// can be traced back to a tab through the hook notes. The briefing tells a child to dispatch
    /// with `root.parent_task` and nothing else — deliberately, because a Codex child has no hook
    /// note to be found by — so at depth 2 this guard failed on the first line and the line was
    /// dropped in silence. What was left was the polling loop the briefing prescribes, which is a
    /// child spending turns on `sleep`. The parent task's own terminal is the answer, and
    /// ``record(of:)`` had been resolving it that way all along.
    static func timeoutClaimNotice(for task: Task) -> String {
        guard task.state == .timeout, !task.claims.isEmpty else { return "" }
        return " — claims released; child tab may still be writing"
    }

    /// Claimed paths this task's child never actually touched, judged once at the terminal
    /// transition: the path's own mtime is before `spawnedAt`, or nothing is there any more.
    /// Purely observational — nothing here blocks anything, and it exists so a root sees when
    /// it declared wider than it needed and can claim narrower next time. `mtime` is injected
    /// so the judgment is pure in tests; production reads the real filesystem. A task that
    /// never spawned has no baseline to judge against, so nothing is reported — and neither is
    /// one that never actually ran to a real ending: `timeout`, `spawn_failed`, and `cancelled`
    /// leave a child's tab in an unknown state (still writing, never opened, or stopped mid-way),
    /// so "never touched" would either be wrong or, worse, an instruction the finish line hands
    /// an agent that is still following it. Only `success` and `failure` are judged.
    static func untouchedClaims(_ task: Task,
                                mtime: (String) -> Date? = fileModificationDate) -> [String] {
        guard task.state == .success || task.state == .failure,
              let spawnedAt = task.spawnedAt, task.claimsDeclared,
              task.claims.count == task.claimKeys.count else { return [] }
        return zip(task.claims, task.claimKeys).compactMap { relative, absolute -> String? in
            var isDirectory: ObjCBool = false
            if FileManager.default.fileExists(atPath: absolute, isDirectory: &isDirectory),
               isDirectory.boolValue {
                // A directory's own mtime only moves when something is added, removed, or
                // renamed directly inside it — a file edited in place further down never touches
                // it, which is exactly the false "untouched" a directory-shaped claim (the
                // common spelling: "Sources", "docs") produced before this recursive, bounded
                // walk replaced the single `stat`.
                switch subtreeTouchedSince(absolute, spawnedAt: spawnedAt) {
                case .some(true), .none: return nil
                case .some(false): return relative
                }
            }
            guard let modified = mtime(absolute), modified >= spawnedAt else { return relative }
            return nil
        }
    }

    private static func fileModificationDate(_ path: String) -> Date? {
        (try? FileManager.default.attributesOfItem(atPath: path))?[.modificationDate] as? Date
    }

    /// Bounded recursive scan behind the directory branch of `untouchedClaims`. Walks depth-first,
    /// skipping `.git` and `node_modules`, and answers `true` as soon as anything at or after
    /// `spawnedAt` is found — the directory itself, a leaf file, or an intermediate directory (so
    /// an add/remove/rename anywhere in the subtree is still caught, not only a content edit).
    /// `nil` means the scan reached `scanCap` entries before finding anything, so a claim this
    /// large is left unknown rather than judged from a partial walk — see docs/orchestrator.md's
    /// terminal claims audit section for the bound this enforces.
    static func subtreeTouchedSince(_ path: String, spawnedAt: Date, scanCap: Int = 2_000) -> Bool? {
        let fm = FileManager.default
        func modifiedAtOrAfter(_ candidate: String) -> Bool {
            ((try? fm.attributesOfItem(atPath: candidate))?[.modificationDate] as? Date)
                .map { $0 >= spawnedAt } ?? false
        }
        if modifiedAtOrAfter(path) { return true }
        var pending = [path]
        var scanned = 0
        while let current = pending.popLast() {
            guard let entries = try? fm.contentsOfDirectory(atPath: current) else { continue }
            for name in entries where name != ".git" && name != "node_modules" {
                scanned += 1
                if scanned > scanCap { return nil }
                let full = current + "/" + name
                if modifiedAtOrAfter(full) { return true }
                var isDirectory: ObjCBool = false
                if fm.fileExists(atPath: full, isDirectory: &isDirectory), isDirectory.boolValue {
                    pending.append(full)
                }
            }
        }
        return false
    }

    /// One reminder appended to the finish line when the audit above found declared claims this
    /// task's child never touched — pure so the wording is tested without a filesystem. Capped at
    /// three named paths so a task with the full 32 claims all untouched cannot type tens of KB,
    /// one character at a time, into a live tab; the complete list still lives in the task record's
    /// `untouched_claims`.
    static func untouchedClaimsNotice(for task: Task) -> String {
        guard !task.untouchedClaims.isEmpty else { return "" }
        let shown = task.untouchedClaims.prefix(3)
        let remainder = task.untouchedClaims.count - shown.count
        let listed = remainder > 0
            ? shown.joined(separator: ", ") + ", and \(remainder) more"
            : shown.joined(separator: ", ")
        return " — \(task.untouchedClaims.count) claimed path(s) never touched: "
            + listed + " (claim narrower next time)"
    }

    /// One finish-line signpost for shared-tree work whose root has not recorded what happens
    /// next. Finalize calls the completion transport once, so this is advisory once and never a
    /// gate. A task whose every claim was observed untouched has left no claimed work to land.
    static func landingNotice(for task: Task) -> String {
        guard task.state.isTerminal, !task.claims.isEmpty, task.landing == nil,
              !Set(task.claims).isSubset(of: Set(task.untouchedClaims)) else { return "" }
        return " — claimed work may still be in the shared tree; mark landing pending so other "
            + "roots can see it"
    }

    /// Keep the transport protocol's closed state vocabulary compiler-coupled to task state.
    /// Non-terminal states do not produce completion notices; every terminal state maps here
    /// explicitly so adding a state cannot silently make a notification disappear.
    static func noticeState(for state: State) -> ClawdlineMessage.TaskState? {
        switch state {
        case .queued, .spawning, .briefed: return nil
        case .success: return .success
        case .failure: return .failure
        case .timeout: return .timeout
        case .cancelled: return .cancelled
        case .spawnFailed: return .spawnFailed
        }
    }

    /// The semantic completion message and its human/model-readable fallback. Kept pure so the
    /// terminal delivery, both transcript readers, and tests all meet at one boundary.
    static func taskFinishedNotice(for task: Task, audience: ClawdlineMessage.Audience,
                                   outstanding: Int = 0,
                                   noticeID: String? = nil) -> ClawdlineMessage.Notice? {
        guard let state = noticeState(for: task.state) else { return nil }
        let short = String(task.id.prefix(8))
        let resultPath = "/tmp/.clawdline/\(task.id)/result.json"
        let file = "read \(resultPath)"
        let rest = outstanding == 0
            ? "nothing else you handed on is still running"
            : "\(outstanding) more of yours still running"
        let prefix = audience == .parent ? "your task" : "task"
        var body = "[clawdline] \(prefix) \(short) (\(task.title)) finished:"
            + " \(state.rawValue) — \(file)"
        if audience == .parent { body += " — \(rest)" }
        body += timeoutClaimNotice(for: task)
        body += untouchedClaimsNotice(for: task)
        body += landingNotice(for: task)
        let acknowledgement = noticeID.map { id -> ClawdlineMessage.CompletionAcknowledgement in
            let path = "/v1/orchestrator/tasks/\(task.id)/completion/ack"
            body += " — after observing, ACK notice \(id) at \(path)"
            return .init(noticeID: id, path: path)
        }
        return ClawdlineMessage.Notice(
            event: .taskFinished(
                task: .init(id: task.id, title: task.title), state: state,
                audience: audience, resultPath: resultPath,
                outstanding: audience == .parent ? outstanding : 0,
                claimsReleased: task.state == .timeout && !task.claims.isEmpty,
                childMayStillWrite: task.state == .timeout && !task.claims.isEmpty
            ),
            body: body,
            completionAcknowledgement: acknowledgement
        )
    }

    static func completionRetryDelay(after attempts: Int) -> TimeInterval {
        guard attempts > 0 else { return 0 }
        let exponent = min(attempts - 1, 10)
        return min(5 * pow(2, Double(exponent)), completionRetryMaximum)
    }

    static func completionTransition(_ original: CompletionDelivery, at now: Date,
                                     result: CompletionTransportResult)
        -> CompletionDelivery {
        guard original.state != .acknowledged, original.state != .deadLetter else {
            return original
        }
        var delivery = original
        delivery.attempts += 1
        delivery.lastAttemptAt = now
        switch result {
        case .delivered:
            delivery.state = .delivered
            delivery.transportDeliveredAt = delivery.transportDeliveredAt ?? now
            delivery.lastError = nil
            delivery.nextRetryAt = now.addingTimeInterval(
                completionRetryDelay(after: delivery.attempts))
        case .failed(let code, let message):
            delivery.lastError = CompletionFailure(code: code,
                message: String(message.prefix(1_000)), at: now)
            if delivery.attempts >= completionAttemptLimit {
                delivery.state = .deadLetter
                delivery.deadLetterAt = now
                delivery.nextRetryAt = nil
            } else {
                // A prior transport success remains a fact even when its unacknowledged retry
                // fails. `last_error` and `next_retry_at` say why it is still pending.
                if delivery.transportDeliveredAt == nil { delivery.state = .pending }
                delivery.nextRetryAt = now.addingTimeInterval(
                    completionRetryDelay(after: delivery.attempts))
            }
        }
        return delivery
    }

    private static func notifyRoot(_ task: Task) {
        rootNotificationObserverForTesting?(task)
        guard Config.shared.orchestratorNotifyRoot else { return }
        if let parentID = task.parentTaskId, let parent = held(parentID),
           !parent.state.isTerminal, let terminalID = parent.childTerminalId,
           let terminal = target(withID: terminalID) {
            // Words into a menu confirm the highlighted row instead of typing.
            let outstanding = liveTasks(under: [parentID]).count
            guard let notice = taskFinishedNotice(for: task, audience: .parent,
                                                  outstanding: outstanding) else { return }
            let line = ClawdlineMessage.encode(notice)
            deliverTerminalNotice(line, to: terminal, label: "parent task notification")
            return
        }

        guard let rootID = task.rootSessionId else { return }
        guard let root = target(forRootSession: rootID,
                                assistant: task.rootAssistant ?? .claude,
                                resolution: .task,
                                among: rootTargets(),
                                sessionID: Transcript.sessionID(of:)) else { return }
        // Words into a menu confirm the highlighted row instead of typing. Skip rather than risk
        // answering a question on the root's behalf; the record is still in the app and the page.
        guard let notice = taskFinishedNotice(for: task, audience: .root) else { return }
        let line = ClawdlineMessage.encode(notice)
        deliverTerminalNotice(line, to: root, label: "root notification")
    }

    /// Notifications are best-effort, but their Apple Events still obey the same bounded broker
    /// as interactive writes. A modal can delay this lane without ever occupying main or HTTP.
    private static func deliverTerminalNotice(_ line: String, to target: TargetSession,
                                              label: String) {
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: target.id) {
            guard !Targets.isChoosing(target) else { return }
            if let failure = Targets.send(line, to: target) {
                Log.write("orchestrator: could not send \(label) — \(failure)")
            }
        }
        if !admitted {
            Log.write("orchestrator: could not send \(label) — terminal broker full")
        }
    }

    private static var completionPumpScheduled = false
    private static var completionPumpGeneration = 0

    private static func markCompletionEnvelopePersisted(taskID: String, noticeID: String) {
        lock.lock()
        if var task = tasks[taskID], var delivery = task.completionDelivery,
           delivery.noticeID == noticeID {
            delivery.persisted = true
            task.completionDelivery = delivery
            tasks[taskID] = task
        }
        lock.unlock()
    }

    private static func markCompletionEnvelopesPersisted(_ taskIDs: [String]) {
        lock.lock()
        for id in taskIDs {
            guard var task = tasks[id], var delivery = task.completionDelivery else { continue }
            delivery.persisted = true
            task.completionDelivery = delivery
            tasks[id] = task
        }
        lock.unlock()
    }

    static func scheduleCompletionPump() {
        lock.lock()
        guard !completionPumpScheduled else { lock.unlock(); return }
        completionPumpScheduled = true
        let generation = completionPumpGeneration
        lock.unlock()
        let work = { completionPump(generation: generation) }
        if let enqueue = completionPumpEnqueuerForTesting {
            enqueue(work)
        } else {
            completionDeliveryQueue.async(execute: work)
        }
    }

    private static func completionPump(generation: Int, now: Date = Date()) {
        lock.lock()
        guard generation == completionPumpGeneration else { lock.unlock(); return }
        lock.unlock()
        defer {
            lock.lock()
            if generation == completionPumpGeneration { completionPumpScheduled = false }
            lock.unlock()
        }
        guard Config.shared.orchestratorNotifyRoot else { return }
        // Every scheduling site already has a loaded registry. Calling `load()` here would let a
        // superseded test/restart generation repopulate memory after `forget()` invalidated it.
        lock.lock()
        let due = tasks.values.filter { task in
            guard let delivery = task.completionDelivery,
                  delivery.persisted,
                  delivery.state != .acknowledged, delivery.state != .deadLetter else {
                return false
            }
            return delivery.nextRetryAt.map { $0 <= now } ?? false
        }.sorted {
            ($0.completionDelivery?.nextRetryAt ?? .distantFuture)
                < ($1.completionDelivery?.nextRetryAt ?? .distantFuture)
        }.prefix(8).compactMap { task in
            task.completionDelivery.map { (task.id, $0.noticeID) }
        }
        lock.unlock()
        let inventory = due.isEmpty ? [] : rootTargets()
        for (id, noticeID) in due {
            lock.lock()
            let currentGeneration = completionPumpGeneration
            lock.unlock()
            guard currentGeneration == generation else { break }
            let attempt = {
                _ = completionAttempt(
                    taskID: id, now: Date(), expectedNoticeID: noticeID,
                    expectedPumpGeneration: generation,
                    deliver: { productionCompletionDelivery($0, $1, targets: inventory) })
            }
            // An attempt that will type into a terminal is admitted by the same bounded lane as
            // every other terminal write; one that cannot resolve a recipient never reaches a
            // terminal at all, so it runs here and records its typed refusal without spending a
            // slot. Admission being refused is backpressure, not a delivery failure: the attempt
            // is not made, no attempt is counted against the retry budget, and the next beat
            // brings it back. Counting it would let a busy terminal exhaust the budget of a task
            // whose root was never even asked.
            guard let task = held(id),
                  case .found(let recipient) = completionRecipient(
                      task, targets: inventory,
                      identity: RemoteServer.sessionWorkIdentity) else {
                attempt()
                continue
            }
            if !RemoteServer.shared.enqueueTerminalCommand(channel: recipient.id, attempt) {
                RemoteAuth.audit("orchestrator.completion.deferred",
                                 ["task": id, "why": "terminal_busy"])
            }
        }
    }

    /// Deterministic failure-injection seam. The current delivery is copied, the terminal call is
    /// made without the registry lock, then the transition is committed only if the same notice
    /// is still unacknowledged. An ACK racing the transport therefore always wins.
    @discardableResult
    static func completionAttempt(taskID: String, now: Date,
                                  expectedNoticeID: String? = nil,
                                  expectedPumpGeneration: Int? = nil,
                                  deliver: (Task, String) -> CompletionTransportResult) -> Bool {
        if let expectedPumpGeneration {
            lock.lock()
            let isCurrentPump = expectedPumpGeneration == completionPumpGeneration
            lock.unlock()
            guard isCurrentPump else { return false }
        }
        let task: Task?
        if let expectedPumpGeneration {
            // A scheduled pump must never call a lazy loader after its generation was revoked.
            // Result receipts are reconciled at finalization, startup, and the explicit endpoint;
            // delivery itself only consumes the already-loaded durable envelope.
            lock.lock()
            task = expectedPumpGeneration == completionPumpGeneration ? tasks[taskID] : nil
            lock.unlock()
        } else {
            if !reconcileResultReceipts(taskID: taskID, limit: 1).isEmpty { _ = save() }
            task = held(taskID)
        }
        guard let task, let delivery = task.completionDelivery,
              delivery.persisted,
              expectedNoticeID == nil || expectedNoticeID == delivery.noticeID,
              delivery.state != .acknowledged, delivery.state != .deadLetter,
              delivery.nextRetryAt.map({ $0 <= now }) == true else { return false }
        if delivery.attempts >= completionAttemptLimit {
            var exhausted = delivery
            exhausted.state = .deadLetter
            exhausted.nextRetryAt = nil
            exhausted.deadLetterAt = now
            exhausted.lastError = CompletionFailure(
                code: .acknowledgementTimeout,
                message: "No root acknowledgement arrived within the bounded retry budget.",
                at: now)
            var exhaustedStored = false
            lock.lock()
            if (expectedPumpGeneration == nil
                    || expectedPumpGeneration == completionPumpGeneration),
               var current = tasks[taskID],
               current.completionDelivery?.noticeID == delivery.noticeID,
               current.completionDelivery?.state != .acknowledged {
                current.completionDelivery = exhausted
                tasks[taskID] = current
                exhaustedStored = true
            }
            lock.unlock()
            guard exhaustedStored else { return false }
            _ = save()
            return true
        }
        let audience: ClawdlineMessage.Audience = task.parentTaskId == nil ? .root : .parent
        let outstanding = task.parentTaskId.map { liveTasks(under: [$0]).count } ?? 0
        guard let notice = taskFinishedNotice(for: task, audience: audience,
                                              outstanding: outstanding,
                                              noticeID: delivery.noticeID) else { return false }
        let result = deliver(task, ClawdlineMessage.encode(notice))
        let advanced = completionTransition(delivery, at: now, result: result)
        lock.lock()
        if let expectedPumpGeneration,
           expectedPumpGeneration != completionPumpGeneration {
            lock.unlock()
            return false
        }
        guard var current = tasks[taskID],
              current.completionDelivery?.noticeID == delivery.noticeID,
              current.completionDelivery?.state != .acknowledged else {
            lock.unlock()
            return false
        }
        current.completionDelivery = advanced
        tasks[taskID] = current
        lock.unlock()
        _ = save()
        let error = advanced.lastError?.code.rawValue ?? "none"
        RemoteAuth.audit("orchestrator.completion.attempt", [
            "task": taskID, "notice": delivery.noticeID,
            "attempt": String(advanced.attempts), "state": advanced.state.rawValue,
            "error": error,
        ])
        DispatchQueue.main.async { RemoteServer.shared.broadcastOrchestrator() }
        return true
    }

    private static func productionCompletionDelivery(_ task: Task, _ line: String)
        -> CompletionTransportResult {
        productionCompletionDelivery(task, line, targets: rootTargets())
    }

    /// `rootTargets()` hops to the main queue and blocks, so a pump pass reads the inventory once
    /// and hands the same view to every task it delivers. One snapshot per pass is also the more
    /// honest subject: the recipient a task is admitted against and the recipient it is typed
    /// into are then the same reading, and a terminal that appears or leaves mid-pass shows up as
    /// a typed refusal on the next one rather than as a disagreement inside this one.
    private static func productionCompletionDelivery(
        _ task: Task, _ line: String, targets: [TargetSession]
    ) -> CompletionTransportResult {
        completionDelivery(
            task, line, targets: targets, identity: RemoteServer.sessionWorkIdentity,
            isChoosing: Targets.isChoosing, send: { Targets.send($0, to: $1) })
    }

    /// Production-shaped completion seam. Tests supply the observed terminal inventory and
    /// transport, while the live path above supplies those same facts from SessionWatch/Targets.
    /// Which terminal this completion is owed to, decided before anything is typed.
    ///
    /// Split out of the delivery below because the attempt has to name that terminal as its
    /// broker channel, and the terminal is not known until the parent/root resolution has run.
    /// Sending outside the terminal broker would put an Apple Event for a notice beside the
    /// bounded lane every other terminal write goes through, which is the one thing the lane
    /// exists to prevent. Pure: it reads the registry and the inventory it is handed, and
    /// changes neither.
    /// Not `Result`: a refusal here is not an error to be thrown and handled, it is already the
    /// transport's own typed answer, and the caller's job is to return it unchanged.
    enum CompletionRecipient {
        case found(TargetSession)
        case refused(CompletionTransportResult)
    }

    static func completionRecipient(
        _ task: Task, targets: [TargetSession],
        identity: (TargetSession) -> SessionWorkIdentity
    ) -> CompletionRecipient {
        let recipient: TargetSession
        if let parentID = task.parentTaskId {
            guard let parent = held(parentID), !parent.state.isTerminal,
                  let terminalID = parent.childTerminalId else {
                return .refused(.failed(.rootMissing,
                    "The parent task session is no longer available."))
            }
            guard let found = targets.first(where: { $0.id == terminalID }) else {
                return .refused(.failed(.rootMissing,
                    "The parent task terminal is not in inventory."))
            }
            guard taskMatchesCurrentSession(parent, identity: identity(found)) else {
                return .refused(.failed(.identityStale,
                    "The parent terminal now belongs to a different process."))
            }
            recipient = found
        } else {
            guard let storedRoot = task.rootSessionId else {
                return .refused(.failed(.rootMissing,
                    "This task has no root conversation to notify."))
            }
            let historicalAssistant = task.rootAssistant ?? .claude
            let rebound = Coordinator.deliveryBinding(
                for: storedRoot, historicalAssistant: historicalAssistant,
                taskCreated: task.created)
            let deliveryRoot = rebound?.conversationID ?? storedRoot
            let deliveryAssistant = rebound?.assistant ?? historicalAssistant
            if let found = target(forRootSession: deliveryRoot, assistant: deliveryAssistant,
                                  resolution: .task, among: targets,
                                  sessionID: { identity($0).conversationID }) {
                recipient = found
            } else {
                let physicalCollision = targets.contains { target in
                    target.id == storedRoot || (identity(target).conversationID == deliveryRoot
                        && target.assistant != deliveryAssistant)
                }
                return .refused(.failed(physicalCollision ? .identityStale : .rootMissing,
                    physicalCollision
                    ? "The stored root identity is physical, stale, or assistant-mismatched."
                    : "No active process currently proves the root conversation identity."))
            }
        }
        return .found(recipient)
    }

    static func completionDelivery(
        _ task: Task, _ line: String, targets: [TargetSession],
        identity: (TargetSession) -> SessionWorkIdentity,
        isChoosing: (TargetSession) -> Bool,
        send: (String, TargetSession) -> String?
    ) -> CompletionTransportResult {
        let recipient: TargetSession
        switch completionRecipient(task, targets: targets, identity: identity) {
        case .refused(let result): return result
        case .found(let found): recipient = found
        }
        guard !isChoosing(recipient) else {
            return .failed(.rootChoosing, "The root is showing a chooser; no text was sent.")
        }
        guard let failure = send(line, recipient) else { return .delivered }
        let lower = failure.lowercased()
        if recipient.backend == .iterm, failure == L.t.itermBusy {
            return .failed(.itermModal, failure)
        }
        if lower.contains("timed out") || lower.contains("timeout")
            || lower.contains("did not respond") {
            return .failed(.terminalTimeout, failure)
        }
        return .failed(.transportFailed, failure)
    }

    static func acknowledgeCompletion(taskID: String, noticeID: String,
                                      now: Date = Date()) -> Reply {
        guard UUID(uuidString: noticeID) != nil else {
            return .refused(400, "bad_request", "notice_id must be a UUID.")
        }
        load()
        lock.lock()
        guard var task = tasks[taskID] else {
            lock.unlock()
            return .refused(404, "not_found", "No task named \(taskID).")
        }
        guard var delivery = task.completionDelivery else {
            lock.unlock()
            return .refused(409, "completion_not_reconciled",
                            "This terminal task has no durable completion envelope; reconcile it "
                                + "or poll result.json.")
        }
        guard delivery.persisted else {
            lock.unlock()
            return .refused(409, "completion_not_persisted",
                            "This completion envelope has not reached durable storage yet; retry.")
        }
        guard RemoteAuth.constantTimeEquals(delivery.noticeID, noticeID.lowercased()) else {
            lock.unlock()
            return .refused(409, "completion_notice_mismatch",
                            "The notice id does not identify this task's completion envelope.")
        }
        if delivery.state == .acknowledged {
            lock.unlock()
            return .ok(["ok": true, "acknowledged": true, "changed": false,
                        "notice_id": delivery.noticeID])
        }
        let previousDelivery = delivery
        delivery.state = .acknowledged
        delivery.observedAt = delivery.observedAt ?? now
        delivery.acknowledgedAt = now
        delivery.nextRetryAt = nil
        delivery.lastError = nil
        task.completionDelivery = delivery
        tasks[taskID] = task
        lock.unlock()
        guard save() else {
            lock.lock()
            // Compare-and-swap only the transition this request installed. Worktree refresh,
            // landing, close and every other field may have advanced while the atomic store write
            // was in flight; replacing the whole earlier Task would silently erase those facts.
            if var latest = tasks[taskID],
               let current = latest.completionDelivery,
               current.noticeID == delivery.noticeID,
               current.state == .acknowledged,
               current.observedAt == delivery.observedAt,
               current.acknowledgedAt == delivery.acknowledgedAt {
                latest.completionDelivery = previousDelivery
                tasks[taskID] = latest
            }
            lock.unlock()
            return .refused(500, "completion_store_failed",
                            "The acknowledgement could not be persisted; retry it.")
        }
        RemoteAuth.audit("orchestrator.completion.ack", [
            "task": taskID, "notice": delivery.noticeID,
        ])
        DispatchQueue.main.async { RemoteServer.shared.broadcastOrchestrator() }
        return .ok(["ok": true, "acknowledged": true, "changed": true,
                    "notice_id": delivery.noticeID])
    }

    /// Machine-readable outbox view. `pendingOnly` includes transport-delivered-but-unacknowledged
    /// envelopes; transport success is not observation.
    static func completionRecords(pendingOnly: Bool = false) -> [[String: Any]] {
        load()
        lock.lock()
        let values = tasks.values.filter { task in
            guard let delivery = task.completionDelivery else { return false }
            return !pendingOnly
                || (delivery.state != .acknowledged && delivery.state != .deadLetter)
        }.sorted { $0.created < $1.created }
        lock.unlock()
        return values.compactMap { task in
            guard let delivery = task.completionDelivery else { return nil }
            var row: [String: Any] = [
                "task_id": task.id, "task_state": task.state.rawValue,
                "accepted_at": Int(task.created.timeIntervalSince1970),
                "executed_at": task.finishedAt.map { Int($0.timeIntervalSince1970) } as Any? ?? NSNull(),
                "result_verified_at": task.resultVerifiedAt.map {
                    Int($0.timeIntervalSince1970)
                } as Any? ?? NSNull(),
                "delivery": completionRecord(delivery),
            ]
            if let parent = task.parentTaskId { row["parent_task_id"] = parent }
            return row
        }
    }

    private static func completionRecord(_ delivery: CompletionDelivery) -> [String: Any] {
        var out = stored(delivery)
        let optionalDates = ["next_retry_at", "last_attempt_at",
                    "transport_delivered_at", "observed_at", "acknowledged_at",
                    "dead_letter_at"]
        for key in ["created_at"] + optionalDates {
            if let raw = out[key] as? Double { out[key] = Int(raw) }
        }
        for key in optionalDates where out[key] == nil { out[key] = NSNull() }
        if var error = out["last_error"] as? [String: Any],
           let raw = error["at"] as? Double {
            error["at"] = Int(raw)
            out["last_error"] = error
        }
        if out["last_error"] == nil { out["last_error"] = NSNull() }
        return out
    }

    private static func reconcileCompletionOutbox(taskID: String?, includeDeadLetters: Bool,
                                                  now: Date)
        -> (created: Int, rearmed: Int, limited: Bool, changed: Bool,
            changedTaskIDs: [String]) {
        load()
        let floor = now.addingTimeInterval(-legacyCompletionLookback)
        var created = 0
        var rearmed = 0
        var eligible = 0
        var changedTaskIDs: [String] = []
        lock.lock()
        let ordered = tasks.values.sorted {
            ($0.finishedAt ?? $0.created) > ($1.finishedAt ?? $1.created)
        }
        for snapshot in ordered {
            guard taskID == nil || snapshot.id == taskID,
                  snapshot.state.isTerminal,
                  snapshot.parentTaskId != nil || snapshot.rootSessionId != nil else { continue }
            var task = snapshot
            if task.completionDelivery == nil {
                guard let finished = task.finishedAt, finished >= floor else { continue }
                eligible += 1
                guard created + rearmed < legacyCompletionBatchLimit else { continue }
                task.completionDelivery = CompletionDelivery(
                    noticeID: UUID().uuidString.lowercased(), created: now,
                    state: .pending, attempts: 0, nextRetryAt: now,
                    legacyReconciled: true, persisted: false)
                tasks[task.id] = task
                created += 1
                changedTaskIDs.append(task.id)
            } else if includeDeadLetters,
                      var delivery = task.completionDelivery,
                      delivery.state == .deadLetter {
                eligible += 1
                guard created + rearmed < legacyCompletionBatchLimit else { continue }
                delivery.state = .pending
                delivery.attempts = 0
                delivery.nextRetryAt = now
                delivery.lastAttemptAt = nil
                delivery.lastError = nil
                delivery.deadLetterAt = nil
                delivery.persisted = false
                task.completionDelivery = delivery
                tasks[task.id] = task
                rearmed += 1
                changedTaskIDs.append(task.id)
            }
        }
        lock.unlock()
        return (created, rearmed, eligible > created + rearmed,
                created > 0 || rearmed > 0, changedTaskIDs)
    }

    /// Best-effort legacy and `/complete` follow-up: a result file can appear after the terminal
    /// transition. Only a secret-verified file advances this receipt, and the scan is bounded so
    /// reconciliation never turns into an unbounded scratch-root walk.
    private static func reconcileResultReceipts(taskID: String?, limit: Int) -> [String] {
        lock.lock()
        let candidates = tasks.values.filter {
            ($0.state.isTerminal && $0.resultVerifiedAt == nil)
                && (taskID == nil || $0.id == taskID)
        }.sorted { ($0.finishedAt ?? $0.created) > ($1.finishedAt ?? $1.created) }
        lock.unlock()
        var changed: [String] = []
        for task in candidates.prefix(max(0, limit)) where readResult(of: task) != nil {
            lock.lock()
            if var current = tasks[task.id], current.resultVerifiedAt == nil {
                current.resultVerifiedAt = Date()
                tasks[task.id] = current
                changed.append(task.id)
            }
            lock.unlock()
        }
        return changed
    }

    static func reconcileCompletions(taskID: String?, includeDeadLetters: Bool,
                                     now: Date = Date()) -> Reply {
        if let taskID, !isTaskID(taskID) {
            return .refused(400, "bad_request", "task_id must be a lowercase UUID.")
        }
        if let taskID, held(taskID) == nil {
            return .refused(404, "not_found", "No task named \(taskID).")
        }
        lock.lock()
        let beforeRecovery = tasks
        lock.unlock()
        let outcome = reconcileCompletionOutbox(taskID: taskID,
                                                includeDeadLetters: includeDeadLetters, now: now)
        let resultReceipts = reconcileResultReceipts(taskID: taskID,
                                                     limit: legacyCompletionBatchLimit)
        if outcome.changed || !resultReceipts.isEmpty {
            guard save() else {
                lock.lock()
                for id in outcome.changedTaskIDs {
                    if var current = tasks[id], current.completionDelivery?.persisted == false {
                        current.completionDelivery = beforeRecovery[id]?.completionDelivery
                        tasks[id] = current
                    }
                }
                for id in resultReceipts {
                    if var current = tasks[id] {
                        current.resultVerifiedAt = beforeRecovery[id]?.resultVerifiedAt
                        tasks[id] = current
                    }
                }
                lock.unlock()
                return .refused(500, "completion_store_failed",
                                "The reconciled completion envelopes could not be persisted.")
            }
            markCompletionEnvelopesPersisted(outcome.changedTaskIDs)
            scheduleCompletionPump()
        }
        return .ok(["ok": true, "created": outcome.created, "rearmed": outcome.rearmed,
                    "result_verified": resultReceipts.count,
                    "limited": outcome.limited,
                    "batch_limit": legacyCompletionBatchLimit,
                    "lookback_seconds": Int(legacyCompletionLookback)])
    }

    /// Tell both roots that two live tasks can touch the same directory. Advisory and one-shot:
    /// like completion notification, a missing root, a menu, or a failed send leaves the API
    /// record as the durable answer and never changes the dispatch outcome.
    private static func notifyWorkspaceOverlaps(newTask: Task,
                                                overlaps: [WorkspaceOverlap]) {
        workspaceOverlapObserverForTesting?(newTask, overlaps)
        guard Config.shared.orchestratorNotifyRoot else { return }
        let notices = workspaceOverlapNotices(newTask: newTask, overlaps: overlaps)
        guard !notices.isEmpty else { return }
        DispatchQueue.global(qos: .utility).async {
            for notice in notices {
                sendWorkspaceOverlap(notice.line, toRootSession: notice.rootSessionID,
                                     taskID: notice.taskID)
            }
        }
    }

    /// The pure notification decision. The new task gets one aggregate line; each identifiable
    /// opposing root gets the one overlap that concerns it. Nil root ids produce no delivery.
    static func workspaceOverlapNotices(newTask: Task,
                                        overlaps: [WorkspaceOverlap]) -> [WorkspaceOverlapNotice] {
        guard !overlaps.isEmpty else { return [] }
        var notices: [WorkspaceOverlapNotice] = []
        if let root = newTask.rootSessionId {
            let details = overlaps.map { overlap in
                let other = overlap.task
                return "task \(other.id.prefix(8)) (\(other.title)) at \(overlap.sharedDir)"
            }.joined(separator: "; ")
            let line = "[clawdline] workspace overlap: task \(newTask.id.prefix(8)) "
                + "(\(newTask.title)) overlaps \(overlaps.count) active "
                + (overlaps.count == 1 ? "task: " : "tasks: ") + details
            let semantic = ClawdlineMessage.Notice(
                event: .workspaceOverlap(
                    task: .init(id: newTask.id, title: newTask.title), audience: .root,
                    overlaps: overlaps.map {
                        .init(task: .init(id: $0.task.id, title: $0.task.title),
                              path: $0.sharedDir)
                    }
                ),
                body: line
            )
            notices.append(WorkspaceOverlapNotice(rootSessionID: root, taskID: newTask.id,
                                                   line: ClawdlineMessage.encode(semantic)))
        }
        for overlap in overlaps {
            let other = overlap.task
            guard let root = other.rootSessionId else { continue }
            let line = "[clawdline] workspace overlap: task \(newTask.id.prefix(8)) "
                + "(\(newTask.title)) and task \(other.id.prefix(8)) (\(other.title)) "
                + "share \(overlap.sharedDir)"
            let semantic = ClawdlineMessage.Notice(
                event: .workspaceOverlap(
                    task: .init(id: newTask.id, title: newTask.title), audience: .root,
                    overlaps: [.init(task: .init(id: other.id, title: other.title),
                                             path: overlap.sharedDir)]
                ),
                body: line
            )
            notices.append(WorkspaceOverlapNotice(rootSessionID: root, taskID: other.id,
                                                   line: ClawdlineMessage.encode(semantic)))
        }
        return notices
    }

    private static func sendWorkspaceOverlap(_ line: String, toRootSession sessionID: String,
                                             taskID: String) {
        let assistant = held(taskID)?.rootAssistant ?? .claude
        guard let root = target(forRootSession: sessionID, assistant: assistant,
                                resolution: .task,
                                among: rootTargets(),
                                sessionID: Transcript.sessionID(of:)) else { return }
        deliverTerminalNotice(line, to: root, label: "task \(taskID) workspace overlap")
    }

    // MARK: - Telling the person, once

    /// What one root's fan-out has come to, accumulated as its tasks end.
    ///
    /// **A phone hears about a batch and never about a task, and the arithmetic is the argument.**
    /// Five children with three of their own is twenty sessions, every one of them a terminal
    /// that goes idle when it is done — so before this existed a fan-out was up to twenty
    /// identical "finished a long run" notifications, none of which said which tree it belonged
    /// to or whether anything was still outstanding. That is the same mistake ``StateHook``'s own
    /// comment argues against for root sessions, made one level down where nobody had looked.
    ///
    /// The one fact a person actually wants out of all twenty is that the work they asked for has
    /// come back, and how much of it failed. That is one sentence, and it arrives once.
    private struct Batch {
        var done = 0
        var failed = 0
        var rootLabel: String?
        var projectDir: String?
        var tasks: [SmartNotification.TaskLine] = []
    }

    /// Root key → tally. In memory only: a process that restarts in the middle of a fan-out has
    /// already interrupted it, and inventing a count for work it did not watch would be worse
    /// than saying nothing.
    private static var batches: [String: Batch] = [:]

    /// Under the lock. The session a whole tree hangs from, so every task in one fan-out is
    /// counted in the same batch however it was filed.
    ///
    /// A task that named nobody gets a key of its own rather than sharing one with every other
    /// anonymous dispatch — the alternative is two unrelated fan-outs waiting for each other.
    private static func rootKeyLocked(of task: Task) -> String {
        rootKey(of: task, among: tasks)
    }

    /// Main thread, from ``finalize(_:as:summary:artifacts:)``. This counts endings for work that
    /// actually ran: success, failure, timeout, cancellation, or a tab that closed under a child.
    /// A dispatch-time tab-opening refusal never ran and is returned directly to its caller, so it
    /// deliberately does not enter a completion batch.
    private static func noteEnded(_ task: Task) {
        lock.lock()
        let key = rootKeyLocked(of: task)
        var batch = batches[key] ?? Batch()
        batch.done += 1
        if task.state != .success { batch.failed += 1 }
        if batch.projectDir == nil { batch.projectDir = task.projectDir }
        if batch.rootLabel == nil { batch.rootLabel = task.rootLabel }
        batch.tasks.append(.init(title: task.title, state: task.state.rawValue,
                                 summary: task.summary))
        batches[key] = batch
        lock.unlock()
    }

    /// Announce any batch that has nothing left running. Called from the beat rather than from
    /// `finalize`, and that is a correctness point rather than tidiness: `finalize` cancels
    /// anything still filed under its task **asynchronously**, so at the moment it ends, a task
    /// about to be taken down still counts as live. Asking again a beat later is asking after
    /// the dust
    /// has settled, and it covers the same ground for cancellation, timeouts, and a tab somebody
    /// closed by hand.
    private static func sweepBatches() {
        lock.lock()
        guard !batches.isEmpty else { lock.unlock(); return }
        var ready: [(String, Batch)] = []
        for (key, batch) in batches {
            let running = tasks.values.contains {
                !$0.state.isTerminal && rootKeyLocked(of: $0) == key
            }
            guard !running else { continue }
            ready.append((key, batch))
            batches.removeValue(forKey: key)
        }
        lock.unlock()
        for (key, batch) in ready { announce(batch, rootKey: key) }
    }

    /// The two lines a finished fan-out is worth. Pure half in ``batchMessage(project:done:failed:)``.
    private static func announce(_ batch: Batch, rootKey key: String) {
        // The "it finished" class, so it takes the preference that class already has. A batch
        // that ended badly is still a batch that ended: whatever is worth doing about it is
        // waiting in the root's own conversation, which was told the moment each part came back.
        guard Config.shared.pushOnFinish else { return }
        let project = batch.projectDir.map { StateHook.projectName(forDirectory: $0) } ?? "Clawdline"
        let message = batchMessage(project: project, label: batch.rootLabel,
                                   done: batch.done, failed: batch.failed)
        var url = "/"
        if !key.hasPrefix("task:"),
           let root = target(forRootSession: key, assistant: nil,
                             resolution: .task,
                             among: rootTargets(),
                             sessionID: Transcript.sessionID(of:)) {
            url = "/#session=\(root.id)"
        }
        // Keyed on the root and not on a task, so a second fan-out from the same session replaces
        // the first rather than stacking under it. Smart output and the ordinary count share the
        // same one-shot delivery boundary.
        let delivery = SmartNotification.Delivery(
            title: message.title, project: project, fallbackBody: message.body,
            url: url, tag: "batch-\(key)",
            icon: RemoteIcon.projectPath(
                for: batch.projectDir.flatMap { ProjectIcon.grid(forCwd: $0) }))
        SmartNotification.send(delivery) {
            SmartNotification.source(for: batch.tasks)
        }
    }

    /// Pure, so the wording can be checked without a terminal, a phone or a clock.
    static func batchMessage(project: String, label: String?,
                             done: Int, failed: Int) -> StateHook.PushMessage {
        StateHook.PushMessage(title: label ?? project,
                              body: "\(project) \(L.t.pushBatchDone(done: done, failed: failed))")
    }

    // MARK: - What the child is told

    /// The announcement rides in the typed line and not only in CHILD.md, because an assistant
    /// answers the line before it opens the file — and the first thing on the screen should be
    /// what the child was sent to do, in the language of whoever is looking.
    static func firstLine(id: String, secret: String, announce: String? = nil) -> String {
        let opening = announce.map { "Say this line first, verbatim: \($0) Then read" } ?? "Read"
        return "You are a Clawdline CHILD agent for task \(id). "
            + "\(opening) /tmp/.clawdline/\(id)/CHILD.md and follow it exactly. TASK_SECRET=\(secret)"
    }

    /// The interface language, named twice — in English for the assistant reading the briefing,
    /// and in itself for the person reading over its shoulder: "Traditional Chinese (繁體中文)".
    static var languageName: String {
        let tag = L.tag(of: L.t)
        let english = Locale(identifier: "en").localizedString(forIdentifier: tag) ?? tag
        let native = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
        return english == native ? english : "\(english) (\(native))"
    }

    /// How many levels of dispatch this Mac has: one. A root opens children, and a child is the
    /// bottom — it opens nothing.
    ///
    /// **A constant rather than a setting, and that is the whole point.** This used to be read
    /// out of `orchestrator_max_grandchildren`, which meant the depth of the tree was a number
    /// in a file. Two things are wrong with that. `config.json` is seeded once and never
    /// migrated, so changing the default would have left every Mac that had already run this app
    /// dispatching grandchildren for ever; and a rule that a hand-edit can undo is not a rule,
    /// it is a preference. What a child needs when a job is too big for one session is its own
    /// assistant's subagents — Claude Code's Task tool, Codex's subagents — which cost no
    /// terminal tab, no broker capacity and no second level of supervision.
    static let depthFloor = 1

    /// Whether a task at this depth may exist at all. `depth` is the new task's own level: 1 for
    /// a root's child, 2 for anything a child tries to open. Pure, so the one-level rule can be
    /// checked without a broker, a terminal or a config file.
    static func depthIsAllowed(_ depth: Int) -> Bool { depth <= depthFloor }

    static func childBrief(for task: Task) -> String {
        let dir = "/tmp/.clawdline/\(task.id)"
        let workspaceRule: String
        let isolationSection: String
        if let worktree = task.worktree {
            workspaceRule = "- Work inside \(worktree.cwd). Commit repository changes there; put "
                + "non-repository artifacts in \(dir)/artifacts/."
            isolationSection = """

            ## Your isolated checkout

            This is a fresh checkout of commit `\(worktree.base)` on branch `\(worktree.branch)`.
            Uncommitted files from the base repository are deliberately absent. Files ignored by
            gitignore — dependencies, build caches, and local environment files — are absent too;
            install them only after checking that doing so will not consume most of your timeout.

            \(task.assistant == .codex
              ? """
                **Do not commit. Leave your work uncommitted in this checkout.** A linked
                worktree keeps its git metadata in the base repository's
                `.git/worktrees/<task-id>/`, which is outside what you can write, so `git add`
                fails with `Operation not permitted` on `index.lock` and the delivery is
                reported as a failure with the work done. That has cost this repository whole
                rounds. The root reads this checkout and commits for you; your delivery is the
                bytes, not a branch. You may use `git status`, `git diff`, `git log` and
                `git show` to read.
                """
              : """
                **Commit early and often.** Commit only on this branch: the branch is the
                delivery, and uncommitted changes can be lost when the checkout is cleaned. You
                may use `git add`, `git commit`, `git status`, `git diff`, `git log`, and
                `git show` here.
                """)
            Do not push, switch or check out another branch, rebase, merge, hard-reset, stash,
            use `--git-dir` or `git -C` to reach the base repository, run any `git worktree`
            command, or run `./build.sh`. The app records commits, HEAD and dirty state from git;
            these rules are briefing rules rather than a shell sandbox.

            This checkout's `.build/` is reclaimed on the same schedule as `work/` once the task
            ends. The source and the delivery branch are never touched by that, but nothing you
            want to keep should be left inside a build directory.
            """
        } else {
            workspaceRule = "- Work inside \(task.projectDir). Put every file you produce in "
                + "\(dir)/artifacts/\n  (create the directory if it is missing)."
            isolationSection = ""
        }
        // Where this one stands, said plainly and once. Written into the briefing rather than
        // left to be discovered, because a child that finds out by being refused has already
        // spent a turn on it — and one that assumes it may dispatch spends several. The second
        // sentence is the part that changes behaviour rather than only forbidding it: the work
        // that used to be handed to a grandchild is work an assistant's own subagents do,
        // without a terminal tab, a briefing or a level of supervision under this one.
        let handOnRule = "**You are the bottom of this tree: you cannot dispatch Clawdline tasks "
            + "of your own, and a request to open one is refused.** When part of this needs to "
            + "run in parallel or wants a context of its own, use your own assistant's built-in "
            + "subagents (Claude Code's Task tool, Codex's subagents). They cost no terminal tab "
            + "and no broker capacity, and their answers come back to you rather than to a file."
        // What this Mac has said about itself, for every child rather than for a dispatcher.
        // See `policySection()`; it is empty when nobody has written anything.
        let houseRules = policySection()
        let verificationMinutes = task.timeoutMinutes % 3 == 0
            ? String(task.timeoutMinutes / 3)
            : String(format: "%.1f", Double(task.timeoutMinutes) / 3.0)
        let attachedSection = task.attachSessionId == nil ? "" : """

        ## Your standing session

        This task was attached to a standing session instead of opening a new tab. Finishing,
        failing or cancelling this task does not end this session; after `result.json` is written,
        leave the tab ready for the next complete follow-up task.

        Clawdline recorded that this process was launched with access to the whole
        `/tmp/.clawdline` task root; sessions given only their original task directory are
        refused before a follow-up is typed. This follow-up did not open the tab, however: if any
        permission, plan or confirmation menu appears, leave it for the session's owner.
        Clawdline does not choose from a menu on a session this task did not open. If the briefing
        is still unaccepted when this task's timeout expires, the task ends as `timeout` and
        releases the standing session and its claims.

        """
        // A Codex child's sandbox has no network at all — measured by task be9a54c0, not
        // inferred: CODEX_SANDBOX_NETWORK_DISABLED=1 is set, a curl to 127.0.0.1 exits 7 after
        // 0 ms, DNS itself is off, and no approval prompt ever appears. 133 codex children
        // were briefed to curl a progress note and 0 notes arrived; result.json always worked
        // because it is a file. So every loopback recipe below is per-assistant: HTTP stays
        // the fast path for a child that can reach it, and a child that cannot is told what
        // actually works instead of being left to discover the dead network by trying.
        let sandboxed = task.assistant == .codex
        let progressFile = """
        ```json
        {"task_secret": "<the TASK_SECRET value from your first message>",
         "note": "<one sentence, at most \(progressLimit) characters>"}
        ```
        """
        let timelySection = sandboxed
            ? """
              ## Notifications cannot leave your sandbox

              Other briefings carry a push-notification recipe here; it is a loopback HTTP call
              your sandbox cannot make, so it is not in yours. Anything the user needs to know
              mid-flight goes in `progress.json` below, and the answer itself in `result.json`
              — both are collected and read, so nothing timely is lost by not pushing.
              """
            : """
              ## Up to 5 timely notifications, when the user is waiting

              You may use your own TASK_SECRET to push one sentence the user needs to know now,
              before completion or for 60 seconds afterwards:

              ```bash
              curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/notify \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"title":"<at most 80 characters>","body":"<at most 500 characters>"}'
              ```

              The value of push is rarity. Routine results belong in `result.json`; notify only
              when the user is waiting for the answer, including a scheduled task such as
              today's weather whose useful output is the notification itself. Empty title/body
              values are refused. Each task may send at most 5 notifications, and this Mac
              accepts at most 30 per hour. The user may turn agent notifications off. A `409
              agent_notify_disabled` response is not your fault: leave the content in
              `result.json`, report failure honestly, and do not retry.
              """
        let progressChannel = sandboxed
            ? """
              **Your sandbox has no network, so say it with a file.** A `curl` to 127.0.0.1
              from here exits 7 after 0 ms — DNS is off too, and no approval prompt will
              appear — so do not spend a turn discovering that. Write \(dir)/progress.json
              with your file-writing tool, replacing the whole file each time:

              \(progressFile)

              The broker collects it within seconds, the way it collects `result.json`; a
              half-written file simply fails to parse and is read again. Only the latest
              sentence in the file is collected — overwrite, do not append.
              """
            : """
              ```bash
              curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/progress \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"note":"<one sentence, at most \(progressLimit) characters>"}'
              ```

              If that `curl` cannot connect — some sandboxes have no loopback — do not keep
              retrying it: write \(dir)/progress.json with your file-writing tool instead,
              replacing the whole file each time,

              \(progressFile)

              and the broker collects it the way it collects `result.json`.
              """
        let inflightSection = sandboxed
            ? """
              ## Before you start work you believe is new

              Another session's isolated checkout is invisible from the shared tree: a finished
              delivery sitting on a branch nobody has merged shows up in no `git status`, no
              `git diff` and no file listing. So "nothing here does that yet" is not evidence.
              Other briefings carry a live self-check against the broker's task list; your
              sandbox cannot reach it, so what you have instead is the plan above, when the
              dispatcher wrote one, and `task.json`. If part of your task looks like it may
              already be somebody else's work, write that suspicion into `progress.json` now
              and into your summary rather than silently building it twice — whoever reads
              your result can see the whole board and settle it.
              """
            : """
              ## Before you start work you believe is new, look

              Another session's isolated checkout is invisible from the shared tree: a finished
              delivery sitting on a branch nobody has merged shows up in no `git status`, no
              `git diff` and no file listing. So "nothing here does that yet" is not evidence.
              This is:

              ```bash
              curl -s http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/inflight \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>"
              ```

              Every line of work outstanding in this repository: what it is, who has it, what
              state it is in, what files it claimed, and for isolated ones the branch and head
              where its code actually lives. Read it before you build something you think
              nobody has built. If a row looks like your job, say so in your result rather
              than doing it twice.
              """
        let announceSection = sandboxed
            ? """
              There is no completion announcement to attempt from your sandbox: the file alone
              is the completion signal, and it has always been enough.
              """
            : """
              Optionally, if outbound network is permitted in your sandbox, you may ALSO announce it:
              `curl -s -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/complete \\
                 -H "X-Clawdline-Task-Secret: <TASK_SECRET>" -H 'Content-Type: application/json' \\
                 -d '{"status":"success","summary":"..."}'`
              This is never required; the file alone is enough.
              """
        return """
        # Clawdline child briefing — task \(task.id)

        You are a CHILD session working for a Clawdline root session. Your one job is the task
        described in \(dir)/task.json — read that file now.
        \(planSection(for: task))\(attachedSection)
        ## Language, and the first thing you say

        The person watching this terminal reads \(languageName). Everything you say in this
        session, and the "summary" you write into result.json, is in that language — this
        briefing is in English only so that every assistant reads it the same way.

        Before you read task.json or touch anything, say exactly this line, on its own:

        \(L.t.childAnnounce(task.title))

        Then, once you have read task.json, one more line in the same language saying in your
        own words what you are about to do and where the output will go.

        ## Rules

        \(workspaceRule)
        - Put heavyweight temporary work (repo copies, build outputs, mutation worktrees and
          compiler indexes) in \(dir)/work/, not in the assistant scratchpad. Everything there
          is deleted when the task ends — immediately on success, after the configured grace
          period otherwise — so copy any log or diff worth keeping into `artifacts/` **before**
          writing `result.json`.
        - \(handOnRule)
        - Do not read any directory under /tmp/.clawdline/ except your own and any your
          instructions name explicitly. That second one is how a reviewing node works: it is sent
          to read what other nodes produced, so its instructions list those paths.
        - Landing records belong to the root after delivery; by protocol convention, a child does
          not call its task's `/landing` route itself even though it holds that task's secret.
        - Do not do work the task did not ask for.
        - You have \(task.timeoutMinutes) minutes before the task is marked timed out.\(isolationSection)\(houseRules)

        ## Verification budget

        `./build.sh` is forbidden. Do not use an app restart or clicking the real UI as acceptance,
        re-run a full suite as a ritual after every small edit, run a suite unrelated to the paths this task claimed,
        or repeat a run whose only purpose is to see whether something is flaky.

        Do one verification that actually proves the change: compile, run the tests covering the
        paths this task touched, and see one red-before-green run for every test you add. Iterating
        until the change first compiles and passes is ordinary work. Verification stops after one
        third of this task's timeout (\(verificationMinutes) minutes) or three full-suite runs,
        whichever comes first. If either limit arrives, stop and report the state reached in
        `result.json`. Point verification's private `TMPDIR` at `\(dir)/work/tmp`; the repository's
        snapshot recipe remains unchanged, and its test binary is then reclaimed with the task.

        \(timelySection)

        ## Say what you are doing — once at the start, and again when it changes

        **Send the first one within about three minutes of starting**, before you begin the work
        rather than during it: one sentence saying what you have decided to do now that you have
        read this file and `task.json`. It is the only thing that lets a wrong direction be
        cancelled at minute three instead of minute twenty-six — the two most expensive cancelled
        tasks on this Mac burned 18.5M and 16.5M tokens before anybody could tell what they had
        set off to do. Nobody can read your screen; this note is the whole of what they have.

        After that, your title was fixed before you started. When what you are actually doing
        stops matching it — you decide to rewrite the fixture too, the real problem turns out to
        be somewhere else, you have moved on to the second half — say so the same way:

        \(progressChannel)

        **This is not a status report and nobody is waiting to read it.** It is one sentence, it
        costs you a second, and it is what another session sees when it asks whether the thing it
        is about to start is already being done. The newest \(progressKept) are kept; sending the
        same sentence twice is ignored rather than refused.

        \(inflightSection)

        ## Reporting — this is the completion signal, do it exactly

        When the work is done (or has failed for good), write \(dir)/result.json:

        ```json
        {"clawdline_protocol": 1,
         "task_id": "\(task.id)",
         "task_secret": "<the TASK_SECRET value from your first message>",
         "status": "success",
         "summary": "<one paragraph: what you did, or why it failed>",
         "symbols": ["<every name your change introduced>", "..."],
         "artifacts": ["artifacts/<file>", "..."],
         "verification": {"runs": 2, "seconds": 940, "last": "pass", "scope": "swift suite + web-schedules"},
         "finished_at": "<ISO8601 UTC>"}
        ```

        Use "status": "failure" when you could not do it. Write it LAST — the moment it exists
        your work is considered finished.

        **`symbols` is how your work is told apart from everybody else's.** This tree is shared:
        by the time root commits, the files you edited may hold two or three sessions' unfinished
        work, and root separates them by looking for vocabulary. Guessing that vocabulary is
        error-prone — root has staged trees that would not compile because a hunk *reading* like
        yours actually called somebody else's new function. So list what you introduced: new
        functions and types, new fields, new string keys, the names of test groups you added.
        Names, not descriptions. A wrong or missing list costs somebody an hour; it costs you a
        minute.

        **If you gave part of this to your own subagents and it did not come back, say so in the
        summary.** Doing it yourself instead is usually right — the answer is what was asked for,
        not who produced it. What is not right is a summary that reads as though those subagents
        did the work when they never finished. Whoever reads this is deciding how much to trust
        the result, and "both halves came back" and "both halves failed and I did it myself" are
        different amounts of evidence behind the same answer.

        **Write it with your file-writing tool, not with a shell command.** A shell line that
        builds JSON and moves it into place gets refused by command screening on its own shape —
        quotes inside braces, a redirect it cannot analyse statically — and that refusal is a
        prompt with no "always allow" on a tab nobody is watching. Atomicity is not yours to
        arrange: a half-written file simply fails to parse and is read again a few seconds later.

        \(announceSection)
        """
    }

    // MARK: - House rules

    /// Where the dispatch policy lives — beside the token and the registry, in the directory
    /// `CLAWDLINE_REMOTE_DIR` moves when it is set.
    ///
    /// A file rather than a string in `config.json`, because it is paragraphs: multi-line prose
    /// survives being hand-edited in a file and does not survive being hand-edited as a JSON
    /// string with `\n` in it.
    static var policyURL: URL { RemoteAuth.directory.appendingPathComponent("dispatch-policy.md") }

    /// Machine facts that must survive edits and syncs of the shipped policy. The app only reads
    /// this optional sibling: it never seeds, writes, or overwrites it.
    static var localPolicyURL: URL {
        RemoteAuth.directory.appendingPathComponent("dispatch-policy.local.md")
    }

    /// Kept in the composed text so a skimming child can see where machine-local precedence starts.
    static let localPolicyHeading = "## Machine-local rules (last, so they win)"

    /// The maximum this app will carry into a briefing.
    ///
    /// It was 4096, which was set against "house rules" meaning a paragraph about which assistant
    /// to use. Once the file also had to carry the decision of *whether to dispatch at all* and a
    /// library of graph shapes, that number cut the last rule off mid-word. Twelve thousand is
    /// room for a policy somebody has actually thought about, and still small enough that a file
    /// with a novel pasted into it cannot push the task itself off the bottom of a child's
    /// attention — which is what the limit is for.
    static let policyLimit = 12_000

    /// The same ceiling for the graph a task carries. Both end up in one briefing beside 16 KiB
    /// of instructions, and a briefing a child skims is worse than a shorter one it reads.
    static let planLimit = 4096

    /// What this Mac says about how work should be handed out, or nil when nobody has said
    /// anything. Read at dispatch rather than at launch, so an edit takes effect on the next
    /// task instead of on the next restart.
    static func policy() -> String? {
        policy(reading: try? String(contentsOf: policyURL, encoding: .utf8),
               local: try? String(contentsOf: localPolicyURL, encoding: .utf8))
    }

    /// The reading half, separated so a test can hand it text instead of a file.
    ///
    /// **Cutting is announced, and it happens at a paragraph.** The first version took a plain
    /// `prefix`, which put the knife wherever 4096 characters landed — and the first policy long
    /// enough to hit it lost its last rule mid-word, silently, with the briefing reading as
    /// though the file simply ended there. That is the failure this whole app keeps meeting: a
    /// thing that did not happen, wearing the shape of a thing that did. So the cut falls on the
    /// last paragraph break before the limit, and says out loud that it fell.
    static func policy(reading raw: String?) -> String? {
        guard let raw else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        guard text.count > policyLimit else { return text }
        let head = String(text.prefix(policyLimit))
        let body = head.range(of: "\n\n", options: .backwards)
            .map { String(head[..<$0.lowerBound]) } ?? head
        return body + "\n\n**[This policy was cut here.** It is longer than \(policyLimit) "
            + "characters, and everything after this point was not included — so if a rule you "
            + "expected is missing, that is why. Shorten the file.**]**"
    }

    /// Compose the shipped/base rules with this machine's optional facts. The local rules are
    /// deliberately last, and spend the budget before the base: silently dropping a machine fact
    /// would recreate the failure the separate file exists to prevent.
    static func policy(reading base: String?, local: String?) -> String? {
        guard let local else { return policy(reading: base) }
        let localText = local.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !localText.isEmpty else { return policy(reading: base) }

        // Let the established single-file cutter handle the degenerate case where the local file
        // alone is too large. Its notice remains part of the local section and therefore stays last.
        // `policy(reading:)` answers nil only for a nil or blank argument, and both are guarded
        // above — but the guarantee would live entirely in those two lines, so the fallback costs
        // nothing and removes a `!` that a later edit could make reachable.
        let localBody = policy(reading: localText) ?? localText
        let localSection = localPolicyHeading + "\n\n" + localBody

        guard let base else { return localSection }
        let baseText = base.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !baseText.isEmpty else { return localSection }

        let whole = baseText + "\n\n" + localSection
        guard whole.count > policyLimit else { return whole }

        // The notice is intentionally outside the content budget, as it is in the single-file
        // cutter above. It must say that a cut happened even when no whole base paragraph fits.
        let baseAllowance = max(0, policyLimit - localSection.count - 2)
        let head = String(baseText.prefix(baseAllowance))
        // `?? head` and not `?? ""`, for the reason the single-file cutter above has it: a base
        // with no blank line inside the allowance — a policy written as one long bulleted list, or
        // a file saved with CRLF, where Swift reads `\r\n` as one Character and this search never
        // matches — has no paragraph boundary to cut at, and the honest answer there is a rule
        // broken mid-word rather than no rules at all. Measured with `?? ""`: a 16,389-character
        // bulleted base went from 12,193 characters of rules in every briefing to 807, the whole
        // base gone, with a notice saying only that "the rest" was missing.
        let baseBody = head.range(of: "\n\n", options: .backwards)
            .map { String(head[..<$0.lowerBound]) } ?? head
        let notice = "**[This policy was cut here.** The base and machine-local rules are longer "
            + "than \(policyLimit) characters together, and part of the base was not included "
            + "— so if a base rule you expected is missing, that is why. Shorten the base file, or "
            + "the machine-local one.**]**"
        return [baseBody, notice, localSection].filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    /// Write the starting policy, once, if there is no file yet — and answer where it is either
    /// way. Never overwrites: what is in there is somebody's, and a default that came back after
    /// being deleted would be a setting that does not stay set.
    @discardableResult
    static func ensurePolicyFile() -> URL {
        let url = policyURL
        guard !FileManager.default.fileExists(atPath: url.path) else { return url }
        // Nothing to write is not the same as writing nothing. This function never overwrites, so
        // an empty file created because the bundled resource could not be read would be permanent:
        // the machine would keep the empty rules for good, and a later launch that can read the
        // resource would leave them alone. Leaving no file at all behaves identically today and
        // stays fixable tomorrow.
        let starting = defaultPolicy
        guard !starting.isEmpty else { return url }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(starting.utf8).write(to: url, options: .atomic)
        return url
    }

    /// Where the starting policy ships: `Resources/dispatch-policy.md`, which `build.sh` copies
    /// into the app bundle beside the hook script and the mascots.
    ///
    /// The override is for the test binary, which is a bare `swiftc` executable with no bundle to
    /// ask — and it is also how "the resource is missing" is tested, by pointing it at a path
    /// that is not there.
    static var bundledPolicyURLOverrideForTesting: URL?
    static var bundledPolicyURL: URL? {
        bundledPolicyURLOverrideForTesting
            ?? Bundle.main.url(forResource: "dispatch-policy", withExtension: "md")
    }

    /// The rules a machine with no policy file of its own starts with.
    ///
    /// **This was a string literal, and the literal went stale.** The live rules are
    /// `Resources/dispatch-policy.md` — the file this repository edits, and the one `build.sh`
    /// already copies into the bundle — while the copy compiled into the app kept an older draft
    /// of them. That copy is not a curiosity: `ensurePolicyFile` writes it, so it is exactly what
    /// a fresh install receives, and a machine could start life with rules nobody had read for
    /// months.
    ///
    /// A missing resource means no house rules at all, which is what an empty policy file has
    /// always meant. Nothing is invented to fill the gap.
    static var defaultPolicy: String {
        guard let url = bundledPolicyURL,
              let text = try? String(contentsOf: url, encoding: .utf8) else { return "" }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The graph this task is one node of, when the dispatcher wrote one down.
    ///
    /// Near the top, above even the language rule, because it is the context every other line is
    /// read in: a child that knows its answer is one of four being joined together writes
    /// something joinable, and one that does not writes a report.
    private static func planSection(for task: Task) -> String {
        guard let plan = task.plan, !plan.isEmpty else { return "" }
        return """


        ## The plan this is part of

        Written by the session that dispatched you. You are one node of it — find yourself in it
        before you start, and hand back what the node after you needs rather than everything you
        found.

        \(plan)

        """
    }

    /// What this Mac has said about itself, for every child briefing.
    ///
    /// **This travelled with the dispatch recipe once, and that was the wrong home for it.**
    /// The file was reasoned about as rules for *handing work out*, so when the tree lost its
    /// second level the section read as dead weight and was deleted with the recipe. It is not
    /// dead weight: the same file is where a person writes down what is true of this machine,
    /// and one of those sentences — that a Codex child's sandbox has no network — is measurably
    /// what stops a Codex child spending a turn on a `curl` that cannot connect. A leaf reads it
    /// and behaves differently, which is the whole test of whether a paragraph belongs in a
    /// briefing. So it goes to every child, dispatcher or not, and there is no longer any such
    /// thing as the second kind.
    ///
    /// Read from disk at briefing time, so an edit reaches the next child rather than the next
    /// launch. Empty when nobody has written anything, rather than a heading with nothing under
    /// it.
    private static func policySection() -> String {
        guard let policy = policy() else { return "" }
        return """


        ## What this Mac says

        House rules and machine facts, from \(policyURL.path) and its optional local sibling at
        \(localPolicyURL.path). They are the person's, not this app's; where they and your own
        judgement disagree, follow them and say so in your summary.

        \(policy)
        """
    }

    private static func writeChildBrief(for task: Task) {
        let url = task.dir.appendingPathComponent("CHILD.md")
        try? Data(childBrief(for: task).utf8).write(to: url, options: .atomic)
        // No `DISPATCHING.md` beside it any more: nothing this app opens may dispatch, so there
        // is no recipe to write. Removed rather than merely not written, because a task briefed
        // by an older build and re-briefed by this one would otherwise leave a child reading a
        // recipe it is no longer allowed to follow.
        try? FileManager.default.removeItem(at: task.dir.appendingPathComponent("DISPATCHING.md"))
    }

    // MARK: - Usage and cost

    /// Per million tokens, US dollars. Codex bills against a plan rather than per token, so an
    /// unknown model honestly costs "we cannot say" instead of a made-up number.
    static func price(forModel model: String?) -> (input: Double, output: Double)? {
        guard let model else { return nil }
        if model.hasPrefix("claude-fable-5") || model.hasPrefix("claude-mythos-5") { return (10, 50) }
        if model.hasPrefix("claude-opus") { return (5, 25) }
        if model.hasPrefix("claude-sonnet") { return (3, 15) }
        if model.hasPrefix("claude-haiku-4-5") { return (1, 5) }
        return nil
    }

    static func cost(of usage: Usage) -> Double? {
        guard let price = price(forModel: usage.model) else { return nil }
        let dollars = Double(usage.input) * price.input
            + Double(usage.output) * price.output
            + Double(usage.cacheRead) * price.input * 0.1
            + Double(usage.cacheWrite) * price.input * 1.25
        return (dollars / 1_000_000 * 10_000).rounded() / 10_000
    }

    static func harvestUsage(_ task: Task) -> Usage? {
        // Usage is accounting, so an absent proven path is an absent answer. Reconstructing a
        // Claude filename from an unverified stored session id charged a sibling's transcript to
        // the wrong task; Codex's time-only lookup has the same identity weakness after a restart.
        guard let path = task.transcriptPath.map(URL.init(fileURLWithPath:)) else { return nil }
        if !task.transcriptProven,
           transcriptOwnership(path, assistant: task.assistant, taskID: task.id) != .belongs {
            return nil
        }
        switch task.assistant {
        case .claude: return claudeUsage(transcript: path)
        case .codex:  return codexUsage(rollout: path)
        }
    }

    /// Sum of every assistant turn's `message.usage` in a Claude transcript.
    static func claudeUsage(transcript: URL) -> Usage? {
        guard let data = try? Data(contentsOf: transcript), !data.isEmpty else { return nil }
        var usage = Usage()
        var found = false
        for line in data.split(separator: 0x0A) {
            guard let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  obj["type"] as? String == "assistant",
                  let message = obj["message"] as? [String: Any],
                  let counts = message["usage"] as? [String: Any] else { continue }
            found = true
            usage.input += counts["input_tokens"] as? Int ?? 0
            usage.output += counts["output_tokens"] as? Int ?? 0
            usage.cacheRead += counts["cache_read_input_tokens"] as? Int ?? 0
            usage.cacheWrite += counts["cache_creation_input_tokens"] as? Int ?? 0
            if let model = message["model"] as? String { usage.model = model }
        }
        guard found else { return nil }
        usage.total = usage.input + usage.output + usage.cacheRead + usage.cacheWrite
        usage.costUsd = cost(of: usage)
        return usage
    }

    /// The last cumulative `token_count` event in a Codex rollout — Codex keeps the running
    /// total itself, so the newest event is the whole answer.
    static func codexUsage(rollout: URL) -> Usage? {
        guard let data = try? Data(contentsOf: rollout), !data.isEmpty else { return nil }
        var usage: Usage?
        var model: String?
        for line in data.split(separator: 0x0A) {
            // A byte scan before a parse: rollouts run to tens of thousands of lines and only a
            // few say either of the words this reader is looking for.
            guard let text = String(data: Data(line), encoding: .utf8) else { continue }
            if model == nil || text.contains("turn_context") {
                if text.contains("\"model\""),
                   let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any] {
                    let payload = obj["payload"] as? [String: Any] ?? obj
                    if let named = payload["model"] as? String { model = named }
                }
            }
            guard text.contains("token_count"),
                  let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  obj["type"] as? String == "event_msg",
                  let payload = obj["payload"] as? [String: Any],
                  payload["type"] as? String == "token_count",
                  let info = payload["info"] as? [String: Any],
                  let totals = info["total_token_usage"] as? [String: Any] else { continue }
            var made = Usage()
            made.input = totals["input_tokens"] as? Int ?? 0
            made.output = totals["output_tokens"] as? Int ?? 0
            made.cacheRead = totals["cached_input_tokens"] as? Int ?? 0
            made.cacheWrite = totals["cache_write_input_tokens"] as? Int ?? 0
            made.total = totals["total_tokens"] as? Int ?? (made.input + made.output)
            usage = made
        }
        guard var made = usage else { return nil }
        made.model = model
        made.costUsd = cost(of: made)
        return made
    }

    // MARK: - What the API answers with

    private static let mainQueueKey: DispatchSpecificKey<Bool> = {
        let key = DispatchSpecificKey<Bool>()
        DispatchQueue.main.setSpecific(key: key, value: true)
        return key
    }()

    static var isOnMainQueue: Bool {
        DispatchQueue.getSpecific(key: mainQueueKey) == true
    }

    /// Every task, newest first, in the wire shape. Hops to the main queue for the live
    /// resolutions (which terminal is the root, right now) the way `session(withID:)` does.
    static func records() -> [[String: Any]] {
        if !isOnMainQueue { return DispatchQueue.main.sync { records() } }
        lock.lock()
        let all = tasks.values.sorted { $0.created > $1.created }
        lock.unlock()
        return all.map { record(of: $0) }
    }

    static func record(id: String) -> [String: Any]? {
        if !isOnMainQueue { return DispatchQueue.main.sync { record(id: id) } }
        guard let task = held(id) else { return nil }
        return record(of: task)
    }

    /// The durable handoff envelope, in its registry spelling. There is intentionally no public
    /// GET route; this seam exists for round-trip and cleanup tests.
    static func handoffRecord(id: String) -> [String: Any]? {
        guard let envelope = heldHandoff(id) else { return nil }
        return stored(envelope)
    }

    static func saveForTesting() { _ = save() }

    /// The stored fields only — safe off the main thread, used where a route already holds the
    /// answer and only needs its shape.
    private static func existingRecord(_ id: String) -> [String: Any]? {
        guard let task = held(id) else { return nil }
        return shape(task, rootTerminal: nil)
    }

    /// Test seam for the GET representation, including optional fields whose absence is semantic.
    static func recordForTesting(_ task: Task) -> [String: Any] {
        shape(task, rootTerminal: nil)
    }

    /// Test seam: the linger a held record is carrying. Not in the GET representation, which
    /// describes the work rather than the housekeeping waiting on it.
    static func closeAtForTesting(_ id: String) -> Date? { held(id)?.closeAt }

    static func workCleanupAtForTesting(_ id: String) -> Date? { held(id)?.workCleanupAt }

    static func buildCleanupAtForTesting(_ id: String) -> Date? { held(id)?.buildCleanupAt }

    /// Test seams for the scheduler's in-memory arbitration. Production reaches the same state
    /// only through ordinary dispatch registration on the remote serial queue.
    static func holdScheduleTaskForTesting(_ task: Task) {
        lock.lock(); tasks[task.id] = task; loaded = true; lock.unlock()
    }

    static func mutateTaskForTesting(_ id: String, _ mutate: (inout Task) -> Void) {
        lock.lock()
        if var task = tasks[id] {
            mutate(&task)
            tasks[id] = task
        }
        lock.unlock()
    }

    static func handledScheduleFireForTesting(_ id: String) -> Date? {
        lock.lock(); defer { lock.unlock() }
        return handledScheduleFires[id]
    }

    /// The terminal a task belongs under. Supplying the process-bound identity reader keeps the
    /// selection pure enough to exercise without live terminals; production supplies
    /// ``Transcript/sessionID(of:)``.
    static func rootTerminalID(for task: Task, parentTerminalID: String?,
                               among targets: [TargetSession],
                               sessionID: (TargetSession) -> String?) -> String? {
        // The parent task first, when there is one. A dispatcher one level down is sitting in a
        // tab this app opened, so its terminal id is written in that task's record — whereas the
        // transcript lookup below need not even run. This preserves the direct depth-2 mapping.
        if let parentTerminalID { return parentTerminalID }
        guard let rootID = task.rootSessionId else { return nil }
        // Registry rows written before root.assistant existed can only have come from the
        // Claude-only protocol. Defaulting those rows is compatibility, not a guess for new JSON.
        let assistant = task.rootAssistant ?? .claude
        return target(forRootSession: rootID, assistant: assistant,
                      resolution: .task, among: targets, sessionID: sessionID)?.id
    }

    private static func record(of task: Task) -> [String: Any] {
        let parentTerminal = task.parentTaskId.flatMap { held($0)?.childTerminalId }
        let rootTerminal = rootTerminalID(for: task, parentTerminalID: parentTerminal,
                                          among: SessionWatch.shared.targets,
                                          sessionID: Transcript.sessionID(of:))
        return shape(task, rootTerminal: rootTerminal)
    }

    private static func shape(_ task: Task, rootTerminal: String?) -> [String: Any] {
        var out: [String: Any] = [
            "id": task.id,
            "state": task.state.rawValue,
            "kind": task.kind,
            "title": task.title,
            "assistant": task.assistant.rawValue,
            "projectDir": task.projectDir,
            "created": Int(task.created.timeIntervalSince1970),
            "depth": task.depth,
            "dir": "/tmp/.clawdline/\(task.id)",
        ]
        if let model = task.model { out["model"] = model }
        if let effort = task.reasoningEffort { out["reasoning_effort"] = effort.rawValue }
        if let scheduleID = task.scheduleID { out["schedule_id"] = scheduleID }
        out["permission"] = task.permission.rawValue
        if task.state == .queued, !task.serialize.isEmpty {
            lock.lock()
            let waiting = serializeBlockersLocked(for: task).map(\.id)
            lock.unlock()
            if !waiting.isEmpty { out["waiting_on"] = waiting }
        }
        if let at = task.spawnedAt { out["spawnedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.briefedAt { out["briefedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.finishedAt { out["finishedAt"] = Int(at.timeIntervalSince1970) }
        if let at = task.resultVerifiedAt {
            out["resultVerifiedAt"] = Int(at.timeIntervalSince1970)
        }
        var root: [String: Any] = [:]
        if let id = task.rootSessionId { root["sessionId"] = id }
        if let assistant = task.rootAssistant { root["assistant"] = assistant.rawValue }
        if let label = task.rootLabel { root["label"] = label }
        if let terminal = rootTerminal { root["terminalId"] = terminal }
        if let parent = task.parentTaskId { root["taskId"] = parent }
        if !root.isEmpty { out["root"] = root }
        if let from = task.respawnOf {
            out["respawn_of"] = from
            out["respawn_generation"] = task.respawnGeneration
        }
        var child: [String: Any] = [:]
        if let id = task.childTerminalId { child["terminalId"] = id }
        if let backend = task.childBackend { child["backend"] = backend.rawValue }
        if let id = task.childSessionId { child["sessionId"] = id }
        if !child.isEmpty { out["child"] = child }
        if let intervention = task.terminalIntervention {
            let modal = intervention.kind == .iTermModal
            var payload: [String: Any] = [
                "code": modal ? "iterm_attention_required" : "terminal_intervention_required",
                "action": modal ? "answer_dialog" : "inspect_terminal",
                "message": intervention.message,
            ]
            if modal { payload["app"] = "iTerm2" }
            if let backend = task.childBackend { payload["backend"] = backend.rawValue }
            out["terminal_intervention"] = payload
        }
        if let summary = task.summary { out["summary"] = summary }
        if let delivery = task.completionDelivery {
            out["completion_delivery"] = completionRecord(delivery)
        }
        if let session = task.attachSessionId {
            out["attached"] = true
            out["attachSession"] = session
        }
        if !task.artifacts.isEmpty { out["artifacts"] = task.artifacts }
        if task.claimsDeclared { out["claims"] = task.claims }
        if !task.releasedClaims.isEmpty {
            out["released_claims"] = task.releasedClaims.map {
                ["path": $0.path, "released_at": Int($0.releasedAt.timeIntervalSince1970)]
                    as [String: Any]
            }
        }
        if !task.untouchedClaims.isEmpty { out["untouched_claims"] = task.untouchedClaims }
        if let landing = task.landing { out["landing"] = landingRecord(landing) }
        if !task.progress.isEmpty { out["progress"] = task.progress.map(progressRecord) }
        if let worktree = task.worktree {
            out["isolation"] = Isolation.worktree.rawValue
            // Before a tab exists the preparation has only a candidate base. `spawn` resolves it
            // again after acquiring any mutex, so publishing it would turn a preview into a receipt.
            if task.spawnedAt != nil {
                out["worktree"] = [
                    "path": worktree.path,
                    "branch": worktree.branch,
                    "base": worktree.base,
                    "head": worktree.head as Any? ?? NSNull(),
                    "commits": worktree.commits as Any? ?? NSNull(),
                    "dirty": worktree.dirty as Any? ?? NSNull(),
                ]
            }
        }
        if let usage = task.usage {
            var counts: [String: Any] = [
                "input": usage.input, "output": usage.output,
                "cacheRead": usage.cacheRead, "cacheWrite": usage.cacheWrite,
                "total": usage.total,
            ]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["costUsd"] = cost }
            out["usage"] = counts
        }
        if let verification = task.verification {
            out["verification"] = verificationRecord(verification)
        }
        return out
    }

    // MARK: - Cross-session coordination waits

    static func fileWaitRequestNotice(waitID: String, repository: String, paths: [String],
                                      waiterSessionID: String, reason: String,
                                      releaseCondition: String) -> ClawdlineMessage.Notice {
        let pathList = paths.joined(separator: ", ")
        let body = "[Clawdline file-wait] Repo: \(repository). Exact paths: \(pathList). "
            + "Waiter Clawdline session id: \(waiterSessionID). Reason: \(reason). "
            + "Release condition: \(releaseCondition). After the condition is met, release this "
            + "wait through Clawdline so every registered waiter is notified."
        return ClawdlineMessage.Notice(
            event: .fileWaitRequest(
                waitID: waitID, repository: repository, paths: paths,
                waiterSessionID: waiterSessionID, reason: reason,
                releaseCondition: releaseCondition),
            body: body)
    }

    static func fileWaitReleaseNotice(waitID: String, repository: String, paths: [String],
                                      commit: String?, note: String?)
        -> ClawdlineMessage.Notice {
        var body = "[Clawdline file-wait release] Repo: \(repository). "
            + "Exact paths: \(paths.joined(separator: ", ")). "
        if let commit { body += "Landed/released in commit \(commit). " }
        else { body += "The owner explicitly released these paths without a commit. " }
        if let note { body += "Note: \(note). " }
        body += "Re-check HEAD, status and diff before editing or integrating."
        return ClawdlineMessage.Notice(
            event: .fileWaitRelease(
                waitID: waitID, repository: repository, paths: paths,
                commit: commit, note: note),
            body: body)
    }

    /// Register a durable wait and deliver its request through Clawdline's own session transport.
    /// A failed delivery leaves the row pending, so retrying the same relationship can deliver it
    /// without losing the fact that the waiter is blocked.
    ///
    /// `readiness` answers whether words typed into that session would be read as words; a
    /// reason means they would not, and nothing is sent. It defaults to "ready" so that tests
    /// about the relationship itself stay about the relationship — the two routes that actually
    /// type into a terminal both pass the real check.
    static func registerCoordinationWait(
        _ raw: [String: Any], now: Date = Date(),
        readiness: (String) -> String? = { _ in nil },
        deliver: (String, String) -> String?
    ) -> Reply {
        guard let repositoryRaw = boundedCoordinationText(raw["repository"], limit: 4_096),
              let repository = canonicalCoordinationRepository(repositoryRaw),
              let rawPaths = raw["paths"] as? [String],
              let paths = canonicalCoordinationPaths(rawPaths, repository: repository),
              let owner = boundedCoordinationText(raw["owner_session_id"], limit: 512),
              let waiter = boundedCoordinationText(raw["waiter_session_id"], limit: 512),
              owner != waiter,
              let reason = boundedCoordinationText(raw["reason"], limit: 1_000),
              let condition = boundedCoordinationText(raw["release_condition"], limit: 1_000)
        else {
            return .refused(400, "bad_wait",
                            "repository, canonical paths, distinct owner/waiter session ids, "
                            + "reason and release_condition are required.")
        }

        load()
        coordinationDeliveryLock.lock(); defer { coordinationDeliveryLock.unlock() }
        var deduplicated = false
        var needsDelivery = true
        var waitID: String
        lock.lock()
        if var existing = coordinationWaits.values.first(where: {
            $0.repository == repository && $0.paths == paths
                && $0.ownerSessionID == owner && $0.releaseCondition == condition
        }) {
            waitID = existing.id
            if let index = existing.waiters.firstIndex(where: { $0.sessionID == waiter }) {
                deduplicated = true
                needsDelivery = existing.waiters[index].requestDeliveredAt == nil
            } else {
                existing.waiters.append(CoordinationWaiter(sessionID: waiter, reason: reason,
                                                            created: now,
                                                            requestDeliveredAt: nil,
                                                            releaseDeliveredAt: nil))
                coordinationWaits[existing.id] = existing
            }
        } else {
            waitID = UUID().uuidString.lowercased()
            coordinationWaits[waitID] = CoordinationWait(
                id: waitID, repository: repository, paths: paths,
                ownerSessionID: owner, releaseCondition: condition, created: now,
                waiters: [CoordinationWaiter(sessionID: waiter, reason: reason, created: now,
                                             requestDeliveredAt: nil,
                                             releaseDeliveredAt: nil)])
        }
        lock.unlock()
        save()

        if needsDelivery {
            // Refuse before typing rather than after: words sent into a permission picker are
            // discarded and the Return that follows answers the highlighted row. Sending anyway
            // would lose the request *and* answer a question on the owner's behalf — and the
            // durable row would carry a receipt saying the owner had been told.
            if let blocked = readiness(owner) {
                RemoteAuth.audit("orchestrator.wait.register", [
                    "wait": waitID, "owner": owner, "waiter": waiter, "result": "owner_busy",
                ])
                return .refused(status: 409, code: "owner_busy",
                                message: blocked + " Nothing was sent and the wait is still "
                                    + "recorded as undelivered; retry this registration.",
                                extra: ["wait": coordinationWaitRecord(id: waitID) ?? [:]])
            }
            let notice = fileWaitRequestNotice(
                waitID: waitID, repository: repository, paths: paths,
                waiterSessionID: waiter, reason: reason, releaseCondition: condition)
            let message = ClawdlineMessage.encode(notice)
            if let problem = deliver(owner, message) {
                RemoteAuth.audit("orchestrator.wait.register", [
                    "wait": waitID, "owner": owner, "waiter": waiter,
                    "result": "delivery_failed",
                ])
                return .refused(status: 502, code: "request_delivery_failed",
                                message: problem,
                                extra: ["wait": coordinationWaitRecord(id: waitID) ?? [:]])
            }
            lock.lock()
            if var current = coordinationWaits[waitID],
               let index = current.waiters.firstIndex(where: { $0.sessionID == waiter }) {
                current.waiters[index].requestDeliveredAt = now
                coordinationWaits[waitID] = current
            }
            lock.unlock()
            save()
        }
        RemoteAuth.audit("orchestrator.wait.register", [
            "wait": waitID, "owner": owner, "waiter": waiter,
            "result": deduplicated && !needsDelivery ? "deduplicated" : "delivered",
        ])
        return .ok(["ok": true, "deduplicated": deduplicated,
                    "wait": coordinationWaitRecord(id: waitID) ?? [:]])
    }

    /// Explicit owner release. Successful deliveries are receipted before returning; a retry
    /// only addresses the waiters that did not receive the first fan-out.
    static func releaseCoordinationWait(
        id: String, ownerSessionID: String, commit: String?, note: String?,
        now: Date = Date(), readiness: (String) -> String? = { _ in nil },
        deliver: (String, String) -> String?
    ) -> Reply {
        guard let owner = boundedCoordinationText(ownerSessionID, limit: 512),
              commit.map({ boundedCoordinationText($0, limit: 200) != nil }) ?? true,
              note.map({ boundedCoordinationText($0, limit: 1_000) != nil }) ?? true
        else { return .refused(400, "bad_wait", "The release fields are not valid.") }
        load()
        coordinationDeliveryLock.lock(); defer { coordinationDeliveryLock.unlock() }
        lock.lock()
        guard let snapshot = coordinationWaits[id] else {
            lock.unlock()
            return .refused(404, "not_found", "No coordination wait named that.")
        }
        guard snapshot.ownerSessionID == owner else {
            lock.unlock()
            return .refused(403, "wrong_owner",
                            "Only the owner recorded on this wait may release it.")
        }
        let pending = snapshot.waiters.filter { $0.releaseDeliveredAt == nil }
        lock.unlock()

        let commitText = commit.flatMap { boundedCoordinationText($0, limit: 200) }
        let noteText = note.flatMap { boundedCoordinationText($0, limit: 1_000) }
        var deliveredIDs: [String] = []
        for waiter in pending {
            // A waiter that cannot be typed into right now is simply not notified in this round.
            // From the owner's side that is the situation `release_incomplete` already describes,
            // so it needs no new code: no receipt is written and a retry reaches exactly the
            // waiters that are still owed one.
            guard readiness(waiter.sessionID) == nil else { continue }
            let notice = fileWaitReleaseNotice(
                waitID: id, repository: snapshot.repository, paths: snapshot.paths,
                commit: commitText, note: noteText)
            let message = ClawdlineMessage.encode(notice)
            if deliver(waiter.sessionID, message) == nil { deliveredIDs.append(waiter.sessionID) }
        }

        lock.lock()
        guard var current = coordinationWaits[id], current.ownerSessionID == owner else {
            lock.unlock()
            return .refused(409, "wait_changed", "The coordination wait changed during release.")
        }
        for index in current.waiters.indices
            where deliveredIDs.contains(current.waiters[index].sessionID) {
            current.waiters[index].releaseDeliveredAt = now
        }
        let total = current.waiters.count
        let stillPending = current.waiters.filter { $0.releaseDeliveredAt == nil }.count
        if stillPending == 0 { coordinationWaits.removeValue(forKey: id) }
        else { coordinationWaits[id] = current }
        lock.unlock()
        save()

        if stillPending > 0 {
            RemoteAuth.audit("orchestrator.wait.release", [
                "wait": id, "owner": owner, "result": "partial",
                "sent": "\(deliveredIDs.count)", "pending": "\(stillPending)",
            ])
            return .refused(status: 502, code: "release_incomplete",
                            message: "Some waiters could not be notified; retry this release.",
                            extra: ["id": id, "sent": deliveredIDs.count,
                                    "pending": stillPending])
        }
        RemoteAuth.audit("orchestrator.wait.release", [
            "wait": id, "owner": owner, "result": "delivered", "sent": "\(total)",
        ])
        return .ok(["ok": true, "id": id, "released": total])
    }

    /// A waiter that abandons its work can remove only itself. This never guesses that a clean
    /// worktree means release and never removes another session's obligation.
    static func cancelCoordinationWait(id: String, waiterSessionID: String) -> Reply {
        guard let waiter = boundedCoordinationText(waiterSessionID, limit: 512) else {
            return .refused(400, "bad_wait", "waiter_session_id is required.")
        }
        load()
        coordinationDeliveryLock.lock(); defer { coordinationDeliveryLock.unlock() }
        lock.lock()
        guard var current = coordinationWaits[id] else {
            lock.unlock()
            return .refused(404, "not_found", "No coordination wait named that.")
        }
        let before = current.waiters.count
        current.waiters.removeAll { $0.sessionID == waiter }
        guard current.waiters.count != before else {
            lock.unlock()
            return .refused(403, "not_waiter", "That session is not a waiter on this group.")
        }
        if current.waiters.isEmpty { coordinationWaits.removeValue(forKey: id) }
        else { coordinationWaits[id] = current }
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.wait.cancel", ["wait": id, "waiter": waiter])
        return .ok(["ok": true, "id": id])
    }

    static func coordinationWaitRecords() -> [[String: Any]] {
        load()
        lock.lock(); defer { lock.unlock() }
        return coordinationWaits.values.sorted {
            $0.created == $1.created ? $0.id < $1.id : $0.created < $1.created
        }
            .map(coordinationWaitRecord)
    }

    static func coordination(forTerminal id: String) -> Coordination {
        load()
        lock.lock(); defer { lock.unlock() }
        return coordinationLocked(forTerminal: id)
    }

    /// The caller owns `lock`; kept separate so Bearings can derive all ownership flags inside
    /// the same observation as task, landing and wait totals.
    private static func coordinationLocked(forTerminal id: String) -> Coordination {
        var waitingOn: [[String: Any]] = []
        var waitedOnBy: [[String: Any]] = []
        for wait in coordinationWaits.values {
            for waiter in wait.waiters where waiter.releaseDeliveredAt == nil {
                if waiter.sessionID == id {
                    var row = coordinationWaitRecord(wait)
                    row.removeValue(forKey: "waiters")
                    row["reason"] = waiter.reason
                    row["waiterCreatedAt"] = Int(waiter.created.timeIntervalSince1970)
                    waitingOn.append(row)
                }
                if wait.ownerSessionID == id {
                    var row = coordinationWaitRecord(wait)
                    row.removeValue(forKey: "waiters")
                    row["waiterSessionId"] = waiter.sessionID
                    row["reason"] = waiter.reason
                    waitedOnBy.append(row)
                }
            }
        }
        waitingOn.sort {
            let left = ($0["createdAt"] as? Int ?? 0, $0["id"] as? String ?? "")
            let right = ($1["createdAt"] as? Int ?? 0, $1["id"] as? String ?? "")
            return left < right
        }
        waitedOnBy.sort {
            let left = ($0["createdAt"] as? Int ?? 0,
                        $0["waiterSessionId"] as? String ?? "")
            let right = ($1["createdAt"] as? Int ?? 0,
                         $1["waiterSessionId"] as? String ?? "")
            return left < right
        }
        return Coordination(waitingOn: waitingOn, waitedOnBy: waitedOnBy)
    }

    private static func coordinationWaitRecord(id: String) -> [String: Any]? {
        lock.lock(); defer { lock.unlock() }
        return coordinationWaits[id].map(coordinationWaitRecord)
    }

    private static func coordinationWaitRecord(_ wait: CoordinationWait) -> [String: Any] {
        [
            "id": wait.id,
            "repository": wait.repository,
            "paths": wait.paths,
            "ownerSessionId": wait.ownerSessionID,
            "releaseCondition": wait.releaseCondition,
            "createdAt": Int(wait.created.timeIntervalSince1970),
            "waiters": wait.waiters.map { waiter -> [String: Any] in
                var row: [String: Any] = [
                    "sessionId": waiter.sessionID, "reason": waiter.reason,
                    "createdAt": Int(waiter.created.timeIntervalSince1970),
                ]
                if let at = waiter.requestDeliveredAt {
                    row["requestDeliveredAt"] = Int(at.timeIntervalSince1970)
                }
                if let at = waiter.releaseDeliveredAt {
                    row["releaseDeliveredAt"] = Int(at.timeIntervalSince1970)
                }
                return row
            },
        ]
    }

    private static func boundedCoordinationText(_ value: Any?, limit: Int) -> String? {
        guard let raw = value as? String else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= limit,
              !text.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return text
    }

    private static func canonicalCoordinationRepository(_ raw: String) -> String? {
        guard raw.hasPrefix("/"), raw != "/" else { return nil }
        let path = URL(fileURLWithPath: raw).standardizedFileURL.path
        guard path.hasPrefix("/"), path != "/" else { return nil }
        return path
    }

    private static func canonicalCoordinationPaths(_ raw: [String], repository: String)
        -> [String]? {
        guard !raw.isEmpty, raw.count <= 200 else { return nil }
        let root = URL(fileURLWithPath: repository, isDirectory: true)
        let prefix = repository + "/"
        var found = Set<String>()
        for path in raw {
            guard !path.isEmpty, path.count <= 1_024,
                  !path.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
            let absolute = path.hasPrefix("/")
                ? URL(fileURLWithPath: path).standardizedFileURL.path
                : URL(fileURLWithPath: path, relativeTo: root).standardizedFileURL.path
            guard absolute.hasPrefix(prefix) else { return nil }
            let relative = String(absolute.dropFirst(prefix.count))
            guard !relative.isEmpty else { return nil }
            found.insert(relative)
        }
        return found.sorted()
    }

    // MARK: - The store

    static func load(force: Bool = false) {
        lock.lock()
        if loaded, !force { lock.unlock(); return }
        loaded = true
        lock.unlock()
        guard let data = try? Data(contentsOf: storeURL),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return }
        var found: [String: Task] = [:]
        for row in obj["tasks"] as? [[String: Any]] ?? [] {
            guard let task = task(from: row) else { continue }
            found[task.id] = task
        }
        var foundHandoffs: [String: HandoffEnvelope] = [:]
        for row in obj["handoffs"] as? [[String: Any]] ?? [] {
            guard let envelope = handoff(from: row) else { continue }
            foundHandoffs[envelope.id] = envelope
        }
        var foundWaits: [String: CoordinationWait] = [:]
        for row in obj["coordination_waits"] as? [[String: Any]] ?? [] {
            guard let wait = coordinationWait(from: row) else { continue }
            foundWaits[wait.id] = wait
        }
        var foundSessionDeliveries: [String: SessionDelivery] = [:]
        for row in obj["session_deliveries"] as? [[String: Any]] ?? [] {
            guard let delivery = sessionDelivery(from: row) else { continue }
            foundSessionDeliveries[delivery.identity.terminalID] = delivery
        }
        var foundSelfStates: [String: SessionSelfState] = [:]
        for row in obj["session_self_states"] as? [[String: Any]] ?? [] {
            guard let selfState = sessionSelfState(from: row) else { continue }
            foundSelfStates[selfState.identity.terminalID] = selfState
        }
        lock.lock()
        tasks = found
        handoffs = foundHandoffs
        coordinationWaits = foundWaits
        sessionDeliveries = foundSessionDeliveries
        sessionSelfStates = foundSelfStates
        reindex()
        lock.unlock()
        // Proofs stored before the independent ledger existed are still strong proofs. Backfill
        // only those exact task/session pairs; never enumerate the surrounding scratch root.
        for task in found.values where task.transcriptProven && task.assistant == .claude {
            _ = registerOwnedScratchpad(for: task)
        }
    }

    /// Atomically replace the registry and report whether the exact snapshot reached disk. Most
    /// callers can remain best-effort; completion delivery is the exception and will not touch a
    /// terminal unless this returns true for the outcome-plus-outbox snapshot.
    @discardableResult
    private static func save() -> Bool {
        storeSaveLock.lock(); defer { storeSaveLock.unlock() }
        lock.lock()
        reindex()
        let rows = tasks.values.sorted { $0.created < $1.created }.map { stored($0) }
        let handoffRows = handoffs.values.sorted { $0.created < $1.created }
            .map { stored($0) }
        let waitRows = coordinationWaits.values.sorted { $0.created < $1.created }
            .map { stored($0) }
        let sessionDeliveryRows = sessionDeliveries.values
            .sorted { $0.reportedAt < $1.reportedAt }.map { stored($0) }
        let sessionSelfStateRows = sessionSelfStates.values
            .sorted { $0.identity.terminalID < $1.identity.terminalID }.map { stored($0) }
        lock.unlock()
        let obj: [String: Any] = ["version": 1, "tasks": rows, "handoffs": handoffRows,
                                  "coordination_waits": waitRows,
                                  "session_deliveries": sessionDeliveryRows,
                                  "session_self_states": sessionSelfStateRows]
        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys,
                                                               .withoutEscapingSlashes]) else {
            Log.write("orchestrator: could not serialise the store, nothing written")
            return false
        }
        if let intercepted = storeSaveInterceptorForTesting?(data) { return intercepted }
        do {
            try FileManager.default.createDirectory(at: RemoteAuth.directory,
                                                    withIntermediateDirectories: true)
            try data.write(to: storeURL, options: .atomic)
        } catch {
            Log.write("orchestrator: could not persist the store — \(error)")
            return false
        }
        try? FileManager.default.createDirectory(at: storeURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? data.write(to: storeURL, options: .atomic)
        // Every time, not only at creation — same reason as `RemoteAuth.save`.
        do {
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: storeURL.path)
            return true
        } catch {
            Log.write("orchestrator: could not protect the store — \(error)")
            return false
        }
    }

    private static func landingRecord(_ landing: Landing) -> [String: Any] {
        var out: [String: Any] = [
            "state": landing.state.rawValue,
            "owner_root_key": landing.ownerRootKey,
            "since": Int(landing.since.timeIntervalSince1970),
        ]
        if let target = landing.target { out["target"] = target }
        if let delivery = landing.delivery { out["delivery"] = delivery }
        if let commit = landing.commit { out["commit"] = commit }
        if let note = landing.note { out["note"] = note }
        if let landedAt = landing.landedAt {
            out["landed_at"] = Int(landedAt.timeIntervalSince1970)
        }
        if let origin = landing.verificationOrigin { out["verification_origin"] = origin }
        if let commit = landing.verifiedCommit { out["verified_commit"] = commit }
        if let targetCommit = landing.verifiedTargetCommit {
            out["verified_target_commit"] = targetCommit
        }
        return out
    }

    private static func stored(_ landing: Landing) -> [String: Any] {
        var out = landingRecord(landing)
        out["since"] = landing.since.timeIntervalSince1970
        if let landedAt = landing.landedAt {
            out["landed_at"] = landedAt.timeIntervalSince1970
        }
        return out
    }

    /// A progress note on the wire — whole seconds, like every other time in a record.
    private static func progressRecord(_ note: ProgressNote) -> [String: Any] {
        ["note": note.note, "at": Int(note.at.timeIntervalSince1970)]
    }

    private static func stored(_ note: ProgressNote) -> [String: Any] {
        ["note": note.note, "at": note.at.timeIntervalSince1970]
    }

    private static func verificationRecord(_ verification: Verification) -> [String: Any] {
        ["runs": verification.runs, "seconds": verification.seconds,
         "last": verification.last.rawValue, "scope": verification.scope]
    }

    /// Notes back off disk. A row that lost its text or its clock is dropped rather than
    /// resurrected with a guess, and the kept-count is applied again on the way in so an older
    /// store written before the cap cannot reintroduce an unbounded list.
    private static func progress(from raw: Any?) -> [ProgressNote] {
        guard let rows = raw as? [[String: Any]] else { return [] }
        let notes = rows.compactMap { row -> ProgressNote? in
            guard let text = row["note"] as? String,
                  !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  text.count <= progressLimit,
                  let at = row["at"] as? Double else { return nil }
            return ProgressNote(note: text, at: Date(timeIntervalSince1970: at))
        }
        return notes.count > progressKept ? Array(notes.suffix(progressKept)) : notes
    }

    static func stored(_ task: Task) -> [String: Any] {
        var out: [String: Any] = [
            "id": task.id,
            "state": task.state.rawValue,
            "kind": task.kind,
            "title": task.title,
            "assistant": task.assistant.rawValue,
            "project_dir": task.projectDir,
            "timeout_minutes": task.timeoutMinutes,
            "created": task.created.timeIntervalSince1970,
            "depth": task.depth,
            "secret_hash": task.secretHash,
            "notify_count": task.notifyCount,
            "artifacts": task.artifacts,
        ]
        if let at = task.spawnedAt { out["spawned_at"] = at.timeIntervalSince1970 }
        if let at = task.briefedAt { out["briefed_at"] = at.timeIntervalSince1970 }
        if let at = task.finishedAt { out["finished_at"] = at.timeIntervalSince1970 }
        if let at = task.resultVerifiedAt {
            out["result_verified_at"] = at.timeIntervalSince1970
        }
        if let v = task.rootSessionId { out["root_session"] = v }
        if let v = task.rootAssistant { out["root_assistant"] = v.rawValue }
        if let v = task.rootLabel { out["root_label"] = v }
        if let v = task.parentTaskId { out["parent_task"] = v }
        if let v = task.respawnOf {
            out["respawn_of"] = v
            out["respawn_generation"] = task.respawnGeneration
        }
        if let v = task.model { out["model"] = v }
        if let v = task.reasoningEffort { out["reasoning_effort"] = v.rawValue }
        out["permission"] = task.permission.rawValue
        if let v = task.plan { out["plan"] = v }
        if let v = task.scheduleID {
            out["schedule_id"] = v
            out["schedule_close_tab"] = task.scheduleCloseTab.rawValue
            out["schedule_notify_failure"] = task.scheduleNotifyFailure
        }
        if !task.serialize.isEmpty { out["serialize"] = task.serialize }
        if task.claimsDeclared { out["claims"] = task.claims }
        if !task.claimKeys.isEmpty { out["claim_keys"] = task.claimKeys }
        if !task.releasedClaims.isEmpty {
            out["released_claims"] = task.releasedClaims.map {
                ["path": $0.path, "released_at": $0.releasedAt.timeIntervalSince1970]
                    as [String: Any]
            }
        }
        if !task.untouchedClaims.isEmpty { out["untouched_claims"] = task.untouchedClaims }
        if let landing = task.landing { out["landing"] = stored(landing) }
        if !task.progress.isEmpty { out["progress"] = task.progress.map(stored) }
        if let v = task.progressFileNote { out["progress_file_note"] = v }
        if let worktree = task.worktree {
            out["isolation"] = Isolation.worktree.rawValue
            var storedWorktree: [String: Any] = [
                "path": worktree.path, "branch": worktree.branch, "base": worktree.base,
                "repository": worktree.repository, "cwd": worktree.cwd,
                "base_dirty": worktree.baseDirty, "requested_base": worktree.requestedBase,
            ]
            if let head = worktree.head { storedWorktree["head"] = head }
            if let commits = worktree.commits { storedWorktree["commits"] = commits }
            if let dirty = worktree.dirty { storedWorktree["dirty"] = dirty }
            out["worktree"] = storedWorktree
        }
        if let v = task.queuedSecret { out["queued_secret"] = v }
        if let v = task.attachSessionId { out["attach_session"] = v }
        if let v = task.childTerminalId { out["child_terminal"] = v }
        if let v = task.childBackend { out["child_backend"] = v.rawValue }
        if task.childTaskRootAccess { out["child_task_root_access"] = true }
        if let v = task.childTTY { out["child_tty"] = v }
        if let v = task.childPID { out["child_pid"] = Int(v) }
        if let v = task.childProcStart { out["child_proc_start"] = v.timeIntervalSince1970 }
        if let v = task.childSessionId { out["child_session"] = v }
        if let at = task.closeAt { out["close_at"] = at.timeIntervalSince1970 }
        if let v = task.terminalIntervention {
            out["terminal_intervention"] = ["kind": v.kind.rawValue, "message": v.message]
        }
        if let at = task.workCleanupAt { out["work_cleanup_at"] = at.timeIntervalSince1970 }
        if let at = task.buildCleanupAt { out["build_cleanup_at"] = at.timeIntervalSince1970 }
        if let v = task.transcriptPath { out["transcript"] = v }
        if task.transcriptProven { out["transcript_proven"] = true }
        if let v = task.summary { out["summary"] = v }
        if let delivery = task.completionDelivery {
            out["completion_delivery"] = stored(delivery)
        }
        if let verification = task.verification {
            out["verification"] = verificationRecord(verification)
        }
        if let usage = task.usage {
            var counts: [String: Any] = ["input": usage.input, "output": usage.output,
                                         "cache_read": usage.cacheRead,
                                         "cache_write": usage.cacheWrite, "total": usage.total]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["cost_usd"] = cost }
            out["usage"] = counts
        }
        return out
    }

    static func stored(_ delivery: CompletionDelivery) -> [String: Any] {
        var out: [String: Any] = [
            "notice_id": delivery.noticeID,
            "created_at": delivery.created.timeIntervalSince1970,
            "state": delivery.state.rawValue,
            "attempts": delivery.attempts,
            "legacy_reconciled": delivery.legacyReconciled,
        ]
        if let at = delivery.nextRetryAt { out["next_retry_at"] = at.timeIntervalSince1970 }
        if let at = delivery.lastAttemptAt { out["last_attempt_at"] = at.timeIntervalSince1970 }
        if let at = delivery.transportDeliveredAt {
            out["transport_delivered_at"] = at.timeIntervalSince1970
        }
        if let at = delivery.observedAt { out["observed_at"] = at.timeIntervalSince1970 }
        if let at = delivery.acknowledgedAt {
            out["acknowledged_at"] = at.timeIntervalSince1970
        }
        if let failure = delivery.lastError {
            out["last_error"] = [
                "code": failure.code.rawValue, "message": failure.message,
                "at": failure.at.timeIntervalSince1970,
            ] as [String: Any]
        }
        if let at = delivery.deadLetterAt { out["dead_letter_at"] = at.timeIntervalSince1970 }
        return out
    }

    static func completionDelivery(from obj: [String: Any]) -> CompletionDelivery? {
        func date(_ key: String) -> Date? {
            guard let value = obj[key] as? Double, value.isFinite, value > 0 else { return nil }
            return Date(timeIntervalSince1970: value)
        }
        guard let noticeID = obj["notice_id"] as? String,
              UUID(uuidString: noticeID) != nil,
              let created = date("created_at"),
              let rawState = obj["state"] as? String,
              let state = CompletionDeliveryState(rawValue: rawState),
              let attempts = obj["attempts"] as? Int, (0...1_000).contains(attempts)
        else { return nil }
        var failure: CompletionFailure?
        if let row = obj["last_error"] as? [String: Any] {
            guard let rawCode = row["code"] as? String,
                  let code = CompletionFailureCode(rawValue: rawCode),
                  let message = row["message"] as? String, !message.isEmpty,
                  message.count <= 1_000,
                  let rawAt = row["at"] as? Double, rawAt.isFinite, rawAt > 0 else { return nil }
            failure = CompletionFailure(code: code, message: message,
                                        at: Date(timeIntervalSince1970: rawAt))
        }
        let next = date("next_retry_at")
        let last = date("last_attempt_at")
        let transported = date("transport_delivered_at")
        let observed = date("observed_at")
        let acknowledged = date("acknowledged_at")
        let dead = date("dead_letter_at")
        guard (state != .acknowledged || (observed != nil && acknowledged != nil && next == nil)),
              (state != .deadLetter || (dead != nil && next == nil)),
              (state != .delivered || transported != nil),
              (obj["next_retry_at"] == nil || next != nil),
              (obj["last_attempt_at"] == nil || last != nil),
              (obj["transport_delivered_at"] == nil || transported != nil),
              (obj["observed_at"] == nil || observed != nil),
              (obj["acknowledged_at"] == nil || acknowledged != nil),
              (obj["dead_letter_at"] == nil || dead != nil),
              (obj["last_error"] == nil || failure != nil) else { return nil }
        return CompletionDelivery(
            noticeID: noticeID.lowercased(), created: created, state: state,
            attempts: attempts, nextRetryAt: next, lastAttemptAt: last,
            transportDeliveredAt: transported, observedAt: observed,
            acknowledgedAt: acknowledged, lastError: failure, deadLetterAt: dead,
            legacyReconciled: obj["legacy_reconciled"] as? Bool ?? false)
    }

    static func stored(_ envelope: HandoffEnvelope) -> [String: Any] {
        var out: [String: Any] = [
            "handoff_id": envelope.id,
            "project_dir": envelope.projectDir,
            "created": envelope.created.timeIntervalSince1970,
            "state": envelope.state.rawValue,
        ]
        if let title = envelope.title { out["title"] = title }
        if let from = envelope.fromSession { out["from_session"] = from }
        return out
    }

    static func stored(_ wait: CoordinationWait) -> [String: Any] {
        [
            "id": wait.id, "repository": wait.repository, "paths": wait.paths,
            "owner_session_id": wait.ownerSessionID,
            "release_condition": wait.releaseCondition,
            "created": wait.created.timeIntervalSince1970,
            "waiters": wait.waiters.map { waiter -> [String: Any] in
                var row: [String: Any] = [
                    "session_id": waiter.sessionID, "reason": waiter.reason,
                    "created": waiter.created.timeIntervalSince1970,
                ]
                if let at = waiter.requestDeliveredAt {
                    row["request_delivered_at"] = at.timeIntervalSince1970
                }
                if let at = waiter.releaseDeliveredAt {
                    row["release_delivered_at"] = at.timeIntervalSince1970
                }
                return row
            },
        ]
    }

    static func stored(_ delivery: SessionDelivery) -> [String: Any] {
        var out: [String: Any] = [
            "terminal_id": delivery.identity.terminalID,
            "tty": delivery.identity.tty,
            "summary": delivery.summary,
            "reported_at": delivery.reportedAt.timeIntervalSince1970,
            "settled": delivery.settled,
        ]
        if let assistant = delivery.identity.assistant { out["assistant"] = assistant.rawValue }
        if let pid = delivery.identity.pid { out["pid"] = Int(pid) }
        if let start = delivery.identity.processStart {
            out["process_start"] = start.timeIntervalSince1970
        }
        if let conversation = delivery.identity.conversationID {
            out["conversation_id"] = conversation
        }
        return out
    }

    static func sessionDelivery(from obj: [String: Any]) -> SessionDelivery? {
        guard let terminalID = obj["terminal_id"] as? String, !terminalID.isEmpty,
              terminalID.count <= 512,
              let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName),
              let tty = obj["tty"] as? String, !tty.isEmpty, tty.count <= 512,
              let pidValue = obj["pid"] as? Int, let pid = Int32(exactly: pidValue),
              let processStart = obj["process_start"] as? Double,
              let conversation = obj["conversation_id"] as? String,
              !conversation.isEmpty, conversation.count <= 512,
              let summary = obj["summary"] as? String,
              !summary.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              summary.count <= sessionDeliverySummaryLimit,
              !summary.unicodeScalars.contains(where: { $0.value == 0 }),
              let reportedAt = obj["reported_at"] as? Double,
              let settled = obj["settled"] as? Bool else { return nil }
        let identity = SessionWorkIdentity(
            terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
            processStart: Date(timeIntervalSince1970: processStart),
            conversationID: conversation)
        return SessionDelivery(identity: identity, summary: summary,
                               reportedAt: Date(timeIntervalSince1970: reportedAt),
                               settled: settled)
    }

    static func stored(_ selfState: SessionSelfState) -> [String: Any] {
        var out: [String: Any] = [
            "terminal_id": selfState.identity.terminalID,
            "tty": selfState.identity.tty,
            "claim_settled": selfState.claimSettled,
        ]
        if let assistant = selfState.identity.assistant { out["assistant"] = assistant.rawValue }
        if let pid = selfState.identity.pid { out["pid"] = Int(pid) }
        if let start = selfState.identity.processStart {
            out["process_start"] = start.timeIntervalSince1970
        }
        if let conversation = selfState.identity.conversationID {
            out["conversation_id"] = conversation
        }
        if let claim = selfState.claim { out["claim"] = claim.rawValue }
        if let note = selfState.note { out["note"] = note }
        if let movedBy = selfState.movedBy { out["moved_by"] = movedBy }
        if let personNeeded = selfState.personNeeded { out["person_needed"] = personNeeded }
        if let reportedAt = selfState.claimReportedAt {
            out["claim_reported_at"] = reportedAt.timeIntervalSince1970
        }
        if let debt = selfState.owed {
            var owed: [String: Any] = ["note": debt.note,
                                       "person_needed": debt.personNeeded,
                                       "since": debt.since.timeIntervalSince1970]
            if let movedBy = debt.movedBy { owed["moved_by"] = movedBy }
            out["owed"] = owed
        }
        return out
    }

    static func sessionSelfState(from obj: [String: Any]) -> SessionSelfState? {
        guard let terminalID = obj["terminal_id"] as? String, !terminalID.isEmpty,
              terminalID.count <= 512,
              let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName),
              let tty = obj["tty"] as? String, !tty.isEmpty, tty.count <= 512,
              let pidValue = obj["pid"] as? Int, let pid = Int32(exactly: pidValue),
              let processStart = obj["process_start"] as? Double,
              let conversation = obj["conversation_id"] as? String,
              !conversation.isEmpty, conversation.count <= 512,
              let claimSettled = obj["claim_settled"] as? Bool else { return nil }
        var claim: SessionWorkState?
        if let rawClaim = obj["claim"] as? String {
            // Only the two declarable states survive a reload; anything else in the store is a
            // record this code has no business believing.
            guard let parsed = SessionWorkState(rawValue: rawClaim),
                  parsed == .ready || parsed == .holding else { return nil }
            claim = parsed
        }
        var owed: OwedDebt?
        if let rawOwed = obj["owed"] as? [String: Any] {
            guard let note = rawOwed["note"] as? String, !note.isEmpty,
                  note.count <= sessionSelfNoteLimit,
                  let personNeeded = rawOwed["person_needed"] as? Bool,
                  let since = rawOwed["since"] as? Double else { return nil }
            owed = OwedDebt(note: note, movedBy: rawOwed["moved_by"] as? String,
                            personNeeded: personNeeded,
                            since: Date(timeIntervalSince1970: since))
        }
        guard claim != nil || owed != nil else { return nil }
        let identity = SessionWorkIdentity(
            terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
            processStart: Date(timeIntervalSince1970: processStart),
            conversationID: conversation)
        return SessionSelfState(
            identity: identity, claim: claim, note: obj["note"] as? String,
            movedBy: obj["moved_by"] as? String,
            personNeeded: obj["person_needed"] as? Bool,
            claimReportedAt: (obj["claim_reported_at"] as? Double)
                .map { Date(timeIntervalSince1970: $0) },
            claimSettled: claimSettled, owed: owed)
    }

    static func coordinationWait(from obj: [String: Any]) -> CoordinationWait? {
        guard let id = obj["id"] as? String, isTaskID(id),
              let repositoryRaw = obj["repository"] as? String,
              let repository = canonicalCoordinationRepository(repositoryRaw),
              let pathsRaw = obj["paths"] as? [String],
              let paths = canonicalCoordinationPaths(pathsRaw, repository: repository),
              let owner = boundedCoordinationText(obj["owner_session_id"], limit: 512),
              let condition = boundedCoordinationText(obj["release_condition"], limit: 1_000),
              let created = obj["created"] as? Double else { return nil }
        var seenWaiters = Set<String>()
        let waiters: [CoordinationWaiter] = (obj["waiters"] as? [[String: Any]] ?? []).compactMap {
            row in
            guard let session = boundedCoordinationText(row["session_id"], limit: 512),
                  seenWaiters.insert(session).inserted,
                  let reason = boundedCoordinationText(row["reason"], limit: 1_000),
                  let made = row["created"] as? Double else { return nil }
            return CoordinationWaiter(
                sessionID: session, reason: reason, created: Date(timeIntervalSince1970: made),
                requestDeliveredAt: (row["request_delivered_at"] as? Double)
                    .map(Date.init(timeIntervalSince1970:)),
                releaseDeliveredAt: (row["release_delivered_at"] as? Double)
                    .map(Date.init(timeIntervalSince1970:)))
        }
        guard !waiters.isEmpty else { return nil }
        return CoordinationWait(id: id, repository: repository, paths: paths,
                                ownerSessionID: owner, releaseCondition: condition,
                                created: Date(timeIntervalSince1970: created), waiters: waiters)
    }

    private static func landing(from obj: [String: Any]) -> Landing? {
        guard let state = (obj["state"] as? String).flatMap(LandingState.init(rawValue:)),
              let owner = obj["owner_root_key"] as? String, owner.count == 8,
              owner.allSatisfy({ ("0"..."9").contains($0) || ("a"..."f").contains($0) }),
              let since = obj["since"] as? Double else { return nil }
        func text(_ key: String, limit: Int) -> String? {
            guard let value = obj[key] as? String,
                  !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  value.count <= limit else { return nil }
            return value
        }
        let target = text("target", limit: 200)
        let delivery = text("delivery", limit: 500)
        let commit = text("commit", limit: 200)
        let note = text("note", limit: 500)
        let landedAt = (obj["landed_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        let verificationOrigin = text("verification_origin", limit: 100)
        let verifiedCommit = text("verified_commit", limit: 200)
        let verifiedTargetCommit = text("verified_target_commit", limit: 200)
        let verificationValues: [String?] = [verificationOrigin, verifiedCommit,
                                              verifiedTargetCommit]
        let hasAnyVerification = verificationValues.contains { $0 != nil }
        let hasCompleteVerification = verificationOrigin == "local_target_branch"
            && verifiedCommit == commit
            && [verifiedCommit, verifiedTargetCommit].allSatisfy { value in
                guard let value else { return false }
                return (value.count == 40 || value.count == 64)
                    && value.allSatisfy {
                        ("0"..."9").contains($0) || ("a"..."f").contains($0)
                    }
            }
        guard (obj["target"] == nil || target != nil),
              (obj["delivery"] == nil || delivery != nil),
              (obj["commit"] == nil || commit != nil),
              (obj["note"] == nil || note != nil),
              (obj["landed_at"] == nil || landedAt != nil),
              (obj["verification_origin"] == nil || verificationOrigin != nil),
              (obj["verified_commit"] == nil || verifiedCommit != nil),
              (obj["verified_target_commit"] == nil || verifiedTargetCommit != nil),
              (landedAt == nil || state == .landed),
              (state == .landed) == (commit != nil),
              !hasAnyVerification || (state == .landed && target != nil
                                      && hasCompleteVerification) else { return nil }
        return Landing(state: state, target: target, delivery: delivery,
                       ownerRootKey: owner, since: Date(timeIntervalSince1970: since),
                       commit: commit, note: note, landedAt: landedAt,
                       verificationOrigin: verificationOrigin,
                       verifiedCommit: verifiedCommit,
                       verifiedTargetCommit: verifiedTargetCommit)
    }

    static func handoff(from obj: [String: Any]) -> HandoffEnvelope? {
        guard let id = obj["handoff_id"] as? String, isTaskID(id),
              let projectDir = obj["project_dir"] as? String, StartPoints.usable(projectDir),
              let created = obj["created"] as? Double,
              let state = (obj["state"] as? String).flatMap(HandoffState.init(rawValue:))
        else { return nil }
        let title = (obj["title"] as? String).flatMap { $0.count <= 200 ? $0 : nil }
        let from = (obj["from_session"] as? String).flatMap { $0.count <= 200 ? $0 : nil }
        return HandoffEnvelope(id: id, projectDir: projectDir, title: title,
                               fromSession: from, created: Date(timeIntervalSince1970: created),
                               state: state)
    }

    static func task(from obj: [String: Any]) -> Task? {
        guard let id = obj["id"] as? String, isTaskID(id),
              let state = (obj["state"] as? String).flatMap(State.init(rawValue:)),
              let assistant = (obj["assistant"] as? String).flatMap(Assistant.init(rawValue:)),
              let projectDir = obj["project_dir"] as? String,
              let created = obj["created"] as? Double,
              let secretHash = obj["secret_hash"] as? String else { return nil }
        var task = Task(id: id, state: state,
                        kind: obj["kind"] as? String ?? "custom",
                        title: obj["title"] as? String ?? "task",
                        assistant: assistant, projectDir: projectDir,
                        timeoutMinutes: obj["timeout_minutes"] as? Int ?? 30,
                        created: Date(timeIntervalSince1970: created),
                        secretHash: secretHash)
        task.spawnedAt = (obj["spawned_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.briefedAt = (obj["briefed_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.finishedAt = (obj["finished_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.resultVerifiedAt = (obj["result_verified_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.rootSessionId = obj["root_session"] as? String
        task.rootAssistant = (obj["root_assistant"] as? String).flatMap(Assistant.init(rawValue:))
        task.rootLabel = obj["root_label"] as? String
        task.parentTaskId = obj["parent_task"] as? String
        // A chain position is only meaningful beside the task it descends from, so a row missing
        // one is missing both: a generation with no origin would count against a cap for a chain
        // nothing can name.
        task.respawnOf = (obj["respawn_of"] as? String).flatMap { isTaskID($0) ? $0 : nil }
        task.respawnGeneration = task.respawnOf == nil
            ? 0
            : min(max(obj["respawn_generation"] as? Int ?? 1, 1), respawnLimit)
        task.model = StartPoints.modelName(obj["model"] as? String)
        task.reasoningEffort = assistant == .codex
            ? (obj["reasoning_effort"] as? String).flatMap(ReasoningEffort.init(rawValue:))
            : nil
        task.permission = (obj["permission"] as? String).flatMap(Permission.init(rawValue:)) ?? .ask
        task.plan = obj["plan"] as? String
        task.scheduleID = (obj["schedule_id"] as? String).flatMap { isTaskID($0) ? $0 : nil }
        task.scheduleCloseTab = (obj["schedule_close_tab"] as? String)
            .flatMap(ScheduleCloseTab.init(rawValue:)) ?? .onSuccess
        task.scheduleNotifyFailure = obj["schedule_notify_failure"] as? Bool ?? true
        task.serialize = (obj["serialize"] as? [String] ?? []).filter {
            StartPoints.modelName($0) == $0
        }
        let rawClaims = obj["claims"] as? [String]
        task.claims = (rawClaims ?? []).filter { path in
            !path.isEmpty && path.count <= 1_024 && !path.hasPrefix("/")
                && !path.split(separator: "/", omittingEmptySubsequences: false)
                    .contains(where: { $0 == ".." })
                && !path.unicodeScalars.contains(where: { $0.value == 0 })
        }
        task.claimsDeclared = rawClaims.map { $0.count == task.claims.count } ?? false
        let storedClaimKeys = (obj["claim_keys"] as? [String] ?? []).filter {
            $0.hasPrefix("/")
        }
        task.claimKeys = storedClaimKeys.count == task.claims.count
            ? storedClaimKeys
            : freezeClaims(task.claims, projectDir: task.projectDir)
        task.releasedClaims = (obj["released_claims"] as? [[String: Any]] ?? []).compactMap { row in
            guard let path = row["path"] as? String, path.hasPrefix("/"),
                  let releasedAt = row["released_at"] as? Double else { return nil }
            return ReleasedClaim(path: path, releasedAt: Date(timeIntervalSince1970: releasedAt))
        }
        task.untouchedClaims = (obj["untouched_claims"] as? [String] ?? []).filter {
            task.claims.contains($0)
        }
        if let rawLanding = obj["landing"] as? [String: Any] {
            task.landing = landing(from: rawLanding)
            if task.landing == nil {
                Log.write("orchestrator: ignored invalid landing record for task \(task.id)")
            }
        }
        task.progress = progress(from: obj["progress"])
        task.progressFileNote = (obj["progress_file_note"] as? String).flatMap {
            !$0.isEmpty && $0.count <= progressLimit ? $0 : nil
        }
        task.isolation = (obj["isolation"] as? String).flatMap(Isolation.init(rawValue:)) ?? .none
        if task.isolation == .worktree {
            guard let raw = obj["worktree"] as? [String: Any],
                  let path = raw["path"] as? String, StartPoints.usable(path),
                  let branch = raw["branch"] as? String,
                  branch == worktreeBranch(for: task.id),
                  let base = raw["base"] as? String, !base.isEmpty,
                  let repository = raw["repository"] as? String, StartPoints.usable(repository),
                  path == worktreePath(project: repository, taskID: task.id),
                  let cwd = raw["cwd"] as? String, StartPoints.usable(cwd),
                  relativePath(from: path, to: cwd) != nil else { return nil }
            let requestedBase = raw["requested_base"] as? String ?? "HEAD"
            guard validIsolationBase(requestedBase),
                  (base.count == 40 || base.count == 64),
                  base.allSatisfy({ ("0"..."9").contains($0) || ("a"..."f").contains($0) })
            else { return nil }
            var worktree = Worktree(path: path, branch: branch, base: base,
                                    repository: repository, cwd: cwd)
            worktree.head = raw["head"] as? String
            worktree.commits = raw["commits"] as? Int
            worktree.dirty = raw["dirty"] as? Bool
            worktree.baseDirty = raw["base_dirty"] as? Int ?? 0
            worktree.requestedBase = requestedBase
            task.worktree = worktree
        }
        task.queuedSecret = obj["queued_secret"] as? String
        task.attachSessionId = (obj["attach_session"] as? String).flatMap {
            !$0.isEmpty && $0.count <= 512 ? $0 : nil
        }
        // A registry written before tasks had a depth holds only tasks a root dispatched, which
        // is exactly what 1 means.
        task.depth = (obj["depth"] as? Int).map { min(max($0, 1), 9) } ?? 1
        task.childTerminalId = obj["child_terminal"] as? String
        task.childBackend = (obj["child_backend"] as? String).flatMap(Backend.init(rawValue:))
        task.childTaskRootAccess = obj["child_task_root_access"] as? Bool == true
        task.childTTY = obj["child_tty"] as? String
        task.childPID = (obj["child_pid"] as? Int).flatMap(Int32.init(exactly:))
        task.childProcStart = (obj["child_proc_start"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.childSessionId = obj["child_session"] as? String
        task.closeAt = (obj["close_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        if let raw = obj["terminal_intervention"] as? [String: Any],
           let kind = (raw["kind"] as? String).flatMap(TerminalInterventionKind.init(rawValue:)),
           let message = raw["message"] as? String, !message.isEmpty {
            task.terminalIntervention = TerminalIntervention(kind: kind, message: message)
        } else if let legacy = obj["terminal_intervention"] as? String, !legacy.isEmpty {
            // Compatibility for the short-lived crash-recovery store that persisted only prose.
            // New rows never infer type from text; this branch exists solely to retain its reason.
            let modal = task.childBackend == .iterm && legacy.contains("iTerm2 needs attention")
            task.terminalIntervention = TerminalIntervention(
                kind: modal ? .iTermModal : .terminal, message: legacy)
        }
        task.workCleanupAt = (obj["work_cleanup_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.buildCleanupAt = (obj["build_cleanup_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.transcriptPath = obj["transcript"] as? String
        task.transcriptProven = obj["transcript_proven"] as? Bool == true
            && task.childSessionId != nil && task.transcriptPath != nil
        task.notifyCount = min(max(obj["notify_count"] as? Int ?? 0, 0), notifyTaskLimit)
        if task.transcriptProven, task.assistant == .claude,
           let path = task.transcriptPath, let sessionID = task.childSessionId,
           URL(fileURLWithPath: path).deletingPathExtension().lastPathComponent != sessionID {
            // A proof belongs to the path/session pair, not merely to the row. A malformed pair
            // is retained for diagnosis but cannot participate in cascade ownership; usage may
            // still independently re-prove the path from its task marker.
            task.transcriptProven = false
        }
        task.summary = obj["summary"] as? String
        if let rawDelivery = obj["completion_delivery"] as? [String: Any] {
            task.completionDelivery = completionDelivery(from: rawDelivery)
            if task.completionDelivery == nil {
                Log.write("orchestrator: ignored invalid completion delivery for task \(task.id)")
            }
        }
        task.artifacts = obj["artifacts"] as? [String] ?? []
        task.verification = verification(from: obj["verification"])
        if let counts = obj["usage"] as? [String: Any] {
            var usage = Usage()
            usage.input = counts["input"] as? Int ?? 0
            usage.output = counts["output"] as? Int ?? 0
            usage.cacheRead = counts["cache_read"] as? Int ?? 0
            usage.cacheWrite = counts["cache_write"] as? Int ?? 0
            usage.total = counts["total"] as? Int ?? 0
            usage.model = counts["model"] as? String
            usage.costUsd = counts["cost_usd"] as? Double
            task.usage = usage
        }
        return task
    }

    // MARK: - Cleanup

    private static func orphanWorktree(at path: String, taskID: String) -> Worktree? {
        guard isTaskID(taskID), let branch = worktreeBranch(for: taskID),
              let common = git(["rev-parse", "--git-common-dir"], cwd: path),
              common.status == 0 else { return nil }
        let rawCommon = common.output.trimmingCharacters(in: .whitespacesAndNewlines)
        let commonPath = rawCommon.hasPrefix("/") ? rawCommon
            : URL(fileURLWithPath: path).appendingPathComponent(rawCommon).standardizedFileURL.path
        let repository = canonicalFilesystemPath(
            URL(fileURLWithPath: commonPath).deletingLastPathComponent().path)
        guard StartPoints.usable(repository),
              let reflog = git(["reflog", "show", "--format=%H", branch],
                               cwd: repository), reflog.status == 0,
              // `git reflog show` is newest first. The branch-creation entry is the oldest and
              // its new value is the commit from which `worktree add -b` created the branch.
              let base = reflog.output.split(whereSeparator: \.isNewline).last.map(String.init)
        else { return nil }
        return Worktree(path: path, branch: branch, base: base,
                        repository: repository, cwd: path)
    }

    /// Registry rows are capped, so the directory shape is a second index. If its git metadata
    /// cannot prove the same disposal facts as a live row, the orphan is deliberately retained.
    private static func cleanupOrphanWorktrees(knownTaskIDs: Set<String>, olderThan cutoff: Date) {
        let manager = FileManager.default
        guard let repositories = try? manager.contentsOfDirectory(at: worktreeRoot,
            includingPropertiesForKeys: [.isDirectoryKey], options: [.skipsHiddenFiles]) else {
            return
        }
        for repositoryDirectory in repositories {
            guard let tasks = try? manager.contentsOfDirectory(at: repositoryDirectory,
                includingPropertiesForKeys: [.isDirectoryKey], options: [.skipsHiddenFiles]) else {
                continue
            }
            for directory in tasks {
                let id = directory.lastPathComponent
                guard isTaskID(id), !knownTaskIDs.contains(id) else { continue }
                let modified = try? directory.resourceValues(
                    forKeys: [.contentModificationDateKey]).contentModificationDate
                guard let modified, modified < cutoff else { continue }
                guard let worktree = orphanWorktree(at: directory.path, taskID: id) else {
                    RemoteAuth.audit("orchestrator.worktree.kept", [
                        "task": id, "branch": worktreeBranch(for: id) ?? "?",
                        "why": "unreadable",
                    ])
                    continue
                }
                disposeWorktree(worktree, taskID: id, why: "swept")
            }
        }
    }

    /// Task directories are working files, not the archive — the record survives here, the
    /// directory goes once it is a day old and its task is over. The registry keeps the newest 200
    /// ordinary records, plus every pending landing obligation until root settles it.
    static func cleanup() {
        load()
        let cutoff = Date().addingTimeInterval(-24 * 3600)
        lock.lock()
        let done = tasks.values.filter { task in
            task.state.isTerminal && task.landing?.state != .pending
                && (task.finishedAt ?? task.created) < cutoff
        }
        let expiredHandoffs = handoffs.values.filter {
            $0.state.isTerminal && $0.created < cutoff
        }
        lock.unlock()
        for task in done {
            if let worktree = task.worktree,
               FileManager.default.fileExists(atPath: worktree.path) {
                disposeWorktree(worktree, taskID: task.id, why: "swept")
            }
            try? FileManager.default.removeItem(at: task.dir)
        }
        for envelope in expiredHandoffs {
            try? FileManager.default.removeItem(at: envelope.dir)
        }
        lock.lock()
        for envelope in expiredHandoffs {
            handoffs.removeValue(forKey: envelope.id)
            handoffDeliveries.removeValue(forKey: envelope.id)
        }
        let all = tasks.values.sorted { $0.created > $1.created }
        let removable = all.dropFirst(200).filter { $0.landing?.state != .pending }
        let removedIDs = Set(removable.map(\.id))
        let oldSessionDeliveryIDs = sessionDeliveries.values
            .sorted { $0.reportedAt > $1.reportedAt }.dropFirst(200)
            .map { $0.identity.terminalID }
        if !removable.isEmpty {
            for task in removable { tasks.removeValue(forKey: task.id) }
        }
        for id in oldSessionDeliveryIDs { sessionDeliveries.removeValue(forKey: id) }
        lock.unlock()
        if !removable.isEmpty || !expiredHandoffs.isEmpty || !oldSessionDeliveryIDs.isEmpty {
            save()
        }
        let retained = Set(all.filter { !removedIDs.contains($0.id) }.map(\.id))
        cleanupOrphanWorktrees(knownTaskIDs: retained, olderThan: cutoff)
        _ = OwnedStorage.compact()
    }

    // MARK: - Small lookups

    private static func held(_ id: String) -> Task? {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks[id]
    }

    private static func heldHandoff(_ id: String) -> HandoffEnvelope? {
        load()
        lock.lock(); defer { lock.unlock() }
        return handoffs[id]
    }

    private static func heldSecret(_ id: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return secrets[id]
    }

    private static func target(withID id: String) -> TargetSession? {
        if isOnMainQueue { return SessionWatch.shared.targets.first { $0.id == id } }
        return DispatchQueue.main.sync { SessionWatch.shared.targets.first { $0.id == id } }
    }

    /// Task roots are conversation identities and therefore require the process-bound reader.
    /// Handoff's documented `from_session` is deliberately wider: it may name the watched
    /// terminal directly. Keeping the policies explicit prevents that compatibility shortcut
    /// from weakening task mounting, notifications, overlap warnings, or root cancellation.
    enum RootResolution: Equatable {
        case task, handoff
    }

    /// Resolve the two namespaces accepted at dispatch into the one durable conversation key
    /// used by notification, grouping and root-close cascade. An unresolved spelling remains in
    /// the record for compatibility, but the response warns that no process-bound owner was
    /// proved; silently accepting it is what used to create an orphaned child.
    static func canonicalRootSession(
        _ supplied: String?, assistant: Assistant?, among targets: [TargetSession],
        sessionID: (TargetSession) -> String?
    ) -> (sessionID: String?, warning: [String: Any]?) {
        guard let supplied else { return (nil, nil) }
        let expectedAssistant = assistant ?? .claude
        var canonical: Set<String> = []
        for target in targets where target.assistant == expectedAssistant {
            let conversation = sessionID(target)
            guard target.id == supplied || conversation == supplied,
                  let conversation else { continue }
            canonical.insert(conversation)
        }
        if canonical.count == 1, let resolved = canonical.first {
            return (resolved, nil)
        }
        return (supplied, [
            "code": "root_unresolved",
            "message": "root.session_id did not resolve to one live process-bound session; "
                + "completion notification, grouping and close cascade are not guaranteed.",
        ])
    }

    static func target(forRootSession rootSessionID: String,
                       assistant: Assistant?, resolution: RootResolution,
                       among targets: [TargetSession],
                       sessionID: (TargetSession) -> String?) -> TargetSession? {
        targets.first { target in
            guard assistant == nil || target.assistant == assistant else { return false }
            if resolution == .handoff && target.id == rootSessionID { return true }
            return sessionID(target) == rootSessionID
        }
    }

    private static func rootTargets() -> [TargetSession] {
        if isOnMainQueue { return SessionWatch.shared.targets }
        return DispatchQueue.main.sync { SessionWatch.shared.targets }
    }

    private static func activeRootIdentityEvidence(claimed: String?)
        -> [RootIdentityEvidence] {
        guard let claimed, !claimed.isEmpty else { return [] }
        return rootTargets().compactMap { target in
            guard target.id == claimed, let actualAssistant = target.assistant,
                  let canonical = Transcript.sessionID(of: target), !canonical.isEmpty else {
                return nil
            }
            return RootIdentityEvidence(source: "active_terminal", terminalID: target.id,
                                        canonicalSessionID: canonical,
                                        assistant: actualAssistant)
        }
    }

    /// Test seam: forget everything in memory.
    static func forget() {
        lock.lock()
        tasks = [:]
        handoffs = [:]
        handoffDeliveries = [:]
        coordinationWaits = [:]
        sessionDeliveries = [:]
        sessionSelfStates = [:]
        handoffTitlesByTerminal = [:]
        secrets = [:]
        dispatchTimes = []
        notifyTimes = []
        notifyCredentialFailureTimes = []
        badResults = []
        handledScheduleFires = [:]
        pendingScheduleFires = [:]
        lastMissedScheduleFires = [:]
        dispatchingSchedules = []
        invalidScheduleFingerprints = [:]
        scheduleWriteTimes = []
        scheduleRunnerForTesting = nil
        scheduleDispatchEnqueuerForTesting = nil
        completionPumpEnqueuerForTesting = nil
        storeSaveInterceptorForTesting = nil
        rootIdentityEvidenceForTesting = nil
        completionPumpScheduled = false
        completionPumpGeneration += 1
        workspaceOverlapObserverForTesting = nil
        rootNotificationObserverForTesting = nil
        attachedSenderForTesting = nil
        attachmentInventoryForTesting = nil
        taskStarterForTesting = nil
        agentPushForTesting = nil
        titlesByTerminal = [:]
        rolesByTerminal = [:]
        loaded = false
        lock.unlock()
        ownershipLock.lock()
        ownershipCache = [:]
        ownershipLock.unlock()
    }

    /// Test seam: wait until every pump already scheduled has finished. Callers first keep the
    /// main run loop moving until any main-thread finalization has happened, so this cannot wait
    /// on a pump that is itself waiting on main.
    @discardableResult
    /// Wait until a serialized promotion has actually happened.
    ///
    /// **Both queues, not one.** The pump hands the promotion to the terminal broker and returns
    /// immediately, and an admission it was refused comes back through the pump queue a quarter
    /// of a second later. A drain that watched only the pump queue therefore returned while the
    /// promotion was still in the lane — reliably enough to pass on an idle machine, and not on
    /// one where anything else was using a terminal.
    static func drainSerializePumpForTesting(timeout: TimeInterval = 3) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        func pause() {
            if Thread.isMainThread { RunLoop.main.run(until: Date().addingTimeInterval(0.01)) }
            else { Thread.sleep(forTimeInterval: 0.01) }
        }
        while Date() < deadline {
            let done = DispatchSemaphore(value: 0)
            serializePumpQueue.async { done.signal() }
            var pumped = false
            while Date() < deadline {
                if done.wait(timeout: .now()) == .success { pumped = true; break }
                pause()
            }
            guard pumped else { return false }
            if RemoteServer.shared.terminalOutstandingForTesting().total == 0 { return true }
            pause()
        }
        return false
    }
}
