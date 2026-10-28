package transcript

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// RemoteControlSession is called only for a process positively classified as
// a native Remote Control worker. Kernel birth and the observed transport name
// the live bridge. A matching registry can additionally name a local UUID;
// a stale PID file alone never manufactures a running conversation.
func (h *Host) RemoteControlSession(ctx context.Context, pid int, remoteID string) (session.Session, bool) {
	if ctx.Err() != nil {
		return session.Session{}, false
	}
	r, ok := h.claudeFor(pid)
	started := ownrecords.ProcessStart(pid)
	remoteID = process.NormalizeRemoteID(remoteID)
	if remoteID == "" || started.IsZero() {
		return session.Session{}, false
	}
	if row, valid := claudeRemoteSession(pid, r, ok, started); valid && process.NormalizeRemoteID(r.BridgeSessionID) == remoteID {
		row.ID = fmt.Sprintf("claude-remote:%d:%s", pid, remoteID)
		row.RemoteSessionID = remoteID
		return row, true
	}
	// Native 2.1.312 SDK workers can have neither registry nor JSONL, including
	// after a quota failure. The process transport still names the live bridge.
	cwd, _ := process.WorkingDirectory(ctx, pid)
	return session.Session{ID: fmt.Sprintf("claude-remote:%d:%s", pid, remoteID),
		Backend: session.BackendClaudeRemote, PID: pid, ExpectedStart: started, RemoteSessionID: remoteID,
		Assistant: session.AssistantClaude, CWD: cwd, State: session.StateUnknown,
		Evidence: session.EvidenceProcess, Binding: session.BindingNoRecord}, true
}

func claudeRemoteSession(pid int, r ClaudeRegistry, found bool, started time.Time) (session.Session, bool) {
	if !found || pid <= 0 || r.PID != pid || r.PIDDomain != runtime.GOOS ||
		r.Entrypoint != "sdk-cli" || r.BridgeSessionID == "" || !isUUID(r.SessionID) ||
		!filepath.IsAbs(r.CWD) || started.IsZero() {
		return session.Session{}, false
	}
	// Claude's procStart is the LC_ALL=C ps lstart string in the local timezone.
	recorded, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", r.ProcStart, time.Local)
	if err != nil || !recorded.Equal(started) {
		return session.Session{}, false
	}
	return session.Session{
		ID:      fmt.Sprintf("claude-remote:%d:%s", pid, r.SessionID),
		Backend: session.BackendClaudeRemote, PID: pid, ExpectedStart: started,
		Assistant: session.AssistantClaude, ConversationID: r.SessionID, CWD: r.CWD,
		State: session.StateFromAssistantStatus(r.Status), Evidence: session.EvidenceRegistry,
		Binding: session.BindingRegistry,
	}, true
}
