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
/// **Acquiring doors only, with domain capabilities behind them.** ``withTransaction(_:)``,
/// ``withSessionRecords(_:)``, ``withCoordinationRecords(_:)`` and ``withTaskRecords(_:)`` each
/// acquire the lock and hand their body one capability. Until W1-4 there was also a second kind of
/// door, `withTransactionOnHeldLock`, which acquired nothing and trusted its caller; W1-4 moved the
/// task table here, converged every one of its call sites onto an acquiring door, and deleted it
/// with its two adapters. A region that must stay atomic across families enters through
/// ``withTaskRecords(_:)`` and reaches the narrower capabilities from that one hold.
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

    // MARK: Coordination records

    /// Handoff id → the durable envelope. Schema-v1 key `handoffs`.
    private static var handoffs: [String: Orchestrator.HandoffEnvelope] = [:]
    /// Handoff id → the tab that handoff was delivered into and what it is called. Durable, and
    /// the only thing that gives a handed-off root its job title back after a restart. Schema-v1
    /// key `handoff_labels`.
    private static var handoffLabels: [String: Orchestrator.HandoffLabel] = [:]
    /// Assignment id → the independent Feature Root record. Schema-v1 key `root_assignments`.
    private static var rootAssignments: [String: Orchestrator.RootAssignment] = [:]
    /// Wait id → one owner's release condition and every session parked on it. Schema-v1 key
    /// `coordination_waits`.
    private static var coordinationWaits: [String: Orchestrator.CoordinationWait] = [:]
    /// Monotonic invalidation source for closeability obligations. Handoff envelopes and waits
    /// are obligation evidence, so every transition that writes either advances it; labels and
    /// assignments are not, and leave it alone. This replaces the `didSet` the two collections
    /// carried while `Orchestrator` declared them: the facade observes the clock at settlement
    /// instead of being called back by its storage.
    private static var coordinationObligationMutationGeneration = 0

    /// All four coordination families as one immutable value, for the store snapshot and for
    /// `load()`. All four persist; none of them is process-local.
    struct PersistentCoordinationRecords {
        let handoffs: [String: Orchestrator.HandoffEnvelope]
        let handoffLabels: [String: Orchestrator.HandoffLabel]
        let rootAssignments: [String: Orchestrator.RootAssignment]
        let coordinationWaits: [String: Orchestrator.CoordinationWait]
    }

    /// What joining a wait decided, taken in the same hold as the write it describes.
    struct CoordinationWaitJoin {
        let waitID: String
        let deduplicated: Bool
        let needsDelivery: Bool
    }

    enum CoordinationWaitReleaseReceipt {
        /// The wait disappeared or changed owner while its release was being delivered.
        case changed
        case recorded(total: Int, stillPending: Int)
    }

    enum CoordinationWaiterWithdrawal {
        case notFound, notWaiter, withdrawn
    }

    // MARK: Task records

    /// Task id → the broker's record of one execution attempt. Schema-v1 key `tasks`, with the
    /// same codec and the same row order on save; only the owner moved here in W1-4.
    private static var tasks: [String: Orchestrator.Task] = [:]
    /// Monotonic invalidation source for closeability obligations. Every task transition advances
    /// it. This replaces the `didSet { obligationFingerprintDirty = true }` the collection carried
    /// while `Orchestrator` declared it: the facade consumes the clock when it settles the
    /// obligation generation instead of being called back by its storage.
    private static var taskMutationGeneration = 0

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

    /// A capability limited to the four durable coordination families moved in W1-3: handoff
    /// envelopes, handoff labels, Root Assignments and coordination waits. It exposes immutable
    /// value projections and closed, named transitions — never a mutable collection — so a
    /// caller can ask for "settle this opening" or "withdraw this waiter" but cannot write a row
    /// the transition did not name. Effects (save, audit, terminal delivery, notification,
    /// filesystem, broadcast) are the caller's, and happen after the door has released the lock.
    ///
    /// Every transition applies to the record as it stands in the registry, so none of them can
    /// resurrect a row another hold has already swept. The two launch-path transitions that
    /// carry a `fallback` keep the facade's historic "restore what this call wrote" shape, and
    /// say so where they take it.
    struct CoordinationRecordsTransaction {
        fileprivate init() {}

        private func noteObligationMutation() {
            OrchestratorRegistry.coordinationObligationMutationGeneration &+= 1
        }

        // MARK: Immutable projections

        func handoff(_ id: String) -> Orchestrator.HandoffEnvelope? {
            OrchestratorRegistry.handoffs[id]
        }

        func handoffEnvelopes() -> [Orchestrator.HandoffEnvelope] {
            Array(OrchestratorRegistry.handoffs.values)
        }

        func handoffLabel(_ id: String) -> Orchestrator.HandoffLabel? {
            OrchestratorRegistry.handoffLabels[id]
        }

        func handoffLabels() -> [Orchestrator.HandoffLabel] {
            Array(OrchestratorRegistry.handoffLabels.values)
        }

        func rootAssignment(_ id: String) -> Orchestrator.RootAssignment? {
            OrchestratorRegistry.rootAssignments[id]
        }

        func rootAssignment(forRequest requestID: String) -> Orchestrator.RootAssignment? {
            OrchestratorRegistry.rootAssignments.values.first { $0.requestID == requestID }
        }

        func rootAssignments() -> [Orchestrator.RootAssignment] {
            Array(OrchestratorRegistry.rootAssignments.values)
        }

        func coordinationWait(_ id: String) -> Orchestrator.CoordinationWait? {
            OrchestratorRegistry.coordinationWaits[id]
        }

        func coordinationWaits() -> [Orchestrator.CoordinationWait] {
            Array(OrchestratorRegistry.coordinationWaits.values)
        }

        func persistentSnapshot() -> PersistentCoordinationRecords {
            PersistentCoordinationRecords(
                handoffs: OrchestratorRegistry.handoffs,
                handoffLabels: OrchestratorRegistry.handoffLabels,
                rootAssignments: OrchestratorRegistry.rootAssignments,
                coordinationWaits: OrchestratorRegistry.coordinationWaits)
        }

        // MARK: Handoff envelopes

        /// Record a new opening envelope unless its id is already held. Returns the held envelope
        /// for an idempotent replay; `nil` means this call recorded `envelope`.
        func openHandoff(_ envelope: Orchestrator.HandoffEnvelope)
            -> Orchestrator.HandoffEnvelope? {
            if let existing = OrchestratorRegistry.handoffs[envelope.id] { return existing }
            OrchestratorRegistry.handoffs[envelope.id] = envelope
            noteObligationMutation()
            return nil
        }

        /// The terminal refused the tab. `opened` is the envelope this call recorded, restored if
        /// the row vanished meanwhile — the facade's historic shape.
        func failHandoffOpening(_ opened: Orchestrator.HandoffEnvelope) {
            var failed = OrchestratorRegistry.handoffs[opened.id] ?? opened
            failed.state = .spawnFailed
            OrchestratorRegistry.handoffs[opened.id] = failed
            noteObligationMutation()
        }

        /// Settle an opening envelope. `nil` when it is absent or already settled, so a second
        /// settlement cannot deliver a second receipt.
        func settleHandoffOpening(_ id: String, delivered: Bool)
            -> Orchestrator.HandoffEnvelope? {
            guard var envelope = OrchestratorRegistry.handoffs[id],
                  envelope.state == .opening else { return nil }
            envelope.state = delivered ? .delivered : .spawnFailed
            OrchestratorRegistry.handoffs[id] = envelope
            noteObligationMutation()
            return envelope
        }

        /// Startup: an opening interrupted by the process ending cannot be resumed without
        /// risking a second tab, so it settles as undelivered. Returns the settled ids.
        func failInterruptedHandoffOpenings() -> [String] {
            var failed: [String] = []
            for (id, envelope) in OrchestratorRegistry.handoffs where envelope.state == .opening {
                var settled = envelope
                settled.state = .spawnFailed
                OrchestratorRegistry.handoffs[id] = settled
                failed.append(id)
            }
            if !failed.isEmpty { noteObligationMutation() }
            return failed
        }

        func expiredTerminalHandoffs(createdBefore cutoff: Date)
            -> [Orchestrator.HandoffEnvelope] {
            OrchestratorRegistry.handoffs.values.filter {
                $0.state.isTerminal && $0.created < cutoff
            }
        }

        func removeHandoffs(_ ids: [String]) {
            guard !ids.isEmpty else { return }
            for id in ids { OrchestratorRegistry.handoffs.removeValue(forKey: id) }
            noteObligationMutation()
        }

        // MARK: Handoff labels

        func bindHandoffLabel(_ label: Orchestrator.HandoffLabel) {
            OrchestratorRegistry.handoffLabels[label.handoffID] = label
        }

        /// Adopt a process identity only while the label is still bound to the identity the
        /// caller decided from. The identity rules themselves stay with the facade.
        func adoptHandoffLabelIdentity(_ id: String,
                                       expected: Orchestrator.RootAssignmentIdentity,
                                       adopted: Orchestrator.RootAssignmentIdentity) -> Bool {
            guard let label = OrchestratorRegistry.handoffLabels[id],
                  label.identity == expected else { return false }
            OrchestratorRegistry.handoffLabels[id] = Orchestrator.HandoffLabel(
                handoffID: label.handoffID, label: label.label, identity: adopted)
            return true
        }

        func forgetHandoffLabels(_ ids: [String]) {
            for id in ids { OrchestratorRegistry.handoffLabels.removeValue(forKey: id) }
        }

        // MARK: Root Assignments

        /// Accept a new assignment unless its request id is already held; the held record is
        /// returned for replay and nothing is written.
        func acceptRootAssignment(_ assignment: Orchestrator.RootAssignment)
            -> Orchestrator.RootAssignment? {
            if let existing = rootAssignment(forRequest: assignment.requestID) { return existing }
            OrchestratorRegistry.rootAssignments[assignment.id] = assignment
            return nil
        }

        /// The acceptance never reached disk, so the record must not outlive the refusal.
        func withdrawUnpersistedRootAssignment(_ id: String) {
            OrchestratorRegistry.rootAssignments.removeValue(forKey: id)
        }

        /// Record the tab the launch opened. `fallback` is the accepted record this launch wrote.
        func recordRootAssignmentTerminal(_ id: String, terminalID: String, at: Date,
                                          fallback: Orchestrator.RootAssignment)
            -> Orchestrator.RootAssignment {
            var opened = OrchestratorRegistry.rootAssignments[id] ?? fallback
            opened.state = .terminalOpened
            opened.terminalOpenedAt = at
            opened.identity = Orchestrator.RootAssignmentIdentity(
                terminalID: terminalID, assistant: opened.assistant, tty: nil, pid: nil,
                processStart: nil, conversationID: nil)
            OrchestratorRegistry.rootAssignments[id] = opened
            return opened
        }

        /// A typed failure. Launch callers pass the record they last wrote as `fallback`; the
        /// step and restart paths pass none and cannot recreate a swept record.
        @discardableResult
        func failRootAssignment(_ id: String, code: String, at: Date,
                                fallback: Orchestrator.RootAssignment? = nil)
            -> Orchestrator.RootAssignment? {
            guard var failed = OrchestratorRegistry.rootAssignments[id] ?? fallback else {
                return nil
            }
            failed.state = .failed
            failed.failure = code
            failed.endedAt = at
            OrchestratorRegistry.rootAssignments[id] = failed
            return failed
        }

        /// Startup: `accepted` was persisted before the terminal was asked for a tab, so the tab
        /// may exist without its receipt. Reopening could duplicate it; this fails closed.
        func failRootAssignmentsWithLostLaunchReceipts(at: Date) -> [String] {
            let ids = OrchestratorRegistry.rootAssignments.values
                .filter { $0.state == .accepted }.map(\.id)
            for id in ids { failRootAssignment(id, code: "launch_receipt_lost", at: at) }
            return ids
        }

        /// Startup: a live assignment without an exact process tuple cannot be matched to a tab
        /// after restart without guessing which reused terminal id is still its own.
        func failRootAssignmentsWithIncompleteIdentity(at: Date) -> [String] {
            let ids = OrchestratorRegistry.rootAssignments.values.filter {
                ![.accepted, .failed, .inactive].contains($0.state)
                    && ($0.identity?.pid == nil || $0.identity?.processStart == nil)
            }.map(\.id)
            for id in ids { failRootAssignment(id, code: "restart_identity_incomplete", at: at) }
            return ids
        }

        /// Commit only the fields reconciliation owns. A trust, injection or report receipt
        /// written by another transition is never overwritten by a reconciliation copy.
        @discardableResult
        func commitRootAssignmentReconciliation(_ candidate: Orchestrator.RootAssignment)
            -> Bool {
            guard var current = OrchestratorRegistry.rootAssignments[candidate.id] else {
                return false
            }
            current.identity = candidate.identity
            current.state = candidate.state
            current.failure = candidate.failure
            current.endedAt = candidate.endedAt
            current.reconciliation = candidate.reconciliation
            current.missingObservedAt = candidate.missingObservedAt
            current.missingGeneration = candidate.missingGeneration
            current.missingEpoch = candidate.missingEpoch
            OrchestratorRegistry.rootAssignments[candidate.id] = current
            return true
        }

        @discardableResult
        func activateRootAssignment(_ id: String, at: Date) -> Bool {
            updateRootAssignment(id) { $0.state = .active; $0.activeAt = at }
        }

        @discardableResult
        func blockRootAssignment(_ id: String, blocker: String) -> Bool {
            updateRootAssignment(id) { $0.state = .blocked; $0.blocker = blocker }
        }

        /// The durable trust receipt, written before any digit reaches the picker.
        @discardableResult
        func recordRootAssignmentTrustAnswer(_ id: String) -> Bool {
            updateRootAssignment(id) { $0.answeredTrustMenu = true }
        }

        /// Rollback for a trust receipt that could not be persisted.
        func withdrawRootAssignmentTrustAnswer(_ id: String) {
            guard OrchestratorRegistry.rootAssignments[id]?.answeredTrustMenu == true else {
                return
            }
            updateRootAssignment(id) { $0.answeredTrustMenu = false }
        }

        @discardableResult
        func markRootAssignmentPromptReady(_ id: String, at: Date,
                                           resumedFromTrust: Bool) -> Bool {
            updateRootAssignment(id) {
                $0.state = .promptReady
                $0.promptReadyAt = at
                if resumedFromTrust { $0.promptTimeoutStartedAt = at }
                $0.blocker = nil
            }
        }

        @discardableResult
        func markRootAssignmentBriefed(_ id: String, at: Date) -> Bool {
            updateRootAssignment(id) { $0.state = .briefed; $0.briefedAt = at }
        }

        /// One more delivery attempt, counted before the line is typed.
        @discardableResult
        func recordRootAssignmentInjection(_ id: String, at: Date) -> Bool {
            updateRootAssignment(id) { $0.injectAttempts += 1; $0.lastInjectAt = at }
        }

        /// The typed attempt did not reach the terminal: keep the count, release the receipt
        /// window — but only the window this attempt opened.
        func withdrawRootAssignmentInjectionTime(_ id: String, at: Date) {
            guard OrchestratorRegistry.rootAssignments[id]?.lastInjectAt == at else { return }
            updateRootAssignment(id) { $0.lastInjectAt = nil }
        }

        /// Write the at-most-once audit receipt. Returns the record and the receipt it replaced,
        /// so a failed save can restore exactly that one.
        func recordRootAssignmentTransitionReport(_ id: String, receipt: String)
            -> (assignment: Orchestrator.RootAssignment, previous: String?)? {
            guard var assignment = OrchestratorRegistry.rootAssignments[id] else { return nil }
            let previous = assignment.reportedTransition
            assignment.reportedTransition = receipt
            OrchestratorRegistry.rootAssignments[id] = assignment
            return (assignment, previous)
        }

        /// Rollback for a receipt whose save failed, only while it is still this call's receipt.
        func withdrawRootAssignmentTransitionReport(_ id: String, receipt: String,
                                                    restoring previous: String?) {
            guard OrchestratorRegistry.rootAssignments[id]?.reportedTransition == receipt else {
                return
            }
            updateRootAssignment(id) { $0.reportedTransition = previous }
        }

        func removeRootAssignments(_ ids: [String]) {
            for id in ids { OrchestratorRegistry.rootAssignments.removeValue(forKey: id) }
        }

        /// Test fixture installation. Production code has no whole-record write.
        func installRootAssignmentForTesting(_ assignment: Orchestrator.RootAssignment) {
            OrchestratorRegistry.rootAssignments[assignment.id] = assignment
        }

        @discardableResult
        private func updateRootAssignment(_ id: String,
                                          _ change: (inout Orchestrator.RootAssignment) -> Void)
            -> Bool {
            guard var assignment = OrchestratorRegistry.rootAssignments[id] else { return false }
            change(&assignment)
            OrchestratorRegistry.rootAssignments[id] = assignment
            return true
        }

        // MARK: Coordination waits

        /// Join the one wait for this owner, repository, path set and release condition, creating
        /// it when none exists. The duplicate test and the write are one hold.
        func joinCoordinationWait(repository: String, paths: [String], owner: String,
                                  releaseCondition: String, waiter: String, reason: String,
                                  now: Date, newWaitID: String) -> CoordinationWaitJoin {
            if var existing = OrchestratorRegistry.coordinationWaits.values.first(where: {
                $0.repository == repository && $0.paths == paths
                    && $0.ownerSessionID == owner && $0.releaseCondition == releaseCondition
            }) {
                if let index = existing.waiters.firstIndex(where: { $0.sessionID == waiter }) {
                    return CoordinationWaitJoin(
                        waitID: existing.id, deduplicated: true,
                        needsDelivery: existing.waiters[index].requestDeliveredAt == nil)
                }
                existing.waiters.append(Orchestrator.CoordinationWaiter(
                    sessionID: waiter, reason: reason, created: now,
                    requestDeliveredAt: nil, releaseDeliveredAt: nil))
                OrchestratorRegistry.coordinationWaits[existing.id] = existing
                noteObligationMutation()
                return CoordinationWaitJoin(waitID: existing.id, deduplicated: false,
                                            needsDelivery: true)
            }
            OrchestratorRegistry.coordinationWaits[newWaitID] = Orchestrator.CoordinationWait(
                id: newWaitID, repository: repository, paths: paths,
                ownerSessionID: owner, releaseCondition: releaseCondition, created: now,
                waiters: [Orchestrator.CoordinationWaiter(
                    sessionID: waiter, reason: reason, created: now,
                    requestDeliveredAt: nil, releaseDeliveredAt: nil)])
            noteObligationMutation()
            return CoordinationWaitJoin(waitID: newWaitID, deduplicated: false,
                                        needsDelivery: true)
        }

        /// The request reached the owner; a retry must never type it again.
        func receiptCoordinationWaitRequest(_ id: String, waiter: String, at: Date) {
            guard var current = OrchestratorRegistry.coordinationWaits[id],
                  let index = current.waiters.firstIndex(where: { $0.sessionID == waiter })
            else { return }
            current.waiters[index].requestDeliveredAt = at
            OrchestratorRegistry.coordinationWaits[id] = current
            noteObligationMutation()
        }

        /// Receipt the waiters a release reached; the wait goes when none is still owed one.
        func receiptCoordinationWaitRelease(_ id: String, owner: String,
                                            deliveredTo delivered: [String],
                                            at: Date) -> CoordinationWaitReleaseReceipt {
            guard var current = OrchestratorRegistry.coordinationWaits[id],
                  current.ownerSessionID == owner else { return .changed }
            for index in current.waiters.indices
                where delivered.contains(current.waiters[index].sessionID) {
                current.waiters[index].releaseDeliveredAt = at
            }
            let total = current.waiters.count
            let stillPending = current.waiters.filter { $0.releaseDeliveredAt == nil }.count
            if stillPending == 0 { OrchestratorRegistry.coordinationWaits.removeValue(forKey: id) }
            else { OrchestratorRegistry.coordinationWaits[id] = current }
            noteObligationMutation()
            return .recorded(total: total, stillPending: stillPending)
        }

        /// A waiter removes only itself; the wait goes with its last waiter.
        func withdrawCoordinationWaiter(_ id: String, waiter: String)
            -> CoordinationWaiterWithdrawal {
            guard var current = OrchestratorRegistry.coordinationWaits[id] else { return .notFound }
            let before = current.waiters.count
            current.waiters.removeAll { $0.sessionID == waiter }
            guard current.waiters.count != before else { return .notWaiter }
            if current.waiters.isEmpty {
                OrchestratorRegistry.coordinationWaits.removeValue(forKey: id)
            } else {
                OrchestratorRegistry.coordinationWaits[id] = current
            }
            noteObligationMutation()
            return .withdrawn
        }

        // MARK: Lifetime

        func replacePersistentRecords(_ records: PersistentCoordinationRecords) {
            OrchestratorRegistry.handoffs = records.handoffs
            OrchestratorRegistry.handoffLabels = records.handoffLabels
            OrchestratorRegistry.rootAssignments = records.rootAssignments
            OrchestratorRegistry.coordinationWaits = records.coordinationWaits
            noteObligationMutation()
        }

        func removeAllPersistentRecords() {
            OrchestratorRegistry.handoffs = [:]
            OrchestratorRegistry.handoffLabels = [:]
            OrchestratorRegistry.rootAssignments = [:]
            OrchestratorRegistry.coordinationWaits = [:]
            noteObligationMutation()
        }

        func consumeObligationMutation(after observed: inout Int) -> Bool {
            let current = OrchestratorRegistry.coordinationObligationMutationGeneration
            guard current != observed else { return false }
            observed = current
            return true
        }
    }

    /// A capability limited to task records, and the one capability that may reach the others
    /// from inside the same hold.
    ///
    /// W1-4 moved the task table — the last collection `Orchestrator` declared behind this lock
    /// that the other capabilities needed to stay atomic with — into this file. Every region that
    /// reads or writes a task enters through ``withTaskRecords(_:)``, which acquires the same
    /// single lock the other doors acquire: dispatch admission, the serialize and claims queries,
    /// the terminal and task indexes, recovery, landing and completion views.
    ///
    /// A region that must stay atomic with graph admissions, the terminal projection, session
    /// records or coordination records reaches them through ``registry``, ``sessionRecords`` and
    /// ``coordinationRecords``. Those are capabilities derived from this hold — not a second
    /// acquisition and not a held-lock door — which is what let `withTransactionOnHeldLock` and
    /// its two adapters be deleted. A capability is meant to live only inside the closure that
    /// received it. No call site returns, stores or captures one today, but Swift 5 cannot
    /// enforce that (non-escapable types are Swift 6.2), so it is a convention kept by review and
    /// by this file's shape, not a compiler guarantee.
    ///
    /// Writes come in three kinds, and only the first is closed:
    ///
    /// - **Named transitions** choose their own guard and write only their own fields.
    ///   ``admitTask(_:)`` inserts and never replaces. Finalization's follow-up facts write only
    ///   while the row still exists and still carries the outcome finalization committed.
    ///   ``commitStepCandidate(_:)`` takes every fact another transition owns from the row as it
    ///   stands, not from the walker's older copy. ``releaseClaims(_:expecting:_:at:)`` appends in
    ///   the hold. The two rollbacks restore only the fields their own step wrote, per row. None
    ///   of them can re-create a swept row.
    /// - **Same-hold read-modify-write** — ``updateTask(_:_:)`` and ``commitTask(_:)`` — is a
    ///   row-scoped capability, not a closed transition: the caller decides the change. It loses
    ///   no concurrent write only because the change is computed from the row read in this same
    ///   hold, which review, not the compiler, keeps. Neither creates a row.
    /// - **Whole-table writes** belong to `load()` (``replaceAllTasks(_:)``) and `forget()`
    ///   (``removeAllTasks()``). ``seedTaskForTesting(_:)`` is the one upsert, for fixtures.
    ///   `tools/check-architecture-boundaries.sh` counts the production callers of all three.
    ///
    /// Every projection is a value. Effects (save, audit, terminal, notification, filesystem,
    /// broadcast) stay the caller's, after the door returns.
    struct TaskRecordsTransaction {
        fileprivate init() {}

        // MARK: Capabilities from the same hold

        var registry: Transaction { Transaction() }
        var sessionRecords: SessionRecordsTransaction { SessionRecordsTransaction() }
        var coordinationRecords: CoordinationRecordsTransaction {
            CoordinationRecordsTransaction()
        }

        private func noteTaskMutation() {
            OrchestratorRegistry.taskMutationGeneration &+= 1
        }

        // MARK: Immutable projections

        func task(_ id: String) -> Orchestrator.Task? {
            OrchestratorRegistry.tasks[id]
        }

        /// The whole table as a value. Writing to the copy writes nothing here.
        func tasksByID() -> [String: Orchestrator.Task] {
            OrchestratorRegistry.tasks
        }

        /// Every row, in the table's own order — the order `Array(tasks.values)` always had.
        func taskValues() -> [Orchestrator.Task] {
            Array(OrchestratorRegistry.tasks.values)
        }

        var taskCount: Int { OrchestratorRegistry.tasks.count }

        // MARK: Admission queries

        /// Older serialized work this candidate must wait behind, judged against the whole table
        /// in this hold.
        func serializeBlockers(for candidate: Orchestrator.Task) -> [Orchestrator.Task] {
            OrchestratorDraft.serializeBlockers(for: candidate, among: taskValues())
        }

        /// Claim overlaps between this candidate and every held task. A dispatch asks this and
        /// records its task in the same hold, so two dispatches cannot both see a path as free.
        func claimsOverlaps(for candidate: Orchestrator.Task)
            -> [OrchestratorDraft.ClaimsOverlap] {
            OrchestratorDraft.claimsOverlaps(for: candidate, among: taskValues())
        }

        /// The session a whole tree hangs from, resolved through the lineage held here, so every
        /// task in one fan-out is counted in the same batch however it was filed. A task that
        /// named nobody gets a key of its own rather than sharing one with every other anonymous
        /// dispatch — the alternative is two unrelated fan-outs waiting for each other.
        func rootKey(of task: Orchestrator.Task) -> String {
            OrchestratorDraft.rootKey(of: task, among: OrchestratorRegistry.tasks)
        }

        func hasActiveScheduleTask(_ scheduleID: String) -> Bool {
            OrchestratorRegistry.tasks.values.contains {
                $0.scheduleID == scheduleID && !$0.state.isTerminal
            }
        }

        // MARK: The terminal index

        /// Rebuild the terminal title and role index from the task, Root Assignment and handoff
        /// label records held here, and replace it whole through ``Transaction/setTerminalProjection(titles:roles:)``.
        /// The inputs and the write share this hold, so no reader sees half of a new projection.
        func rebuildTerminalProjection() {
            let registry = self.registry
            let labels = coordinationRecords.handoffLabels()
            let assignments = coordinationRecords.rootAssignments()
            let taskRows = taskValues()
            var found = registry.handoffTitles()
            // The durable half of the same answer, and the only half a fresh process has: the map
            // above is written when a tab opens and is empty after a restart. Read before the
            // assignment and task rows below, so the precedence between the three sources is
            // exactly what it was — this changes when a handoff label is *known*, never where it
            // ranks against anything else.
            //
            // Two unsuppressed labels on one terminal id are two answers to a question that has
            // one, and dictionary iteration order is not a tie-break. `rootAssignmentSession-
            // Projection` refuses the same class of question with `matches.count == 1`; this
            // refuses per terminal, so an ambiguous tab keeps whatever name it would have had
            // without any handoff label rather than one of the two at random.
            var labelsByTerminal: [String: [String]] = [:]
            for label in labels where !registry.isHandoffLabelSuppressed(label.handoffID) {
                labelsByTerminal[label.identity.terminalID, default: []].append(label.label)
            }
            for (terminal, labels) in labelsByTerminal {
                guard labels.count == 1, let only = labels.first else { continue }
                found[terminal] = only
            }
            var roles: [String: Orchestrator.Role] = [:]
            let rootTaskHosts = taskRows.filter { $0.sessionRoot }
            // A Feature Root receives a label, never a Role. `Role` is the child-lineage type and
            // putting an assignment in it would make a fourth primitive a disguised task.
            for assignment in assignments {
                guard let terminal = assignment.identity?.terminalID,
                      ![.failed, .inactive].contains(assignment.state),
                      !registry.isRootAssignmentLabelSuppressed(assignment.id) else { continue }
                found[terminal] = assignment.label
            }
            for task in taskRows {
                guard let terminal = task.childTerminalId else { continue }
                let role = Orchestrator.Role(
                    taskID: task.id, depth: task.depth, title: task.title,
                    deadline: task.briefedAt?.addingTimeInterval(Double(task.timeoutMinutes) * 60),
                    live: !task.state.isTerminal, taskRootAccess: task.childTaskRootAccess)
                // An attached task is a guest in a session somebody else owns. It may say that the
                // session is busy with broker work while it is running, and that is all: it never
                // renames the session, and it leaves nothing behind when it ends. A tab this app
                // opened normally belongs to that task; `sessionRoot` below is the explicit
                // exception, where the task is only the Root Session's first bounded receipt.
                if task.attachSessionId != nil {
                    let retainedRoots = rootTaskHosts.filter {
                        $0.childTerminalId == terminal && $0.childTTY == task.childTTY
                            && $0.assistant == task.assistant
                    }
                    if retainedRoots.count == 1 { continue }
                    guard role.live else { continue }
                    if let existing = roles[terminal], existing.live { continue }
                    roles[terminal] = role
                    continue
                }
                found[terminal] = Orchestrator.sessionTitle(taskTitle: task.title,
                                                            scheduled: task.scheduleID != nil)
                if task.sessionRoot { continue }
                // A tab is normally one task's for its whole life. When two records name the same
                // one — a terminal id reused after a tab closed and another opened in its place —
                // the live task is the one anything asking this question means.
                if let existing = roles[terminal], existing.live, !role.live { continue }
                roles[terminal] = role
            }
            registry.setTerminalProjection(titles: found, roles: roles)
        }

        // MARK: Named transitions

        /// Admission: insert the row a dispatch just made. `false` — writing nothing — when a row
        /// with this id already exists, so a second admission of one id can neither replace the
        /// first nor stand in for it.
        @discardableResult
        func admitTask(_ task: Orchestrator.Task) -> Bool {
            guard OrchestratorRegistry.tasks[task.id] == nil else { return false }
            OrchestratorRegistry.tasks[task.id] = task
            noteTaskMutation()
            return true
        }

        /// The guard every finalization follow-up shares. Finalization commits the terminal state
        /// in one hold and then reads files — the result, the transcript's usage, claimed paths —
        /// outside it, so each later stage writes from a copy another writer may have overtaken.
        /// Each therefore writes only its own fields, and only while the row still exists and
        /// still carries `outcome`. `nil` — writing nothing — otherwise, so a swept row stays swept.
        private func updateEndedTask(_ id: String, endedAs outcome: Orchestrator.State,
                                     _ change: (inout Orchestrator.Task) -> Void)
            -> Orchestrator.Task? {
            guard var task = OrchestratorRegistry.tasks[id], task.state == outcome else {
                return nil
            }
            change(&task)
            OrchestratorRegistry.tasks[id] = task
            noteTaskMutation()
            return task
        }

        /// The claimed paths the ending found untouched. Returns the row as written.
        func recordUntouchedClaims(from staged: Orchestrator.Task,
                                   endedAs outcome: Orchestrator.State) -> Orchestrator.Task? {
            updateEndedTask(staged.id, endedAs: outcome) {
                $0.untouchedClaims = staged.untouchedClaims
            }
        }

        /// What a verified `result.json` added: each field only where the row as it stands still
        /// lacks it, and the receipt time only once.
        func adoptResultReceipt(from staged: Orchestrator.Task,
                                endedAs outcome: Orchestrator.State) -> Orchestrator.Task? {
            updateEndedTask(staged.id, endedAs: outcome) { task in
                if task.summary == nil { task.summary = staged.summary }
                if task.artifacts.isEmpty { task.artifacts = staged.artifacts }
                if task.verification == nil { task.verification = staged.verification }
                if task.review == nil { task.review = staged.review }
                task.resultVerifiedAt = task.resultVerifiedAt ?? staged.resultVerifiedAt
            }
        }

        func recordHarvestedUsage(from staged: Orchestrator.Task,
                                  endedAs outcome: Orchestrator.State) -> Orchestrator.Task? {
            updateEndedTask(staged.id, endedAs: outcome) { $0.usage = staged.usage }
        }

        /// The two reclaim deadlines, written whole because they are this stage's own fields.
        func scheduleReclaim(from staged: Orchestrator.Task,
                             endedAs outcome: Orchestrator.State) -> Orchestrator.Task? {
            updateEndedTask(staged.id, endedAs: outcome) {
                $0.workCleanupAt = staged.workCleanupAt
                $0.buildCleanupAt = staged.buildCleanupAt
            }
        }

        /// A step walker's candidate — a value copy taken before terminal or filesystem work, whose
        /// state the caller has compared in this hold — replacing the row it came from. The facts
        /// that have writers of their own and that no walker writes are taken from the row as it
        /// stands, so a copy taken before a progress note, a notification, a claim release, a
        /// landing declaration, a completion envelope or a finalization fact cannot erase it. The
        /// walkers' own fields — state, identity, inject counters, deadlines, executor receipts,
        /// interventions — are still the candidate's, and last-writer-wins among walkers.
        /// Returns the row as written, or `nil` — writing nothing — when the row is gone.
        @discardableResult
        func commitStepCandidate(_ candidate: Orchestrator.Task) -> Orchestrator.Task? {
            guard let current = OrchestratorRegistry.tasks[candidate.id] else { return nil }
            var merged = candidate
            merged.progress = current.progress
            merged.progressFileNote = current.progressFileNote
            merged.notifyCount = current.notifyCount
            merged.releasedClaims = current.releasedClaims
            merged.landing = current.landing
            merged.completionDelivery = current.completionDelivery
            merged.untouchedClaims = current.untouchedClaims
            merged.usage = current.usage
            merged.review = current.review
            merged.resultVerifiedAt = current.resultVerifiedAt
            OrchestratorRegistry.tasks[candidate.id] = merged
            noteTaskMutation()
            return merged
        }

        /// The release-claims route: append the paths not already released, while the row still
        /// has the state the route checked. `false` — writing nothing — otherwise.
        func releaseClaims(_ id: String, expecting state: Orchestrator.State,
                           _ paths: [String], at now: Date) -> Bool {
            guard var task = OrchestratorRegistry.tasks[id], task.state == state else {
                return false
            }
            let released = Set(task.releasedClaims.map(\.path))
            task.releasedClaims += paths.filter { !released.contains($0) }
                .map { Orchestrator.ReleasedClaim(path: $0, releasedAt: now) }
            OrchestratorRegistry.tasks[id] = task
            noteTaskMutation()
            return true
        }

        /// Restart reconciliation's rollback after its save failed: each reconciled row gets back
        /// the executor receipt it carried before, but only while it still exists and still carries
        /// the receipt reconciliation wrote. One field per row, never the table.
        func restoreExecutorReceipts(from prior: [String: Orchestrator.Task],
                                     wherever written: [String: Orchestrator.Task]) {
            var restored = false
            for (id, wrote) in written {
                guard var task = OrchestratorRegistry.tasks[id],
                      task.executorReceipt == wrote.executorReceipt else { continue }
                task.executorReceipt = prior[id]?.executorReceipt
                OrchestratorRegistry.tasks[id] = task
                restored = true
            }
            if restored { noteTaskMutation() }
        }

        /// Completion recovery's rollback after its save failed, at startup or on the reconcile
        /// route: an envelope it created or re-armed that is still unpersisted goes back to what
        /// the row carried before, and a result receipt it stamped is withdrawn. Per field, on rows
        /// that still exist; never the table.
        func restoreCompletionRecovery(from before: [String: Orchestrator.Task],
                                       envelopes: [String], resultReceipts: [String]) {
            var restored = false
            for id in envelopes {
                guard var task = OrchestratorRegistry.tasks[id],
                      task.completionDelivery?.persisted == false else { continue }
                task.completionDelivery = before[id]?.completionDelivery
                OrchestratorRegistry.tasks[id] = task
                restored = true
            }
            for id in resultReceipts {
                guard var task = OrchestratorRegistry.tasks[id] else { continue }
                task.resultVerifiedAt = before[id]?.resultVerifiedAt
                OrchestratorRegistry.tasks[id] = task
                restored = true
            }
            if restored { noteTaskMutation() }
        }

        // MARK: Same-hold read-modify-write — row-scoped, not closed

        /// Replace a row only while it still exists, with a candidate computed from the row read
        /// **in this same hold**. `false` means nothing was written. A candidate carried in from an
        /// earlier hold belongs in ``commitStepCandidate(_:)`` or a named transition instead.
        @discardableResult
        func commitTask(_ candidate: Orchestrator.Task) -> Bool {
            guard OrchestratorRegistry.tasks[candidate.id] != nil else { return false }
            OrchestratorRegistry.tasks[candidate.id] = candidate
            noteTaskMutation()
            return true
        }

        /// Change fields of one existing row. Returns the row as written, or `nil` — writing
        /// nothing — when the row is absent. `Task.id` is a `let`, so a change cannot re-key it.
        @discardableResult
        func updateTask(_ id: String, _ change: (inout Orchestrator.Task) -> Void)
            -> Orchestrator.Task? {
            guard var task = OrchestratorRegistry.tasks[id] else { return nil }
            change(&task)
            OrchestratorRegistry.tasks[id] = task
            noteTaskMutation()
            return task
        }

        @discardableResult
        func removeTask(_ id: String) -> Orchestrator.Task? {
            guard let removed = OrchestratorRegistry.tasks.removeValue(forKey: id) else {
                return nil
            }
            noteTaskMutation()
            return removed
        }

        func removeTasks<S: Sequence>(_ ids: S) where S.Element == String {
            var removed = false
            for id in ids where OrchestratorRegistry.tasks.removeValue(forKey: id) != nil {
                removed = true
            }
            if removed { noteTaskMutation() }
        }

        /// `load()` only: the decoded table replaces the held one whole. A rollback restores the
        /// fields it wrote through its own transition rather than a table taken earlier.
        func replaceAllTasks(_ found: [String: Orchestrator.Task]) {
            OrchestratorRegistry.tasks = found
            noteTaskMutation()
        }

        /// Test fixtures only: the one upsert. Production creates a row through ``admitTask(_:)``.
        func seedTaskForTesting(_ task: Orchestrator.Task) {
            OrchestratorRegistry.tasks[task.id] = task
            noteTaskMutation()
        }

        func removeAllTasks() {
            OrchestratorRegistry.tasks = [:]
            noteTaskMutation()
        }

        func consumeTaskMutation(after observed: inout Int) -> Bool {
            let current = OrchestratorRegistry.taskMutationGeneration
            guard current != observed else { return false }
            observed = current
            return true
        }
    }

    /// Run `body` under the lock with the primary registry capability: graph admissions, the
    /// terminal projection, handoff titles, suppressed labels and the rate windows.
    static func withTransaction<R>(_ body: (Transaction) -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body(Transaction())
    }

    /// Acquire the registry lock and expose only the per-session record capability, for a region
    /// that touches secrets, deliveries, self-states or in-flight handoffs and nothing else.
    static func withSessionRecords<R>(_ body: (SessionRecordsTransaction) -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body(SessionRecordsTransaction())
    }

    /// Acquire the registry lock and expose only the coordination-record capability. This is the
    /// door for every region that touches handoffs, labels, assignments or waits and nothing
    /// else; the caller performs its effects after it returns, outside the lock.
    static func withCoordinationRecords<R>(_ body: (CoordinationRecordsTransaction) -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body(CoordinationRecordsTransaction())
    }

    /// Acquire the registry lock and expose task records, together with the narrower
    /// capabilities a region must reach atomically with them.
    ///
    /// **There is no held-lock door any more.** `withTransactionOnHeldLock`,
    /// `withSessionRecordsOnHeldLock` and `withCoordinationRecordsOnHeldLock` ran their bodies
    /// without acquiring anything and trusted the caller to hold the lock — the `…Locked()` suffix
    /// under another name, checked by nothing. W1-4 converged every one of their call sites onto
    /// this door or one of the three above. The lock is still the single non-recursive `NSLock`
    /// `Orchestrator.lock` aliases, so a body must never enter a door again: the registry helpers
    /// that used to require "caller holds the lock" now take a capability parameter, so a caller
    /// must have one to call them. `Orchestrator.noteActivityLocked`, which touches only
    /// `Orchestrator`'s own activity counters, still carries the old unchecked convention.
    @discardableResult
    static func withTaskRecords<R>(_ body: (TaskRecordsTransaction) throws -> R) rethrows -> R {
        lock.lock()
        defer { lock.unlock() }
        return try body(TaskRecordsTransaction())
    }
}
