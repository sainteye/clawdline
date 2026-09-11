import Foundation

/// The registry's state owner: one lock, and the collections it protects.
///
/// `Orchestrator` had no owner. It had one `NSLock`, about 160 bare `lock.lock()` sites, roughly
/// nineteen `static var` collections behind them, and ten `…Locked()` functions whose contract was
/// enforced by nothing but the suffix in their names. This type is where that convention becomes a
/// boundary: the collections below are `private` to this file, so no other file can name them.
/// Callers receive a domain capability whose initializer is `fileprivate`; `Orchestrator` can no
/// longer write `titlesByTerminal[x] = y` or `sessionDeliveries[x] = y` outside the registry.
///
/// It is a separate namespace rather than an `extension Orchestrator` in a second file, for the
/// reason `docs/architecture-refactor.md` gives: an extension moves text without moving the
/// dependency, and the dependency is the only thing this refactor exists to move. Writing
/// `Orchestrator.Role` and `Orchestrator.PlanningGraph` in full is that boundary becoming visible
/// at every crossing.
///
/// **Synchronization is unchanged, deliberately.** ``lock`` is the same single `NSLock`
/// `Orchestrator` has always used — `Orchestrator.lock` is now an alias for this one instance, not
/// a second lock — held for exactly as long as it was held before. Ownership moves here; the
/// concurrency primitive does not move at all. No actor, no queue, no lock splitting: a second
/// lock would be a behavior change even where it looks safer.
///
/// **Two synchronization doors, with domain capabilities behind them.** ``withTransaction(_:)``
/// acquires the lock; ``withTransactionOnHeldLock(_:)`` does not, because its callers are the bare
/// `lock.lock()` regions this stage has not converged yet. Session records use a narrower
/// ``SessionRecordsTransaction`` capability, but its held-lock adapter delegates to that same
/// second door rather than introducing another synchronization primitive. Later cuts move those
/// regions onto the acquiring door and the held-lock adapter goes away.
enum OrchestratorRegistry {

    /// Everything needed only while a handoff line is in flight. Losing this on restart is
    /// deliberate: the durable envelope prevents a second tab and startup settles an interrupted
    /// opening as failed.
    struct HandoffDelivery {
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

    /// The one lock. Every collection in this file, and every collection still declared in
    /// `Orchestrator`, is behind this exact instance.
    static let lock = NSLock()

    /// One process-local sliding window. Tickets stay as `Date`s so a caller can return the
    /// exact admission it received; removing the last exact match preserves the old behavior
    /// when a deterministic test clock (or a coarse real clock) produces duplicate timestamps.
    private struct RateWindow {
        let duration: TimeInterval
        private(set) var tickets: [Date] = []

        mutating func take(now: Date, limit: Int) -> Date? {
            tickets = tickets.filter { now.timeIntervalSince($0) < duration }
            guard tickets.count < limit else { return nil }
            tickets.append(now)
            return now
        }

        mutating func giveBack(_ ticket: Date) {
            guard let index = tickets.lastIndex(of: ticket) else { return }
            tickets.remove(at: index)
        }

        mutating func removeAll() {
            tickets = []
        }
    }

    // MARK: - The collections

    /// Graph node key → the dispatch currently admitting it. A reservation held between the
    /// frontier reading and the task record that replaces it.
    private static var graphAdmissions:
        [String: (taskID: String, graph: Orchestrator.PlanningGraph)] = [:]

    /// Child terminal id → task title, rebuilt whenever the tasks change. Read on every redraw
    /// of every session row, which is why it is a dictionary and not a walk over the tasks.
    private static var titlesByTerminal: [String: String] = [:]

    /// Handoff tabs are roots, not task roles, but still keep the protocol's requested label.
    private static var handoffTitlesByTerminal: [String: String] = [:]

    /// A closed assignment tab may leave a durable live row while two-scan loss confirmation is
    /// pending. Suppress only that assignment's label so a reused terminal id is never renamed.
    private static var suppressedRootAssignmentLabels: Set<String> = []

