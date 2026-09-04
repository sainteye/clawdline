import Foundation

/// Pure graph and review contracts for `Orchestrator`. Keeping them outside the broker's stateful
/// implementation makes the stop-growth boundary real: parsing, validation and projection can
/// evolve without turning terminal lifecycle code into the product's control-sheet renderer.
extension Orchestrator {
    static let graphNodeLimit = 32

    enum GraphNodeKind: String, CaseIterable {
        case decision, delivery, review, correction, verification, landing
    }

    struct GraphNode: Equatable {
        let id: String
        let title: String
        let kind: GraphNodeKind
        let dependsOn: [String]
        let acceptance: [String]
    }

    struct PlanningGraph: Equatable {
        let id: String
        let destination: String
        let currentNode: String
        let nodes: [GraphNode]
        let unknowns: [String]
        let outOfScope: [String]

        func hasSameDefinition(as other: PlanningGraph) -> Bool {
            id == other.id && destination == other.destination && nodes == other.nodes
                && unknowns == other.unknowns && outOfScope == other.outOfScope
        }
    }

    enum ReviewVerdict: String {
        case safeToLand = "safe_to_land"
        case changesRequired = "changes_required"
    }

    enum ReviewAxisName: String, CaseIterable {
        case specification
        case repositoryInvariants = "repository_invariants"
        case runtimeFailureBehavior = "runtime_failure_behavior"
    }

    enum ReviewAxisStatus: String {
        case pass, findings
    }

    enum ReviewSeverity: String {
        case blocking, important, minor
    }

    struct ReviewFinding: Equatable {
        let id: String
        let severity: ReviewSeverity
        let summary: String
        let evidence: [String]
    }

    struct ReviewAxis: Equatable {
        let axis: ReviewAxisName
        let status: ReviewAxisStatus
        let findings: [ReviewFinding]
    }

    struct ReviewReceipt: Equatable {
        let verdict: ReviewVerdict
        let axes: [ReviewAxis]
    }

    private static func boundedPlanningStrings(
        _ raw: Any?, field: String, maximum: Int, length: Int, allowEmpty: Bool = true
    ) -> (values: [String]?, error: String?) {
        guard let rows = raw as? [Any], allowEmpty || !rows.isEmpty, rows.count <= maximum else {
            let amount = allowEmpty ? "0–\(maximum)" : "1–\(maximum)"
            return (nil, "\(field) must be an array of \(amount) non-empty strings")
        }
        var values: [String] = []
        for (index, rawValue) in rows.enumerated() {
            guard let value = rawValue as? String,
                  !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  value.count <= length else {
                return (nil, "\(field)[\(index)] must be a non-empty string of at most "
                    + "\(length) characters")
            }
            values.append(value)
        }
        return (values, nil)
    }

