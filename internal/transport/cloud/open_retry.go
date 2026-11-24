package cloud

import (
	"errors"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

// The daemon retries a Cloud line whose Open failed, on this ladder. Open
// connects nothing, so what fails there is local: a settings or key file that
// could not be read, a spool that could not be made. Those clear by
// themselves, and until 2026-11-24 one of them kept the phone and
// app.clawdline.com away until settings were saved again or Clawdline
// restarted.
const (
	// LineOpenRetryFirstSecondsLimit is the wait before the first retry of a
	// failed Open; each later failure doubles it.
	LineOpenRetryFirstSecondsLimit = 5
	// LineOpenRetryMaxSecondsLimit is the longest wait between two retries.
	// A line that keeps failing is still tried this often, never given up on.
	LineOpenRetryMaxSecondsLimit = 300
)

// OpenRetryDelay is how long to wait after the failures-th consecutive failed
// Open before trying again: the first rung, doubled per failure, capped.
func OpenRetryDelay(failures int) time.Duration {
	delay := LineOpenRetryFirstSecondsLimit * time.Second
	ceiling := LineOpenRetryMaxSecondsLimit * time.Second
	for i := 1; i < failures && delay < ceiling; i++ {
		delay *= 2
	}
	return min(delay, ceiling)
}

// OpenRefusedForGood reports whether a failed Open is an authoritative answer
// that retrying cannot change: no identity on this machine, an identity minted
// by another control plane, or a credential the far end refused. Those wait
// for somebody to sign in again, which saves settings and reloads the line.
// Every other failure is treated as transient and retried.
func OpenRefusedForGood(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, adaptercloud.ErrNoIdentity),
		errors.Is(err, adaptercloud.ErrOtherEnvironment),
		errors.Is(err, adaptercloud.ErrLoginDenied),
		adaptercloud.IsTerminalAuthorization(err):
		return true
	}
	return false
}
