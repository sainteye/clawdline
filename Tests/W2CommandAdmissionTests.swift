import Foundation

// W2-2 closed the owner seams `docs/architecture-refactor.md` named for it: the closeability read
// counter and the restart-maintenance receipt moved behind `OrchestratorRegistry` transitions, and
// the two child-session filesystem probes that ran inside the task door now run outside it against
// a value copy and count only while the row still carries the identity they proved. The bad-result
// intake record had already left with `OrchestratorResultFinalizer`.

func runW2CommandAdmissionTests() {
    group("W2-2 closeability read receipts and the restart receipt are Registry-owned transitions") {
        Orchestrator.forget()
        Orchestrator.resetCloseabilityRegistryReadCountForTesting()
        _ = Orchestrator.closeabilityRegistrySnapshot()
        _ = Orchestrator.closeabilityRegistrySnapshot()
        expect("each closeability snapshot is counted through the task door",
               Orchestrator.closeabilityRegistryReadsForTesting(), 2)
        Orchestrator.resetCloseabilityRegistryReadCountForTesting()
        expect("the reset is a Registry transition too", Orchestrator.closeabilityRegistryReadsForTesting(), 0)

        let requestID = "a94e042a-1111-4222-8333-444444444444"
        defer { Orchestrator.storeSaveInterceptorForTesting = nil; Orchestrator.forget() }
        Orchestrator.forget()
        Orchestrator.storeSaveInterceptorForTesting = { _ in false }
        if case .ok = Orchestrator.beginRestartMaintenance(requestID: requestID, outstanding: 0, channels: [:]) {
            check("a restart intent that cannot be saved is refused", false)
        }
        check("the refused intent was rolled back",
              OrchestratorRegistry.withRestartRecords { $0.current() } == nil)

        // Another writer replaces the receipt while this save is failing. The rollback names the
        // receipt it wrote, so it must leave the other writer's receipt in place.
        let other = Orchestrator.RestartReceipt(
            requestID: "b94e042a-1111-4222-8333-444444444444", requestedInstanceID: "other",
            resumedInstanceID: nil, phase: .draining, requestedAt: Date(), drainedAt: nil,
            resumedAt: nil, reconciledAt: nil, outstanding: 1, channels: [:])
        Orchestrator.storeSaveInterceptorForTesting = { _ in
            OrchestratorRegistry.withRestartRecords { $0.installForTesting(other) }
            return false
        }
        _ = Orchestrator.beginRestartMaintenance(requestID: requestID, outstanding: 0, channels: [:])
        check("a failed-save rollback does not restore over a receipt another writer committed",
              OrchestratorRegistry.withRestartRecords { $0.current() } == other)

        Orchestrator.forget()
        Orchestrator.storeSaveInterceptorForTesting = { _ in true }
        guard case .ok = Orchestrator.beginRestartMaintenance(requestID: requestID, outstanding: 0, channels: [:]) else {
            check("a saved restart intent is accepted", false); return
        }
        let begun = OrchestratorRegistry.withRestartRecords { $0.current() }
        check("the accepted intent is the Registry's receipt, and admission closes",
              begun?.requestID == requestID && Orchestrator.restartAdmissionClosed()
                && Orchestrator.currentRestartRecord()?["request_id"] as? String == requestID)
        Orchestrator.storeSaveInterceptorForTesting = { _ in false }
        if case .ok = Orchestrator.abortRestartMaintenance(requestID: requestID) {
            check("an abort that cannot be saved is refused", false)
        }
        check("the refused abort restored the receipt it replaced",
              OrchestratorRegistry.withRestartRecords { $0.current() } == begun)
        Orchestrator.storeSaveInterceptorForTesting = { _ in true }
        guard case .ok = Orchestrator.abortRestartMaintenance(requestID: requestID) else {
            check("a saved abort is accepted", false); return
        }
        check("the saved abort reopens admission",
              !Orchestrator.restartAdmissionClosed()
                && OrchestratorRegistry.withRestartRecords { $0.current() }?.phase == .aborted)
    }

    group("W2-2 place-resume and cascade queries probe the filesystem outside the task door and revalidate the row") {
        Orchestrator.forget()
        defer { Orchestrator.childSessionProbeObserverForTesting = nil; Orchestrator.forget() }
        let transcript = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-w2-probe-\(UUID().uuidString).jsonl")
        try? Data("{}\n".utf8).write(to: transcript)
        defer { try? FileManager.default.removeItem(at: transcript) }
        let projectDir = "/tmp/w2-probe-project"
        let session = UUID().uuidString.lowercased()
        var parent = Orchestrator.Task(id: UUID().uuidString.lowercased(), state: .success, kind: "custom",
                                       title: "scheduled parent", assistant: .claude, projectDir: projectDir,
                                       timeoutMinutes: 30, created: Date().addingTimeInterval(-60),
                                       secretHash: String(repeating: "0", count: 64))
        parent.scheduleID = "w2-probe"
        parent.childSessionId = session
        parent.transcriptPath = transcript.path
        parent.transcriptProven = true
        parent.childTerminalId = "%81"
        var child = Orchestrator.Task(id: UUID().uuidString.lowercased(), state: .briefed, kind: "custom",
                                      title: "grandchild by session", assistant: .claude, projectDir: projectDir,
                                      timeoutMinutes: 30, created: Date(),
                                      secretHash: String(repeating: "0", count: 64))
        child.rootSessionId = session
        Orchestrator.holdScheduleTaskForTesting(parent)
        Orchestrator.holdScheduleTaskForTesting(child)

        var probesOutsideDoor: [Bool] = []
        Orchestrator.childSessionProbeObserverForTesting = { _ in
            let free = OrchestratorRegistry.lock.try()
            if free { OrchestratorRegistry.lock.unlock() }
            probesOutsideDoor.append(free)
        }
        check("the place-resume query answers for its proven scheduled session",
              Orchestrator.scheduledResumeTitle(sessionID: session, assistant: .claude, projectDir: projectDir) != nil)
        expect("the cascade query finds the task filed under the proven session",
               Orchestrator.liveTasks(under: [parent.id]), [child.id])
        check("every child-session probe ran with the task door released",
              !probesOutsideDoor.isEmpty && probesOutsideDoor.allSatisfy { $0 }, "\(probesOutsideDoor)")

        // The row changes identity while its probe is running: the proof is about a row that no
        // longer exists, so neither query may use it.
        var moved = parent
        moved.childSessionId = UUID().uuidString.lowercased()
        Orchestrator.childSessionProbeObserverForTesting = { _ in
            // Never re-enter the non-recursive lock: a regression that probes inside the door must
            // fail the check above rather than hang the suite here.
            guard OrchestratorRegistry.lock.try() else { return }
            OrchestratorRegistry.lock.unlock()
            Orchestrator.holdScheduleTaskForTesting(moved)
        }
        check("a place-resume proof is not used once the row it proved has changed",
              Orchestrator.scheduledResumeTitle(sessionID: session, assistant: .claude, projectDir: projectDir) == nil)
        Orchestrator.childSessionProbeObserverForTesting = nil
        Orchestrator.holdScheduleTaskForTesting(parent)
        Orchestrator.childSessionProbeObserverForTesting = { _ in
            // Never re-enter the non-recursive lock: a regression that probes inside the door must
            // fail the check above rather than hang the suite here.
            guard OrchestratorRegistry.lock.try() else { return }
            OrchestratorRegistry.lock.unlock()
            Orchestrator.holdScheduleTaskForTesting(moved)
        }
        expect("a cascade identity change fails closed by retaining the proven child",
               Orchestrator.liveTasks(under: [parent.id]), [child.id])
    }
}
