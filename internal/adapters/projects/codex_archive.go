package projects

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

// Codex's own archive, through the app-server that owns its thread index
// (docs/schedules.md "Codex runs leave Codex's list"). An archived thread
// leaves the list Codex and its desktop app show, keeps its rollout under
// ~/.codex/archived_sessions, and is refused by `resume` until it is
// unarchived — measured on codex-cli 0.166.0, 2026-11-11.

// ErrNotCodexConversation is an id that is not a Codex conversation id.
var ErrNotCodexConversation = errors.New("not a codex conversation id")

// CodexArchive archives one conversation. A conversation already archived is
// the answer a second attempt is looking for, and is not an error.
func CodexArchive(ctx context.Context, id string) error {
	return codexThreadCall(ctx, "thread/archive", id, func() bool {
		return transcript.CodexArchivedPath(home(), id) != ""
	})
}

// CodexUnarchive brings an archived conversation back, so that it can be
// resumed. One that is not archived is already where resume looks.
func CodexUnarchive(ctx context.Context, id string) error {
	return codexThreadCall(ctx, "thread/unarchive", id, func() bool {
		return transcript.CodexArchivedPath(home(), id) == "" && transcript.CodexPath(home(), id) != ""
	})
}

// CodexArchived is whether Codex holds this conversation archived.
func CodexArchived(id string) bool {
	id, ok := SessionName(id)
	return ok && transcript.CodexArchivedPath(home(), id) != ""
}

func codexThreadCall(ctx context.Context, method, id string, already func() bool) error {
	id, ok := SessionName(id)
	if !ok {
		return ErrNotCodexConversation
	}
	if already() {
		return nil
	}
	exe := codexExecutable()
	if exe == "" {
		return ErrCodexUnavailable
	}
	select {
	case codexServers <- struct{}{}:
		defer func() { <-codexServers }()
	case <-ctx.Done():
		return errors.Join(ErrCodexUnavailable, ctx.Err())
	}
	_, err := codexRequest(ctx, exe, method, map[string]any{"threadId": id})
	if err != nil && already() {
		return nil
	}
	return err
}