    /// The same shape for a handoff's durable label, by handoff id. Suppression rather than
    /// deletion because this side of it is recomputed from a live reading on every beat: one
    /// inventory that could not see a tab must cost that tab its name until the next reading,
    /// never the durable record that gives the name back after a restart.
    private static var suppressedHandoffLabels: Set<String> = []

    /// Terminal id → where that tab sits in the tree. Rebuilt beside ``titlesByTerminal``.
    private static var rolesByTerminal: [String: Orchestrator.Role] = [:]

    /// These windows are deliberately absent from the store. A restart and `forget()` both
    /// restore their empty process-local state, exactly as before ownership moved here.
    private static var dispatchRateWindow = RateWindow(duration: 10 * 60)
    private static var notificationRateWindow = RateWindow(duration: 60 * 60)
    private static var invalidTaskSecretNotificationRateWindow = RateWindow(duration: 10 * 60)
    private static var scheduleWriteRateWindow = RateWindow(duration: 10 * 60)

    // MARK: Session records

    /// Plaintext child secrets exist only between dispatch and briefing. They are intentionally
    /// process-local and never participate in a persistent projection.
    private static var taskSecrets: [String: String] = [:]

    /// Root terminal id → the last turn that root explicitly delivered. Unlike a child result,
    /// this durable receipt is consumed when the same tab begins another observed turn.
    /// Persistence keeps the existing schema-v1 keys; ownership is the only thing moving here.
    private static var sessionDeliveries: [String: Orchestrator.SessionDelivery] = [:]
    private static var sessionSelfStates: [String: Orchestrator.SessionSelfState] = [:]
    /// Monotonic invalidation source for closeability. The Registry owns both the collection and
    /// its mutation clock; the higher-level facade observes the clock instead of being called by
    /// its storage layer.
    private static var sessionSelfStateMutationGeneration = 0

    /// In-flight handoff delivery state is process-local. Startup settles interrupted openings;
    /// persisting these values would instead create a duplicate-delivery risk.
    private static var handoffDeliveries: [String: HandoffDelivery] = [:]

    /// The immutable persistent half used by store snapshots. Returning copies prevents a save
    /// caller from retaining a mutable registry collection after the lock is released.
    struct PersistentSessionRecords {
        let deliveries: [String: Orchestrator.SessionDelivery]
        let selfStates: [String: Orchestrator.SessionSelfState]
    }

    // MARK: - The transaction

    /// The general orchestration-state capability. Session records use the narrower capability
    /// below; both are manufactured only by this file's synchronization doors.
    ///
    /// It carries no state of its own: it is a capability, and its whole job is that holding one
    /// is the difference between code that compiles and code that does not. `init` is
    /// `fileprivate` and there is no other member, so the two functions below are the only places
    /// in the program where one comes into existence.
    ///
    /// **The residual, named rather than glossed over.** Swift cannot yet say "this value may not
    /// outlive the closure it was handed to" in the language mode this target compiles under
    /// (`-swift-version 5`; `~Escapable` is Swift 6.2), so a caller who deliberately assigned the
    /// token to a variable outside the closure could use it after the lock had been released. That
    /// is an act of sabotage rather than a slip, no call site does it, and it is a strictly
    /// narrower hole than the bare `static var` this replaces — but it is a hole, and a report
    /// that claimed the type system closed it completely would be claiming too much.
    struct Transaction {
        fileprivate init() {}

        // MARK: Graph admissions

        /// The dispatch already admitting this graph node, if one is.
        func graphAdmission(forKey key: String)
            -> (taskID: String, graph: Orchestrator.PlanningGraph)? {
            OrchestratorRegistry.graphAdmissions[key]
        }

        /// Every reservation currently held, in the collection's own order.
        func graphAdmissions() -> [(taskID: String, graph: Orchestrator.PlanningGraph)] {
            Array(OrchestratorRegistry.graphAdmissions.values)
        }