    static func planningGraph(from raw: Any)
        -> (graph: PlanningGraph?, error: String?) {
        guard let obj = raw as? [String: Any] else {
            return (nil, "graph must be an object")
        }
        let allowed = Set(["id", "destination", "current_node", "nodes", "unknowns",
                           "out_of_scope"])
        let unknown = Set(obj.keys).subtracting(allowed).sorted()
        guard unknown.isEmpty else {
            return (nil, "graph has unknown field: \(unknown.joined(separator: ", "))")
        }
        guard let id = obj["id"] as? String, UUID(uuidString: id) != nil,
              id == id.lowercased() else {
            return (nil, "graph.id must be a lowercase UUID")
        }
        guard let destination = obj["destination"] as? String,
              !destination.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              destination.count <= 500 else {
            return (nil, "graph.destination must be a non-empty string of at most 500 characters")
        }
        guard let currentNode = obj["current_node"] as? String,
              StartPoints.modelName(currentNode) == currentNode else {
            return (nil, "graph.current_node must be a 1–64 character lower-case node id")
        }
        guard let rows = obj["nodes"] as? [Any], !rows.isEmpty,
              rows.count <= graphNodeLimit else {
            return (nil, "graph.nodes must contain 1–\(graphNodeLimit) nodes")
        }
        var nodes: [GraphNode] = []
        var nodeIDs: Set<String> = []
        for (index, rawNode) in rows.enumerated() {
            guard let row = rawNode as? [String: Any] else {
                return (nil, "graph.nodes[\(index)] must be an object")
            }
            let nodeAllowed = Set(["id", "title", "kind", "depends_on", "acceptance"])
            let nodeUnknown = Set(row.keys).subtracting(nodeAllowed).sorted()
            guard nodeUnknown.isEmpty else {
                return (nil, "graph.nodes[\(index)] has unknown field: "
                    + nodeUnknown.joined(separator: ", "))
            }
            guard let nodeID = row["id"] as? String,
                  StartPoints.modelName(nodeID) == nodeID else {
                return (nil, "graph.nodes[\(index)].id must be a lower-case node id")
            }
            guard nodeIDs.insert(nodeID).inserted else {
                return (nil, "graph node id \(nodeID) is duplicated")
            }
            guard let title = row["title"] as? String,
                  !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  title.count <= 120 else {
                return (nil, "graph.nodes[\(index)].title must be 1–120 characters")
            }
            guard let rawKind = row["kind"] as? String,
                  let kind = GraphNodeKind(rawValue: rawKind) else {
                return (nil, "graph.nodes[\(index)].kind must be one of: "
                    + GraphNodeKind.allCases.map(\.rawValue).joined(separator: ", "))
            }
            let dependencies = boundedPlanningStrings(
                row["depends_on"], field: "graph.nodes[\(index)].depends_on",
                maximum: graphNodeLimit, length: 64)
            if let error = dependencies.error { return (nil, error) }
            let acceptance = boundedPlanningStrings(
                row["acceptance"], field: "graph.nodes[\(index)].acceptance",
                maximum: 8, length: 300, allowEmpty: false)
            if let error = acceptance.error { return (nil, error) }
            let dependsOn = dependencies.values ?? []
            if Set(dependsOn).count != dependsOn.count {
                return (nil, "graph.nodes[\(index)].depends_on must not contain duplicates")
            }
            nodes.append(GraphNode(id: nodeID, title: title, kind: kind,
                                   dependsOn: dependsOn,
                                   acceptance: acceptance.values ?? []))
        }
        guard nodes.contains(where: { $0.id == currentNode }) else {
            return (nil, "graph.current_node must name a node in this graph")
        }
        for node in nodes {
            for dependency in node.dependsOn {
                guard dependency != node.id, nodeIDs.contains(dependency) else {
                    return (nil, "graph node \(node.id) has an unknown or self dependency "
                        + dependency)
                }
            }
        }
        let byID = Dictionary(uniqueKeysWithValues: nodes.map { ($0.id, $0) })
        var visiting: Set<String> = []
        var visited: Set<String> = []
        func visit(_ nodeID: String) -> Bool {
            if visited.contains(nodeID) { return true }
            if !visiting.insert(nodeID).inserted { return false }
            for dependency in byID[nodeID]?.dependsOn ?? [] where !visit(dependency) {
                return false
            }
            visiting.remove(nodeID)
            visited.insert(nodeID)
            return true
        }
        guard nodes.allSatisfy({ visit($0.id) }) else {
            return (nil, "graph.nodes must be acyclic")
        }
        let unknowns = boundedPlanningStrings(obj["unknowns"] ?? [], field: "graph.unknowns",
                                              maximum: 8, length: 300)
        if let error = unknowns.error { return (nil, error) }
        let outOfScope = boundedPlanningStrings(obj["out_of_scope"] ?? [],
                                                field: "graph.out_of_scope",
                                                maximum: 8, length: 300)
        if let error = outOfScope.error { return (nil, error) }
        return (PlanningGraph(id: id, destination: destination, currentNode: currentNode,
                              nodes: nodes, unknowns: unknowns.values ?? [],
                              outOfScope: outOfScope.values ?? []), nil)
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


    static func review(from raw: Any?) -> ReviewReceipt? {
        guard let obj = raw as? [String: Any],
              Set(obj.keys) == Set(["verdict", "axes"]),
              let rawVerdict = obj["verdict"] as? String,
              let verdict = ReviewVerdict(rawValue: rawVerdict),
              let rows = obj["axes"] as? [Any], rows.count == ReviewAxisName.allCases.count
        else { return nil }
        var axes: [ReviewAxis] = []
        var names: Set<ReviewAxisName> = []
        for rawAxis in rows {
            guard let row = rawAxis as? [String: Any],
                  Set(row.keys) == Set(["axis", "status", "findings"]),
                  let rawName = row["axis"] as? String,
                  let name = ReviewAxisName(rawValue: rawName), names.insert(name).inserted,
                  let rawStatus = row["status"] as? String,
                  let status = ReviewAxisStatus(rawValue: rawStatus),
                  let rawFindings = row["findings"] as? [Any], rawFindings.count <= 32
            else { return nil }
            var findings: [ReviewFinding] = []
            var findingIDs: Set<String> = []
            for rawFinding in rawFindings {
                guard let finding = rawFinding as? [String: Any],
                      Set(finding.keys) == Set(["id", "severity", "summary", "evidence"]),
                      let findingID = finding["id"] as? String,
                      StartPoints.modelName(findingID.lowercased()) == findingID.lowercased(),
                      findingID.count <= 64, findingIDs.insert(findingID).inserted,
                      let rawSeverity = finding["severity"] as? String,
                      let severity = ReviewSeverity(rawValue: rawSeverity),
                      let summary = finding["summary"] as? String,
                      !summary.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                      summary.count <= 500 else { return nil }
                let evidence = boundedPlanningStrings(
                    finding["evidence"], field: "review evidence", maximum: 8,
                    length: 500, allowEmpty: false)
                guard evidence.error == nil else { return nil }
                findings.append(ReviewFinding(id: findingID, severity: severity,
                                              summary: summary,
                                              evidence: evidence.values ?? []))
            }
            guard (status == .pass && findings.isEmpty)
                    || (status == .findings && !findings.isEmpty) else { return nil }
            axes.append(ReviewAxis(axis: name, status: status, findings: findings))
        }
        guard names == Set(ReviewAxisName.allCases) else { return nil }
        let hasFindings = axes.contains { !$0.findings.isEmpty }
        guard (verdict == .safeToLand && !hasFindings)
                || (verdict == .changesRequired && hasFindings) else { return nil }
        return ReviewReceipt(verdict: verdict, axes: axes)
    }

    typealias GraphTaskIndex = [String: [String: Task]]

    static func graphTaskIndex(_ indexed: [String: Task]) -> GraphTaskIndex {
        var result: GraphTaskIndex = [:]
        for task in indexed.values {
            guard let graph = task.graph else { continue }
            let previous = result[graph.id]?[graph.currentNode]
            if previous.map({ $0.created < task.created }) ?? true {
                result[graph.id, default: [:]][graph.currentNode] = task
            }
        }
        return result
    }

    static func graphTaskIndex() -> GraphTaskIndex {
        lock.lock(); let indexed = tasks; lock.unlock()
        return graphTaskIndex(indexed)
    }

    static func graphNodeTask(_ node: GraphNode, graphID: String,
                              index: GraphTaskIndex) -> Task? {
        index[graphID]?[node.id]
    }

    /// Whether the work a landing node is responsible for is already recorded as landed.
    ///
    /// A landing node never has a task of its own. `AGENTS.md` gives landing to the root that
    /// dispatched the graph, and a root does not dispatch itself, so the node this reads was
    /// looking at an empty slot: 23 landing nodes across the graphs on this machine, none with a
    /// task, every one of them `planned` or `blocked` forever — while 66 real landings sat
    /// recorded, verified commit and all, on the delivery tasks beside them. The receipt was never
    /// missing. It was being read in the one place a root never writes it.
    ///
    /// So it is read where the root does write it: on the byte-producing nodes this landing node
    /// depends on, transitively, because the ordinary shape puts a review or a verification
    /// between the two. The two producing kinds are not treated alike, and the difference is the
    /// whole design decision here:
    ///
    /// - A `delivery` node must have a task and that task must carry a `landed` receipt. A graph
    ///   that declared two implementations and ran one has not landed, and one of them did in the
    ///   record this was measured against.
    /// - A `correction` node counts only when it has one. Corrections are contingent by
    ///   construction — a review demands them or it does not — so treating an undispatched
    ///   correction as missing evidence would re-create exactly the bug above one node along, in
    ///   every graph whose review found nothing.
    ///
    /// And at least one landed receipt is required, so a landing node that depends on nothing that
    /// produces bytes stays honest rather than passing on an empty conjunction. Two such graphs
    /// exist in the record.
    ///
    /// The producing task's own state is deliberately not consulted. What is being asserted is
    /// that the bytes are on the target branch, and the thing that asserts it is the landing
    /// receipt the root wrote after resolving the commit against the named local target — three of
    /// the landed receipts here belong to tasks that timed out or failed after their work was
    /// already integrated.
    static func graphLandingEvidence(_ node: GraphNode, graph: PlanningGraph,
                                     index: GraphTaskIndex) -> Bool {
        let byID = Dictionary(graph.nodes.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in
            first
        })
        var seen: Set<String> = []
        var pending = node.dependsOn
        var landed = 0
        while let dependencyID = pending.popLast() {
            guard seen.insert(dependencyID).inserted,
                  let dependency = byID[dependencyID] else { continue }
            pending.append(contentsOf: dependency.dependsOn)
            guard dependency.kind == .delivery || dependency.kind == .correction else { continue }
            guard let producer = graphNodeTask(dependency, graphID: graph.id, index: index) else {
                if dependency.kind == .delivery { return false }
                continue
            }
            guard producer.landing?.state == .landed else { return false }
            landed += 1
        }
        return landed > 0
    }

