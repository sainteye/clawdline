// Package antigravitysession owns personal ACP sessions independently of terminal emulators.
package antigravitysession

import "time"

const (
	MaxSessions   = 1
	MaxFrameBytes = 1 << 20
	MaxViewBytes  = 1 << 20
	MaxCallTime   = 45 * time.Second
)