        func reserveGraphAdmission(_ key: String, taskID: String,
                                   graph: Orchestrator.PlanningGraph) {
            OrchestratorRegistry.graphAdmissions[key] = (taskID, graph)
        }

        func releaseGraphAdmission(_ key: String) {
            OrchestratorRegistry.graphAdmissions.removeValue(forKey: key)
        }

        func removeAllGraphAdmissions() {
            OrchestratorRegistry.graphAdmissions = [:]
        }

        // MARK: Per-terminal facts

        func title(forTerminal id: String) -> String? {
            OrchestratorRegistry.titlesByTerminal[id]
        }

        func role(forTerminal id: String) -> Orchestrator.Role? {
            OrchestratorRegistry.rolesByTerminal[id]
        }

        /// Every role at once, for a caller deciding against the whole tree rather than one tab.
        func roles() -> [String: Orchestrator.Role] {
            OrchestratorRegistry.rolesByTerminal
        }

        /// The titles and roles are rebuilt together and replaced together: the projection is a
        /// whole answer, and half of a new one beside half of an old one is not a state any
        /// reader has ever been able to see.
        func setTerminalProjection(titles: [String: String],
                                   roles: [String: Orchestrator.Role]) {
            OrchestratorRegistry.titlesByTerminal = titles
            OrchestratorRegistry.rolesByTerminal = roles
        }

        func removeAllTerminalTitles() {
            OrchestratorRegistry.titlesByTerminal = [:]
        }

        func removeAllRoles() {
            OrchestratorRegistry.rolesByTerminal = [:]
        }

        // MARK: Handoff labels

        func handoffTitles() -> [String: String] {
            OrchestratorRegistry.handoffTitlesByTerminal
        }

        func setHandoffTitle(_ label: String, forTerminal id: String) {
            OrchestratorRegistry.handoffTitlesByTerminal[id] = label
        }

        func setHandoffTitles(_ titles: [String: String]) {
            OrchestratorRegistry.handoffTitlesByTerminal = titles
        }

        func removeAllHandoffTitles() {
            OrchestratorRegistry.handoffTitlesByTerminal = [:]
        }

        // MARK: Suppressed Root Assignment labels

        func isRootAssignmentLabelSuppressed(_ id: String) -> Bool {
            OrchestratorRegistry.suppressedRootAssignmentLabels.contains(id)
        }

        func suppressRootAssignmentLabel(_ id: String) {
            OrchestratorRegistry.suppressedRootAssignmentLabels.insert(id)
        }

        func unsuppressRootAssignmentLabel(_ id: String) {
            OrchestratorRegistry.suppressedRootAssignmentLabels.remove(id)
        }

        func removeAllSuppressedRootAssignmentLabels() {
            OrchestratorRegistry.suppressedRootAssignmentLabels = []
        }

        // MARK: Suppressed handoff labels

        func isHandoffLabelSuppressed(_ id: String) -> Bool {
            OrchestratorRegistry.suppressedHandoffLabels.contains(id)
        }

        func suppressHandoffLabel(_ id: String) {
            OrchestratorRegistry.suppressedHandoffLabels.insert(id)
        }

        func unsuppressHandoffLabel(_ id: String) {
            OrchestratorRegistry.suppressedHandoffLabels.remove(id)
        }

        func removeAllSuppressedHandoffLabels() {
            OrchestratorRegistry.suppressedHandoffLabels = []
        }

        // MARK: Rate windows

        /// Dispatch capacity is dynamic, so the caller supplies the ceiling read for this take.
        func takeDispatchRate(now: Date, limit: Int) -> Date? {
            OrchestratorRegistry.dispatchRateWindow.take(now: now, limit: limit)
        }

        func refundDispatchRate(_ ticket: Date) {
            OrchestratorRegistry.dispatchRateWindow.giveBack(ticket)
        }

