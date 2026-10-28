package transcript

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// RemoteControlSession is called only for a process positively classified as
// a native Remote Control worker. The registry must name a local conversation,
// a remote bridge and this kernel process generation; a stale PID file alone
// must never manufacture a running conversation.
func (h *Host) RemoteControlSession(ctx context.Context, pid int) (session.Session, bool) {
	if ctx.Err() != nil {
		return session.Session{}, false
	}
	r, ok := h.claudeFor(pid)
	started := ownrecords.ProcessStart(pid)
	return claudeRemoteSession(pid, r, ok, started)
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
