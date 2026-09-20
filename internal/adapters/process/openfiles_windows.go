//go:build windows

package process

import "context"

// systemOpenFiles is not implemented on Windows, where the process scan it
// serves is not implemented either (ps_windows.go).
//
// It answers false rather than an empty table for the reason that file gives:
// an empty answer would say that a session holds no transcript open, which is
// a claim this has no way to make. False says only that it could not look, and
// a Codex row on Windows is `unreadable` rather than `no_record`.
func systemOpenFiles(ctx context.Context, pids []int) (map[int][]string, bool) {
	return nil, false
}