    static func graphNodeOutcome(_ node: GraphNode, graph: PlanningGraph,
                                 index: GraphTaskIndex) -> String {
        guard let task = graphNodeTask(node, graphID: graph.id, index: index) else {
            guard node.kind == .landing,
                  graphLandingEvidence(node, graph: graph, index: index) else { return "planned" }
            return "done"
        }
        if !task.state.isTerminal { return "active" }
        guard task.state == .success else { return "failed" }
        switch node.kind {
        case .review:
            return task.review?.verdict == .safeToLand ? "done" : "failed"
        case .verification:
            return task.verification?.last == .pass ? "done" : "failed"
        case .landing:
            // A landing task of its own does not move where the receipt lives: `CHILD.md` still
            // forbids a child from calling its own `/landing` route, so the root records the
            // landing against the delivery either way.
            if task.landing?.state == .landed { return "done" }
            return graphLandingEvidence(node, graph: graph, index: index)
                ? "done" : "awaiting_landing"
        default:
            return "done"
        }
    }

    /// Only nodes on the current frontier may enter the broker. The dependency reading is made
    /// from durable task/review/landing receipts; no `ready` flag supplied by the caller exists.
    static func graphAdmissionKey(_ graph: PlanningGraph) -> String {
        "\(graph.id)/\(graph.currentNode)"
    }

