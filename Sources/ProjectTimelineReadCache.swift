import Foundation

/// A bounded materialized Timeline projection. GET never invokes Git, transcript readers, or AI.
final class ProjectTimelineReadCache {
    typealias Envelope = [String: Any]
    typealias Executor = (@escaping () -> Void) -> Void
    typealias Refresh = () -> Result<Model, Failure>

    struct Failure: Error { let code: String; let message: String }
    struct Query {
        let project: String?
        let entry: String?
        let cursor: Int
        let environment: String
        let category: String?
        let includeUpcoming: Bool
    }
    struct Model {
        let revision: Int
        let observedAt: Double
        let envelope: Envelope
    }
    struct State { let inFlight: Bool; let generation: UInt64; let refreshStarts: Int; let successorPending: Bool }

    static let pageSize = 40
    private let lock = NSLock()
    private let execute: Executor
    private var model: Model?
    private var enabled: Bool?
    private var inFlight = false
    private var generation: UInt64 = 0
    private var refreshStarts = 0
    private var successor: Refresh?
    private var failure: Failure?

    init(execute: @escaping Executor = { work in
        DispatchQueue(label: "clawdline.timeline.reconcile", qos: .utility).async(execute: work)
    }) { self.execute = execute }

    func seed(_ value: Model) {
        lock.lock(); defer { lock.unlock() }
        guard model == nil else { return }
        model = value
    }

    func read(_ query: Query, header: ProjectTimelineStore.ReadHeader,
              loading: @autoclosure () -> Envelope, refresh: @escaping Refresh) -> Envelope {
        var start: (UInt64, Refresh)?
        lock.lock()
        synchronize(enabled: header.enabled)
        let behind = model.map { $0.revision != header.revision } ?? true
        if header.enabled && !inFlight && behind {
            inFlight = true; refreshStarts += 1; start = (generation, refresh)
        }
        let status = model == nil ? (failure == nil ? "loading" : "error")
            : (behind || inFlight || failure != nil ? "stale" : "ready")
        var answer = select(model?.envelope ?? loading(), query: query)
        stamp(&answer, status: status, header: header, model: model, failure: failure)
        lock.unlock()
        if let start { launch(start.1, generation: start.0) }
        return answer
    }

    func refresh(header: ProjectTimelineStore.ReadHeader, work: @escaping Refresh) {
        var start: (UInt64, Refresh)?
        lock.lock()
        synchronize(enabled: header.enabled)
        guard header.enabled else { lock.unlock(); return }
        if inFlight { successor = work }
        else { inFlight = true; refreshStarts += 1; start = (generation, work) }
        lock.unlock()
        if let start { launch(start.1, generation: start.0) }
    }

    var stateForTesting: State {
        lock.lock(); defer { lock.unlock() }
        return State(inFlight: inFlight, generation: generation, refreshStarts: refreshStarts,
                     successorPending: successor != nil)
    }

    private func synchronize(enabled value: Bool) {
        guard enabled != value else { return }
        enabled = value; generation &+= 1; successor = nil; failure = nil
    }

    private func launch(_ work: @escaping Refresh, generation owned: UInt64) {
        execute { [weak self] in self?.complete(work(), generation: owned) }
    }

    private func complete(_ result: Result<Model, Failure>, generation owned: UInt64) {
        var next: (UInt64, Refresh)?
        lock.lock()
        inFlight = false
        if generation == owned, enabled == true {
            switch result {
            case .success(let value): model = value; failure = nil
            case .failure(let value): failure = value
            }
            if let pending = successor {
                successor = nil; inFlight = true; refreshStarts += 1; next = (generation, pending)
            }
        }
        lock.unlock()
        if let next { launch(next.1, generation: next.0) }
    }

