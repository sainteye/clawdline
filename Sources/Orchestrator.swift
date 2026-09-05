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
                               "permission_mode", "timeout_minutes", "deliverables", "kind", "plan", "graph"])
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
        if case .bad(let why) = OrchestratorDraft.draft(from: validation, expecting: id,
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
                    "deliverables", "kind", "plan", "graph"] where task[key] == nil {
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
        guard OrchestratorDraft.isTaskID(id) else {
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
        guard OrchestratorDraft.isTaskID(id),
              let schedule = schedules().first(where: { $0.id == id }) else {
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

    /// The title for one conversation the ordinary project history deliberately hides because it
    /// began as Clawdline plumbing, or nil when the existing place-resume route must refuse it.
    ///
    /// The exception is schedule-only, terminal-only, project- and assistant-exact, and backed by
    /// the same task/transcript ownership proof used for usage accounting. This keeps dispatched
    /// children out of the general history picker while allowing the schedule detail that disclosed
    /// the run to pick that exact conversation back up.
    static func scheduledResumeTitle(sessionID: String, assistant: Assistant,
                                     projectDir: String) -> String? {
        load()
        lock.lock()
        defer { lock.unlock() }
        return tasks.values.first {
            $0.scheduleID != nil && $0.state.isTerminal && $0.assistant == assistant
                && $0.projectDir == projectDir && $0.childSessionId == sessionID
                && availableScheduledSessionID(of: $0) == sessionID
        }?.title
    }

    private static func availableScheduledSessionID(of task: Task) -> String? {
        guard task.scheduleID != nil, let path = task.transcriptPath,
              FileManager.default.fileExists(atPath: path) else { return nil }
        guard let session = provenChildSessionID(of: task),
              OrchestratorDraft.isTaskID(session) else { return nil }
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

    /// Human names for Usage's durable schedule ids. The live file is authoritative when it
    /// still exists; the newest retained run keeps the title for a schedule that has since been
    /// removed. UUID-only identity remains the fallback when neither source has a label.
    static func usageScheduleLabels() -> [String: String] {
        let live = schedules()
        load()
        lock.lock()
        let snapshots = tasks.values.sorted { $0.created < $1.created }
        lock.unlock()
        var labels: [String: String] = [:]
        for task in snapshots {
            guard let id = task.scheduleID,
                  let label = task.rootLabel?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !label.isEmpty else { continue }
            labels[id] = label
        }
        for schedule in live { labels[schedule.id] = schedule.title }
        return labels
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
        guard OrchestratorDraft.isTaskID(id) else { return nil }
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
        /// The sender's own statement that a plain handoff from the machine's coordinator was
        /// deliberate — that this letter is ordinary work and the crown is not moving. Recorded
        /// exactly as the request set it, including on a handoff from an ordinary session where
        /// it waived nothing: the field says what the caller asserted, never who the sender was.
        let coordinatorPlainHandoff: Bool
        let created: Date
        var state: HandoffState

        var dir: URL {
            Orchestrator.handoffRoot.appendingPathComponent(id, isDirectory: true)
        }
    }

    /// Which tab a handoff was delivered into, and the job title it was opened under.
    ///
    /// Deliberately *not* a field on ``HandoffEnvelope``. The envelope carries no terminal id
    /// because that is a protocol statement about what the registry may remember of a letter,
    /// not an oversight — so the binding lives beside the envelopes in the same file instead,
    /// under its own key, with its own codec.
    ///
    /// **Its lifetime is the tab's, not the envelope's, and that is the whole of the boundary.**
    /// ``cleanup()`` drops an envelope 24 hours after the handoff reaches a terminal state, which
    /// is a clock on the *delivery*: it stops when the first line is confirmed, minutes after the
    /// tab opened. The session that tab holds routinely works for days afterwards, so a label
    /// reclaimed on the envelope's clock would rename a live root on its second morning — which
    /// is the defect this record exists to fix, arriving a day later instead of at the next
    /// restart. The label is therefore reclaimed on the identity below: once a scan of the
    /// machine has failed to find the process it names, it has stopped meaning anything, and
    /// ``pruneClosedHandoffTitles(visible:identities:inventoryComplete:)`` has already stopped
    /// showing it.
    ///
    /// The identity is a ``RootAssignmentIdentity`` because that is the executor-identity record
    /// this app already compares with ``rootAssignmentIdentityMatches(_:observed:)``: an
    /// all-optional tuple that begins as the terminal a tab was opened in and gains its process
    /// fields from the first complete inventory that can see them. The type's name says where it
    /// was introduced, not who is allowed to hold one.
    struct HandoffLabel {
        let handoffID: String
        let label: String
        var identity: RootAssignmentIdentity
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

    // MARK: - Independent feature roots

    /// Persist the audit receipt before emitting the event. That deliberately chooses at-most-once
    /// delivery over restart spam; every event carries the durable assignment and exact executor
    /// identity so an operator can find a pre-brief tab that needs attention.
    @discardableResult
    static func reportRootAssignmentTransition(_ id: String) -> Bool {
        lock.lock()
        guard var assignment = rootAssignments[id],
              let notice = rootAssignmentTransitionNotice(
                state: assignment.state, blocker: assignment.blocker,
                failure: assignment.failure),
              assignment.reportedTransition != notice.receipt else {
            lock.unlock(); return false
        }
        let previous = assignment.reportedTransition
        assignment.reportedTransition = notice.receipt
        rootAssignments[id] = assignment
        lock.unlock()
        guard save() else {
            lock.lock()
            if var current = rootAssignments[id],
               current.reportedTransition == notice.receipt {
                current.reportedTransition = previous
                rootAssignments[id] = current
            }
            lock.unlock()
            return false
        }
        var fields = ["assignment": id, "state": assignment.state.rawValue,
                      "why": notice.reason, "transition": notice.receipt]
        if let identity = assignment.identity {
            fields["terminal"] = identity.terminalID
            fields["assistant"] = identity.assistant.rawValue
            if let pid = identity.pid { fields["pid"] = String(pid) }
            if let start = identity.processStart { fields["process_start"] = String(start) }
            if let conversation = identity.conversationID {
                fields["conversation"] = conversation
            }
        }
        if let observer = rootAssignmentAuditObserverForTesting { observer(notice.event, fields) }
        else { RemoteAuth.audit(notice.event, fields) }
        return true
    }

    /// The directory whose assistant-owned records belong to this child. Kept separate from the
    /// dispatch project so task isolation can choose a different working tree at this seam.
    static func cwd(of task: Task) -> String {
        task.worktree?.cwd ?? task.projectDir
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

    /// The registry lock, which ``OrchestratorRegistry`` now declares. This is that exact
    /// instance rather than a second one: the collections that have moved and the ones still
    /// below are behind the same `NSLock`, held for exactly as long as before. The name stays
    /// here because the bare lock-acquisition regions in this file have not been converged onto
    /// ``OrchestratorRegistry/withTransaction(_:)`` yet, and converging them is a later cut.
    static let lock = OrchestratorRegistry.lock
    /// Opening a terminal is bounded, not cheap: iTerm automation alone may wait 15 seconds.
    /// Pumps arrive from main-thread finalization and startup as well as the remote server, so
    /// they get the same off-main serial shape as ordinary dispatch without competing pumps.
    private static let serializePumpQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.orchestrator.serialize")
    /// Worktree inspection and disposal can each wait on several bounded git subprocesses.
    /// Keeping them on one utility queue both keeps the panel responsive and prevents two close
    /// paths from racing to dispose the same checkout.
    /// Internal rather than private since the worktree lifecycle moved to `OrchestratorDraft`,
    /// which enqueues on it; the queue itself, its label and its QoS are unchanged.
    static let worktreeQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.orchestrator.worktree", qos: .utility)
    /// A background pump and a new remote dispatch may both persist. Serializing the whole
    /// snapshot-and-write prevents an older snapshot from winning the atomic rename last.
    private static let storeSaveLock = NSLock()
    /// Terminal delivery leaves the registry lock before it types. Serializing coordination
    /// mutations closes the otherwise possible gap where two retries both see a missing receipt.
    private static let coordinationDeliveryLock = NSLock()
    private static let completionDeliveryQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.orchestrator.completion", qos: .utility)
    static let completionAttemptLimit = 8
    static let completionRetryMaximum: TimeInterval = 300
    static let legacyCompletionLookback: TimeInterval = 7 * 24 * 3600
    static let legacyCompletionBatchLimit = 25
    private static var loaded = false
    static var tasks: [String: Task] = [:]
    static var restartReceipt: RestartReceipt?
    private static var handoffs: [String: HandoffEnvelope] = [:]
    /// Handoff id → the tab that handoff was delivered into and what it is called. Durable, and
    /// the only thing that gives a handed-off root its job title back after a restart.
    private static var handoffLabels: [String: HandoffLabel] = [:]
    private static var handoffDeliveries: [String: HandoffDelivery] = [:]
    private static var rootAssignments: [String: RootAssignment] = [:]
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
        label: "com.tsunamiworks.clawdline.orchestrator.schedules", qos: .utility)
    static var scheduleRunnerForTesting: ((Schedule) -> Reply)?
    static var scheduleDispatchEnqueuerForTesting: ((@escaping () -> Void) -> Void)?
    static var completionPumpEnqueuerForTesting: ((@escaping () -> Void) -> Void)?
    /// Deterministic persistence seam. Returning non-nil replaces the filesystem write with that
    /// result; production leaves it nil. The serialized snapshot is handed over after the task
    /// lock is released so an injected concurrent mutation exercises the real save window.
    static var storeSaveInterceptorForTesting: ((Data) -> Bool?)?
    static var childIdentityRefreshForTesting: ((TargetSession, inout Task) -> Bool)?
    /// Overrides only the positive ingress evidence. Nil uses current process-bound terminal and
    /// Coordinator facts; an empty array is a deliberate unknown/offline fixture.
    static var rootIdentityEvidenceForTesting: [OrchestratorDraft.RootIdentityEvidence]?
    /// Test seam: observes the warning decision before optional terminal delivery.
    static var workspaceOverlapObserverForTesting:
        ((Task, [OrchestratorDraft.WorkspaceOverlap]) -> Void)?
    /// Test receipt for the semantic root-notification boundary. The terminal transport itself
    /// is exercised elsewhere; this proves a terminal path reached finalization and its notice.
    static var rootNotificationObserverForTesting: ((Task) -> Void)?
    static var rootAssignmentAuditObserverForTesting:
        ((String, [String: String]) -> Void)?
    static var attachedSenderForTesting: ((String, TargetSession) -> String?)?
    /// The session inventory an attachment resolves against, and the starter a tab-opening
    /// dispatch uses.
    ///
    /// Production reads `SessionWatch` and opens a real terminal; a suite can do neither, which
    /// is how everything past
    /// ``OrchestratorDraft/attachmentDecision(sessionID:assistant:sessions:states:tasks:roles:isChoosing:excluding:)``
    /// — `spawnAttached`, `502 attach_delivery_failed`, the single-flight check the serialize
    /// pump re-runs, and every tab-opening failure at dispatch — came to have no test that could
    /// go red. Both are cleared by ``forget()``.
    static var attachmentInventoryForTesting: ([TargetSession], [String: SessionState])?
    static var taskStarterForTesting: TaskStarter?
    /// Test seam for the final display sentence; production always enters WebPush below.
    static var agentPushForTesting:
        ((String, String, String, String?, String?) -> WebPush.Delivery)?
    /// Test seam for the delivery push, so "once for a new receipt, never for a repeat" can be
    /// checked without a paired device. Production always enters WebPush.
    static var sessionDeliveryPushForTesting: ((StateHook.PushMessage) -> Void)?
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
        // Outside the lock, beside the audit, and only on this branch: the `created: false` return
        // above is a root repeating itself inside one turn, and a phone that buzzed twice for one
        // delivery would be the old defect wearing a new event.
        announceDelivery(made)
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

    /// Closure notes have their own persistence bound. Keeping this validator separate prevents
    /// a later self-state copy change from accepting an attestation the loader must discard.
    private static func closureNote(_ raw: String?) -> String?? {
        guard let raw else { return .some(nil) }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= closureNoteLimit,
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
        // The observed-turn clock the closure attestation names. It moves before any receipt is
        // settled, so an attestation cannot survive the next active transition SessionWatch sees.
        if noteActivityLocked(terminalID: terminalID, state: state) { changed = true }
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

    // MARK: - Session closeability

    /// The fourth projection, beside terminal `state`, `work_state` and the `owed` overlay.
    ///
    /// `ready` means able to accept work. This says whether a Session is able to *end*, and the
    /// two are independent: a session with nothing to do can still own a pending landing, and a
    /// session mid-turn can owe nobody anything. Each value has exactly one action, which is the
    /// same design test the work-state vocabulary is held to (docs/session-states.md).
    ///
    /// There is deliberately no `closed` case. A closed Session leaves the live inventory; what
    /// remains is the bounded audit row `RemoteAuth.audit` already writes, not a permanent live
    /// record which every later reader would have to learn to ignore.
    enum SessionCloseability: String, CaseIterable {
        /// The broker sees a positive obligation. Do not close.
        case blocked
        /// Broker blockers are clear, but local or external work is not broker-observable.
        /// Obtain an attestation bound to this exact Session.
        case needsAttestation = "needs_attestation"
        /// Broker blockers are clear and a fresh closure attestation is bound to the exact
        /// current process. The close button may proceed.
        case safe
        /// Evidence is stale, missing or ambiguous. Refresh or audit, and fail closed.
        case unknown
    }

    /// Why a Session is not closeable, in one closed vocabulary. Grouped by `kind` because the
    /// three groups have three different next actions and only one of them is a list of work.
    enum CloseabilityReasonCode: String, CaseIterable {
        // Positive obligations the broker can prove from its own registries.
        case terminalWorking = "terminal_working"
        case terminalWaitingYou = "terminal_waiting_you"
        case ownTaskUnfinished = "own_task_unfinished"
        case liveDescendantTask = "live_descendant_task"
        case taskWithoutResult = "task_without_result"
        case pendingLandingOwned = "pending_landing_owned"
        case coordinationWaitOwned = "coordination_wait_owned"
        case coordinationWaitWaiting = "coordination_wait_waiting"
        case openHandoff = "open_handoff"
        case completionUndelivered = "completion_undelivered"
        case owedDecision = "owed_decision"
        case dirtyIsolatedWorktree = "dirty_isolated_worktree"
        case touchedClaimsWithoutClosure = "touched_claims_without_closure"
        // Evidence problems. Every one of these fails the whole projection closed to `unknown`,
        // because what they cast doubt on is the completeness of the obligation list itself.
        case terminalUnreadable = "terminal_unreadable"
        case sessionInventoryStale = "session_inventory_stale"
        case sessionInventoryMissing = "session_inventory_missing"
        case sessionIdentityUnbound = "session_identity_unbound"
        case sessionIdentityAmbiguous = "session_identity_ambiguous"
        // Answerable only by the Session itself.
        case attestationMissing = "attestation_missing"
        case attestationSuperseded = "attestation_superseded"

        var kind: CloseabilityReasonKind {
            switch self {
            case .terminalUnreadable, .sessionInventoryStale, .sessionInventoryMissing,
                 .sessionIdentityUnbound, .sessionIdentityAmbiguous:
                return .evidence
            case .attestationMissing, .attestationSuperseded:
                return .attestation
            default:
                return .obligation
            }
        }
    }

    enum CloseabilityReasonKind: String {
        case obligation, evidence, attestation
    }

    /// Who or what clears one reason. The same distinction `work_moved_by` draws on the row:
    /// "your build; nobody" and "the user's decision; the user" are opposite calls to action.
    enum CloseabilityMover: Equatable {
        /// The Session being asked about. It is the one that must finish, land or attest.
        case thisSession
        /// Another live Session, named by its terminal-neutral id.
        case otherSession(String)
        /// A person. Nothing moves until somebody decides.
        case person
        /// A task record, when the broker cannot name the terminal holding it.
        case task(String)
        /// A fresh reading. Nobody is at fault; the evidence is simply not current.
        case broker

        var wire: [String: Any] {
            switch self {
            case .thisSession: return ["kind": "session", "self": true, "person_needed": false]
            case .otherSession(let id):
                return ["kind": "session", "session_id": id, "self": false,
                        "person_needed": false]
            case .person: return ["kind": "person", "person_needed": true]
            case .task(let id): return ["kind": "task", "task_id": id, "person_needed": false]
            case .broker: return ["kind": "broker", "person_needed": false]
            }
        }
    }

    /// One typed reason, naming its subject so the reader can go and look at the thing itself.
    struct CloseabilityReason: Equatable {
        let code: CloseabilityReasonCode
        let subjectKind: String?
        let subjectID: String?
        let mover: CloseabilityMover

        init(_ code: CloseabilityReasonCode, subjectKind: String? = nil,
             subjectID: String? = nil, mover: CloseabilityMover = .thisSession) {
            self.code = code
            self.subjectKind = subjectKind
            self.subjectID = subjectID
            self.mover = mover
        }

        var wire: [String: Any] {
            var out: [String: Any] = ["code": code.rawValue, "kind": code.kind.rawValue,
                                      "mover": mover.wire]
            if let subjectKind { out["subject_kind"] = subjectKind }
            if let subjectID { out["subject_id"] = subjectID }
            return out
        }
    }

    /// The exact process's own account of work the broker cannot see: shared-tree hunks it owns,
    /// unregistered local todos, artifacts and deployments outside the repository, decisions
    /// never written to `owed`. It is subject-bound attestation, not caller authentication or
    /// completion — only the broker merge may output `safe`.
    struct ClosureAttestation: Equatable {
        let id: String
        let identity: SessionWorkIdentity
        /// The generation the declaring turn named. A later working/waiting turn moves it.
        let activityGeneration: Int
        /// The obligation clock at the moment of the merge. Any later obligation change moves it.
        let obligationGeneration: Int
        let note: String?
        let auditID: String?
        let created: Date
    }

    static let closureNoteLimit = 200
    static let closureAuditIDLimit = 128
    /// A complete background inventory is expected at least every 20 seconds. Two missed beats
    /// plus scheduling slack is the safety boundary; older evidence cannot support `safe`.
    static let closeabilityInventoryMaxAge: TimeInterval = 45

    /// Per-terminal turn clock. It advances when a terminal is observed *entering* working or
    /// waiting — a new turn — and not for a changed live line inside one turn. Attestations name
    /// it, so a session that starts working again after attesting has invalidated its own claim.
    ///
    /// Deliberately never reset when the process in a terminal changes: a monotone counter that
    /// is never reused cannot make an old attestation match a new process by arithmetic, and the
    /// full identity comparison refuses that case anyway.
    private static var sessionActivityGenerations: [String: Int] = [:]
    private static var sessionActivityClasses: [String: String] = [:]
    private static var closureAttestations: [String: ClosureAttestation] = [:]
    /// Machine-wide obligation clock. It advances only when the *content* of the obligation
    /// evidence changes — see ``obligationFingerprintLocked()`` — so writing an attestation, a
    /// title or a progress note does not silently invalidate the attestation just written.
    private static var obligationGeneration = 0
    private static var obligationFingerprint = ""
    private static var closeabilityRegistryReadCountForTesting = 0

    static func resetCloseabilityRegistryReadCountForTesting() {
        lock.lock(); closeabilityRegistryReadCountForTesting = 0; lock.unlock()
    }

    static func closeabilityRegistryReadsForTesting() -> Int {
        lock.lock(); defer { lock.unlock() }
        return closeabilityRegistryReadCountForTesting
    }

    private static func activityClass(of state: SessionState) -> String {
        switch state {
        case .working: return "working"
        case .waiting: return "waiting"
        case .idle: return "idle"
        case .unknown: return "unknown"
        }
    }

    /// Caller holds `lock`.
    @discardableResult
    private static func noteActivityLocked(terminalID: String, state: SessionState) -> Bool {
        let now = activityClass(of: state)
        let before = sessionActivityClasses[terminalID]
        guard before != now else { return false }
        sessionActivityClasses[terminalID] = now
        guard now == "working" || now == "waiting" else { return true }
        sessionActivityGenerations[terminalID] = (sessionActivityGenerations[terminalID] ?? 0) + 1
        return true
    }

    static func activityGeneration(ofTerminal terminalID: String) -> Int {
        load()
        lock.lock(); defer { lock.unlock() }
        return sessionActivityGenerations[terminalID] ?? 0
    }

    static func currentObligationGeneration() -> Int {
        load()
        lock.lock()
        settleObligationGenerationLocked()
        let generation = obligationGeneration
        lock.unlock()
        return generation
    }

    /// A stable digest of exactly the evidence the closeability projection reads. Caller holds
    /// `lock`. Sorted and fully spelled, because a fingerprint that misses a field is a clock
    /// that does not tick for the change that mattered.
    private static func obligationFingerprintLocked() -> String {
        var parts: [String] = []
        for task in tasks.values.sorted(by: { $0.id < $1.id }) {
            var row: [String] = []
            row.append(task.id)
            row.append(task.state.rawValue)
            row.append(task.rootSessionId ?? "-")
            row.append(task.rootAssistant?.rawValue ?? "-")
            row.append(task.parentTaskId ?? "-")
            row.append(task.childTerminalId ?? "-")
            row.append(task.childSessionId ?? "-")
            row.append(task.resultVerifiedAt == nil ? "0" : "1")
            row.append(task.summary == nil ? "0" : "1")
            row.append(task.landing?.state.rawValue ?? "-")
            row.append(task.landing?.ownerRootKey ?? "-")
            let dirty: String = task.worktree?.dirty.map { $0 ? "1" : "0" } ?? "-"
            row.append(dirty)
            row.append(task.completionDelivery?.state.rawValue ?? "-")
            row.append(task.claims.sorted().joined(separator: ","))
            row.append(task.untouchedClaims.sorted().joined(separator: ","))
            parts.append(row.joined(separator: "\u{1}"))
        }
        for wait in coordinationWaits.values.sorted(by: { $0.id < $1.id }) {
            let pending = wait.waiters.filter { $0.releaseDeliveredAt == nil }
                .map(\.sessionID).sorted().joined(separator: ",")
            parts.append([wait.id, wait.ownerSessionID, pending].joined(separator: "\u{1}"))
        }
        for handoff in handoffs.values.sorted(by: { $0.id < $1.id }) {
            parts.append([handoff.id, handoff.state.rawValue, handoff.fromSession ?? "-"]
                .joined(separator: "\u{1}"))
        }
        for selfState in sessionSelfStates.values
            .sorted(by: { $0.identity.terminalID < $1.identity.terminalID }) {
            parts.append([selfState.identity.terminalID,
                          selfState.owed?.note ?? "-"].joined(separator: "\u{1}"))
        }
        let canonical = parts.joined(separator: "\u{2}")
        return RemoteAuth.hex(SHA256.hash(data: Data(canonical.utf8)))
    }

    /// Advance the obligation clock when, and only when, the obligation evidence changed.
    /// Caller holds `lock`. Returns whether it moved.
    @discardableResult
    private static func settleObligationGenerationLocked() -> Bool {
        let current = obligationFingerprintLocked()
        guard current != obligationFingerprint else { return false }
        obligationFingerprint = current
        obligationGeneration += 1
        return true
    }

    /// The opaque CAS token clients copy from a projection into a close request.
    ///
    /// It covers the exact process identity, the two clocks that can invalidate an attestation,
    /// and the resulting state. It deliberately does **not** cover the SessionWatch scan
    /// generation: that advances on its own every few seconds, and a token nobody could hold
    /// still long enough to send would make every close a race rather than proving anything.
    /// Scan freshness reaches the reader as `source` and as `unknown`, which no close accepts.
    static func closeabilityVersion(identity: SessionWorkIdentity, activityGeneration: Int,
                                    obligationGeneration: Int,
                                    state: SessionCloseability) -> String {
        let canonical = [
            "cl1", identity.terminalID, identity.assistant?.rawValue ?? "-", identity.tty,
            identity.pid.map(String.init) ?? "-",
            identity.processStart.map { String(Int($0.timeIntervalSince1970)) } ?? "-",
            identity.conversationID ?? "-", String(activityGeneration),
            String(obligationGeneration), state.rawValue,
        ].joined(separator: "\u{1}")
        return "cl1_" + RemoteAuth.hex(SHA256.hash(data: Data(canonical.utf8))).prefix(32)
    }

    /// Everything the pure precedence needs, gathered by the caller so the decision itself can be
    /// tested exhaustively without a registry.
    struct CloseabilityInput {
        var terminalState: SessionState
        var identityBound: Bool
        /// The SessionWatch inventory that produced this reading was a complete one.
        var inventoryComplete: Bool
        /// When that inventory was observed. Nil is the closed `missing` source state.
        var inventoryObservedAt: Date?
        /// How many rows of that inventory resolve to this exact identity. Anything but one is
        /// ambiguous, and ambiguity is not evidence.
        var identityMatches: Int
        var obligations: [CloseabilityReason]
        var attestation: ClosureAttestation?
        var identity: SessionWorkIdentity
        var activityGeneration: Int
        var obligationGeneration: Int

        init(terminalState: SessionState, identity: SessionWorkIdentity,
             identityBound: Bool = true, inventoryComplete: Bool = true,
             inventoryObservedAt: Date? = Date(timeIntervalSince1970: 1),
             identityMatches: Int = 1, obligations: [CloseabilityReason] = [],
             attestation: ClosureAttestation? = nil, activityGeneration: Int = 0,
             obligationGeneration: Int = 0) {
            self.terminalState = terminalState
            self.identity = identity
            self.identityBound = identityBound
            self.inventoryComplete = inventoryComplete
            self.inventoryObservedAt = inventoryObservedAt
            self.identityMatches = identityMatches
            self.obligations = obligations
            self.attestation = attestation
            self.activityGeneration = activityGeneration
            self.obligationGeneration = obligationGeneration
        }
    }

    struct SessionCloseabilityProjection {
        let state: SessionCloseability
        let reasons: [CloseabilityReason]
        let observedAt: Date
        let sessionGeneration: Int?
        let sourceFreshness: String
        let sourceObservedAt: Date?
        let activityGeneration: Int
        let obligationGeneration: Int
        let version: String
        let provenance: [String]
        let attestationID: String?
        /// The one mover every outstanding reason points at, when there is exactly one. Nil
        /// means several — which is itself the answer, and the reason list is then the whole
        /// story rather than a headline.
        let mover: CloseabilityMover?

        var wire: [String: Any] {
            var out: [String: Any] = [
                "state": state.rawValue,
                "reasons": reasons.map(\.wire),
                "observed_at": Int(observedAt.timeIntervalSince1970),
                "activity_generation": activityGeneration,
                "obligation_generation": obligationGeneration,
                "version": version,
                "provenance": provenance,
            ]
            out["source"] = [
                "provenance": "session_watch",
                "freshness": sourceFreshness,
                "observed_at": sourceObservedAt.map {
                    Int($0.timeIntervalSince1970)
                } ?? NSNull(),
                "max_age_seconds": Int(closeabilityInventoryMaxAge),
            ]
            out["session_generation"] = sessionGeneration as Any? ?? NSNull()
            out["attestation_id"] = attestationID as Any? ?? NSNull()
            out["mover"] = mover?.wire as Any? ?? NSNull()
            return out
        }
    }

    /// Is this attestation still the current process's, and still about the current clocks?
    static func closureAttestationIsCurrent(_ attestation: ClosureAttestation,
                                            identity: SessionWorkIdentity,
                                            activityGeneration: Int,
                                            obligationGeneration: Int) -> Bool {
        recordedIdentityMatchesCurrentSession(attestation.identity, identity: identity)
            && attestation.activityGeneration == activityGeneration
            && attestation.obligationGeneration == obligationGeneration
    }

    /// The whole precedence, pure.
    ///
    /// **`unknown` outranks `blocked`, and that ordering is the point.** A stale, missing or
    /// ambiguous source does not merely add a row to the obligation list — it makes the list's
    /// *completeness* unknown, and a reader handed an incomplete list reads it as a checklist.
    /// So doubt about the evidence is reported as doubt, with whatever positive obligations were
    /// seen still listed underneath, rather than as a confident short list of two things to do.
    static func projectCloseability(_ input: CloseabilityInput,
                                    now: Date = Date()) -> SessionCloseabilityProjection {
        var evidence: [CloseabilityReason] = []
        if !input.identityBound {
            evidence.append(CloseabilityReason(.sessionIdentityUnbound, mover: .broker))
        }
        let inventoryAge = input.inventoryObservedAt.map { now.timeIntervalSince($0) }
        let inventoryOverAge = inventoryAge.map {
            $0 < 0 || $0 > closeabilityInventoryMaxAge
        } ?? false
        if input.inventoryObservedAt == nil {
            evidence.append(CloseabilityReason(.sessionInventoryMissing, mover: .broker))
        } else if !input.inventoryComplete || inventoryOverAge {
            evidence.append(CloseabilityReason(.sessionInventoryStale, mover: .broker))
        }
        if input.identityMatches != 1 {
            evidence.append(CloseabilityReason(
                .sessionIdentityAmbiguous, subjectKind: "session",
                subjectID: input.identity.terminalID, mover: .broker))
        }
        if input.terminalState == .unknown {
            evidence.append(CloseabilityReason(
                .terminalUnreadable, subjectKind: "session",
                subjectID: input.identity.terminalID, mover: .broker))
        }

        var obligations = input.obligations
        switch input.terminalState {
        case .working:
            obligations.insert(CloseabilityReason(
                .terminalWorking, subjectKind: "session",
                subjectID: input.identity.terminalID, mover: .thisSession), at: 0)
        case .waiting:
            obligations.insert(CloseabilityReason(
                .terminalWaitingYou, subjectKind: "session",
                subjectID: input.identity.terminalID, mover: .person), at: 0)
        case .idle, .unknown:
            break
        }

        var attestationReasons: [CloseabilityReason] = []
        var attestationID: String?
        var provenance = ["broker"]
        if evidence.isEmpty, obligations.isEmpty {
            if let attestation = input.attestation,
               recordedIdentityMatchesCurrentSession(attestation.identity,
                                                     identity: input.identity) {
                provenance.append("self")
                if closureAttestationIsCurrent(
                    attestation, identity: input.identity,
                    activityGeneration: input.activityGeneration,
                    obligationGeneration: input.obligationGeneration) {
                    attestationID = attestation.id
                } else {
                    attestationReasons.append(CloseabilityReason(
                        .attestationSuperseded, subjectKind: "attestation",
                        subjectID: attestation.id, mover: .thisSession))
                }
            } else {
                attestationReasons.append(CloseabilityReason(
                    .attestationMissing, subjectKind: "session",
                    subjectID: input.identity.terminalID, mover: .thisSession))
            }
        } else if let attestation = input.attestation,
                  recordedIdentityMatchesCurrentSession(attestation.identity,
                                                        identity: input.identity) {
            // A session may say it is clear while the broker still sees work. Recording that it
            // said so is useful; letting it decide is not.
            provenance.append("self")
        }

        let state: SessionCloseability
        if !evidence.isEmpty { state = .unknown }
        else if !obligations.isEmpty { state = .blocked }
        else if attestationID != nil { state = .safe }
        else { state = .needsAttestation }

        let reasons = evidence + obligations + attestationReasons
        let unique = reasons.map(\.mover).reduce(into: [CloseabilityMover]()) { seen, mover in
            if !seen.contains(mover) { seen.append(mover) }
        }
        let freshness: String
        if input.inventoryObservedAt == nil { freshness = "missing" }
        else { freshness = input.inventoryComplete && !inventoryOverAge ? "current" : "stale" }
        return SessionCloseabilityProjection(
            state: state, reasons: reasons, observedAt: now,
            sessionGeneration: nil, sourceFreshness: freshness,
            sourceObservedAt: input.inventoryObservedAt,
            activityGeneration: input.activityGeneration,
            obligationGeneration: input.obligationGeneration,
            version: closeabilityVersion(
                identity: input.identity, activityGeneration: input.activityGeneration,
                obligationGeneration: input.obligationGeneration, state: state),
            provenance: provenance, attestationID: attestationID,
            mover: unique.count == 1 ? unique[0] : nil)
    }

    /// Every positive obligation the broker can prove for one Session, from records alone.
    ///
    /// Pure, and given its inputs rather than reading the registry, so the whole table can be
    /// exercised without seeding one. Nothing here stats a filesystem or reads a screen: a
    /// worktree is dirty because the record says a reading found it dirty, and a claim was
    /// touched because the terminal-state audit wrote that down at the time.
    static func closeabilityObligations(identity: SessionWorkIdentity,
                                        tasks: [Task],
                                        waits: [CoordinationWait],
                                        handoffs: [HandoffEnvelope],
                                        owed: OwedDebt?) -> [CloseabilityReason] {
        var out: [CloseabilityReason] = []
        let ownTask = taskForCurrentSession(tasks, identity: identity)
        if let ownTask, !ownTask.state.isTerminal {
            out.append(CloseabilityReason(.ownTaskUnfinished, subjectKind: "task",
                                          subjectID: ownTask.id, mover: .thisSession))
        }

        func dispatchedByThisSession(_ task: Task) -> Bool {
            if let ownTask, task.parentTaskId == ownTask.id { return true }
            guard let conversation = identity.conversationID,
                  task.rootSessionId == conversation,
                  let rootAssistant = task.rootAssistant,
                  rootAssistant == identity.assistant else { return false }
            return true
        }

        for task in tasks.sorted(by: { $0.created < $1.created }) {
            let mine = dispatchedByThisSession(task)
            if mine, !task.state.isTerminal {
                out.append(CloseabilityReason(
                    .liveDescendantTask, subjectKind: "task", subjectID: task.id,
                    mover: task.childTerminalId.map { CloseabilityMover.otherSession($0) }
                        ?? .task(task.id)))
                continue
            }
            // Once an executor's own task is terminal, its result, landing and isolated bytes
            // are obligations of the dispatching root. A child cannot land and must never be
            // told that `this session` can move the root's row.
            guard mine else { continue }
            let landingClosed = task.landing.map {
                $0.state == .landed || $0.state == .abandoned
            } ?? false
            if task.state.isTerminal, task.resultVerifiedAt == nil, task.summary == nil,
               !landingClosed {
                out.append(CloseabilityReason(.taskWithoutResult, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if task.landing?.state == .pending {
                out.append(CloseabilityReason(.pendingLandingOwned, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if let delivery = task.completionDelivery, delivery.state != .acknowledged, mine {
                out.append(CloseabilityReason(.completionUndelivered, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if task.worktree?.dirty == true, !landingClosed {
                out.append(CloseabilityReason(.dirtyIsolatedWorktree, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if !task.claims.isEmpty, task.state.isTerminal, !landingClosed,
               Set(task.untouchedClaims) != Set(task.claims) {
                out.append(CloseabilityReason(.touchedClaimsWithoutClosure, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
        }

        for wait in waits.sorted(by: { $0.created < $1.created }) {
            let pending = wait.waiters.filter { $0.releaseDeliveredAt == nil }
            guard !pending.isEmpty else { continue }
            if wait.ownerSessionID == identity.terminalID {
                out.append(CloseabilityReason(.coordinationWaitOwned, subjectKind: "wait",
                                              subjectID: wait.id, mover: .thisSession))
            } else if pending.contains(where: { $0.sessionID == identity.terminalID }) {
                out.append(CloseabilityReason(
                    .coordinationWaitWaiting, subjectKind: "wait", subjectID: wait.id,
                    mover: .otherSession(wait.ownerSessionID)))
            }
        }

        for handoff in handoffs.sorted(by: { $0.created < $1.created })
        where handoff.state != .delivered
            && handoffSource(handoff.fromSession, matches: identity) {
            out.append(CloseabilityReason(.openHandoff, subjectKind: "handoff",
                                          subjectID: handoff.id, mover: .thisSession))
        }

        if let owed {
            out.append(CloseabilityReason(
                .owedDecision, subjectKind: "session", subjectID: identity.terminalID,
                mover: owed.personNeeded ? .person : .thisSession))
        }
        return out
    }

    /// One immutable registry reading shared by every Session projected in an HTTP response.
    /// Building it settles the machine-wide clock and copies the three record collections once,
    /// so a polled list is O(registry + sessions) rather than O(registry × sessions).
    struct CloseabilityRegistrySnapshot {
        let tasks: [Task]
        let waits: [CoordinationWait]
        let handoffs: [HandoffEnvelope]
        let selfStates: [String: SessionSelfState]
        let attestations: [String: ClosureAttestation]
        let activityGenerations: [String: Int]
        let obligationGeneration: Int
    }

    static func closeabilityRegistrySnapshot() -> CloseabilityRegistrySnapshot {
        load()
        lock.lock()
        settleObligationGenerationLocked()
        closeabilityRegistryReadCountForTesting += 1
        let snapshot = CloseabilityRegistrySnapshot(
            tasks: Array(tasks.values), waits: Array(coordinationWaits.values),
            handoffs: Array(handoffs.values), selfStates: sessionSelfStates,
            attestations: closureAttestations,
            activityGenerations: sessionActivityGenerations,
            obligationGeneration: obligationGeneration)
        lock.unlock()
        return snapshot
    }

    /// The broker-side projection for one live Session. `identityMatches` is supplied by the
    /// caller because only it holds the inventory the ambiguity is about. List callers pass one
    /// shared registry snapshot; a single-row gate takes a fresh one here.
    static func sessionCloseability(identity: SessionWorkIdentity,
                                    terminalState: SessionState,
                                    inventoryComplete: Bool = true,
                                    inventoryObservedAt: Date? = Date(),
                                    inventoryGeneration: Int? = nil,
                                    identityMatches: Int = 1,
                                    registrySnapshot suppliedSnapshot:
                                        CloseabilityRegistrySnapshot? = nil,
                                    now: Date = Date()) -> SessionCloseabilityProjection {
        let snapshot = suppliedSnapshot ?? closeabilityRegistrySnapshot()
        let input = closeabilityInput(
            identity: identity, terminalState: terminalState,
            inventoryComplete: inventoryComplete, inventoryObservedAt: inventoryObservedAt,
            identityMatches: identityMatches, snapshot: snapshot)
        let projected = projectCloseability(input, now: now)
        return SessionCloseabilityProjection(
            state: projected.state, reasons: projected.reasons, observedAt: projected.observedAt,
            sessionGeneration: inventoryGeneration, sourceFreshness: projected.sourceFreshness,
            sourceObservedAt: projected.sourceObservedAt,
            activityGeneration: projected.activityGeneration,
            obligationGeneration: projected.obligationGeneration, version: projected.version,
            provenance: projected.provenance, attestationID: projected.attestationID,
            mover: projected.mover)
    }

    private static func closeabilityInput(identity: SessionWorkIdentity,
                                          terminalState: SessionState,
                                          inventoryComplete: Bool,
                                          inventoryObservedAt: Date?,
                                          identityMatches: Int,
                                          snapshot: CloseabilityRegistrySnapshot)
        -> CloseabilityInput {
        let bound = identity.assistant != nil && identity.pid != nil
            && identity.processStart != nil && identity.conversationID != nil
        let selfState = snapshot.selfStates[identity.terminalID].flatMap {
            recordedIdentityMatchesCurrentSession($0.identity, identity: identity) ? $0 : nil
        }
        let obligations = closeabilityObligations(
            identity: identity, tasks: snapshot.tasks,
            waits: snapshot.waits, handoffs: snapshot.handoffs,
            owed: selfState?.owed)
        return CloseabilityInput(
            terminalState: terminalState, identity: identity, identityBound: bound,
            inventoryComplete: inventoryComplete, inventoryObservedAt: inventoryObservedAt,
            identityMatches: identityMatches, obligations: obligations,
            attestation: snapshot.attestations[identity.terminalID],
            activityGeneration: snapshot.activityGenerations[identity.terminalID] ?? 0,
            obligationGeneration: snapshot.obligationGeneration)
    }

    /// `POST /v1/orchestrator/sessions/:id/closure`.
    ///
    /// **Not gated on the terminal displaying `working` in the same millisecond.** That
    /// requirement is a race the declaring session cannot win: it is writing the request at the
    /// end of its turn, and whether a screen reading landed on the same beat is not evidence
    /// about anything. What binds this to one turn instead is `activity_generation` — the
    /// caller names the turn it is speaking for, and a stale number is refused by name.
    static func attestClosure(identity: SessionWorkIdentity,
                              status rawStatus: String,
                              activityGeneration claimedGeneration: Int?,
                              note rawNote: String?,
                              auditID rawAuditID: String?,
                              now: Date = Date()) -> Reply {
        load()
        guard identity.assistant != nil, identity.pid != nil, identity.processStart != nil,
              identity.conversationID != nil else {
            return .refused(409, "session_unbound",
                            "The current assistant process and conversation could not be bound.")
        }
        guard rawStatus == "clear" else {
            return .refused(422, "closure_status_unsupported",
                            "status must be \"clear\". A session that still owes something says "
                                + "so by leaving the obligation where the broker can see it.")
        }
        guard case .some(let note) = closureNote(rawNote) else {
            return .refused(400, "bad_request",
                            "note must be one line of 1–\(closureNoteLimit) characters.")
        }
        var auditID: String?
        if let rawAuditID {
            let trimmed = rawAuditID.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty, trimmed.count <= closureAuditIDLimit,
                  !trimmed.contains("\n") else {
                return .refused(400, "bad_request",
                                "audit_id must be one line of 1–\(closureAuditIDLimit) "
                                    + "characters.")
            }
            auditID = trimmed
        }

        lock.lock()
        settleObligationGenerationLocked()
        let currentActivity = sessionActivityGenerations[identity.terminalID] ?? 0
        guard let claimedGeneration, claimedGeneration == currentActivity else {
            lock.unlock()
            return .refused(status: 409, code: "closure_generation_stale",
                            message: "activity_generation must name this turn. The broker's "
                                + "current value is \(currentActivity).",
                            extra: ["activity_generation": currentActivity])
        }
        let currentObligation = obligationGeneration
        if let existing = closureAttestations[identity.terminalID],
           closureAttestationIsCurrent(existing, identity: identity,
                                       activityGeneration: currentActivity,
                                       obligationGeneration: currentObligation) {
            lock.unlock()
            return .ok(["ok": true, "created": false, "attestation_id": existing.id])
        }
        let made = ClosureAttestation(
            id: UUID().uuidString.lowercased(), identity: identity,
            activityGeneration: currentActivity, obligationGeneration: currentObligation,
            note: note, auditID: auditID, created: now)
        closureAttestations[identity.terminalID] = made
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.session.closure", [
            "session": identity.terminalID, "attestation": made.id,
            "activity": String(currentActivity), "obligation": String(currentObligation),
            "state": "recorded",
        ])
        return .ok(["ok": true, "created": true, "attestation_id": made.id])
    }

    static func stored(_ attestation: ClosureAttestation) -> [String: Any] {
        var out: [String: Any] = [
            "id": attestation.id,
            "terminal_id": attestation.identity.terminalID,
            "tty": attestation.identity.tty,
            "activity_generation": attestation.activityGeneration,
            "obligation_generation": attestation.obligationGeneration,
            "created": attestation.created.timeIntervalSince1970,
        ]
        if let assistant = attestation.identity.assistant {
            out["assistant"] = assistant.rawValue
        }
        if let pid = attestation.identity.pid { out["pid"] = Int(pid) }
        if let start = attestation.identity.processStart {
            out["process_start"] = start.timeIntervalSince1970
        }
        if let conversation = attestation.identity.conversationID {
            out["conversation_id"] = conversation
        }
        if let note = attestation.note { out["note"] = note }
        if let auditID = attestation.auditID { out["audit_id"] = auditID }
        return out
    }

    static func closureAttestation(from obj: [String: Any]) -> ClosureAttestation? {
        guard let id = obj["id"] as? String, !id.isEmpty, id.count <= 128,
              let terminalID = obj["terminal_id"] as? String, !terminalID.isEmpty,
              terminalID.count <= 512,
              let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName),
              let tty = obj["tty"] as? String, !tty.isEmpty, tty.count <= 512,
              let pidValue = obj["pid"] as? Int, let pid = Int32(exactly: pidValue),
              let processStart = obj["process_start"] as? Double,
              let conversation = obj["conversation_id"] as? String,
              !conversation.isEmpty, conversation.count <= 512,
              let activity = obj["activity_generation"] as? Int, activity >= 0,
              let obligation = obj["obligation_generation"] as? Int, obligation >= 0,
              let created = obj["created"] as? Double else { return nil }
        let note = obj["note"] as? String
        if let note, note.isEmpty || note.count > closureNoteLimit { return nil }
        let auditID = obj["audit_id"] as? String
        if let auditID, auditID.isEmpty || auditID.count > closureAuditIDLimit { return nil }
        return ClosureAttestation(
            id: id,
            identity: SessionWorkIdentity(
                terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
                processStart: Date(timeIntervalSince1970: processStart),
                conversationID: conversation),
            activityGeneration: activity, obligationGeneration: obligation,
            note: note, auditID: auditID, created: Date(timeIntervalSince1970: created))
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
        return OrchestratorRegistry.withTransaction { $0.title(forTerminal: id) }
    }

    /// Where that terminal sits in the tree. Nil for every session a person opened themselves.
    ///
    /// `load()` first, because a role that is briefly missing is a child mistaken for a person —
    /// which is the one wrong answer this whole arrangement exists to avoid.
    static func role(forTerminal id: String) -> Role? {
        load()
        return OrchestratorRegistry.withTransaction { $0.role(forTerminal: id) }
    }

    /// The task record a terminal's spend should be filed under, for ``UsageLedger``. Nil for a
    /// session a person opened themselves, which the ledger files as `manual`.
    ///
    /// The record rather than the ``Role``: the ledger stores what the work *was* — its kind, its
    /// isolation, how many paths it claimed, which schedule made it — and a role carries none of
    /// that. Attachment is treated the way ``reindex()`` treats it: a guest task owns the session
    /// only while it is live, because a standing session wearing a finished task's name is the
    /// one shape it must not have.
    static func ledgerTaskRecord(forTerminal id: String) -> [String: Any]? {
        lock.lock(); defer { lock.unlock() }
        let candidates = tasks.values.filter { $0.childTerminalId == id }
        if let live = candidates.first(where: { !$0.state.isTerminal }) {
            return ledgerRecord(of: live)
        }
        let owned = candidates.filter { $0.attachSessionId == nil }
            .sorted { $0.created > $1.created }
        return owned.first.map(ledgerRecord)
    }

    /// Every task the registry still holds, in its own spelling, for the ledger's backfill.
    ///
    /// The registry keeps 200 rows and task directories are swept a day after they finish, so
    /// this is free evidence that would otherwise age out. Running it again is safe, and every
    /// word of that is the ledger's side of the bargain rather than this function's: the delta
    /// against a session's cursor is zero for a record already attributed, a record that has
    /// never had a session produces no row at all, and a correction that says nothing new is not
    /// written. Handing over the queued half of the registry is therefore not a mistake here —
    /// `UsageLedger.importTaskRecords(_:)` is where "was anything actually observed" is decided,
    /// once, for both this and the finalize collector.
    static func ledgerBackfillRecords() -> [[String: Any]] {
        load()
        lock.lock(); defer { lock.unlock() }
        return tasks.values.sorted { $0.created < $1.created }.map(ledgerRecord)
    }

    /// The stored record, minus the two fields that are credentials rather than facts about the
    /// work. Nothing outside this file needs either, and the ledger is a durable store.
    private static func ledgerRecord(of task: Task) -> [String: Any] {
        var out = OrchestratorStore.stored(task)
        out.removeValue(forKey: "queued_secret")
        out.removeValue(forKey: "secret_hash")
        return out
    }

    /// Under the lock.
    private static func reindex() {
        OrchestratorRegistry.withTransactionOnHeldLock { registry in
            var found = registry.handoffTitles()
            // The durable half of the same answer, and the only half a fresh process has: the
            // map above is written when a tab opens and is empty after a restart. Read before
            // the assignment and task rows below, so the precedence between the three sources is
            // exactly what it was — this changes when a handoff label is *known*, never where it
            // ranks against anything else.
            //
            // Two unsuppressed labels on one terminal id are two answers to a question that has
            // one, and dictionary iteration order is not a tie-break. `rootAssignmentSession-
            // Projection` refuses the same class of question with `matches.count == 1`; this
            // refuses per terminal, so an ambiguous tab keeps whatever name it would have had
            // without any handoff label rather than one of the two at random.
            var labelsByTerminal: [String: [String]] = [:]
            for label in handoffLabels.values
            where !registry.isHandoffLabelSuppressed(label.handoffID) {
                labelsByTerminal[label.identity.terminalID, default: []].append(label.label)
            }
            for (terminal, labels) in labelsByTerminal {
                guard labels.count == 1, let only = labels.first else { continue }
                found[terminal] = only
            }
            var roles: [String: Role] = [:]
            // A Feature Root receives a label, never a Role. `Role` is the child-lineage type and
            // putting an assignment in it would make a fourth primitive a disguised task.
            for assignment in rootAssignments.values {
                guard let terminal = assignment.identity?.terminalID,
                      ![.failed, .inactive].contains(assignment.state),
                      !registry.isRootAssignmentLabelSuppressed(assignment.id) else { continue }
                found[terminal] = assignment.label
            }
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
            registry.setTerminalProjection(titles: found, roles: roles)
        }
    }

    /// Handoff labels are transient UI state. Keep one while its tab is visible or its first line
    /// is still in flight; once a closed tab disappears from the reading, its reusable id must
    /// not carry the old root's label into a later session.
    ///
    /// `inventoryComplete` is the durable half's evidence test, and it is separate from
    /// `identities` because production never passes nil: ``beat(fromTimer:)`` hands over an array
    /// that is *empty* until the first scan publishes, and ``SessionWatch/InventoryPublication``
    /// starts at `targets: [], complete: false`. Read as a reading, that empty array says every
    /// tab on this Mac has closed, and every durable label would be suppressed on the beats
    /// before the first scan — with `cleanup()` wired to delete exactly what is suppressed. An
    /// unfinished reading is no evidence either way; a closed tab is one a *complete* reading
    /// looked for and did not find. An empty complete reading is still a reading, and does
    /// suppress: that is the case the reclaim path exists for.
    static func pruneClosedHandoffTitles(visible: Set<String>,
                                         identities: [SessionWorkIdentity]? = nil,
                                         inventoryComplete: Bool = true) {
        let delivering = Set(handoffDeliveries.values.map(\.terminalID))
        OrchestratorRegistry.withTransactionOnHeldLock { registry in
            registry.setHandoffTitles(registry.handoffTitles().filter {
                visible.contains($0.key) || delivering.contains($0.key)
            })
            if let identities {
                for assignment in rootAssignments.values
                    where ![.failed, .inactive].contains(assignment.state) {
                    guard let stored = assignment.identity else { continue }
                    if identities.contains(where: {
                        rootAssignmentIdentityMatches(stored, observed: $0)
                    }) {
                        registry.unsuppressRootAssignmentLabel(assignment.id)
                    } else {
                        registry.suppressRootAssignmentLabel(assignment.id)
                    }
                }
                // A durable handoff label answers the same question and therefore takes the same
                // answer — but it is the only one of the two whose suppression is wired to
                // deletion, so it also asks whether the reading is finished. An absent or
                // unfinished reading is no evidence either way and leaves the suppression alone;
                // an absent reading is not a closed tab.
                if inventoryComplete {
                    for label in handoffLabels.values {
                        if identities.contains(where: {
                            rootAssignmentIdentityMatches(label.identity, observed: $0)
                        }) {
                            registry.unsuppressHandoffLabel(label.handoffID)
                        } else {
                            registry.suppressHandoffLabel(label.handoffID)
                        }
                    }
                }
            }
        }
        reindex()
    }

    /// Complete a durable handoff label's identity from the first inventory that can see the
    /// process, so that a terminal id stops being the whole of what the label is bound to.
    ///
    /// A tab is opened before anything is known about what will run in it, so the record written
    /// at ``handoff(_:start:)`` carries a terminal id and an assistant and nothing else — and a
    /// terminal id is reused the moment that tab closes and another opens in its place. This is
    /// the cheap form of the first-identity reconciliation
    /// ``reconcileRootAssignment(_:snapshot:identities:)`` does: only a complete inventory is
    /// believed, only the exact terminal the handoff was delivered into may seed the record, and
    /// the process tuple is adopted whole, so the stored identity is either the opening record or
    /// one real process and never a mixture of two.
    ///
    /// **Two questions, not one.** *Has this record been bound to a process at all?* is what may
    /// adopt a new process tuple, and it is the question the sibling above asks —
    /// `stored.pid == nil || stored.processStart == nil`. *Is a field still missing?* is a
    /// different and much weaker one: a Claude tab has a pid before its transcript has a name, so
    /// a bound record routinely has no conversation id yet, and `onThatTab` filters on terminal
    /// id and assistant alone. Asking only the second would re-seed such a record onto whatever
    /// single process is in that tab now — and a terminal id is reused the moment its tab closes.
    /// So a bound record may only have its conversation id filled in, and only by the process it
    /// is already bound to.
    ///
    /// Under the lock. True when a record changed and the store owes a write.
    private static func adoptHandoffLabelIdentitiesLocked(
        snapshot: SessionWatch.IdentitySnapshot, identities: [SessionWorkIdentity]) -> Bool {
        guard snapshot.complete else { return false }
        var changed = false
        for id in Array(handoffLabels.keys) {
            guard let label = handoffLabels[id] else { continue }
            let bound = label.identity.pid != nil && label.identity.processStart != nil
            guard !bound || label.identity.conversationID == nil else { continue }
            let onThatTab = identities.filter {
                $0.terminalID == label.identity.terminalID
                    && $0.assistant == label.identity.assistant
            }
            guard onThatTab.count == 1, let only = onThatTab.first,
                  let pid = only.pid, let start = only.processStart else { continue }
            // An already-bound record accepts nothing but the conversation id, from the same
            // process: a different pid or a different start is a stranger in a reused tab.
            if bound {
                guard label.identity.pid == pid,
                      label.identity.processStart == start.timeIntervalSince1970 else { continue }
            }
            let adopted = RootAssignmentIdentity(
                terminalID: only.terminalID, assistant: label.identity.assistant, tty: only.tty,
                pid: pid, processStart: start.timeIntervalSince1970,
                conversationID: only.conversationID)
            guard adopted != label.identity else { continue }
            handoffLabels[id] = HandoffLabel(handoffID: label.handoffID, label: label.label,
                                             identity: adopted)
            changed = true
        }
        return changed
    }

    /// Commit a value copy only while the record is still the state the caller worked from.
    /// The monotonic check is the second belt: callers without a narrow expectation still cannot
    /// turn `.briefed` back into `.spawning`, or a terminal result back into a live task.
    @discardableResult
    static func replaceTask(_ candidate: Task, expecting expected: State? = nil,
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

    // MARK: - Reading the task a root wrote — the three readings that hold the registry

    // The pure half of this section is `OrchestratorDraft`: the draft a dispatch body decodes
    // into, the four ingress refusals, the worktree lifecycle, and the claim and workspace scans
    // over a table of tasks handed to them. What is left here are the three declarations that
    // fail that file's mechanical test, because each one reads `tasks` — two under the
    // `…Locked()` contract, one taking `lock` itself. Ownership stays with the owner.

    private static func serializeBlockersLocked(for candidate: Task) -> [Task] {
        OrchestratorDraft.serializeBlockers(for: candidate, among: Array(tasks.values))
    }

    private static func claimsOverlapsLocked(for candidate: Task)
        -> [OrchestratorDraft.ClaimsOverlap] {
        OrchestratorDraft.claimsOverlaps(for: candidate, among: Array(tasks.values))
    }

    private static func workspaceOverlaps(for newTask: Task)
        -> [OrchestratorDraft.WorkspaceOverlap] {
        lock.lock()
        let newRoot = rootKeyLocked(of: newTask)
        let existing = tasks.values.map { (task: $0, rootKey: rootKeyLocked(of: $0)) }
        lock.unlock()
        return OrchestratorDraft.workspaceOverlaps(for: newTask, rootKey: newRoot, among: existing)
    }

    // MARK: - Assistant quota at the dispatch gate — Q1 design §D

    /// `age_seconds` for an `AssistantQuota`, in the identical shape
    /// `OrchestratorDraft.ClaimsOverlap.warning(for:)` and
    /// `OrchestratorDraft.workspaceBusyExtra` already use: an integer number of seconds,
    /// `max(0, now -
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
    /// `OrchestratorDraft.dispatchPayload(record:taskID:overlaps:)` already fills with
    /// `workspace_overlap` and
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
        if envelope.coordinatorPlainHandoff { record["coordinatorPlainHandoff"] = true }
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
    ///
    /// `sender` carries the contract of `POST /v1/orchestrator/handoffs` and of nothing else: a
    /// reader of the machine, called at most once, whose verdict is in
    /// ``handoffSenderVerdict(_:evidence:)``. It is optional because
    /// `POST /v1/orchestrator/coordinator/successions` replays an ordinary handoff through this
    /// same function, and *that* sender is the coordinator by construction — applying the
    /// contract there would refuse the one sequence built to move the role properly. The two
    /// callers are the route, which passes it, and the succession service, which does not.
    ///
    /// It is read after the idempotent replay, deliberately. A `handoff_id` already in the
    /// registry is a repeat of a decision rather than a new one: the tab exists, the sender was
    /// proved when the envelope was written, and refusing a retry after a dropped connection
    /// would cost a delivery that has already happened.
    static func handoff(_ obj: [String: Any],
                        sender: (() -> HandoffSenderEvidence)? = nil,
                        start: HandoffStarter = { place, assistant, model, addDir in
                            StartPoints.start(place, assistant: assistant, model: model,
                                              addDir: addDir)
                        }) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled",
                            "Task dispatch is switched off in Settings.")
        }
        guard let id = obj["handoff_id"] as? String, OrchestratorDraft.isTaskID(id) else {
            return .refused(422, "bad_task", "handoff_id must be a lowercase UUID.")
        }
        if let existing = heldHandoff(id) { return successfulHandoffReply(for: existing) }
        if let sender, let refusal = handoffSenderVerdict(obj, evidence: sender()).refusal {
            return refusal
        }
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
                                       coordinatorPlainHandoff: draft.coordinatorPlainHandoff,
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
        case .started(let terminalID, let backend, _):
            let delivery = HandoffDelivery(id: id, assistant: draft.assistant,
                                           model: draft.model, terminalID: terminalID,
                                           backend: backend, spawnedAt: Date())
            lock.lock()
            handoffDeliveries[id] = delivery
            // Only a title the sender actually supplied earns a durable record. `place.label`
            // falls back to `handoff <first eight of the id>`, which says less about the work
            // than the name the conversation will generate for itself, so storing that would
            // cost the tab a better name at every restart from here to the sweep. The iTerm
            // place label is unchanged either way; this is about what outlives the process.
            if let title = draft.title {
                handoffLabels[id] = HandoffLabel(
                    handoffID: id, label: title,
                    identity: RootAssignmentIdentity(
                        terminalID: terminalID, assistant: draft.assistant, tty: nil, pid: nil,
                        processStart: nil, conversationID: nil))
            }
            OrchestratorRegistry.withTransactionOnHeldLock {
                $0.setHandoffTitle(place.label, forTerminal: terminalID)
            }
            reindex()
            lock.unlock()
            if draft.title != nil { save() }
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return successfulHandoffReply(for: envelope, draft: draft,
                                          terminalID: terminalID, backend: backend)
        }
    }

    // MARK: - Root Assignment / Feature Launch

    static func rootAssignmentDraft(
        from obj: [String: Any],
        isDirectory: (String) -> Bool = StartPoints.isDirectory,
        canonicalize: (String) -> String = OrchestratorDraft.canonicalFilesystemPath)
        -> RootAssignmentDraftOutcome {
        let topKeys: Set<String> = ["request_id", "assistant", "model", "project_dir",
                                    "label", "assignment"]
        guard Set(obj.keys) == topKeys else {
            return .bad("request must contain only request_id, assistant, model, project_dir, "
                      + "label, and assignment")
        }
        guard let requestID = obj["request_id"] as? String,
              OrchestratorDraft.isTaskID(requestID) else {
            return .bad("request_id must be a lowercase UUID")
        }
        guard let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName) else {
            return .bad("assistant must be claude or codex")
        }
        guard let model = obj["model"] as? String,
              model == "default" || StartPoints.modelName(model) == model else {
            return .bad("model must be default or a valid model name")
        }
        guard let rawProject = obj["project_dir"] as? String, StartPoints.usable(rawProject),
              isDirectory(rawProject) else {
            return .bad("project_dir must be an existing absolute directory")
        }
        let projectDir = canonicalize(rawProject)
        guard StartPoints.usable(projectDir), isDirectory(projectDir) else {
            return .bad("project_dir could not be resolved to a durable canonical directory")
        }
        guard let label = obj["label"] as? String,
              !label.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              label.utf8.count <= 200 else {
            return .bad("label must be 1–200 UTF-8 bytes")
        }
        guard let fields = obj["assignment"] as? [String: Any] else {
            return .bad("assignment must be an object")
        }
        let fieldKeys: Set<String> = ["objective", "scope", "constraints",
                                      "relevant_references", "acceptance"]
        guard Set(fields.keys) == fieldKeys else {
            return .bad("assignment must contain only objective, scope, constraints, "
                      + "relevant_references, and acceptance")
        }
        func field(_ key: String) -> String? {
            guard let value = fields[key] as? String,
                  !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  value.utf8.count <= 8_192,
                  !value.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
            return value
        }
        guard let objective = field("objective"), let scope = field("scope"),
              let constraints = field("constraints"),
              let references = field("relevant_references"),
              let acceptance = field("acceptance"),
              [objective, scope, constraints, references, acceptance]
                .reduce(0, { $0 + $1.utf8.count }) <= 32_768 else {
            return .bad("each assignment field must be 1–8192 UTF-8 bytes and together at most "
                      + "32768 bytes")
        }
        return .ok(RootAssignmentDraft(
            requestID: requestID, assistant: assistant, model: model,
            projectDir: projectDir, label: label, objective: objective, scope: scope,
            constraints: constraints, relevantReferences: references, acceptance: acceptance))
    }

    private static func rootAssignmentDigest(_ draft: RootAssignmentDraft) -> String {
        let values = [draft.requestID, draft.assistant.rawValue, draft.model, draft.projectDir,
                      draft.label, draft.objective, draft.scope, draft.constraints,
                      draft.relevantReferences, draft.acceptance]
        let canonical = values.map { "\($0.utf8.count):\($0)" }.joined(separator: "|")
        return RemoteAuth.hex(SHA256.hash(data: Data(canonical.utf8)))
    }

    static func rootAssignmentLine(id: String, draft: RootAssignmentDraft,
                                   language: RootAssignmentLanguage?) -> String {
        let languageContract = language.map { "\n\nLANGUAGE CONTRACT\nThe Mac Clawdline interface language was resolved to \($0.name) when this assignment was accepted. Use \($0.name) for every commentary message, progress update, question, final response, and all other user-facing communication in this Root Session, including the very first response you send. This English broker template does not change that language." } ?? ""
        return "You are an independently owned Clawdline Feature Root for Root Assignment \(id). "
          + "Own this feature through implementation, verification, integration, and landing."
          + languageContract + "\n\nOBJECTIVE\n\(draft.objective)\n\nSCOPE\n\(draft.scope)\n\n"
          + "CONSTRAINTS\n\(draft.constraints)\n\nRELEVANT REFERENCES\n"
          + "\(draft.relevantReferences)\n\nACCEPTANCE\n\(draft.acceptance)"
    }

    static func rootAssignmentLine(for assignment: RootAssignment) -> String {
        let draft = RootAssignmentDraft(
            requestID: assignment.requestID, assistant: assignment.assistant,
            model: assignment.model, projectDir: assignment.projectDir, label: assignment.label,
            objective: assignment.objective, scope: assignment.scope, constraints: assignment.constraints,
            relevantReferences: assignment.relevantReferences, acceptance: assignment.acceptance)
        return rootAssignmentLine(id: assignment.id, draft: draft, language: assignment.language)
    }

    /// What one record says about a Root Assignment's delivery: whether the exact briefing line
    /// is a completed user turn, and when that turn happened. The time is the receipt's own event
    /// time, which is the only thing the pre-brief deadline may be compared against.
    struct RootAssignmentTranscriptReceipt: Equatable {
        let recorded: Bool
        let at: Date?
    }

    static func rootAssignmentTranscriptReceipt(_ transcript: String?, assistant: Assistant,
                                                assignmentID: String, line: String)
        -> RootAssignmentTranscriptReceipt {
        let absent = RootAssignmentTranscriptReceipt(recorded: false, at: nil)
        guard let transcript,
              line.hasPrefix("You are an independently owned Clawdline Feature Root for Root "
                           + "Assignment \(assignmentID).") else { return absent }
        // This receipt may first be observed after a busy Root has already written hundreds of
        // newer rows. A UI-tail limit would turn observation lag back into prompt_timeout, so this
        // delivery-only path searches the complete record that its caller has already read.
        guard let turn = Transcript.parse(transcript, assistant: assistant, limit: Int.max)
            .first(where: { $0.kind == .user && $0.text.contains(line) }) else { return absent }
        return RootAssignmentTranscriptReceipt(recorded: true, at: turn.time)
    }

    static func transcriptContainsRootAssignment(_ transcript: String?, assistant: Assistant,
                                                 assignmentID: String, line: String) -> Bool {
        rootAssignmentTranscriptReceipt(transcript, assistant: assistant,
                                        assignmentID: assignmentID, line: line).recorded
    }

    static func rootAssignmentReconciliation(
        stored: RootAssignmentIdentity, candidates: [RootAssignmentIdentity],
        inventoryComplete: Bool, absenceConfirmed: Bool, delivered: Bool)
        -> RootAssignmentReconciliation {
        guard inventoryComplete else { return .wait("stale_inventory") }
        let exact = candidates.filter {
            $0.assistant == stored.assistant && $0.pid == stored.pid
                && $0.processStart == stored.processStart
                && (stored.conversationID == nil || $0.conversationID == stored.conversationID)
        }
        if exact.count > 1 { return .fail("ambiguous_identity") }
        if let only = exact.first { return .rebind(only) }
        guard absenceConfirmed else { return .wait("process_missing_unconfirmed") }
        return delivered ? .inactive("process_lost_after_briefing")
                         : .fail("process_lost_before_briefing")
    }

    static func rootAssignmentInitialIdentityReconciliation(
        candidates: [RootAssignmentIdentity], inventoryComplete: Bool,
        absenceConfirmed: Bool) -> RootAssignmentReconciliation {
        guard inventoryComplete else { return .wait("stale_inventory") }
        if candidates.count > 1 { return .fail("ambiguous_identity") }
        if let only = candidates.first, only.pid != nil, only.processStart != nil {
            return .rebind(only)
        }
        return absenceConfirmed ? .fail("process_lost_before_briefing")
                                : .wait("process_missing_unconfirmed")
    }

    static func rootAssignmentIdentityMatches(_ stored: RootAssignmentIdentity,
                                              observed: SessionWorkIdentity) -> Bool {
        guard stored.terminalID == observed.terminalID,
              observed.assistant == stored.assistant else { return false }
        if let pid = stored.pid, pid != observed.pid { return false }
        if let start = stored.processStart,
           start != observed.processStart?.timeIntervalSince1970 { return false }
        if let conversation = stored.conversationID,
           conversation != observed.conversationID { return false }
        return true
    }

    /// The moment the pre-brief window closes. ``rootAssignmentPromptTimedOut`` asks whether
    /// *now* is past it; a delivery receipt asks whether the *user turn* was. One boundary, read
    /// once against the observer and once against the event, so the two can never drift apart.
    static func rootAssignmentPromptDeadline(openedAt: Date) -> Date {
        openedAt.addingTimeInterval(readyLimit)
    }

    static func rootAssignmentPromptTimedOut(state: RootAssignmentState, openedAt: Date,
                                             now: Date = Date(), briefed: Bool) -> Bool {
        // A person owns this wait. Unlike the ordinary composer-start deadline, workspace trust
        // has no automatic answer and therefore no four-minute expiry.
        state != .blocked && !briefed && now > rootAssignmentPromptDeadline(openedAt: openedAt)
    }

    static func rootAssignmentPromptTimeoutAnchor(terminalOpenedAt: Date,
                                                  trustResumedAt: Date?) -> Date {
        trustResumedAt ?? terminalOpenedAt
    }

    private static func replayRootAssignment(_ existing: RootAssignment, digest: String) -> Reply {
        guard existing.requestDigest == digest else {
            return .refused(409, "request_conflict",
                            "request_id was already accepted with different content.")
        }
        guard existing.state != .failed else {
            return .refused(status: 409, code: "request_terminated",
                message: "That request_id ended in a terminal failure; use a new request_id.",
                extra: ["assignment_id": existing.id,
                        "failure": existing.failure ?? "failed"])
        }
        return .ok(["ok": true, "replayed": true,
                    "root_assignment": rootAssignmentPublicRecord(existing)])
    }

    typealias RootAssignmentStarter = (StartPoints.Place, Assistant, String?)
        -> StartPoints.Outcome

    static func rootAssignment(
        _ obj: [String: Any], idempotencyKey: String?,
        assistantAvailable: (Assistant) -> Bool = { $0.isInstalled },
        // No shipped source is a workspace-trust authority. A future policy adapter may inject a
        // positive answer; recent projects and live sessions are deliberately not treated as one.
        projectApproved: (String) -> Bool = { _ in false },
        start: RootAssignmentStarter = { place, assistant, model in
            StartPoints.start(place, assistant: assistant, model: model)
        }) -> Reply {
        let draft: RootAssignmentDraft
        switch rootAssignmentDraft(from: obj) {
        case .bad(let why): return .refused(422, "bad_root_assignment", why)
        case .ok(let valid): draft = valid
        }
        guard idempotencyKey == draft.requestID else {
            return .refused(422, "idempotency_mismatch",
                            "Idempotency-Key must exactly equal request_id.")
        }
        let digest = rootAssignmentDigest(draft)
        load()
        lock.lock()
        if let existing = rootAssignments.values.first(where: { $0.requestID == draft.requestID }) {
            lock.unlock()
            return replayRootAssignment(existing, digest: digest)
        }
        lock.unlock()
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled",
                            "Root Assignment is switched off in Settings.")
        }
        guard assistantAvailable(draft.assistant) else {
            return .refused(409, "assistant_unavailable",
                            "The selected assistant is not available on this Mac.")
        }
        guard takeDispatchRate() != nil else {
            return .refused(429, "rate_limited", "Too many launches; wait a few minutes.")
        }
        let id = UUID().uuidString.lowercased()
        let now = Date()
        let assignment = RootAssignment(
            id: id, requestID: draft.requestID, requestDigest: digest,
            assistant: draft.assistant, model: draft.model, projectDir: draft.projectDir,
            label: draft.label, objective: draft.objective, scope: draft.scope,
            constraints: draft.constraints, relevantReferences: draft.relevantReferences,
            acceptance: draft.acceptance, projectApproved: projectApproved(draft.projectDir),
            created: now, state: .accepted, language: rootAssignmentLanguage())
        lock.lock()
        if let existing = rootAssignments.values.first(where: { $0.requestID == draft.requestID }) {
            lock.unlock()
            return replayRootAssignment(existing, digest: digest)
        }
        rootAssignments[id] = assignment
        lock.unlock()
        guard save() else {
            lock.lock(); rootAssignments.removeValue(forKey: id); lock.unlock()
            return .refused(503, "persistence_failed",
                            "The accepted assignment could not be durably recorded.")
        }
        let place = StartPoints.Place(id: StartPoints.id(for: draft.projectDir),
                                      path: draft.projectDir, label: draft.label, at: now)
        switch start(place, draft.assistant, draft.model == "default" ? nil : draft.model) {
        case .refused(let status, let code, let message, let app):
            lock.lock()
            var failed = rootAssignments[id] ?? assignment
            failed.state = .failed; failed.failure = code; failed.endedAt = Date()
            rootAssignments[id] = failed
            lock.unlock(); save()
            reportRootAssignmentTransition(id)
            var extra: [String: Any] = ["assignment_id": id]
            if let app { extra["app"] = app }
            return .refused(status: status, code: code, message: message, extra: extra)
        case .started(let terminalID, let backend, _):
            lock.lock()
            var opened = rootAssignments[id] ?? assignment
            opened.state = .terminalOpened
            opened.terminalOpenedAt = Date()
            opened.identity = RootAssignmentIdentity(terminalID: terminalID,
                assistant: draft.assistant, tty: nil, pid: nil, processStart: nil,
                conversationID: nil)
            rootAssignments[id] = opened
            reindex()
            lock.unlock()
            guard save() else {
                lock.lock()
                var lost = rootAssignments[id] ?? opened
                lost.state = .failed; lost.failure = "launch_receipt_lost"
                lost.endedAt = Date(); rootAssignments[id] = lost; reindex()
                lock.unlock()
                if !reportRootAssignmentTransition(id) {
                    RemoteAuth.audit("root_assignment.failed", [
                        "assignment": id, "state": RootAssignmentState.failed.rawValue,
                        "why": "launch_receipt_lost", "terminal": terminalID,
                        "transition": "failed|launch_receipt_lost",
                    ])
                }
                return .refused(status: 503, code: "launch_receipt_lost",
                    message: "The tab opened but its durable terminal receipt was lost.",
                    extra: ["assignment_id": id])
            }
            RemoteAuth.audit("root_assignment.open", ["assignment": id,
                "request": draft.requestID, "terminal": terminalID,
                "backend": backend.rawValue, "assistant": draft.assistant.rawValue])
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return .ok(["ok": true, "replayed": false,
                        "root_assignment": rootAssignmentPublicRecord(opened)])
        }
    }

    // MARK: - Dispatch

    private static func resolveAttachment(sessionID: String, assistant: Assistant,
                                          excluding excludedTaskID: String? = nil)
        -> OrchestratorDraft.AttachmentDecision {
        let inventory: ([TargetSession], [String: SessionState])
        if let supplied = attachmentInventoryForTesting {
            inventory = supplied
        } else {
            inventory = onMain(from: "Orchestrator.resolveAttachment") {
                (SessionWatch.shared.targets, SessionWatch.shared.states)
            }
        }
        lock.lock()
        let live = Array(tasks.values)
        let roles = OrchestratorRegistry.withTransactionOnHeldLock { $0.roles() }
        lock.unlock()
        return OrchestratorDraft.attachmentDecision(sessionID: sessionID, assistant: assistant,
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
                         requireRootSession: Bool = false,
                         allowDetachedAutomation: Bool = false,
                         respawn: RespawnOrigin? = nil) -> Reply {
        guard Config.shared.orchestratorEnabled else {
            return .refused(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
        }
        guard OrchestratorDraft.isTaskID(taskID) else {
            return .refused(422, "bad_task", "task_id must be a lowercase UUID.")
        }
        // Same task again is the same answer again: the root retrying a dispatch that already
        // landed must not spawn a second child.
        if let existing = held(taskID) { return successfulDispatchReply(for: existing) }
        guard OrchestratorDraft.isTaskSecret(secret) else {
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
        var made: OrchestratorDraft.Draft
        switch OrchestratorDraft.draft(from: obj, expecting: taskID) {
        case .bad(let why): return .refused(422, "bad_task", why)
        case .ok(let ok): made = ok
        }
        if requireRootSession || allowDetachedAutomation,
           let refusal = OrchestratorDraft.dispatchDoorRefusal(
                sessionID: made.rootSessionId, pollOnly: made.pollOnly,
                allowDetachedAutomation: allowDetachedAutomation) {
            refundDispatchRate(rateTicket)
            return refusal
        }
        if requireRootSession,
           let refusal = OrchestratorDraft.rootSessionRequirementRefusal(
                sessionID: made.rootSessionId, pollOnly: made.pollOnly) {
            refundDispatchRate(rateTicket)
            return refusal
        }
        if requireRootSession,
           let refusal = OrchestratorDraft.rootAssistantRequirementRefusal(
                sessionID: made.rootSessionId, pollOnly: made.pollOnly,
                assistant: made.rootAssistant) {
            refundDispatchRate(rateTicket)
            return refusal
        }
        // Before the live-session scans below it, and before any git subprocess: this one is
        // decided by the bytes already in hand. A stored schedule template and a respawn carry a
        // body no caller is holding, so both keep the warning instead — see the refusal itself.
        if let refusal = OrchestratorDraft.claimsRequirementRefusal(
                declared: made.claimsDeclared,
                writtenForThisDispatch: schedule == nil && respawn == nil) {
            refundDispatchRate(rateTicket)
            return refusal
        }
        let identityEvidence = rootIdentityEvidenceForTesting
            ?? activeRootIdentityEvidence(claimed: made.rootSessionId)
                + Coordinator.rootIdentityEvidence(claimed: made.rootSessionId)
        if let refusal = OrchestratorDraft.rootIdentityRefusal(claimed: made.rootSessionId,
                                             evidence: identityEvidence) {
            refundDispatchRate(rateTicket)
            return refusal
        }

        let rootBinding = canonicalRootSession(
            made.rootSessionId, assistant: made.rootAssistant,
            among: rootTargets(), sessionID: Transcript.sessionID(of:))
        if requireRootSession, let warning = rootBinding.warning {
            refundDispatchRate(rateTicket)
            return rootBindingRefusal(warning)
        }
        made.rootSessionId = rootBinding.sessionID
        let rootWarnings = rootBinding.warning.map { [$0] } ?? []

        var graphReservation: String?
        defer { if let key = graphReservation { releaseGraphAdmission(key) } }
        if let graph = made.graph,
           let refusal = graphAdmissionRefusal(graph, taskID: taskID, reserve: true) {
            refundDispatchRate(rateTicket)
            return refusal
        }
        graphReservation = made.graph.map(graphAdmissionKey)

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
            switch OrchestratorDraft.prepareWorktree(for: made, taskID: taskID,
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
                        graph: made.graph,
                        serialize: made.serialize, claims: made.claims,
                        claimsDeclared: made.claimsDeclared,
                        secretHash: hash(ofSecret: secret))
        task.repositoryCommonDir = preparedWorktree?.repositoryCommonDir
            ?? OrchestratorDraft.gitCommonDirectory(at: made.projectDir)
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
        worktreeWarnings += OrchestratorLandingQueue.retainLandingPaths(&task)
        task.claimKeys = OrchestratorDraft.freezeClaims(task.claims, projectDir: task.projectDir)
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
                            extra: OrchestratorDraft.workspaceBusyExtra(blocker))
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
                onMain(from: "Orchestrator.dispatch.attachFailure", fail)
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
    /// `spawn_failed` was 34 of 206 dispatches on 2026-08-28, 33 of them Codex — one 200-row
    /// window, not a running total, so a later reading counts a different population and this is
    /// not the current rate. Until this route existed the protocol's answer was that the root must
    /// write the whole `task.json` out again under a fresh id: thirty-four rewrites by the most
    /// context-loaded session in the tree. Two is enough to get past a terminal that would not
    /// open, and few enough that a tab failing for a real reason stops being retried in a loop.
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
        guard OrchestratorDraft.isTaskID(taskID) else {
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
        if let supplied, !OrchestratorDraft.isTaskSecret(supplied) {
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
                                                claimsOverlaps:
                                                    [OrchestratorDraft.ClaimsOverlap]? = nil,
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
        let claimWarnings: [OrchestratorDraft.ClaimsOverlap]
        if let claimsOverlaps {
            claimWarnings = claimsOverlaps
        } else {
            lock.lock()
            claimWarnings = claimsOverlapsLocked(for: task)
            lock.unlock()
        }
        return .ok(OrchestratorDraft.dispatchPayload(
            record: record, taskID: task.id, overlaps: overlaps,
            claimsOverlaps: claimWarnings, additionalWarnings: additionalWarnings))
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
            guard let worktree = OrchestratorDraft.resolveSpawnBase(in: prepared) else {
                task.state = .spawnFailed
                task.summary = "The worktree base no longer resolves to a commit."
                task.finishedAt = Date()
                return task
            }
            task.worktree = worktree
            if let failure = OrchestratorDraft.addWorktree(worktree, taskID: task.id) {
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
        // see `OrchestratorDraft.attachmentDecision`, which refuses a session that was
        // launched without it, and
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
                OrchestratorDraft.disposeWorktree(worktree, taskID: task.id, why: "spawn_failed")
            }
        case .started(let id, let backend, _):
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
            onMain(from: "Orchestrator.startQueuedTaskIfEligible.secretFailure", fail)
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
            onMain(from: "Orchestrator.startQueuedTaskIfEligible.spawnFailure", fail)
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
        /// Test-only preprojection for route fixtures which replace SessionWatch with already
        /// projected Coordinator rows. Production always leaves this nil and derives the state
        /// under the registry lock below.
        let projectedWorkState: SessionWorkState?

        init(identity: SessionWorkIdentity, terminalState: SessionState,
             projectedWorkState: SessionWorkState? = nil) {
            self.identity = identity
            self.terminalState = terminalState
            self.projectedWorkState = projectedWorkState
        }
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
        let pendingLandingRows: [[String: Any]]
        let landingSources: [String: Any]
        let openWaits: Int
    }

    enum LandingOwnershipStatus: String {
        case observedWorking = "observed_working"
        case observedReadyOrHolding = "observed_ready_or_holding"
        case observedOther = "observed_other"
        case taskStillLive = "task_still_live"
        case notObserved = "not_observed"
        case unknown
    }

    private struct LandingSessionFact {
        let identity: SessionWorkIdentity
        let workState: SessionWorkState
    }

    private static func observationSource(provenance: String, freshness: String,
                                          observedAt: Date?, generation: Int? = nil)
        -> [String: Any] {
        var out: [String: Any] = [
            "provenance": provenance, "freshness": freshness,
            "observed_at": observedAt.map { Int($0.timeIntervalSince1970) } as Any? ?? NSNull(),
        ]
        if let generation { out["generation"] = generation }
        return out
    }

    private static func landingSources(sessionsFresh: Bool, sessionsObservedAt: Date?,
                                       sessionsGeneration: Int?, registryObservedAt: Date)
        -> [String: Any] {
        let sessionFreshness = Coordinator.sessionSourceFreshness(
            sessionsFresh: sessionsFresh, observedAt: sessionsObservedAt)
        return [
            "sessions": observationSource(
                provenance: "session_watch", freshness: sessionFreshness,
                observedAt: sessionsObservedAt, generation: sessionsGeneration),
            "tasks": observationSource(
                provenance: "orchestrator_task_registry", freshness: "current",
                observedAt: registryObservedAt),
            "landings": observationSource(
                provenance: "orchestrator_landing_registry", freshness: "current",
                observedAt: registryObservedAt),
        ]
    }

    private static func observedLandingStatus(_ state: SessionWorkState)
        -> LandingOwnershipStatus {
        switch state {
        case .working: return .observedWorking
        case .ready, .holding: return .observedReadyOrHolding
        default: return .observedOther
        }
    }

    /// One landing row's owner/executor projection. Every match uses the same process-bound task
    /// and root tuples as Session work state. An incomplete or timestamp-less inventory can say
    /// only unknown; it can never manufacture offline/dead from absence.
    private static func landingOwnershipRecord(
        task: Task, root: Task, landing: Landing, sessions: [LandingSessionFact],
        sessionsFresh: Bool, sessionsObservedAt: Date?, sessionsGeneration: Int?,
        registryObservedAt: Date
    ) -> [String: Any] {
        let inventoryCurrent = sessionsFresh && sessionsObservedAt != nil
        let rootAssistant = root.rootAssistant
        let executorMatches = sessions.filter {
            taskMatchesCurrentSession(task, identity: $0.identity)
        }
        let rootMatches = sessions.filter { fact in
            guard let rootSession = root.rootSessionId,
                  let assistant = rootAssistant else { return false }
            return fact.identity.assistant == assistant
                && fact.identity.conversationID == rootSession
        }

        let status: LandingOwnershipStatus
        let subject: String
        let observedWorkState: SessionWorkState?
        let reason: String
        if !inventoryCurrent {
            subject = task.state.isTerminal ? "root" : "executor"
            status = .unknown
            observedWorkState = nil
            reason = "session_inventory_incomplete"
        } else if !task.state.isTerminal {
            subject = "executor"
            if executorMatches.count == 1, let match = executorMatches.first {
                status = observedLandingStatus(match.workState)
                observedWorkState = match.workState
                reason = "exact_executor_observation"
            } else if executorMatches.count > 1 {
                status = .unknown
                observedWorkState = nil
                reason = "executor_observation_ambiguous"
            } else {
                status = .taskStillLive
                observedWorkState = nil
                reason = "live_task_without_exact_executor_observation"
            }
        } else {
            subject = "root"
            if rootMatches.count == 1, let match = rootMatches.first {
                status = observedLandingStatus(match.workState)
                observedWorkState = match.workState
                reason = "exact_root_observation"
            } else if rootMatches.count > 1 {
                status = .unknown
                observedWorkState = nil
                reason = "root_observation_ambiguous"
            } else if root.rootSessionId == nil || rootAssistant == nil {
                status = .unknown
                observedWorkState = nil
                reason = "root_identity_missing"
            } else {
                status = .notObserved
                observedWorkState = nil
                reason = "root_absent_from_complete_inventory"
            }
        }

        return [
            "version": 1,
            "status": status.rawValue,
            "subject": subject,
            "reason": reason,
            "task_id": task.id,
            "task_state": task.state.rawValue,
            "root_key": landing.ownerRootKey,
            "root_assistant": rootAssistant?.rawValue as Any? ?? NSNull(),
            "observed_work_state": observedWorkState?.rawValue as Any? ?? NSNull(),
            "evidence": landingSources(
                sessionsFresh: sessionsFresh, sessionsObservedAt: sessionsObservedAt,
                sessionsGeneration: sessionsGeneration,
                registryObservedAt: registryObservedAt),
        ]
    }

    private static func pendingLandingRecordsLocked(
        sessions: [LandingSessionFact], sessionsFresh: Bool, sessionsObservedAt: Date?,
        sessionsGeneration: Int?, registryObservedAt: Date, now: Date
    ) -> [[String: Any]] {
        let indexed = tasks
        return tasks.values.compactMap { task -> [String: Any]? in
            guard let landing = task.landing, landing.state == .pending else { return nil }
            let root = OrchestratorDraft.rootTask(of: task, among: indexed)
            return [
                "id": task.id,
                "title": task.title,
                "root_key": landing.ownerRootKey,
                "root_label": (root.rootLabel ?? task.rootLabel) as Any? ?? NSNull(),
                "paths": task.claims,
                "since": Int(landing.since.timeIntervalSince1970),
                "age_seconds": OrchestratorDraft.ageSeconds(since: landing.since, now: now),
                "target": landing.target as Any? ?? NSNull(),
                "note": landing.note as Any? ?? NSNull(),
                "ownership": landingOwnershipRecord(
                    task: task, root: root, landing: landing, sessions: sessions,
                    sessionsFresh: sessionsFresh, sessionsObservedAt: sessionsObservedAt,
                    sessionsGeneration: sessionsGeneration,
                    registryObservedAt: registryObservedAt),
            ]
        }.sorted { left, right in
            let first = left["since"] as? Int ?? 0
            let second = right["since"] as? Int ?? 0
            if first == second {
                return (left["id"] as? String ?? "") < (right["id"] as? String ?? "")
            }
            return first < second
        }
    }

    /// Build every Orchestrator-derived Bearings fact during one registry lock window. The
    /// SessionWatch inventory was observed separately by the caller; that cross-source boundary
    /// is represented by distinct timestamps/provenance in the response rather than called
    /// transactional.
    static func coordinatorSnapshot(_ observations: [CoordinatorSessionObservation],
                                    sessionsFresh: Bool = true,
                                    sessionsObservedAt: Date? = nil,
                                    sessionsGeneration: Int? = nil,
                                    now: Date = Date()) -> CoordinatorSnapshot {
        load()
        lock.lock(); defer { lock.unlock() }
        let sessionFacts = observations.map { observation -> CoordinatorSessionFacts in
            let work = observation.projectedWorkState.map {
                SessionWorkProjection(state: $0, disposition: nil)
            } ?? sessionWorkProjectionLocked(
                identity: observation.identity, terminalState: observation.terminalState)
            return CoordinatorSessionFacts(
                work: work,
                coordination: coordinationLocked(forTerminal: observation.identity.terminalID))
        }
        let landingSessions = zip(observations, sessionFacts).map {
            LandingSessionFact(identity: $0.0.identity, workState: $0.1.work.state)
        }
        let pendingRows = pendingLandingRecordsLocked(
            sessions: landingSessions, sessionsFresh: sessionsFresh,
            sessionsObservedAt: sessionsObservedAt, sessionsGeneration: sessionsGeneration,
            registryObservedAt: now, now: now)
        return CoordinatorSnapshot(
            observedAt: now,
            sessions: sessionFacts,
            activeTasks: tasks.values.filter { !$0.state.isTerminal }.count,
            pendingLandings: pendingRows.count,
            pendingLandingRows: pendingRows,
            landingSources: landingSources(
                sessionsFresh: sessionsFresh, sessionsObservedAt: sessionsObservedAt,
                sessionsGeneration: sessionsGeneration, registryObservedAt: now),
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

    static let notifyTaskLimit = 5
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
        // Under no push preference of its own: orchestratorAgentNotify already gates
        // agent-authored content, and this is not an automatic completion notice.
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
                onMain(from: "Orchestrator.cancel.child", finish)
            } else {
                cancelInPlace(below)
            }
        }
        if task.state == .queued {
            let finish = { finalize(task.id, as: .cancelled, summary: "Cancelled.") }
            onMain(from: "Orchestrator.cancel.task", finish)
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
    /// descendant, exactly the way dispatch-time arbitration compares claims
    /// (`OrchestratorDraft.sharedClaimPath`):
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
            let frozen = OrchestratorDraft.freezeClaims(paths, projectDir: task.projectDir)
            requested = Set(task.claimKeys.filter { key in
                frozen.contains { OrchestratorDraft.sharedClaimPath($0, key) != nil }
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
        // `nothing_to_land` joins `landed` on the machine credential: both are final claims about
        // a repository, and a child asserting that it wrote nothing is the one witness with an
        // interest in the answer. The other two remain a child's to declare.
        let machineOnly = requestsLanded
            || raw["state"] as? String == LandingState.nothingToLand.rawValue
        guard machineOnly ? machineMatches : (taskSecretMatches || machineMatches) else {
            RemoteAuth.audit("orchestrator.landing", ["task": taskID, "ok": "0",
                                                       "why": "bad_credential"])
            let required = machineOnly
                ? "Only the orchestrator token may settle a landing on a repository's behalf."
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
                            "state must be pending, landed, abandoned, or nothing_to_land.")
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
        // A branch to land on is exactly the thing this state says did not exist.
        if requestedState == .nothingToLand, fields["target"] != nil {
            return .refused(400, "bad_request",
                            "target is not valid when state is nothing_to_land.")
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
        if let existing, existing.state.isSettled, existing.state != requestedState {
            lock.unlock()
            return .refused(409, "invalid_transition",
                            "A settled obligation cannot move to another state; open a new task.")
        }
        if requestedState != .pending, !current.state.isTerminal {
            lock.unlock()
            return .refused(409, "not_terminal",
                            "Only a terminal task can settle its landing obligation.")
        }
        // The evidence gate for the state a read-only delivery needs. It is a refusal built out
        // of what the registry holds, and `nothingToLandAdmission` says what it can and cannot
        // see; the assertion itself is the machine credential's.
        if requestedState == .nothingToLand,
           case .refused(let why) = nothingToLandAdmission(for: current) {
            lock.unlock()
            return .refused(409, "wrote_to_repository",
                            "nothing_to_land says this task wrote nothing to land, and \(why).")
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

        // Pending, abandoned and nothing_to_land are declarations, not verification claims; they
        // retain the existing state-machine behaviour and never persist verification-shaped
        // fields.
        if requestedState != .landed {
            current.landing = Landing(
                state: requestedState,
                target: target,
                delivery: fields["delivery"] ?? existing?.delivery,
                ownerRootKey: existing?.ownerRootKey
                    ?? OrchestratorDraft.rootKeyDigest(rootKeyLocked(of: current)),
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
        let repositoryTask = current
        let repositoryEvidence = Array(tasks.values)
        let requestedCommit = fields["commit"]!
        let requestedTarget = target!
        lock.unlock()

        guard let repositoryIdentity = OrchestratorDraft.landingGitDirectory(
                for: repositoryTask, among: repositoryEvidence),
              let verification = OrchestratorDraft.verifyTargetLanding(
                gitDirectory: repositoryIdentity,
                target: requestedTarget, commit: requestedCommit) else {
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
                ?? OrchestratorDraft.rootKeyDigest(rootKeyLocked(of: verifiedCurrent)),
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
        lock.lock(); defer { lock.unlock() }
        return pendingLandingRecordsLocked(
            sessions: [], sessionsFresh: false, sessionsObservedAt: nil,
            sessionsGeneration: nil, registryObservedAt: now, now: now)
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
                case .landed, .abandoned, .nothingToLand: landing = .known(.settled)
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
                "age_seconds": OrchestratorDraft.ageSeconds(since: finished, now: now),
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
            case .landed, .abandoned, .nothingToLand: return .settled
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
        guard let listed = OrchestratorDraft.git(
                ["for-each-ref", "--format=%(refname:short) %(objectname)",
                 "refs/heads/clawdline/task/"], cwd: repository),
              listed.status == 0 else { return found }
        found.known = true
        for line in listed.output.split(separator: "\n") {
            let parts = line.split(separator: " ", maxSplits: 1)
            guard parts.count == 2 else { continue }
            found.heads[String(parts[0])] = String(parts[1])
        }
        guard let contained = OrchestratorDraft.git(
                ["for-each-ref", "--format=%(refname:short)", "--merged", "HEAD",
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
        guard let answer = OrchestratorDraft.git(["rev-parse", "--show-toplevel"], cwd: project),
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
            "age_seconds": OrchestratorDraft.ageSeconds(since: task.created, now: now),
        ]
        OrchestratorLandingQueue.projectWriteSet(of: task, into: &row, alwaysClaims: true)
        if let label = task.rootLabel { row["root_label"] = label }
        if let session = task.rootSessionId {
            row["root_key"] = OrchestratorDraft.rootKeyDigest(session)
        }
        if !task.progress.isEmpty {
            row["progress"] = task.progress.map {
                ["note": $0.note, "at": Int($0.at.timeIntervalSince1970)] as [String: Any]
            }
        }
        if let landing = task.landing { row["landing"] = OrchestratorStore.landingRecord(landing) }
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
                    OrchestratorDraft.scheduleWorktreeDisposal(
                        worktree, taskID: task.id, why: "empty", allowCommitted: false)
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
        withRestartRecovered(resume: { resumeAfterRestart() }) {
        // Rescue what the registry still holds before the cap evicts it. Off the main thread and
        // idempotent: a record already attributed contributes nothing on a second pass, so this
        // runs on every launch rather than once, and picks up whatever finished while the app
        // was not running.
            let backfill = ledgerBackfillRecords()
            DispatchQueue.global(qos: .utility).async {
                UsageLedger.shared.importTaskRecords(backfill)
            }
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
    }

    /// One sequencing boundary used by production and tests: persisted restart admission is
    /// restored before any observer or timer can begin a fresh beat.
    static func withRestartRecovered(resume: () -> Void, installObservers: () -> Void) {
        resume()
        installObservers()
    }

    /// Recover waiters and fail tasks whose pre-briefing secret died with the previous process.
    /// Separate from timer wiring so the restart handoff can be exercised without opening a live
    /// app lifecycle in the unit suite.
    static func resumeAfterRestart() {
        load()
        resumeRestartIntent()
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
        var lostAssignmentReceipts: [String] = []
        // `accepted` was persisted before StartPoints was invoked. Reopening it could duplicate a
        // tab whose side effect happened just before the crash, so this boundary fails closed.
        for (id, assignment) in rootAssignments where assignment.state == .accepted {
            var failed = assignment
            failed.state = .failed
            failed.failure = "launch_receipt_lost"
            failed.endedAt = Date()
            rootAssignments[id] = failed
            lostAssignmentReceipts.append(id)
        }
        var incompleteAssignmentIdentities: [String] = []
        for (id, assignment) in rootAssignments
            where ![.accepted, .failed, .inactive].contains(assignment.state)
                && (assignment.identity?.pid == nil
                    || assignment.identity?.processStart == nil) {
            var failed = assignment
            failed.state = .failed
            failed.failure = "restart_identity_incomplete"
            failed.endedAt = Date()
            rootAssignments[id] = failed
            incompleteAssignmentIdentities.append(id)
        }
        lock.unlock()
        for id in interruptedHandoffs {
            RemoteAuth.audit("handoff.undelivered", ["handoff": id, "why": "app_restarted"])
        }
        for id in lostAssignmentReceipts {
            reportRootAssignmentTransition(id)
        }
        for id in incompleteAssignmentIdentities {
            reportRootAssignmentTransition(id)
        }
        let rearmed = rearmLingers()
        if !orphaned.isEmpty || !interruptedHandoffs.isEmpty
            || !lostAssignmentReceipts.isEmpty || !incompleteAssignmentIdentities.isEmpty
            || rearmed { save() }
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
        let watchSnapshot = SessionWatch.shared.identitySnapshot()
        let visibleTerminals = Set(watchSnapshot.targets.map(\.id))
        let executorIdentities = watchSnapshot.targets.filter(\.isAssistant)
            .map { RemoteServer.sessionWorkIdentity(
                $0, publishedIdentity: watchSnapshot.identities[$0.id]) }
        reconcileRestartInventory(watchSnapshot, identities: executorIdentities)
        // Outside this type's lock, because it takes one of its own and nothing here needs the
        // two held together.
        SessionNaming.forget(closedFrom: visibleTerminals)
        lock.lock()
        pruneClosedHandoffTitles(visible: visibleTerminals, identities: executorIdentities,
                                 inventoryComplete: watchSnapshot.complete)
        let handoffLabelsAdopted = adoptHandoffLabelIdentitiesLocked(
            snapshot: watchSnapshot, identities: executorIdentities)
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
        let liveRootAssignments = rootAssignments.values.filter {
            ![.failed, .inactive].contains($0.state)
        }.map(\.id)
        lock.unlock()
        defer {
            lock.lock(); beatsInFlight -= 1; lock.unlock()
        }
        // A tab that has just been seen for the first time is a durable fact, and it is the one
        // that turns a reusable terminal id into an identity. It is written here rather than
        // folded into the `changed` save below, because that one is behind `liveIDs` and a
        // handed-off root is not a task.
        if handoffLabelsAdopted { save() }
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
        for id in liveRootAssignments {
            scheduleRootAssignmentStep(id, snapshot: watchSnapshot,
                                       identities: executorIdentities)
        }
        if fromTimer, !liveHandoffs.isEmpty || !liveRootAssignments.isEmpty {
            SessionWatch.shared.nudge()
        }

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
                if settleExitedLauncher(task, snapshot: watchSnapshot, identities: executorIdentities) || expireSpawningIfDue(task) { continue }
                sawSpawning = true
                scheduleBriefStep(task)
            case .briefed:
                changed = watch(task, snapshot: watchSnapshot, identities: executorIdentities) || changed
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
    private static var rootAssignmentStepsInFlight: Set<String> = []

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

    private static func scheduleRootAssignmentStep(
        _ id: String, snapshot: SessionWatch.IdentitySnapshot,
        identities: [SessionWorkIdentity]) {
        guard !rootAssignmentStepsInFlight.contains(id) else { return }
        rootAssignmentStepsInFlight.insert(id)
        lock.lock(); let channel = rootAssignments[id]?.identity?.terminalID; lock.unlock()
        let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: channel) {
            let reconciled = reconcileRootAssignment(id, snapshot: snapshot,
                                                     identities: identities)
            let changed = rootAssignmentStep(id) || reconciled
            DispatchQueue.main.async {
                rootAssignmentStepsInFlight.remove(id)
                if changed {
                    save(); RemoteServer.shared.broadcastOrchestrator()
                    SessionWatch.shared.labelsDidChange()
                }
            }
        }
        if !admitted { rootAssignmentStepsInFlight.remove(id) }
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

    typealias MenuStep = SessionClosePolicy.MenuStep

    static func menuStep(task: Task, menu: SessionState.Menu?) -> MenuStep {
        SessionClosePolicy.menuStep(answered: task.answeredMenu,
                                    attached: task.attachSessionId != nil, menu: menu)
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
        let publishedIdentity = SessionWatch.shared.publishedInventory().identities[child.id]
        let changed = noteChildIdentity(
            child, publishedIdentity: publishedIdentity, in: &task)
        let screen = Targets.capture(child)
        // A brand-new tab has no hook or registry receipt yet; the fact that this task opened it
        // supplies the independent gate an unnumbered startup picker needs. An attached session
        // gets no such gate because its menu belongs to its owner.
        let menu = screen.flatMap {
            SessionState.menu($0, assistant: task.assistant,
                              hookWaiting: task.attachSessionId == nil)
        }
        switch menuStep(task: task, menu: menu) {
        case .none:
            break
        case .answer(let row):
            task.answeredMenu = true
            guard replaceTask(task, expecting: .spawning) else { return false }
            _ = Targets.answer(UInt8(0x30 + row), to: child)
            RemoteAuth.audit("orchestrator.menu", ["task": task.id, "answer": String(row)])
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

    private static func reconcileRootAssignment(
        _ id: String, snapshot: SessionWatch.IdentitySnapshot,
        identities: [SessionWorkIdentity]) -> Bool {
        lock.lock()
        guard var assignment = rootAssignments[id], let stored = assignment.identity,
              ![.failed, .inactive].contains(assignment.state) else {
            lock.unlock(); return false
        }
        lock.unlock()
        let candidates = identities.compactMap { identity -> RootAssignmentIdentity? in
            guard identity.assistant == stored.assistant else { return nil }
            return RootAssignmentIdentity(
                terminalID: identity.terminalID, assistant: stored.assistant, tty: identity.tty,
                pid: identity.pid, processStart: identity.processStart?.timeIntervalSince1970,
                conversationID: identity.conversationID)
        }
        // Before a process tuple has appeared, only the exact terminal opened by StartPoints may
        // seed it. A different tab can never fill a partially accepted assignment.
        if stored.pid == nil || stored.processStart == nil {
            let exactTerminal = candidates.filter { $0.terminalID == stored.terminalID }
            let distinct = assignment.missingGeneration != nil
                && (assignment.missingGeneration != snapshot.generation
                    || assignment.missingEpoch != snapshot.epoch)
            let absenceConfirmed = distinct && (assignment.missingObservedAt.map {
                Date().timeIntervalSince($0) >= 60
            } ?? false)
            switch rootAssignmentInitialIdentityReconciliation(
                candidates: exactTerminal, inventoryComplete: snapshot.complete,
                absenceConfirmed: absenceConfirmed) {
            case .rebind(let adopted):
                assignment.identity = adopted
                assignment.missingObservedAt = nil
                assignment.missingGeneration = nil
                assignment.missingEpoch = nil
                assignment.reconciliation = nil
                lock.lock(); rootAssignments[id] = assignment; reindex(); lock.unlock(); save()
                return true
            case .wait(let code):
                guard assignment.reconciliation != code
                    || (code == "process_missing_unconfirmed"
                        && assignment.missingObservedAt == nil) else { return false }
                assignment.reconciliation = code
                if code == "process_missing_unconfirmed", assignment.missingObservedAt == nil {
                    assignment.missingObservedAt = Date()
                    assignment.missingGeneration = snapshot.generation
                    assignment.missingEpoch = snapshot.epoch
                }
                lock.lock(); rootAssignments[id] = assignment; lock.unlock(); save()
                return true
            case .fail(let code):
                assignment.state = .failed
                assignment.failure = code
                assignment.reconciliation = nil
                assignment.endedAt = Date()
                lock.lock(); rootAssignments[id] = assignment; reindex(); lock.unlock(); save()
                reportRootAssignmentTransition(id)
                return true
            case .inactive:
                return false
            }
        }
        let distinctCompleteScan = assignment.missingGeneration != nil
            && (assignment.missingGeneration != snapshot.generation
                || assignment.missingEpoch != snapshot.epoch)
        let absentLongEnough = distinctCompleteScan && (assignment.missingObservedAt.map {
            Date().timeIntervalSince($0) >= 60
        } ?? false)
        let beforeIdentity = assignment.identity
        let beforeState = assignment.state
        let beforeFailure = assignment.failure
        let beforeReconciliation = assignment.reconciliation
        let beforeMissingAt = assignment.missingObservedAt
        let beforeMissingGeneration = assignment.missingGeneration
        let beforeMissingEpoch = assignment.missingEpoch
        switch rootAssignmentReconciliation(
            stored: stored, candidates: candidates, inventoryComplete: snapshot.complete,
            absenceConfirmed: absentLongEnough,
            delivered: assignment.briefedAt != nil) {
        case .rebind(let identity):
            assignment.identity = identity
            assignment.missingObservedAt = nil
            assignment.missingGeneration = nil
            assignment.missingEpoch = nil
            assignment.reconciliation = nil
        case .wait(let code):
            assignment.reconciliation = code
            if code == "process_missing_unconfirmed", assignment.missingObservedAt == nil {
                assignment.missingObservedAt = Date()
                assignment.missingGeneration = snapshot.generation
                assignment.missingEpoch = snapshot.epoch
            }
        case .fail(let code):
            assignment.state = .failed; assignment.failure = code; assignment.endedAt = Date()
            assignment.reconciliation = nil
        case .inactive(let code):
            assignment.state = .inactive; assignment.failure = code; assignment.endedAt = Date()
            assignment.reconciliation = nil
        }
        guard assignment.identity != beforeIdentity || assignment.state != beforeState
            || assignment.failure != beforeFailure
            || assignment.reconciliation != beforeReconciliation
            || assignment.missingObservedAt != beforeMissingAt
            || assignment.missingGeneration != beforeMissingGeneration
            || assignment.missingEpoch != beforeMissingEpoch else { return false }
        lock.lock(); rootAssignments[id] = assignment; reindex(); lock.unlock(); save()
        reportRootAssignmentTransition(id)
        return true
    }

    @discardableResult
    private static func rootAssignmentStep(_ id: String) -> Bool {
        lock.lock()
        guard var assignment = rootAssignments[id], let identity = assignment.identity,
              identity.pid != nil, identity.processStart != nil,
              ![.accepted, .failed, .inactive, .active].contains(assignment.state) else {
            lock.unlock(); return false
        }
        lock.unlock()
        let now = Date()
        let promptWindowOpenedAt = rootAssignmentPromptTimeoutAnchor(
            terminalOpenedAt: assignment.terminalOpenedAt ?? assignment.created,
            trustResumedAt: assignment.promptTimeoutStartedAt)
        let timedOut = rootAssignmentPromptTimedOut(
            state: assignment.state, openedAt: promptWindowOpenedAt,
            now: now, briefed: assignment.briefedAt != nil)
        guard let target = target(withID: identity.terminalID),
              target.assistant == assignment.assistant else { return false }
        let screen = Targets.capture(target)
        let trustMenu = screen.flatMap {
            SessionState.menu($0, assistant: assignment.assistant, hookWaiting: true)
        }
        let trust = rootAssignmentTrustDecision(
            projectApproved: assignment.projectApproved, menu: trustMenu,
            answeredTrustMenu: assignment.answeredTrustMenu)
        let inputReady = briefingInputReady(screen, assistant: assignment.assistant)
        let firstDecision = rootAssignmentStepDecision(
            state: assignment.state, promptTimedOut: timedOut, trust: trust,
            answeredTrustMenu: assignment.answeredTrustMenu, inputReady: inputReady,
            delivery: nil, injectAttempts: assignment.injectAttempts)
        switch firstDecision {
        case .activate:
            assignment.state = .active; assignment.activeAt = now
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            RemoteAuth.audit("root_assignment.active", ["assignment": id])
            return true
        case .block:
            guard assignment.state != .blocked
                || assignment.blocker != "workspace_trust_required" else { return false }
            assignment.state = .blocked
            assignment.blocker = "workspace_trust_required"
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            reportRootAssignmentTransition(id)
            return true
        case .answerTrust(let row):
            // The receipt must survive a restart before any digit can reach the terminal. A
            // persistence refusal leaves the picker untouched and restores the in-memory bit.
            assignment.answeredTrustMenu = true
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            guard save() else {
                lock.lock()
                if var current = rootAssignments[id], current.answeredTrustMenu {
                    current.answeredTrustMenu = false; rootAssignments[id] = current
                }
                lock.unlock()
                return false
            }
            _ = Targets.answer(UInt8(0x30 + row), to: target)
            RemoteAuth.audit("root_assignment.trust", ["assignment": id,
                                                        "policy": "approved_workspace"])
            return true
        case .promptReady:
            let resumedFromTrust = assignment.state == .blocked
            assignment.state = .promptReady; assignment.promptReadyAt = now
            if resumedFromTrust { assignment.promptTimeoutStartedAt = now }
            assignment.blocker = nil
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            return true
        case .wait:
            return false
        case .fail(let code):
            assignment.state = .failed; assignment.failure = code; assignment.endedAt = now
            lock.lock(); rootAssignments[id] = assignment; reindex(); lock.unlock()
            reportRootAssignmentTransition(id)
            return true
        case .inspectDelivery:
            break
        case .briefed, .inject:
            return false
        }
        let line = rootAssignmentLine(for: assignment)
        var transcriptKnown = false
        var receipt = RootAssignmentTranscriptReceipt(recorded: false, at: nil)
        switch assignment.assistant {
        case .claude:
            _ = Transcript.locate(cwd: assignment.projectDir, tabTitle: target.name,
                startedAt: assignment.terminalOpenedAt ?? assignment.created,
                sessionID: identity.conversationID, accepting: { url in
                    transcriptKnown = true
                    let text = try? String(contentsOf: url, encoding: .utf8)
                    let candidate = rootAssignmentTranscriptReceipt(
                        text, assistant: .claude, assignmentID: id, line: line)
                    if candidate.recorded { receipt = candidate }
                    return candidate.recorded
                })
        case .codex:
            if let url = Codex.locate(cwd: assignment.projectDir,
                                      startedAt: assignment.terminalOpenedAt ?? assignment.created,
                                      pid: identity.pid),
               let text = try? String(contentsOf: url, encoding: .utf8) {
                transcriptKnown = true
                receipt = rootAssignmentTranscriptReceipt(
                    text, assistant: .codex, assignmentID: id, line: line)
            }
        }
        let retryDelayElapsed = assignment.lastInjectAt.map {
            now.timeIntervalSince($0) >= briefingReceiptDelay
        } ?? true
        let deliveryDecision = rootAssignmentStepDecision(
            state: assignment.state, promptTimedOut: timedOut, trust: .none,
            answeredTrustMenu: assignment.answeredTrustMenu, inputReady: inputReady,
            delivery: RootAssignmentDeliveryEvidence(
                transcriptKnown: transcriptKnown, recorded: receipt.recorded,
                recordedAt: receipt.at,
                deadline: rootAssignmentPromptDeadline(openedAt: promptWindowOpenedAt),
                retryDelayElapsed: retryDelayElapsed),
            injectAttempts: assignment.injectAttempts)
        switch deliveryDecision {
        case .briefed:
            assignment.state = .briefed; assignment.briefedAt = Date()
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            RemoteAuth.audit("root_assignment.briefed", ["assignment": id])
            return true
        case .fail(let code):
            assignment.state = .failed; assignment.failure = code; assignment.endedAt = now
            lock.lock(); rootAssignments[id] = assignment; reindex(); lock.unlock()
            reportRootAssignmentTransition(id)
            return true
        case .inject:
            assignment.injectAttempts += 1; assignment.lastInjectAt = now
            if Targets.send(line, to: target) != nil { assignment.lastInjectAt = nil }
            lock.lock(); rootAssignments[id] = assignment; lock.unlock()
            return true
        default:
            return false
        }
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
    static func watch(_ task: Task, snapshot: SessionWatch.IdentitySnapshot,
                      identities: [SessionWorkIdentity]) -> Bool {
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
           let child = snapshot.targets.first(where: { $0.id == childID }) {
            task.lastSeenChild = Date()
            let refreshed = childIdentityRefreshForTesting?(child, &task)
                ?? noteChildIdentity(
                    child, publishedIdentity: snapshot.identities[child.id], in: &task)
            changed = refreshed || changed
        }
        let inventory = ExecutorInventory(
            complete: snapshot.complete, observedAt: snapshot.observedAt,
            generation: snapshot.generation, epoch: snapshot.epoch)
        let receipt = reconcileExecutor(
            task: task, identities: identities, inventory: inventory,
            previous: task.executorReceipt, now: Date())
        changed = receipt != task.executorReceipt || changed
        task.executorReceipt = receipt
        if receipt.status == .executorMissing || receipt.status == .identityChanged {
            guard replaceTask(task, expecting: .briefed) else { return false }
            finalize(task.id, as: .failure, summary: "\(receipt.status.rawValue): persisted child executor did not "
                + "match a persistent complete Session inventory; inspect provenance before respawning.")
            return false
        }
        if changed {
            if !replaceTask(task, expecting: .briefed) { return false }
        }
        return changed
    }

    /// Fill in the assistant's own durable identity as soon as it exists. Briefing and watching
    /// share this because the transcript is now the boundary between those two states.
    private static func noteChildIdentity(
        _ child: TargetSession, publishedIdentity: SessionWatch.PublishedIdentity?,
        in task: inout Task) -> Bool {
        var changed = false
        let published = publishedIdentity.flatMap { identity in
            identity.assistant == child.assistant && identity.tty == child.tty ? identity : nil
        }
        // SessionWatch has already bound pid, start and transcript to this terminal generation on
        // its background worker. A beat consumes those fields or fails closed; it never fills a
        // missing field with ps/lsof from the main queue.
        switch task.assistant {
        case .claude:
            let observedPID = published?.pid
            let observedStart = published?.processStart
            let registrySessionID = published?.conversationSource == .registry ? published?.conversationID : nil
            let registryTranscript = published?.conversationSource == .registry ? published?.recordURL : nil
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
                changed = (adoptPublishedHookIdentity(
                    published, spawnedAt: task.spawnedAt, in: &task)
                    ?? noteClaudeIdentityFromLegacySources(child, in: &task)) || changed
            case .refuseForeignProcess:
                break
            }
        case .codex:
            let observedPID = published?.pid
            let observedStart = published?.processStart
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
            if task.transcriptPath == nil, let rollout = published?.recordURL {
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

    typealias CloseStep = SessionClosePolicy.CloseStep
    static func closeStep(now: Date, closeAt: Date, inventoryComplete: Bool,
                          inventoryEmpty: Bool = false,
                          emptyInventoryAuthoritative: Bool = false,
                          automationReady: Bool,
                          intervention: TerminalIntervention? = nil,
                          child: TargetSession?, assistant: Assistant, tty: String?,
                          startupMenuExitRow: Int? = nil,
                          activity: () -> Targets.SafeCloseActivity) -> CloseStep {
        SessionClosePolicy.closeStep(
            now: now, closeAt: closeAt, inventoryComplete: inventoryComplete,
            inventoryEmpty: inventoryEmpty,
            emptyInventoryAuthoritative: emptyInventoryAuthoritative,
            automationReady: automationReady,
            retryAllowed: terminalCloseRetryAllowed(intervention: intervention,
                                                     automationReady: automationReady),
            child: child, assistant: assistant, tty: tty,
            startupMenuExitRow: startupMenuExitRow, activity: activity)
    }

    static func failedSpawnStartupExitRow(
        task: Task, child: TargetSession, currentPID: Int32?, currentStart: Date?, screen: String?
    ) -> Int? {
        SessionClosePolicy.failedSpawnStartupExitRow(
            failed: task.state == .spawnFailed, wasSpokenTo: childWasSpokenTo(task),
            expectedTerminalID: task.childTerminalId, expectedTTY: task.childTTY,
            expectedAssistant: task.assistant, expectedPID: task.childPID,
            expectedStart: task.childProcStart, child: child, currentPID: currentPID,
            currentStart: currentStart, screen: screen)
    }

    /// A modal failure has exactly one automatic recovery edge: a later well-formed iTerm list
    /// clears the global circuit. Non-modal failures have no equivalent positive recovery proof,
    /// so another timer beat must not repeat `/exit`, TERM or KILL.
    static func terminalCloseRetryAllowed(intervention: TerminalIntervention?,
                                          automationReady: Bool) -> Bool {
        guard automationReady else { return false }
        return intervention == nil || intervention?.kind == .iTermModal
    }

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
                                 inventory: inventory, end: endChildTab,
                                 dismissStartup: dismissStartupMenu)
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
                             inventory: Targets.safeCloseInventory(), end: end,
                             dismissStartup: dismissStartupMenu)
        }
        if !admitted { finishClosing(task.id) }
        return false
    }

    /// One task's whole close decision, run on the terminal queue against an inventory the caller
    /// took there. Releases that task's closing membership on main whichever way it goes.
    private static func decideChildClose(_ task: Task, childID: String, closeAt: Date,
                                         inventory: Targets.Snapshot,
                                         end: (TargetSession, Bool) -> String?,
                                         dismissStartup: (Int, TargetSession) -> String?) {
        let observed = inventory.sessions.first { $0.id == childID }
        let screen = observed.flatMap(Targets.safeCloseScreen)
        let startupExitRow = observed.flatMap { child in
            failedSpawnStartupExitRow(
                task: task, child: child, currentPID: Targets.pid(of: child),
                currentStart: Targets.processStart(of: child), screen: screen)
        }
        let step = closeStep(now: Date(), closeAt: closeAt,
                             inventoryComplete: inventory.isComplete,
                             inventoryEmpty: inventory.sessions.isEmpty,
                             emptyInventoryAuthoritative: false,
                             automationReady: ITerm.automationReady,
                             intervention: task.terminalIntervention,
                             child: observed, assistant: task.assistant, tty: task.childTTY,
                             startupMenuExitRow: startupExitRow,
                             activity: {
                                 guard let observed else { return .unknown }
                                 return Targets.safeCloseActivity(of: observed, screen: screen)
                             })
        switch step {
        case .wait:
            DispatchQueue.main.async { finishClosing(task.id) }
        case .forget:
            DispatchQueue.main.async {
                finishClosing(task.id)
                settleNothingLeftToClose(task, childID: childID)
            }
        case .dismissStartupMenu(let row):
            guard let observed else {
                DispatchQueue.main.async { finishClosing(task.id) }
                return
            }
            RemoteAuth.audit("orchestrator.close", ["task": task.id, "child": childID,
                                                    "how": "decline_startup"])
            let failure = dismissStartup(row, observed)
            let intervention = failure.map {
                terminalIntervention(for: $0, backend: observed.backend)
            }
            DispatchQueue.main.async {
                finishClosing(task.id)
                if let intervention {
                    settleStartupDismissalFailure(task, intervention: intervention)
                } else {
                    // The assistant exits first; keep the deadline standing so a later fresh
                    // inventory closes the ordinary shell tab it leaves behind.
                    SessionWatch.shared.nudge()
                }
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

    private static func dismissStartupMenu(_ row: Int, on child: TargetSession) -> String? {
        guard (1...9).contains(row) else { return "The startup menu row is out of range." }
        return Targets.answer(UInt8(0x30 + row), to: child)
    }

    private static func settleStartupDismissalFailure(
        _ task: Task, intervention: TerminalIntervention
    ) {
        guard var current = held(task.id), current.closeAt != nil else { return }
        current.terminalIntervention = intervention
        guard replaceTask(current, expecting: current.state) else { return }
        Log.write("orchestrator: could not dismiss failed child's startup menu — "
                  + intervention.message)
        save()
        RemoteServer.shared.broadcastOrchestrator()
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
            OrchestratorDraft.scheduleWorktreeDisposal(worktree, taskID: current.id, why: "empty",
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
            OrchestratorDraft.scheduleWorktreeDisposal(worktree, taskID: current.id, why: "empty",
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
        var review: ReviewReceipt?
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
                           verification: verification(from: obj["verification"]),
                           review: review(from: obj["review"]))
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
        // only when a core field or required review receipt is missing. `readResult` is a disk
        // read and secret comparison that files `badResults`; running it for a cancelled task that
        // never wrote a result is a cost with no answer at the end of it.
        if task.summary == nil || task.artifacts.isEmpty || task.verification == nil
            || (requiresTypedReview(task) && task.review == nil),
           let result = readResult(of: task) {
            lock.lock()
            if task.summary == nil { task.summary = result.summary }
            if task.artifacts.isEmpty { task.artifacts = result.artifacts }
            if task.verification == nil { task.verification = result.verification }
            if task.review == nil, requiresTypedReview(task) { task.review = result.review }
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
        // The durable copy, taken here because this record is on a 200-row eviction and this
        // task's directory is swept twenty-four hours from now. The whole record goes over rather
        // than a hand-picked set of fields — see `UsageLedger.spellings` for why picking is the
        // mistake — and the ledger attributes only what it has not already seen for this session,
        // so a task that ran inside a watched session is not counted twice.
        //
        // **Executed, not delivered, and one call rather than logic threaded through here.** The
        // event being recorded is "this task reached a terminal state and this is its usage".
        // Whether the root was successfully told is a different event with its own retries, so
        // this deliberately sits above `completeFinalization` — where `notifyRoot` lives — reads
        // no notification state, and is not conditional on one. If the surrounding code is ever
        // run more than once, the deterministic interval key and its unique constraint make the
        // repeat a no-op rather than a second charge; that is the property being relied on, not
        // an accident of ordering.
        UsageLedger.shared.collect(taskRecord: ledgerRecord(of: task))
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
            let refreshed = OrchestratorDraft.refreshedWorktree(worktree)
            if removeEmpty {
                OrchestratorDraft.disposeWorktree(refreshed, taskID: task.id, why: "empty",
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
            let matches = Self.targets(
                forRootSession: deliveryRoot, assistant: deliveryAssistant,
                resolution: .task, among: targets,
                sessionID: { identity($0).conversationID })
            if matches.count == 1 {
                recipient = matches[0]
            } else if matches.count > 1 {
                return .refused(.failed(
                    .conversationAmbiguous,
                    "More than one live process proves the root conversation identity; "
                        + "no completion recipient was selected."))
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
        var out = OrchestratorStore.stored(delivery)
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
        if let taskID, !OrchestratorDraft.isTaskID(taskID) {
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
                                                overlaps: [OrchestratorDraft.WorkspaceOverlap]) {
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
                                        overlaps: [OrchestratorDraft.WorkspaceOverlap])
        -> [OrchestratorDraft.WorkspaceOverlapNotice] {
        guard !overlaps.isEmpty else { return [] }
        var notices: [OrchestratorDraft.WorkspaceOverlapNotice] = []
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
            notices.append(OrchestratorDraft.WorkspaceOverlapNotice(
                rootSessionID: root, taskID: newTask.id,
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
            notices.append(OrchestratorDraft.WorkspaceOverlapNotice(
                rootSessionID: root, taskID: other.id,
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
    /// Four roots with five children each is twenty sessions, every one of them a terminal
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
        OrchestratorDraft.rootKey(of: task, among: tasks)
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
        // Its own switch, because it is its own event. A batch that ended badly is still a batch
        // that ended: whatever is worth doing about it is waiting in the root's own conversation,
        // which was told the moment each part came back.
        guard Config.shared.pushOnFanout else { return }
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

    /// The two lines a delivered turn is worth, and the one push it earns.
    ///
    /// Called only where a receipt is created, so the de-duplication that already answers a
    /// repeated report `created: false` is the same de-duplication that keeps the phone quiet.
    /// A session the watch no longer holds still buzzes: the receipt is the event, and a tab
    /// closed between the report and this line does not unmake it.
    private static func announceDelivery(_ delivery: SessionDelivery) {
        guard Config.shared.pushOnDelivery else { return }
        let id = delivery.identity.terminalID
        let session = target(withID: id)
        let message = deliveryMessage(project: session.map(StateHook.projectName(for:))
                                          ?? "Clawdline",
                                      label: session?.displayLabel, summary: delivery.summary,
                                      smart: Config.shared.smartNotifications)
        if let observer = sessionDeliveryPushForTesting {
            observer(message)
            return
        }
        // Its own tag rather than the session's, so a delivery never quietly replaces a "waiting
        // for an answer" that is still unanswered on the same phone.
        WebPush.send(title: message.title, body: message.body, url: "/#session=\(id)",
                     tag: "delivery-\(id)",
                     icon: RemoteIcon.projectPath(
                         for: session.flatMap(Targets.workingDirectory(of:))
                             .flatMap(ProjectIcon.grid(forCwd:))))
    }

    /// Pure, so the wording can be checked without a phone, a terminal or a clock.
    ///
    /// **`smart_notifications` means something narrower on this path, and it is worth saying so
    /// here.** For the fan-out push it spends a bounded Haiku turn writing a sentence. Here it
    /// spends nothing: it carries the session's own `summary` from the receipt, verbatim, with no
    /// model call, no timeout and therefore no fallback to arrange. The assistant wrote that line
    /// about its own delivery with the work still in front of it, which is better evidence than a
    /// generated sentence and costs no quota.
    static func deliveryMessage(project: String, label: String?, summary: String,
                                smart: Bool) -> StateHook.PushMessage {
        StateHook.PushMessage(
            title: label ?? project,
            body: smart ? SmartNotification.body(project: project, summary: summary)
                        : "\(project) \(L.t.pushDelivered)")
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
    /// library of graph shapes, that number cut the last rule off mid-word. Sixteen thousand is
    /// room for a policy somebody has actually thought about, and still small enough that a file
    /// with a novel pasted into it cannot push the task itself off the bottom of a child's
    /// attention — which is what the limit is for.
    static let policyLimit = 16_000

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

    static var isOnMainQueue: Bool { MainQueue.isCurrent }

    private static func onMain<T>(from site: String, _ work: () -> T) -> T {
        MainQueue.hop(from: site, alreadyOnMain: MainQueue.isCurrent, work)
    }

    /// Reach the inventory reader — the one crossing in this file a fixture can take for real.
    ///
    /// **The other five are not driven from here, and an earlier version of this function pretended
    /// otherwise.** It looped over their site names calling `onMain` with a harmless closure, which
    /// exercised the shared helper and nothing else: restoring
    /// `Orchestrator.dispatch`'s attached-delivery failure tail to its old
    /// `Thread.isMainThread`/`DispatchQueue.main.sync` shape left the whole suite green. Driving
    /// those tails properly is not something a test process may do — `dispatch` opens a terminal
    /// tab and every one of them calls `finalize`, which types into somebody's session — so the
    /// five are held by the source-shape guard in `Tests/main.swift` instead, which is checked
    /// against the file on disk and says in its own name that it is structural.
    static func exerciseQueueCrossingsForTesting(sessionID: String, assistant: Assistant) {
        _ = resolveAttachment(sessionID: sessionID, assistant: assistant)
    }

    /// Every task, newest first, in the wire shape. Hops to the main queue for the live
    /// resolutions (which terminal is the root, right now) the way `session(withID:)` does.
    static func records() -> [[String: Any]] {
        onMain(from: "Orchestrator.records") {
            let publication = SessionWatch.shared.publishedInventory(); lock.lock()
            let indexed = tasks; let all = indexed.values.sorted { $0.created > $1.created }
            lock.unlock()
            let graphIndex = graphTaskIndex(indexed); return all.map { record(of: $0, graphIndex: graphIndex, publication: publication) }
        }
    }

    static func record(id: String) -> [String: Any]? {
        onMain(from: "Orchestrator.record(id:)") {
            guard let task = held(id) else { return nil }
            return record(of: task, publication: SessionWatch.shared.publishedInventory())
        }
    }

    /// The durable handoff envelope, in its registry spelling. There is intentionally no public
    /// GET route; this seam exists for round-trip and cleanup tests.
    static func handoffRecord(id: String) -> [String: Any]? {
        guard let envelope = heldHandoff(id) else { return nil }
        return OrchestratorStore.stored(envelope)
    }

    static func rootAssignmentRecords() -> [[String: Any]] {
        load()
        lock.lock(); let rows = rootAssignments.values.sorted { $0.created > $1.created }
        lock.unlock()
        return rows.map(rootAssignmentPublicRecord)
    }

    static func rootAssignmentRecord(id: String) -> [String: Any]? {
        load()
        lock.lock(); let row = rootAssignments[id]; lock.unlock()
        return row.map(rootAssignmentPublicRecord)
    }

    static func rootAssignmentSessionRecord(identity observed: SessionWorkIdentity)
        -> [String: Any]? {
        load()
        lock.lock()
        let assignments = Array(rootAssignments.values)
        lock.unlock()
        return rootAssignmentSessionProjection(assignments: assignments, identity: observed)
    }

    static func rootAssignmentSessionProjection(assignments: [RootAssignment],
                                                identity observed: SessionWorkIdentity)
        -> [String: Any]? {
        let matches = assignments.filter { assignment in
            guard let stored = assignment.identity,
                  ![.failed, .inactive].contains(assignment.state) else { return false }
            return rootAssignmentIdentityMatches(stored, observed: observed)
        }
        guard matches.count == 1, let assignment = matches.first else { return nil }
        return ["id": assignment.id, "label": assignment.label,
                "state": assignment.state.rawValue, "ownership": "independent_root",
                "explanation": "owns_feature_lifecycle"]
    }

    private static func rootAssignmentPublicRecord(_ assignment: RootAssignment)
        -> [String: Any] {
        var row: [String: Any] = [
            "id": assignment.id, "request_id": assignment.requestID,
            "assistant": assignment.assistant.rawValue, "model": assignment.model,
            "project_dir": assignment.projectDir, "label": assignment.label,
            "state": assignment.state.rawValue,
            "assignment": ["objective": assignment.objective, "scope": assignment.scope,
                "constraints": assignment.constraints,
                "relevant_references": assignment.relevantReferences,
                "acceptance": assignment.acceptance],
            "created_at": Int(assignment.created.timeIntervalSince1970),
            "ownership": "independent_root"
        ]
        if let identity = assignment.identity {
            var opened: [String: Any] = ["terminal_id": identity.terminalID]
            if let tty = identity.tty { opened["tty"] = tty }
            if let pid = identity.pid { opened["pid"] = Int(pid) }
            if let start = identity.processStart { opened["process_started_at"] = start }
            if let conversation = identity.conversationID {
                opened["conversation_id"] = conversation
            }
            row["executor"] = opened
        }
        if let value = assignment.terminalOpenedAt { row["terminal_opened_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.promptReadyAt { row["prompt_ready_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.promptTimeoutStartedAt { row["prompt_timeout_started_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.briefedAt { row["briefed_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.activeAt { row["active_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.endedAt { row["ended_at"] = Int(value.timeIntervalSince1970) }
        if let value = assignment.blocker { row["blocker"] = ["code": value] }
        if let value = assignment.failure { row["failure"] = ["code": value] }
        if let value = assignment.reconciliation {
            row["reconciliation"] = ["state": value]
        }
        return row
    }

    static func saveForTesting() { _ = save() }

    static func holdRootAssignmentForTesting(_ assignment: RootAssignment) {
        lock.lock(); rootAssignments[assignment.id] = assignment; reindex(); lock.unlock()
    }

    static func rootAssignmentForTesting(_ id: String) -> RootAssignment? {
        load(); lock.lock(); defer { lock.unlock() }
        return rootAssignments[id]
    }

    /// The durable handoff→tab binding as it stands on this process's own records. Deliberately
    /// not a public route: what the outside world may see of a handoff is its envelope.
    static func handoffLabelForTesting(_ id: String) -> HandoffLabel? {
        load(); lock.lock(); defer { lock.unlock() }
        return handoffLabels[id]
    }

    /// The beat's identity adoption, without a beat. Everything else on that path needs a live
    /// terminal; this step needs only a reading, so it is the one a test can hold still.
    static func adoptHandoffLabelIdentitiesForTesting(
        snapshot: SessionWatch.IdentitySnapshot, identities: [SessionWorkIdentity]) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return adoptHandoffLabelIdentitiesLocked(snapshot: snapshot, identities: identities)
    }

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

    private static func record(of task: Task, graphIndex: GraphTaskIndex? = nil, publication: SessionWatch.InventoryPublication) -> [String: Any] {
        let parentTerminal = task.parentTaskId.flatMap { held($0)?.childTerminalId }
        let rootTerminal = rootTerminalID(for: task, parentTerminalID: parentTerminal,
                                          among: publication.targets,
                                          sessionID: { publication.identities[$0.id]?.conversationID })
        return shape(task, rootTerminal: rootTerminal, graphIndex: graphIndex)
    }

    private static func shape(_ task: Task, rootTerminal: String?, graphIndex: GraphTaskIndex? = nil) -> [String: Any] {
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
        if let graph = task.graph {
            out["graph"] = planningGraphRecord(graph, taskIndex: graphIndex ?? graphTaskIndex())
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
        if let receipt = task.executorReceipt { out["executor"] = executorRecord(receipt) }
        if let summary = task.summary { out["summary"] = summary }
        if let delivery = task.completionDelivery {
            out["completion_delivery"] = completionRecord(delivery)
        }
        if let session = task.attachSessionId {
            out["attached"] = true
            out["attachSession"] = session
        }
        if !task.artifacts.isEmpty { out["artifacts"] = task.artifacts }
        OrchestratorLandingQueue.projectWriteSet(of: task, into: &out)
        if !task.releasedClaims.isEmpty {
            out["released_claims"] = task.releasedClaims.map {
                ["path": $0.path, "released_at": Int($0.releasedAt.timeIntervalSince1970)]
                    as [String: Any]
            }
        }
        if !task.untouchedClaims.isEmpty { out["untouched_claims"] = task.untouchedClaims }
        if let landing = task.landing { out["landing"] = OrchestratorStore.landingRecord(landing) }
        if !task.progress.isEmpty {
            out["progress"] = task.progress.map(OrchestratorStore.progressRecord)
        }
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
        if let review = task.review { out["review"] = reviewRecord(review) }
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

    static func boundedCoordinationText(_ value: Any?, limit: Int) -> String? {
        guard let raw = value as? String else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= limit,
              !text.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return text
    }

    static func canonicalCoordinationRepository(_ raw: String) -> String? {
        guard raw.hasPrefix("/"), raw != "/" else { return nil }
        let path = URL(fileURLWithPath: raw).standardizedFileURL.path
        guard path.hasPrefix("/"), path != "/" else { return nil }
        return path
    }

    static func canonicalCoordinationPaths(_ raw: [String], repository: String)
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
            guard let task = OrchestratorStore.task(from: row) else { continue }
            found[task.id] = task
        }
        var foundHandoffs: [String: HandoffEnvelope] = [:]
        for row in obj["handoffs"] as? [[String: Any]] ?? [] {
            guard let envelope = OrchestratorStore.handoff(from: row) else { continue }
            foundHandoffs[envelope.id] = envelope
        }
        var foundLabels: [String: HandoffLabel] = [:]
        for row in obj["handoff_labels"] as? [[String: Any]] ?? [] {
            guard let label = OrchestratorStore.handoffLabel(from: row) else { continue }
            foundLabels[label.handoffID] = label
        }
        var foundRootAssignments: [String: RootAssignment] = [:]
        for row in obj["root_assignments"] as? [[String: Any]] ?? [] {
            guard let assignment = OrchestratorStore.rootAssignment(from: row) else { continue }
            foundRootAssignments[assignment.id] = assignment
        }
        var foundWaits: [String: CoordinationWait] = [:]
        for row in obj["coordination_waits"] as? [[String: Any]] ?? [] {
            guard let wait = OrchestratorStore.coordinationWait(from: row) else { continue }
            foundWaits[wait.id] = wait
        }
        var foundSessionDeliveries: [String: SessionDelivery] = [:]
        for row in obj["session_deliveries"] as? [[String: Any]] ?? [] {
            guard let delivery = OrchestratorStore.sessionDelivery(from: row) else { continue }
            foundSessionDeliveries[delivery.identity.terminalID] = delivery
        }
        var foundSelfStates: [String: SessionSelfState] = [:]
        for row in obj["session_self_states"] as? [[String: Any]] ?? [] {
            guard let selfState = OrchestratorStore.sessionSelfState(from: row) else { continue }
            foundSelfStates[selfState.identity.terminalID] = selfState
        }
        var foundAttestations: [String: ClosureAttestation] = [:]
        for row in obj["closure_attestations"] as? [[String: Any]] ?? [] {
            guard let attestation = closureAttestation(from: row) else { continue }
            foundAttestations[attestation.identity.terminalID] = attestation
        }
        var foundActivity: [String: Int] = [:]
        var foundActivityClasses: [String: String] = [:]
        for row in obj["session_activity"] as? [[String: Any]] ?? [] {
            guard let terminalID = row["terminal_id"] as? String, !terminalID.isEmpty,
                  terminalID.count <= 512,
                  let generation = row["generation"] as? Int, generation >= 0 else { continue }
            foundActivity[terminalID] = generation
            if let observed = row["class"] as? String,
               ["working", "waiting", "idle", "unknown"].contains(observed) {
                foundActivityClasses[terminalID] = observed
            }
        }
        let rawRestart = obj["restart"] as? [String: Any]
        let parsedRestart = rawRestart.flatMap(restartReceipt(from:))
        let foundRestart = parsedRestart ?? rawRestart.map {
            quarantinedRestartReceipt(from: $0)
        }
        if rawRestart != nil, parsedRestart == nil {
            RemoteAuth.audit("orchestrator.restart.invalid_store", [
                "action": "admission_closed_until_explicit_abort",
            ])
        }
        lock.lock()
        tasks = found
        handoffs = foundHandoffs
        handoffLabels = foundLabels
        rootAssignments = foundRootAssignments
        coordinationWaits = foundWaits
        sessionDeliveries = foundSessionDeliveries
        sessionSelfStates = foundSelfStates
        closureAttestations = foundAttestations
        sessionActivityGenerations = foundActivity
        sessionActivityClasses = foundActivityClasses
        restartReceipt = foundRestart
        // Both halves come back together, so a restart neither invents a tick nor loses one:
        // the first save recomputes the same fingerprint over the same records and finds it
        // unchanged, which is what lets an attestation written before the restart survive it.
        obligationGeneration = max(0, obj["obligation_generation"] as? Int ?? 0)
        obligationFingerprint = obj["obligation_fingerprint"] as? String ?? ""
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
    static func save() -> Bool {
        storeSaveLock.lock(); defer { storeSaveLock.unlock() }
        lock.lock()
        reindex()
        // One choke point for the obligation clock, and it moves only when the obligation
        // evidence itself changed. Bumping on every write would invalidate an attestation with
        // the very save that stored it.
        settleObligationGenerationLocked()
        let rows = tasks.values.sorted { $0.created < $1.created }
            .map { OrchestratorStore.stored($0) }
        let handoffRows = handoffs.values.sorted { $0.created < $1.created }
            .map { OrchestratorStore.stored($0) }
        let handoffLabelRows = handoffLabels.values.sorted { $0.handoffID < $1.handoffID }
            .map { OrchestratorStore.stored($0) }
        let rootAssignmentRows = rootAssignments.values.sorted { $0.created < $1.created }
            .map { OrchestratorStore.stored($0) }
        let waitRows = coordinationWaits.values.sorted { $0.created < $1.created }
            .map { OrchestratorStore.stored($0) }
        let sessionDeliveryRows = sessionDeliveries.values
            .sorted { $0.reportedAt < $1.reportedAt }.map { OrchestratorStore.stored($0) }
        let sessionSelfStateRows = sessionSelfStates.values
            .sorted { $0.identity.terminalID < $1.identity.terminalID }
            .map { OrchestratorStore.stored($0) }
        let closureRows = closureAttestations.values
            .sorted { $0.identity.terminalID < $1.identity.terminalID }.map { stored($0) }
        let activityRows = sessionActivityGenerations.keys.sorted().map { terminalID in
            ["terminal_id": terminalID,
             "generation": sessionActivityGenerations[terminalID] ?? 0,
             "class": sessionActivityClasses[terminalID] ?? "unknown"] as [String: Any]
        }
        let generation = obligationGeneration
        let fingerprint = obligationFingerprint
        let restart = restartReceipt.map(stored)
        lock.unlock()
        var obj: [String: Any] = ["version": 1, "tasks": rows, "handoffs": handoffRows,
                                  "handoff_labels": handoffLabelRows,
                                  "root_assignments": rootAssignmentRows,
                                  "coordination_waits": waitRows,
                                  "session_deliveries": sessionDeliveryRows,
                                  "session_self_states": sessionSelfStateRows,
                                  "closure_attestations": closureRows,
                                  "session_activity": activityRows,
                                  "obligation_generation": generation,
                                  "obligation_fingerprint": fingerprint]
        if let restart { obj["restart"] = restart }
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

    // MARK: - Cleanup

    private static func orphanWorktree(at path: String, taskID: String) -> Worktree? {
        guard OrchestratorDraft.isTaskID(taskID),
              let branch = OrchestratorDraft.worktreeBranch(for: taskID),
              let common = OrchestratorDraft.git(["rev-parse", "--git-common-dir"], cwd: path),
              common.status == 0 else { return nil }
        let rawCommon = common.output.trimmingCharacters(in: .whitespacesAndNewlines)
        let commonPath = rawCommon.hasPrefix("/") ? rawCommon
            : URL(fileURLWithPath: path).appendingPathComponent(rawCommon).standardizedFileURL.path
        let repository = OrchestratorDraft.canonicalFilesystemPath(
            URL(fileURLWithPath: commonPath).deletingLastPathComponent().path)
        guard StartPoints.usable(repository),
              let reflog = OrchestratorDraft.git(["reflog", "show", "--format=%H", branch],
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
        guard let repositories = try? manager.contentsOfDirectory(
            at: OrchestratorDraft.worktreeRoot,
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
                guard OrchestratorDraft.isTaskID(id), !knownTaskIDs.contains(id) else { continue }
                let modified = try? directory.resourceValues(
                    forKeys: [.contentModificationDateKey]).contentModificationDate
                guard let modified, modified < cutoff else { continue }
                guard let worktree = orphanWorktree(at: directory.path, taskID: id) else {
                    RemoteAuth.audit("orchestrator.worktree.kept", [
                        "task": id, "branch": OrchestratorDraft.worktreeBranch(for: id) ?? "?",
                        "why": "unreadable",
                    ])
                    continue
                }
                OrchestratorDraft.disposeWorktree(worktree, taskID: id, why: "swept")
            }
        }
    }

    /// One task as the sweep sees it. `settledAt` is `finishedAt ?? created` — when the task
    /// stopped being live, which is the clock both windows read; `created` is what the count
    /// orders by, because that is what it has always ordered by.
    struct TaskRetentionCandidate: Equatable {
        let id: String
        let terminal: Bool
        let landingPending: Bool
        let created: Date
        let settledAt: Date
    }

    /// The two limits, answered separately so that a caller — and a test — can say which one
    /// fired. They are deliberately not the same window. `directories` is heavyweight working
    /// space under `/tmp/.clawdline`; `records` is the registry row, which is small and is the
    /// only durable evidence the usage Feature classifier has, so it is worth keeping for far
    /// longer than the directory it names. Three settings decide it:
    /// `orchestrator_task_dir_retention_hours` for the first, and
    /// `orchestrator_task_record_limit` (a valve on file size) together with
    /// `orchestrator_task_record_retention_days` (the retention policy) for the second.
    ///
    /// Either record limit fires alone: the count drops what is past `recordLimit` however recent
    /// it is, and the age drops what is past `recordDays` however few records there are. A
    /// pending landing is exempt from both, unconditionally, and a task that is not terminal is
    /// never aged out by either clock.
    static func taskRetentionSweep(_ rows: [TaskRetentionCandidate], now: Date = Date(),
                                   directoryHours: Int, recordLimit: Int, recordDays: Int)
        -> (directories: [String], records: [String]) {
        let directoryCutoff = now.addingTimeInterval(-Double(directoryHours) * 3600)
        let recordCutoff = now.addingTimeInterval(-Double(recordDays) * 86_400)
        let overCount = Set(rows.sorted { $0.created > $1.created }
                                .dropFirst(recordLimit).map(\.id))
        return (rows.filter {
                    $0.terminal && !$0.landingPending && $0.settledAt < directoryCutoff
                }.map(\.id),
                rows.filter {
                    !$0.landingPending && (overCount.contains($0.id)
                        || ($0.terminal && $0.settledAt < recordCutoff))
                }.map(\.id))
    }

    /// Task directories are working files, not the archive — the record survives here, the
    /// directory goes once `orchestrator_task_dir_retention_hours` has passed and its task is
    /// over. The registry keeps `orchestrator_task_record_limit` ordinary records for
    /// `orchestrator_task_record_retention_days`, plus every pending landing obligation until root
    /// settles it. See ``taskRetentionSweep(_:now:directoryHours:recordLimit:recordDays:)``.
    static func cleanup() {
        load()
        let now = Date()
        let directoryHours = Config.shared.orchestratorTaskDirRetentionHours
        let recordLimit = Config.shared.orchestratorTaskRecordLimit
        let recordDays = Config.shared.orchestratorTaskRecordRetentionDays
        let cutoff = now.addingTimeInterval(-Double(directoryHours) * 3600)
        // Read before the region below, because this one acquires the lock for itself. A handoff
        // label is reclaimed on the tab rather than on its envelope's 24-hour clock: suppressed
        // means a beat holding a live reading of this machine could not find the process the
        // label names, which is the moment the label stopped describing anything. Nothing here
        // is on the envelope's timetable — a delivered handoff's letter goes a day after it
        // lands, and the session it opened is usually still working then.
        let forgottenLabels = OrchestratorRegistry.withTransaction { registry in
            handoffLabels.keys.filter { registry.isHandoffLabelSuppressed($0) }
        }
        lock.lock()
        let sweep = taskRetentionSweep(tasks.values.map {
            TaskRetentionCandidate(id: $0.id, terminal: $0.state.isTerminal,
                                   landingPending: $0.landing?.state == .pending,
                                   created: $0.created, settledAt: $0.finishedAt ?? $0.created)
        }, now: now, directoryHours: directoryHours, recordLimit: recordLimit,
           recordDays: recordDays)
        let done = sweep.directories.compactMap { tasks[$0] }
        let expiredHandoffs = handoffs.values.filter {
            $0.state.isTerminal && $0.created < cutoff
        }
        lock.unlock()
        for task in done {
            if let worktree = task.worktree,
               FileManager.default.fileExists(atPath: worktree.path) {
                OrchestratorDraft.disposeWorktree(worktree, taskID: task.id, why: "swept")
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
        let oldSessionDeliveryIDs = sessionDeliveries.values
            .sorted { $0.reportedAt > $1.reportedAt }.dropFirst(200)
            .map { $0.identity.terminalID }
        let oldRootAssignmentIDs = rootAssignmentCleanupIDs(rootAssignments.values.map {
            RootAssignmentCleanupCandidate(id: $0.id, state: $0.state, created: $0.created)
        })
        for id in sweep.records { tasks.removeValue(forKey: id) }
        for id in oldSessionDeliveryIDs { sessionDeliveries.removeValue(forKey: id) }
        for id in oldRootAssignmentIDs { rootAssignments.removeValue(forKey: id) }
        for id in forgottenLabels { handoffLabels.removeValue(forKey: id) }
        let retained = Set(tasks.keys)
        lock.unlock()
        if !forgottenLabels.isEmpty {
            OrchestratorRegistry.withTransaction { registry in
                for id in forgottenLabels { registry.unsuppressHandoffLabel(id) }
            }
        }
        if !sweep.records.isEmpty || !expiredHandoffs.isEmpty || !oldSessionDeliveryIDs.isEmpty
            || !oldRootAssignmentIDs.isEmpty || !forgottenLabels.isEmpty {
            save()
        }
        cleanupOrphanWorktrees(knownTaskIDs: retained, olderThan: cutoff)
        _ = OwnedStorage.compact()
    }

    // MARK: - Small lookups

    static func held(_ id: String) -> Task? {
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
        onMain(from: "Orchestrator.target(withID:)") {
            SessionWatch.shared.targets.first { $0.id == id }
        }
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
        var matches: [String] = []
        for target in targets where target.assistant == expectedAssistant {
            let conversation = sessionID(target)
            guard target.id == supplied || conversation == supplied,
                  let conversation else { continue }
            matches.append(conversation)
        }
        if matches.count == 1, let resolved = matches.first {
            return (resolved, nil)
        }
        if matches.count > 1 {
            return (supplied, [
                "code": "conversation_ambiguous",
                "message": "More than one live process of the declared assistant proves "
                    + "root.session_id; no owner was selected.",
            ])
        }
        return (supplied, [
            "code": "root_unresolved",
            "message": "root.session_id did not resolve to one live process-bound session; "
                + "completion notification, grouping and close cascade are not guaranteed.",
        ])
    }

    /// Turn the canonical resolver's typed finding into the HTTP dispatch refusal before any
    /// task is registered. Kept pure so duplicate-row semantics are checked at the route boundary
    /// rather than inferred from the warning dictionary alone.
    static func rootBindingRefusal(_ warning: [String: Any]) -> Reply {
        let code = warning["code"] as? String ?? "root_unresolved"
        return .refused(
            status: code == "conversation_ambiguous" ? 409 : 422, code: code,
            message: (warning["message"] as? String ?? "root.session_id is unresolved.")
                + " Resolve the interactive Root with GET /v1/orchestrator/whoami and resend "
                + "with the current process-bound conversation id; do not downgrade owned "
                + "work to detached polling.",
            extra: [:])
    }

    static func target(forRootSession rootSessionID: String,
                       assistant: Assistant?, resolution: RootResolution,
                       among targets: [TargetSession],
                       sessionID: (TargetSession) -> String?) -> TargetSession? {
        let matches = Self.targets(forRootSession: rootSessionID, assistant: assistant,
                                   resolution: resolution, among: targets,
                                   sessionID: sessionID)
        return matches.count == 1 ? matches[0] : nil
    }

    /// Every exact match seen by the process-bound root resolver. Callers which need to explain
    /// a refusal use the count; the ordinary root lookup above still exposes only one-or-none.
    /// Keeping both on this seam makes ambiguity fail closed without inventing a second identity
    /// algorithm for routes which need a typed `ambiguous` answer.
    static func targets(forRootSession rootSessionID: String,
                        assistant: Assistant?, resolution: RootResolution,
                        among targets: [TargetSession],
                        sessionID: (TargetSession) -> String?) -> [TargetSession] {
        targets.filter { target in
            guard assistant == nil || target.assistant == assistant else { return false }
            if resolution == .handoff && target.id == rootSessionID { return true }
            return sessionID(target) == rootSessionID
        }
    }

    private static func rootTargets() -> [TargetSession] {
        onMain(from: "Orchestrator.rootTargets") { SessionWatch.shared.targets }
    }

    private static func activeRootIdentityEvidence(claimed: String?)
        -> [OrchestratorDraft.RootIdentityEvidence] {
        guard let claimed, !claimed.isEmpty else { return [] }
        return rootTargets().compactMap { target in
            guard target.id == claimed, let actualAssistant = target.assistant,
                  let canonical = Transcript.sessionID(of: target), !canonical.isEmpty else {
                return nil
            }
            return OrchestratorDraft.RootIdentityEvidence(
                source: "active_terminal", terminalID: target.id,
                canonicalSessionID: canonical, assistant: actualAssistant)
        }
    }

    /// Test seam: forget everything in memory.
    static func forget() {
        lock.lock()
        tasks = [:]
        OrchestratorRegistry.withTransactionOnHeldLock { $0.removeAllGraphAdmissions() }
        restartReceipt = nil
        handoffs = [:]
        handoffLabels = [:]
        handoffDeliveries = [:]
        rootAssignments = [:]
        coordinationWaits = [:]
        sessionDeliveries = [:]
        sessionSelfStates = [:]
        closureAttestations = [:]
        sessionActivityGenerations = [:]
        sessionActivityClasses = [:]
        obligationGeneration = 0
        obligationFingerprint = ""
        OrchestratorRegistry.withTransactionOnHeldLock { $0.removeAllHandoffTitles() }
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
        childIdentityRefreshForTesting = nil
        rootIdentityEvidenceForTesting = nil
        completionPumpScheduled = false
        completionPumpGeneration += 1
        workspaceOverlapObserverForTesting = nil
        rootNotificationObserverForTesting = nil
        rootAssignmentAuditObserverForTesting = nil
        attachedSenderForTesting = nil
        attachmentInventoryForTesting = nil
        taskStarterForTesting = nil
        agentPushForTesting = nil
        sessionDeliveryPushForTesting = nil
        OrchestratorRegistry.withTransactionOnHeldLock { $0.removeAllTerminalTitles() }
        OrchestratorRegistry.withTransactionOnHeldLock {
            $0.removeAllSuppressedRootAssignmentLabels()
            $0.removeAllSuppressedHandoffLabels()
        }
        OrchestratorRegistry.withTransactionOnHeldLock { $0.removeAllRoles() }
        loaded = false
        lock.unlock()
        RemoteServer.shared.setRestartMaintenance(active: false, requestID: nil)
        resetTranscriptOwnershipCacheForTesting()
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
