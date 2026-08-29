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

func runCloudClockTests() async throws -> Int {
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
