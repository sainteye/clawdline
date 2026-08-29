import Foundation

/// Integration contract: `EpochGuardState` is intentionally independent of the
/// `CloudEpochGuardState` consumed by cloud persistence units. A future wiring seam must translate
/// the state and effect explicitly; until that seam exists, the compiler does not guarantee that
/// consumers receive this guard's result.
///
/// Call `acceptServerDate(_:)` only to establish or recover calibration, not for every otherwise
/// valid response. Every accepted sample starts a fresh 60-second stability window. Keeping time
/// accumulated before that new calibration would combine evidence from different baselines, so a
/// caller must leave one complete window after its last sample before expecting admission.
/// The three time inputs are closures so tests and callers can provide one coherent snapshot
/// without consulting real time. The caller must only pass a TLS-authenticated, pinned-HTTPS
/// server `Date` to `acceptServerDate(_:)`; an unauthenticated value defeats this guard.
public struct CloudClock {
    public typealias Wall = () -> Date
    public typealias Continuous = () -> TimeInterval
    public typealias BootID = () -> String

    let wall: Wall
    let continuous: Continuous
    let bootID: BootID

    public init(wall: @escaping Wall, continuous: @escaping Continuous, bootID: @escaping BootID) {
        self.wall = wall
        self.continuous = continuous
        self.bootID = bootID
    }
}

public enum EpochGuardUncertaintyReason: Equatable {
    case stabilityPeriodIncomplete
    case serverSampleTooFar
    case wallRollback
    case forwardJump
    case bootIDChanged
    case continuousWentBackwards
}

public enum EpochGuardState: Equatable {
    case uncertain(EpochGuardUncertaintyReason)
    case ready
}

/// A ready-to-uncertain transition carries the caller's mandatory cleanup obligation.
/// This return value is intentionally not `@discardableResult`.
public enum EpochGuardEffect: Equatable {
    case none
    case deleteReservedRow(EpochGuardUncertaintyReason)
}

public struct EpochGuardUpdate: Equatable {
    public let state: EpochGuardState
    public let effect: EpochGuardEffect
}

public enum EpochGuardAdmission: Equatable {
    case available
    case unavailable(EpochGuardUncertaintyReason)
}

public final class EpochGuard {
    public static let maximumServerWallDifference: TimeInterval = 5 * 60
    public static let requiredStableDuration: TimeInterval = 60
    public static let maximumClockDiscontinuity: TimeInterval = 2

    public private(set) var state: EpochGuardState = .uncertain(.stabilityPeriodIncomplete)

    public var requestAdmission: EpochGuardAdmission {
        switch state {
        case .ready:
            return .available
        case let .uncertain(reason):
            return .unavailable(reason)
        }
    }

    private struct Calibration {
        let wall: Date
        let continuous: TimeInterval
        let bootID: String
    }

    private let clock: CloudClock
    private var calibration: Calibration?
    private var lastWall: Date?
    private var lastContinuous: TimeInterval?
    private var lastBootID: String?

    public init(clock: CloudClock) {
        self.clock = clock
    }

    /// Begins a new fail-closed stability window from a TLS-authenticated server sample.
    /// Re-arming during an incomplete window deliberately restarts the full required duration.
    public func acceptServerDate(_ serverDate: Date) -> EpochGuardUpdate {
        let wall = clock.wall()
        let continuous = clock.continuous()
        let bootID = clock.bootID()

        guard abs(serverDate.timeIntervalSince(wall)) <= Self.maximumServerWallDifference else {
            return becomeUncertain(.serverSampleTooFar, invalidateCalibration: true)
        }

        let wasReady = state == .ready
        calibration = Calibration(wall: wall, continuous: continuous, bootID: bootID)
        lastWall = wall
        lastContinuous = continuous
        lastBootID = bootID
        state = .uncertain(.stabilityPeriodIncomplete)
        return EpochGuardUpdate(
            state: state,
            effect: wasReady ? .deleteReservedRow(.stabilityPeriodIncomplete) : .none
        )
    }

    /// Observes one injected clock snapshot and advances or invalidates the guard.
    public func observe() -> EpochGuardUpdate {
        let wall = clock.wall()
        let continuous = clock.continuous()
        let bootID = clock.bootID()

        guard let calibration else {
            return EpochGuardUpdate(state: state, effect: .none)
        }
        let wasReady = state == .ready

        if bootID != calibration.bootID || bootID != lastBootID {
            return becomeUncertain(.bootIDChanged, invalidateCalibration: true)
        }

        if let lastContinuous, continuous < lastContinuous {
            return becomeUncertain(.continuousWentBackwards, invalidateCalibration: true)
        }

        let wallDelta = wall.timeIntervalSince(lastWall ?? calibration.wall)
        let continuousDelta = continuous - (lastContinuous ?? calibration.continuous)

        if wallDelta - continuousDelta < -Self.maximumClockDiscontinuity {
            return becomeUncertain(.wallRollback, invalidateCalibration: true)
        }

        if wallDelta - continuousDelta > Self.maximumClockDiscontinuity {
            return becomeUncertain(.forwardJump, invalidateCalibration: true)
        }

        lastWall = wall
        lastContinuous = continuous
        lastBootID = bootID

        // Once ready, the contract judges each new discontinuity against the preceding
        // observation. The sample-wide drift bound belongs only to the 60-second window.
        if wasReady {
            return EpochGuardUpdate(state: state, effect: .none)
        }

        let stableElapsed = continuous - calibration.continuous
        let stableWallDelta = wall.timeIntervalSince(calibration.wall)
        let stableDrift = stableWallDelta - stableElapsed

        if abs(stableDrift) > Self.maximumClockDiscontinuity {
            let reason: EpochGuardUncertaintyReason = stableDrift < 0 ? .wallRollback : .forwardJump
            return becomeUncertain(reason, invalidateCalibration: true)
        }

        if stableElapsed >= Self.requiredStableDuration {
            state = .ready
        } else {
            state = .uncertain(.stabilityPeriodIncomplete)
        }
        return EpochGuardUpdate(state: state, effect: .none)
    }

    private func becomeUncertain(
        _ reason: EpochGuardUncertaintyReason,
        invalidateCalibration: Bool
    ) -> EpochGuardUpdate {
        let wasReady = state == .ready
        state = .uncertain(reason)
        if invalidateCalibration {
            calibration = nil
            lastWall = nil
            lastContinuous = nil
            lastBootID = nil
        }
        return EpochGuardUpdate(
            state: state,
            effect: wasReady ? .deleteReservedRow(reason) : .none
        )
    }
}
