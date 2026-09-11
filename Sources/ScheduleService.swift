import Foundation

/// Sole owner of the W2-1 schedule-beat bookkeeping: which occurrences have been handled, which
/// are pending admission to a terminal, which were missed, which schedules are mid-dispatch, and
/// which schedule files are currently invalid — plus the beat's own in-flight-walk counter.
/// `Orchestrator` keeps every schedule route, the beat's control flow and every audit line
/// (`deleteSchedule`, `scheduleInventory`, `runSchedule`, `scheduleBeat`, `runScheduledFire`,
/// `beat(fromTimer:)` all still exist there, unchanged in signature and call site); this type
/// closes the nine bare `Orchestrator.lock` regions documented in docs/architecture-refactor.md's
/// W2-1 scheduling row, which used to guard these six collections directly.
///
/// Several entry points are suffixed `Locked` and assume the caller already holds
/// `OrchestratorRegistry.lock` — the same unchecked convention `Orchestrator.noteActivityLocked`
/// already used — because these particular reads must stay atomic with a task-registry read taken
/// under the same `OrchestratorRegistry.withTaskRecords` hold (an active-task check, most often).
/// Those call sites were never bare-lock regions themselves; encapsulating them here is what makes
/// this type the sole *declaring* owner of the six collections without changing when the lock is
/// actually held.
enum ScheduleService {
    /// A skipped or missed occurrence has no task row to remember it. This prevents one audit and
    /// push per minute while the process stays up; a restart deliberately re-evaluates catch-up.
    private static var handledScheduleFires: [String: Date] = [:]
    private static var pendingScheduleFires: [String: Date] = [:]
    private static var lastMissedScheduleFires: [String: Date] = [:]
    private static var dispatchingSchedules: Set<String> = []
    private static var invalidScheduleFingerprints: [String: String] = [:]
    /// How many `beat` walks are inside the loop. Exists to catch the overlap that should not be
    /// possible; it does not change what a walk does.
    private static var beatsInFlight = 0

    private static var lock: NSLock { OrchestratorRegistry.lock }

    // MARK: - Schedule removal (bare lock ×1)

    /// The three occurrence tables and the fingerprint table all forget one schedule id/filename
    /// together, so a restored file with the same id cannot inherit a stale occurrence.
    static func forgetSchedule(id: String, filename: String) {
        lock.lock()
        handledScheduleFires.removeValue(forKey: id)
        pendingScheduleFires.removeValue(forKey: id)
        lastMissedScheduleFires.removeValue(forKey: id)
        invalidScheduleFingerprints.removeValue(forKey: filename)
        lock.unlock()
    }

    // MARK: - Schedule inventory (bare lock ×2)

    /// The directory listing itself failed: every previously-invalid file is now unreadable state
    /// and carries forward no fingerprint to compare a later scan against.
    static func clearInvalidFingerprints() {
        lock.lock(); invalidScheduleFingerprints = [:]; lock.unlock()
    }

    /// Replace the fingerprint table with this scan's findings and report which invalid files are
    /// newly invalid — a different fingerprint than last scan — so the caller audits/pushes only
    /// for files that changed rather than once per poll.
    static func recordInvalidFingerprints(
        _ invalid: [Orchestrator.InvalidSchedule]
    ) -> [Orchestrator.InvalidSchedule] {
        let fingerprints = Dictionary(uniqueKeysWithValues: invalid.map { ($0.file, $0.fingerprint) })
        lock.lock()
        let newlyInvalid = invalid.filter { invalidScheduleFingerprints[$0.file] != $0.fingerprint }
        invalidScheduleFingerprints = fingerprints
        lock.unlock()
        return newlyInvalid
    }

    // MARK: - Manual run

    /// Caller holds the task-records hold already. `true` admits the run and marks the schedule
    /// dispatching; `false` means a task from this schedule is already active, in flight, or a
    /// fire is already pending for it.
    static func admitManualRunLocked(id: String, hasActiveScheduleTask: Bool) -> Bool {
        guard !hasActiveScheduleTask, !dispatchingSchedules.contains(id),
              pendingScheduleFires[id] == nil else { return false }
        dispatchingSchedules.insert(id)
        return true
    }

