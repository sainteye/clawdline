package cloud

import "time"

// The clock, and why a cloud client needs a guard in front of it.
//
// Two of this protocol's rules are about time and neither survives a machine
// whose clock moved: an envelope is refused when its ts is more than five
// minutes from the relay's clock, and a command may take effect only inside its
// own 300-second deadline. A laptop that woke from sleep, a VM that was
// restored, a user who changed the date — each of those can make this machine
// believe a stale command is fresh.
//
// So time is not read directly. The guard takes one TLS-authenticated server
// sample, then watches three clocks together: the wall clock, a continuous
// counter that keeps running while the host sleeps, and the boot id. When the
// wall clock moves further than the continuous counter says it should, the
// guard becomes uncertain and every reservation it was backing is given up.
// Admission opens only after a full 60-second window in which nothing moved.
//
// Ported from ~/code/clawdline/Sources/CloudClock.swift; see docs/cloud-wire.md §6.

// What the guard tolerates.
const (
	// MaxServerWallDifference is how far a server sample may sit from this
	// machine's wall clock and still be used to calibrate.
	MaxServerWallDifference = 5 * time.Minute
	// RequiredStableDuration is how long the three clocks must agree before
	// admission opens.
	RequiredStableDuration = 60 * time.Second
	// MaxClockDiscontinuity is how far the wall clock may drift from the
	// continuous counter between two observations.
	MaxClockDiscontinuity = 2 * time.Second
	// MaxTimestampSkew is the window the relay allows around an envelope's ts
	// (MAX_CLOCK_SKEW_MS). Outside it a publish is refused as clock_skew.
	MaxTimestampSkew = 5 * time.Minute
	// CommandLifetime is how long after its ts a command may still take
	// effect. The Swift bridge computes deadline = ts + 300s.
	CommandLifetime = 300 * time.Second
)

// Clock is the three time sources the guard watches, injected so that a test
// and a caller can both present one coherent snapshot.
//
// Continuous must keep counting while the host sleeps. A counter that stops —
// a plain uptime on some platforms — makes every sleep read as a forward jump
// of the wall clock and closes admission for no reason.
type Clock struct {
	Wall       func() time.Time
	Continuous func() time.Duration
	BootID     func() string
}

// UncertaintyReason is why admission is closed. It is reported, logged and
// compared; it is not a wire field.
type UncertaintyReason string

// The six reasons.
const (
	ReasonStabilityIncomplete UncertaintyReason = "stability_period_incomplete"
	ReasonServerSampleTooFar  UncertaintyReason = "server_sample_too_far"
	ReasonWallRollback        UncertaintyReason = "wall_rollback"
	ReasonForwardJump         UncertaintyReason = "forward_jump"
	ReasonBootIDChanged       UncertaintyReason = "boot_id_changed"
	ReasonContinuousBackwards UncertaintyReason = "continuous_went_backwards"
)

// GuardState is either ready, or uncertain with a reason.
type GuardState struct {
	Ready  bool
	Reason UncertaintyReason
}

// GuardUpdate is the new state and the cleanup the caller now owes.
//
// DeleteReservedRow is set exactly when the guard fell out of ready: whatever
// was reserved while time was trusted must be given up, because it was reserved
// against a clock that has since been contradicted. A caller that ignores this
// keeps a row nobody can justify.
type GuardUpdate struct {
	State             GuardState
	DeleteReservedRow bool
	Reason            UncertaintyReason
}

// EpochGuard watches the three clocks. It is not safe for concurrent use; the
// owner serializes it, as the domain layer holds no locks.
type EpochGuard struct {
	clock Clock
	state GuardState

	calibrated     bool
	calWall        time.Time
	calContinuous  time.Duration
	calBootID      string
	lastWall       time.Time
	lastContinuous time.Duration
	lastBootID     string
	observed       bool
}

// NewEpochGuard starts closed, with the stability window incomplete: a machine
// that has heard no authenticated time may not act on a command.
func NewEpochGuard(clock Clock) *EpochGuard {
	return &EpochGuard{
		clock: clock,
		state: GuardState{Reason: ReasonStabilityIncomplete},
	}
}

// State is the current state.
func (g *EpochGuard) State() GuardState { return g.state }

// Admission is whether a command may take effect, and why not when it may not.
func (g *EpochGuard) Admission() (bool, UncertaintyReason) {
	return g.state.Ready, g.state.Reason
}

// AcceptServerTime begins a new stability window from a TLS-authenticated,
// pinned-HTTPS server time. Re-arming during an incomplete window restarts the
// whole duration on purpose: evidence gathered before a new calibration belongs
// to a different baseline.
//
// Call this to establish or recover calibration, never for every otherwise
// valid response — feeding it each token renewal closes admission for a full
// minute after each one. On 2026-09-13 that refused Cloud commands for 60 of
// every 240 seconds. A sample that arrives on its own schedule goes to
// OfferServerTime.
func (g *EpochGuard) AcceptServerTime(server time.Time) GuardUpdate {
	wall := g.clock.Wall()
	continuous := g.clock.Continuous()
	bootID := g.clock.BootID()

	if absDuration(server.Sub(wall)) > MaxServerWallDifference {
		return g.becomeUncertain(ReasonServerSampleTooFar)
	}

	wasReady := g.state.Ready
	g.calibrated = true
	g.calWall, g.calContinuous, g.calBootID = wall, continuous, bootID
	g.lastWall, g.lastContinuous, g.lastBootID = wall, continuous, bootID
	g.observed = true
	g.state = GuardState{Reason: ReasonStabilityIncomplete}
	return GuardUpdate{
		State:             g.state,
		DeleteReservedRow: wasReady,
		Reason:            ReasonStabilityIncomplete,
	}
}

