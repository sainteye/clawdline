package cloud

import (
	"testing"
	"time"
)

// fakeClock is one coherent snapshot of the three time sources, moved by hand.
// Nothing here waits: a guard that needed real seconds to be tested would be
// tested once and then never again.
type fakeClock struct {
	wall       time.Time
	continuous time.Duration
	bootID     string
}

func (c *fakeClock) clock() Clock {
	return Clock{
		Wall:       func() time.Time { return c.wall },
		Continuous: func() time.Duration { return c.continuous },
		BootID:     func() string { return c.bootID },
	}
}

// advance moves both clocks together, which is what an ordinary second looks
// like.
func (c *fakeClock) advance(d time.Duration) {
	c.wall = c.wall.Add(d)
	c.continuous += d
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		wall:       time.Date(2026, 9, 18, 3, 0, 0, 0, time.UTC),
		continuous: 10 * time.Minute,
		bootID:     "boot-1",
	}
}

// TestAdmissionOpensOnlyAfterAWholeStableWindow. The guard starts closed, and
// the window is not a formality: a machine that has heard no authenticated time
// cannot tell a replayed command from a fresh one.
func TestAdmissionOpensOnlyAfterAWholeStableWindow(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	guard := NewEpochGuard(clock.clock())

	if ready, reason := guard.Admission(); ready || reason != ReasonStabilityIncomplete {
		t.Fatalf("a fresh guard must be closed: ready=%v reason=%s", ready, reason)
	}
	if update := guard.Observe(); update.State.Ready {
		t.Fatal("observing without a calibration cannot open admission")
	}

	guard.AcceptServerTime(clock.wall)
	if remaining, ok := guard.StabilityRemaining(); !ok || remaining != RequiredStableDuration {
		t.Fatalf("remaining %v (ok=%v), want the whole window", remaining, ok)
	}

	clock.advance(RequiredStableDuration - time.Second)
	if update := guard.Observe(); update.State.Ready {
		t.Fatal("one second short of the window is still closed")
	}
	if remaining, _ := guard.StabilityRemaining(); remaining != time.Second {
		t.Errorf("remaining %v, want 1s", remaining)
	}

	clock.advance(time.Second)
	update := guard.Observe()
	if !update.State.Ready {
		t.Fatal("the window closed and admission did not open")
	}
	if _, ok := guard.StabilityRemaining(); ok {
		t.Error("a ready guard has no countdown to show")
	}
}

// TestEachAnomalyClosesAdmissionAndOwesCleanup. Every one of these is a real
// shape: a laptop waking, a VM restored from a snapshot, somebody changing the
// date, a counter that went backwards.
func TestEachAnomalyClosesAdmissionAndOwesCleanup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		when func(*fakeClock)
		want UncertaintyReason
	}{
		{
			name: "the wall clock jumped forward on its own",
			when: func(c *fakeClock) { c.wall = c.wall.Add(time.Hour) },
			want: ReasonForwardJump,
		},
		{
			name: "the wall clock was turned back",
			when: func(c *fakeClock) { c.wall = c.wall.Add(-time.Hour) },
			want: ReasonWallRollback,
		},
		{
			name: "the machine rebooted",
			when: func(c *fakeClock) { c.advance(time.Second); c.bootID = "boot-2" },
			want: ReasonBootIDChanged,
		},
		{
			name: "the continuous counter went backwards",
			when: func(c *fakeClock) { c.continuous -= time.Second },
			want: ReasonContinuousBackwards,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			clock := newFakeClock()
			guard := NewEpochGuard(clock.clock())
			guard.AcceptServerTime(clock.wall)
			clock.advance(RequiredStableDuration)
			if !guard.Observe().State.Ready {
				t.Fatal("setup: admission should be open")
			}

			c.when(clock)
			update := guard.Observe()
			if update.State.Ready {
				t.Fatal("admission stayed open through an anomaly")
			}
			if update.State.Reason != c.want {
				t.Errorf("reason %s, want %s", update.State.Reason, c.want)
			}
			if !update.DeleteReservedRow {
				t.Error("falling out of ready hands the caller a cleanup it must do")
			}
			if _, ok := guard.StabilityRemaining(); ok {
				t.Error("without a calibration there is no honest countdown")
			}
			// And it stays closed: only a new server sample can recover it.
			clock.advance(2 * RequiredStableDuration)
			if guard.Observe().State.Ready {
				t.Error("time alone recovered a guard that lost its calibration")
			}
		})
	}
}