    private func select(_ source: Envelope, query: Query) -> Envelope {
        guard var timeline = source["timeline"] as? Envelope else { return source }
        let all = timeline["entries"] as? [Envelope] ?? []
        let checkpoints = timeline["checkpoints"] as? [Envelope] ?? []
        let selectedCheckpoints = checkpoints.filter {
            query.project == nil || $0["projectId"] as? String == query.project
        }
        timeline["checkpoints"] = selectedCheckpoints
        let issues = (timeline["historySourceIssues"] as? [Envelope] ?? []).filter {
            query.project == nil || $0["projectId"] as? String == query.project
        }
        timeline["historySourceIssues"] = issues
        let reasons = selectedCheckpoints.compactMap { $0["reason"] as? String }
            + issues.compactMap { $0["code"] as? String }
        // A checkpoint subset cannot establish the complete Project domain on this Mac.
        let complete = query.project != nil && !selectedCheckpoints.isEmpty && selectedCheckpoints.allSatisfy {
            $0["historyStatus"] as? String == "complete"
        } && issues.isEmpty
        timeline["coverage"] = ["status": selectedCheckpoints.isEmpty ? "unknown" : complete ? "complete" : "partial",
                                "reasons": Array(Set(reasons)).sorted()]
        let projected = all.map { projectedRow($0, environment: query.environment) }
            .sorted(by: projectedOrder)
        let matching = projected.filter { row in
            guard query.project == nil || row["projectId"] as? String == query.project else { return false }
            let projection = row["projection"] as? Envelope
            let status = projection?["status"] as? String ?? "unknown"
            if !query.includeUpcoming && ["upcoming", "landed_to_git", "unknown"].contains(status) { return false }
            if let category = query.category, row["primaryCategory"] as? String != category { return false }
            return true
        }
        let offset = min(query.cursor, matching.count)
        let end = min(offset + Self.pageSize, matching.count)
        timeline["entries"] = matching[offset..<end].map { compact($0) }
        timeline["selected"] = query.entry.flatMap { id in projected.first {
            $0["id"] as? String == id
                && (query.project == nil || $0["projectId"] as? String == query.project)
        } }
            as Any? ?? NSNull()
        timeline["nextCursor"] = end < matching.count ? String(end) : NSNull()
        if let project = query.project {
            let presentations = timeline["projects"] as? [String: Envelope]
            var presentation = presentations?[project] ?? ["label": project]
            presentation["id"] = project
            timeline["project"] = presentation
        } else {
            timeline["project"] = NSNull()
        }
        timeline.removeValue(forKey: "projects")
        timeline["filters"] = ["environment": query.environment,
            "category": query.category as Any? ?? NSNull(), "upcoming": query.includeUpcoming]
        return ["timeline": timeline]
    }

    private func compact(_ row: Envelope) -> Envelope {
        var value = row
        value.removeValue(forKey: "events")
        value.removeValue(forKey: "projections")
        return value
    }

    private func projectedRow(_ row: Envelope, environment: String) -> Envelope {
        guard environment != "all", let projections = row["projections"] as? [String: Envelope] else {
            return row
        }
        var value = row
        value["projection"] = projections[environment] ?? ["status": "unknown",
            "coverage": "unknown", "basisEventIds": [] as [String], "availableTargets": 0,
            "requiredTargets": 0, "effectiveAt": NSNull(), "observedAt": NSNull()]
        return value
    }

    private func projectedOrder(_ lhs: Envelope, _ rhs: Envelope) -> Bool {
        func moment(_ row: Envelope) -> Double {
            let projection = row["projection"] as? Envelope
            return projection?["effectiveAt"] as? Double
                ?? projection?["observedAt"] as? Double
                ?? row["createdAt"] as? Double
                ?? 0
        }
        let left = moment(lhs), right = moment(rhs)
        return left == right
            ? (lhs["id"] as? String ?? "") < (rhs["id"] as? String ?? "")
            : left > right
    }

    private func stamp(_ envelope: inout Envelope, status: String,
                       header: ProjectTimelineStore.ReadHeader, model: Model?, failure: Failure?) {
        guard var timeline = envelope["timeline"] as? Envelope else { return }
        timeline["status"] = status
        timeline["enabled"] = header.enabled
        timeline["storeRevision"] = header.revision
        timeline["modelRevision"] = model?.revision as Any? ?? NSNull()
        timeline["observedAt"] = model?.observedAt as Any? ?? NSNull()
        if let failure { timeline["error"] = ["code": failure.code, "message": failure.message] }
        envelope["timeline"] = timeline
    }
}
