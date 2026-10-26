// Package antigravitysession owns personal ACP sessions independently of terminal emulators.
package antigravitysession

import "time"

const (
	MaxSessions   = 2
	MaxFrameBytes = 1 << 20
	MaxViewBytes  = 1 << 20
	// MaxCallTime bounds control RPCs and pipe writes, not model turns or human decisions.
	MaxCallTime           = 45 * time.Second
	MaxHistoryConnections = 1
)
