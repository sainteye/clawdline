import Foundation

private struct CloudClockTestFailure: Error, CustomStringConvertible {
    let description: String
}

private final class InjectedCloudTime {
    var wall: TimeInterval
    var continuous: TimeInterval
    var bootID: String

    init(wall: TimeInterval = 1_700_000_000, continuous: TimeInterval = 10_000, bootID: String = "boot-a") {
        self.wall = wall
        self.continuous = continuous
        self.bootID = bootID
    }

    var clock: CloudClock {
        CloudClock(
            wall: { [unowned self] in Date(timeIntervalSince1970: self.wall) },
            continuous: { [unowned self] in self.continuous },
            bootID: { [unowned self] in self.bootID }
        )
    }

    var wallDate: Date {
        Date(timeIntervalSince1970: wall)
    }

    func advance(wall wallDelta: TimeInterval, continuous continuousDelta: TimeInterval) {
        wall += wallDelta
        continuous += continuousDelta
    }
}

// MARK: - Command clock wiring

/// Host time for the command-clock wiring tests. The kernel clocks part ways across a system
/// sleep the way Darwin's do — measured on this Mac on 2026-09-13, `ProcessInfo.systemUptime`
/// and `CLOCK_UPTIME_RAW` stood 60,262 s behind both `CLOCK_MONOTONIC_RAW` and wall time since
/// boot: `CLOCK_UPTIME_RAW` stops while the Mac sleeps and `CLOCK_MONOTONIC_RAW` keeps counting.
private final class CommandClockHostTime: @unchecked Sendable {
    private let lock = NSLock()
    private var wallSeconds: TimeInterval = 1_700_000_000
    private var awakeNanoseconds: UInt64 = 5_000_000_000_000
    private var asleepNanoseconds: UInt64 = 0
    private var unmodelled: [String] = []

    func wall() -> Date {
        lock.lock(); defer { lock.unlock() }
        return Date(timeIntervalSince1970: wallSeconds)
    }

    func kernelNanoseconds(_ clock: clockid_t) -> UInt64 {
        lock.lock(); defer { lock.unlock() }
        if clock == CLOCK_MONOTONIC_RAW { return awakeNanoseconds + asleepNanoseconds }
        if clock == CLOCK_UPTIME_RAW { return awakeNanoseconds }
        unmodelled.append(String(describing: clock))
        return 0
    }

    /// The Mac is awake: wall time and both kernel clocks advance together.
    func run(_ seconds: TimeInterval) {
        lock.lock(); defer { lock.unlock() }
        wallSeconds += seconds
        awakeNanoseconds += UInt64(seconds * 1_000_000_000)
    }

    /// The Mac sleeps: wall time and `CLOCK_MONOTONIC_RAW` advance, `CLOCK_UPTIME_RAW` does not.
    func sleep(_ seconds: TimeInterval) {
        lock.lock(); defer { lock.unlock() }
        wallSeconds += seconds
        asleepNanoseconds += UInt64(seconds * 1_000_000_000)
    }

    /// Somebody sets the wall clock; no physical time passes.
    func setWall(by seconds: TimeInterval) {
        lock.lock(); defer { lock.unlock() }
        wallSeconds += seconds
    }

    func unmodelledClocks() -> [String] {
        lock.lock(); defer { lock.unlock() }
        return unmodelled
    }
}

/// The control plane's side of a device-token fetch: a successful response whose pinned-HTTPS
/// Date header reads the host's wall time plus the configured offset.
private final class CommandClockServerTokens: CloudDeviceTokenProviding, @unchecked Sendable {
    private let lock = NSLock()
    private let time: CommandClockHostTime
    private var offset: TimeInterval = 0

    init(time: CommandClockHostTime) { self.time = time }

    func setServerOffset(_ seconds: TimeInterval) {
        lock.lock(); offset = seconds; lock.unlock()
    }

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        let serverDate = time.wall().addingTimeInterval(currentOffset())
        return CloudDeviceToken(value: "token", expiresAt: Date().addingTimeInterval(240),
                                authenticatedServerDate: serverDate)
    }

    private func currentOffset() -> TimeInterval {
        lock.lock(); defer { lock.unlock() }
        return offset
    }
}

