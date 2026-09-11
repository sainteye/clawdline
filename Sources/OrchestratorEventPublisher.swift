import Foundation

/// Sole owner of the W2-1 event-publication bookkeeping: the completion pump's coalescing flag
/// and cancellation generation, and the per-terminal activity-turn clock. `Orchestrator` keeps
/// every route, every audit line and the pump's own control flow (`scheduleCompletionPump`,
/// `completionPump`, `completionAttempt`, `activityGeneration(ofTerminal:)` all still exist there,
/// unchanged in signature); this type closes the six bare `Orchestrator.lock` regions documented
/// in docs/architecture-refactor.md's W2-1 event-publication row, which used to guard
/// `completionPumpScheduled`, `completionPumpGeneration` and `sessionActivityGenerations` /
/// `sessionActivityClasses` directly.
///
/// Some call sites — `Orchestrator.noteActivityLocked`, `save()`, `installLoadedStore` and
/// `forget()` — already run inside a task- or session-records hold and never took a bare lock of
/// their own for this state; they keep that shape by calling the `Locked`-suffixed entry points
/// below, the same unchecked convention `noteActivityLocked` already used before this move
/// (`OrchestratorRegistry.swift`'s note on it: "a caller must have one" — Swift cannot enforce
/// that for a lock, only for a Registry capability, so this remains review's to keep).
enum OrchestratorEventPublisher {
    private static var completionPumpScheduled = false
    private static var completionPumpGeneration = 0
    /// Per-terminal turn clock. It advances when a terminal is observed *entering* working or
    /// waiting — a new turn — and not for a changed live line inside one turn.
    private static var sessionActivityGenerations: [String: Int] = [:]
    private static var sessionActivityClasses: [String: String] = [:]

    private static var lock: NSLock { OrchestratorRegistry.lock }

    // MARK: - Completion pump coalescing (bare lock ×5: scheduleCompletionPump,
    // completionPump ×3, completionAttempt's generation check)

    /// `scheduleCompletionPump`'s admission: `nil` when a pump is already scheduled (this call
    /// coalesces into the one already coming); otherwise the generation the newly scheduled pump
    /// must run under.
    static func admitCompletionPump() -> Int? {
        lock.lock()
        guard !completionPumpScheduled else { lock.unlock(); return nil }
        completionPumpScheduled = true
        let generation = completionPumpGeneration
        lock.unlock()
        return generation
    }

    /// `true` while `generation` is still the live pump generation. A pump whose generation was
    /// revoked — by `forget()`, most often in tests — must stop rather than keep working through a
    /// registry that was just invalidated.
    static func isLivePumpGeneration(_ generation: Int) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return generation == completionPumpGeneration
    }

    /// The pump's own `defer`: only the generation that scheduled itself may clear the flag, so a
    /// pump superseded mid-run does not un-schedule the one that superseded it.
    static func clearPumpScheduledIfLive(_ generation: Int) {
        lock.lock()
        if generation == completionPumpGeneration { completionPumpScheduled = false }
        lock.unlock()
    }

    static func currentPumpGeneration() -> Int {
        lock.lock(); defer { lock.unlock() }
        return completionPumpGeneration
    }

    /// Caller holds the shared lock — `completionAttempt`'s in-hold generation comparisons, each
    /// already inside an `OrchestratorRegistry.withTaskRecords` closure.
    static func isLivePumpGenerationLocked(_ generation: Int) -> Bool {
        generation == completionPumpGeneration
    }

    /// `true` when there is no expected generation to check, or the expectation still holds. The
    /// exact `expected == nil || expected == completionPumpGeneration` a caller with an optional
    /// generation would otherwise write inline.
    static func matchesCurrentPumpGenerationLocked(_ expected: Int?) -> Bool {
        expected == nil || expected == completionPumpGeneration
    }

    // MARK: - Activity generation (bare lock ×1: activityGeneration(ofTerminal:))

    static func activityGeneration(ofTerminal terminalID: String) -> Int {
        lock.lock(); defer { lock.unlock() }
        return sessionActivityGenerations[terminalID] ?? 0
    }

    // MARK: - Callers that already hold `OrchestratorRegistry.lock`

    /// Caller holds the shared lock. `activityClass` is the caller's own pure classification of
    /// the observed `SessionState` (`Orchestrator.activityClass(of:)`), passed in rather than
    /// recomputed here so this type stays free of a `SessionState` dependency of its own.
    @discardableResult
    static func noteActivityLocked(terminalID: String, activityClass now: String) -> Bool {
        let before = sessionActivityClasses[terminalID]
        guard before != now else { return false }
        sessionActivityClasses[terminalID] = now
        guard now == "working" || now == "waiting" else { return true }
        sessionActivityGenerations[terminalID] = (sessionActivityGenerations[terminalID] ?? 0) + 1
        return true
    }

    /// Caller holds the shared lock — `save()`'s snapshot of both activity tables in one hold, so
    /// the row it writes for each terminal pairs the generation and class it observed together.
    static func activitySnapshotLocked() -> (generations: [String: Int], classes: [String: String]) {
        (sessionActivityGenerations, sessionActivityClasses)
    }

    /// Caller holds the shared lock — the closeability registry snapshot and `attestClosure`'s own
    /// read, both already inside an `OrchestratorRegistry.withTaskRecords` closure.
    static func activityGenerationLocked(ofTerminal terminalID: String) -> Int {
        sessionActivityGenerations[terminalID] ?? 0
    }

    /// Caller holds the shared lock — `installLoadedStore`'s restore from a durable read.
    static func installActivityLocked(generations: [String: Int], classes: [String: String]) {
        sessionActivityGenerations = generations
        sessionActivityClasses = classes
    }

    /// Caller holds the shared lock — `forget()`'s test-only reset.
    static func resetForTestingLocked() {
        sessionActivityGenerations = [:]
        sessionActivityClasses = [:]
        completionPumpScheduled = false
        completionPumpGeneration += 1
    }
}
