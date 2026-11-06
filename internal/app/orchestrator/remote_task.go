package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// OpenRemoteTask opens the target machine's own Agent in a locally resolved
// Project. The caller must first claim the peer receipt and save the target
// task. An uncertain opening is never retried under that claim.
func (b *Broker) OpenRemoteTask(ctx context.Context, taskID, projectDir string,
	body agenthandoff.TaskBody) (terminalID string, err error) {
	if !IsTaskID(taskID) || b.Dir == "" || !filepath.IsAbs(projectDir) ||
		!session.KnownAssistant(body.Assistant) ||
		strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Instructions) == "" {
		return "", errors.New("remote_task_invalid")
	}
	// The sender's opaque ID never becomes a path component. The brief is a
	// local, private file and its directory is what this Agent may read.
	digest := sha256.Sum256([]byte(taskID))
	dir := filepath.Join(b.Dir, "remote-tasks", hex.EncodeToString(digest[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	brief := filepath.Join(dir, "TASK.md")
	content := "# " + body.Title + "\n\n" + body.Instructions +
		"\n\nTask ID: `" + taskID + "`. The source Agent owns the final report.\n"
	if err := writeFileSync(brief, []byte(content)); err != nil {
		return "", err
	}
	opened, err := b.openSession(ctx, projectDir, sessionName("remote", taskID),
		body.Assistant, "", "", []string{dir})
	if err != nil {
		return "", err
	}
	if err := b.waitComposerOrDialog(ctx, opened.TerminalID, body.Assistant, composerWait); err != nil {
		return "", err
	}
	if err := b.typeLine(ctx, opened.TerminalID,
		"Read the remote task brief at "+brief+" and carry it out. The source Agent owns the final report to the person."); err != nil {
		return "", err
	}
	return opened.TerminalID, nil
}
