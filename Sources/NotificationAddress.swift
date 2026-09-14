import Foundation

/// Durable notification destinations live outside the broker's large state owner. The task
/// already recorded every identity below; this file only turns that evidence into a URL.
extension Orchestrator {
    /// A closure rather than a string so tests can also force the honest "Cloud identity absent"
    /// branch. Production reads the lifecycle's process-owned identity on its main-queue owner.
    static var notificationMachineIDForTesting: (() -> String?)?

    private static func notificationMachineID() -> String? {
        if let supplied = notificationMachineIDForTesting { return supplied() }
        return MainQueue.hop(from: "Orchestrator.notificationMachineID",
                             alreadyOnMain: MainQueue.isCurrent) {
            MainActor.assumeIsolated {
                if case .attached(_, let machineID) = CloudBridgeLifecycle.shared.state {
                    return machineID
                }
                return nil
            }
        }
    }

    /// Prefer the durable Cloud identity for a task-authored notification. `childTerminalId` is
    /// only a pane name: it can disappear after completion and can collide with another Mac's
    /// tmux pane. The conversation and Project pair survives both. Older/partially proved records
    /// retain the live-pane behavior rather than manufacturing a stable destination.
    static func pushURL(forTask task: Task) -> String {
        let fallback = pushURL(forSessionID: task.childTerminalId)
        guard let machineID = notificationMachineID(),
              let conversationID = task.childSessionId else { return fallback }
        let canonical = UsageLedger.canonicalProjectKey(
            projectDir: task.projectDir, repositoryCommonDir: task.repositoryCommonDir)
        let projectID = canonical.map(ProjectBoardIntegration.projectID)
        return WebPush.sessionLocatorURL(machineID: machineID,
                                         conversationID: conversationID,
                                         projectID: projectID) ?? fallback
    }
}
