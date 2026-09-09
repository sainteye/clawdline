import Foundation

/// Serves the materialized Board read model without ever joining the reconciliation work.
///
/// The cache owns freshness and single-flight state, not lifecycle truth.  Its Model contains
/// projections derived from ProjectBoardStore and UsageLedger; those owners remain authoritative.
final class ProjectBoardReadCache {
    typealias Envelope = [String: Any]
    typealias Executor = (@escaping () -> Void) -> Void

    struct DirtyReasons: OptionSet {
        let rawValue: UInt8

        static let catalog = DirtyReasons(rawValue: 1 << 0)
        static let durableModel = DirtyReasons(rawValue: 1 << 1)
        static let usage = DirtyReasons(rawValue: 1 << 2)
        static let modeCatchUp = DirtyReasons(rawValue: 1 << 3)
    }

    typealias Refresh = (DirtyReasons) -> Result<Model, Failure>

    struct Failure: Error {
        let code: String
        let message: String
    }

    struct Model {
        typealias ItemResolver = (_ item: String, _ project: String?) -> Envelope?

        let revision: Int
        let observedAt: Double
        let catalog: Envelope
        let projects: [String: Envelope]
        let items: [String: Envelope]
        private let resolveItem: ItemResolver?
        private let resolveSession: ((String) -> Envelope)?

        init(revision: Int, observedAt: Double, catalog: Envelope,
             projects: [String: Envelope], items: [String: Envelope],
             resolveItem: ItemResolver? = nil,
             resolveSession: ((String) -> Envelope)? = nil) {
            self.revision = revision
            self.observedAt = observedAt
            self.catalog = catalog
            self.projects = projects
            self.items = items
            self.resolveItem = resolveItem
            self.resolveSession = resolveSession
        }

        func envelope(project: String?, item: String?) -> Envelope {
            if project == nil, let item, item.hasPrefix("session:"), let resolveSession {
                return resolveSession(String(item.dropFirst("session:".count)))
            }
            if let item {
                if let value = items[item] ?? resolveItem?(item, project),
                   itemProject(in: value) == project || project == nil {
                    return value
                }
                var value = project.flatMap { projects[$0] } ?? unknownProject(project)
                if var board = value["board"] as? [String: Any] {
                    board["items"] = [] as [[String: Any]]
                    board["item"] = NSNull()
                    value["board"] = board
                }
                return value
            }
            if let project {
                return projects[project] ?? unknownProject(project)
            }
            return catalog
        }

        private func unknownProject(_ project: String?) -> Envelope {
            var value = catalog
            var board = value["board"] as? [String: Any] ?? [:]
            board["items"] = [] as [[String: Any]]
            board["item"] = NSNull()
            let scope: [String: Any] = [
                "status": "unknown",
                "projectId": project as Any? ?? NSNull(),
                "coverage": "incomplete",
            ]
            board["scope"] = scope
            value["board"] = board
            return value
        }

        private func itemProject(in envelope: Envelope) -> String? {
            let board = envelope["board"] as? [String: Any]
            return (board?["item"] as? [String: Any])?["projectId"] as? String
        }
    }

    struct State {
        let inFlight: Bool
        let generation: UInt64
        let refreshStarts: Int
        let pendingReasons: DirtyReasons
    }

    private let lock = NSLock()
    private let retryAfter: TimeInterval
    private let clock: () -> Date
    private let execute: Executor
    private var model: Model?
    private var enabled: Bool?
    private var generation: UInt64 = 0
    private var inFlight = false
    private var attemptedAt: Double?
    private var attemptedRevision: Int?
    private var failure: Failure?
    private var refreshStarts = 0
    private var pendingRefresh: Refresh?
    private var pendingReasons: DirtyReasons = []
    private var pendingRevision: Int?

