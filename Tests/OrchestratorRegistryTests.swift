import Foundation

/// The transaction interface itself, asked the three questions it exists to answer: does a
/// transaction read back what it wrote, is that still there for the next one, and is the lock it
/// takes still the single lock `Orchestrator` has always taken?
///
/// The collections behind it are covered by the suites that own the behavior — handoff labels in
/// `OrchestratorCoordinationTests`, suppressed assignment labels in
/// `RootAssignmentCoordinationTests`, the terminal projection wherever a row is drawn. What is new
/// here is the boundary: five collections that used to be `static var`s any line in
/// `Orchestrator.swift` could assign are now private to `OrchestratorRegistry`, and the only way
/// in is a token nothing outside that file can construct.
///
/// **The strongest guarantee in this file is not tested here, because it is not testable here.**
/// That `OrchestratorRegistry.titlesByTerminal` cannot be named from another file is a fact about
/// compilation, and a test that could observe it would be a test that compiled. The proof for that
/// half is a deliberate bypass that fails to build, recorded in the stage report.

private let registryAbsent = "«nil»"

/// One collection, and the two closures that write it and read it back. Rendering to a string is
/// what lets five differently typed collections share one table and one set of checks.
private struct RegistryFact {
    let name: String
    let empty: String
    let write: (OrchestratorRegistry.Transaction) -> Void
    let read: (OrchestratorRegistry.Transaction) -> String
    let want: String
}

private func registryGraph(_ node: String) -> Orchestrator.PlanningGraph {
    Orchestrator.PlanningGraph(
        id: "99999999-8888-7777-6666-555555555555",
        destination: "one registry owner",
        currentNode: node,
        nodes: [Orchestrator.GraphNode(id: node, title: "the node", kind: .delivery,
                                       dependsOn: [], acceptance: ["it lands"])],
        unknowns: [],
        outOfScope: [])
}

private func registryRole(_ taskID: String, title: String) -> Orchestrator.Role {
    Orchestrator.Role(taskID: taskID, depth: 1, title: title, deadline: nil, live: true)
}