    static func releaseGraphAdmission(_ key: String) {
        OrchestratorRegistry.withTransaction { $0.releaseGraphAdmission(key) }
    }

    static func graphAdmissionRefusal(_ graph: PlanningGraph, taskID: String,
                                      reserve: Bool = false) -> Reply? {
        lock.lock(); defer { lock.unlock() }
        let key = graphAdmissionKey(graph)
        if let holder = OrchestratorRegistry
            .withTransactionOnHeldLock({ $0.graphAdmission(forKey: key) }) {
            return .refused(status: 409, code: "graph_node_active",
                            message: "Another dispatch is already admitting this graph node.",
                            extra: ["graph_id": graph.id, "node_id": graph.currentNode,
                                    "task_id": holder.taskID])
        }
        let pendingAdmissions = OrchestratorRegistry
            .withTransactionOnHeldLock { $0.graphAdmissions() }
        for pending in pendingAdmissions where pending.graph.id == graph.id {
            guard pending.graph.hasSameDefinition(as: graph) else {
                return .refused(409, "graph_definition_conflict",
                                "Another dispatch is admitting a different definition for this graph id.")
            }
        }
        let indexed = tasks
        let taskIndex = graphTaskIndex(indexed)

        for existing in indexed.values where existing.graph?.id == graph.id {
            guard let existingGraph = existing.graph,
                  existingGraph.hasSameDefinition(as: graph) else {
                return .refused(409, "graph_definition_conflict",
                                "Another task already carries a different definition for this graph id.")
            }
        }
        guard let current = graph.nodes.first(where: { $0.id == graph.currentNode }) else {
            return .refused(422, "bad_task", "graph.current_node does not exist.")
        }
        let currentOutcome = graphNodeOutcome(current, graph: graph, index: taskIndex)
        if currentOutcome == "active" || currentOutcome == "awaiting_landing" {
            return .refused(status: 409, code: "graph_node_active",
                            message: "This graph node already has an active task or landing obligation.",
                            extra: ["graph_id": graph.id, "node_id": graph.currentNode])
        }
        if currentOutcome == "done" {
            return .refused(status: 409, code: "graph_node_complete",
                            message: "This graph node already has durable completion evidence.",
                            extra: ["graph_id": graph.id, "node_id": graph.currentNode])
        }
        var blockers: [[String: Any]] = []
        var failed = false
        for dependencyID in current.dependsOn {
            guard let dependency = graph.nodes.first(where: { $0.id == dependencyID }) else {
                continue // Parser already proves this cannot happen.
            }
            let outcome = graphNodeOutcome(dependency, graph: graph, index: taskIndex)
            guard outcome != "done" else { continue }
            if outcome == "failed" { failed = true }
            let state = outcome == "planned" ? "blocked" : outcome
            var blocker: [String: Any] = ["node_id": dependency.id, "state": state]
            if let dependencyTask = graphNodeTask(dependency, graphID: graph.id,
                                                  index: taskIndex) {
                blocker["task_id"] = dependencyTask.id
            }
            blockers.append(blocker)
        }
        guard !blockers.isEmpty else {
            if reserve {
                OrchestratorRegistry.withTransactionOnHeldLock {
                    $0.reserveGraphAdmission(key, taskID: taskID, graph: graph)
                }
            }
            return nil
        }
        let code = failed ? "graph_dependency_failed" : "graph_frontier_blocked"
        let message = failed
            ? "A dependency node failed review or execution; correct or replace it before dispatching this node."
            : "This node is not on the graph frontier; its dependencies have no retained completion receipts."
        return .refused(status: 409, code: code, message: message,
                        extra: ["graph_id": graph.id, "node_id": graph.currentNode,
                                "blocking_nodes": blockers])
    }


