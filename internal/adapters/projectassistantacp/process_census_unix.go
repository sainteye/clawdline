//go:build linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import "context"

func processIdentities(ctx context.Context, budget int) ([]processIdentity, error) {
	return psIdentities(ctx, budget)
}
