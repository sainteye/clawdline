package cloud

import (
	"errors"
	"testing"
	"time"
)

// The ladder doubles after the sleep, not before, so the first wait after any
// failure is the initial one however long the previous outage was.
func TestTheLadderDoublesAfterTheWait(t *testing.T) {
	t.Parallel()
	backoff := NewBackoff()
	backoff.Jitter = func() float64 { return 0.5 } // the midpoint, so ×1.0
	want := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	for i, expected := range want {
		if got := backoff.Next(); got != expected {
			t.Fatalf("rung %d: %s, want %s", i, got, expected)
		}
	}
}

// The jitter band is ±25%, symmetric — not full jitter. It exists to stop a
// fleet redialling in lockstep, and a symmetric band keeps the expected delay
// equal to the nominal one.
func TestTheJitterBandIsATwentyFivePercentSpread(t *testing.T) {
	t.Parallel()
	for _, unit := range []float64{0, 0.5, 0.999999} {
		backoff := NewBackoff()
		backoff.Jitter = func() float64 { return unit }
		got := backoff.Next()
		low := time.Duration(float64(InitialBackoff) * 0.75)
		high := time.Duration(float64(InitialBackoff) * 1.25)
		if got < low || got > high {
			t.Fatalf("unit %v gave %s, outside [%s, %s]", unit, got, low, high)
		}
	}
}

// Only a connection that was actually up for the reset window puts the ladder
// back. A socket that is accepted and refused over and over is quick, and
// quick must not read as healthy.
func TestOnlyAStableConnectionResetsTheLadder(t *testing.T) {
	t.Parallel()
	backoff := NewBackoff()
	backoff.Jitter = func() float64 { return 0.5 }
	backoff.Next()
	backoff.Next()
	backoff.Next() // now at 2s

	backoff.ResetIfStable(29 * time.Second)
	if got := backoff.Next(); got != 2*time.Second {
		t.Fatalf("after a 29-second connection: %s, want the ladder kept", got)
	}
	backoff.ResetIfStable(30 * time.Second)
	if got := backoff.Next(); got != InitialBackoff {
		t.Fatalf("after a 30-second connection: %s, want the ladder reset", got)
	}
}

// `unauthorized` is in both tables, and where it arrived decides which wins.
// A 401 at the upgrade is the credential being wrong; the same word in a frame
// on an open socket is the token having aged out.
func TestUnauthorizedMeansDifferentThingsInDifferentPlaces(t *testing.T) {
	t.Parallel()
	upgrade := &UpgradeError{Status: 401}
	if !IsTerminalAuthorization(upgrade) {
		t.Fatal("a 401 at the upgrade is not terminal")
	}
	if IsTokenExpiry(upgrade) {
		t.Fatal("a 401 at the upgrade was read as a token expiry")
	}

	inBand := &RelayError{Code: CodeUnauthorized}
	if !IsTokenExpiry(inBand) {
		t.Fatal("an in-band unauthorized is not a token expiry")
	}
	// It is in the terminal table too; the loop's order is what decides, and
	// that order is tested by the loop, not here. What matters here is that
	// both tables answer.
	if !IsTerminalAuthorization(inBand) {
		t.Fatal("an in-band unauthorized left the terminal table")
	}
}

// A refusal that is not about this credential is retryable, whatever number it
// carries.
func TestAGatewayFailureIsRetryable(t *testing.T) {
	t.Parallel()
	for _, code := range []string{CodeBadGateway, CodeInternal, CodeOverCapacity, CodeRateLimited} {
		err := &RelayError{Code: code}
		if IsTerminalAuthorization(err) {
			t.Fatalf("%s was read as terminal", code)
		}
	}
	if IsTerminalAuthorization(&UpgradeError{Status: 502}) {
		t.Fatal("a 502 at the upgrade was read as terminal")
	}
}

// A revoked device stops, and is not retried into a rate limit.
func TestARevokedDeviceStops(t *testing.T) {
	t.Parallel()
	for _, code := range []string{CodeForbidden, "revoked", "device_revoked", "account_revoked"} {
		if !IsTerminalAuthorization(&RelayError{Code: code}) {
			t.Fatalf("%s did not stop the line", code)
		}
	}
}

// The failure vocabulary is closed, so an operator can grep for one word.
func TestFailureCodesAreTheClosedVocabulary(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		&RelayError{Code: CodeOverCapacity}: "relay_over_capacity",
		&UpgradeError{Status: 503}:          "upgrade_refused_503",
		&CloseError{Code: 4401}:             "closed_4401",
		ErrChallengeTimeout:                 "challenge_timeout",
		ErrReadyTimeout:                     "ready_timeout",
		ErrReceiveTimeout:                   "receive_timeout",
		ErrTokenRotated:                     "token_rotation",
		ErrIdentityBinding:                  "identity_binding",
		ErrUnauthorized:                     "unauthorized",
		errors.New("something else"):        "connection_failed",
	} {
		if got := FailureCode(err); got != want {
			t.Fatalf("%v: %q, want %q", err, got, want)
		}
	}
	if got := FailureCode(nil); got != "" {
		t.Fatalf("no error: %q", got)
	}
}