    static func planningGraphRecord(_ graph: PlanningGraph,
                                    taskIndex: GraphTaskIndex) -> [String: Any] {
        var statuses: [String: String] = [:]
        for node in graph.nodes {
            let outcome = graphNodeOutcome(node, graph: graph, index: taskIndex)
            if outcome == "planned" {
                let ready = node.dependsOn.allSatisfy { dependencyID in
                    guard let dependency = graph.nodes.first(where: { $0.id == dependencyID })
                    else { return false }
                    return graphNodeOutcome(dependency, graph: graph,
                                            index: taskIndex) == "done"
                }
                statuses[node.id] = ready ? "ready" : "blocked"
            } else {
                statuses[node.id] = outcome
            }
        }
        let nodes: [[String: Any]] = graph.nodes.map { node in
            var row: [String: Any] = [
                "id": node.id, "title": node.title, "kind": node.kind.rawValue,
                "depends_on": node.dependsOn, "acceptance": node.acceptance,
                "state": statuses[node.id] ?? "blocked"
            ]
            if let task = graphNodeTask(node, graphID: graph.id, index: taskIndex) {
                row["task_id"] = task.id
            }
            return row
        }
        return ["id": graph.id, "destination": graph.destination,
                "current_node": graph.currentNode, "nodes": nodes,
                "frontier": graph.nodes.compactMap { statuses[$0.id] == "ready" ? $0.id : nil },
                "unknowns": graph.unknowns, "out_of_scope": graph.outOfScope]
    }

    static func planningGraphRecord(_ graph: PlanningGraph) -> [String: Any] {
        planningGraphRecord(graph, taskIndex: graphTaskIndex())
    }

    static func verificationRecord(_ verification: Verification) -> [String: Any] {
        ["runs": verification.runs, "seconds": verification.seconds,
         "last": verification.last.rawValue, "scope": verification.scope]
    }