    /// `freshFor` is retained as the initializer label for source compatibility. It now bounds
    /// retries after a failed source refresh; elapsed time alone never invalidates a good model.
    init(freshFor: TimeInterval = 10,
         clock: @escaping () -> Date = Date.init,
         execute: @escaping Executor = { work in
             DispatchQueue(label: "clawdline.board.reconcile", qos: .utility).async(execute: work)
         }) {
        precondition(freshFor >= 0)
        self.retryAfter = freshFor
        self.clock = clock
        self.execute = execute
    }

    /// Seed the process from the store's startup materialization.  A stale seed remains useful:
    /// its explicit status prevents old counters from being mistaken for current ones.
    func seed(_ value: Model) {
        lock.lock(); defer { lock.unlock() }
        guard model == nil else { return }
        model = value
    }

    /// Mode changes are synchronous and generation-fenced.  Work already running may finish, but
    /// can no longer publish after OFF, and OFF never starts inference or reconciliation.
    func setEnabled(_ value: Bool) {
        lock.lock(); defer { lock.unlock() }
        guard enabled != value else { return }
        enabled = value
        generation &+= 1
        pendingRefresh = nil
        pendingReasons = []
        pendingRevision = nil
        attemptedAt = nil
        attemptedRevision = nil
        failure = nil
    }

    func read(project: String?, item: String?, header: ProjectBoardStore.ReadHeader,
              loading: @autoclosure () -> Envelope,
              refresh: @escaping Refresh) -> Envelope {
        var start: (UInt64, DirtyReasons, Refresh)?
        let now = clock().timeIntervalSince1970
        lock.lock()
        if enabled != header.enabled {
            enabled = header.enabled
            generation &+= 1
            pendingRefresh = nil
            pendingReasons = []
            pendingRevision = nil
            attemptedAt = nil
            attemptedRevision = nil
            failure = nil
        }
        if !header.enabled {
            let retained = model?.envelope(project: project, item: item)
            let revisionBehind = model.map { $0.revision != header.revision } ?? true
            let answer = stamped(retained ?? loading(),
                                 status: retained == nil ? "loading" : (revisionBehind ? "stale" : "ready"),
                                 header: header, attemptedAt: attemptedAt,
                                 observedAt: model?.observedAt, modelRevision: model?.revision,
                                 failure: nil)
            lock.unlock()
            return answer
        }

        let revisionBehind = (model?.revision ?? attemptedRevision).map {
            $0 != header.revision
        } ?? false
        let firstAttempt = model == nil && attemptedRevision == nil
        let retryDue = failure != nil && attemptedAt.map { now - $0 >= retryAfter } == true
        if !inFlight && (firstAttempt || revisionBehind || retryDue) {
            start = admitLocked([.catalog, .durableModel], refresh,
                                revision: header.revision, at: now)
        }
        let status: String
        if model == nil { status = failure == nil ? "loading" : "error" }
        else if inFlight || failure != nil || revisionBehind { status = "stale" }
        else { status = "ready" }
        let answer = stamped(model?.envelope(project: project, item: item) ?? loading(),
                             status: status, header: header, attemptedAt: attemptedAt,
                             observedAt: model?.observedAt, modelRevision: model?.revision,
                             failure: failure)
        lock.unlock()

        if let (refreshGeneration, reasons, work) = start {
            launch(work, reasons: reasons, generation: refreshGeneration)
        }
        return answer
    }

    /// Event-driven invalidation. While a refresh is running, its typed dirty reasons form a union.
    /// One consolidated successor consumes that union; a later event can never erase an earlier
    /// catalog, durable-model, usage, or mode catch-up obligation.
    func refresh(header: ProjectBoardStore.ReadHeader, reasons: DirtyReasons,
                 work: @escaping Refresh) {
        guard !reasons.isEmpty else { return }
        let now = clock().timeIntervalSince1970
        var start: (UInt64, DirtyReasons, Refresh)?
        lock.lock()
        if enabled != header.enabled {
            enabled = header.enabled
            generation &+= 1
            pendingRefresh = nil
            pendingReasons = []
            pendingRevision = nil
            attemptedAt = nil
            attemptedRevision = nil
            failure = nil
        }
        guard header.enabled else { lock.unlock(); return }
        if inFlight {
            pendingRefresh = work
            pendingReasons.formUnion(reasons)
            pendingRevision = header.revision
        } else {
            start = admitLocked(reasons, work, revision: header.revision, at: now)
        }
        lock.unlock()
        if let (refreshGeneration, reasons, work) = start {
            launch(work, reasons: reasons, generation: refreshGeneration)
        }
    }