        func takeNotificationRate(now: Date) -> Date? {
            OrchestratorRegistry.notificationRateWindow.take(now: now, limit: 30)
        }

        func refundNotificationRate(_ ticket: Date) {
            OrchestratorRegistry.notificationRateWindow.giveBack(ticket)
        }

        func takeInvalidTaskSecretNotificationRate(now: Date) -> Bool {
            OrchestratorRegistry.invalidTaskSecretNotificationRateWindow
                .take(now: now, limit: 3) != nil
        }

        func takeScheduleWriteRate(now: Date) -> Bool {
            OrchestratorRegistry.scheduleWriteRateWindow.take(now: now, limit: 10) != nil
        }

        func removeAllRateWindows() {
            OrchestratorRegistry.dispatchRateWindow.removeAll()
            OrchestratorRegistry.notificationRateWindow.removeAll()
            OrchestratorRegistry.invalidTaskSecretNotificationRateWindow.removeAll()
            OrchestratorRegistry.scheduleWriteRateWindow.removeAll()
        }

        /// Test receipts retain the ticket order so duplicate-ticket refunds are observable.
        func dispatchRateTicketsForTesting() -> [Date] {
            OrchestratorRegistry.dispatchRateWindow.tickets
        }

        func notificationRateTicketsForTesting() -> [Date] {
            OrchestratorRegistry.notificationRateWindow.tickets
        }

        func invalidTaskSecretNotificationRateCountForTesting() -> Int {
            OrchestratorRegistry.invalidTaskSecretNotificationRateWindow.tickets.count
        }

        func scheduleWriteRateCountForTesting() -> Int {
            OrchestratorRegistry.scheduleWriteRateWindow.tickets.count
        }

    }

    /// A capability limited to the four session-record collections migrated in W1-2. Keeping it
    /// distinct from ``Transaction`` prevents a delivery call site from reaching graph, title,
    /// or rate-window state merely because both domains currently share one lock.
    struct SessionRecordsTransaction {
        fileprivate init() {}

        func taskSecret(for id: String) -> String? { taskSecrets[id] }
        func setTaskSecret(_ secret: String, for id: String) { taskSecrets[id] = secret }
        func removeTaskSecret(for id: String) { taskSecrets.removeValue(forKey: id) }
        func removeAllTaskSecrets() { taskSecrets = [:] }

        func sessionDelivery(forTerminal id: String) -> Orchestrator.SessionDelivery? {
            sessionDeliveries[id]
        }
        func sessionDeliveriesSnapshot() -> [String: Orchestrator.SessionDelivery] {
            sessionDeliveries
        }
        func setSessionDelivery(_ delivery: Orchestrator.SessionDelivery,
                                forTerminal id: String) {
            sessionDeliveries[id] = delivery
        }
        func removeSessionDelivery(forTerminal id: String) {
            sessionDeliveries.removeValue(forKey: id)
        }
        func replaceSessionDeliveries(_ deliveries: [String: Orchestrator.SessionDelivery]) {
            sessionDeliveries = deliveries
        }
        func removeAllSessionDeliveries() { sessionDeliveries = [:] }

        func handoffDelivery(for id: String) -> HandoffDelivery? {
            handoffDeliveries[id]
        }
        func handoffDeliveriesSnapshot() -> [String: HandoffDelivery] {
            handoffDeliveries
        }
        func setHandoffDelivery(_ delivery: HandoffDelivery, for id: String) {
            handoffDeliveries[id] = delivery
        }
        @discardableResult
        func setHandoffDeliveryIfPresent(_ delivery: HandoffDelivery, for id: String) -> Bool {
            guard handoffDeliveries[id] != nil else { return false }
            handoffDeliveries[id] = delivery
            return true
        }
        func removeHandoffDelivery(for id: String) { handoffDeliveries.removeValue(forKey: id) }
        func removeAllHandoffDeliveries() { handoffDeliveries = [:] }