/// `Services.production()`'s command-clock wiring driving a real durable bridge. A device-token
/// fetch reaches `CloudCommandEpochAuthority` through the supervised provider the production
/// transport receives, and each command's effect-time authorization reaches `CloudCommandLedger`
/// through `CloudAppBridge`, which maps a refusal to its wire code. Only the host's wall and
/// kernel clock readings are replaced. 251090b3 was right in each unit and wrong in this wiring,
/// which is why these scenarios do not stop at `EpochGuard`.
private final class CommandClockWiringFixture {
    let time: CommandClockHostTime
    private let serverTokens: CommandClockServerTokens
    private let tokens: CloudSupervisedDeviceTokenProvider
    private let directory: URL
    private let transport: CloudAppBridgeTestTransport
    private let results: CloudAppBridgeTestResults
    private var runtime: CloudDurableRuntime?
    private var bridge: CloudAppBridge?
    private var sequence: UInt64 = 0

    init() async throws {
        let time = CommandClockHostTime()
        let authority = CloudCommandEpochAuthority(
            wall: { time.wall() },
            kernelNanoseconds: { time.kernelNanoseconds($0) })
        let serverTokens = CommandClockServerTokens(time: time)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "clawdline-command-clock-\(UUID().uuidString)", isDirectory: true)
        let transport = CloudAppBridgeTestTransport()
        let results = CloudAppBridgeTestResults()
        self.time = time
        self.serverTokens = serverTokens
        self.tokens = authority.supervisedTokenProvider(
            inner: serverTokens, onTerminalFailure: { _ in })
        self.directory = directory
        self.transport = transport
        self.results = results
        try FileManager.default.createDirectory(
            at: directory, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let runtime = try CloudDurableRuntime.open(directory: directory, runtime: .mac)
        let bridge = CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(
                machineID: "clock-mac", deviceID: "clock-device", keyID: "ms-1",
                masterSecret: try CloudMasterSecret(
                    rawRepresentation: Data(repeating: 0x63, count: 32)),
                signingKey: CloudDeviceKeyPair()),
            sequencing: CloudAppBridgeTestSequence(), allowCloudCommands: { true },
            currentCommandEffectAuthority: { _, _ in
                authority.effectAuthorization(
                    rosterAllowsSender: true, writeGateAllows: true)
            },
            commandRouter: CloudAppBridgeTestRouter(),
            nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) },
            commandResult: { results.append($0) }, durableRuntime: runtime)
        try await bridge.start()
        self.runtime = runtime
        self.bridge = bridge
    }

    /// One device-token fetch, as a rotation or a reconnect performs it.
    func fetchToken(serverOffset: TimeInterval = 0) async throws {
        serverTokens.setServerOffset(serverOffset)
        _ = try await tokens.fetchDeviceToken()
    }

    /// Sends one fresh write command and returns the bridge's result for it.
    func command() async throws -> CloudCommandResult {
        sequence += 1
        let expected = Int(sequence)
        let now = UInt64(Date().timeIntervalSince1970 * 1_000)
        transport.yield(
            #"{"type":"send","session":"plain","request":"clock-\#(sequence)","text":"hi","images":[]}"#,
            sequence: sequence, timestamp: now, channel: "ctl/clock-mac")
        let results = self.results
        let transport = self.transport
        try await waitForCloudAppBridge("command clock result \(expected)") {
            results.all().count == expected && transport.envelopes().count == expected
        }
        let reply = transport.envelopes()[expected - 1]
        transport.acknowledge(reply)
        let runtime = self.runtime
        try await waitForCloudAppBridge("command clock reply \(expected) acknowledged") {
            await runtime?.spool.row(seq: Int64(reply.seq))?.state == .acked
        }
        return results.all()[expected - 1]
    }

    func stop() async {
        await bridge?.stop()
        bridge = nil
        runtime = nil
        try? FileManager.default.removeItem(at: directory)
    }
}

/// Collects every wiring check, so one run names each red scenario instead of stopping at the
/// first failure the way the guard checks below do.
private final class CommandClockWiringChecks {
    private(set) var count = 0
    private(set) var failures: [String] = []

    func check(_ name: String, _ condition: Bool, _ result: CloudCommandResult? = nil) {
        count += 1
        guard !condition else { return }
        failures.append(result.map { "\(name) — got \($0.status) \($0.code ?? "-")" } ?? name)
    }