    private func admitLocked(_ reasons: DirtyReasons, _ work: @escaping Refresh,
                             revision: Int, at: Double)
        -> (UInt64, DirtyReasons, Refresh) {
        inFlight = true
        attemptedAt = at
        attemptedRevision = revision
        refreshStarts += 1
        return (generation, reasons, work)
    }

    private func launch(_ work: @escaping Refresh, reasons: DirtyReasons, generation: UInt64) {
        execute { [weak self] in
            let result = work(reasons)
            self?.complete(result, generation: generation)
        }
    }

    private func complete(_ result: Result<Model, Failure>, generation completedGeneration: UInt64) {
        var next: (UInt64, DirtyReasons, Refresh)?
        lock.lock()
        inFlight = false
        if completedGeneration == generation, enabled == true {
            switch result {
            case .success(let value):
                model = value
                failure = nil
            case .failure(let error):
                failure = error
            }
        }
        if enabled == true, let pending = pendingRefresh {
            pendingRefresh = nil
            let reasons = pendingReasons
            let revision = pendingRevision ?? attemptedRevision ?? model?.revision ?? 0
            pendingReasons = []
            pendingRevision = nil
            next = admitLocked(reasons, pending, revision: revision,
                               at: clock().timeIntervalSince1970)
        }
        lock.unlock()
        if let (nextGeneration, reasons, work) = next {
            launch(work, reasons: reasons, generation: nextGeneration)
        }
    }

    private func stamped(_ envelope: Envelope, status: String,
                         header: ProjectBoardStore.ReadHeader,
                         attemptedAt: Double?, observedAt: Double?,
                         modelRevision: Int?, failure: Failure?) -> Envelope {
        var envelope = envelope
        var board = envelope["board"] as? [String: Any] ?? [:]
        let scopeUnknown = (board["scope"] as? [String: Any])?["status"] as? String == "unknown"
        // Mode is current store state, never a property an old refresh may restore.
        board["revision"] = header.revision
        board["enabled"] = header.enabled
        board["mode"] = header.enabled ? "board" : "standard"
        board["updatedAt"] = header.updatedAt
        var readState: [String: Any] = [
            "status": scopeUnknown ? "error" : status,
            "revision": modelRevision ?? header.revision,
            "observedAt": observedAt ?? NSNull(),
            "attemptedAt": attemptedAt ?? NSNull(),
            "refreshing": header.enabled && inFlight,
            "error": NSNull(),
        ]
        if let failure {
            readState["error"] = ["code": failure.code, "message": failure.message]
        } else if scopeUnknown {
            readState["error"] = ["code": "project_not_found",
                                  "message": "The requested Project is not in this Board model."]
        }
        board["readState"] = readState
        envelope["board"] = board
        return envelope
    }

    var stateForTesting: State {
        lock.lock(); defer { lock.unlock() }
        return State(inFlight: inFlight, generation: generation, refreshStarts: refreshStarts,
                     pendingReasons: pendingReasons)
    }

    func resetForTesting() {
        lock.lock(); defer { lock.unlock() }
        generation &+= 1
        model = nil
        enabled = nil
        inFlight = false
        pendingRefresh = nil
        pendingReasons = []
        pendingRevision = nil
        attemptedAt = nil
        attemptedRevision = nil
        failure = nil
        refreshStarts = 0
    }
}
