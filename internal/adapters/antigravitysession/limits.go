// Package antigravitysession owns personal ACP sessions independently of terminal emulators.
package antigravitysession

import "time"

const (
	MaxFrameBytes = 1 << 20
	MaxViewBytes  = 1 << 20
	// MaxActiveToolBytes bounds exact IDs retained until tool or turn completion.
	MaxActiveToolBytes = 1 << 20
	// MaxCallTime bounds control RPCs and pipe writes, not model turns or human decisions.
	MaxCallTime           = 45 * time.Second
	MaxHistoryConnections = 1
	// MaxQueuedPrompts and MaxQueuedPromptBytes bound the messages a running
	// owned turn holds for after it ends. They are saved in the conversation
	// row, which every streamed update rewrites, so the bytes stay small.
	MaxQueuedPrompts     = 8
	MaxQueuedPromptBytes = 256 << 10
)