func runOrchestratorRegistryTests() {
    let taskID = "11111111-2222-3333-4444-555555555555"
    let otherID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

    let facts: [RegistryFact] = [
        RegistryFact(
            name: "a graph admission",
            empty: registryAbsent,
            write: { $0.reserveGraphAdmission("graph/stage1", taskID: taskID,
                                              graph: registryGraph("stage1")) },
            read: {
                $0.graphAdmission(forKey: "graph/stage1")
                    .map { "\($0.taskID)|\($0.graph.currentNode)" } ?? registryAbsent
            },
            want: "\(taskID)|stage1"),
        RegistryFact(
            name: "a terminal title",
            empty: registryAbsent,
            write: { $0.setTerminalProjection(titles: ["%child": "a child's task"], roles: [:]) },
            read: { $0.title(forTerminal: "%child") ?? registryAbsent },
            want: "a child's task"),
        RegistryFact(
            name: "a terminal role",
            empty: registryAbsent,
            write: {
                $0.setTerminalProjection(
                    titles: [:], roles: ["%child": registryRole(taskID, title: "a child's task")])
            },
            read: { $0.role(forTerminal: "%child").map(\.taskID) ?? registryAbsent },
            want: taskID),
        RegistryFact(
            name: "a handoff label",
            empty: registryAbsent,
            write: { $0.setHandoffTitle("handoff 1111", forTerminal: "%handoff") },
            read: { $0.handoffTitles()["%handoff"] ?? registryAbsent },
            want: "handoff 1111"),
        RegistryFact(
            name: "a suppressed Root Assignment label",
            empty: "false",
            write: { $0.suppressRootAssignmentLabel("assignment-1") },
            read: { String($0.isRootAssignmentLabelSuppressed("assignment-1")) },
            want: "true"),
    ]

    group("a registry transaction reads back what it wrote, and the next one still sees it") {
        for fact in facts {
            Orchestrator.forget()
            expect("\(fact.name) is absent after forget",
                   OrchestratorRegistry.withTransaction(fact.read), fact.empty)
            let insideTheWriter = OrchestratorRegistry.withTransaction { registry -> String in
                fact.write(registry)
                return fact.read(registry)
            }
            expect("\(fact.name) is visible to the transaction that wrote it",
                   insideTheWriter, fact.want)
            expect("\(fact.name) is still there in the next transaction",
                   OrchestratorRegistry.withTransaction(fact.read), fact.want)
        }
        Orchestrator.forget()
    }

    group("one lock, and a reader waits for the whole of a writer's transaction") {
        // Identity rather than equality: a second `NSLock` that happened to behave the same way
        // would be a behavior change wearing the old name, which is exactly what this cut must
        // not do. `Orchestrator.lock` is the registry's instance or this fails.
        check("Orchestrator still enters the registry's own lock",
              Orchestrator.lock === OrchestratorRegistry.lock)

        Orchestrator.forget()
        let writerIsHalfway = DispatchSemaphore(value: 0)
        let writerFinished = DispatchSemaphore(value: 0)
        DispatchQueue.global(qos: .userInitiated).async {
            OrchestratorRegistry.withTransaction { registry in
                registry.setHandoffTitle("first", forTerminal: "%first")
                writerIsHalfway.signal()
                // Long enough that a reader which did not take the lock would win the race and
                // see the first write alone. With the lock it simply waits.
                Thread.sleep(forTimeInterval: 0.05)
                registry.setHandoffTitle("second", forTerminal: "%second")
            }
            writerFinished.signal()
        }
        writerIsHalfway.wait()
        let seen = OrchestratorRegistry.withTransaction { $0.handoffTitles() }
        expect("a reader that opened mid-write still sees the first half",
               seen["%first"] ?? registryAbsent, "first")
        expect("and the second half, because it waited for the transaction to end",
               seen["%second"] ?? registryAbsent, "second")
        expect("the writing transaction ended", writerFinished.wait(timeout: .now() + 3),
               DispatchTimeoutResult.success)
        Orchestrator.forget()
    }

    group("the per-terminal facts keep the semantics the projection had before") {
        Orchestrator.forget()
        // `load()` ends in a reindex, so warm it before writing a projection by hand: otherwise
        // the first public read would rebuild the projection from an empty store and discard it.
        _ = Orchestrator.title(forTerminal: "%unwarmed")

        let first = registryRole(taskID, title: "the first task")
        let second = registryRole(otherID, title: "the second task")
        OrchestratorRegistry.withTransaction {
            $0.setTerminalProjection(titles: ["%a": "the first task"], roles: ["%a": first])
        }
        OrchestratorRegistry.withTransaction {
            $0.setTerminalProjection(titles: ["%b": "the second task"], roles: ["%b": second])
        }
        check("the projection is replaced whole rather than merged",
              Orchestrator.title(forTerminal: "%a") == nil)
        expect("so only the newest reading answers", Orchestrator.title(forTerminal: "%b"),
               "the second task")
        expect("and the roles are replaced with it",
               Orchestrator.role(forTerminal: "%b")?.taskID, otherID)
        check("a terminal nothing has projected has no role",
              Orchestrator.role(forTerminal: "%a") == nil)

        // Handoff labels are the opposite: one tab at a time is added as it opens, and the prune
        // that removes closed tabs is the only thing that replaces the whole collection.
        OrchestratorRegistry.withTransaction {
            $0.setHandoffTitle("handoff aaaa", forTerminal: "%h1")
            $0.setHandoffTitle("handoff bbbb", forTerminal: "%h2")
        }
        let bothLabels = OrchestratorRegistry.withTransaction { $0.handoffTitles() }
        expect("a handoff label is added beside the labels already there", bothLabels.count, 2)
        OrchestratorRegistry.withTransaction {
            $0.setHandoffTitles($0.handoffTitles().filter { $0.key == "%h1" })
        }
        let survivors = OrchestratorRegistry.withTransaction { $0.handoffTitles() }
        expect("and the prune replaces the whole collection with what survived",
               survivors, ["%h1": "handoff aaaa"])

        OrchestratorRegistry.withTransaction {
            $0.suppressRootAssignmentLabel("assignment-1")
            $0.suppressRootAssignmentLabel("assignment-1")
        }
        let suppressedTwice = OrchestratorRegistry.withTransaction {
            $0.isRootAssignmentLabelSuppressed("assignment-1")
        }
        check("suppressing the same assignment twice suppresses it once", suppressedTwice)
        OrchestratorRegistry.withTransaction {
            $0.unsuppressRootAssignmentLabel("assignment-1")
            $0.unsuppressRootAssignmentLabel("never-suppressed")
        }
        let stillSuppressed = OrchestratorRegistry.withTransaction {
            $0.isRootAssignmentLabelSuppressed("assignment-1")
        }
        check("unsuppressing releases it, and releasing an absent one is not an error",
              !stillSuppressed)

        // `forget()` clears each of the five separately and in the order it always did; what this
        // asks is that none of those five calls reaches past its own collection.
        OrchestratorRegistry.withTransaction { registry in
            registry.setHandoffTitle("handoff cccc", forTerminal: "%h3")
            registry.setTerminalProjection(titles: ["%c": "kept"], roles: ["%c": first])
            registry.removeAllHandoffTitles()
        }
        expect("clearing the handoff labels leaves the terminal titles alone",
               Orchestrator.title(forTerminal: "%c"), "kept")
        OrchestratorRegistry.withTransaction { $0.removeAllTerminalTitles() }
        check("clearing the titles leaves the roles alone",
              Orchestrator.role(forTerminal: "%c") != nil)
        OrchestratorRegistry.withTransaction { $0.removeAllRoles() }
        check("and clearing the roles empties the last of them",
              Orchestrator.role(forTerminal: "%c") == nil)
        Orchestrator.forget()
    }

    group("a graph admission is one reservation, and its release is a transaction of its own") {
        Orchestrator.forget()
        OrchestratorRegistry.withTransaction { registry in
            registry.reserveGraphAdmission("graph/stage1", taskID: taskID,
                                           graph: registryGraph("stage1"))
            registry.reserveGraphAdmission("graph/review", taskID: otherID,
                                           graph: registryGraph("review"))
        }
        let bothReserved = OrchestratorRegistry.withTransaction { $0.graphAdmissions().count }
        expect("both reservations are held at once", bothReserved, 2)
        let holder = OrchestratorRegistry.withTransaction {
            $0.graphAdmission(forKey: "graph/review")?.taskID
        }
        expect("and each names the dispatch holding it", holder, otherID)
        // The public release is the one caller that takes the lock for itself rather than
        // borrowing a region that already holds it.
        Orchestrator.releaseGraphAdmission("graph/stage1")
        let released = OrchestratorRegistry.withTransaction {
            $0.graphAdmission(forKey: "graph/stage1")?.taskID
        }
        check("releasing one admission takes only that one", released == nil)
        let stillHeld = OrchestratorRegistry.withTransaction { $0.graphAdmissions().count }
        expect("leaving the other still held", stillHeld, 1)
        OrchestratorRegistry.withTransaction { $0.removeAllGraphAdmissions() }
        let cleared = OrchestratorRegistry.withTransaction { $0.graphAdmissions().count }
        expect("and clearing them takes the rest", cleared, 0)
        Orchestrator.forget()
    }
}
