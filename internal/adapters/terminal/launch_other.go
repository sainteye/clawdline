//go:build !darwin

package terminal

import "context"

// ITermRunning is always false here: there is no iTerm2 off macOS, and
// `auto` then means tmux, as it does on a Mac with iTerm2 shut.
func (l Launcher) ITermRunning(ctx context.Context) (bool, error) { return false, nil }

// NewITermTab is a named refusal rather than a quiet failure.
func (l Launcher) NewITermTab(ctx context.Context, line string) (string, error) {
	return "", errUnsupported("open an iTerm2 tab on this platform")
}

// CloseITermSession closes nothing: there is no iTerm2 session off macOS.
func (l Launcher) CloseITermSession(ctx context.Context, id string) (bool, error) {
	return false, errUnsupported("close an iTerm2 session on this platform")
}
