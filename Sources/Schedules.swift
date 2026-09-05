import Foundation

/// When a schedule fires, and what happens to one that was only ever meant to fire once.
///
/// This is the half of `docs/schedules.md` that is arithmetic rather than plumbing: the grammar of
/// `when`, the two functions that turn it into an instant, the one that decides what the minute
/// timer should do about that instant, and the stamp a one-shot leaves behind when it has spent
/// itself. `Orchestrator` keeps the files, the tables and the dispatch; it asks this for every
/// answer about *time*.
///
/// **A schedule used to be necessarily recurring.** `when` was a time of day and a set of
/// weekdays, so somebody who wanted a single run at a named time had to make a weekly one and
/// remember to come back afterwards and set `enabled: false` — a chore nothing reminds anybody
/// about, and one this app watched a session be told to do in prose. `when.on` is the shape that
/// says *once*, and three properties make it a schedule rather than a convention:
///
/// - **It is decided here, in the parser, and not in the runner.** A one-shot answers exactly the
///   questions a recurring occurrence answers — `catch_up_hours` if the Mac slept through its
///   minute, `created_at` and `when_changed_at` for an occurrence that predates the file or the
///   save — because it goes through the same two functions and the same ``Orchestrator/scheduleAction(now:fire:catchUpHours:lastRunCreated:lastRunTerminal:createdAt:whenChangedAt:firedAt:)``.
/// - **It retires itself, and says so.** ``Orchestrator/markScheduleFired(id:at:)`` writes
///   `fired_at` into the file the moment a session really opened, so the retirement survives the
///   restart that empties the timer's in-memory tables. A spent row carries that stamp; a paused
///   row carries `enabled: false`. They were indistinguishable while turning one off by hand was
///   the only way to stop it, and a reader could not tell "this ran" from "somebody paused it".
/// - **It cannot be mistaken for a recurring file.** `when` carries `at` plus exactly one of
///   `days` or `on`; every file written before this existed carries `days`, is refused nothing it
///   was accepted for, and means precisely what it always meant.
extension Orchestrator {

    /// One day in the Mac's local calendar, as `when.on` spells it.
    ///
    /// Deliberately not a `Date`: `2026-09-06` is a day, and the instant it becomes depends on the
    /// hour, the minute and the time zone the Mac is in when the question is asked — the same way
    /// `when.at` is a wall-clock time and not a UTC offset.
    struct ScheduleDay: Equatable {
        let year: Int
        let month: Int
        let day: Int

        var text: String { String(format: "%04d-%02d-%02d", year, month, day) }
    }

    /// The three things `when` can say.
    ///
    /// An enum rather than an optional set of weekdays, because the third case is not a weekday
    /// pattern and anything that reads it as one gets a plausible wrong answer rather than an
    /// error: `nil` already means *daily* here, so a one-shot squeezed into that shape would run
    /// every day for the rest of its life.
    enum ScheduleWhen: Equatable {
        case daily
        case weekly(Set<Int>)
        case once(ScheduleDay)

        /// Calendar weekday numbers, or nil for every day — the shape the recurring arithmetic
        /// below wants. **`.once` answers nil too and must never be asked**: it is a date, not a
        /// pattern, and every caller here switches on the case rather than reaching for this.
        var weekdayNumbers: Set<Int>? {
            if case .weekly(let numbers) = self { return numbers }
            return nil
        }

        var onDay: ScheduleDay? {
            if case .once(let day) = self { return day }
            return nil
        }

        var runsOnce: Bool { onDay != nil }
    }

    enum ScheduleWhenOutcome {
        case ok(ScheduleWhen)
        case bad(String)
    }