        func sessionSelfState(forTerminal id: String) -> Orchestrator.SessionSelfState? {
            sessionSelfStates[id]
        }
        func sessionSelfStatesSnapshot() -> [String: Orchestrator.SessionSelfState] {
            sessionSelfStates
        }
        func setSessionSelfState(_ state: Orchestrator.SessionSelfState,
                                 forTerminal id: String) {
            sessionSelfStates[id] = state
            sessionSelfStateMutationGeneration &+= 1
        }
        func removeSessionSelfState(forTerminal id: String) {
            sessionSelfStates.removeValue(forKey: id)
            sessionSelfStateMutationGeneration &+= 1
        }
        func replaceSessionSelfStates(_ states: [String: Orchestrator.SessionSelfState]) {
            sessionSelfStates = states
            sessionSelfStateMutationGeneration &+= 1
        }
        func removeAllSessionSelfStates() {
            sessionSelfStates = [:]
            sessionSelfStateMutationGeneration &+= 1
        }

        func consumeSelfStateMutation(after observed: inout Int) -> Bool {
            let current = sessionSelfStateMutationGeneration
            guard current != observed else { return false }
            observed = current
            return true
        }

        func replacePersistentRecords(_ records: PersistentSessionRecords) {
            replaceSessionDeliveries(records.deliveries)
            replaceSessionSelfStates(records.selfStates)
        }

        func removeAllPersistentRecords() {
            removeAllSessionDeliveries()
            removeAllSessionSelfStates()
        }

        func persistentSnapshot() -> PersistentSessionRecords {
            PersistentSessionRecords(deliveries: sessionDeliveries, selfStates: sessionSelfStates)
        }
    }

    /// Run `body` under the lock. The transaction is the only way to reach the primary registry
    /// collections; per-session records use their narrower capability below. Both are released
    /// with the lock.
    static func withTransaction<R>(_ body: (Transaction) -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body(Transaction())
    }

    /// Run `body` without acquiring the lock, because the caller is already inside a region that
    /// holds it.
    ///
    /// This is the successor of `Orchestrator`'s `…Locked()` suffix and it inherits that
    /// convention's one weakness: nothing here can ask an `NSLock` whether this thread holds it.
    /// What it does not inherit is the other half — the collections stay unreachable without the
    /// token, so a caller that gets this wrong is unsynchronized rather than also unbounded.
    ///
    /// **Every production use is inside a `lock.lock()` region in the same function or its caller;
    /// the test suite is a stated exception.** Six test call sites reach
    /// `pruneClosedHandoffTitles` without the lock — single-threaded, and doing exactly what they
    /// did before this refactor, when they assigned `handoffTitlesByTerminal` directly. Wrapping
    /// them would change what they exercise. The honest reading is that this door is unchecked at
    /// compile time and its contract holds by inspection, which is the same thing the `…Locked()`
    /// suffix offered; what makes it a migration step rather than a rename is that the count of
    /// these sites is ratcheted in `tools/check-architecture-boundaries.sh` and may only fall.
    /// Each one is a site a later cut converges onto ``withTransaction(_:)``.
    static func withTransactionOnHeldLock<R>(_ body: (Transaction) -> R) -> R {
        body(Transaction())
    }

    /// Session-record migration door for callers that still own the shared legacy lock. Its
    /// capability is domain-limited; W1-7 removes this door when the remaining lock regions move.
    static func withSessionRecordsOnHeldLock<R>(
        _ body: (SessionRecordsTransaction) -> R
    ) -> R {
        withTransactionOnHeldLock { _ in body(SessionRecordsTransaction()) }
    }

    /// Acquire the registry lock and expose only the per-session record capability.
    /// Production code currently enters through the held-lock adapter above; focused tests use
    /// this acquiring door to exercise the owner without reaching unrelated registry state.
    static func withSessionRecords<R>(_ body: (SessionRecordsTransaction) -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body(SessionRecordsTransaction())
    }
}
