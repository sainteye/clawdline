package http

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// shellPath reads /v1/sessions/{session}/shells/{shell}. Split before decoded,
// as agentPath is, so an encoded separator can never change the route.
func shellPath(r *http.Request) (sessionID, shellID string, ok bool) {
	rest, cut := strings.CutPrefix(routePath(r), "/v1/sessions/")
	if !cut {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "shells" || parts[2] == "" {
		return "", "", false
	}
	return decodeSegment(parts[0]), decodeSegment(parts[2]), true
}

// shellOutputs is what reads a session's background command output: the
// transcript host in a daemon, anything shaped like it in a test.
type shellOutputs interface {
	ShellOutput(context.Context, session.Session, string, int64) (transcript.ShellOutput, bool)
}

// sessionShellRoute answers the Shell panel: the tail of one command the
// session started in the background (api/v1/shells.schema.json).
func (s *Server) sessionShellRoute(w http.ResponseWriter, r *http.Request, sessionID, shellID string) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a shell's output is a GET")
		return
	}
	window := int64(transcript.ShellOutputDefault)
	if n, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64); err == nil {
		window = n
	}
	window = min(max(window, transcript.ShellOutputFloor), CapacityLimit(capacity.SessionsShellOutputBytes))
	// Refused before the session is looked up: an id that could name anything
	// but a file in the folder is never a question worth a scan.
	if !transcript.ValidShellID(shellID) {
		writeRefusal(w, http.StatusNotFound, "not_found", "that background command is not part of this session")
		return
	}
	reader, ok := s.inventory.Identity.(shellOutputs)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "not_found", "this daemon reads no background commands")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, sessionID)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	out, ok := reader.ShellOutput(ctx, item, shellID, window)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "not_found", "that background command is not part of this session")
		return
	}
	writeJSON(w, contract.ShellOutputReply{
		Shell:     wireShells([]session.Shell{out.Shell})[0],
		Text:      out.Text,
		Truncated: out.Truncated,
		Ended:     out.Ended,
		Signature: out.Signature,
	})
}
