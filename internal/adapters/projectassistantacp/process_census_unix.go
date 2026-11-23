//go:build linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import "context"

func processIdentities(ctx context.Context, budget int) ([]processIdentity, error) {
	return psIdentities(ctx, budget)
}

// Linux subreapers (PR_SET_CHILD_SUBREAPER) can adopt orphans instead of init.
const orphansGoToInit = false