    func scenario(_ name: String, _ body: (CommandClockWiringFixture) async throws -> Void) async {
        do {
            let fixture = try await CommandClockWiringFixture()
            do { try await body(fixture) } catch {
                check("command clock scenario completes: \(name) — \(error)", false)
            }
            await fixture.stop()
        } catch {
            check("command clock fixture opens: \(name) — \(error)", false)
        }
    }
}

private func isClockRefusal(_ result: CloudCommandResult) -> Bool {
    result.status == 503 && result.code == "command_clock_uncertain"
}

private func isAdmitted(_ result: CloudCommandResult) -> Bool {
    result.status == 200 && result.code == nil
}

private func runCommandClockWiringScenarios(_ checks: CommandClockWiringChecks) async {
    await checks.scenario("first calibration") { clock in
        let unCalibrated = try await clock.command()
        checks.check("clock: a command before any authenticated server date is refused as command_clock_uncertain",
                     isClockRefusal(unCalibrated), unCalibrated)

        try await clock.fetchToken()
        let atSample = try await clock.command()
        checks.check("clock (b): the first server date alone does not admit a command",
                     isClockRefusal(atSample), atSample)

        clock.time.run(30)
        try await clock.fetchToken()
        clock.time.run(29)
        let atFiftyNine = try await clock.command()
        checks.check("clock (b): 59 seconds after the first server date a command is still refused",
                     isClockRefusal(atFiftyNine), atFiftyNine)

        clock.time.run(1)
        let atSixty = try await clock.command()
        checks.check("clock: a token renewal inside the first stability window does not restart it",
                     isAdmitted(atSixty), atSixty)
    }

    await checks.scenario("renewal on a ready guard") { clock in
        try await clock.fetchToken()
        clock.time.run(60)
        let ready = try await clock.command()
        checks.check("clock: a quiet 60 seconds after the first server date admits a command",
                     isAdmitted(ready), ready)

        try await clock.fetchToken(serverOffset: 1)
        let afterRenewal = try await clock.command()
        checks.check("clock (a): a command right after a token renewal still executes on a ready guard",
                     isAdmitted(afterRenewal), afterRenewal)

        clock.time.run(30)
        let thirtyAfterRenewal = try await clock.command()
        checks.check("clock (a): 30 seconds after a token renewal a command still executes",
                     isAdmitted(thirtyAfterRenewal), thirtyAfterRenewal)

        clock.time.run(210)
        try await clock.fetchToken(serverOffset: -1)
        clock.time.run(2)
        let nextRotation = try await clock.command()
        checks.check("clock (a): the next four-minute rotation leaves the guard ready too",
                     isAdmitted(nextRotation), nextRotation)
    }

    await checks.scenario("renewal carrying clock evidence") { clock in
        try await clock.fetchToken()
        clock.time.run(60)
        _ = try await clock.command()

        try await clock.fetchToken(serverOffset: EpochGuard.maximumServerWallDifference + 1)
        let farSample = try await clock.command()
        checks.check("clock: a renewal whose server date is more than five minutes off still refuses commands",
                     isClockRefusal(farSample), farSample)

        try await clock.fetchToken()
        clock.time.run(60)
        let recovered = try await clock.command()
        checks.check("clock (c): after a rejected far sample the next server date re-establishes calibration",
                     isAdmitted(recovered), recovered)

        clock.time.run(10)
        clock.time.setWall(by: 10)
        try await clock.fetchToken()
        let jumpAtRenewal = try await clock.command()
        checks.check("clock: a wall jump that only a renewal observes is still refused, not absorbed",
                     isClockRefusal(jumpAtRenewal), jumpAtRenewal)

        clock.time.run(60)
        let afterJump = try await clock.command()
        checks.check("clock (c): the renewal that observed the jump starts the recovery window itself",
                     isAdmitted(afterJump), afterJump)
    }

    await checks.scenario("recovery after invalidation") { clock in
        try await clock.fetchToken()
        clock.time.run(60)
        _ = try await clock.command()

        clock.time.setWall(by: -10)
        let rollback = try await clock.command()
        checks.check("clock: a wall rollback on a ready guard refuses the command",
                     isClockRefusal(rollback), rollback)

        clock.time.run(120)
        let withoutSample = try await clock.command()
        checks.check("clock: an invalidated calibration does not heal without a server date",
                     isClockRefusal(withoutSample), withoutSample)

        try await clock.fetchToken()
        let atRecoverySample = try await clock.command()
        checks.check("clock (c): the recovering server date starts a fresh stability window",
                     isClockRefusal(atRecoverySample), atRecoverySample)

        clock.time.run(60)
        let recovered = try await clock.command()
        checks.check("clock (c): the next server date after invalidation re-establishes calibration",
                     isAdmitted(recovered), recovered)
    }

    await checks.scenario("system sleep") { clock in
        try await clock.fetchToken()
        clock.time.run(60)
        _ = try await clock.command()

        clock.time.sleep(3_600)
        let afterWake = try await clock.command()
        checks.check("clock (d): the first command after an hour of system sleep executes",
                     isAdmitted(afterWake), afterWake)

        clock.time.run(5)
        let later = try await clock.command()
        checks.check("clock (d): commands keep executing after wake without a new server date",
                     isAdmitted(later), later)

        clock.time.sleep(600)
        clock.time.setWall(by: 10)
        let jumpAcrossSleep = try await clock.command()
        checks.check("clock (d): a wall jump hidden inside a sleep is still refused",
                     isClockRefusal(jumpAcrossSleep), jumpAcrossSleep)

        let unmodelled = clock.time.unmodelledClocks()
        checks.check("clock: the host clock reads only kernel clocks this fixture models — read \(unmodelled)",
                     unmodelled.isEmpty)
    }
}