// TestASampleTooFarFromTheWallClockIsRefused — the sample is authenticated, so
// disagreeing with it means this machine's own clock is wrong.
func TestASampleTooFarFromTheWallClockIsRefused(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	guard := NewEpochGuard(clock.clock())
	update := guard.AcceptServerTime(clock.wall.Add(MaxServerWallDifference + time.Second))
	if update.State.Ready || update.State.Reason != ReasonServerSampleTooFar {
		t.Fatalf("state %+v, want uncertain/server_sample_too_far", update.State)
	}
	if _, ok := guard.StabilityRemaining(); ok {
		t.Error("a refused sample must not start a window")
	}
}

// TestOfferDoesNotRestartALiveWindow is the 2026-09-13 defect: feeding every
// token renewal to AcceptServerTime closed admission for 60 of every 240
// seconds. Offer exists so a sample that arrived for its own reasons is not
// treated as evidence that the clock became uncertain.
func TestOfferDoesNotRestartALiveWindow(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	guard := NewEpochGuard(clock.clock())
	guard.AcceptServerTime(clock.wall)
	clock.advance(RequiredStableDuration)
	if !guard.Observe().State.Ready {
		t.Fatal("setup: admission should be open")
	}

	clock.advance(time.Minute)
	if update := guard.OfferServerTime(clock.wall); !update.State.Ready {
		t.Errorf("an ordinary renewal closed admission: %+v", update.State)
	}

	// The same call on a cold guard calibrates, because there is nothing to
	// disturb.
	cold := NewEpochGuard(clock.clock())
	if update := cold.OfferServerTime(clock.wall); update.State.Reason != ReasonStabilityIncomplete {
		t.Errorf("a cold guard should have started its window: %+v", update.State)
	}

	// And a renewal that arrives after a jump still reports the jump.
	clock.wall = clock.wall.Add(time.Hour)
	update := guard.OfferServerTime(clock.wall)
	if update.State.Ready {
		t.Error("a jump under an offered sample must still close admission")
	}
	if !update.DeleteReservedRow {
		t.Error("the caller still owes the cleanup")
	}
}

// TestTimestampRules pins the two numbers a command's freshness rests on.
func TestTimestampRules(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 18, 3, 0, 0, 0, time.UTC)
	ts := Milliseconds(now)

	if TimeAt(ts) != now {
		t.Errorf("milliseconds did not round-trip: %v", TimeAt(ts))
	}
	if !TimestampWithinSkew(Milliseconds(now.Add(-MaxTimestampSkew)), now) {
		t.Error("exactly at the skew bound is inside it")
	}
	if TimestampWithinSkew(Milliseconds(now.Add(-MaxTimestampSkew-time.Second)), now) {
		t.Error("a second past the bound is refused as clock_skew")
	}
	if TimestampWithinSkew(Milliseconds(now.Add(MaxTimestampSkew+time.Second)), now) {
		t.Error("the bound is two-sided")
	}

	if want := now.Add(CommandLifetime); !CommandDeadline(ts).Equal(want) {
		t.Errorf("deadline %v, want %v", CommandDeadline(ts), want)
	}
	if CommandExpired(ts, now.Add(CommandLifetime-time.Millisecond)) {
		t.Error("a command inside its deadline is not expired")
	}
	if !CommandExpired(ts, now.Add(CommandLifetime)) {
		t.Error("the deadline itself is past")
	}
}