// OfferServerTime offers a sample that arrived for its own reasons — a token
// renewal, a reconnect. Arriving is not evidence that the clock became
// uncertain, so it calibrates only when no calibration is live: at startup, and
// after an anomaly threw one away. While a calibration is live the guard first
// observes the current snapshot, so a discontinuity since the last observation
// still invalidates and this same sample then recovers.
func (g *EpochGuard) OfferServerTime(server time.Time) GuardUpdate {
	if !g.calibrated {
		return g.AcceptServerTime(server)
	}
	observed := g.Observe()
	if !g.calibrated {
		recovered := g.AcceptServerTime(server)
		return GuardUpdate{
			State:             recovered.State,
			DeleteReservedRow: observed.DeleteReservedRow,
			Reason:            observed.Reason,
		}
	}
	if absDuration(server.Sub(g.lastWall)) > MaxServerWallDifference {
		return g.becomeUncertain(ReasonServerSampleTooFar)
	}
	return observed
}

// StabilityRemaining is how much of the window is left as of the last
// observation, and whether such a countdown exists at all. Without a live
// calibration the window cannot close until a new server sample arrives and
// there is no honest number to show. This is a report, never an input to
// admission.
func (g *EpochGuard) StabilityRemaining() (time.Duration, bool) {
	if g.state.Ready || g.state.Reason != ReasonStabilityIncomplete || !g.calibrated {
		return 0, false
	}
	elapsed := g.lastContinuous - g.calContinuous
	remaining := RequiredStableDuration - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// Observe reads one snapshot of the three clocks and advances or invalidates
// the guard.
func (g *EpochGuard) Observe() GuardUpdate {
	wall := g.clock.Wall()
	continuous := g.clock.Continuous()
	bootID := g.clock.BootID()

	if !g.calibrated {
		return GuardUpdate{State: g.state}
	}
	wasReady := g.state.Ready

	if bootID != g.calBootID || bootID != g.lastBootID {
		return g.becomeUncertain(ReasonBootIDChanged)
	}
	if g.observed && continuous < g.lastContinuous {
		return g.becomeUncertain(ReasonContinuousBackwards)
	}

	wallDelta := wall.Sub(g.lastWall)
	continuousDelta := continuous - g.lastContinuous
	switch drift := wallDelta - continuousDelta; {
	case drift < -MaxClockDiscontinuity:
		return g.becomeUncertain(ReasonWallRollback)
	case drift > MaxClockDiscontinuity:
		return g.becomeUncertain(ReasonForwardJump)
	}

	g.lastWall, g.lastContinuous, g.lastBootID = wall, continuous, bootID
	g.observed = true

	// Once ready, each new discontinuity is judged against the preceding
	// observation. The window-wide drift bound belongs only to the 60 seconds
	// that opened admission.
	if wasReady {
		return GuardUpdate{State: g.state}
	}

	elapsed := continuous - g.calContinuous
	drift := wall.Sub(g.calWall) - elapsed
	if absDuration(drift) > MaxClockDiscontinuity {
		if drift < 0 {
			return g.becomeUncertain(ReasonWallRollback)
		}
		return g.becomeUncertain(ReasonForwardJump)
	}
	if elapsed >= RequiredStableDuration {
		g.state = GuardState{Ready: true}
	} else {
		g.state = GuardState{Reason: ReasonStabilityIncomplete}
	}
	return GuardUpdate{State: g.state}
}

func (g *EpochGuard) becomeUncertain(reason UncertaintyReason) GuardUpdate {
	wasReady := g.state.Ready
	g.state = GuardState{Reason: reason}
	g.calibrated = false
	g.calWall, g.calContinuous, g.calBootID = time.Time{}, 0, ""
	g.lastWall, g.lastContinuous, g.lastBootID = time.Time{}, 0, ""
	g.observed = false
	return GuardUpdate{State: g.state, DeleteReservedRow: wasReady, Reason: reason}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// MARK: envelope timestamps

// Milliseconds is a Unix epoch millisecond count, which is what ts and every
// other protocol time is.
func Milliseconds(t time.Time) uint64 {
	ms := t.UnixMilli()
	if ms < 0 {
		return 0
	}
	return uint64(ms)
}

// TimeAt turns a protocol millisecond count back into a time.
func TimeAt(ms uint64) time.Time { return time.UnixMilli(int64(ms)).UTC() }

// TimestampWithinSkew reports whether an envelope's ts is inside the relay's
// acceptance window around now. Outside it the relay answers clock_skew on
// publish, so a client that sends anyway has already lost the message.
func TimestampWithinSkew(ts uint64, now time.Time) bool {
	return absDuration(now.Sub(TimeAt(ts))) <= MaxTimestampSkew
}

// CommandDeadline is when a command carried by an envelope stamped ts stops
// being allowed to take effect.
func CommandDeadline(ts uint64) time.Time { return TimeAt(ts).Add(CommandLifetime) }

// CommandExpired reports whether that deadline has passed. The caller must
// still hold clock admission: an expiry judged by an untrusted clock is not
// evidence of anything.
func CommandExpired(ts uint64, now time.Time) bool { return !now.Before(CommandDeadline(ts)) }