func runCloudClockTests() async throws -> Int {
    let wiring = CommandClockWiringChecks()
    await runCommandClockWiringScenarios(wiring)
    let guardChecks: Int
    do {
        guardChecks = try await runCloudClockGuardTests()
    } catch {
        let wiringFailures = wiring.failures.isEmpty ? "" : "; wiring: " + wiring.failures.joined(separator: "; ")
        throw CloudClockTestFailure(description: "\(error)\(wiringFailures)")
    }
    guard wiring.failures.isEmpty else {
        throw CloudClockTestFailure(description:
            "CloudClock wiring: \(wiring.failures.count)/\(wiring.count) failed — "
                + wiring.failures.joined(separator: "; "))
    }
    return guardChecks + wiring.count
}

private func runCloudClockGuardTests() async throws -> Int {
    var checks = 0
    let fiveMinutes: TimeInterval = 300
    let sixtySeconds: TimeInterval = 60
    let twoSeconds: TimeInterval = 2

    func check(_ condition: @autoclosure () -> Bool, _ name: String) throws {
        checks += 1
        guard condition() else {
            throw CloudClockTestFailure(description: "check \(checks) failed: \(name)")
        }
    }

    func checkRelativeRollbackScenarios() throws {
        let frozenWallTime = InjectedCloudTime()
        let frozenWallGuard = try readyGuard(frozenWallTime)
        frozenWallTime.advance(wall: 0, continuous: 3_600)
        let frozenWallUpdate = frozenWallGuard.observe()
        try check(frozenWallUpdate.state == .uncertain(.wallRollback), "frozen wall against advancing continuous clock is rollback")
        try check(frozenWallUpdate.effect == .deleteReservedRow(.wallRollback), "relative rollback requires reserved-row deletion")
        try check(frozenWallGuard.requestAdmission == .unavailable(.wallRollback), "relative rollback makes requests unavailable")

        let advancingWallTime = InjectedCloudTime()
        let advancingWallGuard = try readyGuard(advancingWallTime)
        advancingWallTime.advance(wall: 1, continuous: 61)
        try check(advancingWallGuard.observe().state == .uncertain(.wallRollback), "advancing wall can still roll back relative to continuous clock")

        let absoluteRollbackTime = InjectedCloudTime()
        let absoluteRollbackGuard = try readyGuard(absoluteRollbackTime)
        absoluteRollbackTime.advance(wall: -60, continuous: 0)
        try check(absoluteRollbackGuard.observe().state == .uncertain(.wallRollback), "absolute wall rollback remains a detected control")
    }

    func checkReadyServerRearmCleanup() throws {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        let update = guardUnderTest.acceptServerDate(time.wallDate)
        try check(update.state == .uncertain(.stabilityPeriodIncomplete), "server sample re-arms a ready guard")
        try check(update.effect == .deleteReservedRow(.stabilityPeriodIncomplete), "ready server re-arm requires reserved-row deletion")
        try check(guardUnderTest.requestAdmission == .unavailable(.stabilityPeriodIncomplete), "server re-arm closes admission")
    }

    func checkCumulativeStabilityDrift() throws {
        let time = InjectedCloudTime()
        let guardUnderTest = EpochGuard(clock: time.clock)
        _ = guardUnderTest.acceptServerDate(time.wallDate)
        var update = EpochGuardUpdate(state: guardUnderTest.state, effect: .none)
        for _ in 0..<30 {
            // Each observation drifts by only +1 second, below the per-observation limit,
            // while the complete window accumulates +30 seconds of drift.
            time.advance(wall: 3, continuous: 2)
            update = guardUnderTest.observe()
        }
        try check(update.state == .uncertain(.forwardJump), "cumulative in-window drift is rejected even when every observation is within its limit")
    }

    func checkServerSampleCadenceContract() throws {
        let time = InjectedCloudTime()
        let guardUnderTest = EpochGuard(clock: time.clock)
        var everReady = false
        for _ in 0..<10 {
            let rearm = guardUnderTest.acceptServerDate(time.wallDate)
            everReady = everReady || rearm.state == .ready
            time.advance(wall: 30, continuous: 30)
            let observation = guardUnderTest.observe()
            everReady = everReady || observation.state == .ready
        }
        try check(!everReady, "sampling every 30 seconds intentionally keeps restarting the 60-second stability window")
        time.advance(wall: 30, continuous: 30)
        try check(guardUnderTest.observe().state == .ready, "a full quiet stability window after the last server sample becomes ready")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = EpochGuard(clock: time.clock)
        try check(guardUnderTest.state == .uncertain(.stabilityPeriodIncomplete), "startup is uncertain")
        try check(guardUnderTest.requestAdmission == .unavailable(.stabilityPeriodIncomplete), "startup request is unavailable")
        _ = guardUnderTest.acceptServerDate(time.wallDate)
        try check(guardUnderTest.observe().state == .uncertain(.stabilityPeriodIncomplete), "sample alone is not ready")
        time.advance(wall: sixtySeconds - 0.001, continuous: sixtySeconds - 0.001)
        try check(guardUnderTest.observe().state == .uncertain(.stabilityPeriodIncomplete), "less than 60 seconds is uncertain")
        time.advance(wall: 0.001, continuous: 0.001)
        try check(guardUnderTest.observe().state == .ready, "exactly 60 stable seconds is ready")
        try check(guardUnderTest.requestAdmission == .available, "ready request is available")
    }

    do {
        let time = InjectedCloudTime()
        let exact = EpochGuard(clock: time.clock)
        let exactUpdate = exact.acceptServerDate(Date(timeIntervalSince1970: time.wall + fiveMinutes))
        try check(exactUpdate.state == .uncertain(.stabilityPeriodIncomplete), "exactly five minutes is accepted")
        time.advance(wall: 60, continuous: 60)
        try check(exact.observe().state == .ready, "five-minute boundary sample can become ready")

        let overTime = InjectedCloudTime()
        let over = EpochGuard(clock: overTime.clock)
        let overUpdate = over.acceptServerDate(Date(timeIntervalSince1970: overTime.wall + fiveMinutes + 0.001))
        try check(overUpdate.state == .uncertain(.serverSampleTooFar), "more than five minutes is rejected")
        overTime.advance(wall: 60, continuous: 60)
        try check(over.observe().state == .uncertain(.serverSampleTooFar), "rejected sample cannot become ready")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: -twoSeconds, continuous: 0)
        try check(guardUnderTest.observe().state == .ready, "exactly two-second rollback remains ready")
        time.advance(wall: -twoSeconds, continuous: 0)
        try check(guardUnderTest.observe().state == .ready, "repeated exact rollback boundaries remain ready")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: -(twoSeconds + 0.001), continuous: 0)
        let update = guardUnderTest.observe()
        try check(update.state == .uncertain(.wallRollback), "rollback over two seconds is uncertain")
        try check(update.effect == .deleteReservedRow(.wallRollback), "rollback requires reserved-row deletion")
        try check(guardUnderTest.requestAdmission == .unavailable(.wallRollback), "rollback makes requests unavailable")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: twoSeconds, continuous: 0)
        try check(guardUnderTest.observe().state == .ready, "rollback fixture first reaches positive drift boundary")
        time.advance(wall: -(twoSeconds + 0.001), continuous: 0)
        try check(guardUnderTest.observe().state == .uncertain(.wallRollback), "per-observation rollback is caught inside cumulative boundary")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: 12, continuous: 10)
        try check(guardUnderTest.observe().state == .ready, "exactly two-second relative forward jump remains ready")
        time.advance(wall: 12, continuous: 10)
        try check(guardUnderTest.observe().state == .ready, "repeated exact forward boundaries remain ready")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: 12.001, continuous: 10)
        let update = guardUnderTest.observe()
        try check(update.state == .uncertain(.forwardJump), "relative forward jump over two seconds is uncertain")
        try check(update.effect == .deleteReservedRow(.forwardJump), "forward jump requires reserved-row deletion")
        time.advance(wall: -2.001, continuous: 0)
        try check(guardUnderTest.observe().state == .uncertain(.forwardJump), "putting wall back cannot revive guard")
        try check(guardUnderTest.requestAdmission == .unavailable(.forwardJump), "wall restoration remains unavailable")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: -twoSeconds, continuous: 0)
        try check(guardUnderTest.observe().state == .ready, "forward fixture first reaches negative drift boundary")
        time.advance(wall: twoSeconds + 0.001, continuous: 0)
        try check(guardUnderTest.observe().state == .uncertain(.forwardJump), "per-observation forward jump is caught inside cumulative boundary")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.bootID = "boot-b"
        let update = guardUnderTest.observe()
        try check(update.state == .uncertain(.bootIDChanged), "boot id change is uncertain")
        try check(update.effect == .deleteReservedRow(.bootIDChanged), "boot id change requires deletion")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: 0, continuous: -0.001)
        let update = guardUnderTest.observe()
        try check(update.state == .uncertain(.continuousWentBackwards), "continuous clock reversal is uncertain")
        try check(update.effect == .deleteReservedRow(.continuousWentBackwards), "continuous reversal requires deletion")
    }

    do {
        let time = InjectedCloudTime()
        let original = try readyGuard(time)
        try check(original.state == .ready, "pre-restart guard is ready")
        let restarted = EpochGuard(clock: time.clock)
        try check(restarted.state == .uncertain(.stabilityPeriodIncomplete), "restart is uncertain")
        try check(restarted.requestAdmission == .unavailable(.stabilityPeriodIncomplete), "restart request is unavailable")
    }

    do {
        let time = InjectedCloudTime()
        let guardUnderTest = try readyGuard(time)
        time.advance(wall: 3_600, continuous: 3_600)
        try check(guardUnderTest.observe().state == .ready, "matching wall and continuous sleep advance remains ready")
    }

    try checkRelativeRollbackScenarios()
    try checkReadyServerRearmCleanup()
    try checkCumulativeStabilityDrift()
    try checkServerSampleCadenceContract()

    do {
        let time = InjectedCloudTime()
        let exact = EpochGuard(clock: time.clock)
        _ = exact.acceptServerDate(time.wallDate)
        time.advance(wall: 62, continuous: 60)
        try check(exact.observe().state == .ready, "exactly two seconds of stability drift is ready")

        let overTime = InjectedCloudTime()
        let over = EpochGuard(clock: overTime.clock)
        _ = over.acceptServerDate(overTime.wallDate)
        overTime.advance(wall: 62.001, continuous: 60)
        try check(over.observe().state == .uncertain(.forwardJump), "stability drift over two seconds is uncertain")
        overTime.advance(wall: 60, continuous: 60)
        try check(over.observe().state == .uncertain(.forwardJump), "invalid stability sample requires a new server sample")
    }

    return checks
}

private func readyGuard(_ time: InjectedCloudTime) throws -> EpochGuard {
    let guardUnderTest = EpochGuard(clock: time.clock)
    _ = guardUnderTest.acceptServerDate(time.wallDate)
    time.advance(wall: 60, continuous: 60)
    let update = guardUnderTest.observe()
    guard update.state == .ready else {
        throw CloudClockTestFailure(description: "fixture failed to become ready")
    }
    return guardUnderTest
}
