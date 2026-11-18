package botdevserver

import "context"

// Listener is a source-owned TCP bind observation, not a probed URL or a
// promise that another device can reach it. No process identity crosses here.
type Listener struct {
	Address string
	Port    uint16
}

// ListeningProcess is optional. Unsupported, unreadable and changing ownership
// remain unknown; they must never become a guessed empty set of listeners.
type ListeningProcess interface {
	TCPListeners(context.Context) ([]Listener, error)
}