    /// One control-sheet row per immutable graph definition.
    static func graphRecords() -> [[String: Any]] {
        lock.lock(); let indexed = tasks; lock.unlock()
        let taskIndex = graphTaskIndex(indexed)
        var newest: [String: Task] = [:]
        for task in indexed.values {
            guard let graph = task.graph else { continue }
            if newest[graph.id].map({ $0.created < task.created }) ?? true {
                newest[graph.id] = task
            }
        }
        return newest.values.sorted { $0.created > $1.created }.compactMap { task in
            task.graph.map { planningGraphRecord($0, taskIndex: taskIndex) }
        }
    }

    static func storedPlanningGraph(_ graph: PlanningGraph) -> [String: Any] {
        ["id": graph.id, "destination": graph.destination,
         "current_node": graph.currentNode,
         "nodes": graph.nodes.map { node in
             ["id": node.id, "title": node.title, "kind": node.kind.rawValue,
              "depends_on": node.dependsOn,
              "acceptance": node.acceptance] as [String: Any]
         },
         "unknowns": graph.unknowns, "out_of_scope": graph.outOfScope]
    }

    static func reviewRecord(_ review: ReviewReceipt) -> [String: Any] {
        ["verdict": review.verdict.rawValue,
         "axes": review.axes.map { axis in
             ["axis": axis.axis.rawValue, "status": axis.status.rawValue,
              "findings": axis.findings.map { finding in
                  ["id": finding.id, "severity": finding.severity.rawValue,
                   "summary": finding.summary, "evidence": finding.evidence]
              }] as [String: Any]
         }]
    }

    static func requiresTypedReview(_ task: Task) -> Bool {
        task.graph?.nodes.first(where: {
            $0.id == task.graph?.currentNode
        })?.kind == .review || task.kind == "review"
    }

    static func typedReviewReporting(for task: Task) -> String {
        guard requiresTypedReview(task) else { return "" }
        return """

        Because this is a review node, `result.json` must also contain this typed receipt:

        ```json
        "review": {
          "verdict": "safe_to_land",
          "axes": [
            {"axis":"specification","status":"pass","findings":[]},
            {"axis":"repository_invariants","status":"pass","findings":[]},
            {"axis":"runtime_failure_behavior","status":"pass","findings":[]}
          ]
        }
        ```

        Use `changes_required` when any axis has findings. That axis then has status `findings`;
        every finding carries `id`, `severity` (`blocking`, `important`, or `minor`), `summary`,
        and one or more concrete `evidence` strings. Do not merge one axis into another.
        """
    }

    /// The complete graph goes above the language rule so a child can make its one node joinable.
    static func planningSection(for task: Task) -> String {
        if let graph = task.graph {
            let rows = graph.nodes.map { node in
                let mark = node.id == graph.currentNode ? "→ YOU" : ""
                let dependencies = node.dependsOn.isEmpty
                    ? "start" : "after " + node.dependsOn.joined(separator: ", ")
                return "- `\(node.id)` [\(node.kind.rawValue)] \(node.title) — "
                    + "\(dependencies) \(mark)"
            }.joined(separator: "\n")
            let fog = graph.unknowns.isEmpty ? "- none declared"
                : graph.unknowns.map { "- \($0)" }.joined(separator: "\n")
            let excluded = graph.outOfScope.isEmpty ? "- none declared"
                : graph.outOfScope.map { "- \($0)" }.joined(separator: "\n")
            let legacy = task.plan.map { "\nDispatcher note:\n\n\($0)\n" } ?? ""
            return """


            ## Typed decision and delivery graph

            Destination: \(graph.destination)
            Graph: `\(graph.id)`; your node: `\(graph.currentNode)`.

            \(rows)

            Unknowns (fog of war):
            \(fog)

            Out of scope:
            \(excluded)
            \(legacy)
            The broker, not this file, decides the live frontier from durable task, review and
            landing receipts. Work only on your node and satisfy its acceptance list in task.json.

            """
        }
        guard let plan = task.plan, !plan.isEmpty else { return "" }
        return """


        ## The plan this is part of

        Written by the session that dispatched you. You are one node of it — find yourself in it
        before you start, and hand back what the node after you needs rather than everything you
        found.

        \(plan)

        """
    }
}
