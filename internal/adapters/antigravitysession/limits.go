// Package antigravitysession owns personal ACP sessions independently of terminal emulators.
package antigravitysession

import "time"

const (
	// MaxFrameBytes bounds one ACP line. A native image Read repeats the
	// base64 image in rawOutput and content, so a frame is about 2.7 times the
	// file: 1 MiB ended turns at a 394 KB photo; 8 MiB admits about 3 MB.
	// A whole frame must still fit one MaxWireTraceBytes diagnostic record
	// after base64, which 16 MiB would not.
	MaxFrameBytes = 8 << 20
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