    /// Sunday first, because that is how `Calendar` numbers weekdays: index + 1 is the number
    /// `Calendar.component(.weekday:)` returns, in every calendar Foundation ships.
    ///
    /// **One table, read in both directions.** The parser used to hold a name-to-number
    /// dictionary and the record projection a number-to-name one, written out separately, and two
    /// tables of the same seven facts are two chances to disagree about which day `sun` is. A
    /// mapping that is off by one is not a visible failure — it is a schedule that runs on the
    /// wrong day and a row that draws the wrong chip — so it is worth the single source and the
    /// checks that walk all seven names against real dates.
    static let scheduleDayNames = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]

    static func scheduleWeekday(named name: String) -> Int? {
        scheduleDayNames.firstIndex(of: name).map { $0 + 1 }
    }

    static func scheduleDayName(ofWeekday number: Int) -> String? {
        guard (1...7).contains(number) else { return nil }
        return scheduleDayNames[number - 1]
    }

    /// `when`, read off a file or off an assembled object, with the parser's own sentence for
    /// every way it can be wrong.
    static func scheduleWhen(from raw: Any?) -> ScheduleWhenOutcome {
        // Exactly two keys, one of which is `at`, and the other drawn from the two that say how
        // often: so a `when` carrying both `days` and `on`, or neither, or a key nobody knows, is
        // one sentence rather than three subtly different ones.
        guard let when = raw as? [String: Any], when.count == 2, when["at"] != nil,
              Set(when.keys).isSubset(of: Set(["at", "days", "on"])) else {
            return .bad("when must contain at and exactly one of days or on")
        }
        if let raw = when["on"] {
            guard let text = raw as? String, let day = scheduleDay(from: text) else {
                return .bad("when.on must be a real calendar date spelled YYYY-MM-DD")
            }
            return .ok(.once(day))
        }
        if let days = when["days"] as? String, days == "daily" { return .ok(.daily) }
        guard let days = when["days"] as? [Any], !days.isEmpty else {
            return .bad("when.days must be daily or a non-empty weekday array")
        }
        var found: Set<Int> = []
        for (index, raw) in days.enumerated() {
            guard let name = raw as? String, let number = scheduleWeekday(named: name) else {
                return .bad("when.days[\(index)] must be sun, mon, tue, wed, thu, fri or sat")
            }
            guard found.insert(number).inserted else {
                return .bad("when.days must not contain duplicates")
            }
        }
        return .ok(.weekly(found))
    }

    /// `YYYY-MM-DD`, and a real day — `2026-02-30` is refused rather than rolled forward.
    ///
    /// Validated against a Gregorian calendar rather than the Mac's, because the four digits in
    /// the file are a Gregorian year: on a Mac set to the Buddhist calendar, `2026` read as a
    /// local year is 1483 AD, and a schedule for tonight would silently be a schedule for the
    /// fifteenth century.
    static func scheduleDay(from text: String) -> ScheduleDay? {
        let parts = text.split(separator: "-", omittingEmptySubsequences: false)
        guard parts.count == 3, parts[0].count == 4, parts[1].count == 2, parts[2].count == 2,
              parts.allSatisfy({ $0.allSatisfy { character in ("0"..."9").contains(character) } }),
              let year = Int(parts[0]), let month = Int(parts[1]), let day = Int(parts[2]),
              year >= 1970 else { return nil }
        var gregorian = Calendar(identifier: .gregorian)
        gregorian.timeZone = TimeZone(secondsFromGMT: 0) ?? .current
        guard DateComponents(year: year, month: month, day: day).isValidDate(in: gregorian) else {
            return nil
        }
        return ScheduleDay(year: year, month: month, day: day)
    }

    /// `when` back in the spelling the file uses, for the two read routes and the Mac's own form.
    static func scheduleWhenObject(_ when: ScheduleWhen, hour: Int, minute: Int) -> [String: Any] {
        var out: [String: Any] = ["at": String(format: "%02d:%02d", hour, minute)]
        switch when {
        case .daily:
            out["days"] = "daily"
        case .weekly(let numbers):
            out["days"] = (1...7).compactMap { numbers.contains($0) ? scheduleDayName(ofWeekday: $0) : nil }
        case .once(let day):
            out["on"] = day.text
        }
        return out
    }

    /// The instant a one-shot names, in the Mac's local time zone.
    ///
    /// The time zone comes from the caller's calendar and the year numbering does not, for the
    /// reason ``scheduleDay(from:)`` gives: the file says a Gregorian date at a local wall-clock
    /// time, which is two different calendars' worth of question in one line.
    static func scheduleFire(of day: ScheduleDay, hour: Int, minute: Int,
                             calendar: Calendar) -> Date? {
        var gregorian = Calendar(identifier: .gregorian)
        gregorian.timeZone = calendar.timeZone
        return gregorian.date(from: DateComponents(year: day.year, month: day.month, day: day.day,
                                                   hour: hour, minute: minute, second: 0))
    }

    static func latestFire(of schedule: Schedule, at now: Date,
                           calendar: Calendar = .autoupdatingCurrent) -> Date? {
        if case .once(let day) = schedule.when {
            guard let fire = scheduleFire(of: day, hour: schedule.hour, minute: schedule.minute,
                                          calendar: calendar) else { return nil }
            return fire <= now ? fire : nil
        }
        let start = calendar.startOfDay(for: now)
        for daysAgo in 0...7 {
            guard let day = calendar.date(byAdding: .day, value: -daysAgo, to: start),
                  let candidate = calendar.date(bySettingHour: schedule.hour,
                                                minute: schedule.minute, second: 0, of: day),
                  candidate <= now else { continue }
            if let weekdays = schedule.when.weekdayNumbers,
               !weekdays.contains(calendar.component(.weekday, from: candidate)) { continue }
            return candidate
        }
        return nil
    }

    /// When this schedule fires next, or nil for one that never will again.
    ///
    /// A one-shot answers nil the moment its instant is past, which is most of what "it stops
    /// being a thing that can fire" means and the only half of it the clock can promise on its
    /// own. The other half — an occurrence still inside its catch-up window, on a Mac that has
    /// restarted since the session opened — is `fired_at`'s, because the timer's memory of what
    /// it has already handled does not survive a restart and the clock cannot tell a run that
    /// happened from one that did not.
    static func nextFire(of schedule: Schedule, after now: Date,
                         calendar: Calendar = .autoupdatingCurrent) -> Date? {
        if case .once(let day) = schedule.when {
            guard let fire = scheduleFire(of: day, hour: schedule.hour, minute: schedule.minute,
                                          calendar: calendar) else { return nil }
            return fire > now ? fire : nil
        }
        let start = calendar.startOfDay(for: now)
        for daysAhead in 0...7 {
            guard let day = calendar.date(byAdding: .day, value: daysAhead, to: start),
                  let candidate = calendar.date(bySettingHour: schedule.hour,
                                                minute: schedule.minute, second: 0, of: day),
                  candidate > now else { continue }
            if let weekdays = schedule.when.weekdayNumbers,
               !weekdays.contains(calendar.component(.weekday, from: candidate)) { continue }
            return candidate
        }
        return nil
    }

    /// What the minute timer should do about one occurrence of one schedule.
    ///
    /// `firedAt` is read first and answers for the whole schedule rather than for this occurrence,
    /// because a one-shot has only ever had one: once a session has opened out of it there is
    /// nothing left for any later beat to decide. It is durable on purpose. The occurrence tables
    /// this app keeps are in memory, so a Mac that restarts while the fire is still inside its
    /// catch-up window would otherwise open a second session for a run that already happened —
    /// which is the one failure a schedule promising to run *once* cannot have.
    ///
    /// `createdAt` is the schedule's own age: an occurrence from before the file existed is not a
    /// run this Mac slept through, because there was nothing there to sleep. Without it, "09:00
    /// daily" made at lunchtime is inside the six-hour catch-up window and dispatches within the
    /// minute — while the 200 that created it said the next run was tomorrow — and made in the
    /// evening it instead pushes that a run was missed. A nil `createdAt` is a file that never
    /// said when it was made, and keeps the behaviour every hand-written schedule has always had.
    ///
    /// `whenChangedAt` is the same gate one question further along, and it is read after it
    /// because the two answer different things: the first is "was there a schedule here", the
    /// second is "was there an occurrence here". A save that moves `21:00` to `09:00` at two in
    /// the afternoon leaves `created_at` correctly alone — the schedule is days old — and invents
    /// an occurrence six hours in the past, which is inside the default catch-up window and so was
    /// dispatched within the minute. Nobody missed a nine o'clock that did not exist at nine.
    static func scheduleAction(now: Date, fire: Date, catchUpHours: Int,
                               lastRunCreated: Date?, lastRunTerminal: Bool?,
                               createdAt: Date?, whenChangedAt: Date?,
                               firedAt: Date? = nil) -> ScheduleAction {
        if firedAt != nil { return .spent }
        if let createdAt, fire < createdAt { return .beforeCreation }
        if let whenChangedAt, fire < whenChangedAt { return .beforeRetiming }
        if let created = lastRunCreated, created >= fire { return .alreadyHandled }
        if lastRunTerminal == false { return .active }
        let window = TimeInterval(max(60, catchUpHours * 3600))
        return now.timeIntervalSince(fire) <= window ? .run : .missed
    }

    /// Write `fired_at` into a one-shot's file, so that its retirement outlives this process.
    ///
    /// Called only where an occurrence was consumed by a session that really opened. A dispatch
    /// this Mac refused consumes the occurrence in memory exactly as it does for a recurring
    /// schedule, and deliberately leaves no stamp: `fired_at` is this app's statement that the
    /// work ran, and a refusal that wrote one would be the row saying so about a session nobody
    /// ever had.
    ///
    /// **Everything else in the file is carried across untouched.** The object is read, one key
    /// is added and the whole is written back, so a field this version of the app has never heard
    /// of survives — the file is the source of truth and this is a stamp on it, not a rewrite of
    /// it. Nothing here changes a recurring schedule: it returns false and writes nothing.
    @discardableResult
    static func markScheduleFired(id: String, at fired: Date) -> Bool {
        guard OrchestratorDraft.isTaskID(id) else { return false }
        let filename = "\(id).json"
        let file = scheduleDirectory.appendingPathComponent(filename)
        guard let data = try? Data(contentsOf: file),
              var obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              case .ok(let when) = scheduleWhen(from: obj["when"]), when.runsOnce,
              obj["fired_at"] == nil else { return false }
        obj["fired_at"] = Int(fired.timeIntervalSince1970)
        let staging = scheduleDirectory.appendingPathComponent(".\(filename).fired")
        do {
            let written = try JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys,
                                                               .withoutEscapingSlashes])
            try written.write(to: staging, options: .atomic)
            let manager = FileManager.default
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: staging.path)
            _ = try manager.replaceItemAt(file, withItemAt: staging)
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        } catch {
            try? FileManager.default.removeItem(at: staging)
            RemoteAuth.audit("orchestrator.schedule.spent", ["schedule": id, "ok": "0",
                                                             "why": "write_failed"])
            return false
        }
        RemoteAuth.audit("orchestrator.schedule.spent", ["schedule": id, "ok": "1"])
        return true
    }

    /// Whether a session holding this Mac's orchestrator token may write this schedule, and the
    /// sentence for when it may not.
    ///
    /// **The asymmetry it narrows is deliberate and stays deliberate.** The three write routes
    /// were built for a paired phone and gated like `/v1/voice`: the write switch, a device that
    /// may `send`, an idempotency key. The orchestrator token is a `0600` file that only a local
    /// process running as this user can read, which is what makes it a proof of being local, and
    /// the argument written beside those routes is that a phone cannot hold one — an argument for
    /// why the device gate exists, not for refusing a credential that is strictly more local than
    /// the one it admits. The argument that does bear weight is the one about *what a schedule
    /// is*: unattended execution that repeats, arranged once and running afterwards with nobody
    /// watching. An assistant session can read this token off the disk it runs on, and a session
    /// that can mint a repeating schedule can arrange to be woken every night forever.
    ///
    /// That reason reaches exactly as far as the repetition. A schedule that runs **once** is one
    /// dispatch at a named time; the same credential already opens a session immediately with
    /// `POST /v1/orchestrator/tasks`, and already runs any schedule on the spot with
    /// `POST /v1/orchestrator/schedules/:id/run`. Refusing it the ability to say *at half past
    /// one* — while allowing *now* — protects nothing, and what it actually produced is on the
    /// record: the session that hit this refusal wrote the JSON file by hand instead, at mode
    /// 0644, with no `created_at`, unvalidated, unread-back and unaudited. The route it was
    /// refused does all five of those things.
    ///
    /// So the token creates, changes and removes a one-shot, and a repeating schedule stays a
    /// person's to arrange — with the refusal now saying so, and saying where the supported path
    /// is. A caller who is told, rather than one who infers.
    static func machineScheduleRefusal(method: String, id: String?,
                                       body: [String: Any]) -> Reply? {
        let runsOnce: Bool
        if method == "POST" {
            // Read off the request rather than off a parse: a body that says neither, or both,
            // is not a one-shot being asked for, and the parser has its own sentence for it once
            // this gate is past.
            runsOnce = body["on"] != nil && body["days"] == nil
        } else {
            // A schedule nobody has is the route's ordinary `404`, not a lecture about what this
            // credential may do. The kind of the file being changed is what decides here, and
            // `updateSchedule` separately refuses a save that would change that kind.
            guard let id, let existing = schedules().first(where: { $0.id == id }) else {
                return nil
            }
            runsOnce = existing.when.runsOnce
        }
        guard !runsOnce else { return nil }
        return .refused(403, "forbidden",
                        "This Mac's orchestrator token may make, change and remove a schedule "
                        + "that runs once — a when with an on date. A repeating schedule is "
                        + "arranged by a person: in Settings, from a paired device that may send, "
                        + "or by writing the file yourself at "
                        + "\(scheduleDirectory.path)/<schedule-id>.json. "
                        + "GET /v1/orchestrator/schedules/:id reads back what you wrote, through "
                        + "the same parser this route would have used.")
    }
}
