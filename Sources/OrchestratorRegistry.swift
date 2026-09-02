import Foundation

/// The registry's state owner: one lock, and the collections it protects.
///
/// `Orchestrator` had no owner. It had one `NSLock`, about 160 bare `lock.lock()` sites, roughly
/// nineteen `static var` collections behind them, and ten `…Locked()` functions whose contract was
/// enforced by nothing but the suffix in their names. This type is where that convention becomes a
/// boundary: the collections below are `private` to this file, so no other file can name them, and
/// the only way to reach one is through a ``Transaction`` — a token whose initializer is
/// `fileprivate`, so no other file can make one either. `Orchestrator` can no longer write
/// `titlesByTerminal[x] = y` outside a transaction; that line does not compile anywhere else.
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
/// **Two doors, and the second one is the migration showing.** ``withTransaction(_:)`` acquires
/// the lock; ``withTransactionOnHeldLock(_:)`` does not, because its callers are the bare
/// `lock.lock()` regions this stage has not converged yet. Neither door lets a caller past the
/// collections' `private`, which is what makes bypass impossible rather than discouraged; what the
/// second door still trusts a caller for is the *acquisition*, exactly as the `…Locked()` suffix
/// did. Later cuts move those regions onto the first door and the second one goes away.
enum OrchestratorRegistry {

    /// The one lock. Every collection in this file, and every collection still declared in
    /// `Orchestrator`, is behind this exact instance.
    static let lock = NSLock()

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

    /// Terminal id → where that tab sits in the tree. Rebuilt beside ``titlesByTerminal``.
    private static var rolesByTerminal: [String: Orchestrator.Role] = [:]

    // MARK: - The transaction

    /// The only handle to the collections above.
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
    }

    /// Run `body` under the lock. The transaction it is handed is the only way to reach the
    /// collections, and it is released with the lock.
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
    /// token, so a caller that gets this wrong is unsynchronized rather than also unbounded. Every
    /// use is inside a `lock.lock()` region in the same function or its caller, and each one is a
    /// site a later cut converges onto ``withTransaction(_:)``.
    static func withTransactionOnHeldLock<R>(_ body: (Transaction) -> R) -> R {
        body(Transaction())
    }
}
