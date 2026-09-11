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
        RegistryFact(
            name: "a suppressed handoff label",
            empty: "false",
            write: { $0.suppressHandoffLabel("handoff-1") },
            read: { String($0.isHandoffLabelSuppressed("handoff-1")) },
            want: "true"),
    ]

    group("a registry transaction reads back what it wrote, and the next one still sees it") {
        for fact in facts {
            Orchestrator.forget()
            // Write it, *then* forget. A collection that was never written is empty for the wrong
            // reason, and an expectation that cannot tell "forget() cleared this" from "nothing
            // ever put it there" stays green when one of `forget()`'s calls is deleted outright:
            // that is exactly what happened to `removeAllSuppressedHandoffLabels()`, whose body
            // could be emptied with the whole suite still passing.
            OrchestratorRegistry.withTransaction(fact.write)
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

        // The four coordination families have their own capability. Each is written through a
        // named transition and read back through a value projection: in the writing hold, in the
        // next hold, and across a real save / forget / load, because all four are durable.
        do {
            let store = Orchestrator.storeURL
            let before = try? Data(contentsOf: store)
            defer {
                if let before { try? before.write(to: store, options: .atomic) }
                else { try? FileManager.default.removeItem(at: store) }
                Orchestrator.forget()
            }
            Orchestrator.forget()
            Orchestrator.load()
            // The store codecs admit only lowercase UUID ids, so a fixture that is not one would
            // be dropped on reload and read as a lifetime defect it is not.
            let handoffID = UUID().uuidString.lowercased()
            let acceptedID = UUID().uuidString.lowercased()
            let replayedID = UUID().uuidString.lowercased()
            let requestID = UUID().uuidString.lowercased()
            let waitID = UUID().uuidString.lowercased()
            let duplicateWaitID = UUID().uuidString.lowercased()
            let at = Date(timeIntervalSince1970: 1_800_000_900)
            let envelope = Orchestrator.HandoffEnvelope(
                id: handoffID, projectDir: "/tmp",
                title: "registry handoff", fromSession: "%sender",
                coordinatorPlainHandoff: false, created: at, state: .opening)
            let labelIdentity = Orchestrator.RootAssignmentIdentity(
                terminalID: "%coordination-label", assistant: .codex, tty: nil, pid: nil,
                processStart: nil, conversationID: nil)
            func assignment(_ id: String) -> Orchestrator.RootAssignment {
                Orchestrator.RootAssignment(
                    id: id, requestID: requestID,
                    requestDigest: String(repeating: "c", count: 64), assistant: .codex,
                    model: "default", projectDir: "/tmp", label: "registry assignment",
                    objective: "objective", scope: "scope", constraints: "constraints",
                    relevantReferences: "references", acceptance: "acceptance",
                    projectApproved: false, created: at, state: .accepted, language: nil)
            }
            func join(_ records: OrchestratorRegistry.CoordinationRecordsTransaction,
                      newWaitID: String) -> OrchestratorRegistry.CoordinationWaitJoin {
                records.joinCoordinationWait(
                    repository: "/tmp", paths: ["a.swift"], owner: "%owner",
                    releaseCondition: "landed", waiter: "%waiter", reason: "needs a.swift",
                    now: at, newWaitID: newWaitID)
            }
            let accepted = assignment(acceptedID)
            let wroteAndRead = OrchestratorRegistry.withCoordinationRecords { records -> Bool in
                let opened = records.openHandoff(envelope) == nil
                records.bindHandoffLabel(Orchestrator.HandoffLabel(
                    handoffID: envelope.id, label: "registry label", identity: labelIdentity))
                let admitted = records.acceptRootAssignment(accepted) == nil
                let joined = join(records, newWaitID: waitID)
                return opened && admitted && joined.waitID == waitID
                    && records.handoff(envelope.id)?.state == .opening
                    && records.handoffLabel(envelope.id)?.label == "registry label"
                    && records.rootAssignment(accepted.id)?.state == .accepted
                    && records.coordinationWait(joined.waitID)?.waiters.count == 1
            }
            check("the four coordination families read back inside the hold that wrote them",
                  wroteAndRead)
            let replayed = OrchestratorRegistry.withCoordinationRecords { records in
                (handoff: records.openHandoff(envelope)?.id,
                 assignment: records.acceptRootAssignment(assignment(replayedID))?.id,
                 secondRow: records.rootAssignment(replayedID),
                 join: join(records, newWaitID: duplicateWaitID))
            }
            expect("a replayed handoff id returns the envelope already held",
                   replayed.handoff, envelope.id)
            expect("a replayed request id returns the assignment already held",
                   replayed.assignment, accepted.id)
            check("and records no second assignment under the new id", replayed.secondRow == nil)
            check("a repeated waiter is the wait already held, still owed its request",
                  replayed.join.waitID == waitID
                    && replayed.join.deduplicated && replayed.join.needsDelivery)

            // Obligation evidence is invalidated by the owner's clock, not by a `didSet` in the
            // facade: a wait or handoff written only through the registry must still move it.
            let beforeSettlement = Orchestrator.currentObligationGeneration()
            let settled = OrchestratorRegistry.withCoordinationRecords {
                $0.settleHandoffOpening(envelope.id, delivered: true)
            }
            check("an opening settles once", settled?.state == .delivered
                    && OrchestratorRegistry.withCoordinationRecords {
                        $0.settleHandoffOpening(envelope.id, delivered: false)
                    } == nil)
            check("a handoff settled through the registry advances the obligation clock",
                  Orchestrator.currentObligationGeneration() > beforeSettlement)

            Orchestrator.saveForTesting()
            Orchestrator.forget()
            let forgotten = OrchestratorRegistry.withCoordinationRecords { $0.persistentSnapshot() }
            check("forget clears all four coordination families",
                  forgotten.handoffs.isEmpty && forgotten.handoffLabels.isEmpty
                    && forgotten.rootAssignments.isEmpty && forgotten.coordinationWaits.isEmpty)
            Orchestrator.load()
            let durable = OrchestratorRegistry.withCoordinationRecords { records -> Bool in
                records.handoff(envelope.id)?.state == .delivered
                    && records.handoffLabel(envelope.id)?.identity == labelIdentity
                    && records.rootAssignment(forRequest: accepted.requestID)?.id == accepted.id
                    && records.coordinationWait(waitID)?.waiters
                        .map(\.sessionID) == ["%waiter"]
            }
            check("all four coordination families survive a save and reload", durable)

            let beforeWithdrawal = Orchestrator.currentObligationGeneration()
            let withdrawn = OrchestratorRegistry.withCoordinationRecords { records in
                (stranger: records.withdrawCoordinationWaiter(
                    waitID, waiter: "%stranger"),
                 waiter: records.withdrawCoordinationWaiter(
                    waitID, waiter: "%waiter"),
                 remaining: records.coordinationWait(waitID))
            }
            check("only a waiter withdraws itself, and the wait goes with its last waiter",
                  withdrawn.stranger == .notWaiter && withdrawn.waiter == .withdrawn
                    && withdrawn.remaining == nil)
            check("a wait withdrawn through the registry advances the obligation clock",
                  Orchestrator.currentObligationGeneration() > beforeWithdrawal)
        }
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

        // Two sets, one shape, and an id that means different things in each: a handoff id and an
        // assignment id are drawn from the same alphabet, so a single set would let one primitive
        // hide the other's label.
        // Nothing unsuppresses the handoff side first: an explicit release here would make the
        // next check green under a single shared set as well as under two, which is the one
        // arrangement it exists to refuse.
        OrchestratorRegistry.withTransaction { $0.suppressRootAssignmentLabel("shared-id") }
        let handoffFollowed = OrchestratorRegistry.withTransaction {
            $0.isHandoffLabelSuppressed("shared-id")
        }
        check("suppressing an assignment label does not suppress a handoff's", !handoffFollowed)
        OrchestratorRegistry.withTransaction { $0.suppressHandoffLabel("shared-id") }
        let assignmentStillSuppressed = OrchestratorRegistry.withTransaction {
            $0.isRootAssignmentLabelSuppressed("shared-id")
                && $0.isHandoffLabelSuppressed("shared-id")
        }
        check("and the two are suppressed independently under the same id",
              assignmentStillSuppressed)

        // `forget()` clears each of the six separately and in the order it always did; what this
        // asks is that none of those six calls reaches past its own collection.
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

    group("the four registry rate windows keep their limits, boundaries and identities") {
        Orchestrator.forget()
        let start = Date(timeIntervalSince1970: 1_800_000_000)

        OrchestratorRegistry.withTransaction { registry in
            check("the dynamic dispatch ceiling admits exactly the supplied capacity",
                  registry.takeDispatchRate(now: start, limit: 2) != nil
                    && registry.takeDispatchRate(now: start, limit: 2) != nil
                    && registry.takeDispatchRate(now: start, limit: 2) == nil)
            let notifications = (0..<30).compactMap { _ in
                registry.takeNotificationRate(now: start)
            }
            expect("dispatch saturation does not spend the ordinary notification window",
                   notifications.count, 30)
            check("the ordinary notification window admits thirty and no more",
                  registry.takeNotificationRate(now: start) == nil)
            let invalidSecrets = (0..<3).filter { _ in
                registry.takeInvalidTaskSecretNotificationRate(now: start)
            }
            expect("notification saturation does not spend the invalid-secret window",
                   invalidSecrets.count, 3)
            check("the invalid-secret window admits three and no more",
                  !registry.takeInvalidTaskSecretNotificationRate(now: start))
            let scheduleWrites = (0..<10).filter { _ in
                registry.takeScheduleWriteRate(now: start)
            }
            expect("the other three windows do not spend the schedule-write window",
                   scheduleWrites.count, 10)
            check("the schedule-write window admits ten and no more",
                  !registry.takeScheduleWriteRate(now: start))

            check("dispatch entries expire at the exact ten-minute boundary",
                  registry.takeDispatchRate(now: start.addingTimeInterval(600), limit: 2) != nil)
            check("notification entries expire at the exact one-hour boundary",
                  registry.takeNotificationRate(now: start.addingTimeInterval(3_600)) != nil)
            check("invalid-secret entries expire at the exact ten-minute boundary",
                  registry.takeInvalidTaskSecretNotificationRate(
                    now: start.addingTimeInterval(600)))
            check("schedule-write entries expire at the exact ten-minute boundary",
                  registry.takeScheduleWriteRate(now: start.addingTimeInterval(600)))
        }

        Orchestrator.forget()
        OrchestratorRegistry.withTransaction { registry in
            _ = registry.takeDispatchRate(now: start, limit: 4)
            _ = registry.takeDispatchRate(now: start.addingTimeInterval(1), limit: 4)
            _ = registry.takeDispatchRate(now: start, limit: 4)
            registry.refundDispatchRate(start)
            expect("a duplicate dispatch refund removes the last exact ticket",
                   registry.dispatchRateTicketsForTesting(),
                   [start, start.addingTimeInterval(1)])

            _ = registry.takeNotificationRate(now: start)
            _ = registry.takeNotificationRate(now: start.addingTimeInterval(1))
            _ = registry.takeNotificationRate(now: start)
            registry.refundNotificationRate(start)
            expect("a duplicate notification refund removes the last exact ticket",
                   registry.notificationRateTicketsForTesting(),
                   [start, start.addingTimeInterval(1)])
            _ = registry.takeInvalidTaskSecretNotificationRate(now: start)
            _ = registry.takeScheduleWriteRate(now: start)
        }

        Orchestrator.forget()
        OrchestratorRegistry.withTransaction { registry in
            check("forget clears all four windows through the existing transaction",
                  registry.dispatchRateTicketsForTesting().isEmpty
                    && registry.notificationRateTicketsForTesting().isEmpty
                    && registry.invalidTaskSecretNotificationRateCountForTesting() == 0
                    && registry.scheduleWriteRateCountForTesting() == 0)
        }

        let taskID = "44444444-5555-4666-8777-888888888888"
        let secret = String(repeating: "f6", count: 32)
        var limitedTask = Orchestrator.Task(
            id: taskID, state: .briefed, kind: "custom", title: "prune order",
            assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30, created: start,
            secretHash: Orchestrator.hash(ofSecret: secret))
        limitedTask.notifyCount = 5
        func installLimitedTask() {
            Orchestrator.storeSaveInterceptorForTesting = { _ in true }
            Orchestrator.agentPushForTesting = { _, _, _, _, _ in
                WebPush.Delivery(sent: 1, failed: 0)
            }
            Orchestrator.holdScheduleTaskForTesting(limitedTask)
        }

        Orchestrator.forget(); installLimitedTask()
        OrchestratorRegistry.withTransaction { _ = $0.takeNotificationRate(now: start) }
        let expiredReply = Orchestrator.agentNotify(
            taskID: taskID, secret: secret, title: "ready", body: "done",
            now: start.addingTimeInterval(3_600))
        let expiredTickets = OrchestratorRegistry.withTransaction {
            $0.notificationRateTicketsForTesting()
        }
        if case .refused(let status, let code, _, _) = expiredReply {
            check("a task-limit refusal still prunes expired global notification tickets",
                  status == 429 && code == "notify_limit" && expiredTickets.isEmpty)
        } else {
            check("a task-limit refusal still prunes expired global notification tickets", false)
        }

        Orchestrator.forget(); installLimitedTask()
        OrchestratorRegistry.withTransaction { registry in
            for _ in 0..<30 { _ = registry.takeNotificationRate(now: start) }
        }
        let fullReply = Orchestrator.agentNotify(
            taskID: taskID, secret: secret, title: "ready", body: "done", now: start)
        if case .refused(let status, let code, _, _) = fullReply {
            check("the task limit still wins when the global notification window is full",
                  status == 429 && code == "notify_limit")
        } else {
            check("the task limit still wins when the global notification window is full", false)
        }
        Orchestrator.forget()
    }

    group("session records have one registry owner and preserve their lifetimes") {
        Orchestrator.forget()
        let at = Date(timeIntervalSince1970: 1_800_000_500)
        let identity = Orchestrator.SessionWorkIdentity(
            terminalID: "%session-records", assistant: .codex, tty: "/dev/ttys077",
            pid: 7_777, processStart: at.addingTimeInterval(-30),
            conversationID: "conversation-session-records")
        let delivery = Orchestrator.SessionDelivery(
            identity: identity, summary: "registry-owned delivery", reportedAt: at,
            settled: false)
        let selfState = Orchestrator.SessionSelfState(
            identity: identity, claim: .holding, note: "registry-owned state",
            movedBy: "%owner", personNeeded: false, claimReportedAt: at,
            claimSettled: false, owed: nil)
        let handoff = OrchestratorRegistry.HandoffDelivery(
            id: "handoff-session-records", assistant: .codex, model: nil,
            terminalID: "%handoff-session-records", backend: .tmux, spawnedAt: at)

        OrchestratorRegistry.withSessionRecords { records in
            records.setTaskSecret("ephemeral-secret", for: "task-session-records")
            records.setSessionDelivery(delivery, forTerminal: identity.terminalID)
            records.setSessionSelfState(selfState, forTerminal: identity.terminalID)
            records.setHandoffDelivery(handoff, for: handoff.id)
        }
        let persistentBeforeRestart = OrchestratorRegistry.withSessionRecords {
            $0.persistentSnapshot()
        }
        check("the persistent projection contains only durable session records",
              persistentBeforeRestart.deliveries[identity.terminalID] == delivery
                && persistentBeforeRestart.selfStates[identity.terminalID]?.note == selfState.note)

        Orchestrator.saveForTesting()
        Orchestrator.forget()
        Orchestrator.load()
        OrchestratorRegistry.withSessionRecords { records in
            check("durable session records survive a save and reload through the registry",
                  records.sessionDelivery(forTerminal: identity.terminalID) == delivery)
            expect("the durable self-state survives the same save and reload",
                   records.sessionSelfState(forTerminal: identity.terminalID)?.note,
                   "registry-owned state")
            check("plaintext secrets and in-flight handoffs do not cross a restart",
                  records.taskSecret(for: "task-session-records") == nil
                    && records.handoffDelivery(for: handoff.id) == nil)
        }
        Orchestrator.forget()
    }
}
