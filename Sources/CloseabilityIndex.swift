import Foundation

/// The read-side index for Session closeability. Kept out of `Orchestrator.swift` because this is
/// a projection of its durable records, not another mutation path in the broker state machine.
extension Orchestrator {

    static func closeabilityObligations(identity: SessionWorkIdentity,
                                        tasks: [Task],
                                        waits: [CoordinationWait],
                                        handoffs: [HandoffEnvelope],
                                        owed: OwedDebt?,
                                        inputsAreOrdered: Bool = false) -> [CloseabilityReason] {
        var out: [CloseabilityReason] = []
        let orderedTasks = inputsAreOrdered ? tasks : tasks.sorted(by: { $0.created < $1.created })
        let orderedWaits = inputsAreOrdered ? waits : waits.sorted(by: { $0.created < $1.created })
        let orderedHandoffs = inputsAreOrdered
            ? handoffs : handoffs.sorted(by: { $0.created < $1.created })
        let ownTask = taskForCurrentSession(tasks, identity: identity)
        if let ownTask, !ownTask.state.isTerminal {
            out.append(CloseabilityReason(.ownTaskUnfinished, subjectKind: "task",
                                          subjectID: ownTask.id, mover: .thisSession))
        }

        func dispatchedByThisSession(_ task: Task) -> Bool {
            if let ownTask, task.parentTaskId == ownTask.id { return true }
            guard let conversation = identity.conversationID,
                  task.rootSessionId == conversation,
                  let rootAssistant = task.rootAssistant,
                  rootAssistant == identity.assistant else { return false }
            return true
        }

        for task in orderedTasks {
            let mine = dispatchedByThisSession(task)
            if mine, !task.state.isTerminal {
                out.append(CloseabilityReason(
                    .liveDescendantTask, subjectKind: "task", subjectID: task.id,
                    mover: task.childTerminalId.map { CloseabilityMover.otherSession($0) }
                        ?? .task(task.id)))
                continue
            }
            guard mine else { continue }
            let landingClosed = task.landing?.state.isSettled ?? false
            if task.state.isTerminal, task.resultVerifiedAt == nil, task.summary == nil,
               !landingClosed {
                out.append(CloseabilityReason(.taskWithoutResult, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if task.landing?.state == .pending {
                out.append(CloseabilityReason(.pendingLandingOwned, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if let delivery = task.completionDelivery, delivery.state != .acknowledged, mine {
                out.append(CloseabilityReason(.completionUndelivered, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if task.worktree?.dirty == true, !landingClosed {
                out.append(CloseabilityReason(.dirtyIsolatedWorktree, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
            if !task.claims.isEmpty, task.state.isTerminal, !landingClosed,
               Set(task.untouchedClaims) != Set(task.claims) {
                out.append(CloseabilityReason(.touchedClaimsWithoutClosure, subjectKind: "task",
                                              subjectID: task.id, mover: .thisSession))
            }
        }

        for wait in orderedWaits {
            let pending = wait.waiters.filter { $0.releaseDeliveredAt == nil }
            guard !pending.isEmpty else { continue }
            if wait.ownerSessionID == identity.terminalID {
                out.append(CloseabilityReason(.coordinationWaitOwned, subjectKind: "wait",
                                              subjectID: wait.id, mover: .thisSession))
            } else if pending.contains(where: { $0.sessionID == identity.terminalID }) {
                out.append(CloseabilityReason(
                    .coordinationWaitWaiting, subjectKind: "wait", subjectID: wait.id,
                    mover: .otherSession(wait.ownerSessionID)))
            }
        }

        for handoff in orderedHandoffs
        where handoff.state != .delivered && handoffSource(handoff.fromSession, matches: identity) {
            out.append(CloseabilityReason(.openHandoff, subjectKind: "handoff",
                                          subjectID: handoff.id, mover: .thisSession))
        }
        if let owed {
            out.append(CloseabilityReason(
                .owedDecision, subjectKind: "session", subjectID: identity.terminalID,
                mover: owed.personNeeded ? .person : .thisSession))
        }
        return out
    }

    /// A settled historical row cannot block its root. Keep it out of every root/parent hot
    /// bucket; it remains in the child-identity bucket because duplicate exact task receipts must
    /// still fail closed rather than being hidden by the index.
    private static func taskCanContributeToRootCloseability(_ task: Task) -> Bool {
        if !task.state.isTerminal { return true }
        let landingClosed = task.landing?.state.isSettled ?? false
        if task.resultVerifiedAt == nil, task.summary == nil, !landingClosed { return true }
        if task.landing?.state == .pending { return true }
        if let delivery = task.completionDelivery, delivery.state != .acknowledged { return true }
        if task.worktree?.dirty == true, !landingClosed { return true }
        return !task.claims.isEmpty && !landingClosed
            && Set(task.untouchedClaims) != Set(task.claims)
    }

    private struct CloseabilityRootKey: Hashable {
        let assistant: String
        let conversation: String
    }

    struct CloseabilityCandidates {
        let tasks: [Task]
        let waits: [CoordinationWait]
        let handoffs: [HandoffEnvelope]
    }

    struct CloseabilityRegistryIndex {
        private let tasksByChildTerminal: [String: [Task]]
        private let tasksByRoot: [CloseabilityRootKey: [Task]]
        private let tasksByParent: [String: [Task]]
        private let waitsByTerminal: [String: [CoordinationWait]]
        private let handoffsBySource: [String: [HandoffEnvelope]]

        init(tasks: [Task], waits: [CoordinationWait], handoffs: [HandoffEnvelope]) {
            var child: [String: [Task]] = [:]
            var root: [CloseabilityRootKey: [Task]] = [:]
            var parent: [String: [Task]] = [:]
            for task in tasks {
                if let terminal = task.childTerminalId { child[terminal, default: []].append(task) }
                guard Orchestrator.taskCanContributeToRootCloseability(task) else { continue }
                if let conversation = task.rootSessionId, let assistant = task.rootAssistant {
                    root[CloseabilityRootKey(assistant: assistant.rawValue,
                                             conversation: conversation), default: []].append(task)
                }
                if let parentID = task.parentTaskId { parent[parentID, default: []].append(task) }
            }

            var waitIndex: [String: [CoordinationWait]] = [:]
            for wait in waits {
                let pending = wait.waiters.filter { $0.releaseDeliveredAt == nil }
                guard !pending.isEmpty else { continue }
                var terminals = Set(pending.map(\.sessionID))
                terminals.insert(wait.ownerSessionID)
                for terminal in terminals { waitIndex[terminal, default: []].append(wait) }
            }

            var handoffIndex: [String: [HandoffEnvelope]] = [:]
            for handoff in handoffs where handoff.state != .delivered {
                if let source = handoff.fromSession {
                    handoffIndex[source, default: []].append(handoff)
                }
            }
            tasksByChildTerminal = child
            tasksByRoot = root
            tasksByParent = parent
            waitsByTerminal = waitIndex
            handoffsBySource = handoffIndex
        }

        func candidates(for identity: SessionWorkIdentity) -> CloseabilityCandidates {
            var tasks: [Task] = []
            var taskIDs: Set<String> = []
            func appendTasks(_ rows: [Task]?) {
                for row in rows ?? [] where taskIDs.insert(row.id).inserted { tasks.append(row) }
            }
            let ownCandidates = tasksByChildTerminal[identity.terminalID]
            appendTasks(ownCandidates)
            let ownTask = ownCandidates.flatMap {
                let matches = $0.filter { Orchestrator.taskMatchesCurrentSession($0, identity: identity) }
                return matches.count == 1 ? matches[0] : nil
            }
            if let conversation = identity.conversationID, let assistant = identity.assistant {
                appendTasks(tasksByRoot[CloseabilityRootKey(
                    assistant: assistant.rawValue, conversation: conversation)])
            }
            if let ownTask { appendTasks(tasksByParent[ownTask.id]) }
            tasks.sort { $0.created == $1.created ? $0.id < $1.id : $0.created < $1.created }

            let waits = waitsByTerminal[identity.terminalID] ?? []
            var handoffs = handoffsBySource[identity.terminalID] ?? []
            if let conversation = identity.conversationID, conversation != identity.terminalID {
                let seen = Set(handoffs.map(\.id))
                handoffs.append(contentsOf: (handoffsBySource[conversation] ?? [])
                    .filter { !seen.contains($0.id) })
            }
            handoffs.sort { $0.created == $1.created ? $0.id < $1.id : $0.created < $1.created }
            return CloseabilityCandidates(tasks: tasks, waits: waits, handoffs: handoffs)
        }
    }

    struct CloseabilityRegistrySnapshot {
        private let index: CloseabilityRegistryIndex
        let selfStates: [String: SessionSelfState]
        let attestations: [String: ClosureAttestation]
        let activityGenerations: [String: Int]
        let obligationGeneration: Int

        init(index: CloseabilityRegistryIndex, selfStates: [String: SessionSelfState],
             attestations: [String: ClosureAttestation], activityGenerations: [String: Int],
             obligationGeneration: Int) {
            self.index = index
            self.selfStates = selfStates
            self.attestations = attestations
            self.activityGenerations = activityGenerations
            self.obligationGeneration = obligationGeneration
        }

        func candidates(for identity: SessionWorkIdentity) -> CloseabilityCandidates {
            index.candidates(for: identity)
        }
    }

    static func closeabilityObligationsIndexedForTesting(
        identity: SessionWorkIdentity, tasks: [Task], waits: [CoordinationWait],
        handoffs: [HandoffEnvelope], owed: OwedDebt?
    ) -> (reasons: [CloseabilityReason], taskCandidates: Int) {
        let index = CloseabilityRegistryIndex(
            tasks: tasks.sorted { $0.created < $1.created },
            waits: waits.sorted { $0.created < $1.created },
            handoffs: handoffs.sorted { $0.created < $1.created })
        let candidates = index.candidates(for: identity)
        return (closeabilityObligations(
            identity: identity, tasks: candidates.tasks, waits: candidates.waits,
            handoffs: candidates.handoffs, owed: owed, inputsAreOrdered: true),
                candidates.tasks.count)
    }
}
