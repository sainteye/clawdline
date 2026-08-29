import Foundation

// CloudAccount's deadlock regressions re-exec this shared binary with a mode flag and a
// two-second parent deadline. Dispatch them before any ordinary suite so the child does not spend
// that deadline running the thousands of checks below before it reaches the requested scenario.
func runCloudAccountRegressionModeIfRequested() {
    let key = "CLAWDLINE_CLOUD_ACCOUNT_REGRESSION_MODE"
    guard let mode = ProcessInfo.processInfo.environment[key] else { return }
    Task {
        do {
            try await runCloudAccountRegressionScenario(mode: mode)
            print("CloudAccount regression \(mode) passed")
            exit(EXIT_SUCCESS)
        } catch {
            FileHandle.standardError.write(Data(
                "CloudAccount regression \(mode) failed: \(error)\n".utf8
            ))
            exit(EXIT_FAILURE)
        }
    }
    dispatchMain()
}

/// A subprocess-only entry used by the coordinator singleton race test near the end of this
/// file. Both workers deliberately cache initial absence before the parent releases them; the
/// production registration path must force a post-flock reload rather than trust that cache.
func runCoordinatorRegistrationWorkerIfRequested() {
    let environment = ProcessInfo.processInfo.environment
    guard let role = environment["CLAWDLINE_COORDINATOR_RACE_ROLE"],
          let store = environment["CLAWDLINE_COORDINATOR_RACE_STORE"],
          let barrier = environment["CLAWDLINE_COORDINATOR_RACE_BARRIER"] else { return }
    Coordinator.storeURLOverrideForTesting = URL(fileURLWithPath: store)
    Coordinator.forgetForTesting()
    let isFirst = role == "first"
    let session = Coordinator.LiveSession(
        identity: Orchestrator.SessionWorkIdentity(
            terminalID: role, assistant: isFirst ? .codex : .claude,
            tty: isFirst ? "/dev/ttys071" : "/dev/ttys072",
            pid: isFirst ? 701 : 702,
            processStart: Date(timeIntervalSince1970: isFirst ? 1_800_000_701 : 1_800_000_702),
            conversationID: "conversation-\(role)"),
        label: role, cwd: "/tmp", workState: .ready,
        waitingOnSession: false, hasWaiters: false)
    _ = Coordinator.sessionProjection(for: session)
    let barrierURL = URL(fileURLWithPath: barrier, isDirectory: true)
    try? Data("ready".utf8).write(to: barrierURL.appendingPathComponent(role))
    let go = barrierURL.appendingPathComponent("go").path
    let deadline = Date().addingTimeInterval(5)
    while !FileManager.default.fileExists(atPath: go), Date() < deadline { usleep(10_000) }
    guard FileManager.default.fileExists(atPath: go) else { print("barrier_timeout"); exit(2) }
    switch Coordinator.register(session, among: [session], makeID: {
        UUID(uuidString: isFirst
            ? "33333333-4444-4555-8666-777777777777"
            : "44444444-5555-4666-8777-888888888888")!
    }) {
    case .ok(let body): print(body["created"] as? Bool == true ? "created" : "idempotent")
    case .refused(_, let code, _, _): print(code)
    }
    exit(0)
}

func runIsolationProcessProbesIfRequested() {
    let environment = ProcessInfo.processInfo.environment
    /// A subprocess-only entry used to prove that the test binary protects the live remote store
    /// even when somebody runs the compiled binary directly instead of entering through `test.sh`.
    if environment["CLAWDLINE_TEST_REMOTE_DIRECTORY_PROBE"] == "1" {
        print(RemoteAuth.directory.path)
        try? FileManager.default.removeItem(at: isolatedTestStoreDirectory)
        exit(0)
    }
    /// The same probe for the drop cache, run the same way and for the same reason.
    if environment["CLAWDLINE_TEST_DROPS_DIRECTORY_PROBE"] == "1" {
        print(Drop.directory.path)
        try? FileManager.default.removeItem(at: isolatedTestStoreDirectory)
        exit(0)
    }
}
