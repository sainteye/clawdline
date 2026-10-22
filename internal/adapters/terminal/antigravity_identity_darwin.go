package terminal

import "context"

// No Apple Event, activation or permission prompt is permitted without consent.
// iTerm's ordinary list lacks a kernel-backed root PID; until that proof is
// available, keep its native mapping explicitly unavailable even with consent.
func (i *ITerm) ExactAntigravityTerminal(context.Context, int, string) (string, error) {
	return "", identityUnavailable()
}