    /// Settle a manual run's dispatch attempt (bare lock ×1): always clears the dispatching flag,
    /// and on success records the occurrence it consumed so the beat does not fire it again.
    /// Returns the consumed fire date for the caller's one-shot bookkeeping, done outside this
    /// hold exactly as before.
    static func settleManualRun(id: String, succeeded: Bool, fire: Date?) -> Date? {
        lock.lock()
        dispatchingSchedules.remove(id)
        var consumed: Date?
        if succeeded, let fire {
            handledScheduleFires[id] = fire
            consumed = fire
        }
        lock.unlock()
        return consumed
    }

    // MARK: - Beat decision (caller holds the task-records hold)

    /// `true` when this occurrence is already handled or already pending — the beat has nothing
    /// left to decide for this schedule this minute.
    static func alreadyDecidedLocked(scheduleID: String, fire: Date) -> Bool {
        handledScheduleFires[scheduleID] == fire || pendingScheduleFires[scheduleID] == fire
    }

    static func isDispatchingLocked(_ scheduleID: String) -> Bool {
        dispatchingSchedules.contains(scheduleID)
    }

    static func recordRunDecidedLocked(scheduleID: String, fire: Date) {
        pendingScheduleFires[scheduleID] = fire
    }

    static func recordHandledLocked(scheduleID: String, fire: Date, missed: Bool) {
        handledScheduleFires[scheduleID] = fire
        if missed { lastMissedScheduleFires[scheduleID] = fire }
    }

    // MARK: - Terminal-admission retry (bare lock ×1)

    /// The beat could not admit this fire to the bounded terminal lane. Release the pending mark
    /// only if it is still the same occurrence — a later beat may already have moved it on.
    static func releasePendingFireIfCurrent(scheduleID: String, fire: Date) {
        lock.lock()
        if pendingScheduleFires[scheduleID] == fire {
            pendingScheduleFires.removeValue(forKey: scheduleID)
        }
        lock.unlock()
    }

    // MARK: - Stale-fire skip (bare lock ×1)

    static func clearPendingFire(scheduleID: String) {
        lock.lock(); pendingScheduleFires.removeValue(forKey: scheduleID); lock.unlock()
    }

    // MARK: - Timer-fire admission (caller holds the task-records hold)

    /// `true` admits the timer-driven fire; `false` means an active or already-dispatching task
    /// claims this schedule, and the occurrence is recorded handled without ever dispatching.
    static func admitTimerFireLocked(scheduleID: String, hasActiveScheduleTask: Bool,
                                     fire: Date) -> Bool {
        pendingScheduleFires.removeValue(forKey: scheduleID)
        if hasActiveScheduleTask || dispatchingSchedules.contains(scheduleID) {
            handledScheduleFires[scheduleID] = fire
            return false
        }
        dispatchingSchedules.insert(scheduleID)
        return true
    }

    // MARK: - Dispatch settlement (bare lock ×1)

    static func settleDispatch(scheduleID: String, fire: Date, overCapacity: Bool) {
        lock.lock()
        dispatchingSchedules.remove(scheduleID)
        if !overCapacity { handledScheduleFires[scheduleID] = fire }
        lock.unlock()
    }

    // MARK: - Beat in-flight counter (bare lock ×1: the walk's own `defer`)

    /// Caller holds the task-records hold. Returns whether another walk was already inside `beat`
    /// when this one entered.
    static func beginBeatLocked() -> Bool {
        let overlapping = beatsInFlight > 0
        beatsInFlight += 1
        return overlapping
    }

    /// The walk's `defer` runs after the task-records hold has already been released.
    static func endBeat() {
        lock.lock(); beatsInFlight -= 1; lock.unlock()
    }

    // MARK: - Reads

    /// Caller holds the task-records hold — `scheduleRecord(id:)`'s one-schedule detail read.
    static func lastMissedLocked(_ id: String) -> Date? {
        lastMissedScheduleFires[id]
    }

    /// Caller holds the task-records hold — `scheduleRecords()`'s whole-list read, taken once so
    /// every row's date is paired against the same task snapshot.
    static func lastMissedTableLocked() -> [String: Date] {
        lastMissedScheduleFires
    }

    static func handledFireForTesting(_ id: String) -> Date? {
        lock.lock(); defer { lock.unlock() }
        return handledScheduleFires[id]
    }

    // MARK: - Test reset

    /// Caller holds the task-records hold — `forget()`'s test-only reset.
    static func resetForTestingLocked() {
        handledScheduleFires = [:]
        pendingScheduleFires = [:]
        lastMissedScheduleFires = [:]
        dispatchingSchedules = []
        invalidScheduleFingerprints = [:]
    }
}
